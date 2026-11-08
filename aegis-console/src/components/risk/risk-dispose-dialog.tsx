"use client";

import { useMemo, useState } from "react";
import { CheckCircle2, Gavel, Loader2, XCircle } from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useDisposeRiskMutation, useReviewAssessmentMutation } from "@/lib/risk-hooks";
import type { RiskEnforcement } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { useRiskScope } from "./risk-scope";
import { DURATION_PRESETS, SegmentedControl, ellipsisMiddle, fmtExpiry, useRiskCatalog } from "./risk-shared";

/* ══════════════════════════════════════════════════════════
   处置与复核弹窗

   评估详情、复核队列、账号 / 设备 / IP 画像都会「对这个人做点什么」，
   动作是同一个：选对象（账号 / 设备 / IP / 防火墙）、选黑白观察、选时长。
   一处实现、各处复用，否则某个入口少一个「防火墙」选项没人会发现。
   ══════════════════════════════════════════════════════════ */

export type DisposeSubject = {
  assessmentId?: number;
  appId?: number | null;
  userId?: number | null;
  account?: string;
  ip?: string;
  deviceId?: string;
};

/** 某个对象有没有可处置的标识：没有设备号就不该出现「设备」这个勾选项 */
function subjectValue(target: string, subject: DisposeSubject): string | null {
  switch (target) {
    case "account":
      return subject.account || (subject.userId ? `用户 #${subject.userId}` : null);
    case "device":
      return subject.deviceId ? ellipsisMiddle(subject.deviceId, 12, 6) : null;
    case "ip":
    case "firewall":
      return subject.ip || null;
    default:
      return null;
  }
}

function TargetPicker({ subject, kind, value, onChange }: {
  subject: DisposeSubject; kind: string; value: string[]; onChange: (next: string[]) => void;
}) {
  const { banTargets } = useRiskCatalog();
  // 防火墙只有「拦」：加白或观察一个防火墙规则没有意义
  const options = banTargets.filter((t) => kind === "block" || t.value !== "firewall");
  return (
    <div className="grid gap-1.5 sm:grid-cols-2">
      {options.map((target) => {
        const subjectText = subjectValue(target.value, subject);
        const disabled = !subjectText;
        const checked = value.includes(target.value) && !disabled;
        return (
          <label key={target.value}
            className={cn("flex cursor-pointer items-start gap-2 rounded-lg border px-2.5 py-2 text-xs transition-colors",
              checked && "border-primary/50 bg-primary/5", disabled && "cursor-not-allowed opacity-50")}>
            <Checkbox checked={checked} disabled={disabled} className="mt-0.5"
              onCheckedChange={(next) => onChange(next
                ? [...value.filter((v) => v !== target.value), target.value]
                : value.filter((v) => v !== target.value))} />
            <span className="min-w-0 flex-1">
              <span className="font-medium">{target.label}</span>
              <span className="block truncate font-mono text-[10px] text-muted-foreground">{subjectText ?? "无"}</span>
            </span>
          </label>
        );
      })}
    </div>
  );
}

function DurationPicker({ value, onChange }: { value: number; onChange: (next: number) => void }) {
  return (
    <div className="flex flex-wrap gap-1">
      {DURATION_PRESETS.map((preset) => (
        <button key={preset.value} type="button" onClick={() => onChange(preset.value)}
          className={cn("rounded-md border px-2.5 py-1 text-[11px] transition-colors hover:bg-accent",
            value === preset.value && "border-primary/50 bg-primary/10 font-medium")}>
          {preset.label}
        </button>
      ))}
    </div>
  );
}

/** 处置结果：逐个对象说明成没成，失败标红 —— 「判了封禁」与「真的封上了」是两件事 */
export function EnforcementList({ items }: { items?: RiskEnforcement[] | null }) {
  const { banTargetLabel } = useRiskCatalog();
  const list = items ?? [];
  if (list.length === 0) return null;
  return (
    <div className="space-y-1">
      {list.map((item, index) => (
        <div key={`${item.target}-${index}`}
          className={cn("flex items-start gap-2 rounded-lg border px-2.5 py-1.5 text-[11px]",
            item.ok ? "" : "border-rose-300 bg-rose-50/60 dark:border-rose-900/60 dark:bg-rose-950/30")}>
          {item.ok
            ? <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-emerald-500" />
            : <XCircle className="mt-0.5 size-3.5 shrink-0 text-rose-500" />}
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="font-medium">{banTargetLabel(item.target)}</span>
              <code className="truncate font-mono text-muted-foreground">{item.subject}</code>
            </div>
            {item.detail && (
              <p className={cn("mt-0.5", item.ok ? "text-muted-foreground" : "text-rose-700 dark:text-rose-300")}>
                {item.detail}
              </p>
            )}
          </div>
          <span className="shrink-0 text-muted-foreground">{item.ok ? `至 ${fmtExpiry(item.until)}` : "失败"}</span>
        </div>
      ))}
    </div>
  );
}

/* ── 人工处置 ── */

