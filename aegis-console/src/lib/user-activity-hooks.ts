"use client";

/**
 * 用户详情「活动」页签的审计查询（服务端筛选）。
 *
 * 单用户审计接口支持 status / eventType、keyword、from / to（RFC 3339，左闭右开）与分页：
 *   登录   keyword 匹配 IP、设备标识、UA、登录方式、渠道
 *   会话   keyword 匹配令牌 ID 与 metadata 文本
 * 共享的 lib/api/app-users 里的请求函数不带这些参数，这里单独发请求，不改共享文件。
 */

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import { apiRequest, buildQuery } from "@/lib/api/client";
import type { UserLoginAuditList, UserSessionAuditList } from "@/lib/api/types";

export type UserLoginAuditFilter = {
  status?: string;
  keyword?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type UserSessionAuditFilter = {
  eventType?: string;
  keyword?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

function base(appKey: string, userId: number) {
  return `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}`;
}

export function listUserLoginAudits(token: string, appKey: string, userId: number, filter: UserLoginAuditFilter) {
  const query = buildQuery({
    status: filter.status || undefined,
    keyword: filter.keyword || undefined,
    from: filter.from || undefined,
    to: filter.to || undefined,
    page: filter.page,
    limit: filter.limit
  });
  return apiRequest<UserLoginAuditList>(`${base(appKey, userId)}/audits/login${query}`, { token });
}

export function listUserSessionAudits(token: string, appKey: string, userId: number, filter: UserSessionAuditFilter) {
  const query = buildQuery({
    eventType: filter.eventType || undefined,
    keyword: filter.keyword || undefined,
    from: filter.from || undefined,
    to: filter.to || undefined,
    page: filter.page,
    limit: filter.limit
  });
  return apiRequest<UserSessionAuditList>(`${base(appKey, userId)}/audits/sessions${query}`, { token });
}

export function useUserLoginAuditListQuery(appKey: string, userId: number, filter: UserLoginAuditFilter) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-login-audits-filtered", token, appKey, userId, filter],
    queryFn: () => listUserLoginAudits(token as string, appKey, userId, filter),
    enabled: Boolean(token && appKey && userId),
    // 换页、改筛选时保留上一份数据，表格淡一点而不是闪成骨架
    placeholderData: keepPreviousData
  });
}

export function useUserSessionAuditListQuery(appKey: string, userId: number, filter: UserSessionAuditFilter) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-session-audits-filtered", token, appKey, userId, filter],
    queryFn: () => listUserSessionAudits(token as string, appKey, userId, filter),
    enabled: Boolean(token && appKey && userId),
    placeholderData: keepPreviousData
  });
}
