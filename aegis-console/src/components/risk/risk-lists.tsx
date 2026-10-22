"use client";

import { useMemo, useState } from "react";
import {
  Ban, CheckCircle2, Eye, ListChecks, Loader2, Pencil, Plus, RefreshCw, ScanSearch, ShieldCheck, Trash2,
} from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { EmptyState } from "@/components/ui/data-state";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import {
  useCheckRiskListsMutation, useCreateRiskListMutation, useDeleteRiskListMutation,
  useRiskListsQuery, useUpdateRiskListMutation,
} from "@/lib/risk-hooks";
import type { RiskListEntry, RiskListImportResult } from "@/lib/api/types";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { ScopeBadge, ScopeField, useRiskScope } from "./risk-scope";
import {
  DURATION_PRESETS, ListKindBadge, Pagination, RowsSkeleton, SCENE_ALL, SectionTitle, SegmentedControl,
  fmtExpiry, fmtNumber, fmtRelative, useRiskCatalog,
} from "./risk-shared";

/**
 * 名单库。
 *
 * 黑名单命中直接拦截、白名单命中直接放行（跳过规则）、观察名单交给
 * 「观察名单命中」规则加分。评估、复核、人工处置写进来的条目都在这里，
 * 来源一栏说明它是谁加的。
 */

const PAGE_SIZE = 30;

const SOURCE_LABELS: Record<string, string> = {
  manual: "人工",
  auto: "自动处置",
  review: "复核",
  dispose: "人工处置",
  migrated: "标签迁入",
  import: "导入",
};

export const sourceLabel = (source: string) => SOURCE_LABELS[source] ?? source;

