"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import type { DateRange } from "react-day-picker";
import { CalendarDays, ChevronDown, Download, LayoutDashboard, Loader2, RefreshCw, ShoppingBag, Smartphone, Users } from "lucide-react";
import { useIsFetching, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { EmptyState } from "@/components/ui/data-state";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ReportPalette } from "@/components/reports/report-kit";
import { OverviewPage, UsersPage } from "@/components/reports/page-users";
import { ChannelsPage } from "@/components/reports/page-channels";
import { OperationsPage } from "@/components/reports/page-operations";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useReportExportMutation } from "@/lib/report-hooks";
import type { ReportGranularity, ReportKind, ReportParams } from "@/lib/api/reports";
import { browserTimeZone, localDateKey } from "@/lib/datetime";

/**
 * 报表分析中心：4 个页面，每页把相关报表按分区纵向排好，不再一个报表一个标签。
 *
 * 筛选（应用、日期、粒度、当前页）都放在地址栏里，复制链接给别人看到的是同一份报表。
 * 日期以 YYYY-MM-DD 加浏览器时区传给后端，「哪一天」由后端按这个时区切。
 */

const PAGES = [
  {
    key: "overview",
    label: "总览",
    icon: LayoutDashboard,
    sections: [] as Array<[string, string]>,
    exports: ["overview"] as ReportKind[]
  },
  {
    key: "users",
    label: "用户",
    icon: Users,
    sections: [["growth", "增长"], ["active", "活跃"], ["retention", "留存"], ["funnel", "漏斗"]] as Array<[string, string]>,
    exports: ["registration", "active", "retention", "funnel"] as ReportKind[]
  },
  {
    key: "channels",
    label: "渠道与终端",
    icon: Smartphone,
    sections: [["login", "登录"], ["source", "渠道来源"], ["device", "设备"], ["region", "地区"]] as Array<[string, string]>,
    exports: ["login", "channel", "device", "region"] as ReportKind[]
  },
  {
    key: "operations",
    label: "营收与运营",
    icon: ShoppingBag,
    sections: [["payment", "支付"], ["engagement", "互动"], ["lottery", "抽奖"], ["notification", "通知"], ["risk", "风控"]] as Array<[string, string]>,
    exports: ["payment", "engagement", "activity", "notification", "risk"] as ReportKind[]
  }
] as const;

const EXPORT_LABEL: Record<ReportKind, string> = {
  overview: "总览",
  registration: "用户增长",
  active: "活跃",
  retention: "留存",
  funnel: "转化漏斗",
  login: "登录",
  channel: "渠道来源",
  device: "设备",
  region: "地区",
  payment: "支付",
  engagement: "互动",
  activity: "抽奖活动",
  notification: "通知",
  risk: "风控"
};

const PRESETS: Array<{ key: string; label: string; range: () => [Date, Date] }> = [
  { key: "today", label: "今天", range: () => [daysAgo(0), daysAgo(0)] },
  { key: "yesterday", label: "昨天", range: () => [daysAgo(1), daysAgo(1)] },
  { key: "7", label: "近 7 天", range: () => [daysAgo(6), daysAgo(0)] },
  { key: "14", label: "近 14 天", range: () => [daysAgo(13), daysAgo(0)] },
  { key: "30", label: "近 30 天", range: () => [daysAgo(29), daysAgo(0)] },
  { key: "90", label: "近 90 天", range: () => [daysAgo(89), daysAgo(0)] },
  {
    key: "month",
    label: "本月",
    range: () => {
      const now = new Date();
      return [new Date(now.getFullYear(), now.getMonth(), 1), daysAgo(0)];
    }
  },
  {
    key: "lastMonth",
    label: "上月",
    range: () => {
      const now = new Date();
      return [new Date(now.getFullYear(), now.getMonth() - 1, 1), new Date(now.getFullYear(), now.getMonth(), 0)];
    }
  },
  {
    key: "year",
    label: "今年",
    range: () => [new Date(new Date().getFullYear(), 0, 1), daysAgo(0)]
  }
];

