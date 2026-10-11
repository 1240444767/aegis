"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createInviteCodes,
  deleteInviteCode,
  getInviteCatalog,
  getInviteSettings,
  getInviteStats,
  handleInviteRelation,
  listInviteCodes,
  listInviteRelations,
  saveInviteSettings,
  setInviteCodeStatus,
  updateInviteCode,
  type InviteCodeParams,
  type InviteCodePayload,
  type InviteRelationParams,
  type InviteSettingsPayload
} from "@/lib/api/invite";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 邀请域 hooks。
 *
 * 失效关系集中在这里：改配置影响概览与邀请码（个人码格式）；改码影响码列表与概览；
 * 处理一条邀请人奖励影响记录与概览。整组失效，不在组件里逐个点名。
 */

const KEY = {
  catalog: "admin-invite-catalog",
  settings: "admin-invite-settings",
  codes: "admin-invite-codes",
  relations: "admin-invite-relations",
  stats: "admin-invite-stats"
} as const;

const INVITE_SCOPE = [KEY.settings, KEY.codes, KEY.relations, KEY.stats];

function useInvalidateInviteScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(INVITE_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

// ── 目录与配置 ──

export function useInviteCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getInviteCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 5 * 60_000
  });
}

export function useInviteSettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getInviteSettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 保存后先用返回值写回缓存，表单立即跟上；其余视图随整组失效刷新 */
export function useSaveInviteSettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const invalidate = useInvalidateInviteScope();
  return useMutation({
    mutationFn: (payload: InviteSettingsPayload) => saveInviteSettings(token as string, appKey as string, payload),
    onSuccess: async (settings) => {
      queryClient.setQueryData([KEY.settings, token, appKey], settings);
      await invalidate();
    }
  });
}

// ── 邀请码 ──

export function useInviteCodesQuery(appKey?: string | null, params?: InviteCodeParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.codes,
      token,
      appKey,
      params?.kind ?? "",
      params?.status ?? "",
      params?.ownerId ?? 0,
      params?.keyword ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listInviteCodes(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveInviteCodeMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateInviteScope();
  return useMutation({
    mutationFn: async (input: { codeId?: number; payload: InviteCodePayload }) => {
      if (input.codeId) {
        const code = await updateInviteCode(token as string, appKey as string, input.codeId, input.payload);
        return [code];
      }
      const created = await createInviteCodes(token as string, appKey as string, input.payload);
      return created.items;
    },
    onSuccess: invalidate
  });
}

export function useSetInviteCodeStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateInviteScope();
  return useMutation({
    mutationFn: (input: { codeId: number; active: boolean }) =>
      setInviteCodeStatus(token as string, appKey as string, input.codeId, input.active),
    onSuccess: invalidate
  });
}

export function useDeleteInviteCodeMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateInviteScope();
  return useMutation({
    mutationFn: (codeId: number) => deleteInviteCode(token as string, appKey as string, codeId),
    onSuccess: invalidate
  });
}

// ── 邀请记录与统计 ──

export function useInviteRelationsQuery(appKey?: string | null, params?: InviteRelationParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.relations,
      token,
      appKey,
      params?.inviterId ?? 0,
      params?.inviteeId ?? 0,
      params?.codeId ?? 0,
      params?.status ?? "",
      params?.source ?? "",
      params?.keyword ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listInviteRelations(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useHandleInviteRelationMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateInviteScope();
  return useMutation({
    mutationFn: (input: { relationId: number; action: "settle" | "cancel" }) =>
      handleInviteRelation(token as string, appKey as string, input.relationId, input.action),
    onSuccess: invalidate
  });
}

export function useInviteStatsQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days],
    queryFn: () => getInviteStats(token as string, appKey as string, days),
    enabled: Boolean(token && appKey)
  });
}
