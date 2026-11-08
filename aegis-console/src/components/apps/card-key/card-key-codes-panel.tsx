"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Ban, Copy, Loader2, PackageCheck, PackageX, RotateCcw, Ticket, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";
import { CardKeyDetailSheet } from "@/components/apps/card-key/card-key-detail-sheet";
import {
  CardKindBadge,
  CardStateBadge,
  CopyButton,
  EmptyHint,
  copyText,
  formatCardDate,
  formatRemaining
} from "@/components/apps/card-key/card-key-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import type { CardKey, CardKeyKind, CardKeyStatusFilter } from "@/lib/api/card-key";
import {
  useCardKeyBatchesQuery,
  useCardKeysQuery,
  useDeleteCardKeysMutation,
  useDisableCardKeysMutation,
  useDistributeCardKeysMutation,
  useRestoreCardKeysMutation
} from "@/lib/card-key-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

const LIMIT = 50;

const STATUS_OPTIONS: Array<{ value: CardKeyStatusFilter; label: string }> = [
  { value: "unused", label: "未使用" },
  { value: "partial", label: "部分使用" },
  { value: "active", label: "使用中" },
  { value: "used", label: "已核销" },
  { value: "expired", label: "已过期" },
  { value: "disabled", label: "已作废" }
];

type BulkAction = "disable" | "distribute" | "delete";

/**
 * 单卡列表。
 *
 * 批量操作按**选中的 id 列表**下发，不做「按筛选条件批量」：管理员看到的列表与实际执行
 * 之间存在时间差（翻页期间有人兑换），按条件批量会误伤没被看过的卡。翻页或改筛选即清空选择。
 * 点一行打开单卡详情：调整到期时间与设备数、解绑设备、看核销与解绑流水。
 */
