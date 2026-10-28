"use client";

import { useMemo, useState } from "react";
import { KeyRound, Loader2, Plus, Undo2, Unlock } from "lucide-react";
import { toast } from "sonner";
import { termLabel } from "@/components/apps/points-store/points-store-shared";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useNow } from "@/components/users/ban-form";
import { ApiError } from "@/lib/api/client";
import { cn } from "@/lib/utils";
import type { FeatureUnlock } from "@/lib/api/points-store";
import {
  useGrantUserFeatureUnlockMutation,
  useRevokeUserFeatureUnlockMutation,
  useUserFeatureUnlocksQuery
} from "@/lib/points-store-hooks";
import { useAdminVipFeaturesQuery } from "@/lib/vip-hooks";
import {
  Pager,
  RecordSheet,
  RecordTable,
  Section,
  StateBlock,
  TONE_TEXT,
  type Column,
  type InfoItem
} from "./kit";
import { QuickChips, Segmented, type SegmentOption } from "./user-assets-adjust";
import { EMPTY, formatShortTime, formatTime, textValue } from "./user-detail-shared";

type UnlockTerm = "7" | "30" | "90" | "365" | "permanent" | "custom";
type StatusFilter = "all" | "live" | "ended" | "revoked";

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
  admin_grant: "管理员授予",
  invite: "邀请奖励",
  leaderboard: "排行榜奖励",
  lottery: "抽奖",
  card_key: "卡密兑换",
  item_grant: "物品发放"
};

const REVOKE_REASONS = ["误操作授予", "违规使用", "退款收回", "活动结束"];
const PAGE_SIZE = 10;
const DAY = 86_400_000;

type UnlockState = { key: Exclude<StatusFilter, "all">; label: string; variant: "success" | "outline" | "secondary"; live: boolean };

/** 一条解锁记录此刻的状态：收回 > 已结束 > 生效中 */
function unlockState(item: FeatureUnlock, now: number): UnlockState {
  if (item.revokedAt) return { key: "revoked", label: "已收回", variant: "secondary", live: false };
  if (item.endsAt && new Date(item.endsAt).getTime() <= now) {
    return { key: "ended", label: "已结束", variant: "outline", live: false };
  }
  return { key: "live", label: "生效中", variant: "success", live: true };
}

function sourceLabel(source: string) {
  return SOURCE_LABEL[source] ?? source;
}

/**
 * 功能解锁：不当会员，也能单独拥有某个功能（积分兑换、管理员授予）。
 *
 * 与会员并列的第三条线 —— 只往 features 里加东西、不让人成为会员，所以单独一块，
 * 不混进会员区块的「发放」：混进去会让人以为授予功能等于开了会员。
 * 「当前生效」是后端合并后的结论（同一功能多段取最晚终点、永久盖过限时），前端不再推一遍。
 * 接口一次返回全部记录，状态筛选与分页都在本地做。
 */
