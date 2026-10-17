"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, Cell, Label, Pie, PieChart, XAxis, YAxis } from "recharts";
import {
  AlertTriangle,
  Ban,
  ChevronRight,
  MailCheck,
  MailWarning,
  Paperclip,
  Plus,
  Send,
  Server,
  Share2
} from "lucide-react";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { EmailCount } from "@/lib/api/email-policy";
import type { EmailProviderMeta } from "@/lib/api/types";
import {
  useEmailChannelQuery,
  useEmailOverviewQuery,
  useEmailSceneCatalogQuery,
  type EmailScope
} from "@/lib/email-hooks";
import { cn } from "@/lib/utils";
import { EmailBrandBadge } from "./email-brand-icon";
import { DELIVERY_STATUS, deliveryStatus, formatNumber, formatRate, purposeLabeler } from "./email-shared";

const RANGES = [7, 30, 90] as const;

/** 趋势的四段：已送达、在途（已发送但没有回执）、退信与投诉、失败 */
const SERIES: Array<{ key: "delivered" | "inflight" | "bounced" | "failed"; label: string; color: ThemedColor }> = [
  { key: "delivered", label: "已送达", color: DELIVERY_STATUS.delivered.color },
  { key: "inflight", label: "已发送", color: DELIVERY_STATUS.sent.color },
  { key: "bounced", label: "退信与投诉", color: DELIVERY_STATUS.bounced.color },
  { key: "failed", label: "发送失败", color: DELIVERY_STATUS.failed.color }
];

export type EmailOverviewTarget =
  | { view: "channels" }
  | { view: "deliveries"; status?: string; purpose?: string }
  | { view: "suppressions" };

/**
 * 概览：当前从哪条通道发信、要不要处理什么、发了多少、去哪儿了。
 *
 * 累计数字不受时间窗口影响；趋势与两张分布图按所选天数，趋势按浏览器时区分日。
 * SMTP 没有投递回执，它发出的信停在「已发送」是正常的 —— 趋势里单独一段「已发送」就是为了不把它们算成异常。
 */
