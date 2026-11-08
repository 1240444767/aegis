"use client";

import { useState } from "react";
import { toast } from "sonner";
import { RefreshCw, Search, Trash2 } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { ClientInstall } from "@/lib/api/client-control";
import {
  useClientControlDevicesQuery,
  useClientControlVersionsQuery,
  useDeleteClientControlDeviceMutation
} from "@/lib/client-control-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { ActionBadge, CONTROL_ACTIONS, DATA_FONT, reasonLabel, shortCert } from "./client-control-shared";

const DAY_OPTIONS = [
  { value: "1", label: "近 1 天" },
  { value: "7", label: "近 7 天" },
  { value: "30", label: "近 30 天" },
  { value: "90", label: "近 90 天" }
];

/**
 * 版本分布：按包名、版本号、签名、平台聚合。「当前规则」一栏按现在的规则重新估算，
 * 只看得到这几项，用到安装来源、设备、用户的规则以客户端下次上报的结果为准。
 */
export function ClientControlVersions({
  appKey,
  officialCerts,
  onShowDevices
}: {
  appKey: string;
  officialCerts: string[];
  onShowDevices: (filter: { packageName: string; versionCode: number; certSha256: string }) => void;
}) {
  const [days, setDays] = useState("30");
  const query = useClientControlVersionsQuery(appKey, Number(days));
  const groups = query.data ?? [];

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Select value={days} onValueChange={setDays}>
          <SelectTrigger className="h-8 w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {DAY_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex-1" />
        <Button variant="outline" size="sm" className="h-8" aria-label="刷新" onClick={() => void query.refetch()}>
          <RefreshCw className={cn("size-3.5", query.isFetching && "animate-spin")} />
        </Button>
      </div>
      <div className="overflow-hidden rounded-xl border bg-card">
        <TooltipProvider delayDuration={200}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>包名与版本</TableHead>
                <TableHead className="hidden md:table-cell">签名</TableHead>
                <TableHead className="text-right">设备</TableHead>
                <TableHead className="hidden lg:table-cell text-right">上报</TableHead>
                <TableHead className="hidden lg:table-cell text-right">被拦</TableHead>
                <TableHead>当前规则</TableHead>
                <TableHead className="hidden xl:table-cell">最近上报</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.isLoading ? (
                Array.from({ length: 4 }, (_, index) => (
                  <TableRow key={index}>
                    {Array.from({ length: 7 }, (_, cell) => (
                      <TableCell key={cell} className={cn(cell >= 3 && "hidden lg:table-cell")}>
                        <Skeleton className="h-4 w-full max-w-24" />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : groups.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="py-12 text-center text-sm text-muted-foreground">
                    这段时间内没有客户端上报
                  </TableCell>
                </TableRow>
              ) : (
                groups.map((group) => {
                  const official = officialCerts.length === 0 || officialCerts.includes(group.certSha256);
                  return (
                    <TableRow
                      key={`${group.packageName}-${group.versionCode}-${group.certSha256}-${group.platform}`}
                      className="cursor-pointer"
                      onClick={() =>
                        onShowDevices({ packageName: group.packageName, versionCode: group.versionCode, certSha256: group.certSha256 })
                      }
                    >
                      <TableCell>
                        <div className="font-medium">
                          <span className={DATA_FONT}>{group.versionName || "—"}</span>
                          <span className={cn("ml-2 text-xs text-muted-foreground", DATA_FONT)}>{group.versionCode}</span>
                        </div>
                        <div className="max-w-64 truncate text-xs text-muted-foreground">
                          {group.packageName}
                          {group.platform ? `，${group.platform}` : ""}
                        </div>
                      </TableCell>
                      <TableCell className="hidden md:table-cell">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className={cn("text-xs", DATA_FONT, !official && "text-red-600 dark:text-red-400")}>
                              {shortCert(group.certSha256)}
                            </span>
                          </TooltipTrigger>
                          <TooltipContent className={DATA_FONT}>{group.certSha256 || "未上报"}</TooltipContent>
                        </Tooltip>
                      </TableCell>
                      <TableCell className={cn("text-right", DATA_FONT)}>{group.devices}</TableCell>
                      <TableCell className={cn("hidden lg:table-cell text-right text-muted-foreground", DATA_FONT)}>{group.reports}</TableCell>
                      <TableCell className={cn("hidden lg:table-cell text-right", DATA_FONT)}>{group.blocked}</TableCell>
                      <TableCell>
                        <div className="flex flex-col items-start gap-0.5">
                          <ActionBadge value={group.decision?.action} size="sm" />
                          <span className="text-[11px] text-muted-foreground">
                            {group.decision?.ruleName || reasonLabel(group.decision?.reason)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="hidden xl:table-cell text-xs text-muted-foreground">{formatRelative(group.lastSeenAt)}</TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </TooltipProvider>
      </div>
    </div>
  );
}

export type DeviceFilter = { packageName?: string; versionCode?: number; certSha256?: string };

/** 设备清单：每台设备上每个包名一行，记录最近一次上报与当时拿到的裁决。 */
export function ClientControlDevices({
  appKey,
  filter,
  onClearFilter
}: {
  appKey: string;
  filter: DeviceFilter;
  onClearFilter: () => void;
}) {
  const [page, setPage] = useState(1);
  const [action, setAction] = useState("all");
  const [keywordInput, setKeywordInput] = useState("");
  const keyword = useDebouncedValue(keywordInput.trim(), 300);
  const query = useClientControlDevicesQuery(appKey, {
    ...filter,
    keyword: keyword || undefined,
    action: action === "all" ? undefined : action,
    page,
    limit: 20
  });
  const remove = useDeleteClientControlDeviceMutation(appKey);
  const items = query.data?.items ?? [];
  const totalPages = Math.max(1, query.data?.totalPages ?? 1);
  const filtered = Boolean(filter.packageName || filter.versionCode || filter.certSha256);

  async function removeItem(item: ClientInstall) {
    try {
      await remove.mutateAsync({ deviceId: item.deviceId, packageName: item.packageName });
      toast.success("已删除记录");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-64">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8 text-xs"
            value={keywordInput}
            placeholder="设备 ID、型号、IP、包名、版本名"
            onChange={(event) => {
              setKeywordInput(event.target.value);
              setPage(1);
            }}
          />
        </div>
        <Select
          value={action}
          onValueChange={(value) => {
            setAction(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部结果</SelectItem>
            {CONTROL_ACTIONS.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {filtered ? (
          <Button variant="secondary" size="sm" className="h-8 text-xs" onClick={onClearFilter}>
            {filter.packageName} {filter.versionCode ? `版本号 ${filter.versionCode}` : ""} {filter.certSha256 ? shortCert(filter.certSha256) : ""}
            <span className="ml-1 text-muted-foreground">清除</span>
          </Button>
        ) : null}
        <div className="flex-1" />
        <span className="text-xs text-muted-foreground">共 {query.data?.total ?? 0} 条</span>
      </div>

      <div className="overflow-hidden rounded-xl border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>设备</TableHead>
              <TableHead>版本</TableHead>
              <TableHead className="hidden lg:table-cell">安装来源</TableHead>
              <TableHead>最近裁决</TableHead>
              <TableHead className="hidden md:table-cell">最近上报</TableHead>
              <TableHead className="w-10" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.isLoading ? (
              Array.from({ length: 6 }, (_, index) => (
                <TableRow key={index}>
                  {Array.from({ length: 6 }, (_, cell) => (
                    <TableCell key={cell} className={cn(cell >= 4 && "hidden md:table-cell")}>
                      <Skeleton className="h-4 w-full max-w-24" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="py-12 text-center text-sm text-muted-foreground">
                  {keyword || filtered || action !== "all" ? "没有符合条件的设备" : "暂无设备上报"}
                </TableCell>
              </TableRow>
            ) : (
              items.map((item) => (
                <TableRow key={`${item.deviceId}-${item.packageName}`}>
                  <TableCell>
                    <div className="max-w-56 truncate font-medium">{[item.brand, item.model].filter(Boolean).join(" ") || "未知设备"}</div>
                    <div className={cn("max-w-56 truncate text-xs text-muted-foreground", DATA_FONT)} title={item.deviceId}>
                      {item.deviceId}
                    </div>
                    <div className="text-[11px] text-muted-foreground">
                      {item.platform}
                      {item.osVersion ? ` API ${item.osVersion}` : ""}
                      {item.abi ? `，${item.abi}` : ""}
                      {item.userId ? `，用户 ${item.userId}` : ""}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className={DATA_FONT}>
                      {item.versionName || "—"} <span className="text-xs text-muted-foreground">{item.versionCode}</span>
                    </div>
                    <div className="max-w-48 truncate text-xs text-muted-foreground">{item.packageName}</div>
                    <div className={cn("text-[11px] text-muted-foreground", DATA_FONT)} title={item.certSha256}>
                      {shortCert(item.certSha256)}
                    </div>
                  </TableCell>
                  <TableCell className="hidden lg:table-cell text-xs text-muted-foreground">{item.installer || "未知"}</TableCell>
                  <TableCell>
                    <div className="flex flex-col items-start gap-0.5">
                      <ActionBadge value={item.lastAction} size="sm" />
                      <span className="text-[11px] text-muted-foreground">{reasonLabel(item.lastReason)}</span>
                    </div>
                  </TableCell>
                  <TableCell className="hidden md:table-cell text-xs text-muted-foreground">
                    <div title={formatDateTime(item.lastSeenAt)}>{formatRelative(item.lastSeenAt)}</div>
                    <div>
                      共 <span className={DATA_FONT}>{item.reportCount}</span> 次{item.ip ? `，${item.ip}` : ""}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Button variant="ghost" size="icon" className="size-8" aria-label="删除记录" onClick={() => removeItem(item)}>
                      <Trash2 className="size-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {totalPages > 1 ? (
        <div className="flex items-center justify-center gap-2">
          <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
            上一页
          </Button>
          <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>
            {page} / {totalPages}
          </span>
          <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
            下一页
          </Button>
        </div>
      ) : null}
    </div>
  );
}
