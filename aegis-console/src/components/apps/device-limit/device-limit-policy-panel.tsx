"use client";

import { useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  BellRing,
  ChevronDown,
  Crown,
  Layers,
  MonitorSmartphone,
  Plus,
  RotateCcw,
  Save,
  ShieldX,
  Shuffle,
  Trash2,
  Users
} from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, ModeCard, SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  AUDIENCE_META,
  CLASS_META,
  ClassGlyph,
  ClassIcon,
  cleanClassLimits,
  DEVICE_CLASSES,
  DeviceSlots,
  LimitStepper,
  limitText,
  OVERFLOW_META,
  SAME_DEVICE_META
} from "@/components/apps/device-limit/device-limit-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { RadioGroup } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api-client";
import type {
  ClassLimits,
  DeviceClass,
  DeviceLimitPolicy,
  DeviceOverflow,
  DeviceSameDevice,
  DeviceTier,
  DeviceTierAudience
} from "@/lib/api/device-limit";
import { useSaveDeviceLimitPolicyMutation } from "@/lib/device-limit-hooks";
import { cn } from "@/lib/utils";
import { useAdminVipFeaturesQuery } from "@/lib/vip-hooks";

const OVERFLOWS: DeviceOverflow[] = ["kick_oldest", "kick_idle", "reject", "ask"];
const FALLBACKS: Array<Exclude<DeviceOverflow, "ask">> = ["kick_oldest", "kick_idle", "reject"];
const SAME_DEVICES: DeviceSameDevice[] = ["replace", "coexist"];
const AUDIENCES: DeviceTierAudience[] = ["vip", "lifetime", "trial", "feature"];

/**
 * 策略编辑。
 *
 * 左边是表单，右边是「生效上限一览」：默认与每一档会员规则各占一行，用设备槽位画出几台，
 * 改任何一个数字右边立刻跟着变 —— 多维度叠加的配置，只看表单很难想清楚某一档最终是几台。
 * 草稿由上层持有（模拟视图要用），这里只改与保存。
 */
