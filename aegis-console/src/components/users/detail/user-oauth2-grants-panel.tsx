"use client";

/**
 * 用户详情 › 安全 › OAuth2 授权。
 *
 * 该用户经本应用的 OAuth2 授权服务（Hydra）授权过的客户端。撤销某一个会吊销它持有的令牌；
 * 「全部撤销」另外让用户在授权服务上的登录态失效，下次授权必须重新登录。
 * 授权服务未启用时只给一句说明，不渲染空表。
 */

import { useState, type ReactNode } from "react";
import { ExternalLink, KeyRound, Loader2, LogOut, Network } from "lucide-react";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { OAuth2Grant } from "@/lib/api/oauth2-server";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useOAuth2OverviewQuery, useRevokeUserOAuth2GrantsMutation, useUserOAuth2GrantsQuery } from "@/lib/oauth2-server-hooks";
import { RecordSheet, RecordTable, Section, StateBlock, type Column } from "./kit";

function ClientLogo({ grant }: { grant: OAuth2Grant }) {
  if (grant.logoUri) {
    return (
      // eslint-disable-next-line @next/next/no-img-element -- 接入方自填的任意外链，不走 next/image 的域名白名单
      <img src={grant.logoUri} alt="" className="size-8 shrink-0 rounded-lg object-cover ring-1 ring-border" />
    );
  }
  return (
    <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
      <KeyRound className="size-4" />
    </span>
  );
}

function Scopes({ scopes, limit }: { scopes: string[]; limit?: number }) {
  const shown = limit ? scopes.slice(0, limit) : scopes;
  return (
    <span className="flex flex-wrap items-center gap-1">
      {shown.map((scope) => (
        <Badge key={scope} variant="outline" size="sm" className="font-mono">
          {scope}
        </Badge>
      ))}
      {limit && scopes.length > limit ? <span className="text-xs text-muted-foreground">另 {scopes.length - limit} 项</span> : null}
    </span>
  );
}

