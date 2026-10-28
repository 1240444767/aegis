"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  BadgeCheck,
  CalendarClock,
  Crown,
  Gift,
  History,
  Infinity as InfinityIcon,
  KeyRound,
  Loader2,
  Minus,
  Plus,
  Receipt,
  RotateCcw,
  Shuffle,
  Undo2
} from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
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
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import {
  VipRevokeDialog,
  formatVipTerm,
  isLifetimeGrant,
  vipPlanTerm,
  vipSourceLabel,
  type VipRevokeAction
} from "@/components/apps/vip/vip-shared";
import { useNow } from "@/components/users/ban-form";
import { ApiError } from "@/lib/api-client";
import {
  useAdminUserVipEntitlementQuery,
  useAdminUserVipTransactionsQuery,
  useAdminVipFeaturesQuery,
  useAdminVipPlansQuery,
  useClaimAdminVipTrialMutation,
  useGrantAdminUserVipMutation,
  useResetAdminVipTrialMutation,
  useRevokeAdminUserVipMutation
} from "@/lib/app-user-hooks";
import type { VipEntitlement, VipPlan, VipTransaction } from "@/lib/api/types";
import type { VipConversion, VipGrantPayload, VipGrantPreview, VipOwnedMode, VipUpgradeMode } from "@/lib/api/vip";
import { useAdminVipConversionsQuery, useAdminVipGrantPreviewQuery, useAdminVipTiersQuery } from "@/lib/vip-hooks";
import { cn } from "@/lib/utils";
import {
  InfoGrid,
  Pager,
  RecordSheet,
  RecordTable,
  Section,
  StateBlock,
  TONE_TEXT,
  type Column,
  type InfoItem
} from "./kit";
import { QuickChips, ReasonInput, Segmented, type SegmentOption } from "./user-assets-adjust";
import { EMPTY, formatMoney, formatShortTime, formatTime, textValue } from "./user-detail-shared";

const DAY = 86_400_000;
const HOUR = 3_600_000;

const GRANT_REASONS = ["客诉补偿", "活动奖励", "合作赠送", "内部测试", "补发订单"];
const REVOKE_REASONS = ["误操作发放", "违规使用", "退款收回", "活动结束"];
const RECORD_PAGE_SIZE = 10;

type CustomTerm = "7" | "30" | "90" | "365" | "lifetime" | "custom";

const CUSTOM_TERMS: Array<SegmentOption<CustomTerm>> = [
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天" },
  { value: "90", label: "90 天" },
  { value: "365", label: "1 年" },
  { value: "lifetime", label: "永久" },
  { value: "custom", label: "自定义" }
];

type Operation = "grant" | "revoke" | "trial";

/** 限时那条线现在的到期时间：永久会员看 timedExpireAt，其余看 expireAt（可能已过期）。 */
function timedExpire(entitlement?: VipEntitlement) {
  if (!entitlement) return null;
  return (entitlement.isLifetime ? entitlement.timedExpireAt : entitlement.expireAt) ?? null;
}

/**
 * 发放 N 天后的到期时间，与后端 GrantVip 同一算法：
 * 还在期内的从原到期时间续，已过期或从未开通的从现在起算。
 */
function expireAfterGrant(entitlement: VipEntitlement | undefined, days: number, now: number) {
  const current = timedExpire(entitlement);
  const currentTime = current ? new Date(current).getTime() : Number.NaN;
  const base = Number.isFinite(currentTime) && currentTime > now ? currentTime : now;
  // 自定义天数清空时 days 是 NaN，天数过大会超出 Date 范围；两者都不能 toISOString
  const expire = new Date(base + days * DAY);
  return Number.isNaN(expire.getTime()) ? null : expire.toISOString();
}

function entitlementLabel(entitlement: VipEntitlement) {
  if (entitlement.isLifetime) return "永久会员";
  if (entitlement.isVip) return entitlement.isTrial ? "试用中" : "会员有效";
  return entitlement.expireAt ? "已过期" : "非会员";
}

function entitlementVariant(entitlement: VipEntitlement) {
  if (entitlement.isLifetime || (entitlement.isVip && !entitlement.isTrial)) return "success" as const;
  if (entitlement.isVip) return "info" as const;
  return entitlement.expireAt ? ("warning" as const) : ("outline" as const);
}

/** 「剩余 12 天 5 小时」：不足一天时精确到分钟。 */
function countdown(target: number, now: number) {
  const left = target - now;
  if (left <= 0) return "已到期";
  if (left < HOUR) return `剩余 ${Math.max(1, Math.floor(left / 60_000))} 分钟`;
  if (left < DAY) return `剩余 ${Math.floor(left / HOUR)} 小时 ${Math.floor((left % HOUR) / 60_000)} 分钟`;
  const days = Math.floor(left / DAY);
  const hours = Math.floor((left % DAY) / HOUR);
  return hours ? `剩余 ${days} 天 ${hours} 小时` : `剩余 ${days} 天`;
}

function FieldLabel({ children, aside }: { children: ReactNode; aside?: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-2">
      <Label className="text-xs text-muted-foreground">{children}</Label>
      {aside ? <span className="text-xs text-muted-foreground">{aside}</span> : null}
    </div>
  );
}

/** 提交前的后果预览 + 提交按钮，放在每种操作的最后一行。 */
function SubmitBar({ summary, children }: { summary: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 text-xs leading-5 text-muted-foreground">{summary}</div>
      <div className="flex shrink-0 flex-wrap items-center gap-2">{children}</div>
    </div>
  );
}

/**
 * 会员：左边是现状（与用户端 /vip/status 同一判定入口，管理员看到的结论必须与用户手机上一字不差），
 * 右边是能对它做的事（发放 / 延长、收回、试用），下方是这个人的会员记录。
 *
 * 永久与限时是并列的两条线（`users.vip_lifetime_at` 与 `vip_expire_at`）：
 * 永久会员没有到期时间，另买的限时会员只贡献功能，到期时间单独列为「限时到期」。
 * 收回与试用只在这个人确实有可收回的线、应用确实配置了试用时出现，没有可执行的操作就不占位。
 */
