"use client";

import { useMemo, useState } from "react";
import {
  CornerDownRight,
  Ellipsis,
  Globe,
  KeyRound,
  Layers,
  Lock,
  Pencil,
  Plus,
  RotateCcw,
  Stethoscope,
  Trash2,
  TriangleAlert,
  Unlink,
  Zap
} from "lucide-react";
import { toast } from "sonner";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ApiError } from "@/lib/api/client";
import type {
  StorageConfigOverview,
  StoragePool,
  StorageProbeResult,
  StorageScope,
  StorageSource
} from "@/lib/api/storage-center";
import { formatRelative } from "@/lib/datetime";
import {
  useClearStorageRedirectMutation,
  useDeleteStorageSourceMutation,
  useProbeStorageMutation,
  useResetStorageHealthMutation,
  useStorageProvidersQuery,
  useTestStorageSourceMutation
} from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";
import { StorageCloudflareWizard } from "./storage-cloudflare-wizard";
import { StorageCORSDialog, StorageProbeDialog } from "./storage-source-dialogs";
import { StorageSourceSheet } from "./storage-source-sheet";
import { CloudflareMark, StateBadge, StorageBrandBadge, UsageMeter, providerMeta } from "./storage-shared";

type SourceItem = {
  source: StorageSource;
  overview?: StorageConfigOverview;
};

/**
 * 存储源网格。平台级用总览数据（带用量、健康、能力、所在池），应用级只有配置本身。
 */
