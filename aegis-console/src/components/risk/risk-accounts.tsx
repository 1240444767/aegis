"use client";

import { useState } from "react";
import { Ban, Gavel, Search, ShieldCheck, UserRound } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useRiskAccountQuery, useRiskAccountsQuery } from "@/lib/risk-hooks";
import type { RiskAccountProfile, RiskAssessment } from "@/lib/api/types";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { AssessmentDetailSheet } from "./risk-assessments";
import { DisposeDialog } from "./risk-dispose-dialog";
import { CurrentListEntries } from "./risk-lists";
import { useRiskNav, type RiskEntityFocus } from "./risk-nav";
import { ScopeBadge, useRiskScope } from "./risk-scope";
import {
  ActionBadge, DetailRow, DimensionBar, DimensionBreakdown, LevelBadge, ListKindBadge, Pagination, RowsSkeleton,
  SceneBadge, SectionTitle, SegmentedControl, ellipsisMiddle, fmtDateTime, fmtExpiry, fmtNumber, fmtRelative,
} from "./risk-shared";

/**
 * 账号画像。
 *
 * 账号是「应用 + 登录名」：不同应用的同名账号是两个人。列表按评估记录聚合，
 * 回答「最近谁在被拦、谁在反复触发高危」；详情把这个人的维度得分、现行封禁、
 * 名单、用过的 IP 与设备摆在一起，处置在同一屏完成。
 */

const PAGE_SIZE = 25;

const DAY_OPTIONS = [
  { key: "7", label: "7 天" },
  { key: "30", label: "30 天" },
  { key: "90", label: "90 天" },
] as const;

export function RiskAccountsPanel({ focus, onFocusHandled }: {
  focus?: RiskEntityFocus | null;
  onFocusHandled?: () => void;
}) {
  const { appId } = useRiskScope();
  const [keyword, setKeyword] = useState("");
  const [onlyRisk, setOnlyRisk] = useState(true);
  const [days, setDays] = useState<string>("30");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<RiskEntityFocus | null>(null);
  const debounced = useDebouncedValue(keyword.trim(), 300);

  const query = useRiskAccountsQuery({
    appId, keyword: debounced || undefined, onlyRisk: onlyRisk ? "true" : undefined,
    days: Number(days), page, pageSize: PAGE_SIZE,
  });
  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const active = focus ?? selected;

  return (
    <div className="space-y-3">
      <Card><CardContent className="flex flex-wrap items-end gap-3 p-3">
        <div className="space-y-1">
          <Label className="text-[10px] text-muted-foreground">搜索</Label>
          <Input value={keyword} onChange={(e) => { setKeyword(e.target.value); setPage(1); }}
            placeholder="账号或用户 ID" className="h-8 w-56 text-xs" />
        </div>
        <div className="space-y-1">
          <Label className="text-[10px] text-muted-foreground">时间窗</Label>
          <SegmentedControl value={days} onChange={(v) => { setDays(v); setPage(1); }} options={DAY_OPTIONS} />
        </div>
        <label className="flex items-center gap-2 pb-1 text-xs">
          <Switch checked={onlyRisk} onCheckedChange={(v) => { setOnlyRisk(v); setPage(1); }} />
          只看有风险
        </label>
        <span className="ml-auto text-xs text-muted-foreground">共 {fmtNumber(total)} 个账号</span>
      </CardContent></Card>

      {query.isLoading && items.length === 0 ? (
        <RowsSkeleton />
      ) : items.length === 0 ? (
        <EmptyState title="暂无账号" />
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>账号</TableHead>
                  <TableHead className="w-20 text-right">评估</TableHead>
                  <TableHead className="w-20 text-right">拦截</TableHead>
                  <TableHead className="w-20 text-right">验证</TableHead>
                  <TableHead className="w-20 text-right">高危</TableHead>
                  <TableHead className="w-24 text-right">峰值 / 均分</TableHead>
                  <TableHead className="w-24 text-right">IP / 设备</TableHead>
                  <TableHead className="w-44">最近</TableHead>
                  <TableHead className="w-20">名单</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <AccountRow key={`${item.appId ?? 0}-${item.account}-${item.userId ?? 0}`} item={item}
                    onOpen={() => setSelected({ kind: "account", value: item.account, appId: item.appId, userId: item.userId })} />
                ))}
              </TableBody>
            </Table>
          </div>
          <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
        </>
      )}

      {active && (
        <AccountDetailSheet target={active} onClose={() => { setSelected(null); onFocusHandled?.(); }} />
      )}
    </div>
  );
}

