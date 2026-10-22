import type { LotteryCatalog, LotteryStrategy, LotteryStrategyLimits } from "@/lib/api/lottery";

/**
 * 出奖策略的草稿模型。数字按字符串存（输入框要能清空），开关单独存：关掉一项时保留填过的数，
 * 再打开不用重填。载荷只带打开的项，与后端 `lottery.NormalizeStrategy` 的口径一致。
 * 规则见 docs/lottery.md「出奖策略」。
 */

/** 后端目录未下发时的回落上界（与 `lottery/strategy.go`、`behavior.go`、`simulate.go` 一致） */
export const STRATEGY_LIMITS: LotteryStrategyLimits = {
  rules: 12,
  ruleFactor: 100,
  softPityStep: 50,
  cooldownDraws: 100,
  pacingBoost: 5,
  behaviorBoost: 10,
  prizeLimit: 1_000_000,
  simulationUsers: 5000,
  simulationEach: 1000,
  simulationDraws: 200_000
};

export type RuleDraft = {
  key: number;
  name: string;
  when: string;
  kinds: string[];
  /** 奖盘上的位置（下标） */
  positions: number[];
  factor: string;
  disabled: boolean;
};

export type SignalDraft = { weight: string; half: string };

export type StrategyDraft = {
  softPity: boolean;
  softPityStart: string;
  softPityStep: string;
  pacing: boolean;
  /** 提前量，百分比 */
  pacingSlack: string;
  pacingBoost: string;
  cooldown: boolean;
  cooldownDraws: string;
  cooldownFactor: string;
  budget: boolean;
  budgetPoints: string;
  risk: boolean;
  riskDevice: string;
  riskIp: string;
  behavior: boolean;
  behaviorMin: string;
  behaviorMax: string;
  behaviorKinds: string[];
  /** 按信号键；不在表里的信号按推荐权重。customSignals 为假时整张表不下发（用推荐权重） */
  customSignals: boolean;
  signals: Record<string, SignalDraft>;
  rules: RuleDraft[];
};

const num = (value: number | undefined, fallback: number) => String(value ?? fallback);

export function seedStrategy(strategy?: LotteryStrategy | null): StrategyDraft {
  const s = strategy ?? {};
  const signals: Record<string, SignalDraft> = {};
  for (const signal of s.behavior?.signals ?? []) {
    signals[signal.key] = { weight: String(signal.weight), half: signal.half ? String(signal.half) : "" };
  }
  return {
    softPity: Boolean(s.softPity),
    softPityStart: num(s.softPity?.start, 5),
    softPityStep: num(s.softPity?.step, 5),
    pacing: Boolean(s.pacing),
    pacingSlack: String(Math.round((s.pacing?.slack ?? 0.05) * 1000) / 10),
    pacingBoost: num(s.pacing?.maxBoost, 1.5),
    cooldown: Boolean(s.cooldown),
    cooldownDraws: num(s.cooldown?.draws, 5),
    cooldownFactor: num(s.cooldown?.factor, 0.3),
    budget: Boolean(s.budget),
    budgetPoints: num(s.budget?.dailyPoints, 10000),
    risk: Boolean(s.risk),
    riskDevice: num(s.risk?.deviceAccounts, 3),
    riskIp: num(s.risk?.ipAccounts, 0),
    behavior: Boolean(s.behavior),
    behaviorMin: num(s.behavior?.minFactor, 0.8),
    behaviorMax: num(s.behavior?.maxBoost, 1.5),
    behaviorKinds: s.behavior?.kinds ?? [],
    customSignals: (s.behavior?.signals?.length ?? 0) > 0,
    signals,
    rules: (s.rules ?? []).map((rule, index) => ({
      key: index + 1,
      name: rule.name ?? "",
      when: rule.when ?? "",
      kinds: rule.kinds ?? [],
      positions: rule.positions ?? [],
      factor: String(rule.factor),
      disabled: Boolean(rule.disabled)
    }))
  };
}

const toNumber = (value: string) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};
const toInt = (value: string) => Math.trunc(toNumber(value));

