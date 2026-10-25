"use client";

import { memo, useEffect, useRef } from "react";
import { AlarmClock, ImageIcon, Inbox, MessageSquareText, Paperclip, Star } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { VirtualList, type VirtualListHandle } from "@/components/ui/virtual-list";
import type { TicketItem } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import {
  FeedbackCategoryBadge,
  PersonAvatar,
  PriorityBadge,
  SLABadge,
  STATUS_LABEL,
  StatusDot,
  formatClientShort,
  formatDateTime,
  formatDue,
  formatRelativeTime,
  getTicketClient,
  isAwaitingReply,
  isImageAttachment
} from "../ticket-shared";

// 工作台左栏列表。
//
// 行高固定：VirtualList 给每行写死了 height，所以行内一律单行截断，
// 信息按「谁 / 什么事 / 现在怎样」三行排，扫一眼就能挑出该先回哪条。

export const TICKET_ROW_HEIGHT = 92;

type Props = {
  items: TicketItem[];
  kind: "ticket" | "feedback";
  selectedId: number | null;
  onOpen: (id: number) => void;
  categoryKeyById: Map<number, string>;
  /** 开启后行首显示勾选框，用于批量操作 */
  selectable?: boolean;
  checked: Set<number>;
  onToggleCheck: (id: number) => void;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
  empty: React.ReactNode;
};

export function TicketList({
  items,
  kind,
  selectedId,
  onOpen,
  categoryKeyById,
  selectable,
  checked,
  onToggleCheck,
  hasMore,
  loadingMore,
  onLoadMore,
  empty
}: Props) {
  const listRef = useRef<VirtualListHandle>(null);

  // 键盘切换选中行时，让它始终留在可视区里
  const selectedIndex = selectedId ? items.findIndex((item) => item.id === selectedId) : -1;
  useEffect(() => {
    if (selectedIndex >= 0) listRef.current?.scrollToIndex(selectedIndex, { align: "auto" });
  }, [selectedIndex]);

  return (
    <VirtualList
      ref={listRef}
      items={items}
      estimateSize={TICKET_ROW_HEIGHT}
      getItemKey={(index) => items[index]?.id ?? index}
      onEndReached={onLoadMore}
      hasMore={hasMore}
      isLoading={loadingMore}
      ariaLabel={kind === "feedback" ? "反馈列表" : "工单列表"}
      className="h-full rounded-none"
      empty={empty}
      renderItem={(item) => (
        <TicketRow
          item={item}
          kind={kind}
          active={item.id === selectedId}
          categoryKey={item.categoryId ? categoryKeyById.get(item.categoryId) : undefined}
          selectable={selectable}
          checked={checked.has(item.id)}
          anyChecked={checked.size > 0}
          onOpen={onOpen}
          onToggleCheck={onToggleCheck}
        />
      )}
    />
  );
}

/** 列表接口返回计数时直接用；否则按附件数组统计（旧后端两者都没有时不展示） */
function countAttachments(item: TicketItem) {
  if (item.imageCount !== undefined || item.attachmentCount !== undefined) {
    const images = item.imageCount ?? 0;
    return { images, files: Math.max(0, (item.attachmentCount ?? images) - images) };
  }
  const list = item.attachments ?? [];
  const images = list.filter(isImageAttachment).length;
  return { images, files: list.length - images };
}

