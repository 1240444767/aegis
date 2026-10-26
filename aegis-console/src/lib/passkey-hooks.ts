"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getAppPasskeyPolicy, saveAppPasskeyPolicy, type AppPasskeyPolicy } from "@/lib/api/passkey";
import { useAdminToken } from "@/lib/admin-hooks";

const ROOT = "admin-app-passkey";

export function useAppPasskeyPolicyQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, token, appKey],
    queryFn: () => getAppPasskeyPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 保存后用返回值写回缓存：推导出的来源与 assetlinks 立刻跟上 */
export function useSaveAppPasskeyPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (policy: AppPasskeyPolicy) => saveAppPasskeyPolicy(token as string, appKey as string, policy),
    onSuccess: (view) => queryClient.setQueryData([ROOT, token, appKey], view)
  });
}
