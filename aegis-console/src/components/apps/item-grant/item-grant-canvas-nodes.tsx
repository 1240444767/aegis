"use client";

import { createContext, useContext, useEffect } from "react";
import {
  WorkflowNodePortsData,
  WorkflowPortRender,
  useNodeRender,
  type WorkflowNodeEntity,
  type WorkflowPortEntity
} from "@flowgram.ai/free-layout-editor";
import { AlertTriangle, CircleAlert, Copy, Footprints, Trash2 } from "lucide-react";
import { NODE_WIDTH } from "@/components/apps/item-grant/item-grant-graph";
import {
  HANDLE_TONE,
  ITEM_ICON,
  NODE_VISUAL,
  describeItem,
  nodeOutputs,
  summarizeNode
} from "@/components/apps/item-grant/item-grant-meta";
import { ToneIcon } from "@/components/users/detail/kit";
import type {
  ItemGrantCatalog,
  ItemGrantEstimate,
  ItemGrantGrantData,
  ItemGrantIssue,
  ItemGrantNodeType,
  ItemGrantOutcome
} from "@/lib/api/item-grant";
import { cn } from "@/lib/utils";

/**
 * 画布上的节点卡片（Flowgram 的 renderDefaultNode）。
 *
 * 左侧一个输入端口（触发器没有），右侧按目录列出输出端口：每个端口是卡片里带 `data-port-id` 的元素，
 * Flowgram 按它的位置算连线端点（useDynamicPort），随机分流增删分支时端口跟着变。
 * 叠加层来自编辑器上下文：校验问题（红 / 黄角标）、抽样预演的到达人数、单人追踪走过的路径（其余节点变淡）。
 */

export type CanvasOverlay = {
  catalog?: ItemGrantCatalog;
  issues: Map<string, ItemGrantIssue[]>;
  estimate?: ItemGrantEstimate | null;
  trace?: ItemGrantOutcome | null;
  /** 点输出端口：在点击处弹出「接一个节点」 */
  onPortClick?: (port: WorkflowPortEntity, event: React.MouseEvent) => void;
  onDuplicate?: (node: WorkflowNodeEntity) => void;
  onDelete?: (node: WorkflowNodeEntity) => void;
};

export const CanvasOverlayContext = createContext<CanvasOverlay>({ issues: new Map() });

function percent(part: number, total: number) {
  if (!total) return "0%";
  const value = (part / total) * 100;
  return `${value >= 10 || value === 0 ? value.toFixed(0) : value.toFixed(1)}%`;
}

