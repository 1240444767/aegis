"use client";

import { useMemo, useState } from "react";
import { Check, ChevronDown, Copy, Eye, EyeOff, ExternalLink } from "lucide-react";
import type { PaymentConfigField, PaymentProviderMeta } from "@/lib/api/types";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

type ChangeHandler = (key: string, value: unknown) => void;

// ── 分区定义 ──────────────────────────
// 顺序即渲染顺序；后端字段的 group 落在这里，未知 group 归入「其他」

const GROUPS: Array<{ key: string; title: string; hint?: string }> = [
  { key: "credential", title: "商户凭据" },
  { key: "gateway", title: "网关与回调" },
  { key: "limit", title: "交易限额", hint: "0 表示不限" },
  { key: "other", title: "其他" }
];

// ── 单字段渲染 ──────────────────────────

function FieldShell({ field, children }: { field: PaymentConfigField; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={`pf-${field.key}`} className="text-xs font-medium">
        {field.label}
        {field.required ? <span className="text-destructive">*</span> : null}
      </Label>
      {children}
      {field.help ? <p className="text-[11px] leading-relaxed text-muted-foreground">{field.help}</p> : null}
    </div>
  );
}

function SecretInput({ field, value, onChange }: { field: PaymentConfigField; value: string; onChange: (v: string) => void }) {
  const [show, setShow] = useState(false);
  return (
    <div className="relative">
      <Input
        id={`pf-${field.key}`}
        type={show ? "text" : "password"}
        autoComplete="off"
        className="h-9 pr-9 font-mono text-xs"
        placeholder={field.placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <button
        type="button"
        aria-label={show ? "隐藏" : "显示"}
        className="absolute top-1/2 right-2.5 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
        onClick={() => setShow(!show)}
      >
        {show ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  );
}

/** 按后端下发的字段类型动态渲染控件 */
function DynamicField({ field, data, onChange }: { field: PaymentConfigField; data: Record<string, unknown>; onChange: ChangeHandler }) {
  const raw = data[field.key];
  const id = `pf-${field.key}`;

  switch (field.type) {
    case "switch": {
      const checked = raw === undefined ? field.default === true : !!raw;
      return (
        <label
          htmlFor={id}
          className="flex h-full cursor-pointer items-start justify-between gap-3 rounded-xl border px-3 py-2.5 transition-colors hover:bg-muted/40"
        >
          <span className="min-w-0 space-y-0.5">
            <span className="block text-xs font-medium">{field.label}</span>
            {field.help ? <span className="block text-[11px] leading-relaxed text-muted-foreground">{field.help}</span> : null}
          </span>
          <Switch id={id} checked={checked} onCheckedChange={(v) => onChange(field.key, v)} className="mt-0.5 shrink-0" />
        </label>
      );
    }

    case "select": {
      const current = String(raw ?? field.default ?? field.options?.[0]?.value ?? "");
      return (
        <FieldShell field={field}>
          <Select value={current} onValueChange={(v) => onChange(field.key, v)}>
            <SelectTrigger id={id} className="h-9 w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(field.options ?? []).map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldShell>
      );
    }

    case "number":
      return (
        <FieldShell field={field}>
          <Input
            id={id}
            type="number"
            className="h-9 tabular-nums"
            placeholder={field.placeholder}
            value={raw === undefined || raw === null ? "" : String(raw)}
            onChange={(e) => onChange(field.key, e.target.value === "" ? "" : Number(e.target.value))}
          />
        </FieldShell>
      );

    case "textarea":
      return (
        <FieldShell field={field}>
          <Textarea
            id={id}
            className="min-h-24 font-mono text-xs"
            rows={4}
            placeholder={field.placeholder}
            value={String(raw ?? "")}
            onChange={(e) => onChange(field.key, e.target.value)}
          />
        </FieldShell>
      );

    case "tags": {
      const text = Array.isArray(raw) ? (raw as string[]).join(", ") : String(raw ?? "");
      return (
        <FieldShell field={field}>
          <Textarea
            id={id}
            className="min-h-16 text-xs"
            rows={2}
            placeholder={field.placeholder}
            value={text}
            onChange={(e) => {
              const items = e.target.value
                .split(/[,，\n]/)
                .map((s) => s.trim())
                .filter(Boolean);
              onChange(field.key, items.length ? items : "");
            }}
          />
        </FieldShell>
      );
    }

    case "secret":
      return (
        <FieldShell field={field}>
          <SecretInput field={field} value={String(raw ?? "")} onChange={(v) => onChange(field.key, v)} />
        </FieldShell>
      );

    default:
      return (
        <FieldShell field={field}>
          <Input
            id={id}
            className={cn("h-9", field.type === "url" && "font-mono text-xs")}
            placeholder={field.placeholder}
            value={String(raw ?? "")}
            onChange={(e) => onChange(field.key, e.target.value)}
          />
        </FieldShell>
      );
  }
}

/** 文本域与开关占满一行；其余字段在宽抽屉里两列排布。 */
function fieldSpan(field: PaymentConfigField) {
  return field.type === "textarea" || field.type === "tags" ? "@xl/form:col-span-2" : undefined;
}

// ── 回调地址 ──────────────────────────

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      aria-label="复制回调地址"
      className="flex size-8 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-background hover:text-foreground"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1600);
        } catch {
          // 剪贴板不可用（非安全上下文）时静默失败，用户仍可手动选中复制
        }
      }}
    >
      {copied ? <Check className="size-4 text-emerald-500" /> : <Copy className="size-4" />}
    </button>
  );
}

/**
 * 回调地址：把后端下发的 callbackPath 拼成完整地址供一键复制。
 *
 * 基址优先取显式配置的 API 直连地址，未配置（同源代理部署）时退回当前 origin，
 * 因此控制台与后端不同域时也不会给出错误的地址。
 */