export function UserOAuth2GrantsPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const overviewQuery = useOAuth2OverviewQuery(appKey);
  const usable = Boolean(overviewQuery.data?.enabled && overviewQuery.data.ready);
  const grantsQuery = useUserOAuth2GrantsQuery(appKey, userId, usable);
  const revoke = useRevokeUserOAuth2GrantsMutation(appKey, userId);
  const [pending, setPending] = useState<OAuth2Grant | "all" | null>(null);
  const [detail, setDetail] = useState<OAuth2Grant | null>(null);
  const grants = grantsQuery.data?.items ?? [];

  async function handleRevoke() {
    if (!pending) return;
    try {
      await revoke.mutateAsync(pending === "all" ? undefined : pending.clientId);
      toast.success(pending === "all" ? "已撤销全部授权" : `已撤销对「${pending.clientName || pending.clientId}」的授权`);
      if (pending === "all" || detail?.clientId === pending.clientId) setDetail(null);
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "撤销失败");
    }
  }

  const columns: Array<Column<OAuth2Grant>> = [
    {
      id: "client",
      header: "客户端",
      cell: (g) => (
        <span className="flex min-w-0 items-center gap-2.5">
          <ClientLogo grant={g} />
          <span className="flex min-w-0 flex-col">
            <span className="max-w-[220px] truncate text-sm font-medium">{g.clientName || g.clientId}</span>
            <span className="max-w-[220px] truncate font-mono text-xs text-muted-foreground">{g.clientId}</span>
          </span>
        </span>
      )
    },
    { id: "scopes", header: "授权范围", hideBelow: "md", cell: (g) => <Scopes scopes={g.scopes} limit={4} /> },
    {
      id: "granted",
      header: "授权时间",
      cell: (g) => (
        <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(g.grantedAt)}>
          {formatRelative(g.grantedAt)}
        </span>
      )
    },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "72px",
      cell: (g) => (
        <Button
          size="xs"
          variant="ghost"
          className="text-destructive hover:text-destructive"
          onClick={(event) => {
            event.stopPropagation();
            setPending(g);
          }}
        >
          撤销
        </Button>
      )
    }
  ];

  let body: ReactNode;
  if (overviewQuery.isLoading) {
    body = (
      <div className="px-4 py-4 sm:px-5">
        <Skeleton className="h-10 w-full" />
      </div>
    );
  } else if (overviewQuery.isError) {
    body = <StateBlock compact kind="error" description={overviewQuery.error instanceof Error ? overviewQuery.error.message : undefined} onRetry={() => void overviewQuery.refetch()} />;
  } else if (!usable) {
    body = (
      <StateBlock
        compact
        icon={Network}
        title={overviewQuery.data?.enabled ? "授权服务未就绪" : "授权服务未启用"}
        description="本应用的 OAuth2 授权服务可用后，这里会列出该用户授权过的客户端"
      />
    );
  } else {
    body = (
      <RecordTable
        columns={columns}
        rows={grants}
        rowKey={(g) => g.clientId}
        loading={grantsQuery.isLoading}
        fetching={grantsQuery.isFetching}
        error={grantsQuery.error}
        onRetry={() => void grantsQuery.refetch()}
        onRowClick={setDetail}
        skeletonRows={2}
        minWidth={560}
        empty={<StateBlock compact icon={Network} title="暂无授权记录" description="该用户没有通过 OAuth2 授权过任何客户端" />}
      />
    );
  }

  return (
    <>
      <Section
        title="OAuth2 授权"
        description="该用户经本应用授权服务登录过的第三方客户端"
        icon={Network}
        tone="violet"
        flush
        actions={
          usable && grants.length > 0 ? (
            <Button size="sm" variant="outline" className="text-destructive hover:text-destructive" onClick={() => setPending("all")}>
              <LogOut />
              全部撤销
            </Button>
          ) : null
        }
        footer={usable && grants.length ? <span className="text-xs text-muted-foreground tabular-nums">共 {grants.length} 个客户端</span> : undefined}
      >
        {body}
      </Section>

      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => !open && setDetail(null)}
        title={detail?.clientName || detail?.clientId || ""}
        description={detail ? `授权于 ${formatDateTime(detail.grantedAt)}` : undefined}
        hero={
          detail ? (
            <div className="flex items-center gap-3 rounded-xl border px-4 py-3">
              <ClientLogo grant={detail} />
              <div className="flex min-w-0 flex-col">
                <span className="truncate text-sm font-medium">{detail.clientName || detail.clientId}</span>
                <span className="truncate font-mono text-xs text-muted-foreground">{detail.clientId}</span>
              </div>
            </div>
          ) : undefined
        }
        fields={
          detail
            ? [
                { label: "客户端 ID", value: detail.clientId, mono: true, copy: detail.clientId, wide: true },
                {
                  label: "主页",
                  value: detail.clientUri ? (
                    <a href={detail.clientUri} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1 text-primary hover:underline">
                      {detail.clientUri}
                      <ExternalLink className="size-3" />
                    </a>
                  ) : undefined,
                  wide: true
                },
                { label: "授权范围", value: detail.scopes.length ? <Scopes scopes={detail.scopes} /> : undefined, wide: true },
                { label: "授权时间", value: formatDateTime(detail.grantedAt), hint: formatRelative(detail.grantedAt) }
              ]
            : undefined
        }
        raw={detail}
        actions={
          detail ? (
            <Button size="sm" variant="destructive" onClick={() => setPending(detail)}>
              撤销授权
            </Button>
          ) : undefined
        }
      />

      <AlertDialog open={Boolean(pending)} onOpenChange={(open) => !open && !revoke.isPending && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pending === "all" ? "撤销全部授权？" : `撤销对「${pending?.clientName || pending?.clientId}」的授权？`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pending === "all"
                ? "全部客户端持有的令牌立即失效，该用户在授权服务上的登录状态一并清除。"
                : "该客户端持有的访问令牌与刷新令牌立即失效，用户下次使用时需要重新授权。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revoke.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={revoke.isPending}
              onClick={(event) => {
                event.preventDefault();
                void handleRevoke();
              }}
            >
              {revoke.isPending ? <Loader2 className="animate-spin" /> : null}
              确认撤销
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
