import { apiRequest, buildQuery } from "./client";
import type { EmailDelivery, EmailDeliveryStats } from "./types";

/**
 * 邮件策略、场景目录、模板预览、概览、投递详情与抑制名单。
 *
 * 与通道配置同一条分工：平台级走 `/api/admin/system/email/*`（RESTful），
 * 应用级走 `/api/admin/app/email-config/*`（动词式，appid 在请求体里）。
 */

// ── 类型 ──

export type EmailBrand = {
  displayName?: string;
  logoUrl?: string;
  accentColor?: string;
  supportEmail?: string;
  footerText?: string;
};

export type EmailCodePolicy = {
  length: number;
  ttlMinutes: number;
  resendSeconds: number;
  maxAttempts: number;
  addressHourly: number;
  addressDaily: number;
  ipHourly: number;
};

/** 主题设计令牌，留空的项用内置主题 */
export type EmailTheme = {
  background?: string;
  card?: string;
  radius?: number;
  font?: string;
  logoAlign?: "left" | "center" | string;
  logoHeight?: number;
  header?: "plain" | "bar" | "banner" | string;
};

/** 自定义版式：enabled 为 false 时源码保留但不生效 */
export type EmailLayoutTemplate = { enabled: boolean; source?: string };

/** 场景的完整自定义模板 */
export type EmailSceneTemplate = {
  enabled: boolean;
  /** mjml（片段套进版式；以 <mjml 开头的完整文档不套）或 html（完整 HTML 文档） */
  format?: "mjml" | "html" | string;
  body?: string;
  /** 纯文本模板，留空时从 HTML 抓取 */
  text?: string;
};

export type EmailSceneSetting = {
  /** 只对可关闭的场景有意义；缺省按场景默认 */
  enabled?: boolean;
  subject?: string;
  title?: string;
  lead?: string;
  footer?: string;
  template?: EmailSceneTemplate | null;
};

export type EmailPolicy = {
  brand: EmailBrand;
  theme: EmailTheme;
  layout: EmailLayoutTemplate;
  code: EmailCodePolicy;
  scenes: Record<string, EmailSceneSetting>;
  suppression: { hardBounce: boolean; complaint: boolean };
  resetLinks: string[];
};

export type EmailPolicyView = {
  appid: number;
  policy: EmailPolicy;
  configured: boolean;
  updatedBy?: string;
  updatedAt?: string;
  /** 品牌名留空时邮件里实际用的名字 */
  scopeName: string;
  defaults: EmailPolicy;
  limits: Record<string, [number, number]>;
};

export type EmailSceneVariable = { key: string; label: string; example: string };

export type EmailSceneCopy = { subject: string; title: string; lead: string; footer: string };

export type EmailScene = {
  key: string;
  name: string;
  category: string;
  categoryName: string;
  description: string;
  toggle: boolean;
  defaultOn: boolean;
  variables: EmailSceneVariable[];
  defaults: EmailSceneCopy;
  purposes: string[] | null;
};

export type EmailTemplateReferenceItem = { name: string; syntax: string; description: string };

export type EmailTemplateReference = {
  globals: EmailTemplateReferenceItem[];
  theme: EmailTemplateReferenceItem[];
  components: EmailTemplateReferenceItem[];
  filters: EmailTemplateReferenceItem[];
};

export type EmailThemeDefaults = {
  accent: string;
  background: string;
  card: string;
  radius: number;
  font: string;
  logoAlign: string;
  logoHeight: number;
  header: string;
};

export type EmailSceneCatalog = {
  scenes: EmailScene[];
  categories: Array<{ key: string; name: string }>;
  /** 内置模板源码：版式、纯文本、每个场景的正文（键为场景键，另有 blocks） */
  templates: { layout: string; text: string; bodies: Record<string, string> };
  reference: EmailTemplateReference;
  theme: { defaults: EmailThemeDefaults; fonts: string[]; headers: string[] };
};

/** 模板问题。part：layout 版式 / body 正文 / text 纯文本 / copy 文案 */
export type EmailTemplateProblem = { part: string; line?: number; message: string };

export type EmailPreview = {
  scene: string;
  subject: string;
  html: string;
  text: string;
  problems: EmailTemplateProblem[];
};

export type EmailCount = { key: string; count: number };

export type EmailOverview = EmailDeliveryStats & {
  days: number;
  windowTotal: number;
  trend: Array<{ date: string; total: number; delivered: number; failed: number; bounced: number }>;
  byPurpose: EmailCount[];
  byStatus: EmailCount[];
  suppressions: number;
  failedLast24h: number;
};

