"use client";

import { useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  AtSign,
  Calculator,
  EyeOff,
  ListOrdered,
  ShieldCheck,
  Trophy,
  UserRound
} from "lucide-react";
import { toast } from "sonner";
import {
  FieldGroup,
  ModeCard,
  NumberField,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";
import {
  CATEGORY_RULE_META,
  IDENTITY_META,
  LeaderboardSaveBar,
  PERIOD_LABEL,
  isPeriodic,
  savedPayload,
  toInt
} from "@/components/apps/leaderboard/leaderboard-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RadioGroup } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api/client";
import type {
  LeaderboardBoardSetting,
  LeaderboardIdentity,
  LeaderboardSettings,
  LeaderboardSettingsPayload
} from "@/lib/api/leaderboard";
import type { PointsStoreRewardSpec } from "@/lib/api/points-store";
import { useLeaderboardSettingsQuery, useSaveLeaderboardSettingsMutation } from "@/lib/leaderboard-hooks";
import { cn } from "@/lib/utils";

const IDENTITY_ICON: Record<LeaderboardIdentity, React.ReactNode> = {
  nickname: <UserRound className="size-4" />,
  masked: <AtSign className="size-4" />
};

type Form = {
  enabled: boolean;
  boards: LeaderboardBoardSetting[];
  topLimit: string;
  minValue: string;
  identity: LeaderboardIdentity;
  allowAnonymous: boolean;
  countAdminAdjust: boolean;
};

function seed(settings: LeaderboardSettings): Form {
  return {
    enabled: settings.enabled,
    boards: settings.boards.map((board) => ({ key: board.key, enabled: board.enabled })),
    topLimit: String(settings.topLimit),
    minValue: String(settings.minValue),
    identity: settings.identity,
    allowAnonymous: settings.allowAnonymous,
    countAdminAdjust: settings.countAdminAdjust
  };
}

/** 本页只改设置这一半；奖励按服务端已保存的原样带上 */
function toPayload(form: Form, settings: LeaderboardSettings, catalog: PointsStoreRewardSpec[]): LeaderboardSettingsPayload {
  return {
    ...savedPayload(settings, catalog),
    enabled: form.enabled,
    boards: form.boards,
    topLimit: toInt(form.topLimit),
    minValue: toInt(form.minValue),
    identity: form.identity,
    allowAnonymous: form.allowAnonymous,
    countAdminAdjust: form.countAdminAdjust
  };
}

/**
 * 设置：总开关、榜单启用与标签顺序、展示名次、上榜门槛、展示身份、匿名与积分口径。
 *
 * 榜单、身份、上下界都来自 `GET settings` 的目录；校验失败（40000）的原因原样 toast。
 * 与奖励共用一个 PUT，这里保存时奖励按已保存的值带上。草稿按应用绑定，无草稿即从服务端派生。
 */
export function LeaderboardSettingsPanel({ appKey }: { appKey: string }) {
  const settingsQuery = useLeaderboardSettingsQuery(appKey);
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
        <Skeleton className="h-28 w-full rounded-2xl" />
        <Skeleton className="h-72 w-full rounded-2xl" />
        <Skeleton className="h-64 w-full rounded-2xl" />
      </div>
    );
  }

  const { settings, catalog, rewardCatalog, limits, integralCategories } = data;
  const identities = data.identities?.length ? data.identities : (["nickname", "masked"] as LeaderboardIdentity[]);
  const saved = seed(settings);
  const form = draft?.scope === appKey ? draft.value : saved;
  const dirty =
    JSON.stringify(toPayload(form, settings, rewardCatalog)) !== JSON.stringify(toPayload(saved, settings, rewardCatalog));
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope: appKey, value: { ...form, [key]: value } });

  const topLimit = toInt(form.topLimit);
  const minValue = toInt(form.minValue);
  const topLimitInvalid = topLimit < limits.minTopLimit || topLimit > limits.maxTopLimit;
  const minValueInvalid = form.minValue.trim() === "" || minValue < 0 || minValue > limits.maxMinValue;
  // 已保存的奖励档位超出新的展示名次：后端会拒绝，先提示
  const overflowing = Object.entries(settings.rewards ?? {}).filter(([, board]) =>
    (board.tiers ?? []).some((tier) => tier.rankTo > topLimit)
  );

  const moveBoard = (index: number, offset: -1 | 1) => {
    const target = index + offset;
    if (target < 0 || target >= form.boards.length) return;
    const boards = [...form.boards];
    [boards[index], boards[target]] = [boards[target], boards[index]];
    patch("boards", boards);
  };

  const toggleBoard = (key: string, enabled: boolean) =>
    patch(
      "boards",
      form.boards.map((board) => (board.key === key ? { ...board, enabled } : board))
    );

  const save = async () => {
    if (topLimitInvalid) {
      toast.error(`展示名次需要在 ${limits.minTopLimit}–${limits.maxTopLimit} 之间`);
      return;
    }
    if (minValueInvalid) {
      toast.error(`上榜门槛需要在 0–${limits.maxMinValue} 之间`);
      return;
    }
    try {
      await saveMutation.mutateAsync(toPayload(form, settings, rewardCatalog));
      setDraft(null);
      toast.success("排行榜设置已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Trophy className="size-4" />}
        title="排行榜"
        aside={
          <div className="flex items-center gap-2">
            {!settings.configured ? (
              <Badge variant="outline" size="sm">
                未配置
              </Badge>
            ) : null}
            <StatusDot active={settings.enabled} labelActive="已开启" labelInactive="未开启" />
          </div>
        }
      >
        <SwitchRow
          label="开启排行榜"
          hint="关闭后用户端显示排行榜暂未开放，周期榜不再结算"
          icon={<Trophy className="size-4" />}
          checked={form.enabled}
          onChange={(value) => patch("enabled", value)}
        />
      </SectionCard>

      <SectionCard icon={<ListOrdered className="size-4" />} title="榜单" description="顺序即客户端标签顺序">
        <div className="space-y-2">
          {form.boards.map((board, index) => {
            const spec = catalog.find((item) => item.key === board.key);
            return (
              <div
                key={board.key}
                className={cn(
                  "flex items-center gap-3 rounded-xl bg-muted px-3 py-2.5 transition-colors",
                  board.enabled && "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30"
                )}
              >
                <div className="flex shrink-0 flex-col">
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-6"
                    aria-label="上移"
                    disabled={index === 0}
                    onClick={() => moveBoard(index, -1)}
                  >
                    <ArrowUp className="size-3.5" />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-6"
                    aria-label="下移"
                    disabled={index === form.boards.length - 1}
                    onClick={() => moveBoard(index, 1)}
                  >
                    <ArrowDown className="size-3.5" />
                  </Button>
                </div>
                <div className="min-w-0 flex-1 space-y-0.5">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-xs font-medium">{spec?.label ?? board.key}</span>
                    {spec ? (
                      <Badge variant={isPeriodic(spec) ? "info" : "outline"} size="sm" className="font-normal">
                        {isPeriodic(spec) ? `按${PERIOD_LABEL[spec.period]}结算` : "累计"}
                      </Badge>
                    ) : null}
                  </div>
                  {spec?.description ? (
                    <p className="text-[11px] leading-snug text-muted-foreground">
                      {spec.description}
                      {spec.valueUnit ? `，单位${spec.valueUnit}` : ""}
                    </p>
                  ) : null}
                </div>
                <Switch
                  checked={board.enabled}
                  onCheckedChange={(value) => toggleBoard(board.key, value)}
                  aria-label={`开启${spec?.label ?? board.key}`}
                />
              </div>
            );
          })}
        </div>
      </SectionCard>

      <SectionCard icon={<UserRound className="size-4" />} title="展示">
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField
              label="展示名次"
              value={form.topLimit}
              onChange={(value) => patch("topLimit", value)}
              min={limits.minTopLimit}
              max={limits.maxTopLimit}
              unit={`前 N 名，${limits.minTopLimit}–${limits.maxTopLimit}`}
              hint="用户端只展示前 N 名，榜外的人只看到自己的名次；奖励档位不能超出它"
            />
            <NumberField
              label="上榜门槛"
              value={form.minValue}
              onChange={(value) => patch("minValue", value)}
              min={0}
              max={limits.maxMinValue}
              unit={`0–${limits.maxMinValue.toLocaleString("zh-CN")}`}
              hint="取值低于门槛的人不上榜"
            />
          </div>
          {topLimitInvalid ? (
            <p className="text-xs text-amber-600 dark:text-amber-400">
              展示名次需要在 {limits.minTopLimit}–{limits.maxTopLimit} 之间
            </p>
          ) : null}
          {!topLimitInvalid && overflowing.length > 0 ? (
            <p className="text-xs text-amber-600 dark:text-amber-400">
              {overflowing.map(([key]) => catalog.find((item) => item.key === key)?.label ?? key).join("、")}
              的奖励档位超出前 {topLimit} 名，需先调整奖励
            </p>
          ) : null}

          <FieldGroup label="展示身份">
            <RadioGroup value={form.identity} onValueChange={(value) => patch("identity", value as LeaderboardIdentity)}>
              <div className="grid gap-2 sm:grid-cols-2">
                {identities.map((identity) => (
                  <ModeCard
                    key={identity}
                    value={identity}
                    active={form.identity === identity}
                    title={IDENTITY_META[identity]?.title ?? identity}
                    description={IDENTITY_META[identity]?.description ?? ""}
                    icon={IDENTITY_ICON[identity] ?? <UserRound className="size-4" />}
                  />
                ))}
              </div>
            </RadioGroup>
            <p className="text-[11px] text-muted-foreground">用户端永远不返回原始账号</p>
          </FieldGroup>

          <SwitchRow
            label="允许匿名上榜"
            hint="用户可开启匿名，名次与奖励照常，其他人看到的是匿名用户"
            icon={<EyeOff className="size-4" />}
            checked={form.allowAnonymous}
            onChange={(value) => patch("allowAnonymous", value)}
          />
        </div>
      </SectionCard>

      <SectionCard
        icon={<Calculator className="size-4" />}
        title="积分口径"
        description="周期榜统计周期内获得的积分，只计正数流水"
      >
        <div className="space-y-4">
          <SwitchRow
            label="计入管理员调整"
            hint="只影响控制台里由管理员操作的调整；抽奖与远程函数发放的积分始终计入"
            icon={<ShieldCheck className="size-4" />}
            checked={form.countAdminAdjust}
            onChange={(value) => patch("countAdminAdjust", value)}
          />
          {integralCategories?.length ? (
            <div className="grid gap-x-4 gap-y-1.5 sm:grid-cols-2">
              {integralCategories.map((item) => {
                const rule =
                  item.rule === "admin" ? (form.countAdminAdjust ? "counted" : "excluded") : item.rule;
                const meta = CATEGORY_RULE_META[rule] ?? CATEGORY_RULE_META.counted;
                return (
                  <div key={item.category} className="flex items-center justify-between gap-2 py-1">
                    <span className="min-w-0 truncate text-xs">
                      {item.label}
                      <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{item.category}</span>
                    </span>
                    <Badge variant={meta.variant} size="sm" className="shrink-0 font-normal">
                      {meta.label}
                    </Badge>
                  </div>
                );
              })}
            </div>
          ) : null}
        </div>
      </SectionCard>

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
