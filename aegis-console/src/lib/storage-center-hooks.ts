"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  clearStorageRedirect,
  connectCloudflare,
  createCloudflareBucket,
  getCloudflareBucketDetail,
  getCloudflareResources,
  setupCloudflareR2,
  type CloudflareSetupInput,
  collectStorageUsage,
  controlStorageMigration,
  createStorageMigration,
  deleteStoragePool,
  deleteStorageSource,
  getStorageCORS,
  getStorageOverview,
  getStorageProviders,
  listStorageEvents,
  listStorageMigrations,
  listStorageSources,
  probeStorageSource,
  resetStorageHealth,
  saveStoragePool,
  saveStorageSource,
  setStorageCORS,
  simulateStoragePool,
  testStorageSource,
  type StorageEventQuery,
  type StorageMigrationInput,
  type StoragePoolInput,
  type StorageScope,
  type StorageSourcePayload
} from "@/lib/api/storage-center";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 存储中心 hooks。
 *
 * 失效关系集中在这里：改存储源、改池、清退避、迁移都会同时改动总览的状态、
 * 池成员的可写性与事件流，所以整组一起失效，不逐个判断「这次写影响哪几块」。
 */

const KEY = {
  providers: "storage-center-providers",
  overview: "storage-center-overview",
  sources: "storage-center-sources",
  events: "storage-center-events",
  migrations: "storage-center-migrations",
  cors: "storage-center-cors",
  // 旧存储页的查询键：文件管理、CDN 的存储选择器仍在读它们。
  legacyGlobal: "storage-global",
  legacyApp: "storage-app"
} as const;

function useInvalidateStorage() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(Object.values(KEY).map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useStorageProvidersQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.providers, token],
    queryFn: () => getStorageProviders(token as string),
    enabled: Boolean(token),
    staleTime: 10 * 60_000,
    select: (data) => data.providers
  });
}

export function useStorageOverviewQuery(options?: { live?: boolean }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.overview, token],
    queryFn: () => getStorageOverview(token as string),
    enabled: Boolean(token),
    // 有迁移在跑、或成员在退避时，状态是分钟级在变的：页面开着就该跟上。
    refetchInterval: options?.live ? 5_000 : 30_000
  });
}

export function useStorageSourcesQuery(scope: StorageScope, appid?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.sources, token, scope, appid ?? null],
    queryFn: () => listStorageSources(token as string, scope, appid),
    enabled: Boolean(token) && (scope === "global" || Boolean(appid))
  });
}

export function useSaveStorageSourceMutation(scope: StorageScope) {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (payload: StorageSourcePayload & { config_id?: number; appid?: number | null }) =>
      saveStorageSource(token as string, scope, payload),
    onSuccess: invalidate
  });
}

export function useDeleteStorageSourceMutation(scope: StorageScope) {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (args: { configId: number; appid?: number | null }) =>
      deleteStorageSource(token as string, scope, args.configId, args.appid),
    onSuccess: invalidate
  });
}

export function useTestStorageSourceMutation(scope: StorageScope) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (args: { configId: number; appid?: number | null }) =>
      testStorageSource(token as string, scope, args.configId, args.appid)
  });
}

export function useProbeStorageMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (configId: number) => probeStorageSource(token as string, configId),
    onSuccess: invalidate
  });
}

export function useResetStorageHealthMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (configId: number) => resetStorageHealth(token as string, configId),
    onSuccess: invalidate
  });
}

export function useStorageCORSQuery(configId: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.cors, token, configId],
    queryFn: () => getStorageCORS(token as string, configId as number),
    enabled: Boolean(token && configId),
    retry: false
  });
}

export function useSetStorageCORSMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (args: { configId: number; origins: string[] }) =>
      setStorageCORS(token as string, args.configId, args.origins),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: [KEY.cors] })
  });
}

export function useClearStorageRedirectMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (configId: number) => clearStorageRedirect(token as string, configId),
    onSuccess: invalidate
  });
}

export function useSaveStoragePoolMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (args: { input: StoragePoolInput; poolId?: number }) => saveStoragePool(token as string, args.input, args.poolId),
    onSuccess: invalidate
  });
}

export function useDeleteStoragePoolMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (poolId: number) => deleteStoragePool(token as string, poolId),
    onSuccess: invalidate
  });
}

export function useSimulateStoragePoolMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (args: { poolId: number; size: number }) => simulateStoragePool(token as string, args.poolId, args.size)
  });
}

export function useStorageEventsQuery(query: StorageEventQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.events, token, query],
    queryFn: () => listStorageEvents(token as string, query),
    enabled: Boolean(token),
    refetchInterval: 15_000
  });
}

export function useStorageMigrationsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.migrations, token],
    queryFn: () => listStorageMigrations(token as string),
    enabled: Boolean(token),
    // 有任务在跑时每 2 秒刷新进度，跑完就停。
    refetchInterval: (query) =>
      (query.state.data ?? []).some((item) => item.status === "running" || item.status === "pending") ? 2_000 : false
  });
}

export function useCreateStorageMigrationMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (input: StorageMigrationInput) => createStorageMigration(token as string, input),
    onSuccess: invalidate
  });
}

export function useControlStorageMigrationMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (args: { id: number; action: "pause" | "resume" | "cancel" }) =>
      controlStorageMigration(token as string, args.id, args.action),
    onSuccess: invalidate
  });
}

export function useCollectStorageUsageMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => collectStorageUsage(token as string),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["storage-usage-history"] })
  });
}

// ── Cloudflare 快速接入：令牌只在组件状态里，不进查询缓存键之外的任何地方 ──

export function useConnectCloudflareMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (cfToken: string) => connectCloudflare(token as string, cfToken) });
}

export function useCloudflareResourcesQuery(cfToken: string | null, accountId: string | null) {
  const token = useAdminToken();
  return useQuery({
    // 令牌不进查询键：缓存键会被开发工具与日志看到。按账户区分即可，连接变了组件会整体重置。
    queryKey: ["storage-cloudflare-resources", token, accountId],
    queryFn: () => getCloudflareResources(token as string, cfToken as string, accountId as string),
    enabled: Boolean(token && cfToken && accountId),
    retry: false,
    staleTime: 30_000
  });
}

export function useCreateCloudflareBucketMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: Parameters<typeof createCloudflareBucket>[1]) => createCloudflareBucket(token as string, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["storage-cloudflare-resources"] })
  });
}

export function useCloudflareBucketDetailQuery(input: { token: string; accountId: string; bucket: string; jurisdiction: string } | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["storage-cloudflare-bucket", token, input?.accountId, input?.bucket, input?.jurisdiction],
    queryFn: () => getCloudflareBucketDetail(token as string, input!),
    enabled: Boolean(token && input),
    retry: false
  });
}

export function useSetupCloudflareMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidateStorage();
  return useMutation({
    mutationFn: (input: CloudflareSetupInput) => setupCloudflareR2(token as string, input),
    onSuccess: invalidate
  });
}