export function RiskListsPanel() {
  const { listTypes, listKinds, listTypeLabel } = useRiskCatalog();
  const { appId } = useRiskScope();
  const [filters, setFilters] = useState({ listType: "", kind: "", source: "", active: "true", keyword: "" });
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [creating, setCreating] = useState(false);
  const [checking, setChecking] = useState(false);
  const [editing, setEditing] = useState<RiskListEntry | null>(null);
  const [pendingDelete, setPendingDelete] = useState<number[] | null>(null);
  const [now] = useState(() => Date.now());

  const keyword = useDebouncedValue(filters.keyword.trim(), 300);
  const query = useRiskListsQuery({
    appId,
    listType: filters.listType || undefined,
    kind: filters.kind || undefined,
    source: filters.source || undefined,
    active: filters.active || undefined,
    keyword: keyword || undefined,
    page, pageSize: PAGE_SIZE,
  });
  const deleteMut = useDeleteRiskListMutation();

  const items = useMemo(() => query.data?.items ?? [], [query.data?.items]);
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const allChecked = items.length > 0 && items.every((item) => selected.has(item.id));

  const patch = (next: Partial<typeof filters>) => {
    setFilters((prev) => ({ ...prev, ...next }));
    setPage(1);
    setSelected(new Set());
  };

  const toggle = (id: number, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id); else next.delete(id);
      return next;
    });
  };

  return (
    <div className="space-y-3">
      <Card>
        <CardContent className="flex flex-wrap items-end gap-2 p-3">
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">种类</Label>
            <SegmentedControl value={filters.kind || "all"} onChange={(v) => patch({ kind: v === "all" ? "" : v })}
              options={[{ key: "all", label: "全部" }, ...listKinds.map((k) => ({ key: k.value, label: k.label }))]} />
          </div>
          <FilterSelect label="类型" value={filters.listType} onChange={(v) => patch({ listType: v })}
            options={listTypes.map((t) => ({ value: t.value, label: t.label }))} />
          <FilterSelect label="来源" value={filters.source} onChange={(v) => patch({ source: v })}
            options={Object.entries(SOURCE_LABELS).map(([value, label]) => ({ value, label }))} />
          <FilterSelect label="状态" value={filters.active} onChange={(v) => patch({ active: v })}
            options={[{ value: "true", label: "生效中" }, { value: "false", label: "已过期" }]} />
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">搜索</Label>
            <Input value={filters.keyword} onChange={(e) => patch({ keyword: e.target.value })}
              placeholder="值或理由" className="h-8 w-48 text-xs" />
          </div>
          <div className="ml-auto flex items-center gap-1">
            <Button variant="ghost" size="sm" className="h-8" onClick={() => query.refetch()}>
              <RefreshCw className={cn("size-3.5", query.isFetching && "animate-spin")} />
            </Button>
            <Button variant="outline" size="sm" className="h-8" onClick={() => setChecking(true)}>
              <ScanSearch className="size-3.5" />名单检测
            </Button>
            <Button size="sm" className="h-8" onClick={() => setCreating(true)}>
              <Plus className="size-3.5" />添加
            </Button>
          </div>
        </CardContent>
      </Card>

      {selected.size > 0 && (
        <div className="flex items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 px-3 py-1.5 text-xs">
          <span>已选 {selected.size} 条</span>
          <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={() => setSelected(new Set())}>取消选择</Button>
          <Button variant="ghost" size="sm" className="ml-auto h-6 px-2 text-xs text-destructive"
            onClick={() => setPendingDelete([...selected])}>
            <Trash2 className="size-3.5" />删除
          </Button>
        </div>
      )}

      {query.isLoading && items.length === 0 ? (
        <RowsSkeleton />
      ) : items.length === 0 ? (
        <EmptyState title="暂无名单" />
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-8">
                    <Checkbox checked={allChecked}
                      onCheckedChange={(v) => setSelected(v ? new Set(items.map((i) => i.id)) : new Set())} />
                  </TableHead>
                  <TableHead className="w-24">类型</TableHead>
                  <TableHead>值</TableHead>
                  <TableHead className="w-20">种类</TableHead>
                  <TableHead className="w-36">作用域与场景</TableHead>
                  <TableHead className="w-36">到期</TableHead>
                  <TableHead className="w-24 text-right">命中</TableHead>
                  <TableHead className="w-20">来源</TableHead>
                  <TableHead className="w-48">理由</TableHead>
                  <TableHead className="w-16" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((entry) => {
                  const expired = Boolean(entry.expiresAt && new Date(entry.expiresAt).getTime() <= now);
                  return (
                    <TableRow key={entry.id} className={cn("group text-xs", expired && "opacity-60")}>
                      <TableCell>
                        <Checkbox checked={selected.has(entry.id)} onCheckedChange={(v) => toggle(entry.id, Boolean(v))} />
                      </TableCell>
                      <TableCell className="text-muted-foreground">{listTypeLabel(entry.listType)}</TableCell>
                      <TableCell className="max-w-0">
                        <span className="block truncate font-mono" title={entry.value}>{entry.value}</span>
                        {entry.assessmentId && (
                          <span className="text-[10px] text-muted-foreground">评估 #{entry.assessmentId}</span>
                        )}
                      </TableCell>
                      <TableCell><ListKindBadge kind={entry.kind} /></TableCell>
                      <TableCell>
                        <div className="flex flex-wrap items-center gap-1">
                          <ScopeBadge appId={entry.appId} />
                          <SceneList scenes={entry.scenes} />
                        </div>
                      </TableCell>
                      <TableCell className={cn("whitespace-nowrap", expired ? "text-rose-600 dark:text-rose-400" : "text-muted-foreground")}>
                        {fmtExpiry(entry.expiresAt)}
                      </TableCell>
                      <TableCell className="text-right">
                        <span className="font-mono tabular-nums">{fmtNumber(entry.hitCount)}</span>
                        {entry.lastHitAt && (
                          <div className="text-[10px] text-muted-foreground">{fmtRelative(entry.lastHitAt)}</div>
                        )}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" size="sm">{sourceLabel(entry.source)}</Badge>
                      </TableCell>
                      <TableCell className="max-w-0">
                        <span className="block truncate text-muted-foreground" title={entry.reason}>{entry.reason || "--"}</span>
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                          <Button variant="ghost" size="sm" className="h-7 px-1.5" onClick={() => setEditing(entry)}>
                            <Pencil className="size-3.5" />
                          </Button>
                          <Button variant="ghost" size="sm" className="h-7 px-1.5 text-destructive"
                            onClick={() => setPendingDelete([entry.id])}>
                            <Trash2 className="size-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
          <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
        </>
      )}

      {creating && <ListCreateDialog onClose={() => setCreating(false)} />}
      {editing && <ListEditDialog entry={editing} onClose={() => setEditing(null)} />}
      {checking && <ListCheckDialog onClose={() => setChecking(false)} />}

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除 {pendingDelete?.length ?? 0} 条名单？</AlertDialogTitle>
            <AlertDialogDescription>删除后对应对象立即恢复按规则评估。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={async () => {
              if (!pendingDelete) return;
              try {
                const result = await deleteMut.mutateAsync(pendingDelete);
                toast.success(`已删除 ${fmtNumber(result.removed)} 条`);
                setSelected(new Set());
              } catch (error) {
                toast.error(error instanceof ApiError ? error.message : "删除失败");
              } finally {
                setPendingDelete(null);
              }
            }}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

export function FilterSelect({ label, value, onChange, options, width = "w-24" }: {
  label: string; value: string; onChange: (v: string) => void;
  options: Array<{ value: string; label: string }>; width?: string;
}) {
  return (
    <div className="space-y-1">
      <Label className="text-[10px] text-muted-foreground">{label}</Label>
      <Select value={value || "all"} onValueChange={(v) => onChange(v === "all" ? "" : v)}>
        <SelectTrigger className={cn("h-8 text-xs", width)}><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectItem value="all">全部</SelectItem>
          {options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
        </SelectContent>
      </Select>
    </div>
  );
}

function SceneList({ scenes }: { scenes: string[] | null }) {
  const { sceneLabel } = useRiskCatalog();
  const list = (scenes ?? []).filter((s) => s !== SCENE_ALL);
  if (list.length === 0) return <span className="text-[10px] text-muted-foreground">全部场景</span>;
  return (
    <span className="truncate text-[10px] text-muted-foreground" title={list.map(sceneLabel).join("、")}>
      {list.length <= 2 ? list.map(sceneLabel).join("、") : `${sceneLabel(list[0])} 等 ${list.length} 个`}
    </span>
  );
}

/** 场景多选：空即全部场景 */
function ScenePicker({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
  const { scenes } = useRiskCatalog();
  return (
    <div className="flex flex-wrap gap-1">
      <button type="button" onClick={() => onChange([])}
        className={cn("rounded-md border px-2 py-0.5 text-[11px] transition-colors hover:bg-accent",
          value.length === 0 && "border-primary/50 bg-primary/10 font-medium")}>
        全部场景
      </button>
      {scenes.map((scene) => {
        const active = value.includes(scene.value);
        return (
          <button key={scene.value} type="button"
            onClick={() => onChange(active ? value.filter((v) => v !== scene.value) : [...value, scene.value])}
            className={cn("rounded-md border px-2 py-0.5 text-[11px] transition-colors hover:bg-accent",
              active && "border-primary/50 bg-primary/10 font-medium")}>
            {scene.label}
          </button>
        );
      })}
    </div>
  );
}

/** 到期选择：预设时长，返回 RFC3339 或空串（永久）。keep 用于编辑时「不改」。 */
function ExpiryPicker({ value, onChange, allowKeep }: {
  value: number | "keep"; onChange: (next: number | "keep") => void; allowKeep?: boolean;
}) {
  return (
    <div className="flex flex-wrap gap-1">
      {allowKeep && (
        <button type="button" onClick={() => onChange("keep")}
          className={cn("rounded-md border px-2.5 py-1 text-[11px] transition-colors hover:bg-accent",
            value === "keep" && "border-primary/50 bg-primary/10 font-medium")}>
          不变
        </button>
      )}
      {DURATION_PRESETS.map((preset) => (
        <button key={preset.value} type="button" onClick={() => onChange(preset.value)}
          className={cn("rounded-md border px-2.5 py-1 text-[11px] transition-colors hover:bg-accent",
            value === preset.value && "border-primary/50 bg-primary/10 font-medium")}>
          {preset.label}
        </button>
      ))}
    </div>
  );
}

const expiresFrom = (seconds: number) => (seconds > 0 ? new Date(Date.now() + seconds * 1000).toISOString() : "");

function ListCreateDialog({ onClose }: { onClose: () => void }) {
  const { listTypes, listKinds } = useRiskCatalog();
  const { appId } = useRiskScope();
  const createMut = useCreateRiskListMutation();
  const [listType, setListType] = useState("ip");
  const [kind, setKind] = useState("block");
  const [scope, setScope] = useState<number | null>(appId ?? null);
  const [scenes, setScenes] = useState<string[]>([]);
  const [expiry, setExpiry] = useState<number | "keep">(0);
  const [reason, setReason] = useState("");
  const [raw, setRaw] = useState("");
  const [result, setResult] = useState<RiskListImportResult | null>(null);

  const typeMeta = listTypes.find((t) => t.value === listType);
  const values = useMemo(
    () => [...new Set(raw.split(/[\n,，;；\s]+/).map((v) => v.trim()).filter(Boolean))],
    [raw],
  );

  const submit = async () => {
    if (values.length === 0) { toast.error("请填写至少一个值"); return; }
    try {
      const data = await createMut.mutateAsync({
        appId: scope, listType, values, kind,
        scenes: scenes.length > 0 ? scenes : undefined,
        reason: reason.trim() || undefined,
        expiresAt: typeof expiry === "number" ? expiresFrom(expiry) || undefined : undefined,
      });
      setResult(data);
      toast.success(`新增 ${data.created} 条，更新 ${data.updated} 条`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "添加失败");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><ListChecks className="size-4" />添加名单</DialogTitle>
          <DialogDescription>一行一个值，已在名单里的对象改写为本次设置</DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="space-y-3">
            <div className="grid grid-cols-3 gap-2">
              <ResultTile label="新增" value={result.created} />
              <ResultTile label="更新" value={result.updated} />
              <ResultTile label="无效" value={(result.invalid ?? []).length} danger />
            </div>
            {(result.invalid ?? []).length > 0 && (
              <div className="flex flex-wrap gap-1">
                {(result.invalid ?? []).map((v) => (
                  <code key={v} className="rounded border border-rose-300 bg-rose-50 px-1.5 py-0.5 font-mono text-[10px] text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-300">
                    {v}
                  </code>
                ))}
              </div>
            )}
          </div>
        ) : (
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs">类型</Label>
                <Select value={listType} onValueChange={setListType}>
                  <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {listTypes.map((t) => <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">作用域</Label>
                <ScopeField value={scope} onChange={setScope} />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">种类</Label>
              <SegmentedControl value={kind} onChange={setKind}
                options={listKinds.map((k) => ({ key: k.value, label: k.label }))} />
            </div>
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <Label className="text-xs">值</Label>
                <span className="text-[10px] tabular-nums text-muted-foreground">{values.length} 个</span>
              </div>
              <Textarea rows={5} className="font-mono text-xs" value={raw} onChange={(e) => setRaw(e.target.value)}
                placeholder={typeMeta?.placeholder} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">场景</Label>
              <ScenePicker value={scenes} onChange={setScenes} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">有效期</Label>
              <ExpiryPicker value={expiry} onChange={setExpiry} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">理由</Label>
              <Input className="h-8 text-xs" value={reason} onChange={(e) => setReason(e.target.value)} />
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>{result ? "关闭" : "取消"}</Button>
          {!result && (
            <Button size="sm" disabled={createMut.isPending || values.length === 0} onClick={submit}>
              {createMut.isPending && <Loader2 className="size-3.5 animate-spin" />}
              添加 {values.length > 0 ? values.length : ""} 条
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ResultTile({ label, value, danger }: { label: string; value: number; danger?: boolean }) {
  return (
    <div className="rounded-lg border p-2.5 text-center">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className={cn("font-mono text-lg font-bold tabular-nums", danger && value > 0 && "text-rose-600 dark:text-rose-400")}>
        {value}
      </div>
    </div>
  );
}

function ListEditDialog({ entry, onClose }: { entry: RiskListEntry; onClose: () => void }) {
  const { listKinds, listTypeLabel } = useRiskCatalog();
  const updateMut = useUpdateRiskListMutation();
  const [kind, setKind] = useState(entry.kind);
  const [scenes, setScenes] = useState<string[]>((entry.scenes ?? []).filter((s) => s !== SCENE_ALL));
  const [reason, setReason] = useState(entry.reason);
  const [expiry, setExpiry] = useState<number | "keep">("keep");

  const submit = async () => {
    try {
      await updateMut.mutateAsync({
        id: entry.id,
        data: {
          kind, scenes, reason: reason.trim(),
          expiresAt: expiry === "keep" ? undefined : expiresFrom(expiry),
        },
      });
      toast.success("名单已更新");
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>编辑名单</DialogTitle>
          <DialogDescription className="font-mono">{listTypeLabel(entry.listType)} {entry.value}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label className="text-xs">种类</Label>
            <SegmentedControl value={kind} onChange={setKind}
              options={listKinds.map((k) => ({ key: k.value, label: k.label }))} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">场景</Label>
            <ScenePicker value={scenes} onChange={setScenes} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">有效期（当前：{fmtExpiry(entry.expiresAt)}）</Label>
            <ExpiryPicker value={expiry} onChange={setExpiry} allowKeep />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">理由</Label>
            <Input className="h-8 text-xs" value={reason} onChange={(e) => setReason(e.target.value)} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>取消</Button>
          <Button size="sm" disabled={updateMut.isPending} onClick={submit}>
            {updateMut.isPending && <Loader2 className="size-3.5 animate-spin" />}保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ListCheckDialog({ onClose }: { onClose: () => void }) {
  const { scenes } = useRiskCatalog();
  const { appId } = useRiskScope();
  const checkMut = useCheckRiskListsMutation();
  const [form, setForm] = useState({ scene: "", ip: "", deviceId: "", account: "", userId: "", userAgent: "" });
  const [hits, setHits] = useState<RiskListEntry[] | null>(null);

  const run = async () => {
    try {
      const data = await checkMut.mutateAsync({
        appId: appId ?? null,
        scene: form.scene || undefined,
        ip: form.ip.trim() || undefined,
        deviceId: form.deviceId.trim() || undefined,
        account: form.account.trim() || undefined,
        userId: form.userId ? Number(form.userId) : undefined,
        userAgent: form.userAgent.trim() || undefined,
      });
      setHits(data.hits ?? []);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "检测失败");
    }
  };

  const field = (key: keyof typeof form, label: string, mono = true) => (
    <div className="space-y-1">
      <Label className="text-xs">{label}</Label>
      <Input className={cn("h-8 text-xs", mono && "font-mono")} value={form[key]}
        onChange={(e) => setForm({ ...form, [key]: e.target.value })} />
    </div>
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><ScanSearch className="size-4" />名单检测</DialogTitle>
          <DialogDescription>按当前作用域检测这组对象命中的名单</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1">
            <Label className="text-xs">场景</Label>
            <Select value={form.scene || "all"} onValueChange={(v) => setForm({ ...form, scene: v === "all" ? "" : v })}>
              <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">不限</SelectItem>
                {scenes.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          {field("ip", "IP")}
          {field("deviceId", "设备号")}
          {field("account", "账号", false)}
          {field("userId", "用户 ID")}
          {field("userAgent", "User-Agent")}
        </div>

        {hits && (
          <div className="space-y-1.5">
            <SectionTitle>命中 {hits.length} 条</SectionTitle>
            {hits.length === 0 ? (
              <p className="flex items-center gap-1.5 rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                <CheckCircle2 className="size-3.5 text-emerald-500" />未命中任何名单
              </p>
            ) : (
              <CurrentListEntries entries={hits} />
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>关闭</Button>
          <Button size="sm" disabled={checkMut.isPending} onClick={run}>
            {checkMut.isPending && <Loader2 className="size-3.5 animate-spin" />}检测
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const KIND_ICONS: Record<string, typeof Ban> = { block: Ban, allow: ShieldCheck, watch: Eye };

/**
 * 现行名单：详情抽屉里「这个对象现在在哪些名单上」。可直接移除。
 */
export function CurrentListEntries({ entries, removable }: { entries?: RiskListEntry[] | null; removable?: boolean }) {
  const { listTypeLabel } = useRiskCatalog();
  const deleteMut = useDeleteRiskListMutation();
  const list = entries ?? [];
  if (list.length === 0) {
    return <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">不在任何名单上</p>;
  }
  return (
    <div className="space-y-1">
      {list.map((entry) => {
        const Icon = KIND_ICONS[entry.kind] ?? Eye;
        return (
          <div key={entry.id} className="group flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[11px]">
            <Icon className={cn("size-3.5 shrink-0",
              entry.kind === "block" ? "text-rose-500" : entry.kind === "allow" ? "text-emerald-500" : "text-amber-500")} />
            <ListKindBadge kind={entry.kind} />
            <span className="shrink-0 text-muted-foreground">{listTypeLabel(entry.listType)}</span>
            <code className="min-w-0 flex-1 truncate font-mono" title={entry.value}>{entry.value}</code>
            <ScopeBadge appId={entry.appId} />
            <span className="shrink-0 text-muted-foreground">{fmtExpiry(entry.expiresAt)}</span>
            {removable && (
              <Button variant="ghost" size="sm" className="h-6 px-1.5 text-destructive opacity-0 group-hover:opacity-100"
                disabled={deleteMut.isPending}
                onClick={async () => {
                  try {
                    await deleteMut.mutateAsync([entry.id]);
                    toast.success("已移出名单");
                  } catch (error) {
                    toast.error(error instanceof ApiError ? error.message : "移除失败");
                  }
                }}>
                <Trash2 className="size-3" />
              </Button>
            )}
          </div>
        );
      })}
    </div>
  );
}