export function DeviceLimitPolicyPanel({
  appKey,
  policy,
  configured,
  loading,
  dirty,
  onChange,
  onReset,
  onSaved
}: {
  appKey: string;
  policy: DeviceLimitPolicy;
  configured: boolean;
  loading: boolean;
  dirty: boolean;
  onChange: (policy: DeviceLimitPolicy) => void;
  onReset: () => void;
  onSaved: () => void;
}) {
  const mutation = useSaveDeviceLimitPolicyMutation(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const features = (featuresQuery.data ?? []).filter((item) => item.isActive);

  const patch = <K extends keyof DeviceLimitPolicy>(key: K, value: DeviceLimitPolicy[K]) =>
    onChange({ ...policy, [key]: value });
  const patchClass = (cls: DeviceClass, value: number) =>
    patch("classLimits", cleanClassLimits({ ...policy.classLimits, [cls]: value }));
  const patchTier = (index: number, next: DeviceTier) =>
    patch(
      "tiers",
      policy.tiers.map((tier, i) => (i === index ? next : tier))
    );
  const moveTier = (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= policy.tiers.length) return;
    const tiers = [...policy.tiers];
    [tiers[index], tiers[target]] = [tiers[target], tiers[index]];
    patch("tiers", tiers);
  };
  const addTier = () => {
    const used = new Set(policy.tiers.map((tier) => tier.audience));
    const audience = AUDIENCES.find((item) => item !== "feature" && !used.has(item)) ?? "vip";
    patch("tiers", [
      ...policy.tiers,
      {
        id: `tier-${Date.now().toString(36)}`,
        name: AUDIENCE_META[audience].label,
        audience,
        maxDevices: Math.max(policy.maxDevices, 1) + 2,
        classLimits: null
      }
    ]);
  };

  async function handleSave() {
    const missing = policy.tiers.find((tier) => tier.audience === "feature" && !tier.featureTag);
    if (missing) {
      toast.error(`「${missing.name || "档位"}」需要选择功能`);
      return;
    }
    try {
      await mutation.mutateAsync({
        ...policy,
        classLimits: cleanClassLimits(policy.classLimits),
        tiers: policy.tiers.map((tier) => ({
          ...tier,
          name: tier.name.trim(),
          classLimits: Object.keys(cleanClassLimits(tier.classLimits)).length ? cleanClassLimits(tier.classLimits) : null
        })),
        rejectMessage: policy.rejectMessage.trim()
      });
      onSaved();
      toast.success("已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  if (loading) {
    return (
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
        <div className="space-y-4">
          <Skeleton className="h-72 w-full rounded-2xl" />
          <Skeleton className="h-64 w-full rounded-2xl" />
        </div>
        <Skeleton className="h-80 w-full rounded-2xl" />
      </div>
    );
  }

  const off = !policy.enabled;
  const showRejectMessage = policy.overflow === "reject" || (policy.overflow === "ask" && policy.askFallback === "reject");

  return (
    <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
      <div className="space-y-4">
        <SectionCard
          icon={<MonitorSmartphone className="size-4" />}
          title="同时登录设备"
          aside={
            <label className="flex cursor-pointer items-center gap-2 text-xs font-medium">
              {policy.enabled ? "已开启" : "未开启"}
              <Switch checked={policy.enabled} onCheckedChange={(value) => patch("enabled", value)} />
            </label>
          }
        >
          <div className={cn("space-y-5", off && "opacity-60")}>
            {!configured ? (
              <p className="rounded-lg bg-muted px-3 py-2 text-xs text-muted-foreground">
                当前沿用旧的同时在线上限，保存后改用这里的策略
              </p>
            ) : null}
            <FieldGroup label="默认上限">
              <div className="flex flex-wrap items-center gap-4 rounded-xl bg-muted px-3 py-3">
                <LimitStepper value={policy.maxDevices} onChange={(value) => patch("maxDevices", value)} />
                <DeviceSlots limit={policy.maxDevices} used={policy.maxDevices} />
              </div>
            </FieldGroup>
            <FieldGroup label="终端类别" hint="0 为不单独限制">
              <div className="grid gap-2.5 sm:grid-cols-2">
                {DEVICE_CLASSES.map((cls) => {
                  const value = policy.classLimits?.[cls] ?? 0;
                  const exceeds = policy.maxDevices > 0 && value > policy.maxDevices;
                  return (
                    <div
                      key={cls}
                      className={cn(
                        "flex items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5",
                        value > 0 && "ring-1 ring-inset ring-border"
                      )}
                    >
                      <div className="flex min-w-0 items-center gap-2.5">
                        <ClassGlyph cls={cls} />
                        <div className="min-w-0">
                          <div className="text-xs font-medium">{CLASS_META[cls].label}</div>
                          {exceeds ? <div className="text-[10px] text-amber-600 dark:text-amber-400">超过默认上限</div> : null}
                        </div>
                      </div>
                      <LimitStepper value={value} onChange={(next) => patchClass(cls, next)} />
                    </div>
                  );
                })}
              </div>
            </FieldGroup>
          </div>
        </SectionCard>

        <SectionCard icon={<Shuffle className="size-4" />} title="超限处置">
          <div className={cn("space-y-5", off && "opacity-60")}>
            <RadioGroup
              value={policy.overflow}
              onValueChange={(value) => patch("overflow", value as DeviceOverflow)}
              className="grid gap-2.5 sm:grid-cols-2"
            >
              {OVERFLOWS.map((mode) => {
                const meta = OVERFLOW_META[mode];
                const Icon = meta.icon;
                return (
                  <ModeCard
                    key={mode}
                    value={mode}
                    active={policy.overflow === mode}
                    title={meta.title}
                    description={meta.description}
                    icon={<Icon className="size-3.5" />}
                  />
                );
              })}
            </RadioGroup>

            {policy.overflow === "ask" ? (
              <FieldGroup label="客户端不支持选择时">
                <ToggleGroup
                  type="single"
                  size="sm"
                  variant="outline"
                  value={policy.askFallback}
                  onValueChange={(value) => value && patch("askFallback", value as DeviceLimitPolicy["askFallback"])}
                  className="flex-wrap"
                >
                  {FALLBACKS.map((mode) => (
                    <ToggleGroupItem key={mode} value={mode} className="h-8 px-3 text-xs">
                      {OVERFLOW_META[mode].short}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </FieldGroup>
            ) : null}

            {showRejectMessage ? (
              <FieldGroup label="拒绝提示" hint="{limit} 替换为上限">
                <Input
                  value={policy.rejectMessage}
                  maxLength={200}
                  placeholder="同时登录的设备已达上限（{limit} 台），请先在已登录的设备上退出"
                  onChange={(event) => patch("rejectMessage", event.target.value)}
                  className="text-xs"
                />
              </FieldGroup>
            ) : null}

            <FieldGroup label="同一设备再次登录">
              <RadioGroup
                value={policy.sameDevice}
                onValueChange={(value) => patch("sameDevice", value as DeviceSameDevice)}
                className="grid gap-2.5 sm:grid-cols-2"
              >
                {SAME_DEVICES.map((mode) => {
                  const meta = SAME_DEVICE_META[mode];
                  const Icon = meta.icon;
                  return (
                    <ModeCard
                      key={mode}
                      value={mode}
                      active={policy.sameDevice === mode}
                      title={meta.title}
                      description={meta.description}
                      icon={<Icon className="size-3.5" />}
                    />
                  );
                })}
              </RadioGroup>
            </FieldGroup>

            <SwitchRow
              icon={<BellRing className="size-3.5" />}
              label="自动下线时发站内信"
              checked={policy.notifyKicked}
              onChange={(value) => patch("notifyKicked", value)}
            />
          </div>
        </SectionCard>

        <SectionCard
          icon={<Crown className="size-4" />}
          title="会员档位"
          aside={
            <Button size="sm" variant="outline" className="h-7" onClick={addTier} disabled={policy.tiers.length >= 10}>
              <Plus className="size-3.5" /> 添加档位
            </Button>
          }
        >
          <div className={cn(off && "opacity-60")}>
            {policy.tiers.length === 0 ? (
              <p className="py-6 text-center text-xs text-muted-foreground">暂无档位，所有人使用默认上限</p>
            ) : (
              <ol className="space-y-2.5">
                {policy.tiers.map((tier, index) => (
                  <TierRow
                    key={tier.id}
                    tier={tier}
                    index={index}
                    count={policy.tiers.length}
                    defaultClassLimits={policy.classLimits}
                    features={features.map((item) => ({ tag: item.tag, name: item.name }))}
                    onChange={(next) => patchTier(index, next)}
                    onMove={(delta) => moveTier(index, delta)}
                    onRemove={() => patch("tiers", policy.tiers.filter((_, i) => i !== index))}
                  />
                ))}
              </ol>
            )}
          </div>
        </SectionCard>

        <SectionCard
          icon={<ShieldX className="size-4" />}
          title="新设备频率"
          aside={
            <Switch
              checked={policy.newDevice.enabled}
              onCheckedChange={(value) =>
                patch("newDevice", { ...policy.newDevice, enabled: value, maxNew: Math.max(1, policy.newDevice.maxNew) })
              }
            />
          }
        >
          <div className={cn("grid gap-3 sm:grid-cols-2", (off || !policy.newDevice.enabled) && "opacity-60")}>
            <div className="flex items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
              <span className="text-xs font-medium">统计窗口</span>
              <LimitStepper
                value={policy.newDevice.windowDays}
                min={1}
                max={365}
                unit="天"
                zeroLabel=""
                disabled={!policy.newDevice.enabled}
                onChange={(value) => patch("newDevice", { ...policy.newDevice, windowDays: value })}
              />
            </div>
            <div className="flex items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
              <span className="text-xs font-medium">窗口内新设备</span>
              <LimitStepper
                value={policy.newDevice.maxNew}
                min={1}
                max={100}
                zeroLabel=""
                disabled={!policy.newDevice.enabled}
                onChange={(value) => patch("newDevice", { ...policy.newDevice, maxNew: value })}
              />
            </div>
          </div>
        </SectionCard>
      </div>

      <aside className="space-y-3 xl:sticky xl:top-4">
        <EffectivePreview policy={policy} />
        <div className="flex items-center justify-between gap-2 rounded-2xl border border-border bg-card px-4 py-3">
          <span className={cn("text-xs", dirty ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
            {dirty ? "有未保存的改动" : "已是最新"}
          </span>
          <div className="flex gap-2">
            <Button size="sm" variant="ghost" disabled={!dirty || mutation.isPending} onClick={onReset}>
              <RotateCcw className="size-3.5" /> 还原
            </Button>
            <Button size="sm" disabled={(!dirty && configured) || mutation.isPending} onClick={handleSave}>
              <Save className="size-3.5" />
              {mutation.isPending ? "保存中" : "保存"}
            </Button>
          </div>
        </div>
      </aside>
    </div>
  );
}

function TierRow({
  tier,
  index,
  count,
  defaultClassLimits,
  features,
  onChange,
  onMove,
  onRemove
}: {
  tier: DeviceTier;
  index: number;
  count: number;
  defaultClassLimits: ClassLimits;
  features: Array<{ tag: string; name: string }>;
  onChange: (tier: DeviceTier) => void;
  onMove: (delta: number) => void;
  onRemove: () => void;
}) {
  const ownClasses = cleanClassLimits(tier.classLimits);
  const hasOwn = Object.keys(ownClasses).length > 0;
  const [open, setOpen] = useState(hasOwn);
  const Icon = AUDIENCE_META[tier.audience]?.icon ?? Layers;

  const setAudience = (audience: DeviceTierAudience) => {
    const renamed = !tier.name || Object.values(AUDIENCE_META).some((meta) => meta.label === tier.name);
    onChange({
      ...tier,
      audience,
      featureTag: audience === "feature" ? tier.featureTag : undefined,
      name: renamed ? AUDIENCE_META[audience].label : tier.name
    });
  };

  return (
    <li className="rounded-xl border border-border bg-background">
      <div className="flex flex-wrap items-center gap-2.5 p-2.5">
        <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-sky-500/10 text-[11px] font-semibold tabular-nums text-sky-700 ring-1 ring-inset ring-sky-500/20 dark:text-sky-300">
          {index + 1}
        </span>
        <Select value={tier.audience} onValueChange={(value) => setAudience(value as DeviceTierAudience)}>
          <SelectTrigger size="sm" className="w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {AUDIENCES.map((audience) => {
              const AudienceIcon = AUDIENCE_META[audience].icon;
              return (
                <SelectItem key={audience} value={audience} className="text-xs">
                  <AudienceIcon className="size-3.5" />
                  {AUDIENCE_META[audience].label}
                </SelectItem>
              );
            })}
          </SelectContent>
        </Select>
        {tier.audience === "feature" ? (
          <Select value={tier.featureTag ?? ""} onValueChange={(value) => onChange({ ...tier, featureTag: value })}>
            <SelectTrigger size="sm" className={cn("w-40 text-xs", !tier.featureTag && "border-amber-500/60")}>
              <SelectValue placeholder="选择功能" />
            </SelectTrigger>
            <SelectContent>
              {features.length === 0 ? (
                <div className="px-2 py-3 text-center text-xs text-muted-foreground">暂无功能标识</div>
              ) : (
                features.map((feature) => (
                  <SelectItem key={feature.tag} value={feature.tag} className="text-xs">
                    {feature.name}
                    <span className="font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
                  </SelectItem>
                ))
              )}
            </SelectContent>
          </Select>
        ) : null}
        <Input
          value={tier.name}
          maxLength={40}
          placeholder={AUDIENCE_META[tier.audience].label}
          onChange={(event) => onChange({ ...tier, name: event.target.value })}
          className="h-8 w-32 min-w-0 flex-1 text-xs"
        />
        <LimitStepper value={tier.maxDevices} onChange={(value) => onChange({ ...tier, maxDevices: value })} />
        <div className="ml-auto flex items-center gap-0.5">
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            className={cn("size-8", open && "bg-muted")}
            onClick={() => setOpen((value) => !value)}
            aria-label="类别上限"
            title="类别上限"
          >
            <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} />
          </Button>
          <Button type="button" size="icon-sm" variant="ghost" className="size-8" disabled={index === 0} onClick={() => onMove(-1)} aria-label="上移">
            <ArrowUp className="size-3.5" />
          </Button>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            className="size-8"
            disabled={index === count - 1}
            onClick={() => onMove(1)}
            aria-label="下移"
          >
            <ArrowDown className="size-3.5" />
          </Button>
          <Button type="button" size="icon-sm" variant="ghost" className="size-8 text-destructive" onClick={onRemove} aria-label="删除">
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>
      {open ? (
        <div className="border-t border-border bg-muted/30 p-2.5">
          <div className="mb-2 flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <Icon className="size-3" />
            {hasOwn ? "本档的类别上限" : "沿用默认的类别上限"}
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {DEVICE_CLASSES.map((cls) => {
              const own = ownClasses[cls] ?? 0;
              const inherited = defaultClassLimits?.[cls] ?? 0;
              return (
                <div key={cls} className="flex items-center justify-between gap-2 rounded-lg bg-background px-2.5 py-1.5">
                  <span className="flex items-center gap-1.5 text-xs">
                    <ClassIcon cls={cls} className="size-3.5 text-muted-foreground" />
                    {CLASS_META[cls].label}
                  </span>
                  <LimitStepper
                    value={hasOwn ? own : inherited}
                    onChange={(value) =>
                      onChange({
                        ...tier,
                        // 第一次改动时把默认值抄进来，之后这一档就有了自己的整组类别上限
                        classLimits: cleanClassLimits({ ...(hasOwn ? ownClasses : defaultClassLimits), [cls]: value })
                      })
                    }
                  />
                </div>
              );
            })}
          </div>
          {hasOwn ? (
            <button
              type="button"
              onClick={() => onChange({ ...tier, classLimits: null })}
              className="mt-2 text-[11px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
            >
              改回沿用默认
            </button>
          ) : null}
        </div>
      ) : null}
    </li>
  );
}

/** 生效上限一览：默认一行，每一档会员规则一行，按优先级从上到下 */
function EffectivePreview({ policy }: { policy: DeviceLimitPolicy }) {
  const rows = [
    ...policy.tiers.map((tier) => ({
      key: tier.id,
      label: tier.name || AUDIENCE_META[tier.audience].label,
      icon: AUDIENCE_META[tier.audience]?.icon ?? Layers,
      max: tier.maxDevices,
      classes: Object.keys(cleanClassLimits(tier.classLimits)).length ? cleanClassLimits(tier.classLimits) : policy.classLimits,
      tier: true
    })),
    { key: "default", label: "其他用户", icon: Users, max: policy.maxDevices, classes: policy.classLimits, tier: false }
  ];
  return (
    <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
      <header className="flex items-center justify-between border-b border-border px-4 py-3">
        <h3 className="text-sm font-semibold tracking-tight">生效上限</h3>
        {policy.enabled ? <Badge variant="success" size="sm">已开启</Badge> : <Badge variant="outline" size="sm">未开启</Badge>}
      </header>
      <ol className={cn("divide-y divide-border", !policy.enabled && "opacity-50")}>
        <li className="flex items-center gap-2 px-4 py-2.5 text-[11px] text-muted-foreground">
          <Badge variant="warning" size="sm">单人例外</Badge>
          优先于以下全部
        </li>
        {rows.map((row) => {
          const Icon = row.icon;
          const classes = DEVICE_CLASSES.filter((cls) => (row.classes?.[cls] ?? 0) > 0);
          return (
            <li key={row.key} className="space-y-2 px-4 py-3">
              <div className="flex items-center justify-between gap-2">
                <span className="flex min-w-0 items-center gap-1.5 text-xs font-medium">
                  <Icon className={cn("size-3.5 shrink-0", row.tier ? "text-sky-600 dark:text-sky-400" : "text-muted-foreground")} />
                  <span className="truncate">{row.label}</span>
                </span>
                <span className="shrink-0 text-xs font-semibold tabular-nums">{limitText(row.max)}</span>
              </div>
              <DeviceSlots limit={row.max} used={row.max} size="sm" />
              {classes.length ? (
                <div className="flex flex-wrap gap-1">
                  {classes.map((cls) => (
                    <span key={cls} className={cn("inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] ring-1 ring-inset", CLASS_META[cls].tint)}>
                      <ClassIcon cls={cls} className="size-2.5" />
                      {CLASS_META[cls].label} {row.classes?.[cls]}
                    </span>
                  ))}
                </div>
              ) : null}
            </li>
          );
        })}
      </ol>
      <footer className="space-y-1 border-t border-border bg-muted/30 px-4 py-3 text-[11px] text-muted-foreground">
        <div>超限：{OVERFLOW_META[policy.overflow].short}</div>
        <div>同一设备：{SAME_DEVICE_META[policy.sameDevice].title}</div>
        {policy.newDevice.enabled ? (
          <div>
            {policy.newDevice.windowDays} 天内最多 {policy.newDevice.maxNew} 台新设备
          </div>
        ) : null}
      </footer>
    </section>
  );
}
