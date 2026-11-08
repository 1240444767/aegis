import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 崩溃报告域 API（docs/crash-reports.md）。
 *
 * 客户端在崩溃屏上经用户逐份同意后免登录上报；这里是管理端的查看、标记已处理与删除。
 * 列表不带报告正文，正文只在详情里取。
 */

export type CrashReportKind = "java" | "native" | "anr" | "exit";

export type CrashReportDevice = {
  platform?: string;
  /** 按设备字典翻译的展示名，未命中时为客户端上报的型号 */
  name?: string;
  manufacturer?: string;
  brand?: string;
  codename?: string;
  os?: string;
  osVersion?: string;
  appVersion?: string;
};

export type CrashReport = {
  id: number;
  /** 对外编号，上报接口返回给客户端的那个 */
  publicId: string;
  appid: number;
  userId?: number;
  /** 上报账号的对外用户 ID */
  userPublicId?: number;
  account?: string;
  /** 客户端自己的记录编号 */
  reportId: string;
  kind: CrashReportKind;
  title: string;
  detail?: string;
  /** 只在详情里有 */
  report?: string;
  reportBytes: number;
  appVersion?: string;
  versionCode?: number;
  osVersion?: string;
  model?: string;
  abi?: string;
  processName?: string;
  source?: string;
  deviceId?: string;
  device: CrashReportDevice;
  userAgent?: string;
  clientIp?: string;
  location: { countryCode?: string; country?: string; region?: string; city?: string };
  resolved: boolean;
  resolvedAt?: string;
  resolvedBy?: string;
  occurredAt: string;
  createdAt: string;
};

export type CrashReportPage = {
  items: CrashReport[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
};

export type CrashReportParams = {
  kind?: CrashReportKind;
  appVersion?: string;
  keyword?: string;
  resolved?: boolean;
  userId?: number;
  deviceId?: string;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type CrashReportFacets = {
  total: number;
  unresolved: number;
  last24h: number;
  kinds: Array<{ kind: CrashReportKind; count: number }>;
  versions: Array<{ appVersion: string; count: number }>;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/crash-reports`;

export function listCrashReports(token: string, appKey: string, params?: CrashReportParams) {
  return apiRequest<CrashReportPage>(`${base(appKey)}${buildQuery(params ?? {})}`, { token });
}

export function getCrashReportFacets(token: string, appKey: string) {
  return apiRequest<CrashReportFacets>(`${base(appKey)}/facets`, { token });
}

export function getCrashReport(token: string, appKey: string, id: number) {
  return apiRequest<CrashReport>(`${base(appKey)}/${id}`, { token });
}

export function resolveCrashReports(token: string, appKey: string, ids: number[], resolved: boolean) {
  return apiRequest<{ updated: number }>(`${base(appKey)}/resolve`, {
    method: "POST",
    token,
    body: JSON.stringify({ ids, resolved })
  });
}

export function deleteCrashReport(token: string, appKey: string, id: number) {
  return apiRequest<{ deleted: number }>(`${base(appKey)}/${id}`, { method: "DELETE", token });
}

export function deleteCrashReports(token: string, appKey: string, ids: number[]) {
  return apiRequest<{ deleted: number }>(`${base(appKey)}/delete`, {
    method: "POST",
    token,
    body: JSON.stringify({ ids })
  });
}