export function UserFeatureUnlocksPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const unlocksQuery = useUserFeatureUnlocksQuery(appKey, userId);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const revoke = useRevokeUserFeatureUnlockMutation(appKey, userId);
  const now = useNow();

  const [grantOpen, setGrantOpen] = useState(false);
  const [status, setStatus] = useState<StatusFilter>("all");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<FeatureUnlock | null>(null);
  const [revoking, setRevoking] = useState<FeatureUnlock | null>(null);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [reason, setReason] = useState("");

  const features = useMemo(() => featuresQuery.data ?? [], [featuresQuery.data]);
  const featureName = (value: string) => features.find((feature) => feature.tag === value)?.name || value;
  const active = unlocksQuery.data?.active ?? [];
  const items = useMemo(() => unlocksQuery.data?.items ?? [], [unlocksQuery.data]);

  const counts = useMemo(() => {
    const result = { all: items.length, live: 0, ended: 0, revoked: 0 };
    for (const item of items) result[unlockState(item, now).key] += 1;
    return result;
  }, [items, now]);
  const filtered = status === "all" ? items : items.filter((item) => unlockState(item, now).key === status);
  const pages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const currentPage = Math.min(page, pages);
  const rows = filtered.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE);

  const statusOptions: Array<SegmentOption<StatusFilter>> = [
    { value: "all", label: "全部", count: counts.all },
    { value: "live", label: "生效中", count: counts.live },
    { value: "ended", label: "已结束", count: counts.ended },
    { value: "revoked", label: "已收回", count: counts.revoked }
  ];

  function askRevoke(item: FeatureUnlock) {
    setReason("");
    setRevoking(item);
    setRevokeOpen(true);
  }

  async function submitRevoke() {
    if (!revoking) return;
    try {
      await revoke.mutateAsync({ unlockId: revoking.id, reason: reason.trim() });
      toast.success(`已收回「${featureName(revoking.tag)}」`);
      setRevokeOpen(false);
      setSelected(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "收回失败");
    }
  }

  const columns: Array<Column<FeatureUnlock>> = [
    {
      id: "feature",
      header: "功能",
      cell: (item) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-sm">{featureName(item.tag)}</span>
          <span className="truncate font-mono text-xs text-muted-foreground">{item.tag}</span>
        </div>
      )
    },
    {
      id: "source",
      header: "来源",
      cell: (item) => (
        <div className="flex min-w-0 flex-col">
          <span className="text-sm">{sourceLabel(item.source)}</span>
          <span className="max-w-[180px] truncate font-mono text-xs text-muted-foreground">
            {item.sourceRef || textValue(item.operator)}
          </span>
        </div>
      )
    },
    {
      id: "term",
      header: "期限",
      cell: (item) => (
        <span className="text-xs whitespace-nowrap tabular-nums text-muted-foreground">
          {item.endsAt
            ? `${formatShortTime(item.startsAt)} → ${formatShortTime(item.endsAt)}`
            : `${formatShortTime(item.startsAt)} 起永久`}
        </span>
      )
    },
    {
      id: "status",
      header: "状态",
      cell: (item) => {
        const state = unlockState(item, now);
        return (
          <Badge variant={state.variant} size="sm" title={item.revokeReason || undefined}>
            {state.label}
          </Badge>
        );
      }
    },
    {
      id: "actions",
      header: <span className="sr-only">操作</span>,
      align: "right",
      width: "80px",
      cell: (item) =>
        unlockState(item, now).live ? (
          <Button
            size="xs"
            variant="ghost"
            onClick={(event) => {
              event.stopPropagation();
              askRevoke(item);
            }}
          >
            <Undo2 />
            收回
          </Button>
        ) : null
    }
  ];

  const selectedState = selected ? unlockState(selected, now) : null;
  const selectedFields: InfoItem[] = selected
    ? [
        { label: "功能", value: featureName(selected.tag) },
        { label: "功能标识", value: selected.tag, mono: true, copy: selected.tag },
        { label: "来源", value: sourceLabel(selected.source) },
        { label: "来源单号", value: selected.sourceRef || EMPTY, mono: true, copy: selected.sourceRef || null },
        { label: "时长", value: termLabel(selected.durationDays) },
        { label: "操作人", value: textValue(selected.operator) },
        { label: "开始", value: formatTime(selected.startsAt) },
        { label: "结束", value: selected.endsAt ? formatTime(selected.endsAt) : "永久" },
        { label: "创建时间", value: formatTime(selected.createdAt) },
        ...(selected.revokedAt
          ? [
              { label: "收回时间", value: formatTime(selected.revokedAt), tone: "danger" as const },
              { label: "收回人", value: textValue(selected.revokedBy) },
              { label: "收回原因", value: textValue(selected.revokeReason), wide: true }
            ]
          : [])
      ]
    : [];

  return (
    <Section
      title="功能解锁"
      description="不开会员也能单独拥有的功能，与会员权益合并生效"
      icon={KeyRound}
      tone="info"
      flush
      actions={
        <>
          <Badge variant={active.length ? "success" : "outline"} size="sm">
            生效 {active.length} 项
          </Badge>
          <Button size="sm" onClick={() => setGrantOpen(true)} disabled={featuresQuery.isLoading}>
            <Plus />
            授予解锁
          </Button>
        </>
      }
      toolbar={
        <div className="flex flex-col gap-2.5">
          {active.length ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs text-muted-foreground">当前生效</span>
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
          <Segmented
            value={status}
            onChange={(value) => {
              setStatus(value);
              setPage(1);
            }}
            options={statusOptions}
            ariaLabel="解锁状态"
            className="self-start"
          />
        </div>
      }
      footer={filtered.length > PAGE_SIZE ? <Pager page={currentPage} limit={PAGE_SIZE} total={filtered.length} onPage={setPage} /> : undefined}
    >
      <RecordTable
        columns={columns}
        rows={rows}
        rowKey={(item) => item.id}
        loading={unlocksQuery.isLoading}
        fetching={unlocksQuery.isFetching}
        error={unlocksQuery.error}
        onRetry={() => void unlocksQuery.refetch()}
        onRowClick={setSelected}
        rowClassName={(item) => (unlockState(item, now).live ? undefined : "text-muted-foreground")}
        empty={
          <StateBlock
            compact
            icon={KeyRound}
            title={status === "all" ? "暂无解锁记录" : "没有这一状态的记录"}
            action={
              status === "all" ? (
                <Button size="sm" variant="outline" onClick={() => setGrantOpen(true)}>
                  <Plus />
                  授予解锁
                </Button>
              ) : undefined
            }
          />
        }
        minWidth={620}
        skeletonRows={3}
      />

      <GrantUnlockDialog
        open={grantOpen}
        onOpenChange={setGrantOpen}
        appKey={appKey}
        userId={userId}
        features={features.filter((feature) => feature.isActive)}
        items={items}
      />

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        title={selected ? featureName(selected.tag) : ""}
        description={selected ? `解锁记录 #${selected.id}` : undefined}
        hero={
          selected && selectedState ? (
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={selectedState.variant}>{selectedState.label}</Badge>
              <span className="text-sm text-muted-foreground">
                {sourceLabel(selected.source)}，{termLabel(selected.durationDays)}
              </span>
            </div>
          ) : null
        }
        fields={selectedFields}
        raw={selected}
        actions={
          selected && selectedState?.live ? (
            <Button size="sm" variant="destructive" onClick={() => askRevoke(selected)}>
              <Undo2 />
              收回这一条
            </Button>
          ) : undefined
        }
      />

      <AlertDialog open={revokeOpen} onOpenChange={(open) => !revoke.isPending && setRevokeOpen(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>收回「{revoking ? featureName(revoking.tag) : ""}」？</AlertDialogTitle>
            <AlertDialogDescription>只收回这一条，同一功能的其它解锁与会员权益不受影响。</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex flex-col gap-2">
            <Label className="text-xs text-muted-foreground">收回原因</Label>
            <Input value={reason} maxLength={256} onChange={(event) => setReason(event.target.value)} placeholder="选填" />
            <QuickChips items={REVOKE_REASONS} value={reason.trim()} onPick={setReason} />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revoke.isPending}>取消</AlertDialogCancel>
            <Button variant="destructive" onClick={() => void submitRevoke()} disabled={revoke.isPending}>
              {revoke.isPending ? <Loader2 className="animate-spin" /> : null}
              确认收回
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Section>
  );
}

function GrantUnlockDialog({
  open,
  onOpenChange,
  appKey,
  userId,
  features,
  items
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  userId: number;
  features: Array<{ tag: string; name: string; description?: string }>;
  /** 已有的解锁记录：用来预估起点（同一功能限时解锁接在最晚终点之后）与拦下已永久拥有的功能 */
  items: FeatureUnlock[];
}) {
  const grant = useGrantUserFeatureUnlockMutation(appKey, userId);
  const now = useNow();
  const [tag, setTag] = useState("");
  const [term, setTerm] = useState<UnlockTerm>("30");
  const [days, setDays] = useState("30");

  const selectedTag = tag || features[0]?.tag || "";
  const selectedFeature = features.find((feature) => feature.tag === selectedTag);
  const dayCount = Number(days);
  const daysValid = days !== "" && Number.isInteger(dayCount) && dayCount >= 0 && dayCount <= 36500;

  // 与后端 grantFeatureUnlockTx 同一规则：同一功能仍在期内的限时解锁，新的一段接在最晚终点之后；
  // 已有永久解锁时后端拒绝。永久解锁总是从现在起算。
  let permanentOwned = false;
  let latestEnd = Number.NaN;
  for (const item of items) {
    if (item.tag !== selectedTag || item.revokedAt) continue;
    if (!item.endsAt) {
      permanentOwned = true;
      continue;
    }
    const end = new Date(item.endsAt).getTime();
    if (end > now && !(end <= latestEnd)) latestEnd = end;
  }
  const start = Number.isFinite(latestEnd) ? latestEnd : now;
  const ready = Boolean(selectedTag) && daysValid && !permanentOwned;

  async function submit() {
    if (!ready || grant.isPending) return;
    const name = selectedFeature?.name || selectedTag;
    try {
      await grant.mutateAsync({ tag: selectedTag, days: dayCount });
      toast.success(`已解锁「${name}」${termLabel(dayCount)}`);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解锁失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !grant.isPending && onOpenChange(next)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>授予功能解锁</DialogTitle>
          <DialogDescription>只解锁单个功能，不会让用户成为会员。</DialogDescription>
        </DialogHeader>

        {features.length === 0 ? (
          <StateBlock compact title="暂无可用功能" description="先在会员设置的功能目录里添加并启用功能" />
        ) : (
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label className="text-xs text-muted-foreground">功能</Label>
              <Select value={selectedTag} onValueChange={setTag}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="选择功能" />
                </SelectTrigger>
                <SelectContent>
                  {features.map((feature) => (
                    <SelectItem key={feature.tag} value={feature.tag}>
                      {feature.name || feature.tag}
                      <span className="ml-1.5 font-mono text-xs text-muted-foreground">{feature.tag}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {selectedFeature?.description ? (
                <p className="text-xs leading-5 text-muted-foreground">{selectedFeature.description}</p>
              ) : null}
            </div>

            <div className="flex flex-col gap-2">
              <Label className="text-xs text-muted-foreground">期限</Label>
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
                <div className="relative sm:max-w-48">
                  <Input
                    autoFocus
                    value={days}
                    inputMode="numeric"
                    aria-label="解锁天数"
                    placeholder="天数，0 为永久"
                    className="pr-8 tabular-nums"
                    onChange={(event) => setDays(event.target.value.replace(/\D/g, ""))}
                  />
                  <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-muted-foreground">
                    天
                  </span>
                </div>
              ) : null}
              <p className={cn("text-xs", permanentOwned ? TONE_TEXT.warning : "text-muted-foreground")}>
                {permanentOwned
                  ? "该用户已永久拥有这个功能，无需再授予"
                  : !daysValid
                    ? "天数需为 0 到 36500 的整数"
                    : dayCount === 0
                      ? "永久有效，从现在起算"
                      : `${Number.isFinite(latestEnd) ? "接在现有解锁之后" : "从现在起算"}，到 ${formatTime(new Date(start + dayCount * DAY).toISOString())}`}
              </p>
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={grant.isPending}>
            取消
          </Button>
          <Button onClick={() => void submit()} disabled={!ready || grant.isPending}>
            {grant.isPending ? <Loader2 className="animate-spin" /> : <Unlock />}
            {!ready ? "解锁" : dayCount === 0 ? "永久解锁" : `解锁 ${termLabel(dayCount)}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
