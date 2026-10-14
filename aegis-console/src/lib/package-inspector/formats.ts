/**
 * 安装包解析用到的其余格式：APK 签名块与 X.509 证书、plist（iOS）、protobuf（AAB 清单）。
 */

import { sha256Hex } from "./sha256";
import type { ZipReader } from "./zip";

// ── APK 签名块（v2 / v3） ──────────────────────────

const SIG_BLOCK_MAGIC = "APK Sig Block 42";
const SCHEME_V2 = 0x7109871a;
const SCHEME_V3 = 0xf05368c0;
const SCHEME_V31 = 0x1b93ad61;

export type SignerInfo = {
  scheme: string;
  sha256: string;
  subject?: string;
  issuer?: string;
  notAfter?: string;
};

function lengthPrefixed(view: DataView, offset: number): [number, number] {
  const len = view.getUint32(offset, true);
  return [offset + 4, offset + 4 + len];
}

/** 读签名块里第一个签名者的第一张证书。没有 v2/v3 签名（只有 v1）时返回 null。 */
export async function readApkSigner(zip: ZipReader): Promise<SignerInfo | null> {
  const cdOffset = zip.centralDirectoryOffset;
  if (cdOffset < 32) return null;
  const footer = await zip.readBytes(cdOffset - 24, cdOffset);
  const magic = new TextDecoder().decode(footer.subarray(8, 24));
  if (magic !== SIG_BLOCK_MAGIC) return null;
  const footerView = new DataView(footer.buffer, footer.byteOffset, footer.byteLength);
  const blockSize = footerView.getUint32(0, true) + footerView.getUint32(4, true) * 0x1_0000_0000;
  if (blockSize > 64 * 1024 * 1024 || blockSize + 8 > cdOffset) return null;
  const block = await zip.readBytes(cdOffset - blockSize - 8, cdOffset);
  const view = new DataView(block.buffer, block.byteOffset, block.byteLength);

  const schemes = new Map<number, [number, number]>();
  let p = 8;
  const end = block.byteLength - 24;
  while (p + 12 <= end) {
    const len = view.getUint32(p, true) + view.getUint32(p + 4, true) * 0x1_0000_0000;
    const id = view.getUint32(p + 8, true);
    schemes.set(id, [p + 12, p + 8 + len]);
    p += 8 + len;
  }
  for (const [id, label] of [
    [SCHEME_V31, "v3.1"],
    [SCHEME_V3, "v3"],
    [SCHEME_V2, "v2"]
  ] as const) {
    const range = schemes.get(id);
    if (!range) continue;
    try {
      // signers（长度前缀序列）→ 第一个 signer → signed data → digests、certificates
      const [signersStart] = lengthPrefixed(view, range[0]);
      const [signerStart] = lengthPrefixed(view, signersStart);
      const [signedStart] = lengthPrefixed(view, signerStart);
      const [, digestsEnd] = lengthPrefixed(view, signedStart);
      const [certsStart] = lengthPrefixed(view, digestsEnd);
      const [certStart, certEnd] = lengthPrefixed(view, certsStart);
      const der = block.subarray(certStart, certEnd);
      const fingerprint = await sha256Hex(der);
      return { scheme: label, sha256: fingerprint, ...describeCertificate(der) };
    } catch {
      continue;
    }
  }
  return null;
}

// ── 最小 DER 解析：只取证书的主体、颁发者与有效期 ──────────────────────────

type Tlv = { tag: number; start: number; end: number; next: number };

function readTlv(der: Uint8Array, offset: number): Tlv {
  const tag = der[offset];
  let len = der[offset + 1];
  let p = offset + 2;
  if (len & 0x80) {
    const n = len & 0x7f;
    len = 0;
    for (let i = 0; i < n; i++) len = len * 256 + der[p + i];
    p += n;
  }
  return { tag, start: p, end: p + len, next: p + len };
}

function children(der: Uint8Array, tlv: Tlv): Tlv[] {
  const out: Tlv[] = [];
  for (let p = tlv.start; p < tlv.end; ) {
    const child = readTlv(der, p);
    out.push(child);
    p = child.next;
  }
  return out;
}

const OID_NAMES: Record<string, string> = { "2.5.4.3": "CN", "2.5.4.10": "O", "2.5.4.11": "OU", "2.5.4.7": "L", "2.5.4.8": "ST", "2.5.4.6": "C" };

function oid(der: Uint8Array, tlv: Tlv) {
  const bytes = der.subarray(tlv.start, tlv.end);
  const parts = [Math.floor(bytes[0] / 40), bytes[0] % 40];
  let value = 0;
  for (let i = 1; i < bytes.length; i++) {
    value = value * 128 + (bytes[i] & 0x7f);
    if (!(bytes[i] & 0x80)) {
      parts.push(value);
      value = 0;
    }
  }
  return parts.join(".");
}

