"use client";

/**
 * 记录表：服务端分页的列表统一走这里（流水、签到、抽奖、登录记录……）。
 *
 * 只做三件事：按列渲染、给出加载 / 空 / 错三种状态、底部分页。
 * 排序与筛选在服务端，所以这里不引入表格引擎；窄屏下整表横向滚动，列宽由 `minWidth` 兜底。
 * 行可点击时整行是按钮语义（Enter 也能打开），用来开详情抽屉。
 */

import * as React from "react";
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { StateBlock, errorMessage } from "./surface";

export type Column<T> = {
  id: string;
  header: React.ReactNode;
  cell: (row: T, index: number) => React.ReactNode;
  align?: "left" | "right" | "center";
  /** 列宽，如 "120px" 或 "18%" */
  width?: string;
  className?: string;
  /** 窄于该断点时隐藏 */
  hideBelow?: "sm" | "md" | "lg" | "xl";
};

const HIDE: Record<NonNullable<Column<unknown>["hideBelow"]>, string> = {
  sm: "hidden sm:table-cell",
  md: "hidden md:table-cell",
  lg: "hidden lg:table-cell",
  xl: "hidden xl:table-cell"
};

export function RecordTable<T>({
  columns,
  rows,
  rowKey,
  loading,
  fetching,
  error,
  onRetry,
  onRowClick,
  rowClassName,
  empty,
  minWidth = 720,
  skeletonRows = 6
}: {
  columns: Array<Column<T>>;
  rows: T[] | null | undefined;
  rowKey: (row: T, index: number) => React.Key;
  loading?: boolean;
  /** 换页时的后台刷新：保留旧数据、表格淡一点 */
  fetching?: boolean;
  error?: unknown;
  onRetry?: () => void;
  onRowClick?: (row: T) => void;
  rowClassName?: (row: T) => string | undefined;
  empty?: React.ReactNode;
  minWidth?: number;
  skeletonRows?: number;
}) {
  const list = rows ?? [];
  if (error && !list.length) {
    return <StateBlock kind="error" description={errorMessage(error)} onRetry={onRetry} />;
  }
  if (!loading && !list.length) {
    return <>{empty ?? <StateBlock />}</>;
  }
  const alignClass = (align?: Column<T>["align"]) =>
    align === "right" ? "text-right" : align === "center" ? "text-center" : "text-left";

  return (
    <div className="relative w-full overflow-x-auto">
      <table className={cn("w-full caption-bottom text-sm transition-opacity", fetching && !loading && "opacity-60")} style={{ minWidth }}>
        <thead>
          <tr className="border-b bg-muted/30">
            {columns.map((column) => (
              <th
                key={column.id}
                style={column.width ? { width: column.width } : undefined}
                className={cn(
                  "h-9 px-3 text-xs font-medium whitespace-nowrap text-muted-foreground first:pl-4 last:pr-4 sm:first:pl-5 sm:last:pr-5",
                  alignClass(column.align),
                  column.hideBelow && HIDE[column.hideBelow]
                )}
              >
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loading
            ? Array.from({ length: skeletonRows }, (_, i) => (
                <tr key={i} className="border-b last:border-b-0">
                  {columns.map((column) => (
                    <td key={column.id} className={cn("px-3 py-3 first:pl-4 last:pr-4 sm:first:pl-5 sm:last:pr-5", column.hideBelow && HIDE[column.hideBelow])}>
                      <Skeleton className={cn("h-4", column.align === "right" ? "ml-auto w-16" : "w-24")} />
                    </td>
                  ))}
                </tr>
              ))
            : list.map((row, index) => (
                <tr
                  key={rowKey(row, index)}
                  tabIndex={onRowClick ? 0 : undefined}
                  onClick={onRowClick ? () => onRowClick(row) : undefined}
                  onKeyDown={
                    onRowClick
                      ? (event) => {
                          if (event.key === "Enter") onRowClick(row);
                        }
                      : undefined
                  }
                  className={cn(
                    "border-b transition-colors last:border-b-0",
                    onRowClick && "cursor-pointer hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none",
                    rowClassName?.(row)
                  )}
                >
                  {columns.map((column) => (
                    <td
                      key={column.id}
                      className={cn(
                        "px-3 py-2.5 align-middle first:pl-4 last:pr-4 sm:first:pl-5 sm:last:pr-5",
                        alignClass(column.align),
                        column.hideBelow && HIDE[column.hideBelow],
                        column.className
                      )}
                    >
                      {column.cell(row, index)}
                    </td>
                  ))}
                </tr>
              ))}
        </tbody>
      </table>
    </div>
  );
}

/** 分页条：总数、每页条数、首页 / 上一页 / 页码 / 下一页 / 末页。 */
export function Pager({
  page,
  limit,
  total,
  onPage,
  onLimit,
  limits = [20, 50, 100],
  summary
}: {
  page: number;
  limit: number;
  total: number;
  onPage: (page: number) => void;
  onLimit?: (limit: number) => void;
  limits?: number[];
  /** 左侧附加信息（合计等），默认显示「共 N 条」 */
  summary?: React.ReactNode;
}) {
  const pages = Math.max(1, Math.ceil(total / Math.max(limit, 1)));
  const current = Math.min(Math.max(page, 1), pages);
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 text-xs text-muted-foreground">
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
        <span className="tabular-nums">共 {total.toLocaleString("zh-CN")} 条</span>
        {summary}
      </div>
      <div className="flex items-center gap-1.5">
        {onLimit ? (
          <Select value={String(limit)} onValueChange={(value) => onLimit(Number(value))}>
            <SelectTrigger size="sm" className="w-[92px] text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              {limits.map((item) => (
                <SelectItem key={item} value={String(item)} className="text-xs">
                  {item} 条/页
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
        <Button variant="ghost" size="icon-sm" className="hidden sm:inline-flex" disabled={current <= 1} onClick={() => onPage(1)} aria-label="首页">
          <ChevronsLeft />
        </Button>
        <Button variant="ghost" size="icon-sm" disabled={current <= 1} onClick={() => onPage(current - 1)} aria-label="上一页">
          <ChevronLeft />
        </Button>
        <span className="min-w-14 text-center tabular-nums text-foreground">
          {current} / {pages}
        </span>
        <Button variant="ghost" size="icon-sm" disabled={current >= pages} onClick={() => onPage(current + 1)} aria-label="下一页">
          <ChevronRight />
        </Button>
        <Button variant="ghost" size="icon-sm" className="hidden sm:inline-flex" disabled={current >= pages} onClick={() => onPage(pages)} aria-label="末页">
          <ChevronsRight />
        </Button>
      </div>
    </div>
  );
}
