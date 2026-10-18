"use client";

import { useMemo } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { ArrowRight, ArrowUpRight, Boxes, Database, HardDrive, Layers, PenLine, Star, TriangleAlert } from "lucide-react";
import { buildChartConfig, ChartCard } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import type { StorageOverview } from "@/lib/api/storage-center";
import { useStorageEventsQuery, useStorageProvidersQuery } from "@/lib/storage-center-hooks";
import { StorageEventList } from "./storage-events-panel";
import {
  STATE_META,
  STRATEGY_META,
  StateBadge,
  StorageBrandBadge,
  UsageMeter,
  formatSize,
  providerMeta
} from "./storage-shared";

const USAGE_CHART = buildChartConfig([{ key: "used", label: "已用", color: { light: "#0ea5e9", dark: "#38bdf8" } }]);

export function StorageOverviewPanel({
  overview,
  loading,
  onNavigate
}: {
  overview?: StorageOverview;
  loading: boolean;
  onNavigate: (tab: string) => void;
}) {
  const providers = useStorageProvidersQuery().data;
  const events = useStorageEventsQuery({ limit: 8 });

  const configs = useMemo(() => overview?.configs ?? [], [overview?.configs]);
  const troubled = configs.filter(
    (item) => item.config.enabled && (item.state === "backoff" || item.state === "full" || item.state === "degraded")
  );
  const chartRows = useMemo(
    () =>
      [...configs]
        .filter((item) => (item.usage?.usedBytes ?? 0) > 0)
        .sort((a, b) => (b.usage?.usedBytes ?? 0) - (a.usage?.usedBytes ?? 0))
        .slice(0, 10)
        .map((item) => ({ name: item.config.config_name, used: item.usage?.usedBytes ?? 0 })),
    [configs]
  );
  const activeMigrations = (overview?.migrations ?? []).filter((item) => item.status === "running" || item.status === "pending");
  const target = overview?.writeTarget;
  const targetMeta = target?.kind === "config" ? providerMeta(providers, target.provider) : undefined;

  if (loading) {
    return (
      <div className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[88px] rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-64 rounded-2xl" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile
          label="存储源"
          icon={Database}
          value={overview?.totals.configs ?? 0}
          hint={`正常 ${overview?.totals.healthy ?? 0} · 异常 ${overview?.totals.unhealthy ?? 0}`}
        />
        <StatTile
          label="已用空间"
          icon={HardDrive}
          value={formatSize(overview?.totals.usedBytes ?? 0)}
          hint={overview?.totals.capacityBytes ? `容量上限 ${formatSize(overview.totals.capacityBytes)}` : undefined}
        />
        <StatTile label="对象" icon={Boxes} value={(overview?.totals.objects ?? 0).toLocaleString()} />
        <button
          type="button"
          className="rounded-xl border bg-card px-4 py-3 text-left transition-colors hover:border-foreground/20"
          onClick={() => onNavigate(target?.kind === "pool" ? "pools" : "global")}
        >
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <PenLine className="size-3.5" />
            默认写入
          </div>
          <div className="mt-1 flex items-center gap-2">
            {target?.kind === "pool" ? (
              <span className="grid size-7 place-items-center rounded-lg bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
                <Layers className="size-3.5" />
              </span>
            ) : (
              <StorageBrandBadge meta={targetMeta} provider={target?.provider} size="sm" />
            )}
            <span className="truncate text-lg font-semibold tracking-tight">{target?.name ?? "—"}</span>
          </div>
          <div className="mt-0.5 text-[11px] text-muted-foreground">{target?.kind === "pool" ? "存储池" : "存储源"}</div>
        </button>
      </div>

      {troubled.length > 0 ? (
        <Alert variant="destructive">
          <TriangleAlert className="size-4" />
          <AlertTitle>{troubled.length} 个存储源异常</AlertTitle>
          <AlertDescription className="flex flex-wrap items-center gap-1.5">
            {troubled.map((item) => (
              <Badge key={item.config.id} variant={STATE_META[item.state].variant} size="sm">
                {item.config.config_name} · {STATE_META[item.state].label}
              </Badge>
            ))}
            <Button size="xs" variant="ghost" className="ml-auto" onClick={() => onNavigate("events")}>
              查看事件
              <ArrowRight />
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      {activeMigrations.length > 0 ? (
        <div className="space-y-2 rounded-2xl border bg-card p-4">
          <div className="flex items-center justify-between text-sm font-semibold">
            迁移进行中
            <Button size="xs" variant="ghost" onClick={() => onNavigate("migrations")}>
              详情
              <ArrowUpRight />
            </Button>
          </div>
          {activeMigrations.map((item) => {
            const done = item.copiedObjects + item.skippedObjects + item.failedObjects;
            return (
              <div key={item.id} className="space-y-1">
                <div className="flex items-center gap-1.5 text-xs">
                  {item.sourceName}
                  <ArrowRight className="size-3 text-muted-foreground" />
                  {item.targetName}
                  <span className="ml-auto tabular-nums text-muted-foreground">
                    {done.toLocaleString()} / {item.totalObjects.toLocaleString()}
                  </span>
                </div>
                <Progress value={item.totalObjects ? (done / item.totalObjects) * 100 : 0} className="h-1.5" />
              </div>
            );
          })}
        </div>
      ) : null}

      <div className="grid gap-4 xl:grid-cols-5">
        <div className="xl:col-span-3">
          <ChartCard title="各存储用量" empty={chartRows.length === 0} height={260}>
            <ChartContainer config={USAGE_CHART} className="h-[260px] w-full">
              <BarChart data={chartRows} layout="vertical" margin={{ left: 8, right: 16, top: 4 }}>
                <CartesianGrid horizontal={false} strokeDasharray="3 3" />
                <XAxis type="number" tickLine={false} axisLine={false} fontSize={11} tickFormatter={(value) => formatSize(Number(value))} />
                <YAxis type="category" dataKey="name" tickLine={false} axisLine={false} fontSize={11} width={96} />
                <ChartTooltip content={<ChartTooltipContent formatter={(value) => formatSize(Number(value))} />} />
                <Bar dataKey="used" fill="var(--color-used)" radius={[0, 4, 4, 0]} maxBarSize={22} />
              </BarChart>
            </ChartContainer>
          </ChartCard>
        </div>
        <div className="rounded-2xl border bg-card xl:col-span-2">
          <div className="flex items-center justify-between border-b px-4 py-3 text-sm font-semibold">
            最近事件
            <Button size="xs" variant="ghost" onClick={() => onNavigate("events")}>
              全部
              <ArrowUpRight />
            </Button>
          </div>
          <div className="max-h-[296px] overflow-y-auto p-2">
            {events.isLoading ? <Skeleton className="h-40 rounded-xl" /> : <StorageEventList events={events.data ?? []} compact />}
          </div>
        </div>
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">存储池</div>
          <Button size="xs" variant="ghost" onClick={() => onNavigate("pools")}>
            管理
            <ArrowUpRight />
          </Button>
        </div>
        {(overview?.pools ?? []).length === 0 ? (
          <button
            type="button"
            onClick={() => onNavigate("pools")}
            className="flex w-full items-center justify-center gap-2 rounded-2xl border border-dashed py-8 text-xs text-muted-foreground hover:text-foreground"
          >
            <Layers className="size-4" />
            暂无存储池
          </button>
        ) : (
          <div className="grid gap-3 lg:grid-cols-2">
            {(overview?.pools ?? []).map((pool) => (
              <div key={pool.id} className="space-y-3 rounded-2xl border bg-card p-4">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-sm font-semibold">{pool.name}</span>
                  {pool.isDefault ? (
                    <Badge variant="info" size="sm" className="gap-1">
                      <Star className="size-3" />
                      默认
                    </Badge>
                  ) : null}
                  <span className="ml-auto text-[11px] text-muted-foreground">{STRATEGY_META[pool.strategy].label}</span>
                </div>
                <div className="space-y-2.5">
                  {[...pool.members]
                    .sort((a, b) => a.priority - b.priority)
                    .map((member) => (
                      <div key={member.configId} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_auto] items-center gap-3">
                        <div className="flex min-w-0 items-center gap-2">
                          <StorageBrandBadge
                            meta={providerMeta(providers, member.config?.provider ?? "")}
                            provider={member.config?.provider}
                            size="sm"
                            className="size-6 rounded-md"
                          />
                          <span className="truncate text-xs font-medium">{member.config?.name ?? `#${member.configId}`}</span>
                        </div>
                        <UsageMeter used={member.usage?.usedBytes ?? 0} capacity={member.capacityBytes} warnPercent={pool.warnPercent} />
                        <StateBadge state={member.state} />
                      </div>
                    ))}
                  {pool.members.length === 0 ? <div className="text-xs text-muted-foreground">暂无成员</div> : null}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
