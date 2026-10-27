"use client";

import { useState } from "react";
import { BarChart3, Dices, Loader2, Pause, Pencil, Play, Plus, ReceiptText, Square, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { LotteryActivityEditor } from "@/components/apps/lottery/lottery-activity-editor";
import { OWNABLE_KINDS } from "@/components/apps/lottery/lottery-activity-form";
import {
  LIFETIME_POLICY_META,
  LotteryStatusBadge,
  UI_MODE_LABEL,
  activityPhase,
  activityWindow,
  formatCount,
  formatProbability,
  kindLabelOf,
  prizeProbabilities
} from "@/components/apps/lottery/lottery-shared";
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
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type { LotteryAdminActivity, LotteryStatus } from "@/lib/api/lottery";
import {
  useDeleteLotteryActivityMutation,
  useLotteryActivitiesQuery,
  useLotteryCatalogQuery,
  useSetLotteryActivityStatusMutation
} from "@/lib/lottery-hooks";

/** 有抽奖记录的活动不能删除，后端回这个码；记录是用户的到账凭据 */
const CODE_HAS_DRAWS = 40927;

/** 每种状态下能去的状态：草稿只能上线，进行中可暂停或结束，暂停可恢复或结束，结束可重新上线 */
const TRANSITIONS: Record<LotteryStatus, Array<{ to: LotteryStatus; label: string; icon: typeof Play }>> = {
  draft: [{ to: "active", label: "上线", icon: Play }],
  active: [
    { to: "paused", label: "暂停", icon: Pause },
    { to: "ended", label: "结束", icon: Square }
  ],
  paused: [
    { to: "active", label: "上线", icon: Play },
    { to: "ended", label: "结束", icon: Square }
  ],
  ended: [{ to: "active", label: "上线", icon: Play }]
};

/**
 * 活动列表。
 *
 * 每张卡回答三件事：盘上有什么（奖品与公示概率）、怎么抽（免费 / 积分 / 视频 / 保底）、抽得怎么样
 * （累计、今日、待发放）。汇总随列表一起下发；待发放可直达抽奖记录。
 * 顺序即用户端的展示顺序（排序值、再按创建先后），由后端排好。
 */
export function LotteryActivitiesPanel({
  appKey,
  onOpenStats,
  onOpenDraws
}: {
  appKey: string;
  onOpenStats: (activityId: number) => void;
  onOpenDraws: (activityId: number, pendingOnly: boolean) => void;
}) {
  const activitiesQuery = useLotteryActivitiesQuery(appKey);
  const catalogQuery = useLotteryCatalogQuery(appKey);
  const statusMutation = useSetLotteryActivityStatusMutation(appKey);
  const deleteMutation = useDeleteLotteryActivityMutation(appKey);

  const [editor, setEditor] = useState<{ open: boolean; activity: LotteryAdminActivity | null }>({
    open: false,
    activity: null
  });
  const [pendingDelete, setPendingDelete] = useState<LotteryAdminActivity | null>(null);

  const kinds = catalogQuery.data?.kinds ?? [];
  const scenes = catalogQuery.data?.videoScenes ?? [];

  const changeStatus = async (activity: LotteryAdminActivity, status: LotteryStatus) => {
    try {
      await statusMutation.mutateAsync({ activityId: activity.id, status });
      toast.success(`「${activity.name}」已${status === "active" ? "上线" : status === "paused" ? "暂停" : "结束"}`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    const target = pendingDelete;
    try {
      await deleteMutation.mutateAsync(target.id);
      toast.success(`已删除「${target.name}」`);
      setPendingDelete(null);
    } catch (error) {
      setPendingDelete(null);
      if (error instanceof ApiError && error.code === CODE_HAS_DRAWS) {
        toast.error(error.message, {
          action:
            target.status === "ended"
              ? undefined
              : { label: "结束活动", onClick: () => void changeStatus(target, "ended") }
        });
        return;
      }
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  if (activitiesQuery.isLoading) {
    return (
      <div className="space-y-3">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-36 w-full rounded-xl" />
        ))}
      </div>
    );
  }

  const activities = activitiesQuery.data ?? [];

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Dices className="size-4" />}
        title="活动"
        aside={
          <Button size="sm" onClick={() => setEditor({ open: true, activity: null })}>
            <Plus className="size-3.5" /> 新建活动
          </Button>
        }
      >
        {activities.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            暂无活动
          </p>
        ) : (
          <div className="space-y-2.5">
            {activities.map((activity) => {
              const prizes = [...activity.prizes].sort((a, b) => a.position - b.position);
              const probabilities = prizeProbabilities(prizes);
              const pityPrize = prizes.find((prize) => prize.id === activity.pityPrizeId);
              const scene = scenes.find((item) => item.key === activity.videoScene);
              const phase = activityPhase(activity);
              const lifetimePolicy = activity.lifetimePolicy || "points";
              const timeWindow = activityWindow(activity.startTime, activity.endTime);
              const ways = [
                activity.freeDaily > 0 ? `每日免费 ${activity.freeDaily} 次` : null,
                activity.pointsCost > 0
                  ? `${formatCount(activity.pointsCost)} 积分一次${activity.pointsDaily > 0 ? `，每日 ${activity.pointsDaily} 次` : ""}`
                  : null,
                activity.videoScene ? `视频抽奖：${scene?.name || activity.videoScene}` : null,
                activity.pityThreshold > 0 && pityPrize ? `${activity.pityThreshold} 次保底「${pityPrize.name}」` : null
              ].filter(Boolean);
              return (
                <div key={activity.id} className="rounded-xl border border-border p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-semibold">{activity.name}</span>
                        <LotteryStatusBadge status={activity.status} />
                        {phase ? (
                          <Badge variant="info" size="sm">
                            {phase}
                          </Badge>
                        ) : null}
                        <Badge variant="outline" size="sm" className="font-normal">
                          {UI_MODE_LABEL[activity.uiMode] ?? activity.uiMode}
                        </Badge>
                        {activity.perks?.some((perk) => !perk.disabled) ? (
                          <Badge
                            variant="outline"
                            size="sm"
                            className="font-normal"
                            title={activity.perks.filter((perk) => !perk.disabled).map((perk) => perk.name).join("、")}
                          >
                            特权 {activity.perks.filter((perk) => !perk.disabled).length} 条
                          </Badge>
                        ) : null}
                        {prizes.some((prize) => OWNABLE_KINDS.includes(prize.kind)) ? (
                          <Badge
                            variant="outline"
                            size="sm"
                            className="font-normal"
                            title={LIFETIME_POLICY_META[lifetimePolicy]?.hint}
                          >
                            已拥有：{LIFETIME_POLICY_META[lifetimePolicy]?.short ?? lifetimePolicy}
                          </Badge>
                        ) : null}
                      </div>
                      {activity.description ? (
                        <p className="text-xs text-muted-foreground">{activity.description}</p>
                      ) : null}
                      <p className="text-xs text-muted-foreground">{ways.length > 0 ? ways.join("，") : "未开放任何抽奖方式"}</p>
                      {timeWindow ? <p className="text-xs text-muted-foreground">{timeWindow}</p> : null}
                    </div>

                    <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                      {TRANSITIONS[activity.status]?.map((transition) => (
                        <Button
                          key={transition.to}
                          size="sm"
                          variant="outline"
                          className="h-8"
                          disabled={statusMutation.isPending}
                          onClick={() => changeStatus(activity, transition.to)}
                        >
                          <transition.icon className="size-3.5" /> {transition.label}
                        </Button>
                      ))}
                      <Button size="sm" variant="ghost" onClick={() => setEditor({ open: true, activity })}>
                        <Pencil className="size-3.5" /> 编辑
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => onOpenStats(activity.id)}>
                        <BarChart3 className="size-3.5" /> 统计
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => onOpenDraws(activity.id, false)}>
                        <ReceiptText className="size-3.5" /> 记录
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setPendingDelete(activity)} aria-label="删除活动">
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  </div>

                  <div className="mt-3 flex flex-wrap gap-1">
                    {prizes.map((prize, index) => (
                      <Badge
                        key={prize.id}
                        variant={prize.kind === "none" ? "secondary" : "outline"}
                        size="sm"
                        className="gap-1 font-normal"
                        title={kindLabelOf(prize.kind, kinds)}
                      >
                        {prize.name}
                        <span className="font-mono tabular-nums text-muted-foreground">
                          {formatProbability(probabilities[index] ?? 0)}
                        </span>
                        {prize.quantity >= 0 ? (
                          <span className="tabular-nums text-muted-foreground">
                            {formatCount(prize.used)}/{formatCount(prize.quantity)}
                          </span>
                        ) : null}
                      </Badge>
                    ))}
                  </div>

                  <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span className="tabular-nums">
                      累计 <span className="font-medium text-foreground">{formatCount(activity.totalDraws)}</span> 次
                    </span>
                    <span className="tabular-nums">{formatCount(activity.totalUsers)} 人</span>
                    <span className="tabular-nums">今日 {formatCount(activity.todayDraws)} 次</span>
                    {activity.economy && activity.economy.price > 0 ? (
                      <span
                        className="tabular-nums"
                        title="积分能换到抽奖次数时，抽一次期望拿回的积分；永久会员抽中已拥有的奖品时折算会被封顶，保证不超过价格"
                      >
                        每次 {formatCount(activity.economy.price)} 积分，期望回报 {activity.economy.memberReturn < 0 ? "无上限" : activity.economy.memberReturn}
                        {activity.economy.lifetimeCap >= 0 ? `，永久会员折算上限 ${formatCount(activity.economy.lifetimeCap)}` : ""}
                      </span>
                    ) : null}
                    {activity.pending > 0 ? (
                      <button
                        type="button"
                        className="tabular-nums font-medium text-amber-600 hover:underline dark:text-amber-400"
                        onClick={() => onOpenDraws(activity.id, true)}
                      >
                        待发放 {activity.pending}
                      </button>
                    ) : null}
                  </div>
                  {activity.economy?.warnings.map((warning) => (
                    <p
                      key={warning}
                      role="alert"
                      className="mt-2 rounded-lg bg-destructive/10 px-3 py-2 text-xs leading-relaxed text-destructive"
                    >
                      {warning}
                    </p>
                  ))}
                </div>
              );
            })}
          </div>
        )}
      </SectionCard>

      <LotteryActivityEditor
        appKey={appKey}
        activity={editor.activity}
        open={editor.open}
        onOpenChange={(open) => setEditor((current) => ({ open, activity: open ? current.activity : null }))}
      />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除活动「{pendingDelete?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete?.totalDraws ? "已有抽奖记录的活动无法删除，可改为结束。" : "活动与奖品一并删除。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
