"use client";

/**
 * 流水页共用：分类中文名、每日走势补齐、区间分段。
 */

import type { PointLedgerDay } from "@/lib/api/user-ledger";
import { localDateKey } from "@/lib/datetime";

/** 已知分类的中文名；未知分类用该分类最近一笔流水的标题兜底。 */
export const POINT_CATEGORY_LABEL: Record<string, string> = {
  daily_signin: "每日签到",
  admin_adjust: "管理员调整",
  admin_manual: "批量调整",
  points_store: "积分商城",
  points_exchange: "积分兑换",
  invite: "邀请奖励",
  leaderboard: "排行榜奖励",
  lottery: "抽奖",
  lottery_cost: "抽奖消耗",
  lottery_prize: "抽奖奖励",
  vip_bonus: "会员赠送",
  vip_convert: "会员权益折算",
  vip_purchase: "开通会员",
  card_key: "卡密兑换",
  rewarded_ad: "激励广告",
  ai_gateway: "AI 调用",
  ai_gateway_refund: "AI 调用退款",
  register: "注册奖励",
  task: "任务奖励",
  app_function: "云函数"
};

export function categoryLabel(category: string, fallbackTitle?: string) {
  return POINT_CATEGORY_LABEL[category] ?? (fallbackTitle?.trim() || category || "未分类");
}

export type FilledDay = { date: string; label: string; in: number; out: number; count: number; balance: number };

/**
 * 后端只返回有流水的日子；这里按浏览器时区把窗口内每一天补齐，
 * 没有流水的日子入出账为 0、余额沿用前一天（起点是窗口开始时的余额）。
 */
export function fillDays(daily: PointLedgerDay[] | null | undefined, days: number, startBalance: number): FilledDay[] {
  const byDate = new Map((daily ?? []).map((item) => [item.date, item]));
  const today = new Date();
  const result: FilledDay[] = [];
  let balance = startBalance;
  for (let offset = days - 1; offset >= 0; offset -= 1) {
    const at = new Date(today.getFullYear(), today.getMonth(), today.getDate() - offset);
    const key = localDateKey(at);
    const hit = byDate.get(key);
    if (hit) balance = hit.balance;
    result.push({
      date: key,
      label: `${at.getMonth() + 1}/${at.getDate()}`,
      in: hit?.in ?? 0,
      out: hit?.out ?? 0,
      count: hit?.count ?? 0,
      balance
    });
  }
  return result;
}

export const RANGE_OPTIONS = [
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天" },
  { value: "90", label: "90 天" },
  { value: "180", label: "180 天" }
] as const;
