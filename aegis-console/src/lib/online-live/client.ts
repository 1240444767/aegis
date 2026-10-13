import { joinApiUrl } from "@/lib/api/client";
import { completeHandshake, liveCryptoAvailable, type LiveChannel, type LiveHello } from "./crypto";
import type { LiveEvent, LiveSnapshot, LiveState } from "./types";

/**
 * 实时在线统计的加密长连接（/api/admin/system/online/live）。
 *
 * 职责：建连 → 明文握手换钥 → 之后全部走加密二进制帧；断线按关闭码决定是否重连；
 * 请求带 id，响应以 re 回填后兑现对应的 Promise；快照与动态写进一个可订阅的状态。
 *
 * 一个在线会话页面一个实例，页面卸载即断开 —— 在线统计只在有人看时才推送，
 * 不像通知那条 /api/ws 需要常驻。
 */

const SOCKET_PATH = "/api/admin/system/online/live";
const FEED_LIMIT = 200;
const REQUEST_TIMEOUT_MS = 10_000;
const PING_INTERVAL_MS = 15_000;
const HANDSHAKE_TIMEOUT_MS = 10_000;

/** 不再重连的关闭码与对应提示，见 docs/online-live.md。 */
const FATAL_CLOSES: Record<number, string> = {
  4400: "加密握手失败",
  4401: "登录状态已失效，请重新登录",
  4403: "当前账号无权查看在线统计"
};
const ROTATE_CLOSE = 4408;

export class LiveRequestError extends Error {
  readonly code: number;
  constructor(code: number, message: string) {
    super(message);
    this.code = code;
  }
}

type Pending = {
  resolve: (frame: Record<string, unknown>) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};

type Subscription = { app: number; feed: boolean };

function toWebSocketUrl(path: string) {
  const url = new URL(path, window.location.href);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}

export class OnlineLiveClient {
  private state: LiveState = { status: "idle", feed: [], dropped: 0 };
  private listeners = new Set<() => void>();
  private frameListeners = new Set<(frame: Record<string, unknown>) => void>();
  private socket: WebSocket | null = null;
  private channel: LiveChannel | null = null;
  private token: string | null = null;
  private pending = new Map<string, Pending>();
  private nextId = 1;
  private attempt = 0;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private handshakeTimer: ReturnType<typeof setTimeout> | null = null;
  private subscription: Subscription = { app: 0, feed: true };
  private generation = 0;

  // ── 状态订阅（供 useSyncExternalStore） ──

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getState = () => this.state;

  /** 监听每一个解密后的帧（快照、动态等），返回取消函数。 */
  onFrame(listener: (frame: Record<string, unknown>) => void) {
    this.frameListeners.add(listener);
    return () => {
      this.frameListeners.delete(listener);
    };
  }

  private setState(patch: Partial<LiveState>) {
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((l) => l());
  }

  // ── 生命周期 ──

  connect(token: string) {
    if (this.token === token && this.socket) return;
    this.token = token;
    this.attempt = 0;
    this.open();
  }

  disconnect() {
    this.token = null;
    this.teardown();
    this.setState({ status: "idle", error: undefined });
  }

  /** 失败后手动重试（如重新登录后）。 */
  retry() {
    if (!this.token) return;
    this.attempt = 0;
    this.open();
  }

  private open() {
    this.teardown();
    if (!this.token) return;
    if (!liveCryptoAvailable()) {
      this.setState({ status: "failed", error: "当前页面不是安全上下文（HTTPS），无法建立加密连接" });
      return;
    }
    const generation = ++this.generation;
    this.setState({ status: this.state.ready ? "reconnecting" : "connecting" });
    let socket: WebSocket;
    try {
      socket = new WebSocket(toWebSocketUrl(joinApiUrl(SOCKET_PATH)), ["aegis", `aegis.jwt.${this.token}`]);
    } catch {
      this.scheduleRetry("无法创建连接");
      return;
    }
    socket.binaryType = "arraybuffer";
    this.socket = socket;
    this.handshakeTimer = setTimeout(() => {
      if (generation === this.generation && !this.channel) socket.close(4000, "handshake timeout");
    }, HANDSHAKE_TIMEOUT_MS);

    socket.onmessage = (event) => {
      if (generation !== this.generation) return;
      if (typeof event.data === "string") {
        void this.handleHello(event.data, socket, generation);
      } else {
        void this.handleFrame(event.data as ArrayBuffer, generation);
      }
    };
    socket.onclose = (event) => {
      if (generation !== this.generation) return;
      this.handleClose(event.code, event.reason);
    };
  }

  private async handleHello(raw: string, socket: WebSocket, generation: number) {
    if (this.channel) {
      // 握手之后服务端不会再发明文；收到即视为异常
      socket.close(4000, "unexpected plaintext");
      return;
    }
    try {
      const hello = JSON.parse(raw) as LiveHello;
      if (hello.t !== "hello") throw new Error("握手帧格式错误");
      const { clientKey, channel } = await completeHandshake(hello, this.token ?? "");
      if (generation !== this.generation) return;
      this.channel = channel;
      socket.send(JSON.stringify({ t: "key", key: clientKey }));
      this.sendSubscription();
      this.pingTimer = setInterval(() => void this.ping(), PING_INTERVAL_MS);
      void this.ping();
    } catch {
      this.setState({ status: "failed", error: "加密握手失败" });
      socket.close(4000, "handshake failed");
    }
  }

