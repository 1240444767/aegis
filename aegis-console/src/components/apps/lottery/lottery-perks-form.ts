import type { LotteryPerk, LotteryPerkAudience, LotteryPerkLimits } from "@/lib/api/lottery";

/**
 * 人群特权的草稿模型：种子、载荷与校验。
 *
 * 校验照抄后端 `lottery.ValidatePerks` 的规则与说法；表达式只有服务端能编译，保存时由它报错。
 * 数字按字符串存（输入框清得空）。上界与后端 `lottery/perks.go` 一致，目录下发时以目录为准。
 */

export const PERK_LIMITS: LotteryPerkLimits = {
  perks: 10,
  name: 16,
  extraFree: 20,
  extraPoints: 100,
  pointsDiscount: 90,
  adSkipSlots: 20,
  defaultAdSkipSlots: 5,
  onlineMinutes: 1440,
  winBoost: 3,
  pityCut: 1000
};

/** 目录未下发时的人群（与后端 `PerkAudiences` 一致） */
export const PERK_AUDIENCES: Array<{ key: LotteryPerkAudience; label: string; hint: string }> = [
  { key: "all", label: "全部用户", hint: "每个登录用户" },
  { key: "member", label: "会员", hint: "当前有效的会员（含永久会员）" },
  { key: "lifetime", label: "永久会员", hint: "已开通永久会员" },
  { key: "non_member", label: "非会员", hint: "当前不是会员" },
  { key: "non_lifetime", label: "非永久会员", hint: "不是永久会员（含限时会员）" },
  { key: "ad_free", label: "免广告用户", hint: "免广告时长有效期内的用户" }
];

export type PerkDraft = {
  key: number;
  name: string;
  disabled: boolean;
  audience: LotteryPerkAudience;
  signedToday: boolean;
  onlineMinutes: string;
  when: string;
  extraFree: string;
  adSkip: string;
  adSkipSlots: string;
  pointsDiscount: string;
  extraPoints: string;
  pityCut: string;
  winBoost: string;
};

export type PerkTemplate = Omit<PerkDraft, "key" | "disabled">;

const blank: PerkTemplate = {
  name: "",
  audience: "all",
  signedToday: false,
  onlineMinutes: "0",
  when: "",
  extraFree: "0",
  adSkip: "0",
  adSkipSlots: "0",
  pointsDiscount: "0",
  extraPoints: "0",
  pityCut: "0",
  winBoost: "1"
};

/** 常用特权（添加菜单里的模板） */
export const PERK_TEMPLATES: PerkTemplate[] = [
  { ...blank, name: "永久会员免广告", audience: "lifetime", adSkip: "100", extraFree: "1" },
  { ...blank, name: "会员概率免广告", audience: "member", adSkip: "30" },
  { ...blank, name: "免广告用户免看视频", audience: "ad_free", adSkip: "100" },
  { ...blank, name: "签到送次数", signedToday: true, extraFree: "1" },
  { ...blank, name: "在线半小时", onlineMinutes: "30", extraFree: "1" },
  { ...blank, name: "会员积分折扣", audience: "member", pointsDiscount: "20", extraPoints: "1" },
  { ...blank, name: "永久会员保底提前", audience: "lifetime", pityCut: "2", winBoost: "1.2" },
  { ...blank, name: "新人福利", when: "user.registeredDays < 7", extraFree: "2" }
];

export const blankPerk = (key: number, template?: PerkTemplate): PerkDraft => ({
  key,
  disabled: false,
  ...(template ?? blank)
});

export function seedPerks(perks?: LotteryPerk[] | null): PerkDraft[] {
  return (perks ?? []).map((perk, index) => ({
    key: index + 1,
    name: perk.name ?? "",
    disabled: !!perk.disabled,
    audience: perk.audience || "all",
    signedToday: !!perk.signedToday,
    onlineMinutes: String(perk.onlineMinutes ?? 0),
    when: perk.when ?? "",
    extraFree: String(perk.extraFree ?? 0),
    adSkip: String(perk.adSkip ?? 0),
    adSkipSlots: String(perk.adSkipSlots ?? 0),
    pointsDiscount: String(perk.pointsDiscount ?? 0),
    extraPoints: String(perk.extraPoints ?? 0),
    pityCut: String(perk.pityCut ?? 0),
    winBoost: String(perk.winBoost && perk.winBoost > 0 ? perk.winBoost : 1)
  }));
}

const int = (value: string) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? Math.trunc(parsed) : 0;
};
const num = (value: string) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

export function perksPayload(perks: PerkDraft[]): LotteryPerk[] {
  return perks.map((perk) => {
    const boost = num(perk.winBoost);
    const adSkip = num(perk.adSkip);
    return {
      name: perk.name.trim(),
      disabled: perk.disabled || undefined,
      audience: perk.audience,
      signedToday: perk.signedToday || undefined,
      onlineMinutes: int(perk.onlineMinutes) || undefined,
      when: perk.when.trim() || undefined,
      extraFree: int(perk.extraFree) || undefined,
      adSkip: adSkip || undefined,
      adSkipSlots: adSkip > 0 ? int(perk.adSkipSlots) || undefined : undefined,
      pointsDiscount: int(perk.pointsDiscount) || undefined,
      extraPoints: int(perk.extraPoints) || undefined,
      pityCut: int(perk.pityCut) || undefined,
      winBoost: boost > 1 ? boost : undefined
    };
  });
}

