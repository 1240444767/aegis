"use client";

import {
  CheckCircle2,
  Fingerprint,
  KeyRound,
  Loader2,
  LogIn,
  MinusCircle,
  Play,
  Stethoscope,
  UserPlus,
  XCircle
} from "lucide-react";
import type { AuthProtocolPolicy, SelfTestResult } from "@/lib/api/app-auth-protocol";
import { buildEnvSnippet } from "@/lib/integration-snippets";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { StatTile } from "@/components/content/content-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  CopyButton,
  LOGIN_METHODS,
  REGISTER_METHODS,
  formatTime,
  levelMeta,
  methodLabels
} from "./auth-protocol-shared";

/**
 * 概览：接入方最先要的三样 —— 现在是什么配置、往哪发拿什么凭据、链路通没通。
 * 这里读的是服务端已保存的策略，不是草稿：它回答的是「客户端此刻看到的是什么」。
 */
export function AuthProtocolOverview({
  appKey,
  baseUrl,
  policy,
  selfTest,
  selfTesting,
  onRunSelfTest,
  onManageSecret
}: {
  appKey: string;
  baseUrl: string;
  policy: AuthProtocolPolicy;
  selfTest: SelfTestResult | null;
  selfTesting: boolean;
  onRunSelfTest: () => void;
  onManageSecret: () => void;
}) {
  const level = levelMeta(policy.securityLevel);
  const logins = methodLabels(policy.loginMethods, LOGIN_METHODS);
  const registers = methodLabels(policy.registerMethods, REGISTER_METHODS);
  const needsSecret = policy.securityLevel !== "standard";

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="安全等级" icon={level.icon} value={level.title} hint={level.requirement} />
        <StatTile
          label="登录方式"
          icon={LogIn}
          value={logins.length ? `${logins.length} 种` : "未开放"}
          tone={logins.length ? "default" : "muted"}
          hint={logins.join("、") || undefined}
        />
        <StatTile
          label="注册方式"
          icon={UserPlus}
          value={registers.length ? `${registers.length} 种` : "未开放"}
          tone={registers.length ? "default" : "muted"}
          hint={registers.join("、") || undefined}
        />
        <StatTile
          label="接入自检"
          icon={Stethoscope}
          value={selfTest ? (selfTest.ok ? "通过" : "未通过") : "未运行"}
          tone={selfTest?.ok ? "positive" : "muted"}
          hint={selfTest ? `${formatTime(selfTest.startedAt)}，耗时 ${selfTest.durationMs} ms` : undefined}
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <SectionCard
          icon={<Fingerprint className="size-4" />}
          title="接入凭据"
          aside={
            <CopyButton
              value={buildEnvSnippet({ baseUrl, appKey, level: policy.securityLevel })}
              label="复制 .env"
              className="h-8 border bg-background px-2.5"
            />
          }
        >
          <dl className="divide-y divide-border">
            <CredentialRow label="Base URL" value={baseUrl} placeholder="当前站点" />
            <CredentialRow label="App Key" value={appKey} />
            <div className="flex items-center justify-between gap-3 py-2.5">
              <dt className="shrink-0 text-xs text-muted-foreground">App Secret</dt>
              <dd className="flex min-w-0 items-center gap-2">
                {needsSecret ? (
                  <>
                    <span className="truncate font-mono text-xs">
                      {policy.signingSecretSet ? policy.signingSecretHint : "尚未签发"}
                    </span>
                    <Button size="sm" variant="ghost" className="h-7 gap-1 px-2 text-xs" onClick={onManageSecret}>
                      <KeyRound className="size-3.5" />
                      管理
                    </Button>
                  </>
                ) : (
                  <span className="text-xs text-muted-foreground">标准档无需密钥</span>
                )}
              </dd>
            </div>
          </dl>
        </SectionCard>

        <SectionCard
          icon={<Stethoscope className="size-4" />}
          title="接入自检"
          aside={
            <Button size="sm" variant="outline" className="h-8 gap-1.5" disabled={selfTesting} onClick={onRunSelfTest}>
              {selfTesting ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
              {selfTest ? "重新运行" : "运行自检"}
            </Button>
          }
        >
          {selfTest ? (
            <SelfTestTimeline result={selfTest} />
          ) : (
            <div className="flex flex-col items-center gap-2 py-6 text-center">
              <span className="grid size-10 place-items-center rounded-xl bg-muted text-muted-foreground">
                <Stethoscope className="size-5" />
              </span>
              <p className="text-sm text-muted-foreground">尚未运行</p>
            </div>
          )}
        </SectionCard>
      </div>
    </div>
  );
}

function CredentialRow({ label, value, placeholder }: { label: string; value: string; placeholder?: string }) {
  return (
    <div className="flex items-center justify-between gap-3 py-2.5">
      <dt className="shrink-0 text-xs text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 items-center gap-1">
        <span className={cn("truncate font-mono text-xs", !value && "text-muted-foreground")}>{value || placeholder}</span>
        {value ? <CopyButton value={value} /> : null}
      </dd>
    </div>
  );
}

/** 自检步骤：竖向时间线，每一步一个状态点，失败的那一步给出修复建议。 */
function SelfTestTimeline({ result }: { result: SelfTestResult }) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={result.ok ? "success" : "danger"} size="sm">
          {result.ok ? "全部通过" : "存在失败"}
        </Badge>
        <span className="text-xs text-muted-foreground">{levelMeta(result.securityLevel).title}档</span>
        <span className="ml-auto font-mono text-xs tabular-nums text-muted-foreground">{result.durationMs} ms</span>
      </div>
      <ol className="relative space-y-0">
        {result.steps.map((step, index) => {
          const failed = !step.ok && !step.skipped;
          const last = index === result.steps.length - 1;
          return (
            <li key={step.key} className="relative flex gap-3 pb-3 last:pb-0">
              {!last ? <span className="absolute left-[9px] top-5 h-[calc(100%-12px)] w-px bg-border" aria-hidden /> : null}
              <span className="relative z-[1] mt-0.5 grid size-[19px] shrink-0 place-items-center rounded-full bg-card">
                {step.skipped ? (
                  <MinusCircle className="size-[18px] text-muted-foreground" />
                ) : step.ok ? (
                  <CheckCircle2 className="size-[18px] text-emerald-600 dark:text-emerald-400" />
                ) : (
                  <XCircle className="size-[18px] text-destructive" />
                )}
              </span>
              <div className="min-w-0 flex-1 space-y-0.5">
                <div className="flex items-baseline justify-between gap-2">
                  <span className={cn("text-sm", failed ? "font-medium text-destructive" : "text-foreground")}>
                    {step.title}
                  </span>
                  {!step.skipped ? (
                    <span className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
                      {step.durationMs} ms
                    </span>
                  ) : null}
                </div>
                {step.detail ? <p className="break-all text-xs text-muted-foreground">{step.detail}</p> : null}
                {step.hint ? (
                  <p className="rounded-md bg-amber-50/70 px-2 py-1 text-xs text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
                    {step.hint}
                  </p>
                ) : null}
              </div>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