function AccountRow({ item, onOpen }: { item: RiskAccountProfile; onOpen: () => void }) {
  return (
    <TableRow className="cursor-pointer text-xs" onClick={onOpen}>
      <TableCell className="max-w-0">
        <div className="flex items-center gap-1.5">
          <span className="truncate font-medium">{item.account || `用户 #${item.userId ?? "--"}`}</span>
          <ScopeBadge appId={item.appId} className="shrink-0" />
        </div>
        {item.userId != null && <div className="font-mono text-[10px] text-muted-foreground">#{item.userId}</div>}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">{fmtNumber(item.totalAssessments)}</TableCell>
      <TableCell className={cn("text-right font-mono tabular-nums", item.blocked > 0 && "text-rose-600 dark:text-rose-400")}>
        {fmtNumber(item.blocked)}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">{fmtNumber(item.challenged)}</TableCell>
      <TableCell className={cn("text-right font-mono tabular-nums", item.highRisk > 0 && "text-amber-600 dark:text-amber-400")}>
        {fmtNumber(item.highRisk)}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {item.maxScore}<span className="text-muted-foreground"> / {item.avgScore.toFixed(1)}</span>
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {fmtNumber(item.distinctIps)}<span className="text-muted-foreground"> / </span>{fmtNumber(item.distinctDevices)}
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-1">
          {item.lastLevel && <LevelBadge level={item.lastLevel} />}
          {item.lastScene && <SceneBadge scene={item.lastScene} />}
        </div>
        <div className="text-[10px] text-muted-foreground">{fmtRelative(item.lastSeenAt)}</div>
      </TableCell>
      <TableCell>{item.listed ? <ListKindBadge kind={item.listed} /> : <span className="text-muted-foreground">--</span>}</TableCell>
    </TableRow>
  );
}

export function AccountDetailSheet({ target, onClose }: { target: RiskEntityFocus; onClose: () => void }) {
  const nav = useRiskNav();
  const query = useRiskAccountQuery({
    appId: target.appId ?? null, account: target.value || undefined, userId: target.userId ?? null,
  });
  const [dispose, setDispose] = useState<"block" | "allow" | null>(null);
  const [assessmentId, setAssessmentId] = useState<number | null>(null);
  const detail = query.data;
  const profile = detail?.profile;
  const name = target.value || (target.userId ? `用户 #${target.userId}` : "--");

  const subject = {
    appId: target.appId ?? null, userId: profile?.userId ?? target.userId ?? null, account: target.value || undefined,
  };

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="flex items-center gap-2"><UserRound className="size-4" />{name}</SheetTitle>
          <SheetDescription className="flex items-center gap-1.5">
            <ScopeBadge appId={target.appId} />
            {profile?.userId != null && <span className="font-mono">#{profile.userId}</span>}
          </SheetDescription>
          <div className="flex flex-wrap gap-1.5 pt-1">
            <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setDispose("block")}>
              <Gavel className="size-3.5" />处置
            </Button>
            <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setDispose("allow")}>
              <ShieldCheck className="size-3.5" />加白
            </Button>
            <Button size="sm" variant="ghost" className="h-7 text-xs" disabled={!target.value}
              onClick={() => { onClose(); nav.inspectAssessments({ account: target.value }); }}>
              <Search className="size-3.5" />评估记录
            </Button>
          </div>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {query.isLoading || !detail || !profile ? (
            <div className="p-5"><RowsSkeleton rows={5} height={56} /></div>
          ) : (
            <div className="space-y-5 px-5 py-4">
              <section className="grid grid-cols-4 gap-2">
                <Tile label="评估" value={fmtNumber(profile.totalAssessments)} />
                <Tile label="拦截" value={fmtNumber(profile.blocked)} danger={profile.blocked > 0} />
                <Tile label="高危" value={fmtNumber(profile.highRisk)} danger={profile.highRisk > 0} />
                <Tile label="峰值分" value={String(profile.maxScore)} />
              </section>

              {detail.activeBan && (
                <section className="flex items-start gap-2 rounded-lg border border-rose-300 bg-rose-50/60 px-3 py-2 text-xs dark:border-rose-900/60 dark:bg-rose-950/30">
                  <Ban className="mt-0.5 size-3.5 shrink-0 text-rose-500" />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5 font-medium">
                      账号封禁中
                      <Badge variant="danger" size="sm">{detail.activeBan.banScope === "all" ? "全部访问" : "仅登录"}</Badge>
                    </div>
                    <p className="mt-0.5 text-muted-foreground">
                      {detail.activeBan.reason || "--"}，至 {fmtExpiry(detail.activeBan.endAt)}
                    </p>
                  </div>
                </section>
              )}

              <section className="space-y-2">
                <SectionTitle>维度得分（窗口内累计）</SectionTitle>
                <DimensionBreakdown scores={detail.dimensionScores} />
              </section>

              <section className="space-y-2">
                <SectionTitle>现行名单</SectionTitle>
                <CurrentListEntries entries={detail.listEntries} removable />
              </section>

              <EntityChips title="使用过的 IP" items={detail.ips}
                onPick={(ip) => { onClose(); nav.openEntity({ kind: "ip", value: ip }); }} />
              <EntityChips title="使用过的设备" items={detail.devices} format={(d) => ellipsisMiddle(d, 12, 6)}
                onPick={(deviceId) => { onClose(); nav.openEntity({ kind: "device", value: deviceId }); }} />

              <section className="space-y-2">
                <SectionTitle>概况</SectionTitle>
                <Card><CardContent className="p-3">
                  <DetailRow label="均分" mono>{profile.avgScore.toFixed(1)}</DetailRow>
                  <DetailRow label="验证码 / 复核" mono>{fmtNumber(profile.challenged)}</DetailRow>
                  <DetailRow label="IP / 设备" mono>{fmtNumber(profile.distinctIps)} / {fmtNumber(profile.distinctDevices)}</DetailRow>
                  <DetailRow label="最近出现">{fmtDateTime(profile.lastSeenAt)}</DetailRow>
                </CardContent></Card>
              </section>

              <RecentList items={detail.recent} onOpen={setAssessmentId} />
            </div>
          )}
        </ScrollArea>
      </SheetContent>

      {dispose && <DisposeDialog subject={subject} defaultKind={dispose} onClose={() => setDispose(null)} />}
      {assessmentId && (
        <AssessmentDetailSheet id={assessmentId} onClose={() => setAssessmentId(null)} onOpen={setAssessmentId} />
      )}
    </Sheet>
  );
}

