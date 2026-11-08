"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import * as risk from "@/lib/api/risk";
import { useAdminToken } from "@/lib/admin-hooks";
import type {
  RiskAccountQuery, RiskActionInput, RiskAssessmentQuery, RiskDisposeInput, RiskEntityQuery,
  RiskListCheckInput, RiskListCreateInput, RiskListQuery, RiskListUpdateInput, RiskReviewInput,
  RiskRuleInput, RiskSimulatePayload, RiskTemplateApplyInput,
} from "@/lib/api/types";

/**
 * 风控中心的 React Query hooks。
 *
 * 从 admin-hooks.ts 拆出来，与工单 / 组织域同一约定：一个域一个 hooks 文件。
 * 失效关系集中在这里定义 —— 「复核完之后大盘没刷新」这类问题的根源
 * 就是失效关系散落在各个组件里。
 */

/** 任何写操作之后都要连带失效的查询键。 */
const RISK_KEYS = {
  metadata: ["risk-metadata"] as const,
  dashboard: ["risk-dashboard"] as const,
  rules: ["risk-rules"] as const,
  rule: ["risk-rule"] as const,
  templates: ["risk-templates"] as const,
  assessments: ["risk-assessments"] as const,
  assessment: ["risk-assessment"] as const,
  reviews: ["risk-reviews"] as const,
  devices: ["risk-devices"] as const,
  device: ["risk-device"] as const,
  ips: ["risk-ips"] as const,
  ip: ["risk-ip"] as const,
  actions: ["risk-actions"] as const,
  lists: ["risk-lists"] as const,
  accounts: ["risk-accounts"] as const,
  account: ["risk-account"] as const,
};

/**
 * 「对象的名单状态变了」会同时影响的全部查询：名单本身、账号 / 设备 / IP 的画像
 * （各自带「现行名单」）、评估详情（带现行名单）、大盘（名单统计）。
 * 名单、处置、复核三类写操作都走这一组，避免各自漏掉其中几张表。
 */
const ENTITY_STATE_KEYS = [
  RISK_KEYS.lists, RISK_KEYS.accounts, RISK_KEYS.account, RISK_KEYS.devices, RISK_KEYS.device,
  RISK_KEYS.ips, RISK_KEYS.ip, RISK_KEYS.assessment, RISK_KEYS.dashboard,
] as const;

function useInvalidate() {
  const qc = useQueryClient();
  return (...keys: readonly (readonly string[])[]) => {
    keys.forEach((key) => qc.invalidateQueries({ queryKey: key }));
  };
}

// ── 自描述目录 ──

/**
 * 目录几乎不变，缓存 30 分钟。它驱动整页的枚举与条件参数表单，
 * 每次切页签都重拉一次纯属浪费。
 */
export function useRiskMetadataQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.metadata, token],
    queryFn: () => risk.getRiskMetadata(token as string),
    enabled: Boolean(token),
    staleTime: 30 * 60 * 1000,
  });
}

export function useValidateExpressionMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (expression: string) => risk.validateRiskExpression(token as string, expression) });
}

// ── 大盘 ──

export function useRiskDashboardQuery(start?: string, end?: string, appId?: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.dashboard, start, end, appId ?? null, token],
    queryFn: () => risk.getRiskDashboard(token as string, start as string, end as string, appId),
    enabled: Boolean(token && start && end),
    placeholderData: (previous) => previous,
  });
}

// ── 规则 ──

export function useRiskRulesQuery(scene?: string, appId?: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.rules, scene ?? "", appId ?? null, token],
    queryFn: () => risk.listRiskRules(token as string, scene, appId),
    enabled: Boolean(token),
  });
}

export function useRiskRuleQuery(id?: number, start?: string, end?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.rule, id, start, end, token],
    queryFn: () => risk.getRiskRule(token as string, id as number, start, end),
    enabled: Boolean(token && id),
  });
}

