"use client";

import { useState } from "react";
import { ArrowRight, CirclePause, CirclePlay, CircleX, CornerDownRight, Loader2, Truck, Unlink } from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
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
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type {
  StorageConfigOverview,
  StorageMigration,
  StorageMigrationStatus,
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
import { StorageBrandBadge, formatSize, providerMeta } from "./storage-shared";

const STATUS_META: Record<StorageMigrationStatus, { label: string; variant: "success" | "warning" | "danger" | "info" | "outline" }> = {
  pending: { label: "排队中", variant: "info" },
  running: { label: "迁移中", variant: "info" },
  paused: { label: "已暂停", variant: "warning" },
  completed: { label: "已完成", variant: "success" },
  failed: { label: "已失败", variant: "danger" },
  cancelled: { label: "已取消", variant: "outline" }
};

export function StorageMigrationsPanel({
  sources,
  overviews
}: {
  sources: StorageSource[];
  overviews: StorageConfigOverview[];
}) {
  const providers = useStorageProvidersQuery().data;
  const migrations = useStorageMigrationsQuery();
  const create = useCreateStorageMigrationMutation();
  const control = useControlStorageMigrationMutation();
  const clearRedirect = useClearStorageRedirectMutation();
  const [source, setSource] = useState<string>("");
  const [target, setTarget] = useState<string>("");
  const [setRedirect, setSetRedirect] = useState(true);
  const [deleteSource, setDeleteSource] = useState(false);
  const [confirming, setConfirming] = useState(false);

  const items = migrations.data ?? [];
  const sourceConfig = sources.find((item) => String(item.id) === source);
  const sourceUsage = overviews.find((item) => item.config.id === sourceConfig?.id)?.usage;
  const targets = sources.filter((item) => item.enabled && String(item.id) !== source);
  const redirected = sources.filter((item) => item.redirect_config_id);
  const nameOf = (id?: number | null) => sources.find((item) => item.id === id)?.config_name ?? `#${id}`;

  async function start() {
    try {
      await create.mutateAsync({
        sourceConfigId: Number(source),
        targetConfigId: Number(target),
        setRedirect,
        deleteSource: setRedirect && deleteSource
      });
      toast.success("迁移已开始");
      setConfirming(false);
      setSource("");
      setTarget("");
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
      <section className="space-y-4 rounded-2xl border bg-card p-4">
        <div className="flex items-center gap-2 text-sm font-semibold">
          <Truck className="size-4 text-muted-foreground" />
          新建迁移
        </div>
        <div className="grid items-end gap-3 md:grid-cols-[1fr_auto_1fr]">
          <SourcePicker label="源存储" value={source} sources={sources} providers={providers} onChange={setSource} />
          <ArrowRight className="mx-auto mb-2.5 hidden size-4 text-muted-foreground md:block" />
          <SourcePicker label="目标存储" value={target} sources={targets} providers={providers} onChange={setTarget} />
        </div>
        {sourceUsage ? (
          <div className="text-xs tabular-nums text-muted-foreground">
            待迁移 {sourceUsage.objects.toLocaleString()} 个对象 · {formatSize(sourceUsage.usedBytes)}
          </div>
        ) : null}
        <div className="grid gap-2 sm:grid-cols-2">
          <SwitchRow
            label="迁移后读取转向目标"
            checked={setRedirect}
            onChange={(value) => {
              setSetRedirect(value);
              if (!value) setDeleteSource(false);
            }}
          />
          <SwitchRow label="删除源对象" checked={deleteSource} disabled={!setRedirect} onChange={setDeleteSource} />
        </div>
        <div className="flex justify-end">
          <Button disabled={!source || !target || create.isPending} onClick={() => setConfirming(true)}>
            {create.isPending ? <Loader2 className="size-4 animate-spin" /> : <Truck className="size-4" />}
            开始迁移
          </Button>
        </div>
      </section>

      <section className="space-y-2">
        <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">迁移任务</div>
        {migrations.isLoading ? (
          <Skeleton className="h-28 rounded-2xl" />
        ) : items.length === 0 ? (
          <div className="rounded-2xl border border-dashed py-10 text-center text-xs text-muted-foreground">暂无迁移任务</div>
        ) : (
          <div className="space-y-2">
            {items.map((migration) => {
              const done = migration.copiedObjects + migration.skippedObjects + migration.failedObjects;
              const percent = migration.totalObjects > 0 ? Math.min(100, (done / migration.totalObjects) * 100) : migration.status === "completed" ? 100 : 0;
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
                    {migration.failedObjects ? <span className="text-red-600 dark:text-red-400">失败 {migration.failedObjects}</span> : null}
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
              将 {sourceConfig?.config_name} 的对象迁移到 {nameOf(Number(target))}
              {setRedirect && deleteSource ? "，并删除源对象" : ""}？
            </AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={start}>开始</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function SourcePicker({
  label,
  value,
  sources,
  providers,
  onChange
}: {
  label: string;
  value: string;
  sources: StorageSource[];
  providers: ReturnType<typeof useStorageProvidersQuery>["data"];
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs">{label}</Label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger className="w-full">
          <SelectValue placeholder="选择存储" />
        </SelectTrigger>
        <SelectContent>
          {sources.map((source) => (
            <SelectItem key={source.id} value={String(source.id)}>
              <StorageBrandBadge meta={providerMeta(providers, source.provider)} provider={source.provider} size="sm" className="size-5 rounded-md" />
              {source.config_name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
