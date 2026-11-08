import { apiRequest, buildQuery } from "./client";
import type { PermissionGroup } from "./types";

/**
 * MCP 密钥（docs/mcp.md）：管理员给 MCP 服务端（AI 助手）签发的长期凭证。
 *
 * 一把密钥恒代表签发人，能做的事是「签发人此刻的权限 ∩ 密钥范围」。
 * 管理接口只接受控制台会话，后端会拒绝用 MCP 密钥自己调用。
 */

export type McpKeyStatus = "active" | "disabled" | "revoked" | "expired";

export type McpKey = {
  id: number;
  adminId: number;
  adminAccount?: string;
  name: string;
  description: string;
  keyPrefix: string;
  /** 权限点模式：`*` 为全部，`ticket:*` 为整组 */
  permissions: string[];
  /** 空数组为不限应用 */
  appIds: number[];
  readOnly: boolean;
  allowIps: string[];
  /** 0 为不限 */
  rateLimitPerMinute: number;
  status: McpKeyStatus;
  expiresAt?: string;
  lastUsedAt?: string;
  lastUsedIp: string;
  useCount: number;
  rotatedAt?: string;
  revokedAt?: string;
  createdAt: string;
  updatedAt: string;
};

/** 明文只在签发与轮换的响应里出现一次 */
export type CreatedMcpKey = McpKey & { secret: string };

export type McpKeyPayload = {
  name: string;
  description?: string;
  permissions: string[];
  appIds: number[];
  readOnly: boolean;
  allowIps: string[];
  rateLimitPerMinute: number;
  /** 修改时三者都不给保持原过期时间；签发时都不给为永不过期 */
  expiresAt?: string;
  expiresInDays?: number;
  neverExpires?: boolean;
};

export type McpKeyCatalog = {
  groups: PermissionGroup[];
  effective: { global: string[]; apps: Record<string, string[]>; all: boolean };
  maxPerAdmin: number;
  defaultRate: number;
  keyPrefix: string;
};

const BASE = "/api/admin/profile/mcp-keys";

export function listMcpKeys(token: string, scope?: "all") {
  return apiRequest<McpKey[]>(`${BASE}${buildQuery({ scope })}`, { token });
}

export function getMcpKeyCatalog(token: string) {
  return apiRequest<McpKeyCatalog>(`${BASE}/catalog`, { token });
}

export function createMcpKey(token: string, payload: McpKeyPayload) {
  return apiRequest<CreatedMcpKey>(BASE, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateMcpKey(token: string, id: number, payload: McpKeyPayload) {
  return apiRequest<McpKey>(`${BASE}/${id}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function rotateMcpKey(token: string, id: number) {
  return apiRequest<CreatedMcpKey>(`${BASE}/${id}/rotate`, { method: "POST", token });
}

export function setMcpKeyEnabled(token: string, id: number, enabled: boolean) {
  return apiRequest<McpKey>(`${BASE}/${id}/${enabled ? "enable" : "disable"}`, { method: "POST", token });
}

export function revokeMcpKey(token: string, id: number) {
  return apiRequest<{ revoked: boolean }>(`${BASE}/${id}`, { method: "DELETE", token });
}
