"use client";

import { useMemo, useState } from "react";
import { FileUp, KeyRound, Loader2, Repeat, Save, Sparkles, Ticket, TicketCheck, User } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, ModeCard, NumberField } from "@/components/apps/app-config-primitives";
import {
  formFromBatch,
  LIMITS,
  parseImportCodes,
  sampleCode,
  seedForm,
  toGeneratePayload,
  toUpdatePayload,
  validateForm,
  codeCapacity,
  type BatchForm,
  type EditorMode
} from "@/components/apps/card-key/card-key-batch-form";
import { CardKeyRewardEditor } from "@/components/apps/card-key/card-key-reward-editor";
import {
  AUDIENCE_LABEL,
  CHARSET_META,
  VALIDITY_OPTIONS,
  cardKindHint,
  formatCount
} from "@/components/apps/card-key/card-key-shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { RadioGroup } from "@/components/ui/radio-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { CardKeyAudience, CardKeyBatch, CardKeyCharset, CardKeyKind, CardKeyValidityMode } from "@/lib/api/card-key";
import { useCardKeyCatalogQuery, useGenerateCardKeysMutation, useUpdateCardKeyBatchMutation } from "@/lib/card-key-hooks";
import { cn } from "@/lib/utils";

export type BatchEditorTarget =
  | { mode: "new" }
  | { mode: "template"; batch: CardKeyBatch }
  | { mode: "edit"; batch: CardKeyBatch };

/**
 * 批次编辑抽屉：新建（生成或导入）、以已有批次为模板新建、修改。
 *
 * 修改时只开放「之后才生效」的那些项：名称、备注、渠道、面值、权益、领取规则、生效时间、
 * 统一到期时间与自助解绑次数。卡面格式、形态、用法、有效期模式与可绑设备数已经落在每一张卡上，
 * 灰显而不是隐藏 —— 运营要能看到这批卡当初是怎么配的。
 *
 * 草稿用「作用域绑定」而不是 `useEffect` 同步：抽屉关闭即清空，与抽奖、积分商城的编辑器同一条约束。
 */
