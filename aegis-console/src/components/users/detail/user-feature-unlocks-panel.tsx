"use client";

import { useMemo, useState } from "react";
import { KeyRound, Loader2, Undo2, Unlock } from "lucide-react";
import { toast } from "sonner";
import { termLabel } from "@/components/apps/points-store/points-store-shared";
import { Badge } from "@/components/ui/badge";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type { FeatureUnlock } from "@/lib/api/points-store";
import {
  useGrantUserFeatureUnlockMutation,
  useRevokeUserFeatureUnlockMutation,
  useUserFeatureUnlocksQuery
} from "@/lib/points-store-hooks";
import { useAdminVipFeaturesQuery } from "@/lib/vip-hooks";
import { Segmented, type SegmentOption } from "./user-assets-adjust";
import { EMPTY, EmptyRow, Panel, formatShortTime, formatTime, textValue } from "./user-detail-shared";

type UnlockTerm = "7" | "30" | "90" | "365" | "permanent" | "custom";

/** 天数为 0 即永久，与积分商城的权益期限同一约定。 */
const UNLOCK_TERMS: Array<SegmentOption<UnlockTerm>> = [
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天" },
  { value: "90", label: "90 天" },
  { value: "365", label: "1 年" },
  { value: "permanent", label: "永久" },
  { value: "custom", label: "自定义" }
];

const SOURCE_LABEL: Record<string, string> = {
  points_store: "积分兑换",
  admin_grant: "管理员授予"
};

/** 一条解锁记录此刻的状态：收回 > 已结束 > 生效中 */
function unlockState(item: FeatureUnlock, now = Date.now()) {
  if (item.revokedAt) return { label: "已收回", variant: "secondary" as const, live: false };
  if (item.endsAt && new Date(item.endsAt).getTime() <= now) {
    return { label: "已结束", variant: "outline" as const, live: false };
  }
  return { label: "生效中", variant: "success" as const, live: true };
}

/**
 * 功能解锁：不当会员，也能单独拥有某个功能（积分兑换、管理员授予）。
 *
 * 与会员并列的第三条线 —— 只往 features 里加东西、不让人成为会员，所以单独一块，
 * 不混进会员面板的「发放」：混进去会让人以为授予功能等于开了会员。
 * 「当前生效」是后端合并后的结论（同一功能多段取最晚终点、永久盖过限时），前端不再推一遍。
 */
