"use client";

/**
 * 用户详情「互动记录」页签的查询。
 *
 * 每个业务域的列表都按 userId 过滤；查询键的第一段沿用各域 hooks 的键名
 * （admin-lottery-draws、admin-invite-relations……），这样各域已有的写操作 hooks
 * （发放、取消、补发）在成功后按前缀整组失效时，这里的列表与计数也会一起刷新。
 *
 * 指标卡的数字都来自接口的 `total`：同一个接口加一个筛选、limit=1 只取总数，
 * 不在前端拿当前页去推算全量。
 */

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import { apiRequest, buildQuery } from "@/lib/api/client";
import { listCardKeyRedemptions, type CardKeyRedemptionParams, type CardKeySource } from "@/lib/api/card-key";
import { listInviteRelations, type InviteRelationParams, type InviterStatus } from "@/lib/api/invite";
import { listLotteryDraws, type LotteryDrawParams } from "@/lib/api/lottery";
import { listPointsStoreRedemptions, type PointsStoreRedemptionParams } from "@/lib/api/points-store";
import { listRewardedAdViews, type RewardedAdViewParams } from "@/lib/api/rewarded-ad";
import type { AppSignInRecordItem, PagedResult } from "@/lib/api/types";

const KEY = {
  signIn: "admin-app-signin-records",
  invite: "admin-invite-relations",
  lottery: "admin-lottery-draws",
  cardKey: "admin-card-key-redemptions",
  pointsStore: "admin-points-store-redemptions",
  rewardedAd: "admin-rewarded-ad-views"
} as const;

// ── 签到 ──

export type UserSignInParams = {
  source?: string;
  bonusType?: string;
  /** 策略时区的日历日 YYYY-MM-DD，闭区间 */
  dateFrom?: string;
  dateTo?: string;
  page?: number;
  limit?: number;
};

