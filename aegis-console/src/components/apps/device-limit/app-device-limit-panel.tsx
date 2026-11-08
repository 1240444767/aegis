"use client";

import { useState } from "react";
import { BarChart3, FlaskConical, ListTree, MonitorSmartphone, ShieldPlus, SlidersHorizontal, UserSearch } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { DeviceLimitEventsPanel } from "@/components/apps/device-limit/device-limit-events-panel";
import { DeviceLimitOverridesPanel } from "@/components/apps/device-limit/device-limit-overrides-panel";
import { DeviceLimitOverviewPanel } from "@/components/apps/device-limit/device-limit-overview-panel";
import { DeviceLimitPolicyPanel } from "@/components/apps/device-limit/device-limit-policy-panel";
import { defaultPolicy } from "@/components/apps/device-limit/device-limit-shared";
import { DeviceLimitSimulatorPanel } from "@/components/apps/device-limit/device-limit-simulator-panel";
import { DeviceLimitUsersPanel } from "@/components/apps/device-limit/device-limit-users-panel";
import type { PickedUser } from "@/components/commerce/user-picker";
import type { DeviceLimitPolicy } from "@/lib/api/device-limit";
import { useDeviceLimitPolicyQuery } from "@/lib/device-limit-hooks";
import { cn } from "@/lib/utils";

/**
 * 登录设备区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 开没开、上限多少、每天有多少设备被挤下线 / 被拒、设备怎么分布、谁最常撞上限 |
 * | 策略 | 总数、类别、会员档位、新设备频率、同设备与超限处置 |
 * | 用户设备 | 某个人此刻在哪几台设备上登录、上限出自哪一层、登录过哪些设备；下线、整理、例外 |
 * | 例外名单 | 谁不受限制或另配上限、何时到期 |
 * | 处置记录 | 每一次自动下线、拒绝、选择与人工下线 |
 * | 模拟 | 用草稿或已保存的策略，拿真实用户或虚构场景试一次登录 |
 *
 * 策略草稿放在这一层而不是策略视图里：模拟要能拿未保存的草稿试，切视图也不该丢改动。
 * 视图状态不进 URL，`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "policy", label: "策略", icon: SlidersHorizontal },
  { key: "users", label: "用户设备", icon: UserSearch },
  { key: "overrides", label: "例外名单", icon: ShieldPlus },
  { key: "events", label: "处置记录", icon: ListTree },
  { key: "simulate", label: "模拟", icon: FlaskConical }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppDeviceLimitPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");
  const [user, setUser] = useState<PickedUser | null>(null);
  const policyQuery = useDeviceLimitPolicyQuery(appKey);

  // 草稿按 appKey 绑定：没有本应用的草稿时从服务端派生，切换应用自动丢弃上一个应用的改动
  const [draft, setDraft] = useState<{ scope: string; value: DeviceLimitPolicy } | null>(null);
  const scope = appKey ?? "";
  const saved = policyQuery.data?.policy;
  const policy = draft?.scope === scope ? draft.value : (saved ?? defaultPolicy());
  const dirty = draft?.scope === scope && JSON.stringify(draft.value) !== JSON.stringify(saved ?? defaultPolicy());

  if (!appKey) {
    return <NoAppSelected icon={<MonitorSmartphone className="size-5" />} />;
  }

  const openUser = (next: PickedUser) => {
    setUser(next);
    setView("users");
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setView(item.key)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
              view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
            {item.key === "policy" && dirty ? <span className="size-1.5 rounded-full bg-amber-500" aria-label="有未保存的改动" /> : null}
          </button>
        ))}
      </div>

      {view === "overview" ? (
        <DeviceLimitOverviewPanel appKey={appKey} onOpenPolicy={() => setView("policy")} onOpenUser={openUser} />
      ) : null}
      {view === "policy" ? (
        <DeviceLimitPolicyPanel
          appKey={appKey}
          policy={policy}
          configured={policyQuery.data?.configured ?? false}
          loading={policyQuery.isLoading}
          dirty={dirty}
          onChange={(value) => setDraft({ scope, value })}
          onReset={() => setDraft(null)}
          onSaved={() => setDraft(null)}
        />
      ) : null}
      {view === "users" ? <DeviceLimitUsersPanel appKey={appKey} user={user} onUserChange={setUser} /> : null}
      {view === "overrides" ? <DeviceLimitOverridesPanel appKey={appKey} onOpenUser={openUser} /> : null}
      {view === "events" ? <DeviceLimitEventsPanel appKey={appKey} onOpenUser={openUser} /> : null}
      {view === "simulate" ? (
        <DeviceLimitSimulatorPanel appKey={appKey} draft={policy} dirty={dirty} />
      ) : null}
    </div>
  );
}
