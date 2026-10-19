"use client";

import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { parseUserIds } from "@/components/apps/leaderboard/leaderboard-shared";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import { useAddLeaderboardExclusionsMutation, useLeaderboardSettingsQuery } from "@/lib/leaderboard-hooks";

/** 目录还没到时的兜底上限，与后端 leaderboard/types.go 一致 */
const FALLBACK_REASON_LENGTH = 200;
const FALLBACK_MAX_EXCLUSIONS = 100;

/**
 * 移出排行榜：给了 `target` 时只移出这一个人（实时榜的行操作），否则按用户 ID 批量移出。
 *
 * 移出对所有榜生效，含未结算的周期；已结算的快照不改写，其中尚未发放的奖励在发放时记为跳过。
 * 不存在的用户 ID 由后端在 missing 里返回，这里提示出来。
 */
export function LeaderboardExcludeDialog({
  appKey,
  open,
  target,
  onOpenChange
}: {
  appKey: string;
  open: boolean;
  target?: { userId: number; label: string } | null;
  onOpenChange: (open: boolean) => void;
}) {
  const settingsQuery = useLeaderboardSettingsQuery(appKey);
  const addMutation = useAddLeaderboardExclusionsMutation(appKey);
  const [ids, setIds] = useState("");
  const [reason, setReason] = useState("");

  const limits = settingsQuery.data?.limits;
  const maxReason = limits?.maxReasonLength ?? FALLBACK_REASON_LENGTH;
  const maxExclusions = limits?.maxExclusions ?? FALLBACK_MAX_EXCLUSIONS;
  const parsed = parseUserIds(ids);
  const userIds = target ? [target.userId] : parsed.ids;
  const tooMany = userIds.length > maxExclusions;

  const close = (next: boolean) => {
    if (!next) {
      setIds("");
      setReason("");
    }
    onOpenChange(next);
  };

  const submit = async () => {
    if (userIds.length === 0) {
      toast.error("请填写要移出的用户 ID");
      return;
    }
    if (tooMany) {
      toast.error(`一次最多移出 ${maxExclusions} 人`);
      return;
    }
    try {
      const result = await addMutation.mutateAsync({ userIds, reason: reason.trim() });
      if (result.missing?.length) {
        toast.warning(`已移出 ${result.added} 人，以下用户不存在：${result.missing.join("、")}`);
      } else {
        toast.success(target ? `已将 ${target.label} 移出排行榜` : `已移出 ${result.added} 人`);
      }
      close(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "移出失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{target ? `将 ${target.label} 移出排行榜` : "批量移出排行榜"}</DialogTitle>
          <DialogDescription>
            对所有榜生效，含未结算的周期；已结算的往期不改写，其中未发放的奖励不再发放。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          {target ? null : (
            <div className="space-y-1.5">
              <div className="flex items-baseline justify-between gap-2">
                <Label className="text-[11px] font-medium">用户 ID</Label>
                <span className="text-[10px] text-muted-foreground tabular-nums">
                  {parsed.ids.length}/{maxExclusions}
                </span>
              </div>
              <Textarea
                value={ids}
                rows={4}
                className="font-mono text-xs"
                placeholder="多个 ID 用空格、逗号或换行分隔"
                onChange={(event) => setIds(event.target.value)}
              />
              {parsed.invalid.length ? (
                <p className="text-[11px] text-amber-600 dark:text-amber-400">
                  已忽略无效内容：{parsed.invalid.slice(0, 5).join("、")}
                  {parsed.invalid.length > 5 ? ` 等 ${parsed.invalid.length} 项` : ""}
                </p>
              ) : null}
              {tooMany ? (
                <p className="text-[11px] text-amber-600 dark:text-amber-400">一次最多移出 {maxExclusions} 人</p>
              ) : null}
            </div>
          )}
          <div className="space-y-1.5">
            <div className="flex items-baseline justify-between gap-2">
              <Label className="text-[11px] font-medium">原因</Label>
              <span className="text-[10px] text-muted-foreground tabular-nums">
                {reason.length}/{maxReason}
              </span>
            </div>
            <Textarea
              value={reason}
              rows={3}
              maxLength={maxReason}
              placeholder="例如：刷分、测试账号"
              onChange={(event) => setReason(event.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={() => close(false)}>
            返回
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => void submit()}
            disabled={addMutation.isPending || userIds.length === 0 || tooMany}
          >
            {addMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            确认移出
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
