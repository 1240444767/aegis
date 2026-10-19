"use client";

import { useState } from "react";
import { ListOrdered, Loader2, Undo2, UserMinus } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { InvitePager, formatCount } from "@/components/apps/invite/invite-shared";
import { LeaderboardExcludeDialog } from "@/components/apps/leaderboard/leaderboard-exclude-dialog";
import {
  LeaderboardUser,
  LeaderboardValue,
  PERIOD_KEY_HINT,
  isPeriodic,
  periodRangeLabel,
  settlementLabel
} from "@/components/apps/leaderboard/leaderboard-shared";
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
import { Input } from "@/components/ui/input";
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
import type { LeaderboardAdminEntry } from "@/lib/api/leaderboard";
import { formatDateTimeMinute } from "@/lib/datetime";
import {
  useLeaderboardBoardQuery,
  useLeaderboardSettingsQuery,
  useLeaderboardSnapshotsQuery,
  useRemoveLeaderboardExclusionMutation
} from "@/lib/leaderboard-hooks";

const LIMIT = 50;
const CURRENT = "__current";
const CUSTOM = "__custom";

/**
 * 实时榜：某个榜本期或某一期的完整名次，带真实账号。
 *
 * 实时计算时不限展示名次，被移出的人也列出来（不参与排名）；选了已结算的往期取自快照，
 * 选了未结算的期号按那个周期实时预览。移出对所有榜生效，列表最多晚 60 秒反映（缓存）。
 */
