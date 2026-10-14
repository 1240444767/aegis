/**
 * Android 二进制格式：编译后的 XML（AndroidManifest.xml、res/*.xml）与资源表（resources.arsc）。
 *
 * 只实现读清单与解析名称、图标需要的部分。格式参考 AOSP
 * frameworks/base/libs/androidfw/include/androidfw/ResourceTypes.h。
 */

const RES_STRING_POOL_TYPE = 0x0001;
const RES_TABLE_TYPE = 0x0002;
const RES_XML_TYPE = 0x0003;
const RES_XML_START_ELEMENT_TYPE = 0x0102;
const RES_XML_END_ELEMENT_TYPE = 0x0103;
const RES_XML_RESOURCE_MAP_TYPE = 0x0180;
const RES_TABLE_PACKAGE_TYPE = 0x0200;
const RES_TABLE_TYPE_TYPE = 0x0201;

const TYPE_REFERENCE = 0x01;
const TYPE_STRING = 0x03;
const TYPE_FLOAT = 0x04;
const TYPE_INT_DEC = 0x10;
const TYPE_INT_HEX = 0x11;
const TYPE_INT_BOOLEAN = 0x12;
const TYPE_FIRST_COLOR = 0x1c;
const TYPE_LAST_COLOR = 0x1f;

/** 常用属性的资源 ID：混淆过的包会把属性名清空，只能靠 ID 认 */
const ATTR_BY_ID: Record<number, string> = {
  0x01010001: "label",
  0x01010002: "icon",
  0x01010003: "name",
  0x0101020c: "minSdkVersion",
  0x0101021b: "versionCode",
  0x0101021c: "versionName",
  0x01010270: "targetSdkVersion",
  0x0101052c: "roundIcon",
  0x01010572: "compileSdkVersion",
  0x01010573: "compileSdkVersionCodename",
  0x01010199: "drawable",
  0x0101028e: "required"
};

export type XmlValue =
  | { kind: "string"; value: string }
  | { kind: "int"; value: number }
  | { kind: "bool"; value: boolean }
  | { kind: "ref"; value: number }
  | { kind: "color"; value: number }
  | { kind: "other"; value: number };

export type XmlElement = {
  name: string;
  attrs: Record<string, XmlValue>;
  children: XmlElement[];
};

const utf16 = new TextDecoder("utf-16le");
const utf8 = new TextDecoder("utf-8");

function readStringPool(view: DataView, start: number): string[] {
  const headerSize = view.getUint16(start + 2, true);
  const count = view.getUint32(start + 8, true);
  const flags = view.getUint32(start + 16, true);
  const stringsStart = view.getUint32(start + 20, true);
  const isUtf8 = (flags & 0x100) !== 0;
  const offsets = start + headerSize;
  const base = start + stringsStart;
  const out: string[] = new Array(count);
  const bytes = new Uint8Array(view.buffer, view.byteOffset, view.byteLength);
  for (let i = 0; i < count; i++) {
    let p = base + view.getUint32(offsets + i * 4, true);
    try {
      if (isUtf8) {
        // 先是字符数（1-2 字节），再是字节数（1-2 字节）
        let n = bytes[p++];
        if (n & 0x80) p++;
        let len = bytes[p++];
        if (len & 0x80) len = ((len & 0x7f) << 8) | bytes[p++];
        out[i] = utf8.decode(bytes.subarray(p, p + len));
      } else {
        let len = view.getUint16(p, true);
        p += 2;
        if (len & 0x8000) {
          len = ((len & 0x7fff) << 16) | view.getUint16(p, true);
          p += 2;
        }
        out[i] = utf16.decode(bytes.subarray(p, p + len * 2));
      }
    } catch {
      out[i] = "";
    }
  }
  return out;
}

function typedValue(dataType: number, data: number, strings: string[], raw: number): XmlValue {
  if (dataType === TYPE_STRING) return { kind: "string", value: strings[data] ?? "" };
  if (dataType === TYPE_REFERENCE) return { kind: "ref", value: data >>> 0 };
  if (dataType === TYPE_INT_BOOLEAN) return { kind: "bool", value: data !== 0 };
  if (dataType === TYPE_INT_DEC || dataType === TYPE_INT_HEX) return { kind: "int", value: data | 0 };
  if (dataType >= TYPE_FIRST_COLOR && dataType <= TYPE_LAST_COLOR) return { kind: "color", value: data >>> 0 };
  if (dataType === TYPE_FLOAT) return { kind: "other", value: new Float32Array(new Uint32Array([data]).buffer)[0] };
  if (raw !== 0xffffffff && strings[raw] !== undefined) return { kind: "string", value: strings[raw] };
  return { kind: "other", value: data >>> 0 };
}

