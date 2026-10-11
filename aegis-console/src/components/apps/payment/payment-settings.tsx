"use client";

import { useState, type ReactNode } from "react";
import { Loader2, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { AppCommerceSettings } from "@/lib/api/types";
import { useAdminAppCommerceSettingsQuery, useUpdateAdminAppCommerceSettingsMutation } from "@/lib/admin-hooks";
import { usePaymentReceiptCapabilityQuery } from "@/lib/commerce-hooks";
import { CurrencySelect } from "@/components/commerce/currency-select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

/** 「跟随用户设置」的哨兵值：Select 不接受空字符串作为选项值 */
const AUTO_LOCALE = "__auto__";
const RATE_PRESETS = ["10", "100", "500", "1000"];

type Draft = {
  integralPerCurrency: string;
  receiptEmailOnPaid: boolean;
  receiptLocale: string;
  walletCurrency: string;
};

function fromServer(server?: AppCommerceSettings): Draft {
  return {
    integralPerCurrency: String(server?.integralPerCurrency ?? 100),
    receiptEmailOnPaid: Boolean(server?.receiptEmailOnPaid),
    receiptLocale: server?.receiptLocale?.trim() || "",
    walletCurrency: server?.walletCurrency?.trim() || "CNY"
  };
}

/**
 * 交易设置。
 *
 * 四个字段**必须整体提交**：后端的 PUT 是全量覆盖。曾经只发 `integralPerCurrency`，
 * 于是每次保存都会把 `receiptEmailOnPaid` 静默关掉、把 `receiptLocale` 清空 ——
 * 表现是「凭证自动寄送前几天还好好的，改了下兑换率就不发了」。
 *
 * 草稿为空时直接从服务端值派生（不用 effect 同步）；改过才出现保存条。
 */
export function PaymentSettings({ appId, appKey }: { appId: number; appKey: string }) {
  const query = useAdminAppCommerceSettingsQuery(appKey);
  const mutation = useUpdateAdminAppCommerceSettingsMutation(appKey);
  const capabilityQuery = usePaymentReceiptCapabilityQuery(appId);
  const [draft, setDraft] = useState<Draft | null>(null);

  const saved = fromServer(query.data);
  const current = draft ?? saved;
  const dirty = draft !== null && JSON.stringify(draft) !== JSON.stringify(saved);
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes });

  const parsedRate = Number(current.integralPerCurrency);
  const rateValid = Number.isInteger(parsedRate) && parsedRate >= 1 && parsedRate <= 1_000_000;
  const currencyValid = /^[A-Za-z]{3}$/.test(current.walletCurrency.trim());
  const locales = capabilityQuery.data?.locales ?? [];

  async function save() {
    if (!rateValid) {
      toast.error("积分兑换率需为 1 至 1000000 之间的整数");
      return;
    }
    if (!currencyValid) {
      toast.error("钱包币种需为 3 位 ISO 4217 代码");
      return;
    }
    try {
      await mutation.mutateAsync({
        integralPerCurrency: parsedRate,
        receiptEmailOnPaid: current.receiptEmailOnPaid,
        receiptLocale: current.receiptLocale,
        walletCurrency: current.walletCurrency.trim().toUpperCase()
      });
      setDraft(null);
      toast.success("交易设置已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  if (query.isLoading) {
    return (
      <div className="overflow-hidden rounded-2xl border bg-card">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="flex flex-col gap-3 border-b px-5 py-5 last:border-b-0 md:flex-row md:items-center">
            <div className="space-y-2 md:w-72">
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-3 w-44" />
            </div>
            <Skeleton className="h-9 w-56 rounded-lg" />
          </div>
        ))}
      </div>
    );
  }

  return (
    <section className="overflow-hidden rounded-2xl border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
      <header className="border-b px-4 py-3 sm:px-5">
        <h3 className="text-sm font-semibold">交易设置</h3>
      </header>

      <SettingRow title="积分兑换率" description="积分充值订单每 1 单位金额兑换的积分">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative w-36">
            <Input
              type="number"
              min={1}
              max={1000000}
              inputMode="numeric"
              aria-invalid={!rateValid}
              className="h-9 pr-10 tabular-nums"
              value={current.integralPerCurrency}
              onChange={(e) => patch({ integralPerCurrency: e.target.value })}
            />
            <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-muted-foreground">积分</span>
          </div>
          <div className="flex gap-1">
            {RATE_PRESETS.map((preset) => (
              <button
                key={preset}
                type="button"
                onClick={() => patch({ integralPerCurrency: preset })}
                className={cn(
                  "h-9 min-w-11 rounded-lg border px-2 text-xs tabular-nums transition-colors",
                  current.integralPerCurrency === preset
                    ? "border-foreground bg-foreground text-background"
                    : "text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                )}
              >
                {preset}
              </button>
            ))}
          </div>
        </div>
        <p className={cn("mt-2 text-xs tabular-nums", rateValid ? "text-muted-foreground" : "text-destructive")}>
          {rateValid ? `支付 10 元可得 ${(parsedRate * 10).toLocaleString("zh-CN")} 积分` : "需为 1 至 1000000 之间的整数"}
        </p>
      </SettingRow>

      <SettingRow title="钱包币种" description="钱包余额与流水凭证使用的币种">
        <div className="w-full max-w-64">
          <CurrencySelect value={current.walletCurrency} onChange={(code) => patch({ walletCurrency: code })} />
        </div>
        {!currencyValid ? <p className="mt-2 text-xs text-destructive">需为 3 位 ISO 4217 代码</p> : null}
      </SettingRow>

      <SettingRow title="自动寄送凭证" description="支付成功后发送到下单用户绑定的邮箱">
        <Switch
          checked={current.receiptEmailOnPaid}
          onCheckedChange={(value) => patch({ receiptEmailOnPaid: value })}
          aria-label="自动寄送凭证"
        />
      </SettingRow>

      <SettingRow title="凭证语言" description="自动寄送凭证时使用的语言" muted={!current.receiptEmailOnPaid}>
        <Select
          value={current.receiptLocale || AUTO_LOCALE}
          disabled={!current.receiptEmailOnPaid}
          onValueChange={(value) => patch({ receiptLocale: value === AUTO_LOCALE ? "" : value })}
        >
          <SelectTrigger className="h-9 w-full max-w-64">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={AUTO_LOCALE}>跟随用户设置</SelectItem>
            {locales.map((locale) => (
              <SelectItem key={locale.tag} value={locale.tag}>
                {locale.nativeName}
                {locale.available ? "" : "（缺少字体，将以英文输出）"}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </SettingRow>

      <footer
        className={cn(
          "flex flex-wrap items-center justify-end gap-2 border-t px-4 py-3 transition-colors sm:px-5",
          dirty ? "bg-amber-500/5" : "bg-muted/20"
        )}
      >
        {dirty ? (
          <span className="mr-auto flex items-center gap-2 text-sm">
            <span className="size-2 rounded-full bg-amber-500" />
            有未保存的修改
          </span>
        ) : null}
        <Button size="sm" variant="outline" className="h-9 gap-1.5" disabled={!dirty || mutation.isPending} onClick={() => setDraft(null)}>
          <RotateCcw className="size-4" />
          放弃修改
        </Button>
        <Button size="sm" className="h-9 min-w-20 gap-1.5" disabled={!dirty || mutation.isPending} onClick={() => void save()}>
          {mutation.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
          保存
        </Button>
      </footer>
    </section>
  );
}

function SettingRow({
  title,
  description,
  muted,
  children
}: {
  title: string;
  description: string;
  muted?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="grid gap-3 border-b px-4 py-5 last:border-b-0 sm:px-5 md:grid-cols-[minmax(0,18rem)_minmax(0,1fr)] md:items-center md:gap-8">
      <div className={cn("space-y-0.5", muted && "opacity-60")}>
        <p className="text-sm font-medium">{title}</p>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}
