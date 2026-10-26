"use client";

/**
 * 记录详情抽屉：任何一行记录点开后，右侧滑出完整字段与原始 JSON。
 *
 * 列表只放判断用的几列，其余字段（流水号、来源、IP、UA、附加数据）都在这里，
 * 底部 `actions` 放针对这一条的操作（冲正、发放、撤销……）。
 */

import * as React from "react";
import { JsonViewer } from "@/components/ui/json-viewer";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { InfoGrid, type InfoItem } from "./surface";

export function RecordSheet({
  open,
  onOpenChange,
  title,
  description,
  hero,
  fields,
  raw,
  actions,
  children
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: React.ReactNode;
  description?: React.ReactNode;
  /** 标题下方的醒目区（金额、状态） */
  hero?: React.ReactNode;
  fields?: InfoItem[];
  raw?: unknown;
  actions?: React.ReactNode;
  children?: React.ReactNode;
}) {
  const rawText = React.useMemo(() => {
    if (raw === undefined || raw === null) return "";
    try {
      return JSON.stringify(raw, null, 2);
    } catch {
      return String(raw);
    }
  }, [raw]);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 p-0 sm:max-w-lg">
        <SheetHeader className="gap-1 border-b px-5 py-4 pr-12">
          <SheetTitle className="text-base">{title}</SheetTitle>
          {description ? <SheetDescription>{description}</SheetDescription> : null}
        </SheetHeader>
        <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-5 py-5">
          {hero}
          {fields?.length ? <InfoGrid items={fields} columns={2} /> : null}
          {children}
          {rawText ? (
            <div className="flex flex-col gap-2">
              <div className="text-xs font-medium text-muted-foreground">原始数据</div>
              <JsonViewer value={rawText} height={220} />
            </div>
          ) : null}
        </div>
        {actions ? <SheetFooter className="flex-row flex-wrap justify-end gap-2 border-t px-5 py-3">{actions}</SheetFooter> : null}
      </SheetContent>
    </Sheet>
  );
}
