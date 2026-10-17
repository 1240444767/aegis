"use client";

import { useRef, useState } from "react";
import {
  Braces,
  FileText,
  Image as ImageIcon,
  Loader2,
  Monitor,
  Palette,
  RotateCcw,
  Smartphone,
  Sparkles
} from "lucide-react";
import { FieldGroup, SectionCard } from "@/components/apps/app-config-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { EmailBrand, EmailPolicy, EmailScene, EmailSceneSetting } from "@/lib/api/email-policy";
import { useEmailPreviewQuery, useEmailSceneCatalogQuery, type EmailScope } from "@/lib/email-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { PolicySaveBar } from "./email-policy-panel";
import type { EmailPolicyDraft } from "./use-email-policy-draft";

type CopyField = "subject" | "title" | "lead" | "footer";

const COPY_FIELDS: Array<{ key: CopyField; label: string; max: number; multiline?: boolean }> = [
  { key: "subject", label: "主题", max: 120 },
  { key: "title", label: "标题", max: 60 },
  { key: "lead", label: "引导语", max: 300, multiline: true },
  { key: "footer", label: "页脚提示", max: 300, multiline: true }
];

const ACCENT_SWATCHES = ["#18181b", "#2563eb", "#0891b2", "#059669", "#d97706", "#dc2626", "#7c3aed", "#db2777"];

/**
 * 模板：品牌与每类信的文案。
 *
 * 文案留空即用默认文案（输入框的占位符就是默认文案）；变量写成 {{app}} 这种形式，点变量即插入到光标处。
 * 右侧预览由服务端用当前草稿渲染（与真实发信同一套模板），所见即收件人所见。
 */
export function EmailTemplatesPanel({
  scope,
  draft,
  canWrite
}: {
  scope: EmailScope;
  draft: EmailPolicyDraft;
  canWrite: boolean;
}) {
  const catalogQuery = useEmailSceneCatalogQuery();
  const scenes = catalogQuery.data?.scenes ?? [];
  const categories = catalogQuery.data?.categories ?? [];
  const [selected, setSelected] = useState<string | null>(null);
  const current = scenes.find((scene) => scene.key === selected) ?? scenes[0];
  const policy = draft.policy;

  const setBrand = (key: keyof EmailBrand, value: string) =>
    draft.update((prev) => ({ ...prev, brand: { ...prev.brand, [key]: value } }));
  const setScene = (key: string, next: EmailSceneSetting) =>
    draft.update((prev) => ({ ...prev, scenes: { ...prev.scenes, [key]: next } }));

  return (
    <div className="space-y-4">
      <BrandCard brand={policy.brand} scopeName={draft.view?.scopeName ?? ""} onChange={setBrand} disabled={!canWrite} />

      <div className="grid gap-4 xl:grid-cols-[260px_minmax(0,1fr)]">
        <SceneList
          loading={catalogQuery.isLoading}
          scenes={scenes}
          categories={categories}
          policy={policy}
          selected={current?.key}
          onSelect={setSelected}
        />
        {current ? (
          <SceneEditor
            key={current.key}
            scope={scope}
            scene={current}
            policy={policy}
            setting={policy.scenes[current.key] ?? {}}
            onChange={(next) => setScene(current.key, next)}
            disabled={!canWrite}
          />
        ) : (
          <Skeleton className="h-[560px] rounded-2xl" />
        )}
      </div>

      <PolicySaveBar draft={draft} canWrite={canWrite} />
    </div>
  );
}

