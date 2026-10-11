"use client";

import { Bug, Database, MemoryStick, Server } from "lucide-react";
import { CrashLogPanel } from "@/components/monitor/crash-log-panel";
import { DatabasePanel } from "@/components/monitor/database-panel";
import { MemoryPanel } from "@/components/monitor/memory-panel";
import { SystemInfoPanel } from "@/components/monitor/system-info-panel";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

const TABS = [
  { value: "host", label: "主机", icon: Server, render: () => <SystemInfoPanel /> },
  { value: "database", label: "数据库", icon: Database, render: () => <DatabasePanel /> },
  { value: "memory", label: "内存", icon: MemoryStick, render: () => <MemoryPanel /> },
  { value: "crash", label: "崩溃日志", icon: Bug, render: () => <CrashLogPanel /> }
] as const;

/**
 * 系统运行：原先四块面板在概览底部纵向堆叠，整页要滚好几屏。
 * 合进一张分页卡片后只挂载当前分页，未打开的面板也不再轮询。
 * 各接口要求超管，由调用方判定后再挂载。
 */
export function RuntimeSection() {
  return (
    <section aria-label="系统运行" className="rounded-xl border bg-card">
      <Tabs defaultValue="host" className="gap-0">
        <div className="flex flex-wrap items-center gap-3 border-b px-4 py-2.5">
          <h2 className="text-sm font-semibold">系统运行</h2>
          <TabsList className="ml-auto">
            {TABS.map((tab) => (
              <TabsTrigger key={tab.value} value={tab.value} className="px-3 text-xs">
                <tab.icon className="size-3.5" />
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        {TABS.map((tab) => (
          <TabsContent key={tab.value} value={tab.value} className="p-4 md:p-5">
            <WidgetBoundary title={`${tab.label}信息加载失败`}>{tab.render()}</WidgetBoundary>
          </TabsContent>
        ))}
      </Tabs>
    </section>
  );
}
