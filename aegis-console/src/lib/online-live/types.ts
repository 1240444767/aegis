import type { OnlinePresenceConnection, OnlineUserItem, PagedResult } from "@/lib/api/types";

/** 与后端 docs/online-live.md 的帧结构一一对应。 */

export type LiveBucket = { key: string; count: number };

export type LiveDimensions = {
  platform: LiveBucket[];
  os: LiveBucket[];
  appVersion: LiveBucket[];
  country: LiveBucket[];
  region: LiveBucket[];
  device: LiveBucket[];
};

export type LiveDimensionKey = keyof LiveDimensions;

export type LiveAppRow = {
  appid: number;
  name?: string;
  appKey?: string;
  users: number;
  connections: number;
};

export type LiveSnapshot = {
  t: "snapshot";
  v: number;
  at: string;
  totals: { users: number; connections: number; apps: number };
  apps: LiveAppRow[];
  dims: LiveDimensions;
  focus?: LiveAppRow & { dims: LiveDimensions };
  nodes?: { serverId: string; connections: number; admins: number }[];
  admins?: { admins: number; connections: number };
  truncated?: boolean;
};

export type LiveReady = {
  t: "ready";
  serverTime: string;
  node: string;
  admin: { id: number; account: string; displayName: string; isSuperAdmin: boolean };
  scope: { all: boolean; apps: number[] };
  limits: { tickMs: number; retentionDays: number; lifetimeSec: number };
};

export type LiveEvent = {
  kind: "online" | "offline";
  appid: number;
  userId: number;
  account?: string;
  connectionId: string;
  ip?: string;
  location?: string;
  platform?: string;
  deviceName?: string;
  serverId?: string;
  at: string;
  duration?: number;
  admin?: boolean;
};

export type LiveRange = "1h" | "6h" | "24h" | "7d";

/** [时间(秒), 在线用户, 连接数, 在线管理员] */
export type LiveSeriesPoint = [number, number, number, number];

export type LiveSeries = {
  t: "series";
  app: number;
  range: LiveRange;
  step: number;
  from: number;
  to: number;
  points: LiveSeriesPoint[];
};

export type LiveUsers = {
  t: "users";
  q: string;
  list: PagedResult<OnlineUserItem> & { appid: number; refreshedAt: string };
};

export type LiveAdminPresence = {
  adminId: number;
  account?: string;
  displayName?: string;
  avatar?: string;
  isSuperAdmin?: boolean;
  connections: OnlinePresenceConnection[];
  connectedAt?: string;
  lastSeenAt: string;
};

export type LiveAdmins = { t: "admins"; items: LiveAdminPresence[] };

export type LiveStatus = "idle" | "connecting" | "open" | "reconnecting" | "failed";

export type LiveState = {
  status: LiveStatus;
  /** failed 时的原因；reconnecting 时是最近一次断开的原因 */
  error?: string;
  ready?: LiveReady;
  snapshot?: LiveSnapshot;
  /** 最近的上下线动态，新的在前 */
  feed: LiveEvent[];
  /** 最近一批里被服务端合并掉的事件数（瞬时大量上下线） */
  dropped: number;
  latency?: number;
  connectedAt?: number;
};
