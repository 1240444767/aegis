"use client";

import { useMemo, useState } from "react";
import { ChevronLeft, ChevronRight, LogOut, Search, ShieldCheck, X } from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { formatTime, relativeTime } from "@/components/users/detail/user-detail-shared";
import { adminInitials, flattenAdmins } from "@/components/users/admins/admin-shared";
import { ApiError } from "@/lib/api-client";
import type { AdminSessionRecord } from "@/lib/api/types";
import {
  useAdminAccountsQuery,
  useAdminSessionsQuery,
  useAllSessionsQuery,
  useForceLogoutMutation,
  useRevokeSessionMutation
} from "@/lib/admin-hooks";
import { useLiveAdmins } from "@/lib/online-live/hooks";
import type { LiveAdminPresence } from "@/lib/online-live/types";
import { cn } from "@/lib/utils";
import { AdminSessionRow } from "./admin-session-row";
import { ConfirmActionDialog, durationSince, parseClient } from "./session-shared";

const PAGE_SIZE = 30;

type Pending =
  | { kind: "session"; session: AdminSessionRecord }
  | { kind: "admin"; admin: LiveAdminPresence };

function adminName(a: LiveAdminPresence) {
  return a.displayName || a.account || `管理员 #${a.adminId}`;
}

/** 控制台窗口的一行摘要：浏览器与系统、IP 与归属地。 */
function windowSummary(a: LiveAdminPresence) {
  const first = a.connections[0];
  if (!first) return "";
  const client = parseClient(first.userAgent).label;
  const where = [first.region || first.country, first.ip].filter(Boolean).join(" ");
  return [client, where].filter(Boolean).join("，");
}

/**
 * 管理员（仅超管）。
 *
 * 上面一排是此刻开着控制台的管理员（实时连接，来自加密长连接，上下线即时刷新）；
 * 点一位只看其登录会话（按管理员查询，拿到的是全量）。不选时是全站有效会话的分页列表。
 */
