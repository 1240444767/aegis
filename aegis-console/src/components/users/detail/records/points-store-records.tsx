"use client";

/**
 * 积分商城兑换记录。
 *
 * 自动发放的权益兑换后直接「已到账」；人工发放的商品是「待发放」，可以在这里发放
 * （写给用户的话进站内信）或取消（积分退回、库存加回）。
 *
 * 指标口径：该用户的兑换记录总数，以及分别按状态筛选的条数。
 */

import * as React from "react";
import { Ban, CheckCircle2, Clock3, PackageCheck, ShoppingBag, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { RedemptionStatusBadge, redemptionStatusLabel } from "@/components/apps/points-store/points-store-shared";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  listPointsStoreRedemptions,
  type PointsStoreRedemption,
  type PointsStoreRedemptionStatus
} from "@/lib/api/points-store";
import { formatDateTime } from "@/lib/datetime";
import { useHandlePointsStoreRedemptionMutation } from "@/lib/points-store-hooks";
import { useUserPointsStoreCountsQuery, useUserPointsStoreRedemptionsQuery } from "@/lib/user-records-hooks";
import {
  FilterBar,
  FilterSelect,
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  errorMessage,
  formatInt,
  useDebouncedValue,
  type Column,
  type CsvColumn
} from "../kit";
import { ConfirmActionDialog, ExportButton, MetricGrid, ResultChips, ResultList, exportStamp, resultsCsv, usePaging } from "./records-shared";

const STATUSES: PointsStoreRedemptionStatus[] = ["completed", "pending", "cancelled"];

type Handling = { item: PointsStoreRedemption; action: "fulfill" | "cancel" } | null;

