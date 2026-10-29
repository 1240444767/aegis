"use client";

import { useState } from "react";
import { ArrowUpCircle, Loader2, Lock, LockOpen, Users, X } from "lucide-react";
import { toast } from "sonner";
import { NumberInput } from "@/components/apps/item-grant/item-grant-fields";
import { EVENT_REASON_LABEL, LEVEL_ICON, REWARD_STATUS_META, formatNumber } from "@/components/apps/levels/level-meta";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  useDebouncedValue,
  type Column
} from "@/components/users/detail/kit";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import {
  LEVEL_BATCH_ADJUST_MAX,
  type LevelAdjustPayload,
  type LevelBatchAdjustResult,
  type LevelRow,
  type LevelUser,
  type LevelUserParams
} from "@/lib/api/levels";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";
import {
  useAdjustLevelUserMutation,
  useBatchAdjustLevelUsersMutation,
  useLevelSchemeQuery,
  useLevelUserQuery,
  useLevelUsersQuery
} from "@/lib/level-hooks";

/**
 * 用户等级：按等级、关键字筛选与排序；勾选多人批量强制升级；点开看等级详情（进度、享有的权益、升级记录、达成奖励），
 * 在详情里补判、强制升级（越过经验门槛与晋级条件）、锁定或解除锁定保底等级。
 */

/** 强制升级的选项（单人与批量共用） */
type ForceOptions = { level: number; topUp: boolean; rewards: boolean; notify: boolean; lock: boolean };

const FORCE_DEFAULTS: ForceOptions = { level: 1, topUp: true, rewards: true, notify: true, lock: false };

function forcePayload(options: ForceOptions, note: string): LevelAdjustPayload {
  return {
    level: options.level,
    topUp: options.topUp,
    lock: options.lock,
    skipRewards: !options.rewards,
    silent: !options.notify,
    note: note.trim()
  };
}

