"use client";

import { createContext, useContext } from "react";
import {
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  type Connection,
  type EdgeChange,
  type NodeChange,
  type XYPosition
} from "@xyflow/react";
import { temporal, type TemporalState } from "zundo";
import { useStore } from "zustand";
import { createStore, type StoreApi } from "zustand/vanilla";
import type { ItemGrantCatalog, ItemGrantNodeType } from "@/lib/api/item-grant";
import {
  EDGE_MARKER,
  autoLayout,
  defaultNodeData,
  graphSignature,
  newId,
  type FlowEdge,
  type FlowNode,
  type FlowNodeData
} from "@/components/apps/item-grant/item-grant-graph";

/**
 * 画布编辑器的状态：节点与连线（带撤销 / 重做）。
 *
 * 撤销历史用 zundo：只记节点与连线；选中、拖动中、量尺寸这类变化不算一步（equality 按配置与结构比较）；
 * 连续的变化（拖动一个节点、在表单里连续打字）合成一步 —— 一串变化的第一下记下改之前的样子，
 * 停下 350ms 之后的下一次变化才另起一步。
 */

export type EditorState = {
  nodes: FlowNode[];
  edges: FlowEdge[];
  onNodesChange: (changes: NodeChange<FlowNode>[]) => void;
  onEdgesChange: (changes: EdgeChange<FlowEdge>[]) => void;
  connect: (connection: Connection) => void;
  addNode: (type: ItemGrantNodeType, position: XYPosition, catalog?: ItemGrantCatalog) => string;
  updateData: (id: string, patch: FlowNodeData) => void;
  replaceData: (id: string, data: FlowNodeData) => void;
  removeNodes: (ids: string[]) => void;
  removeEdges: (ids: string[]) => void;
  /** 删掉从某个节点某个端口出发的连线（随机分流删分支时） */
  dropHandleEdges: (nodeId: string, handles: string[]) => void;
  duplicate: (ids: string[]) => string[];
  layout: () => void;
  load: (nodes: FlowNode[], edges: FlowEdge[]) => void;
  select: (id: string | null) => void;
};

type Tracked = Pick<EditorState, "nodes" | "edges">;

export type EditorStore = StoreApi<EditorState> & { temporal: StoreApi<TemporalState<Tracked>> };

function burst<T extends (...args: never[]) => void>(handler: T, quietMs: number) {
  let armed = true;
  let timer: ReturnType<typeof setTimeout> | undefined;
  return ((...args: Parameters<T>) => {
    if (armed) {
      handler(...args);
      armed = false;
    }
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      armed = true;
    }, quietMs);
  }) as T;
}

export function createEditorStore(nodes: FlowNode[], edges: FlowEdge[]): EditorStore {
  return createStore<EditorState>()(
    temporal(
      (set, get) => ({
        nodes,
        edges,
        onNodesChange: (changes) => set({ nodes: applyNodeChanges(changes, get().nodes) }),
        onEdgesChange: (changes) => set({ edges: applyEdgeChanges(changes, get().edges) }),
        connect: (connection) => {
          const id = newId("e");
          set({
            edges: addEdge({ ...connection, id, type: "grant", markerEnd: EDGE_MARKER }, get().edges)
          });
        },
        addNode: (type, position, catalog) => {
          const id = type === "trigger" ? "trigger" : newId(type);
          const node: FlowNode = { id, type, position, data: defaultNodeData(type, catalog), selected: true };
          set({ nodes: [...get().nodes.map((item) => ({ ...item, selected: false })), node] });
          return id;
        },
        updateData: (id, patch) =>
          set({ nodes: get().nodes.map((node) => (node.id === id ? { ...node, data: { ...node.data, ...patch } } : node)) }),
        replaceData: (id, data) => set({ nodes: get().nodes.map((node) => (node.id === id ? { ...node, data } : node)) }),
        removeNodes: (ids) => {
          const drop = new Set(ids);
          set({
            nodes: get().nodes.filter((node) => !drop.has(node.id)),
            edges: get().edges.filter((edge) => !drop.has(edge.source) && !drop.has(edge.target))
          });
        },
        removeEdges: (ids) => {
          const drop = new Set(ids);
          set({ edges: get().edges.filter((edge) => !drop.has(edge.id)) });
        },
        dropHandleEdges: (nodeId, handles) => {
          const drop = new Set(handles);
          set({ edges: get().edges.filter((edge) => !(edge.source === nodeId && drop.has(edge.sourceHandle ?? ""))) });
        },
        duplicate: (ids) => {
          const source = get().nodes.filter((node) => ids.includes(node.id) && node.type !== "trigger");
          const mapping = new Map<string, string>();
          const copies = source.map((node) => {
            const id = newId(node.type ?? "node");
            mapping.set(node.id, id);
            return {
              ...node,
              id,
              selected: true,
              position: { x: node.position.x + 40, y: node.position.y + 40 },
              data: JSON.parse(JSON.stringify(node.data)) as FlowNodeData
            };
          });
          // 选中的几个节点之间的连线一并复制
          const innerEdges = get()
            .edges.filter((edge) => mapping.has(edge.source) && mapping.has(edge.target))
            .map((edge) => ({
              ...edge,
              id: newId("e"),
              source: mapping.get(edge.source) as string,
              target: mapping.get(edge.target) as string,
              markerEnd: EDGE_MARKER,
              selected: false
            }));
          set({
            nodes: [...get().nodes.map((node) => ({ ...node, selected: false })), ...copies],
            edges: [...get().edges, ...innerEdges]
          });
          return copies.map((node) => node.id);
        },
        layout: () => set({ nodes: autoLayout(get().nodes, get().edges) }),
        load: (nextNodes, nextEdges) => set({ nodes: nextNodes, edges: nextEdges }),
        select: (id) =>
          set({
            nodes: get().nodes.map((node) => (node.selected === (node.id === id) ? node : { ...node, selected: node.id === id })),
            edges: get().edges.map((edge) => (edge.selected ? { ...edge, selected: false } : edge))
          })
      }),
      {
        limit: 100,
        partialize: (state) => ({ nodes: state.nodes, edges: state.edges }),
        equality: (past, current) => graphSignature(past.nodes, past.edges) === graphSignature(current.nodes, current.edges),
        handleSet: (handleSet) =>
          burst(handleSet as unknown as (...args: never[]) => void, 350) as unknown as typeof handleSet
      }
    )
  ) as EditorStore;
}

export const EditorStoreContext = createContext<EditorStore | null>(null);

function useStoreApi() {
  const store = useContext(EditorStoreContext);
  if (!store) throw new Error("EditorStoreContext 未提供");
  return store;
}

export function useEditor<T>(selector: (state: EditorState) => T): T {
  return useStore(useStoreApi(), selector);
}

export function useEditorApi() {
  return useStoreApi();
}

export function useEditorHistory<T>(selector: (state: TemporalState<Tracked>) => T): T {
  return useStore(useStoreApi().temporal, selector);
}
