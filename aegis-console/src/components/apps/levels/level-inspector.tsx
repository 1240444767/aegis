"use client";

import { useState } from "react";
import { useNodeRender, type WorkflowNodeEntity } from "@flowgram.ai/free-layout-editor";
import { Copy, History, Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Field, FieldGroup, ItemListEditor, NumberInput } from "@/components/apps/item-grant/item-grant-fields";
import { ConditionForm } from "@/components/apps/item-grant/item-grant-inspector";
import {
  LEVEL_COLORS,
  LEVEL_ICON,
  LEVEL_NODE_VISUAL,
  PRIVILEGE_ICON,
  asItemGrantCatalog,
  formatNumber,
  privilegeValueText
} from "@/components/apps/levels/level-meta";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { ToneIcon } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { ItemGrantConditionData, ItemGrantIssue } from "@/lib/api/item-grant";
import type {
  LevelCatalog,
  LevelGateData,
  LevelLevelData,
  LevelNodeType,
  LevelPrivilegeData,
  LevelPrivilegeKind,
  LevelRewardData,
  LevelRow,
  LevelSchemeData
} from "@/lib/api/levels";
import { ApiError } from "@/lib/api/client";
import { useBackfillLevelRewardMutation } from "@/lib/level-hooks";
import { cn } from "@/lib/utils";

/**
 * 等级画布的节点检查器：选中一个节点时右侧显示它的配置表单。改动立刻写回节点表单（进撤销历史）。
 * 晋级条件复用物品发放的条件构建器（字段换成晋级条件可用的行为画像），达成奖励复用物品编辑器。
 */

type Patch = (next: Record<string, unknown>) => void;

export function LevelInspector({
  appKey,
  node,
  catalog,
  issues,
  row,
  owner,
  published,
  onDuplicate,
  onDelete
}: {
  appKey: string;
  node: WorkflowNodeEntity;
  catalog: LevelCatalog;
  issues: ItemGrantIssue[];
  /** 等级节点编译后的那一级 */
  row?: LevelRow;
  /** 奖励 / 权益挂在哪一级、晋级条件拦在哪一级之前 */
  owner?: number;
  /** 当前画布与已发布的一致（补发只对已发布的奖励有效） */
  published: boolean;
  onDuplicate: () => void;
  onDelete: () => void;
}) {
  const { data: rawData, updateData } = useNodeRender(node);
  const type = node.flowNodeType as LevelNodeType;
  const spec = catalog.nodes.find((candidate) => candidate.type === type);
  const visual = LEVEL_NODE_VISUAL[type];
  const data = (rawData ?? {}) as Record<string, unknown>;
  const patch: Patch = (next) => updateData({ ...data, ...next });

  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3">
        <ToneIcon icon={visual.icon} tone={visual.tone} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 text-sm font-semibold">
            {spec?.label ?? type}
            {row ? <Badge size="sm" variant="secondary">Lv.{row.level}</Badge> : null}
            {owner && type !== "level" ? (
              <Badge size="sm" variant="outline">
                {type === "gate" ? `拦在 Lv.${owner} 前` : `挂在 Lv.${owner}`}
              </Badge>
            ) : null}
          </div>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{spec?.hint}</p>
        </div>
        <div className="flex shrink-0 gap-1">
          {type !== "scheme" ? (
            <Button size="icon" variant="ghost" className="size-7" onClick={onDuplicate} aria-label="复制节点">
              <Copy className="size-3.5" />
            </Button>
          ) : null}
          <Button size="icon" variant="ghost" className="size-7 hover:text-destructive" onClick={onDelete} aria-label="删除节点">
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      {issues.length ? (
        <div className="space-y-1.5">
          {issues.map((issue, index) => (
            <div
              key={index}
              className={cn(
                "rounded-lg px-2.5 py-1.5 text-xs leading-5",
                issue.level === "error" ? "bg-red-500/10 text-red-700 dark:text-red-300" : "bg-amber-500/10 text-amber-700 dark:text-amber-300"
              )}
            >
              {issue.message}
            </div>
          ))}
        </div>
      ) : null}

      <FieldGroup title="显示">
        <div className="grid gap-3">
          <Field label={type === "scheme" ? "体系名称" : type === "level" ? "等级名称" : type === "reward" ? "礼包名称" : type === "privilege" ? "权益名称" : "条件名称"}>
            <Input
              className="h-8"
              value={(data.label as string) ?? ""}
              maxLength={24}
              placeholder={spec?.label}
              onChange={(event) => patch({ label: event.target.value })}
            />
          </Field>
          <Field label="备注">
            <Textarea
              className="min-h-14 text-xs"
              value={(data.note as string) ?? ""}
              maxLength={200}
              placeholder="写给团队看的说明，不展示给用户"
              onChange={(event) => patch({ note: event.target.value })}
            />
          </Field>
        </div>
      </FieldGroup>

      {type === "scheme" ? <SchemeForm data={data as LevelSchemeData} catalog={catalog} patch={patch} /> : null}
      {type === "level" ? <LevelForm data={data as LevelLevelData} catalog={catalog} patch={patch} row={row} /> : null}
      {type === "gate" ? <GateForm data={data as LevelGateData} catalog={catalog} patch={patch} /> : null}
      {type === "reward" ? (
        <RewardForm appKey={appKey} nodeId={node.id} data={data as LevelRewardData} catalog={catalog} patch={patch} owner={owner} published={published} />
      ) : null}
      {type === "privilege" ? <PrivilegeForm data={data as LevelPrivilegeData} catalog={catalog} patch={patch} /> : null}
    </div>
  );
}

