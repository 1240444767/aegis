"use client";

import { useState } from "react";
import { History, Loader2, Square } from "lucide-react";
import { toast } from "sonner";
import { RUN_STATUS_META, TRIGGER_LABEL, formatNumber } from "@/components/apps/item-grant/item-grant-meta";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordTable,
  Section,
  StateBlock,
  type Column
} from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import type { ItemGrantRun } from "@/lib/api/item-grant";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";
import { useCancelItemGrantRunMutation, useItemGrantRulesQuery, useItemGrantRunsQuery } from "@/lib/item-grant-hooks";

/**
 * 执行记录：每次手动或定时执行一行，带进度（执行中每 2 秒刷新）、到账 / 跳过 / 失败人数与耗时。
 * 点一行跳到这次执行的发放记录。
 */

function duration(run: ItemGrantRun) {
  const end = run.finishedAt ? new Date(run.finishedAt).getTime() : Date.now();
  const seconds = Math.max(0, Math.round((end - new Date(run.startedAt).getTime()) / 1000));
  if (seconds < 60) return `${seconds} 秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
  return `${Math.floor(seconds / 3600)} 小时 ${Math.floor((seconds % 3600) / 60)} 分`;
}

export function ItemGrantRunsPanel({
  appKey,
  ruleId,
  onRuleChange,
  onOpenRecords
}: {
  appKey: string;
  ruleId?: number;
  onRuleChange: (ruleId?: number) => void;
  onOpenRecords: (runId: number, ruleId: number) => void;
}) {
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const rulesQuery = useItemGrantRulesQuery(appKey);
  const runsQuery = useItemGrantRunsQuery(appKey, { ruleId, page, limit });
  const cancel = useCancelItemGrantRunMutation(appKey);
  const data = runsQuery.data;

  const columns: Array<Column<ItemGrantRun>> = [
    {
      id: "rule",
      header: "规则",
      width: "20%",
      cell: (run) => (
        <div className="min-w-0">
          <div className="truncate text-xs font-medium">{run.ruleName || `#${run.ruleId}`}</div>
          <div className="truncate text-[11px] text-muted-foreground">
            #{run.id} {TRIGGER_LABEL[run.trigger]}，第 {run.ruleVersion} 版
          </div>
        </div>
      )
    },
    {
      id: "progress",
      header: "进度",
      cell: (run) => {
        const percent = run.total > 0 ? Math.min(100, (run.processed / run.total) * 100) : run.status === "succeeded" ? 100 : 0;
        return (
          <div className="min-w-40 space-y-1">
            <div className="flex items-center justify-between text-[11px] tabular-nums text-muted-foreground">
              <span>
                {formatNumber(run.processed)} / {formatNumber(run.total)}
              </span>
              <span>{percent.toFixed(0)}%</span>
            </div>
            <Progress value={percent} className="h-1.5" />
          </div>
        );
      }
    },
    {
      id: "counts",
      header: "到账 / 跳过 / 失败",
      width: "150px",
      hideBelow: "md",
      cell: (run) => (
        <span className="text-xs tabular-nums">
          <span className="text-emerald-600 dark:text-emerald-400">{formatNumber(run.granted)}</span>
          <span className="text-muted-foreground"> / {formatNumber(run.skipped)} / </span>
          <span className={run.failed ? "text-red-600 dark:text-red-400" : "text-muted-foreground"}>{formatNumber(run.failed)}</span>
        </span>
      )
    },
    {
      id: "status",
      header: "状态",
      width: "100px",
      cell: (run) => {
        const meta = RUN_STATUS_META[run.status];
        return (
          <Badge variant={meta.variant} size="sm" className="gap-1" title={run.error || undefined}>
            {run.status === "running" ? <Loader2 className="size-3 animate-spin" /> : null}
            {meta.label}
          </Badge>
        );
      }
    },
    {
      id: "time",
      header: "开始 / 耗时",
      width: "170px",
      hideBelow: "lg",
      cell: (run) => (
        <div className="text-[11px] tabular-nums text-muted-foreground">
          <div>{formatDateTime(run.startedAt)}</div>
          <div>
            {duration(run)}
            {run.operator ? `（${run.operator}）` : ""}
          </div>
        </div>
      )
    },
    {
      id: "actions",
      header: "",
      width: "72px",
      align: "right",
      cell: (run) =>
        run.status === "running" ? (
          <Button
            size="sm"
            variant="ghost"
            className="h-7 px-2 text-xs hover:text-destructive"
            disabled={cancel.isPending}
            onClick={(event) => {
              event.stopPropagation();
              cancel.mutate(run.id, {
                onSuccess: () => toast.success("已取消"),
                onError: (error) => toast.error(error instanceof ApiError ? error.message : "取消失败")
              });
            }}
          >
            <Square className="size-3" /> 取消
          </Button>
        ) : null
    }
  ];

  return (
    <Section
      title="执行记录"
      description="手动与定时执行的每一次；进程重启时由其它实例从游标接着跑完"
      icon={History}
      tone="info"
      flush
      toolbar={
        <FilterBar>
          <FilterSelect
            label="规则"
            value={ruleId ? String(ruleId) : ""}
            onChange={(value) => {
              onRuleChange(value ? Number(value) : undefined);
              setPage(1);
            }}
            options={(rulesQuery.data ?? []).map((rule) => ({ value: String(rule.id), label: rule.name }))}
            allLabel="全部规则"
          />
        </FilterBar>
      }
      footer={
        data ? (
          <Pager
            page={page}
            limit={limit}
            total={data.total}
            onPage={setPage}
            onLimit={(value) => {
              setLimit(value);
              setPage(1);
            }}
          />
        ) : null
      }
    >
      <RecordTable
        columns={columns}
        rows={data?.items}
        rowKey={(run) => run.id}
        loading={runsQuery.isLoading}
        fetching={runsQuery.isFetching}
        error={runsQuery.error}
        onRetry={() => void runsQuery.refetch()}
        onRowClick={(run) => onOpenRecords(run.id, run.ruleId)}
        empty={<StateBlock compact icon={History} title="还没有执行过" description="手动规则在规则卡片上点「执行」；定时规则上线后按计划自动执行" />}
      />
    </Section>
  );
}
