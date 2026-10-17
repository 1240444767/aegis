import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 账号注销域 API。
 *
 * 配置（方式、审核、验证、原因、须知、前置条件、冷静期行为、重新注册限制）、申请的审核与处置、
 * 重新注册限制（墓碑）与统计。字段名与 Go 侧 `internal/domain/accountdeletion/types.go` 一致；
 * 状态迁移、前置条件与验证判定全部在后端，控制台只配置与展示。规则见 docs/account-deletion.md。
 *
 * 这些接口归「应用用户」权限点：读为 app:user:read，写为 app:user:write。
 */

export type DeletionMode = "immediate" | "cooling";
export type DeletionStatus =
  | "reviewing"
  | "scheduled"
  | "processing"
  | "completed"
  | "cancelled"
  | "rejected"
  | "failed";
export type DeletionSource = "user" | "admin";
export type DeletionVerifyMethod = "password" | "email" | "phone";
export type DeletionGuardLevel = "off" | "warn" | "block";
export type DeletionLoginBehavior = "keep" | "cancel";
export type DeletionGuardKey =
  | "activeBan"
  | "pendingOrders"
  | "pendingRefunds"
  | "walletBalance"
  | "activeVip"
  | "lifetimeVip"
  | "points";
export type DeletionTombstoneKind = "account" | "email" | "phone";

export type DeletionReason = { code: string; label: string };

export type DeletionGuards = Record<DeletionGuardKey, DeletionGuardLevel>;

/** 应用的注销配置（`account_deletion_settings.config`） */
export type DeletionConfig = {
  allowImmediate: boolean;
  allowCooling: boolean;
  defaultMode: DeletionMode;
  coolingDays: number;
  requireReview: boolean;
  verifyMethods: DeletionVerifyMethod[];
  requireTotp: boolean;
  recentLoginMinutes: number;
  reasons: DeletionReason[];
  reasonRequired: boolean;
  allowCustomReason: boolean;
  noticeTitle: string;
  noticeContent: string;
  agreementLabel: string;
  supportContact: string;
  guards: DeletionGuards;
  loginDuringCooling: DeletionLoginBehavior;
  signOutOthers: boolean;
  remindBeforeHours: number;
  reregisterDays: number;
  notifyEmail: boolean;
};

export type DeletionSettings = {
  appId: number;
  enabled: boolean;
  config: DeletionConfig;
  /** 是否保存过；没保存过时 config 是默认值 */
  configured: boolean;
  updatedBy?: string;
  updatedAt?: string;
};

export type DeletionSettingsPayload = { enabled: boolean; config: DeletionConfig };

export type DeletionCatalogLimits = {
  minCoolingDays: number;
  maxCoolingDays: number;
  maxRecentLoginMinutes: number;
  maxRemindBeforeHours: number;
  maxReregisterDays: number;
  maxReasons: number;
  maxNoticeRunes: number;
  maxReasonTextRunes: number;
};

export type DeletionCatalog = {
  defaults: DeletionConfig;
  modes: DeletionMode[];
  verifyMethods: DeletionVerifyMethod[];
  guardKeys: DeletionGuardKey[];
  guardLevels: DeletionGuardLevel[];
  loginBehaviors: DeletionLoginBehavior[];
  statuses: DeletionStatus[];
  limits: Partial<DeletionCatalogLimits>;
};

/** 提交时一项前置条件的结论 */
export type DeletionCheck = {
  key: DeletionGuardKey | string;
  title: string;
  level: DeletionGuardLevel;
  passed: boolean;
  message: string;
};

/** 提交时的账号快照（服务端 snapshot()；邮箱与手机号已打码） */
export type DeletionSnapshot = {
  registeredAt?: string;
  points?: number;
  wallet?: string;
  lifetime?: boolean;
  hasPassword?: boolean;
  totp?: boolean;
  checks?: DeletionCheck[];
  vipExpireAt?: string;
  email?: string;
  phone?: string;
};

export type DeletionRequest = {
  id: number;
  appId: number;
  userId: number;
  account: string;
  nickname: string;
  mode: DeletionMode;
  status: DeletionStatus;
  source: DeletionSource;
  reasonCode: string;
  reasonText: string;
  coolingDays: number;
  scheduledAt?: string;
  remindAt?: string;
  remindedAt?: string;
  reviewedAt?: string;
  reviewedBy: string;
  reviewNote: string;
  cancelledAt?: string;
  /** user / login / admin:<账号> */
  cancelledBy: string;
  cancelReason: string;
  completedAt?: string;
  failure: string;
  attempts: number;
  verifiedBy: string[] | null;
  operator: string;
  ip: string;
  deviceId: string;
  device: string;
  userAgent: string;
  snapshot: DeletionSnapshot | null;
  createdAt: string;
  updatedAt: string;
  /** 账号是否还在（已注销的为 false） */
  userExists: boolean;
};

export type DeletionPage<T> = {
  items: T[];
  total: number;
  page: number;
  limit: number;
};

