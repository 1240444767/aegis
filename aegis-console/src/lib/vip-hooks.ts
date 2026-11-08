"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  claimAdminVipTrial,
  deleteAdminVipFeature,
  deleteAdminVipPlan,
  deleteAdminVipTier,
  getAdminVipConversionPolicy,
  listAdminVipConversions,
  listAdminVipFeatures,
  listAdminVipPlans,
  listAdminVipTiers,
  listAdminVipTransactions,
  listAdminVipTrialClaims,
  previewAdminVipGrant,
  resetAdminVipTrial,
  saveAdminVipConversionPolicy,
  saveAdminVipFeature,
  saveAdminVipPlan,
  saveAdminVipTier,
  type VipConversionPolicyPayload,
  type VipFeaturePayload,
  type VipGrantPayload,
  type VipPlanPayload,
  type VipTierPayload
} from "@/lib/api/vip";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 会员域 hooks。
 *
 * **失效关系集中在这里**，不散在组件里：一次套餐保存会同时影响套餐列表、
 * 某个用户的权益判定（他的套餐名变了）与开通记录三张表；删一个功能标识
 * 还会牵动套餐（那几个套餐从此少一项权益）。散着写必然漏掉其中几张，
 * 表现为「保存成功了但列表没变」，而那看起来像是保存失败。
 */

const KEY = {
  plans: "admin-vip-plans",
  tiers: "admin-vip-tiers",
  features: "admin-vip-features",
  entitlement: "admin-vip-entitlement",
  claims: "admin-vip-trial-claims",
  transactions: "admin-vip-transactions",
  conversions: "admin-vip-conversions",
  conversionPolicy: "admin-vip-conversion-policy",
  grantPreview: "admin-vip-grant-preview"
} as const;

/**
 * 会员写操作后要成组失效的缓存键。
 *
 * 除了会员域自己的几张表，还有用户详情那一套（记录、权益判定、详情与流水、列表）、
 * 功能解锁 —— 一次发放可能折成积分、开通永久会折掉功能解锁与功能时长，
 * 从哪个页面发的都要让另一个页面看到。
 */
const MEMBERSHIP_SCOPE_KEYS = [
  ...Object.values(KEY),
  "admin-user-vip-txns",
  "admin-user-vip-entitlement",
  "admin-app-user",
  "admin-app-users",
  "admin-points-store-unlocks"
];

/** 会员发放 / 收回 / 试用 / 套餐与功能变更之后调用（用户详情的会员面板也走它）。 */
export function useInvalidateMembershipScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(MEMBERSHIP_SCOPE_KEYS.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

const useInvalidateVipScope = useInvalidateMembershipScope;

// ── 套餐 ──

export function useAdminVipPlansQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.plans, token, appKey],
    queryFn: () => listAdminVipPlans(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAdminVipPlanMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (payload: VipPlanPayload) => saveAdminVipPlan(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAdminVipPlanMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (planId: number) => deleteAdminVipPlan(token as string, appKey as string, planId),
    onSuccess: invalidate
  });
}

// ── 会员档位 ──

export function useAdminVipTiersQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.tiers, token, appKey],
    queryFn: () => listAdminVipTiers(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAdminVipTierMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (payload: VipTierPayload) => saveAdminVipTier(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAdminVipTierMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: ({ tierId, moveTo }: { tierId: number; moveTo?: number }) =>
      deleteAdminVipTier(token as string, appKey as string, tierId, moveTo),
    onSuccess: invalidate
  });
}

// ── 功能标识目录 ──

export function useAdminVipFeaturesQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.features, token, appKey],
    queryFn: () => listAdminVipFeatures(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAdminVipFeatureMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (payload: VipFeaturePayload) => saveAdminVipFeature(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAdminVipFeatureMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (tag: string) => deleteAdminVipFeature(token as string, appKey as string, tag),
    onSuccess: invalidate
  });
}

// ── 发放预览 ──
//
// 发放、收回与试用的写操作在 app-user-hooks（按用户），失效用 useInvalidateMembershipScope。

/** 发放前预览（不落账）。payload 为空时不请求。 */
export function useAdminVipGrantPreviewQuery(appKey?: string | null, payload?: VipGrantPayload | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.grantPreview, token, appKey, payload],
    queryFn: () => previewAdminVipGrant(token as string, appKey as string, payload as VipGrantPayload),
    enabled: Boolean(token && appKey && payload && payload.userId > 0),
    staleTime: 0,
    retry: false
  });
}

// ── 折算 ──

export function useAdminVipConversionPolicyQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.conversionPolicy, token, appKey],
    queryFn: () => getAdminVipConversionPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAdminVipConversionPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateMembershipScope();
  return useMutation({
    mutationFn: (payload: VipConversionPolicyPayload) =>
      saveAdminVipConversionPolicy(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useAdminVipConversionsQuery(
  appKey?: string | null,
  params?: { userId?: number; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.conversions, token, appKey, params?.userId ?? 0, params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listAdminVipConversions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminVipTransactionsQuery(
  appKey?: string | null,
  params?: { userId?: number; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.transactions, token, appKey, params?.userId ?? 0, params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listAdminVipTransactions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

// ── 试用 ──

export function useAdminVipTrialClaimsQuery(
  appKey?: string | null,
  params?: { page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.claims, token, appKey, params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listAdminVipTrialClaims(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useClaimAdminVipTrialMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (userId: number) => claimAdminVipTrial(token as string, appKey as string, userId),
    onSuccess: invalidate
  });
}

export function useResetAdminVipTrialMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (userId: number) => resetAdminVipTrial(token as string, appKey as string, userId),
    onSuccess: invalidate
  });
}