export function UserVipPanel({
  appKey,
  userId,
  onViewLedger
}: {
  appKey: string;
  userId: number;
  /** 给出时区块右上角出现「查看流水」 */
  onViewLedger?: () => void;
}) {
  const entitlementQuery = useAdminUserVipEntitlementQuery(appKey, userId);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const entitlement = entitlementQuery.data;

  const featureName = useMemo(() => {
    const map = new Map<string, string>();
    for (const feature of featuresQuery.data ?? []) map.set(feature.tag, feature.name);
    return (tag: string) => map.get(tag) || tag;
  }, [featuresQuery.data]);
  const featureTags = useMemo(
    () => (featuresQuery.data ?? []).filter((feature) => feature.isActive).map((feature) => feature.tag),
    [featuresQuery.data]
  );

  const lifetime = Boolean(entitlement?.isLifetime);
  const hasTimed = lifetime ? Boolean(entitlement?.timedExpireAt) : Boolean(entitlement?.isVip);
  const canRevoke = Boolean(entitlement) && (lifetime || hasTimed);
  const offer = entitlement?.trialOffer;
  const trialConfigured = Boolean(offer) && offer?.reason !== "not_configured";

  const operations: Array<SegmentOption<Operation>> = [
    { value: "grant", label: entitlement?.isVip ? "延长 / 发放" : "发放", icon: Gift },
    ...(canRevoke ? [{ value: "revoke" as const, label: "收回", icon: Undo2 }] : []),
    ...(trialConfigured ? [{ value: "trial" as const, label: "试用", icon: RotateCcw }] : [])
  ];
  const [operation, setOperation] = useState<Operation>("grant");
  // 收回 / 试用入口随数据消失（刚收完、试用被关）时退回发放
  const activeOperation = operations.some((item) => item.value === operation) ? operation : "grant";

  return (
    <div className="flex flex-col gap-5">
      <Section
        title="会员"
        description="与用户端会员状态同一判定"
        icon={Crown}
        tone="violet"
        flush
        actions={
          <>
            {entitlement ? (
              <Badge size="sm" variant={entitlementVariant(entitlement)}>
                {entitlementLabel(entitlement)}
              </Badge>
            ) : null}
            {onViewLedger ? (
              <Button size="sm" variant="outline" onClick={onViewLedger}>
                <Receipt />
                查看流水
              </Button>
            ) : null}
          </>
        }
      >
        {entitlementQuery.isError && !entitlement ? (
          <StateBlock
            kind="error"
            description={entitlementQuery.error instanceof Error ? entitlementQuery.error.message : undefined}
            onRetry={() => void entitlementQuery.refetch()}
          />
        ) : (
          <div className="grid lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
            <div className="border-b px-4 py-4 sm:px-5 lg:border-r lg:border-b-0">
              <VipStatus loading={entitlementQuery.isLoading} entitlement={entitlement} featureName={featureName} />
            </div>

            <div className="flex min-w-0 flex-col gap-4 px-4 py-4 sm:px-5">
              {operations.length > 1 ? (
                <Segmented value={activeOperation} onChange={setOperation} options={operations} ariaLabel="会员操作" />
              ) : null}

              {activeOperation === "grant" ? (
                <GrantForm
                  appKey={appKey}
                  userId={userId}
                  entitlement={entitlement}
                  plans={plansQuery.data ?? []}
                  plansLoading={plansQuery.isLoading}
                  featureTags={featureTags}
                  featureName={featureName}
                />
              ) : activeOperation === "revoke" && entitlement ? (
                <RevokeForm appKey={appKey} userId={userId} entitlement={entitlement} />
              ) : activeOperation === "trial" && entitlement ? (
                <TrialForm appKey={appKey} userId={userId} entitlement={entitlement} />
              ) : null}
            </div>
          </div>
        )}
      </Section>

      <VipRecords appKey={appKey} userId={userId} featureName={featureName} onViewLedger={onViewLedger} />

      <VipConversionRecords appKey={appKey} userId={userId} />
    </div>
  );
}

// ── 现状 ──────────────────────────

function VipStatus({
  loading,
  entitlement,
  featureName
}: {
  loading: boolean;
  entitlement?: VipEntitlement;
  featureName: (tag: string) => string;
}) {
  const now = useNow();
  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-2 w-full" />
        <div className="grid gap-4 sm:grid-cols-2">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="flex flex-col gap-1.5">
              <Skeleton className="h-3 w-12" />
              <Skeleton className="h-4 w-24" />
            </div>
          ))}
        </div>
      </div>
    );
  }
  if (!entitlement) {
    return <StateBlock compact title="权益信息不可用" />;
  }

  const expire = entitlement.expireAt ? new Date(entitlement.expireAt).getTime() : Number.NaN;
  const headline = entitlement.isLifetime
    ? "永久有效"
    : entitlement.isVip && Number.isFinite(expire)
      ? countdown(expire, now)
      : entitlement.expireAt
        ? "已过期"
        : "未开通";

  // 只有试用知道整段时长，才画剩余进度；付费会员可能多次续期，没有可信的「整段」
  const trialTotal = entitlement.trial?.active && entitlement.trial.durationDays ? entitlement.trial.durationDays * DAY : 0;
  const showProgress = entitlement.isTrial && entitlement.isVip && trialTotal > 0 && Number.isFinite(expire);
  const progress = showProgress ? Math.min(100, Math.max(2, ((expire - now) / trialTotal) * 100)) : 0;

  const unlockTags = new Set((entitlement.unlocks ?? []).map((item) => item.tag));

  const fields: InfoItem[] = [
    { label: "当前套餐", value: textValue(entitlement.planName) },
    { label: "来源", value: entitlement.source === "none" ? EMPTY : vipSourceLabel(entitlement.source) },
    entitlement.isLifetime
      ? { label: "开通时间", value: formatTime(entitlement.lifetimeSince) }
      : { label: entitlement.isVip ? "到期时间" : "上次到期", value: entitlement.expireAt ? formatTime(entitlement.expireAt) : EMPTY },
    ...(entitlement.isLifetime
      ? [{ label: "限时到期", value: entitlement.timedExpireAt ? formatTime(entitlement.timedExpireAt) : EMPTY, hint: "另买的限时会员只贡献功能" }]
      : []),
    ...(entitlement.trial
      ? [
          {
            label: "试用",
            value: entitlement.trial.active ? "试用中" : "已用过",
            tone: entitlement.trial.active ? ("info" as const) : undefined,
            hint: `领取于 ${formatShortTime(entitlement.trial.claimedAt)}`
          }
        ]
      : [])
  ];

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-2">
        <div
          className={cn(
            "flex items-center gap-2 text-2xl font-semibold tracking-tight tabular-nums",
            entitlement.isLifetime || entitlement.isVip ? "text-foreground" : "text-muted-foreground"
          )}
        >
          {entitlement.isLifetime ? <InfinityIcon className={cn("size-6", TONE_TEXT.violet)} /> : null}
          {headline}
        </div>
        {showProgress ? <Progress value={progress} className="h-1.5" /> : null}
      </div>

      <InfoGrid items={fields} />

      <div className="flex flex-col gap-2">
        <div className="text-xs text-muted-foreground">
          生效权益 <span className="tabular-nums">{entitlement.features.length}</span> 项
          {unlockTags.size ? `，其中单独解锁 ${unlockTags.size} 项` : ""}
        </div>
        {entitlement.features.length ? (
          <div className="flex flex-wrap gap-1.5">
            {entitlement.features.map((tag) => {
              const unlocked = unlockTags.has(tag);
              return (
                <Badge
                  key={tag}
                  variant={unlocked ? "info" : "secondary"}
                  size="sm"
                  className="gap-1"
                  title={unlocked ? "单独解锁" : "会员权益"}
                >
                  {unlocked ? <KeyRound className="size-3" /> : <BadgeCheck className="size-3" />}
                  {featureName(tag)}
                </Badge>
              );
            })}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">暂无生效权益</p>
        )}
      </div>
    </div>
  );
}

