"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { ChartConfig } from "@/components/ui/chart";
import type {
  RiskCatalogEntry, RiskConditionCatalog, RiskLevelCatalog, RiskListTypeCatalog, RiskMetadata,
  RiskSceneCatalog, RiskTemplateSummary,
} from "@/lib/api/types";
import { formatDate, formatDateTime, formatRelative } from "@/lib/datetime";
import { useRiskMetadataQuery } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

/* ══════════════════════════════════════════════════════════
   目录上下文

   场景 / 等级 / 动作 / 条件类型的枚举与参数 schema 全部来自后端
   `/risk/metadata`，整页共用一份。在组件里另抄一份常量表，会让
   「后端新增一种条件类型」变成一次静默的漏项 —— 规则存得进去，
   但表单上没有它的参数字段，配出来的是一条永不命中的规则。
   ══════════════════════════════════════════════════════════ */

type RiskCatalog = {
  metadata: RiskMetadata | undefined;
  isLoading: boolean;
  sceneLabel: (value: string) => string;
  levelLabel: (value: string) => string;
  actionLabel: (value: string) => string;
  conditionLabel: (value: string) => string;
  dimensionLabel: (value: string) => string;
  listKindLabel: (value: string) => string;
  listTypeLabel: (value: string) => string;
  banTargetLabel: (value: string) => string;
  decisionLabel: (value: string) => string;
  modeLabel: (value: string) => string;
  condition: (value: string) => RiskConditionCatalog | undefined;
  scene: (value: string) => RiskSceneCatalog | undefined;
  levels: RiskLevelCatalog[];
  scenes: RiskSceneCatalog[];
  actions: RiskCatalogEntry[];
  conditions: RiskConditionCatalog[];
  dimensions: RiskCatalogEntry[];
  ruleModes: RiskCatalogEntry[];
  listKinds: RiskCatalogEntry[];
  listTypes: RiskListTypeCatalog[];
  banTargets: RiskCatalogEntry[];
  decisionBy: RiskCatalogEntry[];
  templates: RiskTemplateSummary[];
  deviceTags: RiskCatalogEntry[];
  ipTags: RiskCatalogEntry[];
};

/** 规则与策略的「全部场景」取值，与后端 SceneAll 对应 */
export const SCENE_ALL = "*";

const RiskCatalogContext = createContext<RiskCatalog | null>(null);

function labelLookup(entries: Array<{ value: string; label: string }> | undefined) {
  const map = new Map((entries ?? []).map((e) => [e.value, e.label]));
  // 找不到时回落到原值而不是空串：目录漏了一项时，界面上至少还看得到
  // 后端实际存的是什么，而不是一片空白。
  return (value: string) => map.get(value) ?? value;
}

export function RiskCatalogProvider({ children }: { children: ReactNode }) {
  const query = useRiskMetadataQuery();
  const metadata = query.data;

  const value = useMemo<RiskCatalog>(() => {
    const conditions = metadata?.conditionTypes ?? [];
    const conditionMap = new Map(conditions.map((c) => [c.value, c]));
    const scenes = metadata?.scenes ?? [];
    const sceneMap = new Map(scenes.map((s) => [s.value, s]));
    const sceneLookup = labelLookup(scenes);
    return {
      metadata,
      isLoading: query.isLoading,
      sceneLabel: (v: string) => (v === SCENE_ALL ? "全部场景" : sceneLookup(v)),
      levelLabel: labelLookup(metadata?.levels),
      actionLabel: labelLookup(metadata?.actions),
      conditionLabel: labelLookup(conditions),
      dimensionLabel: labelLookup(metadata?.dimensions),
      listKindLabel: labelLookup(metadata?.listKinds),
      listTypeLabel: labelLookup(metadata?.listTypes),
      banTargetLabel: labelLookup(metadata?.banTargets),
      decisionLabel: labelLookup(metadata?.decisionBy),
      modeLabel: labelLookup(metadata?.ruleModes),
      condition: (v: string) => conditionMap.get(v),
      scene: (v: string) => sceneMap.get(v),
      levels: metadata?.levels ?? [],
      scenes,
      actions: metadata?.actions ?? [],
      conditions,
      dimensions: metadata?.dimensions ?? [],
      ruleModes: metadata?.ruleModes ?? [],
      listKinds: metadata?.listKinds ?? [],
      listTypes: metadata?.listTypes ?? [],
      banTargets: metadata?.banTargets ?? [],
      decisionBy: metadata?.decisionBy ?? [],
      templates: metadata?.templates ?? [],
      deviceTags: metadata?.deviceTags ?? [],
      ipTags: metadata?.ipTags ?? [],
    };
  }, [metadata, query.isLoading]);

  return <RiskCatalogContext.Provider value={value}>{children}</RiskCatalogContext.Provider>;
}

