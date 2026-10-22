"use client";

import { useMemo, useState } from "react";
import {
  Activity, AlertTriangle, Ban, CheckCircle2, ChevronRight, CircleSlash, Clock, Cpu, Eye, Gauge, Layers,
  ShieldAlert, ShieldCheck, Timer, TrendingUp, Users,
} from "lucide-react";
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, Label, Line, LineChart,
  Pie, PieChart, PolarAngleAxis, PolarGrid, Radar, RadarChart, XAxis, YAxis,
} from "recharts";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { useRiskDashboardQuery } from "@/lib/risk-hooks";
import type { RiskDashboard, RiskSeriesPoint } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { useRiskNav } from "./risk-nav";
import { ScopeBadge, useRiskScope } from "./risk-scope";
import {
  ChartSkeleton, DeltaHint, InlineEmpty, LIST_KIND_COLORS, LevelBadge, SegmentedControl,
  actionChartConfig, decisionChartConfig, dimensionChartConfig, ellipsisMiddle, fmtBucket, fmtNumber,
  fmtPercent, fmtRelative, levelChartConfig, useRiskCatalog,
} from "./risk-shared";

/**
 * 风控大盘。
 *
 * 三段：引擎状态（现在在跑什么）、概要指标（带环比）、图表（趋势 / 分布 / 榜单）。
 * 引擎状态放最上面是因为「大盘全是 0」有两种截然不同的原因：真没风险，
 * 或者根本没规则在跑。不先把这件事说清楚，后面的图全是误导。
 */

const RANGES = [
  { value: "24h", label: "近 24 小时", hours: 24 },
  { value: "7d", label: "近 7 天", hours: 24 * 7 },
  { value: "30d", label: "近 30 天", hours: 24 * 30 },
  { value: "90d", label: "近 90 天", hours: 24 * 90 },
] as const;

function rangeFor(key: string) {
  const hours = RANGES.find((r) => r.value === key)?.hours ?? 24 * 7;
  // 对齐到分钟，同一分钟内切回来命中同一份缓存
  const alignedEnd = Math.floor(Date.now() / 60000) * 60000;
  return {
    start: new Date(alignedEnd - hours * 3600 * 1000).toISOString(),
    end: new Date(alignedEnd).toISOString(),
  };
}

export function RiskDashboardPanel() {
  const nav = useRiskNav();
  const { appId } = useRiskScope();
  const [rangeKey, setRangeKey] = useState<string>("7d");
  // 区间只在切换时计算一次：在渲染里读 Date.now() 会让每次渲染都得到新的 ISO 字符串，
  // query key 每次都变，于是永远在重新请求。
  const [{ start, end }, setRange] = useState(() => rangeFor("7d"));
  const changeRange = (next: string) => {
    setRangeKey(next);
    setRange(rangeFor(next));
  };

  const query = useRiskDashboardQuery(start, end, appId);
  const data = query.data;
  const loading = query.isLoading;

  return (
    <div className="space-y-4">
      <EngineStatusCard data={data} rangeKey={rangeKey} onRangeChange={changeRange} />

      <SummaryCards data={data} loading={loading} />

      <TrendCard data={data} loading={loading} />

      <div className="grid gap-4 lg:grid-cols-3">
        <DimensionCard data={data} loading={loading} />
        <DecisionCard data={data} loading={loading} />
        <LevelDonutCard data={data} loading={loading} />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <ActionBarCard data={data} loading={loading} />
        <ScoreHistogramCard data={data} loading={loading} />
        <ListStatsCard data={data} loading={loading} />
      </div>

      <SceneMatrixCard data={data} loading={loading} />

      <div className="grid gap-4 lg:grid-cols-2">
        <TopRulesCard data={data} loading={loading} onInspect={nav.openRule} />
        <TopAccountsCard data={data} loading={loading} />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <TopIPsCard data={data} loading={loading} onInspect={(ip) => nav.openEntity({ kind: "ip", value: ip })} />
        <TopDevicesCard data={data} loading={loading}
          onInspect={(deviceId) => nav.openEntity({ kind: "device", value: deviceId })} />
      </div>
    </div>
  );
}

/* ── 引擎状态 ── */

