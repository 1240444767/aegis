"use client";

import type { ClientControlAction } from "@/lib/api/client-control";
import { Badge } from "@/components/ui/badge";

/** 版本管控共用的取值表与小部件。取值与后端 internal/domain/app/client_control.go 一一对应。 */

export const DATA_FONT = "font-mono tabular-nums";

export const CONTROL_ACTIONS: Array<{
  value: ClientControlAction;
  label: string;
  hint: string;
  variant: "success" | "info" | "warning" | "danger";
}> = [
  { value: "allow", label: "放行", hint: "正常使用，常用于给测试设备开例外", variant: "success" },
  { value: "warn", label: "提醒", hint: "弹出提示，可以继续使用", variant: "info" },
  { value: "force_update", label: "强制更新", hint: "必须更新到新版本才能继续使用", variant: "warning" },
  { value: "block", label: "停用", hint: "立即停止使用，只有新的放行裁决才能解除", variant: "danger" }
];

export const CONTROL_REASONS: Record<string, string> = {
  disabled: "管控未启用",
  rule: "命中规则",
  package_not_allowed: "非官方包名",
  signature_mismatch: "签名不符",
  default: "默认放行",
  undetermined: "需客户端上报确认"
};

export const CONTROL_PLATFORMS = [
  { value: "android", label: "Android" },
  { value: "ios", label: "iOS" },
  { value: "harmonyos", label: "HarmonyOS" },
  { value: "windows", label: "Windows" },
  { value: "macos", label: "macOS" },
  { value: "linux", label: "Linux" }
];

export function actionLabel(value?: string) {
  return CONTROL_ACTIONS.find((item) => item.value === value)?.label ?? value ?? "—";
}

export function ActionBadge({ value, size }: { value?: string; size?: "sm" | "default" }) {
  const item = CONTROL_ACTIONS.find((entry) => entry.value === value);
  return (
    <Badge variant={item?.variant ?? "secondary"} size={size}>
      {item?.label ?? value ?? "—"}
    </Badge>
  );
}

export function reasonLabel(value?: string) {
  return (value && CONTROL_REASONS[value]) || value || "—";
}

/** 证书摘要只显示首尾，完整值放在 title 里。 */
export function shortCert(value?: string) {
  if (!value) return "未上报";
  return value.length > 16 ? `${value.slice(0, 8)}…${value.slice(-8)}` : value;
}

/** 一行一个或逗号分隔的列表输入。 */
export function parseList(value: string) {
  return value
    .split(/[\n,，\s]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function joinList(values?: Array<string | number>) {
  return (values ?? []).join("\n");
}

export function formatSeconds(seconds: number) {
  if (seconds % 86400 === 0) return `${seconds / 86400} 天`;
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}
