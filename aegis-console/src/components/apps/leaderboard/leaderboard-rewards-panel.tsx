"use client";

import { useState } from "react";
import { Gift, Info, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { NumberField, SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  LeaderboardSaveBar,
  PERIOD_LABEL,
  isPeriodic,
  savedPayload,
  tierLabel,
  toInt
} from "@/components/apps/leaderboard/leaderboard-shared";
import {
  RewardListEditor,
  rewardDraftsFrom,
  rewardsFromDrafts,
  type RewardDraft
} from "@/components/apps/rewards/reward-list-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type {
  LeaderboardBoardRewards,
  LeaderboardBoardSpec,
  LeaderboardSettings
} from "@/lib/api/leaderboard";
import type { PointsStoreRewardSpec } from "@/lib/api/points-store";
import { useLeaderboardSettingsQuery, useSaveLeaderboardSettingsMutation } from "@/lib/leaderboard-hooks";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

type TierDraft = { key: number; rankFrom: string; rankTo: string; rewards: RewardDraft[] };
type BoardDraft = { enabled: boolean; tiers: TierDraft[] };
type Form = Record<string, BoardDraft>;

function seed(settings: LeaderboardSettings, boards: LeaderboardBoardSpec[]): Form {
  const form: Form = {};
  for (const spec of boards) {
    const saved = settings.rewards?.[spec.key];
    form[spec.key] = {
      enabled: Boolean(saved?.enabled),
      tiers: (saved?.tiers ?? []).map((tier, index) => ({
        key: index + 1,
        rankFrom: String(tier.rankFrom),
        rankTo: String(tier.rankTo),
        rewards: rewardDraftsFrom(tier.rewards)
      }))
    };
  }
  return form;
}

function toRewards(form: Form, catalog: PointsStoreRewardSpec[]): Record<string, LeaderboardBoardRewards> {
  const out: Record<string, LeaderboardBoardRewards> = {};
  for (const [key, board] of Object.entries(form)) {
    out[key] = {
      enabled: board.enabled,
      tiers: board.tiers.map((tier) => ({
        rankFrom: toInt(tier.rankFrom),
        rankTo: toInt(tier.rankTo),
        rewards: rewardsFromDrafts(tier.rewards, catalog)
      }))
    };
  }
  return out;
}

/** 前端先看一遍档位，说法与后端 ValidateTiers 一致；最终以后端校验为准 */
function tierIssues(board: BoardDraft, topLimit: number) {
  const issues: string[] = [];
  const tiers = [...board.tiers]
    .map((tier) => ({ from: toInt(tier.rankFrom), to: toInt(tier.rankTo), rewards: tier.rewards.length }))
    .sort((a, b) => a.from - b.from);
  let previousTo = 0;
  for (const tier of tiers) {
    const label = tierLabel(tier.from, tier.to);
    if (tier.from < 1 || tier.to < tier.from) issues.push(`${label}的名次范围不合法`);
    else if (tier.to > topLimit) issues.push(`${label}超出了展示名次（前 ${topLimit} 名）`);
    else if (tier.from <= previousTo) issues.push(`${label}与上一档的名次重叠`);
    if (tier.rewards === 0) issues.push(`${label}没有配置奖励`);
    previousTo = Math.max(previousTo, tier.to);
  }
  return issues;
}

/**
 * 奖励：每个周期榜（日 / 周 / 月）一组名次档位，结算时按名次落档发放；并列名次按名次落档。
 *
 * 权益目录、档位上限与每档权益上限都来自 `GET settings`；会员套餐去掉试用套餐，与邀请奖励同一取法。
 * 与设置共用一个 PUT，这里保存时设置按已保存的值带上。总榜、经验榜、等级榜不结算，不出现在这里。
 */
