"use client";

import { useMemo, useState } from "react";
import { Coins, Crown, Receipt, Sparkles, Wallet } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { formatVipTerm, isLifetimeGrant, vipSourceLabel } from "@/components/apps/vip/vip-shared";
import { ApiError } from "@/lib/api-client";
import {
  useAdjustUserExperienceMutation,
  useAdjustUserIntegralMutation
} from "@/lib/admin-hooks";
import {
  useAdjustAdminUserWalletMutation,
  useAdminUserVipTransactionsQuery,
  useAdminUserWalletQuery,
  useAdminUserWalletTransactionsQuery,
  useAdminVipFeaturesQuery
} from "@/lib/app-user-hooks";
import type { AdminAppUserDetail, UserWallet } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { AdjustCard, Segmented, formatIntegerString } from "./user-assets-adjust";
import {
  EMPTY,
  EmptyRow,
  Panel,
  WALLET_TXN_LABEL,
  formatMoney,
  formatShortTime,
  formatTime,
  hasWallet,
  isNegativeAmount,
  isZeroTime,
  textValue
} from "./user-detail-shared";
import { UserFeatureUnlocksPanel } from "./user-feature-unlocks-panel";
import { UserVipPanel } from "./user-vip-panel";

const POINT_QUICK = ["10", "50", "100", "500", "1000"];
const MONEY_QUICK = ["1", "5", "10", "50", "100"];
const POINT_REASONS = ["活动奖励", "客诉补偿", "签到补发", "错误更正", "违规扣除"];
const EXPERIENCE_REASONS = ["活动奖励", "客诉补偿", "签到补发", "错误更正"];
/** 与后端 pointdomain.MaxAdminAdjustAmount / MaxBalance 一致 */
const POINT_MAX_ADJUST = "100000000";
const POINT_MAX_BALANCE = "9007199254740991";
const MONEY_REASONS = ["客诉补偿", "线下充值", "退款补录", "错误更正"];

const LEDGER_PAGE_SIZE = 10;

/**
 * 资产：这个用户在平台上拥有的一切可计价的东西，以及改动它们的入口。
 *
 * 四种资产分属四条完全不同的账：
 *   积分 / 经验  —— points_service，整数，无小数
 *   钱包余额     —— wallet，decimal 字符串，带流水与幂等
 *   会员          —— vip，套餐 / 天数发放，功能权益跟随套餐当前配置
 *   功能解锁      —— 积分兑换或管理员授予的单项功能，不让人成为会员
 * 放在一页是因为管理员的问题是"这人有什么"，而不是"平台有几张表"；
 * 但调整入口各自独立，绝不做成一个统一的"调整资产"表单 —— 那会掩盖它们不同的结算语义。
 *
 * 自上而下：三张调整卡（当前值、方向、数量、调整后预览一屏看完）→ 会员 → 功能解锁 →
 * 账目记录（钱包流水与会员记录合在一处，按类型筛选、分页）。
 */
