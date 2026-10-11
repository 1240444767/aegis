import { apiRequest, buildQuery } from "@/lib/api/client";
import type {
  PointsStorePage,
  PointsStoreReward,
  PointsStoreRewardResult,
  PointsStoreRewardSpec
} from "@/lib/api/points-store";

/**
 * 邀请域 API。
 *
 * 个人码（一人一个）与活动码（运营创建，可挂推广人）、邀请关系与双方奖励。配置表单与活动码表单由
 * 后端目录驱动（`/invite/catalog`）：权益、各枚举、默认配置与上限都从那里来。能否填写、奖励怎么发
 * 全部在后端判定，控制台只配置与展示。规则见 docs/invite.md。
 */

export type InviteRegisterField = "hidden" | "optional" | "required";
export type InviteCharset = "alnum" | "digits" | "letters";
export type InviteAudience = "all" | "vip";
export type InviteTrigger = "bind" | "signin";
export type InviteCodeKind = "personal" | "campaign";
export type InviteCodeStatus = "active" | "disabled";
export type InviteSource = "register" | "bind";
export type InviterStatus = "none" | "pending" | "granted" | "limited" | "cancelled";

/** 一项权益，形状与积分商城相同（同一套账本）；金额按字符串传输 */
export type InviteReward = PointsStoreReward;
export type InviteRewardResult = PointsStoreRewardResult;

export type InviteMilestone = {
  count: number;
  title?: string;
  rewards: InviteReward[];
};

/** 应用的邀请配置（除开关以外的全部） */
export type InviteConfig = {
  registerField: InviteRegisterField;
  fieldLabel: string;
  fieldPlaceholder: string;
  /** 注册后多少小时内可以补填，0 为只能注册时填 */
  bindWindowHours: number;
  codeLength: number;
  codeCharset: InviteCharset;
  codePrefix: string;
  allowCustomCode: boolean;
  inviterAudience: InviteAudience;
  inviterMinLevel: number;
  inviteeRewards: InviteReward[];
  inviterRewards: InviteReward[];
  milestones: InviteMilestone[];
  inviterTrigger: InviteTrigger;
  triggerSignInDays: number;
  inviterDailyLimit: number;
  inviterTotalLimit: number;
  notifyInviter: boolean;
  deviceLimit: number;
  ipDailyLimit: number;
  rulesText: string;
  shareTemplate: string;
  landingUrl: string;
};

export type InviteSettings = InviteConfig & {
  appid: number;
  enabled: boolean;
  /** 是否保存过（没保存过时按关闭处理） */
  configured: boolean;
  updatedBy?: string;
  updatedAt?: string;
};

export type InviteSettingsPayload = InviteConfig & { enabled: boolean };

export type InviteCatalogLimits = {
  maxRewards: number;
  maxMilestones: number;
  maxBindWindowHours: number;
  minCodeLength: number;
  minDigitsCodeLength: number;
  maxCodeLength: number;
  maxPrefixLength: number;
  minCustomCodeLength: number;
  maxCustomCodeLength: number;
  maxBatchCount: number;
  maxRulesLength: number;
  maxShareLength: number;
};

export type InviteCatalog = {
  rewards: PointsStoreRewardSpec[];
  registerFields: InviteRegisterField[];
  charsets: InviteCharset[];
  audiences: InviteAudience[];
  triggers: InviteTrigger[];
  defaults: InviteConfig;
  limits: InviteCatalogLimits;
};

export type InviteCode = {
  id: number;
  appid: number;
  code: string;
  kind: InviteCodeKind;
  /** 个人码的主人 / 活动码的推广人 */
  ownerUserId?: number;
  title?: string;
  note?: string;
  /** 活动码单独配置的被邀请人奖励；缺省为跟随应用默认，空数组为不发奖励 */
  rewards?: InviteReward[];
  /** 缺省为不限 */
  maxUses?: number;
  usedCount: number;
  startsAt?: string;
  endsAt?: string;
  status: InviteCodeStatus;
  custom?: boolean;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
  legacy?: boolean;
};

export type InviteAdminCode = InviteCode & {
  ownerAccount?: string;
  ownerNickname?: string;
};

