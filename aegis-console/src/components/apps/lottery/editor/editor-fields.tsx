"use client";

import { useId, useLayoutEffect, useRef, useState } from "react";
import { CalendarClock, Check, Minus, Plus, X } from "lucide-react";
import { motion } from "motion/react";
import { Calendar } from "@/components/ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import { localDateKey } from "@/lib/datetime";
import { cn } from "@/lib/utils";

/**
 * 活动编辑器的字段原语。
 *
 * 编辑器整体走「浅底填充 + 聚焦描边」的输入风格，和控制台其它配置页的 Input 不同：
 * 抽屉里字段密，描边一多就成了一张表格。这里所有原语共用 `FIELD_SURFACE`，
 * 改风格只改这一处。数值一律按字符串受控（同 NumberField），输入过程中能清空。
 */

export const FIELD_SURFACE =
  "rounded-xl bg-muted/70 ring-1 ring-inset ring-transparent transition-[background-color,box-shadow] duration-150 hover:bg-muted focus-within:bg-background focus-within:ring-ring/40 dark:bg-input/30";

/** 字段：标签 + 右侧补充 + 下方说明 */
export function Field({
  label,
  aside,
  hint,
  htmlFor,
  className,
  children
}: {
  label: React.ReactNode;
  aside?: React.ReactNode;
  hint?: React.ReactNode;
  htmlFor?: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={cn("min-w-0 space-y-1.5", className)}>
      <div className="flex min-h-4 items-baseline justify-between gap-2">
        <label htmlFor={htmlFor} className="text-xs font-medium text-foreground/80">
          {label}
        </label>
        {aside ? <span className="truncate text-[11px] text-muted-foreground">{aside}</span> : null}
      </div>
      {children}
      {hint ? <p className="text-[11px] leading-snug text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

/** 单行文本，右侧显示字数 */
export function TextField({
  value,
  onChange,
  maxLength,
  placeholder,
  id,
  size = "md",
  invalid
}: {
  value: string;
  onChange: (value: string) => void;
  maxLength?: number;
  placeholder?: string;
  id?: string;
  size?: "md" | "lg";
  invalid?: boolean;
}) {
  const length = Array.from(value).length;
  return (
    <div className={cn(FIELD_SURFACE, "flex items-center gap-2 px-3", invalid && "ring-destructive/50")}>
      <input
        id={id}
        value={value}
        maxLength={maxLength}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className={cn(
          "min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground/70",
          size === "lg" ? "h-11 text-base font-medium" : "h-9 text-sm"
        )}
        suppressHydrationWarning
      />
      {maxLength ? <CharCount length={length} max={maxLength} /> : null}
    </div>
  );
}

export function CharCount({ length, max }: { length: number; max: number }) {
  return (
    <span
      className={cn(
        "shrink-0 font-data text-[10px] tabular-nums text-muted-foreground/70",
        length >= max && "text-amber-600 dark:text-amber-400"
      )}
    >
      {length}/{max}
    </span>
  );
}

const clamp = (value: number, min?: number, max?: number) =>
  Math.min(max ?? Number.POSITIVE_INFINITY, Math.max(min ?? Number.NEGATIVE_INFINITY, value));

/**
 * 数值步进器：中间可直接输入，两侧 ± 按 step 增减并夹在 [min, max] 内。
 * 直接输入不夹值（输入「1」再补「00」的中途不能被夹回下限），越界由表单校验提示。
 */
export function Stepper({
  value,
  onChange,
  min,
  max,
  step = 1,
  suffix,
  disabled,
  placeholder,
  id,
  className
}: {
  value: string;
  onChange: (value: string) => void;
  min?: number;
  max?: number;
  step?: number;
  suffix?: React.ReactNode;
  disabled?: boolean;
  placeholder?: string;
  id?: string;
  className?: string;
}) {
  const current = Number(value);
  const numeric = Number.isFinite(current) && value.trim() !== "" ? current : (min ?? 0);
  const bump = (direction: 1 | -1) => {
    const next = clamp(Math.round((numeric + direction * step) * 1000) / 1000, min, max);
    onChange(String(next));
  };
  const button =
    "grid size-7 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-background hover:text-foreground disabled:pointer-events-none disabled:opacity-30 dark:hover:bg-input/60";
  return (
    <div
      className={cn(
        FIELD_SURFACE,
        "flex h-9 items-center gap-0.5 px-1",
        disabled && "pointer-events-none opacity-45",
        className
      )}
    >
      <button
        type="button"
        tabIndex={-1}
        className={button}
        disabled={disabled || (min !== undefined && numeric <= min)}
        onClick={() => bump(-1)}
        aria-label="减少"
      >
        <Minus className="size-3.5" />
      </button>
      <input
        id={id}
        type="number"
        inputMode="decimal"
        value={value}
        min={min}
        max={max}
        step={step}
        disabled={disabled}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="h-full min-w-0 flex-1 bg-transparent text-center font-data text-sm tabular-nums outline-none [appearance:textfield] placeholder:text-muted-foreground/60 [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
        suppressHydrationWarning
      />
      {suffix ? <span className="shrink-0 pr-1 text-[11px] text-muted-foreground">{suffix}</span> : null}
      <button
        type="button"
        tabIndex={-1}
        className={button}
        disabled={disabled || (max !== undefined && numeric >= max)}
        onClick={() => bump(1)}
        aria-label="增加"
      >
        <Plus className="size-3.5" />
      </button>
    </div>
  );
}

/** 分段选择：选中块用 motion 的共享布局在选项间滑动 */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  size = "md",
  className
}: {
  value: T;
  options: Array<{ value: T; label: React.ReactNode; icon?: React.ReactNode }>;
  onChange: (value: T) => void;
  size?: "sm" | "md";
  className?: string;
}) {
  const layoutId = useId();
  return (
    <div
      role="radiogroup"
      className={cn("inline-flex items-center gap-0.5 rounded-xl bg-muted p-0.5 dark:bg-input/30", className)}
    >
      {options.map((option) => {
        const active = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(option.value)}
            className={cn(
              "relative flex flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-[10px] font-medium transition-colors",
              size === "sm" ? "h-7 px-2.5 text-xs" : "h-8 px-3.5 text-xs",
              active ? "text-foreground" : "text-muted-foreground hover:text-foreground"
            )}
          >
            {active ? (
              <motion.span
                layoutId={layoutId}
                className="absolute inset-0 rounded-[10px] bg-background shadow-sm ring-1 ring-border/60 dark:bg-card"
                transition={{ type: "spring", stiffness: 520, damping: 38 }}
              />
            ) : null}
            <span className="relative flex items-center gap-1.5 [&>svg]:size-3.5">
              {option.icon}
              {option.label}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/** 单选卡：图标块 + 标题 + 说明，选中时右上角打勾 */
export function ChoiceCard({
  active,
  onSelect,
  icon,
  title,
  description,
  tone = "bg-muted text-muted-foreground",
  activeTone = "bg-primary text-primary-foreground",
  disabled,
  className,
  children
}: {
  active: boolean;
  onSelect: () => void;
  icon?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  tone?: string;
  activeTone?: string;
  disabled?: boolean;
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      disabled={disabled}
      onClick={onSelect}
      className={cn(
        "group relative flex min-w-0 items-start gap-3 rounded-2xl border p-3 text-left transition-[border-color,background-color,box-shadow] duration-150",
        active
          ? "border-foreground/80 bg-background shadow-[0_0_0_1px_var(--foreground)] dark:border-foreground/70 dark:bg-card"
          : "border-border bg-card hover:border-foreground/25 hover:bg-muted/40",
        disabled && "pointer-events-none opacity-45",
        className
      )}
    >
      {icon ? (
        <span
          className={cn(
            "grid size-8 shrink-0 place-items-center rounded-lg transition-colors [&>svg]:size-4",
            active ? activeTone : tone
          )}
        >
          {icon}
        </span>
      ) : null}
      <span className="min-w-0 flex-1 space-y-0.5 pr-4">
        <span className="block truncate text-[13px] font-semibold">{title}</span>
        {description ? (
          <span className="block text-[11px] leading-snug text-muted-foreground">{description}</span>
        ) : null}
        {children}
      </span>
      <span
        className={cn(
          "absolute right-2.5 top-2.5 grid size-4 place-items-center rounded-full transition-all duration-150",
          active ? "scale-100 bg-foreground text-background" : "scale-75 opacity-0"
        )}
      >
        <Check className="size-2.5" strokeWidth={3} />
      </span>
    </button>
  );
}

/** 开关块：整块可点，开启时图标块上色 */
export function ToggleTile({
  checked,
  onChange,
  icon,
  label,
  hint,
  disabled,
  className
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  icon?: React.ReactNode;
  label: React.ReactNode;
  hint?: React.ReactNode;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-center gap-3 rounded-xl border border-border bg-card px-3 py-2.5 transition-colors hover:bg-muted/40",
        checked && "border-emerald-500/40 bg-emerald-500/[0.04] hover:bg-emerald-500/[0.07]",
        disabled && "pointer-events-none opacity-45",
        className
      )}
    >
      {icon ? (
        <span
          className={cn(
            "grid size-7 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground transition-colors [&>svg]:size-3.5",
            checked && "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
          )}
        >
          {icon}
        </span>
      ) : null}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-xs font-medium">{label}</span>
        {hint ? <span className="block text-[11px] leading-snug text-muted-foreground">{hint}</span> : null}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} />
    </label>
  );
}

const pad = (value: number) => String(value).padStart(2, "0");
const HOURS = Array.from({ length: 24 }, (_, hour) => hour);
const MINUTES = Array.from({ length: 12 }, (_, index) => index * 5);

/** `YYYY-MM-DDTHH:mm`（浏览器时区墙上时间）↔ Date */
function parseLocal(value: string) {
  const matched = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value);
  if (!matched) return null;
  const [, y, m, d, hh, mm] = matched.map(Number);
  return new Date(y, m - 1, d, hh, mm);
}

function formatLocal(date: Date) {
  return `${localDateKey(date)}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/**
 * 日期时间选择：日历 + 时、分两列。值是 datetime-local 的格式，和表单里原来的字段同形，
 * 载荷换算仍走 `new Date(value)`。空值显示 `emptyLabel`，可清除。
 */
export function DateTimePicker({
  value,
  onChange,
  emptyLabel = "不限",
  defaultTime = "00:00",
  invalid
}: {
  value: string;
  onChange: (value: string) => void;
  emptyLabel?: string;
  /** 首次选日期时补的时刻 */
  defaultTime?: string;
  invalid?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const date = parseLocal(value);

  const pickDay = (day: Date | undefined) => {
    if (!day) return;
    const [hh, mm] = defaultTime.split(":").map(Number);
    const next = new Date(day.getFullYear(), day.getMonth(), day.getDate(), date?.getHours() ?? hh, date?.getMinutes() ?? mm);
    onChange(formatLocal(next));
  };
  const pickTime = (hours: number | null, minutes: number | null) => {
    const base = date ?? new Date(new Date().setHours(0, 0, 0, 0));
    const next = new Date(base);
    if (hours !== null) next.setHours(hours);
    if (minutes !== null) next.setMinutes(minutes);
    onChange(formatLocal(next));
  };
  const minutes = date && date.getMinutes() % 5 !== 0 ? [...MINUTES, date.getMinutes()].sort((a, b) => a - b) : MINUTES;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            FIELD_SURFACE,
            "group flex h-9 w-full items-center gap-2 px-3 text-left text-sm",
            open && "bg-background ring-ring/40",
            invalid && "ring-destructive/50"
          )}
        >
          <CalendarClock className="size-3.5 shrink-0 text-muted-foreground" />
          {date ? (
            <span className="min-w-0 flex-1 truncate font-data tabular-nums">
              {date.getFullYear()}-{pad(date.getMonth() + 1)}-{pad(date.getDate())}
              <span className="ml-2 text-muted-foreground">
                {pad(date.getHours())}:{pad(date.getMinutes())}
              </span>
            </span>
          ) : (
            <span className="min-w-0 flex-1 truncate text-muted-foreground">{emptyLabel}</span>
          )}
          {date ? (
            <span
              role="button"
              tabIndex={-1}
              aria-label="清除"
              onClick={(event) => {
                event.stopPropagation();
                onChange("");
              }}
              className="grid size-5 place-items-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground group-hover:opacity-100"
            >
              <X className="size-3" />
            </span>
          ) : null}
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-auto overflow-hidden rounded-2xl p-0">
        <div className="flex">
          <Calendar mode="single" selected={date ?? undefined} onSelect={pickDay} defaultMonth={date ?? undefined} />
          <div className="flex w-28 border-l">
            <TimeColumn
              label="时"
              values={HOURS}
              selected={date?.getHours()}
              onPick={(hour) => pickTime(hour, null)}
            />
            <TimeColumn
              label="分"
              values={minutes}
              selected={date?.getMinutes()}
              onPick={(minute) => pickTime(null, minute)}
            />
          </div>
        </div>
        <div className="flex items-center justify-between gap-2 border-t px-3 py-2">
          <button
            type="button"
            className="text-xs text-muted-foreground transition-colors hover:text-foreground"
            onClick={() => {
              onChange("");
              setOpen(false);
            }}
          >
            清除
          </button>
          <div className="flex gap-1.5">
            <button
              type="button"
              className="h-7 rounded-lg px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              onClick={() => onChange(formatLocal(new Date()))}
            >
              现在
            </button>
            <button
              type="button"
              className="h-7 rounded-lg bg-foreground px-3 text-xs font-medium text-background transition-opacity hover:opacity-90"
              onClick={() => setOpen(false)}
            >
              完成
            </button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function TimeColumn({
  label,
  values,
  selected,
  onPick
}: {
  label: string;
  values: number[];
  selected?: number;
  onPick: (value: number) => void;
}) {
  const listRef = useRef<HTMLDivElement>(null);
  // 打开时把已选的值滚到列中间；只在挂载时做，点选时不跟着跳
  useLayoutEffect(() => {
    const list = listRef.current;
    const item = list?.querySelector<HTMLElement>("[data-selected]");
    if (list && item) list.scrollTop = item.offsetTop - list.clientHeight / 2 + item.clientHeight / 2;
  }, []);
  return (
    <div className="flex min-w-0 flex-1 flex-col">
      <span className="py-2 text-center text-[11px] text-muted-foreground">{label}</span>
      <div ref={listRef} className="relative h-[15.5rem] space-y-0.5 overflow-y-auto px-1 pb-2 [scrollbar-width:thin]">
        {values.map((value) => (
          <button
            key={value}
            type="button"
            data-selected={value === selected ? "" : undefined}
            onClick={() => onPick(value)}
            className={cn(
              "flex h-7 w-full items-center justify-center rounded-md font-data text-xs tabular-nums transition-colors",
              value === selected ? "bg-primary text-primary-foreground" : "hover:bg-muted"
            )}
          >
            {pad(value)}
          </button>
        ))}
      </div>
    </div>
  );
}
