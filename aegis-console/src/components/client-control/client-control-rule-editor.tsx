"use client";

import { useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { ClientControlAction, ClientControlRule, ClientControlRuleInput } from "@/lib/api/client-control";
import { useSaveClientControlRuleMutation } from "@/lib/client-control-hooks";
import { FieldGroup, SwitchRow } from "@/components/apps/app-config-primitives";
import { fromLocalInput, toLocalInput } from "@/components/content/content-shared";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { CONTROL_ACTIONS, CONTROL_PLATFORMS, joinList, parseList } from "./client-control-shared";

type Draft = {
  name: string;
  enabled: boolean;
  action: ClientControlAction;
  packages: string;
  packagesNegate: boolean;
  versionCodeMin: string;
  versionCodeMax: string;
  versionNames: string;
  certs: string;
  certsNegate: boolean;
  platforms: string[];
  deviceIds: string;
  userIds: string;
  installers: string;
  installersNegate: boolean;
  osVersionMin: string;
  osVersionMax: string;
  title: string;
  message: string;
  actionUrl: string;
  actionLabel: string;
  startsAt: string;
  endsAt: string;
  note: string;
};

function draftFrom(rule?: ClientControlRule | null): Draft {
  const c = rule?.conditions ?? {};
  const num = (value?: number) => (value ? String(value) : "");
  return {
    name: rule?.name ?? "",
    enabled: rule?.enabled ?? true,
    action: rule?.action ?? "block",
    packages: joinList(c.packages),
    packagesNegate: Boolean(c.packagesNegate),
    versionCodeMin: num(c.versionCodeMin),
    versionCodeMax: num(c.versionCodeMax),
    versionNames: joinList(c.versionNames),
    certs: joinList(c.certs),
    certsNegate: Boolean(c.certsNegate),
    platforms: c.platforms ?? [],
    deviceIds: joinList(c.deviceIds),
    userIds: joinList(c.userIds),
    installers: joinList(c.installers),
    installersNegate: Boolean(c.installersNegate),
    osVersionMin: num(c.osVersionMin),
    osVersionMax: num(c.osVersionMax),
    title: rule?.title ?? "",
    message: rule?.message ?? "",
    actionUrl: rule?.actionUrl ?? "",
    actionLabel: rule?.actionLabel ?? "",
    startsAt: toLocalInput(rule?.startsAt),
    endsAt: toLocalInput(rule?.endsAt),
    note: rule?.note ?? ""
  };
}

function payloadFrom(draft: Draft, priority: number): ClientControlRuleInput {
  const int = (value: string) => Math.max(0, Math.trunc(Number(value) || 0));
  return {
    name: draft.name.trim(),
    enabled: draft.enabled,
    priority,
    action: draft.action,
    conditions: {
      packages: parseList(draft.packages),
      packagesNegate: draft.packagesNegate,
      versionCodeMin: int(draft.versionCodeMin),
      versionCodeMax: int(draft.versionCodeMax),
      versionNames: parseList(draft.versionNames),
      certs: parseList(draft.certs),
      certsNegate: draft.certsNegate,
      platforms: draft.platforms,
      deviceIds: parseList(draft.deviceIds),
      userIds: parseList(draft.userIds)
        .map((value) => Number(value))
        .filter((value) => Number.isInteger(value) && value > 0),
      installers: parseList(draft.installers),
      installersNegate: draft.installersNegate,
      osVersionMin: int(draft.osVersionMin),
      osVersionMax: int(draft.osVersionMax)
    },
    title: draft.title.trim(),
    message: draft.message.trim(),
    actionUrl: draft.actionUrl.trim(),
    actionLabel: draft.actionLabel.trim(),
    startsAt: fromLocalInput(draft.startsAt),
    endsAt: fromLocalInput(draft.endsAt),
    note: draft.note.trim()
  };
}

/**
 * 规则编辑器。条件之间是「且」，同一条件内的多个取值是「或」，留空的条件不参与判定。
 * 新建的规则排在最前（优先级最高），顺序在列表里调整。
 */
export function ClientControlRuleEditor({
  appKey,
  rule,
  topPriority,
  open,
  onOpenChange
}: {
  appKey: string;
  rule?: ClientControlRule | null;
  topPriority: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const save = useSaveClientControlRuleMutation(appKey);
  const [draft, setDraft] = useState<Draft>(() => draftFrom(rule));
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((state) => ({ ...state, [key]: value }));
  const blocking = draft.action === "block" || draft.action === "force_update";

  async function submit() {
    if (!draft.name.trim()) {
      toast.error("请填写规则名称");
      return;
    }
    try {
      await save.mutateAsync({ ruleId: rule?.id, payload: payloadFrom(draft, rule ? rule.priority : topPriority + 10) });
      toast.success(rule ? "规则已保存" : "规则已创建");
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[96vw] flex-col gap-0 p-0 sm:max-w-2xl" onOpenAutoFocus={(e) => e.preventDefault()}>
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{rule ? `编辑规则` : "新建规则"}</SheetTitle>
          <SheetDescription>条件之间同时满足才命中，同一条件内任一取值即可；留空的条件不参与判定。</SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-6 py-5">
          <FieldGroup label="基本">
            <div className="space-y-3">
              <Input value={draft.name} placeholder="规则名称，例如：停用 1.2 以下版本" onChange={(e) => set("name", e.target.value)} />
              <div className="grid grid-cols-2 gap-2">
                {CONTROL_ACTIONS.map((action) => (
                  <button
                    key={action.value}
                    type="button"
                    onClick={() => set("action", action.value)}
                    className={cn(
                      "rounded-lg border px-3 py-2 text-left transition-colors",
                      draft.action === action.value ? "border-primary bg-primary/5" : "hover:bg-muted/50"
                    )}
                  >
                    <div className="text-sm font-medium">{action.label}</div>
                    <div className="text-xs text-muted-foreground">{action.hint}</div>
                  </button>
                ))}
              </div>
              <SwitchRow label="启用" checked={draft.enabled} onChange={(value) => set("enabled", value)} />
            </div>
          </FieldGroup>

          <FieldGroup label="包名与签名">
            <div className="space-y-3">
              <ListField
                label="包名"
                value={draft.packages}
                onChange={(value) => set("packages", value)}
                placeholder="com.example.app"
                negate={draft.packagesNegate}
                onNegate={(value) => set("packagesNegate", value)}
              />
              <ListField
                label="签名证书 SHA-256"
                value={draft.certs}
                onChange={(value) => set("certs", value)}
                placeholder="64 位十六进制，可带冒号"
                negate={draft.certsNegate}
                onNegate={(value) => set("certsNegate", value)}
                negateHint="选中后命中不在清单内的签名，用于停用非官方签名的安装包"
                mono
              />
              <ListField
                label="安装来源"
                value={draft.installers}
                onChange={(value) => set("installers", value)}
                placeholder="com.android.vending"
                negate={draft.installersNegate}
                onNegate={(value) => set("installersNegate", value)}
              />
            </div>
          </FieldGroup>

          <FieldGroup label="版本" hint="版本号为闭区间，0 或留空表示不限">
            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-3">
                <RangeInput label="版本号不低于" value={draft.versionCodeMin} onChange={(value) => set("versionCodeMin", value)} />
                <RangeInput label="版本号不高于" value={draft.versionCodeMax} onChange={(value) => set("versionCodeMax", value)} />
              </div>
              <ListField label="版本名" value={draft.versionNames} onChange={(value) => set("versionNames", value)} placeholder="1.2.0" />
            </div>
          </FieldGroup>

          <FieldGroup label="平台与设备">
            <div className="space-y-3">
              <div className="flex flex-wrap gap-x-4 gap-y-2">
                {CONTROL_PLATFORMS.map((platform) => (
                  <label key={platform.value} className="flex items-center gap-1.5 text-sm">
                    <Checkbox
                      checked={draft.platforms.includes(platform.value)}
                      onCheckedChange={(checked) =>
                        set(
                          "platforms",
                          checked ? [...draft.platforms, platform.value] : draft.platforms.filter((item) => item !== platform.value)
                        )
                      }
                    />
                    {platform.label}
                  </label>
                ))}
              </div>
              <div className="grid grid-cols-2 gap-3">
                <RangeInput label="系统 API 级别不低于" value={draft.osVersionMin} onChange={(value) => set("osVersionMin", value)} />
                <RangeInput label="系统 API 级别不高于" value={draft.osVersionMax} onChange={(value) => set("osVersionMax", value)} />
              </div>
              <ListField label="设备 ID" value={draft.deviceIds} onChange={(value) => set("deviceIds", value)} mono />
              <ListField
                label="用户 ID"
                value={draft.userIds}
                onChange={(value) => set("userIds", value)}
                negateHint="未登录的客户端不会命中用户条件"
                mono
              />
            </div>
          </FieldGroup>

          <FieldGroup label="客户端提示" hint={blocking ? "留空使用默认文案" : undefined}>
            <div className="space-y-3">
              <Input value={draft.title} placeholder="标题" onChange={(e) => set("title", e.target.value)} />
              <Textarea rows={3} value={draft.message} placeholder="说明" onChange={(e) => set("message", e.target.value)} />
              <div className="grid grid-cols-[1fr_10rem] gap-3">
                <Input value={draft.actionUrl} placeholder="按钮链接，例如下载页 https://" onChange={(e) => set("actionUrl", e.target.value)} />
                <Input value={draft.actionLabel} placeholder="按钮文字" onChange={(e) => set("actionLabel", e.target.value)} />
              </div>
            </div>
          </FieldGroup>

          <FieldGroup label="生效时间" hint="留空表示不限">
            <div className="grid grid-cols-2 gap-3">
              <Input type="datetime-local" value={draft.startsAt} onChange={(e) => set("startsAt", e.target.value)} />
              <Input type="datetime-local" value={draft.endsAt} onChange={(e) => set("endsAt", e.target.value)} />
            </div>
          </FieldGroup>

          <FieldGroup label="备注">
            <Textarea rows={2} value={draft.note} placeholder="仅管理员可见" onChange={(e) => set("note", e.target.value)} />
          </FieldGroup>
        </div>

        <SheetFooter className="shrink-0 flex-row justify-end gap-2 border-t px-6 py-3">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={submit} disabled={save.isPending}>
            {save.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function ListField({
  label,
  value,
  onChange,
  placeholder,
  negate,
  onNegate,
  negateHint,
  mono
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  negate?: boolean;
  onNegate?: (value: boolean) => void;
  negateHint?: string;
  mono?: boolean;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <Label className="text-sm">{label}</Label>
        {onNegate ? (
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Checkbox checked={Boolean(negate)} onCheckedChange={(checked) => onNegate(checked === true)} />
            不在清单内时命中
          </label>
        ) : null}
      </div>
      <Textarea
        rows={2}
        className={cn("text-sm", mono && "font-mono text-xs")}
        value={value}
        placeholder={placeholder ? `${placeholder}（一行一个）` : "一行一个"}
        onChange={(e) => onChange(e.target.value)}
      />
      {negateHint ? <p className="text-[11px] text-muted-foreground">{negateHint}</p> : null}
    </div>
  );
}

function RangeInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <Input inputMode="numeric" className="font-mono" value={value} placeholder="不限" onChange={(e) => onChange(e.target.value.replace(/\D/g, ""))} />
    </div>
  );
}
