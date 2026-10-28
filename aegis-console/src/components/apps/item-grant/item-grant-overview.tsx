"use client";

import { useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { AlertTriangle, Gift, Hourglass, Loader2, Users, Workflow, Zap } from "lucide-react";
import { ITEM_ICON, formatNumber } from "@/components/apps/item-grant/item-grant-meta";
import { MetricCard, Section, StateBlock } from "@/components/users/detail/kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useItemGrantOverviewQuery } from "@/lib/item-grant-hooks";

/**
 * 物品发放总览：规则与发放量、待处理与失败、近 N 天趋势、近 30 天按物品汇总。
 */

const TREND_CONFIG: ChartConfig = {
  records: { label: "发放次数", color: "var(--chart-1)" },
  users: { label: "覆盖人数", color: "var(--chart-2)" }
};

export function ItemGrantOverview({
  appKey,
  onOpenPending,
  onOpenFailed
}: {
  appKey: string;
  onOpenPending: () => void;
  onOpenFailed: () => void;
}) {
  const [days, setDays] = useState(14);
  const query = useItemGrantOverviewQuery(appKey, days);
  const data = query.data;
  const loading = query.isLoading;
  const maxCount = Math.max(1, ...(data?.byItem ?? []).map((item) => item.count));

  if (query.error) {
    return <StateBlock kind="error" description={query.error instanceof Error ? query.error.message : "加载失败"} onRetry={() => void query.refetch()} />;
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <MetricCard label="生效规则" value={formatNumber(data?.activeRules)} sub={`共 ${formatNumber(data?.rules)} 条`} icon={Workflow} tone="primary" loading={loading} />
        <MetricCard
          label="累计发放"
          value={formatNumber(data?.records)}
          sub="次"
          icon={Gift}
          tone="success"
          loading={loading}
          spark={data?.trend.map((point) => point.records)}
        />
        <MetricCard label="覆盖人数" value={formatNumber(data?.users)} sub="人" icon={Users} tone="info" loading={loading} />
        <MetricCard
          label="今日发放"
          value={formatNumber(data?.today)}
          sub={data?.running ? `${data.running} 个执行中` : "次"}
          icon={data?.running ? Loader2 : Zap}
          tone="violet"
          loading={loading}
        />
        <MetricCard
          label="待人工发放"
          value={formatNumber(data?.pending)}
          sub="点击处理"
          icon={Hourglass}
          tone={data?.pending ? "warning" : "neutral"}
          loading={loading}
          onClick={onOpenPending}
        />
        <MetricCard
          label="发放失败"
          value={formatNumber(data?.failed)}
          sub="点击重试"
          icon={AlertTriangle}
          tone={data?.failed ? "danger" : "neutral"}
          loading={loading}
          onClick={onOpenFailed}
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Section
          title="发放趋势"
          description="按天的发放次数与覆盖人数（不含失败）"
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
          <ChartContainer config={TREND_CONFIG} className="h-[240px] w-full">
            <AreaChart data={(data?.trend ?? []).map((point) => ({ ...point, label: point.date.slice(5) }))} margin={{ left: 0, right: 8, top: 8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={36} />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Area dataKey="records" type="monotone" stroke="var(--color-records)" strokeWidth={2} fill="var(--color-records)" fillOpacity={0.12} />
              <Area dataKey="users" type="monotone" stroke="var(--color-users)" strokeWidth={2} fill="transparent" />
            </AreaChart>
          </ChartContainer>
        </Section>

        <Section title="近 30 天发了什么" description="按物品类型汇总的份数与数量">
          {data && data.byItem.length === 0 ? (
            <StateBlock compact icon={Gift} title="近 30 天还没有发放" />
          ) : (
            <div className="space-y-3">
              {(data?.byItem ?? []).map((item) => {
                const Icon = ITEM_ICON[item.type] ?? Gift;
                return (
                  <div key={item.type} className="space-y-1">
                    <div className="flex items-center justify-between gap-2 text-xs">
                      <span className="flex items-center gap-1.5 font-medium">
                        <Icon className="size-3.5 text-muted-foreground" />
                        {item.label || item.type}
                      </span>
                      <span className="tabular-nums text-muted-foreground">
                        {formatNumber(item.count)} 份{item.amount ? `，共 ${formatNumber(item.amount)}` : ""}
                      </span>
                    </div>
                    <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                      <div className="h-full rounded-full bg-primary/70" style={{ width: `${(item.count / maxCount) * 100}%` }} />
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Section>
      </div>
    </div>
  );
}
