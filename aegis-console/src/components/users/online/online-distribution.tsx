"use client";

import { useState } from "react";
import { PieChart } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { LiveDimensionKey, LiveDimensions } from "@/lib/online-live/types";
import { cn } from "@/lib/utils";
import { DIMENSION_TABS, PanelHeader, bucketLabel, formatCount } from "./live-shared";

/**
 * 在线连接的分布（按连接计，同一用户多台设备分别计入）。
 * 条形为占比，单一色相；「未上报」与「其他」排在末尾并用中性色。
 */
export function OnlineDistribution({ dims, className }: { dims?: LiveDimensions; className?: string }) {
  const [tab, setTab] = useState<LiveDimensionKey>("platform");
  const buckets = dims?.[tab] ?? [];
  const total = buckets.reduce((sum, b) => sum + b.count, 0);

  return (
    <div className={cn("flex min-w-0 flex-col rounded-2xl border bg-card", className)}>
      <PanelHeader title="连接分布" icon={<PieChart className="size-4 text-muted-foreground" />} />
      <div className="overflow-x-auto px-4 pt-3 [scrollbar-width:none]">
        <ToggleGroup
          type="single"
          value={tab}
          onValueChange={(v) => v && setTab(v as LiveDimensionKey)}
          className="w-max rounded-lg border bg-muted/40 p-0.5"
        >
          {DIMENSION_TABS.map((d) => (
            <ToggleGroupItem
              key={d.key}
              value={d.key}
              className="h-7 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm"
            >
              {d.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      {!dims ? (
        <div className="space-y-3 p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-7 w-full rounded-lg" />
          ))}
        </div>
      ) : buckets.length === 0 ? (
        <div className="flex min-h-40 items-center justify-center text-xs text-muted-foreground">暂无在线连接</div>
      ) : (
        <ul className="space-y-2.5 px-4 pt-3 pb-4">
          {buckets.map((b) => {
            const share = total > 0 ? b.count / total : 0;
            const muted = b.key === "" || b.key === "*";
            return (
              <li key={b.key} className="min-w-0">
                <div className="flex items-baseline justify-between gap-3 text-xs">
                  <span className={cn("truncate", muted ? "text-muted-foreground" : "font-medium")}>{bucketLabel(b.key)}</span>
                  <span className="shrink-0 text-muted-foreground tabular-nums">
                    <span className="text-foreground">{formatCount(b.count)}</span>
                    <span className="ml-2 inline-block w-11 text-right">{(share * 100).toFixed(share < 0.1 ? 1 : 0)}%</span>
                  </span>
                </div>
                <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted">
                  <div
                    className={cn("h-full rounded-full", muted ? "bg-muted-foreground/40" : "bg-blue-600 dark:bg-blue-500")}
                    style={{ width: `${Math.max(share * 100, 1)}%` }}
                  />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
