"use client";

import type { ThemedColor } from "@/components/commerce/commerce-charts";
import type { EmailScene } from "@/lib/api/email-policy";

/**
 * 邮件区块各视图共用的标签与配色。
 *
 * 状态配色与列表徽标、图表扇区一一对应：同一个「已退信」在表格里是红色，在环形图里也必须是红色。
 */

export type BadgeTone = "success" | "warning" | "danger" | "info" | "outline" | "secondary";

export const DELIVERY_STATUS: Record<string, { label: string; tone: BadgeTone; color: ThemedColor }> = {
  pending: { label: "已入队", tone: "secondary", color: { light: "#a1a1aa", dark: "#71717a" } },
  sent: { label: "已发送", tone: "info", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  delivered: { label: "已送达", tone: "success", color: { light: "#10b981", dark: "#34d399" } },
  bounced: { label: "已退信", tone: "danger", color: { light: "#f43f5e", dark: "#fb7185" } },
  complained: { label: "被投诉", tone: "warning", color: { light: "#f59e0b", dark: "#fbbf24" } },
  rejected: { label: "被拒绝", tone: "danger", color: { light: "#e11d48", dark: "#f43f5e" } },
  failed: { label: "发送失败", tone: "danger", color: { light: "#dc2626", dark: "#ef4444" } }
};

export function deliveryStatus(status?: string | null) {
  return (status && DELIVERY_STATUS[status]) || { label: status || "未知", tone: "outline" as BadgeTone, color: undefined };
}

/** 场景目录之外的用途。目录内的用途按场景名显示（见 purposeLabeler）。 */
const EXTRA_PURPOSES: Record<string, string> = {
  test: "通道自检",
  notification: "通知",
  document: "附件邮件",
  receipt: "支付凭证",
  password_reset_link: "重置密码链接",
  password_reset_notice: "密码已重置",
  account_deletion_submitted: "注销申请已提交",
  account_deletion_scheduled: "注销冷静期",
  account_deletion_reminder: "注销提醒",
  account_deletion_cancelled: "注销已撤销",
  account_deletion_rejected: "注销未通过",
  account_deletion_completed: "账号已注销",
  unknown: "未标注"
};

/** 用途 → 中文。用场景目录里的 purposes 反查，后端新增一类信时这里自动认得。 */
export function purposeLabeler(scenes: EmailScene[] | undefined) {
  const map = new Map<string, string>();
  for (const scene of scenes ?? []) {
    for (const purpose of scene.purposes ?? []) {
      if (!map.has(purpose)) map.set(purpose, scene.name);
    }
  }
  return (purpose?: string | null) => {
    if (!purpose) return "未标注";
    return EXTRA_PURPOSES[purpose] ?? map.get(purpose) ?? purpose;
  };
}

export const SUPPRESSION_REASON: Record<string, { label: string; tone: BadgeTone }> = {
  bounce: { label: "硬退信", tone: "danger" },
  complaint: { label: "投诉", tone: "warning" },
  manual: { label: "手动", tone: "secondary" }
};

export const SUPPRESSION_SOURCE: Record<string, string> = {
  webhook: "投递回执",
  admin: "管理员"
};

/** 回执事件 → 中文 */
export const DELIVERY_EVENT: Record<string, string> = {
  send: "服务商已接收",
  delivery: "已送达收件服务器",
  bounce: "被退回",
  complaint: "被标记为垃圾邮件",
  reject: "被服务商拒绝",
  open: "被打开",
  click: "链接被点击",
  delay: "投递延迟",
  deferred: "暂缓投递",
  unknown: "其他事件"
};

export function formatNumber(value?: number | null) {
  if (value === null || value === undefined) return "—";
  return value.toLocaleString("zh-CN");
}

export function formatRate(part: number, whole: number) {
  if (!whole) return "—";
  const rate = (part / whole) * 100;
  return `${rate >= 10 || rate === 0 ? rate.toFixed(0) : rate.toFixed(1)}%`;
}
