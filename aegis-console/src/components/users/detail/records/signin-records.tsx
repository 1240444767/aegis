"use client";

/**
 * 签到记录。
 *
 * 签到日（signDate）是签到策略时区的日历日，后端按 `policy.timezone` 落库与筛选，
 * 所以「今天」「本月」「近 90 天」都按策略时区取，而不是浏览器时区。
 *
 * 指标口径：
 *  - 累计签到：该用户全部签到记录条数（一天至多一条）。
 *  - 连续签到：最近一条记录的连签天数；若最近一次签到早于昨天，连签已中断，记为 0。
 *  - 本月签到：策略时区本月 1 日至今天的记录条数。
 *  - 近 90 天：日历热力格覆盖的区间内的记录条数。
 */

import * as React from "react";
import { CalendarCheck2, CalendarDays, Flame, ShieldAlert, Trophy } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { bonusLabel, buildBonusLabels, sourceLabel } from "@/components/apps/signin-reward/signin-shared";
import { useAdminAppSignInRewardQuery, useAdminToken } from "@/lib/admin-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { listUserSignInRecords, useUserSignInRecordsQuery, useUserSignInSummaryQuery } from "@/lib/user-records-hooks";
import type { AppSignInRecordItem } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  FilterBar,
  FilterSelect,
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  Section,
  StateBlock,
  TimeRangePicker,
  formatInt,
  type Column,
  type CsvColumn,
  type TimeRange
} from "../kit";
import { ExportButton, MetricGrid, exportStamp, usePaging } from "./records-shared";

const RECENT_DAYS = 90;

// ── 策略时区下的日历日 ──

function browserZone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

function dateKeyIn(date: Date, timeZone: string) {
  try {
    return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(date);
  } catch {
    const pad = (value: number) => String(value).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  }
}

function keyToUtc(key: string) {
  const [y, m, d] = key.split("-").map(Number);
  return new Date(Date.UTC(y, (m || 1) - 1, d || 1));
}

