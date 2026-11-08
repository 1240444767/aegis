"use client";

import Link from "next/link";
import { CheckCheck, Copy, Download, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  CrashKindBadge,
  CrashStatusBadge,
  crashDeviceLabel,
  crashLocationLabel,
  crashVersionLabel,
  downloadCrashReport,
  formatBytes
} from "@/components/apps/crash-reports/crash-report-shared";
import { CopyButton, copyText } from "@/components/apps/card-key/card-key-shared";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { CrashReport } from "@/lib/api/crash-reports";
import {
  useCrashReportQuery,
  useDeleteCrashReportsMutation,
  useResolveCrashReportsMutation
} from "@/lib/crash-report-hooks";
import { formatDateTime } from "@/lib/datetime";

/**
 * 崩溃报告详情：全部字段 + 报告正文（等宽、可复制、可下载为 .txt）+ 标记处理状态与删除。
 * 正文只在这里取，列表接口不带。
 */
export function CrashReportDetailSheet({
  appKey,
  reportId,
  onOpenChange
}: {
  appKey: string;
  reportId: number | null;
  onOpenChange: (open: boolean) => void;
}) {
  const query = useCrashReportQuery(appKey, reportId);
  const resolveMutation = useResolveCrashReportsMutation(appKey);
  const deleteMutation = useDeleteCrashReportsMutation(appKey);
  const item = query.data;

  const toggleResolved = async (target: CrashReport) => {
    try {
      await resolveMutation.mutateAsync({ ids: [target.id], resolved: !target.resolved });
      toast.success(target.resolved ? "已标记为未处理" : "已标记为已处理");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const remove = async (target: CrashReport) => {
    try {
      await deleteMutation.mutateAsync([target.id]);
      toast.success("已删除");
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  return (
    <Sheet open={reportId != null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[92vw] max-w-4xl flex-col p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle className="pr-8 break-all">{item?.title ?? "崩溃报告"}</SheetTitle>
          <SheetDescription asChild>
            <div className="flex flex-wrap items-center gap-2 text-xs">
              {item ? (
                <>
                  <CrashKindBadge kind={item.kind} />
                  <CrashStatusBadge resolved={item.resolved} />
                  <span className="font-mono text-muted-foreground">{item.publicId}</span>
                  <CopyButton text={item.publicId} label="复制编号" />
                </>
              ) : null}
            </div>
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
          {query.isLoading || !item ? (
            <div className="space-y-3">
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-80 w-full" />
            </div>
          ) : (
            <div className="flex flex-col gap-5">
              <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-xs sm:grid-cols-2">
                <Field label="发生时间" value={formatDateTime(item.occurredAt)} />
                <Field label="上报时间" value={formatDateTime(item.createdAt)} />
                <Field label="应用版本" value={crashVersionLabel(item)} />
                <Field label="系统版本" value={item.osVersion || item.device?.osVersion} />
                <Field label="设备" value={crashDeviceLabel(item)} />
                <Field label="型号" value={item.model} mono />
                <Field label="厂商" value={[item.device?.manufacturer, item.device?.brand].filter(Boolean).join(" / ")} />
                <Field label="ABI" value={item.abi} mono />
                <Field label="进程" value={item.processName} mono />
                <Field label="来源" value={item.source} mono />
                <Field label="客户端编号" value={item.reportId} mono />
                <Field label="设备标识" value={item.deviceId} mono />
                <div className="flex min-w-0 gap-3">
                  <dt className="w-20 shrink-0 text-muted-foreground">账号</dt>
                  <dd className="min-w-0 truncate">
                    {item.userId ? (
                      <Link
                        href={`/app-users/${encodeURIComponent(appKey)}/${item.userId}`}
                        className="font-medium hover:underline"
                      >
                        {item.account || `#${item.userId}`}
                      </Link>
                    ) : (
                      <span className="text-muted-foreground">未登录</span>
                    )}
                    {item.userPublicId ? (
                      <span className="ml-2 font-mono text-muted-foreground">{item.userPublicId}</span>
                    ) : null}
                  </dd>
                </div>
                <Field label="IP" value={item.clientIp} mono />
                <Field label="位置" value={crashLocationLabel(item)} />
                <Field label="处理" value={item.resolved ? `${item.resolvedBy || "—"} ${formatDateTime(item.resolvedAt)}` : ""} />
                <div className="flex min-w-0 gap-3 sm:col-span-2">
                  <dt className="w-20 shrink-0 text-muted-foreground">User-Agent</dt>
                  <dd className="min-w-0 break-all font-mono text-muted-foreground">{item.userAgent || "—"}</dd>
                </div>
              </dl>

              {item.detail ? (
                <section className="flex flex-col gap-2">
                  <h4 className="text-xs font-medium text-muted-foreground">详情</h4>
                  <pre className="max-h-48 overflow-auto rounded-xl border border-border bg-muted/40 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all">
                    {item.detail}
                  </pre>
                </section>
              ) : null}

              <section className="flex min-h-0 flex-col gap-2">
                <div className="flex items-center justify-between gap-2">
                  <h4 className="text-xs font-medium text-muted-foreground">
                    报告 <span className="tabular-nums">{formatBytes(item.reportBytes)}</span>
                  </h4>
                  <div className="flex gap-1.5">
                    <Button size="sm" variant="outline" onClick={() => void copyText(item.report ?? "", "报告已复制")}>
                      <Copy className="size-3.5" />
                      复制
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => downloadCrashReport(item)}>
                      <Download className="size-3.5" />
                      下载
                    </Button>
                  </div>
                </div>
                <pre className="max-h-[60vh] overflow-auto rounded-xl border border-border bg-muted/40 p-3 font-mono text-xs leading-relaxed whitespace-pre">
                  {item.report}
                </pre>
              </section>
            </div>
          )}
        </div>

        {item ? (
          <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button size="sm" variant="outline" className="text-destructive" disabled={deleteMutation.isPending}>
                  <Trash2 className="size-3.5" />
                  删除
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>删除这份崩溃报告？</AlertDialogTitle>
                  <AlertDialogDescription>删除后不可恢复。</AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>取消</AlertDialogCancel>
                  <AlertDialogAction
                    className="bg-destructive text-white hover:bg-destructive/90"
                    onClick={() => void remove(item)}
                  >
                    删除
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
            <Button size="sm" disabled={resolveMutation.isPending} onClick={() => void toggleResolved(item)}>
              {item.resolved ? <RotateCcw className="size-3.5" /> : <CheckCheck className="size-3.5" />}
              {item.resolved ? "标记未处理" : "标记已处理"}
            </Button>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function Field({ label, value, mono }: { label: string; value?: string | null; mono?: boolean }) {
  return (
    <div className="flex min-w-0 gap-3">
      <dt className="w-20 shrink-0 text-muted-foreground">{label}</dt>
      <dd className={mono ? "min-w-0 truncate font-mono" : "min-w-0 truncate"} title={value || undefined}>
        {value || "—"}
      </dd>
    </div>
  );
}
