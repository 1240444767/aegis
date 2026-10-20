"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Loader2, PackageCheck, ReceiptText } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";
import { LIMITS } from "@/components/apps/lottery/lottery-activity-form";
import {
  ConversionBadge,
  DRAW_STATUSES,
  DrawStatusBadge,
  SOURCE_LABEL,
  drawStatusLabel,
  formatCount,
  kindLabelOf
} from "@/components/apps/lottery/lottery-shared";
import { formatStoreDate } from "@/components/apps/points-store/points-store-shared";
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
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { LotteryDraw, LotteryDrawStatus, LotterySource } from "@/lib/api/lottery";
import {
  useFulfillLotteryDrawMutation,
  useLotteryActivitiesQuery,
  useLotteryCatalogQuery,
  useLotteryDrawsQuery
} from "@/lib/lottery-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;
const SOURCES: LotterySource[] = ["free", "bonus", "points"];

/**
 * 抽奖记录。
 *
 * 奖品列是抽中那一刻的快照（奖品后来被改、被删都不影响），「实际发放」是 `results`：
 * 两者的差别正是排障要看的（例如限量奖恰好被抢光时改发了兜底奖）。
 * 人工发放的奖品在这里「标记已发放」，写给用户的话进站内信。只有待发放的能处理。
 */