function EngineStatusCard({ data, rangeKey, onRangeChange }: {
  data?: RiskDashboard; rangeKey: string; onRangeChange: (value: string) => void;
}) {
  const { sceneLabel } = useRiskCatalog();
  const nav = useRiskNav();
  const engine = data?.engine;
  const uncovered = engine?.scenesUncovered ?? [];
  const noRules = engine ? engine.activeRules === 0 : false;
  const noActions = engine ? engine.activeActions === 0 : false;

  const readiness = engine ? [
    { label: engine.ipProviderReady ? `情报 ${engine.ipProvider}` : "情报源", ok: engine.ipProviderReady },
    { label: "GeoIP", ok: Boolean(engine.geoReady) },
    { label: "Redis", ok: Boolean(engine.redisReady) },
    { label: "频率限流", ok: engine.rateLimitOn },
    { label: "账号封禁", ok: Boolean(engine.accountBanReady) },
    { label: "防火墙", ok: Boolean(engine.firewallReady) },
    { label: "验证码", ok: Boolean(engine.captchaReady) },
  ] : [];

  return (
    <Card>
      <CardContent className="space-y-2.5 p-3">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
          <span className="flex items-center gap-1.5 text-xs font-medium">
            <Cpu className="size-3.5 text-muted-foreground" />引擎
          </span>
          {engine ? (
            <>
              <Metric label="生效规则" value={`${engine.activeRules - (engine.observeRules ?? 0)} / ${engine.totalRules}`} alert={noRules} />
              <Metric label="观察规则" value={String(engine.observeRules ?? 0)} />
              <Metric label="启用策略" value={`${engine.activeActions} / ${engine.totalActions}`} alert={noActions} />
              <Metric label="名单" value={fmtNumber(engine.listEntries ?? 0)} />
              {engine.snapshotAt && <Metric label="快照" value={fmtRelative(engine.snapshotAt)} />}
              {uncovered.length > 0 && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Badge variant="warning" size="sm" className="cursor-help gap-1">
                      <AlertTriangle className="size-3" />
                      {uncovered.length} 个场景无规则
                    </Badge>
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    <p className="text-xs">{uncovered.map(sceneLabel).join("、")}</p>
                  </TooltipContent>
                </Tooltip>
              )}
            </>
          ) : (
            <span className="text-xs text-muted-foreground">读取中</span>
          )}
          <Select value={rangeKey} onValueChange={onRangeChange}>
            <SelectTrigger className="ml-auto h-7 w-28 shrink-0 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {RANGES.map((r) => <SelectItem key={r.value} value={r.value}>{r.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>

        {engine && (
          <div className="flex flex-wrap items-center gap-1.5">
            {readiness.map((item) => (
              <Badge key={item.label} variant={item.ok ? "outline" : "warning"} size="sm" className="gap-1">
                {item.ok
                  ? <CheckCircle2 className="size-3 text-emerald-500" />
                  : <CircleSlash className="size-3" />}
                {item.label}
              </Badge>
            ))}
          </div>
        )}

        {(noRules || noActions) && (
          <div className="flex flex-wrap items-center gap-2 rounded-lg border border-rose-300/70 bg-rose-50/60 px-3 py-2 text-xs dark:border-rose-900/60 dark:bg-rose-950/30">
            <AlertTriangle className="size-3.5 text-rose-500" />
            <span className="text-rose-700 dark:text-rose-300">
              {noRules ? "无生效规则，所有评估都按 0 分放行" : "无启用策略，所有评估都按放行处理"}
            </span>
            <Button size="sm" variant="outline" className="ml-auto h-7 text-xs" onClick={nav.openTemplates}>
              <Layers className="size-3.5" />安装规则模板
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Metric({ label, value, alert }: { label: string; value: string; alert?: boolean }) {
  return (
    <span className="flex items-baseline gap-1.5 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className={cn("font-mono font-medium tabular-nums",
        alert && "text-rose-600 dark:text-rose-400")}>{value}</span>
    </span>
  );
}

/* ── 概要指标 ── */

function SummaryCards({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const s = data?.summary;
  if (loading && !s) {
    return (
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {Array.from({ length: 8 }, (_, i) => <ChartSkeleton key={i} height={80} />)}
      </div>
    );
  }
  if (!s) return null;

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <StatCard label="总评估" icon={Activity} value={fmtNumber(s.totalAssessments)}
        footer={<DeltaHint current={s.totalAssessments} previous={s.prevTotalAssessments} />} />
      <StatCard label="已拦截" icon={ShieldAlert} tone="danger" value={fmtNumber(s.totalBlocked)}
        footer={<DeltaHint current={s.totalBlocked} previous={s.prevTotalBlocked} invert />} />
      <StatCard label="拦截率" icon={Gauge} value={fmtPercent(s.blockRate)}
        footer={<DeltaHint current={s.blockRate} previous={s.prevBlockRate} invert />} />
      <StatCard label="待复核" icon={Clock} tone={s.pendingReviews > 0 ? "warning" : undefined}
        value={fmtNumber(s.pendingReviews)}
        footer={<Sub>累计复核 {fmtNumber(s.totalReviews)}</Sub>} />
      <StatCard label="平均分" icon={TrendingUp} value={s.avgScore.toFixed(1)}
        footer={<DeltaHint current={s.avgScore} previous={s.prevAvgScore} invert />} />
      <StatCard label="高危及以上" icon={AlertTriangle} tone="danger" value={fmtNumber(s.highRiskCount)}
        footer={<Sub>最高 {s.maxScore} 分</Sub>} />
      <StatCard label="独立 IP" icon={Users} value={fmtNumber(s.distinctIps)}
        footer={<Sub>设备 {fmtNumber(s.distinctDevices)}，账号 {fmtNumber(s.distinctAccounts)}</Sub>} />
      <StatCard label="判定耗时" icon={Timer} value={`${s.avgLatencyMs.toFixed(1)} ms`} />
    </div>
  );
}

function Sub({ children }: { children: React.ReactNode }) {
  return <span className="text-[10px] text-muted-foreground">{children}</span>;
}

function StatCard({ label, value, icon: Icon, tone, footer }: {
  label: string; value: string; icon: typeof Activity;
  tone?: "danger" | "warning"; footer?: React.ReactNode;
}) {
  return (
    <Card>
      <CardContent className="p-3.5">
        <div className="flex items-center gap-1.5 text-muted-foreground">
          <Icon className="size-3.5" />
          <span className="text-[10px] font-medium uppercase tracking-widest">{label}</span>
        </div>
        <div className={cn("mt-1 font-mono text-2xl font-bold tabular-nums",
          tone === "danger" && "text-rose-600 dark:text-rose-400",
          tone === "warning" && "text-amber-600 dark:text-amber-400")}>
          {value}
        </div>
        <div className="mt-0.5 min-h-4">{footer}</div>
      </CardContent>
    </Card>
  );
}

/* ── 趋势 ── */

type TrendView = "level" | "action" | "rate";

const TREND_VIEWS = [
  { key: "level", label: "风险等级" },
  { key: "action", label: "处置动作" },
  { key: "rate", label: "拦截率与均分" },
] as const;

function TrendCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { levels, actions } = useRiskCatalog();
  const [view, setView] = useState<TrendView>("level");

  const series = useMemo(() => data?.series ?? [], [data?.series]);
  const bucket = data?.range.bucket ?? "hour";

  const chartData = useMemo(
    () => series.map((point: RiskSeriesPoint) => ({
      ...point,
      label: fmtBucket(point.time, bucket),
      blockRatePct: Number((point.blockRate * 100).toFixed(2)),
    })),
    [series, bucket],
  );

  const config = useMemo<ChartConfig>(() => {
    if (view === "action") return actionChartConfig(actions);
    if (view === "rate") {
      return {
        blockRatePct: { label: "拦截率 %", theme: { light: "#f43f5e", dark: "#fb7185" } },
        avgScore: { label: "平均分", theme: { light: "#6366f1", dark: "#818cf8" } },
      };
    }
    return levelChartConfig(levels);
  }, [view, levels, actions]);

  const stackKeys = view === "action" ? actions.map((a) => a.value) : levels.map((l) => l.value);
  const hasData = chartData.some((point) => point.total > 0);

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold">评估趋势</h3>
            <Badge variant="outline" size="sm">{bucket === "day" ? "按天" : "按小时"}</Badge>
          </div>
          <SegmentedControl value={view} options={TREND_VIEWS} onChange={setView} />
        </div>

        {loading && chartData.length === 0 ? <ChartSkeleton height={260} />
          : !hasData ? <InlineEmpty text="暂无评估记录" height={260} />
          : (
            <ChartContainer config={config} className="h-[260px] w-full">
              {view === "rate" ? (
                <LineChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                  <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={40} fontSize={10} />
                  <YAxis tickLine={false} axisLine={false} width={40} fontSize={10} />
                  <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
                  <ChartLegend content={<ChartLegendContent />} />
                  <Line dataKey="blockRatePct" type="monotone" stroke="var(--color-blockRatePct)" strokeWidth={2} dot={false} />
                  <Line dataKey="avgScore" type="monotone" stroke="var(--color-avgScore)" strokeWidth={2} dot={false} />
                </LineChart>
              ) : (
                <AreaChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                  <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={40} fontSize={10} />
                  <YAxis tickLine={false} axisLine={false} width={40} allowDecimals={false} fontSize={10} />
                  <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
                  <ChartLegend content={<ChartLegendContent />} />
                  {stackKeys.map((key) => (
                    <Area key={key} dataKey={key} type="monotone" stackId="risk"
                      stroke={`var(--color-${key})`} fill={`var(--color-${key})`} fillOpacity={0.35} strokeWidth={1.5} />
                  ))}
                </AreaChart>
              )}
            </ChartContainer>
          )}
      </CardContent>
    </Card>
  );
}

