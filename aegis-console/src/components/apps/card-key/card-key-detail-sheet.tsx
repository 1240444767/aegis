"use client";

import { useState } from "react";
import Link from "next/link";
import { Eraser, Laptop, Loader2, Save, Unlink } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField } from "@/components/apps/app-config-primitives";
import {
  AUDIENCE_LABEL,
  CardKindBadge,
  CardStateBadge,
  CopyButton,
  RewardSummary,
  ResultSummary,
  formatCardDate,
  formatRemaining,
  redemptionSourceLabel,
  validityLabel
} from "@/components/apps/card-key/card-key-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { CardKeyDetail, CardKeyExpiryAction, UpdateCardKeyPayload } from "@/lib/api/card-key";
import {
  useCardKeyCatalogQuery,
  useCardKeyDetailQuery,
  useResetCardKeyDevicesMutation,
  useUnbindCardKeyDeviceMutation,
  useUpdateCardKeyMutation
} from "@/lib/card-key-hooks";
import { fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";

type Adjust = {
  remark: string;
  maxDevices: string;
  expiry: "keep" | CardKeyExpiryAction;
  extendDays: string;
  expiresAt: string;
};

function seedAdjust(detail: CardKeyDetail): Adjust {
  return {
    remark: detail.card.remark ?? "",
    maxDevices: String(detail.card.maxDevices),
    expiry: "keep",
    extendDays: "30",
    expiresAt: toLocalInputValue(detail.card.expiresAt)
  };
}

/**
 * 单卡详情：一次拿齐卡、批次、设备、核销与解绑流水（`GET /codes/{id}`）。
 *
 * 调整只开放三项：备注、授权卡的可绑设备数、到期时间（延长 N 天 / 设为某一时刻 / 改为永久）。
 * 延长从「原到期时间与现在两者较晚的那个」起算，给已经过期的卡加 7 天就是再用 7 天。
 * 解绑设备把名额还回去，是「用户换手机了」的兜底出口；清空设备会一次解绑全部。
 */
export function CardKeyDetailSheet({
  appKey,
  cardId,
  onClose
}: {
  appKey: string;
  cardId: number | null;
  onClose: () => void;
}) {
  const detailQuery = useCardKeyDetailQuery(appKey, cardId);
  const catalogQuery = useCardKeyCatalogQuery(appKey);
  const detail = detailQuery.data;

  return (
    <Sheet open={Boolean(cardId)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-[96vw] max-w-2xl flex-col gap-0 p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle className="flex items-center gap-2 font-mono">
            {detail?.card.code ?? "卡密详情"}
            {detail ? <CopyButton text={detail.card.code} label="复制卡面" /> : null}
          </SheetTitle>
          <SheetDescription>{detail ? detail.batch.name : "加载中"}</SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {detailQuery.isLoading || !detail ? (
            <div className="space-y-3 px-6 py-5">
              {[0, 1, 2].map((index) => (
                <Skeleton key={index} className="h-24 w-full rounded-xl" />
              ))}
            </div>
          ) : (
            <DetailBody
              key={`${detail.card.id}-${detail.card.updatedAt}`}
              appKey={appKey}
              detail={detail}
              catalog={catalogQuery.data}
            />
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}

function DetailBody({
  appKey,
  detail,
  catalog
}: {
  appKey: string;
  detail: CardKeyDetail;
  catalog: Parameters<typeof RewardSummary>[0]["catalog"];
}) {
  const { card, batch } = detail;
  const login = card.kind === "login";
  const [adjust, setAdjust] = useState<Adjust>(() => seedAdjust(detail));
  const patch = (next: Partial<Adjust>) => setAdjust({ ...adjust, ...next });
  const updateMutation = useUpdateCardKeyMutation(appKey);
  const unbindMutation = useUnbindCardKeyDeviceMutation(appKey);
  const resetMutation = useResetCardKeyDevicesMutation(appKey);

  const save = async () => {
    const payload: UpdateCardKeyPayload = { remark: adjust.remark.trim() };
    if (login) {
      const devices = Number(adjust.maxDevices);
      if (!Number.isInteger(devices) || devices < 1 || devices > 64) {
        toast.error("可绑定设备数需要在 1–64 之间");
        return;
      }
      payload.maxDevices = devices;
    }
    if (adjust.expiry === "extend") {
      const days = Number(adjust.extendDays);
      if (!Number.isInteger(days) || days === 0 || Math.abs(days) > 36500) {
        toast.error("延长天数需要在 -36500–36500 之间且不为 0");
        return;
      }
      payload.expiryAction = "extend";
      payload.extendDays = days;
    } else if (adjust.expiry === "set") {
      const at = fromLocalInputValue(adjust.expiresAt);
      if (!at) {
        toast.error("请填写到期时间");
        return;
      }
      payload.expiryAction = "set";
      payload.expiresAt = at;
    } else if (adjust.expiry === "permanent") {
      payload.expiryAction = "permanent";
    }
    try {
      await updateMutation.mutateAsync({ cardId: card.id, payload });
      toast.success("已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const unbind = async (deviceId: string) => {
    try {
      await unbindMutation.mutateAsync({ cardId: card.id, deviceId });
      toast.success("已解绑");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解绑失败");
    }
  };

  const reset = async () => {
    try {
      const result = await resetMutation.mutateAsync(card.id);
      toast.success(`已解绑 ${result.affected} 台设备`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const info: Array<[string, React.ReactNode]> = [
    ["类型", <CardKindBadge key="kind" kind={card.kind} />],
    ["状态", <CardStateBadge key="state" card={card} />],
    ["批次", batch.name],
    ["有效期", validityLabel(batch.validityMode, batch.validityDays, batch.validUntil)],
    [
      "到期",
      card.expiresAt ? `${formatCardDate(card.expiresAt)}（${formatRemaining(card.expiresAt)}）` : login && card.status === "unused" ? "激活后计算" : "永久"
    ],
    ["使用", card.maxUses > 1 ? `${card.useCount}/${card.maxUses} 人次` : card.useCount > 0 ? "已使用" : "未使用"],
    [
      "绑定账号",
      card.boundUserId ? (
        <Link key="user" href={`/app-users/${encodeURIComponent(appKey)}/${card.boundUserId}`} className="font-medium hover:underline">
          {card.boundAccount || `#${card.boundUserId}`}
        </Link>
      ) : (
        "—"
      )
    ],
    ["激活时间", formatCardDate(card.activatedAt)],
    ["提取", card.distributedAt ? `${formatCardDate(card.distributedAt)}${card.distributionNote ? `，${card.distributionNote}` : ""}` : "未提取"],
    ["领取对象", AUDIENCE_LABEL[batch.audience] ?? batch.audience],
    ["生成时间", formatCardDate(card.createdAt)]
  ];
  if (card.disabledAt) info.push(["作废", `${formatCardDate(card.disabledAt)}${card.disabledReason ? `，${card.disabledReason}` : ""}`]);
  if (login) info.push(["设备", `${detail.devices.length}/${card.maxDevices} 台`]);

  return (
    <div className="space-y-6 px-6 py-5">
      <dl className="grid gap-x-6 gap-y-2.5 rounded-xl border border-border p-4 sm:grid-cols-2">
        {info.map(([label, value]) => (
          <div key={label} className="flex min-w-0 items-baseline gap-3 text-xs">
            <dt className="w-16 shrink-0 text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-all">{value}</dd>
          </div>
        ))}
      </dl>

      <FieldGroup label={login ? "随卡权益" : "权益"} hint="批次当前配置">
        <RewardSummary rewards={batch.rewards} catalog={catalog} />
      </FieldGroup>

      <FieldGroup label="调整">
        <div className="space-y-4 rounded-xl border border-border p-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className={login ? "space-y-1.5" : "space-y-1.5 sm:col-span-2"}>
              <span className="text-[11px] font-medium">备注</span>
              <Input value={adjust.remark} maxLength={256} onChange={(event) => patch({ remark: event.target.value })} />
            </div>
            {login ? (
              <NumberField
                label="可绑定设备数"
                value={adjust.maxDevices}
                min={1}
                max={64}
                unit="台"
                onChange={(maxDevices) => patch({ maxDevices })}
              />
            ) : null}
          </div>
          <div className="space-y-2">
            <span className="text-[11px] font-medium">到期时间</span>
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              value={adjust.expiry}
              onValueChange={(value) => value && patch({ expiry: value as Adjust["expiry"] })}
            >
              <ToggleGroupItem value="keep" className="h-8 px-3 text-xs">
                不变
              </ToggleGroupItem>
              <ToggleGroupItem value="extend" className="h-8 px-3 text-xs">
                延长
              </ToggleGroupItem>
              <ToggleGroupItem value="set" className="h-8 px-3 text-xs">
                设为
              </ToggleGroupItem>
              <ToggleGroupItem value="permanent" className="h-8 px-3 text-xs">
                永久
              </ToggleGroupItem>
            </ToggleGroup>
            {adjust.expiry === "extend" ? (
              <NumberField
                label="延长天数"
                value={adjust.extendDays}
                min={-36500}
                max={36500}
                unit="天，负数为缩短"
                onChange={(extendDays) => patch({ extendDays })}
              />
            ) : null}
            {adjust.expiry === "set" ? (
              <Input type="datetime-local" value={adjust.expiresAt} onChange={(event) => patch({ expiresAt: event.target.value })} />
            ) : null}
            {adjust.expiry === "permanent" ? (
              <p className="text-[11px] text-muted-foreground">保存后这张卡不再过期</p>
            ) : null}
          </div>
          <div className="flex justify-end">
            <Button size="sm" onClick={save} disabled={updateMutation.isPending}>
              {updateMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              保存调整
            </Button>
          </div>
        </div>
      </FieldGroup>

      {login ? (
        <FieldGroup label="绑定设备" hint={`${detail.devices.length}/${card.maxDevices}`}>
          {detail.devices.length === 0 ? (
            <p className="rounded-xl border border-dashed border-border px-4 py-5 text-center text-xs text-muted-foreground">
              暂无绑定设备
            </p>
          ) : (
            <div className="space-y-2">
              <ul className="divide-y divide-border rounded-xl border border-border">
                {detail.devices.map((device) => (
                  <li key={device.id} className="flex items-center gap-3 px-4 py-2.5">
                    <Laptop className="size-4 shrink-0 text-muted-foreground" />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-xs font-medium">{device.deviceName || "未知设备"}</p>
                      <p className="truncate font-mono text-[11px] text-muted-foreground">{device.deviceId}</p>
                      <p className="text-[11px] text-muted-foreground">
                        首次 {formatCardDate(device.firstSeenAt)}，最近 {formatCardDate(device.lastSeenAt)}，共 {device.seenCount} 次
                      </p>
                    </div>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-7 shrink-0"
                      onClick={() => unbind(device.deviceId)}
                      disabled={unbindMutation.isPending}
                    >
                      <Unlink className="size-3.5" /> 解绑
                    </Button>
                  </li>
                ))}
              </ul>
              <div className="flex justify-end">
                <Button size="sm" variant="outline" onClick={reset} disabled={resetMutation.isPending}>
                  {resetMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Eraser className="size-3.5" />}
                  清空设备
                </Button>
              </div>
            </div>
          )}
        </FieldGroup>
      ) : null}

      <FieldGroup label="核销记录" hint={detail.redemptions.length ? `最近 ${detail.redemptions.length} 条` : undefined}>
        {detail.redemptions.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-5 text-center text-xs text-muted-foreground">
            还没有被使用
          </p>
        ) : (
          <ul className="divide-y divide-border rounded-xl border border-border">
            {detail.redemptions.map((item) => (
              <li key={item.id} className="space-y-1.5 px-4 py-2.5">
                <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                  <div className="flex items-center gap-2">
                    <Link
                      href={`/app-users/${encodeURIComponent(appKey)}/${item.userId}`}
                      className="font-medium hover:underline"
                    >
                      {item.account || `#${item.userId}`}
                    </Link>
                    <Badge variant="secondary" size="sm">
                      {redemptionSourceLabel(item.source)}
                    </Badge>
                    {item.operator ? <span className="text-[11px] text-muted-foreground">{item.operator}</span> : null}
                  </div>
                  <span className="text-[11px] text-muted-foreground">{formatCardDate(item.createdAt)}</span>
                </div>
                <ResultSummary results={item.results} />
              </li>
            ))}
          </ul>
        )}
      </FieldGroup>

      {detail.unbinds.length ? (
        <FieldGroup label="解绑记录">
          <ul className="divide-y divide-border rounded-xl border border-border">
            {detail.unbinds.map((item) => (
              <li key={item.id} className="flex flex-wrap items-center justify-between gap-2 px-4 py-2 text-xs">
                <div className="min-w-0">
                  <span className="font-medium">{item.deviceName || "未知设备"}</span>
                  <span className="ml-2 font-mono text-[11px] text-muted-foreground">{item.deviceId}</span>
                </div>
                <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                  <Badge variant={item.source === "self" ? "info" : "secondary"} size="sm">
                    {item.source === "self" ? "用户自助" : "管理员"}
                  </Badge>
                  {item.operator ? <span>{item.operator}</span> : null}
                  <span>{formatCardDate(item.createdAt)}</span>
                </div>
              </li>
            ))}
          </ul>
        </FieldGroup>
      ) : null}
    </div>
  );
}
