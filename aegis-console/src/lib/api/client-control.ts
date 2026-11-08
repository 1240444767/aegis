import { apiRequest, buildQuery } from "./client";

/**
 * 版本管控：/api/admin/apps/{appKey}/client-control/*，权限 version:read / version:write。
 * 字段与后端 internal/domain/app/client_control.go 的 json tag 一一对应，契约见 docs/client-control.md。
 */

export type ClientControlAction = "allow" | "warn" | "force_update" | "block";

export type ClientControlSettings = {
  appId: number;
  enabled: boolean;
  policyVersion: number;
  allowedPackages: string[];
  allowedCerts: string[];
  verdictTtlSeconds: number;
  recheckSeconds: number;
  offlinePolicy: "allow" | "block";
  watchEnabled: boolean;
  serverGate: boolean;
  blockTitle: string;
  blockMessage: string;
  updatedAt: string;
  updatedBy: string;
};

export type ClientControlSettingsPatch = Partial<
  Pick<
    ClientControlSettings,
    | "enabled"
    | "allowedPackages"
    | "allowedCerts"
    | "verdictTtlSeconds"
    | "recheckSeconds"
    | "offlinePolicy"
    | "watchEnabled"
    | "serverGate"
    | "blockTitle"
    | "blockMessage"
  >
>;

export type ClientControlConditions = {
  packages?: string[];
  packagesNegate?: boolean;
  versionCodeMin?: number;
  versionCodeMax?: number;
  versionNames?: string[];
  certs?: string[];
  certsNegate?: boolean;
  platforms?: string[];
  deviceIds?: string[];
  userIds?: number[];
  installers?: string[];
  installersNegate?: boolean;
  osVersionMin?: number;
  osVersionMax?: number;
};

export type ClientControlRule = {
  id: number;
  appId: number;
  name: string;
  enabled: boolean;
  priority: number;
  action: ClientControlAction;
  conditions: ClientControlConditions;
  title: string;
  message: string;
  actionUrl: string;
  actionLabel: string;
  startsAt: string | null;
  endsAt: string | null;
  note: string;
  hitCount: number;
  lastHitAt: string | null;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
};

export type ClientControlRuleInput = {
  name: string;
  enabled?: boolean;
  priority: number;
  action: ClientControlAction;
  conditions: ClientControlConditions;
  title: string;
  message: string;
  actionUrl: string;
  actionLabel: string;
  startsAt: string | null;
  endsAt: string | null;
  note: string;
};

export type ClientControlTraceStep = {
  ruleId?: number;
  name: string;
  matched: boolean;
  detail: string;
};

export type ClientControlDecision = {
  action: ClientControlAction;
  reason: string;
  ruleId?: number;
  ruleName?: string;
  title?: string;
  message?: string;
  actionUrl?: string;
  actionLabel?: string;
  trace?: ClientControlTraceStep[];
};

export type ClientInstall = {
  deviceId: string;
  packageName: string;
  versionName: string;
  versionCode: number;
  platform: string;
  certSha256: string;
  installer: string;
  osVersion: number;
  model: string;
  brand: string;
  abi: string;
  userId?: number;
  ip: string;
  lastAction: ClientControlAction;
  lastReason: string;
  lastRuleId?: number;
  reportCount: number;
  firstSeenAt: string;
  lastSeenAt: string;
};

export type ClientInstallGroup = {
  packageName: string;
  versionName: string;
  versionCode: number;
  certSha256: string;
  platform: string;
  devices: number;
  reports: number;
  blocked: number;
  firstSeenAt: string;
  lastSeenAt: string;
  decision?: ClientControlDecision;
};

