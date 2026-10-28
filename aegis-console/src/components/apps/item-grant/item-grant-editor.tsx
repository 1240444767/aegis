"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  BackgroundVariant,
  ConnectionLineType,
  Controls,
  MiniMap,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Connection,
  type Edge
} from "@xyflow/react";
import { useTheme } from "next-themes";
import { useHotkeys } from "react-hotkeys-hook";
import {
  AlertTriangle,
  ArrowLeft,
  CircleAlert,
  CircleCheck,
  Footprints,
  LayoutGrid,
  ListChecks,
  Loader2,
  Maximize,
  PanelRight,
  Redo2,
  Save,
  Settings2,
  Undo2
} from "lucide-react";
import { toast } from "sonner";
import { CanvasOverlayContext, edgeTypes, nodeTypes } from "@/components/apps/item-grant/item-grant-canvas-nodes";
import { Field, FieldGroup } from "@/components/apps/item-grant/item-grant-fields";
import { fromFlow, graphSignature, toFlow, wouldCycle, type FlowNode } from "@/components/apps/item-grant/item-grant-graph";
import { NodeInspector } from "@/components/apps/item-grant/item-grant-inspector";
import { NODE_VISUAL, STATUS_META, TRIGGER_LABEL, formatNumber } from "@/components/apps/item-grant/item-grant-meta";
import { PreviewPanel } from "@/components/apps/item-grant/item-grant-preview-panel";
import {
  EditorStoreContext,
  createEditorStore,
  useEditor,
  useEditorApi,
  useEditorHistory
} from "@/components/apps/item-grant/item-grant-store";
import { ToneIcon } from "@/components/users/detail/kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import type {
  ItemGrantCatalog,
  ItemGrantEstimate,
  ItemGrantIssue,
  ItemGrantNodeType,
  ItemGrantPreview,
  ItemGrantRule,
  ItemGrantTriggerData
} from "@/lib/api/item-grant";
import { ApiError } from "@/lib/api/client";
import { formatDateTime, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import {
  useItemGrantCatalogQuery,
  useItemGrantRuleQuery,
  useSaveItemGrantRuleMutation,
  useValidateItemGrantMutation
} from "@/lib/item-grant-hooks";
import { cn } from "@/lib/utils";

/**
 * 规则画布编辑器（全屏）。
 *
 * | 区域 | 作用 |
 * |---|---|
 * | 顶栏 | 规则名、状态、撤销 / 重做、自动排版、适应画布、问题数、保存（⌘S） |
 * | 左侧节点库 | 按分类列出节点；拖到画布上，或点一下放在画布中央。触发器只能有一个 |
 * | 画布 | React Flow：连线按端口语义着色、不允许成环与连回触发器；小地图、缩放、网格吸附；Delete 删除、⌘D 复制 |
 * | 右侧 | 配置（选中节点的表单 / 未选中时是规则设置）、问题（实时校验，点击定位）、预演（抽样估算 / 单人追踪，结果叠加在画布上） |
 *
 * 画布每次变化 600ms 后自动调后端校验（与上线时同一套判据），问题以角标标在节点与连线上。
 */

const DRAG_TYPE = "application/x-item-grant-node";

export function ItemGrantEditor({
  appKey,
  ruleId,
  open,
  onOpenChange
}: {
  appKey: string;
  ruleId: number | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const catalogQuery = useItemGrantCatalogQuery(open ? appKey : null);
  const ruleQuery = useItemGrantRuleQuery(open ? appKey : null, open ? ruleId : null);
  const [confirmClose, setConfirmClose] = useState(false);
  const dirtyRef = useRef(false);

  const requestClose = () => {
    if (dirtyRef.current) setConfirmClose(true);
    else onOpenChange(false);
  };

  const ready = catalogQuery.data && ruleQuery.data && ruleQuery.data.id === ruleId;

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? onOpenChange(true) : requestClose())}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[100dvh] w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none border-0 p-0 sm:max-w-none"
        onEscapeKeyDown={(event) => event.preventDefault()}
      >
        <DialogTitle className="sr-only">编辑发放规则</DialogTitle>
        <DialogDescription className="sr-only">在画布上编排触发、筛选、分流、限额与发放</DialogDescription>
        <TooltipProvider delayDuration={300}>
          {ready ? (
            <ReactFlowProvider>
              <EditorBody
                key={ruleQuery.data.id}
                appKey={appKey}
                rule={ruleQuery.data}
                catalog={catalogQuery.data}
                onClose={requestClose}
                onDirtyChange={(dirty) => {
                  dirtyRef.current = dirty;
                }}
              />
            </ReactFlowProvider>
          ) : (
            <EditorSkeleton error={catalogQuery.error ?? ruleQuery.error} onClose={() => onOpenChange(false)} />
          )}
        </TooltipProvider>
      </DialogContent>
      <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle>
            <AlertDialogDescription>画布上还有没保存的改动，关闭后会丢失。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                dirtyRef.current = false;
                onOpenChange(false);
              }}
            >
              放弃修改
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  );
}

