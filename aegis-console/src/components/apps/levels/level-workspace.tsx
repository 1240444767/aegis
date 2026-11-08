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
  FlaskConical,
  LayoutTemplate,
  ListChecks,
  ListOrdered,
  Loader2,
  Map as MapIcon,
  Maximize,
  Minus,
  PanelLeft,
  PanelRight,
  Plus,
  Redo2,
  Rocket,
  Rows3,
  Save,
  Settings2,
  Spline,
  Undo2,
  Waypoints,
  X
} from "lucide-react";
import { toast } from "sonner";
import { Field, FieldGroup, NumberInput } from "@/components/apps/item-grant/item-grant-fields";
import { NODE_WIDTH, edgeId, fromCanvas, graphSignature, newId, toCanvas, type CanvasJSON } from "@/components/apps/item-grant/item-grant-graph";
import { LevelNodeCard, LevelOverlayContext, type LevelCanvasOverlay } from "@/components/apps/levels/level-canvas-nodes";
import { LevelInspector } from "@/components/apps/levels/level-inspector";
import {
  LEVEL_ICON,
  LEVEL_NODE_VISUAL,
  PRIVILEGE_ICON,
  asItemGrantCatalog,
  defaultLevelNodeData,
  describeRewardItems,
  formatNumber,
  privilegeValueText
} from "@/components/apps/levels/level-meta";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { ToneIcon } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { ItemGrantGraph, ItemGrantIssue } from "@/lib/api/item-grant";
import type { LevelCatalog, LevelNodeType, LevelRow, LevelSchemeView, LevelSimulation, LevelSummary, LevelTemplate } from "@/lib/api/levels";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";
import {
  usePublishLevelSchemeMutation,
  useSaveLevelSchemeMutation,
  useSimulateLevelMutation,
  useValidateLevelSchemeMutation
} from "@/lib/level-hooks";
import { cn } from "@/lib/utils";

/**
 * 等级体系画布工作区（Flowgram 自由布局编辑器，只在浏览器里加载）。
 *
 * | 区域 | 作用 |
 * |---|---|
 * | 顶栏 | 体系状态（已发布版本、草稿是否有改动）、问题数、模板、保存草稿（⌘S）、发布 |
 * | 节点库（左侧浮层） | 体系设置、等级、晋级条件、达成奖励、等级权益；拖到画布上，或点一下放到画布中央 |
 * | 画布 | 主干竖排：上一级从顶边接入、下一级从底边连出；奖励与权益从等级右侧挂出。Lv.N 按主干顺序自动编排；
 *          只能连合法的线（「下一级」接等级或晋级条件，「奖励与权益」接奖励或权益，每个节点只有一个上游，不成环） |
 * | 底部工具条 | 撤销 / 重做、缩放、适应画布、整理排版（按编译结果排成阶梯）、曲线 / 折线、小地图 |
 * | 右侧 | 配置（选中节点的表单 / 未选中时是体系概况）、问题（实时校验）、推演（按经验或用户，结果叠加在画布上）、等级表 |
 */

const ACCENT = "#6366f1";

