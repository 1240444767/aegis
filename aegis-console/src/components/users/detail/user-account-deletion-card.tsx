"use client";

import { useState } from "react";
import Link from "next/link";
import {
  ArrowUpRight,
  CalendarClock,
  CheckCircle2,
  ChevronRight,
  Gavel,
  Hourglass,
  Loader2,
  OctagonAlert,
  Undo2,
  UserX,
  XCircle,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import { ModeCard, NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  DeletionActionDialog,
  ExecuteConfirm,
  type DeletionActionKind
} from "@/components/apps/account-deletion/account-deletion-action-dialog";
import { DeletionRequestSheet } from "@/components/apps/account-deletion/account-deletion-request-sheet";
import {
  DeletionStatusBadge,
  MODE_META,
  availableActions,
  modeLabel,
  reasonText,
  sourceLabel
} from "@/components/apps/account-deletion/account-deletion-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { RadioGroup } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { DeletionConfig, DeletionMode, DeletionRequest } from "@/lib/api/account-deletion";
import {
  useCreateUserDeletionMutation,
  useDeletionCatalogQuery,
  useUserDeletionQuery
} from "@/lib/account-deletion-hooks";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { Panel } from "./user-detail-shared";

const REASON_MAX = 500;
/** 说明的常用写法，点一下填入；与应用配置的原因（reasonCode）互不冲突 */
const ADMIN_NOTE_PRESETS = ["用户通过客服申请注销", "账号长期未使用", "违规账号清理"];

/**
 * 用户详情「处置」页签里的账号注销卡片。
 *
 * 有进行中的申请时展示进度与快捷处置（审核、撤销、改期、立即执行）；没有时提供「发起注销」：
 * 管理员代发起不过身份验证与前置条件（结论仍记进快照），立即注销要输入账号名确认。
 * 账号在这里被注销之后用户已不存在，交给 `onUserDeleted` 跳回列表。
 */
export function UserAccountDeletionCard({
  appKey,
  userId,
  account,
  onUserDeleted
}: {
  appKey: string;
  userId: number;
  account: string;
  onUserDeleted: () => void;
}) {
  const can = usePermissionChecker();
  const canWrite = can("app:user:write");
  const viewQuery = useUserDeletionQuery(appKey, userId);
  const catalogQuery = useDeletionCatalogQuery(appKey);
  const view = viewQuery.data;
  const open = view?.open ?? null;
  const history = (view?.history ?? []).filter((item) => item.id !== open?.id);

  const [sheetItem, setSheetItem] = useState<DeletionRequest | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [action, setAction] = useState<DeletionActionKind | null>(null);

  const showSheet = (item: DeletionRequest) => {
    setSheetItem(item);
    setSheetOpen(true);
  };
  const handleDone = (result: DeletionRequest) => {
    if (result.status === "completed") onUserDeleted();
  };

  return (
    <Panel
      title="账号注销"
      icon={<UserX className="size-4" />}
      action={
        <>
          {view && !view.enabled ? (
            <Badge variant="outline" size="sm">
              应用未开放注销
            </Badge>
          ) : null}
          <Button asChild size="sm" variant="ghost" className="h-7 px-2 text-xs text-muted-foreground">
            <Link href={`/apps/${encodeURIComponent(appKey)}?tab=account-deletion`}>
              注销管理 <ArrowUpRight className="size-3.5" />
            </Link>
          </Button>
        </>
      }
      bodyClassName="p-0 sm:px-0"
    >
      {viewQuery.isLoading ? (
        <div className="space-y-3 px-4 py-5 sm:px-5">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-16 w-full rounded-xl" />
          <Skeleton className="h-4 w-64" />
        </div>
      ) : open ? (
        <OpenRequest
          item={open}
          config={view?.config}
          canWrite={canWrite}
          onAction={setAction}
          onShowDetail={() => showSheet(open)}
        />
      ) : canWrite && view ? (
        <CreateForm
          appKey={appKey}
          userId={userId}
          account={account}
          defaultCoolingDays={view.config.coolingDays}
          reasons={view.config.reasons}
          maxCoolingDays={catalogQuery.data?.limits?.maxCoolingDays ?? 180}
          onDone={handleDone}
        />
      ) : (
        <p className="px-4 py-8 text-center text-sm text-muted-foreground sm:px-5">暂无进行中的注销申请</p>
      )}

      {history.length ? (
        <div className="border-t">
          <div className="px-4 pt-3 text-[11px] font-semibold tracking-[0.14em] text-muted-foreground sm:px-5">历史申请</div>
          <ul className="px-2 py-2 sm:px-3">
            {history.map((item) => (
              <li key={item.id}>
                <button
                  type="button"
                  onClick={() => showSheet(item)}
                  className="flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-muted/60"
                >
                  <DeletionStatusBadge status={item.status} />
                  <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                    {modeLabel(item.mode)}，{sourceLabel(item.source)}
                    {reasonText(item, view?.config) ? `，${reasonText(item, view?.config)}` : ""}
                  </span>
                  <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                    {formatDateTimeMinute(item.createdAt)}
                  </span>
                  <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <DeletionActionDialog
        appKey={appKey}
        request={action ? open : null}
        kind={action}
        onOpenChange={(next) => !next && setAction(null)}
        onDone={handleDone}
      />
      <DeletionRequestSheet
        appKey={appKey}
        request={sheetItem}
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        config={view?.config}
        canWrite={canWrite}
        onCompleted={handleDone}
      />
    </Panel>
  );
}

function OpenRequest({
  item,
  config,
  canWrite,
  onAction,
  onShowDetail
}: {
  item: DeletionRequest;
  config?: DeletionConfig | null;
  canWrite: boolean;
  onAction: (kind: DeletionActionKind) => void;
  onShowDetail: () => void;
}) {
  const actions = availableActions(item.status);
  const reason = reasonText(item, config);
  const tone =
    item.status === "failed"
      ? "border-red-200 bg-red-50/60 dark:border-red-900/60 dark:bg-red-950/30"
      : item.status === "reviewing"
        ? "border-amber-200 bg-amber-50/60 dark:border-amber-900/60 dark:bg-amber-950/30"
        : "border-sky-200 bg-sky-50/60 dark:border-sky-900/60 dark:bg-sky-950/30";
  const Icon = item.status === "failed" ? OctagonAlert : item.status === "reviewing" ? Gavel : Hourglass;

  return (
    <div className="space-y-4 px-4 py-4 sm:px-5 sm:py-5">
      <div className={cn("flex flex-col gap-3 rounded-xl border px-4 py-3.5 sm:flex-row sm:items-center", tone)}>
        <Icon className="size-5 shrink-0 text-foreground/70" />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <DeletionStatusBadge status={item.status} />
            <span className="text-sm font-medium">{modeLabel(item.mode)}</span>
            <span className="text-xs text-muted-foreground">{sourceLabel(item.source)}</span>
          </div>
          {item.status === "scheduled" && item.scheduledAt ? (
            <div className="text-sm tabular-nums">
              {formatDateTimeMinute(item.scheduledAt)} 注销
              <span className="ml-1.5 text-muted-foreground">{formatRelative(item.scheduledAt)}</span>
            </div>
          ) : null}
          {item.status === "failed" && item.failure ? (
            <p className="text-xs leading-5 break-words text-red-700 dark:text-red-300">{item.failure}</p>
          ) : null}
          <p className="text-xs leading-5 break-words text-muted-foreground">
            {reason || "未填写原因"}，提交于 {formatDateTimeMinute(item.createdAt)}
          </p>
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={onShowDetail} className="mr-auto">
          查看详情
        </Button>
        {canWrite && actions.cancel ? (
          <Button size="sm" variant="outline" onClick={() => onAction("cancel")}>
            <Undo2 className="size-3.5" /> 撤销
          </Button>
        ) : null}
        {canWrite && actions.reschedule ? (
          <Button size="sm" variant="outline" onClick={() => onAction("reschedule")}>
            <CalendarClock className="size-3.5" /> 改期
          </Button>
        ) : null}
        {canWrite && actions.reject ? (
          <Button size="sm" variant="outline" onClick={() => onAction("reject")}>
            <XCircle className="size-3.5" /> 驳回
          </Button>
        ) : null}
        {canWrite && actions.approve ? (
          <Button size="sm" onClick={() => onAction("approve")}>
            <CheckCircle2 className="size-3.5" /> 批准
          </Button>
        ) : null}
        {canWrite && actions.execute ? (
          <Button size="sm" variant="destructive" onClick={() => onAction("execute")}>
            <OctagonAlert className="size-3.5" /> 立即执行
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function CreateForm({
  appKey,
  userId,
  account,
  defaultCoolingDays,
  reasons,
  maxCoolingDays,
  onDone
}: {
  appKey: string;
  userId: number;
  account: string;
  defaultCoolingDays: number;
  reasons: DeletionConfig["reasons"];
  maxCoolingDays: number;
  onDone: (result: DeletionRequest) => void;
}) {
  const mutation = useCreateUserDeletionMutation(appKey, userId);
  const [mode, setMode] = useState<DeletionMode>("cooling");
  const [coolingDays, setCoolingDays] = useState(String(defaultCoolingDays || 15));
  const [reasonCode, setReasonCode] = useState("");
  const [reason, setReason] = useState("");
  const [notify, setNotify] = useState(true);
  const [confirm, setConfirm] = useState("");

  const days = Number.parseInt(coolingDays, 10);
  const problem = !reasonCode && !reason.trim()
    ? "请选择或填写注销原因"
    : mode === "cooling" && !(days >= 1 && days <= maxCoolingDays)
      ? `冷静期为 1–${maxCoolingDays} 天`
      : mode === "immediate" && confirm.trim() !== account
        ? "输入账号名确认后才能立即注销"
        : null;

  async function submit() {
    if (problem || mutation.isPending) return;
    try {
      const result = await mutation.mutateAsync({
        mode,
        coolingDays: mode === "cooling" ? days : undefined,
        reasonCode: reasonCode || undefined,
        reason: reason.trim(),
        notify,
        confirm: mode === "immediate" ? confirm.trim() : undefined
      });
      if (result.status === "completed") {
        toast.success(`账号 ${account} 已注销`);
      } else if (result.status === "failed") {
        toast.error(result.failure ? `执行失败：${result.failure}` : "执行失败");
      } else {
        toast.success("已进入注销冷静期");
      }
      setReason("");
      setReasonCode("");
      setConfirm("");
      onDone(result);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "发起失败");
    }
  }

  return (
    <>
      <div className="grid gap-6 px-4 py-5 sm:px-5 lg:grid-cols-2">
        <div className="space-y-4">
          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground">注销方式</Label>
            <RadioGroup value={mode} onValueChange={(value) => setMode(value as DeletionMode)}>
              <div className="grid gap-2 sm:grid-cols-2">
                <ModeCard
                  value="cooling"
                  active={mode === "cooling"}
                  title={MODE_META.cooling.label}
                  description="期满自动注销，期间可撤销"
                  icon={<Hourglass className="size-4" />}
                />
                <ModeCard
                  value="immediate"
                  active={mode === "immediate"}
                  title={MODE_META.immediate.label}
                  description="当场删除账号，无法恢复"
                  icon={<Zap className="size-4" />}
                />
              </div>
            </RadioGroup>
          </div>
          {mode === "cooling" ? (
            <div className="max-w-48">
              <NumberField
                label="冷静期"
                value={coolingDays}
                onChange={setCoolingDays}
                min={1}
                max={maxCoolingDays}
                unit="天"
              />
            </div>
          ) : null}
          <SwitchRow label="通知用户" checked={notify} onChange={setNotify} />
        </div>

        <div className="space-y-2">
          <Label className="text-xs text-muted-foreground">
            注销原因<span className="ml-0.5 text-red-500">*</span>
          </Label>
          {reasons.length ? (
            <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label="注销原因">
              {reasons.map((item) => {
                const active = item.code === reasonCode;
                return (
                  <button
                    key={item.code}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    onClick={() => setReasonCode(active ? "" : item.code)}
                    className={cn(
                      "rounded-lg border px-2.5 py-1 text-xs transition-colors",
                      active
                        ? "border-primary bg-primary/10 font-medium text-primary"
                        : "border-border text-muted-foreground hover:text-foreground"
                    )}
                  >
                    {item.label}
                  </button>
                );
              })}
            </div>
          ) : null}
          <div className="flex items-baseline justify-between gap-2 pt-1">
            <span className="text-[11px] text-muted-foreground">
              {reasons.length ? "补充说明（选了原因时可不填）" : "说明"}
            </span>
            <span className="text-[10px] tabular-nums text-muted-foreground">
              {reason.length}/{REASON_MAX}
            </span>
          </div>
          <Textarea
            rows={3}
            value={reason}
            maxLength={REASON_MAX}
            onChange={(event) => setReason(event.target.value)}
            className="resize-none text-sm"
          />
          <div className="flex flex-wrap gap-1.5">
            {ADMIN_NOTE_PRESETS.map((preset) => (
              <button
                key={preset}
                type="button"
                onClick={() => setReason(preset)}
                className="rounded-md border border-border bg-muted px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
              >
                {preset}
              </button>
            ))}
          </div>
          {mode === "immediate" ? (
            <div className="pt-2">
              <ExecuteConfirm account={account} value={confirm} onChange={setConfirm} />
            </div>
          ) : null}
        </div>
      </div>
      <div className="flex flex-col gap-3 border-t bg-muted/30 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5">
        <span className="text-xs text-muted-foreground">
          {problem ??
            (mode === "cooling"
              ? `${days} 天后自动注销，期间可撤销`
              : "提交后立即删除账号")}
        </span>
        <Button
          size="sm"
          variant="destructive"
          disabled={Boolean(problem) || mutation.isPending}
          onClick={() => void submit()}
          className="self-end sm:self-auto"
        >
          {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <UserX className="size-3.5" />}
          {mode === "immediate" ? "立即注销" : "发起注销"}
        </Button>
      </div>
    </>
  );
}
