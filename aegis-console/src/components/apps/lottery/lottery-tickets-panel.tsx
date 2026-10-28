"use client";

import { useState } from "react";
import { ArrowRight, Coins, Gift, Loader2, Minus, Plus, ReceiptText, Search, ShoppingBag, Ticket, Users, Wallet } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { formatCount } from "@/components/apps/lottery/lottery-shared";
import { formatStoreDate } from "@/components/apps/points-store/points-store-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { StatTile } from "@/components/content/content-shared";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { LotteryLedgerKind, LotteryTicketOrigin, LotteryTicketOverview } from "@/lib/api/lottery";
import {
  useAdjustLotteryTicketsMutation,
  useLotteryTicketAccountQuery,
  useLotteryTicketAccountsQuery,
  useLotteryTicketLedgerQuery,
  useLotteryTicketOverviewQuery
} from "@/lib/lottery-hooks";
import { cn } from "@/lib/utils";

/**
 * 抽奖次数：bonus 的分批账本。
 *
 * | 区块 | 回答什么 |
 * |---|---|
 * | 总览 | 多少人手上有次数、还剩多少（赠送 / 兑换两档）、今天发了与用了几次、各来源发了多少还剩多少 |
 * | 账户 | 谁手上有次数；选一个人看他的批次（排在前面的先用），发放或扣减 |
 * | 流水 | 每一次获得、抽奖使用、扣减，带变动后余额与来源单号 |
 *
 * 扣除顺序只有一条（后端 lottery.ConsumptionOrder）：今日免费 → 赠送的（先到先用）→ 兑换的（先到先用）→ 积分。
 * 管理员扣减按同一顺序，先扣赠送的。
 */

const ORIGIN_LABEL: Record<LotteryTicketOrigin, string> = {
  rewarded_ad: "看视频",
  lottery: "抽奖所得",
  invite: "邀请奖励",
  leaderboard: "排行榜奖励",
  cardkey: "卡密",
  points_store: "积分兑换",
  admin: "系统发放",
  legacy: "早期余额"
};

const ORIGINS = Object.keys(ORIGIN_LABEL) as LotteryTicketOrigin[];

const KIND_META: Record<LotteryLedgerKind, { label: string; variant: "success" | "secondary" | "warning" }> = {
  grant: { label: "获得", variant: "success" },
  consume: { label: "抽奖使用", variant: "secondary" },
  deduct: { label: "系统扣减", variant: "warning" }
};

function errorText(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

function TierBadge({ paid }: { paid: boolean }) {
  return (
    <Badge variant={paid ? "info" : "success"} size="sm">
      {paid ? "兑换" : "赠送"}
    </Badge>
  );
}

function userLabel(item: { account?: string; nickname?: string; userId?: number }) {
  return item.nickname || item.account || (item.userId ? `UID-${item.userId}` : "—");
}

function Pager({ page, totalPages, onChange }: { page: number; totalPages: number; onChange: (page: number) => void }) {
  if (totalPages <= 1) return null;
  return (
    <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
      <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => onChange(page - 1)}>
        上一页
      </Button>
      <span className="tabular-nums">
        {page} / {totalPages}
      </span>
      <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => onChange(page + 1)}>
        下一页
      </Button>
    </div>
  );
}

/** 扣除顺序：一排步骤，箭头连起来 */
function ConsumptionOrder({ order }: { order: string[] }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {order.map((step, index) => (
        <div key={step} className="flex items-center gap-1.5">
          <span className="flex items-center gap-1.5 rounded-lg border border-border bg-muted/40 px-2.5 py-1 text-xs">
            <span className="grid size-4 place-items-center rounded-full bg-foreground text-[10px] font-semibold tabular-nums text-background">
              {index + 1}
            </span>
            {step}
          </span>
          {index < order.length - 1 ? <ArrowRight className="size-3.5 text-muted-foreground" /> : null}
        </div>
      ))}
    </div>
  );
}

export function LotteryTicketsPanel({ appKey }: { appKey: string }) {
  const [selected, setSelected] = useState<PickedUser | null>(null);
  return (
    <div className="space-y-4">
      <OverviewSection appKey={appKey} />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <AccountsSection appKey={appKey} selected={selected} onSelect={setSelected} />
        <AccountSection appKey={appKey} selected={selected} onSelect={setSelected} />
      </div>
      <LedgerSection appKey={appKey} user={selected} />
    </div>
  );
}

