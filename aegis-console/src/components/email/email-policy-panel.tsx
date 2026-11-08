"use client";

import { useState } from "react";
import { Ban, Gauge, KeyRound, Link2, Loader2, Plus, RotateCcw, Save, Trash2 } from "lucide-react";
import { SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
import { AutoSkeleton } from "@/components/ui/auto-skeleton";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { EmailCodePolicy } from "@/lib/api/email-policy";
import { formatDateTimeMinute } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import type { EmailPolicyDraft } from "./use-email-policy-draft";

/** 与后端 PolicyLimits 一致；接口下发了以接口为准 */
const FALLBACK_LIMITS: Record<keyof EmailCodePolicy, [number, number]> = {
  length: [4, 8],
  ttlMinutes: [1, 60],
  resendSeconds: [0, 600],
  maxAttempts: [1, 20],
  addressHourly: [0, 100],
  addressDaily: [0, 500],
  ipHourly: [0, 1000]
};

/**
 * 发信策略：验证码规则、频次上限、退信抑制、重置链接白名单。
 *
 * 与模板视图共用一份草稿（见 useEmailPolicyDraft），两边任一处保存即一起生效。
 */
export function EmailPolicyPanel({ draft, canWrite }: { draft: EmailPolicyDraft; canWrite: boolean }) {
  const policy = draft.policy;
  const limits = { ...FALLBACK_LIMITS, ...(draft.view?.limits ?? {}) } as Record<keyof EmailCodePolicy, [number, number]>;
  const setCode = (key: keyof EmailCodePolicy, value: number) =>
    draft.update((prev) => ({ ...prev, code: { ...prev.code, [key]: value } }));
  const links = policy.resetLinks;
  const setLinks = (next: string[]) => draft.update((prev) => ({ ...prev, resetLinks: next }));
  const maxLinks = draft.view?.limits?.resetLinks?.[1] ?? 10;
  const quotaConflict =
    policy.code.addressHourly > 0 && policy.code.addressDaily > 0 && policy.code.addressHourly > policy.code.addressDaily;

  const field = (key: keyof EmailCodePolicy, label: string, unit: string, hint?: string) => (
    <PolicyNumber
      label={label}
      unit={unit}
      hint={hint}
      value={policy.code[key]}
      min={limits[key][0]}
      max={limits[key][1]}
      disabled={!canWrite}
      onCommit={(value) => setCode(key, value)}
    />
  );

  return (
    <AutoSkeleton loading={draft.loading}>
      <div className="space-y-4">
        <SectionCard icon={<KeyRound className="size-4" />} title="验证码" description="邮箱验证码的位数、有效期与防试错">
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {field("length", "位数", "位")}
            {field("ttlMinutes", "有效期", "分钟", "调用方可以要求更短，不能更长")}
            {field("resendSeconds", "重发间隔", "秒，0 为不限")}
            {field("maxAttempts", "错误次数上限", "次", "错满即作废，需重新获取")}
          </div>
        </SectionCard>

        <SectionCard icon={<Gauge className="size-4" />} title="发送频次" description="防止接口被用来轰炸邮箱，被拒的请求不占额度">
          <div className="grid gap-4 sm:grid-cols-3">
            {field("addressHourly", "单个邮箱每小时", "封，0 为不限")}
            {field("addressDaily", "单个邮箱每天", "封，0 为不限")}
            {field("ipHourly", "单个 IP 每小时", "封，0 为不限", "按客户端地址计")}
          </div>
          {quotaConflict ? (
            <p className="mt-3 text-xs text-amber-600 dark:text-amber-400">每小时上限大于每天上限，保存时会被拒绝</p>
          ) : null}
        </SectionCard>

        <SectionCard icon={<Ban className="size-4" />} title="退信抑制" description="收到投递回执后自动把地址加入抑制名单，不再往里投递">
          <div className="grid gap-2 sm:grid-cols-2">
            <SwitchRow
              label="硬退信"
              hint="地址不存在或被永久拒收"
              checked={policy.suppression.hardBounce}
              disabled={!canWrite}
              onChange={(value) => draft.update((prev) => ({ ...prev, suppression: { ...prev.suppression, hardBounce: value } }))}
            />
            <SwitchRow
              label="投诉"
              hint="收件人把邮件标记为垃圾邮件"
              checked={policy.suppression.complaint}
              disabled={!canWrite}
              onChange={(value) => draft.update((prev) => ({ ...prev, suppression: { ...prev.suppression, complaint: value } }))}
            />
          </div>
          <p className="mt-3 text-[11px] text-muted-foreground">软退信与延迟不会触发抑制；SMTP 通道没有投递回执，不会自动抑制。</p>
        </SectionCard>

        <SectionCard
          icon={<Link2 className="size-4" />}
          title="密码重置链接"
          description="旧版找回接口发出的重置链接只能指向这些地址，令牌会拼在后面"
          aside={
            canWrite ? (
              <Button
                size="sm"
                variant="outline"
                className="h-8"
                disabled={links.length >= maxLinks}
                onClick={() => setLinks([...links, ""])}
              >
                <Plus className="size-3.5" /> 添加地址
              </Button>
            ) : null
          }
        >
          {links.length === 0 ? (
            <p className="rounded-xl border border-dashed px-4 py-6 text-center text-xs text-muted-foreground">
              未登记任何地址，旧版重置接口会拒绝发信。新接入请使用找回密码流程，不需要在这里登记。
            </p>
          ) : (
            <div className="space-y-2">
              {links.map((link, index) => (
                <div key={index} className="flex items-center gap-2">
                  <span className="w-5 shrink-0 text-right font-mono text-[11px] text-muted-foreground">{index + 1}</span>
                  <Input
                    value={link}
                    disabled={!canWrite}
                    placeholder="https://app.example.com/reset"
                    className="font-mono text-xs"
                    onChange={(event) => setLinks(links.map((item, i) => (i === index ? event.target.value : item)))}
                  />
                  {canWrite ? (
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-8 shrink-0 text-muted-foreground hover:text-destructive"
                      aria-label="移除"
                      onClick={() => setLinks(links.filter((_, i) => i !== index))}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  ) : null}
                </div>
              ))}
              <p className="pt-1 text-[11px] text-muted-foreground">
                匹配协议、域名与路径前缀；调用方不传地址时使用第一条。须使用 https，本机调试可用 http://localhost。
              </p>
            </div>
          )}
        </SectionCard>

        <PolicySaveBar draft={draft} canWrite={canWrite} />
      </div>
    </AutoSkeleton>
  );
}

/** 模板与策略两个视图底部共用的保存条 */
export function PolicySaveBar({ draft, canWrite }: { draft: EmailPolicyDraft; canWrite: boolean }) {
  const view = draft.view;
  return (
    <div className="sticky bottom-3 z-10 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border bg-card px-4 py-3 shadow-sm">
      <span className="text-xs text-muted-foreground">
        {!canWrite
          ? "没有修改权限"
          : draft.dirty
            ? "有未保存的修改，模板与发信策略一起保存"
            : view?.updatedAt
              ? `${view.updatedBy ? `${view.updatedBy} ` : ""}保存于 ${formatDateTimeMinute(view.updatedAt)}`
              : "使用默认策略"}
      </span>
      {canWrite ? (
        <div className="flex items-center gap-2">
          {draft.dirty ? (
            <Button variant="outline" size="sm" className="h-8 gap-1.5" disabled={draft.saving} onClick={draft.reset}>
              <RotateCcw className="size-3.5" /> 放弃修改
            </Button>
          ) : null}
          <Button size="sm" className="h-8 gap-1.5" disabled={!draft.dirty || draft.saving} onClick={() => void draft.save()}>
            {draft.saving ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      ) : null}
    </div>
  );
}

/**
 * 数值字段。输入过程中保留原始字符串（清空再输入不会先跳成 0），失焦或合法时提交整数。
 */
function PolicyNumber({
  label,
  unit,
  hint,
  value,
  min,
  max,
  disabled,
  onCommit
}: {
  label: string;
  unit: string;
  hint?: string;
  value: number;
  min: number;
  max: number;
  disabled?: boolean;
  onCommit: (value: number) => void;
}) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? String(value);
  const parsed = Number(shown);
  const invalid = shown.trim() === "" || !Number.isInteger(parsed) || parsed < min || parsed > max;
  return (
    <div className={cn("space-y-1.5", disabled && "opacity-60")}>
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        <span className="text-[10px] text-muted-foreground">{unit}</span>
      </div>
      <Input
        type="number"
        inputMode="numeric"
        min={min}
        max={max}
        value={shown}
        disabled={disabled}
        className={cn("font-mono text-sm tabular-nums", invalid && "border-destructive focus-visible:ring-destructive/30")}
        onChange={(event) => {
          const next = event.target.value;
          setText(next);
          const number = Number(next);
          if (next.trim() !== "" && Number.isInteger(number)) onCommit(number);
        }}
        onBlur={() => setText(null)}
      />
      <p className={cn("text-[10px] leading-snug", invalid ? "text-destructive" : "text-muted-foreground")}>
        {invalid ? `须在 ${min} 到 ${max} 之间` : hint ?? `范围 ${min} 到 ${max}`}
      </p>
    </div>
  );
}
