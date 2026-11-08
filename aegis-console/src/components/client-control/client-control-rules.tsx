"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ArrowDown, ArrowUp, Pencil, Plus, Trash2 } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { ClientControlConditions, ClientControlRule } from "@/lib/api/client-control";
import {
  useClientControlRulesQuery,
  useDeleteClientControlRuleMutation,
  useReorderClientControlRulesMutation,
  useToggleClientControlRuleMutation
} from "@/lib/client-control-hooks";
import { formatDateTime } from "@/lib/datetime";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/data-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { ClientControlRuleEditor } from "./client-control-rule-editor";
import { ActionBadge, DATA_FONT, shortCert } from "./client-control-shared";

/** 把条件写成一句人话，列表里一眼看出这条规则管谁。 */
export function describeConditions(c: ClientControlConditions): string[] {
  const parts: string[] = [];
  const list = (values?: Array<string | number>, max = 3) =>
    values && values.length > max ? `${values.slice(0, max).join("、")} 等 ${values.length} 个` : (values ?? []).join("、");
  if (c.packages?.length) parts.push(`包名${c.packagesNegate ? "不是" : "是"} ${list(c.packages)}`);
  if (c.versionCodeMin && c.versionCodeMax) parts.push(`版本号 ${c.versionCodeMin}–${c.versionCodeMax}`);
  else if (c.versionCodeMin) parts.push(`版本号 ≥ ${c.versionCodeMin}`);
  else if (c.versionCodeMax) parts.push(`版本号 ≤ ${c.versionCodeMax}`);
  if (c.versionNames?.length) parts.push(`版本名 ${list(c.versionNames)}`);
  if (c.certs?.length) parts.push(`签名${c.certsNegate ? "不是" : "是"} ${list(c.certs.map(shortCert), 2)}`);
  if (c.installers?.length) parts.push(`安装来源${c.installersNegate ? "不是" : "是"} ${list(c.installers)}`);
  if (c.platforms?.length) parts.push(`平台 ${list(c.platforms)}`);
  if (c.osVersionMin && c.osVersionMax) parts.push(`系统 API ${c.osVersionMin}–${c.osVersionMax}`);
  else if (c.osVersionMin) parts.push(`系统 API ≥ ${c.osVersionMin}`);
  else if (c.osVersionMax) parts.push(`系统 API ≤ ${c.osVersionMax}`);
  if (c.deviceIds?.length) parts.push(`${c.deviceIds.length} 台指定设备`);
  if (c.userIds?.length) parts.push(`${c.userIds.length} 个指定用户`);
  return parts;
}

/**
 * 规则列表。从上到下依次匹配，第一条命中的规则决定结果；都没命中时再检查官方包名与签名。
 */
export function ClientControlRules({ appKey }: { appKey: string }) {
  const rulesQuery = useClientControlRulesQuery(appKey);
  const toggle = useToggleClientControlRuleMutation(appKey);
  const remove = useDeleteClientControlRuleMutation(appKey);
  const reorder = useReorderClientControlRulesMutation(appKey);
  const [editor, setEditor] = useState<{ open: boolean; rule?: ClientControlRule | null; seq: number }>({ open: false, seq: 0 });
  const [deleting, setDeleting] = useState<ClientControlRule | null>(null);
  const rules = rulesQuery.data ?? [];
  const topPriority = rules.reduce((max, rule) => Math.max(max, rule.priority), 0);

  const openEditor = (rule?: ClientControlRule | null) => setEditor((state) => ({ open: true, rule: rule ?? null, seq: state.seq + 1 }));

  async function move(index: number, delta: number) {
    const ids = rules.map((rule) => rule.id);
    const target = index + delta;
    if (target < 0 || target >= ids.length) return;
    [ids[index], ids[target]] = [ids[target], ids[index]];
    try {
      await reorder.mutateAsync(ids);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整顺序失败");
    }
  }

  async function setEnabled(rule: ClientControlRule, enabled: boolean) {
    try {
      await toggle.mutateAsync({ ruleId: rule.id, enabled });
      toast.success(enabled ? "已启用" : "已停用");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">从上到下依次匹配，第一条命中的规则决定结果。</p>
        <Button size="sm" onClick={() => openEditor(null)}>
          <Plus className="size-3.5" />
          新建规则
        </Button>
      </div>

      {rulesQuery.isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }, (_, index) => (
            <Skeleton key={index} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : rules.length === 0 ? (
        <EmptyState title="暂无规则" description="没有规则时只检查官方包名与签名，其余一律放行。" />
      ) : (
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
          {rules.map((rule, index) => {
            const conditions = describeConditions(rule.conditions);
            return (
              <div key={rule.id} className={cn("flex items-start gap-3 px-4 py-3", !rule.enabled && "opacity-60")}>
                <div className="flex shrink-0 flex-col">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-6"
                    aria-label="上移"
                    disabled={index === 0 || reorder.isPending}
                    onClick={() => move(index, -1)}
                  >
                    <ArrowUp className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-6"
                    aria-label="下移"
                    disabled={index === rules.length - 1 || reorder.isPending}
                    onClick={() => move(index, 1)}
                  >
                    <ArrowDown className="size-3.5" />
                  </Button>
                </div>
                <div className="min-w-0 flex-1 space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>#{index + 1}</span>
                    <span className="font-medium">{rule.name}</span>
                    <ActionBadge value={rule.action} size="sm" />
                  </div>
                  <p className="text-sm text-muted-foreground">{conditions.length ? conditions.join("，") : "所有客户端"}</p>
                  <p className="text-xs text-muted-foreground">
                    命中 <span className={DATA_FONT}>{rule.hitCount}</span> 次
                    {rule.lastHitAt ? `，最近 ${formatDateTime(rule.lastHitAt)}` : ""}
                    {rule.startsAt || rule.endsAt
                      ? `，生效 ${rule.startsAt ? formatDateTime(rule.startsAt) : "即时"} 至 ${rule.endsAt ? formatDateTime(rule.endsAt) : "不限"}`
                      : ""}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Switch
                    checked={rule.enabled}
                    aria-label={rule.enabled ? "停用规则" : "启用规则"}
                    disabled={toggle.isPending}
                    onCheckedChange={(checked) => setEnabled(rule, checked)}
                  />
                  <Button variant="ghost" size="icon" className="size-8" aria-label="编辑" onClick={() => openEditor(rule)}>
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button variant="ghost" size="icon" className="size-8" aria-label="删除" onClick={() => setDeleting(rule)}>
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {editor.open ? (
        <ClientControlRuleEditor
          key={editor.seq}
          appKey={appKey}
          rule={editor.rule}
          topPriority={topPriority}
          open={editor.open}
          onOpenChange={(open) => setEditor((state) => ({ ...state, open }))}
        />
      ) : null}

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除规则</AlertDialogTitle>
            <AlertDialogDescription>
              删除「{deleting?.name}」后，客户端会在下一次校验时按其余规则重新判定。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                if (!deleting) return;
                try {
                  await remove.mutateAsync(deleting.id);
                  toast.success("已删除");
                } catch (error) {
                  toast.error(error instanceof ApiError ? error.message : "删除失败");
                } finally {
                  setDeleting(null);
                }
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
