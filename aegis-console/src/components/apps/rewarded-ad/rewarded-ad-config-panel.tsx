"use client";

import { useState } from "react";
import { Eye, EyeOff, Gift, Loader2, Plug, Plus, Save, ShieldCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  FieldGroup,
  ModeCard,
  NumberField,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";
import { VERIFY_MODE_META } from "@/components/apps/rewarded-ad/rewarded-ad-shared";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";
import { CopyButton } from "@/components/profile/profile-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api/client";
import type { CardKeyReward, CardKeyRewardSpec } from "@/lib/api/card-key";
import type {
  RewardedAdConfig,
  RewardedAdLifetime,
  RewardedAdLifetimePolicy,
  RewardedAdLifetimePolicySpec,
  RewardedAdScene,
  RewardedAdVerifyMode
} from "@/lib/api/rewarded-ad";
import type { VipFeature } from "@/lib/api/vip";
import { useRewardedAdConfigQuery, useSaveRewardedAdConfigMutation } from "@/lib/rewarded-ad-hooks";
import { cn } from "@/lib/utils";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

/**
 * Apex 回调地址要自带参数宏（见 doc.apexadx.com/server/reward-callback），平台看完广告后替换成实际值再 GET。
 * 参数名与后端回调处理器认的名字一致。
 */
const CALLBACK_PARAMS: [string, string][] = [
  ["userId", "USER_ID"],
  ["transId", "TRANS_ID"],
  ["sign", "SIGN"],
  ["extrainfo", "EXTRAINFO"],
  ["placementId", "PLACEMENT_ID"]
];
const CALLBACK_QUERY = `?${CALLBACK_PARAMS.map(([name, macro]) => `${name}={{${macro}}}`).join("&")}`;

const PROVIDER_LABELS: Record<string, string> = { apex: "Apex" };

/**
 * 数字项一律按字符串存（理由同卡密批次编辑器：存 number 的话输入框清不空）。
 * uid 只给 React 列表用，场景标识可以被改，不能拿它当 key。
 */
type SceneDraft = {
  uid: string;
  key: string;
  name: string;
  placementId: string;
  enabled: boolean;
  dailyLimit: string;
  cooldownSeconds: string;
  /** feature 只对取值形态为 feature 的档位（功能时长）有意义 */
  rewards: Record<string, { amount: string; refId: number; feature: string }>;
  /** 会员权益：「会员天数」带的功能标识，以及送出的会员是否免广告 */
  features: string[];
  adFree: boolean;
  lifetime: LifetimeDraft;
};

/** 已拥有权益的处理方式。数字同样按字符串存；replace 的改发按档位存数量。 */
type LifetimeDraft = {
  policy: RewardedAdLifetimePolicy;
  target: string;
  perVipDay: string;
  perFeatureHour: string;
  rewards: Record<string, string>;
};

/** 送会员的两档：会员权益只在场景勾了其中一档时才有意义。 */
const VIP_DAYS = "vip_days";
const VIP_PLAN = "vip_plan";

/** 「功能时长」：永久会员使用时免扣，也算可能已拥有的一档 */
const FEATURE_MINUTES = "feature_minutes";
const OWNABLE = [VIP_DAYS, VIP_PLAN, FEATURE_MINUTES];

/** 后端没下发目录时的兜底（与服务端 rewardedad.LifetimePolicyCatalog 一致） */
const FALLBACK_LIFETIME_POLICIES: RewardedAdLifetimePolicySpec[] = [
  { key: "convert", label: "按价值折算", hint: "会员按天、功能时长按小时折成积分、AI 额度、经验或抽奖次数，其余权益照发" },
  { key: "replace", label: "改发固定权益", hint: "已拥有的权益不再发放，改发一组固定的积分、经验、抽奖次数或 AI 额度" },
  { key: "hide", label: "不展示该场景", hint: "对已拥有的用户隐藏这个场景；仍有观看到达时按价值折算" },
  { key: "grant", label: "照常发放", hint: "仍记一段会员或功能时长（永久会员实际拿不到新东西）" }
];
const FALLBACK_CONVERT_TARGETS = ["integral", "ai_credit", "experience", "lottery_draws"];

function lifetimeToDraft(rule?: RewardedAdLifetime): LifetimeDraft {
  const rewards: Record<string, string> = {};
  (rule?.rewards ?? []).forEach((reward) => {
    rewards[reward.type] = String(reward.amount ?? 1);
  });
  const unset = !rule?.perVipDay && !rule?.perFeatureHour;
  return {
    policy: rule?.policy || "convert",
    target: rule?.target || "integral",
    perVipDay: String(unset ? 10 : rule?.perVipDay ?? 0),
    perFeatureHour: String(unset ? 10 : rule?.perFeatureHour ?? 0),
    rewards
  };
}

function lifetimeToPayload(draft: LifetimeDraft): RewardedAdLifetime {
  switch (draft.policy) {
    case "replace":
      return {
        policy: "replace",
        rewards: Object.entries(draft.rewards).map(([type, amount]) => ({ type, amount: Number(amount) || 0 }))
      };
    case "grant":
      return { policy: "grant" };
    default:
      return {
        policy: draft.policy,
        target: draft.target,
        perVipDay: Number(draft.perVipDay) || 0,
        perFeatureHour: Number(draft.perFeatureHour) || 0
      };
  }
}

/** 功能时长新勾选时的默认分钟数，再夹到目录给的上下界内 */
const DEFAULT_FEATURE_MINUTES = 60;

type Draft = {
  scope: string;
  enabled: boolean;
  provider: string;
  providerAppId: string;
  verifyMode: RewardedAdVerifyMode;
  dailyLimit: string;
  scenes: SceneDraft[];
};

let uidSeed = 0;
const nextUid = () => `scene-${++uidSeed}`;

// 从服务端数据播种的场景用下标作 uid：草稿未落地前每次渲染都会重新播种，
// 随机 uid 会让输入框在每次重渲染时被卸载重建。
function sceneToDraft(scene: RewardedAdScene, index: number): SceneDraft {
  const rewards: SceneDraft["rewards"] = {};
  scene.rewards.forEach((reward) => {
    rewards[reward.type] = {
      amount: String(reward.amount ?? 1),
      refId: reward.refId ?? 0,
      feature: reward.feature ?? ""
    };
  });
  return {
    uid: `seed-${index}`,
    key: scene.key,
    name: scene.name,
    placementId: scene.placementId,
    enabled: scene.enabled,
    dailyLimit: String(scene.dailyLimit ?? 0),
    cooldownSeconds: String(scene.cooldownSeconds ?? 0),
    rewards,
    features: scene.membership?.features ?? [],
    // 没设置过的场景按免广告（与服务端默认一致）
    adFree: scene.membership?.adFree ?? true,
    lifetime: lifetimeToDraft(scene.lifetime)
  };
}

function seedDraft(scope: string, config?: RewardedAdConfig): Draft {
  return {
    scope,
    enabled: config?.enabled ?? false,
    provider: config?.provider || "apex",
    providerAppId: config?.providerAppId ?? "",
    verifyMode: config?.verifyMode ?? "dual",
    dailyLimit: String(config?.dailyLimit ?? 0),
    scenes: (config?.scenes ?? []).map((scene, index) => sceneToDraft(scene, index))
  };
}

function blankScene(index: number, placementId: string): SceneDraft {
  return {
    uid: nextUid(),
    key: index === 0 ? "vip" : `scene${index + 1}`,
    name: index === 0 ? "看广告领会员" : "",
    placementId,
    enabled: true,
    dailyLimit: "3",
    cooldownSeconds: "60",
    rewards: { vip_days: { amount: "1", refId: 0, feature: "" } },
    features: [],
    adFree: true,
    lifetime: lifetimeToDraft()
  };
}

export function RewardedAdConfigPanel({ appKey }: { appKey: string }) {
  const configQuery = useRewardedAdConfigQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSaveRewardedAdConfigMutation(appKey);

  const [draft, setDraft] = useState<Draft | null>(null);
  const [secret, setSecret] = useState<{ scope: string; value: string }>({ scope: appKey, value: "" });
  const [showSecret, setShowSecret] = useState(false);

  const config = configQuery.data;
  const current = draft?.scope === appKey ? draft : seedDraft(appKey, config);
  const secretValue = secret.scope === appKey ? secret.value : "";
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes, scope: appKey });
  const patchScene = (uid: string, changes: Partial<SceneDraft>) =>
    patch({ scenes: current.scenes.map((scene) => (scene.uid === uid ? { ...scene, ...changes } : scene)) });

  const catalog = config?.catalog ?? [];
  // 永久套餐不能作为激励广告奖励（后端保存时拒绝），不列出来免得选了再报错
  const plans = (plansQuery.data ?? []).filter((plan) => !plan.lifetime);
  const vipFeatures = featuresQuery.data ?? [];
  const callbackUrl = config
    ? `${
        config.callbackAbsolute || typeof window === "undefined"
          ? config.callbackUrl
          : `${window.location.origin}${config.callbackUrl}`
      }${CALLBACK_QUERY}`
    : "";

  const toggleReward = (scene: SceneDraft, spec: CardKeyRewardSpec, checked: boolean) => {
    const rewards = { ...scene.rewards };
    if (checked) {
      const amount =
        spec.value === "feature"
          ? Math.min(Math.max(DEFAULT_FEATURE_MINUTES, spec.min ?? 1), spec.max ?? DEFAULT_FEATURE_MINUTES)
          : spec.min ?? 1;
      rewards[spec.type] = {
        amount: String(amount),
        refId: plans[0]?.id ?? 0,
        feature: vipFeatures.find((feature) => feature.isActive)?.tag ?? ""
      };
    } else {
      delete rewards[spec.type];
    }
    patchScene(scene.uid, { rewards });
  };

  const buildRewards = (scene: SceneDraft): CardKeyReward[] =>
    catalog
      .filter((spec) => scene.rewards[spec.type])
      .map((spec) => {
        const value = scene.rewards[spec.type];
        switch (spec.value) {
          case "ref":
            return { type: spec.type, refId: Number(value.refId) };
          case "feature":
            return { type: spec.type, amount: Number(value.amount), feature: value.feature };
          default:
            return { type: spec.type, amount: Number(value.amount) };
        }
      });

  const save = async () => {
    for (const scene of current.scenes) {
      if (!scene.key.trim() || !scene.name.trim() || !scene.placementId.trim()) {
        toast.error("场景的标识、名称与广告位 ID 都不能为空");
        return;
      }
      if (buildRewards(scene).length === 0) {
        toast.error(`场景「${scene.name || scene.key}」至少配置一项权益`);
        return;
      }
      const featureless = catalog.find(
        (spec) => spec.value === "feature" && scene.rewards[spec.type] && !scene.rewards[spec.type].feature
      );
      if (featureless) {
        toast.error(`场景「${scene.name || scene.key}」的${featureless.label}未选择功能`);
        return;
      }
      if (
        OWNABLE.some((type) => scene.rewards[type]) &&
        scene.lifetime.policy === "replace" &&
        Object.keys(scene.lifetime.rewards).length === 0
      ) {
        toast.error(`场景「${scene.name || scene.key}」改发固定权益时至少选择一项`);
        return;
      }
    }
    try {
      await saveMutation.mutateAsync({
        enabled: current.enabled,
        provider: current.provider,
        providerAppId: current.providerAppId.trim(),
        securityKey: secretValue.trim() || undefined,
        verifyMode: current.verifyMode,
        dailyLimit: Number(current.dailyLimit) || 0,
        scenes: current.scenes.map((scene) => ({
          key: scene.key.trim(),
          name: scene.name.trim(),
          placementId: scene.placementId.trim(),
          enabled: scene.enabled,
          rewards: buildRewards(scene),
          dailyLimit: Number(scene.dailyLimit) || 0,
          cooldownSeconds: Number(scene.cooldownSeconds) || 0,
          // 功能只对会员天数生效：没勾会员天数时一并清掉，否则服务端会拒收
          membership: {
            features: scene.rewards[VIP_DAYS] ? scene.features : [],
            adFree: scene.adFree
          },
          lifetime: lifetimeToPayload(scene.lifetime)
        }))
      });
      toast.success("激励广告配置已保存");
      setDraft(null);
      setSecret({ scope: appKey, value: "" });
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  if (configQuery.isLoading) {
    return (
      <div className="space-y-4">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-40 w-full rounded-2xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Plug className="size-4" />}
        title="接入"
        aside={<StatusDot active={Boolean(config?.enabled)} labelActive="已启用" labelInactive="未启用" />}
      >
        <div className="space-y-5">
          <SwitchRow
            label="启用激励广告"
            checked={current.enabled}
            onChange={(value) => patch({ enabled: value })}
          />

          <div className="grid gap-4 sm:grid-cols-2">
            <FieldGroup label="广告平台">
              <Select value={current.provider} onValueChange={(value) => patch({ provider: value })}>
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(config?.providers ?? ["apex"]).map((provider) => (
                    <SelectItem key={provider} value={provider}>
                      {PROVIDER_LABELS[provider] ?? provider}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </FieldGroup>
            <FieldGroup label="平台应用 ID">
              <Input
                className="h-8 font-mono text-xs"
                value={current.providerAppId}
                placeholder="84371"
                onChange={(event) => patch({ providerAppId: event.target.value })}
              />
            </FieldGroup>
          </div>

          <FieldGroup
            label="Security Key"
            hint={config?.hasSecurityKey ? `已配置 ${config.securityKeyHint ?? ""}`.trim() : "未配置"}
          >
            <div className="relative">
              <Input
                type={showSecret ? "text" : "password"}
                autoComplete="off"
                className="pr-9 font-mono text-xs"
                placeholder={config?.hasSecurityKey ? "留空不修改" : "与平台「配置回调URL」中的 Security Key 一致"}
                value={secretValue}
                onChange={(event) => setSecret({ scope: appKey, value: event.target.value })}
              />
              {secretValue ? (
                <button
                  type="button"
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  onClick={() => setShowSecret(!showSecret)}
                  aria-label={showSecret ? "隐藏密钥" : "显示密钥"}
                >
                  {showSecret ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              ) : null}
            </div>
          </FieldGroup>

          <FieldGroup label="回调地址">
            <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2">
              <code className="min-w-0 flex-1 truncate font-mono text-xs" title={callbackUrl}>
                {callbackUrl}
              </code>
              <CopyButton value={callbackUrl} label="复制回调地址" />
            </div>
            <div className="flex flex-wrap items-center gap-1">
              <span className="mr-1 text-[11px] text-muted-foreground">参数</span>
              {CALLBACK_PARAMS.map(([name]) => (
                <Badge key={name} variant="outline" size="sm" className="font-mono font-normal">
                  {name}
                </Badge>
              ))}
            </div>
          </FieldGroup>
        </div>
      </SectionCard>

      <SectionCard icon={<ShieldCheck className="size-4" />} title="校验与限额">
        <div className="space-y-5">
          <FieldGroup label="校验模式">
            <RadioGroup
              value={current.verifyMode}
              onValueChange={(value) => patch({ verifyMode: value as RewardedAdVerifyMode })}
              className="grid gap-2 sm:grid-cols-3"
            >
              {(config?.verifyModes ?? (["dual", "server", "client"] as RewardedAdVerifyMode[])).map((mode) => (
                <ModeCard
                  key={mode}
                  value={mode}
                  active={current.verifyMode === mode}
                  title={VERIFY_MODE_META[mode]?.title ?? mode}
                  description={VERIFY_MODE_META[mode]?.description ?? ""}
                  icon={<ShieldCheck className="size-3.5" />}
                />
              ))}
            </RadioGroup>
          </FieldGroup>
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField
              label="每人每日总次数"
              unit="次"
              min={0}
              max={1000}
              hint="0 表示不限"
              value={current.dailyLimit}
              onChange={(value) => patch({ dailyLimit: value })}
            />
          </div>
        </div>
      </SectionCard>

      <SectionCard
        icon={<Gift className="size-4" />}
        title="奖励场景"
        aside={
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              patch({
                scenes: [
                  ...current.scenes,
                  blankScene(current.scenes.length, current.scenes[0]?.placementId ?? "")
                ]
              })
            }
          >
            <Plus className="size-3.5" />
            添加场景
          </Button>
        }
        footer={
          <div className="flex items-center justify-between gap-3">
            <span className="text-[11px] text-muted-foreground">
              {config?.updatedAt ? `上次保存 ${new Date(config.updatedAt).toLocaleString("zh-CN")}` : "尚未保存"}
              {config?.updatedBy ? ` · ${config.updatedBy}` : ""}
            </span>
            <Button size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              保存
            </Button>
          </div>
        }
      >
        {current.scenes.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            暂无场景
          </p>
        ) : (
          <div className="space-y-3">
            {current.scenes.map((scene) => (
              <div key={scene.uid} className="space-y-4 rounded-xl border border-border p-4">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-sm font-medium">{scene.name || "未命名场景"}</span>
                    <code className="font-mono text-[11px] text-muted-foreground">{scene.key}</code>
                  </div>
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={scene.enabled}
                      onCheckedChange={(value) => patchScene(scene.uid, { enabled: value })}
                      aria-label="启用场景"
                    />
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-7 text-muted-foreground hover:text-destructive"
                      aria-label="删除场景"
                      onClick={() => patch({ scenes: current.scenes.filter((item) => item.uid !== scene.uid) })}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </div>

                <div className="grid gap-3 sm:grid-cols-3">
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">名称</Label>
                    <Input
                      className="h-8 text-xs"
                      value={scene.name}
                      maxLength={32}
                      onChange={(event) => patchScene(scene.uid, { name: event.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">标识</Label>
                    <Input
                      className="h-8 font-mono text-xs"
                      value={scene.key}
                      maxLength={32}
                      onChange={(event) => patchScene(scene.uid, { key: event.target.value.toLowerCase() })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">广告位 ID</Label>
                    <Input
                      className="h-8 font-mono text-xs"
                      value={scene.placementId}
                      onChange={(event) => patchScene(scene.uid, { placementId: event.target.value })}
                    />
                  </div>
                </div>

                <div className="grid gap-3 sm:grid-cols-3">
                  <NumberField
                    label="每日次数"
                    unit="次"
                    min={0}
                    max={1000}
                    hint="0 表示只受总次数约束"
                    value={scene.dailyLimit}
                    onChange={(value) => patchScene(scene.uid, { dailyLimit: value })}
                  />
                  <NumberField
                    label="冷却"
                    unit="秒"
                    min={0}
                    max={86400}
                    hint="0 表示不限"
                    value={scene.cooldownSeconds}
                    onChange={(value) => patchScene(scene.uid, { cooldownSeconds: value })}
                  />
                </div>

                <FieldGroup label="奖励">
                  <div className="grid gap-2 sm:grid-cols-2">
                    {catalog.map((spec) => {
                      const value = scene.rewards[spec.type];
                      return (
                        <div key={spec.type} className="rounded-lg border border-border p-3">
                          <label className="flex items-center gap-2.5">
                            <Checkbox
                              checked={Boolean(value)}
                              onCheckedChange={(checked) => toggleReward(scene, spec, Boolean(checked))}
                            />
                            <span className="text-xs font-medium">{spec.label}</span>
                          </label>
                          {value ? (
                            <div className="mt-2.5 pl-7">
                              {spec.value === "ref" ? (
                                plans.length === 0 ? (
                                  <p className="text-xs text-muted-foreground">暂无会员套餐</p>
                                ) : (
                                  <Select
                                    value={String(value.refId || plans[0].id)}
                                    onValueChange={(next) =>
                                      patchScene(scene.uid, {
                                        rewards: { ...scene.rewards, [spec.type]: { ...value, refId: Number(next) } }
                                      })
                                    }
                                  >
                                    <SelectTrigger className="h-8 w-full text-xs">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      {plans.map((plan) => (
                                        <SelectItem key={plan.id} value={String(plan.id)}>
                                          {plan.name}（{vipPlanTerm(plan)}）
                                        </SelectItem>
                                      ))}
                                    </SelectContent>
                                  </Select>
                                )
                              ) : spec.value === "feature" ? (
                                <FeatureAmountField
                                  spec={spec}
                                  value={value}
                                  features={vipFeatures}
                                  onChange={(next) =>
                                    patchScene(scene.uid, {
                                      rewards: { ...scene.rewards, [spec.type]: { ...value, ...next } }
                                    })
                                  }
                                />
                              ) : (
                                <div className="flex items-center gap-2">
                                  <Input
                                    type="number"
                                    className="h-8 w-28 font-mono text-xs"
                                    value={value.amount}
                                    min={spec.min}
                                    max={spec.max}
                                    onChange={(event) =>
                                      patchScene(scene.uid, {
                                        rewards: {
                                          ...scene.rewards,
                                          [spec.type]: { ...value, amount: event.target.value }
                                        }
                                      })
                                    }
                                  />
                                  <span className="text-xs text-muted-foreground">{spec.unit}</span>
                                </div>
                              )}
                            </div>
                          ) : null}
                        </div>
                      );
                    })}
                  </div>
                </FieldGroup>

                {scene.rewards[VIP_DAYS] || scene.rewards[VIP_PLAN] ? (
                  <FieldGroup
                    label="会员权益"
                    hint={scene.rewards[VIP_DAYS] ? `已选 ${scene.features.length} / ${vipFeatures.length}` : undefined}
                  >
                    <div className="space-y-2">
                      <SwitchRow
                        label="会员期间免广告"
                        checked={scene.adFree}
                        onChange={(value) => patchScene(scene.uid, { adFree: value })}
                      />
                      {scene.rewards[VIP_DAYS] ? (
                        vipFeatures.length === 0 ? (
                          <p className="rounded-xl border border-dashed border-border px-3 py-4 text-center text-[11px] text-muted-foreground">
                            暂无功能标识
                          </p>
                        ) : (
                          <div className="grid gap-1 rounded-xl border border-border p-2 sm:grid-cols-2">
                            {vipFeatures.map((feature) => (
                              <label
                                key={feature.tag}
                                className={cn(
                                  "flex cursor-pointer items-center gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-muted/60",
                                  !feature.isActive && "opacity-60"
                                )}
                              >
                                <Checkbox
                                  checked={scene.features.includes(feature.tag)}
                                  onCheckedChange={(checked) =>
                                    patchScene(scene.uid, {
                                      features: checked
                                        ? [...scene.features, feature.tag]
                                        : scene.features.filter((tag) => tag !== feature.tag)
                                    })
                                  }
                                />
                                <span className="truncate text-xs font-medium">{feature.name}</span>
                                <code className="rounded bg-muted px-1 text-[10px] text-muted-foreground">
                                  {feature.tag}
                                </code>
                                {!feature.isActive ? (
                                  <span className="text-[10px] text-muted-foreground">已停用</span>
                                ) : null}
                              </label>
                            ))}
                          </div>
                        )
                      ) : (
                        <p className="text-[11px] text-muted-foreground">会员套餐的功能随所选套餐</p>
                      )}
                    </div>
                  </FieldGroup>
                ) : null}

                {OWNABLE.some((type) => scene.rewards[type]) ? (
                  <LifetimeField
                    value={scene.lifetime}
                    policies={config?.lifetimePolicies?.length ? config.lifetimePolicies : FALLBACK_LIFETIME_POLICIES}
                    targets={(config?.convertTargets?.length ? config.convertTargets : FALLBACK_CONVERT_TARGETS)
                      .map((type) => catalog.find((spec) => spec.type === type))
                      .filter((spec): spec is CardKeyRewardSpec => Boolean(spec))}
                    onChange={(next) => patchScene(scene.uid, { lifetime: { ...scene.lifetime, ...next } })}
                  />
                ) : null}
              </div>
            ))}
          </div>
        )}
      </SectionCard>
    </div>
  );
}

/**
 * 功能时长：选一个会员功能，再填分钟数。
 * 已保存的标识若已从功能目录删除，仍列出来并标「未登记」，避免下拉框显示为空。
 */
function FeatureAmountField({
  spec,
  value,
  features,
  onChange
}: {
  spec: CardKeyRewardSpec;
  value: { amount: string; feature: string };
  features: VipFeature[];
  onChange: (next: { amount?: string; feature?: string }) => void;
}) {
  const orphan = value.feature && !features.some((feature) => feature.tag === value.feature) ? value.feature : "";
  if (features.length === 0 && !orphan) {
    return <p className="text-xs text-muted-foreground">暂无功能标识</p>;
  }
  return (
    <div className="space-y-2">
      <Select value={value.feature} onValueChange={(next) => onChange({ feature: next })}>
        <SelectTrigger className="h-8 w-full text-xs">
          <SelectValue placeholder="选择功能" />
        </SelectTrigger>
        {/* 还没选功能时没有对齐项，弹层改为贴着触发器向下展开 */}
        <SelectContent position="popper">
          {orphan ? (
            <SelectItem value={orphan} disabled>
              {orphan}
              <span className="ml-1.5 text-[10px] text-muted-foreground">未登记</span>
            </SelectItem>
          ) : null}
          {features.map((feature) => (
            <SelectItem
              key={feature.tag}
              value={feature.tag}
              disabled={!feature.isActive && feature.tag !== value.feature}
            >
              {feature.name || feature.tag}
              <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
              {!feature.isActive ? <span className="ml-1.5 text-[10px] text-muted-foreground">已停用</span> : null}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <div className="flex items-center gap-2">
        <Input
          type="number"
          className="h-8 w-28 font-mono text-xs"
          value={value.amount}
          min={spec.min}
          max={spec.max}
          onChange={(event) => onChange({ amount: event.target.value })}
        />
        <span className="text-xs text-muted-foreground">{spec.unit}</span>
      </div>
    </div>
  );
}

/**
 * 永久会员（或已永久拥有某功能的人）看这个场景时，已拥有的会员、套餐、功能时长怎么办。
 * 只在场景配了这几档时出现；判定与折算都在服务端结算事务里。
 */
function LifetimeField({
  value,
  policies,
  targets,
  onChange
}: {
  value: LifetimeDraft;
  policies: RewardedAdLifetimePolicySpec[];
  targets: CardKeyRewardSpec[];
  onChange: (next: Partial<LifetimeDraft>) => void;
}) {
  const active = policies.find((policy) => policy.key === value.policy);
  const target = targets.find((spec) => spec.type === value.target);
  return (
    <FieldGroup label="已拥有的权益" hint="永久会员或已永久解锁功能的用户">
      <div className="space-y-3 rounded-xl border border-border p-3">
        <div className="space-y-1.5">
          <Select value={value.policy} onValueChange={(next) => onChange({ policy: next as RewardedAdLifetimePolicy })}>
            <SelectTrigger className="h-8 w-full text-xs sm:w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {policies.map((policy) => (
                <SelectItem key={policy.key} value={policy.key}>
                  {policy.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {active ? <p className="text-[11px] text-muted-foreground">{active.hint}</p> : null}
        </div>

        {value.policy === "convert" || value.policy === "hide" ? (
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">折算为</Label>
              <Select value={value.target} onValueChange={(next) => onChange({ target: next })}>
                <SelectTrigger className="h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {targets.map((spec) => (
                    <SelectItem key={spec.type} value={spec.type}>
                      {spec.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <NumberField
              label="会员每天"
              unit={target?.unit ?? ""}
              min={0}
              max={100000}
              hint="会员天数与套餐天数"
              value={value.perVipDay}
              onChange={(next) => onChange({ perVipDay: next })}
            />
            <NumberField
              label="功能时长每小时"
              unit={target?.unit ?? ""}
              min={0}
              max={100000}
              hint="不足一小时按比例向上取整"
              value={value.perFeatureHour}
              onChange={(next) => onChange({ perFeatureHour: next })}
            />
          </div>
        ) : null}

        {value.policy === "replace" ? (
          <div className="grid gap-2 sm:grid-cols-2">
            {targets.map((spec) => {
              const amount = value.rewards[spec.type];
              return (
                <div key={spec.type} className="flex items-center gap-2.5 rounded-lg border border-border px-3 py-2">
                  <Checkbox
                    checked={amount !== undefined}
                    onCheckedChange={(checked) => {
                      const rewards = { ...value.rewards };
                      if (checked) {
                        rewards[spec.type] = String(spec.min ?? 1);
                      } else {
                        delete rewards[spec.type];
                      }
                      onChange({ rewards });
                    }}
                  />
                  <span className="min-w-0 flex-1 truncate text-xs font-medium">{spec.label}</span>
                  {amount !== undefined ? (
                    <>
                      <Input
                        type="number"
                        className="h-7 w-24 font-mono text-xs"
                        value={amount}
                        min={spec.min}
                        max={spec.max}
                        onChange={(event) =>
                          onChange({ rewards: { ...value.rewards, [spec.type]: event.target.value } })
                        }
                      />
                      <span className="text-xs text-muted-foreground">{spec.unit}</span>
                    </>
                  ) : null}
                </div>
              );
            })}
          </div>
        ) : null}
      </div>
    </FieldGroup>
  );
}
