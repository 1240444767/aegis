"use client";

import { useState } from "react";
import { Code2, Image as ImageIcon, LayoutPanelTop, Palette, Type } from "lucide-react";
import { FieldGroup, SectionCard, SliderField } from "@/components/apps/app-config-primitives";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { EmailBrand, EmailPolicy, EmailScene, EmailSceneCatalog, EmailTheme } from "@/lib/api/email-policy";
import { useEmailSceneCatalogQuery, type EmailScope } from "@/lib/email-hooks";
import { cn } from "@/lib/utils";
import { PolicySaveBar } from "./email-policy-panel";
import { LayoutEditor, SceneEditor } from "./email-scene-editor";
import type { EmailPolicyDraft } from "./use-email-policy-draft";

export const LAYOUT_KEY = "__layout";

const ACCENT_SWATCHES = ["#18181b", "#2563eb", "#0891b2", "#059669", "#d97706", "#dc2626", "#7c3aed", "#db2777"];
const BACKGROUND_SWATCHES = ["#f4f4f5", "#f8fafc", "#fbf7f2", "#eef2ff", "#ecfdf5", "#0f172a"];
const CARD_SWATCHES = ["#ffffff", "#fffdf8", "#18181b"];

const FONT_LABELS: Record<string, { label: string; sample: string; family: string }> = {
  system: { label: "系统", sample: "系统默认字体", family: "-apple-system,'PingFang SC','Microsoft YaHei',sans-serif" },
  sans: { label: "现代", sample: "现代无衬线", family: "'Inter','PingFang SC',sans-serif" },
  serif: { label: "衬线", sample: "典雅衬线体", family: "'Songti SC','Noto Serif SC',Georgia,serif" },
  rounded: { label: "圆体", sample: "柔和圆体", family: "'SF Pro Rounded','HarmonyOS Sans SC','PingFang SC',sans-serif" }
};

const HEADER_LABELS: Record<string, string> = { plain: "纯净", bar: "顶部色条", banner: "色块页眉" };

