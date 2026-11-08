"use client";

import { keepPreviousData, useMutation, useQuery } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import { downloadReport, getReport, type ReportKind, type ReportOf, type ReportParams } from "@/lib/api/reports";

/**
 * 报表分析中心的查询。
 *
 * 切换日期 / 粒度时保留上一份数据（placeholderData），图表在新数据到来前不闪成骨架；
 * 后端对同一窗口的明细有一分钟缓存，所以这里的 staleTime 也给一分钟。
 */
export function useReportQuery<K extends ReportKind>(kind: K, appKey: string | undefined, params: ReportParams | undefined) {
  const token = useAdminToken();
  return useQuery<ReportOf[K]>({
    queryKey: ["report", kind, appKey, params, token],
    queryFn: () => getReport(token as string, appKey as string, kind, params as ReportParams),
    enabled: Boolean(token && appKey && params),
    placeholderData: keepPreviousData,
    staleTime: 60_000
  });
}

export function useReportExportMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: ({ appKey, type, params }: { appKey: string; type: ReportKind | "all"; params: ReportParams }) =>
      downloadReport(token as string, appKey, type, params)
  });
}
