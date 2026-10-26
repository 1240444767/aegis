"use client";

/**
 * 安全：这个账号怎么证明「他是他」，以及这些证明现在还好不好使。
 *
 *   安全评分     由密码强度、二次验证、Passkey、恢复码、第三方绑定推导，逐项给出结论
 *   登录凭据     密码生命周期、二次验证、恢复码三列并排；Passkey 单独一张表
 *   登录绑定基线 强绑定策略下的设备 / IP / 属地基线，可重置
 *   第三方绑定   可解绑（含强制解绑）
 *   OAuth2 授权  可逐个或全部撤销
 *   活跃会话     多选下线、全部下线
 *   认证模块     服务端各认证模块的运行态
 *
 * 评分只是控制台按已拉取的字段算出来的参考值，后端没有这个分数。
 */

import { useMemo, useState, type ComponentType, type ReactNode } from "react";
import Link from "next/link";
import {
  BadgeCheck,
  CircleAlert,
  CircleCheck,
  CircleMinus,
  Fingerprint,
  KeyRound,
  Link2,
  Loader2,
  Lock,
  MonitorSmartphone,
  RotateCcw,
  ShieldCheck,
  ShieldHalf,
  Ticket,
  Unlock
} from "lucide-react";
import { toast } from "sonner";
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
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import { useAdminAppLoginBaselineQuery, useAdminAppPolicyQuery, useResetAdminAppLoginBaselineMutation } from "@/lib/admin-hooks";
import type { AdminAppUserDetail, AdminAppUserSecurity, PasskeyItem } from "@/lib/api/types";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { UserResetPasswordDialog } from "../user-reset-password-dialog";
import { UserSessionsPanel } from "../user-sessions-panel";
import {
  InfoGrid,
  RecordSheet,
  RecordTable,
  Section,
  StateBlock,
  TONE_TEXT,
  ToneIcon,
  type Column,
  type Tone
} from "./kit";
import { isPast, numberValue, textValue } from "./user-detail-shared";
import { UserOAuthBindingsPanel } from "./user-oauth-bindings-panel";
import { UserOAuth2GrantsPanel } from "./user-oauth2-grants-panel";

// ── 评分 ──────────────────────────

/** 强度分刻度（0~100）与后端 `password_strength_score` 同一把尺子。 */
function strengthOf(score?: number): { label: string; tone: Tone } {
  if (typeof score !== "number" || score <= 0) return { label: "未评估", tone: "neutral" };
  if (score < 40) return { label: "弱", tone: "danger" };
  if (score < 60) return { label: "一般", tone: "warning" };
  if (score < 80) return { label: "良好", tone: "success" };
  return { label: "强", tone: "success" };
}

type CheckState = "pass" | "warn" | "fail" | "na";

type Check = {
  key: string;
  label: string;
  icon: ComponentType<{ className?: string }>;
  state: CheckState;
  detail: string;
  /** 本项满分与得分 */
  weight: number;
  score: number;
};

/**
 * 满分 100：密码 30（按强度折算，过期只给三成）、二次验证 25、Passkey 20、
 * 恢复码 15（剩余 3 个以上满分，1~2 个给一半）、第三方绑定 10（有备用登录方式）。
 */
