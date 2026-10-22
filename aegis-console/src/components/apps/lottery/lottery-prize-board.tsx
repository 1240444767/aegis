"use client";

import { ArrowDown, ArrowUp, ChevronLeft, ChevronRight, Plus, Trash2 } from "lucide-react";
import { NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  OWNABLE_KINDS,
  SWAP_KINDS,
  blankPrize,
  nextPrizeKey,
  rewardOf,
  type PrizeDraft
} from "@/components/apps/lottery/lottery-activity-form";
import {
  GRID_CELLS,
  defaultPrizeName,
  formatProbability
} from "@/components/apps/lottery/lottery-shared";
import { termLabel } from "@/components/apps/points-store/points-store-shared";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import type { LotteryCatalog, LotteryUIMode } from "@/lib/api/lottery";
import { cn } from "@/lib/utils";

/** Select 不接受空串：「按活动的处理方式」用这个占位 */
const SWAP_POLICY = "__policy";

/**
 * 奖盘编辑：左边选格子，右边改这一格。
 *
 * 九宫格是一张 3×3 的盘，外圈 8 格按顺时针从左上角起编号，中心是抽奖按钮（与 App 端同一张表，
 * 见 `GRID_CELLS`）；转盘是一列从顶部起顺时针的奖品，可增删、上下移。两种形态共用右侧的单格表单，
 * 每格都显示按当前草稿算出的公示概率，改权重或库存立刻能看到整盘的变化。
 */
