"use client";

import { useMemo, useState, type ReactNode } from "react";
import {
  Area,
  Bar,
  CartesianGrid,
  Cell,
  ComposedChart,
  Line,
  Pie,
  PieChart,
  XAxis,
  YAxis
} from "recharts";
import {
  ArrowDownRight,
  ArrowUpRight,
  ChevronDown,
  ChevronUp,
  Minus,
  TrendingDown,
  TrendingUp,
  Zap,
  type LucideIcon
} from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { ChartContainer, ChartTooltip, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type {
  ReportHeatmap,
  ReportInsight,
  ReportKPI,
  ReportLifecycle,
  ReportSlice,
  ReportSummary,
  ReportTrend
} from "@/lib/api/reports";
import { WEEKDAY, bucketLabel, formatCompact, formatValue, labelOf, OTHER_KEY, type ValueFormat } from "./report-labels";

/* ───────────────────────── 配色 ───────────────────────── */

/**
 * 报表色板。用 CSS 变量而不是在组件里写死十六进制：同一个色号在深色模式下要换亮一档，
 * recharts 的 fill / stroke 与普通 div 的背景都读同一组变量，两处永远对得上。
 */
const PALETTE = [
  ["#6366f1", "#818cf8"],
  ["#0ea5e9", "#38bdf8"],
  ["#14b8a6", "#2dd4bf"],
  ["#f59e0b", "#fbbf24"],
  ["#ec4899", "#f472b6"],
  ["#8b5cf6", "#a78bfa"],
  ["#10b981", "#34d399"],
  ["#f43f5e", "#fb7185"],
  ["#84cc16", "#a3e635"],
  ["#64748b", "#94a3b8"]
] as const;

export const TONE = {
  indigo: 0,
  sky: 1,
  teal: 2,
  amber: 3,
  pink: 4,
  violet: 5,
  emerald: 6,
  rose: 7,
  lime: 8,
  slate: 9
} as const;

export type Tone = keyof typeof TONE;

export function color(index: number | Tone) {
  const i = typeof index === "number" ? index % PALETTE.length : TONE[index];
  return `var(--rp-${i})`;
}

export function ReportPalette() {
  const light = PALETTE.map(([l], i) => `--rp-${i}:${l};`).join("");
  const dark = PALETTE.map(([, d], i) => `--rp-${i}:${d};`).join("");
  return <style>{`.report-scope{${light}}.dark .report-scope{${dark}}`}</style>;
}

/* ───────────────────────── 外壳 ───────────────────────── */

export function ReportCard({
  title,
  description,
  action,
  loading,
  empty,
  emptyText = "这段时间没有数据",
  height = 240,
  className,
  children
}: {
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  loading?: boolean;
  empty?: boolean;
  emptyText?: string;
  height?: number;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Card className={cn("gap-0 py-0", className)}>
      <CardContent className="flex h-full flex-col gap-3 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 space-y-0.5">
            <h3 className="text-sm font-medium">{title}</h3>
            {description ? <p className="text-xs leading-snug text-muted-foreground">{description}</p> : null}
          </div>
          {action}
        </div>
        {loading ? (
          <Skeleton className="w-full rounded-xl" style={{ height }} />
        ) : empty ? (
          <div
            className="flex items-center justify-center rounded-xl border border-dashed text-xs text-muted-foreground"
            style={{ height }}
          >
            {emptyText}
          </div>
        ) : (
          children
        )}
      </CardContent>
    </Card>
  );
}

/* ───────────────────────── KPI ───────────────────────── */

export type KpiSpec = {
  key: string;
  label: string;
  icon: LucideIcon;
  format?: ValueFormat;
  tone?: Tone;
  /** 数值变大是坏事（失败数、退款、流失） */
  inverse?: boolean;
  hint?: string;
};

export function KpiGrid({
  specs,
  kpis,
  loading,
  columns = 4
}: {
  specs: KpiSpec[];
  kpis?: Partial<Record<string, ReportKPI>>;
  loading?: boolean;
  columns?: 3 | 4 | 5 | 6;
}) {
  return (
    <div
      className={cn(
        "grid gap-3 sm:grid-cols-2",
        columns === 3 && "lg:grid-cols-3",
        columns === 4 && "lg:grid-cols-4",
        columns === 5 && "lg:grid-cols-3 2xl:grid-cols-5",
        columns === 6 && "lg:grid-cols-3 2xl:grid-cols-6"
      )}
    >
      {specs.map((spec) => (
        <KpiCard key={spec.key} spec={spec} kpi={kpis?.[spec.key]} loading={loading && !kpis} />
      ))}
    </div>
  );
}

export function KpiCard({ spec, kpi, loading }: { spec: KpiSpec; kpi?: ReportKPI; loading?: boolean }) {
  const Icon = spec.icon;
  const tone = color(spec.tone ?? "indigo");
  const format = spec.format ?? "int";
  return (
    <div className="relative overflow-hidden rounded-2xl border bg-card px-4 pt-3.5 pb-3">
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
          <span
            className="flex size-6 shrink-0 items-center justify-center rounded-lg"
            style={{ backgroundColor: `color-mix(in oklab, ${tone} 14%, transparent)`, color: tone }}
          >
            <Icon className="size-3.5" />
          </span>
          <span className="truncate">{spec.label}</span>
        </div>
        {kpi ? <DeltaBadge change={kpi.change} inverse={spec.inverse} previous={kpi.previous} format={format} /> : null}
      </div>
      {loading ? (
        <Skeleton className="mt-2.5 h-7 w-24" />
      ) : (
        <div className="mt-2 flex items-end justify-between gap-3">
          <div className="min-w-0">
            <div className="truncate text-2xl font-semibold tracking-tight tabular-nums">
              {kpi ? formatValue(kpi.value, format) : "—"}
            </div>
            {spec.hint ? <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{spec.hint}</div> : null}
          </div>
          {kpi?.spark && kpi.spark.length > 1 ? <Sparkline values={kpi.spark} stroke={tone} /> : null}
        </div>
      )}
    </div>
  );
}

function DeltaBadge({
  change,
  previous,
  inverse,
  format
}: {
  change?: number;
  previous?: number;
  inverse?: boolean;
  format: ValueFormat;
}) {
  if (change === undefined || change === null) {
    if (previous === undefined) return null;
    return (
      <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">上期 {formatValue(previous, format)}</span>
    );
  }
  const flat = Math.abs(change) < 0.0005;
  const up = change > 0;
  const good = flat ? null : inverse ? !up : up;
  const Icon = flat ? Minus : up ? ArrowUpRight : ArrowDownRight;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cn(
            "inline-flex shrink-0 items-center gap-0.5 rounded-full px-1.5 py-0.5 text-[11px] font-medium tabular-nums",
            good === null && "bg-muted text-muted-foreground",
            good === true && "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
            good === false && "bg-rose-500/10 text-rose-600 dark:text-rose-400"
          )}
        >
          <Icon className="size-3" />
          {flat ? "持平" : `${Math.abs(change * 100).toFixed(Math.abs(change) < 0.1 ? 1 : 0)}%`}
        </span>
      </TooltipTrigger>
      <TooltipContent>上期 {formatValue(previous, format)}</TooltipContent>
    </Tooltip>
  );
}

