"use client";

import { useState } from "react";
import { History, Loader2, RotateCcw, Save, Shuffle } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { formatVipDate, vipSourceLabel } from "@/components/apps/vip/vip-shared";
import {
  useAdminVipConversionPolicyQuery,
  useAdminVipConversionsQuery,
  useSaveAdminVipConversionPolicyMutation
} from "@/lib/vip-hooks";
import type { VipConversionModeSpec, VipConversionPolicy, VipConversionPolicyPayload } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

/** 折算单价的四个输入（单位与说明以文档为准：docs/vip-conversion.md） */
const RATE_FIELDS: Array<{ key: keyof Pick<VipConversionPolicyPayload, "perVipDay" | "perFeatureDay" | "perFeatureHour" | "perLifetime">; label: string; unit: string; hint: string }> = [
  { key: "perVipDay", label: "会员", unit: "积分 / 天", hint: "限时会员、会员天数、限时套餐，按秒计、向上取整" },
  { key: "perFeatureDay", label: "功能解锁", unit: "积分 / 天", hint: "限时功能解锁与免广告时长" },
  { key: "perFeatureHour", label: "功能时长", unit: "积分 / 小时", hint: "按实际使用计时的功能余额（如在线听歌）" },
  { key: "perLifetime", label: "永久权益", unit: "积分 / 次", hint: "已包含的永久套餐、已永久解锁的功能再被发放一次" }
];

function toPayload(policy: VipConversionPolicy): VipConversionPolicyPayload {
  return {
    owned: policy.owned,
    upgrade: policy.upgrade,
    perVipDay: policy.perVipDay,
    perFeatureDay: policy.perFeatureDay,
    perFeatureHour: policy.perFeatureHour,
    perLifetime: policy.perLifetime,
    restoreOnRevoke: policy.restoreOnRevoke
  };
}

/**
 * 会员权益折算：一份策略管所有入口。
 *
 * 两件事：永久会员收到已包含的会员 / 功能时怎么办（卡密、邀请、排行榜、管理员与远程函数发放），
 * 开通永久时原有的限时会员、限时功能解锁、功能时长与免广告怎么办（所有开通永久的入口）。
 * 抽奖与激励广告另有活动 / 场景级的已拥有处理方式，判据与这里相同。
 */
export function VipConversionPanel({ appKey }: { appKey: string }) {
  const policyQuery = useAdminVipConversionPolicyQuery(appKey);
  const bundle = policyQuery.data;

  return (
    <div className="space-y-5">
      {policyQuery.isLoading || !bundle ? (
        <SectionCard icon={<Shuffle className="size-4" />} title="折算策略">
          {policyQuery.isError ? (
            <p className="text-xs text-destructive">{policyQuery.error instanceof Error ? policyQuery.error.message : "加载失败"}</p>
          ) : (
            <Skeleton className="h-64 w-full rounded-xl" />
          )}
        </SectionCard>
      ) : (
        <PolicyForm
          key={bundle.policy.updatedAt ?? "default"}
          appKey={appKey}
          policy={bundle.policy}
          defaults={bundle.defaults}
          ownedModes={bundle.ownedModes}
          upgradeModes={bundle.upgradeModes}
        />
      )}
      <ConversionList appKey={appKey} />
    </div>
  );
}