export function AdminSessionsView({
  currentSessionId,
  selfId
}: {
  currentSessionId?: string;
  /** 自己不出现「强制下线」：那等于把当前会话一起踢掉，应走退出登录 */
  selfId: number | null;
}) {
  const live = useLiveAdmins(true);
  const onlineAdmins = live.items ?? [];
  const onlineLoading = live.loading;
  const [selected, setSelected] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState("");
  const [pending, setPending] = useState<Pending | null>(null);

  const accountsQuery = useAdminAccountsQuery();
  const allQuery = useAllSessionsQuery(page, PAGE_SIZE, { enabled: selected == null, refetchInterval: 30_000 });
  const oneQuery = useAdminSessionsQuery(selected, { enabled: selected != null });
  const revokeMutation = useRevokeSessionMutation();
  const forceLogoutMutation = useForceLogoutMutation();

  const avatars = useMemo(
    () => new Map(flattenAdmins(accountsQuery.data).map((a) => [a.id, a])),
    [accountsQuery.data]
  );

  const sessions = useMemo(() => {
    const source = selected != null ? oneQuery.data ?? [] : allQuery.data?.items ?? [];
    const q = keyword.trim().toLowerCase();
    return source
      .filter((s) => !s.isRevoked)
      .filter((s) => {
        if (!q) return true;
        return [s.ip, s.adminAccount, s.adminName, s.device, parseClient(s.userAgent).label, s.id]
          .some((v) => v?.toLowerCase().includes(q));
      })
      .sort((a, b) => new Date(b.lastActiveAt).getTime() - new Date(a.lastActiveAt).getTime());
  }, [selected, oneQuery.data, allQuery.data, keyword]);

  const loading = selected != null ? oneQuery.isLoading : allQuery.isLoading;
  const total = selected != null ? sessions.length : allQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil((allQuery.data?.total ?? 0) / PAGE_SIZE));
  const totalWindows = onlineAdmins.reduce((sum, a) => sum + a.connections.length, 0);

  async function confirmPending() {
    if (!pending) return;
    try {
      if (pending.kind === "session") {
        await revokeMutation.mutateAsync(pending.session.id);
        toast.success("会话已撤销");
      } else {
        const result = await forceLogoutMutation.mutateAsync(pending.admin.adminId);
        toast.success(`已撤销 ${result?.revokedCount ?? 0} 个会话`);
        live.refresh();
        if (selected === pending.admin.adminId) setSelected(null);
      }
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }

  const pendingName =
    pending?.kind === "admin"
      ? adminName(pending.admin)
      : pending?.session.adminName || pending?.session.adminAccount || "";

  return (
    <div className="space-y-3">
      {/* 在线管理员 */}
      <div className="rounded-2xl border bg-card">
        <div className="flex items-center justify-between gap-3 border-b px-4 py-2.5">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <ShieldCheck className="size-4 text-muted-foreground" />
            控制台在线
          </h3>
          <span className="text-xs text-muted-foreground tabular-nums">
            {onlineLoading ? "" : `${onlineAdmins.length} 位，${totalWindows} 个窗口`}
          </span>
        </div>
        {onlineLoading ? (
          <div className="flex gap-2 overflow-hidden p-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[74px] w-64 shrink-0 rounded-xl" />
            ))}
          </div>
        ) : onlineAdmins.length === 0 ? (
          <p className="px-4 py-5 text-sm text-muted-foreground">{live.error || "当前没有打开控制台的管理员"}</p>
        ) : (
          <div className="flex gap-2 overflow-x-auto p-3 [scrollbar-width:thin]">
            {onlineAdmins.map((a) => {
              const active = selected === a.adminId;
              const account = avatars.get(a.adminId);
              return (
                <div
                  key={a.adminId}
                  className={cn(
                    "flex w-64 shrink-0 items-center gap-2 rounded-xl border p-2 pr-1.5 transition-colors",
                    active ? "border-foreground/30 bg-muted" : "hover:bg-muted/50"
                  )}
                >
                  <button
                    type="button"
                    aria-pressed={active}
                    className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
                    onClick={() => { setSelected(active ? null : a.adminId); setKeyword(""); }}
                  >
                    <span className="relative inline-flex shrink-0">
                      <Avatar className="size-9 rounded-xl border">
                        <AvatarImage src={a.avatar || account?.avatar} alt={adminName(a)} />
                        <AvatarFallback className="rounded-xl text-[11px] font-medium">
                          {adminInitials({ displayName: a.displayName ?? "", account: a.account ?? "" })}
                        </AvatarFallback>
                      </Avatar>
                      <span className="absolute -right-0.5 -bottom-0.5 size-3 rounded-full border-2 border-background bg-emerald-500" />
                    </span>
                    <span className="min-w-0">
                      <span className="flex min-w-0 items-center gap-1.5">
                        <span className="truncate text-sm font-medium">{adminName(a)}</span>
                        {a.isSuperAdmin ? <span className="shrink-0 text-[10px] text-muted-foreground">超管</span> : null}
                      </span>
                      <span className="block truncate text-[11px] text-muted-foreground" title={windowSummary(a)}>
                        {a.connections.length} 个窗口，在线 {durationSince(a.connectedAt) || "不足 1 分钟"}
                      </span>
                      <span className="block truncate text-[11px] text-muted-foreground" title={formatTime(a.lastSeenAt)}>
                        {windowSummary(a) || `${relativeTime(a.lastSeenAt)}心跳`}
                      </span>
                    </span>
                  </button>
                  {a.adminId === selfId ? (
                    <span className="shrink-0 px-1.5 text-[11px] text-muted-foreground">本人</span>
                  ) : (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
                      aria-label={`强制 ${adminName(a)} 下线`}
                      title="强制下线"
                      onClick={() => setPending({ kind: "admin", admin: a })}
                    >
                      <LogOut className="size-3.5" />
                    </Button>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* 会话列表 */}
      <div className="rounded-2xl border bg-card">
        <div className="flex flex-col gap-2 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 items-center gap-2">
            <h3 className="truncate text-sm font-semibold">
              {selected != null
                ? `${(() => { const a = onlineAdmins.find((x) => x.adminId === selected); return a ? adminName(a) : "所选管理员"; })()} 的会话`
                : "全部有效会话"}
            </h3>
            {selected != null ? (
              <Button variant="ghost" size="sm" className="h-7 gap-1 px-2 text-xs" onClick={() => setSelected(null)}>
                <X className="size-3" />
                查看全部
              </Button>
            ) : null}
          </div>
          <div className="relative w-full sm:w-64">
            <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="搜索管理员、IP 或客户端"
              className="h-8 pl-8 text-sm"
            />
          </div>
        </div>

        {loading ? (
          <div className="divide-y px-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex gap-3 py-3">
                <Skeleton className="size-9 rounded-xl" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-3.5 w-48" />
                  <Skeleton className="h-3 w-64 max-w-full" />
                  <Skeleton className="h-3 w-28" />
                </div>
              </div>
            ))}
          </div>
        ) : sessions.length === 0 ? (
          <p className="px-4 py-10 text-center text-sm text-muted-foreground">
            {keyword ? "没有匹配的会话" : "暂无有效会话"}
          </p>
        ) : (
          <div className="divide-y px-4">
            {sessions.map((s) => (
              <AdminSessionRow
                key={s.id}
                session={s}
                showAdmin
                current={s.id === currentSessionId}
                revoking={revokeMutation.isPending && pending?.kind === "session" && pending.session.id === s.id}
                onRevoke={(session) => setPending({ kind: "session", session })}
              />
            ))}
          </div>
        )}

        <div className="flex items-center justify-between gap-2 border-t px-4 py-2.5 text-xs text-muted-foreground">
          <span className="tabular-nums">
            {loading ? "" : keyword ? `匹配 ${sessions.length} 个` : `共 ${total.toLocaleString("zh-CN")} 个有效会话`}
          </span>
          {selected == null && totalPages > 1 ? (
            <div className="flex items-center gap-1.5">
              <Button variant="outline" size="icon" className="size-7" aria-label="上一页" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                <ChevronLeft className="size-3.5" />
              </Button>
              <span className="min-w-14 text-center tabular-nums">{page} / {totalPages}</span>
              <Button variant="outline" size="icon" className="size-7" aria-label="下一页" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
                <ChevronRight className="size-3.5" />
              </Button>
            </div>
          ) : null}
        </div>
      </div>

      <ConfirmActionDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={pending?.kind === "admin" ? `强制 ${pendingName} 下线？` : "撤销该登录会话？"}
        description={
          pending?.kind === "admin"
            ? "将撤销其全部登录会话并关闭已打开的控制台窗口，对方需要重新登录。"
            : `${pendingName ? `${pendingName} ` : ""}来自 ${pending?.session.ip || "未知 IP"} 的会话将立即失效，对方需要重新登录。`
        }
        confirmLabel={pending?.kind === "admin" ? "强制下线" : "撤销会话"}
        pending={revokeMutation.isPending || forceLogoutMutation.isPending}
        onConfirm={confirmPending}
      />
    </div>
  );
}
