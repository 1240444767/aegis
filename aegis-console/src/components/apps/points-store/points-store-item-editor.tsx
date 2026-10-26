"use client";

import { useState, type ComponentType, type ReactNode } from "react";
import {
  AlertCircle,
  CalendarClock,
  CheckCircle2,
  Coins,
  Gift,
  Loader2,
  PackageOpen,
  Save,
  ShieldCheck,
  Tag,
  Text,
  X
} from "lucide-react";
import { toast } from "sonner";
import { ItemPrice } from "@/components/apps/points-store/points-store-item-card";
import {
  AUDIENCE_LABEL,
  LIMIT_PERIOD_LABEL,
  describeReward,
  formatPoints,
  itemRules,
  rewardVisual,
  saleWindow
} from "@/components/apps/points-store/points-store-shared";
import {
  RewardListEditor,
  rewardDraftsFrom,
  rewardsFromDrafts,
  type RewardDraft
} from "@/components/apps/rewards/reward-list-editor";
import { ToneIcon, errorMessage, type Tone } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type {
  PointsStoreAdminItem,
  PointsStoreAudience,
  PointsStoreItem,
  PointsStoreLimitPeriod
} from "@/lib/api/points-store";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { usePointsStoreCatalogQuery, useSavePointsStoreItemMutation } from "@/lib/points-store-hooks";
import { cn } from "@/lib/utils";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

export type EditorTarget =
  | { mode: "create" }
  | { mode: "edit"; item: PointsStoreAdminItem }
  | { mode: "duplicate"; item: PointsStoreAdminItem };

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

const BADGE_PRESETS = ["限时", "热门", "新品", "特惠", "推荐"];

