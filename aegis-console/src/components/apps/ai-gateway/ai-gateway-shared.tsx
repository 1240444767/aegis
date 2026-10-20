"use client";

import { Badge } from "@/components/ui/badge";
import type { AIGatewayCharge, AIGatewayPriceKey, AIGatewayRequestStatus, AIGatewaySourceKey } from "@/lib/api/ai-gateway";

/** 1 点 = 1,000,000 微点：调用记录里的 holds / charges 是微点整数。 */
export const MICRO_PER_POINT = 1_000_000;

export const STATUS_LABEL: Record<AIGatewayRequestStatus, string> = {
  reserved: "进行中",
  succeeded: "成功",
  aborted: "中途断开",
  failed: "失败",
  expired: "超时放回",
  refunded: "已退款"
};

export function RequestStatusBadge({ status }: { status: AIGatewayRequestStatus }) {
  const variant =
    status === "succeeded"
      ? "success"
      : status === "aborted"
        ? "warning"
        : status === "failed" || status === "expired"
          ? "danger"
          : status === "refunded"
            ? "info"
            : "outline";
  return (
    <Badge variant={variant} size="sm">
      {STATUS_LABEL[status] ?? status}
    </Badge>
  );
}

export const ENDPOINT_LABEL: Record<string, string> = {
  "chat.completions": "对话",
  messages: "对话（Anthropic）",
  embeddings: "嵌入",
  "images.generations": "生图",
  "images.edits": "改图",
  "audio.speech": "语音合成",
  "audio.transcriptions": "语音转写",
  "audio.translations": "语音翻译",
  rerank: "重排序"
};

export const SOURCE_LABEL: Record<AIGatewaySourceKey, string> = {
  free_daily: "每日免费",
  vip_daily: "会员每日",
  vip_monthly: "会员每月",
  credit: "赠送额度",
  integral: "积分",
  wallet: "余额"
};

export const PRICE_LABEL: Record<AIGatewayPriceKey, { label: string; unit: string }> = {
  inputPrice: { label: "输入", unit: "点 / 百万 token" },
  outputPrice: { label: "输出", unit: "点 / 百万 token" },
  requestPrice: { label: "每次调用", unit: "点" },
  imagePrice: { label: "每张图", unit: "点" },
  charPrice: { label: "字符", unit: "点 / 百万字" },
  minutePrice: { label: "音频", unit: "点 / 分钟" }
};

/** 点数展示：最多 4 位小数、去掉末尾的 0。只用于展示，计算一律在服务端。 */
export function formatPoints(value?: string | number | null) {
  if (value === null || value === undefined || value === "") return "—";
  const num = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(num)) return String(value);
  return num.toLocaleString("zh-CN", { maximumFractionDigits: 4 });
}

export function formatMicro(micro: number) {
  return formatPoints(micro / MICRO_PER_POINT);
}

export function formatTokens(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 10_000) return `${(value / 1000).toFixed(1)}K`;
  return value.toLocaleString("zh-CN");
}

/** 一笔扣费在来源上的说法：积分为积分数，余额为元，其余为点。 */
export function describeCharge(charge: AIGatewayCharge) {
  const label = SOURCE_LABEL[charge.source] ?? charge.source;
  if (charge.source === "integral") return `${label} ${charge.units ?? 0}（${formatMicro(charge.micro)} 点）`;
  if (charge.source === "wallet") return `${label} ¥${((charge.units ?? 0) / 100).toFixed(2)}（${formatMicro(charge.micro)} 点）`;
  return `${label} ${formatMicro(charge.micro)} 点`;
}

/** 调用记录的用量摘要。 */
export function describeUsage(record: {
  inputTokens: number;
  outputTokens: number;
  images: number;
  characters: number;
  audioMs: number;
}) {
  const parts: string[] = [];
  if (record.inputTokens || record.outputTokens) {
    parts.push(`${formatTokens(record.inputTokens)} / ${formatTokens(record.outputTokens)}`);
  }
  if (record.images) parts.push(`${record.images} 张`);
  if (record.characters) parts.push(`${record.characters.toLocaleString("zh-CN")} 字`);
  if (record.audioMs) parts.push(`${(record.audioMs / 1000).toFixed(1)} 秒`);
  return parts.join(" · ") || "—";
}