function errorText(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

export function LevelUsersPanel({ appKey, initialLevel }: { appKey: string; initialLevel?: number }) {
  const [level, setLevel] = useState(initialLevel ? String(initialLevel) : "");
  const [sort, setSort] = useState<NonNullable<LevelUserParams["sort"]>>("level");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [opened, setOpened] = useState<number | null>(null);
  const [selected, setSelected] = useState<Map<number, LevelUser>>(() => new Map());
  const [batchOpen, setBatchOpen] = useState(false);
  // 每次打开批量对话框换一个 key，表单回到默认值
  const [batchKey, setBatchKey] = useState(0);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const schemeQuery = useLevelSchemeQuery(appKey);
  const query = useLevelUsersQuery(appKey, { level: level ? Number(level) : undefined, sort, keyword: debounced || undefined, page, limit });
  const data = query.data;
  const rows = schemeQuery.data?.levels ?? [];
  const colorOf = (value: number) => rows.find((row) => row.level === value)?.color || "#6366f1";

  const pageRows = data?.items ?? [];
  const pageChecked = pageRows.length > 0 && pageRows.every((user) => selected.has(user.userId));
  const pagePartial = !pageChecked && pageRows.some((user) => selected.has(user.userId));
  const toggle = (users: LevelUser[], checked: boolean) =>
    setSelected((previous) => {
      const next = new Map(previous);
      for (const user of users) {
        if (checked) next.set(user.userId, user);
        else next.delete(user.userId);
      }
      return next;
    });

  const columns: Array<Column<LevelUser>> = [
    {
      id: "pick",
      header: (
        <Checkbox
          aria-label="选择本页"
          checked={pageChecked ? true : pagePartial ? "indeterminate" : false}
          onCheckedChange={(value) => toggle(pageRows, Boolean(value))}
        />
      ),
      width: "40px",
      cell: (user) => (
        <div onClick={(event) => event.stopPropagation()}>
          <Checkbox aria-label="选择" checked={selected.has(user.userId)} onCheckedChange={(value) => toggle([user], Boolean(value))} />
        </div>
      )
    },
    {
      id: "user",
      header: "用户",
      width: "26%",
      cell: (user) => (
        <div className="min-w-0">
          <div className="truncate text-xs font-medium">{user.nickname || user.account}</div>
          <div className="truncate text-[11px] text-muted-foreground">
            {user.account} #{user.userId}
          </div>
        </div>
      )
    },
    {
      id: "level",
      header: "等级",
      width: "22%",
      cell: (user) => (
        <div className="min-w-0">
          <div className="flex items-center gap-1 truncate text-xs font-medium">
            <span className="tabular-nums" style={{ color: colorOf(user.level) }}>
              Lv.{user.level}
            </span>
            <span className="truncate">{user.levelName}</span>
            {user.lockedLevel ? (
              <span title={`已锁定保底 Lv.${user.lockedLevel}`} className="text-muted-foreground">
                <Lock className="size-3" />
              </span>
            ) : null}
          </div>
          <Progress className="mt-1 h-1" value={user.progress} />
        </div>
      )
    },
    { id: "exp", header: "累计经验", width: "14%", align: "right", cell: (user) => <span className="text-xs tabular-nums">{formatNumber(user.experience)}</span> },
    {
      id: "ups",
      header: "升级次数",
      width: "12%",
      align: "right",
      hideBelow: "md",
      cell: (user) => <span className="text-xs tabular-nums text-muted-foreground">{formatNumber(user.levelUpCount)}</span>
    },
    {
      id: "claimable",
      header: "待领取",
      width: "10%",
      align: "right",
      hideBelow: "md",
      cell: (user) => (user.claimable ? <Badge size="sm" variant="secondary">{user.claimable}</Badge> : <span className="text-xs text-muted-foreground">0</span>)
    },
    {
      id: "time",
      header: "最近升级",
      width: "150px",
      align: "right",
      hideBelow: "sm",
      cell: (user) => <span className="text-xs tabular-nums text-muted-foreground">{user.lastLevelUpAt ? formatDateTime(user.lastLevelUpAt) : "—"}</span>
    }
  ];

  const filtered = Boolean(level || keyword || sort !== "level");
  return (
    <Section
      title="用户等级"
      description="没有等级记录的用户按 Lv.1 计；勾选后可批量强制升级，点开可以查看详情与调整等级"
      icon={Users}
      tone="primary"
      flush
      actions={
        selected.size ? (
          <>
            <Button size="sm" variant="ghost" onClick={() => setSelected(new Map())}>
              <X /> 清除选择
            </Button>
            <Button
              size="sm"
              onClick={() => {
                setBatchKey((key) => key + 1);
                setBatchOpen(true);
              }}
              disabled={selected.size > LEVEL_BATCH_ADJUST_MAX}
            >
              <ArrowUpCircle /> 强制升级 {selected.size} 人
            </Button>
          </>
        ) : null
      }
      toolbar={
        <FilterBar>
          <FilterSelect
            label="等级"
            value={level}
            onChange={(value) => {
              setLevel(value);
              setPage(1);
            }}
            options={rows.map((row) => ({ value: String(row.level), label: `Lv.${row.level} ${row.name}` }))}
            allLabel="全部等级"
          />
          <FilterSelect
            label="排序"
            value={sort === "level" ? "" : sort}
            onChange={(value) => {
              setSort((value || "level") as NonNullable<LevelUserParams["sort"]>);
              setPage(1);
            }}
            options={[
              { value: "experience", label: "经验从高到低" },
              { value: "recent", label: "最近升级" }
            ]}
            allLabel="等级从高到低"
          />
          <SearchField
            value={keyword}
            onChange={(value) => {
              setKeyword(value);
              setPage(1);
            }}
            placeholder="用户 ID / 账号 / 昵称"
          />
          <ResetButton
            visible={filtered}
            onClick={() => {
              setLevel("");
              setSort("level");
              setKeyword("");
              setPage(1);
            }}
          />
        </FilterBar>
      }
      footer={
        data ? (
          <Pager
            page={page}
            limit={limit}
            total={data.total}
            onPage={setPage}
            onLimit={(value) => {
              setLimit(value);
              setPage(1);
            }}
          />
        ) : null
      }
    >
      <RecordTable
        columns={columns}
        rows={data?.items}
        rowKey={(user) => user.userId}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        onRowClick={(user) => setOpened(user.userId)}
        empty={<StateBlock compact title={filtered ? "没有符合条件的用户" : "还没有用户"} />}
      />
      <UserLevelSheet appKey={appKey} userId={opened} onClose={() => setOpened(null)} />
      <BatchForceDialog
        key={batchKey}
        appKey={appKey}
        open={batchOpen}
        onOpenChange={setBatchOpen}
        users={[...selected.values()]}
        rows={rows}
        onDone={() => setSelected(new Map())}
      />
    </Section>
  );
}

/** 强制升级的表单：目标等级与四个选项。 */
function ForceUpgradeFields({
  value,
  onChange,
  rows,
  current
}: {
  value: ForceOptions;
  onChange: (value: ForceOptions) => void;
  rows: LevelRow[];
  /** 单人时的当前等级，用来提示目标要高于它 */
  current?: number;
}) {
  const target = rows.find((row) => row.level === value.level);
  const set = (patch: Partial<ForceOptions>) => onChange({ ...value, ...patch });
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Label className="w-16 shrink-0 text-xs">目标等级</Label>
        <NumberInput value={value.level} min={1} max={rows.length || 1} onChange={(level) => set({ level })} suffix={target?.name ?? ""} />
      </div>
      {current && value.level <= current ? <p className="text-xs text-destructive">目标等级需要高于当前的 Lv.{current}</p> : null}
      <OptionCheck
        id="force-topup"
        checked={value.topUp}
        onChange={(topUp) => set({ topUp })}
        title="同时把经验补到目标等级的门槛"
        hint={`门槛 ${formatNumber(target?.threshold ?? 0)} 经验；不补时等级高于经验对应的等级，进度条从 0 开始。`}
      />
      <OptionCheck
        id="force-rewards"
        checked={value.rewards}
        onChange={(rewards) => set({ rewards })}
        title="发放经过的各级达成奖励"
        hint="不发时这些奖励之后也不会自动补上，需要时在体系里对奖励节点补发。"
      />
      <OptionCheck id="force-notify" checked={value.notify} onChange={(notify) => set({ notify })} title="发送升级通知" hint="升级记录照常写入。" />
      <OptionCheck
        id="force-lock"
        checked={value.lock}
        onChange={(lock) => set({ lock })}
        title="锁定为保底等级"
        hint="「重算并允许降级」不会把用户降到这一级以下；随时可以在用户详情里解除。"
      />
    </div>
  );
}

