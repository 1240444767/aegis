import { apiRequest, buildQuery } from "./client";
import type {
  RiskRule, RiskRuleDetail, RiskAssessment, RiskAssessmentDetail, RiskDeviceFingerprint,
  RiskDeviceDetail, IPRiskRecord, IPRiskDetail, RiskAction, RiskDashboard, RiskEvalResult,
  RiskMetadata, RiskExprValidation, RiskAssessmentQuery, RiskEntityQuery, RiskSimulatePayload,
  RiskRuleInput, RiskActionInput, RiskListEnvelope, RiskListEntry, RiskListQuery,
  RiskListCreateInput, RiskListUpdateInput, RiskListImportResult, RiskListCheckInput,
  RiskAccountProfile, RiskAccountQuery, RiskAccountDetail, RiskDisposeInput, RiskEnforcement,
  RiskReviewInput, RiskTemplateDetail, RiskTemplateApplyInput, RiskTemplateApplyResult,
} from "./types";

const P = "/api/admin/system/risk";

/**
 * 后端分页一律返回 `{ list, total }`（与平台其余管理端接口一致）。
 * 这一层负责把它转成前端惯用的 `{ items, total }` —— 重构前前端直接把响应
 * 当成 `{ items }` 用，于是评估记录 / 待复核 / 设备 / IP 四个页签
 * 无论后端有多少数据都永远显示空列表。
 */
async function paged<T>(promise: Promise<RiskListEnvelope<T>>): Promise<{ items: T[]; total: number }> {
  const data = await promise;
  return { items: data?.list ?? [], total: data?.total ?? 0 };
}

type QueryParams = Record<string, string | number | boolean | undefined | null>;

const json = (method: string, token: string, body?: unknown) => ({
  method, token, body: body === undefined ? undefined : JSON.stringify(body),
});

// ── 自描述目录 ──
export const getRiskMetadata = (token: string) =>
  apiRequest<RiskMetadata>(`${P}/metadata`, { token });

export const validateRiskExpression = (token: string, expression: string) =>
  apiRequest<RiskExprValidation>(`${P}/expression/validate`, json("POST", token, { expression }));

// ── 大盘 ──
export const getRiskDashboard = (token: string, start: string, end: string, appId?: number) =>
  apiRequest<RiskDashboard>(`${P}/dashboard${buildQuery({ start, end, appId })}`, { token });

// ── 规则 ──
export const listRiskRules = (token: string, scene?: string, appId?: number) =>
  apiRequest<RiskRule[] | null>(`${P}/rules${buildQuery({ scene, appId })}`, { token }).then((r) => r ?? []);

export const getRiskRule = (token: string, id: number, start?: string, end?: string) =>
  apiRequest<RiskRuleDetail>(`${P}/rules/${id}${buildQuery({ start, end })}`, { token });

export const createRiskRule = (token: string, data: RiskRuleInput) =>
  apiRequest<RiskRule>(`${P}/rules`, json("POST", token, data));

export const updateRiskRule = (token: string, id: number, data: Partial<RiskRuleInput>) =>
  apiRequest<void>(`${P}/rules/${id}`, json("PUT", token, data));

export const deleteRiskRule = (token: string, id: number) =>
  apiRequest<void>(`${P}/rules/${id}`, { method: "DELETE", token });

// ── 规则模板 ──
export const listRiskTemplates = (token: string, appId?: number) =>
  apiRequest<RiskTemplateDetail[] | null>(`${P}/templates${buildQuery({ appId })}`, { token }).then((r) => r ?? []);

export const applyRiskTemplate = (token: string, data: RiskTemplateApplyInput) =>
  apiRequest<RiskTemplateApplyResult>(`${P}/templates/apply`, json("POST", token, data));

// ── 模拟 / 评估 ──
export const simulateRisk = (token: string, data: RiskSimulatePayload) =>
  apiRequest<RiskEvalResult>(`${P}/simulate`, json("POST", token, data));

export const simulateRule = (token: string, id: number, data: RiskSimulatePayload) =>
  apiRequest<RiskEvalResult>(`${P}/rules/${id}/simulate`, json("POST", token, data));

export const replayAssessment = (token: string, id: number) =>
  apiRequest<RiskEvalResult>(`${P}/assessments/${id}/replay`, { method: "POST", token });

// ── 评估记录 ──
export const listAssessments = (token: string, params?: RiskAssessmentQuery) =>
  paged(apiRequest<RiskListEnvelope<RiskAssessment>>(`${P}/assessments${buildQuery(params as QueryParams)}`, { token }));

export const getAssessment = (token: string, id: number) =>
  apiRequest<RiskAssessmentDetail>(`${P}/assessments/${id}`, { token });

export const purgeAssessments = (token: string, days: number) =>
  apiRequest<{ removed: number; before: string }>(`${P}/assessments`, json("DELETE", token, { days }));