export function DisposeDialog({ subject, defaultKind = "block", onClose }: {
  subject: DisposeSubject; defaultKind?: string; onClose: () => void;
}) {
  const { listKinds } = useRiskCatalog();
  const { appName } = useRiskScope();
  const disposeMut = useDisposeRiskMutation();
  const initialTargets = useMemo(
    () => ["account", "device", "ip"].filter((t) => subjectValue(t, subject)),
    [subject],
  );
  const [kind, setKind] = useState(defaultKind);
  const [targets, setTargets] = useState<string[]>(initialTargets);
  const [duration, setDuration] = useState<number>(86400);
  const [reason, setReason] = useState("");
  const [result, setResult] = useState<RiskEnforcement[] | null>(null);

  const effective = targets.filter((t) => subjectValue(t, subject) && (kind === "block" || t !== "firewall"));

  const submit = async () => {
    if (effective.length === 0) { toast.error("请选择处置对象"); return; }
    try {
      const data = await disposeMut.mutateAsync({
        ...subject, targets: effective, kind, duration, reason: reason.trim() || undefined,
      });
      const items = data.enforcement ?? [];
      setResult(items);
      const failed = items.filter((i) => !i.ok).length;
      if (failed > 0) toast.warning(`${failed} 项处置失败`);
      else toast.success("处置已生效");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "处置失败");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Gavel className="size-4" />人工处置</DialogTitle>
          <DialogDescription>作用域：{appName(subject.appId)}</DialogDescription>
        </DialogHeader>

        {result ? (
          <EnforcementList items={result} />
        ) : (
          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label className="text-xs">名单</Label>
              <SegmentedControl value={kind} onChange={setKind}
                options={listKinds.map((k) => ({ key: k.value, label: k.label }))} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">对象</Label>
              <TargetPicker subject={subject} kind={kind} value={targets} onChange={setTargets} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">时长</Label>
              <DurationPicker value={duration} onChange={setDuration} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">理由</Label>
              <Textarea rows={2} className="text-xs" value={reason} onChange={(e) => setReason(e.target.value)} />
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>{result ? "关闭" : "取消"}</Button>
          {!result && (
            <Button size="sm" variant={kind === "block" ? "destructive" : "default"}
              disabled={disposeMut.isPending || effective.length === 0} onClick={submit}>
              {disposeMut.isPending && <Loader2 className="size-3.5 animate-spin" />}
              确认处置
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ── 复核 ── */

export function ReviewDialog({ assessmentId, subject, initialResult, onClose, onDone }: {
  assessmentId: number;
  subject: DisposeSubject;
  initialResult: "approved" | "rejected";
  onClose: () => void;
  onDone?: () => void;
}) {
  const reviewMut = useReviewAssessmentMutation();
  const [result, setResult] = useState<"approved" | "rejected">(initialResult);
  const [rejectTargets, setRejectTargets] = useState<string[]>(
    () => ["account", "device"].filter((t) => subjectValue(t, subject)),
  );
  const [allowTargets, setAllowTargets] = useState<string[]>([]);
  const [revokeAuto, setRevokeAuto] = useState(true);
  const [duration, setDuration] = useState<number>(7 * 86400);
  const [comment, setComment] = useState("");

  const approved = result === "approved";
  const targets = (approved ? allowTargets : rejectTargets)
    .filter((t) => subjectValue(t, subject) && (!approved || t !== "firewall"));

  const submit = async () => {
    try {
      await reviewMut.mutateAsync({
        id: assessmentId, result, comment: comment.trim() || undefined,
        targets, duration, revokeAuto: approved ? revokeAuto : undefined,
      });
      toast.success(approved ? "已通过" : targets.length > 0 ? "已驳回并处置" : "已驳回");
      onDone?.();
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "复核失败");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>复核评估 #{assessmentId}</DialogTitle>
          <DialogDescription>{subject.account || subject.ip || "--"}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <SegmentedControl value={result} onChange={setResult}
            options={[{ key: "approved", label: "通过" }, { key: "rejected", label: "驳回" }]} />

          {approved ? (
            <>
              <div className="space-y-1.5">
                <Label className="text-xs">加入白名单</Label>
                <TargetPicker subject={subject} kind="allow" value={allowTargets} onChange={setAllowTargets} />
              </div>
              <label className="flex items-center gap-2 text-xs">
                <Checkbox checked={revokeAuto} onCheckedChange={(v) => setRevokeAuto(Boolean(v))} />
                撤销该记录触发的自动封禁
              </label>
            </>
          ) : (
            <div className="space-y-1.5">
              <Label className="text-xs">处置对象</Label>
              <TargetPicker subject={subject} kind="block" value={rejectTargets} onChange={setRejectTargets} />
            </div>
          )}

          {targets.length > 0 && (
            <div className="space-y-1.5">
              <Label className="text-xs">时长</Label>
              <DurationPicker value={duration} onChange={setDuration} />
            </div>
          )}

          <div className="space-y-1.5">
            <Label className="text-xs">备注</Label>
            <Textarea rows={2} className="text-xs" value={comment} onChange={(e) => setComment(e.target.value)} />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>取消</Button>
          <Button size="sm" variant={approved ? "default" : "destructive"} disabled={reviewMut.isPending} onClick={submit}>
            {reviewMut.isPending && <Loader2 className="size-3.5 animate-spin" />}
            {approved ? "确认通过" : "确认驳回"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
