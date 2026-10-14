/**
 * 增量 SHA-256。
 *
 * crypto.subtle.digest 只能一次吃下整份数据，几百 MB 的安装包要先整份读进内存。
 * 这里按块喂入，每块之间让出主线程，界面能照常显示进度、响应点击。
 */

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01,
  0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
  0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
  0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08,
  0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
]);

export class Sha256 {
  private h = new Uint32Array([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19]);
  private w = new Uint32Array(64);
  private buffer = new Uint8Array(64);
  private buffered = 0;
  private length = 0;

  update(data: Uint8Array) {
    let offset = 0;
    this.length += data.length;
    if (this.buffered > 0) {
      const take = Math.min(64 - this.buffered, data.length);
      this.buffer.set(data.subarray(0, take), this.buffered);
      this.buffered += take;
      offset = take;
      if (this.buffered === 64) {
        this.block(this.buffer, 0);
        this.buffered = 0;
      }
    }
    while (offset + 64 <= data.length) {
      this.block(data, offset);
      offset += 64;
    }
    if (offset < data.length) {
      this.buffer.set(data.subarray(offset), 0);
      this.buffered = data.length - offset;
    }
    return this;
  }

  digestHex(): string {
    const bitLength = this.length * 8;
    const pad = new Uint8Array(((this.buffered < 56 ? 56 : 120) - this.buffered) + 8);
    pad[0] = 0x80;
    const view = new DataView(pad.buffer);
    view.setUint32(pad.length - 8, Math.floor(bitLength / 0x1_0000_0000), false);
    view.setUint32(pad.length - 4, bitLength >>> 0, false);
    this.length -= pad.length; // update 会把填充也计入长度，这里先抵消
    this.update(pad);
    return Array.from(this.h, (v) => v.toString(16).padStart(8, "0")).join("");
  }

  private block(data: Uint8Array, offset: number) {
    const w = this.w;
    for (let i = 0; i < 16; i++) {
      const j = offset + i * 4;
      w[i] = (data[j] << 24) | (data[j + 1] << 16) | (data[j + 2] << 8) | data[j + 3];
    }
    for (let i = 16; i < 64; i++) {
      const a = w[i - 15];
      const b = w[i - 2];
      const s0 = ((a >>> 7) | (a << 25)) ^ ((a >>> 18) | (a << 14)) ^ (a >>> 3);
      const s1 = ((b >>> 17) | (b << 15)) ^ ((b >>> 19) | (b << 13)) ^ (b >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0;
    }
    let [a, b, c, d, e, f, g, h] = this.h;
    for (let i = 0; i < 64; i++) {
      const S1 = ((e >>> 6) | (e << 26)) ^ ((e >>> 11) | (e << 21)) ^ ((e >>> 25) | (e << 7));
      const ch = (e & f) ^ (~e & g);
      const t1 = (h + S1 + ch + K[i] + w[i]) | 0;
      const S0 = ((a >>> 2) | (a << 30)) ^ ((a >>> 13) | (a << 19)) ^ ((a >>> 22) | (a << 10));
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) | 0;
      h = g;
      g = f;
      f = e;
      e = (d + t1) | 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) | 0;
    }
    this.h[0] += a;
    this.h[1] += b;
    this.h[2] += c;
    this.h[3] += d;
    this.h[4] += e;
    this.h[5] += f;
    this.h[6] += g;
    this.h[7] += h;
  }
}

const CHUNK = 4 * 1024 * 1024;

/** 分块计算文件的 SHA-256；signal 中止时抛出 AbortError。 */
export async function hashBlob(blob: Blob, onProgress?: (done: number, total: number) => void, signal?: AbortSignal): Promise<string> {
  const hasher = new Sha256();
  for (let offset = 0; offset < blob.size; offset += CHUNK) {
    if (signal?.aborted) throw new DOMException("已取消", "AbortError");
    const chunk = new Uint8Array(await blob.slice(offset, offset + CHUNK).arrayBuffer());
    hasher.update(chunk);
    onProgress?.(Math.min(offset + CHUNK, blob.size), blob.size);
  }
  return hasher.digestHex();
}

/** 小块数据的 SHA-256（证书指纹等），走 WebCrypto。 */
export async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", bytes as Uint8Array<ArrayBuffer>);
  return Array.from(new Uint8Array(digest), (v) => v.toString(16).padStart(2, "0")).join("");
}
