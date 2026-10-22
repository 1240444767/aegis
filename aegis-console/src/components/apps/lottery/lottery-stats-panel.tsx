"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, Line, ComposedChart, XAxis, YAxis } from "recharts";
import {
  ArrowRightLeft,
  CalendarCheck,
  Coins,
  Dices,
  HandCoins,
  Hourglass,
  ShieldCheck,
  Sparkles,
  Trophy,
  Users
} from "lucide-react";
import {
  SOURCE_LABEL,
  formatCount,
  kindLabelOf
} from "@/components/apps/lottery/lottery-shared";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { LotteryAuditCard } from "@/components/apps/lottery/lottery-audit-card";
import { StatTile } from "@/components/content/content-shared";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { LotterySource } from "@/lib/api/lottery";
import { useLotteryActivitiesQuery, useLotteryCatalogQuery, useLotteryStatsQuery } from "@/lib/lottery-hooks";

/** 抽奖次数与中奖次数画成柱，人数是去重后的另一种口径，画成线叠在上面 */
const TREND_SERIES: Array<{ key: "draws" | "wins" | "users"; label: string; color: ThemedColor }> = [
  { key: "draws", label: "抽奖次数", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "wins", label: "中奖次数", color: { light: "#f59e0b", dark: "#fbbf24" } },
  { key: "users", label: "抽奖人数", color: { light: "#0ea5e9", dark: "#38bdf8" } }
];

const SOURCE_SERIES: Array<{ key: LotterySource; label: string; color: ThemedColor }> = [
  { key: "free", label: SOURCE_LABEL.free, color: { light: "#10b981", dark: "#34d399" } },
  { key: "bonus", label: SOURCE_LABEL.bonus, color: { light: "#0ea5e9", dark: "#38bdf8" } },
  { key: "points", label: SOURCE_LABEL.points, color: { light: "#f59e0b", dark: "#fbbf24" } }
];

const RANGES = [7, 14, 30] as const;

/**
 * 一个活动的统计：今日与累计数字、按天趋势、次数来源、各奖品在窗口内抽中多少次与库存。
 * 「中奖」不含谢谢参与；保底触发是走保底分支发出去的次数。
 */
