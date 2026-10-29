"use client";

import { useMemo, useState } from "react";
import {
  Clover,
  Dices,
  Eye,
  Flame,
  Loader2,
  MoveVertical,
  Pin,
  RotateCcw,
  Search,
  ShieldOff,
  SlidersHorizontal,
  Undo2,
  Users,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { SupremeCard, SupremeReadingNote } from "@/components/apps/lottery/lottery-supreme-panel";
import { formatStoreDate } from "@/components/apps/points-store/points-store-shared";
import { UserPicker as SingleUserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { StatTile } from "@/components/content/content-shared";
import { UserPicker as MultiUserPicker } from "@/components/releases/release-audience";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Slider } from "@/components/ui/slider";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type {
  LotteryFortuneControl,
  LotteryFortuneControlKind,
  LotteryFortuneControlPayload,
  LotteryFortuneControlStatus,
  LotteryFortuneDistribution,
  LotteryFortuneLevel,
  LotteryFortuneOverview,
  LotteryFortuneReading
} from "@/lib/api/lottery";
import {
  useCreateLotteryFortuneControlsMutation,
  useLotteryActivitiesQuery,
  useLotteryFortuneControlsQuery,
  useLotteryFortuneQuery,
  useLotteryFortuneUserQuery,
  usePreviewLotteryFortuneControlMutation,
  useRevokeLotteryFortuneControlsMutation,
  useSetLotteryFortuneSpreadMutation
} from "@/lib/lottery-hooks";
import { cn } from "@/lib/utils";

/**
 * 运气控制：每日运气的总览与管理员干预。
 *
 * | 区块 | 做什么 |
 * |---|---|
 * | 运气差异 | 开关每日运气、调 σ（只改策略里这一项） |
 * | 今日分布 | 今天抽过奖的人：理论占比 / 干预前 / 干预后三列对比，平均系数，被干预人数 |
 * | 下干预 | 对象（指定用户 / 全站）× 方式（重抽 / 定档 / 偏移）× 时间窗；先预演看分布，再执行 |
 * | 查一个人 | 他此刻干预前后的运气、生效的干预；一键重抽、定为大爆发、撤销他的全部干预 |
 * | 干预记录 | 全部干预与状态，逐条撤销，或全部恢复自然运气 |
 *
 * 合成规则（后端 ResolveFortune）：重抽加盐 → 定档（个人优先于全站）→ 没有定档时偏移累加 → z 截在 ±2。
 * 活动页、抽奖与模拟用同一个结果；用户端看不到有没有被干预。
 */

const LEVEL_META: Record<LotteryFortuneLevel, { label: string; bar: string; badge: "danger" | "success" | "info" | "secondary" }> = {
  burst: { label: "运气大爆发", bar: "bg-orange-500", badge: "danger" },
  lucky: { label: "鸿运当头", bar: "bg-emerald-500", badge: "success" },
  steady: { label: "运势平稳", bar: "bg-sky-500", badge: "info" },
  quiet: { label: "手气平淡", bar: "bg-slate-400", badge: "secondary" }
};

const LEVELS: LotteryFortuneLevel[] = ["burst", "lucky", "steady", "quiet"];

const KIND_META: Record<LotteryFortuneControlKind, { label: string; hint: string; icon: typeof Dices }> = {
  reroll: { label: "重抽", hint: "换一个新手气，分布不变", icon: Dices },
  pin: { label: "定档", hint: "定在某一档或某个系数", icon: Pin },
  shift: { label: "偏移", hint: "整体加减运气", icon: MoveVertical }
};

const STATUS_META: Record<LotteryFortuneControlStatus, { label: string; variant: "success" | "warning" | "secondary" | "outline" }> = {
  active: { label: "生效中", variant: "success" },
  scheduled: { label: "待生效", variant: "warning" },
  expired: { label: "已到期", variant: "secondary" },
  revoked: { label: "已撤销", variant: "outline" }
};

type WindowPreset = "today" | "1h" | "24h" | "7d" | "custom";

const WINDOWS: Array<{ key: WindowPreset; label: string }> = [
  { key: "today", label: "今天" },
  { key: "1h", label: "1 小时" },
  { key: "24h", label: "24 小时" },
  { key: "7d", label: "7 天" },
  { key: "custom", label: "自定义" }
];

function errorText(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

/** z 与差异换算成系数（与后端 exp(σz − σ²/2) 同式） */
function factorOf(spread: number, z: number) {
  return Math.exp(spread * z - (spread * spread) / 2);
}

function formatFactor(value: number) {
  return `×${value.toFixed(2)}`;
}

function describeControl(control: LotteryFortuneControl, spread: number) {
  switch (control.kind) {
    case "pin":
      return control.level
        ? `定为${LEVEL_META[control.level as LotteryFortuneLevel]?.label ?? control.level}`
        : `定为系数 ${formatFactor(factorOf(spread, control.z))}`;
    case "shift":
      return `运气 ${control.z > 0 ? "+" : ""}${control.z.toFixed(2)}`;
    default:
      return "重抽";
  }
}

function targetLabel(control: LotteryFortuneControl) {
  if (!control.userId) return "全站";
  return control.nickname || control.account || `UID-${control.userId}`;
}

export function LotteryFortunePanel({
  appKey,
  activityId,
  onActivityChange
}: {
  appKey: string;
  activityId?: number;
  onActivityChange: (activityId: number) => void;
}) {
  const activitiesQuery = useLotteryActivitiesQuery(appKey);
  const activities = useMemo(() => activitiesQuery.data ?? [], [activitiesQuery.data]);
  const currentId = activityId && activities.some((item) => item.id === activityId) ? activityId : activities[0]?.id;
  const overviewQuery = useLotteryFortuneQuery(appKey, currentId);
  const overview = overviewQuery.data;

  if (activitiesQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }
  if (!currentId) {
    return (
      <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
        暂无活动
      </p>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Select value={String(currentId)} onValueChange={(next) => onActivityChange(Number(next))}>
          <SelectTrigger className="h-8 w-60">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {activities.map((activity) => (
              <SelectItem key={activity.id} value={String(activity.id)}>
                {activity.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant="outline" size="sm" onClick={() => overviewQuery.refetch()} disabled={overviewQuery.isFetching}>
          {overviewQuery.isFetching ? <Loader2 className="size-3.5 animate-spin" /> : <RotateCcw className="size-3.5" />}
          刷新
        </Button>
      </div>

      {overviewQuery.isLoading || !overview ? (
        <Skeleton className="h-64 w-full rounded-2xl" />
      ) : (
        <>
          <SpreadCard appKey={appKey} activityId={currentId} overview={overview} />
          <SupremeCard appKey={appKey} overview={overview} />
          {overview.enabled ? (
            <>
              <DistributionCard overview={overview} />
              <div className="grid gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)]">
                <ComposerCard appKey={appKey} activityId={currentId} overview={overview} />
                <UserLookupCard appKey={appKey} activityId={currentId} overview={overview} />
              </div>
            </>
          ) : null}
          <ControlsCard appKey={appKey} activityId={currentId} overview={overview} />
        </>
      )}
    </div>
  );
}

// ── 运气差异 ──

function SpreadCard({ appKey, activityId, overview }: { appKey: string; activityId: number; overview: LotteryFortuneOverview }) {
  const [draft, setDraft] = useState<number | null>(null);
  const mutation = useSetLotteryFortuneSpreadMutation(appKey, activityId);
  const spread = draft ?? (overview.enabled ? overview.spread : 0.3);
  const low = factorOf(spread, -2);
  const high = factorOf(spread, 2);
  const dirty = draft !== null && (!overview.enabled || Math.abs(draft - overview.spread) > 1e-6);

  const save = async (value: number) => {
    try {
      await mutation.mutateAsync(value);
      setDraft(null);
      toast.success(value === 0 ? "已关闭每日运气" : `运气差异已设为 ${value.toFixed(2)}`);
    } catch (error) {
      toast.error(errorText(error, "保存失败"));
    }
  };

  return (
    <SectionCard
      icon={<Clover className="size-4" />}
      title="每日运气"
      description="每人每天一个运气系数，乘在兜底奖以外的奖品上；差异越大，人与人之间的手气差得越多"
      aside={
        <Badge variant={overview.enabled ? "success" : "outline"} size="sm">
          {overview.enabled ? "已开启" : "未开启"}
        </Badge>
      }
    >
      <div className="grid gap-5 px-5 py-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
        <div className="space-y-3">
          <div className="flex items-baseline justify-between gap-3">
            <Label className="text-xs text-muted-foreground">运气差异 σ</Label>
            <span className="font-mono text-sm tabular-nums">{spread.toFixed(2)}</span>
          </div>
          <Slider min={0.05} max={1} step={0.05} value={[spread]} onValueChange={([value]) => setDraft(value)} />
          <p className="text-xs text-muted-foreground">
            系数范围 <span className="font-mono text-foreground">{formatFactor(low)}</span> 至{" "}
            <span className="font-mono text-foreground">{formatFactor(high)}</span>，最走运与最倒霉相差{" "}
            <span className="font-mono text-foreground">{(high / low).toFixed(1)}</span> 倍
          </p>
        </div>
        <div className="flex gap-2">
          {overview.enabled ? (
            <Button variant="outline" size="sm" onClick={() => save(0)} disabled={mutation.isPending}>
              <ShieldOff className="size-3.5" />
              关闭
            </Button>
          ) : null}
          <Button size="sm" onClick={() => save(spread)} disabled={mutation.isPending || (overview.enabled && !dirty)}>
            {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {overview.enabled ? "保存差异" : "开启每日运气"}
          </Button>
        </div>
      </div>
    </SectionCard>
  );
}

// ── 今日分布 ──

function DistributionCard({ overview }: { overview: LotteryFortuneOverview }) {
  const today = overview.today;
  return (
    <SectionCard
      icon={<Users className="size-4" />}
      title="今日分布"
      description={`今天抽过奖的人（最多 5000 人）此刻的运气，${formatStoreDate(overview.resetsAt)} 换运气`}
    >
      <div className="space-y-4 px-5 py-4">
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatTile label="今日抽奖人数" value={today.users} icon={Users} />
          <StatTile label="被干预人数" value={today.controlled} icon={SlidersHorizontal} tone={today.controlled ? "positive" : "muted"} />
          <StatTile
            label="平均系数"
            value={formatFactor(today.meanFactor)}
            hint={`干预前 ${formatFactor(today.baseMeanFactor)}`}
            icon={Zap}
          />
          <StatTile
            label="生效中的干预"
            value={overview.counts.activeGlobal + overview.counts.activeUsers}
            hint={`全站 ${overview.counts.activeGlobal}，个人 ${overview.counts.activeUsers}，待生效 ${overview.counts.scheduled}`}
            icon={Pin}
          />
        </div>
        <LevelBars overview={overview} distribution={today} />
      </div>
    </SectionCard>
  );
}

/** 各档位：理论占比、干预前、干预后（预演时「干预后」换成预演结果） */
function LevelBars({
  overview,
  distribution,
  afterLabel = "干预后"
}: {
  overview: LotteryFortuneOverview;
  distribution: LotteryFortuneDistribution;
  afterLabel?: string;
}) {
  const total = Math.max(distribution.users, 1);
  return (
    <div className="space-y-2.5">
      <div className="grid grid-cols-[6.5rem_1fr_4.5rem_4.5rem_4.5rem] gap-3 text-[11px] text-muted-foreground">
        <span>档位</span>
        <span />
        <span className="text-right">理论</span>
        <span className="text-right">干预前</span>
        <span className="text-right">{afterLabel}</span>
      </div>
      {LEVELS.map((level) => {
        const info = overview.levels.find((item) => item.level === level);
        const after = distribution.levels[level] ?? 0;
        const before = distribution.baseLevels[level] ?? 0;
        return (
          <div key={level} className="grid grid-cols-[6.5rem_1fr_4.5rem_4.5rem_4.5rem] items-center gap-3 text-xs">
            <div className="flex flex-col">
              <span className="font-medium">{LEVEL_META[level].label}</span>
              {info ? <span className="font-mono text-[10px] text-muted-foreground">{formatFactor(info.factor)}</span> : null}
            </div>
            <div className="relative h-2.5 overflow-hidden rounded-full bg-muted">
              <div
                className="absolute inset-y-0 left-0 rounded-full bg-foreground/15"
                style={{ width: `${(before / total) * 100}%` }}
              />
              <div
                className={cn("absolute inset-y-0 left-0 rounded-full transition-[width] duration-500", LEVEL_META[level].bar)}
                style={{ width: `${(after / total) * 100}%` }}
              />
              {info ? (
                <div className="absolute inset-y-0 w-0.5 bg-foreground/60" style={{ left: `${info.share * 100}%` }} />
              ) : null}
            </div>
            <span className="text-right font-mono tabular-nums text-muted-foreground">
              {info ? `${(info.share * 100).toFixed(1)}%` : "—"}
            </span>
            <span className="text-right font-mono tabular-nums text-muted-foreground">{before}</span>
            <span className="text-right font-mono tabular-nums">{after}</span>
          </div>
        );
      })}
      <p className="text-[11px] text-muted-foreground">实色条是干预后的人数，浅色条是干预前，竖线是没有干预时的理论占比。</p>
    </div>
  );
}

// ── 下干预 ──

function windowOf(preset: WindowPreset, customStart: string, customEnd: string): { startsAt?: string; endsAt?: string } {
  const now = Date.now();
  switch (preset) {
    case "1h":
      return { endsAt: new Date(now + 3_600_000).toISOString() };
    case "24h":
      return { endsAt: new Date(now + 86_400_000).toISOString() };
    case "7d":
      return { endsAt: new Date(now + 7 * 86_400_000).toISOString() };
    case "custom":
      return {
        startsAt: customStart ? new Date(customStart).toISOString() : undefined,
        endsAt: customEnd ? new Date(customEnd).toISOString() : undefined
      };
    default:
      return {};
  }
}

function ComposerCard({ appKey, activityId, overview }: { appKey: string; activityId: number; overview: LotteryFortuneOverview }) {
  const [scope, setScope] = useState<"users" | "all">("users");
  const [userIds, setUserIds] = useState<number[]>([]);
  const [kind, setKind] = useState<LotteryFortuneControlKind>("pin");
  const [pinBy, setPinBy] = useState<"level" | "factor">("level");
  const [level, setLevel] = useState<LotteryFortuneLevel>("burst");
  const [factor, setFactor] = useState("1.2");
  const [shift, setShift] = useState(0.5);
  const [preset, setPreset] = useState<WindowPreset>("today");
  const [customStart, setCustomStart] = useState("");
  const [customEnd, setCustomEnd] = useState("");
  const [note, setNote] = useState("");
  const [confirming, setConfirming] = useState(false);
  const preview = usePreviewLotteryFortuneControlMutation(appKey, activityId);
  const create = useCreateLotteryFortuneControlsMutation(appKey, activityId);

  const payload = (): LotteryFortuneControlPayload => ({
    ...(scope === "all" ? { allUsers: true } : { userIds }),
    kind,
    ...(kind === "pin" ? (pinBy === "level" ? { level } : { factor: Number(factor) }) : {}),
    ...(kind === "shift" ? { shift } : {}),
    ...windowOf(preset, customStart, customEnd),
    note: note.trim() || undefined
  });
  const ready = scope === "all" || userIds.length > 0;

  const runPreview = async () => {
    try {
      await preview.mutateAsync(payload());
    } catch (error) {
      toast.error(errorText(error, "预演失败"));
    }
  };

  const submit = async () => {
    try {
      const result = await create.mutateAsync(payload());
      toast.success(scope === "all" ? "全站干预已生效" : `已对 ${result.items.length} 人生效`);
      setConfirming(false);
      preview.reset();
      setNote("");
    } catch (error) {
      toast.error(errorText(error, "干预失败"));
    }
  };

  const summary = (() => {
    const who = scope === "all" ? "全站所有人" : `${userIds.length} 位用户`;
    const what =
      kind === "reroll"
        ? "重抽今日运气"
        : kind === "shift"
          ? `运气 ${shift > 0 ? "+" : ""}${shift.toFixed(2)}`
          : pinBy === "level"
            ? `定为${LEVEL_META[level].label}`
            : `定为系数 ×${Number(factor).toFixed(2)}`;
    const when = WINDOWS.find((item) => item.key === preset)?.label ?? "";
    return `${who}，${what}，${preset === "today" ? "到今天结束" : `生效 ${when}`}`;
  })();

  return (
    <SectionCard icon={<SlidersHorizontal className="size-4" />} title="干预运气" description="先预演看分布，再执行；执行后活动页与抽奖立刻按它算">
      <div className="space-y-4 px-5 py-4">
        <Field label="对象">
          <ToggleGroup type="single" size="sm" variant="outline" value={scope} onValueChange={(value) => value && setScope(value as "users" | "all")}>
            <ToggleGroupItem value="users">指定用户</ToggleGroupItem>
            <ToggleGroupItem value="all">全站</ToggleGroupItem>
          </ToggleGroup>
          {scope === "users" ? <MultiUserPicker appKey={appKey} value={userIds} onChange={setUserIds} /> : null}
        </Field>

        <Field label="方式">
          <div className="grid grid-cols-3 gap-2">
            {(Object.keys(KIND_META) as LotteryFortuneControlKind[]).map((key) => {
              const meta = KIND_META[key];
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => setKind(key)}
                  className={cn(
                    "flex flex-col items-start gap-1 rounded-xl border px-3 py-2.5 text-left transition-colors",
                    kind === key ? "border-primary bg-primary/5" : "border-border hover:bg-muted/50"
                  )}
                >
                  <span className="flex items-center gap-1.5 text-xs font-medium">
                    <meta.icon className="size-3.5" />
                    {meta.label}
                  </span>
                  <span className="text-[11px] text-muted-foreground">{meta.hint}</span>
                </button>
              );
            })}
          </div>
        </Field>

        {kind === "pin" ? (
          <Field label="定在">
            <ToggleGroup type="single" size="sm" variant="outline" value={pinBy} onValueChange={(value) => value && setPinBy(value as "level" | "factor")}>
              <ToggleGroupItem value="level">按档位</ToggleGroupItem>
              <ToggleGroupItem value="factor">按系数</ToggleGroupItem>
            </ToggleGroup>
            {pinBy === "level" ? (
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                {LEVELS.map((item) => {
                  const info = overview.levels.find((entry) => entry.level === item);
                  return (
                    <button
                      key={item}
                      type="button"
                      onClick={() => setLevel(item)}
                      className={cn(
                        "flex items-center justify-between gap-2 rounded-lg border px-2.5 py-2 text-xs transition-colors",
                        level === item ? "border-primary bg-primary/5" : "border-border hover:bg-muted/50"
                      )}
                    >
                      <span className="flex items-center gap-1.5">
                        <span className={cn("size-2 rounded-full", LEVEL_META[item].bar)} />
                        {LEVEL_META[item].label}
                      </span>
                      {info ? <span className="font-mono text-[10px] text-muted-foreground">{formatFactor(info.factor)}</span> : null}
                    </button>
                  );
                })}
              </div>
            ) : (
              <div className="flex items-center gap-2">
                <Input
                  type="number"
                  step="0.01"
                  min={overview.low}
                  max={overview.high}
                  value={factor}
                  onChange={(event) => setFactor(event.target.value)}
                  className="h-8 w-32 font-mono"
                />
                <span className="text-xs text-muted-foreground">
                  范围 {formatFactor(overview.low)} 至 {formatFactor(overview.high)}
                </span>
              </div>
            )}
          </Field>
        ) : null}

        {kind === "shift" ? (
          <Field label={`偏移 ${shift > 0 ? "+" : ""}${shift.toFixed(2)}`}>
            <Slider min={-4} max={4} step={0.25} value={[shift]} onValueChange={([value]) => setShift(value === 0 ? 0.25 : value)} />
            <p className="text-[11px] text-muted-foreground">
              以标准差为单位。+0.5 约把一个运势平稳的人推到鸿运当头；±2 已能从最倒霉推到最走运。有定档的人不受偏移影响。
            </p>
          </Field>
        ) : null}

        <Field label="生效时间">
          <ToggleGroup type="single" size="sm" variant="outline" value={preset} onValueChange={(value) => value && setPreset(value as WindowPreset)}>
            {WINDOWS.map((item) => (
              <ToggleGroupItem key={item.key} value={item.key}>
                {item.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          {preset === "custom" ? (
            <div className="grid gap-2 sm:grid-cols-2">
              <Input type="datetime-local" value={customStart} onChange={(event) => setCustomStart(event.target.value)} className="h-8" />
              <Input type="datetime-local" value={customEnd} onChange={(event) => setCustomEnd(event.target.value)} className="h-8" />
            </div>
          ) : null}
        </Field>

        <Field label="备注">
          <Input value={note} maxLength={200} placeholder="可选，例如：活动补偿" onChange={(event) => setNote(event.target.value)} className="h-8" />
        </Field>

        {preview.data ? (
          <div className="rounded-xl border border-dashed border-border p-3">
            <p className="mb-2 text-xs font-medium">
              预演结果（{preview.data.users} 人，平均系数 {formatFactor(preview.data.baseMeanFactor)} → {formatFactor(preview.data.meanFactor)}）
            </p>
            <LevelBars overview={overview} distribution={preview.data} afterLabel="预演后" />
          </div>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-4">
          <p className="text-xs text-muted-foreground">{summary}</p>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={runPreview} disabled={!ready || preview.isPending}>
              {preview.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Eye className="size-3.5" />}
              预演
            </Button>
            <Button size="sm" onClick={() => setConfirming(true)} disabled={!ready || create.isPending}>
              {create.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Zap className="size-3.5" />}
              执行
            </Button>
          </div>
        </div>
      </div>

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{scope === "all" ? "确认干预全站运气" : "确认干预运气"}</AlertDialogTitle>
            <AlertDialogDescription>{summary}。立刻生效，活动页与抽奖同时按它算；可在干预记录里撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void submit();
              }}
              disabled={create.isPending}
            >
              {create.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              执行
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-2">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {children}
    </div>
  );
}

// ── 查一个人 ──

function UserLookupCard({ appKey, activityId, overview }: { appKey: string; activityId: number; overview: LotteryFortuneOverview }) {
  const [user, setUser] = useState<PickedUser | null>(null);
  const query = useLotteryFortuneUserQuery(appKey, activityId, user?.id);
  const create = useCreateLotteryFortuneControlsMutation(appKey, activityId);
  const revoke = useRevokeLotteryFortuneControlsMutation(appKey, activityId);
  const reading = query.data;
  const own = reading?.controls.filter((item) => item.status === "active" || item.status === "scheduled") ?? [];

  const quick = async (payload: LotteryFortuneControlPayload, done: string) => {
    if (!user) return;
    try {
      await create.mutateAsync({ ...payload, userIds: [user.id] });
      toast.success(done);
    } catch (error) {
      toast.error(errorText(error, "操作失败"));
    }
  };

  const revokeOwn = async () => {
    try {
      const result = await revoke.mutateAsync({ ids: own.map((item) => item.id) });
      toast.success(`已撤销 ${result.revoked} 条`);
    } catch (error) {
      toast.error(errorText(error, "撤销失败"));
    }
  };

  const busy = create.isPending || revoke.isPending;

  return (
    <SectionCard icon={<Search className="size-4" />} title="查一个人" description="此刻的运气与他名下的干预">
      <div className="space-y-4 px-5 py-4">
        <SingleUserPicker appKey={appKey} value={user} onChange={setUser} />
        {!user ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">选择一位用户</p>
        ) : query.isLoading || !reading ? (
          <Skeleton className="h-40 w-full rounded-xl" />
        ) : !reading.enabled ? (
          <p className="text-xs text-muted-foreground">活动没有开启每日运气。</p>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3">
              <ReadingTile title="干预前" reading={reading.base} z={reading.baseZ} />
              <ReadingTile
                title="此刻"
                reading={reading.current}
                z={reading.z}
                highlight={reading.applied.length > 0 || Boolean(reading.current?.supreme)}
              />
            </div>
            <SupremeReadingNote supreme={reading.current?.supreme ?? undefined} />
            {reading.applied.length ? (
              <div className="space-y-1.5">
                <p className="text-[11px] text-muted-foreground">生效的干预</p>
                <div className="flex flex-wrap gap-1.5">
                  {reading.applied.map((item) => (
                    <Badge key={item.id} variant="outline" size="sm">
                      {item.userId ? "个人" : "全站"} {describeControl(item, overview.spread)}
                    </Badge>
                  ))}
                </div>
              </div>
            ) : null}
            <div className="flex flex-wrap gap-2 border-t border-border pt-3">
              <Button variant="outline" size="sm" disabled={busy} onClick={() => quick({ kind: "reroll" }, "已重抽")}>
                <Dices className="size-3.5" />
                重抽
              </Button>
              <Button variant="outline" size="sm" disabled={busy} onClick={() => quick({ kind: "pin", level: "burst" }, "已设为运气大爆发")}>
                <Flame className="size-3.5" />
                设为运气大爆发
              </Button>
              <Button variant="outline" size="sm" disabled={busy} onClick={() => quick({ kind: "pin", level: "quiet" }, "已设为手气平淡")}>
                <Pin className="size-3.5" />
                设为手气平淡
              </Button>
              <Button variant="ghost" size="sm" disabled={busy || own.length === 0} onClick={revokeOwn}>
                <Undo2 className="size-3.5" />
                撤销他的干预{own.length ? `（${own.length}）` : ""}
              </Button>
            </div>
          </>
        )}
      </div>
    </SectionCard>
  );
}

function ReadingTile({
  title,
  reading,
  z,
  highlight
}: {
  title: string;
  reading?: LotteryFortuneReading | null;
  z: number;
  highlight?: boolean;
}) {
  if (!reading) return null;
  const meta = LEVEL_META[reading.level];
  return (
    <div className={cn("space-y-1.5 rounded-xl border px-3 py-2.5", highlight ? "border-primary/50 bg-primary/5" : "border-border")}>
      <p className="text-[11px] text-muted-foreground">{title}</p>
      <Badge variant={meta.badge} size="sm">
        {meta.label}
      </Badge>
      <p className="font-mono text-lg font-semibold tabular-nums">{formatFactor(reading.factor)}</p>
      <p className="text-[11px] text-muted-foreground">
        超过 {reading.percentile}% 的用户 <span className="font-mono">z {z.toFixed(2)}</span>
      </p>
    </div>
  );
}

// ── 干预记录 ──

function ControlsCard({ appKey, activityId, overview }: { appKey: string; activityId: number; overview: LotteryFortuneOverview }) {
  const [status, setStatus] = useState<LotteryFortuneControlStatus | "all">("active");
  const [kind, setKind] = useState<LotteryFortuneControlKind | "all">("all");
  const [page, setPage] = useState(1);
  const [restoring, setRestoring] = useState(false);
  const query = useLotteryFortuneControlsQuery(appKey, activityId, {
    status: status === "all" ? undefined : status,
    kind: kind === "all" ? undefined : kind,
    page,
    limit: 20
  });
  const revoke = useRevokeLotteryFortuneControlsMutation(appKey, activityId);
  const items = query.data?.items ?? [];
  const totalPages = query.data?.totalPages ?? 0;
  const live = overview.counts.activeGlobal + overview.counts.activeUsers + overview.counts.scheduled;

  const revokeOne = async (control: LotteryFortuneControl) => {
    try {
      await revoke.mutateAsync({ ids: [control.id] });
      toast.success("已撤销");
    } catch (error) {
      toast.error(errorText(error, "撤销失败"));
    }
  };

  const restoreAll = async () => {
    try {
      const result = await revoke.mutateAsync({ all: true });
      toast.success(`已撤销 ${result.revoked} 条，全部恢复自然运气`);
      setRestoring(false);
    } catch (error) {
      toast.error(errorText(error, "撤销失败"));
    }
  };

  return (
    <SectionCard
      icon={<Pin className="size-4" />}
      title="干预记录"
      description="所有干预都留痕；撤销后立刻恢复，记录仍在"
      aside={
        <Button variant="outline" size="sm" disabled={live === 0 || revoke.isPending} onClick={() => setRestoring(true)}>
          <Undo2 className="size-3.5" />
          全部恢复自然运气
        </Button>
      }
    >
      <div className="space-y-3 px-5 py-4">
        <div className="flex flex-wrap gap-2">
          <ToggleGroup
            type="single"
            size="sm"
            variant="outline"
            value={status}
            onValueChange={(value) => {
              if (!value) return;
              setStatus(value as LotteryFortuneControlStatus | "all");
              setPage(1);
            }}
          >
            <ToggleGroupItem value="all">全部</ToggleGroupItem>
            {(Object.keys(STATUS_META) as LotteryFortuneControlStatus[]).map((key) => (
              <ToggleGroupItem key={key} value={key}>
                {STATUS_META[key].label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <ToggleGroup
            type="single"
            size="sm"
            variant="outline"
            value={kind}
            onValueChange={(value) => {
              if (!value) return;
              setKind(value as LotteryFortuneControlKind | "all");
              setPage(1);
            }}
          >
            <ToggleGroupItem value="all">全部方式</ToggleGroupItem>
            {(Object.keys(KIND_META) as LotteryFortuneControlKind[]).map((key) => (
              <ToggleGroupItem key={key} value={key}>
                {KIND_META[key].label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>

        {query.isLoading ? (
          <Skeleton className="h-40 w-full rounded-xl" />
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">暂无干预</p>
        ) : (
          <div className="overflow-x-auto rounded-xl border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>对象</TableHead>
                  <TableHead>干预</TableHead>
                  <TableHead>生效时间</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>操作人</TableHead>
                  <TableHead className="w-20" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="text-xs">
                      <div className="flex flex-col">
                        <span className={cn(!item.userId && "font-medium")}>{targetLabel(item)}</span>
                        {item.userId ? <span className="font-mono text-[10px] text-muted-foreground">UID-{item.userId}</span> : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs">
                      <div className="flex flex-col">
                        <span>{describeControl(item, overview.spread)}</span>
                        {item.note ? <span className="text-[11px] text-muted-foreground">{item.note}</span> : null}
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-[11px] text-muted-foreground">
                      <div className="flex flex-col">
                        <span>{formatStoreDate(item.startsAt)}</span>
                        <span>至 {formatStoreDate(item.endsAt)}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={STATUS_META[item.status].variant} size="sm">
                        {STATUS_META[item.status].label}
                      </Badge>
                      {item.status === "revoked" && item.revokedBy ? (
                        <p className="mt-0.5 text-[10px] text-muted-foreground">{item.revokedBy}</p>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-[11px] text-muted-foreground">{item.operator || "—"}</TableCell>
                    <TableCell>
                      {item.status === "active" || item.status === "scheduled" ? (
                        <Button variant="ghost" size="sm" disabled={revoke.isPending} onClick={() => revokeOne(item)}>
                          撤销
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}

        {totalPages > 1 ? (
          <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
            <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              上一页
            </Button>
            <span className="tabular-nums">
              {page} / {totalPages}
            </span>
            <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
              下一页
            </Button>
          </div>
        ) : null}
      </div>

      <AlertDialog open={restoring} onOpenChange={setRestoring}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>全部恢复自然运气</AlertDialogTitle>
            <AlertDialogDescription>
              撤销这个活动所有生效中与待生效的干预（{live} 条），所有人立刻回到哈希决定的自然运气。记录保留。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void restoreAll();
              }}
              disabled={revoke.isPending}
            >
              {revoke.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              全部撤销
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  );
}