export function useRiskCatalog(): RiskCatalog {
  const ctx = useContext(RiskCatalogContext);
  if (!ctx) throw new Error("useRiskCatalog 必须在 RiskCatalogProvider 内使用");
  return ctx;
}

/* ══════════════════════════════════════════════════════════
   配色

   一套颜色贯穿徽标、图表与图例。等级色与动作色刻意区分色系：
   等级回答「风险有多高」，动作回答「拦了没有」，用同一套色系
   会让「高危但放行」和「低危但拦截」这两种最值得注意的组合
   在图上看起来一模一样。

   每个色值都给 light / dark 两档 —— shadcn chart 的 ChartStyle
   会据此生成 `--color-<key>` 变量，深色下不会糊成一团。
   ══════════════════════════════════════════════════════════ */

type ThemedColor = { light: string; dark: string };

export const LEVEL_COLORS: Record<string, ThemedColor> = {
  normal: { light: "#a1a1aa", dark: "#71717a" },
  low: { light: "#38bdf8", dark: "#0ea5e9" },
  medium: { light: "#fbbf24", dark: "#f59e0b" },
  high: { light: "#fb923c", dark: "#f97316" },
  critical: { light: "#f43f5e", dark: "#e11d48" },
};

export const ACTION_COLORS: Record<string, ThemedColor> = {
  pass: { light: "#34d399", dark: "#10b981" },
  captcha: { light: "#60a5fa", dark: "#3b82f6" },
  review: { light: "#fbbf24", dark: "#f59e0b" },
  block: { light: "#fb7185", dark: "#f43f5e" },
  ban: { light: "#be123c", dark: "#9f1239" },
};

/**
 * 风险维度色。维度是「问题出在哪一面」，与等级（多严重）、动作（怎么处置）
 * 再分一套色系，三者同屏出现时不会互相混淆。
 */
export const DIMENSION_COLORS: Record<string, ThemedColor> = {
  network: { light: "#0ea5e9", dark: "#38bdf8" },
  device: { light: "#8b5cf6", dark: "#a78bfa" },
  account: { light: "#e11d48", dark: "#fb7185" },
  behavior: { light: "#f59e0b", dark: "#fbbf24" },
  geo: { light: "#14b8a6", dark: "#2dd4bf" },
  client: { light: "#64748b", dark: "#94a3b8" },
  list: { light: "#be123c", dark: "#f43f5e" },
  custom: { light: "#6366f1", dark: "#818cf8" },
};

export const LIST_KIND_COLORS: Record<string, ThemedColor> = {
  block: { light: "#f43f5e", dark: "#fb7185" },
  allow: { light: "#10b981", dark: "#34d399" },
  watch: { light: "#f59e0b", dark: "#fbbf24" },
};

export const DECISION_COLORS: Record<string, ThemedColor> = {
  allowlist: { light: "#10b981", dark: "#34d399" },
  blocklist: { light: "#f43f5e", dark: "#fb7185" },
  rule: { light: "#f59e0b", dark: "#fbbf24" },
  policy: { light: "#6366f1", dark: "#818cf8" },
  none: { light: "#a1a1aa", dark: "#71717a" },
};

export function dimensionChartConfig(dimensions: RiskCatalogEntry[]): ChartConfig {
  const config: ChartConfig = {};
  for (const dim of dimensions) {
    config[dim.value] = { label: dim.label, theme: DIMENSION_COLORS[dim.value] ?? CATEGORICAL_COLORS[0] };
  }
  return config;
}

export function decisionChartConfig(entries: RiskCatalogEntry[]): ChartConfig {
  const config: ChartConfig = {};
  for (const entry of entries) {
    config[entry.value] = { label: entry.label, theme: DECISION_COLORS[entry.value] ?? CATEGORICAL_COLORS[0] };
  }
  return config;
}

/** 分类色轮：给场景 / 国家这类没有固有语义的维度用。 */
export const CATEGORICAL_COLORS: ThemedColor[] = [
  { light: "#6366f1", dark: "#818cf8" },
  { light: "#0ea5e9", dark: "#38bdf8" },
  { light: "#14b8a6", dark: "#2dd4bf" },
  { light: "#f59e0b", dark: "#fbbf24" },
  { light: "#ec4899", dark: "#f472b6" },
  { light: "#8b5cf6", dark: "#a78bfa" },
];

