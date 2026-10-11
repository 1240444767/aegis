"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { useAdminPaymentMethodsQuery } from "@/lib/admin-hooks";
import {
  useDownloadOrderReceiptMutation,
  useEmailOrderReceiptMutation,
  useReceiptRecipients
} from "@/lib/commerce-hooks";
import { ReceiptEmailDialog } from "@/components/commerce/receipt-email-dialog";
import { PaymentOrderDetailSheet } from "./payment-order-detail";
import { PaymentRefundDialog } from "./payment-refund-dialog";

export type MethodMeta = { name: string; icon?: string | null; brandColor?: string | null };

type EmailTarget = { orderNo: string; subject: string; userId?: number | null };

/** 支付方式代码 → 展示名与品牌图标。与渠道目录同源，列表、详情、概览三处叫法一致。 */
export function usePaymentMethodMeta() {
  const methodsQuery = useAdminPaymentMethodsQuery();
  const methods = useMemo(() => methodsQuery.data ?? [], [methodsQuery.data]);
  const resolve = useMemo(() => {
    const byCode = new Map(methods.map((m) => [m.method, m] as const));
    return (code?: string): MethodMeta => {
      const found = code ? byCode.get(code) : undefined;
      return { name: found?.name ?? code ?? "未知方式", icon: found?.icon, brandColor: found?.brandColor };
    };
  }, [methods]);
  return { methods, resolve };
}

/**
 * 订单的一组操作：详情抽屉、退款、下载与寄送凭证。
 *
 * 订单列表、退款列表与支付概览都要能点开同一笔订单，状态与弹层收在这里，
 * 三处行为不会走样。调用方把返回的 `dialogs` 渲染到自己的树里即可。
 */
export function usePaymentOrderActions({
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
  const [detailOrderNo, setDetailOrderNo] = useState<string | null>(null);
  const [refundOrderNo, setRefundOrderNo] = useState<string | null>(null);
  const [emailTarget, setEmailTarget] = useState<EmailTarget | null>(null);
  const { resolve } = usePaymentMethodMeta();

  const downloadReceipt = useDownloadOrderReceiptMutation(appId);
  const emailReceipt = useEmailOrderReceiptMutation(appId);
  const recipients = useReceiptRecipients(appKey, emailTarget?.userId ?? null);

  async function downloadOrderReceipt(orderNo: string) {
    try {
      await downloadReceipt.mutateAsync({ order_no: orderNo, locale: receiptLocale });
      toast.success("凭证已生成");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "凭证生成失败");
    }
  }

  const emailOrderReceipt = (order: EmailTarget) => setEmailTarget(order);

  const dialogs = (
    <>
      <PaymentOrderDetailSheet
        appId={appId}
        orderNo={detailOrderNo}
        method={resolve}
        onOpenChange={(open) => !open && setDetailOrderNo(null)}
        onRefund={setRefundOrderNo}
        onDownloadReceipt={(orderNo) => void downloadOrderReceipt(orderNo)}
        onEmailReceipt={(order) => emailOrderReceipt({ orderNo: order.order_no, subject: order.subject, userId: order.user_id })}
        downloading={downloadReceipt.isPending}
      />
      <PaymentRefundDialog
        open={Boolean(refundOrderNo)}
        onOpenChange={(open) => !open && setRefundOrderNo(null)}
        appId={appId}
        orderNo={refundOrderNo}
      />
      <ReceiptEmailDialog
        open={Boolean(emailTarget)}
        onOpenChange={(open) => !open && setEmailTarget(null)}
        title="寄送订单凭证"
        subject={emailTarget?.subject ?? ""}
        reference={emailTarget?.orderNo ?? ""}
        suggestions={recipients}
        localeLabel={receiptLocaleLabel}
        pending={emailReceipt.isPending}
        onSubmit={async (to) => {
          if (!emailTarget) return;
          await emailReceipt.mutateAsync({ order_no: emailTarget.orderNo, email: to, locale: receiptLocale });
          setEmailTarget(null);
        }}
      />
    </>
  );

  return {
    method: resolve,
    openDetail: setDetailOrderNo,
    openRefund: setRefundOrderNo,
    downloadReceipt: (orderNo: string) => void downloadOrderReceipt(orderNo),
    emailReceipt: emailOrderReceipt,
    downloading: downloadReceipt.isPending,
    dialogs
  };
}