export function LotteryDrawsPanel({
  appKey,
  activityId,
  status,
  onActivityIdChange,
  onStatusChange
}: {
  appKey: string;
  activityId?: number;
  status?: LotteryDrawStatus;
  onActivityIdChange: (activityId?: number) => void;
  onStatusChange: (status?: LotteryDrawStatus) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [source, setSource] = useState<LotterySource | undefined>(undefined);
  const [winning, setWinning] = useState(false);
  const [user, setUser] = useState<PickedUser | null>(null);
  const [range, setRange] = useState<CommerceRange>({});
  const [page, setPage] = useState(1);
  const [handling, setHandling] = useState<LotteryDraw | null>(null);
  const [note, setNote] = useState("");

  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const activitiesQuery = useLotteryActivitiesQuery(appKey);
  const catalogQuery = useLotteryCatalogQuery(appKey);
  const listQuery = useLotteryDrawsQuery(appKey, {
    activityId,
    userId: user?.id,
    status,
    source,
    winning: winning || undefined,
    keyword: debouncedKeyword || undefined,
    start: range.start,
    end: range.end,
    page,
    limit: LIMIT
  });
  const fulfillMutation = useFulfillLotteryDrawMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? 1);
  const filtered = Boolean(keyword || activityId || status || source || winning || user || range.start || range.end);
  const kinds = catalogQuery.data?.kinds ?? [];

  const userHref = (userId: number) => `/app-users/${encodeURIComponent(appKey)}/${userId}?tab=assets`;

  const openFulfill = (draw: LotteryDraw) => {
    setNote("");
    setHandling(draw);
  };

  const submitFulfill = async () => {
    if (!handling) return;
    try {
      await fulfillMutation.mutateAsync({ drawId: handling.id, note: note.trim() });
      toast.success("已标记为已发放");
      setHandling(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <SectionCard icon={<ReceiptText className="size-4" />} title="抽奖记录">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索账号、单号或奖品"
          className="h-8 w-56"
        />
        <Select
          value={activityId ? String(activityId) : "all"}
          onValueChange={(value) => {
            onActivityIdChange(value === "all" ? undefined : Number(value));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-44">
            <SelectValue placeholder="全部活动" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部活动</SelectItem>
            {(activitiesQuery.data ?? []).map((activity) => (
              <SelectItem key={activity.id} value={String(activity.id)}>
                {activity.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            onStatusChange(value === "all" ? undefined : (value as LotteryDrawStatus));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {DRAW_STATUSES.map((item) => (
              <SelectItem key={item} value={item}>
                {drawStatusLabel(item)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={source ?? "all"}
          onValueChange={(value) => {
            setSource(value === "all" ? undefined : (value as LotterySource));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28">
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
        {/* UserPicker 的触发器是 w-full，套一层定宽容器 */}
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
        <label className="flex items-center gap-2 text-xs font-medium">
          <Switch
            checked={winning}
            onCheckedChange={(next) => {
              setWinning(next);
              setPage(1);
            }}
            aria-label="只看中奖"
          />
          只看中奖
        </label>
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配记录" : "暂无抽奖记录"}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>奖品</TableHead>
                <TableHead>账号</TableHead>
                <TableHead>活动</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>实际发放</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">时间</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((draw) => (
                <TableRow key={draw.id}>
                  <TableCell className="max-w-56">
                    <div className="flex items-center gap-1.5">
                      <span className={draw.prizeKind === "none" ? "truncate text-xs text-muted-foreground" : "truncate text-xs font-medium"}>
                        {draw.prizeName}
                      </span>
                      {draw.pity ? (
                        <Badge variant="info" size="sm" className="shrink-0">
                          保底
                        </Badge>
                      ) : null}
                      {draw.conversion ? <ConversionBadge /> : null}
                    </div>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">{kindLabelOf(draw.prizeKind, kinds)}</p>
                    <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">{draw.drawNo}</p>
                  </TableCell>
                  <TableCell className="text-xs">
                    <Link href={userHref(draw.userId)} className="font-medium hover:underline">
                      {draw.account || `#${draw.userId}`}
                    </Link>
                    {draw.nickname ? <p className="mt-0.5 text-[11px] text-muted-foreground">{draw.nickname}</p> : null}
                  </TableCell>
                  <TableCell className="max-w-40">
                    <button
                      type="button"
                      className="truncate text-left text-xs hover:underline"
                      onClick={() => {
                        onActivityIdChange(draw.activityId);
                        setPage(1);
                      }}
                    >
                      {draw.activityName || `#${draw.activityId}`}
                    </button>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs">
                    {SOURCE_LABEL[draw.source] ?? draw.source}
                    {draw.pointsCost ? (
                      <p className="mt-0.5 text-[11px] tabular-nums text-muted-foreground">
                        {formatCount(draw.pointsCost)} 积分
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="max-w-72">
                    {draw.results?.length ? (
                      <div className="flex flex-wrap gap-1">
                        {draw.results.map((result, index) => (
                          <Badge
                            key={`${result.type}-${index}`}
                            variant={result.pending ? "warning" : "outline"}
                            size="sm"
                            className="font-normal"
                            title={result.transactionNo ? `流水 ${result.transactionNo}` : undefined}
                          >
                            {result.detail || result.label}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-xs text-muted-foreground">—</span>
                    )}
                    {draw.conversion ? (
                      <p className="mt-1 text-[11px] text-muted-foreground" title={draw.conversion.message}>
                        {draw.conversion.reason === "feature" ? "已永久拥有该功能" : "永久会员已包含"}，改发 {draw.conversion.label}
                      </p>
                    ) : null}
                    {draw.note ? <p className="mt-1 text-[11px] text-muted-foreground">{draw.note}</p> : null}
                  </TableCell>
                  <TableCell>
                    <DrawStatusBadge status={draw.status} />
                    {draw.handledBy ? <p className="mt-0.5 text-[11px] text-muted-foreground">{draw.handledBy}</p> : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-right text-xs text-muted-foreground">
                    {formatStoreDate(draw.createdAt)}
                    {draw.clientIp ? <p className="mt-0.5 font-mono text-[11px]">{draw.clientIp}</p> : null}
                  </TableCell>
                  <TableCell>
                    {draw.status === "pending" ? (
                      <Button size="sm" variant="outline" className="h-7" onClick={() => openFulfill(draw)}>
                        <PackageCheck className="size-3.5" /> 标记已发放
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <AdPolicyPager total={total} page={page} totalPages={totalPages} onPage={setPage} />

      <Dialog open={Boolean(handling)} onOpenChange={(open) => !open && setHandling(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>标记为已发放</DialogTitle>
            <DialogDescription>
              {handling ? `${handling.account || `#${handling.userId}`}，${handling.prizeName}` : null}
            </DialogDescription>
          </DialogHeader>
          <Textarea
            value={note}
            rows={3}
            maxLength={LIMITS.note}
            onChange={(event) => setNote(event.target.value)}
            placeholder="兑换码、快递单号"
          />
          <DialogFooter>
            <Button variant="ghost" size="sm" onClick={() => setHandling(null)}>
              返回
            </Button>
            <Button size="sm" onClick={submitFulfill} disabled={fulfillMutation.isPending}>
              {fulfillMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认发放
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SectionCard>
  );
}
