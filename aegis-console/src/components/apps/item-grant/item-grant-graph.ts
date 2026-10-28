import dagre from "dagre";
import { MarkerType, type Edge, type EdgeMarker, type Node, type Viewport } from "@xyflow/react";
import type { ItemGrantCatalog, ItemGrantGraph, ItemGrantItem, ItemGrantNodeType } from "@/lib/api/item-grant";

/**
 * 画布模型：后端的画布（nodes / edges / viewport）与 React Flow 的节点、连线之间的往返，
 * 新节点的默认配置、自动排版（dagre，从左到右）、连线合法性（不成环、不连回触发器）。
 *
 * React Flow 节点的 `data` 就是后端节点的配置本身，运行期的叠加信息（问题、预演命中、追踪路径）
 * 不放进 data，而是走编辑器上下文 —— 否则撤销历史里会混进这些与配置无关的变化。
 */

export type FlowNodeData = Record<string, unknown>;
export type FlowNode = Node<FlowNodeData, ItemGrantNodeType>;
export type FlowEdge = Edge;

export const NODE_WIDTH = 264;

/** 连线终点的箭头（载入的与新连的都带上） */
export const EDGE_MARKER: EdgeMarker = { type: MarkerType.ArrowClosed, width: 16, height: 16 };
const NODE_BASE_HEIGHT = 112;

export function toFlow(graph?: ItemGrantGraph | null): { nodes: FlowNode[]; edges: FlowEdge[] } {
  const nodes: FlowNode[] = (graph?.nodes ?? []).map((node) => ({
    id: node.id,
    type: node.type,
    position: { x: node.position?.x ?? 0, y: node.position?.y ?? 0 },
    data: { ...(node.data ?? {}) }
  }));
  const edges: FlowEdge[] = (graph?.edges ?? []).map((edge) => ({
    id: edge.id || `${edge.source}:${edge.sourceHandle ?? ""}->${edge.target}`,
    source: edge.source,
    sourceHandle: edge.sourceHandle || null,
    target: edge.target,
    type: "grant",
    markerEnd: EDGE_MARKER
  }));
  return { nodes, edges };
}

export function fromFlow(nodes: FlowNode[], edges: FlowEdge[], viewport?: Viewport): ItemGrantGraph {
  return {
    nodes: nodes.map((node) => ({
      id: node.id,
      type: node.type as ItemGrantNodeType,
      position: { x: Math.round(node.position.x), y: Math.round(node.position.y) },
      data: node.data
    })),
    edges: edges.map((edge) => ({
      id: edge.id,
      source: edge.source,
      sourceHandle: edge.sourceHandle ?? undefined,
      target: edge.target
    })),
    viewport: viewport ? { x: Math.round(viewport.x), y: Math.round(viewport.y), zoom: Number(viewport.zoom.toFixed(3)) } : undefined
  };
}

/** 只比较配置与结构（选中、拖动中、量出来的尺寸都不算改动）：撤销历史与「未保存」提示都用它。 */
export function graphSignature(nodes: FlowNode[], edges: FlowEdge[]) {
  return JSON.stringify([
    nodes.map((node) => [node.id, node.type, Math.round(node.position.x), Math.round(node.position.y), node.data]),
    edges.map((edge) => [edge.id, edge.source, edge.sourceHandle ?? "", edge.target])
  ]);
}

let counter = 0;

/** 新节点 / 分支 / 选项的标识：类型前缀 + 时间 + 计数，同一画布内足够唯一。 */
export function newId(prefix: string) {
  counter = (counter + 1) % 1_000_000;
  return `${prefix}-${Date.now().toString(36)}${counter.toString(36)}`;
}

export function shortId() {
  counter = (counter + 1) % 1_000_000;
  return `${Math.random().toString(36).slice(2, 6)}${counter.toString(36)}`;
}

