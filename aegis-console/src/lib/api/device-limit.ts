import { apiRequest, buildQuery } from "@/lib/api/client";
import type { DeviceInfo } from "@/lib/api/device-info";

/**
 * 登录设备数控制 API。
 *
 * 一个账号同时能在几台设备上保持登录：总数、终端类别、会员档位、单人例外、新设备频率、同设备顶替
 * 多个维度叠加判定；超限自动下线、拒绝或让用户选择。判定全部在后端（`devicelimit.Plan`），
 * 控制台只配置、查看与模拟。规则见 docs/device-limit.md。
 */

export type DeviceClass = "mobile" | "desktop" | "web" | "other";
export type DeviceOverflow = "kick_oldest" | "kick_idle" | "reject" | "ask";
export type DeviceSameDevice = "replace" | "coexist";
export type DeviceTierAudience = "trial" | "vip" | "lifetime" | "feature";
export type DeviceLimitSource = "disabled" | "default" | "tier" | "override" | "exempt";
export type DeviceDimension = "total" | "class" | "new_device";
export type DeviceLimitAction =
  | "kicked"
  | "rejected"
  | "challenged"
  | "resolved"
  | "new_device"
  | "user_removed"
  | "admin_removed"
  | "admin_enforced";
export type DeviceDecisionAction = "allow" | "kick" | "reject" | "ask";

export type ClassLimits = Partial<Record<DeviceClass, number>>;

export type DeviceTier = {
  id: string;
  name: string;
  audience: DeviceTierAudience;
  featureTag?: string;
  maxDevices: number;
  /** 为空时沿用默认的类别上限 */
  classLimits?: ClassLimits | null;
};

export type DeviceLimitPolicy = {
  enabled: boolean;
  /** 0 表示不限总数 */
  maxDevices: number;
  classLimits: ClassLimits;
  overflow: DeviceOverflow;
  /** 处置方式为 ask、客户端不支持选择页时改用的方式 */
  askFallback: Exclude<DeviceOverflow, "ask">;
  sameDevice: DeviceSameDevice;
  tiers: DeviceTier[];
  newDevice: { enabled: boolean; windowDays: number; maxNew: number };
  notifyKicked: boolean;
  rejectMessage: string;
};

export type DeviceLimitPolicyView = {
  policy: DeviceLimitPolicy;
  /** false：还没用新策略保存过，当前值由旧的「同时在线上限」推导 */
  configured: boolean;
};

export type DeviceLimits = {
  unlimited: boolean;
  maxDevices: number;
  classLimits: ClassLimits | null;
  source: DeviceLimitSource;
  sourceLabel: string;
  tierId?: string;
  newDevice: { enabled: boolean; windowDays: number; maxNew: number };
};

/** 一台已登录的设备（同一台设备上的多次登录合成一台） */
export type LoginDevice = {
  key: string;
  deviceId?: string;
  platform?: string;
  class: DeviceClass;
  name?: string;
  deviceInfo?: DeviceInfo;
  ip?: string;
  userAgent?: string;
  provider?: string;
  sessions: number;
  loginAt: string;
  lastActiveAt: string;
  current: boolean;
};

export type DeviceUsage = { total: number; byClass: Partial<Record<DeviceClass, number>> };

export type DeviceLimitStatus = {
  enabled: boolean;
  limits: DeviceLimits;
  usage: DeviceUsage;
  devices: LoginDevice[];
  sameDevice: DeviceSameDevice;
  overflow: DeviceOverflow;
  newDevicesInWindow: number;
};

export type DeviceLedgerEntry = {
  key: string;
  platform?: string;
  class: DeviceClass;
  name?: string;
  deviceInfo?: DeviceInfo;
  firstSeenAt: string;
  lastSeenAt: string;
  lastIp?: string;
  loginCount: number;
  online: boolean;
};

export type DeviceLimitOverride = {
  userId: number;
  account?: string;
  exempt: boolean;
  maxDevices: number;
  classLimits: ClassLimits | null;
  note: string;
  expiresAt?: string | null;
  updatedBy?: string;
  createdAt: string;
  updatedAt: string;
};

export type DeviceLimitOverridePayload = {
  exempt: boolean;
  maxDevices: number;
  classLimits: ClassLimits;
  note: string;
  expiresAt: string | null;
};

export type DeviceRequirement = {
  dimension: DeviceDimension;
  class?: DeviceClass;
  limit: number;
  current: number;
  mustRemove: number;
};

export type DeviceLimitEvent = {
  id: number;
  userId: number;
  account?: string;
  action: DeviceLimitAction;
  dimension?: DeviceDimension | "";
  deviceKey?: string;
  deviceName?: string;
  platform?: string;
  class?: DeviceClass | "";
  triggerName?: string;
  ip?: string;
  limit?: number;
  detail?: { requirements?: DeviceRequirement[]; sourceLabel?: string; source?: string } | null;
  operator?: string;
  createdAt: string;
};

export type DeviceMember = { isVip: boolean; isLifetime: boolean; isTrial: boolean; features: string[] | null };