export function LeaderboardRewardsPanel({ appKey }: { appKey: string }) {
  const settingsQuery = useLeaderboardSettingsQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSaveLeaderboardSettingsMutation(appKey);
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);

  const data = settingsQuery.data;

  if (settingsQuery.isLoading || !data) {
    if (settingsQuery.isError) {
      return (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {settingsQuery.error instanceof ApiError ? settingsQuery.error.message : "排行榜设置加载失败"}
        </p>
      );
    }
    return (
      <div className="space-y-4">
        <Skeleton className="h-16 w-full rounded-2xl" />
        <Skeleton className="h-64 w-full rounded-2xl" />
        <Skeleton className="h-64 w-full rounded-2xl" />
      </div>
    );
  }

  const { settings, catalog, rewardCatalog, limits } = data;
  const plans = (plansQuery.data ?? []).filter((plan) => plan.kind !== "trial");
  const features = featuresQuery.data ?? [];
  // 按设置里的标签顺序排，目录里有但设置里没有的排在末尾
  const order = settings.boards.map((board) => board.key);
  const periodicBoards = catalog
    .filter((spec) => isPeriodic(spec))
    .sort((a, b) => {
      const ia = order.indexOf(a.key);
      const ib = order.indexOf(b.key);
      return (ia < 0 ? Number.MAX_SAFE_INTEGER : ia) - (ib < 0 ? Number.MAX_SAFE_INTEGER : ib);
    });
  const fixedBoards = catalog.filter((spec) => !isPeriodic(spec));

  const saved = seed(settings, periodicBoards);
  const form = draft?.scope === appKey ? draft.value : saved;
  const dirty =
    JSON.stringify(toRewards(form, rewardCatalog)) !== JSON.stringify(toRewards(saved, rewardCatalog));

  const patchBoard = (key: string, next: Partial<BoardDraft>) =>
    setDraft({ scope: appKey, value: { ...form, [key]: { ...form[key], ...next } } });

  const patchTier = (boardKey: string, tierKey: number, next: Partial<TierDraft>) =>
    patchBoard(boardKey, {
      tiers: form[boardKey].tiers.map((tier) => (tier.key === tierKey ? { ...tier, ...next } : tier))
    });

  const addTier = (boardKey: string) => {
    const tiers = form[boardKey].tiers;
    const lastTo = tiers.reduce((max, tier) => Math.max(max, toInt(tier.rankTo)), 0);
    const rank = Math.min(lastTo + 1, settings.topLimit);
    const key = tiers.reduce((max, tier) => Math.max(max, tier.key), 0) + 1;
    patchBoard(boardKey, { tiers: [...tiers, { key, rankFrom: String(rank), rankTo: String(rank), rewards: [] }] });
  };

  const save = async () => {
    try {
      await saveMutation.mutateAsync({ ...savedPayload(settings, rewardCatalog), rewards: toRewards(form, rewardCatalog) });
      setDraft(null);
      toast.success("排行榜奖励已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-2.5 rounded-xl border border-border bg-muted/40 px-4 py-3 text-xs text-muted-foreground">
        <Info className="mt-0.5 size-3.5 shrink-0" />
        <p className="leading-relaxed">
          周期结束后自动结算，按结算那一刻的档位发放，之后修改不影响已结算的周期。并列名次按名次落档。
          每个榜最多 {limits.maxTiers} 档，每档最多 {limits.maxRewardsInTier} 项权益，名次不能超出前 {settings.topLimit} 名。
          {fixedBoards.length ? `${fixedBoards.map((spec) => spec.label).join("、")}不结算，不能配置奖励。` : null}
        </p>
      </div>

      {periodicBoards.map((spec) => {
        const board = form[spec.key];
        const boardEnabled = settings.boards.find((item) => item.key === spec.key)?.enabled ?? false;
        const issues = tierIssues(board, settings.topLimit);
        return (
          <SectionCard
            key={spec.key}
            icon={<Gift className="size-4" />}
            title={spec.label}
            description={`按${PERIOD_LABEL[spec.period]}结算，${spec.description}`}
            aside={
              <div className="flex items-center gap-1.5">
                {boardEnabled ? null : (
                  <Badge variant="secondary" size="sm">
                    榜单未开启
                  </Badge>
                )}
                <Badge variant="outline" size="sm" className="tabular-nums">
                  {board.tiers.length}/{limits.maxTiers} 档
                </Badge>
              </div>
            }
          >
            <div className="space-y-3">
              <SwitchRow
                label="发放奖励"
                hint={boardEnabled ? "关闭时照常结算快照，不发奖励" : "榜单未开启时不结算，奖励也不会发放"}
                icon={<Gift className="size-4" />}
                checked={board.enabled}
                onChange={(value) => patchBoard(spec.key, { enabled: value })}
              />

              {board.tiers.length === 0 ? (
                <p className="rounded-lg border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
                  暂无档位
                </p>
              ) : null}

              {board.tiers.map((tier) => (
                <div key={tier.key} className="space-y-3 rounded-xl border border-border p-4">
                  <div className="flex items-end gap-3">
                    <div className="grid flex-1 gap-4 sm:grid-cols-3">
                      <NumberField
                        label="名次从"
                        value={tier.rankFrom}
                        onChange={(value) => patchTier(spec.key, tier.key, { rankFrom: value })}
                        min={1}
                        max={settings.topLimit}
                      />
                      <NumberField
                        label="名次到"
                        value={tier.rankTo}
                        onChange={(value) => patchTier(spec.key, tier.key, { rankTo: value })}
                        min={1}
                        max={settings.topLimit}
                      />
                      <div className="flex items-end pb-2 text-xs font-medium">
                        {tierLabel(toInt(tier.rankFrom), toInt(tier.rankTo))}
                      </div>
                    </div>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-9 shrink-0"
                      aria-label="移除档位"
                      onClick={() =>
                        patchBoard(spec.key, { tiers: board.tiers.filter((item) => item.key !== tier.key) })
                      }
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                  <RewardListEditor
                    catalog={rewardCatalog}
                    maxRewards={limits.maxRewardsInTier}
                    rewards={tier.rewards}
                    plans={plans}
                    features={features}
                    emptyText="暂无奖励"
                    addLabel="添加奖励"
                    onChange={(rewards) => patchTier(spec.key, tier.key, { rewards })}
                  />
                </div>
              ))}

              {issues.length > 0 ? (
                <ul className="space-y-0.5 text-xs text-amber-600 dark:text-amber-400">
                  {issues.map((issue) => (
                    <li key={issue}>{issue}</li>
                  ))}
                </ul>
              ) : null}

              {board.tiers.length < limits.maxTiers ? (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
                  onClick={() => addTier(spec.key)}
                >
                  <Plus className="size-3.5" /> 添加档位
                </Button>
              ) : null}
            </div>
          </SectionCard>
        );
      })}

      <LeaderboardSaveBar
        settings={settings}
        dirty={dirty}
        pending={saveMutation.isPending}
        onReset={() => setDraft(null)}
        onSave={() => void save()}
      />
    </div>
  );
}
