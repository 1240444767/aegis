"use client";

import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import type { ReportParams } from "@/lib/api/reports";
import { color, type Tone } from "./report-kit";

/** 每个分区共用的入参 */
export type SectionProps = { appKey: string; params: ReportParams };

/**
 * 页内分区：一个页面纵向排几组报表，每组一个标题行。
 * 分区标题带 id，页头的分区导航点一下就滚到这里。
 */
export function ReportSection({
  id,
  title,
  description,
  icon: Icon,
  tone = "indigo",
  action,
  children
}: {
  id: string;
  title: string;
  description?: string;
  icon: LucideIcon;
  tone?: Tone;
  action?: ReactNode;
  children: ReactNode;
}) {
  const c = color(tone);
  return (
    <section id={id} className="scroll-mt-24 space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <span
            className="flex size-8 items-center justify-center rounded-xl"
            style={{ backgroundColor: `color-mix(in oklab, ${c} 14%, transparent)`, color: c }}
          >
            <Icon className="size-4" />
          </span>
          <div>
            <h2 className="text-base font-semibold tracking-tight">{title}</h2>
            {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
          </div>
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}