export function Sparkline({ values, stroke, width = 88, height = 30 }: { values: number[]; stroke: string; width?: number; height?: number }) {
  const path = useMemo(() => {
    const max = Math.max(...values);
    const min = Math.min(...values, 0);
    const span = max - min || 1;
    const step = values.length > 1 ? width / (values.length - 1) : width;
    const points = values.map((v, i) => [i * step, height - 2 - ((v - min) / span) * (height - 4)] as const);
    const line = points.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`).join("");
    return { line, area: `${line}L${width},${height}L0,${height}Z` };
  }, [values, width, height]);
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="shrink-0 overflow-visible">
      <path d={path.area} fill={stroke} fillOpacity={0.1} />
      <path d={path.line} fill="none" stroke={stroke} strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

/* ───────────────────────── 趋势图 ───────────────────────── */

export type SeriesSpec = {
  key: string;
  label: string;
  tone?: Tone | number;
  type?: "area" | "line" | "bar";
  stack?: string;
  /** 放到右轴（比率类序列与计数放在一起时用） */
  right?: boolean;
  dashed?: boolean;
};

export function TrendChart({
  trend,
  series,
  granularity,
  format = "int",
  rightFormat = "percent",
  height = 260
}: {
  trend?: ReportTrend;
  series: SeriesSpec[];
  granularity: string;
  format?: ValueFormat;
  rightFormat?: ValueFormat;
  height?: number;
}) {
  const config = useMemo<ChartConfig>(() => {
    const c: ChartConfig = {};
    series.forEach((s, i) => {
      c[s.key] = { label: s.label, color: color(s.tone ?? i) };
    });
    return c;
  }, [series]);
  const rows = useMemo(
    () =>
      (trend?.buckets ?? []).map((bucket, i) => {
        const row: Record<string, number | string> = { bucket, label: bucketLabel(bucket, granularity) };
        series.forEach((s) => {
          row[s.key] = trend?.series?.[s.key]?.[i] ?? 0;
        });
        return row;
      }),
    [trend, series, granularity]
  );
  const hasRight = series.some((s) => s.right);
  const formats = useMemo(() => Object.fromEntries(series.map((s) => [s.key, s.right ? rightFormat : format])), [series, format, rightFormat]);

  return (
    <ChartContainer config={config} className="aspect-auto w-full" style={{ height }}>
      <ComposedChart data={rows} margin={{ left: 4, right: hasRight ? 4 : 12, top: 8, bottom: 0 }}>
        <CartesianGrid vertical={false} strokeDasharray="3 3" />
        <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={18} tickMargin={6} />
        <YAxis
          yAxisId="left"
          tickLine={false}
          axisLine={false}
          fontSize={11}
          width={48}
          allowDecimals={format !== "int"}
          tickFormatter={(v: number) => formatCompact(v, format)}
        />
        {hasRight ? (
          <YAxis
            yAxisId="right"
            orientation="right"
            tickLine={false}
            axisLine={false}
            fontSize={11}
            width={44}
            tickFormatter={(v: number) => formatCompact(v, rightFormat)}
          />
        ) : null}
        <ChartTooltip cursor={{ strokeDasharray: "3 3" }} content={<TrendTooltip config={config} formats={formats} />} />
        {series.map((s) => {
          const fill = `var(--color-${s.key})`;
          const axis = s.right ? "right" : "left";
          if (s.type === "bar") {
            return (
              <Bar
                key={s.key}
                yAxisId={axis}
                dataKey={s.key}
                stackId={s.stack}
                fill={fill}
                radius={s.stack ? 0 : [3, 3, 0, 0]}
                maxBarSize={28}
              />
            );
          }
          if (s.type === "line") {
            return (
              <Line
                key={s.key}
                yAxisId={axis}
                dataKey={s.key}
                type="monotone"
                stroke={fill}
                strokeWidth={2}
                strokeDasharray={s.dashed ? "4 4" : undefined}
                dot={false}
                activeDot={{ r: 3.5 }}
              />
            );
          }
          return (
            <Area
              key={s.key}
              yAxisId={axis}
              dataKey={s.key}
              type="monotone"
              stackId={s.stack}
              stroke={fill}
              strokeWidth={2}
              fill={fill}
              fillOpacity={s.stack ? 0.35 : 0.12}
              strokeDasharray={s.dashed ? "4 4" : undefined}
              dot={false}
              activeDot={{ r: 3.5 }}
            />
          );
        })}
      </ComposedChart>
    </ChartContainer>
  );
}

type TooltipEntry = { dataKey?: string | number; value?: number | string; payload?: Record<string, unknown> };

function TrendTooltip({
  active,
  payload,
  config,
  formats
}: {
  active?: boolean;
  payload?: TooltipEntry[];
  config: ChartConfig;
  formats: Record<string, ValueFormat>;
}) {
  if (!active || !payload?.length) return null;
  const bucket = String(payload[0]?.payload?.bucket ?? "");
  return (
    <div className="min-w-40 rounded-lg border bg-background px-3 py-2 text-xs shadow-lg">
      <div className="mb-1.5 font-medium tabular-nums">{bucket}</div>
      <div className="space-y-1">
        {payload.map((item) => {
          const key = String(item.dataKey);
          return (
            <div key={key} className="flex items-center gap-2">
              <span className="size-2 shrink-0 rounded-[3px]" style={{ backgroundColor: `var(--color-${key})` }} />
              <span className="flex-1 text-muted-foreground">{config[key]?.label}</span>
              <span className="font-mono font-medium tabular-nums">{formatValue(Number(item.value ?? 0), formats[key])}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/* ───────────────────────── 排行与分布 ───────────────────────── */

export type RankListProps = {
  items?: ReportSlice[];
  dict?: Record<string, string>;
  /** 主值取哪一列 */
  metric?: "count" | "users" | "amount" | "value";
  format?: ValueFormat;
  /** 右侧的副值 */
  secondary?: (item: ReportSlice) => ReactNode;
  leading?: (item: ReportSlice) => ReactNode;
  labelOf?: (item: ReportSlice) => ReactNode;
  limit?: number;
  tone?: Tone | number;
  /** 分档类分布（1 次、2-3 次 …）按原顺序展示，不显示名次 */
  ordinal?: boolean;
};

export function RankList({
  items = [],
  dict,
  metric = "count",
  format = "int",
  secondary,
  leading,
  labelOf: customLabel,
  limit = 8,
  tone,
  ordinal = true
}: RankListProps) {
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? items : items.slice(0, limit);
  const max = Math.max(...items.map((it) => Number(it[metric] ?? 0)), 0) || 1;
  if (items.length === 0) {
    return <div className="flex h-24 items-center justify-center text-xs text-muted-foreground">暂无数据</div>;
  }
  return (
    <div className="space-y-1">
      {visible.map((item, i) => {
        const value = Number(item[metric] ?? 0);
        const fill = color(tone ?? (item.key === OTHER_KEY ? "slate" : i));
        return (
          <div key={item.key} className="group relative flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-xs">
            <div
              className="absolute inset-y-0.5 left-0 rounded-lg transition-[width] duration-500"
              style={{ width: `${Math.max((value / max) * 100, 1.5)}%`, backgroundColor: `color-mix(in oklab, ${fill} 13%, transparent)` }}
            />
            {ordinal ? (
              <span className="relative w-4 shrink-0 text-right font-mono text-[11px] text-muted-foreground tabular-nums">{i + 1}</span>
            ) : null}
            {leading ? <span className="relative shrink-0">{leading(item)}</span> : null}
            <span className="relative min-w-0 flex-1 truncate">
              {customLabel ? customLabel(item) : labelOf(dict, item.key, item.label)}
            </span>
            {secondary ? <span className="relative shrink-0 text-muted-foreground tabular-nums">{secondary(item)}</span> : null}
            <span className="relative w-16 shrink-0 text-right font-mono font-medium tabular-nums">{formatValue(value, format)}</span>
            <span className="relative w-12 shrink-0 text-right font-mono text-muted-foreground tabular-nums">
              {(item.share * 100).toFixed(1)}%
            </span>
          </div>
        );
      })}
      {items.length > limit ? (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex w-full items-center justify-center gap-1 rounded-lg py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        >
          {expanded ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
          {expanded ? "收起" : `展开全部 ${items.length} 项`}
        </button>
      ) : null}
    </div>
  );
}

export function Donut({
  items = [],
  dict,
  metric = "count",
  format = "int",
  centerLabel = "合计",
  size = 168
}: {
  items?: ReportSlice[];
  dict?: Record<string, string>;
  metric?: "count" | "users" | "amount" | "value";
  format?: ValueFormat;
  centerLabel?: string;
  size?: number;
}) {
  const data = useMemo(() => items.filter((it) => Number(it[metric] ?? 0) > 0), [items, metric]);
  const total = data.reduce((sum, it) => sum + Number(it[metric] ?? 0), 0);
  const config = useMemo<ChartConfig>(() => {
    const c: ChartConfig = {};
    data.forEach((it, i) => {
      c[it.key] = { label: labelOf(dict, it.key, it.label), color: color(it.key === OTHER_KEY ? "slate" : i) };
    });
    return c;
  }, [data, dict]);
  if (data.length === 0) {
    return <div className="flex h-32 items-center justify-center text-xs text-muted-foreground">暂无数据</div>;
  }
  return (
    <div className="flex flex-col items-center gap-4 sm:flex-row">
      <div className="relative shrink-0" style={{ width: size, height: size }}>
        <ChartContainer config={config} className="aspect-square" style={{ width: size, height: size }}>
          <PieChart>
            <ChartTooltip content={<DonutTooltip dict={dict} metric={metric} format={format} />} />
            <Pie data={data} dataKey={metric} nameKey="key" innerRadius={size * 0.32} outerRadius={size * 0.48} paddingAngle={1.5} strokeWidth={0}>
              {data.map((it, i) => (
                <Cell key={it.key} fill={color(it.key === OTHER_KEY ? "slate" : i)} />
              ))}
            </Pie>
          </PieChart>
        </ChartContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-[11px] text-muted-foreground">{centerLabel}</span>
          <span className="text-base font-semibold tabular-nums">{formatCompact(total, format)}</span>
        </div>
      </div>
      <div className="w-full min-w-0 flex-1 space-y-1.5">
        {data.slice(0, 8).map((it, i) => (
          <div key={it.key} className="flex items-center gap-2 text-xs">
            <span className="size-2.5 shrink-0 rounded-[3px]" style={{ backgroundColor: color(it.key === OTHER_KEY ? "slate" : i) }} />
            <span className="min-w-0 flex-1 truncate">{labelOf(dict, it.key, it.label)}</span>
            <span className="font-mono tabular-nums">{formatValue(Number(it[metric] ?? 0), format)}</span>
            <span className="w-12 text-right font-mono text-muted-foreground tabular-nums">
              {((Number(it[metric] ?? 0) / (total || 1)) * 100).toFixed(1)}%
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function DonutTooltip({
  active,
  payload,
  dict,
  metric,
  format
}: {
  active?: boolean;
  payload?: Array<{ payload?: ReportSlice }>;
  dict?: Record<string, string>;
  metric: "count" | "users" | "amount" | "value";
  format: ValueFormat;
}) {
  const item = payload?.[0]?.payload;
  if (!active || !item) return null;
  return (
    <div className="rounded-lg border bg-background px-3 py-2 text-xs shadow-lg">
      <div className="font-medium">{labelOf(dict, item.key, item.label)}</div>
      <div className="mt-0.5 font-mono tabular-nums">{formatValue(Number(item[metric] ?? 0), format)}</div>
    </div>
  );
}

/** 一条横向的占比条：分布项少（≤6）时比环形图更省地方、更好比较 */
export function ShareBar({ items = [], dict, metric = "count" }: { items?: ReportSlice[]; dict?: Record<string, string>; metric?: "count" | "users" | "amount" }) {
  const total = items.reduce((sum, it) => sum + Number(it[metric] ?? 0), 0);
  if (total === 0) return <div className="h-2.5 rounded-full bg-muted" />;
  return (
    <div className="space-y-2.5">
      <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-muted">
        {items.map((it, i) => {
          const v = Number(it[metric] ?? 0);
          if (v === 0) return null;
          return <div key={it.key} style={{ width: `${(v / total) * 100}%`, backgroundColor: color(it.key === OTHER_KEY ? "slate" : i) }} />;
        })}
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1">
        {items.map((it, i) => (
          <span key={it.key} className="flex items-center gap-1.5 text-xs">
            <span className="size-2 rounded-[3px]" style={{ backgroundColor: color(it.key === OTHER_KEY ? "slate" : i) }} />
            <span className="text-muted-foreground">{labelOf(dict, it.key, it.label)}</span>
            <span className="font-mono tabular-nums">{((Number(it[metric] ?? 0) / total) * 100).toFixed(1)}%</span>
          </span>
        ))}
      </div>
    </div>
  );
}

/* ───────────────────────── 热力图 ───────────────────────── */

export function HeatmapGrid({ heatmap, tone = "indigo", unit = "次" }: { heatmap?: ReportHeatmap; tone?: Tone; unit?: string }) {
  const fill = color(tone);
  if (!heatmap || heatmap.total === 0) {
    return <div className="flex h-40 items-center justify-center text-xs text-muted-foreground">暂无数据</div>;
  }
  const peak = (() => {
    let best = { d: 0, h: 0, v: -1 };
    heatmap.cells.forEach((row, d) => row.forEach((v, h) => v > best.v && (best = { d, h, v })));
    return best;
  })();
  return (
    <div className="space-y-2">
      <div className="overflow-x-auto">
        <div className="grid min-w-[560px] gap-[3px]" style={{ gridTemplateColumns: "36px repeat(24, minmax(0, 1fr))" }}>
          <span />
          {Array.from({ length: 24 }, (_, h) => (
            <span key={h} className="text-center font-mono text-[10px] text-muted-foreground">
              {h % 3 === 0 ? h : ""}
            </span>
          ))}
          {heatmap.cells.map((row, d) => (
            <HeatRow key={d} day={d} row={row} max={heatmap.max} fill={fill} unit={unit} />
          ))}
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-muted-foreground">
        <span>
          高峰在{WEEKDAY[peak.d]} {peak.h}:00 到 {peak.h + 1}:00，共 {formatValue(peak.v)} {unit}
        </span>
        <span className="flex items-center gap-1">
          少
          {[0.08, 0.3, 0.55, 0.8, 1].map((o) => (
            <span key={o} className="size-2.5 rounded-[3px]" style={{ backgroundColor: `color-mix(in oklab, ${fill} ${o * 100}%, transparent)` }} />
          ))}
          多
        </span>
      </div>
    </div>
  );
}

function HeatRow({ day, row, max, fill, unit }: { day: number; row: number[]; max: number; fill: string; unit: string }) {
  return (
    <>
      <span className="flex items-center text-[11px] text-muted-foreground">{WEEKDAY[day]}</span>
      {row.map((v, h) => {
        const ratio = max > 0 ? v / max : 0;
        return (
          <Tooltip key={h}>
            <TooltipTrigger asChild>
              <span
                className="aspect-square rounded-[4px] bg-muted/60"
                style={v > 0 ? { backgroundColor: `color-mix(in oklab, ${fill} ${Math.max(ratio, 0.08) * 100}%, transparent)` } : undefined}
              />
            </TooltipTrigger>
            <TooltipContent>
              {WEEKDAY[day]} {h}:00 到 {h + 1}:00：{formatValue(v)} {unit}
            </TooltipContent>
          </Tooltip>
        );
      })}
    </>
  );
}

/* ───────────────────────── 统计摘要 ───────────────────────── */

export function SummaryStrip({ summary, format = "decimal" }: { summary?: ReportSummary; format?: ValueFormat }) {
  if (!summary || summary.count === 0) {
    return <div className="flex h-16 items-center justify-center text-xs text-muted-foreground">暂无样本</div>;
  }
  const cells: Array<[string, number]> = [
    ["最小", summary.min],
    ["P25", summary.p25],
    ["中位数", summary.p50],
    ["P75", summary.p75],
    ["P90", summary.p90],
    ["P99", summary.p99],
    ["最大", summary.max]
  ];
  const span = summary.max - summary.min || 1;
  const pos = (v: number) => `${((v - summary.min) / span) * 100}%`;
  return (
    <div className="space-y-3">
      <div className="relative h-7">
        <div className="absolute inset-x-0 top-1/2 h-px -translate-y-1/2 bg-border" />
        <div
          className="absolute top-1/2 h-4 -translate-y-1/2 rounded-md"
          style={{ left: pos(summary.p25), width: `calc(${pos(summary.p75)} - ${pos(summary.p25)})`, backgroundColor: `color-mix(in oklab, ${color("indigo")} 22%, transparent)`, minWidth: 2 }}
        />
        <div className="absolute top-1/2 h-6 w-0.5 -translate-y-1/2 rounded bg-foreground" style={{ left: pos(summary.p50) }} />
        <div className="absolute top-1/2 h-3 w-px -translate-y-1/2 bg-muted-foreground" style={{ left: pos(summary.p90) }} />
      </div>
      <div className="grid grid-cols-4 gap-2 sm:grid-cols-7">
        {cells.map(([label, v]) => (
          <div key={label} className="rounded-lg bg-muted/50 px-2 py-1.5">
            <div className="text-[10px] text-muted-foreground">{label}</div>
            <div className="truncate font-mono text-xs font-medium tabular-nums">{formatValue(v, format)}</div>
          </div>
        ))}
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
        <span>样本 {formatValue(summary.count)}</span>
        <span>均值 {formatValue(summary.mean, format)}</span>
        <span>标准差 {formatValue(summary.std, format)}</span>
        {format === "money" ? <span>合计 {formatValue(summary.sum, format)}</span> : null}
      </div>
    </div>
  );
}

/* ───────────────────────── 生命周期 ───────────────────────── */

export function LifecyclePanel({ lifecycle }: { lifecycle?: ReportLifecycle }) {
  const parts = [
    { key: "new", label: "新增", hint: "本期注册且活跃", tone: "emerald" as Tone, value: lifecycle?.new ?? 0 },
    { key: "retained", label: "留存", hint: "上期活跃，本期仍活跃", tone: "indigo" as Tone, value: lifecycle?.retained ?? 0 },
    { key: "resurrected", label: "回流", hint: "上期未活跃，本期回来了", tone: "amber" as Tone, value: lifecycle?.resurrected ?? 0 },
    { key: "churned", label: "流失", hint: "上期活跃，本期未出现", tone: "rose" as Tone, value: lifecycle?.churned ?? 0 }
  ];
  const active = parts[0].value + parts[1].value + parts[2].value;
  const total = active + parts[3].value || 1;
  const growth = parts[0].value + parts[2].value - parts[3].value;
  return (
    <div className="space-y-4">
      <div className="flex h-3 w-full overflow-hidden rounded-full bg-muted">
        {parts.map((p) =>
          p.value > 0 ? (
            <div
              key={p.key}
              style={{
                width: `${(p.value / total) * 100}%`,
                backgroundColor: color(p.tone),
                opacity: p.key === "churned" ? 0.45 : 1
              }}
            />
          ) : null
        )}
      </div>
      <div className="grid grid-cols-2 gap-2 lg:grid-cols-4">
        {parts.map((p) => (
          <div key={p.key} className="rounded-xl border px-3 py-2">
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <span className="size-2 rounded-full" style={{ backgroundColor: color(p.tone) }} />
              {p.label}
            </div>
            <div className="mt-0.5 text-lg font-semibold tabular-nums">{formatValue(p.value)}</div>
            <div className="text-[11px] text-muted-foreground">{p.hint}</div>
          </div>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        净增长（新增加回流减流失）
        <span className={cn("ml-1 font-mono font-medium tabular-nums", growth >= 0 ? "text-emerald-600 dark:text-emerald-400" : "text-rose-600 dark:text-rose-400")}>
          {growth >= 0 ? "+" : ""}
          {formatValue(growth)}
        </span>
        ，增长质量（新增加回流比流失）
        <span className="ml-1 font-mono font-medium tabular-nums">
          {parts[3].value > 0 ? formatValue((parts[0].value + parts[2].value) / parts[3].value, "decimal") : "—"}
        </span>
      </p>
    </div>
  );
}

/* ───────────────────────── 自动洞察 ───────────────────────── */

const INSIGHT_METRIC: Record<string, { label: string; format: ValueFormat }> = {
  newUsers: { label: "新增用户", format: "int" },
  dau: { label: "日活", format: "int" },
  revenue: { label: "收入", format: "money" },
  logins: { label: "登录", format: "int" }
};

export function InsightList({ insights }: { insights?: ReportInsight[] }) {
  if (!insights || insights.length === 0) {
    return (
      <div className="flex h-24 flex-col items-center justify-center gap-1 text-xs text-muted-foreground">
        <Zap className="size-4" />
        各项指标都在正常波动范围内
      </div>
    );
  }
  return (
    <ul className="space-y-1.5">
      {insights.map((it, i) => {
        const meta = INSIGHT_METRIC[it.metric] ?? { label: it.metric, format: "int" as ValueFormat };
        const trend = it.kind === "growth" || it.kind === "decline";
        const good = it.kind === "spike" || it.kind === "growth";
        const Icon = trend ? (good ? TrendingUp : TrendingDown) : good ? ArrowUpRight : ArrowDownRight;
        return (
          <li key={i} className="flex items-start gap-2.5 rounded-xl border px-3 py-2 text-xs">
            <span
              className={cn(
                "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-lg",
                good ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" : "bg-rose-500/10 text-rose-600 dark:text-rose-400"
              )}
            >
              <Icon className="size-3.5" />
            </span>
            <span className="leading-relaxed">
              {trend ? (
                <>
                  {meta.label}整体{good ? "上升" : "下滑"}，按线性趋势估算整段变化约
                  <b className="mx-0.5 font-mono tabular-nums">{(Math.abs(it.value) * 100).toFixed(0)}%</b>
                </>
              ) : (
                <>
                  <span className="font-mono tabular-nums">{it.bucket}</span> {meta.label}
                  <b className="mx-0.5 font-mono tabular-nums">{formatValue(it.value, meta.format)}</b>，
                  {good ? "高于" : "低于"}此前均值 {formatValue(it.expected, meta.format)}，偏离
                  <span className="mx-0.5 font-mono tabular-nums">{Math.abs(it.score).toFixed(1)}</span>个标准差
                </>
              )}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

/* ───────────────────────── 小工具 ───────────────────────── */

export function SectionGrid({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("grid gap-4 lg:grid-cols-2", className)}>{children}</div>;
}

export function trendEmpty(trend: ReportTrend | undefined, keys: string[]) {
  if (!trend) return true;
  return keys.every((k) => (trend.series?.[k] ?? []).every((v) => !v));
}

export function MiniStat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-xl bg-muted/50 px-3 py-2">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="mt-0.5 font-mono text-sm font-medium tabular-nums">{value}</div>
    </div>
  );
}
