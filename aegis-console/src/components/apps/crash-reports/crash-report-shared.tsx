"use client";

import { Badge } from "@/components/ui/badge";
import type { CrashReport, CrashReportKind } from "@/lib/api/crash-reports";

/** 报告类型 → 标签与徽标色。与后端 crashreport.Kinds 一一对应，顺序即筛选项顺序。 */
export const CRASH_KIND_META: Record<CrashReportKind, { label: string; variant: "danger" | "warning" | "info" | "secondary" }> = {
  java: { label: "Java 异常", variant: "danger" },
  native: { label: "原生崩溃", variant: "danger" },
  anr: { label: "无响应", variant: "warning" },
  exit: { label: "异常退出", variant: "info" }
};

export const CRASH_KINDS = Object.keys(CRASH_KIND_META) as CrashReportKind[];

export function CrashKindBadge({ kind }: { kind: CrashReportKind }) {
  const meta = CRASH_KIND_META[kind] ?? { label: kind, variant: "secondary" as const };
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export function CrashStatusBadge({ resolved }: { resolved: boolean }) {
  return (
    <Badge variant={resolved ? "success" : "secondary"} size="sm">
      {resolved ? "已处理" : "未处理"}
    </Badge>
  );
}

/** 字节数的人话形式。 */
export function formatBytes(value: number) {
  if (!value) return "0 B";
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(2)} MB`;
}

/** 设备一行：字典展示名优先，其次上报的型号。 */
export function crashDeviceLabel(item: CrashReport) {
  return item.device?.name || item.model || "";
}

/** 应用版本一行：版本名加版本号。 */
export function crashVersionLabel(item: CrashReport) {
  if (!item.appVersion && item.versionCode == null) return "";
  if (item.versionCode == null) return item.appVersion ?? "";
  return item.appVersion ? `${item.appVersion} (${item.versionCode})` : String(item.versionCode);
}

/** 位置一行：国家、地区、城市去重后拼接。 */
export function crashLocationLabel(item: CrashReport) {
  const parts = [item.location?.country, item.location?.region, item.location?.city].filter(
    (part): part is string => Boolean(part)
  );
  return parts.filter((part, index) => parts.indexOf(part) === index).join(" ");
}

/** 把报告正文存成 .txt。 */
export function downloadCrashReport(item: CrashReport) {
  const blob = new Blob([item.report ?? ""], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `${item.publicId || `crash-${item.id}`}.txt`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}
