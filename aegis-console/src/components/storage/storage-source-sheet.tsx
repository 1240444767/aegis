"use client";

import { useMemo, useState } from "react";
import { ArrowLeft, ChevronDown, ExternalLink, Info, Loader2, Lock, Star } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type {
  StorageConfigField,
  StorageProviderMeta,
  StorageScope,
  StorageSource,
  StorageSourcePayload
} from "@/lib/api/storage-center";
import { useSaveStorageSourceMutation, useStorageProvidersQuery } from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";
import { StorageBrandBadge } from "./storage-shared";

type Form = StorageSourcePayload;

function seedForm(meta: StorageProviderMeta | undefined, source: StorageSource | null): Form {
  if (source) {
    return {
      provider: source.provider,
      config_name: source.config_name,
      access_mode: source.access_mode,
      enabled: source.enabled,
      is_default: source.is_default,
      proxy_download: source.proxy_download,
      base_url: source.base_url ?? "",
      root_path: source.root_path ?? "",
      description: source.description ?? "",
      config_data: { ...source.config_data }
    };
  }
  const data: Record<string, unknown> = {};
  for (const field of meta?.fields ?? []) {
    if (field.default !== undefined) data[field.key] = field.default;
  }
  return {
    provider: meta?.provider ?? "",
    config_name: meta?.provider ?? "",
    access_mode: "private",
    enabled: true,
    is_default: false,
    proxy_download: true,
    base_url: "",
    root_path: "",
    description: "",
    config_data: data
  };
}

/**
 * 新建 / 编辑存储源。
 *
 * 新建先选提供商，再填由后端目录声明的字段；编辑直接进表单。密钥字段出网已被抹除：
 * 已配置的显示「已配置」占位，留空提交即不修改。
 */
