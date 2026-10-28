"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  cancelItemGrantRun,
  createItemGrantRule,
  deleteItemGrantRule,
  fulfillItemGrantRecord,
  getItemGrantCatalog,
  getItemGrantOverview,
  getItemGrantRule,
  getItemGrantRun,
  listItemGrantRecords,
  listItemGrantRules,
  listItemGrantRuns,
  previewItemGrant,
  retryItemGrantRecord,
  runItemGrantRule,
  setItemGrantRuleStatus,
  updateItemGrantRule,
  validateItemGrant,
  type ItemGrantGraph,
  type ItemGrantPreviewPayload,
  type ItemGrantRecordParams,
  type ItemGrantRulePayload,
  type ItemGrantRunParams,
  type ItemGrantStatus
} from "@/lib/api/item-grant";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 物品发放域 hooks。
 *
 * 失效关系集中在这里：改规则、改状态、执行、处理记录都会影响规则列表（统计与执行中）、
 * 执行记录、发放记录与总览，整组失效。目录里的套餐与功能归会员区块管，等目录自然过期。
 * 执行中的规则每 2 秒刷新一次列表与执行详情，跑完自动停。
 */

const KEY = {
  catalog: "admin-item-grant-catalog",
  overview: "admin-item-grant-overview",
  rules: "admin-item-grant-rules",
  rule: "admin-item-grant-rule",
  runs: "admin-item-grant-runs",
  run: "admin-item-grant-run",
  records: "admin-item-grant-records"
} as const;

const SCOPE = [KEY.overview, KEY.rules, KEY.rule, KEY.runs, KEY.run, KEY.records];

function useInvalidateItemGrantScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useItemGrantCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getItemGrantCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 60_000
  });
}

export function useItemGrantOverviewQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.overview, token, appKey, days],
    queryFn: () => getItemGrantOverview(token as string, appKey as string, days),
    enabled: Boolean(token && appKey)
  });
}

export function useItemGrantRulesQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.rules, token, appKey],
    queryFn: () => listItemGrantRules(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    refetchInterval: (query) => (query.state.data?.some((rule) => rule.running) ? 2000 : false)
  });
}

export function useItemGrantRuleQuery(appKey?: string | null, ruleId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.rule, token, appKey, ruleId],
    queryFn: () => getItemGrantRule(token as string, appKey as string, ruleId as number),
    enabled: Boolean(token && appKey && ruleId)
  });
}

export function useSaveItemGrantRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateItemGrantScope();
  return useMutation({
    mutationFn: (input: { ruleId?: number; payload: ItemGrantRulePayload }) =>
      input.ruleId
        ? updateItemGrantRule(token as string, appKey as string, input.ruleId, input.payload)
        : createItemGrantRule(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useSetItemGrantRuleStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateItemGrantScope();
  return useMutation({
    mutationFn: (input: { ruleId: number; status: ItemGrantStatus }) =>
      setItemGrantRuleStatus(token as string, appKey as string, input.ruleId, input.status),
    onSuccess: invalidate
  });
}

export function useDeleteItemGrantRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateItemGrantScope();
  return useMutation({
    mutationFn: (ruleId: number) => deleteItemGrantRule(token as string, appKey as string, ruleId),
    onSuccess: invalidate
  });
}

export function useRunItemGrantRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateItemGrantScope();
  return useMutation({
    mutationFn: (ruleId: number) => runItemGrantRule(token as string, appKey as string, ruleId),
    onSuccess: invalidate
  });
}

/** 校验与预演不改任何数据，不失效缓存 */
export function useValidateItemGrantMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (graph: ItemGrantGraph) => validateItemGrant(token as string, appKey as string, graph)
  });
}

export function usePreviewItemGrantMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: ItemGrantPreviewPayload) => previewItemGrant(token as string, appKey as string, payload)
  });
}

// ── 执行 ──

export function useItemGrantRunsQuery(appKey?: string | null, params?: ItemGrantRunParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.runs, token, appKey, params?.ruleId ?? 0, params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listItemGrantRuns(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    refetchInterval: (query) => (query.state.data?.items.some((run) => run.status === "running") ? 2000 : false)
  });
}

export function useItemGrantRunQuery(appKey?: string | null, runId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.run, token, appKey, runId],
    queryFn: () => getItemGrantRun(token as string, appKey as string, runId as number),
    enabled: Boolean(token && appKey && runId),
    refetchInterval: (query) => (query.state.data?.status === "running" ? 2000 : false)
  });
}

export function useCancelItemGrantRunMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateItemGrantScope();
  return useMutation({
    mutationFn: (runId: number) => cancelItemGrantRun(token as string, appKey as string, runId),
    onSuccess: invalidate
  });
}

// ── 记录 ──

export function useItemGrantRecordsQuery(appKey?: string | null, params?: ItemGrantRecordParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.records,
      token,
      appKey,
      params?.ruleId ?? 0,
      params?.runId ?? 0,
      params?.userId ?? 0,
      params?.status ?? "",
      params?.keyword ?? "",
      params?.start ?? "",
      params?.end ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listItemGrantRecords(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useFulfillItemGrantRecordMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateItemGrantScope();
  return useMutation({
    mutationFn: (input: { recordId: number; note: string }) =>
      fulfillItemGrantRecord(token as string, appKey as string, input.recordId, input.note),
    onSuccess: invalidate
  });
}

export function useRetryItemGrantRecordMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateItemGrantScope();
  return useMutation({
    mutationFn: (recordId: number) => retryItemGrantRecord(token as string, appKey as string, recordId),
    onSuccess: invalidate
  });
}
