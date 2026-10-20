"use client";

import { useState } from "react";
import { Boxes, Crown, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { AIGatewayModelSheet, seedModelDraft, type ModelDraft } from "@/components/apps/ai-gateway/ai-gateway-model-sheet";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useAIGatewayCatalogQuery, useAIGatewayModelsQuery, useDeleteAIGatewayModelMutation } from "@/lib/ai-gateway-hooks";
import { useAIChannelQuery } from "@/lib/ai-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewayKindMeta, AIGatewayModel, AIGatewayPriceKey } from "@/lib/api/ai-gateway";

const ALL_PRICES: AIGatewayPriceKey[] = ["inputPrice", "outputPrice", "requestPrice", "imagePrice", "charPrice", "minutePrice"];

/** 列表里的一句话价格：只列这一种类会用到、且不为 0 的价格项。 */
function priceSummary(model: AIGatewayModel, kinds: AIGatewayKindMeta[]) {
  if (model.free) return "免费";
  const keys = kinds.find((item) => item.key === model.kind)?.prices ?? ALL_PRICES;
  const parts = keys
    .filter((key) => Number(model[key]) > 0)
    .map((key) => `${PRICE_LABEL[key].label} ${formatPoints(model[key])}`);
  const tiers = Object.keys(model.imagePrices ?? {}).length;
  if (tiers > 0) parts.push(`${tiers} 档分档价`);
  return parts.join(" · ") || "—";
}

/** 模型与价格：列表在这里，新建与编辑在大号侧栏（ai-gateway-model-sheet.tsx）。 */
export function AIGatewayModelsPanel({ appKey }: { appKey: string }) {
  const catalogQuery = useAIGatewayCatalogQuery(appKey);
  const modelsQuery = useAIGatewayModelsQuery(appKey);
  const channelQuery = useAIChannelQuery({ kind: "app", appKey });
  const deleteMutation = useDeleteAIGatewayModelMutation(appKey);
  const [editing, setEditing] = useState<ModelDraft | null>(null);
  const [deleting, setDeleting] = useState<AIGatewayModel | null>(null);

  const kinds = catalogQuery.data?.kinds ?? [];
  const models = modelsQuery.data?.items ?? [];
  const channels = channelQuery.data ?? [];
  const kindLabel = (key: string) => kinds.find((item) => item.key === key)?.label ?? key;

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
          <Button size="sm" onClick={() => setEditing(seedModelDraft())}>
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
                <TableRow key={model.id} className="cursor-pointer" onClick={() => setEditing(seedModelDraft(model))}>
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
                  <TableCell className="text-right" onClick={(event) => event.stopPropagation()}>
                    <Button size="icon" variant="ghost" className="size-7" aria-label="编辑"
                      onClick={() => setEditing(seedModelDraft(model))}>
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

      <AIGatewayModelSheet appKey={appKey} kinds={kinds} existing={models} initial={editing} onClose={() => setEditing(null)} />

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
