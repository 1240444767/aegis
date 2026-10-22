"use client";

import { useEffect, useMemo, useState } from "react";
import { Activity, Coins, Gauge, Pause, Play, ReceiptText, Route, Timer, Waypoints, X, Zap } from "lucide-react";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { AIGatewayRequestSheet } from "@/components/apps/ai-gateway/ai-gateway-request-sheet";
import {
  ENDPOINT_LABEL,
  PulseDot,
  RequestStatusBadge,
  STATUS_LABEL,
  describeUsage,
  formatDuration,
  formatPoints,
  formatTokens
} from "@/components/apps/ai-gateway/ai-gateway-shared";
import { StatTile } from "@/components/content/content-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  useAIGatewayLiveFeed,
  useAIGatewayRequestSummaryQuery,
  useAIGatewayRequestsQuery,
  type AIGatewayLiveStatus
} from "@/lib/ai-gateway-hooks";
import type { AIGatewayLiveSummary, AIGatewayRequest, AIGatewayRequestStatus } from "@/lib/api/ai-gateway";
import { formatDateTime, parseInstant } from "@/lib/datetime";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

const PAGE_SIZE = 20;
const ALL = "__all__";
const RANGES = [
  { minutes: 15, label: "15 分钟" },
  { minutes: 60, label: "1 小时" },
  { minutes: 1440, label: "24 小时" }
] as const;
/** 实时连接断开时退回的刷新间隔。 */
const FALLBACK_REFRESH_MS = 10_000;

const LIVE_LABEL: Record<AIGatewayLiveStatus, string> = {
  connecting: "正在连接",
  live: "实时",
  offline: "已断开，每 10 秒刷新",
  paused: "已暂停",
  unauthorized: "无权订阅实时推送"
};

/** 每秒走一次的时钟：有进行中的调用时才开。 */
function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const tick = () => setNow(Date.now());
    const first = setTimeout(tick, 0);
    const timer = setInterval(tick, 1000);
    return () => {
      clearTimeout(first);
      clearInterval(timer);
    };
  }, [active]);
  return now;
}

