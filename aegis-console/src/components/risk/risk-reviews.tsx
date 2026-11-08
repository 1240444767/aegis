"use client";

import { useState } from "react";
import { CheckCircle2, Clock, Eye, XCircle } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/data-state";
import { usePendingReviewsQuery } from "@/lib/risk-hooks";
import type { RiskAssessment } from "@/lib/api/types";
import { AssessmentDetailSheet } from "./risk-assessments";
import { ReviewDialog } from "./risk-dispose-dialog";
import { ScopeBadge, useRiskScope } from "./risk-scope";
import {
  ActionBadge, DecisionBadge, DimensionBadge, DimensionBar, LevelBadge, Pagination, RowsSkeleton, SceneBadge,
  ellipsisMiddle, fmtDateTime, fmtRelative, useRiskCatalog,
} from "./risk-shared";

/**
 * 人工复核队列。
 *
 * 每张卡给出判断依据（维度得分、命中判据、名单命中），复核弹窗把结论落到对象上：
 * 通过可以顺手加白并撤销自动封禁，驳回可以选择封账号 / 拉黑设备与 IP。
 * 复核结论不落到对象上时，同一个人下一次请求还会被同样地拦下或放过。
 */

const PAGE_SIZE = 20;

type Pending = { item: RiskAssessment; result: "approved" | "rejected" };

export function RiskReviewsPanel() {
  const { appId } = useRiskScope();
  const [page, setPage] = useState(1);
  const [detailId, setDetailId] = useState<number | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const query = usePendingReviewsQuery(page, PAGE_SIZE, appId);

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Clock className="size-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">待复核队列</h3>
        <Badge variant={total > 0 ? "warning" : "outline"} size="sm">{total}</Badge>
      </div>

      {query.isLoading && items.length === 0 ? (
        <RowsSkeleton rows={4} height={120} />
      ) : items.length === 0 ? (
        <EmptyState title="暂无待复核记录" />
      ) : (
        <>
          <div className="grid gap-2 xl:grid-cols-2">
            {items.map((item) => (
              <ReviewCard key={item.id} item={item}
                onDecide={(result) => setPending({ item, result })}
                onInspect={() => setDetailId(item.id)} />
            ))}
          </div>
          <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
        </>
      )}

      {detailId && <AssessmentDetailSheet id={detailId} onClose={() => setDetailId(null)} onOpen={setDetailId} />}
      {pending && (
        <ReviewDialog assessmentId={pending.item.id} initialResult={pending.result}
          subject={{
            assessmentId: pending.item.id, appId: pending.item.appId, userId: pending.item.userId,
            account: pending.item.account || undefined, ip: pending.item.ip || undefined,
            deviceId: pending.item.deviceId || undefined,
          }}
          onClose={() => setPending(null)} />
      )}
    </div>
  );
}

function ReviewCard({ item, onDecide, onInspect }: {
  item: RiskAssessment;
  onDecide: (result: "approved" | "rejected") => void;
  onInspect: () => void;
}) {
  const { listKindLabel, listTypeLabel } = useRiskCatalog();
  const rules = item.matchedRules ?? [];
  const lists = item.listHits ?? [];
  return (
    <Card>
      <CardContent className="space-y-2.5 p-3">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="font-mono text-lg font-bold tabular-nums">{item.totalScore}</span>
          <LevelBadge level={item.riskLevel} />
          <SceneBadge scene={item.scene} />
          <ActionBadge action={item.action} />
          <DecisionBadge decisionBy={item.decisionBy} />
          {item.appId != null && <ScopeBadge appId={item.appId} />}
          <span className="ml-auto text-[10px] text-muted-foreground" title={fmtDateTime(item.createdAt)}>
            {fmtRelative(item.createdAt)}
          </span>
        </div>

        <DimensionBar scores={item.dimensionScores} width={240} className="h-2" />

        <div className="grid gap-x-4 gap-y-0.5 text-[11px] sm:grid-cols-3">
          <Field label="账号" value={item.account || (item.userId ? `#${item.userId}` : "--")} />
          <Field label="IP" value={`${item.ip || "--"}${item.country ? ` ${item.country}` : ""}`} />
          <Field label="设备" value={item.deviceId ? ellipsisMiddle(item.deviceId, 12, 6) : "未上报"} />
        </div>

        {(rules.length > 0 || lists.length > 0) && (
          <div className="space-y-0.5 rounded-lg border bg-muted/30 px-2.5 py-1.5">
            {lists.map((hit) => (
              <div key={`l-${hit.entryId}`} className="flex items-center gap-2 text-[11px]">
                <span className="shrink-0 font-medium">{listKindLabel(hit.kind)}</span>
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {listTypeLabel(hit.listType)} {hit.value}
                </span>
              </div>
            ))}
            {rules.map((rule) => (
              <div key={rule.ruleId} className="flex items-center gap-2 text-[11px]">
                <span className="shrink-0 font-medium">{rule.ruleName}</span>
                <DimensionBadge dimension={rule.dimension} />
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{rule.reason || "—"}</span>
                <span className="shrink-0 font-mono text-amber-700 dark:text-amber-400">
                  {rule.observe ? "观察" : `+${rule.score}`}
                </span>
              </div>
            ))}
          </div>
        )}

        <div className="flex items-center gap-1.5">
          <Button size="sm" className="h-7 text-xs" onClick={() => onDecide("approved")}>
            <CheckCircle2 className="size-3.5" />通过
          </Button>
          <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => onDecide("rejected")}>
            <XCircle className="size-3.5" />驳回
          </Button>
          <Button size="sm" variant="ghost" className="ml-auto h-7 text-xs" onClick={onInspect}>
            <Eye className="size-3.5" />详情
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate font-mono">{value}</span>
    </div>
  );
}
