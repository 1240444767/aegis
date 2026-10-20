"use client";

import { useState } from "react";
import { Loader2, ReceiptText, RotateCcw, X } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import {
  ENDPOINT_LABEL,
  RequestStatusBadge,
  STATUS_LABEL,
  describeCharge,
  describeUsage,
  formatPoints
} from "@/components/apps/ai-gateway/ai-gateway-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { useAIGatewayRequestsQuery, useRefundAIGatewayRequestMutation } from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewayRequest, AIGatewayRequestStatus } from "@/lib/api/ai-gateway";
import { formatDateTime } from "@/lib/datetime";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const PAGE_SIZE = 20;
const ALL = "__all__";
const REFUND_REASONS = ["上游返回内容异常", "用户投诉，人工核实", "重复调用"];

export function AIGatewayRequestsPanel({
  appKey,
  initialFilter
}: {
  appKey: string;
  initialFilter: { userId?: number; keyId?: number; model?: string };
}) {
  const [user, setUser] = useState<PickedUser | null>(initialFilter.userId ? { id: initialFilter.userId } : null);
  const [keyId, setKeyId] = useState<number | undefined>(initialFilter.keyId);
  const [model, setModel] = useState(initialFilter.model ?? "");
  const [status, setStatus] = useState<string>(ALL);
  const [endpoint, setEndpoint] = useState<string>(ALL);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<AIGatewayRequest | null>(null);
  const debouncedModel = useDebouncedValue(model.trim(), 300);

  const query = useAIGatewayRequestsQuery(appKey, {
    userId: user?.id,
    keyId,
    model: debouncedModel || undefined,
    status: status === ALL ? undefined : status,
    endpoint: endpoint === ALL ? undefined : endpoint,
    page,
    limit: PAGE_SIZE
  });
  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <>
      <SectionCard icon={<ReceiptText className="size-4" />} title="调用记录">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <div className="w-60">
            <UserPicker appKey={appKey} value={user} onChange={(value) => { setUser(value); setPage(1); }} />
          </div>
          <Input value={model} placeholder="模型" className="h-8 w-40 font-mono text-xs"
            onChange={(event) => { setModel(event.target.value); setPage(1); }} />
          <Select value={status} onValueChange={(value) => { setStatus(value); setPage(1); }}>
            <SelectTrigger className="h-8 w-32 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部状态</SelectItem>
              {(Object.keys(STATUS_LABEL) as AIGatewayRequestStatus[]).map((key) => (
                <SelectItem key={key} value={key}>
                  {STATUS_LABEL[key]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={endpoint} onValueChange={(value) => { setEndpoint(value); setPage(1); }}>
            <SelectTrigger className="h-8 w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部接口</SelectItem>
              {Object.entries(ENDPOINT_LABEL).map(([key, label]) => (
                <SelectItem key={key} value={key}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {keyId ? (
            <Badge variant="outline" className="gap-1">
              密钥 #{keyId}
              <button type="button" aria-label="清除密钥筛选" onClick={() => { setKeyId(undefined); setPage(1); }}>
                <X className="size-3" />
              </button>
            </Badge>
          ) : null}
          <span className="ml-auto text-xs text-muted-foreground tabular-nums">共 {total} 条</span>
        </div>

        {query.isLoading ? (
          <Skeleton className="h-60 w-full rounded-xl" />
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-10 text-center text-xs text-muted-foreground">
            暂无调用记录
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>用户</TableHead>
                <TableHead>模型</TableHead>
                <TableHead>用量</TableHead>
                <TableHead className="text-right">扣费（点）</TableHead>
                <TableHead>状态</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.requestNo} className="cursor-pointer" onClick={() => setSelected(item)}>
                  <TableCell className="whitespace-nowrap text-xs tabular-nums">{formatDateTime(item.createdAt)}</TableCell>
                  <TableCell className="text-xs">
                    <button type="button" className="hover:underline"
                      onClick={(event) => { event.stopPropagation(); setUser({ id: item.userId, account: item.account }); setPage(1); }}>
                      {item.account || `#${item.userId}`}
                    </button>
                  </TableCell>
                  <TableCell>
                    <div className="font-mono text-xs">{item.model}</div>
                    <div className="text-[11px] text-muted-foreground">
                      {ENDPOINT_LABEL[item.endpoint] ?? item.endpoint}
                      {item.stream ? " · 流式" : ""}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs tabular-nums">
                    {describeUsage(item)}
                    {item.usageEstimated ? <span className="ml-1 text-muted-foreground">（估）</span> : null}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{formatPoints(item.charged)}</TableCell>
                  <TableCell>
                    <RequestStatusBadge status={item.status} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}

        {totalPages > 1 ? (
          <div className="mt-3 flex items-center justify-end gap-2 text-xs">
            <Button size="xs" variant="outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              上一页
            </Button>
            <span className="tabular-nums text-muted-foreground">
              {page} / {totalPages}
            </span>
            <Button size="xs" variant="outline" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
              下一页
            </Button>
          </div>
        ) : null}
      </SectionCard>

      <RequestDetailDialog appKey={appKey} record={selected} onClose={() => setSelected(null)} onChanged={setSelected} />
    </>
  );
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[6rem_minmax(0,1fr)] gap-2 py-1.5 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

function RequestDetailDialog({
  appKey,
  record,
  onClose,
  onChanged
}: {
  appKey: string;
  record: AIGatewayRequest | null;
  onClose: () => void;
  onChanged: (record: AIGatewayRequest) => void;
}) {
  const refundMutation = useRefundAIGatewayRequestMutation(appKey);
  const [refunding, setRefunding] = useState(false);
  const [reason, setReason] = useState("");
  const refundable = record?.status === "succeeded" || record?.status === "aborted";

  const refund = async () => {
    if (!record) return;
    try {
      const updated = await refundMutation.mutateAsync({ requestNo: record.requestNo, reason: reason.trim() });
      toast.success("已退款");
      setRefunding(false);
      setReason("");
      onChanged(updated);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "退款失败");
    }
  };

  return (
    <Dialog open={Boolean(record)} onOpenChange={(open) => { if (!open) { setRefunding(false); onClose(); } }}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 font-mono text-sm">
            {record?.requestNo}
            {record ? <RequestStatusBadge status={record.status} /> : null}
          </DialogTitle>
        </DialogHeader>
        {record ? (
          <div className="divide-y divide-border">
            <DetailRow label="用户">{record.account || `#${record.userId}`}</DetailRow>
            <DetailRow label="凭据">
              {record.authKind === "key" ? `密钥 ${record.keyHint ?? ""}` : "登录令牌"}
            </DetailRow>
            <DetailRow label="接口">
              {ENDPOINT_LABEL[record.endpoint] ?? record.endpoint}
              {record.stream ? " · 流式" : ""}
            </DetailRow>
            <DetailRow label="模型">
              <span className="font-mono">{record.model}</span>
              {record.upstreamModel && record.upstreamModel !== record.model ? (
                <span className="text-muted-foreground"> → {record.upstreamModel}</span>
              ) : null}
              {record.provider ? <span className="text-muted-foreground"> · {record.provider}</span> : null}
            </DetailRow>
            <DetailRow label="用量">
              {describeUsage(record)}
              {record.usageEstimated ? "（估算）" : ""}
              {record.clampMaxTokens ? ` · 输出上限压到 ${record.clampMaxTokens}` : ""}
            </DetailRow>
            <DetailRow label="倍率">{record.factor}</DetailRow>
            <DetailRow label="暂扣">{formatPoints(record.reserved)} 点</DetailRow>
            <DetailRow label="价格">{formatPoints(record.cost)} 点</DetailRow>
            <DetailRow label="实扣">
              {formatPoints(record.charged)} 点
              {Number(record.shortfall) > 0 ? (
                <span className="text-amber-600 dark:text-amber-400"> · 未收 {formatPoints(record.shortfall)} 点</span>
              ) : null}
            </DetailRow>
            {record.charges.length > 0 ? (
              <DetailRow label="扣费明细">
                <div className="space-y-0.5">
                  {record.charges.map((charge, index) => (
                    <div key={`${charge.source}-${index}`}>{describeCharge(charge)}</div>
                  ))}
                </div>
              </DetailRow>
            ) : null}
            {record.integralTxnNo || record.walletTxnNo ? (
              <DetailRow label="流水号">
                <span className="font-mono">{[record.integralTxnNo, record.walletTxnNo].filter(Boolean).join(" / ")}</span>
              </DetailRow>
            ) : null}
            {record.errorMessage ? (
              <DetailRow label="错误">
                <span className="text-destructive">{record.errorMessage}</span>
                {record.errorCode ? <span className="text-muted-foreground"> · {record.errorCode}</span> : null}
              </DetailRow>
            ) : null}
            <DetailRow label="耗时">{record.latencyMs} ms</DetailRow>
            <DetailRow label="时间">
              {formatDateTime(record.createdAt)}
              {record.settledAt ? ` → ${formatDateTime(record.settledAt)}` : ""}
            </DetailRow>
            <DetailRow label="来源">
              {record.clientIp || "—"}
              {record.userAgent ? <span className="text-muted-foreground"> · {record.userAgent}</span> : null}
            </DetailRow>
            {record.status === "refunded" ? (
              <DetailRow label="退款">
                {formatDateTime(record.refundedAt)}
                {record.refundedBy ? ` · ${record.refundedBy}` : ""}
                {record.refundReason ? ` · ${record.refundReason}` : ""}
              </DetailRow>
            ) : null}
          </div>
        ) : null}

        {refunding ? (
          <div className="space-y-2 rounded-xl border border-border p-3">
            <div className="flex flex-wrap gap-1.5">
              {REFUND_REASONS.map((item) => (
                <Button key={item} size="xs" variant={reason === item ? "secondary" : "outline"} onClick={() => setReason(item)}>
                  {item}
                </Button>
              ))}
            </div>
            <Textarea value={reason} maxLength={200} rows={2} placeholder="退款原因" className="text-xs"
              onChange={(event) => setReason(event.target.value)} />
          </div>
        ) : null}

        <DialogFooter>
          {refundable && !refunding ? (
            <Button size="sm" variant="outline" onClick={() => setRefunding(true)}>
              <RotateCcw className="size-3.5" />
              整笔退款
            </Button>
          ) : null}
          {refunding ? (
            <>
              <Button size="sm" variant="outline" onClick={() => setRefunding(false)}>
                取消
              </Button>
              <Button size="sm" variant="destructive" onClick={() => void refund()} disabled={refundMutation.isPending}>
                {refundMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
                退回 {formatPoints(record?.charged)} 点
              </Button>
            </>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