export function EmailOverviewPanel({
  scope,
  providers,
  onNavigate
}: {
  scope: EmailScope;
  providers: EmailProviderMeta[];
  onNavigate: (target: EmailOverviewTarget) => void;
}) {
  const [days, setDays] = useState<number>(30);
  const channelQuery = useEmailChannelQuery(scope);
  const overviewQuery = useEmailOverviewQuery(scope, days);
  const scenesQuery = useEmailSceneCatalogQuery();
  const overview = overviewQuery.data;
  const labelOf = useMemo(() => purposeLabeler(scenesQuery.data?.scenes), [scenesQuery.data]);

  const trendConfig = useMemo(() => buildChartConfig(SERIES), []);
  const rows = useMemo(
    () =>
      (overview?.trend ?? []).map((day) => ({
        label: day.date.slice(5),
        delivered: day.delivered,
        bounced: day.bounced,
        failed: day.failed,
        inflight: Math.max(0, day.total - day.delivered - day.bounced - day.failed)
      })),
    [overview]
  );
  const trendEmpty = rows.every((row) => row.delivered + row.bounced + row.failed + row.inflight === 0);
  const loading = overviewQuery.isLoading;
  const windowText = overview ? `近 ${days} 天发出 ${formatNumber(overview.windowTotal)} 封` : `近 ${days} 天`;
  const finished = overview ? overview.total - overview.pending : 0;

  const rangePicker = (
    <ToggleGroup
      type="single"
      size="sm"
      value={String(days)}
      onValueChange={(next) => next && setDays(Number(next))}
      className="shrink-0"
    >
      {RANGES.map((range) => (
        <ToggleGroupItem key={range} value={String(range)} className="h-7 px-2.5 text-xs">
          {range} 天
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );

  return (
    <div className="space-y-4">
      <ChannelCard
        loading={channelQuery.isLoading}
        resolution={channelQuery.data ?? null}
        providers={providers}
        onManage={() => onNavigate({ view: "channels" })}
      />

      {overview && overview.failedLast24h > 0 ? (
        <AttentionButton
          tone="danger"
          icon={AlertTriangle}
          label={`近 24 小时有 ${formatNumber(overview.failedLast24h)} 封发送失败`}
          onClick={() => onNavigate({ view: "deliveries", status: "failed" })}
        />
      ) : null}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile label="近 24 小时" icon={Send} value={loading ? "—" : formatNumber(overview?.last24h)} hint="发出的封数" />
        <button type="button" className="text-left" onClick={() => onNavigate({ view: "deliveries" })}>
          <StatTile
            label="累计发信"
            icon={MailCheck}
            value={loading ? "—" : formatNumber(overview?.total)}
            hint={overview ? `已送达 ${formatNumber(overview.delivered)}` : undefined}
          />
        </button>
        <button
          type="button"
          className="text-left"
          disabled={!overview?.bounced}
          onClick={() => onNavigate({ view: "deliveries", status: "bounced" })}
        >
          <StatTile
            label="退信率"
            icon={MailWarning}
            tone={overview && overview.bounced > 0 ? "default" : "muted"}
            value={overview ? formatRate(overview.bounced, finished) : "—"}
            hint={overview ? `退信与投诉 ${formatNumber(overview.bounced)}` : undefined}
          />
        </button>
        <button type="button" className="text-left" onClick={() => onNavigate({ view: "suppressions" })}>
          <StatTile
            label="抑制名单"
            icon={Ban}
            tone={overview && overview.suppressions > 0 ? "default" : "muted"}
            value={loading ? "—" : formatNumber(overview?.suppressions)}
            hint="暂停投递的地址"
          />
        </button>
      </div>

      <ChartCard title="发信趋势" description={windowText} loading={loading} empty={trendEmpty} height={230} action={rangePicker}>
        <ChartContainer config={trendConfig} className="h-[230px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={12} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {SERIES.map((series, index) => (
              <Bar
                key={series.key}
                dataKey={series.key}
                stackId="mail"
                fill={`var(--color-${series.key})`}
                radius={index === SERIES.length - 1 ? [3, 3, 0, 0] : 0}
              />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <PurposeBars
          loading={loading}
          description={windowText}
          items={overview?.byPurpose ?? []}
          labelOf={labelOf}
          onPick={(purpose) => onNavigate({ view: "deliveries", purpose })}
        />
        <StatusDonut loading={loading} description={windowText} items={overview?.byStatus ?? []} />
      </div>
    </div>
  );
}

function ChannelCard({
  loading,
  resolution,
  providers,
  onManage
}: {
  loading: boolean;
  resolution: ReturnType<typeof useEmailChannelQuery>["data"] | null;
  providers: EmailProviderMeta[];
  onManage: () => void;
}) {
  if (loading) {
    return <Skeleton className="h-[72px] w-full rounded-xl" />;
  }
  if (!resolution) {
    return (
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50/60 px-4 py-3 dark:border-amber-900/60 dark:bg-amber-950/30">
        <div className="flex min-w-0 items-center gap-3">
          <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-amber-500/15 text-amber-700 dark:text-amber-300">
            <AlertTriangle className="size-5" />
          </span>
          <div className="min-w-0">
            <p className="text-sm font-medium text-amber-900 dark:text-amber-200">还没有可用的发信通道</p>
            <p className="text-xs text-amber-800/80 dark:text-amber-300/80">验证码、通知与凭证邮件都无法发出</p>
          </div>
        </div>
        <Button size="sm" className="h-8" onClick={onManage}>
          <Plus className="size-3.5" /> 新建通道
        </Button>
      </div>
    );
  }

  const meta = providers.find((item) => item.provider === resolution.provider);
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-card px-4 py-3">
      <div className="flex min-w-0 items-center gap-3">
        <EmailBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name} />
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="truncate text-sm font-semibold">{resolution.configName}</span>
            <Badge variant="secondary" size="sm" className="font-normal">
              {meta?.name ?? resolution.provider}
            </Badge>
            {resolution.inherited ? (
              <Badge variant="info" size="sm" className="gap-1 font-normal">
                <Share2 className="size-3" /> 平台共享通道
              </Badge>
            ) : null}
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
            <span className="inline-flex items-center gap-1">
              <Server className="size-3" /> 当前发信通道
            </span>
            <span className="inline-flex items-center gap-1">
              <Paperclip className="size-3" /> {resolution.attachments ? "支持附件" : "不支持附件"}
            </span>
            <span>{meta?.capabilities?.webhook ? "有投递回执" : "无投递回执，止于已发送"}</span>
          </div>
        </div>
      </div>
      <Button size="sm" variant="outline" className="h-8" onClick={onManage}>
        管理通道 <ChevronRight className="size-3.5" />
      </Button>
    </div>
  );
}

function AttentionButton({
  icon: Icon,
  tone,
  label,
  onClick
}: {
  icon: typeof AlertTriangle;
  tone: "warning" | "danger";
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2.5 rounded-xl border px-4 py-2.5 text-left text-sm transition-colors",
        tone === "warning"
          ? "border-amber-200 bg-amber-50/60 text-amber-800 hover:bg-amber-100/70 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300 dark:hover:bg-amber-950/50"
          : "border-red-200 bg-red-50/60 text-red-700 hover:bg-red-100/70 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300 dark:hover:bg-red-950/50"
      )}
    >
      <Icon className="size-4 shrink-0" />
      <span className="flex-1 font-medium">{label}</span>
      <ChevronRight className="size-4 shrink-0 opacity-60" />
    </button>
  );
}

