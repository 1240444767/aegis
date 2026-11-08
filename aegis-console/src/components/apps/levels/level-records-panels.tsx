"use client";

import { useState } from "react";
import { CheckCheck, Gift, History, PackageCheck, RotateCw } from "lucide-react";
import { toast } from "sonner";
import { ITEM_ICON } from "@/components/apps/item-grant/item-grant-meta";
import { EVENT_REASON_LABEL, REWARD_STATUS_META, formatNumber } from "@/components/apps/levels/level-meta";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  Section,
  StateBlock,
  TimeRangePicker,
  rangeToParams,
  type Column,
  type TimeRange
} from "@/components/users/detail/kit";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { LevelEvent, LevelRewardRecord } from "@/lib/api/levels";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";
import {
  useFulfillLevelRewardMutation,
  useLevelEventsQuery,
  useLevelRewardsQuery,
  useLevelSchemeQuery,
  useRetryLevelRewardMutation
} from "@/lib/level-hooks";

/**
 * 升级记录与达成奖励记录。
 * 升级记录：谁在什么时候因为什么从几级升到几级、升级通知是否送达。
 * 奖励记录：每人每个奖励节点一条；失败的在这里重试，含人工物品的在这里标记已发放。
 */

const NOTIFY_LABEL: Record<string, string> = { none: "不通知", pending: "待发送", sending: "发送中", sent: "已通知", failed: "通知失败" };

