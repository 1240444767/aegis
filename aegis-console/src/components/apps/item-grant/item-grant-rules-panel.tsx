"use client";

import { useMemo, useState } from "react";
import {
  Archive,
  CirclePause,
  CirclePlay,
  Copy,
  FilePlus2,
  History,
  Loader2,
  MoreHorizontal,
  PencilRuler,
  Play,
  Plus,
  ReceiptText,
  RotateCcw,
  Search,
  Trash2,
  Workflow
} from "lucide-react";
import { toast } from "sonner";
import { ItemGrantEditor } from "@/components/apps/item-grant/item-grant-editor";
import {
  NODE_VISUAL,
  STATUS_META,
  TRIGGER_ICON,
  TRIGGER_LABEL,
  formatNumber
} from "@/components/apps/item-grant/item-grant-meta";
import { StateBlock, ToneIcon, type Tone } from "@/components/users/detail/kit";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { ItemGrantCatalog, ItemGrantGraph, ItemGrantNodeType, ItemGrantRule, ItemGrantStatus } from "@/lib/api/item-grant";
import { ApiError } from "@/lib/api/client";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import {
  useCancelItemGrantRunMutation,
  useDeleteItemGrantRuleMutation,
  useItemGrantCatalogQuery,
  useItemGrantRulesQuery,
  useRunItemGrantRuleMutation,
  useSaveItemGrantRuleMutation,
  useSetItemGrantRuleStatusMutation
} from "@/lib/item-grant-hooks";
import { cn } from "@/lib/utils";

/**
 * 发放规则列表：每条规则一张卡片 —— 触发方式、状态、画布里有哪些节点、发放统计、正在执行的进度。
 * 新建时从空白画布或内置模板开始，建好直接进入画布编辑器。
 */

const TRIGGER_TONE: Record<string, Tone> = { manual: "primary", schedule: "violet", event: "warning" };

type StatusFilter = "all" | ItemGrantStatus;

