"use client";

/**
 * 用户详情「流水」页的 hooks。
 *
 * 查询键以 `admin-app-user` 开头：积分 / 经验调整成功后已有的
 * `invalidateQueries({ queryKey: ["admin-app-user"] })` 会顺带刷新这里，不必再逐个登记。
 */
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import { browserTimeZone } from "@/lib/datetime";
import {
  getUserPointLedger,
  getUserPointSummary,
  reverseUserIntegral,
  type PointKind,
  type PointLedgerParams
} from "@/lib/api/user-ledger";

type Key = string | null | undefined;
type Id = number | string | null | undefined;

export function useUserPointLedgerQuery(appKey: Key, userId: Id, kind: PointKind, params: PointLedgerParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-user", "ledger", kind, token, appKey, userId, params],
    queryFn: () => getUserPointLedger(token as string, appKey as string, userId as number, kind, params),
    enabled: Boolean(token && appKey && userId),
    placeholderData: keepPreviousData
  });
}

export function useUserPointSummaryQuery(appKey: Key, userId: Id, kind: PointKind, days = 30) {
  const token = useAdminToken();
  const tz = browserTimeZone();
  return useQuery({
    queryKey: ["admin-app-user", "ledger-summary", kind, token, appKey, userId, days, tz],
    queryFn: () => getUserPointSummary(token as string, appKey as string, userId as number, kind, { days, tz }),
    enabled: Boolean(token && appKey && userId),
    placeholderData: keepPreviousData
  });
}

export function useReverseUserIntegralMutation(appKey: Key, userId: Id) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (args: { transactionNo: string; reason?: string }) =>
      reverseUserIntegral(token as string, appKey as string, userId as number, args.transactionNo, args.reason),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-app-user"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-users"] })
      ])
  });
}
