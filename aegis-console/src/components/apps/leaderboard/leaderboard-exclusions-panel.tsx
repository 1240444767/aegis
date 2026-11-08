"use client";

import { useMemo, useState } from "react";
import { Loader2, Undo2, UserMinus } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { InvitePager } from "@/components/apps/invite/invite-shared";
import { LeaderboardExcludeDialog } from "@/components/apps/leaderboard/leaderboard-exclude-dialog";
import { LeaderboardUser } from "@/components/apps/leaderboard/leaderboard-shared";
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
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import type { LeaderboardExclusion } from "@/lib/api/leaderboard";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useLeaderboardExclusionsQuery, useRemoveLeaderboardExclusionMutation } from "@/lib/leaderboard-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;

/**
 * 移出名单：被管理员移出排行榜的人（刷分、测试号）。对所有榜生效，含未结算的周期；
 * 已结算的快照不改写。恢复后重新参与排名，实时榜列表最多晚 60 秒反映。
 */
export function LeaderboardExclusionsPanel({ appKey }: { appKey: string }) {
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [adding, setAdding] = useState(false);
  const [restoring, setRestoring] = useState<LeaderboardExclusion | null>(null);

  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);
  const listQuery = useLeaderboardExclusionsQuery(appKey, {
    keyword: debouncedKeyword || undefined,
    page,
    limit: LIMIT
  });
  const removeMutation = useRemoveLeaderboardExclusionMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? 1);

  const restoringLabel = restoring ? restoring.nickname || restoring.account || `#${restoring.userId}` : "";

  const submitRestore = async () => {
    if (!restoring) return;
    try {
      await removeMutation.mutateAsync(restoring.userId);
      toast.success(`已将 ${restoringLabel} 移回排行榜`);
      setRestoring(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <SectionCard
      icon={<UserMinus className="size-4" />}
      title="移出名单"
      aside={
        <Button size="sm" variant="outline" className="h-8" onClick={() => setAdding(true)}>
          <UserMinus className="size-3.5" /> 批量移出
        </Button>
      }
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索账号、昵称或用户 ID"
          className="h-8 w-56"
        />
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-11 w-full" />
          ))}
        </div>
      ) : listQuery.isError ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {listQuery.error instanceof ApiError ? listQuery.error.message : "移出名单加载失败"}
        </p>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {keyword ? "暂无匹配记录" : "暂无被移出的用户"}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>用户</TableHead>
                <TableHead>原因</TableHead>
                <TableHead>操作人</TableHead>
                <TableHead className="text-right">移出时间</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.userId}>
                  <TableCell>
                    <LeaderboardUser
                      appKey={appKey}
                      userId={item.userId}
                      name={item.nickname || item.account}
                      account={item.account}
                    />
                  </TableCell>
                  <TableCell className="max-w-72 text-xs whitespace-normal break-words">
                    {item.reason || <span className="text-muted-foreground">未填写</span>}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{item.operator || "—"}</TableCell>
                  <TableCell className="whitespace-nowrap text-right text-xs text-muted-foreground">
                    {formatDateTimeMinute(item.createdAt)}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end">
                      <Button size="sm" variant="ghost" className="h-7" onClick={() => setRestoring(item)}>
                        <Undo2 className="size-3.5" /> 恢复
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <InvitePager total={total} page={page} totalPages={totalPages} onPage={setPage} />

      <LeaderboardExcludeDialog appKey={appKey} open={adding} onOpenChange={setAdding} />

      <AlertDialog open={Boolean(restoring)} onOpenChange={(open) => !open && setRestoring(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>将 {restoringLabel} 移回排行榜？</AlertDialogTitle>
            <AlertDialogDescription>移回后重新参与所有榜的排名，已结算的往期不变。</AlertDialogDescription>
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