export function AIGatewayRequestsPanel({
  appKey,
  initialFilter
}: {
  appKey: string;
  initialFilter: { userId?: number; keyId?: number; model?: string };
}) {
  const [user, setUser] = useState<PickedUser | null>(initialFilter.userId ? { id: initialFilter.userId } : null);
  const [keyId, setKeyId] = useState<number | undefined>(initialFilter.keyId);
  const [channel, setChannel] = useState<{ id: number; name: string } | null>(null);
  const [model, setModel] = useState(initialFilter.model ?? "");
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState<string>(ALL);
  const [endpoint, setEndpoint] = useState<string>(ALL);
  const [page, setPage] = useState(1);
  const [minutes, setMinutes] = useState(60);
  const [paused, setPaused] = useState(false);
  const [selected, setSelected] = useState<AIGatewayRequest | null>(null);
  const debouncedModel = useDebouncedValue(model.trim(), 300);
  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);

  const live = useAIGatewayLiveFeed(appKey, paused);
  const polling = live.status === "live" ? false : FALLBACK_REFRESH_MS;
  const query = useAIGatewayRequestsQuery(
    appKey,
    {
      userId: user?.id,
      keyId,
      configId: channel?.id,
      model: debouncedModel || undefined,
      keyword: debouncedKeyword || undefined,
      status: status === ALL ? undefined : status,
      endpoint: endpoint === ALL ? undefined : endpoint,
      page,
      limit: PAGE_SIZE
    },
    paused ? false : polling
  );
  const summaryQuery = useAIGatewayRequestSummaryQuery(appKey, minutes, live.status === "live" ? 60_000 : FALLBACK_REFRESH_MS);
  const items = useMemo(() => query.data?.items ?? [], [query.data]);
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const now = useNow(items.some((item) => item.status === "reserved") || selected?.status === "reserved");

  const resetPage = () => setPage(1);
  const pickChannel = (id: number, name: string) => {
    setChannel(id ? { id, name } : null);
    resetPage();
  };

  return (
    <div className="space-y-4">
      <LiveSummaryCard
        summary={summaryQuery.data}
        loading={summaryQuery.isLoading}
        minutes={minutes}
        onMinutes={setMinutes}
        onPickChannel={pickChannel}
      />

      <SectionCard
        icon={<ReceiptText className="size-4" />}
        title="调用记录"
        description="每一次调用的上游链路、用量、耗时与扣费；进行中的调用实时更新"
        aside={<LiveIndicator status={live.status} paused={paused} onToggle={() => setPaused((value) => !value)} />}
      >
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <div className="w-60">
            <UserPicker appKey={appKey} value={user} onChange={(value) => { setUser(value); resetPage(); }} />
          </div>
          <Input value={keyword} placeholder="调用编号 / 上游请求编号" className="h-8 w-52 font-mono text-xs"
            onChange={(event) => { setKeyword(event.target.value); resetPage(); }} />
          <Input value={model} placeholder="模型" className="h-8 w-36 font-mono text-xs"
            onChange={(event) => { setModel(event.target.value); resetPage(); }} />
          <Select value={status} onValueChange={(value) => { setStatus(value); resetPage(); }}>
            <SelectTrigger className="h-8 w-32 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部状态</SelectItem>
              {(Object.keys(STATUS_LABEL) as AIGatewayRequestStatus[]).map((key) => (
                <SelectItem key={key} value={key}>
                  {STATUS_LABEL[key]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={endpoint} onValueChange={(value) => { setEndpoint(value); resetPage(); }}>
            <SelectTrigger className="h-8 w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>全部接口</SelectItem>
              {Object.entries(ENDPOINT_LABEL).map(([key, label]) => (
                <SelectItem key={key} value={key}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {keyId ? (
            <FilterBadge label={`密钥 #${keyId}`} onClear={() => { setKeyId(undefined); resetPage(); }} />
          ) : null}
          {channel ? <FilterBadge label={`通道 ${channel.name}`} onClear={() => pickChannel(0, "")} /> : null}
          <span className="ml-auto text-xs text-muted-foreground tabular-nums">共 {total} 条</span>
        </div>

        {query.isLoading ? (
          <Skeleton className="h-60 w-full rounded-xl" />
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-10 text-center text-xs text-muted-foreground">
            暂无调用记录
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>用户</TableHead>
                <TableHead>模型</TableHead>
                <TableHead>上游</TableHead>
                <TableHead>用量</TableHead>
                <TableHead className="text-right">耗时</TableHead>
                <TableHead className="text-right">扣费（点）</TableHead>
                <TableHead>状态</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <RequestRow
                  key={item.requestNo}
                  item={item}
                  now={now}
                  fresh={live.fresh.has(item.requestNo)}
                  onOpen={() => setSelected(item)}
                  onPickUser={() => { setUser({ id: item.userId, account: item.account }); resetPage(); }}
                  onPickChannel={() => item.configId && pickChannel(item.configId, item.configName || item.provider || `#${item.configId}`)}
                />
              ))}
            </TableBody>
          </Table>
        )}

        {totalPages > 1 ? (
          <div className="mt-3 flex items-center justify-end gap-2 text-xs">
            <Button size="xs" variant="outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              上一页
            </Button>
            <span className="tabular-nums text-muted-foreground">
              {page} / {totalPages}
            </span>
            <Button size="xs" variant="outline" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
              下一页
            </Button>
          </div>
        ) : null}
      </SectionCard>

      <AIGatewayRequestSheet
        appKey={appKey}
        record={selected ? (items.find((item) => item.requestNo === selected.requestNo) ?? selected) : null}
        now={now}
        onClose={() => setSelected(null)}
      />
    </div>
  );
}

function FilterBadge({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <Badge variant="outline" className="gap-1">
      {label}
      <button type="button" aria-label={`清除${label}筛选`} onClick={onClear}>
        <X className="size-3" />
      </button>
    </Badge>
  );
}

function LiveIndicator({ status, paused, onToggle }: { status: AIGatewayLiveStatus; paused: boolean; onToggle: () => void }) {
  const tone =
    status === "live"
      ? "text-emerald-600 dark:text-emerald-400"
      : status === "offline" || status === "unauthorized"
        ? "text-amber-600 dark:text-amber-400"
        : "text-muted-foreground";
  return (
    <div className="flex items-center gap-2">
      <span className={cn("flex items-center gap-1.5 text-xs", tone)}>
        {status === "live" ? <PulseDot /> : <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />}
        {LIVE_LABEL[status]}
      </span>
      <Button size="xs" variant="outline" onClick={onToggle} aria-label={paused ? "继续实时更新" : "暂停实时更新"}>
        {paused ? <Play className="size-3" /> : <Pause className="size-3" />}
        {paused ? "继续" : "暂停"}
      </Button>
    </div>
  );
}

function RequestRow({
  item,
  now,
  fresh,
  onOpen,
  onPickUser,
  onPickChannel
}: {
  item: AIGatewayRequest;
  now: number;
  fresh: boolean;
  onOpen: () => void;
  onPickUser: () => void;
  onPickChannel: () => void;
}) {
  const running = item.status === "reserved";
  const created = parseInstant(item.createdAt)?.getTime() ?? now;
  const elapsed = running ? Math.max(now - created, 0) : item.latencyMs;
  const firstToken = running ? item.live?.firstTokenMs : item.firstTokenMs;
  const channel = item.configName || item.provider;

  return (
    <TableRow
      className={cn("cursor-pointer transition-colors duration-700", fresh && "bg-emerald-500/10")}
      onClick={onOpen}
    >
      <TableCell className="whitespace-nowrap text-xs tabular-nums">{formatDateTime(item.createdAt)}</TableCell>
      <TableCell className="text-xs">
        <button type="button" className="hover:underline" onClick={(event) => { event.stopPropagation(); onPickUser(); }}>
          {item.account || `#${item.userId}`}
        </button>
      </TableCell>
      <TableCell>
        <div className="font-mono text-xs">{item.model}</div>
        <div className="text-[11px] text-muted-foreground">
          {ENDPOINT_LABEL[item.endpoint] ?? item.endpoint}
          {item.stream ? " · 流式" : ""}
        </div>
      </TableCell>
      <TableCell className="text-xs">
        {channel ? (
          <button type="button" className="hover:underline" onClick={(event) => { event.stopPropagation(); onPickChannel(); }}>
            {channel}
          </button>
        ) : (
          <span className="text-muted-foreground">{running ? "调度中" : "—"}</span>
        )}
        <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
          {item.upstreamModel && item.upstreamModel !== item.model ? (
            <span className="max-w-36 truncate font-mono">{item.upstreamModel}</span>
          ) : null}
          {item.attempts > 1 ? (
            <Badge variant="warning" size="sm">
              回退 {item.attempts - 1} 次
            </Badge>
          ) : null}
        </div>
      </TableCell>
      <TableCell className="text-xs tabular-nums">
        {running ? (
          item.live?.outputTokens ? (
            <span className="text-sky-600 dark:text-sky-400">已生成约 {formatTokens(item.live.outputTokens)}</span>
          ) : (
            <span className="text-muted-foreground">等待输出</span>
          )
        ) : (
          <>
            {describeUsage(item)}
            {item.usageEstimated ? <span className="ml-1 text-muted-foreground">（估）</span> : null}
          </>
        )}
      </TableCell>
      <TableCell className="text-right text-xs tabular-nums">
        <div className={cn(running && "text-sky-600 dark:text-sky-400")}>{formatDuration(elapsed)}</div>
        {firstToken ? <div className="text-[11px] text-muted-foreground">首字 {formatDuration(firstToken)}</div> : null}
      </TableCell>
      <TableCell className="text-right font-mono text-xs tabular-nums">
        {running ? <span className="text-muted-foreground">暂扣 {formatPoints(item.reserved)}</span> : formatPoints(item.charged)}
      </TableCell>
      <TableCell>
        <RequestStatusBadge status={item.status} />
        {item.errorMessage && !running ? (
          <div className="mt-0.5 max-w-40 truncate text-[11px] text-destructive" title={item.errorMessage}>
            {item.errorMessage}
          </div>
        ) : null}
      </TableCell>
    </TableRow>
  );
}

function LiveSummaryCard({
  summary,
  loading,
  minutes,
  onMinutes,
  onPickChannel
}: {
  summary?: AIGatewayLiveSummary;
  loading: boolean;
  minutes: number;
  onMinutes: (minutes: number) => void;
  onPickChannel: (id: number, name: string) => void;
}) {
  const successRate = summary && summary.total > 0 ? Math.round((summary.succeeded / summary.total) * 1000) / 10 : null;
  const range = RANGES.find((item) => item.minutes === minutes)?.label ?? `${minutes} 分钟`;

  return (
    <SectionCard
      icon={<Activity className="size-4" />}
      title="实时概况"
      description={`最近 ${range}的调用，以及每条上游通道的尝试情况`}
      aside={
        <ToggleGroup type="single" size="sm" variant="outline" value={String(minutes)}
          onValueChange={(value) => value && onMinutes(Number(value))}>
          {RANGES.map((item) => (
            <ToggleGroupItem key={item.minutes} value={String(item.minutes)} className="px-2.5 text-xs">
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      }
    >
      {loading && !summary ? (
        <Skeleton className="h-24 w-full rounded-xl" />
      ) : (
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-6">
            <StatTile label="进行中" icon={Zap} value={summary?.inFlight ?? "—"}
              tone={summary?.inFlight ? "positive" : "default"} />
            <StatTile label="调用" icon={Activity} value={summary?.total ?? "—"}
              hint={successRate !== null ? `成功率 ${successRate}%，失败 ${summary?.failed ?? 0}` : undefined} />
            <StatTile label="平均耗时" icon={Timer} value={formatDuration(summary?.avgLatencyMs)}
              hint={summary?.p95LatencyMs ? `P95 ${formatDuration(summary.p95LatencyMs)}` : undefined} />
            <StatTile label="平均首字" icon={Gauge} value={formatDuration(summary?.avgFirstTokenMs)} hint="流式调用" />
            <StatTile label="Token" icon={Waypoints}
              value={summary ? formatTokens(summary.inputTokens + summary.outputTokens) : "—"}
              hint={summary ? `输入 ${formatTokens(summary.inputTokens)}，输出 ${formatTokens(summary.outputTokens)}` : undefined} />
            <StatTile label="扣费（点）" icon={Coins} value={formatPoints(summary?.charged)}
              hint={summary ? `${summary.users} 位用户，回退 ${summary.retried} 次` : undefined} />
          </div>

          {summary && summary.upstreams.length > 0 ? (
            <div className="overflow-hidden rounded-xl border border-border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>
                      <span className="flex items-center gap-1.5">
                        <Route className="size-3.5" />
                        上游通道
                      </span>
                    </TableHead>
                    <TableHead className="text-right">尝试</TableHead>
                    <TableHead className="text-right">失败率</TableHead>
                    <TableHead className="text-right">平均耗时</TableHead>
                    <TableHead>最近错误</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {summary.upstreams.map((item) => {
                    const failRate = item.attempts > 0 ? Math.round((item.failed / item.attempts) * 1000) / 10 : 0;
                    return (
                      <TableRow
                        key={`${item.configId}-${item.configName}-${item.provider}`}
                        className={cn(item.configId ? "cursor-pointer" : undefined)}
                        onClick={() => item.configId && onPickChannel(item.configId, item.configName || item.provider)}
                      >
                        <TableCell className="text-xs">
                          {item.configName || "—"}
                          {item.provider ? <span className="ml-1.5 text-muted-foreground">{item.provider}</span> : null}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">{item.attempts}</TableCell>
                        <TableCell
                          className={cn(
                            "text-right text-xs tabular-nums",
                            failRate >= 20 ? "text-destructive" : failRate > 0 ? "text-amber-600 dark:text-amber-400" : undefined
                          )}
                        >
                          {failRate}%
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">{formatDuration(item.avgLatencyMs)}</TableCell>
                        <TableCell className="max-w-64 truncate text-xs text-muted-foreground" title={item.lastError}>
                          {item.lastError || "—"}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          ) : null}
        </div>
      )}
    </SectionCard>
  );
}