export function LotteryPrizeBoard({
  uiMode,
  prizes,
  probabilities,
  selectedKey,
  pityKey,
  catalog,
  swapPreview,
  onSelect,
  onChange
}: {
  uiMode: LotteryUIMode;
  prizes: PrizeDraft[];
  probabilities: number[];
  selectedKey: number | null;
  pityKey: number | null;
  catalog?: LotteryCatalog | null;
  /** 这一格被已拥有的人抽中时改发什么（一句话），不改发时为 null */
  swapPreview?: (prize: PrizeDraft) => string | null;
  onSelect: (key: number) => void;
  onChange: (prizes: PrizeDraft[]) => void;
}) {
  const kinds = catalog?.kinds ?? [];
  const plans = catalog?.plans ?? [];
  const features = catalog?.features ?? [];
  const wheelMin = catalog?.wheelMinPrizes ?? 2;
  const wheelMax = catalog?.wheelMaxPrizes ?? 12;

  const selectedIndex = Math.max(
    0,
    prizes.findIndex((prize) => prize.key === selectedKey)
  );
  const selected = prizes[selectedIndex];

  const displayName = (prize: PrizeDraft) =>
    prize.name.trim() || defaultPrizeName(rewardOf(prize, kinds), kinds, plans, features);

  const patch = (key: number, next: Partial<PrizeDraft>) =>
    onChange(prizes.map((prize) => (prize.key === key ? { ...prize, ...next } : prize)));

  /** 兜底奖只能有一个，且必须不限库存：勾上时顺手把其它的取消、把库存放开 */
  const setFallback = (key: number, fallback: boolean) =>
    onChange(
      prizes.map((prize) =>
        prize.key === key
          ? { ...prize, fallback, unlimited: fallback ? true : prize.unlimited }
          : fallback
            ? { ...prize, fallback: false }
            : prize
      )
    );

  const move = (index: number, offset: number) => {
    const target = index + offset;
    if (target < 0 || target >= prizes.length) return;
    const next = [...prizes];
    [next[index], next[target]] = [next[target], next[index]];
    onChange(next);
  };

  const add = () => {
    const prize = blankPrize(nextPrizeKey(prizes), "points", 10, 10);
    onChange([...prizes, prize]);
    onSelect(prize.key);
  };

  const remove = (key: number) => {
    const index = prizes.findIndex((prize) => prize.key === key);
    const next = prizes.filter((prize) => prize.key !== key);
    onChange(next);
    const neighbour = next[Math.min(index, next.length - 1)];
    if (neighbour) onSelect(neighbour.key);
  };

  const changeKind = (prize: PrizeDraft, type: string) => {
    const spec = kinds.find((item) => item.type === type);
    patch(prize.key, {
      type,
      amount: String(spec?.value === "feature" ? 7 : spec?.min ?? 1),
      refId: plans[0]?.id ?? 0,
      tag: features[0]?.tag ?? "",
      text: ""
    });
  };

  const cellMarks = (prize: PrizeDraft) => (
    <span className="flex flex-wrap justify-center gap-0.5">
      {prize.key === pityKey ? (
        <Badge variant="info" size="sm" className="px-1 py-0 text-[10px]">
          保底
        </Badge>
      ) : null}
      {prize.fallback ? (
        <Badge variant="secondary" size="sm" className="px-1 py-0 text-[10px]">
          兜底
        </Badge>
      ) : null}
      {!prize.unlimited ? (
        <Badge variant="outline" size="sm" className="px-1 py-0 text-[10px] tabular-nums">
          限 {prize.quantity}
        </Badge>
      ) : null}
      {Number(prize.dailyLimit) > 0 ? (
        <Badge variant="outline" size="sm" className="px-1 py-0 text-[10px] tabular-nums">
          日 {prize.dailyLimit}
        </Badge>
      ) : null}
      {Number(prize.userLimit) > 0 || Number(prize.userDailyLimit) > 0 ? (
        <Badge variant="outline" size="sm" className="px-1 py-0 text-[10px] tabular-nums">
          每人 {Number(prize.userLimit) > 0 ? prize.userLimit : `日 ${prize.userDailyLimit}`}
        </Badge>
      ) : null}
    </span>
  );

  const board =
    uiMode === "grid" ? (
      <div className="grid aspect-square w-full max-w-[18rem] grid-cols-3 grid-rows-3 gap-1.5">
        {[0, 1, 2].flatMap((row) =>
          [0, 1, 2].map((col) => {
            if (row === 1 && col === 1) {
              return (
                <div
                  key="go"
                  className="grid place-items-center rounded-xl bg-primary text-sm font-bold tracking-widest text-primary-foreground"
                >
                  GO
                </div>
              );
            }
            const position = GRID_CELLS.findIndex(([r, c]) => r === row && c === col);
            const prize = prizes[position];
            if (!prize) {
              return (
                <div
                  key={`${row}-${col}`}
                  className="grid place-items-center rounded-xl border border-dashed border-border text-[11px] text-muted-foreground"
                >
                  空
                </div>
              );
            }
            return (
              <button
                key={prize.key}
                type="button"
                onClick={() => onSelect(prize.key)}
                className={cn(
                  "relative flex min-w-0 flex-col items-center justify-center gap-0.5 rounded-xl border p-1 text-center transition-colors",
                  prize.key === selected?.key
                    ? "border-primary bg-primary/5 ring-1 ring-primary"
                    : "border-border hover:bg-muted/60",
                  prize.type === "none" && "text-muted-foreground"
                )}
              >
                <span className="absolute left-1.5 top-1 text-[10px] tabular-nums text-muted-foreground">
                  {position + 1}
                </span>
                <span className="line-clamp-2 w-full text-[11px] font-medium leading-tight">{displayName(prize)}</span>
                <span className="font-mono text-[10px] tabular-nums text-muted-foreground">
                  {formatProbability(probabilities[position] ?? 0)}
                </span>
                {cellMarks(prize)}
              </button>
            );
          })
        )}
      </div>
    ) : (
      <div className="w-full space-y-1.5">
        {prizes.map((prize, index) => (
          <div
            key={prize.key}
            className={cn(
              "flex items-center gap-1 rounded-lg border px-2 py-1.5 transition-colors",
              prize.key === selected?.key ? "border-primary bg-primary/5" : "border-border hover:bg-muted/60"
            )}
          >
            <button
              type="button"
              onClick={() => onSelect(prize.key)}
              className="flex min-w-0 flex-1 items-center gap-2 text-left"
            >
              <span className="w-5 shrink-0 text-center text-[11px] tabular-nums text-muted-foreground">{index + 1}</span>
              <span className={cn("min-w-0 flex-1 truncate text-xs font-medium", prize.type === "none" && "text-muted-foreground")}>
                {displayName(prize)}
              </span>
              {cellMarks(prize)}
              <span className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
                {formatProbability(probabilities[index] ?? 0)}
              </span>
            </button>
            <Button
              size="icon"
              variant="ghost"
              className="size-6 shrink-0"
              disabled={index === 0}
              onClick={() => move(index, -1)}
              aria-label="上移"
            >
              <ArrowUp className="size-3" />
            </Button>
            <Button
              size="icon"
              variant="ghost"
              className="size-6 shrink-0"
              disabled={index === prizes.length - 1}
              onClick={() => move(index, 1)}
              aria-label="下移"
            >
              <ArrowDown className="size-3" />
            </Button>
            <Button
              size="icon"
              variant="ghost"
              className="size-6 shrink-0"
              disabled={prizes.length <= wheelMin}
              onClick={() => remove(prize.key)}
              aria-label="删除奖品"
            >
              <Trash2 className="size-3" />
            </Button>
          </div>
        ))}
        {prizes.length < wheelMax ? (
          <Button
            variant="outline"
            size="sm"
            className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
            onClick={add}
          >
            <Plus className="size-3.5" /> 添加奖品
          </Button>
        ) : null}
      </div>
    );

  const spec = selected ? kinds.find((item) => item.type === selected.type) : undefined;

  return (
    <div className="grid gap-4 md:grid-cols-[minmax(0,18rem)_minmax(0,1fr)]">
      <div className="flex justify-center md:block">{board}</div>

      {selected ? (
        <div className="min-w-0 space-y-3 rounded-xl border border-border p-3">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-semibold">
              {uiMode === "grid" ? `第 ${selectedIndex + 1} 格` : `第 ${selectedIndex + 1} 个`}
              {selected.id ? (
                <span className="ml-1.5 font-normal tabular-nums text-muted-foreground">已抽中 {selected.used}</span>
              ) : null}
            </span>
            {uiMode === "grid" ? (
              <div className="flex gap-0.5">
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-6"
                  disabled={selectedIndex === 0}
                  onClick={() => move(selectedIndex, -1)}
                  aria-label="与上一格交换"
                >
                  <ChevronLeft className="size-3.5" />
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-6"
                  disabled={selectedIndex === prizes.length - 1}
                  onClick={() => move(selectedIndex, 1)}
                  aria-label="与下一格交换"
                >
                  <ChevronRight className="size-3.5" />
                </Button>
              </div>
            ) : null}
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <span className="text-[11px] font-medium">奖品类型</span>
              <Select value={selected.type} onValueChange={(type) => changeKind(selected, type)}>
                <SelectTrigger className="h-9 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {kinds.map((item) => (
                    <SelectItem key={item.type} value={item.type}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <span className="text-[11px] font-medium">名称</span>
              <Input
                value={selected.name}
                maxLength={16}
                placeholder={defaultPrizeName(rewardOf(selected, kinds), kinds, plans, features)}
                onChange={(event) => patch(selected.key, { name: event.target.value })}
              />
            </div>
          </div>

          {spec?.value === "amount" ? (
            <NumberField
              label={spec.label}
              value={selected.amount}
              min={spec.min}
              max={spec.max}
              unit={spec.unit ? `${spec.unit}（${spec.min ?? 0}–${spec.max ?? ""}）` : undefined}
              onChange={(value) => patch(selected.key, { amount: value })}
            />
          ) : null}

          {spec?.value === "ref" ? (
            plans.length === 0 ? (
              <p className="text-xs text-muted-foreground">暂无会员套餐</p>
            ) : (
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">会员套餐</span>
                <Select
                  value={selected.refId ? String(selected.refId) : ""}
                  onValueChange={(value) => patch(selected.key, { refId: Number(value) })}
                >
                  <SelectTrigger className="h-9 w-full">
                    <SelectValue placeholder="选择套餐" />
                  </SelectTrigger>
                  <SelectContent position="popper">
                    {plans.map((plan) => (
                      <SelectItem key={plan.id} value={String(plan.id)}>
                        {plan.name}（{vipPlanTerm(plan)}）
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )
          ) : null}

          {spec?.value === "feature" ? (
            features.length === 0 ? (
              <p className="text-xs text-muted-foreground">暂无功能标识</p>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">功能</span>
                  <Select
                    value={selected.tag}
                    onValueChange={(value) => patch(selected.key, { tag: value })}
                  >
                    <SelectTrigger className="h-9 w-full">
                      <SelectValue placeholder="选择功能" />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      {features.map((feature) => (
                        <SelectItem key={feature.tag} value={feature.tag}>
                          {feature.name || feature.tag}
                          <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <NumberField
                  label="天数"
                  value={selected.amount}
                  min={0}
                  max={spec.max}
                  unit={termLabel(Number(selected.amount) || 0)}
                  onChange={(value) => patch(selected.key, { amount: value })}
                />
              </div>
            )
          ) : null}

          {spec?.value === "text" ? (
            <div className="space-y-1.5">
              <span className="text-[11px] font-medium">发放内容</span>
              <Input
                value={selected.text}
                maxLength={64}
                placeholder="例如：定制贴纸一套"
                onChange={(event) => patch(selected.key, { text: event.target.value })}
              />
            </div>
          ) : null}

          <div className="grid gap-3 sm:grid-cols-2">
            <NumberField
              label="权重"
              value={selected.weight}
              min={0}
              unit={formatProbability(probabilities[selectedIndex] ?? 0)}
              onChange={(value) => patch(selected.key, { weight: value })}
            />
            {selected.unlimited ? (
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">库存</span>
                <div className="flex h-9 items-center rounded-md border border-dashed border-border px-3 text-xs text-muted-foreground">
                  不限
                </div>
              </div>
            ) : (
              <NumberField
                label="总库存"
                value={selected.quantity}
                min={selected.used}
                unit={`剩余 ${Math.max(0, (Number(selected.quantity) || 0) - selected.used)}`}
                onChange={(value) => patch(selected.key, { quantity: value })}
              />
            )}
          </div>

          <div className="grid gap-3 sm:grid-cols-3">
            <NumberField
              label="全站每日"
              value={selected.dailyLimit}
              min={0}
              unit="个，0 为不限"
              disabled={selected.fallback}
              onChange={(value) => patch(selected.key, { dailyLimit: value })}
            />
            <NumberField
              label="每人累计"
              value={selected.userLimit}
              min={0}
              unit="次，0 为不限"
              disabled={selected.fallback}
              onChange={(value) => patch(selected.key, { userLimit: value })}
            />
            <NumberField
              label="每人每日"
              value={selected.userDailyLimit}
              min={0}
              unit="次，0 为不限"
              disabled={selected.fallback}
              onChange={(value) => patch(selected.key, { userDailyLimit: value })}
            />
          </div>

          <div className="grid gap-2 sm:grid-cols-2">
            <SwitchRow
              label="不限库存"
              checked={selected.unlimited}
              onChange={(value) =>
                patch(selected.key, { unlimited: value, fallback: value ? selected.fallback : false })
              }
            />
            <SwitchRow
              label="兜底奖"
              checked={selected.fallback}
              onChange={(value) => setFallback(selected.key, value)}
            />
          </div>

          {OWNABLE_KINDS.includes(selected.type) ? (
            <div className="space-y-2 rounded-lg border border-dashed border-border p-3">
              <div className="flex items-baseline justify-between gap-2">
                <span className="text-[11px] font-medium">已拥有时改发</span>
                <span className="text-[10px] text-muted-foreground">
                  {(() => {
                    const preview = swapPreview?.(selected);
                    return preview ? `抽中后改发 ${preview}` : "照常发放";
                  })()}
                </span>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <Select
                  value={selected.swapType || SWAP_POLICY}
                  onValueChange={(value) => {
                    const swapType = value === SWAP_POLICY ? "" : value;
                    const swapSpec = kinds.find((item) => item.type === swapType);
                    patch(selected.key, {
                      swapType,
                      swapAmount: swapType ? selected.swapAmount || String(Math.max(swapSpec?.min ?? 1, 1)) : ""
                    });
                  }}
                >
                  <SelectTrigger className="h-9 w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={SWAP_POLICY}>按活动的处理方式</SelectItem>
                    {(catalog?.swapKinds?.length ? catalog.swapKinds : SWAP_KINDS).map((type) => (
                      <SelectItem key={type} value={type}>
                        {kinds.find((item) => item.type === type)?.label ?? type}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {selected.swapType ? (
                  <Input
                    type="number"
                    inputMode="numeric"
                    min={kinds.find((item) => item.type === selected.swapType)?.min}
                    max={kinds.find((item) => item.type === selected.swapType)?.max}
                    value={selected.swapAmount}
                    aria-label="改发数量"
                    onChange={(event) => patch(selected.key, { swapAmount: event.target.value })}
                  />
                ) : null}
              </div>
              <p className="text-[11px] text-muted-foreground">永久会员已包含或已永久解锁时生效；活动选择照常发放时不生效。</p>
            </div>
          ) : null}

          <div className="space-y-1.5">
            <span className="text-[11px] font-medium">图片</span>
            <Input
              value={selected.imageUrl}
              maxLength={512}
              placeholder="https://"
              onChange={(event) => patch(selected.key, { imageUrl: event.target.value })}
            />
          </div>
        </div>
      ) : (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          暂无奖品
        </p>
      )}
    </div>
  );
}

