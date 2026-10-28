"use client";

import "@flowgram.ai/free-layout-editor/index.css";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  EditorRenderer,
  FreeLayoutEditorProvider,
  LineType,
  WorkflowDragService,
  WorkflowLineEntity,
  WorkflowNodeEntity,
  WorkflowSelectService,
  delay,
  getNodeForm,
  useClientContext,
  usePlaygroundTools,
  useService,
  type FreeLayoutPluginContext,
  type FreeLayoutProps,
  type ShortcutsHandler,
  type WorkflowJSON,
  type WorkflowPortEntity
} from "@flowgram.ai/free-layout-editor";
import { createFreeSnapPlugin } from "@flowgram.ai/free-snap-plugin";
import { FlowMinimapService, MinimapRender, createMinimapPlugin } from "@flowgram.ai/minimap-plugin";
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
  Map as MapIcon,
  Maximize,
  Minus,
  PanelLeft,
  PanelRight,
  Plus,
  Redo2,
  Save,
  Search,
  Settings2,
  Spline,
  Undo2,
  Waypoints,
  X
} from "lucide-react";
import { toast } from "sonner";
import { CanvasOverlayContext, GrantNodeCard, type CanvasOverlay } from "@/components/apps/item-grant/item-grant-canvas-nodes";
import { Field, FieldGroup } from "@/components/apps/item-grant/item-grant-fields";
import {
  NODE_WIDTH,
  defaultNodeData,
  edgeId,
  fromCanvas,
  graphSignature,
  newId,
  toCanvas,
  wouldCycle,
  type CanvasJSON
} from "@/components/apps/item-grant/item-grant-graph";
import { NodeInspector } from "@/components/apps/item-grant/item-grant-inspector";
import { HANDLE_TONE, NODE_VISUAL, STATUS_META, TRIGGER_LABEL, formatNumber, nodeOutputs } from "@/components/apps/item-grant/item-grant-meta";
import { PreviewPanel } from "@/components/apps/item-grant/item-grant-preview-panel";
import { ToneIcon } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type {
  ItemGrantCatalog,
  ItemGrantEstimate,
  ItemGrantGraph,
  ItemGrantIssue,
  ItemGrantNodeSpec,
  ItemGrantNodeType,
  ItemGrantPreview,
  ItemGrantRule,
  ItemGrantTriggerData
} from "@/lib/api/item-grant";
import { ApiError } from "@/lib/api/client";
import { formatDateTime, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { useSaveItemGrantRuleMutation, useValidateItemGrantMutation } from "@/lib/item-grant-hooks";
import { cn } from "@/lib/utils";

/**
 * 规则画布工作区（Flowgram 自由布局编辑器，只在浏览器里加载）。
 *
 * | 区域 | 作用 |
 * |---|---|
 * | 顶栏 | 规则名、状态、版本、未保存提示、问题数、保存（⌘S） |
 * | 节点库（左侧浮层） | 按分类列出节点，可搜索；拖到画布上，或点一下放到画布中央。触发器只能有一个 |
 * | 画布 | 贝塞尔连线按端口语义着色、追踪路径流动；对齐吸附线、框选、⌘A 全选、Delete 删除、⌘D 复制、⌘Z 撤销；
 *          从输出端口拖线到空白处或点一下端口，就地选一个节点接上 |
 * | 底部工具条 | 撤销 / 重做、缩放、适应画布、自动排版、曲线 / 折线、小地图 |
 * | 右侧 | 配置（选中节点的表单 / 未选中时是规则设置）、问题（实时校验，点击定位）、预演（结果叠加在画布上） |
 *
 * 画布每次变化 600ms 后自动调后端校验（与上线时同一套判据），问题以角标标在节点上、出错的连线标红。
 */

const ACCENT = "#6366f1";

type RuleMeta = { name: string; description: string; startsAt: string; endsAt: string };

function metaOf(rule: ItemGrantRule): RuleMeta {
  return {
    name: rule.name,
    description: rule.description ?? "",
    startsAt: rule.startsAt ? toLocalInputValue(rule.startsAt) : "",
    endsAt: rule.endsAt ? toLocalInputValue(rule.endsAt) : ""
  };
}

/** 画布外部状态：给 Flowgram 的回调（连线着色、流动、合法性、拖线落空）读，这些回调在 React 之外调用。 */
type CanvasBridge = {
  catalog: ItemGrantCatalog;
  edgeIssues: Set<string>;
  trace: CanvasOverlay["trace"];
  onContentChange: () => void;
  /** 画布载入完成，参数是载入时的签名（「未保存」的基准） */
  onLoaded: (signature: string) => void;
  onDropLine: (port: WorkflowPortEntity, canvas: { x: number; y: number }, client: { x: number; y: number }) => void;
};

function nodeData(node: WorkflowNodeEntity | undefined) {
  return (node ? (getNodeForm(node)?.values ?? {}) : {}) as Record<string, unknown>;
}

function lineEdgeId(line: WorkflowLineEntity) {
  const port = line.fromPort?.portID;
  return edgeId(line.from?.id ?? "", port === undefined ? "" : String(port), line.to?.id ?? "");
}

function lineTraced(line: WorkflowLineEntity, trace: CanvasOverlay["trace"]) {
  if (!trace || !line.from) return false;
  const port = String(line.fromPort?.portID ?? "");
  return trace.steps.some((step) => step.nodeId === line.from?.id && step.handle === port);
}

function buildEditorProps(rule: ItemGrantRule, catalog: ItemGrantCatalog, bridge: { current: CanvasBridge }): FreeLayoutProps {
  const selectAll: (ctx: FreeLayoutPluginContext) => ShortcutsHandler = (ctx) => ({
    commandId: "item-grant.select-all",
    shortcuts: ["meta a", "ctrl a"],
    execute: () => {
      ctx.selection.selection = ctx.document.getAllNodes();
    }
  });
  const remove: (ctx: FreeLayoutPluginContext) => ShortcutsHandler = (ctx) => ({
    commandId: "item-grant.delete",
    shortcuts: ["backspace", "delete"],
    execute: () => {
      if (ctx.playground.config.readonly) return;
      ctx.history.startTransaction();
      for (const entity of ctx.selection.selection) {
        if (entity instanceof WorkflowNodeEntity) {
          if (ctx.document.canRemove(entity)) entity.dispose();
        } else if (entity instanceof WorkflowLineEntity) {
          if (ctx.document.linesManager.canRemove(entity)) entity.dispose();
        }
      }
      ctx.selection.selection = ctx.selection.selection.filter((entity) => !entity.disposed);
      ctx.history.endTransaction();
    }
  });
  const duplicate: (ctx: FreeLayoutPluginContext) => ShortcutsHandler = (ctx) => ({
    commandId: "item-grant.duplicate",
    shortcuts: ["meta d", "ctrl d"],
    execute: () => {
      const nodes = ctx.selection.selection.filter((entity): entity is WorkflowNodeEntity => entity instanceof WorkflowNodeEntity);
      duplicateNodes(ctx, nodes);
    }
  });

  return {
    background: { gridSize: 20, dotSize: 1, dotColor: "var(--ig-dot)", dotOpacity: 1 },
    readonly: false,
    twoWayConnection: false,
    initialData: toCanvas(rule.graph, catalog) as unknown as WorkflowJSON,
    nodeRegistries: catalog.nodes.map((spec) => ({
      type: spec.type,
      meta: {
        useDynamicPort: true,
        defaultPorts: spec.noInput ? [] : [{ type: "input" as const }],
        defaultExpanded: true
      },
      // 节点配置存在表单里（toJSON 的 data 就是它），卡片自己渲染，表单不出界面
      formMeta: { render: () => <></> }
    })),
    nodeEngine: { enable: true },
    history: { enable: true, enableChangeNode: true },
    materials: { renderDefaultNode: GrantNodeCard },
    lineColor: {
      hidden: "transparent",
      default: "var(--ig-line, #94a3b8)",
      drawing: ACCENT,
      hovered: ACCENT,
      selected: ACCENT,
      error: "#ef4444",
      flowing: "#10b981"
    },
    setLineClassName: (_ctx, line) => {
      const state = bridge.current;
      const from = line.from;
      const outputs = from ? nodeOutputs(from.flowNodeType as ItemGrantNodeType, nodeData(from), state.catalog) : [];
      const output = outputs.find((candidate) => candidate.id === String(line.fromPort?.portID ?? "")) ?? outputs[0];
      return cn(
        `ig-line-${output?.tone ?? "neutral"}`,
        state.edgeIssues.has(lineEdgeId(line)) && "ig-line-error",
        state.trace && !lineTraced(line, state.trace) && "ig-line-dim"
      );
    },
    isFlowingLine: (_ctx, line) => lineTraced(line, bridge.current.trace),
    canAddLine: (ctx, fromPort, toPort) => {
      if (fromPort.portType !== "output" || toPort.portType !== "input") return false;
      const source = fromPort.node.id;
      const target = toPort.node.id;
      if (source === target || toPort.node.flowNodeType === "trigger") return false;
      const lines = ctx.document.linesManager.getAllLines();
      if (lines.some((line) => line.from?.id === source && line.fromPort === fromPort && line.to?.id === target)) return false;
      const edges = lines.filter((line) => line.from && line.to).map((line) => ({ source: line.from!.id, target: line.to!.id }));
      return !wouldCycle(edges, source, target);
    },
    onDragLineEnd: async (ctx, params) => {
      const { fromPort, toPort, mousePos, line, originLine, event } = params;
      if (originLine || !line || toPort || !fromPort) return;
      const native = event as unknown as { clientX?: number; clientY?: number; event?: { clientX: number; clientY: number } };
      const client = native.event ?? { clientX: native.clientX ?? 0, clientY: native.clientY ?? 0 };
      bridge.current.onDropLine(fromPort, mousePos, { x: client.clientX, y: client.clientY });
    },
    onContentChange: () => bridge.current.onContentChange(),
    onAllLayersRendered: (ctx) => {
      void ctx.tools.fitView(false);
      bridge.current.onLoaded(graphSignature(fromCanvas(ctx.document.toJSON() as unknown as CanvasJSON)));
    },
    shortcuts: (registry, ctx) => registry.addHandlers(selectAll(ctx), remove(ctx), duplicate(ctx)),
    plugins: () => [
      createMinimapPlugin({ disableLayer: true, canvasStyle: { canvasWidth: 200, canvasHeight: 124, canvasPadding: 40 } }),
      createFreeSnapPlugin({ edgeColor: ACCENT, alignColor: ACCENT, edgeLineWidth: 1, alignLineWidth: 1, alignCrossWidth: 8 })
    ]
  };
}

type BridgePatch = Partial<Pick<CanvasBridge, "edgeIssues" | "trace">>;

/** 挂载时建一次：编辑器配置与它读的外部状态（闭包里），update 在校验、预演变化后改它。 */
function createEditor(
  rule: ItemGrantRule,
  catalog: ItemGrantCatalog,
  handlers: Pick<CanvasBridge, "onContentChange" | "onLoaded" | "onDropLine">
) {
  const bridge = { current: { catalog, edgeIssues: new Set<string>(), trace: null, ...handlers } as CanvasBridge };
  return {
    props: buildEditorProps(rule, catalog, bridge),
    update: (patch: BridgePatch) => {
      Object.assign(bridge.current, patch);
    }
  };
}

/** 复制节点（触发器除外），连同它们之间的连线；整体算一步撤销。 */
async function duplicateNodes(ctx: FreeLayoutPluginContext, nodes: WorkflowNodeEntity[]) {
  const source = nodes.filter((node) => node.flowNodeType !== "trigger");
  if (!source.length) return [];
  const mapping = new Map<string, WorkflowNodeEntity>();
  ctx.history.startTransaction();
  for (const node of source) {
    const position = node.transform.position;
    const copy = ctx.document.createWorkflowNodeByType(
      node.flowNodeType,
      { x: position.x + 40, y: position.y + 40 },
      { id: newId(String(node.flowNodeType)), data: JSON.parse(JSON.stringify(nodeData(node))) }
    );
    mapping.set(node.id, copy);
  }
  await delay(30);
  for (const line of ctx.document.linesManager.getAllLines()) {
    const from = line.from && mapping.get(line.from.id);
    const to = line.to && mapping.get(line.to.id);
    if (from && to) ctx.document.linesManager.createLine({ from: from.id, fromPort: line.fromPort?.portID, to: to.id });
  }
  ctx.history.endTransaction();
  ctx.selection.selection = [...mapping.values()];
  return [...mapping.values()];
}

function cssVar(element: Element, name: string, fallback: string) {
  return getComputedStyle(element).getPropertyValue(name).trim() || fallback;
}

function clearSelection(ctx: FreeLayoutPluginContext) {
  ctx.selection.selection = [];
}

/** 小地图画在 canvas 上，颜色要是具体值：从当前主题的变量里取。 */
function applyMinimapTheme(minimap: FlowMinimapService, dark: boolean) {
  const root = document.querySelector(".ig-canvas") ?? document.documentElement;
  const service = minimap as unknown as { options: { canvasStyle: Record<string, unknown> }; initStyle?: () => void; render?: () => void };
  service.options.canvasStyle = {
    ...service.options.canvasStyle,
    canvasBackground: cssVar(root, "--card", "#ffffff"),
    canvasBorderRadius: 12,
    viewportBackground: dark ? "rgba(255,255,255,0.06)" : "rgba(15,23,42,0.05)",
    viewportBorderRadius: 6,
    viewportBorderColor: ACCENT,
    viewportBorderWidth: 1,
    viewportBorderDashLength: undefined,
    nodeColor: dark ? "rgba(255,255,255,0.22)" : "rgba(15,23,42,0.16)",
    nodeBorderRadius: 3,
    nodeBorderWidth: 0,
    overlayColor: "rgba(0,0,0,0)"
  };
  service.initStyle?.();
  service.render?.();
}

export default function ItemGrantWorkspace({
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
  const [revision, setRevision] = useState(0);
  const [baseline, setBaseline] = useState<string | null>(null);
  const [dropMenu, setDropMenu] = useState<DropMenuState | null>(null);
  // 编辑器只在挂载时读一次配置；运行期变化都经 update
  const [editor] = useState(() =>
    createEditor(rule, catalog, {
      onContentChange: () => setRevision((value) => value + 1),
      onLoaded: (signature) => setBaseline(signature),
      onDropLine: (port, canvas, client) => setDropMenu({ port, canvas, client })
    })
  );

  return (
    <FreeLayoutEditorProvider {...editor.props}>
      <Workspace
        appKey={appKey}
        rule={rule}
        catalog={catalog}
        onClose={onClose}
        onDirtyChange={onDirtyChange}
        updateBridge={editor.update}
        revision={revision}
        baseline={baseline}
        dropMenu={dropMenu}
        setDropMenu={setDropMenu}
      />
    </FreeLayoutEditorProvider>
  );
}

type DropMenuState = { port: WorkflowPortEntity; canvas: { x: number; y: number }; client: { x: number; y: number } };

function Workspace({
  appKey,
  rule,
  catalog,
  onClose,
  onDirtyChange,
  updateBridge,
  revision,
  baseline,
  dropMenu,
  setDropMenu
}: {
  appKey: string;
  rule: ItemGrantRule;
  catalog: ItemGrantCatalog;
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
  updateBridge: (patch: BridgePatch) => void;
  revision: number;
  /** 载入时的签名；null 表示画布还没载入完 */
  baseline: string | null;
  dropMenu: DropMenuState | null;
  setDropMenu: (menu: DropMenuState | null) => void;
}) {
  const ctx = useClientContext();
  const selectService = useService(WorkflowSelectService);
  const canvasRef = useRef<HTMLDivElement>(null);

  const [meta, setMeta] = useState<RuleMeta>(() => metaOf(rule));
  const [savedMeta, setSavedMeta] = useState<RuleMeta>(() => metaOf(rule));
  const [savedSignature, setSavedSignature] = useState<string | null>(null);
  const loaded = baseline !== null;
  const [tab, setTab] = useState("config");
  const [panelOpen, setPanelOpen] = useState(true);
  const [paletteOpen, setPaletteOpen] = useState(true);
  const [issues, setIssues] = useState<ItemGrantIssue[]>([]);
  const [estimate, setEstimate] = useState<ItemGrantEstimate | null>(null);
  const [trace, setTrace] = useState<ItemGrantPreview["trace"] | null>(null);
  const [version, setVersion] = useState(rule.version);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const save = useSaveItemGrantRuleMutation(appKey);
  const validate = useValidateItemGrantMutation(appKey);
  const runValidate = validate.mutate;

  const toGraph = useCallback((): ItemGrantGraph => fromCanvas(ctx.document.toJSON() as unknown as CanvasJSON), [ctx]);

  // 每次内容变化（revision）重新取一次画布：签名、触发器、节点数都从它来
  const graph = useMemo(() => (loaded ? toGraph() : null), [loaded, revision, toGraph]); // eslint-disable-line react-hooks/exhaustive-deps
  const signature = graph ? graphSignature(graph) : null;
  const reference = savedSignature ?? baseline;
  const dirty = (signature !== null && reference !== null && signature !== reference) || JSON.stringify(meta) !== JSON.stringify(savedMeta);
  useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange]);

  const triggerNode = graph?.nodes.find((node) => node.type === "trigger");
  const triggerData = triggerNode?.data as ItemGrantTriggerData | undefined;
  const triggerEvent = triggerData?.kind === "event" ? triggerData.event : undefined;

  useEffect(() => {
    const update = () => {
      const nodes = selectService.selectedNodes.filter((node) => !node.disposed);
      setSelectedId(nodes.length === 1 ? nodes[0].id : null);
    };
    update();
    const disposable = selectService.onSelectionChanged(update);
    return () => disposable.dispose();
  }, [selectService]);
  const selectedNode = selectedId ? ctx.document.getNode(selectedId) : undefined;

  // 画布一变，旧的预演结果就不再对应当前配置
  const firstSignature = useRef<string | null>(null);
  useEffect(() => {
    if (signature === null) return;
    if (firstSignature.current === null) {
      firstSignature.current = signature;
      return;
    }
    if (signature === firstSignature.current) return;
    firstSignature.current = signature;
    setEstimate(null);
    setTrace(null);
  }, [signature]);

  // 实时校验：停手 600ms 后调后端（与上线时同一套判据）
  useEffect(() => {
    if (!loaded) return;
    const timer = setTimeout(() => runValidate(toGraph(), { onSuccess: (data) => setIssues(data.issues) }), 600);
    return () => clearTimeout(timer);
  }, [signature, loaded, runValidate, toGraph]);

  const issueMap = useMemo(() => {
    const map = new Map<string, ItemGrantIssue[]>();
    for (const issue of issues) {
      if (issue.nodeId) map.set(issue.nodeId, [...(map.get(issue.nodeId) ?? []), issue]);
    }
    return map;
  }, [issues]);
  const errorCount = issues.filter((issue) => issue.level === "error").length;
  const warningCount = issues.length - errorCount;

  // 连线的着色、标红、流动在 Flowgram 的回调里读 bridge，改完要让连线层重画
  useEffect(() => {
    updateBridge({
      edgeIssues: new Set(issues.filter((issue) => issue.edgeId).map((issue) => issue.edgeId as string)),
      trace: trace?.outcome ?? null
    });
    ctx.document.linesManager.forceUpdate();
  }, [issues, trace, ctx, updateBridge]);

  const removeNode = useCallback(
    (node: WorkflowNodeEntity) => {
      if (!ctx.document.canRemove(node)) return;
      node.dispose();
    },
    [ctx]
  );

  const focusNode = useCallback(
    (id: string) => {
      const node = ctx.document.getNode(id);
      if (!node) return;
      selectService.selectNode(node);
      setTab("config");
      setPanelOpen(true);
      void ctx.playground.scrollToView({ bounds: node.transform.bounds, scrollToCenter: true, zoom: Math.max(ctx.playground.config.zoom, 1) });
    },
    [ctx, selectService]
  );

  const addNodeAt = useCallback(
    (type: ItemGrantNodeType, position: { x: number; y: number }) => {
      if (type === "trigger" && ctx.document.getAllNodes().some((node) => node.flowNodeType === "trigger")) {
        toast.error("一条规则只能有一个触发器");
        return undefined;
      }
      const node = ctx.document.createWorkflowNodeByType(type, position, {
        id: type === "trigger" ? "trigger" : newId(type),
        data: defaultNodeData(type, catalog)
      });
      selectService.selectNode(node);
      setTab("config");
      return node;
    },
    [ctx, catalog, selectService]
  );

  const canvasCenter = () => {
    const rect = canvasRef.current?.getBoundingClientRect();
    const point = ctx.playground.config.getPosFromMouseEvent({
      clientX: (rect ? rect.left + rect.width / 2 : window.innerWidth / 2) + (Math.random() - 0.5) * 40,
      clientY: (rect ? rect.top + rect.height / 2 : window.innerHeight / 2) + (Math.random() - 0.5) * 40
    });
    return { x: point.x, y: point.y - 60 };
  };

  const openPortMenu = useCallback(
    (port: WorkflowPortEntity, event: React.MouseEvent) => {
      const canvas = ctx.playground.config.getPosFromMouseEvent(event.nativeEvent);
      setDropMenu({ port, canvas: { x: canvas.x + 120, y: canvas.y }, client: { x: event.clientX + 16, y: event.clientY } });
    },
    [ctx, setDropMenu]
  );

  const connectNew = async (menu: DropMenuState, type: ItemGrantNodeType) => {
    setDropMenu(null);
    ctx.history.startTransaction();
    const node = addNodeAt(type, { x: menu.canvas.x + NODE_WIDTH / 2, y: menu.canvas.y - 36 });
    if (node) {
      await delay(30);
      ctx.document.linesManager.createLine({ from: menu.port.node.id, fromPort: menu.port.portID, to: node.id });
    }
    ctx.history.endTransaction();
  };

  const overlay = useMemo<CanvasOverlay>(
    () => ({
      catalog,
      issues: issueMap,
      estimate,
      trace: trace?.outcome ?? null,
      onPortClick: openPortMenu,
      onDuplicate: (node) => void duplicateNodes(ctx, [node]),
      onDelete: removeNode
    }),
    [catalog, issueMap, estimate, trace, openPortMenu, ctx, removeNode]
  );

  const submit = () => {
    if (!meta.name.trim()) {
      toast.error("规则名称不能为空");
      setTab("config");
      clearSelection(ctx);
      return;
    }
    const current = toGraph();
    const sentSignature = graphSignature(current);
    save.mutate(
      {
        ruleId: rule.id,
        payload: {
          name: meta.name.trim(),
          description: meta.description.trim(),
          startsAt: fromLocalInputValue(meta.startsAt) ?? null,
          endsAt: fromLocalInputValue(meta.endsAt) ?? null,
          graph: current
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

  const status = STATUS_META[rule.status];
  const empty = loaded && (graph?.nodes.length ?? 0) === 0;

  return (
    <CanvasOverlayContext.Provider value={overlay}>
      <style>{CANVAS_STYLE}</style>
      <div className="ig-canvas flex h-full flex-col">
        {/* 顶栏 */}
        <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-background px-3">
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
            <Button
              variant="ghost"
              size="sm"
              className={cn(
                "gap-1.5",
                errorCount ? "text-red-600 dark:text-red-400" : warningCount ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400"
              )}
              onClick={() => {
                setTab("issues");
                setPanelOpen(true);
              }}
            >
              {validate.isPending ? <Loader2 className="animate-spin" /> : errorCount ? <CircleAlert /> : warningCount ? <AlertTriangle /> : <CircleCheck />}
              <span className="hidden sm:inline">{errorCount ? `${errorCount} 个错误` : warningCount ? `${warningCount} 个提醒` : "校验通过"}</span>
            </Button>
            <ToolbarButton label={panelOpen ? "收起右侧面板" : "展开右侧面板"} onClick={() => setPanelOpen(!panelOpen)}>
              <PanelRight />
            </ToolbarButton>
            <span className="mx-1 h-5 w-px bg-border" />
            <Button size="sm" onClick={submit} disabled={save.isPending || !dirty}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存
            </Button>
          </div>
        </header>

        <div className="relative flex min-h-0 flex-1">
          {/* 画布 */}
          <div ref={canvasRef} className="relative min-h-0 min-w-0 flex-1 overflow-hidden" onMouseDown={() => dropMenu && setDropMenu(null)}>
            <EditorRenderer className="absolute inset-0" />

            {/* 节点库 */}
            <div className="pointer-events-none absolute top-3 left-3 z-20 flex max-h-[calc(100%-5.5rem)] flex-col">
              {paletteOpen ? (
                <NodePalette
                  catalog={catalog}
                  hasTrigger={Boolean(triggerNode)}
                  onCollapse={() => setPaletteOpen(false)}
                  onAdd={(type) => addNodeAt(type, canvasCenter())}
                />
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  className="pointer-events-auto gap-1.5 rounded-xl bg-background/95 shadow-sm"
                  onClick={() => setPaletteOpen(true)}
                >
                  <PanelLeft /> 节点库
                </Button>
              )}
            </div>

            {/* 预演叠加提示 */}
            {trace || estimate ? (
              <div className="pointer-events-none absolute top-3 left-1/2 z-20 -translate-x-1/2">
                <div className="pointer-events-auto flex items-center gap-2 rounded-xl border bg-background/95 px-3 py-1.5 text-xs shadow-sm backdrop-blur">
                  <Footprints className="size-3.5 text-emerald-500" />
                  {trace
                    ? `正在显示 ${trace.user.nickname || trace.user.account} 的判定路径`
                    : `抽样 ${estimate?.sampled} 人的到达分布，目标人群 ${formatNumber(estimate?.candidates)} 人`}
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground"
                    onClick={() => {
                      setTrace(null);
                      setEstimate(null);
                    }}
                  >
                    清除
                  </button>
                </div>
              </div>
            ) : null}

            {empty ? (
              <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
                <div className="rounded-2xl border border-dashed bg-background/90 px-6 py-5 text-center">
                  <div className="text-sm font-medium">从节点库拖一个「触发器」到画布上开始</div>
                  <p className="mt-1 text-xs text-muted-foreground">之后从输出端口拖线到空白处，就能就地接下一个节点</p>
                </div>
              </div>
            ) : null}

            <CanvasToolbar />

            {dropMenu ? (
              <QuickAddMenu
                catalog={catalog}
                menu={dropMenu}
                onPick={(type) => void connectNew(dropMenu, type)}
                onClose={() => setDropMenu(null)}
              />
            ) : null}
          </div>

          {/* 右侧 */}
          <aside
            className={cn(
              "min-h-0 w-[min(380px,100%)] flex-col border-l bg-background",
              "absolute inset-y-0 right-0 z-30 shadow-xl xl:static xl:z-auto xl:shadow-none",
              panelOpen ? "flex" : "hidden"
            )}
          >
            <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-0">
              <div className="flex items-center gap-2 border-b px-3 py-2">
                <TabsList className="flex-1">
                  <TabsTrigger value="config" className="flex-1 text-xs">
                    <Settings2 /> 配置
                  </TabsTrigger>
                  <TabsTrigger value="issues" className="flex-1 text-xs">
                    <ListChecks /> 问题
                    {issues.length ? (
                      <span className={cn("ml-0.5 rounded-full px-1.5 text-[10px] tabular-nums text-white", errorCount ? "bg-red-500" : "bg-amber-500")}>
                        {issues.length}
                      </span>
                    ) : null}
                  </TabsTrigger>
                  <TabsTrigger value="preview" className="flex-1 text-xs">
                    <Footprints /> 预演
                  </TabsTrigger>
                </TabsList>
                <Button size="icon" variant="ghost" className="size-7 xl:hidden" onClick={() => setPanelOpen(false)} aria-label="收起">
                  <X />
                </Button>
              </div>
              <TabsContent value="config" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                {selectedNode ? (
                  <NodeInspector
                    key={selectedNode.id}
                    node={selectedNode}
                    catalog={catalog}
                    issues={issueMap.get(selectedNode.id) ?? []}
                    triggerEvent={triggerEvent}
                    onDuplicate={() => void duplicateNodes(ctx, [selectedNode])}
                    onDelete={() => removeNode(selectedNode)}
                    onRemoveBranch={(branchId) => {
                      for (const line of ctx.document.linesManager.getAllLines()) {
                        if (line.from?.id === selectedNode.id && String(line.fromPort?.portID ?? "") === branchId) line.dispose();
                      }
                    }}
                  />
                ) : (
                  <RuleSettings rule={rule} meta={meta} onChange={setMeta} nodeCount={graph?.nodes.length ?? 0} edgeCount={graph?.edges.length ?? 0} />
                )}
              </TabsContent>
              <TabsContent value="issues" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                <IssueList issues={issues} catalog={catalog} graph={graph} onFocus={focusNode} />
              </TabsContent>
              <TabsContent value="preview" className="min-h-0 flex-1 overflow-y-auto px-4 py-4" forceMount hidden={tab !== "preview"}>
                <PreviewPanel
                  appKey={appKey}
                  ruleId={rule.id}
                  catalog={catalog}
                  graph={toGraph}
                  eventKey={triggerEvent}
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
    </CanvasOverlayContext.Provider>
  );
}

/** 画布配色：Flowgram 的变量（背景、框选、端口底色）与连线按端口语义着色的类名。 */
const CANVAS_STYLE = `
.ig-canvas {
  --g-editor-background: color-mix(in oklab, var(--muted) 60%, var(--background));
  --g-selection-background: ${ACCENT};
  --g-playground-select: ${ACCENT};
  --g-playground-hover: ${ACCENT};
  --g-playground-line: ${ACCENT};
  --g-playground-selectBox-outline: ${ACCENT};
  --g-playground-selectBox-background: rgba(99, 102, 241, 0.08);
  --g-playground-select-hover-background: rgba(99, 102, 241, 0.08);
  --g-workflow-port-color-background: var(--card);
  --ig-dot: color-mix(in oklab, var(--foreground) 18%, transparent);
}
.ig-canvas .ig-line-positive { --ig-line: ${HANDLE_TONE.positive.stroke}; }
.ig-canvas .ig-line-negative { --ig-line: ${HANDLE_TONE.negative.stroke}; }
.ig-canvas .ig-line-branch { --ig-line: ${HANDLE_TONE.branch.stroke}; }
.ig-canvas .ig-line-neutral { --ig-line: color-mix(in oklab, var(--muted-foreground) 70%, transparent); }
.ig-canvas .ig-line-error { --ig-line: #ef4444; }
.ig-canvas .ig-line-dim { opacity: 0.25; }
.ig-canvas .gedit-flow-activity-edge { transition: opacity 0.2s; }
`;

function ToolbarButton({
  label,
  disabled,
  active,
  onClick,
  children
}: {
  label: string;
  disabled?: boolean;
  active?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          size="icon"
          variant="ghost"
          className={cn("size-8 rounded-lg", active && "bg-accent text-foreground")}
          disabled={disabled}
          onClick={onClick}
          aria-label={label}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** 底部浮动工具条与小地图。 */
function CanvasToolbar() {
  const ctx = useClientContext();
  const tools = usePlaygroundTools();
  const minimap = useService(FlowMinimapService);
  const { resolvedTheme } = useTheme();
  const [canUndo, setCanUndo] = useState(false);
  const [canRedo, setCanRedo] = useState(false);
  const [showMinimap, setShowMinimap] = useState(true);

  useEffect(() => {
    const disposable = ctx.history.undoRedoService.onChange(() => {
      setCanUndo(ctx.history.canUndo());
      setCanRedo(ctx.history.canRedo());
    });
    return () => disposable.dispose();
  }, [ctx]);

  useEffect(() => applyMinimapTheme(minimap, resolvedTheme === "dark"), [minimap, resolvedTheme]);

  const curved = tools.lineType === LineType.BEZIER;

  return (
    <>
      <div className="absolute bottom-4 left-1/2 z-20 flex -translate-x-1/2 items-center gap-0.5 rounded-2xl border bg-background/95 p-1 shadow-lg backdrop-blur">
        <ToolbarButton label="撤销（⌘Z）" disabled={!canUndo} onClick={() => ctx.history.undo()}>
          <Undo2 />
        </ToolbarButton>
        <ToolbarButton label="重做（⌘⇧Z）" disabled={!canRedo} onClick={() => ctx.history.redo()}>
          <Redo2 />
        </ToolbarButton>
        <span className="mx-1 h-5 w-px bg-border" />
        <ToolbarButton label="缩小" onClick={() => tools.zoomout()}>
          <Minus />
        </ToolbarButton>
        <button
          type="button"
          className="h-8 w-12 rounded-lg text-xs tabular-nums text-muted-foreground hover:bg-accent hover:text-foreground"
          onClick={() => tools.fitView()}
          title="适应画布"
        >
          {Math.round(tools.zoom * 100)}%
        </button>
        <ToolbarButton label="放大" onClick={() => tools.zoomin()}>
          <Plus />
        </ToolbarButton>
        <ToolbarButton label="适应画布" onClick={() => tools.fitView()}>
          <Maximize />
        </ToolbarButton>
        <span className="mx-1 h-5 w-px bg-border" />
        <ToolbarButton
          label="自动排版"
          onClick={() => {
            void tools.autoLayout().then(() => tools.fitView());
          }}
        >
          <LayoutGrid />
        </ToolbarButton>
        <ToolbarButton label={curved ? "改为折线" : "改为曲线"} onClick={() => tools.switchLineType(curved ? LineType.LINE_CHART : LineType.BEZIER)}>
          {curved ? <Spline /> : <Waypoints />}
        </ToolbarButton>
        <ToolbarButton label={showMinimap ? "隐藏小地图" : "显示小地图"} active={showMinimap} onClick={() => setShowMinimap(!showMinimap)}>
          <MapIcon />
        </ToolbarButton>
      </div>
      <div className={cn("absolute right-4 bottom-4 z-20 overflow-hidden rounded-2xl border bg-card shadow-lg", !showMinimap && "hidden")}>
        <MinimapRender
          containerStyles={{ pointerEvents: "auto", position: "relative", top: "unset", right: "unset", bottom: "unset", left: "unset" }}
          inactiveStyle={{ opacity: 1, scale: 1, translateX: 0, translateY: 0 }}
        />
      </div>
    </>
  );
}

/** 节点库：拖到画布上放在落点，点一下放到画布中央。 */
function NodePalette({
  catalog,
  hasTrigger,
  onAdd,
  onCollapse
}: {
  catalog: ItemGrantCatalog;
  hasTrigger: boolean;
  onAdd: (type: ItemGrantNodeType) => void;
  onCollapse: () => void;
}) {
  const dragService = useService(WorkflowDragService);
  const [keyword, setKeyword] = useState("");
  const pressed = useRef<{ type: ItemGrantNodeType; x: number; y: number; dragging: boolean } | null>(null);
  const query = keyword.trim().toLowerCase();
  const match = (spec: ItemGrantNodeSpec) =>
    !query || spec.label.toLowerCase().includes(query) || spec.hint.toLowerCase().includes(query) || spec.type.includes(query);

  return (
    <div className="pointer-events-auto flex min-h-0 w-60 flex-col overflow-hidden rounded-2xl border bg-background/95 shadow-lg backdrop-blur">
      <div className="flex items-center gap-2 px-3 pt-3 pb-2">
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold">节点库</div>
          <p className="text-[11px] leading-4 text-muted-foreground">拖到画布上，或点击添加</p>
        </div>
        <Button size="icon" variant="ghost" className="size-7" onClick={onCollapse} aria-label="收起节点库">
          <PanelLeft />
        </Button>
      </div>
      <div className="px-3 pb-2">
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-8 pl-8 text-xs" placeholder="搜索节点" value={keyword} onChange={(event) => setKeyword(event.target.value)} />
        </div>
      </div>
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-3 pb-3">
        {catalog.categories.map((category) => {
          const specs = catalog.nodes.filter((spec) => spec.category === category.key && match(spec));
          if (specs.length === 0) return null;
          return (
            <div key={category.key} className="space-y-1">
              <div className="px-1 text-[11px] font-medium text-muted-foreground">{category.label}</div>
              {specs.map((spec) => {
                const visual = NODE_VISUAL[spec.type];
                const disabled = spec.singleton && hasTrigger;
                const outputs = spec.outputs ?? [];
                return (
                  <Tooltip key={spec.type}>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        disabled={disabled}
                        onMouseDown={(event) => {
                          if (disabled || event.button !== 0) return;
                          pressed.current = { type: spec.type, x: event.clientX, y: event.clientY, dragging: false };
                        }}
                        onMouseMove={(event) => {
                          const state = pressed.current;
                          if (!state || state.dragging || state.type !== spec.type) return;
                          if (Math.abs(event.clientX - state.x) + Math.abs(event.clientY - state.y) < 5) return;
                          state.dragging = true;
                          void dragService.startDragCard(spec.type, event, {
                            id: spec.type === "trigger" ? "trigger" : newId(spec.type),
                            data: defaultNodeData(spec.type, catalog)
                          });
                        }}
                        onMouseLeave={() => {
                          if (pressed.current && !pressed.current.dragging) pressed.current = null;
                        }}
                        onClick={() => {
                          const state = pressed.current;
                          pressed.current = null;
                          if (state?.dragging) return;
                          onAdd(spec.type);
                        }}
                        className={cn(
                          "group flex w-full cursor-grab items-center gap-2.5 rounded-xl border border-transparent px-2 py-1.5 text-left transition-colors hover:border-border hover:bg-accent/50 active:cursor-grabbing",
                          disabled && "cursor-not-allowed opacity-45 hover:border-transparent hover:bg-transparent"
                        )}
                      >
                        <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
                        <div className="min-w-0">
                          <div className="truncate text-xs font-medium">{spec.label}</div>
                          <div className="truncate text-[11px] text-muted-foreground">
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
              ["⌘A", "全选"],
              ["Delete", "删除"],
              ["Shift 拖动", "框选"]
            ].map(([key, label]) => (
              <div key={key} className="contents">
                <dt className="font-mono">{key}</dt>
                <dd>{label}</dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </div>
  );
}

/** 从输出端口拖线到空白处（或点端口）后弹出的节点选单：选一个就建在落点并接上线。 */
function QuickAddMenu({
  catalog,
  menu,
  onPick,
  onClose
}: {
  catalog: ItemGrantCatalog;
  menu: DropMenuState;
  onPick: (type: ItemGrantNodeType) => void;
  onClose: () => void;
}) {
  useHotkeys("escape", onClose, { enableOnFormTags: true });
  // 全屏弹层铺满视口，fixed 定位直接用鼠标的视口坐标
  const width = 248;
  const left = Math.max(8, Math.min(menu.client.x + 8, window.innerWidth - width - 8));
  const top = Math.max(64, Math.min(menu.client.y - 24, window.innerHeight - 368));
  const specs = catalog.nodes.filter((spec) => !spec.noInput);
  const fromOutputs = nodeOutputs(menu.port.node.flowNodeType as ItemGrantNodeType, nodeData(menu.port.node), catalog);
  const fromLabel = fromOutputs.find((output) => output.id === String(menu.port.portID ?? ""))?.label;

  return (
    <div
      className="fixed z-40 max-h-[360px] overflow-y-auto rounded-2xl border bg-popover p-1.5 text-popover-foreground shadow-xl"
      style={{ left, top, width }}
      onMouseDown={(event) => event.stopPropagation()}
    >
      <div className="px-2 pt-1 pb-1.5 text-[11px] text-muted-foreground">{fromLabel ? `从「${fromLabel}」接一个节点` : "接一个节点"}</div>
      {catalog.categories.map((category) => {
        const items = specs.filter((spec) => spec.category === category.key);
        if (!items.length) return null;
        return (
          <div key={category.key} className="py-0.5">
            {items.map((spec) => {
              const visual = NODE_VISUAL[spec.type];
              return (
                <button
                  key={spec.type}
                  type="button"
                  onClick={() => onPick(spec.type)}
                  className="flex w-full items-center gap-2.5 rounded-xl px-2 py-1.5 text-left transition-colors hover:bg-accent"
                >
                  <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
                  <div className="min-w-0">
                    <div className="truncate text-xs font-medium">{spec.label}</div>
                    <div className="truncate text-[11px] text-muted-foreground">{category.label}</div>
                  </div>
                </button>
              );
            })}
          </div>
        );
      })}
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
          <Input type="datetime-local" className="h-8" value={meta.startsAt} onChange={(event) => onChange({ ...meta, startsAt: event.target.value })} />
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
          <dd>{rule.nextRunAt ? formatDateTime(rule.nextRunAt) : "无"}</dd>
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
  graph,
  onFocus
}: {
  issues: ItemGrantIssue[];
  catalog: ItemGrantCatalog;
  graph: ItemGrantGraph | null;
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
        const edge = issue.edgeId ? graph?.edges.find((candidate) => candidate.id === issue.edgeId) : undefined;
        const nodeId = issue.nodeId ?? edge?.source;
        const node = nodeId ? graph?.nodes.find((candidate) => candidate.id === nodeId) : undefined;
        const spec = node ? catalog.nodes.find((candidate) => candidate.type === node.type) : undefined;
        const name = node ? ((node.data.label as string) || spec?.label || node.type) : "";
        const label = issue.nodeId ? name : issue.edgeId ? (name ? `「${name}」连出的线` : "连线") : "整张画布";
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