function EditorSkeleton({ error, onClose }: { error: unknown; onClose: () => void }) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex h-14 items-center gap-3 border-b px-4">
        <Button size="icon" variant="ghost" onClick={onClose} aria-label="返回">
          <ArrowLeft />
        </Button>
        <Skeleton className="h-5 w-48" />
      </div>
      {error ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {error instanceof Error ? error.message : "加载失败"}
        </div>
      ) : (
        <div className="grid flex-1 grid-cols-[240px_minmax(0,1fr)_360px]">
          <Skeleton className="m-3 rounded-xl" />
          <Skeleton className="m-3 rounded-xl opacity-60" />
          <Skeleton className="m-3 rounded-xl" />
        </div>
      )}
    </div>
  );
}

function EditorBody({
  appKey,
  rule,
  catalog,
  onClose,
  onDirtyChange
}: {
  appKey: string;
  rule: ItemGrantRule;
  catalog: ItemGrantCatalog;
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const [store] = useState(() => {
    const flow = toFlow(rule.graph);
    return createEditorStore(flow.nodes, flow.edges);
  });
  return (
    <EditorStoreContext.Provider value={store}>
      <EditorWorkspace appKey={appKey} rule={rule} catalog={catalog} onClose={onClose} onDirtyChange={onDirtyChange} />
    </EditorStoreContext.Provider>
  );
}

type RuleMeta = { name: string; description: string; startsAt: string; endsAt: string };

function metaOf(rule: ItemGrantRule): RuleMeta {
  return {
    name: rule.name,
    description: rule.description ?? "",
    startsAt: rule.startsAt ? toLocalInputValue(rule.startsAt) : "",
    endsAt: rule.endsAt ? toLocalInputValue(rule.endsAt) : ""
  };
}

function EditorWorkspace({
  appKey,
  rule,
  catalog,
  onClose,
  onDirtyChange
}: {
  appKey: string;
  rule: ItemGrantRule;
  catalog: ItemGrantCatalog;
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const { resolvedTheme } = useTheme();
  const flow = useReactFlow();
  const api = useEditorApi();
  const nodes = useEditor((state) => state.nodes);
  const edges = useEditor((state) => state.edges);
  const onNodesChange = useEditor((state) => state.onNodesChange);
  const onEdgesChange = useEditor((state) => state.onEdgesChange);
  const connect = useEditor((state) => state.connect);
  const addNode = useEditor((state) => state.addNode);
  const layout = useEditor((state) => state.layout);
  const duplicate = useEditor((state) => state.duplicate);
  const select = useEditor((state) => state.select);
  const canUndo = useEditorHistory((state) => state.pastStates.length > 0);
  const canRedo = useEditorHistory((state) => state.futureStates.length > 0);

  const [meta, setMeta] = useState<RuleMeta>(() => metaOf(rule));
  const [savedMeta, setSavedMeta] = useState<RuleMeta>(() => metaOf(rule));
  const [savedSignature, setSavedSignature] = useState(() => graphSignature(nodes, edges));
  const [tab, setTab] = useState("config");
  // 窄于 xl 时右侧面板是浮在画布上的抽屉
  const [panelOpen, setPanelOpen] = useState(true);
  const [issues, setIssues] = useState<ItemGrantIssue[]>([]);
  const [estimate, setEstimate] = useState<ItemGrantEstimate | null>(null);
  const [trace, setTrace] = useState<ItemGrantPreview["trace"] | null>(null);
  const [version, setVersion] = useState(rule.version);
  const save = useSaveItemGrantRuleMutation(appKey);
  const validate = useValidateItemGrantMutation(appKey);
  // react-query 的 mutate 引用稳定，可以放进依赖
  const runValidate = validate.mutate;

  const signature = useMemo(() => graphSignature(nodes, edges), [nodes, edges]);
  const dirty = signature !== savedSignature || JSON.stringify(meta) !== JSON.stringify(savedMeta);
  useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange]);

  const selectedId = useMemo(() => {
    const selected = nodes.filter((node) => node.selected);
    return selected.length === 1 ? selected[0].id : null;
  }, [nodes]);
  const trigger = nodes.find((node) => node.type === "trigger");
  const triggerData = trigger?.data as ItemGrantTriggerData | undefined;

  // 画布一变，旧的预演结果就不再对应当前配置
  const firstSignature = useRef(signature);
  useEffect(() => {
    if (signature === firstSignature.current) return;
    setEstimate(null);
    setTrace(null);
  }, [signature]);

  // 实时校验：停手 600ms 后调后端（与上线时同一套判据）
  useEffect(() => {
    const timer = setTimeout(() => {
      const { nodes: current, edges: currentEdges } = api.getState();
      runValidate(fromFlow(current, currentEdges), { onSuccess: (data) => setIssues(data.issues) });
    }, 600);
    return () => clearTimeout(timer);
  }, [signature, api, runValidate]);

  const issueMap = useMemo(() => {
    const map = new Map<string, ItemGrantIssue[]>();
    for (const issue of issues) {
      if (issue.nodeId) map.set(issue.nodeId, [...(map.get(issue.nodeId) ?? []), issue]);
    }
    return map;
  }, [issues]);
  const edgeIssues = useMemo(() => new Set(issues.filter((issue) => issue.edgeId).map((issue) => issue.edgeId as string)), [issues]);
  const errorCount = issues.filter((issue) => issue.level === "error").length;
  const warningCount = issues.length - errorCount;

  const overlay = useMemo(
    () => ({ catalog, issues: issueMap, edgeIssues, estimate, trace: trace?.outcome ?? null }),
    [catalog, issueMap, edgeIssues, estimate, trace]
  );

  const currentGraph = useCallback(() => {
    const { nodes: current, edges: currentEdges } = api.getState();
    return fromFlow(current, currentEdges, flow.getViewport());
  }, [api, flow]);

  const submit = () => {
    if (!meta.name.trim()) {
      toast.error("规则名称不能为空");
      setTab("config");
      select(null);
      return;
    }
    const graph = currentGraph();
    const sentSignature = graphSignature(api.getState().nodes, api.getState().edges);
    save.mutate(
      {
        ruleId: rule.id,
        payload: {
          name: meta.name.trim(),
          description: meta.description.trim(),
          startsAt: fromLocalInputValue(meta.startsAt) ?? null,
          endsAt: fromLocalInputValue(meta.endsAt) ?? null,
          graph
        }
      },
      {
        onSuccess: (saved) => {
          setSavedSignature(sentSignature);
          setSavedMeta(meta);
          setVersion(saved.version);
          toast.success(`已保存（第 ${saved.version} 版）`);
        },
        onError: (error) => toast.error(error instanceof ApiError ? error.message : "保存失败")
      }
    );
  };

  useHotkeys(
    "mod+s",
    (event) => {
      event.preventDefault();
      if (!save.isPending) submit();
    },
    { enableOnFormTags: true, enableOnContentEditable: true }
  );
  useHotkeys("mod+z", (event) => {
    event.preventDefault();
    api.temporal.getState().undo();
  });
  useHotkeys(["mod+shift+z", "mod+y"], (event) => {
    event.preventDefault();
    api.temporal.getState().redo();
  });
  useHotkeys("mod+d", (event) => {
    event.preventDefault();
    const ids = api.getState().nodes.filter((node) => node.selected).map((node) => node.id);
    if (ids.length) duplicate(ids);
  });

  const isValidConnection = useCallback(
    (connection: Connection | Edge) => {
      const { nodes: current, edges: currentEdges } = api.getState();
      if (!connection.source || !connection.target || connection.source === connection.target) return false;
      if (current.find((node) => node.id === connection.target)?.type === "trigger") return false;
      if (
        currentEdges.some(
          (edge) =>
            edge.source === connection.source &&
            (edge.sourceHandle ?? null) === (connection.sourceHandle ?? null) &&
            edge.target === connection.target
        )
      ) {
        return false;
      }
      return !wouldCycle(currentEdges, connection.source, connection.target);
    },
    [api]
  );

  const placeNode = (type: ItemGrantNodeType, position?: { x: number; y: number }) => {
    if (type === "trigger" && api.getState().nodes.some((node) => node.type === "trigger")) {
      toast.error("一条规则只能有一个触发器");
      return;
    }
    const at =
      position ??
      flow.screenToFlowPosition({
        x: window.innerWidth / 2 - 132 + Math.random() * 40,
        y: window.innerHeight / 2 - 60 + Math.random() * 40
      });
    addNode(type, at, catalog);
    setTab("config");
  };

  const focusNode = (id: string) => {
    const node = api.getState().nodes.find((candidate) => candidate.id === id);
    if (!node) return;
    select(id);
    setTab("config");
    void flow.setCenter(node.position.x + 132, node.position.y + 60, { zoom: Math.max(flow.getZoom(), 1), duration: 400 });
  };

  const status = STATUS_META[rule.status];

  return (
    <CanvasOverlayContext.Provider value={overlay}>
      <style>{`@keyframes item-grant-dash { to { stroke-dashoffset: -20; } }`}</style>
      <div className="flex h-full flex-col">
        {/* 顶栏 */}
        <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-background/95 px-3 backdrop-blur">
          <Button size="icon" variant="ghost" onClick={onClose} aria-label="返回">
            <ArrowLeft />
          </Button>
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <Input
              className="h-8 max-w-72 border-transparent bg-transparent px-2 text-sm font-semibold shadow-none hover:border-border focus-visible:border-ring"
              value={meta.name}
              maxLength={40}
              onChange={(event) => setMeta({ ...meta, name: event.target.value })}
              aria-label="规则名称"
            />
            <Badge variant={status.variant} size="sm">
              {status.label}
            </Badge>
            <span className="hidden text-xs text-muted-foreground md:inline">
              第 {version} 版，{triggerData ? TRIGGER_LABEL[triggerData.kind] : "缺少触发器"}
            </span>
            {dirty ? (
              <span className="hidden items-center gap-1 text-xs text-amber-600 sm:flex dark:text-amber-400">
                <span className="size-1.5 rounded-full bg-amber-500" />
                未保存
              </span>
            ) : null}
          </div>
          <div className="flex items-center gap-1">
            <ToolbarButton label="撤销（⌘Z）" disabled={!canUndo} onClick={() => api.temporal.getState().undo()}>
              <Undo2 />
            </ToolbarButton>
            <ToolbarButton label="重做（⌘⇧Z）" disabled={!canRedo} onClick={() => api.temporal.getState().redo()}>
              <Redo2 />
            </ToolbarButton>
            <ToolbarButton
              label="自动排版"
              onClick={() => {
                layout();
                window.requestAnimationFrame(() => void flow.fitView({ padding: 0.2, duration: 400 }));
              }}
            >
              <LayoutGrid />
            </ToolbarButton>
            <ToolbarButton label="适应画布" onClick={() => void flow.fitView({ padding: 0.2, duration: 400 })}>
              <Maximize />
            </ToolbarButton>
            <span className="xl:hidden">
              <ToolbarButton label={panelOpen ? "收起面板" : "展开面板"} onClick={() => setPanelOpen(!panelOpen)}>
                <PanelRight />
              </ToolbarButton>
            </span>
            <span className="mx-1 h-5 w-px bg-border" />
            <Button
              variant="ghost"
              size="sm"
              className={cn("gap-1.5", errorCount ? "text-red-600 dark:text-red-400" : warningCount ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400")}
              onClick={() => setTab("issues")}
            >
              {validate.isPending ? (
                <Loader2 className="animate-spin" />
              ) : errorCount ? (
                <CircleAlert />
              ) : warningCount ? (
                <AlertTriangle />
              ) : (
                <CircleCheck />
              )}
              <span className="hidden sm:inline">{errorCount ? `${errorCount} 个错误` : warningCount ? `${warningCount} 个提醒` : "校验通过"}</span>
            </Button>
            <Button size="sm" onClick={submit} disabled={save.isPending || !dirty}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存
            </Button>
          </div>
        </header>

        <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[232px_minmax(0,1fr)]">
          {/* 节点库 */}
          <aside className="hidden min-h-0 flex-col overflow-y-auto border-r bg-muted/20 md:flex">
            <NodePalette catalog={catalog} hasTrigger={Boolean(trigger)} onAdd={(type) => placeNode(type)} />
          </aside>

          <div className="relative flex min-h-0">
            {/* 画布 */}
            <div
              className="relative min-h-0 min-w-0 flex-1"
              onDragOver={(event) => {
                if (event.dataTransfer.types.includes(DRAG_TYPE)) {
                  event.preventDefault();
                  event.dataTransfer.dropEffect = "move";
                }
              }}
              onDrop={(event) => {
                const type = event.dataTransfer.getData(DRAG_TYPE) as ItemGrantNodeType;
                if (!type) return;
                event.preventDefault();
                const position = flow.screenToFlowPosition({ x: event.clientX - 132, y: event.clientY - 30 });
                placeNode(type, position);
              }}
            >
              <ReactFlow<FlowNode>
                nodes={nodes}
                edges={edges}
                nodeTypes={nodeTypes}
                edgeTypes={edgeTypes}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                onConnect={connect}
                isValidConnection={isValidConnection}
                onPaneClick={() => select(null)}
                colorMode={resolvedTheme === "dark" ? "dark" : "light"}
                defaultViewport={rule.graph.viewport}
                fitView={!rule.graph.viewport}
                fitViewOptions={{ padding: 0.2, maxZoom: 1.1 }}
                minZoom={0.2}
                maxZoom={1.8}
                snapToGrid
                snapGrid={[8, 8]}
                deleteKeyCode={["Backspace", "Delete"]}
                multiSelectionKeyCode={["Meta", "Control", "Shift"]}
                connectionLineType={ConnectionLineType.SmoothStep}
                connectionLineStyle={{ strokeWidth: 2, stroke: "var(--primary)" }}
                proOptions={{ hideAttribution: true }}
                className="bg-muted/10"
              >
                <Background variant={BackgroundVariant.Dots} gap={16} size={1.2} />
                <MiniMap
                  pannable
                  zoomable
                  className="!rounded-xl !border !bg-background/90"
                  nodeBorderRadius={12}
                  nodeColor={(node) => NODE_VISUAL[node.type as ItemGrantNodeType]?.accent ?? "#94a3b8"}
                  maskColor="color-mix(in oklab, var(--background) 70%, transparent)"
                />
                <Controls showInteractive={false} className="!rounded-xl !border !shadow-sm" />
                {nodes.length === 0 ? (
                  <Panel position="top-center" className="!mt-24">
                    <div className="rounded-2xl border border-dashed bg-background/90 px-6 py-5 text-center text-sm text-muted-foreground">
                      从左侧拖一个「触发器」到这里开始
                    </div>
                  </Panel>
                ) : null}
                {trace ? (
                  <Panel position="top-left">
                    <div className="flex items-center gap-2 rounded-xl border bg-background/95 px-3 py-1.5 text-xs shadow-sm">
                      <Footprints className="size-3.5 text-emerald-500" />
                      正在显示 {trace.user.nickname || trace.user.account} 的判定路径
                      <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setTrace(null)}>
                        清除
                      </button>
                    </div>
                  </Panel>
                ) : estimate ? (
                  <Panel position="top-left">
                    <div className="flex items-center gap-2 rounded-xl border bg-background/95 px-3 py-1.5 text-xs shadow-sm">
                      抽样 {estimate.sampled} 人的到达分布，目标人群 {formatNumber(estimate.candidates)} 人
                      <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setEstimate(null)}>
                        清除
                      </button>
                    </div>
                  </Panel>
                ) : null}
              </ReactFlow>
            </div>

            {/* 右侧 */}
            <aside
              className={cn(
                "min-h-0 w-[min(380px,100%)] flex-col border-l bg-background",
                "absolute inset-y-0 right-0 z-10 shadow-xl xl:static xl:z-auto xl:flex xl:shadow-none",
                panelOpen ? "flex" : "hidden"
              )}
            >
              <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-0">
                <div className="border-b px-3 py-2">
                  <TabsList className="w-full">
                    <TabsTrigger value="config" className="flex-1 text-xs">
                      <Settings2 /> 配置
                    </TabsTrigger>
                    <TabsTrigger value="issues" className="flex-1 text-xs">
                      <ListChecks /> 问题
                      {issues.length ? (
                        <span
                          className={cn(
                            "ml-0.5 rounded-full px-1.5 text-[10px] tabular-nums text-white",
                            errorCount ? "bg-red-500" : "bg-amber-500"
                          )}
                        >
                          {issues.length}
                        </span>
                      ) : null}
                    </TabsTrigger>
                    <TabsTrigger value="preview" className="flex-1 text-xs">
                      <Footprints /> 预演
                    </TabsTrigger>
                  </TabsList>
                </div>
                <TabsContent value="config" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                  {selectedId ? (
                    <NodeInspector nodeId={selectedId} catalog={catalog} issues={issueMap.get(selectedId) ?? []} />
                  ) : (
                    <RuleSettings rule={rule} meta={meta} onChange={setMeta} nodeCount={nodes.length} edgeCount={edges.length} />
                  )}
                </TabsContent>
                <TabsContent value="issues" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                  <IssueList issues={issues} catalog={catalog} nodes={nodes} onFocus={focusNode} />
                </TabsContent>
                <TabsContent value="preview" className="min-h-0 flex-1 overflow-y-auto px-4 py-4" forceMount hidden={tab !== "preview"}>
                  <PreviewPanel
                    appKey={appKey}
                    ruleId={rule.id}
                    catalog={catalog}
                    graph={currentGraph}
                    eventKey={triggerData?.kind === "event" ? triggerData.event : undefined}
                    onEstimate={(next) => {
                      setEstimate(next);
                      if (next) setTrace(null);
                    }}
                    onTrace={(next) => {
                      setTrace(next);
                      if (next) setEstimate(null);
                    }}
                  />
                </TabsContent>
              </Tabs>
            </aside>
          </div>
        </div>
      </div>
    </CanvasOverlayContext.Provider>
  );
}

