"use client";

import { useRef, useState } from "react";
import {
  Bell,
  Crown,
  FileText,
  Gift,
  KeyRound,
  Loader2,
  PenLine,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  ShieldCheck,
  Trash2,
  UserCheck,
  UserPlus,
  Users
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
  AUDIENCE_META,
  CHARSET_LABEL,
  REGISTER_FIELD_META,
  TRIGGER_META,
  normalizeCodeInput,
  sampleInviteCode
} from "@/components/apps/invite/invite-shared";
import {
  RewardListEditor,
  rewardDraftsFrom,
  rewardsFromDrafts,
  type RewardDraft
} from "@/components/apps/rewards/reward-list-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type {
  InviteAudience,
  InviteCatalog,
  InviteCharset,
  InviteConfig,
  InviteRegisterField,
  InviteSettings,
  InviteSettingsPayload,
  InviteTrigger
} from "@/lib/api/invite";
import type { PointsStoreRewardSpec } from "@/lib/api/points-store";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useInviteCatalogQuery, useInviteSettingsQuery, useSaveInviteSettingsMutation } from "@/lib/invite-hooks";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

/** 目录里没有下发的上限，与后端 invite/types.go 一致；最终以后端校验为准 */
const MAX_FIELD_LABEL = 16;
const MAX_PLACEHOLDER = 32;
const MAX_MILESTONE_TITLE = 16;
const MAX_LANDING_URL = 512;
const MAX_MIN_LEVEL = 1000;
const MAX_SIGNIN_DAYS = 365;
const MAX_INVITER_LIMIT = 1_000_000;
const MAX_DEVICE_LIMIT = 100;
const MAX_IP_DAILY_LIMIT = 10_000;
const MAX_MILESTONE_COUNT = 100_000;

const SHARE_TOKENS = [
  { token: "{code}", label: "邀请码" },
  { token: "{link}", label: "落地页" },
  { token: "{app}", label: "应用名称" }
];

const AUDIENCE_ICON: Record<InviteAudience, React.ReactNode> = {
  all: <Users className="size-4" />,
  vip: <Crown className="size-4" />
};

const TRIGGER_ICON: Record<InviteTrigger, React.ReactNode> = {
  bind: <UserCheck className="size-4" />,
  signin: <PenLine className="size-4" />
};

type MilestoneDraft = { key: number; count: string; title: string; rewards: RewardDraft[] };

type Form = {
  enabled: boolean;
  registerField: InviteRegisterField;
  fieldLabel: string;
  fieldPlaceholder: string;
  bindWindowHours: string;
  codeLength: string;
  codeCharset: InviteCharset;
  codePrefix: string;
  allowCustomCode: boolean;
  inviterAudience: InviteAudience;
  inviterMinLevel: string;
  inviteeRewards: RewardDraft[];
  inviterRewards: RewardDraft[];
  milestones: MilestoneDraft[];
  inviterTrigger: InviteTrigger;
  triggerSignInDays: string;
  inviterDailyLimit: string;
  inviterTotalLimit: string;
  notifyInviter: boolean;
  deviceLimit: string;
  ipDailyLimit: string;
  rulesText: string;
  shareTemplate: string;
  landingUrl: string;
};

/** 兜底默认值：目录还没到时也能渲染出一份完整的表单 */
const FALLBACK: InviteConfig = {
  registerField: "optional",
  fieldLabel: "邀请码",
  fieldPlaceholder: "选填",
  bindWindowHours: 72,
  codeLength: 8,
  codeCharset: "alnum",
  codePrefix: "",
  allowCustomCode: false,
  inviterAudience: "all",
  inviterMinLevel: 0,
  inviteeRewards: [],
  inviterRewards: [],
  milestones: [],
  inviterTrigger: "bind",
  triggerSignInDays: 1,
  inviterDailyLimit: 0,
  inviterTotalLimit: 0,
  notifyInviter: true,
  deviceLimit: 1,
  ipDailyLimit: 0,
  rulesText: "",
  shareTemplate: "",
  landingUrl: ""
};

