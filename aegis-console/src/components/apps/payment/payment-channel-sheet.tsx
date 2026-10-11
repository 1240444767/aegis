"use client";

import { useMemo, useState, type ReactNode } from "react";
import { ArrowLeft, CheckCircle2, ChevronRight, Loader2, PlugZap, Trash2, TriangleAlert, X } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { PaymentConfig, PaymentProviderMeta } from "@/lib/api/types";
import {
  useCreateAdminPaymentConfigMutation,
  useDeleteAdminPaymentConfigMutation,
  useTestAdminPaymentConfigMutation,
  useUpdateAdminPaymentConfigMutation
} from "@/lib/admin-hooks";
import { PaymentBrandBadge } from "@/components/payment/payment-brand-icon";
import { SearchBox } from "@/components/payment/payment-list-parts";
import {
  PaymentCallbackAddress,
  PaymentCapabilityBadges,
  PaymentProviderFields,
  buildDefaultConfigData
} from "@/components/payment/provider-fields";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

type FormData = {
  configName: string;
  enabled: boolean;
  isDefault: boolean;
  description: string;
  configData: Record<string, unknown>;
};

type TestResult = Record<string, unknown>;

export type ChannelSheetTarget = { kind: "create" } | { kind: "edit"; config: PaymentConfig };

/**
 * 收款渠道抽屉：选渠道与填参数在同一个抽屉里完成。
 *
 * 新建时先列出渠道目录，选中后原地切到参数表单（可返回重选）；编辑时直接进表单。
 * 以前是「弹窗选渠道 → 关弹窗 → 开抽屉」，两层浮层来回切换，选错了也回不去。
 *
 * 调用方每次打开都换 key，表单只在挂载时按 target 初始化一次。
 */
export function PaymentChannelSheet({
  open,
  onOpenChange,
  target,
  appId,
  methods,
  methodsLoading,
  configs
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  target: ChannelSheetTarget;
  appId: number;
  methods: PaymentProviderMeta[];
  methodsLoading: boolean;
  configs: PaymentConfig[];
}) {
  const editing = target.kind === "edit" ? target.config : null;
  const [method, setMethod] = useState<string | null>(editing ? String(editing.payment_method ?? "") : null);
  const [form, setForm] = useState<FormData | null>(() =>
    editing
      ? {
          configName: String(editing.config_name ?? ""),
          enabled: editing.enabled !== false,
          isDefault: Boolean(editing.is_default),
          description: String(editing.description ?? ""),
          configData: (editing.config_data as Record<string, unknown>) || {}
        }
      : null
  );
  const meta = methods.find((m) => m.method === method);

  const configuredCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const cfg of configs) {
      const key = String(cfg.payment_method);
      counts[key] = (counts[key] ?? 0) + 1;
    }
    return counts;
  }, [configs]);

  function pick(next: PaymentProviderMeta) {
    const sameMethod = configuredCounts[next.method] ?? 0;
    setMethod(next.method);
    setForm({
      configName: sameMethod ? `${next.name} ${sameMethod + 1}` : next.name,
      enabled: true,
      // 应用的第一个渠道预先勾上「默认渠道」，管理员可以取消
      isDefault: configs.length === 0,
      description: "",
      // 用后端下发的字段默认值预填，减少必填项
      configData: buildDefaultConfigData(next)
    });
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      {/* 不自动聚焦：编辑时首个输入框会被全选，误触一个键就把配置名称清掉了 */}
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-[640px]" onOpenAutoFocus={(e) => e.preventDefault()}>
        {method && form ? (
          meta ? (
            <ChannelForm
              key={method}
              appId={appId}
              meta={meta}
              editing={editing}
              form={form}
              setForm={setForm}
              onBack={editing ? undefined : () => setMethod(null)}
              onDone={() => onOpenChange(false)}
            />
          ) : (
            <div className="space-y-4 p-6">
              <SheetTitle className="sr-only">收款渠道</SheetTitle>
              <SheetDescription className="sr-only">加载渠道信息</SheetDescription>
              <Skeleton className="h-12 w-60 rounded-xl" />
              <Skeleton className="h-64 w-full rounded-xl" />
            </div>
          )
        ) : (
          <ChannelCatalog methods={methods} loading={methodsLoading} configuredCounts={configuredCounts} onPick={pick} />
        )}
      </SheetContent>
    </Sheet>
  );
}

