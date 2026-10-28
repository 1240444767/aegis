"use client";

import { BellRing, CircleCheck, Hourglass, KeyRound, Loader2, PanelTop, RotateCcw, Save, TriangleAlert, User, UserCog, Users } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { FieldGroup, ModeCard, NoAppSelected, NumberField, SectionCard, StatusDot, SwitchRow } from "@/components/apps/app-config-primitives";
import { InlineNotice, MethodToggle, toggleValue, type MethodOption } from "@/components/apps/auth-protocol/auth-protocol-shared";
import { StatTile } from "@/components/content/content-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RadioGroup } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { AccountSetupFirstPrompt, AccountSetupPolicy, AccountSetupSource } from "@/lib/api/account-setup";
import { useAccountSetupPolicyQuery, useSaveAccountSetupPolicyMutation } from "@/lib/account-setup-hooks";
import { ACCOUNT_SETUP_SOURCES } from "./account-setup-shared";

/**
 * 账号初始化区块：快速登录建的号要不要、怎么引导补设用户名与密码。
 *
 * 第三方、短信、卡密登录首次建号时账号名由系统生成且没有密码。建号时不管策略开没开都登记，
 * 策略只决定提不提醒；是否需要提醒、弹窗还是横幅全部由后端判定（docs/account-setup.md）。
 * 单个用户的改名与「要求重新设置」在用户详情里（资料页签的账号字段、安全页签的账号初始化）。
 *
 * 保存前的两条校验与后端 NormalizeAccountSetupPolicy 一致（40968），以后端为准，原因原样 toast。
 */

const REQUIREMENTS: MethodOption[] = [
  { value: "account", label: "用户名", icon: User },
  { value: "password", label: "密码", icon: KeyRound }
];

const PROMPTS: Array<{ value: AccountSetupFirstPrompt; title: string; description: string; icon: React.ReactNode }> = [
  { value: "dialog", title: "首次弹窗", description: "第一次弹窗，之后显示横幅", icon: <BellRing className="size-4" /> },
  { value: "banner", title: "仅横幅", description: "一律显示横幅", icon: <PanelTop className="size-4" /> }
];

/** 与后端 accountSetupMaxSnoozeHours 一致 */
const MAX_SNOOZE_HOURS = 24 * 90;

type Form = {
  enabled: boolean;
  sources: AccountSetupSource[];
  requireAccount: boolean;
  requirePassword: boolean;
  firstPrompt: AccountSetupFirstPrompt;
  /** 字符串态以支持编辑中的空串 */
  snoozeHours: string;
};

function seedForm(policy?: AccountSetupPolicy): Form {
  return {
    enabled: policy?.enabled ?? true,
    sources: [...(policy?.sources ?? ["oauth", "sms"])],
    requireAccount: policy?.requireAccount ?? true,
    requirePassword: policy?.requirePassword ?? true,
    firstPrompt: policy?.firstPrompt ?? "dialog",
    snoozeHours: String(policy?.snoozeHours ?? 72)
  };
}

function sameForm(a: Form, b: Form) {
  return (
    a.enabled === b.enabled &&
    a.sources.length === b.sources.length &&
    a.sources.every((source) => b.sources.includes(source)) &&
    a.requireAccount === b.requireAccount &&
    a.requirePassword === b.requirePassword &&
    a.firstPrompt === b.firstPrompt &&
    a.snoozeHours === b.snoozeHours
  );
}

function snoozeText(hours: number) {
  if (hours <= 0) return "横幅不能关闭";
  if (hours % 24 === 0) return `关闭后 ${hours / 24} 天再出现`;
  return `关闭后 ${hours} 小时再出现`;
}

function formProblem(form: Form): string | null {
  const hours = Number(form.snoozeHours);
  if (form.snoozeHours.trim() === "" || !Number.isInteger(hours) || hours < 0 || hours > MAX_SNOOZE_HOURS) {
    return `横幅再次出现的间隔应在 0 到 ${MAX_SNOOZE_HOURS} 小时之间`;
  }
  if (!form.enabled) return null;
  if (form.sources.length === 0) return "至少选择一种建号方式";
  if (!form.requireAccount && !form.requirePassword) return "至少要求设置用户名或密码之一";
  return null;
}

const count = (value?: number | null) => (value ?? 0).toLocaleString("zh-CN");

