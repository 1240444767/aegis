"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { ArrowRight, Ban, BadgeCheck, ListTree, Loader2, X } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import {
  CODE_KIND_LABEL,
  INVITER_STATUSES,
  InvitePager,
  InviterStatusBadge,
  ResultBadges,
  SOURCE_LABEL,
  inviterStatusLabel
} from "@/components/apps/invite/invite-shared";
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
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type { InviteRelation, InviteSource, InviterStatus } from "@/lib/api/invite";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useHandleInviteRelationMutation, useInviteRelationsQuery } from "@/lib/invite-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;

/** 从概览或邀请码带过来的范围：某个码、某个邀请人 */
export type InviteRelationScope = {
  code?: { id: number; code: string };
  inviter?: { id: number; label: string };
};

/**
 * 邀请记录。
 *
 * 展示双方实际拿到了什么（results），不是配置里的奖励。等待中的邀请人奖励可以在这里
 * 立即发放（不看发放条件与上限）或取消；被邀请人已到账的奖励不收回。
 */
export function InviteRelationsPanel({
  appKey,
  scope,
  status,
  onScopeChange,
  onStatusChange
}: {
  appKey: string;
  scope: InviteRelationScope;
  status?: InviterStatus;
  onScopeChange: (scope: InviteRelationScope) => void;
  onStatusChange: (status?: InviterStatus) => void;
}) {
  const [source, setSource] = useState<InviteSource | undefined>(undefined);
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [handling, setHandling] = useState<{ item: InviteRelation; action: "settle" | "cancel" } | null>(null);

  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);
  const listQuery = useInviteRelationsQuery(appKey, {
    codeId: scope.code?.id,
    inviterId: scope.inviter?.id,
    status,
    source,
    keyword: debouncedKeyword || undefined,
    page,
    limit: LIMIT
  });
  const handleMutation = useHandleInviteRelationMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? 1);
  const filtered = Boolean(keyword || status || source || scope.code || scope.inviter);

  const submitHandle = async () => {
    if (!handling) return;
    try {
      await handleMutation.mutateAsync({ relationId: handling.item.id, action: handling.action });
      toast.success(handling.action === "settle" ? "邀请人奖励已发放" : "已取消");
      setHandling(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const handlingInviter = handling
    ? handling.item.inviterNickname || handling.item.inviterAccount || `#${handling.item.inviterId ?? ""}`
    : "";

  return (
    <SectionCard icon={<ListTree className="size-4" />} title="邀请记录">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索账号、昵称或邀请码"
          className="h-8 w-56"
        />
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            onStatusChange(value === "all" ? undefined : (value as InviterStatus));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部邀请人奖励</SelectItem>
            {INVITER_STATUSES.map((item) => (
              <SelectItem key={item} value={item}>
                {inviterStatusLabel(item)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={source ?? "all"}
          onValueChange={(value) => {
            setSource(value === "all" ? undefined : (value as InviteSource));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部来源</SelectItem>
            <SelectItem value="register">{SOURCE_LABEL.register}</SelectItem>
            <SelectItem value="bind">{SOURCE_LABEL.bind}</SelectItem>
          </SelectContent>
        </Select>
        {scope.code ? (
          <ScopeChip
            label={`邀请码 ${scope.code.code}`}
            onClear={() => {
              onScopeChange({ ...scope, code: undefined });
              setPage(1);
            }}
          />
        ) : null}
        {scope.inviter ? (
          <ScopeChip
            label={`邀请人 ${scope.inviter.label}`}
            onClear={() => {
              onScopeChange({ ...scope, inviter: undefined });
              setPage(1);
            }}
          />
        ) : null}
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配记录" : "暂无邀请记录"}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>被邀请人与邀请人</TableHead>
                <TableHead>邀请码</TableHead>
                <TableHead>被邀请人奖励</TableHead>
                <TableHead>邀请人奖励</TableHead>
                <TableHead className="text-right">时间</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <RelationUser
                        appKey={appKey}
                        userId={item.inviteeId}
                        account={item.inviteeAccount}
                        nickname={item.inviteeNickname}
                        avatar={item.inviteeAvatar}
                      />
                      <ArrowRight className="size-3.5 shrink-0 text-muted-foreground" />
                      {item.inviterId ? (
                        <RelationUser
                          appKey={appKey}
                          userId={item.inviterId}
                          account={item.inviterAccount}
                          nickname={item.inviterNickname}
                          avatar={item.inviterAvatar}
                        />
                      ) : (
                        <span className="text-xs text-muted-foreground">无邀请人</span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    <p className="font-mono text-xs font-semibold tracking-wider">{item.code}</p>
                    <div className="mt-1 flex flex-wrap gap-1">
                      <Badge variant={item.codeKind === "campaign" ? "info" : "outline"} size="sm">
                        {CODE_KIND_LABEL[item.codeKind] ?? item.codeKind}
                      </Badge>
                      <Badge variant="outline" size="sm" className="font-normal">
                        {SOURCE_LABEL[item.source] ?? item.source}
                      </Badge>
                    </div>
                  </TableCell>
                  <TableCell className="max-w-64">
                    <ResultBadges results={item.inviteeResults ?? []} />
                  </TableCell>
                  <TableCell className="max-w-64">
                    <InviterStatusBadge status={item.inviterStatus} />
                    {item.inviterResults?.length ? (
                      <div className="mt-1">
                        <ResultBadges results={item.inviterResults} />
                      </div>
                    ) : null}
                    {item.handledBy ? (
                      <p className="mt-0.5 text-[11px] text-muted-foreground">{item.handledBy}</p>
                    ) : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-right text-xs text-muted-foreground">
                    {formatDateTimeMinute(item.createdAt)}
                    {item.inviterGrantedAt ? (
                      <p className="mt-0.5 text-[11px]">结算 {formatDateTimeMinute(item.inviterGrantedAt)}</p>
                    ) : null}
                    {item.clientIp ? <p className="mt-0.5 font-mono text-[11px]">{item.clientIp}</p> : null}
                  </TableCell>
                  <TableCell>
                    {item.inviterStatus === "pending" ? (
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7"
                          onClick={() => setHandling({ item, action: "settle" })}
                        >
                          <BadgeCheck className="size-3.5" /> 立即发放
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7"
                          onClick={() => setHandling({ item, action: "cancel" })}
                        >
                          <Ban className="size-3.5" /> 取消
                        </Button>
                      </div>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <InvitePager total={total} page={page} totalPages={totalPages} onPage={setPage} />

      <AlertDialog open={Boolean(handling)} onOpenChange={(open) => !open && setHandling(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {handling?.action === "settle" ? `立即向 ${handlingInviter} 发放邀请人奖励？` : `取消 ${handlingInviter} 的邀请人奖励？`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {handling?.action === "settle"
                ? "不再等待发放条件，也不受每日与累计上限限制。"
                : "取消后不再发放，被邀请人已到账的奖励不收回。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>返回</AlertDialogCancel>
            <AlertDialogAction onClick={() => void submitHandle()} disabled={handleMutation.isPending}>
              {handleMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {handling?.action === "settle" ? "确认发放" : "确认取消"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  );
}

function RelationUser({
  appKey,
  userId,
  account,
  nickname,
  avatar
}: {
  appKey: string;
  userId: number;
  account?: string;
  nickname?: string;
  avatar?: string;
}) {
  const name = nickname || account || `#${userId}`;
  return (
    <Link
      href={`/app-users/${encodeURIComponent(appKey)}/${userId}`}
      className="group flex min-w-0 items-center gap-2"
    >
      <Avatar className="size-7 rounded-lg border">
        <AvatarImage src={avatar} alt={name} />
        <AvatarFallback className="rounded-lg text-[10px]">{name.slice(0, 2)}</AvatarFallback>
      </Avatar>
      <span className="min-w-0">
        <span className="block max-w-32 truncate text-xs font-medium group-hover:underline">{name}</span>
        {nickname && account ? (
          <span className="block max-w-32 truncate font-mono text-[11px] text-muted-foreground">{account}</span>
        ) : null}
      </span>
    </Link>
  );
}

function ScopeChip({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <Badge variant="secondary" className="h-8 gap-1 pr-1 font-normal">
      {label}
      <button
        type="button"
        onClick={onClear}
        aria-label="清除筛选"
        className="rounded-sm p-0.5 text-muted-foreground transition-colors hover:text-foreground"
      >
        <X className="size-3" />
      </button>
    </Badge>
  );
}
