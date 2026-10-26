"use client";

/**
 * 互动记录各子页共用的小件：发放结果徽标、分页状态、导出按钮、带备注的确认框、关联用户。
 */

import * as React from "react";
import Link from "next/link";
import { Download, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { formatDateTime } from "@/lib/datetime";
import { collectPages, downloadCsv, errorMessage, type CsvColumn } from "../kit";
import { userInitials } from "../user-detail-shared";

export const EXPORT_MAX_ROWS = 5000;

/** 发放结果（各域的 results 结构相同：type / label / detail / transactionNo / expireAt / pending） */
export type RewardResultLike = {
  type: string;
  label: string;
  detail: string;
  transactionNo?: string;
  expireAt?: string;
  pending?: boolean;
};

export function ResultChips({ results, empty = "—" }: { results?: RewardResultLike[] | null; empty?: string }) {
  if (!results?.length) return <span className="text-xs text-muted-foreground">{empty}</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {results.map((result, index) => (
        <Badge
          key={`${result.type}-${index}`}
          variant={result.pending ? "warning" : "outline"}
          size="sm"
          className="font-normal"
          title={result.transactionNo ? `流水号 ${result.transactionNo}` : undefined}
        >
          {resultText(result)}
        </Badge>
      ))}
    </div>
  );
}

export function resultText(result: RewardResultLike) {
  const head = result.detail && result.detail.includes(result.label) ? result.detail : [result.label, result.detail].filter(Boolean).join(" ");
  return result.pending ? `${head}（待人工发放）` : head;
}

export function resultsCsv(results?: RewardResultLike[] | null) {
  return (results ?? []).map(resultText).join("；");
}

/** 结果明细：抽屉里逐项列出，带流水号与到期时间 */
export function ResultList({ results, title = "发放结果" }: { results?: RewardResultLike[] | null; title?: string }) {
  if (!results?.length) return null;
  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs font-medium text-muted-foreground">{title}</div>
      <ul className="flex flex-col divide-y rounded-lg border">
        {results.map((result, index) => (
          <li key={`${result.type}-${index}`} className="flex flex-col gap-0.5 px-3 py-2">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="font-medium">{result.label || result.type}</span>
              {result.pending ? (
                <Badge variant="warning" size="sm">
                  待人工发放
                </Badge>
              ) : null}
            </div>
            {result.detail ? <div className="text-xs text-muted-foreground">{result.detail}</div> : null}
            {result.transactionNo || result.expireAt ? (
              <div className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                {result.transactionNo ? <span className="font-mono">流水号 {result.transactionNo}</span> : null}
                {result.expireAt ? <span>到期 {formatDateTime(result.expireAt)}</span> : null}
              </div>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** 页码与每页条数；筛选变化时调用 reset 回到第一页 */
export function usePaging(initialLimit = 20) {
  const [page, setPage] = React.useState(1);
  const [limit, setLimitState] = React.useState(initialLimit);
  const setLimit = React.useCallback((next: number) => {
    setLimitState(next);
    setPage(1);
  }, []);
  const reset = React.useCallback(() => setPage(1), []);
  return { page, limit, setPage, setLimit, reset };
}

/** 导出 CSV：逐页拉全量（最多 5000 条） */
export function ExportButton<T>({
  filename,
  columns,
  fetchPage,
  disabled
}: {
  filename: string;
  columns: Array<CsvColumn<T>>;
  fetchPage: (page: number, limit: number) => Promise<{ items: T[] | null; total: number }>;
  disabled?: boolean;
}) {
  const [busy, setBusy] = React.useState(false);
  const run = async () => {
    setBusy(true);
    try {
      const { rows, total, truncated } = await collectPages(fetchPage, { limit: 100, maxRows: EXPORT_MAX_ROWS });
      if (!rows.length) {
        toast.info("没有可导出的记录");
        return;
      }
      downloadCsv(filename, columns, rows);
      if (truncated) toast.warning(`共 ${total.toLocaleString("zh-CN")} 条，仅导出前 ${EXPORT_MAX_ROWS} 条，请缩小筛选范围`);
      else toast.success(`已导出 ${rows.length.toLocaleString("zh-CN")} 条`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Button variant="outline" size="sm" onClick={run} disabled={busy || disabled}>
      {busy ? <Loader2 className="animate-spin" /> : <Download />}
      导出 CSV
    </Button>
  );
}

/** 文件名里的日期：本地日期，不用 toISOString */
export function exportStamp() {
  const now = new Date();
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}`;
}

/**
 * 二次确认框，可选备注。破坏性操作 `destructive`；`note` 给出时显示备注输入（写进给用户的站内信）。
 * 提交中不允许关闭，失败时保留输入。
 */
export function ConfirmActionDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  destructive,
  pending,
  disabled,
  note,
  onConfirm,
  children
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: React.ReactNode;
  description?: React.ReactNode;
  confirmLabel: string;
  destructive?: boolean;
  pending?: boolean;
  /** 确认按钮不可用（表单未填完） */
  disabled?: boolean;
  note?: { value: string; onChange: (value: string) => void; placeholder?: string; hint?: string; max?: number; required?: boolean };
  onConfirm: () => void;
  children?: React.ReactNode;
}) {
  const noteEmpty = note?.required && !note.value.trim();
  return (
    <AlertDialog open={open} onOpenChange={(next) => (!pending ? onOpenChange(next) : undefined)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description ? <AlertDialogDescription>{description}</AlertDialogDescription> : null}
        </AlertDialogHeader>
        {children}
        {note ? (
          <div className="flex flex-col gap-1.5">
            <Textarea
              value={note.value}
              rows={3}
              maxLength={note.max ?? 500}
              placeholder={note.placeholder}
              onChange={(event) => note.onChange(event.target.value)}
            />
            <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
              <span>{note.hint}</span>
              <span className="tabular-nums">
                {note.value.length}/{note.max ?? 500}
              </span>
            </div>
          </div>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>返回</AlertDialogCancel>
          <Button variant={destructive ? "destructive" : "default"} disabled={pending || disabled || noteEmpty} onClick={onConfirm}>
            {pending ? <Loader2 className="animate-spin" /> : null}
            {confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** 关联到的另一个用户：头像 + 昵称 / 账号，点击进入他的详情页 */
export function UserChip({
  appKey,
  userId,
  account,
  nickname,
  avatar,
  sub
}: {
  appKey: string;
  userId?: number;
  account?: string;
  nickname?: string;
  avatar?: string;
  sub?: string;
}) {
  if (!userId) return <span className="text-muted-foreground">—</span>;
  const name = nickname || account || `#${userId}`;
  return (
    <Link
      href={`/app-users/${encodeURIComponent(appKey)}/${userId}?tab=records&sub=invite`}
      onClick={(event) => event.stopPropagation()}
      className="group inline-flex min-w-0 items-center gap-2"
    >
      <Avatar className="size-7">
        {avatar ? <AvatarImage src={avatar} alt="" /> : null}
        <AvatarFallback className="text-xs">{userInitials(nickname, account)}</AvatarFallback>
      </Avatar>
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-sm font-medium group-hover:underline">{name}</span>
        <span className="truncate text-xs text-muted-foreground">{sub ?? (account && nickname ? account : `ID ${userId}`)}</span>
      </span>
    </Link>
  );
}

/** 指标卡网格 */
export function MetricGrid({ children }: { children: React.ReactNode }) {
  return <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">{children}</div>;
}

export function percent(part: number, total: number) {
  if (!total) return "—";
  return `${((part / total) * 100).toFixed(part === total || part === 0 ? 0 : 1)}%`;
}
