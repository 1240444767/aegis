"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, Cell, Label, Pie, PieChart, XAxis, YAxis } from "recharts";
import {
  AlertTriangle,
  CalendarClock,
  ChevronRight,
  FileStack,
  Gavel,
  HeartHandshake,
  Lock,
  SlidersHorizontal
} from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import {
  MODE_META,
  SOURCE_META,
  STATUS_META,
  formatCount,
  modeLabel,
  sourceLabel,
  statusLabel
} from "@/components/apps/account-deletion/account-deletion-shared";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type {
  DeletionCountItem,
  DeletionMode,
  DeletionSource,
  DeletionStatus
} from "@/lib/api/account-deletion";
import {
  useDeletionRequestsQuery,
  useDeletionSettingsQuery,
  useDeletionStatsQuery
} from "@/lib/account-deletion-hooks";

const SERIES: Array<{ key: "requested" | "completed" | "cancelled"; label: string; color: ThemedColor }> = [
  { key: "requested", label: "新增申请", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "completed", label: "已注销", color: { light: "#f43f5e", dark: "#fb7185" } },
  { key: "cancelled", label: "已撤销", color: { light: "#10b981", dark: "#34d399" } }
];

const RANGES = [7, 30, 90] as const;

const statusColor = (key: string) => STATUS_META[key as DeletionStatus]?.color;
const modeColor = (key: string) => MODE_META[key as DeletionMode]?.color;
const sourceColor = (key: string) => SOURCE_META[key as DeletionSource]?.color;

/**
 * 概览。四个数字（总申请、7 天内到期、挽留率、重新注册限制）是累计值；趋势与四张分布图按所选天数
 * （后端按提交时间截取，趋势按浏览器时区分日）。挽留率 = 已结束的申请里被撤销或驳回的比例。
 *
 * 待审核与执行失败是需要人处理的两类，单独一条提示点进申请列表。它们取自申请列表的总数而不是 byStatus：
 * 后者只算所选天数内提交的，一条 40 天前失败的申请在「30 天」下会被漏掉。
 */
