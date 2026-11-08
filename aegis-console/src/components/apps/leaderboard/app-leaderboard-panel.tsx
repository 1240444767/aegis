"use client";

import { useState } from "react";
import { BarChart3, Gift, History, ListOrdered, SlidersHorizontal, Trophy, UserMinus } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { LeaderboardBoardPanel } from "@/components/apps/leaderboard/leaderboard-board-panel";
import { LeaderboardExclusionsPanel } from "@/components/apps/leaderboard/leaderboard-exclusions-panel";
import { LeaderboardOverviewPanel } from "@/components/apps/leaderboard/leaderboard-overview-panel";
import { LeaderboardRewardsPanel } from "@/components/apps/leaderboard/leaderboard-rewards-panel";
import { LeaderboardSettingsPanel } from "@/components/apps/leaderboard/leaderboard-settings-panel";
import { LeaderboardSnapshotsPanel } from "@/components/apps/leaderboard/leaderboard-snapshots-panel";
import { cn } from "@/lib/utils";

/**
 * 排行榜区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 开没开、各榜本期多少人、前 3 名是谁、上次什么时候结算、有没有待发或失败的奖励 |
 * | 设置 | 开哪些榜、标签顺序、展示多少名、上榜门槛、展示身份、匿名与管理员调整的口径 |
 * | 奖励 | 日榜、周榜、月榜按名次档位发什么 |
 * | 实时榜 | 某个榜本期或某一期的完整名次（带真实账号），可把人移出排行榜 |
 * | 移出名单 | 被移出的人、原因与操作人；批量移出与恢复 |
 * | 结算记录 | 每一期的快照与发放统计、条目明细、重试失败、手动结算 |
 *
 * 视图状态不进 URL，理由同积分商城：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "settings", label: "设置", icon: SlidersHorizontal },
  { key: "rewards", label: "奖励", icon: Gift },
  { key: "board", label: "实时榜", icon: ListOrdered },
  { key: "exclusions", label: "移出名单", icon: UserMinus },
  { key: "snapshots", label: "结算记录", icon: History }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppLeaderboardPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  // 从概览跳到实时榜时带过去的榜单
  const [boardKey, setBoardKey] = useState<string | null>(null);

  if (!appKey) {
    return <NoAppSelected icon={<Trophy className="size-5" />} />;
  }

  const openBoard = (key: string) => {
    setBoardKey(key);
    setView("board");
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
        <LeaderboardOverviewPanel
          appKey={appKey}
          onOpenSettings={() => setView("settings")}
          onOpenBoard={openBoard}
          onOpenExclusions={() => setView("exclusions")}
          onOpenSnapshots={() => setView("snapshots")}
        />
      ) : null}
      {view === "settings" ? <LeaderboardSettingsPanel appKey={appKey} /> : null}
      {view === "rewards" ? <LeaderboardRewardsPanel appKey={appKey} /> : null}
      {view === "board" ? (
        <LeaderboardBoardPanel appKey={appKey} boardKey={boardKey} onBoardChange={setBoardKey} />
      ) : null}
      {view === "exclusions" ? <LeaderboardExclusionsPanel appKey={appKey} /> : null}
      {view === "snapshots" ? <LeaderboardSnapshotsPanel appKey={appKey} /> : null}
    </div>
  );
}