type CanvasBridge = {
  edgeIssues: Set<string>;
  onContentChange: () => void;
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

/** 某个端口能接哪些节点。 */
function allowedTargets(port: string): LevelNodeType[] {
  return port === "attach" ? ["reward", "privilege"] : ["level", "gate"];
}

function inputLocation(type: LevelNodeType) {
  return type === "reward" || type === "privilege" ? ("left" as const) : ("top" as const);
}

function buildEditorProps(graph: ItemGrantGraph, catalog: LevelCatalog, bridge: { current: CanvasBridge }): FreeLayoutProps {
  const adapted = asItemGrantCatalog(catalog);
  const selectAll: (ctx: FreeLayoutPluginContext) => ShortcutsHandler = (ctx) => ({
    commandId: "levels.select-all",
    shortcuts: ["meta a", "ctrl a"],
    execute: () => {
      ctx.selection.selection = ctx.document.getAllNodes();
    }
  });
  const remove: (ctx: FreeLayoutPluginContext) => ShortcutsHandler = (ctx) => ({
    commandId: "levels.delete",
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
    commandId: "levels.duplicate",
    shortcuts: ["meta d", "ctrl d"],
    execute: () => {
      const nodes = ctx.selection.selection.filter((entity): entity is WorkflowNodeEntity => entity instanceof WorkflowNodeEntity);
      void duplicateNodes(ctx, nodes);
    }
  });

  return {
    background: { gridSize: 20, dotSize: 1, dotColor: "var(--lv-dot)", dotOpacity: 1 },
    readonly: false,
    twoWayConnection: false,
    initialData: toCanvas(graph, adapted) as unknown as WorkflowJSON,
    nodeRegistries: catalog.nodes.map((spec) => ({
      type: spec.type,
      meta: {
        useDynamicPort: true,
        defaultPorts: spec.inputs ? [{ type: "input" as const, location: inputLocation(spec.type) }] : [],
        defaultExpanded: true
      },
      formMeta: { render: () => <></> }
    })),
    nodeEngine: { enable: true },
    history: { enable: true, enableChangeNode: true },
    materials: { renderDefaultNode: LevelNodeCard },
    lineColor: {
      hidden: "transparent",
      default: "var(--lv-line, #94a3b8)",
      drawing: ACCENT,
      hovered: ACCENT,
      selected: ACCENT,
      error: "#ef4444",
      flowing: "#10b981"
    },
    setLineClassName: (_ctx, line) =>
      cn(String(line.fromPort?.portID ?? "") === "attach" ? "lv-line-attach" : "lv-line-spine", bridge.current.edgeIssues.has(lineEdgeId(line)) && "lv-line-error"),
    canAddLine: (ctx, fromPort, toPort) => {
      if (fromPort.portType !== "output" || toPort.portType !== "input") return false;
      const source = fromPort.node.id;
      const target = toPort.node;
      if (source === target.id) return false;
      if (!allowedTargets(String(fromPort.portID ?? "")).includes(target.flowNodeType as LevelNodeType)) return false;
      const lines = ctx.document.linesManager.getAllLines();
      // 每个节点只有一个上游；「下一级」只能连一条
      if (lines.some((line) => line.to?.id === target.id)) return false;
      if (String(fromPort.portID) === "next" && lines.some((line) => line.from?.id === source && String(line.fromPort?.portID) === "next")) return false;
      // 不成环：从 target 沿连线走不回 source
      const next = new Map<string, string[]>();
      for (const line of lines) if (line.from && line.to) next.set(line.from.id, [...(next.get(line.from.id) ?? []), line.to.id]);
      const stack = [target.id];
      const seen = new Set<string>();
      while (stack.length) {
        const id = stack.pop() as string;
        if (id === source) return false;
        if (seen.has(id)) continue;
        seen.add(id);
        stack.push(...(next.get(id) ?? []));
      }
      return true;
    },
    onDragLineEnd: async (_ctx, params) => {
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

function createEditor(graph: ItemGrantGraph, catalog: LevelCatalog, handlers: Omit<CanvasBridge, "edgeIssues">) {
  const bridge = { current: { edgeIssues: new Set<string>(), ...handlers } as CanvasBridge };
  return {
    props: buildEditorProps(graph, catalog, bridge),
    update: (patch: Partial<Pick<CanvasBridge, "edgeIssues">>) => {
      Object.assign(bridge.current, patch);
    }
  };
}

/** 复制节点（体系设置除外），连同它们之间的连线；整体算一步撤销。 */
async function duplicateNodes(ctx: FreeLayoutPluginContext, nodes: WorkflowNodeEntity[]) {
  const source = nodes.filter((node) => node.flowNodeType !== "scheme");
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

function applyMinimapTheme(minimap: FlowMinimapService, dark: boolean) {
  const root = document.querySelector(".lv-canvas") ?? document.documentElement;
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

/** 服务端草稿按画布往返一次再算签名，与载入后的画布同一口径。 */
export function draftSignature(graph: ItemGrantGraph, catalog: LevelCatalog) {
  return graphSignature(fromCanvas(toCanvas(graph, asItemGrantCatalog(catalog))));
}

type DropMenuState = { port: WorkflowPortEntity; canvas: { x: number; y: number }; client: { x: number; y: number } };

export default function LevelWorkspace({
  appKey,
  scheme,
  catalog,
  graph,
  seededFrom,
  onClose,
  onDirtyChange,
  onReplace
}: {
  appKey: string;
  scheme: LevelSchemeView;
  catalog: LevelCatalog;
  /** 载入画布的内容：草稿、某个模板或某次发布 */
  graph: ItemGrantGraph;
  /** 不是草稿时说明来自哪里（顶栏提示，且一开始就算「未保存」） */
  seededFrom?: string;
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
  /** 用另一张画布替换（模板）：由外层重新挂载编辑器 */
  onReplace: (graph: ItemGrantGraph, from: string) => void;
}) {
  const [revision, setRevision] = useState(0);
  const [baseline, setBaseline] = useState<string | null>(null);
  const [dropMenu, setDropMenu] = useState<DropMenuState | null>(null);
  const [editor] = useState(() =>
    createEditor(graph, catalog, {
      onContentChange: () => setRevision((value) => value + 1),
      onLoaded: (signature) => setBaseline(signature),
      onDropLine: (port, canvas, client) => setDropMenu({ port, canvas, client })
    })
  );
  const [initialSaved] = useState(() => (seededFrom ? draftSignature(scheme.draft, catalog) : null));

  return (
    <FreeLayoutEditorProvider {...editor.props}>
      <Workspace
        appKey={appKey}
        scheme={scheme}
        catalog={catalog}
        seededFrom={seededFrom}
        initialSaved={initialSaved}
        onClose={onClose}
        onDirtyChange={onDirtyChange}
        onReplace={onReplace}
        updateBridge={editor.update}
        revision={revision}
        baseline={baseline}
        dropMenu={dropMenu}
        setDropMenu={setDropMenu}
      />
    </FreeLayoutEditorProvider>
  );
}

function Workspace({
  appKey,
  scheme,
  catalog,
  seededFrom,
  initialSaved,
  onClose,
  onDirtyChange,
  onReplace,
  updateBridge,
  revision,
  baseline,
  dropMenu,
  setDropMenu
}: {
  appKey: string;
  scheme: LevelSchemeView;
  catalog: LevelCatalog;
  seededFrom?: string;
  initialSaved: string | null;
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onReplace: (graph: ItemGrantGraph, from: string) => void;
  updateBridge: (patch: Partial<Pick<CanvasBridge, "edgeIssues">>) => void;
  revision: number;
  baseline: string | null;
  dropMenu: DropMenuState | null;
  setDropMenu: (menu: DropMenuState | null) => void;
}) {
  const ctx = useClientContext();
  const selectService = useService(WorkflowSelectService);
  const canvasRef = useRef<HTMLDivElement>(null);
  const loaded = baseline !== null;
  const [savedSignature, setSavedSignature] = useState<string | null>(initialSaved);
  const [tab, setTab] = useState("config");
  const [panelOpen, setPanelOpen] = useState(true);
  const [paletteOpen, setPaletteOpen] = useState(true);
  const [issues, setIssues] = useState<ItemGrantIssue[]>(scheme.issues);
  const [rows, setRows] = useState<LevelRow[]>(scheme.levels);
  const [summary, setSummary] = useState<LevelSummary>(scheme.summary);
  const [simulation, setSimulation] = useState<LevelSimulation | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [publishOpen, setPublishOpen] = useState(false);
  const [pendingTemplate, setPendingTemplate] = useState<LevelTemplate | null>(null);
  const save = useSaveLevelSchemeMutation(appKey);
  const validate = useValidateLevelSchemeMutation(appKey);
  const runValidate = validate.mutate;

  const toGraph = useCallback((): ItemGrantGraph => fromCanvas(ctx.document.toJSON() as unknown as CanvasJSON), [ctx]);
  const graph = useMemo(() => (loaded ? toGraph() : null), [loaded, revision, toGraph]); // eslint-disable-line react-hooks/exhaustive-deps
  const signature = graph ? graphSignature(graph) : null;
  const reference = savedSignature ?? baseline;
  const dirty = signature !== null && reference !== null && signature !== reference;
  useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange]);

  // 已发布且草稿与已发布一致、画布也没改：补发才有意义
  const published = scheme.publishedVersion > 0 && !scheme.dirty && !dirty;

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

  // 画布一变，旧的推演结果就不再对应当前配置
  const lastSignature = useRef<string | null>(null);
  useEffect(() => {
    if (signature === null) return;
    if (lastSignature.current !== null && lastSignature.current !== signature) setSimulation(null);
    lastSignature.current = signature;
  }, [signature]);

  // 实时校验：停手 600ms 后调后端（与发布时同一套判据），同时拿到编译后的逐级展开
  useEffect(() => {
    if (!loaded) return;
    const timer = setTimeout(
      () =>
        runValidate(toGraph(), {
          onSuccess: (data) => {
            setIssues(data.issues);
            setRows(data.levels);
            setSummary(data.summary);
          }
        }),
      600
    );
    return () => clearTimeout(timer);
  }, [signature, loaded, runValidate, toGraph]);

  const issueMap = useMemo(() => {
    const map = new Map<string, ItemGrantIssue[]>();
    for (const issue of issues) if (issue.nodeId) map.set(issue.nodeId, [...(map.get(issue.nodeId) ?? []), issue]);
    return map;
  }, [issues]);
  const errorCount = issues.filter((issue) => issue.level === "error").length;
  const warningCount = issues.length - errorCount;

  const { levelMap, owners } = useMemo(() => {
    const levelMap = new Map<string, LevelRow>();
    const owners = new Map<string, number>();
    for (const row of rows) {
      levelMap.set(row.nodeId, row);
      if (row.gate) owners.set(row.gate.nodeId, row.level);
      for (const reward of row.rewards) owners.set(reward.nodeId, row.level);
      for (const privilege of row.privileges) owners.set(privilege.nodeId, row.level);
    }
    return { levelMap, owners };
  }, [rows]);

  useEffect(() => {
    updateBridge({ edgeIssues: new Set(issues.filter((issue) => issue.edgeId).map((issue) => issue.edgeId as string)) });
    ctx.document.linesManager.forceUpdate();
  }, [issues, ctx, updateBridge]);

  const removeNode = useCallback(
    (node: WorkflowNodeEntity) => {
      if (ctx.document.canRemove(node)) node.dispose();
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

  const nextThreshold = () => {
    const top = rows.length ? rows[rows.length - 1].threshold : 0;
    const step = rows.length > 1 ? rows[rows.length - 1].threshold - rows[rows.length - 2].threshold : 100;
    return top + Math.max(step, 100);
  };

  const addNodeAt = useCallback(
    (type: LevelNodeType, position: { x: number; y: number }) => {
      if (type === "scheme" && ctx.document.getAllNodes().some((node) => node.flowNodeType === "scheme")) {
        toast.error("画布上只能有一个「体系设置」");
        return undefined;
      }
      const node = ctx.document.createWorkflowNodeByType(type, position, {
        id: newId(type),
        data: defaultLevelNodeData(type, { nextThreshold: nextThreshold() })
      });
      selectService.selectNode(node);
      setTab("config");
      return node;
    },
    [ctx, selectService, rows] // eslint-disable-line react-hooks/exhaustive-deps
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
      const below = String(port.portID) === "next";
      setDropMenu({
        port,
        canvas: below ? { x: canvas.x, y: canvas.y + 60 } : { x: canvas.x + 120 + NODE_WIDTH / 2, y: canvas.y - 30 },
        client: { x: event.clientX + 16, y: event.clientY }
      });
    },
    [ctx, setDropMenu]
  );

  const connectNew = async (menu: DropMenuState, type: LevelNodeType) => {
    setDropMenu(null);
    ctx.history.startTransaction();
    const node = addNodeAt(type, { x: menu.canvas.x, y: menu.canvas.y });
    if (node) {
      await delay(30);
      ctx.document.linesManager.createLine({ from: menu.port.node.id, fromPort: menu.port.portID, to: node.id });
    }
    ctx.history.endTransaction();
  };

  /** 整理排版：主干按编译顺序竖排，挂件横排在所属等级右侧，没接上的节点放在左侧一列。 */
  const tidy = () => {
    const nodes = ctx.document.getAllNodes();
    const byId = new Map(nodes.map((node) => [node.id, node]));
    const schemeNode = nodes.find((node) => node.flowNodeType === "scheme");
    const placed = new Set<string>();
    const height = (node: WorkflowNodeEntity) => Math.max(node.transform.bounds.height || 0, 120);
    ctx.history.startTransaction();
    let y = 0;
    const put = (node: WorkflowNodeEntity | undefined, x: number, top: number) => {
      if (!node || placed.has(node.id)) return;
      ctx.operation.updateNodePosition(node, { x, y: top });
      placed.add(node.id);
    };
    if (schemeNode) {
      put(schemeNode, 0, y);
      y += height(schemeNode) + 70;
    }
    for (const row of rows) {
      if (row.gate) {
        const gate = byId.get(row.gate.nodeId);
        put(gate, 0, y);
        if (gate) y += height(gate) + 70;
      }
      const level = byId.get(row.nodeId);
      put(level, 0, y);
      const attachments = [...row.rewards.map((item) => item.nodeId), ...row.privileges.map((item) => item.nodeId)];
      attachments.forEach((id, index) => put(byId.get(id), NODE_WIDTH + 100 + index * (NODE_WIDTH + 40), y));
      const tallest = Math.max(level ? height(level) : 120, ...attachments.map((id) => (byId.get(id) ? height(byId.get(id) as WorkflowNodeEntity) : 0)));
      y += tallest + 70;
    }
    let looseY = 0;
    for (const node of nodes) {
      if (placed.has(node.id)) continue;
      put(node, -(NODE_WIDTH + 100), looseY);
      looseY += height(node) + 40;
    }
    ctx.history.endTransaction();
    void delay(60).then(() => ctx.tools.fitView());
  };

  const overlay = useMemo<LevelCanvasOverlay>(
    () => ({
      catalog,
      issues: issueMap,
      levels: levelMap,
      owners,
      reached: simulation?.level ?? null,
      blockedNodeId: simulation?.blocked?.nodeId ?? null,
      onPortClick: openPortMenu,
      onDuplicate: (node) => void duplicateNodes(ctx, [node]),
      onDelete: removeNode
    }),
    [catalog, issueMap, levelMap, owners, simulation, openPortMenu, ctx, removeNode]
  );

  const saveDraft = () => {
    const current = toGraph();
    const sent = graphSignature(current);
    save.mutate(
      { graph: current, revision: scheme.draftRevision },
      {
        onSuccess: () => {
          setSavedSignature(sent);
          toast.success("草稿已保存");
        },
        onError: (error) => toast.error(error instanceof ApiError ? error.message : "保存失败")
      }
    );
  };

  useHotkeys(
    "mod+s",
    (event) => {
      event.preventDefault();
      if (!save.isPending) saveDraft();
    },
    { enableOnFormTags: true, enableOnContentEditable: true }
  );

  const empty = loaded && (graph?.nodes.length ?? 0) === 0;
  const hasScheme = Boolean(graph?.nodes.some((node) => (node.type as string) === "scheme"));

  return (
    <LevelOverlayContext.Provider value={overlay}>
      <style>{CANVAS_STYLE}</style>
      <div className="lv-canvas flex h-full flex-col">
        <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-background px-3">
          <Button size="icon" variant="ghost" onClick={onClose} aria-label="返回">
            <ArrowLeft />
          </Button>
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <span className="truncate text-sm font-semibold">{summary.name || "等级体系"}</span>
            {scheme.publishedVersion ? (
              <Badge variant="success" size="sm">
                已发布第 {scheme.publishedVersion} 版
              </Badge>
            ) : (
              <Badge variant="secondary" size="sm">
                {scheme.isDefault ? "默认体系" : "未发布"}
              </Badge>
            )}
            <span className="hidden text-xs text-muted-foreground md:inline">
              {summary.levels} 级，{summary.rewards} 份奖励，{summary.privileges} 项权益
              {summary.gates ? `，${summary.gates} 道晋级条件` : ""}
            </span>
            {seededFrom ? <span className="hidden text-xs text-muted-foreground lg:inline">载入自{seededFrom}</span> : null}
            {dirty ? (
              <span className="hidden items-center gap-1 text-xs text-amber-600 sm:flex dark:text-amber-400">
                <span className="size-1.5 rounded-full bg-amber-500" />
                未保存
              </span>
            ) : scheme.dirty && scheme.publishedVersion ? (
              <span className="hidden text-xs text-muted-foreground sm:inline">草稿与已发布不同</span>
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
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="gap-1.5">
                  <LayoutTemplate />
                  <span className="hidden lg:inline">模板</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-72">
                <DropdownMenuLabel className="text-xs">用模板替换整张画布</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {catalog.templates.map((template) => (
                  <DropdownMenuItem key={template.key} className="flex-col items-start gap-0.5" onSelect={() => setPendingTemplate(template)}>
                    <span className="text-xs font-medium">{template.name}</span>
                    <span className="text-[11px] leading-4 text-muted-foreground">{template.description}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <ToolbarButton label={panelOpen ? "收起右侧面板" : "展开右侧面板"} onClick={() => setPanelOpen(!panelOpen)}>
              <PanelRight />
            </ToolbarButton>
            <span className="mx-1 h-5 w-px bg-border" />
            <Button size="sm" variant="outline" onClick={saveDraft} disabled={save.isPending || !dirty}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存草稿
            </Button>
            <Button size="sm" onClick={() => setPublishOpen(true)} disabled={errorCount > 0 || !loaded}>
              <Rocket />
              发布
            </Button>
          </div>
        </header>

        <div className="relative flex min-h-0 flex-1">
          <div ref={canvasRef} className="relative min-h-0 min-w-0 flex-1 overflow-hidden" onMouseDown={() => dropMenu && setDropMenu(null)}>
            <EditorRenderer className="absolute inset-0" />

            <div className="pointer-events-none absolute top-3 left-3 z-20 flex max-h-[calc(100%-5.5rem)] flex-col">
              {paletteOpen ? (
                <NodePalette catalog={catalog} hasScheme={hasScheme} onCollapse={() => setPaletteOpen(false)} onAdd={(type) => addNodeAt(type, canvasCenter())} />
              ) : (
                <Button variant="outline" size="sm" className="pointer-events-auto gap-1.5 rounded-xl bg-background/95 shadow-sm" onClick={() => setPaletteOpen(true)}>
                  <PanelLeft /> 节点库
                </Button>
              )}
            </div>

            {simulation ? (
              <div className="pointer-events-none absolute top-3 left-1/2 z-20 -translate-x-1/2">
                <div className="pointer-events-auto flex items-center gap-2 rounded-xl border bg-background/95 px-3 py-1.5 text-xs shadow-sm backdrop-blur">
                  <FlaskConical className="size-3.5 text-emerald-500" />
                  {`经验 ${formatNumber(simulation.experience)} 到达 Lv.${simulation.level} ${simulation.name}`}
                  {simulation.blocked ? "，卡在晋级条件" : ""}
                  <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setSimulation(null)}>
                    清除
                  </button>
                </div>
              </div>
            ) : null}

            {empty ? (
              <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
                <div className="rounded-2xl border border-dashed bg-background/90 px-6 py-5 text-center">
                  <div className="text-sm font-medium">从节点库拖一个「体系设置」到画布上开始，或在顶栏选一个模板</div>
                  <p className="mt-1 text-xs text-muted-foreground">从「第一级」端口往下拖线到空白处，就能就地接出等级</p>
                </div>
              </div>
            ) : null}

            <CanvasToolbar onTidy={tidy} />

            {dropMenu ? (
              <QuickAddMenu catalog={catalog} menu={dropMenu} onPick={(type) => void connectNew(dropMenu, type)} onClose={() => setDropMenu(null)} />
            ) : null}
          </div>

          <aside
            className={cn(
              "min-h-0 w-[min(400px,100%)] flex-col border-l bg-background",
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
                      <span className={cn("ml-0.5 rounded-full px-1.5 text-[10px] tabular-nums text-white", errorCount ? "bg-red-500" : "bg-amber-500")}>{issues.length}</span>
                    ) : null}
                  </TabsTrigger>
                  <TabsTrigger value="simulate" className="flex-1 text-xs">
                    <FlaskConical /> 推演
                  </TabsTrigger>
                  <TabsTrigger value="ladder" className="flex-1 text-xs">
                    <ListOrdered /> 等级表
                  </TabsTrigger>
                </TabsList>
                <Button size="icon" variant="ghost" className="size-7 xl:hidden" onClick={() => setPanelOpen(false)} aria-label="收起">
                  <X />
                </Button>
              </div>
              <TabsContent value="config" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                {selectedNode ? (
                  <LevelInspector
                    key={selectedNode.id}
                    appKey={appKey}
                    node={selectedNode}
                    catalog={catalog}
                    issues={issueMap.get(selectedNode.id) ?? []}
                    row={levelMap.get(selectedNode.id)}
                    owner={owners.get(selectedNode.id)}
                    published={published}
                    onDuplicate={() => void duplicateNodes(ctx, [selectedNode])}
                    onDelete={() => removeNode(selectedNode)}
                  />
                ) : (
                  <SchemeOverview scheme={scheme} summary={summary} graph={graph} />
                )}
              </TabsContent>
              <TabsContent value="issues" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                <IssueList issues={issues} catalog={catalog} graph={graph} onFocus={focusNode} />
              </TabsContent>
              <TabsContent value="simulate" className="min-h-0 flex-1 overflow-y-auto px-4 py-4" forceMount hidden={tab !== "simulate"}>
                <SimulatePanel appKey={appKey} catalog={catalog} graph={toGraph} rows={rows} result={simulation} onResult={setSimulation} />
              </TabsContent>
              <TabsContent value="ladder" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                <LadderTable rows={rows} catalog={catalog} onFocus={focusNode} />
              </TabsContent>
            </Tabs>
          </aside>
        </div>
      </div>

      <PublishDialog
        appKey={appKey}
        open={publishOpen}
        onOpenChange={setPublishOpen}
        graph={toGraph}
        warnings={warningCount}
        onPublished={() => setSavedSignature(signature)}
      />
      <ConfirmActionDialog
        open={Boolean(pendingTemplate)}
        onOpenChange={(open) => !open && setPendingTemplate(null)}
        title={`使用模板「${pendingTemplate?.name ?? ""}」`}
        description="整张画布会被替换成模板（还没保存，可以撤回到草稿：关闭编辑器不保存即可）。"
        confirmLabel="替换画布"
        onConfirm={() => {
          if (pendingTemplate) onReplace(pendingTemplate.graph, `模板「${pendingTemplate.name}」`);
          setPendingTemplate(null);
        }}
      />
    </LevelOverlayContext.Provider>
  );
}

const CANVAS_STYLE = `
.lv-canvas {
  --g-editor-background: color-mix(in oklab, var(--muted) 60%, var(--background));
  --g-selection-background: ${ACCENT};
  --g-playground-select: ${ACCENT};
  --g-playground-hover: ${ACCENT};
  --g-playground-line: ${ACCENT};
  --g-playground-selectBox-outline: ${ACCENT};
  --g-playground-selectBox-background: rgba(99, 102, 241, 0.08);
  --g-playground-select-hover-background: rgba(99, 102, 241, 0.08);
  --g-workflow-port-color-background: var(--card);
  --lv-dot: color-mix(in oklab, var(--foreground) 18%, transparent);
}
.lv-canvas .lv-line-spine { --lv-line: ${ACCENT}; }
.lv-canvas .lv-line-attach { --lv-line: #10b981; }
.lv-canvas .lv-line-error { --lv-line: #ef4444; }
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
        <Button size="icon" variant="ghost" className={cn("size-8 rounded-lg", active && "bg-accent text-foreground")} disabled={disabled} onClick={onClick} aria-label={label}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function CanvasToolbar({ onTidy }: { onTidy: () => void }) {
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
        <ToolbarButton label="整理排版（按等级顺序排成阶梯）" onClick={onTidy}>
          <Rows3 />
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

function NodePalette({
  catalog,
  hasScheme,
  onAdd,
  onCollapse
}: {
  catalog: LevelCatalog;
  hasScheme: boolean;
  onAdd: (type: LevelNodeType) => void;
  onCollapse: () => void;
}) {
  const dragService = useService(WorkflowDragService);
  const pressed = useRef<{ type: LevelNodeType; x: number; y: number; dragging: boolean } | null>(null);

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
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-3 pb-3">
        {catalog.categories.map((category) => {
          const specs = catalog.nodes.filter((spec) => spec.category === category.key);
          if (!specs.length) return null;
          return (
            <div key={category.key} className="space-y-1">
              <div className="px-1 text-[11px] font-medium text-muted-foreground">{category.label}</div>
              {specs.map((spec) => {
                const visual = LEVEL_NODE_VISUAL[spec.type];
                const disabled = Boolean(spec.single && hasScheme);
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
                          void dragService.startDragCard(spec.type, event, { id: newId(spec.type), data: defaultLevelNodeData(spec.type) });
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
                            {disabled ? "画布上已有" : spec.outputs.length ? spec.outputs.map((output) => output.label).join(" / ") : "挂在等级上"}
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
          <div className="font-medium text-foreground">怎么搭</div>
          <ol className="mt-1 list-decimal space-y-0.5 pl-4">
            <li>体系设置的「第一级」往下连第一个等级（门槛 0）</li>
            <li>每个等级的「下一级」往下连下一个等级，中间可以插晋级条件</li>
            <li>等级右侧「奖励与权益」挂达成奖励与等级权益</li>
          </ol>
          <div className="mt-2 font-medium text-foreground">快捷键</div>
          <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-2">
            {[
              ["⌘S", "保存草稿"],
              ["⌘Z", "撤销"],
              ["⌘D", "复制"],
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

function QuickAddMenu({
  catalog,
  menu,
  onPick,
  onClose
}: {
  catalog: LevelCatalog;
  menu: DropMenuState;
  onPick: (type: LevelNodeType) => void;
  onClose: () => void;
}) {
  useHotkeys("escape", onClose, { enableOnFormTags: true });
  const width = 248;
  const left = Math.max(8, Math.min(menu.client.x + 8, window.innerWidth - width - 8));
  const top = Math.max(64, Math.min(menu.client.y - 24, window.innerHeight - 260));
  const port = String(menu.port.portID ?? "");
  const allowed = allowedTargets(port);
  const specs = catalog.nodes.filter((spec) => allowed.includes(spec.type));

  return (
    <div
      className="fixed z-40 max-h-[320px] overflow-y-auto rounded-2xl border bg-popover p-1.5 text-popover-foreground shadow-xl"
      style={{ left, top, width }}
      onMouseDown={(event) => event.stopPropagation()}
    >
      <div className="px-2 pt-1 pb-1.5 text-[11px] text-muted-foreground">{port === "attach" ? "挂一个奖励或权益" : "接下一个节点"}</div>
      {specs.map((spec) => {
        const visual = LEVEL_NODE_VISUAL[spec.type];
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
              <div className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{spec.hint}</div>
            </div>
          </button>
        );
      })}
    </div>
  );
}

function SchemeOverview({ scheme, summary, graph }: { scheme: LevelSchemeView; summary: LevelSummary; graph: ItemGrantGraph | null }) {
  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3">
        <ToneIcon icon={LEVEL_NODE_VISUAL.scheme.icon} tone="violet" size="lg" />
        <div>
          <div className="text-sm font-semibold">{summary.name || "等级体系"}</div>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">选中画布上的节点可以编辑它的配置；体系名称与升级通知在「体系设置」节点里。</p>
        </div>
      </div>
      <FieldGroup title="概况">
        <dl className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
          <dt className="text-muted-foreground">等级</dt>
          <dd className="tabular-nums">
            {summary.levels} 级{summary.levels ? `（${summary.firstLevel} 至 ${summary.topLevel}）` : ""}
          </dd>
          <dt className="text-muted-foreground">满级门槛</dt>
          <dd className="tabular-nums">{formatNumber(summary.maxThreshold)} 经验</dd>
          <dt className="text-muted-foreground">达成奖励</dt>
          <dd className="tabular-nums">
            {summary.rewards} 份{summary.manualRewards ? `（${summary.manualRewards} 份需领取）` : ""}
          </dd>
          <dt className="text-muted-foreground">等级权益</dt>
          <dd className="tabular-nums">{summary.privileges} 项</dd>
          <dt className="text-muted-foreground">晋级条件</dt>
          <dd className="tabular-nums">{summary.gates} 道</dd>
          <dt className="text-muted-foreground">节点 / 连线</dt>
          <dd className="tabular-nums">
            {graph?.nodes.length ?? 0} / {graph?.edges.length ?? 0}
          </dd>
        </dl>
      </FieldGroup>
      <FieldGroup title="版本">
        <dl className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
          <dt className="text-muted-foreground">已发布</dt>
          <dd>{scheme.publishedVersion ? `第 ${scheme.publishedVersion} 版` : scheme.isDefault ? "使用默认体系" : "尚未发布"}</dd>
          {scheme.publishedAt ? (
            <>
              <dt className="text-muted-foreground">发布于</dt>
              <dd>
                {formatDateTime(scheme.publishedAt)}
                {scheme.publishedBy ? `（${scheme.publishedBy}）` : ""}
              </dd>
            </>
          ) : null}
          {scheme.draftUpdatedAt ? (
            <>
              <dt className="text-muted-foreground">草稿保存于</dt>
              <dd>
                {formatDateTime(scheme.draftUpdatedAt)}
                {scheme.draftUpdatedBy ? `（${scheme.draftUpdatedBy}）` : ""}
              </dd>
            </>
          ) : null}
        </dl>
      </FieldGroup>
      <p className="rounded-lg bg-muted/60 px-3 py-2 text-xs leading-5 text-muted-foreground">
        保存草稿不影响线上；发布后立即生效。调高门槛不会让已达到的人掉级，需要按新门槛重新判定时在发布时勾选「重算并允许降级」。
      </p>
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
  catalog: LevelCatalog;
  graph: ItemGrantGraph | null;
  onFocus: (id: string) => void;
}) {
  if (issues.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-12 text-center">
        <ToneIcon icon={CircleCheck} tone="success" size="lg" />
        <div className="text-sm font-medium">校验通过</div>
        <p className="max-w-64 text-xs leading-5 text-muted-foreground">主干结构、门槛、奖励物品、晋级条件与引用的套餐、功能都没有问题，可以发布。</p>
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
        const spec = node ? catalog.nodes.find((candidate) => candidate.type === (node.type as string)) : undefined;
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
            {issue.level === "error" ? <CircleAlert className="mt-0.5 size-4 shrink-0 text-red-500" /> : <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-500" />}
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

function SimulatePanel({
  appKey,
  catalog,
  graph,
  rows,
  result,
  onResult
}: {
  appKey: string;
  catalog: LevelCatalog;
  graph: () => ItemGrantGraph;
  rows: LevelRow[];
  result: LevelSimulation | null;
  onResult: (result: LevelSimulation | null) => void;
}) {
  const [experience, setExperience] = useState(0);
  const [user, setUser] = useState("");
  const simulate = useSimulateLevelMutation(appKey);
  const max = rows.length ? Math.max(rows[rows.length - 1].threshold * 1.2, 100) : 1000;
  const run = (payload: { experience?: number; user?: string }) =>
    simulate.mutate(
      { graph: graph(), ...payload },
      {
        onSuccess: onResult,
        onError: (error) => toast.error(error instanceof ApiError ? error.message : "推演失败")
      }
    );

  return (
    <div className="space-y-5">
      <FieldGroup title="按经验推演">
        <Field label="累计经验" hint="晋级条件按「不满足」处理（没有具体的人）">
          <NumberInput value={experience} min={0} onChange={setExperience} suffix="点" />
          <input
            type="range"
            className="w-full accent-primary"
            min={0}
            max={Math.round(max)}
            step={Math.max(1, Math.round(max / 200))}
            value={Math.min(experience, Math.round(max))}
            onChange={(event) => {
              const value = Number(event.target.value);
              setExperience(value);
            }}
            onMouseUp={() => run({ experience })}
            onTouchEnd={() => run({ experience })}
          />
        </Field>
        <Button size="sm" className="w-full" onClick={() => run({ experience })} disabled={simulate.isPending}>
          {simulate.isPending ? <Loader2 className="animate-spin" /> : <FlaskConical />}
          推演
        </Button>
      </FieldGroup>
      <FieldGroup title="按用户推演">
        <Field label="用户 ID 或账号" hint="取这个人的经验，并按他的行为画像判定晋级条件">
          <Input className="h-8" value={user} onChange={(event) => setUser(event.target.value)} placeholder="10086 或 alice" />
        </Field>
        <Button size="sm" variant="outline" className="w-full" onClick={() => run({ user: user.trim() })} disabled={simulate.isPending || !user.trim()}>
          追踪这个人
        </Button>
      </FieldGroup>

      {result ? (
        <div className="space-y-3 rounded-xl border p-3">
          <div className="flex items-center justify-between gap-2">
            <div className="text-sm font-semibold">
              Lv.{result.level} {result.name}
            </div>
            <span className="text-xs tabular-nums text-muted-foreground">经验 {formatNumber(result.experience)}</span>
          </div>
          <Progress value={result.progress} />
          <div className="text-xs text-muted-foreground">{result.toNext ? `距下一级还差 ${formatNumber(result.toNext)} 经验` : "已满级"}</div>
          {result.blocked ? (
            <div className="rounded-lg bg-amber-500/10 px-2.5 py-1.5 text-xs leading-5 text-amber-700 dark:text-amber-300">
              经验已够下一级，卡在晋级条件「{result.blocked.label || result.blocked.hint || "未命名"}」
              {result.blocked.expr ? <code className="mt-1 block font-mono text-[11px]">{result.blocked.expr}</code> : null}
            </div>
          ) : null}
          {result.trace?.length ? (
            <div className="space-y-1">
              <div className="text-[11px] font-medium text-muted-foreground">晋级条件判定</div>
              {result.trace.map((trace) => (
                <div key={trace.nodeId} className="flex items-center justify-between text-xs">
                  <span>
                    Lv.{trace.level} 前：{trace.label || "晋级条件"}
                  </span>
                  <Badge size="sm" variant={trace.passed ? "success" : "warning"}>
                    {trace.passed ? "满足" : trace.error ? "算不出来" : "不满足"}
                  </Badge>
                </div>
              ))}
            </div>
          ) : null}
          <div className="space-y-1">
            <div className="text-[11px] font-medium text-muted-foreground">享有的权益</div>
            {result.effective.length ? (
              result.effective.map((privilege) => {
                const Icon = PRIVILEGE_ICON[privilege.kind];
                return (
                  <div key={privilege.nodeId} className="flex items-center gap-2 text-xs">
                    <Icon className="size-3.5 text-sky-500" />
                    <span className="flex-1 truncate">{privilege.label}</span>
                    <span className="text-muted-foreground">{privilegeValueText(privilege, catalog)}</span>
                  </div>
                );
              })
            ) : (
              <div className="text-xs text-muted-foreground">无</div>
            )}
          </div>
          <div className="space-y-1">
            <div className="text-[11px] font-medium text-muted-foreground">一路获得的达成奖励</div>
            {result.rewards.length ? (
              result.rewards.map((reward) => (
                <div key={reward.nodeId} className="text-xs">
                  <span className="font-medium">{reward.label}</span>
                  <span className="text-muted-foreground">：{describeRewardItems({ items: reward.items, claim: reward.claim }, catalog).join("、")}</span>
                </div>
              ))
            ) : (
              <div className="text-xs text-muted-foreground">无</div>
            )}
          </div>
        </div>
      ) : (
        <p className="text-xs leading-5 text-muted-foreground">推演用的是当前画布（未保存的改动也算），结果会标在画布上：已达到的等级变绿，卡住的晋级条件变黄。</p>
      )}
    </div>
  );
}

function LadderTable({ rows, catalog, onFocus }: { rows: LevelRow[]; catalog: LevelCatalog; onFocus: (id: string) => void }) {
  if (!rows.length) return <p className="py-8 text-center text-xs text-muted-foreground">画布还编译不出等级（先修正问题）</p>;
  return (
    <div className="divide-y rounded-xl border">
      {rows.map((row, index) => {
        const Icon = LEVEL_ICON[row.icon ?? ""];
        const gap = index > 0 ? row.threshold - rows[index - 1].threshold : 0;
        return (
          <button key={row.nodeId} type="button" onClick={() => onFocus(row.nodeId)} className="flex w-full items-center gap-2.5 px-3 py-2 text-left transition-colors hover:bg-accent/40">
            <span
              className="inline-flex size-7 shrink-0 items-center justify-center rounded-lg text-[11px] font-semibold [&>svg]:size-3.5"
              style={{ background: `color-mix(in oklab, ${row.color || "#6366f1"} 16%, transparent)`, color: row.color || "#6366f1" }}
            >
              {Icon ? <Icon /> : row.level}
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-xs font-medium">
                Lv.{row.level} {row.name}
              </div>
              <div className="truncate text-[11px] text-muted-foreground">
                {formatNumber(row.threshold)} 经验{index > 0 ? `，比上一级多 ${formatNumber(gap)}` : ""}
                {row.gate ? "，有晋级条件" : ""}
              </div>
            </div>
            <div className="flex shrink-0 gap-1">
              {row.rewards.length ? <Badge size="sm" variant="success">{row.rewards.length} 奖励</Badge> : null}
              {row.privileges.length ? <Badge size="sm" variant="info">{row.privileges.length} 权益</Badge> : null}
            </div>
          </button>
        );
      })}
      <div className="px-3 py-2 text-[11px] text-muted-foreground">倍率按「{catalog.privileges[0]?.label ?? "签到经验加成"}」等权益计算，见各级「达到本级后享有」。</div>
    </div>
  );
}

function PublishDialog({
  appKey,
  open,
  onOpenChange,
  graph,
  warnings,
  onPublished
}: {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  graph: () => ItemGrantGraph;
  warnings: number;
  onPublished: () => void;
}) {
  const publish = usePublishLevelSchemeMutation(appKey);
  const [note, setNote] = useState("");
  const [recalc, setRecalc] = useState(false);
  const [downgrade, setDowngrade] = useState(false);
  return (
    <ConfirmActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title="发布等级体系"
      description="发布后立即对这个应用的全部用户生效；草稿同时更新为当前画布，并留一份版本快照。"
      confirmLabel="发布"
      pending={publish.isPending}
      note={{ value: note, onChange: setNote, placeholder: "这次改了什么（写进版本记录）", max: 200 }}
      onConfirm={() =>
        publish.mutate(
          { graph: graph(), note: note.trim(), recalc, downgrade: recalc && downgrade },
          {
            onSuccess: (result) => {
              onOpenChange(false);
              onPublished();
              setNote("");
              toast.success(`已发布第 ${result.version} 版${result.job ? "，重算任务已开始" : ""}`);
            },
            onError: (error) => toast.error(error instanceof ApiError ? error.message : "发布失败")
          }
        )
      }
    >
      <div className="space-y-3">
        {warnings ? <p className="rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">画布还有 {warnings} 个提醒，不影响发布。</p> : null}
        <div className="flex items-start gap-2.5">
          <Checkbox id="lv-recalc" checked={recalc} onCheckedChange={(value) => setRecalc(Boolean(value))} />
          <Label htmlFor="lv-recalc" className="grid gap-0.5 text-xs font-normal">
            <span className="font-medium">发布后让全体用户按新体系判定一次</span>
            <span className="text-muted-foreground">调低了门槛、新增了等级时勾选：够格的人立即升级并拿到奖励。不勾选时各人在下次经验变化或打开等级页时判定。</span>
          </Label>
        </div>
        {recalc ? (
          <div className="flex items-start gap-2.5 pl-6">
            <Checkbox id="lv-downgrade" checked={downgrade} onCheckedChange={(value) => setDowngrade(Boolean(value))} />
            <Label htmlFor="lv-downgrade" className="grid gap-0.5 text-xs font-normal">
              <span className="font-medium">允许降级</span>
              <span className="text-muted-foreground">按经验从第一级重新判定；调高了门槛时会有人掉级，已发的奖励不收回。</span>
            </Label>
          </div>
        ) : null}
      </div>
    </ConfirmActionDialog>
  );
}