export type ClientInstallListResult = {
  items: ClientInstall[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type ClientControlKeyView = {
  keyId: string;
  kind: "sign" | "box";
  publicKey: string;
  status: "active" | "retired";
  endorsedBy?: string;
  endorsement?: string;
  createdAt: string;
  retiredAt?: string;
};

export type ClientControlOverview = {
  settings: ClientControlSettings;
  devices24h: number;
  devices7d: number;
  devices30d: number;
  blocked7d: number;
  packages: number;
  versions: number;
  rules: number;
  activeRules: number;
  signingKey?: ClientControlKeyView;
  boxKey?: ClientControlKeyView;
  actionBreakdown: Record<string, number>;
  daily: Array<{ day: string; devices: number; blocked: number }>;
};

export type ClientControlSimulateInput = {
  packageName: string;
  versionName: string;
  versionCode: number;
  platform: string;
  certSha256: string;
  installer: string;
  deviceId: string;
  osVersion: number;
  userId: number;
};

export type ClientInstallParams = {
  keyword?: string;
  packageName?: string;
  versionCode?: number;
  certSha256?: string;
  action?: string;
  days?: number;
  page?: number;
  limit?: number;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/client-control`;
const json = (method: string, token: string, body: unknown) => ({ method, token, body: JSON.stringify(body) });

export function getClientControlOverview(token: string, appKey: string) {
  return apiRequest<ClientControlOverview>(base(appKey), { token });
}

export function getClientControlSettings(token: string, appKey: string) {
  return apiRequest<ClientControlSettings>(`${base(appKey)}/settings`, { token });
}

export function updateClientControlSettings(token: string, appKey: string, patch: ClientControlSettingsPatch) {
  return apiRequest<ClientControlSettings>(`${base(appKey)}/settings`, json("PUT", token, patch));
}

export function refreshClientControl(token: string, appKey: string) {
  return apiRequest<{ policyVersion: number }>(`${base(appKey)}/refresh`, json("POST", token, {}));
}

export function getClientControlRules(token: string, appKey: string) {
  return apiRequest<ClientControlRule[]>(`${base(appKey)}/rules`, { token });
}

export function createClientControlRule(token: string, appKey: string, input: ClientControlRuleInput) {
  return apiRequest<ClientControlRule>(`${base(appKey)}/rules`, json("POST", token, input));
}

export function updateClientControlRule(token: string, appKey: string, ruleId: number, input: ClientControlRuleInput) {
  return apiRequest<ClientControlRule>(`${base(appKey)}/rules/${ruleId}`, json("PUT", token, input));
}

export function setClientControlRuleEnabled(token: string, appKey: string, ruleId: number, enabled: boolean) {
  return apiRequest<ClientControlRule>(`${base(appKey)}/rules/${ruleId}/${enabled ? "enable" : "disable"}`, json("POST", token, {}));
}

export function deleteClientControlRule(token: string, appKey: string, ruleId: number) {
  return apiRequest<{ id: number }>(`${base(appKey)}/rules/${ruleId}`, { method: "DELETE", token });
}

/** ruleIds 按匹配顺序排列，第一个最先匹配；必须包含全部规则。 */
export function reorderClientControlRules(token: string, appKey: string, ruleIds: number[]) {
  return apiRequest<ClientControlRule[]>(`${base(appKey)}/rules/reorder`, json("POST", token, { ruleIds }));
}

export function getClientControlVersions(token: string, appKey: string, days: number) {
  return apiRequest<ClientInstallGroup[]>(`${base(appKey)}/versions${buildQuery({ days })}`, { token });
}

export function getClientControlDevices(token: string, appKey: string, params: ClientInstallParams) {
  return apiRequest<ClientInstallListResult>(`${base(appKey)}/devices${buildQuery({ ...params })}`, { token });
}

export function deleteClientControlDevice(token: string, appKey: string, deviceId: string, packageName: string) {
  return apiRequest<{ deviceId: string }>(
    `${base(appKey)}/devices/${encodeURIComponent(deviceId)}${buildQuery({ packageName })}`,
    { method: "DELETE", token }
  );
}

export function getClientControlKeys(token: string, appKey: string) {
  return apiRequest<ClientControlKeyView[]>(`${base(appKey)}/keys`, { token });
}

export function rotateClientControlKey(token: string, appKey: string, kind: "sign" | "box") {
  return apiRequest<ClientControlKeyView>(`${base(appKey)}/keys/${kind}/rotate`, json("POST", token, {}));
}

export function simulateClientControl(token: string, appKey: string, input: ClientControlSimulateInput) {
  return apiRequest<ClientControlDecision>(`${base(appKey)}/simulate`, json("POST", token, input));
}
