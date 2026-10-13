"use client";

import { useState } from "react";
import { AppWindow, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useLive } from "@/lib/online-live/hooks";
import { cn } from "@/lib/utils";
import { PanelHeader, formatCount } from "./live-shared";

const COLLAPSED = 8;

/**
 * 应用在线排行：按在线人数降序，条形为占全部可见在线人数的比例。点击一行进入该应用的在线用户。
 */
export function AppRanking({ onOpen, className }: { onOpen: (appId: number) => void; className?: string }) {
  const apps = useLive((s) => s.snapshot?.apps);
  const total = useLive((s) => s.snapshot?.totals.users ?? 0);
  const [expanded, setExpanded] = useState(false);

  const rows = apps ?? [];
  const visible = expanded ? rows : rows.slice(0, COLLAPSED);

  return (
    <div className={cn("flex min-w-0 flex-col rounded-2xl border bg-card", className)}>
      <PanelHeader
        title="应用排行"
        icon={<AppWindow className="size-4 text-muted-foreground" />}
        extra={<span className="text-xs text-muted-foreground tabular-nums">{apps ? `${rows.length} 个应用在线` : ""}</span>}
      />
      {!apps ? (
        <div className="space-y-3 p-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-9 w-full rounded-lg" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="flex min-h-40 items-center justify-center text-xs text-muted-foreground">当前没有应用存在在线用户</div>
      ) : (
        <>
          <ol className="divide-y">
            {visible.map((app, index) => {
              const share = total > 0 ? app.users / total : 0;
              return (
                <li key={app.appid}>
                  <button
                    type="button"
                    className="group flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-muted/40"
                    onClick={() => onOpen(app.appid)}
                  >
                    <span className="w-5 shrink-0 text-xs text-muted-foreground tabular-nums">{index + 1}</span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-baseline justify-between gap-3">
                        <span className="truncate text-sm font-medium">{app.name || `应用 #${app.appid}`}</span>
                        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                          <span className="font-semibold text-foreground">{formatCount(app.users)}</span> 人
                          <span className="ml-2">{formatCount(app.connections)} 连接</span>
                        </span>
                      </div>
                      <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
                        <div className="h-full rounded-full bg-blue-600 dark:bg-blue-500" style={{ width: `${Math.max(share * 100, 1.5)}%` }} />
                      </div>
                    </div>
                    <ChevronRight className="size-4 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                  </button>
                </li>
              );
            })}
          </ol>
          {rows.length > COLLAPSED ? (
            <div className="border-t px-4 py-2">
              <Button variant="ghost" size="sm" className="h-7 w-full text-xs" onClick={() => setExpanded(!expanded)}>
                {expanded ? "收起" : `查看全部 ${rows.length} 个应用`}
              </Button>
            </div>
          ) : null}
        </>
      )}
    </div>
  );
}
