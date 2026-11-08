"use client";

import { useState } from "react";
import { Loader2, Send } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useNow } from "@/components/users/ban-form";
import { ApiError } from "@/lib/api-client";
import { useBulkNotifyAppUsersMutation } from "@/lib/app-user-hooks";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import {
  USER_NOTIFICATION_CATEGORIES,
  USER_NOTIFICATION_LEVELS,
  USER_NOTIFICATION_ROUTES,
  isHttpsUrl,
  type UserNotificationAction
} from "@/lib/user-notification-meta";

type ActionKind = "none" | "route" | "url";

const ACTION_KINDS: { value: ActionKind; label: string }[] = [
  { value: "none", label: "无" },
  { value: "route", label: "应用内页面" },
  { value: "url", label: "链接" }
];

const DAY = 24 * 60 * 60 * 1000;
const EXPIRE_PRESETS = [
  { label: "1 天", days: 1 },
  { label: "7 天", days: 7 },
  { label: "30 天", days: 30 }
];

function Field({ label, hint, error, children }: { label: string; hint?: string; error?: string | null; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {children}
      {error ? (
        <p className="text-[11px] text-destructive">{error}</p>
      ) : hint ? (
        <p className="text-[11px] text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  );
}

/**
 * 给选中的用户发站内信（docs/notification-center.md「管理端」）。
 *
 * 分类默认「公告活动」；动作二选一：应用内页面（路由键白名单）或 https 链接。
 * 前端校验与后端一致，提交仍以后端为准（动作不合法 42230、分类不存在 42231）。
 */
export function BulkNotifyDialog({
  open,
  onOpenChange,
  appKey,
  selectedIds,
  onDone
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string | null;
  selectedIds: number[];
  onDone: () => void;
}) {
  const mutation = useBulkNotifyAppUsersMutation(appKey);
  const now = useNow();
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [category, setCategory] = useState("announcement");
  const [level, setLevel] = useState("info");
  const [actionKind, setActionKind] = useState<ActionKind>("none");
  const [route, setRoute] = useState<string>(USER_NOTIFICATION_ROUTES[0].value);
  const [url, setUrl] = useState("");
  const [actionLabel, setActionLabel] = useState("");
  const [image, setImage] = useState("");
  const [expiresAt, setExpiresAt] = useState("");

  const urlError = actionKind === "url" && url.trim() && !isHttpsUrl(url.trim()) ? "必须是 https 地址" : null;
  const imageError = image.trim() && !isHttpsUrl(image.trim()) ? "必须是 https 地址" : null;
  const expiresIso = fromLocalInputValue(expiresAt);
  const expiresError = expiresAt && (!expiresIso || new Date(expiresIso).getTime() <= now) ? "必须晚于现在" : null;
  const labelError = [...actionLabel.trim()].length > 16 ? "不能超过 16 个字" : null;

  const canSubmit =
    title.trim().length > 0 &&
    content.trim().length > 0 &&
    !urlError &&
    !imageError &&
    !expiresError &&
    !labelError &&
    (actionKind !== "url" || url.trim().length > 0);

  function reset() {
    setTitle("");
    setContent("");
    setCategory("announcement");
    setLevel("info");
    setActionKind("none");
    setUrl("");
    setActionLabel("");
    setImage("");
    setExpiresAt("");
  }

  function buildAction(): UserNotificationAction | undefined {
    const label = actionLabel.trim() || undefined;
    if (actionKind === "route") return { type: "route", target: route, label };
    if (actionKind === "url") return { type: "url", target: url.trim(), label };
    return undefined;
  }

  async function submit() {
    try {
      await mutation.mutateAsync({
        userIds: selectedIds,
        type: "system",
        title: title.trim(),
        content: content.trim(),
        level,
        category,
        action: buildAction(),
        image: image.trim() || undefined,
        expiresAt: expiresIso
      });
      toast.success(`已向 ${selectedIds.length} 个用户发送站内信`);
      reset();
      onOpenChange(false);
      onDone();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "发送失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="text-sm">给 {selectedIds.length} 个用户发站内信</DialogTitle>
          <DialogDescription>发送到用户的消息中心，在线设备实时收到。</DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <Field label="标题">
            <Input
              value={title}
              maxLength={200}
              className="h-8 text-xs"
              placeholder="例如：版本更新"
              onChange={(event) => setTitle(event.target.value)}
            />
          </Field>
          <Field label="正文">
            <Textarea rows={4} value={content} placeholder="正文内容" onChange={(event) => setContent(event.target.value)} />
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field label="分类">
              <Select value={category} onValueChange={setCategory}>
                <SelectTrigger size="sm" className="h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {USER_NOTIFICATION_CATEGORIES.map((item) => (
                    <SelectItem key={item.value} value={item.value} className="text-xs">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="级别">
              <Select value={level} onValueChange={setLevel}>
                <SelectTrigger size="sm" className="h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {USER_NOTIFICATION_LEVELS.map((item) => (
                    <SelectItem key={item.value} value={item.value} className="text-xs">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label="跳转">
              <Select value={actionKind} onValueChange={(value) => setActionKind(value as ActionKind)}>
                <SelectTrigger size="sm" className="h-8 w-full text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ACTION_KINDS.map((item) => (
                    <SelectItem key={item.value} value={item.value} className="text-xs">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            {actionKind === "route" ? (
              <Field label="页面">
                <Select value={route} onValueChange={setRoute}>
                  <SelectTrigger size="sm" className="h-8 w-full text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {USER_NOTIFICATION_ROUTES.map((item) => (
                      <SelectItem key={item.value} value={item.value} className="text-xs">
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            ) : null}
            {actionKind !== "none" ? (
              <Field label="按钮文案" hint={actionKind === "route" ? "可不填" : undefined} error={labelError}>
                <Input
                  value={actionLabel}
                  className="h-8 text-xs"
                  placeholder="例如：立即查看"
                  onChange={(event) => setActionLabel(event.target.value)}
                />
              </Field>
            ) : null}
          </div>
          {actionKind === "url" ? (
            <Field label="链接" error={urlError}>
              <Input
                value={url}
                className="h-8 font-mono text-xs"
                placeholder="https://"
                onChange={(event) => setUrl(event.target.value)}
              />
            </Field>
          ) : null}

          <Field label="配图地址" hint="可不填" error={imageError}>
            <Input
              value={image}
              className="h-8 font-mono text-xs"
              placeholder="https://"
              onChange={(event) => setImage(event.target.value)}
            />
          </Field>

          <Field label="过期时间" hint="过期后不再出现在用户的消息列表中，不填即长期有效" error={expiresError}>
            <div className="flex flex-wrap items-center gap-1.5">
              <Input
                type="datetime-local"
                value={expiresAt}
                min={toLocalInputValue(now)}
                className="h-8 w-52 font-mono text-xs tabular-nums"
                onChange={(event) => setExpiresAt(event.target.value)}
              />
              {EXPIRE_PRESETS.map((preset) => (
                <button
                  key={preset.days}
                  type="button"
                  onClick={() => setExpiresAt(toLocalInputValue(now + preset.days * DAY))}
                  className="rounded-md border border-border bg-muted px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
                >
                  {preset.label}
                </button>
              ))}
              {expiresAt ? (
                <button
                  type="button"
                  onClick={() => setExpiresAt("")}
                  className="px-1 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
                >
                  清除
                </button>
              ) : null}
            </div>
          </Field>
        </div>

        <DialogFooter>
          <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={!canSubmit || mutation.isPending} onClick={submit}>
            {mutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <Send className="size-3" />}
            发送
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