export function LotteryStatsPanel({
  appKey,
  activityId,
  onActivityChange,
  onOpenPending
}: {
  appKey: string;
  activityId?: number;
  onActivityChange: (activityId: number) => void;
  onOpenPending: (activityId: number) => void;
}) {
  const [days, setDays] = useState<number>(14);
  const activitiesQuery = useLotteryActivitiesQuery(appKey);
  const catalogQuery = useLotteryCatalogQuery(appKey);
  const activities = useMemo(() => activitiesQuery.data ?? [], [activitiesQuery.data]);
  const currentId = activityId && activities.some((item) => item.id === activityId) ? activityId : activities[0]?.id;
  const statsQuery = useLotteryStatsQuery(appKey, currentId, days);

  const stats = statsQuery.data;
  const kinds = catalogQuery.data?.kinds ?? [];
  const trendConfig = useMemo(() => buildChartConfig(TREND_SERIES), []);
  const sourceConfig = useMemo(() => buildChartConfig(SOURCE_SERIES), []);
  const rows = useMemo(() => (stats?.trend ?? []).map((day) => ({ ...day, label: day.date.slice(5) })), [stats]);
  const sourceRows = useMemo(
    () =>
      SOURCE_SERIES.map((series) => ({
        key: series.key,
        label: series.label,
        draws: stats?.sources.find((item) => item.source === series.key)?.draws ?? 0,
        fill: `var(--color-${series.key})`
      })),
    [stats]
  );
  const trendEmpty = rows.every((row) => row.draws === 0);
  const sourceEmpty = sourceRows.every((row) => row.draws === 0);

  if (activitiesQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }

  if (!currentId) {
    return (
      <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
        暂无活动
      </p>
    );
  }

  const summary = stats?.summary;
  const value = (number?: number) => (summary ? formatCount(number) : "—");

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Select value={String(currentId)} onValueChange={(next) => onActivityChange(Number(next))}>
          <SelectTrigger className="h-8 w-60">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {activities.map((activity) => (
              <SelectItem key={activity.id} value={String(activity.id)}>
                {activity.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <StatTile label="今日抽奖" icon={CalendarCheck} value={value(summary?.todayDraws)} />
        <StatTile label="今日人数" icon={Users} value={value(summary?.todayUsers)} />
        <StatTile label="今日中奖" icon={Trophy} value={value(summary?.todayWins)} />
        <StatTile label="累计抽奖" icon={Dices} value={value(summary?.totalDraws)} />
        <StatTile label="累计人数" icon={Users} value={value(summary?.totalUsers)} />
        <StatTile label="消耗积分" icon={Coins} value={value(summary?.pointsSpent)} />
        <button
          type="button"
          className="text-left"
          onClick={() => onOpenPending(currentId)}
          disabled={!summary?.pending}
        >
          <StatTile
            label="待发放"
            icon={Hourglass}
            tone={summary?.pending ? "default" : "muted"}
            value={value(summary?.pending)}
          />
        </button>
        <StatTile label="保底触发" icon={ShieldCheck} value={value(summary?.pityHits)} />
        <StatTile label="已拥有改发" icon={ArrowRightLeft} value={value(summary?.converted ?? 0)} />
        <StatTile label="折算积分" icon={HandCoins} value={value(summary?.convertedPoints ?? 0)} />
      </div>

      <ChartCard title="趋势" loading={statsQuery.isLoading} empty={trendEmpty} height={240}>
        <ChartContainer config={trendConfig} className="h-[240px] w-full">
          <ComposedChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={36} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar dataKey="draws" fill="var(--color-draws)" radius={[3, 3, 0, 0]} />
            <Bar dataKey="wins" fill="var(--color-wins)" radius={[3, 3, 0, 0]} />
            <Line dataKey="users" type="monotone" stroke="var(--color-users)" strokeWidth={2} dot={false} />
            <ChartLegend content={<ChartLegendContent />} />
          </ComposedChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        <ChartCard title={`次数来源（${days} 天）`} loading={statsQuery.isLoading} empty={sourceEmpty} height={180}>
          <ChartContainer config={sourceConfig} className="h-[180px] w-full">
            <BarChart data={sourceRows} layout="vertical" margin={{ left: 0, right: 16, top: 4 }}>
              <CartesianGrid horizontal={false} strokeDasharray="3 3" />
              <XAxis type="number" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} />
              <YAxis type="category" dataKey="label" tickLine={false} axisLine={false} fontSize={11} width={40} />
              <ChartTooltip content={<ChartTooltipContent hideLabel nameKey="key" />} />
              <Bar dataKey="draws" radius={[0, 3, 3, 0]} />
            </BarChart>
          </ChartContainer>
        </ChartCard>

        <div className="rounded-xl border border-border">
          <div className="flex items-center gap-1.5 border-b border-border px-4 py-3 text-sm font-medium">
            <Sparkles className="size-3.5 text-muted-foreground" />
            奖品
          </div>
          {statsQuery.isLoading ? (
            <div className="space-y-2 p-4">
              {[0, 1, 2].map((index) => (
                <Skeleton key={index} className="h-8 w-full" />
              ))}
            </div>
          ) : !stats?.prizes.length ? (
            <p className="px-4 py-8 text-center text-xs text-muted-foreground">暂无奖品</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>奖品</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead className="text-right">{days} 天抽中</TableHead>
                  <TableHead className="text-right">其中改发</TableHead>
                  <TableHead className="text-right">库存</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {stats.prizes.map((prize, index) => (
                  <TableRow key={`${prize.prizeId ?? "deleted"}-${index}`}>
                    <TableCell className="text-xs">
                      {prize.name}
                      {prize.prizeId ? null : <span className="ml-1.5 text-muted-foreground">已删除</span>}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{kindLabelOf(prize.kind, kinds)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{formatCount(prize.draws)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums text-muted-foreground">
                      {prize.converted ? formatCount(prize.converted) : "—"}
                    </TableCell>
                    <TableCell className="text-right text-xs tabular-nums text-muted-foreground">
                      {prize.quantity < 0 ? "不限" : `${formatCount(prize.used)}/${formatCount(prize.quantity)}`}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </div>

      <LotteryAuditCard
        appKey={appKey}
        activityId={currentId}
        days={days}
        prizes={activities.find((activity) => activity.id === currentId)?.prizes ?? []}
      />
    </div>
  );
}
