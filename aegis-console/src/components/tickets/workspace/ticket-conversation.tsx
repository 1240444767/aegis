"use client";

import { Fragment, useLayoutEffect, useMemo, useRef } from "react";
import { ArrowRightLeft, Lock, RotateCcw, Star, Tag, UserCheck } from "lucide-react";
import { Markdown } from "@/components/ai/markdown";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { TicketEvent, TicketItem, TicketMessage } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { PersonAvatar, formatDateTime } from "../ticket-shared";
import { TicketAttachmentList } from "../ticket-attachments";

// 会话流。
//
// 用户的话靠左、我们的回复靠右、内部备注是整宽的琥珀色便签，
// 状态 / 指派这类关键操作以细线穿插在消息之间，不用切到「时间线」就知道发生过什么。
// 同一人 5 分钟内的连续消息合并，只在第一条画头像和名字。

const GROUP_GAP_MS = 5 * 60_000;

// 穿插进会话的事件；回复、备注本身已经是消息，不重复画
const INLINE_EVENTS = new Set([
  "status_changed",
  "assigned",
  "group_changed",
  "reopened",
  "priority_changed",
  "category_changed",
  "tags_changed",
  "merged"
]);

type Entry =
  | { type: "day"; key: string; label: string }
  | { type: "message"; key: string; message: TicketMessage; head: boolean }
  | { type: "event"; key: string; event: TicketEvent };

function dayLabel(date: Date) {
  const today = new Date();
  const startOf = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const diff = Math.round((startOf(today) - startOf(date)) / 86_400_000);
  if (diff === 0) return "今天";
  if (diff === 1) return "昨天";
  const sameYear = date.getFullYear() === today.getFullYear();
  return date.toLocaleDateString("zh-CN", sameYear ? { month: "long", day: "numeric", weekday: "short" } : { year: "numeric", month: "long", day: "numeric" });
}

function clock(value: string) {
  return new Date(value).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
}

function buildEntries(messages: TicketMessage[], events: TicketEvent[]): Entry[] {
  const merged = [
    ...messages.map((message) => ({ at: new Date(message.createdAt).getTime(), message })),
    ...events
      .filter((event) => INLINE_EVENTS.has(event.event))
      .map((event) => ({ at: new Date(event.createdAt).getTime(), event }))
  ].sort((a, b) => a.at - b.at);

  const entries: Entry[] = [];
  let lastDay = "";
  let prev: TicketMessage | null = null;
  for (const item of merged) {
    const date = new Date(item.at);
    const day = date.toDateString();
    if (day !== lastDay) {
      entries.push({ type: "day", key: `day-${day}`, label: dayLabel(date) });
      lastDay = day;
      prev = null;
    }
    if ("message" in item) {
      const message = item.message;
      const head =
        !prev ||
        prev.authorType !== message.authorType ||
        prev.authorName !== message.authorName ||
        prev.internal !== message.internal ||
        item.at - new Date(prev.createdAt).getTime() > GROUP_GAP_MS;
      entries.push({ type: "message", key: `m-${message.id}`, message, head });
      prev = message;
    } else {
      entries.push({ type: "event", key: `e-${item.event.id}`, event: item.event });
      prev = null;
    }
  }
  return entries;
}

export function TicketConversation({ ticket }: { ticket: TicketItem }) {
  const messages = useMemo(() => ticket.messages ?? [], [ticket.messages]);
  const entries = useMemo(() => buildEntries(messages, ticket.events ?? []), [messages, ticket.events]);
  const scrollRef = useRef<HTMLDivElement>(null);

  // 打开或有新消息时贴到底部；切换工单时整个组件按 key 重建，自然回到底部
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages.length]);

  return (
    <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
      <div className="mx-auto max-w-3xl space-y-1 px-5 py-5">
        {entries.length === 0 ? (
          <p className="py-16 text-center text-sm text-muted-foreground">暂无会话记录</p>
        ) : null}
        {entries.map((entry) => (
          <Fragment key={entry.key}>
            {entry.type === "day" ? (
              <div className="sticky top-0 z-10 flex justify-center py-2">
                <span className="rounded-full border bg-background/90 px-2.5 py-0.5 text-xs text-muted-foreground shadow-xs backdrop-blur">
                  {entry.label}
                </span>
              </div>
            ) : entry.type === "event" ? (
              <EventLine event={entry.event} />
            ) : (
              <MessageBubble message={entry.message} head={entry.head} />
            )}
          </Fragment>
        ))}
        {ticket.rating ? <RatingCard ticket={ticket} /> : null}
      </div>
    </div>
  );
}

