"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle, CheckCircle2, ChevronRight, Eye, Fingerprint, Gavel, Globe2, ListChecks,
  RefreshCw, Repeat, Search, Trash2, UserRound, XCircle,
} from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { fromLocalInputValue } from "@/lib/datetime";
import {
  usePurgeAssessmentsMutation, useReplayAssessmentMutation,
  useRiskAssessmentQuery, useRiskAssessmentsQuery,
} from "@/lib/risk-hooks";
import type {
  RiskAssessment, RiskAssessmentQuery, RiskEntitySummary, RiskEvalResult, RiskListHit, RiskRuleEvaluation,
} from "@/lib/api/types";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { DisposeDialog, EnforcementList, ReviewDialog, type DisposeSubject } from "./risk-dispose-dialog";
import { CurrentListEntries, FilterSelect } from "./risk-lists";
import { useRiskNav, type RiskAssessmentPreset } from "./risk-nav";
import { ScopeBadge, useRiskScope } from "./risk-scope";
import {
  ActionBadge, DecisionBadge, DetailRow, DimensionBadge, DimensionBar, DimensionBreakdown, LevelBadge,
  ListKindBadge, ModeBadge, Pagination, RowsSkeleton, SceneBadge, SectionTitle, ellipsisMiddle, fmtDateTime,
  fmtNumber, fmtRelative, useRiskCatalog,
} from "./risk-shared";

/**
 * 评估记录。
 *
 * 全维度筛选 + 分页 + 详情抽屉。表格多出两列信息：维度条（这一分是哪一面给的）
 * 与判定来源（名单直接定的、规则强制的、还是分数策略给的）—— 一条「拦截」
 * 如果说不出是谁拦的，复核人无从判断该改名单还是改规则。
 */

const PAGE_SIZE = 25;

