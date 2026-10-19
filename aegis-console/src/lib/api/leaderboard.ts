import { apiRequest, buildQuery } from "@/lib/api/client";
import type { InviteReward, InviteRewardResult } from "@/lib/api/invite";
import type { PointsStorePage, PointsStoreRewardSpec } from "@/lib/api/points-store";

/**
 * 积分排行榜域 API。
 *
 * 总榜、日榜、周榜、月榜（另有经验榜、等级榜），周期榜到期自动结算成快照并按名次档位发奖励。
 * 榜单目录、权益目录、展示身份、奖励状态与上下限全部由 `GET settings` 下发，控制台不另抄一份；
 * 名次、口径、结算与发放都在后端。规则见 docs/leaderboard.md。
 */

export type LeaderboardPeriod = "total" | "day" | "week" | "month";
export type LeaderboardMetric = "points" | "experience" | "level";
export type LeaderboardIdentity = "nickname" | "masked";
export type LeaderboardRewardStatus = "pending" | "granted" | "failed" | "skipped" | "none";
export type LeaderboardCategoryRule = "counted" | "excluded" | "admin";

/** 一项权益，形状同邀请（同一套目录、同一套账本）；金额按字符串传输 */
export type LeaderboardReward = InviteReward;
export type LeaderboardRewardResult = InviteRewardResult;

/** 榜单目录的一项 */
export type LeaderboardBoardSpec = {
  key: string;
  label: string;
  metric: LeaderboardMetric;
  period: LeaderboardPeriod;
  valueUnit: string;
  /** 一句话说清取值口径 */
  description: string;
  defaultEnabled: boolean;
};

export type LeaderboardBoardSetting = { key: string; enabled: boolean };

/** 一档奖励：第 rankFrom 到第 rankTo 名（含两端）各发一份 */
export type LeaderboardTier = {
  rankFrom: number;
  rankTo: number;
  rewards: LeaderboardReward[];
};

export type LeaderboardBoardRewards = {
  enabled: boolean;
  tiers: LeaderboardTier[];
};

/** 应用的排行榜配置（除总开关以外的全部） */
export type LeaderboardConfig = {
  /** 顺序即客户端标签顺序 */
  boards: LeaderboardBoardSetting[];
  topLimit: number;
  minValue: number;
  identity: LeaderboardIdentity;
  allowAnonymous: boolean;
  countAdminAdjust: boolean;
  /** 只有周期榜能配；键为榜单 key */
  rewards: Record<string, LeaderboardBoardRewards>;
};

export type LeaderboardSettings = LeaderboardConfig & {
  appid: number;
  enabled: boolean;
  /** 是否保存过（没保存过时按关闭处理） */
  configured: boolean;
  updatedBy?: string;
  updatedAt?: string;
};

export type LeaderboardSettingsPayload = LeaderboardConfig & { enabled: boolean };

export type LeaderboardIntegralCategory = {
  category: string;
  label: string;
  rule: LeaderboardCategoryRule;
};

export type LeaderboardLimits = {
  minTopLimit: number;
  maxTopLimit: number;
  maxMinValue: number;
  maxTiers: number;
  maxRewardsInTier: number;
  maxReasonLength: number;
  maxExclusions: number;
};

export type LeaderboardSettingsData = {
  settings: LeaderboardSettings;
  catalog: LeaderboardBoardSpec[];
  /** 即邀请的权益目录 */
  rewardCatalog: PointsStoreRewardSpec[];
  identities: LeaderboardIdentity[];
  rewardStatuses: LeaderboardRewardStatus[];
  integralCategories: LeaderboardIntegralCategory[];
  defaults: LeaderboardConfig;
  limits: LeaderboardLimits;
};

/** 用户端看到的一项权益描述 */
export type LeaderboardClientReward = { type: string; label: string; detail: string };

export type LeaderboardClientTier = {
  rankFrom: number;
  rankTo: number;
  label: string;
  rewards: LeaderboardClientReward[];
};

/** 榜单此刻（或所选期号）的自述 */
export type LeaderboardClientBoard = {
  key: string;
  label: string;
  metric: LeaderboardMetric;
  period: LeaderboardPeriod;
  periodKey: string;
  periodLabel: string;
  periodStart?: string | null;
  periodEnd?: string | null;
  valueUnit: string;
  rewards: LeaderboardClientTier[];
};

/** 管理端榜单的一行：带真实账号，标出匿名与被移出 */
export type LeaderboardAdminEntry = {
  /** 被移出的人不参与排名，为 null */
  rank: number | null;
  userId: number;
  /** 真实账号；快照里已注销的为空串 */
  account: string;
  nickname: string;
  /** 按展示身份算的名字 */
  name: string;
  avatar: string;
  value: number;
  /** 用户的匿名偏好 */
  anonymous: boolean;
  excluded: boolean;
  levelName?: string;
  /** 周期榜最后一次获得积分的时刻 */
  lastAt?: string;
};

export type LeaderboardSnapshotStats = {
  entries: number;
  /** 应发 = pending + granted + failed + skipped */
  rewarded: number;
  pending: number;
  granted: number;
  failed: number;
  skipped: number;
};

export type LeaderboardSnapshot = {
  id: number;
  appid: number;
  board: string;
  periodKey: string;
  periodStart: string;
  periodEnd: string;
  status: "settling" | "settled" | string;
  participants: number;
  /** 结算时生效的档位 */
  tiers: LeaderboardTier[];
  /** system 或手动结算的管理员 */
  settledBy?: string;
  settledAt?: string;
  createdAt: string;
  stats?: LeaderboardSnapshotStats;
};