/**
 * 模板：品牌与主题、全局版式、每类信的文案与完整模板。
 *
 * 三档自定义由浅到深：只改主题（颜色、圆角、字体、页眉），改某个场景的文案，
 * 或者直接写 MJML / HTML + Liquid 模板（单个场景的正文，或整个作用域的版式）。
 * 右侧预览由服务端用当前草稿渲染，与真实发信同一套引擎，所见即收件人所见。
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
  const catalog = catalogQuery.data;
  const scenes = catalog?.scenes ?? [];
  const [selected, setSelected] = useState<string | null>(null);
  const currentKey = selected ?? scenes[0]?.key ?? null;
  const current = scenes.find((scene) => scene.key === currentKey);
  const policy = draft.policy;

  const setBrand = (key: keyof EmailBrand, value: string) =>
    draft.update((prev) => ({ ...prev, brand: { ...prev.brand, [key]: value } }));
  const setTheme = (patch: Partial<EmailTheme>) => draft.update((prev) => ({ ...prev, theme: { ...prev.theme, ...patch } }));

  return (
    <div className="space-y-4">
      <BrandThemeCard
        brand={policy.brand}
        theme={policy.theme}
        catalog={catalog}
        scopeName={draft.view?.scopeName ?? ""}
        onBrand={setBrand}
        onTheme={setTheme}
        disabled={!canWrite}
      />

      <div className="grid gap-4 xl:grid-cols-[248px_minmax(0,1fr)]">
        <SceneList
          loading={catalogQuery.isLoading}
          catalog={catalog}
          policy={policy}
          selected={currentKey}
          onSelect={setSelected}
        />
        {!catalog ? (
          <Skeleton className="h-[640px] rounded-2xl" />
        ) : currentKey === LAYOUT_KEY ? (
          <LayoutEditor scope={scope} draft={draft} catalog={catalog} disabled={!canWrite} />
        ) : current ? (
          <SceneEditor key={current.key} scope={scope} draft={draft} scene={current} catalog={catalog} disabled={!canWrite} />
        ) : null}
      </div>

      <PolicySaveBar draft={draft} canWrite={canWrite} />
    </div>
  );
}

function BrandThemeCard({
  brand,
  theme,
  catalog,
  scopeName,
  onBrand,
  onTheme,
  disabled
}: {
  brand: EmailBrand;
  theme: EmailTheme;
  catalog?: EmailSceneCatalog;
  scopeName: string;
  onBrand: (key: keyof EmailBrand, value: string) => void;
  onTheme: (patch: Partial<EmailTheme>) => void;
  disabled: boolean;
}) {
  const defaults = catalog?.theme.defaults;
  const fonts = catalog?.theme.fonts ?? Object.keys(FONT_LABELS);
  const headers = catalog?.theme.headers ?? Object.keys(HEADER_LABELS);
  const accent = brand.accentColor?.trim() || defaults?.accent || "#18181b";
  const background = theme.background || defaults?.background || "#f4f4f5";
  const card = theme.card || defaults?.card || "#ffffff";
  const radius = theme.radius ?? defaults?.radius ?? 16;
  const header = theme.header || defaults?.header || "plain";
  const font = theme.font || defaults?.font || "system";
  const logoAlign = theme.logoAlign || defaults?.logoAlign || "left";
  const logoHeight = theme.logoHeight || defaults?.logoHeight || 32;

  return (
    <SectionCard
      icon={<Palette className="size-4" />}
      title="品牌与主题"
      description="对这个作用域发出的全部邮件生效，包括验证码、通知、凭证与自定义模板"
    >
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_260px]">
        <div className="space-y-3">
          <FieldGroup label="品牌">
            <TextInput label="品牌名称" value={brand.displayName ?? ""} placeholder={scopeName || "留空使用应用名称"} max={40} disabled={disabled} onChange={(value) => onBrand("displayName", value)} />
            <TextInput
              label="Logo 地址"
              value={brand.logoUrl ?? ""}
              placeholder="https://cdn.example.com/logo.png"
              icon={<ImageIcon className="size-3.5" />}
              disabled={disabled}
              onChange={(value) => onBrand("logoUrl", value)}
              hint="须为 https 地址；没有 Logo 时显示品牌首字徽标"
            />
            <TextInput label="客服邮箱" value={brand.supportEmail ?? ""} placeholder="support@example.com" disabled={disabled} onChange={(value) => onBrand("supportEmail", value)} />
            <TextInput label="页脚附加文字" value={brand.footerText ?? ""} placeholder="公司名称、地址或备案号" max={200} disabled={disabled} onChange={(value) => onBrand("footerText", value)} />
          </FieldGroup>
        </div>

        <div className="space-y-4">
          <FieldGroup label="颜色">
            <ColorRow label="强调色" hint="按钮、验证码、链接" value={brand.accentColor ?? ""} swatches={ACCENT_SWATCHES} disabled={disabled} onChange={(value) => onBrand("accentColor", value)} />
            <ColorRow label="页面底色" value={theme.background ?? ""} swatches={BACKGROUND_SWATCHES} disabled={disabled} onChange={(value) => onTheme({ background: value })} />
            <ColorRow label="卡片底色" value={theme.card ?? ""} swatches={CARD_SWATCHES} disabled={disabled} onChange={(value) => onTheme({ card: value })} />
          </FieldGroup>
          <FieldGroup label="排版">
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">字体</Label>
              <ToggleGroup type="single" size="sm" value={font} disabled={disabled} onValueChange={(next) => next && onTheme({ font: next })} className="w-full justify-start">
                {fonts.map((key) => (
                  <ToggleGroupItem key={key} value={key} className="h-8 flex-1 px-2 text-xs" style={{ fontFamily: FONT_LABELS[key]?.family }}>
                    {FONT_LABELS[key]?.label ?? key}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
            <SliderField label="卡片圆角" value={radius} min={0} max={32} unit="px" disabled={disabled} onChange={(value) => onTheme({ radius: value })} />
          </FieldGroup>
        </div>

        <div className="space-y-4">
          <FieldGroup label="页眉">
            <div className="grid grid-cols-3 gap-2">
              {headers.map((key) => (
                <button
                  key={key}
                  type="button"
                  disabled={disabled}
                  onClick={() => onTheme({ header: key })}
                  className={cn(
                    "space-y-1.5 rounded-xl border p-1.5 text-center text-[11px] transition-colors",
                    header === key ? "border-primary bg-primary/5 font-medium" : "hover:bg-muted/50"
                  )}
                >
                  <HeaderThumb kind={key} accent={accent} background={background} card={card} />
                  {HEADER_LABELS[key] ?? key}
                </button>
              ))}
            </div>
            <div className="flex items-center justify-between gap-2">
              <Label className="text-[11px] font-medium">Logo 对齐</Label>
              <ToggleGroup type="single" size="sm" value={logoAlign} disabled={disabled} onValueChange={(next) => next && onTheme({ logoAlign: next })}>
                <ToggleGroupItem value="left" className="h-7 px-3 text-xs">居左</ToggleGroupItem>
                <ToggleGroupItem value="center" className="h-7 px-3 text-xs">居中</ToggleGroupItem>
              </ToggleGroup>
            </div>
            <SliderField label="Logo 高度" value={logoHeight} min={20} max={72} unit="px" disabled={disabled} onChange={(value) => onTheme({ logoHeight: value })} />
          </FieldGroup>
          <ThemeSwatch accent={accent} background={background} card={card} radius={radius} font={FONT_LABELS[font]} />
        </div>
      </div>
    </SectionCard>
  );
}

/** 页眉样式的示意小图 */
function HeaderThumb({ kind, accent, background, card }: { kind: string; accent: string; background: string; card: string }) {
  return (
    <div className="overflow-hidden rounded-lg border" style={{ backgroundColor: background }}>
      {kind === "banner" ? <div className="h-3" style={{ backgroundColor: accent }} /> : <div className="flex h-3 items-center px-1.5"><span className="size-1.5 rounded-sm" style={{ backgroundColor: accent }} /></div>}
      <div className="mx-1.5 mb-1.5 mt-1 h-7 rounded-md border" style={{ backgroundColor: card, borderTop: kind === "bar" ? `3px solid ${accent}` : undefined }}>
        <div className="mx-1.5 mt-1.5 h-1 w-1/2 rounded-full bg-current opacity-30" />
        <div className="mx-1.5 mt-1 h-1 w-3/4 rounded-full bg-current opacity-15" />
      </div>
    </div>
  );
}