function SchemeForm({ data, catalog, patch }: { data: LevelSchemeData; catalog: LevelCatalog; patch: Patch }) {
  return (
    <>
      <FieldGroup title="升级通知">
        <div className="flex items-center justify-between gap-3 rounded-xl border px-3 py-2.5">
          <div>
            <div className="text-xs font-medium">升级时发站内信</div>
            <p className="text-[11px] leading-4 text-muted-foreground">每升一次发一条，内容里可以列出这次获得的奖励</p>
          </div>
          <Switch checked={Boolean(data.notify)} onCheckedChange={(notify) => patch({ notify })} />
        </div>
        {data.notify ? (
          <>
            <Field label="标题">
              <Input className="h-8" value={data.notifyTitle ?? ""} maxLength={60} onChange={(event) => patch({ notifyTitle: event.target.value })} />
            </Field>
            <Field label="内容">
              <Textarea
                className="min-h-20 text-xs"
                value={data.notifyContent ?? ""}
                maxLength={500}
                onChange={(event) => patch({ notifyContent: event.target.value })}
              />
              <div className="flex flex-wrap gap-1 pt-1">
                {catalog.placeholders.map((placeholder) => (
                  <button
                    key={placeholder.key}
                    type="button"
                    className="rounded border bg-background px-1.5 py-0.5 text-[11px] transition-colors hover:border-primary hover:text-primary"
                    onClick={() => patch({ notifyContent: `${data.notifyContent ?? ""}${placeholder.key}` })}
                  >
                    <span className="font-mono">{placeholder.key}</span> {placeholder.label}
                  </button>
                ))}
              </div>
            </Field>
            <Field label="点开后跳转" hint="默认打开 App 的我的等级">
              <Select value={data.notifyRoute || "level"} onValueChange={(route) => patch({ notifyRoute: route === "level" ? "" : route })}>
                <SelectTrigger size="sm" className="h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper">
                  {catalog.routes.map((route) => (
                    <SelectItem key={route.key} value={route.key}>
                      {route.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </>
        ) : null}
      </FieldGroup>
      <FieldGroup title="App 展示">
        <div className="flex items-center justify-between gap-3 rounded-xl border px-3 py-2.5">
          <div>
            <div className="text-xs font-medium">隐藏未达到的等级</div>
            <p className="text-[11px] leading-4 text-muted-foreground">开启后等级列表只露出到下一级，更高的等级保持神秘</p>
          </div>
          <Switch checked={Boolean(data.hideLocked)} onCheckedChange={(hideLocked) => patch({ hideLocked })} />
        </div>
      </FieldGroup>
    </>
  );
}

export function IconPicker({ value, onChange, color }: { value?: string; onChange: (icon: string) => void; color?: string }) {
  return (
    <div className="grid grid-cols-7 gap-1">
      {Object.entries(LEVEL_ICON).map(([key, Icon]) => {
        const active = value === key;
        return (
          <button
            key={key}
            type="button"
            title={key}
            onClick={() => onChange(active ? "" : key)}
            className={cn(
              "flex aspect-square items-center justify-center rounded-lg border transition-colors [&>svg]:size-4",
              active ? "border-primary bg-primary/10" : "border-transparent bg-muted/40 text-muted-foreground hover:border-border hover:text-foreground"
            )}
            style={active && color ? { color } : undefined}
          >
            <Icon />
          </button>
        );
      })}
    </div>
  );
}

function LevelForm({ data, catalog, patch, row }: { data: LevelLevelData; catalog: LevelCatalog; patch: Patch; row?: LevelRow }) {
  const color = data.color || "#6366f1";
  return (
    <>
      <FieldGroup title="门槛">
        <Field label="累计经验" hint="达到这个累计经验值即升到本级（还要满足拦在本级前的晋级条件）；第一级必须为 0，之后逐级递增">
          <NumberInput value={data.threshold} min={0} max={catalog.limits.maxThreshold} onChange={(threshold) => patch({ threshold })} suffix="点" />
        </Field>
      </FieldGroup>
      <FieldGroup title="外观">
        <Field label="图标" hint="不选时 App 显示等级号">
          <IconPicker value={data.icon} color={color} onChange={(icon) => patch({ icon })} />
        </Field>
        <Field label="颜色">
          <div className="flex flex-wrap gap-1.5">
            {LEVEL_COLORS.map((swatch) => (
              <button
                key={swatch}
                type="button"
                aria-label={swatch}
                onClick={() => patch({ color: swatch })}
                className={cn("size-6 rounded-full border-2 transition-transform hover:scale-110", data.color === swatch ? "border-foreground" : "border-transparent")}
                style={{ background: swatch }}
              />
            ))}
          </div>
          <div className="flex items-center gap-2 pt-1">
            <input
              type="color"
              className="h-8 w-10 cursor-pointer rounded-md border bg-transparent p-0.5"
              value={/^#[0-9a-f]{6}$/i.test(color) ? color : "#6366f1"}
              onChange={(event) => patch({ color: event.target.value })}
              aria-label="自定义颜色"
            />
            <Input className="h-8 font-mono text-xs" value={data.color ?? ""} maxLength={7} placeholder="#6366f1" onChange={(event) => patch({ color: event.target.value.trim() })} />
          </div>
        </Field>
        <Field label="等级说明" hint="展示在 App 的等级页">
          <Textarea className="min-h-16 text-xs" value={data.description ?? ""} maxLength={200} onChange={(event) => patch({ description: event.target.value })} />
        </Field>
      </FieldGroup>
      {row ? (
        <FieldGroup title="达到本级后享有">
          {row.effective.length ? (
            <div className="divide-y rounded-xl border">
              {row.effective.map((privilege) => {
                const Icon = PRIVILEGE_ICON[privilege.kind];
                return (
                  <div key={privilege.nodeId} className="flex items-center gap-2 px-3 py-2">
                    <Icon className="size-3.5 shrink-0 text-sky-500" />
                    <span className="min-w-0 flex-1 truncate text-xs">{privilege.label}</span>
                    <span className="shrink-0 text-[11px] text-muted-foreground">
                      {privilegeValueText(privilege, catalog)}
                      {privilege.level !== row.level ? `，来自 Lv.${privilege.level}` : ""}
                    </span>
                  </div>
                );
              })}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">没有权益。从「奖励与权益」端口拖出一个「等级权益」挂上来。</p>
          )}
          {row.rewards.length ? (
            <p className="text-[11px] leading-4 text-muted-foreground">首次达到时发放：{row.rewards.map((reward) => reward.label).join("、")}</p>
          ) : null}
        </FieldGroup>
      ) : null}
    </>
  );
}

function GateForm({ data, catalog, patch }: { data: LevelGateData; catalog: LevelCatalog; patch: Patch }) {
  const adapted = asItemGrantCatalog(catalog);
  return (
    <>
      <FieldGroup title="给用户的说明">
        <Field label="说明" hint="App 在经验已够、条件未满足时显示，例如「需要开通会员」">
          <Input className="h-8" value={data.hint ?? ""} maxLength={60} onChange={(event) => patch({ hint: event.target.value })} />
        </Field>
      </FieldGroup>
      <ConditionForm
        title="晋级条件"
        data={data as ItemGrantConditionData}
        catalog={adapted}
        patch={patch}
        newRule={{ field: "user.signStreak", op: "gte", value: [7] }}
        exprPlaceholder="user.member && user.signStreak >= 7"
      />
      <p className="text-[11px] leading-4 text-muted-foreground">
        条件读的是用户的行为画像（与抽奖、物品发放同一份）。经验变化时与用户打开等级页时判定，后来满足了会自动补升。
      </p>
    </>
  );
}

function RewardForm({
  appKey,
  nodeId,
  data,
  catalog,
  patch,
  owner,
  published
}: {
  appKey: string;
  nodeId: string;
  data: LevelRewardData;
  catalog: LevelCatalog;
  patch: Patch;
  owner?: number;
  published: boolean;
}) {
  const adapted = asItemGrantCatalog(catalog);
  const backfill = useBackfillLevelRewardMutation(appKey);
  const [confirm, setConfirm] = useState(false);
  return (
    <>
      <FieldGroup title="领取方式">
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          className="w-full"
          value={data.claim ?? "auto"}
          onValueChange={(claim) => claim && patch({ claim })}
        >
          <ToggleGroupItem value="auto" className="flex-1 text-xs">
            升级自动到账
          </ToggleGroupItem>
          <ToggleGroupItem value="manual" className="flex-1 text-xs">
            用户领取
          </ToggleGroupItem>
        </ToggleGroup>
        <p className="text-[11px] leading-4 text-muted-foreground">
          {data.claim === "manual"
            ? "升级后进入「待领取」，用户在 App 的我的等级里点领取；适合做礼包、提升回访。"
            : "升级后由后台逐条发放，发放失败会留下记录供重试，不影响升级本身。"}
        </p>
      </FieldGroup>
      <FieldGroup title="物品">
        <ItemListEditor catalog={adapted} items={data.items ?? []} onChange={(items) => patch({ items })} />
      </FieldGroup>
      <FieldGroup title="补发">
        <div className="space-y-2 rounded-xl border px-3 py-2.5">
          <p className="text-[11px] leading-4 text-muted-foreground">
            新挂上的奖励只发给之后升到{owner ? ` Lv.${owner} ` : "本级"}的人。要发给已经达到的人，发布后在这里补发；已有这份奖励的人不会重复。
          </p>
          <Button size="sm" variant="outline" className="w-full" disabled={!published || !owner || backfill.isPending} onClick={() => setConfirm(true)}>
            {backfill.isPending ? <Loader2 className="animate-spin" /> : <History />}
            {published ? "补发给已达到的人" : "发布后才能补发"}
          </Button>
        </div>
      </FieldGroup>
      <ConfirmActionDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="补发达成奖励"
        description={`给所有已达到 Lv.${owner ?? "?"} 、还没有「${data.label || "这份奖励"}」的用户各写一条奖励记录；${
          data.claim === "manual" ? "他们会在 App 里看到待领取。" : "后台会逐条自动发放。"
        }`}
        confirmLabel="补发"
        pending={backfill.isPending}
        onConfirm={() =>
          backfill.mutate(nodeId, {
            onSuccess: (result) => {
              setConfirm(false);
              toast.success(result.created ? `已为 ${formatNumber(result.created)} 人补发` : "没有需要补发的人");
            },
            onError: (error) => toast.error(error instanceof ApiError ? error.message : "补发失败")
          })
        }
      />
    </>
  );
}

function PrivilegeForm({ data, catalog, patch }: { data: LevelPrivilegeData; catalog: LevelCatalog; patch: Patch }) {
  const spec = catalog.privileges.find((item) => item.kind === data.kind);
  return (
    <>
      <FieldGroup title="权益">
        <Field label="种类" hint={spec?.hint}>
          <Select
            value={data.kind}
            onValueChange={(kind) => {
              const next = catalog.privileges.find((item) => item.kind === kind);
              patch({
                kind: kind as LevelPrivilegeKind,
                value: next?.numeric ? Math.max(next.min ?? 1, 1.1) : kind === "custom" ? data.value : undefined,
                key: next?.needsKey ? (data.key ?? "") : undefined
              });
            }}
          >
            <SelectTrigger size="sm" className="h-8 w-full text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper">
              {catalog.privileges.map((item) => (
                <SelectItem key={item.kind} value={item.kind}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        {spec?.needsKey ? (
          <Field label="权益键" hint="App 按这个键判断是否享有，例如 daily_downloads；小写字母开头">
            <Input className="h-8 font-mono text-xs" value={data.key ?? ""} maxLength={48} onChange={(event) => patch({ key: event.target.value.trim() })} />
          </Field>
        ) : null}
        {spec?.numeric || data.kind === "custom" ? (
          <Field label={spec?.numeric ? "倍率" : "数值（可选）"} hint={spec?.numeric ? `${spec.min}–${spec.max} 倍` : "App 读到的数值，例如每日下载次数"}>
            <NumberInput decimal value={data.value} onChange={(value) => patch({ value })} suffix={spec?.unit} />
          </Field>
        ) : null}
        <Field label="说明" hint="展示在 App 的等级页">
          <Input className="h-8" value={data.text ?? ""} maxLength={80} onChange={(event) => patch({ text: event.target.value })} />
        </Field>
        <Field label="图标">
          <IconPicker value={data.icon} onChange={(icon) => patch({ icon })} />
        </Field>
      </FieldGroup>
      <FieldGroup title="作用范围">
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          className="w-full"
          value={data.scope ?? "inherit"}
          onValueChange={(scope) => scope && patch({ scope })}
        >
          <ToggleGroupItem value="inherit" className="flex-1 text-xs">
            本级及以上
          </ToggleGroupItem>
          <ToggleGroupItem value="only" className="flex-1 text-xs">
            仅本级
          </ToggleGroupItem>
        </ToggleGroup>
        <p className="text-[11px] leading-4 text-muted-foreground">倍率与同一个键的自定义权益，多个等级都配置时取最高等级的那一条。</p>
      </FieldGroup>
    </>
  );
}