export function UserAssetsTab({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  const appId = user.appid;
  const walletQuery = useAdminUserWalletQuery(appKey, userId);
  const wallet = walletQuery.data as UserWallet | undefined;
  const walletReady = hasWallet(wallet);

  const integralMutation = useAdjustUserIntegralMutation();
  const experienceMutation = useAdjustUserExperienceMutation();
  const walletMutation = useAdjustAdminUserWalletMutation(appKey, userId);

  async function adjustPoints(kind: "integral" | "experience", signed: string, reason: string, requestId: string) {
    if (!appId) {
      toast.error("缺少应用标识");
      return false;
    }
    // 卡片已把数量限制在单次上限内，远小于 2^53，转 Number 不丢精度
    const amount = Number.parseInt(signed, 10);
    if (!Number.isSafeInteger(amount) || amount === 0) {
      toast.error("调整数量无效");
      return false;
    }
    const label = kind === "integral" ? "积分" : "经验";
    const mutation = kind === "integral" ? integralMutation : experienceMutation;
    try {
      const result = await mutation.mutateAsync({ userId, appid: appId, amount, reason: reason || undefined, requestId });
      const after = typeof result?.afterAmount === "number" ? `，当前 ${formatIntegerString(String(result.afterAmount))}` : "";
      if (result?.replayed) {
        toast.info(`该笔调整已处理过，未重复${amount > 0 ? "增加" : "扣减"}${after}`);
      } else {
        toast.success(`${label}已${amount > 0 ? "增加" : "扣减"} ${formatIntegerString(String(Math.abs(amount)))}${after}`);
      }
      return true;
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
      return false;
    }
  }

  async function adjustWallet(signed: string, reason: string) {
    try {
      const result = await walletMutation.mutateAsync({ amount: signed, reason: reason || undefined });
      toast.success(isNegativeAmount(signed) ? "余额已扣减" : "余额已增加", {
        description: `当前余额 ¥${formatMoney(result.wallet?.balance)}`
      });
      return true;
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
      return false;
    }
  }

  const numberString = (value: unknown) =>
    typeof value === "number" && Number.isFinite(value) ? String(value) : undefined;
  const formatCount = (value: string) => formatIntegerString(value);

  return (
    <div className="space-y-5">
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <AdjustCard
          title="积分"
          icon={<Coins className="size-4" />}
          unit="积分"
          current={numberString(user.integral)}
          format={formatCount}
          quick={POINT_QUICK}
          reasons={POINT_REASONS}
          shortageText="积分不足"
          maxAmount={POINT_MAX_ADJUST}
          maxBalance={POINT_MAX_BALANCE}
          pending={integralMutation.isPending}
          onSubmit={(signed, reason, requestId) => adjustPoints("integral", signed, reason, requestId)}
        />
        <AdjustCard
          title="经验"
          icon={<Sparkles className="size-4" />}
          unit="经验"
          current={numberString(user.experience)}
          format={formatCount}
          hint="经验只增不减，等级随之重算"
          quick={POINT_QUICK}
          reasons={EXPERIENCE_REASONS}
          shortageText="经验不足"
          addOnly
          maxAmount={POINT_MAX_ADJUST}
          maxBalance={POINT_MAX_BALANCE}
          pending={experienceMutation.isPending}
          onSubmit={(signed, reason, requestId) => adjustPoints("experience", signed, reason, requestId)}
        />
        <AdjustCard
          title="钱包余额"
          icon={<Wallet className="size-4" />}
          unit="元"
          decimal
          loading={walletQuery.isLoading}
          current={walletReady ? wallet?.balance : undefined}
          format={(value) => `¥${formatMoney(value)}`}
          hint={walletReady ? `冻结 ¥${formatMoney(wallet?.frozen)}` : undefined}
          unavailable={!walletQuery.isLoading && !walletReady ? "钱包数据不可用" : undefined}
          quick={MONEY_QUICK}
          reasons={MONEY_REASONS}
          shortageText="余额不足"
          pending={walletMutation.isPending}
          onSubmit={adjustWallet}
        />
      </div>

      <UserVipPanel appKey={appKey} userId={userId} />

      <UserFeatureUnlocksPanel appKey={appKey} userId={userId} />

      <LedgerPanel appKey={appKey} userId={userId} wallet={walletReady ? wallet : undefined} />
    </div>
  );
}

// ── 账目记录 ──────────────────────────

type LedgerView = "wallet" | "vip";

function LedgerPanel({
  appKey,
  userId,
  wallet
}: {
  appKey: string;
  userId: number;
  wallet?: UserWallet;
}) {
  const [view, setView] = useState<LedgerView>("wallet");
  const [walletType, setWalletType] = useState("all");
  const [walletPage, setWalletPage] = useState(1);
  const [vipPage, setVipPage] = useState(1);

  const walletTxns = useAdminUserWalletTransactionsQuery(appKey, userId, {
    type: walletType === "all" ? undefined : walletType,
    page: walletPage,
    limit: LEDGER_PAGE_SIZE
  });
  const vipTxns = useAdminUserVipTransactionsQuery(appKey, userId, {
    page: vipPage,
    limit: LEDGER_PAGE_SIZE
  });

  const vipTotalPages = Math.max(1, Math.ceil((vipTxns.data?.total ?? 0) / LEDGER_PAGE_SIZE));

  return (
    <Panel
      title="账目记录"
      icon={<Receipt className="size-4" />}
      action={
        <Segmented
          value={view}
          onChange={setView}
          ariaLabel="账目类型"
          options={[
            {
              value: "wallet",
              label: "钱包流水",
              icon: Wallet,
              count: walletType === "all" ? walletTxns.data?.total : undefined
            },
            { value: "vip", label: "会员记录", icon: Crown, count: vipTxns.data?.total }
          ]}
        />
      }
      bodyClassName="p-0 sm:px-0"
    >
      {view === "wallet" ? (
        <>
          <div className="flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5">
            <dl className="flex flex-wrap gap-x-6 gap-y-1 text-xs">
              <LedgerFact label="累计充值" value={wallet ? `¥${formatMoney(wallet.totalRecharged)}` : EMPTY} />
              <LedgerFact label="累计消费" value={wallet ? `¥${formatMoney(wallet.totalConsumed)}` : EMPTY} />
              <LedgerFact
                label="开户时间"
                value={wallet && !isZeroTime(wallet.createdAt) ? formatTime(wallet.createdAt) : "未开户"}
              />
            </dl>
            <Select
              value={walletType}
              onValueChange={(value) => {
                setWalletType(value);
                setWalletPage(1);
              }}
            >
              <SelectTrigger size="sm" className="h-8 w-full text-xs sm:w-36" aria-label="流水类型">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all" className="text-xs">
                  全部类型
                </SelectItem>
                {Object.entries(WALLET_TXN_LABEL).map(([value, label]) => (
                  <SelectItem key={value} value={value} className="text-xs">
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <WalletLedger
            loading={walletTxns.isLoading}
            items={walletTxns.data?.items ?? []}
            emptyText={walletType === "all" ? "暂无流水记录" : "没有这一类型的流水"}
          />
          <Pager
            page={walletPage}
            totalPages={walletTxns.data?.totalPages ?? 1}
            total={walletTxns.data?.total ?? 0}
            busy={walletTxns.isFetching}
            onChange={setWalletPage}
          />
        </>
      ) : (
        <>
          <VipLedger appKey={appKey} loading={vipTxns.isLoading} items={vipTxns.data?.items ?? []} />
          <Pager
            page={vipPage}
            totalPages={vipTotalPages}
            total={vipTxns.data?.total ?? 0}
            busy={vipTxns.isFetching}
            onChange={setVipPage}
          />
        </>
      )}
    </Panel>
  );
}

function LedgerFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium tabular-nums text-foreground">{value}</dd>
    </div>
  );
}

function LedgerSkeleton() {
  return (
    <div className="space-y-2 px-4 py-4 sm:px-5">
      {[0, 1, 2, 3].map((i) => (
        <Skeleton key={i} className="h-9 w-full rounded-lg" />
      ))}
    </div>
  );
}

function WalletLedger({
  loading,
  items,
  emptyText
}: {
  loading: boolean;
  items: NonNullable<ReturnType<typeof useAdminUserWalletTransactionsQuery>["data"]>["items"];
  emptyText: string;
}) {
  if (loading) return <LedgerSkeleton />;
  if (!items?.length) return <EmptyRow text={emptyText} />;
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-4 sm:pl-5">时间</TableHead>
            <TableHead>类型</TableHead>
            <TableHead>说明</TableHead>
            <TableHead className="text-right">金额</TableHead>
            <TableHead className="text-right">余额</TableHead>
            <TableHead className="pr-4 sm:pr-5">操作人</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((txn) => {
            const negative = isNegativeAmount(txn.amount);
            return (
              <TableRow key={txn.id}>
                <TableCell
                  className="whitespace-nowrap pl-4 text-xs tabular-nums text-muted-foreground sm:pl-5"
                  title={formatTime(txn.createdAt)}
                >
                  {formatShortTime(txn.createdAt)}
                </TableCell>
                <TableCell>
                  <Badge variant="outline" size="sm">
                    {WALLET_TXN_LABEL[txn.type] ?? txn.type}
                  </Badge>
                </TableCell>
                <TableCell className="max-w-[260px]">
                  <div className="truncate text-xs" title={txn.title}>
                    {textValue(txn.title)}
                  </div>
                  {txn.remark || txn.relatedOrderNo ? (
                    <div className="truncate text-[11px] text-muted-foreground">
                      {[txn.remark, txn.relatedOrderNo].filter(Boolean).join("，")}
                    </div>
                  ) : null}
                </TableCell>
                <TableCell
                  className={cn(
                    "text-right text-xs font-medium tabular-nums",
                    negative ? "text-red-600 dark:text-red-400" : "text-emerald-600 dark:text-emerald-400"
                  )}
                >
                  {negative ? "" : "+"}
                  {formatMoney(txn.amount)}
                </TableCell>
                <TableCell className="text-right text-xs tabular-nums text-muted-foreground">
                  {formatMoney(txn.balanceAfter)}
                </TableCell>
                <TableCell className="pr-4 text-xs text-muted-foreground sm:pr-5">
                  {textValue(txn.operator)}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

function VipLedger({
  appKey,
  loading,
  items
}: {
  appKey: string;
  loading: boolean;
  items: NonNullable<ReturnType<typeof useAdminUserVipTransactionsQuery>["data"]>["items"];
}) {
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const featureName = useMemo(() => {
    const map = new Map<string, string>();
    for (const feature of featuresQuery.data ?? []) map.set(feature.tag, feature.name);
    return (tag: string) => map.get(tag) || tag;
  }, [featuresQuery.data]);

  if (loading) return <LedgerSkeleton />;
  if (!items?.length) return <EmptyRow text="暂无会员记录" />;
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="pl-4 sm:pl-5">时间</TableHead>
            <TableHead>套餐</TableHead>
            <TableHead className="text-right">时长</TableHead>
            <TableHead>渠道</TableHead>
            <TableHead className="text-right">金额</TableHead>
            <TableHead className="pr-4 sm:pr-5">到期变化</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((txn) => (
            <TableRow key={txn.id} className={cn(txn.revokedAt && "text-muted-foreground")}>
              <TableCell
                className="whitespace-nowrap pl-4 text-xs tabular-nums text-muted-foreground sm:pl-5"
                title={formatTime(txn.createdAt)}
              >
                {formatShortTime(txn.createdAt)}
              </TableCell>
              <TableCell className="max-w-[220px]">
                <div className="truncate text-xs">{textValue(txn.planName)}</div>
                {txn.features?.length ? (
                  <div className="truncate text-[11px] text-muted-foreground">
                    {txn.features.map(featureName).join("、")}
                  </div>
                ) : null}
              </TableCell>
              <TableCell
                className={cn(
                  "text-right text-xs font-medium tabular-nums",
                  txn.durationDays < 0 || txn.payChannel === "admin_revoke"
                    ? "text-red-600 dark:text-red-400"
                    : "text-foreground"
                )}
              >
                {formatVipTerm(txn)}
              </TableCell>
              <TableCell>
                <div className="flex items-center gap-1">
                  <Badge variant="outline" size="sm">
                    {vipSourceLabel(txn.payChannel)}
                  </Badge>
                  {txn.revokedAt ? (
                    <Badge variant="danger" size="sm" title={txn.revokeReason || undefined}>
                      已作废
                    </Badge>
                  ) : null}
                </div>
              </TableCell>
              <TableCell className="text-right text-xs tabular-nums">{formatMoney(txn.payAmount)}</TableCell>
              <TableCell className="whitespace-nowrap pr-4 text-[11px] tabular-nums text-muted-foreground sm:pr-5">
                {/* 永久开通与取消永久会员都不动限时那条线的到期时间 */}
                {isLifetimeGrant(txn) ? (
                  "永久"
                ) : txn.lifetime ? (
                  EMPTY
                ) : (
                  <>
                    {txn.expireBefore ? `${formatShortTime(txn.expireBefore)} → ` : "开通 → "}
                    {formatShortTime(txn.expireAfter)}
                  </>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function Pager({
  page,
  totalPages,
  total,
  busy,
  onChange
}: {
  page: number;
  totalPages: number;
  total: number;
  busy: boolean;
  onChange: (page: number) => void;
}) {
  if (totalPages <= 1) return null;
  return (
    <div className="flex items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground sm:px-5">
      <span className="tabular-nums">
        第 {page} / {totalPages} 页，共 {total} 条
      </span>
      <div className="flex gap-2">
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs"
          disabled={page <= 1 || busy}
          onClick={() => onChange(page - 1)}
        >
          上一页
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs"
          disabled={page >= totalPages || busy}
          onClick={() => onChange(page + 1)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}