/** 草稿 → 载荷。positionOf 把规则里的位置按当前奖盘重新映射（奖品挪位置后仍指向同一格的下标） */
export function strategyPayload(draft: StrategyDraft, catalog?: LotteryCatalog | null): LotteryStrategy {
  const out: LotteryStrategy = {};
  if (draft.softPity) out.softPity = { start: toInt(draft.softPityStart), step: toNumber(draft.softPityStep) };
  if (draft.pacing) out.pacing = { slack: toNumber(draft.pacingSlack) / 100, maxBoost: toNumber(draft.pacingBoost) };
  if (draft.cooldown) out.cooldown = { draws: toInt(draft.cooldownDraws), factor: toNumber(draft.cooldownFactor) };
  if (draft.budget) out.budget = { dailyPoints: toInt(draft.budgetPoints) };
  if (draft.risk) out.risk = { deviceAccounts: toInt(draft.riskDevice), ipAccounts: toInt(draft.riskIp) };
  if (draft.behavior) {
    out.behavior = {
      minFactor: toNumber(draft.behaviorMin),
      maxBoost: toNumber(draft.behaviorMax),
      kinds: draft.behaviorKinds.length ? draft.behaviorKinds : undefined
    };
    if (draft.customSignals) {
      out.behavior.signals = (catalog?.signals ?? []).map((spec) => {
        const own = draft.signals[spec.key];
        return {
          key: spec.key,
          weight: own ? toNumber(own.weight) : spec.weight,
          half: own?.half ? toNumber(own.half) : undefined
        };
      });
    }
  }
  if (draft.rules.length) {
    out.rules = draft.rules.map((rule) => ({
      name: rule.name.trim(),
      when: rule.when.trim(),
      kinds: rule.kinds.length ? rule.kinds : undefined,
      positions: rule.positions.length ? rule.positions : undefined,
      factor: toNumber(rule.factor),
      disabled: rule.disabled || undefined
    }));
  }
  return out;
}

/** 与后端 `ValidateStrategy` 同一套范围与说法（表达式本身由后端编译校验） */
export function validateStrategy(draft: StrategyDraft, prizeCount: number, limits: LotteryStrategyLimits): string[] {
  const issues: string[] = [];
  if (draft.softPity) {
    if (toInt(draft.softPityStart) < 1) issues.push("软保底起点至少为 1");
    const step = toNumber(draft.softPityStep);
    if (step <= 0 || step > limits.softPityStep) issues.push(`软保底每次提升需要在 0–${limits.softPityStep} 个百分点之间`);
  }
  if (draft.pacing) {
    const slack = toNumber(draft.pacingSlack);
    if (slack < 0 || slack > 100) issues.push("匀速释放的提前量需要在 0–100% 之间");
    const boost = toNumber(draft.pacingBoost);
    if (boost < 1 || boost > limits.pacingBoost) issues.push(`匀速释放的追赶倍数需要在 1–${limits.pacingBoost} 之间`);
  }
  if (draft.cooldown) {
    const draws = toInt(draft.cooldownDraws);
    if (draws < 1 || draws > limits.cooldownDraws) issues.push(`中奖冷却的次数需要在 1–${limits.cooldownDraws} 之间`);
    const factor = toNumber(draft.cooldownFactor);
    if (factor < 0 || factor >= 1) issues.push("中奖冷却的系数需要在 0–1 之间（不含 1）");
  }
  if (draft.budget && toInt(draft.budgetPoints) <= 0) issues.push("每日积分预算需要大于 0");
  if (draft.risk && toInt(draft.riskDevice) <= 0 && toInt(draft.riskIp) <= 0) issues.push("同设备与同网络账号数上限至少设置一项");
  if (draft.behavior) {
    const min = toNumber(draft.behaviorMin);
    const maxBoost = toNumber(draft.behaviorMax);
    if (min < 0 || min > 1) issues.push("行为分为 0 时的系数需要在 0–1 之间");
    if (maxBoost < min || maxBoost > limits.behaviorBoost) issues.push(`行为分满分时的系数需要在 ${min}–${limits.behaviorBoost} 之间`);
  }
  if (draft.rules.length > limits.rules) issues.push(`自定义规则最多 ${limits.rules} 条`);
  draft.rules.forEach((rule, index) => {
    const label = rule.name.trim() ? `规则「${rule.name.trim()}」` : `第 ${index + 1} 条规则`;
    const factor = toNumber(rule.factor);
    if (factor < 0 || factor > limits.ruleFactor) issues.push(`${label}的系数需要在 0–${limits.ruleFactor} 之间`);
    if (rule.positions.some((position) => position >= prizeCount)) issues.push(`${label}里有超出奖盘的位置`);
  });
  return issues;
}

/** 维度标识 → 中文名（出奖方案与模拟结果里用） */
export const FACTOR_LABEL: Record<string, string> = {
  stock: "库存",
  dailyLimit: "全站每日限量",
  userLimit: "个人上限",
  userDailyLimit: "个人每日上限",
  risk: "设备聚集",
  pacing: "匀速释放",
  budget: "积分预算",
  behavior: "行为画像",
  cooldown: "中奖冷却",
  rule: "规则",
  softPity: "软保底"
};
