"use client";

import { useMemo } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  AlertTriangle,
  BadgeDollarSign,
  Ban,
  Bell,
  BellRing,
  CalendarCheck,
  CircleDollarSign,
  Clock3,
  Coins,
  Eye,
  Flame,
  Gauge,
  Gift,
  MailOpen,
  Percent,
  Receipt,
  Repeat,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Target,
  Ticket,
  Trophy,
  UserPlus,
  Users,
  Wallet,
  Zap
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useReportQuery } from "@/lib/report-hooks";
import { CountryFlag } from "@/components/ui/country-flag";
import {
  Donut,
  HeatmapGrid,
  InsightList,
  KpiGrid,
  RankList,
  ReportCard,
  SectionGrid,
  ShareBar,
  SummaryStrip,
  TrendChart,
  color,
  trendEmpty,
  type KpiSpec,
  type SeriesSpec,
  type Tone
} from "./report-kit";
import {
  ACTIVITY_STATUS,
  AD_STATUS,
  DRAW_SOURCE,
  NOTIFICATION_LEVEL,
  NOTIFICATION_TYPE,
  ORDER_STATUS,
  PAYMENT_METHOD,
  POINT_CATEGORY,
  PRIZE_KIND,
  RISK_ACTION,
  RISK_LEVEL,
  RISK_SCENE,
  SEVERITY,
  SIGN_SOURCE,
  formatValue,
  labelOf
} from "./report-labels";
import { ReportSection, type SectionProps } from "./report-section";

export function OperationsPage(props: SectionProps) {
  return (
    <div className="space-y-10">
      <PaymentSection {...props} />
      <EngagementSection {...props} />
      <LotterySection {...props} />
      <NotificationSection {...props} />
      <RiskSection {...props} />
    </div>
  );
}

/* ── 支付 ── */

const PAYMENT_KPIS: KpiSpec[] = [
  { key: "revenue", label: "收入", icon: Wallet, tone: "emerald", format: "money", hint: "按到账时间" },
  { key: "refunded", label: "退款", icon: RotateCcw, tone: "rose", format: "money", inverse: true },
  { key: "net", label: "净收入", icon: CircleDollarSign, tone: "emerald", format: "money" },
  { key: "orders", label: "支付订单", icon: Receipt, tone: "indigo" },
  { key: "payers", label: "付费用户", icon: Users, tone: "sky" },
  { key: "newPayers", label: "首次付费", icon: UserPlus, tone: "teal" },
  { key: "arppu", label: "付费用户均收入", icon: Gauge, tone: "amber", format: "money" },
  { key: "arpu", label: "活跃用户均收入", icon: Gauge, tone: "amber", format: "money", hint: "按净收入" },
  { key: "avgOrder", label: "客单价", icon: BadgeDollarSign, tone: "violet", format: "money" },
  { key: "payRate", label: "付费率", icon: Percent, tone: "pink", format: "percent" },
  { key: "successRate", label: "下单支付成功率", icon: Target, tone: "sky", format: "percent" },
  { key: "repeatRate", label: "复购率", icon: Repeat, tone: "indigo", format: "percent", hint: "期内付费 2 次以上" }
];

function PaymentSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("payment", appKey, params);
  const d = q.data;
  const histConfig = useMemo(() => ({ count: { label: "订单数", color: color("emerald") } }), []);
  return (
    <ReportSection id="payment" title="支付" description="收入按到账时间计，退款按退款成功时间计" icon={Wallet} tone="emerald">
      <KpiGrid specs={PAYMENT_KPIS} kpis={d?.kpis} loading={q.isLoading} />
      <SectionGrid>
        <ReportCard title="收入趋势" loading={q.isLoading} empty={trendEmpty(d?.trend, ["revenue", "refunded"])} height={260}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            format="money"
            height={260}
            series={[
              { key: "revenue", label: "收入", type: "area", tone: "emerald" },
              { key: "refunded", label: "退款", type: "bar", tone: "rose" },
              { key: "net", label: "净收入", type: "line", tone: "indigo", dashed: true }
            ]}
          />
        </ReportCard>
        <ReportCard title="订单与付费用户" loading={q.isLoading} empty={trendEmpty(d?.trend, ["orders", "created"])} height={260}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={260}
            series={[
              { key: "created", label: "下单", type: "bar", tone: "slate" },
              { key: "orders", label: "支付", type: "bar", tone: "indigo" },
              { key: "payers", label: "付费用户", type: "line", tone: "sky" }
            ]}
          />
        </ReportCard>
      </SectionGrid>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="支付方式" loading={q.isLoading}>
          <Donut items={d?.methods} dict={PAYMENT_METHOD} metric="amount" format="money" centerLabel="收入" />
        </ReportCard>
        <ReportCard title="商品" loading={q.isLoading}>
          <RankList items={d?.products} metric="amount" format="money" secondary={(it) => `${formatValue(it.count)} 单`} />
        </ReportCard>
        <ReportCard title="会员开通" description="含购买、兑换与赠送" loading={q.isLoading}>
          <RankList
            items={d?.vipPlans}
            tone="violet"
            labelOf={(it) => (
              <>
                {labelOf(undefined, it.key)}
                {it.label === "lifetime" ? <Badge variant="warning" size="sm" className="ml-1.5">永久</Badge> : null}
              </>
            )}
            secondary={(it) => formatValue(it.amount ?? 0, "money")}
          />
        </ReportCard>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="订单金额分布" loading={q.isLoading} empty={!d || d.amounts.count === 0} height={200}>
          <ChartContainer config={histConfig} className="aspect-auto h-[200px] w-full">
            <BarChart data={d?.histogram ?? []} margin={{ left: 0, right: 8, top: 8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={10} interval={0} angle={-30} textAnchor="end" height={40} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Bar dataKey="count" fill="var(--color-count)" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ChartContainer>
        </ReportCard>
        <ReportCard title="订单金额统计" loading={q.isLoading}>
          <SummaryStrip summary={d?.amounts} format="money" />
        </ReportCard>
        <ReportCard title="订单状态与通道" description="订单状态按下单时间" loading={q.isLoading}>
          <ShareBar items={d?.statuses} dict={ORDER_STATUS} />
          <div className="mt-4 border-t pt-3">
            <RankList items={d?.providers} metric="amount" format="money" limit={5} tone="teal" />
          </div>
        </ReportCard>
      </div>
      <SectionGrid>
        <ReportCard title="付款时段" loading={q.isLoading}>
          <HeatmapGrid heatmap={d?.heatmap} tone="emerald" unit="单" />
        </ReportCard>
        <ReportCard title="收入异常" loading={q.isLoading}>
          <InsightList insights={d?.insights} />
        </ReportCard>
      </SectionGrid>
    </ReportSection>
  );
}

/* ── 互动 ── */

const ENGAGEMENT_KPIS: KpiSpec[] = [
  { key: "signins", label: "签到次数", icon: CalendarCheck, tone: "lime" },
  { key: "signUsers", label: "签到用户", icon: Users, tone: "teal" },
  { key: "pointsEarned", label: "发放积分", icon: Coins, tone: "amber" },
  { key: "pointsSpent", label: "消耗积分", icon: Flame, tone: "rose" },
  { key: "adViews", label: "激励广告", icon: Eye, tone: "sky" },
  { key: "adGrantRate", label: "广告奖励发放率", icon: Percent, tone: "indigo", format: "percent" },
  { key: "splashShown", label: "开屏展示", icon: Zap, tone: "violet" },
  { key: "signReward", label: "签到奖励积分", icon: Gift, tone: "pink" }
];

function EngagementSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("engagement", appKey, params);
  const d = q.data;
  return (
    <ReportSection id="engagement" title="互动" description="签到、积分流转与广告" icon={Sparkles} tone="lime">
      <KpiGrid specs={ENGAGEMENT_KPIS} kpis={d?.kpis} loading={q.isLoading} />
      <div className="grid gap-4 xl:grid-cols-3">
        <ReportCard title="签到" loading={q.isLoading} empty={trendEmpty(d?.trend, ["signins"])} height={240}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={240}
            series={[
              { key: "signins", label: "签到次数", type: "bar", tone: "lime" },
              { key: "signUsers", label: "签到用户", type: "line", tone: "teal" }
            ]}
          />
        </ReportCard>
        <ReportCard title="积分流转" loading={q.isLoading} empty={trendEmpty(d?.trend, ["pointsEarned", "pointsSpent"])} height={240}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={240}
            series={[
              { key: "pointsEarned", label: "发放", type: "area", tone: "amber" },
              { key: "pointsSpent", label: "消耗", type: "area", tone: "rose" }
            ]}
          />
        </ReportCard>
        <ReportCard title="激励广告" loading={q.isLoading} empty={trendEmpty(d?.trend, ["adViews"])} height={240}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={240}
            series={[
              { key: "adViews", label: "观看", type: "bar", tone: "sky" },
              { key: "adGranted", label: "已发奖励", type: "line", tone: "indigo" }
            ]}
          />
        </ReportCard>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="积分从哪里来" loading={q.isLoading}>
          <RankList items={d?.pointsEarned} dict={POINT_CATEGORY} metric="value" tone="amber" secondary={(it) => `${formatValue(it.users)} 人`} />
        </ReportCard>
        <ReportCard title="积分花在哪里" loading={q.isLoading}>
          <RankList items={d?.pointsSpent} dict={POINT_CATEGORY} metric="value" tone="rose" secondary={(it) => `${formatValue(it.users)} 人`} />
        </ReportCard>
        <ReportCard title="连续签到" description="期内每人的最长连签天数" loading={q.isLoading}>
          <RankList items={d?.streaks} ordinal={false} labelOf={(it) => `${it.key} 天`} tone="lime" />
          <div className="mt-3 border-t pt-3">
            <div className="mb-2 text-xs text-muted-foreground">签到入口</div>
            <ShareBar items={d?.signSources} dict={SIGN_SOURCE} />
          </div>
        </ReportCard>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="激励广告状态" loading={q.isLoading}>
          <Donut items={d?.rewardedAds} dict={AD_STATUS} centerLabel="观看" />
        </ReportCard>
        <ReportCard title="激励广告场景" loading={q.isLoading}>
          <RankList items={d?.rewardedScenes} tone="sky" secondary={(it) => `${formatValue(it.value ?? 0)} 发奖`} />
        </ReportCard>
        <ReportCard title="开屏广告" description="附注为平均加载耗时" loading={q.isLoading}>
          <RankList items={d?.splashAds} dict={AD_STATUS} tone="violet" secondary={(it) => formatValue(it.value ?? 0, "ms")} />
        </ReportCard>
      </div>
    </ReportSection>
  );
}

