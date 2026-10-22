"use client";

import { useState } from "react";
import { FlaskConical, Loader2, Play } from "lucide-react";
import { toast } from "sonner";
import { NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import { BehaviorScoreView, DrawPlanView, percent } from "@/components/apps/lottery/lottery-plan-view";
import { SOURCE_LABEL, formatCount } from "@/components/apps/lottery/lottery-shared";
import { STRATEGY_LIMITS } from "@/components/apps/lottery/lottery-strategy-form";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ApiError } from "@/lib/api/client";
import type {
  LotteryActivityPayload,
  LotteryCatalog,
  LotteryDistribution,
  LotterySimulateResponse,
  LotterySource
} from "@/lib/api/lottery";
import { useSimulateLotteryMutation } from "@/lib/lottery-hooks";
import { cn } from "@/lib/utils";

/**
 * 策略模拟：按编辑器里当前（未保存也行）的奖盘与策略，让一批虚拟用户各自连续抽若干次。
 * 填用户 ID 时用这个真实用户的全部行为与本活动进度，并给出他此刻再抽一次的出奖方案。
 * 只算不写，不发奖。
 */
export function LotteryStrategySimulator({
  appKey,
  activityId,
  payload,
  catalog,
  open,
  onOpenChange
}: {
  appKey: string;
  activityId?: number;
  /** 编辑器当前内容 */
  payload: () => LotteryActivityPayload;
  catalog?: LotteryCatalog | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const limits = catalog?.strategyLimits ?? STRATEGY_LIMITS;
  const simulate = useSimulateLotteryMutation(appKey);
  const [users, setUsers] = useState("500");
  const [draws, setDraws] = useState("30");
  const [source, setSource] = useState<LotterySource>("free");
  const [userId, setUserId] = useState("");
  const [isolated, setIsolated] = useState(false);
  const [result, setResult] = useState<LotterySimulateResponse | null>(null);

  const total = (Number(users) || 0) * (Number(draws) || 0);
  const run = async () => {
    if (total > limits.simulationDraws) {
      toast.error(`总抽奖次数不能超过 ${formatCount(limits.simulationDraws)}`);
      return;
    }
    try {
      const response = await simulate.mutateAsync({
        activityId,
        activity: payload(),
        userId: Number(userId) > 0 ? Number(userId) : undefined,
        users: Number(users) || 1,
        drawsPerUser: Number(draws) || 1,
        source,
        isolatedStock: isolated
      });
      setResult(response);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "模拟失败");
    }
  };

  const nameOf = (id: number, position: number) =>
    result?.result.prizes.find((prize) => prize.prizeId === id)?.name ||
    result?.result.prizes.find((prize) => prize.position === position)?.name ||
    `位置 ${position + 1}`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-3xl">
        <DialogHeader className="shrink-0 border-b px-6 py-4">
          <DialogTitle className="flex items-center gap-2">
            <FlaskConical className="size-4" /> 策略模拟
          </DialogTitle>
          <DialogDescription>按当前编辑内容计算，不写入数据、不发奖</DialogDescription>
        </DialogHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-6 py-5">
            <div className="grid gap-3 sm:grid-cols-4">
              <NumberField
                label="虚拟用户"
                value={users}
                min={1}
                max={limits.simulationUsers}
                unit="人"
                onChange={setUsers}
              />
              <NumberField
                label="每人抽奖"
                value={draws}
                min={1}
                max={limits.simulationEach}
                unit="次"
                onChange={setDraws}
              />
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">次数来源</span>
                <Select value={source} onValueChange={(value) => setSource(value as LotterySource)}>
                  <SelectTrigger className="h-9 w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(["free", "bonus", "points"] as LotterySource[]).map((item) => (
                      <SelectItem key={item} value={item}>
                        {SOURCE_LABEL[item]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">用户 ID</span>
                <Input
                  inputMode="numeric"
                  value={userId}
                  placeholder="可选"
                  onChange={(event) => setUserId(event.target.value.replace(/\D/g, ""))}
                  className="h-9 font-mono text-xs"
                />
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-3">
              <div className="min-w-56 flex-1">
                <SwitchRow
                  label="每人独立库存"
                  hint="关闭时所有虚拟用户共享库存与每日限量"
                  checked={isolated}
                  onChange={setIsolated}
                />
              </div>
              <Button onClick={run} disabled={simulate.isPending} className="shrink-0">
                {simulate.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                运行 {formatCount(total)} 次
              </Button>
            </div>

            {result ? <SimulationReport result={result} nameOf={nameOf} catalog={catalog} hasUser={Number(userId) > 0} /> : null}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

function SimulationReport({
  result,
  nameOf,
  catalog,
  hasUser
}: {
  result: LotterySimulateResponse;
  nameOf: (id: number, position: number) => string;
  catalog?: LotteryCatalog | null;
  hasUser: boolean;
}) {
  const sim = result.result;
  const delta = sim.winRate - sim.baseWinRate;
  const maxRate = Math.max(0.0001, ...sim.prizes.map((prize) => Math.max(prize.rate, prize.baseRate)));

  return (
    <div className="space-y-5">
      <div className="grid gap-2 sm:grid-cols-4">
        <Stat
          label="实际中奖率"
          value={percent(sim.winRate)}
          sub={`公示 ${percent(sim.baseWinRate)}`}
          tone={delta > 0.0005 ? "up" : delta < -0.0005 ? "down" : undefined}
        />
        <Stat label="首次中奖" value={distText(sim.firstWin, "p50")} sub={`P90 ${distText(sim.firstWin, "p90")}，未中过 ${sim.neverWon} 人`} />
        <Stat label="最长连续未中" value={distText(sim.missStreak, "p90")} sub={`P90，最多 ${sim.missStreak.max} 次`} />
        <Stat
          label="保底"
          value={`${formatCount(sim.pityHits)} 次`}
          sub={sim.softPityDraws ? `软保底生效 ${formatCount(sim.softPityDraws)} 次` : "软保底未生效"}
        />
      </div>
      <p className="text-[11px] text-muted-foreground">
        共 {formatCount(sim.users)} 人、{formatCount(sim.draws)} 次，发出积分 {formatCount(sim.pointsIssued)}
      </p>

      <div className="space-y-1.5">
        <span className="text-[11px] font-medium">各奖品出现比例</span>
        <div className="divide-y divide-border rounded-lg border border-border">
          {sim.prizes.map((prize) => (
            <div key={`${prize.prizeId}-${prize.position}`} className="grid grid-cols-[1.25rem_1fr_7rem] items-center gap-2 px-2.5 py-1.5">
              <span className="font-mono text-[11px] text-muted-foreground">{prize.position + 1}</span>
              <div className="min-w-0 space-y-1">
                <div className="flex items-center gap-2 text-xs">
                  <span className="truncate">{prize.name}</span>
                  {prize.blocked ? (
                    <span className="shrink-0 text-[10px] text-amber-600 dark:text-amber-400">
                      受限 {formatCount(prize.blocked)} 次
                    </span>
                  ) : null}
                </div>
                <div className="relative h-1.5 overflow-hidden rounded-full bg-muted">
                  <span
                    className="absolute inset-y-0 left-0 rounded-full bg-muted-foreground/30"
                    style={{ width: `${(prize.baseRate / maxRate) * 100}%` }}
                  />
                  <span
                    className="absolute inset-y-0 left-0 rounded-full bg-emerald-500"
                    style={{ width: `${(prize.rate / maxRate) * 100}%`, opacity: 0.85 }}
                  />
                </div>
              </div>
              <div className="text-right font-mono text-[11px] tabular-nums">
                {percent(prize.rate)}
                <span className="ml-1 text-muted-foreground">/ {percent(prize.baseRate)}</span>
              </div>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-muted-foreground">绿色为模拟结果，灰色为公示概率</p>
      </div>

      {hasUser ? <ProfileView result={result} catalog={catalog} /> : result.behavior ? (
        <BehaviorScoreView score={result.behavior} signals={catalog?.signals} />
      ) : null}

      {result.current ? (
        <div className="space-y-1.5">
          <span className="text-[11px] font-medium">此用户下一次抽奖</span>
          <DrawPlanView plan={result.current} prizeName={nameOf} signals={catalog?.signals} />
        </div>
      ) : null}
    </div>
  );
}

function ProfileView({ result, catalog }: { result: LotterySimulateResponse; catalog?: LotteryCatalog | null }) {
  const profile = result.profile as unknown as Record<string, number | boolean>;
  const signals = catalog?.signals ?? [];
  return (
    <div className="space-y-2">
      <span className="text-[11px] font-medium">用户行为</span>
      <div className="grid gap-1.5 sm:grid-cols-3">
        {signals.map((spec) => {
          const raw = profile[spec.key];
          const text =
            typeof raw === "boolean"
              ? raw
                ? "是"
                : "否"
              : spec.key === "memberRate"
                ? percent(Number(raw) || 0, 0)
                : `${Number((Number(raw) || 0).toFixed(1)).toLocaleString("zh-CN")}${spec.unit && spec.unit !== "%" ? ` ${spec.unit}` : ""}`;
          return (
            <div key={spec.key} className="flex items-baseline justify-between gap-2 rounded-md bg-muted px-2 py-1 text-[11px]">
              <span className="truncate text-muted-foreground">{spec.label}</span>
              <span className="shrink-0 font-mono tabular-nums">{text}</span>
            </div>
          );
        })}
      </div>
      {result.behavior ? <BehaviorScoreView score={result.behavior} signals={signals} /> : null}
    </div>
  );
}

function distText(dist: LotteryDistribution, key: "p50" | "p90") {
  return dist[key] ? `第 ${dist[key]} 次` : "—";
}

function Stat({ label, value, sub, tone }: { label: string; value: string; sub?: string; tone?: "up" | "down" }) {
  return (
    <div className="space-y-0.5 rounded-xl bg-muted px-3 py-2.5">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div
        className={cn(
          "font-mono text-lg font-semibold tabular-nums",
          tone === "up" && "text-emerald-600 dark:text-emerald-400",
          tone === "down" && "text-amber-600 dark:text-amber-400"
        )}
      >
        {value}
      </div>
      {sub ? <div className="truncate text-[10px] text-muted-foreground">{sub}</div> : null}
    </div>
  );
}
