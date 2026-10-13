"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Code2,
  ExternalLink,
  LayoutDashboard,
  Loader2,
  PlugZap,
  RotateCcw,
  Save,
  ShieldCheck,
  SlidersHorizontal,
  TriangleAlert
} from "lucide-react";
import { toast } from "sonner";
import { useAdminToken } from "@/lib/admin-hooks";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { buildScenarios } from "@/lib/integration-snippets";
import {
  getAuthProtocol,
  rotateSigningSecret,
  runIntegrationSelfTest,
  updateAuthProtocol,
  type AuthProtocolPolicy,
  type SelfTestResult
} from "@/lib/api/app-auth-protocol";
import { NoAppSelected, SectionCard } from "@/components/apps/app-config-primitives";
import { AuthProtocolOverview } from "@/components/apps/auth-protocol/auth-protocol-overview";
import { AuthProtocolPolicy as PolicyView } from "@/components/apps/auth-protocol/auth-protocol-policy";
import { AuthProtocolSecurity } from "@/components/apps/auth-protocol/auth-protocol-security";
import {
  PLACEHOLDER_POLICY,
  draftOf,
  errorMessage,
  sameDraft,
  type PolicyDraft
} from "@/components/apps/auth-protocol/auth-protocol-shared";
import { CodeSamples } from "@/components/developers/code-samples";
import { AutoSkeleton } from "@/components/ui/auto-skeleton";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * 接入区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 现在是什么配置、往哪发拿什么凭据、链路通没通（读已保存的策略） |
 * | 认证策略 | 登录 / 注册方式、登录标识、行为开关、注册字段 |
 * | 安全与密钥 | 安全等级、应用密钥轮换、加密档传输公钥、旧接口的传输加密 |
 * | 代码示例 | 按已启用的方式生成的接入代码 |
 *
 * 策略与安全等级共用一份草稿：两个视图之间来回切不丢改动，有改动时底部常驻保存条，
 * 离开页面前浏览器提示。视图状态不进 URL，`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: LayoutDashboard, draft: false },
  { key: "policy", label: "认证策略", icon: SlidersHorizontal, draft: true },
  { key: "security", label: "安全与密钥", icon: ShieldCheck, draft: true },
  { key: "samples", label: "代码示例", icon: Code2, draft: false }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppAuthProtocolPanel({ appKey }: { appKey?: string | null }) {
  if (!appKey) return <NoAppSelected icon={<PlugZap className="size-5" />} />;
  // key={appKey}：切换应用时整块重挂载，草稿、自检结果与明文密钥都不串到另一个应用
  return <AuthProtocolWorkspace key={appKey} appKey={appKey} />;
}

function AuthProtocolWorkspace({ appKey }: { appKey: string }) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const origin = useOrigin();
  // 显式配置了直连地址就用它，否则用控制台当前站点（同源反代场景）
  const baseUrl = appConfig.apiBaseUrl || origin || "";

  const policyQuery = useQuery({
    queryKey: ["app-auth-protocol", token, appKey],
    queryFn: () => getAuthProtocol(token as string, appKey),
    enabled: Boolean(token)
  });

  const [view, setView] = useState<ViewKey>("overview");
  const [draft, setDraft] = useState<PolicyDraft | null>(null);
  const [saving, setSaving] = useState(false);
  const [selfTest, setSelfTest] = useState<SelfTestResult | null>(null);
  const [selfTesting, setSelfTesting] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null);

  const loading = policyQuery.isLoading;
  const saved: AuthProtocolPolicy = policyQuery.data ?? PLACEHOLDER_POLICY;
  const form = draft ?? draftOf(saved);
  const dirty = Boolean(draft && policyQuery.data && !sameDraft(draft, draftOf(policyQuery.data)));

  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  const update = (patch: Partial<PolicyDraft>) => setDraft({ ...form, ...patch });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["app-auth-protocol"] });

  async function save() {
    if (!token) return;
    setSaving(true);
    try {
      await updateAuthProtocol(token, appKey, form);
      await refresh();
      setDraft(null);
      toast.success("接入配置已保存");
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    } finally {
      setSaving(false);
    }
  }

  async function runSelfTest() {
    if (!token) return;
    setSelfTesting(true);
    try {
      const outcome = await runIntegrationSelfTest(token, appKey, baseUrl);
      setSelfTest(outcome);
      if (outcome.ok) toast.success("接入链路全部通过");
      else toast.error("自检未通过");
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setSelfTesting(false);
    }
  }

  async function rotateSecret() {
    if (!token) return;
    setRotating(true);
    try {
      const rotated = await rotateSigningSecret(token, appKey);
      setRevealedSecret(rotated.appSecret);
      await refresh();
      toast.success("应用密钥已轮换");
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setRotating(false);
    }
  }

  if (policyQuery.isError) {
    return (
      <SectionCard icon={<TriangleAlert className="size-4" />} title="接入配置加载失败">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-muted-foreground">{errorMessage(policyQuery.error, "请稍后重试")}</p>
          <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => void policyQuery.refetch()}>
            <RotateCcw className="size-3.5" />
            重试
          </Button>
        </div>
      </SectionCard>
    );
  }

  // 示例按已保存的策略生成：没启用的方式不出示例，页面上不摆调了必 403 的代码
  const smsOn = saved.loginMethods.includes("sms") || saved.registerMethods.includes("sms");
  const oauthOn = saved.loginMethods.includes("oauth");
  const scenarios = buildScenarios({ appKey, baseUrl, level: saved.securityLevel }).filter(
    (scenario) => (scenario.id !== "sms" || smsOn) && (scenario.id !== "oauth" || oauthOn)
  );

  return (
    <div className={cn("space-y-4", dirty && "pb-20 md:pb-0")}>
      <div className="-mx-1 overflow-x-auto px-1 [scrollbar-width:none]">
        <div className="inline-flex min-w-max gap-1 rounded-xl border bg-muted/40 p-1">
          {VIEWS.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => setView(item.key)}
              className={cn(
                "flex items-center gap-1.5 whitespace-nowrap rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <item.icon className="size-3.5" />
              {item.label}
              {item.draft && dirty ? (
                <span className="size-1.5 rounded-full bg-amber-500" aria-label="有未保存的修改" />
              ) : null}
            </button>
          ))}
        </div>
      </div>

      <AutoSkeleton loading={loading}>
        {view === "overview" ? (
          <AuthProtocolOverview
            appKey={appKey}
            baseUrl={baseUrl}
            policy={saved}
            selfTest={selfTest}
            selfTesting={selfTesting}
            onRunSelfTest={() => void runSelfTest()}
            onManageSecret={() => setView("security")}
          />
        ) : null}
        {view === "policy" ? <PolicyView appKey={appKey} draft={form} update={update} /> : null}
        {view === "security" ? (
          <AuthProtocolSecurity
            appKey={appKey}
            saved={saved}
            draft={form}
            update={update}
            rotating={rotating}
            revealedSecret={revealedSecret}
            onRotateSecret={() => void rotateSecret()}
            onDismissSecret={() => setRevealedSecret(null)}
          />
        ) : null}
        {view === "samples" ? (
          <SectionCard
            icon={<Code2 className="size-4" />}
            title="代码示例"
            aside={
              <Button asChild size="sm" variant="outline" className="h-8 gap-1.5">
                <Link href="/developers" target="_blank">
                  完整文档
                  <ExternalLink className="size-3.5" />
                </Link>
              </Button>
            }
          >
            <CodeSamples scenarios={scenarios} />
          </SectionCard>
        ) : null}
      </AutoSkeleton>

      {dirty ? (
        <div className="fixed inset-x-0 bottom-0 z-40 border-t bg-background/95 px-4 py-3 shadow-[0_-4px_16px_rgba(0,0,0,0.06)] md:sticky md:inset-x-auto md:bottom-4 md:rounded-2xl md:border md:shadow-lg">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-sm">
              <span className="size-2 rounded-full bg-amber-500" />
              <span className="font-medium">接入配置有未保存的修改</span>
            </div>
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" className="h-8 gap-1.5" disabled={saving} onClick={() => setDraft(null)}>
                <RotateCcw className="size-3.5" />
                放弃修改
              </Button>
              <Button size="sm" className="h-8 gap-1.5" disabled={saving} onClick={() => void save()}>
                {saving ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                保存并生效
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
