"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { CheckCircle2, Gift, Loader2, ReceiptText } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";
import {
  CardKindBadge,
  CopyButton,
  EmptyHint,
  ResultSummary,
  SOURCE_LABEL,
  formatCardDate
} from "@/components/apps/card-key/card-key-shared";
import { CommerceRangePicker, type CommerceRange } from "@/components/commerce/commerce-range-picker";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
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
import { ApiError } from "@/lib/api/client";
import type { CardKeyRedeemResult, CardKeySource } from "@/lib/api/card-key";
import {
  useAdminRedeemCardKeyMutation,
  useCardKeyBatchesQuery,
  useCardKeyRedemptionsQuery
} from "@/lib/card-key-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;
const SOURCES: CardKeySource[] = ["redeem", "login", "admin"];

/**
 * 核销记录。
 *
 * 展示的是 `results`（实际发成了什么）而不是卡上配的 `rewards`：两者绝大多数时候一致，
 * 但排障要的恰恰是不一致的那些情况（批次权益后来改过、余额发到了哪笔流水）。
 * 「代兑」替一个用户核销一张卡（客服补发），来源记为管理员代兑并给用户发站内信。
 */
export function CardKeyRedemptionsPanel({
  appKey,
  batchId,
  onBatchIdChange
}: {
  appKey: string;
  batchId?: number;
  onBatchIdChange: (batchId?: number) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [source, setSource] = useState<CardKeySource | undefined>(undefined);
  const [user, setUser] = useState<PickedUser | null>(null);
  const [range, setRange] = useState<CommerceRange>({});
  const [page, setPage] = useState(1);
  const [redeemOpen, setRedeemOpen] = useState(false);

  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const batchesQuery = useCardKeyBatchesQuery(appKey);
  const listQuery = useCardKeyRedemptionsQuery(appKey, {
    batchId,
    userId: user?.id,
    source,
    keyword: debouncedKeyword || undefined,
    from: range.start,
    to: range.end,
    page,
    limit: LIMIT
  });

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / LIMIT));
  const filtered = Boolean(keyword || batchId || source || user || range.start || range.end);
  const userHref = (userId: number) => `/app-users/${encodeURIComponent(appKey)}/${userId}`;

  return (
    <SectionCard
      icon={<ReceiptText className="size-4" />}
      title="核销记录"
      description="每一次兑换与授权卡激活实际发放了什么"
      aside={
        <Button size="sm" variant="outline" onClick={() => setRedeemOpen(true)}>
          <Gift className="size-3.5" /> 代兑
        </Button>
      }
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索卡面或账号"
          className="h-8 w-52"
        />
        <Select
          value={batchId ? String(batchId) : "all"}
          onValueChange={(value) => {
            onBatchIdChange(value === "all" ? undefined : Number(value));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-44">
            <SelectValue placeholder="全部批次" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部批次</SelectItem>
            {(batchesQuery.data ?? []).map((batch) => (
              <SelectItem key={batch.id} value={String(batch.id)}>
                {batch.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={source ?? "all"}
          onValueChange={(value) => {
            setSource(value === "all" ? undefined : (value as CardKeySource));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部来源</SelectItem>
            {SOURCES.map((item) => (
              <SelectItem key={item} value={item}>
                {SOURCE_LABEL[item]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="w-52">
          <UserPicker
            appKey={appKey}
            value={user}
            onChange={(next) => {
              setUser(next);
              setPage(1);
            }}
            placeholder="全部用户"
          />
        </div>
        <CommerceRangePicker
          value={range}
          onChange={(next) => {
            setRange(next);
            setPage(1);
          }}
        />
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-11 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyHint>{filtered ? "暂无匹配的记录" : "暂无核销记录"}</EmptyHint>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>卡面</TableHead>
                <TableHead>账号</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>实际发放</TableHead>
                <TableHead className="text-right">时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="max-w-64">
                    <div className="flex items-center gap-1">
                      <span className="truncate font-mono text-xs font-medium">{item.code}</span>
                      <CopyButton text={item.code} label="复制卡面" />
                    </div>
                    <div className="mt-0.5 flex items-center gap-1.5">
                      <button
                        type="button"
                        className="truncate text-left text-[11px] text-muted-foreground hover:underline"
                        onClick={() => {
                          if (item.batchId) onBatchIdChange(item.batchId);
                          setPage(1);
                        }}
                      >
                        {item.batchName || (item.batchId ? `#${item.batchId}` : "批次已删除")}
                      </button>
                      {item.kind ? <CardKindBadge kind={item.kind} /> : null}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">
                    <Link href={userHref(item.userId)} className="font-medium hover:underline">
                      {item.account || `#${item.userId}`}
                    </Link>
                    {item.nickname ? <p className="mt-0.5 text-[11px] text-muted-foreground">{item.nickname}</p> : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs">
                    <Badge variant={item.source === "admin" ? "warning" : item.source === "login" ? "info" : "secondary"} size="sm">
                      {SOURCE_LABEL[item.source] ?? item.source}
                    </Badge>
                    {item.operator ? <p className="mt-0.5 text-[11px] text-muted-foreground">{item.operator}</p> : null}
                  </TableCell>
                  <TableCell className="max-w-80">
                    <ResultSummary results={item.results} />
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-right text-xs text-muted-foreground">
                    {formatCardDate(item.createdAt)}
                    {item.clientIp ? <p className="mt-0.5 font-mono text-[11px]">{item.clientIp}</p> : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <AdPolicyPager total={total} page={page} totalPages={totalPages} onPage={setPage} />

      <AdminRedeemDialog appKey={appKey} open={redeemOpen} onClose={() => setRedeemOpen(false)} />
    </SectionCard>
  );
}

/** 代兑：替用户核销一张卡。与用户自己兑换走同一套判据（每人一次、限兑、领取对象、生效时间） */
function AdminRedeemDialog({ appKey, open, onClose }: { appKey: string; open: boolean; onClose: () => void }) {
  const [code, setCode] = useState("");
  const [user, setUser] = useState<PickedUser | null>(null);
  const [result, setResult] = useState<CardKeyRedeemResult | null>(null);
  const mutation = useAdminRedeemCardKeyMutation(appKey);

  const close = () => {
    setCode("");
    setUser(null);
    setResult(null);
    onClose();
  };

  const submit = async () => {
    if (!code.trim()) {
      toast.error("请输入卡密");
      return;
    }
    if (!user) {
      toast.error("请选择要代兑的用户");
      return;
    }
    try {
      setResult(await mutation.mutateAsync({ code: code.trim(), userId: user.id }));
      toast.success("兑换成功");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "兑换失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{result ? "兑换成功" : "代兑卡密"}</DialogTitle>
          <DialogDescription>
            {result
              ? `已为 ${user?.account || user?.nickname || `#${user?.id}`} 兑换 ${result.code}，并发送站内信`
              : "替用户核销一张卡，适用于客服补发；与用户自己兑换适用同一套规则"}
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="space-y-2 rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 dark:border-emerald-800/70 dark:bg-emerald-950/40">
            <p className="flex items-center gap-1.5 text-xs font-medium text-emerald-700 dark:text-emerald-300">
              <CheckCircle2 className="size-3.5" />
              {result.batchName || "实际发放"}
            </p>
            <ResultSummary results={result.results} />
            {result.expiresAt ? (
              <p className="text-[11px] text-muted-foreground">授权至 {formatCardDate(result.expiresAt)}</p>
            ) : null}
          </div>
        ) : (
          <div className="space-y-4">
            <div className="space-y-1.5">
              <span className="text-[11px] font-medium">卡密</span>
              <Input
                value={code}
                onChange={(event) => setCode(event.target.value)}
                placeholder="大小写与分隔符不限"
                className="font-mono"
              />
            </div>
            <div className="space-y-1.5">
              <span className="text-[11px] font-medium">用户</span>
              <UserPicker appKey={appKey} value={user} onChange={setUser} placeholder="搜索账号、昵称或邮箱" />
            </div>
          </div>
        )}

        <DialogFooter>
          {result ? (
            <Button size="sm" onClick={close}>
              完成
            </Button>
          ) : (
            <>
              <Button variant="ghost" size="sm" onClick={close}>
                取消
              </Button>
              <Button size="sm" onClick={submit} disabled={mutation.isPending}>
                {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Gift className="size-3.5" />}
                兑换
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