function Tile({ label, value, danger }: { label: string; value: string; danger?: boolean }) {
  return (
    <div className="rounded-lg border p-2.5 text-center">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className={cn("font-mono text-lg font-bold tabular-nums", danger && "text-rose-600 dark:text-rose-400")}>{value}</div>
    </div>
  );
}

export function EntityChips({ title, items, onPick, format }: {
  title: string; items: string[] | null | undefined;
  onPick?: (value: string) => void; format?: (value: string) => string;
}) {
  const list = items ?? [];
  if (list.length === 0) return null;
  return (
    <section className="space-y-1.5">
      <SectionTitle>{title}（{list.length}）</SectionTitle>
      <div className="flex flex-wrap gap-1">
        {list.map((item) => (
          <button key={item} type="button" disabled={!onPick} onClick={() => onPick?.(item)} title={item}
            className={cn("rounded border bg-muted/40 px-1.5 py-0.5 font-mono text-[10px]",
              onPick && "transition-colors hover:border-primary/40 hover:bg-accent")}>
            {format ? format(item) : item}
          </button>
        ))}
      </div>
    </section>
  );
}

export function RecentList({ items, onOpen }: { items: RiskAssessment[] | null | undefined; onOpen?: (id: number) => void }) {
  const list = items ?? [];
  if (list.length === 0) return null;
  return (
    <section className="space-y-2">
      <SectionTitle>最近评估</SectionTitle>
      <div className="space-y-1">
        {list.map((item) => (
          <button key={item.id} type="button" onClick={() => onOpen?.(item.id)} disabled={!onOpen}
            className="flex w-full items-center gap-2 rounded-lg border px-2.5 py-1.5 text-left text-[11px] transition-colors enabled:hover:bg-accent/40">
            <SceneBadge scene={item.scene} />
            <LevelBadge level={item.riskLevel} />
            <ActionBadge action={item.action} />
            <DimensionBar scores={item.dimensionScores} width={48} />
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
