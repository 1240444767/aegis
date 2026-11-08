"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, Cell, Label, Pie, PieChart, ReferenceLine, XAxis, YAxis } from "recharts";
import {
  Ban,
  CircleUser,
  Layers,
  ListChecks,
  LogOut,
  MonitorSmartphone,
  ShieldPlus,
  ShieldX,
  SlidersHorizontal,
  Target,
  Users
} from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import {
  ACTION_META,
  AUDIENCE_META,
  Block,
  CLASS_META,
  ClassIcon,
  DEVICE_CLASSES,
  DIMENSION_LABEL,
  DeviceSlots,
  formatCount,
  limitText,
  OVERFLOW_META,
  SAME_DEVICE_META,
  TREND_ACTIONS
} from "@/components/apps/device-limit/device-limit-shared";
import type { PickedUser } from "@/components/commerce/user-picker";
import { buildChartConfig, ChartCard } from "@/components/commerce/commerce-charts";
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
import type { DeviceDimension, DeviceLimitOverview, DeviceLimitPolicy } from "@/lib/api/device-limit";
import { formatRelative } from "@/lib/datetime";
import { useDeviceLimitOverviewQuery } from "@/lib/device-limit-hooks";
import { cn } from "@/lib/utils";

const RANGES = [7, 14, 30] as const;

const TREND_CONFIG = buildChartConfig(
  TREND_ACTIONS.map((action) => ({ key: action, label: ACTION_META[action].label, color: ACTION_META[action].color }))
);
const CLASS_CONFIG = buildChartConfig(
  DEVICE_CLASSES.map((cls) => ({ key: cls, label: CLASS_META[cls].label, color: CLASS_META[cls].color }))
);
const DISTRIBUTION_CONFIG = buildChartConfig([
  { key: "users", label: "用户", color: { light: "#6366f1", dark: "#818cf8" } }
]);
const DIMENSION_CONFIG = buildChartConfig([
  { key: "count", label: "次数", color: { light: "#f59e0b", dark: "#fbbf24" } }
]);

/**
 * 概览。处置次数与趋势按所选天数、按浏览器时区分日；设备分布取台账里窗口内出现过的设备，
 * 它是「登录过」的口径，不是此刻在线 —— 此刻在线要逐个用户看会话，放在用户设备视图里。
 */
