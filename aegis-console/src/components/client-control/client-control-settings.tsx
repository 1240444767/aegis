"use client";

import { useState } from "react";
import { toast } from "sonner";
import { BellRing, Loader2, Radio, Save, ServerCog, ShieldCheck } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { ClientControlSettings } from "@/lib/api/client-control";
import { useRefreshClientControlMutation, useUpdateClientControlSettingsMutation } from "@/lib/client-control-hooks";
import { formatDateTime } from "@/lib/datetime";
import { FieldGroup, NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { joinList, parseList } from "./client-control-shared";

type Form = {
  enabled: boolean;
  allowedPackages: string;
  allowedCerts: string;
  verdictTtlHours: string;
  recheckMinutes: string;
  offlinePolicy: "allow" | "block";
  watchEnabled: boolean;
  serverGate: boolean;
  blockTitle: string;
  blockMessage: string;
};

function formFrom(settings: ClientControlSettings): Form {
  return {
    enabled: settings.enabled,
    allowedPackages: joinList(settings.allowedPackages),
    allowedCerts: joinList(settings.allowedCerts),
    verdictTtlHours: String(Math.round(settings.verdictTtlSeconds / 3600) || 1),
    recheckMinutes: String(Math.round(settings.recheckSeconds / 60) || 1),
    offlinePolicy: settings.offlinePolicy,
    watchEnabled: settings.watchEnabled,
    serverGate: settings.serverGate,
    blockTitle: settings.blockTitle,
    blockMessage: settings.blockMessage
  };
}

/**
 * 管控设置。由父组件以策略版本作 key 重建，保存后表单自动回到服务端的值。
 */
export function ClientControlSettingsPanel({ appKey, settings }: { appKey: string; settings: ClientControlSettings }) {
  const update = useUpdateClientControlSettingsMutation(appKey);
  const refresh = useRefreshClientControlMutation(appKey);
  const [form, setForm] = useState<Form>(() => formFrom(settings));
  const set = <K extends keyof Form>(key: K, value: Form[K]) => setForm((state) => ({ ...state, [key]: value }));

  async function save() {
    try {
      await update.mutateAsync({
        enabled: form.enabled,
        allowedPackages: parseList(form.allowedPackages),
        allowedCerts: parseList(form.allowedCerts),
        verdictTtlSeconds: Math.round(Number(form.verdictTtlHours) * 3600),
        recheckSeconds: Math.round(Number(form.recheckMinutes) * 60),
        offlinePolicy: form.offlinePolicy,
        watchEnabled: form.watchEnabled,
        serverGate: form.serverGate,
        blockTitle: form.blockTitle.trim(),
        blockMessage: form.blockMessage.trim()
      });
      toast.success("已保存，客户端将重新校验");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function notifyClients() {
    try {
      const result = await refresh.mutateAsync();
      toast.success(`已通知客户端重新校验，策略版本 ${result.policyVersion}`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  return (
    <div className="max-w-3xl space-y-6">
      <FieldGroup label="开关">
        <div className="grid gap-2 sm:grid-cols-2">
          <SwitchRow
            label="启用版本管控"
            hint="关闭时所有客户端都拿到放行裁决，此前停用的设备随之恢复"
            icon={<ShieldCheck className="size-4" />}
            checked={form.enabled}
            onChange={(value) => set("enabled", value)}
          />
          <SwitchRow
            label="服务端拦截"
            hint="被停用或要求强制更新的客户端调用其他接口时一律拒绝"
            icon={<ServerCog className="size-4" />}
            checked={form.serverGate}
            onChange={(value) => set("serverGate", value)}
          />
          <SwitchRow
            label="即时通知"
            hint="客户端在前台时用长轮询得知规则变更，不需要登录"
            icon={<Radio className="size-4" />}
            checked={form.watchEnabled}
            onChange={(value) => set("watchEnabled", value)}
          />
        </div>
      </FieldGroup>

      <FieldGroup label="官方身份" hint="没有规则命中时检查；留空表示不检查">
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">官方包名</Label>
            <Textarea
              rows={4}
              className="text-sm"
              value={form.allowedPackages}
              placeholder="一行一个，不在其中的包名一律停用"
              onChange={(e) => set("allowedPackages", e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">官方签名证书 SHA-256</Label>
            <Textarea
              rows={4}
              className="font-mono text-xs"
              value={form.allowedCerts}
              placeholder="一行一个，可带冒号；签名不符的安装包一律停用"
              onChange={(e) => set("allowedCerts", e.target.value)}
            />
          </div>
        </div>
      </FieldGroup>

      <FieldGroup label="校验节奏">
        <div className="grid gap-3 sm:grid-cols-3">
          <NumberField
            label="前台复查间隔"
            unit="分钟"
            min={1}
            max={1440}
            value={form.recheckMinutes}
            onChange={(value) => set("recheckMinutes", value)}
          />
          <NumberField
            label="放行裁决离线有效期"
            unit="小时"
            min={1}
            max={720}
            value={form.verdictTtlHours}
            onChange={(value) => set("verdictTtlHours", value)}
          />
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">离线且裁决过期时</Label>
            <Select value={form.offlinePolicy} onValueChange={(value) => set("offlinePolicy", value as Form["offlinePolicy"])}>
              <SelectTrigger className="text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="allow">继续放行</SelectItem>
                <SelectItem value="block">暂停使用</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </FieldGroup>

      <FieldGroup label="默认停用文案" hint="规则没有填写标题与说明时使用">
        <div className="space-y-3">
          <Input value={form.blockTitle} placeholder="当前版本已停用" onChange={(e) => set("blockTitle", e.target.value)} />
          <Textarea
            rows={3}
            value={form.blockMessage}
            placeholder="当前版本已停止服务，请安装官方最新版本后继续使用。"
            onChange={(e) => set("blockMessage", e.target.value)}
          />
        </div>
      </FieldGroup>

      <div className="flex flex-wrap items-center gap-2 border-t pt-4">
        <Button onClick={save} disabled={update.isPending}>
          {update.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
          保存
        </Button>
        <Button variant="outline" onClick={notifyClients} disabled={refresh.isPending}>
          <BellRing className="size-3.5" />
          通知客户端重新校验
        </Button>
        <span className="ml-auto text-xs text-muted-foreground">
          策略版本 {settings.policyVersion}
          {settings.updatedBy ? `，${settings.updatedBy} 更新于 ${formatDateTime(settings.updatedAt)}` : ""}
        </span>
      </div>
    </div>
  );
}
