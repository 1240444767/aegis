"use client";

import type { ReactNode } from "react";
import { AppWindow, Cable, Server, ShieldCheck, Users } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { useLive } from "@/lib/online-live/hooks";
import { cn } from "@/lib/utils";
import { formatCount } from "./live-shared";

type Tile = { key: string; icon: ReactNode; label: string; value?: number; hint: string };

/**
 * 全站在线概览：数字来自实时快照，随推送更新。
 * 在线管理员与接入节点两格只有超级管理员的快照里才有。
 */
export function OnlineOverview() {
  const snapshot = useLive((s) => s.snapshot);
  const scopeAll = useLive((s) => s.ready?.scope.all ?? true);
  const loading = !snapshot;

  const totals = snapshot?.totals;
  const perUser = totals && totals.users > 0 ? totals.connections / totals.users : 0;
  const tiles: Tile[] = [
    { key: "users", icon: <Users className="size-3.5" />, label: "在线用户", value: totals?.users, hint: scopeAll ? "按应用分别计数" : "可见应用合计" },
    { key: "connections", icon: <Cable className="size-3.5" />, label: "实时连接", value: totals?.connections, hint: perUser > 0 ? `人均 ${perUser.toFixed(1)} 条` : "暂无连接" },
    { key: "apps", icon: <AppWindow className="size-3.5" />, label: "活跃应用", value: totals?.apps, hint: "存在在线用户的应用" }
  ];
  if (snapshot?.admins) {
    tiles.push({
      key: "admins",
      icon: <ShieldCheck className="size-3.5" />,
      label: "在线管理员",
      value: snapshot.admins.admins,
      hint: `${formatCount(snapshot.admins.connections)} 个控制台窗口`
    });
  }
  if (snapshot?.nodes) {
    tiles.push({
      key: "nodes",
      icon: <Server className="size-3.5" />,
      label: "接入节点",
      value: snapshot.nodes.length,
      hint: "承载连接的服务实例"
    });
  }

  const columns = loading ? 4 : tiles.length;
  return (
    <div className="overflow-hidden rounded-2xl border bg-card text-card-foreground">
      <div className={cn("grid grid-cols-2", columns >= 5 ? "lg:grid-cols-5" : columns === 4 ? "lg:grid-cols-4" : "lg:grid-cols-3")}>
        {(loading ? placeholderTiles : tiles).map((tile, index) => (
          <div
            key={tile.key}
            className={cn(
              "min-w-0 px-4 py-3.5",
              index % 2 === 1 && "border-l",
              index >= 2 && "border-t lg:border-t-0",
              index >= 1 && "lg:border-l"
            )}
          >
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              {tile.icon}
              {tile.label}
            </div>
            {loading ? (
              <Skeleton className="mt-1.5 h-7 w-14" />
            ) : (
              <div className="mt-1 text-2xl leading-8 font-semibold tracking-tight tabular-nums">{formatCount(tile.value)}</div>
            )}
            <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{loading ? " " : tile.hint}</div>
          </div>
        ))}
      </div>
      {snapshot?.truncated ? (
        <div className="border-t px-4 py-2 text-[11px] text-muted-foreground">连接数超过统计上限，分布按最近活跃的 5 万条连接计算</div>
      ) : null}
    </div>
  );
}

const placeholderTiles: Tile[] = [
  { key: "users", icon: <Users className="size-3.5" />, label: "在线用户", hint: "" },
  { key: "connections", icon: <Cable className="size-3.5" />, label: "实时连接", hint: "" },
  { key: "apps", icon: <AppWindow className="size-3.5" />, label: "活跃应用", hint: "" },
  { key: "admins", icon: <ShieldCheck className="size-3.5" />, label: "在线管理员", hint: "" }
];
