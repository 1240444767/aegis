"use client";

import { Loader2, Lock, RotateCw, WifiOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatClockSeconds } from "@/lib/datetime";
import { useLive, useLiveClient } from "@/lib/online-live/hooks";
import type { LiveDimensionKey } from "@/lib/online-live/types";
import { cn } from "@/lib/utils";

/** 两条序列的配色，亮暗各一组，均已过色觉与对比度校验。 */
export const SERIES_COLORS = {
  users: { light: "#2563eb", dark: "#3b82f6" },
  connections: { light: "#d97706", dark: "#c97a06" }
} as const;

export function formatCount(value?: number | null) {
  return typeof value === "number" ? value.toLocaleString("zh-CN") : "0";
}

/** 分布桶的显示名：空串为客户端未上报，"*" 为截断后其余项之和。 */
export function bucketLabel(key: string) {
  if (key === "") return "未上报";
  if (key === "*") return "其他";
  return key;
}

export const DIMENSION_TABS: Array<{ key: LiveDimensionKey; label: string }> = [
  { key: "platform", label: "平台" },
  { key: "os", label: "系统" },
  { key: "appVersion", label: "版本" },
  { key: "device", label: "机型" },
  { key: "country", label: "国家" },
  { key: "region", label: "地区" }
];

/** 「3 小时 12 分」样式的时长，入参为秒。 */
export function formatDuration(seconds?: number | null) {
  if (!seconds || seconds < 60) return "不足 1 分钟";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时 ${minutes % 60} 分`;
  return `${Math.floor(hours / 24)} 天 ${hours % 24} 小时`;
}

/**
 * 连接状态：实时、重连中或失败。加密套件与往返时延放在提示里，
 * 失败时给出原因与重试按钮。
 */
export function LiveStatusBadge() {
  const client = useLiveClient();
  const status = useLive((s) => s.status);
  const error = useLive((s) => s.error);
  const latency = useLive((s) => s.latency);
  const updatedAt = useLive((s) => s.snapshot?.at);

  if (status === "failed") {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-2.5 py-1.5 text-xs text-destructive">
        <WifiOff className="size-3.5" />
        <span className="max-w-64 truncate">{error || "连接失败"}</span>
        <Button variant="ghost" size="sm" className="h-6 gap-1 px-1.5 text-xs" onClick={() => client.retry()}>
          <RotateCw className="size-3" />
          重试
        </Button>
      </div>
    );
  }

  if (status !== "open") {
    return (
      <div className="flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" />
        <span>{status === "reconnecting" ? "连接中断，正在重连" : "正在建立加密连接"}</span>
      </div>
    );
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex cursor-default items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs">
          <span className="relative flex size-2">
            <span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-500/60" />
            <span className="relative inline-flex size-2 rounded-full bg-emerald-500" />
          </span>
          <span className="font-medium">实时</span>
          <Lock className="size-3 text-muted-foreground" />
          <span className="text-muted-foreground tabular-nums">{latency != null ? `${latency} ms` : "加密"}</span>
        </div>
      </TooltipTrigger>
      <TooltipContent side="bottom" className="max-w-72 text-xs leading-5">
        <div>端到端加密：ECDH P-256 密钥交换，AES-256-GCM 逐帧加密</div>
        <div>每次连接使用独立的临时密钥</div>
        {updatedAt ? <div>最近更新 {formatClockSeconds(updatedAt)}</div> : null}
      </TooltipContent>
    </Tooltip>
  );
}

/** 卡片标题行：左侧标题，右侧附加内容。 */
export function PanelHeader({ title, icon, extra, className }: { title: string; icon?: React.ReactNode; extra?: React.ReactNode; className?: string }) {
  return (
    <div className={cn("flex min-h-11 flex-wrap items-center justify-between gap-2 border-b px-4 py-2", className)}>
      <h3 className="flex items-center gap-2 text-sm font-semibold">
        {icon}
        {title}
      </h3>
      {extra}
    </div>
  );
}
