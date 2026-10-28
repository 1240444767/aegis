"use client";

import { createContext, useContext, useEffect } from "react";
import {
  WorkflowNodePortsData,
  WorkflowPortRender,
  useNodeRender,
  type WorkflowNodeEntity,
  type WorkflowPortEntity
} from "@flowgram.ai/free-layout-editor";
import { AlertTriangle, CircleAlert, Copy, Footprints, Lock, Trash2 } from "lucide-react";
import { NODE_WIDTH } from "@/components/apps/item-grant/item-grant-graph";
import { ITEM_ICON, describeItem } from "@/components/apps/item-grant/item-grant-meta";
import {
  LEVEL_ICON,
  LEVEL_NODE_VISUAL,
  PRIVILEGE_ICON,
  asItemGrantCatalog,
  formatNumber,
  summarizeLevelNode
} from "@/components/apps/levels/level-meta";
import { ToneIcon } from "@/components/users/detail/kit";
import type { ItemGrantIssue } from "@/lib/api/item-grant";
import type { LevelCatalog, LevelLevelData, LevelNodeType, LevelPrivilegeData, LevelRewardData, LevelRow } from "@/lib/api/levels";
import { cn } from "@/lib/utils";

/**
 * 等级画布上的节点卡片（Flowgram 的 renderDefaultNode）。
 *
 * 主干竖排：体系设置与等级、晋级条件的「下一级」端口在卡片底边中点，输入端口在顶边中点；
 * 等级的「奖励与权益」端口在右边，奖励与权益卡片的输入端口在左边。端口是带 `data-port-id` 的元素（useDynamicPort）。
 * 叠加信息来自编辑器上下文：校验问题、编译后的等级号（Lv.N 按主干顺序编排）、推演到达的等级。
 */

export type LevelCanvasOverlay = {
  catalog?: LevelCatalog;
  issues: Map<string, ItemGrantIssue[]>;
  /** 节点 → 编译后的那一级（等级节点）或它挂在哪一级（奖励、权益、晋级条件指向的下一级） */
  levels: Map<string, LevelRow>;
  owners: Map<string, number>;
  /** 推演结果：到达的等级、卡住的晋级条件 */
  reached?: number | null;
  blockedNodeId?: string | null;
  onPortClick?: (port: WorkflowPortEntity, event: React.MouseEvent) => void;
  onDuplicate?: (node: WorkflowNodeEntity) => void;
  onDelete?: (node: WorkflowNodeEntity) => void;
};

export const LevelOverlayContext = createContext<LevelCanvasOverlay>({ issues: new Map(), levels: new Map(), owners: new Map() });

