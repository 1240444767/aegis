"use client";

/**
 * 某个用户当前有效的会话（令牌）。
 *
 * 一张表：多选后批量下线，或一键全部下线；单行也能下线。每一种下线都先二次确认，
 * 被下线的设备下次请求即返回未登录。行点开是完整字段（令牌标识、到期时间、UA、设备字典）。
 *
 * 内网 / 回环来源不说「未知位置」：它不是查不到，而是这条会话本来就来自服务端那一侧
 * （本机调试、反代未透传真实 IP、内网客户端），写成「未知」会让排查的人去追一个不存在的地点。
 */

import { useMemo, useState } from "react";
import { Globe2, Loader2, LogOut, MonitorSmartphone, RefreshCw, Server, XCircle } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { SessionDetailView } from "@/lib/api/apps";
import { classifyIp, isServerSideAddress } from "@/lib/geo/private-network";
import {
  useAdminUserSessionsQuery,
  useRevokeAdminAppUserSessionsMutation,
  useRevokeAdminUserSessionMutation,
  useRevokeAdminUserSessionsBatchMutation
} from "@/lib/admin-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
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
import { Checkbox } from "@/components/ui/checkbox";
import { CountryFlag } from "@/components/ui/country-flag";
import { DeviceInfoCard, DeviceInfoChip } from "./device-info-chip";
import { RecordSheet, RecordTable, Section, StateBlock, type Column } from "./detail/kit";
import { describeUserAgent } from "./detail/user-detail-shared";

function locationText(s: SessionDetailView): { text: string; server: boolean; hint?: string } {
  if (isServerSideAddress(s.ip, s.isPrivate)) {
    const cls = classifyIp(s.ip);
    return { text: cls.label, server: true, hint: cls.detail };
  }
  const parts: string[] = [];
  for (const part of [s.country, s.region, s.city]) {
    if (part && parts[parts.length - 1] !== part) parts.push(part);
  }
  if (parts.length) return { text: parts.join(" "), server: false };
  return { text: s.location || "未知位置", server: false };
}

function isExpiringSoon(value?: string) {
  const t = value ? new Date(value).getTime() : NaN;
  return !Number.isNaN(t) && t - Date.now() < 86_400_000;
}

type Pending = { kind: "one"; session: SessionDetailView } | { kind: "selected" } | { kind: "all" };

