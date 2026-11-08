"use client";

import { useState } from "react";
import { toast } from "sonner";
import { formatPoints } from "@/components/apps/points-store/points-store-shared";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { errorMessage } from "@/components/users/detail/kit";
import { Progress } from "@/components/ui/progress";
import type { PointsStoreRedemption } from "@/lib/api/points-store";
import { useHandlePointsStoreRedemptionMutation } from "@/lib/points-store-hooks";

export type HandleAction = "fulfill" | "cancel";
export type HandleRequest = { action: HandleAction; items: PointsStoreRedemption[] } | null;

/**
 * 处理待发放的兑换：发放（备注写进站内信，如兑换码、快递单号）或取消（退积分、加回库存）。
 *
 * 单笔与批量同一个框：批量时逐笔提交（接口是单笔的，各自独立成败），
 * 进度条显示走到第几笔，结束后汇总成功与失败；失败的那几笔留给调用方继续处理。
 */
export function RedemptionHandleDialog({
  appKey,
  request,
  onClose,
  onDone
}: {
  appKey: string;
  request: HandleRequest;
  onClose: () => void;
  /** 处理完成：成功的记录（已更新）与失败的编号 */
  onDone?: (result: { updated: PointsStoreRedemption[]; failedIds: number[] }) => void;
}) {
  const mutation = useHandlePointsStoreRedemptionMutation(appKey);
  const [note, setNote] = useState("");
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [lastRequest, setLastRequest] = useState<HandleRequest>(null);

  // 换了一批要处理的记录就清空备注（渲染期比对，不用 effect 同步）
  if (request !== lastRequest) {
    setLastRequest(request);
    if (request) {
      setNote("");
      setProgress(null);
    }
  }

  const items = request?.items ?? [];
  const fulfill = request?.action === "fulfill";
  const single = items.length === 1 ? items[0] : null;
  const totalCost = items.reduce((sum, item) => sum + item.cost, 0);
  const busy = progress !== null;

  const submit = async () => {
    if (!request) return;
    const updated: PointsStoreRedemption[] = [];
    const failedIds: number[] = [];
    let lastError: unknown = null;
    setProgress({ done: 0, total: items.length });
    for (const [index, item] of items.entries()) {
      try {
        updated.push(await mutation.mutateAsync({ redemptionId: item.id, action: request.action, note: note.trim() }));
      } catch (error) {
        failedIds.push(item.id);
        lastError = error;
      }
      setProgress({ done: index + 1, total: items.length });
    }
    setProgress(null);
    const verb = fulfill ? "发放" : "取消";
    if (!failedIds.length) {
      toast.success(single ? (fulfill ? "已发放，并通知了用户" : "已取消，积分已退回") : `已${verb} ${updated.length} 笔`);
      onClose();
    } else if (updated.length) {
      toast.warning(`已${verb} ${updated.length} 笔，${failedIds.length} 笔失败：${errorMessage(lastError)}`);
      onClose();
    } else {
      toast.error(errorMessage(lastError, `${verb}失败`));
    }
    onDone?.({ updated, failedIds });
  };

  const who = single ? single.account || single.nickname || `#${single.userId}` : "";

  return (
    <ConfirmActionDialog
      open={Boolean(request)}
      onOpenChange={(open) => (!open && !busy ? onClose() : undefined)}
      title={single ? (fulfill ? "发放兑换" : "取消兑换") : fulfill ? `批量发放 ${items.length} 笔` : `批量取消 ${items.length} 笔`}
      description={
        fulfill
          ? single
            ? `${who} 兑换的「${single.itemTitle}」标记为已发放，用户会收到「兑换已发放」的站内信。`
            : "所选兑换全部标记为已发放，每位用户都会收到站内信，备注内容相同。"
          : single
            ? `取消 ${who} 兑换的「${single.itemTitle}」，退回 ${formatPoints(single.cost)} 积分并加回库存，用户会收到站内信。此操作不可撤销。`
            : `共退回 ${formatPoints(totalCost)} 积分并加回库存，每位用户都会收到站内信。此操作不可撤销。`
      }
      confirmLabel={fulfill ? "确认发放" : "确认取消"}
      destructive={!fulfill}
      pending={busy}
      note={{
        value: note,
        onChange: setNote,
        placeholder: fulfill ? "兑换码、快递单号等，会附在站内信里" : "取消原因，会附在站内信里",
        hint: "选填，写给用户看",
        max: 500
      }}
      onConfirm={submit}
    >
      {progress && progress.total > 1 ? (
        <div className="flex flex-col gap-1.5">
          <Progress value={(progress.done / progress.total) * 100} />
          <span className="text-xs tabular-nums text-muted-foreground">
            {progress.done} / {progress.total}
          </span>
        </div>
      ) : null}
    </ConfirmActionDialog>
  );
}
