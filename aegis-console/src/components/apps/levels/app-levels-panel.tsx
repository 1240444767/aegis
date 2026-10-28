"use client";

import { useState } from "react";
import { Gauge, Gift, GitCommitVertical, History, Milestone, Users, Waypoints } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { LevelEditor, type LevelEditorSeed } from "@/components/apps/levels/level-editor";
import { LevelOverview } from "@/components/apps/levels/level-overview";
import { LevelEventsPanel, LevelRewardsPanel } from "@/components/apps/levels/level-records-panels";
import { LevelSchemePanel } from "@/components/apps/levels/level-scheme-panel";
import { LevelUsersPanel } from "@/components/apps/levels/level-users-panel";
import { LevelVersionsPanel } from "@/components/apps/levels/level-versions-panel";
import { TooltipProvider } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 等级体系区块（docs/levels.md）。所有配置都在画布里：等级、门槛、晋级条件、达成奖励、等级权益、升级通知。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 体系 | 当前草稿逐级展开、发布状态与问题；打开画布编辑、推演、发布 |
 * | 总览 | 每级多少人、升级与奖励趋势、待领取 / 待处理 / 失败 |
 * | 用户等级 | 谁在几级；详情里看权益、奖励与升级记录，补判或调级 |
 * | 升级记录 | 谁在什么时候因为什么升级 |
 * | 奖励记录 | 达成奖励的发放状态；重试失败、标记人工物品 |
 * | 版本与重算 | 发布历史（查看、取回草稿），全体重算任务 |
 *
 * 视图状态不进 URL：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "scheme", label: "体系", icon: Waypoints },
  { key: "overview", label: "总览", icon: Gauge },
  { key: "users", label: "用户等级", icon: Users },
  { key: "events", label: "升级记录", icon: History },
  { key: "rewards", label: "奖励记录", icon: Gift },
  { key: "versions", label: "版本与重算", icon: GitCommitVertical }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppLevelsPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("scheme");
  const [editorOpen, setEditorOpen] = useState(false);
  const [seed, setSeed] = useState<LevelEditorSeed | null>(null);
  const [rewardStatus, setRewardStatus] = useState("");
  const [userLevel, setUserLevel] = useState<number | undefined>(undefined);
  const [viewKey, setViewKey] = useState(0);

  if (!appKey) {
    return <NoAppSelected icon={<Milestone className="size-5" />} />;
  }

  const go = (next: ViewKey) => {
    setViewKey((key) => key + 1);
    setView(next);
  };

  return (
    <TooltipProvider delayDuration={300}>
      <div className="space-y-4">
        <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
          {VIEWS.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => {
                if (item.key === "rewards") setRewardStatus("");
                if (item.key === "users") setUserLevel(undefined);
                go(item.key);
              }}
              className={cn(
                "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <item.icon className="size-3.5" />
              {item.label}
            </button>
          ))}
        </div>

        {view === "scheme" ? (
          <LevelSchemePanel
            appKey={appKey}
            onOpenEditor={() => {
              setSeed(null);
              setEditorOpen(true);
            }}
          />
        ) : null}
        {view === "overview" ? (
          <LevelOverview
            appKey={appKey}
            onOpenRewards={(status) => {
              setRewardStatus(status);
              go("rewards");
            }}
            onOpenLevel={(level) => {
              setUserLevel(level);
              go("users");
            }}
          />
        ) : null}
        {view === "users" ? <LevelUsersPanel key={viewKey} appKey={appKey} initialLevel={userLevel} /> : null}
        {view === "events" ? <LevelEventsPanel appKey={appKey} /> : null}
        {view === "rewards" ? <LevelRewardsPanel key={viewKey} appKey={appKey} initialStatus={rewardStatus} /> : null}
        {view === "versions" ? (
          <LevelVersionsPanel
            appKey={appKey}
            onOpenGraph={(graph, from) => {
              setSeed({ graph, from });
              setEditorOpen(true);
            }}
          />
        ) : null}
      </div>
      <LevelEditor
        appKey={appKey}
        open={editorOpen}
        seed={seed}
        onOpenChange={(open) => {
          setEditorOpen(open);
          if (!open) setSeed(null);
        }}
      />
    </TooltipProvider>
  );
}
