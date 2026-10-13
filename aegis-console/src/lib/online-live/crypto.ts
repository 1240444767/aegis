/**
 * 实时在线统计长连接的应用层加密（浏览器一侧）。
 *
 * 与后端 internal/service/online_live_crypto.go 一一对应，协议见 docs/online-live.md：
 *   ECDH P-256 交换临时公钥 → HKDF-SHA256 派生两把 AES-256-GCM 密钥 → 逐帧加密。
 * info 绑定访问令牌的 SHA-256：拿不到令牌的中间人替换公钥后派生不出同一把钥匙。
 *
 * 帧：[版本 1][标志 0][序号 8 字节大端] + 密文与 16 字节标签；
 * nonce = 方向前缀 4 字节 + 序号；AAD = 帧头 10 字节。序号必须逐帧加一。
 * 私钥生成时即标为不可导出，连接结束随对象回收。
 */

export const LIVE_SUITE = "ECDH-P256/HKDF-SHA256/AES-256-GCM";
const INFO_PREFIX = "aegis/online-live/v1";
const VERSION = 1;
const HEADER_LEN = 10;
const TAG_LEN = 16;
const C2S = new Uint8Array([0x43, 0x32, 0x53, 0x00]); // "C2S\0"
const S2C = new Uint8Array([0x53, 0x32, 0x43, 0x00]); // "S2C\0"

export type LiveHello = {
  t: "hello";
  v: number;
  suite: string;
  sid: string;
  key: string;
  salt: string;
};

export class LiveCryptoError extends Error {}

/** WebCrypto 只在安全上下文（HTTPS 或 localhost）可用。 */
export function liveCryptoAvailable() {
  return typeof window !== "undefined" && window.isSecureContext && Boolean(globalThis.crypto?.subtle);
}

function fromBase64(value: string): Uint8Array<ArrayBuffer> {
  const binary = atob(value);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

function toBase64(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}

function toHex(buffer: ArrayBuffer): string {
  return Array.from(new Uint8Array(buffer), (b) => b.toString(16).padStart(2, "0")).join("");
}

function nonceOf(prefix: Uint8Array, header: Uint8Array): Uint8Array<ArrayBuffer> {
  const nonce = new Uint8Array(12);
  nonce.set(prefix, 0);
  nonce.set(header.subarray(2, HEADER_LEN), 4);
  return nonce;
}

/**
 * 一条连接上的双向加密状态。
 *
 * WebCrypto 是异步的：两次 encrypt 的 Promise 不保证按调用顺序完成，
 * 而服务端要求序号严格递增。所以发送与接收各自串成一条 Promise 链，
 * 序号在入链时同步分配，帧按序号顺序进出。
 */
export class LiveChannel {
  private sendSeq = 0n;
  private recvSeq = 0n;
  private sendChain: Promise<unknown> = Promise.resolve();
  private recvChain: Promise<unknown> = Promise.resolve();

  private readonly sendKey: CryptoKey;
  private readonly recvKey: CryptoKey;

  constructor(sendKey: CryptoKey, recvKey: CryptoKey) {
    this.sendKey = sendKey;
    this.recvKey = recvKey;
  }

  /** 加密一帧；deliver 按序号顺序被调用。 */
  seal(plaintext: Uint8Array<ArrayBuffer>, deliver: (frame: ArrayBuffer) => void): Promise<void> {
    this.sendSeq += 1n;
    const header = new Uint8Array(HEADER_LEN);
    header[0] = VERSION;
    header[1] = 0;
    new DataView(header.buffer).setBigUint64(2, this.sendSeq, false);
    const job = this.sendChain.then(async () => {
      const sealed = await crypto.subtle.encrypt(
        { name: "AES-GCM", iv: nonceOf(C2S, header), additionalData: header, tagLength: TAG_LEN * 8 },
        this.sendKey,
        plaintext
      );
      const frame = new Uint8Array(HEADER_LEN + sealed.byteLength);
      frame.set(header, 0);
      frame.set(new Uint8Array(sealed), HEADER_LEN);
      deliver(frame.buffer);
    });
    this.sendChain = job.catch(() => undefined);
    return job;
  }

  /** 校验并解密一帧，按到达顺序串行处理；序号不连续或校验失败即抛错。 */
  open(frame: ArrayBuffer): Promise<Uint8Array> {
    const job = this.recvChain.then(async () => {
      const bytes = new Uint8Array(frame);
      if (bytes.length < HEADER_LEN + TAG_LEN || bytes[0] !== VERSION || bytes[1] !== 0) {
        throw new LiveCryptoError("加密帧格式错误");
      }
      const header = bytes.slice(0, HEADER_LEN);
      const seq = new DataView(header.buffer).getBigUint64(2, false);
      if (seq !== this.recvSeq + 1n) {
        throw new LiveCryptoError("加密帧序号不连续");
      }
      let plain: ArrayBuffer;
      try {
        plain = await crypto.subtle.decrypt(
          { name: "AES-GCM", iv: nonceOf(S2C, header), additionalData: header, tagLength: TAG_LEN * 8 },
          this.recvKey,
          bytes.slice(HEADER_LEN)
        );
      } catch {
        throw new LiveCryptoError("加密帧校验失败");
      }
      this.recvSeq = seq;
      return new Uint8Array(plain);
    });
    this.recvChain = job.catch(() => undefined);
    return job;
  }
}

/** 用服务端 hello 完成密钥交换，返回要发给服务端的公钥与加密通道。 */
export async function completeHandshake(hello: LiveHello, accessToken: string): Promise<{ clientKey: string; channel: LiveChannel }> {
  if (hello.v !== VERSION || hello.suite !== LIVE_SUITE) {
    throw new LiveCryptoError(`不支持的加密套件：${hello.suite}`);
  }
  const subtle = crypto.subtle;
  const pair = (await subtle.generateKey({ name: "ECDH", namedCurve: "P-256" }, false, ["deriveBits"])) as CryptoKeyPair;
  const serverKey = await subtle.importKey("raw", fromBase64(hello.key), { name: "ECDH", namedCurve: "P-256" }, false, []);
  const shared = await subtle.deriveBits({ name: "ECDH", public: serverKey }, pair.privateKey, 256);

  const tokenDigest = toHex(await subtle.digest("SHA-256", new TextEncoder().encode(accessToken)));
  const info = new TextEncoder().encode(`${INFO_PREFIX}|${hello.sid}|${tokenDigest}`);
  const ikm = await subtle.importKey("raw", shared, "HKDF", false, ["deriveBits"]);
  const okm = new Uint8Array(
    await subtle.deriveBits({ name: "HKDF", hash: "SHA-256", salt: fromBase64(hello.salt), info }, ikm, 512)
  );
  const sendKey = await subtle.importKey("raw", okm.slice(0, 32), "AES-GCM", false, ["encrypt"]);
  const recvKey = await subtle.importKey("raw", okm.slice(32, 64), "AES-GCM", false, ["decrypt"]);
  okm.fill(0);

  const publicRaw = new Uint8Array(await subtle.exportKey("raw", pair.publicKey));
  return { clientKey: toBase64(publicRaw), channel: new LiveChannel(sendKey, recvKey) };
}
