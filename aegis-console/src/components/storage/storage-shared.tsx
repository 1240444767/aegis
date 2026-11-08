"use client";

import { Cloud, Database, HardDrive, Server } from "lucide-react";
import { siAlibabacloud, siCloudflare, siDropbox, siGoogledrive, siMinio } from "simple-icons";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type {
  StorageMemberMode,
  StorageMemberState,
  StoragePoolStrategy,
  StorageProviderMeta
} from "@/lib/api/storage-center";

export { formatBytes } from "@/components/storage/file-shared";

/* ================================================================== */
/*  品牌徽章                                                           */
/* ================================================================== */

/**
 * 存储提供商品牌图标。与邮件、支付渠道同一规范：图形取自 Simple Icons，slug 由后端目录下发；
 * Simple Icons 未收录的（S3、OneDrive、Azure、腾讯云、七牛……）按分组用中性图标，不冒充品牌。
 */
type IconEntry = { hex: string; path: string };

const registry: Record<string, IconEntry> = {
  cloudflare: siCloudflare,
  minio: siMinio,
  alibabacloud: siAlibabacloud,
  dropbox: siDropbox,
  googledrive: siGoogledrive
};

function NeutralIcon({ provider, category, className }: { provider?: string; category?: string; className?: string }) {
  if (provider === "local") return <HardDrive className={className} />;
  switch (category) {
    case "drive":
    case "china":
      return <Cloud className={className} />;
    case "self_host":
      return <Server className={className} />;
    default:
      return <Database className={className} />;
  }
}

export function StorageBrandBadge({
  meta,
  provider,
  size = "md",
  className
}: {
  meta?: Pick<StorageProviderMeta, "icon" | "brandColor" | "category" | "provider"> | null;
  provider?: string;
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  const entry = meta?.icon ? registry[meta.icon] : undefined;
  const color = meta?.brandColor ?? (entry ? `#${entry.hex}` : "#64748B");
  const box = size === "sm" ? "size-7 rounded-lg" : size === "lg" ? "size-11 rounded-2xl" : "size-9 rounded-xl";
  const glyph = size === "sm" ? "size-3.5" : size === "lg" ? "size-5.5" : "size-4.5";
  return (
    <span
      className={cn("inline-flex shrink-0 items-center justify-center", box, className)}
      style={{ backgroundColor: `${color}1F`, color }}
    >
      {entry ? (
        <svg viewBox="0 0 24 24" className={glyph} fill="currentColor" aria-hidden>
          <path d={entry.path} />
        </svg>
      ) : (
        <NeutralIcon provider={meta?.provider ?? provider} category={meta?.category} className={glyph} />
      )}
    </span>
  );
}

/** Cloudflare 标志（按钮里用，不带底色）。 */
export function CloudflareMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={cn("size-4 text-[#F38020]", className)} fill="currentColor" aria-hidden>
      <path d={siCloudflare.path} />
    </svg>
  );
}

/* ================================================================== */
/*  状态                                                               */
/* ================================================================== */

type StateMeta = {
  label: string;
  variant: "success" | "warning" | "danger" | "info" | "outline";
  dot: string;
};

export const STATE_META: Record<StorageMemberState, StateMeta> = {
  healthy: { label: "正常", variant: "success", dot: "bg-emerald-500" },
  degraded: { label: "不稳定", variant: "warning", dot: "bg-amber-500" },
  backoff: { label: "退避中", variant: "danger", dot: "bg-red-500" },
  near_full: { label: "将满", variant: "warning", dot: "bg-amber-500" },
  full: { label: "已满", variant: "danger", dot: "bg-red-500" },
  readonly: { label: "只读", variant: "info", dot: "bg-sky-500" },
  disabled: { label: "已停用", variant: "outline", dot: "bg-muted-foreground/50" }
};

export function StateBadge({ state, className }: { state?: StorageMemberState; className?: string }) {
  const meta = STATE_META[state ?? "healthy"] ?? STATE_META.healthy;
  return (
    <Badge variant={meta.variant} size="sm" className={cn("gap-1", className)}>
      <span className={cn("size-1.5 rounded-full", meta.dot)} />
      {meta.label}
    </Badge>
  );
}