export function LeaderboardBoardPanel({
  appKey,
  boardKey,
  onBoardChange
}: {
  appKey: string;
  boardKey: string | null;
  onBoardChange: (key: string) => void;
}) {
  const settingsQuery = useLeaderboardSettingsQuery(appKey);
  const [period, setPeriod] = useState<string>(CURRENT);
  const [customDraft, setCustomDraft] = useState("");
  const [customKey, setCustomKey] = useState("");
  const [page, setPage] = useState(1);
  const [excluding, setExcluding] = useState<{ userId: number; label: string } | null>(null);
  const [restoring, setRestoring] = useState<LeaderboardAdminEntry | null>(null);

  const data = settingsQuery.data;
  const catalog = data?.catalog ?? [];
  // 下拉按设置里的标签顺序，开启的在前
  const ordered = (data?.settings.boards ?? [])
    .map((board) => ({ ...board, spec: catalog.find((item) => item.key === board.key) }))
    .filter((board) => board.spec)
    .sort((a, b) => Number(b.enabled) - Number(a.enabled));
  const activeKey = boardKey ?? ordered[0]?.key ?? null;
  const spec = catalog.find((item) => item.key === activeKey);
  const periodic = isPeriodic(spec);

  const periodKey = !periodic ? undefined : period === CURRENT ? undefined : period === CUSTOM ? customKey || undefined : period;

  const snapshotsQuery = useLeaderboardSnapshotsQuery(periodic ? appKey : null, { board: activeKey ?? undefined, limit: 50 });
  const boardQuery = useLeaderboardBoardQuery(appKey, activeKey, { periodKey, page, limit: LIMIT });
  const removeMutation = useRemoveLeaderboardExclusionMutation(appKey);

  const result = boardQuery.data;
  const items = result?.items ?? [];
  const live = result?.source !== "snapshot";
  const showLastAt = periodic && live;

  const selectBoard = (key: string) => {
    onBoardChange(key);
    setPeriod(CURRENT);
    setCustomDraft("");
    setCustomKey("");
    setPage(1);
  };

  const selectPeriod = (value: string) => {
    setPeriod(value);
    setPage(1);
  };

  const applyCustom = () => {
    setCustomKey(customDraft.trim());
    setPage(1);
  };

  const submitRestore = async () => {
    if (!restoring) return;
    try {
      await removeMutation.mutateAsync(restoring.userId);
      toast.success("已移回排行榜");
      setRestoring(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const labelOf = (entry: LeaderboardAdminEntry) => entry.nickname || entry.name || entry.account || `#${entry.userId}`;

  return (
    <SectionCard
      icon={<ListOrdered className="size-4" />}
      title="实时榜"
      aside={
        result ? (
          <Badge variant={live ? "info" : "secondary"} size="sm">
            {live ? "实时计算" : "结算快照"}
          </Badge>
        ) : null
      }
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        {settingsQuery.isLoading ? (
          <Skeleton className="h-8 w-40" />
        ) : (
          <Select value={activeKey ?? ""} onValueChange={selectBoard}>
            <SelectTrigger className="h-8 w-40">
              <SelectValue placeholder="选择榜单" />
            </SelectTrigger>
            <SelectContent>
              {ordered.map((board) => (
                <SelectItem key={board.key} value={board.key}>
                  {board.spec?.label ?? board.key}
                  {board.enabled ? "" : "（未开启）"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {periodic ? (
          <Select value={period} onValueChange={selectPeriod}>
            <SelectTrigger className="h-8 w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper">
              <SelectItem value={CURRENT}>本期</SelectItem>
              {(snapshotsQuery.data?.items ?? []).map((snapshot) => (
                <SelectItem key={snapshot.id} value={snapshot.periodKey}>
                  {snapshot.periodKey}
                </SelectItem>
              ))}
              <SelectItem value={CUSTOM}>指定期号</SelectItem>
            </SelectContent>
          </Select>
        ) : null}
        {periodic && period === CUSTOM && spec ? (
          <>
            <Input
              value={customDraft}
              onChange={(event) => setCustomDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") applyCustom();
              }}
              placeholder={PERIOD_KEY_HINT[spec.period as "day" | "week" | "month"]?.example}
              className="h-8 w-36 font-mono text-xs"
            />
            <Button size="sm" variant="outline" className="h-8" onClick={applyCustom} disabled={!customDraft.trim()}>
              查看
            </Button>
          </>
        ) : null}
      </div>

      {result ? (
        <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl bg-muted px-3 py-2 text-xs text-muted-foreground">
          <span>
            <span className="text-foreground">{result.board.label}</span>
            <span className="ml-1.5">{result.board.periodLabel}</span>
            {periodic ? <span className="ml-1.5 font-mono">{result.board.periodKey}</span> : null}
          </span>
          {periodic ? <span>{periodRangeLabel(result.board.periodStart, result.board.periodEnd)}</span> : null}
          <span>
            参与人数 <span className="font-medium text-foreground tabular-nums">{formatCount(result.total)}</span>
          </span>
          {result.snapshot ? (
            <span>{settlementLabel(result.snapshot.settledBy, result.snapshot.settledAt)}</span>
          ) : (
            <span>不限展示名次，被移出的人不参与排名</span>
          )}
        </div>
      ) : null}

      {boardQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2, 3, 4].map((index) => (
            <Skeleton key={index} className="h-11 w-full" />
          ))}
        </div>
      ) : boardQuery.isError ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {boardQuery.error instanceof ApiError ? boardQuery.error.message : "榜单加载失败"}
        </p>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          暂无人上榜
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-14 text-center">名次</TableHead>
                <TableHead>用户</TableHead>
                <TableHead className="text-right">取值</TableHead>
                <TableHead>状态</TableHead>
                {showLastAt ? <TableHead className="text-right">最后获得</TableHead> : null}
                {live ? <TableHead className="w-0" /> : null}
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((entry) => (
                <TableRow key={entry.userId} className={entry.excluded ? "opacity-60" : undefined}>
                  <TableCell className="text-center text-xs font-semibold tabular-nums">{entry.rank ?? "—"}</TableCell>
                  <TableCell>
                    <LeaderboardUser
                      appKey={appKey}
                      userId={entry.userId}
                      name={entry.nickname || entry.name}
                      account={entry.account}
                      avatar={entry.avatar}
                    />
                  </TableCell>
                  <TableCell className="text-right text-xs">
                    <LeaderboardValue value={entry.value} unit={result?.board.valueUnit} levelName={entry.levelName} />
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {entry.excluded ? (
                        <Badge variant="danger" size="sm">
                          已移出
                        </Badge>
                      ) : null}
                      {entry.anonymous ? (
                        <Badge variant="outline" size="sm" className="font-normal">
                          匿名
                        </Badge>
                      ) : null}
                      {!entry.excluded && !entry.anonymous ? (
                        <span className="text-xs text-muted-foreground">—</span>
                      ) : null}
                    </div>
                  </TableCell>
                  {showLastAt ? (
                    <TableCell className="whitespace-nowrap text-right text-xs text-muted-foreground">
                      {formatDateTimeMinute(entry.lastAt)}
                    </TableCell>
                  ) : null}
                  {live ? (
                    <TableCell>
                      <div className="flex justify-end">
                        {entry.excluded ? (
                          <Button size="sm" variant="ghost" className="h-7" onClick={() => setRestoring(entry)}>
                            <Undo2 className="size-3.5" /> 恢复
                          </Button>
                        ) : (
                          <Button
                            size="sm"
                            variant="ghost"
                            className="h-7"
                            onClick={() => setExcluding({ userId: entry.userId, label: labelOf(entry) })}
                          >
                            <UserMinus className="size-3.5" /> 移出排行榜
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  ) : null}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <InvitePager
        total={result?.totalPages ? Math.max(result.total, items.length) : 0}
        page={page}
        totalPages={Math.max(1, result?.totalPages ?? 1)}
        onPage={setPage}
      />

      <LeaderboardExcludeDialog
        appKey={appKey}
        open={Boolean(excluding)}
        target={excluding}
        onOpenChange={(open) => !open && setExcluding(null)}
      />

      <AlertDialog open={Boolean(restoring)} onOpenChange={(open) => !open && setRestoring(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>将 {restoring ? labelOf(restoring) : ""} 移回排行榜？</AlertDialogTitle>
            <AlertDialogDescription>移回后重新参与所有榜的排名，列表最多晚 60 秒更新。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>返回</AlertDialogCancel>
            <AlertDialogAction onClick={() => void submitRestore()} disabled={removeMutation.isPending}>
              {removeMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认恢复
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  );
}
