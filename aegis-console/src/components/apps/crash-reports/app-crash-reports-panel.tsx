"use client";

import { useMemo, useState } from "react";
import { Bug, CheckCheck, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";
import { CrashReportDetailSheet } from "@/components/apps/crash-reports/crash-report-detail-sheet";
import {
  CRASH_KINDS,
  CRASH_KIND_META,
  CrashKindBadge,
  CrashStatusBadge,
  crashDeviceLabel,
  crashVersionLabel
} from "@/components/apps/crash-reports/crash-report-shared";
import { CommerceRangePicker, type CommerceRange } from "@/components/commerce/commerce-range-picker";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ApiError } from "@/lib/api-client";
import type { CrashReportKind } from "@/lib/api/crash-reports";
import {
  useCrashReportFacetsQuery,
  useCrashReportsQuery,
  useDeleteCrashReportsMutation,
  useResolveCrashReportsMutation
} from "@/lib/crash-report-hooks";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const PAGE_SIZE = 20;
type StatusFilter = "all" | "open" | "resolved";

/**
 * 崩溃报告（/apps/{appKey}?tab=crash-reports）：一页完成，不分标签。
 *
 * 摘要（总数 / 未处理 / 近 24 小时）在标题栏右侧；筛选的版本下拉只列真实出现过的版本（facets 接口）。
 * 列表不带正文，点行打开详情抽屉再取。选择只作用于当前页，翻页或改筛选时清空。
 */
