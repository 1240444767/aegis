"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useReducedMotion } from "motion/react";
import { useTheme } from "next-themes";
import { Activity, ArrowUpRight, CloudSun, LayoutGrid, LogIn, Moon, ShieldCheck, Sun, Sunrise, Sunset, Timer, Users } from "lucide-react";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { HeroClock } from "./hero-clock";
import { formatWith } from "@/lib/datetime";
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

// three.js 只在浏览器里加载，也不进首屏主包
const HeroGlobe = dynamic(() => import("./hero-globe").then((mod) => mod.HeroGlobe), { ssr: false });

/** 数值变化时从旧值缓动到新值；减少动态效果时直接跳到目标。 */
function useCountUp(target: number | undefined, disabled: boolean) {
  const [value, setValue] = useState(0);
  const fromRef = useRef(0);

  useEffect(() => {
    if (typeof target !== "number") return;
    const from = fromRef.current;
    if (disabled || from === target) {
      fromRef.current = target;
      const id = requestAnimationFrame(() => setValue(target));
      return () => cancelAnimationFrame(id);
    }
    const started = performance.now();
    let frame = requestAnimationFrame(function step(now) {
      const progress = Math.min(1, (now - started) / 900);
      const eased = 1 - Math.pow(1 - progress, 3);
      setValue(Math.round(from + (target - from) * eased));
      if (progress < 1) frame = requestAnimationFrame(step);
      else fromRef.current = target;
    });
    return () => {
      cancelAnimationFrame(frame);
      fromRef.current = target;
    };
  }, [target, disabled]);

  return typeof target === "number" ? value : undefined;
}

function formatCount(value: number) {
  if (value >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
  if (value >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
  return value.toLocaleString("zh-CN");
}

/**
 * 概览首屏：问候、浏览器时区时钟、实时昼夜的点阵地球与四项平台指标。
 *
 * 时钟下方的 24 格时间轴按浏览器时区标出当前小时，地球上高亮的经度带也是本机时区，
 * 让「控制台全部按本机时区显示」这件事一眼可见。
 */
export function OverviewHero() {
  const operator = useAuthStore((s) => s.operator);
  const accessToken = useAuthStore((s) => s.accessToken);
  const dashboard = useAdminDashboardQuery();
  const stats = dashboard.data?.stats;
  const isClient = useIsClient();
  const now = useClock();
  const onlineDuration = useOnlineDuration(accessToken, now);
  const reducedMotion = Boolean(useReducedMotion());
  const { resolvedTheme } = useTheme();
  const [globeReady, setGlobeReady] = useState(false);
  // 时钟每秒重渲染首屏；回调保持稳定，地球组件（memo）才不会跟着重渲染
  const handleGlobeReady = useCallback(() => setGlobeReady(true), []);

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
    <section aria-label="平台概览" className="relative overflow-hidden rounded-xl border bg-card text-card-foreground">
      <div className="grid md:min-h-[320px] md:grid-cols-[minmax(0,1fr)_minmax(0,46%)]">
        <div className="relative z-10 flex flex-col justify-between gap-8 p-5 md:p-7">
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

          {isClient ? (
            <HeroClock now={now} animate={!reducedMotion} />
          ) : (
            <div className="space-y-4">
              <Skeleton className="h-12 w-60 md:h-[60px]" />
              <Skeleton className="h-6 w-full max-w-[22rem]" />
              <Skeleton className="h-4 w-48" />
            </div>
          )}
        </div>

        <div className="relative h-[280px] md:h-auto">
          {/* 贴图到位前的占位：与地球同位置同大小的圆 */}
          <div
            aria-hidden
            className={cn(
              "pointer-events-none absolute top-1/2 left-1/2 aspect-square h-[78%] max-w-[92%] -translate-x-1/2 -translate-y-[44%] rounded-full bg-muted transition-opacity duration-700",
              globeReady ? "opacity-0" : "animate-pulse opacity-60"
            )}
          />
          {isClient ? (
            <HeroGlobe
              theme={resolvedTheme === "light" ? "light" : "dark"}
              reducedMotion={reducedMotion}
              onReady={handleGlobeReady}
              className="absolute inset-0"
            />
          ) : null}
        </div>
      </div>

      <div className="relative z-10 grid grid-cols-2 border-t bg-card lg:grid-cols-4">
        {metrics.map((metric, index) => (
          <MetricCell
            key={metric.label}
            {...metric}
            loading={dashboard.isLoading}
            reducedMotion={reducedMotion}
            className={cn(
              index % 2 === 1 && "border-l",
              index >= 2 && "border-t lg:border-t-0",
              index === 2 && "lg:border-l"
            )}
          />
        ))}
      </div>
    </section>
  );
}

function MetricCell({
  icon: Icon,
  label,
  value,
  href,
  loading,
  reducedMotion,
  className
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value?: number;
  href: string;
  loading: boolean;
  reducedMotion: boolean;
  className?: string;
}) {
  const shown = useCountUp(value, reducedMotion);
  return (
    <Link
      href={href}
      className={cn(
        "group/metric flex flex-col gap-2 px-5 py-4 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none md:px-7",
        className
      )}
    >
      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
        <ArrowUpRight className="ml-auto size-3.5 -translate-x-1 opacity-0 transition-[opacity,translate] group-hover/metric:translate-x-0 group-hover/metric:opacity-70" />
      </span>
      {loading ? (
        <Skeleton className="h-7 w-16" />
      ) : (
        <span className="font-data text-2xl leading-none font-semibold tabular-nums">{typeof shown === "number" ? formatCount(shown) : "—"}</span>
      )}
    </Link>
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
