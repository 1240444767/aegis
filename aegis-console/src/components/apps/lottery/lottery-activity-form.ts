import { toLocalInput } from "@/components/apps/points-store/points-store-shared";
import type {
  LotteryActivityPayload,
  LotteryAdminActivity,
  LotteryCatalog,
  LotteryReward,
  LotteryRewardSpec,
  LotteryStatus,
  LotteryUIMode
} from "@/lib/api/lottery";

/**
 * 活动编辑器的草稿模型：种子、载荷与校验。
 *
 * 校验照抄后端 `lottery.ValidateInput` 的规则与说法，目的是在保存前就能看到「还差什么」；
 * 它只是提示，最终以后端为准（40000 的原因原样 toast）。上界常量与后端 `lottery/types.go` 一致。
 */

export const LIMITS = {
  name: 32,
  description: 200,
  rules: 2000,
  prizeName: 16,
  freeDaily: 100,
  pointsDaily: 1000,
  pointsCost: 1_000_000,
  weight: 1_000_000,
  quantity: 100_000_000,
  pity: 1000,
  note: 500
} as const;

/** 一个奖品的草稿。数字一律按字符串存：存 number 的输入框清不空。 */
export type PrizeDraft = {
  /** 草稿内的行键，与后端 id 无关 */
  key: number;
  /** 后端奖品 id，0 为新建；更新时带回去才能保留已抽中数量 */
  id: number;
  name: string;
  type: string;
  amount: string;
  refId: number;
  tag: string;
  text: string;
  imageUrl: string;
  unlimited: boolean;
  quantity: string;
  /** 已抽中数量（只读） */
  used: number;
  weight: string;
  fallback: boolean;
};

export type ActivityForm = {
  name: string;
  description: string;
  rules: string;
  uiMode: LotteryUIMode;
  status: LotteryStatus;
  startTime: string;
  endTime: string;
  freeDaily: string;
  pointsCost: string;
  pointsDaily: string;
  videoScene: string;
  pity: boolean;
  pityThreshold: string;
  /** 保底奖的行键：按行键记，奖品挪位置时保底跟着奖品走 */
  pityKey: number | null;
  sortOrder: string;
  /** 下标即位置：九宫格为外圈顺时针 0–7，转盘为从顶部起顺时针 */
  prizes: PrizeDraft[];
};

export type FormIssue = { scope: "basic" | "prizes"; message: string };

export const nextPrizeKey = (prizes: PrizeDraft[]) => prizes.reduce((max, prize) => Math.max(max, prize.key), 0) + 1;

export function blankPrize(key: number, type: string, amount: number, weight: number): PrizeDraft {
  return {
    key,
    id: 0,
    name: "",
    type,
    amount: String(amount),
    refId: 0,
    tag: "",
    text: "",
    imageUrl: "",
    unlimited: true,
    quantity: "100",
    used: 0,
    weight: String(weight),
    fallback: false
  };
}

/** 新活动的默认奖盘：8 格、权重合计 100，全部不限库存，「谢谢参与」作兜底 */
function defaultPrizes(): PrizeDraft[] {
  const rows: Array<[string, number, number]> = [
    ["points", 5, 20],
    ["none", 0, 30],
    ["points", 10, 15],
    ["experience", 20, 10],
    ["points", 50, 3],
    ["lottery_draws", 1, 7],
    ["points", 20, 5],
    ["experience", 50, 10]
  ];
  return rows.map(([type, amount, weight], index) => ({
    ...blankPrize(index + 1, type, amount, weight),
    fallback: type === "none"
  }));
}