function seed(target: EditorTarget): Form {
  if (target.mode === "create") {
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
  const item = target.item;
  const duplicate = target.mode === "duplicate";
  return {
    title: duplicate ? `${item.title}（副本）`.slice(0, 32) : item.title,
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
    startsAt: item.startsAt ? toLocalInputValue(item.startsAt) : "",
    endsAt: item.endsAt ? toLocalInputValue(item.endsAt) : "",
    // 复制出来的商品先不上架，改完再开
    active: duplicate ? false : item.status === "active",
    sortOrder: String(item.sortOrder)
  };
}

/**
 * 新建 / 编辑 / 复制一件商品。
 *
 * 左边是分区表单，右边是用户端卡片的实时预览与保存前检查（窄屏时预览收到表单下方）。
 * 权益由后端目录驱动（`/points-store/catalog`）：能加哪些、单位与上下界、
 * 哪一档能重复（功能解锁）、哪一档只能单独存在（人工发放）都来自目录。
 * 草稿跟着打开对象走：换一件商品或关闭即丢弃，不用 effect 同步。
 */
export function PointsStoreItemEditor({
  appKey,
  target,
  onClose
}: {
  appKey: string;
  target: EditorTarget | null;
  onClose: () => void;
}) {
  // 关闭动画期间 target 已为空，沿用上一次的对象渲染
  const [shown, setShown] = useState<EditorTarget | null>(target);
  if (target && target !== shown) setShown(target);

  return (
    <Sheet open={Boolean(target)} onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-4xl">
        {shown ? <EditorBody key={editorKey(shown)} appKey={appKey} target={shown} onClose={onClose} /> : null}
      </SheetContent>
    </Sheet>
  );
}

function editorKey(target: EditorTarget) {
  return target.mode === "create" ? "create" : `${target.mode}-${target.item.id}`;
}

function EditorBody({ appKey, target, onClose }: { appKey: string; target: EditorTarget; onClose: () => void }) {
  const catalogQuery = usePointsStoreCatalogQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSavePointsStoreItemMutation(appKey);
  const [form, setForm] = useState<Form>(() => seed(target));
  const patch = <K extends keyof Form>(key: K, value: Form[K]) => setForm((current) => ({ ...current, [key]: value }));

  const catalog = catalogQuery.data?.rewards ?? [];
  const maxRewards = catalogQuery.data?.maxRewards ?? 6;
  const categories = catalogQuery.data?.categories ?? [];
  const plans = (plansQuery.data ?? []).filter((plan) => plan.kind !== "trial");
  const features = featuresQuery.data ?? [];
  const editing = target.mode === "edit" ? target.item : null;

  const cost = Number(form.cost) || 0;
  const originalCost = form.originalCost ? Number(form.originalCost) || 0 : 0;
  const startsAt = fromLocalInputValue(form.startsAt);
  const endsAt = fromLocalInputValue(form.endsAt);

  const problems: Array<{ ok: boolean; label: string; blocking: boolean }> = [
    { ok: Boolean(form.title.trim()), label: "填写商品名称", blocking: true },
    { ok: form.rewards.length > 0, label: "至少配置一项权益", blocking: true },
    { ok: cost >= 1, label: "兑换积分至少为 1", blocking: true },
    { ok: !startsAt || !endsAt || startsAt < endsAt, label: "结束时间晚于开始时间", blocking: true },
    { ok: !originalCost || originalCost > cost, label: "划线价高于兑换积分", blocking: false },
    { ok: !form.limited || Number(form.stock) > 0, label: "限量商品库存大于 0", blocking: false }
  ];
  const blocked = problems.some((problem) => problem.blocking && !problem.ok);

  // 预览用的商品形状
  const preview: PointsStoreItem = {
    id: editing?.id ?? 0,
    appid: 0,
    title: form.title.trim() || "商品名称",
    subtitle: form.subtitle.trim(),
    description: form.description.trim(),
    category: form.category.trim(),
    badge: form.badge.trim(),
    cost,
    originalCost: originalCost || undefined,
    rewards: rewardsFromDrafts(form.rewards, catalog),
    stock: form.limited ? Number(form.stock) || 0 : undefined,
    soldCount: editing?.soldCount ?? 0,
    limitPeriod: form.limitPeriod,
    limitCount: Number(form.limitCount) || 0,
    audience: form.audience,
    minLevel: Number(form.minLevel) || 0,
    startsAt: startsAt ?? undefined,
    endsAt: endsAt ?? undefined,
    status: form.active ? "active" : "inactive",
    sortOrder: Number(form.sortOrder) || 0,
    createdAt: "",
    updatedAt: ""
  };

  const submit = async () => {
    const firstBlocking = problems.find((problem) => problem.blocking && !problem.ok);
    if (firstBlocking) {
      toast.error(`请先${firstBlocking.label}`);
      return;
    }
    try {
      await saveMutation.mutateAsync({
        itemId: editing?.id,
        payload: {
          title: form.title.trim(),
          subtitle: form.subtitle.trim(),
          description: form.description.trim(),
          category: form.category.trim(),
          badge: form.badge.trim(),
          cost,
          originalCost: originalCost || null,
          rewards: preview.rewards,
          stock: form.limited ? Number(form.stock) || 0 : null,
          limitPeriod: form.limitPeriod,
          limitCount: Number(form.limitCount) || 0,
          audience: form.audience,
          minLevel: Number(form.minLevel) || 0,
          startsAt: startsAt ?? null,
          endsAt: endsAt ?? null,
          status: form.active ? "active" : "inactive",
          sortOrder: Number(form.sortOrder) || 0
        }
      });
      toast.success(editing ? "商品已保存" : "商品已创建");
      onClose();
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  const title = target.mode === "edit" ? "编辑商品" : target.mode === "duplicate" ? "复制商品" : "新建商品";

  return (
    <>
      <SheetHeader className="shrink-0 gap-1 border-b px-5 py-4 pr-12">
        <SheetTitle className="text-base">{title}</SheetTitle>
        <SheetDescription>
          {editing
            ? `编号 ${editing.id}，已兑换 ${formatPoints(editing.redemptions)} 笔。修改只影响之后的兑换，已有记录按快照保留。`
            : target.mode === "duplicate"
              ? `从「${target.item.title}」复制，默认不上架`
              : "配置价格、权益与兑换规则，保存后即按上架设置对用户可见"}
        </SheetDescription>
      </SheetHeader>

      <div className="grid min-h-0 flex-1 overflow-y-auto lg:grid-cols-[minmax(0,1fr)_320px] lg:overflow-hidden">
        <div className="flex flex-col gap-0 divide-y lg:overflow-y-auto">
          <FormBlock icon={Tag} tone="primary" title="基本信息">
            <Field label="商品名称" counter={`${form.title.length}/32`}>
              <Input value={form.title} maxLength={32} onChange={(event) => patch("title", event.target.value)} placeholder="例如：在线音乐 7 天" />
            </Field>
            <Field label="副标题" counter={`${form.subtitle.length}/64`}>
              <Input value={form.subtitle} maxLength={64} onChange={(event) => patch("subtitle", event.target.value)} placeholder="一句话说明，显示在名称下方" />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="分类" hint="同分类的商品在用户端排在一组">
                <Input value={form.category} maxLength={16} onChange={(event) => patch("category", event.target.value)} placeholder="不填即未分类" />
                {categories.length ? (
                  <Chips options={categories} value={form.category} onPick={(value) => patch("category", value)} />
                ) : null}
              </Field>
              <Field label="角标" counter={`${form.badge.length}/6`}>
                <Input value={form.badge} maxLength={6} onChange={(event) => patch("badge", event.target.value)} placeholder="不显示" />
                <Chips options={BADGE_PRESETS} value={form.badge} onPick={(value) => patch("badge", value)} />
              </Field>
            </div>
          </FormBlock>

          <FormBlock icon={Coins} tone="warning" title="价格">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="兑换积分">
                <NumberInput value={form.cost} min={1} onChange={(value) => patch("cost", value)} suffix="积分" />
              </Field>
              <Field label="划线价" hint="选填，高于兑换积分时显示折扣">
                <NumberInput value={form.originalCost} min={0} onChange={(value) => patch("originalCost", value)} suffix="积分" placeholder="不设置" />
              </Field>
            </div>
          </FormBlock>

          <FormBlock icon={Gift} tone="violet" title="权益" aside={`${form.rewards.length}/${maxRewards}`} description="兑换成功后发给用户的东西；人工发放只能单独成为一件商品">
            <RewardListEditor
              catalog={catalog}
              maxRewards={maxRewards}
              rewards={form.rewards}
              plans={plans}
              features={features}
              onChange={(rewards: RewardDraft[]) => patch("rewards", rewards)}
            />
          </FormBlock>

          <FormBlock icon={PackageOpen} tone="success" title="库存与限兑">
            <ToggleRow
              label="限量"
              hint={form.limited ? "库存为 0 时显示已兑完，取消兑换会加回库存" : "不限量，任何人满足条件都能兑换"}
              checked={form.limited}
              onChange={(value) => patch("limited", value)}
            />
            {form.limited ? (
              <Field label="剩余库存" hint={editing ? `已兑出 ${formatPoints(editing.soldCount)} 件` : undefined}>
                <NumberInput value={form.stock} min={0} onChange={(value) => patch("stock", value)} suffix="件" />
              </Field>
            ) : null}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="限兑周期">
                <Select value={form.limitPeriod} onValueChange={(value) => patch("limitPeriod", value as PointsStoreLimitPeriod)}>
                  <SelectTrigger className="w-full">
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
              </Field>
              <Field label="每人可兑" hint="0 为不限">
                <NumberInput value={form.limitCount} min={0} onChange={(value) => patch("limitCount", value)} suffix="次" />
              </Field>
            </div>
          </FormBlock>

          <FormBlock icon={ShieldCheck} tone="info" title="兑换条件">
            <Field label="兑换对象">
              <ToggleGroup
                type="single"
                variant="outline"
                value={form.audience}
                onValueChange={(value) => value && patch("audience", value as PointsStoreAudience)}
                className="w-full"
              >
                {(catalogQuery.data?.audiences ?? ["all", "vip", "non_vip"]).map((audience) => (
                  <ToggleGroupItem key={audience} value={audience} className="flex-1 text-xs">
                    {AUDIENCE_LABEL[audience]}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="等级门槛" hint="0 为不限">
                <NumberInput value={form.minLevel} min={0} onChange={(value) => patch("minLevel", value)} prefix="Lv." />
              </Field>
              <Field label="排序值" hint="越小越靠前，也可以在列表里拖动">
                <NumberInput value={form.sortOrder} onChange={(value) => patch("sortOrder", value)} />
              </Field>
            </div>
          </FormBlock>

          <FormBlock icon={CalendarClock} tone="neutral" title="上架时间" description="按浏览器时区填写；不填即上架后一直可兑">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="开始">
                <DateInput value={form.startsAt} onChange={(value) => patch("startsAt", value)} />
              </Field>
              <Field label="结束">
                <DateInput value={form.endsAt} onChange={(value) => patch("endsAt", value)} />
              </Field>
            </div>
          </FormBlock>

          <FormBlock icon={Text} tone="neutral" title="详情">
            <Field label="商品说明" counter={`${form.description.length}/1000`}>
              <Textarea
                value={form.description}
                maxLength={1000}
                rows={4}
                onChange={(event) => patch("description", event.target.value)}
                placeholder="兑换须知、使用方式、发放时效等"
              />
            </Field>
          </FormBlock>
        </div>

        <aside className="flex flex-col gap-4 border-t bg-muted/30 p-5 lg:overflow-y-auto lg:border-t-0 lg:border-l">
          <div className="text-xs font-medium text-muted-foreground">用户端预览</div>
          <PreviewCard item={preview} catalog={catalog} plans={plans} features={features} />
          <div className="flex flex-col gap-2">
            <div className="text-xs font-medium text-muted-foreground">保存前检查</div>
            <ul className="flex flex-col gap-1.5">
              {problems.map((problem) => (
                <li key={problem.label} className="flex items-center gap-2 text-xs">
                  {problem.ok ? (
                    <CheckCircle2 className="size-3.5 shrink-0 text-emerald-500" />
                  ) : (
                    <AlertCircle className={cn("size-3.5 shrink-0", problem.blocking ? "text-red-500" : "text-amber-500")} />
                  )}
                  <span className={problem.ok ? "text-muted-foreground" : ""}>{problem.label}</span>
                  {!problem.ok && !problem.blocking ? <span className="text-muted-foreground">（建议）</span> : null}
                </li>
              ))}
            </ul>
          </div>
        </aside>
      </div>

      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t px-5 py-3">
        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <Switch checked={form.active} onCheckedChange={(value) => patch("active", value)} />
          {form.active ? "保存后上架" : "保存为下架"}
        </label>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={onClose} disabled={saveMutation.isPending}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={saveMutation.isPending || blocked}>
            {saveMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            {editing ? "保存修改" : "创建商品"}
          </Button>
        </div>
      </div>
    </>
  );
}

function FormBlock({
  icon,
  tone,
  title,
  description,
  aside,
  children
}: {
  icon: ComponentType<{ className?: string }>;
  tone: Tone;
  title: string;
  description?: string;
  aside?: string;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-4 px-5 py-5">
      <div className="flex items-center gap-2.5">
        <ToneIcon icon={icon} tone={tone} size="sm" />
        <div className="flex min-w-0 flex-1 flex-col">
          <h3 className="text-sm font-semibold">{title}</h3>
          {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
        </div>
        {aside ? <span className="text-xs tabular-nums text-muted-foreground">{aside}</span> : null}
      </div>
      <div className="flex flex-col gap-4">{children}</div>
    </section>
  );
}

function Field({ label, hint, counter, children }: { label: string; hint?: string; counter?: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-xs font-medium">{label}</Label>
        {counter ? <span className="text-xs tabular-nums text-muted-foreground">{counter}</span> : null}
      </div>
      {children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function NumberInput({
  value,
  onChange,
  min,
  prefix,
  suffix,
  placeholder
}: {
  value: string;
  onChange: (value: string) => void;
  min?: number;
  prefix?: string;
  suffix?: string;
  placeholder?: string;
}) {
  return (
    <div className="flex h-9 items-center rounded-md border bg-transparent shadow-xs focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50 dark:bg-input/30">
      {prefix ? <span className="pl-3 text-xs text-muted-foreground">{prefix}</span> : null}
      <input
        type="number"
        inputMode="numeric"
        min={min}
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="h-full min-w-0 flex-1 bg-transparent px-3 text-sm tabular-nums outline-none placeholder:text-muted-foreground"
      />
      {suffix ? <span className="pr-3 text-xs text-muted-foreground">{suffix}</span> : null}
    </div>
  );
}

function DateInput({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className="relative">
      <Input type="datetime-local" value={value} onChange={(event) => onChange(event.target.value)} className={value ? "pr-8" : undefined} />
      {value ? (
        <button
          type="button"
          aria-label="清除"
          onClick={() => onChange("")}
          className="absolute top-1/2 right-2 inline-flex size-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      ) : null}
    </div>
  );
}

function Chips({ options, value, onPick }: { options: string[]; value: string; onPick: (value: string) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((option) => (
        <button
          key={option}
          type="button"
          onClick={() => onPick(value === option ? "" : option)}
          className={cn(
            "rounded-md border px-2 py-0.5 text-xs transition-colors",
            value === option ? "border-primary bg-primary/10 text-primary" : "text-muted-foreground hover:bg-muted hover:text-foreground"
          )}
        >
          {option}
        </button>
      ))}
    </div>
  );
}

function ToggleRow({ label, hint, checked, onChange }: { label: string; hint: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2.5 transition-colors hover:bg-muted/40">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs text-muted-foreground">{hint}</span>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} />
    </label>
  );
}

/** 接近 App 里的商品卡：图标、名称与角标、权益、价格与兑换按钮、库存与限制 */
function PreviewCard({
  item,
  catalog,
  plans,
  features
}: {
  item: PointsStoreItem;
  catalog: Parameters<typeof describeReward>[1];
  plans: Parameters<typeof describeReward>[2];
  features: Parameters<typeof describeReward>[3];
}) {
  const visual = rewardVisual(item.rewards[0]?.type);
  const rules = itemRules(item);
  const saleTime = saleWindow(item.startsAt, item.endsAt);
  const soldOut = item.stock !== undefined && item.stock <= 0;
  return (
    <div className="flex flex-col gap-3 rounded-2xl border bg-card p-4 shadow-xs">
      <div className="flex items-start gap-3">
        <ToneIcon icon={visual.icon} tone={visual.tone} size="lg" />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className={cn("truncate text-sm font-semibold", !item.title && "text-muted-foreground")}>{item.title}</span>
            {item.badge ? <span className="shrink-0 rounded bg-red-500 px-1 text-[11px] font-medium text-white">{item.badge}</span> : null}
          </div>
          {item.subtitle ? <span className="line-clamp-2 text-xs text-muted-foreground">{item.subtitle}</span> : null}
        </div>
      </div>
      {item.rewards.length ? (
        <ul className="flex flex-col gap-1">
          {item.rewards.map((entry, index) => {
            const entryVisual = rewardVisual(entry.type);
            return (
              <li key={`${entry.type}-${index}`} className="flex items-center gap-1.5 text-xs">
                <entryVisual.icon className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="truncate">{describeReward(entry, catalog, plans, features)}</span>
              </li>
            );
          })}
        </ul>
      ) : (
        <p className="rounded-lg border border-dashed px-3 py-2 text-center text-xs text-muted-foreground">还没有权益</p>
      )}
      <div className="flex items-end justify-between gap-2">
        <ItemPrice item={item} />
        <span
          className={cn(
            "rounded-full px-3 py-1 text-xs font-medium",
            soldOut || item.status !== "active" ? "bg-muted text-muted-foreground" : "bg-primary text-primary-foreground"
          )}
        >
          {item.status !== "active" ? "未上架" : soldOut ? "已兑完" : "兑换"}
        </span>
      </div>
      {item.stock !== undefined || rules.length || saleTime ? (
        <div className="flex flex-wrap gap-x-3 gap-y-1 border-t pt-2.5 text-xs text-muted-foreground">
          {item.stock !== undefined ? <span>剩余 {formatPoints(item.stock)} 件</span> : null}
          {rules.map((rule) => (
            <span key={rule}>{rule}</span>
          ))}
          {saleTime ? <span>{saleTime}</span> : null}
        </div>
      ) : null}
    </div>
  );
}