export function PaymentCallbackAddress({ meta }: { meta: PaymentProviderMeta }) {
  const origin = useOrigin();
  const base = appConfig.apiBaseUrl || origin;
  const url = meta.callbackPath ? `${base}${meta.callbackPath}` : "";

  if (!url && !meta.callbackNote) return null;

  return (
    <div className="space-y-2">
      {url ? (
        <div className="flex items-start gap-1 rounded-xl bg-muted/60 py-1 pr-1 pl-3">
          {/* 地址要原样填进上游后台，截断会让人复制或核对时看不出差异，因此换行显示 */}
          <code className="min-w-0 flex-1 py-1.5 font-mono text-xs leading-relaxed break-all">{url}</code>
          <CopyButton value={url} />
        </div>
      ) : null}
      {meta.callbackNote ? <p className="text-[11px] leading-relaxed text-muted-foreground">{meta.callbackNote}</p> : null}
    </div>
  );
}

// ── 能力标签 ──────────────────────────

const CAPABILITY_LABELS: Array<{ key: keyof PaymentProviderMeta["capabilities"]; label: string }> = [
  { key: "redirect", label: "收银台跳转" },
  { key: "qrcode", label: "扫码支付" },
  { key: "webhookSignature", label: "回调验签" },
  { key: "remoteQuery", label: "上游查单" },
  { key: "sandbox", label: "沙箱环境" },
  { key: "subMerchant", label: "服务商模式" }
];

export function PaymentCapabilityBadges({ meta, className }: { meta: PaymentProviderMeta; className?: string }) {
  const active = CAPABILITY_LABELS.filter((c) => meta.capabilities?.[c.key]);
  if (!active.length) return null;
  return (
    <div className={cn("flex flex-wrap gap-1", className)}>
      {active.map((c) => (
        <Badge key={c.key} variant="outline" size="sm" className="font-normal text-muted-foreground">
          {c.label}
        </Badge>
      ))}
    </div>
  );
}

// ── 导出：动态配置表单 ──────────────────────────

/**
 * 渠道参数表单，由后端字段 schema 驱动，按「凭据 / 网关 / 限额 / 其他」分区，
 * 高级项折叠。分区标题与外层表单的分区标题同一套样式，由 `section` 渲染。
 */
export function PaymentProviderFields({
  meta,
  data,
  onChange,
  section
}: {
  meta: PaymentProviderMeta;
  data: Record<string, unknown>;
  onChange: ChangeHandler;
  section: (props: { title: string; hint?: string; children: React.ReactNode }) => React.ReactNode;
}) {
  const [showAdvanced, setShowAdvanced] = useState(false);

  const { grouped, advancedFields } = useMemo(() => {
    const all = meta.fields ?? [];
    const adv = all.filter((f) => f.advanced);
    const groups = new Map<string, PaymentConfigField[]>();
    for (const field of all) {
      if (field.advanced) continue;
      const key = GROUPS.some((g) => g.key === field.group) ? (field.group as string) : "other";
      const list = groups.get(key) ?? [];
      list.push(field);
      groups.set(key, list);
    }
    return { grouped: groups, advancedFields: adv };
  }, [meta]);

  if (!meta.fields?.length) {
    return section({
      title: "渠道参数",
      children: <p className="rounded-xl border border-dashed px-4 py-5 text-center text-xs text-muted-foreground">该渠道无需额外参数</p>
    });
  }

  return (
    <>
      {GROUPS.map((group) => {
        const list = grouped.get(group.key);
        if (!list?.length) return null;
        return (
          <div key={group.key}>
            {section({
              title: group.title,
              hint: group.hint,
              children: (
                <div className="grid gap-x-4 gap-y-4 @xl/form:grid-cols-2">
                  {list.map((field) => (
                    <div key={field.key} className={fieldSpan(field)}>
                      <DynamicField field={field} data={data} onChange={onChange} />
                    </div>
                  ))}
                </div>
              )
            })}
          </div>
        );
      })}

      {advancedFields.length > 0 ? (
        <Collapsible open={showAdvanced} onOpenChange={setShowAdvanced}>
          <CollapsibleTrigger asChild>
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-xl border px-4 py-3 text-left text-sm font-medium transition-colors hover:bg-muted/40"
            >
              高级选项
              <span className="text-xs font-normal text-muted-foreground tabular-nums">{advancedFields.length}</span>
              <ChevronDown className={cn("ml-auto size-4 text-muted-foreground transition-transform", showAdvanced && "rotate-180")} />
            </button>
          </CollapsibleTrigger>
          <CollapsibleContent className="pt-4">
            <div className="grid gap-x-4 gap-y-4 @xl/form:grid-cols-2">
              {advancedFields.map((field) => (
                <div key={field.key} className={fieldSpan(field)}>
                  <DynamicField field={field} data={data} onChange={onChange} />
                </div>
              ))}
            </div>
          </CollapsibleContent>
        </Collapsible>
      ) : null}

      {meta.docUrl ? (
        <a
          href={meta.docUrl}
          target="_blank"
          rel="noreferrer noopener"
          className="inline-flex items-center gap-1.5 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        >
          <ExternalLink className="size-3.5" />
          {meta.name} 官方文档
        </a>
      ) : null}
    </>
  );
}

/**
 * 用后端下发的字段默认值初始化配置数据。
 * 新建渠道时调用，避免用户必须手动填写每一个有合理默认的字段。
 */
export function buildDefaultConfigData(meta?: PaymentProviderMeta | null): Record<string, unknown> {
  const data: Record<string, unknown> = {};
  for (const field of meta?.fields ?? []) {
    if (field.default !== undefined && field.default !== null) {
      data[field.key] = field.default;
    }
  }
  return data;
}
