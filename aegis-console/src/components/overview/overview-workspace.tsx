"use client";

import Link from "next/link";
import { AlertTriangle, ArrowRight, CheckCircle2, ClipboardList, ListChecks, Mail, Shield, ShieldAlert } from "lucide-react";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { formatCompact, formatDateTime } from "@/lib/datetime";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/* 审计明细形如 `GET /api/admin/auth/me → 200 (0ms)`，解析失败时按原文展示。 */
const DETAIL_RE = /^([A-Z]+)\s+(\S+)\s*(?:→|->)\s*(\d{3})(?:\s*\((\d+)\s*ms\))?/;

function parseDetail(detail: string) {
  const matched = DETAIL_RE.exec(detail.trim());
  if (!matched) return null;
  return {
    method: matched[1]!,
    path: matched[2]!,
    status: Number(matched[3]),
    latency: matched[4] ? Number(matched[4]) : undefined
  };
}

function methodClass(method: string) {
  const key = method.toLowerCase();
  return ["post", "put", "delete", "get"].includes(key) ? `chip-method--${key}` : "chip-method--other";
}

function statusClass(status: number) {
  if (status >= 500) return "status-code-5xx";
  if (status >= 400) return "status-code-4xx";
  if (status >= 300) return "status-code-3xx";
  if (status >= 200) return "status-code-success";
  return "status-code-idle";
}

/** 审计路径里 UUID 在中间、动作名在末尾，截断时保头保尾。 */
function elidePath(path: string, max = 52) {
  if (path.length <= max) return path;
  const tail = Math.floor((max - 1) * 0.62);
  return `${path.slice(0, max - 1 - tail)}…${path.slice(-tail)}`;
}

function PanelShell({
  icon: Icon,
  title,
  action,
  children,
  className
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("flex min-h-[280px] flex-col rounded-xl border bg-card", className)}>
      <header className="flex h-12 items-center gap-2 border-b px-4">
        <Icon className="size-4 text-muted-foreground" />
        <h2 className="text-sm font-semibold">{title}</h2>
        <div className="ml-auto flex items-center gap-2">{action}</div>
      </header>
      <div className="flex flex-1 flex-col p-2">{children}</div>
    </section>
  );
}

function PanelEmpty({ icon: Icon, title }: { icon: React.ComponentType<{ className?: string }>; title: string }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 py-10 text-center">
      <Icon className="size-6 text-muted-foreground/40" />
      <p className="text-xs text-muted-foreground">{title}</p>
    </div>
  );
}

const ALERT_TONE: Record<string, string> = {
  firewall_block: "text-red-600 dark:text-red-400",
  login_failed: "text-amber-600 dark:text-amber-400"
};

/**
 * 待处理：审批类待办与安全提醒合在一处。两者都是「需要有人去看一眼」，
 * 分成两块时没有提醒的那块常年空着。
 */
