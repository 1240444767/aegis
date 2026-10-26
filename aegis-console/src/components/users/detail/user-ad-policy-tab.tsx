"use client";

import { useState } from "react";
import {
  AlertTriangle,
  BadgeCheck,
  Crown,
  Download,
  Eye,
  FileCheck2,
  History,
  Loader2,
  MonitorSmartphone,
  MousePointerClick,
  ShieldCheck,
  ShieldOff
} from "lucide-react";
import { toast } from "sonner";
import {
  ConsentBadge,
  ModeBadge,
  SPLASH_STATUS_OPTIONS,
  SplashStatusBadge,
  consentSourceLabel,
  formatMillis
} from "@/components/apps/ad-policy/ad-policy-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdConsentLogsQuery, useSplashAdEventsQuery, useUserAdProfileQuery } from "@/lib/ad-policy-hooks";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  listAdConsentLogs,
  listSplashAdEvents,
  type AdConsentLog,
  type SplashAdEvent,
  type SplashAdStatus,
  type UserAdProfile
} from "@/lib/api/ad-policy";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import {
  FilterBar,
  FilterSelect,
  InfoGrid,
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  TimeRangePicker,
  ToneIcon,
  collectPages,
  downloadCsv,
  errorMessage,
  formatInt,
  rangeToParams,
  useDebouncedValue,
  type Column,
  type TimeRange,
  type Tone
} from "./kit";

const SPLASH_LABEL: Record<string, string> = Object.fromEntries(SPLASH_STATUS_OPTIONS.map((item) => [item.value, item.label]));

/**
 * 广告服务：这个用户对广告服务的选择、因此得到的服务模式，以及开屏广告的展示记录。
 *
 *   当前判定   选择、条款版本、会员免除与最终的服务模式（出自后端同一处判定，前端只展示结论）
 *   开屏统计   累计与今日
 *   同意记录   每一次同意与撤回：在哪里、哪台设备、哪个 IP（分页、按设备搜索、导出）
 *   开屏记录   每一次开屏的结局（按结果、时间筛选，按设备 / 错误码搜索，导出）
 */
export function UserAdPolicyTab({ appKey, userId }: { appKey: string; userId: number }) {
  const profileQuery = useUserAdProfileQuery(appKey, userId);
  const profile = profileQuery.data;

  return (
    <div className="flex flex-col gap-5">
      {profileQuery.isLoading ? (
        <ProfileSkeleton />
      ) : !profile ? (
        <Section title="当前判定" icon={ShieldCheck} tone="primary">
          <StateBlock
            kind="error"
            description={errorMessage(profileQuery.error, "未能读取广告服务状态")}
            onRetry={() => void profileQuery.refetch()}
          />
        </Section>
      ) : (
        <ProfileOverview profile={profile} />
      )}

      <ConsentLogSection appKey={appKey} userId={userId} />
      <SplashSection appKey={appKey} userId={userId} />
    </div>
  );
}

function ProfileSkeleton() {
  return (
    <>
      <Section title="当前判定" icon={ShieldCheck} tone="primary">
        <div className="flex flex-col gap-5">
          <Skeleton className="h-16 w-full rounded-xl" />
          <div className="grid gap-4 sm:grid-cols-3">
            {[0, 1, 2, 3, 4, 5].map((index) => (
              <div key={index} className="flex flex-col gap-1.5">
                <Skeleton className="h-3 w-14" />
                <Skeleton className="h-4 w-28" />
              </div>
            ))}
          </div>
        </div>
      </Section>
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        {[0, 1, 2, 3].map((index) => (
          <MetricCard key={index} label={<Skeleton className="h-3 w-16" />} value="" loading />
        ))}
      </div>
    </>
  );
}

