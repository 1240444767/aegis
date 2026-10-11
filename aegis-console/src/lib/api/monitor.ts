import { apiRequest, buildQuery } from "./client";
import type {
  AppOnlineStats,
  OnlineStats,
  OnlineUserItem,
  PagedResult
} from "./types";

export function getSystemOnlineStats(token: string) {
  return apiRequest<OnlineStats>("/api/admin/system/online/stats", { token });
}

export function getAppOnlineStats(token: string, appId: number | string) {
  return apiRequest<AppOnlineStats>(`/api/admin/system/online/apps/${appId}`, { token });
}

export function getAppOnlineUsers(
  token: string,
  appId: number | string,
  params?: { page?: number; limit?: number }
) {
  const query = buildQuery({
    page: params?.page,
    limit: params?.limit
  });
  return apiRequest<PagedResult<OnlineUserItem>>(`/api/admin/system/online/apps/${appId}/users${query}`, { token });
}