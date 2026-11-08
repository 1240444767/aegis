"use client";

import { useMemo, useState } from "react";
import {
  CalendarClock,
  Copy,
  Download,
  Layers,
  Loader2,
  MoreHorizontal,
  PackageOpen,
  Pencil,
  Plus,
  PlusCircle,
  Ticket,
  Trash2
} from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { CardKeyAppendDialog, CardKeyExtractDialog } from "@/components/apps/card-key/card-key-batch-dialogs";
import { CardKeyBatchEditor, type BatchEditorTarget } from "@/components/apps/card-key/card-key-batch-editor";
import {
  BatchProgress,
  CardKindBadge,
  EmptyHint,
  RewardSummary,
  batchRuleChips,
  formatCardDate,
  formatFaceValue,
  isPending,
  validityLabel
} from "@/components/apps/card-key/card-key-shared";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
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
import { ApiError } from "@/lib/api/client";
import type { CardKeyBatch, CardKeyExportOptions, CardKeyKind } from "@/lib/api/card-key";
import {
  useCardKeyBatchesQuery,
  useCardKeyCatalogQuery,
  useDeleteCardKeyBatchMutation,
  useExportCardKeyBatchMutation,
  useSetCardKeyBatchStatusMutation
} from "@/lib/card-key-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

/** 批次有核销记录时后端拒删的业务码 */
const BATCH_IN_USE = 40982;

const EXPORTS: Array<{ label: string; hint: string; options: CardKeyExportOptions }> = [
  { label: "CSV 全部", hint: "带状态、绑定账号与提取信息", options: { format: "csv" } },
  { label: "TXT 仅未使用", hint: "一行一张，可直接发给渠道", options: { format: "txt", unusedOnly: true } },
  {
    label: "TXT 未使用且未提取",
    hint: "不会与已交给渠道的卡重复",
    options: { format: "txt", unusedOnly: true, undistributedOnly: true }
  }
];

/**
 * 批次列表。
 *
 * 每一张卡片回答三件事：这批是什么卡、按什么规则发什么、用掉多少还剩多少能卖。
 * 核销进度随列表一起下发（后端 `stats`），不另开一个接口 —— 分两次请求会出现
 * 「列表已刷新、进度还是上一次」的画面，而运营正是照着那个数字决定要不要补发、提卡。
 */
