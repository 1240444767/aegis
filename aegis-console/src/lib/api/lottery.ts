import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 抽奖域 API（管理端）。
 *
 * 应用级抽奖活动：九宫格（恰好 8 个奖品）或转盘（2–12 个），每人每天的免费次数、
 * 抽奖次数余额（看激励视频、卡密、积分商城等发放）、积分抽奖，以及「连续 N 次未中必中」的保底。
 * 结果只在服务端产生；活动连同全部奖品一次保存（整张奖盘的约束分开保存会有违反约束的中间态）。
 * 奖品表单由后端目录驱动（`/lottery/catalog`）。规则见 docs/lottery.md。
 */

export type LotteryUIMode = "grid" | "wheel";
export type LotteryStatus = "draft" | "active" | "paused" | "ended";
export type LotterySource = "free" | "bonus" | "points";
export type LotteryDrawStatus = "granted" | "pending" | "fulfilled";
/** 抽中已拥有的奖品怎么办：折算积分 / 改发抽奖次数 / 不参与抽取 / 照常发放 */
export type LotteryLifetimePolicy = "points" | "draws" | "exclude" | "grant";
/** 奖品取值形态：空为不需要取值（谢谢参与） */
export type LotteryRewardValue = "" | "amount" | "ref" | "feature" | "text";

/** 一档奖品的自述，由后端目录下发，前端不另抄一份 */
export type LotteryRewardSpec = {
  type: string;
  label: string;
  hint: string;
  value: LotteryRewardValue;
  unit?: string;
  min?: number;
  max?: number;
};

export type LotteryPlanRef = {
  id: number;
  name: string;
  durationDays: number;
  lifetime: boolean;
  trial: boolean;
  features: string[];
};

export type LotteryFeatureRef = {
  tag: string;
  name: string;
  isActive: boolean;
};

/** 发「抽奖次数」的激励广告场景 */
export type LotteryVideoScene = {
  key: string;
  name: string;
  enabled: boolean;
  drawsPerView: number;
  dailyLimit: number;
};

export type LotteryCatalog = {
  kinds: LotteryRewardSpec[];
  uiModes: LotteryUIMode[];
  statuses: LotteryStatus[];
  sources: LotterySource[];
  gridPrizeCount: number;
  wheelMinPrizes: number;
  wheelMaxPrizes: number;
  /** 可选的会员套餐（已去掉试用套餐） */
  plans: LotteryPlanRef[];
  /** 可选的功能（只含启用中的） */
  features: LotteryFeatureRef[];
  videoScenes: LotteryVideoScene[];
  /** 激励广告总开关 */
  rewardedAds: boolean;
  /** 已拥有奖品的处理方式（文案由后端下发） */
  lifetimePolicies?: Array<{ key: LotteryLifetimePolicy; label: string; hint: string }>;
  /** 「已拥有时改发」可选的奖品类型 */
  swapKinds?: string[];
  lifetimeDefaults?: {
    policy: LotteryLifetimePolicy;
    pointsPerDay: number;
    pointsLifetime: number;
    draws: number;
  };
  /** 行为画像的信号目录 */
  signals?: LotterySignalSpec[];
  /** 自定义规则里能用的变量 */
  ruleVariables?: LotteryRuleVariable[];
  strategyLimits?: LotteryStrategyLimits;
};

// ── 出奖策略 ──

/** 信号映射：saturate 越多越高（边际递减）/ decay 越大越低 / bool / ratio（0–1 原值） */
export type LotterySignalMap = "saturate" | "decay" | "bool" | "ratio";

export type LotterySignalSpec = {
  key: string;
  label: string;
  group: string;
  unit?: string;
  map: LotterySignalMap;
  /** 负向信号（风险）：乘法惩罚 */
  negative?: boolean;
  /** 半饱和值：达到它时映射为 0.5 */
  half?: number;
  /** 推荐权重 */
  weight: number;
};

export type LotteryRuleVariable = { name: string; type: "number" | "bool" | "string"; label: string };

