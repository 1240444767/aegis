"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  approveDeletionRequest,
  cancelDeletionRequest,
  createUserDeletion,
  executeDeletionRequest,
  getDeletionCatalog,
  getDeletionRequest,
  getDeletionSettings,
  getDeletionStats,
  getUserDeletion,
  listDeletionRequests,
  listDeletionTombstones,
  rejectDeletionRequest,
  releaseDeletionTombstone,
  rescheduleDeletionRequest,
  saveDeletionSettings,
  type DeletionAdminCreatePayload,
  type DeletionRequest,
  type DeletionRequestParams,
  type DeletionSettingsPayload
} from "@/lib/api/account-deletion";
import { useAdminToken } from "@/lib/admin-hooks";
import { browserTimeZone } from "@/lib/datetime";

/**
 * 账号注销域 hooks。
 *
 * 任何一次处置（审核、撤销、执行、改期、代发起、解除限制）都同时影响申请列表、单条详情、统计、
 * 用户详情里的注销卡片与重新注册限制；执行成功还会让用户列表少一个人。整组失效，不在组件里逐个点名。
 *
 * 刻意不失效 `admin-app-user`：立即注销成功后用户已经不存在，失效会让用户详情页先闪一下「未找到该用户」
 * 再跳走；用户详情页在拿到 completed 后自己跳回列表。
 */

const KEY = {
  catalog: "admin-account-deletion-catalog",
  settings: "admin-account-deletion-settings",
  stats: "admin-account-deletion-stats",
  requests: "admin-account-deletion-requests",
  request: "admin-account-deletion-request",
  tombstones: "admin-account-deletion-tombstones",
  user: "admin-account-deletion-user"
} as const;

const DELETION_SCOPE = [
  [KEY.settings],
  [KEY.stats],
  [KEY.requests],
  [KEY.request],
  [KEY.tombstones],
  [KEY.user],
  ["admin-app-users"],
  ["admin-app-stats"]
];

function useInvalidateDeletionScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(DELETION_SCOPE.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
  };
}

// ── 目录与配置 ──

export function useDeletionCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getDeletionCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 5 * 60_000
  });
}

export function useDeletionSettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getDeletionSettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 保存后先用返回值写回缓存，表单立即跟上；其余视图随整组失效刷新 */
export function useSaveDeletionSettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const invalidate = useInvalidateDeletionScope();
  return useMutation({
    mutationFn: (payload: DeletionSettingsPayload) => saveDeletionSettings(token as string, appKey as string, payload),
    onSuccess: async (settings) => {
      queryClient.setQueryData([KEY.settings, token, appKey], settings);
      await invalidate();
    }
  });
}

// ── 统计 ──

export function useDeletionStatsQuery(appKey?: string | null, days = 30) {
  const token = useAdminToken();
  const tz = browserTimeZone();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days, tz],
    queryFn: () => getDeletionStats(token as string, appKey as string, days, tz),
    enabled: Boolean(token && appKey)
  });
}

// ── 申请 ──

export function useDeletionRequestsQuery(appKey?: string | null, params?: DeletionRequestParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.requests,
      token,
      appKey,
      params?.status ?? "",
      params?.mode ?? "",
      params?.source ?? "",
      params?.userId ?? 0,
      params?.keyword ?? "",
      params?.from ?? "",
      params?.to ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listDeletionRequests(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

/** 单条详情；列表里已有的那一行作为占位，打开抽屉时不必等请求 */
export function useDeletionRequestQuery(appKey?: string | null, id?: number | null, placeholder?: DeletionRequest | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.request, token, appKey, id],
    queryFn: () => getDeletionRequest(token as string, appKey as string, id as number),
    enabled: Boolean(token && appKey && id),
    placeholderData: placeholder ?? undefined
  });
}

export type DeletionAction =
  | { kind: "approve"; id: number; note: string }
  | { kind: "reject"; id: number; note: string }
  | { kind: "cancel"; id: number; note: string; notify: boolean }
  | { kind: "execute"; id: number; confirm: string }
  | { kind: "reschedule"; id: number; scheduledAt: string; notify: boolean };

/** 审核、撤销、立即执行与改期共用一个 mutation，返回处置后的申请 */
export function useDeletionActionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateDeletionScope();
  return useMutation({
    mutationFn: (action: DeletionAction) => {
      const t = token as string;
      const app = appKey as string;
      switch (action.kind) {
        case "approve":
          return approveDeletionRequest(t, app, action.id, action.note);
        case "reject":
          return rejectDeletionRequest(t, app, action.id, action.note);
        case "cancel":
          return cancelDeletionRequest(t, app, action.id, action.note, action.notify);
        case "execute":
          return executeDeletionRequest(t, app, action.id, action.confirm);
        case "reschedule":
          return rescheduleDeletionRequest(t, app, action.id, action.scheduledAt, action.notify);
      }
    },
    onSuccess: invalidate
  });
}

// ── 重新注册限制 ──

export function useDeletionTombstonesQuery(
  appKey?: string | null,
  params?: { keyword?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.tombstones, token, appKey, params?.keyword ?? "", params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listDeletionTombstones(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useReleaseDeletionTombstoneMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateDeletionScope();
  return useMutation({
    mutationFn: (id: number) => releaseDeletionTombstone(token as string, appKey as string, id),
    onSuccess: invalidate
  });
}

// ── 单个用户 ──

export function useUserDeletionQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.user, token, appKey, userId],
    queryFn: () => getUserDeletion(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useCreateUserDeletionMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateDeletionScope();
  return useMutation({
    mutationFn: (payload: DeletionAdminCreatePayload) =>
      createUserDeletion(token as string, appKey as string, userId as number, payload),
    onSuccess: invalidate
  });
}
