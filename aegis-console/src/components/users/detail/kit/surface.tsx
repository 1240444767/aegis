"use client";

/**
 * 用户详情页的版面原语（第二版）。
 *
 * 一个页签由若干 `Section` 组成：标题行带色调图标块、可选说明与右侧操作，正文区不再套第二层卡片。
 * 数字用 `MetricCard`，字段用 `InfoGrid`，空 / 错状态用 `StateBlock`，所有页签共用这一套，
 * 换页签时视觉节奏一致。颜色只走语义色（tone），不在业务代码里散写色板。
 */

import * as React from "react";
import { AlertTriangle, Check, Copy, Inbox, RotateCw } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export type Tone = "neutral" | "primary" | "success" | "warning" | "danger" | "info" | "violet";

const TONE_TILE: Record<Tone, string> = {
  neutral: "bg-muted text-muted-foreground",
  primary: "bg-primary/10 text-primary",
  success: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  warning: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  danger: "bg-red-500/10 text-red-600 dark:text-red-400",
  info: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
  violet: "bg-violet-500/10 text-violet-600 dark:text-violet-400"
};

export const TONE_TEXT: Record<Tone, string> = {
  neutral: "text-foreground",
  primary: "text-primary",
  success: "text-emerald-600 dark:text-emerald-400",
  warning: "text-amber-600 dark:text-amber-400",
  danger: "text-red-600 dark:text-red-400",
  info: "text-sky-600 dark:text-sky-400",
  violet: "text-violet-600 dark:text-violet-400"
};

/** 色调图标块：图标统一放在带底色的圆角方块里。 */
export function ToneIcon({
  icon: Icon,
  tone = "neutral",
  size = "md",
  className
}: {
  icon: React.ComponentType<{ className?: string }>;
  tone?: Tone;
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center",
        size === "sm" && "size-7 rounded-lg [&>svg]:size-3.5",
        size === "md" && "size-8 rounded-[10px] [&>svg]:size-4",
        size === "lg" && "size-10 rounded-xl [&>svg]:size-[18px]",
        TONE_TILE[tone],
        className
      )}
    >
      <Icon />
    </span>
  );
}

