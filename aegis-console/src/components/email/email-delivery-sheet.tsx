"use client";

import { Ban, CircleAlert, Loader2, Share2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type { EmailProviderMeta } from "@/lib/api/types";
import { formatDateTime } from "@/lib/datetime";
import {
  useDeleteEmailSuppressionMutation,
  useEmailDeliveryDetailQuery,
  type EmailScope
} from "@/lib/email-hooks";
import { cn } from "@/lib/utils";
import { DELIVERY_EVENT, SUPPRESSION_REASON, deliveryStatus } from "./email-shared";

type TimelineItem = { key: string; label: string; detail?: string; at?: string | null; tone: "muted" | "ok" | "bad" | "info" };

/**
 * 一封信的完整留痕：基本信息、错误原因、回执时间线、收件地址当前是否被抑制。
 *
 * 时间线的前两步（创建、交给服务商）来自留痕本身，其后是逐条回执事件。
 * 没有回执事件不代表出了问题：SMTP 通道本来就没有回执。
 */
export function EmailDeliverySheet({
  scope,
  deliveryId,
  providers,
  labelOf,
  onClose
}: {
  scope: EmailScope;
  deliveryId: number | null;
  providers: EmailProviderMeta[];
  labelOf: (purpose?: string | null) => string;
  onClose: () => void;
}) {
  const query = useEmailDeliveryDetailQuery(scope, deliveryId);
  const unsuppress = useDeleteEmailSuppressionMutation(scope);
  const detail = query.data;
  const delivery = detail?.delivery;
  const meta = deliveryStatus(delivery?.status);
  const providerMeta = providers.find((item) => item.provider === delivery?.provider);

  const timeline: TimelineItem[] = [];
  if (delivery) {
    timeline.push({ key: "created", label: "生成邮件", at: delivery.createdAt, tone: "muted" });
    if (delivery.status === "failed") {
      timeline.push({ key: "failed", label: "发送失败", detail: delivery.errorMessage, at: delivery.updatedAt, tone: "bad" });
    } else if (delivery.sentAt) {
      timeline.push({ key: "sent", label: `交给 ${providerMeta?.name ?? delivery.provider}`, at: delivery.sentAt, tone: "info" });
    }
    for (const event of detail?.events ?? []) {
      const bad = event.status === "bounced" || event.status === "complained" || event.status === "rejected";
      timeline.push({
        key: `event-${event.id}`,
        label: DELIVERY_EVENT[event.event] ?? event.event,
        detail: event.detail,
        at: event.occurredAt,
        tone: bad ? "bad" : event.event === "delivery" ? "ok" : "info"
      });
    }
  }

  const removeSuppression = async () => {
    if (!detail?.suppression) return;
    try {
      await unsuppress.mutateAsync(detail.suppression.id);
      toast.success("已移出抑制名单，可以再次向这个地址发信");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <Sheet open={deliveryId !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="pr-8 text-base leading-snug">
            {delivery ? delivery.subject || "无主题" : <Skeleton className="h-5 w-2/3" />}
          </SheetTitle>
          <SheetDescription asChild>
            <div className="flex flex-wrap items-center gap-1.5">
              {delivery ? (
                <>
                  <Badge variant={meta.tone} size="sm">
                    {meta.label}
                  </Badge>
                  <Badge variant="secondary" size="sm" className="font-normal">
                    {labelOf(delivery.purpose)}
                  </Badge>
                  {delivery.inherited ? (
                    <Badge variant="info" size="sm" className="gap-1 font-normal">
                      <Share2 className="size-3" /> 平台共享通道
                    </Badge>
                  ) : null}
                </>
              ) : null}
            </div>
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-5 overflow-y-auto px-5 py-4">
          {query.isLoading || !delivery ? (
            <div className="space-y-2">
              {Array.from({ length: 8 }).map((_, index) => (
                <Skeleton key={index} className="h-5 w-full" />
              ))}
            </div>
          ) : (
            <>
              {detail?.suppression ? (
                <div className="flex items-start gap-3 rounded-xl border border-red-200 bg-red-50/60 px-4 py-3 text-sm dark:border-red-900/60 dark:bg-red-950/30">
                  <Ban className="mt-0.5 size-4 shrink-0 text-red-600 dark:text-red-400" />
                  <div className="min-w-0 flex-1 space-y-1">
                    <p className="font-medium text-red-800 dark:text-red-300">
                      这个地址已暂停投递（{SUPPRESSION_REASON[detail.suppression.reason]?.label ?? detail.suppression.reason}）
                    </p>
                    {detail.suppression.note ? (
                      <p className="break-all text-xs text-red-700/80 dark:text-red-300/80">{detail.suppression.note}</p>
                    ) : null}
                  </div>
                  <Button size="sm" variant="outline" className="h-7 shrink-0" disabled={unsuppress.isPending} onClick={() => void removeSuppression()}>
                    {unsuppress.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
                    移出
                  </Button>
                </div>
              ) : null}

              {delivery.errorMessage ? (
                <div className="flex items-start gap-2.5 rounded-xl border bg-muted/40 px-4 py-3">
                  <CircleAlert className="mt-0.5 size-4 shrink-0 text-destructive" />
                  <p className="break-all text-xs leading-relaxed">{delivery.errorMessage}</p>
                </div>
              ) : null}

              <dl className="divide-y rounded-xl border text-xs">
                <Row label="收件人" value={delivery.toAddress} mono />
                <Row label="发件人" value={delivery.fromAddress} mono />
                <Row label="通道" value={[delivery.configName, providerMeta?.name ?? delivery.provider].filter(Boolean).join("，")} />
                <Row label="服务商消息 ID" value={delivery.providerMessageId} mono />
                <Row label="Message-ID" value={delivery.messageId} mono />
                <Row label="打开 / 点击" value={`${delivery.openCount ?? 0} / ${delivery.clickCount ?? 0}`} />
                <Row label="送达时间" value={delivery.deliveredAt ? formatDateTime(delivery.deliveredAt) : undefined} />
              </dl>

              <section className="space-y-3">
                <h4 className="text-xs font-medium text-muted-foreground">投递过程</h4>
                <ol className="relative space-y-4 pl-5">
                  <span className="absolute bottom-1 left-[5px] top-1 w-px bg-border" aria-hidden />
                  {timeline.map((item) => (
                    <li key={item.key} className="relative">
                      <span
                        className={cn(
                          "absolute -left-5 top-1 size-[11px] rounded-full border-2 border-background",
                          item.tone === "ok" && "bg-emerald-500",
                          item.tone === "bad" && "bg-red-500",
                          item.tone === "info" && "bg-sky-500",
                          item.tone === "muted" && "bg-muted-foreground/40"
                        )}
                      />
                      <div className="flex items-baseline justify-between gap-3">
                        <span className="text-sm font-medium">{item.label}</span>
                        <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{formatDateTime(item.at)}</span>
                      </div>
                      {item.detail ? <p className="mt-0.5 break-all text-xs text-muted-foreground">{item.detail}</p> : null}
                    </li>
                  ))}
                </ol>
                {(detail?.events ?? []).length === 0 && delivery.status !== "failed" ? (
                  <p className="text-[11px] text-muted-foreground">
                    {providerMeta?.capabilities?.webhook
                      ? "还没有收到投递回执。若长时间没有，检查服务商控制台里回执地址是否已配置。"
                      : "这条通道没有投递回执，交给服务商即是能确认的最终状态。"}
                  </p>
                ) : null}
              </section>

              {delivery.lastEventPayload && Object.keys(delivery.lastEventPayload).length > 0 ? (
                <section className="space-y-2">
                  <h4 className="text-xs font-medium text-muted-foreground">最近一次回执原文</h4>
                  <pre className="max-h-64 overflow-auto rounded-xl border bg-muted/40 p-3 font-mono text-[11px] leading-relaxed">
                    {JSON.stringify(delivery.lastEventPayload, null, 2)}
                  </pre>
                </section>
              ) : null}
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function Row({ label, value, mono }: { label: string; value?: string | null; mono?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-4 px-4 py-2.5">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className={cn("min-w-0 break-all text-right", mono && "font-mono")}>{value || "—"}</dd>
    </div>
  );
}