function BrandCard({
  brand,
  scopeName,
  onChange,
  disabled
}: {
  brand: EmailBrand;
  scopeName: string;
  onChange: (key: keyof EmailBrand, value: string) => void;
  disabled: boolean;
}) {
  const accent = brand.accentColor?.trim() || "";
  const validAccent = /^#[0-9a-fA-F]{6}$/.test(accent);
  return (
    <SectionCard icon={<Palette className="size-4" />} title="品牌" description="对这个作用域发出的全部邮件生效，包括验证码、通知与凭证">
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-3">
          <TextInput
            label="品牌名称"
            value={brand.displayName ?? ""}
            placeholder={scopeName || "留空使用应用名称"}
            max={40}
            disabled={disabled}
            onChange={(value) => onChange("displayName", value)}
          />
          <TextInput
            label="Logo 地址"
            value={brand.logoUrl ?? ""}
            placeholder="https://cdn.example.com/logo.png"
            icon={<ImageIcon className="size-3.5" />}
            disabled={disabled}
            onChange={(value) => onChange("logoUrl", value)}
            hint="须为 https 地址，显示高度 32 像素"
          />
          <TextInput
            label="客服邮箱"
            value={brand.supportEmail ?? ""}
            placeholder="support@example.com"
            disabled={disabled}
            onChange={(value) => onChange("supportEmail", value)}
            hint="显示在页脚"
          />
        </div>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">强调色</Label>
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                disabled={disabled}
                onClick={() => onChange("accentColor", "")}
                className={cn(
                  "h-7 rounded-lg border px-2 text-[11px] transition-colors hover:bg-muted",
                  !accent && "border-primary bg-primary/5 font-medium"
                )}
              >
                默认
              </button>
              {ACCENT_SWATCHES.map((color) => (
                <button
                  key={color}
                  type="button"
                  disabled={disabled}
                  aria-label={color}
                  onClick={() => onChange("accentColor", color)}
                  className={cn(
                    "size-7 rounded-lg ring-offset-2 ring-offset-background transition-shadow",
                    accent.toLowerCase() === color && "ring-2 ring-primary"
                  )}
                  style={{ backgroundColor: color }}
                />
              ))}
              <div className="flex items-center gap-1.5">
                <input
                  type="color"
                  disabled={disabled}
                  value={validAccent ? accent : "#18181b"}
                  onChange={(event) => onChange("accentColor", event.target.value)}
                  className="size-7 cursor-pointer rounded-lg border bg-transparent p-0.5"
                  aria-label="自定义颜色"
                />
                <Input
                  value={accent}
                  disabled={disabled}
                  placeholder="#2563eb"
                  onChange={(event) => onChange("accentColor", event.target.value.trim())}
                  className={cn("h-7 w-24 font-mono text-xs", accent && !validAccent && "border-destructive")}
                />
              </div>
            </div>
            <p className="text-[10px] text-muted-foreground">用于按钮与验证码数字，文字颜色按亮度自动取黑或白</p>
          </div>
          <TextInput
            label="页脚附加文字"
            value={brand.footerText ?? ""}
            placeholder="例如公司名称、地址或备案号"
            max={200}
            disabled={disabled}
            onChange={(value) => onChange("footerText", value)}
          />
        </div>
      </div>
    </SectionCard>
  );
}

function SceneList({
  loading,
  scenes,
  categories,
  policy,
  selected,
  onSelect
}: {
  loading: boolean;
  scenes: EmailScene[];
  categories: Array<{ key: string; name: string }>;
  policy: EmailPolicy;
  selected?: string;
  onSelect: (key: string) => void;
}) {
  if (loading) {
    return <Skeleton className="h-[560px] rounded-2xl" />;
  }
  return (
    <nav className="space-y-4 rounded-2xl border bg-card p-3 xl:sticky xl:top-4 xl:self-start">
      {categories.map((category) => {
        const items = scenes.filter((scene) => scene.category === category.key);
        if (!items.length) return null;
        return (
          <div key={category.key} className="space-y-1">
            <div className="px-2 text-[11px] text-muted-foreground">{category.name}</div>
            {items.map((scene) => {
              const setting = policy.scenes[scene.key] ?? {};
              const edited = Boolean(setting.subject || setting.title || setting.lead || setting.footer);
              const enabled = scene.toggle ? setting.enabled ?? scene.defaultOn : true;
              return (
                <button
                  key={scene.key}
                  type="button"
                  onClick={() => onSelect(scene.key)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm transition-colors",
                    selected === scene.key ? "bg-muted font-medium" : "hover:bg-muted/60"
                  )}
                >
                  <span className={cn("min-w-0 flex-1 truncate", !enabled && "text-muted-foreground line-through")}>
                    {scene.name}
                  </span>
                  {edited ? <span className="size-1.5 shrink-0 rounded-full bg-sky-500" aria-label="已改写" /> : null}
                  {scene.toggle && !enabled ? (
                    <Badge variant="secondary" size="sm" className="shrink-0 font-normal">
                      已关闭
                    </Badge>
                  ) : null}
                </button>
              );
            })}
          </div>
        );
      })}
    </nav>
  );
}

