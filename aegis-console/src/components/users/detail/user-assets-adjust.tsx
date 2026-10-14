"use client";

/**
 * 资产页的调整部件：积分、经验、余额三张调整卡共用一套交互，以及页内的分段选择与快捷标签。
 *
 * 三条账的结算语义不同（积分 / 经验是整数，余额是 decimal 字符串、带幂等），
 * 所以卡片只统一**交互**——方向、数量、预览、原因、提交——不统一请求：
 * 每张卡的 `onSubmit` 仍各走各的接口。
 *
 * 方向（增加 / 扣减）单独选，而不是让人在数量前面打负号：
 * 负号在输入框里很容易漏看，扣成了加、加成了扣都要再做一笔冲正。
 * 提交按钮把方向与数量写出来（「扣减 50 积分」），点下去之前最后确认一眼。
 *
 * 数值一律按字符串放大成 BigInt 比较，不经过 Number：超过 2^53 的整数转成浮点数会被舍入，
 * 「调整后」预览与上限判断都会算错。卡片在本地先拦下单次超限与调整后越界，
 * 后端仍会按同样的规则再校验一遍。
 *
 * 每次提交附带一个请求编号：同一笔（方向、数量、原因都没变）重试时复用，
 * 后端据此只生效一次 —— 网络超时后再点一次不会扣两遍。成功或改动内容后换新编号。
 */

