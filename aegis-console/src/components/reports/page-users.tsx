"use client";

import { useMemo, useState } from "react";
import {
  Activity,
  BadgeDollarSign,
  CalendarCheck,
  CalendarDays,
  Coins,
  Crown,
  Filter,
  Flame,
  Gauge,
  LogIn,
  Magnet,
  Percent,
  Repeat,
  Sparkles,
  TrendingUp,
  UserCheck,
  UserPlus,
  Users,
  Wallet
} from "lucide-react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import { useReportQuery } from "@/lib/report-hooks";
import {
  Donut,
  HeatmapGrid,
  InsightList,
  KpiGrid,
  LifecyclePanel,
  RankList,
  ReportCard,
  SectionGrid,
  ShareBar,
  SummaryStrip,
  TrendChart,
  color,
  trendEmpty,
  type KpiSpec,
  type SeriesSpec
} from "./report-kit";
import { ACTIVE_SIGNAL, PROVIDER, WEEKDAY, formatValue } from "./report-labels";
import { ReportSection, type SectionProps } from "./report-section";

/* ═════════════════════════ 总览 ═════════════════════════ */

const OVERVIEW_KPIS: KpiSpec[] = [
  { key: "newUsers", label: "新增用户", icon: UserPlus, tone: "indigo" },
  { key: "activeUsers", label: "活跃用户", icon: Users, tone: "sky", hint: "期内去重" },
  { key: "avgDau", label: "日均活跃", icon: Activity, tone: "teal", format: "decimal" },
  { key: "totalUsers", label: "累计用户", icon: Crown, tone: "violet" },
  { key: "revenue", label: "收入", icon: Wallet, tone: "emerald", format: "money" },
  { key: "netRevenue", label: "净收入", icon: Coins, tone: "emerald", format: "money", hint: "扣除退款" },
  { key: "payers", label: "付费用户", icon: BadgeDollarSign, tone: "amber" },
  { key: "arppu", label: "付费用户均收入", icon: Gauge, tone: "amber", format: "money" },
  { key: "payRate", label: "付费率", icon: Percent, tone: "pink", format: "percent", hint: "付费用户占活跃" },
  { key: "d1Retention", label: "次日留存", icon: Repeat, tone: "sky", format: "percent" },
  { key: "logins", label: "登录次数", icon: LogIn, tone: "slate" },
  { key: "signins", label: "签到次数", icon: CalendarCheck, tone: "lime" }
];

const OVERVIEW_VIEWS: Record<string, { label: string; series: SeriesSpec[]; format?: "int" | "money" }> = {
  users: {
    label: "用户",
    series: [
      { key: "newUsers", label: "新增用户", type: "bar", tone: "indigo" },
      { key: "dau", label: "日均活跃", type: "line", tone: "teal" },
      { key: "active", label: "活跃用户", type: "line", tone: "sky", dashed: true }
    ]
  },
  revenue: { label: "收入", format: "money", series: [{ key: "revenue", label: "收入", type: "area", tone: "emerald" }] },
  behavior: {
    label: "行为",
    series: [
      { key: "logins", label: "登录", type: "bar", tone: "slate" },
      { key: "signins", label: "签到", type: "line", tone: "lime" }
    ]
  }
};

