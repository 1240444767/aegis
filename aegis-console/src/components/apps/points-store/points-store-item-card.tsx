"use client";

import type { ReactNode } from "react";
import { CalendarClock, Clock3, Hourglass, RefreshCw, Users } from "lucide-react";
import {
  CURRENT_PERIOD_LABEL,
  SHELF_META,
  describeReward,
  formatPoints,
  formatStoreDate,
  hasStock,
  itemRules,
  liveStock,
  openHoursLabel,
  restocks,
  rewardVisual,
  saleWindow,
  shelfKey
} from "@/components/apps/points-store/points-store-shared";
import { ToneIcon } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import type { PointsStoreAdminItem, PointsStoreItem, PointsStoreLimitPeriod, PointsStoreRewardSpec } from "@/lib/api/points-store";
import type { VipFeature, VipPlan } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

export type RewardContext = { catalog: PointsStoreRewardSpec[]; plans: VipPlan[]; features: VipFeature[] };

const SHELF_BADGE: Record<string, "success" | "info" | "warning" | "secondary"> = {
  success: "success",
  info: "info",
  warning: "warning",
  neutral: "secondary"
};

export function ShelfBadge({ item }: { item: PointsStoreItem }) {
  const meta = SHELF_META[shelfKey(item)];
  return (
    <Badge variant={SHELF_BADGE[meta.tone] ?? "secondary"} size="sm">
      {meta.label}
    </Badge>
  );
}

/** 价格：现价、划线价与折扣 */
export function ItemPrice({ item, compact }: { item: Pick<PointsStoreItem, "cost" | "originalCost">; compact?: boolean }) {
  const discount = item.originalCost && item.originalCost > item.cost ? Math.round((item.cost / item.originalCost) * 100) / 10 : null;
  return (
    <span className={cn("inline-flex items-baseline gap-1.5", compact && "justify-end")}>
      <span className={cn("font-semibold tabular-nums", compact ? "text-sm" : "text-2xl tracking-tight")}>{formatPoints(item.cost)}</span>
      <span className="text-xs text-muted-foreground">积分</span>
      {item.originalCost ? <span className="text-xs tabular-nums text-muted-foreground line-through">{formatPoints(item.originalCost)}</span> : null}
      {discount !== null && !compact ? (
        <span className="rounded bg-red-500/10 px-1 text-[11px] font-medium text-red-600 dark:text-red-400">{discount} 折</span>
      ) : null}
    </span>
  );
}

/** 一条细进度条：value / total，按余量着色 */
function Meter({ value, total, tone }: { value: number; total: number; tone: "ok" | "low" | "out" | "info" }) {
  const ratio = total > 0 ? Math.min(value / total, 1) : 0;
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
      <div
        className={cn(
          "h-full rounded-full transition-[width]",
          tone === "out" ? "bg-red-500" : tone === "low" ? "bg-amber-500" : tone === "info" ? "bg-sky-500" : "bg-emerald-500"
        )}
        style={{ width: `${Math.max(ratio * 100, value ? 2 : 0)}%` }}
      />
    </div>
  );
}

/**
 * 库存：不限量写「不限量」；固定库存画已兑 / 剩余；周期补货画本期剩余 / 补货量，并写下次补货时刻。
 * 剩余按换算后的此刻库存（liveStock），不会把上个周期的 0 当成「已兑完」。
 */
export function ItemStock({ item, compact }: { item: PointsStoreItem & { currentStock?: number; nextRestockAt?: string }; compact?: boolean }) {
  if (!hasStock(item)) return <span className="text-xs text-muted-foreground">不限量</span>;
  const stock = liveStock(item) ?? 0;
  const periodic = restocks(item);
  const total = periodic ? (item.restockQuantity ?? stock) : stock + (item.soldCount ?? 0);
  const low = stock <= Math.max(periodic ? 0 : 5, total * 0.1);
  const tone = stock <= 0 ? "out" : low ? "low" : "ok";
  const stockClass = stock <= 0 ? "text-red-600 dark:text-red-400" : low ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground";
  return (
    <div className="flex w-full flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className={cn("inline-flex items-center gap-1 tabular-nums", stockClass)}>
          {periodic ? <RefreshCw className="size-3" /> : null}
          {periodic ? `${CURRENT_PERIOD_LABEL[item.restockPeriod as PointsStoreLimitPeriod]}剩 ${formatPoints(stock)}/${formatPoints(total)}` : `剩 ${formatPoints(stock)}`}
        </span>
        {!compact ? (
          <span className="tabular-nums text-muted-foreground">
            {periodic && item.nextRestockAt ? `${formatStoreDate(item.nextRestockAt).slice(5)} 补货` : `已兑 ${formatPoints(item.soldCount)}`}
          </span>
        ) : null}
      </div>
      {periodic ? <Meter value={stock} total={total} tone={tone} /> : <Meter value={item.soldCount ?? 0} total={total} tone={tone} />}
    </div>
  );
}

/** 全站名额：本期已用 / 名额，满了标红 */
export function ItemQuota({ item }: { item: PointsStoreAdminItem }) {
  if (!item.globalLimitCount) return null;
  const used = item.globalUsed ?? 0;
  const full = used >= item.globalLimitCount;
  return (
    <div className="flex w-full flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className={cn("inline-flex items-center gap-1 tabular-nums", full ? "text-red-600 dark:text-red-400" : "text-muted-foreground")}>
          <Users className="size-3" />
          {CURRENT_PERIOD_LABEL[item.globalLimitPeriod]}名额 {formatPoints(used)}/{formatPoints(item.globalLimitCount)}
        </span>
        {item.globalResetsAt ? <span className="tabular-nums text-muted-foreground">{formatStoreDate(item.globalResetsAt).slice(5)} 清零</span> : null}
      </div>
      <Meter value={used} total={item.globalLimitCount} tone={full ? "out" : "info"} />
    </div>
  );
}

