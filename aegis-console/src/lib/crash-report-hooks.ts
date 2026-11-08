"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteCrashReport,
  deleteCrashReports,
  getCrashReport,
  getCrashReportFacets,
  listCrashReports,
  resolveCrashReports,
  type CrashReportParams
} from "@/lib/api/crash-reports";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 崩溃报告域 hooks。标记与删除会同时改变列表、摘要计数与详情，写操作后整组失效（前缀 `admin-crash-reports`）。
 */

const KEY = {
  list: "admin-crash-reports-list",
  facets: "admin-crash-reports-facets",
  detail: "admin-crash-reports-detail"
} as const;

function useInvalidateCrashReports() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(Object.values(KEY).map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useCrashReportsQuery(appKey?: string | null, params?: CrashReportParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.list,
      token,
      appKey,
      params?.kind ?? "",
      params?.appVersion ?? "",
      params?.keyword ?? "",
      params?.resolved ?? "",
      params?.userId ?? 0,
      params?.deviceId ?? "",
      params?.start ?? "",
      params?.end ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listCrashReports(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useCrashReportFacetsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.facets, token, appKey],
    queryFn: () => getCrashReportFacets(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useCrashReportQuery(appKey?: string | null, id?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.detail, token, appKey, id],
    queryFn: () => getCrashReport(token as string, appKey as string, id as number),
    enabled: Boolean(token && appKey && id)
  });
}

export function useResolveCrashReportsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCrashReports();
  return useMutation({
    mutationFn: ({ ids, resolved }: { ids: number[]; resolved: boolean }) =>
      resolveCrashReports(token as string, appKey as string, ids, resolved),
    onSuccess: invalidate
  });
}

export function useDeleteCrashReportsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCrashReports();
  return useMutation({
    mutationFn: (ids: number[]) =>
      ids.length === 1
        ? deleteCrashReport(token as string, appKey as string, ids[0])
        : deleteCrashReports(token as string, appKey as string, ids),
    onSuccess: invalidate
  });
}
