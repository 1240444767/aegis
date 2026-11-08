import { ApiError, apiRequest, buildQuery, joinApiUrl } from "./client";

/**
 * 报表分析中心。
 *
 * 所有报表共用一个窗口：起止日期按浏览器时区（tz）取日历日、含首尾两天；
 * 趋势是列式的（buckets + series[key][i]），没有数据的桶由后端补 0。
 * KPI 自带上一个等长窗口的对照值与相对变化。
 */

export type ReportWindow = {
  from: string;
  to: string;
  previousFrom: string;
  previousTo: string;
  timeZone: string;
  granularity: ReportGranularity;
  days: number;
  buckets: string[];
  generatedAt: string;
};

export type ReportGranularity = "day" | "week" | "month";

export type ReportKPI = { value: number; previous?: number; change?: number; spark?: number[] };
export type ReportKPIs<K extends string = string> = Partial<Record<K, ReportKPI>>;

export type ReportTrend<K extends string = string> = { buckets: string[]; series: Partial<Record<K, number[]>> };

export type ReportSlice = {
  key: string;
  label?: string;
  count: number;
  users?: number;
  amount?: number;
  value?: number;
  share: number;
  meta?: Record<string, string>;
};

export type ReportHeatmap = { cells: number[][]; max: number; total: number };

export type ReportSummary = {
  count: number;
  sum: number;
  mean: number;
  std: number;
  min: number;
  p25: number;
  p50: number;
  p75: number;
  p90: number;
  p99: number;
  max: number;
};

export type ReportBin = { label: string; min: number; max: number; count: number };

export type ReportInsight = {
  kind: "spike" | "drop" | "growth" | "decline";
  metric: string;
  bucket?: string;
  value: number;
  expected: number;
  score: number;
};

export type ReportLifecycle = { new: number; retained: number; resurrected: number; churned: number };

type Base = { window: ReportWindow };

export type OverviewReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  lifecycle: ReportLifecycle;
  insights: ReportInsight[];
};

export type UsersReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  sources: ReportSlice[];
  provinces: ReportSlice[];
  weekdays: ReportSlice[];
  heatmap: ReportHeatmap;
  insights: ReportInsight[];
};

export type LoginReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  types: ReportSlice[];
  providers: ReportSlice[];
  failures: ReportSlice[];
  failureIps: ReportSlice[];
  frequency: ReportSlice[];
  heatmap: ReportHeatmap;
};

export type ActiveReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  lifecycle: ReportLifecycle;
  frequency: ReportSlice[];
  signals: ReportSlice[];
  heatmap: ReportHeatmap;
};

export type RetentionCohort = { cohort: string; size: number; retained: Array<number | null>; rates: Array<number | null> };

export type RetentionReport = Base & {
  unit: "day" | "week";
  mode: "exact" | "rolling";
  offsets: number[];
  rows: RetentionCohort[];
  average: Array<number | null>;
  kpis: ReportKPIs;
};

export type DeviceReport = Base & {
  kpis: ReportKPIs;
  platforms: ReportSlice[];
  manufacturers: ReportSlice[];
  models: ReportSlice[];
  osVersions: ReportSlice[];
  appVersions: ReportSlice[];
  browsers: ReportSlice[];
};

export type RegionReport = Base & {
  kpis: ReportKPIs;
  countries: ReportSlice[];
  provinces: ReportSlice[];
  cities: ReportSlice[];
  isps: ReportSlice[];
  registerProvinces: ReportSlice[];
};

export type ChannelReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  registerSources: ReportSlice[];
  loginProviders: ReportSlice[];
  inviteSources: ReportSlice[];
  cardKeySources: ReportSlice[];
};

export type PaymentReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  methods: ReportSlice[];
  providers: ReportSlice[];
  products: ReportSlice[];
  statuses: ReportSlice[];
  vipPlans: ReportSlice[];
  amounts: ReportSummary;
  histogram: ReportBin[];
  heatmap: ReportHeatmap;
  insights: ReportInsight[];
};

