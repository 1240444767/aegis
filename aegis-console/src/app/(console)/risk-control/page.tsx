"use client";

import { Suspense, useCallback, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Activity, Clock, Eye, FlaskConical, ListChecks, Radar, Shield,
} from "lucide-react";

import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RiskAssessmentsPanel } from "@/components/risk/risk-assessments";
import { RiskDashboardPanel } from "@/components/risk/risk-dashboard";
import { RiskEntitiesPanel } from "@/components/risk/risk-entities";
import { RiskListsPanel } from "@/components/risk/risk-lists";
import {
  RiskNavProvider, type RiskAssessmentPreset, type RiskEntityFocus, type RiskEntityKind, type RiskNav,
} from "@/components/risk/risk-nav";
import { RiskPolicyPanel, type PolicySection } from "@/components/risk/risk-policy";
import { RiskReviewsPanel } from "@/components/risk/risk-reviews";
import { RiskScopeProvider, RiskScopeSelect } from "@/components/risk/risk-scope";
import { RiskSimulatorPanel } from "@/components/risk/risk-simulator";
import { RiskCatalogProvider } from "@/components/risk/risk-shared";

/**
 * 风控中心。
 *
 * 七个页签：
 *   **看** —— 大盘 / 评估记录 / 复核（发生了什么、拦得对不对）
 *   **配** —— 规则策略（规则 + 处置策略 + 模板）/ 名单库（谁直接拦、谁直接放）
 *   **查** —— 实体画像（账号 / 设备 / IP）
 * 模拟器横跨"配"与"看"：改规则之前先在这里试。
 *
 * 页签、规则策略的分区、实体画像的类型、应用作用域都同步到 URL
 * （`?tab=&section=&entity=&app=`），侧边栏三级子项与深链都依赖它。
 * 旧页签值（actions / devices / ips）兼容跳转到新位置。
 */
const TABS = [
  { value: "dashboard", label: "风控大盘", icon: Activity },
  { value: "assessments", label: "评估记录", icon: Eye },
  { value: "reviews", label: "人工复核", icon: Clock },
  { value: "rules", label: "规则策略", icon: Shield },
  { value: "lists", label: "名单库", icon: ListChecks },
  { value: "entities", label: "实体画像", icon: Radar },
  { value: "simulator", label: "规则模拟", icon: FlaskConical },
] as const;

const VALID_TABS = new Set<string>(TABS.map((tab) => tab.value));
const LEGACY_TABS: Record<string, { tab: string; section?: PolicySection; entity?: RiskEntityKind }> = {
  actions: { tab: "rules", section: "actions" },
  templates: { tab: "rules", section: "templates" },
  devices: { tab: "entities", entity: "device" },
  ips: { tab: "entities", entity: "ip" },
  accounts: { tab: "entities", entity: "account" },
};
const SECTIONS = new Set<string>(["rules", "actions", "templates"]);
const ENTITY_KINDS = new Set<string>(["account", "device", "ip"]);

function RiskControlPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const rawTab = searchParams.get("tab") ?? "";
  const legacy = LEGACY_TABS[rawTab];
  const tab = legacy?.tab ?? (VALID_TABS.has(rawTab) ? rawTab : "dashboard");
  const rawSection = searchParams.get("section") ?? "";
  const section: PolicySection = legacy?.section ?? (SECTIONS.has(rawSection) ? rawSection as PolicySection : "rules");
  const rawEntity = searchParams.get("entity") ?? "";
  const entity: RiskEntityKind = legacy?.entity ?? (ENTITY_KINDS.has(rawEntity) ? rawEntity as RiskEntityKind : "account");

  // 跨页签的「带着一个对象去看它」。大盘榜单、评估详情、实体画像都会用到，
  // 否则排查时只能手动复制 IP 再切页签粘贴。
  const [focusRuleId, setFocusRuleId] = useState<number | null>(null);
  const [preset, setPreset] = useState<RiskAssessmentPreset | null>(null);
  const [entityFocus, setEntityFocus] = useState<RiskEntityFocus | null>(null);

  const go = useCallback((next: { tab?: string; section?: PolicySection; entity?: RiskEntityKind }) => {
    const params = new URLSearchParams(searchParams.toString());
    params.set("tab", next.tab ?? tab);
    if ((next.tab ?? tab) === "rules") params.set("section", next.section ?? section); else params.delete("section");
    if ((next.tab ?? tab) === "entities") params.set("entity", next.entity ?? entity); else params.delete("entity");
    router.replace(`/risk-control?${params.toString()}`, { scroll: false });
  }, [router, searchParams, tab, section, entity]);

  const nav = useMemo<RiskNav>(() => ({
    inspectAssessments: (next) => { setPreset(next); go({ tab: "assessments" }); },
    openEntity: (focus) => { setEntityFocus(focus); go({ tab: "entities", entity: focus.kind }); },
    openRule: (ruleId) => { setFocusRuleId(ruleId); go({ tab: "rules", section: "rules" }); },
    openTemplates: () => go({ tab: "rules", section: "templates" }),
    openLists: () => go({ tab: "lists" }),
  }), [go]);

  return (
    <RiskNavProvider value={nav}>
      <div className="page-stack">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <SectionHeading eyebrow="控制台" title="风控中心" />
          <RiskScopeSelect />
        </div>

        <Tabs value={tab} onValueChange={(next) => go({ tab: next })} className="space-y-4">
          <TabsList className="w-full justify-start overflow-x-auto">
            {TABS.map(({ value, label, icon: Icon }) => (
              <TabsTrigger key={value} value={value}>
                <Icon className="size-3.5" />
                {label}
              </TabsTrigger>
            ))}
          </TabsList>

          <TabsContent value="dashboard"><RiskDashboardPanel /></TabsContent>

          <TabsContent value="assessments">
            <RiskAssessmentsPanel preset={preset} onClearPreset={() => setPreset(null)} />
          </TabsContent>

          <TabsContent value="reviews"><RiskReviewsPanel /></TabsContent>

          <TabsContent value="rules">
            <RiskPolicyPanel section={section} onSectionChange={(next) => go({ tab: "rules", section: next })}
              focusRuleId={focusRuleId} onFocusHandled={() => setFocusRuleId(null)} />
          </TabsContent>

          <TabsContent value="lists"><RiskListsPanel /></TabsContent>

          <TabsContent value="entities">
            <RiskEntitiesPanel kind={entity} onKindChange={(next) => go({ tab: "entities", entity: next })}
              focus={entityFocus} onFocusHandled={() => setEntityFocus(null)} />
          </TabsContent>

          <TabsContent value="simulator"><RiskSimulatorPanel /></TabsContent>
        </Tabs>
      </div>
    </RiskNavProvider>
  );
}

export default function RiskControlPage() {
  return (
    // useSearchParams 必须包在 Suspense 边界内，否则 next build 报错、
    // 整页退化为客户端渲染（与 console-shell 的 withActiveTab 同一约束）。
    <Suspense fallback={<div className="page-stack"><SectionHeading eyebrow="控制台" title="风控中心" /></div>}>
      {/* 目录（场景 / 等级 / 条件参数 schema）与应用作用域整页共用一份 */}
      <RiskCatalogProvider>
        <RiskScopeProvider>
          <RiskControlPageInner />
        </RiskScopeProvider>
      </RiskCatalogProvider>
    </Suspense>
  );
}