/* ── 抽奖 ── */

const LOTTERY_KPIS: KpiSpec[] = [
  { key: "draws", label: "抽奖次数", icon: Ticket, tone: "pink" },
  { key: "participants", label: "参与人数", icon: Users, tone: "sky" },
  { key: "wins", label: "中奖次数", icon: Trophy, tone: "amber" },
  { key: "winRate", label: "中奖率", icon: Percent, tone: "emerald", format: "percent" },
  { key: "perUser", label: "人均抽奖", icon: Repeat, tone: "indigo", format: "decimal" },
  { key: "pointsCost", label: "消耗积分", icon: Coins, tone: "rose" }
];

function LotterySection({ appKey, params }: SectionProps) {
  const q = useReportQuery("activity", appKey, params);
  const d = q.data;
  return (
    <ReportSection id="lottery" title="抽奖活动" icon={Trophy} tone="pink">
      <KpiGrid specs={LOTTERY_KPIS} kpis={d?.kpis} loading={q.isLoading} columns={6} />
      <div className="grid gap-4 xl:grid-cols-3">
        <ReportCard title="抽奖趋势" className="xl:col-span-2" loading={q.isLoading} empty={trendEmpty(d?.trend, ["draws"])} height={240}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={240}
            series={[
              { key: "draws", label: "抽奖", type: "bar", tone: "pink" },
              { key: "wins", label: "中奖", type: "bar", tone: "amber" },
              { key: "pointsCost", label: "消耗积分", type: "line", tone: "rose", right: true }
            ]}
            rightFormat="int"
          />
        </ReportCard>
        <ReportCard title="次数来源" loading={q.isLoading}>
          <Donut items={d?.sources} dict={DRAW_SOURCE} centerLabel="抽奖" />
        </ReportCard>
      </div>
      <SectionGrid>
        <ReportCard title="活动明细" loading={q.isLoading} empty={!d?.activities.length} height={160}>
          <div className="overflow-x-auto rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>活动</TableHead>
                  <TableHead className="text-right">抽奖</TableHead>
                  <TableHead className="text-right">人数</TableHead>
                  <TableHead className="text-right">中奖率</TableHead>
                  <TableHead className="text-right">积分</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {d?.activities.map((a) => (
                  <TableRow key={a.id}>
                    <TableCell className="text-xs">
                      <div className="flex items-center gap-1.5">
                        <span className="max-w-[180px] truncate">{a.name}</span>
                        <Badge variant={a.status === "active" ? "success" : "outline"} size="sm">
                          {labelOf(ACTIVITY_STATUS, a.status)}
                        </Badge>
                      </div>
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">{formatValue(a.draws)}</TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">{formatValue(a.participants)}</TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">{formatValue(a.winRate, "percent")}</TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">{formatValue(a.pointsCost)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </ReportCard>
        <ReportCard title="奖品发放" loading={q.isLoading}>
          <RankList
            items={d?.prizes}
            labelOf={(it) => (
              <>
                {labelOf(undefined, it.key)}
                {it.label ? <span className="ml-1.5 text-muted-foreground">{labelOf(PRIZE_KIND, it.label)}</span> : null}
              </>
            )}
            secondary={(it) => `${formatValue(it.users)} 人`}
          />
        </ReportCard>
      </SectionGrid>
    </ReportSection>
  );
}

/* ── 通知 ── */

const NOTIFICATION_KPIS: KpiSpec[] = [
  { key: "sent", label: "发出通知", icon: Bell, tone: "violet" },
  { key: "deliveries", label: "送达人次", icon: BellRing, tone: "indigo" },
  { key: "reads", label: "阅读人次", icon: MailOpen, tone: "sky", hint: "按阅读时间" },
  { key: "readRate", label: "已读率", icon: Percent, tone: "emerald", format: "percent", hint: "期内发出的通知" },
  { key: "medianRead", label: "阅读耗时中位数", icon: Clock3, tone: "amber", format: "minutes" }
];

function NotificationSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("notification", appKey, params);
  const d = q.data;
  const readRate = (it: { users?: number; value?: number }) => (it.users ? formatValue((it.value ?? 0) / it.users, "percent") : "—");
  return (
    <ReportSection id="notification" title="通知" icon={Bell} tone="violet">
      <KpiGrid specs={NOTIFICATION_KPIS} kpis={d?.kpis} loading={q.isLoading} columns={5} />
      <div className="grid gap-4 xl:grid-cols-3">
        <ReportCard title="发送与阅读" className="xl:col-span-2" loading={q.isLoading} empty={trendEmpty(d?.trend, ["sent", "reads"])} height={240}>
          <TrendChart
            trend={d?.trend}
            granularity={params.granularity}
            height={240}
            series={[
              { key: "deliveries", label: "送达", type: "bar", tone: "indigo" },
              { key: "reads", label: "阅读", type: "bar", tone: "sky" },
              { key: "sent", label: "通知条数", type: "line", tone: "violet" }
            ]}
          />
        </ReportCard>
        <ReportCard title="从发送到阅读" loading={q.isLoading}>
          <SummaryStrip summary={d?.readLatency} format="minutes" />
        </ReportCard>
      </div>
      <SectionGrid>
        <ReportCard title="通知类型" description="附注为已读率" loading={q.isLoading}>
          <RankList items={d?.types} dict={NOTIFICATION_TYPE} metric="users" tone="violet" secondary={readRate} />
        </ReportCard>
        <ReportCard title="通知级别" description="附注为已读率" loading={q.isLoading}>
          <RankList items={d?.levels} dict={NOTIFICATION_LEVEL} metric="users" tone="indigo" secondary={readRate} />
        </ReportCard>
      </SectionGrid>
    </ReportSection>
  );
}

/* ── 风控 ── */

const RISK_KPIS: KpiSpec[] = [
  { key: "assessments", label: "风控评估", icon: ShieldCheck, tone: "indigo" },
  { key: "blocked", label: "拦截与封禁", icon: Ban, tone: "rose", inverse: true },
  { key: "blockRate", label: "拦截率", icon: Percent, tone: "rose", format: "percent", inverse: true },
  { key: "flagged", label: "需处置", icon: AlertTriangle, tone: "amber", hint: "验证、复核、拦截与封禁" },
  { key: "avgScore", label: "平均风险分", icon: Gauge, tone: "violet", format: "decimal" },
  { key: "p90Latency", label: "评估耗时 P90", icon: Clock3, tone: "sky", format: "ms" }
];

const LEVEL_TONE: Record<string, Tone> = { normal: "emerald", low: "sky", medium: "amber", high: "rose", critical: "violet", unknown: "slate" };
const LEVEL_ORDER = ["normal", "low", "medium", "high", "critical", "unknown"];

function RiskSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("risk", appKey, params);
  const d = q.data;
  const levelSeries = useMemo<SeriesSpec[]>(
    () =>
      Object.keys(d?.trend.series ?? {})
        .sort((a, b) => LEVEL_ORDER.indexOf(a) - LEVEL_ORDER.indexOf(b))
        .map((key) => ({ key, label: labelOf(RISK_LEVEL, key), type: "bar", stack: "l", tone: LEVEL_TONE[key] ?? "slate" })),
    [d]
  );
  const fwSeries = useMemo<SeriesSpec[]>(
    () =>
      Object.keys(d?.firewall.trend.series ?? {}).map((key, i) => ({ key, label: labelOf(SEVERITY, key), type: "area", stack: "f", tone: LEVEL_TONE[key] ?? i })),
    [d]
  );
  return (
    <ReportSection id="risk" title="风控" description="本应用的风控评估；防火墙拦截为全平台数据" icon={ShieldAlert} tone="rose">
      <KpiGrid specs={RISK_KPIS} kpis={d?.kpis} loading={q.isLoading} columns={6} />
      <div className="grid gap-4 xl:grid-cols-3">
        <ReportCard title="评估趋势" description="按风险等级堆叠" className="xl:col-span-2" loading={q.isLoading} empty={levelSeries.length === 0} height={240}>
          <TrendChart trend={d?.trend} granularity={params.granularity} height={240} series={levelSeries} />
        </ReportCard>
        <ReportCard title="处置动作" loading={q.isLoading}>
          <Donut items={d?.actions} dict={RISK_ACTION} centerLabel="评估" />
        </ReportCard>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="命中规则" description="附注为累计加分" loading={q.isLoading}>
          <RankList items={d?.rules} tone="rose" secondary={(it) => `${formatValue(it.value ?? 0)} 分`} />
        </ReportCard>
        <ReportCard title="场景" description="附注为拦截次数" loading={q.isLoading}>
          <RankList items={d?.scenes} dict={RISK_SCENE} tone="amber" secondary={(it) => `${formatValue(it.value ?? 0)} 拦截`} />
          <div className="mt-3 border-t pt-3">
            <div className="mb-2 text-xs text-muted-foreground">风险等级</div>
            <ShareBar items={d?.levels} dict={RISK_LEVEL} />
          </div>
        </ReportCard>
        <ReportCard title="高风险 IP" description="非放行的评估，附注为最高分" loading={q.isLoading}>
          <RankList
            items={d?.ips}
            tone="rose"
            labelOf={(it) => (
              <>
                <span className="font-mono">{it.key}</span>
                {it.label ? <span className="ml-1.5 text-muted-foreground">{it.label}</span> : null}
              </>
            )}
            secondary={(it) => `${formatValue(it.value ?? 0)} 分`}
          />
        </ReportCard>
      </div>
      <SectionGrid>
        <ReportCard title="风险分分布" loading={q.isLoading}>
          <SummaryStrip summary={d?.scores} format="decimal" />
        </ReportCard>
        <ReportCard title="评估耗时" loading={q.isLoading}>
          <SummaryStrip summary={d?.latency} format="ms" />
        </ReportCard>
      </SectionGrid>
      <div className="grid gap-4 xl:grid-cols-3">
        <ReportCard
          title={
            <span className="flex items-center gap-2">
              防火墙拦截
              <Badge variant="outline" size="sm">全平台</Badge>
            </span>
          }
          description={d ? `期内共拦截 ${formatValue(d.firewall.total)} 次` : undefined}
          className="xl:col-span-2"
          loading={q.isLoading}
          empty={fwSeries.length === 0}
          height={220}
        >
          <TrendChart trend={d?.firewall.trend} granularity={params.granularity} height={220} series={fwSeries} />
        </ReportCard>
        <ReportCard title="拦截来源" loading={q.isLoading}>
          <RankList
            items={d?.firewall.countries}
            limit={6}
            tone="rose"
            leading={(it) => <CountryFlag code={it.key === "unknown" ? "" : it.key} size={16} />}
            labelOf={(it) => it.label || labelOf(undefined, it.key)}
          />
        </ReportCard>
      </div>
      <SectionGrid>
        <ReportCard title="防火墙规则" loading={q.isLoading}>
          <RankList items={d?.firewall.rules} tone="rose" labelOf={(it) => it.label || it.key} secondary={(it) => `${formatValue(it.users)} 个 IP`} />
        </ReportCard>
        <ReportCard title="被拦截的路径" loading={q.isLoading}>
          <RankList items={d?.firewall.paths} tone="amber" labelOf={(it) => <span className="font-mono">{it.key}</span>} />
        </ReportCard>
      </SectionGrid>
    </ReportSection>
  );
}