function daysAgo(n: number) {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  d.setDate(d.getDate() - n);
  return d;
}

function parseDay(value: string | null) {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return undefined;
  const [y, m, d] = value.split("-").map(Number);
  return new Date(y, m - 1, d);
}

function spanDays(from: Date, to: Date) {
  return Math.round((to.getTime() - from.getTime()) / 86_400_000) + 1;
}

function ReportsPageInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const queryClient = useQueryClient();
  const fetching = useIsFetching({ queryKey: ["report"] });
  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const exportMutation = useReportExportMutation();
  const [rangeOpen, setRangeOpen] = useState(false);

  const tab = PAGES.some((p) => p.key === searchParams.get("tab")) ? (searchParams.get("tab") as string) : "overview";
  const appKey = searchParams.get("app") || apps[0]?.appKey || (apps[0] ? String(apps[0].id) : "");
  const from = parseDay(searchParams.get("from")) ?? daysAgo(29);
  const to = parseDay(searchParams.get("to")) ?? daysAgo(0);
  const presetKey = searchParams.get("from") ? searchParams.get("preset") : "30";
  const granularity = (["day", "week", "month"].includes(searchParams.get("g") ?? "") ? searchParams.get("g") : "day") as ReportGranularity;
  const days = spanDays(from, to);

  const update = (patch: Record<string, string | undefined>) => {
    const next = new URLSearchParams(searchParams.toString());
    Object.entries(patch).forEach(([k, v]) => (v ? next.set(k, v) : next.delete(k)));
    router.replace(`/reports?${next.toString()}`, { scroll: false });
  };

  // 跨度很长时按日画会挤成一团：超过 120 天自动切到周，超过 366 天由后端拒绝
  useEffect(() => {
    if (granularity === "day" && days > 120) update({ g: "week" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days, granularity]);

  const fromKey = localDateKey(from);
  const toKey = localDateKey(to);
  const params: ReportParams | undefined = appKey ? { start: fromKey, end: toKey, tz: browserTimeZone(), granularity } : undefined;

  const setRange = (a: Date, b: Date, preset?: string) => {
    update({ from: localDateKey(a), to: localDateKey(b), preset });
  };

  const page = PAGES.find((p) => p.key === tab) ?? PAGES[0];
  const rangeLabel = presetKey ? PRESETS.find((p) => p.key === presetKey)?.label : undefined;

  const runExport = (type: ReportKind | "all") => {
    if (!params) return;
    exportMutation.mutate(
      { appKey, type, params },
      {
        onSuccess: (name) => toast.success(`已导出 ${name}`),
        onError: (err) => toast.error(err instanceof Error ? err.message : "导出失败")
      }
    );
  };

  return (
    <div className="report-scope page-stack">
      <ReportPalette />
      <SectionHeading
        eyebrow="控制台"
        title="报表分析中心"
        action={
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              className="h-8 text-xs"
              disabled={!params}
              onClick={() => queryClient.invalidateQueries({ queryKey: ["report"] })}
            >
              <RefreshCw className={fetching ? "size-3.5 animate-spin" : "size-3.5"} />
              刷新
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="sm" className="h-8 text-xs" disabled={!params || exportMutation.isPending}>
                  {exportMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}
                  导出
                  <ChevronDown className="size-3 opacity-60" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuLabel className="text-xs text-muted-foreground">{page.label}</DropdownMenuLabel>
                {page.exports.map((kind) => (
                  <DropdownMenuItem key={kind} className="text-xs" onSelect={() => runExport(kind)}>
                    {EXPORT_LABEL[kind]}
                  </DropdownMenuItem>
                ))}
                <DropdownMenuSeparator />
                <DropdownMenuItem className="text-xs" onSelect={() => runExport("all")}>
                  全部报表（一个文件）
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <Select value={appKey} onValueChange={(v) => update({ app: v })}>
          <SelectTrigger className="h-8 w-48 text-xs">
            <SelectValue placeholder={appsQuery.isLoading ? "加载应用..." : "选择应用"} />
          </SelectTrigger>
          <SelectContent>
            {apps.map((a) => (
              <SelectItem key={a.appKey || a.id} value={a.appKey || String(a.id)}>
                {a.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Popover open={rangeOpen} onOpenChange={setRangeOpen}>
          <PopoverTrigger asChild>
            <Button size="sm" variant="outline" className="h-8 gap-1.5 text-xs">
              <CalendarDays className="size-3.5" />
              {rangeLabel ? <span>{rangeLabel}</span> : null}
              <span className={rangeLabel ? "text-muted-foreground tabular-nums" : "tabular-nums"}>
                {localDateKey(from)} 至 {localDateKey(to)}
              </span>
              <ChevronDown className="size-3 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-auto p-0">
            <div className="flex flex-col sm:flex-row">
              <div className="flex flex-row flex-wrap gap-1 border-b p-2 sm:w-28 sm:flex-col sm:border-r sm:border-b-0">
                {PRESETS.map((preset) => (
                  <Button
                    key={preset.key}
                    size="sm"
                    variant={presetKey === preset.key ? "secondary" : "ghost"}
                    className="h-7 justify-start text-xs"
                    onClick={() => {
                      const [a, b] = preset.range();
                      setRange(a, b, preset.key);
                      setRangeOpen(false);
                    }}
                  >
                    {preset.label}
                  </Button>
                ))}
              </div>
              <Calendar
                mode="range"
                numberOfMonths={2}
                defaultMonth={from}
                disabled={{ after: new Date() }}
                selected={{ from, to } satisfies DateRange}
                onSelect={(range) => {
                  if (!range?.from) return;
                  setRange(range.from, range.to ?? range.from);
                }}
              />
            </div>
          </PopoverContent>
        </Popover>

        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={granularity}
          onValueChange={(v) => v && update({ g: v })}
        >
          <ToggleGroupItem value="day" className="h-8 px-3 text-xs" disabled={days > 120}>
            按日
          </ToggleGroupItem>
          <ToggleGroupItem value="week" className="h-8 px-3 text-xs">
            按周
          </ToggleGroupItem>
          <ToggleGroupItem value="month" className="h-8 px-3 text-xs">
            按月
          </ToggleGroupItem>
        </ToggleGroup>

        <span className="text-xs text-muted-foreground">
          共 {days} 天，按 {browserTimeZone()} 分日，与之前 {days} 天对比
        </span>
      </div>

      <Tabs value={tab} onValueChange={(v) => update({ tab: v })} className="gap-4">
        <div className="sticky top-0 z-20 -mx-1 flex flex-wrap items-center justify-between gap-2 bg-background/85 px-1 py-2 backdrop-blur">
          <TabsList className="h-9">
            {PAGES.map((p) => (
              <TabsTrigger key={p.key} value={p.key} className="px-3 text-xs">
                <p.icon className="size-3.5" />
                {p.label}
              </TabsTrigger>
            ))}
          </TabsList>
          {page.sections.length > 0 ? (
            <nav className="flex flex-wrap items-center gap-1">
              {page.sections.map(([id, label]) => (
                <button
                  key={id}
                  type="button"
                  onClick={() => document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" })}
                  className="rounded-full border px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                >
                  {label}
                </button>
              ))}
            </nav>
          ) : null}
        </div>

        {!params ? (
          <EmptyState title={appsQuery.isLoading ? "正在加载应用" : "请先选择应用"} />
        ) : (
          <>
            <TabsContent value="overview">
              <OverviewPage appKey={appKey} params={params} />
            </TabsContent>
            <TabsContent value="users">
              <UsersPage appKey={appKey} params={params} />
            </TabsContent>
            <TabsContent value="channels">
              <ChannelsPage appKey={appKey} params={params} />
            </TabsContent>
            <TabsContent value="operations">
              <OperationsPage appKey={appKey} params={params} />
            </TabsContent>
          </>
        )}
      </Tabs>
    </div>
  );
}

export default function ReportsPage() {
  return (
    <Suspense>
      <ReportsPageInner />
    </Suspense>
  );
}