export type NotificationReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  types: ReportSlice[];
  levels: ReportSlice[];
  readLatency: ReportSummary;
};

export type RiskReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  levels: ReportSlice[];
  actions: ReportSlice[];
  scenes: ReportSlice[];
  rules: ReportSlice[];
  ips: ReportSlice[];
  scores: ReportSummary;
  latency: ReportSummary;
  firewall: {
    total: number;
    trend: ReportTrend;
    severity: ReportSlice[];
    rules: ReportSlice[];
    countries: ReportSlice[];
    paths: ReportSlice[];
  };
};

export type ActivityRow = {
  id: number;
  name: string;
  status: string;
  draws: number;
  participants: number;
  wins: number;
  winRate: number;
  pointsCost: number;
  pity: number;
};

export type ActivityReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  activities: ActivityRow[];
  prizes: ReportSlice[];
  kinds: ReportSlice[];
  sources: ReportSlice[];
};

export type EngagementReport = Base & {
  kpis: ReportKPIs;
  trend: ReportTrend;
  signSources: ReportSlice[];
  streaks: ReportSlice[];
  pointsEarned: ReportSlice[];
  pointsSpent: ReportSlice[];
  rewardedAds: ReportSlice[];
  rewardedScenes: ReportSlice[];
  splashAds: ReportSlice[];
};

export type FunnelStep = { key: string; users: number; stepRate: number; overallRate: number };

export type FunnelReport = Base & {
  kpis: ReportKPIs;
  steps: FunnelStep[];
  trend: ReportTrend;
  hoursToPay: ReportSummary;
  daysToReturn: ReportSummary;
};

export type ReportParams = {
  start: string;
  end: string;
  tz: string;
  granularity: ReportGranularity;
  unit?: "day" | "week";
  mode?: "exact" | "rolling";
};

export type ReportKind =
  | "overview"
  | "registration"
  | "active"
  | "retention"
  | "funnel"
  | "login"
  | "device"
  | "region"
  | "channel"
  | "payment"
  | "engagement"
  | "activity"
  | "notification"
  | "risk";

export type ReportOf = {
  overview: OverviewReport;
  registration: UsersReport;
  active: ActiveReport;
  retention: RetentionReport;
  funnel: FunnelReport;
  login: LoginReport;
  device: DeviceReport;
  region: RegionReport;
  channel: ChannelReport;
  payment: PaymentReport;
  engagement: EngagementReport;
  activity: ActivityReport;
  notification: NotificationReport;
  risk: RiskReport;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/reports`;

export function getReport<K extends ReportKind>(token: string, appKey: string, kind: K, params: ReportParams) {
  return apiRequest<ReportOf[K]>(`${base(appKey)}/${kind}${buildQuery(params)}`, { token });
}

/**
 * 导出 CSV。接口要 Bearer 令牌，不能直接把地址塞进 <a href> 或 window.open
 * （浏览器发出的请求不带头，令牌也不该出现在地址栏和访问日志里），这里 fetch 成 blob 再触发下载。
 */
export async function downloadReport(token: string, appKey: string, type: ReportKind | "all", params: ReportParams) {
  const response = await fetch(joinApiUrl(`${base(appKey)}/export${buildQuery({ ...params, type })}`), {
    headers: { Authorization: `Bearer ${token}`, "X-Admin-Token": token },
    cache: "no-store"
  });
  if (!response.ok) {
    let message = `导出失败（HTTP ${response.status}）`;
    try {
      const payload = await response.json();
      if (payload?.message) message = payload.message;
    } catch {
      // 响应不是 JSON，保留默认提示
    }
    throw new ApiError(message, { status: response.status });
  }
  const blob = await response.blob();
  const disposition = response.headers.get("content-disposition") || "";
  const match = /filename\*?=(?:UTF-8'')?"?([^;"]+)"?/i.exec(disposition);
  const filename = match ? decodeURIComponent(match[1]) : `report_${type}.csv`;
  const objectUrl = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = objectUrl;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(objectUrl);
  return filename;
}