function OverviewSection({ appKey }: { appKey: string }) {
  const query = useLotteryTicketOverviewQuery(appKey);
  const overview: LotteryTicketOverview | undefined = query.data;
  return (
    <SectionCard
      icon={<Ticket className="size-4" />}
      title="抽奖次数"
      description="看视频、抽中、邀请、排行榜、卡密、积分兑换发的次数分批记账，任意活动可用"
    >
      {query.isLoading || !overview ? (
        <Skeleton className="h-40 w-full rounded-xl" />
      ) : (
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatTile icon={Users} label="持有人数" value={formatCount(overview.holders)} />
            <StatTile
              icon={Wallet}
              label="未用次数"
              value={formatCount(overview.outstanding)}
              hint={`赠送 ${formatCount(overview.tickets.gift)}，兑换 ${formatCount(overview.tickets.purchased)}`}
            />
            <StatTile icon={Plus} label="今日发放" value={formatCount(overview.grantedToday)} tone="positive" />
            <StatTile icon={Minus} label="今日抽奖使用" value={formatCount(overview.usedToday)} />
          </div>
          <div className="space-y-2">
            <p className="text-xs font-medium text-muted-foreground">扣除顺序</p>
            <ConsumptionOrder order={overview.order} />
          </div>
          {overview.origins.length ? (
            <div className="overflow-x-auto rounded-xl border border-border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>来源</TableHead>
                    <TableHead>档</TableHead>
                    <TableHead className="text-right">累计发放</TableHead>
                    <TableHead className="text-right">剩余</TableHead>
                    <TableHead className="text-right">人数</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {overview.origins.map((item) => (
                    <TableRow key={`${item.origin}-${item.paid}`}>
                      <TableCell className="text-xs">{item.originLabel}</TableCell>
                      <TableCell>
                        <TierBadge paid={item.paid} />
                      </TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{formatCount(item.granted)}</TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{formatCount(item.remaining)}</TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{formatCount(item.users)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : (
            <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
              还没有发放过抽奖次数
            </p>
          )}
        </div>
      )}
    </SectionCard>
  );
}

function AccountsSection({
  appKey,
  selected,
  onSelect
}: {
  appKey: string;
  selected: PickedUser | null;
  onSelect: (user: PickedUser) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [page, setPage] = useState(1);
  const query = useLotteryTicketAccountsQuery(appKey, { keyword: submitted || undefined, page, limit: 10 });
  const items = query.data?.items ?? [];
  return (
    <SectionCard icon={<Users className="size-4" />} title="账户" description="手上有次数的人，余额多的在前">
      <div className="space-y-3">
        <form
          className="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            setSubmitted(keyword.trim());
            setPage(1);
          }}
        >
          <Input value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="账号、昵称或用户 ID" />
          <Button type="submit" variant="outline" size="sm">
            <Search className="size-3.5" />
            搜索
          </Button>
        </form>
        {query.isLoading ? (
          <Skeleton className="h-40 w-full rounded-xl" />
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">没有账户</p>
        ) : (
          <div className="overflow-x-auto rounded-xl border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户</TableHead>
                  <TableHead className="text-right">余额</TableHead>
                  <TableHead className="text-right">赠送</TableHead>
                  <TableHead className="text-right">兑换</TableHead>
                  <TableHead className="text-right">累计发放 / 使用</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow
                    key={item.userId}
                    className={cn("cursor-pointer", selected?.id === item.userId && "bg-muted/60")}
                    onClick={() =>
                      item.userId && onSelect({ id: item.userId, account: item.account, nickname: item.nickname })
                    }
                  >
                    <TableCell className="text-xs">
                      <div className="flex flex-col">
                        <span>{userLabel(item)}</span>
                        <span className="font-mono text-[10px] text-muted-foreground">UID-{item.userId}</span>
                      </div>
                    </TableCell>
                    <TableCell className="text-right text-sm font-semibold tabular-nums">{formatCount(item.balance)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{formatCount(item.tickets.gift)}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{formatCount(item.tickets.purchased)}</TableCell>
                    <TableCell className="text-right text-[11px] tabular-nums text-muted-foreground">
                      {formatCount(item.totalGranted)} / {formatCount(item.totalConsumed)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        <Pager page={page} totalPages={query.data?.totalPages ?? 1} onChange={setPage} />
      </div>
    </SectionCard>
  );
}

function AccountSection({
  appKey,
  selected,
  onSelect
}: {
  appKey: string;
  selected: PickedUser | null;
  onSelect: (user: PickedUser | null) => void;
}) {
  const query = useLotteryTicketAccountQuery(appKey, selected?.id);
  const adjust = useAdjustLotteryTicketsMutation(appKey);
  const [direction, setDirection] = useState<"grant" | "deduct">("grant");
  const [paid, setPaid] = useState(false);
  const [amount, setAmount] = useState("1");
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState(false);
  const account = query.data;
  const count = Math.trunc(Number(amount));
  const valid = Number.isFinite(count) && count > 0 && count <= 10000 && reason.trim().length > 0;

  const submit = async () => {
    if (!selected) return;
    try {
      await adjust.mutateAsync({
        userId: selected.id,
        payload: {
          amount: direction === "grant" ? count : -count,
          paid: direction === "grant" ? paid : undefined,
          reason: reason.trim()
        }
      });
      toast.success(direction === "grant" ? `已发放 ${count} 次` : `已扣减 ${count} 次`);
      setConfirming(false);
      setReason("");
    } catch (error) {
      toast.error(errorText(error, "操作失败"));
    }
  };

  return (
    <SectionCard icon={<Coins className="size-4" />} title="一个人的次数" description="批次按使用顺序排列，第一批最先用">
      <div className="space-y-4">
        <UserPicker appKey={appKey} value={selected} onChange={onSelect} />
        {!selected ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            在左侧账户里选一个人，或在这里搜索
          </p>
        ) : query.isLoading || !account ? (
          <Skeleton className="h-48 w-full rounded-xl" />
        ) : (
          <>
            <div className="grid grid-cols-3 gap-3">
              <StatTile icon={Wallet} label="余额" value={formatCount(account.balance)} />
              <StatTile icon={Gift} label="赠送的" value={formatCount(account.tickets.gift)} hint="先用" />
              <StatTile icon={ShoppingBag} label="兑换的" value={formatCount(account.tickets.purchased)} hint="后用" />
            </div>
            {account.lots.length ? (
              <ol className="space-y-1.5">
                {account.lots.map((lot, index) => (
                  <li key={lot.id} className="flex items-center gap-3 rounded-xl border border-border px-3 py-2">
                    <span
                      className={cn(
                        "grid size-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold tabular-nums",
                        index === 0 ? "bg-foreground text-background" : "bg-muted text-muted-foreground"
                      )}
                    >
                      {index + 1}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5 text-xs">
                        <span className="truncate font-medium">{lot.title || lot.originLabel}</span>
                        <TierBadge paid={lot.paid} />
                      </div>
                      <p className="truncate text-[11px] text-muted-foreground">
                        {lot.originLabel}
                        {lot.ref ? `，${lot.ref}` : ""}，{formatStoreDate(lot.createdAt)}
                      </p>
                    </div>
                    <span className="text-sm font-semibold tabular-nums">
                      {formatCount(lot.remaining)}
                      <span className="text-[11px] font-normal text-muted-foreground"> / {formatCount(lot.amount)}</span>
                    </span>
                  </li>
                ))}
              </ol>
            ) : (
              <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">
                没有剩余的次数
              </p>
            )}

            <div className="space-y-3 rounded-xl border border-border bg-muted/30 p-3">
              <div className="flex flex-wrap items-center gap-2">
                <ToggleGroup
                  type="single"
                  size="sm"
                  variant="outline"
                  value={direction}
                  onValueChange={(value) => value && setDirection(value as "grant" | "deduct")}
                >
                  <ToggleGroupItem value="grant">
                    <Plus className="size-3.5" />
                    发放
                  </ToggleGroupItem>
                  <ToggleGroupItem value="deduct">
                    <Minus className="size-3.5" />
                    扣减
                  </ToggleGroupItem>
                </ToggleGroup>
                {direction === "grant" ? (
                  <ToggleGroup
                    type="single"
                    size="sm"
                    variant="outline"
                    value={paid ? "paid" : "gift"}
                    onValueChange={(value) => value && setPaid(value === "paid")}
                  >
                    <ToggleGroupItem value="gift">记为赠送</ToggleGroupItem>
                    <ToggleGroupItem value="paid">记为兑换</ToggleGroupItem>
                  </ToggleGroup>
                ) : (
                  <span className="text-[11px] text-muted-foreground">按扣除顺序先扣赠送的</span>
                )}
              </div>
              <div className="grid gap-2 sm:grid-cols-[8rem_minmax(0,1fr)]">
                <div className="space-y-1">
                  <Label className="text-xs">次数</Label>
                  <Input type="number" min={1} max={10000} value={amount} onChange={(event) => setAmount(event.target.value)} />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs">原因</Label>
                  <Input
                    value={reason}
                    maxLength={100}
                    onChange={(event) => setReason(event.target.value)}
                    placeholder={direction === "grant" ? "写进给用户的站内信" : "仅后台可见"}
                  />
                </div>
              </div>
              <div className="flex justify-end">
                <Button size="sm" disabled={!valid || adjust.isPending} onClick={() => setConfirming(true)}>
                  {direction === "grant" ? "发放" : "扣减"}
                </Button>
              </div>
            </div>
          </>
        )}
      </div>

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{direction === "grant" ? "发放抽奖次数" : "扣减抽奖次数"}</AlertDialogTitle>
            <AlertDialogDescription>
              {direction === "grant"
                ? `给 ${selected ? userLabel({ ...selected, userId: selected.id }) : ""} 发放 ${count} 次（记为${paid ? "兑换" : "赠送"}），并发站内信：${reason.trim()}`
                : `从 ${selected ? userLabel({ ...selected, userId: selected.id }) : ""} 扣减 ${count} 次，先扣赠送的。扣减不可撤回。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void submit();
              }}
              disabled={adjust.isPending}
            >
              {adjust.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  );
}

function LedgerSection({ appKey, user }: { appKey: string; user: PickedUser | null }) {
  const [kind, setKind] = useState<LotteryLedgerKind | "all">("all");
  const [origin, setOrigin] = useState<LotteryTicketOrigin | "all">("all");
  const [onlyUser, setOnlyUser] = useState(true);
  const [page, setPage] = useState(1);
  const userId = onlyUser ? user?.id : undefined;
  const query = useLotteryTicketLedgerQuery(appKey, {
    userId,
    kind: kind === "all" ? undefined : kind,
    origin: origin === "all" ? undefined : origin,
    page,
    limit: 20
  });
  const items = query.data?.items ?? [];
  return (
    <SectionCard
      icon={<ReceiptText className="size-4" />}
      title="次数流水"
      description={userId ? `只看 ${userLabel({ ...user, userId: user?.id })}` : "全部用户，新的在前"}
    >
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <ToggleGroup
            type="single"
            size="sm"
            variant="outline"
            value={kind}
            onValueChange={(value) => {
              if (!value) return;
              setKind(value as LotteryLedgerKind | "all");
              setPage(1);
            }}
          >
            <ToggleGroupItem value="all">全部</ToggleGroupItem>
            {(Object.keys(KIND_META) as LotteryLedgerKind[]).map((key) => (
              <ToggleGroupItem key={key} value={key}>
                {KIND_META[key].label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <Select
            value={origin}
            onValueChange={(value) => {
              setOrigin(value as LotteryTicketOrigin | "all");
              setPage(1);
            }}
          >
            <SelectTrigger className="h-8 w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部来源</SelectItem>
              {ORIGINS.map((key) => (
                <SelectItem key={key} value={key}>
                  {ORIGIN_LABEL[key]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {user ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setOnlyUser(!onlyUser);
                setPage(1);
              }}
            >
              {onlyUser ? "看全部用户" : "只看所选用户"}
            </Button>
          ) : null}
        </div>
        {query.isLoading ? (
          <Skeleton className="h-40 w-full rounded-xl" />
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">暂无流水</p>
        ) : (
          <div className="overflow-x-auto rounded-xl border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>时间</TableHead>
                  {userId ? null : <TableHead>用户</TableHead>}
                  <TableHead>种类</TableHead>
                  <TableHead>来源</TableHead>
                  <TableHead>说明</TableHead>
                  <TableHead className="text-right">变动</TableHead>
                  <TableHead className="text-right">余额</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="whitespace-nowrap text-[11px] text-muted-foreground">{formatStoreDate(item.createdAt)}</TableCell>
                    {userId ? null : (
                      <TableCell className="text-xs">
                        <div className="flex flex-col">
                          <span>{userLabel(item)}</span>
                          <span className="font-mono text-[10px] text-muted-foreground">UID-{item.userId}</span>
                        </div>
                      </TableCell>
                    )}
                    <TableCell>
                      <Badge variant={KIND_META[item.kind]?.variant ?? "secondary"} size="sm">
                        {item.kindLabel}
                      </Badge>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-xs">
                      <span className="mr-1.5">{item.originLabel}</span>
                      <TierBadge paid={item.paid} />
                    </TableCell>
                    <TableCell className="max-w-72 text-xs">
                      <p className="truncate">{item.title || "—"}</p>
                      {item.ref || item.operator ? (
                        <p className="truncate font-mono text-[10px] text-muted-foreground">
                          {[item.ref, item.operator].filter(Boolean).join("，")}
                        </p>
                      ) : null}
                    </TableCell>
                    <TableCell
                      className={cn(
                        "text-right text-sm font-semibold tabular-nums",
                        item.delta > 0 ? "text-emerald-600 dark:text-emerald-400" : "text-foreground"
                      )}
                    >
                      {item.delta > 0 ? `+${item.delta}` : item.delta}
                    </TableCell>
                    <TableCell className="text-right text-xs tabular-nums text-muted-foreground">
                      {formatCount(item.balanceAfter)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        <Pager page={page} totalPages={query.data?.totalPages ?? 1} onChange={setPage} />
      </div>
    </SectionCard>
  );
}
