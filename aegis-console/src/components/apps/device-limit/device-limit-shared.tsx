"use client";

import type { ReactNode } from "react";
import {
  Ban,
  BadgeCheck,
  CircleDashed,
  Crown,
  Gem,
  Globe,
  History,
  Hourglass,
  Laptop,
  ListChecks,
  LogOut,
  Minus,
  MousePointerClick,
  Plus,
  Replace,
  ShieldX,
  Smartphone,
  Sparkles,
  UserRoundX,
  Users,
  Wand2,
  type LucideIcon
} from "lucide-react";
import type { ThemedColor } from "@/components/commerce/commerce-charts";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type {
  ClassLimits,
  DeviceClass,
  DeviceDimension,
  DeviceLimitAction,
  DeviceLimitPolicy,
  DeviceLimitSource,
  DeviceOverflow,
  DeviceSameDevice,
  DeviceTierAudience
} from "@/lib/api/device-limit";
import { cn } from "@/lib/utils";

/**
 * 登录设备数的展示口径：类别、处置方式、事件、来源的文案 / 图标 / 配色，以及几个小部件。
 * 集中在一处，概览、策略、用户设备、记录与模拟说的是同一套话、用的是同一种颜色。
 */

export const DEVICE_CLASSES: DeviceClass[] = ["mobile", "desktop", "web", "other"];

/**
 * `color` 给图表（ChartConfig 的 theme），`fill` / `tint` 给槽位与图标底：两套写的是同一组色，
 * 改一处要同步改另一处，否则图例和槽位颜色对不上。
 */
export const CLASS_META: Record<
  DeviceClass,
  { label: string; icon: LucideIcon; color: ThemedColor; fill: string; tint: string }
> = {
  mobile: {
    label: "手机",
    icon: Smartphone,
    color: { light: "#6366f1", dark: "#818cf8" },
    fill: "bg-indigo-500 ring-indigo-500 dark:bg-indigo-400 dark:ring-indigo-400",
    tint: "bg-indigo-500/10 text-indigo-600 ring-indigo-500/20 dark:text-indigo-400"
  },
  desktop: {
    label: "电脑",
    icon: Laptop,
    color: { light: "#0ea5e9", dark: "#38bdf8" },
    fill: "bg-sky-500 ring-sky-500 dark:bg-sky-400 dark:ring-sky-400",
    tint: "bg-sky-500/10 text-sky-600 ring-sky-500/20 dark:text-sky-400"
  },
  web: {
    label: "网页",
    icon: Globe,
    color: { light: "#14b8a6", dark: "#2dd4bf" },
    fill: "bg-teal-500 ring-teal-500 dark:bg-teal-400 dark:ring-teal-400",
    tint: "bg-teal-500/10 text-teal-600 ring-teal-500/20 dark:text-teal-400"
  },
  other: {
    label: "其他",
    icon: CircleDashed,
    color: { light: "#a1a1aa", dark: "#71717a" },
    fill: "bg-zinc-400 ring-zinc-400 dark:bg-zinc-500 dark:ring-zinc-500",
    tint: "bg-zinc-500/10 text-zinc-600 ring-zinc-500/20 dark:text-zinc-400"
  }
};

export function classOf(value?: string | null): DeviceClass {
  return (DEVICE_CLASSES as string[]).includes(value ?? "") ? (value as DeviceClass) : "other";
}

/** 平台 → 类别，与后端 devicelimit.ClassOf 一致 */
export function classOfPlatform(platform?: string | null): DeviceClass {
  switch ((platform ?? "").toLowerCase()) {
    case "android":
    case "ios":
    case "harmonyos":
      return "mobile";
    case "windows":
    case "macos":
    case "linux":
      return "desktop";
    case "web":
      return "web";
    default:
      return "other";
  }
}

export const PLATFORM_OPTIONS: Array<{ value: string; label: string }> = [
  { value: "android", label: "Android" },
  { value: "ios", label: "iOS" },
  { value: "harmonyos", label: "HarmonyOS" },
  { value: "windows", label: "Windows" },
  { value: "macos", label: "macOS" },
  { value: "linux", label: "Linux" },
  { value: "web", label: "网页" }
];

export const OVERFLOW_META: Record<DeviceOverflow, { title: string; short: string; description: string; icon: LucideIcon }> = {
  kick_oldest: { title: "下线最早登录的设备", short: "下线最早登录", description: "新设备直接登录", icon: History },
  kick_idle: { title: "下线最久未活动的设备", short: "下线最久未活动", description: "按最近一次刷新令牌计", icon: Hourglass },
  reject: { title: "拒绝新登录", short: "拒绝新登录", description: "已登录的设备不受影响", icon: Ban },
  ask: { title: "让用户选择", short: "让用户选择", description: "列出已登录设备，选好后完成登录", icon: ListChecks }
};

