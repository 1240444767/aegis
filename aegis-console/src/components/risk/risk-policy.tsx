"use client";

import { Layers, ListOrdered, ShieldAlert } from "lucide-react";

import { cn } from "@/lib/utils";
import { RiskActionsPanel } from "./risk-actions";
import { RiskRulesPanel } from "./risk-rules";
import { RiskTemplatesPanel } from "./risk-templates";

/**
 * 规则策略：规则（怎么判）、处置策略（判了怎么办）、规则模板（一键装一组）。
 *
 * 三者是同一件事的三面，合在一个页签里用分区切换 —— 拆成三个页签时，
 * 改完规则想顺手看一眼分数段会落到哪个动作上，得在页签间来回跳。
 */

export type PolicySection = "rules" | "actions" | "templates";

const SECTIONS = [
  { key: "rules", label: "风险规则", icon: ListOrdered },
  { key: "actions", label: "处置策略", icon: ShieldAlert },
  { key: "templates", label: "规则模板", icon: Layers },
] as const;

export function RiskPolicyPanel({ section, onSectionChange, focusRuleId, onFocusHandled }: {
  section: PolicySection;
  onSectionChange: (next: PolicySection) => void;
  focusRuleId?: number | null;
  onFocusHandled?: () => void;
}) {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-1 border-b">
        {SECTIONS.map(({ key, label, icon: Icon }) => (
          <button key={key} type="button" onClick={() => onSectionChange(key)}
            className={cn("-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-xs transition-colors",
              section === key
                ? "border-primary font-medium text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground")}>
            <Icon className="size-3.5" />{label}
          </button>
        ))}
      </div>

      {section === "rules" && <RiskRulesPanel focusRuleId={focusRuleId} onFocusHandled={onFocusHandled} />}
      {section === "actions" && <RiskActionsPanel />}
      {section === "templates" && <RiskTemplatesPanel />}
    </div>
  );
}