export function AttentionPanel() {
  const { data, isLoading } = useAdminDashboardQuery();

  const todos = [
    // 组织页的 Tab 只存在于组件 state，带 ?tab= 不会切面板，这里只能落到页面本身
    { icon: Mail, label: "部门邀请待处理", count: data?.pendingInvitations ?? 0, href: "/organization" },
    { icon: Shield, label: "角色申请待审核", count: data?.pendingRoleApps ?? 0, href: "/reviews" }
  ].filter((todo) => todo.count > 0);
  const alerts = data?.alerts ?? [];
  const total = todos.reduce((sum, todo) => sum + todo.count, 0) + alerts.length;

  return (
    <PanelShell
      icon={ListChecks}
      title="待处理"
      action={total > 0 ? <Badge variant="danger" size="sm">{total}</Badge> : null}
    >
      {isLoading ? (
        <div className="space-y-1.5 p-1">
          <Skeleton className="h-10 rounded-lg" />
          <Skeleton className="h-10 rounded-lg" />
          <Skeleton className="h-10 rounded-lg" />
        </div>
      ) : total === 0 ? (
        <PanelEmpty icon={CheckCircle2} title="暂无待处理事项" />
      ) : (
        <ul className="flex flex-col">
          {todos.map((todo) => (
            <li key={todo.href}>
              <Link
                href={todo.href}
                className="group/todo flex items-center gap-3 rounded-lg px-2.5 py-2.5 text-xs transition-colors hover:bg-muted/60"
              >
                <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                  <todo.icon className="size-3.5" />
                </span>
                <span className="flex-1 truncate font-medium">{todo.label}</span>
                <span className="font-data text-sm font-semibold tabular-nums">{todo.count}</span>
                <ArrowRight className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/todo:opacity-100" />
              </Link>
            </li>
          ))}
          {alerts.map((alert, index) => (
            <li key={`${alert.type}-${index}`}>
              <Link
                href="/security"
                className="group/todo flex items-start gap-3 rounded-lg px-2.5 py-2.5 text-xs transition-colors hover:bg-muted/60"
              >
                <span className={cn("flex size-7 shrink-0 items-center justify-center rounded-md bg-muted", ALERT_TONE[alert.type] ?? "text-muted-foreground")}>
                  {alert.type === "firewall_block" ? <ShieldAlert className="size-3.5" /> : <AlertTriangle className="size-3.5" />}
                </span>
                <span className="min-w-0 flex-1 space-y-0.5">
                  <span className="block truncate font-medium">{alert.title}</span>
                  <span className="block truncate text-muted-foreground">{alert.detail}</span>
                </span>
                <span className="shrink-0 font-data text-[11px] text-muted-foreground tabular-nums" title={formatDateTime(alert.time)}>
                  {formatCompact(alert.time, "")}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </PanelShell>
  );
}

export function RecentActivity({ className }: { className?: string }) {
  const { data, isLoading } = useAdminDashboardQuery();
  const logs = data?.recentAuditLogs ?? [];

  return (
    <PanelShell
      icon={ClipboardList}
      title="最近操作"
      className={className}
      action={
        <Link href="/audit" className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground">
          审计日志
          <ArrowRight className="size-3" />
        </Link>
      }
    >
      {isLoading ? (
        <div className="space-y-1.5 p-1">
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className="h-8 rounded-md" />
          ))}
        </div>
      ) : logs.length === 0 ? (
        <PanelEmpty icon={ClipboardList} title="暂无操作记录" />
      ) : (
        <ul className="flex flex-col">
          {logs.map((log, index) => {
            const parsed = parseDetail(log.detail || "");
            return (
              <li key={index} className="flex h-9 items-center gap-3 rounded-md px-2.5 text-xs transition-colors hover:bg-muted/50">
                {parsed ? (
                  <>
                    <span className={cn("chip-method w-14 shrink-0 justify-center", methodClass(parsed.method))}>{parsed.method}</span>
                    <span className="min-w-0 flex-1 truncate font-data text-[11px] text-muted-foreground" title={parsed.path}>
                      {elidePath(parsed.path)}
                    </span>
                    <span className={cn("shrink-0 font-data text-[11px] font-medium", statusClass(parsed.status))}>{parsed.status}</span>
                    <span className="hidden w-12 shrink-0 text-right font-data text-[11px] text-muted-foreground sm:block">
                      {typeof parsed.latency === "number" ? `${parsed.latency}ms` : ""}
                    </span>
                  </>
                ) : (
                  <>
                    <Badge variant="outline" size="sm" className="shrink-0 font-mono">
                      {log.action.split(".").pop()}
                    </Badge>
                    <span className="min-w-0 flex-1 truncate text-muted-foreground">{log.detail}</span>
                  </>
                )}
                <span className="w-20 shrink-0 text-right font-data text-[11px] text-muted-foreground tabular-nums" title={formatDateTime(log.createdAt)}>
                  {formatCompact(log.createdAt, "")}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </PanelShell>
  );
}