function PolicyForm({
  appKey,
  policy,
  defaults,
  ownedModes,
  upgradeModes
}: {
  appKey: string;
  policy: VipConversionPolicy;
  defaults: VipConversionPolicy;
  ownedModes: VipConversionModeSpec[];
  upgradeModes: VipConversionModeSpec[];
}) {
  const save = useSaveAdminVipConversionPolicyMutation(appKey);
  const [draft, setDraft] = useState<VipConversionPolicyPayload>(() => toPayload(policy));
  const dirty = JSON.stringify(draft) !== JSON.stringify(toPayload(policy));
  const converting = draft.owned === "convert" || draft.upgrade === "convert";

  async function submit() {
    try {
      await save.mutateAsync(draft);
      toast.success("折算策略已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  return (
    <SectionCard
      icon={<Shuffle className="size-4" />}
      title="折算策略"
      description={
        policy.configured
          ? `最近由 ${policy.updatedBy || "管理员"} 于 ${formatVipDate(policy.updatedAt)} 修改`
          : "尚未保存过，当前按默认策略执行"
      }
      footer={
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={() => setDraft(toPayload(defaults))} disabled={save.isPending}>
            <RotateCcw />
            恢复默认
          </Button>
          <Button size="sm" onClick={() => void submit()} disabled={!dirty || save.isPending}>
            {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            保存
          </Button>
        </div>
      }
    >
      <div className="grid gap-6 lg:grid-cols-2">
        <ModeGroup
          title="已拥有的权益被发放时"
          description="永久会员收到已包含的会员天数、套餐或功能解锁；已永久解锁的功能再被发放"
          modes={ownedModes}
          value={draft.owned}
          onChange={(value) => setDraft((prev) => ({ ...prev, owned: value as VipConversionPolicy["owned"] }))}
        />
        <ModeGroup
          title="开通永久会员时"
          description="原有的限时会员、限时功能解锁、功能时长与免广告时长被永久会员覆盖"
          modes={upgradeModes}
          value={draft.upgrade}
          onChange={(value) => setDraft((prev) => ({ ...prev, upgrade: value as VipConversionPolicy["upgrade"] }))}
        />
      </div>

      <div className={cn("mt-6 flex flex-col gap-3", !converting && "opacity-60")}>
        <div className="text-xs font-medium">折算单价</div>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {RATE_FIELDS.map((field) => (
            <div key={field.key} className="flex flex-col gap-1.5 rounded-xl border border-border bg-muted/30 p-3">
              <Label htmlFor={`rate-${field.key}`} className="text-xs">
                {field.label}
              </Label>
              <div className="flex items-center gap-2">
                <Input
                  id={`rate-${field.key}`}
                  inputMode="numeric"
                  value={String(draft[field.key])}
                  className="w-28 tabular-nums"
                  onChange={(event) => {
                    const next = Number.parseInt(event.target.value.replace(/\D/g, "") || "0", 10);
                    setDraft((prev) => ({ ...prev, [field.key]: Math.min(1_000_000, next) }));
                  }}
                />
                <span className="text-xs text-muted-foreground">{field.unit}</span>
              </div>
              <p className="text-xs text-muted-foreground">{field.hint}</p>
            </div>
          ))}
        </div>
      </div>

      <div className="mt-6 flex items-start justify-between gap-4 rounded-xl border border-border p-3">
        <div className="flex flex-col gap-0.5">
          <Label htmlFor="restore-on-revoke" className="text-xs">
            取消永久会员时还回
          </Label>
          <p className="text-xs text-muted-foreground">
            管理员取消永久会员或永久套餐退款后，把开通时折掉的存量还回去，并收回折给的积分（余额不足时收到 0 为止）
          </p>
        </div>
        <Switch
          id="restore-on-revoke"
          checked={draft.restoreOnRevoke}
          onCheckedChange={(checked) => setDraft((prev) => ({ ...prev, restoreOnRevoke: checked }))}
        />
      </div>
    </SectionCard>
  );
}

function ModeGroup({
  title,
  description,
  modes,
  value,
  onChange
}: {
  title: string;
  description: string;
  modes: VipConversionModeSpec[];
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <div className="text-xs font-medium">{title}</div>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <RadioGroup value={value} onValueChange={onChange} className="gap-2">
        {modes.map((mode) => (
          <label
            key={mode.key}
            className={cn(
              "flex cursor-pointer items-start gap-3 rounded-xl border p-3 transition-colors",
              value === mode.key ? "border-primary/50 bg-primary/5" : "hover:bg-accent/40"
            )}
          >
            <RadioGroupItem value={mode.key} className="mt-0.5" />
            <span className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">{mode.label}</span>
              <span className="text-xs text-muted-foreground">{mode.hint}</span>
            </span>
          </label>
        ))}
      </RadioGroup>
    </div>
  );
}

/** 全应用的折算记录，新的在前。某个人的记录在用户详情的会员面板里。 */
function ConversionList({ appKey }: { appKey: string }) {
  const [page, setPage] = useState(1);
  const limit = 20;
  const query = useAdminVipConversionsQuery(appKey, { page, limit });
  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <SectionCard icon={<History className="size-4" />} title="折算记录" description={`共 ${total} 条`}>
      {query.isLoading ? (
        <Skeleton className="h-32 w-full rounded-xl" />
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          暂无折算记录
        </p>
      ) : (
        <>
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead>折掉的权益</TableHead>
                  <TableHead>来源</TableHead>
                  <TableHead className="text-right">积分</TableHead>
                  <TableHead>时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="text-xs tabular-nums">#{item.userId}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        <Badge variant={item.kind === "upgrade" ? "info" : "warning"} size="sm">
                          {item.kind === "upgrade" ? "开通永久" : "已拥有"}
                        </Badge>
                        {item.status === "restored" ? (
                          <Badge variant="secondary" size="sm">
                            已还回
                          </Badge>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-72 text-xs">
                      <div className="truncate" title={item.message}>
                        {item.items.map((entry) => entry.label).join("、")}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs">{vipSourceLabel(item.source)}</TableCell>
                    <TableCell className={cn("text-right text-xs tabular-nums", item.status === "restored" && "line-through text-muted-foreground")}>
                      +{item.points.toLocaleString("zh-CN")}
                    </TableCell>
                    <TableCell className="text-xs">{formatVipDate(item.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          {totalPages > 1 ? (
            <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
              <span>
                第 {page} / {totalPages} 页
              </span>
              <div className="flex gap-2">
                <Button variant="outline" size="xs" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                  上一页
                </Button>
                <Button variant="outline" size="xs" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
                  下一页
                </Button>
              </div>
            </div>
          ) : null}
        </>
      )}
    </SectionCard>
  );
}
