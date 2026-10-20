"use client";

import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

/**
 * 大号编辑 Sheet 的版面零件：AI 通道编辑与 AI 中转模型编辑共用，两边的分块、开关块长得一样。
 */

/** 一个带图标标题的分块（卡片）。 */
export function SheetBlock({
  icon,
  title,
  aside,
  children,
  className
}: {
  icon: React.ReactNode;
  title: string;
  aside?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("rounded-2xl border bg-card p-5", className)}>
      <div className="mb-4 flex items-center gap-2">
        <span className="flex size-7 items-center justify-center rounded-lg bg-primary/10 text-primary">{icon}</span>
        <h3 className="text-sm font-semibold">{title}</h3>
        {aside ? <div className="ml-auto flex items-center gap-2">{aside}</div> : null}
      </div>
      {children}
    </section>
  );
}

/** 整块可点的开关：选中时带强调色描边。 */
export function ToggleTile({
  label,
  hint,
  checked,
  onChange,
  disabled
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-center justify-between gap-3 rounded-xl border px-4 py-3 transition-colors",
        checked ? "border-primary/40 bg-primary/5" : "hover:bg-muted/40",
        disabled && "cursor-not-allowed opacity-60"
      )}
    >
      <span className="min-w-0">
        <span className="block text-sm">{label}</span>
        {hint ? <span className="block text-[11px] text-muted-foreground">{hint}</span> : null}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} className="shrink-0" />
    </label>
  );
}
