"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createLotteryActivity,
  createLotteryFortuneControls,
  getLotteryFortune,
  getLotteryFortuneUser,
  listLotteryFortuneControls,
  previewLotteryFortuneControl,
  revokeLotteryFortuneControls,
  setLotteryFortuneSpread,
  type LotteryFortuneControlParams,
  type LotteryFortuneControlPayload,
  deleteLotteryActivity,
  fulfillLotteryDraw,
  getLotteryAudit,
  getLotteryCatalog,
  getLotteryStats,
  listLotteryActivities,
  listLotteryDraws,
  setLotteryActivityStatus,
  simulateLottery,
  updateLotteryActivity,
  type LotteryActivityPayload,
  type LotteryDrawParams,
  type LotterySimulatePayload,
  type LotteryStatus
} from "@/lib/api/lottery";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 抽奖域 hooks。
 *
 * 失效关系集中在这里：改活动影响活动列表与统计里的奖品；标记发放影响记录、
 * 活动上的待发放数与统计。整组失效，不在组件里逐个点名。
 * 目录里的激励广告场景归激励广告区块管，改了场景要等目录自然过期或重新进入区块。
 */

const KEY = {
  catalog: "admin-lottery-catalog",
  activities: "admin-lottery-activities",
  draws: "admin-lottery-draws",
  stats: "admin-lottery-stats",
  audit: "admin-lottery-audit",
  fortune: "admin-lottery-fortune",
  fortuneUser: "admin-lottery-fortune-user",
  fortuneControls: "admin-lottery-fortune-controls"
} as const;

const LOTTERY_SCOPE = [KEY.activities, KEY.draws, KEY.stats];

function useInvalidateLotteryScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(LOTTERY_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useLotteryCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getLotteryCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

// ── 活动 ──

export function useLotteryActivitiesQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.activities, token, appKey],
    queryFn: () => listLotteryActivities(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveLotteryActivityMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (input: { activityId?: number; payload: LotteryActivityPayload }) =>
      input.activityId
        ? updateLotteryActivity(token as string, appKey as string, input.activityId, input.payload)
        : createLotteryActivity(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useSetLotteryActivityStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (input: { activityId: number; status: LotteryStatus }) =>
      setLotteryActivityStatus(token as string, appKey as string, input.activityId, input.status),
    onSuccess: invalidate
  });
}

export function useDeleteLotteryActivityMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (activityId: number) => deleteLotteryActivity(token as string, appKey as string, activityId),
    onSuccess: invalidate
  });
}

export function useLotteryStatsQuery(appKey?: string | null, activityId?: number | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, activityId, days],
    queryFn: () => getLotteryStats(token as string, appKey as string, activityId as number, days),
    enabled: Boolean(token && appKey && activityId)
  });
}

// ── 抽奖记录 ──

export function useLotteryDrawsQuery(appKey?: string | null, params?: LotteryDrawParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.draws,
      token,
      appKey,
      params?.activityId ?? 0,
      params?.userId ?? 0,
      params?.status ?? "",
      params?.source ?? "",
      params?.winning ?? false,
      params?.keyword ?? "",
      params?.start ?? "",
      params?.end ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listLotteryDraws(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useFulfillLotteryDrawMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (input: { drawId: number; note: string }) =>
      fulfillLotteryDraw(token as string, appKey as string, input.drawId, input.note),
    onSuccess: invalidate
  });
}

// ── 策略模拟 ──

/** 模拟不改任何数据，不失效缓存 */
export function useSimulateLotteryMutation(appKey: string) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: LotterySimulatePayload) => simulateLottery(token as string, appKey, payload)
  });
}

/** 出奖审计（读全部带方案的记录，按需手动刷新） */
export function useLotteryAuditQuery(appKey: string | null | undefined, activityId: number | null | undefined, days: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.audit, token, appKey, activityId, days],
    queryFn: () => getLotteryAudit(token as string, appKey as string, activityId as number, days),
    enabled: Boolean(token && appKey && activityId),
    staleTime: 60_000
  });
}

// ── 运气控制 ──
//
// 下干预、撤销、调差异都会改变今日分布、单人读数与记录：三者整组失效。调差异还改了活动策略，活动列表一并失效。

const FORTUNE_SCOPE = [KEY.fortune, KEY.fortuneUser, KEY.fortuneControls];

function useInvalidateFortuneScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(FORTUNE_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useLotteryFortuneQuery(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.fortune, token, appKey, activityId],
    queryFn: () => getLotteryFortune(token as string, appKey as string, activityId as number),
    enabled: Boolean(token && appKey && activityId)
  });
}

export function useLotteryFortuneUserQuery(appKey?: string | null, activityId?: number | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.fortuneUser, token, appKey, activityId, userId],
    queryFn: () => getLotteryFortuneUser(token as string, appKey as string, activityId as number, userId as number),
    enabled: Boolean(token && appKey && activityId && userId)
  });
}

export function useLotteryFortuneControlsQuery(
  appKey?: string | null,
  activityId?: number | null,
  params?: LotteryFortuneControlParams
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.fortuneControls,
      token,
      appKey,
      activityId,
      params?.userId ?? 0,
      params?.kind ?? "",
      params?.status ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listLotteryFortuneControls(token as string, appKey as string, activityId as number, params),
    enabled: Boolean(token && appKey && activityId),
    placeholderData: (previous) => previous
  });
}

export function useCreateLotteryFortuneControlsMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateFortuneScope();
  return useMutation({
    mutationFn: (payload: LotteryFortuneControlPayload) =>
      createLotteryFortuneControls(token as string, appKey as string, activityId as number, payload),
    onSuccess: invalidate
  });
}

/** 预演不改任何数据，不失效缓存 */
export function usePreviewLotteryFortuneControlMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: LotteryFortuneControlPayload) =>
      previewLotteryFortuneControl(token as string, appKey as string, activityId as number, payload)
  });
}

export function useRevokeLotteryFortuneControlsMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateFortuneScope();
  return useMutation({
    mutationFn: (input: { ids?: number[]; all?: boolean }) =>
      revokeLotteryFortuneControls(token as string, appKey as string, activityId as number, input),
    onSuccess: invalidate
  });
}

export function useSetLotteryFortuneSpreadMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const invalidateFortune = useInvalidateFortuneScope();
  const invalidateLottery = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (spread: number) => setLotteryFortuneSpread(token as string, appKey as string, activityId as number, spread),
    onSuccess: async () => {
      await Promise.all([invalidateFortune(), invalidateLottery()]);
    }
  });
}