export type InviteCodePayload = {
  /** 创建时：指定码（单个）；为空时按应用格式生成 count 个 */
  code?: string;
  count?: number;
  prefix?: string;
  ownerUserId: number | null;
  title: string;
  note: string;
  /** null 为跟随应用默认，[] 为不发奖励 */
  rewards: InviteReward[] | null;
  /** null 为不限 */
  maxUses: number | null;
  startsAt: string | null;
  endsAt: string | null;
  status: InviteCodeStatus;
};

export type InviteCodeParams = {
  kind?: InviteCodeKind;
  status?: InviteCodeStatus;
  ownerId?: number;
  keyword?: string;
  page?: number;
  limit?: number;
};

export type InviteRelation = {
  id: number;
  appid: number;
  inviteeId: number;
  inviterId?: number;
  codeId?: number;
  code: string;
  codeKind: InviteCodeKind;
  source: InviteSource;
  inviteeResults: InviteRewardResult[];
  inviterStatus: InviterStatus;
  inviterResults: InviteRewardResult[];
  inviterGrantedAt?: string;
  handledBy?: string;
  deviceId?: string;
  clientIp?: string;
  createdAt: string;
  updatedAt: string;
  inviteeAccount?: string;
  inviteeNickname?: string;
  inviteeAvatar?: string;
  inviterAccount?: string;
  inviterNickname?: string;
  inviterAvatar?: string;
};

export type InviteRelationParams = {
  inviterId?: number;
  inviteeId?: number;
  codeId?: number;
  status?: InviterStatus;
  source?: InviteSource;
  keyword?: string;
  page?: number;
  limit?: number;
};

export type InviteStats = {
  summary: {
    relations: number;
    today: number;
    granted: number;
    pending: number;
    limited: number;
    inviters: number;
    fromRegister: number;
    fromBind: number;
    personalCodes: number;
    campaignCodes: number;
  };
  trend: Array<{ date: string; relations: number; granted: number }>;
  topInviters: Array<{ userId: number; account: string; nickname?: string; invited: number; granted: number }>;
  topCodes: Array<{ codeId: number; code: string; title?: string; used: number }>;
};

export type InvitePage<T> = PointsStorePage<T>;

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/invite`;

// ── 目录与配置 ──

export function getInviteCatalog(token: string, appKey: string) {
  return apiRequest<InviteCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getInviteSettings(token: string, appKey: string) {
  return apiRequest<InviteSettings>(`${base(appKey)}/settings`, { token });
}

export function saveInviteSettings(token: string, appKey: string, payload: InviteSettingsPayload) {
  return apiRequest<InviteSettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 邀请码 ──

export function listInviteCodes(token: string, appKey: string, params?: InviteCodeParams) {
  return apiRequest<InvitePage<InviteAdminCode>>(`${base(appKey)}/codes${buildQuery(params ?? {})}`, { token });
}

export function createInviteCodes(token: string, appKey: string, payload: InviteCodePayload) {
  return apiRequest<{ items: InviteCode[] }>(`${base(appKey)}/codes`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 码本身、数量与前缀在更新时忽略 */
export function updateInviteCode(token: string, appKey: string, codeId: number, payload: InviteCodePayload) {
  return apiRequest<InviteCode>(`${base(appKey)}/codes/${codeId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function setInviteCodeStatus(token: string, appKey: string, codeId: number, active: boolean) {
  return apiRequest<InviteCode>(`${base(appKey)}/codes/${codeId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ active })
  });
}

/** 只能删除活动码；个人码只能停用 */
export function deleteInviteCode(token: string, appKey: string, codeId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/codes/${codeId}`, { method: "DELETE", token });
}

// ── 邀请记录与统计 ──

export function listInviteRelations(token: string, appKey: string, params?: InviteRelationParams) {
  return apiRequest<InvitePage<InviteRelation>>(`${base(appKey)}/relations${buildQuery(params ?? {})}`, { token });
}

/** 处理一条等待中的邀请人奖励：立即发放（不看条件与上限）或取消 */
export function handleInviteRelation(token: string, appKey: string, relationId: number, action: "settle" | "cancel") {
  return apiRequest<InviteRelation>(`${base(appKey)}/relations/${relationId}/${action}`, { method: "POST", token });
}

export function getInviteStats(token: string, appKey: string, days: number) {
  return apiRequest<InviteStats>(`${base(appKey)}/stats${buildQuery({ days })}`, { token });
}