function nameString(der: Uint8Array, name: Tlv) {
  const parts: string[] = [];
  for (const rdn of children(der, name)) {
    for (const attr of children(der, rdn)) {
      const [type, value] = children(der, attr);
      if (!type || !value) continue;
      const key = OID_NAMES[oid(der, type)];
      if (!key) continue;
      const text = value.tag === 0x1e ? new TextDecoder("utf-16be").decode(der.subarray(value.start, value.end)) : new TextDecoder().decode(der.subarray(value.start, value.end));
      parts.push(`${key}=${text}`);
    }
  }
  return parts.join(", ");
}

function timeString(der: Uint8Array, t: Tlv) {
  const s = new TextDecoder().decode(der.subarray(t.start, t.end));
  const full = t.tag === 0x17 ? (Number(s.slice(0, 2)) >= 50 ? "19" : "20") + s : s;
  return `${full.slice(0, 4)}-${full.slice(4, 6)}-${full.slice(6, 8)}`;
}

function describeCertificate(der: Uint8Array): Pick<SignerInfo, "subject" | "issuer" | "notAfter"> {
  try {
    const cert = readTlv(der, 0);
    const tbs = children(der, cert)[0];
    const fields = children(der, tbs);
    // [0] 版本是可选的
    const offset = fields[0]?.tag === 0xa0 ? 1 : 0;
    const issuer = fields[offset + 2];
    const validity = fields[offset + 3];
    const subject = fields[offset + 4];
    const [, notAfter] = validity ? children(der, validity) : [];
    return {
      subject: subject ? nameString(der, subject) : undefined,
      issuer: issuer ? nameString(der, issuer) : undefined,
      notAfter: notAfter ? timeString(der, notAfter) : undefined
    };
  } catch {
    return {};
  }
}

// ── plist ──────────────────────────

export type PlistValue = string | number | boolean | null | PlistValue[] | { [key: string]: PlistValue };

/** 解析二进制（bplist00）或 XML plist。 */
export function parsePlist(bytes: Uint8Array): PlistValue {
  const head = new TextDecoder().decode(bytes.subarray(0, 8));
  if (head.startsWith("bplist00")) return parseBinaryPlist(bytes);
  return parseXmlPlist(new TextDecoder().decode(bytes));
}

function parseBinaryPlist(bytes: Uint8Array): PlistValue {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const trailer = bytes.byteLength - 32;
  const offsetSize = view.getUint8(trailer + 6);
  const refSize = view.getUint8(trailer + 7);
  const numObjects = Number(view.getBigUint64(trailer + 8));
  const top = Number(view.getBigUint64(trailer + 16));
  const tableOffset = Number(view.getBigUint64(trailer + 24));
  const readUint = (offset: number, size: number) => {
    let v = 0;
    for (let i = 0; i < size; i++) v = v * 256 + bytes[offset + i];
    return v;
  };
  const offsets = Array.from({ length: numObjects }, (_, i) => readUint(tableOffset + i * offsetSize, offsetSize));
  const seen = new Set<number>();

  const readObject = (ref: number): PlistValue => {
    if (seen.has(ref)) return null;
    const offset = offsets[ref];
    const marker = bytes[offset];
    const high = marker >> 4;
    let low = marker & 0x0f;
    let p = offset + 1;
    const readLength = () => {
      if (low !== 0x0f) return low;
      const intMarker = bytes[p];
      const size = 1 << (intMarker & 0x0f);
      const v = readUint(p + 1, size);
      p += 1 + size;
      return v;
    };
    switch (high) {
      case 0x0:
        return low === 0x8 ? false : low === 0x9 ? true : null;
      case 0x1: {
        const size = 1 << low;
        return size === 8 ? Number(view.getBigInt64(p)) : readUint(p, size);
      }
      case 0x2:
        return low === 2 ? view.getFloat32(p) : view.getFloat64(p);
      case 0x3:
        return new Date(Date.UTC(2001, 0, 1) + view.getFloat64(p) * 1000).toISOString();
      case 0x4: {
        const len = readLength();
        return `<${len} bytes>`;
      }
      case 0x5: {
        const len = readLength();
        return new TextDecoder("latin1").decode(bytes.subarray(p, p + len));
      }
      case 0x6: {
        const len = readLength();
        return new TextDecoder("utf-16be").decode(bytes.subarray(p, p + len * 2));
      }
      case 0x8:
        return readUint(p, low + 1);
      case 0xa: {
        const len = readLength();
        seen.add(ref);
        const out = Array.from({ length: len }, (_, i) => readObject(readUint(p + i * refSize, refSize)));
        seen.delete(ref);
        return out;
      }
      case 0xd: {
        const len = readLength();
        seen.add(ref);
        const out: Record<string, PlistValue> = {};
        for (let i = 0; i < len; i++) {
          const key = readObject(readUint(p + i * refSize, refSize));
          out[String(key)] = readObject(readUint(p + (len + i) * refSize, refSize));
        }
        seen.delete(ref);
        return out;
      }
      default:
        low = 0;
        return null;
    }
  };
  return readObject(top);
}

