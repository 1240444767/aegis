"use client";

import { useMemo, useState } from "react";
import { ChevronLeft, ChevronRight, Inbox, RefreshCw, Search, Share2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import type { EmailDelivery, EmailProviderMeta } from "@/lib/api/types";
import { formatDateTimeMinute } from "@/lib/datetime";
import { useEmailDeliveriesQuery, useEmailSceneCatalogQuery, type EmailScope } from "@/lib/email-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { EmailBrandBadge } from "./email-brand-icon";
import { EmailDeliverySheet } from "./email-delivery-sheet";
import { DELIVERY_STATUS, deliveryStatus, formatNumber, purposeLabeler } from "./email-shared";

const PAGE_SIZE = 30;

/**
 * 投递记录。点一行看这封信的完整留痕与回执时间线。
 *
 * 状态语义按通道分两档：SMTP 止于「已发送」（协议没有回执），其余服务商会由回执推进到送达或退信。
 * 因此「停在已发送」在前一档是正常的，在后一档才值得怀疑。
 */
export function EmailDeliveryTable({
  scope,
  providers,
  initialStatus,
  initialPurpose
}: {
  scope: EmailScope;
  providers: EmailProviderMeta[];
  initialStatus?: string;
  initialPurpose?: string;
}) {
  const [status, setStatus] = useState(initialStatus ?? "all");
  const [provider, setProvider] = useState("all");
  const [purpose, setPurpose] = useState(initialPurpose ?? "all");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [openId, setOpenId] = useState<number | null>(null);
  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const scenesQuery = useEmailSceneCatalogQuery();
  const labelOf = useMemo(() => purposeLabeler(scenesQuery.data?.scenes), [scenesQuery.data]);
  const purposeOptions = useMemo(() => {
    const options = new Map<string, string>();
    for (const scene of scenesQuery.data?.scenes ?? []) {
      const first = scene.purposes?.[0];
      if (first) options.set(first, scene.name);
    }
    for (const extra of ["test", "notification", "receipt", "password_reset_notice"]) {
      options.set(extra, labelOf(extra));
    }
    if (initialPurpose && !options.has(initialPurpose)) options.set(initialPurpose, labelOf(initialPurpose));
    return [...options.entries()];
  }, [scenesQuery.data, labelOf, initialPurpose]);

  const query = useEmailDeliveriesQuery(scope, {
    status: status === "all" ? undefined : status,
    provider: provider === "all" ? undefined : provider,
    purpose: purpose === "all" ? undefined : purpose,
    keyword: debouncedKeyword.trim() || undefined,
    page,
    pageSize: PAGE_SIZE
  });

  const items: EmailDelivery[] = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / (query.data?.pageSize ?? PAGE_SIZE)));

  /** 筛选变化回到第 1 页，否则会停在新条件下不存在的页码上 */
  const withReset =
    <T,>(setter: (value: T) => void) =>
    (value: T) => {
      setter(value);
      setPage(1);
    };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-64">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8 text-sm"
            placeholder="收件地址或主题"
            value={keyword}
            onChange={(event) => withReset(setKeyword)(event.target.value)}
          />
        </div>
        <FilterSelect value={status} onChange={withReset(setStatus)} allLabel="全部状态" width="w-32">
          {Object.entries(DELIVERY_STATUS).map(([value, meta]) => (
            <SelectItem key={value} value={value}>
              {meta.label}
            </SelectItem>
          ))}
        </FilterSelect>
        <FilterSelect value={purpose} onChange={withReset(setPurpose)} allLabel="全部用途" width="w-36">
          {purposeOptions.map(([value, label]) => (
            <SelectItem key={value} value={value}>
              {label}
            </SelectItem>
          ))}
        </FilterSelect>
        <FilterSelect value={provider} onChange={withReset(setProvider)} allLabel="全部服务商" width="w-36">
          {providers.map((item) => (
            <SelectItem key={item.provider} value={item.provider}>
              {item.name}
            </SelectItem>
          ))}
        </FilterSelect>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-xs text-muted-foreground">共 {formatNumber(total)} 封</span>
          <Button
            type="button"
            size="icon"
            variant="outline"
            className="size-8"
            aria-label="刷新"
            onClick={() => void query.refetch()}
            disabled={query.isFetching}
          >
            <RefreshCw className={cn("size-3.5", query.isFetching && "animate-spin")} />
          </Button>
        </div>
      </div>

      <div className="overflow-hidden rounded-2xl border bg-card">
        {query.isLoading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 6 }).map((_, index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-16 text-center">
            <Inbox className="size-6 text-muted-foreground/60" />
            <p className="text-sm text-muted-foreground">没有符合条件的投递记录</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 text-left font-medium">收件人与主题</th>
                  <th className="px-3 py-2.5 text-left font-medium">用途</th>
                  <th className="px-3 py-2.5 text-left font-medium">通道</th>
                  <th className="px-3 py-2.5 text-left font-medium">状态</th>
                  <th className="px-4 py-2.5 text-right font-medium">时间</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => {
                  const meta = deliveryStatus(item.status);
                  const providerMeta = providers.find((p) => p.provider === item.provider);
                  return (
                    <tr
                      key={item.id}
                      onClick={() => setOpenId(item.id)}
                      className="cursor-pointer border-b transition-colors last:border-b-0 hover:bg-muted/40"
                    >
                      <td className="max-w-[26rem] px-4 py-2.5">
                        <div className="truncate font-mono text-xs">{item.toAddress}</div>
                        <div className="truncate text-xs text-muted-foreground" title={item.subject}>
                          {item.subject || "无主题"}
                        </div>
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 text-xs">{labelOf(item.purpose)}</td>
                      <td className="px-3 py-2.5">
                        <div className="flex items-center gap-2">
                          <EmailBrandBadge
                            slug={providerMeta?.icon}
                            brandColor={providerMeta?.brandColor}
                            name={providerMeta?.name}
                            size="sm"
                            className="size-6! rounded-md!"
                          />
                          <div className="min-w-0 text-xs">
                            <div className="truncate">{item.configName || providerMeta?.name || item.provider}</div>
                            {item.inherited ? (
                              <div className="flex items-center gap-1 text-[10px] text-sky-600 dark:text-sky-400">
                                <Share2 className="size-3" /> 平台共享
                              </div>
                            ) : null}
                          </div>
                        </div>
                      </td>
                      <td className="px-3 py-2.5">
                        <Badge variant={meta.tone} size="sm">
                          {meta.label}
                        </Badge>
                        {item.errorMessage ? (
                          <div className="mt-1 max-w-[16rem] truncate text-[11px] text-muted-foreground" title={item.errorMessage}>
                            {item.errorMessage}
                          </div>
                        ) : null}
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 text-right text-xs tabular-nums text-muted-foreground">
                        {formatDateTimeMinute(item.deliveredAt || item.sentAt || item.createdAt)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {totalPages > 1 ? (
        <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
          <Button
            size="icon"
            variant="outline"
            className="size-7"
            aria-label="上一页"
            disabled={page <= 1}
            onClick={() => setPage((value) => Math.max(1, value - 1))}
          >
            <ChevronLeft className="size-3.5" />
          </Button>
          <span className="tabular-nums">
            {page} / {totalPages}
          </span>
          <Button
            size="icon"
            variant="outline"
            className="size-7"
            aria-label="下一页"
            disabled={page >= totalPages}
            onClick={() => setPage((value) => Math.min(totalPages, value + 1))}
          >
            <ChevronRight className="size-3.5" />
          </Button>
        </div>
      ) : null}

      <EmailDeliverySheet
        scope={scope}
        deliveryId={openId}
        providers={providers}
        labelOf={labelOf}
        onClose={() => setOpenId(null)}
      />
    </div>
  );
}

function FilterSelect({
  value,
  onChange,
  allLabel,
  width,
  children
}: {
  value: string;
  onChange: (value: string) => void;
  allLabel: string;
  width: string;
  children: React.ReactNode;
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className={cn("h-8 text-sm", width)}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">{allLabel}</SelectItem>
        {children}
      </SelectContent>
    </Select>
  );
}
