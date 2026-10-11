"use client";

import { useState } from "react";
import { BarChart3, ReceiptText, ShoppingBag } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { PointsStoreItemsPanel } from "@/components/apps/points-store/points-store-items-panel";
import { PointsStoreOverviewPanel } from "@/components/apps/points-store/points-store-overview-panel";
import { PointsStoreRedemptionsPanel } from "@/components/apps/points-store/points-store-redemptions-panel";
import type { PointsStoreRedemptionStatus } from "@/lib/api/points-store";
import { cn } from "@/lib/utils";

/**
 * 积分商城区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 开没开、每天兑多少、花掉多少积分、哪些商品最受欢迎、还有几笔等人处理 |
 * | 商品 | 卖什么、多少积分、限量与限兑、卖得怎么样 |
 * | 兑换记录 | 谁在什么时候兑了什么、实际发出去了什么；人工发放在这里处理 |
 *
 * 视图状态不进 URL，理由同卡密区块：`?tab=` 已经被应用区块占用。
 * 用户的功能解锁在用户详情的「资产」页签里看与改，不在这里另开入口。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "items", label: "商品", icon: ShoppingBag },
  { key: "redemptions", label: "兑换记录", icon: ReceiptText }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppPointsStorePanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  // 从商品或概览跳到兑换记录时带过去的筛选条件
  const [itemFilter, setItemFilter] = useState<number | undefined>(undefined);
  const [statusFilter, setStatusFilter] = useState<PointsStoreRedemptionStatus | undefined>(undefined);

  if (!appKey) {
    return <NoAppSelected icon={<ShoppingBag className="size-5" />} />;
  }

  const openRedemptions = (itemId?: number, status?: PointsStoreRedemptionStatus) => {
    setItemFilter(itemId);
    setStatusFilter(status);
    setView("redemptions");
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setView(item.key)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
              view === item.key
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
          </button>
        ))}
      </div>

      {view === "overview" ? (
        <PointsStoreOverviewPanel appKey={appKey} onOpenPending={() => openRedemptions(undefined, "pending")} />
      ) : null}
      {view === "items" ? (
        <PointsStoreItemsPanel
          appKey={appKey}
          onOpenRedemptions={(itemId, pendingOnly) => openRedemptions(itemId, pendingOnly ? "pending" : undefined)}
        />
      ) : null}
      {view === "redemptions" ? (
        <PointsStoreRedemptionsPanel
          appKey={appKey}
          itemId={itemFilter}
          status={statusFilter}
          onItemIdChange={setItemFilter}
          onStatusChange={setStatusFilter}
        />
      ) : null}
    </div>
  );
}