export function useCreateRiskRuleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (data: RiskRuleInput) => risk.createRiskRule(token as string, data),
    onSuccess: () => invalidate(RISK_KEYS.rules, RISK_KEYS.dashboard, RISK_KEYS.templates),
  });
}

export function useUpdateRiskRuleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; data: Partial<RiskRuleInput> }) =>
      risk.updateRiskRule(token as string, args.id, args.data),
    onSuccess: () => invalidate(RISK_KEYS.rules, RISK_KEYS.rule, RISK_KEYS.dashboard),
  });
}

export function useDeleteRiskRuleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => risk.deleteRiskRule(token as string, id),
    onSuccess: () => invalidate(RISK_KEYS.rules, RISK_KEYS.dashboard, RISK_KEYS.templates),
  });
}

// ── 规则模板 ──

export function useRiskTemplatesQuery(appId?: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.templates, appId ?? null, token],
    queryFn: () => risk.listRiskTemplates(token as string, appId),
    enabled: Boolean(token),
  });
}

export function useApplyRiskTemplateMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (data: RiskTemplateApplyInput) => risk.applyRiskTemplate(token as string, data),
    onSuccess: () => invalidate(RISK_KEYS.templates, RISK_KEYS.rules, RISK_KEYS.actions, RISK_KEYS.dashboard),
  });
}

// ── 模拟 / 重放 ──

export function useSimulateRiskMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (data: RiskSimulatePayload) => risk.simulateRisk(token as string, data) });
}

export function useSimulateRuleMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (args: { ruleId: number; data: RiskSimulatePayload }) =>
      risk.simulateRule(token as string, args.ruleId, args.data),
  });
}

export function useReplayAssessmentMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (id: number) => risk.replayAssessment(token as string, id) });
}

// ── 评估记录 ──

export function useRiskAssessmentsQuery(params?: RiskAssessmentQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.assessments, params, token],
    queryFn: () => risk.listAssessments(token as string, params),
    enabled: Boolean(token),
    // 排查中的运维会反复刷新这张表；保留上一页数据避免翻页时整屏闪空。
    placeholderData: (previous) => previous,
  });
}

export function useRiskAssessmentQuery(id?: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.assessment, id, token],
    queryFn: () => risk.getAssessment(token as string, id as number),
    enabled: Boolean(token && id),
  });
}

export function usePurgeAssessmentsMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (days: number) => risk.purgeAssessments(token as string, days),
    onSuccess: () => invalidate(RISK_KEYS.assessments, RISK_KEYS.reviews, RISK_KEYS.dashboard, RISK_KEYS.accounts),
  });
}

// ── 复核 ──

export function usePendingReviewsQuery(page = 1, pageSize = 20, appId?: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.reviews, page, pageSize, appId ?? null, token],
    queryFn: () => risk.listPendingReviews(token as string, { page, pageSize, appId }),
    enabled: Boolean(token),
    placeholderData: (previous) => previous,
  });
}

export function useReviewAssessmentMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number } & RiskReviewInput) => {
      const { id, ...data } = args;
      return risk.reviewAssessment(token as string, id, data);
    },
    // 复核会顺带加白 / 处置对象、撤销自动封禁：名单与全部实体画像一起失效
    onSuccess: () => invalidate(RISK_KEYS.reviews, RISK_KEYS.assessments, ...ENTITY_STATE_KEYS),
  });
}

// ── 人工处置 ──

export function useDisposeRiskMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (data: RiskDisposeInput) => risk.disposeRisk(token as string, data),
    onSuccess: () => invalidate(...ENTITY_STATE_KEYS),
  });
}

// ── 名单库 ──

export function useRiskListsQuery(params?: RiskListQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.lists, params, token],
    queryFn: () => risk.listRiskLists(token as string, params),
    enabled: Boolean(token),
    placeholderData: (previous) => previous,
  });
}

export function useCreateRiskListMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (data: RiskListCreateInput) => risk.createRiskListEntries(token as string, data),
    onSuccess: () => invalidate(...ENTITY_STATE_KEYS),
  });
}