export function defaultItem(type: string, catalog?: ItemGrantCatalog): ItemGrantItem {
  const spec = catalog?.items.find((item) => item.type === type);
  switch (spec?.value) {
    case "money":
      return { type, money: "1.00" };
    case "ref":
      return { type, refId: catalog?.plans.find((plan) => plan.active)?.id ?? catalog?.plans[0]?.id ?? 0 };
    case "feature":
      return { type, tag: catalog?.features.find((feature) => feature.isActive)?.tag ?? "", amount: 7 };
    case "feature_minutes":
      return { type, tag: catalog?.features.find((feature) => feature.isActive)?.tag ?? "", amount: 60 };
    case "text":
      return { type, text: "" };
    case "":
      return { type };
    default: {
      const presets: Record<string, number> = { points: 100, experience: 100, vip_days: 7, ad_free: 3, lottery_draws: 3, ai_credit: 10 };
      return { type, amount: presets[type] ?? spec?.min ?? 1 };
    }
  }
}

export function defaultNodeData(type: ItemGrantNodeType, catalog?: ItemGrantCatalog): FlowNodeData {
  switch (type) {
    case "trigger":
      return { kind: "manual" };
    case "audience":
      return { scope: "all" };
    case "condition":
      return { mode: "all", rules: [{ field: "user.level", op: "gte", value: [3] }] };
    case "window":
      return { weekdays: [], startMinute: 0, endMinute: 0 };
    case "split":
      return {
        sticky: true,
        branches: [
          { id: shortId(), label: "A 组", weight: 50 },
          { id: shortId(), label: "B 组", weight: 50 }
        ]
      };
    case "limit":
      return { period: "day", perUser: 1 };
    case "grant":
      return { items: [defaultItem("points", catalog)] };
    case "random":
      return {
        picks: 1,
        options: [
          { id: shortId(), label: "积分", weight: 70, items: [defaultItem("points", catalog)] },
          { id: shortId(), label: "抽奖次数", weight: 30, items: [defaultItem("lottery_draws", catalog)] }
        ]
      };
    case "notify":
      return { title: "奖励已到账", content: "{items}" };
    case "end":
      return {};
  }
}

/** 估算节点高度（排版用）：摘要行数与输出端口数取大者。 */
function estimateHeight(node: FlowNode) {
  const measured = node.measured?.height;
  if (measured) return measured;
  const data = node.data as Record<string, unknown>;
  const rows = Array.isArray(data.items) ? data.items.length : Array.isArray(data.options) ? data.options.length + 1 : 1;
  const outputs = Array.isArray(data.branches) ? data.branches.length : 2;
  return NODE_BASE_HEIGHT + Math.max(rows, outputs) * 18;
}

/** 自动排版：dagre 从左到右分层，端口顺序靠 sourceHandle 在同一层里排开。 */
export function autoLayout(nodes: FlowNode[], edges: FlowEdge[]): FlowNode[] {
  const graph = new dagre.graphlib.Graph();
  graph.setGraph({ rankdir: "LR", nodesep: 36, ranksep: 96, marginx: 24, marginy: 24 });
  graph.setDefaultEdgeLabel(() => ({}));
  for (const node of nodes) {
    graph.setNode(node.id, { width: node.measured?.width ?? NODE_WIDTH, height: estimateHeight(node) });
  }
  for (const edge of edges) {
    if (graph.hasNode(edge.source) && graph.hasNode(edge.target)) graph.setEdge(edge.source, edge.target);
  }
  dagre.layout(graph);
  return nodes.map((node) => {
    const position = graph.node(node.id);
    if (!position) return node;
    return {
      ...node,
      position: { x: position.x - position.width / 2, y: position.y - position.height / 2 }
    };
  });
}

/** 从 target 出发沿连线能不能走回 source（连上这条线会不会成环）。 */
export function wouldCycle(edges: FlowEdge[], source: string, target: string) {
  if (source === target) return true;
  const next = new Map<string, string[]>();
  for (const edge of edges) next.set(edge.source, [...(next.get(edge.source) ?? []), edge.target]);
  const stack = [target];
  const seen = new Set<string>();
  while (stack.length) {
    const id = stack.pop() as string;
    if (id === source) return true;
    if (seen.has(id)) continue;
    seen.add(id);
    stack.push(...(next.get(id) ?? []));
  }
  return false;
}

export function cloneGraph(graph: ItemGrantGraph): ItemGrantGraph {
  return JSON.parse(JSON.stringify(graph)) as ItemGrantGraph;
}