/** 解析编译后的 XML，返回根元素。 */
export function parseBinaryXml(bytes: Uint8Array): XmlElement | null {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  if (view.byteLength < 8 || view.getUint16(0, true) !== RES_XML_TYPE) return null;
  let strings: string[] = [];
  let resourceIds: number[] = [];
  const stack: XmlElement[] = [];
  let root: XmlElement | null = null;
  let p = view.getUint16(2, true);
  while (p + 8 <= view.byteLength) {
    const type = view.getUint16(p, true);
    const headerSize = view.getUint16(p + 2, true);
    const size = view.getUint32(p + 4, true);
    if (size < 8) break;
    if (type === RES_STRING_POOL_TYPE) {
      strings = readStringPool(view, p);
    } else if (type === RES_XML_RESOURCE_MAP_TYPE) {
      resourceIds = [];
      for (let q = p + headerSize; q + 4 <= p + size; q += 4) resourceIds.push(view.getUint32(q, true));
    } else if (type === RES_XML_START_ELEMENT_TYPE) {
      const ext = p + headerSize;
      const name = strings[view.getUint32(ext + 4, true)] ?? "";
      const attributeStart = view.getUint16(ext + 8, true);
      const attributeSize = view.getUint16(ext + 10, true);
      const attributeCount = view.getUint16(ext + 12, true);
      const element: XmlElement = { name, attrs: {}, children: [] };
      for (let i = 0; i < attributeCount; i++) {
        const a = ext + attributeStart + i * attributeSize;
        const nameIndex = view.getUint32(a + 4, true);
        const rawValue = view.getUint32(a + 8, true);
        const dataType = view.getUint8(a + 15);
        const data = view.getUint32(a + 16, true);
        const attrName = ATTR_BY_ID[resourceIds[nameIndex]] ?? strings[nameIndex] ?? "";
        if (attrName) element.attrs[attrName] = typedValue(dataType, data, strings, rawValue);
      }
      if (stack.length) stack[stack.length - 1].children.push(element);
      else root = element;
      stack.push(element);
    } else if (type === RES_XML_END_ELEMENT_TYPE) {
      stack.pop();
    }
    p += size;
  }
  return root;
}

export function attrString(el: XmlElement | undefined, name: string): string | undefined {
  const v = el?.attrs[name];
  if (!v) return undefined;
  if (v.kind === "string") return v.value;
  if (v.kind === "int" || v.kind === "other") return String(v.value);
  if (v.kind === "bool") return String(v.value);
  return undefined;
}

export function attrInt(el: XmlElement | undefined, name: string): number | undefined {
  const v = el?.attrs[name];
  if (!v) return undefined;
  if (v.kind === "int") return v.value;
  if (v.kind === "string" && /^-?\d+$/.test(v.value.trim())) return Number(v.value.trim());
  return undefined;
}

export function findChildren(el: XmlElement | undefined, name: string): XmlElement[] {
  return el?.children.filter((child) => child.name === name) ?? [];
}

// ── 资源表 ──────────────────────────

export type ResourceCandidate = {
  /** 配置的语言（空串为默认） */
  language: string;
  /** 屏幕密度（0 为默认，65534 为 anydpi，65535 为 nodpi） */
  density: number;
  value: XmlValue;
};

/** resources.arsc：资源 ID → 各配置下的取值。只建索引，不展开复杂资源（style、array）。 */
export class ResourceTable {
  private readonly values = new Map<number, ResourceCandidate[]>();
  private readonly keyNames = new Map<number, string>();

  static parse(bytes: Uint8Array): ResourceTable | null {
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    if (view.byteLength < 12 || view.getUint16(0, true) !== RES_TABLE_TYPE) return null;
    const table = new ResourceTable();
    let globalStrings: string[] = [];
    let p = view.getUint16(2, true);
    while (p + 8 <= view.byteLength) {
      const type = view.getUint16(p, true);
      const size = view.getUint32(p + 4, true);
      if (size < 8) break;
      if (type === RES_STRING_POOL_TYPE) globalStrings = readStringPool(view, p);
      else if (type === RES_TABLE_PACKAGE_TYPE) table.readPackage(view, p, globalStrings);
      p += size;
    }
    return table;
  }

  private readPackage(view: DataView, start: number, globalStrings: string[]) {
    const headerSize = view.getUint16(start + 2, true);
    const size = view.getUint32(start + 4, true);
    const packageId = view.getUint32(start + 8, true);
    const keyStringsOffset = view.getUint32(start + 8 + 4 + 256 + 8, true);
    let keyStrings: string[] = [];
    if (keyStringsOffset && start + keyStringsOffset < view.byteLength) {
      try {
        keyStrings = readStringPool(view, start + keyStringsOffset);
      } catch {
        keyStrings = [];
      }
    }
    let p = start + headerSize;
    const end = start + size;
    while (p + 8 <= end) {
      const type = view.getUint16(p, true);
      const chunkSize = view.getUint32(p + 4, true);
      if (chunkSize < 8) break;
      if (type === RES_TABLE_TYPE_TYPE) this.readType(view, p, packageId, globalStrings, keyStrings);
      p += chunkSize;
    }
  }