export type LotteryStrategyLimits = {
  rules: number;
  ruleFactor: number;
  softPityStep: number;
  cooldownDraws: number;
  pacingBoost: number;
  behaviorBoost: number;
  prizeLimit: number;
  simulationUsers: number;
  simulationEach: number;
  simulationDraws: number;
};

export type LotteryStrategyRule = {
  name: string;
  /** expr 表达式，空为总是生效 */
  when: string;
  kinds?: string[];
  positions?: number[];
  factor: number;
  disabled?: boolean;
};

export type LotteryBehaviorModel = {
  /** 空为全部信号的推荐权重 */
  signals?: Array<{ key: string; weight: number; half?: number }>;
  minFactor: number;
  maxBoost: number;
  kinds?: string[];
};

/** 多维度出奖策略：每一项都可以不设，全部不设时为纯加权随机 + 硬保底 */
export type LotteryStrategy = {
  softPity?: { start: number; step: number };
  pacing?: { slack: number; maxBoost: number };
  cooldown?: { draws: number; factor: number };
  budget?: { dailyPoints: number };
  risk?: { deviceAccounts: number; ipAccounts: number };
  behavior?: LotteryBehaviorModel;
  rules?: LotteryStrategyRule[];
};

/** 行为画像（全部行为的原始值） */
export type LotteryBehaviorProfile = {
  registeredDays: number;
  onlineMinutes7d: number;
  onlineMinutes30d: number;
  onlineDays30d: number;
  loginDays30d: number;
  recencyDays: number;
  signStreak: number;
  signDays30d: number;
  signTotalDays: number;
  signedToday: boolean;
  member: boolean;
  lifetime: boolean;
  memberDays: number;
  memberRate: number;
  paidAmount: number;
  paidOrders: number;
  experience: number;
  level: number;
  integral: number;
  pointsEarned30d: number;
  invites: number;
  adViews30d: number;
  redemptions: number;
  devices: number;
  riskScore30d: number;
};

export type LotteryBehaviorScore = {
  score: number;
  factor: number;
  signals: Array<{ key: string; value: number; score: number; weight: number }>;
};

/** 一个奖品在一次抽奖里的系数与最终概率（k 为维度：stock / dailyLimit / userLimit / userDailyLimit / risk / pacing / budget / behavior / cooldown / rule / softPity） */
export type LotteryPrizeOdds = {
  id: number;
  pos: number;
  base: number;
  w: number;
  /** 0–1 */
  p: number;
  f?: Array<{ k: string; v: number; n?: string }>;
  blocked?: string;
};

/** 一次抽奖的出奖方案（记录的 trace） */
export type LotteryDrawPlan = {
  odds: LotteryPrizeOdds[];
  softPity?: number;
  risk?: boolean;
  behavior?: LotteryBehaviorScore;
  winRate: number;
  pity?: boolean;
  picked?: number;
  fallback?: boolean;
};

export type LotteryDistribution = { mean: number; p50: number; p90: number; p99: number; max: number };

export type LotterySimulationResult = {
  users: number;
  draws: number;
  winRate: number;
  baseWinRate: number;
  pityHits: number;
  softPityDraws: number;
  firstWin: LotteryDistribution;
  neverWon: number;
  missStreak: LotteryDistribution;
  pointsIssued: number;
  prizes: Array<{
    prizeId: number;
    position: number;
    name: string;
    kind: string;
    wins: number;
    rate: number;
    baseRate: number;
    blocked: number;
  }>;
  behavior?: LotteryBehaviorScore;
};

export type LotterySimulatePayload = {
  activityId?: number;
  /** 还没保存的编辑内容 */
  activity?: LotteryActivityPayload;
  userId?: number;
  profile?: Partial<LotteryBehaviorProfile>;
  users: number;
  drawsPerUser: number;
  source?: string;
  isolatedStock?: boolean;
  seed?: number;
};

export type LotterySimulateResponse = {
  result: LotterySimulationResult;
  profile: LotteryBehaviorProfile;
  behavior?: LotteryBehaviorScore;
  /** 指定用户此刻再抽一次的方案（只对已保存的活动） */
  current?: LotteryDrawPlan;
};

export type LotteryReward = {
  type: string;
  /** 整数量；功能解锁时是天数，0 为永久 */
  amount?: number;
  refId?: number;
  tag?: string;
  text?: string;
};

