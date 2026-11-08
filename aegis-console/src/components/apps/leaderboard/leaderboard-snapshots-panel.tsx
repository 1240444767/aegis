"use client";

import { useState } from "react";
import { ArrowLeft, CalendarCheck, History, Loader2, RotateCw } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { InvitePager, ResultBadges, formatCount } from "@/components/apps/invite/invite-shared";
import {
  DEFAULT_REWARD_STATUSES,
  LeaderboardUser,
  LeaderboardValue,
  PERIOD_KEY_HINT,
  RewardStatusBadge,
  isPeriodic,
  periodRangeLabel,
  previousPeriodKey,
  rewardStatusLabel,
  settledByLabel,
  settlementLabel,
  tierLabel
} from "@/components/apps/leaderboard/leaderboard-shared";
import { RewardBadges } from "@/components/apps/points-store/points-store-shared";
import { StatTile } from "@/components/content/content-shared";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type {
  LeaderboardBoardSpec,
  LeaderboardRewardStatus,
  LeaderboardSnapshot,
  LeaderboardSnapshotStats
} from "@/lib/api/leaderboard";
import { formatDateTimeMinute } from "@/lib/datetime";
import {
  useLeaderboardSettingsQuery,
  useLeaderboardSnapshotEntriesQuery,
  useLeaderboardSnapshotsQuery,
  useRetryLeaderboardSnapshotMutation,
  useSettleLeaderboardMutation
} from "@/lib/leaderboard-hooks";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

const LIMIT = 20;
const ALL = "__all";

/**
 * 结算记录：每个周期榜每一期一份快照（无论是否配奖励），点开看条目明细。
 *
 * 发放失败的条目可整期重试（置回待发放，由结算循环发放）；已结束但还没结算的周期可手动结算，
 * 与自动结算同一入口、幂等，周期没结束时后端回 40922，原因原样提示。
 */