export function useUpdateRiskListMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; data: RiskListUpdateInput }) =>
      risk.updateRiskListEntry(token as string, args.id, args.data),
    onSuccess: () => invalidate(...ENTITY_STATE_KEYS),
  });
}

export function useDeleteRiskListMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (ids: number[]) => ids.length === 1
      ? risk.deleteRiskListEntry(token as string, ids[0]).then(() => ({ removed: 1 }))
      : risk.batchDeleteRiskListEntries(token as string, ids),
    onSuccess: () => invalidate(...ENTITY_STATE_KEYS),
  });
}

export function useCheckRiskListsMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (data: RiskListCheckInput) => risk.checkRiskLists(token as string, data) });
}

// ── 账号画像 ──

export function useRiskAccountsQuery(params?: RiskAccountQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.accounts, params, token],
    queryFn: () => risk.listRiskAccounts(token as string, params),
    enabled: Boolean(token),
    placeholderData: (previous) => previous,
  });
}

export function useRiskAccountQuery(target?: { appId?: number | null; account?: string; userId?: number | null } | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.account, target?.appId ?? null, target?.account ?? "", target?.userId ?? null, token],
    queryFn: () => risk.getRiskAccountDetail(token as string, target ?? {}),
    enabled: Boolean(token && target && (target.account || target.userId)),
  });
}

// ── 设备指纹 ──

export function useRiskDevicesQuery(params?: RiskEntityQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.devices, params, token],
    queryFn: () => risk.listDevices(token as string, params),
    enabled: Boolean(token),
    placeholderData: (previous) => previous,
  });
}

export function useRiskDeviceQuery(deviceId?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.device, deviceId, token],
    queryFn: () => risk.getDeviceDetail(token as string, deviceId as string),
    enabled: Boolean(token && deviceId),
  });
}

export function useUpdateDeviceTagMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; tag: string; note?: string }) =>
      risk.updateDeviceTag(token as string, args.id, args.tag, args.note),
    // 封禁 / 受信标签与名单库联动
    onSuccess: () => invalidate(RISK_KEYS.devices, RISK_KEYS.device, RISK_KEYS.lists),
  });
}

// ── IP 风险库 ──

export function useRiskIPsQuery(params?: RiskEntityQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.ips, params, token],
    queryFn: () => risk.listIPs(token as string, params),
    enabled: Boolean(token),
    placeholderData: (previous) => previous,
  });
}

export function useRiskIPQuery(ip?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.ip, ip, token],
    queryFn: () => risk.getIPDetail(token as string, ip as string),
    enabled: Boolean(token && ip),
  });
}

export function useUpdateIPTagMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; tag: string; note?: string }) =>
      risk.updateIPTag(token as string, args.id, args.tag, args.note),
    onSuccess: () => invalidate(RISK_KEYS.ips, RISK_KEYS.ip, RISK_KEYS.lists),
  });
}

export function useRefreshIPReputationMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (ip: string) => risk.refreshIPReputation(token as string, ip),
    onSuccess: () => invalidate(RISK_KEYS.ips, RISK_KEYS.ip),
  });
}

// ── 处置策略 ──

export function useRiskActionsQuery(scene?: string, appId?: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.actions, scene ?? "", appId ?? null, token],
    queryFn: () => risk.listRiskActions(token as string, scene, appId),
    enabled: Boolean(token),
  });
}

export function useCreateRiskActionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (data: RiskActionInput) => risk.createRiskAction(token as string, data),
    onSuccess: () => invalidate(RISK_KEYS.actions, RISK_KEYS.dashboard),
  });
}

export function useUpdateRiskActionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; data: Partial<RiskActionInput> }) =>
      risk.updateRiskAction(token as string, args.id, args.data),
    onSuccess: () => invalidate(RISK_KEYS.actions, RISK_KEYS.dashboard),
  });
}

export function useDeleteRiskActionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => risk.deleteRiskAction(token as string, id),
    onSuccess: () => invalidate(RISK_KEYS.actions, RISK_KEYS.dashboard),
  });
}
