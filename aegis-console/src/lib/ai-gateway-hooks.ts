"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  adjustAIGatewayUserCredit,
  createAIGatewayModel,
  deleteAIGatewayModel,
  getAIGatewayCatalog,
  getAIGatewayRequest,
  getAIGatewaySettings,
  getAIGatewayStats,
  getAIGatewayUserAccount,
  listAIGatewayKeys,
  listAIGatewayModels,
  listAIGatewayRequests,
  listAIGatewayUserCredits,
  refundAIGatewayRequest,
  saveAIGatewaySettings,
  setAIGatewayKeyStatus,
  updateAIGatewayModel,
  type AIGatewayCreditAdjustPayload,
  type AIGatewayKeyParams,
  type AIGatewayModelPayload,
  type AIGatewayRequestParams,
  type AIGatewaySettings
} from "@/lib/api/ai-gateway";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * AI 中转网关域 hooks。
 *
 * 失效关系：改设置会改动用户的额度与倍率（账户视图），改模型会改动统计里的模型名，
 * 退款与额度调整会改动统计、记录与账户 —— 整组一起失效，与云存储区块同一个取舍。
 */

const KEY = {
  catalog: "admin-ai-gateway-catalog",
  settings: "admin-ai-gateway-settings",
  models: "admin-ai-gateway-models",
  requests: "admin-ai-gateway-requests",
  request: "admin-ai-gateway-request",
  keys: "admin-ai-gateway-keys",
  stats: "admin-ai-gateway-stats",
  account: "admin-ai-gateway-account",
  credits: "admin-ai-gateway-credits"
} as const;

function useInvalidateScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(
      Object.values(KEY)
        .filter((key) => key !== KEY.catalog)
        .map((key) => queryClient.invalidateQueries({ queryKey: [key] }))
    );
  };
}

export function useAIGatewayCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getAIGatewayCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: Infinity
  });
}

export function useAIGatewaySettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getAIGatewaySettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAIGatewaySettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: (payload: AIGatewaySettings) => saveAIGatewaySettings(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useAIGatewayModelsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.models, token, appKey],
    queryFn: () => listAIGatewayModels(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAIGatewayModelMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ id, payload }: { id?: number; payload: AIGatewayModelPayload }) =>
      id
        ? updateAIGatewayModel(token as string, appKey as string, id, payload)
        : createAIGatewayModel(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAIGatewayModelMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: (modelId: number) => deleteAIGatewayModel(token as string, appKey as string, modelId),
    onSuccess: invalidate
  });
}

export function useAIGatewayRequestsQuery(appKey: string | null | undefined, params: AIGatewayRequestParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.requests, token, appKey, params],
    queryFn: () => listAIGatewayRequests(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useAIGatewayRequestQuery(appKey?: string | null, requestNo?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.request, token, appKey, requestNo],
    queryFn: () => getAIGatewayRequest(token as string, appKey as string, requestNo as string),
    enabled: Boolean(token && appKey && requestNo)
  });
}

export function useRefundAIGatewayRequestMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ requestNo, reason }: { requestNo: string; reason: string }) =>
      refundAIGatewayRequest(token as string, appKey as string, requestNo, reason),
    onSuccess: invalidate
  });
}

export function useAIGatewayKeysQuery(appKey: string | null | undefined, params: AIGatewayKeyParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.keys, token, appKey, params],
    queryFn: () => listAIGatewayKeys(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useSetAIGatewayKeyStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ keyId, status }: { keyId: number; status: "active" | "disabled" }) =>
      setAIGatewayKeyStatus(token as string, appKey as string, keyId, status),
    onSuccess: invalidate
  });
}

export function useAIGatewayStatsQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days],
    queryFn: () => getAIGatewayStats(token as string, appKey as string, days),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useAIGatewayUserAccountQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.account, token, appKey, userId],
    queryFn: () => getAIGatewayUserAccount(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useAIGatewayUserCreditsQuery(appKey: string | null | undefined, userId: number | null, page: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.credits, token, appKey, userId, page],
    queryFn: () => listAIGatewayUserCredits(token as string, appKey as string, userId as number, page, 20),
    enabled: Boolean(token && appKey && userId),
    placeholderData: (previous) => previous
  });
}

export function useAdjustAIGatewayCreditMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ userId, payload }: { userId: number; payload: AIGatewayCreditAdjustPayload }) =>
      adjustAIGatewayUserCredit(token as string, appKey as string, userId, payload),
    onSuccess: invalidate
  });
}