export function DeviceLimitOverviewPanel({
  appKey,
  onOpenPolicy,
  onOpenUser
}: {
  appKey: string;
  onOpenPolicy: () => void;
  onOpenUser: (user: PickedUser) => void;
}) {
  const [days, setDays] = useState<number>(7);
  const query = useDeviceLimitOverviewQuery(appKey, days);
  const data = query.data;
  const loading = query.isLoading;

  const trendRows = useMemo(
    () =>
      (data?.trend ?? []).map((point) => {
        const counts: Record<string, number> = {};
        for (const action of TREND_ACTIONS) counts[action] = point.counts?.[action] ?? 0;
        return { label: point.date.slice(5), counts, ...counts };
      }),
    [data]
  );
  const trendEmpty = trendRows.every((row) => TREND_ACTIONS.every((action) => !row.counts[action]));

  const classRows = useMemo(
    () =>
      DEVICE_CLASSES.map((cls) => ({ key: cls, label: CLASS_META[cls].label, value: data?.byClass?.[cls] ?? 0 })).filter(
        (row) => row.value > 0
      ),
    [data]
  );

  const distributionRows = useMemo(
    () =>
      Array.from({ length: 10 }, (_, index) => ({
        bucket: index === 9 ? "10+" : String(index + 1),
        users: data?.distribution?.[String(index + 1)] ?? 0
      })),
    [data]
  );
  const distributionEmpty = distributionRows.every((row) => row.users === 0);

  const dimensionRows = useMemo(
    () =>
      (["class", "total", "new_device"] as DeviceDimension[]).map((dimension) => ({
        label: DIMENSION_LABEL[dimension],
        count: data?.dimensions?.[dimension] ?? 0
      })),
    [data]
  );
  const dimensionEmpty = dimensionRows.every((row) => row.count === 0);

  const total = (action: keyof DeviceLimitOverview["totals"]) => (data ? formatCount(data.totals?.[action]) : "—");
  const value = (count?: number) => (data ? formatCount(count) : "—");
  const policy = data?.policy;
  const defaultLimit = policy?.enabled ? policy.maxDevices : 0;

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
      <PolicySummary policy={policy} configured={data?.configured ?? false} loading={loading} onOpenPolicy={onOpenPolicy} aside={rangePicker} />

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="自动下线" icon={LogOut} value={total("kicked")} />
        <StatTile label="拒绝登录" icon={Ban} value={total("rejected")} />
        <StatTile
          label="请用户选择"
          icon={ListChecks}
          value={total("challenged")}
          hint={data && data.totals?.challenged ? `已选择 ${formatCount(data.totals.resolved)}` : undefined}
        />
        <StatTile label="新设备拦截" icon={ShieldX} value={total("new_device")} />
        <StatTile label="活跃设备" icon={MonitorSmartphone} value={value(data?.activeDevices)} />
        <StatTile
          label="活跃用户"
          icon={Users}
          value={value(data?.activeUsers)}
          hint={
            data && data.activeUsers
              ? `人均 ${(data.activeDevices / data.activeUsers).toFixed(1)} 台`
              : undefined
          }
        />
        <StatTile
          label="设备数达默认上限"
          icon={Target}
          value={defaultLimit > 0 ? value(data?.atLimitUsers) : "—"}
          tone={defaultLimit > 0 ? "default" : "muted"}
        />
        <StatTile label="例外用户" icon={ShieldPlus} value={value(data?.overrides)} />
      </div>

      <ChartCard title="处置趋势" loading={loading} empty={trendEmpty} height={220}>
        <ChartContainer config={TREND_CONFIG} className="h-[220px] w-full">
          <BarChart data={trendRows} margin={{ left: 0, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {TREND_ACTIONS.map((action, index) => (
              <Bar
                key={action}
                dataKey={action}
                stackId="events"
                fill={`var(--color-${action})`}
                radius={index === TREND_ACTIONS.length - 1 ? [3, 3, 0, 0] : 0}
              />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title="终端分布" loading={loading} empty={classRows.length === 0} emptyText="暂无设备" height={220}>
          <div className="grid items-center gap-4 sm:grid-cols-[220px_minmax(0,1fr)]">
            <ChartContainer config={CLASS_CONFIG} className="mx-auto aspect-square h-[220px]">
              <PieChart>
                <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} />
                <Pie data={classRows} dataKey="value" nameKey="key" innerRadius={62} outerRadius={92} strokeWidth={2} paddingAngle={2}>
                  {classRows.map((row) => (
                    <Cell key={row.key} fill={`var(--color-${row.key})`} />
                  ))}
                  <Label
                    content={({ viewBox }) => {
                      if (!viewBox || !("cx" in viewBox)) return null;
                      return (
                        <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                          <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) - 6} className="fill-foreground text-xl font-semibold">
                            {formatCount(data?.activeDevices)}
                          </tspan>
                          <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 14} className="fill-muted-foreground text-[11px]">
                            台设备
                          </tspan>
                        </text>
                      );
                    }}
                  />
                </Pie>
              </PieChart>
            </ChartContainer>
            <ul className="space-y-2">
              {DEVICE_CLASSES.map((cls) => {
                const count = data?.byClass?.[cls] ?? 0;
                const share = data?.activeDevices ? (count / data.activeDevices) * 100 : 0;
                return (
                  <li key={cls} className="flex items-center gap-3 rounded-lg px-2 py-1.5">
                    <span className={cn("grid size-7 place-items-center rounded-md ring-1 ring-inset", CLASS_META[cls].tint)}>
                      <ClassIcon cls={cls} className="size-3.5" />
                    </span>
                    <span className="flex-1 text-xs">{CLASS_META[cls].label}</span>
                    <span className="text-xs font-medium tabular-nums">{formatCount(count)}</span>
                    <span className="w-12 text-right text-[11px] tabular-nums text-muted-foreground">{share.toFixed(1)}%</span>
                  </li>
                );
              })}
            </ul>
          </div>
        </ChartCard>

        <ChartCard
          title="每人设备数"
          loading={loading}
          empty={distributionEmpty}
          emptyText="暂无设备"
          height={220}
          action={defaultLimit > 0 ? <Badge variant="outline" size="sm">默认上限 {defaultLimit} 台</Badge> : null}
        >
          <ChartContainer config={DISTRIBUTION_CONFIG} className="h-[220px] w-full">
            <BarChart data={distributionRows} margin={{ left: 0, right: 8, top: 16 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="bucket" tickLine={false} axisLine={false} fontSize={11} tickFormatter={(v) => `${v} 台`} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
              <ChartTooltip content={<ChartTooltipContent labelFormatter={(label) => `${label} 台设备`} />} />
              <Bar dataKey="users" radius={[3, 3, 0, 0]}>
                {distributionRows.map((row, index) => (
                  <Cell
                    key={row.bucket}
                    fill="var(--color-users)"
                    // 达到默认上限的那几档加深：一眼看出有多少人顶在上限上
                    fillOpacity={defaultLimit > 0 && index + 1 >= defaultLimit ? 1 : 0.5}
                  />
                ))}
              </Bar>
              {defaultLimit > 0 && defaultLimit <= 10 ? (
                <ReferenceLine
                  x={defaultLimit === 10 ? "10+" : String(defaultLimit)}
                  stroke="var(--color-users)"
                  strokeDasharray="4 4"
                  label={{ value: "上限", position: "top", fontSize: 10, fill: "currentColor" }}
                />
              ) : null}
            </BarChart>
          </ChartContainer>
        </ChartCard>
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        <ChartCard title="触发维度" loading={loading} empty={dimensionEmpty} height={180}>
          <ChartContainer config={DIMENSION_CONFIG} className="h-[180px] w-full">
            <BarChart data={dimensionRows} layout="vertical" margin={{ left: 8, right: 24, top: 4, bottom: 4 }}>
              <XAxis type="number" hide allowDecimals={false} />
              <YAxis dataKey="label" type="category" tickLine={false} axisLine={false} fontSize={11} width={72} />
              <ChartTooltip content={<ChartTooltipContent hideLabel />} />
              <Bar dataKey="count" fill="var(--color-count)" radius={[0, 4, 4, 0]} barSize={18} />
            </BarChart>
          </ChartContainer>
        </ChartCard>

        <Block title="撞上限最多的用户" bodyClassName="p-0">
          {loading ? (
            <div className="space-y-2 p-4">
              {[0, 1, 2, 3].map((index) => (
                <Skeleton key={index} className="h-9 w-full" />
              ))}
            </div>
          ) : !data?.topUsers?.length ? (
            <p className="px-4 py-10 text-center text-xs text-muted-foreground">暂无</p>
          ) : (
            <ul className="divide-y divide-border">
              {data.topUsers.map((item, index) => (
                <li key={item.userId}>
                  <button
                    type="button"
                    onClick={() => onOpenUser({ id: item.userId, account: item.account })}
                    className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-muted/50"
                  >
                    <span
                      className={cn(
                        "grid size-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold tabular-nums",
                        index === 0 ? "bg-amber-500/15 text-amber-700 dark:text-amber-300" : "bg-muted text-muted-foreground"
                      )}
                    >
                      {index + 1}
                    </span>
                    <CircleUser className="size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate text-xs font-medium">{item.account || `UID-${item.userId}`}</span>
                    <span className="text-[11px] text-muted-foreground">{formatRelative(item.lastAt)}</span>
                    <Badge variant="warning" size="sm" className="tabular-nums">
                      {formatCount(item.count)} 次
                    </Badge>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Block>
      </div>
    </div>
  );
}

/** 策略摘要条：一眼看到开没开、默认几台、各类别、超限怎么办、有几档会员规则 */
function PolicySummary({
  policy,
  configured,
  loading,
  onOpenPolicy,
  aside
}: {
  policy?: DeviceLimitPolicy;
  configured: boolean;
  loading: boolean;
  onOpenPolicy: () => void;
  aside: React.ReactNode;
}) {
  if (loading || !policy) {
    return <Skeleton className="h-[116px] w-full rounded-2xl" />;
  }
  const overflow = OVERFLOW_META[policy.overflow];
  const same = SAME_DEVICE_META[policy.sameDevice];
  const OverflowIcon = overflow.icon;
  const SameIcon = same.icon;
  const classes = DEVICE_CLASSES.filter((cls) => (policy.classLimits?.[cls] ?? 0) > 0);

  return (
    <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3">
        <div className="flex items-center gap-3">
          <StatusDot active={policy.enabled} labelActive="设备数控制已开启" labelInactive="设备数控制未开启" />
          {!configured && policy.enabled ? (
            <Badge variant="outline" size="sm">沿用旧的同时在线上限</Badge>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          {aside}
          <Button size="sm" variant="outline" className="h-7" onClick={onOpenPolicy}>
            <SlidersHorizontal className="size-3.5" /> 编辑策略
          </Button>
        </div>
      </div>
      <div className={cn("grid gap-px bg-border sm:grid-cols-2 xl:grid-cols-4", !policy.enabled && "opacity-60")}>
        <SummaryCell label="默认上限">
          <div className="flex items-center gap-2.5">
            <span className="text-xl font-semibold tabular-nums">{limitText(policy.maxDevices)}</span>
            {policy.maxDevices > 0 ? <DeviceSlots limit={policy.maxDevices} used={policy.maxDevices} size="sm" /> : null}
          </div>
        </SummaryCell>
        <SummaryCell label="终端类别">
          {classes.length ? (
            <div className="flex flex-wrap gap-1.5">
              {classes.map((cls) => (
                <span key={cls} className={cn("inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs ring-1 ring-inset", CLASS_META[cls].tint)}>
                  <ClassIcon cls={cls} className="size-3" />
                  {CLASS_META[cls].label} {policy.classLimits[cls]}
                </span>
              ))}
            </div>
          ) : (
            <span className="text-sm text-muted-foreground">不单独限制</span>
          )}
        </SummaryCell>
        <SummaryCell label="超限处置">
          <div className="flex items-center gap-1.5 text-sm font-medium">
            <OverflowIcon className="size-4 text-muted-foreground" />
            {overflow.short}
          </div>
          <div className="mt-1 flex items-center gap-1 text-[11px] text-muted-foreground">
            <SameIcon className="size-3" />
            同一设备{same.title}
          </div>
        </SummaryCell>
        <SummaryCell label="附加规则">
          <div className="flex flex-wrap gap-1.5">
            {policy.tiers.length ? (
              policy.tiers.slice(0, 3).map((tier) => {
                const Icon = AUDIENCE_META[tier.audience]?.icon ?? Layers;
                return (
                  <Badge key={tier.id} variant="info" size="sm" className="gap-1 font-normal">
                    <Icon className="size-3" />
                    {tier.name} {limitText(tier.maxDevices)}
                  </Badge>
                );
              })
            ) : (
              <span className="text-sm text-muted-foreground">无会员档位</span>
            )}
            {policy.tiers.length > 3 ? <Badge variant="outline" size="sm">+{policy.tiers.length - 3}</Badge> : null}
            {policy.newDevice.enabled ? (
              <Badge variant="outline" size="sm" className="font-normal">
                {policy.newDevice.windowDays} 天内新设备 {policy.newDevice.maxNew} 台
              </Badge>
            ) : null}
          </div>
        </SummaryCell>
      </div>
    </section>
  );
}

function SummaryCell({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="bg-card px-5 py-3.5">
      <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{label}</div>
      {children}
    </div>
  );
}
