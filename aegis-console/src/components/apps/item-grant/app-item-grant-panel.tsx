"use client";

import { useState } from "react";
import { Gauge, History, PackagePlus, ReceiptText, Workflow } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { ItemGrantOverview } from "@/components/apps/item-grant/item-grant-overview";
import { ItemGrantRecordsPanel } from "@/components/apps/item-grant/item-grant-records-panel";
import { ItemGrantRulesPanel } from "@/components/apps/item-grant/item-grant-rules-panel";
import { ItemGrantRunsPanel } from "@/components/apps/item-grant/item-grant-runs-panel";
import { TooltipProvider } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 物品发放区块（docs/item-grant.md）。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 规则 | 有哪些规则、怎么触发、发了多少、正在执行到哪；新建（空白或模板）、进入画布编辑、上线 / 暂停 / 执行 / 归档 |
 * | 总览 | 生效规则、累计发放与覆盖人数、待处理与失败、趋势、近 30 天发了什么 |
 * | 发放记录 | 谁在什么时候从哪条规则拿到了什么、判定路径；人工发放在这里标记，失败的在这里重试 |
 * | 执行记录 | 每次手动 / 定时执行的进度、到账 / 跳过 / 失败人数与耗时 |
 *
 * 视图状态不进 URL：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "rules", label: "规则", icon: Workflow },
  { key: "overview", label: "总览", icon: Gauge },
  { key: "records", label: "发放记录", icon: ReceiptText },
  { key: "runs", label: "执行记录", icon: History }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppItemGrantPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("rules");
  const [ruleId, setRuleId] = useState<number | undefined>(undefined);
  const [runId, setRunId] = useState<number | undefined>(undefined);
  const [recordsKey, setRecordsKey] = useState(0);
  const [initialStatus, setInitialStatus] = useState<string>("");

  if (!appKey) {
    return <NoAppSelected icon={<PackagePlus className="size-5" />} />;
  }

  const openRecords = (nextRuleId?: number, nextRunId?: number, status = "") => {
    setRuleId(nextRuleId);
    setRunId(nextRunId);
    setInitialStatus(status);
    setRecordsKey((key) => key + 1);
    setView("records");
  };

  return (
    <TooltipProvider delayDuration={300}>
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
            </button>
          ))}
        </div>

        {view === "rules" ? (
          <ItemGrantRulesPanel
            appKey={appKey}
            onOpenRecords={(id) => openRecords(id)}
            onOpenRuns={(id) => {
              setRuleId(id);
              setView("runs");
            }}
          />
        ) : null}
        {view === "overview" ? (
          <ItemGrantOverview
            appKey={appKey}
            onOpenPending={() => openRecords(undefined, undefined, "pending")}
            onOpenFailed={() => openRecords(undefined, undefined, "failed")}
          />
        ) : null}
        {view === "records" ? (
          <ItemGrantRecordsPanel
            key={recordsKey}
            appKey={appKey}
            ruleId={ruleId}
            runId={runId}
            initialStatus={initialStatus}
            onRuleChange={setRuleId}
            onRunChange={setRunId}
          />
        ) : null}
        {view === "runs" ? (
          <ItemGrantRunsPanel
            appKey={appKey}
            ruleId={ruleId}
            onRuleChange={setRuleId}
            onOpenRecords={(nextRunId, nextRuleId) => openRecords(nextRuleId, nextRunId)}
          />
        ) : null}
      </div>
    </TooltipProvider>
  );
}
