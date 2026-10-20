import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * AI 中转网关域 API（docs/ai-gateway.md）。
 *
 * 应用的终端用户凭自己的 sk-ag- 密钥（或登录令牌）调 `/api/ai/v1`，按模型价格 × 倍率
 * 从免费额度、会员额度、赠送额度、积分、余额里依次扣费。这里是它的管理端：
 * 设置、模型与价格、调用记录与退款、用户密钥、统计、用户的赠送额度。
 *
 * 金额（点、元、倍率、单价）一律是十进制字符串（后端 shopspring/decimal），只在展示时格式化。
 */

export type AIGatewaySourceKey = "free_daily" | "vip_daily" | "vip_monthly" | "credit" | "integral" | "wallet";

export type AIGatewaySourceMeta = {
  key: AIGatewaySourceKey;
  label: string;
  hint: string;
  periodic: boolean;
  period?: "day" | "month";
  vipOnly?: boolean;
};

export type AIGatewayPriceKey =
  | "inputPrice"
  | "outputPrice"
  | "requestPrice"
  | "imagePrice"
  | "charPrice"
  | "minutePrice";

export type AIGatewayKindMeta = {
  key: string;
  label: string;
  endpoints: string[];
  prices: AIGatewayPriceKey[];
};

export type AIGatewayLevelMultiplier = { level: number; multiplier: string };

export type AIGatewaySettings = {
  enabled: boolean;
  allowSessionToken: boolean;
  audience: "all" | "vip";
  sources: AIGatewaySourceKey[];
  priceMultiplier: string;
  vipMultiplier: string;
  levelMultipliers: AIGatewayLevelMultiplier[];
  freeDaily: string;
  vipDaily: string;
  vipMonthly: string;
  walletRate: string;
  dailySpendLimit: string;
  rpm: number;
  maxConcurrent: number;
  maxKeysPerUser: number;
  defaultMaxTokens: number;
  timezone: string;
  updatedBy?: string;
  updatedAt?: string;
};

export type AIGatewayCatalog = {
  sources: AIGatewaySourceMeta[];
  kinds: AIGatewayKindMeta[];
  defaults: AIGatewaySettings;
  baseUrl: string;
  keyPrefix: string;
};

export type AIGatewayPrice = {
  inputPrice: string;
  outputPrice: string;
  requestPrice: string;
  imagePrice: string;
  imagePrices?: Record<string, string>;
  charPrice: string;
  minutePrice: string;
  free: boolean;
};

export type AIGatewayModel = AIGatewayPrice & {
  id: number;
  appid: number;
  model: string;
  upstreamModel: string;
  kind: string;
  displayName: string;
  description: string;
  configId: number;
  enabled: boolean;
  vipOnly: boolean;
  maxOutputTokens: number;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
};

export type AIGatewayModelPayload = {
  model: string;
  upstreamModel: string;
  kind: string;
  displayName: string;
  description: string;
  configId: number;
  enabled: boolean;
  vipOnly: boolean;
  free: boolean;
  inputPrice: string;
  outputPrice: string;
  requestPrice: string;
  imagePrice: string;
  imagePrices: Record<string, string>;
  charPrice: string;
  minutePrice: string;
  maxOutputTokens: number;
  sortOrder: number;
};

export type AIGatewayRequestStatus = "reserved" | "succeeded" | "aborted" | "failed" | "expired" | "refunded";

export type AIGatewayHold = { source: AIGatewaySourceKey; micro: number; units?: number; period?: string };
export type AIGatewayCharge = AIGatewayHold & { txnNo?: string };

export type AIGatewayRequest = {
  id: number;
  requestNo: string;
  appid: number;
  userId: number;
  account?: string;
  keyId?: number;
  keyHint?: string;
  authKind: "key" | "session";
  endpoint: string;
  model: string;
  upstreamModel?: string;
  configId?: number;
  provider?: string;
  stream: boolean;
  status: AIGatewayRequestStatus;
  price: AIGatewayPrice;
  factor: string;
  estimate: string;
  reserved: string;
  holds: AIGatewayHold[];
  clampMaxTokens?: number;
  inputTokens: number;
  outputTokens: number;
  images: number;
  imageSize?: string;
  imageQuality?: string;
  characters: number;
  audioMs: number;
  usageEstimated: boolean;
  cost: string;
  charged: string;
  shortfall: string;
  charges: AIGatewayCharge[];
  integralTxnNo?: string;
  walletTxnNo?: string;
  errorCode?: string;
  errorMessage?: string;
  clientIp?: string;
  userAgent?: string;
  latencyMs: number;
  refundReason?: string;
  refundedBy?: string;
  refundedAt?: string;
  createdAt: string;
  expiresAt: string;
  settledAt?: string;
};

export type AIGatewayPage<T> = { items: T[]; total: number; page: number; limit: number };

