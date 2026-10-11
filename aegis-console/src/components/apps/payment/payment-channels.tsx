"use client";

import { useMemo, useState } from "react";
import { ChevronRight, CreditCard, Plus } from "lucide-react";
import type { PaymentConfig } from "@/lib/api/types";
import { useAdminPaymentConfigsQuery, useAdminPaymentMethodsQuery } from "@/lib/admin-hooks";
import { PaymentBrandBadge } from "@/components/payment/payment-brand-icon";
import { ListState, RefreshButton } from "@/components/payment/payment-list-parts";
import { relativeOrderTime } from "@/components/payment/payment-order-meta";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { PaymentChannelSheet, type ChannelSheetTarget } from "./payment-channel-sheet";

/** 收款渠道列表：已接入的商户配置，点一行进抽屉编辑。 */
export function PaymentChannels({ appId }: { appId: number }) {
  const configsQuery = useAdminPaymentConfigsQuery(appId);
  const methodsQuery = useAdminPaymentMethodsQuery();
  const [target, setTarget] = useState<ChannelSheetTarget | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  // 每次打开都自增，作为抽屉的 key：抽屉据此重新挂载并按最新 target 初始化
  const [session, setSession] = useState(0);

  const configs = useMemo(() => (configsQuery.data ?? []) as PaymentConfig[], [configsQuery.data]);
  const methods = useMemo(() => methodsQuery.data ?? [], [methodsQuery.data]);
  const metaByMethod = useMemo(() => new Map(methods.map((m) => [m.method, m] as const)), [methods]);
  const enabledCount = configs.filter((c) => c.enabled !== false).length;

  function open(next: ChannelSheetTarget) {
    setTarget(next);
    setSession((s) => s + 1);
    setSheetOpen(true);
  }

  return (
    <TooltipProvider delayDuration={300}>
      <section className="overflow-hidden rounded-2xl border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
        <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3 sm:px-5">
          <div className="min-w-0">
            <h3 className="text-sm font-semibold">收款渠道</h3>
            {configsQuery.isLoading ? null : (
              <p className="text-xs text-muted-foreground tabular-nums">
                {configs.length ? `已接入 ${configs.length} 个，启用 ${enabledCount} 个` : `平台支持 ${methods.length} 种渠道`}
              </p>
            )}
          </div>
          <div className="ml-auto flex items-center gap-1">
            <RefreshButton fetching={configsQuery.isFetching} onClick={() => void configsQuery.refetch()} />
            <Button size="sm" className="h-9 gap-1.5" onClick={() => open({ kind: "create" })}>
              <Plus className="size-4" />
              接入渠道
            </Button>
          </div>
        </header>

        {configsQuery.isLoading ? (
          <ul className="divide-y">
            {Array.from({ length: 3 }).map((_, i) => (
              <li key={i} className="flex items-center gap-3 px-4 py-4 sm:px-5">
                <Skeleton className="size-9 rounded-xl" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-36" />
                  <Skeleton className="h-3 w-52" />
                </div>
                <Skeleton className="h-5 w-12" />
              </li>
            ))}
          </ul>
        ) : configs.length === 0 ? (
          <ListState
            icon={<CreditCard className="size-5" />}
            title="尚未接入收款渠道"
            action={
              <Button size="sm" className="gap-1.5" onClick={() => open({ kind: "create" })}>
                <Plus className="size-4" />
                接入渠道
              </Button>
            }
          />
        ) : (
          <ul className="divide-y">
            {configs.map((cfg) => {
              const meta = metaByMethod.get(String(cfg.payment_method));
              const enabled = cfg.enabled !== false;
              return (
                <li key={cfg.id}>
                  <button
                    type="button"
                    onClick={() => open({ kind: "edit", config: cfg })}
                    className="flex w-full items-center gap-3 px-4 py-3.5 text-left transition-colors hover:bg-muted/40 focus-visible:bg-muted/50 focus-visible:outline-none sm:px-5"
                  >
                    <span className={cn(!enabled && "opacity-50 grayscale")}>
                      <PaymentBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name ?? String(cfg.payment_method)} />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex flex-wrap items-center gap-1.5">
                        <span className={cn("truncate text-sm font-medium", !enabled && "text-muted-foreground")}>
                          {cfg.config_name || meta?.name}
                        </span>
                        {cfg.is_default ? (
                          <Badge variant="info" size="sm">
                            默认
                          </Badge>
                        ) : null}
                        {!enabled ? (
                          <Badge variant="secondary" size="sm">
                            已停用
                          </Badge>
                        ) : null}
                      </span>
                      <span className="mt-0.5 flex min-w-0 gap-3 text-xs text-muted-foreground">
                        <span className="shrink-0">{meta?.name ?? cfg.payment_method}</span>
                        {cfg.description ? <span className="truncate">{cfg.description}</span> : null}
                      </span>
                    </span>
                    {cfg.updatedAt ? (
                      <span className="hidden shrink-0 text-xs text-muted-foreground tabular-nums sm:block">
                        更新于 {relativeOrderTime(cfg.updatedAt)}
                      </span>
                    ) : null}
                    <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      {target ? (
        <PaymentChannelSheet
          key={session}
          open={sheetOpen}
          onOpenChange={setSheetOpen}
          target={target}
          appId={appId}
          methods={methods}
          methodsLoading={methodsQuery.isLoading}
          configs={configs}
        />
      ) : null}
    </TooltipProvider>
  );
}