// ── 复核 ──
export const listPendingReviews = (token: string, params?: { page?: number; pageSize?: number; appId?: number }) =>
  paged(apiRequest<RiskListEnvelope<RiskAssessment>>(`${P}/reviews/pending${buildQuery(params as QueryParams)}`, { token }));

export const reviewAssessment = (token: string, id: number, data: RiskReviewInput) =>
  apiRequest<void>(`${P}/assessments/${id}/review`, json("POST", token, data));

// ── 处置 ──
export const disposeRisk = (token: string, data: RiskDisposeInput) =>
  apiRequest<{ enforcement: RiskEnforcement[] | null }>(`${P}/dispose`, json("POST", token, data));

// ── 名单库 ──
export const listRiskLists = (token: string, params?: RiskListQuery) =>
  paged(apiRequest<RiskListEnvelope<RiskListEntry>>(`${P}/lists${buildQuery(params as QueryParams)}`, { token }));

export const createRiskListEntries = (token: string, data: RiskListCreateInput) =>
  apiRequest<RiskListImportResult>(`${P}/lists`, json("POST", token, data));

export const updateRiskListEntry = (token: string, id: number, data: RiskListUpdateInput) =>
  apiRequest<RiskListEntry>(`${P}/lists/${id}`, json("PUT", token, data));

export const deleteRiskListEntry = (token: string, id: number) =>
  apiRequest<void>(`${P}/lists/${id}`, { method: "DELETE", token });

export const batchDeleteRiskListEntries = (token: string, ids: number[]) =>
  apiRequest<{ removed: number }>(`${P}/lists/batch-delete`, json("POST", token, { ids }));

export const checkRiskLists = (token: string, data: RiskListCheckInput) =>
  apiRequest<{ hits: RiskListEntry[] | null }>(`${P}/lists/check`, json("POST", token, data));

// ── 账号画像 ──
export const listRiskAccounts = (token: string, params?: RiskAccountQuery) =>
  paged(apiRequest<RiskListEnvelope<RiskAccountProfile>>(`${P}/accounts${buildQuery(params as QueryParams)}`, { token }));

export const getRiskAccountDetail = (token: string, params: { appId?: number | null; account?: string; userId?: number | null }) =>
  apiRequest<RiskAccountDetail>(`${P}/accounts/detail${buildQuery(params as QueryParams)}`, { token });

// ── 设备指纹 ──
export const listDevices = (token: string, params?: RiskEntityQuery) =>
  paged(apiRequest<RiskListEnvelope<RiskDeviceFingerprint>>(`${P}/devices${buildQuery(params as QueryParams)}`, { token }));

export const getDeviceDetail = (token: string, deviceId: string) =>
  apiRequest<RiskDeviceDetail>(`${P}/devices/${encodeURIComponent(deviceId)}`, { token });

// 后端 DTO 绑定的字段名是 `tag`（不是 `riskTag`）；写错会被 binding:"required" 挡下来
// 并返回 400 —— 重构前前端发的正是 `riskTag`，于是"更新标记"这个动作从来没成功过。
export const updateDeviceTag = (token: string, id: number, tag: string, note?: string) =>
  apiRequest<void>(`${P}/devices/${id}/tag`, json("PUT", token, { tag, note }));

// ── IP 风险库 ──
export const listIPs = (token: string, params?: RiskEntityQuery) =>
  paged(apiRequest<RiskListEnvelope<IPRiskRecord>>(`${P}/ips${buildQuery(params as QueryParams)}`, { token }));

export const getIPDetail = (token: string, ip: string) =>
  apiRequest<IPRiskDetail>(`${P}/ips/${encodeURIComponent(ip)}`, { token });

export const updateIPTag = (token: string, id: number, tag: string, note?: string) =>
  apiRequest<void>(`${P}/ips/${id}/tag`, json("PUT", token, { tag, note }));

export const refreshIPReputation = (token: string, ip: string) =>
  apiRequest<IPRiskRecord>(`${P}/ips/${encodeURIComponent(ip)}/refresh`, { method: "POST", token });

// ── 处置策略 ──
export const listRiskActions = (token: string, scene?: string, appId?: number) =>
  apiRequest<RiskAction[] | null>(`${P}/actions${buildQuery({ scene, appId })}`, { token }).then((r) => r ?? []);

export const createRiskAction = (token: string, data: RiskActionInput) =>
  apiRequest<RiskAction>(`${P}/actions`, json("POST", token, data));

export const updateRiskAction = (token: string, id: number, data: Partial<RiskActionInput>) =>
  apiRequest<void>(`${P}/actions/${id}`, json("PUT", token, data));

export const deleteRiskAction = (token: string, id: number) =>
  apiRequest<void>(`${P}/actions/${id}`, { method: "DELETE", token });