/** 一句话解释为什么得到这个服务模式。 */
function explainMode(profile: UserAdProfile, outdated: boolean): { title: string; detail: string; tone: Tone } {
  if (!profile.configured) {
    return { title: "完整服务", detail: "应用未配置广告服务策略，客户端按内置默认策略处理", tone: "success" };
  }
  if (!profile.enabled) {
    return { title: "完整服务", detail: "策略未要求同意广告服务，所有用户均可使用完整服务", tone: "success" };
  }
  if (profile.exempt) {
    return { title: "完整服务", detail: "有效会员，免除广告服务同意要求", tone: "success" };
  }
  if (profile.mode === "full") {
    return { title: "完整服务", detail: "已同意当前版本的广告服务条款", tone: "success" };
  }
  if (!profile.consent) {
    return { title: "基础服务", detail: "尚未就广告服务作出选择，仅可使用基础服务", tone: "warning" };
  }
  if (outdated) {
    return { title: "基础服务", detail: "条款已更新，需重新选择后才能恢复完整服务", tone: "warning" };
  }
  return { title: "基础服务", detail: "已拒绝广告服务，仅可使用基础服务", tone: "danger" };
}

function ProfileOverview({ profile }: { profile: UserAdProfile }) {
  const consent = profile.consent;
  const outdated = Boolean(consent && (consent.outdated || consent.version < profile.consentVersion));
  const verdict = explainMode(profile, outdated);
  const splash = profile.splash;
  const ctr = splash.shown > 0 ? Math.round((splash.clicked / splash.shown) * 1000) / 10 : null;

  return (
    <>
      <Section
        title="当前判定"
        description="服务模式由后端统一判定"
        icon={ShieldCheck}
        tone="primary"
        actions={<ModeBadge mode={profile.mode} />}
      >
        <div className="flex flex-col gap-5">
          <div className="flex items-start gap-3 rounded-xl border bg-muted/30 px-4 py-3.5">
            <ToneIcon icon={profile.mode === "full" ? BadgeCheck : ShieldOff} tone={verdict.tone} size="lg" />
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="text-sm font-semibold">{verdict.title}</span>
              <span className="text-xs leading-5 text-muted-foreground">{verdict.detail}</span>
            </div>
            {profile.decisionRequired ? (
              <Badge variant="warning" size="sm" className="ml-auto shrink-0">
                待选择
              </Badge>
            ) : null}
          </div>
          <InfoGrid
            columns={3}
            items={[
              {
                label: "选择",
                value: consent ? <ConsentBadge accepted={consent.accepted} outdated={outdated} /> : "未选择"
              },
              {
                label: "条款版本",
                mono: true,
                value: consent ? `v${consent.version} / 当前 v${profile.consentVersion}` : `当前 v${profile.consentVersion}`,
                tone: outdated ? "warning" : undefined
              },
              { label: "来源", value: consent ? consentSourceLabel(consent.source) : null },
              {
                label: "选择时间",
                value: consent ? formatDateTime(consent.decidedAt) : null,
                hint: consent ? formatRelative(consent.decidedAt) : undefined
              },
              {
                label: "选择设备",
                value: consent?.deviceId || null,
                mono: true,
                copy: consent?.deviceId || null
              },
              { label: "选择 IP", value: consent?.clientIp || null, mono: true, copy: consent?.clientIp || null },
              {
                label: "会员",
                icon: Crown,
                value: profile.vip ? (
                  <Badge variant={profile.exempt ? "success" : "outline"} size="sm">
                    {profile.exempt ? "会员免除" : profile.adFree ? "会员" : "会员（不免广告）"}
                  </Badge>
                ) : (
                  "否"
                )
              },
              {
                label: "策略",
                value: !profile.configured ? "未配置" : profile.enabled ? "拒绝时仅提供基础服务" : "未要求同意"
              }
            ]}
          />
        </div>
      </Section>

      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <MetricCard label="累计展示" value={formatInt(splash.shown)} icon={Eye} tone="success" sub="开屏广告" />
        <MetricCard
          label="累计点击"
          value={formatInt(splash.clicked)}
          icon={MousePointerClick}
          tone={splash.clicked ? "info" : "neutral"}
          sub={ctr !== null ? `点击率 ${ctr}%` : "暂无展示"}
        />
        <MetricCard
          label="累计失败"
          value={formatInt(splash.failed)}
          icon={AlertTriangle}
          tone={splash.failed ? "warning" : "neutral"}
          sub="含加载失败与超时"
        />
        <MetricCard
          label="今日展示"
          value={formatInt(splash.todayShown)}
          icon={MonitorSmartphone}
          tone="primary"
          sub={splash.lastShownAt ? `上次 ${formatRelative(splash.lastShownAt)}` : "从未展示"}
        />
      </div>
    </>
  );
}

