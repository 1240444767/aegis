"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { CalendarCheck2, Crown, Infinity as InfinityIcon, Loader2, RotateCcw, Save, Tag, Timer, TrendingUp, Users } from "lucide-react";
import { toast } from "sonner";
import { ModeCard, StatusDot, SwitchRow } from "@/components/apps/app-config-primitives";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { RadioGroup } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { AutoSignAudience, AutoSignPolicy } from "@/lib/api/types";
import { useAdminAppAutoSignQuery, useUpdateAdminAppAutoSignMutation } from "@/lib/admin-hooks";
import { useAdminVipFeaturesQuery } from "@/lib/vip-hooks";
import { Field, MetricTile, Panel, formatNumber } from "./signin-shared";

/**
 * 自动签到：应用级开放策略 + 使用情况。
 *
 * 只管「开不开放、开放给谁」；几点签、失败重试、通知是用户自己的个人设置（用户详情里的 autoSign 分类）。
 * 资格判定只在后端（EvaluateAutoSign），这里不推演谁能用。指定功能只能从会员功能目录里选，
 * 后端保存时还会再核对一次（40486）。契约见 docs/auto-sign.md。
 */
const AUDIENCES: { value: AutoSignAudience; title: string; description: string; icon: React.ReactNode }[] = [
  { value: "all", title: "所有用户", description: "所有登录用户", icon: <Users className="size-4" /> },
  { value: "vip", title: "会员", description: "有效会员，含永久会员", icon: <Crown className="size-4" /> },
  { value: "lifetime", title: "永久会员", description: "仅永久会员", icon: <InfinityIcon className="size-4" /> },
  { value: "feature", title: "持有指定功能", description: "含会员套餐与功能解锁", icon: <Tag className="size-4" /> }
];

const TREND_CHART: ChartConfig = { count: { label: "自动签到", theme: { light: "#8b5cf6", dark: "#a78bfa" } } };

type Form = {
  enabled: boolean;
  audience: AutoSignAudience;
  includeTrial: boolean;
  featureTag: string;
};

function seedForm(policy?: AutoSignPolicy): Form {
  return {
    enabled: policy?.enabled ?? false,
    audience: policy?.audience ?? "all",
    includeTrial: policy?.includeTrial ?? true,
    featureTag: policy?.featureTag ?? ""
  };
}

function sameForm(a: Form, b: Form) {
  return a.enabled === b.enabled && a.audience === b.audience && a.includeTrial === b.includeTrial && a.featureTag === b.featureTag;
}

