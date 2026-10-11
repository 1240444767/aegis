"use client";

import { useMemo, useState, type ReactNode } from "react";
import { ArrowRight, ReceiptText } from "lucide-react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import type { PaymentConfig } from "@/lib/api/types";
import { useAdminPaymentConfigsQuery, useAdminPaymentOrdersQuery } from "@/lib/admin-hooks";
import { useCommerceOverviewQuery } from "@/lib/commerce-hooks";
import { MONEY_COLORS, buildChartConfig } from "@/components/commerce/commerce-charts";
import { formatMoney } from "@/components/commerce/commerce-format";
import { commerceRangePreset } from "@/components/commerce/commerce-range-picker";
import { PaymentBrandBadge } from "@/components/payment/payment-brand-icon";
import { usePaymentOrderActions } from "@/components/payment/payment-order-actions";
import { OrderStatusBadge, relativeOrderTime } from "@/components/payment/payment-order-meta";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const RANGES = [
  { days: 7, label: "近 7 天" },
  { days: 30, label: "近 30 天" },
  { days: 90, label: "近 90 天" }
];

const BUCKET_LABEL: Record<string, string> = { day: "按天", week: "按周", month: "按月" };

const chartConfig = buildChartConfig([
  { key: "net", label: "实收净额", color: MONEY_COLORS.paid },
  { key: "refunded", label: "退款", color: MONEY_COLORS.refunded }
]);

/**
 * 支付概览：这段时间收了多少、走的哪些渠道、最近有哪些单。
 * 金额口径与交易中心一致（同一个 overview 接口），两处数字不会对不上。
 */
