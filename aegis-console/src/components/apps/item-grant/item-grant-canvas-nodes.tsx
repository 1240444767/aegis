"use client";

import { createContext, memo, useContext } from "react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  Handle,
  Position,
  useReactFlow,
  type EdgeProps,
  type NodeProps
} from "@xyflow/react";
import { AlertTriangle, CircleAlert, X } from "lucide-react";
import {
  HANDLE_TONE,
  NODE_VISUAL,
  nodeOutputs,
  summarizeNode
} from "@/components/apps/item-grant/item-grant-meta";
import type { FlowNode } from "@/components/apps/item-grant/item-grant-graph";
import { ToneIcon } from "@/components/users/detail/kit";
import type { ItemGrantCatalog, ItemGrantEstimate, ItemGrantIssue, ItemGrantNodeType, ItemGrantOutcome } from "@/lib/api/item-grant";
import { cn } from "@/lib/utils";

/**
 * 画布上的节点与连线。
 *
 * 节点：左侧一个输入端口，右侧按目录列出输出端口（带名称与语义色），正文是配置的一句话摘要；
 * 叠加层来自编辑器上下文 —— 校验问题（红 / 黄角标）、抽样预演的到达人数、单人追踪走过的路径（其余节点变淡）。
 */

export type CanvasOverlay = {
  catalog?: ItemGrantCatalog;
  issues: Map<string, ItemGrantIssue[]>;
  edgeIssues: Set<string>;
  estimate?: ItemGrantEstimate | null;
  trace?: ItemGrantOutcome | null;
};

export const CanvasOverlayContext = createContext<CanvasOverlay>({ issues: new Map(), edgeIssues: new Set() });

function percent(part: number, total: number) {
  if (!total) return "0%";
  const value = (part / total) * 100;
  return `${value >= 10 || value === 0 ? value.toFixed(0) : value.toFixed(1)}%`;
}

