"use client";

/**
 * 邀请记录：「谁邀请了我」（inviteeId，至多一条）与「我邀请的人」（inviterId）。
 *
 * 一条邀请关系里有两份奖励：被邀请人的奖励在建立关系时就发了；邀请人的奖励可能要等条件
 * （首次签到等），状态是 inviterStatus。只有「等待中」的邀请人奖励可以人工立即发放或取消，
 * 取消不收回被邀请人已到账的奖励。
 *
 * 指标口径：全部来自 `inviterId = 本人` 的列表总数，分别按邀请人奖励状态筛选。
 */

import * as React from "react";
import { Ban, CheckCircle2, Clock3, Gift, Gauge, UserPlus, Users } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  CODE_KIND_LABEL,
  INVITER_STATUSES,
  InviterStatusBadge,
  SOURCE_LABEL,
  inviterStatusLabel
} from "@/components/apps/invite/invite-shared";
import { useAdminToken } from "@/lib/admin-hooks";
import { listInviteRelations, type InviteRelation, type InviteSource, type InviterStatus } from "@/lib/api/invite";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useHandleInviteRelationMutation } from "@/lib/invite-hooks";
import { useUserInviteCountsQuery, useUserInviteRelationsQuery } from "@/lib/user-records-hooks";
import {
  FilterBar,
  FilterSelect,
  InfoGrid,
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
import {
  ConfirmActionDialog,
  ExportButton,
  MetricGrid,
  ResultChips,
  ResultList,
  UserChip,
  exportStamp,
  percent,
  resultsCsv,
  usePaging
} from "./records-shared";

type Pending = { relation: InviteRelation; action: "settle" | "cancel" } | null;

export function InviteRecords({ appKey, userId }: { appKey: string; userId: number }) {
  const token = useAdminToken();
  const countsQuery = useUserInviteCountsQuery(appKey, userId);
  const counts = countsQuery.data;
  const inviterQuery = useUserInviteRelationsQuery(appKey, { inviteeId: userId, page: 1, limit: 1 });
  const myInviter = inviterQuery.data?.items?.[0];

  const [status, setStatus] = React.useState("");
  const [source, setSource] = React.useState("");
  const [keyword, setKeyword] = React.useState("");
  const debouncedKeyword = useDebouncedValue(keyword.trim());
  const paging = usePaging(20);
  const filters = {
    inviterId: userId,
    status: (status || undefined) as InviterStatus | undefined,
    source: (source || undefined) as InviteSource | undefined,
    keyword: debouncedKeyword || undefined
  };
  const listQuery = useUserInviteRelationsQuery(appKey, { ...filters, page: paging.page, limit: paging.limit });

  const [selected, setSelected] = React.useState<InviteRelation | null>(null);
  const [pending, setPending] = React.useState<Pending>(null);
  const mutation = useHandleInviteRelationMutation(appKey);

  const filtered = Boolean(status || source || keyword);

  const confirm = async () => {
    if (!pending) return;
    try {
      const updated = await mutation.mutateAsync({ relationId: pending.relation.id, action: pending.action });
      toast.success(pending.action === "settle" ? "邀请人奖励已发放" : "邀请人奖励已取消");
      if (selected?.id === updated.id) setSelected(updated);
      setPending(null);
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  const rowActions = (row: InviteRelation) =>
    row.inviterStatus === "pending" ? (
      <div className="flex justify-end gap-1.5">
        <Button
          size="xs"
          variant="outline"
          onClick={(event) => {
            event.stopPropagation();
            setPending({ relation: row, action: "settle" });
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
            setPending({ relation: row, action: "cancel" });
          }}
        >
          取消
        </Button>
      </div>
    ) : null;

  const columns: Array<Column<InviteRelation>> = [
    {
      id: "invitee",
      header: "被邀请人",
      width: "200px",
      cell: (row) => (
        <UserChip
          appKey={appKey}
          userId={row.inviteeId}
          account={row.inviteeAccount}
          nickname={row.inviteeNickname}
          avatar={row.inviteeAvatar}
        />
      )
    },
    {
      id: "code",
      header: "邀请码",
      cell: (row) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-mono text-xs">{row.code || "—"}</span>
          <span className="text-xs text-muted-foreground">
            {CODE_KIND_LABEL[row.codeKind] ?? row.codeKind}，{SOURCE_LABEL[row.source] ?? row.source}
          </span>
        </div>
      )
    },
    {
      id: "inviteeResults",
      header: "被邀请人奖励",
      hideBelow: "lg",
      cell: (row) => <ResultChips results={row.inviteeResults} empty="无" />
    },
    {
      id: "inviterStatus",
      header: "我的奖励",
      cell: (row) => (
        <div className="flex flex-col items-start gap-1">
          <InviterStatusBadge status={row.inviterStatus} />
          {row.inviterResults?.length ? <ResultChips results={row.inviterResults} /> : null}
        </div>
      )
    },
    {
      id: "time",
      header: "邀请时间",
      hideBelow: "md",
      cell: (row) => (
        <span className="text-xs text-muted-foreground tabular-nums" title={formatDateTime(row.createdAt)}>
          {formatDateTime(row.createdAt)}
        </span>
      )
    },
    { id: "actions", header: "", align: "right", width: "120px", cell: rowActions }
  ];

  const csvColumns: Array<CsvColumn<InviteRelation>> = [
    { header: "关系 ID", value: (row) => row.id },
    { header: "被邀请人 ID", value: (row) => row.inviteeId },
    { header: "被邀请人账号", value: (row) => row.inviteeAccount },
    { header: "被邀请人昵称", value: (row) => row.inviteeNickname },
    { header: "邀请码", value: (row) => row.code },
    { header: "码类型", value: (row) => CODE_KIND_LABEL[row.codeKind] ?? row.codeKind },
    { header: "填写方式", value: (row) => SOURCE_LABEL[row.source] ?? row.source },
    { header: "被邀请人奖励", value: (row) => resultsCsv(row.inviteeResults) },
    { header: "邀请人奖励状态", value: (row) => inviterStatusLabel(row.inviterStatus) },
    { header: "邀请人奖励", value: (row) => resultsCsv(row.inviterResults) },
    { header: "邀请人奖励发放时间", value: (row) => formatDateTime(row.inviterGrantedAt, "") },
    { header: "处理人", value: (row) => row.handledBy },
    { header: "设备", value: (row) => row.deviceId },
    { header: "IP", value: (row) => row.clientIp },
    { header: "邀请时间", value: (row) => formatDateTime(row.createdAt, "") }
  ];

  return (
    <div className="flex flex-col gap-5">
      <MetricGrid>
        <MetricCard label="邀请人数" icon={Users} tone="primary" loading={countsQuery.isLoading} value={formatInt(counts?.total ?? 0)} sub="通过本人邀请码建立的关系" />
        <MetricCard
          label="奖励已发放"
          icon={CheckCircle2}
          tone="success"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.granted ?? 0)}
          sub={`占 ${percent(counts?.granted ?? 0, counts?.total ?? 0)}`}
        />
        <MetricCard
          label="等待发放"
          icon={Clock3}
          tone="warning"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.pending ?? 0)}
          sub="可人工发放或取消"
          onClick={
            counts?.pending
              ? () => {
                  setStatus("pending");
                  paging.reset();
                }
              : undefined
          }
        />
        <MetricCard label="已达上限" icon={Gauge} tone="info" loading={countsQuery.isLoading} value={formatInt(counts?.limited ?? 0)} sub="超出邀请奖励上限，未发放" />
      </MetricGrid>

      <Section title="我的邀请人" description="本人注册或补填时使用的邀请码" icon={UserPlus} tone="violet">
        {inviterQuery.isLoading ? (
          <div className="flex items-center gap-3">
            <Skeleton className="size-8 rounded-full" />
            <Skeleton className="h-4 w-40" />
          </div>
        ) : inviterQuery.isError ? (
          <StateBlock kind="error" compact description={errorMessage(inviterQuery.error)} onRetry={() => inviterQuery.refetch()} />
        ) : !myInviter ? (
          <StateBlock compact title="没有使用邀请码" description="该用户注册时未填写邀请码，之后也没有补填" />
        ) : (
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              {myInviter.inviterId ? (
                <UserChip
                  appKey={appKey}
                  userId={myInviter.inviterId}
                  account={myInviter.inviterAccount}
                  nickname={myInviter.inviterNickname}
                  avatar={myInviter.inviterAvatar}
                />
              ) : (
                <div className="flex items-center gap-2 text-sm">
                  <Badge variant="secondary" size="sm">
                    活动码
                  </Badge>
                  <span className="text-muted-foreground">没有关联邀请人</span>
                </div>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <Button size="sm" variant="outline" onClick={() => setSelected(myInviter)}>
                  查看详情
                </Button>
                {myInviter.inviterStatus === "pending" ? (
                  <Button size="sm" onClick={() => setPending({ relation: myInviter, action: "settle" })}>
                    发放邀请人奖励
                  </Button>
                ) : null}
              </div>
            </div>
            <InfoGrid
              columns={3}
              items={[
                { label: "邀请码", value: myInviter.code, mono: true, copy: myInviter.code },
                { label: "码类型", value: CODE_KIND_LABEL[myInviter.codeKind] ?? myInviter.codeKind },
                { label: "填写方式", value: SOURCE_LABEL[myInviter.source] ?? myInviter.source },
                { label: "本人获得", value: <ResultChips results={myInviter.inviteeResults} empty="无" /> },
                { label: "邀请人奖励", value: <InviterStatusBadge status={myInviter.inviterStatus} /> },
                { label: "建立时间", value: formatDateTime(myInviter.createdAt), hint: formatRelative(myInviter.createdAt) }
              ]}
            />
          </div>
        )}
      </Section>

      <Section
        flush
        title="我邀请的人"
        description="邀请人奖励「等待中」时可立即发放或取消"
        icon={Gift}
        tone="primary"
        actions={
          <ExportButton
            filename={`邀请记录-${userId}-${exportStamp()}`}
            columns={csvColumns}
            disabled={!token || !listQuery.data?.total}
            fetchPage={(page, limit) => listInviteRelations(token as string, appKey, { ...filters, page, limit })}
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
              placeholder="搜索账号、昵称、邀请码"
            />
            <FilterSelect
              label="奖励"
              value={status}
              onChange={(next) => {
                setStatus(next);
                paging.reset();
              }}
              options={INVITER_STATUSES.filter((item) => item !== "none").map((item) => ({ value: item, label: inviterStatusLabel(item) }))}
            />
            <FilterSelect
              label="方式"
              value={source}
              onChange={(next) => {
                setSource(next);
                paging.reset();
              }}
              options={(Object.keys(SOURCE_LABEL) as InviteSource[]).map((item) => ({ value: item, label: SOURCE_LABEL[item] }))}
            />
            <ResetButton
              visible={filtered}
              onClick={() => {
                setStatus("");
                setSource("");
                setKeyword("");
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
          minWidth={760}
          empty={<StateBlock title={filtered ? "没有符合条件的邀请" : "还没有邀请过别人"} />}
        />
      </Section>

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => (!open ? setSelected(null) : undefined)}
        title="邀请关系"
        description={selected ? `关系 #${selected.id}，${formatDateTime(selected.createdAt)}` : undefined}
        hero={
          selected ? (
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-2 rounded-lg border px-3 py-2.5">
                <span className="text-xs text-muted-foreground">邀请人</span>
                {selected.inviterId ? (
                  <UserChip
                    appKey={appKey}
                    userId={selected.inviterId}
                    account={selected.inviterAccount}
                    nickname={selected.inviterNickname}
                    avatar={selected.inviterAvatar}
                  />
                ) : (
                  <span className="text-sm text-muted-foreground">活动码，无邀请人</span>
                )}
              </div>
              <div className="flex flex-col gap-2 rounded-lg border px-3 py-2.5">
                <span className="text-xs text-muted-foreground">被邀请人</span>
                <UserChip
                  appKey={appKey}
                  userId={selected.inviteeId}
                  account={selected.inviteeAccount}
                  nickname={selected.inviteeNickname}
                  avatar={selected.inviteeAvatar}
                />
              </div>
            </div>
          ) : null
        }
        fields={
          selected
            ? [
                { label: "邀请码", value: selected.code, mono: true, copy: selected.code },
                { label: "码类型", value: CODE_KIND_LABEL[selected.codeKind] ?? selected.codeKind },
                { label: "填写方式", value: SOURCE_LABEL[selected.source] ?? selected.source },
                { label: "邀请人奖励", value: <InviterStatusBadge status={selected.inviterStatus} /> },
                { label: "邀请人奖励发放时间", value: selected.inviterGrantedAt ? formatDateTime(selected.inviterGrantedAt) : undefined },
                { label: "处理人", value: selected.handledBy },
                { label: "设备", value: selected.deviceId, mono: true, copy: selected.deviceId || null },
                { label: "IP", value: selected.clientIp, mono: true, copy: selected.clientIp || null },
                { label: "建立时间", value: formatDateTime(selected.createdAt) },
                { label: "更新时间", value: formatDateTime(selected.updatedAt) }
              ]
            : undefined
        }
        raw={selected}
        actions={
          selected?.inviterStatus === "pending" ? (
            <>
              <Button variant="outline" size="sm" onClick={() => setPending({ relation: selected, action: "cancel" })}>
                <Ban />
                取消邀请人奖励
              </Button>
              <Button size="sm" onClick={() => setPending({ relation: selected, action: "settle" })}>
                <CheckCircle2 />
                立即发放
              </Button>
            </>
          ) : undefined
        }
      >
        {selected ? (
          <>
            <ResultList title="被邀请人奖励" results={selected.inviteeResults} />
            <ResultList title="邀请人奖励" results={selected.inviterResults} />
          </>
        ) : null}
      </RecordSheet>

      <ConfirmActionDialog
        open={Boolean(pending)}
        onOpenChange={(open) => (!open ? setPending(null) : undefined)}
        title={pending?.action === "settle" ? "立即发放邀请人奖励" : "取消邀请人奖励"}
        description={
          pending?.action === "settle"
            ? "不再等待发放条件，也不检查邀请奖励上限，直接把奖励发给邀请人。"
            : "邀请人将不会获得这条邀请的奖励，被邀请人已到账的奖励不会收回。此操作不可撤销。"
        }
        confirmLabel={pending?.action === "settle" ? "确认发放" : "确认取消"}
        destructive={pending?.action === "cancel"}
        pending={mutation.isPending}
        onConfirm={confirm}
      />
    </div>
  );
}
