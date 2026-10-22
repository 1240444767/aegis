"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, ArrowRight, Ban, Coins, Disc3, FlaskConical, Gift, Grid3x3, Loader2, Save, Ticket } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, ModeCard, NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  LIMITS,
  OWNABLE_KINDS,
  blankPrize,
  nextPrizeKey,
  rewardOf,
  seedForm,
  swapPreview,
  toPayload,
  validateForm,
  weightedOf,
  type ActivityForm
} from "@/components/apps/lottery/lottery-activity-form";
import { LotteryPrizeBoard } from "@/components/apps/lottery/lottery-prize-board";
import { LotteryStrategyEditor } from "@/components/apps/lottery/lottery-strategy-editor";
import { LotteryStrategySimulator } from "@/components/apps/lottery/lottery-strategy-simulator";
import {
  LIFETIME_POLICIES,
  LIFETIME_POLICY_META,
  LOTTERY_STATUSES,
  UI_MODE_LABEL,
  defaultPrizeName,
  lotteryStatusLabel,
  prizeProbabilities
} from "@/components/apps/lottery/lottery-shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { RadioGroup } from "@/components/ui/radio-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle
} from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { LotteryAdminActivity, LotteryLifetimePolicy, LotteryStatus, LotteryUIMode } from "@/lib/api/lottery";
import { useLotteryCatalogQuery, useSaveLotteryActivityMutation } from "@/lib/lottery-hooks";

/** Select 不接受空串作为选项值：「不开放视频抽奖」用这个占位 */
const NO_SCENE = "__none";

const POLICY_ICON: Record<LotteryLifetimePolicy, React.ReactNode> = {
  points: <Coins className="size-3.5" />,
  draws: <Ticket className="size-3.5" />,
  exclude: <Ban className="size-3.5" />,
  grant: <Gift className="size-3.5" />
};

/**
 * 新建 / 编辑一个活动（连同全部奖品，一次保存）。
 *
 * 奖品与活动是一个整体：九宫格恰好 8 格、保底奖必须是本活动的奖品、至少一个不限库存的奖品，
 * 都是「整张奖盘」的约束，后端只接受整体保存。已有奖品带着 id 回传才能保留已抽中数量。
 * 草稿形状与积分商城编辑器相同：抽屉关闭即清空，不用 effect 同步。
 */
