"use client";

import { useState } from "react";
import { KeyRound } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useAIGatewayKeysQuery, useSetAIGatewayKeyStatusMutation } from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";

const PAGE_SIZE = 20;
const ALL = "__all__";

/**
 * 用户自己领的 API 密钥。管理端只能启用 / 停用，不能新建（密钥属于用户，明文只给本人看一次）。
 * 已删除（revoked）的密钥默认不列，筛「已删除」时才出现。
 */
export function AIGatewayKeysPanel({
  appKey,
  onOpenRequests
}: {
  appKey: string;
  onOpenRequests: (filter: { userId?: number; keyId?: number }) => void;
}) {
  const [user, setUser] = useState<PickedUser | null>(null);
  const [status, setStatus] = useState<string>(ALL);
  const [page, setPage] = useState(1);
  const query = useAIGatewayKeysQuery(appKey, {
    userId: user?.id,
    status: status === ALL ? undefined : status,
    page,
    limit: PAGE_SIZE
  });
  const statusMutation = useSetAIGatewayKeyStatusMutation(appKey);
  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const toggle = async (keyId: number, active: boolean) => {
    try {
      await statusMutation.mutateAsync({ keyId, status: active ? "active" : "disabled" });
      toast.success(active ? "密钥已启用" : "密钥已停用");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <SectionCard icon={<KeyRound className="size-4" />} title="用户密钥">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="w-60">
          <UserPicker appKey={appKey} value={user} onChange={(value) => { setUser(value); setPage(1); }} />
        </div>
        <Select value={status} onValueChange={(value) => { setStatus(value); setPage(1); }}>
          <SelectTrigger className="h-8 w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>未删除</SelectItem>
            <SelectItem value="active">启用中</SelectItem>
            <SelectItem value="disabled">已停用</SelectItem>
            <SelectItem value="revoked">已删除</SelectItem>
          </SelectContent>
        </Select>
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">共 {total} 把</span>
      </div>

      {query.isLoading ? (
        <Skeleton className="h-48 w-full rounded-xl" />
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-10 text-center text-xs text-muted-foreground">
          暂无密钥
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>密钥</TableHead>
              <TableHead>用户</TableHead>
              <TableHead>可用模型</TableHead>
              <TableHead className="text-right">已用 / 上限（点）</TableHead>
              <TableHead>最近使用</TableHead>
              <TableHead>启用</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((key) => (
              <TableRow key={key.id}>
                <TableCell>
                  <button type="button" className="text-left hover:underline"
                    onClick={() => onOpenRequests({ keyId: key.id })}>
                    <div className="text-xs font-medium">{key.name}</div>
                    <div className="font-mono text-[11px] text-muted-foreground">{key.hint}</div>
                  </button>
                </TableCell>
                <TableCell className="text-xs">
                  <button type="button" className="hover:underline" onClick={() => onOpenRequests({ userId: key.userId })}>
                    {key.account || `#${key.userId}`}
                  </button>
                </TableCell>
                <TableCell className="max-w-48 truncate font-mono text-[11px]">
                  {key.models.length > 0 ? key.models.join(", ") : <span className="text-muted-foreground">不限</span>}
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  {formatPoints(key.spent)} / {Number(key.spendLimit) > 0 ? formatPoints(key.spendLimit) : "不限"}
                </TableCell>
                <TableCell className="text-xs tabular-nums">
                  {formatDateTime(key.lastUsedAt)}
                  {key.expiresAt ? (
                    <div className="text-[11px] text-muted-foreground">到期 {formatDateTime(key.expiresAt)}</div>
                  ) : null}
                </TableCell>
                <TableCell>
                  {key.status === "revoked" ? (
                    <Badge variant="outline" size="sm">已删除</Badge>
                  ) : (
                    <Switch checked={key.status === "active"} disabled={statusMutation.isPending}
                      onCheckedChange={(checked) => void toggle(key.id, checked)} />
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {totalPages > 1 ? (
        <div className="mt-3 flex items-center justify-end gap-2 text-xs">
          <Button size="xs" variant="outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
          <span className="tabular-nums text-muted-foreground">{page} / {totalPages}</span>
          <Button size="xs" variant="outline" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>下一页</Button>
        </div>
      ) : null}
    </SectionCard>
  );
}