export type LotteryRewardResult = {
  type: string;
  label: string;
  detail: string;
  transactionNo?: string;
  expireAt?: string;
  pending?: boolean;
};

export type LotteryPrize = {
  id: number;
  activityId: number;
  name: string;
  /** 与 reward.type 相同 */
  kind: string;
  reward: LotteryReward;
  imageUrl: string;
  /** 总库存，-1 为不限 */
  quantity: number;
  /** 已抽中次数（不限库存的奖品不维护） */
  used: number;
  /** 0 表示不参与常规抽取（只作保底奖或兜底奖） */
  weight: number;
  /** 在奖盘上的位置；九宫格为外圈顺时针 0–7，从左上角起 */
  position: number;
  fallback: boolean;
  /** 抽中的人已拥有这个奖品时改发什么；空为按活动的处理方式 */
  lifetimeReward?: LotteryReward | null;
  /** 全站每天最多出几个 / 每人最多中几次 / 每人每天最多中几次，0 为不限 */
  dailyLimit?: number;
  userLimit?: number;
  userDailyLimit?: number;
  createdAt: string;
  updatedAt: string;
};

export type LotteryActivity = {
  id: number;
  appid: number;
  name: string;
  description: string;
  rules: string;
  uiMode: LotteryUIMode;
  status: LotteryStatus;
  startTime?: string;
  endTime?: string;
  freeDaily: number;
  pointsCost: number;
  /** 积分抽奖每日上限，0 为不限 */
  pointsDaily: number;
  /** 激励广告场景，空为不开放视频抽奖 */
  videoScene: string;
  /** 0 为关闭 */
  pityThreshold: number;
  pityPrizeId?: number;
  lifetimePolicy?: LotteryLifetimePolicy;
  /** 折算积分时每天会员 / 功能折多少积分 */
  lifetimePointsPerDay?: number;
  /** 永久套餐 / 永久功能折多少积分 */
  lifetimePointsLifetime?: number;
  /** 改发抽奖次数时发几次 */
  lifetimeDraws?: number;
  strategy?: LotteryStrategy;
  sortOrder: number;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
};

/** 列表上的一行：活动 + 奖品 + 汇总 */
export type LotteryAdminActivity = LotteryActivity & {
  prizes: LotteryPrize[];
  totalDraws: number;
  totalUsers: number;
  todayDraws: number;
  pending: number;
};

export type LotteryPrizePayload = {
  /** 0 为新建；带 id 为更新（保留已抽中数量） */
  id: number;
  name: string;
  reward: LotteryReward;
  imageUrl: string;
  quantity: number;
  weight: number;
  position: number;
  fallback: boolean;
  /** 未设置时为 null */
  lifetimeReward: LotteryReward | null;
  dailyLimit: number;
  userLimit: number;
  userDailyLimit: number;
};

export type LotteryActivityPayload = {
  name: string;
  description: string;
  rules: string;
  uiMode: LotteryUIMode;
  status: LotteryStatus;
  startTime: string | null;
  endTime: string | null;
  freeDaily: number;
  pointsCost: number;
  pointsDaily: number;
  videoScene: string;
  pityThreshold: number;
  /** 保底奖在 prizes 里的 position；关闭保底时为 null */
  pityPosition: number | null;
  lifetimePolicy: LotteryLifetimePolicy;
  lifetimePointsPerDay: number;
  lifetimePointsLifetime: number;
  lifetimeDraws: number;
  strategy: LotteryStrategy;
  sortOrder: number;
  prizes: LotteryPrizePayload[];
};

export type LotteryDraw = {
  id: number;
  drawNo: string;
  appid: number;
  activityId: number;
  activityName?: string;
  userId: number;
  account?: string;
  nickname?: string;
  prizeId?: number;
  prizeName: string;
  prizeKind: string;
  reward: LotteryReward;
  position: number;
  source: LotterySource;
  pointsCost?: number;
  pity?: boolean;
  status: LotteryDrawStatus;
  results: LotteryRewardResult[];
  /** 抽中的奖品对这个人是已拥有的、改发了别的东西（prizeName / reward 仍是抽中的那一格） */
  conversion?: LotteryConversion;
  note?: string;
  handledBy?: string;
  handledAt?: string;
  requestId?: string;
  deviceId?: string;
  clientIp?: string;
  /** 出奖方案：各奖品的系数与最终概率、行为分 */
  trace?: LotteryDrawPlan;
  createdAt: string;
};