function GrantNodeView({ id, type, data, selected }: NodeProps<FlowNode>) {
  const overlay = useContext(CanvasOverlayContext);
  const nodeType = type as ItemGrantNodeType;
  const visual = NODE_VISUAL[nodeType];
  const spec = overlay.catalog?.nodes.find((candidate) => candidate.type === nodeType);
  const outputs = nodeOutputs(nodeType, data, overlay.catalog);
  const summary = summarizeNode(nodeType, data, overlay.catalog);
  const issues = overlay.issues.get(id) ?? [];
  const hasError = issues.some((issue) => issue.level === "error");
  const label = (data.label as string | undefined) || spec?.label || nodeType;

  const estimate = overlay.estimate;
  const hits = estimate?.nodeHits[id] ?? 0;
  const traceStep = overlay.trace?.steps.find((step) => step.nodeId === id);
  const dimmed = Boolean(overlay.trace) && !traceStep;
  const traced = Boolean(traceStep);

  return (
    <div
      className={cn(
        "group relative w-[264px] rounded-2xl border bg-card text-card-foreground shadow-sm transition-[box-shadow,opacity,border-color] duration-200",
        selected ? "border-primary shadow-[0_0_0_3px_color-mix(in_oklab,var(--primary)_18%,transparent)]" : "border-border",
        hasError && !selected && "border-red-400/70 dark:border-red-500/60",
        traced && "border-emerald-500 shadow-[0_0_0_3px_rgba(16,185,129,0.18)]",
        dimmed && "opacity-40"
      )}
    >
      <div className="h-1 rounded-t-2xl" style={{ background: visual.accent }} />
      {spec?.noInput ? null : (
        <Handle
          type="target"
          position={Position.Left}
          className="!size-3 !border-2 !border-background !bg-muted-foreground/70 transition-colors hover:!bg-primary"
        />
      )}

      <div className="flex items-start gap-2.5 px-3 pt-2.5">
        <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-[13px] font-semibold leading-5">{label}</span>
            {issues.length ? (
              hasError ? (
                <CircleAlert className="size-3.5 shrink-0 text-red-500" aria-label="有错误" />
              ) : (
                <AlertTriangle className="size-3.5 shrink-0 text-amber-500" aria-label="有提醒" />
              )
            ) : null}
          </div>
          <div className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">{spec?.label ?? nodeType}</div>
        </div>
        {estimate ? (
          <span className="shrink-0 rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-muted-foreground">
            {hits} 人（{percent(hits, estimate.sampled)}）
          </span>
        ) : null}
      </div>

      <div className="space-y-0.5 px-3 pt-1.5 pb-2.5">
        {summary.map((line, index) => (
          <div
            key={index}
            className={cn(
              "truncate text-[11px] leading-[18px]",
              index === 0 ? "text-foreground/80" : "text-muted-foreground",
              nodeType === "condition" && (data.mode as string) === "expr" && "font-mono"
            )}
            title={line}
          >
            {line}
          </div>
        ))}
        {traceStep ? (
          <div className="mt-1.5 rounded-lg bg-emerald-500/10 px-2 py-1 text-[11px] leading-4 text-emerald-700 dark:text-emerald-300">
            {traceStep.detail}
          </div>
        ) : null}
      </div>

      {outputs.length ? (
        <div className="border-t border-dashed px-3 py-1.5">
          {outputs.map((output) => {
            const tone = HANDLE_TONE[output.tone] ?? HANDLE_TONE.neutral;
            const taken = traceStep?.handle === output.id;
            const handleHits = estimate?.handleHits[`${id}/${output.id}`] ?? 0;
            return (
              <div key={output.id} className="relative flex h-6 items-center justify-end gap-1.5 pr-1">
                {estimate ? (
                  <span className="text-[10px] tabular-nums text-muted-foreground">{handleHits}</span>
                ) : null}
                <span className={cn("truncate text-[11px] font-medium", tone.text, taken && "underline underline-offset-2")}>
                  {output.label}
                </span>
                <Handle
                  type="source"
                  id={output.id}
                  position={Position.Right}
                  className="!-right-[13px] !size-3 !border-2 !border-background transition-transform hover:!scale-125"
                  style={{ background: tone.stroke }}
                />
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

export const GrantNode = memo(GrantNodeView);

export const nodeTypes = {
  trigger: GrantNode,
  audience: GrantNode,
  condition: GrantNode,
  window: GrantNode,
  split: GrantNode,
  limit: GrantNode,
  grant: GrantNode,
  random: GrantNode,
  notify: GrantNode,
  end: GrantNode
};

/** 连线：颜色跟随出发端口的语义，追踪路径上的线流动；选中时中点出现删除按钮，预演时标人数。 */
function GrantEdgeView(props: EdgeProps) {
  const { id, source, sourceHandleId, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, selected } = props;
  const overlay = useContext(CanvasOverlayContext);
  const { deleteElements, getNode } = useReactFlow();
  const [path, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    borderRadius: 14,
    offset: 18
  });
  const sourceNode = getNode(source);
  const outputs = sourceNode ? nodeOutputs(sourceNode.type as ItemGrantNodeType, sourceNode.data, overlay.catalog) : [];
  const output = outputs.find((candidate) => candidate.id === sourceHandleId) ?? outputs[0];
  const tone = HANDLE_TONE[output?.tone ?? "neutral"] ?? HANDLE_TONE.neutral;
  const traced = overlay.trace?.steps.some((step) => step.nodeId === source && step.handle === (sourceHandleId ?? output?.id));
  const invalid = overlay.edgeIssues.has(id);
  const hits = overlay.estimate?.handleHits[`${source}/${sourceHandleId ?? output?.id ?? ""}`];

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={props.markerEnd}
        style={{
          stroke: invalid ? "#ef4444" : tone.stroke,
          strokeWidth: selected || traced ? 2.6 : 1.8,
          strokeDasharray: traced ? "6 4" : undefined,
          animation: traced ? "item-grant-dash 0.8s linear infinite" : undefined,
          opacity: overlay.trace && !traced ? 0.3 : 1
        }}
      />
      <EdgeLabelRenderer>
        {selected ? (
          <button
            type="button"
            className="nodrag nopan pointer-events-auto absolute flex size-5 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm hover:text-destructive"
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
            onClick={() => void deleteElements({ edges: [{ id }] })}
            aria-label="删除连线"
          >
            <X className="size-3" />
          </button>
        ) : hits !== undefined && overlay.estimate ? (
          <span
            className="pointer-events-none absolute rounded-md border bg-background/90 px-1.5 py-0.5 text-[10px] tabular-nums text-muted-foreground"
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
          >
            {hits} 人
          </span>
        ) : null}
      </EdgeLabelRenderer>
    </>
  );
}

export const GrantEdge = memo(GrantEdgeView);

export const edgeTypes = { grant: GrantEdge };
