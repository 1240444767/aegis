"use client";

import NumberFlow from "@number-flow/react";
import { AlertTriangle, CircleCheck, type LucideIcon } from "lucide-react";
import { motion } from "motion/react";
import { kindVisual } from "@/components/apps/lottery/editor/prize-visuals";
import type { FormIssue, PrizeDraft } from "@/components/apps/lottery/lottery-activity-form";
import { cn } from "@/lib/utils";

/**
 * 活动编辑器的外壳：分区卡、左侧分区导航、右侧实时概览。
 *
 * 校验结果按分区归类（`issueSection`），导航上每个分区显示自己还差几项，
 * 概览里的问题列表点一下就滚到对应分区。
 */

export type SectionKey = "basic" | "draws" | "prizes" | "pity" | "strategy" | "perks" | "owned" | "rules";

export type SectionMeta = {
  key: SectionKey;
  label: string;
  icon: LucideIcon;
};

/**
 * 一条校验问题属于哪个分区。表单校验只分 basic / prizes / strategy 三档，
 * 这里按说法再细分：保底、次数、已拥有的折算各回各的分区，导航上的计数才对得上。
 */
export function issueSection(issue: FormIssue): SectionKey {
  const message = issue.message;
  if (issue.scope === "strategy") return "strategy";
  if (issue.scope === "perks") return "perks";
  if (message.includes("保底")) return "pity";
  if (message.includes("折算") || message.includes("改发抽奖次数")) return "owned";
  if (issue.scope === "prizes") return "prizes";
  if (message.includes("活动规则")) return "rules";
  if (message.includes("免费") || message.includes("积分")) return "draws";
  return "basic";
}

/** 分区卡：色调图标 + 标题 + 说明，右上角放分区级的开关或计数 */
export function EditorSection({
  id,
  icon: Icon,
  title,
  description,
  aside,
  issues = 0,
  bodyClassName,
  children
}: {
  id: SectionKey;
  icon: LucideIcon;
  title: string;
  description?: string;
  aside?: React.ReactNode;
  issues?: number;
  bodyClassName?: string;
  children?: React.ReactNode;
}) {
  return (
    <section data-section={id} className="scroll-mt-4 overflow-hidden rounded-[1.25rem] border border-border bg-card">
      <header className="flex items-center gap-3 px-5 pb-1 pt-4">
        <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-muted text-foreground/70 [&>svg]:size-4">
          <Icon />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="flex items-center gap-2 text-sm font-semibold tracking-tight">
            {title}
            {issues > 0 ? (
              <span className="rounded-full bg-amber-500/12 px-1.5 text-[10px] font-medium tabular-nums text-amber-700 dark:text-amber-400">
                {issues} 项待处理
              </span>
            ) : null}
          </h3>
          {description ? <p className="truncate text-xs text-muted-foreground">{description}</p> : null}
        </div>
        {aside ? <div className="shrink-0">{aside}</div> : null}
      </header>
      {children ? <div className={cn("space-y-5 px-5 pb-5 pt-4", bodyClassName)}>{children}</div> : <div className="pb-3" />}
    </section>
  );
}

/** 左侧分区导航：当前分区由滚动位置决定，指示块在分区间滑动 */
export function SectionNav({
  sections,
  active,
  issueCount,
  onJump
}: {
  sections: SectionMeta[];
  active: SectionKey;
  issueCount: (key: SectionKey) => number;
  onJump: (key: SectionKey) => void;
}) {
  return (
    <nav className="space-y-0.5">
      {sections.map((section, index) => {
        const Icon = section.icon;
        const current = section.key === active;
        const issues = issueCount(section.key);
        return (
          <button
            key={section.key}
            type="button"
            onClick={() => onJump(section.key)}
            className={cn(
              "relative flex h-9 w-full items-center gap-2.5 rounded-xl px-2.5 text-left text-[13px] transition-colors",
              current ? "text-foreground" : "text-muted-foreground hover:text-foreground"
            )}
          >
            {current ? (
              <motion.span
                layoutId="lottery-editor-nav"
                className="absolute inset-0 rounded-xl bg-card ring-1 ring-border"
                transition={{ type: "spring", stiffness: 500, damping: 40 }}
              />
            ) : null}
            <span className="relative font-data text-[10px] tabular-nums text-muted-foreground/60">{String(index + 1).padStart(2, "0")}</span>
            <Icon className="relative size-3.5 shrink-0" />
            <span className="relative min-w-0 flex-1 truncate font-medium">{section.label}</span>
            {issues > 0 ? (
              <span className="relative grid min-w-4 place-items-center rounded-full bg-amber-500 px-1 text-[10px] font-semibold tabular-nums text-white">
                {issues}
              </span>
            ) : null}
          </button>
        );
      })}
    </nav>
  );
}

