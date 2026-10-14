"use client";

import { useMemo, useState } from "react";
import { Area, CartesianGrid, ComposedChart, Line, Tooltip, XAxis, YAxis } from "recharts";
import { TrendingUp } from "lucide-react";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { formatWith } from "@/lib/datetime";
import { useLiveSeries } from "@/lib/online-live/hooks";
import type { LiveRange } from "@/lib/online-live/types";
import { cn } from "@/lib/utils";
import { PanelHeader, SERIES_COLORS, formatCount } from "./live-shared";

const RANGES: Array<{ value: LiveRange; label: string }> = [
  { value: "1h", label: "1 小时" },
  { value: "6h", label: "6 小时" },
  { value: "24h", label: "24 小时" },
  { value: "7d", label: "7 天" }
];

const chartConfig = {
  users: { label: "在线用户", theme: SERIES_COLORS.users },
  connections: { label: "实时连接", theme: SERIES_COLORS.connections }
} satisfies ChartConfig;

function tickLabel(seconds: number, range: LiveRange) {
  const options: Intl.DateTimeFormatOptions =
    range === "7d" ? { month: "2-digit", day: "2-digit" } : { hour: "2-digit", minute: "2-digit", hour12: false };
  return formatWith(seconds * 1000, options, "");
}

function fullLabel(seconds: number) {
  return formatWith(seconds * 1000, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }, "");
}

/**
 * 在线曲线：在线用户与实时连接同一量纲，共用一条纵轴。
 * 历史来自服务端每分钟的采样，当前这一段随快照实时延伸；按步长分桶时取峰值。
 */
export function OnlineTrend({ app, className }: { app: number; className?: string }) {
  const [range, setRange] = useState<LiveRange>("24h");
  const { series, loading, error } = useLiveSeries(app, range);

  const data = useMemo(
    () => (series?.points ?? []).map(([t, users, connections]) => ({ t, users, connections })),
    [series]
  );
  const summary = useMemo(() => {
    if (data.length === 0) return null;
    let peak = data[0];
    let sum = 0;
    for (const point of data) {
      if (point.users > peak.users) peak = point;
      sum += point.users;
    }
    return { peak, average: sum / data.length, current: data[data.length - 1] };
  }, [data]);

  return (
    // 图例与提示在 ChartContainer 之外，序列色变量在卡片根上再定义一次（与 SERIES_COLORS 同值）
    <div
      className={cn(
        "flex min-w-0 flex-col rounded-2xl border bg-card",
        "[--color-connections:#d97706] [--color-users:#2563eb] dark:[--color-connections:#c97a06] dark:[--color-users:#3b82f6]",
        className
      )}
    >
      <PanelHeader
        title="在线趋势"
        icon={<TrendingUp className="size-4 text-muted-foreground" />}
        extra={
          <ToggleGroup
            type="single"
            value={range}
            onValueChange={(v) => v && setRange(v as LiveRange)}
            className="rounded-lg border bg-muted/40 p-0.5"
          >
            {RANGES.map((r) => (
              <ToggleGroupItem
                key={r.value}
                value={r.value}
                className="h-7 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm"
              >
                {r.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
      />

      <div className="flex flex-wrap items-end gap-x-6 gap-y-2 px-4 pt-3">
        <Figure label="峰值" value={summary?.peak.users} hint={summary ? fullLabel(summary.peak.t) : undefined} loading={loading} />
        <Figure label="平均" value={summary ? Math.round(summary.average) : undefined} loading={loading} />
        <Figure label="当前" value={summary?.current.users} loading={loading} />
        <div className="ml-auto flex items-center gap-3 pb-1 text-xs text-muted-foreground">
          <LegendSwatch className="bg-(--color-users)" label="在线用户" />
          <LegendSwatch className="bg-(--color-connections)" label="实时连接" line />
        </div>
      </div>

      <div className="min-h-0 flex-1 px-2 pt-2 pb-3">
        {loading ? (
          <Skeleton className="mx-2 h-56 rounded-lg" />
        ) : error ? (
          <div className="flex h-56 items-center justify-center text-xs text-muted-foreground">{error}</div>
        ) : data.length < 2 ? (
          <div className="flex h-56 flex-col items-center justify-center gap-1 text-center text-xs text-muted-foreground">
            <span>采样数据不足</span>
            <span>服务端每分钟记录一次在线人数，稍后即可看到曲线</span>
          </div>
        ) : (
          <ChartContainer config={chartConfig} className="h-56 w-full">
            <ComposedChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: -8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/50" />
              <XAxis
                dataKey="t"
                type="number"
                scale="time"
                domain={["dataMin", "dataMax"]}
                tickFormatter={(v: number) => tickLabel(v, range)}
                tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                axisLine={false}
                tickLine={false}
                minTickGap={32}
              />
              <YAxis
                tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                axisLine={false}
                tickLine={false}
                allowDecimals={false}
                width={44}
              />
              <Tooltip
                cursor={{ stroke: "var(--border)", strokeWidth: 1 }}
                content={({ active, payload }) => {
                  if (!active || !payload?.length) return null;
                  const point = payload[0].payload as (typeof data)[number];
                  return (
                    <div className="rounded-lg border bg-popover px-3 py-2 text-xs shadow-md">
                      <div className="text-muted-foreground">{fullLabel(point.t)}</div>
                      <div className="mt-1 flex items-center gap-2">
                        <span className="size-2 rounded-full bg-(--color-users)" />
                        <span className="text-muted-foreground">在线用户</span>
                        <span className="ml-auto pl-4 font-semibold tabular-nums">{formatCount(point.users)}</span>
                      </div>
                      <div className="mt-0.5 flex items-center gap-2">
                        <span className="size-2 rounded-full bg-(--color-connections)" />
                        <span className="text-muted-foreground">实时连接</span>
                        <span className="ml-auto pl-4 font-semibold tabular-nums">{formatCount(point.connections)}</span>
                      </div>
                    </div>
                  );
                }}
              />
              <Area
                type="monotone"
                dataKey="users"
                stroke="var(--color-users)"
                fill="var(--color-users)"
                fillOpacity={0.1}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4, fill: "var(--color-users)", stroke: "var(--background)", strokeWidth: 2 }}
                isAnimationActive={false}
              />
              <Line
                type="monotone"
                dataKey="connections"
                stroke="var(--color-connections)"
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4, fill: "var(--color-connections)", stroke: "var(--background)", strokeWidth: 2 }}
                isAnimationActive={false}
              />
            </ComposedChart>
          </ChartContainer>
        )}
      </div>
    </div>
  );
}

function Figure({ label, value, hint, loading }: { label: string; value?: number; hint?: string; loading: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      {loading ? (
        <Skeleton className="my-1 h-5 w-12" />
      ) : (
        <div className="flex items-baseline gap-1.5">
          <span className="text-lg leading-7 font-semibold tabular-nums">{formatCount(value)}</span>
          {hint ? <span className="text-[11px] text-muted-foreground tabular-nums">{hint}</span> : null}
        </div>
      )}
    </div>
  );
}

function LegendSwatch({ className, label, line }: { className: string; label: string; line?: boolean }) {
  return (
    <span className="flex items-center gap-1.5">
      <span className={cn(line ? "h-0.5 w-3 rounded-full" : "size-2.5 rounded-sm", className)} />
      {label}
    </span>
  );
}
