"use client";

/**
 * 记录筛选条：关键词（防抖）、单选筛选、时间区间（预设 + 自定义），以及清空。
 *
 * 时间一律按浏览器时区：预设「今天」是本地今天 00:00 起；自定义用 datetime-local，
 * 再换算成 ISO 瞬时值交给接口（lib/datetime）。区间左闭右开。
 */

import * as React from "react";
import { CalendarRange, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { fromLocalInputValue, startOfLocalDay, toLocalInputValue } from "@/lib/datetime";
import { cn } from "@/lib/utils";

export function useDebouncedValue<T>(value: T, delay = 300) {
  const [debounced, setDebounced] = React.useState(value);
  React.useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

export function FilterBar({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn("flex flex-wrap items-center gap-2", className)}>{children}</div>;
}

/** 关键词输入。值立即更新，调用方用 useDebouncedValue 再去请求。 */
export function SearchField({
  value,
  onChange,
  placeholder = "搜索",
  className
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <div className={cn("relative w-full sm:w-60", className)}>
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="h-8 pr-8 pl-8 text-sm"
      />
      {value ? (
        <button
          type="button"
          aria-label="清空"
          onClick={() => onChange("")}
          className="absolute top-1/2 right-1.5 inline-flex size-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      ) : null}
    </div>
  );
}

export type FilterOption = { value: string; label: React.ReactNode };

const ALL = "__all__";

/** 单选筛选；空串表示不限。 */
export function FilterSelect({
  value,
  onChange,
  options,
  allLabel = "全部",
  label,
  className
}: {
  value: string;
  onChange: (value: string) => void;
  options: FilterOption[];
  allLabel?: string;
  /** 触发器左侧的灰色前缀，如「方向」 */
  label?: string;
  className?: string;
}) {
  return (
    <Select value={value || ALL} onValueChange={(next) => onChange(next === ALL ? "" : next)}>
      <SelectTrigger size="sm" className={cn("min-w-[112px] text-sm", className)}>
        {label ? <span className="text-xs text-muted-foreground">{label}</span> : null}
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{allLabel}</SelectItem>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export type TimeRange = { preset: string; from?: string; to?: string };

const PRESETS: Array<{ value: string; label: string; days?: number }> = [
  { value: "all", label: "全部时间" },
  { value: "today", label: "今天", days: 1 },
  { value: "7d", label: "近 7 天", days: 7 },
  { value: "30d", label: "近 30 天", days: 30 },
  { value: "90d", label: "近 90 天", days: 90 },
  { value: "365d", label: "近一年", days: 365 }
];

/** 把区间换成接口参数（ISO 瞬时值）。预设按本地零点切日。 */
export function rangeToParams(range: TimeRange): { from?: string; to?: string } {
  if (range.preset === "custom") return { from: range.from, to: range.to };
  const preset = PRESETS.find((item) => item.value === range.preset);
  if (!preset?.days) return {};
  const start = startOfLocalDay();
  start.setDate(start.getDate() - (preset.days - 1));
  return { from: start.toISOString() };
}

export function rangeLabel(range: TimeRange) {
  if (range.preset !== "custom") return PRESETS.find((item) => item.value === range.preset)?.label ?? "全部时间";
  const fmt = (iso?: string) => (iso ? toLocalInputValue(iso).replace("T", " ") : "…");
  return `${fmt(range.from)} 至 ${fmt(range.to)}`;
}

export function TimeRangePicker({ value, onChange }: { value: TimeRange; onChange: (value: TimeRange) => void }) {
  const [open, setOpen] = React.useState(false);
  const [from, setFrom] = React.useState(value.preset === "custom" && value.from ? toLocalInputValue(value.from) : "");
  const [to, setTo] = React.useState(value.preset === "custom" && value.to ? toLocalInputValue(value.to) : "");
  const invalid = Boolean(from && to && from >= to);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className={cn("max-w-full font-normal", value.preset !== "all" && "border-primary/40 text-primary")}>
          <CalendarRange />
          <span className="truncate">{rangeLabel(value)}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[300px] p-3">
        <div className="grid grid-cols-3 gap-1.5">
          {PRESETS.map((preset) => (
            <Button
              key={preset.value}
              size="xs"
              variant={value.preset === preset.value ? "default" : "outline"}
              onClick={() => {
                onChange({ preset: preset.value });
                setOpen(false);
              }}
            >
              {preset.label}
            </Button>
          ))}
        </div>
        <div className="mt-3 flex flex-col gap-2 border-t pt-3">
          <div className="text-xs font-medium">自定义区间</div>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            开始
            <Input type="datetime-local" value={from} onChange={(event) => setFrom(event.target.value)} className="h-8 text-sm" />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            结束
            <Input type="datetime-local" value={to} onChange={(event) => setTo(event.target.value)} className="h-8 text-sm" />
          </label>
          {invalid ? <p className="text-xs text-red-600 dark:text-red-400">开始时间必须早于结束时间</p> : null}
          <Button
            size="sm"
            disabled={invalid || (!from && !to)}
            onClick={() => {
              onChange({
                preset: "custom",
                from: fromLocalInputValue(from) ?? undefined,
                to: fromLocalInputValue(to) ?? undefined
              });
              setOpen(false);
            }}
          >
            应用
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function ResetButton({ visible, onClick }: { visible: boolean; onClick: () => void }) {
  if (!visible) return null;
  return (
    <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={onClick}>
      <X />
      清空筛选
    </Button>
  );
}
