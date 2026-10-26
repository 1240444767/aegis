"use client";

import { cn } from "@/lib/utils";

/** 带符号的变动额：正数绿色带 +，负数红色，0 灰色。数字按千分位，等宽数字。 */
export function SignedAmount({
  value,
  unit,
  className
}: {
  value: number;
  unit?: string;
  className?: string;
}) {
  const positive = value > 0;
  const negative = value < 0;
  return (
    <span
      className={cn(
        "font-medium tabular-nums",
        positive && "text-emerald-600 dark:text-emerald-400",
        negative && "text-red-600 dark:text-red-400",
        !positive && !negative && "text-muted-foreground",
        className
      )}
    >
      {positive ? "+" : ""}
      {value.toLocaleString("zh-CN")}
      {unit ? <span className="ml-0.5 text-xs font-normal text-muted-foreground">{unit}</span> : null}
    </span>
  );
}

/** 千分位整数 */
export function formatInt(value?: number | null) {
  return typeof value === "number" && Number.isFinite(value) ? value.toLocaleString("zh-CN") : "—";
}
