"use client";

import { useState } from "react";
import { Footprints, Loader2, Radar, UserRound, X } from "lucide-react";
import { toast } from "sonner";
import { Field, NumberInput } from "@/components/apps/item-grant/item-grant-fields";
import { ITEM_ICON, NODE_VISUAL, formatNumber } from "@/components/apps/item-grant/item-grant-meta";
import { ToneIcon } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { ItemGrantCatalog, ItemGrantEstimate, ItemGrantGraph, ItemGrantPreview } from "@/lib/api/item-grant";
import { ApiError } from "@/lib/api/client";
import { usePreviewItemGrantMutation } from "@/lib/item-grant-hooks";
import { cn } from "@/lib/utils";

/**
 * 预演：只判定、不发放，走的是与正式执行同一个判定函数。
 *
 * - 抽样估算：从目标人群随机抽 N 人逐个判定，画布上每个节点标出到达人数、每条线标出走过的人数，
 *   下面按物品推算全量发放量（抽样量 × 候选人数 / 样本数）。
 * - 单人追踪：指定一个人（用户 ID 或账号）此刻会走哪条路、拿到什么；事件触发的规则可以填事件数据。
 *   画布上走过的节点高亮、其余变淡，线上流动。
 */

export function PreviewPanel({
  appKey,
  ruleId,
  catalog,
  graph,
  eventKey,
  onEstimate,
  onTrace
}: {
  appKey: string;
  ruleId?: number;
  catalog: ItemGrantCatalog;
  graph: () => ItemGrantGraph;
  eventKey?: string;
  onEstimate: (estimate: ItemGrantEstimate | null) => void;
  onTrace: (preview: ItemGrantPreview["trace"] | null) => void;
}) {
  const [mode, setMode] = useState<"sample" | "user">("sample");
  const [samples, setSamples] = useState("200");
  const [user, setUser] = useState("");
  const [eventData, setEventData] = useState<Record<string, number>>({});
  const [result, setResult] = useState<ItemGrantPreview | null>(null);
  const preview = usePreviewItemGrantMutation(appKey);
  const event = catalog.events.find((candidate) => candidate.key === eventKey);

  const run = () => {
    const payload =
      mode === "sample"
        ? { ruleId, graph: graph(), samples: Number(samples) }
        : { ruleId, graph: graph(), user: user.trim(), eventData };
    preview.mutate(payload, {
      onSuccess: (data) => {
        setResult(data);
        const errors = data.issues.filter((issue) => issue.level === "error");
        if (errors.length && !data.estimate && !data.trace) {
          toast.error(`画布还有 ${errors.length} 个错误，先在「问题」里修好`);
        }
        onEstimate(data.estimate ?? null);
        onTrace(data.trace ?? null);
      },
      onError: (error) => toast.error(error instanceof ApiError ? error.message : "预演失败")
    });
  };

  const clear = () => {
    setResult(null);
    onEstimate(null);
    onTrace(null);
  };

  return (
    <div className="space-y-4">
      <ToggleGroup
        type="single"
        variant="outline"
        size="sm"
        className="w-full"
        value={mode}
        onValueChange={(next) => {
          if (!next) return;
          setMode(next as "sample" | "user");
          clear();
        }}
      >
        <ToggleGroupItem value="sample" className="flex-1 text-xs">
          <Radar className="size-3.5" /> 抽样估算
        </ToggleGroupItem>
        <ToggleGroupItem value="user" className="flex-1 text-xs">
          <Footprints className="size-3.5" /> 单人追踪
        </ToggleGroupItem>
      </ToggleGroup>

      {mode === "sample" ? (
        <Field label="样本人数" hint="从目标人群随机抽取，逐个判定；不会发放任何东西">
          <Select value={samples} onValueChange={setSamples}>
            <SelectTrigger size="sm" className="h-8 w-full text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper">
              {["100", "200", "500", "1000"].map((value) => (
                <SelectItem key={value} value={value}>
                  {value} 人
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      ) : (
        <div className="space-y-3">
          <Field label="用户" hint="管理端用户 ID 或账号">
            <div className="relative">
              <UserRound className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="h-8 pl-8"
                value={user}
                placeholder="例如 10086 或 alice"
                onChange={(event) => setUser(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && user.trim()) run();
                }}
              />
            </div>
          </Field>
          {event?.fields.length ? (
            <div className="grid grid-cols-2 gap-3">
              {event.fields.map((field) => (
                <Field key={field.key} label={field.label}>
                  <NumberInput
                    decimal
                    value={eventData[field.key.replace("event.", "")]}
                    onChange={(value) => setEventData({ ...eventData, [field.key.replace("event.", "")]: value })}
                    suffix={field.unit}
                  />
                </Field>
              ))}
            </div>
          ) : null}
        </div>
      )}

      <div className="flex gap-2">
        <Button className="flex-1" size="sm" onClick={run} disabled={preview.isPending || (mode === "user" && !user.trim())}>
          {preview.isPending ? <Loader2 className="animate-spin" /> : mode === "sample" ? <Radar /> : <Footprints />}
          {mode === "sample" ? "开始估算" : "追踪这个人"}
        </Button>
        {result ? (
          <Button size="sm" variant="outline" onClick={clear}>
            <X /> 清除
          </Button>
        ) : null}
      </div>

      {result?.estimate ? <EstimateView estimate={result.estimate} /> : null}
      {result?.trace ? <TraceView trace={result.trace} catalog={catalog} /> : null}
    </div>
  );
}

function EstimateView({ estimate }: { estimate: ItemGrantEstimate }) {
  const rate = estimate.sampled ? (estimate.granting / estimate.sampled) * 100 : 0;
  const projectedUsers = estimate.sampled ? Math.round((estimate.granting / estimate.sampled) * estimate.candidates) : 0;
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-2">
        <Metric label="目标人群" value={formatNumber(estimate.candidates)} sub="人" />
        <Metric label="样本命中" value={`${rate.toFixed(rate >= 10 ? 0 : 1)}%`} sub={`${estimate.granting} / ${estimate.sampled}`} />
        <Metric label="预计到账" value={formatNumber(projectedUsers)} sub="人" className="col-span-2" />
      </div>
      {estimate.blocked ? (
        <p className="text-[11px] text-muted-foreground">样本里有 {estimate.blocked} 个账号不可用（停用 / 冻结 / 封禁），不会发放</p>
      ) : null}
      <div className="rounded-xl border">
        <div className="border-b px-3 py-2 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
          推算全量发放
        </div>
        {estimate.items.length === 0 ? (
          <p className="px-3 py-4 text-center text-xs text-muted-foreground">样本里没有人拿到物品</p>
        ) : (
          <div className="divide-y">
            {estimate.items.map((item, index) => {
              const Icon = ITEM_ICON[item.type] ?? Radar;
              return (
                <div key={index} className="flex items-center gap-2 px-3 py-2">
                  <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-xs font-medium">{item.detail || item.label}</div>
                    <div className="text-[11px] text-muted-foreground">
                      样本 {item.count} 份{item.amount ? `，共 ${formatNumber(item.amount)}` : ""}
                    </div>
                  </div>
                  <div className="text-right">
                    <div className="text-xs font-semibold tabular-nums">≈ {formatNumber(Math.round(item.projected))}</div>
                    <div className="text-[10px] text-muted-foreground">{item.amount ? "总量" : "份"}</div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function Metric({ label, value, sub, className }: { label: string; value: string; sub?: string; className?: string }) {
  return (
    <div className={cn("rounded-xl border bg-muted/20 px-3 py-2", className)}>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="mt-0.5 flex items-baseline gap-1">
        <span className="text-lg font-semibold tabular-nums">{value}</span>
        {sub ? <span className="text-[11px] text-muted-foreground">{sub}</span> : null}
      </div>
    </div>
  );
}

function TraceView({ trace, catalog }: { trace: NonNullable<ItemGrantPreview["trace"]>; catalog: ItemGrantCatalog }) {
  const { user, outcome, items } = trace;
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 rounded-xl border bg-muted/20 px-3 py-2">
        <ToneIcon icon={UserRound} tone="info" size="sm" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs font-medium">{user.nickname || user.account}</div>
          <div className="text-[11px] text-muted-foreground">
            #{user.userId} Lv.{user.level}
            {user.lifetime ? " 永久会员" : user.member ? " 会员" : ""}
          </div>
        </div>
        <Badge variant={outcome.items.length ? "success" : "secondary"} size="sm">
          {outcome.blocked ? "不可用" : outcome.items.length ? `${outcome.items.length} 项物品` : "不发放"}
        </Badge>
      </div>
      {outcome.blocked ? <p className="text-xs text-muted-foreground">{outcome.blocked}</p> : null}
      <ol className="relative space-y-2 border-l border-dashed pl-4">
        {outcome.steps.map((step, index) => {
          const visual = NODE_VISUAL[step.type];
          const spec = catalog.nodes.find((candidate) => candidate.type === step.type);
          const Icon = visual.icon;
          return (
            <li key={`${step.nodeId}-${index}`} className="relative">
              <span
                className="absolute top-1 -left-[23px] flex size-3.5 items-center justify-center rounded-full border-2 border-background"
                style={{ background: visual.accent }}
              />
              <div className="flex items-center gap-1.5 text-xs font-medium">
                <Icon className="size-3.5 text-muted-foreground" />
                {spec?.label ?? step.type}
                {step.handle ? (
                  <span className="rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">→ {step.handle}</span>
                ) : null}
              </div>
              <div className="text-[11px] leading-4 text-muted-foreground">{step.detail}</div>
            </li>
          );
        })}
      </ol>
      {items.length ? (
        <div className="rounded-xl border bg-emerald-500/5 px-3 py-2">
          <div className="mb-1 text-[11px] font-semibold text-emerald-700 dark:text-emerald-300">会拿到</div>
          <ul className="list-inside list-disc space-y-0.5 text-xs">
            {items.map((item, index) => (
              <li key={index}>{item}</li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}
