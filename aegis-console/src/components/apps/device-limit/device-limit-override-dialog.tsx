"use client";

import { useState } from "react";
import { ShieldCheck, SlidersHorizontal } from "lucide-react";
import { toast } from "sonner";
import {
  CLASS_META,
  ClassIcon,
  cleanClassLimits,
  DEVICE_CLASSES,
  LimitStepper
} from "@/components/apps/device-limit/device-limit-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError } from "@/lib/api-client";
import type { ClassLimits, DeviceLimitOverride } from "@/lib/api/device-limit";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { useSaveDeviceLimitOverrideMutation } from "@/lib/device-limit-hooks";
import { cn } from "@/lib/utils";

type Form = {
  user: PickedUser | null;
  exempt: boolean;
  maxDevices: number;
  classLimits: ClassLimits;
  note: string;
  expiresAt: string;
};

const EXPIRY_PRESETS = [
  { label: "7 天", days: 7 },
  { label: "30 天", days: 30 },
  { label: "90 天", days: 90 },
  { label: "长期", days: 0 }
];

function seed(user: PickedUser | null, override?: DeviceLimitOverride | null): Form {
  return {
    user,
    exempt: override?.exempt ?? false,
    maxDevices: override?.maxDevices ?? 5,
    classLimits: cleanClassLimits(override?.classLimits),
    note: override?.note ?? "",
    expiresAt: override?.expiresAt ? toLocalInputValue(override.expiresAt) : ""
  };
}

/**
 * 单人例外：豁免（不受任何设备数限制，含新设备频率），或另配一组上限。优先于会员档位与默认。
 * 打开时就确定了编辑哪个人；`user` 为空时在弹窗里挑。
 */
export function DeviceLimitOverrideDialog({
  appKey,
  open,
  onOpenChange,
  user,
  override
}: {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  user: PickedUser | null;
  override?: DeviceLimitOverride | null;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {open ? (
          <OverrideForm appKey={appKey} initial={seed(user, override)} editing={Boolean(override)} onDone={() => onOpenChange(false)} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function OverrideForm({
  appKey,
  initial,
  editing,
  onDone
}: {
  appKey: string;
  initial: Form;
  editing: boolean;
  onDone: () => void;
}) {
  const [form, setForm] = useState<Form>(initial);
  const mutation = useSaveDeviceLimitOverrideMutation(appKey);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) => setForm((current) => ({ ...current, [key]: value }));

  function setExpiry(days: number) {
    if (!days) {
      patch("expiresAt", "");
      return;
    }
    const at = new Date();
    at.setDate(at.getDate() + days);
    patch("expiresAt", toLocalInputValue(at));
  }

  async function handleSave() {
    if (!form.user) {
      toast.error("请选择用户");
      return;
    }
    try {
      await mutation.mutateAsync({
        userId: form.user.id,
        payload: {
          exempt: form.exempt,
          maxDevices: form.maxDevices,
          classLimits: cleanClassLimits(form.classLimits),
          note: form.note.trim(),
          expiresAt: fromLocalInputValue(form.expiresAt) ?? null
        }
      });
      toast.success("已保存");
      onDone();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{editing ? "编辑例外" : "添加例外"}</DialogTitle>
      </DialogHeader>
      <div className="space-y-4">
        <div className="space-y-1.5">
          <Label className="text-xs">用户</Label>
          <UserPicker appKey={appKey} value={form.user} onChange={(user) => patch("user", user)} disabled={editing} />
        </div>

        <div className="grid grid-cols-2 gap-2">
          {[
            { exempt: true, title: "豁免", icon: ShieldCheck },
            { exempt: false, title: "另配上限", icon: SlidersHorizontal }
          ].map((item) => (
            <button
              key={item.title}
              type="button"
              onClick={() => patch("exempt", item.exempt)}
              className={cn(
                "flex items-center gap-2 rounded-xl bg-muted px-3 py-2.5 text-left text-xs font-medium transition-colors hover:bg-muted-foreground/10",
                form.exempt === item.exempt && "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30"
              )}
            >
              <item.icon className={cn("size-4 text-muted-foreground", form.exempt === item.exempt && "text-emerald-600 dark:text-emerald-400")} />
              {item.title}
            </button>
          ))}
        </div>

        {!form.exempt ? (
          <div className="space-y-2 rounded-xl border border-border p-3">
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs font-medium">同时登录设备</span>
              <LimitStepper value={form.maxDevices} onChange={(value) => patch("maxDevices", value)} />
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              {DEVICE_CLASSES.map((cls) => (
                <div key={cls} className="flex items-center justify-between gap-2 rounded-lg bg-muted px-2.5 py-1.5">
                  <span className="flex items-center gap-1.5 text-xs">
                    <ClassIcon cls={cls} className="size-3.5 text-muted-foreground" />
                    {CLASS_META[cls].label}
                  </span>
                  <LimitStepper
                    value={form.classLimits[cls] ?? 0}
                    onChange={(value) => patch("classLimits", cleanClassLimits({ ...form.classLimits, [cls]: value }))}
                  />
                </div>
              ))}
            </div>
          </div>
        ) : null}

        <div className="space-y-1.5">
          <div className="flex items-center justify-between">
            <Label className="text-xs">到期</Label>
            <div className="flex gap-1">
              {EXPIRY_PRESETS.map((preset) => (
                <Button key={preset.label} type="button" size="xs" variant="ghost" onClick={() => setExpiry(preset.days)}>
                  {preset.label}
                </Button>
              ))}
            </div>
          </div>
          <Input
            type="datetime-local"
            value={form.expiresAt}
            onChange={(event) => patch("expiresAt", event.target.value)}
            className="text-xs"
          />
        </div>

        <div className="space-y-1.5">
          <Label className="text-xs">备注</Label>
          <Input value={form.note} maxLength={200} onChange={(event) => patch("note", event.target.value)} className="text-xs" />
        </div>
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={onDone}>
          取消
        </Button>
        <Button onClick={handleSave} disabled={mutation.isPending || !form.user}>
          {mutation.isPending ? "保存中" : "保存"}
        </Button>
      </DialogFooter>
    </>
  );
}
