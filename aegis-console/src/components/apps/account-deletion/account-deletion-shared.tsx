"use client";

import type { ThemedColor } from "@/components/commerce/commerce-charts";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type {
  DeletionConfig,
  DeletionGuardKey,
  DeletionGuardLevel,
  DeletionLoginBehavior,
  DeletionMode,
  DeletionRequest,
  DeletionSource,
  DeletionStatus,
  DeletionTombstoneKind,
  DeletionVerifyMethod
} from "@/lib/api/account-deletion";

/**
 * 账号注销的展示口径：枚举文案、状态徽标与配色、前置条件的中文名称与含义、可执行的操作、分页条。
 * 概览、申请、配置、重新注册限制与用户详情里的注销卡片说的是同一套话。
 */

type BadgeVariant = "success" | "warning" | "danger" | "info" | "secondary" | "outline";

export const STATUS_META: Record<DeletionStatus, { label: string; variant: BadgeVariant; color: ThemedColor }> = {
  reviewing: { label: "待审核", variant: "warning", color: { light: "#f59e0b", dark: "#fbbf24" } },
  scheduled: { label: "冷静期中", variant: "info", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  processing: { label: "执行中", variant: "info", color: { light: "#6366f1", dark: "#818cf8" } },
  completed: { label: "已注销", variant: "secondary", color: { light: "#71717a", dark: "#a1a1aa" } },
  cancelled: { label: "已撤销", variant: "success", color: { light: "#10b981", dark: "#34d399" } },
  rejected: { label: "已驳回", variant: "outline", color: { light: "#a855f7", dark: "#c084fc" } },
  failed: { label: "执行失败", variant: "danger", color: { light: "#f43f5e", dark: "#fb7185" } }
};

export const STATUSES: DeletionStatus[] = [
  "reviewing",
  "scheduled",
  "processing",
  "failed",
  "completed",
  "cancelled",
  "rejected"
];

export function statusLabel(status: string) {
  return STATUS_META[status as DeletionStatus]?.label ?? status;
}

export function DeletionStatusBadge({ status, size = "sm" }: { status: string; size?: "sm" | "default" }) {
  const meta = STATUS_META[status as DeletionStatus];
  return (
    <Badge variant={meta?.variant ?? "outline"} size={size}>
      {meta?.label ?? status}
    </Badge>
  );
}

export const MODE_META: Record<DeletionMode, { label: string; description: string; color: ThemedColor }> = {
  cooling: {
    label: "冷静期注销",
    description: "先进入冷静期，期满后自动注销，期间可撤销",
    color: { light: "#0ea5e9", dark: "#38bdf8" }
  },
  immediate: {
    label: "立即注销",
    description: "验证通过后当场注销账号",
    color: { light: "#f43f5e", dark: "#fb7185" }
  }
};

export function modeLabel(mode: string) {
  return MODE_META[mode as DeletionMode]?.label ?? mode;
}

export const SOURCE_META: Record<DeletionSource, { label: string; color: ThemedColor }> = {
  user: { label: "用户申请", color: { light: "#6366f1", dark: "#818cf8" } },
  admin: { label: "管理员发起", color: { light: "#f59e0b", dark: "#fbbf24" } }
};

export function sourceLabel(source: string) {
  return SOURCE_META[source as DeletionSource]?.label ?? source;
}

export const VERIFY_METHOD_META: Record<DeletionVerifyMethod, { label: string; description: string }> = {
  password: { label: "登录密码", description: "输入当前登录密码" },
  email: { label: "邮箱验证码", description: "向绑定邮箱发送验证码" },
  phone: { label: "短信验证码", description: "向绑定手机号发送验证码" }
};

/** 申请里记录的实际验证方式（含两步验证与最近登录放行） */
const VERIFIED_BY_LABEL: Record<string, string> = {
  password: "登录密码",
  email: "邮箱验证码",
  phone: "短信验证码",
  totp: "动态码",
  recovery: "恢复码",
  session: "最近登录"
};

export function verifiedByLabel(method: string) {
  return VERIFIED_BY_LABEL[method] ?? method;
}

/** 七项前置条件：中文名称与含义，顺序与后端 GuardKeys 一致 */
export const GUARD_META: Record<DeletionGuardKey, { label: string; description: string }> = {
  activeBan: { label: "封禁中", description: "账号存在生效中的封禁" },
  pendingOrders: { label: "待支付订单", description: "最近 48 小时内创建且尚未支付完成的订单" },
  pendingRefunds: { label: "处理中的退款", description: "存在尚未完成的退款" },
  walletBalance: { label: "钱包余额", description: "钱包仍有余额，含冻结部分" },
  activeVip: { label: "会员未到期", description: "限时会员尚未到期" },
  lifetimeVip: { label: "永久会员", description: "账号为永久会员" },
  points: { label: "剩余积分", description: "账号仍有积分" }
};

export const GUARD_KEYS: DeletionGuardKey[] = [
  "activeBan",
  "pendingOrders",
  "pendingRefunds",
  "walletBalance",
  "activeVip",
  "lifetimeVip",
  "points"
];

export const GUARD_LEVEL_META: Record<DeletionGuardLevel, { label: string; variant: BadgeVariant }> = {
  off: { label: "不检查", variant: "outline" },
  warn: { label: "提示", variant: "warning" },
  block: { label: "阻止", variant: "danger" }
};

export const LOGIN_BEHAVIOR_META: Record<DeletionLoginBehavior, { label: string; description: string }> = {
  keep: { label: "不影响冷静期", description: "冷静期内登录照常，用户可在 App 内手动撤销" },
  cancel: { label: "登录即撤销", description: "冷静期内登录视为放弃注销，申请自动撤销" }
};

export const TOMBSTONE_KIND_LABEL: Record<DeletionTombstoneKind, string> = {
  account: "账号名",
  email: "邮箱",
  phone: "手机号"
};

/** 原因：选项按应用当前配置翻成名称，自填的说明跟在后面 */
export function reasonText(request: Pick<DeletionRequest, "reasonCode" | "reasonText">, config?: DeletionConfig | null) {
  const label = request.reasonCode
    ? config?.reasons?.find((item) => item.code === request.reasonCode)?.label ?? request.reasonCode
    : "";
  const text = request.reasonText?.trim() ?? "";
  if (label && text) return `${label}：${text}`;
  return label || text;
}

/** 撤销人：user / login / admin:<账号> */
export function cancelledByLabel(value: string) {
  if (!value) return "";
  if (value === "user") return "用户本人";
  if (value === "login") return "冷静期内登录";
  if (value.startsWith("admin:")) return `管理员 ${value.slice("admin:".length)}`;
  return value;
}

/** 某个状态下管理员能做的事，与后端各接口允许的起始状态一致 */
export function availableActions(status: string) {
  return {
    approve: status === "reviewing",
    reject: status === "reviewing",
    cancel: status === "reviewing" || status === "scheduled" || status === "failed",
    execute: status === "scheduled" || status === "failed",
    reschedule: status === "scheduled"
  };
}

export function isOpenStatus(status: string) {
  return status === "reviewing" || status === "scheduled" || status === "processing" || status === "failed";
}

export function displayName(request: Pick<DeletionRequest, "nickname" | "account" | "userId">) {
  return request.nickname || request.account || `#${request.userId}`;
}

export function formatCount(value?: number) {
  return (value ?? 0).toLocaleString("zh-CN");
}

/** 列表底部的分页条 */
export function DeletionPager({
  total,
  page,
  limit,
  onPage
}: {
  total: number;
  page: number;
  limit: number;
  onPage: (page: number) => void;
}) {
  if (total <= 0) return null;
  const totalPages = Math.max(1, Math.ceil(total / Math.max(1, limit)));
  return (
    <div className="mt-3 flex items-center justify-between gap-3 text-xs text-muted-foreground">
      <span className="tabular-nums">
        共 {formatCount(total)} 条，第 {page}/{totalPages} 页
      </span>
      <div className="flex gap-1.5">
        <Button size="sm" variant="outline" className="h-7" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          上一页
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-7"
          disabled={page >= totalPages}
          onClick={() => onPage(page + 1)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}