export function UserSessionsPanel({ appKey, userId }: { appKey?: string | null; userId?: number | null }) {
  const sessionsQuery = useAdminUserSessionsQuery(appKey, userId);
  const revokeOne = useRevokeAdminUserSessionMutation(appKey, userId);
  const revokeBatch = useRevokeAdminUserSessionsBatchMutation(appKey, userId);
  const revokeAll = useRevokeAdminAppUserSessionsMutation(appKey, userId);

  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [pending, setPending] = useState<Pending | null>(null);
  const [detail, setDetail] = useState<SessionDetailView | null>(null);

  const sessions = useMemo(() => sessionsQuery.data?.items ?? [], [sessionsQuery.data]);
  // 列表刷新后，已经不存在的会话不再算作选中
  const liveSelected = useMemo(
    () => new Set(sessions.map((s) => s.tokenHash).filter((hash) => selected.has(hash))),
    [sessions, selected]
  );
  const allChecked = sessions.length > 0 && liveSelected.size === sessions.length;
  const someChecked = liveSelected.size > 0 && !allChecked;
  const busy = revokeOne.isPending || revokeBatch.isPending || revokeAll.isPending;

  function toggle(hash: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(hash)) next.delete(hash);
      else next.add(hash);
      return next;
    });
  }

  async function confirm() {
    if (!pending) return;
    try {
      if (pending.kind === "one") {
        await revokeOne.mutateAsync(pending.session.tokenHash);
        setSelected((prev) => {
          const next = new Set(prev);
          next.delete(pending.session.tokenHash);
          return next;
        });
        if (detail?.tokenHash === pending.session.tokenHash) setDetail(null);
        toast.success("会话已下线");
      } else if (pending.kind === "selected") {
        const result = await revokeBatch.mutateAsync(Array.from(liveSelected));
        setSelected(new Set());
        toast.success(`已下线 ${result.revoked} 个会话`);
      } else {
        await revokeAll.mutateAsync();
        setSelected(new Set());
        setDetail(null);
        toast.success("已下线全部会话");
        void sessionsQuery.refetch();
      }
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "下线失败");
    }
  }

  const columns: Array<Column<SessionDetailView>> = [
    {
      id: "select",
      width: "44px",
      header: (
        <Checkbox
          aria-label="全选"
          checked={allChecked ? true : someChecked ? "indeterminate" : false}
          onCheckedChange={() => setSelected(allChecked ? new Set() : new Set(sessions.map((s) => s.tokenHash)))}
        />
      ),
      cell: (s) => (
        <span onClick={(event) => event.stopPropagation()} className="inline-flex">
          <Checkbox aria-label="选择会话" checked={liveSelected.has(s.tokenHash)} onCheckedChange={() => toggle(s.tokenHash)} />
        </span>
      )
    },
    {
      id: "device",
      header: "设备",
      width: "26%",
      cell: (s) => {
        const ua = describeUserAgent(s.userAgent);
        return (
          <div className="max-w-[240px] min-w-0" onClick={(event) => event.stopPropagation()}>
            <DeviceInfoChip info={s.deviceInfo} fallback={s.device || ua.label} />
          </div>
        );
      }
    },
    {
      id: "location",
      header: "位置",
      cell: (s) => {
        const loc = locationText(s);
        return (
          <span className="inline-flex max-w-[200px] min-w-0 items-center gap-1.5 text-xs" title={loc.hint}>
            {loc.server ? (
              <Server className="size-3.5 shrink-0 text-violet-600 dark:text-violet-400" />
            ) : s.countryCode ? (
              <CountryFlag code={s.countryCode} size={14} className="shrink-0 rounded-[2px]" />
            ) : (
              <Globe2 className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span className="truncate">{loc.text}</span>
          </span>
        );
      }
    },
    {
      id: "ip",
      header: "IP",
      hideBelow: "md",
      cell: (s) => <span className="font-mono text-xs text-muted-foreground">{s.ip || "—"}</span>
    },
    {
      id: "provider",
      header: "方式",
      hideBelow: "lg",
      cell: (s) =>
        s.provider ? (
          <Badge variant="outline" size="sm">
            {s.provider}
          </Badge>
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        )
    },
    {
      id: "issued",
      header: "签发",
      cell: (s) => (
        <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(s.issuedAt)}>
          {formatRelative(s.issuedAt)}
        </span>
      )
    },
    {
      id: "expires",
      header: "到期",
      hideBelow: "lg",
      cell: (s) => (
        <span
          className={
            isExpiringSoon(s.expiresAt)
              ? "text-xs whitespace-nowrap text-amber-600 tabular-nums dark:text-amber-400"
              : "text-xs whitespace-nowrap text-muted-foreground tabular-nums"
          }
          title={formatDateTime(s.expiresAt)}
        >
          {formatRelative(s.expiresAt)}
        </span>
      )
    },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "72px",
      cell: (s) => (
        <Button
          size="xs"
          variant="ghost"
          className="text-destructive hover:text-destructive"
          disabled={busy}
          onClick={(event) => {
            event.stopPropagation();
            setPending({ kind: "one", session: s });
          }}
        >
          <XCircle />
          下线
        </Button>
      )
    }
  ];

  const pendingTitle =
    pending?.kind === "all"
      ? "下线全部会话？"
      : pending?.kind === "selected"
        ? `下线选中的 ${liveSelected.size} 个会话？`
        : "下线该会话？";
  const pendingText =
    pending?.kind === "all"
      ? "该用户在所有设备上的登录状态立即失效，需要重新登录。"
      : pending?.kind === "selected"
        ? "选中设备上的登录状态立即失效，其余会话不受影响。"
        : "该设备上的登录状态立即失效，下次请求时需要重新登录。";

  const detailLoc = detail ? locationText(detail) : null;
  const detailUa = detail ? describeUserAgent(detail.userAgent) : null;

  return (
    <>
      <Section
        title="活跃会话"
        description="当前仍然有效的登录令牌，下线后对应设备需要重新登录"
        icon={MonitorSmartphone}
        tone="success"
        flush
        actions={
          <>
            {liveSelected.size > 0 ? (
              <Button size="sm" variant="outline" className="text-destructive hover:text-destructive" disabled={busy} onClick={() => setPending({ kind: "selected" })}>
                <XCircle />
                下线选中（{liveSelected.size}）
              </Button>
            ) : null}
            <Button size="sm" variant="outline" disabled={sessionsQuery.isFetching} onClick={() => void sessionsQuery.refetch()} aria-label="刷新">
              <RefreshCw className={sessionsQuery.isFetching ? "animate-spin" : undefined} />
              刷新
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="text-destructive hover:text-destructive"
              disabled={busy || !sessions.length}
              onClick={() => setPending({ kind: "all" })}
            >
              <LogOut />
              全部下线
            </Button>
          </>
        }
        footer={
          sessions.length ? (
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
              <span className="tabular-nums">共 {sessions.length} 个会话</span>
              {liveSelected.size ? <span className="tabular-nums">已选 {liveSelected.size} 个</span> : null}
            </div>
          ) : undefined
        }
      >
        <RecordTable
          columns={columns}
          rows={sessions}
          rowKey={(s) => s.tokenHash}
          loading={sessionsQuery.isLoading}
          fetching={sessionsQuery.isFetching}
          error={sessionsQuery.error}
          onRetry={() => void sessionsQuery.refetch()}
          onRowClick={setDetail}
          skeletonRows={3}
          minWidth={760}
          rowClassName={(s) => (liveSelected.has(s.tokenHash) ? "bg-muted/30" : undefined)}
          empty={<StateBlock compact icon={MonitorSmartphone} title="暂无活跃会话" description="该用户当前没有有效的登录令牌" />}
        />
      </Section>

      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => !open && setDetail(null)}
        title="会话详情"
        description={detail ? `签发于 ${formatDateTime(detail.issuedAt)}` : undefined}
        hero={
          detail?.deviceInfo ? (
            <div className="overflow-hidden rounded-xl border">
              <DeviceInfoCard info={detail.deviceInfo} />
            </div>
          ) : undefined
        }
        fields={
          detail
            ? [
                { label: "设备", value: detail.deviceInfo?.name || detail.device || detailUa?.label },
                { label: "登录方式", value: detail.provider },
                { label: "位置", value: detailLoc?.text, hint: detailLoc?.hint },
                { label: "运营商", value: detail.isp },
                { label: "IP", value: detail.ip, mono: true, copy: detail.ip },
                { label: "设备标识", value: detail.deviceId, mono: true, copy: detail.deviceId },
                { label: "签发时间", value: formatDateTime(detail.issuedAt), hint: formatRelative(detail.issuedAt) },
                {
                  label: "到期时间",
                  value: formatDateTime(detail.expiresAt),
                  hint: formatRelative(detail.expiresAt),
                  tone: isExpiringSoon(detail.expiresAt) ? "warning" : undefined
                },
                { label: "令牌 ID", value: detail.tokenId, mono: true, copy: detail.tokenId, wide: true },
                { label: "令牌摘要", value: detail.tokenHash, mono: true, copy: detail.tokenHash, wide: true },
                { label: "User-Agent", value: detail.userAgent, mono: true, copy: detail.userAgent, wide: true }
              ]
            : undefined
        }
        raw={detail}
        actions={
          detail ? (
            <Button size="sm" variant="destructive" disabled={busy} onClick={() => setPending({ kind: "one", session: detail })}>
              <XCircle />
              下线该会话
            </Button>
          ) : undefined
        }
      />

      <AlertDialog open={Boolean(pending)} onOpenChange={(open) => !open && !busy && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{pendingTitle}</AlertDialogTitle>
            <AlertDialogDescription>{pendingText}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={busy}
              onClick={(event) => {
                event.preventDefault();
                void confirm();
              }}
            >
              {busy ? <Loader2 className="animate-spin" /> : null}
              确认下线
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