export function PointsStoreRecords({ appKey, userId }: { appKey: string; userId: number }) {
  const token = useAdminToken();
  const countsQuery = useUserPointsStoreCountsQuery(appKey, userId);
  const counts = countsQuery.data;

  const [status, setStatus] = React.useState("");
  const [keyword, setKeyword] = React.useState("");
  const debouncedKeyword = useDebouncedValue(keyword.trim());
  const paging = usePaging(20);
  const filters = {
    userId,
    status: (status || undefined) as PointsStoreRedemptionStatus | undefined,
    keyword: debouncedKeyword || undefined
  };
  const listQuery = useUserPointsStoreRedemptionsQuery(appKey, { ...filters, page: paging.page, limit: paging.limit });

  const [selected, setSelected] = React.useState<PointsStoreRedemption | null>(null);
  const [handling, setHandling] = React.useState<Handling>(null);
  const [note, setNote] = React.useState("");
  const mutation = useHandlePointsStoreRedemptionMutation(appKey);

  const filtered = Boolean(status || keyword);
  const pageCost = (listQuery.data?.items ?? []).filter((item) => item.status !== "cancelled").reduce((sum, item) => sum + item.cost, 0);

  const openHandle = (item: PointsStoreRedemption, action: "fulfill" | "cancel") => {
    setNote("");
    setHandling({ item, action });
  };

  const submit = async () => {
    if (!handling) return;
    try {
      const updated = await mutation.mutateAsync({ redemptionId: handling.item.id, action: handling.action, note: note.trim() });
      toast.success(handling.action === "fulfill" ? "已发放，并通知了用户" : "已取消，积分已退回");
      if (selected?.id === updated.id) setSelected(updated);
      setHandling(null);
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  const columns: Array<Column<PointsStoreRedemption>> = [
    {
      id: "time",
      header: "时间",
      cell: (row) => (
        <div className="flex flex-col">
          <span className="tabular-nums">{formatDateTime(row.createdAt)}</span>
          <span className="font-mono text-xs text-muted-foreground">{row.redemptionNo}</span>
        </div>
      )
    },
    { id: "item", header: "商品", cell: (row) => <span className="font-medium">{row.itemTitle}</span> },
    {
      id: "cost",
      header: "消耗积分",
      align: "right",
      cell: (row) => (
        <span className={row.status === "cancelled" ? "text-muted-foreground line-through tabular-nums" : "font-medium text-red-600 tabular-nums dark:text-red-400"}>
          -{formatInt(row.cost)}
        </span>
      )
    },
    { id: "status", header: "状态", cell: (row) => <RedemptionStatusBadge status={row.status} /> },
    { id: "results", header: "发放结果", hideBelow: "lg", cell: (row) => <ResultChips results={row.results} /> },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "120px",
      cell: (row) =>
        row.status === "pending" ? (
          <div className="flex justify-end gap-1.5">
            <Button
              size="xs"
              variant="outline"
              onClick={(event) => {
                event.stopPropagation();
                openHandle(row, "fulfill");
              }}
            >
              发放
            </Button>
            <Button
              size="xs"
              variant="ghost"
              className="text-muted-foreground"
              onClick={(event) => {
                event.stopPropagation();
                openHandle(row, "cancel");
              }}
            >
              取消
            </Button>
          </div>
        ) : null
    }
  ];

  const csvColumns: Array<CsvColumn<PointsStoreRedemption>> = [
    { header: "兑换单号", value: (row) => row.redemptionNo },
    { header: "时间", value: (row) => formatDateTime(row.createdAt, "") },
    { header: "商品", value: (row) => row.itemTitle },
    { header: "商品 ID", value: (row) => row.itemId },
    { header: "消耗积分", value: (row) => row.cost },
    { header: "状态", value: (row) => redemptionStatusLabel(row.status) },
    { header: "发放结果", value: (row) => resultsCsv(row.results) },
    { header: "扣积分流水", value: (row) => row.integralTransactionNo },
    { header: "退积分流水", value: (row) => row.refundTransactionNo },
    { header: "备注", value: (row) => row.note },
    { header: "处理人", value: (row) => row.handledBy },
    { header: "处理时间", value: (row) => formatDateTime(row.handledAt, "") },
    { header: "设备", value: (row) => row.deviceId },
    { header: "IP", value: (row) => row.clientIp }
  ];

  return (
    <div className="flex flex-col gap-5">
      <MetricGrid>
        <MetricCard label="兑换次数" icon={ShoppingBag} tone="primary" loading={countsQuery.isLoading} value={formatInt(counts?.total ?? 0)} sub="含已取消" />
        <MetricCard label="已到账" icon={CheckCircle2} tone="success" loading={countsQuery.isLoading} value={formatInt(counts?.completed ?? 0)} sub="自动发放或已人工发放" />
        <MetricCard
          label="待发放"
          icon={Clock3}
          tone="warning"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.pending ?? 0)}
          sub="需人工发放或取消"
          onClick={
            counts?.pending
              ? () => {
                  setStatus("pending");
                  paging.reset();
                }
              : undefined
          }
        />
        <MetricCard label="已取消" icon={Undo2} tone="neutral" loading={countsQuery.isLoading} value={formatInt(counts?.cancelled ?? 0)} sub="积分已退回" />
      </MetricGrid>

      <Section
        flush
        title="积分商城兑换"
        description="待发放的兑换可发放或取消，用户都会收到站内信"
        icon={ShoppingBag}
        tone="warning"
        actions={
          <ExportButton
            filename={`积分商城兑换-${userId}-${exportStamp()}`}
            columns={csvColumns}
            disabled={!token || !listQuery.data?.total}
            fetchPage={(page, limit) => listPointsStoreRedemptions(token as string, appKey, { ...filters, page, limit })}
          />
        }
        toolbar={
          <FilterBar>
            <SearchField
              value={keyword}
              onChange={(next) => {
                setKeyword(next);
                paging.reset();
              }}
              placeholder="搜索兑换单号、商品"
            />
            <FilterSelect
              label="状态"
              value={status}
              onChange={(next) => {
                setStatus(next);
                paging.reset();
              }}
              options={STATUSES.map((item) => ({ value: item, label: redemptionStatusLabel(item) }))}
            />
            <ResetButton
              visible={filtered}
              onClick={() => {
                setStatus("");
                setKeyword("");
                paging.reset();
              }}
            />
          </FilterBar>
        }
        footer={
          listQuery.data?.total ? (
            <Pager
              page={paging.page}
              limit={paging.limit}
              total={listQuery.data.total}
              onPage={paging.setPage}
              onLimit={paging.setLimit}
              summary={<span className="tabular-nums">本页消耗 {formatInt(pageCost)} 积分（不含已取消）</span>}
            />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={listQuery.data?.items}
          rowKey={(row) => row.id}
          loading={listQuery.isLoading}
          fetching={listQuery.isFetching}
          error={listQuery.error}
          onRetry={() => listQuery.refetch()}
          onRowClick={setSelected}
          minWidth={680}
          empty={<StateBlock title={filtered ? "没有符合条件的兑换" : "暂无积分商城兑换"} />}
        />
      </Section>

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => (!open ? setSelected(null) : undefined)}
        title="兑换详情"
        description={selected ? `${selected.redemptionNo}，${formatDateTime(selected.createdAt)}` : undefined}
        hero={
          selected ? (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
              <div className="flex min-w-0 flex-col gap-1">
                <span className="truncate text-base font-semibold">{selected.itemTitle}</span>
                <RedemptionStatusBadge status={selected.status} />
              </div>
              <span className="text-xl font-semibold tabular-nums">-{formatInt(selected.cost)} 积分</span>
            </div>
          ) : null
        }
        fields={
          selected
            ? [
                { label: "兑换单号", value: selected.redemptionNo, mono: true, copy: selected.redemptionNo },
                { label: "商品 ID", value: selected.itemId ? String(selected.itemId) : "已删除" },
                { label: "扣积分流水", value: selected.integralTransactionNo, mono: true, copy: selected.integralTransactionNo || null },
                { label: "退积分流水", value: selected.refundTransactionNo, mono: true, copy: selected.refundTransactionNo || null },
                { label: "处理备注", value: selected.note, wide: true },
                { label: "处理人", value: selected.handledBy },
                { label: "处理时间", value: selected.handledAt ? formatDateTime(selected.handledAt) : undefined },
                { label: "请求 ID", value: selected.requestId, mono: true, copy: selected.requestId || null },
                { label: "设备", value: selected.deviceId, mono: true, copy: selected.deviceId || null },
                { label: "IP", value: selected.clientIp, mono: true, copy: selected.clientIp || null },
                { label: "兑换时间", value: formatDateTime(selected.createdAt) },
                { label: "更新时间", value: formatDateTime(selected.updatedAt) }
              ]
            : undefined
        }
        raw={selected}
        actions={
          selected?.status === "pending" ? (
            <>
              <Button variant="outline" size="sm" onClick={() => openHandle(selected, "cancel")}>
                <Ban />
                取消并退积分
              </Button>
              <Button size="sm" onClick={() => openHandle(selected, "fulfill")}>
                <PackageCheck />
                发放
              </Button>
            </>
          ) : undefined
        }
      >
        {selected ? <ResultList results={selected.results} /> : null}
      </RecordSheet>

      <ConfirmActionDialog
        open={Boolean(handling)}
        onOpenChange={(open) => (!open ? setHandling(null) : undefined)}
        title={handling?.action === "fulfill" ? "发放兑换" : "取消兑换"}
        description={
          handling
            ? handling.action === "fulfill"
              ? `「${handling.item.itemTitle}」标记为已发放，用户会收到「兑换已发放」的站内信。`
              : `取消「${handling.item.itemTitle}」，退回 ${formatInt(handling.item.cost)} 积分并加回库存，用户会收到站内信。此操作不可撤销。`
            : undefined
        }
        confirmLabel={handling?.action === "fulfill" ? "确认发放" : "确认取消"}
        destructive={handling?.action === "cancel"}
        pending={mutation.isPending}
        note={{
          value: note,
          onChange: setNote,
          placeholder: handling?.action === "fulfill" ? "兑换码、快递单号等，会附在站内信里" : "取消原因，会附在站内信里",
          hint: "选填，写给用户看",
          max: 500
        }}
        onConfirm={submit}
      />
    </div>
  );
}
