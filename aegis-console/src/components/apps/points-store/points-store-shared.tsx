"use client";

import { Badge } from "@/components/ui/badge";
import type {
  PointsStoreAudience,
  PointsStoreItem,
  PointsStoreLimitPeriod,
  PointsStoreRedemptionStatus,
  PointsStoreReward,
  PointsStoreRewardSpec
} from "@/lib/api/points-store";
import type { VipFeature, VipPlan } from "@/lib/api/vip";
import { vipPlanTerm } from "@/components/apps/vip/vip-shared";

/**
 * 积分商城的展示口径：周期、对象、状态、权益摘要。集中在一处，
 * 商品列表、编辑抽屉、兑换记录与用户详情说的是同一套话。
 */

export const LIMIT_PERIOD_LABEL: Record<PointsStoreLimitPeriod, string> = {
  total: "累计",
  day: "每天",
  week: "每周",
  month: "每月"
};

export const AUDIENCE_LABEL: Record<PointsStoreAudience, string> = {
  all: "所有用户",
  vip: "仅会员",
  non_vip: "仅非会员"
};

const REDEMPTION_STATUS_META: Record<
  PointsStoreRedemptionStatus,
  { label: string; variant: "success" | "warning" | "secondary" }
> = {
  completed: { label: "已到账", variant: "success" },
  pending: { label: "待发放", variant: "warning" },
  cancelled: { label: "已取消", variant: "secondary" }
};

export function RedemptionStatusBadge({ status }: { status: PointsStoreRedemptionStatus }) {
  const meta = REDEMPTION_STATUS_META[status] ?? REDEMPTION_STATUS_META.completed;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export function redemptionStatusLabel(status: PointsStoreRedemptionStatus) {
  return REDEMPTION_STATUS_META[status]?.label ?? status;
}

/** 时长：0 天的功能解锁是永久，不写成「0 天」 */
export function termLabel(days: number, permanent = false) {
  if (permanent || days <= 0) return "永久";
  if (days % 365 === 0) return `${days / 365} 年`;
  if (days % 30 === 0) return `${days / 30} 个月`;
  return `${days} 天`;
}

/** 一项权益的一句话摘要 */
export function describeReward(
  reward: PointsStoreReward,
  catalog: PointsStoreRewardSpec[],
  plans: VipPlan[],
  features: VipFeature[]
) {
  const spec = catalog.find((item) => item.type === reward.type);
  const label = spec?.label ?? reward.type;
  switch (spec?.value) {
    case "ref": {
      const plan = plans.find((item) => item.id === reward.refId);
      return plan ? `${plan.name}（${vipPlanTerm(plan)}）` : `${label} #${reward.refId}`;
    }
    case "feature": {
      const feature = features.find((item) => item.tag === reward.tag);
      return `${feature?.name || reward.tag} ${termLabel(reward.amount ?? 0)}`;
    }
    case "money":
      return `${label} ¥${reward.money}`;
    case "text":
      return reward.text ?? label;
    case "amount":
      return `${label} ${reward.amount}${spec.unit ? ` ${spec.unit}` : ""}`;
    default:
      return label;
  }
}

export function RewardBadges({
  rewards,
  catalog,
  plans,
  features
}: {
  rewards: PointsStoreReward[];
  catalog: PointsStoreRewardSpec[];
  plans: VipPlan[];
  features: VipFeature[];
}) {
  return (
    <div className="flex flex-wrap gap-1">
      {rewards.map((reward, index) => (
        <Badge key={`${reward.type}-${index}`} variant="outline" size="sm" className="font-normal">
          {describeReward(reward, catalog, plans, features)}
        </Badge>
      ))}
    </div>
  );
}

/** 商品此刻的上架状态：下架 / 未开始 / 已结束 / 售罄，都不是则为空 */
export function itemShelfState(item: PointsStoreItem, now = Date.now()) {
  if (item.status !== "active") return { label: "已下架", variant: "secondary" as const };
  if (item.endsAt && new Date(item.endsAt).getTime() <= now) return { label: "已结束", variant: "secondary" as const };
  if (item.startsAt && new Date(item.startsAt).getTime() > now) return { label: "未开始", variant: "info" as const };
  if (item.stock !== undefined && item.stock !== null && item.stock <= 0) {
    return { label: "已兑完", variant: "warning" as const };
  }
  return null;
}

export function formatPoints(value?: number) {
  return (value ?? 0).toLocaleString("zh-CN");
}

export function formatStoreDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

/** datetime-local 输入框要的本地时间串（YYYY-MM-DDTHH:mm） */
export function toLocalInput(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