/** 按用途：横向条形，点一条跳到按该用途筛选的投递记录 */
function PurposeBars({
  loading,
  description,
  items,
  labelOf,
  onPick
}: {
  loading: boolean;
  description: string;
  items: EmailCount[];
  labelOf: (purpose?: string | null) => string;
  onPick: (purpose: string) => void;
}) {
  const rows = useMemo(
    () =>
      items
        .filter((item) => item.count > 0)
        .slice(0, 8)
        .map((item) => ({ key: item.key, name: labelOf(item.key), count: item.count })),
    [items, labelOf]
  );
  const config = useMemo(() => buildChartConfig([{ key: "count", label: "封数" }]), []);
  const height = Math.max(160, rows.length * 30 + 20);

  return (
    <ChartCard title="按用途" description={description} loading={loading} empty={rows.length === 0} height={220}>
      <ChartContainer config={config} className="w-full" style={{ height }}>
        <BarChart data={rows} layout="vertical" margin={{ left: 8, right: 12 }}>
          <CartesianGrid horizontal={false} strokeDasharray="3 3" />
          <XAxis type="number" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} />
          <YAxis type="category" dataKey="name" tickLine={false} axisLine={false} width={96} fontSize={11} />
          <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
          <Bar
            dataKey="count"
            radius={[0, 4, 4, 0]}
            barSize={16}
            className="cursor-pointer"
            onClick={(data) => {
              const key = (data?.payload as { key?: string } | undefined)?.key;
              if (key && key !== "unknown") onPick(key);
            }}
          >
            {rows.map((row, index) => (
              <Cell key={row.key} fill={`var(--chart-${(index % 5) + 1})`} />
            ))}
          </Bar>
        </BarChart>
      </ChartContainer>
    </ChartCard>
  );
}

/** 按状态：环形图，配色与列表徽标一致 */
function StatusDonut({ loading, description, items }: { loading: boolean; description: string; items: EmailCount[] }) {
  const rows = useMemo(
    () => items.filter((item) => item.count > 0).map((item, index) => ({ ...item, slot: `s${index}` })),
    [items]
  );
  const total = rows.reduce((sum, item) => sum + item.count, 0);
  const config = useMemo(
    () =>
      buildChartConfig(
        rows.map((item) => ({ key: item.slot, label: deliveryStatus(item.key).label, color: deliveryStatus(item.key).color }))
      ),
    [rows]
  );

  return (
    <ChartCard title="按状态" description={description} loading={loading} empty={total === 0} height={220}>
      <ChartContainer config={config} className="mx-auto h-[220px] w-full">
        <PieChart>
          <ChartTooltip content={<ChartTooltipContent nameKey="slot" hideLabel />} />
          <Pie data={rows} dataKey="count" nameKey="slot" innerRadius={55} outerRadius={85} strokeWidth={2} paddingAngle={2}>
            {rows.map((item) => (
              <Cell key={item.slot} fill={`var(--color-${item.slot})`} />
            ))}
            <Label
              content={({ viewBox }) => {
                if (!viewBox || !("cx" in viewBox)) return null;
                return (
                  <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                    <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-xl font-bold">
                      {total.toLocaleString("zh-CN")}
                    </tspan>
                    <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-[10px]">
                      封邮件
                    </tspan>
                  </text>
                );
              }}
            />
          </Pie>
          <ChartLegend content={<ChartLegendContent nameKey="slot" className="flex-wrap gap-x-3 gap-y-1" />} />
        </PieChart>
      </ChartContainer>
    </ChartCard>
  );
}
