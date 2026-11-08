"use client";

import { useState } from "react";
import { GitCommitVertical, Loader2, Play, RefreshCcw, Square, Undo2, View } from "lucide-react";
import { toast } from "sonner";
import { JOB_STATUS_META, formatNumber } from "@/components/apps/levels/level-meta";
import { Section, StateBlock } from "@/components/users/detail/kit";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import type { ItemGrantGraph } from "@/lib/api/item-grant";
import type { LevelVersion } from "@/lib/api/levels";
import { ApiError } from "@/lib/api/client";
import { getLevelVersion } from "@/lib/api/levels";
import { useAdminToken } from "@/lib/admin-hooks";
import { formatDateTime } from "@/lib/datetime";
import {
  useCancelLevelJobMutation,
  useLevelJobsQuery,
  useLevelSchemeQuery,
  useLevelVersionsQuery,
  useRestoreLevelVersionMutation,
  useStartLevelJobMutation
} from "@/lib/level-hooks";

/**
 * 版本与重算：每次发布留一份画布快照（在画布里打开查看、取回草稿）；重算任务让全体用户按当前体系判定一次。
 */


export function LevelVersionsPanel({
  appKey,
  onOpenGraph
}: {
  appKey: string;
  /** 在画布里打开某一版（只是载入，保存才会覆盖草稿） */
  onOpenGraph: (graph: ItemGrantGraph, from: string) => void;
}) {
  const token = useAdminToken();
  const versionsQuery = useLevelVersionsQuery(appKey);
  const schemeQuery = useLevelSchemeQuery(appKey);
  const restore = useRestoreLevelVersionMutation(appKey);
  const [restoring, setRestoring] = useState<LevelVersion | null>(null);
  const [opening, setOpening] = useState<number | null>(null);
  const current = schemeQuery.data?.publishedVersion ?? 0;

  const open = async (version: LevelVersion) => {
    if (!token) return;
    setOpening(version.version);
    try {
      const detail = await getLevelVersion(token, appKey, version.version);
      if (detail.graph) onOpenGraph(detail.graph, `第 ${version.version} 版`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "加载失败");
    } finally {
      setOpening(null);
    }
  };

  return (
    <div className="space-y-4">
      <RecalcJobs appKey={appKey} />
      <Section title="发布历史" description="每次发布的画布快照；取回草稿后在画布里修改再发布即可回滚" icon={GitCommitVertical} tone="violet" flush>
        {versionsQuery.isLoading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-12 w-full" />
            ))}
          </div>
        ) : versionsQuery.error ? (
          <StateBlock kind="error" description={versionsQuery.error instanceof Error ? versionsQuery.error.message : "加载失败"} onRetry={() => void versionsQuery.refetch()} />
        ) : versionsQuery.data?.length ? (
          <div className="divide-y">
            {versionsQuery.data.map((version) => (
              <div key={version.version} className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:px-5">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    第 {version.version} 版
                    {version.version === current ? <Badge size="sm" variant="success">生效中</Badge> : null}
                    {version.summary.name ? <span className="truncate text-xs font-normal text-muted-foreground">{version.summary.name}</span> : null}
                  </div>
                  <div className="truncate text-[11px] text-muted-foreground">
                    {formatDateTime(version.publishedAt)}
                    {version.publishedBy ? `，${version.publishedBy}` : ""}，{version.summary.levels} 级，{version.summary.rewards} 份奖励，{version.summary.privileges} 项权益
                    {version.summary.gates ? `，${version.summary.gates} 道晋级条件` : ""}
                  </div>
                  {version.note ? <div className="mt-0.5 text-xs">{version.note}</div> : null}
                </div>
                <div className="flex shrink-0 gap-1.5">
                  <Button size="sm" variant="outline" onClick={() => void open(version)} disabled={opening === version.version}>
                    {opening === version.version ? <Loader2 className="animate-spin" /> : <View />}
                    在画布中查看
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => setRestoring(version)}>
                    <Undo2 /> 取回草稿
                  </Button>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <StateBlock compact title="还没有发布过" description="在画布里修改并发布后，这里会留下每一版的快照" />
        )}
      </Section>
      <ConfirmActionDialog
        open={Boolean(restoring)}
        onOpenChange={(value) => !value && setRestoring(null)}
        title={`取回第 ${restoring?.version ?? ""} 版`}
        description="当前草稿会被这一版的画布覆盖（不影响线上）；之后在画布里检查并发布即可回滚。"
        confirmLabel="取回草稿"
        pending={restore.isPending}
        onConfirm={() =>
          restoring &&
          restore.mutate(restoring.version, {
            onSuccess: () => {
              toast.success(`已把第 ${restoring.version} 版取回草稿`);
              setRestoring(null);
            },
            onError: (error) => toast.error(error instanceof ApiError ? error.message : "取回失败")
          })
        }
      />
    </div>
  );
}