export function DeletionOverviewPanel({
  appKey,
  onOpenConfig,
  onOpenRequests,
  onOpenTombstones
}: {
  appKey: string;
  onOpenConfig: () => void;
  onOpenRequests: (status?: DeletionStatus) => void;
  onOpenTombstones: () => void;
}) {
  const [days, setDays] = useState<number>(30);
  const settingsQuery = useDeletionSettingsQuery(appKey);
  const statsQuery = useDeletionStatsQuery(appKey, days);

  const settings = settingsQuery.data;
  const config = settings?.config;
  const stats = statsQuery.data;
  const trendConfig = useMemo(() => buildChartConfig(SERIES), []);
  const rows = useMemo(() => (stats?.trend ?? []).map((day) => ({ ...day, label: day.date.slice(5) })), [stats]);
  const trendEmpty = rows.every((row) => row.requested === 0 && row.completed === 0 && row.cancelled === 0);
  const reviewingQuery = useDeletionRequestsQuery(appKey, { status: "reviewing", page: 1, limit: 1 });
  const failedQuery = useDeletionRequestsQuery(appKey, { status: "failed", page: 1, limit: 1 });
  const reviewing = reviewingQuery.data?.total ?? 0;
  const failed = failedQuery.data?.total ?? 0;
  const windowText = `近 ${days} 天提交`;
  const value = (count?: number) => (stats ? formatCount(count) : "—");

  const rangePicker = (
    <ToggleGroup
      type="single"
      size="sm"
      value={String(days)}
      onValueChange={(next) => next && setDays(Number(next))}
      className="shrink-0"
    >
      {RANGES.map((range) => (
        <ToggleGroupItem key={range} value={String(range)} className="h-7 px-2.5 text-xs">
          {range} 天
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border px-4 py-3">
        {settingsQuery.isLoading ? (
          <Skeleton className="h-4 w-48" />
        ) : (
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
            <StatusDot active={Boolean(settings?.enabled)} labelActive="账号注销已开启" labelInactive="账号注销未开启" />
            {config ? (
              <div className="flex flex-wrap gap-1.5">
                {config.allowCooling ? (
                  <Badge variant="outline" size="sm" className="font-normal">
                    冷静期 {config.coolingDays} 天
                  </Badge>
                ) : null}
                {config.allowImmediate ? (
                  <Badge variant="outline" size="sm" className="font-normal">
                    可立即注销
                  </Badge>
                ) : null}
                {config.requireReview ? (
                  <Badge variant="outline" size="sm" className="font-normal">
                    人工审核
                  </Badge>
                ) : null}
              </div>
            ) : null}
          </div>
        )}
        <Button size="sm" variant="outline" className="h-8" onClick={onOpenConfig}>
          <SlidersHorizontal className="size-3.5" /> 配置
        </Button>
      </div>

      {reviewing > 0 || failed > 0 ? (
        <div className="flex flex-col gap-2 sm:flex-row">
          {reviewing > 0 ? (
            <AttentionButton
              icon={Gavel}
              tone="warning"
              label={`${formatCount(reviewing)} 条申请待审核`}
              onClick={() => onOpenRequests("reviewing")}
            />
          ) : null}
          {failed > 0 ? (
            <AttentionButton
              icon={AlertTriangle}
              tone="danger"
              label={`${formatCount(failed)} 条申请执行失败`}
              onClick={() => onOpenRequests("failed")}
            />
          ) : null}
        </div>
      ) : null}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <button type="button" className="text-left" onClick={() => onOpenRequests()}>
          <StatTile label="总申请" icon={FileStack} value={value(stats?.total)} />
        </button>
        <button
          type="button"
          className="text-left"
          onClick={() => onOpenRequests("scheduled")}
          disabled={!stats?.dueIn7Days}
        >
          <StatTile
            label="7 天内到期"
            icon={CalendarClock}
            tone={stats?.dueIn7Days ? "default" : "muted"}
            value={value(stats?.dueIn7Days)}
          />
        </button>
        <StatTile
          label="挽留率"
          icon={HeartHandshake}
          tone={stats && stats.retentionRate > 0 ? "positive" : "default"}
          value={stats ? `${(stats.retentionRate * 100).toFixed(1)}%` : "—"}
        />
        <button type="button" className="text-left" onClick={onOpenTombstones}>
          <StatTile label="重新注册限制" icon={Lock} value={value(stats?.tombstones)} />
        </button>
      </div>

      <ChartCard title="申请趋势" loading={statsQuery.isLoading} empty={trendEmpty} height={220} action={rangePicker}>
        <ChartContainer config={trendConfig} className="h-[220px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={12} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {SERIES.map((series) => (
              <Bar key={series.key} dataKey={series.key} fill={`var(--color-${series.key})`} radius={[3, 3, 0, 0]} />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <Donut
          title="按状态"
          description={windowText}
          unit="条申请"
          loading={statsQuery.isLoading}
          items={stats?.byStatus ?? []}
          labelOf={statusLabel}
          colorOf={statusColor}
        />
        <ReasonBars loading={statsQuery.isLoading} items={stats?.reasons ?? []} description={windowText} />
        <Donut
          title="按方式"
          description={windowText}
          unit="条申请"
          loading={statsQuery.isLoading}
          items={stats?.byMode ?? []}
          labelOf={modeLabel}
          colorOf={modeColor}
        />
        <Donut
          title="按来源"
          description={windowText}
          unit="条申请"
          loading={statsQuery.isLoading}
          items={stats?.bySource ?? []}
          labelOf={sourceLabel}
          colorOf={sourceColor}
        />
      </div>
    </div>
  );
}

function AttentionButton({
  icon: Icon,
  tone,
  label,
  onClick
}: {
  icon: typeof Gavel;
  tone: "warning" | "danger";
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        tone === "warning"
          ? "flex flex-1 items-center gap-2.5 rounded-xl border border-amber-200 bg-amber-50/60 px-4 py-2.5 text-left text-sm text-amber-800 transition-colors hover:bg-amber-100/70 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300 dark:hover:bg-amber-950/50"
          : "flex flex-1 items-center gap-2.5 rounded-xl border border-red-200 bg-red-50/60 px-4 py-2.5 text-left text-sm text-red-700 transition-colors hover:bg-red-100/70 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300 dark:hover:bg-red-950/50"
      }
    >
      <Icon className="size-4 shrink-0" />
      <span className="flex-1 font-medium">{label}</span>
      <ChevronRight className="size-4 shrink-0 opacity-60" />
    </button>
  );
}

/** 环形分布图：中间写合计；配色跟随状态 / 方式 / 来源的固定色，与列表徽标对得上 */
function Donut({
  title,
  description,
  unit,
  loading,
  items,
  labelOf,
  colorOf
}: {
  title: string;
  description: string;
  unit: string;
  loading: boolean;
  items: DeletionCountItem[];
  labelOf: (key: string) => string;
  colorOf: (key: string) => ThemedColor | undefined;
}) {
  const rows = useMemo(
    // 键可能为空或带特殊字符，CSS 变量名统一用序号
    () => items.filter((item) => item.count > 0).map((item, index) => ({ ...item, slot: `s${index}` })),
    [items]
  );
  const total = rows.reduce((sum, item) => sum + item.count, 0);
  const config = useMemo(
    () => buildChartConfig(rows.map((item) => ({ key: item.slot, label: labelOf(item.key), color: colorOf(item.key) }))),
    [rows, labelOf, colorOf]
  );

  return (
    <ChartCard title={title} description={description} loading={loading} empty={total === 0} height={220}>
      <ChartContainer config={config} className="mx-auto h-[220px] w-full">
        <PieChart>
          <ChartTooltip content={<ChartTooltipContent nameKey="slot" hideLabel />} />
          <Pie data={rows} dataKey="count" nameKey="slot" innerRadius={55} outerRadius={85} strokeWidth={2} paddingAngle={2}>
            {rows.map((item) => (
              <Cell key={item.slot} fill={`var(--color-${item.slot})`} />
            ))}
            <Label
              content={({ viewBox }) => {
                if (!viewBox || !("cx" in viewBox)) return null;
                return (
                  <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                    <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-xl font-bold">
                      {total.toLocaleString("zh-CN")}
                    </tspan>
                    <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-[10px]">
                      {unit}
                    </tspan>
                  </text>
                );
              }}
            />
          </Pie>
          <ChartLegend content={<ChartLegendContent nameKey="slot" className="flex-wrap gap-x-3 gap-y-1" />} />
        </PieChart>
      </ChartContainer>
    </ChartCard>
  );
}

/** 注销原因：横向条形，按次数从多到少 */
function ReasonBars({
  loading,
  items,
  description
}: {
  loading: boolean;
  items: DeletionCountItem[];
  description: string;
}) {
  const rows = useMemo(
    () =>
      [...items]
        .filter((item) => item.count > 0)
        .sort((a, b) => b.count - a.count)
        .slice(0, 8)
        .map((item) => ({ key: item.key || "none", name: item.label || item.key || "未选择", count: item.count })),
    [items]
  );
  const config = useMemo(() => buildChartConfig([{ key: "count", label: "申请" }]), []);
  const height = Math.max(160, rows.length * 30 + 20);

  return (
    <ChartCard title="按原因" description={description} loading={loading} empty={rows.length === 0} height={220}>
      <ChartContainer config={config} className="w-full" style={{ height }}>
        <BarChart data={rows} layout="vertical" margin={{ left: 8, right: 12 }}>
          <CartesianGrid horizontal={false} strokeDasharray="3 3" />
          <XAxis type="number" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} />
          <YAxis type="category" dataKey="name" tickLine={false} axisLine={false} width={96} fontSize={11} />
          <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
          <Bar dataKey="count" radius={[0, 4, 4, 0]} barSize={16}>
            {rows.map((row, index) => (
              <Cell key={row.key} fill={`var(--chart-${(index % 5) + 1})`} />
            ))}
          </Bar>
        </BarChart>
      </ChartContainer>
    </ChartCard>
  );
}
