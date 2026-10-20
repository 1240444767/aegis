"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  adminRedeemCardKey,
  appendCardKeys,
  deleteCardKeyBatch,
  deleteCardKeys,
  disableCardKeys,
  distributeCardKeys,
  downloadCardKeyBatch,
  extractCardKeys,
  generateCardKeys,
  getCardKeyCatalog,
  getCardKeyDetail,
  getCardKeyStats,
  listCardKeyBatches,
  listCardKeyRedemptions,
  listCardKeys,
  resetCardKeyDevices,
  restoreCardKeys,
  setCardKeyBatchStatus,
  unbindCardKeyDevice,
  updateCardKey,
  updateCardKeyBatch,
  type CardKeyExportOptions,
  type CardKeyKind,
  type CardKeyListParams,
  type CardKeyRedemptionParams,
  type GenerateCardKeysPayload,
  type UpdateCardKeyBatchPayload,
  type UpdateCardKeyPayload
} from "@/lib/api/card-key";
import { useAdminToken } from "@/lib/admin-hooks";
import { browserTimeZone } from "@/lib/datetime";

/**
 * 卡密域 hooks。
 *
 * **失效关系集中在这里**，不散在组件里：生成、补发、提卡、作废、代兑都会同时影响
 * 批次进度、单卡列表、单卡详情、核销记录与概览。任一写操作后整组失效 ——
 * 散着写必然漏掉其中几张，表现是「操作成功了但列表没变」，而那看起来像操作失败。
 * 目录不在其中：它只随后端发版与会员套餐 / 功能目录变化。
 */

const KEY = {
  catalog: "admin-card-key-catalog",
  stats: "admin-card-key-stats",
  batches: "admin-card-key-batches",
  codes: "admin-card-keys",
  detail: "admin-card-key-detail",
  redemptions: "admin-card-key-redemptions"
} as const;

const CARD_KEY_SCOPE = [KEY.stats, KEY.batches, KEY.codes, KEY.detail, KEY.redemptions];

function useInvalidateCardKeyScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(CARD_KEY_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

function useScopedMutation<TInput, TResult>(
  appKey: string | null | undefined,
  run: (token: string, appKey: string, input: TInput) => Promise<TResult>
) {
  const token = useAdminToken();
  const invalidate = useInvalidateCardKeyScope();
  return useMutation({
    mutationFn: (input: TInput) => run(token as string, appKey as string, input),
    onSuccess: invalidate
  });
}

// ── 目录与统计 ──

/** 权益目录与可引用的套餐、功能。staleTime 五分钟：套餐与功能在会员区块里可能刚改过。 */
export function useCardKeyCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getCardKeyCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 5 * 60 * 1000
  });
}

/** 概览按浏览器时区分日（今日核销与趋势的日界）。 */
export function useCardKeyStatsQuery(appKey?: string | null, days = 30) {
  const token = useAdminToken();
  const tz = browserTimeZone();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days, tz],
    queryFn: () => getCardKeyStats(token as string, appKey as string, days, tz),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

// ── 批次 ──

export function useCardKeyBatchesQuery(appKey?: string | null, params?: { keyword?: string; kind?: CardKeyKind }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.batches, token, appKey, params?.keyword ?? "", params?.kind ?? ""],
    queryFn: () => listCardKeyBatches(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useGenerateCardKeysMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, payload: GenerateCardKeysPayload) =>
    generateCardKeys(token, app, payload)
  );
}

export function useUpdateCardKeyBatchMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, input: { batchId: number; payload: UpdateCardKeyBatchPayload }) =>
    updateCardKeyBatch(token, app, input.batchId, input.payload)
  );
}

export function useSetCardKeyBatchStatusMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, input: { batchId: number; enabled: boolean }) =>
    setCardKeyBatchStatus(token, app, input.batchId, input.enabled)
  );
}

export function useDeleteCardKeyBatchMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, batchId: number) => deleteCardKeyBatch(token, app, batchId));
}

export function useAppendCardKeysMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, input: { batchId: number; count: number }) =>
    appendCardKeys(token, app, input.batchId, input.count)
  );
}

export function useExtractCardKeysMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, input: { batchId: number; count: number; note: string }) =>
    extractCardKeys(token, app, input.batchId, input.count, input.note)
  );
}

/** 导出不失效任何缓存 —— 它只是把已有数据取一份下来。 */
export function useExportCardKeyBatchMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (input: { batchId: number; options: CardKeyExportOptions }) =>
      downloadCardKeyBatch(token as string, appKey as string, input.batchId, input.options)
  });
}

// ── 单卡 ──

export function useCardKeysQuery(appKey?: string | null, params?: CardKeyListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.codes,
      token,
      appKey,
      params?.batchId ?? 0,
      params?.status ?? "",
      params?.kind ?? "",
      params?.distributed ?? "",
      params?.keyword ?? "",
      params?.userId ?? 0,
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listCardKeys(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useCardKeyDetailQuery(appKey?: string | null, cardId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.detail, token, appKey, cardId ?? 0],
    queryFn: () => getCardKeyDetail(token as string, appKey as string, cardId as number),
    enabled: Boolean(token && appKey && cardId)
  });
}

export function useUpdateCardKeyMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, input: { cardId: number; payload: UpdateCardKeyPayload }) =>
    updateCardKey(token, app, input.cardId, input.payload)
  );
}

export function useDisableCardKeysMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, input: { ids: number[]; reason?: string }) =>
    disableCardKeys(token, app, input.ids, input.reason)
  );
}

export function useRestoreCardKeysMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, ids: number[]) => restoreCardKeys(token, app, ids));
}

export function useDeleteCardKeysMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, ids: number[]) => deleteCardKeys(token, app, ids));
}

export function useDistributeCardKeysMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, input: { ids: number[]; distributed: boolean; note?: string }) =>
    distributeCardKeys(token, app, input.ids, input.distributed, input.note)
  );
}

export function useUnbindCardKeyDeviceMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, input: { cardId: number; deviceId: string }) =>
    unbindCardKeyDevice(token, app, input.cardId, input.deviceId)
  );
}

export function useResetCardKeyDevicesMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, cardId: number) => resetCardKeyDevices(token, app, cardId));
}

// ── 核销记录与代兑 ──

export function useCardKeyRedemptionsQuery(appKey?: string | null, params?: CardKeyRedemptionParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.redemptions,
      token,
      appKey,
      params?.batchId ?? 0,
      params?.cardId ?? 0,
      params?.userId ?? 0,
      params?.source ?? "",
      params?.keyword ?? "",
      params?.from ?? "",
      params?.to ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listCardKeyRedemptions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useAdminRedeemCardKeyMutation(appKey?: string | null) {
  return useScopedMutation(appKey, (token, app, payload: { code: string; userId?: number; account?: string }) =>
    adminRedeemCardKey(token, app, payload)
  );
}
