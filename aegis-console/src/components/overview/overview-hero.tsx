"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Activity, ArrowUpRight, CloudSun, Globe2, LayoutGrid, LogIn, Moon, ShieldCheck, Sun, Sunrise, Sunset, Timer, Users } from "lucide-react";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { browserTimeZone, browserUtcOffset, formatClockSeconds, formatWith } from "@/lib/datetime";
import { useIsClient } from "@/lib/use-client-value";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

type Period = "dawn" | "morning" | "noon" | "afternoon" | "evening" | "night";

function getPeriod(hour: number): Period {
  if (hour >= 5 && hour < 8) return "dawn";
  if (hour >= 8 && hour < 11) return "morning";
  if (hour >= 11 && hour < 13) return "noon";
  if (hour >= 13 && hour < 17) return "afternoon";
  if (hour >= 17 && hour < 19) return "evening";
  return "night";
}

const GREETINGS: Record<Period, string> = {
  dawn: "早上好",
  morning: "上午好",
  noon: "中午好",
  afternoon: "下午好",
  evening: "傍晚好",
  night: "晚上好"
};

const PERIOD_ICONS: Record<Period, React.ComponentType<{ className?: string }>> = {
  dawn: Sunrise,
  morning: Sun,
  noon: Sun,
  afternoon: CloudSun,
  evening: Sunset,
  night: Moon
};

/** 对齐到整秒的时钟，避免秒数跳动不均。 */
function useClock() {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    let interval: ReturnType<typeof setInterval> | undefined;
    const align = setTimeout(() => {
      setNow(new Date());
      interval = setInterval(() => setNow(new Date()), 1000);
    }, 1000 - (Date.now() % 1000));
    return () => {
      clearTimeout(align);
      clearInterval(interval);
    };
  }, []);

  return now;
}

/*
 * 本次在线时长。起点存在 sessionStorage：刷新保留，新标签页重新计时；
 * 记录里带着 token，重新登录后自动重置。
 */
const SESSION_KEY = "aegis_session_start";

function readSessionStart(token: string) {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY);
    const stored = raw ? (JSON.parse(raw) as { t: number; k: string }) : null;
    if (stored && stored.k === token) return stored.t;
    const startedAt = Date.now();
    sessionStorage.setItem(SESSION_KEY, JSON.stringify({ t: startedAt, k: token }));
    return startedAt;
  } catch {
    return Date.now();
  }
}

function useOnlineDuration(token: string | null, now: Date) {
  const [start, setStart] = useState<{ token: string; at: number } | null>(null);

  useEffect(() => {
    if (!token) return;
    const at = readSessionStart(token);
    // 外部存储只能在挂载后读取，读一次即可
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setStart({ token, at });
  }, [token]);

  if (!token || !start || start.token !== token) return null;
  const minutes = Math.max(0, Math.floor((now.getTime() - start.at) / 60_000));
  const hours = Math.floor(minutes / 60);
  return hours > 0 ? `${hours} 小时 ${minutes % 60} 分钟` : `${minutes} 分钟`;
}

