"use client";

import { useState } from "react";
import { Check, Copy, Loader2, Save, X } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import { normalizeCodeInput } from "@/components/apps/invite/invite-shared";
import {
  RewardListEditor,
  rewardDraftsFrom,
  rewardsFromDrafts,
  type RewardDraft
} from "@/components/apps/rewards/reward-list-editor";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
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
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { InviteAdminCode, InviteCode, InviteCodePayload } from "@/lib/api/invite";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { useInviteCatalogQuery, useSaveInviteCodeMutation } from "@/lib/invite-hooks";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

const MAX_TITLE = 32;
const MAX_NOTE = 128;

type Form = {
  mode: "single" | "batch";
  code: string;
  count: string;
  prefix: string;
  owner: PickedUser | null;
  title: string;
  note: string;
  customRewards: boolean;
  rewards: RewardDraft[];
  maxUses: string;
  startsAt: string;
  endsAt: string;
  active: boolean;
};

function seed(code: InviteAdminCode | null): Form {
  return {
    mode: "single",
    code: code?.code ?? "",
    count: "10",
    prefix: "",
    owner: code?.ownerUserId
      ? { id: code.ownerUserId, account: code.ownerAccount, nickname: code.ownerNickname }
      : null,
    title: code?.title ?? "",
    note: code?.note ?? "",
    customRewards: Boolean(code && code.rewards !== undefined && code.rewards !== null),
    rewards: rewardDraftsFrom(code?.rewards),
    maxUses: code?.maxUses ? String(code.maxUses) : "",
    startsAt: toLocalInputValue(code?.startsAt),
    endsAt: toLocalInputValue(code?.endsAt),
    active: code ? code.status === "active" : true
  };
}

/**
 * 新建 / 编辑活动码。
 *
 * 新建时指定一个码，或按应用的个人码格式批量生成（可另给前缀）。被邀请人奖励默认跟随应用配置，
 * 选「自定义」后按这里的列表发（可以为空，即这个码不发奖励）。推广人拿邀请人奖励，不选即纯渠道码。
 * 编辑时码本身不能改。草稿与积分商城编辑器同一个形状：弹窗关闭即清空。
 */
