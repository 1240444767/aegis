"use client";

/**
 * 卡密兑换记录（核销流水）。
 *
 * 来源：用户兑换 / 授权卡激活 / 管理员代兑。列表的「发放结果」是这次核销实际发了什么，
 * 排障看它而不是卡上配置的权益。可以替该用户代兑一张卡（与卡密区块的代兑同一接口）。
 *
 * 指标口径：该用户的核销记录总数，以及分别按来源筛选的条数。
 */

import * as React from "react";
import { KeyRound, Loader2, ShieldCheck, Ticket, UserCog } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { CardKindBadge, cardKindLabel, redemptionSourceLabel } from "@/components/apps/card-key/card-key-shared";
import { useAdminToken } from "@/lib/admin-hooks";
import { listCardKeyRedemptions, type CardKeyRedemption, type CardKeySource } from "@/lib/api/card-key";
import { useAdminRedeemCardKeyMutation } from "@/lib/card-key-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useUserCardKeyCountsQuery, useUserCardKeyRedemptionsQuery } from "@/lib/user-records-hooks";
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
  TimeRangePicker,
  errorMessage,
  formatInt,
  rangeToParams,
  useDebouncedValue,
  type Column,
  type CsvColumn,
  type TimeRange
} from "../kit";
import { describeUserAgent } from "../user-detail-shared";
import { ExportButton, MetricGrid, ResultChips, ResultList, exportStamp, resultsCsv, usePaging } from "./records-shared";

const SOURCES: CardKeySource[] = ["redeem", "login", "admin"];

