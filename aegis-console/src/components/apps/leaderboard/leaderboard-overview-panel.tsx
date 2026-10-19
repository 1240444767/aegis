"use client";

import Link from "next/link";
import {
  AlertTriangle,
  ChevronRight,
  Hourglass,
  ListOrdered,
  SlidersHorizontal,
  Trophy,
  UserMinus,
  XCircle
} from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import { formatCount } from "@/components/apps/invite/invite-shared";
import { LeaderboardValue, isPeriodic } from "@/components/apps/leaderboard/leaderboard-shared";
import { StatTile } from "@/components/content/content-shared";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { LeaderboardAdminEntry, LeaderboardBoardSpec, LeaderboardBoardStats } from "@/lib/api/leaderboard";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useLeaderboardSettingsQuery, useLeaderboardStatsQuery } from "@/lib/leaderboard-hooks";

/**
 * 概览。各榜的参与人数与前 3 名是本期实时算的（未开启的榜不算）；待发与失败的奖励数跨全部已结算的期，
 * 失败的需要人处理，单独一条提示点进结算记录。
 */
export function LeaderboardOverviewPanel({
  appKey,
  onOpenSettings,
  onOpenBoard,
  onOpenExclusions,
  onOpenSnapshots
}: {
  appKey: string;
  onOpenSettings: () => void;
  onOpenBoard: (key: string) => void;
  onOpenExclusions: () => void;
  onOpenSnapshots: () => void;
}) {
  const settingsQuery = useLeaderboardSettingsQuery(appKey);
  const statsQuery = useLeaderboardStatsQuery(appKey);

  const stats = statsQuery.data;
  const settings = settingsQuery.data?.settings;
  const catalog = settingsQuery.data?.catalog ?? [];
  const boards = stats?.boards ?? [];
  const enabledCount = boards.filter((board) => board.enabled).length;
  const value = (count?: number) => (stats ? formatCount(count) : "—");

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border px-4 py-3">
        {statsQuery.isLoading ? (
          <Skeleton className="h-4 w-28" />
        ) : (
          <div className="flex items-center gap-2">
            <StatusDot active={Boolean(stats?.enabled)} labelActive="排行榜已开启" labelInactive="排行榜未开启" />
            {settings && !settings.configured ? (
              <Badge variant="outline" size="sm">
                未配置
              </Badge>
            ) : null}
          </div>
        )}
        <Button size="sm" variant="outline" className="h-8" onClick={onOpenSettings}>
          <SlidersHorizontal className="size-3.5" /> 设置
        </Button>
      </div>

      {stats && (stats.failedRewards > 0 || stats.pendingRewards > 0) ? (
        <div className="flex flex-col gap-2 sm:flex-row">
          {stats.failedRewards > 0 ? (
            <AttentionButton
              icon={AlertTriangle}
              tone="danger"
              label={`${formatCount(stats.failedRewards)} 份奖励发放失败`}
              onClick={onOpenSnapshots}
            />
          ) : null}
          {stats.pendingRewards > 0 ? (
            <AttentionButton
              icon={Hourglass}
              tone="warning"
              label={`${formatCount(stats.pendingRewards)} 份奖励等待发放`}
              onClick={onOpenSnapshots}
            />
          ) : null}
        </div>
      ) : null}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile
          label="开启的榜单"
          icon={Trophy}
          value={stats ? `${enabledCount}/${boards.length}` : "—"}
        />
        <button type="button" className="text-left" onClick={onOpenExclusions}>
          <StatTile
            label="移出人数"
            icon={UserMinus}
            tone={stats?.exclusions ? "default" : "muted"}
            value={value(stats?.exclusions)}
          />
        </button>
        <button type="button" className="text-left" onClick={onOpenSnapshots} disabled={!stats?.pendingRewards}>
          <StatTile
            label="待发奖励"
            icon={Hourglass}
            tone={stats?.pendingRewards ? "default" : "muted"}
            value={value(stats?.pendingRewards)}
          />
        </button>
        <button type="button" className="text-left" onClick={onOpenSnapshots} disabled={!stats?.failedRewards}>
          <StatTile
            label="发放失败"
            icon={XCircle}
            tone={stats?.failedRewards ? "default" : "muted"}
            value={
              stats?.failedRewards ? (
                <span className="text-red-600 dark:text-red-400">{formatCount(stats.failedRewards)}</span>
              ) : (
                value(stats?.failedRewards)
              )
            }
          />
        </button>
      </div>

      {statsQuery.isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-56 w-full rounded-xl" />
          ))}
        </div>
      ) : boards.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          暂无榜单
        </p>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {boards.map((board) => (
            <BoardCard
              key={board.key}
              appKey={appKey}
              board={board}
              spec={catalog.find((item) => item.key === board.key)}
              onOpen={() => onOpenBoard(board.key)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function BoardCard({
  appKey,
  board,
  spec,
  onOpen
}: {
  appKey: string;
  board: LeaderboardBoardStats;
  spec?: LeaderboardBoardSpec;
  onOpen: () => void;
}) {
  const periodic = isPeriodic(spec);
  return (
    <div className="flex flex-col rounded-xl border border-border bg-card">
      <div className="flex items-start justify-between gap-2 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">{board.label}</span>
            {board.enabled ? null : (
              <Badge variant="secondary" size="sm">
                未开启
              </Badge>
            )}
          </div>
          <p className="mt-0.5 text-[11px] text-muted-foreground">
            {board.periodLabel}
            {periodic ? <span className="ml-1.5 font-mono">{board.periodKey}</span> : null}
          </p>
        </div>
        <div className="text-right">
          <div className="text-lg font-semibold tabular-nums">{board.enabled ? formatCount(board.participants) : "—"}</div>
          <div className="text-[11px] text-muted-foreground">参与人数</div>
        </div>
      </div>

      <div className="flex-1 px-4 py-3">
        {!board.enabled ? (
          <p className="py-4 text-center text-xs text-muted-foreground">榜单未开启，不计算名次</p>
        ) : board.top.length === 0 ? (
          <p className="py-4 text-center text-xs text-muted-foreground">暂无人上榜</p>
        ) : (
          <ol className="space-y-2">
            {board.top.map((entry) => (
              <TopEntry key={entry.userId} appKey={appKey} entry={entry} unit={spec?.valueUnit} />
            ))}
          </ol>
        )}
      </div>

      <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-2.5">
        <span className="text-[11px] text-muted-foreground">
          {periodic
            ? board.lastSettledAt
              ? `上次结算 ${formatDateTimeMinute(board.lastSettledAt)}`
              : "尚未结算"
            : "总榜不结算"}
        </span>
        <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={onOpen}>
          <ListOrdered className="size-3.5" /> 完整榜单
        </Button>
      </div>
    </div>
  );
}

function TopEntry({ appKey, entry, unit }: { appKey: string; entry: LeaderboardAdminEntry; unit?: string }) {
  const title = entry.nickname || entry.name || entry.account || `#${entry.userId}`;
  return (
    <li className="flex items-center gap-2.5">
      <span className="w-5 shrink-0 text-center text-xs font-semibold text-muted-foreground tabular-nums">
        {entry.rank ?? "—"}
      </span>
      <Link
        href={`/app-users/${encodeURIComponent(appKey)}/${entry.userId}`}
        className="group flex min-w-0 flex-1 items-center gap-2"
      >
        <Avatar className="size-7 rounded-lg border">
          <AvatarImage src={entry.avatar || undefined} alt={title} />
          <AvatarFallback className="rounded-lg text-[10px]">{title.slice(0, 2)}</AvatarFallback>
        </Avatar>
        <span className="min-w-0">
          <span className="flex items-center gap-1.5">
            <span className="truncate text-xs font-medium group-hover:underline">{title}</span>
            {entry.anonymous ? (
              <Badge variant="outline" size="sm" className="font-normal">
                匿名
              </Badge>
            ) : null}
            {entry.excluded ? (
              <Badge variant="danger" size="sm">
                已移出
              </Badge>
            ) : null}
          </span>
          {entry.account && entry.account !== title ? (
            <span className="block truncate font-mono text-[11px] text-muted-foreground">{entry.account}</span>
          ) : null}
        </span>
      </Link>
      <span className="shrink-0 text-xs">
        <LeaderboardValue value={entry.value} unit={unit} levelName={entry.levelName} />
      </span>
    </li>
  );
}

function AttentionButton({
  icon: Icon,
  tone,
  label,
  onClick
}: {
  icon: typeof AlertTriangle;
  tone: "warning" | "danger";
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        tone === "warning"
          ? "flex flex-1 items-center gap-2.5 rounded-xl border border-amber-200 bg-amber-50/60 px-4 py-2.5 text-left text-sm text-amber-800 transition-colors hover:bg-amber-100/70 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300 dark:hover:bg-amber-950/50"
          : "flex flex-1 items-center gap-2.5 rounded-xl border border-red-200 bg-red-50/60 px-4 py-2.5 text-left text-sm text-red-700 transition-colors hover:bg-red-100/70 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300 dark:hover:bg-red-950/50"
      }
    >
      <Icon className="size-4 shrink-0" />
      <span className="flex-1 font-medium">{label}</span>
      <ChevronRight className="size-4 shrink-0 opacity-60" />
    </button>
  );
}
