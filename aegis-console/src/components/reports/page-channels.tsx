"use client";

import { useMemo } from "react";
import {
  Building2,
  CheckCircle2,
  Cpu,
  Gift,
  Globe2,
  KeyRound,
  Link2,
  LogIn,
  Map as MapIcon,
  MapPin,
  Percent,
  Repeat,
  Smartphone,
  UserPlus,
  Users,
  XCircle
} from "lucide-react";
import { CountryFlag } from "@/components/ui/country-flag";
import { useReportQuery } from "@/lib/report-hooks";
import type { ReportSlice } from "@/lib/api/reports";
import { ChinaMap } from "./china-map";
import {
  Donut,
  HeatmapGrid,
  KpiGrid,
  RankList,
  ReportCard,
  SectionGrid,
  TrendChart,
  trendEmpty,
  type KpiSpec,
  type SeriesSpec
} from "./report-kit";
import { FAILURE_REASON, INVITE_SOURCE, LOGIN_TYPE, OTHER_KEY, PLATFORM, PROVIDER, formatValue, labelOf } from "./report-labels";
import { ReportSection, type SectionProps } from "./report-section";

export function ChannelsPage(props: SectionProps) {
  return (
    <div className="space-y-10">
      <LoginSection {...props} />
      <SourceSection {...props} />
      <DeviceSection {...props} />
      <RegionSection {...props} />
    </div>
  );
}

const LOGIN_KPIS: KpiSpec[] = [
  { key: "success", label: "成功登录", icon: CheckCircle2, tone: "emerald" },
  { key: "failure", label: "失败登录", icon: XCircle, tone: "rose", inverse: true },
  { key: "failureRate", label: "失败率", icon: Percent, tone: "rose", format: "percent", inverse: true },
  { key: "users", label: "登录用户", icon: Users, tone: "sky" },
  { key: "perUser", label: "人均登录", icon: Repeat, tone: "indigo", format: "decimal" }
];

function LoginSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("login", appKey, params);
  const d = q.data;
  return (
    <ReportSection id="login" title="登录" description="成功与失败、登录方式和失败原因" icon={LogIn} tone="sky">
      <KpiGrid specs={LOGIN_KPIS} kpis={d?.kpis} loading={q.isLoading} columns={5} />
      <ReportCard title="登录趋势" description="右轴为失败率" loading={q.isLoading} empty={trendEmpty(d?.trend, ["success", "failure"])} height={260}>
        <TrendChart
          trend={d?.trend}
          granularity={params.granularity}
          height={260}
          series={[
            { key: "success", label: "成功", type: "bar", stack: "a", tone: "emerald" },
            { key: "failure", label: "失败", type: "bar", stack: "a", tone: "rose" },
            { key: "users", label: "登录用户", type: "line", tone: "sky" },
            { key: "failureRate", label: "失败率", type: "line", tone: "amber", right: true, dashed: true }
          ]}
        />
      </ReportCard>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="登录方式" loading={q.isLoading}>
          <Donut items={d?.types} dict={LOGIN_TYPE} />
        </ReportCard>
        <ReportCard title="登录渠道" loading={q.isLoading}>
          <RankList items={d?.providers} dict={PROVIDER} secondary={(it) => `${formatValue(it.users)} 人`} />
        </ReportCard>
        <ReportCard title="单人登录次数" description="期内每个用户的成功登录次数" loading={q.isLoading}>
          <RankList items={d?.frequency} ordinal={false} labelOf={(it) => `${it.key} 次`} tone="indigo" />
        </ReportCard>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="失败原因" loading={q.isLoading}>
          <RankList items={d?.failures} dict={FAILURE_REASON} tone="rose" secondary={(it) => `${formatValue(it.users)} 个账号`} />
        </ReportCard>
        <ReportCard title="失败最多的来源 IP" loading={q.isLoading}>
          <RankList
            items={d?.failureIps}
            tone="rose"
            labelOf={(it) => <span className="font-mono">{it.key}</span>}
            secondary={(it) => `${formatValue(it.users)} 个账号`}
          />
        </ReportCard>
        <ReportCard title="登录时段" loading={q.isLoading}>
          <HeatmapGrid heatmap={d?.heatmap} tone="sky" />
        </ReportCard>
      </div>
    </ReportSection>
  );
}