/* ── 分布 ── */

function CardShell({ title, extra, children }: { title: string; extra?: React.ReactNode; children: React.ReactNode }) {
  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-sm font-semibold">{title}</h3>
          {extra}
        </div>
        {children}
      </CardContent>
    </Card>
  );
}

function LevelDonutCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { levels } = useRiskCatalog();
  const config = useMemo(() => levelChartConfig(levels), [levels]);
  const items = (data?.levelDistribution ?? []).filter((l) => l.count > 0);
  const total = items.reduce((sum, l) => sum + l.count, 0);
  const highShare = total > 0
    ? items.filter((i) => i.level === "high" || i.level === "critical").reduce((s, i) => s + i.count, 0) / total
    : 0;

  return (
    <CardShell title="风险等级分布">
      {loading && items.length === 0 ? <ChartSkeleton height={220} />
        : total === 0 ? <InlineEmpty text="暂无评估记录" height={220} />
        : (
          <>
            <ChartContainer config={config} className="mx-auto h-[220px] w-full">
              <PieChart>
                <ChartTooltip content={<ChartTooltipContent nameKey="level" hideLabel />} />
                <Pie data={items} dataKey="count" nameKey="level" innerRadius={55} outerRadius={85}
                  strokeWidth={2} paddingAngle={2}>
                  {items.map((item) => <Cell key={item.level} fill={`var(--color-${item.level})`} />)}
                  <Label content={({ viewBox }) => {
                    if (!viewBox || !("cx" in viewBox)) return null;
                    return (
                      <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                        <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-xl font-bold">
                          {fmtNumber(total)}
                        </tspan>
                        <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-[10px]">
                          次评估
                        </tspan>
                      </text>
                    );
                  }} />
                </Pie>
                <ChartLegend content={<ChartLegendContent nameKey="level" className="flex-wrap gap-x-3 gap-y-1" />} />
              </PieChart>
            </ChartContainer>
            <p className="text-[10px] text-muted-foreground">高危及以上占 {fmtPercent(highShare)}</p>
          </>
        )}
    </CardShell>
  );
}

function ActionBarCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { actions, actionLabel } = useRiskCatalog();
  const config = useMemo(() => actionChartConfig(actions), [actions]);
  const items = useMemo(
    () => (data?.actionDistribution ?? []).map((a) => ({ ...a, label: actionLabel(a.action) })),
    [data?.actionDistribution, actionLabel],
  );
  const total = items.reduce((sum, a) => sum + a.count, 0);

  return (
    <CardShell title="处置动作分布">
      {loading && items.length === 0 ? <ChartSkeleton height={220} />
        : total === 0 ? <InlineEmpty text="暂无评估记录" height={220} />
        : (
          <ChartContainer config={config} className="h-[220px] w-full">
            <BarChart data={items} layout="vertical" margin={{ top: 4, right: 24, bottom: 4, left: 4 }}>
              <CartesianGrid horizontal={false} strokeDasharray="3 3" className="stroke-border/40" />
              <XAxis type="number" hide />
              <YAxis type="category" dataKey="label" tickLine={false} axisLine={false} width={56} fontSize={10} />
              <ChartTooltip content={<ChartTooltipContent nameKey="action" hideLabel />} />
              <Bar dataKey="count" radius={[0, 4, 4, 0]} barSize={18}>
                {items.map((item) => <Cell key={item.action} fill={`var(--color-${item.action})`} />)}
              </Bar>
            </BarChart>
          </ChartContainer>
        )}
    </CardShell>
  );
}

function ScoreHistogramCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { levels } = useRiskCatalog();
  const config = useMemo(() => levelChartConfig(levels), [levels]);
  const items = useMemo(
    // 桶边界与等级分档一致。用独立的分桶宽度会让直方图的峰和饼图的占比对不上。
    () => (data?.scoreHistogram ?? []).map((bucket) => ({
      ...bucket,
      label: bucket.max > 1000 ? `${bucket.min}+` : `${bucket.min}-${bucket.max}`,
    })),
    [data?.scoreHistogram],
  );
  const total = items.reduce((sum, b) => sum + b.count, 0);

  return (
    <CardShell title="分数分布">
      {loading && items.length === 0 ? <ChartSkeleton height={220} />
        : total === 0 ? <InlineEmpty text="暂无评估记录" height={220} />
        : (
          <ChartContainer config={config} className="h-[220px] w-full">
            <BarChart data={items} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} fontSize={10} />
              <YAxis tickLine={false} axisLine={false} width={40} allowDecimals={false} fontSize={10} />
              <ChartTooltip content={<ChartTooltipContent nameKey="level" hideLabel />} />
              <Bar dataKey="count" radius={[4, 4, 0, 0]}>
                {items.map((item) => <Cell key={item.level} fill={`var(--color-${item.level})`} />)}
              </Bar>
            </BarChart>
          </ChartContainer>
        )}
    </CardShell>
  );
}

/** 维度贡献：每个维度命中多少次、贡献多少分。雷达看形状，柱看量。 */
function DimensionCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { dimensions, dimensionLabel } = useRiskCatalog();
  const [view, setView] = useState<"score" | "radar">("score");
  const config = useMemo<ChartConfig>(() => ({
    ...dimensionChartConfig(dimensions),
    score: { label: "贡献分", theme: { light: "#6366f1", dark: "#818cf8" } },
    hits: { label: "命中次数", theme: { light: "#f59e0b", dark: "#fbbf24" } },
  }), [dimensions]);
  const items = useMemo(() => {
    const byDim = new Map((data?.dimensions ?? []).map((d) => [d.dimension, d]));
    return dimensions.map((dim) => ({
      dimension: dim.value,
      label: dimensionLabel(dim.value),
      score: byDim.get(dim.value)?.score ?? 0,
      hits: byDim.get(dim.value)?.hits ?? 0,
      requests: byDim.get(dim.value)?.requests ?? 0,
    }));
  }, [data?.dimensions, dimensions, dimensionLabel]);
  const total = items.reduce((sum, d) => sum + d.hits, 0);

  return (
    <CardShell title="维度贡献" extra={(
      <SegmentedControl value={view} onChange={setView}
        options={[{ key: "score", label: "柱" }, { key: "radar", label: "雷达" }]} />
    )}>
      {loading && !data ? <ChartSkeleton height={220} />
        : total === 0 ? <InlineEmpty text="暂无规则命中" height={220} />
        : view === "score" ? (
          <ChartContainer config={config} className="h-[220px] w-full">
            <BarChart data={items} layout="vertical" margin={{ top: 4, right: 24, bottom: 4, left: 4 }}>
              <CartesianGrid horizontal={false} strokeDasharray="3 3" className="stroke-border/40" />
              <XAxis type="number" hide />
              <YAxis type="category" dataKey="label" tickLine={false} axisLine={false} width={48} fontSize={10} />
              <ChartTooltip content={<ChartTooltipContent hideLabel />} />
              <Bar dataKey="score" radius={[0, 4, 4, 0]} barSize={14}>
                {items.map((item) => <Cell key={item.dimension} fill={`var(--color-${item.dimension})`} />)}
              </Bar>
            </BarChart>
          </ChartContainer>
        ) : (
          <ChartContainer config={config} className="mx-auto h-[220px] w-full">
            <RadarChart data={items} outerRadius={78}>
              <ChartTooltip content={<ChartTooltipContent />} />
              <PolarGrid className="stroke-border/60" />
              <PolarAngleAxis dataKey="label" fontSize={10} />
              <Radar dataKey="hits" stroke="var(--color-hits)" fill="var(--color-hits)" fillOpacity={0.35} />
            </RadarChart>
          </ChartContainer>
        )}
    </CardShell>
  );
}

