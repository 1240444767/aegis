"use client";

import { useState } from "react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, XAxis, YAxis } from "recharts";
import { AlertTriangle, ArrowUpRight, Gift, Hourglass, Loader2, TrendingUp, Users } from "lucide-react";
import { formatNumber } from "@/components/apps/levels/level-meta";
import { MetricCard, Section, StateBlock } from "@/components/users/detail/kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Progress } from "@/components/ui/progress";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useLevelOverviewQuery } from "@/lib/level-hooks";

/**
 * 等级总览：人数分布（每级多少人）、升级与奖励趋势、奖励各状态、正在跑的重算任务。
 */

const DISTRIBUTION_CONFIG: ChartConfig = { users: { label: "人数", color: "var(--chart-1)" } };
const TREND_CONFIG: ChartConfig = {
  levelUps: { label: "升级人次", color: "var(--chart-1)" },
  rewards: { label: "奖励到账", color: "var(--chart-2)" }
};

export function LevelOverview({
  appKey,
  onOpenRewards,
  onOpenLevel
}: {
  appKey: string;
  onOpenRewards: (status: string) => void;
  onOpenLevel: (level: number) => void;
}) {
  const [days, setDays] = useState(14);
  const query = useLevelOverviewQuery(appKey, days);
  const data = query.data;
  const loading = query.isLoading;

  if (query.error) {
    return <StateBlock kind="error" description={query.error instanceof Error ? query.error.message : "加载失败"} onRetry={() => void query.refetch()} />;
  }

  const job = data?.runningJob;
  return (
    <div className="space-y-4">
      {job ? (
        <div className="flex items-center gap-3 rounded-xl border border-sky-500/30 bg-sky-500/5 px-4 py-3">
          <Loader2 className="size-4 shrink-0 animate-spin text-sky-500" />
          <div className="min-w-0 flex-1">
            <div className="text-xs font-medium">
              重算任务进行中：{formatNumber(job.processed)} / {formatNumber(job.total)} 人，已变动 {formatNumber(job.changed)} 人
            </div>
            <Progress className="mt-1.5 h-1.5" value={job.total ? (job.processed / job.total) * 100 : 0} />
          </div>
        </div>
      ) : null}

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <MetricCard label="用户" value={formatNumber(data?.totalUsers)} sub={`平均 Lv.${data?.averageLevel ?? "—"}`} icon={Users} tone="primary" loading={loading} />
        <MetricCard
          label="今日升级"
          value={formatNumber(data?.levelUpsToday)}
          sub={`近 7 天 ${formatNumber(data?.levelUps7d)} 人次`}
          icon={TrendingUp}
          tone="success"
          loading={loading}
          spark={data?.trend.map((point) => point.levelUps)}
        />
        <MetricCard label="奖励已到账" value={formatNumber(data?.rewards.granted)} sub="份" icon={Gift} tone="info" loading={loading} onClick={() => onOpenRewards("granted")} />
        <MetricCard
          label="待用户领取"
          value={formatNumber(data?.rewards.claimable)}
          sub={data?.rewards.queued ? `${data.rewards.queued} 份发放中` : "份"}
          icon={ArrowUpRight}
          tone="violet"
          loading={loading}
          onClick={() => onOpenRewards("claimable")}
        />
        <MetricCard
          label="待人工处理"
          value={formatNumber(data?.rewards.pending)}
          sub="点击处理"
          icon={Hourglass}
          tone={data?.rewards.pending ? "warning" : "neutral"}
          loading={loading}
          onClick={() => onOpenRewards("pending")}
        />
        <MetricCard
          label="发放失败"
          value={formatNumber(data?.rewards.failed)}
          sub="点击重试"
          icon={AlertTriangle}
          tone={data?.rewards.failed ? "danger" : "neutral"}
          loading={loading}
          onClick={() => onOpenRewards("failed")}
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Section title="等级分布" description={`当前体系${data?.version ? `第 ${data.version} 版` : "（默认体系）"}下每一级的人数；点柱子查看这一级的用户`}>
          <ChartContainer config={DISTRIBUTION_CONFIG} className="h-[260px] w-full">
            <BarChart data={(data?.distribution ?? []).map((item) => ({ ...item, label: `Lv.${item.level}` }))} margin={{ left: 0, right: 8, top: 8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} interval="preserveStartEnd" />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={40} />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    labelFormatter={(_, payload) => {
                      const item = payload?.[0]?.payload as { level: number; name: string; threshold: number } | undefined;
                      return item ? `Lv.${item.level} ${item.name}（${formatNumber(item.threshold)} 经验起）` : "";
                    }}
                  />
                }
              />
              <Bar dataKey="users" radius={[6, 6, 0, 0]} className="cursor-pointer" onClick={(entry) => onOpenLevel((entry as unknown as { level: number }).level)}>
                {(data?.distribution ?? []).map((item) => (
                  <Cell key={item.level} fill={item.color || "var(--color-users)"} />
                ))}
              </Bar>
            </BarChart>
          </ChartContainer>
        </Section>

        <Section
          title="升级与奖励趋势"
          description="按天的升级人次与奖励到账份数"
          actions={
            <ToggleGroup type="single" variant="outline" size="sm" value={String(days)} onValueChange={(value) => value && setDays(Number(value))}>
              {[7, 14, 30, 90].map((value) => (
                <ToggleGroupItem key={value} value={String(value)} className="px-2.5 text-xs">
                  {value} 天
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          }
        >
          <ChartContainer config={TREND_CONFIG} className="h-[260px] w-full">
            <AreaChart data={(data?.trend ?? []).map((point) => ({ ...point, label: point.date.slice(5) }))} margin={{ left: 0, right: 8, top: 8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={36} />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Area dataKey="levelUps" type="monotone" stroke="var(--color-levelUps)" strokeWidth={2} fill="var(--color-levelUps)" fillOpacity={0.12} />
              <Area dataKey="rewards" type="monotone" stroke="var(--color-rewards)" strokeWidth={2} fill="transparent" />
            </AreaChart>
          </ChartContainer>
        </Section>
      </div>
    </div>
  );
}