/* ================================================================== */
/*  容量条                                                             */
/* ================================================================== */

export function UsageMeter({
  used,
  capacity,
  warnPercent = 85,
  className
}: {
  used: number;
  capacity?: number;
  warnPercent?: number;
  className?: string;
}) {
  const limited = Boolean(capacity && capacity > 0);
  const ratio = limited ? Math.min(1, used / (capacity as number)) : 0;
  const tone = !limited
    ? "bg-sky-500"
    : ratio >= 1
      ? "bg-red-500"
      : ratio * 100 >= warnPercent
        ? "bg-amber-500"
        : "bg-emerald-500";
  return (
    <div className={cn("space-y-1", className)}>
      <div className="flex items-baseline justify-between gap-2 text-[11px] tabular-nums">
        <span className="font-medium text-foreground">{formatSize(used)}</span>
        <span className="text-muted-foreground">
          {limited ? `${formatSize(capacity as number)} · ${Math.round(ratio * 100)}%` : "不限"}
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div
          className={cn("h-full rounded-full transition-[width] duration-500", tone)}
          style={{ width: limited ? `${Math.max(ratio > 0 ? 2 : 0, ratio * 100)}%` : used > 0 ? "100%" : "0%" }}
        />
      </div>
    </div>
  );
}

/* ================================================================== */
/*  策略与角色                                                         */
/* ================================================================== */

export const STRATEGY_META: Record<StoragePoolStrategy, { label: string; short: string }> = {
  priority: { label: "按优先级", short: "写满一个再用下一个" },
  round_robin: { label: "轮询", short: "依次轮流写入" },
  weighted: { label: "按权重", short: "按权重随机分配" },
  least_used: { label: "最少占用", short: "优先写入占用比例最低的" }
};

export const MODE_META: Record<StorageMemberMode, { label: string }> = {
  active: { label: "主用" },
  standby: { label: "备用" },
  readonly: { label: "只读" }
};

/* ================================================================== */
/*  大小换算                                                           */
/* ================================================================== */

const UNITS = ["MB", "GB", "TB"] as const;
export type SizeUnit = (typeof UNITS)[number];
export const SIZE_UNITS = UNITS;

const UNIT_BYTES: Record<SizeUnit, number> = { MB: 1024 ** 2, GB: 1024 ** 3, TB: 1024 ** 4 };

/** 字节数拆成「数值 + 单位」，挑能整除的最大单位，0 返回空串（表示不限）。 */
export function splitSize(bytes: number): { value: string; unit: SizeUnit } {
  if (!bytes || bytes <= 0) return { value: "", unit: "GB" };
  for (const unit of [...UNITS].reverse()) {
    const value = bytes / UNIT_BYTES[unit];
    if (value >= 1 && Number.isInteger(Math.round(value * 100) / 100)) {
      return { value: String(Math.round(value * 100) / 100), unit };
    }
  }
  return { value: String(Math.round((bytes / UNIT_BYTES.MB) * 100) / 100), unit: "MB" };
}

export function joinSize(value: string, unit: SizeUnit): number {
  const parsed = Number.parseFloat(value);
  if (!Number.isFinite(parsed) || parsed <= 0) return 0;
  return Math.round(parsed * UNIT_BYTES[unit]);
}

const SIZE_LABELS = ["B", "KB", "MB", "GB", "TB", "PB"];

/** 紧凑大小（卡片与图表刻度用）。 */
export function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), SIZE_LABELS.length - 1);
  const value = bytes / 1024 ** exponent;
  return `${value.toFixed(exponent === 0 || value >= 100 ? 0 : 1)} ${SIZE_LABELS[exponent]}`;
}

export function providerName(providers: StorageProviderMeta[] | undefined, provider: string): string {
  return providers?.find((item) => item.provider === provider)?.name ?? provider;
}

export function providerMeta(providers: StorageProviderMeta[] | undefined, provider: string) {
  return providers?.find((item) => item.provider === provider);
}