  private readType(view: DataView, start: number, packageId: number, strings: string[], keyStrings: string[]) {
    const headerSize = view.getUint16(start + 2, true);
    const typeId = view.getUint8(start + 8);
    const flags = view.getUint8(start + 9);
    const entryCount = view.getUint32(start + 12, true);
    const entriesStart = view.getUint32(start + 16, true);
    const config = start + 20;
    const language = String.fromCharCode(view.getUint8(config + 8), view.getUint8(config + 9)).replace(/\0/g, "");
    const density = view.getUint16(config + 14, true);
    const sparse = (flags & 0x01) !== 0;
    const offset16 = (flags & 0x02) !== 0;
    const offsets = start + headerSize;

    const visit = (entryIndex: number, entryOffset: number) => {
      const e = start + entriesStart + entryOffset;
      if (e + 8 > view.byteLength) return;
      const entrySize = view.getUint16(e, true);
      const entryFlags = view.getUint16(e + 2, true);
      const id = ((packageId & 0xff) << 24) | (typeId << 16) | entryIndex;
      let value: XmlValue;
      let keyIndex: number;
      if (entryFlags & 0x0008) {
        // 紧凑条目（Android 14+）：key 在前 2 字节，类型在 flags 高字节，数据紧随其后
        keyIndex = entrySize;
        value = typedValue(entryFlags >>> 8, view.getUint32(e + 4, true), strings, 0xffffffff);
      } else {
        keyIndex = view.getUint32(e + 4, true);
        if (entryFlags & 0x0001) return; // 复杂资源（style、array 等）
        const v = e + entrySize;
        if (v + 8 > view.byteLength) return;
        value = typedValue(view.getUint8(v + 3), view.getUint32(v + 4, true), strings, 0xffffffff);
      }
      const list = this.values.get(id >>> 0) ?? [];
      list.push({ language, density, value });
      this.values.set(id >>> 0, list);
      if (!this.keyNames.has(id >>> 0) && keyStrings[keyIndex]) this.keyNames.set(id >>> 0, keyStrings[keyIndex]);
    };

    if (sparse) {
      for (let i = 0; i < entryCount; i++) {
        const idx = view.getUint16(offsets + i * 4, true);
        const off = view.getUint16(offsets + i * 4 + 2, true) * 4;
        visit(idx, off);
      }
      return;
    }
    for (let i = 0; i < entryCount; i++) {
      const off = offset16 ? view.getUint16(offsets + i * 2, true) : view.getUint32(offsets + i * 4, true);
      if (offset16 ? off === 0xffff : off === 0xffffffff) continue;
      visit(i, offset16 ? off * 4 : off);
    }
  }

  candidates(id: number): ResourceCandidate[] {
    return this.values.get(id >>> 0) ?? [];
  }

  keyName(id: number) {
    return this.keyNames.get(id >>> 0);
  }

  /** 解析字符串资源：优先默认语言，其次中文，再其次任意语言；引用链最多跟 5 层。 */
  resolveString(id: number, depth = 0): string | undefined {
    if (depth > 5) return undefined;
    const list = this.candidates(id);
    const rank = (c: ResourceCandidate) => (c.language === "" ? 0 : c.language === "zh" ? 1 : 2);
    for (const c of [...list].sort((a, b) => rank(a) - rank(b))) {
      if (c.value.kind === "string" && c.value.value) return c.value.value;
      if (c.value.kind === "ref") {
        const nested = this.resolveString(c.value.value, depth + 1);
        if (nested) return nested;
      }
    }
    return undefined;
  }

  /** 文件型资源（图标）的候选路径，按密度从高到低；anydpi（自适应图标）排最后。 */
  resolveFiles(id: number, depth = 0): Array<{ path: string; density: number }> {
    if (depth > 5) return [];
    const out: Array<{ path: string; density: number }> = [];
    for (const c of this.candidates(id)) {
      if (c.value.kind === "string") out.push({ path: c.value.value, density: c.density });
      else if (c.value.kind === "ref") out.push(...this.resolveFiles(c.value.value, depth + 1));
    }
    const score = (d: number) => (d === 0xfffe ? -1 : d === 0xffff ? 0 : d === 0 ? 160 : d);
    return out.sort((a, b) => score(b.density) - score(a.density));
  }

  /** 颜色资源（自适应图标的背景色） */
  resolveColor(id: number, depth = 0): number | undefined {
    if (depth > 5) return undefined;
    for (const c of this.candidates(id)) {
      if (c.value.kind === "color") return c.value.value;
      if (c.value.kind === "ref") {
        const nested = this.resolveColor(c.value.value, depth + 1);
        if (nested !== undefined) return nested;
      }
    }
    return undefined;
  }
}