export type LeaderboardSnapshotEntry = {
  snapshotId: number;
  rank: number;
  position: number;
  userId: number;
  value: number;
  /** 结算那一刻冻结的展示名 */
  displayName: string;
  anonymous: boolean;
  levelName?: string;
  /** 按档位冻结的应发权益 */
  rewards: LeaderboardReward[];
  rewardStatus: LeaderboardRewardStatus;
  rewardResults: LeaderboardRewardResult[];
  rewardError?: string;
  grantedAt?: string;
  /** 当前账号；已注销的为空 */
  account?: string;
  avatar?: string;
};

export type LeaderboardBoardPage = {
  board: LeaderboardClientBoard;
  /** live 实时计算 / snapshot 取自结算快照 */
  source: "live" | "snapshot";
  snapshot?: LeaderboardSnapshot;
  /** 参与者人数（不含被移出的） */
  total: number;
  page: number;
  limit: number;
  totalPages: number;
  items: LeaderboardAdminEntry[];
};

export type LeaderboardExclusion = {
  appid: number;
  userId: number;
  account: string;
  nickname: string;
  reason: string;
  operator: string;
  createdAt: string;
};

export type LeaderboardBoardStats = {
  key: string;
  label: string;
  enabled: boolean;
  periodKey: string;
  periodLabel: string;
  participants: number;
  /** 本期前 3 */
  top: LeaderboardAdminEntry[];
  lastSettledAt?: string | null;
};

export type LeaderboardStats = {
  enabled: boolean;
  boards: LeaderboardBoardStats[];
  exclusions: number;
  pendingRewards: number;
  failedRewards: number;
};

export type LeaderboardPage<T> = PointsStorePage<T>;

export type LeaderboardBoardParams = { periodKey?: string; page?: number; limit?: number };
export type LeaderboardExclusionParams = { keyword?: string; page?: number; limit?: number };
export type LeaderboardSnapshotParams = { board?: string; page?: number; limit?: number };
export type LeaderboardEntryParams = { status?: LeaderboardRewardStatus; page?: number; limit?: number };

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/leaderboard`;

// ── 设置 ──

export function getLeaderboardSettings(token: string, appKey: string) {
  return apiRequest<LeaderboardSettingsData>(`${base(appKey)}/settings`, { token });
}

export function saveLeaderboardSettings(token: string, appKey: string, payload: LeaderboardSettingsPayload) {
  return apiRequest<LeaderboardSettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 榜单 ──

/** 不给期号看当前周期；给了已结算的期号取快照，未结算的按那个周期实时预览 */
export function getLeaderboardBoard(token: string, appKey: string, key: string, params?: LeaderboardBoardParams) {
  return apiRequest<LeaderboardBoardPage>(
    `${base(appKey)}/boards/${encodeURIComponent(key)}${buildQuery(params ?? {})}`,
    { token }
  );
}

export function getLeaderboardStats(token: string, appKey: string) {
  return apiRequest<LeaderboardStats>(`${base(appKey)}/stats`, { token });
}

// ── 移出名单 ──

export function listLeaderboardExclusions(token: string, appKey: string, params?: LeaderboardExclusionParams) {
  return apiRequest<LeaderboardPage<LeaderboardExclusion>>(
    `${base(appKey)}/exclusions${buildQuery(params ?? {})}`,
    { token }
  );
}

/** 一次 1 到 maxExclusions 人；不存在的用户 ID 在 missing 里返回 */
export function addLeaderboardExclusions(token: string, appKey: string, userIds: number[], reason: string) {
  return apiRequest<{ added: number; missing: number[] }>(`${base(appKey)}/exclusions`, {
    method: "POST",
    token,
    body: JSON.stringify({ userIds, reason })
  });
}

export function removeLeaderboardExclusion(token: string, appKey: string, userId: number) {
  return apiRequest<{ removed: boolean }>(`${base(appKey)}/exclusions/${userId}`, { method: "DELETE", token });
}

// ── 结算 ──

export function listLeaderboardSnapshots(token: string, appKey: string, params?: LeaderboardSnapshotParams) {
  return apiRequest<LeaderboardPage<LeaderboardSnapshot>>(
    `${base(appKey)}/snapshots${buildQuery(params ?? {})}`,
    { token }
  );
}

export function listLeaderboardSnapshotEntries(
  token: string,
  appKey: string,
  snapshotId: number,
  params?: LeaderboardEntryParams
) {
  return apiRequest<{ snapshot: LeaderboardSnapshot; entries: LeaderboardPage<LeaderboardSnapshotEntry> }>(
    `${base(appKey)}/snapshots/${snapshotId}/entries${buildQuery(params ?? {})}`,
    { token }
  );
}

/** 把该期发放失败的条目置回待发放，由结算循环发放 */
export function retryLeaderboardSnapshot(token: string, appKey: string, snapshotId: number) {
  return apiRequest<{ retried: number }>(`${base(appKey)}/snapshots/${snapshotId}/retry`, { method: "POST", token });
}

/** 手动结算一个已结束的周期；已有快照时直接返回它（created 为 false） */
export function settleLeaderboard(token: string, appKey: string, board: string, periodKey: string) {
  return apiRequest<{ snapshot: LeaderboardSnapshot; created: boolean }>(`${base(appKey)}/settle`, {
    method: "POST",
    token,
    body: JSON.stringify({ board, periodKey })
  });
}