export function OverviewPage({ appKey, params }: SectionProps) {
  const q = useReportQuery("overview", appKey, params);
  const [view, setView] = useState("users");
  const d = q.data;
  const spec = OVERVIEW_VIEWS[view];
  return (
    <div className="space-y-4">
      <KpiGrid specs={OVERVIEW_KPIS} kpis={d?.kpis} loading={q.isLoading} />
      <ReportCard
        title="核心趋势"
        description={d ? `${d.window.from} 至 ${d.window.to}，对比上期 ${d.window.previousFrom} 至 ${d.window.previousTo}` : undefined}
        loading={q.isLoading}
        height={300}
        action={
          <ToggleGroup type="single" size="sm" value={view} onValueChange={(v) => v && setView(v)}>
            {Object.entries(OVERVIEW_VIEWS).map(([k, v]) => (
              <ToggleGroupItem key={k} value={k} className="h-7 px-2.5 text-xs">
                {v.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
      >
        <TrendChart trend={d?.trend} series={spec.series} granularity={params.granularity} format={spec.format} height={300} />
      </ReportCard>
      <SectionGrid>
        <ReportCard title="用户分层" description="与上一个等长周期相比的活跃用户构成" loading={q.isLoading}>
          <LifecyclePanel lifecycle={d?.lifecycle} />
        </ReportCard>
        <ReportCard title="自动洞察" description="按日序列检测突增、突降与整体趋势" loading={q.isLoading}>
          <InsightList insights={d?.insights} />
        </ReportCard>
      </SectionGrid>
    </div>
  );
}

/* ═════════════════════════ 用户 ═════════════════════════ */

export function UsersPage(props: SectionProps) {
  return (
    <div className="space-y-10">
      <GrowthSection {...props} />
      <ActiveSection {...props} />
      <RetentionSection {...props} />
      <FunnelSection {...props} />
    </div>
  );
}

const GROWTH_KPIS: KpiSpec[] = [
  { key: "newUsers", label: "新增用户", icon: UserPlus, tone: "indigo" },
  { key: "totalUsers", label: "累计用户", icon: Users, tone: "violet" },
  { key: "avgDaily", label: "日均新增", icon: CalendarDays, tone: "sky", format: "decimal" },
  { key: "peakDaily", label: "单日峰值", icon: Flame, tone: "amber" },
  { key: "activationRate", label: "7 日回访率", icon: UserCheck, tone: "emerald", format: "percent", hint: "注册后 7 天内再次活跃" }
];

function GrowthSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("registration", appKey, params);
  const d = q.data;
  const weekdays = useMemo(() => (d?.weekdays ?? []).map((w) => ({ ...w, label: WEEKDAY[Number(w.key) - 1] })), [d]);
  return (
    <ReportSection id="growth" title="用户增长" description="注册量、来源与注册时段" icon={UserPlus}>
      <KpiGrid specs={GROWTH_KPIS} kpis={d?.kpis} loading={q.isLoading} columns={5} />
      <ReportCard
        title="注册趋势"
        description="虚线为上期同位置的注册量，右轴为累计用户"
        loading={q.isLoading}
        empty={trendEmpty(d?.trend, ["newUsers", "cumulative"])}
        height={280}
      >
        <TrendChart
          trend={d?.trend}
          granularity={params.granularity}
          rightFormat="int"
          height={280}
          series={[
            { key: "newUsers", label: "新增用户", type: "bar", tone: "indigo" },
            { key: "activated", label: "其中 7 日内回访", type: "bar", tone: "teal" },
            { key: "previous", label: "上期新增", type: "line", tone: "slate", dashed: true },
            { key: "cumulative", label: "累计用户", type: "line", tone: "violet", right: true }
          ]}
        />
      </ReportCard>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="注册来源" loading={q.isLoading}>
          <Donut items={d?.sources} dict={PROVIDER} centerLabel="新增" />
        </ReportCard>
        <ReportCard title="注册省份" description="按注册时的 IP 归属" loading={q.isLoading}>
          <RankList items={d?.provinces} limit={7} />
        </ReportCard>
        <ReportCard title="注册星期分布" loading={q.isLoading}>
          <RankList items={weekdays} ordinal={false} limit={7} tone="sky" />
        </ReportCard>
      </div>
      <SectionGrid>
        <ReportCard title="注册时段" description="按浏览器时区的星期与小时" loading={q.isLoading}>
          <HeatmapGrid heatmap={d?.heatmap} unit="人" />
        </ReportCard>
        <ReportCard title="注册异常" loading={q.isLoading}>
          <InsightList insights={d?.insights} />
        </ReportCard>
      </SectionGrid>
    </ReportSection>
  );
}

const ACTIVE_KPIS: KpiSpec[] = [
  { key: "activeUsers", label: "活跃用户", icon: Users, tone: "sky" },
  { key: "avgDau", label: "日均活跃", icon: Activity, tone: "teal", format: "decimal" },
  { key: "peakDau", label: "日活峰值", icon: Flame, tone: "amber" },
  { key: "wau", label: "周活跃", icon: CalendarDays, tone: "indigo", hint: "截至最后一天的 7 日" },
  { key: "mau", label: "月活跃", icon: CalendarDays, tone: "violet", hint: "截至最后一天的 30 日" },
  { key: "stickiness", label: "粘性", icon: Magnet, tone: "pink", format: "percent", hint: "日活与月活之比" }
];

function ActiveSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("active", appKey, params);
  const d = q.data;
  return (
    <ReportSection id="active" title="活跃" description="登录、会话活动与签到任一发生即计为当天活跃" icon={Activity} tone="teal">
      <KpiGrid specs={ACTIVE_KPIS} kpis={d?.kpis} loading={q.isLoading} columns={6} />
      <SectionGrid>
        <ReportCard title="日活、周活与月活" loading={q.isLoading} empty={trendEmpty(d?.trend, ["dau"])} height={260}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={260}
            series={[
              { key: "mau", label: "月活跃", type: "area", tone: "violet" },
              { key: "wau", label: "周活跃", type: "area", tone: "indigo" },
              { key: "dau", label: "日活跃", type: "area", tone: "teal" }
            ]}
          />
        </ReportCard>
        <ReportCard title="新老用户构成" description="柱为新增与回访活跃，线为粘性（右轴）" loading={q.isLoading} empty={trendEmpty(d?.trend, ["dau"])} height={260}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={260}
            format="decimal"
            series={[
              { key: "new", label: "新用户", type: "bar", stack: "a", tone: "emerald" },
              { key: "returning", label: "老用户", type: "bar", stack: "a", tone: "sky" },
              { key: "stickiness", label: "粘性", type: "line", tone: "pink", right: true }
            ]}
          />
        </ReportCard>
      </SectionGrid>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="用户分层" className="lg:col-span-2" loading={q.isLoading}>
          <LifecyclePanel lifecycle={d?.lifecycle} />
        </ReportCard>
        <ReportCard title="活跃天数分布" description="期内每个活跃用户出现的天数" loading={q.isLoading}>
          <RankList items={d?.frequency} ordinal={false} labelOf={(it) => `${it.key} 天`} tone="teal" />
          <div className="mt-3 border-t pt-3">
            <div className="mb-2 text-xs text-muted-foreground">活跃信号覆盖</div>
            <ShareBar items={d?.signals} dict={ACTIVE_SIGNAL} />
          </div>
        </ReportCard>
      </div>
      <ReportCard title="活跃时段" description="登录与会话事件的星期与小时分布" loading={q.isLoading}>
        <HeatmapGrid heatmap={d?.heatmap} tone="teal" />
      </ReportCard>
    </ReportSection>
  );
}

function RetentionSection({ appKey, params }: SectionProps) {
  const [unit, setUnit] = useState<"day" | "week">("day");
  const [mode, setMode] = useState<"exact" | "rolling">("exact");
  const q = useReportQuery("retention", appKey, { ...params, unit, mode });
  const d = q.data;
  const unitLabel = unit === "day" ? "天" : "周";
  const curve = useMemo(
    () => ({
      buckets: (d?.offsets ?? []).map((o) => `第 ${o} ${unitLabel}`),
      series: { rate: (d?.average ?? []).map((v) => v ?? 0) }
    }),
    [d, unitLabel]
  );
  const rows = (d?.rows ?? []).filter((r) => r.size > 0);
  return (
    <ReportSection
      id="retention"
      title="留存"
      description="按注册日期分队列；还没到观察日的格子留空"
      icon={Repeat}
      tone="sky"
      action={
        <div className="flex flex-wrap gap-2">
          <ToggleGroup type="single" size="sm" variant="outline" value={unit} onValueChange={(v) => v && setUnit(v as "day" | "week")}>
            <ToggleGroupItem value="day" className="h-7 px-2.5 text-xs">按日</ToggleGroupItem>
            <ToggleGroupItem value="week" className="h-7 px-2.5 text-xs">按周</ToggleGroupItem>
          </ToggleGroup>
          <ToggleGroup type="single" size="sm" variant="outline" value={mode} onValueChange={(v) => v && setMode(v as "exact" | "rolling")}>
            <ToggleGroupItem value="exact" className="h-7 px-2.5 text-xs">当期回访</ToggleGroupItem>
            <ToggleGroupItem value="rolling" className="h-7 px-2.5 text-xs">此后回访</ToggleGroupItem>
          </ToggleGroup>
        </div>
      }
    >
      <KpiGrid
        loading={q.isLoading}
        kpis={d?.kpis}
        specs={[
          { key: "cohortUsers", label: "队列用户", icon: Users, tone: "indigo" },
          { key: "d1", label: "次日留存", icon: Repeat, tone: "sky", format: "percent" },
          { key: "d7", label: "7 日留存", icon: Repeat, tone: "teal", format: "percent" },
          { key: "d30", label: "30 日留存", icon: Repeat, tone: "violet", format: "percent" }
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-3">
        <ReportCard title="平均留存曲线" description="各队列按人数加权" loading={q.isLoading} height={260}>
          <TrendChart trend={curve} granularity="month" format="percent" height={260} series={[{ key: "rate", label: "留存率", type: "area", tone: "sky" }]} />
        </ReportCard>
        <ReportCard title="留存矩阵" className="xl:col-span-2" loading={q.isLoading} empty={rows.length === 0} height={260}>
          <div className="max-h-[420px] overflow-auto rounded-xl border">
            <table className="w-full text-xs">
              <thead className="sticky top-0 z-10 bg-card">
                <tr className="border-b text-muted-foreground">
                  <th className="px-3 py-2 text-left font-medium">队列</th>
                  <th className="px-3 py-2 text-right font-medium">人数</th>
                  {d?.offsets.map((o) => (
                    <th key={o} className="px-1.5 py-2 text-center font-medium whitespace-nowrap">
                      {unit === "day" ? `D${o}` : `W${o}`}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                <tr className="border-b bg-muted/40 font-medium">
                  <td className="px-3 py-1.5">加权平均</td>
                  <td className="px-3 py-1.5 text-right font-mono tabular-nums">{formatValue(rows.reduce((s, r) => s + r.size, 0))}</td>
                  {d?.average.map((v, i) => <RetentionCell key={i} rate={v} />)}
                </tr>
                {rows.map((row) => (
                  <tr key={row.cohort} className="border-b last:border-0">
                    <td className="px-3 py-1.5 font-mono whitespace-nowrap tabular-nums">{row.cohort}</td>
                    <td className="px-3 py-1.5 text-right font-mono tabular-nums">{formatValue(row.size)}</td>
                    {row.rates.map((v, i) => <RetentionCell key={i} rate={v} count={row.retained[i]} />)}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </ReportCard>
      </div>
    </ReportSection>
  );
}

function RetentionCell({ rate, count }: { rate: number | null; count?: number | null }) {
  if (rate === null || rate === undefined) {
    return <td className="px-1 py-1 text-center text-muted-foreground/50">—</td>;
  }
  return (
    <td className="px-1 py-1">
      <div
        title={count !== undefined && count !== null ? `${count} 人` : undefined}
        className={cn("rounded-md px-1.5 py-1 text-center font-mono tabular-nums", rate > 0.45 && "text-white")}
        style={{ backgroundColor: `color-mix(in oklab, ${color("sky")} ${Math.max(rate, 0.03) * 100}%, transparent)` }}
      >
        {(rate * 100).toFixed(1)}%
      </div>
    </td>
  );
}

const FUNNEL_STEPS: Record<string, { label: string; hint: string }> = {
  registered: { label: "注册", hint: "期内注册的用户" },
  returned: { label: "回访", hint: "注册日之后再次活跃" },
  active3: { label: "活跃 3 天以上", hint: "累计活跃至少 3 天" },
  paid: { label: "付费", hint: "完成过至少一笔支付" }
};

function FunnelSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("funnel", appKey, params);
  const d = q.data;
  const first = d?.steps[0]?.users || 0;
  return (
    <ReportSection id="funnel" title="转化漏斗" description="期内注册的用户截至今天的转化，每一步都是上一步的子集" icon={Filter} tone="amber">
      <KpiGrid
        loading={q.isLoading}
        kpis={d?.kpis}
        specs={[
          { key: "registered", label: "注册用户", icon: UserPlus, tone: "indigo" },
          { key: "returnRate", label: "回访率", icon: Repeat, tone: "sky", format: "percent" },
          { key: "interactRate", label: "参与互动率", icon: Sparkles, tone: "pink", format: "percent", hint: "签到、抽奖、激励广告或兑换" },
          { key: "payRate", label: "付费转化率", icon: TrendingUp, tone: "emerald", format: "percent" }
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-5">
        <ReportCard title="转化路径" className="xl:col-span-2" loading={q.isLoading} empty={first === 0} height={260}>
          <div className="space-y-2">
            {d?.steps.map((step, i) => (
              <div key={step.key}>
                {i > 0 ? (
                  <div className="flex items-center gap-2 py-1 pl-3 text-[11px] text-muted-foreground">
                    <span className="h-3 w-px bg-border" />
                    转化 <span className="font-mono tabular-nums text-foreground">{(step.stepRate * 100).toFixed(1)}%</span>
                  </div>
                ) : null}
                <div className="relative overflow-hidden rounded-xl border px-3 py-2.5">
                  <div
                    className="absolute inset-y-0 left-0"
                    style={{ width: `${Math.max(step.overallRate * 100, 1)}%`, backgroundColor: `color-mix(in oklab, ${color(i)} 16%, transparent)` }}
                  />
                  <div className="relative flex items-center justify-between gap-3 text-xs">
                    <div>
                      <div className="font-medium">{FUNNEL_STEPS[step.key]?.label ?? step.key}</div>
                      <div className="text-[11px] text-muted-foreground">{FUNNEL_STEPS[step.key]?.hint}</div>
                    </div>
                    <div className="text-right">
                      <div className="font-mono text-sm font-semibold tabular-nums">{formatValue(step.users)}</div>
                      <div className="font-mono text-[11px] text-muted-foreground tabular-nums">{(step.overallRate * 100).toFixed(1)}%</div>
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </ReportCard>
        <ReportCard title="分批转化" description="每批注册用户的回访率与付费率" className="xl:col-span-3" loading={q.isLoading} height={300}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={300}
            rightFormat="percent"
            series={[
              { key: "registered", label: "注册", type: "bar", tone: "indigo" },
              { key: "returnRate", label: "回访率", type: "line", tone: "sky", right: true },
              { key: "payRate", label: "付费率", type: "line", tone: "emerald", right: true }
            ]}
          />
        </ReportCard>
      </div>
      <SectionGrid>
        <ReportCard title="注册到首次回访" loading={q.isLoading}>
          <SummaryStrip summary={d?.daysToReturn} format="days" />
        </ReportCard>
        <ReportCard title="注册到首次付费" loading={q.isLoading}>
          <SummaryStrip summary={d?.hoursToPay} format="hours" />
        </ReportCard>
      </SectionGrid>
    </ReportSection>
  );
}