/** 页签里的一个区块。`flush` 让正文贴边（表格、列表），否则正文有内边距。 */
export function Section({
  title,
  description,
  icon,
  tone = "neutral",
  actions,
  toolbar,
  footer,
  flush = false,
  className,
  bodyClassName,
  children
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  icon?: React.ComponentType<{ className?: string }>;
  tone?: Tone;
  actions?: React.ReactNode;
  /** 标题行下方、正文上方的一条工具栏（筛选、分段选择） */
  toolbar?: React.ReactNode;
  footer?: React.ReactNode;
  flush?: boolean;
  className?: string;
  bodyClassName?: string;
  children?: React.ReactNode;
}) {
  return (
    <Card className={cn("gap-0 overflow-hidden py-0", className)}>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 pt-4 pb-3 sm:px-5">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          {icon ? <ToneIcon icon={icon} tone={tone} /> : null}
          <div className="flex min-w-0 flex-col gap-0.5">
            <h3 className="truncate text-sm font-semibold tracking-tight">{title}</h3>
            {description ? <p className="text-xs leading-5 text-muted-foreground">{description}</p> : null}
          </div>
        </div>
        {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
      {toolbar ? <div className="border-t bg-muted/30 px-4 py-2.5 sm:px-5">{toolbar}</div> : null}
      {children !== undefined && children !== null ? (
        <div className={cn("border-t", !flush && "px-4 py-4 sm:px-5", bodyClassName)}>{children}</div>
      ) : null}
      {footer ? <div className="border-t bg-muted/20 px-4 py-2.5 sm:px-5">{footer}</div> : null}
    </Card>
  );
}

/** 指标卡。`spark` 给一串数时在右下角画一条迷你折线。 */
export function MetricCard({
  label,
  value,
  sub,
  icon,
  tone = "neutral",
  spark,
  onClick,
  loading,
  className
}: {
  label: React.ReactNode;
  value: React.ReactNode;
  sub?: React.ReactNode;
  icon?: React.ComponentType<{ className?: string }>;
  tone?: Tone;
  spark?: number[];
  onClick?: () => void;
  loading?: boolean;
  className?: string;
}) {
  const body = (
    <>
      <div className="flex items-center justify-between gap-2">
        <span className="truncate text-xs text-muted-foreground">{label}</span>
        {icon ? <ToneIcon icon={icon} tone={tone} size="sm" /> : null}
      </div>
      {loading ? (
        <Skeleton className="mt-2 h-7 w-24" />
      ) : (
        <div className="mt-1.5 truncate text-2xl font-semibold tracking-tight tabular-nums">{value}</div>
      )}
      <div className="mt-1 flex min-h-5 items-end justify-between gap-2">
        <span className="min-w-0 truncate text-xs text-muted-foreground">{loading ? null : sub}</span>
        {spark && spark.length > 1 ? <Sparkline values={spark} tone={tone} /> : null}
      </div>
    </>
  );
  const classes = cn(
    "flex min-w-0 flex-col rounded-xl border bg-card px-4 py-3.5 text-left text-card-foreground",
    onClick && "cursor-pointer transition-colors hover:bg-accent/40",
    className
  );
  return onClick ? (
    <button type="button" className={classes} onClick={onClick}>
      {body}
    </button>
  ) : (
    <div className={classes}>{body}</div>
  );
}

/** 迷你折线：纯 SVG，不经过图表库，几十张卡同屏也不卡。 */
export function Sparkline({ values, tone = "primary" }: { values: number[]; tone?: Tone }) {
  const width = 72;
  const height = 22;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const step = width / (values.length - 1);
  const points = values
    .map((v, i) => `${(i * step).toFixed(1)},${(height - 2 - ((v - min) / span) * (height - 4)).toFixed(1)}`)
    .join(" ");
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className={cn("shrink-0", TONE_TEXT[tone === "neutral" ? "primary" : tone])} aria-hidden>
      <polyline points={points} fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

export type InfoItem = {
  label: React.ReactNode;
  value?: React.ReactNode;
  icon?: React.ComponentType<{ className?: string }>;
  mono?: boolean;
  /** 给出时值旁边带复制按钮 */
  copy?: string | null;
  hint?: React.ReactNode;
  tone?: Tone;
  /** 占满整行（两列布局下） */
  wide?: boolean;
};

const EMPTY = "—";

/** 字段网格：标签在上、值在下，两列或三列，适合档案类信息。 */
export function InfoGrid({ items, columns = 2, className }: { items: InfoItem[]; columns?: 1 | 2 | 3; className?: string }) {
  return (
    <dl
      className={cn(
        "grid gap-x-6 gap-y-4",
        columns === 2 && "sm:grid-cols-2",
        columns === 3 && "sm:grid-cols-2 lg:grid-cols-3",
        className
      )}
    >
      {items.map((item, index) => {
        const empty = item.value === null || item.value === undefined || item.value === "" || item.value === EMPTY;
        const Icon = item.icon;
        return (
          <div key={index} className={cn("flex min-w-0 flex-col gap-1", item.wide && "sm:col-span-full")}>
            <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
              {Icon ? <Icon className="size-3.5" /> : null}
              {item.label}
            </dt>
            <dd className="flex min-w-0 items-start gap-1">
              <span
                className={cn(
                  "min-w-0 text-sm break-all",
                  item.mono && "font-mono text-[13px]",
                  empty ? "text-muted-foreground" : item.tone && TONE_TEXT[item.tone]
                )}
              >
                {empty ? EMPTY : item.value}
              </span>
              {item.copy && !empty ? <CopyButton value={item.copy} label={typeof item.label === "string" ? item.label : ""} /> : null}
            </dd>
            {item.hint ? <div className="text-xs text-muted-foreground">{item.hint}</div> : null}
          </div>
        );
      })}
    </dl>
  );
}

export async function copyToClipboard(value: string, label = "") {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(label ? `已复制${label}` : "已复制");
    return true;
  } catch {
    toast.error("复制失败");
    return false;
  }
}

export function CopyButton({ value, label, className }: { value: string; label?: string; className?: string }) {
  const [copied, setCopied] = React.useState(false);
  return (
    <button
      type="button"
      title={label ? `复制${label}` : "复制"}
      className={cn(
        "inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
        className
      )}
      onClick={async (event) => {
        event.stopPropagation();
        if (await copyToClipboard(value, label)) {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1200);
        }
      }}
    >
      {copied ? <Check className="size-3 text-emerald-500" /> : <Copy className="size-3" />}
    </button>
  );
}

/** 空状态与错误状态。错误时给「重试」，空时可给一个主操作。 */
export function StateBlock({
  kind = "empty",
  title,
  description,
  action,
  onRetry,
  icon,
  compact
}: {
  kind?: "empty" | "error";
  title?: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  onRetry?: () => void;
  icon?: React.ComponentType<{ className?: string }>;
  compact?: boolean;
}) {
  const Icon = icon ?? (kind === "error" ? AlertTriangle : Inbox);
  return (
    <div className={cn("flex flex-col items-center justify-center gap-2 px-6 text-center", compact ? "py-8" : "py-12")}>
      <ToneIcon icon={Icon} tone={kind === "error" ? "danger" : "neutral"} size="lg" />
      <div className="mt-1 text-sm font-medium">{title ?? (kind === "error" ? "加载失败" : "暂无记录")}</div>
      {description ? <p className="max-w-sm text-xs leading-5 text-muted-foreground">{description}</p> : null}
      {kind === "error" && onRetry ? (
        <Button size="sm" variant="outline" className="mt-1" onClick={onRetry}>
          <RotateCw />
          重试
        </Button>
      ) : null}
      {action ? <div className="mt-1">{action}</div> : null}
    </div>
  );
}

export function errorMessage(error: unknown, fallback = "请求失败") {
  return error instanceof Error && error.message ? error.message : fallback;
}
