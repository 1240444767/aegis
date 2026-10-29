import type { LotterySupreme, LotterySupremeScope } from "@/lib/api/lottery";

/**
 * 至尊加成的草稿模型（应用设置与活动策略共用）。数字按字符串存，空为不设；载荷与后端 `lottery.NormalizeSupreme` 同口径。
 * 规则见 docs/lottery.md「至尊加成」：f' = max(f, min(max(f × 幅度, 保底), 上限))。
 */

/** 与后端 lottery/supreme.go 一致 */
export const SUPREME_LIMITS = { boost: 3, factor: 5, tiers: 20, title: 16 } as const;

export type SupremeTierDraft = {
  key: number;
  tierId: number;
  lifetimeOnly: boolean;
  boost: string;
  floor: string;
  cap: string;
};

export type SupremeDraft = {
  enabled: boolean;
  scope: LotterySupremeScope;
  boost: string;
  floor: string;
  cap: string;
  tiers: SupremeTierDraft[];
  title: string;
};

/** 活动里的至尊加成：跟随应用设置 / 本活动不加成 / 本活动单独设置 */
export type SupremeMode = "inherit" | "off" | "custom";

const text = (value?: number) => (value ? String(value) : "");

export function seedSupreme(supreme?: LotterySupreme | null): SupremeDraft {
  return {
    enabled: Boolean(supreme?.enabled),
    scope: supreme?.scope === "tiers" ? "tiers" : "lifetime",
    boost: supreme ? text(supreme.boost) : "1.5",
    floor: supreme ? text(supreme.floor) : "1",
    cap: supreme ? text(supreme.cap) : "",
    tiers: (supreme?.tiers ?? []).map((tier, index) => ({
      key: index + 1,
      tierId: tier.tierId,
      lifetimeOnly: Boolean(tier.lifetimeOnly),
      boost: text(tier.boost),
      floor: text(tier.floor),
      cap: text(tier.cap)
    })),
    title: supreme?.title ?? ""
  };
}

export function supremeModeOf(supreme?: LotterySupreme | null): SupremeMode {
  if (!supreme) return "inherit";
  return supreme.enabled ? "custom" : "off";
}

const toNumber = (value: string) => {
  const parsed = Number(value);
  return value.trim() && Number.isFinite(parsed) ? parsed : 0;
};

export function supremePayload(draft: SupremeDraft): LotterySupreme {
  const out: LotterySupreme = {
    enabled: draft.enabled,
    scope: draft.scope,
    boost: toNumber(draft.boost) || undefined,
    floor: toNumber(draft.floor) || undefined,
    cap: toNumber(draft.cap) || undefined,
    title: draft.title.trim() || undefined
  };
  if (draft.scope === "tiers" && draft.tiers.length) {
    out.tiers = draft.tiers.map((tier) => ({
      tierId: tier.tierId,
      lifetimeOnly: tier.lifetimeOnly || undefined,
      boost: toNumber(tier.boost) || undefined,
      floor: toNumber(tier.floor) || undefined,
      cap: toNumber(tier.cap) || undefined
    }));
  }
  return out;
}

/** 活动策略里的 fortune.supreme：跟随应用时不写 */
export function activitySupremePayload(mode: SupremeMode, draft: SupremeDraft): LotterySupreme | undefined {
  if (mode === "inherit") return undefined;
  if (mode === "off") return { enabled: false, scope: "lifetime" };
  return supremePayload({ ...draft, enabled: true });
}

type Grade = { boost: number; floor: number; cap: number };

function gradeIssues(label: string, grade: Grade, requireEffect: boolean): string[] {
  const issues: string[] = [];
  if (grade.boost && (grade.boost < 1 || grade.boost > SUPREME_LIMITS.boost)) {
    issues.push(`${label}的加成幅度需要在 1–${SUPREME_LIMITS.boost} 倍之间`);
  }
  if (grade.floor < 0 || grade.floor > SUPREME_LIMITS.factor) issues.push(`${label}的保底运气需要在 0–${SUPREME_LIMITS.factor} 之间`);
  if (grade.cap && (grade.cap < 1 || grade.cap > SUPREME_LIMITS.factor)) issues.push(`${label}的运气上限需要在 1–${SUPREME_LIMITS.factor} 之间`);
  if (grade.cap && grade.floor > grade.cap) issues.push(`${label}的保底运气不能高于上限`);
  if (requireEffect && grade.boost <= 1 && grade.floor <= 0) issues.push(`${label}需要设置加成幅度或保底运气`);
  return issues;
}

/** 与后端 `ValidateSupreme` 同一套范围与说法 */
export function validateSupreme(draft: SupremeDraft): string[] {
  if (!draft.enabled) return [];
  const issues: string[] = [];
  if (draft.title.trim().length > SUPREME_LIMITS.title) issues.push(`至尊加成的名称不能超过 ${SUPREME_LIMITS.title} 字`);
  const top = { boost: toNumber(draft.boost), floor: toNumber(draft.floor), cap: toNumber(draft.cap) };
  if (draft.scope === "lifetime") return [...issues, ...gradeIssues("永久会员", top, true)];
  issues.push(...gradeIssues("其余永久会员", top, false));
  if (draft.tiers.length > SUPREME_LIMITS.tiers) issues.push(`至尊加成最多设置 ${SUPREME_LIMITS.tiers} 个档位`);
  const hasTop = top.boost > 1 || top.floor > 0;
  if (!draft.tiers.length && !hasTop) issues.push("按会员档位分级时至少设置一个档位");
  const seen = new Set<number>();
  draft.tiers.forEach((tier, index) => {
    const label = `第 ${index + 1} 个档位`;
    if (!tier.tierId) issues.push(`${label}没有选择会员档位`);
    else if (seen.has(tier.tierId)) issues.push(`${label}与前面的档位重复`);
    seen.add(tier.tierId);
    issues.push(...gradeIssues(label, { boost: toNumber(tier.boost), floor: toNumber(tier.floor), cap: toNumber(tier.cap) }, true));
  });
  return issues;
}

/** 与后端 SupremeGrade.Apply 同一个式子 */
export function applySupreme(natural: number, boost: number, floor: number, cap: number) {
  const ceiling = cap > 0 ? cap : SUPREME_LIMITS.factor;
  return Math.max(natural, Math.min(Math.max(natural * Math.max(boost, 1), floor), ceiling));
}

/** σ 与一组参数 → 拿到加成的人的系数范围说明 */
export function supremeRangeHint(spread: number | undefined, boost: string, floor: string, cap: string) {
  const b = toNumber(boost);
  const f = toNumber(floor);
  const c = toNumber(cap);
  if (b <= 1 && f <= 0) return "未设置加成幅度或保底运气";
  if (!spread || spread <= 0) return `系数 ×${b > 1 ? b : 1}，保底 ${f || "不设"}，上限 ${c || SUPREME_LIMITS.factor}`;
  const at = (z: number) => Math.exp(spread * z - (spread * spread) / 2);
  const low = applySupreme(at(-2), b, f, c);
  const high = applySupreme(at(2), b, f, c);
  const mid = applySupreme(at(0), b, f, c);
  return `系数 ${at(-2).toFixed(2)}–${at(2).toFixed(2)} 提升至 ${low.toFixed(2)}–${high.toFixed(2)}，平均水平的人 ×${mid.toFixed(2)}`;
}
