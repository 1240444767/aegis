"use client";

import { useState } from "react";
import { AlertTriangle, Globe, Link2, Plus, Save, ScanFace, Smartphone, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  FieldGroup,
  NoAppSelected,
  SectionCard,
  StatusDot,
  SwitchRow
} from "@/components/apps/app-config-primitives";
import { CopyButton } from "@/components/apps/auth-protocol/auth-protocol-shared";
import { publicAssetLinksPath, type AppPasskeyPolicy } from "@/lib/api/passkey";
import { appConfig } from "@/lib/env";
import { useAppPasskeyPolicyQuery, useSaveAppPasskeyPolicyMutation } from "@/lib/passkey-hooks";

/**
 * 应用级通行密钥依赖方：RP ID、网页来源、安卓包与签名证书。
 *
 * 编辑态里的来源与指纹用多行文本（一行一个），提交时拆成数组；校验与规范化
 * （域名、https、指纹长度、来源必须落在 RP ID 下）全部在后端，报错原样提示。
 */

type AndroidDraft = { packageName: string; fingerprints: string };

type PasskeyForm = {
  enabled: boolean;
  rpId: string;
  rpName: string;
  origins: string;
  android: AndroidDraft[];
};

function seed(policy?: AppPasskeyPolicy | null): PasskeyForm {
  return {
    enabled: policy?.enabled ?? true,
    rpId: policy?.rpId ?? "",
    rpName: policy?.rpName ?? "",
    origins: (policy?.origins ?? []).join("\n"),
    android: (policy?.android ?? []).map((item) => ({
      packageName: item.packageName,
      fingerprints: item.sha256CertFingerprints.join("\n")
    }))
  };
}

const lines = (value: string) =>
  value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);

