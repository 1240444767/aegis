"use client";

import { Server } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useLive } from "@/lib/online-live/hooks";
import { cn } from "@/lib/utils";
import { PanelHeader, formatCount } from "./live-shared";
import { shortId } from "./session-shared";

/** 接入节点负载（仅超级管理员）：每个服务实例上的用户连接与控制台连接。 */
export function NodeLoad({ className }: { className?: string }) {
  const nodes = useLive((s) => s.snapshot?.nodes);
  const current = useLive((s) => s.ready?.node);
  if (!nodes) return null;
  const total = nodes.reduce((sum, n) => sum + n.connections, 0);

  return (
    <div className={cn("rounded-2xl border bg-card", className)}>
      <PanelHeader
        title="接入节点"
        icon={<Server className="size-4 text-muted-foreground" />}
        extra={<span className="text-xs text-muted-foreground tabular-nums">{nodes.length} 个实例</span>}
      />
      {nodes.length === 0 ? (
        <div className="px-4 py-6 text-center text-xs text-muted-foreground">暂无承载连接的实例</div>
      ) : (
        <ul className="grid gap-px bg-border sm:grid-cols-2 xl:grid-cols-3">
          {nodes.map((n) => {
            const share = total > 0 ? n.connections / total : 0;
            return (
              <li key={n.serverId} className="min-w-0 bg-card px-4 py-3">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate font-mono text-xs" title={n.serverId}>{shortId(n.serverId, 8, 4)}</span>
                  {n.serverId === current ? <Badge variant="outline" size="sm">当前连接</Badge> : null}
                </div>
                <div className="mt-1.5 flex items-baseline gap-3 text-xs text-muted-foreground">
                  <span><span className="text-base font-semibold text-foreground tabular-nums">{formatCount(n.connections)}</span> 用户连接</span>
                  <span className="tabular-nums">{formatCount(n.admins)} 控制台</span>
                </div>
                <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
                  <div className="h-full rounded-full bg-blue-600 dark:bg-blue-500" style={{ width: `${Math.max(share * 100, 1)}%` }} />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
