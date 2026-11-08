"use client";

import { useMemo, useState } from "react";
import { Reorder, useDragControls } from "motion/react";
import { GripVertical, ListOrdered, Loader2, Plus, Repeat, Scale, Trash2, TrendingDown, X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { toast } from "sonner";
import { SliderField, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { ApiError } from "@/lib/api/client";
import type {
  StorageMemberMode,
  StoragePool,
  StoragePoolStrategy,
  StorageProviderMeta,
  StorageSource
} from "@/lib/api/storage-center";
import {
  useDeleteStoragePoolMutation,
  useSaveStoragePoolMutation,
  useStorageProvidersQuery
} from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";
import {
  MODE_META,
  SIZE_UNITS,
  STRATEGY_META,
  StorageBrandBadge,
  joinSize,
  providerMeta,
  splitSize,
  type SizeUnit
} from "./storage-shared";

type MemberDraft = {
  configId: number;
  weight: string;
  capacity: string;
  unit: SizeUnit;
  mode: StorageMemberMode;
};

type PoolForm = {
  name: string;
  description: string;
  strategy: StoragePoolStrategy;
  enabled: boolean;
  isDefault: boolean;
  readFallback: boolean;
  maxAttempts: number;
  warnPercent: number;
  members: MemberDraft[];
};

const STRATEGY_ICONS: Record<StoragePoolStrategy, LucideIcon> = {
  priority: ListOrdered,
  round_robin: Repeat,
  weighted: Scale,
  least_used: TrendingDown
};

function seed(pool: StoragePool | null): PoolForm {
  if (!pool) {
    return {
      name: "",
      description: "",
      strategy: "priority",
      enabled: true,
      isDefault: false,
      readFallback: true,
      maxAttempts: 3,
      warnPercent: 85,
      members: []
    };
  }
  return {
    name: pool.name,
    description: pool.description,
    strategy: pool.strategy,
    enabled: pool.enabled,
    isDefault: pool.isDefault,
    readFallback: pool.readFallback,
    maxAttempts: pool.maxAttempts,
    warnPercent: pool.warnPercent,
    members: [...pool.members]
      .sort((a, b) => a.priority - b.priority)
      .map((member) => {
        const size = splitSize(member.capacityBytes);
        return { configId: member.configId, weight: String(member.weight), capacity: size.value, unit: size.unit, mode: member.mode };
      })
  };
}

/** 新建 / 编辑存储池。成员拖动排序即优先级（越靠前越先用）。 */
export function StoragePoolEditor({
  open,
  pool,
  sources,
  onOpenChange
}: {
  open: boolean;
  pool: StoragePool | null;
  sources: StorageSource[];
  onOpenChange: (open: boolean) => void;
}) {
  const providers = useStorageProvidersQuery().data;
  const save = useSaveStoragePoolMutation();
  const remove = useDeleteStoragePoolMutation();
  const [draft, setDraft] = useState<{ key: string; value: PoolForm } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const key = String(pool?.id ?? "new");
  const form = draft?.key === key ? draft.value : seed(pool);
  const patch = (next: Partial<PoolForm>) => setDraft({ key, value: { ...form, ...next } });
  const patchMember = (configId: number, next: Partial<MemberDraft>) =>
    patch({ members: form.members.map((member) => (member.configId === configId ? { ...member, ...next } : member)) });

  const globalSources = useMemo(() => sources.filter((source) => source.scope === "global"), [sources]);
  const available = globalSources.filter((source) => !form.members.some((member) => member.configId === source.id));
  const sourceOf = (id: number) => globalSources.find((source) => source.id === id);

  const close = (next: boolean) => {
    if (!next) setDraft(null);
    onOpenChange(next);
  };

  async function submit() {
    if (!form.name.trim()) {
      toast.error("请填写池名称");
      return;
    }
    try {
      await save.mutateAsync({
        poolId: pool?.id,
        input: {
          name: form.name.trim(),
          description: form.description.trim(),
          strategy: form.strategy,
          enabled: form.enabled,
          isDefault: form.isDefault,
          readFallback: form.readFallback,
          maxAttempts: form.maxAttempts,
          warnPercent: form.warnPercent,
          members: form.members.map((member, index) => ({
            configId: member.configId,
            priority: index * 10,
            weight: Math.max(1, Math.min(1000, Number.parseInt(member.weight, 10) || 1)),
            capacityBytes: joinSize(member.capacity, member.unit),
            mode: member.mode
          }))
        }
      });
      toast.success(pool ? "已保存" : "已创建");
      close(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function confirmRemove() {
    if (!pool) return;
    try {
      await remove.mutateAsync(pool.id);
      toast.success("已删除");
      setConfirmDelete(false);
      close(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent side="right" className="w-[min(720px,100vw)] gap-0 overflow-y-auto p-0">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="text-base">{pool ? `编辑 ${pool.name}` : "新建存储池"}</SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 px-5 py-5">
          <section className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">
                名称<span className="text-destructive">*</span>
              </Label>
              <Input
                value={form.name}
                placeholder="main"
                className="font-mono"
                onChange={(e) => patch({ name: e.target.value.toLowerCase() })}
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">描述</Label>
              <Input value={form.description} onChange={(e) => patch({ description: e.target.value })} />
            </div>
          </section>

          <section className="space-y-2">
            <SectionLabel>选择策略</SectionLabel>
            <div className="grid gap-2 sm:grid-cols-2">
              {(Object.keys(STRATEGY_META) as StoragePoolStrategy[]).map((strategy) => {
                const Icon = STRATEGY_ICONS[strategy];
                const active = form.strategy === strategy;
                return (
                  <button
                    key={strategy}
                    type="button"
                    onClick={() => patch({ strategy })}
                    className={cn(
                      "flex items-start gap-3 rounded-xl bg-muted px-3 py-2.5 text-left transition-colors hover:bg-muted-foreground/10",
                      active && "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30 hover:bg-emerald-500/15"
                    )}
                  >
                    <Icon className={cn("mt-0.5 size-4 text-muted-foreground", active && "text-emerald-600 dark:text-emerald-400")} />
                    <div className="space-y-0.5">
                      <div className="text-sm font-medium">{STRATEGY_META[strategy].label}</div>
                      <div className="text-[11px] text-muted-foreground">{STRATEGY_META[strategy].short}</div>
                    </div>
                  </button>
                );
              })}
            </div>
          </section>

          <section className="space-y-2">
            <div className="flex items-center justify-between">
              <SectionLabel>成员</SectionLabel>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="xs" variant="outline" disabled={available.length === 0}>
                    <Plus />
                    添加成员
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-60">
                  {available.map((source) => (
                    <DropdownMenuItem
                      key={source.id}
                      onClick={() =>
                        patch({
                          members: [
                            ...form.members,
                            { configId: source.id, weight: "1", capacity: "", unit: "GB", mode: "active" }
                          ]
                        })
                      }
                    >
                      <StorageBrandBadge meta={providerMeta(providers, source.provider)} provider={source.provider} size="sm" />
                      <span className="truncate">{source.config_name}</span>
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            {form.members.length === 0 ? (
              <div className="rounded-xl border border-dashed py-8 text-center text-xs text-muted-foreground">暂无成员</div>
            ) : (
              <Reorder.Group
                axis="y"
                values={form.members}
                onReorder={(members) => patch({ members })}
                className="space-y-2"
              >
                {form.members.map((member, index) => (
                  <MemberRow
                    key={member.configId}
                    member={member}
                    index={index}
                    strategy={form.strategy}
                    source={sourceOf(member.configId)}
                    providers={providers}
                    onPatch={(next) => patchMember(member.configId, next)}
                    onRemove={() => patch({ members: form.members.filter((item) => item.configId !== member.configId) })}
                  />
                ))}
              </Reorder.Group>
            )}
          </section>

          <section className="grid gap-2 sm:grid-cols-3">
            <SwitchRow label="启用" checked={form.enabled} onChange={(enabled) => patch({ enabled })} />
            <SwitchRow label="平台默认" checked={form.isDefault} onChange={(isDefault) => patch({ isDefault })} />
            <SwitchRow label="读取回退" checked={form.readFallback} onChange={(readFallback) => patch({ readFallback })} />
          </section>

          <section className="grid gap-3 sm:grid-cols-2">
            <SliderField
              label="单次写入最多尝试"
              value={form.maxAttempts}
              min={1}
              max={10}
              unit="个成员"
              onChange={(maxAttempts) => patch({ maxAttempts })}
            />
            <SliderField
              label="容量预警"
              value={form.warnPercent}
              min={50}
              max={100}
              unit="%"
              onChange={(warnPercent) => patch({ warnPercent })}
            />
          </section>
        </div>

        <div className="sticky bottom-0 flex items-center gap-2 border-t bg-background px-5 py-3">
          {pool ? (
            <Button variant="ghost" className="text-destructive hover:text-destructive" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="size-4" />
              删除
            </Button>
          ) : null}
          <div className="ml-auto flex gap-2">
            <Button variant="ghost" onClick={() => close(false)}>
              取消
            </Button>
            <Button onClick={submit} disabled={save.isPending}>
              {save.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
              {pool ? "保存" : "创建"}
            </Button>
          </div>
        </div>
      </SheetContent>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除存储池 {pool?.name}？已写入的对象不受影响。</AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={confirmRemove}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Sheet>
  );
}

function MemberRow({
  member,
  index,
  strategy,
  source,
  providers,
  onPatch,
  onRemove
}: {
  member: MemberDraft;
  index: number;
  strategy: StoragePoolStrategy;
  source?: StorageSource;
  providers?: StorageProviderMeta[];
  onPatch: (next: Partial<MemberDraft>) => void;
  onRemove: () => void;
}) {
  const controls = useDragControls();
  return (
    <Reorder.Item
      value={member}
      dragListener={false}
      dragControls={controls}
      className="flex flex-wrap items-center gap-2 rounded-xl border bg-card px-2 py-2"
    >
      <button
        type="button"
        className="cursor-grab touch-none rounded-md p-1 text-muted-foreground hover:bg-muted active:cursor-grabbing"
        onPointerDown={(event) => controls.start(event)}
        aria-label="拖动排序"
      >
        <GripVertical className="size-4" />
      </button>
      <span className="w-5 text-center font-mono text-xs tabular-nums text-muted-foreground">{index + 1}</span>
      <StorageBrandBadge meta={providerMeta(providers, source?.provider ?? "")} provider={source?.provider} size="sm" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{source?.config_name ?? `#${member.configId}`}</div>
        {source && !source.enabled ? <div className="text-[10px] text-muted-foreground">已停用</div> : null}
      </div>
      <Select value={member.mode} onValueChange={(mode) => onPatch({ mode: mode as StorageMemberMode })}>
        <SelectTrigger size="sm" className="w-[84px] text-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {(Object.keys(MODE_META) as StorageMemberMode[]).map((mode) => (
            <SelectItem key={mode} value={mode}>
              {MODE_META[mode].label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {strategy === "weighted" ? (
        <div className="flex items-center gap-1">
          <span className="text-[11px] text-muted-foreground">权重</span>
          <Input
            value={member.weight}
            inputMode="numeric"
            className="h-8 w-14 font-mono text-xs"
            onChange={(e) => onPatch({ weight: e.target.value.replace(/[^\d]/g, "") })}
          />
        </div>
      ) : null}
      <div className="flex items-center">
        <Input
          value={member.capacity}
          inputMode="decimal"
          placeholder="不限"
          className="h-8 w-20 rounded-r-none font-mono text-xs"
          onChange={(e) => onPatch({ capacity: e.target.value.replace(/[^\d.]/g, "") })}
        />
        <Select value={member.unit} onValueChange={(unit) => onPatch({ unit: unit as SizeUnit })}>
          <SelectTrigger size="sm" className="w-[64px] rounded-l-none border-l-0 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SIZE_UNITS.map((unit) => (
              <SelectItem key={unit} value={unit}>
                {unit}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Button variant="ghost" size="icon-sm" className="text-muted-foreground" onClick={onRemove} aria-label="移除">
        <X className="size-4" />
      </Button>
    </Reorder.Item>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{children}</div>;
}
