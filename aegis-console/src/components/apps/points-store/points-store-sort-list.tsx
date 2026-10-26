"use client";

import { useState } from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ArrowDownUp, ArrowDownToLine, ArrowUpToLine, GripVertical, Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { ShelfBadge } from "@/components/apps/points-store/points-store-item-card";
import { formatPoints, rewardVisual } from "@/components/apps/points-store/points-store-shared";
import { Section, ToneIcon, errorMessage } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import type { PointsStoreAdminItem } from "@/lib/api/points-store";
import { useReorderPointsStoreItemsMutation } from "@/lib/points-store-hooks";
import { cn } from "@/lib/utils";

/**
 * 调整顺序：拖动（或聚焦后用方向键）排出用户端的展示顺序，保存时按新顺序把排序值写成 10、20、30……
 *
 * 用户端按分类分组，分类的先后取该分类里最靠前的那件商品；所以这里是一条全局顺序，
 * 每行带着分类名，拖动跨分类也能直接看出效果。保存只改排序值，不碰库存（接口单独一条）。
 */
export function PointsStoreSortList({
  appKey,
  items,
  onDone
}: {
  appKey: string;
  items: PointsStoreAdminItem[];
  onDone: () => void;
}) {
  const [order, setOrder] = useState(() => items.map((item) => item.id));
  const mutation = useReorderPointsStoreItemsMutation(appKey);
  const byId = new Map(items.map((item) => [item.id, item]));
  const rows = order.map((id) => byId.get(id)).filter((item): item is PointsStoreAdminItem => Boolean(item));
  const dirty = order.some((id, index) => id !== items[index]?.id);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    setOrder((current) => arrayMove(current, current.indexOf(Number(active.id)), current.indexOf(Number(over.id))));
  };

  const move = (id: number, to: "top" | "bottom") =>
    setOrder((current) => {
      const rest = current.filter((value) => value !== id);
      return to === "top" ? [id, ...rest] : [...rest, id];
    });

  const save = async () => {
    try {
      await mutation.mutateAsync(order);
      toast.success("排序已保存");
      onDone();
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <Section
      flush
      title="调整顺序"
      icon={ArrowDownUp}
      tone="primary"
      description="拖动手柄排序，或聚焦手柄后用方向键与空格。分类的先后取该分类里最靠前的商品。"
      actions={
        <>
          <Button variant="ghost" size="sm" onClick={onDone} disabled={mutation.isPending}>
            取消
          </Button>
          <Button size="sm" onClick={save} disabled={!dirty || mutation.isPending}>
            {mutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            保存顺序
          </Button>
        </>
      }
    >
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={order} strategy={verticalListSortingStrategy}>
          <ol className="flex flex-col divide-y">
            {rows.map((item, index) => (
              <SortableRow
                key={item.id}
                item={item}
                index={index}
                last={index === rows.length - 1}
                onTop={() => move(item.id, "top")}
                onBottom={() => move(item.id, "bottom")}
              />
            ))}
          </ol>
        </SortableContext>
      </DndContext>
    </Section>
  );
}

function SortableRow({
  item,
  index,
  last,
  onTop,
  onBottom
}: {
  item: PointsStoreAdminItem;
  index: number;
  last: boolean;
  onTop: () => void;
  onBottom: () => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: item.id });
  const visual = rewardVisual(item.rewards[0]?.type);
  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "group flex items-center gap-3 bg-card px-3 py-2.5 sm:px-4",
        isDragging && "relative z-10 rounded-lg shadow-lg ring-1 ring-border"
      )}
    >
      <button
        ref={setActivatorNodeRef}
        type="button"
        aria-label={`拖动排序：${item.title}`}
        className="flex size-8 shrink-0 cursor-grab touch-none items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground active:cursor-grabbing"
        {...attributes}
        {...listeners}
      >
        <GripVertical className="size-4" />
      </button>
      <span className="w-6 shrink-0 text-right text-xs tabular-nums text-muted-foreground">{index + 1}</span>
      <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-sm font-medium">{item.title}</span>
        <span className="truncate text-xs text-muted-foreground">
          {item.category || "未分类"}，{formatPoints(item.cost)} 积分
        </span>
      </div>
      <ShelfBadge item={item} />
      <div className="hidden items-center gap-0.5 sm:flex">
        <Button variant="ghost" size="icon-sm" disabled={index === 0} onClick={onTop} aria-label="移到最前">
          <ArrowUpToLine />
        </Button>
        <Button variant="ghost" size="icon-sm" disabled={last} onClick={onBottom} aria-label="移到最后">
          <ArrowDownToLine />
        </Button>
      </div>
    </li>
  );
}