function SceneEditor({
  scope,
  scene,
  policy,
  setting,
  onChange,
  disabled
}: {
  scope: EmailScope;
  scene: EmailScene;
  policy: EmailPolicy;
  setting: EmailSceneSetting;
  onChange: (next: EmailSceneSetting) => void;
  disabled: boolean;
}) {
  const [device, setDevice] = useState<"desktop" | "mobile">("desktop");
  const [format, setFormat] = useState<"html" | "text">("html");
  const focused = useRef<{ field: CopyField; element: HTMLInputElement | HTMLTextAreaElement } | null>(null);
  // 预览跟着草稿走，400ms 防抖：每次按键都请求一次服务端渲染没有必要
  const debounced = useDebouncedValue(policy, 400);
  const previewQuery = useEmailPreviewQuery(scope, scene.key, debounced);
  const preview = previewQuery.data;
  const enabled = scene.toggle ? setting.enabled ?? scene.defaultOn : true;
  const edited = Boolean(setting.subject || setting.title || setting.lead || setting.footer);

  const insertVariable = (key: string) => {
    const token = `{{${key}}}`;
    const target = focused.current;
    const field = target?.field ?? "subject";
    const value = setting[field] ?? "";
    let next = value + token;
    if (target) {
      const start = target.element.selectionStart ?? value.length;
      const end = target.element.selectionEnd ?? value.length;
      next = value.slice(0, start) + token + value.slice(end);
      requestAnimationFrame(() => {
        target.element.focus();
        target.element.setSelectionRange(start + token.length, start + token.length);
      });
    }
    onChange({ ...setting, [field]: next });
  };

  return (
    <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
      <SectionCard
        icon={<FileText className="size-4" />}
        title={scene.name}
        description={scene.description}
        aside={
          <div className="flex items-center gap-2">
            {edited && !disabled ? (
              <Button
                size="sm"
                variant="ghost"
                className="h-7 gap-1 px-2 text-xs"
                onClick={() => onChange({ enabled: setting.enabled })}
              >
                <RotateCcw className="size-3.5" /> 恢复默认文案
              </Button>
            ) : null}
            {scene.toggle ? (
              <label className="flex items-center gap-2 text-xs">
                <span className="text-muted-foreground">{enabled ? "发送" : "不发送"}</span>
                <Switch
                  checked={enabled}
                  disabled={disabled}
                  onCheckedChange={(value) => onChange({ ...setting, enabled: value })}
                />
              </label>
            ) : null}
          </div>
        }
      >
        <div className="space-y-4">
          {COPY_FIELDS.map((field) => {
            const value = setting[field.key] ?? "";
            const common = {
              value,
              disabled,
              maxLength: field.max,
              placeholder: scene.defaults[field.key],
              onFocus: (event: React.FocusEvent<HTMLInputElement | HTMLTextAreaElement>) => {
                focused.current = { field: field.key, element: event.currentTarget };
              },
              onChange: (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
                onChange({ ...setting, [field.key]: event.target.value })
            };
            return (
              <div key={field.key} className="space-y-1.5">
                <div className="flex items-baseline justify-between gap-2">
                  <Label className="text-[11px] font-medium">{field.label}</Label>
                  <span className="text-[10px] tabular-nums text-muted-foreground">
                    {value ? `${value.length}/${field.max}` : "默认"}
                  </span>
                </div>
                {field.multiline ? (
                  <Textarea {...common} rows={2} className="resize-none text-sm" />
                ) : (
                  <Input {...common} className="text-sm" />
                )}
              </div>
            );
          })}

          <FieldGroup label="可用变量" hint="点击插入到光标处">
            <div className="flex flex-wrap gap-1.5">
              {scene.variables.map((variable) => (
                <button
                  key={variable.key}
                  type="button"
                  disabled={disabled}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => insertVariable(variable.key)}
                  title={`示例：${variable.example}`}
                  className="inline-flex items-center gap-1 rounded-lg border bg-muted/40 px-2 py-1 text-[11px] transition-colors hover:border-primary/40 hover:bg-muted disabled:opacity-50"
                >
                  <Braces className="size-3 text-muted-foreground" />
                  <span className="font-mono">{`{{${variable.key}}}`}</span>
                  <span className="text-muted-foreground">{variable.label}</span>
                </button>
              ))}
            </div>
          </FieldGroup>
        </div>
      </SectionCard>

      <section className="flex min-h-[560px] flex-col overflow-hidden rounded-2xl border bg-card">
        <header className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <Sparkles className="size-4 text-muted-foreground" /> 预览
            {previewQuery.isFetching ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
          </div>
          <div className="flex items-center gap-2">
            <ToggleGroup type="single" size="sm" value={format} onValueChange={(next) => next && setFormat(next as "html" | "text")}>
              <ToggleGroupItem value="html" className="h-7 px-2.5 text-xs">
                网页
              </ToggleGroupItem>
              <ToggleGroupItem value="text" className="h-7 px-2.5 text-xs">
                纯文本
              </ToggleGroupItem>
            </ToggleGroup>
            <ToggleGroup type="single" size="sm" value={device} onValueChange={(next) => next && setDevice(next as "desktop" | "mobile")}>
              <ToggleGroupItem value="desktop" className="h-7 px-2" aria-label="桌面">
                <Monitor className="size-3.5" />
              </ToggleGroupItem>
              <ToggleGroupItem value="mobile" className="h-7 px-2" aria-label="手机">
                <Smartphone className="size-3.5" />
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
        </header>

        <div className="border-b bg-muted/30 px-4 py-2.5">
          <div className="text-[10px] text-muted-foreground">主题</div>
          {preview ? (
            <div className="truncate text-sm font-medium" title={preview.subject}>
              {preview.subject}
            </div>
          ) : (
            <Skeleton className="mt-1 h-4 w-2/3" />
          )}
        </div>

        <div className="flex flex-1 justify-center bg-muted/40 p-4">
          {previewQuery.isError ? (
            <div className="self-center text-xs text-destructive">
              {previewQuery.error instanceof Error ? previewQuery.error.message : "预览失败"}
            </div>
          ) : !preview ? (
            <Skeleton className="h-full min-h-[440px] w-full max-w-[640px] rounded-xl" />
          ) : format === "html" ? (
            <iframe
              title="邮件预览"
              sandbox=""
              srcDoc={preview.html}
              className={cn(
                "h-[520px] rounded-xl border bg-white shadow-sm transition-[width] duration-200 ease-[cubic-bezier(0.22,1,0.36,1)]",
                device === "mobile" ? "w-[375px]" : "w-full max-w-[680px]"
              )}
            />
          ) : (
            <pre
              className={cn(
                "h-[520px] overflow-auto whitespace-pre-wrap rounded-xl border bg-background p-4 font-mono text-xs leading-relaxed",
                device === "mobile" ? "w-[375px]" : "w-full max-w-[680px]"
              )}
            >
              {preview.text}
            </pre>
          )}
        </div>
      </section>
    </div>
  );
}

function TextInput({
  label,
  value,
  placeholder,
  max,
  hint,
  icon,
  disabled,
  onChange
}: {
  label: string;
  value: string;
  placeholder?: string;
  max?: number;
  hint?: string;
  icon?: React.ReactNode;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  const hasIcon = Boolean(icon);
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        {max ? (
          <span className="text-[10px] tabular-nums text-muted-foreground">
            {value.length}/{max}
          </span>
        ) : null}
      </div>
      <div className="relative">
        {hasIcon ? <span className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground">{icon}</span> : null}
        <Input
          value={value}
          maxLength={max}
          placeholder={placeholder}
          disabled={disabled}
          className={cn("text-sm", hasIcon && "pl-8")}
          onChange={(event) => onChange(event.target.value)}
        />
      </div>
      {hint ? <p className="text-[10px] text-muted-foreground">{hint}</p> : null}
    </div>
  );
}
