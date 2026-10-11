"use client";

import type { ReactNode } from "react";
import { ChevronLeft, ChevronRight, RefreshCw, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 订单与退款两张列表的共用部件：状态分段、搜索框、刷新、分页、空态。
 * 两张表并排出现在同一个页签里，工具栏长得不一样会让人以为筛选语义也不一样。
 */

export function StatusSegments({
  options,
  value,
  onChange,
  label
}: {
  options: Array<{ value: string; label: string }>;
  value: string;
  onChange: (value: string) => void;
  label: string;
}) {
  return (
    <div className="-mx-1 overflow-x-auto px-1 [scrollbar-width:none]">
      <div className="inline-flex min-w-max rounded-lg border bg-muted/40 p-0.5" role="tablist" aria-label={label}>
        {options.map((option) => (
          <button
            key={option.value || "all"}
            type="button"
            role="tab"
            aria-selected={value === option.value}
            onClick={() => onChange(option.value)}
            className={cn(
              "rounded-md px-3 py-1.5 text-xs font-medium whitespace-nowrap text-muted-foreground transition-colors hover:text-foreground",
              value === option.value && "bg-background text-foreground shadow-sm"
            )}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  );
}

export function SearchBox({
  value,
  onChange,
  placeholder,
  className
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  className?: string;
}) {
  return (
    <div className={cn("relative min-w-0", className)}>
      <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input className="h-9 pr-8 pl-9" placeholder={placeholder} value={value} onChange={(e) => onChange(e.target.value)} />
      {value ? (
        <button
          type="button"
          aria-label="清空搜索"
          className="absolute top-1/2 right-2.5 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          onClick={() => onChange("")}
        >
          <X className="size-4" />
        </button>
      ) : null}
    </div>
  );
}

export function RefreshButton({ fetching, onClick }: { fetching?: boolean; onClick: () => void }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon" variant="ghost" className="size-9 shrink-0" aria-label="刷新" onClick={onClick}>
          <RefreshCw className={cn("size-4", fetching && "animate-spin")} />
        </Button>
      </TooltipTrigger>
      <TooltipContent>刷新</TooltipContent>
    </Tooltip>
  );
}

export function ListPagination({
  page,
  totalPages,
  total,
  unit,
  onPage
}: {
  page: number;
  totalPages: number;
  total: number;
  unit: string;
  onPage: (page: number) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 border-t bg-muted/20 px-4 py-2.5">
      <span className="text-xs text-muted-foreground tabular-nums">
        共 {total} {unit}
      </span>
      <div className="flex items-center gap-1">
        <Button size="icon" variant="ghost" className="size-8" aria-label="上一页" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          <ChevronLeft className="size-4" />
        </Button>
        {/* 窄屏只给「当前 / 总数」，页码按钮留给宽屏 */}
        <span className="px-1 text-xs text-muted-foreground tabular-nums sm:hidden">
          {page} / {totalPages}
        </span>
        <div className="hidden items-center gap-1 sm:flex">
          {pageWindow(page, totalPages).map((p, i) =>
            p === "…" ? (
              <span key={`gap-${i}`} className="px-1 text-xs text-muted-foreground">
                …
              </span>
            ) : (
              <Button
                key={p}
                size="sm"
                variant={p === page ? "secondary" : "ghost"}
                className="h-8 min-w-8 px-2 tabular-nums"
                onClick={() => onPage(p)}
              >
                {p}
              </Button>
            )
          )}
        </div>
        <Button size="icon" variant="ghost" className="size-8" aria-label="下一页" disabled={page >= totalPages} onClick={() => onPage(page + 1)}>
          <ChevronRight className="size-4" />
        </Button>
      </div>
    </div>
  );
}

export function ListState({
  icon,
  title,
  hint,
  action
}: {
  icon: ReactNode;
  title: string;
  hint?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-16 text-center">
      <span className="flex size-11 items-center justify-center rounded-full bg-muted text-muted-foreground">{icon}</span>
      <p className="text-sm font-medium">{title}</p>
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  );
}

/** 分页窗口：首尾页 + 当前页前后各一页，中间用省略号。 */
function pageWindow(current: number, total: number): Array<number | "…"> {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const pages = new Set([1, total, current - 1, current, current + 1]);
  const sorted = [...pages].filter((p) => p >= 1 && p <= total).sort((a, b) => a - b);
  const out: Array<number | "…"> = [];
  sorted.forEach((p, i) => {
    if (i > 0 && p - (sorted[i - 1] as number) > 1) out.push("…");
    out.push(p);
  });
  return out;
}