export function LevelNodeCard() {
  const overlay = useContext(LevelOverlayContext);
  const { node, data: rawData, selected, startDrag, ports, selectNode, nodeRef, onFocus, onBlur, readonly } = useNodeRender();
  const data = (rawData ?? {}) as Record<string, unknown>;
  const type = node.flowNodeType as LevelNodeType;
  const visual = LEVEL_NODE_VISUAL[type] ?? LEVEL_NODE_VISUAL.level;
  const spec = overlay.catalog?.nodes.find((candidate) => candidate.type === type);
  const issues = overlay.issues.get(node.id) ?? [];
  const hasError = issues.some((issue) => issue.level === "error");
  const row = overlay.levels.get(node.id);
  const owner = overlay.owners.get(node.id);
  const note = (data.note as string | undefined)?.trim();
  const summary = summarizeLevelNode(type, data, overlay.catalog);

  const simulating = overlay.reached !== undefined && overlay.reached !== null;
  const levelNumber = type === "level" ? row?.level : owner;
  const reached = simulating && levelNumber !== undefined && levelNumber <= (overlay.reached as number);
  const blocked = overlay.blockedNodeId === node.id;
  const dimmed = simulating && type !== "scheme" && !reached && !blocked;

  const outputs = spec?.outputs ?? [];
  const portKey = outputs.map((output) => output.id).join("|");
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => node.getData(WorkflowNodePortsData).updateDynamicPorts());
    return () => window.cancelAnimationFrame(frame);
  }, [node, portKey]);

  const levelData = data as LevelLevelData;
  const levelColor = type === "level" && levelData.color ? levelData.color : visual.accent;
  const LevelIcon = type === "level" ? (LEVEL_ICON[levelData.icon ?? ""] ?? visual.icon) : null;
  const privilege = type === "privilege" ? (data as LevelPrivilegeData) : null;
  const reward = type === "reward" ? (data as LevelRewardData) : null;
  const adapted = overlay.catalog ? asItemGrantCatalog(overlay.catalog) : undefined;
  const label =
    (data.label as string | undefined) ||
    (privilege ? overlay.catalog?.privileges.find((item) => item.kind === privilege.kind)?.label : undefined) ||
    spec?.label ||
    type;

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
          reached && !selected && "border-emerald-500 ring-4 ring-emerald-500/15",
          blocked && !selected && "border-amber-500 ring-4 ring-amber-500/20",
          dimmed && "opacity-40"
        )}
      >
        <span className="absolute inset-x-5 top-0 h-[3px] rounded-b-full" style={{ background: levelColor }} />

        {selected && !readonly ? (
          <div className="absolute -top-10 right-0 flex items-center gap-0.5 rounded-xl border bg-popover p-0.5 shadow-md">
            {type !== "scheme" ? (
              <NodeAction label="复制" onClick={() => overlay.onDuplicate?.(node)}>
                <Copy />
              </NodeAction>
            ) : null}
            <NodeAction label="删除" danger onClick={() => overlay.onDelete?.(node)}>
              <Trash2 />
            </NodeAction>
          </div>
        ) : null}

        <div className="flex items-start gap-2.5 px-3.5 pt-3.5">
          {LevelIcon ? (
            <span
              className="inline-flex size-9 shrink-0 items-center justify-center rounded-[11px] [&>svg]:size-[18px]"
              style={{ background: `color-mix(in oklab, ${levelColor} 16%, transparent)`, color: levelColor }}
            >
              <LevelIcon />
            </span>
          ) : (
            <ToneIcon icon={privilege ? PRIVILEGE_ICON[privilege.kind] ?? visual.icon : visual.icon} tone={visual.tone} size="md" />
          )}
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              {type === "level" ? (
                <span
                  className="shrink-0 rounded-md px-1.5 text-[11px] font-semibold leading-5 tabular-nums"
                  style={{ background: `color-mix(in oklab, ${levelColor} 14%, transparent)`, color: levelColor }}
                >
                  {row ? `Lv.${row.level}` : "未接入"}
                </span>
              ) : null}
              <span className="truncate text-[13px] font-semibold leading-5">{label}</span>
              {issues.length ? (
                hasError ? (
                  <CircleAlert className="size-3.5 shrink-0 text-red-500" aria-label="有错误" />
                ) : (
                  <AlertTriangle className="size-3.5 shrink-0 text-amber-500" aria-label="有提醒" />
                )
              ) : null}
            </div>
            <div className="truncate text-[11px] leading-4 text-muted-foreground">
              {type === "level"
                ? `累计经验 ${formatNumber(levelData.threshold ?? 0)}`
                : owner && type !== "gate"
                  ? `${spec?.label ?? type}，挂在 Lv.${owner}`
                  : owner && type === "gate"
                    ? `升到 Lv.${owner} 前判定`
                    : (spec?.label ?? type)}
            </div>
          </div>
          {reached && type === "level" ? (
            <span className="shrink-0 rounded-lg bg-emerald-500/10 px-1.5 py-0.5 text-[11px] font-semibold text-emerald-600 dark:text-emerald-400">已达到</span>
          ) : null}
          {blocked ? (
            <span className="flex shrink-0 items-center gap-1 rounded-lg bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-semibold text-amber-600 dark:text-amber-400">
              <Lock className="size-3" /> 卡在这里
            </span>
          ) : null}
        </div>

        <div className="space-y-1 px-3.5 pt-2 pb-3">
          {reward ? (
            <>
              <div className="flex flex-wrap gap-1">
                {(reward.items ?? []).slice(0, 6).map((item, index) => {
                  const Icon = ITEM_ICON[item.type] ?? ITEM_ICON.manual;
                  return (
                    <span
                      key={index}
                      className="inline-flex max-w-full items-center gap-1 rounded-lg bg-emerald-500/10 px-1.5 py-0.5 text-[11px] leading-4 text-emerald-700 dark:text-emerald-300"
                    >
                      <Icon className="size-3 shrink-0" />
                      <span className="truncate">{describeItem(item, adapted)}</span>
                    </span>
                  );
                })}
                {(reward.items ?? []).length > 6 ? <span className="px-1 text-[11px] text-muted-foreground">另 {(reward.items ?? []).length - 6} 项</span> : null}
              </div>
              <div className="text-[11px] text-muted-foreground">{summary[0]}</div>
            </>
          ) : type === "level" && row ? (
            <LevelAttachments row={row} />
          ) : (
            summary
              .filter((line) => type !== "level" || !line.startsWith("累计经验"))
              .map((line, index) => (
                <div
                  key={index}
                  className={cn(
                    "truncate text-xs leading-[18px]",
                    index === 0 ? "text-foreground/85" : "text-muted-foreground",
                    type === "gate" && (data.mode as string) === "expr" && index === 0 && "font-mono text-[11px]"
                  )}
                  title={line}
                >
                  {line}
                </div>
              ))
          )}
          {type === "level" && levelData.description ? (
            <div className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{levelData.description}</div>
          ) : null}
          {note ? <div className="line-clamp-2 pt-0.5 text-[11px] leading-4 text-muted-foreground/80">{note}</div> : null}
          {blocked ? (
            <div className="mt-1.5 flex items-start gap-1.5 rounded-lg bg-amber-500/10 px-2 py-1 text-[11px] leading-4 text-amber-700 dark:text-amber-300">
              <Footprints className="mt-px size-3 shrink-0" />
              经验已够，条件不满足
            </div>
          ) : null}
        </div>

        {outputs.map((output) => {
          const bottom = output.id === "next";
          return bottom ? (
            <div key={output.id} className="relative flex h-6 items-center justify-center border-t border-dashed">
              <span className="text-[11px] font-medium text-muted-foreground">{output.label}</span>
              <span
                data-port-id={output.id}
                data-port-type="output"
                data-port-location="bottom"
                className="absolute -bottom-2.5 left-1/2 size-5 -translate-x-1/2"
              />
            </div>
          ) : (
            <div key={output.id} className="relative flex h-7 items-center justify-end gap-1.5 border-t border-dashed pr-3.5 pl-3">
              <span className="size-1.5 shrink-0 rounded-full bg-emerald-500" />
              <span className="truncate text-xs font-medium text-emerald-600 dark:text-emerald-400">{output.label}</span>
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
      {ports.map((port) => {
        const attach = port.portType === "output" && String(port.portID) === "attach";
        const color = attach ? "#10b981" : "color-mix(in oklab, var(--muted-foreground) 80%, transparent)";
        return (
          <WorkflowPortRender
            key={port.id}
            entity={port}
            primaryColor={attach ? "#10b981" : "#6366f1"}
            secondaryColor={color}
            backgroundColor="var(--card)"
            onClick={!readonly && port.portType === "output" ? (event, entity) => overlay.onPortClick?.(entity, event) : undefined}
          />
        );
      })}
    </>
  );
}

/** 等级卡片里的挂件摘要：奖励、权益、晋级条件。 */
function LevelAttachments({ row }: { row: LevelRow }) {
  if (!row.rewards.length && !row.privileges.length && !row.gate) {
    return <div className="text-xs text-muted-foreground">没有挂奖励与权益</div>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {row.gate ? (
        <span className="inline-flex items-center gap-1 rounded-lg bg-amber-500/10 px-1.5 py-0.5 text-[11px] text-amber-700 dark:text-amber-300">
          <Lock className="size-3" /> {row.gate.label || "晋级条件"}
        </span>
      ) : null}
      {row.rewards.map((reward) => (
        <span key={reward.nodeId} className="inline-flex max-w-full items-center gap-1 rounded-lg bg-emerald-500/10 px-1.5 py-0.5 text-[11px] text-emerald-700 dark:text-emerald-300">
          <span className="truncate">{reward.label}</span>
        </span>
      ))}
      {row.privileges.map((privilege) => (
        <span key={privilege.nodeId} className="inline-flex max-w-full items-center gap-1 rounded-lg bg-sky-500/10 px-1.5 py-0.5 text-[11px] text-sky-700 dark:text-sky-300">
          <span className="truncate">{privilege.label}</span>
        </span>
      ))}
    </div>
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