function buildChecks(security: AdminAppUserSecurity | undefined): Check[] {
  const strength = numberValue(security?.passwordStrengthScore);
  const expired = isPast(security?.passwordExpiresAt);
  const hasPassword = Boolean(security?.hasPassword);
  const totpOn = Boolean(security?.twoFactorEnabled) && !security?.twoFactor?.pendingSetup;
  const passkeys = numberValue(security?.passkeys?.count);
  const codesOn = Boolean(security?.recoveryCodes?.enabled);
  const codesLeft = numberValue(security?.recoveryCodes?.remaining);
  const bindings = numberValue(security?.oauth2Bindings);

  const passwordRatio = !hasPassword ? 0 : expired ? 0.3 : strength > 0 ? Math.min(1, strength / 100) : 0.5;
  const passwordState: CheckState = !hasPassword
    ? bindings > 0
      ? "warn"
      : "fail"
    : expired || security?.passwordChangeRequired || (strength > 0 && strength < 60)
      ? "warn"
      : "pass";

  return [
    {
      key: "password",
      label: "密码",
      icon: Lock,
      state: passwordState,
      detail: !hasPassword
        ? bindings > 0
          ? "未设置，仅能第三方登录"
          : "未设置"
        : expired
          ? "已过期"
          : security?.passwordChangeRequired
            ? "已标记强制改密"
            : `强度 ${strength > 0 ? strength : "未评估"}（${strengthOf(security?.passwordStrengthScore).label}）`,
      weight: 30,
      score: Math.round(30 * passwordRatio)
    },
    {
      key: "totp",
      label: "二次验证",
      icon: ShieldCheck,
      state: totpOn ? "pass" : security?.twoFactor?.pendingSetup ? "warn" : "fail",
      detail: totpOn ? textValue(security?.twoFactorMethod, "TOTP") + " 已启用" : security?.twoFactor?.pendingSetup ? "配置未完成" : "未启用",
      weight: 25,
      score: totpOn ? 25 : 0
    },
    {
      key: "passkey",
      label: "Passkey",
      icon: Fingerprint,
      state: passkeys > 0 ? "pass" : "fail",
      detail: passkeys > 0 ? `${passkeys} 个凭据` : "未注册",
      weight: 20,
      score: passkeys > 0 ? 20 : 0
    },
    {
      key: "recovery",
      label: "恢复码",
      icon: Ticket,
      state: !totpOn && !codesOn ? "na" : !codesOn || codesLeft === 0 ? "fail" : codesLeft <= 2 ? "warn" : "pass",
      detail: codesOn ? `剩余 ${codesLeft} / ${numberValue(security?.recoveryCodes?.total)}` : totpOn ? "未生成" : "未启用二次验证，无需恢复码",
      weight: 15,
      score: codesOn ? (codesLeft > 2 ? 15 : codesLeft > 0 ? 8 : 0) : 0
    },
    {
      key: "oauth",
      label: "第三方绑定",
      icon: Link2,
      state: bindings > 0 ? "pass" : "na",
      detail: bindings > 0 ? security?.oauth2Providers?.join("、") || `${bindings} 个` : "未绑定",
      weight: 10,
      score: bindings > 0 ? 10 : 0
    }
  ];
}

const CHECK_META: Record<CheckState, { icon: ComponentType<{ className?: string }>; tone: Tone; label: string }> = {
  pass: { icon: CircleCheck, tone: "success", label: "达标" },
  warn: { icon: CircleAlert, tone: "warning", label: "需关注" },
  fail: { icon: CircleAlert, tone: "danger", label: "缺失" },
  na: { icon: CircleMinus, tone: "neutral", label: "不适用" }
};

function scoreLevel(score: number): { label: string; tone: Tone; badge: "success" | "warning" | "danger" } {
  if (score >= 80) return { label: "防护良好", tone: "success", badge: "success" };
  if (score >= 50) return { label: "防护一般", tone: "warning", badge: "warning" };
  return { label: "防护薄弱", tone: "danger", badge: "danger" };
}

