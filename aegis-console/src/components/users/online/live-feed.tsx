"use client";

import { useEffect, useMemo, useState } from "react";
import { ArrowDownRight, ArrowUpRight, Pause, Play, Radio, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { formatClockSeconds } from "@/lib/datetime";
import { useLive } from "@/lib/online-live/hooks";
import type { LiveEvent } from "@/lib/online-live/types";
import { cn } from "@/lib/utils";
import { PanelHeader, formatDuration } from "./live-shared";

function since(at: string, now: number) {
  const seconds = Math.max(0, Math.floor((now - new Date(at).getTime()) / 1000));
  if (seconds < 5) return "刚刚";
  if (seconds < 60) return `${seconds} 秒前`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟前`;
  return formatClockSeconds(at);
}

/**
 * 上下线动态：服务端半秒一批推来，新的在上。可暂停查看，暂停期间新事件照常累积。
 * 瞬时大量上下线时服务端只下发前 100 条，其余以「合并」数提示。
 */
export function LiveFeed({ className, showApp = true }: { className?: string; showApp?: boolean }) {
  const feed = useLive((s) => s.feed);
  const dropped = useLive((s) => s.dropped);
  const apps = useLive((s) => s.snapshot?.apps);
  const [frozen, setFrozen] = useState<LiveEvent[] | null>(null);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(timer);
  }, []);

  const names = useMemo(() => new Map((apps ?? []).map((a) => [a.appid, a.name || `应用 #${a.appid}`])), [apps]);
  const items = frozen ?? feed;
  const pending = frozen ? feed.length - frozen.length : 0;

  return (
    <div className={cn("flex min-h-0 min-w-0 flex-col rounded-2xl border bg-card", className)}>
      <PanelHeader
        title="实时动态"
        icon={<Radio className="size-4 text-muted-foreground" />}
        extra={
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setFrozen(frozen ? null : feed)}
          >
            {frozen ? <Play className="size-3" /> : <Pause className="size-3" />}
            {frozen ? (pending > 0 ? `继续（${pending} 条新动态）` : "继续") : "暂停"}
          </Button>
        }
      />
      {dropped > 0 && !frozen ? (
        <div className="border-b bg-amber-500/10 px-4 py-1.5 text-[11px] text-amber-700 dark:text-amber-300">
          上下线较为密集，最近一批另有 {dropped.toLocaleString("zh-CN")} 条已合并
        </div>
      ) : null}
      {items.length === 0 ? (
        <div className="flex min-h-48 flex-1 flex-col items-center justify-center gap-1 px-6 text-center text-xs text-muted-foreground">
          <span>暂无动态</span>
          <span>用户上线或下线时将实时显示在这里</span>
        </div>
      ) : (
        <ul className="max-h-[22rem] min-h-48 flex-1 divide-y overflow-y-auto [scrollbar-width:thin]">
          {items.map((evt) => (
            <FeedRow key={`${evt.connectionId}:${evt.kind}`} evt={evt} now={now} appName={showApp ? names.get(evt.appid) : undefined} />
          ))}
        </ul>
      )}
    </div>
  );
}

function FeedRow({ evt, now, appName }: { evt: LiveEvent; now: number; appName?: string }) {
  const online = evt.kind === "online";
  const who = evt.account || (evt.admin ? `管理员 #${evt.userId}` : `用户 #${evt.userId}`);
  const where = [evt.location, evt.ip].filter(Boolean).join(" ");
  const device = evt.deviceName || evt.platform;
  return (
    <li className="flex items-start gap-3 px-4 py-2.5">
      <span
        className={cn(
          "mt-0.5 grid size-6 shrink-0 place-items-center rounded-full",
          online ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300" : "bg-muted text-muted-foreground"
        )}
        aria-label={online ? "上线" : "下线"}
      >
        {online ? <ArrowUpRight className="size-3.5" /> : <ArrowDownRight className="size-3.5" />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-1.5 text-sm">
          {evt.admin ? <ShieldCheck className="size-3.5 shrink-0 text-muted-foreground" /> : null}
          <span className="truncate font-medium">{who}</span>
          <span className="shrink-0 text-xs text-muted-foreground">{online ? "上线" : "下线"}</span>
          {appName ? <span className="truncate text-xs text-muted-foreground">{appName}</span> : null}
        </div>
        <div className="mt-0.5 flex min-w-0 flex-wrap gap-x-3 text-[11px] text-muted-foreground">
          {where ? <span className="truncate font-mono">{where}</span> : null}
          {device ? <span className="truncate">{device}</span> : null}
          {!online && evt.duration ? <span>在线 {formatDuration(evt.duration)}</span> : null}
        </div>
      </div>
      <span className="shrink-0 pt-0.5 text-[11px] text-muted-foreground tabular-nums" title={formatClockSeconds(evt.at)}>
        {since(evt.at, now)}
      </span>
    </li>
  );
}