function OptionCheck({ id, checked, onChange, title, hint }: { id: string; checked: boolean; onChange: (value: boolean) => void; title: string; hint: string }) {
  return (
    <div className="flex items-start gap-2.5">
      <Checkbox id={id} checked={checked} onCheckedChange={(value) => onChange(Boolean(value))} />
      <Label htmlFor={id} className="grid gap-0.5 text-xs font-normal">
        <span className="font-medium">{title}</span>
        <span className="text-muted-foreground">{hint}</span>
      </Label>
    </div>
  );
}

function BatchForceDialog({
  appKey,
  open,
  onOpenChange,
  users,
  rows,
  onDone
}: {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  users: LevelUser[];
  rows: LevelRow[];
  onDone: () => void;
}) {
  const batch = useBatchAdjustLevelUsersMutation(appKey);
  const [options, setOptions] = useState<ForceOptions>(() => ({ ...FORCE_DEFAULTS, level: Math.min(2, rows.length || 1) }));
  const [note, setNote] = useState("");
  const names = users.slice(0, 6).map((user) => user.nickname || user.account);

  const report = (result: LevelBatchAdjustResult) => {
    const unchanged = result.succeeded - result.changed;
    const head = `已强制升级 ${result.changed} 人${unchanged ? `，${unchanged} 人已在目标等级及以上` : ""}`;
    if (!result.failed) {
      toast.success(head);
      return;
    }
    const failures = result.items
      .filter((item) => item.error)
      .slice(0, 3)
      .map((item) => `#${item.userId} ${item.error}`)
      .join("；");
    toast.warning(`${head}，${result.failed} 人失败`, { description: failures });
  };

  return (
    <ConfirmActionDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`强制升级 ${users.length} 人`}
      description="直接升到目标等级，不看经验门槛与晋级条件；已在目标等级及以上的人保持不变。逐人执行，个别失败不影响其他人。"
      confirmLabel="强制升级"
      pending={batch.isPending}
      disabled={!options.level || options.level > rows.length || !users.length}
      note={{ value: note, onChange: setNote, placeholder: "调整原因（写进升级记录）", max: 200 }}
      onConfirm={() =>
        batch.mutate(
          { ...forcePayload(options, note), userIds: users.map((user) => user.userId) },
          {
            onSuccess: (result) => {
              report(result);
              setNote("");
              onOpenChange(false);
              onDone();
            },
            onError: (error) => toast.error(errorText(error, "批量强制升级失败"))
          }
        )
      }
    >
      <div className="space-y-3">
        <p className="text-xs text-muted-foreground">
          {names.join("、")}
          {users.length > names.length ? ` 等 ${users.length} 人` : ""}
        </p>
        <ForceUpgradeFields value={options} onChange={setOptions} rows={rows} />
      </div>
    </ConfirmActionDialog>
  );
}

