"use client";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type {
  InviteAudience,
  InviteCharset,
  InviteCode,
  InviteCodeKind,
  InviteRegisterField,
  InviteRewardResult,
  InviteSource,
  InviteTrigger,
  InviterStatus
} from "@/lib/api/invite";
import { formatDateTimeMinute, parseInstant } from "@/lib/datetime";

/**
 * 邀请的展示口径：枚举文案、状态徽标、示例码、分页条。集中在一处，
 * 概览、配置、邀请码与邀请记录说的是同一套话。
 */

export const REGISTER_FIELD_META: Record<InviteRegisterField, { title: string; description: string }> = {
  hidden: { title: "不出现", description: "注册表单不显示，注册后可在时限内补填" },
  optional: { title: "选填", description: "注册表单显示，可不填" },
  required: { title: "必填", description: "没有邀请码不能注册" }
};

export const CHARSET_LABEL: Record<InviteCharset, string> = {
  alnum: "字母与数字",
  digits: "纯数字",
  letters: "纯字母"
};

/** 与后端 invite/code.go 的字符集一致：去掉 0 O 1 I 这类容易看错的字符 */
const CHARSET_ALPHABET: Record<InviteCharset, string> = {
  alnum: "ABCDEFGHJKLMNPQRSTUVWXYZ23456789",
  digits: "0123456789",
  letters: "ABCDEFGHJKLMNPQRSTUVWXYZ"
};

export const AUDIENCE_META: Record<InviteAudience, { title: string; description: string }> = {
  all: { title: "所有用户", description: "满足等级门槛即可邀请" },
  vip: { title: "会员", description: "有效会员，含永久与试用" }
};

export const TRIGGER_META: Record<InviteTrigger, { title: string; description: string }> = {
  bind: { title: "填写即发", description: "被邀请人填写邀请码后立即发放" },
  signin: { title: "签到后发", description: "被邀请人累计签到满指定天数后发放" }
};

export const CODE_KIND_LABEL: Record<InviteCodeKind, string> = {
  personal: "个人码",
  campaign: "活动码"
};

export const SOURCE_LABEL: Record<InviteSource, string> = {
  register: "注册时填写",
  bind: "注册后补填"
};

const INVITER_STATUS_META: Record<
  InviterStatus,
  { label: string; variant: "success" | "warning" | "secondary" | "info" | "outline" }
> = {
  none: { label: "无邀请人", variant: "outline" },
  pending: { label: "等待中", variant: "warning" },
  granted: { label: "已发放", variant: "success" },
  limited: { label: "已达上限", variant: "info" },
  cancelled: { label: "已取消", variant: "secondary" }
};

export const INVITER_STATUSES: InviterStatus[] = ["pending", "granted", "limited", "cancelled", "none"];

export function inviterStatusLabel(status: InviterStatus) {
  return INVITER_STATUS_META[status]?.label ?? status;
}

export function InviterStatusBadge({ status }: { status: InviterStatus }) {
  const meta = INVITER_STATUS_META[status] ?? INVITER_STATUS_META.none;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

/** 实际发放结果：一项一个徽标，带流水号提示 */
export function ResultBadges({ results, empty = "无" }: { results: InviteRewardResult[]; empty?: string }) {
  if (!results.length) return <span className="text-xs text-muted-foreground">{empty}</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {results.map((result, index) => (
        <Badge
          key={`${result.type}-${index}`}
          variant="outline"
          size="sm"
          className="font-normal"
          title={result.transactionNo ? `流水 ${result.transactionNo}` : undefined}
        >
          {result.detail || result.label}
        </Badge>
      ))}
    </div>
  );
}

/**
 * 示例码：前缀 + 主体。主体由种子确定性地取字符（同一种子同一结果），
 * 渲染期不调用随机数；「换一个」只是换种子。
 */
export function sampleInviteCode(charset: InviteCharset, length: number, prefix: string, seed: number) {
  const alphabet = CHARSET_ALPHABET[charset] ?? CHARSET_ALPHABET.alnum;
  const size = Math.max(0, Math.min(32, Math.floor(length) || 0));
  let state = (seed * 2654435761) >>> 0;
  let body = "";
  for (let index = 0; index < size; index += 1) {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    body += alphabet[state % alphabet.length];
  }
  return `${normalizeCodeInput(prefix)}${body}`;
}

/** 与后端 NormalizeCode 一致：去空白与连字符、转大写 */
export function normalizeCodeInput(raw: string) {
  return raw.replace(/[\s\-　]/g, "").toUpperCase();
}

/** 码此刻的可用状态：停用 / 未开始 / 已结束 / 已用完，都不是则为空 */
export function codeAvailability(code: InviteCode, now = Date.now()) {
  if (code.status !== "active") return { label: "已停用", variant: "secondary" as const };
  const ends = parseInstant(code.endsAt);
  if (ends && ends.getTime() <= now) return { label: "已结束", variant: "secondary" as const };
  const starts = parseInstant(code.startsAt);
  if (starts && starts.getTime() > now) return { label: "未开始", variant: "info" as const };
  if (code.maxUses && code.usedCount >= code.maxUses) return { label: "已用完", variant: "warning" as const };
  return null;
}

/** 有效期：两头都有写「起 至 止」，只有一头写「起」或「截至」，都没有为「长期」 */
export function validityLabel(startsAt?: string, endsAt?: string) {
  if (startsAt && endsAt) return `${formatDateTimeMinute(startsAt)} 至 ${formatDateTimeMinute(endsAt)}`;
  if (startsAt) return `${formatDateTimeMinute(startsAt)} 起`;
  if (endsAt) return `截至 ${formatDateTimeMinute(endsAt)}`;
  return "长期";
}

export function formatCount(value?: number) {
  return (value ?? 0).toLocaleString("zh-CN");
}

/** 列表底部的分页条 */
export function InvitePager({
  total,
  page,
  totalPages,
  onPage
}: {
  total: number;
  page: number;
  totalPages: number;
  onPage: (page: number) => void;
}) {
  if (total <= 0) return null;
  return (
    <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
      <span className="tabular-nums">
        共 {total} 条，第 {page}/{totalPages} 页
      </span>
      <div className="flex gap-1.5">
        <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          上一页
        </Button>
        <Button size="sm" variant="outline" disabled={page >= totalPages} onClick={() => onPage(page + 1)}>
          下一页
        </Button>
      </div>
    </div>
  );
}