export function seedForm(activity?: LotteryAdminActivity | null): ActivityForm {
  if (!activity) {
    return {
      name: "",
      description: "",
      rules: "",
      uiMode: "grid",
      status: "draft",
      startTime: "",
      endTime: "",
      freeDaily: "1",
      pointsCost: "0",
      pointsDaily: "0",
      videoScene: "",
      pity: false,
      pityThreshold: "10",
      pityKey: null,
      sortOrder: "0",
      prizes: defaultPrizes()
    };
  }
  const prizes = [...activity.prizes]
    .sort((a, b) => a.position - b.position)
    .map<PrizeDraft>((prize, index) => ({
      key: index + 1,
      id: prize.id,
      name: prize.name,
      type: prize.reward?.type || prize.kind,
      amount: String(prize.reward?.amount ?? 0),
      refId: prize.reward?.refId ?? 0,
      tag: prize.reward?.tag ?? "",
      text: prize.reward?.text ?? "",
      imageUrl: prize.imageUrl ?? "",
      unlimited: prize.quantity < 0,
      quantity: String(prize.quantity < 0 ? Math.max(prize.used, 100) : prize.quantity),
      used: prize.used,
      weight: String(prize.weight),
      fallback: prize.fallback
    }));
  const pityPrize = activity.prizes.find((prize) => prize.id === activity.pityPrizeId);
  const pityKey = pityPrize ? prizes.find((draft) => draft.id === pityPrize.id)?.key ?? null : null;
  return {
    name: activity.name,
    description: activity.description ?? "",
    rules: activity.rules ?? "",
    uiMode: activity.uiMode,
    status: activity.status,
    startTime: toLocalInput(activity.startTime),
    endTime: toLocalInput(activity.endTime),
    freeDaily: String(activity.freeDaily),
    pointsCost: String(activity.pointsCost),
    pointsDaily: String(activity.pointsDaily),
    videoScene: activity.videoScene ?? "",
    pity: activity.pityThreshold > 0,
    pityThreshold: String(activity.pityThreshold > 0 ? activity.pityThreshold : 10),
    pityKey,
    sortOrder: String(activity.sortOrder),
    prizes
  };
}

const toInt = (value: string) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? Math.trunc(parsed) : 0;
};

/** 草稿 → 奖品发的东西：只带取值形态需要的字段 */
export function rewardOf(prize: PrizeDraft, kinds: LotteryRewardSpec[]): LotteryReward {
  const spec = kinds.find((item) => item.type === prize.type);
  switch (spec?.value) {
    case "":
      return { type: prize.type };
    case "ref":
      return { type: prize.type, refId: prize.refId };
    case "feature":
      return { type: prize.type, tag: prize.tag, amount: toInt(prize.amount) };
    case "text":
      return { type: prize.type, text: prize.text.trim() };
    default:
      return prize.type === "none" ? { type: "none" } : { type: prize.type, amount: toInt(prize.amount) };
  }
}

/** 概率与库存口径的数值形态（与列表上的奖品同形），供公示概率计算 */
export function weightedOf(prize: PrizeDraft) {
  return {
    weight: Math.max(0, toInt(prize.weight)),
    quantity: prize.unlimited ? -1 : Math.max(0, toInt(prize.quantity)),
    used: prize.used
  };
}

export function toPayload(form: ActivityForm, kinds: LotteryRewardSpec[]): LotteryActivityPayload {
  const pityIndex = form.pity ? form.prizes.findIndex((prize) => prize.key === form.pityKey) : -1;
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    rules: form.rules.trim(),
    uiMode: form.uiMode,
    status: form.status,
    startTime: form.startTime ? new Date(form.startTime).toISOString() : null,
    endTime: form.endTime ? new Date(form.endTime).toISOString() : null,
    freeDaily: toInt(form.freeDaily),
    pointsCost: toInt(form.pointsCost),
    pointsDaily: toInt(form.pointsDaily),
    videoScene: form.videoScene,
    pityThreshold: form.pity ? toInt(form.pityThreshold) : 0,
    pityPosition: pityIndex >= 0 ? pityIndex : null,
    sortOrder: toInt(form.sortOrder),
    prizes: form.prizes.map((prize, index) => {
      const weighted = weightedOf(prize);
      return {
        id: prize.id,
        name: prize.name.trim(),
        reward: rewardOf(prize, kinds),
        imageUrl: prize.imageUrl.trim(),
        quantity: weighted.quantity,
        weight: weighted.weight,
        position: index,
        fallback: prize.fallback
      };
    })
  };
}

const runes = (value: string) => Array.from(value).length;

