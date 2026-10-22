"use client";

import { Badge } from "@/components/ui/badge";
import { FACTOR_LABEL } from "@/components/apps/lottery/lottery-strategy-form";
import type { LotteryBehaviorScore, LotteryDrawPlan, LotterySignalSpec } from "@/lib/api/lottery";
import { cn } from "@/lib/utils";

/**
 * 出奖方案的展示：每个奖品的基础权重、各维度系数、最终概率，以及行为分明细。
 * 抽奖记录的 trace 与模拟里「此人下一次」共用。
 */

export const percent = (value: number, digits = 2) => `${(value * 100).toFixed(digits)}%`;

function factorText(value: number) {
  if (value === 0) return "0";
  return `×${Number(value.toFixed(3))}`;
}

export function DrawPlanView({
  plan,
  prizeName,
  signals
}: {
  plan: LotteryDrawPlan;
  /** 按奖品 ID 取名字（奖品可能已被删，取不到时显示位置） */
  prizeName: (id: number, position: number) => string;
  signals?: LotterySignalSpec[];
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-1.5 text-[11px]">
        <Badge variant="outline" size="sm">
          中奖率 {percent(plan.winRate)}
        </Badge>
        {plan.softPity ? (
          <Badge variant="info" size="sm">
            软保底 +{plan.softPity.toFixed(1)} 个百分点
          </Badge>
        ) : null}
        {plan.pity ? (
          <Badge variant="info" size="sm">
            保底触发
          </Badge>
        ) : null}
        {plan.fallback ? (
          <Badge variant="warning" size="sm">
            改发兜底奖
          </Badge>
        ) : null}
        {plan.risk ? (
          <Badge variant="danger" size="sm">
            设备聚集
          </Badge>
        ) : null}
      </div>

      <div className="divide-y divide-border rounded-lg border border-border">
        {plan.odds.map((odds) => {
          const picked = plan.picked === odds.id;
          return (
            <div key={`${odds.id}-${odds.pos}`} className={cn("space-y-1 px-2.5 py-2", picked && "bg-emerald-500/5")}>
              <div className="flex items-center gap-2 text-xs">
                <span className="w-4 shrink-0 font-mono text-[11px] text-muted-foreground">{odds.pos + 1}</span>
                <span className={cn("min-w-0 flex-1 truncate", picked && "font-medium")}>{prizeName(odds.id, odds.pos)}</span>
                {picked ? (
                  <Badge variant="success" size="sm">
                    抽中
                  </Badge>
                ) : null}
                <span className="w-16 shrink-0 text-right font-mono text-[11px] tabular-nums text-muted-foreground">
                  {odds.base}
                </span>
                <span className="w-16 shrink-0 text-right font-mono text-[11px] tabular-nums">{percent(odds.p)}</span>
              </div>
              <div className="ml-6 flex items-center gap-2">
                <span className="h-1 flex-1 overflow-hidden rounded-full bg-muted">
                  <span
                    className={cn("block h-full rounded-full", odds.blocked ? "bg-muted-foreground/30" : "bg-emerald-500")}
                    style={{ width: `${Math.min(100, odds.p * 100)}%` }}
                  />
                </span>
              </div>
              {odds.f?.length ? (
                <div className="ml-6 flex flex-wrap gap-1">
                  {odds.f.map((factor, index) => (
                    <span
                      key={`${factor.k}-${index}`}
                      title={factor.n}
                      className={cn(
                        "rounded px-1.5 py-px text-[10px]",
                        factor.v === 0
                          ? "bg-rose-500/10 text-rose-600 dark:text-rose-400"
                          : factor.v > 1
                            ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
                            : "bg-amber-500/10 text-amber-700 dark:text-amber-400"
                      )}
                    >
                      {FACTOR_LABEL[factor.k] ?? factor.k}
                      {factor.k === "rule" && factor.n ? `「${factor.n}」` : ""} {factorText(factor.v)}
                      {factor.k !== "rule" && factor.n ? `（${factor.n}）` : ""}
                    </span>
                  ))}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
      <p className="text-[10px] text-muted-foreground">左列为基础权重，右列为这一次的最终概率</p>

      {plan.behavior ? <BehaviorScoreView score={plan.behavior} signals={signals} /> : null}
    </div>
  );
}

export function BehaviorScoreView({ score, signals }: { score: LotteryBehaviorScore; signals?: LotterySignalSpec[] }) {
  const specOf = (key: string) => signals?.find((spec) => spec.key === key);
  return (
    <div className="space-y-2 rounded-lg border border-border p-2.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[11px] font-medium">行为分</span>
        <span className="font-mono text-xs tabular-nums">
          {score.score.toFixed(3)}
          <span className="ml-2 text-muted-foreground">中奖权重 ×{score.factor.toFixed(3)}</span>
        </span>
      </div>
      <div className="grid gap-x-4 gap-y-1 sm:grid-cols-2">
        {score.signals.map((signal) => {
          const spec = specOf(signal.key);
          return (
            <div key={signal.key} className="flex items-center gap-2 text-[11px]">
              <span className="w-28 shrink-0 truncate text-muted-foreground">{spec?.label ?? signal.key}</span>
              <span className="h-1 flex-1 overflow-hidden rounded-full bg-muted">
                <span
                  className={cn("block h-full rounded-full", spec?.negative ? "bg-rose-500" : "bg-emerald-500")}
                  style={{ width: `${Math.min(100, signal.score * 100)}%` }}
                />
              </span>
              <span className="w-16 shrink-0 text-right font-mono tabular-nums">
                {spec?.key === "memberRate" ? percent(signal.value, 0) : Number(signal.value.toFixed(1)).toLocaleString("zh-CN")}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