export type EmailDeliveryEvent = {
  id: number;
  deliveryId: number;
  event: string;
  status?: string;
  detail?: string;
  payload?: Record<string, unknown>;
  occurredAt: string;
};

export type EmailSuppression = {
  id: number;
  appid: number;
  address: string;
  reason: "bounce" | "complaint" | "manual" | string;
  source: "webhook" | "admin" | string;
  note?: string;
  deliveryId?: number;
  createdBy?: string;
  expiresAt?: string | null;
  createdAt: string;
  updatedAt: string;
};

export type EmailDeliveryDetail = {
  delivery: EmailDelivery;
  events: EmailDeliveryEvent[];
  suppression?: EmailSuppression | null;
};

export type EmailSuppressionPage = {
  items: EmailSuppression[];
  total: number;
  page: number;
  pageSize: number;
};

export type EmailSuppressionFilters = { reason?: string; keyword?: string; page?: number; pageSize?: number };

export type EmailSuppressionInput = { address: string; reason?: string; note?: string; expires_at?: string | null };

// ── 场景目录（两个作用域共用，静态） ──

export function getEmailSceneCatalog(token: string) {
  return apiRequest<EmailSceneCatalog>("/api/admin/system/email/scenes", { token });
}

// ── 平台级 ──

export function getPlatformEmailPolicy(token: string) {
  return apiRequest<EmailPolicyView>("/api/admin/system/email/policy", { token });
}

export function savePlatformEmailPolicy(token: string, policy: EmailPolicy) {
  return apiRequest<EmailPolicyView>("/api/admin/system/email/policy", {
    method: "PUT",
    token,
    body: JSON.stringify({ policy })
  });
}

export function previewPlatformEmail(token: string, scene: string, policy?: EmailPolicy) {
  return apiRequest<EmailPreview>("/api/admin/system/email/preview", {
    method: "POST",
    token,
    body: JSON.stringify({ scene, policy })
  });
}

export function getPlatformEmailOverview(token: string, days: number, tz: string) {
  return apiRequest<EmailOverview>(`/api/admin/system/email/overview${buildQuery({ days, tz })}`, { token });
}

export function getPlatformEmailDeliveryDetail(token: string, deliveryId: number) {
  return apiRequest<EmailDeliveryDetail>(`/api/admin/system/email/deliveries/${deliveryId}`, { token });
}

export function getPlatformEmailSuppressions(token: string, filters: EmailSuppressionFilters) {
  return apiRequest<EmailSuppressionPage>(`/api/admin/system/email/suppressions${buildQuery(filters)}`, { token });
}

export function createPlatformEmailSuppression(token: string, input: EmailSuppressionInput) {
  return apiRequest<EmailSuppression>("/api/admin/system/email/suppressions", {
    method: "POST",
    token,
    body: JSON.stringify(input)
  });
}

export function deletePlatformEmailSuppression(token: string, id: number) {
  return apiRequest<null>(`/api/admin/system/email/suppressions/${id}`, { method: "DELETE", token });
}

// ── 应用级 ──

function appPost<T>(token: string, path: string, body: Record<string, unknown>) {
  return apiRequest<T>(`/api/admin/app/email-config/${path}`, {
    method: "POST",
    token,
    body: JSON.stringify(body)
  });
}

export function getAppEmailPolicy(token: string, appid: number) {
  return appPost<EmailPolicyView>(token, "policy", { appid });
}

export function saveAppEmailPolicy(token: string, appid: number, policy: EmailPolicy) {
  return appPost<EmailPolicyView>(token, "policy/save", { appid, policy });
}

export function previewAppEmail(token: string, appid: number, scene: string, policy?: EmailPolicy) {
  return appPost<EmailPreview>(token, "preview", { appid, scene, policy });
}

export function getAppEmailOverview(token: string, appid: number, days: number, tz: string) {
  return appPost<EmailOverview>(token, "overview", { appid, days, tz });
}

export function getAppEmailDeliveryDetail(token: string, appid: number, deliveryId: number) {
  return appPost<EmailDeliveryDetail>(token, "deliveries/detail", { appid, delivery_id: deliveryId });
}

export function getAppEmailSuppressions(token: string, appid: number, filters: EmailSuppressionFilters) {
  return appPost<EmailSuppressionPage>(token, "suppressions", { appid, ...filters });
}

export function createAppEmailSuppression(token: string, appid: number, input: EmailSuppressionInput) {
  return appPost<EmailSuppression>(token, "suppressions/create", { appid, ...input });
}

export function deleteAppEmailSuppression(token: string, appid: number, id: number) {
  return appPost<null>(token, "suppressions/delete", { appid, id });
}