/** 主题小样：颜色、圆角与字体放在一起看 */
function ThemeSwatch({
  accent,
  background,
  card,
  radius,
  font
}: {
  accent: string;
  background: string;
  card: string;
  radius: number;
  font?: { sample: string; family: string };
}) {
  const darkCard = luminance(card) < 0.4;
  const text = darkCard ? "#f4f4f5" : "#18181b";
  const muted = darkCard ? "#a1a1aa" : "#71717a";
  return (
    <div className="rounded-xl p-3" style={{ backgroundColor: background }} aria-hidden>
      <div className="space-y-2 border p-3" style={{ backgroundColor: card, borderRadius: radius, borderColor: darkCard ? "#3f3f46" : "#e4e4e7", fontFamily: font?.family }}>
        <div className="text-[13px] font-semibold" style={{ color: text }}>{font?.sample ?? "示例标题"}</div>
        <div className="text-[11px]" style={{ color: muted }}>正文与次要文字的效果</div>
        <div className="inline-block px-3 py-1.5 text-[11px] font-semibold" style={{ backgroundColor: accent, color: luminance(accent) > 0.5 ? "#18181b" : "#ffffff", borderRadius: Math.min(14, Math.round(radius * 0.6)) }}>
          按钮
        </div>
      </div>
    </div>
  );
}

function luminance(hex: string) {
  const match = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!match) return 1;
  const value = parseInt(match[1], 16);
  const channel = (c: number) => {
    const v = c / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel((value >> 16) & 255) + 0.7152 * channel((value >> 8) & 255) + 0.0722 * channel(value & 255);
}