const TicketRow = memo(function TicketRow({
  item,
  kind,
  active,
  categoryKey,
  selectable,
  checked,
  anyChecked,
  onOpen,
  onToggleCheck
}: {
  item: TicketItem;
  kind: "ticket" | "feedback";
  active: boolean;
  categoryKey?: string;
  selectable?: boolean;
  checked: boolean;
  anyChecked: boolean;
  onOpen: (id: number) => void;
  onToggleCheck: (id: number) => void;
}) {
  const client = formatClientShort(getTicketClient(item.metadata));
  const { images, files } = countAttachments(item);
  const awaiting = isAwaitingReply(item);
  const lastAt = item.lastMessageAt || item.updatedAt;
  const due = item.firstRespondedAt ? formatDue(item.resolveDueAt) : formatDue(item.firstResponseDueAt);
  const showDue = kind === "ticket" && due.overdue && awaiting;

  return (
    <div
      className={cn(
        "group/row relative flex h-full cursor-pointer gap-3 border-b px-4 py-3 transition-colors",
        active ? "bg-accent" : "hover:bg-muted/50",
        checked && !active && "bg-primary/5"
      )}
      onClick={() => onOpen(item.id)}
    >
      {/* 选中行左侧的指示条 */}
      <span
        aria-hidden
        className={cn("absolute inset-y-0 left-0 w-0.5 bg-primary transition-opacity", active ? "opacity-100" : "opacity-0")}
      />

      <div className="relative mt-0.5 shrink-0">
        {selectable ? (
          <div
            className={cn(
              "absolute inset-0 z-10 flex items-center justify-center rounded-full bg-background transition-opacity",
              checked || anyChecked ? "opacity-100" : "opacity-0 group-hover/row:opacity-100"
            )}
            onClick={(event) => event.stopPropagation()}
          >
            <Checkbox
              checked={checked}
              onCheckedChange={() => onToggleCheck(item.id)}
              aria-label={`选择 ${item.ticketNo}`}
            />
          </div>
        ) : null}
        <PersonAvatar name={item.requesterName} />
        {awaiting ? (
          <span className="absolute -right-0.5 -top-0.5 size-2.5 rounded-full bg-primary ring-2 ring-background" />
        ) : null}
      </div>

      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2">
          <span className={cn("truncate text-sm", awaiting ? "font-semibold text-foreground" : "font-medium text-foreground/90")}>
            {item.requesterName || "匿名用户"}
          </span>
          {item.appName ? <span className="truncate text-xs text-muted-foreground">{item.appName}</span> : null}
          <span className="ml-auto shrink-0 text-xs text-muted-foreground" title={formatDateTime(lastAt)}>
            {formatRelativeTime(lastAt)}
          </span>
        </div>

        <div className="flex min-w-0 items-center gap-1.5">
          {kind === "feedback" ? <FeedbackCategoryBadge name={item.categoryName} categoryKey={categoryKey} /> : null}
          {item.priority === "urgent" || item.priority === "high" ? <PriorityBadge priority={item.priority} /> : null}
          <span className="truncate text-sm text-foreground/80">{item.title}</span>
        </div>

        <div className="flex min-w-0 items-center gap-3 text-xs text-muted-foreground">
          <span className="inline-flex shrink-0 items-center gap-1.5">
            <StatusDot status={item.status} />
            {STATUS_LABEL[item.status]}
          </span>
          {awaiting ? (
            <Badge variant="info" size="sm" className="shrink-0">
              待回复
            </Badge>
          ) : null}
          {showDue ? (
            <span className="inline-flex shrink-0 items-center gap-1 font-medium text-destructive">
              <AlarmClock className="size-3" />
              {due.text}
            </span>
          ) : (
            <SLABadge state={item.slaState} />
          )}
          {kind === "ticket" ? (
            <span className="truncate">{item.assigneeName || item.groupName || "未指派"}</span>
          ) : client ? (
            <span className="truncate">{client}</span>
          ) : null}
          <span className="ml-auto flex shrink-0 items-center gap-2.5">
            {images > 0 ? (
              <span className="inline-flex items-center gap-0.5" title={`${images} 张图片`}>
                <ImageIcon className="size-3" />
                {images}
              </span>
            ) : null}
            {files > 0 ? (
              <span className="inline-flex items-center gap-0.5" title={`${files} 个附件`}>
                <Paperclip className="size-3" />
                {files}
              </span>
            ) : null}
            {item.rating ? (
              <span className="inline-flex items-center gap-0.5 text-amber-500" title={`满意度 ${item.rating} 星`}>
                <Star className="size-3 fill-current" />
                {item.rating}
              </span>
            ) : null}
            <span className="inline-flex items-center gap-0.5" title={`${item.messageCount} 条会话`}>
              <MessageSquareText className="size-3" />
              {item.messageCount}
            </span>
          </span>
        </div>
      </div>
    </div>
  );
});

export function TicketListSkeleton({ rows = 8 }: { rows?: number }) {
  return (
    <div aria-busy className="divide-y">
      {Array.from({ length: rows }, (_, index) => (
        <div key={index} className="flex gap-3 px-4 py-3" style={{ height: TICKET_ROW_HEIGHT }}>
          <Skeleton className="size-8 shrink-0 rounded-full" />
          <div className="flex-1 space-y-2">
            <div className="flex justify-between">
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-3 w-12" />
            </div>
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  );
}

export function TicketListEmpty({ filtered, kind }: { filtered: boolean; kind: "ticket" | "feedback" }) {
  return (
    <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
      <span className="flex size-12 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
        <Inbox className="size-5" />
      </span>
      <div className="space-y-1">
        <p className="text-sm font-medium text-foreground">
          {filtered ? "没有符合条件的记录" : kind === "feedback" ? "还没有收到反馈" : "暂无工单"}
        </p>
        <p className="text-xs text-muted-foreground">{filtered ? "换个关键词或清除筛选条件再试" : "新的提交会自动出现在这里"}</p>
      </div>
    </div>
  );
}
