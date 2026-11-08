"use client";

import Link from "next/link";
import { Loader2, RotateCcw, Save } from "lucide-react";
import { rewardDraftsFrom, rewardsFromDrafts } from "@/components/apps/rewards/reward-list-editor";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type {
  LeaderboardBoardRewards,
  LeaderboardBoardSpec,
  LeaderboardCategoryRule,
  LeaderboardIdentity,
  LeaderboardPeriod,
  LeaderboardRewardStatus,
  LeaderboardSettings,
  LeaderboardSettingsPayload
} from "@/lib/api/leaderboard";
import type { PointsStoreRewardSpec } from "@/lib/api/points-store";
import { formatDate, formatDateTimeMinute } from "@/lib/datetime";

/**
 * 排行榜的展示口径：枚举文案、状态徽标、用户单元格、期号格式。集中在一处，
 * 概览、设置、奖励、实时榜、移出名单与结算记录说的是同一套话。
 */

export const PERIOD_LABEL: Record<LeaderboardPeriod, string> = {
  total: "累计",
  day: "日",
  week: "周",
  month: "月"
};

/** 期号格式，与后端 leaderboard/period.go 一致 */
export const PERIOD_KEY_HINT: Record<Exclude<LeaderboardPeriod, "total">, { example: string; text: string }> = {
  day: { example: "2026-10-19", text: "日期，例如 2026-10-19" },
  week: { example: "2026-W43", text: "ISO 年与周，例如 2026-W43" },
  month: { example: "2026-10", text: "年与月，例如 2026-10" }
};

export const IDENTITY_META: Record<LeaderboardIdentity, { title: string; description: string }> = {
  nickname: { title: "昵称优先", description: "有昵称显示昵称，没有昵称显示脱敏账号" },
  masked: { title: "脱敏账号", description: "一律显示脱敏账号，不显示昵称" }
};

export const CATEGORY_RULE_META: Record<
  LeaderboardCategoryRule,
  { label: string; variant: "success" | "secondary" | "info" }
> = {
  counted: { label: "计入", variant: "success" },
  excluded: { label: "不计入", variant: "secondary" },
  admin: { label: "按设置", variant: "info" }
};

const REWARD_STATUS_META: Record<
  LeaderboardRewardStatus,
  { label: string; variant: "success" | "warning" | "danger" | "secondary" | "outline" }
> = {
  pending: { label: "待发放", variant: "warning" },
  granted: { label: "已发放", variant: "success" },
  failed: { label: "发放失败", variant: "danger" },
  skipped: { label: "已跳过", variant: "secondary" },
  none: { label: "无奖励", variant: "outline" }
};

export const DEFAULT_REWARD_STATUSES: LeaderboardRewardStatus[] = ["pending", "granted", "failed", "skipped", "none"];

export function rewardStatusLabel(status: LeaderboardRewardStatus) {
  return REWARD_STATUS_META[status]?.label ?? status;
}

export function RewardStatusBadge({ status }: { status: LeaderboardRewardStatus }) {
  const meta = REWARD_STATUS_META[status] ?? REWARD_STATUS_META.none;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

/** 结算人：system 是后台循环，其余是手动结算的管理员 */
export function settledByLabel(value?: string) {
  if (!value) return "—";
  return value === "system" ? "自动结算" : value;
}

/** 一句话说清这一期怎么结算的：「自动结算于 …」「admin 手动结算于 …」 */
export function settlementLabel(settledBy?: string, settledAt?: string | null) {
  const who = !settledBy || settledBy === "system" ? "自动结算" : `${settledBy} 手动结算`;
  return `${who}于 ${formatDateTimeMinute(settledAt)}`;
}

/** 只有周期榜（日 / 周 / 月）会结算、能配奖励 */
export function isPeriodic(spec?: Pick<LeaderboardBoardSpec, "period"> | null) {
  return Boolean(spec && spec.period !== "total");
}

/** 档位的说法：「第 1 名」「第 2–3 名」，与后端 TierLabel 一致 */
export function tierLabel(rankFrom: number, rankTo: number) {
  return rankFrom === rankTo ? `第 ${rankFrom} 名` : `第 ${rankFrom}–${rankTo} 名`;
}

/** 周期起止：结束时刻是下一期的开始，展示时退一天到本期最后一天 */
export function periodRangeLabel(start?: string | null, end?: string | null) {
  if (!start || !end) return "—";
  const last = new Date(new Date(end).getTime() - 1);
  const from = formatDate(start);
  const to = formatDate(last);
  return from === to ? from : `${from} 至 ${to}`;
}

const pad = (value: number) => String(value).padStart(2, "0");

/** ISO 周：周一为一周的第一天，含 1 月 4 日的那一周是第 1 周 */
function isoWeekKey(date: Date) {
  const day = new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()));
  const weekday = day.getUTCDay() || 7;
  day.setUTCDate(day.getUTCDate() + 4 - weekday);
  const yearStart = new Date(Date.UTC(day.getUTCFullYear(), 0, 1));
  const week = Math.ceil(((day.getTime() - yearStart.getTime()) / 86_400_000 + 1) / 7);
  return `${day.getUTCFullYear()}-W${pad(week)}`;
}

/**
 * 上一期的期号，按浏览器时区算，只作手动结算的默认值；周期边界以后端应用默认时区为准，
 * 两边时区不同时在跨期的那几个小时里可能差一期，提交前核对一下即可。
 */
