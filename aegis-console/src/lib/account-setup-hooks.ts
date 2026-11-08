"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getAccountSetupPolicy,
  getUserAccountSetup,
  renameAppUser,
  resetUserAccountSetup,
  saveAccountSetupPolicy,
  type AccountSetupPolicy
} from "@/lib/api/account-setup";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 账号初始化 hooks。
 *
 * 单个用户的改名与重置都会改变应用策略页的统计（待设置 / 已完成人数），所以写完整组失效；
 * 改名还会改变用户详情、用户列表与在线用户里显示的账号名，一并失效。
 */

const ROOT = "admin-account-setup";

/** 账号名会出现在这些列表与详情里 */
const ACCOUNT_NAME_KEYS = [["admin-app-user"], ["admin-app-users"], ["app-online-users"]] as const;

type Key = string | null | undefined;
type Id = number | string | null | undefined;

export function useAccountSetupPolicyQuery(appKey?: Key) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "policy", token, appKey],
    queryFn: () => getAccountSetupPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 保存后用返回的策略与统计写回缓存，表单立即跟上 */
export function useSaveAccountSetupPolicyMutation(appKey?: Key) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (policy: Omit<AccountSetupPolicy, "isDefault">) =>
      saveAccountSetupPolicy(token as string, appKey as string, policy),
    onSuccess: async (view) => {
      queryClient.setQueryData([ROOT, "policy", token, appKey], view);
      // 策略变了，每个用户的「是否需要提醒 / 策略是否覆盖」都跟着变
      await queryClient.invalidateQueries({ queryKey: [ROOT, "user"] });
    }
  });
}

export function useUserAccountSetupQuery(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "user", token, appKey, userId],
    queryFn: () => getUserAccountSetup(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useResetUserAccountSetupMutation(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (resetAccount: boolean) =>
      resetUserAccountSetup(token as string, appKey as string, userId as number, resetAccount),
    onSuccess: async (view) => {
      queryClient.setQueryData([ROOT, "user", token, appKey, userId], view);
      await queryClient.invalidateQueries({ queryKey: [ROOT, "policy"] });
    }
  });
}

export function useRenameAppUserMutation(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (account: string) => renameAppUser(token as string, appKey as string, userId as number, account),
    onSuccess: async (result) => {
      queryClient.setQueryData([ROOT, "user", token, appKey, userId], result.accountSetup);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [ROOT, "policy"] }),
        ...ACCOUNT_NAME_KEYS.map((queryKey) => queryClient.invalidateQueries({ queryKey }))
      ]);
    }
  });
}