export type DeletionRequestParams = {
  status?: DeletionStatus;
  mode?: DeletionMode;
  source?: DeletionSource;
  userId?: number;
  keyword?: string;
  /** RFC 3339 */
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type DeletionTombstone = {
  id: number;
  appId: number;
  requestId?: number;
  kind: DeletionTombstoneKind;
  hint: string;
  expiresAt: string;
  createdAt: string;
  /** 对应申请里的账号名（申请还在时） */
  account?: string;
};

export type DeletionCountItem = { key: string; label?: string; count: number };

export type DeletionTrendPoint = { date: string; requested: number; completed: number; cancelled: number };

export type DeletionStats = {
  total: number;
  byStatus: DeletionCountItem[];
  byMode: DeletionCountItem[];
  bySource: DeletionCountItem[];
  reasons: DeletionCountItem[];
  trend: DeletionTrendPoint[];
  dueIn7Days: number;
  tombstones: number;
  /** 已结束的申请里被撤销（含驳回）的比例，0–1 */
  retentionRate: number;
  days: number;
  timeZone: string;
};

/** 用户详情页上的注销状况 */
export type DeletionUserView = {
  open: DeletionRequest | null;
  history: DeletionRequest[] | null;
  enabled: boolean;
  config: DeletionConfig;
};

export type DeletionAdminCreatePayload = {
  mode: DeletionMode;
  /** 不传按应用配置 */
  coolingDays?: number;
  reason: string;
  notify: boolean;
  /** 立即注销时填账号名 */
  confirm?: string;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/account-deletion`;
const requestPath = (appKey: string, id: number) => `${base(appKey)}/requests/${id}`;

// ── 目录与配置 ──

export function getDeletionCatalog(token: string, appKey: string) {
  return apiRequest<DeletionCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getDeletionSettings(token: string, appKey: string) {
  return apiRequest<DeletionSettings>(`${base(appKey)}/settings`, { token });
}

export function saveDeletionSettings(token: string, appKey: string, payload: DeletionSettingsPayload) {
  return apiRequest<DeletionSettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 统计 ──

/** days 7–180；tz 为浏览器时区，趋势按它分日 */
export function getDeletionStats(token: string, appKey: string, days: number, tz: string) {
  return apiRequest<DeletionStats>(`${base(appKey)}/stats${buildQuery({ days, tz })}`, { token });
}

// ── 申请 ──

export function listDeletionRequests(token: string, appKey: string, params?: DeletionRequestParams) {
  return apiRequest<DeletionPage<DeletionRequest>>(`${base(appKey)}/requests${buildQuery(params ?? {})}`, {
    token
  });
}

export function getDeletionRequest(token: string, appKey: string, id: number) {
  return apiRequest<DeletionRequest>(requestPath(appKey, id), { token });
}

export function approveDeletionRequest(token: string, appKey: string, id: number, note: string) {
  return apiRequest<DeletionRequest>(`${requestPath(appKey, id)}/approve`, {
    method: "POST",
    token,
    body: JSON.stringify({ note })
  });
}

/** note 必填，用户可见 */
export function rejectDeletionRequest(token: string, appKey: string, id: number, note: string) {
  return apiRequest<DeletionRequest>(`${requestPath(appKey, id)}/reject`, {
    method: "POST",
    token,
    body: JSON.stringify({ note })
  });
}

export function cancelDeletionRequest(token: string, appKey: string, id: number, note: string, notify: boolean) {
  return apiRequest<DeletionRequest>(`${requestPath(appKey, id)}/cancel`, {
    method: "POST",
    token,
    body: JSON.stringify({ note, notify })
  });
}

/** confirm 填申请里的账号名；返回的 status 为 completed 或 failed */
export function executeDeletionRequest(token: string, appKey: string, id: number, confirm: string) {
  return apiRequest<DeletionRequest>(`${requestPath(appKey, id)}/execute`, {
    method: "POST",
    token,
    body: JSON.stringify({ confirm })
  });
}

/** scheduledAt 为 RFC 3339，晚于现在且不晚于一年 */
export function rescheduleDeletionRequest(
  token: string,
  appKey: string,
  id: number,
  scheduledAt: string,
  notify: boolean
) {
  return apiRequest<DeletionRequest>(`${requestPath(appKey, id)}/reschedule`, {
    method: "POST",
    token,
    body: JSON.stringify({ scheduledAt, notify })
  });
}

// ── 重新注册限制 ──

export function listDeletionTombstones(
  token: string,
  appKey: string,
  params?: { keyword?: string; page?: number; limit?: number }
) {
  return apiRequest<DeletionPage<DeletionTombstone>>(`${base(appKey)}/tombstones${buildQuery(params ?? {})}`, {
    token
  });
}

export function releaseDeletionTombstone(token: string, appKey: string, id: number) {
  return apiRequest<{ id: string }>(`${base(appKey)}/tombstones/${id}`, { method: "DELETE", token });
}

// ── 单个用户 ──

const userPath = (appKey: string, userId: number) =>
  `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}/account-deletion`;

export function getUserDeletion(token: string, appKey: string, userId: number) {
  return apiRequest<DeletionUserView>(userPath(appKey, userId), { token });
}

/** 不过验证与前置条件；立即注销返回的 status 为 completed 或 failed */
export function createUserDeletion(token: string, appKey: string, userId: number, payload: DeletionAdminCreatePayload) {
  return apiRequest<DeletionRequest>(userPath(appKey, userId), {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}
