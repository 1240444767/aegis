/**
 * 只读 ZIP 读取器，按需随机访问。
 *
 * 安装包动辄几百 MB，整份读进内存再解压既慢又占内存。这里只读末尾的中央目录，
 * 用到哪个条目再 slice 出那一段解压 —— 解析一个 300 MB 的 APK 只会读几 MB。
 * 支持 ZIP64（超过 4 GB 或条目过多的包）；解压用浏览器自带的 DecompressionStream。
 */

export type ZipEntry = {
  name: string;
  method: number;
  compressedSize: number;
  size: number;
  localHeaderOffset: number;
};

export class ZipError extends Error {}

const EOCD_SIG = 0x06054b50;
const ZIP64_LOCATOR_SIG = 0x07064b50;
const ZIP64_EOCD_SIG = 0x06064b50;
const CENTRAL_SIG = 0x02014b50;
const LOCAL_SIG = 0x04034b50;

async function readRange(blob: Blob, start: number, end: number): Promise<DataView> {
  const buffer = await blob.slice(start, end).arrayBuffer();
  return new DataView(buffer);
}

function u64(view: DataView, offset: number) {
  const low = view.getUint32(offset, true);
  const high = view.getUint32(offset + 4, true);
  return high * 0x1_0000_0000 + low;
}

const utf8 = new TextDecoder("utf-8");

export class ZipReader {
  readonly entries = new Map<string, ZipEntry>();
  /** 中央目录的起始偏移；APK 签名块紧贴在它前面 */
  centralDirectoryOffset = 0;

  private constructor(private readonly blob: Blob) {}

  static async open(blob: Blob): Promise<ZipReader> {
    const reader = new ZipReader(blob);
    await reader.load();
    return reader;
  }

  get size() {
    return this.blob.size;
  }

  /** 读原始字节（签名块等不在条目里的数据） */
  async readBytes(start: number, end: number): Promise<Uint8Array> {
    return new Uint8Array(await this.blob.slice(start, end).arrayBuffer());
  }

  names(): string[] {
    return Array.from(this.entries.keys());
  }

  has(name: string) {
    return this.entries.has(name);
  }

  async read(name: string, maxBytes = 64 * 1024 * 1024): Promise<Uint8Array | null> {
    const entry = this.entries.get(name);
    if (!entry) return null;
    if (entry.size > maxBytes) throw new ZipError(`${name} 过大，跳过解析`);
    const header = await readRange(this.blob, entry.localHeaderOffset, entry.localHeaderOffset + 30);
    if (header.getUint32(0, true) !== LOCAL_SIG) throw new ZipError(`${name} 的本地文件头损坏`);
    const nameLength = header.getUint16(26, true);
    const extraLength = header.getUint16(28, true);
    const dataStart = entry.localHeaderOffset + 30 + nameLength + extraLength;
    const raw = this.blob.slice(dataStart, dataStart + entry.compressedSize);
    if (entry.method === 0) return new Uint8Array(await raw.arrayBuffer());
    if (entry.method !== 8) throw new ZipError(`${name} 使用了不支持的压缩方式 ${entry.method}`);
    const stream = raw.stream().pipeThrough(new DecompressionStream("deflate-raw"));
    return new Uint8Array(await new Response(stream).arrayBuffer());
  }

  async readText(name: string): Promise<string | null> {
    const bytes = await this.read(name);
    return bytes ? utf8.decode(bytes) : null;
  }

  private async load() {
    const size = this.blob.size;
    if (size < 22) throw new ZipError("不是有效的 ZIP 文件");
    // EOCD 在末尾 22 字节 + 最多 65535 字节注释之内
    const tailStart = Math.max(0, size - 22 - 0xffff);
    const tail = await readRange(this.blob, tailStart, size);
    let eocd = -1;
    for (let i = tail.byteLength - 22; i >= 0; i--) {
      if (tail.getUint32(i, true) === EOCD_SIG) {
        eocd = i;
        break;
      }
    }
    if (eocd < 0) throw new ZipError("不是有效的 ZIP 文件（找不到目录结尾）");
    let count = tail.getUint16(eocd + 10, true);
    let cdSize = tail.getUint32(eocd + 12, true);
    let cdOffset = tail.getUint32(eocd + 16, true);

    if (count === 0xffff || cdSize === 0xffffffff || cdOffset === 0xffffffff) {
      const locator = eocd - 20;
      if (locator < 0 || tail.getUint32(locator, true) !== ZIP64_LOCATOR_SIG) throw new ZipError("ZIP64 目录定位失败");
      const zip64Offset = u64(tail, locator + 8);
      const z64 = await readRange(this.blob, zip64Offset, zip64Offset + 56);
      if (z64.getUint32(0, true) !== ZIP64_EOCD_SIG) throw new ZipError("ZIP64 目录结尾损坏");
      count = u64(z64, 32);
      cdSize = u64(z64, 40);
      cdOffset = u64(z64, 48);
    }
    this.centralDirectoryOffset = cdOffset;

    const cd = await readRange(this.blob, cdOffset, cdOffset + cdSize);
    let p = 0;
    for (let i = 0; i < count && p + 46 <= cd.byteLength; i++) {
      if (cd.getUint32(p, true) !== CENTRAL_SIG) throw new ZipError("中央目录损坏");
      const method = cd.getUint16(p + 10, true);
      let compressedSize = cd.getUint32(p + 20, true);
      let uncompressedSize = cd.getUint32(p + 24, true);
      const nameLength = cd.getUint16(p + 28, true);
      const extraLength = cd.getUint16(p + 30, true);
      const commentLength = cd.getUint16(p + 32, true);
      let localHeaderOffset = cd.getUint32(p + 42, true);
      const name = utf8.decode(new Uint8Array(cd.buffer, cd.byteOffset + p + 46, nameLength));

      // ZIP64 扩展字段：按「哪个值是 0xFFFFFFFF 就读哪个」的顺序取
      let e = p + 46 + nameLength;
      const extraEnd = e + extraLength;
      while (e + 4 <= extraEnd) {
        const id = cd.getUint16(e, true);
        const len = cd.getUint16(e + 2, true);
        if (id === 0x0001) {
          let q = e + 4;
          if (uncompressedSize === 0xffffffff) {
            uncompressedSize = u64(cd, q);
            q += 8;
          }
          if (compressedSize === 0xffffffff) {
            compressedSize = u64(cd, q);
            q += 8;
          }
          if (localHeaderOffset === 0xffffffff) localHeaderOffset = u64(cd, q);
        }
        e += 4 + len;
      }
      this.entries.set(name, { name, method, compressedSize, size: uncompressedSize, localHeaderOffset });
      p += 46 + nameLength + extraLength + commentLength;
    }
  }
}