// ── 同意记录 ──────────────────────────

function ConsentLogSection({ appKey, userId }: { appKey: string; userId: number }) {
  const token = useAdminToken();
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [selected, setSelected] = useState<AdConsentLog | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [exporting, setExporting] = useState(false);
  const debounced = useDebouncedValue(keyword.trim(), 300);

  const query = useAdConsentLogsQuery(appKey, { userId, keyword: debounced || undefined, page, limit });
  const rows = query.data?.items ?? [];
  const total = query.data?.total ?? 0;

  const columns: Array<Column<AdConsentLog>> = [
    { id: "accepted", header: "选择", width: "110px", cell: (row) => <ConsentBadge accepted={row.accepted} /> },
    {
      id: "version",
      header: "条款版本",
      width: "90px",
      cell: (row) => <span className="font-mono text-xs tabular-nums">v{row.version}</span>
    },
    {
      id: "source",
      header: "来源",
      width: "110px",
      cell: (row) => <span className="text-xs text-muted-foreground">{consentSourceLabel(row.source)}</span>
    },
    {
      id: "device",
      header: "设备 / IP",
      hideBelow: "sm",
      cell: (row) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="max-w-56 truncate font-mono text-xs">{row.deviceId || "—"}</span>
          {row.clientIp ? <span className="font-mono text-xs text-muted-foreground">{row.clientIp}</span> : null}
        </div>
      )
    },
    {
      id: "time",
      header: "时间",
      align: "right",
      width: "170px",
      cell: (row) => <span className="text-xs text-muted-foreground tabular-nums">{formatDateTime(row.createdAt)}</span>
    }
  ];

  const exportCsv = async () => {
    if (!token) return;
    setExporting(true);
    try {
      const result = await collectPages((p, l) =>
        listAdConsentLogs(token, appKey, { userId, keyword: debounced || undefined, page: p, limit: l })
      );
      downloadCsv(
        `ad-consent-logs-${userId}`,
        [
          { header: "ID", value: (row) => row.id },
          { header: "选择", value: (row) => (row.accepted ? "同意" : "拒绝") },
          { header: "条款版本", value: (row) => row.version },
          { header: "来源", value: (row) => consentSourceLabel(row.source) },
          { header: "设备", value: (row) => row.deviceId ?? "" },
          { header: "IP", value: (row) => row.clientIp ?? "" },
          { header: "时间", value: (row) => formatDateTime(row.createdAt) }
        ],
        result.rows
      );
      if (result.truncated) toast.warning(`共 ${result.total} 条，仅导出前 ${result.rows.length} 条`);
      else toast.success(`已导出 ${result.rows.length} 条`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  };

  return (
    <Section
      title="同意记录"
      description="每一次同意与拒绝，含条款版本、来源、设备与 IP"
      icon={History}
      tone="violet"
      flush
      actions={
        <Button size="sm" variant="outline" onClick={() => void exportCsv()} disabled={exporting || total === 0}>
          {exporting ? <Loader2 className="animate-spin" /> : <Download />}
          导出 CSV
        </Button>
      }
      toolbar={
        <FilterBar>
          <SearchField
            value={keyword}
            onChange={(value) => {
              setKeyword(value);
              setPage(1);
            }}
            placeholder="搜索设备"
          />
          <ResetButton
            visible={Boolean(keyword)}
            onClick={() => {
              setKeyword("");
              setPage(1);
            }}
          />
        </FilterBar>
      }
      footer={
        total > 0 ? (
          <Pager
            page={page}
            limit={limit}
            total={total}
            onPage={setPage}
            onLimit={(value) => {
              setLimit(value);
              setPage(1);
            }}
          />
        ) : undefined
      }
    >
      <RecordTable
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        onRowClick={(row) => {
          setSelected(row);
          setSheetOpen(true);
        }}
        minWidth={560}
        skeletonRows={4}
        empty={
          <StateBlock
            icon={FileCheck2}
            title={keyword ? "没有符合条件的记录" : "暂无同意记录"}
            description={keyword ? "调整搜索条件后再试" : "用户在 App 或官网作出选择后会记录在这里"}
          />
        }
      />

      <RecordSheet
        open={sheetOpen && Boolean(selected)}
        onOpenChange={setSheetOpen}
        title="同意记录"
        description={selected ? `#${selected.id}` : undefined}
        hero={
          selected ? (
            <div className="flex items-center gap-3 rounded-xl border bg-muted/30 px-3.5 py-3">
              <ToneIcon icon={selected.accepted ? BadgeCheck : ShieldOff} tone={selected.accepted ? "success" : "danger"} />
              <div className="flex min-w-0 flex-col gap-0.5">
                <ConsentBadge accepted={selected.accepted} />
                <span className="text-xs text-muted-foreground">条款 v{selected.version}</span>
              </div>
            </div>
          ) : null
        }
        fields={
          selected
            ? [
                { label: "来源", value: consentSourceLabel(selected.source) },
                { label: "时间", value: formatDateTime(selected.createdAt), hint: formatRelative(selected.createdAt) },
                { label: "设备", value: selected.deviceId || null, mono: true, copy: selected.deviceId || null, wide: true },
                { label: "IP", value: selected.clientIp || null, mono: true, copy: selected.clientIp || null },
                { label: "账号", value: selected.account || null }
              ]
            : undefined
        }
        raw={selected}
      />
    </Section>
  );
}

// ── 开屏记录 ──────────────────────────

function SplashSection({ appKey, userId }: { appKey: string; userId: number }) {
  const token = useAdminToken();
  const [status, setStatus] = useState("");
  const [keyword, setKeyword] = useState("");
  const [range, setRange] = useState<TimeRange>({ preset: "all" });
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [selected, setSelected] = useState<SplashAdEvent | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [exporting, setExporting] = useState(false);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const { from, to } = rangeToParams(range);

  const filters = {
    userId,
    status: (status || undefined) as SplashAdStatus | undefined,
    keyword: debounced || undefined,
    start: from,
    end: to
  };
  const query = useSplashAdEventsQuery(appKey, { ...filters, page, limit });
  const rows = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const filtered = Boolean(status || keyword || range.preset !== "all");

  const columns: Array<Column<SplashAdEvent>> = [
    {
      id: "status",
      header: "结果",
      width: "150px",
      cell: (row) => (
        <div className="flex items-center gap-1.5">
          <SplashStatusBadge status={row.status} />
          {row.errorCode ? <span className="font-mono text-xs text-muted-foreground">{row.errorCode}</span> : null}
        </div>
      )
    },
    {
      id: "placement",
      header: "广告位",
      cell: (row) => <span className="font-mono text-xs text-muted-foreground">{row.placementId || "—"}</span>
    },
    {
      id: "timing",
      header: "加载 / 展示",
      width: "150px",
      hideBelow: "sm",
      cell: (row) => (
        <span className="text-xs text-muted-foreground tabular-nums">
          {formatMillis(row.loadMs)} / {formatMillis(row.shownMs)}
        </span>
      )
    },
    {
      id: "device",
      header: "设备",
      hideBelow: "md",
      cell: (row) => <span className="block max-w-44 truncate font-mono text-xs text-muted-foreground">{row.deviceId || "—"}</span>
    },
    {
      id: "time",
      header: "时间",
      align: "right",
      width: "170px",
      cell: (row) => <span className="text-xs text-muted-foreground tabular-nums">{formatDateTime(row.occurredAt)}</span>
    }
  ];

  const exportCsv = async () => {
    if (!token) return;
    setExporting(true);
    try {
      const result = await collectPages((p, l) => listSplashAdEvents(token, appKey, { ...filters, page: p, limit: l }));
      downloadCsv(
        `splash-events-${userId}`,
        [
          { header: "ID", value: (row) => row.id },
          { header: "事件", value: (row) => row.eventId },
          { header: "结果", value: (row) => SPLASH_LABEL[row.status] ?? row.status },
          { header: "错误码", value: (row) => row.errorCode ?? "" },
          { header: "错误信息", value: (row) => row.errorMessage ?? "" },
          { header: "广告位", value: (row) => row.placementId },
          { header: "加载(毫秒)", value: (row) => row.loadMs },
          { header: "展示(毫秒)", value: (row) => row.shownMs },
          { header: "设备", value: (row) => row.deviceId ?? "" },
          { header: "IP", value: (row) => row.clientIp ?? "" },
          { header: "发生时间", value: (row) => formatDateTime(row.occurredAt) },
          { header: "上报时间", value: (row) => formatDateTime(row.createdAt) }
        ],
        result.rows
      );
      if (result.truncated) toast.warning(`共 ${result.total} 条，仅导出前 ${result.rows.length} 条`);
      else toast.success(`已导出 ${result.rows.length} 条`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  };

  return (
    <Section
      title="开屏记录"
      description="每一次开屏广告的结局与耗时"
      icon={MonitorSmartphone}
      tone="info"
      flush
      actions={
        <Button size="sm" variant="outline" onClick={() => void exportCsv()} disabled={exporting || total === 0}>
          {exporting ? <Loader2 className="animate-spin" /> : <Download />}
          导出 CSV
        </Button>
      }
      toolbar={
        <FilterBar>
          <SearchField
            value={keyword}
            onChange={(value) => {
              setKeyword(value);
              setPage(1);
            }}
            placeholder="搜索设备或错误码"
          />
          <FilterSelect
            label="结果"
            value={status}
            options={SPLASH_STATUS_OPTIONS}
            onChange={(value) => {
              setStatus(value);
              setPage(1);
            }}
          />
          <TimeRangePicker
            value={range}
            onChange={(value) => {
              setRange(value);
              setPage(1);
            }}
          />
          <ResetButton
            visible={filtered}
            onClick={() => {
              setStatus("");
              setKeyword("");
              setRange({ preset: "all" });
              setPage(1);
            }}
          />
        </FilterBar>
      }
      footer={
        total > 0 ? (
          <Pager
            page={page}
            limit={limit}
            total={total}
            onPage={setPage}
            onLimit={(value) => {
              setLimit(value);
              setPage(1);
            }}
          />
        ) : undefined
      }
    >
      <RecordTable
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        onRowClick={(row) => {
          setSelected(row);
          setSheetOpen(true);
        }}
        minWidth={600}
        empty={
          <StateBlock
            icon={MonitorSmartphone}
            title={filtered ? "没有符合条件的记录" : "暂无开屏记录"}
            description={filtered ? "调整筛选条件后再试" : "用户登录后看到的开屏广告会记录在这里"}
          />
        }
      />

      <RecordSheet
        open={sheetOpen && Boolean(selected)}
        onOpenChange={setSheetOpen}
        title="开屏记录"
        description={selected ? `#${selected.id}` : undefined}
        hero={
          selected ? (
            <div className="flex flex-col gap-2 rounded-xl border bg-muted/30 px-3.5 py-3">
              <div className="flex items-center gap-2">
                <SplashStatusBadge status={selected.status} />
                {selected.errorCode ? <span className="font-mono text-xs">{selected.errorCode}</span> : null}
                <span className="ml-auto text-xs text-muted-foreground tabular-nums">{formatRelative(selected.occurredAt)}</span>
              </div>
              {selected.errorMessage ? (
                <p className="text-xs leading-5 break-words text-red-700 dark:text-red-300">{selected.errorMessage}</p>
              ) : null}
            </div>
          ) : null
        }
        fields={
          selected
            ? [
                { label: "广告位", value: selected.placementId || null, mono: true, copy: selected.placementId || null },
                { label: "事件 ID", value: selected.eventId, mono: true, copy: selected.eventId },
                { label: "加载耗时", value: formatMillis(selected.loadMs) },
                { label: "展示时长", value: formatMillis(selected.shownMs) },
                { label: "发生时间", value: formatDateTime(selected.occurredAt) },
                { label: "上报时间", value: formatDateTime(selected.createdAt) },
                { label: "设备", value: selected.deviceId || null, mono: true, copy: selected.deviceId || null, wide: true },
                { label: "IP", value: selected.clientIp || null, mono: true, copy: selected.clientIp || null }
              ]
            : undefined
        }
        raw={selected}
      />
    </Section>
  );
}