/** 判定来源：名单直接定的、规则强制的、分数策略给的、无干预 */
function DecisionCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { decisionBy } = useRiskCatalog();
  const config = useMemo(() => decisionChartConfig(decisionBy), [decisionBy]);
  const items = (data?.decisionSources ?? []).filter((d) => d.count > 0);
  const total = items.reduce((sum, d) => sum + d.count, 0);
  const intervened = items.filter((d) => d.decisionBy !== "none").reduce((sum, d) => sum + d.count, 0);

  return (
    <CardShell title="判定来源">
      {loading && !data ? <ChartSkeleton height={220} />
        : total === 0 ? <InlineEmpty text="暂无评估记录" height={220} />
        : (
          <ChartContainer config={config} className="mx-auto h-[220px] w-full">
            <PieChart>
              <ChartTooltip content={<ChartTooltipContent nameKey="decisionBy" hideLabel />} />
              <Pie data={items} dataKey="count" nameKey="decisionBy" innerRadius={55} outerRadius={85}
                strokeWidth={2} paddingAngle={2}>
                {items.map((item) => <Cell key={item.decisionBy} fill={`var(--color-${item.decisionBy})`} />)}
                <Label content={({ viewBox }) => {
                  if (!viewBox || !("cx" in viewBox)) return null;
                  return (
                    <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                      <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-xl font-bold">
                        {fmtPercent(total > 0 ? intervened / total : 0, 0)}
                      </tspan>
                      <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-[10px]">
                        有干预
                      </tspan>
                    </text>
                  );
                }} />
              </Pie>
              <ChartLegend content={<ChartLegendContent nameKey="decisionBy" className="flex-wrap gap-x-3 gap-y-1" />} />
            </PieChart>
          </ChartContainer>
        )}
    </CardShell>
  );
}

const LIST_KIND_ICONS: Record<string, typeof Ban> = { block: Ban, allow: ShieldCheck, watch: Eye };

function ListStatsCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { listKinds } = useRiskCatalog();
  const nav = useRiskNav();
  const byKind = new Map((data?.lists ?? []).map((l) => [l.kind, l]));
  const maxHits = Math.max(1, ...(data?.lists ?? []).map((l) => l.hits));

  return (
    <CardShell title="名单" extra={(
      <Button variant="ghost" size="sm" className="h-6 px-2 text-[11px]" onClick={nav.openLists}>
        名单库<ChevronRight className="size-3" />
      </Button>
    )}>
      {loading && !data ? <ChartSkeleton height={220} /> : (
        <div className="flex h-[220px] flex-col justify-center gap-3">
          {listKinds.map((kind) => {
            const stat = byKind.get(kind.value);
            const Icon = LIST_KIND_ICONS[kind.value] ?? Eye;
            return (
              <div key={kind.value} className="space-y-1.5 rounded-lg border px-3 py-2.5">
                <div className="flex items-center gap-2 text-xs">
                  <Icon className="size-3.5" style={{ color: LIST_KIND_COLORS[kind.value]?.light }} />
                  <span className="font-medium">{kind.label}</span>
                  <span className="ml-auto font-mono tabular-nums text-muted-foreground">
                    {fmtNumber(stat?.entries ?? 0)} 条
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                    <span className="block h-full rounded-full" style={{
                      width: `${((stat?.hits ?? 0) / maxHits) * 100}%`, background: LIST_KIND_COLORS[kind.value]?.light,
                    }} />
                  </span>
                  <span className="w-20 shrink-0 text-right text-[10px] tabular-nums text-muted-foreground">
                    命中 {fmtNumber(stat?.hits ?? 0)}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </CardShell>
  );
}

/** 场景矩阵：每个场景的量、验证、复核、拦截与均分并列，哪个场景在被打一眼可见 */
function SceneMatrixCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { scenes, sceneLabel } = useRiskCatalog();
  const nav = useRiskNav();
  const byScene = new Map((data?.sceneDistribution ?? []).map((s) => [s.scene, s]));
  const uncovered = new Set(data?.engine.scenesUncovered ?? []);
  const rows = scenes.map((scene) => ({ scene: scene.value, service: scene.service, stat: byScene.get(scene.value) }));
  const maxCount = Math.max(1, ...rows.map((r) => r.stat?.count ?? 0));

  return (
    <CardShell title="场景">
      {loading && !data ? <ChartSkeleton height={240} /> : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-28">场景</TableHead>
                <TableHead className="w-24">服务</TableHead>
                <TableHead>评估量</TableHead>
                <TableHead className="w-20 text-right">验证码</TableHead>
                <TableHead className="w-20 text-right">复核</TableHead>
                <TableHead className="w-20 text-right">拦截</TableHead>
                <TableHead className="w-20 text-right">拦截率</TableHead>
                <TableHead className="w-16 text-right">均分</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map(({ scene, service, stat }) => {
                const count = stat?.count ?? 0;
                return (
                  <TableRow key={scene} className="text-xs">
                    <TableCell>
                      <div className="flex items-center gap-1.5">
                        <span className="font-medium">{sceneLabel(scene)}</span>
                        {uncovered.has(scene) && (
                          <button type="button" onClick={nav.openTemplates}>
                            <Badge variant="warning" size="sm">无规则</Badge>
                          </button>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{service}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Progress value={(count / maxCount) * 100} className="h-1.5 flex-1" />
                        <span className="w-14 shrink-0 text-right font-mono tabular-nums">{fmtNumber(count)}</span>
                      </div>
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">{fmtNumber(stat?.challenged ?? 0)}</TableCell>
                    <TableCell className="text-right font-mono tabular-nums">{fmtNumber(stat?.reviewed ?? 0)}</TableCell>
                    <TableCell className={cn("text-right font-mono tabular-nums", (stat?.blocked ?? 0) > 0 && "text-rose-600 dark:text-rose-400")}>
                      {fmtNumber(stat?.blocked ?? 0)}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
                      {count > 0 ? fmtPercent((stat?.blocked ?? 0) / count) : "--"}
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">{count > 0 ? (stat?.avgScore ?? 0).toFixed(1) : "--"}</TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </CardShell>
  );
}

/* ── 榜单 ── */

function RankRow({ onClick, children }: { onClick?: () => void; children: React.ReactNode }) {
  return (
    <button type="button" onClick={onClick} disabled={!onClick}
      className={cn("flex w-full items-center gap-2 rounded-lg border px-2.5 py-1.5 text-left text-xs transition-colors",
        onClick && "hover:border-primary/40 hover:bg-accent/40")}>
      {children}
      {onClick && <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
    </button>
  );
}

function TopRulesCard({ data, loading, onInspect }: {
  data?: RiskDashboard; loading: boolean; onInspect?: (ruleId: number) => void;
}) {
  const { conditionLabel } = useRiskCatalog();
  const items = data?.topRules ?? [];
  const max = items[0]?.hits || 1;

  return (
    <CardShell title="命中最多的规则">
      {loading && items.length === 0 ? <ChartSkeleton height={200} />
        : items.length === 0 ? <InlineEmpty text="暂无命中" height={200} />
        : (
          <div className="space-y-1.5">
            {items.map((rule) => (
              <RankRow key={rule.ruleId} onClick={onInspect ? () => onInspect(rule.ruleId) : undefined}>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate font-medium">{rule.ruleName || `规则 ${rule.ruleId}`}</span>
                    <Badge variant="outline" size="sm" className="shrink-0">{conditionLabel(rule.conditionType)}</Badge>
                    {!rule.isActive && <Badge variant="warning" size="sm" className="shrink-0">已停用</Badge>}
                  </div>
                  <div className="mt-1 flex items-center gap-2">
                    <Progress value={(rule.hits / max) * 100} className="h-1.5 flex-1" />
                    <span className="w-36 shrink-0 text-right tabular-nums text-[10px] text-muted-foreground">
                      命中 {fmtNumber(rule.hits)}，拦截 {fmtNumber(rule.blocked)}，{fmtRelative(rule.lastHitAt)}
                    </span>
                  </div>
                </div>
              </RankRow>
            ))}
          </div>
        )}
    </CardShell>
  );
}

function TopIPsCard({ data, loading, onInspect }: {
  data?: RiskDashboard; loading: boolean; onInspect?: (ip: string) => void;
}) {
  const items = data?.topIps ?? [];
  return (
    <CardShell title="高频 IP">
      {loading && items.length === 0 ? <ChartSkeleton height={200} />
        : items.length === 0 ? <InlineEmpty text="暂无评估记录" height={200} />
        : (
          <div className="space-y-1">
            {items.map((item) => (
              <RankRow key={item.ip} onClick={onInspect ? () => onInspect(item.ip) : undefined}>
                <span className="w-36 shrink-0 truncate font-mono">{item.ip}</span>
                <span className="w-10 shrink-0 text-muted-foreground">{item.country || "未知"}</span>
                {item.riskTag !== "normal" && <Badge variant="danger" size="sm">{item.riskTag}</Badge>}
                <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
                  {fmtNumber(item.count)} 次，拦截 {fmtNumber(item.blocked)}，最高 {item.maxScore} 分
                </span>
              </RankRow>
            ))}
          </div>
        )}
    </CardShell>
  );
}

function TopDevicesCard({ data, loading, onInspect }: {
  data?: RiskDashboard; loading: boolean; onInspect?: (deviceId: string) => void;
}) {
  const items = data?.topDevices ?? [];
  return (
    <CardShell title="高频设备">
      {loading && items.length === 0 ? <ChartSkeleton height={200} />
        : items.length === 0 ? <InlineEmpty text="暂无设备" height={200} />
        : (
          <div className="space-y-1">
            {items.map((item) => (
              <RankRow key={item.deviceId} onClick={onInspect ? () => onInspect(item.deviceId) : undefined}>
                <span className="w-40 shrink-0 truncate font-mono" title={item.deviceId}>
                  {ellipsisMiddle(item.deviceId, 14, 8)}
                </span>
                {item.riskTag !== "normal" && <Badge variant="danger" size="sm">{item.riskTag}</Badge>}
                <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
                  {fmtNumber(item.count)} 次，{fmtNumber(item.accounts)} 个账号，最高 {item.maxScore} 分
                </span>
              </RankRow>
            ))}
          </div>
        )}
    </CardShell>
  );
}

function TopAccountsCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const nav = useRiskNav();
  const items = data?.topAccounts ?? [];
  return (
    <CardShell title="高危账号">
      {loading && items.length === 0 ? <ChartSkeleton height={200} />
        : items.length === 0 ? <InlineEmpty text="暂无账号" height={200} />
        : (
          <div className="space-y-1">
            {items.map((item) => (
              <RankRow key={`${item.appId ?? 0}-${item.account}-${item.userId ?? 0}`}
                onClick={() => nav.openEntity({ kind: "account", value: item.account, appId: item.appId, userId: item.userId })}>
                <span className="min-w-0 flex-1 truncate font-medium">{item.account || `用户 #${item.userId ?? "--"}`}</span>
                <ScopeBadge appId={item.appId} className="shrink-0" />
                {item.lastLevel && <LevelBadge level={item.lastLevel} className="shrink-0" />}
                <span className="shrink-0 tabular-nums text-muted-foreground">
                  {fmtNumber(item.totalAssessments)} 次，拦截 {fmtNumber(item.blocked)}，最高 {item.maxScore} 分
                </span>
              </RankRow>
            ))}
          </div>
        )}
    </CardShell>
  );
}
