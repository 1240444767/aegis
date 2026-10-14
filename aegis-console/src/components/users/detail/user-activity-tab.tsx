"use client";

import { useMemo, useState, type ComponentType, type ReactNode } from "react";
import Link from "next/link";
import {
  ArrowUpRight,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Clock3,
  FileClock,
  Fingerprint,
  Globe2,
  KeyRound,
  KeySquare,
  LogIn,
  LogOut,
  Map as MapIcon,
  MapPin,
  MonitorSmartphone,
  Radar,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  ShieldX,
  TimerOff,
  Wifi
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { CountryFlag } from "@/components/ui/country-flag";
import { useNow } from "@/components/users/ban-form";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { ActionBadge, LEVEL_COLORS, LevelBadge, RiskCatalogProvider, SceneBadge } from "@/components/risk/risk-shared";
import { useAdminUserSessionsQuery } from "@/lib/admin-hooks";
import { useAdminUserLoginAuditsQuery, useAdminUserSessionAuditsQuery } from "@/lib/app-user-hooks";
import type { AdminAppUserDetail, UserLoginAuditRecord, UserSessionAuditRecord } from "@/lib/api/types";
import { formatClock, formatWith, localDateKey } from "@/lib/datetime";
import { useRiskAssessmentsQuery } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";
import { DeviceInfoChip } from "../device-info-chip";
import { UserSessionsPanel } from "../user-sessions-panel";
import { UserActivityMap } from "./user-activity-map";
import {
  LOGIN_STATUS_LABEL,
  Panel,
  SESSION_EVENT_LABEL,
  ValueTree,
  describeUserAgent,
  formatTime,
  relativeTime,
  textValue
} from "./user-detail-shared";

/**
 * 活动：这个账号最近发生了什么。
 *
 * 自上而下是「先看结论、再看明细」：
 *
 *   活动概览   最近登录、成功与失败、去过几个地方、近 30 天每天的登录情况
 *   活动地图   同一批数据的空间投影
 *   活跃会话   现在谁拿着这个账号的令牌（可撤销）
 *   登录记录   按天分组的时间线，可按结果筛选，展开看设备与原始信息
 *   会话事件   令牌的签发、刷新、撤销轨迹
 *   风控评估   引擎给这个账号打的分
 *
 * 四条流刻意不合并成一条「统一时间线」：合并之后就没法按「只看失败登录」这种
 * 真实的排查意图筛选了。概览与地图共用同一份不带筛选的 100 条登录样本
 * （同一个查询键，只请求一次），不会随下方的筛选变化。
 */

/** 与 UserActivityMap 的样本量一致：同一个查询键命中同一份缓存 */
const LOGIN_SAMPLE = 100;
const LOGIN_PAGE_SIZE = 15;
const EVENT_PAGE_SIZE = 15;
const STRIP_DAYS = 30;

type LoginFilter = "all" | "success" | "failed" | "blocked";

const LOGIN_FILTERS: Array<{ value: LoginFilter; label: string }> = [
  { value: "all", label: "全部" },
  { value: "success", label: "成功" },
  { value: "failed", label: "失败" },
  { value: "blocked", label: "被拦截" }
];

const LOGIN_TYPE_LABEL: Record<string, string> = {
  password: "密码登录",
  register: "注册",
  refresh: "令牌续期",
  sms: "短信验证码",
  email: "邮箱验证码",
  email_code: "邮箱验证码",
  oauth: "第三方登录",
  oauth2: "第三方登录",
  native_oauth: "第三方登录",
  qrcode: "扫码登录",
  qr: "扫码登录",
  scan: "扫码登录",
  cardkey: "卡密登录",
  passkey: "通行密钥",
  webauthn: "通行密钥",
  guest: "游客登录",
  device: "设备登录"
};

const PROVIDER_LABEL: Record<string, string> = {
  password: "密码",
  sms: "短信",
  email: "邮箱",
  cardkey: "卡密",
  qq: "QQ",
  wechat: "微信",
  weixin: "微信",
  github: "GitHub",
  google: "Google",
  apple: "Apple",
  microsoft: "Microsoft",
  gitee: "Gitee",
  dingtalk: "钉钉",
  feishu: "飞书",
  alipay: "支付宝"
};

const FAILURE_REASON_LABEL: Record<string, string> = {
  invalid_password: "密码错误",
  invalid_code: "验证码错误",
  user_disabled: "账号已停用",
  user_banned: "账号被封禁",
  rate_limited: "尝试过于频繁",
  risk_blocked: "风控拦截",
  captcha_failed: "人机验证未通过"
};

function loginTypeLabel(item: UserLoginAuditRecord) {
  const type = LOGIN_TYPE_LABEL[item.loginType] ?? textValue(item.loginType, "登录");
  const provider = item.provider ? PROVIDER_LABEL[item.provider.toLowerCase()] ?? item.provider : "";
  if (!provider || type.includes(provider) || provider === "密码") return type;
  return `${type}（${provider}）`;
}

function loginLocation(item: Pick<UserLoginAuditRecord, "isPrivate" | "city" | "region" | "country" | "location">) {
  if (item.isPrivate) return "内网地址";
  const parts: string[] = [];
  for (const part of [item.country, item.region, item.city]) {
    if (part && parts[parts.length - 1] !== part) parts.push(part);
  }
  return parts.length ? parts.join(" ") : textValue(item.location, "");
}

function failureReason(item: UserLoginAuditRecord) {
  const reason = item.metadata?.reason;
  if (typeof reason !== "string" || !reason) return "";
  return FAILURE_REASON_LABEL[reason] ?? reason;
}

function ts(value?: string | null) {
  const t = value ? new Date(value).getTime() : NaN;
  return Number.isNaN(t) ? 0 : t;
}

/** 「今天」「昨天」「10月12日 周日」 */
function dayLabel(key: string, todayKey: string, yesterdayKey: string, sample: string) {
  if (key === todayKey) return "今天";
  if (key === yesterdayKey) return "昨天";
  return formatWith(sample, { month: "numeric", day: "numeric", weekday: "short" }, key);
}

export function UserActivityTab({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  return (
    <TooltipProvider delayDuration={150}>
      <div className="space-y-5">
        <ActivitySummary appKey={appKey} userId={userId} />

        <Panel title="活动地图" icon={<MapIcon className="size-4" />}>
          <UserActivityMap appKey={appKey} userId={userId} />
        </Panel>

        <Panel title="活跃会话" icon={<MonitorSmartphone className="size-4" />}>
          <UserSessionsPanel appKey={appKey} userId={userId} />
        </Panel>

        <LoginRecords appKey={appKey} userId={userId} />

        <div className="grid gap-5 xl:grid-cols-2">
          <SessionEvents appKey={appKey} userId={userId} />
          <RiskCatalogProvider>
            <UserRiskPanel account={user.account} />
          </RiskCatalogProvider>
        </div>
      </div>
    </TooltipProvider>
  );
}

// ── 活动概览 ──────────────────────────

type DayBucket = { key: string; date: Date; success: number; failed: number; blocked: number };

function ActivitySummary({ appKey, userId }: { appKey: string; userId: number }) {
  const loginsQuery = useAdminUserLoginAuditsQuery(appKey, userId, { limit: LOGIN_SAMPLE });
  const sessionsQuery = useAdminUserSessionsQuery(appKey, userId);
  const loading = loginsQuery.isLoading || sessionsQuery.isLoading;
  // 每分钟换一次：跨过零点时「近 30 天」的格子跟着挪
  const now = useNow(60_000);

  const summary = useMemo(() => {
    const logins = loginsQuery.data?.items ?? [];
    const sessions = sessionsQuery.data?.items ?? [];
    const since = now - STRIP_DAYS * 86_400_000;

    const lastSuccess = logins.find((l) => l.status === "success");
    const lastFailure = logins.find((l) => l.status === "failed" || l.status === "blocked");
    const recent = logins.filter((l) => ts(l.createdAt) >= since);
    const success = recent.filter((l) => l.status === "success").length;
    const failed = recent.filter((l) => l.status === "failed").length;
    const blocked = recent.filter((l) => l.status === "blocked").length;

    const places = new Map<string, number>();
    const ips = new Set<string>();
    for (const l of logins) {
      if (l.loginIp) ips.add(l.loginIp);
      const place = loginLocation(l);
      if (place) places.set(place, (places.get(place) ?? 0) + 1);
    }
    const topPlace = [...places.entries()].sort((a, b) => b[1] - a[1])[0]?.[0];
    const devices = new Set(sessions.map((s) => s.deviceId || s.userAgent).filter(Boolean));

    // 近 30 天每天一格，从最早一天到今天
    const days: DayBucket[] = [];
    const index = new Map<string, DayBucket>();
    for (let i = STRIP_DAYS - 1; i >= 0; i--) {
      const date = new Date(now - i * 86_400_000);
      const bucket: DayBucket = { key: localDateKey(date), date, success: 0, failed: 0, blocked: 0 };
      days.push(bucket);
      index.set(bucket.key, bucket);
    }
    for (const l of recent) {
      const bucket = index.get(localDateKey(l.createdAt));
      if (!bucket) continue;
      if (l.status === "success") bucket.success++;
      else if (l.status === "blocked") bucket.blocked++;
      else bucket.failed++;
    }
    const oldest = logins[logins.length - 1];
    // 样本是最近 100 条：若第 100 条仍在 30 天内，说明这 30 天里还有更早的记录没取到
    const truncated = (loginsQuery.data?.total ?? 0) > logins.length && Boolean(oldest) && ts(oldest?.createdAt) >= since;

    return {
      sessions: sessions.length,
      devices: devices.size,
      lastSuccess,
      lastFailure,
      success,
      failed,
      blocked,
      places: places.size,
      topPlace,
      ips: ips.size,
      days,
      truncated,
      empty: logins.length === 0
    };
  }, [loginsQuery.data, sessionsQuery.data, now]);

  const risky = summary.failed + summary.blocked;

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="grid grid-cols-2 lg:grid-cols-4">
        <SummaryTile
          icon={Wifi}
          label="活跃会话"
          loading={loading}
          value={`${summary.sessions}`}
          unit="个"
          hint={summary.sessions ? `${summary.devices} 台设备` : "当前没有有效会话"}
          tone={summary.sessions ? "success" : "muted"}
          index={0}
        />
        <SummaryTile
          icon={Clock3}
          label="最近登录"
          loading={loading}
          value={summary.lastSuccess ? relativeTime(summary.lastSuccess.createdAt) : "暂无"}
          hint={
            summary.lastSuccess
              ? [loginLocation(summary.lastSuccess), loginTypeLabel(summary.lastSuccess)].filter(Boolean).join("，")
              : "没有成功的登录记录"
          }
          title={summary.lastSuccess ? formatTime(summary.lastSuccess.createdAt) : undefined}
          index={1}
        />
        <SummaryTile
          icon={risky ? ShieldAlert : ShieldCheck}
          label={`近 ${STRIP_DAYS} 天登录`}
          loading={loading}
          value={`${summary.success}`}
          unit="次成功"
          hint={risky ? `失败 ${summary.failed} 次，拦截 ${summary.blocked} 次` : "没有失败或被拦截的登录"}
          tone={summary.blocked ? "danger" : summary.failed ? "warning" : "default"}
          index={2}
        />
        <SummaryTile
          icon={MapPin}
          label="登录地点"
          loading={loading}
          value={`${summary.places}`}
          unit="处"
          hint={summary.places ? `常用 ${summary.topPlace}，${summary.ips} 个 IP` : "未解析出地点"}
          index={3}
        />
      </div>

      <div className="border-t px-4 py-3.5 sm:px-5">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
          <span>
            近 {STRIP_DAYS} 天每日登录{summary.truncated ? `（按最近 ${LOGIN_SAMPLE} 条统计）` : ""}
          </span>
          <span className="flex items-center gap-3">
            <Legend className="bg-blue-600 dark:bg-blue-500" label="成功" />
            <Legend className="bg-amber-500" label="失败" />
            <Legend className="bg-red-500" label="被拦截" />
          </span>
        </div>
        {loading ? (
          <Skeleton className="h-12 w-full rounded-lg" />
        ) : (
          <DayStrip days={summary.days} />
        )}
        {summary.lastFailure && !loading ? (
          <div className="mt-2.5 flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <ShieldAlert className="size-3 text-amber-500" />
            最近一次失败 {relativeTime(summary.lastFailure.createdAt)}
            {failureReason(summary.lastFailure) ? `，${failureReason(summary.lastFailure)}` : ""}
            {summary.lastFailure.loginIp ? (
              <span className="font-mono">{summary.lastFailure.loginIp}</span>
            ) : null}
          </div>
        ) : null}
      </div>
    </Card>
  );
}

function SummaryTile({
  icon: Icon,
  label,
  value,
  unit,
  hint,
  title,
  tone = "default",
  loading,
  index
}: {
  icon: ComponentType<{ className?: string }>;
  label: string;
  value: string;
  unit?: string;
  hint: string;
  title?: string;
  tone?: "default" | "success" | "warning" | "danger" | "muted";
  loading: boolean;
  index: number;
}) {
  return (
    <div
      className={cn(
        "min-w-0 px-4 py-4 sm:px-5",
        index % 2 === 1 && "border-l",
        index >= 2 && "border-t lg:border-t-0",
        index >= 1 && "lg:border-l"
      )}
    >
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span
          className={cn(
            "grid size-6 place-items-center rounded-md",
            tone === "success" && "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
            tone === "warning" && "bg-amber-500/10 text-amber-600 dark:text-amber-400",
            tone === "danger" && "bg-red-500/10 text-red-600 dark:text-red-400",
            (tone === "default" || tone === "muted") && "bg-muted text-muted-foreground"
          )}
        >
          <Icon className="size-3.5" />
        </span>
        {label}
      </div>
      {loading ? (
        <>
          <Skeleton className="mt-2.5 h-6 w-20" />
          <Skeleton className="mt-1.5 h-3 w-28" />
        </>
      ) : (
        <>
          <div className="mt-2 flex items-baseline gap-1 truncate" title={title}>
            <span className="text-xl font-semibold tracking-tight tabular-nums">{value}</span>
            {unit ? <span className="text-xs text-muted-foreground">{unit}</span> : null}
          </div>
          <div className="mt-0.5 truncate text-[11px] text-muted-foreground" title={hint}>
            {hint}
          </div>
        </>
      )}
    </div>
  );
}

function Legend({ className, label }: { className: string; label: string }) {
  return (
    <span className="flex items-center gap-1">
      <span className={cn("size-2 rounded-sm", className)} />
      {label}
    </span>
  );
}

/** 每天一根柱：成功在下、失败与拦截叠在上，柱高按这 30 天里最多的一天归一。 */
function DayStrip({ days }: { days: DayBucket[] }) {
  const peak = Math.max(1, ...days.map((d) => d.success + d.failed + d.blocked));
  return (
    <div className="flex h-12 items-end gap-[3px]">
      {days.map((day) => {
        const total = day.success + day.failed + day.blocked;
        return (
          <Tooltip key={day.key}>
            <TooltipTrigger asChild>
              <div className="flex h-full min-w-0 flex-1 cursor-default flex-col justify-end rounded-sm hover:bg-muted/60">
                {total === 0 ? (
                  <div className="h-[3px] rounded-full bg-muted" />
                ) : (
                  <div className="flex flex-col gap-px overflow-hidden rounded-[3px]" style={{ height: `${Math.max(12, (total / peak) * 100)}%` }}>
                    {day.blocked ? <div className="bg-red-500" style={{ flexGrow: day.blocked }} /> : null}
                    {day.failed ? <div className="bg-amber-500" style={{ flexGrow: day.failed }} /> : null}
                    {day.success ? <div className="bg-blue-600 dark:bg-blue-500" style={{ flexGrow: day.success }} /> : null}
                  </div>
                )}
              </div>
            </TooltipTrigger>
            <TooltipContent side="top" className="text-xs">
              <div className="font-medium">{formatWith(day.date, { month: "numeric", day: "numeric", weekday: "short" })}</div>
              {total === 0 ? (
                <div className="text-muted-foreground">没有登录</div>
              ) : (
                <div className="tabular-nums">
                  成功 {day.success}，失败 {day.failed}，拦截 {day.blocked}
                </div>
              )}
            </TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}

// ── 登录记录 ──────────────────────────

const LOGIN_TONE: Record<string, { icon: ComponentType<{ className?: string }>; className: string }> = {
  success: { icon: LogIn, className: "bg-emerald-500/10 text-emerald-600 ring-emerald-500/20 dark:text-emerald-400" },
  failed: { icon: ShieldAlert, className: "bg-amber-500/10 text-amber-600 ring-amber-500/20 dark:text-amber-400" },
  blocked: { icon: ShieldX, className: "bg-red-500/10 text-red-600 ring-red-500/20 dark:text-red-400" },
  challenge: { icon: Fingerprint, className: "bg-sky-500/10 text-sky-600 ring-sky-500/20 dark:text-sky-400" }
};

function LoginRecords({ appKey, userId }: { appKey: string; userId: number }) {
  const [filter, setFilter] = useState<LoginFilter>("all");
  const [page, setPage] = useState(1);
  const [expanded, setExpanded] = useState<number | null>(null);
  const query = useAdminUserLoginAuditsQuery(appKey, userId, {
    status: filter === "all" ? undefined : filter,
    page,
    limit: LOGIN_PAGE_SIZE
  });
  const items = useMemo(() => query.data?.items ?? [], [query.data]);
  const totalPages = Math.max(1, query.data?.totalPages ?? 1);
  const now = useNow(60_000);

  const groups = useMemo(() => {
    const todayKey = localDateKey(now);
    const yesterdayKey = localDateKey(now - 86_400_000);
    const out: Array<{ key: string; label: string; items: UserLoginAuditRecord[] }> = [];
    for (const item of items) {
      const key = localDateKey(item.createdAt) || "unknown";
      let group = out[out.length - 1];
      if (!group || group.key !== key) {
        group = { key, label: dayLabel(key, todayKey, yesterdayKey, item.createdAt), items: [] };
        out.push(group);
      }
      group.items.push(item);
    }
    return out;
  }, [items, now]);

  return (
    <Panel
      title="登录记录"
      icon={<LogIn className="size-4" />}
      action={
        <div className="flex items-center gap-2">
          <span className="hidden text-xs text-muted-foreground tabular-nums sm:inline">
            {query.data ? `共 ${query.data.total.toLocaleString("zh-CN")} 条` : ""}
          </span>
          <FilterGroup
            value={filter}
            options={LOGIN_FILTERS}
            onChange={(value) => {
              setFilter(value);
              setPage(1);
              setExpanded(null);
            }}
          />
        </div>
      }
      bodyClassName="p-0 sm:px-0"
    >
      {query.isLoading ? (
        <ListSkeleton rows={5} />
      ) : items.length === 0 ? (
        <EmptyState icon={LogIn} text={filter === "all" ? "暂无登录记录" : `没有${LOGIN_STATUS_LABEL[filter]}的登录`} />
      ) : (
        <div className={cn(query.isFetching && "opacity-70 transition-opacity")}>
          {groups.map((group) => (
            <section key={group.key}>
              <div className="flex items-center gap-2 border-b bg-muted/40 px-4 py-1.5 text-[11px] font-medium text-muted-foreground sm:px-5">
                {group.label}
                <span className="font-normal tabular-nums">{group.items.length} 条</span>
              </div>
              <ul className="divide-y">
                {group.items.map((item) => (
                  <LoginRow
                    key={item.id}
                    item={item}
                    open={expanded === item.id}
                    onToggle={() => setExpanded(expanded === item.id ? null : item.id)}
                  />
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
      {totalPages > 1 ? (
        <Pager page={page} totalPages={totalPages} fetching={query.isFetching} onChange={(next) => { setPage(next); setExpanded(null); }} />
      ) : null}
    </Panel>
  );
}

function LoginRow({ item, open, onToggle }: { item: UserLoginAuditRecord; open: boolean; onToggle: () => void }) {
  const tone = LOGIN_TONE[item.status] ?? LOGIN_TONE.failed;
  const Icon = tone.icon;
  const ua = describeUserAgent(item.userAgent);
  const location = loginLocation(item);
  const reason = item.status !== "success" ? failureReason(item) : "";

  return (
    <li>
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full items-start gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/30 sm:px-5"
      >
        <span className={cn("mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg ring-1 ring-inset", tone.className)}>
          <Icon className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="text-sm font-medium">{loginTypeLabel(item)}</span>
            {item.status !== "success" ? (
              <Badge variant={item.status === "blocked" ? "danger" : item.status === "challenge" ? "info" : "warning"} size="sm">
                {LOGIN_STATUS_LABEL[item.status] ?? item.status}
                {reason ? `，${reason}` : ""}
              </Badge>
            ) : null}
          </div>
          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span className="inline-flex min-w-0 items-center gap-1.5">
              {item.countryCode && !item.isPrivate ? (
                <CountryFlag code={item.countryCode} size={14} className="shrink-0 rounded-[2px]" />
              ) : (
                <Globe2 className="size-3 shrink-0" />
              )}
              <span className="truncate">{location || "未知地点"}</span>
            </span>
            <span className="font-mono text-[11px]">{textValue(item.loginIp, "无 IP")}</span>
            {item.isp ? <span className="hidden truncate md:inline">{item.isp}</span> : null}
          </div>
        </div>
        <div className="hidden w-48 shrink-0 pt-0.5 lg:block">
          {item.deviceInfo ? (
            <DeviceInfoChip info={item.deviceInfo} fallback={ua.label} />
          ) : (
            <span className="block truncate text-xs text-muted-foreground" title={item.userAgent}>
              {ua.label}
            </span>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2 pt-0.5">
          <span className="text-xs tabular-nums text-muted-foreground" title={formatTime(item.createdAt)}>
            {formatClock(item.createdAt)}
          </span>
          <ChevronDown className={cn("size-4 text-muted-foreground transition-transform", open && "rotate-180")} />
        </div>
      </button>
      {open ? (
        <div className="border-t bg-muted/20 px-4 py-3.5 sm:px-5 sm:pl-16">
          <dl className="grid gap-x-6 gap-y-2.5 text-xs sm:grid-cols-2 xl:grid-cols-3">
            <Detail label="时间" value={formatTime(item.createdAt)} />
            <Detail label="来源 IP" value={item.loginIp} mono />
            <Detail label="归属地" value={[location, item.isp].filter(Boolean).join(" ")} />
            <Detail label="设备" value={item.deviceInfo ? <DeviceInfoChip info={item.deviceInfo} fallback={ua.label} /> : ua.label} />
            <Detail label="设备标识" value={item.deviceId} mono />
            <Detail label="令牌" value={item.tokenJti} mono />
            <Detail label="User-Agent" value={item.userAgent} mono wide />
          </dl>
          {item.metadata && Object.keys(item.metadata).length ? (
            <details className="group mt-3 rounded-lg border bg-background">
              <summary className="cursor-pointer list-none px-3 py-2 text-xs text-muted-foreground select-none hover:text-foreground">
                原始信息
              </summary>
              <div className="border-t px-3 py-2">
                <ValueTree value={item.metadata} />
              </div>
            </details>
          ) : null}
        </div>
      ) : null}
    </li>
  );
}

function Detail({ label, value, mono, wide }: { label: string; value?: ReactNode; mono?: boolean; wide?: boolean }) {
  const empty = value === undefined || value === null || value === "";
  return (
    <div className={cn("min-w-0", wide && "sm:col-span-2 xl:col-span-3")}>
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className={cn("mt-0.5 break-all", mono && !empty && "font-mono text-[11px]", empty && "text-muted-foreground")}>
        {empty ? "未记录" : value}
      </dd>
    </div>
  );
}

// ── 会话事件 ──────────────────────────

type EventTone = { icon: ComponentType<{ className?: string }>; className: string };

const EVENT_TONE: Record<string, EventTone> = {
  issued: { icon: KeyRound, className: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" },
  refreshed: { icon: RefreshCw, className: "bg-sky-500/10 text-sky-600 dark:text-sky-400" },
  logout: { icon: LogOut, className: "bg-muted text-muted-foreground" },
  expired: { icon: TimerOff, className: "bg-muted text-muted-foreground" },
  replaced: { icon: RefreshCw, className: "bg-muted text-muted-foreground" },
  password_changed: { icon: KeySquare, className: "bg-amber-500/10 text-amber-600 dark:text-amber-400" },
  password_reset: { icon: KeySquare, className: "bg-amber-500/10 text-amber-600 dark:text-amber-400" }
};

const REVOKE_TONE: EventTone = { icon: ShieldX, className: "bg-red-500/10 text-red-600 dark:text-red-400" };

function eventTone(type: string): EventTone {
  return EVENT_TONE[type] ?? (type.startsWith("revoked") ? REVOKE_TONE : { icon: FileClock, className: "bg-muted text-muted-foreground" });
}

/** 元数据里值得直接显示的几项，其余放进「详情」 */
const EVENT_HIGHLIGHTS: Array<{ key: string; label: string; mono?: boolean }> = [
  { key: "ip", label: "IP", mono: true },
  { key: "provider", label: "方式" },
  { key: "device", label: "设备" },
  { key: "reason", label: "原因" },
  { key: "operator", label: "操作人" }
];

const EVENT_FILTERS = [{ value: "all", label: "全部事件" }, ...Object.entries(SESSION_EVENT_LABEL).map(([value, label]) => ({ value, label }))];

function SessionEvents({ appKey, userId }: { appKey: string; userId: number }) {
  const [eventType, setEventType] = useState("all");
  const [page, setPage] = useState(1);
  const query = useAdminUserSessionAuditsQuery(appKey, userId, {
    eventType: eventType === "all" ? undefined : eventType,
    page,
    limit: EVENT_PAGE_SIZE
  });
  const items = query.data?.items ?? [];
  const totalPages = Math.max(1, query.data?.totalPages ?? 1);

  return (
    <Panel
      title="会话事件"
      icon={<FileClock className="size-4" />}
      action={
        <Select
          value={eventType}
          onValueChange={(value) => {
            setEventType(value);
            setPage(1);
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-[132px] text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {EVENT_FILTERS.map((option) => (
              <SelectItem key={option.value} value={option.value} className="text-xs">
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      }
      bodyClassName="p-0 sm:px-0"
    >
      {query.isLoading ? (
        <ListSkeleton rows={4} />
      ) : items.length === 0 ? (
        <EmptyState icon={FileClock} text={eventType === "all" ? "暂无会话事件" : "没有这类事件"} />
      ) : (
        <ol className={cn("px-4 py-4 sm:px-5", query.isFetching && "opacity-70 transition-opacity")}>
          {items.map((event, i) => (
            <EventItem key={event.id} event={event} last={i === items.length - 1} />
          ))}
        </ol>
      )}
      {totalPages > 1 ? <Pager page={page} totalPages={totalPages} fetching={query.isFetching} onChange={setPage} /> : null}
    </Panel>
  );
}

function EventItem({ event, last }: { event: UserSessionAuditRecord; last: boolean }) {
  const tone = eventTone(event.eventType);
  const Icon = tone.icon;
  const metadata = event.metadata ?? {};
  const highlights = EVENT_HIGHLIGHTS.flatMap(({ key, label, mono }) => {
    const value = metadata[key];
    return typeof value === "string" && value.trim() ? [{ key, label, mono, value: value.trim() }] : [];
  });
  const rest = Object.fromEntries(Object.entries(metadata).filter(([key]) => !EVENT_HIGHLIGHTS.some((h) => h.key === key)));

  return (
    <li className="relative flex gap-3 pb-4 last:pb-0">
      {!last ? <span className="absolute top-8 bottom-0 left-[13px] w-px bg-border" aria-hidden /> : null}
      <span className={cn("relative grid size-7 shrink-0 place-items-center rounded-full ring-4 ring-card", tone.className)}>
        <Icon className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-sm font-medium">{SESSION_EVENT_LABEL[event.eventType] ?? event.eventType}</span>
          <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground" title={formatTime(event.createdAt)}>
            {relativeTime(event.createdAt)}
          </span>
        </div>
        {highlights.length || event.tokenJti ? (
          <div className="mt-1 flex min-w-0 flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
            {highlights.map((h) => (
              <span key={h.key} className="min-w-0 truncate">
                {h.label} <span className={cn("text-foreground/80", h.mono && "font-mono")}>{h.value}</span>
              </span>
            ))}
            {event.tokenJti ? (
              <span className="truncate font-mono" title={event.tokenJti}>
                令牌 {event.tokenJti.slice(0, 8)}
              </span>
            ) : null}
          </div>
        ) : null}
        {Object.keys(rest).length ? (
          <details className="mt-1.5">
            <summary className="cursor-pointer list-none text-[11px] text-muted-foreground select-none hover:text-foreground">
              详情
            </summary>
            <div className="mt-1 rounded-lg bg-muted/40 px-2.5 py-1.5">
              <ValueTree value={rest} />
            </div>
          </details>
        ) : null}
      </div>
    </li>
  );
}

// ── 风控评估 ──────────────────────────

/**
 * 风控评估。按 account 查 —— 风控引擎在登录成功前就已经打过分，
 * 那时还没有 userId，account 才是贯穿始终的那个标识。
 */
function UserRiskPanel({ account }: { account?: string }) {
  const query = useRiskAssessmentsQuery(account ? { account, pageSize: 10 } : undefined);
  const items = query.data?.items ?? [];

  return (
    <Panel
      title="风控评估"
      icon={<Radar className="size-4" />}
      action={
        <Button asChild size="sm" variant="ghost" className="h-7 text-xs">
          <Link href="/risk-control?tab=assessments">
            风控中心
            <ArrowUpRight className="size-3" />
          </Link>
        </Button>
      }
      bodyClassName="p-0 sm:px-0"
    >
      {!account ? (
        <EmptyState icon={Radar} text="无账号标识" />
      ) : query.isLoading ? (
        <ListSkeleton rows={4} />
      ) : items.length === 0 ? (
        <EmptyState icon={ShieldCheck} text="暂无评估记录" />
      ) : (
        <ul className="divide-y">
          {items.map((item) => {
            const score = Math.max(0, Math.min(100, item.totalScore));
            const color = LEVEL_COLORS[item.riskLevel]?.light ?? "#a1a1aa";
            return (
              <li key={item.id} className="flex gap-3 px-4 py-3 sm:px-5">
                <ScoreRing score={item.totalScore} ratio={score / 100} color={color} />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <SceneBadge scene={item.scene} />
                    <LevelBadge level={item.riskLevel} />
                    <ActionBadge action={item.action} />
                    {item.reviewed ? (
                      <Badge variant="outline" size="sm">
                        已复核
                      </Badge>
                    ) : null}
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
                    <span className="font-mono">{textValue(item.ip)}</span>
                    {item.country ? <span>{item.country}</span> : null}
                    <span title={formatTime(item.createdAt)}>{relativeTime(item.createdAt)}</span>
                  </div>
                  {item.matchedRules?.length ? (
                    <div className="mt-1.5 flex flex-wrap gap-1">
                      {item.matchedRules.slice(0, 4).map((rule, i) => (
                        <span key={`${rule.ruleName}-${i}`} className="max-w-full truncate rounded-md bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
                          {rule.reason || rule.ruleName}
                        </span>
                      ))}
                      {item.matchedRules.length > 4 ? (
                        <span className="px-1 py-0.5 text-[11px] text-muted-foreground">另 {item.matchedRules.length - 4} 条</span>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Panel>
  );
}

/** 风险分环：分值写在中间，弧长按 0-100 分归一，颜色取风险等级色。 */
function ScoreRing({ score, ratio, color }: { score: number; ratio: number; color: string }) {
  const r = 15;
  const c = 2 * Math.PI * r;
  return (
    <div className="relative grid size-10 shrink-0 place-items-center">
      <svg viewBox="0 0 36 36" className="absolute inset-0 -rotate-90">
        <circle cx="18" cy="18" r={r} fill="none" strokeWidth="3" className="stroke-muted" />
        <circle cx="18" cy="18" r={r} fill="none" strokeWidth="3" stroke={color} strokeLinecap="round" strokeDasharray={`${c * ratio} ${c}`} />
      </svg>
      <span className="text-xs font-semibold tabular-nums">{score}</span>
    </div>
  );
}

// ── 公共小件 ──────────────────────────

function FilterGroup<T extends string>({
  value,
  options,
  onChange
}: {
  value: T;
  options: Array<{ value: T; label: string }>;
  onChange: (value: T) => void;
}) {
  return (
    <div role="radiogroup" className="inline-flex rounded-lg border bg-muted/40 p-0.5">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={value === option.value}
          onClick={() => onChange(option.value)}
          className={cn(
            "h-7 rounded-md px-2.5 text-xs transition-colors",
            value === option.value ? "bg-background font-medium shadow-sm" : "text-muted-foreground hover:text-foreground"
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function Pager({
  page,
  totalPages,
  fetching,
  onChange
}: {
  page: number;
  totalPages: number;
  fetching: boolean;
  onChange: (page: number) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 border-t px-4 py-2.5 text-xs text-muted-foreground sm:px-5">
      <span className="tabular-nums">
        第 {page} / {totalPages} 页
      </span>
      <div className="flex gap-1.5">
        <Button size="icon" variant="outline" className="size-7" aria-label="上一页" disabled={page <= 1 || fetching} onClick={() => onChange(page - 1)}>
          <ChevronLeft className="size-3.5" />
        </Button>
        <Button
          size="icon"
          variant="outline"
          className="size-7"
          aria-label="下一页"
          disabled={page >= totalPages || fetching}
          onClick={() => onChange(page + 1)}
        >
          <ChevronRight className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}

function ListSkeleton({ rows }: { rows: number }) {
  return (
    <div className="divide-y">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-3 sm:px-5">
          <Skeleton className="size-8 rounded-lg" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-32" />
            <Skeleton className="h-3 w-56 max-w-full" />
          </div>
          <Skeleton className="h-3 w-10" />
        </div>
      ))}
    </div>
  );
}

function EmptyState({ icon: Icon, text }: { icon: ComponentType<{ className?: string }>; text: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
      <span className="grid size-10 place-items-center rounded-full bg-muted">
        <Icon className="size-4" />
      </span>
      {text}
    </div>
  );
}