export function LevelEventsPanel({ appKey }: { appKey: string }) {
  const [reason, setReason] = useState("");
  const [level, setLevel] = useState("");
  const [range, setRange] = useState<TimeRange>({ preset: "all" });
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const schemeQuery = useLevelSchemeQuery(appKey);
  const { from, to } = rangeToParams(range);
  const query = useLevelEventsQuery(appKey, { reason: reason || undefined, level: level ? Number(level) : undefined, start: from, end: to, page, limit });
  const data = query.data;
  const filtered = Boolean(reason || level || range.preset !== "all");

  const columns: Array<Column<LevelEvent>> = [
    {
      id: "user",
      header: "用户",
      width: "24%",
      cell: (event) => (
        <div className="min-w-0">
          <div className="truncate text-xs font-medium">{event.nickname || event.account || `#${event.userId}`}</div>
          <div className="truncate text-[11px] text-muted-foreground">
            {event.account} #{event.userId}
          </div>
        </div>
      )
    },
    {
      id: "change",
      header: "变化",
      cell: (event) => (
        <div className="text-xs">
          <span className="tabular-nums text-muted-foreground">Lv.{event.fromLevel}</span>
          <span className="mx-1.5 text-muted-foreground">→</span>
          <span className={event.toLevel >= event.fromLevel ? "font-medium" : "font-medium text-amber-600 dark:text-amber-400"}>
            Lv.{event.toLevel} {event.toName}
          </span>
        </div>
      )
    },
    {
      id: "reason",
      header: "原因",
      width: "16%",
      cell: (event) => (
        <div className="min-w-0">
          <div className="text-xs">{EVENT_REASON_LABEL[event.reason] ?? event.reason}</div>
          {event.operator || event.note ? (
            <div className="truncate text-[11px] text-muted-foreground">
              {event.operator}
              {event.note ? `：${event.note}` : ""}
            </div>
          ) : null}
        </div>
      )
    },
    { id: "exp", header: "当时经验", width: "12%", align: "right", hideBelow: "md", cell: (event) => <span className="text-xs tabular-nums">{formatNumber(event.experience)}</span> },
    {
      id: "notify",
      header: "通知",
      width: "90px",
      hideBelow: "lg",
      cell: (event) => <span className="text-xs text-muted-foreground">{NOTIFY_LABEL[event.notifyStatus] ?? event.notifyStatus}</span>
    },
    {
      id: "time",
      header: "时间",
      width: "150px",
      align: "right",
      hideBelow: "sm",
      cell: (event) => <span className="text-xs tabular-nums text-muted-foreground">{formatDateTime(event.createdAt)}</span>
    }
  ];

  return (
    <Section
      title="升级记录"
      description="经验增长、补判（晋级条件后来满足）、重算任务与管理员调整引起的等级变化"
      icon={History}
      tone="primary"
      flush
      toolbar={
        <FilterBar>
          <FilterSelect
            label="原因"
            value={reason}
            onChange={(value) => {
              setReason(value);
              setPage(1);
            }}
            options={Object.entries(EVENT_REASON_LABEL).map(([value, label]) => ({ value, label }))}
          />
          <FilterSelect
            label="升到"
            value={level}
            onChange={(value) => {
              setLevel(value);
              setPage(1);
            }}
            options={(schemeQuery.data?.levels ?? []).map((row) => ({ value: String(row.level), label: `Lv.${row.level} ${row.name}` }))}
            allLabel="全部等级"
          />
          <TimeRangePicker
            value={range}
            onChange={(value) => {
              setRange(value);
              setPage(1);
            }}
          />
          <ResetButton
            visible={filtered}
            onClick={() => {
              setReason("");
              setLevel("");
              setRange({ preset: "all" });
              setPage(1);
            }}
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
        rowKey={(event) => event.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        empty={<StateBlock compact title={filtered ? "没有符合条件的记录" : "还没有升级记录"} />}
      />
    </Section>
  );
}

export function LevelRewardsPanel({ appKey, initialStatus = "" }: { appKey: string; initialStatus?: string }) {
  const [status, setStatus] = useState(initialStatus);
  const [level, setLevel] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [opened, setOpened] = useState<LevelRewardRecord | null>(null);
  const schemeQuery = useLevelSchemeQuery(appKey);
  const query = useLevelRewardsQuery(appKey, { status: status || undefined, level: level ? Number(level) : undefined, page, limit });
  const data = query.data;
  const filtered = Boolean(status || level);

  const columns: Array<Column<LevelRewardRecord>> = [
    {
      id: "user",
      header: "用户",
      width: "22%",
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
      id: "reward",
      header: "奖励",
      cell: (record) => (
        <div className="min-w-0">
          <div className="truncate text-xs font-medium">
            Lv.{record.level} {record.label}
            {record.origin === "backfill" ? <span className="ml-1.5 rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">补发</span> : null}
          </div>
          <div className="truncate text-[11px] text-muted-foreground">{record.itemsText.join("、")}</div>
        </div>
      )
    },
    { id: "claim", header: "方式", width: "90px", hideBelow: "md", cell: (record) => <span className="text-xs text-muted-foreground">{record.claim === "manual" ? "用户领取" : "自动到账"}</span> },
    {
      id: "status",
      header: "状态",
      width: "110px",
      cell: (record) => {
        const meta = REWARD_STATUS_META[record.status];
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
      cell: (record) => <span className="text-xs tabular-nums text-muted-foreground">{formatDateTime(record.grantedAt ?? record.createdAt)}</span>
    }
  ];

  return (
    <Section
      title="达成奖励记录"
      description="升级经过的每一级各一份；自动到账的由后台逐条发放，用户领取的在 App 里领取"
      icon={Gift}
      tone="success"
      flush
      toolbar={
        <FilterBar>
          <FilterSelect
            label="状态"
            value={status}
            onChange={(value) => {
              setStatus(value);
              setPage(1);
            }}
            options={Object.entries(REWARD_STATUS_META).map(([value, meta]) => ({ value, label: meta.label }))}
          />
          <FilterSelect
            label="等级"
            value={level}
            onChange={(value) => {
              setLevel(value);
              setPage(1);
            }}
            options={(schemeQuery.data?.levels ?? []).map((row) => ({ value: String(row.level), label: `Lv.${row.level} ${row.name}` }))}
            allLabel="全部等级"
          />
          <ResetButton
            visible={filtered}
            onClick={() => {
              setStatus("");
              setLevel("");
              setPage(1);
            }}
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
        rowKey={(record) => record.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        onRowClick={setOpened}
        empty={<StateBlock compact title={filtered ? "没有符合条件的记录" : "还没有达成奖励记录"} />}
      />
      <RewardDetail appKey={appKey} record={opened} onClose={() => setOpened(null)} onChanged={setOpened} />
    </Section>
  );
}

function RewardDetail({
  appKey,
  record,
  onClose,
  onChanged
}: {
  appKey: string;
  record: LevelRewardRecord | null;
  onClose: () => void;
  onChanged: (record: LevelRewardRecord | null) => void;
}) {
  const retry = useRetryLevelRewardMutation(appKey);
  const fulfill = useFulfillLevelRewardMutation(appKey);
  const [confirm, setConfirm] = useState<"retry" | "fulfill" | null>(null);
  const [note, setNote] = useState("");
  if (!record) return <RecordSheet open={false} onOpenChange={() => onClose()} title="" />;
  const meta = REWARD_STATUS_META[record.status];

  return (
    <RecordSheet
      open={Boolean(record)}
      onOpenChange={(open) => !open && onClose()}
      title={`Lv.${record.level} ${record.label}`}
      description={`LVR${record.id}`}
      hero={
        <div className="flex items-center gap-2">
          <Badge variant={meta.variant}>{meta.label}</Badge>
          <span className="text-xs text-muted-foreground">{formatDateTime(record.createdAt)}</span>
        </div>
      }
      fields={[
        { label: "用户", value: `${record.nickname || record.account} #${record.userId}` },
        { label: "方式", value: record.claim === "manual" ? "用户领取" : "自动到账" },
        { label: "来源", value: record.origin === "backfill" ? "补发" : "升级" },
        { label: "体系版本", value: record.schemeVersion ? `第 ${record.schemeVersion} 版` : "默认体系" },
        { label: "尝试次数", value: String(record.attempts) },
        ...(record.grantedAt ? [{ label: "到账时间", value: formatDateTime(record.grantedAt) }] : []),
        ...(record.handledBy ? [{ label: "处理人", value: record.handledBy }] : []),
        ...(record.handledNote ? [{ label: "处理说明", value: record.handledNote, wide: true }] : [])
      ]}
      raw={{ items: record.items, results: record.results }}
      actions={
        record.status === "failed" ? (
          <Button size="sm" onClick={() => setConfirm("retry")}>
            <RotateCw /> 重试
          </Button>
        ) : record.status === "pending" ? (
          <Button size="sm" onClick={() => setConfirm("fulfill")}>
            <PackageCheck /> 标记已发放
          </Button>
        ) : null
      }
    >
      {record.error ? <div className="rounded-lg bg-red-500/10 px-3 py-2 text-xs leading-5 text-red-700 dark:text-red-300">{record.error}</div> : null}
      <div className="space-y-2">
        <div className="text-xs font-medium text-muted-foreground">{record.results.length ? "实际到账" : "物品"}</div>
        <div className="divide-y rounded-xl border">
          {(record.results.length
            ? record.results.map((result) => ({ type: result.type, label: result.label, detail: result.detail, txn: result.transactionNo }))
            : record.items.map((item, index) => ({ type: item.type, label: record.itemsText[index] ?? item.type, detail: "", txn: undefined }))
          ).map((row, index) => {
            const Icon = ITEM_ICON[row.type] ?? CheckCheck;
            return (
              <div key={index} className="flex items-start gap-2.5 px-3 py-2">
                <Icon className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="text-xs font-medium">{row.label}</div>
                  {row.detail ? <div className="text-[11px] leading-4 break-all text-muted-foreground">{row.detail}</div> : null}
                  {row.txn ? <div className="font-mono text-[10px] text-muted-foreground">{row.txn}</div> : null}
                </div>
              </div>
            );
          })}
        </div>
      </div>
      <ConfirmActionDialog
        open={confirm === "retry"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="重试发放"
        description="立即重新发放这份奖励（物品以记录里的为准）；仍然失败会更新失败原因。"
        confirmLabel="重试"
        pending={retry.isPending}
        onConfirm={() =>
          retry.mutate(record.id, {
            onSuccess: (next) => {
              setConfirm(null);
              if (next.status === "failed") toast.error(next.error || "仍然发放失败");
              else toast.success("已重新发放");
              onChanged(next);
            },
            onError: (error) => toast.error(error instanceof ApiError ? error.message : "重试失败")
          })
        }
      />
      <ConfirmActionDialog
        open={confirm === "fulfill"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="标记已发放"
        description="确认人工物品已经交到用户手上；用户会收到一条站内信。"
        confirmLabel="标记已发放"
        pending={fulfill.isPending}
        note={{ value: note, onChange: setNote, placeholder: "例如快递单号、兑换码", max: 200 }}
        onConfirm={() =>
          fulfill.mutate(
            { rewardId: record.id, note },
            {
              onSuccess: (next) => {
                toast.success("已标记发放");
                setConfirm(null);
                setNote("");
                onChanged(next);
              },
              onError: (error) => toast.error(error instanceof ApiError ? error.message : "操作失败")
            }
          )
        }
      />
    </RecordSheet>
  );
}
