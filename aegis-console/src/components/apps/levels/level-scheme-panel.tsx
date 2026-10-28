"use client";

import { AlertTriangle, CircleAlert, Gift, Lock, Pencil, Rocket, Sparkles, Waypoints } from "lucide-react";
import { LEVEL_ICON, PRIVILEGE_ICON, describeRewardItems, formatNumber, privilegeValueText } from "@/components/apps/levels/level-meta";
import { Section, StateBlock } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { LevelCatalog, LevelRow } from "@/lib/api/levels";
import { formatDateTime } from "@/lib/datetime";
import { useLevelCatalogQuery, useLevelSchemeQuery } from "@/lib/level-hooks";

/**
 * 体系视图：当前草稿的逐级展开（门槛、晋级条件、奖励、权益），发布状态与问题。所有修改都在画布里做。
 */

export function LevelSchemePanel({ appKey, onOpenEditor }: { appKey: string; onOpenEditor: () => void }) {
  const schemeQuery = useLevelSchemeQuery(appKey);
  const catalogQuery = useLevelCatalogQuery(appKey);
  const scheme = schemeQuery.data;

  if (schemeQuery.error) {
    return <StateBlock kind="error" description={schemeQuery.error instanceof Error ? schemeQuery.error.message : "加载失败"} onRetry={() => void schemeQuery.refetch()} />;
  }

  const errors = scheme?.issues.filter((issue) => issue.level === "error") ?? [];
  const warnings = scheme?.issues.filter((issue) => issue.level === "warning") ?? [];

  return (
    <div className="space-y-4">
      <Section
        title={scheme?.summary.name || "等级体系"}
        description={
          scheme
            ? scheme.publishedVersion
              ? `已发布第 ${scheme.publishedVersion} 版，${scheme.publishedAt ? formatDateTime(scheme.publishedAt) : ""}${scheme.publishedBy ? `（${scheme.publishedBy}）` : ""}`
              : scheme.isDefault
                ? "这个应用还没有自己的等级体系，正在使用默认体系（由旧版全站等级表转换）。在画布里修改并发布即成为本应用的体系。"
                : "草稿还没有发布，线上仍在使用默认体系"
            : "加载中"
        }
        icon={Waypoints}
        tone="violet"
        actions={
          <>
            {scheme?.dirty && scheme.publishedVersion ? <Badge variant="warning" size="sm">草稿有未发布的改动</Badge> : null}
            <Button size="sm" onClick={onOpenEditor}>
              <Pencil /> 打开画布
            </Button>
          </>
        }
      >
        {scheme ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
            <Stat label="等级" value={`${scheme.summary.levels} 级`} sub={scheme.summary.levels ? `${scheme.summary.firstLevel} 至 ${scheme.summary.topLevel}` : "—"} />
            <Stat label="满级门槛" value={formatNumber(scheme.summary.maxThreshold)} sub="累计经验" />
            <Stat label="达成奖励" value={`${scheme.summary.rewards} 份`} sub={scheme.summary.manualRewards ? `${scheme.summary.manualRewards} 份需领取` : "全部自动到账"} />
            <Stat label="等级权益" value={`${scheme.summary.privileges} 项`} />
            <Stat label="晋级条件" value={`${scheme.summary.gates} 道`} />
            <Stat
              label="校验"
              value={errors.length ? `${errors.length} 个错误` : warnings.length ? `${warnings.length} 个提醒` : "通过"}
              tone={errors.length ? "danger" : warnings.length ? "warning" : "success"}
            />
          </div>
        ) : (
          <Skeleton className="h-16 w-full" />
        )}
        {errors.length || warnings.length ? (
          <div className="mt-3 space-y-1.5">
            {[...errors, ...warnings].slice(0, 5).map((issue, index) => (
              <div key={index} className="flex items-start gap-2 text-xs">
                {issue.level === "error" ? <CircleAlert className="mt-0.5 size-3.5 shrink-0 text-red-500" /> : <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />}
                <span className="text-muted-foreground">{issue.message}</span>
              </div>
            ))}
          </div>
        ) : null}
      </Section>

      <Section title="逐级展开" description="草稿按主干顺序编排的等级；打开画布可以修改、推演与发布" icon={Rocket} tone="primary" flush>
        {!scheme ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2, 3].map((index) => (
              <Skeleton key={index} className="h-14 w-full" />
            ))}
          </div>
        ) : scheme.levels.length ? (
          <div className="divide-y">
            {scheme.levels.map((row, index) => (
              <LevelLine key={row.nodeId} row={row} previous={scheme.levels[index - 1]} catalog={catalogQuery.data} />
            ))}
          </div>
        ) : (
          <StateBlock compact title="画布还编译不出等级" description="打开画布修正问题后这里会显示逐级展开" />
        )}
      </Section>
    </div>
  );
}