export function AppCrashReportsPanel({ appKey }: { appKey: string }) {
  const [keyword, setKeyword] = useState("");
  const [kind, setKind] = useState<CrashReportKind | undefined>(undefined);
  const [appVersion, setAppVersion] = useState<string | undefined>(undefined);
  const [status, setStatus] = useState<StatusFilter>("all");
  const [range, setRange] = useState<CommerceRange>({});
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<number[]>([]);
  const [openId, setOpenId] = useState<number | null>(null);
  const debouncedKeyword = useDebouncedValue(keyword, 300);

  const facetsQuery = useCrashReportFacetsQuery(appKey);
  const listQuery = useCrashReportsQuery(appKey, {
    kind,
    appVersion,
    keyword: debouncedKeyword || undefined,
    resolved: status === "all" ? undefined : status === "resolved",
    start: range.start,
    end: range.end,
    page,
    limit: PAGE_SIZE
  });
  const resolveMutation = useResolveCrashReportsMutation(appKey);
  const deleteMutation = useDeleteCrashReportsMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? Math.ceil(total / PAGE_SIZE));
  const filtered = Boolean(keyword || kind || appVersion || status !== "all" || range.start || range.end);
  const facets = facetsQuery.data;
  const kindCount = (value: CrashReportKind) => facets?.kinds.find((item) => item.kind === value)?.count ?? 0;

  const visibleSelected = selected.filter((id) => items.some((item) => item.id === id));
  const allSelected = items.length > 0 && visibleSelected.length === items.length;

  // 改筛选或翻页时清空选择，避免批量操作落到看不见的行上
  const resetPaging = () => {
    setPage(1);
    setSelected([]);
  };

  const batchResolve = async (resolved: boolean) => {
    try {
      const result = await resolveMutation.mutateAsync({ ids: visibleSelected, resolved });
      toast.success(`已更新 ${result.updated} 条`);
      setSelected([]);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const batchDelete = async () => {
    try {
      const result = await deleteMutation.mutateAsync(visibleSelected);
      toast.success(`已删除 ${result.deleted} 条`);
      setSelected([]);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  const summary = facets ? (
    <div className="flex items-center gap-4 text-xs text-muted-foreground">
      <span>
        总数 <span className="font-medium text-foreground tabular-nums">{facets.total}</span>
      </span>
      <span>
        未处理 <span className="font-medium text-foreground tabular-nums">{facets.unresolved}</span>
      </span>
      <span>
        近 24 小时 <span className="font-medium text-foreground tabular-nums">{facets.last24h}</span>
      </span>
    </div>
  ) : null;

  return (
    <>
      <SectionCard icon={<Bug className="size-4" />} title="崩溃报告" aside={summary}>
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <Input
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              resetPaging();
            }}
            placeholder="搜索标题、详情或编号"
            className="h-8 w-56"
          />
          <Select
            value={kind ?? "all"}
            onValueChange={(value) => {
              setKind(value === "all" ? undefined : (value as CrashReportKind));
              resetPaging();
            }}
          >
            <SelectTrigger className="h-8 w-36">
              <SelectValue placeholder="全部类型" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部类型</SelectItem>
              {CRASH_KINDS.map((value) => (
                <SelectItem key={value} value={value}>
                  {CRASH_KIND_META[value].label}
                  {facets ? <span className="ml-1 text-muted-foreground tabular-nums">{kindCount(value)}</span> : null}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={appVersion ?? "all"}
            onValueChange={(value) => {
              setAppVersion(value === "all" ? undefined : value);
              resetPaging();
            }}
          >
            <SelectTrigger className="h-8 w-40">
              <SelectValue placeholder="全部版本" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部版本</SelectItem>
              {(facets?.versions ?? []).map((item) => (
                <SelectItem key={item.appVersion} value={item.appVersion}>
                  {item.appVersion}
                  <span className="ml-1 text-muted-foreground tabular-nums">{item.count}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={status}
            onValueChange={(value) => {
              setStatus(value as StatusFilter);
              resetPaging();
            }}
          >
            <SelectTrigger className="h-8 w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="open">未处理</SelectItem>
              <SelectItem value="resolved">已处理</SelectItem>
            </SelectContent>
          </Select>
          <CommerceRangePicker
            value={range}
            onChange={(next) => {
              setRange(next);
              resetPaging();
            }}
          />
        </div>

        {visibleSelected.length > 0 ? (
          <div className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-border bg-muted/40 px-3 py-2 text-xs">
            <span className="tabular-nums">已选 {visibleSelected.length} 条</span>
            <div className="ml-auto flex flex-wrap gap-1.5">
              <Button size="sm" variant="outline" disabled={resolveMutation.isPending} onClick={() => void batchResolve(true)}>
                <CheckCheck className="size-3.5" />
                标记已处理
              </Button>
              <Button size="sm" variant="outline" disabled={resolveMutation.isPending} onClick={() => void batchResolve(false)}>
                <RotateCcw className="size-3.5" />
                标记未处理
              </Button>
              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button size="sm" variant="outline" className="text-destructive" disabled={deleteMutation.isPending}>
                    <Trash2 className="size-3.5" />
                    删除
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>删除 {visibleSelected.length} 份崩溃报告？</AlertDialogTitle>
                    <AlertDialogDescription>删除后不可恢复。</AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>取消</AlertDialogCancel>
                    <AlertDialogAction
                      className="bg-destructive text-white hover:bg-destructive/90"
                      onClick={() => void batchDelete()}
                    >
                      删除
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </div>
          </div>
        ) : null}

        {listQuery.isLoading ? (
          <div className="space-y-2">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            {filtered ? "暂无匹配记录" : "暂无崩溃报告"}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8">
                  <Checkbox
                    checked={allSelected}
                    onCheckedChange={(value) => setSelected(value === true ? items.map((item) => item.id) : [])}
                    aria-label="选择本页"
                  />
                </TableHead>
                <TableHead>报告</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>版本</TableHead>
                <TableHead>设备</TableHead>
                <TableHead>账号</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">发生时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => {
                const checked = visibleSelected.includes(item.id);
                return (
                  <TableRow
                    key={item.id}
                    className="cursor-pointer"
                    data-state={checked ? "selected" : undefined}
                    onClick={() => setOpenId(item.id)}
                  >
                    <TableCell onClick={(event) => event.stopPropagation()}>
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(value) =>
                          setSelected((current) =>
                            value === true ? [...current, item.id] : current.filter((id) => id !== item.id)
                          )
                        }
                        aria-label="选择"
                      />
                    </TableCell>
                    <TableCell className="max-w-80">
                      <p className="truncate text-xs font-medium" title={item.title}>
                        {item.title}
                      </p>
                      <p className="mt-0.5 truncate font-mono text-[11px] text-muted-foreground" title={item.detail || undefined}>
                        {item.detail || item.publicId}
                      </p>
                    </TableCell>
                    <TableCell>
                      <CrashKindBadge kind={item.kind} />
                    </TableCell>
                    <TableCell className="text-xs tabular-nums text-muted-foreground">
                      {crashVersionLabel(item) || "—"}
                    </TableCell>
                    <TableCell className="max-w-44 text-xs">
                      <p className="truncate">{crashDeviceLabel(item) || "—"}</p>
                      {item.osVersion ? (
                        <p className="mt-0.5 text-[11px] text-muted-foreground">系统 {item.osVersion}</p>
                      ) : null}
                    </TableCell>
                    <TableCell className="max-w-36 truncate text-xs">
                      {item.userId ? (
                        item.account || `#${item.userId}`
                      ) : (
                        <span className="text-muted-foreground">未登录</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <CrashStatusBadge resolved={item.resolved} />
                    </TableCell>
                    <TableCell className="text-right text-xs whitespace-nowrap text-muted-foreground">
                      {formatDateTimeMinute(item.occurredAt)}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}

        <AdPolicyPager
          total={total}
          page={page}
          totalPages={totalPages}
          onPage={(next) => {
            setPage(next);
            setSelected([]);
          }}
        />
      </SectionCard>

      <CrashReportDetailSheet
        appKey={appKey}
        reportId={openId}
        onOpenChange={(open) => {
          if (!open) setOpenId(null);
        }}
      />
    </>
  );
}
