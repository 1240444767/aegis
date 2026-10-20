"use client";

import { useMemo, useState } from "react";
import {
  AudioLines,
  Binary,
  Calculator,
  Check,
  CloudDownload,
  Coins,
  Crown,
  Image as ImageIcon,
  ListOrdered,
  Loader2,
  MessageSquare,
  Mic,
  Plus,
  Route,
  Save,
  Search,
  SlidersHorizontal,
  Tag,
  Trash2
} from "lucide-react";
import { toast } from "sonner";
import { AIBrandBadge } from "@/components/ai/ai-brand-icon";
import { SheetBlock, ToggleTile } from "@/components/ai/ai-sheet-parts";
import { PRICE_LABEL, formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { useDeleteAIGatewayModelMutation, useSaveAIGatewayModelMutation } from "@/lib/ai-gateway-hooks";
import { useAIChannelQuery, useAIProviderCatalogQuery, useFetchAIDraftModelsMutation } from "@/lib/ai-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIResolution } from "@/lib/api/ai";
import type { AIGatewayKindMeta, AIGatewayModel, AIGatewayModelPayload, AIGatewayPriceKey } from "@/lib/api/ai-gateway";
import { cn } from "@/lib/utils";

/** 与后端 aigateway.ValidModelName 同一条规则。 */
const MODEL_RE = /^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$/;
/** 与后端图片分档键同一条规则：尺寸、质量或「质量:尺寸」。 */
const IMAGE_KEY_RE = /^([a-z0-9]{1,16}:)?[a-z0-9x]{1,16}$/;
const DECIMAL_RE = /^\d+(\.\d{1,6})?$/;
const ALL_PRICES: AIGatewayPriceKey[] = ["inputPrice", "outputPrice", "requestPrice", "imagePrice", "charPrice", "minutePrice"];

const KIND_ICON: Record<string, React.ComponentType<{ className?: string }>> = {
  chat: MessageSquare,
  embedding: Binary,
  image: ImageIcon,
  speech: AudioLines,
  transcription: Mic,
  rerank: ListOrdered
};

type TierDraft = { uid: string; key: string; price: string };

/** 数字一律按字符串存：存 number 的话输入框清不空（理由同云存储配置）。 */
export type ModelDraft = {
  id?: number;
  model: string;
  upstreamModel: string;
  kind: string;
  displayName: string;
  description: string;
  configId: number;
  enabled: boolean;
  vipOnly: boolean;
  free: boolean;
  prices: Record<AIGatewayPriceKey, string>;
  tiers: TierDraft[];
  maxOutputTokens: string;
  sortOrder: string;
};

let tierSeed = 0;

function trimZero(value?: string) {
  if (!value || Number(value) === 0) return "";
  return value.includes(".") ? value.replace(/0+$/, "").replace(/\.$/, "") : value;
}

export function seedModelDraft(model?: AIGatewayModel, kind = "chat"): ModelDraft {
  return {
    id: model?.id,
    model: model?.model ?? "",
    upstreamModel: model?.upstreamModel ?? "",
    kind: model?.kind ?? kind,
    displayName: model?.displayName ?? "",
    description: model?.description ?? "",
    configId: model?.configId ?? 0,
    enabled: model?.enabled ?? true,
    vipOnly: model?.vipOnly ?? false,
    free: model?.free ?? false,
    prices: Object.fromEntries(ALL_PRICES.map((key) => [key, model ? trimZero(model[key]) : ""])) as Record<
      AIGatewayPriceKey,
      string
    >,
    tiers: Object.entries(model?.imagePrices ?? {}).map(([key, price], index) => ({
      uid: `seed-${index}`,
      key,
      price: trimZero(price)
    })),
    maxOutputTokens: model?.maxOutputTokens ? String(model.maxOutputTokens) : "",
    sortOrder: String(model?.sortOrder ?? 0)
  };
}

/**
 * AI 中转「添加 / 编辑模型」的大号侧栏。
 *
 * 与「AI 服务」区块的通道是一条线：左栏列当前生效的通道链路（含借用的平台共享通道），
 * 右栏先从选中通道的型号清单里挑上游模型（应用自有通道可一键向供应商拉取），再定对外名、种类、价格与选项。
 * 新建时可多选上游模型批量添加：每个上游模型成一个同名的对外模型，共用这一份种类与价格。
 *
 * 对话以外的种类只能走 OpenAI 协议的通道（后端原样转发），侧栏里其余通道置灰。
 */
export function AIGatewayModelSheet({
  appKey,
  kinds,
  existing,
  initial,
  onClose
}: {
  appKey: string;
  kinds: AIGatewayKindMeta[];
  existing: AIGatewayModel[];
  initial: ModelDraft | null;
  onClose: () => void;
}) {
  return (
    <Sheet open={Boolean(initial)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-5xl">
        {initial ? (
          <ModelSheetBody
            key={initial.id ?? "new"}
            appKey={appKey}
            kinds={kinds}
            existing={existing}
            initial={initial}
            onClose={onClose}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function ModelSheetBody({
  appKey,
  kinds,
  existing,
  initial,
  onClose
}: {
  appKey: string;
  kinds: AIGatewayKindMeta[];
  existing: AIGatewayModel[];
  initial: ModelDraft;
  onClose: () => void;
}) {
  const scope = useMemo(() => ({ kind: "app" as const, appKey }), [appKey]);
  const channelQuery = useAIChannelQuery(scope);
  const providerQuery = useAIProviderCatalogQuery(scope);
  const fetchMutation = useFetchAIDraftModelsMutation(scope);
  const saveMutation = useSaveAIGatewayModelMutation(appKey);
  const deleteMutation = useDeleteAIGatewayModelMutation(appKey);

  const [draft, setDraft] = useState<ModelDraft>(initial);
  /** 批量添加时选中的上游模型（只在新建时可多选）。 */
  const [picked, setPicked] = useState<string[]>(initial.upstreamModel ? [initial.upstreamModel] : []);
  const [fetched, setFetched] = useState<Record<number, string[]>>({});
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [keyword, setKeyword] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [saving, setSaving] = useState(false);

  const creating = !draft.id;
  const channels = useMemo(() => channelQuery.data ?? [], [channelQuery.data]);
  const providers = providerQuery.data?.providers ?? [];
  const kindMeta = kinds.find((item) => item.key === draft.kind);
  const usedPrices = kindMeta?.prices ?? ALL_PRICES;
  const passthroughOnly = draft.kind !== "chat";
  const channel = channels.find((item) => item.configId === draft.configId);
  const batch = creating && picked.length > 1;

  const patch = (changes: Partial<ModelDraft>) => setDraft((current) => ({ ...current, ...changes }));
  const channelUsable = (item: AIResolution) => !passthroughOnly || item.protocol === "openai";

  /** 候选上游模型：选中通道的清单 + 拉取结果；按链路时取全部可用通道的并集。 */
  const candidates = useMemo(() => {
    const pool = draft.configId
      ? channels.filter((item) => item.configId === draft.configId)
      : channels.filter((item) => !passthroughOnly || item.protocol === "openai");
    const seen = new Set<string>();
    const out: Array<{ id: string; channel: string }> = [];
    for (const item of pool) {
      for (const id of [...(item.models ?? []), ...(item.model ? [item.model] : []), ...(fetched[item.configId] ?? [])]) {
        if (id && !seen.has(id)) {
          seen.add(id);
          out.push({ id, channel: item.configName });
        }
      }
    }
    const needle = keyword.trim().toLowerCase();
    return out
      .filter((item) => !needle || item.id.toLowerCase().includes(needle))
      .sort((a, b) => a.id.localeCompare(b.id));
  }, [channels, draft.configId, fetched, keyword, passthroughOnly]);

  const existingNames = new Set(existing.filter((item) => item.id !== draft.id).map((item) => item.model));

  const pickUpstream = (id: string) => {
    if (creating) {
      const next = picked.includes(id) ? picked.filter((item) => item !== id) : [...picked, id];
      setPicked(next);
      if (next.length === 1) {
        // 只选一个时直接填进表单；对外名还没填（或还等于上一个上游名）就跟着走
        setDraft((current) => ({
          ...current,
          upstreamModel: next[0],
          model: !current.model || current.model === current.upstreamModel ? next[0] : current.model
        }));
      }
      return;
    }
    setDraft((current) => ({ ...current, upstreamModel: id }));
  };

  const fetchModels = async () => {
    if (!channel || channel.inherited) return;
    setFetchError(null);
    try {
      const result = await fetchMutation.mutateAsync({ configId: channel.configId, provider: channel.provider, secrets: {} });
      if (result.ok) {
        const ids = (result.models ?? []).map((item) => item.id);
        setFetched((current) => ({ ...current, [channel.configId]: ids }));
        toast.success(`拉取到 ${ids.length} 个型号`);
      } else {
        setFetchError(`拉取失败${result.status ? `（HTTP ${result.status}）` : ""}：${result.error ?? "未知错误"}`);
      }
    } catch (error) {
      setFetchError(error instanceof ApiError ? error.message : "拉取失败");
    }
  };

  const buildPayload = (model: string, upstream: string): AIGatewayModelPayload | string => {
    if (!MODEL_RE.test(model)) return `模型名「${model || "未填写"}」只能包含字母、数字与 . _ : / @ + -`;
    if (upstream && !MODEL_RE.test(upstream)) return `上游模型名「${upstream}」格式无效`;
    const used = new Set(usedPrices);
    const prices = Object.fromEntries(
      ALL_PRICES.map((key) => [key, used.has(key) && draft.prices[key].trim() ? draft.prices[key].trim() : "0"])
    ) as Record<AIGatewayPriceKey, string>;
    for (const key of ALL_PRICES) {
      if (!DECIMAL_RE.test(prices[key])) return `${PRICE_LABEL[key].label}价格需为非负数，最多 6 位小数`;
    }
    const imagePrices: Record<string, string> = {};
    if (draft.kind === "image") {
      for (const tier of draft.tiers) {
        const key = tier.key.trim().toLowerCase();
        if (!IMAGE_KEY_RE.test(key) || !DECIMAL_RE.test(tier.price.trim())) return `分档「${tier.key || "未填写"}」无效`;
        imagePrices[key] = tier.price.trim();
      }
    }
    const hasPrice =
      Object.values(prices).some((value) => Number(value) > 0) || Object.values(imagePrices).some((value) => Number(value) > 0);
    if (!draft.free && !hasPrice) return "没有设置价格，免费提供请打开「免费」";
    return {
      model,
      upstreamModel: upstream === model ? "" : upstream,
      kind: draft.kind,
      displayName: batch ? "" : draft.displayName.trim(),
      description: draft.description.trim(),
      configId: draft.configId,
      enabled: draft.enabled,
      vipOnly: draft.vipOnly,
      free: draft.free,
      ...prices,
      imagePrices,
      maxOutputTokens: draft.kind === "chat" ? Number(draft.maxOutputTokens) || 0 : 0,
      sortOrder: Number(draft.sortOrder) || 0
    };
  };

  const save = async () => {
    const targets = batch
      ? picked.map((id) => ({ model: id, upstream: id }))
      : [{ model: draft.model.trim(), upstream: draft.upstreamModel.trim() }];
    const payloads: AIGatewayModelPayload[] = [];
    for (const target of targets) {
      if (existingNames.has(target.model)) {
        toast.error(`已有模型「${target.model}」`);
        return;
      }
      const payload = buildPayload(target.model, target.upstream);
      if (typeof payload === "string") {
        toast.error(payload);
        return;
      }
      payloads.push(payload);
    }
    setSaving(true);
    const failed: string[] = [];
    let lastError = "";
    for (const payload of payloads) {
      try {
        await saveMutation.mutateAsync({ id: draft.id, payload });
      } catch (error) {
        failed.push(payload.model);
        lastError = error instanceof ApiError ? error.message : "保存失败";
      }
    }
    setSaving(false);
    if (failed.length === 0) {
      toast.success(draft.id ? "模型已保存" : payloads.length > 1 ? `已添加 ${payloads.length} 个模型` : "模型已添加");
      onClose();
      return;
    }
    if (payloads.length > 1) {
      // 成功的从选择里去掉，失败的留着让管理员改了再试
      setPicked(failed);
      toast.error(`${payloads.length - failed.length} 个已添加，${failed.length} 个失败：${lastError}`);
    } else {
      toast.error(lastError);
    }
  };

  const remove = async () => {
    if (!draft.id) return;
    try {
      await deleteMutation.mutateAsync(draft.id);
      toast.success("模型已删除");
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  const KindIcon = KIND_ICON[draft.kind] ?? MessageSquare;

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
        <span className="flex size-12 shrink-0 items-center justify-center rounded-2xl bg-primary/10 text-primary">
          <KindIcon className="size-6" />
        </span>
        <div className="min-w-0 flex-1 space-y-1">
          <SheetTitle className="flex items-center gap-2 truncate text-lg">
            {creating ? (batch ? `批量添加 ${picked.length} 个模型` : "添加模型") : `编辑「${initial.model}」`}
            {!creating ? (
              <Badge variant={draft.enabled ? "success" : "outline"} size="sm">
                {draft.enabled ? "开放" : "停用"}
              </Badge>
            ) : null}
            {draft.vipOnly ? (
              <Badge variant="warning" size="sm" className="gap-1">
                <Crown className="size-3" />
                会员专享
              </Badge>
            ) : null}
          </SheetTitle>
          <SheetDescription className="flex flex-wrap items-center gap-1.5 text-xs">
            <span>{kindMeta?.label ?? draft.kind}</span>
            <span className="text-muted-foreground/60">·</span>
            <span className="font-mono">{batch ? picked.join("、") : draft.model || "未命名"}</span>
            {!batch && draft.upstreamModel && draft.upstreamModel !== draft.model ? (
              <span className="font-mono text-muted-foreground">→ {draft.upstreamModel}</span>
            ) : null}
          </SheetDescription>
        </div>
      </SheetHeader>

      <div className="grid min-h-0 flex-1 lg:grid-cols-[17rem_minmax(0,1fr)]">
        <ChannelRail
          channels={channels}
          providers={providers}
          loading={channelQuery.isLoading}
          value={draft.configId}
          usable={channelUsable}
          onChange={(configId) => {
            patch({ configId });
            setFetchError(null);
          }}
        />

        <div className="min-h-0 space-y-4 overflow-y-auto bg-muted/20 px-6 py-5">
          <SheetBlock
            icon={<Route className="size-4" />}
            title="上游模型"
            aside={
              channel && !channel.inherited ? (
                <Button type="button" size="sm" onClick={() => void fetchModels()} disabled={fetchMutation.isPending}>
                  {fetchMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <CloudDownload className="size-3.5" />}
                  一键拉取
                </Button>
              ) : null
            }
          >
            <div className="space-y-3">
              <div className="relative">
                <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  className="h-9 pl-8 text-xs"
                  placeholder={channel ? `在「${channel.configName}」的型号里搜索` : "在全部通道的型号里搜索"}
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                />
              </div>
              {fetchError ? (
                <p className="whitespace-pre-wrap break-all rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                  {fetchError}
                </p>
              ) : null}
              <div className="max-h-64 overflow-y-auto rounded-xl border bg-background">
                {candidates.length === 0 ? (
                  <p className="px-3 py-6 text-center text-xs text-muted-foreground">
                    {channel && !channel.inherited ? "通道没有登记型号，点「一键拉取」或在下面手动填写" : "暂无可选型号"}
                  </p>
                ) : (
                  candidates.map((item) => {
                    const selected = creating ? picked.includes(item.id) : draft.upstreamModel === item.id;
                    const taken = existingNames.has(item.id) || existing.some((model) => model.id !== draft.id && model.upstreamModel === item.id);
                    return (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => pickUpstream(item.id)}
                        className={cn(
                          "flex w-full items-center gap-2.5 border-b px-3 py-1.5 text-left transition-colors last:border-b-0 hover:bg-muted/50",
                          selected && "bg-primary/5"
                        )}
                      >
                        {creating ? (
                          <Checkbox checked={selected} tabIndex={-1} className="pointer-events-none" />
                        ) : (
                          <Check className={cn("size-3.5 shrink-0", selected ? "text-primary" : "text-transparent")} />
                        )}
                        <span className="min-w-0 flex-1 truncate font-mono text-xs">{item.id}</span>
                        {!draft.configId ? (
                          <span className="hidden shrink-0 text-[10px] text-muted-foreground sm:inline">{item.channel}</span>
                        ) : null}
                        {taken ? (
                          <Badge variant="outline" size="sm">
                            已开放
                          </Badge>
                        ) : null}
                      </button>
                    );
                  })
                )}
              </div>
              {batch ? (
                <p className="text-xs text-muted-foreground">
                  已选 {picked.length} 个：每个上游模型添加为一个同名模型，共用下面的种类、价格与选项
                </p>
              ) : null}
            </div>
          </SheetBlock>

          {!batch ? (
            <SheetBlock icon={<Tag className="size-4" />} title="对外信息">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="模型名" required>
                  <Input
                    className="h-9 font-mono text-xs"
                    value={draft.model}
                    placeholder="gpt-4o"
                    maxLength={128}
                    aria-invalid={draft.model !== "" && (!MODEL_RE.test(draft.model) || existingNames.has(draft.model))}
                    onChange={(event) => patch({ model: event.target.value.trim() })}
                  />
                </Field>
                <Field label="上游模型名" aside="留空与模型名相同">
                  <Input
                    className="h-9 font-mono text-xs"
                    value={draft.upstreamModel}
                    placeholder={draft.model || "openai/gpt-4o"}
                    maxLength={128}
                    onChange={(event) => {
                      patch({ upstreamModel: event.target.value.trim() });
                      if (creating) setPicked(event.target.value.trim() ? [event.target.value.trim()] : []);
                    }}
                  />
                </Field>
                <Field label="显示名">
                  <Input className="h-9 text-xs" value={draft.displayName} maxLength={128}
                    onChange={(event) => patch({ displayName: event.target.value })} />
                </Field>
                <Field label="排序">
                  <Input className="h-9 text-xs" inputMode="numeric" value={draft.sortOrder}
                    onChange={(event) => patch({ sortOrder: event.target.value })} />
                </Field>
                <div className="sm:col-span-2">
                  <Field label="说明">
                    <Textarea className="min-h-9 text-xs" rows={2} value={draft.description} maxLength={500}
                      onChange={(event) => patch({ description: event.target.value })} />
                  </Field>
                </div>
              </div>
            </SheetBlock>
          ) : null}

          <SheetBlock icon={<SlidersHorizontal className="size-4" />} title="种类">
            <div className="grid gap-2 sm:grid-cols-3">
              {kinds.map((kind) => {
                const Icon = KIND_ICON[kind.key] ?? MessageSquare;
                const active = draft.kind === kind.key;
                return (
                  <button
                    key={kind.key}
                    type="button"
                    onClick={() => {
                      const leavesChannel = kind.key !== "chat" && channel && channel.protocol !== "openai";
                      patch({ kind: kind.key, ...(leavesChannel ? { configId: 0 } : {}) });
                    }}
                    className={cn(
                      "flex items-center gap-2.5 rounded-xl border px-3 py-2.5 text-left transition-colors",
                      active ? "border-primary/50 bg-primary/5 ring-1 ring-primary/20" : "hover:bg-muted/40"
                    )}
                  >
                    <span className={cn("flex size-8 items-center justify-center rounded-lg",
                      active ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground")}>
                      <Icon className="size-4" />
                    </span>
                    <span className="min-w-0">
                      <span className="block text-sm font-medium">{kind.label}</span>
                      <span className="block truncate text-[10px] text-muted-foreground">
                        {kind.prices.map((key) => PRICE_LABEL[key].label).join(" / ")}
                      </span>
                    </span>
                  </button>
                );
              })}
            </div>
          </SheetBlock>

          <SheetBlock
            icon={<Coins className="size-4" />}
            title="价格"
            aside={<span className="text-xs text-muted-foreground">单位：点（1 点 = 1 积分）</span>}
          >
            <div className="space-y-4">
              <ToggleTile label="免费" checked={draft.free} onChange={(value) => patch({ free: value })} />
              {!draft.free ? (
                <div className="grid gap-4 sm:grid-cols-2">
                  {usedPrices.map((key) => (
                    <Field key={key} label={PRICE_LABEL[key].label} aside={PRICE_LABEL[key].unit}>
                      <Input
                        className="h-9 font-mono text-xs"
                        inputMode="decimal"
                        placeholder="0"
                        value={draft.prices[key]}
                        aria-invalid={draft.prices[key] !== "" && !DECIMAL_RE.test(draft.prices[key])}
                        onChange={(event) => patch({ prices: { ...draft.prices, [key]: event.target.value.trim() } })}
                      />
                    </Field>
                  ))}
                </div>
              ) : null}
              {draft.kind === "image" && !draft.free ? (
                <ImageTiers tiers={draft.tiers} onChange={(tiers) => patch({ tiers })} />
              ) : null}
              <PricePreview draft={draft} usedPrices={usedPrices} />
            </div>
          </SheetBlock>

          <SheetBlock icon={<Crown className="size-4" />} title="选项">
            <div className="grid gap-3 sm:grid-cols-2">
              {draft.kind === "chat" ? (
                <div className="sm:col-span-2">
                  <Field label="输出上限" aside="token，留空不限">
                    <Input className="h-9 font-mono text-xs" inputMode="numeric" placeholder="不限" value={draft.maxOutputTokens}
                      onChange={(event) => patch({ maxOutputTokens: event.target.value.trim() })} />
                  </Field>
                </div>
              ) : null}
              <ToggleTile label="会员专享" checked={draft.vipOnly} onChange={(value) => patch({ vipOnly: value })} />
              <ToggleTile label="开放" checked={draft.enabled} onChange={(value) => patch({ enabled: value })} />
            </div>
          </SheetBlock>
        </div>
      </div>

      <div className="flex items-center justify-between gap-3 border-t bg-background px-6 py-3">
        <div>
          {!creating ? (
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
          <Button type="button" onClick={() => void save()} disabled={saving}>
            {saving ? <Loader2 className="size-3.5 animate-spin" /> : batch ? <Plus className="size-3.5" /> : <Save className="size-3.5" />}
            {batch ? `添加 ${picked.length} 个` : "保存"}
          </Button>
        </div>
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除模型「{initial.model}」后，用户调用它会返回 404</AlertDialogTitle>
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

function Field({
  label,
  aside,
  required,
  children
}: {
  label: string;
  aside?: string;
  required?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-xs">
          {label}
          {required ? <span className="text-destructive"> *</span> : null}
        </Label>
        {aside ? <span className="text-[11px] text-muted-foreground">{aside}</span> : null}
      </div>
      {children}
    </div>
  );
}

function ImageTiers({ tiers, onChange }: { tiers: TierDraft[]; onChange: (tiers: TierDraft[]) => void }) {
  return (
    <div className="space-y-2 rounded-xl border bg-muted/20 p-3">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium">分档价格</span>
        <Button type="button" size="xs" variant="outline"
          onClick={() => onChange([...tiers, { uid: `tier-${++tierSeed}`, key: "", price: "" }])}>
          <Plus className="size-3" />
          添加分档
        </Button>
      </div>
      {tiers.length === 0 ? (
        <p className="text-[11px] text-muted-foreground">按尺寸（1024x1024）、质量（hd）或「质量:尺寸」覆盖每张图的价格</p>
      ) : (
        tiers.map((tier) => (
          <div key={tier.uid} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] gap-2">
            <Input className="h-8 font-mono text-xs" value={tier.key} placeholder="hd:1024x1792"
              aria-invalid={tier.key !== "" && !IMAGE_KEY_RE.test(tier.key.trim().toLowerCase())}
              onChange={(event) => onChange(tiers.map((item) => (item.uid === tier.uid ? { ...item, key: event.target.value } : item)))} />
            <Input className="h-8 font-mono text-xs" value={tier.price} placeholder="点 / 张" inputMode="decimal"
              onChange={(event) => onChange(tiers.map((item) => (item.uid === tier.uid ? { ...item, price: event.target.value } : item)))} />
            <Button type="button" size="icon" variant="ghost" className="size-8 text-muted-foreground hover:text-destructive"
              aria-label="删除分档" onClick={() => onChange(tiers.filter((item) => item.uid !== tier.uid))}>
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        ))
      )}
    </div>
  );
}

/** 按种类给一个典型用量的估价，帮管理员对一下数量级（倍率前）。计算与后端 CostMicro 同一个公式。 */
function PricePreview({ draft, usedPrices }: { draft: ModelDraft; usedPrices: AIGatewayPriceKey[] }) {
  const price = (key: AIGatewayPriceKey) => (usedPrices.includes(key) ? Number(draft.prices[key]) || 0 : 0);
  const scenarios: Record<string, { label: string; cost: number }> = {
    chat: { label: "1,000 输入 + 500 输出 token", cost: (1000 * price("inputPrice") + 500 * price("outputPrice")) / 1e6 },
    embedding: { label: "1,000 token", cost: (1000 * price("inputPrice")) / 1e6 },
    image: { label: "1 张图", cost: price("imagePrice") + (1000 * price("inputPrice") + 4160 * price("outputPrice")) / 1e6 },
    speech: { label: "1,000 字", cost: (1000 * price("charPrice")) / 1e6 },
    transcription: { label: "1 分钟音频", cost: price("minutePrice") },
    rerank: { label: "1,000 token", cost: (1000 * price("inputPrice")) / 1e6 }
  };
  const scenario = scenarios[draft.kind];
  if (!scenario) return null;
  const total = draft.free ? 0 : scenario.cost + price("requestPrice");
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-xl border border-dashed px-4 py-3 text-xs">
      <Calculator className="size-3.5 text-muted-foreground" />
      <span className="text-muted-foreground">一次调用（{scenario.label}）约</span>
      <span className="font-mono text-sm font-semibold">{formatPoints(total)}</span>
      <span className="text-muted-foreground">点，再乘以用户的会员与等级倍率</span>
    </div>
  );
}

/** 通道侧栏：当前生效的链路（含借用的平台共享通道）。非对话种类只能选 OpenAI 协议的通道。 */
function ChannelRail({
  channels,
  providers,
  loading,
  value,
  usable,
  onChange
}: {
  channels: AIResolution[];
  providers: Array<{ provider: string; icon?: string; brandColor?: string; name: string }>;
  loading: boolean;
  value: number;
  usable: (item: AIResolution) => boolean;
  onChange: (configId: number) => void;
}) {
  return (
    <aside className="flex max-h-64 min-h-0 flex-col border-b bg-background lg:max-h-none lg:border-r lg:border-b-0">
      <div className="px-4 pt-4 pb-2 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">上游通道</div>
      <div className="min-h-0 flex-1 space-y-1 overflow-y-auto px-2 pb-3">
        <RailItem
          active={value === 0}
          onClick={() => onChange(0)}
          icon={
            <span className="flex size-7 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Route className="size-3.5" />
            </span>
          }
          title="按链路"
          subtitle="按优先级尝试，失败自动回退"
        />
        {loading ? (
          <div className="mx-2 h-12 animate-pulse rounded-lg bg-muted" />
        ) : channels.length === 0 ? (
          <p className="px-2 py-4 text-center text-[11px] text-muted-foreground">没有可用通道，先到「AI 服务」区块添加</p>
        ) : (
          channels.map((item) => {
            const meta = providers.find((provider) => provider.provider === item.provider);
            const ok = usable(item);
            return (
              <RailItem
                key={item.configId}
                active={value === item.configId}
                disabled={!ok}
                onClick={() => ok && onChange(item.configId)}
                icon={<AIBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name} size="sm" />}
                title={item.configName}
                subtitle={ok ? `${meta?.name ?? item.provider} · ${(item.models ?? []).length} 个型号` : "该种类需要 OpenAI 协议的通道"}
                badge={item.inherited ? "平台共享" : undefined}
              />
            );
          })
        )}
      </div>
    </aside>
  );
}

function RailItem({
  active,
  disabled,
  onClick,
  icon,
  title,
  subtitle,
  badge
}: {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  badge?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors",
        active ? "bg-primary/10 ring-1 ring-primary/30" : "hover:bg-muted/60",
        disabled && "cursor-not-allowed opacity-50 hover:bg-transparent"
      )}
    >
      {icon}
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5">
          <span className={cn("truncate text-xs", active ? "font-semibold" : "font-medium")}>{title}</span>
          {badge ? (
            <Badge variant="info" size="sm" className="shrink-0">
              {badge}
            </Badge>
          ) : null}
        </span>
        <span className="block truncate text-[10px] text-muted-foreground">{subtitle}</span>
      </span>
    </button>
  );
}