export type AIGatewayRequestParams = {
  userId?: number;
  keyId?: number;
  model?: string;
  status?: string;
  endpoint?: string;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type AIGatewayKey = {
  id: number;
  appid: number;
  userId: number;
  account?: string;
  name: string;
  hint: string;
  status: "active" | "disabled" | "revoked";
  models: string[];
  spendLimit: string;
  spent: string;
  expiresAt?: string;
  lastUsedAt?: string;
  lastUsedIp?: string;
  createdAt: string;
  updatedAt: string;
};

export type AIGatewayKeyParams = { userId?: number; status?: string; page?: number; limit?: number };

export type AIGatewayStats = {
  days: number;
  requests: number;
  succeeded: number;
  failed: number;
  users: number;
  inputTokens: number;
  outputTokens: number;
  images: number;
  charged: string;
  shortfall: string;
  refunded: string;
  bySource: Array<{ source: AIGatewaySourceKey; amount: string; units: number }>;
  byModel: Array<{ model: string; requests: number; inputTokens: number; outputTokens: number; charged: string }>;
  daily: Array<{ date: string; requests: number; failed: number; charged: string }>;
  reservedNow: number;
  creditBalance: string;
};

export type AIGatewayQuotaView = {
  source: AIGatewaySourceKey;
  label: string;
  period: string;
  limit: string;
  used: string;
  reserved: string;
  remaining: string;
  eligible: boolean;
};

export type AIGatewayOverview = {
  enabled: boolean;
  allowSessionToken: boolean;
  audience: "all" | "vip";
  eligible: boolean;
  sources: AIGatewaySourceMeta[];
  profile: { isVip: boolean; level: number };
  factor: string;
  quotas: AIGatewayQuotaView[];
  credit: string;
  creditFrozen: string;
  integral: number;
  wallet: string;
  walletRate: string;
  available: string;
  dailySpendLimit: string;
  todaySpent: string;
  totalSpent: string;
  totalRequests: number;
  rpm: number;
  maxConcurrent: number;
  maxKeysPerUser: number;
  baseUrl: string;
};

export type AIGatewayCreditEntry = {
  id: number;
  kind: "grant" | "consume" | "refund" | "adjust";
  amount: string;
  balanceAfter: string;
  refType?: string;
  refId?: string;
  remark?: string;
  operator?: string;
  createdAt: string;
};

export type AIGatewayCreditAdjustPayload = { amount: string; reason: string; requestId: string };

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/ai-gateway`;

export function getAIGatewayCatalog(token: string, appKey: string) {
  return apiRequest<AIGatewayCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getAIGatewaySettings(token: string, appKey: string) {
  return apiRequest<AIGatewaySettings>(`${base(appKey)}/settings`, { token });
}

export function saveAIGatewaySettings(token: string, appKey: string, payload: AIGatewaySettings) {
  return apiRequest<AIGatewaySettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function listAIGatewayModels(token: string, appKey: string) {
  return apiRequest<{ items: AIGatewayModel[] }>(`${base(appKey)}/models`, { token });
}

export function createAIGatewayModel(token: string, appKey: string, payload: AIGatewayModelPayload) {
  return apiRequest<AIGatewayModel>(`${base(appKey)}/models`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateAIGatewayModel(token: string, appKey: string, modelId: number, payload: AIGatewayModelPayload) {
  return apiRequest<AIGatewayModel>(`${base(appKey)}/models/${modelId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAIGatewayModel(token: string, appKey: string, modelId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/models/${modelId}`, { method: "DELETE", token });
}

export function listAIGatewayRequests(token: string, appKey: string, params: AIGatewayRequestParams) {
  return apiRequest<AIGatewayPage<AIGatewayRequest>>(`${base(appKey)}/requests${buildQuery(params)}`, { token });
}

export function getAIGatewayRequest(token: string, appKey: string, requestNo: string) {
  return apiRequest<AIGatewayRequest>(`${base(appKey)}/requests/${encodeURIComponent(requestNo)}`, { token });
}

export function refundAIGatewayRequest(token: string, appKey: string, requestNo: string, reason: string) {
  return apiRequest<AIGatewayRequest>(`${base(appKey)}/requests/${encodeURIComponent(requestNo)}/refund`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason })
  });
}

export function listAIGatewayKeys(token: string, appKey: string, params: AIGatewayKeyParams) {
  return apiRequest<AIGatewayPage<AIGatewayKey>>(`${base(appKey)}/keys${buildQuery(params)}`, { token });
}

export function setAIGatewayKeyStatus(token: string, appKey: string, keyId: number, status: "active" | "disabled") {
  return apiRequest<AIGatewayKey>(`${base(appKey)}/keys/${keyId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

export function getAIGatewayStats(token: string, appKey: string, days: number) {
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
  return apiRequest<AIGatewayStats>(`${base(appKey)}/stats${buildQuery({ days, tz })}`, { token });
}

export function getAIGatewayUserAccount(token: string, appKey: string, userId: number) {
  return apiRequest<AIGatewayOverview>(`${base(appKey)}/users/${userId}/account`, { token });
}

export function listAIGatewayUserCredits(token: string, appKey: string, userId: number, page: number, limit: number) {
  return apiRequest<AIGatewayPage<AIGatewayCreditEntry>>(
    `${base(appKey)}/users/${userId}/credits${buildQuery({ page, limit })}`,
    { token }
  );
}

export function adjustAIGatewayUserCredit(
  token: string,
  appKey: string,
  userId: number,
  payload: AIGatewayCreditAdjustPayload
) {
  return apiRequest<{ balance: string; replayed: boolean }>(`${base(appKey)}/users/${userId}/credits`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}
