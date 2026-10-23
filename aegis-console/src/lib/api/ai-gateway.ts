import { apiRequest, buildQuery, joinApiUrl } from "@/lib/api/client";

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
  redactContent: boolean;
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

/** 思考强度，与 OpenAI 的 reasoning_effort 同一组取值，由低到高。 */
export const AI_REASONING_EFFORTS = ["none", "minimal", "low", "medium", "high", "xhigh", "max"] as const;
export type AIReasoningEffort = (typeof AI_REASONING_EFFORTS)[number];

/** 对话模型的能力声明：客户端据此决定发不发图片、带不带工具、给不给思考强度的选项。 */
export type AIGatewayCapabilities = {
  vision: boolean;
  tools: boolean;
  reasoning: boolean;
  /** 空表示 low / medium / high；含 none 表示可以关闭思考。 */
  reasoningEfforts: AIReasoningEffort[];
  /** 上下文长度（token），0 表示未知。 */
  contextLength: number;
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
  capabilities?: AIGatewayCapabilities;
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
  capabilities?: AIGatewayCapabilities;
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
  /** 上游尝试次数；大于 1 表示发生过链路回退。 */
  attempts: number;
  firstTokenMs?: number;
  finishReason?: string;
  upstreamRequestId?: string;
  requestBytes?: number;
  responseBytes?: number;
  configName?: string;
  /** 只有单条查询带。 */
  trace?: AIGatewayTrace | null;
  /** 只在实时推送里有。 */
  live?: AIGatewayLiveProgress;
  refundReason?: string;
  refundedBy?: string;
  refundedAt?: string;
  createdAt: string;
  expiresAt: string;
  settledAt?: string;
};

export type AIGatewayAttemptStatus = "running" | "succeeded" | "failed" | "canceled";

/** 一次上游尝试（链路里的一条通道被调用了一次）。 */
export type AIGatewayAttempt = {
  seq: number;
  configId?: number;
  configName?: string;
  provider?: string;
  protocol?: string;
  inherited?: boolean;
  model?: string;
  url?: string;
  method?: string;
  status: AIGatewayAttemptStatus;
  httpStatus?: number;
  upstreamRequestId?: string;
  error?: string;
  retryable?: boolean;
  startedAt: string;
  headersMs?: number;
  firstTokenMs?: number;
  latencyMs: number;
  requestBytes?: number;
  responseBytes?: number;
  rateLimit?: Record<string, string>;
};

export type AIGatewayTrace = {
  attempts: AIGatewayAttempt[];
  request: Record<string, unknown>;
  response: Record<string, unknown>;
};

export type AIGatewayLiveProgress = {
  elapsedMs: number;
  firstTokenMs?: number;
  outputChars?: number;
  outputTokens?: number;
  attempt?: number;
};

export type AIGatewayUpstreamSummary = {
  configId: number;
  configName: string;
  provider: string;
  attempts: number;
  failed: number;
  avgLatencyMs: number;
  lastError?: string;
};

export type AIGatewayLiveSummary = {
  minutes: number;
  inFlight: number;
  total: number;
  succeeded: number;
  failed: number;
  retried: number;
  users: number;
  inputTokens: number;
  outputTokens: number;
  charged: string;
  avgLatencyMs: number;
  p95LatencyMs: number;
  avgFirstTokenMs: number;
  upstreams: AIGatewayUpstreamSummary[];
};

export type AIGatewayLiveEventType = "created" | "attempt" | "progress" | "finished" | "resync";

/** 调用记录实时事件（SSE，见 docs/ai-gateway.md「实时」）。 */
export type AIGatewayLiveEvent = {
  type: AIGatewayLiveEventType;
  appid: number;
  requestNo?: string;
  request?: AIGatewayRequest;
  attempts?: AIGatewayAttempt[];
  progress?: AIGatewayLiveProgress;
  at: string;
};

export type AIGatewayPage<T> = { items: T[]; total: number; page: number; limit: number };

export type AIGatewayRequestParams = {
  userId?: number;
  keyId?: number;
  model?: string;
  status?: string;
  endpoint?: string;
  /** 调用编号或上游请求编号（前缀）。 */
  keyword?: string;
  configId?: number;
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

export function getAIGatewayRequestSummary(token: string, appKey: string, minutes: number) {
  return apiRequest<AIGatewayLiveSummary>(`${base(appKey)}/requests/summary${buildQuery({ minutes })}`, { token });
}

/** SSE 连接的结局：服务端到点换连接（bye）、断开、或凭据失效（不再重连）。 */
export type AIGatewayLiveEnd = "bye" | "closed" | "unauthorized";

/**
 * 读调用记录的实时事件流。浏览器的 EventSource 带不了 Authorization 头，所以用 fetch 读流自己按 SSE 切分。
 * 连上后先 onOpen；流结束时兑现结局，网络错误抛出（由调用方退避重连）。
 */
export async function streamAIGatewayLive(
  token: string,
  appKey: string,
  handlers: { onOpen: () => void; onEvent: (event: AIGatewayLiveEvent) => void },
  signal: AbortSignal
): Promise<AIGatewayLiveEnd> {
  const response = await fetch(joinApiUrl(`${base(appKey)}/requests/live`), {
    headers: { Accept: "text/event-stream", Authorization: `Bearer ${token}`, "X-Admin-Token": token },
    cache: "no-store",
    signal
  });
  if (response.status === 401 || response.status === 403) return "unauthorized";
  if (!response.ok || !response.body) throw new Error(`实时连接失败（${response.status}）`);
  handlers.onOpen();
  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return "closed";
    buffer += value;
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const block = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      boundary = buffer.indexOf("\n\n");
      let name = "message";
      const data: string[] = [];
      for (const line of block.split("\n")) {
        if (line.startsWith("event:")) name = line.slice(6).trim();
        else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
      }
      if (name === "bye") {
        await reader.cancel().catch(() => undefined);
        return "bye";
      }
      if (name === "ready" || data.length === 0) continue;
      try {
        handlers.onEvent(JSON.parse(data.join("\n")) as AIGatewayLiveEvent);
      } catch {
        // 单条坏数据不影响后续事件。
      }
    }
  }
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