// ── 第一步：渠道目录 ──────────────────────────

function ChannelCatalog({
  methods,
  loading,
  configuredCounts,
  onPick
}: {
  methods: PaymentProviderMeta[];
  loading: boolean;
  configuredCounts: Record<string, number>;
  onPick: (meta: PaymentProviderMeta) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [category, setCategory] = useState("");

  const categories = useMemo(() => {
    const seen: string[] = [];
    for (const m of methods) {
      const name = m.categoryName || "其他";
      if (!seen.includes(name)) seen.push(name);
    }
    return seen;
  }, [methods]);

  const groups = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    const matched = methods.filter((m) => {
      if (category && (m.categoryName || "其他") !== category) return false;
      if (!kw) return true;
      return [m.name, m.method, m.description, m.categoryName, ...(m.regions ?? []), ...(m.currencies ?? [])]
        .filter(Boolean)
        .some((v) => String(v).toLowerCase().includes(kw));
    });
    // 保持后端下发的顺序（已按分组归并），仅在此处切分成组
    const ordered: Array<{ category: string; items: PaymentProviderMeta[] }> = [];
    for (const item of matched) {
      const name = item.categoryName || "其他";
      const group = ordered.find((g) => g.category === name);
      if (group) group.items.push(item);
      else ordered.push({ category: name, items: [item] });
    }
    return ordered;
  }, [methods, keyword, category]);

  return (
    <>
      <SheetHeader className="shrink-0 gap-4 border-b px-5 pt-5 pb-4 sm:px-6">
        <div className="space-y-1 pr-8">
          <SheetTitle>接入收款渠道</SheetTitle>
          <SheetDescription className="text-xs">选择渠道后填写商户参数</SheetDescription>
        </div>
        <SearchBox value={keyword} onChange={setKeyword} placeholder="搜索渠道名称、地区或币种" />
        <div className="-mx-1 flex gap-1.5 overflow-x-auto px-1 [scrollbar-width:none]">
          {["", ...categories].map((name) => (
            <button
              key={name || "all"}
              type="button"
              onClick={() => setCategory(name)}
              className={cn(
                "shrink-0 rounded-full border px-3 py-1 text-xs whitespace-nowrap transition-colors",
                category === name ? "border-foreground bg-foreground text-background" : "text-muted-foreground hover:text-foreground"
              )}
            >
              {name || "全部"}
            </button>
          ))}
        </div>
      </SheetHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6">
        {loading ? (
          <div className="space-y-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full rounded-xl" />
            ))}
          </div>
        ) : groups.length === 0 ? (
          <p className="py-16 text-center text-sm text-muted-foreground">没有匹配的渠道</p>
        ) : (
          <div className="space-y-6">
            {groups.map((group) => (
              <section key={group.category} className="space-y-2">
                <h3 className="text-xs font-medium text-muted-foreground">{group.category}</h3>
                <ul className="divide-y overflow-hidden rounded-xl border">
                  {group.items.map((meta) => {
                    const count = configuredCounts[meta.method] ?? 0;
                    return (
                      <li key={meta.method}>
                        <button
                          type="button"
                          onClick={() => onPick(meta)}
                          className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/40 focus-visible:bg-muted/50 focus-visible:outline-none"
                        >
                          <PaymentBrandBadge slug={meta.icon} brandColor={meta.brandColor} name={meta.name} />
                          <span className="min-w-0 flex-1">
                            <span className="flex items-center gap-2">
                              <span className="truncate text-sm font-medium">{meta.name}</span>
                              {count > 0 ? (
                                <Badge variant="secondary" size="sm" className="shrink-0 font-normal">
                                  已接入 {count}
                                </Badge>
                              ) : null}
                            </span>
                            {meta.description ? (
                              <span className="mt-0.5 line-clamp-1 text-xs text-muted-foreground">{meta.description}</span>
                            ) : null}
                            {meta.regions?.length ? (
                              <span className="mt-0.5 block truncate text-[11px] text-muted-foreground/80">{meta.regions.join("、")}</span>
                            ) : null}
                          </span>
                          <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </section>
            ))}
          </div>
        )}
      </div>
    </>
  );
}

// ── 第二步：渠道参数 ──────────────────────────

/**
 * 必填校验：返回第一个未填字段的显示名，全部填齐时返回 null。
 * 表单上的必填标记来自后端 schema，这里按同一份 schema 校验，避免「标了必填却能提交、由上游报错兜底」。
 */
function findMissingField(form: FormData, meta: PaymentProviderMeta): string | null {
  if (!form.configName.trim()) return "配置名称";
  for (const field of meta.fields ?? []) {
    if (!field.required) continue;
    const value = form.configData[field.key];
    const empty =
      value === undefined ||
      value === null ||
      (typeof value === "string" && !value.trim()) ||
      (Array.isArray(value) && value.length === 0);
    if (empty) return field.label;
  }
  return null;
}

function ChannelForm({
  appId,
  meta,
  editing,
  form,
  setForm,
  onBack,
  onDone
}: {
  appId: number;
  meta: PaymentProviderMeta;
  editing: PaymentConfig | null;
  form: FormData;
  setForm: (update: (form: FormData | null) => FormData | null) => void;
  onBack?: () => void;
  onDone: () => void;
}) {
  const createMutation = useCreateAdminPaymentConfigMutation();
  const updateMutation = useUpdateAdminPaymentConfigMutation();
  const deleteMutation = useDeleteAdminPaymentConfigMutation();
  const testMutation = useTestAdminPaymentConfigMutation();
  const [testResult, setTestResult] = useState<TestResult | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const saving = createMutation.isPending || updateMutation.isPending;

  const set = <K extends keyof FormData>(key: K, value: FormData[K]) => setForm((s) => (s ? { ...s, [key]: value } : s));
  const setConfigField = (key: string, value: unknown) =>
    setForm((s) => (s ? { ...s, configData: { ...s.configData, [key]: value } } : s));

  async function handleSave() {
    const missing = findMissingField(form, meta);
    if (missing) {
      toast.error(`请填写「${missing}」`);
      return;
    }
    const payload: Record<string, unknown> = {
      appid: appId,
      payment_method: meta.method,
      config_name: form.configName.trim(),
      enabled: form.enabled,
      is_default: form.isDefault,
      description: form.description.trim(),
      config_data: form.configData
    };
    try {
      if (editing) {
        await updateMutation.mutateAsync({ ...payload, config_id: editing.id });
        toast.success("渠道配置已保存");
      } else {
        await createMutation.mutateAsync(payload);
        toast.success(`已接入${meta.name}`);
      }
      onDone();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  async function handleTest() {
    if (!editing) return;
    try {
      const result = await testMutation.mutateAsync({ appid: appId, config_id: editing.id });
      setTestResult(result && typeof result === "object" ? result : { api_accessible: true });
    } catch (err) {
      setTestResult({ config_valid: false, error: err instanceof ApiError ? err.message : "连通测试失败" });
    }
  }

  async function handleDelete() {
    if (!editing) return;
    try {
      await deleteMutation.mutateAsync({ appid: appId, config_id: editing.id });
      setConfirmDelete(false);
      toast.success("渠道配置已删除");
      onDone();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  }

  return (
    <>
      <SheetHeader className="shrink-0 gap-0 border-b px-5 pt-4 pb-4 sm:px-6">
        {onBack ? (
          <button
            type="button"
            onClick={onBack}
            className="mb-3 inline-flex w-fit items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft className="size-3.5" />
            重新选择渠道
          </button>
        ) : null}
        <div className="flex items-start gap-3 pr-8">
          <PaymentBrandBadge slug={meta.icon} brandColor={meta.brandColor} name={meta.name} size="lg" />
          <div className="min-w-0 flex-1 space-y-1">
            <div className="flex flex-wrap items-center gap-2">
              <SheetTitle className="text-base">{editing ? form.configName || meta.name : `接入${meta.name}`}</SheetTitle>
              {meta.categoryName ? (
                <Badge variant="outline" size="sm" className="font-normal">
                  {meta.categoryName}
                </Badge>
              ) : null}
            </div>
            <SheetDescription className="line-clamp-2 text-xs leading-relaxed">
              {editing ? meta.name : meta.description || meta.name}
            </SheetDescription>
          </div>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
          {meta.regions?.length ? <span>地区 {meta.regions.join("、")}</span> : null}
          {meta.currencies?.length ? (
            <span>
              币种 {meta.currencies.slice(0, 6).join("、")}
              {meta.currencies.length > 6 ? ` 等 ${meta.currencies.length} 种` : ""}
            </span>
          ) : null}
        </div>
        <PaymentCapabilityBadges meta={meta} className="mt-2" />
      </SheetHeader>

      {/* 分栏跟随抽屉宽度（@container），不跟随视口：视口断点会在窄窗口里把抽屉也排成两列 */}
      <div className="@container/form min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6">
        <div className="space-y-7">
          <FormSection title="基本信息">
            <div className="grid gap-4 @xl/form:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="channel-name" className="text-xs font-medium">
                  配置名称<span className="text-destructive">*</span>
                </Label>
                <Input id="channel-name" className="h-9" value={form.configName} onChange={(e) => set("configName", e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="channel-note" className="text-xs font-medium">
                  备注
                </Label>
                <Input
                  id="channel-note"
                  className="h-9"
                  placeholder="选填"
                  value={form.description}
                  onChange={(e) => set("description", e.target.value)}
                />
              </div>
              <ToggleCard
                id="channel-enabled"
                label="启用"
                hint="停用后不再用于新订单"
                checked={form.enabled}
                onChange={(v) => set("enabled", v)}
              />
              <ToggleCard
                id="channel-default"
                label="默认渠道"
                hint="下单未指定渠道时使用"
                checked={form.isDefault}
                onChange={(v) => set("isDefault", v)}
              />
            </div>
          </FormSection>

          {meta.callbackPath || meta.callbackNote ? (
            <FormSection title="回调地址">
              <PaymentCallbackAddress meta={meta} />
            </FormSection>
          ) : null}

          <PaymentProviderFields
            meta={meta}
            data={form.configData}
            onChange={setConfigField}
            section={({ title, hint, children }) => (
              <FormSection title={title} hint={hint}>
                {children}
              </FormSection>
            )}
          />
        </div>
      </div>

      {/* 连通测试结果贴在操作条上方：测试按钮在固定区，结果若放进滚动区会看不见 */}
      {testResult ? (
        <div className="max-h-52 shrink-0 overflow-y-auto border-t px-5 py-3 sm:px-6">
          <TestResultPanel result={testResult} onDismiss={() => setTestResult(null)} />
        </div>
      ) : null}

      <div className="flex shrink-0 flex-wrap items-center gap-2 border-t bg-muted/20 px-5 py-3 sm:px-6">
        {editing ? (
          <Button
            size="sm"
            variant="ghost"
            className="h-9 gap-1.5 text-destructive hover:bg-destructive/10 hover:text-destructive"
            disabled={deleteMutation.isPending}
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2 className="size-4" />
            <span className="hidden sm:inline">删除</span>
          </Button>
        ) : null}
        <div className="ml-auto flex items-center gap-2">
          {editing ? (
            <Button size="sm" variant="outline" className="h-9 gap-1.5" disabled={testMutation.isPending} onClick={() => void handleTest()}>
              {testMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <PlugZap className="size-4" />}
              连通测试
            </Button>
          ) : null}
          <Button size="sm" className="h-9 min-w-20 gap-1.5" disabled={saving} onClick={() => void handleSave()}>
            {saving ? <Loader2 className="size-4 animate-spin" /> : null}
            {editing ? "保存" : "接入"}
          </Button>
        </div>
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除「{form.configName || meta.name}」？</AlertDialogTitle>
            <AlertDialogDescription>删除后该配置不能再用于下单，已保存的商户凭据无法恢复。已有订单不受影响。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              disabled={deleteMutation.isPending}
              onClick={(e) => {
                e.preventDefault();
                void handleDelete();
              }}
            >
              {deleteMutation.isPending ? "删除中…" : "删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function FormSection({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="space-y-3">
      <div className="flex items-baseline gap-2 border-b pb-2">
        <h3 className="text-sm font-medium">{title}</h3>
        {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
      </div>
      {children}
    </section>
  );
}

function ToggleCard({
  id,
  label,
  hint,
  checked,
  onChange
}: {
  id: string;
  label: string;
  hint: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <label
      htmlFor={id}
      className={cn(
        "flex cursor-pointer items-start justify-between gap-3 rounded-xl border px-3 py-2.5 transition-colors hover:bg-muted/40",
        checked && "border-emerald-500/30 bg-emerald-500/5 hover:bg-emerald-500/10"
      )}
    >
      <span className="min-w-0 space-y-0.5">
        <span className="block text-xs font-medium">{label}</span>
        <span className="block text-[11px] text-muted-foreground">{hint}</span>
      </span>
      <Switch id={id} checked={checked} onCheckedChange={onChange} className="mt-0.5 shrink-0" />
    </label>
  );
}

/**
 * 连通测试结果。
 *
 * 各渠道返回的字段不统一（config_valid / api_accessible / error 之外还有渠道特有信息，
 * 如 Square 的门店名、微信的证书下载结果），因此只把三个通用字段提为结论，
 * 其余原样列出，避免丢掉排障时最有用的上游报错原文。
 */
function TestResultPanel({ result, onDismiss }: { result: Record<string, unknown>; onDismiss: () => void }) {
  const configValid = result.config_valid !== false;
  const accessible = result.api_accessible === true;
  const error = typeof result.error === "string" ? result.error : "";
  const ok = configValid && accessible;
  const extras = Object.entries(result).filter(([k]) => !["config_valid", "api_accessible", "error"].includes(k));

  return (
    <div className={cn("space-y-2 rounded-xl border p-3", ok ? "border-emerald-500/30 bg-emerald-500/5" : "border-amber-500/30 bg-amber-500/5")}>
      <div className="flex items-center gap-2">
        {ok ? <CheckCircle2 className="size-4 shrink-0 text-emerald-500" /> : <TriangleAlert className="size-4 shrink-0 text-amber-500" />}
        <span className="text-sm font-medium">{ok ? "连通正常" : configValid ? "上游不可达" : "配置校验未通过"}</span>
        <button type="button" className="ml-auto text-muted-foreground hover:text-foreground" onClick={onDismiss} aria-label="关闭测试结果">
          <X className="size-4" />
        </button>
      </div>
      {error ? <p className="rounded-lg bg-background/70 px-2.5 py-1.5 font-mono text-[11px] leading-relaxed break-all">{error}</p> : null}
      {extras.length > 0 ? (
        <dl className="space-y-1">
          {extras.map(([key, value]) => (
            <div key={key} className="flex items-baseline gap-2 text-[11px]">
              <dt className="shrink-0 text-muted-foreground">{key}</dt>
              <dd className="min-w-0 truncate font-mono">{String(value)}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </div>
  );
}
