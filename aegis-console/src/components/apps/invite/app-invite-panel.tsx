"use client";

import { useState } from "react";
import { BarChart3, ListTree, SlidersHorizontal, TicketCheck, UserPlus } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { InviteCodesPanel } from "@/components/apps/invite/invite-codes-panel";
import { InviteConfigPanel } from "@/components/apps/invite/invite-config-panel";
import { InviteOverviewPanel } from "@/components/apps/invite/invite-overview-panel";
import { InviteRelationsPanel, type InviteRelationScope } from "@/components/apps/invite/invite-relations-panel";
import type { InviterStatus } from "@/lib/api/invite";
import { cn } from "@/lib/utils";

/**
 * 邀请区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 开没开、每天多少人填写邀请码、发了多少邀请人奖励、谁邀请得最多、哪个活动码最好用 |
 * | 配置 | 注册表单、个人码格式、邀请人资格、双方奖励与阶梯、防刷、文案 |
 * | 邀请码 | 个人码与活动码：使用次数、有效期、启停；活动码在这里创建与编辑 |
 * | 邀请记录 | 谁填了谁的码、双方实际拿到什么；等待中的邀请人奖励在这里发放或取消 |
 *
 * 视图状态不进 URL，理由同积分商城：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "config", label: "配置", icon: SlidersHorizontal },
  { key: "codes", label: "邀请码", icon: TicketCheck },
  { key: "relations", label: "邀请记录", icon: ListTree }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppInvitePanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  // 从概览或邀请码跳到邀请记录时带过去的筛选条件
  const [scope, setScope] = useState<InviteRelationScope>({});
  const [statusFilter, setStatusFilter] = useState<InviterStatus | undefined>(undefined);

  if (!appKey) {
    return <NoAppSelected icon={<UserPlus className="size-5" />} />;
  }

  const openRelations = (next: InviteRelationScope, status?: InviterStatus) => {
    setScope(next);
    setStatusFilter(status);
    setView("relations");
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setView(item.key)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
              view === item.key
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
          </button>
        ))}
      </div>

      {view === "overview" ? (
        <InviteOverviewPanel
          appKey={appKey}
          onOpenConfig={() => setView("config")}
          onOpenRelations={openRelations}
        />
      ) : null}
      {view === "config" ? <InviteConfigPanel appKey={appKey} /> : null}
      {view === "codes" ? (
        <InviteCodesPanel appKey={appKey} onOpenRelations={(code) => openRelations({ code })} />
      ) : null}
      {view === "relations" ? (
        <InviteRelationsPanel
          appKey={appKey}
          scope={scope}
          status={statusFilter}
          onScopeChange={setScope}
          onStatusChange={setStatusFilter}
        />
      ) : null}
    </div>
  );
}