export function SignInAutoSign({ appKey }: { appKey: string }) {
  const overviewQuery = useAdminAppAutoSignQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useUpdateAdminAppAutoSignMutation(appKey);
  const overview = overviewQuery.data;
  const loading = overviewQuery.isLoading;

  // 草稿按应用绑定：无草稿即从服务端派生，保存后清掉草稿，表单跟着返回值走
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const saved = seedForm(overview?.policy);
  const form = draft?.scope === appKey ? draft.value : saved;
  const dirty = !sameForm(form, saved);
  const patch = (changes: Partial<Form>) => setDraft({ scope: appKey, value: { ...form, ...changes } });

  const features = featuresQuery.data ?? [];
  // 已保存的功能被删出了目录：仍列出来，否则选择框显示为空，看不出当前配的是什么
  const orphanTag = form.featureTag && !features.some((feature) => feature.tag === form.featureTag) ? form.featureTag : "";

  const trend = useMemo(
    () => (overview?.trend ?? []).map((point) => ({ ...point, label: point.date.slice(5).replace("-", "/") })),
    [overview]
  );
  const trendTotal = trend.reduce((sum, point) => sum + point.count, 0);
  const timezone = overview?.timezone ?? "";

  async function save() {
    if (form.audience === "feature" && !form.featureTag) {
      toast.error("请选择功能");
      return;
    }
    try {
      await saveMutation.mutateAsync({
        policy: {
          enabled: form.enabled,
          audience: form.audience,
          includeTrial: form.includeTrial,
          featureTag: form.audience === "feature" ? form.featureTag : ""
        }
      });
      setDraft(null);
      toast.success("自动签到策略已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
        <MetricTile
          label="开启人数"
          icon={<Users className="size-3" />}
          value={formatNumber(overview?.enabledUsers)}
          hint="含暂不满足开放对象的用户"
          loading={loading}
        />
        <MetricTile
          label="今日自动签到"
          icon={<CalendarCheck2 className="size-3" />}
          value={formatNumber(overview?.todayCount)}
          hint={timezone ? `按 ${timezone} 计日` : undefined}
          loading={loading}
        />
        <MetricTile
          label="近 14 天自动签到"
          icon={<TrendingUp className="size-3" />}
          value={formatNumber(trendTotal)}
          hint={`日均 ${formatNumber(trendTotal / 14, 1)} 次`}
          loading={loading}
        />
      </div>

      <Panel
        title="开放策略"
        icon={<Timer className="size-4" />}
        action={
          loading ? (
            <Skeleton className="h-5 w-16 rounded-full" />
          ) : (
            <StatusDot active={Boolean(overview?.policy.enabled)} labelActive="已开放" labelInactive="未开放" />
          )
        }
      >
        {loading ? (
          <div className="space-y-4">
            <Skeleton className="h-14 w-full rounded-xl" />
            <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
              {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
            </div>
            <Skeleton className="h-9 w-full" />
          </div>
        ) : (
          <div className="space-y-4">
            <SwitchRow
              label="开放自动签到"
              icon={<Timer className="size-4" />}
              checked={form.enabled}
              onChange={(value) => patch({ enabled: value })}
            />

            <Field label="开放对象">
              <RadioGroup value={form.audience} onValueChange={(value) => patch({ audience: value as AutoSignAudience })} disabled={!form.enabled}>
                <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
                  {AUDIENCES.map((item) => (
                    <ModeCard
                      key={item.value}
                      value={item.value}
                      active={form.audience === item.value}
                      title={item.title}
                      description={item.description}
                      icon={item.icon}
                      disabled={!form.enabled}
                    />
                  ))}
                </div>
              </RadioGroup>
            </Field>

            {form.audience === "vip" ? (
              <SwitchRow
                label="试用会员可用"
                checked={form.includeTrial}
                onChange={(value) => patch({ includeTrial: value })}
                disabled={!form.enabled}
              />
            ) : null}

            {form.audience === "feature" ? (
              <Field label="功能">
                {featuresQuery.isLoading ? (
                  <Skeleton className="h-9 w-full sm:w-72" />
                ) : features.length === 0 && !orphanTag ? (
                  <p className="text-xs text-muted-foreground">
                    暂无功能标识，请先在
                    <Link href={`/apps/${encodeURIComponent(appKey)}?tab=vip`} className="mx-0.5 text-foreground underline underline-offset-2">
                      会员功能目录
                    </Link>
                    登记
                  </p>
                ) : (
                  <Select value={form.featureTag} onValueChange={(value) => patch({ featureTag: value })} disabled={!form.enabled}>
                    <SelectTrigger className="h-9 w-full text-sm sm:w-72">
                      <SelectValue placeholder="选择功能" />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      {orphanTag ? (
                        <SelectItem value={orphanTag} disabled>
                          {overview?.policy.featureTag === orphanTag && overview.featureName ? overview.featureName : orphanTag}
                          <span className="ml-1.5 text-[10px] text-muted-foreground">未登记</span>
                        </SelectItem>
                      ) : null}
                      {features.map((feature) => (
                        <SelectItem key={feature.tag} value={feature.tag} disabled={!feature.isActive}>
                          {feature.name || feature.tag}
                          <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
                          {!feature.isActive ? <span className="ml-1.5 text-[10px] text-muted-foreground">已停用</span> : null}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </Field>
            ) : null}

            <div className="flex flex-wrap items-center justify-end gap-2 border-t pt-4">
              {dirty ? (
                <Button variant="outline" size="sm" className="h-8 gap-1.5" disabled={saveMutation.isPending} onClick={() => setDraft(null)}>
                  <RotateCcw className="size-3.5" />
                  放弃修改
                </Button>
              ) : null}
              <Button size="sm" className="h-8 gap-1.5" disabled={!dirty || saveMutation.isPending} onClick={() => void save()}>
                {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                保存
              </Button>
            </div>
          </div>
        )}
      </Panel>

      <Panel
        title="自动签到趋势"
        icon={<TrendingUp className="size-4" />}
        description={timezone ? `近 14 天每日次数，按 ${timezone} 计日` : "近 14 天每日次数"}
      >
        {loading ? (
          <Skeleton className="h-64 w-full rounded-xl" />
        ) : trendTotal === 0 ? (
          <div className="flex h-64 flex-col items-center justify-center gap-1 rounded-xl border border-dashed text-center">
            <Timer className="size-5 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">近 14 天暂无自动签到</p>
          </div>
        ) : (
          <ChartContainer config={TREND_CHART} className="h-64 w-full">
            <AreaChart data={trend} margin={{ top: 8, right: 8, bottom: 0, left: -8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/50" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={24} tickMargin={8} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={44} />
              <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
              <Area
                dataKey="count"
                type="monotone"
                stroke="var(--color-count)"
                fill="var(--color-count)"
                fillOpacity={0.12}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
            </AreaChart>
          </ChartContainer>
        )}
      </Panel>
    </div>
  );
}
