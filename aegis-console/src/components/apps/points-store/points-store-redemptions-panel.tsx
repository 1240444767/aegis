"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Ban, Loader2, PackageCheck, ReceiptText } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";
import {
  RedemptionStatusBadge,
  formatPoints,
  formatStoreDate
} from "@/components/apps/points-store/points-store-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
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
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { PointsStoreRedemption, PointsStoreRedemptionStatus } from "@/lib/api/points-store";
import {
  useHandlePointsStoreRedemptionMutation,
  usePointsStoreItemsQuery,
  usePointsStoreRedemptionsQuery
} from "@/lib/points-store-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;

/**
 * 兑换记录。
 *
 * 展示 `results`（实际发出去了什么），不是商品上配的权益 —— 两者的差别正是排障要看的。
 * 人工发放的记录在这里处理：发放时写给用户的话（兑换码、快递单号）进站内信，
 * 取消即退回积分、库存加回。只有待发放的能处理。
 */
export function PointsStoreRedemptionsPanel({
  appKey,
  itemId,
  status,
  onItemIdChange,
  onStatusChange
}: {
  appKey: string;
  itemId?: number;
  status?: PointsStoreRedemptionStatus;
  onItemIdChange: (itemId?: number) => void;
  onStatusChange: (status?: PointsStoreRedemptionStatus) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [handling, setHandling] = useState<{ item: PointsStoreRedemption; action: "fulfill" | "cancel" } | null>(null);
  const [note, setNote] = useState("");

  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const itemsQuery = usePointsStoreItemsQuery(appKey);
  const listQuery = usePointsStoreRedemptionsQuery(appKey, {
    itemId,
    status,
    keyword: debouncedKeyword || undefined,
    page,
    limit: LIMIT
  });
  const handleMutation = useHandlePointsStoreRedemptionMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? 1);
  const filtered = Boolean(keyword || itemId || status);

  const userHref = (userId: number) => `/app-users/${encodeURIComponent(appKey)}/${userId}?tab=assets`;

  const openHandle = (item: PointsStoreRedemption, action: "fulfill" | "cancel") => {
    setNote("");
    setHandling({ item, action });
  };

  const submitHandle = async () => {
    if (!handling) return;
    try {
      await handleMutation.mutateAsync({ redemptionId: handling.item.id, action: handling.action, note: note.trim() });
      toast.success(handling.action === "fulfill" ? "已标记为已发放" : "已取消，积分已退回");
      setHandling(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <SectionCard icon={<ReceiptText className="size-4" />} title="兑换记录">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索账号、单号或商品"
          className="h-8 w-56"
        />
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            onStatusChange(value === "all" ? undefined : (value as PointsStoreRedemptionStatus));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="pending">待发放</SelectItem>
            <SelectItem value="completed">已到账</SelectItem>
            <SelectItem value="cancelled">已取消</SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={itemId ? String(itemId) : "all"}
          onValueChange={(value) => {
            onItemIdChange(value === "all" ? undefined : Number(value));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-48">
            <SelectValue placeholder="全部商品" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部商品</SelectItem>
            {(itemsQuery.data ?? []).map((item) => (
              <SelectItem key={item.id} value={String(item.id)}>
                {item.title}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配记录" : "暂无兑换记录"}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>商品</TableHead>
                <TableHead>账号</TableHead>
                <TableHead className="text-right">积分</TableHead>
                <TableHead>实际发放</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">时间</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="max-w-56">
                    <p className="truncate text-xs font-medium">{item.itemTitle}</p>
                    <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">{item.redemptionNo}</p>
                  </TableCell>
                  <TableCell className="text-xs">
                    <Link href={userHref(item.userId)} className="font-medium hover:underline">
                      {item.account || `#${item.userId}`}
                    </Link>
                    {item.nickname ? <p className="mt-0.5 text-[11px] text-muted-foreground">{item.nickname}</p> : null}
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums">{formatPoints(item.cost)}</TableCell>
                  <TableCell className="max-w-72">
                    <div className="flex flex-wrap gap-1">
                      {item.results.map((result, index) => (
                        <Badge
                          key={`${result.type}-${index}`}
                          variant={result.pending ? "warning" : "outline"}
                          size="sm"
                          className="font-normal"
                          title={result.transactionNo ? `流水 ${result.transactionNo}` : undefined}
                        >
                          {result.detail || result.label}
                        </Badge>
                      ))}
                    </div>
                    {item.note ? <p className="mt-1 text-[11px] text-muted-foreground">{item.note}</p> : null}
                  </TableCell>
                  <TableCell>
                    <RedemptionStatusBadge status={item.status} />
                    {item.handledBy ? <p className="mt-0.5 text-[11px] text-muted-foreground">{item.handledBy}</p> : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-right text-xs text-muted-foreground">
                    {formatStoreDate(item.createdAt)}
                    {item.clientIp ? <p className="mt-0.5 font-mono text-[11px]">{item.clientIp}</p> : null}
                  </TableCell>
                  <TableCell>
                    {item.status === "pending" ? (
                      <div className="flex justify-end gap-1">
                        <Button size="sm" variant="outline" className="h-7" onClick={() => openHandle(item, "fulfill")}>
                          <PackageCheck className="size-3.5" /> 发放
                        </Button>
                        <Button size="sm" variant="ghost" className="h-7" onClick={() => openHandle(item, "cancel")}>
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

      <AdPolicyPager total={total} page={page} totalPages={totalPages} onPage={setPage} />

      <Dialog open={Boolean(handling)} onOpenChange={(open) => !open && setHandling(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{handling?.action === "fulfill" ? "标记为已发放" : "取消兑换"}</DialogTitle>
            <DialogDescription>
              {handling?.action === "fulfill"
                ? `${handling?.item.account || `#${handling?.item.userId}`}，${handling?.item.itemTitle}`
                : `退回 ${formatPoints(handling?.item.cost)} 积分，库存加回。`}
            </DialogDescription>
          </DialogHeader>
          <Textarea
            value={note}
            rows={3}
            maxLength={500}
            onChange={(event) => setNote(event.target.value)}
            placeholder={handling?.action === "fulfill" ? "兑换码、快递单号" : "取消原因"}
          />
          <DialogFooter>
            <Button variant="ghost" size="sm" onClick={() => setHandling(null)}>
              返回
            </Button>
            <Button
              size="sm"
              variant={handling?.action === "cancel" ? "destructive" : "default"}
              onClick={submitHandle}
              disabled={handleMutation.isPending}
            >
              {handleMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {handling?.action === "fulfill" ? "确认发放" : "确认取消"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SectionCard>
  );
}
