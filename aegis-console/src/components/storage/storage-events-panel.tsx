"use client";

import { useMemo, useState } from "react";
import {
  ArrowRightLeft,
  CircleCheck,
  CircleX,
  Gauge,
  Layers,
  RefreshCw,
  Settings2,
  Timer,
  TriangleAlert,
  Truck
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { StorageEvent, StorageEventLevel, StoragePool, StorageSource } from "@/lib/api/storage-center";
import { formatDate, formatDateTime, formatRelative } from "@/lib/datetime";
import { useStorageEventsQuery } from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";

const KIND_META: Record<string, { label: string; icon: LucideIcon }> = {
  failover: { label: "切换", icon: ArrowRightLeft },
  backoff: { label: "退避", icon: Timer },
  recovered: { label: "恢复", icon: CircleCheck },
  near_full: { label: "将满", icon: Gauge },
  full: { label: "已满", icon: Gauge },
  exhausted: { label: "不可写", icon: CircleX },
  migration: { label: "迁移", icon: Truck },
  config: { label: "配置", icon: Settings2 }
};

const LEVEL_TONE: Record<StorageEventLevel, string> = {
  info: "bg-sky-500/10 text-sky-600 ring-sky-500/20 dark:text-sky-400",
  warn: "bg-amber-500/10 text-amber-600 ring-amber-500/20 dark:text-amber-400",
  error: "bg-red-500/10 text-red-600 ring-red-500/20 dark:text-red-400"
};

export function StorageEventList({ events, compact }: { events: StorageEvent[]; compact?: boolean }) {
  const groups = useMemo(() => {
    const map = new Map<string, StorageEvent[]>();
    for (const event of events) {
      const day = formatDate(event.createdAt);
      if (!map.has(day)) map.set(day, []);
      map.get(day)!.push(event);
    }
    return [...map.entries()];
  }, [events]);

  if (events.length === 0) {
    return <div className="py-10 text-center text-xs text-muted-foreground">暂无事件</div>;
  }

  return (
    <div className="space-y-4">
      {groups.map(([day, items]) => (
        <div key={day} className="space-y-1">
          {!compact ? <div className="px-1 text-[11px] font-medium text-muted-foreground">{day}</div> : null}
          <ol className="relative space-y-1">
            {items.map((event) => {
              const meta = KIND_META[event.kind] ?? { label: event.kind, icon: TriangleAlert };
              const Icon = meta.icon;
              return (
                <li key={event.id} className="flex items-start gap-3 rounded-xl px-2 py-2 hover:bg-muted/50">
                  <span className={cn("mt-0.5 grid size-7 shrink-0 place-items-center rounded-lg ring-1 ring-inset", LEVEL_TONE[event.level])}>
                    <Icon className="size-3.5" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="text-[11px] font-semibold">{meta.label}</span>
                      {event.poolName ? (
                        <Badge variant="outline" size="sm" className="gap-1">
                          <Layers className="size-3" />
                          {event.poolName}
                        </Badge>
                      ) : null}
                      {event.configName ? (
                        <Badge variant="outline" size="sm">
                          {event.configName}
                        </Badge>
                      ) : null}
                    </div>
                    <div className="mt-0.5 break-words text-sm">{event.message}</div>
                    {typeof event.detail?.error === "string" && event.detail.error ? (
                      <div className="mt-0.5 break-all font-mono text-[11px] text-muted-foreground">{event.detail.error}</div>
                    ) : null}
                  </div>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{formatRelative(event.createdAt)}</span>
                    </TooltipTrigger>
                    <TooltipContent>{formatDateTime(event.createdAt)}</TooltipContent>
                  </Tooltip>
                </li>
              );
            })}
          </ol>
        </div>
      ))}
    </div>
  );
}

export function StorageEventsPanel({ pools, sources }: { pools: StoragePool[]; sources: StorageSource[] }) {
  const [level, setLevel] = useState<"all" | StorageEventLevel>("all");
  const [poolId, setPoolId] = useState<string>("all");
  const [configId, setConfigId] = useState<string>("all");
  const query = useStorageEventsQuery({
    level: level === "all" ? undefined : level,
    poolId: poolId === "all" ? undefined : Number(poolId),
    configId: configId === "all" ? undefined : Number(configId),
    limit: 200
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={level}
          onValueChange={(value) => value && setLevel(value as typeof level)}
        >
          <ToggleGroupItem value="all" className="px-3 text-xs">
            全部
          </ToggleGroupItem>
          <ToggleGroupItem value="error" className="px-3 text-xs">
            错误
          </ToggleGroupItem>
          <ToggleGroupItem value="warn" className="px-3 text-xs">
            警告
          </ToggleGroupItem>
          <ToggleGroupItem value="info" className="px-3 text-xs">
            信息
          </ToggleGroupItem>
        </ToggleGroup>
        <Select value={poolId} onValueChange={setPoolId}>
          <SelectTrigger size="sm" className="w-40 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部存储池</SelectItem>
            {pools.map((pool) => (
              <SelectItem key={pool.id} value={String(pool.id)}>
                {pool.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={configId} onValueChange={setConfigId}>
          <SelectTrigger size="sm" className="w-40 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部存储源</SelectItem>
            {sources.map((source) => (
              <SelectItem key={source.id} value={String(source.id)}>
                {source.config_name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button size="sm" variant="ghost" className="ml-auto" onClick={() => query.refetch()} disabled={query.isFetching}>
          <RefreshCw className={cn("size-3.5", query.isFetching && "animate-spin")} />
          刷新
        </Button>
      </div>
      <div className="rounded-2xl border bg-card p-2">
        {query.isLoading ? (
          <div className="space-y-2 p-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-12 rounded-xl" />
            ))}
          </div>
        ) : (
          <StorageEventList events={query.data ?? []} />
        )}
      </div>
    </div>
  );
}