export const SAME_DEVICE_META: Record<DeviceSameDevice, { title: string; description: string; icon: LucideIcon }> = {
  replace: { title: "顶替旧登录", description: "一台设备只保留一份登录", icon: Replace },
  coexist: { title: "保留旧登录", description: "仍只算一台设备", icon: Users }
};

export const AUDIENCE_META: Record<DeviceTierAudience, { label: string; icon: LucideIcon }> = {
  trial: { label: "试用会员", icon: Sparkles },
  vip: { label: "会员", icon: Crown },
  lifetime: { label: "永久会员", icon: Gem },
  feature: { label: "持有功能", icon: BadgeCheck }
};

export const ACTION_META: Record<
  DeviceLimitAction,
  { label: string; icon: LucideIcon; variant: "danger" | "warning" | "info" | "success" | "secondary" | "outline"; color: ThemedColor }
> = {
  kicked: { label: "自动下线", icon: LogOut, variant: "warning", color: { light: "#f59e0b", dark: "#fbbf24" } },
  rejected: { label: "拒绝登录", icon: Ban, variant: "danger", color: { light: "#f43f5e", dark: "#fb7185" } },
  challenged: { label: "请用户选择", icon: ListChecks, variant: "info", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  resolved: { label: "用户已选择", icon: MousePointerClick, variant: "success", color: { light: "#10b981", dark: "#34d399" } },
  new_device: { label: "新设备拦截", icon: ShieldX, variant: "danger", color: { light: "#8b5cf6", dark: "#a78bfa" } },
  user_removed: { label: "用户下线", icon: UserRoundX, variant: "secondary", color: { light: "#64748b", dark: "#94a3b8" } },
  admin_removed: { label: "管理员下线", icon: UserRoundX, variant: "outline", color: { light: "#78716c", dark: "#a8a29e" } },
  admin_enforced: { label: "按策略整理", icon: Wand2, variant: "outline", color: { light: "#a1a1aa", dark: "#71717a" } }
};

export const EVENT_ACTIONS = Object.keys(ACTION_META) as DeviceLimitAction[];

/** 趋势图只画登录时发生的处置，人工操作另看记录 */
export const TREND_ACTIONS: DeviceLimitAction[] = ["kicked", "rejected", "challenged", "new_device"];

export const DIMENSION_LABEL: Record<DeviceDimension, string> = {
  total: "总数",
  class: "终端类别",
  new_device: "新设备频率"
};

export const SOURCE_META: Record<DeviceLimitSource, { label: string; variant: "secondary" | "info" | "success" | "warning" | "outline" }> = {
  disabled: { label: "未开启", variant: "outline" },
  default: { label: "默认", variant: "secondary" },
  tier: { label: "会员档位", variant: "info" },
  override: { label: "单人例外", variant: "warning" },
  exempt: { label: "豁免", variant: "success" }
};

export function limitText(limit?: number | null) {
  return limit && limit > 0 ? `${limit} 台` : "不限";
}

export function formatCount(value?: number | null) {
  return (value ?? 0).toLocaleString("zh-CN");
}

/** 只保留大于 0 的类别上限，与后端 normalizeClassLimits 一致 */
export function cleanClassLimits(limits?: ClassLimits | null): ClassLimits {
  const out: ClassLimits = {};
  for (const cls of DEVICE_CLASSES) {
    const value = limits?.[cls];
    if (value && value > 0) out[cls] = value;
  }
  return out;
}

export function defaultPolicy(): DeviceLimitPolicy {
  return {
    enabled: false,
    maxDevices: 3,
    classLimits: {},
    overflow: "kick_oldest",
    askFallback: "kick_oldest",
    sameDevice: "replace",
    tiers: [],
    newDevice: { enabled: false, windowDays: 30, maxNew: 5 },
    notifyKicked: true,
    rejectMessage: ""
  };
}

export function ClassIcon({ cls, className }: { cls: DeviceClass; className?: string }) {
  const Icon = CLASS_META[cls].icon;
  return <Icon className={className} />;
}

/** 带类别色底的图标块 */
export function ClassGlyph({ cls, className }: { cls: DeviceClass; className?: string }) {
  const Icon = CLASS_META[cls].icon;
  return (
    <span className={cn("grid size-8 shrink-0 place-items-center rounded-lg ring-1 ring-inset", CLASS_META[cls].tint, className)}>
      <Icon className="size-4" />
    </span>
  );
}

export function ActionBadge({ action }: { action: DeviceLimitAction }) {
  const meta = ACTION_META[action];
  if (!meta) return <Badge variant="outline" size="sm">{action}</Badge>;
  const Icon = meta.icon;
  return (
    <Badge variant={meta.variant} size="sm" className="gap-1">
      <Icon className="size-3" />
      {meta.label}
    </Badge>
  );
}

/**
 * 设备槽位：上限几台就画几格，已占用的实心。比「2 / 3」更快读出离上限还差多少。
 * 上限 0（不限）或超过 12 台时退回文字，画几十个格子没有意义。
 */
export function DeviceSlots({
  limit,
  used = 0,
  cls,
  size = "md"
}: {
  limit: number;
  used?: number;
  cls?: DeviceClass;
  size?: "sm" | "md";
}) {
  const box = size === "sm" ? "size-2.5 rounded-[3px]" : "size-3.5 rounded";
  if (!limit || limit <= 0 || limit > 12) {
    return (
      <span className="text-xs tabular-nums text-muted-foreground">
        {limit > 0 ? `${used} / ${limit}` : used > 0 ? `${used} / 不限` : "不限"}
      </span>
    );
  }
  const over = used > limit;
  return (
    <span className="inline-flex items-center gap-1" aria-label={`${used} / ${limit}`}>
      {Array.from({ length: limit }).map((_, index) => (
        <span
          key={index}
          className={cn(
            box,
            "ring-1 ring-inset transition-colors",
            index < used
              ? over
                ? "bg-rose-500 ring-rose-500"
                : cls
                  ? CLASS_META[cls].fill
                  : "bg-primary ring-primary"
              : "bg-transparent ring-border"
          )}
        />
      ))}
      {over ? <span className="ml-0.5 text-[10px] font-medium text-rose-600 dark:text-rose-400">+{used - limit}</span> : null}
    </span>
  );
}

/** 上限步进器：0 表示不限。取值范围大时也能直接输入 */
export function LimitStepper({
  value,
  onChange,
  max = 100,
  min = 0,
  zeroLabel = "不限",
  unit = "台",
  disabled,
  className
}: {
  value: number;
  onChange: (value: number) => void;
  max?: number;
  min?: number;
  zeroLabel?: string;
  unit?: string;
  disabled?: boolean;
  className?: string;
}) {
  const clamp = (next: number) => Math.max(min, Math.min(max, Math.round(next)));
  return (
    <div
      className={cn(
        "inline-flex h-8 items-center rounded-lg border border-border bg-background",
        disabled && "pointer-events-none opacity-50",
        className
      )}
    >
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="size-8 rounded-r-none"
        disabled={disabled || value <= min}
        onClick={() => onChange(clamp(value - 1))}
        aria-label="减少"
      >
        <Minus className="size-3.5" />
      </Button>
      <label className="relative flex h-full min-w-16 items-center justify-center border-x border-border px-1">
        <input
          type="number"
          inputMode="numeric"
          min={min}
          max={max}
          value={value}
          disabled={disabled}
          onChange={(event) => onChange(clamp(Number(event.target.value) || 0))}
          className="absolute inset-0 w-full bg-transparent text-center text-xs tabular-nums opacity-0 focus:opacity-100 focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none"
        />
        <span className="pointer-events-none text-xs font-medium tabular-nums [input:focus+&]:invisible">
          {value === 0 && zeroLabel ? zeroLabel : `${value} ${unit}`}
        </span>
      </label>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="size-8 rounded-l-none"
        disabled={disabled || value >= max}
        onClick={() => onChange(clamp(value + 1))}
        aria-label="增加"
      >
        <Plus className="size-3.5" />
      </Button>
    </div>
  );
}

/** 列表底部的分页条 */
export function DeviceLimitPager({
  total,
  page,
  limit,
  onPage
}: {
  total: number;
  page: number;
  limit: number;
  onPage: (page: number) => void;
}) {
  const pages = Math.max(1, Math.ceil(total / Math.max(1, limit)));
  if (total <= 0) return null;
  return (
    <div className="flex items-center justify-between px-1 pt-3 text-xs text-muted-foreground">
      <span className="tabular-nums">
        共 {formatCount(total)} 条，第 {page}/{pages} 页
      </span>
      <div className="flex gap-1.5">
        <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          上一页
        </Button>
        <Button size="sm" variant="outline" disabled={page >= pages} onClick={() => onPage(page + 1)}>
          下一页
        </Button>
      </div>
    </div>
  );
}

/** 面板里的一块：标题行 + 内容，不加图标底色，区别于配置卡 SectionCard */
export function Block({
  title,
  aside,
  children,
  className,
  bodyClassName
}: {
  title: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section className={cn("rounded-xl border border-border bg-card", className)}>
      <header className="flex min-h-11 items-center justify-between gap-3 border-b border-border px-4 py-2.5">
        <h3 className="text-sm font-medium">{title}</h3>
        {aside}
      </header>
      <div className={cn("p-4", bodyClassName)}>{children}</div>
    </section>
  );
}