// ── 发放 ──────────────────────────

function GrantForm({
  appKey,
  userId,
  entitlement,
  plans,
  plansLoading,
  featureTags,
  featureName
}: {
  appKey: string;
  userId: number;
  entitlement?: VipEntitlement;
  plans: VipPlan[];
  plansLoading: boolean;
  featureTags: string[];
  featureName: (tag: string) => string;
}) {
  const grant = useGrantAdminUserVipMutation(appKey, userId);

  // 只列在售付费套餐：试用有独立的资格体系，下架套餐不应继续发放
  const grantablePlans = useMemo(() => plans.filter((plan) => plan.isActive && plan.kind !== "trial"), [plans]);

  const [modeChoice, setModeChoice] = useState<"plan" | "custom" | null>(null);
  const mode = modeChoice ?? (grantablePlans.length || plansLoading ? "plan" : "custom");

  const [planId, setPlanId] = useState<number | null>(null);
  const [quantity, setQuantity] = useState(1);
  const [term, setTerm] = useState<CustomTerm>("30");
  const [customDays, setCustomDays] = useState("");
  const [bonus, setBonus] = useState("");
  const [features, setFeatures] = useState<string[]>([]);
  // 自定义发放归入的档位；0 为默认（永久会员归入应用的永久档位，限时的不挂档位）
  const [tierChoice, setTierChoice] = useState(0);
  const tiersQuery = useAdminVipTiersQuery(appKey);
  const tiers = useMemo(() => tiersQuery.data ?? [], [tiersQuery.data]);
  const [reason, setReason] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  // 这一次单独指定的折算处理方式；空为跟随应用策略
  const [ownedPolicy, setOwnedPolicy] = useState<"" | VipOwnedMode>("");
  const [upgradePolicy, setUpgradePolicy] = useState<"" | VipUpgradeMode>("");

  const plan = grantablePlans.find((item) => item.id === planId);
  // 永久套餐只能发 1 份：永久乘以 N 还是永久，赠送积分却会乘上去（后端同样拒绝）
  const planLifetime = Boolean(plan?.lifetime);
  const planQuantity = planLifetime ? 1 : quantity;

  const customLifetime = term === "lifetime";
  const days = term === "custom" ? Number.parseInt(customDays, 10) : customLifetime ? 0 : Number(term);
  const customValid = customLifetime || (Number.isInteger(days) && days > 0 && days <= 36500);
  const autoTier = customLifetime ? tiers.find((tier) => tier.defaultLifetime) : undefined;
  const grantTier = tierChoice ? tiers.find((tier) => tier.id === tierChoice) : autoTier;
  const customFeatureCount = new Set([...(grantTier?.features ?? []), ...features]).size;
  const bonusValue = Number.parseInt(bonus, 10);
  const bonusCount = Number.isFinite(bonusValue) && bonusValue > 0 ? bonusValue : 0;

  const now = useNow();
  const grantLifetime = mode === "plan" ? planLifetime : customLifetime;
  const grantDays = mode === "plan" ? (plan ? plan.durationDays * planQuantity : 0) : days;
  const grantBonus = mode === "plan" ? (plan?.bonusIntegral ?? 0) * planQuantity : bonusCount;
  const grantFeatures = mode === "plan" ? (plan?.features?.length ?? 0) : customFeatureCount;
  const ready = mode === "plan" ? Boolean(plan) : customValid;

  const payload: Omit<VipGrantPayload, "userId"> | null = !ready
    ? null
    : mode === "plan" && plan
      ? { planId: plan.id, quantity: planQuantity, reason: reason.trim() || undefined }
      : {
          ...(customLifetime ? { lifetime: true } : { days }),
          tierId: tierChoice || undefined,
          bonusIntegral: bonusCount || undefined,
          features: features.length ? features : undefined,
          reason: reason.trim() || undefined
        };
  const previewPayload = useDebounced(
    payload ? { userId, ...payload, ownedPolicy: undefined, upgradePolicy: upgradePolicy || undefined } : null
  );
  const previewQuery = useAdminVipGrantPreviewQuery(appKey, previewPayload);
  const preview = payload ? previewQuery.data : undefined;
  const ownedConverted = Boolean(preview?.owned?.converted) && ownedPolicy !== "grant";

  async function submit() {
    if (!payload || grant.isPending) return;
    try {
      const result = await grant.mutateAsync({
        ...payload,
        ownedPolicy: ownedPolicy || undefined,
        upgradePolicy: upgradePolicy || undefined
      });
      if (result.conversion?.kind === "owned") {
        toast.success("已按折算策略折成积分", { description: result.conversion.message });
      } else {
        toast.success(result.lifetime ? "永久会员已发放" : "会员已发放", {
          description: result.conversion?.message ?? (result.expireAfter ? `到期时间 ${formatTime(result.expireAfter)}` : undefined)
        });
      }
      setConfirmOpen(false);
      setOwnedPolicy("");
      setUpgradePolicy("");
      setQuantity(1);
      setCustomDays("");
      setBonus("");
      setFeatures([]);
      setReason("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "发放失败");
    }
  }

  const extras = [
    grantBonus > 0 ? `附赠 ${grantBonus.toLocaleString("zh-CN")} 积分` : "",
    grantFeatures > 0 ? `含 ${grantFeatures} 项权益` : ""
  ].filter(Boolean);

  const lifetimeText = entitlement?.isLifetime ? "已是永久会员，本次只追加权益与积分" : "开通永久会员";
  const expireText = formatTime(expireAfterGrant(entitlement, grantDays, now));

  const summary = !ready ? (
    mode === "plan" ? "选择一个套餐" : "填写发放天数"
  ) : ownedConverted && preview?.owned ? (
    <span className="flex items-center gap-1.5">
      <Shuffle className="size-3.5 shrink-0" />
      已包含，将折算为 {preview.owned.item.points.toLocaleString("zh-CN")} 积分
    </span>
  ) : grantLifetime ? (
    <span className="flex items-center gap-1.5">
      <InfinityIcon className="size-3.5 shrink-0" />
      {lifetimeText}
      {extras.length ? `，${extras.join("，")}` : ""}
    </span>
  ) : (
    <span className="flex flex-wrap items-center gap-x-1.5">
      <CalendarClock className="size-3.5 shrink-0" />
      <span>
        {entitlement?.isLifetime ? "限时到期" : "到期"}
        <span className="mx-1 font-medium tabular-nums text-foreground">{expireText}</span>
        （+{grantDays} 天）
      </span>
      {extras.length ? <span>{extras.join("，")}</span> : null}
    </span>
  );

  return (
    <div className="flex flex-col gap-4">
      <Segmented
        value={mode}
        onChange={setModeChoice}
        stretch
        options={[
          { value: "plan", label: "按套餐", count: grantablePlans.length },
          { value: "custom", label: "自定义" }
        ]}
        ariaLabel="发放方式"
      />

      {mode === "plan" ? (
        plansLoading ? (
          <div className="grid gap-2 sm:grid-cols-2">
            {[0, 1].map((i) => (
              <Skeleton key={i} className="h-16 rounded-xl" />
            ))}
          </div>
        ) : grantablePlans.length === 0 ? (
          <StateBlock compact title="暂无在售套餐" description="可切换到「自定义」按天数发放" />
        ) : (
          <div className="flex flex-col gap-3">
            <div className="grid max-h-72 gap-2 overflow-y-auto sm:grid-cols-2" role="radiogroup" aria-label="套餐">
              {grantablePlans.map((item) => (
                <PlanOption key={item.id} plan={item} selected={item.id === planId} onSelect={() => setPlanId(item.id)} />
              ))}
            </div>
            {plan && !planLifetime ? (
              <div className="flex items-center justify-between gap-3">
                <FieldLabel>份数</FieldLabel>
                <QuantityStepper value={quantity} onChange={setQuantity} />
              </div>
            ) : null}
          </div>
        )
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <FieldLabel>时长</FieldLabel>
            <Segmented value={term} onChange={setTerm} options={CUSTOM_TERMS} ariaLabel="发放时长" />
            {term === "custom" ? (
              <div className="relative sm:max-w-48">
                <Input
                  autoFocus
                  value={customDays}
                  inputMode="numeric"
                  placeholder="天数，最多 36500"
                  aria-label="发放天数"
                  className="pr-8 tabular-nums"
                  onChange={(event) => setCustomDays(event.target.value.replace(/\D/g, ""))}
                />
                <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-muted-foreground">
                  天
                </span>
              </div>
            ) : null}
          </div>

          <div className="flex flex-col gap-2">
            <FieldLabel>附赠积分</FieldLabel>
            <div className="flex flex-wrap items-center gap-2">
              <Input
                value={bonus}
                inputMode="numeric"
                placeholder="0"
                aria-label="附赠积分"
                className="w-28 tabular-nums"
                onChange={(event) => setBonus(event.target.value.replace(/\D/g, ""))}
              />
              <QuickChips items={["100", "500", "1000"]} value={bonus} onPick={setBonus} />
            </div>
          </div>

          {tiers.length ? (
            <div className="flex flex-col gap-2">
              <FieldLabel>会员档位</FieldLabel>
              <div className="flex flex-wrap gap-1.5">
                {[
                  { id: 0, label: customLifetime ? `默认（${autoTier?.name ?? "不挂档位"}）` : "不挂档位" },
                  ...tiers.map((tier) => ({ id: tier.id, label: tier.name }))
                ].map((option) => {
                  const selected = tierChoice === option.id;
                  return (
                    <button
                      key={option.id}
                      type="button"
                      aria-pressed={selected}
                      className={cn(
                        "inline-flex h-7 items-center gap-1 rounded-md border px-2.5 text-xs transition-colors",
                        selected
                          ? "border-primary/40 bg-primary/10 text-foreground"
                          : "bg-background text-muted-foreground hover:bg-accent hover:text-foreground"
                      )}
                      onClick={() => setTierChoice(option.id)}
                    >
                      {selected ? <BadgeCheck className="size-3.5" /> : null}
                      {option.label}
                    </button>
                  );
                })}
              </div>
              {grantTier ? (
                <p className="text-[11px] text-muted-foreground">
                  权益随档位「{grantTier.name}」变化：{grantTier.features.length
                    ? grantTier.features.map(featureName).join("、")
                    : "仅会员身份"}
                </p>
              ) : null}
            </div>
          ) : null}

          {featureTags.length ? (
            <div className="flex flex-col gap-2">
              <FieldLabel
                aside={
                  <button
                    type="button"
                    className="transition-colors hover:text-foreground"
                    onClick={() => setFeatures(features.length === featureTags.length ? [] : featureTags)}
                  >
                    {features.length === featureTags.length ? "全不选" : "全选"}
                  </button>
                }
              >
                {grantTier ? "档位之外另给" : "附带权益"}
              </FieldLabel>
              <div className="flex flex-wrap gap-1.5">
                {featureTags.map((tag) => {
                  const selected = features.includes(tag);
                  return (
                    <button
                      key={tag}
                      type="button"
                      aria-pressed={selected}
                      className={cn(
                        "inline-flex h-7 items-center gap-1 rounded-md border px-2.5 text-xs transition-colors",
                        selected
                          ? "border-primary/40 bg-primary/10 text-foreground"
                          : "bg-background text-muted-foreground hover:bg-accent hover:text-foreground"
                      )}
                      onClick={() =>
                        setFeatures((prev) => (selected ? prev.filter((item) => item !== tag) : [...prev, tag]))
                      }
                    >
                      {selected ? <BadgeCheck className="size-3.5" /> : null}
                      {featureName(tag)}
                    </button>
                  );
                })}
              </div>
            </div>
          ) : null}
        </div>
      )}

      <div className="flex flex-col gap-2">
        <FieldLabel>发放原因</FieldLabel>
        <ReasonInput
          value={reason}
          onChange={setReason}
          suggestions={GRANT_REASONS}
          chips
          placeholder={mode === "custom" ? "选填，留空则记为「管理员授予」" : "选填"}
          onEnter={() => ready && setConfirmOpen(true)}
        />
      </div>

      <ConversionNotice
        preview={preview}
        ownedPolicy={ownedPolicy}
        onOwnedPolicy={setOwnedPolicy}
        upgradePolicy={upgradePolicy}
        onUpgradePolicy={setUpgradePolicy}
      />

      <SubmitBar summary={summary}>
        <Button size="sm" disabled={!ready || grant.isPending} onClick={() => setConfirmOpen(true)}>
          {grant.isPending ? <Loader2 className="animate-spin" /> : <Crown />}
          {entitlement?.isVip && !grantLifetime ? "延长会员" : "发放会员"}
        </Button>
      </SubmitBar>

      <AlertDialog open={confirmOpen} onOpenChange={(open) => !grant.isPending && setConfirmOpen(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {grantLifetime ? "确认发放永久会员？" : `确认发放 ${grantDays} 天会员？`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {ownedConverted && preview?.owned
                ? preview.owned.message
                : grantLifetime
                  ? lifetimeText
                  : `${entitlement?.isLifetime ? "限时到期" : "到期时间"}将变为 ${expireText}`}
              {extras.length ? `，${extras.join("，")}` : ""}。
              {!ownedConverted && preview?.upgrade
                ? (upgradePolicy || preview.policy.upgrade) === "convert"
                  ? ` ${preview.upgrade.message}。`
                  : " 原有的限时会员等存量将保留。"
                : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <dl className="grid gap-2 rounded-lg border bg-muted/30 p-3 text-sm">
            <div className="flex items-center justify-between gap-3">
              <dt className="text-xs text-muted-foreground">方式</dt>
              <dd className="min-w-0 truncate">
                {mode === "plan" && plan ? `${plan.name} × ${planQuantity}` : "自定义发放"}
              </dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="text-xs text-muted-foreground">原因</dt>
              <dd className="min-w-0 truncate">{reason.trim() || "未填写"}</dd>
            </div>
          </dl>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={grant.isPending}>取消</AlertDialogCancel>
            <Button disabled={grant.isPending} onClick={() => void submit()}>
              {grant.isPending ? <Loader2 className="animate-spin" /> : null}
              确认发放
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function PlanOption({ plan, selected, onSelect }: { plan: VipPlan; selected: boolean; onSelect: () => void }) {
  const meta = [
    vipPlanTerm(plan),
    plan.bonusIntegral ? `赠 ${plan.bonusIntegral.toLocaleString("zh-CN")} 积分` : "",
    plan.features?.length ? `${plan.features.length} 项权益` : ""
  ].filter(Boolean);
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex min-w-0 flex-col gap-1 rounded-xl border p-3 text-left transition-colors",
        selected ? "border-primary/50 bg-primary/5 ring-1 ring-primary/20 ring-inset" : "hover:bg-accent/40"
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
          {plan.lifetime ? <InfinityIcon className="size-3.5 shrink-0 text-muted-foreground" /> : null}
          <span className="truncate">{plan.name}</span>
        </span>
        <span className="shrink-0 text-xs tabular-nums text-muted-foreground">¥{formatMoney(plan.price)}</span>
      </div>
      <div className="truncate text-xs text-muted-foreground">{meta.join("，")}</div>
    </button>
  );
}

function QuantityStepper({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  const clamp = (next: number) => Math.min(100, Math.max(1, next));
  return (
    <div className="flex items-center gap-1">
      <Button size="icon-sm" variant="outline" aria-label="减少份数" disabled={value <= 1} onClick={() => onChange(clamp(value - 1))}>
        <Minus />
      </Button>
      <Input
        value={String(value)}
        inputMode="numeric"
        aria-label="份数"
        className="w-14 text-center tabular-nums"
        onChange={(event) => {
          const next = Number.parseInt(event.target.value, 10);
          onChange(Number.isFinite(next) ? clamp(next) : 1);
        }}
      />
      <Button size="icon-sm" variant="outline" aria-label="增加份数" disabled={value >= 100} onClick={() => onChange(clamp(value + 1))}>
        <Plus />
      </Button>
    </div>
  );
}

/** 输入停下来 delay 毫秒后才更新：发放预览跟着表单走，不必每敲一个数字请求一次。 */
function useDebounced<T>(value: T, delay = 300) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

/**
 * 发放的折算预览（后端同一套判据算出，不落账）：
 *
 * - 这一笔对他已经是空的（永久会员已包含）：按应用策略折成积分，管理员可以这一次选照常发放；
 * - 发的是永久会员、而他还有限时会员 / 功能解锁 / 功能时长 / 免广告：开通后这些会怎样，管理员可以这一次改成折算或保留。
 */
function ConversionNotice({
  preview,
  ownedPolicy,
  onOwnedPolicy,
  upgradePolicy,
  onUpgradePolicy
}: {
  preview?: VipGrantPreview;
  ownedPolicy: "" | VipOwnedMode;
  onOwnedPolicy: (value: "" | VipOwnedMode) => void;
  upgradePolicy: "" | VipUpgradeMode;
  onUpgradePolicy: (value: "" | VipUpgradeMode) => void;
}) {
  if (!preview) return null;
  const owned = preview.owned;
  const upgrade = preview.upgrade;
  const appConverts = preview.policy.owned === "convert";
  if (!owned && !upgrade) return null;
  const upgradeMode: VipUpgradeMode = upgradePolicy || preview.policy.upgrade;
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-amber-500/30 bg-amber-500/5 p-3">
      {owned ? (
        <div className="flex flex-col gap-2">
          <div className="flex items-start gap-2 text-xs leading-5">
            <Shuffle className={cn("mt-0.5 size-3.5 shrink-0", TONE_TEXT.warning)} />
            <span>{owned.message}</span>
          </div>
          {appConverts ? (
            <Segmented
              value={ownedPolicy === "grant" ? "grant" : "convert"}
              onChange={(value) => onOwnedPolicy(value === "grant" ? "grant" : "")}
              options={[
                { value: "convert", label: `折算 ${owned.item.points.toLocaleString("zh-CN")} 积分` },
                { value: "grant", label: "照常发放" }
              ]}
              ariaLabel="已拥有的处理方式"
            />
          ) : (
            <p className="text-xs text-muted-foreground">应用的折算策略为「照常发放」，可在会员 › 折算中修改</p>
          )}
        </div>
      ) : null}
      {upgrade ? (
        <div className="flex flex-col gap-2">
          <div className="flex items-start gap-2 text-xs leading-5">
            <Shuffle className={cn("mt-0.5 size-3.5 shrink-0", TONE_TEXT.warning)} />
            <span>{upgrade.message}</span>
          </div>
          <ul className="flex flex-col gap-1 text-xs">
            {upgrade.items.map((item, index) => (
              <li key={`${item.kind}-${item.ref ?? index}`} className="flex items-center justify-between gap-3">
                <span className="min-w-0 truncate text-muted-foreground">{item.label}</span>
                {upgradeMode === "convert" ? (
                  <span className="shrink-0 tabular-nums">{item.points.toLocaleString("zh-CN")} 积分</span>
                ) : null}
              </li>
            ))}
          </ul>
          <Segmented
            value={upgradeMode}
            onChange={(value) => onUpgradePolicy(value === preview.policy.upgrade ? "" : value)}
            options={[
              { value: "convert", label: "折算为积分" },
              { value: "keep", label: "保留存量" }
            ]}
            ariaLabel="开通永久时存量的处理方式"
          />
        </div>
      ) : null}
    </div>
  );
}

// ── 收回 ──────────────────────────

/**
 * 收回会员：扣减限时天数 / 取消永久会员。只收权益不退钱，付过钱的走订单退款。
 *
 * 两个动作各管一条线，按这个人实际有哪条线分别出现：扣减天数只动限时那条线
 * （永久会员没有另买限时的就无天可扣），取消永久会员只动永久那条线。
 */
function RevokeForm({ appKey, userId, entitlement }: { appKey: string; userId: number; entitlement: VipEntitlement }) {
  const revoke = useRevokeAdminUserVipMutation(appKey, userId);
  const lifetime = Boolean(entitlement.isLifetime);
  const expire = timedExpire(entitlement);
  const expireTime = expire ? new Date(expire).getTime() : Number.NaN;
  const now = useNow();
  const hasTimed = Number.isFinite(expireTime) && expireTime > now;
  const remainingDays = hasTimed ? Math.ceil((expireTime - now) / DAY) : 0;

  const [days, setDays] = useState("");
  const [reason, setReason] = useState("");
  const [dialog, setDialog] = useState<{ open: boolean; action: VipRevokeAction }>({
    open: false,
    action: { lifetime: true }
  });

  const dayCount = Number(days);
  const daysValid = Number.isInteger(dayCount) && dayCount > 0;
  const quick = ["1", "7", "30"].filter((item) => Number(item) < remainingDays);

  async function confirm() {
    const action = dialog.action;
    try {
      await revoke.mutateAsync(
        action.lifetime
          ? { lifetime: true, reason: reason.trim() || undefined }
          : { days: action.days, reason: reason.trim() || undefined }
      );
      toast.success(action.lifetime ? "已取消永久会员" : `已扣减 ${action.days} 天`);
      setDialog((prev) => ({ ...prev, open: false }));
      setDays("");
      setReason("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  const afterRevoke = daysValid && hasTimed ? expireTime - dayCount * DAY : Number.NaN;

  return (
    <div className="flex flex-col gap-4">
      {hasTimed ? (
        <div className="flex flex-col gap-2">
          <FieldLabel aside={`限时剩余约 ${remainingDays} 天`}>扣减天数</FieldLabel>
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative">
              <Input
                value={days}
                inputMode="numeric"
                placeholder="0"
                aria-label="扣减天数"
                className="w-28 pr-8 tabular-nums"
                onChange={(event) => setDays(event.target.value.replace(/\D/g, ""))}
              />
              <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-muted-foreground">
                天
              </span>
            </div>
            <QuickChips
              items={[...quick, String(remainingDays)]}
              value={days}
              onPick={setDays}
              render={(item) => (item === String(remainingDays) ? "全部" : `${item} 天`)}
            />
          </div>
          <div className="flex min-h-5 items-center gap-1.5 text-xs text-muted-foreground">
            <CalendarClock className="size-3.5 shrink-0" />
            {Number.isFinite(afterRevoke) ? (
              afterRevoke <= now ? (
                <span className={TONE_TEXT.warning}>{lifetime ? "限时那段将立即结束" : "会员将立即到期"}</span>
              ) : (
                <span>
                  到期 <span className="tabular-nums">{formatTime(expire)}</span>
                  <span className="mx-1">→</span>
                  <span className="font-medium tabular-nums text-foreground">
                    {formatTime(new Date(afterRevoke).toISOString())}
                  </span>
                </span>
              )
            ) : (
              <span>当前到期 {formatTime(expire)}</span>
            )}
          </div>
        </div>
      ) : null}

      <div className="flex flex-col gap-2">
        <FieldLabel>收回原因</FieldLabel>
        <ReasonInput value={reason} onChange={setReason} suggestions={REVOKE_REASONS} chips placeholder="选填" />
      </div>

      <SubmitBar summary="只收回权益，不退款；已付款的请走订单退款">
        {lifetime ? (
          <Button
            size="sm"
            variant="destructive"
            disabled={revoke.isPending}
            onClick={() => setDialog({ open: true, action: { lifetime: true } })}
          >
            <InfinityIcon />
            取消永久会员
          </Button>
        ) : null}
        {hasTimed ? (
          <Button
            size="sm"
            variant="outline"
            disabled={!daysValid || revoke.isPending}
            onClick={() => setDialog({ open: true, action: { lifetime: false, days: dayCount } })}
          >
            <Undo2 />
            {daysValid ? `扣减 ${dayCount} 天` : "扣减天数"}
          </Button>
        ) : null}
      </SubmitBar>

      <VipRevokeDialog
        open={dialog.open}
        action={dialog.action}
        pending={revoke.isPending}
        onOpenChange={(open) => setDialog((prev) => ({ ...prev, open }))}
        onConfirm={() => void confirm()}
      />
    </div>
  );
}

// ── 试用 ──────────────────────────

/** 试用管理：代领 / 恢复资格。资格判据与文案由服务端给出。 */
function TrialForm({ appKey, userId, entitlement }: { appKey: string; userId: number; entitlement: VipEntitlement }) {
  const claim = useClaimAdminVipTrialMutation(appKey, userId);
  const reset = useResetAdminVipTrialMutation(appKey, userId);
  const [resetOpen, setResetOpen] = useState(false);
  const offer = entitlement.trialOffer;
  const trial = entitlement.trial;

  async function run(action: "claim" | "reset") {
    try {
      if (action === "claim") {
        await claim.mutateAsync();
        toast.success("试用已开通");
      } else {
        await reset.mutateAsync();
        toast.success("试用资格已恢复");
        setResetOpen(false);
      }
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  const fields: InfoItem[] = [
    {
      label: "试用套餐",
      value: textValue(trial?.planName || offer.planName),
      hint: offer.durationDays ? `${offer.durationDays} 天` : undefined
    },
    {
      label: "状态",
      value: trial ? (trial.active ? "试用中" : "已结束") : offer.available ? "可领取" : "不可领取",
      tone: trial?.active ? "success" : undefined,
      hint: trial ? `领取于 ${formatTime(trial.claimedAt)}` : offer.available ? undefined : offer.message
    },
    ...(trial ? [{ label: "试用结束", value: formatTime(trial.endsAt) }] : []),
    ...(offer.bonusIntegral ? [{ label: "附赠积分", value: offer.bonusIntegral.toLocaleString("zh-CN") }] : [])
  ];

  return (
    <div className="flex flex-col gap-4">
      <InfoGrid items={fields} />
      {offer.description ? <p className="text-xs leading-5 text-muted-foreground">{offer.description}</p> : null}

      <SubmitBar summary={offer.deviceLimited ? "该试用套餐按设备限领" : "代领与用户自己领取走同一资格判定"}>
        {trial ? (
          <Button size="sm" variant="outline" disabled={reset.isPending} onClick={() => setResetOpen(true)}>
            <RotateCcw />
            恢复试用资格
          </Button>
        ) : null}
        {offer.available ? (
          <Button size="sm" disabled={claim.isPending} onClick={() => void run("claim")}>
            {claim.isPending ? <Loader2 className="animate-spin" /> : <Gift />}
            代领试用
          </Button>
        ) : null}
      </SubmitBar>

      <AlertDialog open={resetOpen} onOpenChange={(open) => !reset.isPending && setResetOpen(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>恢复试用资格？</AlertDialogTitle>
            <AlertDialogDescription>
              删除这个人的试用领取记录，之后可以再领一次试用。已发放的试用时长不会被收回。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={reset.isPending}>取消</AlertDialogCancel>
            <Button disabled={reset.isPending} onClick={() => void run("reset")}>
              {reset.isPending ? <Loader2 className="animate-spin" /> : null}
              确认恢复
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ── 会员记录 ──────────────────────────

function VipRecords({
  appKey,
  userId,
  featureName,
  onViewLedger
}: {
  appKey: string;
  userId: number;
  featureName: (tag: string) => string;
  onViewLedger?: () => void;
}) {
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<VipTransaction | null>(null);
  const query = useAdminUserVipTransactionsQuery(appKey, userId, { page, limit: RECORD_PAGE_SIZE });
  const total = query.data?.total ?? 0;

  const columns: Array<Column<VipTransaction>> = [
    {
      id: "time",
      header: "时间",
      width: "128px",
      cell: (txn) => (
        <span className="text-xs whitespace-nowrap tabular-nums text-muted-foreground" title={formatTime(txn.createdAt)}>
          {formatShortTime(txn.createdAt)}
        </span>
      )
    },
    {
      id: "plan",
      header: "套餐",
      cell: (txn) => (
        <div className="flex max-w-[240px] min-w-0 flex-col">
          <span className="truncate text-sm">{textValue(txn.planName)}</span>
          {txn.features?.length ? (
            <span className="truncate text-xs text-muted-foreground">{txn.features.map(featureName).join("、")}</span>
          ) : null}
        </div>
      )
    },
    {
      id: "term",
      header: "时长",
      align: "right",
      cell: (txn) => (
        <span
          className={cn(
            "text-sm font-medium tabular-nums",
            txn.conversion?.kind === "owned"
              ? TONE_TEXT.warning
              : txn.durationDays < 0 || txn.payChannel === "admin_revoke"
                ? TONE_TEXT.danger
                : "text-foreground"
          )}
        >
          {formatVipTerm(txn)}
        </span>
      )
    },
    {
      id: "channel",
      header: "渠道",
      cell: (txn) => (
        <div className="flex flex-wrap items-center gap-1">
          <Badge variant="outline" size="sm">
            {vipSourceLabel(txn.payChannel)}
          </Badge>
          {txn.revokedAt ? (
            <Badge variant="danger" size="sm" title={txn.revokeReason || undefined}>
              已作废
            </Badge>
          ) : null}
          {txn.conversion ? (
            <Badge variant="warning" size="sm" title={txn.conversion.message}>
              已折算
            </Badge>
          ) : null}
        </div>
      )
    },
    {
      id: "amount",
      header: "金额",
      align: "right",
      hideBelow: "md",
      cell: (txn) => <span className="text-sm tabular-nums">{formatMoney(txn.payAmount)}</span>
    },
    {
      id: "expire",
      header: "到期变化",
      hideBelow: "lg",
      cell: (txn) => (
        <span className="text-xs whitespace-nowrap tabular-nums text-muted-foreground">
          {/* 永久开通与取消永久会员都不动限时那条线的到期时间 */}
          {isLifetimeGrant(txn)
            ? "永久"
            : txn.lifetime
              ? EMPTY
              : `${txn.expireBefore ? formatShortTime(txn.expireBefore) : "开通"} → ${formatShortTime(txn.expireAfter)}`}
        </span>
      )
    }
  ];

  return (
    <Section
      title="会员记录"
      description="每一次开通、续期、收回与作废"
      icon={History}
      tone="violet"
      flush
      actions={
        onViewLedger ? (
          <Button size="sm" variant="ghost" onClick={onViewLedger}>
            <Receipt />
            完整流水
          </Button>
        ) : null
      }
      footer={total > RECORD_PAGE_SIZE ? <Pager page={page} limit={RECORD_PAGE_SIZE} total={total} onPage={setPage} /> : undefined}
    >
      <RecordTable
        columns={columns}
        rows={query.data?.items}
        rowKey={(txn) => txn.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        onRowClick={setSelected}
        rowClassName={(txn) => (txn.revokedAt ? "text-muted-foreground" : undefined)}
        empty={<StateBlock compact title="暂无会员记录" />}
        minWidth={640}
        skeletonRows={4}
      />

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        title={selected ? textValue(selected.planName) : ""}
        description={selected ? `流水号 ${selected.transactionNo}` : undefined}
        hero={
          selected ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-2xl font-semibold tabular-nums">{formatVipTerm(selected)}</span>
              <Badge variant="outline" size="sm">
                {vipSourceLabel(selected.payChannel)}
              </Badge>
              {selected.revokedAt ? (
                <Badge variant="danger" size="sm">
                  已作废
                </Badge>
              ) : null}
            </div>
          ) : null
        }
        fields={selected ? vipRecordFields(selected, featureName) : undefined}
        raw={selected}
      />
    </Section>
  );
}

/**
 * 折算记录：已拥有的发放折成的积分、开通永久时折掉的存量。
 * 取消永久会员后已还回的标「已还回」，并说明收回了多少积分、差多少没收回。
 */
function VipConversionRecords({ appKey, userId }: { appKey: string; userId: number }) {
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<VipConversion | null>(null);
  const query = useAdminVipConversionsQuery(appKey, { userId, page, limit: RECORD_PAGE_SIZE });
  const total = query.data?.total ?? 0;
  if (!query.isLoading && total === 0 && !query.error) return null;

  const columns: Array<Column<VipConversion>> = [
    {
      id: "time",
      header: "时间",
      width: "128px",
      cell: (item) => (
        <span className="text-xs whitespace-nowrap tabular-nums text-muted-foreground" title={formatTime(item.createdAt)}>
          {formatShortTime(item.createdAt)}
        </span>
      )
    },
    {
      id: "kind",
      header: "类型",
      cell: (item) => (
        <div className="flex flex-wrap items-center gap-1">
          <Badge variant={item.kind === "upgrade" ? "info" : "warning"} size="sm">
            {item.kind === "upgrade" ? "开通永久" : "已拥有"}
          </Badge>
          {item.status === "restored" ? (
            <Badge variant="secondary" size="sm">
              已还回
            </Badge>
          ) : null}
        </div>
      )
    },
    {
      id: "items",
      header: "折掉的权益",
      cell: (item) => (
        <span className="block max-w-[280px] truncate text-sm" title={item.items.map((entry) => entry.label).join("、")}>
          {item.items.map((entry) => entry.label).join("、") || EMPTY}
        </span>
      )
    },
    {
      id: "source",
      header: "来源",
      hideBelow: "md",
      cell: (item) => (
        <Badge variant="outline" size="sm">
          {vipSourceLabel(item.source)}
        </Badge>
      )
    },
    {
      id: "points",
      header: "积分",
      align: "right",
      cell: (item) => (
        <span className={cn("text-sm font-medium tabular-nums", item.status === "restored" && "line-through text-muted-foreground")}>
          +{item.points.toLocaleString("zh-CN")}
        </span>
      )
    }
  ];

  return (
    <Section
      title="折算记录"
      description="已拥有的发放与开通永久时被覆盖的存量，按折算策略折成的积分"
      icon={Shuffle}
      tone="warning"
      flush
      footer={total > RECORD_PAGE_SIZE ? <Pager page={page} limit={RECORD_PAGE_SIZE} total={total} onPage={setPage} /> : undefined}
    >
      <RecordTable
        columns={columns}
        rows={query.data?.items}
        rowKey={(item) => item.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        onRowClick={setSelected}
        empty={<StateBlock compact title="暂无折算记录" />}
        minWidth={600}
        skeletonRows={2}
      />
      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        title={selected ? (selected.kind === "upgrade" ? "开通永久会员折算" : "已拥有权益折算") : ""}
        description={selected ? `折算单号 ${selected.conversionNo}` : undefined}
        hero={
          selected ? (
            <div className="flex flex-col gap-1">
              <span className="text-2xl font-semibold tabular-nums">+{selected.points.toLocaleString("zh-CN")} 积分</span>
              <span className="text-xs text-muted-foreground">{selected.message}</span>
            </div>
          ) : null
        }
        fields={selected ? conversionFields(selected) : undefined}
        raw={selected}
      />
    </Section>
  );
}

function conversionFields(item: VipConversion): InfoItem[] {
  return [
    { label: "折算单号", value: item.conversionNo, mono: true, copy: item.conversionNo, wide: true },
    { label: "时间", value: formatTime(item.createdAt) },
    { label: "来源", value: vipSourceLabel(item.source) },
    { label: "操作人", value: textValue(item.operator) },
    { label: "积分流水", value: item.integralTransactionNo || EMPTY, mono: true, copy: item.integralTransactionNo || null },
    ...(item.triggerTransactionNo
      ? [{ label: "触发的开通", value: item.triggerTransactionNo, mono: true, copy: item.triggerTransactionNo }]
      : []),
    ...item.items.map((entry) => ({ label: entry.label, value: `${entry.points.toLocaleString("zh-CN")} 积分`, wide: true })),
    ...(item.status === "restored"
      ? [
          { label: "还回时间", value: item.restoredAt ? formatTime(item.restoredAt) : EMPTY, tone: "info" as const },
          { label: "收回积分", value: (item.restore?.pointsClawedBack ?? 0).toLocaleString("zh-CN") },
          ...(item.restore?.pointsShortfall
            ? [{ label: "未能收回", value: item.restore.pointsShortfall.toLocaleString("zh-CN"), tone: "danger" as const }]
            : [])
        ]
      : [])
  ];
}

function vipRecordFields(txn: VipTransaction, featureName: (tag: string) => string): InfoItem[] {
  return [
    { label: "流水号", value: txn.transactionNo, mono: true, copy: txn.transactionNo, wide: true },
    { label: "时间", value: formatTime(txn.createdAt) },
    { label: "操作人", value: textValue(txn.operator) },
    { label: "支付金额", value: `¥${formatMoney(txn.payAmount)}` },
    { label: "附赠积分", value: txn.bonusIntegral ? txn.bonusIntegral.toLocaleString("zh-CN") : EMPTY },
    { label: "原到期", value: txn.expireBefore ? formatTime(txn.expireBefore) : EMPTY },
    { label: "新到期", value: isLifetimeGrant(txn) ? "永久" : txn.expireAfter ? formatTime(txn.expireAfter) : EMPTY },
    {
      label: "关联订单",
      value: txn.relatedOrderNo || EMPTY,
      mono: true,
      copy: txn.relatedOrderNo || null,
      wide: true
    },
    { label: "权益", value: txn.features?.length ? txn.features.map(featureName).join("、") : EMPTY, wide: true },
    ...(txn.conversion
      ? [
          { label: "折算说明", value: txn.conversion.message, wide: true, tone: "warning" as const },
          { label: "折算积分", value: txn.conversion.points.toLocaleString("zh-CN") },
          { label: "折算单号", value: txn.conversion.conversionNo, mono: true, copy: txn.conversion.conversionNo }
        ]
      : []),
    ...(txn.revokedAt
      ? [
          { label: "作废时间", value: formatTime(txn.revokedAt), tone: "danger" as const },
          { label: "作废原因", value: textValue(txn.revokeReason) }
        ]
      : [])
  ];
}
