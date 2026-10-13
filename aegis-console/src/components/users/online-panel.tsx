"use client";

import { useState } from "react";
import { AppWindow, LayoutDashboard, ShieldCheck } from "lucide-react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useAdminAppsQuery, useAdminSessionQuery } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { OnlineLiveProvider, useLive, useLiveFocus } from "@/lib/online-live/hooks";
import { AdminSessionsView } from "./online/admin-sessions-view";
import { AppOnlineView } from "./online/app-online-view";
import { AppRanking } from "./online/app-ranking";
import { LiveFeed } from "./online/live-feed";
import { LiveStatusBadge } from "./online/live-shared";
import { NodeLoad } from "./online/node-load";
import { OnlineDistribution } from "./online/online-distribution";
import { OnlineOverview } from "./online/online-overview";
import { OnlineTrend } from "./online/online-trend";

type View = "overview" | "apps" | "admins";

/**
 * 在线会话。
 *
 * 整页数据来自一条加密 WebSocket（/api/admin/system/online/live）：快照约两秒一份、
 * 上下线动态半秒一批，不再轮询。三个视图：
 *
 *   总览     —— 全站（或可见应用）总量、在线趋势、实时动态、应用排行、连接分布、接入节点
 *   应用用户 —— 单个应用的在线用户，可展开到每条连接、可强制下线
 *   管理员   —— 此刻开着控制台的管理员与全部登录会话（仅超级管理员）
 */
export function OnlinePanel() {
  return (
    <OnlineLiveProvider>
      <OnlineWorkspace />
    </OnlineLiveProvider>
  );
}

function OnlineWorkspace() {
  const operator = useAuthStore((s) => s.operator);
  const selfId = operator?.id != null ? Number(operator.id) : null;
  // 以服务端 ready 帧为准；连上之前先按本地资料显示入口
  const isSuperAdmin = useLive((s) => s.ready?.admin.isSuperAdmin ?? operator?.isSuperAdmin ?? false);

  const [view, setView] = useState<View>("overview");
  const [appId, setAppId] = useState<number | null>(null);
  const appsQuery = useAdminAppsQuery();
  const sessionQuery = useAdminSessionQuery();
  const activeView: View = view === "admins" && !isSuperAdmin ? "overview" : view;
  const adminCount = useLive((s) => s.snapshot?.admins?.admins ?? 0);

  function openApp(id: number) {
    setAppId(id);
    setView("apps");
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <ToggleGroup
          type="single"
          value={activeView}
          onValueChange={(v) => v && setView(v as View)}
          className="rounded-lg border bg-muted/40 p-0.5"
        >
          <ToggleGroupItem value="overview" className="h-8 gap-1.5 rounded-md px-3 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">
            <LayoutDashboard className="size-3.5" />
            总览
          </ToggleGroupItem>
          <ToggleGroupItem value="apps" className="h-8 gap-1.5 rounded-md px-3 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">
            <AppWindow className="size-3.5" />
            应用用户
          </ToggleGroupItem>
          {isSuperAdmin ? (
            <ToggleGroupItem value="admins" className="h-8 gap-1.5 rounded-md px-3 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">
              <ShieldCheck className="size-3.5" />
              管理员
              {adminCount > 0 ? (
                <span className="rounded-full bg-emerald-500/15 px-1.5 text-[10px] leading-4 text-emerald-700 tabular-nums dark:text-emerald-300">
                  {adminCount}
                </span>
              ) : null}
            </ToggleGroupItem>
          ) : null}
        </ToggleGroup>
        <LiveStatusBadge />
      </div>

      {activeView === "overview" ? (
        <OverviewView onOpenApp={openApp} />
      ) : activeView === "apps" ? (
        <AppOnlineView apps={appsQuery.data ?? []} appsLoading={appsQuery.isLoading} appId={appId} onSelectApp={setAppId} />
      ) : (
        <AdminsView currentSessionId={sessionQuery.data?.tokenId} selfId={selfId} />
      )}
    </div>
  );
}

function OverviewView({ onOpenApp }: { onOpenApp: (appId: number) => void }) {
  useLiveFocus(0);
  const dims = useLive((s) => s.snapshot?.dims);
  return (
    <div className="space-y-3">
      <OnlineOverview />
      <div className="grid gap-3 lg:grid-cols-3">
        <OnlineTrend app={0} className="lg:col-span-2" />
        <LiveFeed />
      </div>
      <div className="grid gap-3 lg:grid-cols-2">
        <AppRanking onOpen={onOpenApp} />
        <OnlineDistribution dims={dims} />
      </div>
      <NodeLoad />
    </div>
  );
}

function AdminsView({ currentSessionId, selfId }: { currentSessionId?: string; selfId: number | null }) {
  useLiveFocus(0);
  return <AdminSessionsView currentSessionId={currentSessionId} selfId={selfId} />;
}