export function AppAccountSetupPanel({ appKey }: { appKey?: string | null }) {
  const query = useAccountSetupPolicyQuery(appKey);
  const saveMutation = useSaveAccountSetupPolicyMutation(appKey);
  const view = query.data;
  const loading = query.isLoading;

  // 草稿按应用绑定：无草稿即从服务端派生，切换应用自动丢弃上一个应用的改动
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const scope = appKey ?? "";
  const saved = seedForm(view?.policy);
  const form = draft?.scope === scope ? draft.value : saved;
  const dirty = !sameForm(form, saved);
  const patch = (changes: Partial<Form>) => setDraft({ scope, value: { ...form, ...changes } });
  const problem = formProblem(form);
  const snoozeHours = Math.max(0, Number(form.snoozeHours) || 0);

  if (!appKey) return <NoAppSelected icon={<UserCog className="size-5" />} />;

  const stats = view?.stats;
  const bySource = ACCOUNT_SETUP_SOURCES.map((item) => `${item.label.replace("登录", "")} ${count(stats?.bySource?.[item.value])}`).join("，");

  async function save() {
    if (problem) {
      toast.error(problem);
      return;
    }
    try {
      await saveMutation.mutateAsync({
        enabled: form.enabled,
        sources: form.sources,
        requireAccount: form.requireAccount,
        requirePassword: form.requirePassword,
        firstPrompt: form.firstPrompt,
        snoozeHours
      });
      setDraft(null);
      toast.success("账号初始化策略已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {loading ? (
          Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-[86px] rounded-xl" />)
        ) : (
          <>
            <StatTile label="快速登录账号" icon={Users} value={count(stats?.tracked)} hint={bySource} />
            <StatTile label="已完成" icon={CircleCheck} tone="positive" value={count(stats?.completed)} />
            <StatTile label="未完成" icon={Hourglass} value={count(stats?.pending)} hint={`用户名待设置 ${count(stats?.accountPending)}`} />
            <StatTile label="已弹窗提醒" icon={BellRing} value={count(stats?.prompted)} />
          </>
        )}
      </div>

      <SectionCard
        icon={<UserCog className="size-4" />}
        title="初始化策略"
        aside={
          loading ? (
            <Skeleton className="h-5 w-16 rounded-full" />
          ) : (
            <div className="flex items-center gap-2">
              {view?.policy.isDefault ? (
                <Badge variant="outline" size="sm">
                  默认策略
                </Badge>
              ) : null}
              <StatusDot active={Boolean(view?.policy.enabled)} labelActive="已开启" labelInactive="未开启" />
            </div>
          )
        }
      >
        {loading ? (
          <div className="space-y-4">
            <Skeleton className="h-12 w-full rounded-xl" />
            <div className="grid gap-2 sm:grid-cols-3">
              {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-12 rounded-xl" />)}
            </div>
            <Skeleton className="h-16 w-full rounded-xl" />
            <Skeleton className="h-9 w-full" />
          </div>
        ) : (
          <div className="space-y-5">
            <SwitchRow
              label="引导快速登录账号设置用户名与密码"
              icon={<UserCog className="size-4" />}
              checked={form.enabled}
              onChange={(enabled) => patch({ enabled })}
            />

            <FieldGroup label="建号方式">
              <div className="grid gap-2 sm:grid-cols-3">
                {ACCOUNT_SETUP_SOURCES.map((option) => (
                  <MethodToggle
                    key={option.value}
                    option={option}
                    checked={form.sources.includes(option.value)}
                    disabled={!form.enabled}
                    onToggle={() => patch({ sources: toggleValue(form.sources, option.value) as AccountSetupSource[] })}
                  />
                ))}
              </div>
            </FieldGroup>

            <FieldGroup label="要求设置">
              <div className="grid gap-2 sm:grid-cols-3">
                {REQUIREMENTS.map((option) => (
                  <MethodToggle
                    key={option.value}
                    option={option}
                    checked={option.value === "account" ? form.requireAccount : form.requirePassword}
                    disabled={!form.enabled}
                    onToggle={() =>
                      patch(option.value === "account" ? { requireAccount: !form.requireAccount } : { requirePassword: !form.requirePassword })
                    }
                  />
                ))}
              </div>
              {view?.rules.hint ? (
                <p className="text-[11px] text-muted-foreground">用户名规则：{view.rules.hint}</p>
              ) : null}
            </FieldGroup>

            <FieldGroup label="提醒方式">
              <RadioGroup
                value={form.firstPrompt}
                onValueChange={(value) => patch({ firstPrompt: value as AccountSetupFirstPrompt })}
                disabled={!form.enabled}
              >
                <div className="grid gap-2 sm:grid-cols-2">
                  {PROMPTS.map((item) => (
                    <ModeCard
                      key={item.value}
                      value={item.value}
                      active={form.firstPrompt === item.value}
                      title={item.title}
                      description={item.description}
                      icon={item.icon}
                      disabled={!form.enabled}
                    />
                  ))}
                </div>
              </RadioGroup>
              <div className="grid gap-4 sm:grid-cols-[minmax(0,220px)_1fr] sm:items-start">
                <NumberField
                  label="横幅再次出现"
                  unit="小时"
                  min={0}
                  max={MAX_SNOOZE_HOURS}
                  value={form.snoozeHours}
                  disabled={!form.enabled}
                  onChange={(snoozeHours) => patch({ snoozeHours })}
                  hint={snoozeText(snoozeHours)}
                />
              </div>
            </FieldGroup>

            {problem ? (
              <InlineNotice tone="warning" icon={TriangleAlert}>
                {problem}
              </InlineNotice>
            ) : null}

            <div className="flex flex-wrap items-center justify-end gap-2 border-t pt-4">
              {dirty ? (
                <Button variant="outline" size="sm" className="h-8 gap-1.5" disabled={saveMutation.isPending} onClick={() => setDraft(null)}>
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
        )}
      </SectionCard>
    </div>
  );
}