export function AppPasskeyPanel({ appKey }: { appKey?: string | null }) {
  const query = useAppPasskeyPolicyQuery(appKey);
  const mutation = useSaveAppPasskeyPolicyMutation(appKey);

  // 草稿按 appKey 绑定作用域，与其他配置面板同一做法：切换应用时自动丢弃上一个应用的未保存改动
  const [draft, setDraft] = useState<{ scope: string; value: PasskeyForm } | null>(null);
  const scope = appKey ?? "";
  const form = draft?.scope === scope ? draft.value : seed(query.data?.policy);
  const patch = (value: Partial<PasskeyForm>) => setDraft({ scope, value: { ...form, ...value } });
  const patchAndroid = (index: number, value: Partial<AndroidDraft>) =>
    patch({ android: form.android.map((item, i) => (i === index ? { ...item, ...value } : item)) });

  async function handleSave() {
    try {
      await mutation.mutateAsync({
        enabled: form.enabled,
        rpId: form.rpId.trim(),
        rpName: form.rpName.trim(),
        origins: lines(form.origins),
        android: form.android
          .filter((item) => item.packageName.trim() || item.fingerprints.trim())
          .map((item) => ({ packageName: item.packageName.trim(), sha256CertFingerprints: lines(item.fingerprints) }))
      });
      setDraft(null);
      toast.success("已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  if (!appKey) return <NoAppSelected icon={<ScanFace className="size-8" />} />;
  if (query.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-64 w-full rounded-2xl" />
        <Skeleton className="h-48 w-full rounded-2xl" />
      </div>
    );
  }

  const view = query.data;
  const policy = view?.policy;
  const configured = Boolean(policy && !policy.isDefault && policy.rpId);
  const assetLinksJson = JSON.stringify(view?.assetLinks ?? [], null, 2);
  const proxySource = `${appConfig.apiBaseUrl || (typeof window !== "undefined" ? window.location.origin : "")}${publicAssetLinksPath(appKey)}`;

  return (
    <div className="space-y-6">
      {view && !view.moduleReady ? (
        <div className="flex items-start gap-2 rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-xs text-amber-700 dark:text-amber-300">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          平台 Passkey 模块未启用或不可用，这里的配置暂不生效。请先在「平台配置 · 系统安全」开启 Passkey。
        </div>
      ) : null}

      <SectionCard
        icon={<ScanFace className="size-4" />}
        title="依赖方"
        description="App 与网站共用的通行密钥域名。凭据绑定在 RP ID 上，改动后已创建的通行密钥将无法使用。"
        aside={
          <StatusDot
            active={configured && form.enabled}
            labelActive="独立依赖方已生效"
            labelInactive={policy?.enabled === false ? "已关闭" : "沿用平台配置"}
          />
        }
      >
        <div className="space-y-5">
          <div className="grid gap-2.5 sm:grid-cols-2">
            <SwitchRow
              label="允许使用通行密钥"
              hint="关闭后本应用的通行密钥注册与登录全部拒绝"
              checked={form.enabled}
              onChange={(enabled) => patch({ enabled })}
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">RP ID</Label>
              <Input
                value={form.rpId}
                placeholder="voyage.example.com"
                onChange={(e) => patch({ rpId: e.target.value })}
                className="font-mono text-xs"
              />
              <p className="text-[10px] leading-snug text-muted-foreground">
                网站域名，不带协议与端口；须在该域名下发布 assetlinks.json。留空则沿用平台配置（仅浏览器可用）。
              </p>
            </div>
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">显示名称</Label>
              <Input
                value={form.rpName}
                placeholder="留空使用应用名"
                onChange={(e) => patch({ rpName: e.target.value })}
                className="text-xs"
              />
              <p className="text-[10px] leading-snug text-muted-foreground">系统创建通行密钥时展示给用户的名称。</p>
            </div>
          </div>
          <FieldGroup label="网页来源" hint="一行一个，须为 RP ID 或其子域">
            <Textarea
              value={form.origins}
              placeholder="https://voyage.example.com"
              rows={3}
              onChange={(e) => patch({ origins: e.target.value })}
              className="font-mono text-xs"
            />
          </FieldGroup>
        </div>
      </SectionCard>

      <SectionCard
        icon={<Smartphone className="size-4" />}
        title="安卓应用"
        description="可以使用本应用通行密钥的安卓包。证书指纹即 keytool 输出的 SHA-256，签名轮换时新旧证书都要登记。"
      >
        <div className="space-y-3">
          {form.android.length === 0 ? (
            <p className="rounded-xl bg-muted px-3 py-4 text-center text-xs text-muted-foreground">尚未登记安卓应用</p>
          ) : (
            form.android.map((item, index) => (
              <div key={index} className="grid gap-3 rounded-xl bg-muted p-3 sm:grid-cols-[minmax(0,260px)_1fr_auto]">
                <div className="space-y-1.5">
                  <Label className="text-[11px] font-medium">包名</Label>
                  <Input
                    value={item.packageName}
                    placeholder="com.example.app"
                    onChange={(e) => patchAndroid(index, { packageName: e.target.value })}
                    className="bg-background font-mono text-xs"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-[11px] font-medium">签名证书 SHA-256（一行一个）</Label>
                  <Textarea
                    value={item.fingerprints}
                    rows={2}
                    placeholder="AB:CD:…"
                    onChange={(e) => patchAndroid(index, { fingerprints: e.target.value })}
                    className="bg-background font-mono text-[11px]"
                  />
                </div>
                <Button
                  size="icon"
                  variant="ghost"
                  className="self-end"
                  aria-label="移除"
                  onClick={() => patch({ android: form.android.filter((_, i) => i !== index) })}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            ))
          )}
          <div className="flex flex-wrap justify-between gap-2 pt-1">
            <Button
              size="sm"
              variant="outline"
              onClick={() => patch({ android: [...form.android, { packageName: "", fingerprints: "" }] })}
            >
              <Plus className="size-3.5" />
              添加安卓应用
            </Button>
            <Button size="sm" disabled={mutation.isPending} onClick={handleSave}>
              <Save className="size-3.5" />
              {mutation.isPending ? "保存中..." : "保存配置"}
            </Button>
          </div>
        </div>
      </SectionCard>

      {configured && view ? (
        <SectionCard
          icon={<Link2 className="size-4" />}
          title="域名关联"
          description="Credential Manager 会抓取 RP ID 域名下的 assetlinks.json 核对包名与证书，缺失时 App 报 SecurityError。"
        >
          <div className="space-y-5">
            <FieldGroup label="发布地址">
              <div className="flex items-center justify-between gap-2 rounded-xl bg-muted px-3 py-2">
                <code className="truncate font-mono text-xs">{view.assetLinksUrl}</code>
                <CopyButton value={view.assetLinksUrl ?? ""} />
              </div>
            </FieldGroup>
            <FieldGroup label="反代来源" hint="网站把发布地址反代到这里，指纹只需在本页维护">
              <div className="flex items-center justify-between gap-2 rounded-xl bg-muted px-3 py-2">
                <code className="truncate font-mono text-xs">{proxySource}</code>
                <CopyButton value={proxySource} />
              </div>
            </FieldGroup>
            <FieldGroup label="文件内容">
              <div className="relative">
                <pre className="max-h-72 overflow-auto rounded-xl bg-muted p-3 font-mono text-[11px] leading-relaxed">
                  {assetLinksJson}
                </pre>
                <CopyButton value={assetLinksJson} className="absolute right-2 top-2" />
              </div>
            </FieldGroup>
            <FieldGroup label="校验来源" hint="clientDataJSON.origin 必须是其中之一">
              <ul className="space-y-1.5">
                {view.allowedOrigins.map((origin) => (
                  <li key={origin} className="flex items-center gap-2 rounded-lg bg-muted px-3 py-1.5">
                    {origin.startsWith("android:") ? (
                      <Smartphone className="size-3.5 shrink-0 text-muted-foreground" />
                    ) : (
                      <Globe className="size-3.5 shrink-0 text-muted-foreground" />
                    )}
                    <code className="truncate font-mono text-[11px]">{origin}</code>
                  </li>
                ))}
              </ul>
            </FieldGroup>
          </div>
        </SectionCard>
      ) : null}
    </div>
  );
}