export type DeviceLimitUserView = {
  userId: number;
  account: string;
  nickname?: string;
  avatar?: string;
  member: DeviceMember;
  status: DeviceLimitStatus;
  override: DeviceLimitOverride | null;
  ledger: DeviceLedgerEntry[];
  events: DeviceLimitEvent[];
};

export type DeviceLimitOverview = {
  policy: DeviceLimitPolicy;
  configured: boolean;
  days: number;
  totals: Record<DeviceLimitAction, number>;
  trend: Array<{ date: string; counts: Record<DeviceLimitAction, number> }>;
  dimensions: Partial<Record<DeviceDimension, number>>;
  activeDevices: number;
  activeUsers: number;
  byClass: Record<DeviceClass, number>;
  /** 键 "1".."10"，10 表示 10 台及以上 */
  distribution: Record<string, number>;
  atLimitUsers: number;
  overrides: number;
  topUsers: Array<{ userId: number; account: string; count: number; lastAt: string }>;
};

export type DeviceLimitPage<T> = { items: T[]; total: number; page: number; limit: number };

export type DeviceLimitEventParams = {
  action?: DeviceLimitAction;
  dimension?: DeviceDimension;
  userId?: number;
  keyword?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type SimulateDevice = {
  key: string;
  name: string;
  platform: string;
  loginMinutesAgo: number;
  idleMinutesAgo: number;
};

export type DeviceLimitSimulateInput = {
  policy?: DeviceLimitPolicy | null;
  userId?: number;
  member?: { isVip: boolean; isLifetime: boolean; isTrial: boolean; features: string[] };
  override?: DeviceLimitOverridePayload | null;
  devices?: SimulateDevice[];
  platform: string;
  deviceKey?: string;
  known?: boolean;
  newDevicesInWindow?: number;
  canAsk?: boolean;
};

export type DeviceDecision = {
  action: DeviceDecisionAction;
  dimension?: DeviceDimension;
  requirements: DeviceRequirement[];
  replaced: LoginDevice[];
  victims: LoginDevice[];
  message?: string;
};

export type DeviceLimitSimulateResult = {
  limits: DeviceLimits;
  devices: LoginDevice[];
  decision: DeviceDecision;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/device-limit`;

export function getDeviceLimitOverview(token: string, appKey: string, days: number, tz: string) {
  return apiRequest<DeviceLimitOverview>(`${base(appKey)}${buildQuery({ days, tz })}`, { token });
}

export function getDeviceLimitPolicy(token: string, appKey: string) {
  return apiRequest<DeviceLimitPolicyView>(`${base(appKey)}/policy`, { token });
}

export function saveDeviceLimitPolicy(token: string, appKey: string, policy: DeviceLimitPolicy) {
  return apiRequest<DeviceLimitPolicyView>(`${base(appKey)}/policy`, {
    method: "PUT",
    token,
    body: JSON.stringify(policy)
  });
}

export function simulateDeviceLimit(token: string, appKey: string, input: DeviceLimitSimulateInput) {
  return apiRequest<DeviceLimitSimulateResult>(`${base(appKey)}/simulate`, {
    method: "POST",
    token,
    body: JSON.stringify(input)
  });
}

export function listDeviceLimitEvents(token: string, appKey: string, params?: DeviceLimitEventParams) {
  return apiRequest<DeviceLimitPage<DeviceLimitEvent>>(`${base(appKey)}/events${buildQuery(params ?? {})}`, { token });
}

export function listDeviceLimitOverrides(
  token: string,
  appKey: string,
  params?: { keyword?: string; page?: number; limit?: number }
) {
  return apiRequest<DeviceLimitPage<DeviceLimitOverride>>(`${base(appKey)}/overrides${buildQuery(params ?? {})}`, {
    token
  });
}

export function saveDeviceLimitOverride(
  token: string,
  appKey: string,
  userId: number,
  payload: DeviceLimitOverridePayload
) {
  return apiRequest<DeviceLimitOverride>(`${base(appKey)}/overrides/${userId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteDeviceLimitOverride(token: string, appKey: string, userId: number) {
  return apiRequest<{ userId: number }>(`${base(appKey)}/overrides/${userId}`, { method: "DELETE", token });
}

export function getDeviceLimitUser(token: string, appKey: string, userId: number) {
  return apiRequest<DeviceLimitUserView>(`${base(appKey)}/users/${userId}`, { token });
}

export function removeDeviceLimitDevices(token: string, appKey: string, userId: number, deviceKeys: string[]) {
  return apiRequest<{ removed: number }>(`${base(appKey)}/users/${userId}/remove`, {
    method: "POST",
    token,
    body: JSON.stringify({ deviceKeys })
  });
}

export function enforceDeviceLimit(token: string, appKey: string, userId: number) {
  return apiRequest<{ removed: number }>(`${base(appKey)}/users/${userId}/enforce`, { method: "POST", token });
}

export function resetDeviceLimitLedger(token: string, appKey: string, userId: number, key?: string) {
  return apiRequest<{ removed: number }>(`${base(appKey)}/users/${userId}/ledger${buildQuery({ key })}`, {
    method: "DELETE",
    token
  });
}