export function StorageSourcesPanel({
  scope,
  appid,
  sources,
  overviews,
  pools,
  loading
}: {
  scope: StorageScope;
  appid?: number | null;
  sources: StorageSource[];
  overviews?: StorageConfigOverview[];
  pools?: StoragePool[];
  loading: boolean;
}) {
  const providers = useStorageProvidersQuery().data;
  const [editing, setEditing] = useState<StorageSource | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [deleting, setDeleting] = useState<StorageSource | null>(null);
  const [probing, setProbing] = useState<{ name: string; result: StorageProbeResult | null } | null>(null);
  const [cors, setCors] = useState<{ id: number; name: string } | null>(null);
  const [cloudflare, setCloudflare] = useState<{ existing: StorageSource | null } | null>(null);

  const remove = useDeleteStorageSourceMutation(scope);
  const probe = useProbeStorageMutation();
  const test = useTestStorageSourceMutation(scope);
  const reset = useResetStorageHealthMutation();
  const clearRedirect = useClearStorageRedirectMutation();

  const items = useMemo<SourceItem[]>(() => {
    const byId = new Map((overviews ?? []).map((item) => [item.config.id, item]));
    return sources.map((source) => ({ source, overview: byId.get(source.id) }));
  }, [sources, overviews]);
  const nameOf = (id?: number | null) => sources.find((item) => item.id === id)?.config_name ?? `#${id}`;
  const poolNames = (ids: number[]) =>
    ids.map((id) => pools?.find((pool) => pool.id === id)?.name).filter((name): name is string => Boolean(name));

  async function runProbe(source: StorageSource) {
    if (scope === "app") {
      try {
        const result = await test.mutateAsync({ configId: source.id, appid });
        toast.success(result.message || "连通正常");
      } catch (error) {
        toast.error(error instanceof ApiError ? error.message : "连通失败");
      }
      return;
    }
    setProbing({ name: source.config_name, result: null });
    try {
      const result = await probe.mutateAsync(source.id);
      setProbing({ name: source.config_name, result });
    } catch (error) {
      setProbing(null);
      toast.error(error instanceof ApiError ? error.message : "检测失败");
    }
  }

  async function confirmDelete() {
    if (!deleting) return;
    try {
      await remove.mutateAsync({ configId: deleting.id, appid });
      toast.success("已删除");
      setDeleting(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  const openEditor = (source: StorageSource | null) => {
    setEditing(source);
    setSheetOpen(true);
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <span className="text-xs tabular-nums text-muted-foreground">{sources.length} 个存储源</span>
        <div className="flex items-center gap-2">
          {scope === "global" ? (
            <Button size="sm" variant="outline" onClick={() => setCloudflare({ existing: null })}>
              <CloudflareMark />
              Cloudflare 快速接入
            </Button>
          ) : null}
          <Button size="sm" onClick={() => openEditor(null)}>
            <Plus className="size-3.5" />
            添加存储
          </Button>
        </div>
      </div>

      {loading ? (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-44 rounded-2xl" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <button
          type="button"
          onClick={() => openEditor(null)}
          className="flex w-full flex-col items-center justify-center gap-2 rounded-2xl border border-dashed py-14 text-sm text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground"
        >
          <Plus className="size-5" />
          暂无存储源
        </button>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {items.map(({ source, overview }) => {
            const meta = providerMeta(providers, source.provider);
            const health = overview?.health;
            const backoff = overview?.state === "backoff" || overview?.state === "degraded";
            const caps = overview?.capabilities ?? meta?.capabilities;
            const inPools = poolNames(overview?.poolIds ?? []);
            return (
              <div
                key={source.id}
                className={cn(
                  "group flex flex-col rounded-2xl border bg-card transition-colors",
                  !source.enabled && "opacity-70",
                  overview?.state === "backoff" && "border-red-500/40"
                )}
              >
                <div className="flex items-start gap-3 p-4 pb-3">
                  <StorageBrandBadge meta={meta} provider={source.provider} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <button
                        type="button"
                        className="truncate text-sm font-semibold hover:underline"
                        onClick={() => openEditor(source)}
                      >
                        {source.config_name}
                      </button>
                      {source.is_default ? (
                        <Badge variant="info" size="sm">
                          默认
                        </Badge>
                      ) : null}
                    </div>
                    <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                      {meta?.name ?? source.provider}
                      <span>·</span>
                      {source.access_mode === "private" ? (
                        <span className="inline-flex items-center gap-0.5">
                          <Lock className="size-3" />
                          私有
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-0.5">
                          <Globe className="size-3" />
                          公开
                        </span>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-1">
                    {overview ? <StateBadge state={source.enabled ? overview.state : "disabled"} /> : null}
                    {!overview && !source.enabled ? <StateBadge state="disabled" /> : null}
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon-xs" className="text-muted-foreground">
                          <Ellipsis />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-40">
                        <DropdownMenuItem onClick={() => openEditor(source)}>
                          <Pencil />
                          编辑
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => runProbe(source)}>
                          <Stethoscope />
                          {scope === "global" ? "深度检测" : "连通测试"}
                        </DropdownMenuItem>
                        {scope === "global" && source.provider === "r2" ? (
                          <DropdownMenuItem onClick={() => setCloudflare({ existing: source })}>
                            <KeyRound />
                            更新凭据
                          </DropdownMenuItem>
                        ) : null}
                        {scope === "global" && caps?.cors ? (
                          <DropdownMenuItem onClick={() => setCors({ id: source.id, name: source.config_name })}>
                            <Zap />
                            配置 CORS
                          </DropdownMenuItem>
                        ) : null}
                        {backoff ? (
                          <DropdownMenuItem
                            onClick={async () => {
                              await reset.mutateAsync(source.id);
                              toast.success("已恢复写入");
                            }}
                          >
                            <RotateCcw />
                            恢复写入
                          </DropdownMenuItem>
                        ) : null}
                        {source.redirect_config_id ? (
                          <DropdownMenuItem
                            onClick={async () => {
                              await clearRedirect.mutateAsync(source.id);
                              toast.success("已撤销读取转向");
                            }}
                          >
                            <Unlink />
                            撤销转向
                          </DropdownMenuItem>
                        ) : null}
                        <DropdownMenuSeparator />
                        <DropdownMenuItem variant="destructive" onClick={() => setDeleting(source)}>
                          <Trash2 />
                          删除
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </div>

                <div className="flex-1 space-y-3 px-4 pb-3">
                  {overview?.usage ? (
                    <UsageMeter used={overview.usage.usedBytes} capacity={overview.capacityBytes} />
                  ) : null}
                  {overview && health && backoff ? (
                    <div className="flex items-start gap-1.5 rounded-lg bg-red-500/10 px-2.5 py-1.5 text-[11px] text-red-700 dark:text-red-300">
                      <TriangleAlert className="mt-0.5 size-3 shrink-0" />
                      <span className="min-w-0 flex-1 break-all">
                        {health.backoffUntil ? `${formatRelative(health.backoffUntil)}重试 · ` : ""}
                        {health.lastError ?? `连续失败 ${health.consecutiveFailures} 次`}
                      </span>
                    </div>
                  ) : null}
                  {source.redirect_config_id ? (
                    <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                      <CornerDownRight className="size-3" />
                      读取转向 {nameOf(source.redirect_config_id)}
                    </div>
                  ) : null}
                </div>

                <div className="flex flex-wrap items-center gap-1.5 border-t px-4 py-2.5 text-[11px] text-muted-foreground">
                  {inPools.map((name) => (
                    <Badge key={name} variant="outline" size="sm" className="gap-1">
                      <Layers className="size-3" />
                      {name}
                    </Badge>
                  ))}
                  {caps?.directUpload ? <Capability label="直传" /> : null}
                  {caps?.bucketUsage ? <Capability label="真实用量" /> : null}
                  {overview?.usage ? (
                    <span className="ml-auto tabular-nums">{overview.usage.objects.toLocaleString()} 个对象</span>
                  ) : null}
                  {health?.lastLatencyMs ? (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span className={cn("tabular-nums", !overview?.usage && "ml-auto")}>{health.lastLatencyMs} ms</span>
                      </TooltipTrigger>
                      <TooltipContent>最近一次写入或检测耗时</TooltipContent>
                    </Tooltip>
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>
      )}

      <StorageSourceSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        scope={scope}
        appid={appid}
        source={editing}
        onSaved={(saved, created) => {
          if (created && scope === "global") void runProbe(saved);
        }}
      />
      <StorageProbeDialog
        name={probing?.name ?? ""}
        result={probing?.result ?? null}
        pending={Boolean(probing) && probe.isPending}
        onOpenChange={(open) => !open && setProbing(null)}
      />
      <StorageCloudflareWizard
        open={Boolean(cloudflare)}
        existing={cloudflare?.existing}
        pools={pools ?? []}
        onOpenChange={(open) => !open && setCloudflare(null)}
      />
      <StorageCORSDialog configId={cors?.id ?? null} name={cors?.name ?? ""} onOpenChange={(open) => !open && setCors(null)} />
      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除 {deleting?.config_name}？已写入的对象将无法再读取。</AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={confirmDelete}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Capability({ label }: { label: string }) {
  return (
    <span className="inline-flex items-center rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
      {label}
    </span>
  );
}