export function previousPeriodKey(period: LeaderboardPeriod, now: Date = new Date()) {
  switch (period) {
    case "day": {
      const day = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
      return `${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}`;
    }
    case "week":
      return isoWeekKey(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 7));
    case "month": {
      const month = new Date(now.getFullYear(), now.getMonth() - 1, 1);
      return `${month.getFullYear()}-${pad(month.getMonth() + 1)}`;
    }
    default:
      return "total";
  }
}

/** 解析多个用户 ID：空白、逗号、顿号、分号分隔，去重，只留正整数 */
export function parseUserIds(raw: string) {
  const ids: number[] = [];
  const invalid: string[] = [];
  for (const token of raw.split(/[\s,，、;；]+/)) {
    if (!token) continue;
    const id = Number(token);
    if (Number.isInteger(id) && id > 0) {
      if (!ids.includes(id)) ids.push(id);
    } else {
      invalid.push(token);
    }
  }
  return { ids, invalid };
}

/** 取值：数字加单位，等级榜另带等级名 */
export function LeaderboardValue({ value, unit, levelName }: { value: number; unit?: string; levelName?: string }) {
  return (
    <span className="whitespace-nowrap">
      <span className="font-medium tabular-nums">{value.toLocaleString("zh-CN")}</span>
      {unit ? <span className="ml-1 text-[11px] text-muted-foreground">{unit}</span> : null}
      {levelName ? <span className="ml-1.5 text-[11px] text-muted-foreground">{levelName}</span> : null}
    </span>
  );
}

/** 用户单元格：头像、名字与真实账号，点进用户详情 */
export function LeaderboardUser({
  appKey,
  userId,
  name,
  account,
  avatar
}: {
  appKey: string;
  userId: number;
  name?: string;
  account?: string;
  avatar?: string;
}) {
  const title = name || account || `#${userId}`;
  return (
    <Link href={`/app-users/${encodeURIComponent(appKey)}/${userId}`} className="group flex min-w-0 items-center gap-2">
      <Avatar className="size-7 rounded-lg border">
        <AvatarImage src={avatar || undefined} alt={title} />
        <AvatarFallback className="rounded-lg text-[10px]">{title.slice(0, 2)}</AvatarFallback>
      </Avatar>
      <span className="min-w-0">
        <span className="block max-w-40 truncate text-xs font-medium group-hover:underline">{title}</span>
        <span className="block max-w-40 truncate font-mono text-[11px] text-muted-foreground">
          {account && account !== title ? `${account} ` : ""}#{userId}
        </span>
      </span>
    </Link>
  );
}

/**
 * 已保存的奖励配置按草稿往返一次：只留取值形态需要的字段（后端出网的权益带着 money "0" 之类的零值），
 * 设置页回传奖励、奖励页比对改动都用这一份，两边的形状一致。
 */
export function normalizeBoardRewards(
  rewards: Record<string, LeaderboardBoardRewards> | undefined,
  catalog: PointsStoreRewardSpec[]
): Record<string, LeaderboardBoardRewards> {
  const out: Record<string, LeaderboardBoardRewards> = {};
  for (const [key, board] of Object.entries(rewards ?? {})) {
    out[key] = {
      enabled: Boolean(board.enabled),
      tiers: (board.tiers ?? []).map((tier) => ({
        rankFrom: tier.rankFrom,
        rankTo: tier.rankTo,
        rewards: rewardsFromDrafts(rewardDraftsFrom(tier.rewards), catalog)
      }))
    };
  }
  return out;
}

/**
 * 设置与奖励共用一个 PUT：一边保存时，另一边按服务端已保存的值原样带上。
 * 这里给出「全部按已保存」的载荷，两个面板各自覆盖自己那一半。
 */
export function savedPayload(settings: LeaderboardSettings, catalog: PointsStoreRewardSpec[]): LeaderboardSettingsPayload {
  return {
    enabled: settings.enabled,
    boards: settings.boards.map((board) => ({ key: board.key, enabled: board.enabled })),
    topLimit: settings.topLimit,
    minValue: settings.minValue,
    identity: settings.identity,
    allowAnonymous: settings.allowAnonymous,
    countAdminAdjust: settings.countAdminAdjust,
    rewards: normalizeBoardRewards(settings.rewards, catalog)
  };
}

export const toInt = (value: string) => {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : 0;
};

/** 底部吸附的保存条：上次保存人与时间、放弃修改、保存 */
export function LeaderboardSaveBar({
  settings,
  dirty,
  pending,
  onReset,
  onSave
}: {
  settings?: LeaderboardSettings;
  dirty: boolean;
  pending: boolean;
  onReset: () => void;
  onSave: () => void;
}) {
  return (
    <div className="sticky bottom-3 z-10 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border bg-card px-4 py-3">
      <span className="text-xs text-muted-foreground">
        {settings?.updatedAt
          ? `${settings.updatedBy ? `${settings.updatedBy} ` : ""}保存于 ${formatDateTimeMinute(settings.updatedAt)}`
          : "尚未保存"}
      </span>
      <div className="flex items-center gap-2">
        {dirty ? (
          <Button variant="outline" size="sm" className="h-8 gap-1.5" disabled={pending} onClick={onReset}>
            <RotateCcw className="size-3.5" />
            放弃修改
          </Button>
        ) : null}
        <Button size="sm" className="h-8 gap-1.5" disabled={!dirty || pending} onClick={onSave}>
          {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
          保存
        </Button>
      </div>
    </div>
  );
}