export function CardKeyCodesPanel({
  appKey,
  batchId,
  onBatchIdChange
}: {
  appKey: string;
  batchId?: number;
  onBatchIdChange: (batchId?: number) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState<CardKeyStatusFilter | undefined>(undefined);
  const [kind, setKind] = useState<CardKeyKind | undefined>(undefined);
  const [distributed, setDistributed] = useState<"yes" | "no" | undefined>(undefined);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [openCardId, setOpenCardId] = useState<number | null>(null);
  const [action, setAction] = useState<BulkAction | null>(null);
  const [note, setNote] = useState("");

  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const batchesQuery = useCardKeyBatchesQuery(appKey);
  const listQuery = useCardKeysQuery(appKey, {
    batchId,
    status,
    kind,
    distributed,
    keyword: debouncedKeyword || undefined,
    page,
    limit: LIMIT
  });
  const disableMutation = useDisableCardKeysMutation(appKey);
  const restoreMutation = useRestoreCardKeysMutation(appKey);
  const deleteMutation = useDeleteCardKeysMutation(appKey);
  const distributeMutation = useDistributeCardKeysMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / LIMIT));
  const filtered = Boolean(keyword || batchId || status || kind || distributed);
  const selectedCards = items.filter((card) => selected.has(card.id));
  const allSelected = items.length > 0 && items.every((card) => selected.has(card.id));

  const resetSelection = () => setSelected(new Set());
  const withReset = <T,>(apply: (value: T) => void) => (value: T) => {
    apply(value);
    setPage(1);
    resetSelection();
  };

  const toggle = (id: number, checked: boolean) => {
    const next = new Set(selected);
    if (checked) next.add(id);
    else next.delete(id);
    setSelected(next);
  };

  const toggleAll = (checked: boolean) => setSelected(checked ? new Set(items.map((card) => card.id)) : new Set());

  const ids = () => Array.from(selected);

  const run = async (task: () => Promise<string>) => {
    try {
      toast.success(await task());
      resetSelection();
      setAction(null);
      setNote("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const confirmAction = () => {
    if (action === "disable") {
      return run(async () => {
        const result = await disableMutation.mutateAsync({ ids: ids(), reason: note.trim() });
        return skippedText(`已作废 ${result.affected} 张`, selected.size - result.affected, "已核销或已作废的卡不受影响");
      });
    }
    if (action === "distribute") {
      return run(async () => {
        const result = await distributeMutation.mutateAsync({ ids: ids(), distributed: true, note: note.trim() });
        return `已标记 ${result.affected} 张为已提取`;
      });
    }
    if (action === "delete") {
      return run(async () => {
        const result = await deleteMutation.mutateAsync(ids());
        return skippedText(`已删除 ${result.affected} 张`, result.skipped, "被使用过的卡只能作废");
      });
    }
  };

  const restore = () =>
    run(async () => {
      const result = await restoreMutation.mutateAsync(ids());
      return skippedText(`已恢复 ${result.affected} 张`, selected.size - result.affected, "只有已作废的卡能恢复");
    });

  const undistribute = () =>
    run(async () => {
      const result = await distributeMutation.mutateAsync({ ids: ids(), distributed: false });
      return `已取消 ${result.affected} 张的提取标记`;
    });

  const busy =
    disableMutation.isPending || restoreMutation.isPending || deleteMutation.isPending || distributeMutation.isPending;

  return (
    <SectionCard icon={<Ticket className="size-4" />} title="卡密" description="点击一行查看详情与调整">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => withReset(setKeyword)(event.target.value)}
          placeholder="搜索卡面、账号或备注"
          className="h-8 w-56"
        />
        <Select
          value={batchId ? String(batchId) : "all"}
          onValueChange={withReset((value: string) => onBatchIdChange(value === "all" ? undefined : Number(value)))}
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
          value={status ?? "all"}
          onValueChange={withReset((value: string) => setStatus(value === "all" ? undefined : (value as CardKeyStatusFilter)))}
        >
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {STATUS_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={kind ?? "all"}
          onValueChange={withReset((value: string) => setKind(value === "all" ? undefined : (value as CardKeyKind)))}
        >
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部类型</SelectItem>
            <SelectItem value="redeem">兑换卡</SelectItem>
            <SelectItem value="login">授权卡</SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={distributed ?? "all"}
          onValueChange={withReset((value: string) => setDistributed(value === "all" ? undefined : (value as "yes" | "no")))}
        >
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">提取不限</SelectItem>
            <SelectItem value="no">未提取</SelectItem>
            <SelectItem value="yes">已提取</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {selected.size > 0 ? (
        <div className="mb-3 flex flex-wrap items-center gap-1.5 rounded-xl bg-muted/60 px-3 py-2">
          <span className="mr-1 text-xs font-medium tabular-nums">已选 {selected.size} 张</span>
          <Button
            size="sm"
            variant="ghost"
            className="h-7"
            onClick={() => void copyText(selectedCards.map((card) => card.code).join("\n"), `已复制 ${selectedCards.length} 张卡面`)}
          >
            <Copy className="size-3.5" /> 复制卡面
          </Button>
          <Button size="sm" variant="ghost" className="h-7" disabled={busy} onClick={() => setAction("distribute")}>
            <PackageCheck className="size-3.5" /> 标记已提取
          </Button>
          <Button size="sm" variant="ghost" className="h-7" disabled={busy} onClick={undistribute}>
            <PackageX className="size-3.5" /> 取消提取
          </Button>
          <Button size="sm" variant="ghost" className="h-7" disabled={busy} onClick={() => setAction("disable")}>
            <Ban className="size-3.5" /> 作废
          </Button>
          <Button size="sm" variant="ghost" className="h-7" disabled={busy} onClick={restore}>
            <RotateCcw className="size-3.5" /> 恢复
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 text-destructive hover:text-destructive"
            disabled={busy}
            onClick={() => setAction("delete")}
          >
            <Trash2 className="size-3.5" /> 删除
          </Button>
          <Button size="icon" variant="ghost" className="ml-auto size-7" onClick={resetSelection} aria-label="清空选择">
            <X className="size-3.5" />
          </Button>
        </div>
      ) : null}

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-11 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyHint>{filtered ? "暂无匹配的卡密" : "暂无卡密"}</EmptyHint>
      ) : (
        <div className={cn("overflow-x-auto", listQuery.isFetching && "opacity-70 transition-opacity")}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8">
                  <Checkbox checked={allSelected} onCheckedChange={(value) => toggleAll(Boolean(value))} aria-label="全选本页" />
                </TableHead>
                <TableHead>卡面</TableHead>
                <TableHead>批次</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>使用</TableHead>
                <TableHead>到期</TableHead>
                <TableHead>提取</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((card) => (
                <CardRow
                  key={card.id}
                  appKey={appKey}
                  card={card}
                  checked={selected.has(card.id)}
                  onCheck={(checked) => toggle(card.id, checked)}
                  onOpen={() => setOpenCardId(card.id)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <AdPolicyPager
        total={total}
        page={page}
        totalPages={totalPages}
        onPage={(next) => {
          setPage(next);
          resetSelection();
        }}
      />

      <CardKeyDetailSheet appKey={appKey} cardId={openCardId} onClose={() => setOpenCardId(null)} />

      <Dialog open={Boolean(action)} onOpenChange={(open) => !open && setAction(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {action === "disable" ? "作废卡密" : action === "distribute" ? "标记为已提取" : "删除卡密"}
            </DialogTitle>
            <DialogDescription>
              {action === "disable"
                ? `作废选中的 ${selected.size} 张卡。已核销的卡不受影响，作废后可以恢复。`
                : action === "distribute"
                  ? `选中的 ${selected.size} 张卡标记为已交给渠道，之后提卡不会再取到它们。`
                  : `删除选中的 ${selected.size} 张卡，无法恢复。被使用过的卡会被跳过，只能作废。`}
            </DialogDescription>
          </DialogHeader>
          {action !== "delete" ? (
            <Input
              value={note}
              maxLength={action === "disable" ? 256 : 128}
              onChange={(event) => setNote(event.target.value)}
              placeholder={action === "disable" ? "作废原因（可选）" : "交给谁（可选）"}
            />
          ) : null}
          <DialogFooter>
            <Button variant="ghost" size="sm" onClick={() => setAction(null)}>
              取消
            </Button>
            <Button
              size="sm"
              variant={action === "distribute" ? "default" : "destructive"}
              onClick={confirmAction}
              disabled={busy}
            >
              {busy ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SectionCard>
  );
}

function skippedText(done: string, skipped: number, why: string) {
  return skipped > 0 ? `${done}，跳过 ${skipped} 张（${why}）` : done;
}

function CardRow({
  appKey,
  card,
  checked,
  onCheck,
  onOpen
}: {
  appKey: string;
  card: CardKey;
  checked: boolean;
  onCheck: (checked: boolean) => void;
  onOpen: () => void;
}) {
  const multi = card.maxUses > 1;
  return (
    <TableRow className="cursor-pointer" data-state={checked ? "selected" : undefined} onClick={onOpen}>
      <TableCell onClick={(event) => event.stopPropagation()}>
        <Checkbox checked={checked} onCheckedChange={(value) => onCheck(Boolean(value))} aria-label="选择" />
      </TableCell>
      <TableCell className="max-w-72">
        <div className="flex items-center gap-1">
          <span className="truncate font-mono text-xs font-medium">{card.code}</span>
          <CopyButton text={card.code} label="复制卡面" />
        </div>
        {card.remark ? <p className="truncate text-[11px] text-muted-foreground">{card.remark}</p> : null}
      </TableCell>
      <TableCell className="max-w-44">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-xs">{card.batchName || `#${card.batchId}`}</span>
          <CardKindBadge kind={card.kind} />
        </div>
      </TableCell>
      <TableCell>
        <CardStateBadge card={card} />
        {card.disabledReason ? (
          <p className="mt-0.5 max-w-40 truncate text-[11px] text-muted-foreground">{card.disabledReason}</p>
        ) : null}
      </TableCell>
      <TableCell className="text-xs">
        {card.boundUserId ? (
          <Link
            href={`/app-users/${encodeURIComponent(appKey)}/${card.boundUserId}`}
            className="font-medium hover:underline"
            onClick={(event) => event.stopPropagation()}
          >
            {card.boundAccount || `#${card.boundUserId}`}
          </Link>
        ) : multi ? (
          <span className="tabular-nums">
            {card.useCount}/{card.maxUses} 人次
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
        {card.kind === "login" ? (
          <p className="mt-0.5 text-[11px] tabular-nums text-muted-foreground">
            设备 {card.deviceCount}/{card.maxDevices}
          </p>
        ) : null}
      </TableCell>
      <TableCell className="whitespace-nowrap text-xs">
        {card.expiresAt ? (
          <>
            <span>{formatRemaining(card.expiresAt)}</span>
            <p className="mt-0.5 text-[11px] text-muted-foreground">{formatCardDate(card.expiresAt)}</p>
          </>
        ) : (
          <span className="text-muted-foreground">{card.kind === "login" && card.status === "unused" ? "未激活" : "永久"}</span>
        )}
      </TableCell>
      <TableCell className="text-xs">
        {card.distributedAt ? (
          <Badge variant="outline" size="sm" className="font-normal" title={formatCardDate(card.distributedAt)}>
            {card.distributionNote || "已提取"}
          </Badge>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </TableCell>
    </TableRow>
  );
}