export function CardKeyRecords({ appKey, userId, account }: { appKey: string; userId: number; account?: string }) {
  const token = useAdminToken();
  const countsQuery = useUserCardKeyCountsQuery(appKey, userId);
  const counts = countsQuery.data;

  const [source, setSource] = React.useState("");
  const [keyword, setKeyword] = React.useState("");
  const debouncedKeyword = useDebouncedValue(keyword.trim());
  const [range, setRange] = React.useState<TimeRange>({ preset: "all" });
  const paging = usePaging(20);
  const time = rangeToParams(range);
  const filters = {
    userId,
    source: (source || undefined) as CardKeySource | undefined,
    keyword: debouncedKeyword || undefined,
    from: time.from,
    to: time.to
  };
  const listQuery = useUserCardKeyRedemptionsQuery(appKey, { ...filters, page: paging.page, limit: paging.limit });
  const [selected, setSelected] = React.useState<CardKeyRedemption | null>(null);
  const [redeemOpen, setRedeemOpen] = React.useState(false);

  const filtered = Boolean(source || keyword || range.preset !== "all");

  const columns: Array<Column<CardKeyRedemption>> = [
    {
      id: "time",
      header: "时间",
      cell: (row) => <span className="tabular-nums">{formatDateTime(row.createdAt)}</span>
    },
    {
      id: "code",
      header: "卡密",
      cell: (row) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-mono text-xs">{row.code}</span>
          <span className="truncate text-xs text-muted-foreground">{row.batchName || (row.batchId ? `批次 #${row.batchId}` : "—")}</span>
        </div>
      )
    },
    { id: "kind", header: "类型", hideBelow: "sm", cell: (row) => (row.kind ? <CardKindBadge kind={row.kind} /> : "—") },
    {
      id: "source",
      header: "来源",
      cell: (row) => (
        <span className="text-xs">
          {redemptionSourceLabel(row.source)}
          {row.source === "admin" && row.operator ? <span className="text-muted-foreground">（{row.operator}）</span> : null}
        </span>
      )
    },
    { id: "results", header: "发放结果", hideBelow: "md", cell: (row) => <ResultChips results={row.results} /> },
    {
      id: "ip",
      header: "IP",
      hideBelow: "lg",
      cell: (row) => <span className="font-mono text-xs text-muted-foreground">{row.clientIp || "—"}</span>
    }
  ];

  const csvColumns: Array<CsvColumn<CardKeyRedemption>> = [
    { header: "记录 ID", value: (row) => row.id },
    { header: "时间", value: (row) => formatDateTime(row.createdAt, "") },
    { header: "卡密", value: (row) => row.code },
    { header: "批次", value: (row) => row.batchName || row.batchId },
    { header: "类型", value: (row) => (row.kind ? cardKindLabel(row.kind) : "") },
    { header: "来源", value: (row) => redemptionSourceLabel(row.source) },
    { header: "操作人", value: (row) => row.operator },
    { header: "发放结果", value: (row) => resultsCsv(row.results) },
    { header: "设备", value: (row) => row.deviceId },
    { header: "IP", value: (row) => row.clientIp },
    { header: "UA", value: (row) => row.userAgent }
  ];

  return (
    <div className="flex flex-col gap-5">
      <MetricGrid>
        <MetricCard
          label="兑换次数"
          icon={Ticket}
          tone="primary"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.total ?? 0)}
          sub={counts?.latestAt ? `最近 ${formatRelative(counts.latestAt)}` : "暂无兑换"}
        />
        <MetricCard label="用户兑换" icon={KeyRound} tone="success" loading={countsQuery.isLoading} value={formatInt(counts?.redeem ?? 0)} sub="兑换卡核销" />
        <MetricCard label="授权卡激活" icon={ShieldCheck} tone="info" loading={countsQuery.isLoading} value={formatInt(counts?.login ?? 0)} sub="卡密登录时激活" />
        <MetricCard label="管理员代兑" icon={UserCog} tone="violet" loading={countsQuery.isLoading} value={formatInt(counts?.admin ?? 0)} sub="后台替用户核销" />
      </MetricGrid>

      <Section
        flush
        title="卡密兑换记录"
        description="每次核销实际发放的权益"
        icon={Ticket}
        tone="primary"
        actions={
          <>
            <ExportButton
              filename={`卡密兑换-${userId}-${exportStamp()}`}
              columns={csvColumns}
              disabled={!token || !listQuery.data?.total}
              fetchPage={(page, limit) => listCardKeyRedemptions(token as string, appKey, { ...filters, page, limit })}
            />
            <Button size="sm" onClick={() => setRedeemOpen(true)}>
              <KeyRound />
              代兑卡密
            </Button>
          </>
        }
        toolbar={
          <FilterBar>
            <SearchField
              value={keyword}
              onChange={(next) => {
                setKeyword(next);
                paging.reset();
              }}
              placeholder="搜索卡密"
            />
            <TimeRangePicker
              value={range}
              onChange={(next) => {
                setRange(next);
                paging.reset();
              }}
            />
            <FilterSelect
              label="来源"
              value={source}
              onChange={(next) => {
                setSource(next);
                paging.reset();
              }}
              options={SOURCES.map((item) => ({ value: item, label: redemptionSourceLabel(item) }))}
            />
            <ResetButton
              visible={filtered}
              onClick={() => {
                setSource("");
                setKeyword("");
                setRange({ preset: "all" });
                paging.reset();
              }}
            />
          </FilterBar>
        }
        footer={
          listQuery.data?.total ? (
            <Pager page={paging.page} limit={paging.limit} total={listQuery.data.total} onPage={paging.setPage} onLimit={paging.setLimit} />
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
          empty={<StateBlock title={filtered ? "没有符合条件的兑换" : "暂无卡密兑换记录"} />}
        />
      </Section>

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => (!open ? setSelected(null) : undefined)}
        title="兑换详情"
        description={selected ? formatDateTime(selected.createdAt) : undefined}
        hero={
          selected ? (
            <div className="flex flex-col gap-1.5 rounded-lg border px-4 py-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-base font-semibold break-all">{selected.code}</span>
                {selected.kind ? <CardKindBadge kind={selected.kind} /> : null}
              </div>
              <span className="text-xs text-muted-foreground">{redemptionSourceLabel(selected.source)}</span>
            </div>
          ) : null
        }
        fields={
          selected
            ? [
                { label: "卡密", value: selected.code, mono: true, copy: selected.code },
                { label: "卡 ID", value: String(selected.cardKeyId), mono: true },
                { label: "批次", value: selected.batchName || (selected.batchId ? `#${selected.batchId}` : undefined) },
                { label: "来源", value: redemptionSourceLabel(selected.source) },
                { label: "操作人", value: selected.operator },
                { label: "兑换时间", value: formatDateTime(selected.createdAt) },
                { label: "设备", value: selected.deviceId, mono: true, copy: selected.deviceId || null },
                { label: "IP", value: selected.clientIp, mono: true, copy: selected.clientIp || null },
                {
                  label: "客户端",
                  value: selected.userAgent ? describeUserAgent(selected.userAgent).label : undefined,
                  hint: selected.userAgent,
                  wide: true
                }
              ]
            : undefined
        }
        raw={selected}
      >
        {selected ? <ResultList results={selected.results} /> : null}
      </RecordSheet>

      <AdminRedeemDialog open={redeemOpen} onOpenChange={setRedeemOpen} appKey={appKey} userId={userId} account={account} />
    </div>
  );
}

function AdminRedeemDialog({
  open,
  onOpenChange,
  appKey,
  userId,
  account
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  userId: number;
  account?: string;
}) {
  const [code, setCode] = React.useState("");
  const mutation = useAdminRedeemCardKeyMutation(appKey);
  const trimmed = code.trim();

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!trimmed) return;
    try {
      const result = await mutation.mutateAsync({ code: trimmed, userId });
      const summary = result.results?.map((item) => item.detail || item.label).filter(Boolean).join("，");
      toast.success(summary ? `已代兑：${summary}` : "已代兑");
      setCode("");
      onOpenChange(false);
    } catch (error) {
      toast.error(errorMessage(error, "代兑失败"));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (!mutation.isPending ? onOpenChange(next) : undefined)}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>代兑卡密</DialogTitle>
            <DialogDescription>
              替 {account || `用户 #${userId}`} 核销一张卡，权益直接发到该账号，记录来源为「管理员代兑」。核销后不可撤销。
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="admin-redeem-code">卡密</Label>
            <Input
              id="admin-redeem-code"
              value={code}
              autoFocus
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
              placeholder="输入完整卡密"
              onChange={(event) => setCode(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
              取消
            </Button>
            <Button type="submit" disabled={!trimmed || mutation.isPending}>
              {mutation.isPending ? <Loader2 className="animate-spin" /> : null}
              确认代兑
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
