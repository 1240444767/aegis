"use client";

import { useState } from "react";
import { Coins, Crown, Loader2, Minus, Plus, ReceiptText, UserRound } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { SOURCE_LABEL, formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { StatTile } from "@/components/content/content-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  useAdjustAIGatewayCreditMutation,
  useAIGatewayUserAccountQuery,
  useAIGatewayUserCreditsQuery
} from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";

const AMOUNTS = ["10", "50", "100", "500"];
const REASONS = ["客服补偿", "活动奖励", "调用异常补偿", "误发收回"];
const KIND_LABEL: Record<string, string> = { grant: "看广告", consume: "调用", refund: "退款", adjust: "管理员调整" };

/**
 * 某个用户的 AI 账户：现在能用多少（与用户自己看到的总览同一份）、赠送额度的调整与流水。
 *
 * 调整用「方向 + 恒为正数的数量」，不用负数表示扣减（交易中心「别让管理员当输入机」那一节）。
 * 每次提交带一个请求编号，网络重试不会加两次。
 */
export function AIGatewayCreditsPanel({
  appKey,
  onOpenRequests
}: {
  appKey: string;
  onOpenRequests: (filter: { userId?: number }) => void;
}) {
  const [user, setUser] = useState<PickedUser | null>(null);
  const [direction, setDirection] = useState<"add" | "sub">("add");
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const [requestId, setRequestId] = useState(() => crypto.randomUUID());
  const [page, setPage] = useState(1);

  const accountQuery = useAIGatewayUserAccountQuery(appKey, user?.id);
  const creditsQuery = useAIGatewayUserCreditsQuery(appKey, user?.id ?? null, page);
  const adjustMutation = useAdjustAIGatewayCreditMutation(appKey);
  const account = accountQuery.data;
  const entries = creditsQuery.data?.items ?? [];
  const total = creditsQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / 20));

  const submit = async () => {
    if (!user) return;
    if (!/^\d+(\.\d{1,6})?$/.test(amount.trim()) || Number(amount) <= 0) {
      toast.error("数量需为正数，最多 6 位小数");
      return;
    }
    try {
      const result = await adjustMutation.mutateAsync({
        userId: user.id,
        payload: { amount: direction === "sub" ? `-${amount.trim()}` : amount.trim(), reason: reason.trim(), requestId }
      });
      toast.success(result.replayed ? "这笔调整已经生效过" : `已调整，余额 ${formatPoints(result.balance)} 点`);
      setAmount("");
      setRequestId(crypto.randomUUID());
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
    }
  };

  return (
    <div className="space-y-4">
      <SectionCard icon={<UserRound className="size-4" />} title="用户">
        <div className="flex flex-wrap items-center gap-2">
          <div className="w-72">
            <UserPicker appKey={appKey} value={user} onChange={(value) => { setUser(value); setPage(1); }} />
          </div>
          {user ? (
            <Button size="sm" variant="outline" onClick={() => onOpenRequests({ userId: user.id })}>
              <ReceiptText className="size-3.5" />
              调用记录
            </Button>
          ) : null}
        </div>
      </SectionCard>

      {!user ? null : accountQuery.isLoading ? (
        <Skeleton className="h-40 w-full rounded-2xl" />
      ) : account ? (
        <>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <StatTile label="现在可用" icon={Coins} tone="positive" value={`${formatPoints(account.available)} 点`}
              hint={`倍率 ${account.factor}`} />
            <StatTile label="赠送额度" value={`${formatPoints(account.credit)} 点`}
              hint={Number(account.creditFrozen) > 0 ? `暂扣中 ${formatPoints(account.creditFrozen)}` : undefined} />
            <StatTile label="积分 / 余额" value={`${account.integral} / ¥${account.wallet}`} />
            <StatTile label="今日已用" value={`${formatPoints(account.todaySpent)} 点`}
              hint={`累计 ${formatPoints(account.totalSpent)} 点 · ${account.totalRequests} 次`} />
          </div>

          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl border border-border bg-card px-4 py-3 text-xs">
            {account.profile.isVip ? (
              <Badge variant="warning" size="sm" className="gap-1"><Crown className="size-3" />会员</Badge>
            ) : (
              <Badge variant="outline" size="sm">非会员</Badge>
            )}
            <span className="text-muted-foreground">Lv.{account.profile.level}</span>
            {account.quotas.map((quota) => (
              <span key={quota.source} className={quota.eligible ? "" : "text-muted-foreground line-through"}>
                {SOURCE_LABEL[quota.source] ?? quota.label} {formatPoints(quota.remaining)} / {formatPoints(quota.limit)}
              </span>
            ))}
            {!account.eligible ? <span className="text-amber-600 dark:text-amber-400">仅会员可用</span> : null}
          </div>

          <SectionCard icon={<Coins className="size-4" />} title="调整赠送额度">
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <ToggleGroup type="single" size="sm" value={direction}
                  onValueChange={(value) => value && setDirection(value as "add" | "sub")}>
                  <ToggleGroupItem value="add" className="h-8 gap-1 px-3 text-xs"><Plus className="size-3" />增加</ToggleGroupItem>
                  <ToggleGroupItem value="sub" className="h-8 gap-1 px-3 text-xs"><Minus className="size-3" />扣减</ToggleGroupItem>
                </ToggleGroup>
                <Input value={amount} inputMode="decimal" placeholder="点数" className="h-8 w-32 font-mono text-xs"
                  onChange={(event) => setAmount(event.target.value)} />
                {AMOUNTS.map((item) => (
                  <Button key={item} size="xs" variant={amount === item ? "secondary" : "outline"} onClick={() => setAmount(item)}>
                    {item}
                  </Button>
                ))}
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Input value={reason} maxLength={200} placeholder="原因" className="h-8 w-72 text-xs"
                  onChange={(event) => setReason(event.target.value)} />
                {REASONS.map((item) => (
                  <Button key={item} size="xs" variant={reason === item ? "secondary" : "outline"} onClick={() => setReason(item)}>
                    {item}
                  </Button>
                ))}
                <Button size="sm" className="ml-auto" onClick={() => void submit()} disabled={adjustMutation.isPending || !amount}>
                  {adjustMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
                  {direction === "add" ? "增加" : "扣减"}
                </Button>
              </div>
            </div>
          </SectionCard>

          <SectionCard icon={<ReceiptText className="size-4" />} title="赠送额度流水">
            {creditsQuery.isLoading ? (
              <Skeleton className="h-40 w-full rounded-xl" />
            ) : entries.length === 0 ? (
              <p className="py-8 text-center text-xs text-muted-foreground">暂无流水</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>时间</TableHead>
                    <TableHead>类型</TableHead>
                    <TableHead className="text-right">变动（点）</TableHead>
                    <TableHead className="text-right">余额</TableHead>
                    <TableHead>说明</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {entries.map((entry) => (
                    <TableRow key={entry.id}>
                      <TableCell className="whitespace-nowrap text-xs tabular-nums">{formatDateTime(entry.createdAt)}</TableCell>
                      <TableCell className="text-xs">{KIND_LABEL[entry.kind] ?? entry.kind}</TableCell>
                      <TableCell className={`text-right font-mono text-xs tabular-nums ${Number(entry.amount) > 0 ? "text-emerald-600" : ""}`}>
                        {Number(entry.amount) > 0 ? "+" : ""}
                        {formatPoints(entry.amount)}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs tabular-nums">{formatPoints(entry.balanceAfter)}</TableCell>
                      <TableCell className="max-w-64 truncate text-xs text-muted-foreground">
                        {entry.remark}
                        {entry.operator ? ` · ${entry.operator}` : ""}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            {totalPages > 1 ? (
              <div className="mt-3 flex items-center justify-end gap-2 text-xs">
                <Button size="xs" variant="outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
                <span className="tabular-nums text-muted-foreground">{page} / {totalPages}</span>
                <Button size="xs" variant="outline" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>下一页</Button>
              </div>
            ) : null}
          </SectionCard>
        </>
      ) : null}
    </div>
  );
}
