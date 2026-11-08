"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  createClientControlRule,
  deleteClientControlDevice,
  deleteClientControlRule,
  getClientControlDevices,
  getClientControlKeys,
  getClientControlOverview,
  getClientControlRules,
  getClientControlSettings,
  getClientControlVersions,
  refreshClientControl,
  reorderClientControlRules,
  rotateClientControlKey,
  setClientControlRuleEnabled,
  simulateClientControl,
  updateClientControlRule,
  updateClientControlSettings,
  type ClientControlRuleInput,
  type ClientControlSettingsPatch,
  type ClientControlSimulateInput,
  type ClientInstallParams
} from "@/lib/api/client-control";

/**
 * 版本管控的 React Query hooks。
 *
 * 任何规则或设置的写操作都会让策略版本加一，并改变版本分布里「按当前规则」的判定，
 * 所以写操作一律把整组查询（前缀 client-control）失效，而不是逐个挑。
 */

const ROOT = "client-control";

function useInvalidateAll() {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: [ROOT] });
}

function useScopedQuery<T>(appKey: string | null | undefined, part: unknown[], fn: (token: string, appKey: string) => Promise<T>) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, ...part, token, appKey],
    queryFn: () => fn(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useClientControlOverviewQuery(appKey?: string | null) {
  return useScopedQuery(appKey, ["overview"], getClientControlOverview);
}

export function useClientControlSettingsQuery(appKey?: string | null) {
  return useScopedQuery(appKey, ["settings"], getClientControlSettings);
}

export function useClientControlRulesQuery(appKey?: string | null) {
  return useScopedQuery(appKey, ["rules"], getClientControlRules);
}

export function useClientControlVersionsQuery(appKey: string | null | undefined, days: number) {
  return useScopedQuery(appKey, ["versions", days], (token, key) => getClientControlVersions(token, key, days));
}

export function useClientControlDevicesQuery(appKey: string | null | undefined, params: ClientInstallParams) {
  return useScopedQuery(appKey, ["devices", params], (token, key) => getClientControlDevices(token, key, params));
}

export function useClientControlKeysQuery(appKey?: string | null) {
  return useScopedQuery(appKey, ["keys"], getClientControlKeys);
}

export function useUpdateClientControlSettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (patch: ClientControlSettingsPatch) => updateClientControlSettings(token as string, appKey as string, patch),
    onSuccess: invalidate
  });
}

export function useRefreshClientControlMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: () => refreshClientControl(token as string, appKey as string),
    onSuccess: invalidate
  });
}

export function useSaveClientControlRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (input: { ruleId?: number | null; payload: ClientControlRuleInput }) =>
      input.ruleId
        ? updateClientControlRule(token as string, appKey as string, input.ruleId, input.payload)
        : createClientControlRule(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useToggleClientControlRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (input: { ruleId: number; enabled: boolean }) =>
      setClientControlRuleEnabled(token as string, appKey as string, input.ruleId, input.enabled),
    onSuccess: invalidate
  });
}

export function useDeleteClientControlRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (ruleId: number) => deleteClientControlRule(token as string, appKey as string, ruleId),
    onSuccess: invalidate
  });
}

export function useReorderClientControlRulesMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (ruleIds: number[]) => reorderClientControlRules(token as string, appKey as string, ruleIds),
    onSuccess: invalidate
  });
}

export function useDeleteClientControlDeviceMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (input: { deviceId: string; packageName: string }) =>
      deleteClientControlDevice(token as string, appKey as string, input.deviceId, input.packageName),
    onSuccess: invalidate
  });
}

export function useRotateClientControlKeyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (kind: "sign" | "box") => rotateClientControlKey(token as string, appKey as string, kind),
    onSuccess: invalidate
  });
}

export function useSimulateClientControlMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (input: ClientControlSimulateInput) => simulateClientControl(token as string, appKey as string, input)
  });
}