function seed(settings?: InviteSettings, defaults?: InviteConfig): Form {
  const source: InviteConfig & { enabled?: boolean } = { ...FALLBACK, ...defaults, ...settings };
  return {
    enabled: Boolean(settings?.enabled),
    registerField: source.registerField,
    fieldLabel: source.fieldLabel ?? "",
    fieldPlaceholder: source.fieldPlaceholder ?? "",
    bindWindowHours: String(source.bindWindowHours ?? 0),
    codeLength: String(source.codeLength ?? 8),
    codeCharset: source.codeCharset,
    codePrefix: source.codePrefix ?? "",
    allowCustomCode: Boolean(source.allowCustomCode),
    inviterAudience: source.inviterAudience,
    inviterMinLevel: String(source.inviterMinLevel ?? 0),
    inviteeRewards: rewardDraftsFrom(source.inviteeRewards),
    inviterRewards: rewardDraftsFrom(source.inviterRewards),
    milestones: (source.milestones ?? []).map((milestone, index) => ({
      key: index + 1,
      count: String(milestone.count),
      title: milestone.title ?? "",
      rewards: rewardDraftsFrom(milestone.rewards)
    })),
    inviterTrigger: source.inviterTrigger,
    triggerSignInDays: String(source.triggerSignInDays || 1),
    inviterDailyLimit: String(source.inviterDailyLimit ?? 0),
    inviterTotalLimit: String(source.inviterTotalLimit ?? 0),
    notifyInviter: Boolean(source.notifyInviter),
    deviceLimit: String(source.deviceLimit ?? 0),
    ipDailyLimit: String(source.ipDailyLimit ?? 0),
    rulesText: source.rulesText ?? "",
    shareTemplate: source.shareTemplate ?? "",
    landingUrl: source.landingUrl ?? ""
  };
}

const toInt = (value: string) => {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : 0;
};

function toPayload(form: Form, catalog: PointsStoreRewardSpec[]): InviteSettingsPayload {
  return {
    enabled: form.enabled,
    registerField: form.registerField,
    fieldLabel: form.fieldLabel.trim(),
    fieldPlaceholder: form.fieldPlaceholder.trim(),
    bindWindowHours: toInt(form.bindWindowHours),
    codeLength: toInt(form.codeLength),
    codeCharset: form.codeCharset,
    codePrefix: normalizeCodeInput(form.codePrefix),
    allowCustomCode: form.allowCustomCode,
    inviterAudience: form.inviterAudience,
    inviterMinLevel: toInt(form.inviterMinLevel),
    inviteeRewards: rewardsFromDrafts(form.inviteeRewards, catalog),
    inviterRewards: rewardsFromDrafts(form.inviterRewards, catalog),
    milestones: form.milestones.map((milestone) => ({
      count: toInt(milestone.count),
      title: milestone.title.trim(),
      rewards: rewardsFromDrafts(milestone.rewards, catalog)
    })),
    inviterTrigger: form.inviterTrigger,
    triggerSignInDays: toInt(form.triggerSignInDays),
    inviterDailyLimit: toInt(form.inviterDailyLimit),
    inviterTotalLimit: toInt(form.inviterTotalLimit),
    notifyInviter: form.notifyInviter,
    deviceLimit: toInt(form.deviceLimit),
    ipDailyLimit: toInt(form.ipDailyLimit),
    rulesText: form.rulesText.trim(),
    shareTemplate: form.shareTemplate.trim(),
    landingUrl: form.landingUrl.trim()
  };
}

/**
 * 配置：开关与全部配置一起保存（PUT /invite/settings）。
 *
 * 枚举选项、默认值、上限与权益都来自目录；引用的套餐与功能在保存时由后端核对，
 * 校验失败（40000）的原因原样 toast 出来。草稿按应用绑定，无草稿即从服务端派生。
 */
