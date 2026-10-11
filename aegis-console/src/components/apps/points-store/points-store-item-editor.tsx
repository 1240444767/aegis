"use client";

import { useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  AUDIENCE_LABEL,
  LIMIT_PERIOD_LABEL,
  toLocalInput
} from "@/components/apps/points-store/points-store-shared";
import {
  RewardListEditor,
  rewardDraftsFrom,
  rewardsFromDrafts,
  type RewardDraft
} from "@/components/apps/rewards/reward-list-editor";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle
} from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { PointsStoreAudience, PointsStoreItem, PointsStoreLimitPeriod } from "@/lib/api/points-store";
import { usePointsStoreCatalogQuery, useSavePointsStoreItemMutation } from "@/lib/points-store-hooks";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

type Form = {
  title: string;
  subtitle: string;
  description: string;
  category: string;
  badge: string;
  cost: string;
  originalCost: string;
  rewards: RewardDraft[];
  limited: boolean;
  stock: string;
  limitPeriod: PointsStoreLimitPeriod;
  limitCount: string;
  audience: PointsStoreAudience;
  minLevel: string;
  startsAt: string;
  endsAt: string;
  active: boolean;
  sortOrder: string;
};

function seed(item?: PointsStoreItem | null): Form {
  if (!item) {
    return {
      title: "",
      subtitle: "",
      description: "",
      category: "",
      badge: "",
      cost: "100",
      originalCost: "",
      rewards: [],
      limited: false,
      stock: "100",
      limitPeriod: "total",
      limitCount: "0",
      audience: "all",
      minLevel: "0",
      startsAt: "",
      endsAt: "",
      active: true,
      sortOrder: "0"
    };
  }
  return {
    title: item.title,
    subtitle: item.subtitle ?? "",
    description: item.description ?? "",
    category: item.category ?? "",
    badge: item.badge ?? "",
    cost: String(item.cost),
    originalCost: item.originalCost ? String(item.originalCost) : "",
    rewards: rewardDraftsFrom(item.rewards),
    limited: item.stock !== undefined && item.stock !== null,
    stock: String(item.stock ?? 100),
    limitPeriod: item.limitPeriod,
    limitCount: String(item.limitCount),
    audience: item.audience,
    minLevel: String(item.minLevel),
    startsAt: toLocalInput(item.startsAt),
    endsAt: toLocalInput(item.endsAt),
    active: item.status === "active",
    sortOrder: String(item.sortOrder)
  };
}

/**
 * 新建 / 编辑一件商品。
 *
 * 权益由后端目录驱动（`/points-store/catalog`）：能加哪些、单位与上下界、
 * 哪一档能重复（功能解锁）、哪一档只能单独存在（人工发放）都来自目录。
 * 草稿与卡密编辑器同一个形状：抽屉关闭即清空，不用 effect 同步。
 */