function shiftKey(key: string, days: number) {
  const date = keyToUtc(key);
  date.setUTCDate(date.getUTCDate() + days);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}`;
}

/** 周一为 0 */
function weekdayIndex(key: string) {
  return (keyToUtc(key).getUTCDay() + 6) % 7;
}

const PRESET_DAYS: Record<string, number> = { today: 1, "7d": 7, "30d": 30, "90d": 90, "365d": 365 };

function rangeToDates(range: TimeRange, today: string, timeZone: string) {
  if (range.preset === "custom") {
    return {
      dateFrom: range.from ? dateKeyIn(new Date(range.from), timeZone) : undefined,
      dateTo: range.to ? dateKeyIn(new Date(range.to), timeZone) : undefined
    };
  }
  const days = PRESET_DAYS[range.preset];
  if (!days) return { dateFrom: undefined, dateTo: undefined };
  return { dateFrom: shiftKey(today, -(days - 1)), dateTo: today };
}

function isBonus(type?: string) {
  return Boolean(type && type !== "normal");
}

export function SignInRecords({ appKey, userId }: { appKey: string; userId: number }) {
  const token = useAdminToken();
  const policyQuery = useAdminAppSignInRewardQuery(appKey);
  const policy = policyQuery.data?.policy;
  const policyReady = Boolean(policy) || policyQuery.isError;
  const timeZone = policy?.timezone || browserZone();
  const labels = React.useMemo(() => buildBonusLabels(policy), [policy]);

  const today = dateKeyIn(new Date(), timeZone);
  const span = React.useMemo(
    () => (policyReady ? { today, monthStart: `${today.slice(0, 8)}01`, recentStart: shiftKey(today, -(RECENT_DAYS - 1)) } : null),
    [policyReady, today]
  );
  const summaryQuery = useUserSignInSummaryQuery(appKey, userId, span);
  const summary = summaryQuery.data;

  const [range, setRange] = React.useState<TimeRange>({ preset: "all" });
  const [source, setSource] = React.useState("");
  const [bonusType, setBonusType] = React.useState("");
  const paging = usePaging(20);
  const dates = rangeToDates(range, today, timeZone);
  const filters = { source: source || undefined, bonusType: bonusType || undefined, ...dates };
  const listQuery = useUserSignInRecordsQuery(appKey, userId, { ...filters, page: paging.page, limit: paging.limit });
  const [selected, setSelected] = React.useState<AppSignInRecordItem | null>(null);

  const bonusOptions = React.useMemo(() => {
    const keys = new Set<string>(["normal", "compound", "milestone"]);
    for (const rule of policy?.rules ?? []) if (rule.bonusType) keys.add(rule.bonusType);
    for (const m of policy?.milestones ?? []) if (m.bonusType) keys.add(m.bonusType);
    return [...keys].map((key) => ({ value: key, label: bonusLabel(labels, key) }));
  }, [policy, labels]);

  const unfiltered =
    summary?.unfiltered || Boolean(listQuery.data?.items?.some((item) => item.userId !== userId));

  // ── 指标 ──
  const latest = summary?.latest;
  const yesterday = shiftKey(today, -1);
  const streakAlive = latest ? latest.signDate === today || latest.signDate === yesterday : false;
  const streak = streakAlive ? (latest?.consecutiveDays ?? 0) : 0;
  const streakSub = !latest
    ? "暂无签到"
    : latest.signDate === today
      ? "今日已签到"
      : streakAlive
        ? "今日未签到，今天签到可延续"
        : `已中断，最后签到 ${latest.signDate}`;

  const recentMap = React.useMemo(() => {
    const map = new Map<string, AppSignInRecordItem>();
    for (const item of summary?.recent ?? []) map.set(item.signDate, item);
    return map;
  }, [summary?.recent]);
  const recentCount = recentMap.size;
  const weekly = React.useMemo(() => {
    if (!span) return [];
    const weeks: number[] = [];
    for (let start = 0; start < RECENT_DAYS; start += 7) {
      let count = 0;
      for (let i = start; i < Math.min(start + 7, RECENT_DAYS); i += 1) {
        if (recentMap.has(shiftKey(span.recentStart, i))) count += 1;
      }
      weeks.push(count);
    }
    return weeks;
  }, [recentMap, span]);

  const filtered = range.preset !== "all" || Boolean(source || bonusType);

  const columns: Array<Column<AppSignInRecordItem>> = [
    {
      id: "date",
      header: "签到日",
      cell: (row) => (
        <div className="flex flex-col">
          <span className="font-medium tabular-nums">{row.signDate}</span>
          <span className="text-xs text-muted-foreground tabular-nums">{formatDateTime(row.signedAt)}</span>
        </div>
      )
    },
    {
      id: "streak",
      header: "连签",
      align: "right",
      cell: (row) => <span className="tabular-nums">{formatInt(row.consecutiveDays)} 天</span>
    },
    {
      id: "integral",
      header: "积分",
      align: "right",
      cell: (row) => <span className="font-medium text-emerald-600 tabular-nums dark:text-emerald-400">+{formatInt(row.integralReward)}</span>
    },
    {
      id: "experience",
      header: "经验",
      align: "right",
      hideBelow: "sm",
      cell: (row) => <span className="tabular-nums">+{formatInt(row.experienceReward)}</span>
    },
    {
      id: "bonus",
      header: "奖励类型",
      cell: (row) => (
        <div className="flex flex-wrap items-center gap-1">
          <Badge variant={isBonus(row.bonusType) ? "info" : "outline"} size="sm">
            {bonusLabel(labels, row.bonusType)}
          </Badge>
          {row.rewardMultiplier && row.rewardMultiplier !== 1 ? (
            <span className="text-xs text-muted-foreground tabular-nums">×{row.rewardMultiplier}</span>
          ) : null}
        </div>
      )
    },
    {
      id: "source",
      header: "来源",
      hideBelow: "md",
      cell: (row) => (
        <Badge variant={row.signInSource === "auto" ? "secondary" : "outline"} size="sm">
          {sourceLabel(row.signInSource)}
        </Badge>
      )
    },
    {
      id: "ip",
      header: "IP 与归属地",
      hideBelow: "lg",
      cell: (row) => (
        <div className="flex min-w-0 flex-col">
          <span className="font-mono text-xs">{row.ipAddress || "—"}</span>
          {row.location ? <span className="truncate text-xs text-muted-foreground">{row.location}</span> : null}
        </div>
      )
    }
  ];

  const csvColumns: Array<CsvColumn<AppSignInRecordItem>> = [
    { header: "签到日", value: (row) => row.signDate },
    { header: "签到时间", value: (row) => formatDateTime(row.signedAt, "") },
    { header: "连签天数", value: (row) => row.consecutiveDays },
    { header: "积分", value: (row) => row.integralReward },
    { header: "经验", value: (row) => row.experienceReward },
    { header: "倍率", value: (row) => row.rewardMultiplier },
    { header: "奖励类型", value: (row) => bonusLabel(labels, row.bonusType) },
    { header: "奖励说明", value: (row) => row.bonusDescription },
    { header: "来源", value: (row) => sourceLabel(row.signInSource) },
    { header: "IP", value: (row) => row.ipAddress },
    { header: "归属地", value: (row) => row.location },
    { header: "设备", value: (row) => row.deviceInfo }
  ];

  if (unfiltered) {
    return (
      <Section title="签到记录" icon={CalendarCheck2} tone="success">
        <StateBlock
          kind="error"
          icon={ShieldAlert}
          title="签到明细暂不支持按用户筛选"
          description="接口返回了其他用户的签到记录，为避免误导，这里不展示数据。请确认后端签到明细接口已支持 userId 参数。"
        />
      </Section>
    );
  }

  const summaryLoading = !span || summaryQuery.isLoading;

  return (
    <div className="flex flex-col gap-5">
      <MetricGrid>
        <MetricCard
          label="累计签到"
          icon={Trophy}
          tone="primary"
          loading={summaryLoading}
          value={`${formatInt(summary?.total ?? 0)} 天`}
          sub={latest ? `最近 ${formatRelative(latest.signedAt)}` : "暂无签到"}
        />
        <MetricCard label="连续签到" icon={Flame} tone="warning" loading={summaryLoading} value={`${formatInt(streak)} 天`} sub={streakSub} />
        <MetricCard
          label="本月签到"
          icon={CalendarDays}
          tone="success"
          loading={summaryLoading}
          value={`${formatInt(summary?.month ?? 0)} 次`}
          sub={`本月已过 ${Number(today.slice(8, 10))} 天`}
        />
        <MetricCard
          label={`近 ${RECENT_DAYS} 天`}
          icon={CalendarCheck2}
          tone="info"
          loading={summaryLoading}
          value={`${formatInt(recentCount)} 天`}
          sub={`签到率 ${((recentCount / RECENT_DAYS) * 100).toFixed(0)}%`}
          spark={weekly}
        />
      </MetricGrid>

      <Section
        title={`近 ${RECENT_DAYS} 天签到`}
        description={`按签到策略时区（${timeZone}）计日，深色为命中加成奖励的签到`}
        icon={CalendarDays}
        tone="success"
      >
        {summaryQuery.isError ? (
          <StateBlock kind="error" compact description={summaryQuery.error instanceof Error ? summaryQuery.error.message : undefined} onRetry={() => summaryQuery.refetch()} />
        ) : summaryLoading || !span ? (
          <Skeleton className="h-[124px] w-full max-w-[260px]" />
        ) : (
          <SignInCalendar start={span.recentStart} today={today} records={recentMap} labels={labels} />
        )}
      </Section>

      <Section
        flush
        title="签到明细"
        description="每天至多一条，按签到日倒序"
        icon={CalendarCheck2}
        tone="success"
        actions={
          <ExportButton
            filename={`签到记录-${userId}-${exportStamp()}`}
            columns={csvColumns}
            disabled={!token || !listQuery.data?.total}
            fetchPage={(page, limit) => listUserSignInRecords(token as string, appKey, userId, { ...filters, page, limit })}
          />
        }
        toolbar={
          <FilterBar>
            <TimeRangePicker
              value={range}
              onChange={(next) => {
                setRange(next);
                paging.reset();
              }}
            />
            <FilterSelect
              label="来源"
              value={source}
              onChange={(next) => {
                setSource(next);
                paging.reset();
              }}
              options={[
                { value: "manual", label: "手动签到" },
                { value: "auto", label: "自动签到" }
              ]}
            />
            <FilterSelect
              label="奖励"
              value={bonusType}
              onChange={(next) => {
                setBonusType(next);
                paging.reset();
              }}
              options={bonusOptions}
            />
            <ResetButton
              visible={filtered}
              onClick={() => {
                setRange({ preset: "all" });
                setSource("");
                setBonusType("");
                paging.reset();
              }}
            />
          </FilterBar>
        }
        footer={
          listQuery.data?.total ? (
            <Pager page={paging.page} limit={paging.limit} total={listQuery.data.total} onPage={paging.setPage} onLimit={paging.setLimit} />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={listQuery.data?.items}
          rowKey={(row) => row.id}
          loading={listQuery.isLoading}
          fetching={listQuery.isFetching}
          error={listQuery.error}
          onRetry={() => listQuery.refetch()}
          onRowClick={setSelected}
          minWidth={640}
          empty={<StateBlock title={filtered ? "没有符合条件的签到" : "暂无签到记录"} />}
        />
      </Section>

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => (!open ? setSelected(null) : undefined)}
        title="签到详情"
        description={selected ? `签到日 ${selected.signDate}` : undefined}
        hero={
          selected ? (
            <div className="grid grid-cols-2 gap-3">
              <div className="rounded-lg border px-3 py-2.5">
                <div className="text-xs text-muted-foreground">积分</div>
                <div className="text-xl font-semibold text-emerald-600 tabular-nums dark:text-emerald-400">
                  +{formatInt(selected.integralReward)}
                </div>
              </div>
              <div className="rounded-lg border px-3 py-2.5">
                <div className="text-xs text-muted-foreground">经验</div>
                <div className="text-xl font-semibold tabular-nums">+{formatInt(selected.experienceReward)}</div>
              </div>
            </div>
          ) : null
        }
        fields={
          selected
            ? [
                { label: "记录 ID", value: String(selected.id), mono: true, copy: String(selected.id) },
                { label: "签到日", value: selected.signDate },
                { label: "签到时间", value: formatDateTime(selected.signedAt) },
                { label: "连签天数", value: `${formatInt(selected.consecutiveDays)} 天` },
                { label: "奖励类型", value: bonusLabel(labels, selected.bonusType) },
                { label: "奖励倍率", value: `×${selected.rewardMultiplier}` },
                { label: "奖励说明", value: selected.bonusDescription, wide: true },
                { label: "来源", value: sourceLabel(selected.signInSource) },
                { label: "IP", value: selected.ipAddress, mono: true, copy: selected.ipAddress || null },
                { label: "归属地", value: selected.location },
                { label: "设备", value: selected.deviceInfo, wide: true },
                { label: "写入时间", value: formatDateTime(selected.createdAt) }
              ]
            : undefined
        }
        raw={selected}
      />
    </div>
  );
}

/** 近 90 天签到格：一列一周（周一在上），纯 CSS 网格 */
function SignInCalendar({
  start,
  today,
  records,
  labels
}: {
  start: string;
  today: string;
  records: Map<string, AppSignInRecordItem>;
  labels: Record<string, string>;
}) {
  const lead = weekdayIndex(start);
  const cells: Array<string | null> = [...Array.from({ length: lead }, () => null)];
  for (let i = 0; i < RECENT_DAYS; i += 1) cells.push(shiftKey(start, i));
  const weeks = Math.ceil(cells.length / 7);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex gap-2 overflow-x-auto pb-1">
        <div className="grid shrink-0 grid-rows-7 gap-[3px] text-xs leading-[14px] text-muted-foreground">
          {["一", "", "三", "", "五", "", "日"].map((label, index) => (
            <span key={index} className="h-3.5">
              {label}
            </span>
          ))}
        </div>
        <div className="grid grid-flow-col grid-rows-7 gap-[3px]" style={{ gridTemplateColumns: `repeat(${weeks}, 14px)` }}>
          {cells.map((key, index) => {
            if (!key) return <span key={`pad-${index}`} className="size-3.5" />;
            const record = records.get(key);
            return (
              <Tooltip key={key}>
                <TooltipTrigger asChild>
                  <span
                    className={cn(
                      "size-3.5 rounded-[3px]",
                      !record && "bg-muted",
                      record && !isBonus(record.bonusType) && "bg-emerald-500/50 dark:bg-emerald-400/45",
                      record && isBonus(record.bonusType) && "bg-emerald-600 dark:bg-emerald-400",
                      key === today && "ring-1 ring-foreground/60 ring-offset-1 ring-offset-background"
                    )}
                  />
                </TooltipTrigger>
                <TooltipContent side="top" className="text-xs">
                  {record
                    ? `${key} 已签到，连签 ${record.consecutiveDays} 天，积分 +${record.integralReward}，${bonusLabel(labels, record.bonusType)}`
                    : `${key} 未签到`}
                </TooltipContent>
              </Tooltip>
            );
          })}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <span className="size-3 rounded-[3px] bg-muted" />
          未签到
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="size-3 rounded-[3px] bg-emerald-500/50 dark:bg-emerald-400/45" />
          普通签到
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="size-3 rounded-[3px] bg-emerald-600 dark:bg-emerald-400" />
          加成签到
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="size-3 rounded-[3px] ring-1 ring-foreground/60" />
          今天
        </span>
      </div>
    </div>
  );
}
