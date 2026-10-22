"use client";

import { useState } from "react";
import { CheckCircle2, Circle, Download, Layers, Loader2 } from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Switch } from "@/components/ui/switch";
import { useApplyRiskTemplateMutation, useRiskTemplatesQuery } from "@/lib/risk-hooks";
import type { RiskTemplateDetail } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { ScopeField, useRiskScope } from "./risk-scope";
import {
  ActionBadge, DIMENSION_COLORS, DimensionBadge, RowsSkeleton, SceneBadge, SectionTitle, useRiskCatalog,
} from "./risk-shared";

/**
 * 规则模板。
 *
 * 零规则时每次评估都是 0 分放行，大盘上一片正常 —— 管理员会把「根本没有防线」
 * 读成「没有风险」。模板一键装上一组可编辑的普通规则（与模板脱钩），
 * 重复安装按来源跳过已装过的那条，可以放心地多点几次。
 */
export function RiskTemplatesPanel() {
  const { appId } = useRiskScope();
  const query = useRiskTemplatesQuery(appId);
  const [preview, setPreview] = useState<RiskTemplateDetail | null>(null);
  const [installing, setInstalling] = useState<RiskTemplateDetail | null>(null);

  const templates = query.data ?? [];

  if (query.isLoading) return <RowsSkeleton rows={2} height={180} />;
  if (templates.length === 0) return <EmptyState title="暂无规则模板" />;

  return (
    <>
      <div className="grid gap-3 md:grid-cols-2">
        {templates.map((tpl) => (
          <TemplateCard key={tpl.key} template={tpl}
            onPreview={() => setPreview(tpl)} onInstall={() => setInstalling(tpl)} />
        ))}
      </div>

      {preview && (
        <TemplatePreviewDialog template={preview} onClose={() => setPreview(null)}
          onInstall={() => { setInstalling(preview); setPreview(null); }} />
      )}
      {installing && <TemplateInstallDialog template={installing} onClose={() => setInstalling(null)} />}
    </>
  );
}

