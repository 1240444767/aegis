"use client";

import { useState, type ComponentType } from "react";
import {
  Gavel,
  History,
  Hourglass,
  Loader2,
  ShieldAlert,
  ShieldBan,
  ShieldCheck,
  Trash2,
  Undo2
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  BanDurationField,
  BanReasonField,
  BanScopeField,
  describeBanDraft,
  useBanDraft,
  useNow
} from "@/components/users/ban-form";
import { ApiError } from "@/lib/api-client";
import { useUpdateAdminAppUserStatusMutation } from "@/lib/admin-hooks";
import {
  useAdminUserBansQuery,
  useCreateAdminUserBanMutation,
  useRevokeAdminUserBanMutation
} from "@/lib/app-user-hooks";
import type {
  AccountBanCounts,
  AdminAppUserDetail,
  AdminUserBan
} from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  BAN_SCOPE_LABEL,
  BAN_TYPE_LABEL,
  BanStatusBadge,
  Panel,
  ValueTree,
  formatShortTime,
  formatTime,
  isPast,
  relativeTime,
  textValue
} from "./user-detail-shared";

/** 被新封禁顶替时后端写在旧记录上的撤销原因，与 pgrepo.AccountBanSupersededReason 一致。 */
const SUPERSEDED_REASON = "已被新的封禁替代";

const HISTORY_PAGE_SIZE = 10;

type HistoryFilter = "all" | "active" | "expired" | "revoked";

const HISTORY_FILTERS: Array<{ value: HistoryFilter; label: string; count: keyof AccountBanCounts }> = [
  { value: "all", label: "全部", count: "total" },
  { value: "active", label: "生效中", count: "active" },
  { value: "expired", label: "已到期", count: "expired" },
  { value: "revoked", label: "已解除", count: "revoked" }
];

/**
 * 处置：这个账号现在被怎么处置着、要怎么处置、以前被怎么处置过。
 *
 * 一切处置都落成封禁记录（app_user_bans）：有类型、范围、起止、操作人，可解除、留痕、到期自动失效。
 * 旧版另有一张「账号开关」卡片直接翻 users.enabled，后端早已把它映射成封禁记录，
 * 两个入口做的是同一件事却长得不一样，这里只留一个。
 *
 * 只有迁移来的老数据可能「受限却没有封禁记录」，状态卡为它单独留了解除入口。
 */
