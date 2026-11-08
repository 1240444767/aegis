"use client";

import { useState } from "react";
import { CheckCheck, Loader2, PackageCheck, ReceiptText, RotateCw } from "lucide-react";
import { toast } from "sonner";
import { ITEM_ICON, NODE_VISUAL, RECORD_STATUS_META, TRIGGER_LABEL } from "@/components/apps/item-grant/item-grant-meta";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  TimeRangePicker,
  rangeToParams,
  useDebouncedValue,
  type Column,
  type TimeRange
} from "@/components/users/detail/kit";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { ItemGrantCatalog, ItemGrantRecord, ItemGrantRecordStatus } from "@/lib/api/item-grant";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";
import {
  useFulfillItemGrantRecordMutation,
  useItemGrantCatalogQuery,
  useItemGrantRecordsQuery,
  useItemGrantRulesQuery,
  useRetryItemGrantRecordMutation
} from "@/lib/item-grant-hooks";

/**
 * 发放记录：一个人在一条规则里的一次发放。
 * 点开看判定路径（走过哪些节点、为什么）、计划发放与实际结果（流水号、折算说明）；
 * 人工发放在这里标记已发放，发放失败的在这里重试（按规则当前的画布重新判定）。
 */

function errorText(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

export function ItemGrantRecordsPanel({
  appKey,
  ruleId,
  runId,
  initialStatus = "",
  onRuleChange,
  onRunChange
}: {
  appKey: string;
  ruleId?: number;
  runId?: number;
  /** 从总览点「待人工发放」「发放失败」进来时预设的状态筛选 */
  initialStatus?: string;
  onRuleChange: (ruleId?: number) => void;
  onRunChange: (runId?: number) => void;
}) {
  const [status, setStatus] = useState<string>(initialStatus);
  const [keyword, setKeyword] = useState("");
  const [range, setRange] = useState<TimeRange>({ preset: "all" });
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [opened, setOpened] = useState<ItemGrantRecord | null>(null);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const rulesQuery = useItemGrantRulesQuery(appKey);
  const catalogQuery = useItemGrantCatalogQuery(appKey);
  const { from, to } = rangeToParams(range);
  const recordsQuery = useItemGrantRecordsQuery(appKey, {
    ruleId,
    runId,
    status: (status || undefined) as ItemGrantRecordStatus | undefined,
    keyword: debounced || undefined,
    start: from,
    end: to,
    page,
    limit
  });
  const data = recordsQuery.data;
  const filtered = Boolean(ruleId || runId || status || keyword || range.preset !== "all");

  const columns: Array<Column<ItemGrantRecord>> = [
    {
      id: "user",
      header: "用户",
      width: "18%",
      cell: (record) => (
        <div className="min-w-0">
          <div className="truncate text-xs font-medium">{record.nickname || record.account || `#${record.userId}`}</div>
          <div className="truncate text-[11px] text-muted-foreground">
            {record.account} #{record.userId}
          </div>
        </div>
      )
    },
    {
      id: "rule",
      header: "规则",
      width: "18%",
      hideBelow: "md",
      cell: (record) => (
        <div className="min-w-0">
          <div className="truncate text-xs">{record.ruleName || `#${record.ruleId}`}</div>
          <div className="truncate text-[11px] text-muted-foreground">
            {TRIGGER_LABEL[record.trigger]}
            {record.event ? `（${catalogQuery.data?.events.find((event) => event.key === record.event)?.label ?? record.event}）` : ""}
          </div>
        </div>
      )
    },
    {
      id: "items",
      header: "物品",
      cell: (record) => (
        <div className="flex flex-wrap gap-1">
          {(record.results.length ? record.results.map((result) => result.detail) : record.items.map((item) => item.item.type))
            .slice(0, 3)
            .map((text, index) => (
              <span key={index} className="truncate rounded-md bg-muted px-1.5 py-0.5 text-[11px]">
                {text}
              </span>
            ))}
          {Math.max(record.results.length, record.items.length) > 3 ? (
            <span className="text-[11px] text-muted-foreground">+{Math.max(record.results.length, record.items.length) - 3}</span>
          ) : null}
        </div>
      )
    },
    {
      id: "status",
      header: "状态",
      width: "110px",
      cell: (record) => {
        const meta = RECORD_STATUS_META[record.status];
        return (
          <Badge variant={meta.variant} size="sm">
            {meta.label}
          </Badge>
        );
      }
    },
    {
      id: "time",
      header: "时间",
      width: "150px",
      align: "right",
      hideBelow: "sm",
      cell: (record) => <span className="text-xs tabular-nums text-muted-foreground">{formatDateTime(record.createdAt)}</span>
    }
  ];

  return (
    <Section
      title="发放记录"
      description="每条记录是一个人在一条规则里的一次发放；点开查看判定路径与实际到账"
      icon={ReceiptText}
      tone="success"
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
          <FilterSelect
            label="状态"
            value={status}
            onChange={(value) => {
              setStatus(value);
              setPage(1);
            }}
            options={Object.entries(RECORD_STATUS_META).map(([value, meta]) => ({ value, label: meta.label }))}
          />
          <TimeRangePicker
            value={range}
            onChange={(value) => {
              setRange(value);
              setPage(1);
            }}
          />
          <SearchField
            value={keyword}
            onChange={(value) => {
              setKeyword(value);
              setPage(1);
            }}
            placeholder="记录号 / 账号 / 昵称"
          />
          {runId ? (
            <Badge variant="info" size="sm" className="gap-1">
              执行 #{runId}
              <button type="button" className="hover:text-foreground" onClick={() => onRunChange(undefined)} aria-label="清除执行筛选">
                ×
              </button>
            </Badge>
          ) : null}
          <ResetButton
            visible={filtered}
            onClick={() => {
              onRuleChange(undefined);
              onRunChange(undefined);
              setStatus("");
              setKeyword("");
              setRange({ preset: "all" });
              setPage(1);
            }}
          />
        </FilterBar>
      }
      footer={data ? <Pager page={page} limit={limit} total={data.total} onPage={setPage} onLimit={(value) => { setLimit(value); setPage(1); }} /> : null}
    >
      <RecordTable
        columns={columns}
        rows={data?.items}
        rowKey={(record) => record.id}
        loading={recordsQuery.isLoading}
        fetching={recordsQuery.isFetching}
        error={recordsQuery.error}
        onRetry={() => void recordsQuery.refetch()}
        onRowClick={setOpened}
        empty={<StateBlock compact title={filtered ? "没有符合条件的记录" : "还没有发放记录"} />}
      />
      <RecordDetail appKey={appKey} record={opened} catalog={catalogQuery.data} onClose={() => setOpened(null)} onChanged={setOpened} />
    </Section>
  );
}