function MessageBubble({ message, head }: { message: TicketMessage; head: boolean }) {
  const name = message.authorName || "系统";

  if (message.authorType === "system") {
    return (
      <div className="flex justify-center py-1.5">
        <span className="max-w-[80%] rounded-lg bg-muted px-3 py-1.5 text-center text-xs text-muted-foreground">
          {message.content}
        </span>
      </div>
    );
  }

  if (message.internal) {
    return (
      <div className={cn("pl-11", head ? "pt-3" : "pt-0.5")}>
        <div className="rounded-xl border border-amber-200 bg-amber-50/70 px-3.5 py-2.5 dark:border-amber-900/60 dark:bg-amber-950/30">
          {head ? (
            <div className="mb-1 flex items-center gap-2 text-xs">
              <Lock className="size-3 text-amber-600 dark:text-amber-400" />
              <span className="font-medium text-amber-800 dark:text-amber-300">{name} 的内部备注</span>
              <span className="text-amber-700/70 dark:text-amber-400/70" title={formatDateTime(message.createdAt)}>
                {clock(message.createdAt)}
              </span>
            </div>
          ) : null}
          <MessageBody message={message} />
          {message.attachments?.length ? <TicketAttachmentList attachments={message.attachments} className="mt-2" /> : null}
        </div>
      </div>
    );
  }

  const mine = message.authorType === "agent";
  return (
    <div className={cn("group/msg flex gap-3", mine && "flex-row-reverse", head ? "pt-3" : "pt-0.5")}>
      <div className="w-8 shrink-0">{head ? <PersonAvatar name={name} /> : null}</div>
      <div className={cn("flex min-w-0 max-w-[78%] flex-col gap-1", mine && "items-end")}>
        {head ? (
          <div className={cn("flex items-center gap-2 text-xs text-muted-foreground", mine && "flex-row-reverse")}>
            <span className="font-medium text-foreground">{name}</span>
            {mine ? (
              <Badge variant="info" size="sm">
                客服
              </Badge>
            ) : null}
            <span title={formatDateTime(message.createdAt)}>{clock(message.createdAt)}</span>
            {message.editedAt ? <span>已编辑</span> : null}
          </div>
        ) : null}
        {message.content ? (
          <Tooltip delayDuration={600}>
            <TooltipTrigger asChild>
              <div
                className={cn(
                  "rounded-2xl px-3.5 py-2 text-sm leading-6",
                  mine
                    ? "rounded-tr-md bg-primary text-primary-foreground [&_a]:text-primary-foreground [&_a]:underline"
                    : "rounded-tl-md bg-muted text-foreground",
                  !head && (mine ? "rounded-tr-2xl" : "rounded-tl-2xl")
                )}
              >
                <MessageBody message={message} />
              </div>
            </TooltipTrigger>
            <TooltipContent side={mine ? "left" : "right"}>{formatDateTime(message.createdAt)}</TooltipContent>
          </Tooltip>
        ) : null}
        {message.attachments?.length ? <TicketAttachmentList attachments={message.attachments} /> : null}
      </div>
    </div>
  );
}

// 用户的纯文本只识别网址；客服回复和用户声明为 Markdown 的内容按 Markdown 渲染（App 端也是这么显示的）
const URL_PATTERN = /(https?:\/\/[^\s<>"']+)/g;

function MessageBody({ message }: { message: TicketMessage }) {
  if (message.authorType === "agent" || message.contentType === "markdown") {
    return <Markdown text={message.content} className="text-sm leading-6 [&_p]:my-0 [&_p+p]:mt-2" />;
  }
  const parts = message.content.split(URL_PATTERN);
  return (
    <p className="whitespace-pre-wrap break-words">
      {parts.map((part, index) =>
        index % 2 === 1 ? (
          <a key={index} href={part} target="_blank" rel="noreferrer" className="text-primary underline underline-offset-2">
            {part}
          </a>
        ) : (
          <Fragment key={index}>{part}</Fragment>
        )
      )}
    </p>
  );
}

const EVENT_ICON: Record<string, typeof ArrowRightLeft> = {
  status_changed: ArrowRightLeft,
  assigned: UserCheck,
  group_changed: UserCheck,
  reopened: RotateCcw,
  tags_changed: Tag
};

function EventLine({ event }: { event: TicketEvent }) {
  const Icon = EVENT_ICON[event.event] ?? ArrowRightLeft;
  return (
    <div className="flex items-center gap-3 py-2 text-xs text-muted-foreground">
      <span className="h-px flex-1 bg-border" />
      <span className="inline-flex max-w-[80%] items-center gap-1.5">
        <Icon className="size-3 shrink-0" />
        <span className="truncate">
          <span className="font-medium text-foreground/80">{event.actorName || "系统"}</span> {event.summary || event.event}
        </span>
        <span className="shrink-0" title={formatDateTime(event.createdAt)}>
          {clock(event.createdAt)}
        </span>
      </span>
      <span className="h-px flex-1 bg-border" />
    </div>
  );
}

function RatingCard({ ticket }: { ticket: TicketItem }) {
  const rating = ticket.rating ?? 0;
  return (
    <div className="flex justify-center pt-4">
      <div className="w-full max-w-sm rounded-xl border bg-card px-4 py-3 text-center">
        <p className="text-xs text-muted-foreground">{ticket.requesterName || "用户"} 的评价</p>
        <div className="mt-1 flex justify-center gap-0.5" aria-label={`${rating} 星`}>
          {Array.from({ length: 5 }, (_, index) => (
            <Star
              key={index}
              className={cn("size-4", index < rating ? "fill-amber-400 text-amber-400" : "text-muted-foreground/30")}
            />
          ))}
        </div>
        {ticket.ratingComment ? <p className="mt-2 text-sm text-foreground">{ticket.ratingComment}</p> : null}
        {ticket.ratedAt ? <p className="mt-1 text-xs text-muted-foreground">{formatDateTime(ticket.ratedAt)}</p> : null}
      </div>
    </div>
  );
}