export function levelChartConfig(levels: RiskLevelCatalog[]): ChartConfig {
  const config: ChartConfig = {};
  for (const level of levels) {
    config[level.value] = { label: level.label, theme: LEVEL_COLORS[level.value] ?? CATEGORICAL_COLORS[0] };
  }
  return config;
}

export function actionChartConfig(actions: RiskCatalogEntry[]): ChartConfig {
  const config: ChartConfig = {};
  for (const action of actions) {
    config[action.value] = { label: action.label, theme: ACTION_COLORS[action.value] ?? CATEGORICAL_COLORS[0] };
  }
  return config;
}

/** 由 tone 推导 Badge 变体，让后端的语义提示直接驱动前端着色。 */
export function toneVariant(tone?: string): "outline" | "success" | "warning" | "danger" | "info" {
  switch (tone) {
    case "success": return "success";
    case "warning": return "warning";
    case "danger": return "danger";
    case "info": return "info";
    default: return "outline";
  }
}

/* ══════════════════════════════════════════════════════════
   徽标
   ══════════════════════════════════════════════════════════ */

export function LevelBadge({ level, className }: { level: string; className?: string }) {
  const { levels } = useRiskCatalog();
  const entry = levels.find((l) => l.value === level);
  return (
    <Badge variant={toneVariant(entry?.tone)} size="sm" className={cn("gap-1", className)}>
      <span className="size-1.5 rounded-full" style={{ background: LEVEL_COLORS[level]?.light ?? "#a1a1aa" }} />
      {entry?.label ?? level}
    </Badge>
  );
}

export function ActionBadge({ action, className }: { action: string; className?: string }) {
  const { actions } = useRiskCatalog();
  const entry = actions.find((a) => a.value === action);
  return (
    <Badge variant={toneVariant(entry?.tone)} size="sm" className={className}>
      {entry?.label ?? action}
    </Badge>
  );
}

export function TagBadge({ tag, kind, className }: { tag: string; kind: "device" | "ip"; className?: string }) {
  const { deviceTags, ipTags } = useRiskCatalog();
  const entry = (kind === "device" ? deviceTags : ipTags).find((t) => t.value === tag);
  return (
    <Badge variant={toneVariant(entry?.tone)} size="sm" className={className}>
      {entry?.label ?? tag}
    </Badge>
  );
}

export function SceneBadge({ scene, className }: { scene: string; className?: string }) {
  const { sceneLabel } = useRiskCatalog();
  return (
    <Badge variant={scene === SCENE_ALL ? "secondary" : "outline"} size="sm" className={className}>
      {sceneLabel(scene)}
    </Badge>
  );
}

function Dot({ color }: { color?: ThemedColor }) {
  return <span className="size-1.5 shrink-0 rounded-full" style={{ background: color?.light ?? "#a1a1aa" }} />;
}

export function DimensionBadge({ dimension, className }: { dimension?: string; className?: string }) {
  const { dimensionLabel } = useRiskCatalog();
  if (!dimension) return null;
  return (
    <Badge variant="outline" size="sm" className={cn("gap-1", className)}>
      <Dot color={DIMENSION_COLORS[dimension]} />
      {dimensionLabel(dimension)}
    </Badge>
  );
}

export function ListKindBadge({ kind, className }: { kind: string; className?: string }) {
  const { listKinds } = useRiskCatalog();
  const entry = listKinds.find((k) => k.value === kind);
  return <Badge variant={toneVariant(entry?.tone)} size="sm" className={className}>{entry?.label ?? kind}</Badge>;
}

export function DecisionBadge({ decisionBy, className }: { decisionBy?: string; className?: string }) {
  const { decisionBy: entries } = useRiskCatalog();
  if (!decisionBy || decisionBy === "none") return null;
  const entry = entries.find((e) => e.value === decisionBy);
  return (
    <Badge variant="outline" size="sm" className={cn("gap-1", className)}>
      <Dot color={DECISION_COLORS[decisionBy]} />
      {entry?.label ?? decisionBy}
    </Badge>
  );
}

export function ModeBadge({ mode, className }: { mode?: string; className?: string }) {
  const { ruleModes } = useRiskCatalog();
  if (!mode || mode === "enforce") return null;
  const entry = ruleModes.find((m) => m.value === mode);
  return <Badge variant={toneVariant(entry?.tone ?? "info")} size="sm" className={className}>{entry?.label ?? mode}</Badge>;
}

/**
 * 维度得分迷你条：各维度按得分占比分段。表格里一眼看出「这 75 分是网络给的还是账号给的」。
 */
