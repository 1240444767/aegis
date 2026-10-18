import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 存储中心 API：存储源（平台级 / 应用级配置）、提供商目录、总览、存储池、运行事件、
 * 深度检测、CORS、读取转向、迁移。
 *
 * 存储源的增删改沿用 `storage-config` 那组 POST 接口（权限点也随之沿用），
 * 其余在 `/api/admin/platform/storage` 下。密钥字段出网已被抹除：
 * 编辑表单只拿到 `secrets_set`，提交时留空即不修改。
 */

// ── 提供商目录 ──

export type StorageFieldType = "text" | "secret" | "url" | "switch" | "select";

export type StorageConfigField = {
  key: string;
  label: string;
  type: StorageFieldType;
  required?: boolean;
  secret?: boolean;
  placeholder?: string;
  help?: string;
  default?: unknown;
  options?: Array<{ value: string; label: string }>;
  advanced?: boolean;
};

export type StorageCapabilities = {
  directUpload: boolean;
  delete: boolean;
  cors: boolean;
  bucketUsage: boolean;
};

export type StorageProviderMeta = {
  provider: string;
  name: string;
  description?: string;
  category: string;
  categoryName: string;
  icon?: string;
  brandColor?: string;
  docUrl?: string;
  featured?: boolean;
  globalOnly?: boolean;
  capabilities: StorageCapabilities;
  publicBaseUrlRequired?: boolean;
  baseUrlLabel?: string;
  baseUrlPlaceholder?: string;
  fields: StorageConfigField[];
  notes?: string[];
};

export function getStorageProviders(token: string) {
  return apiRequest<{ providers: StorageProviderMeta[] }>("/api/admin/app/storage-config/providers", { token });
}

// ── 存储源 ──

export type StorageScope = "global" | "app";

export type StorageSource = {
  id: number;
  scope: StorageScope;
  appid?: number | null;
  provider: string;
  config_name: string;
  access_mode: "public" | "private";
  enabled: boolean;
  is_default: boolean;
  proxy_download: boolean;
  base_url?: string;
  root_path?: string;
  description?: string;
  config_data: Record<string, unknown>;
  redirect_config_id?: number | null;
  secrets_set?: string[];
  created_at: string;
  updated_at: string;
};

export type StorageSourcePayload = {
  provider: string;
  config_name: string;
  access_mode: "public" | "private";
  enabled: boolean;
  is_default: boolean;
  proxy_download: boolean;
  base_url: string;
  root_path: string;
  description: string;
  config_data: Record<string, unknown>;
};

function configBase(scope: StorageScope) {
  return scope === "global" ? "/api/admin/platform/storage-config" : "/api/admin/app/storage-config";
}

export function listStorageSources(token: string, scope: StorageScope, appid?: number | null) {
  return apiRequest<StorageSource[]>(`${configBase(scope)}/list`, {
    method: "POST",
    token,
    body: JSON.stringify(scope === "app" ? { appid } : {})
  });
}