export function StorageSourceSheet({
  open,
  onOpenChange,
  scope,
  appid,
  source,
  onSaved
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  scope: StorageScope;
  appid?: number | null;
  source: StorageSource | null;
  onSaved?: (saved: StorageSource, created: boolean) => void;
}) {
  const providersQuery = useStorageProvidersQuery();
  const providers = useMemo(
    () => (providersQuery.data ?? []).filter((item) => scope === "global" || !item.globalOnly),
    [providersQuery.data, scope]
  );
  const [picked, setPicked] = useState<string | null>(null);
  const [draft, setDraft] = useState<{ key: string; value: Form } | null>(null);
  const save = useSaveStorageSourceMutation(scope);

  const providerKey = source?.provider ?? picked;
  const meta = providers.find((item) => item.provider === providerKey);
  const draftKey = `${source?.id ?? "new"}:${providerKey ?? ""}`;
  const form = draft?.key === draftKey ? draft.value : seedForm(meta, source);
  const patch = (next: Partial<Form>) => setDraft({ key: draftKey, value: { ...form, ...next } });
  const setField = (key: string, value: unknown) => patch({ config_data: { ...form.config_data, [key]: value } });

  const close = (next: boolean) => {
    if (!next) {
      setDraft(null);
      setPicked(null);
    }
    onOpenChange(next);
  };

  const needsDomain =
    meta?.publicBaseUrlRequired && form.access_mode === "public" && !form.proxy_download && !form.base_url.trim();

  async function submit() {
    if (!meta) return;
    const missing = meta.fields.find((field) => {
      if (!field.required) return false;
      const value = form.config_data[field.key];
      if (field.secret && source?.secrets_set?.includes(field.key)) return false;
      return value === undefined || value === null || String(value).trim() === "";
    });
    if (!form.config_name.trim()) {
      toast.error("请填写名称");
      return;
    }
    if (missing) {
      toast.error(`请填写${missing.label}`);
      return;
    }
    try {
      const saved = await save.mutateAsync({
        ...form,
        provider: meta.provider,
        proxy_download: form.access_mode === "private" ? true : form.proxy_download,
        config_id: source?.id,
        appid: scope === "app" ? appid : undefined
      });
      toast.success(source ? "已保存" : "已添加");
      onSaved?.(saved, !source);
      close(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  const showPicker = !source && !meta;

  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent side="right" className="w-[min(640px,100vw)] gap-0 overflow-y-auto p-0">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="flex items-center gap-3 text-base">
            {!source && meta ? (
              <Button variant="ghost" size="icon-sm" className="-ml-2" onClick={() => setPicked(null)}>
                <ArrowLeft className="size-4" />
              </Button>
            ) : null}
            {meta ? <StorageBrandBadge meta={meta} size="sm" /> : null}
            {source ? `编辑 ${source.config_name}` : meta ? `添加 ${meta.name}` : "添加存储"}
          </SheetTitle>
        </SheetHeader>

        {showPicker ? (
          <ProviderPicker providers={providers} loading={providersQuery.isLoading} onPick={setPicked} />
        ) : meta ? (
          <div className="flex flex-1 flex-col">
            <div className="flex-1 space-y-6 px-5 py-5">
              {meta.notes?.length ? (
                <div className="space-y-1 rounded-xl border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
                  {meta.notes.map((note) => (
                    <div key={note} className="flex items-start gap-1.5">
                      <Info className="mt-0.5 size-3.5 shrink-0" />
                      {note}
                    </div>
                  ))}
                </div>
              ) : null}

              <section className="grid gap-3 sm:grid-cols-2">
                <FieldShell label="名称" required>
                  <Input value={form.config_name} onChange={(e) => patch({ config_name: e.target.value })} />
                </FieldShell>
                <FieldShell label="描述">
                  <Input value={form.description} onChange={(e) => patch({ description: e.target.value })} />
                </FieldShell>
              </section>

              <section className="space-y-3">
                <SectionTitle title="连接" action={meta.docUrl ? <DocLink href={meta.docUrl} /> : null} />
                <div className="grid gap-3 sm:grid-cols-2">
                  {meta.fields
                    .filter((field) => !field.advanced)
                    .map((field) => (
                      <DynamicField
                        key={field.key}
                        field={field}
                        value={form.config_data[field.key]}
                        configured={Boolean(source?.secrets_set?.includes(field.key))}
                        onChange={(value) => setField(field.key, value)}
                      />
                    ))}
                </div>
                {meta.fields.some((field) => field.advanced) ? (
                  <Collapsible>
                    <CollapsibleTrigger className="group flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
                      <ChevronDown className="size-3.5 transition-transform group-data-[state=open]:rotate-180" />
                      高级
                    </CollapsibleTrigger>
                    <CollapsibleContent className="pt-3">
                      <div className="grid gap-3 sm:grid-cols-2">
                        {meta.fields
                          .filter((field) => field.advanced)
                          .map((field) => (
                            <DynamicField
                              key={field.key}
                              field={field}
                              value={form.config_data[field.key]}
                              configured={Boolean(source?.secrets_set?.includes(field.key))}
                              onChange={(value) => setField(field.key, value)}
                            />
                          ))}
                      </div>
                    </CollapsibleContent>
                  </Collapsible>
                ) : null}
              </section>

              <section className="space-y-3">
                <SectionTitle title="访问" />
                <ToggleGroup
                  type="single"
                  variant="outline"
                  value={form.access_mode}
                  onValueChange={(value) => value && patch({ access_mode: value as Form["access_mode"] })}
                  className="w-full"
                >
                  <ToggleGroupItem value="private" className="flex-1 gap-1.5 text-xs">
                    <Lock className="size-3.5" />
                    私有
                  </ToggleGroupItem>
                  <ToggleGroupItem value="public" className="flex-1 gap-1.5 text-xs">
                    <ExternalLink className="size-3.5" />
                    公开
                  </ToggleGroupItem>
                </ToggleGroup>
                {form.access_mode === "public" ? (
                  <ToggleRow
                    label="经 Aegis 代理下载"
                    checked={form.proxy_download}
                    onChange={(value) => patch({ proxy_download: value })}
                  />
                ) : null}
                <div className="grid gap-3 sm:grid-cols-2">
                  <FieldShell label={meta.baseUrlLabel ?? "公开访问域名"} invalid={Boolean(needsDomain)}>
                    <Input
                      value={form.base_url}
                      placeholder={meta.baseUrlPlaceholder ?? "https://cdn.example.com"}
                      onChange={(e) => patch({ base_url: e.target.value })}
                    />
                  </FieldShell>
                  <FieldShell label="根路径">
                    <Input
                      value={form.root_path}
                      placeholder="uploads"
                      onChange={(e) => patch({ root_path: e.target.value })}
                    />
                  </FieldShell>
                </div>
                {needsDomain ? <p className="text-xs text-destructive">公开直链需要公开访问域名，或开启代理下载</p> : null}
              </section>

              <section className="grid gap-2 sm:grid-cols-2">
                <ToggleRow label="启用" checked={form.enabled} onChange={(value) => patch({ enabled: value })} />
                <ToggleRow
                  label={scope === "global" ? "平台默认" : "应用默认"}
                  checked={form.is_default}
                  onChange={(value) => patch({ is_default: value })}
                />
              </section>
            </div>

            <div className="sticky bottom-0 flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
              <Button variant="ghost" onClick={() => close(false)}>
                取消
              </Button>
              <Button onClick={submit} disabled={save.isPending || Boolean(needsDomain)}>
                {save.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                {source ? "保存" : "添加"}
              </Button>
            </div>
          </div>
        ) : (
          <div className="px-5 py-10 text-center text-sm text-muted-foreground">不支持的提供商</div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function ProviderPicker({
  providers,
  loading,
  onPick
}: {
  providers: StorageProviderMeta[];
  loading: boolean;
  onPick: (provider: string) => void;
}) {
  const groups = useMemo(() => {
    const map = new Map<string, { name: string; items: StorageProviderMeta[] }>();
    for (const item of providers) {
      const key = item.featured ? "featured" : item.category;
      const name = item.featured ? "推荐" : item.categoryName;
      if (!map.has(key)) map.set(key, { name, items: [] });
      map.get(key)!.items.push(item);
    }
    return [...map.entries()].sort(([a], [b]) => (a === "featured" ? -1 : b === "featured" ? 1 : 0));
  }, [providers]);

  if (loading) {
    return (
      <div className="grid gap-2 p-5 sm:grid-cols-2">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="h-16 animate-pulse rounded-xl bg-muted" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-5 p-5">
      {groups.map(([key, group]) => (
        <div key={key} className="space-y-2">
          <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{group.name}</div>
          <div className="grid gap-2 sm:grid-cols-2">
            {group.items.map((item) => (
              <button
                key={item.provider}
                type="button"
                onClick={() => onPick(item.provider)}
                className={cn(
                  "flex items-center gap-3 rounded-xl border bg-card px-3 py-3 text-left transition-colors hover:border-foreground/20 hover:bg-accent/40",
                  item.featured && "border-orange-500/40"
                )}
              >
                <StorageBrandBadge meta={item} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5 text-sm font-medium">
                    {item.name}
                    {item.featured ? <Star className="size-3 fill-orange-500 text-orange-500" /> : null}
                  </div>
                  {item.description ? (
                    <div className="truncate text-[11px] text-muted-foreground">{item.description}</div>
                  ) : null}
                </div>
                <div className="flex shrink-0 flex-col items-end gap-1">
                  {item.capabilities.directUpload ? (
                    <Badge variant="outline" size="sm">
                      直传
                    </Badge>
                  ) : null}
                </div>
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function DynamicField({
  field,
  value,
  configured,
  onChange
}: {
  field: StorageConfigField;
  value: unknown;
  configured: boolean;
  onChange: (value: unknown) => void;
}) {
  if (field.type === "switch") {
    return <ToggleRow label={field.label} checked={Boolean(value ?? field.default)} onChange={onChange} />;
  }
  if (field.type === "select") {
    return (
      <FieldShell label={field.label} required={field.required} help={field.help}>
        <Select value={String(value ?? field.default ?? "")} onValueChange={onChange}>
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(field.options ?? []).map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FieldShell>
    );
  }
  return (
    <FieldShell label={field.label} required={field.required && !configured} help={field.help}>
      <Input
        type={field.secret ? "password" : field.type === "url" ? "url" : "text"}
        autoComplete={field.secret ? "new-password" : "off"}
        value={String(value ?? "")}
        placeholder={field.secret && configured ? "已配置，留空不修改" : field.placeholder}
        className={cn(field.secret && "font-mono")}
        onChange={(e) => onChange(e.target.value)}
      />
    </FieldShell>
  );
}

function FieldShell({
  label,
  required,
  help,
  invalid,
  children
}: {
  label: string;
  required?: boolean;
  help?: string;
  invalid?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label className={cn("text-xs", invalid && "text-destructive")}>
        {label}
        {required ? <span className="text-destructive">*</span> : null}
      </Label>
      {children}
      {help ? <p className="text-[11px] text-muted-foreground">{help}</p> : null}
    </div>
  );
}

function ToggleRow({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5 text-xs font-medium transition-colors",
        checked && "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30"
      )}
    >
      {label}
      <Switch checked={checked} onCheckedChange={onChange} />
    </label>
  );
}

function SectionTitle({ title, action }: { title: string; action?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between">
      <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{title}</div>
      {action}
    </div>
  );
}

function DocLink({ href }: { href: string }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
    >
      文档
      <ExternalLink className="size-3" />
    </a>
  );
}