const SOURCE_KPIS: KpiSpec[] = [
  { key: "newUsers", label: "新增用户", icon: UserPlus, tone: "indigo" },
  { key: "thirdPartyShare", label: "第三方登录注册", icon: Link2, tone: "violet", format: "percent" },
  { key: "invited", label: "受邀注册", icon: Gift, tone: "pink" },
  { key: "inviteRate", label: "邀请占比", icon: Percent, tone: "pink", format: "percent" },
  { key: "cardKeys", label: "卡密兑换", icon: KeyRound, tone: "amber" }
];

function SourceSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("channel", appKey, params);
  const d = q.data;
  const series = useMemo<SeriesSpec[]>(
    () =>
      Object.keys(d?.trend.series ?? {})
        .sort((a, b) => (a === OTHER_KEY ? 1 : b === OTHER_KEY ? -1 : 0))
        .map((key, i) => ({ key, label: labelOf(PROVIDER, key), type: "area", stack: "s", tone: key === OTHER_KEY ? "slate" : i })),
    [d]
  );
  return (
    <ReportSection id="source" title="渠道来源" description="新用户从哪里来：注册方式、邀请与卡密" icon={Link2} tone="violet">
      <KpiGrid specs={SOURCE_KPIS} kpis={d?.kpis} loading={q.isLoading} columns={5} />
      <div className="grid gap-4 xl:grid-cols-3">
        <ReportCard title="注册来源趋势" className="xl:col-span-2" loading={q.isLoading} empty={series.length === 0} height={260}>
          <TrendChart trend={d?.trend} granularity={params.granularity} height={260} series={series} />
        </ReportCard>
        <ReportCard title="注册来源" loading={q.isLoading}>
          <Donut items={d?.registerSources} dict={PROVIDER} centerLabel="新增" />
        </ReportCard>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="登录渠道用户" loading={q.isLoading}>
          <RankList items={d?.loginProviders} dict={PROVIDER} metric="users" secondary={(it) => `${formatValue(it.count)} 次`} />
        </ReportCard>
        <ReportCard title="邀请来源" description="附注为已给邀请人发放奖励的数量" loading={q.isLoading}>
          <RankList items={d?.inviteSources} dict={INVITE_SOURCE} tone="pink" secondary={(it) => `${formatValue(it.value ?? 0)} 已发奖`} />
        </ReportCard>
        <ReportCard title="卡密兑换来源" loading={q.isLoading}>
          <RankList items={d?.cardKeySources} tone="amber" secondary={(it) => `${formatValue(it.users)} 人`} />
        </ReportCard>
      </div>
    </ReportSection>
  );
}

const DEVICE_KPIS: KpiSpec[] = [
  { key: "devices", label: "设备数", icon: Smartphone, tone: "teal" },
  { key: "users", label: "用户数", icon: Users, tone: "sky" },
  { key: "devicesPerUser", label: "人均设备", icon: Cpu, tone: "indigo", format: "decimal" },
  { key: "matchedRate", label: "机型识别率", icon: CheckCircle2, tone: "emerald", format: "percent", hint: "命中设备字典的比例" }
];

function DeviceThumb({ item, field }: { item: ReportSlice; field: "image" | "icon" }) {
  const src = item.meta?.[field];
  if (!src) {
    return (
      <span className="flex size-6 items-center justify-center rounded-md bg-muted text-muted-foreground">
        {field === "icon" ? <Building2 className="size-3.5" /> : <Smartphone className="size-3.5" />}
      </span>
    );
  }
  // eslint-disable-next-line @next/next/no-img-element
  return <img src={src} alt="" className="size-6 rounded-md object-contain" loading="lazy" />;
}

function DeviceSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("device", appKey, params);
  const d = q.data;
  return (
    <ReportSection id="device" title="设备" description="每台设备取期内最近一次登录时上报的信息，机型按设备字典显示" icon={Smartphone} tone="teal">
      <KpiGrid specs={DEVICE_KPIS} kpis={d?.kpis} loading={q.isLoading} />
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="平台" loading={q.isLoading}>
          <Donut items={d?.platforms} dict={PLATFORM} centerLabel="设备" />
        </ReportCard>
        <ReportCard title="厂商" loading={q.isLoading}>
          <RankList items={d?.manufacturers} leading={(it) => <DeviceThumb item={it} field="icon" />} secondary={(it) => `${formatValue(it.users)} 人`} />
        </ReportCard>
        <ReportCard title="机型" loading={q.isLoading}>
          <RankList items={d?.models} leading={(it) => <DeviceThumb item={it} field="image" />} secondary={(it) => `${formatValue(it.users)} 人`} />
        </ReportCard>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="系统版本" loading={q.isLoading}>
          <RankList items={d?.osVersions} tone="indigo" />
        </ReportCard>
        <ReportCard title="应用版本" loading={q.isLoading}>
          <RankList items={d?.appVersions} tone="violet" labelOf={(it) => (it.key === "unknown" ? "未上报" : <span className="font-mono">{it.key}</span>)} />
        </ReportCard>
        <ReportCard title="浏览器" description="只统计网页端登录" loading={q.isLoading}>
          <RankList items={d?.browsers} tone="sky" labelOf={(it) => (it.key === "other" ? "其他浏览器" : labelOf(undefined, it.key))} />
        </ReportCard>
      </div>
    </ReportSection>
  );
}

const REGION_KPIS: KpiSpec[] = [
  { key: "countries", label: "国家和地区", icon: Globe2, tone: "sky" },
  { key: "provinces", label: "省份", icon: MapIcon, tone: "indigo" },
  { key: "cities", label: "城市", icon: MapPin, tone: "teal" },
  { key: "domesticShare", label: "境内用户占比", icon: Percent, tone: "emerald", format: "percent" }
];

function RegionSection({ appKey, params }: SectionProps) {
  const q = useReportQuery("region", appKey, params);
  const d = q.data;
  const cnProvinces = useMemo(() => (d?.provinces ?? []).filter((p) => p.label === "CN" || p.label === ""), [d]);
  return (
    <ReportSection id="region" title="地区" description="登录归属地，按去重用户数排序" icon={Globe2} tone="indigo">
      <KpiGrid specs={REGION_KPIS} kpis={d?.kpis} loading={q.isLoading} />
      <div className="grid gap-4 xl:grid-cols-5">
        <ReportCard title="境内分布" className="xl:col-span-3" loading={q.isLoading} height={400}>
          <ChinaMap items={cnProvinces} height={400} />
        </ReportCard>
        <ReportCard title="省份排行" className="xl:col-span-2" loading={q.isLoading}>
          <RankList items={d?.provinces} metric="users" limit={12} secondary={(it) => `${formatValue(it.count)} 次`} labelOf={(it) => labelOf(undefined, it.key)} />
        </ReportCard>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <ReportCard title="国家和地区" loading={q.isLoading}>
          <RankList
            items={d?.countries}
            metric="users"
            leading={(it) => <CountryFlag code={it.key === "unknown" ? "" : it.key} size={16} />}
            labelOf={(it) => it.label || labelOf(undefined, it.key)}
          />
        </ReportCard>
        <ReportCard title="城市" loading={q.isLoading}>
          <RankList
            items={d?.cities}
            metric="users"
            labelOf={(it) => (
              <>
                {labelOf(undefined, it.key)}
                {it.label && it.label !== it.key ? <span className="ml-1.5 text-muted-foreground">{it.label}</span> : null}
              </>
            )}
          />
        </ReportCard>
        <ReportCard title="运营商" loading={q.isLoading}>
          <RankList items={d?.isps} metric="users" tone="teal" />
        </ReportCard>
      </div>
      <SectionGrid>
        <ReportCard title="新用户注册省份" description="按注册时 IP 归属，与登录归属地对照" loading={q.isLoading}>
          <RankList items={d?.registerProvinces} limit={10} tone="violet" />
        </ReportCard>
        <ReportCard title="海外登录" description="境外国家和地区的登录用户" loading={q.isLoading}>
          <RankList
            items={(d?.countries ?? []).filter((c) => c.key !== "CN" && c.key !== "unknown")}
            metric="users"
            tone="amber"
            leading={(it) => <CountryFlag code={it.key} size={16} />}
            labelOf={(it) => it.label || it.key}
          />
        </ReportCard>
      </SectionGrid>
    </ReportSection>
  );
}