  private async handleFrame(data: ArrayBuffer, generation: number) {
    if (!this.channel) return;
    let frame: Record<string, unknown>;
    try {
      const plain = await this.channel.open(data);
      frame = JSON.parse(new TextDecoder().decode(plain)) as Record<string, unknown>;
    } catch {
      if (generation === this.generation) this.socket?.close(4000, "bad frame");
      return;
    }
    if (generation !== this.generation) return;
    this.dispatch(frame);
  }

  private dispatch(frame: Record<string, unknown>) {
    const type = frame.t as string;
    if (type === "ready") {
      if (this.handshakeTimer) clearTimeout(this.handshakeTimer);
      this.attempt = 0;
      this.setState({ status: "open", error: undefined, ready: frame as never, connectedAt: Date.now() });
    } else if (type === "snapshot") {
      this.setState({ snapshot: frame as unknown as LiveSnapshot });
    } else if (type === "feed") {
      const events = (frame.events as LiveEvent[] | null) ?? [];
      const merged = [...[...events].reverse(), ...this.state.feed].slice(0, FEED_LIMIT);
      this.setState({ feed: merged, dropped: Number(frame.dropped ?? 0) });
    }
    const re = frame.re as string | undefined;
    if (re && this.pending.has(re)) {
      const pending = this.pending.get(re)!;
      this.pending.delete(re);
      clearTimeout(pending.timer);
      if (type === "error") pending.reject(new LiveRequestError(Number(frame.code ?? 0), String(frame.message ?? "请求失败")));
      else pending.resolve(frame);
    }
    this.frameListeners.forEach((l) => l(frame));
  }

  private handleClose(code: number, reason: string) {
    const fatal = FATAL_CLOSES[code];
    this.teardownSocket();
    if (!this.token) return;
    if (fatal) {
      this.setState({ status: "failed", error: fatal });
      return;
    }
    if (code === ROTATE_CLOSE) {
      // 到达最长寿命，服务端要求换钥：立即重连，不计入失败次数
      this.open();
      return;
    }
    this.scheduleRetry(reason || "连接已断开");
  }

  private scheduleRetry(reason: string) {
    this.attempt += 1;
    const base = Math.min(30_000, 1000 * 1.6 ** (this.attempt - 1));
    const delay = base * (0.75 + Math.random() * 0.5);
    this.setState({ status: "reconnecting", error: reason });
    this.retryTimer = setTimeout(() => this.open(), delay);
  }

  private teardownSocket() {
    if (this.handshakeTimer) clearTimeout(this.handshakeTimer);
    if (this.pingTimer) clearInterval(this.pingTimer);
    this.handshakeTimer = null;
    this.pingTimer = null;
    this.channel = null;
    if (this.socket) {
      this.socket.onmessage = null;
      this.socket.onclose = null;
      this.socket = null;
    }
    this.pending.forEach((p) => {
      clearTimeout(p.timer);
      p.reject(new LiveRequestError(0, "连接已断开"));
    });
    this.pending.clear();
  }

  private teardown() {
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    const socket = this.socket;
    this.generation++;
    this.teardownSocket();
    if (socket && socket.readyState <= WebSocket.OPEN) socket.close(1000);
  }

  // ── 发送 ──

  private send(message: Record<string, unknown>) {
    const socket = this.socket;
    const channel = this.channel;
    if (!socket || !channel) return false;
    void channel
      .seal(new TextEncoder().encode(JSON.stringify(message)), (frame) => {
        if (socket.readyState === WebSocket.OPEN) socket.send(frame);
      })
      .catch(() => socket.close(4000, "encrypt failed"));
    return true;
  }

  /** 发请求并等响应；连接未就绪时立即失败，由调用方在 open 后重试。 */
  request<T>(message: Record<string, unknown>): Promise<T> {
    const id = String(this.nextId++);
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new LiveRequestError(0, "请求超时"));
      }, REQUEST_TIMEOUT_MS);
      this.pending.set(id, { resolve: resolve as Pending["resolve"], reject, timer });
      if (!this.send({ ...message, id })) {
        clearTimeout(timer);
        this.pending.delete(id);
        reject(new LiveRequestError(0, "连接未就绪"));
      }
    });
  }

  /** 设定关注范围（0 为概览）；重连后自动重发。 */
  setSubscription(next: Partial<Subscription>) {
    const merged = { ...this.subscription, ...next };
    if (merged.app === this.subscription.app && merged.feed === this.subscription.feed) return;
    const appChanged = merged.app !== this.subscription.app;
    this.subscription = merged;
    // 动态按关注范围过滤，换范围后旧的动态不再属于当前视图
    if (appChanged) this.setState({ feed: [], dropped: 0 });
    this.sendSubscription();
  }

  private sendSubscription() {
    this.send({ t: "sub", app: this.subscription.app, feed: this.subscription.feed });
  }

  private async ping() {
    const started = performance.now();
    try {
      await this.request({ t: "ping", ts: Date.now() });
      this.setState({ latency: Math.round(performance.now() - started) });
    } catch {
      // 连接问题由 onclose 处理
    }
  }
}
