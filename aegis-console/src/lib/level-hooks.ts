"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  adjustLevelUser,
  backfillLevelReward,
  cancelLevelJob,
  fulfillLevelReward,
  getLevelCatalog,
  getLevelOverview,
  getLevelScheme,
  getLevelUser,
  getLevelVersion,
  listLevelEvents,
  listLevelJobs,
  listLevelRewards,
  listLevelUsers,
  listLevelVersions,
  publishLevelScheme,
  restoreLevelVersion,
  retryLevelReward,
  saveLevelScheme,
  simulateLevel,
  startLevelJob,
  validateLevelScheme,
  type LevelAdjustPayload,
  type LevelEventParams,
  type LevelPublishPayload,
  type LevelRewardParams,
  type LevelSimulatePayload,
  type LevelUserParams
} from "@/lib/api/levels";
import type { ItemGrantGraph } from "@/lib/api/item-grant";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 等级体系 hooks（docs/levels.md）。
 *
 * 发布、调级、重算、奖励处理都会影响总览、用户等级、升级记录与奖励记录，整组失效。
 * 有任务在跑时总览与任务列表每 2 秒刷新，跑完自动停；有奖励在排队发放时奖励记录每 3 秒刷新。
 */

const KEY = {
  catalog: "admin-level-catalog",
  overview: "admin-level-overview",
  scheme: "admin-level-scheme",
  versions: "admin-level-versions",
  version: "admin-level-version",
  users: "admin-level-users",
  user: "admin-level-user",
  events: "admin-level-events",
  rewards: "admin-level-rewards",
  jobs: "admin-level-jobs"
} as const;

const SCOPE = [KEY.overview, KEY.scheme, KEY.versions, KEY.users, KEY.user, KEY.events, KEY.rewards, KEY.jobs];

function useInvalidateLevelScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useLevelCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getLevelCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 60_000
  });
}

export function useLevelOverviewQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.overview, token, appKey, days],
    queryFn: () => getLevelOverview(token as string, appKey as string, days),
    enabled: Boolean(token && appKey),
    refetchInterval: (query) => (query.state.data?.runningJob || query.state.data?.rewards.queued ? 2000 : false)
  });
}

export function useLevelSchemeQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.scheme, token, appKey],
    queryFn: () => getLevelScheme(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveLevelSchemeMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { graph: ItemGrantGraph; revision: number }) =>
      saveLevelScheme(token as string, appKey as string, input.graph, input.revision),
    onSuccess: (data) => {
      queryClient.setQueryData([KEY.scheme, token, appKey], data);
    }
  });
}

/** 校验与推演不改任何数据，不失效缓存 */
export function useValidateLevelSchemeMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (graph: ItemGrantGraph) => validateLevelScheme(token as string, appKey as string, graph)
  });
}

export function useSimulateLevelMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: LevelSimulatePayload) => simulateLevel(token as string, appKey as string, payload)
  });
}

export function usePublishLevelSchemeMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLevelScope();
  return useMutation({
    mutationFn: (payload: LevelPublishPayload) => publishLevelScheme(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useLevelVersionsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.versions, token, appKey],
    queryFn: () => listLevelVersions(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useLevelVersionQuery(appKey?: string | null, version?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.version, token, appKey, version],
    queryFn: () => getLevelVersion(token as string, appKey as string, version as number),
    enabled: Boolean(token && appKey && version),
    staleTime: Infinity
  });
}

export function useRestoreLevelVersionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLevelScope();
  return useMutation({
    mutationFn: (version: number) => restoreLevelVersion(token as string, appKey as string, version),
    onSuccess: invalidate
  });
}

// ── 用户 ──

export function useLevelUsersQuery(appKey?: string | null, params: LevelUserParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.users, token, appKey, params],
    queryFn: () => listLevelUsers(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useLevelUserQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.user, token, appKey, userId],
    queryFn: () => getLevelUser(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useAdjustLevelUserMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLevelScope();
  return useMutation({
    mutationFn: (input: { userId: number; payload: LevelAdjustPayload }) =>
      adjustLevelUser(token as string, appKey as string, input.userId, input.payload),
    onSuccess: invalidate
  });
}

// ── 记录 ──

export function useLevelEventsQuery(appKey?: string | null, params: LevelEventParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.events, token, appKey, params],
    queryFn: () => listLevelEvents(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useLevelRewardsQuery(appKey?: string | null, params: LevelRewardParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.rewards, token, appKey, params],
    queryFn: () => listLevelRewards(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous,
    refetchInterval: (query) => (query.state.data?.items.some((item) => item.status === "queued") ? 3000 : false)
  });
}

export function useRetryLevelRewardMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLevelScope();
  return useMutation({
    mutationFn: (rewardId: number) => retryLevelReward(token as string, appKey as string, rewardId),
    onSuccess: invalidate
  });
}

export function useFulfillLevelRewardMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLevelScope();
  return useMutation({
    mutationFn: (input: { rewardId: number; note: string }) =>
      fulfillLevelReward(token as string, appKey as string, input.rewardId, input.note),
    onSuccess: invalidate
  });
}

export function useBackfillLevelRewardMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLevelScope();
  return useMutation({
    mutationFn: (nodeId: string) => backfillLevelReward(token as string, appKey as string, nodeId),
    onSuccess: invalidate
  });
}

// ── 重算任务 ──

export function useLevelJobsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.jobs, token, appKey],
    queryFn: () => listLevelJobs(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    refetchInterval: (query) => (query.state.data?.some((job) => job.status === "running") ? 2000 : false)
  });
}

export function useStartLevelJobMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLevelScope();
  return useMutation({
    mutationFn: (downgrade: boolean) => startLevelJob(token as string, appKey as string, downgrade),
    onSuccess: invalidate
  });
}

export function useCancelLevelJobMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLevelScope();
  return useMutation({
    mutationFn: (jobId: number) => cancelLevelJob(token as string, appKey as string, jobId),
    onSuccess: invalidate
  });
}
