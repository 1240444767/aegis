"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  BadgeCheck,
  CalendarCheck,
  Gauge,
  Hourglass,
  KeyRound,
  Megaphone,
  SlidersHorizontal,
  UserPlus,
  Users
} from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import type { InviteRelationScope } from "@/components/apps/invite/invite-relations-panel";
import { formatCount } from "@/components/apps/invite/invite-shared";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Button } from "@/components/ui/button";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
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
import type { InviterStatus } from "@/lib/api/invite";
import { useInviteSettingsQuery, useInviteStatsQuery } from "@/lib/invite-hooks";

const SERIES: Array<{ key: "relations" | "granted"; label: string; color: ThemedColor }> = [
  { key: "relations", label: "新增邀请", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "granted", label: "邀请人奖励已发放", color: { light: "#10b981", dark: "#34d399" } }
];

const RANGES = [7, 14, 30] as const;

/**
 * 概览。汇总数字是累计值（今日除外），趋势、邀请排行与活动码排行按所选天数计；
 * 日期按服务器时区分桶，与后端统计口径一致。
 */
export function InviteOverviewPanel({
  appKey,
  onOpenConfig,
  onOpenRelations
}: {
  appKey: string;
  onOpenConfig: () => void;
  onOpenRelations: (scope: InviteRelationScope, status?: InviterStatus) => void;
}) {
  const [days, setDays] = useState<number>(14);
  const settingsQuery = useInviteSettingsQuery(appKey);
  const statsQuery = useInviteStatsQuery(appKey, days);

  const settings = settingsQuery.data;
  const stats = statsQuery.data;
  const summary = stats?.summary;
  const config = useMemo(() => buildChartConfig(SERIES), []);
  const rows = useMemo(() => (stats?.trend ?? []).map((day) => ({ ...day, label: day.date.slice(5) })), [stats]);
  const empty = rows.every((row) => row.relations === 0 && row.granted === 0);
  const value = (count?: number) => (summary ? formatCount(count) : "—");

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
          <Skeleton className="h-4 w-28" />
        ) : (
          <StatusDot active={Boolean(settings?.enabled)} labelActive="邀请已开启" labelInactive="邀请未开启" />
        )}
        <Button size="sm" variant="outline" className="h-8" onClick={onOpenConfig}>
          <SlidersHorizontal className="size-3.5" /> 配置
        </Button>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="今日邀请" icon={CalendarCheck} value={value(summary?.today)} />
        <StatTile
          label="累计邀请"
          icon={UserPlus}
          value={value(summary?.relations)}
          hint={summary ? `注册时填写 ${formatCount(summary.fromRegister)}，补填 ${formatCount(summary.fromBind)}` : undefined}
        />
        <StatTile label="邀请人" icon={Users} value={value(summary?.inviters)} />
        <StatTile label="邀请人奖励已发放" icon={BadgeCheck} value={value(summary?.granted)} />
        <button
          type="button"
          className="text-left"
          onClick={() => onOpenRelations({}, "pending")}
          disabled={!summary?.pending}
        >
          <StatTile
            label="等待发放"
            icon={Hourglass}
            tone={summary?.pending ? "default" : "muted"}
            value={value(summary?.pending)}
          />
        </button>
        <button
          type="button"
          className="text-left"
          onClick={() => onOpenRelations({}, "limited")}
          disabled={!summary?.limited}
        >
          <StatTile
            label="达到上限未发放"
            icon={Gauge}
            tone={summary?.limited ? "default" : "muted"}
            value={value(summary?.limited)}
          />
        </button>
        <StatTile label="个人码" icon={KeyRound} value={value(summary?.personalCodes)} />
        <StatTile label="活动码" icon={Megaphone} value={value(summary?.campaignCodes)} />
      </div>

      <ChartCard title="邀请趋势" loading={statsQuery.isLoading} empty={empty} height={220} action={rangePicker}>
        <ChartContainer config={config} className="h-[220px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
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
        <div className="rounded-xl border border-border">
          <div className="border-b border-border px-4 py-3 text-sm font-medium">邀请排行</div>
          {statsQuery.isLoading ? (
            <ListSkeleton />
          ) : !stats?.topInviters.length ? (
            <p className="px-4 py-8 text-center text-xs text-muted-foreground">暂无邀请</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>邀请人</TableHead>
                  <TableHead className="text-right">邀请</TableHead>
                  <TableHead className="text-right">已发放</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {stats.topInviters.map((item) => {
                  const label = item.nickname || item.account || `#${item.userId}`;
                  return (
                    <TableRow
                      key={item.userId}
                      className="cursor-pointer"
                      onClick={() => onOpenRelations({ inviter: { id: item.userId, label } })}
                    >
                      <TableCell className="text-xs">
                        <span className="font-medium">{label}</span>
                        {item.nickname && item.account ? (
                          <span className="ml-1.5 text-muted-foreground">{item.account}</span>
                        ) : null}
                      </TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{formatCount(item.invited)}</TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{formatCount(item.granted)}</TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </div>

        <div className="rounded-xl border border-border">
          <div className="border-b border-border px-4 py-3 text-sm font-medium">活动码排行</div>
          {statsQuery.isLoading ? (
            <ListSkeleton />
          ) : !stats?.topCodes.length ? (
            <p className="px-4 py-8 text-center text-xs text-muted-foreground">暂无活动码使用记录</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>活动码</TableHead>
                  <TableHead className="text-right">使用</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {stats.topCodes.map((item) => (
                  <TableRow
                    key={item.codeId}
                    className="cursor-pointer"
                    onClick={() => onOpenRelations({ code: { id: item.codeId, code: item.code } })}
                  >
                    <TableCell className="text-xs">
                      <span className="font-mono font-medium">{item.code}</span>
                      {item.title ? <span className="ml-1.5 text-muted-foreground">{item.title}</span> : null}
                    </TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{formatCount(item.used)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </div>
    </div>
  );
}

function ListSkeleton() {
  return (
    <div className="space-y-2 p-4">
      {[0, 1, 2].map((index) => (
        <Skeleton key={index} className="h-8 w-full" />
      ))}
    </div>
  );
}
