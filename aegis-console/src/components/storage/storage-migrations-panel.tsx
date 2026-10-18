"use client";

import { useState } from "react";
import {
  ArrowDown,
  ArrowRight,
  Check,
  CirclePause,
  CirclePlay,
  CircleX,
  CornerDownRight,
  Loader2,
  Plus,
  Truck,
  Unlink
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
import { Checkbox } from "@/components/ui/checkbox";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type {
  StorageConfigOverview,
  StorageMigration,
  StorageMigrationStatus,
  StoragePool,
  StorageProviderMeta,
  StorageSource
} from "@/lib/api/storage-center";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import {
  useClearStorageRedirectMutation,
  useControlStorageMigrationMutation,
  useCreateStorageMigrationMutation,
  useStorageMigrationsQuery,
  useStorageProvidersQuery
} from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";
import { StorageCloudflareWizard } from "./storage-cloudflare-wizard";
import { CloudflareMark, StateBadge, StorageBrandBadge, formatSize, providerMeta } from "./storage-shared";

const STATUS_META: Record<StorageMigrationStatus, { label: string; variant: "success" | "warning" | "danger" | "info" | "outline" }> = {
  pending: { label: "排队中", variant: "info" },
  running: { label: "迁移中", variant: "info" },
  paused: { label: "已暂停", variant: "warning" },
  completed: { label: "已完成", variant: "success" },
  failed: { label: "已失败", variant: "danger" },
  cancelled: { label: "已取消", variant: "outline" }
};

type PickItem = {
  source: StorageSource;
  overview?: StorageConfigOverview;
  /** 不可选时的原因 */
  blocked?: string;
};

export function StorageMigrationsPanel({
  sources,
  overviews,
  pools,
  onAddSource
}: {
  sources: StorageSource[];
  overviews: StorageConfigOverview[];
  pools: StoragePool[];
  onAddSource: () => void;
}) {
  const providers = useStorageProvidersQuery().data;
  const migrations = useStorageMigrationsQuery();
  const create = useCreateStorageMigrationMutation();
  const control = useControlStorageMigrationMutation();
  const clearRedirect = useClearStorageRedirectMutation();
  const [sourceId, setSourceId] = useState<number | null>(null);
  const [targetId, setTargetId] = useState<number | null>(null);
  const [setRedirect, setSetRedirect] = useState(true);
  const [deleteSource, setDeleteSource] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [cloudflareOpen, setCloudflareOpen] = useState(false);

  const items = migrations.data ?? [];
  const overviewOf = (id: number) => overviews.find((item) => item.config.id === id);
  const busySources = new Set(
    items.filter((item) => ["pending", "running", "paused"].includes(item.status)).map((item) => item.sourceConfigId)
  );
  const sourceItems: PickItem[] = sources.map((source) => ({
    source,
    overview: overviewOf(source.id),
    blocked: busySources.has(source.id) ? "迁移进行中" : (overviewOf(source.id)?.usage?.objects ?? 0) === 0 ? "没有对象" : undefined
  }));
  const targetItems: PickItem[] = sources
    .filter((source) => source.id !== sourceId)
    .map((source) => ({
      source,
      overview: overviewOf(source.id),
      blocked: !source.enabled ? "已停用" : source.scope !== "global" ? "应用级存储" : undefined
    }));
  const sourceConfig = sources.find((item) => item.id === sourceId);
  const targetConfig = sources.find((item) => item.id === targetId);
  const sourceUsage = sourceId ? overviewOf(sourceId)?.usage : undefined;
  const ready = Boolean(sourceConfig && targetConfig && sourceId !== targetId);
  const redirected = sources.filter((item) => item.redirect_config_id);
  const nameOf = (id?: number | null) => sources.find((item) => item.id === id)?.config_name ?? `#${id}`;

  async function start() {
    if (!sourceId || !targetId) return;
    try {
      await create.mutateAsync({
        sourceConfigId: sourceId,
        targetConfigId: targetId,
        setRedirect,
        deleteSource: setRedirect && deleteSource
      });
      toast.success("迁移已开始");
      setConfirming(false);
      setSourceId(null);
      setTargetId(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "创建失败");
    }
  }

  async function act(migration: StorageMigration, action: "pause" | "resume" | "cancel") {
    try {
      await control.mutateAsync({ id: migration.id, action });
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  return (
    <div className="space-y-5">
      <section className="overflow-hidden rounded-2xl border bg-card">
        <header className="flex items-center gap-2 border-b px-4 py-3 text-sm font-semibold">
          <Truck className="size-4 text-muted-foreground" />
          新建迁移
        </header>

        <div className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
          <PickColumn
            title="从"
            items={sourceItems}
            selected={sourceId}
            providers={providers}
            onSelect={(id) => {
              setSourceId(id);
              if (id === targetId) setTargetId(null);
            }}
          />
          <div className="flex items-center justify-center text-muted-foreground">
            <ArrowRight className="hidden size-5 lg:block" />
            <ArrowDown className="size-5 lg:hidden" />
          </div>
          <PickColumn
            title="到"
            items={targetItems}
            selected={targetId}
            providers={providers}
            onSelect={setTargetId}
            empty={
              <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-8 text-center">
                <span className="text-xs text-muted-foreground">没有可迁移到的存储</span>
                <div className="flex flex-wrap justify-center gap-2">
                  <Button size="sm" variant="outline" onClick={() => setCloudflareOpen(true)}>
                    <CloudflareMark />
                    Cloudflare 快速接入
                  </Button>
                  <Button size="sm" variant="ghost" onClick={onAddSource}>
                    <Plus className="size-3.5" />
                    添加存储
                  </Button>
                </div>
              </div>
            }
          />
        </div>

        <footer className="flex flex-wrap items-center gap-x-5 gap-y-3 border-t bg-muted/30 px-4 py-3">
          <label className="flex cursor-pointer items-center gap-2 text-xs">
            <Checkbox
              checked={setRedirect}
              onCheckedChange={(value) => {
                const next = value === true;
                setSetRedirect(next);
                if (!next) setDeleteSource(false);
              }}
            />
            迁移后读取转向目标
          </label>
          <label className={cn("flex cursor-pointer items-center gap-2 text-xs", !setRedirect && "cursor-not-allowed opacity-50")}>
            <Checkbox checked={deleteSource} disabled={!setRedirect} onCheckedChange={(value) => setDeleteSource(value === true)} />
            删除源对象
          </label>
          <div className="ml-auto flex items-center gap-3">
            {sourceUsage && sourceConfig ? (
              <span className="text-xs tabular-nums text-muted-foreground">
                {sourceUsage.objects.toLocaleString()} 个对象 · {formatSize(sourceUsage.usedBytes)}
              </span>
            ) : null}
            <Button disabled={!ready || create.isPending} onClick={() => setConfirming(true)}>
              {create.isPending ? <Loader2 className="size-4 animate-spin" /> : <Truck className="size-4" />}
              开始迁移
            </Button>
          </div>
        </footer>
      </section>

      <section className="space-y-2">
        <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">迁移任务</div>
        {migrations.isLoading ? (
          <Skeleton className="h-28 rounded-2xl" />
        ) : items.length === 0 ? (
          <div className="rounded-2xl border border-dashed bg-card py-10 text-center text-xs text-muted-foreground">暂无迁移任务</div>
        ) : (
          <div className="space-y-2">
            {items.map((migration) => {
              const done = migration.copiedObjects + migration.skippedObjects + migration.failedObjects;
              const percent =
                migration.totalObjects > 0
                  ? Math.min(100, (done / migration.totalObjects) * 100)
                  : migration.status === "completed"
                    ? 100
                    : 0;
              const meta = STATUS_META[migration.status];
              const active = migration.status === "running" || migration.status === "pending";
              return (
                <div key={migration.id} className="space-y-3 rounded-2xl border bg-card p-4">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono text-xs text-muted-foreground">#{migration.id}</span>
                    <span className="text-sm font-medium">{migration.sourceName || nameOf(migration.sourceConfigId)}</span>
                    <ArrowRight className="size-3.5 text-muted-foreground" />
                    <span className="text-sm font-medium">{migration.targetName || nameOf(migration.targetConfigId)}</span>
                    <Badge variant={meta.variant} size="sm" className="gap-1">
                      {active ? <Loader2 className="size-3 animate-spin" /> : null}
                      {meta.label}
                    </Badge>
                    {migration.deleteSource ? (
                      <Badge variant="outline" size="sm">
                        删除源对象
                      </Badge>
                    ) : null}
                    <div className="ml-auto flex items-center gap-1">
                      {active ? (
                        <Button size="xs" variant="outline" onClick={() => act(migration, "pause")}>
                          <CirclePause />
                          暂停
                        </Button>
                      ) : null}
                      {migration.status === "paused" || migration.status === "failed" ? (
                        <Button size="xs" variant="outline" onClick={() => act(migration, "resume")}>
                          <CirclePlay />
                          继续
                        </Button>
                      ) : null}
                      {active || migration.status === "paused" || migration.status === "failed" ? (
                        <Button size="xs" variant="ghost" className="text-destructive" onClick={() => act(migration, "cancel")}>
                          <CircleX />
                          取消
                        </Button>
                      ) : null}
                    </div>
                  </div>
                  <Progress value={percent} className="h-1.5" />
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] tabular-nums text-muted-foreground">
                    <span>
                      {done.toLocaleString()} / {migration.totalObjects.toLocaleString()} 个
                    </span>
                    <span>
                      {formatSize(migration.copiedBytes)} / {formatSize(migration.totalBytes)}
                    </span>
                    {migration.skippedObjects ? <span>跳过 {migration.skippedObjects}</span> : null}
                    {migration.failedObjects ? (
                      <span className="text-red-600 dark:text-red-400">失败 {migration.failedObjects}</span>
                    ) : null}
                    <span className="ml-auto" title={formatDateTime(migration.createdAt)}>
                      {migration.createdBy ? `${migration.createdBy} · ` : ""}
                      {formatRelative(migration.finishedAt ?? migration.createdAt)}
                    </span>
                  </div>
                  {migration.lastError ? (
                    <div className="break-all rounded-lg bg-red-500/10 px-2.5 py-1.5 font-mono text-[11px] text-red-700 dark:text-red-300">
                      {migration.lastError}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </div>
        )}
      </section>

      {redirected.length > 0 ? (
        <section className="space-y-2">
          <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">读取转向</div>
          <div className="divide-y rounded-2xl border bg-card">
            {redirected.map((item) => (
              <div key={item.id} className="flex items-center gap-2 px-4 py-2.5 text-sm">
                <span className="font-medium">{item.config_name}</span>
                <CornerDownRight className="size-3.5 text-muted-foreground" />
                <span>{nameOf(item.redirect_config_id)}</span>
                <Button
                  size="xs"
                  variant="ghost"
                  className="ml-auto"
                  onClick={async () => {
                    await clearRedirect.mutateAsync(item.id);
                    toast.success("已撤销");
                  }}
                >
                  <Unlink />
                  撤销
                </Button>
              </div>
            ))}
          </div>
        </section>
      ) : null}

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              将 {sourceConfig?.config_name} 的对象迁移到 {targetConfig?.config_name}
              {setRedirect && deleteSource ? "，并删除源对象" : ""}？
            </AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={start}>开始</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <StorageCloudflareWizard open={cloudflareOpen} pools={pools} onOpenChange={setCloudflareOpen} />
    </div>
  );
}

/** 一列可点选的存储卡片。不可选的照样列出来并写明原因，比从下拉里消失更好懂。 */
function PickColumn({
  title,
  items,
  selected,
  providers,
  onSelect,
  empty
}: {
  title: string;
  items: PickItem[];
  selected: number | null;
  providers?: StorageProviderMeta[];
  onSelect: (id: number) => void;
  empty?: React.ReactNode;
}) {
  const selectable = items.filter((item) => !item.blocked);
  return (
    <div className="min-w-0 space-y-2">
      <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{title}</div>
      {selectable.length === 0 && empty ? empty : null}
      <div className="max-h-80 space-y-1.5 overflow-y-auto">
        {items.map(({ source, overview, blocked }) => {
          const active = selected === source.id;
          const meta = providerMeta(providers, source.provider);
          return (
            <button
              key={source.id}
              type="button"
              disabled={Boolean(blocked)}
              onClick={() => onSelect(source.id)}
              className={cn(
                "flex w-full items-center gap-3 rounded-xl border bg-background px-3 py-2.5 text-left transition-colors",
                "hover:border-foreground/20 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:border-border",
                active && "border-emerald-500/50 bg-emerald-500/5 ring-1 ring-inset ring-emerald-500/30 hover:border-emerald-500/50"
              )}
            >
              <StorageBrandBadge meta={meta} provider={source.provider} size="sm" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  <span className="truncate text-sm font-medium">{source.config_name}</span>
                  {source.scope === "app" ? (
                    <Badge variant="outline" size="sm">
                      应用级
                    </Badge>
                  ) : null}
                </div>
                <div className="truncate text-[11px] tabular-nums text-muted-foreground">
                  {meta?.name ?? source.provider}
                  {overview?.usage ? ` · ${overview.usage.objects.toLocaleString()} 个对象 · ${formatSize(overview.usage.usedBytes)}` : ""}
                </div>
              </div>
              {blocked ? (
                <span className="shrink-0 text-[11px] text-muted-foreground">{blocked}</span>
              ) : overview ? (
                <StateBadge state={source.enabled ? overview.state : "disabled"} />
              ) : null}
              <span
                className={cn(
                  "grid size-5 shrink-0 place-items-center rounded-full border",
                  active ? "border-emerald-500 bg-emerald-500 text-white" : "border-border"
                )}
              >
                {active ? <Check className="size-3" /> : null}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