import * as React from "react";
import { ArrowRight, Loader2, Minus, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

// ── 十进制运算 ──────────────────────────
//
// 余额是 shopspring/decimal 字符串，转 Number 会丢精度。预览「调整后余额」时
// 按 4 位小数放大成 BigInt 做整数运算，结果再交给 formatMoney 展示。

const SCALE = 4;
const ZERO = BigInt(0);

function toScaled(value?: string | null): bigint | null {
  const match = (value ?? "").trim().match(/^([+-])?(\d+)(?:\.(\d+))?$/);
  if (!match) return null;
  const fraction = (match[3] ?? "").padEnd(SCALE, "0").slice(0, SCALE);
  const scaled = BigInt(match[2] + fraction);
  return match[1] === "-" ? -scaled : scaled;
}

function fromScaled(value: bigint) {
  const negative = value < ZERO;
  const digits = (negative ? -value : value).toString().padStart(SCALE + 1, "0");
  return `${negative ? "-" : ""}${digits.slice(0, -SCALE)}.${digits.slice(-SCALE)}`;
}

/** 比较两个十进制字符串；任一无法解析时返回 null。 */
export function compareDecimal(a?: string | null, b?: string | null) {
  const left = toScaled(a);
  const right = toScaled(b);
  if (left === null || right === null) return null;
  return left === right ? 0 : left > right ? 1 : -1;
}

/** 千分位显示整数字符串（BigInt，不丢精度）。 */
export function formatIntegerString(value: string) {
  try {
    return BigInt(value).toLocaleString("zh-CN");
  } catch {
    return value;
  }
}

/** 生成请求编号；没有 randomUUID 的环境（非安全上下文）退回随机数拼接。 */
export function newRequestId() {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID();
  return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

/** 两个十进制字符串相加；任一无法解析时返回 null。 */
export function addDecimal(a?: string | null, b?: string | null) {
  const left = toScaled(a);
  const right = toScaled(b);
  if (left === null || right === null) return null;
  return fromScaled(left + right);
}

export function isNegativeDecimal(value?: string | null) {
  const scaled = toScaled(value);
  return scaled !== null && scaled < ZERO;
}

export function isPositiveDecimal(value?: string | null) {
  const scaled = toScaled(value);
  return scaled !== null && scaled > ZERO;
}

// ── 分段选择 ──────────────────────────

export type SegmentOption<T extends string> = {
  value: T;
  label: React.ReactNode;
  icon?: React.ComponentType<{ className?: string }>;
  count?: number;
  disabled?: boolean;
};

/** 小号分段选择，用在面板标题栏与表单里；样式与「处置」页的记录筛选一致。 */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  stretch = false,
  className,
  ariaLabel
}: {
  value: T;
  onChange: (value: T) => void;
  options: Array<SegmentOption<T>>;
  /** 铺满父容器、各项等宽 */
  stretch?: boolean;
  className?: string;
  ariaLabel?: string;
}) {
  return (
    <ToggleGroup
      type="single"
      value={value}
      aria-label={ariaLabel}
      onValueChange={(next) => {
        if (next) onChange(next as T);
      }}
      className={cn(
        "rounded-lg border bg-muted/40 p-0.5",
        stretch && "grid w-full",
        className
      )}
      style={stretch ? { gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))` } : undefined}
    >
      {options.map((option) => {
        const Icon = option.icon;
        return (
          <ToggleGroupItem
            key={option.value}
            value={option.value}
            disabled={option.disabled}
            className="h-7 gap-1 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:font-medium data-[state=on]:shadow-sm"
          >
            {Icon ? <Icon className="size-3.5" /> : null}
            {option.label}
            {typeof option.count === "number" ? (
              <span className="tabular-nums text-muted-foreground">{option.count}</span>
            ) : null}
          </ToggleGroupItem>
        );
      })}
    </ToggleGroup>
  );
}

/** 一排可点的快捷值（数量、天数、原因）。再点一次已选中的项不会取消，只有输入框能清空。 */
export function QuickChips({
  items,
  value,
  onPick,
  render,
  className
}: {
  items: string[];
  value?: string;
  onPick: (value: string) => void;
  render?: (value: string) => React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-wrap gap-1.5", className)}>
      {items.map((item) => {
        const active = value === item;
        return (
          <button
            key={item}
            type="button"
            aria-pressed={active}
            onClick={() => onPick(item)}
            className={cn(
              "h-6 rounded-full border px-2.5 text-[11px] tabular-nums transition-colors",
              active
                ? "border-foreground/20 bg-foreground text-background"
                : "text-muted-foreground hover:bg-accent hover:text-foreground"
            )}
          >
            {render ? render(item) : item}
          </button>
        );
      })}
    </div>
  );
}

/** 带建议项的原因输入框：建议项走原生 datalist，不额外占一排。 */
export function ReasonInput({
  value,
  onChange,
  suggestions,
  placeholder = "原因（选填）",
  className,
  onEnter
}: {
  value: string;
  onChange: (value: string) => void;
  suggestions: string[];
  placeholder?: string;
  className?: string;
  onEnter?: () => void;
}) {
  const listId = React.useId();
  return (
    <>
      <Input
        value={value}
        maxLength={200}
        list={listId}
        placeholder={placeholder}
        className={cn("h-8 text-xs", className)}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && onEnter) {
            event.preventDefault();
            onEnter();
          }
        }}
      />
      <datalist id={listId}>
        {suggestions.map((item) => (
          <option key={item} value={item} />
        ))}
      </datalist>
    </>
  );
}

// ── 调整卡 ──────────────────────────

type Direction = "add" | "deduct";

const DIRECTIONS: Array<SegmentOption<Direction>> = [
  { value: "add", label: "增加", icon: Plus },
  { value: "deduct", label: "扣减", icon: Minus }
];

const INTEGER_RE = /^\d+$/;
const DECIMAL_RE = /^\d+(\.\d{1,2})?$/;

export function AdjustCard({
  title,
  icon,
  unit,
  decimal = false,
  current,
  format,
  hint,
  loading = false,
  unavailable,
  quick,
  reasons,
  shortageText,
  addOnly = false,
  maxAmount,
  maxBalance,
  pending,
  onSubmit
}: {
  title: string;
  icon: React.ReactNode;
  /** 按钮与输入框里的单位：「积分」「经验」「元」 */
  unit: string;
  /** 余额允许两位小数，积分与经验只收整数 */
  decimal?: boolean;
  /** 当前值的十进制字符串；拿不到时不出预览，也不拦扣减 */
  current?: string;
  format: (value: string) => string;
  hint?: React.ReactNode;
  loading?: boolean;
  /** 数据不可用时的说明；给出后整张卡只读 */
  unavailable?: string;
  quick: string[];
  reasons: string[];
  /** 扣减超过当前值时的提示（后端同样拒绝），如「积分不足」 */
  shortageText: string;
  /** 只允许增加（经验只增不减），不显示方向选择 */
  addOnly?: boolean;
  /** 单次调整数量上限（十进制字符串） */
  maxAmount?: string;
  /** 调整后余额上限（十进制字符串） */
  maxBalance?: string;
  pending: boolean;
  /** 返回 true 表示成功，卡片随即清空数量与原因；requestId 同一笔重试时不变 */
  onSubmit: (signedAmount: string, reason: string, requestId: string) => Promise<boolean>;
}) {
  const [pickedDirection, setDirection] = React.useState<Direction>("add");
  const [amount, setAmount] = React.useState("");
  const [reason, setReason] = React.useState("");
  const direction: Direction = addOnly ? "add" : pickedDirection;

  const trimmed = amount.trim();
  const amountValid = (decimal ? DECIMAL_RE : INTEGER_RE).test(trimmed) && isPositiveDecimal(trimmed);
  const signed = direction === "deduct" ? `-${trimmed}` : trimmed;
  const after = amountValid && current !== undefined ? addDecimal(current, signed) : null;
  const shortage = after !== null && isNegativeDecimal(after);
  const overAmount = amountValid && maxAmount !== undefined && (compareDecimal(trimmed, maxAmount) ?? 0) > 0;
  const overBalance = !shortage && after !== null && maxBalance !== undefined && (compareDecimal(after, maxBalance) ?? 0) > 0;
  const disabled = Boolean(unavailable) || loading || pending || !amountValid || shortage || overAmount || overBalance;

  const amountLabel = decimal ? `¥${trimmed}` : `${formatIntegerString(trimmed)} ${unit}`;

  // 请求编号跟着「这一笔」走：内容不变就复用，改了任何一项就换新的
  const requestRef = React.useRef<{ key: string; id: string } | null>(null);
  function requestIdFor(key: string) {
    if (requestRef.current?.key !== key) requestRef.current = { key, id: newRequestId() };
    return requestRef.current.id;
  }

  async function submit() {
    if (disabled) return;
    const finalReason = reason.trim();
    const ok = await onSubmit(signed, finalReason, requestIdFor(`${signed}|${finalReason}`));
    if (ok) {
      requestRef.current = null;
      setAmount("");
      setReason("");
    }
  }

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="flex items-center justify-between gap-3 px-4 pt-4 sm:px-5">
        <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
          <span className="text-muted-foreground">{icon}</span>
          {title}
        </div>
      </div>

      <div className="px-4 pb-4 pt-2 sm:px-5">
        {loading ? (
          <Skeleton className="h-8 w-32 rounded-lg" />
        ) : (
          <div className="truncate text-2xl font-semibold tabular-nums text-foreground">
            {current !== undefined ? format(current) : "—"}
          </div>
        )}
        <div className="mt-1 h-4 truncate text-[11px] text-muted-foreground">
          {unavailable ?? hint}
        </div>
      </div>

      <div className="mt-auto space-y-2.5 border-t bg-muted/20 px-4 py-4 sm:px-5">
        <div className="flex gap-2">
          {addOnly ? null : (
            <Segmented
              value={direction}
              onChange={setDirection}
              options={DIRECTIONS}
              ariaLabel={`${title}调整方向`}
              className="shrink-0"
            />
          )}
          <div className="relative min-w-0 flex-1">
            <Input
              value={amount}
              inputMode={decimal ? "decimal" : "numeric"}
              placeholder={decimal ? "0.00" : "0"}
              aria-label={`${title}调整数量`}
              disabled={Boolean(unavailable)}
              maxLength={decimal ? 16 : 12}
              className="h-8 pr-10 text-sm tabular-nums"
              onChange={(event) => setAmount(event.target.value.replace(/[^\d.]/g, ""))}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  void submit();
                }
              }}
            />
            <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-muted-foreground">
              {decimal ? "元" : unit}
            </span>
          </div>
        </div>

        <QuickChips
          items={quick}
          value={trimmed}
          onPick={setAmount}
          render={(item) => (decimal ? `¥${item}` : formatIntegerString(item))}
        />

        <div className="flex h-5 items-center gap-1.5 text-xs text-muted-foreground">
          {overAmount ? (
            <span className="text-destructive">单次最多调整 {formatIntegerString(maxAmount ?? "")} {unit}</span>
          ) : shortage ? (
            <span className="text-destructive">
              {shortageText}，最多扣减 {current !== undefined ? format(current) : ""}
            </span>
          ) : overBalance ? (
            <span className="text-destructive">调整后将超过上限 {formatIntegerString(maxBalance ?? "")}</span>
          ) : after !== null && current !== undefined ? (
            <>
              <span className="tabular-nums">{format(current)}</span>
              <ArrowRight className="size-3" />
              <span
                className={cn(
                  "font-medium tabular-nums",
                  direction === "add"
                    ? "text-emerald-600 dark:text-emerald-400"
                    : "text-amber-600 dark:text-amber-400"
                )}
              >
                {format(after)}
              </span>
            </>
          ) : (
            <span>调整后 —</span>
          )}
        </div>

        <ReasonInput value={reason} onChange={setReason} suggestions={reasons} onEnter={() => void submit()} />

        <Button size="sm" className="w-full" disabled={disabled} onClick={() => void submit()}>
          {pending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : direction === "add" ? (
            <Plus className="size-3.5" />
          ) : (
            <Minus className="size-3.5" />
          )}
          {amountValid ? `${direction === "add" ? "增加" : "扣减"} ${amountLabel}` : direction === "add" ? "增加" : "扣减"}
        </Button>
      </div>
    </Card>
  );
}
