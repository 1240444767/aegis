"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, ComposedChart, Line, XAxis, YAxis } from "recharts";
import {
  CalendarCheck,
  CircleDollarSign,
  KeyRound,
  Layers,
  PackageOpen,
  ReceiptText,
  Ticket,
  TicketCheck,
  Users
} from "lucide-react";
import { CardKindBadge, SOURCE_LABEL, formatCount } from "@/components/apps/card-key/card-key-shared";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { CardKeySource } from "@/lib/api/card-key";
import { useCardKeyStatsQuery } from "@/lib/card-key-hooks";

const TREND_SERIES: Array<{ key: "redemptions" | "users"; label: string; color: ThemedColor }> = [
  { key: "redemptions", label: "核销次数", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "users", label: "核销人数", color: { light: "#0ea5e9", dark: "#38bdf8" } }
];

const SOURCE_SERIES: Array<{ key: CardKeySource; label: string; color: ThemedColor }> = [
  { key: "redeem", label: SOURCE_LABEL.redeem, color: { light: "#10b981", dark: "#34d399" } },
  { key: "login", label: SOURCE_LABEL.login, color: { light: "#0ea5e9", dark: "#38bdf8" } },
  { key: "admin", label: SOURCE_LABEL.admin, color: { light: "#f59e0b", dark: "#fbbf24" } }
];

const RANGES = [7, 30, 90] as const;

function money(value?: string) {
  const number = Number(value ?? 0);
  return `¥${(Number.isFinite(number) ? number : 0).toLocaleString("zh-CN", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  })}`;
}

/**
 * 卡密概览：发了多少、还能卖多少、交出去多少、用掉多少、值多少钱。
 *
 * 「可提取」是未使用、未提取、未过期的卡 —— 也就是手里还能交给渠道的库存；
 * 面值按「单张面值 × 可用人次」计发出、按「× 已用人次」计已用，没配面值的批次不计入。
 * 趋势按浏览器时区分日。
 */
export function CardKeyOverviewPanel({
  appKey,
  onOpenBatch
}: {
  appKey: string;
  onOpenBatch: (batchId: number) => void;
}) {
  const [days, setDays] = useState<number>(30);
  const statsQuery = useCardKeyStatsQuery(appKey, days);
  const stats = statsQuery.data;

  const trendConfig = useMemo(() => buildChartConfig(TREND_SERIES), []);
  const sourceConfig = useMemo(() => buildChartConfig(SOURCE_SERIES), []);
  const rows = useMemo(() => (stats?.trend ?? []).map((point) => ({ ...point, label: point.day.slice(5) })), [stats]);
  const sourceRows = useMemo(
    () =>
      SOURCE_SERIES.map((series) => ({
        key: series.key,
        label: series.label,
        count: stats?.sources.find((item) => item.source === series.key)?.count ?? 0,
        fill: `var(--color-${series.key})`
      })),
    [stats]
  );
  const trendEmpty = rows.every((row) => row.redemptions === 0);
  const sourceEmpty = sourceRows.every((row) => row.count === 0);
  const value = (number?: number) => (stats ? formatCount(number) : "—");

  if (statsQuery.isLoading && !stats) {
    return (
      <div className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 8 }, (_, index) => (
            <Skeleton key={index} className="h-[74px] rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-72 w-full rounded-xl" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {stats ? `共 ${formatCount(stats.batches)} 个批次` : null}
        </p>
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
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="卡密总数" icon={Layers} value={value(stats?.cards)} hint={stats ? `未使用 ${formatCount(stats.unused)}` : undefined} />
        <StatTile
          label="可提取"
          icon={Ticket}
          value={value(stats?.available)}
          hint="未使用、未提取、未过期"
        />
        <StatTile label="已提取" icon={PackageOpen} value={value(stats?.distributed)} hint="已交给渠道" />
        <StatTile label="有效授权" icon={KeyRound} value={value(stats?.activeLicenses)} hint="授权期内的授权卡" />
        <StatTile label="今日核销" icon={CalendarCheck} value={value(stats?.redemptionsToday)} />
        <StatTile label="累计核销" icon={TicketCheck} value={value(stats?.redemptions)} />
        <StatTile label="核销人数" icon={Users} value={value(stats?.users)} />
        <StatTile
          label="面值"
          icon={CircleDollarSign}
          value={stats ? money(stats.faceValueUsed) : "—"}
          hint={stats ? `已用 / 发出 ${money(stats.faceValueIssued)}` : undefined}
        />
      </div>

      {stats && (stats.expired > 0 || stats.disabled > 0) ? (
        <p className="text-[11px] text-muted-foreground">
          另有已过期 {formatCount(stats.expired)} 张，已作废 {formatCount(stats.disabled)} 张
        </p>
      ) : null}

      <ChartCard title={`核销趋势（${days} 天）`} loading={statsQuery.isLoading} empty={trendEmpty} height={240}>
        <ChartContainer config={trendConfig} className="h-[240px] w-full">
          <ComposedChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={16} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={36} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar dataKey="redemptions" fill="var(--color-redemptions)" radius={[3, 3, 0, 0]} />
            <Line dataKey="users" type="monotone" stroke="var(--color-users)" strokeWidth={2} dot={false} />
            <ChartLegend content={<ChartLegendContent />} />
          </ComposedChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        <ChartCard title={`核销来源（${days} 天）`} loading={statsQuery.isLoading} empty={sourceEmpty} height={180}>
          <ChartContainer config={sourceConfig} className="h-[180px] w-full">
            <BarChart data={sourceRows} layout="vertical" margin={{ left: 0, right: 16, top: 4 }}>
              <CartesianGrid horizontal={false} strokeDasharray="3 3" />
              <XAxis type="number" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} />
              <YAxis type="category" dataKey="label" tickLine={false} axisLine={false} fontSize={11} width={72} />
              <ChartTooltip content={<ChartTooltipContent hideLabel nameKey="key" />} />
              <Bar dataKey="count" radius={[0, 3, 3, 0]} />
            </BarChart>
          </ChartContainer>
        </ChartCard>

        <div className="rounded-xl border border-border">
          <div className="flex items-center gap-1.5 border-b border-border px-4 py-3 text-sm font-medium">
            <ReceiptText className="size-3.5 text-muted-foreground" />
            核销最多的批次（{days} 天）
          </div>
          {!stats?.topBatches.length ? (
            <p className="px-4 py-8 text-center text-xs text-muted-foreground">暂无核销</p>
          ) : (
            <ul className="divide-y divide-border">
              {stats.topBatches.map((batch) => {
                const top = stats.topBatches[0]?.redemptions || 1;
                return (
                  <li key={batch.batchId}>
                    <button
                      type="button"
                      onClick={() => onOpenBatch(batch.batchId)}
                      className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-muted/50"
                    >
                      <div className="min-w-0 flex-1 space-y-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-xs font-medium">{batch.name}</span>
                          <CardKindBadge kind={batch.kind} />
                        </div>
                        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                          <div
                            className="h-full rounded-full bg-indigo-500 dark:bg-indigo-400"
                            style={{ width: `${(batch.redemptions / top) * 100}%` }}
                          />
                        </div>
                      </div>
                      <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                        {formatCount(batch.redemptions)} 次
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
