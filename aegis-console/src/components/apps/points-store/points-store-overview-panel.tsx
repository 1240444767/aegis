"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  ArrowRight,
  CalendarCheck,
  Coins,
  Hourglass,
  PackageCheck,
  PackageOpen,
  Plus,
  Repeat2,
  Trophy,
  Undo2,
  UserRound,
  Users
} from "lucide-react";
import { RedemptionHandleDialog, type HandleRequest } from "@/components/apps/points-store/points-store-handle-dialog";
import {
  SHELF_META,
  SHELF_ORDER,
  formatPoints,
  rewardVisual,
  shelfKey,
  type ShelfKey
} from "@/components/apps/points-store/points-store-shared";
import { MetricGrid } from "@/components/users/detail/records/records-shared";
import { MetricCard, Section, StateBlock, ToneIcon } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { PointsStoreRedemptionStatus } from "@/lib/api/points-store";
import { formatRelative } from "@/lib/datetime";
import {
  usePointsStoreItemsQuery,
  usePointsStoreRedemptionsQuery,
  usePointsStoreStatsQuery
} from "@/lib/points-store-hooks";
import { cn } from "@/lib/utils";

const RANGES = [7, 14, 30, 90] as const;

const METRICS = {
  redemptions: { label: "兑换笔数", unit: "笔", color: { light: "#6366f1", dark: "#818cf8" } },
  redeemers: { label: "兑换人数", unit: "人", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  pointsSpent: { label: "消耗积分", unit: "积分", color: { light: "#f59e0b", dark: "#fbbf24" } }
} as const;

type MetricKey = keyof typeof METRICS;

/**
 * 概览。
 *
 * 指标卡回答「这段时间兑了多少」，走势图一次只画一个量（笔数、人数与积分差两三个数量级，
 * 叠在一张图上前两者会贴着横轴），热门商品看谁在拉动兑换，右栏是商品状态构成与待处理队列。
 * 时间窗按浏览器时区切日（stats 接口带 tz）。
 */
export function PointsStoreOverviewPanel({
  appKey,
  onOpenItems,
  onOpenRedemptions
}: {
  appKey: string;
  onOpenItems: () => void;
  onOpenRedemptions: (itemId?: number, status?: PointsStoreRedemptionStatus) => void;
}) {
  const [days, setDays] = useState<number>(14);
  const [metric, setMetric] = useState<MetricKey>("redemptions");
  const statsQuery = usePointsStoreStatsQuery(appKey, days);
  const itemsQuery = usePointsStoreItemsQuery(appKey);
  const stats = statsQuery.data;
  const loading = statsQuery.isLoading;

  const rows = useMemo(() => (stats?.trend ?? []).map((day) => ({ ...day, label: day.date.slice(5).replace("-", "/") })), [stats]);
  const series = (key: MetricKey) => rows.map((row) => row[key]);
  const summary = stats?.summary;
  const perUser = summary && summary.redeemers ? Math.round(summary.pointsSpent / summary.redeemers) : 0;
  const empty = !loading && rows.every((row) => row.redemptions === 0);
  const chartConfig = useMemo<ChartConfig>(
    () => ({ [metric]: { label: METRICS[metric].label, theme: METRICS[metric].color } }),
    [metric]
  );

  if (!itemsQuery.isLoading && itemsQuery.data?.length === 0) {
    return (
      <Section title="还没有商品" icon={PackageOpen} tone="primary" description="先上架几件商品，用户才能用积分兑换">
        <StateBlock
          icon={PackageOpen}
          title="积分商城是空的"
          description="可以兑换会员套餐、会员天数、功能解锁、经验、余额、抽奖次数，或需要人工发放的实物与兑换码。"
          action={
            <Button size="sm" onClick={onOpenItems}>
              <Plus />
              去新建商品
            </Button>
          }
        />
      </Section>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">按浏览器时区统计，已取消的兑换不计入</p>
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          value={String(days)}
          onValueChange={(value) => value && setDays(Number(value))}
        >
          {RANGES.map((range) => (
            <ToggleGroupItem key={range} value={String(range)} className="px-3 text-xs">
              {range} 天
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      <MetricGrid>
        <MetricCard label="今日兑换" icon={CalendarCheck} tone="primary" loading={loading} value={formatPoints(summary?.today)} sub="笔，自本地零点起" />
        <MetricCard
          label={`${days} 天兑换笔数`}
          icon={Repeat2}
          tone="primary"
          loading={loading}
          value={formatPoints(summary?.redemptions)}
          sub={`日均 ${formatPoints(Math.round((summary?.redemptions ?? 0) / days))} 笔`}
          spark={series("redemptions")}
        />
        <MetricCard
          label={`${days} 天兑换人数`}
          icon={Users}
          tone="info"
          loading={loading}
          value={formatPoints(summary?.redeemers)}
          sub="去重后的用户数"
          spark={series("redeemers")}
        />
        <MetricCard
          label={`${days} 天消耗积分`}
          icon={Coins}
          tone="warning"
          loading={loading}
          value={formatPoints(summary?.pointsSpent)}
          sub={`人均 ${formatPoints(perUser)} 积分`}
          spark={series("pointsSpent")}
        />
        <MetricCard
          label="待发放"
          icon={Hourglass}
          tone={summary?.pending ? "warning" : "neutral"}
          loading={loading}
          value={formatPoints(summary?.pending)}
          sub={summary?.pending ? "点击去处理" : "没有积压"}
          onClick={summary?.pending ? () => onOpenRedemptions(undefined, "pending") : undefined}
        />
        <MetricCard
          label="在售商品"
          icon={PackageOpen}
          tone="success"
          loading={loading}
          value={formatPoints(summary?.activeItems)}
          sub={itemsQuery.data ? `共 ${itemsQuery.data.length} 件商品` : undefined}
          onClick={onOpenItems}
        />
        <MetricCard
          label={`${days} 天已取消`}
          icon={Undo2}
          tone="neutral"
          loading={loading}
          value={formatPoints(summary?.cancelled)}
          sub="积分已退回"
          onClick={summary?.cancelled ? () => onOpenRedemptions(undefined, "cancelled") : undefined}
        />
        <MetricCard
          label="人均兑换"
          icon={UserRound}
          tone="violet"
          loading={loading}
          value={summary?.redeemers ? (summary.redemptions / summary.redeemers).toFixed(1) : "—"}
          sub={`${days} 天内每人平均笔数`}
        />
      </MetricGrid>

      <Section
        title="兑换走势"
        icon={METRIC_ICON[metric]}
        tone="primary"
        description={`近 ${days} 天每日${METRICS[metric].label}`}
        actions={
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            value={metric}
            onValueChange={(value) => value && setMetric(value as MetricKey)}
          >
            {(Object.keys(METRICS) as MetricKey[]).map((key) => (
              <ToggleGroupItem key={key} value={key} className="px-3 text-xs">
                {METRICS[key].label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
      >
        {loading ? (
          <Skeleton className="h-[260px] w-full rounded-lg" />
        ) : empty ? (
          <StateBlock compact title="这段时间没有兑换" description="换个时间范围看看，或检查商城是否开放、商品是否在售" />
        ) : (
          <ChartContainer config={chartConfig} className="h-[260px] w-full">
            <AreaChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={16} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={metric === "pointsSpent" ? 52 : 32} />
              <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
              <Area
                type="monotone"
                dataKey={metric}
                stroke={`var(--color-${metric})`}
                fill={`var(--color-${metric})`}
                fillOpacity={0.12}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
            </AreaChart>
          </ChartContainer>
        )}
      </Section>

      <div className="grid gap-4 lg:grid-cols-5">
        <TopItems
          className="lg:col-span-3"
          days={days}
          loading={loading}
          items={stats?.topItems ?? []}
          rewardTypes={new Map((itemsQuery.data ?? []).map((item) => [item.id, item.rewards[0]?.type]))}
          onOpen={(itemId) => onOpenRedemptions(itemId)}
        />
        <div className="flex flex-col gap-4 lg:col-span-2">
          <ShelfMix appKey={appKey} onOpenItems={onOpenItems} />
          <PendingQueue appKey={appKey} total={summary?.pending ?? 0} onOpenAll={() => onOpenRedemptions(undefined, "pending")} />
        </div>
      </div>
    </div>
  );
}

const METRIC_ICON: Record<MetricKey, typeof Repeat2> = { redemptions: Repeat2, redeemers: Users, pointsSpent: Coins };

/** 热门商品：名次、权益图标、占比条。点一行看该商品的兑换记录。 */
function TopItems({
  className,
  days,
  loading,
  items,
  rewardTypes,
  onOpen
}: {
  className?: string;
  days: number;
  loading: boolean;
  items: Array<{ itemId?: number; title: string; redemptions: number; pointsSpent: number }>;
  rewardTypes: Map<number, string | undefined>;
  onOpen: (itemId?: number) => void;
}) {
  const max = Math.max(1, ...items.map((item) => item.redemptions));
  const total = items.reduce((sum, item) => sum + item.redemptions, 0);
  return (
    <Section className={className} flush title="热门商品" icon={Trophy} tone="warning" description={`近 ${days} 天兑换最多的商品`}>
      {loading ? (
        <div className="flex flex-col gap-3 p-5">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : !items.length ? (
        <StateBlock compact title="暂无兑换" />
      ) : (
        <ol className="flex flex-col divide-y">
          {items.map((item, index) => {
            const visual = rewardVisual(item.itemId ? rewardTypes.get(item.itemId) : undefined);
            return (
              <li key={`${item.itemId ?? "deleted"}-${index}`}>
                <button
                  type="button"
                  disabled={!item.itemId}
                  onClick={() => onOpen(item.itemId)}
                  className="group flex w-full items-center gap-3 px-4 py-3 text-left transition-colors enabled:hover:bg-muted/40 sm:px-5"
                >
                  <span
                    className={cn(
                      "w-5 shrink-0 text-center text-sm font-semibold tabular-nums",
                      index < 3 ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground"
                    )}
                  >
                    {index + 1}
                  </span>
                  <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
                  <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                    <div className="flex items-center justify-between gap-3">
                      <span className="truncate text-sm font-medium">
                        {item.title}
                        {item.itemId ? null : <span className="ml-1.5 text-xs font-normal text-muted-foreground">已删除</span>}
                      </span>
                      <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                        <span className="font-medium text-foreground">{formatPoints(item.redemptions)}</span> 笔
                        <span className="ml-2">{formatPoints(item.pointsSpent)} 积分</span>
                      </span>
                    </div>
                    <div className="flex items-center gap-2">
                      <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                        <div className="h-full rounded-full bg-primary/70" style={{ width: `${(item.redemptions / max) * 100}%` }} />
                      </div>
                      <span className="w-10 shrink-0 text-right text-xs tabular-nums text-muted-foreground">
                        {total ? `${Math.round((item.redemptions / total) * 100)}%` : "—"}
                      </span>
                    </div>
                  </div>
                  {item.itemId ? (
                    <ArrowRight className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                  ) : null}
                </button>
              </li>
            );
          })}
        </ol>
      )}
    </Section>
  );
}

/** 商品状态构成：一条分段条 + 图例 */
function ShelfMix({ appKey, onOpenItems }: { appKey: string; onOpenItems: () => void }) {
  const itemsQuery = usePointsStoreItemsQuery(appKey);
  const counts = useMemo(() => {
    const result = Object.fromEntries(SHELF_ORDER.map((key) => [key, 0])) as Record<ShelfKey, number>;
    for (const item of itemsQuery.data ?? []) result[shelfKey(item)] += 1;
    return result;
  }, [itemsQuery.data]);
  const total = itemsQuery.data?.length ?? 0;

  return (
    <Section
      title="商品状态"
      icon={PackageOpen}
      tone="success"
      actions={
        <Button variant="ghost" size="xs" onClick={onOpenItems}>
          管理商品
          <ArrowRight />
        </Button>
      }
    >
      {itemsQuery.isLoading ? (
        <Skeleton className="h-16 w-full" />
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-muted">
            {SHELF_ORDER.map((key) =>
              counts[key] ? (
                <div key={key} className={SHELF_META[key].color} style={{ width: `${(counts[key] / Math.max(total, 1)) * 100}%` }} />
              ) : null
            )}
          </div>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 sm:grid-cols-3 lg:grid-cols-2 xl:grid-cols-3">
            {SHELF_ORDER.map((key) => (
              <div key={key} className="flex items-center gap-2 text-xs">
                <span className={cn("size-2 rounded-full", SHELF_META[key].color)} />
                <span className="text-muted-foreground">{SHELF_META[key].label}</span>
                <span className="ml-auto font-medium tabular-nums">{counts[key]}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </Section>
  );
}

/** 待处理队列：最新的几笔待发放，可直接发放或取消 */
function PendingQueue({ appKey, total, onOpenAll }: { appKey: string; total: number; onOpenAll: () => void }) {
  const listQuery = usePointsStoreRedemptionsQuery(appKey, { status: "pending", page: 1, limit: 5 });
  const [request, setRequest] = useState<HandleRequest>(null);
  const items = listQuery.data?.items ?? [];

  return (
    <Section
      flush
      title="待处理"
      icon={Hourglass}
      tone="warning"
      description={total ? `${formatPoints(total)} 笔人工发放等待处理` : "人工发放的商品兑换后在这里处理"}
      actions={
        total > items.length ? (
          <Button variant="ghost" size="xs" onClick={onOpenAll}>
            全部
            <ArrowRight />
          </Button>
        ) : undefined
      }
    >
      {listQuery.isLoading ? (
        <div className="flex flex-col gap-2 p-4">
          {[0, 1].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : !items.length ? (
        <StateBlock compact icon={PackageCheck} title="没有待发放的兑换" />
      ) : (
        <ul className="flex flex-col divide-y">
          {items.map((item) => (
            <li key={item.id} className="flex items-center gap-3 px-4 py-2.5 sm:px-5">
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-sm font-medium">{item.itemTitle}</span>
                <span className="truncate text-xs text-muted-foreground">
                  {item.nickname || item.account || `#${item.userId}`}，{formatRelative(item.createdAt)}
                </span>
              </div>
              <Button size="xs" variant="outline" onClick={() => setRequest({ action: "fulfill", items: [item] })}>
                发放
              </Button>
              <Button
                size="xs"
                variant="ghost"
                className="text-muted-foreground"
                onClick={() => setRequest({ action: "cancel", items: [item] })}
              >
                取消
              </Button>
            </li>
          ))}
        </ul>
      )}
      <RedemptionHandleDialog appKey={appKey} request={request} onClose={() => setRequest(null)} />
    </Section>
  );
}
