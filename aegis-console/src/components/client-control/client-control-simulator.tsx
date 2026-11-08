"use client";

import { useState } from "react";
import { toast } from "sonner";
import { CheckCircle2, CircleDashed, FlaskConical } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { ClientControlDecision } from "@/lib/api/client-control";
import { useSimulateClientControlMutation } from "@/lib/client-control-hooks";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { ActionBadge, CONTROL_PLATFORMS, DATA_FONT, reasonLabel } from "./client-control-shared";

const FIELDS: Array<{ key: keyof Omit<SimForm, "platform">; label: string; placeholder?: string; numeric?: boolean; mono?: boolean }> = [
  { key: "packageName", label: "包名", placeholder: "com.example.app" },
  { key: "versionName", label: "版本名", placeholder: "1.2.0" },
  { key: "versionCode", label: "版本号", placeholder: "120", numeric: true },
  { key: "certSha256", label: "签名证书 SHA-256", placeholder: "64 位十六进制，可带冒号", mono: true },
  { key: "installer", label: "安装来源", placeholder: "com.android.vending" },
  { key: "osVersion", label: "系统 API 级别", placeholder: "35", numeric: true },
  { key: "deviceId", label: "设备 ID", mono: true },
  { key: "userId", label: "用户 ID", placeholder: "未登录留空", numeric: true }
];

type SimForm = {
  packageName: string;
  versionName: string;
  versionCode: string;
  platform: string;
  certSha256: string;
  installer: string;
  osVersion: string;
  deviceId: string;
  userId: string;
};

/** 模拟判定：以给定的客户端身份按当前规则跑一遍，逐条列出每条规则为什么命中或没命中。 */
export function ClientControlSimulator({ appKey }: { appKey: string }) {
  const simulate = useSimulateClientControlMutation(appKey);
  const [form, setForm] = useState<SimForm>({
    packageName: "",
    versionName: "",
    versionCode: "",
    platform: "android",
    certSha256: "",
    installer: "",
    osVersion: "",
    deviceId: "",
    userId: ""
  });
  const [result, setResult] = useState<ClientControlDecision | null>(null);

  async function run() {
    try {
      setResult(
        await simulate.mutateAsync({
          packageName: form.packageName.trim(),
          versionName: form.versionName.trim(),
          versionCode: Number(form.versionCode) || 0,
          platform: form.platform,
          certSha256: form.certSha256.trim(),
          installer: form.installer.trim(),
          deviceId: form.deviceId.trim(),
          osVersion: Number(form.osVersion) || 0,
          userId: Number(form.userId) || 0
        })
      );
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "模拟失败");
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[22rem_1fr]">
      <div className="space-y-3 rounded-xl border bg-card px-4 py-4">
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">平台</Label>
          <Select value={form.platform} onValueChange={(value) => setForm((state) => ({ ...state, platform: value }))}>
            <SelectTrigger className="h-8 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CONTROL_PLATFORMS.map((platform) => (
                <SelectItem key={platform.value} value={platform.value}>
                  {platform.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {FIELDS.map((field) => (
          <div key={field.key} className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">{field.label}</Label>
            <Input
              className={cn("h-8 text-sm", field.mono && "font-mono text-xs")}
              inputMode={field.numeric ? "numeric" : undefined}
              value={form[field.key]}
              placeholder={field.placeholder}
              onChange={(event) => {
                const value = field.numeric ? event.target.value.replace(/\D/g, "") : event.target.value;
                setForm((state) => ({ ...state, [field.key]: value }));
              }}
            />
          </div>
        ))}
        <Button className="w-full" onClick={run} disabled={simulate.isPending || !form.packageName.trim()}>
          <FlaskConical className="size-3.5" />
          模拟判定
        </Button>
      </div>

      <div className="rounded-xl border bg-card">
        {!result ? (
          <div className="flex h-full min-h-48 items-center justify-center px-4 text-sm text-muted-foreground">
            填写客户端身份后查看判定过程
          </div>
        ) : (
          <div className="space-y-4 px-4 py-4">
            <div className="flex flex-wrap items-center gap-2">
              <ActionBadge value={result.action} />
              <span className="text-sm text-muted-foreground">{result.ruleName || reasonLabel(result.reason)}</span>
            </div>
            {result.title || result.message ? (
              <div className="rounded-lg bg-muted/50 px-3 py-2 text-sm">
                {result.title ? <div className="font-medium">{result.title}</div> : null}
                {result.message ? <div className="text-muted-foreground">{result.message}</div> : null}
                {result.actionUrl ? <div className={cn("mt-1 break-all text-xs text-muted-foreground", DATA_FONT)}>{result.actionUrl}</div> : null}
              </div>
            ) : null}
            <ol className="space-y-2">
              {(result.trace ?? []).map((step, index) => (
                <li key={index} className="flex items-start gap-2 text-sm">
                  {step.matched ? (
                    <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
                  ) : (
                    <CircleDashed className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  )}
                  <div className="min-w-0">
                    <span className={cn(step.matched ? "font-medium" : "text-muted-foreground")}>{step.name}</span>
                    <span className="ml-2 text-xs text-muted-foreground">{step.detail}</span>
                  </div>
                </li>
              ))}
            </ol>
          </div>
        )}
      </div>
    </div>
  );
}