function ColorRow({
  label,
  hint,
  value,
  swatches,
  disabled,
  onChange
}: {
  label: string;
  hint?: string;
  value: string;
  swatches: string[];
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const color = value.trim();
  const valid = /^#[0-9a-fA-F]{6}$/.test(color);
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        {hint ? <span className="text-[10px] text-muted-foreground">{hint}</span> : null}
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        <button
          type="button"
          disabled={disabled}
          onClick={() => onChange("")}
          className={cn("h-7 rounded-lg border px-2 text-[11px] transition-colors hover:bg-muted", !color && "border-primary bg-primary/5 font-medium")}
        >
          默认
        </button>
        {swatches.map((swatch) => (
          <button
            key={swatch}
            type="button"
            disabled={disabled}
            aria-label={swatch}
            onClick={() => onChange(swatch)}
            className={cn("size-7 rounded-lg border ring-offset-2 ring-offset-background transition-shadow", color.toLowerCase() === swatch && "ring-2 ring-primary")}
            style={{ backgroundColor: swatch }}
          />
        ))}
        <input
          type="color"
          disabled={disabled}
          value={valid ? color : swatches[0]}
          onChange={(event) => onChange(event.target.value)}
          className="size-7 cursor-pointer rounded-lg border bg-transparent p-0.5"
          aria-label={`自定义${label}`}
        />
        <Input
          value={color}
          disabled={disabled}
          placeholder="#rrggbb"
          onChange={(event) => onChange(event.target.value.trim())}
          className={cn("h-7 w-[5.5rem] font-mono text-xs", color && !valid && "border-destructive")}
        />
      </div>
    </div>
  );
}

function SceneList({
  loading,
  catalog,
  policy,
  selected,
  onSelect
}: {
  loading: boolean;
  catalog?: EmailSceneCatalog;
  policy: EmailPolicy;
  selected: string | null;
  onSelect: (key: string) => void;
}) {
  if (loading || !catalog) {
    return <Skeleton className="h-[640px] rounded-2xl" />;
  }
  const layoutOn = Boolean(policy.layout?.enabled && policy.layout.source?.trim());
  return (
    <nav className="space-y-4 rounded-2xl border bg-card p-3 xl:sticky xl:top-4 xl:self-start">
      <button
        type="button"
        onClick={() => onSelect(LAYOUT_KEY)}
        className={cn(
          "flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors",
          selected === LAYOUT_KEY ? "bg-muted font-medium" : "hover:bg-muted/60"
        )}
      >
        <LayoutPanelTop className="size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate">全局版式</span>
        <Badge variant={layoutOn ? "info" : "secondary"} size="sm" className="shrink-0 font-normal">
          {layoutOn ? "自定义" : "内置"}
        </Badge>
      </button>
      {catalog.categories.map((category) => {
        const items = catalog.scenes.filter((scene) => scene.category === category.key);
        if (!items.length) return null;
        return (
          <div key={category.key} className="space-y-0.5">
            <div className="px-2 pb-1 text-[11px] text-muted-foreground">{category.name}</div>
            {items.map((scene) => (
              <SceneRow key={scene.key} scene={scene} policy={policy} active={selected === scene.key} onSelect={onSelect} />
            ))}
          </div>
        );
      })}
    </nav>
  );
}

function SceneRow({ scene, policy, active, onSelect }: { scene: EmailScene; policy: EmailPolicy; active: boolean; onSelect: (key: string) => void }) {
  const setting = policy.scenes[scene.key] ?? {};
  const copyEdited = Boolean(setting.subject || setting.title || setting.lead || setting.footer);
  const custom = Boolean(setting.template?.enabled && setting.template.body?.trim());
  const enabled = scene.toggle ? setting.enabled ?? scene.defaultOn : true;
  return (
    <button
      type="button"
      onClick={() => onSelect(scene.key)}
      className={cn(
        "flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm transition-colors",
        active ? "bg-muted font-medium" : "hover:bg-muted/60"
      )}
    >
      <span className={cn("min-w-0 flex-1 truncate", !enabled && "text-muted-foreground line-through")}>{scene.name}</span>
      {custom ? <Code2 className="size-3.5 shrink-0 text-sky-600 dark:text-sky-400" aria-label="自定义模板" /> : null}
      {copyEdited && !custom ? <Type className="size-3.5 shrink-0 text-sky-600 dark:text-sky-400" aria-label="已改写文案" /> : null}
      {scene.toggle && !enabled ? (
        <Badge variant="secondary" size="sm" className="shrink-0 font-normal">
          已关闭
        </Badge>
      ) : null}
    </button>
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
        {icon ? <span className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground">{icon}</span> : null}
        <Input value={value} maxLength={max} placeholder={placeholder} disabled={disabled} className={cn("text-sm", icon && "pl-8")} onChange={(event) => onChange(event.target.value)} />
      </div>
      {hint ? <p className="text-[10px] text-muted-foreground">{hint}</p> : null}
    </div>
  );
}
