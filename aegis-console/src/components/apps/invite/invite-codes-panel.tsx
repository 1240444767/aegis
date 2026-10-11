"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { ListTree, Loader2, Pencil, Plus, TicketCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { InviteCodeEditor } from "@/components/apps/invite/invite-code-editor";
import {
  CODE_KIND_LABEL,
  InvitePager,
  codeAvailability,
  formatCount,
  validityLabel
} from "@/components/apps/invite/invite-shared";
import { RewardBadges } from "@/components/apps/points-store/points-store-shared";
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
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type { InviteAdminCode, InviteCodeKind, InviteCodeStatus } from "@/lib/api/invite";
import {
  useDeleteInviteCodeMutation,
  useInviteCatalogQuery,
  useInviteCodesQuery,
  useSetInviteCodeStatusMutation
} from "@/lib/invite-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

const LIMIT = 20;

/**
 * 邀请码：个人码与活动码同一张表。
 *
 * 个人码由用户打开邀请页时生成，这里只能停用（违规推广时用），不能删除：
 * 删了主人下次打开邀请页又会生成一个新的。活动码可创建、编辑、启停与删除，
 * 删除后邀请记录保留码的快照。
 */
export function InviteCodesPanel({
  appKey,
  onOpenRelations
}: {
  appKey: string;
  onOpenRelations: (code: { id: number; code: string }) => void;
}) {
  const [kind, setKind] = useState<InviteCodeKind | undefined>(undefined);
  const [status, setStatus] = useState<InviteCodeStatus | undefined>(undefined);
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [editor, setEditor] = useState<{ open: boolean; code: InviteAdminCode | null }>({ open: false, code: null });
  const [pendingDelete, setPendingDelete] = useState<InviteAdminCode | null>(null);

  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);
  const listQuery = useInviteCodesQuery(appKey, {
    kind,
    status,
    keyword: debouncedKeyword || undefined,
    page,
    limit: LIMIT
  });
  const catalogQuery = useInviteCatalogQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const statusMutation = useSetInviteCodeStatusMutation(appKey);
  const deleteMutation = useDeleteInviteCodeMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? 1);
  const filtered = Boolean(keyword || kind || status);
  const catalog = catalogQuery.data?.rewards ?? [];
  const plans = plansQuery.data ?? [];
  const features = featuresQuery.data ?? [];

  const userHref = (userId: number) => `/app-users/${encodeURIComponent(appKey)}/${userId}`;

  const toggleStatus = async (code: InviteAdminCode, active: boolean) => {
    try {
      await statusMutation.mutateAsync({ codeId: code.id, active });
      toast.success(active ? `已启用 ${code.code}` : `已停用 ${code.code}`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await deleteMutation.mutateAsync(pendingDelete.id);
      toast.success(`已删除 ${pendingDelete.code}`);
      setPendingDelete(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  return (
    <SectionCard
      icon={<TicketCheck className="size-4" />}
      title="邀请码"
      aside={
        <Button size="sm" onClick={() => setEditor({ open: true, code: null })}>
          <Plus className="size-3.5" /> 新建活动码
        </Button>
      }
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索邀请码、活动名称或账号"
          className="h-8 w-56"
        />
        <Select
          value={kind ?? "all"}
          onValueChange={(value) => {
            setKind(value === "all" ? undefined : (value as InviteCodeKind));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部类型</SelectItem>
            <SelectItem value="personal">个人码</SelectItem>
            <SelectItem value="campaign">活动码</SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            setStatus(value === "all" ? undefined : (value as InviteCodeStatus));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="active">已启用</SelectItem>
            <SelectItem value="disabled">已停用</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配邀请码" : "暂无邀请码"}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>邀请码</TableHead>
                <TableHead>归属</TableHead>
                <TableHead>被邀请人奖励</TableHead>
                <TableHead className="text-right">已使用</TableHead>
                <TableHead>有效期</TableHead>
                <TableHead>启用</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((code) => {
                const availability = codeAvailability(code);
                const campaign = code.kind === "campaign";
                const ownerLabel = code.ownerNickname || code.ownerAccount || (code.ownerUserId ? `#${code.ownerUserId}` : "");
                return (
                  <TableRow key={`${code.kind}-${code.id}-${code.code}`}>
                    <TableCell>
                      <p className="font-mono text-xs font-semibold tracking-wider">{code.code}</p>
                      <div className="mt-1 flex flex-wrap items-center gap-1">
                        <Badge variant={campaign ? "info" : "outline"} size="sm">
                          {CODE_KIND_LABEL[code.kind] ?? code.kind}
                        </Badge>
                        {code.custom ? (
                          <Badge variant="outline" size="sm">
                            自定义
                          </Badge>
                        ) : null}
                        {availability ? (
                          <Badge variant={availability.variant} size="sm">
                            {availability.label}
                          </Badge>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-56 text-xs">
                      {campaign ? <p className="truncate font-medium">{code.title || "未命名"}</p> : null}
                      {code.ownerUserId ? (
                        <p className={campaign ? "mt-0.5 truncate text-[11px] text-muted-foreground" : "truncate"}>
                          {campaign ? "推广人 " : null}
                          <Link href={userHref(code.ownerUserId)} className="hover:underline">
                            {ownerLabel}
                          </Link>
                        </p>
                      ) : campaign ? (
                        <p className="mt-0.5 text-[11px] text-muted-foreground">无推广人</p>
                      ) : null}
                      {code.note ? <p className="mt-0.5 truncate text-[11px] text-muted-foreground">{code.note}</p> : null}
                    </TableCell>
                    <TableCell className="max-w-64">
                      {!campaign || code.rewards === undefined || code.rewards === null ? (
                        <span className="text-xs text-muted-foreground">跟随默认</span>
                      ) : code.rewards.length === 0 ? (
                        <span className="text-xs text-muted-foreground">不发奖励</span>
                      ) : (
                        <RewardBadges rewards={code.rewards} catalog={catalog} plans={plans} features={features} />
                      )}
                    </TableCell>
                    <TableCell className="text-right text-xs tabular-nums">
                      {formatCount(code.usedCount)}
                      {code.maxUses ? <span className="text-muted-foreground"> / {formatCount(code.maxUses)}</span> : null}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                      {campaign ? validityLabel(code.startsAt, code.endsAt) : "长期"}
                    </TableCell>
                    <TableCell>
                      {code.id > 0 ? (
                        <Switch
                          checked={code.status === "active"}
                          onCheckedChange={(next) => void toggleStatus(code, next)}
                          disabled={statusMutation.isPending}
                          aria-label="启用"
                        />
                      ) : null}
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        {code.id > 0 ? (
                          <Button
                            size="sm"
                            variant="ghost"
                            className="h-7"
                            onClick={() => onOpenRelations({ id: code.id, code: code.code })}
                          >
                            <ListTree className="size-3.5" /> 记录
                          </Button>
                        ) : null}
                        {campaign ? (
                          <>
                            <Button size="sm" variant="ghost" className="h-7" onClick={() => setEditor({ open: true, code })}>
                              <Pencil className="size-3.5" /> 编辑
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              className="h-7"
                              onClick={() => setPendingDelete(code)}
                              aria-label="删除活动码"
                            >
                              <Trash2 className="size-3.5" />
                            </Button>
                          </>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <InvitePager total={total} page={page} totalPages={totalPages} onPage={setPage} />

      <InviteCodeEditor
        appKey={appKey}
        code={editor.code}
        open={editor.open}
        onOpenChange={(open) => setEditor((current) => ({ open, code: open ? current.code : null }))}
      />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除活动码 {pendingDelete?.code}？</AlertDialogTitle>
            <AlertDialogDescription>
              删除后该码不能再填写，邀请记录保留。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmDelete()} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  );
}