export function CardKeyBatchEditor({
  appKey,
  target,
  onClose,
  onCreated
}: {
  appKey: string;
  target: BatchEditorTarget | null;
  onClose: () => void;
  onCreated?: (batch: CardKeyBatch) => void;
}) {
  const catalogQuery = useCardKeyCatalogQuery(appKey);
  const generateMutation = useGenerateCardKeysMutation(appKey);
  const updateMutation = useUpdateCardKeyBatchMutation(appKey);
  const catalog = catalogQuery.data;

  const [draft, setDraft] = useState<BatchForm | null>(null);
  const [source, setSource] = useState<"generate" | "import">("generate");
  const editing = target?.mode === "edit";
  const initial = useMemo(() => {
    if (!target || target.mode === "new") return seedForm();
    return formFromBatch(target.batch, target.mode === "edit" ? "edit" : "template");
  }, [target]);
  const form = draft ?? initial;
  const mode: EditorMode = editing ? "edit" : source === "import" ? "import" : "create";
  const update = (next: Partial<BatchForm>) => setDraft({ ...form, ...next });
  const patch = <K extends keyof BatchForm>(key: K, value: BatchForm[K]) => update({ [key]: value } as Partial<BatchForm>);

  const parsed = useMemo(() => parseImportCodes(form.importText), [form.importText]);
  const sample = useMemo(() => sampleCode(form), [form]);
  const capacity = codeCapacity(form.charset, Number(form.segments) || 4, Number(form.segmentLength) || 4);
  const login = form.kind === "login";

  const close = () => {
    setDraft(null);
    setSource("generate");
    onClose();
  };

  const submit = async () => {
    const issues = validateForm(form, mode, catalog, initial);
    if (issues.length > 0) {
      toast.error(issues[0]);
      return;
    }
    try {
      if (target?.mode === "edit") {
        await updateMutation.mutateAsync({ batchId: target.batch.id, payload: toUpdatePayload(form, catalog, initial) });
        toast.success("批次已保存");
      } else {
        const batch = await generateMutation.mutateAsync(toGeneratePayload(form, mode, catalog));
        toast.success(mode === "import" ? `已导入 ${batch.total} 张卡密` : `已生成 ${batch.total} 张卡密`);
        onCreated?.(batch);
      }
      close();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const pending = generateMutation.isPending || updateMutation.isPending;
  const title = editing ? "编辑批次" : target?.mode === "template" ? "以模板新建" : "新建卡密";

  return (
    <Sheet open={Boolean(target)} onOpenChange={(open) => !open && close()}>
      <SheetContent side="right" className="flex w-[96vw] max-w-2xl flex-col gap-0 p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>
            {editing ? target.batch.name : target?.mode === "template" ? `模板：${target.batch.name}` : "生成或导入一批卡密"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-6 px-6 py-5">
            {editing ? (
              <p className="rounded-xl bg-sky-500/10 px-3 py-2.5 text-xs leading-relaxed text-sky-800 dark:text-sky-300">
                权益与领取规则的修改只影响之后的核销，已经发出去的权益不受影响。卡面格式、类型、用法、
                有效期模式与可绑设备数生成后不能修改。
              </p>
            ) : null}

            <FieldGroup label="基本信息">
              <div className="space-y-1.5">
                <span className="text-[11px] font-medium">批次名称</span>
                <Input
                  value={form.name}
                  maxLength={LIMITS.name}
                  onChange={(event) => patch("name", event.target.value)}
                  placeholder="例如：双十一会员月卡"
                />
              </div>
              <RadioGroup
                value={form.kind}
                onValueChange={(value) => patch("kind", value as CardKeyKind)}
                className="grid gap-2 sm:grid-cols-2"
                disabled={editing}
              >
                <ModeCard
                  value="redeem"
                  active={form.kind === "redeem"}
                  title="兑换卡"
                  description={cardKindHint("redeem")}
                  icon={<Ticket className="size-4" />}
                  disabled={editing}
                />
                <ModeCard
                  value="login"
                  active={form.kind === "login"}
                  title="授权卡"
                  description={cardKindHint("login")}
                  icon={<KeyRound className="size-4" />}
                  disabled={editing}
                />
              </RadioGroup>
            </FieldGroup>

            {!editing ? (
              <FieldGroup label="卡面">
                <ToggleGroup
                  type="single"
                  variant="outline"
                  size="sm"
                  value={source}
                  onValueChange={(value) => value && setSource(value as "generate" | "import")}
                >
                  <ToggleGroupItem value="generate" className="h-8 px-3 text-xs">
                    <Sparkles className="size-3.5" /> 随机生成
                  </ToggleGroupItem>
                  <ToggleGroupItem value="import" className="h-8 px-3 text-xs">
                    <FileUp className="size-3.5" /> 导入卡面
                  </ToggleGroupItem>
                </ToggleGroup>

                {source === "generate" ? (
                  <div className="space-y-4">
                    <div className="grid gap-4 sm:grid-cols-2">
                      <NumberField
                        label="生成数量"
                        value={form.count}
                        min={1}
                        max={catalog?.maxCount ?? LIMITS.maxCount}
                        unit="张"
                        onChange={(value) => patch("count", value)}
                      />
                      <div className="space-y-1.5">
                        <span className="text-[11px] font-medium">前缀</span>
                        <Input
                          value={form.codePrefix}
                          maxLength={LIMITS.prefix}
                          onChange={(event) => patch("codePrefix", event.target.value.toUpperCase())}
                          placeholder="可选，例如 VIP"
                          className="font-mono"
                        />
                      </div>
                      <NumberField
                        label="段数"
                        value={form.segments}
                        min={1}
                        max={8}
                        unit="段"
                        onChange={(value) => patch("segments", value)}
                      />
                      <NumberField
                        label="每段位数"
                        value={form.segmentLength}
                        min={3}
                        max={12}
                        unit="位"
                        onChange={(value) => patch("segmentLength", value)}
                      />
                    </div>
                    <div className="space-y-1.5">
                      <span className="text-[11px] font-medium">字符集</span>
                      <Select value={form.charset} onValueChange={(value) => patch("charset", value as CardKeyCharset)}>
                        <SelectTrigger className="h-9 w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent position="popper">
                          {(catalog?.charsets ?? (Object.keys(CHARSET_META) as CardKeyCharset[])).map((charset) => (
                            <SelectItem key={charset} value={charset}>
                              {CHARSET_META[charset]?.label ?? charset}
                              <span className="ml-1.5 text-[11px] text-muted-foreground">{CHARSET_META[charset]?.hint}</span>
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <CodePreview
                      sample={sample}
                      capacity={capacity}
                      count={Number(form.count) || 0}
                    />
                  </div>
                ) : (
                  <div className="space-y-2">
                    <Textarea
                      value={form.importText}
                      rows={8}
                      onChange={(event) => patch("importText", event.target.value)}
                      placeholder={"一行一张卡面，也可以用逗号分隔\nOLD-0001\nOLD-0002"}
                      className="font-mono text-xs"
                    />
                    <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
                      <span className="tabular-nums">
                        可导入 <span className="font-medium text-foreground">{formatCount(parsed.codes.length)}</span> 张
                      </span>
                      {parsed.duplicates.length ? (
                        <span className="tabular-nums text-amber-700 dark:text-amber-400">
                          重复 {parsed.duplicates.length} 张（已去重）
                        </span>
                      ) : null}
                      {parsed.invalid.length ? (
                        <span className="tabular-nums text-red-600 dark:text-red-400">
                          无效 {parsed.invalid.length} 张，例如 {parsed.invalid[0]}
                        </span>
                      ) : null}
                      <span>大小写与分隔符会按兑换时的规则统一，库里已有的卡面会被列出并拒绝</span>
                    </div>
                  </div>
                )}
              </FieldGroup>
            ) : null}

            {!login ? (
              <FieldGroup label="用法">
                <RadioGroup
                  value={form.usage}
                  onValueChange={(value) => patch("usage", value as BatchForm["usage"])}
                  className="grid gap-2 sm:grid-cols-2"
                  disabled={editing}
                >
                  <ModeCard
                    value="single"
                    active={form.usage === "single"}
                    title="一卡一用"
                    description="一张卡只能被一个人兑换一次，适合激活码与礼品卡"
                    icon={<TicketCheck className="size-4" />}
                    disabled={editing}
                  />
                  <ModeCard
                    value="multi"
                    active={form.usage === "multi"}
                    title="活动码"
                    description="一个码可被多人各用一次，适合直播口令与群发福利"
                    icon={<Repeat className="size-4" />}
                    disabled={editing}
                  />
                </RadioGroup>
                {form.usage === "multi" ? (
                  <NumberField
                    label="每个码可用人次"
                    value={form.maxUses}
                    min={2}
                    max={LIMITS.maxUses}
                    unit="人次"
                    disabled={editing}
                    onChange={(value) => patch("maxUses", value)}
                  />
                ) : null}
              </FieldGroup>
            ) : null}

            <FieldGroup label={login ? "授权期" : "有效期"}>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">模式</span>
                  <Select
                    value={form.validityMode}
                    onValueChange={(value) => patch("validityMode", value as CardKeyValidityMode)}
                    disabled={editing}
                  >
                    <SelectTrigger className="h-9 w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      {VALIDITY_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                          <span className="ml-1.5 text-[11px] text-muted-foreground">{option.hint}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                {form.validityMode === "days_from_first_use" ? (
                  <NumberField
                    label="有效天数"
                    value={form.validityDays}
                    min={1}
                    max={LIMITS.validityDays}
                    unit="天"
                    disabled={editing}
                    onChange={(value) => patch("validityDays", value)}
                  />
                ) : null}
                {form.validityMode === "fixed_until" ? (
                  <div className="space-y-1.5">
                    <span className="text-[11px] font-medium">到期时间</span>
                    <Input
                      type="datetime-local"
                      value={form.validUntil}
                      onChange={(event) => patch("validUntil", event.target.value)}
                    />
                    {editing ? (
                      <p className="text-[11px] text-muted-foreground">修改后同步到这一批里还没用过的卡</p>
                    ) : null}
                  </div>
                ) : null}
              </div>
            </FieldGroup>

            <FieldGroup label="领取规则">
              <div className="grid gap-4 sm:grid-cols-2">
                {login ? (
                  <>
                    <NumberField
                      label="可绑定设备数"
                      value={form.maxDevices}
                      min={1}
                      max={LIMITS.maxDevices}
                      unit="台"
                      disabled={editing}
                      onChange={(value) => patch("maxDevices", value)}
                    />
                    <NumberField
                      label="每 30 天自助解绑"
                      value={form.selfUnbindLimit}
                      min={0}
                      max={LIMITS.selfUnbindLimit}
                      unit="次，0 为不允许"
                      onChange={(value) => patch("selfUnbindLimit", value)}
                    />
                  </>
                ) : (
                  <>
                    <NumberField
                      label="每人限兑"
                      value={form.perUserLimit}
                      min={0}
                      max={LIMITS.perUserLimit}
                      unit="张，0 为不限"
                      onChange={(value) => patch("perUserLimit", value)}
                    />
                    <div className="space-y-1.5">
                      <span className="text-[11px] font-medium">领取对象</span>
                      <Select value={form.audience} onValueChange={(value) => patch("audience", value as CardKeyAudience)}>
                        <SelectTrigger className="h-9 w-full">
                          <User className="size-3.5 text-muted-foreground" />
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent position="popper">
                          {(catalog?.audiences ?? (Object.keys(AUDIENCE_LABEL) as CardKeyAudience[])).map((audience) => (
                            <SelectItem key={audience} value={audience}>
                              {AUDIENCE_LABEL[audience] ?? audience}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                  </>
                )}
                <div className="space-y-1.5 sm:col-span-2">
                  <span className="text-[11px] font-medium">生效时间</span>
                  <Input
                    type="datetime-local"
                    value={form.startsAt}
                    onChange={(event) => patch("startsAt", event.target.value)}
                  />
                  <p className="text-[11px] text-muted-foreground">留空即生成后立即可用；之前兑换会提示具体的生效时刻</p>
                </div>
              </div>
            </FieldGroup>

            <FieldGroup label={login ? "随卡权益" : "权益"} hint={login ? "首次激活时发放" : undefined}>
              <CardKeyRewardEditor
                catalog={catalog}
                kind={form.kind}
                rows={form.rewards}
                onChange={(rewards) => patch("rewards", rewards)}
              />
            </FieldGroup>

            <FieldGroup label="渠道与备注">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">渠道</span>
                  <Input
                    value={form.channel}
                    maxLength={LIMITS.channel}
                    onChange={(event) => patch("channel", event.target.value)}
                    placeholder="例如：淘宝旗舰店"
                  />
                </div>
                <div className="space-y-1.5">
                  <span className="text-[11px] font-medium">单张面值</span>
                  <div className="relative">
                    <Input
                      type="number"
                      inputMode="decimal"
                      min={0}
                      step="0.01"
                      value={form.faceValue}
                      onChange={(event) => patch("faceValue", event.target.value)}
                      placeholder="0.00"
                      className="pr-10 tabular-nums"
                    />
                    <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-[11px] text-muted-foreground">
                      元
                    </span>
                  </div>
                </div>
                <div className="space-y-1.5 sm:col-span-2">
                  <span className="text-[11px] font-medium">备注</span>
                  <Textarea
                    value={form.remark}
                    rows={2}
                    onChange={(event) => patch("remark", event.target.value)}
                    placeholder="只在控制台显示"
                  />
                </div>
              </div>
            </FieldGroup>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-between gap-2 border-t px-6 py-3">
          <span className="text-[11px] text-muted-foreground">
            {editing
              ? ""
              : mode === "import"
                ? `将导入 ${formatCount(parsed.codes.length)} 张`
                : `将生成 ${formatCount(Number(form.count) || 0)} 张`}
          </span>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={close}>
              取消
            </Button>
            <Button size="sm" onClick={submit} disabled={pending || catalogQuery.isLoading}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              {editing ? "保存" : mode === "import" ? "导入" : "生成"}
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function CodePreview({ sample, capacity, count }: { sample: string; capacity: number; count: number }) {
  const enough = count <= capacity / 4;
  return (
    <div
      className={cn(
        "rounded-xl border px-4 py-3",
        enough ? "border-border bg-muted/40" : "border-amber-300 bg-amber-50 dark:border-amber-800/70 dark:bg-amber-950/40"
      )}
    >
      <p className="text-[11px] text-muted-foreground">卡面样例</p>
      <p className="mt-1 break-all font-mono text-base font-semibold tracking-wider">{sample}</p>
      <p className={cn("mt-1 text-[11px]", enough ? "text-muted-foreground" : "text-amber-700 dark:text-amber-400")}>
        {enough
          ? `可表达 ${capacity >= 1e12 ? "超过一万亿" : formatCount(capacity)} 种组合`
          : "当前格式能表达的组合太少，装不下这个数量；请增加段数或每段位数"}
      </p>
    </div>
  );
}
