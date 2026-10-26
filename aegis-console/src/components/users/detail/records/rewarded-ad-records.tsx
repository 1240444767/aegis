"use client";

/**
 * 激励广告观看记录。
 *
 * 一次观看由服务端回调与客户端上报两方确认；待确认 / 未发放的记录可以补发：
 * 补发不再等另一方、不再看限额，可选按哪个场景的奖励补发，一条记录只能补发一次。
 *
 * 指标口径：该用户的观看记录总数，以及分别按状态筛选的条数。
 */

import * as React from "react";
import { CheckCircle2, Clock3, HandCoins, MonitorPlay, XCircle } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  REWARDED_AD_STATUS_OPTIONS,
  RewardedAdStatusBadge,
  rewardedAdReasonLabel
} from "@/components/apps/rewarded-ad/rewarded-ad-shared";
import { useAdminToken } from "@/lib/admin-hooks";
import { listRewardedAdViews, type RewardedAdView, type RewardedAdViewStatus } from "@/lib/api/rewarded-ad";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useGrantRewardedAdViewMutation, useRewardedAdConfigQuery } from "@/lib/rewarded-ad-hooks";
import { useUserRewardedAdCountsQuery, useUserRewardedAdViewsQuery } from "@/lib/user-records-hooks";
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
import { ConfirmActionDialog, ExportButton, MetricGrid, ResultChips, ResultList, exportStamp, percent, resultsCsv, usePaging } from "./records-shared";

const STATUS_LABEL: Record<string, string> = Object.fromEntries(REWARDED_AD_STATUS_OPTIONS.map((item) => [item.value, item.label]));

function verifyText(view: RewardedAdView) {
  const server = view.serverVerifiedAt ? "服务端已回调" : "服务端未回调";
  const client = view.clientReportedAt ? (view.clientVerified === false ? "客户端校验未通过" : "客户端已上报") : "客户端未上报";
  return `${server}，${client}`;
}

