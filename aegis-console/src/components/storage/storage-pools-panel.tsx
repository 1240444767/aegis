"use client";

import { useState } from "react";
import {
  CircleCheck,
  CircleMinus,
  FlaskConical,
  Layers,
  Loader2,
  Pencil,
  Plus,
  RotateCcw,
  Star,
  TriangleAlert
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ApiError } from "@/lib/api/client";
import type { StoragePool, StoragePoolPlan, StorageSource } from "@/lib/api/storage-center";
import { formatRelative } from "@/lib/datetime";
import {
  useResetStorageHealthMutation,
  useSimulateStoragePoolMutation,
  useStorageProvidersQuery
} from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";
import { StoragePoolEditor } from "./storage-pool-editor";
import {
  MODE_META,
  SIZE_UNITS,
  STRATEGY_META,
  StateBadge,
  StorageBrandBadge,
  UsageMeter,
  joinSize,
  providerMeta,
  type SizeUnit
} from "./storage-shared";

export function StoragePoolsPanel({
  pools,
  sources,
  loading
}: {
  pools: StoragePool[];
  sources: StorageSource[];
  loading: boolean;
}) {
  const providers = useStorageProvidersQuery().data;
  const reset = useResetStorageHealthMutation();
  const [editing, setEditing] = useState<StoragePool | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [simulating, setSimulating] = useState<StoragePool | null>(null);

  const openEditor = (pool: StoragePool | null) => {
    setEditing(pool);
    setEditorOpen(true);
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <span className="text-xs tabular-nums text-muted-foreground">{pools.length} 个存储池</span>
        <Button size="sm" onClick={() => openEditor(null)}>
          <Plus className="size-3.5" />
          新建存储池
        </Button>
      </div>

      {loading ? (
        <Skeleton className="h-64 rounded-2xl" />
      ) : pools.length === 0 ? (
        <button
          type="button"
          onClick={() => openEditor(null)}
          className="flex w-full flex-col items-center justify-center gap-2 rounded-2xl border border-dashed py-14 text-sm text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground"
        >
          <Layers className="size-5" />
          暂无存储池
        </button>
      ) : (
        <div className="space-y-4">
          {pools.map((pool) => {
            const writable = pool.members.filter(
              (member) => member.mode !== "readonly" && member.state !== "disabled" && member.state !== "backoff" && member.state !== "full"
            ).length;
            return (
              <section key={pool.id} className={cn("overflow-hidden rounded-2xl border bg-card", !pool.enabled && "opacity-70")}>
                <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
                  <div className="grid size-9 place-items-center rounded-xl bg-emerald-500/10 text-emerald-600 ring-1 ring-inset ring-emerald-500/20 dark:text-emerald-400">
                    <Layers className="size-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="font-mono text-sm font-semibold">{pool.name}</span>
                      {pool.isDefault ? (
                        <Badge variant="info" size="sm" className="gap-1">
                          <Star className="size-3" />
                          默认
                        </Badge>
                      ) : null}
                      {!pool.enabled ? (
                        <Badge variant="outline" size="sm">
                          已停用
                        </Badge>
                      ) : null}
                    </div>
                    <div className="text-[11px] text-muted-foreground">
                      {STRATEGY_META[pool.strategy].label} · 可写 {writable}/{pool.members.length} · 最多尝试 {pool.maxAttempts} 个
                      {pool.readFallback ? " · 读取回退" : ""}
                      {pool.description ? ` · ${pool.description}` : ""}
                    </div>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <Button size="sm" variant="outline" onClick={() => setSimulating(pool)}>
                      <FlaskConical className="size-3.5" />
                      模拟写入
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => openEditor(pool)}>
                      <Pencil className="size-3.5" />
                      编辑
                    </Button>
                  </div>
                </header>

                {pool.members.length === 0 ? (
                  <div className="px-4 py-8 text-center text-xs text-muted-foreground">暂无成员</div>
                ) : (
                  <div className="divide-y">
                    {[...pool.members]
                      .sort((a, b) => a.priority - b.priority)
                      .map((member, index) => {
                        const meta = providerMeta(providers, member.config?.provider ?? "");
                        const health = member.health;
                        const cooling = member.state === "backoff" || member.state === "degraded";
                        return (
                          <div key={member.configId} className="grid items-center gap-3 px-4 py-3 md:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_auto]">
                            <div className="flex min-w-0 items-center gap-3">
                              <span className="w-4 text-center font-mono text-xs tabular-nums text-muted-foreground">{index + 1}</span>
                              <StorageBrandBadge meta={meta} provider={member.config?.provider} size="sm" />
                              <div className="min-w-0">
                                <div className="flex items-center gap-1.5">
                                  <span className="truncate text-sm font-medium">{member.config?.name ?? `#${member.configId}`}</span>
                                  {member.mode !== "active" ? (
                                    <Badge variant="outline" size="sm">
                                      {MODE_META[member.mode].label}
                                    </Badge>
                                  ) : null}
                                  {pool.strategy === "weighted" ? (
                                    <span className="font-mono text-[11px] text-muted-foreground">×{member.weight}</span>
                                  ) : null}
                                </div>
                                {cooling && health ? (
                                  <Tooltip>
                                    <TooltipTrigger asChild>
                                      <div className="flex items-center gap-1 truncate text-[11px] text-red-600 dark:text-red-400">
                                        <TriangleAlert className="size-3 shrink-0" />
                                        <span className="truncate">
                                          {health.backoffUntil ? `${formatRelative(health.backoffUntil)}重试` : `失败 ${health.consecutiveFailures} 次`}
                                        </span>
                                      </div>
                                    </TooltipTrigger>
                                    {health.lastError ? (
                                      <TooltipContent className="max-w-sm break-all font-mono text-[11px]">{health.lastError}</TooltipContent>
                                    ) : null}
                                  </Tooltip>
                                ) : (
                                  <div className="text-[11px] text-muted-foreground tabular-nums">
                                    {health?.uploads ? `${health.uploads} 次写入` : "暂无写入"}
                                    {health?.lastLatencyMs ? ` · ${health.lastLatencyMs} ms` : ""}
                                  </div>
                                )}
                              </div>
                            </div>
                            <UsageMeter used={member.usage?.usedBytes ?? 0} capacity={member.capacityBytes} warnPercent={pool.warnPercent} />
                            <div className="flex items-center justify-end gap-1.5">
                              <StateBadge state={member.state} />
                              {cooling ? (
                                <Button
                                  size="icon-xs"
                                  variant="ghost"
                                  aria-label="恢复写入"
                                  onClick={async () => {
                                    await reset.mutateAsync(member.configId);
                                    toast.success("已恢复写入");
                                  }}
                                >
                                  <RotateCcw />
                                </Button>
                              ) : null}
                            </div>
                          </div>
                        );
                      })}
                  </div>
                )}
              </section>
            );
          })}
        </div>
      )}

      <StoragePoolEditor open={editorOpen} pool={editing} sources={sources} onOpenChange={setEditorOpen} />
      <SimulateDialog pool={simulating} onOpenChange={(open) => !open && setSimulating(null)} />
    </div>
  );
}

