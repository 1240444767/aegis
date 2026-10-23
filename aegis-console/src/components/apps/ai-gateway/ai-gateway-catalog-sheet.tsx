"use client";

import { useState } from "react";
import { BookOpenCheck, Check, Database, Eye, Loader2, RefreshCw, Search, SlidersHorizontal } from "lucide-react";
import { toast } from "sonner";
import { SheetBlock, ToggleTile } from "@/components/ai/ai-sheet-parts";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import {
  useModelCatalogQuery,
  useModelCatalogSearchQuery,
  useRefreshModelCatalogMutation,
  useSyncAIGatewayModelsMutation
} from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type {
  AIGatewayModel,
  ModelCatalogCandidate,
  ModelCatalogEntry,
  ModelCatalogField,
  ModelCatalogMatch,
  ModelCatalogSyncResult
} from "@/lib/api/ai-gateway";
import { formatDateTime } from "@/lib/datetime";
import { cn } from "@/lib/utils";

const DECIMAL_RE = /^\d+(\.\d{1,6})?$/;

const MATCH_LABEL: Record<ModelCatalogMatch, { label: string; variant: "success" | "info" | "warning" }> = {
  exact: { label: "精确", variant: "success" },
  normalized: { label: "去后缀", variant: "info" },
  fuzzy: { label: "相近", variant: "warning" }
};

const FIELD_LABEL: Record<string, string> = {
  "capabilities.vision": "识图",
  "capabilities.tools": "工具调用",
  "capabilities.reasoning": "思考",
  "capabilities.reasoningEfforts": "思考强度",
  "capabilities.contextLength": "上下文",
  "capabilities.maxOutput": "最大输出",
  "capabilities.catalogKey": "目录条目",
  displayName: "显示名",
  description: "说明",
  inputPrice: "输入单价",
  outputPrice: "输出单价",
  requestPrice: "每次调用",
  imagePrice: "每张图",
  free: "免费"
};

function showValue(value: unknown) {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "boolean") return value ? "是" : "否";
  if (Array.isArray(value)) return value.length ? value.join(" / ") : "—";
  const text = String(value);
  return text.length > 60 ? `${text.slice(0, 60)}…` : text;
}

/**
 * 「从模型目录同步」大号侧栏。
 *
 * 目录是 models.dev 为主、OpenRouter 与 LiteLLM 补缺的公开模型元数据（全平台一份，服务端每天刷新）。
 * 先选要同步的内容与价格汇率、预览每个模型对上了哪一条与会改什么，可以给某个模型改选别的条目或手动搜一条，
 * 勾选后应用。只找到「相近」条目的模型默认不勾，留给人确认。
 */
