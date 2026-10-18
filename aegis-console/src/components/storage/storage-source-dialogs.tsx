"use client";

import { useState } from "react";
import { CircleCheck, CircleMinus, CircleX, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { StorageProbeResult } from "@/lib/api/storage-center";
import { useSetStorageCORSMutation, useStorageCORSQuery } from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";
import { formatSize } from "./storage-shared";

/** 深度检测结果：连通 → 写入 → 读回校验 → 删除，逐步计时。 */
export function StorageProbeDialog({
  name,
  result,
  pending,
  onOpenChange
}: {
  name: string;
  result: StorageProbeResult | null;
  pending: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const open = pending || Boolean(result);
  const total = result?.steps.reduce((sum, step) => sum + step.latencyMs, 0) ?? 0;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            检测 {name}
            {result ? (
              <span
                className={cn(
                  "ml-auto text-xs font-medium",
                  result.ok ? "text-emerald-600 dark:text-emerald-400" : "text-destructive"
                )}
              >
                {result.ok ? `通过 · ${total} ms` : "未通过"}
              </span>
            ) : null}
          </DialogTitle>
        </DialogHeader>
        {pending && !result ? (
          <div className="space-y-2">
            {["连通", "写入", "读回校验", "删除"].map((step) => (
              <div key={step} className="flex items-center gap-3 rounded-xl bg-muted/60 px-3 py-2.5">
                <Loader2 className="size-4 animate-spin text-muted-foreground" />
                <span className="text-sm">{step}</span>
                <Skeleton className="ml-auto h-3 w-10" />
              </div>
            ))}
          </div>
        ) : result ? (
          <div className="space-y-2">
            {result.steps.map((step) => {
              const Icon = step.skipped ? CircleMinus : step.ok ? CircleCheck : CircleX;
              return (
                <div
                  key={step.name}
                  className={cn(
                    "rounded-xl px-3 py-2.5",
                    step.ok || step.skipped ? "bg-muted/60" : "bg-destructive/10 ring-1 ring-inset ring-destructive/20"
                  )}
                >
                  <div className="flex items-center gap-3">
                    <Icon
                      className={cn(
                        "size-4 shrink-0",
                        step.skipped ? "text-muted-foreground" : step.ok ? "text-emerald-500" : "text-destructive"
                      )}
                    />
                    <span className="text-sm font-medium">{step.name}</span>
                    {!step.skipped ? (
                      <span className="ml-auto font-mono text-xs tabular-nums text-muted-foreground">{step.latencyMs} ms</span>
                    ) : null}
                  </div>
                  {step.message ? (
                    <div className="mt-1 break-all pl-7 font-mono text-[11px] text-muted-foreground">{step.message}</div>
                  ) : null}
                </div>
              );
            })}
            {result.usage ? (
              <div className="flex items-center justify-between rounded-xl border px-3 py-2 text-xs">
                <span className="text-muted-foreground">桶用量</span>
                <span className="font-mono tabular-nums">
                  {formatSize(result.usage.bytes)} · {result.usage.objects} 个对象
                </span>
              </div>
            ) : null}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

/** 存储桶 CORS：浏览器直传需要允许控制台来源 PUT。 */
export function StorageCORSDialog({
  configId,
  name,
  onOpenChange
}: {
  configId: number | null;
  name: string;
  onOpenChange: (open: boolean) => void;
}) {
  const query = useStorageCORSQuery(configId);
  const setCORS = useSetStorageCORSMutation();
  const [draft, setDraft] = useState<{ id: number | null; value: string } | null>(null);
  const defaultOrigins = typeof window === "undefined" ? "" : window.location.origin;
  const current = (query.data ?? []).flatMap((rule) => rule.allowedOrigins);
  const value =
    draft?.id === configId ? draft.value : (current.length > 0 ? current : [defaultOrigins]).filter(Boolean).join("\n");

  async function submit() {
    if (!configId) return;
    const origins = value
      .split(/[\n,]/)
      .map((item) => item.trim())
      .filter(Boolean);
    try {
      await setCORS.mutateAsync({ configId, origins });
      toast.success("CORS 规则已写入");
      setDraft(null);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "写入失败");
    }
  }

  return (
    <Dialog
      open={configId !== null}
      onOpenChange={(open) => {
        if (!open) setDraft(null);
        onOpenChange(open);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-base">CORS · {name}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          {query.isLoading ? (
            <Skeleton className="h-24 w-full rounded-xl" />
          ) : query.isError ? (
            <div className="rounded-xl bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {query.error instanceof ApiError ? query.error.message : "读取失败"}
            </div>
          ) : current.length > 0 ? (
            <div className="space-y-1 rounded-xl bg-muted/60 px-3 py-2 text-xs">
              <div className="text-muted-foreground">当前允许</div>
              {current.map((origin) => (
                <div key={origin} className="font-mono">
                  {origin}
                </div>
              ))}
            </div>
          ) : (
            <div className="rounded-xl bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">未配置 CORS</div>
          )}
          <div className="space-y-1.5">
            <Label className="text-xs">允许的来源</Label>
            <Textarea
              rows={4}
              className="font-mono text-xs"
              value={value}
              onChange={(e) => setDraft({ id: configId, value: e.target.value })}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={submit} disabled={setCORS.isPending}>
            {setCORS.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
            写入
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