export function PointsStoreItemEditor({
  appKey,
  item,
  open,
  onOpenChange
}: {
  appKey: string;
  /** 为空即新建 */
  item: PointsStoreItem | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const catalogQuery = usePointsStoreCatalogQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSavePointsStoreItemMutation(appKey);

  const [draft, setDraft] = useState<{ id: number; value: Form } | null>(null);
  const scope = item?.id ?? 0;
  const form = draft?.id === scope ? draft.value : seed(item);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ id: scope, value: { ...form, [key]: value } });

  const catalog = catalogQuery.data?.rewards ?? [];
  const maxRewards = catalogQuery.data?.maxRewards ?? 6;
  const categories = catalogQuery.data?.categories ?? [];
  const plans = (plansQuery.data ?? []).filter((plan) => plan.kind !== "trial");
  const features = featuresQuery.data ?? [];
  const close = () => {
    setDraft(null);
    onOpenChange(false);
  };

  const submit = async () => {
    if (!form.title.trim()) {
      toast.error("请填写商品名称");
      return;
    }
    if (form.rewards.length === 0) {
      toast.error("至少配置一项权益");
      return;
    }
    try {
      await saveMutation.mutateAsync({
        itemId: item?.id,
        payload: {
          title: form.title.trim(),
          subtitle: form.subtitle.trim(),
          description: form.description.trim(),
          category: form.category.trim(),
          badge: form.badge.trim(),
          cost: Number(form.cost) || 0,
          originalCost: form.originalCost ? Number(form.originalCost) : null,
          rewards: rewardsFromDrafts(form.rewards, catalog),
          stock: form.limited ? Number(form.stock) || 0 : null,
          limitPeriod: form.limitPeriod,
          limitCount: Number(form.limitCount) || 0,
          audience: form.audience,
          minLevel: Number(form.minLevel) || 0,
          startsAt: form.startsAt ? new Date(form.startsAt).toISOString() : null,
          endsAt: form.endsAt ? new Date(form.endsAt).toISOString() : null,
          status: form.active ? "active" : "inactive",
          sortOrder: Number(form.sortOrder) || 0
        }
      });
      toast.success(item ? "商品已保存" : "商品已创建");
      close();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) setDraft(null);
        onOpenChange(next);
      }}
    >
      <SheetContent side="right" className="flex w-[92vw] max-w-xl flex-col gap-0 p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{item ? "编辑商品" : "新建商品"}</SheetTitle>
          <SheetDescription>{item ? item.title : "积分商城"}</SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-6 py-5">
            <FieldGroup label="商品名称">
              <Input
                value={form.title}
                maxLength={32}
                onChange={(event) => patch("title", event.target.value)}
                placeholder="例如：在线音乐 7 天"
              />
            </FieldGroup>

            <div className="grid gap-4 sm:grid-cols-2">
              <FieldGroup label="副标题">
                <Input value={form.subtitle} maxLength={64} onChange={(event) => patch("subtitle", event.target.value)} />
              </FieldGroup>
              <FieldGroup label="分类">
                <Input
                  value={form.category}
                  maxLength={16}
                  list="points-store-categories"
                  onChange={(event) => patch("category", event.target.value)}
                  placeholder="会员"
                />
                <datalist id="points-store-categories">
                  {categories.map((category) => (
                    <option key={category} value={category} />
                  ))}
                </datalist>
              </FieldGroup>
              <NumberField label="兑换积分" value={form.cost} onChange={(value) => patch("cost", value)} min={1} unit="积分" />
              <NumberField
                label="划线价"
                value={form.originalCost}
                onChange={(value) => patch("originalCost", value)}
                min={0}
                unit="积分"
                placeholder="不设置"
              />
              <FieldGroup label="角标">
                <Input value={form.badge} maxLength={6} onChange={(event) => patch("badge", event.target.value)} placeholder="限时" />
              </FieldGroup>
              <NumberField label="排序" value={form.sortOrder} onChange={(value) => patch("sortOrder", value)} unit="越小越前" />
            </div>

            <FieldGroup label="权益" hint={`${form.rewards.length}/${maxRewards}`}>
              <RewardListEditor
                catalog={catalog}
                maxRewards={maxRewards}
                rewards={form.rewards}
                plans={plans}
                features={features}
                onChange={(rewards: RewardDraft[]) => patch("rewards", rewards)}
              />
            </FieldGroup>

            <FieldGroup label="库存与限兑">
              <div className="space-y-3">
                <SwitchRow label="限量" checked={form.limited} onChange={(value) => patch("limited", value)} />
                {form.limited ? (
                  <NumberField label="剩余库存" value={form.stock} onChange={(value) => patch("stock", value)} min={0} unit="件" />
                ) : null}
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <span className="text-[11px] font-medium">限兑周期</span>
                    <Select
                      value={form.limitPeriod}
                      onValueChange={(value) => patch("limitPeriod", value as PointsStoreLimitPeriod)}
                    >
                      <SelectTrigger className="h-9 w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {(catalogQuery.data?.limitPeriods ?? ["total", "day", "week", "month"]).map((period) => (
                          <SelectItem key={period} value={period}>
                            {LIMIT_PERIOD_LABEL[period]}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <NumberField
                    label="每人可兑"
                    value={form.limitCount}
                    onChange={(value) => patch("limitCount", value)}
                    min={0}
                    unit="次，0 为不限"
                  />
                </div>
              </div>
            </FieldGroup>

            <FieldGroup label="兑换条件">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">兑换对象</span>
                  <Select value={form.audience} onValueChange={(value) => patch("audience", value as PointsStoreAudience)}>
                    <SelectTrigger className="h-9 w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {(catalogQuery.data?.audiences ?? ["all", "vip", "non_vip"]).map((audience) => (
                        <SelectItem key={audience} value={audience}>
                          {AUDIENCE_LABEL[audience]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <NumberField
                  label="等级门槛"
                  value={form.minLevel}
                  onChange={(value) => patch("minLevel", value)}
                  min={0}
                  unit="Lv，0 为不限"
                />
              </div>
            </FieldGroup>

            <FieldGroup label="上架时间">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">开始</span>
                  <Input type="datetime-local" value={form.startsAt} onChange={(event) => patch("startsAt", event.target.value)} />
                </div>
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">结束</span>
                  <Input type="datetime-local" value={form.endsAt} onChange={(event) => patch("endsAt", event.target.value)} />
                </div>
              </div>
              <SwitchRow label="上架" checked={form.active} onChange={(value) => patch("active", value)} />
            </FieldGroup>

            <FieldGroup label="详情">
              <Textarea
                value={form.description}
                maxLength={1000}
                rows={3}
                onChange={(event) => patch("description", event.target.value)}
              />
            </FieldGroup>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="ghost" size="sm" onClick={close}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={saveMutation.isPending}>
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