function TemplateCard({ template, onPreview, onInstall }: {
  template: RiskTemplateDetail; onPreview: () => void; onInstall: () => void;
}) {
  const { sceneLabel } = useRiskCatalog();
  const total = template.rules || (template.items ?? []).length;
  const installed = template.installed ?? 0;
  const done = total > 0 && installed >= total;
  const scenes = template.scenes ?? [];

  return (
    <Card className={cn(done && "border-emerald-300/60 dark:border-emerald-900/60")}>
      <CardContent className="flex h-full flex-col gap-3 p-4">
        <div className="flex items-start gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <Layers className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <h3 className="text-sm font-semibold">{template.name}</h3>
              {done && <Badge variant="success" size="sm">已安装</Badge>}
            </div>
            <p className="mt-0.5 text-xs text-muted-foreground">{template.description}</p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-1">
          {(template.dimensions ?? []).map((dim) => <DimensionBadge key={dim} dimension={dim} />)}
        </div>
        <p className="truncate text-[11px] text-muted-foreground" title={scenes.map(sceneLabel).join("、")}>
          {scenes.map(sceneLabel).join("、")}
        </p>

        <div className="mt-auto space-y-1.5">
          <div className="flex items-center justify-between text-[11px] text-muted-foreground">
            <span>{template.rules} 条规则{template.actions > 0 ? `，${template.actions} 条策略` : ""}</span>
            <span className="tabular-nums">已装 {installed} / {total}</span>
          </div>
          <Progress value={total > 0 ? (installed / total) * 100 : 0} className="h-1.5" />
        </div>

        <div className="flex gap-1.5">
          <Button size="sm" variant="outline" className="h-7 flex-1 text-xs" onClick={onPreview}>预览</Button>
          <Button size="sm" className="h-7 flex-1 text-xs" disabled={done} onClick={onInstall}>
            <Download className="size-3.5" />{installed > 0 && !done ? "补装" : "安装"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function TemplatePreviewDialog({ template, onClose, onInstall }: {
  template: RiskTemplateDetail; onClose: () => void; onInstall: () => void;
}) {
  const { conditionLabel, actionLabel } = useRiskCatalog();
  const items = template.items ?? [];
  const actionItems = template.actionItems ?? [];
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{template.name}</DialogTitle>
          <DialogDescription>{template.description}</DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-[60vh] pr-3">
          <div className="space-y-4">
            <section className="space-y-1.5">
              <SectionTitle>规则</SectionTitle>
              {items.map((item) => (
                <div key={item.key} className={cn("flex items-start gap-2 rounded-lg border px-2.5 py-2 text-xs",
                  item.installed && "bg-muted/30")}>
                  {item.installed
                    ? <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-emerald-500" />
                    : <Circle className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />}
                  <span className="mt-1 h-3 w-0.5 shrink-0 rounded-full"
                    style={{ background: DIMENSION_COLORS[item.dimension]?.light ?? "#a1a1aa" }} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="font-medium">{item.name}</span>
                      <SceneBadge scene={item.scene} />
                      <Badge variant="outline" size="sm">{conditionLabel(item.conditionType)}</Badge>
                      {item.decision && <Badge variant="danger" size="sm">命中即{actionLabel(item.decision)}</Badge>}
                    </div>
                    {item.explanation && <p className="mt-0.5 text-[11px] text-muted-foreground">{item.explanation}</p>}
                  </div>
                  <span className="shrink-0 font-mono font-semibold tabular-nums text-amber-700 dark:text-amber-400">
                    +{item.score}
                  </span>
                </div>
              ))}
            </section>
            {actionItems.length > 0 && (
              <section className="space-y-1.5">
                <SectionTitle>附带处置策略</SectionTitle>
                {actionItems.map((action, index) => (
                  <div key={index} className="flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs">
                    <SceneBadge scene={action.scene} />
                    <Badge variant="outline" size="sm" className="font-mono">
                      {action.minScore} – {action.maxScore ?? "∞"} 分
                    </Badge>
                    <ActionBadge action={action.action} />
                    {action.description && <span className="truncate text-muted-foreground">{action.description}</span>}
                  </div>
                ))}
              </section>
            )}
          </div>
        </ScrollArea>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>关闭</Button>
          <Button size="sm" onClick={onInstall}><Download className="size-3.5" />安装</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TemplateInstallDialog({ template, onClose }: { template: RiskTemplateDetail; onClose: () => void }) {
  const { appId } = useRiskScope();
  const applyMut = useApplyRiskTemplateMutation();
  const [scope, setScope] = useState<number | null>(appId ?? null);
  const [observe, setObserve] = useState(true);
  const [withActions, setWithActions] = useState((template.actionItems ?? []).length > 0);

  const submit = async () => {
    try {
      const result = await applyMut.mutateAsync({ key: template.key, appId: scope, observe, withActions });
      const skipped = (result.actionsSkipped ?? []).length;
      toast.success(`新增规则 ${result.rulesCreated} 条${result.rulesSkipped ? `，跳过 ${result.rulesSkipped} 条` : ""}`
        + (withActions ? `；新增策略 ${result.actionsCreated} 条${skipped ? `，跳过 ${skipped} 条` : ""}` : ""));
      if (skipped > 0) {
        toast.warning((result.actionsSkipped ?? []).join("；"));
      }
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "安装失败");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>安装「{template.name}」</DialogTitle>
          <DialogDescription>{template.rules} 条规则</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label className="text-xs">作用域</Label>
            <ScopeField value={scope} onChange={setScope} />
          </div>
          <label className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
            <span className="text-xs font-medium">以观察模式安装</span>
            <Switch checked={observe} onCheckedChange={setObserve} />
          </label>
          {(template.actionItems ?? []).length > 0 && (
            <label className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
              <span className="text-xs font-medium">附带处置策略</span>
              <Switch checked={withActions} onCheckedChange={setWithActions} />
            </label>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>取消</Button>
          <Button size="sm" disabled={applyMut.isPending} onClick={submit}>
            {applyMut.isPending && <Loader2 className="size-3.5 animate-spin" />}安装
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
