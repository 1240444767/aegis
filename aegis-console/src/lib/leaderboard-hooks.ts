"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  addLeaderboardExclusions,
  getLeaderboardBoard,
  getLeaderboardSettings,
  getLeaderboardStats,
  listLeaderboardExclusions,
  listLeaderboardSnapshotEntries,
  listLeaderboardSnapshots,
  removeLeaderboardExclusion,
  retryLeaderboardSnapshot,
  saveLeaderboardSettings,
  settleLeaderboard,
  type LeaderboardBoardParams,
  type LeaderboardEntryParams,
  type LeaderboardExclusionParams,
  type LeaderboardSettingsPayload,
  type LeaderboardSnapshotParams
} from "@/lib/api/leaderboard";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 排行榜域 hooks。
 *
 * 失效关系集中在这里：改设置影响榜单内容（门槛、展示身份、口径）与概览；移出或恢复一个人影响榜单、
 * 移出名单与概览；结算与重试影响结算记录、明细与概览里的待发 / 失败数。整组失效，不在组件里逐个点名。
 */

const KEY = {
  settings: "admin-leaderboard-settings",
  board: "admin-leaderboard-board",
  stats: "admin-leaderboard-stats",
  exclusions: "admin-leaderboard-exclusions",
  snapshots: "admin-leaderboard-snapshots",
  entries: "admin-leaderboard-entries"
} as const;

const LEADERBOARD_SCOPE = Object.values(KEY);

function useInvalidateLeaderboardScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(LEADERBOARD_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

// ── 设置 ──

export function useLeaderboardSettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getLeaderboardSettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveLeaderboardSettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLeaderboardScope();
  return useMutation({
    mutationFn: (payload: LeaderboardSettingsPayload) => saveLeaderboardSettings(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

// ── 榜单与概览 ──

export function useLeaderboardBoardQuery(appKey?: string | null, key?: string | null, params?: LeaderboardBoardParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.board, token, appKey, key, params?.periodKey ?? "", params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => getLeaderboardBoard(token as string, appKey as string, key as string, params),
    enabled: Boolean(token && appKey && key),
    // 翻页时保留上一页，表格不闪成骨架
    placeholderData: keepPreviousData
  });
}

export function useLeaderboardStatsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey],
    queryFn: () => getLeaderboardStats(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

// ── 移出名单 ──

export function useLeaderboardExclusionsQuery(appKey?: string | null, params?: LeaderboardExclusionParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.exclusions, token, appKey, params?.keyword ?? "", params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listLeaderboardExclusions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAddLeaderboardExclusionsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLeaderboardScope();
  return useMutation({
    mutationFn: (input: { userIds: number[]; reason: string }) =>
      addLeaderboardExclusions(token as string, appKey as string, input.userIds, input.reason),
    onSuccess: invalidate
  });
}

export function useRemoveLeaderboardExclusionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLeaderboardScope();
  return useMutation({
    mutationFn: (userId: number) => removeLeaderboardExclusion(token as string, appKey as string, userId),
    onSuccess: invalidate
  });
}

// ── 结算 ──

export function useLeaderboardSnapshotsQuery(appKey?: string | null, params?: LeaderboardSnapshotParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.snapshots, token, appKey, params?.board ?? "", params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listLeaderboardSnapshots(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useLeaderboardSnapshotEntriesQuery(
  appKey?: string | null,
  snapshotId?: number | null,
  params?: LeaderboardEntryParams
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.entries, token, appKey, snapshotId, params?.status ?? "", params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listLeaderboardSnapshotEntries(token as string, appKey as string, snapshotId as number, params),
    enabled: Boolean(token && appKey && snapshotId),
    placeholderData: keepPreviousData
  });
}

export function useRetryLeaderboardSnapshotMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLeaderboardScope();
  return useMutation({
    mutationFn: (snapshotId: number) => retryLeaderboardSnapshot(token as string, appKey as string, snapshotId),
    onSuccess: invalidate
  });
}

export function useSettleLeaderboardMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLeaderboardScope();
  return useMutation({
    mutationFn: (input: { board: string; periodKey: string }) =>
      settleLeaderboard(token as string, appKey as string, input.board, input.periodKey),
    onSuccess: invalidate
  });
}