export function RewardedAdRecords({ appKey, userId }: { appKey: string; userId: number }) {
  const token = useAdminToken();
  const countsQuery = useUserRewardedAdCountsQuery(appKey, userId);
  const counts = countsQuery.data;
  const configQuery = useRewardedAdConfigQuery(appKey);
  const scenes = React.useMemo(() => configQuery.data?.scenes ?? [], [configQuery.data]);
  const sceneName = React.useCallback(
    (key: string, fallback?: string) => fallback || scenes.find((scene) => scene.key === key)?.name || key || "—",
    [scenes]
  );

  const [status, setStatus] = React.useState("");
  const [scene, setScene] = React.useState("");
  const [keyword, setKeyword] = React.useState("");
  const debouncedKeyword = useDebouncedValue(keyword.trim());
  const [range, setRange] = React.useState<TimeRange>({ preset: "all" });
  const paging = usePaging(20);
  const time = rangeToParams(range);
  const filters = {
    userId,
    status: (status || undefined) as RewardedAdViewStatus | undefined,
    scene: scene || undefined,
    keyword: debouncedKeyword || undefined,
    start: time.from,
    end: time.to
  };
  const listQuery = useUserRewardedAdViewsQuery(appKey, { ...filters, page: paging.page, limit: paging.limit });

  const [selected, setSelected] = React.useState<RewardedAdView | null>(null);
  const [grantTarget, setGrantTarget] = React.useState<RewardedAdView | null>(null);
  const [grantScene, setGrantScene] = React.useState("");
  const grantMutation = useGrantRewardedAdViewMutation(appKey);

  const filtered = Boolean(status || scene || keyword || range.preset !== "all");

  const openGrant = (view: RewardedAdView) => {
    setGrantScene(scenes.some((item) => item.key === view.scene) ? view.scene : (scenes[0]?.key ?? ""));
    setGrantTarget(view);
  };

  const confirmGrant = async () => {
    if (!grantTarget) return;
    try {
      const updated = await grantMutation.mutateAsync({ viewId: grantTarget.id, scene: grantScene || undefined });
      toast.success("已补发");
      if (selected?.id === updated.id) setSelected(updated);
      setGrantTarget(null);
    } catch (error) {
      toast.error(errorMessage(error, "补发失败"));
    }
  };

  const columns: Array<Column<RewardedAdView>> = [
    {
      id: "time",
      header: "时间",
      cell: (row) => (
        <div className="flex flex-col">
          <span className="tabular-nums">{formatDateTime(row.createdAt)}</span>
          <span className="max-w-[180px] truncate font-mono text-xs text-muted-foreground" title={row.transId}>
            {row.transId}
          </span>
        </div>
      )
    },
    { id: "scene", header: "场景", cell: (row) => sceneName(row.scene, row.sceneName) },
    {
      id: "status",
      header: "状态",
      cell: (row) => (
        <div className="flex flex-col items-start gap-1">
          <div className="flex flex-wrap items-center gap-1">
            <RewardedAdStatusBadge status={row.status} />
            {row.conversion ? (
              <Badge variant="info" size="sm">
                已折算
              </Badge>
            ) : null}
          </div>
          {row.reason ? <span className="text-xs text-muted-foreground">{rewardedAdReasonLabel(row.reason)}</span> : null}
        </div>
      )
    },
    { id: "results", header: "奖励", hideBelow: "md", cell: (row) => <ResultChips results={row.results} /> },
    {
      id: "verify",
      header: "确认",
      hideBelow: "lg",
      cell: (row) => (
        <div className="flex flex-wrap gap-1">
          <Badge variant={row.serverVerifiedAt ? "success" : "outline"} size="sm">
            服务端
          </Badge>
          <Badge variant={row.clientReportedAt ? (row.clientVerified === false ? "danger" : "success") : "outline"} size="sm">
            客户端
          </Badge>
        </div>
      )
    },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "88px",
      cell: (row) =>
        row.status !== "granted" && row.userId ? (
          <Button
            size="xs"
            variant="outline"
            onClick={(event) => {
              event.stopPropagation();
              openGrant(row);
            }}
          >
            补发
          </Button>
        ) : null
    }
  ];

  const csvColumns: Array<CsvColumn<RewardedAdView>> = [
    { header: "记录 ID", value: (row) => row.id },
    { header: "时间", value: (row) => formatDateTime(row.createdAt, "") },
    { header: "交易 ID", value: (row) => row.transId },
    { header: "场景", value: (row) => sceneName(row.scene, row.sceneName) },
    { header: "广告位", value: (row) => row.placementId },
    { header: "状态", value: (row) => STATUS_LABEL[row.status] ?? row.status },
    { header: "原因", value: (row) => rewardedAdReasonLabel(row.reason) },
    { header: "奖励", value: (row) => resultsCsv(row.results) },
    { header: "折算", value: (row) => row.conversion?.label },
    { header: "服务端回调", value: (row) => formatDateTime(row.serverVerifiedAt, "") },
    { header: "客户端上报", value: (row) => formatDateTime(row.clientReportedAt, "") },
    { header: "客户端校验", value: (row) => (row.clientVerified === undefined ? "" : row.clientVerified ? "通过" : "未通过") },
    { header: "发放时间", value: (row) => formatDateTime(row.grantedAt, "") },
    { header: "操作人", value: (row) => row.operator },
    { header: "设备", value: (row) => row.deviceId },
    { header: "IP", value: (row) => row.clientIp }
  ];

  return (
    <div className="flex flex-col gap-5">
      <MetricGrid>
        <MetricCard
          label="观看次数"
          icon={MonitorPlay}
          tone="primary"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.total ?? 0)}
          sub={counts?.latestAt ? `最近 ${formatRelative(counts.latestAt)}` : "暂无观看"}
        />
        <MetricCard
          label="已发放"
          icon={CheckCircle2}
          tone="success"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.granted ?? 0)}
          sub={`发放率 ${percent(counts?.granted ?? 0, counts?.total ?? 0)}`}
        />
        <MetricCard
          label="待确认"
          icon={Clock3}
          tone="warning"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.pending ?? 0)}
          sub="等待另一方确认，可补发"
          onClick={
            counts?.pending
              ? () => {
                  setStatus("pending");
                  paging.reset();
                }
              : undefined
          }
        />
        <MetricCard
          label="未发放"
          icon={XCircle}
          tone="danger"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.rejected ?? 0)}
          sub="被限额或校验拒绝，可补发"
          onClick={
            counts?.rejected
              ? () => {
                  setStatus("rejected");
                  paging.reset();
                }
              : undefined
          }
        />
      </MetricGrid>

      <Section
        flush
        title="激励广告观看"
        description="未发放的记录可补发，补发不再校验限额"
        icon={MonitorPlay}
        tone="info"
        actions={
          <ExportButton
            filename={`激励广告-${userId}-${exportStamp()}`}
            columns={csvColumns}
            disabled={!token || !listQuery.data?.total}
            fetchPage={(page, limit) => listRewardedAdViews(token as string, appKey, { ...filters, page, limit })}
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
              placeholder="搜索交易 ID"
            />
            <TimeRangePicker
              value={range}
              onChange={(next) => {
                setRange(next);
                paging.reset();
              }}
            />
            <FilterSelect
              label="状态"
              value={status}
              onChange={(next) => {
                setStatus(next);
                paging.reset();
              }}
              options={REWARDED_AD_STATUS_OPTIONS}
            />
            {scenes.length ? (
              <FilterSelect
                label="场景"
                value={scene}
                onChange={(next) => {
                  setScene(next);
                  paging.reset();
                }}
                options={scenes.map((item) => ({ value: item.key, label: item.name || item.key }))}
              />
            ) : null}
            <ResetButton
              visible={filtered}
              onClick={() => {
                setStatus("");
                setScene("");
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
          minWidth={720}
          empty={<StateBlock title={filtered ? "没有符合条件的观看记录" : "暂无激励广告观看记录"} />}
        />
      </Section>

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => (!open ? setSelected(null) : undefined)}
        title="观看详情"
        description={selected ? formatDateTime(selected.createdAt) : undefined}
        hero={
          selected ? (
            <div className="flex flex-col gap-1.5 rounded-lg border px-4 py-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-base font-semibold">{sceneName(selected.scene, selected.sceneName)}</span>
                <RewardedAdStatusBadge status={selected.status} />
              </div>
              <span className="text-xs text-muted-foreground">{verifyText(selected)}</span>
              {selected.conversion ? <p className="text-xs leading-5 text-muted-foreground">{selected.conversion.message}</p> : null}
            </div>
          ) : null
        }
        fields={
          selected
            ? [
                { label: "交易 ID", value: selected.transId, mono: true, copy: selected.transId, wide: true },
                { label: "广告位", value: selected.placementId, mono: true },
                { label: "广告网络", value: selected.networkId },
                { label: "原因", value: rewardedAdReasonLabel(selected.reason) },
                {
                  label: "SDK 奖励",
                  value: selected.rewardName ? `${selected.rewardName}${selected.rewardAmount ? ` ×${selected.rewardAmount}` : ""}` : undefined
                },
                { label: "服务端回调", value: selected.serverVerifiedAt ? formatDateTime(selected.serverVerifiedAt) : undefined },
                { label: "客户端上报", value: selected.clientReportedAt ? formatDateTime(selected.clientReportedAt) : undefined },
                { label: "客户端错误", value: selected.clientError, wide: true, tone: "danger" },
                { label: "发放时间", value: selected.grantedAt ? formatDateTime(selected.grantedAt) : undefined },
                { label: "操作人", value: selected.operator },
                { label: "设备", value: selected.deviceId, mono: true, copy: selected.deviceId || null },
                { label: "IP", value: selected.clientIp, mono: true, copy: selected.clientIp || null },
                { label: "附加数据", value: selected.extra, mono: true, wide: true }
              ]
            : undefined
        }
        raw={selected}
        actions={
          selected && selected.status !== "granted" && selected.userId ? (
            <Button size="sm" onClick={() => openGrant(selected)}>
              <HandCoins />
              补发奖励
            </Button>
          ) : undefined
        }
      >
        {selected ? <ResultList results={selected.results} /> : null}
      </RecordSheet>

      <ConfirmActionDialog
        open={Boolean(grantTarget)}
        onOpenChange={(open) => (!open ? setGrantTarget(null) : undefined)}
        title="补发奖励"
        description="按所选场景的奖励直接发放，不再等待另一方确认，也不检查每日次数与冷却。每条记录只能补发一次。"
        confirmLabel="确认补发"
        pending={grantMutation.isPending}
        disabled={!grantScene}
        onConfirm={confirmGrant}
      >
        <div className="flex flex-col gap-1.5">
          <Label>奖励场景</Label>
          {scenes.length ? (
            <Select value={grantScene} onValueChange={setGrantScene}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="选择场景" />
              </SelectTrigger>
              <SelectContent>
                {scenes.map((item) => (
                  <SelectItem key={item.key} value={item.key}>
                    {item.name || item.key}
                    {item.enabled === false ? "（已停用）" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <p className="text-xs text-muted-foreground">{configQuery.isLoading ? "正在读取场景" : "激励广告未配置任何场景，无法补发"}</p>
          )}
        </div>
      </ConfirmActionDialog>
    </div>
  );
}
