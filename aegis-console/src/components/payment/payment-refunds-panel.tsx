"use client";

import { useMemo, useState } from "react";
import { Loader2, RotateCw, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { PaymentRefund } from "@/lib/api/types";
import { useAdminPaymentRefundsQuery, useSyncAdminPaymentRefundMutation } from "@/lib/admin-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { formatMoney } from "@/components/commerce/commerce-format";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { PaymentBrandBadge } from "./payment-brand-icon";
import { ListPagination, ListState, RefreshButton, SearchBox, StatusSegments } from "./payment-list-parts";
import { usePaymentMethodMeta, usePaymentOrderActions, type MethodMeta } from "./payment-order-actions";
import {
  REFUND_STATUS_FILTERS,
  formatOrderTime,
  refundStatusBadge,
  relativeOrderTime,
  reversalBadge
} from "./payment-order-meta";

const ALL = "__all__";
const PAGE_SIZE = 20;
const GRID = "md:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1.2fr)_minmax(0,0.9fr)_5.5rem]";

/**
 * 退款单列表。点一行打开所属订单的详情 —— 退款记录、回调与权益回收都在那里，
 * 不在这张表上再铺一遍。
 */
export function PaymentRefundsPanel({
  appId,
  appKey,
  receiptLocale,
  receiptLocaleLabel
}: {
  appId?: number | null;
  appKey?: string | null;
  receiptLocale?: string;
  receiptLocaleLabel?: string;
}) {
  const [status, setStatus] = useState("");
  const [method, setMethod] = useState(ALL);
  const [keywordInput, setKeywordInput] = useState("");
  const keyword = useDebouncedValue(keywordInput.trim(), 300);
  const [page, setPage] = useState(1);

  const actions = usePaymentOrderActions({ appId, appKey, receiptLocale, receiptLocaleLabel });
  const { methods } = usePaymentMethodMeta();
  const syncMutation = useSyncAdminPaymentRefundMutation();
  const [syncing, setSyncing] = useState<string | null>(null);

  const filters = useMemo(
    () => ({
      status: status || undefined,
      payment_method: method === ALL ? undefined : method,
      keyword: keyword || undefined,
      page,
      limit: PAGE_SIZE
    }),
    [status, method, keyword, page]
  );
  const refundsQuery = useAdminPaymentRefundsQuery(appId, filters);

  const data = refundsQuery.data;
  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, data?.totalPages ?? 1);
  const filtered = Boolean(status || method !== ALL || keyword);

  function resetPage<T>(setter: (value: T) => void) {
    return (value: T) => {
      setter(value);
      setPage(1);
    };
  }

  function clearFilters() {
    setStatus("");
    setMethod(ALL);
    setKeywordInput("");
    setPage(1);
  }

  async function handleSync(refundNo: string) {
    if (!appId) return;
    setSyncing(refundNo);
    try {
      const refund = await syncMutation.mutateAsync({ appid: appId, refund_no: refundNo });
      toast.success(`已同步，当前状态：${REFUND_STATUS_FILTERS.find((o) => o.value === refund.status)?.label ?? refund.status}`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "同步失败");
    } finally {
      setSyncing(null);
    }
  }

  return (
    <TooltipProvider delayDuration={300}>
      <div className="space-y-3">
        <div className="flex flex-col gap-3 xl:flex-row xl:items-center">
          <StatusSegments label="退款状态" options={REFUND_STATUS_FILTERS} value={status} onChange={resetPage(setStatus)} />
          <div className="flex flex-1 flex-wrap items-center gap-2">
            <SearchBox
              className="w-full sm:w-auto sm:min-w-56 sm:flex-1 xl:max-w-72"
              placeholder="搜索退款单号、订单号或上游单号"
              value={keywordInput}
              onChange={resetPage(setKeywordInput)}
            />
            <Select value={method} onValueChange={resetPage(setMethod)}>
              <SelectTrigger className="h-9 min-w-0 flex-1 sm:w-40 sm:flex-none">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>全部支付方式</SelectItem>
                {methods.map((m) => (
                  <SelectItem key={m.method} value={m.method}>
                    {m.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="ml-auto flex items-center gap-1">
              {filtered ? (
                <Button size="sm" variant="ghost" className="h-9 text-muted-foreground" onClick={clearFilters}>
                  清除筛选
                </Button>
              ) : null}
              <RefreshButton fetching={refundsQuery.isFetching} onClick={() => void refundsQuery.refetch()} />
            </div>
          </div>
        </div>

        <div className="overflow-hidden rounded-2xl border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
          <div className={cn("hidden items-center gap-4 border-b bg-muted/30 px-4 py-2.5 text-xs font-medium text-muted-foreground md:grid", GRID)}>
            <span>退款单</span>
            <span className="text-right">金额</span>
            <span>状态</span>
            <span>时间</span>
            <span className="sr-only">操作</span>
          </div>

          {refundsQuery.isLoading ? (
            <ul className="divide-y">
              {Array.from({ length: 5 }).map((_, i) => (
                <li key={i} className="flex items-center gap-3 px-4 py-3.5">
                  <Skeleton className="size-9 rounded-xl" />
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-48" />
                    <Skeleton className="h-3 w-60" />
                  </div>
                  <Skeleton className="h-5 w-16" />
                  <Skeleton className="hidden h-5 w-20 md:block" />
                </li>
              ))}
            </ul>
          ) : refundsQuery.isError ? (
            <ListState
              icon={<Undo2 className="size-5" />}
              title="退款单加载失败"
              action={
                <Button size="sm" variant="outline" onClick={() => void refundsQuery.refetch()}>
                  重试
                </Button>
              }
            />
          ) : items.length === 0 ? (
            <ListState
              icon={<Undo2 className="size-5" />}
              title={filtered ? "没有符合条件的退款单" : "暂无退款单"}
              hint={filtered ? "调整筛选条件后再试" : "在订单详情中发起退款后，退款单会出现在这里"}
              action={
                filtered ? (
                  <Button size="sm" variant="outline" onClick={clearFilters}>
                    清除筛选
                  </Button>
                ) : null
              }
            />
          ) : (
            <ul className={cn("divide-y transition-opacity", refundsQuery.isFetching && "opacity-60")}>
              {items.map((refund) => (
                <RefundRow
                  key={refund.id}
                  refund={refund}
                  method={actions.method(refund.payment_method)}
                  onOpen={() => actions.openDetail(refund.order_no)}
                  onSync={() => void handleSync(refund.refund_no)}
                  syncing={syncing === refund.refund_no}
                />
              ))}
            </ul>
          )}

          {items.length > 0 ? <ListPagination page={page} totalPages={totalPages} total={total} unit="笔" onPage={setPage} /> : null}
        </div>

        {actions.dialogs}
      </div>
    </TooltipProvider>
  );
}

function RefundRow({
  refund,
  method,
  onOpen,
  onSync,
  syncing
}: {
  refund: PaymentRefund;
  method: MethodMeta;
  onOpen: () => void;
  onSync: () => void;
  syncing: boolean;
}) {
  // 只有未达终态的退款单需要同步；终态单再查上游没有意义
  const syncable = refund.status === "processing" || refund.status === "pending";
  const note = refund.error_message || refund.reversal_message || refund.reason;
  return (
    <li
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      className={cn(
        "grid cursor-pointer grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2 px-4 py-3 transition-colors outline-none hover:bg-muted/40 focus-visible:bg-muted/50",
        GRID
      )}
    >
      <div className="flex min-w-0 items-center gap-3">
        <PaymentBrandBadge slug={method.icon} brandColor={method.brandColor} name={method.name} />
        <div className="min-w-0">
          <p className="truncate font-mono text-sm font-medium">{refund.refund_no}</p>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            订单 <span className="font-mono">{refund.order_no}</span>
          </p>
          {note ? (
            <p
              className={cn(
                "mt-0.5 truncate text-xs",
                refund.error_message ? "text-destructive" : refund.reversal_message ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground"
              )}
              title={note}
            >
              {note}
            </p>
          ) : null}
        </div>
      </div>

      <div className="text-right">
        <p className="font-mono text-sm font-semibold tabular-nums">-{formatMoney(refund.amount)}</p>
        <p className="mt-0.5 truncate text-xs text-muted-foreground">{refund.operator || "系统"}</p>
      </div>

      <div className="col-span-2 flex flex-wrap items-center gap-1.5 md:col-span-1">
        {refundStatusBadge(refund.status)}
        {reversalBadge(refund)}
        <span className="ml-auto text-xs text-muted-foreground tabular-nums md:hidden">{relativeOrderTime(refund.createdAt)}</span>
      </div>

      <div className="hidden md:block">
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="text-sm text-muted-foreground tabular-nums">{relativeOrderTime(refund.createdAt)}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>发起 {formatOrderTime(refund.createdAt, true)}</p>
            {refund.updatedAt ? <p>更新 {formatOrderTime(refund.updatedAt, true)}</p> : null}
          </TooltipContent>
        </Tooltip>
      </div>

      <div
        className={cn("col-span-2 flex justify-end md:col-span-1", !syncable && "hidden md:flex")}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
      >
        {syncable ? (
          <Button size="sm" variant="outline" className="h-8 gap-1.5" disabled={syncing} onClick={onSync}>
            {syncing ? <Loader2 className="size-3.5 animate-spin" /> : <RotateCw className="size-3.5" />}
            同步状态
          </Button>
        ) : null}
      </div>
    </li>
  );
}