export function InviteConfigPanel({ appKey }: { appKey: string }) {
  const catalogQuery = useInviteCatalogQuery(appKey);
  const settingsQuery = useInviteSettingsQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSaveInviteSettingsMutation(appKey);

  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const [sampleSeed, setSampleSeed] = useState(1);
  const shareRef = useRef<HTMLTextAreaElement>(null);
  const landingRef = useRef<HTMLInputElement>(null);

  const catalog: InviteCatalog | undefined = catalogQuery.data;
  const settings = settingsQuery.data;
  const rewardCatalog = catalog?.rewards ?? [];
  const limits = catalog?.limits;
  const maxRewards = limits?.maxRewards ?? 6;
  const maxMilestones = limits?.maxMilestones ?? 10;
  const plans = (plansQuery.data ?? []).filter((plan) => plan.kind !== "trial");
  const features = featuresQuery.data ?? [];

  const saved = seed(settings, catalog?.defaults);
  const form = draft?.scope === appKey ? draft.value : saved;
  const dirty = JSON.stringify(toPayload(form, rewardCatalog)) !== JSON.stringify(toPayload(saved, rewardCatalog));
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope: appKey, value: { ...form, [key]: value } });

  const minCodeLength =
    form.codeCharset === "digits" ? limits?.minDigitsCodeLength ?? 6 : limits?.minCodeLength ?? 4;
  const maxCodeLength = limits?.maxCodeLength ?? 16;
  const sampleLength = Math.min(maxCodeLength, Math.max(minCodeLength, toInt(form.codeLength)));
  const samplePrefix = normalizeCodeInput(form.codePrefix);
  const sampleBody = sampleInviteCode(form.codeCharset, sampleLength, "", sampleSeed);
  const sampleCode = `${samplePrefix}${sampleBody}`;
  const sampleLink = form.landingUrl.trim() ? form.landingUrl.trim().split("{code}").join(sampleCode) : "";
  const unreachable = form.registerField === "hidden" && toInt(form.bindWindowHours) === 0;

  /** 在光标处插入占位符，插完把光标放到占位符后面 */
  const insertToken = (
    field: "shareTemplate" | "landingUrl",
    element: HTMLTextAreaElement | HTMLInputElement | null,
    token: string
  ) => {
    const current = form[field];
    const start = element?.selectionStart ?? current.length;
    const end = element?.selectionEnd ?? current.length;
    patch(field, `${current.slice(0, start)}${token}${current.slice(end)}`);
    requestAnimationFrame(() => {
      if (!element) return;
      element.focus();
      element.setSelectionRange(start + token.length, start + token.length);
    });
  };

  const patchMilestone = (key: number, next: Partial<MilestoneDraft>) =>
    patch(
      "milestones",
      form.milestones.map((milestone) => (milestone.key === key ? { ...milestone, ...next } : milestone))
    );

  const addMilestone = () => {
    const lastCount = form.milestones.reduce((max, milestone) => Math.max(max, toInt(milestone.count)), 0);
    const key = form.milestones.reduce((max, milestone) => Math.max(max, milestone.key), 0) + 1;
    patch("milestones", [...form.milestones, { key, count: String(lastCount + 5), title: "", rewards: [] }]);
  };

  const save = async () => {
    const emptyMilestone = form.milestones.find((milestone) => milestone.rewards.length === 0);
    if (emptyMilestone) {
      toast.error(`邀请满 ${toInt(emptyMilestone.count)} 人的阶梯没有配置奖励`);
      return;
    }
    try {
      await saveMutation.mutateAsync(toPayload(form, rewardCatalog));
      setDraft(null);
      toast.success("邀请配置已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  if (settingsQuery.isLoading || catalogQuery.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-28 w-full rounded-2xl" />
        <Skeleton className="h-64 w-full rounded-2xl" />
        <Skeleton className="h-64 w-full rounded-2xl" />
      </div>
    );
  }

  const registerFields = catalog?.registerFields ?? (["hidden", "optional", "required"] as InviteRegisterField[]);
  const charsets = catalog?.charsets ?? (["alnum", "digits", "letters"] as InviteCharset[]);
  const audiences = catalog?.audiences ?? (["all", "vip"] as InviteAudience[]);
  const triggers = catalog?.triggers ?? (["bind", "signin"] as InviteTrigger[]);

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<UserPlus className="size-4" />}
        title="邀请"
        aside={
          <div className="flex items-center gap-2">
            {settings && !settings.configured ? (
              <Badge variant="outline" size="sm">
                未配置
              </Badge>
            ) : null}
            <StatusDot active={Boolean(settings?.enabled)} labelActive="已开启" labelInactive="未开启" />
          </div>
        }
      >
        <SwitchRow
          label="开启邀请"
          icon={<UserPlus className="size-4" />}
          checked={form.enabled}
          onChange={(value) => patch("enabled", value)}
        />
      </SectionCard>

      <SectionCard icon={<FileText className="size-4" />} title="注册表单">
        <div className="space-y-5">
          <FieldGroup label="邀请码字段">
            <RadioGroup
              value={form.registerField}
              onValueChange={(value) => patch("registerField", value as InviteRegisterField)}
            >
              <div className="grid gap-2 sm:grid-cols-3">
                {registerFields.map((field) => (
                  <ModeCard
                    key={field}
                    value={field}
                    active={form.registerField === field}
                    title={REGISTER_FIELD_META[field]?.title ?? field}
                    description={REGISTER_FIELD_META[field]?.description ?? ""}
                    icon={<FileText className="size-4" />}
                  />
                ))}
              </div>
            </RadioGroup>
          </FieldGroup>
          <div className="grid gap-4 sm:grid-cols-3">
            <TextField
              label="字段名称"
              value={form.fieldLabel}
              maxLength={MAX_FIELD_LABEL}
              onChange={(value) => patch("fieldLabel", value)}
            />
            <TextField
              label="输入提示"
              value={form.fieldPlaceholder}
              maxLength={MAX_PLACEHOLDER}
              onChange={(value) => patch("fieldPlaceholder", value)}
            />
            <NumberField
              label="补填时限"
              value={form.bindWindowHours}
              onChange={(value) => patch("bindWindowHours", value)}
              min={0}
              max={limits?.maxBindWindowHours ?? 720}
              unit={`小时，0 为只能注册时填写，最多 ${limits?.maxBindWindowHours ?? 720}`}
            />
          </div>
          {unreachable ? (
            <p className="text-xs text-amber-600 dark:text-amber-400">注册表单不出现且补填时限为 0 时，用户无法填写邀请码</p>
          ) : null}
        </div>
      </SectionCard>

      <SectionCard icon={<KeyRound className="size-4" />} title="邀请码格式">
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">字符集</Label>
              <Select value={form.codeCharset} onValueChange={(value) => patch("codeCharset", value as InviteCharset)}>
                <SelectTrigger className="h-9 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {charsets.map((charset) => (
                    <SelectItem key={charset} value={charset}>
                      {CHARSET_LABEL[charset] ?? charset}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <NumberField
              label="长度"
              value={form.codeLength}
              onChange={(value) => patch("codeLength", value)}
              min={minCodeLength}
              max={maxCodeLength}
              unit={`${minCodeLength}–${maxCodeLength} 位，不含前缀`}
            />
            <TextField
              label="前缀"
              value={form.codePrefix}
              maxLength={limits?.maxPrefixLength ?? 6}
              mono
              placeholder="不设置"
              hint={`字母与数字，最多 ${limits?.maxPrefixLength ?? 6} 位`}
              onChange={(value) => patch("codePrefix", normalizeCodeInput(value))}
            />
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
            <div className="min-w-0 space-y-0.5">
              <div className="text-[11px] text-muted-foreground">示例</div>
              <div className="font-mono text-base font-semibold tracking-[0.12em] break-all">
                {samplePrefix ? <span className="text-muted-foreground">{samplePrefix}</span> : null}
                {sampleBody}
              </div>
            </div>
            <Button size="sm" variant="ghost" className="h-8" onClick={() => setSampleSeed((value) => value + 1)}>
              <RefreshCw className="size-3.5" /> 换一个
            </Button>
          </div>
          <SwitchRow
            label="允许用户自定义邀请码"
            hint={`${limits?.minCustomCodeLength ?? 4}–${limits?.maxCustomCodeLength ?? 16} 位字母与数字`}
            checked={form.allowCustomCode}
            onChange={(value) => patch("allowCustomCode", value)}
          />
        </div>
      </SectionCard>

      <SectionCard icon={<UserCheck className="size-4" />} title="邀请人资格">
        <div className="space-y-4">
          <RadioGroup
            value={form.inviterAudience}
            onValueChange={(value) => patch("inviterAudience", value as InviteAudience)}
          >
            <div className="grid gap-2 sm:grid-cols-2">
              {audiences.map((audience) => (
                <ModeCard
                  key={audience}
                  value={audience}
                  active={form.inviterAudience === audience}
                  title={AUDIENCE_META[audience]?.title ?? audience}
                  description={AUDIENCE_META[audience]?.description ?? ""}
                  icon={AUDIENCE_ICON[audience] ?? <Users className="size-4" />}
                />
              ))}
            </div>
          </RadioGroup>
          <div className="grid gap-4 sm:grid-cols-3">
            <NumberField
              label="等级门槛"
              value={form.inviterMinLevel}
              onChange={(value) => patch("inviterMinLevel", value)}
              min={0}
              max={MAX_MIN_LEVEL}
              unit="Lv，0 为不限"
            />
          </div>
        </div>
      </SectionCard>

      <SectionCard icon={<Gift className="size-4" />} title="奖励">
        <div className="space-y-6">
          <div className="grid gap-5 lg:grid-cols-2">
            <FieldGroup label="被邀请人奖励" hint={`${form.inviteeRewards.length}/${maxRewards}`}>
              <RewardListEditor
                catalog={rewardCatalog}
                maxRewards={maxRewards}
                rewards={form.inviteeRewards}
                plans={plans}
                features={features}
                emptyText="不发奖励"
                addLabel="添加奖励"
                onChange={(rewards) => patch("inviteeRewards", rewards)}
              />
            </FieldGroup>
            <FieldGroup label="邀请人奖励" hint={`${form.inviterRewards.length}/${maxRewards}`}>
              <RewardListEditor
                catalog={rewardCatalog}
                maxRewards={maxRewards}
                rewards={form.inviterRewards}
                plans={plans}
                features={features}
                emptyText="不发奖励"
                addLabel="添加奖励"
                onChange={(rewards) => patch("inviterRewards", rewards)}
              />
            </FieldGroup>
          </div>

          <FieldGroup label="阶梯奖励" hint={`${form.milestones.length}/${maxMilestones}`}>
            <div className="space-y-3">
              {form.milestones.length === 0 ? (
                <p className="rounded-lg border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
                  暂无阶梯
                </p>
              ) : null}
              {form.milestones.map((milestone) => (
                <div key={milestone.key} className="space-y-3 rounded-xl border border-border p-4">
                  <div className="flex items-end gap-3">
                    <div className="grid flex-1 gap-4 sm:grid-cols-2">
                      <NumberField
                        label="累计成功邀请"
                        value={milestone.count}
                        onChange={(value) => patchMilestone(milestone.key, { count: value })}
                        min={1}
                        max={MAX_MILESTONE_COUNT}
                        unit="人"
                      />
                      <TextField
                        label="名称"
                        value={milestone.title}
                        maxLength={MAX_MILESTONE_TITLE}
                        placeholder="不设置"
                        onChange={(value) => patchMilestone(milestone.key, { title: value })}
                      />
                    </div>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-9 shrink-0"
                      aria-label="移除阶梯"
                      onClick={() =>
                        patch(
                          "milestones",
                          form.milestones.filter((item) => item.key !== milestone.key)
                        )
                      }
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                  <RewardListEditor
                    catalog={rewardCatalog}
                    maxRewards={maxRewards}
                    rewards={milestone.rewards}
                    plans={plans}
                    features={features}
                    emptyText="暂无奖励"
                    addLabel="添加奖励"
                    onChange={(rewards) => patchMilestone(milestone.key, { rewards })}
                  />
                </div>
              ))}
              {form.milestones.length < maxMilestones ? (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
                  onClick={addMilestone}
                >
                  <Plus className="size-3.5" /> 添加阶梯
                </Button>
              ) : null}
            </div>
          </FieldGroup>

          <FieldGroup label="邀请人奖励发放时机">
            <RadioGroup
              value={form.inviterTrigger}
              onValueChange={(value) => patch("inviterTrigger", value as InviteTrigger)}
            >
              <div className="grid gap-2 sm:grid-cols-2">
                {triggers.map((trigger) => (
                  <ModeCard
                    key={trigger}
                    value={trigger}
                    active={form.inviterTrigger === trigger}
                    title={TRIGGER_META[trigger]?.title ?? trigger}
                    description={TRIGGER_META[trigger]?.description ?? ""}
                    icon={TRIGGER_ICON[trigger] ?? <Gift className="size-4" />}
                  />
                ))}
              </div>
            </RadioGroup>
          </FieldGroup>

          <div className="grid gap-4 sm:grid-cols-3">
            {form.inviterTrigger === "signin" ? (
              <NumberField
                label="被邀请人累计签到"
                value={form.triggerSignInDays}
                onChange={(value) => patch("triggerSignInDays", value)}
                min={1}
                max={MAX_SIGNIN_DAYS}
                unit="天"
              />
            ) : null}
            <NumberField
              label="邀请人每日上限"
              value={form.inviterDailyLimit}
              onChange={(value) => patch("inviterDailyLimit", value)}
              min={0}
              max={MAX_INVITER_LIMIT}
              unit="次，0 为不限"
            />
            <NumberField
              label="邀请人累计上限"
              value={form.inviterTotalLimit}
              onChange={(value) => patch("inviterTotalLimit", value)}
              min={0}
              max={MAX_INVITER_LIMIT}
              unit="次，0 为不限"
            />
          </div>

          <SwitchRow
            label="邀请人获得奖励时发送站内信"
            icon={<Bell className="size-4" />}
            checked={form.notifyInviter}
            onChange={(value) => patch("notifyInviter", value)}
          />
        </div>
      </SectionCard>

      <SectionCard icon={<ShieldCheck className="size-4" />} title="防刷">
        <div className="grid gap-4 sm:grid-cols-3">
          <NumberField
            label="同一设备最多填写"
            value={form.deviceLimit}
            onChange={(value) => patch("deviceLimit", value)}
            min={0}
            max={MAX_DEVICE_LIMIT}
            unit="次，0 为不限"
          />
          <NumberField
            label="同一 IP 每日最多填写"
            value={form.ipDailyLimit}
            onChange={(value) => patch("ipDailyLimit", value)}
            min={0}
            max={MAX_IP_DAILY_LIMIT}
            unit="次，0 为不限"
          />
        </div>
      </SectionCard>

      <SectionCard icon={<PenLine className="size-4" />} title="文案">
        <div className="space-y-5">
          <FieldGroup label="活动规则" hint={`${form.rulesText.length}/${limits?.maxRulesLength ?? 2000}`}>
            <Textarea
              value={form.rulesText}
              rows={5}
              maxLength={limits?.maxRulesLength ?? 2000}
              onChange={(event) => patch("rulesText", event.target.value)}
            />
          </FieldGroup>

          <FieldGroup label="分享文案" hint={`${form.shareTemplate.length}/${limits?.maxShareLength ?? 300}`}>
            <Textarea
              ref={shareRef}
              value={form.shareTemplate}
              rows={3}
              maxLength={limits?.maxShareLength ?? 300}
              onChange={(event) => patch("shareTemplate", event.target.value)}
            />
            <TokenChips
              tokens={SHARE_TOKENS}
              onInsert={(token) => insertToken("shareTemplate", shareRef.current, token)}
            />
          </FieldGroup>

          <FieldGroup label="落地页">
            <Input
              ref={landingRef}
              value={form.landingUrl}
              maxLength={MAX_LANDING_URL}
              placeholder="https://example.com/invite?code={code}"
              className="font-mono text-xs"
              onChange={(event) => patch("landingUrl", event.target.value)}
            />
            <TokenChips
              tokens={SHARE_TOKENS.slice(0, 1)}
              onInsert={(token) => insertToken("landingUrl", landingRef.current, token)}
            />
            {sampleLink ? (
              <p className="font-mono text-[11px] break-all text-muted-foreground">{sampleLink}</p>
            ) : null}
          </FieldGroup>
        </div>
      </SectionCard>

      <div className="sticky bottom-3 z-10 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border bg-card px-4 py-3">
        <span className="text-xs text-muted-foreground">
          {settings?.updatedAt
            ? `${settings.updatedBy ? `${settings.updatedBy} ` : ""}保存于 ${formatDateTimeMinute(settings.updatedAt)}`
            : "尚未保存"}
        </span>
        <div className="flex items-center gap-2">
          {dirty ? (
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1.5"
              disabled={saveMutation.isPending}
              onClick={() => setDraft(null)}
            >
              <RotateCcw className="size-3.5" />
              放弃修改
            </Button>
          ) : null}
          <Button size="sm" className="h-8 gap-1.5" disabled={!dirty || saveMutation.isPending} onClick={() => void save()}>
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>
    </div>
  );
}

function TextField({
  label,
  value,
  onChange,
  maxLength,
  placeholder,
  hint,
  mono
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  maxLength?: number;
  placeholder?: string;
  hint?: string;
  mono?: boolean;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        {maxLength ? (
          <span className="text-[10px] text-muted-foreground tabular-nums">
            {value.length}/{maxLength}
          </span>
        ) : null}
      </div>
      <Input
        value={value}
        maxLength={maxLength}
        placeholder={placeholder}
        className={mono ? "font-mono text-xs" : "text-xs"}
        onChange={(event) => onChange(event.target.value)}
      />
      {hint ? <p className="text-[10px] leading-snug text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function TokenChips({
  tokens,
  onInsert
}: {
  tokens: Array<{ token: string; label: string }>;
  onInsert: (token: string) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-[11px] text-muted-foreground">插入</span>
      {tokens.map((item) => (
        <button
          key={item.token}
          type="button"
          onClick={() => onInsert(item.token)}
          className="rounded-md border border-border bg-muted px-1.5 py-0.5 text-[11px] transition-colors hover:bg-muted-foreground/10"
        >
          <span className="font-mono">{item.token}</span>
          <span className="ml-1 text-muted-foreground">{item.label}</span>
        </button>
      ))}
    </div>
  );
}