/** 与后端 `ValidateInput` / `validatePrizes` 同一套规则与说法 */
export function validateForm(form: ActivityForm, catalog?: LotteryCatalog | null): FormIssue[] {
  const issues: FormIssue[] = [];
  const basic = (message: string) => issues.push({ scope: "basic", message });
  const prizes = (message: string) => issues.push({ scope: "prizes", message });

  const name = form.name.trim();
  if (!name) basic("活动名称不能为空");
  else if (runes(name) > LIMITS.name) basic(`活动名称不能超过 ${LIMITS.name} 字`);
  if (runes(form.description.trim()) > LIMITS.description) basic(`活动简介不能超过 ${LIMITS.description} 字`);
  if (runes(form.rules.trim()) > LIMITS.rules) basic(`活动规则不能超过 ${LIMITS.rules} 字`);
  if (form.startTime && form.endTime && new Date(form.endTime).getTime() <= new Date(form.startTime).getTime()) {
    basic("结束时间需要晚于开始时间");
  }
  const freeDaily = toInt(form.freeDaily);
  if (freeDaily < 0 || freeDaily > LIMITS.freeDaily) basic(`每日免费次数需要在 0–${LIMITS.freeDaily} 之间`);
  const pointsCost = toInt(form.pointsCost);
  if (pointsCost < 0 || pointsCost > LIMITS.pointsCost) basic(`每次消耗积分需要在 0–${LIMITS.pointsCost} 之间`);
  const pointsDaily = toInt(form.pointsDaily);
  if (pointsDaily < 0 || pointsDaily > LIMITS.pointsDaily) basic(`积分抽奖每日上限需要在 0–${LIMITS.pointsDaily} 之间`);

  const gridCount = catalog?.gridPrizeCount ?? 8;
  const wheelMin = catalog?.wheelMinPrizes ?? 2;
  const wheelMax = catalog?.wheelMaxPrizes ?? 12;
  const count = form.prizes.length;
  if (form.uiMode === "grid" && count !== gridCount) {
    prizes(`九宫格需要恰好 ${gridCount} 个奖品，当前 ${count} 个`);
  }
  if (form.uiMode === "wheel" && (count < wheelMin || count > wheelMax)) {
    prizes(`转盘需要 ${wheelMin}–${wheelMax} 个奖品，当前 ${count} 个`);
  }

  const kinds = catalog?.kinds ?? [];
  let totalWeight = 0;
  let unlimited = false;
  let fallbacks = 0;
  form.prizes.forEach((prize, index) => {
    const label = `第 ${index + 1} 个奖品`;
    if (runes(prize.name.trim()) > LIMITS.prizeName) prizes(`${label}的名称不能超过 ${LIMITS.prizeName} 字`);
    const image = prize.imageUrl.trim();
    if (image && !/^https?:\/\//.test(image)) prizes(`${label}的图片地址需以 http(s):// 开头`);

    const spec = kinds.find((item) => item.type === prize.type);
    if (catalog && !spec) prizes(`${label}：未登记的奖品类型：${prize.type}`);
    if (spec?.value === "amount") {
      const amount = toInt(prize.amount);
      if ((spec.min !== undefined && amount < spec.min) || (spec.max !== undefined && amount > spec.max)) {
        prizes(`${label}：${spec.label}需要在 ${spec.min ?? 0}–${spec.max ?? amount} 之间`);
      }
    }
    if (spec?.value === "ref" && !prize.refId) prizes(`${label}：请选择会员套餐`);
    if (spec?.value === "feature") {
      if (!prize.tag) prizes(`${label}：请选择功能`);
      if (toInt(prize.amount) < 0) prizes(`${label}：天数不能为负`);
    }
    if (spec?.value === "text" && !prize.text.trim()) prizes(`${label}：请填写发放内容`);

    const weight = toInt(prize.weight);
    if (weight < 0 || weight > LIMITS.weight) prizes(`${label}的权重需要在 0–${LIMITS.weight} 之间`);
    if (!prize.unlimited) {
      const quantity = toInt(prize.quantity);
      if (quantity > LIMITS.quantity) prizes(`${label}的库存不能超过 ${LIMITS.quantity}`);
      if (quantity < prize.used) prizes(`${label}的库存不能少于已抽中的 ${prize.used} 份`);
    }
    totalWeight += Math.max(0, weight);
    if (prize.unlimited) unlimited = true;
    if (prize.fallback) {
      fallbacks += 1;
      if (!prize.unlimited) prizes(`${label}是兜底奖，库存必须不限`);
    }
  });
  if (fallbacks > 1) prizes("最多设置一个兜底奖");
  if (count > 0 && totalWeight <= 0) prizes("奖品的权重合计必须大于 0");
  if (count > 0 && !unlimited) prizes("至少需要一个不限库存的奖品（通常是「谢谢参与」）");

  if (form.pity) {
    const threshold = toInt(form.pityThreshold);
    if (threshold < 0 || threshold > LIMITS.pity) prizes(`保底次数需要在 0–${LIMITS.pity} 之间`);
    // 开关打开时 0 不当作「关闭」：那是开关的事，这里要一个真正的次数
    else if (threshold < 2) prizes(threshold === 1 ? "保底次数至少为 2（为 1 等于每次必中保底奖）" : "保底次数至少为 2");
    const pityPrize = form.prizes.find((prize) => prize.key === form.pityKey);
    if (threshold > 0) {
      if (!pityPrize) prizes("开启保底时需要选择保底奖品");
      else if (pityPrize.type === "none") prizes("保底奖品不能是「谢谢参与」");
    }
  }
  return issues;
}