export function LotteryActivityEditor({
  appKey,
  activity,
  open,
  onOpenChange
}: {
  appKey: string;
  /** 为空即新建 */
  activity: LotteryAdminActivity | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const catalogQuery = useLotteryCatalogQuery(appKey);
  const saveMutation = useSaveLotteryActivityMutation(appKey);
  const catalog = catalogQuery.data;

  const [draft, setDraft] = useState<{ id: number; value: ActivityForm } | null>(null);
  const [selectedKey, setSelectedKey] = useState<number | null>(null);
  const [simulating, setSimulating] = useState(false);
  const scope = activity?.id ?? 0;
  const form = draft?.id === scope ? draft.value : seedForm(activity, catalog);
  const update = (next: Partial<ActivityForm>) => setDraft({ id: scope, value: { ...form, ...next } });
  const patch = <K extends keyof ActivityForm>(key: K, value: ActivityForm[K]) => update({ [key]: value } as Partial<ActivityForm>);

  const kinds = catalog?.kinds ?? [];
  const plans = catalog?.plans ?? [];
  const features = catalog?.features ?? [];
  const scenes = catalog?.videoScenes ?? [];
  const gridCount = catalog?.gridPrizeCount ?? 8;

  const probabilities = useMemo(() => prizeProbabilities(form.prizes.map(weightedOf)), [form.prizes]);
  const issues = useMemo(() => validateForm(form, catalog), [form, catalog]);
  const prizeIssues = issues.filter((issue) => issue.scope === "prizes" || issue.scope === "strategy");

  const close = () => {
    setDraft(null);
    setSelectedKey(null);
    onOpenChange(false);
  };

  /** 转盘切到九宫格：多于 8 个不替人删，少于 8 个补「谢谢参与」（权重 0，不改变现有概率） */
  const changeMode = (uiMode: LotteryUIMode) => {
    if (uiMode === form.uiMode) return;
    if (uiMode === "grid" && form.prizes.length > gridCount) {
      toast.error(`九宫格需要恰好 ${gridCount} 个奖品，请先删到 ${gridCount} 个`);
      return;
    }
    const prizes = [...form.prizes];
    while (uiMode === "grid" && prizes.length < gridCount) {
      prizes.push(blankPrize(nextPrizeKey(prizes), "none", 0, 0));
    }
    update({ uiMode, prizes });
  };

  const pityCandidates = form.prizes
    .map((prize, index) => ({ prize, index }))
    .filter(({ prize }) => prize.type !== "none");
  const pityPrize = form.prizes.find((prize) => prize.key === form.pityKey);
  const pityThreshold = Number(form.pityThreshold) || 0;
  const prizeName = (key: number) => {
    const prize = form.prizes.find((item) => item.key === key);
    if (!prize) return "";
    return prize.name.trim() || defaultPrizeName(rewardOf(prize, kinds), kinds, plans, features);
  };

  const policies = catalog?.lifetimePolicies?.length
    ? catalog.lifetimePolicies
    : LIFETIME_POLICIES.map((key) => ({ key, ...LIFETIME_POLICY_META[key] }));
  /** 奖盘里会被「已拥有」的格子，配一行「抽中 X → 改发 Y」的实时示例 */
  const ownablePrizes = form.prizes.filter((prize) => OWNABLE_KINDS.includes(prize.type));
  const showPoints = form.lifetimePolicy === "points" || form.lifetimePolicy === "exclude";

  const currentScene = scenes.find((scene) => scene.key === form.videoScene);
  const sceneHint = !catalog
    ? undefined
    : !catalog.rewardedAds
      ? "激励广告未开启"
      : scenes.length === 0
        ? "暂无发放抽奖次数的激励广告场景"
        : form.videoScene && !currentScene
          ? "该场景已不再发放抽奖次数"
          : currentScene && !currentScene.enabled
            ? "该场景已停用"
            : undefined;

  const submit = async () => {
    if (issues.length > 0) {
      toast.error(issues[0].message);
      return;
    }
    try {
      await saveMutation.mutateAsync({ activityId: activity?.id, payload: toPayload(form, kinds, catalog) });
      toast.success(activity ? "活动已保存" : "活动已创建");
      close();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setDraft(null);
          setSelectedKey(null);
        }
        onOpenChange(next);
      }}
    >
      <SheetContent side="right" className="flex w-[96vw] max-w-3xl flex-col gap-0 p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{activity ? "编辑活动" : "新建活动"}</SheetTitle>
          <SheetDescription>{activity ? activity.name : "抽奖"}</SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-6 px-6 py-5">
            <FieldGroup label="基本信息">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5 sm:col-span-2">
                  <span className="text-[11px] font-medium">活动名称</span>
                  <Input
                    value={form.name}
                    maxLength={LIMITS.name}
                    onChange={(event) => patch("name", event.target.value)}
                    placeholder="例如：每日幸运抽奖"
                  />
                </div>
                <div className="space-y-1.5 sm:col-span-2">
                  <span className="text-[11px] font-medium">简介</span>
                  <Input
                    value={form.description}
                    maxLength={LIMITS.description}
                    onChange={(event) => patch("description", event.target.value)}
                  />
                </div>
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">状态</span>
                  <Select value={form.status} onValueChange={(value) => patch("status", value as LotteryStatus)}>
                    <SelectTrigger className="h-9 w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {(catalog?.statuses ?? LOTTERY_STATUSES).map((status) => (
                        <SelectItem key={status} value={status}>
                          {lotteryStatusLabel(status)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <NumberField label="排序" value={form.sortOrder} onChange={(value) => patch("sortOrder", value)} unit="越小越前" />
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">开始时间</span>
                  <Input type="datetime-local" value={form.startTime} onChange={(event) => patch("startTime", event.target.value)} />
                </div>
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">结束时间</span>
                  <Input type="datetime-local" value={form.endTime} onChange={(event) => patch("endTime", event.target.value)} />
                </div>
              </div>
            </FieldGroup>

            <FieldGroup label="抽奖次数">
              <div className="grid gap-4 sm:grid-cols-3">
                <NumberField
                  label="每日免费"
                  value={form.freeDaily}
                  min={0}
                  max={LIMITS.freeDaily}
                  unit="次"
                  onChange={(value) => patch("freeDaily", value)}
                />
                <NumberField
                  label="每次消耗积分"
                  value={form.pointsCost}
                  min={0}
                  max={LIMITS.pointsCost}
                  unit="0 为不开放"
                  onChange={(value) => patch("pointsCost", value)}
                />
                <NumberField
                  label="积分抽奖每日上限"
                  value={form.pointsDaily}
                  min={0}
                  max={LIMITS.pointsDaily}
                  unit="次，0 为不限"
                  disabled={!(Number(form.pointsCost) > 0)}
                  onChange={(value) => patch("pointsDaily", value)}
                />
              </div>
              <div className="space-y-1.5">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="text-[11px] font-medium">视频抽奖</span>
                  {sceneHint ? <span className="text-[10px] text-amber-600 dark:text-amber-400">{sceneHint}</span> : null}
                </div>
                <Select
                  value={form.videoScene || NO_SCENE}
                  onValueChange={(value) => patch("videoScene", value === NO_SCENE ? "" : value)}
                >
                  <SelectTrigger className="h-9 w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NO_SCENE}>不开放</SelectItem>
                    {scenes.map((scene) => (
                      <SelectItem key={scene.key} value={scene.key}>
                        {scene.name || scene.key}
                        <span className="ml-1.5 text-[11px] text-muted-foreground">
                          看一次得 {scene.drawsPerView} 次
                          {scene.dailyLimit > 0 ? `，每日 ${scene.dailyLimit} 次` : ""}
                          {scene.enabled ? "" : "，已停用"}
                        </span>
                      </SelectItem>
                    ))}
                    {form.videoScene && !currentScene ? (
                      <SelectItem value={form.videoScene}>{form.videoScene}</SelectItem>
                    ) : null}
                  </SelectContent>
                </Select>
              </div>
            </FieldGroup>

            <FieldGroup
              label="奖品"
              hint={
                form.uiMode === "grid"
                  ? `${form.prizes.length}/${gridCount}`
                  : `${form.prizes.length}/${catalog?.wheelMaxPrizes ?? 12}`
              }
            >
              <ToggleGroup
                type="single"
                variant="outline"
                size="sm"
                value={form.uiMode}
                onValueChange={(value) => value && changeMode(value as LotteryUIMode)}
              >
                {(catalog?.uiModes ?? (["grid", "wheel"] as LotteryUIMode[])).map((mode) => (
                  <ToggleGroupItem key={mode} value={mode} className="h-8 px-3 text-xs">
                    {mode === "grid" ? <Grid3x3 className="size-3.5" /> : <Disc3 className="size-3.5" />}
                    {UI_MODE_LABEL[mode]}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
              <LotteryPrizeBoard
                uiMode={form.uiMode}
                prizes={form.prizes}
                probabilities={probabilities}
                selectedKey={selectedKey}
                pityKey={form.pity ? form.pityKey : null}
                catalog={catalog}
                swapPreview={(prize) => swapPreview(form, prize, catalog)}
                onSelect={setSelectedKey}
                onChange={(prizes) =>
                  update({
                    prizes,
                    pityKey: prizes.some((prize) => prize.key === form.pityKey && prize.type !== "none")
                      ? form.pityKey
                      : null
                  })
                }
              />
            </FieldGroup>

            <FieldGroup label="保底">
              <SwitchRow label="开启保底" checked={form.pity} onChange={(value) => patch("pity", value)} />
              {form.pity ? (
                <div className="grid gap-4 sm:grid-cols-2">
                  <NumberField
                    label="保底次数"
                    value={form.pityThreshold}
                    min={2}
                    max={LIMITS.pity}
                    unit="次"
                    onChange={(value) => patch("pityThreshold", value)}
                  />
                  <div className="space-y-1.5">
                    <span className="text-[11px] font-medium">保底奖品</span>
                    <Select
                      value={pityPrize ? String(pityPrize.key) : ""}
                      onValueChange={(value) => patch("pityKey", Number(value))}
                    >
                      <SelectTrigger className="h-9 w-full">
                        <SelectValue placeholder="选择奖品" />
                      </SelectTrigger>
                      <SelectContent position="popper">
                        {pityCandidates.map(({ prize, index }) => (
                          <SelectItem key={prize.key} value={String(prize.key)}>
                            <span className="mr-1.5 tabular-nums text-muted-foreground">{index + 1}</span>
                            {prizeName(prize.key)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  {pityPrize && pityThreshold >= 2 ? (
                    <p className="text-xs text-muted-foreground sm:col-span-2">
                      连续 {pityThreshold - 1} 次未中后，第 {pityThreshold} 次必中「{prizeName(pityPrize.key)}」
                      {pityPrize.unlimited ? "" : "，保底奖抽完后不再保底"}
                    </p>
                  ) : null}
                </div>
              ) : null}
            </FieldGroup>

            <LotteryStrategyEditor
              value={form.strategy}
              onChange={(strategy) => patch("strategy", strategy)}
              catalog={catalog}
              prizeLabels={form.prizes.map((prize) => prizeName(prize.key))}
            />

            <FieldGroup label="已拥有的奖品">
              <p className="text-xs text-muted-foreground">
                永久会员抽中会员天数或已包含的套餐、功能，或用户已永久解锁某个功能时，这一格对他不再有新的权益。抽奖时由服务端按下面的方式处理。
              </p>
              <RadioGroup
                value={form.lifetimePolicy}
                onValueChange={(value) => patch("lifetimePolicy", value as LotteryLifetimePolicy)}
                className="grid gap-2 sm:grid-cols-2"
              >
                {policies.map((policy) => (
                  <ModeCard
                    key={policy.key}
                    value={policy.key}
                    active={form.lifetimePolicy === policy.key}
                    title={policy.label}
                    description={policy.hint}
                    icon={POLICY_ICON[policy.key] ?? <Gift className="size-3.5" />}
                  />
                ))}
              </RadioGroup>
              {showPoints ? (
                <div className="grid gap-4 sm:grid-cols-2">
                  <NumberField
                    label="每天折算积分"
                    value={form.lifetimePointsPerDay}
                    min={0}
                    max={LIMITS.lifetimePointsPerDay}
                    unit="积分/天"
                    onChange={(value) => patch("lifetimePointsPerDay", value)}
                  />
                  <NumberField
                    label="永久奖品折算积分"
                    value={form.lifetimePointsLifetime}
                    min={0}
                    max={LIMITS.lifetimePointsLifetime}
                    unit="永久套餐与永久功能"
                    onChange={(value) => patch("lifetimePointsLifetime", value)}
                  />
                </div>
              ) : null}
              {form.lifetimePolicy === "draws" ? (
                <div className="grid gap-4 sm:grid-cols-2">
                  <NumberField
                    label="改发次数"
                    value={form.lifetimeDraws}
                    min={1}
                    max={LIMITS.lifetimeDraws}
                    unit="次"
                    onChange={(value) => patch("lifetimeDraws", value)}
                  />
                </div>
              ) : null}
              {form.lifetimePolicy !== "grant" && ownablePrizes.length > 0 ? (
                <div className="space-y-1 rounded-lg border border-dashed border-border px-3 py-2">
                  <span className="text-[11px] font-medium text-muted-foreground">本活动的会员与功能奖品</span>
                  {ownablePrizes.map((prize) => {
                    const target = swapPreview(form, prize, catalog);
                    return (
                      <div key={prize.key} className="flex items-center gap-1.5 text-xs">
                        <span className="truncate">{prizeName(prize.key)}</span>
                        <ArrowRight className="size-3 shrink-0 text-muted-foreground" />
                        <span className="shrink-0 font-medium tabular-nums">{target ?? "照常发放"}</span>
                        {form.lifetimePolicy === "exclude" && Number(prize.weight) > 0 ? (
                          <span className="shrink-0 text-muted-foreground">（仅保底时）</span>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              ) : null}
            </FieldGroup>

            <FieldGroup label="活动规则" hint={`${Array.from(form.rules).length}/${LIMITS.rules}`}>
              <Textarea
                value={form.rules}
                maxLength={LIMITS.rules}
                rows={5}
                onChange={(event) => patch("rules", event.target.value)}
              />
            </FieldGroup>
          </div>
        </ScrollArea>

        {prizeIssues.length > 0 ? (
          <div className="shrink-0 space-y-0.5 border-t bg-amber-500/5 px-6 py-2 text-xs text-amber-700 dark:text-amber-400">
            {prizeIssues.slice(0, 3).map((issue) => (
              <p key={issue.message} className="flex items-center gap-1.5">
                <AlertTriangle className="size-3.5 shrink-0" />
                {issue.message}
              </p>
            ))}
            {prizeIssues.length > 3 ? <p className="pl-5">另有 {prizeIssues.length - 3} 项</p> : null}
          </div>
        ) : null}

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button
            variant="outline"
            size="sm"
            className="mr-auto"
            onClick={() => {
              if (issues.length > 0) {
                toast.error(issues[0].message);
                return;
              }
              setSimulating(true);
            }}
            disabled={catalogQuery.isLoading}
          >
            <FlaskConical className="size-3.5" />
            模拟
          </Button>
          <Button variant="ghost" size="sm" onClick={close}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={saveMutation.isPending || catalogQuery.isLoading}>
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </SheetContent>
      <LotteryStrategySimulator
        appKey={appKey}
        activityId={activity?.id}
        payload={() => toPayload(form, kinds, catalog)}
        catalog={catalog}
        open={simulating}
        onOpenChange={setSimulating}
      />
    </Sheet>
  );
}
