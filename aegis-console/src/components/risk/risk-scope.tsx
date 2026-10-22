"use client";

import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Boxes, Globe2 } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectSeparator, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import type { AppSummary } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/* ══════════════════════════════════════════════════════════
   应用作用域

   页头一个选择器，作用于大盘、评估、复核、规则、策略、名单与账号画像。
   放在页面级而不是各面板里：它对每个页签都成立，分散到各面板会让人
   切页签时反复设置同一件事（与交易中心的时间窗同一约定）。

   作用域同步到 URL（`?app=`）：从别处深链过来时带着作用域，刷新也不丢。
   ══════════════════════════════════════════════════════════ */

type RiskScope = {
  /** undefined = 全平台（不按应用过滤） */
  appId: number | undefined;
  setAppId: (next: number | undefined) => void;
  apps: AppSummary[];
  appName: (id?: number | null) => string;
};

const RiskScopeContext = createContext<RiskScope | null>(null);

export function RiskScopeProvider({ children }: { children: ReactNode }) {
  const searchParams = useSearchParams();
  const router = useRouter();
  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);

  const raw = Number(searchParams.get("app"));
  const appId = Number.isFinite(raw) && raw > 0 ? raw : undefined;

  const setAppId = useCallback((next: number | undefined) => {
    const params = new URLSearchParams(searchParams.toString());
    if (next) params.set("app", String(next));
    else params.delete("app");
    router.replace(`/risk-control?${params.toString()}`, { scroll: false });
  }, [router, searchParams]);

  const value = useMemo<RiskScope>(() => {
    const names = new Map(apps.map((app) => [app.id, app.name]));
    return {
      appId,
      setAppId,
      apps,
      appName: (id) => (id == null ? "全平台" : names.get(id) ?? `应用 #${id}`),
    };
  }, [apps, appId, setAppId]);

  return <RiskScopeContext.Provider value={value}>{children}</RiskScopeContext.Provider>;
}

export function useRiskScope(): RiskScope {
  const ctx = useContext(RiskScopeContext);
  if (!ctx) throw new Error("useRiskScope 必须在 RiskScopeProvider 内使用");
  return ctx;
}

export function RiskScopeSelect({ className }: { className?: string }) {
  const { appId, setAppId, apps } = useRiskScope();
  return (
    <Select value={appId ? String(appId) : "all"} onValueChange={(v) => setAppId(v === "all" ? undefined : Number(v))}>
      <SelectTrigger className={cn("h-8 w-48 text-xs", className)}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">
          <span className="flex items-center gap-1.5"><Globe2 className="size-3.5" />全平台</span>
        </SelectItem>
        {apps.length > 0 && <SelectSeparator />}
        {apps.map((app) => (
          <SelectItem key={app.id} value={String(app.id)}>
            <span className="flex items-center gap-1.5"><Boxes className="size-3.5" />{app.name}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** 规则 / 策略 / 名单的作用域徽标：全平台是默认，只有应用级的才着色。 */
export function ScopeBadge({ appId, className }: { appId?: number | null; className?: string }) {
  const { appName } = useRiskScope();
  if (appId == null) {
    return <Badge variant="outline" size="sm" className={cn("text-muted-foreground", className)}>全平台</Badge>;
  }
  return <Badge variant="info" size="sm" className={className}>{appName(appId)}</Badge>;
}

/**
 * 编辑器里的作用域选择。value 为 null 表示全平台。
 */
export function ScopeField({ value, onChange, disabled }: {
  value: number | null; onChange: (next: number | null) => void; disabled?: boolean;
}) {
  const { apps } = useRiskScope();
  return (
    <Select value={value ? String(value) : "global"} disabled={disabled}
      onValueChange={(v) => onChange(v === "global" ? null : Number(v))}>
      <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
      <SelectContent>
        <SelectItem value="global">全平台</SelectItem>
        {apps.map((app) => <SelectItem key={app.id} value={String(app.id)}>{app.name}</SelectItem>)}
      </SelectContent>
    </Select>
  );
}
