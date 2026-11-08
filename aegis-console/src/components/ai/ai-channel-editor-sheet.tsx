"use client";

import { useMemo, useState } from "react";
import {
  CheckCircle2,
  ExternalLink,
  KeyRound,
  Layers,
  Loader2,
  Save,
  Search,
  Sparkles,
  ToggleRight,
  Trash2,
  Type,
  XCircle,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { AIConfigDraftPayload, AIModelInfo, AIProviderMeta, AITestResult } from "@/lib/api/ai";
import {
  useDeleteAIConfigMutation,
  useFetchAIDraftModelsMutation,
  useSaveAIConfigMutation,
  useTestAIDraftMutation,
  type AIScope
} from "@/lib/ai-hooks";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { AIBrandBadge } from "./ai-brand-icon";
import { AIModelManager, parseModels } from "./ai-model-manager";
import { SheetBlock, ToggleTile } from "./ai-sheet-parts";
import { AI_CAPABILITY_LABELS, AIProviderFields, buildDefaultAISettings } from "./ai-provider-fields";

/** 型号管理器接管的两个字段（键与后端目录 KeyModels / KeyDefaultModel 一致）。 */
const MODELS_KEY = "models";
const DEFAULT_MODEL_KEY = "defaultModel";
const DEFAULT_TEST_MODEL = "__default__";

export type ConfigDraft = {
  configId?: number;
  provider: string;
  name: string;
  description: string;
  enabled: boolean;
  isDefault: boolean;
  shared: boolean;
  settings: Record<string, string>;
  secrets: Record<string, string>;
  secretSet: Record<string, boolean>;
};

/**
 * 新建 / 编辑 AI 通道的大号侧栏。
 *
 * 左栏选供应商（按目录分类、可搜索），右栏按「基本信息 → 连接 → 型号 → 选项 → 测试」排开。
 * 型号与测试都对**草稿**生效：新建时不必先保存就能拉型号、能测；编辑时密钥留空即沿用已存的，
 * 与保存时「留空不修改」同一个约定（后端 /ai/draft/*）。
 */
export function AIChannelEditorSheet({
  scope,
  providers,
  initial,
  onClose
}: {
  scope: AIScope;
  providers: AIProviderMeta[];
  initial: ConfigDraft | null;
  onClose: () => void;
}) {
  return (
    <Sheet open={Boolean(initial)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-5xl">
        {initial ? (
          <EditorBody
            key={initial.configId ?? "new"}
            scope={scope}
            providers={providers}
            initial={initial}
            onClose={onClose}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function EditorBody({
  scope,
  providers,
  initial,
  onClose
}: {
  scope: AIScope;
  providers: AIProviderMeta[];
  initial: ConfigDraft;
  onClose: () => void;
}) {
  const isPlatform = scope.kind === "platform";
  const saveMutation = useSaveAIConfigMutation(scope);
  const deleteMutation = useDeleteAIConfigMutation(scope);
  const fetchMutation = useFetchAIDraftModelsMutation(scope);
  const testMutation = useTestAIDraftMutation(scope);

  const [draft, setDraft] = useState<ConfigDraft>(initial);
  const [fetched, setFetched] = useState<AIModelInfo[] | null>(null);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [testModel, setTestModel] = useState(DEFAULT_TEST_MODEL);
  const [testResult, setTestResult] = useState<AITestResult | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const meta = providers.find((item) => item.provider === draft.provider);
  const models = parseModels(draft.settings[MODELS_KEY]);
  const defaultModel = (draft.settings[DEFAULT_MODEL_KEY] ?? "").trim();
  const modelsField = meta?.fields?.find((field) => field.key === MODELS_KEY);
  const capabilities = AI_CAPABILITY_LABELS.filter((item) => meta?.capabilities?.[item.key]);

  const patch = (changes: Partial<ConfigDraft>) => setDraft((current) => ({ ...current, ...changes }));
  const setSetting = (key: string, value: string) =>
    setDraft((current) => ({ ...current, settings: { ...current.settings, [key]: value } }));

  const draftPayload = (model?: string): AIConfigDraftPayload => ({
    configId: draft.configId,
    provider: draft.provider,
    settings: draft.settings,
    secrets: Object.fromEntries(Object.entries(draft.secrets).filter(([, value]) => value.trim() !== "")),
    model
  });

  const switchProvider = (provider: string) => {
    if (provider === draft.provider) return;
    const next = providers.find((item) => item.provider === provider);
    // 换供应商时字段重置成新供应商的默认值，上一家的残留值与密钥都不带过去
    setDraft((current) => ({ ...current, provider, settings: buildDefaultAISettings(next), secrets: {} }));
    setFetched(null);
    setFetchError(null);
    setTestResult(null);
    setTestModel(DEFAULT_TEST_MODEL);
  };

  const fetchModels = async () => {
    setFetchError(null);
    try {
      const result = await fetchMutation.mutateAsync(draftPayload());
      if (result.ok) {
        setFetched(result.models ?? []);
        toast.success(`拉取到 ${result.count ?? result.models?.length ?? 0} 个型号`);
      } else {
        setFetched(null);
        setFetchError(`拉取失败${result.status ? `（HTTP ${result.status}）` : ""}：${result.error ?? "未知错误"}`);
      }
    } catch (error) {
      setFetchError(error instanceof ApiError ? error.message : "拉取失败");
    }
  };

  const runTest = async () => {
    setTestResult(null);
    try {
      const result = await testMutation.mutateAsync(
        draftPayload(testModel === DEFAULT_TEST_MODEL ? undefined : testModel)
      );
      setTestResult(result);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "测试失败");
    }
  };

  const save = async () => {
    if (!draft.name.trim()) {
      toast.error("请填写通道名称");
      return;
    }
    const missing = (meta?.fields ?? []).filter(
      (field) =>
        field.required &&
        (field.secret
          ? !draft.secrets[field.key]?.trim() && !draft.secretSet[field.key]
          : !(draft.settings[field.key] ?? "").trim())
    );
    if (missing.length > 0) {
      toast.error(`请填写${missing.map((field) => `「${field.label}」`).join("")}`);
      return;
    }
    try {
      await saveMutation.mutateAsync({
        configId: draft.configId,
        payload: {
          name: draft.name.trim(),
          provider: draft.provider,
          description: draft.description,
          enabled: draft.enabled,
          isDefault: draft.isDefault,
          ...(isPlatform ? { shared: draft.shared } : {}),
          settings: draft.settings,
          // 密钥留空的键不发出去，后端也会再挡一道 —— 这条约定失效的代价是凭据被静默清空。
          secrets: Object.fromEntries(Object.entries(draft.secrets).filter(([, value]) => value.trim() !== "")),
          replaceSettings: true
        }
      });
      toast.success(draft.configId ? "通道已更新" : "通道已创建");
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const remove = async () => {
    if (!draft.configId) return;
    try {
      await deleteMutation.mutateAsync(draft.configId);
      toast.success("已删除");
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  return (
    <div
      className="flex min-h-0 flex-1 flex-col"
      onKeyDown={(event) => {
        if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "s") {
          event.preventDefault();
          void save();
        }
      }}
    >
      <SheetHeader className="flex-row items-center gap-3 border-b px-6 py-4 pr-12">
        <AIBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name} size="lg" />
        <div className="min-w-0 flex-1 space-y-1">
          <SheetTitle className="flex items-center gap-2 truncate text-lg">
            {draft.configId ? draft.name || "编辑 AI 通道" : "新建 AI 通道"}
            {draft.configId ? (
              <Badge variant={draft.enabled ? "success" : "warning"} size="sm">
                {draft.enabled ? "启用" : "停用"}
              </Badge>
            ) : null}
          </SheetTitle>
          <SheetDescription className="flex flex-wrap items-center gap-1.5 text-xs">
            <span className="font-medium text-foreground">{meta?.name ?? draft.provider}</span>
            {meta?.protocol ? (
              <Badge variant="outline" size="sm" className="font-mono uppercase">
                {meta.protocol}
              </Badge>
            ) : null}
            {capabilities.map((item) => (
              <Badge key={item.key} variant="secondary" size="sm" className="font-normal">
                {item.label}
              </Badge>
            ))}
          </SheetDescription>
        </div>
      </SheetHeader>

      <div className="grid min-h-0 flex-1 lg:grid-cols-[17rem_minmax(0,1fr)]">
        <ProviderRail providers={providers} value={draft.provider} onChange={switchProvider} />

        <div className="min-h-0 space-y-4 overflow-y-auto bg-muted/20 px-6 py-5">
          <SheetBlock icon={<Type className="size-4" />} title="基本信息">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs">
                  通道名称<span className="text-destructive"> *</span>
                </Label>
                <Input
                  className="h-9"
                  placeholder="default"
                  value={draft.name}
                  autoFocus={!draft.configId}
                  onChange={(event) => patch({ name: event.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">说明</Label>
                <Textarea
                  className="min-h-9 text-sm"
                  rows={1}
                  placeholder="可选"
                  value={draft.description}
                  onChange={(event) => patch({ description: event.target.value })}
                />
              </div>
            </div>
          </SheetBlock>

          <SheetBlock
            icon={<KeyRound className="size-4" />}
            title="连接"
            aside={
              meta?.docUrl ? (
                <a
                  href={meta.docUrl}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  <ExternalLink className="size-3" />
                  官方文档
                </a>
              ) : null
            }
          >
            <AIProviderFields
              meta={meta ? { ...meta, docUrl: undefined } : meta}
              settings={draft.settings}
              secrets={draft.secrets}
              secretSet={draft.secretSet}
              exclude={[MODELS_KEY, DEFAULT_MODEL_KEY]}
              onSetting={setSetting}
              onSecret={(key, value) => setDraft((current) => ({ ...current, secrets: { ...current.secrets, [key]: value } }))}
            />
            {meta?.notes?.length ? (
              <ul className="mt-4 space-y-1 rounded-lg bg-muted/50 px-3 py-2 text-[11px] text-muted-foreground">
                {meta.notes.map((note) => (
                  <li key={note}>{note}</li>
                ))}
              </ul>
            ) : null}
          </SheetBlock>

          {modelsField ? (
            <SheetBlock icon={<Layers className="size-4" />} title="型号">
              <AIModelManager
                label={modelsField.label}
                models={models}
                defaultModel={defaultModel}
                suggested={meta?.suggestedModels ?? []}
                fetching={fetchMutation.isPending}
                fetchError={fetchError}
                fetched={fetched}
                onFetch={() => void fetchModels()}
                onCloseFetched={() => setFetched(null)}
                onChange={(nextModels, nextDefault) =>
                  setDraft((current) => ({
                    ...current,
                    settings: { ...current.settings, [MODELS_KEY]: nextModels.join("\n"), [DEFAULT_MODEL_KEY]: nextDefault }
                  }))
                }
              />
            </SheetBlock>
          ) : null}

          <SheetBlock icon={<ToggleRight className="size-4" />} title="选项">
            <div className="grid gap-3 sm:grid-cols-2">
              <ToggleTile label="启用" checked={draft.enabled} onChange={(value) => patch({ enabled: value })} />
              <ToggleTile label="设为默认" checked={draft.isDefault} onChange={(value) => patch({ isDefault: value })} />
              {isPlatform ? (
                <ToggleTile
                  label="共享给应用兜底"
                  hint="应用没有自有通道时使用，费用由平台承担"
                  checked={draft.shared}
                  onChange={(value) => patch({ shared: value })}
                />
              ) : null}
            </div>
          </SheetBlock>

          <SheetBlock icon={<Zap className="size-4" />} title="连通性测试">
            <div className="flex flex-wrap items-center gap-2">
              <Select value={testModel} onValueChange={setTestModel}>
                <SelectTrigger className="h-9 w-72 font-mono text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={DEFAULT_TEST_MODEL}>默认型号{defaultModel || models[0] ? `（${defaultModel || models[0]}）` : ""}</SelectItem>
                  {models.map((model) => (
                    <SelectItem key={model} value={model} className="font-mono text-xs">
                      {model}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button type="button" variant="outline" onClick={() => void runTest()} disabled={testMutation.isPending}>
                {testMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Sparkles className="size-3.5" />}
                {testMutation.isPending ? "测试中" : "发送测试"}
              </Button>
            </div>
            {testResult ? <TestResultCard result={testResult} /> : null}
          </SheetBlock>
        </div>
      </div>

      <div className="flex items-center justify-between gap-3 border-t bg-background px-6 py-3">
        <div>
          {draft.configId ? (
            <Button type="button" variant="ghost" className="text-destructive hover:text-destructive" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="size-3.5" />
              删除
            </Button>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <span className="hidden text-[11px] text-muted-foreground sm:inline">⌘S 保存</span>
          <Button type="button" variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button type="button" onClick={() => void save()} disabled={saveMutation.isPending}>
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除通道「{draft.name}」后，链路里会少这一档</AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => void remove()} disabled={deleteMutation.isPending}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function TestResultCard({ result }: { result: AITestResult }) {
  const text = result.ok ? result.reply ?? "" : result.error ?? "未知错误";
  return (
    <div
      className={cn(
        "mt-3 rounded-xl border px-4 py-3",
        result.ok
          ? "border-emerald-300/60 bg-emerald-50 dark:border-emerald-700/50 dark:bg-emerald-950/40"
          : "border-destructive/40 bg-destructive/5"
      )}
    >
      <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
        {result.ok ? (
          <CheckCircle2 className="size-4 text-emerald-600 dark:text-emerald-400" />
        ) : (
          <XCircle className="size-4 text-destructive" />
        )}
        {result.ok ? "连通正常" : `测试未通过${result.status ? `（HTTP ${result.status}）` : ""}`}
        <span className="ml-auto flex items-center gap-2 font-mono text-[11px] font-normal text-muted-foreground">
          {result.model ? <span>{result.model}</span> : null}
          <span>{result.elapsedMs} ms</span>
          {result.usage ? (
            <span>
              {result.usage.inputTokens} / {result.usage.outputTokens} tokens
            </span>
          ) : null}
        </span>
      </div>
      {text ? (
        <p
          className={cn(
            "mt-2 max-h-40 overflow-y-auto whitespace-pre-wrap break-all text-xs leading-relaxed",
            result.ok ? "text-foreground" : "text-destructive"
          )}
        >
          {text.length > 800 ? `${text.slice(0, 800)}…` : text}
        </p>
      ) : null}
    </div>
  );
}

/** 供应商侧栏：按目录分类分组、可搜索；后端加一个分类时这里自动多一组。 */
function ProviderRail({
  providers,
  value,
  onChange
}: {
  providers: AIProviderMeta[];
  value: string;
  onChange: (provider: string) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const groups = useMemo(() => {
    const needle = keyword.trim().toLowerCase();
    const map = new Map<string, AIProviderMeta[]>();
    for (const provider of providers) {
      if (
        needle &&
        !provider.name.toLowerCase().includes(needle) &&
        !provider.provider.toLowerCase().includes(needle) &&
        !(provider.description ?? "").toLowerCase().includes(needle)
      ) {
        continue;
      }
      const key = provider.categoryName || "其他";
      const list = map.get(key) ?? [];
      list.push(provider);
      map.set(key, list);
    }
    return [...map.entries()];
  }, [providers, keyword]);

  return (
    <aside className="flex max-h-72 min-h-0 flex-col border-b bg-background lg:max-h-none lg:border-r lg:border-b-0">
      <div className="p-3">
        <div className="relative">
          <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8 text-xs"
            placeholder="搜索供应商"
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-2 pb-3">
        {groups.length === 0 ? (
          <p className="px-2 py-6 text-center text-xs text-muted-foreground">没有匹配的供应商</p>
        ) : (
          groups.map(([category, items]) => (
            <div key={category} className="space-y-0.5">
              <div className="px-2 pb-1 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">
                {category}
              </div>
              {items.map((provider) => {
                const active = provider.provider === value;
                return (
                  <button
                    key={provider.provider}
                    type="button"
                    onClick={() => onChange(provider.provider)}
                    className={cn(
                      "flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors",
                      active ? "bg-primary/10 ring-1 ring-primary/30" : "hover:bg-muted/60"
                    )}
                  >
                    <AIBrandBadge
                      slug={provider.icon}
                      brandColor={provider.brandColor}
                      name={provider.name}
                      size="sm"
                    />
                    <span className="min-w-0 flex-1">
                      <span className={cn("block truncate text-xs", active ? "font-semibold" : "font-medium")}>
                        {provider.name}
                      </span>
                      {provider.description ? (
                        <span className="block truncate text-[10px] text-muted-foreground">{provider.description}</span>
                      ) : null}
                    </span>
                  </button>
                );
              })}
            </div>
          ))
        )}
      </div>
    </aside>
  );
}