function parseXmlPlist(text: string): PlistValue {
  if (typeof DOMParser === "undefined") return null;
  const doc = new DOMParser().parseFromString(text, "application/xml");
  const read = (el: Element): PlistValue => {
    switch (el.tagName) {
      case "dict": {
        const out: Record<string, PlistValue> = {};
        const items = Array.from(el.children);
        for (let i = 0; i + 1 < items.length; i += 2) out[items[i].textContent ?? ""] = read(items[i + 1]);
        return out;
      }
      case "array":
        return Array.from(el.children).map(read);
      case "string":
      case "date":
        return el.textContent ?? "";
      case "integer":
      case "real":
        return Number(el.textContent);
      case "true":
        return true;
      case "false":
        return false;
      default:
        return null;
    }
  };
  const root = doc.documentElement?.firstElementChild;
  return root ? read(root) : null;
}

// ── protobuf（AAB 的 AndroidManifest.xml 是 aapt2 的 XmlNode） ──────────────────────────

type PbField = { field: number; wire: number; varint?: number; bytes?: Uint8Array };

function readVarint(bytes: Uint8Array, p: number): [number, number] {
  let value = 0;
  let shift = 0;
  while (p < bytes.length) {
    const b = bytes[p++];
    value += (b & 0x7f) * 2 ** shift;
    if (!(b & 0x80)) break;
    shift += 7;
  }
  return [value, p];
}

function pbFields(bytes: Uint8Array): PbField[] {
  const out: PbField[] = [];
  let p = 0;
  while (p < bytes.length) {
    const [key, next] = readVarint(bytes, p);
    p = next;
    const field = Math.floor(key / 8);
    const wire = key & 7;
    if (wire === 0) {
      const [v, n] = readVarint(bytes, p);
      out.push({ field, wire, varint: v });
      p = n;
    } else if (wire === 2) {
      const [len, n] = readVarint(bytes, p);
      out.push({ field, wire, bytes: bytes.subarray(n, n + len) });
      p = n + len;
    } else if (wire === 5) {
      p += 4;
    } else if (wire === 1) {
      p += 8;
    } else {
      break;
    }
  }
  return out;
}

export type PbElement = { name: string; attrs: Record<string, string | number>; children: PbElement[] };

const pbText = new TextDecoder();

/** XmlNode{element=1} → XmlElement{name=3, attribute=4, child=5}；属性 XmlAttribute{name=2, value=3, compiled_item=6} */
export function parseProtoXml(bytes: Uint8Array): PbElement | null {
  const elementBytes = pbFields(bytes).find((f) => f.field === 1 && f.bytes)?.bytes;
  return elementBytes ? parseProtoElement(elementBytes) : null;
}

function parseProtoElement(bytes: Uint8Array): PbElement {
  const el: PbElement = { name: "", attrs: {}, children: [] };
  for (const f of pbFields(bytes)) {
    if (f.field === 3 && f.bytes) el.name = pbText.decode(f.bytes);
    else if (f.field === 4 && f.bytes) {
      let name = "";
      let value: string | number = "";
      for (const a of pbFields(f.bytes)) {
        if (a.field === 2 && a.bytes) name = pbText.decode(a.bytes);
        else if (a.field === 3 && a.bytes) value = pbText.decode(a.bytes);
        else if (a.field === 6 && a.bytes && value === "") {
          // Item{prim=7} → Primitive{int_decimal_value=6, int_hexadecimal_value=7, boolean_value=8}
          const prim = pbFields(a.bytes).find((x) => x.field === 7 && x.bytes)?.bytes;
          const pv = prim ? pbFields(prim).find((x) => x.varint !== undefined) : undefined;
          if (pv?.varint !== undefined) value = pv.varint;
        }
      }
      if (name) el.attrs[name] = value;
    } else if (f.field === 5 && f.bytes) {
      const child = parseProtoXml(f.bytes);
      if (child) el.children.push(child);
    }
  }
  return el;
}