export function CardKeyBatchesPanel({
  appKey,
  onOpenCodes
}: {
  appKey: string;
  onOpenCodes: (batchId: number) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [kind, setKind] = useState<CardKeyKind | undefined>(undefined);
  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const batchesQuery = useCardKeyBatchesQuery(appKey, { keyword: debouncedKeyword || undefined, kind });
  const catalogQuery = useCardKeyCatalogQuery(appKey);
  const statusMutation = useSetCardKeyBatchStatusMutation(appKey);
  const deleteMutation = useDeleteCardKeyBatchMutation(appKey);
  const exportMutation = useExportCardKeyBatchMutation(appKey);

  const [editor, setEditor] = useState<BatchEditorTarget | null>(null);
  const [appending, setAppending] = useState<CardKeyBatch | null>(null);
  const [extracting, setExtracting] = useState<CardKeyBatch | null>(null);
  const [pendingDelete, setPendingDelete] = useState<CardKeyBatch | null>(null);
  const [inUse, setInUse] = useState<CardKeyBatch | null>(null);

  const batches = useMemo(() => batchesQuery.data ?? [], [batchesQuery.data]);
  const catalog = catalogQuery.data;
  const filtered = Boolean(keyword || kind);

  const toggleStatus = async (batch: CardKeyBatch, enabled: boolean) => {
    try {
      await statusMutation.mutateAsync({ batchId: batch.id, enabled });
      toast.success(enabled ? `已启用「${batch.name}」` : `已停用「${batch.name}」`);
      return true;
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
      return false;
    }
  };

  const handleExport = async (batch: CardKeyBatch, options: CardKeyExportOptions) => {
    try {
      await exportMutation.mutateAsync({ batchId: batch.id, options });
      toast.success("导出已开始下载");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "导出失败");
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    const target = pendingDelete;
    try {
      await deleteMutation.mutateAsync(target.id);
      toast.success(`已删除「${target.name}」`);
      setPendingDelete(null);
    } catch (error) {
      setPendingDelete(null);
      if (error instanceof ApiError && error.code === BATCH_IN_USE) {
        setInUse(target);
        return;
      }
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Layers className="size-4" />}
        title="卡密批次"
        description="生成、导入、补发与提卡都以批次为单位"
        aside={
          <Button size="sm" onClick={() => setEditor({ mode: "new" })}>
            <Plus className="size-3.5" /> 新建卡密
          </Button>
        }
      >
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索名称、渠道或备注"
            className="h-8 w-56"
          />
          <Select value={kind ?? "all"} onValueChange={(value) => setKind(value === "all" ? undefined : (value as CardKeyKind))}>
            <SelectTrigger className="h-8 w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部类型</SelectItem>
              <SelectItem value="redeem">兑换卡</SelectItem>
              <SelectItem value="login">授权卡</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {batchesQuery.isLoading ? (
          <div className="space-y-3">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-36 w-full rounded-xl" />
            ))}
          </div>
        ) : batches.length === 0 ? (
          <EmptyHint>{filtered ? "暂无匹配的批次" : "暂无卡密批次，点击右上角新建"}</EmptyHint>
        ) : (
          <div className="space-y-3">
            {batches.map((batch) => (
              <BatchCard
                key={batch.id}
                batch={batch}
                catalog={catalog}
                exporting={exportMutation.isPending}
                onToggle={(enabled) => toggleStatus(batch, enabled)}
                onEdit={() => setEditor({ mode: "edit", batch })}
                onTemplate={() => setEditor({ mode: "template", batch })}
                onAppend={() => setAppending(batch)}
                onExtract={() => setExtracting(batch)}
                onExport={(options) => handleExport(batch, options)}
                onOpenCodes={() => onOpenCodes(batch.id)}
                onDelete={() => setPendingDelete(batch)}
              />
            ))}
          </div>
        )}
      </SectionCard>

      <CardKeyBatchEditor appKey={appKey} target={editor} onClose={() => setEditor(null)} />
      <CardKeyAppendDialog appKey={appKey} batch={appending} onClose={() => setAppending(null)} />
      <CardKeyExtractDialog appKey={appKey} batch={extracting} onClose={() => setExtracting(null)} />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除批次「{pendingDelete?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete?.stats?.total ?? pendingDelete?.total ?? 0} 张卡将一并删除，且无法恢复。
              只有从没被用过的批次能删除。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={Boolean(inUse)} onOpenChange={(open) => !open && setInUse(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>「{inUse?.name}」不能删除</AlertDialogTitle>
            <AlertDialogDescription>
              这一批已经有卡被使用，删除会连同核销记录一起删掉，而那是「谁领了什么」唯一的凭据。
              可以停用批次：所有未使用的卡立即不可用，之后还能重新启用。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>返回</AlertDialogCancel>
            {inUse?.status === "active" ? (
              <AlertDialogAction
                onClick={async () => {
                  if (inUse) await toggleStatus(inUse, false);
                  setInUse(null);
                }}
              >
                停用批次
              </AlertDialogAction>
            ) : null}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function BatchCard({
  batch,
  catalog,
  exporting,
  onToggle,
  onEdit,
  onTemplate,
  onAppend,
  onExtract,
  onExport,
  onOpenCodes,
  onDelete
}: {
  batch: CardKeyBatch;
  catalog: Parameters<typeof RewardSummary>[0]["catalog"];
  exporting: boolean;
  onToggle: (enabled: boolean) => void;
  onEdit: () => void;
  onTemplate: () => void;
  onAppend: () => void;
  onExtract: () => void;
  onExport: (options: CardKeyExportOptions) => void;
  onOpenCodes: () => void;
  onDelete: () => void;
}) {
  const face = formatFaceValue(batch.faceValue);
  const chips = batchRuleChips(batch);
  const scheduled = isPending(batch.startsAt);
  const available = batch.stats?.available ?? 0;

  return (
    <div className="rounded-xl border border-border p-4 transition-colors hover:border-foreground/15">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold">{batch.name}</span>
            <CardKindBadge kind={batch.kind} />
            {batch.status === "disabled" ? (
              <Badge variant="danger" size="sm">
                已停用
              </Badge>
            ) : null}
            {scheduled ? (
              <Badge variant="warning" size="sm">
                <CalendarClock /> {formatCardDate(batch.startsAt)} 生效
              </Badge>
            ) : null}
            {batch.channel ? (
              <Badge variant="outline" size="sm" className="font-normal">
                {batch.channel}
              </Badge>
            ) : null}
            {face ? <span className="text-xs font-medium tabular-nums text-muted-foreground">{face} / 张</span> : null}
          </div>
          <RewardSummary rewards={batch.rewards} catalog={catalog} />
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
            <span>{validityLabel(batch.validityMode, batch.validityDays, batch.validUntil)}</span>
            {chips.map((chip) => (
              <span key={chip}>{chip}</span>
            ))}
            {batch.codePrefix ? <span className="font-mono">前缀 {batch.codePrefix}</span> : null}
            <span>
              {formatCardDate(batch.createdAt)}
              {batch.createdBy ? ` ${batch.createdBy}` : ""}
            </span>
          </div>
          {batch.remark ? <p className="text-xs text-muted-foreground">{batch.remark}</p> : null}
        </div>

        <div className="flex shrink-0 items-center gap-1">
          <Switch checked={batch.status === "active"} onCheckedChange={onToggle} aria-label="启用批次" />
          <Button size="sm" variant="ghost" onClick={onOpenCodes}>
            <Ticket className="size-3.5" /> 查看卡
          </Button>
          <Button size="sm" variant="outline" onClick={onExtract} disabled={batch.status !== "active" || available === 0}>
            <PackageOpen className="size-3.5" /> 提卡
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon" variant="ghost" className="size-8" aria-label="更多操作">
                <MoreHorizontal className="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuItem onSelect={onEdit}>
                <Pencil /> 编辑
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onAppend} disabled={batch.origin === "imported"}>
                <PlusCircle /> {batch.origin === "imported" ? "补发（导入批次不支持）" : "补发"}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onTemplate}>
                <Copy /> 以此为模板新建
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuLabel className="text-[11px] font-normal text-muted-foreground">导出</DropdownMenuLabel>
              {EXPORTS.map((item) => (
                <DropdownMenuItem key={item.label} disabled={exporting} onSelect={() => onExport(item.options)}>
                  <Download />
                  <div className="min-w-0">
                    <p className="text-xs">{item.label}</p>
                    <p className="text-[11px] text-muted-foreground">{item.hint}</p>
                  </div>
                </DropdownMenuItem>
              ))}
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                <Trash2 /> 删除
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      <div className="mt-3">
        <BatchProgress stats={batch.stats} total={batch.total} />
      </div>
    </div>
  );
}