function SimulateDialog({ pool, onOpenChange }: { pool: StoragePool | null; onOpenChange: (open: boolean) => void }) {
  const simulate = useSimulateStoragePoolMutation();
  const [size, setSize] = useState("10");
  const [unit, setUnit] = useState<SizeUnit>("MB");
  const [plan, setPlan] = useState<StoragePoolPlan | null>(null);
  const current = plan && pool && plan.poolId === pool.id ? plan : null;

  async function run() {
    if (!pool) return;
    try {
      setPlan(await simulate.mutateAsync({ poolId: pool.id, size: joinSize(size, unit) }));
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "模拟失败");
    }
  }

  return (
    <Dialog
      open={Boolean(pool)}
      onOpenChange={(open) => {
        if (!open) setPlan(null);
        onOpenChange(open);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-base">模拟写入 · {pool?.name}</DialogTitle>
        </DialogHeader>
        <div className="flex items-center gap-2">
          <div className="flex flex-1 items-center">
            <Input
              value={size}
              inputMode="decimal"
              className="rounded-r-none font-mono"
              onChange={(e) => setSize(e.target.value.replace(/[^\d.]/g, ""))}
              onKeyDown={(e) => e.key === "Enter" && run()}
            />
            <Select value={unit} onValueChange={(value) => setUnit(value as SizeUnit)}>
              <SelectTrigger className="w-[72px] rounded-l-none border-l-0">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SIZE_UNITS.map((item) => (
                  <SelectItem key={item} value={item}>
                    {item}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button onClick={run} disabled={simulate.isPending}>
            {simulate.isPending ? <Loader2 className="size-4 animate-spin" /> : <FlaskConical className="size-4" />}
            模拟
          </Button>
        </div>
        {current ? (
          <div className="space-y-1.5">
            {current.target === 0 ? (
              <div className="rounded-xl bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">没有可写入的成员</div>
            ) : null}
            {current.steps.map((step, index) => {
              const target = step.configId === current.target;
              return (
                <div
                  key={step.configId}
                  className={cn(
                    "flex items-center gap-2.5 rounded-xl px-3 py-2 text-sm",
                    target ? "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30" : "bg-muted/60"
                  )}
                >
                  {step.eligible ? (
                    <CircleCheck className={cn("size-4", target ? "text-emerald-500" : "text-muted-foreground")} />
                  ) : (
                    <CircleMinus className="size-4 text-muted-foreground" />
                  )}
                  <span className="w-4 font-mono text-xs text-muted-foreground">{index + 1}</span>
                  <span className={cn("font-medium", !step.eligible && "text-muted-foreground")}>{step.name}</span>
                  <span className="ml-auto truncate text-[11px] text-muted-foreground">
                    {target ? "写入这里" : step.eligible ? "失败时尝试" : step.reason}
                  </span>
                </div>
              );
            })}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
