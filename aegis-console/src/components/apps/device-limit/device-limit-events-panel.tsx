"use client";

import { useMemo, useState } from "react";
import { ArrowRight, ListTree, Search, X } from "lucide-react";
import {
  ACTION_META,
  ActionBadge,
  ClassIcon,
  classOf,
  DeviceLimitPager,
  DIMENSION_LABEL,
  EVENT_ACTIONS
} from "@/components/apps/device-limit/device-limit-shared";
import type { PickedUser } from "@/components/commerce/user-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { DeviceDimension, DeviceLimitAction } from "@/lib/api/device-limit";
import { formatDateTime, formatRelative, startOfLocalDay } from "@/lib/datetime";
import { useDeviceLimitEventsQuery } from "@/lib/device-limit-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;
const RANGES = [
  { key: "today", label: "今天", days: 0 },
  { key: "7", label: "7 天", days: 7 },
  { key: "30", label: "30 天", days: 30 },
  { key: "all", label: "全部", days: -1 }
] as const;
type RangeKey = (typeof RANGES)[number]["key"];

/** 处置记录：每一次自动下线、拒绝、选择与人工下线。筛选任何一项变化都回到第 1 页。 */
export function DeviceLimitEventsPanel({
  appKey,
  onOpenUser
}: {
  appKey: string;
  onOpenUser: (user: PickedUser) => void;
}) {
  const [action, setAction] = useState<DeviceLimitAction | "all">("all");
  const [dimension, setDimension] = useState<DeviceDimension | "all">("all");
  const [range, setRange] = useState<RangeKey>("7");
  const [keyword, setKeyword] = useState("");
  const [user, setUser] = useState<PickedUser | null>(null);
  const [page, setPage] = useState(1);
  const debounced = useDebouncedValue(keyword.trim(), 300);

  // 时间窗的起点在选中那一刻定下，不随每次渲染漂移（否则查询键每次都变）
  const [rangeStart, setRangeStart] = useState<string | undefined>(() => startOfDaysAgo(7));
  const params = useMemo(
    () => ({
      action: action === "all" ? undefined : action,
      dimension: dimension === "all" ? undefined : dimension,
      userId: user?.id,
      keyword: debounced || undefined,
      from: rangeStart,
      page,
      limit: LIMIT
    }),
    [action, dimension, user, debounced, rangeStart, page]
  );
  const query = useDeviceLimitEventsQuery(appKey, params);
  const items = query.data?.items ?? [];

  const reset = <T,>(setter: (value: T) => void) => (value: T) => {
    setter(value);
    setPage(1);
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={action} onValueChange={reset((value: string) => setAction(value as DeviceLimitAction | "all"))}>
          <SelectTrigger size="sm" className="w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all" className="text-xs">
              全部处置
            </SelectItem>
            {EVENT_ACTIONS.map((item) => {
              const Icon = ACTION_META[item].icon;
              return (
                <SelectItem key={item} value={item} className="text-xs">
                  <Icon className="size-3.5" />
                  {ACTION_META[item].label}
                </SelectItem>
              );
            })}
          </SelectContent>
        </Select>
        <Select value={dimension} onValueChange={reset((value: string) => setDimension(value as DeviceDimension | "all"))}>
          <SelectTrigger size="sm" className="w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all" className="text-xs">
              全部维度
            </SelectItem>
            {(Object.keys(DIMENSION_LABEL) as DeviceDimension[]).map((item) => (
              <SelectItem key={item} value={item} className="text-xs">
                {DIMENSION_LABEL[item]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={range}
          onValueChange={(value) => {
            if (!value) return;
            const next = RANGES.find((item) => item.key === value);
            setRange(value as RangeKey);
            setRangeStart(next && next.days >= 0 ? startOfDaysAgo(next.days) : undefined);
            setPage(1);
          }}
        >
          {RANGES.map((item) => (
            <ToggleGroupItem key={item.key} value={item.key} className="h-8 px-2.5 text-xs">
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <div className="relative min-w-48 flex-1 sm:max-w-64">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setPage(1);
            }}
            placeholder="账号、设备或 IP"
            className="h-8 pl-8 text-xs"
          />
        </div>
        {user ? (
          <Badge variant="secondary" className="gap-1 pr-1">
            {user.account || `UID-${user.id}`}
            <button type="button" aria-label="清除" onClick={() => reset(setUser)(null)} className="rounded-full p-0.5 hover:bg-background">
              <X className="size-3" />
            </button>
          </Badge>
        ) : null}
      </div>

      <div className="rounded-xl border border-border bg-card">
        {query.isLoading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2, 3, 4].map((index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-14 text-center">
            <ListTree className="size-6 text-muted-foreground/50" />
            <span className="text-sm text-muted-foreground">暂无记录</span>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-36">时间</TableHead>
                <TableHead>用户</TableHead>
                <TableHead>处置</TableHead>
                <TableHead>设备</TableHead>
                <TableHead>维度</TableHead>
                <TableHead>来源</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((event) => {
                const cls = classOf(event.class);
                return (
                  <TableRow key={event.id}>
                    <TableCell className="text-xs">
                      <div>{formatRelative(event.createdAt)}</div>
                      <div className="text-[10px] text-muted-foreground">{formatDateTime(event.createdAt)}</div>
                    </TableCell>
                    <TableCell>
                      <button
                        type="button"
                        className="text-left text-xs font-medium hover:underline"
                        onClick={() => {
                          setUser({ id: event.userId, account: event.account });
                          setPage(1);
                        }}
                        title="只看该用户"
                      >
                        {event.account || `UID-${event.userId}`}
                      </button>
                      <Button
                        size="xs"
                        variant="link"
                        className="h-auto px-0 text-[10px] text-muted-foreground"
                        onClick={() => onOpenUser({ id: event.userId, account: event.account })}
                      >
                        查看设备
                      </Button>
                    </TableCell>
                    <TableCell>
                      <ActionBadge action={event.action} />
                    </TableCell>
                    <TableCell className="max-w-72">
                      <div className="flex items-center gap-1.5 text-xs">
                        <ClassIcon cls={cls} className="size-3.5 shrink-0 text-muted-foreground" />
                        <span className="truncate">{event.deviceName || "未知设备"}</span>
                      </div>
                      {event.triggerName ? (
                        <div className="mt-0.5 flex items-center gap-1 text-[10px] text-muted-foreground">
                          <ArrowRight className="size-3 shrink-0" />
                          <span className="truncate">{event.triggerName} 登录</span>
                        </div>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-xs">
                      {event.dimension ? (
                        <span>
                          {DIMENSION_LABEL[event.dimension as DeviceDimension] ?? event.dimension}
                          {event.limit ? <span className="text-muted-foreground"> 上限 {event.limit}</span> : null}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                      {event.detail?.sourceLabel ? (
                        <div className="text-[10px] text-muted-foreground">{event.detail.sourceLabel}</div>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-[11px] text-muted-foreground">
                      {event.operator ? <div>{event.operator}</div> : null}
                      {event.ip ? <div className="font-mono">{event.ip}</div> : null}
                      {!event.operator && !event.ip ? "—" : null}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </div>
      <DeviceLimitPager total={query.data?.total ?? 0} page={page} limit={LIMIT} onPage={setPage} />
    </div>
  );
}

/** 今天 0 点往前推 days 天（浏览器时区）。0 即今天 0 点 */
function startOfDaysAgo(days: number) {
  const start = startOfLocalDay();
  start.setDate(start.getDate() - days);
  return start.toISOString();
}