function errorText(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

export function ItemGrantRulesPanel({
  appKey,
  onOpenRecords,
  onOpenRuns
}: {
  appKey: string;
  onOpenRecords: (ruleId: number) => void;
  onOpenRuns: (ruleId: number) => void;
}) {
  const rulesQuery = useItemGrantRulesQuery(appKey);
  const catalogQuery = useItemGrantCatalogQuery(appKey);
  const [filter, setFilter] = useState<StatusFilter>("all");
  const [keyword, setKeyword] = useState("");
  const [editing, setEditing] = useState<number | null>(null);
  const [creating, setCreating] = useState(false);

  const rules = useMemo(() => {
    const list = rulesQuery.data ?? [];
    const term = keyword.trim().toLowerCase();
    return list.filter(
      (rule) =>
        (filter === "all" ? rule.status !== "archived" : rule.status === filter) &&
        (!term || rule.name.toLowerCase().includes(term) || rule.description.toLowerCase().includes(term))
    );
  }, [rulesQuery.data, filter, keyword]);

  const counts = useMemo(() => {
    const list = rulesQuery.data ?? [];
    return {
      all: list.filter((rule) => rule.status !== "archived").length,
      active: list.filter((rule) => rule.status === "active").length,
      draft: list.filter((rule) => rule.status === "draft").length,
      paused: list.filter((rule) => rule.status === "paused").length,
      archived: list.filter((rule) => rule.status === "archived").length
    };
  }, [rulesQuery.data]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          value={filter}
          onValueChange={(value) => value && setFilter(value as StatusFilter)}
        >
          {(
            [
              ["all", "全部"],
              ["active", "生效中"],
              ["draft", "草稿"],
              ["paused", "已暂停"],
              ["archived", "已归档"]
            ] as const
          ).map(([value, label]) => (
            <ToggleGroupItem key={value} value={value} className="gap-1 px-2.5 text-xs">
              {label}
              <span className="tabular-nums text-muted-foreground">{counts[value]}</span>
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <div className="relative ml-auto w-full sm:w-56">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-8 pl-8 text-xs" value={keyword} placeholder="搜索规则" onChange={(event) => setKeyword(event.target.value)} />
        </div>
        <Button size="sm" onClick={() => setCreating(true)} disabled={!catalogQuery.data}>
          <Plus /> 新建规则
        </Button>
      </div>

      {rulesQuery.isLoading ? (
        <div className="grid gap-3 lg:grid-cols-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-48 rounded-2xl" />
          ))}
        </div>
      ) : rulesQuery.error ? (
        <StateBlock kind="error" description={errorText(rulesQuery.error, "规则加载失败")} onRetry={() => void rulesQuery.refetch()} />
      ) : rules.length === 0 ? (
        <div className="rounded-2xl border border-dashed">
          <StateBlock
            icon={Workflow}
            title={filter === "all" && !keyword ? "还没有发放规则" : "没有符合条件的规则"}
            description="在画布上编排「触发 → 筛选 → 分流 / 限额 → 发放 → 站内信」，可以手动执行、定时执行，或在用户注册、登录、签到、支付时自动发放。"
            action={
              <Button size="sm" onClick={() => setCreating(true)} disabled={!catalogQuery.data}>
                <Plus /> 新建规则
              </Button>
            }
          />
        </div>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {rules.map((rule) => (
            <RuleCard
              key={rule.id}
              appKey={appKey}
              rule={rule}
              catalog={catalogQuery.data}
              onEdit={() => setEditing(rule.id)}
              onOpenRecords={() => onOpenRecords(rule.id)}
              onOpenRuns={() => onOpenRuns(rule.id)}
            />
          ))}
        </div>
      )}

      {catalogQuery.data ? (
        <CreateRuleDialog
          appKey={appKey}
          catalog={catalogQuery.data}
          open={creating}
          onOpenChange={setCreating}
          onCreated={(rule) => setEditing(rule.id)}
        />
      ) : null}
      <ItemGrantEditor appKey={appKey} ruleId={editing} open={editing !== null} onOpenChange={(open) => !open && setEditing(null)} />
    </div>
  );
}

function nodeCounts(graph: ItemGrantGraph) {
  const counts = new Map<ItemGrantNodeType, number>();
  for (const node of graph.nodes ?? []) counts.set(node.type, (counts.get(node.type) ?? 0) + 1);
  return Array.from(counts.entries()).filter(([type]) => type !== "trigger" && type !== "end");
}

function triggerDetail(rule: ItemGrantRule, catalog?: ItemGrantCatalog) {
  if (rule.trigger === "schedule") {
    return rule.status === "active" && rule.nextRunAt ? `${rule.cron}，下次 ${formatDateTime(rule.nextRunAt)}` : rule.cron || "未设置";
  }
  if (rule.trigger === "event") return catalog?.events.find((event) => event.key === rule.event)?.label ?? rule.event ?? "未选择事件";
  return rule.lastRunAt ? `上次执行 ${formatRelative(rule.lastRunAt)}` : "在这里点「执行」开始发放";
}

function RuleCard({
  appKey,
  rule,
  catalog,
  onEdit,
  onOpenRecords,
  onOpenRuns
}: {
  appKey: string;
  rule: ItemGrantRule;
  catalog?: ItemGrantCatalog;
  onEdit: () => void;
  onOpenRecords: () => void;
  onOpenRuns: () => void;
}) {
  const setStatus = useSetItemGrantRuleStatusMutation(appKey);
  const runRule = useRunItemGrantRuleMutation(appKey);
  const cancelRun = useCancelItemGrantRunMutation(appKey);
  const deleteRule = useDeleteItemGrantRuleMutation(appKey);
  const save = useSaveItemGrantRuleMutation(appKey);
  const [confirm, setConfirm] = useState<"run" | "delete" | "archive" | "cancel" | null>(null);

  const status = STATUS_META[rule.status];
  const TriggerIcon = TRIGGER_ICON[rule.trigger];
  const running = rule.running;
  const progress = running && running.total > 0 ? Math.min(100, (running.processed / running.total) * 100) : 0;
  const stats = rule.stats;

  const changeStatus = (next: ItemGrantStatus, message: string) =>
    setStatus.mutate(
      { ruleId: rule.id, status: next },
      { onSuccess: () => toast.success(message), onError: (error) => toast.error(errorText(error, "操作失败")) }
    );

  const duplicate = () =>
    save.mutate(
      { payload: { name: `${rule.name} 副本`.slice(0, 40), description: rule.description, graph: rule.graph } },
      { onSuccess: () => toast.success("已复制为草稿"), onError: (error) => toast.error(errorText(error, "复制失败")) }
    );

  return (
    <div
      className={cn(
        "group flex flex-col rounded-2xl border bg-card transition-colors hover:border-primary/30",
        rule.status === "archived" && "opacity-70"
      )}
    >
      <div className="flex items-start gap-3 p-4 pb-3">
        <ToneIcon icon={TriggerIcon} tone={TRIGGER_TONE[rule.trigger]} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <button type="button" className="truncate text-left text-sm font-semibold hover:underline" onClick={onEdit}>
              {rule.name}
            </button>
            <Badge variant={status.variant} size="sm">
              {status.label}
            </Badge>
          </div>
          <div className="mt-0.5 truncate text-xs text-muted-foreground">
            {TRIGGER_LABEL[rule.trigger]}：{triggerDetail(rule, catalog)}
          </div>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" className="size-8 shrink-0" aria-label="更多操作">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuItem onSelect={onEdit}>
              <PencilRuler /> 编辑画布
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={duplicate}>
              <Copy /> 复制为草稿
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={onOpenRecords}>
              <ReceiptText /> 发放记录
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={onOpenRuns}>
              <History /> 执行记录
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {rule.status === "archived" ? (
              <DropdownMenuItem onSelect={() => changeStatus("draft", "已恢复为草稿")}>
                <RotateCcw /> 恢复为草稿
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem onSelect={() => setConfirm("archive")}>
                <Archive /> 归档
              </DropdownMenuItem>
            )}
            <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={() => setConfirm("delete")}>
              <Trash2 /> 删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {rule.description ? <p className="line-clamp-2 px-4 text-xs leading-5 text-muted-foreground">{rule.description}</p> : null}

      <div className="flex flex-wrap gap-1.5 px-4 pt-3">
        {nodeCounts(rule.graph).map(([type, count]) => {
          const visual = NODE_VISUAL[type];
          const Icon = visual.icon;
          const label = catalog?.nodes.find((spec) => spec.type === type)?.label ?? type;
          return (
            <span key={type} className="inline-flex items-center gap-1 rounded-md border bg-muted/30 px-1.5 py-0.5 text-[11px]">
              <Icon className="size-3" style={{ color: visual.accent }} />
              {label}
              {count > 1 ? <span className="tabular-nums text-muted-foreground">×{count}</span> : null}
            </span>
          );
        })}
      </div>

      <div className="mt-3 grid grid-cols-4 divide-x border-y bg-muted/20">
        {[
          { label: "累计发放", value: stats?.records ?? 0 },
          { label: "覆盖人数", value: stats?.users ?? 0 },
          { label: "今日", value: stats?.today ?? 0 },
          { label: stats?.failed ? "失败" : "待处理", value: stats?.failed ? stats.failed : stats?.pending ?? 0, warn: Boolean(stats?.failed || stats?.pending) }
        ].map((item) => (
          <button key={item.label} type="button" className="px-3 py-2 text-left transition-colors hover:bg-accent/40" onClick={onOpenRecords}>
            <div className="text-[11px] text-muted-foreground">{item.label}</div>
            <div className={cn("text-sm font-semibold tabular-nums", item.warn && "text-amber-600 dark:text-amber-400")}>
              {formatNumber(item.value)}
            </div>
          </button>
        ))}
      </div>

      {running ? (
        <div className="space-y-1.5 border-b bg-sky-500/5 px-4 py-2.5">
          <div className="flex items-center justify-between gap-2 text-xs">
            <span className="flex items-center gap-1.5 font-medium text-sky-700 dark:text-sky-300">
              <Loader2 className="size-3.5 animate-spin" />
              正在执行 {formatNumber(running.processed)} / {formatNumber(running.total)}
            </span>
            <button type="button" className="text-muted-foreground hover:text-destructive" onClick={() => setConfirm("cancel")}>
              取消
            </button>
          </div>
          <Progress value={progress} className="h-1.5" />
          <div className="text-[11px] text-muted-foreground">
            到账 {formatNumber(running.granted)}，跳过 {formatNumber(running.skipped)}，失败 {formatNumber(running.failed)}
          </div>
        </div>
      ) : null}

      <div className="mt-auto flex items-center gap-2 px-4 py-3">
        <Button size="sm" variant="secondary" onClick={onEdit}>
          <PencilRuler /> 编辑画布
        </Button>
        {rule.status === "active" && rule.trigger !== "event" ? (
          <Button size="sm" variant="outline" disabled={Boolean(running) || runRule.isPending} onClick={() => setConfirm("run")}>
            {runRule.isPending ? <Loader2 className="animate-spin" /> : <Play />}
            执行
          </Button>
        ) : null}
        <div className="ml-auto flex items-center gap-1">
          {rule.status === "active" ? (
            <Button size="sm" variant="ghost" onClick={() => changeStatus("paused", "已暂停")} disabled={setStatus.isPending}>
              <CirclePause /> 暂停
            </Button>
          ) : rule.status !== "archived" ? (
            <Button size="sm" variant="ghost" onClick={() => changeStatus("active", "已上线")} disabled={setStatus.isPending}>
              {setStatus.isPending ? <Loader2 className="animate-spin" /> : <CirclePlay />}
              上线
            </Button>
          ) : null}
        </div>
      </div>

      <ConfirmActionDialog
        open={confirm === "run"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={`执行「${rule.name}」`}
        description="对目标人群逐人判定并发放，后台执行、可随时取消；已发出的不会收回。同一次执行里每人最多发一次。"
        confirmLabel="开始执行"
        pending={runRule.isPending}
        onConfirm={() =>
          runRule.mutate(rule.id, {
            onSuccess: (run) => {
              toast.success(`已开始执行，目标 ${formatNumber(run.total)} 人`);
              setConfirm(null);
            },
            onError: (error) => toast.error(errorText(error, "执行失败"))
          })
        }
      />
      <ConfirmActionDialog
        open={confirm === "cancel"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="取消执行"
        description="当前这一批处理完就停下，已经发出的不会收回。"
        confirmLabel="取消执行"
        destructive
        pending={cancelRun.isPending}
        onConfirm={() =>
          running &&
          cancelRun.mutate(running.id, {
            onSuccess: () => {
              toast.success("已取消");
              setConfirm(null);
            },
            onError: (error) => toast.error(errorText(error, "取消失败"))
          })
        }
      />
      <ConfirmActionDialog
        open={confirm === "archive"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={`归档「${rule.name}」`}
        description="归档后规则不再触发与执行，发放记录保留；可以随时恢复为草稿。"
        confirmLabel="归档"
        pending={setStatus.isPending}
        onConfirm={() => {
          changeStatus("archived", "已归档");
          setConfirm(null);
        }}
      />
      <ConfirmActionDialog
        open={confirm === "delete"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={`删除「${rule.name}」`}
        description="只有没有发放记录的规则可以删除；有记录的请归档。删除后不可恢复。"
        confirmLabel="删除"
        destructive
        pending={deleteRule.isPending}
        onConfirm={() =>
          deleteRule.mutate(rule.id, {
            onSuccess: () => {
              toast.success("已删除");
              setConfirm(null);
            },
            onError: (error) => toast.error(errorText(error, "删除失败"))
          })
        }
      />
    </div>
  );
}

function CreateRuleDialog({
  appKey,
  catalog,
  open,
  onOpenChange,
  onCreated
}: {
  appKey: string;
  catalog: ItemGrantCatalog;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (rule: ItemGrantRule) => void;
}) {
  const save = useSaveItemGrantRuleMutation(appKey);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [template, setTemplate] = useState<string>("blank");

  const choose = (key: string) => {
    setTemplate(key);
    const picked = catalog.templates.find((item) => item.key === key);
    if (picked && !name.trim()) setName(picked.name);
    if (picked && !description.trim()) setDescription(picked.description);
  };

  const submit = () => {
    const picked = catalog.templates.find((item) => item.key === template);
    save.mutate(
      {
        payload: {
          name: name.trim(),
          description: description.trim(),
          graph: picked ? picked.graph : { nodes: [], edges: [] }
        }
      },
      {
        onSuccess: (rule) => {
          toast.success("已创建草稿，开始编排吧");
          onOpenChange(false);
          setName("");
          setDescription("");
          setTemplate("blank");
          onCreated(rule);
        },
        onError: (error) => toast.error(errorText(error, "创建失败"))
      }
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>新建发放规则</DialogTitle>
          <DialogDescription>从空白画布或模板开始，建好后进入画布编辑；新规则是草稿，上线后才会生效。</DialogDescription>
        </DialogHeader>
        <div className="grid max-h-[50vh] gap-2 overflow-y-auto pr-1 sm:grid-cols-2 lg:grid-cols-3">
          <TemplateCard
            active={template === "blank"}
            title="空白画布"
            description="手动执行 → 全部用户 → 发 10 积分，从这里开始自由编排"
            graph={null}
            onClick={() => choose("blank")}
          />
          {catalog.templates.map((item) => (
            <TemplateCard
              key={item.key}
              active={template === item.key}
              title={item.name}
              description={item.description}
              graph={item.graph}
              onClick={() => choose(item.key)}
            />
          ))}
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label className="text-xs">规则名称</Label>
            <Input value={name} maxLength={40} placeholder="例如：新人礼包" onChange={(event) => setName(event.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">说明（可选）</Label>
            <Textarea
              className="min-h-9"
              rows={1}
              value={description}
              maxLength={500}
              onChange={(event) => setDescription(event.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={submit} disabled={!name.trim() || save.isPending}>
            {save.isPending ? <Loader2 className="animate-spin" /> : <FilePlus2 />}
            创建并编辑
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TemplateCard({
  active,
  title,
  description,
  graph,
  onClick
}: {
  active: boolean;
  title: string;
  description: string;
  graph: ItemGrantGraph | null;
  onClick: () => void;
}) {
  const types = graph ? graph.nodes.map((node) => node.type) : (["trigger", "audience", "grant"] as ItemGrantNodeType[]);
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex flex-col gap-2 rounded-xl border p-3 text-left transition-colors hover:border-primary/40",
        active && "border-primary bg-primary/5 ring-2 ring-primary/15"
      )}
    >
      <div className="flex items-center gap-1">
        {types.slice(0, 7).map((type, index) => {
          const visual = NODE_VISUAL[type];
          const Icon = visual.icon;
          return (
            <span key={index} className="flex items-center">
              {index > 0 ? <span className="mx-0.5 h-px w-2 bg-border" /> : null}
              <span className="flex size-5 items-center justify-center rounded-md" style={{ background: `${visual.accent}1f` }}>
                <Icon className="size-3" style={{ color: visual.accent }} />
              </span>
            </span>
          );
        })}
        {types.length > 7 ? <span className="ml-1 text-[10px] text-muted-foreground">+{types.length - 7}</span> : null}
      </div>
      <div className="text-xs font-semibold">{title}</div>
      <p className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{description}</p>
    </button>
  );
}

