"use client";

import { useState } from "react";
import { Boxes, Crown, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField, SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
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
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  useAIGatewayCatalogQuery,
  useAIGatewayModelsQuery,
  useDeleteAIGatewayModelMutation,
  useSaveAIGatewayModelMutation
} from "@/lib/ai-gateway-hooks";
import { useAIChannelQuery } from "@/lib/ai-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewayKindMeta, AIGatewayModel, AIGatewayModelPayload, AIGatewayPriceKey } from "@/lib/api/ai-gateway";

/** 与后端 aigateway.ValidModelName 同一条规则。 */
const MODEL_RE = /^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$/;
/** 与后端图片分档键同一条规则：尺寸、质量或「质量:尺寸」。 */
const IMAGE_KEY_RE = /^([a-z0-9]{1,16}:)?[a-z0-9x]{1,16}$/;
const ALL_PRICES: AIGatewayPriceKey[] = ["inputPrice", "outputPrice", "requestPrice", "imagePrice", "charPrice", "minutePrice"];
const CHAIN = "__chain__";

type TierDraft = { uid: string; key: string; price: string };

/** 数字一律按字符串存，理由同云存储配置：存 number 的话输入框清不空。 */
type ModelDraft = {
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

function seedModel(model?: AIGatewayModel, kind = "chat"): ModelDraft {
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

function trimZero(value?: string) {
  if (!value || Number(value) === 0) return "";
  return value.includes(".") ? value.replace(/0+$/, "").replace(/\.$/, "") : value;
}

/** 列表里的一句话价格：只列这一种类会用到、且不为 0 的价格项。 */
function priceSummary(model: AIGatewayModel, kinds: AIGatewayKindMeta[]) {
  if (model.free) return "免费";
  const meta = kinds.find((item) => item.key === model.kind);
  const keys = meta?.prices ?? ALL_PRICES;
  const parts = keys
    .filter((key) => Number(model[key]) > 0)
    .map((key) => `${PRICE_LABEL[key].label} ${formatPoints(model[key])}`);
  const tiers = Object.keys(model.imagePrices ?? {}).length;
  if (tiers > 0) parts.push(`${tiers} 档分档价`);
  return parts.join(" · ") || "—";
}

export function AIGatewayModelsPanel({ appKey }: { appKey: string }) {
  const catalogQuery = useAIGatewayCatalogQuery(appKey);
  const modelsQuery = useAIGatewayModelsQuery(appKey);
  const channelQuery = useAIChannelQuery({ kind: "app", appKey });
  const saveMutation = useSaveAIGatewayModelMutation(appKey);
  const deleteMutation = useDeleteAIGatewayModelMutation(appKey);
  const [editing, setEditing] = useState<ModelDraft | null>(null);
  const [deleting, setDeleting] = useState<AIGatewayModel | null>(null);

  const kinds = catalogQuery.data?.kinds ?? [];
  const models = modelsQuery.data?.items ?? [];
  const channels = channelQuery.data ?? [];
  const kindLabel = (key: string) => kinds.find((item) => item.key === key)?.label ?? key;

  const save = async () => {
    if (!editing) return;
    const draft = editing;
    if (!MODEL_RE.test(draft.model.trim())) {
      toast.error("模型名只能包含字母、数字与 . _ : / @ + -");
      return;
    }
    if (draft.upstreamModel.trim() && !MODEL_RE.test(draft.upstreamModel.trim())) {
      toast.error("上游模型名格式无效");
      return;
    }
    const meta = kinds.find((item) => item.key === draft.kind);
    const used = new Set(meta?.prices ?? ALL_PRICES);
    const prices = Object.fromEntries(
      ALL_PRICES.map((key) => [key, used.has(key) && draft.prices[key].trim() ? draft.prices[key].trim() : "0"])
    ) as Record<AIGatewayPriceKey, string>;
    for (const key of ALL_PRICES) {
      if (!/^\d+(\.\d{1,6})?$/.test(prices[key])) {
        toast.error(`${PRICE_LABEL[key].label}价格需为非负数，最多 6 位小数`);
        return;
      }
    }
    const imagePrices: Record<string, string> = {};
    if (draft.kind === "image") {
      for (const tier of draft.tiers) {
        const key = tier.key.trim().toLowerCase();
        if (!IMAGE_KEY_RE.test(key) || !/^\d+(\.\d{1,6})?$/.test(tier.price.trim())) {
          toast.error(`分档「${tier.key || "未填写"}」无效`);
          return;
        }
        imagePrices[key] = tier.price.trim();
      }
    }
    const hasPrice = Object.values(prices).some((value) => Number(value) > 0) ||
      Object.values(imagePrices).some((value) => Number(value) > 0);
    if (!draft.free && !hasPrice) {
      toast.error("没有设置价格，免费提供请打开「免费」");
      return;
    }
    const payload: AIGatewayModelPayload = {
      model: draft.model.trim(),
      upstreamModel: draft.upstreamModel.trim(),
      kind: draft.kind,
      displayName: draft.displayName.trim(),
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
    try {
      await saveMutation.mutateAsync({ id: draft.id, payload });
      toast.success(draft.id ? "模型已保存" : "模型已添加");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const remove = async () => {
    if (!deleting) return;
    try {
      await deleteMutation.mutateAsync(deleting.id);
      toast.success("模型已删除");
      setDeleting(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  if (modelsQuery.isLoading || catalogQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }

  return (
    <>
      <SectionCard
        icon={<Boxes className="size-4" />}
        title="模型与价格"
        aside={
          <Button size="sm" onClick={() => setEditing(seedModel())}>
            <Plus className="size-3.5" />
            添加模型
          </Button>
        }
      >
        {models.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-10 text-center text-xs text-muted-foreground">
            暂无模型
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>模型</TableHead>
                <TableHead>种类</TableHead>
                <TableHead>通道</TableHead>
                <TableHead>价格（点）</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="w-20" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {models.map((model) => (
                <TableRow key={model.id}>
                  <TableCell>
                    <div className="flex items-center gap-1.5 font-mono text-xs font-medium">
                      {model.model}
                      {model.vipOnly ? <Crown className="size-3 text-amber-500" aria-label="会员专享" /> : null}
                    </div>
                    {model.upstreamModel || model.displayName ? (
                      <div className="mt-0.5 text-[11px] text-muted-foreground">
                        {model.displayName}
                        {model.displayName && model.upstreamModel ? " · " : ""}
                        {model.upstreamModel ? `上游 ${model.upstreamModel}` : ""}
                      </div>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-xs">{kindLabel(model.kind)}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {model.configId
                      ? channels.find((item) => item.configId === model.configId)?.configName ?? `#${model.configId}`
                      : "按链路"}
                  </TableCell>
                  <TableCell className="text-xs tabular-nums">{priceSummary(model, kinds)}</TableCell>
                  <TableCell>
                    <Badge variant={model.enabled ? "success" : "outline"} size="sm">
                      {model.enabled ? "开放" : "停用"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button size="icon" variant="ghost" className="size-7" aria-label="编辑"
                      onClick={() => setEditing(seedModel(model))}>
                      <Pencil className="size-3.5" />
                    </Button>
                    <Button size="icon" variant="ghost" className="size-7 text-muted-foreground hover:text-destructive"
                      aria-label="删除" onClick={() => setDeleting(model)}>
                      <Trash2 className="size-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </SectionCard>

      <Dialog open={Boolean(editing)} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editing?.id ? "编辑模型" : "添加模型"}</DialogTitle>
          </DialogHeader>
          {editing ? (
            <ModelEditor
              draft={editing}
              kinds={kinds}
              channels={channels.map((item) => ({ id: item.configId, name: item.configName, protocol: item.protocol }))}
              onChange={setEditing}
            />
          ) : null}
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setEditing(null)}>
              取消
            </Button>
            <Button size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除模型「{deleting?.model}」后，用户调用它会返回 404</AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => void remove()}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function ModelEditor({
  draft,
  kinds,
  channels,
  onChange
}: {
  draft: ModelDraft;
  kinds: AIGatewayKindMeta[];
  channels: Array<{ id: number; name: string; protocol: string }>;
  onChange: (draft: ModelDraft) => void;
}) {
  const patch = (changes: Partial<ModelDraft>) => onChange({ ...draft, ...changes });
  const meta = kinds.find((item) => item.key === draft.kind);
  // 对话以外的接口只能走 OpenAI 兼容协议的通道（原样转发），可选通道随种类收窄。
  const usableChannels = draft.kind === "chat" ? channels : channels.filter((item) => item.protocol === "openai");

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2">
        <FieldGroup label="模型名">
          <Input className="h-8 font-mono text-xs" value={draft.model} placeholder="gpt-4o" maxLength={128}
            onChange={(event) => patch({ model: event.target.value.trim() })} />
        </FieldGroup>
        <FieldGroup label="上游模型名" hint="留空与模型名相同">
          <Input className="h-8 font-mono text-xs" value={draft.upstreamModel} placeholder={draft.model || "openai/gpt-4o"}
            maxLength={128} onChange={(event) => patch({ upstreamModel: event.target.value.trim() })} />
        </FieldGroup>
        <FieldGroup label="种类">
          <Select value={draft.kind} onValueChange={(value) => patch({ kind: value, configId: 0 })}>
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {kinds.map((kind) => (
                <SelectItem key={kind.key} value={kind.key}>
                  {kind.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldGroup>
        <FieldGroup label="通道">
          <Select value={draft.configId ? String(draft.configId) : CHAIN}
            onValueChange={(value) => patch({ configId: value === CHAIN ? 0 : Number(value) })}>
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={CHAIN}>按链路（自动回退）</SelectItem>
              {usableChannels.map((channel) => (
                <SelectItem key={channel.id} value={String(channel.id)}>
                  {channel.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldGroup>
        <FieldGroup label="显示名">
          <Input className="h-8 text-xs" value={draft.displayName} maxLength={128}
            onChange={(event) => patch({ displayName: event.target.value })} />
        </FieldGroup>
        <NumberField label="排序" value={draft.sortOrder} onChange={(value) => patch({ sortOrder: value })} />
      </div>

      <FieldGroup label="说明">
        <Input className="h-8 text-xs" value={draft.description} maxLength={500}
          onChange={(event) => patch({ description: event.target.value })} />
      </FieldGroup>

      <div className="space-y-2">
        <SwitchRow label="免费" checked={draft.free} onChange={(value) => patch({ free: value })} />
        {!draft.free ? (
          <div className="grid gap-4 sm:grid-cols-2">
            {(meta?.prices ?? ALL_PRICES).map((key) => (
              <NumberField
                key={key}
                label={PRICE_LABEL[key].label}
                unit={PRICE_LABEL[key].unit}
                min={0}
                placeholder="0"
                value={draft.prices[key]}
                onChange={(value) => patch({ prices: { ...draft.prices, [key]: value } })}
              />
            ))}
          </div>
        ) : null}
      </div>

      {draft.kind === "image" && !draft.free ? (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium">分档价格</span>
            <Button size="xs" variant="outline"
              onClick={() => patch({ tiers: [...draft.tiers, { uid: `tier-${++tierSeed}`, key: "", price: "" }] })}>
              <Plus className="size-3" />
              添加
            </Button>
          </div>
          {draft.tiers.map((tier) => (
            <div key={tier.uid} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] gap-2">
              <Input className="h-8 font-mono text-xs" value={tier.key} placeholder="hd:1024x1792"
                onChange={(event) =>
                  patch({ tiers: draft.tiers.map((item) => (item.uid === tier.uid ? { ...item, key: event.target.value } : item)) })
                } />
              <Input className="h-8 text-xs" value={tier.price} placeholder="点 / 张" inputMode="decimal"
                onChange={(event) =>
                  patch({ tiers: draft.tiers.map((item) => (item.uid === tier.uid ? { ...item, price: event.target.value } : item)) })
                } />
              <Button size="icon" variant="ghost" className="size-8 text-muted-foreground hover:text-destructive"
                aria-label="删除分档" onClick={() => patch({ tiers: draft.tiers.filter((item) => item.uid !== tier.uid) })}>
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          ))}
        </div>
      ) : null}

      {draft.kind === "chat" ? (
        <NumberField label="输出上限" unit="token" min={0} placeholder="不限" value={draft.maxOutputTokens}
          onChange={(value) => patch({ maxOutputTokens: value })} />
      ) : null}

      <div className="space-y-2">
        <SwitchRow label="会员专享" checked={draft.vipOnly} onChange={(value) => patch({ vipOnly: value })} />
        <SwitchRow label="开放" checked={draft.enabled} onChange={(value) => patch({ enabled: value })} />
      </div>
    </div>
  );
}