export function DimensionBar({ scores, className, width = 72 }: {
  scores?: Record<string, number> | null; className?: string; width?: number;
}) {
  const { dimensionLabel } = useRiskCatalog();
  const entries = Object.entries(scores ?? {}).filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1]);
  const total = entries.reduce((sum, [, v]) => sum + v, 0);
  if (total === 0) {
    return <span className={cn("block h-1.5 rounded-full bg-muted", className)} style={{ width }} />;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn("flex h-1.5 overflow-hidden rounded-full bg-muted", className)} style={{ width }}>
          {entries.map(([dim, value]) => (
            <span key={dim} style={{ width: `${(value / total) * 100}%`, background: DIMENSION_COLORS[dim]?.light ?? "#a1a1aa" }} />
          ))}
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <div className="space-y-0.5 text-[11px]">
          {entries.map(([dim, value]) => (
            <div key={dim} className="flex items-center gap-2">
              <Dot color={DIMENSION_COLORS[dim]} />
              <span className="flex-1">{dimensionLabel(dim)}</span>
              <span className="font-mono tabular-nums">+{value}</span>
            </div>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

/** 维度得分明细：每个维度一行条形，详情抽屉与模拟结果共用。 */
export function DimensionBreakdown({ scores }: { scores?: Record<string, number> | null }) {
  const { dimensions } = useRiskCatalog();
  const values = scores ?? {};
  const max = Math.max(1, ...Object.values(values));
  const rows = dimensions.filter((d) => (values[d.value] ?? 0) > 0);
  if (rows.length === 0) {
    return <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">各维度均无得分</p>;
  }
  return (
    <div className="space-y-1.5">
      {rows.map((dim) => {
        const value = values[dim.value] ?? 0;
        return (
          <div key={dim.value} className="flex items-center gap-2 text-[11px]">
            <span className="w-14 shrink-0 text-muted-foreground">{dim.label}</span>
            <span className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
              <span className="block h-full rounded-full" style={{
                width: `${(value / max) * 100}%`, background: DIMENSION_COLORS[dim.value]?.light ?? "#a1a1aa",
              }} />
            </span>
            <span className="w-10 shrink-0 text-right font-mono tabular-nums">+{value}</span>
          </div>
        );
      })}
    </div>
  );
}

/* ══════════════════════════════════════════════════════════
   格式化
   ══════════════════════════════════════════════════════════ */

const numberFormatter = new Intl.NumberFormat("zh-CN");

export function fmtNumber(value: number | undefined | null) {
  return numberFormatter.format(value ?? 0);
}

export function fmtPercent(value: number | undefined | null, digits = 1) {
  return `${((value ?? 0) * 100).toFixed(digits)}%`;
}

export function fmtDateTime(iso?: string | null) {
  return formatDateTime(iso, "--");
}

export function fmtDate(iso?: string | null) {
  return formatDate(iso, "--");
}

/** 图表 X 轴的时间刻度：按小时聚合时不显示年份，按天聚合时不显示时分。 */
export function fmtBucket(iso: string, bucket: string) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  if (bucket === "day") {
    return date.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" });
  }
  return date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false });
}

/** 相对时间。「3 分钟前」比一个绝对时间戳更能说明「现在还在打」。 */
export function fmtRelative(iso?: string | null) {
  return formatRelative(iso, "--");
}

/** 到期时间：空为永久，已过期标明 */
export function fmtExpiry(iso?: string | null) {
  if (!iso) return "永久";
  const at = new Date(iso).getTime();
  if (Number.isNaN(at)) return "--";
  if (at <= Date.now()) return "已过期";
  return formatDateTime(iso, "--");
}

/** 中间省略。设备指纹动辄 64 位十六进制，头尾各留一截才认得出是不是同一台。 */
export function ellipsisMiddle(text: string, head = 10, tail = 6) {
  if (!text) return "--";
  if (text.length <= head + tail + 1) return text;
  return `${text.slice(0, head)}…${text.slice(-tail)}`;
}

/** 处置 / 名单的时长预设（秒，0 = 永久） */
export const DURATION_PRESETS = [
  { value: 3600, label: "1 小时" },
  { value: 86400, label: "1 天" },
  { value: 7 * 86400, label: "7 天" },
  { value: 30 * 86400, label: "30 天" },
  { value: 0, label: "永久" },
] as const;