/** 每日开放时段与此刻是否开放 */
export function ItemHours({ item }: { item: PointsStoreAdminItem }) {
  const hours = openHoursLabel(item);
  if (!hours) return null;
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
      <span className={cn("size-1.5 rounded-full", item.openNow ? "bg-emerald-500" : "bg-zinc-400")} />
      <Clock3 className="size-3" />
      每日 {hours}
      <span className={item.openNow ? "text-emerald-600 dark:text-emerald-400" : ""}>{item.openNow ? "开放中" : "未开放"}</span>
    </span>
  );
}

/**
 * 商品卡。整卡点开编辑；左上角勾选框悬停或进入多选后出现；右上角状态与菜单。
 * 底部一栏是销售数据与上架开关，待发放用提醒色、可点进兑换记录。
 */
export function PointsStoreItemCard({
  item,
  reward,
  selected,
  selecting,
  onSelect,
  onToggle,
  onOpen,
  onPending,
  menu
}: {
  item: PointsStoreAdminItem;
  reward: RewardContext;
  selected: boolean;
  selecting: boolean;
  onSelect: (next: boolean) => void;
  onToggle: (next: boolean) => void;
  onOpen: () => void;
  onPending: () => void;
  menu: ReactNode;
}) {
  const visual = rewardVisual(item.rewards[0]?.type);
  // 补货、名额、开放时段在卡片上各有一条可视化，规则行里不再重复
  const rules = itemRules(item).filter((rule) => !/补货|全站|开放/.test(rule));
  const saleTime = saleWindow(item.startsAt, item.endsAt);
  const dimmed = item.status !== "active";

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(event) => {
        if (event.key === "Enter") onOpen();
      }}
      className={cn(
        "group relative flex cursor-pointer flex-col overflow-hidden rounded-xl border bg-card text-card-foreground transition-colors",
        "hover:border-foreground/20 focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none",
        selected && "border-primary ring-1 ring-primary"
      )}
    >
      <div className={cn("flex flex-1 flex-col gap-3 p-4", dimmed && "opacity-70")}>
        <div className="flex items-start justify-between gap-2">
          <div className="relative">
            <ToneIcon icon={visual.icon} tone={visual.tone} size="lg" className={cn("transition-opacity", (selected || selecting) && "opacity-0", "group-hover:opacity-0")} />
            <span
              className={cn(
                "absolute inset-0 flex items-center justify-center opacity-0 transition-opacity group-hover:opacity-100",
                (selected || selecting) && "opacity-100"
              )}
              onClick={(event) => event.stopPropagation()}
            >
              <Checkbox checked={selected} onCheckedChange={(next) => onSelect(Boolean(next))} aria-label="选择商品" className="size-5" />
            </span>
          </div>
          <div className="flex items-center gap-1" onClick={(event) => event.stopPropagation()}>
            <ShelfBadge item={item} />
            {menu}
          </div>
        </div>

        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex min-w-0 items-center gap-2">
            <h4 className="truncate text-[15px] font-semibold tracking-tight">{item.title}</h4>
            {item.badge ? (
              <span className="shrink-0 rounded-md bg-primary/10 px-1.5 py-px text-[11px] font-medium text-primary">{item.badge}</span>
            ) : null}
          </div>
          <p className="line-clamp-1 min-h-4 text-xs text-muted-foreground">{item.subtitle || item.description || " "}</p>
        </div>

        <div className="flex flex-wrap gap-1.5">
          {item.rewards.map((entry, index) => {
            const entryVisual = rewardVisual(entry.type);
            return (
              <span key={`${entry.type}-${index}`} className="inline-flex max-w-full items-center gap-1 rounded-md border bg-muted/40 px-1.5 py-0.5 text-xs">
                <entryVisual.icon className="size-3 shrink-0 text-muted-foreground" />
                <span className="truncate">{describeReward(entry, reward.catalog, reward.plans, reward.features)}</span>
              </span>
            );
          })}
        </div>

        <div className="mt-auto flex flex-col gap-2.5">
          <ItemPrice item={item} />
          <ItemStock item={item} />
          <ItemQuota item={item} />
          <ItemHours item={item} />
          {rules.length || saleTime ? (
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
              {rules.map((rule) => (
                <span key={rule}>{rule}</span>
              ))}
              {saleTime ? (
                <span className="inline-flex items-center gap-1">
                  <CalendarClock className="size-3" />
                  {saleTime}
                </span>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>

      <div className="flex items-center gap-3 border-t bg-muted/20 px-4 py-2.5 text-xs" onClick={(event) => event.stopPropagation()}>
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1 text-muted-foreground">
          <span className="tabular-nums">
            <span className="font-medium text-foreground">{formatPoints(item.redemptions)}</span> 笔
          </span>
          <span className="tabular-nums">{formatPoints(item.redeemers)} 人</span>
          <span className="tabular-nums">{formatPoints(item.pointsSpent)} 积分</span>
          {item.pending > 0 ? (
            <button
              type="button"
              onClick={onPending}
              className="inline-flex items-center gap-1 rounded-md bg-amber-500/10 px-1.5 py-0.5 font-medium text-amber-700 tabular-nums transition-colors hover:bg-amber-500/20 dark:text-amber-300"
            >
              <Hourglass className="size-3" />
              待发放 {item.pending}
            </button>
          ) : null}
        </div>
        <Switch checked={item.status === "active"} onCheckedChange={onToggle} aria-label={item.status === "active" ? "下架" : "上架"} />
      </div>
    </div>
  );
}
