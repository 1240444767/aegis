"use client";

/**
 * 用户详情 › 安全 › 第三方绑定（QQ、微信等登录渠道）。
 *
 * 解绑先按普通方式提交；账号没有密码、这是最后一个登录方式时后端拒绝（40094），
 * 弹窗原地换成强制解绑的确认，管理员再确认一次才带 force 提交。
 */

import { useState } from "react";
import { Link2, Loader2, Unlink } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { BrandIcon } from "@/components/ui/brand-icon";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api-client";
import type { OAuthBinding } from "@/lib/api/app-oauth";
import { useAppOAuthBindingsQuery, useDeleteAppOAuthBindingMutation } from "@/lib/admin-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { Pager, RecordSheet, RecordTable, Section, StateBlock, type Column } from "./kit";

/** 后端「解绑后该账号将无法登录」的保护码。 */
const LAST_LOGIN_METHOD = 40094;

type PendingUnbind = { binding: OAuthBinding; force: boolean };

function providerName(binding: OAuthBinding) {
  return binding.displayName || binding.provider;
}

function Avatar({ binding }: { binding: OAuthBinding }) {
  if (binding.avatar) {
    return (
      // eslint-disable-next-line @next/next/no-img-element -- 第三方头像是任意外链，不走 next/image 的域名白名单
      <img src={binding.avatar} alt="" referrerPolicy="no-referrer" className="size-8 shrink-0 rounded-full object-cover ring-1 ring-border" />
    );
  }
  return (
    <span className="grid size-8 shrink-0 place-items-center rounded-full bg-muted">
      <BrandIcon slug={binding.icon} className="size-4" title={providerName(binding)} />
    </span>
  );
}