function UserLevelSheet({ appKey, userId, onClose }: { appKey: string; userId: number | null; onClose: () => void }) {
  const query = useLevelUserQuery(appKey, userId);
  const schemeQuery = useLevelSchemeQuery(appKey);
  const adjust = useAdjustLevelUserMutation(appKey);
  const [confirm, setConfirm] = useState<"sync" | "force" | "lock" | "unlock" | null>(null);
  const [options, setOptions] = useState<ForceOptions>(FORCE_DEFAULTS);
  const [note, setNote] = useState("");
  const detail = query.data;
  const info = detail?.info;
  const Icon = LEVEL_ICON[info?.icon ?? ""];
  const color = info?.color || "#6366f1";
  const rows = schemeQuery.data?.levels ?? [];
  const max = info?.maxLevel ?? rows.length;
  const current = info?.currentLevel ?? 1;
  const locked = detail?.user.lockedLevel ?? 0;

  const submit = (payload: LevelAdjustPayload, done: (from: number, to: number, addExperience: number) => string) =>
    userId &&
    adjust.mutate(
      { userId, payload },
      {
        onSuccess: (result) => {
          setConfirm(null);
          setNote("");
          toast.success(done(result.from, result.to, result.addExperience));
          void query.refetch();
        },
        onError: (error) => toast.error(errorText(error, "调整失败"))
      }
    );

  return (
    <RecordSheet
      open={userId !== null}
      onOpenChange={(open) => !open && onClose()}
      title={detail ? detail.user.nickname || detail.user.account : "用户等级"}
      description={detail ? `${detail.user.account} #${detail.user.userId}` : undefined}
      hero={
        info ? (
          <div className="flex items-center gap-3">
            <span
              className="inline-flex size-11 shrink-0 items-center justify-center rounded-xl text-sm font-semibold [&>svg]:size-5"
              style={{ background: `color-mix(in oklab, ${color} 16%, transparent)`, color }}
            >
              {Icon ? <Icon /> : info.currentLevel}
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-sm font-semibold">
                Lv.{info.currentLevel} {info.currentLevelName}
                <span className="ml-1.5 text-xs font-normal text-muted-foreground">/ {info.maxLevel} 级</span>
              </div>
              <Progress className="mt-1.5 h-1.5" value={info.levelProgress} />
              <div className="mt-1 text-[11px] text-muted-foreground">
                {info.isMaxLevel
                  ? `已满级，累计 ${formatNumber(info.totalExperience)} 经验`
                  : `累计 ${formatNumber(info.totalExperience)} 经验，距 Lv.${info.nextLevel} 还差 ${formatNumber(info.expToNextLevel)}`}
              </div>
            </div>
          </div>
        ) : query.isLoading ? (
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        ) : null
      }
      fields={
        detail && info
          ? [
              { label: "最高到过", value: `Lv.${detail.user.highestLevel}` },
              { label: "升级次数", value: formatNumber(detail.user.levelUpCount) },
              { label: "签到经验倍率", value: `${info.expMultiplier} 倍` },
              { label: "待领取奖励", value: `${info.claimableRewards} 份` },
              { label: "保底锁定", value: locked ? `Lv.${locked}，降级重算不低于此级` : "未锁定", wide: true },
              ...(info.blockedBy ? [{ label: "卡在晋级条件", value: info.blockedBy.hint || info.blockedBy.label || "未满足", wide: true }] : []),
              { label: "享有的权益", value: info.privileges.length ? info.privileges.map((item) => item.label).join("、") : "无", wide: true }
            ]
          : []
      }
      actions={
        detail ? (
          <>
            <Button size="sm" variant="outline" onClick={() => setConfirm("sync")} disabled={adjust.isPending}>
              按当前体系补判
            </Button>
            {locked ? (
              <Button size="sm" variant="outline" onClick={() => setConfirm("unlock")} disabled={adjust.isPending}>
                <LockOpen /> 解除锁定
              </Button>
            ) : (
              <Button size="sm" variant="outline" onClick={() => setConfirm("lock")} disabled={adjust.isPending}>
                <Lock /> 锁定当前等级
              </Button>
            )}
            <Button
              size="sm"
              onClick={() => {
                setOptions({ ...FORCE_DEFAULTS, level: Math.min(current + 1, max || 1) });
                setConfirm("force");
              }}
              disabled={adjust.isPending || (info?.isMaxLevel ?? false)}
            >
              <ArrowUpCircle /> 强制升级
            </Button>
          </>
        ) : null
      }
    >
      {query.error ? <StateBlock kind="error" description={query.error instanceof Error ? query.error.message : "加载失败"} onRetry={() => void query.refetch()} /> : null}
      {detail ? (
        <>
          <div className="space-y-2">
            <div className="text-xs font-medium text-muted-foreground">达成奖励</div>
            {detail.rewards.length ? (
              <div className="divide-y rounded-xl border">
                {detail.rewards.map((reward) => {
                  const meta = REWARD_STATUS_META[reward.status];
                  return (
                    <div key={reward.id} className="flex items-start gap-2 px-3 py-2">
                      <div className="min-w-0 flex-1">
                        <div className="text-xs font-medium">
                          Lv.{reward.level} {reward.label}
                        </div>
                        <div className="text-[11px] leading-4 text-muted-foreground">{reward.itemsText.join("、")}</div>
                      </div>
                      <Badge size="sm" variant={meta.variant}>
                        {meta.label}
                      </Badge>
                    </div>
                  );
                })}
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">还没有达成奖励</p>
            )}
          </div>
          <div className="space-y-2">
            <div className="text-xs font-medium text-muted-foreground">升级记录</div>
            {detail.events.length ? (
              <ol className="relative space-y-2 border-l border-dashed pl-4">
                {detail.events.map((event) => (
                  <li key={event.id} className="relative">
                    <span className="absolute top-1 -left-[23px] size-3.5 rounded-full border-2 border-background bg-primary" />
                    <div className="text-xs font-medium">
                      Lv.{event.fromLevel} → Lv.{event.toLevel} {event.toName}
                      <span className="ml-1.5 rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">{EVENT_REASON_LABEL[event.reason]}</span>
                    </div>
                    <div className="text-[11px] leading-4 text-muted-foreground">
                      {formatDateTime(event.createdAt)}，{formatNumber(event.experience)} 经验
                      {event.operator ? `，${event.operator}` : ""}
                      {event.note ? `：${event.note}` : ""}
                    </div>
                  </li>
                ))}
              </ol>
            ) : (
              <p className="text-xs text-muted-foreground">还没有升级记录</p>
            )}
          </div>
        </>
      ) : null}

      <ConfirmActionDialog
        open={confirm === "sync"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="按当前体系补判"
        description="按已发布的体系重新判定一次：经验与晋级条件都满足的会升级并写入达成奖励；不会降级。"
        confirmLabel="补判"
        pending={adjust.isPending}
        onConfirm={() => submit({ level: 0 }, (from, to) => (to !== from ? `已从 Lv.${from} 升到 Lv.${to}` : "等级没有变化"))}
      />
      <ConfirmActionDialog
        open={confirm === "lock"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={`锁定 Lv.${current}`}
        description="锁定前先按当前体系补判一次。锁定后「重算并允许降级」不会把这个人降到锁定的等级以下；正常升级不受影响。"
        confirmLabel="锁定"
        pending={adjust.isPending}
        onConfirm={() => submit({ level: 0, lock: true }, (_from, to) => `已锁定 Lv.${to}`)}
      />
      <ConfirmActionDialog
        open={confirm === "unlock"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="解除锁定"
        description={`当前等级不变；解除后再「重算并允许降级」，会按经验与晋级条件从头判定，可能低于 Lv.${locked}。`}
        confirmLabel="解除锁定"
        pending={adjust.isPending}
        onConfirm={() => submit({ level: 0, unlock: true }, () => "已解除锁定")}
      />
      <ConfirmActionDialog
        open={confirm === "force"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="强制升级"
        description="直接升到目标等级，不看经验门槛与晋级条件。等级只能调高，经验不扣减。"
        confirmLabel="强制升级"
        pending={adjust.isPending}
        disabled={!options.level || options.level <= current || options.level > max}
        note={{ value: note, onChange: setNote, placeholder: "调整原因（写进升级记录）", max: 200 }}
        onConfirm={() =>
          submit(forcePayload(options, note), (from, to, addExperience) =>
            to !== from ? `已从 Lv.${from} 强制升到 Lv.${to}${addExperience ? `，补了 ${formatNumber(addExperience)} 经验` : ""}` : "等级没有变化"
          )
        }
      >
        <ForceUpgradeFields value={options} onChange={setOptions} rows={rows} current={current} />
      </ConfirmActionDialog>
    </RecordSheet>
  );
}
