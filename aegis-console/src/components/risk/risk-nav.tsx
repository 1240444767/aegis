"use client";

import { createContext, useContext, type ReactNode } from "react";

/* ══════════════════════════════════════════════════════════
   跨页签导航

   「带着一个对象去另一个页签看它」在风控中心无处不在：大盘榜单 → 评估记录、
   评估详情 → 账号画像、规则榜 → 规则详情、零规则 → 模板。页面持有状态，
   面板只调这里的函数，不必层层透传回调。
   ══════════════════════════════════════════════════════════ */

export type RiskEntityKind = "account" | "device" | "ip";

export type RiskEntityFocus = {
  kind: RiskEntityKind;
  value: string;
  appId?: number | null;
  userId?: number | null;
};

export type RiskAssessmentPreset = {
  ip?: string;
  deviceId?: string;
  account?: string;
  ruleId?: number;
};

export type RiskNav = {
  inspectAssessments: (preset: RiskAssessmentPreset) => void;
  openEntity: (focus: RiskEntityFocus) => void;
  openRule: (ruleId: number) => void;
  openTemplates: () => void;
  openLists: () => void;
};

const noop = () => {};

const RiskNavContext = createContext<RiskNav>({
  inspectAssessments: noop, openEntity: noop, openRule: noop, openTemplates: noop, openLists: noop,
});

export function RiskNavProvider({ value, children }: { value: RiskNav; children: ReactNode }) {
  return <RiskNavContext.Provider value={value}>{children}</RiskNavContext.Provider>;
}

export function useRiskNav(): RiskNav {
  return useContext(RiskNavContext);
}
