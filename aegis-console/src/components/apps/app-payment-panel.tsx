"use client";

import { useMemo, useState } from "react";
import { BarChart3, CreditCard, ReceiptText, Settings2, Undo2, Wallet } from "lucide-react";
import { useAdminAppCommerceSettingsQuery } from "@/lib/admin-hooks";
import { usePaymentReceiptCapabilityQuery } from "@/lib/commerce-hooks";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { PaymentChannels } from "@/components/apps/payment/payment-channels";
import { PaymentOverview } from "@/components/apps/payment/payment-overview";
import { PaymentSettings } from "@/components/apps/payment/payment-settings";
import { PaymentOrdersPanel } from "@/components/payment/payment-orders-panel";
import { PaymentRefundsPanel } from "@/components/payment/payment-refunds-panel";
import { cn } from "@/lib/utils";

/**
 * 应用的支付区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 这段时间收了多少、走的哪些渠道、最近有哪些单 |
 * | 收款渠道 | 接了哪些商户、参数对不对、连不连得通 |
 * | 订单 / 退款 | 某一笔钱的来龙去脉，退款与凭证从这里发起 |
 * | 交易设置 | 积分兑换率、钱包币种、凭证自动寄送 |
 *
 * 会员套餐不在这里：它有自己的「会员」区块，两处各管一份只会互相覆盖。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "channels", label: "收款渠道", icon: Wallet },
  { key: "orders", label: "订单", icon: ReceiptText },
  { key: "refunds", label: "退款", icon: Undo2 },
  { key: "settings", label: "交易设置", icon: Settings2 }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppPaymentPanel({ appId, appKey }: { appId?: number | null; appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");

  // 下载与寄送凭证用应用设置里的凭证语言；没设置时用服务端默认语言
  const commerceQuery = useAdminAppCommerceSettingsQuery(appKey);
  const capabilityQuery = usePaymentReceiptCapabilityQuery(appId);
  const receiptLocale = commerceQuery.data?.receiptLocale?.trim() || capabilityQuery.data?.defaultLocale;
  const receiptLocaleLabel = useMemo(
    () => capabilityQuery.data?.locales?.find((item) => item.tag === receiptLocale)?.nativeName ?? receiptLocale,
    [capabilityQuery.data, receiptLocale]
  );

  if (!appId || !appKey) return <NoAppSelected icon={<CreditCard className="size-5" />} />;

  const receipt = { receiptLocale, receiptLocaleLabel };

  return (
    <div className="space-y-4">
      <div className="-mx-1 overflow-x-auto px-1 [scrollbar-width:none]">
        <div className="inline-flex min-w-max gap-1 rounded-xl border bg-muted/40 p-1" role="tablist" aria-label="支付">
          {VIEWS.map((item) => (
            <button
              key={item.key}
              type="button"
              role="tab"
              aria-selected={view === item.key}
              onClick={() => setView(item.key)}
              className={cn(
                "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium whitespace-nowrap transition-colors",
                view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <item.icon className="size-3.5" />
              {item.label}
            </button>
          ))}
        </div>
      </div>

      {view === "overview" ? <PaymentOverview appId={appId} appKey={appKey} {...receipt} onOpenView={setView} /> : null}
      {view === "channels" ? <PaymentChannels appId={appId} /> : null}
      {view === "orders" ? <PaymentOrdersPanel appId={appId} appKey={appKey} {...receipt} /> : null}
      {view === "refunds" ? <PaymentRefundsPanel appId={appId} appKey={appKey} {...receipt} /> : null}
      {view === "settings" ? <PaymentSettings appId={appId} appKey={appKey} /> : null}
    </div>
  );
}