export function fmtDuration(seconds: number) {
  if (seconds <= 0) return "--";
  if (seconds < 60) return `${seconds} 秒`;
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)} 小时`;
  return `${Math.round(seconds / 86400)} 天`;
}

/* ══════════════════════════════════════════════════════════
   小组件
   ══════════════════════════════════════════════════════════ */

/** 图表加载占位。给固定高度，避免数据到达时整页跳动。 */
export function ChartSkeleton({ height = 240 }: { height?: number }) {
  return <Skeleton className="w-full rounded-lg" style={{ height }} />;
}

/**
 * 卡片内的空态。ui/data-state 里的 EmptyState 是整页级的（min-h-56 + p-8），
 * 塞进图表卡片会把卡片撑成两倍高，一屏放不下三张。
 */
export function InlineEmpty({ text, height = 180 }: { text: string; height?: number }) {
  return (
    <div className="flex items-center justify-center rounded-lg border border-dashed text-xs text-muted-foreground"
      style={{ minHeight: height }}>
      {text}
    </div>
  );
}

/**
 * 环比变化。只给绝对数看不出今天是不是异常，而那正是大盘的用途。
 */
export function DeltaHint({ current, previous, invert }: { current: number; previous: number; invert?: boolean }) {
  if (previous === 0) {
    return <span className="text-[10px] text-muted-foreground">{current > 0 ? "无对比数据" : "—"}</span>;
  }
  const ratio = (current - previous) / previous;
  if (Math.abs(ratio) < 0.005) {
    return <span className="text-[10px] text-muted-foreground">较上期持平</span>;
  }
  const rising = ratio > 0;
  // invert 用于「拦截数上升是坏事」这类指标：涨了要标红而不是标绿。
  const bad = invert ? rising : !rising;
  return (
    <span className={cn("text-[10px] font-medium tabular-nums", bad ? "text-rose-600 dark:text-rose-400" : "text-emerald-600 dark:text-emerald-400")}>
      较上期 {rising ? "+" : "-"}{Math.abs(ratio * 100).toFixed(1)}%
    </span>
  );
}

/** 键值对行，详情抽屉里大量复用。 */
export function DetailRow({ label, children, mono }: { label: string; children: ReactNode; mono?: boolean }) {
  return (
    <div className="flex items-start gap-3 py-1.5 text-xs">
      <span className="w-24 shrink-0 text-muted-foreground">{label}</span>
      <span className={cn("min-w-0 flex-1 break-all", mono && "font-mono")}>{children}</span>
    </div>
  );
}

export function SectionTitle({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <h4 className={cn("text-[10px] font-semibold uppercase tracking-widest text-muted-foreground", className)}>
      {children}
    </h4>
  );
}

/** 页内分段切换（大盘趋势视图、规则策略分区、实体画像类型共用） */
export function SegmentedControl<T extends string>({ value, options, onChange, className }: {
  value: T;
  options: ReadonlyArray<{ key: T; label: string; count?: number }>;
  onChange: (next: T) => void;
  className?: string;
}) {
  return (
    <div className={cn("flex w-fit items-center gap-0.5 rounded-lg border bg-muted/50 p-0.5", className)}>
      {options.map((option) => (
        <button key={option.key} type="button" onClick={() => onChange(option.key)}
          className={cn("flex items-center gap-1 rounded-md px-2.5 py-1 text-[11px] transition-colors",
            value === option.key ? "bg-background font-medium shadow-sm" : "text-muted-foreground hover:text-foreground")}>
          {option.label}
          {option.count != null && <span className="font-mono tabular-nums text-muted-foreground">{option.count}</span>}
        </button>
      ))}
    </div>
  );
}

export function Pagination({ page, totalPages, total, onChange }: {
  page: number; totalPages: number; total: number; onChange: (page: number) => void;
}) {
  return (
    <div className="flex items-center justify-between px-1 text-xs text-muted-foreground">
      <span className="tabular-nums">共 {fmtNumber(total)} 条，第 {page} / {totalPages} 页</span>
      <div className="flex items-center gap-1">
        <Button variant="outline" size="sm" className="h-7 px-2" disabled={page <= 1}
          onClick={() => onChange(page - 1)}>
          <ChevronLeft className="size-3.5" />
        </Button>
        <Button variant="outline" size="sm" className="h-7 px-2" disabled={page >= totalPages}
          onClick={() => onChange(page + 1)}>
          <ChevronRight className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}

/** 表格加载占位行 */
export function RowsSkeleton({ rows = 8, height = 44 }: { rows?: number; height?: number }) {
  return (
    <div className="space-y-1.5">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="w-full rounded-lg" style={{ height }} />
      ))}
    </div>
  );
}