export function PaymentOverview({
  appId,
  appKey,
  receiptLocale,
  receiptLocaleLabel,
  onOpenView
}: {
  appId: number;
  appKey: string;
  receiptLocale?: string;
  receiptLocaleLabel?: string;
  onOpenView: (view: "orders" | "channels") => void;
}) {
  const [days, setDays] = useState(30);
  const window = useMemo(() => commerceRangePreset(days - 1), [days]);
  const query = useCommerceOverviewQuery(appKey, window);
  const configsQuery = useAdminPaymentConfigsQuery(appId);
  const recentQuery = useAdminPaymentOrdersQuery(appId, { page: 1, limit: 6 });
  const actions = usePaymentOrderActions({ appId, appKey, receiptLocale, receiptLocaleLabel });

  const orders = query.data?.orders;
  const trend = query.data?.trend;
  const rows = useMemo(
    () =>
      (trend?.points ?? []).map((point) => ({
        label: point.label,
        net: Number(point.netAmount),
        refunded: Number(point.refundedAmount),
        orders: point.paidOrders
      })),
    [trend]
  );
  const hasTrend = rows.some((row) => row.net || row.refunded);

  const paidOrders = orders?.paidOrders ?? 0;
  const totalOrders = orders?.totalOrders ?? 0;
  const successRate = totalOrders ? Math.round((paidOrders / totalOrders) * 1000) / 10 : 0;
  const average = paidOrders ? (Number(orders?.paidAmount ?? 0) / paidOrders).toFixed(2) : "0";

  const byMethod = useMemo(() => {
    const items = [...(orders?.byMethod ?? [])].sort((a, b) => Number(b.amount) - Number(a.amount));
    const sum = items.reduce((acc, item) => acc + Number(item.amount), 0);
    return items.map((item) => ({ ...item, share: sum ? Number(item.amount) / sum : 0 }));
  }, [orders]);

  const configs = (configsQuery.data ?? []) as PaymentConfig[];
  const recent = (recentQuery.data?.items ?? []).slice(0, 6);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {RANGES.find((r) => r.days === days)?.label}
          {trend?.bucket ? `，${BUCKET_LABEL[trend.bucket] ?? "按天"}统计` : ""}
        </p>
        <div className="inline-flex rounded-lg border bg-muted/40 p-0.5" role="tablist" aria-label="统计范围">
          {RANGES.map((range) => (
            <button
              key={range.days}
              type="button"
              role="tab"
              aria-selected={days === range.days}
              onClick={() => setDays(range.days)}
              className={cn(
                "rounded-md px-2.5 py-1 text-xs font-medium whitespace-nowrap text-muted-foreground transition-colors hover:text-foreground",
                days === range.days && "bg-background text-foreground shadow-sm"
              )}
            >
              {range.label}
            </button>
          ))}
        </div>
      </div>

      {/* 指标：一张卡四格，窄屏两列 */}
      <Card className="grid grid-cols-2 gap-px bg-border lg:grid-cols-4">
        <Metric
          loading={query.isLoading}
          label="实收净额"
          value={formatMoney(orders?.netAmount)}
          hint={`已收 ${formatMoney(orders?.paidAmount)}`}
        />
        <Metric loading={query.isLoading} label="支付订单" value={paidOrders.toLocaleString("zh-CN")} hint={`成功率 ${successRate}%`} />
        <Metric
          loading={query.isLoading}
          label="付款用户"
          value={(orders?.payerCount ?? 0).toLocaleString("zh-CN")}
          hint={`客单价 ${formatMoney(average)}`}
        />
        <Metric
          loading={query.isLoading}
          label="退款"
          value={formatMoney(orders?.refundedAmount)}
          hint={`${orders?.refundCount ?? 0} 笔`}
        />
      </Card>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <Card>
          <CardHead title="收款趋势" />
          <div className="px-2 pb-3 sm:px-4">
            {query.isLoading ? (
              <Skeleton className="h-[260px] w-full rounded-xl" />
            ) : !hasTrend ? (
              <div className="flex h-[260px] items-center justify-center text-xs text-muted-foreground">这段时间没有收款</div>
            ) : (
              <ChartContainer config={chartConfig} className="h-[260px] w-full">
                <BarChart data={rows} margin={{ left: 0, right: 4, top: 8 }} barCategoryGap="22%">
                  <CartesianGrid vertical={false} strokeDasharray="3 3" />
                  <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={20} fontSize={11} />
                  <YAxis tickLine={false} axisLine={false} width={48} fontSize={11} tickFormatter={compactNumber} />
                  <ChartTooltip cursor={{ fill: "var(--muted)", opacity: 0.5 }} content={<ChartTooltipContent indicator="dot" />} />
                  <Bar dataKey="net" stackId="money" fill="var(--color-net)" radius={[0, 0, 0, 0]} />
                  <Bar dataKey="refunded" stackId="money" fill="var(--color-refunded)" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ChartContainer>
            )}
          </div>
        </Card>

        <Card>
          <CardHead title="支付方式" />
          <div className="px-4 pb-4">
            {query.isLoading ? (
              <div className="space-y-3">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full rounded-lg" />
                ))}
              </div>
            ) : byMethod.length === 0 ? (
              <p className="py-10 text-center text-xs text-muted-foreground">暂无已支付订单</p>
            ) : (
              <ul className="space-y-3.5">
                {byMethod.map((item) => {
                  const meta = actions.method(item.key);
                  return (
                    <li key={item.key} className="space-y-1.5">
                      <div className="flex items-center gap-2.5">
                        <PaymentBrandBadge slug={meta.icon} brandColor={meta.brandColor} name={meta.name} size="sm" />
                        <div className="min-w-0 flex-1">
                          <div className="flex items-baseline justify-between gap-2">
                            <span className="truncate text-sm">{meta.name}</span>
                            <span className="shrink-0 font-mono text-sm tabular-nums">{formatMoney(item.amount)}</span>
                          </div>
                          <div className="flex items-baseline justify-between gap-2 text-[11px] text-muted-foreground tabular-nums">
                            <span>{item.count} 笔</span>
                            <span>{(item.share * 100).toFixed(1)}%</span>
                          </div>
                        </div>
                      </div>
                      <div className="h-1 overflow-hidden rounded-full bg-muted">
                        <div className="h-full rounded-full bg-foreground/70" style={{ width: `${Math.max(item.share * 100, 1)}%` }} />
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </Card>
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start">
        <Card>
          <CardHead title="最近订单" action={<LinkButton onClick={() => onOpenView("orders")}>全部订单</LinkButton>} />
          {recentQuery.isLoading ? (
            <div className="space-y-2 px-4 pb-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-11 w-full rounded-lg" />
              ))}
            </div>
          ) : recent.length === 0 ? (
            <div className="flex flex-col items-center gap-2 px-4 pt-6 pb-10 text-center text-xs text-muted-foreground">
              <ReceiptText className="size-5" />
              暂无订单
            </div>
          ) : (
            <ul className="divide-y border-t">
              {recent.map((order) => {
                const meta = actions.method(order.payment_method);
                return (
                  <li key={order.id}>
                    <button
                      type="button"
                      onClick={() => actions.openDetail(order.order_no)}
                      className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-muted/40 focus-visible:bg-muted/50 focus-visible:outline-none"
                    >
                      <PaymentBrandBadge slug={meta.icon} brandColor={meta.brandColor} name={meta.name} size="sm" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm">{order.subject || "未命名商品"}</span>
                        <span className="block truncate text-[11px] text-muted-foreground tabular-nums">
                          {relativeOrderTime(order.createdAt)}
                          {order.user_id ? `，用户 #${order.user_id}` : ""}
                        </span>
                      </span>
                      <span className="shrink-0 font-mono text-sm tabular-nums">{formatMoney(order.amount)}</span>
                      <OrderStatusBadge status={order.status} className="hidden w-[4.75rem] shrink-0 justify-center sm:inline-flex" />
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>

        <Card>
          <CardHead title="收款渠道" action={<LinkButton onClick={() => onOpenView("channels")}>管理</LinkButton>} />
          {configsQuery.isLoading ? (
            <div className="space-y-2 px-4 pb-4">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-9 w-full rounded-lg" />
              ))}
            </div>
          ) : configs.length === 0 ? (
            <div className="px-4 pb-6 text-center">
              <p className="py-4 text-xs text-muted-foreground">尚未接入收款渠道</p>
              <LinkButton onClick={() => onOpenView("channels")}>去接入</LinkButton>
            </div>
          ) : (
            <ul className="divide-y border-t">
              {configs.map((cfg) => {
                const meta = actions.method(String(cfg.payment_method));
                const enabled = cfg.enabled !== false;
                return (
                  <li key={cfg.id} className="flex items-center gap-2.5 px-4 py-2.5">
                    <span className={cn(!enabled && "opacity-50 grayscale")}>
                      <PaymentBrandBadge slug={meta.icon} brandColor={meta.brandColor} name={meta.name} size="sm" />
                    </span>
                    <span className={cn("min-w-0 flex-1 truncate text-sm", !enabled && "text-muted-foreground")}>
                      {cfg.config_name || meta.name}
                    </span>
                    {cfg.is_default ? <span className="shrink-0 text-[11px] text-sky-600 dark:text-sky-400">默认</span> : null}
                    <span
                      className={cn("size-1.5 shrink-0 rounded-full", enabled ? "bg-emerald-500" : "bg-muted-foreground/40")}
                      aria-label={enabled ? "启用" : "停用"}
                    />
                  </li>
                );
              })}
            </ul>
          )}
        </Card>
      </div>

      {actions.dialogs}
    </div>
  );
}

function Card({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <section className={cn("overflow-hidden rounded-2xl border bg-card", className)} style={{ boxShadow: "var(--shadow-soft)" }}>
      {children}
    </section>
  );
}

function CardHead({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <header className="flex items-center justify-between gap-3 px-4 pt-3.5 pb-3">
      <h3 className="text-sm font-semibold">{title}</h3>
      {action}
    </header>
  );
}

function LinkButton({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
    >
      {children}
      <ArrowRight className="size-3.5" />
    </button>
  );
}

function Metric({
  label,
  value,
  hint,
  loading
}: {
  label: string;
  value: string;
  hint?: string;
  loading?: boolean;
}) {
  return (
    <div className="min-w-0 space-y-1 bg-card px-4 py-4 sm:px-5">
      <p className="text-xs text-muted-foreground">{label}</p>
      {loading ? (
        <Skeleton className="h-7 w-28" />
      ) : (
        <p className="truncate font-mono text-xl font-semibold tracking-tight tabular-nums sm:text-2xl">
          {value}
        </p>
      )}
      {loading ? <Skeleton className="h-3 w-20" /> : hint ? <p className="truncate text-xs text-muted-foreground tabular-nums">{hint}</p> : null}
    </div>
  );
}

function compactNumber(value: number) {
  if (Math.abs(value) >= 10000) return `${(value / 10000).toFixed(value >= 100000 ? 0 : 1)}万`;
  return String(value);
}