/** 签到明细（带 userId）。apps.ts 里的封装不带 userId，这里单独请求。 */
export function listUserSignInRecords(token: string, appKey: string, userId: number, params?: UserSignInParams) {
  const query = buildQuery({
    userId,
    source: params?.source,
    bonusType: params?.bonusType,
    dateFrom: params?.dateFrom,
    dateTo: params?.dateTo,
    page: params?.page,
    limit: params?.limit
  });
  return apiRequest<PagedResult<AppSignInRecordItem>>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/signin/records${query}`,
    { token }
  );
}

export function useUserSignInRecordsQuery(appKey: string, userId: number, params: UserSignInParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.signIn, "user", token, appKey, userId, params],
    queryFn: () => listUserSignInRecords(token as string, appKey, userId, params),
    enabled: Boolean(token && appKey && userId),
    placeholderData: keepPreviousData
  });
}

export type UserSignInSummary = {
  /** 全部签到记录条数（一天至多一条，即累计签到天数） */
  total: number;
  latest?: AppSignInRecordItem;
  /** 本月（策略时区）签到次数 */
  month: number;
  /** 近 90 天的签到记录（一天一条，最多 90 条） */
  recent: AppSignInRecordItem[];
  /** 后端返回了别的用户的记录：说明接口没有按 userId 过滤，数字不可信 */
  unfiltered: boolean;
};

export function useUserSignInSummaryQuery(
  appKey: string,
  userId: number,
  window: { today: string; monthStart: string; recentStart: string } | null
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.signIn, "user-summary", token, appKey, userId, window],
    enabled: Boolean(token && appKey && userId && window),
    queryFn: async (): Promise<UserSignInSummary> => {
      const w = window as NonNullable<typeof window>;
      const [all, month, recent] = await Promise.all([
        listUserSignInRecords(token as string, appKey, userId, { page: 1, limit: 1 }),
        listUserSignInRecords(token as string, appKey, userId, { dateFrom: w.monthStart, dateTo: w.today, page: 1, limit: 1 }),
        listUserSignInRecords(token as string, appKey, userId, { dateFrom: w.recentStart, dateTo: w.today, page: 1, limit: 100 })
      ]);
      const sample = [...(all.items ?? []), ...(recent.items ?? [])];
      return {
        total: all.total,
        latest: all.items?.[0],
        month: month.total,
        recent: recent.items ?? [],
        unfiltered: sample.some((item) => item.userId !== userId)
      };
    }
  });
}

// ── 邀请 ──

export function useUserInviteRelationsQuery(appKey: string, params: InviteRelationParams, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.invite, "user", token, appKey, params],
    queryFn: () => listInviteRelations(token as string, appKey, params),
    enabled: Boolean(token && appKey && enabled),
    placeholderData: keepPreviousData
  });
}

export function useUserInviteCountsQuery(appKey: string, userId: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.invite, "user-counts", token, appKey, userId],
    enabled: Boolean(token && appKey && userId),
    queryFn: async () => {
      const count = (status?: InviterStatus) =>
        listInviteRelations(token as string, appKey, { inviterId: userId, status, page: 1, limit: 1 }).then((page) => page.total);
      const [total, granted, pending, limited] = await Promise.all([count(), count("granted"), count("pending"), count("limited")]);
      return { total, granted, pending, limited };
    }
  });
}

// ── 抽奖 ──

export function useUserLotteryDrawsQuery(appKey: string, params: LotteryDrawParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.lottery, "user", token, appKey, params],
    queryFn: () => listLotteryDraws(token as string, appKey, params),
    enabled: Boolean(token && appKey && params.userId),
    placeholderData: keepPreviousData
  });
}

export function useUserLotteryCountsQuery(appKey: string, userId: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.lottery, "user-counts", token, appKey, userId],
    enabled: Boolean(token && appKey && userId),
    queryFn: async () => {
      const run = (extra: LotteryDrawParams) => listLotteryDraws(token as string, appKey, { userId, page: 1, limit: 1, ...extra });
      const [all, wins, pending] = await Promise.all([run({}), run({ winning: true }), run({ status: "pending" })]);
      return { total: all.total, wins: wins.total, pending: pending.total, latestAt: all.items?.[0]?.createdAt };
    }
  });
}

// ── 卡密兑换 ──

export function useUserCardKeyRedemptionsQuery(appKey: string, params: CardKeyRedemptionParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.cardKey, "user", token, appKey, params],
    queryFn: () => listCardKeyRedemptions(token as string, appKey, params),
    enabled: Boolean(token && appKey && params.userId),
    placeholderData: keepPreviousData
  });
}

export function useUserCardKeyCountsQuery(appKey: string, userId: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.cardKey, "user-counts", token, appKey, userId],
    enabled: Boolean(token && appKey && userId),
    queryFn: async () => {
      const count = (source?: CardKeySource) =>
        listCardKeyRedemptions(token as string, appKey, { userId, source, page: 1, limit: 1 });
      const [all, redeem, login, admin] = await Promise.all([count(), count("redeem"), count("login"), count("admin")]);
      return { total: all.total, redeem: redeem.total, login: login.total, admin: admin.total, latestAt: all.items?.[0]?.createdAt };
    }
  });
}

// ── 积分商城兑换 ──

export function useUserPointsStoreRedemptionsQuery(appKey: string, params: PointsStoreRedemptionParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.pointsStore, "user", token, appKey, params],
    queryFn: () => listPointsStoreRedemptions(token as string, appKey, params),
    enabled: Boolean(token && appKey && params.userId),
    placeholderData: keepPreviousData
  });
}

export function useUserPointsStoreCountsQuery(appKey: string, userId: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.pointsStore, "user-counts", token, appKey, userId],
    enabled: Boolean(token && appKey && userId),
    queryFn: async () => {
      const count = (status?: PointsStoreRedemptionParams["status"]) =>
        listPointsStoreRedemptions(token as string, appKey, { userId, status, page: 1, limit: 1 }).then((page) => page.total);
      const [total, completed, pending, cancelled] = await Promise.all([
        count(),
        count("completed"),
        count("pending"),
        count("cancelled")
      ]);
      return { total, completed, pending, cancelled };
    }
  });
}

// ── 激励广告 ──

export function useUserRewardedAdViewsQuery(appKey: string, params: RewardedAdViewParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.rewardedAd, "user", token, appKey, params],
    queryFn: () => listRewardedAdViews(token as string, appKey, params),
    enabled: Boolean(token && appKey && params.userId),
    placeholderData: keepPreviousData
  });
}

export function useUserRewardedAdCountsQuery(appKey: string, userId: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.rewardedAd, "user-counts", token, appKey, userId],
    enabled: Boolean(token && appKey && userId),
    queryFn: async () => {
      const count = (status?: RewardedAdViewParams["status"]) =>
        listRewardedAdViews(token as string, appKey, { userId, status, page: 1, limit: 1 });
      const [all, granted, pending, rejected] = await Promise.all([count(), count("granted"), count("pending"), count("rejected")]);
      return {
        total: all.total,
        granted: granted.total,
        pending: pending.total,
        rejected: rejected.total,
        latestAt: all.items?.[0]?.createdAt
      };
    }
  });
}