export const perkHasEffect = (perk: PerkDraft) =>
  int(perk.extraFree) > 0 ||
  num(perk.adSkip) > 0 ||
  int(perk.pointsDiscount) > 0 ||
  int(perk.extraPoints) > 0 ||
  int(perk.pityCut) > 0 ||
  num(perk.winBoost) > 1;

/** 与后端 `ValidatePerks` 同一套规则与说法（表达式由服务端编译校验） */
export function validatePerks(perks: PerkDraft[], limits: LotteryPerkLimits = PERK_LIMITS): string[] {
  const issues: string[] = [];
  if (perks.length > limits.perks) issues.push(`人群特权最多 ${limits.perks} 条`);
  perks.forEach((perk, index) => {
    const label = perk.name.trim() ? `特权「${perk.name.trim()}」` : `第 ${index + 1} 条特权`;
    if (Array.from(perk.name.trim()).length > limits.name) issues.push(`${label}的名称不能超过 ${limits.name} 字`);
    const online = int(perk.onlineMinutes);
    if (online < 0 || online > limits.onlineMinutes) issues.push(`${label}的在线时长条件需要在 0–${limits.onlineMinutes} 分钟之间`);
    const extraFree = int(perk.extraFree);
    if (extraFree < 0 || extraFree > limits.extraFree) issues.push(`${label}的额外免费次数需要在 0–${limits.extraFree} 之间`);
    const adSkip = num(perk.adSkip);
    if (adSkip < 0 || adSkip > 100) issues.push(`${label}的免看广告概率需要在 0–100% 之间`);
    const slots = int(perk.adSkipSlots);
    if (slots < 0 || slots > limits.adSkipSlots) issues.push(`${label}的免看广告判定次数需要在 0–${limits.adSkipSlots} 之间`);
    const discount = int(perk.pointsDiscount);
    if (discount < 0 || discount > limits.pointsDiscount) issues.push(`${label}的积分折扣需要在 0–${limits.pointsDiscount}% 之间`);
    const extraPoints = int(perk.extraPoints);
    if (extraPoints < 0 || extraPoints > limits.extraPoints) issues.push(`${label}的积分抽奖额外次数需要在 0–${limits.extraPoints} 之间`);
    const pityCut = int(perk.pityCut);
    if (pityCut < 0 || pityCut > limits.pityCut) issues.push(`${label}的保底提前次数需要在 0–${limits.pityCut} 之间`);
    const boost = num(perk.winBoost);
    if (boost !== 0 && (boost < 1 || boost > limits.winBoost)) issues.push(`${label}的中奖加成需要在 1–${limits.winBoost} 倍之间`);
    if (!perkHasEffect(perk)) issues.push(`${label}没有设置任何效果`);
  });
  return issues;
}

/** 一条特权的效果，一项一句（与 App 上的说明同口径） */
export function perkEffectLines(perk: PerkDraft, limits: LotteryPerkLimits = PERK_LIMITS): string[] {
  const lines: string[] = [];
  const extraFree = int(perk.extraFree);
  if (extraFree > 0) lines.push(`每天多 ${extraFree} 次免费`);
  const adSkip = num(perk.adSkip);
  if (adSkip > 0) {
    const slots = int(perk.adSkipSlots) || limits.defaultAdSkipSlots;
    lines.push(adSkip >= 100 ? "视频抽奖免看广告（逐次解锁）" : `视频抽奖每次 ${adSkip}% 概率免看广告`);
    if (slots !== limits.defaultAdSkipSlots) lines.push(`不限次时判定前 ${slots} 次`);
  }
  const discount = int(perk.pointsDiscount);
  if (discount > 0) lines.push(`积分抽奖 ${(100 - discount) / 10} 折`);
  const extraPoints = int(perk.extraPoints);
  if (extraPoints > 0) lines.push(`积分抽奖每天多 ${extraPoints} 次`);
  const pityCut = int(perk.pityCut);
  if (pityCut > 0) lines.push(`保底提前 ${pityCut} 次`);
  const boost = num(perk.winBoost);
  if (boost > 1) lines.push(`中奖 ×${boost}`);
  return lines;
}

/** 生效条件一句话 */
export function perkConditionLine(perk: PerkDraft, audienceLabel: string): string {
  const parts = [audienceLabel];
  if (perk.signedToday) parts.push("今天已签到");
  const online = int(perk.onlineMinutes);
  if (online > 0) parts.push(`今天在线满 ${online} 分钟`);
  if (perk.when.trim()) parts.push("满足条件表达式");
  return parts.join("，");
}
