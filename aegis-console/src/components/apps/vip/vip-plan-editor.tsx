"use client";

import { useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { FeatureChecklist, FeatureTagList } from "@/components/apps/vip/vip-shared";
import { useSaveAdminVipPlanMutation } from "@/lib/vip-hooks";
import type { VipFeature, VipPlan, VipPlanKind, VipTier } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

type Props = {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 为空即新建 */
  plan?: VipPlan | null;
  features: VipFeature[];
  tiers: VipTier[];
  /** 该应用是否已有启用中的试用套餐（不含正在编辑的这个） */
  hasOtherActiveTrial: boolean;
};

type Form = {
  name: string;
  kind: VipPlanKind;
  lifetime: boolean;
  trialDeviceLimited: boolean;
  /** 卖的档位：null 跟随默认（见 defaultTierChoice），"new" 按套餐名新建一个档位 */
  tier: number | "new" | null;
  /** 仅 tier 为 "new" 时使用：新档位的功能 */
  features: string[];
  durationDays: string;
  price: string;
  originalPrice: string;
  bonusIntegral: string;
  description: string;
  isActive: boolean;
  sortOrder: string;
};

function seed(plan?: VipPlan | null): Form {
  return {
    name: plan?.name ?? "",
    kind: plan?.kind ?? "paid",
    lifetime: plan?.lifetime ?? false,
    trialDeviceLimited: plan?.trialDeviceLimited ?? false,
    tier: plan?.tierId ? plan.tierId : null,
    features: plan?.features ?? [],
    durationDays: String(plan?.durationDays ?? 30),
    price: plan?.price ?? "",
    originalPrice: plan?.originalPrice ?? "",
    bonusIntegral: String(plan?.bonusIntegral ?? 0),
    description: plan?.description ?? "",
    isActive: plan?.isActive ?? true,
    sortOrder: String(plan?.sortOrder ?? 0)
  };
}

/**
 * 新建套餐默认卖哪个档位：永久套餐取永久会员的默认档位，限时 / 试用取已有限时套餐所在的档位，
 * 都没有时取第一个档位；一个档位都没有时新建。删掉套餐再上架时，默认就回到原来那个档位。
 */
function defaultTierChoice(tiers: VipTier[], lifetime: boolean): number | "new" {
  const found = lifetime
    ? (tiers.find((tier) => tier.defaultLifetime) ?? tiers.find((tier) => tier.plans.some((plan) => plan.lifetime)))
    : tiers.find((tier) => tier.plans.some((plan) => !plan.lifetime));
  return found?.id ?? tiers[0]?.id ?? "new";
}

/**
 * 套餐编辑抽屉。
 *
 * 草稿按「正在编辑哪一个」绑定（`draft.scope`），不用 `useEffect` 同步 ——
 * 与配置面板同一条约束：effect 同步既触发级联渲染，也会把上一个套餐的
 * 未保存改动串到下一个上。
 *
 * 期限（限时 / 永久）只在新建时可选：后端不允许已有套餐在两者之间切换
 * （已卖出的开通按开通时的形态记账），试用套餐也不能是永久的。
 */
export function VipPlanEditor({ appKey, open, onOpenChange, plan, features, tiers, hasOtherActiveTrial }: Props) {
  const scope = plan ? `plan:${plan.id}` : "new";
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const form = draft?.scope === scope ? draft.value : seed(plan);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  const saveMutation = useSaveAdminVipPlanMutation(appKey);
  const isTrial = form.kind === "trial";
  const lifetime = !isTrial && form.lifetime;
  // 已有套餐的期限是定死的；永久套餐因此也不能改成试用
  const termLocked = Boolean(plan);
  const trialConflict = isTrial && form.isActive && hasOtherActiveTrial;

  const pickKind = (kind: VipPlanKind) =>
    setDraft({ scope, value: { ...form, kind, lifetime: kind === "trial" ? false : form.lifetime } });

  const tierChoice = form.tier ?? defaultTierChoice(tiers, lifetime);
  const selectedTier = tierChoice === "new" ? null : (tiers.find((tier) => tier.id === tierChoice) ?? null);
  const tierMoved = Boolean(plan && plan.tierId && tierChoice !== plan.tierId);

  const submit = async () => {
    const name = form.name.trim();
    if (!name) {
      toast.error("套餐名称不能为空");
      return;
    }
    const durationDays = Number(form.durationDays);
    if (!lifetime && (!Number.isFinite(durationDays) || durationDays <= 0)) {
      toast.error("套餐时长必须大于 0 天");
      return;
    }
    // 试用恒 0 元：后端有 CHECK 约束，这里先拦一道，免得让人填完再被拒
    const price = isTrial ? "0" : form.price.trim() || "0";
    if (!isTrial && Number.isNaN(Number(price))) {
      toast.error("套餐价格格式无效");
      return;
    }

    try {
      await saveMutation.mutateAsync({
        id: plan?.id,
        name,
        kind: form.kind,
        // 期限只在创建时下发：对已有套餐传一个不同的值会被后端拒绝
        lifetime: plan ? undefined : lifetime,
        trialDeviceLimited: isTrial ? form.trialDeviceLimited : false,
        // 选了已有档位只传档位（功能在档位里改）；新建档位时带上功能，后端按套餐名建档
        ...(selectedTier ? { tierId: selectedTier.id } : { features: form.features }),
        durationDays: lifetime ? undefined : durationDays,
        price,
        originalPrice: form.originalPrice.trim() || undefined,
        bonusIntegral: Number(form.bonusIntegral) || 0,
        description: form.description.trim(),
        isActive: form.isActive,
        sortOrder: Number(form.sortOrder) || 0
      });
      toast.success(plan ? "套餐已更新" : "套餐已创建");
      setDraft(null);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存套餐失败");
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
          <SheetTitle>{plan ? "编辑套餐" : "新建套餐"}</SheetTitle>
          <SheetDescription>
            {isTrial ? "试用套餐" : lifetime ? "永久套餐" : "付费套餐"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-6 py-5">
            {/* 种类：决定这个套餐是被买的还是被领的 */}
            <div className="space-y-2">
              <Label className="text-xs">套餐种类</Label>
              <div className="grid grid-cols-2 gap-2">
                {(["paid", "trial"] as const).map((kind) => (
                  <button
                    key={kind}
                    type="button"
                    disabled={kind === "trial" && termLocked && form.lifetime}
                    onClick={() => pickKind(kind)}
                    className={cn(
                      "rounded-xl border px-3 py-2.5 text-left transition-colors disabled:pointer-events-none disabled:opacity-50",
                      form.kind === kind
                        ? "border-primary/50 bg-primary/5 ring-1 ring-inset ring-primary/30"
                        : "border-border hover:bg-muted/60"
                    )}
                  >
                    <div className="text-xs font-medium">{kind === "paid" ? "付费套餐" : "试用套餐"}</div>
                  </button>
                ))}
              </div>
              {trialConflict ? (
                <p className="text-[11px] text-destructive">已有启用中的试用套餐</p>
              ) : null}
            </div>

            {/* 期限：只在新建付费套餐时可选，之后只读 */}
            {!isTrial ? (
              <div className="space-y-2">
                <Label className="text-xs">套餐期限</Label>
                <div className="grid grid-cols-2 gap-2">
                  {([false, true] as const).map((value) => (
                    <button
                      key={String(value)}
                      type="button"
                      disabled={termLocked}
                      onClick={() => patch("lifetime", value)}
                      className={cn(
                        "rounded-xl border px-3 py-2.5 text-left transition-colors disabled:pointer-events-none",
                        form.lifetime === value
                          ? "border-primary/50 bg-primary/5 ring-1 ring-inset ring-primary/30"
                          : "border-border hover:bg-muted/60",
                        termLocked && form.lifetime !== value && "opacity-50"
                      )}
                    >
                      <div className="text-xs font-medium">{value ? "永久" : "限时"}</div>
                    </button>
                  ))}
                </div>
                {termLocked ? (
                  <p className="text-[11px] text-muted-foreground">创建后不可更改</p>
                ) : null}
              </div>
            ) : null}

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5 sm:col-span-2">
                <Label className="text-xs">名称</Label>
                <Input
                  value={form.name}
                  onChange={(event) => patch("name", event.target.value)}
                  placeholder={isTrial ? "如：7 天免费试用" : "如：月度会员"}
                />
              </div>

              {lifetime ? null : (
                <div className="space-y-1.5">
                  <Label className="text-xs">时长（天）</Label>
                  <Input
                    inputMode="numeric"
                    value={form.durationDays}
                    onChange={(event) => patch("durationDays", event.target.value)}
                  />
                </div>
              )}

              <div className="space-y-1.5">
                <Label className="text-xs">价格</Label>
                <Input
                  inputMode="decimal"
                  value={isTrial ? "0" : form.price}
                  disabled={isTrial}
                  onChange={(event) => patch("price", event.target.value)}
                  placeholder="0.00"
                />
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs">原价（划线价）</Label>
                <Input
                  inputMode="decimal"
                  value={form.originalPrice}
                  disabled={isTrial}
                  onChange={(event) => patch("originalPrice", event.target.value)}
                  placeholder="选填"
                />
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs">赠送积分</Label>
                <Input
                  inputMode="numeric"
                  value={form.bonusIntegral}
                  onChange={(event) => patch("bonusIntegral", event.target.value)}
                />
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs">排序（小的在前）</Label>
                <Input
                  inputMode="numeric"
                  value={form.sortOrder}
                  onChange={(event) => patch("sortOrder", event.target.value)}
                />
              </div>

              <div className="space-y-1.5 sm:col-span-2">
                <Label className="text-xs">描述</Label>
                <Textarea
                  rows={2}
                  value={form.description}
                  onChange={(event) => patch("description", event.target.value)}
                  placeholder="选填"
                />
              </div>
            </div>

            {/* 档位：这个套餐卖的是哪一档权益。功能属于档位，删掉套餐不影响已开通的会员 */}
            <div className="space-y-2">
              <Label className="text-xs">会员档位</Label>
              <div className="grid gap-2 sm:grid-cols-2">
                {tiers.map((tier) => (
                  <button
                    key={tier.id}
                    type="button"
                    onClick={() => patch("tier", tier.id)}
                    className={cn(
                      "rounded-xl border px-3 py-2.5 text-left transition-colors",
                      tierChoice === tier.id
                        ? "border-primary/50 bg-primary/5 ring-1 ring-inset ring-primary/30"
                        : "border-border hover:bg-muted/60"
                    )}
                  >
                    <div className="truncate text-xs font-medium">{tier.name}</div>
                    <div className="text-[10px] text-muted-foreground">
                      {tier.features.length} 项功能，{tier.members} 位在期会员
                    </div>
                  </button>
                ))}
                <button
                  type="button"
                  onClick={() => patch("tier", "new")}
                  className={cn(
                    "rounded-xl border border-dashed px-3 py-2.5 text-left transition-colors",
                    tierChoice === "new"
                      ? "border-primary/50 bg-primary/5 ring-1 ring-inset ring-primary/30"
                      : "border-border hover:bg-muted/60"
                  )}
                >
                  <div className="text-xs font-medium">新建档位</div>
                  <div className="text-[10px] text-muted-foreground">以套餐名命名</div>
                </button>
              </div>

              {selectedTier ? (
                <div className="space-y-1.5 rounded-xl bg-muted px-3 py-2.5">
                  <FeatureTagList tags={selectedTier.features} catalog={features} emptyHint="仅会员身份，无细分权益" />
                  <p className="text-[11px] text-muted-foreground">
                    {tierMoved
                      ? "改档位只影响以后的售卖，已开通的会员仍在原档位"
                      : "功能在「档位」视图里修改，修改即对该档位全部会员生效"}
                  </p>
                </div>
              ) : (
                <div className="space-y-1.5">
                  <div className="flex items-baseline justify-between gap-2">
                    <span className="text-[11px] text-muted-foreground">新档位的功能</span>
                    <span className="text-[11px] text-muted-foreground">
                      已选 {form.features.length} / {features.length}
                    </span>
                  </div>
                  <FeatureChecklist catalog={features} value={form.features} onChange={(next) => patch("features", next)} />
                  <p className="text-[11px] text-muted-foreground">功能与已有档位完全相同时自动并入该档位</p>
                </div>
              )}
            </div>

            {isTrial ? (
              <label className="flex cursor-pointer items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
                <div className="min-w-0 space-y-0.5">
                  <div className="text-xs font-medium">同一设备只能领一次</div>
                </div>
                <Switch
                  checked={form.trialDeviceLimited}
                  onCheckedChange={(value) => patch("trialDeviceLimited", value)}
                />
              </label>
            ) : null}

            <label className="flex cursor-pointer items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
              <div className="min-w-0 space-y-0.5">
                <div className="text-xs font-medium">启用</div>
              </div>
              <Switch checked={form.isActive} onCheckedChange={(value) => patch("isActive", value)} />
            </label>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
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
