"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { CalendarCheck, Coins, Hourglass, PackageOpen, Repeat2, Users } from "lucide-react";
import { toast } from "sonner";
import { StatusDot } from "@/components/apps/app-config-primitives";
import { formatPoints } from "@/components/apps/points-store/points-store-shared";
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
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import {
  usePointsStoreSettingsQuery,
  usePointsStoreStatsQuery,
  useSavePointsStoreSettingsMutation
} from "@/lib/points-store-hooks";

/** 笔数与人数同量级，放一张图；消耗积分高出两三个数量级，单独一张，否则前两者贴着横轴。 */
const COUNT_SERIES: Array<{ key: "redemptions" | "redeemers"; label: string; color: ThemedColor }> = [
  { key: "redemptions", label: "兑换笔数", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "redeemers", label: "兑换人数", color: { light: "#0ea5e9", dark: "#38bdf8" } }
];

const POINTS_SERIES: Array<{ key: "pointsSpent"; label: string; color: ThemedColor }> = [
  { key: "pointsSpent", label: "消耗积分", color: { light: "#f59e0b", dark: "#fbbf24" } }
];

const RANGES = [7, 14, 30] as const;

export function PointsStoreOverviewPanel({
  appKey,
  onOpenPending
}: {
  appKey: string;
  onOpenPending: () => void;
}) {
  const [days, setDays] = useState<number>(14);
  const settingsQuery = usePointsStoreSettingsQuery(appKey);
  const statsQuery = usePointsStoreStatsQuery(appKey, days);
  const saveMutation = useSavePointsStoreSettingsMutation(appKey);

  const settings = settingsQuery.data;
  const stats = statsQuery.data;
  const countConfig = useMemo(() => buildChartConfig(COUNT_SERIES), []);
  const pointsConfig = useMemo(() => buildChartConfig(POINTS_SERIES), []);
  const rows = useMemo(() => (stats?.trend ?? []).map((day) => ({ ...day, label: day.date.slice(5) })), [stats]);
  const empty = rows.every((row) => row.redemptions === 0);

  const toggle = async (enabled: boolean) => {
    try {
      await saveMutation.mutateAsync(enabled);
      toast.success(enabled ? "积分商城已开启" : "积分商城已关闭");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  if (settingsQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }

  const rangePicker = (
    <ToggleGroup
      type="single"
      size="sm"
      value={String(days)}
      onValueChange={(value) => value && setDays(Number(value))}
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
        <StatusDot active={Boolean(settings?.enabled)} labelActive="积分商城已开启" labelInactive="积分商城未开启" />
        <label className="flex items-center gap-2 text-xs font-medium">
          开启
          <Switch
            checked={Boolean(settings?.enabled)}
            onCheckedChange={toggle}
            disabled={saveMutation.isPending}
            aria-label="开启积分商城"
          />
        </label>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <StatTile label="今日兑换" icon={CalendarCheck} value={stats ? formatPoints(stats.summary.today) : "—"} />
        <StatTile label={`${days} 天兑换笔数`} icon={Repeat2} value={stats ? formatPoints(stats.summary.redemptions) : "—"} />
        <StatTile label={`${days} 天兑换人数`} icon={Users} value={stats ? formatPoints(stats.summary.redeemers) : "—"} />
        <StatTile label={`${days} 天消耗积分`} icon={Coins} value={stats ? formatPoints(stats.summary.pointsSpent) : "—"} />
        <button type="button" className="text-left" onClick={onOpenPending} disabled={!stats?.summary.pending}>
          <StatTile
            label="待发放"
            icon={Hourglass}
            tone={stats?.summary.pending ? "default" : "muted"}
            value={stats ? formatPoints(stats.summary.pending) : "—"}
          />
        </button>
        <StatTile label="在售商品" icon={PackageOpen} value={stats ? formatPoints(stats.summary.activeItems) : "—"} />
      </div>

      <ChartCard title="兑换" loading={statsQuery.isLoading} empty={empty} height={220} action={rangePicker}>
        <ChartContainer config={countConfig} className="h-[220px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {COUNT_SERIES.map((series) => (
              <Bar key={series.key} dataKey={series.key} fill={`var(--color-${series.key})`} radius={[3, 3, 0, 0]} />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <ChartCard title="消耗积分" loading={statsQuery.isLoading} empty={empty} height={180}>
        <ChartContainer config={pointsConfig} className="h-[180px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={44} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar dataKey="pointsSpent" fill="var(--color-pointsSpent)" radius={[3, 3, 0, 0]} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <div className="rounded-xl border border-border">
        <div className="border-b border-border px-4 py-3 text-sm font-medium">热门商品</div>
        {statsQuery.isLoading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-8 w-full" />
            ))}
          </div>
        ) : !stats?.topItems.length ? (
          <p className="px-4 py-8 text-center text-xs text-muted-foreground">暂无兑换</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>商品</TableHead>
                <TableHead className="text-right">兑换笔数</TableHead>
                <TableHead className="text-right">消耗积分</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.topItems.map((item, index) => (
                <TableRow key={`${item.itemId ?? "deleted"}-${index}`}>
                  <TableCell className="text-xs">
                    {item.title}
                    {item.itemId ? null : <span className="ml-1.5 text-muted-foreground">已删除</span>}
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums">{formatPoints(item.redemptions)}</TableCell>
                  <TableCell className="text-right text-xs tabular-nums">{formatPoints(item.pointsSpent)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>
    </div>
  );
}