export type LotteryConversion = {
  /** lifetime 永久会员已包含 / feature 已永久解锁该功能 */
  reason: "lifetime" | "feature";
  reward: LotteryReward;
  label: string;
  message: string;
};

export type LotteryDrawParams = {
  activityId?: number;
  userId?: number;
  status?: LotteryDrawStatus;
  source?: LotterySource;
  /** 只看抽中的（排除谢谢参与） */
  winning?: boolean;
  keyword?: string;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type LotteryPage<T> = {
  items: T[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
};

export type LotteryStats = {
  activityId: number;
  days: number;
  summary: {
    todayDraws: number;
    todayUsers: number;
    todayWins: number;
    totalDraws: number;
    totalUsers: number;
    pointsSpent: number;
    pending: number;
    pityHits: number;
    /** 抽中已拥有的奖品、改发的次数 */
    converted?: number;
    /** 其中折算出去的积分 */
    convertedPoints?: number;
  };
  trend: Array<{ date: string; draws: number; wins: number; users: number }>;
  sources: Array<{ source: LotterySource; draws: number }>;
  prizes: Array<{ prizeId?: number; name: string; kind: string; draws: number; quantity: number; used: number; converted?: number }>;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/lottery`;

// ── 目录 ──

export function getLotteryCatalog(token: string, appKey: string) {
  return apiRequest<LotteryCatalog>(`${base(appKey)}/catalog`, { token });
}

// ── 活动 ──

export function listLotteryActivities(token: string, appKey: string) {
  return apiRequest<LotteryAdminActivity[]>(`${base(appKey)}/activities`, { token });
}

export function createLotteryActivity(token: string, appKey: string, payload: LotteryActivityPayload) {
  return apiRequest<LotteryAdminActivity>(`${base(appKey)}/activities`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateLotteryActivity(
  token: string,
  appKey: string,
  activityId: number,
  payload: LotteryActivityPayload
) {
  return apiRequest<LotteryAdminActivity>(`${base(appKey)}/activities/${activityId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

/** 上线前后端按当前奖盘再校验一次 */
export function setLotteryActivityStatus(token: string, appKey: string, activityId: number, status: LotteryStatus) {
  return apiRequest<LotteryAdminActivity>(`${base(appKey)}/activities/${activityId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

/** 有抽奖记录的活动不能删除（40927），改为结束 */
export function deleteLotteryActivity(token: string, appKey: string, activityId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/activities/${activityId}`, { method: "DELETE", token });
}

export function getLotteryStats(token: string, appKey: string, activityId: number, days: number) {
  return apiRequest<LotteryStats>(`${base(appKey)}/activities/${activityId}/stats${buildQuery({ days })}`, { token });
}

// ── 抽奖记录 ──

export function listLotteryDraws(token: string, appKey: string, params?: LotteryDrawParams) {
  return apiRequest<LotteryPage<LotteryDraw>>(`${base(appKey)}/draws${buildQuery(params ?? {})}`, { token });
}

/** 人工发放的奖品标记已发放；note 写进给用户的站内信（重复标记 40926） */
export function fulfillLotteryDraw(token: string, appKey: string, drawId: number, note: string) {
  return apiRequest<LotteryDraw>(`${base(appKey)}/draws/${drawId}/fulfill`, {
    method: "POST",
    token,
    body: JSON.stringify({ note })
  });
}

// ── 策略模拟 ──

/** 虚拟用户连续抽奖的模拟（不写库、不发奖）；指定 userId 时附带此人的画像与下一次的出奖方案 */
export function simulateLottery(token: string, appKey: string, payload: LotterySimulatePayload) {
  return apiRequest<LotterySimulateResponse>(`${base(appKey)}/simulate`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}
