"use client";

import { createContext, useCallback, useContext, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { useAuthStore } from "@/lib/auth-store";
import { LiveRequestError, OnlineLiveClient } from "./client";
import type {
  LiveAdminPresence,
  LiveAdmins,
  LiveRange,
  LiveSeries,
  LiveSeriesPoint,
  LiveSnapshot,
  LiveState,
  LiveUsers
} from "./types";

/**
 * 在线统计页面的 React 绑定。
 *
 * Provider 持有一条加密长连接（页面卸载即断开）；各组件用 useLive(selector)
 * 只订阅自己用到的那一片状态。selector 必须返回状态里已有的引用或原始值，
 * 不能现场拼新对象，否则 useSyncExternalStore 会认为每次都变了。
 */

const LiveContext = createContext<OnlineLiveClient | null>(null);

const IDLE: LiveState = { status: "idle", feed: [], dropped: 0 };

export function OnlineLiveProvider({ children }: { children: ReactNode }) {
  const token = useAuthStore((s) => s.accessToken);
  const [client] = useState(() => new OnlineLiveClient());
  useEffect(() => {
    if (token) client.connect(token);
    else client.disconnect();
  }, [client, token]);
  useEffect(() => () => client.disconnect(), [client]);
  return <LiveContext.Provider value={client}>{children}</LiveContext.Provider>;
}

export function useLiveClient() {
  const client = useContext(LiveContext);
  if (!client) throw new Error("useLiveClient 必须在 OnlineLiveProvider 内使用");
  return client;
}

export function useLive<T>(selector: (state: LiveState) => T): T {
  const client = useLiveClient();
  return useSyncExternalStore(
    client.subscribe,
    () => selector(client.getState()),
    () => selector(IDLE)
  );
}

/** 设定当前视图关注的应用（0 为概览），卸载时不回退：下一个视图会自己设定。 */
export function useLiveFocus(app: number) {
  const client = useLiveClient();
  useEffect(() => {
    client.setSubscription({ app, feed: true });
  }, [client, app]);
}

// ── 在线曲线 ──

export type SeriesState = {
  app: number;
  range: LiveRange;
  step: number;
  windowSec: number;
  points: LiveSeriesPoint[];
};

const RANGE_SECONDS: Record<LiveRange, number> = { "1h": 3600, "6h": 21600, "24h": 86400, "7d": 604800 };

/**
 * 历史部分向服务端要一次；之后每份快照把当前值并进最后一个桶（桶内取峰值），
 * 跨桶就追加新点并丢掉窗口外的旧点。十分钟整体重拉一次，纠正累积偏差。
 */
export function useLiveSeries(app: number, range: LiveRange) {
  const client = useLiveClient();
  const status = useLive((s) => s.status);
  const [series, setSeries] = useState<SeriesState | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (status !== "open") return;
    let cancelled = false;
    const load = () => {
      client
        .request<LiveSeries>({ t: "series", app, range })
        .then((res) => {
          if (cancelled) return;
          setError(null);
          setSeries({ app, range, step: res.step, windowSec: RANGE_SECONDS[range], points: res.points ?? [] });
        })
        .catch((err: unknown) => {
          if (!cancelled) setError(err instanceof LiveRequestError ? err.message : "读取在线曲线失败");
        });
    };
    load();
    const timer = setInterval(load, 10 * 60_000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [client, status, app, range]);

  useEffect(
    () =>
      client.onFrame((frame) => {
        if (frame.t !== "snapshot") return;
        const snap = frame as unknown as LiveSnapshot;
        let users: number;
        let connections: number;
        if (app === 0) {
          users = snap.totals.users;
          connections = snap.totals.connections;
        } else if (snap.focus?.appid === app) {
          users = snap.focus.users;
          connections = snap.focus.connections;
        } else {
          return;
        }
        const admins = app === 0 ? snap.admins?.admins ?? 0 : 0;
        const at = Math.floor(new Date(snap.at).getTime() / 1000);
        setSeries((prev) => {
          if (!prev || prev.app !== app || prev.range !== range) return prev;
          return appendPoint(prev, at, users, connections, admins);
        });
      }),
    [client, app, range]
  );

  const current = series && series.app === app && series.range === range ? series : null;
  return { series: current, loading: !current && !error, error };
}

function appendPoint(prev: SeriesState, at: number, users: number, connections: number, admins: number): SeriesState {
  const bucket = at - (at % prev.step);
  const points = prev.points.slice();
  const last = points[points.length - 1];
  if (last && last[0] === bucket) {
    points[points.length - 1] = [bucket, Math.max(last[1], users), Math.max(last[2], connections), Math.max(last[3], admins)];
  } else if (!last || bucket > last[0]) {
    points.push([bucket, users, connections, admins]);
  } else {
    return prev;
  }
  const cutoff = at - prev.windowSec;
  while (points.length > 0 && points[0][0] < cutoff) points.shift();
  return { ...prev, points };
}

// ── 应用在线用户 ──

/**
 * 应用在线用户列表：参数变化时请求；之后随快照里该应用的人数与连接数变化刷新
 * （至多三秒一次），另有三十秒的兜底刷新覆盖「人数不变但换了人」的情况。
 */
export function useLiveUsers(app: number | null, page: number, limit: number, q: string) {
  const client = useLiveClient();
  const status = useLive((s) => s.status);
  const focusUsers = useLive((s) => (app && s.snapshot?.focus?.appid === app ? s.snapshot.focus.users : -1));
  const focusConns = useLive((s) => (app && s.snapshot?.focus?.appid === app ? s.snapshot.focus.connections : -1));
  const [data, setData] = useState<LiveUsers | null>(null);
  const [error, setError] = useState<string | null>(null);
  const lastFetch = useRef(0);
  // 只采纳最近一次请求的响应：翻页、改关键字时，慢的旧响应不能覆盖新的
  const latest = useRef(0);

  const load = useCallback(() => {
    if (!app || client.getState().status !== "open") return;
    const ticket = ++latest.current;
    lastFetch.current = Date.now();
    client
      .request<LiveUsers>({ t: "users", app, page, limit, q })
      .then((res) => {
        if (ticket !== latest.current) return;
        setError(null);
        setData(res);
      })
      .catch((err: unknown) => {
        if (ticket === latest.current) setError(err instanceof LiveRequestError ? err.message : "读取在线用户失败");
      });
  }, [client, app, page, limit, q]);

  useEffect(() => {
    if (status === "open") load();
  }, [status, load]);

  useEffect(() => {
    if (status !== "open" || focusUsers < 0) return;
    const wait = Math.max(0, 3000 - (Date.now() - lastFetch.current));
    const timer = setTimeout(load, wait);
    return () => clearTimeout(timer);
    // 只在人数或连接数变化时触发
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusUsers, focusConns]);

  useEffect(() => {
    if (status !== "open") return;
    const timer = setInterval(load, 30_000);
    return () => clearInterval(timer);
  }, [status, load]);

  // 翻页、换关键字时保留上一屏，直到新数据到达，不闪骨架；只有换了应用才清空
  const current = data?.list.appid === app ? data : null;
  // 手上这屏与当前参数不符，说明新的请求还在路上
  const fetching = Boolean(current) && (current!.q !== q || current!.list.page !== page || current!.list.limit !== limit);
  return { data: current, loading: !current && !error && Boolean(app), fetching, error, refresh: load };
}

// ── 控制台在线的管理员（仅超管） ──

export function useLiveAdmins(enabled: boolean) {
  const client = useLiveClient();
  const status = useLive((s) => s.status);
  const [items, setItems] = useState<LiveAdminPresence[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    if (!enabled || client.getState().status !== "open") return;
    client
      .request<LiveAdmins>({ t: "admins" })
      .then((res) => {
        setError(null);
        setItems(res.items ?? []);
      })
      .catch((err: unknown) => setError(err instanceof LiveRequestError ? err.message : "读取在线管理员失败"));
  }, [client, enabled]);

  useEffect(() => {
    if (!enabled || status !== "open") return;
    load();
    const timer = setInterval(load, 15_000);
    // 有管理员上下线时立即刷新
    const off = client.onFrame((frame) => {
      if (frame.t !== "feed") return;
      const events = (frame.events as { admin?: boolean }[] | null) ?? [];
      if (events.some((e) => e.admin)) load();
    });
    return () => {
      clearInterval(timer);
      off();
    };
  }, [client, enabled, status, load]);

  return { items, loading: enabled && items === null && !error, error, refresh: load };
}
