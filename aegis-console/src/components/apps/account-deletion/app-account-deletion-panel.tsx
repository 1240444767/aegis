"use client";

import { useState } from "react";
import { BarChart3, ListChecks, Lock, SlidersHorizontal, UserX } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { DeletionConfigPanel } from "@/components/apps/account-deletion/account-deletion-config-panel";
import { DeletionOverviewPanel } from "@/components/apps/account-deletion/account-deletion-overview-panel";
import { DeletionRequestsPanel } from "@/components/apps/account-deletion/account-deletion-requests-panel";
import { DeletionTombstonesPanel } from "@/components/apps/account-deletion/account-deletion-tombstones-panel";
import type { DeletionStatus } from "@/lib/api/account-deletion";
import { cn } from "@/lib/utils";

/**
 * 账号注销区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 开没开、有多少申请、几条快到期、挽留了多少、有没有要处理的（待审核 / 执行失败） |
 * | 申请 | 每条申请的进度、提交时的账号快照与前置条件；在这里批准、驳回、撤销、立即执行、改期 |
 * | 配置 | 注销方式、审核、验证、原因、须知、前置条件、冷静期行为、重新注册限制 |
 * | 重新注册限制 | 注销后被拦着不能再注册的账号名、邮箱、手机号；在这里解除 |
 *
 * 视图状态不进 URL，理由同邀请区块：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "requests", label: "申请", icon: ListChecks },
  { key: "config", label: "配置", icon: SlidersHorizontal },
  { key: "tombstones", label: "重新注册限制", icon: Lock }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppAccountDeletionPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  // 从概览跳到申请列表时带过去的状态筛选
  const [statusFilter, setStatusFilter] = useState<DeletionStatus | undefined>(undefined);

  if (!appKey) {
    return <NoAppSelected icon={<UserX className="size-5" />} />;
  }

  const openRequests = (status?: DeletionStatus) => {
    setStatusFilter(status);
    setView("requests");
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
        <DeletionOverviewPanel
          appKey={appKey}
          onOpenConfig={() => setView("config")}
          onOpenRequests={openRequests}
          onOpenTombstones={() => setView("tombstones")}
        />
      ) : null}
      {view === "requests" ? (
        <DeletionRequestsPanel appKey={appKey} status={statusFilter} onStatusChange={setStatusFilter} />
      ) : null}
      {view === "config" ? <DeletionConfigPanel appKey={appKey} /> : null}
      {view === "tombstones" ? <DeletionTombstonesPanel appKey={appKey} /> : null}
    </div>
  );
}