function ScoreCard({ security, onResetPassword }: { security?: AdminAppUserSecurity; onResetPassword: () => void }) {
  const checks = useMemo(() => buildChecks(security), [security]);
  const score = checks.reduce((sum, item) => sum + item.score, 0);
  const level = scoreLevel(score);

  return (
    <Section
      title="安全评分"
      description="按密码强度、二次验证、Passkey、恢复码与第三方绑定推导，仅供参考"
      icon={ShieldHalf}
      tone={level.tone}
      actions={
        <Button size="sm" variant="outline" onClick={onResetPassword}>
          <KeyRound />
          重置密码
        </Button>
      }
    >
      <div className="flex flex-col gap-5 lg:flex-row lg:items-center">
        <div className="flex shrink-0 flex-col gap-2 lg:w-52">
          <div className="flex items-baseline gap-2">
            <span className={cn("text-4xl font-semibold tracking-tight tabular-nums", TONE_TEXT[level.tone])}>{score}</span>
            <span className="text-sm text-muted-foreground">/ 100</span>
          </div>
          <Progress value={score} className="h-1.5" />
          <Badge variant={level.badge} size="sm">
            {level.label}
          </Badge>
        </div>
        <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-2 xl:grid-cols-5">
          {checks.map((item) => {
            const meta = CHECK_META[item.state];
            const StateIcon = meta.icon;
            return (
              <div key={item.key} className="flex min-w-0 flex-col gap-1.5 rounded-lg border px-3 py-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
                    <item.icon className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className="truncate">{item.label}</span>
                  </span>
                  <StateIcon className={cn("size-4 shrink-0", item.state === "na" ? "text-muted-foreground" : TONE_TEXT[meta.tone])} aria-label={meta.label} />
                </div>
                <span className="truncate text-xs text-muted-foreground" title={item.detail}>
                  {item.detail}
                </span>
                <span className="text-xs text-muted-foreground tabular-nums">
                  {item.score} / {item.weight} 分
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </Section>
  );
}

// ── 登录凭据 ──────────────────────────

function SubHeading({ icon, tone, title, badge }: { icon: ComponentType<{ className?: string }>; tone: Tone; title: string; badge?: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="flex items-center gap-2 text-sm font-medium">
        <ToneIcon icon={icon} tone={tone} size="sm" />
        {title}
      </span>
      {badge}
    </div>
  );
}

function CredentialsSection({ security }: { security?: AdminAppUserSecurity }) {
  const strength = strengthOf(security?.passwordStrengthScore);
  const expired = isPast(security?.passwordExpiresAt);
  const totp = security?.twoFactor;
  const codes = security?.recoveryCodes;
  const codesLeft = numberValue(codes?.remaining);

  return (
    <Section title="登录凭据" description="密码生命周期、二次验证与恢复码" icon={KeyRound} tone="primary">
      <div className="grid gap-6 lg:grid-cols-3 lg:gap-0 lg:divide-x">
        <div className="flex min-w-0 flex-col gap-4 lg:pr-5">
          <SubHeading
            icon={Lock}
            tone="primary"
            title="密码"
            badge={
              <Badge variant={!security?.hasPassword ? "outline" : expired ? "danger" : "success"} size="sm">
                {!security?.hasPassword ? "未设置" : expired ? "已过期" : "已设置"}
              </Badge>
            }
          />
          <div className="flex flex-col gap-1.5">
            <div className="flex items-baseline justify-between text-xs">
              <span className="text-muted-foreground">强度分</span>
              <span className="tabular-nums">
                <span className="text-sm font-medium">{security?.passwordStrengthScore ? security.passwordStrengthScore : "—"}</span>
                <span className={cn("ml-1.5", strength.tone === "neutral" ? "text-muted-foreground" : TONE_TEXT[strength.tone])}>{strength.label}</span>
              </span>
            </div>
            <Progress value={Math.max(0, Math.min(100, numberValue(security?.passwordStrengthScore)))} className="h-1.5" />
          </div>
          <InfoGrid
            columns={1}
            items={[
              {
                label: "最近变更",
                value: security?.passwordChangedAt ? formatDateTime(security.passwordChangedAt) : undefined,
                hint: security?.passwordChangedAt ? formatRelative(security.passwordChangedAt) : undefined
              },
              {
                label: "过期时间",
                value: security?.passwordExpiresAt ? formatDateTime(security.passwordExpiresAt) : "永不过期",
                tone: expired ? "danger" : undefined,
                hint: security?.passwordExpiresAt ? (expired ? "已过期" : formatRelative(security.passwordExpiresAt)) : undefined
              },
              {
                label: "强制改密标记",
                value: security?.passwordChangeRequired ? "已标记，下次登录须修改" : "无",
                tone: security?.passwordChangeRequired ? "warning" : undefined
              }
            ]}
          />
        </div>

        <div className="flex min-w-0 flex-col gap-4 lg:px-5">
          <SubHeading
            icon={ShieldCheck}
            tone="success"
            title="二次验证"
            badge={
              <Badge variant={totp?.pendingSetup ? "warning" : totp?.enabled ? "success" : "outline"} size="sm">
                {totp?.pendingSetup ? "配置未完成" : totp?.enabled ? "已启用" : "未启用"}
              </Badge>
            }
          />
          <InfoGrid
            columns={1}
            items={[
              { label: "方式", value: totp?.enabled ? textValue(security?.twoFactorMethod ?? totp?.method, "TOTP") : undefined },
              { label: "Issuer", value: totp?.issuer },
              { label: "账户名", value: totp?.accountName, mono: true, copy: totp?.accountName },
              { label: "启用时间", value: totp?.enabledAt ? formatDateTime(totp.enabledAt) : undefined },
              {
                label: "最近校验",
                value: totp?.lastVerifiedAt ? formatDateTime(totp.lastVerifiedAt) : undefined,
                hint: totp?.lastVerifiedAt ? formatRelative(totp.lastVerifiedAt) : undefined
              }
            ]}
          />
        </div>

        <div className="flex min-w-0 flex-col gap-4 lg:pl-5">
          <SubHeading
            icon={Ticket}
            tone="warning"
            title="恢复码"
            badge={
              <Badge variant={!codes?.enabled ? "outline" : codesLeft <= 2 ? "warning" : "success"} size="sm">
                {!codes?.enabled ? "未生成" : `${codesLeft} / ${numberValue(codes.total)} 可用`}
              </Badge>
            }
          />
          <InfoGrid
            columns={1}
            items={[
              {
                label: "剩余可用",
                value: codes?.enabled ? `${codesLeft} 个` : undefined,
                tone: codes?.enabled && codesLeft <= 2 ? "warning" : undefined,
                hint: codes?.enabled && codesLeft <= 2 ? "即将用尽，建议提醒用户重新生成" : undefined
              },
              { label: "总数", value: codes?.enabled ? `${numberValue(codes.total)} 个` : undefined },
              { label: "生成时间", value: codes?.generatedAt ? formatDateTime(codes.generatedAt) : undefined }
            ]}
          />
          {codes?.items?.length ? (
            <div className="flex flex-wrap gap-1.5">
              {codes.items.map((item) => (
                <Badge
                  key={item.id}
                  variant={item.usedAt ? "secondary" : "outline"}
                  size="sm"
                  className={cn("font-mono", item.usedAt && "line-through opacity-70")}
                  title={item.usedAt ? `使用于 ${formatDateTime(item.usedAt)}` : "未使用"}
                >
                  {item.codeHint}
                </Badge>
              ))}
            </div>
          ) : null}
        </div>
      </div>
    </Section>
  );
}

// ── Passkey ──────────────────────────

function PasskeySection({ security }: { security?: AdminAppUserSecurity }) {
  const [detail, setDetail] = useState<PasskeyItem | null>(null);
  const items = security?.passkeys?.items ?? [];
  const columns: Array<Column<PasskeyItem>> = [
    {
      id: "name",
      header: "名称",
      cell: (p) => <span className="text-sm font-medium">{textValue(p.credentialName, `凭据 #${p.id}`)}</span>
    },
    {
      id: "credential",
      header: "凭据 ID",
      hideBelow: "md",
      cell: (p) => (
        <span className="block max-w-[220px] truncate font-mono text-xs text-muted-foreground" title={p.credentialId}>
          {p.credentialId}
        </span>
      )
    },
    { id: "count", header: "使用次数", align: "right", cell: (p) => <span className="tabular-nums">{numberValue(p.signCount).toLocaleString("zh-CN")}</span> },
    {
      id: "last",
      header: "最近使用",
      cell: (p) => (
        <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(p.lastUsedAt)}>
          {p.lastUsedAt ? formatRelative(p.lastUsedAt) : "从未使用"}
        </span>
      )
    },
    {
      id: "created",
      header: "注册时间",
      hideBelow: "sm",
      cell: (p) => <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">{formatDateTime(p.createdAt)}</span>
    }
  ];

  return (
    <>
      <Section
        title="Passkey"
        description="该用户注册的通行密钥凭据"
        icon={Fingerprint}
        tone="violet"
        flush
        actions={
          <Badge variant="outline" size="sm">
            {numberValue(security?.passkeys?.count)} 个
          </Badge>
        }
      >
        <RecordTable
          columns={columns}
          rows={items}
          rowKey={(p) => `${p.id}-${p.credentialId}`}
          onRowClick={setDetail}
          minWidth={560}
          empty={<StateBlock compact icon={Fingerprint} title="暂无 Passkey" description="该用户没有注册通行密钥" />}
        />
      </Section>
      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => !open && setDetail(null)}
        title={detail ? textValue(detail.credentialName, `凭据 #${detail.id}`) : ""}
        description="Passkey 凭据"
        fields={
          detail
            ? [
                { label: "凭据 ID", value: detail.credentialId, mono: true, copy: detail.credentialId, wide: true },
                { label: "使用次数", value: numberValue(detail.signCount).toLocaleString("zh-CN") },
                { label: "记录 ID", value: String(detail.id), mono: true },
                { label: "最近使用", value: detail.lastUsedAt ? formatDateTime(detail.lastUsedAt) : "从未使用", hint: detail.lastUsedAt ? formatRelative(detail.lastUsedAt) : undefined },
                { label: "注册时间", value: formatDateTime(detail.createdAt) },
                { label: "更新时间", value: formatDateTime(detail.updatedAt) }
              ]
            : undefined
        }
        raw={detail}
      />
    </>
  );
}

// ── 登录绑定基线 ──────────────────────────

function BaselineSection({ appKey, userId }: { appKey: string; userId: number }) {
  const policyQuery = useAdminAppPolicyQuery(appKey);
  const baselineQuery = useAdminAppLoginBaselineQuery(appKey, userId);
  const resetBaseline = useResetAdminAppLoginBaselineMutation(appKey);
  const [confirming, setConfirming] = useState(false);

  const policy = policyQuery.data;
  const baseline = baselineQuery.data;
  // 三项强绑定全关时不产生任何 Redis I/O，基线也就无从谈起
  const bindingEnabled = Boolean(policy?.loginCheckDevice || policy?.loginCheckIp || policy?.loginCheckUser);

  async function handleReset() {
    try {
      await resetBaseline.mutateAsync(userId);
      toast.success("登录绑定已重置");
      setConfirming(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "重置失败");
    }
  }

  let body: ReactNode;
  if (policyQuery.isLoading || baselineQuery.isLoading) {
    body = (
      <div className="grid gap-4 sm:grid-cols-2">
        {Array.from({ length: 4 }, (_, i) => (
          <div key={i} className="flex flex-col gap-1.5">
            <Skeleton className="h-3 w-16" />
            <Skeleton className="h-4 w-32" />
          </div>
        ))}
      </div>
    );
  } else if (policyQuery.isError || baselineQuery.isError) {
    body = (
      <StateBlock
        compact
        kind="error"
        description={(policyQuery.error ?? baselineQuery.error) instanceof Error ? (policyQuery.error ?? baselineQuery.error)?.message : undefined}
        onRetry={() => {
          void policyQuery.refetch();
          void baselineQuery.refetch();
        }}
      />
    );
  } else if (!bindingEnabled) {
    body = (
      <StateBlock
        compact
        icon={Unlock}
        title="未开启强绑定策略"
        action={
          <Button asChild size="sm" variant="outline">
            <Link href={`/apps/${encodeURIComponent(appKey)}?tab=policy`}>前往应用认证与会话</Link>
          </Button>
        }
      />
    );
  } else if (!baseline?.bound) {
    body = <StateBlock compact icon={CircleAlert} title="尚未建立基线" description="用户下一次成功登录时会记录设备与网络基线" />;
  } else {
    body = (
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap gap-1.5">
          {policy?.loginCheckDevice ? (
            <Badge variant="info" size="sm">
              设备绑定
            </Badge>
          ) : null}
          {policy?.loginCheckIp ? (
            <Badge variant="info" size="sm">
              IP 网段
            </Badge>
          ) : null}
          {policy?.loginCheckUser ? (
            <Badge variant="info" size="sm">
              登录属地
            </Badge>
          ) : null}
        </div>
        <InfoGrid
          items={[
            { label: "设备标识", value: baseline.baseline?.deviceId, mono: true, copy: baseline.baseline?.deviceId, wide: true },
            { label: "IP", value: baseline.baseline?.ip, mono: true, copy: baseline.baseline?.ip },
            { label: "属地", value: baseline.baseline?.region },
            { label: "设备绑定时间", value: baseline.baseline?.deviceBoundAt ? formatDateTime(baseline.baseline.deviceBoundAt) : undefined },
            { label: "基线更新时间", value: baseline.baseline?.updatedAt ? formatDateTime(baseline.baseline.updatedAt) : undefined }
          ]}
        />
      </div>
    );
  }

  return (
    <>
      <Section
        title="登录绑定基线"
        description="强绑定策略下，登录须与此基线一致"
        icon={MonitorSmartphone}
        tone="info"
        actions={
          baseline?.bound && bindingEnabled ? (
            <Button size="sm" variant="outline" disabled={resetBaseline.isPending} onClick={() => setConfirming(true)}>
              <RotateCcw />
              重置绑定
            </Button>
          ) : null
        }
      >
        {body}
      </Section>
      <AlertDialog open={confirming} onOpenChange={(open) => !resetBaseline.isPending && setConfirming(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重置登录绑定？</AlertDialogTitle>
            <AlertDialogDescription>清除当前的设备、IP 与属地基线，用户下一次成功登录时重新建立。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={resetBaseline.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={resetBaseline.isPending}
              onClick={(event) => {
                event.preventDefault();
                void handleReset();
              }}
            >
              {resetBaseline.isPending ? <Loader2 className="animate-spin" /> : null}
              确认重置
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

// ── 认证模块运行态 ──────────────────────────

function ModulesSection({ security }: { security?: AdminAppUserSecurity }) {
  const modules = security?.modules ?? [];
  return (
    <Section title="认证模块运行态" description="服务端各认证模块是否启用、是否就绪" icon={BadgeCheck} tone="neutral">
      {modules.length ? (
        <div className="grid gap-2 sm:grid-cols-2">
          {modules.map((module) => {
            const healthy = module.enabled && module.ready;
            return (
              <div key={module.key} className="flex min-w-0 flex-col gap-1 rounded-lg border px-3 py-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-sm font-medium">{module.name}</span>
                  <Badge variant={healthy ? "success" : module.enabled ? "warning" : "outline"} size="sm">
                    {healthy ? "就绪" : module.enabled ? "未就绪" : "未启用"}
                  </Badge>
                </div>
                <span className="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                  <span className="font-mono">{module.key}</span>
                  {module.hotReload ? <span>支持热更新</span> : null}
                </span>
                {module.message ? <span className="text-xs leading-5 text-muted-foreground">{module.message}</span> : null}
              </div>
            );
          })}
        </div>
      ) : (
        <StateBlock compact icon={BadgeCheck} title="暂无模块信息" />
      )}
    </Section>
  );
}

// ── 页签 ──────────────────────────

export function UserSecurityTab({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const security = user.security;
  const [showResetPassword, setShowResetPassword] = useState(false);

  return (
    <>
      <div className="flex flex-col gap-5">
        {!security ? (
          <Section title="安全信息" icon={ShieldHalf}>
            <StateBlock compact title="暂无安全信息" description="接口未返回该用户的安全字段" />
          </Section>
        ) : null}

        <ScoreCard security={security} onResetPassword={() => setShowResetPassword(true)} />

        <CredentialsSection security={security} />

        <div className="grid gap-5 xl:grid-cols-2">
          <PasskeySection security={security} />
          <BaselineSection appKey={appKey} userId={userId} />
        </div>

        <UserOAuthBindingsPanel appKey={appKey} userId={userId} />

        <UserOAuth2GrantsPanel appKey={appKey} userId={userId} />

        <UserSessionsPanel appKey={appKey} userId={userId} />

        {security?.modules?.length ? <ModulesSection security={security} /> : null}
      </div>

      <UserResetPasswordDialog open={showResetPassword} onOpenChange={setShowResetPassword} appKey={appKey} userId={userId} />
    </>
  );
}