export function saveStorageSource(
  token: string,
  scope: StorageScope,
  payload: StorageSourcePayload & { config_id?: number; appid?: number | null }
) {
  const action = payload.config_id ? "update" : "create";
  return apiRequest<StorageSource>(`${configBase(scope)}/${action}`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteStorageSource(token: string, scope: StorageScope, configId: number, appid?: number | null) {
  return apiRequest<null>(`${configBase(scope)}/delete`, {
    method: "POST",
    token,
    body: JSON.stringify({ config_id: configId, appid: appid ?? undefined })
  });
}

export function testStorageSource(token: string, scope: StorageScope, configId: number, appid?: number | null) {
  return apiRequest<{ success: boolean; message: string; metadata?: Record<string, unknown> }>(`${configBase(scope)}/test`, {
    method: "POST",
    token,
    body: JSON.stringify({ config_id: configId, appid: appid ?? undefined })
  });
}

// ── 总览 ──

export type StorageMemberState =
  | "healthy"
  | "degraded"
  | "backoff"
  | "near_full"
  | "full"
  | "readonly"
  | "disabled";

export type StorageUsage = {
  usedBytes: number;
  objects: number;
  source: "index" | "cloudflare-analytics" | string;
  updatedAt: string;
};

export type StorageHealth = {
  consecutiveFailures: number;
  backoffUntil?: string;
  lastError?: string;
  lastLatencyMs: number;
  lastSuccessAt?: string;
  lastFailureAt?: string;
  lastProbeAt?: string;
  uploads: number;
  failures: number;
};

export type StorageConfigOverview = {
  config: StorageSource;
  usage?: StorageUsage;
  health?: StorageHealth;
  state: StorageMemberState;
  capabilities: StorageCapabilities;
  poolIds: number[];
  /** 所在池给它设的容量上限（多个池取最小），0 为不限 */
  capacityBytes: number;
};

export type StorageWriteTarget = {
  kind: "config" | "pool";
  configId?: number;
  poolId?: number;
  name: string;
  provider: string;
  scope: string;
};

export type StorageOverview = {
  configs: StorageConfigOverview[];
  pools: StoragePool[];
  migrations: StorageMigration[];
  writeTarget?: StorageWriteTarget;
  totals: {
    configs: number;
    healthy: number;
    unhealthy: number;
    usedBytes: number;
    objects: number;
    capacityBytes: number;
  };
  generatedAt: string;
};

export function getStorageOverview(token: string) {
  return apiRequest<StorageOverview>("/api/admin/platform/storage/overview", { token });
}

// ── 存储池 ──

export type StoragePoolStrategy = "priority" | "round_robin" | "weighted" | "least_used";
export type StorageMemberMode = "active" | "standby" | "readonly";

export type StoragePoolMember = {
  configId: number;
  priority: number;
  weight: number;
  capacityBytes: number;
  mode: StorageMemberMode;
  config?: { name: string; provider: string; enabled: boolean; accessMode: string };
  usage?: StorageUsage;
  health?: StorageHealth;
  state?: StorageMemberState;
};

export type StoragePool = {
  id: number;
  name: string;
  description: string;
  strategy: StoragePoolStrategy;
  enabled: boolean;
  isDefault: boolean;
  readFallback: boolean;
  maxAttempts: number;
  warnPercent: number;
  members: StoragePoolMember[];
  createdAt: string;
  updatedAt: string;
};

export type StoragePoolInput = {
  name: string;
  description: string;
  strategy: StoragePoolStrategy;
  enabled: boolean;
  isDefault: boolean;
  readFallback: boolean;
  maxAttempts: number;
  warnPercent: number;
  members: Array<Pick<StoragePoolMember, "configId" | "priority" | "weight" | "capacityBytes" | "mode">>;
};

export type StoragePoolPlan = {
  poolId: number;
  size: number;
  strategy: StoragePoolStrategy;
  target: number;
  steps: Array<{ configId: number; name: string; provider: string; eligible: boolean; reason?: string }>;
};

const CENTER = "/api/admin/platform/storage";

export function saveStoragePool(token: string, input: StoragePoolInput, poolId?: number) {
  return apiRequest<StoragePool>(poolId ? `${CENTER}/pools/${poolId}` : `${CENTER}/pools`, {
    method: poolId ? "PUT" : "POST",
    token,
    body: JSON.stringify(input)
  });
}

export function deleteStoragePool(token: string, poolId: number) {
  return apiRequest<null>(`${CENTER}/pools/${poolId}`, { method: "DELETE", token });
}

export function simulateStoragePool(token: string, poolId: number, size: number) {
  return apiRequest<StoragePoolPlan>(`${CENTER}/pools/${poolId}/simulate`, {
    method: "POST",
    token,
    body: JSON.stringify({ size })
  });
}

// ── 事件 ──

export type StorageEventLevel = "info" | "warn" | "error";

export type StorageEvent = {
  id: number;
  poolId?: number;
  configId?: number;
  level: StorageEventLevel;
  kind: string;
  message: string;
  detail?: Record<string, unknown>;
  createdAt: string;
  poolName?: string;
  configName?: string;
};

export type StorageEventQuery = {
  poolId?: number;
  configId?: number;
  level?: StorageEventLevel;
  limit?: number;
};

export function listStorageEvents(token: string, query: StorageEventQuery) {
  return apiRequest<StorageEvent[]>(`${CENTER}/events${buildQuery(query)}`, { token });
}

// ── 检测、CORS、转向 ──

export type StorageProbeResult = {
  configId: number;
  provider: string;
  ok: boolean;
  steps: Array<{ name: string; ok: boolean; latencyMs: number; message?: string; skipped?: boolean }>;
  metadata?: Record<string, unknown>;
  usage?: { bytes: number; objects: number; source: string; sampledAt?: string };
};

export function probeStorageSource(token: string, configId: number) {
  return apiRequest<StorageProbeResult>(`${CENTER}/configs/${configId}/probe`, { method: "POST", token });
}

export function resetStorageHealth(token: string, configId: number) {
  return apiRequest<null>(`${CENTER}/configs/${configId}/reset-health`, { method: "POST", token });
}

export type StorageCORSRule = {
  allowedOrigins: string[];
  allowedMethods: string[];
  allowedHeaders: string[];
  exposeHeaders: string[];
  maxAgeSeconds: number;
};

export function getStorageCORS(token: string, configId: number) {
  return apiRequest<StorageCORSRule[]>(`${CENTER}/configs/${configId}/cors`, { token });
}

export function setStorageCORS(token: string, configId: number, origins: string[]) {
  return apiRequest<StorageCORSRule[]>(`${CENTER}/configs/${configId}/cors`, {
    method: "PUT",
    token,
    body: JSON.stringify({ origins })
  });
}

export function clearStorageRedirect(token: string, configId: number) {
  return apiRequest<null>(`${CENTER}/configs/${configId}/redirect`, { method: "DELETE", token });
}

export function collectStorageUsage(token: string) {
  return apiRequest<null>(`${CENTER}/usage/collect`, { method: "POST", token });
}

// ── 迁移 ──

export type StorageMigrationStatus = "pending" | "running" | "paused" | "completed" | "failed" | "cancelled";

export type StorageMigration = {
  id: number;
  sourceConfigId: number;
  targetConfigId: number;
  status: StorageMigrationStatus;
  deleteSource: boolean;
  setRedirect: boolean;
  totalObjects: number;
  totalBytes: number;
  copiedObjects: number;
  copiedBytes: number;
  skippedObjects: number;
  failedObjects: number;
  lastObjectId: number;
  lastError?: string;
  createdBy?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  updatedAt: string;
  sourceName?: string;
  targetName?: string;
};

export type StorageMigrationInput = {
  sourceConfigId: number;
  targetConfigId: number;
  deleteSource: boolean;
  setRedirect: boolean;
};

export function listStorageMigrations(token: string) {
  return apiRequest<StorageMigration[]>(`${CENTER}/migrations`, { token });
}

export function createStorageMigration(token: string, input: StorageMigrationInput) {
  return apiRequest<StorageMigration>(`${CENTER}/migrations`, {
    method: "POST",
    token,
    body: JSON.stringify(input)
  });
}

export function controlStorageMigration(token: string, id: number, action: "pause" | "resume" | "cancel") {
  return apiRequest<StorageMigration>(`${CENTER}/migrations/${id}/${action}`, { method: "POST", token });
}

// ── Cloudflare 快速接入 ──
//
// 令牌放在请求体里，只在这几次请求里用；后端不落库。

export type CloudflareConnection = {
  tokenId: string;
  tokenKind: "user" | "account";
  status: string;
  expiresOn?: string;
  accounts: Array<{ id: string; name: string }>;
  accountsError?: string;
};

export type CloudflareBucket = {
  name: string;
  jurisdiction: "default" | "eu" | "fedramp";
  location?: string;
  storageClass?: string;
  createdAt?: string;
  configId?: number;
  configName?: string;
};

export type CloudflareZone = { id: string; name: string; status: string };

export type CloudflareResources = {
  buckets: CloudflareBucket[];
  zones: CloudflareZone[];
  zonesError?: string;
};

export type CloudflareBucketDetail = {
  managedDomain?: string;
  managedDomainEnabled: boolean;
  customDomains: Array<{ domain: string; enabled: boolean; zoneName?: string; ownership?: string; ssl?: string }>;
  corsOrigins: string[];
};

export type CloudflareSetupInput = {
  token: string;
  accountId: string;
  bucket: string;
  jurisdiction: string;
  credentialMode: "create" | "reuse";
  publicAccess: "private" | "r2dev" | "custom";
  customDomain?: string;
  zoneId?: string;
  corsOrigins: string[];
  configId?: number;
  configName: string;
  description?: string;
  setDefault: boolean;
  poolId?: number;
  poolMode?: StorageMemberMode;
};

export type CloudflareSetupResult = {
  steps: Array<{ name: string; status: "ok" | "failed" | "skipped"; message?: string }>;
  config?: StorageSource;
  probe?: StorageProbeResult;
};

const CF = `${CENTER}/cloudflare`;

export function connectCloudflare(token: string, cfToken: string) {
  return apiRequest<CloudflareConnection>(`${CF}/connect`, { method: "POST", token, body: JSON.stringify({ token: cfToken }) });
}

export function getCloudflareResources(token: string, cfToken: string, accountId: string) {
  return apiRequest<CloudflareResources>(`${CF}/resources`, {
    method: "POST",
    token,
    body: JSON.stringify({ token: cfToken, accountId })
  });
}

export function createCloudflareBucket(
  token: string,
  input: { token: string; accountId: string; bucket: string; jurisdiction: string; locationHint?: string }
) {
  return apiRequest<CloudflareBucket>(`${CF}/buckets`, { method: "POST", token, body: JSON.stringify(input) });
}

export function getCloudflareBucketDetail(
  token: string,
  input: { token: string; accountId: string; bucket: string; jurisdiction: string }
) {
  return apiRequest<CloudflareBucketDetail>(`${CF}/bucket`, { method: "POST", token, body: JSON.stringify(input) });
}

export function setupCloudflareR2(token: string, input: CloudflareSetupInput) {
  return apiRequest<CloudflareSetupResult>(`${CF}/setup`, { method: "POST", token, body: JSON.stringify(input) });
}