export function InviteCodeEditor({
  appKey,
  code,
  open,
  onOpenChange
}: {
  appKey: string;
  /** 为空即新建 */
  code: InviteAdminCode | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const catalogQuery = useInviteCatalogQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSaveInviteCodeMutation(appKey);

  const [draft, setDraft] = useState<{ id: number; value: Form } | null>(null);
  const [created, setCreated] = useState<InviteCode[] | null>(null);
  const [copied, setCopied] = useState(false);
  const scope = code?.id ?? 0;
  const form = draft?.id === scope ? draft.value : seed(code);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ id: scope, value: { ...form, [key]: value } });

  const catalog = catalogQuery.data?.rewards ?? [];
  const limits = catalogQuery.data?.limits;
  const maxRewards = limits?.maxRewards ?? 6;
  const minCustom = limits?.minCustomCodeLength ?? 4;
  const maxCustom = limits?.maxCustomCodeLength ?? 16;
  const maxBatch = limits?.maxBatchCount ?? 500;
  const maxPrefix = limits?.maxPrefixLength ?? 6;
  const plans = (plansQuery.data ?? []).filter((plan) => plan.kind !== "trial");
  const features = featuresQuery.data ?? [];

  const close = () => {
    setDraft(null);
    onOpenChange(false);
  };

  const submit = async () => {
    const title = form.title.trim();
    if (!title) {
      toast.error("请填写活动名称");
      return;
    }
    const payload: InviteCodePayload = {
      ownerUserId: form.owner?.id ?? null,
      title,
      note: form.note.trim(),
      rewards: form.customRewards ? rewardsFromDrafts(form.rewards, catalog) : null,
      maxUses: Number.parseInt(form.maxUses, 10) > 0 ? Number.parseInt(form.maxUses, 10) : null,
      startsAt: fromLocalInputValue(form.startsAt) ?? null,
      endsAt: fromLocalInputValue(form.endsAt) ?? null,
      status: form.active ? "active" : "disabled"
    };
    if (!code) {
      if (form.mode === "single") {
        const value = normalizeCodeInput(form.code);
        if (value.length < minCustom || value.length > maxCustom) {
          toast.error(`邀请码需要 ${minCustom}–${maxCustom} 位字母与数字`);
          return;
        }
        payload.code = value;
      } else {
        const count = Number.parseInt(form.count, 10);
        if (!(count >= 1 && count <= maxBatch)) {
          toast.error(`生成数量需要在 1–${maxBatch} 之间`);
          return;
        }
        payload.count = count;
        payload.prefix = normalizeCodeInput(form.prefix);
      }
    }
    try {
      const items = await saveMutation.mutateAsync({ codeId: code?.id, payload });
      if (code) {
        toast.success("邀请码已保存");
      } else {
        toast.success(`已创建 ${items.length} 个邀请码`);
        if (items.length > 1) {
          setCopied(false);
          setCreated(items);
        }
      }
      close();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const copyCreated = async () => {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created.map((item) => item.code).join("\n"));
      setCopied(true);
    } catch {
      toast.error("复制失败");
    }
  };

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next) setDraft(null);
          onOpenChange(next);
        }}
      >
        <DialogContent className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-xl">
          <DialogHeader className="shrink-0 border-b px-6 py-4">
            <DialogTitle>{code ? "编辑活动码" : "新建活动码"}</DialogTitle>
            <DialogDescription className={code ? "font-mono" : undefined}>{code ? code.code : "邀请码"}</DialogDescription>
          </DialogHeader>

          <div className="min-h-0 flex-1 overflow-y-auto">
            <div className="space-y-5 px-6 py-5">
              {code ? null : (
                <FieldGroup label="邀请码">
                  <ToggleGroup
                    type="single"
                    size="sm"
                    variant="outline"
                    value={form.mode}
                    onValueChange={(value) => value && patch("mode", value as Form["mode"])}
                  >
                    <ToggleGroupItem value="single" className="h-8 px-3 text-xs">
                      指定
                    </ToggleGroupItem>
                    <ToggleGroupItem value="batch" className="h-8 px-3 text-xs">
                      批量生成
                    </ToggleGroupItem>
                  </ToggleGroup>
                  {form.mode === "single" ? (
                    <div className="space-y-1.5">
                      <Input
                        value={form.code}
                        maxLength={maxCustom}
                        placeholder="SUMMER2026"
                        className="font-mono text-xs"
                        onChange={(event) => patch("code", normalizeCodeInput(event.target.value))}
                      />
                      <p className="text-[10px] text-muted-foreground">
                        {minCustom}–{maxCustom} 位字母与数字
                      </p>
                    </div>
                  ) : (
                    <div className="grid gap-4 sm:grid-cols-2">
                      <NumberField
                        label="数量"
                        value={form.count}
                        onChange={(value) => patch("count", value)}
                        min={1}
                        max={maxBatch}
                        unit={`个，最多 ${maxBatch}`}
                      />
                      <div className="space-y-1.5">
                        <div className="flex items-baseline justify-between gap-2">
                          <Label className="text-[11px] font-medium">前缀</Label>
                          <span className="text-[10px] text-muted-foreground">最多 {maxPrefix} 位</span>
                        </div>
                        <Input
                          value={form.prefix}
                          maxLength={maxPrefix}
                          placeholder="不设置"
                          className="font-mono text-xs"
                          onChange={(event) => patch("prefix", normalizeCodeInput(event.target.value))}
                        />
                      </div>
                    </div>
                  )}
                </FieldGroup>
              )}

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <div className="flex items-baseline justify-between gap-2">
                    <Label className="text-[11px] font-medium">活动名称</Label>
                    <span className="text-[10px] text-muted-foreground tabular-nums">
                      {form.title.length}/{MAX_TITLE}
                    </span>
                  </div>
                  <Input
                    value={form.title}
                    maxLength={MAX_TITLE}
                    placeholder="例如：暑期推广"
                    className="text-xs"
                    onChange={(event) => patch("title", event.target.value)}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-[11px] font-medium">推广人</Label>
                  <div className="flex items-center gap-1.5">
                    <div className="min-w-0 flex-1">
                      <UserPicker appKey={appKey} value={form.owner} onChange={(user) => patch("owner", user)} />
                    </div>
                    {form.owner ? (
                      <Button
                        size="icon"
                        variant="ghost"
                        className="size-9 shrink-0"
                        aria-label="清除推广人"
                        onClick={() => patch("owner", null)}
                      >
                        <X className="size-3.5" />
                      </Button>
                    ) : null}
                  </div>
                </div>
              </div>

              <div className="space-y-1.5">
                <div className="flex items-baseline justify-between gap-2">
                  <Label className="text-[11px] font-medium">备注</Label>
                  <span className="text-[10px] text-muted-foreground tabular-nums">
                    {form.note.length}/{MAX_NOTE}
                  </span>
                </div>
                <Textarea
                  value={form.note}
                  rows={2}
                  maxLength={MAX_NOTE}
                  onChange={(event) => patch("note", event.target.value)}
                />
              </div>

              <FieldGroup
                label="被邀请人奖励"
                hint={form.customRewards ? `${form.rewards.length}/${maxRewards}` : undefined}
              >
                <ToggleGroup
                  type="single"
                  size="sm"
                  variant="outline"
                  value={form.customRewards ? "custom" : "default"}
                  onValueChange={(value) => value && patch("customRewards", value === "custom")}
                >
                  <ToggleGroupItem value="default" className="h-8 px-3 text-xs">
                    跟随默认
                  </ToggleGroupItem>
                  <ToggleGroupItem value="custom" className="h-8 px-3 text-xs">
                    自定义
                  </ToggleGroupItem>
                </ToggleGroup>
                {form.customRewards ? (
                  <RewardListEditor
                    catalog={catalog}
                    maxRewards={maxRewards}
                    rewards={form.rewards}
                    plans={plans}
                    features={features}
                    emptyText="不发奖励"
                    addLabel="添加奖励"
                    onChange={(rewards) => patch("rewards", rewards)}
                  />
                ) : null}
              </FieldGroup>

              <FieldGroup label="使用限制">
                <div className="grid gap-4 sm:grid-cols-3">
                  <NumberField
                    label="可使用次数"
                    value={form.maxUses}
                    onChange={(value) => patch("maxUses", value)}
                    min={0}
                    placeholder="不限"
                    unit="次"
                  />
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">开始</Label>
                    <Input
                      type="datetime-local"
                      value={form.startsAt}
                      className="text-xs"
                      onChange={(event) => patch("startsAt", event.target.value)}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">结束</Label>
                    <Input
                      type="datetime-local"
                      value={form.endsAt}
                      className="text-xs"
                      onChange={(event) => patch("endsAt", event.target.value)}
                    />
                  </div>
                </div>
                <SwitchRow label="启用" checked={form.active} onChange={(value) => patch("active", value)} />
              </FieldGroup>
            </div>
          </div>

          <DialogFooter className="shrink-0 border-t px-6 py-3">
            <Button variant="ghost" size="sm" onClick={close}>
              取消
            </Button>
            <Button size="sm" onClick={() => void submit()} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              {code ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(created)} onOpenChange={(next) => !next && setCreated(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>已创建 {created?.length ?? 0} 个邀请码</DialogTitle>
            <DialogDescription>{created?.[0]?.title}</DialogDescription>
          </DialogHeader>
          <Textarea
            readOnly
            rows={10}
            value={(created ?? []).map((item) => item.code).join("\n")}
            className="font-mono text-xs"
          />
          <DialogFooter>
            <Button variant="ghost" size="sm" onClick={() => setCreated(null)}>
              关闭
            </Button>
            <Button size="sm" onClick={() => void copyCreated()}>
              {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
              {copied ? "已复制" : "复制全部"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
