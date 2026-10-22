"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteDeviceLimitOverride,
  enforceDeviceLimit,
  getDeviceLimitOverview,
  getDeviceLimitPolicy,
  getDeviceLimitUser,
  listDeviceLimitEvents,
  listDeviceLimitOverrides,
  removeDeviceLimitDevices,
  resetDeviceLimitLedger,
  saveDeviceLimitOverride,
  saveDeviceLimitPolicy,
  simulateDeviceLimit,
  type DeviceLimitEventParams,
  type DeviceLimitOverridePayload,
  type DeviceLimitPolicy,
  type DeviceLimitSimulateInput
} from "@/lib/api/device-limit";
import { useAdminToken } from "@/lib/admin-hooks";
import { browserTimeZone } from "@/lib/datetime";

/**
 * 登录设备数控制 hooks。
 *
 * 任何写操作都可能同时改变概览、处置记录、例外名单与用户视图（下线一台设备会多一条记录、
 * 改策略会改变每个人的生效上限），所以写完整组失效，不在组件里逐个点名。
 */

const ROOT = "admin-device-limit";

function useInvalidateDeviceLimit() {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: [ROOT] });
}

export function useDeviceLimitOverviewQuery(appKey: string | null | undefined, days: number) {
  const token = useAdminToken();
  const tz = browserTimeZone();
  return useQuery({
    queryKey: [ROOT, "overview", token, appKey, days, tz],
    queryFn: () => getDeviceLimitOverview(token as string, appKey as string, days, tz),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useDeviceLimitPolicyQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "policy", token, appKey],
    queryFn: () => getDeviceLimitPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 保存后先用返回值写回缓存，编辑器立即跟上；其余视图随整组失效刷新 */
export function useSaveDeviceLimitPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const invalidate = useInvalidateDeviceLimit();
  return useMutation({
    mutationFn: (policy: DeviceLimitPolicy) => saveDeviceLimitPolicy(token as string, appKey as string, policy),
    onSuccess: async (view) => {
      queryClient.setQueryData([ROOT, "policy", token, appKey], view);
      await invalidate();
    }
  });
}

export function useSimulateDeviceLimitMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (input: DeviceLimitSimulateInput) => simulateDeviceLimit(token as string, appKey as string, input)
  });
}

export function useDeviceLimitEventsQuery(appKey: string | null | undefined, params: DeviceLimitEventParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "events", token, appKey, params],
    queryFn: () => listDeviceLimitEvents(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useDeviceLimitOverridesQuery(
  appKey: string | null | undefined,
  params: { keyword?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "overrides", token, appKey, params],
    queryFn: () => listDeviceLimitOverrides(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useSaveDeviceLimitOverrideMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateDeviceLimit();
  return useMutation({
    mutationFn: ({ userId, payload }: { userId: number; payload: DeviceLimitOverridePayload }) =>
      saveDeviceLimitOverride(token as string, appKey as string, userId, payload),
    onSuccess: () => invalidate()
  });
}

export function useDeleteDeviceLimitOverrideMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateDeviceLimit();
  return useMutation({
    mutationFn: (userId: number) => deleteDeviceLimitOverride(token as string, appKey as string, userId),
    onSuccess: () => invalidate()
  });
}

export function useDeviceLimitUserQuery(appKey: string | null | undefined, userId: number | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "user", token, appKey, userId],
    queryFn: () => getDeviceLimitUser(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useRemoveDeviceLimitDevicesMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateDeviceLimit();
  return useMutation({
    mutationFn: ({ userId, deviceKeys }: { userId: number; deviceKeys: string[] }) =>
      removeDeviceLimitDevices(token as string, appKey as string, userId, deviceKeys),
    onSuccess: () => invalidate()
  });
}

export function useEnforceDeviceLimitMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateDeviceLimit();
  return useMutation({
    mutationFn: (userId: number) => enforceDeviceLimit(token as string, appKey as string, userId),
    onSuccess: () => invalidate()
  });
}

export function useResetDeviceLimitLedgerMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateDeviceLimit();
  return useMutation({
    mutationFn: ({ userId, key }: { userId: number; key?: string }) =>
      resetDeviceLimitLedger(token as string, appKey as string, userId, key),
    onSuccess: () => invalidate()
  });
}