export function GrantNodeCard() {
  const overlay = useContext(CanvasOverlayContext);
  const { node, data: rawData, selected, startDrag, ports, selectNode, nodeRef, onFocus, onBlur, readonly } = useNodeRender();
  const data = (rawData ?? {}) as Record<string, unknown>;
  const nodeType = node.flowNodeType as ItemGrantNodeType;
  const visual = NODE_VISUAL[nodeType] ?? NODE_VISUAL.end;
  const spec = overlay.catalog?.nodes.find((candidate) => candidate.type === nodeType);
  const outputs = nodeOutputs(nodeType, data, overlay.catalog);
  const summary = summarizeNode(nodeType, data, overlay.catalog);
  const issues = overlay.issues.get(node.id) ?? [];
  const hasError = issues.some((issue) => issue.level === "error");
  const label = (data.label as string | undefined) || spec?.label || nodeType;
  const note = (data.note as string | undefined)?.trim();

  const estimate = overlay.estimate;
  const hits = estimate?.nodeHits[node.id] ?? 0;
  const traceStep = overlay.trace?.steps.find((step) => step.nodeId === node.id);
  const dimmed = Boolean(overlay.trace) && !traceStep;
  const traced = Boolean(traceStep);

  // 分支增删、改名后让 Flowgram 重新读一遍端口元素
  const portKey = outputs.map((output) => output.id).join("|");
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => node.getData(WorkflowNodePortsData).updateDynamicPorts());
    return () => window.cancelAnimationFrame(frame);
  }, [node, portKey]);

  const toneOf = new Map(outputs.map((output) => [output.id, output.tone]));
  const items = nodeType === "grant" ? ((data as ItemGrantGrantData).items ?? []) : [];

  return (
    <>
      <div
        ref={nodeRef}
        draggable
        onDragStart={startDrag}
        onClick={selectNode}
        onFocus={onFocus}
        onBlur={onBlur}
        data-node-selected={String(selected)}
        style={{ width: NODE_WIDTH }}
        className={cn(
          "group/node relative cursor-default rounded-2xl border bg-card text-card-foreground shadow-[0_1px_2px_rgba(15,23,42,0.06),0_4px_16px_-6px_rgba(15,23,42,0.12)] transition-[box-shadow,opacity,border-color] duration-200",
          selected ? "border-primary ring-4 ring-primary/15" : "border-border hover:border-foreground/20",
          hasError && !selected && "border-red-400/80 dark:border-red-500/60",
          traced && "border-emerald-500 ring-4 ring-emerald-500/15",
          dimmed && "opacity-35"
        )}
      >
        <span className="absolute inset-x-5 top-0 h-[3px] rounded-b-full" style={{ background: visual.accent }} />

        {selected && !readonly && nodeType !== "trigger" ? (
          <div className="absolute -top-10 right-0 flex items-center gap-0.5 rounded-xl border bg-popover p-0.5 shadow-md">
            <NodeAction label="复制" onClick={() => overlay.onDuplicate?.(node)}>
              <Copy />
            </NodeAction>
            <NodeAction label="删除" danger onClick={() => overlay.onDelete?.(node)}>
              <Trash2 />
            </NodeAction>
          </div>
        ) : selected && !readonly ? (
          <div className="absolute -top-10 right-0 flex items-center gap-0.5 rounded-xl border bg-popover p-0.5 shadow-md">
            <NodeAction label="删除" danger onClick={() => overlay.onDelete?.(node)}>
              <Trash2 />
            </NodeAction>
          </div>
        ) : null}

        <div className="flex items-start gap-2.5 px-3.5 pt-3.5">
          <ToneIcon icon={visual.icon} tone={visual.tone} size="md" />
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
            <div className="truncate text-[11px] leading-4 text-muted-foreground">{spec?.label ?? nodeType}</div>
          </div>
          {estimate ? (
            <span className="shrink-0 rounded-lg bg-primary/10 px-1.5 py-0.5 text-[11px] font-semibold tabular-nums text-primary">
              {hits} 人
              <span className="ml-1 font-normal text-primary/70">{percent(hits, estimate.sampled)}</span>
            </span>
          ) : null}
        </div>

        <div className="space-y-1 px-3.5 pt-2 pb-3">
          {items.length ? (
            <div className="flex flex-wrap gap-1">
              {items.slice(0, 6).map((item, index) => {
                const Icon = ITEM_ICON[item.type] ?? ITEM_ICON.manual;
                return (
                  <span
                    key={index}
                    className="inline-flex max-w-full items-center gap-1 rounded-lg bg-emerald-500/10 px-1.5 py-0.5 text-[11px] leading-4 text-emerald-700 dark:text-emerald-300"
                  >
                    <Icon className="size-3 shrink-0" />
                    <span className="truncate">{describeItem(item, overlay.catalog)}</span>
                  </span>
                );
              })}
              {items.length > 6 ? <span className="px-1 text-[11px] text-muted-foreground">另 {items.length - 6} 项</span> : null}
            </div>
          ) : (
            summary.map((line, index) => (
              <div
                key={index}
                className={cn(
                  "truncate text-xs leading-[18px]",
                  index === 0 ? "text-foreground/85" : "text-muted-foreground",
                  nodeType === "condition" && (data.mode as string) === "expr" && "font-mono text-[11px]"
                )}
                title={line}
              >
                {line}
              </div>
            ))
          )}
          {note ? <div className="line-clamp-2 pt-0.5 text-[11px] leading-4 text-muted-foreground/80">{note}</div> : null}
          {traceStep ? (
            <div className="mt-1.5 flex items-start gap-1.5 rounded-lg bg-emerald-500/10 px-2 py-1 text-[11px] leading-4 text-emerald-700 dark:text-emerald-300">
              <Footprints className="mt-px size-3 shrink-0" />
              {traceStep.detail}
            </div>
          ) : null}
        </div>

        {outputs.length ? (
          <div className="border-t border-dashed py-1">
            {outputs.map((output) => {
              const tone = HANDLE_TONE[output.tone] ?? HANDLE_TONE.neutral;
              const taken = traceStep?.handle === output.id;
              const handleHits = estimate?.handleHits[`${node.id}/${output.id}`] ?? 0;
              return (
                <div key={output.id} className="relative flex h-7 items-center justify-end gap-1.5 pr-3.5 pl-3">
                  {estimate ? <span className="text-[11px] tabular-nums text-muted-foreground">{handleHits} 人</span> : null}
                  <span className={cn("size-1.5 shrink-0 rounded-full", tone.dot)} />
                  <span className={cn("truncate text-xs font-medium", tone.text, taken && "underline underline-offset-2")}>{output.label}</span>
                  <span
                    data-port-id={output.id}
                    data-port-type="output"
                    data-port-location="right"
                    className="absolute top-1/2 -right-2.5 size-5 -translate-y-1/2"
                  />
                </div>
              );
            })}
          </div>
        ) : null}
      </div>
      {ports.map((port) => {
        const tone = HANDLE_TONE[port.portType === "input" ? "neutral" : (toneOf.get(String(port.portID)) ?? "neutral")] ?? HANDLE_TONE.neutral;
        return (
          <WorkflowPortRender
            key={port.id}
            entity={port}
            primaryColor={tone.stroke}
            secondaryColor={port.portType === "input" ? "var(--muted-foreground)" : tone.stroke}
            backgroundColor="var(--card)"
            onClick={!readonly && port.portType === "output" ? (event, entity) => overlay.onPortClick?.(entity, event) : undefined}
          />
        );
      })}
    </>
  );
}

function NodeAction({
  label,
  danger,
  onClick,
  children
}: {
  label: string;
  danger?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onMouseDown={(event) => event.stopPropagation()}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
      className={cn(
        "flex size-7 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent [&>svg]:size-3.5",
        danger ? "hover:text-destructive" : "hover:text-foreground"
      )}
    >
      {children}
    </button>
  );
}