export function RiskAssessmentsPanel({ preset, onClearPreset }: {
  preset?: RiskAssessmentPreset | null;
  onClearPreset?: () => void;
}) {
  const { scenes, levels, actions, decisionBy, dimensions } = useRiskCatalog();
  const { appId } = useRiskScope();
  const [filters, setFilters] = useState({
    scene: "", riskLevel: "", action: "", reviewed: "", keyword: "", decisionBy: "", dimension: "",
    minScore: "", start: "", end: "",
  });
  const [page, setPage] = useState(1);
  const [detailId, setDetailId] = useState<number | null>(null);
  const [purgeOpen, setPurgeOpen] = useState(false);
  const [purgeDays, setPurgeDays] = useState("30");
  const keyword = useDebouncedValue(filters.keyword.trim(), 300);

  const query = useMemo<RiskAssessmentQuery>(() => ({
    appId,
    scene: filters.scene || undefined,
    riskLevel: filters.riskLevel || undefined,
    action: filters.action || undefined,
    reviewed: filters.reviewed || undefined,
    decisionBy: filters.decisionBy || undefined,
    dimension: filters.dimension || undefined,
    keyword: keyword || undefined,
    minScore: filters.minScore ? Number(filters.minScore) : undefined,
    start: fromLocalInputValue(filters.start) ?? undefined,
    end: fromLocalInputValue(filters.end) ?? undefined,
    // 从大盘榜单 / 实体画像点进来时带上对象过滤
    ip: preset?.ip || undefined,
    deviceId: preset?.deviceId || undefined,
    account: preset?.account || undefined,
    ruleId: preset?.ruleId || undefined,
    page,
    pageSize: PAGE_SIZE,
  }), [filters, keyword, page, preset, appId]);

  const listQuery = useRiskAssessmentsQuery(query);
  const purgeMut = usePurgeAssessmentsMutation();
  const items = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const patch = (next: Partial<typeof filters>) => {
    setFilters((prev) => ({ ...prev, ...next }));
    setPage(1); // 换筛选条件必须回第一页，否则会停在一个空页上
  };

  const presetText = preset
    ? preset.ip ? `IP ${preset.ip}`
      : preset.deviceId ? `设备 ${ellipsisMiddle(preset.deviceId, 16, 8)}`
      : preset.account ? `账号 ${preset.account}`
      : preset.ruleId ? `规则 #${preset.ruleId}` : null
    : null;

  return (
    <div className="space-y-3">
      {presetText && (
        <div className="flex items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 px-3 py-2 text-xs">
          <Search className="size-3.5 text-primary" />
          <span>已按 <code className="font-mono">{presetText}</code> 过滤</span>
          <Button variant="ghost" size="sm" className="ml-auto h-6 px-2 text-xs" onClick={onClearPreset}>
            清除
          </Button>
        </div>
      )}

      <Card>
        <CardContent className="flex flex-wrap items-end gap-2 p-3">
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">搜索</Label>
            <Input value={filters.keyword} onChange={(e) => patch({ keyword: e.target.value })}
              placeholder="IP、设备号或账号" className="h-8 w-48 text-xs" />
          </div>
          <FilterSelect label="场景" value={filters.scene} onChange={(v) => patch({ scene: v })}
            options={scenes.map((s) => ({ value: s.value, label: s.label }))} />
          <FilterSelect label="等级" value={filters.riskLevel} onChange={(v) => patch({ riskLevel: v })}
            options={levels.map((l) => ({ value: l.value, label: l.label }))} />
          <FilterSelect label="动作" value={filters.action} onChange={(v) => patch({ action: v })}
            options={actions.map((a) => ({ value: a.value, label: a.label }))} />
          <FilterSelect label="判定来源" value={filters.decisionBy} onChange={(v) => patch({ decisionBy: v })}
            options={decisionBy.map((d) => ({ value: d.value, label: d.label }))} />
          <FilterSelect label="维度" value={filters.dimension} onChange={(v) => patch({ dimension: v })}
            options={dimensions.map((d) => ({ value: d.value, label: d.label }))} />
          <FilterSelect label="复核" value={filters.reviewed} onChange={(v) => patch({ reviewed: v })}
            options={[{ value: "false", label: "未复核" }, { value: "true", label: "已复核" }]} />
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">最低分</Label>
            <Input type="number" value={filters.minScore} onChange={(e) => patch({ minScore: e.target.value })}
              placeholder="0" className="h-8 w-16 font-mono text-xs" />
          </div>
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">起始时间</Label>
            <Input type="datetime-local" value={filters.start} onChange={(e) => patch({ start: e.target.value })}
              className="h-8 w-44 text-xs" />
          </div>
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">结束时间</Label>
            <Input type="datetime-local" value={filters.end} onChange={(e) => patch({ end: e.target.value })}
              className="h-8 w-44 text-xs" />
          </div>
          <div className="ml-auto flex items-center gap-1">
            <Button variant="ghost" size="sm" className="h-8" onClick={() => listQuery.refetch()}>
              <RefreshCw className={cn("size-3.5", listQuery.isFetching && "animate-spin")} />
            </Button>
            <Button variant="ghost" size="sm" className="h-8 text-destructive" onClick={() => setPurgeOpen(true)}>
              <Trash2 className="size-3.5" />清理
            </Button>
          </div>
        </CardContent>
      </Card>

      {listQuery.isLoading && items.length === 0 ? (
        <RowsSkeleton />
      ) : items.length === 0 ? (
        <EmptyState title="暂无评估记录" />
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-36">时间</TableHead>
                  <TableHead className="w-20">场景</TableHead>
                  <TableHead className="w-28">分数与维度</TableHead>
                  <TableHead className="w-20">等级</TableHead>
                  <TableHead className="w-32">处置</TableHead>
                  <TableHead className="w-36">IP</TableHead>
                  <TableHead>账号与设备</TableHead>
                  <TableHead className="w-48">命中</TableHead>
                  <TableHead className="w-8" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id} className="cursor-pointer text-xs" onClick={() => setDetailId(item.id)}>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {fmtDateTime(item.createdAt)}
                    </TableCell>
                    <TableCell><SceneBadge scene={item.scene} /></TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="w-7 text-right font-mono font-semibold tabular-nums">{item.totalScore}</span>
                        <DimensionBar scores={item.dimensionScores} width={56} />
                      </div>
                    </TableCell>
                    <TableCell><LevelBadge level={item.riskLevel} /></TableCell>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-1">
                        <ActionBadge action={item.action} />
                        {item.action === "review" && (
                          <Badge variant={item.reviewed ? "success" : "warning"} size="sm">
                            {item.reviewed ? "已核" : "待核"}
                          </Badge>
                        )}
                        <DecisionBadge decisionBy={item.decisionBy} />
                      </div>
                    </TableCell>
                    <TableCell className="font-mono">
                      {item.ip || "--"}
                      {item.country && <span className="ml-1 text-muted-foreground">{item.country}</span>}
                    </TableCell>
                    <TableCell className="max-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate">{item.account || (item.userId ? `用户 #${item.userId}` : "--")}</span>
                        {item.appId != null && <ScopeBadge appId={item.appId} className="shrink-0" />}
                      </div>
                      <div className="truncate font-mono text-[10px] text-muted-foreground">
                        {item.deviceId ? ellipsisMiddle(item.deviceId, 12, 6) : "无设备标识"}
                      </div>
                    </TableCell>
                    <TableCell>
                      <MatchedCell rules={item.matchedRules} listHits={item.listHits} />
                    </TableCell>
                    <TableCell><ChevronRight className="size-3.5 text-muted-foreground" /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
        </>
      )}

      {detailId && <AssessmentDetailSheet id={detailId} onClose={() => setDetailId(null)} onOpen={setDetailId} />}

      <AlertDialog open={purgeOpen} onOpenChange={setPurgeOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>清理历史评估记录</AlertDialogTitle>
            <AlertDialogDescription>
              删除后无法恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-1">
            <Label className="text-xs">保留最近多少天</Label>
            <Input type="number" min={1} value={purgeDays} onChange={(e) => setPurgeDays(e.target.value)}
              className="h-8 w-32 font-mono text-xs" />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={async () => {
              const days = Number(purgeDays);
              if (!Number.isFinite(days) || days < 1) { toast.error("请填写有效天数"); return; }
              try {
                const result = await purgeMut.mutateAsync(days);
                toast.success(`已清理 ${fmtNumber(result.removed)} 条记录`);
              } catch (error) {
                toast.error(error instanceof ApiError ? error.message : "清理失败");
              }
            }}>确认清理</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function MatchedCell({ rules, listHits }: { rules: RiskAssessment["matchedRules"]; listHits?: RiskListHit[] | null }) {
  const { listKindLabel } = useRiskCatalog();
  const list = (rules ?? []).filter((r) => !r.observe);
  const observed = (rules ?? []).length - list.length;
  const lists = listHits ?? [];
  if (list.length === 0 && lists.length === 0) {
    return <span className="text-[10px] text-muted-foreground">{observed > 0 ? `观察命中 ${observed}` : "未命中"}</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {lists.slice(0, 1).map((hit) => (
        <Badge key={hit.entryId} variant={hit.kind === "allow" ? "success" : hit.kind === "block" ? "danger" : "warning"} size="sm">
          {listKindLabel(hit.kind)}
        </Badge>
      ))}
      {list.slice(0, 2).map((rule) => (
        <Badge key={rule.ruleId} variant="outline" size="sm" title={rule.reason}>
          {rule.ruleName} +{rule.score}
        </Badge>
      ))}
      {list.length + lists.length > 3 && <Badge variant="outline" size="sm">+{list.length + lists.length - 3}</Badge>}
      {observed > 0 && <Badge variant="info" size="sm">观察 {observed}</Badge>}
    </div>
  );
}

/* ══════════════════════════════════════════════════════════
   评估详情
   ══════════════════════════════════════════════════════════ */

export function AssessmentDetailSheet({ id, onClose, onOpen }: {
  id: number; onClose: () => void; onOpen?: (id: number) => void;
}) {
  const { listTypeLabel } = useRiskCatalog();
  const nav = useRiskNav();
  const query = useRiskAssessmentQuery(id);
  const replayMut = useReplayAssessmentMutation();
  const [replay, setReplay] = useState<RiskEvalResult | null>(null);
  const [disposing, setDisposing] = useState(false);
  const [reviewing, setReviewing] = useState<"approved" | "rejected" | null>(null);
  const [replayFor, setReplayFor] = useState(id);

  // 关联记录点进来时换一条：重放结果属于上一条，跟着清掉
  if (replayFor !== id) {
    setReplayFor(id);
    setReplay(null);
  }

  const detail = query.data;
  const assessment = detail?.assessment;

  const runReplay = async () => {
    try {
      const result = await replayMut.mutateAsync(id);
      setReplay(result);
      toast.success("已重放");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "重放失败");
    }
  };

  const needsReview = assessment?.action === "review" && !assessment.reviewed;
  const subject: DisposeSubject | null = assessment ? {
    assessmentId: assessment.id, appId: assessment.appId, userId: assessment.userId,
    account: assessment.account || undefined, ip: assessment.ip || undefined, deviceId: assessment.deviceId || undefined,
  } : null;

  const openEntity = (kind: "account" | "device" | "ip") => {
    if (!assessment) return;
    const value = kind === "account" ? assessment.account : kind === "device" ? assessment.deviceId : assessment.ip;
    if (!value && !(kind === "account" && assessment.userId)) return;
    onClose();
    nav.openEntity({ kind, value, appId: assessment.appId, userId: assessment.userId });
  };

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-3xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="flex items-center gap-2">
            <Eye className="size-4" />评估记录 #{id}
          </SheetTitle>
          <SheetDescription>
            {assessment ? `${fmtDateTime(assessment.createdAt)}，${fmtRelative(assessment.createdAt)}` : "判定详情"}
          </SheetDescription>
          {assessment && (
            <div className="flex flex-wrap items-center gap-1.5 pt-1">
              {needsReview && (
                <>
                  <Button size="sm" className="h-7 text-xs" onClick={() => setReviewing("approved")}>
                    <CheckCircle2 className="size-3.5" />通过
                  </Button>
                  <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setReviewing("rejected")}>
                    <XCircle className="size-3.5" />驳回
                  </Button>
                </>
              )}
              <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setDisposing(true)}>
                <Gavel className="size-3.5" />处置
              </Button>
              <Button size="sm" variant="ghost" className="h-7 text-xs" disabled={!assessment.account && !assessment.userId}
                onClick={() => openEntity("account")}>
                <UserRound className="size-3.5" />账号画像
              </Button>
              <Button size="sm" variant="ghost" className="h-7 text-xs" disabled={!assessment.deviceId}
                onClick={() => openEntity("device")}>
                <Fingerprint className="size-3.5" />设备
              </Button>
              <Button size="sm" variant="ghost" className="h-7 text-xs" disabled={!assessment.ip}
                onClick={() => openEntity("ip")}>
                <Globe2 className="size-3.5" />IP
              </Button>
            </div>
          )}
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {query.isLoading || !detail || !assessment ? (
            <div className="p-5"><RowsSkeleton rows={6} height={56} /></div>
          ) : (
            <div className="space-y-5 px-5 py-4">
              <section className="grid grid-cols-2 gap-2 sm:grid-cols-5">
                <VerdictTile label="总分" value={String(assessment.totalScore)} />
                <VerdictTile label="等级" node={<LevelBadge level={assessment.riskLevel} />} />
                <VerdictTile label="动作" node={<ActionBadge action={assessment.action} />} />
                <VerdictTile label="判定来源" node={assessment.decisionBy && assessment.decisionBy !== "none"
                  ? <DecisionBadge decisionBy={assessment.decisionBy} />
                  : <span className="text-xs text-muted-foreground">无干预</span>} />
                <VerdictTile label="判定耗时" value={`${assessment.latencyMs} ms`} />
              </section>

              {assessment.actionDetail && (
                <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                  {assessment.actionDetail}
                </p>
              )}

              <section className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <SectionTitle>维度得分</SectionTitle>
                  <DimensionBreakdown scores={assessment.dimensionScores} />
                </div>
                <div className="space-y-2">
                  <SectionTitle>名单命中</SectionTitle>
                  {(assessment.listHits ?? []).length === 0 ? (
                    <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">未命中名单</p>
                  ) : (
                    <div className="space-y-1">
                      {(assessment.listHits ?? []).map((hit) => (
                        <div key={hit.entryId} className="flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[11px]">
                          <ListChecks className="size-3.5 shrink-0 text-muted-foreground" />
                          <ListKindBadge kind={hit.kind} />
                          <span className="shrink-0 text-muted-foreground">{listTypeLabel(hit.listType)}</span>
                          <code className="min-w-0 flex-1 truncate font-mono">{hit.value}</code>
                          {hit.reason && <span className="truncate text-muted-foreground" title={hit.reason}>{hit.reason}</span>}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </section>

              {(assessment.enforcement ?? []).length > 0 && (
                <section className="space-y-2">
                  <SectionTitle>实际处置</SectionTitle>
                  <EnforcementList items={assessment.enforcement} />
                </section>
              )}

              <section className="space-y-2">
                <SectionTitle>规则轨迹</SectionTitle>
                <RuleEvaluationList rules={detail.rules} />
              </section>

              <section className="space-y-2">
                <SectionTitle>请求事实</SectionTitle>
                <Card><CardContent className="p-3">
                  <DetailRow label="场景"><SceneBadge scene={assessment.scene} /></DetailRow>
                  <DetailRow label="作用域"><ScopeBadge appId={assessment.appId} /></DetailRow>
                  <DetailRow label="账号" mono>{assessment.account || "--"}</DetailRow>
                  {assessment.userId != null && <DetailRow label="用户 ID" mono>{assessment.userId}</DetailRow>}
                  <DetailRow label="IP" mono>
                    {assessment.ip || "--"}
                    {assessment.country && <span className="ml-2 text-muted-foreground">{assessment.country}</span>}
                  </DetailRow>
                  <DetailRow label="设备标识" mono>{assessment.deviceId || "未上报"}</DetailRow>
                  <DetailRow label="User-Agent" mono>{assessment.userAgent || "未上报"}</DetailRow>
                </CardContent></Card>
              </section>

              <section className="space-y-2">
                <SectionTitle>现行名单</SectionTitle>
                <CurrentListEntries entries={detail.listEntries} removable />
              </section>

              <section className="space-y-2">
                <SectionTitle>环境快照</SectionTitle>
                <EvalContextGrid context={assessment.evalContext} />
              </section>

              <section className="space-y-2">
                <div className="flex items-center justify-between">
                  <SectionTitle>用当前规则集重放</SectionTitle>
                  <Button variant="outline" size="sm" className="h-7 text-xs" disabled={replayMut.isPending}
                    onClick={runReplay}>
                    <Repeat className={cn("size-3.5", replayMut.isPending && "animate-spin")} />重放
                  </Button>
                </div>
                {replay && (
                  <Card><CardContent className="space-y-2 p-3">
                    <div className="flex flex-wrap items-center gap-2 text-xs">
                      <span className="text-muted-foreground">当时</span>
                      <Badge variant="outline" size="sm" className="font-mono">{assessment.totalScore} 分</Badge>
                      <ActionBadge action={assessment.action} />
                      <span className="text-muted-foreground">现在</span>
                      <Badge variant="outline" size="sm" className="font-mono">{replay.totalScore} 分</Badge>
                      <ActionBadge action={replay.action} />
                      <DecisionBadge decisionBy={replay.decisionBy} />
                      {replay.totalScore !== assessment.totalScore && (
                        <Badge variant={replay.totalScore > assessment.totalScore ? "danger" : "success"} size="sm">
                          {replay.totalScore > assessment.totalScore ? "+" : ""}
                          {replay.totalScore - assessment.totalScore}
                        </Badge>
                      )}
                    </div>
                    <RuleEvaluationList rules={replay.evaluatedRules ?? null} showMisses />
                  </CardContent></Card>
                )}
              </section>

              <section className="grid gap-3 sm:grid-cols-2">
                <EntitySummaryCard title="该 IP 的画像" icon={Globe2} summary={detail.ipSummary} subject={assessment.ip} />
                <EntitySummaryCard title="该设备的画像" icon={Fingerprint} summary={detail.deviceSummary} subject={assessment.deviceId} />
              </section>

              <RelatedList title="同 IP 的近期评估" items={detail.sameIp} currentId={id} onOpen={(next) => onOpen?.(next)} />
              <RelatedList title="同设备的近期评估" items={detail.sameDevice} currentId={id} onOpen={(next) => onOpen?.(next)} />
              <RelatedList title="同账号的近期评估" items={detail.sameAccount} currentId={id} onOpen={(next) => onOpen?.(next)} />

              {assessment.reviewed && (
                <section className="space-y-2">
                  <SectionTitle>复核</SectionTitle>
                  <Card><CardContent className="p-3">
                    <DetailRow label="结论">
                      <Badge variant={assessment.reviewResult === "approved" ? "success" : "danger"} size="sm">
                        {assessment.reviewResult === "approved" ? "通过" : "驳回"}
                      </Badge>
                    </DetailRow>
                    <DetailRow label="复核人">{assessment.reviewerName || `#${assessment.reviewerId ?? "--"}`}</DetailRow>
                    <DetailRow label="时间">{fmtDateTime(assessment.reviewedAt)}</DetailRow>
                    {assessment.reviewComment && <DetailRow label="备注">{assessment.reviewComment}</DetailRow>}
                  </CardContent></Card>
                </section>
              )}
            </div>
          )}
        </ScrollArea>
      </SheetContent>

      {disposing && subject && <DisposeDialog subject={subject} onClose={() => setDisposing(false)} />}
      {reviewing && subject && (
        <ReviewDialog assessmentId={id} subject={subject} initialResult={reviewing} onClose={() => setReviewing(null)} />
      )}
    </Sheet>
  );
}

function VerdictTile({ label, value, node }: { label: string; value?: string; node?: React.ReactNode }) {
  return (
    <div className="rounded-lg border p-2.5 text-center">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className="mt-1 flex items-center justify-center">
        {node ?? <span className="font-mono text-lg font-bold tabular-nums">{value}</span>}
      </div>
    </div>
  );
}

/**
 * 规则轨迹列表。
 *
 * showMisses 打开时连未命中的规则一起列出 —— 只显示命中项等于让排查的人
 * 猜「另外几条为什么没中」，那是调规则时最耗时的一步。
 */
export function RuleEvaluationList({ rules, showMisses }: { rules: RiskRuleEvaluation[] | null; showMisses?: boolean }) {
  const { conditionLabel, actionLabel } = useRiskCatalog();
  const list = (rules ?? []).filter((rule) => showMisses || rule.hit);
  if (list.length === 0) {
    return (
      <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
        未命中任何规则
      </p>
    );
  }
  return (
    <div className="space-y-1">
      {list.map((rule) => {
        const observe = rule.mode === "observe";
        const counted = rule.hit && !observe;
        return (
          <div key={`${rule.ruleId}-${rule.ruleName}`}
            className={cn("flex items-start gap-2 rounded-lg border px-2.5 py-1.5 text-xs",
              counted && "border-amber-300/70 bg-amber-50/50 dark:border-amber-900/50 dark:bg-amber-950/20",
              rule.hit && observe && "border-sky-300/70 bg-sky-50/50 dark:border-sky-900/50 dark:bg-sky-950/20",
              !rule.hit && "opacity-60")}>
            {rule.hit
              ? <AlertTriangle className={cn("mt-0.5 size-3.5 shrink-0", observe ? "text-sky-500" : "text-amber-500")} />
              : <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />}
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="font-medium">{rule.ruleName}</span>
                <DimensionBadge dimension={rule.dimension} />
                {rule.conditionType && <Badge variant="outline" size="sm">{conditionLabel(rule.conditionType)}</Badge>}
                <ModeBadge mode={rule.mode} />
                {rule.decision && <Badge variant="danger" size="sm">强制{actionLabel(rule.decision)}</Badge>}
              </div>
              {rule.reason && <p className="mt-0.5 text-[11px] text-muted-foreground">{rule.reason}</p>}
              {rule.error && (
                <p className="mt-0.5 font-mono text-[10px] text-rose-600 dark:text-rose-400">评估出错：{rule.error}</p>
              )}
            </div>
            <span className={cn("shrink-0 font-mono font-semibold tabular-nums",
              counted ? "text-amber-700 dark:text-amber-400" : "text-muted-foreground")}>
              {counted ? `+${rule.score}` : `(${rule.score})`}
            </span>
          </div>
        );
      })}
    </div>
  );
}

/** 环境快照按后端目录的分组呈现，一坨扁平 JSON 是读不动的。 */
export function EvalContextGrid({ context }: { context?: Record<string, unknown> | null }) {
  const { metadata } = useRiskCatalog();
  const entries = useMemo(() => context ?? {}, [context]);

  const grouped = useMemo(() => {
    const byName = new Map((metadata?.variables ?? []).map((v) => [v.name, v]));
    const map = new Map<string, Array<{ name: string; label: string; value: string }>>();
    for (const [name, raw] of Object.entries(entries)) {
      if (name === "extra") continue;
      const meta = byName.get(name);
      const group = meta?.group ?? "其他";
      const list = map.get(group) ?? [];
      list.push({ name, label: meta?.description ?? name, value: formatContextValue(raw) });
      map.set(group, list);
    }
    return [...map.entries()];
  }, [entries, metadata]);

  if (grouped.length === 0) {
    return (
      <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
        暂无环境快照
      </p>
    );
  }

  const extra = entries.extra as Record<string, unknown> | undefined;

  return (
    <div className="space-y-2">
      {grouped.map(([group, items]) => (
        <div key={group} className="rounded-lg border">
          <div className="border-b bg-muted/30 px-2.5 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            {group}
          </div>
          <div className="grid gap-x-4 gap-y-0.5 p-2.5 sm:grid-cols-2">
            {items.map((item) => (
              <div key={item.name} className="flex items-baseline gap-2 text-[11px]" title={item.label}>
                <code className="shrink-0 font-mono text-muted-foreground">{item.name}</code>
                <span className="min-w-0 flex-1 truncate text-right font-mono tabular-nums">{item.value}</span>
              </div>
            ))}
          </div>
        </div>
      ))}
      {extra && Object.keys(extra).length > 0 && (
        <div className="rounded-lg border">
          <div className="border-b bg-muted/30 px-2.5 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            调用方透传
          </div>
          <pre className="overflow-x-auto p-2.5 font-mono text-[10px]">{JSON.stringify(extra, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}

function formatContextValue(value: unknown): string {
  if (value === null || value === undefined) return "--";
  if (typeof value === "boolean") return value ? "是" : "否";
  if (typeof value === "number") return Number.isInteger(value) ? String(value) : value.toFixed(2);
  if (typeof value === "string") return value === "" ? "--" : value;
  return JSON.stringify(value);
}

function EntitySummaryCard({ title, icon: Icon, summary, subject }: {
  title: string; icon: typeof Globe2; summary: RiskEntitySummary; subject: string;
}) {
  if (!subject) {
    return (
      <Card><CardContent className="p-3">
        <h5 className="flex items-center gap-1.5 text-xs font-medium"><Icon className="size-3.5" />{title}</h5>
        <p className="mt-2 text-[11px] text-muted-foreground">未上报</p>
      </CardContent></Card>
    );
  }
  return (
    <Card><CardContent className="space-y-1.5 p-3">
      <h5 className="flex items-center gap-1.5 text-xs font-medium"><Icon className="size-3.5" />{title}</h5>
      <div className="grid grid-cols-2 gap-x-3 gap-y-0.5 text-[11px]">
        <SummaryLine label="历史评估" value={fmtNumber(summary.totalAssessments)} />
        <SummaryLine label="其中拦截" value={fmtNumber(summary.blocked)} />
        <SummaryLine label="平均分" value={summary.avgScore.toFixed(1)} />
        <SummaryLine label="峰值分" value={String(summary.maxScore)} />
        <SummaryLine label="关联账号" value={fmtNumber(summary.distinctAccounts)} />
        <SummaryLine label="关联设备" value={fmtNumber(summary.distinctDevices)} />
      </div>
      <Separator />
      <p className="text-[10px] text-muted-foreground">
        首次 {fmtRelative(summary.firstSeenAt)}，最近 {fmtRelative(summary.lastSeenAt)}
      </p>
    </CardContent></Card>
  );
}

function SummaryLine({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono tabular-nums">{value}</span>
    </div>
  );
}

function RelatedList({ title, items, currentId, onOpen }: {
  title: string; items: RiskAssessment[] | null; currentId: number; onOpen: (id: number) => void;
}) {
  const list = (items ?? []).filter((item) => item.id !== currentId);
  if (list.length === 0) return null;
  return (
    <section className="space-y-2">
      <SectionTitle>{title}</SectionTitle>
      <div className="space-y-1">
        {list.map((item) => (
          <button key={item.id} type="button" onClick={() => onOpen(item.id)}
            className="flex w-full items-center gap-2 rounded-lg border px-2.5 py-1.5 text-left text-[11px] transition-colors hover:bg-accent/40">
            <SceneBadge scene={item.scene} />
            <LevelBadge level={item.riskLevel} />
            <ActionBadge action={item.action} />
            <span className="truncate font-mono text-muted-foreground">{item.account || item.ip || "--"}</span>
            <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
              {item.totalScore} 分，{fmtRelative(item.createdAt)}
            </span>
          </button>
        ))}
      </div>
    </section>
  );
}
