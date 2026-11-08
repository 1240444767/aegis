"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { AlarmClock, CheckCircle2, Clock, Plus, Star, Timer } from "lucide-react";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import { buildChartConfig, ChartCard } from "@/components/commerce/commerce-charts";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useTicketAgentStatsQuery, useTicketStatsQuery, useTicketTrendQuery } from "@/lib/ticket-hooks";
import type { TicketKind, TicketStats } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { PersonAvatar, formatDuration } from "../ticket-shared";

// 服务台统计：页头的指标条，以及工单中心「统计分析」页。

const TREND_CONFIG = buildChartConfig([
  { key: "created", label: "新建", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "resolved", label: "解决", color: { light: "#10b981", dark: "#34d399" } },
  { key: "closed", label: "关闭", color: { light: "#a1a1aa", dark: "#71717a" } }
]);

const CATEGORY_CONFIG = buildChartConfig([{ key: "count", label: "数量", color: { light: "#0ea5e9", dark: "#38bdf8" } }]);

/** 页头指标条：今天的量、响应速度、满意度 */
export function TicketKpiStrip({ stats, loading }: { stats?: TicketStats; loading?: boolean }) {
  const overdue = stats ? stats.overdueFirstResponse + stats.overdueResolve : 0;
  const items = [
    { key: "today", label: "今日新增", value: stats ? String(stats.createdToday) : "", icon: Plus },
    { key: "solved", label: "今日解决", value: stats ? String(stats.resolvedToday) : "", icon: CheckCircle2 },
    { key: "first", label: "平均首响", value: stats ? formatDuration(stats.avgFirstResponseMs) : "", icon: Timer },
    { key: "resolve", label: "平均解决", value: stats ? formatDuration(stats.avgResolveMs) : "", icon: Clock },
    {
      key: "rating",
      label: "满意度",
      value: stats ? (stats.ratingCount ? `${stats.avgRating.toFixed(1)}（${stats.ratingCount} 评）` : "暂无评价") : "",
      icon: Star
    },
    { key: "overdue", label: "已超时", value: stats ? String(overdue) : "", icon: AlarmClock, danger: overdue > 0 }
  ];

  return (
    <div className="grid grid-cols-2 gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-3 xl:grid-cols-6">
      {items.map((item) => (
        <div key={item.key} className="flex items-center gap-3 bg-card px-4 py-3">
          <span
            className={cn(
              "flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground",
              item.danger && "bg-destructive/10 text-destructive"
            )}
          >
            <item.icon className="size-4" />
          </span>
          <div className="min-w-0">
            <p className="text-xs text-muted-foreground">{item.label}</p>
            {loading || !stats ? (
              <Skeleton className="mt-1 h-5 w-12" />
            ) : (
              <p className={cn("truncate text-base font-semibold tabular-nums", item.danger ? "text-destructive" : "text-foreground")}>
                {item.value}
              </p>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

export function TicketAnalytics({ kind = "ticket" }: { kind?: TicketKind }) {
  const [days, setDays] = useState(30);
  const statsQuery = useTicketStatsQuery(undefined, kind);
  const trendQuery = useTicketTrendQuery(days);
  const agentsQuery = useTicketAgentStatsQuery(20);

  const stats = statsQuery.data;
  const trend = useMemo(
    () =>
      (trendQuery.data ?? []).map((point) => ({
        ...point,
        label: point.date.slice(5).replace("-", "/")
      })),
    [trendQuery.data]
  );
  const trendEmpty = trend.every((point) => !point.created && !point.resolved && !point.closed);

  const categories = useMemo(
    () => [...(stats?.byCategory ?? [])].sort((a, b) => b.count - a.count).slice(0, 8),
    [stats?.byCategory]
  );
  const priorityTotal = Object.values(stats?.byPriority ?? {}).reduce((sum, value) => sum + value, 0);
  const agents = agentsQuery.data ?? [];

  return (
    <div className="space-y-4">
      <TicketKpiStrip stats={stats} loading={statsQuery.isLoading} />

      <ChartCard
        title="新建与解决趋势"
        description="按服务器时区分日"
        loading={trendQuery.isLoading}
        empty={trendEmpty}
        height={260}
        action={
          <ToggleGroup
            type="single"
            size="sm"
            variant="outline"
            value={String(days)}
            onValueChange={(value) => value && setDays(Number(value))}
          >
            {[7, 30, 90].map((value) => (
              <ToggleGroupItem key={value} value={String(value)} className="px-2.5 text-xs">
                {value} 天
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
      >
        <ChartContainer config={TREND_CONFIG} className="h-[260px] w-full">
          <AreaChart data={trend} margin={{ left: 0, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={16} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Area dataKey="created" type="monotone" stroke="var(--color-created)" fill="var(--color-created)" fillOpacity={0.12} strokeWidth={2} dot={false} />
            <Area dataKey="resolved" type="monotone" stroke="var(--color-resolved)" fill="var(--color-resolved)" fillOpacity={0.12} strokeWidth={2} dot={false} />
            <Area dataKey="closed" type="monotone" stroke="var(--color-closed)" fill="transparent" strokeWidth={1.5} strokeDasharray="4 3" dot={false} />
            <ChartLegend content={<ChartLegendContent />} />
          </AreaChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <ChartCard title="分类分布" loading={statsQuery.isLoading} empty={!categories.length} height={Math.max(160, categories.length * 32)}>
          <ChartContainer config={CATEGORY_CONFIG} className="w-full" style={{ height: Math.max(160, categories.length * 32) }}>
            <BarChart data={categories} layout="vertical" margin={{ left: 0, right: 16, top: 4 }}>
              <CartesianGrid horizontal={false} strokeDasharray="3 3" />
              <XAxis type="number" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} />
              <YAxis type="category" dataKey="categoryName" tickLine={false} axisLine={false} fontSize={11} width={84} />
              <ChartTooltip content={<ChartTooltipContent hideLabel />} />
              <Bar dataKey="count" fill="var(--color-count)" radius={[0, 4, 4, 0]} />
            </BarChart>
          </ChartContainer>
        </ChartCard>

        <Card>
          <CardContent className="space-y-3 p-4">
            <h3 className="text-sm font-medium">优先级构成</h3>
            {statsQuery.isLoading ? (
              <Skeleton className="h-32 w-full rounded-xl" />
            ) : priorityTotal === 0 ? (
              <p className="py-10 text-center text-xs text-muted-foreground">暂无数据</p>
            ) : (
              <div className="space-y-3">
                <div className="flex h-2.5 overflow-hidden rounded-full bg-muted">
                  {PRIORITY_ROWS.map((row) => {
                    const value = stats?.byPriority?.[row.key] ?? 0;
                    return value ? (
                      <div key={row.key} className={row.bar} style={{ width: `${(value / priorityTotal) * 100}%` }} />
                    ) : null;
                  })}
                </div>
                <ul className="space-y-2">
                  {PRIORITY_ROWS.map((row) => {
                    const value = stats?.byPriority?.[row.key] ?? 0;
                    return (
                      <li key={row.key} className="flex items-center gap-2 text-xs">
                        <span className={cn("size-2 rounded-full", row.bar)} />
                        <span className="text-muted-foreground">{row.label}</span>
                        <span className="ml-auto font-medium tabular-nums">{value}</span>
                        <span className="w-10 text-right text-muted-foreground tabular-nums">
                          {Math.round((value / priorityTotal) * 100)}%
                        </span>
                      </li>
                    );
                  })}
                </ul>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardContent className="p-0">
          <div className="border-b px-4 py-3">
            <h3 className="text-sm font-medium">处理人绩效</h3>
          </div>
          {agentsQuery.isLoading ? (
            <div className="space-y-2 p-4">
              {[0, 1, 2].map((index) => (
                <Skeleton key={index} className="h-10 w-full" />
              ))}
            </div>
          ) : agents.length === 0 ? (
            <p className="py-10 text-center text-xs text-muted-foreground">暂无数据</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-4">处理人</TableHead>
                  <TableHead className="text-right">受理</TableHead>
                  <TableHead className="text-right">进行中</TableHead>
                  <TableHead className="text-right">已解决</TableHead>
                  <TableHead className="text-right">平均首响</TableHead>
                  <TableHead className="text-right">平均解决</TableHead>
                  <TableHead className="text-right">超时</TableHead>
                  <TableHead className="pr-4 text-right">满意度</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {agents.map((agent) => (
                  <TableRow key={agent.adminId}>
                    <TableCell className="pl-4">
                      <div className="flex items-center gap-2">
                        <PersonAvatar name={agent.displayName || agent.account} size="sm" />
                        <span className="truncate">{agent.displayName || agent.account}</span>
                      </div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{agent.assigned}</TableCell>
                    <TableCell className="text-right tabular-nums">{agent.open}</TableCell>
                    <TableCell className="text-right tabular-nums">{agent.resolved}</TableCell>
                    <TableCell className="text-right">{formatDuration(agent.avgFirstResponseMs)}</TableCell>
                    <TableCell className="text-right">{formatDuration(agent.avgResolveMs)}</TableCell>
                    <TableCell className="text-right">
                      {agent.breached > 0 ? (
                        <Badge variant="danger" size="sm">
                          {agent.breached}
                        </Badge>
                      ) : (
                        <span className="text-muted-foreground">0</span>
                      )}
                    </TableCell>
                    <TableCell className="pr-4 text-right">
                      {agent.avgRating > 0 ? (
                        <span className="inline-flex items-center gap-1">
                          <Star className="size-3 fill-amber-400 text-amber-400" />
                          {agent.avgRating.toFixed(1)}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

const PRIORITY_ROWS = [
  { key: "urgent", label: "紧急", bar: "bg-red-500" },
  { key: "high", label: "高", bar: "bg-amber-500" },
  { key: "normal", label: "中", bar: "bg-sky-500" },
  { key: "low", label: "低", bar: "bg-slate-400" }
];
