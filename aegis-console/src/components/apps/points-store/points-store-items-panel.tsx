"use client";

import { useMemo, useState } from "react";
import { Loader2, Pencil, Plus, ReceiptText, ShoppingBag, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { PointsStoreItemEditor } from "@/components/apps/points-store/points-store-item-editor";
import {
  AUDIENCE_LABEL,
  LIMIT_PERIOD_LABEL,
  RewardBadges,
  formatPoints,
  formatStoreDate,
  itemShelfState
} from "@/components/apps/points-store/points-store-shared";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api/client";
import type { PointsStoreAdminItem } from "@/lib/api/points-store";
import {
  useDeletePointsStoreItemMutation,
  usePointsStoreCatalogQuery,
  usePointsStoreItemsQuery,
  useSetPointsStoreItemStatusMutation
} from "@/lib/points-store-hooks";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

/**
 * 商品列表。
 *
 * 每一行回答三件事：卖什么、多少积分、卖得怎么样。兑换汇总随列表一起下发
 * （后端按商品聚合，不含已取消），待发放笔数挂在行上并可直达兑换记录。
 * 按分类分组，组内顺序即用户端的展示顺序（排序值、再按创建先后）。
 */
export function PointsStoreItemsPanel({
  appKey,
  onOpenRedemptions
}: {
  appKey: string;
  onOpenRedemptions: (itemId: number, pendingOnly: boolean) => void;
}) {
  const itemsQuery = usePointsStoreItemsQuery(appKey);
  const catalogQuery = usePointsStoreCatalogQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const statusMutation = useSetPointsStoreItemStatusMutation(appKey);
  const deleteMutation = useDeletePointsStoreItemMutation(appKey);

  const [editor, setEditor] = useState<{ open: boolean; item: PointsStoreAdminItem | null }>({ open: false, item: null });
  const [pendingDelete, setPendingDelete] = useState<PointsStoreAdminItem | null>(null);

  const groups = useMemo(() => {
    const map = new Map<string, PointsStoreAdminItem[]>();
    for (const item of itemsQuery.data ?? []) {
      const key = item.category?.trim() || "";
      map.set(key, [...(map.get(key) ?? []), item]);
    }
    return Array.from(map.entries());
  }, [itemsQuery.data]);

  const catalog = catalogQuery.data?.rewards ?? [];
  const plans = plansQuery.data ?? [];
  const features = featuresQuery.data ?? [];

  const toggleStatus = async (item: PointsStoreAdminItem, active: boolean) => {
    try {
      await statusMutation.mutateAsync({ itemId: item.id, active });
      toast.success(active ? `已上架「${item.title}」` : `已下架「${item.title}」`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await deleteMutation.mutateAsync(pendingDelete.id);
      toast.success(`已删除「${pendingDelete.title}」`);
      setPendingDelete(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  if (itemsQuery.isLoading) {
    return (
      <div className="space-y-3">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-28 w-full rounded-xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<ShoppingBag className="size-4" />}
        title="商品"
        aside={
          <Button size="sm" onClick={() => setEditor({ open: true, item: null })}>
            <Plus className="size-3.5" /> 新建商品
          </Button>
        }
      >
        {groups.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            暂无商品
          </p>
        ) : (
          <div className="space-y-5">
            {groups.map(([category, items]) => (
              <div key={category || "_"} className="space-y-2.5">
                <div className="flex items-center gap-2 text-[11px] font-semibold tracking-wide text-muted-foreground">
                  {category || "未分类"}
                  <span className="tabular-nums font-normal">{items.length}</span>
                </div>
                {items.map((item) => {
                  const shelf = itemShelfState(item);
                  return (
                    <div key={item.id} className="rounded-xl border border-border p-4">
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="min-w-0 space-y-1.5">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="text-sm font-semibold">{item.title}</span>
                            {item.badge ? (
                              <Badge variant="info" size="sm">
                                {item.badge}
                              </Badge>
                            ) : null}
                            {shelf ? (
                              <Badge variant={shelf.variant} size="sm">
                                {shelf.label}
                              </Badge>
                            ) : null}
                          </div>
                          {item.subtitle ? <p className="text-xs text-muted-foreground">{item.subtitle}</p> : null}
                          <RewardBadges rewards={item.rewards} catalog={catalog} plans={plans} features={features} />
                          <p className="text-xs text-muted-foreground">
                            <span className="font-medium tabular-nums text-foreground">{formatPoints(item.cost)}</span> 积分
                            {item.originalCost ? (
                              <span className="ml-1 tabular-nums line-through">{formatPoints(item.originalCost)}</span>
                            ) : null}
                            {item.stock !== undefined && item.stock !== null ? `，库存 ${formatPoints(item.stock)}` : "，不限量"}
                            {item.limitCount > 0
                              ? `，每人${item.limitPeriod === "total" ? "" : LIMIT_PERIOD_LABEL[item.limitPeriod]}限兑 ${item.limitCount} 次`
                              : null}
                            {item.audience !== "all" ? `，${AUDIENCE_LABEL[item.audience]}` : null}
                            {item.minLevel > 0 ? `，Lv.${item.minLevel} 起` : null}
                          </p>
                          {item.startsAt || item.endsAt ? (
                            <p className="text-xs text-muted-foreground">{saleWindow(item.startsAt, item.endsAt)}</p>
                          ) : null}
                        </div>

                        <div className="flex shrink-0 items-center gap-1.5">
                          <Switch
                            checked={item.status === "active"}
                            onCheckedChange={(next) => toggleStatus(item, next)}
                            aria-label="上架"
                          />
                          <Button size="sm" variant="ghost" onClick={() => setEditor({ open: true, item })}>
                            <Pencil className="size-3.5" /> 编辑
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => onOpenRedemptions(item.id, false)}>
                            <ReceiptText className="size-3.5" /> 记录
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => setPendingDelete(item)}
                            aria-label="删除商品"
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </div>
                      </div>

                      <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        <span className="tabular-nums">
                          兑换 <span className="font-medium text-foreground">{formatPoints(item.redemptions)}</span> 笔
                        </span>
                        <span className="tabular-nums">{formatPoints(item.redeemers)} 人</span>
                        <span className="tabular-nums">消耗 {formatPoints(item.pointsSpent)} 积分</span>
                        {item.pending > 0 ? (
                          <button
                            type="button"
                            className="tabular-nums font-medium text-amber-600 hover:underline dark:text-amber-400"
                            onClick={() => onOpenRedemptions(item.id, true)}
                          >
                            待发放 {item.pending}
                          </button>
                        ) : null}
                      </div>
                    </div>
                  );
                })}
              </div>
            ))}
          </div>
        )}
      </SectionCard>

      <PointsStoreItemEditor
        appKey={appKey}
        item={editor.item}
        open={editor.open}
        onOpenChange={(open) => setEditor((current) => ({ open, item: open ? current.item : null }))}
      />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除商品「{pendingDelete?.title}」？</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete?.pending
                ? `兑换记录保留，${pendingDelete.pending} 笔待发放仍需处理。`
                : "兑换记录保留。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** 上下架时间：两头都有写「起 至 止」，只有一头写「起」或「截至」 */
function saleWindow(startsAt?: string, endsAt?: string) {
  if (startsAt && endsAt) return `${formatStoreDate(startsAt)} 至 ${formatStoreDate(endsAt)}`;
  if (startsAt) return `${formatStoreDate(startsAt)} 起`;
  return `截至 ${formatStoreDate(endsAt)}`;
}