/** 环形概率图：每段弧长即公示概率，「谢谢参与」用中性色 */
function ProbabilityRing({ prizes, probabilities, winRate }: { prizes: PrizeDraft[]; probabilities: number[]; winRate: number }) {
  const radius = 42;
  const circumference = 2 * Math.PI * radius;
  const total = probabilities.reduce((sum, value) => sum + value, 0) || 1;
  // 每段的起点是前面各段弧长之和，先算好再画
  const lengths = probabilities.map((value) => (Math.max(0, value) / total) * circumference);
  const starts = lengths.map((_, index) => lengths.slice(0, index).reduce((sum, length) => sum + length, 0));
  return (
    <div className="relative mx-auto size-36">
      <svg viewBox="0 0 100 100" className="size-full -rotate-90">
        <circle cx="50" cy="50" r={radius} fill="none" strokeWidth="9" className="stroke-muted" />
        {prizes.map((prize, index) => {
          const value = probabilities[index] ?? 0;
          if (value <= 0) return null;
          const length = lengths[index];
          const gap = Math.min(1.2, length / 3);
          const dash = `${Math.max(0, length - gap)} ${circumference - Math.max(0, length - gap)}`;
          return (
            <motion.circle
              key={prize.key}
              cx="50"
              cy="50"
              r={radius}
              fill="none"
              strokeWidth="9"
              className={kindVisual(prize.type).stroke}
              initial={false}
              animate={{ strokeDasharray: dash, strokeDashoffset: -starts[index] }}
              transition={{ duration: 0.3, ease: [0.22, 1, 0.36, 1] }}
            />
          );
        })}
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="font-data text-2xl font-semibold tabular-nums tracking-tight">
          <NumberFlow value={Math.min(100, winRate)} format={{ maximumFractionDigits: 1 }} suffix="%" />
        </span>
        <span className="text-[11px] text-muted-foreground">中奖率</span>
      </div>
    </div>
  );
}

export type InsightStat = { label: string; value: React.ReactNode; hint?: string };

/** 右侧实时概览：环形概率图、关键数字、待处理问题 */
export function InsightPanel({
  prizes,
  probabilities,
  stats,
  issues,
  onJump
}: {
  prizes: PrizeDraft[];
  probabilities: number[];
  stats: InsightStat[];
  issues: FormIssue[];
  onJump: (key: SectionKey) => void;
}) {
  const winRate = prizes.reduce((sum, prize, index) => (prize.type === "none" ? sum : sum + (probabilities[index] ?? 0)), 0);
  return (
    <div className="space-y-3">
      <div className="rounded-[1.25rem] border border-border bg-card p-4">
        <div className="mb-3 flex items-baseline justify-between">
          <span className="text-xs font-semibold">实时概览</span>
          <span className="text-[11px] text-muted-foreground">随草稿更新</span>
        </div>
        <ProbabilityRing prizes={prizes} probabilities={probabilities} winRate={winRate} />
        <dl className="mt-4 grid grid-cols-2 gap-2">
          {stats.map((stat) => (
            <div key={stat.label} className="rounded-xl bg-muted/60 px-3 py-2 dark:bg-muted/30">
              <dt className="truncate text-[11px] text-muted-foreground">{stat.label}</dt>
              <dd className="truncate font-data text-sm font-semibold tabular-nums">{stat.value}</dd>
              {stat.hint ? <dd className="truncate text-[10px] text-muted-foreground">{stat.hint}</dd> : null}
            </div>
          ))}
        </dl>
      </div>

      <IssueList issues={issues} onJump={onJump} />
    </div>
  );
}

/** 待处理问题列表；没有问题时是一行「可以保存」 */
export function IssueList({ issues, onJump, compact }: { issues: FormIssue[]; onJump: (key: SectionKey) => void; compact?: boolean }) {
  if (issues.length === 0) {
    return (
      <div className={cn("flex items-center gap-2 rounded-[1.25rem] border border-emerald-500/25 bg-emerald-500/[0.06] px-4 py-3 text-xs text-emerald-700 dark:text-emerald-400", compact && "border-0 bg-transparent p-0")}>
        <CircleCheck className="size-4 shrink-0" />
        检查通过，可以保存
      </div>
    );
  }
  return (
    <div className={cn("rounded-[1.25rem] border border-amber-500/25 bg-amber-500/[0.05] p-2", compact && "border-0 bg-transparent p-0")}>
      <div className="flex items-center gap-1.5 px-2 pb-1.5 pt-1 text-xs font-semibold text-amber-700 dark:text-amber-400">
        <AlertTriangle className="size-3.5" />
        {issues.length} 项待处理
      </div>
      <ul className="max-h-72 space-y-0.5 overflow-y-auto">
        {issues.map((issue) => (
          <li key={issue.message}>
            <button
              type="button"
              onClick={() => onJump(issueSection(issue))}
              className="w-full rounded-lg px-2 py-1.5 text-left text-[12px] leading-snug text-foreground/80 transition-colors hover:bg-amber-500/10 hover:text-foreground"
            >
              {issue.message}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