function RecordDetail({
  appKey,
  record,
  catalog,
  onClose,
  onChanged
}: {
  appKey: string;
  record: ItemGrantRecord | null;
  catalog?: ItemGrantCatalog;
  onClose: () => void;
  onChanged: (record: ItemGrantRecord | null) => void;
}) {
  const fulfill = useFulfillItemGrantRecordMutation(appKey);
  const retry = useRetryItemGrantRecordMutation(appKey);
  const [confirm, setConfirm] = useState<"fulfill" | "retry" | null>(null);
  const [note, setNote] = useState("");
  if (!record) return <RecordSheet open={false} onOpenChange={() => onClose()} title="" />;
  const meta = RECORD_STATUS_META[record.status];

  return (
    <RecordSheet
      open={Boolean(record)}
      onOpenChange={(open) => !open && onClose()}
      title={record.ruleName || "发放记录"}
      description={record.recordNo}
      hero={
        <div className="flex items-center gap-2">
          <Badge variant={meta.variant}>{meta.label}</Badge>
          <span className="text-xs text-muted-foreground">{formatDateTime(record.createdAt)}</span>
        </div>
      }
      fields={[
        { label: "用户", value: `${record.nickname || record.account} #${record.userId}` },
        { label: "触发", value: `${TRIGGER_LABEL[record.trigger]}${record.event ? `（${catalog?.events.find((event) => event.key === record.event)?.label ?? record.event}）` : ""}` },
        { label: "规则版本", value: `第 ${record.ruleVersion} 版` },
        { label: "执行", value: record.runId ? `#${record.runId}` : "—" },
        ...(record.handledBy ? [{ label: "处理人", value: `${record.handledBy}，${formatDateTime(record.handledAt)}` }] : []),
        ...(record.note ? [{ label: "处理说明", value: record.note }] : [])
      ]}
      raw={{ eventData: record.eventData, items: record.items, results: record.results }}
      actions={
        record.status === "pending" ? (
          <Button size="sm" onClick={() => setConfirm("fulfill")}>
            <PackageCheck /> 标记已发放
          </Button>
        ) : record.status === "failed" ? (
          <Button size="sm" onClick={() => setConfirm("retry")}>
            <RotateCw /> 重试
          </Button>
        ) : null
      }
    >
      {record.error ? (
        <div className="rounded-lg bg-red-500/10 px-3 py-2 text-xs leading-5 text-red-700 dark:text-red-300">{record.error}</div>
      ) : null}

      <div className="space-y-2">
        <div className="text-xs font-medium text-muted-foreground">{record.results.length ? "实际到账" : "计划发放"}</div>
        <div className="divide-y rounded-xl border">
          {(record.results.length
            ? record.results.map((result) => ({ type: result.type, label: result.label, detail: result.detail, extra: result }))
            : record.items.map((item) => ({ type: item.item.type, label: catalog?.items.find((spec) => spec.type === item.item.type)?.label ?? item.item.type, detail: JSON.stringify(item.item), extra: null }))
          ).map((row, index) => {
            const Icon = ITEM_ICON[row.type] ?? CheckCheck;
            return (
              <div key={index} className="flex items-start gap-2.5 px-3 py-2">
                <Icon className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="text-xs font-medium">{row.label}</div>
                  <div className="text-[11px] leading-4 break-all text-muted-foreground">{row.detail}</div>
                  {row.extra?.transactionNo ? (
                    <div className="font-mono text-[10px] text-muted-foreground">{row.extra.transactionNo}</div>
                  ) : null}
                </div>
                {row.extra?.converted ? <Badge variant="warning" size="sm">已折算</Badge> : null}
                {row.extra?.pending ? <Badge variant="warning" size="sm">待人工</Badge> : null}
              </div>
            );
          })}
        </div>
      </div>

      <div className="space-y-2">
        <div className="text-xs font-medium text-muted-foreground">判定路径</div>
        <ol className="relative space-y-2 border-l border-dashed pl-4">
          {record.steps.map((step, index) => {
            const visual = NODE_VISUAL[step.type];
            const spec = catalog?.nodes.find((candidate) => candidate.type === step.type);
            return (
              <li key={`${step.nodeId}-${index}`} className="relative">
                <span
                  className="absolute top-1 -left-[23px] size-3.5 rounded-full border-2 border-background"
                  style={{ background: visual?.accent ?? "#94a3b8" }}
                />
                <div className="text-xs font-medium">
                  {spec?.label ?? step.type}
                  {step.handle ? <span className="ml-1.5 rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">→ {step.handle}</span> : null}
                </div>
                <div className="text-[11px] leading-4 text-muted-foreground">{step.detail}</div>
              </li>
            );
          })}
        </ol>
      </div>

      <ConfirmActionDialog
        open={confirm === "fulfill"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="标记已发放"
        description="确认人工发放的物品已经交到用户手上；用户会收到一条站内信。"
        confirmLabel="标记已发放"
        pending={fulfill.isPending}
        note={{ value: note, onChange: setNote, placeholder: "例如快递单号、兑换码", max: 200 }}
        onConfirm={() =>
          fulfill.mutate(
            { recordId: record.id, note },
            {
              onSuccess: (next) => {
                toast.success("已标记发放");
                setConfirm(null);
                setNote("");
                onChanged(next);
              },
              onError: (error) => toast.error(errorText(error, "操作失败"))
            }
          )
        }
      />
      <ConfirmActionDialog
        open={confirm === "retry"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="重试发放"
        description="按规则当前的画布重新为这个人判定并发放；仍然失败会留下新的失败记录。"
        confirmLabel="重试"
        pending={retry.isPending}
        onConfirm={() =>
          retry.mutate(record.id, {
            onSuccess: (result) => {
              setConfirm(null);
              if (result.status === "granted" || result.status === "pending") {
                toast.success("已重新发放");
                onChanged(result.record ?? null);
              } else if (result.status === "failed") {
                toast.error(result.error || "仍然发放失败");
                onChanged(result.record ?? null);
              } else {
                toast.message(result.status === "skipped" ? "按当前画布这个人不再命中任何物品" : "已经发放过");
                onChanged(null);
              }
            },
            onError: (error) => toast.error(errorText(error, "重试失败"))
          })
        }
      />
      {fulfill.isPending || retry.isPending ? <Loader2 className="mx-auto size-4 animate-spin text-muted-foreground" /> : null}
    </RecordSheet>
  );
}