export function AIGatewayCatalogSheet({
  appKey,
  open,
  models,
  onClose
}: {
  appKey: string;
  open: boolean;
  models: AIGatewayModel[];
  onClose: () => void;
}) {
  return (
    <Sheet open={open} onOpenChange={(value) => !value && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-5xl">
        {open ? <CatalogSheetBody appKey={appKey} models={models} onClose={onClose} /> : null}
      </SheetContent>
    </Sheet>
  );
}

function CatalogSheetBody({ appKey, models, onClose }: { appKey: string; models: AIGatewayModel[]; onClose: () => void }) {
  const statusQuery = useModelCatalogQuery(appKey);
  const refreshMutation = useRefreshModelCatalogMutation(appKey);
  const syncMutation = useSyncAIGatewayModelsMutation(appKey);
  const [fields, setFields] = useState<ModelCatalogField[]>(["capabilities", "info"]);
  const [rate, setRate] = useState("");
  const [overwrite, setOverwrite] = useState(false);
  const [preview, setPreview] = useState<ModelCatalogSyncResult[] | null>(null);
  const [keys, setKeys] = useState<Record<number, string>>({});
  const [picked, setPicked] = useState<Set<number>>(new Set());
  const [searching, setSearching] = useState<number | null>(null);

  const status = statusQuery.data;
  const withPrices = fields.includes("prices");

  const toggleField = (field: ModelCatalogField) => {
    setFields((current) => (current.includes(field) ? current.filter((item) => item !== field) : [...current, field]));
    setPreview(null);
  };

  const payloadBase = () => {
    if (fields.length === 0) return "至少选一项要同步的内容";
    if (withPrices && (!DECIMAL_RE.test(rate.trim()) || Number(rate) <= 0)) return "同步价格需要填写汇率：1 美元折合多少点";
    return null;
  };

  const runPreview = async (overrides = keys) => {
    const problem = payloadBase();
    if (problem) {
      toast.error(problem);
      return;
    }
    try {
      const result = await syncMutation.mutateAsync({
        modelIds: models.map((item) => item.id),
        fields,
        priceRate: withPrices ? rate.trim() : undefined,
        overwrite,
        preview: true,
        allowFuzzy: true,
        keys: overrides
      });
      const items = result.items;
      setPreview(items);
      setPicked(new Set(items.filter((item) => item.match && item.match.match !== "fuzzy" && item.changes.length > 0).map((item) => item.modelId)));
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "预览失败");
    }
  };

  const apply = async () => {
    if (!preview || picked.size === 0) return;
    try {
      const result = await syncMutation.mutateAsync({
        modelIds: [...picked],
        fields,
        priceRate: withPrices ? rate.trim() : undefined,
        overwrite,
        allowFuzzy: true,
        keys: Object.fromEntries(
          [...picked].map((id) => [id, keys[id] ?? preview.find((item) => item.modelId === id)?.match?.entry.key ?? ""])
        )
      });
      const applied = result.items.filter((item) => item.applied).length;
      const failed = result.items.filter((item) => item.skipped).length;
      toast.success(failed ? `已更新 ${applied} 个模型，${failed} 个未更新` : `已更新 ${applied} 个模型`);
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "同步失败");
    }
  };

  const choose = (modelId: number, key: string) => {
    const next = { ...keys, [modelId]: key };
    setKeys(next);
    setSearching(null);
    void runPreview(next);
  };

  const refresh = async () => {
    try {
      const result = await refreshMutation.mutateAsync();
      toast.success(result.refreshed ? `目录已刷新，共 ${result.count} 个模型` : "刚刷新过，稍后再试");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "刷新失败");
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <SheetHeader className="border-b px-6 py-4">
        <SheetTitle>从模型目录同步</SheetTitle>
        <SheetDescription>按 models.dev、OpenRouter 与 LiteLLM 的公开数据更新模型的能力、名称说明与价格</SheetDescription>
      </SheetHeader>

      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-5">
        <SheetBlock
          icon={<Database className="size-4" />}
          title="目录"
          aside={
            <Button size="sm" variant="outline" onClick={() => void refresh()} disabled={refreshMutation.isPending}>
              {refreshMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
              立即刷新
            </Button>
          }
        >
          {statusQuery.isLoading ? (
            <p className="text-xs text-muted-foreground">读取中</p>
          ) : !status?.fetchedAt ? (
            <p className="text-xs text-muted-foreground">还没有拉取过目录，先点「立即刷新」</p>
          ) : (
            <div className="space-y-3">
              <p className="text-xs text-muted-foreground">
                共 <span className="font-medium text-foreground tabular-nums">{status.count}</span> 个模型，
                更新于 {formatDateTime(status.fetchedAt)}，服务端每天自动刷新
              </p>
              <div className="flex flex-wrap gap-2">
                {status.sources.map((source) => (
                  <Badge key={source.source} variant={source.error ? "danger" : "outline"} size="sm" title={source.error || source.url}>
                    {source.source}
                    <span className="ml-1 tabular-nums opacity-70">{source.error ? "失败" : source.count}</span>
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </SheetBlock>

        <SheetBlock icon={<SlidersHorizontal className="size-4" />} title="同步内容">
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-3">
              <ToggleTile label="能力" hint="识图、工具、思考与强度、上下文、最大输出"
                checked={fields.includes("capabilities")} onChange={() => toggleField("capabilities")} />
              <ToggleTile label="名称与说明" hint={overwrite ? "覆盖已有内容" : "只填空着的"}
                checked={fields.includes("info")} onChange={() => toggleField("info")} />
              <ToggleTile label="价格" hint="美元单价按汇率换成点" checked={withPrices} onChange={() => toggleField("prices")} />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              {withPrices ? (
                <label className="flex items-center gap-2 text-xs">
                  <span className="shrink-0 text-muted-foreground">1 美元 =</span>
                  <Input className="h-9 font-mono text-xs" inputMode="decimal" placeholder="例如 720" value={rate}
                    aria-invalid={rate !== "" && !DECIMAL_RE.test(rate.trim())}
                    onChange={(event) => { setRate(event.target.value.trim()); setPreview(null); }} />
                  <span className="shrink-0 text-muted-foreground">点</span>
                </label>
              ) : null}
              <ToggleTile label="覆盖名称与说明" checked={overwrite} onChange={(value) => { setOverwrite(value); setPreview(null); }} />
            </div>
          </div>
        </SheetBlock>

        <SheetBlock
          icon={<BookOpenCheck className="size-4" />}
          title="预览"
          aside={
            <Button size="sm" variant="outline" onClick={() => void runPreview()} disabled={syncMutation.isPending || !status?.fetchedAt}>
              {syncMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Eye className="size-3.5" />}
              {preview ? "重新预览" : "预览变化"}
            </Button>
          }
        >
          {!preview ? (
            <p className="text-xs text-muted-foreground">
              将检查 {models.length} 个模型（能力只更新对话模型）。预览只读，不会改动任何模型
            </p>
          ) : preview.length === 0 ? (
            <p className="text-xs text-muted-foreground">没有模型</p>
          ) : (
            <div className="divide-y rounded-xl border">
              {preview.map((item) => (
                <PreviewRow
                  key={item.modelId}
                  appKey={appKey}
                  item={item}
                  picked={picked.has(item.modelId)}
                  onPick={(value) =>
                    setPicked((current) => {
                      const next = new Set(current);
                      if (value) next.add(item.modelId);
                      else next.delete(item.modelId);
                      return next;
                    })
                  }
                  searching={searching === item.modelId}
                  onSearch={(value) => setSearching(value ? item.modelId : null)}
                  onChoose={(key) => choose(item.modelId, key)}
                />
              ))}
            </div>
          )}
        </SheetBlock>
      </div>

      <div className="flex items-center justify-end gap-2 border-t bg-background px-6 py-3">
        <Button type="button" variant="outline" onClick={onClose}>
          取消
        </Button>
        <Button type="button" onClick={() => void apply()} disabled={!preview || picked.size === 0 || syncMutation.isPending}>
          {syncMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Check className="size-3.5" />}
          应用到 {picked.size} 个模型
        </Button>
      </div>
    </div>
  );
}

function PreviewRow({
  appKey,
  item,
  picked,
  onPick,
  searching,
  onSearch,
  onChoose
}: {
  appKey: string;
  item: ModelCatalogSyncResult;
  picked: boolean;
  onPick: (value: boolean) => void;
  searching: boolean;
  onSearch: (value: boolean) => void;
  onChoose: (key: string) => void;
}) {
  const match = item.match;
  const selectable = Boolean(match) && item.changes.length > 0;
  return (
    <div className="space-y-2 px-4 py-3">
      <div className="flex items-start gap-3">
        <Checkbox className="mt-0.5" checked={picked} disabled={!selectable} onCheckedChange={(value) => onPick(value === true)}
          aria-label={`同步 ${item.model}`} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-xs font-medium">{item.model}</span>
            {item.upstream !== item.model ? <span className="text-[11px] text-muted-foreground">上游 {item.upstream}</span> : null}
            {match ? (
              <>
                <span className="text-[11px] text-muted-foreground">→</span>
                <span className="font-mono text-[11px]">{match.entry.key}</span>
                <Badge variant={MATCH_LABEL[match.match].variant} size="sm">{MATCH_LABEL[match.match].label}</Badge>
              </>
            ) : null}
            <button type="button" className="ml-auto inline-flex items-center gap-1 text-[11px] text-primary hover:underline"
              onClick={() => onSearch(!searching)}>
              <Search className="size-3" />
              {searching ? "收起" : "改选条目"}
            </button>
          </div>
          {item.skipped && !match ? <p className="mt-1 text-[11px] text-muted-foreground">{item.skipped}</p> : null}
          {match && item.changes.length === 0 ? <p className="mt-1 text-[11px] text-muted-foreground">已是最新</p> : null}
          {item.changes.length > 0 ? (
            <div className="mt-1.5 grid gap-x-4 gap-y-0.5 text-[11px] sm:grid-cols-2">
              {item.changes.map((change) => (
                <div key={change.field} className="flex min-w-0 gap-1.5">
                  <span className="shrink-0 text-muted-foreground">{FIELD_LABEL[change.field] ?? change.field}</span>
                  <span className="truncate text-muted-foreground line-through decoration-muted-foreground/50">{showValue(change.from)}</span>
                  <span className="shrink-0 text-muted-foreground">→</span>
                  <span className="truncate font-medium">{showValue(change.to)}</span>
                </div>
              ))}
            </div>
          ) : null}
        </div>
      </div>
      {searching ? <EntryPicker appKey={appKey} seed={item.upstream} others={item.others ?? []} onChoose={onChoose} /> : null}
    </div>
  );
}

function EntryPicker({
  appKey,
  seed,
  others,
  onChoose
}: {
  appKey: string;
  seed: string;
  others: ModelCatalogCandidate[];
  onChoose: (key: string) => void;
}) {
  const [query, setQuery] = useState(seed);
  const searchQuery = useModelCatalogSearchQuery(appKey, query);
  const results: ModelCatalogEntry[] = query.trim().length >= 2 ? searchQuery.data?.items ?? [] : others.map((item) => item.entry);
  return (
    <div className="ml-7 space-y-2 rounded-xl bg-muted/40 p-3">
      <Input className="h-8 text-xs" placeholder="搜索目录：供应商或模型名" value={query} onChange={(event) => setQuery(event.target.value)} />
      <div className="max-h-56 space-y-1 overflow-y-auto">
        {searchQuery.isFetching ? <p className="px-1 text-[11px] text-muted-foreground">搜索中</p> : null}
        {results.length === 0 && !searchQuery.isFetching ? <p className="px-1 text-[11px] text-muted-foreground">没有结果</p> : null}
        {results.map((entry) => (
          <button
            key={entry.key}
            type="button"
            onClick={() => onChoose(entry.key)}
            className={cn("flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-background")}
          >
            <span className="min-w-0 flex-1 truncate font-mono text-[11px]">{entry.key}</span>
            {entry.name ? <span className="truncate text-[11px] text-muted-foreground">{entry.name}</span> : null}
            <span className="shrink-0 text-[10px] text-muted-foreground tabular-nums">
              {entry.contextLength ? `${Math.round(entry.contextLength / 1000)}K` : ""}
              {entry.cost?.input !== undefined ? ` $${entry.cost.input}/${entry.cost.output ?? 0}` : ""}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}
