"use client";

/**
 * 会员记录：每一笔开通、延长、扣减、取消永久、退款冲正。
 * 已作废的记录（revokedAt）整行淡化并标「已作废」，不再贡献权益。
 */

import * as React from "react";
import { CalendarClock, Crown, Download, Infinity as InfinityIcon, Loader2, Receipt, SlidersHorizontal } from "lucide-react";
import { toast } from "sonner";
import { formatVipTerm, isLifetimeGrant, vipSourceLabel } from "@/components/apps/vip/vip-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useAdminToken } from "@/lib/admin-hooks";
import { getAdminVipTransactions } from "@/lib/api/configuration";
import type { AdminAppUserDetail, VipTransaction } from "@/lib/api/types";
import { useAdminUserVipTransactionsQuery } from "@/lib/app-user-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import {
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  Section,
  StateBlock,
  collectPages,
  downloadCsv,
  errorMessage,
  type Column
} from "../kit";
import { formatMoney, isPast } from "../user-detail-shared";

export function VipLedgerView({
  appKey,
  userId,
  user,
  onManage
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  onManage?: () => void;
}) {
  const token = useAdminToken();
  const [page, setPage] = React.useState(1);
  const [limit, setLimit] = React.useState(20);
  const query = useAdminUserVipTransactionsQuery(appKey, userId, { page, limit });
  const data = query.data;
  const [selected, setSelected] = React.useState<VipTransaction | null>(null);
  const [shown, setShown] = React.useState<VipTransaction | null>(null);
  const [exporting, setExporting] = React.useState(false);
  const txn = selected ?? shown;

  const lifetime = Boolean(user.vipLifetimeAt);
  const active = Boolean(user.vipExpireAt) && !isPast(user.vipExpireAt);

  async function exportCsv() {
    if (!token) return;
    setExporting(true);
    try {
      const { rows, total, truncated } = await collectPages((p, l) =>
        getAdminVipTransactions(token, appKey, { userId, page: p, limit: l })
      );
      downloadCsv(`会员记录-${userId}`, [
        { header: "时间", value: (r: VipTransaction) => formatDateTime(r.createdAt) },
        { header: "流水号", value: (r) => r.transactionNo },
        { header: "套餐", value: (r) => r.planName },
        { header: "时长", value: (r) => formatVipTerm(r as Parameters<typeof formatVipTerm>[0]) },
        { header: "渠道", value: (r) => vipSourceLabel(r.payChannel) },
        { header: "金额", value: (r) => r.payAmount },
        { header: "到期前", value: (r) => formatDateTime(r.expireBefore) },
        { header: "到期后", value: (r) => formatDateTime(r.expireAfter) },
        { header: "赠送积分", value: (r) => r.bonusIntegral },
        { header: "操作人", value: (r) => r.operator },
        { header: "作废时间", value: (r) => formatDateTime(r.revokedAt, "") },
        { header: "作废原因", value: (r) => r.revokeReason }
      ], rows);
      toast.success(truncated ? `已导出前 ${rows.length} 条（共 ${total} 条）` : `已导出 ${rows.length} 条`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  }

  const columns: Array<Column<VipTransaction>> = [
    {
      id: "time",
      header: "时间",
      width: "150px",
      cell: (row) => (
        <div className="flex flex-col">
          <span className="tabular-nums">{formatDateTime(row.createdAt)}</span>
          <span className="text-xs text-muted-foreground">{formatRelative(row.createdAt)}</span>
        </div>
      )
    },
    {
      id: "plan",
      header: "套餐",
      cell: (row) => (
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="truncate font-medium">{row.planName || "—"}</span>
          {row.revokedAt ? <Badge variant="outline" size="sm">已作废</Badge> : null}
          {isLifetimeGrant(row as Parameters<typeof isLifetimeGrant>[0]) ? (
            <Badge variant="warning" size="sm">永久</Badge>
          ) : null}
        </div>
      )
    },
    {
      id: "term",
      header: "时长",
      align: "right",
      width: "90px",
      cell: (row) => <span className="tabular-nums">{formatVipTerm(row as Parameters<typeof formatVipTerm>[0])}</span>
    },
    { id: "channel", header: "渠道", width: "110px", hideBelow: "sm", cell: (row) => vipSourceLabel(row.payChannel) },
    {
      id: "amount",
      header: "金额",
      align: "right",
      width: "100px",
      hideBelow: "md",
      cell: (row) => <span className="tabular-nums">¥ {formatMoney(row.payAmount)}</span>
    },
    {
      id: "expire",
      header: "到期变化",
      width: "200px",
      hideBelow: "lg",
      cell: (row) => (
        <span className="text-xs text-muted-foreground tabular-nums">
          {row.expireAfter ? `${formatDateTime(row.expireBefore, "无")} → ${formatDateTime(row.expireAfter)}` : "—"}
        </span>
      )
    }
  ];

  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-3">
        <MetricCard
          label="会员状态"
          value={lifetime ? "永久会员" : active ? "会员中" : user.vipExpireAt ? "已过期" : "非会员"}
          icon={Crown}
          tone={lifetime || active ? "success" : user.vipExpireAt ? "warning" : "neutral"}
          sub={lifetime ? `开通于 ${formatDateTime(user.vipLifetimeAt)}` : undefined}
        />
        <MetricCard
          label="限时到期"
          value={user.vipExpireAt ? formatRelative(user.vipExpireAt) : "—"}
          icon={CalendarClock}
          tone={active ? "info" : "neutral"}
          sub={user.vipExpireAt ? formatDateTime(user.vipExpireAt) : "从未开通限时会员"}
        />
        <MetricCard
          label="记录笔数"
          value={data ? data.total.toLocaleString("zh-CN") : "—"}
          icon={InfinityIcon}
          loading={query.isLoading}
          className="col-span-2 xl:col-span-1"
        />
      </div>

      <Section
        title="会员记录"
        icon={Receipt}
        flush
        actions={
          <>
            {onManage ? (
              <Button size="sm" variant="outline" onClick={onManage}>
                <SlidersHorizontal />
                管理会员
              </Button>
            ) : null}
            <Button size="sm" variant="outline" disabled={exporting || !data?.total} onClick={exportCsv}>
              {exporting ? <Loader2 className="animate-spin" /> : <Download />}
              导出 CSV
            </Button>
          </>
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
          rowKey={(row) => row.id}
          loading={query.isLoading}
          fetching={query.isFetching}
          error={query.error}
          onRetry={() => query.refetch()}
          onRowClick={(row) => {
            setSelected(row);
            setShown(row);
          }}
          rowClassName={(row) => (row.revokedAt ? "opacity-60" : undefined)}
          empty={<StateBlock icon={Crown} title="还没有会员记录" />}
        />
      </Section>

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(value) => !value && setSelected(null)}
        title={txn?.planName || "会员记录"}
        description={txn ? vipSourceLabel(txn.payChannel) : undefined}
        fields={
          txn
            ? [
                { label: "流水号", value: txn.transactionNo, mono: true, copy: txn.transactionNo, wide: true },
                { label: "时长", value: formatVipTerm(txn as Parameters<typeof formatVipTerm>[0]) },
                { label: "金额", value: `¥ ${formatMoney(txn.payAmount)}` },
                { label: "到期前", value: formatDateTime(txn.expireBefore) },
                { label: "到期后", value: formatDateTime(txn.expireAfter) },
                { label: "赠送积分", value: txn.bonusIntegral ? String(txn.bonusIntegral) : undefined },
                { label: "关联订单", value: txn.relatedOrderNo, mono: true, copy: txn.relatedOrderNo || null },
                { label: "功能", value: txn.features?.length ? txn.features.join("、") : undefined, wide: true },
                { label: "操作人", value: txn.operator },
                { label: "时间", value: formatDateTime(txn.createdAt) },
                { label: "作废时间", value: txn.revokedAt ? formatDateTime(txn.revokedAt) : undefined, tone: "danger" },
                { label: "作废原因", value: txn.revokeReason }
              ]
            : []
        }
        raw={txn ?? undefined}
      />
    </div>
  );
}
