"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { BarChart3, FlaskConical, KeyRound, Layers, ListChecks, Settings2, Smartphone } from "lucide-react";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useAppScopeStore } from "@/lib/app-scope-store";
import { useClientControlOverviewQuery, useClientControlSettingsQuery } from "@/lib/client-control-hooks";
import { ClientControlDevices, ClientControlVersions, type DeviceFilter } from "@/components/client-control/client-control-installs";
import { ClientControlKeys } from "@/components/client-control/client-control-keys";
import { ClientControlOverview } from "@/components/client-control/client-control-overview";
import { ClientControlRules } from "@/components/client-control/client-control-rules";
import { ClientControlSettingsPanel } from "@/components/client-control/client-control-settings";
import { ClientControlSimulator } from "@/components/client-control/client-control-simulator";
import { Badge } from "@/components/ui/badge";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

/**
 * 版本管控。客户端启动与回到前台时加密上报包名、版本、签名证书等身份，
 * 服务端按这里的规则签发裁决：放行、提醒、强制更新、停用。契约见 docs/client-control.md。
 */
const TABS = ["overview", "rules", "versions", "devices", "simulate", "keys", "settings"] as const;
type ControlTab = (typeof TABS)[number];

function resolveTab(value?: string | null): ControlTab {
  return TABS.includes(value as ControlTab) ? (value as ControlTab) : "overview";
}

function ClientControlPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = resolveTab(searchParams.get("tab"));
  const switchTab = (value: string) => router.replace(`/client-control?tab=${value}`, { scroll: false });

  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const { lastAppKey, setLastAppKey } = useAppScopeStore();
  const [picked, setPicked] = useState<string | null>(lastAppKey);
  const appKey = useMemo(() => {
    if (picked && apps.some((item) => item.appKey === picked)) return picked;
    return apps[0]?.appKey ?? "";
  }, [picked, apps]);

  const overviewQuery = useClientControlOverviewQuery(appKey || null);
  const settingsQuery = useClientControlSettingsQuery(appKey || null);
  const settings = settingsQuery.data;
  const [deviceFilter, setDeviceFilter] = useState<DeviceFilter>({});

  if (appsQuery.isLoading) {
    return <LoadingState title="正在加载版本管控" />;
  }
  if (!appKey) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="Client Control" title="版本管控" description="当前没有可管理的应用。" />
        <EmptyState title="暂无应用" />
      </div>
    );
  }

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="Client Control"
        title="版本管控"
        description="按包名、版本、签名与设备下发放行、提醒、强制更新或停用。"
        action={
          <div className="flex items-center gap-2">
            {settings ? (
              settings.enabled ? (
                <Badge variant="success">已启用</Badge>
              ) : (
                <Badge variant="secondary">未启用</Badge>
              )
            ) : null}
            <Select
              value={appKey}
              onValueChange={(value) => {
                setPicked(value);
                setLastAppKey(value);
                setDeviceFilter({});
              }}
            >
              <SelectTrigger className="w-56">
                <SelectValue placeholder="选择应用" />
              </SelectTrigger>
              <SelectContent>
                {apps.map((item) => (
                  <SelectItem key={item.appKey} value={item.appKey || String(item.id)}>
                    {item.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
      />

      <Tabs value={tab} onValueChange={switchTab}>
        <TabsList className="flex-wrap">
          <TabsTrigger value="overview">
            <BarChart3 className="size-3.5" />
            概览
          </TabsTrigger>
          <TabsTrigger value="rules">
            <ListChecks className="size-3.5" />
            规则
          </TabsTrigger>
          <TabsTrigger value="versions">
            <Layers className="size-3.5" />
            版本分布
          </TabsTrigger>
          <TabsTrigger value="devices">
            <Smartphone className="size-3.5" />
            设备
          </TabsTrigger>
          <TabsTrigger value="simulate">
            <FlaskConical className="size-3.5" />
            模拟
          </TabsTrigger>
          <TabsTrigger value="keys">
            <KeyRound className="size-3.5" />
            密钥
          </TabsTrigger>
          <TabsTrigger value="settings">
            <Settings2 className="size-3.5" />
            设置
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="pt-4">
          <ClientControlOverview overview={overviewQuery.data} loading={overviewQuery.isLoading} />
        </TabsContent>
        <TabsContent value="rules" className="pt-4">
          <ClientControlRules appKey={appKey} />
        </TabsContent>
        <TabsContent value="versions" className="pt-4">
          <ClientControlVersions
            appKey={appKey}
            officialCerts={settings?.allowedCerts ?? []}
            onShowDevices={(filter) => {
              setDeviceFilter(filter);
              switchTab("devices");
            }}
          />
        </TabsContent>
        <TabsContent value="devices" className="pt-4">
          <ClientControlDevices appKey={appKey} filter={deviceFilter} onClearFilter={() => setDeviceFilter({})} />
        </TabsContent>
        <TabsContent value="simulate" className="pt-4">
          <ClientControlSimulator appKey={appKey} />
        </TabsContent>
        <TabsContent value="keys" className="pt-4">
          <ClientControlKeys appKey={appKey} />
        </TabsContent>
        <TabsContent value="settings" className="pt-4">
          {settings ? (
            <ClientControlSettingsPanel key={`${appKey}-${settings.policyVersion}`} appKey={appKey} settings={settings} />
          ) : (
            <Skeleton className="h-96 max-w-3xl rounded-xl" />
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}

export default function ClientControlPage() {
  return (
    <Suspense fallback={null}>
      <ClientControlPageInner />
    </Suspense>
  );
}