function formatCount(value: number) {
  if (value >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
  if (value >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
  return value.toLocaleString("zh-CN");
}

/**
 * 概览首屏：问候、浏览器时区时钟与四项平台指标。
 *
 * 纯色底，不再使用着色器背景。时钟下方的 24 格时间轴按浏览器时区标出当前小时，
 * 同时把时区名写在旁边，让「控制台全部按本机时区显示」这件事一眼可见。
 */
export function OverviewHero() {
  const operator = useAuthStore((s) => s.operator);
  const accessToken = useAuthStore((s) => s.accessToken);
  const dashboard = useAdminDashboardQuery();
  const stats = dashboard.data?.stats;
  const isClient = useIsClient();
  const now = useClock();
  const onlineDuration = useOnlineDuration(accessToken, now);

  const hour = now.getHours();
  const period = getPeriod(hour);
  const PeriodIcon = PERIOD_ICONS[period];
  const displayName = operator?.displayName || operator?.account || "管理员";
  const roleLabel = operator?.isSuperAdmin ? "超级管理员" : operator?.role || operator?.assignments?.[0]?.roleKey || "管理员";

  const metrics = [
    { icon: LayoutGrid, label: "应用", value: stats?.totalApps, href: "/apps" },
    { icon: Users, label: "用户", value: stats?.totalUsers, href: "/app-users" },
    { icon: LogIn, label: "今日登录", value: stats?.todayLogins, href: "/reports?tab=login" },
    { icon: Activity, label: "活跃会话", value: stats?.activeSessions, href: "/users?tab=online" }
  ];

  return (
    <section aria-label="平台概览" className="overflow-hidden rounded-xl border bg-card text-card-foreground">
      <div className="flex flex-col gap-6 p-5 md:flex-row md:items-end md:justify-between md:p-6">
        <div className="min-w-0 space-y-3">
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <PeriodIcon className="size-3.5" />
            {isClient ? formatWith(now, { year: "numeric", month: "long", day: "numeric", weekday: "long" }) : <Skeleton className="h-3.5 w-36" />}
          </p>
          <h1 className="truncate text-2xl font-semibold tracking-tight md:text-[28px]">
            {isClient ? GREETINGS[period] : "你好"}，{displayName}
          </h1>
          <div className="flex flex-wrap items-center gap-1.5">
            <HeroChip icon={ShieldCheck}>{roleLabel}</HeroChip>
            {onlineDuration ? <HeroChip icon={Timer}>本次在线 {onlineDuration}</HeroChip> : null}
          </div>
        </div>

        <div className="flex shrink-0 flex-col gap-3 md:items-end">
          {isClient ? (
            <p className="font-data text-4xl leading-none font-semibold tracking-tight tabular-nums md:text-5xl">{formatClockSeconds(now)}</p>
          ) : (
            <Skeleton className="h-10 w-48 md:h-12" />
          )}
          <div className="flex items-center gap-3">
            <DayRail hour={isClient ? hour : -1} />
            <span className="flex items-center gap-1 text-[11px] whitespace-nowrap text-muted-foreground">
              <Globe2 className="size-3" />
              {isClient ? `${browserTimeZone()} ${browserUtcOffset(now)}` : "本地时区"}
            </span>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-2 border-t lg:grid-cols-4">
        {metrics.map((metric, index) => (
          <Link
            key={metric.label}
            href={metric.href}
            className={cn(
              "group/metric flex flex-col gap-2 px-5 py-4 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none md:px-6",
              index % 2 === 1 && "border-l",
              index >= 2 && "border-t lg:border-t-0",
              index === 2 && "lg:border-l"
            )}
          >
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <metric.icon className="size-3.5" />
              {metric.label}
              <ArrowUpRight className="ml-auto size-3.5 opacity-0 transition-opacity group-hover/metric:opacity-70" />
            </span>
            {dashboard.isLoading ? (
              <Skeleton className="h-7 w-16" />
            ) : (
              <span className="font-data text-2xl leading-none font-semibold tabular-nums">
                {typeof metric.value === "number" ? formatCount(metric.value) : "—"}
              </span>
            )}
          </Link>
        ))}
      </div>
    </section>
  );
}

function HeroChip({ icon: Icon, children }: { icon: React.ComponentType<{ className?: string }>; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border bg-muted/50 px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
      <Icon className="size-3" />
      {children}
    </span>
  );
}

/** 24 格日内时间轴：已过的小时浅色，当前小时实色。 */
function DayRail({ hour }: { hour: number }) {
  return (
    <div aria-hidden className="flex items-end gap-[3px]">
      {Array.from({ length: 24 }, (_, index) => (
        <span
          key={index}
          className={cn(
            "w-1 rounded-full transition-colors",
            index % 6 === 0 ? "h-3" : "h-2",
            index === hour ? "bg-foreground" : index < hour ? "bg-foreground/25" : "bg-border"
          )}
        />
      ))}
    </div>
  );
}