export function UserFeatureUnlocksPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const unlocksQuery = useUserFeatureUnlocksQuery(appKey, userId);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const grant = useGrantUserFeatureUnlockMutation(appKey, userId);
  const revoke = useRevokeUserFeatureUnlockMutation(appKey, userId);

  const [tag, setTag] = useState("");
  const [term, setTerm] = useState<UnlockTerm>("30");
  const [days, setDays] = useState("30");
  const [revoking, setRevoking] = useState<FeatureUnlock | null>(null);
  const [reason, setReason] = useState("");

  const features = useMemo(() => featuresQuery.data ?? [], [featuresQuery.data]);
  const featureName = (value: string) => features.find((feature) => feature.tag === value)?.name || value;
  const activeFeatures = features.filter((feature) => feature.isActive);
  const selectedTag = tag || activeFeatures[0]?.tag || "";
  const active = unlocksQuery.data?.active ?? [];
  const items = unlocksQuery.data?.items ?? [];

  const submitGrant = async () => {
    if (!selectedTag) {
      toast.error("请选择功能");
      return;
    }
    const value = Number(days);
    if (!Number.isInteger(value) || value < 0) {
      toast.error("天数需为非负整数");
      return;
    }
    try {
      await grant.mutateAsync({ tag: selectedTag, days: value });
      toast.success(`已解锁「${featureName(selectedTag)}」${termLabel(value)}`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解锁失败");
    }
  };

  const submitRevoke = async () => {
    if (!revoking) return;
    try {
      await revoke.mutateAsync({ unlockId: revoking.id, reason: reason.trim() });
      toast.success("已收回");
      setRevoking(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "收回失败");
    }
  };

  return (
    <Panel
      title="功能解锁"
      icon={<KeyRound className="size-4" />}
      action={
        <Badge variant={active.length ? "success" : "outline"} size="sm">
          生效 {active.length} 项
        </Badge>
      }
      bodyClassName="p-0"
    >
      <div className="space-y-3 border-b px-5 py-4">
        {active.length ? (
          <div className="flex flex-wrap items-center gap-1.5">
            {active.map((view) => (
              <Badge key={view.tag} variant="secondary" size="sm" className="gap-1">
                <Unlock className="size-3" />
                {featureName(view.tag)}
                <span className="font-normal text-muted-foreground">
                  {view.permanent ? "永久" : `至 ${formatShortTime(view.expireAt)}`}
                </span>
              </Badge>
            ))}
          </div>
        ) : null}

        <div className="grid gap-3 lg:grid-cols-[minmax(0,14rem)_minmax(0,1fr)_auto] lg:items-end">
          <div className="space-y-1.5">
            <Label className="text-xs font-medium text-muted-foreground">功能</Label>
            <Select value={selectedTag} onValueChange={setTag} disabled={!activeFeatures.length}>
              <SelectTrigger className="h-8 w-full text-xs">
                <SelectValue placeholder="暂无功能标识" />
              </SelectTrigger>
              <SelectContent>
                {activeFeatures.map((feature) => (
                  <SelectItem key={feature.tag} value={feature.tag}>
                    {feature.name || feature.tag}
                    <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs font-medium text-muted-foreground">期限</Label>
            <div className="flex flex-wrap items-center gap-2">
              <Segmented
                value={term}
                onChange={(value) => {
                  setTerm(value);
                  if (value !== "custom") setDays(value === "permanent" ? "0" : value);
                }}
                options={UNLOCK_TERMS}
                ariaLabel="解锁期限"
              />
              {term === "custom" ? (
                <div className="relative">
                  <Input
                    autoFocus
                    value={days}
                    inputMode="numeric"
                    aria-label="解锁天数"
                    className="h-8 w-24 pr-7 text-xs tabular-nums"
                    onChange={(event) => setDays(event.target.value.replace(/\D/g, ""))}
                  />
                  <span className="pointer-events-none absolute inset-y-0 right-2.5 flex items-center text-xs text-muted-foreground">
                    天
                  </span>
                </div>
              ) : null}
            </div>
          </div>
          <Button size="sm" className="h-8" onClick={submitGrant} disabled={grant.isPending || !selectedTag || days === ""}>
            {grant.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Unlock className="size-3.5" />}
            {!selectedTag || days === "" ? "解锁" : Number(days) === 0 ? "永久解锁" : `解锁 ${termLabel(Number(days))}`}
          </Button>
        </div>
      </div>

      {unlocksQuery.isLoading ? (
        <div className="space-y-2 p-5">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-9 w-full rounded-lg" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyRow text="暂无解锁记录" />
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>功能</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>期限</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => {
                const state = unlockState(item);
                return (
                  <TableRow key={item.id}>
                    <TableCell className="text-xs">
                      {featureName(item.tag)}
                      <p className="font-mono text-[10px] text-muted-foreground">{item.tag}</p>
                    </TableCell>
                    <TableCell className="text-xs">
                      {SOURCE_LABEL[item.source] ?? item.source}
                      <p className="font-mono text-[10px] text-muted-foreground">
                        {item.sourceRef || textValue(item.operator)}
                      </p>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-[11px] tabular-nums text-muted-foreground">
                      {item.endsAt ? (
                        <>
                          {formatShortTime(item.startsAt)} → {formatShortTime(item.endsAt)}
                        </>
                      ) : (
                        `${formatShortTime(item.startsAt)} 起永久`
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant={state.variant} size="sm" title={item.revokeReason || undefined}>
                        {state.label}
                      </Badge>
                      {item.revokedAt ? (
                        <p className="mt-0.5 text-[10px] text-muted-foreground" title={formatTime(item.revokedAt)}>
                          {textValue(item.revokedBy)}
                        </p>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      {state.live ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7"
                          onClick={() => {
                            setReason("");
                            setRevoking(item);
                          }}
                        >
                          <Undo2 className="size-3.5" /> 收回
                        </Button>
                      ) : (
                        EMPTY
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <Dialog open={Boolean(revoking)} onOpenChange={(open) => !open && setRevoking(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>收回「{revoking ? featureName(revoking.tag) : ""}」？</DialogTitle>
            <DialogDescription>只收回这一条，同一功能的其它解锁不受影响。</DialogDescription>
          </DialogHeader>
          <Input value={reason} maxLength={256} onChange={(event) => setReason(event.target.value)} placeholder="原因" />
          <DialogFooter>
            <Button variant="ghost" size="sm" onClick={() => setRevoking(null)}>
              返回
            </Button>
            <Button size="sm" variant="destructive" onClick={submitRevoke} disabled={revoke.isPending}>
              {revoke.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认收回
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Panel>
  );
}