function RecalcJobs({ appKey }: { appKey: string }) {
  const jobsQuery = useLevelJobsQuery(appKey);
  const start = useStartLevelJobMutation(appKey);
  const cancel = useCancelLevelJobMutation(appKey);
  const [confirm, setConfirm] = useState(false);
  const [downgrade, setDowngrade] = useState(false);
  const jobs = jobsQuery.data ?? [];
  const running = jobs.find((job) => job.status === "running");

  return (
    <Section
      title="重算任务"
      description="全体用户按已发布的体系判定一次：够格的升级并写入达成奖励；勾选允许降级时按经验从第一级重新判定"
      icon={RefreshCcw}
      tone="info"
      actions={
        <Button size="sm" onClick={() => setConfirm(true)} disabled={Boolean(running) || start.isPending}>
          <Play /> 开始重算
        </Button>
      }
      flush
    >
      {jobs.length ? (
        <div className="divide-y">
          {jobs.slice(0, 8).map((job) => {
            const meta = JOB_STATUS_META[job.status];
            const percent = job.total ? Math.min(100, (job.processed / job.total) * 100) : job.status === "succeeded" ? 100 : 0;
            return (
              <div key={job.id} className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:px-5">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 text-xs font-medium">
                    #{job.id}
                    <Badge size="sm" variant={meta.variant}>
                      {meta.label}
                    </Badge>
                    {job.params.downgrade ? <Badge size="sm" variant="warning">允许降级</Badge> : null}
                    <span className="font-normal text-muted-foreground">
                      {job.schemeVersion ? `第 ${job.schemeVersion} 版` : "默认体系"}，{formatDateTime(job.createdAt)}
                      {job.createdBy ? `，${job.createdBy}` : ""}
                    </span>
                  </div>
                  <Progress className="mt-1.5 h-1.5" value={percent} />
                  <div className="mt-1 text-[11px] text-muted-foreground">
                    {formatNumber(job.processed)} / {formatNumber(job.total)} 人，等级变动 {formatNumber(job.changed)} 人
                    {job.message ? `，${job.message}` : ""}
                  </div>
                </div>
                {job.status === "running" ? (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={cancel.isPending}
                    onClick={() =>
                      cancel.mutate(job.id, {
                        onSuccess: () => toast.success("已取消，这一批处理完就停"),
                        onError: (error) => toast.error(error instanceof ApiError ? error.message : "取消失败")
                      })
                    }
                  >
                    <Square /> 取消
                  </Button>
                ) : null}
              </div>
            );
          })}
        </div>
      ) : (
        <StateBlock compact title="还没有重算过" />
      )}
      <ConfirmActionDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="开始重算"
        description="后台逐批处理这个应用的全部用户（每批 200 人），期间可以取消；实例中断后会自动接着跑。"
        confirmLabel="开始"
        pending={start.isPending}
        onConfirm={() =>
          start.mutate(downgrade, {
            onSuccess: () => {
              setConfirm(false);
              toast.success("重算任务已开始");
            },
            onError: (error) => toast.error(error instanceof ApiError ? error.message : "启动失败")
          })
        }
      >
        <div className="flex items-start gap-2.5">
          <Checkbox id="lv-job-downgrade" checked={downgrade} onCheckedChange={(value) => setDowngrade(Boolean(value))} />
          <Label htmlFor="lv-job-downgrade" className="grid gap-0.5 text-xs font-normal">
            <span className="font-medium">允许降级</span>
            <span className="text-muted-foreground">按经验从第一级重新判定；门槛调高后会有人掉级，已发的奖励不收回。</span>
          </Label>
        </div>
      </ConfirmActionDialog>
    </Section>
  );
}