export function UserOAuthBindingsPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const bindingsQuery = useAppOAuthBindingsQuery(appKey, { userId, page, pageSize });
  const unbind = useDeleteAppOAuthBindingMutation(appKey);
  const [pending, setPending] = useState<PendingUnbind | null>(null);
  const [detail, setDetail] = useState<OAuthBinding | null>(null);
  const items = bindingsQuery.data?.items ?? [];
  const total = bindingsQuery.data?.total ?? 0;

  async function handleUnbind() {
    if (!pending) return;
    const { binding, force } = pending;
    try {
      await unbind.mutateAsync({ userId, provider: binding.provider, force });
      toast.success(`已解绑${providerName(binding)}`);
      setPending(null);
      if (detail?.id === binding.id) setDetail(null);
    } catch (err) {
      if (!force && err instanceof ApiError && err.code === LAST_LOGIN_METHOD) {
        setPending({ binding, force: true });
        return;
      }
      toast.error(err instanceof ApiError ? err.message : "解绑失败");
    }
  }

  const columns: Array<Column<OAuthBinding>> = [
    {
      id: "provider",
      header: "渠道",
      cell: (b) => (
        <span className="inline-flex items-center gap-2 text-sm font-medium whitespace-nowrap">
          <BrandIcon slug={b.icon} className="size-4 shrink-0" title={providerName(b)} />
          {providerName(b)}
        </span>
      )
    },
    {
      id: "account",
      header: "第三方账号",
      cell: (b) => (
        <span className="flex min-w-0 items-center gap-2.5">
          <Avatar binding={b} />
          <span className="flex min-w-0 flex-col">
            <span className="max-w-[200px] truncate text-sm">{b.nickname || "未提供昵称"}</span>
            <span className="max-w-[200px] truncate font-mono text-xs text-muted-foreground" title={b.providerUserId}>
              {b.providerUserId}
            </span>
          </span>
        </span>
      )
    },
    {
      id: "union",
      header: "UnionID",
      hideBelow: "lg",
      cell: (b) => (
        <span className="block max-w-[180px] truncate font-mono text-xs text-muted-foreground" title={b.unionId}>
          {b.unionId || "—"}
        </span>
      )
    },
    {
      id: "created",
      header: "绑定时间",
      hideBelow: "sm",
      cell: (b) => (
        <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(b.createdAt)}>
          {formatRelative(b.createdAt)}
        </span>
      )
    },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "72px",
      cell: (b) => (
        <Button
          size="xs"
          variant="ghost"
          className="text-destructive hover:text-destructive"
          onClick={(event) => {
            event.stopPropagation();
            setPending({ binding: b, force: false });
          }}
        >
          <Unlink />
          解绑
        </Button>
      )
    }
  ];

  const pendingName = pending ? providerName(pending.binding) : "";

  return (
    <>
      <Section
        title="第三方绑定"
        description="可用于登录的第三方账号，解绑后该渠道不能再登录此账号"
        icon={Link2}
        tone="info"
        flush
        footer={total > pageSize ? (
          <Pager
            page={page}
            limit={pageSize}
            total={total}
            onPage={setPage}
            onLimit={(next) => {
              setPageSize(next);
              setPage(1);
            }}
          />
        ) : undefined}
      >
        <RecordTable
          columns={columns}
          rows={items}
          rowKey={(b) => b.id}
          loading={bindingsQuery.isLoading}
          fetching={bindingsQuery.isFetching}
          error={bindingsQuery.error}
          onRetry={() => void bindingsQuery.refetch()}
          onRowClick={setDetail}
          skeletonRows={2}
          minWidth={600}
          empty={<StateBlock compact icon={Link2} title="暂无第三方绑定" description="该用户没有绑定任何第三方登录渠道" />}
        />
      </Section>

      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => !open && setDetail(null)}
        title={detail ? `${providerName(detail)}绑定` : ""}
        description={detail ? `绑定于 ${formatDateTime(detail.createdAt)}` : undefined}
        hero={
          detail ? (
            <div className="flex items-center gap-3 rounded-xl border px-4 py-3">
              <Avatar binding={detail} />
              <div className="flex min-w-0 flex-col">
                <span className="truncate text-sm font-medium">{detail.nickname || "未提供昵称"}</span>
                <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                  <BrandIcon slug={detail.icon} className="size-3.5" title={providerName(detail)} />
                  {providerName(detail)}
                </span>
              </div>
            </div>
          ) : undefined
        }
        fields={
          detail
            ? [
                { label: "渠道标识", value: detail.provider, mono: true },
                { label: "绑定 ID", value: String(detail.id), mono: true },
                { label: "第三方用户 ID", value: detail.providerUserId, mono: true, copy: detail.providerUserId, wide: true },
                { label: "UnionID", value: detail.unionId, mono: true, copy: detail.unionId, wide: true },
                { label: "邮箱", value: detail.email, copy: detail.email },
                { label: "账号", value: detail.account },
                { label: "绑定时间", value: formatDateTime(detail.createdAt) },
                { label: "更新时间", value: formatDateTime(detail.updatedAt) }
              ]
            : undefined
        }
        raw={detail}
        actions={
          detail ? (
            <Button size="sm" variant="destructive" onClick={() => setPending({ binding: detail, force: false })}>
              <Unlink />
              解绑
            </Button>
          ) : undefined
        }
      />

      <AlertDialog open={Boolean(pending)} onOpenChange={(open) => !open && !unbind.isPending && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{pending?.force ? `强制解绑${pendingName}？` : `解绑${pendingName}？`}</AlertDialogTitle>
            <AlertDialogDescription>
              {pending?.force
                ? "该账号没有密码，这是最后一个可用的登录方式，解绑后将无法登录。"
                : `该用户将不能再用这个${pendingName}账号登录。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={unbind.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={unbind.isPending}
              onClick={(event) => {
                // 保持弹窗：40094 时要原地换成强制确认
                event.preventDefault();
                void handleUnbind();
              }}
            >
              {unbind.isPending ? <Loader2 className="animate-spin" /> : null}
              {pending?.force ? "强制解绑" : "确认解绑"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