function Stat({ label, value, sub, tone }: { label: string; value: string; sub?: string; tone?: "danger" | "warning" | "success" }) {
  return (
    <div className="rounded-xl border px-3 py-2.5">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div
        className={
          tone === "danger"
            ? "text-base font-semibold text-red-600 dark:text-red-400"
            : tone === "warning"
              ? "text-base font-semibold text-amber-600 dark:text-amber-400"
              : tone === "success"
                ? "text-base font-semibold text-emerald-600 dark:text-emerald-400"
                : "text-base font-semibold tabular-nums"
        }
      >
        {value}
      </div>
      {sub ? <div className="truncate text-[11px] text-muted-foreground">{sub}</div> : null}
    </div>
  );
}

function LevelLine({ row, previous, catalog }: { row: LevelRow; previous?: LevelRow; catalog?: LevelCatalog }) {
  const Icon = LEVEL_ICON[row.icon ?? ""];
  const color = row.color || "#6366f1";
  return (
    <div className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start sm:px-5">
      <div className="flex min-w-0 items-center gap-3 sm:w-64 sm:shrink-0">
        <span
          className="inline-flex size-9 shrink-0 items-center justify-center rounded-[11px] text-xs font-semibold [&>svg]:size-[18px]"
          style={{ background: `color-mix(in oklab, ${color} 16%, transparent)`, color }}
        >
          {Icon ? <Icon /> : row.level}
        </span>
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">
            <span className="mr-1.5 tabular-nums" style={{ color }}>
              Lv.{row.level}
            </span>
            {row.name}
          </div>
          <div className="truncate text-[11px] text-muted-foreground">
            {formatNumber(row.threshold)} 经验{previous ? `，+${formatNumber(row.threshold - previous.threshold)}` : ""}
          </div>
        </div>
      </div>
      <div className="flex min-w-0 flex-1 flex-wrap gap-1.5">
        {row.gate ? (
          <span className="inline-flex items-center gap-1 rounded-lg bg-amber-500/10 px-2 py-0.5 text-[11px] text-amber-700 dark:text-amber-300">
            <Lock className="size-3" />
            {row.gate.hint || row.gate.label || "晋级条件"}
          </span>
        ) : null}
        {row.rewards.map((reward) => (
          <span
            key={reward.nodeId}
            className="inline-flex max-w-full items-center gap-1 rounded-lg bg-emerald-500/10 px-2 py-0.5 text-[11px] text-emerald-700 dark:text-emerald-300"
            title={describeRewardItems({ items: reward.items, claim: reward.claim }, catalog).join("、")}
          >
            <Gift className="size-3 shrink-0" />
            <span className="truncate">
              {reward.label}
              {reward.claim === "manual" ? "（需领取）" : ""}
            </span>
          </span>
        ))}
        {row.privileges.map((privilege) => {
          const PIcon = PRIVILEGE_ICON[privilege.kind] ?? Sparkles;
          return (
            <span key={privilege.nodeId} className="inline-flex max-w-full items-center gap-1 rounded-lg bg-sky-500/10 px-2 py-0.5 text-[11px] text-sky-700 dark:text-sky-300">
              <PIcon className="size-3 shrink-0" />
              <span className="truncate">
                {privilege.label}：{privilegeValueText(privilege, catalog)}
                {privilege.scope === "only" ? "（仅本级）" : ""}
              </span>
            </span>
          );
        })}
        {!row.gate && !row.rewards.length && !row.privileges.length ? <span className="text-[11px] text-muted-foreground">无奖励与权益</span> : null}
      </div>
    </div>
  );
}