function ToolbarButton({
  label,
  disabled,
  onClick,
  children
}: {
  label: string;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon" variant="ghost" className="size-8" disabled={disabled} onClick={onClick} aria-label={label}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function NodePalette({
  catalog,
  hasTrigger,
  onAdd
}: {
  catalog: ItemGrantCatalog;
  hasTrigger: boolean;
  onAdd: (type: ItemGrantNodeType) => void;
}) {
  return (
    <div className="space-y-4 p-3">
      <div>
        <div className="text-xs font-semibold">节点库</div>
        <p className="mt-0.5 text-[11px] leading-4 text-muted-foreground">拖到画布上，或点击添加到中央</p>
      </div>
      {catalog.categories.map((category) => {
        const specs = catalog.nodes.filter((spec) => spec.category === category.key);
        if (specs.length === 0) return null;
        return (
          <div key={category.key} className="space-y-1.5">
            <div className="px-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{category.label}</div>
            {specs.map((spec) => {
              const visual = NODE_VISUAL[spec.type];
              const disabled = spec.singleton && hasTrigger;
              const outputs = spec.outputs ?? [];
              return (
                <Tooltip key={spec.type}>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      draggable={!disabled}
                      disabled={disabled}
                      onDragStart={(event) => {
                        event.dataTransfer.setData(DRAG_TYPE, spec.type);
                        event.dataTransfer.effectAllowed = "move";
                      }}
                      onClick={() => onAdd(spec.type)}
                      className={cn(
                        "group flex w-full cursor-grab items-center gap-2.5 rounded-xl border bg-background px-2.5 py-2 text-left transition-colors hover:border-primary/40 hover:bg-accent/40 active:cursor-grabbing",
                        disabled && "cursor-not-allowed opacity-50 hover:border-border hover:bg-background"
                      )}
                    >
                      <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
                      <div className="min-w-0">
                        <div className="truncate text-xs font-medium">{spec.label}</div>
                        <div className="truncate text-[10px] text-muted-foreground">
                          {disabled ? "已有触发器" : outputs.length ? outputs.map((output) => output.label).join(" / ") : spec.dynamicOutputs ? "多个分支" : "终点"}
                        </div>
                      </div>
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="right" className="max-w-64">
                    {spec.hint}
                  </TooltipContent>
                </Tooltip>
              );
            })}
          </div>
        );
      })}
      <div className="rounded-xl border border-dashed p-2.5 text-[11px] leading-5 text-muted-foreground">
        <div className="font-medium text-foreground">快捷键</div>
        <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-2">
          {[
            ["⌘S", "保存"],
            ["⌘Z", "撤销"],
            ["⌘⇧Z", "重做"],
            ["⌘D", "复制"],
            ["Delete", "删除"],
            ["Shift", "多选"]
          ].map(([key, label]) => (
            <div key={key} className="contents">
              <dt className="font-mono">{key}</dt>
              <dd>{label}</dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}

function RuleSettings({
  rule,
  meta,
  onChange,
  nodeCount,
  edgeCount
}: {
  rule: ItemGrantRule;
  meta: RuleMeta;
  onChange: (meta: RuleMeta) => void;
  nodeCount: number;
  edgeCount: number;
}) {
  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3">
        <ToneIcon icon={Settings2} tone="neutral" size="lg" />
        <div>
          <div className="text-sm font-semibold">规则设置</div>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">选中画布上的节点可以编辑它的配置</p>
        </div>
      </div>
      <FieldGroup title="基本信息">
        <Field label="名称">
          <Input className="h-8" value={meta.name} maxLength={40} onChange={(event) => onChange({ ...meta, name: event.target.value })} />
        </Field>
        <Field label="说明">
          <Textarea
            className="min-h-20 text-xs"
            value={meta.description}
            maxLength={500}
            placeholder="这条规则在做什么、为什么"
            onChange={(event) => onChange({ ...meta, description: event.target.value })}
          />
        </Field>
      </FieldGroup>
      <FieldGroup title="生效期">
        <Field label="开始时间" hint="空为立即生效">
          <Input
            type="datetime-local"
            className="h-8"
            value={meta.startsAt}
            onChange={(event) => onChange({ ...meta, startsAt: event.target.value })}
          />
        </Field>
        <Field label="结束时间" hint="空为长期有效；过期后不再触发与执行">
          <Input type="datetime-local" className="h-8" value={meta.endsAt} onChange={(event) => onChange({ ...meta, endsAt: event.target.value })} />
        </Field>
      </FieldGroup>
      <FieldGroup title="概况">
        <dl className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
          <dt className="text-muted-foreground">节点 / 连线</dt>
          <dd className="tabular-nums">
            {nodeCount} / {edgeCount}
          </dd>
          <dt className="text-muted-foreground">累计发放</dt>
          <dd className="tabular-nums">{formatNumber(rule.stats?.records ?? 0)} 次</dd>
          <dt className="text-muted-foreground">覆盖人数</dt>
          <dd className="tabular-nums">{formatNumber(rule.stats?.users ?? 0)} 人</dd>
          <dt className="text-muted-foreground">下次执行</dt>
          <dd>{rule.nextRunAt ? formatDateTime(rule.nextRunAt) : "—"}</dd>
          <dt className="text-muted-foreground">最后修改</dt>
          <dd>
            {formatDateTime(rule.updatedAt)}
            {rule.updatedBy ? `（${rule.updatedBy}）` : ""}
          </dd>
        </dl>
      </FieldGroup>
      {rule.status === "active" ? (
        <p className="rounded-lg bg-amber-500/10 px-3 py-2 text-xs leading-5 text-amber-700 dark:text-amber-300">
          规则生效中：保存时画布不能有错误，保存后立即按新画布执行。
        </p>
      ) : null}
    </div>
  );
}

function IssueList({
  issues,
  catalog,
  nodes,
  onFocus
}: {
  issues: ItemGrantIssue[];
  catalog: ItemGrantCatalog;
  nodes: FlowNode[];
  onFocus: (id: string) => void;
}) {
  if (issues.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-12 text-center">
        <ToneIcon icon={CircleCheck} tone="success" size="lg" />
        <div className="text-sm font-medium">校验通过</div>
        <p className="max-w-64 text-xs leading-5 text-muted-foreground">结构、配置、表达式与引用的套餐、功能都没有问题，可以上线。</p>
      </div>
    );
  }
  const sorted = [...issues].sort((a, b) => (a.level === b.level ? 0 : a.level === "error" ? -1 : 1));
  return (
    <div className="space-y-2">
      {sorted.map((issue, index) => {
        const node = issue.nodeId ? nodes.find((candidate) => candidate.id === issue.nodeId) : undefined;
        const spec = node ? catalog.nodes.find((candidate) => candidate.type === node.type) : undefined;
        const label = node ? ((node.data.label as string) || spec?.label || node.type) : issue.edgeId ? "连线" : "整张画布";
        return (
          <button
            key={index}
            type="button"
            disabled={!node}
            onClick={() => node && onFocus(node.id)}
            className={cn(
              "flex w-full items-start gap-2.5 rounded-xl border px-3 py-2 text-left transition-colors",
              node && "hover:bg-accent/40",
              issue.level === "error" ? "border-red-500/30" : "border-amber-500/30"
            )}
          >
            {issue.level === "error" ? (
              <CircleAlert className="mt-0.5 size-4 shrink-0 text-red-500" />
            ) : (
              <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-500" />
            )}
            <div className="min-w-0">
              <div className="text-xs font-medium">{label}</div>
              <div className="text-xs leading-5 text-muted-foreground">{issue.message}</div>
            </div>
          </button>
        );
      })}
    </div>
  );
}