export function UserGovernanceTab({
  appKey,
  userId,
  user,
  activeBan,
  onDelete,
  deletePending
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  activeBan?: AdminUserBan | null;
  onDelete: () => void;
  deletePending: boolean;
}) {
  const now = useNow();
  const [filter, setFilter] = useState<HistoryFilter>("all");
  const [page, setPage] = useState(1);
  const bansQuery = useAdminUserBansQuery(appKey, userId, {
    status: filter === "all" ? undefined : filter,
    page,
    limit: HISTORY_PAGE_SIZE
  });
  const counts = bansQuery.data?.counts;
  const [revoking, setRevoking] = useState<AdminUserBan | null>(null);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const openRevoke = (ban: AdminUserBan) => {
    setRevoking(ban);
    setRevokeOpen(true);
  };

  return (
    <div className="space-y-5">
      <StatusCard
        appKey={appKey}
        userId={userId}
        user={user}
        activeBan={activeBan ?? null}
        counts={counts}
        now={now}
        onRevoke={openRevoke}
      />

      <BanComposer appKey={appKey} userId={userId} activeBan={activeBan ?? null} now={now} />

      <Panel
        title="封禁记录"
        icon={<History className="size-4" />}
        action={
          <ToggleGroup
            type="single"
            value={filter}
            onValueChange={(value) => {
              if (!value) return;
              setFilter(value as HistoryFilter);
              setPage(1);
            }}
            className="rounded-lg border bg-muted/40 p-0.5"
          >
            {HISTORY_FILTERS.map((item) => (
              <ToggleGroupItem
                key={item.value}
                value={item.value}
                className="h-7 gap-1 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm"
              >
                {item.label}
                {counts ? (
                  <span className="tabular-nums text-muted-foreground">{counts[item.count]}</span>
                ) : null}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
        bodyClassName="px-0 py-0 sm:px-0"
      >
        <BanTimeline
          loading={bansQuery.isLoading}
          bans={bansQuery.data?.items ?? []}
          emptyText={filter === "all" ? "这个账号没有被封禁过" : "没有符合条件的记录"}
          onRevoke={openRevoke}
        />
        {(bansQuery.data?.totalPages ?? 0) > 1 ? (
          <div className="flex items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground sm:px-5">
            <span className="tabular-nums">
              第 {page} / {bansQuery.data?.totalPages} 页，共 {bansQuery.data?.total} 条
            </span>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                disabled={page <= 1 || bansQuery.isFetching}
                onClick={() => setPage((value) => value - 1)}
              >
                上一页
              </Button>
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                disabled={page >= (bansQuery.data?.totalPages ?? 1) || bansQuery.isFetching}
                onClick={() => setPage((value) => value + 1)}
              >
                下一页
              </Button>
            </div>
          </div>
        ) : null}
      </Panel>

      <Card className="flex flex-col gap-3 border-destructive/30 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-5">
        <div className="flex items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-destructive/10 text-destructive">
            <Trash2 className="size-4" />
          </span>
          <div className="min-w-0">
            <div className="text-sm font-semibold">删除用户</div>
            <div className="text-xs text-muted-foreground">账号与其资产、会话、记录一并删除，无法恢复</div>
          </div>
        </div>
        <Button size="sm" variant="destructive" disabled={deletePending} onClick={onDelete} className="self-start sm:self-auto">
          {deletePending ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
          删除用户
        </Button>
      </Card>

      <RevokeBanDialog
        appKey={appKey}
        userId={userId}
        ban={revoking}
        open={revokeOpen}
        onOpenChange={setRevokeOpen}
      />
    </div>
  );
}

// ── 当前状态 ──────────────────────────

function StatusCard({
  appKey,
  userId,
  user,
  activeBan,
  counts,
  now,
  onRevoke
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  activeBan: AdminUserBan | null;
  counts?: AccountBanCounts;
  now: number;
  onRevoke: (ban: AdminUserBan) => void;
}) {
  if (activeBan) {
    return <ActiveBanCard ban={activeBan} now={now} onRevoke={() => onRevoke(activeBan)} />;
  }
  const legacyRestricted =
    user.enabled === false || (Boolean(user.disabledEndTime) && !isPast(user.disabledEndTime));
  if (legacyRestricted) {
    return <LegacyRestrictionCard appKey={appKey} userId={userId} user={user} />;
  }

  const total = counts?.total ?? 0;
  return (
    <Card className="flex flex-col gap-4 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-5">
      <div className="flex items-center gap-3">
        <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-emerald-500/10 text-emerald-600 ring-1 ring-inset ring-emerald-500/20 dark:text-emerald-400">
          <ShieldCheck className="size-5" />
        </span>
        <div className="min-w-0">
          <div className="text-base font-semibold">账号状态正常</div>
          <div className="text-xs text-muted-foreground">没有生效中的封禁，可正常登录与访问</div>
        </div>
      </div>
      <div className="flex gap-6 sm:text-right">
        <Stat label="历史封禁" value={counts ? `${total} 次` : "—"} />
        <Stat label="其中已解除" value={counts ? `${counts.revoked} 次` : "—"} />
      </div>
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="text-sm font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function ActiveBanCard({ ban, now, onRevoke }: { ban: AdminUserBan; now: number; onRevoke: () => void }) {
  const permanent = ban.banType === "permanent";
  const start = new Date(ban.startAt).getTime();
  const end = ban.endAt ? new Date(ban.endAt).getTime() : null;
  const progress =
    end && end > start ? Math.min(100, Math.max(0, ((now - start) / (end - start)) * 100)) : null;

  return (
    <Card className="gap-0 overflow-hidden border-red-200 py-0 dark:border-red-900/60">
      <div className="flex flex-col gap-4 bg-red-500/[0.04] px-4 py-4 sm:flex-row sm:items-start sm:px-5 sm:py-5">
        <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-red-500/10 text-red-600 ring-1 ring-inset ring-red-500/20 dark:text-red-400">
          <ShieldBan className="size-5" />
        </span>
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-base font-semibold">
              {BAN_TYPE_LABEL[ban.banType] ?? ban.banType}封禁中
            </span>
            <Badge variant="danger" size="sm">
              {BAN_SCOPE_LABEL[ban.banScope] ?? ban.banScope}
            </Badge>
          </div>
          <p className="break-words text-sm leading-6 text-foreground/90">{textValue(ban.reason, "未填写原因")}</p>
        </div>
        <Button size="sm" variant="outline" className="shrink-0 self-start bg-background" onClick={onRevoke}>
          <Undo2 className="size-3.5" />
          解除封禁
        </Button>
      </div>

      <div className="grid grid-cols-2 gap-x-6 gap-y-3 border-t border-red-200/70 px-4 py-3.5 sm:grid-cols-4 sm:px-5 dark:border-red-900/40">
        <Fact label="开始" value={formatTime(ban.startAt)} />
        <Fact label="解除" value={permanent ? "永久，需手动解除" : formatTime(ban.endAt)} />
        <Fact label="剩余" value={permanent ? "—" : relativeTime(ban.endAt).replace(/后$/, "")} />
        <Fact label="操作人" value={textValue(ban.bannedByAdminName, "系统")} />
      </div>
      {progress !== null ? (
        <div className="h-1 w-full bg-red-500/10">
          <div className="h-full bg-red-500/70 transition-[width] duration-500" style={{ width: `${progress}%` }} />
        </div>
      ) : null}
    </Card>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="truncate text-sm tabular-nums" title={value}>
        {value}
      </div>
    </div>
  );
}

/** 迁移来的老数据：users 上标着受限，却没有对应的封禁记录。 */
function LegacyRestrictionCard({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  const mutation = useUpdateAdminAppUserStatusMutation(appKey, userId);

  async function lift() {
    try {
      await mutation.mutateAsync({ enabled: true, clearDisabledEndTime: true });
      toast.success("已解除限制");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解除失败");
    }
  }

  return (
    <Card className="flex flex-col gap-4 border-amber-200 bg-amber-500/[0.04] px-4 py-4 sm:flex-row sm:items-center sm:px-5 dark:border-amber-900/60">
      <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-amber-500/10 text-amber-600 ring-1 ring-inset ring-amber-500/20 dark:text-amber-400">
        <ShieldAlert className="size-5" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-base font-semibold">账号受限，无封禁记录</div>
        <div className="text-xs leading-5 text-muted-foreground">
          {textValue(user.disabledReason, "未填写原因")}
          {user.disabledEndTime ? `，${formatTime(user.disabledEndTime)} 解除` : ""}
        </div>
      </div>
      <Button size="sm" variant="outline" className="shrink-0 self-start bg-background sm:self-auto" disabled={mutation.isPending} onClick={lift}>
        {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Undo2 className="size-3.5" />}
        解除限制
      </Button>
    </Card>
  );
}

// ── 新建封禁 ──────────────────────────

function BanComposer({
  appKey,
  userId,
  activeBan,
  now
}: {
  appKey: string;
  userId: number;
  activeBan: AdminUserBan | null;
  now: number;
}) {
  const mutation = useCreateAdminUserBanMutation(appKey, userId);
  const draft = useBanDraft();
  const problem = draft.validate(now);

  async function submit() {
    const submitAt = Date.now();
    const error = draft.validate(submitAt);
    if (error) {
      toast.error(error);
      return;
    }
    try {
      await mutation.mutateAsync(draft.toPayload(submitAt));
      draft.reset();
      toast.success(activeBan ? "已替换为新的封禁" : "封禁已生效");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "封禁失败");
    }
  }

  return (
    <Panel
      title={activeBan ? "替换当前封禁" : "新建封禁"}
      description={activeBan ? "提交后当前封禁立即失效，以新的封禁为准" : undefined}
      icon={<Gavel className="size-4" />}
      bodyClassName="p-0 sm:px-0"
    >
      <div className="grid gap-6 px-4 py-5 sm:px-5 lg:grid-cols-2">
        <div className="space-y-5">
          <BanDurationField draft={draft} now={now} />
          <BanScopeField draft={draft} />
        </div>
        <BanReasonField draft={draft} rows={5} />
      </div>
      <div className="flex flex-col gap-3 border-t bg-muted/30 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5">
        <span className="text-xs text-muted-foreground">{problem ?? describeBanDraft(draft)}</span>
        <Button
          size="sm"
          variant="destructive"
          disabled={Boolean(problem) || mutation.isPending}
          onClick={submit}
          className="self-end sm:self-auto"
        >
          {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Gavel className="size-3.5" />}
          {activeBan ? "替换封禁" : "执行封禁"}
        </Button>
      </div>
    </Panel>
  );
}

// ── 记录 ──────────────────────────

const STATUS_DOT: Record<string, { icon: ComponentType<{ className?: string }>; className: string }> = {
  active: { icon: ShieldBan, className: "bg-red-500/10 text-red-600 dark:text-red-400" },
  expired: { icon: Hourglass, className: "bg-muted text-muted-foreground" },
  revoked: { icon: Undo2, className: "bg-sky-500/10 text-sky-600 dark:text-sky-400" }
};

function BanTimeline({
  loading,
  bans,
  emptyText,
  onRevoke
}: {
  loading: boolean;
  bans: AdminUserBan[];
  emptyText: string;
  onRevoke: (ban: AdminUserBan) => void;
}) {
  if (loading) {
    return (
      <div className="space-y-5 px-4 py-5 sm:px-5">
        {[0, 1, 2].map((i) => (
          <div key={i} className="flex gap-3">
            <Skeleton className="size-7 shrink-0 rounded-full" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="h-4 w-full max-w-md" />
              <Skeleton className="h-3 w-64" />
            </div>
          </div>
        ))}
      </div>
    );
  }
  if (!bans.length) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 px-4 py-12 text-center">
        <span className="grid size-10 place-items-center rounded-xl bg-muted text-muted-foreground">
          <History className="size-4" />
        </span>
        <p className="text-sm text-muted-foreground">{emptyText}</p>
      </div>
    );
  }
  return (
    <ol className="px-4 py-5 sm:px-5">
      {bans.map((ban, index) => (
        <BanEntry key={ban.id} ban={ban} last={index === bans.length - 1} onRevoke={() => onRevoke(ban)} />
      ))}
    </ol>
  );
}

function BanEntry({ ban, last, onRevoke }: { ban: AdminUserBan; last: boolean; onRevoke: () => void }) {
  const dot = STATUS_DOT[ban.status] ?? STATUS_DOT.expired;
  const Icon = dot.icon;
  const hasEvidence = ban.evidence && Object.keys(ban.evidence).length > 0;
  const superseded = ban.status === "revoked" && ban.revokeReason === SUPERSEDED_REASON;

  return (
    <li className={cn("relative flex gap-3", !last && "pb-6")}>
      {!last ? <span aria-hidden className="absolute bottom-0 left-3.5 top-8 w-px bg-border" /> : null}
      <span className={cn("relative grid size-7 shrink-0 place-items-center rounded-full", dot.className)}>
        <Icon className="size-3.5" />
      </span>

      <div className="min-w-0 flex-1 space-y-1.5 pt-0.5">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-sm font-medium">{BAN_TYPE_LABEL[ban.banType] ?? ban.banType}封禁</span>
          <Badge variant="outline" size="sm">
            {BAN_SCOPE_LABEL[ban.banScope] ?? ban.banScope}
          </Badge>
          <BanStatusBadge status={ban.status} />
          <span className="ml-auto text-xs tabular-nums text-muted-foreground" title={formatTime(ban.createdAt)}>
            {relativeTime(ban.createdAt)}
          </span>
        </div>

        <p className="break-words text-sm leading-6">{textValue(ban.reason, "未填写原因")}</p>

        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="tabular-nums" title={`${formatTime(ban.startAt)} 至 ${ban.endAt ? formatTime(ban.endAt) : "永久"}`}>
            {formatShortTime(ban.startAt)} 至 {ban.endAt ? formatShortTime(ban.endAt) : "永久"}
          </span>
          <span>操作人 {textValue(ban.bannedByAdminName, "系统")}</span>
        </div>

        {ban.status === "revoked" ? (
          <div className="rounded-lg bg-muted/50 px-3 py-2 text-xs leading-5 text-muted-foreground">
            {superseded ? (
              <>于 {formatShortTime(ban.revokedAt)} 被新的封禁替代</>
            ) : (
              <>
                {textValue(ban.revokedByAdminName, "系统")} 于 {formatShortTime(ban.revokedAt)} 解除
                {ban.revokeReason ? `：${ban.revokeReason}` : ""}
              </>
            )}
          </div>
        ) : null}

        {hasEvidence ? (
          <details className="group text-xs">
            <summary className="cursor-pointer select-none text-muted-foreground transition-colors hover:text-foreground">
              查看证据
            </summary>
            <div className="mt-1.5 rounded-lg bg-muted/40 px-3 py-2">
              <ValueTree value={ban.evidence} />
            </div>
          </details>
        ) : null}
      </div>

      {ban.status === "active" ? (
        <Button size="sm" variant="ghost" className="h-7 shrink-0 gap-1 px-2 text-xs" onClick={onRevoke}>
          <Undo2 className="size-3" />
          解除
        </Button>
      ) : null}
    </li>
  );
}

// ── 解除 ──────────────────────────

function RevokeBanDialog({
  appKey,
  userId,
  ban,
  open,
  onOpenChange
}: {
  appKey: string;
  userId: number;
  /** 关闭后仍保留上一条，关闭动画期间内容不会闪空 */
  ban: AdminUserBan | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const revoke = useRevokeAdminUserBanMutation(appKey, userId);
  const [reason, setReason] = useState("");

  async function submit() {
    if (!ban) return;
    try {
      await revoke.mutateAsync({ banId: ban.id, reason: reason.trim() || undefined });
      setReason("");
      onOpenChange(false);
      toast.success("封禁已解除");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解除失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>解除封禁</DialogTitle>
          <DialogDescription>解除后立即生效，用户可以重新登录。</DialogDescription>
        </DialogHeader>
        {ban ? (
          <div className="rounded-xl border bg-muted/30 px-3 py-2.5 text-xs leading-5">
            <div className="font-medium">
              {BAN_TYPE_LABEL[ban.banType] ?? ban.banType}封禁，{BAN_SCOPE_LABEL[ban.banScope] ?? ban.banScope}
            </div>
            <div className="text-muted-foreground">{textValue(ban.reason, "未填写原因")}</div>
          </div>
        ) : null}
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">解除原因（可选）</Label>
          <Textarea
            rows={3}
            value={reason}
            maxLength={500}
            placeholder="例如：申诉通过"
            onChange={(event) => setReason(event.target.value)}
            className="resize-none"
          />
        </div>
        <DialogFooter>
          <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={revoke.isPending} onClick={submit}>
            {revoke.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Undo2 className="size-3.5" />}
            确认解除
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