export function LeaderboardSnapshotsPanel({ appKey }: { appKey: string }) {
  const settingsQuery = useLeaderboardSettingsQuery(appKey);
  const [board, setBoard] = useState<string>(ALL);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<number | null>(null);
  const [settling, setSettling] = useState(false);
  const [retrying, setRetrying] = useState<LeaderboardSnapshot | null>(null);

  const catalog = settingsQuery.data?.catalog ?? [];
  const periodicBoards = catalog.filter((spec) => isPeriodic(spec));
  const listQuery = useLeaderboardSnapshotsQuery(appKey, { board: board === ALL ? undefined : board, page, limit: LIMIT });
  const retryMutation = useRetryLeaderboardSnapshotMutation(appKey);

  const items = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? 1);
  const labelOf = (key: string) => catalog.find((spec) => spec.key === key)?.label ?? key;

  const submitRetry = async () => {
    if (!retrying) return;
    try {
      const result = await retryMutation.mutateAsync(retrying.id);
      toast.success(result.retried > 0 ? `已重新排队 ${result.retried} 份奖励` : "没有发放失败的奖励");
      setRetrying(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "重试失败");
    }
  };

  const retryDialog = (
    <AlertDialog open={Boolean(retrying)} onOpenChange={(open) => !open && setRetrying(null)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            重试 {retrying ? `${labelOf(retrying.board)} ${retrying.periodKey}` : ""} 发放失败的奖励？
          </AlertDialogTitle>
          <AlertDialogDescription>
            {retrying?.stats?.failed ? `${formatCount(retrying.stats.failed)} 份` : "失败的"}
            奖励置回待发放，由结算循环在一分钟内重新发放；发放幂等，不会重复到账。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>返回</AlertDialogCancel>
          <AlertDialogAction onClick={() => void submitRetry()} disabled={retryMutation.isPending}>
            {retryMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            确认重试
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  if (selected) {
    return (
      <>
        <SnapshotDetail
          appKey={appKey}
          snapshotId={selected}
          catalog={catalog}
          onBack={() => setSelected(null)}
          onRetry={setRetrying}
        />
        {retryDialog}
      </>
    );
  }

  return (
    <SectionCard
      icon={<History className="size-4" />}
      title="结算记录"
      aside={
        <Button
          size="sm"
          variant="outline"
          className="h-8"
          onClick={() => setSettling(true)}
          disabled={periodicBoards.length === 0}
        >
          <CalendarCheck className="size-3.5" /> 手动结算
        </Button>
      }
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Select
          value={board}
          onValueChange={(value) => {
            setBoard(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>全部榜单</SelectItem>
            {periodicBoards.map((spec) => (
              <SelectItem key={spec.key} value={spec.key}>
                {spec.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      ) : listQuery.isError ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {listQuery.error instanceof ApiError ? listQuery.error.message : "结算记录加载失败"}
        </p>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          暂无结算记录
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>榜单与期号</TableHead>
                <TableHead>周期</TableHead>
                <TableHead className="text-right">参与人数</TableHead>
                <TableHead>发放</TableHead>
                <TableHead>结算</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id} className="cursor-pointer" onClick={() => setSelected(item.id)}>
                  <TableCell className="whitespace-nowrap">
                    <p className="text-xs font-medium">{labelOf(item.board)}</p>
                    <p className="font-mono text-[11px] text-muted-foreground">{item.periodKey}</p>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                    {periodRangeLabel(item.periodStart, item.periodEnd)}
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums">{formatCount(item.participants)}</TableCell>
                  <TableCell>
                    <StatsBadges stats={item.stats} />
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs">
                    <div className="flex items-center gap-1.5">
                      <SnapshotStatusBadge status={item.status} />
                      <span className="text-muted-foreground">{settledByLabel(item.settledBy)}</span>
                    </div>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">{formatDateTimeMinute(item.settledAt)}</p>
                  </TableCell>
                  <TableCell>
                    {item.stats?.failed ? (
                      <div className="flex justify-end">
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7"
                          onClick={(event) => {
                            event.stopPropagation();
                            setRetrying(item);
                          }}
                        >
                          <RotateCw className="size-3.5" /> 重试失败
                        </Button>
                      </div>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <InvitePager total={total} page={page} totalPages={totalPages} onPage={setPage} />

      {retryDialog}

      <SettleDialog
        appKey={appKey}
        open={settling}
        boards={periodicBoards}
        defaultBoard={board === ALL ? undefined : board}
        onOpenChange={setSettling}
        onSettled={(snapshot) => setSelected(snapshot.id)}
      />
    </SectionCard>
  );
}

function SnapshotStatusBadge({ status }: { status: string }) {
  return status === "settled" ? (
    <Badge variant="success" size="sm">
      已结算
    </Badge>
  ) : (
    <Badge variant="warning" size="sm">
      结算中
    </Badge>
  );
}

/** 发放统计：应发为零时写无奖励，否则逐项列非零的状态 */
function StatsBadges({ stats }: { stats?: LeaderboardSnapshotStats }) {
  if (!stats || stats.rewarded === 0) return <span className="text-xs text-muted-foreground">无奖励</span>;
  const parts: Array<{ status: LeaderboardRewardStatus; count: number }> = [
    { status: "granted", count: stats.granted },
    { status: "pending", count: stats.pending },
    { status: "failed", count: stats.failed },
    { status: "skipped", count: stats.skipped }
  ];
  return (
    <div className="space-y-1">
      <p className="text-xs">
        应发 <span className="font-medium tabular-nums">{formatCount(stats.rewarded)}</span>
      </p>
      <div className="flex flex-wrap gap-1">
        {parts
          .filter((part) => part.count > 0)
          .map((part) => (
            <Badge
              key={part.status}
              variant={part.status === "failed" ? "danger" : part.status === "pending" ? "warning" : "outline"}
              size="sm"
              className="font-normal tabular-nums"
            >
              {rewardStatusLabel(part.status)} {formatCount(part.count)}
            </Badge>
          ))}
      </div>
    </div>
  );
}

/** 一期的条目明细：名次、用户、取值、应发奖励、状态、失败原因与发放结果 */
function SnapshotDetail({
  appKey,
  snapshotId,
  catalog,
  onBack,
  onRetry
}: {
  appKey: string;
  snapshotId: number;
  catalog: LeaderboardBoardSpec[];
  onBack: () => void;
  onRetry: (snapshot: LeaderboardSnapshot) => void;
}) {
  const settingsQuery = useLeaderboardSettingsQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const [status, setStatus] = useState<LeaderboardRewardStatus | typeof ALL>(ALL);
  const [page, setPage] = useState(1);
  const entriesQuery = useLeaderboardSnapshotEntriesQuery(appKey, snapshotId, {
    status: status === ALL ? undefined : status,
    page,
    limit: LIMIT
  });

  const rewardCatalog = settingsQuery.data?.rewardCatalog ?? [];
  const statuses = settingsQuery.data?.rewardStatuses?.length
    ? settingsQuery.data.rewardStatuses
    : DEFAULT_REWARD_STATUSES;
  const plans = plansQuery.data ?? [];
  const features = featuresQuery.data ?? [];
  const snapshot = entriesQuery.data?.snapshot;
  const entries = entriesQuery.data?.entries;
  const items = entries?.items ?? [];
  const spec = catalog.find((item) => item.key === snapshot?.board);
  const stats = snapshot?.stats;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border px-4 py-3">
        <div className="flex min-w-0 items-center gap-3">
          <Button size="icon" variant="ghost" className="size-8 shrink-0" aria-label="返回结算记录" onClick={onBack}>
            <ArrowLeft className="size-4" />
          </Button>
          {snapshot ? (
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{spec?.label ?? snapshot.board}</span>
                <span className="font-mono text-xs text-muted-foreground">{snapshot.periodKey}</span>
                <SnapshotStatusBadge status={snapshot.status} />
              </div>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                {periodRangeLabel(snapshot.periodStart, snapshot.periodEnd)}，
                {settlementLabel(snapshot.settledBy, snapshot.settledAt)}
              </p>
            </div>
          ) : (
            <Skeleton className="h-8 w-48" />
          )}
        </div>
        {snapshot && stats?.failed ? (
          <Button size="sm" variant="outline" className="h-8" onClick={() => onRetry(snapshot)}>
            <RotateCw className="size-3.5" /> 重试失败
          </Button>
        ) : null}
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
        <StatTile label="参与人数" value={snapshot ? formatCount(snapshot.participants) : "—"} />
        <StatTile label="上榜条目" value={stats ? formatCount(stats.entries) : "—"} />
        <StatTile label="应发奖励" value={stats ? formatCount(stats.rewarded) : "—"} />
        <StatTile label="已发放" value={stats ? formatCount(stats.granted) : "—"} tone={stats?.granted ? "positive" : "muted"} />
        <StatTile label="待发放" value={stats ? formatCount(stats.pending) : "—"} tone={stats?.pending ? "default" : "muted"} />
        <StatTile
          label="发放失败"
          value={
            stats?.failed ? (
              <span className="text-red-600 dark:text-red-400">{formatCount(stats.failed)}</span>
            ) : stats ? (
              formatCount(stats.failed)
            ) : (
              "—"
            )
          }
          tone={stats?.failed ? "default" : "muted"}
          hint={stats?.skipped ? `另有 ${formatCount(stats.skipped)} 份已跳过` : undefined}
        />
      </div>

      {snapshot?.tiers?.length ? (
        <div className="rounded-xl border border-border px-4 py-3">
          <p className="mb-2 text-xs font-medium">结算时的奖励档位</p>
          <div className="space-y-1.5">
            {snapshot.tiers.map((tier) => (
              <div key={`${tier.rankFrom}-${tier.rankTo}`} className="flex flex-wrap items-center gap-2">
                <span className="w-24 shrink-0 text-xs text-muted-foreground">{tierLabel(tier.rankFrom, tier.rankTo)}</span>
                <RewardBadges rewards={tier.rewards} catalog={rewardCatalog} plans={plans} features={features} />
              </div>
            ))}
          </div>
        </div>
      ) : null}

      <SectionCard icon={<History className="size-4" />} title="条目明细">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <Select
            value={status}
            onValueChange={(value) => {
              setStatus(value as LeaderboardRewardStatus | typeof ALL);
              setPage(1);
            }}
          >
            <SelectTrigger className="h-8 w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部状态</SelectItem>
              {statuses.map((item) => (
                <SelectItem key={item} value={item}>
                  {rewardStatusLabel(item)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {entriesQuery.isLoading ? (
          <div className="space-y-2">
            {[0, 1, 2, 3].map((index) => (
              <Skeleton key={index} className="h-12 w-full" />
            ))}
          </div>
        ) : entriesQuery.isError ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            {entriesQuery.error instanceof ApiError ? entriesQuery.error.message : "结算明细加载失败"}
          </p>
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            {status === ALL ? "这一期没有人上榜" : "暂无匹配条目"}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-14 text-center">名次</TableHead>
                  <TableHead>用户</TableHead>
                  <TableHead className="text-right">取值</TableHead>
                  <TableHead>应发奖励</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>发放结果</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((entry) => (
                  <TableRow key={entry.userId}>
                    <TableCell className="text-center text-xs font-semibold tabular-nums">{entry.rank}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1.5">
                        <LeaderboardUser
                          appKey={appKey}
                          userId={entry.userId}
                          name={entry.displayName}
                          account={entry.account}
                          avatar={entry.avatar}
                        />
                        {entry.anonymous ? (
                          <Badge variant="outline" size="sm" className="font-normal">
                            匿名
                          </Badge>
                        ) : null}
                        {!entry.account ? (
                          <Badge variant="secondary" size="sm" className="font-normal">
                            已注销
                          </Badge>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-right text-xs">
                      <LeaderboardValue value={entry.value} unit={spec?.valueUnit} levelName={entry.levelName} />
                    </TableCell>
                    <TableCell className="max-w-64">
                      {entry.rewards?.length ? (
                        <RewardBadges rewards={entry.rewards} catalog={rewardCatalog} plans={plans} features={features} />
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell className="max-w-56">
                      <RewardStatusBadge status={entry.rewardStatus} />
                      {entry.rewardError ? (
                        <p className="mt-1 text-[11px] whitespace-normal break-words text-red-600 dark:text-red-400">
                          {entry.rewardError}
                        </p>
                      ) : null}
                    </TableCell>
                    <TableCell className="max-w-64">
                      {entry.rewardStatus === "granted" ? (
                        <>
                          <ResultBadges results={entry.rewardResults ?? []} empty="—" />
                          <p className="mt-1 text-[11px] text-muted-foreground">{formatDateTimeMinute(entry.grantedAt)}</p>
                        </>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}

        <InvitePager
          total={entries?.total ?? 0}
          page={page}
          totalPages={Math.max(1, entries?.totalPages ?? 1)}
          onPage={setPage}
        />
      </SectionCard>
    </div>
  );
}

/** 手动结算：选周期榜与期号；默认填上一期（按浏览器时区算，提交前核对） */
function SettleDialog({
  appKey,
  open,
  boards,
  defaultBoard,
  onOpenChange,
  onSettled
}: {
  appKey: string;
  open: boolean;
  boards: LeaderboardBoardSpec[];
  defaultBoard?: string;
  onOpenChange: (open: boolean) => void;
  onSettled: (snapshot: LeaderboardSnapshot) => void;
}) {
  const settleMutation = useSettleLeaderboardMutation(appKey);
  const [form, setForm] = useState<{ board: string; periodKey: string } | null>(null);
  const [error, setError] = useState("");

  const initialBoard = boards.find((spec) => spec.key === defaultBoard) ?? boards[0];
  const current = form ?? {
    board: initialBoard?.key ?? "",
    periodKey: initialBoard ? previousPeriodKey(initialBoard.period) : ""
  };
  const spec = boards.find((item) => item.key === current.board);
  const hint = spec && isPeriodic(spec) ? PERIOD_KEY_HINT[spec.period as "day" | "week" | "month"] : undefined;

  const close = (next: boolean) => {
    if (!next) {
      setForm(null);
      setError("");
    }
    onOpenChange(next);
  };

  const submit = async () => {
    if (!current.board || !current.periodKey.trim()) {
      setError("请选择榜单并填写期号");
      return;
    }
    setError("");
    try {
      const result = await settleMutation.mutateAsync({ board: current.board, periodKey: current.periodKey.trim() });
      toast.success(result.created ? `${spec?.label ?? ""} ${result.snapshot.periodKey} 已结算` : "这一期已经结算过");
      close(false);
      onSettled(result.snapshot);
    } catch (err) {
      const message = err instanceof ApiError ? err.message : "结算失败";
      setError(message);
      toast.error(message);
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>手动结算</DialogTitle>
          <DialogDescription>
            结算一个已结束但还没结算的周期，按此刻的奖励档位发放；已结算的期直接返回原快照。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">榜单</Label>
            <Select
              value={current.board}
              onValueChange={(value) => {
                const next = boards.find((item) => item.key === value);
                setForm({ board: value, periodKey: next ? previousPeriodKey(next.period) : "" });
                setError("");
              }}
            >
              <SelectTrigger className="h-9 w-full">
                <SelectValue placeholder="选择榜单" />
              </SelectTrigger>
              <SelectContent>
                {boards.map((item) => (
                  <SelectItem key={item.key} value={item.key}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">期号</Label>
            <Input
              value={current.periodKey}
              className="font-mono text-xs"
              placeholder={hint?.example}
              onChange={(event) => {
                setForm({ ...current, periodKey: event.target.value });
                setError("");
              }}
            />
            {hint ? <p className="text-[10px] text-muted-foreground">{hint.text}，默认填上一期</p> : null}
          </div>
          {error ? <p className="text-xs text-red-600 dark:text-red-400">{error}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={() => close(false)}>
            返回
          </Button>
          <Button size="sm" onClick={() => void submit()} disabled={settleMutation.isPending || !current.board}>
            {settleMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            确认结算
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
