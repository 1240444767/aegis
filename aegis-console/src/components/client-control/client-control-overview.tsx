"use client";

import { useState } from "react";
import { Ban, Boxes, Layers, ListChecks, Smartphone, Users } from "lucide-react";
import type { ClientControlOverview as Overview } from "@/lib/api/client-control";
import { StatTile } from "@/components/content/content-shared";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { CONTROL_ACTIONS, DATA_FONT, formatSeconds } from "./client-control-shared";

export function ClientControlOverview({ overview, loading }: { overview?: Overview; loading: boolean }) {
  if (loading || !overview) {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className="h-[84px] rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-44 rounded-xl" />
      </div>
    );
  }
  const settings = overview.settings;
  const breakdownTotal = Object.values(overview.actionBreakdown ?? {}).reduce((sum, value) => sum + value, 0);

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
        <StatTile label="24 小时设备" icon={Smartphone} value={overview.devices24h} hint={`7 天 ${overview.devices7d}`} />
        <StatTile label="30 天设备" icon={Users} value={overview.devices30d} />
        <StatTile label="7 天被拦" icon={Ban} tone={overview.blocked7d ? "default" : "muted"} value={overview.blocked7d} hint="停用与强制更新" />
        <StatTile label="包名" icon={Boxes} value={overview.packages} hint="近 30 天" />
        <StatTile label="版本" icon={Layers} value={overview.versions} hint="近 30 天" />
        <StatTile label="生效规则" icon={ListChecks} value={overview.activeRules} hint={`共 ${overview.rules} 条`} />
      </div>

      <div className="grid gap-3 lg:grid-cols-[1.6fr_1fr]">
        <DailyDevices daily={overview.daily} />

        <div className="space-y-3">
          <div className="rounded-xl border bg-card">
            <div className="border-b px-4 py-2 text-xs font-medium text-muted-foreground">近 7 天设备的最近裁决</div>
            <div className="space-y-2.5 px-4 py-3">
              {CONTROL_ACTIONS.map((action) => {
                const value = overview.actionBreakdown?.[action.value] ?? 0;
                const pct = breakdownTotal ? Math.round((value / breakdownTotal) * 100) : 0;
                return (
                  <div key={action.value} className="flex items-center gap-3 text-sm">
                    <span className="w-16 shrink-0 text-muted-foreground">{action.label}</span>
                    <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                      <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
                    </div>
                    <span className={cn("w-12 shrink-0 text-right text-xs", DATA_FONT)}>{value}</span>
                  </div>
                );
              })}
            </div>
          </div>
          <div className="rounded-xl border bg-card px-4 py-3 text-sm">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">策略版本</span>
              <span className={DATA_FONT}>{settings.policyVersion}</span>
            </div>
            <div className="mt-1.5 flex items-center justify-between">
              <span className="text-muted-foreground">复查间隔</span>
              <span>{formatSeconds(settings.recheckSeconds)}</span>
            </div>
            <div className="mt-1.5 flex items-center justify-between">
              <span className="text-muted-foreground">放行裁决有效期</span>
              <span>{formatSeconds(settings.verdictTtlSeconds)}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

/** 近 14 天每天上报的设备数（单一序列）；悬停显示当天的被拦次数。 */
function DailyDevices({ daily }: { daily: Overview["daily"] }) {
  const [hover, setHover] = useState<number | null>(null);
  const max = Math.max(1, ...daily.map((item) => item.devices));
  const active = hover !== null ? daily[hover] : null;
  return (
    <div className="rounded-xl border bg-card">
      <div className="flex items-center justify-between border-b px-4 py-2 text-xs">
        <span className="font-medium text-muted-foreground">近 14 天上报设备</span>
        {active ? (
          <span className="text-muted-foreground">
            {active.day.slice(5)} 设备 <span className={cn("text-foreground", DATA_FONT)}>{active.devices}</span>
            {"  "}被拦 <span className={cn("text-foreground", DATA_FONT)}>{active.blocked}</span>
          </span>
        ) : null}
      </div>
      <div className="px-4 pb-2 pt-4">
        <div className="flex h-32 items-end gap-0.5" onMouseLeave={() => setHover(null)}>
          {daily.map((item, index) => (
            <div
              key={item.day}
              className="flex h-full flex-1 cursor-default items-end"
              onMouseEnter={() => setHover(index)}
              aria-label={`${item.day} 设备 ${item.devices}，被拦 ${item.blocked}`}
            >
              <div
                className={cn(
                  "w-full rounded-t-[4px] bg-primary transition-colors",
                  hover !== null && hover !== index && "bg-primary/40"
                )}
                style={{ height: `${Math.max(item.devices ? 3 : 0, (item.devices / max) * 100)}%` }}
              />
            </div>
          ))}
        </div>
        <div className="mt-1.5 flex justify-between border-t pt-1 text-[11px] text-muted-foreground">
          <span>{daily[0]?.day.slice(5)}</span>
          <span>{daily[daily.length - 1]?.day.slice(5)}</span>
        </div>
      </div>
    </div>
  );
}
