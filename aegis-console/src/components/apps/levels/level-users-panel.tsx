"use client";

import { useState } from "react";
import { ArrowUpCircle, Loader2, Users } from "lucide-react";
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
import type { LevelUser, LevelUserParams } from "@/lib/api/levels";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";
import { useAdjustLevelUserMutation, useLevelSchemeQuery, useLevelUserQuery, useLevelUsersQuery } from "@/lib/level-hooks";

/**
 * 用户等级：按等级、关键字筛选与排序；点开看等级详情（进度、享有的权益、升级记录、达成奖励），
 * 在详情里补判或调级（越过门槛与晋级条件，可选同时把经验补到门槛）。
 */

export function LevelUsersPanel({ appKey, initialLevel }: { appKey: string; initialLevel?: number }) {
  const [level, setLevel] = useState(initialLevel ? String(initialLevel) : "");
  const [sort, setSort] = useState<NonNullable<LevelUserParams["sort"]>>("level");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [opened, setOpened] = useState<number | null>(null);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const schemeQuery = useLevelSchemeQuery(appKey);
  const query = useLevelUsersQuery(appKey, { level: level ? Number(level) : undefined, sort, keyword: debounced || undefined, page, limit });
  const data = query.data;
  const rows = schemeQuery.data?.levels ?? [];
  const colorOf = (value: number) => rows.find((row) => row.level === value)?.color || "#6366f1";

  const columns: Array<Column<LevelUser>> = [
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
          <div className="truncate text-xs font-medium">
            <span className="mr-1 tabular-nums" style={{ color: colorOf(user.level) }}>
              Lv.{user.level}
            </span>
            {user.levelName}
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
      description="没有等级记录的用户按 Lv.1 计；点开可以查看详情与调整等级"
      icon={Users}
      tone="primary"
      flush
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
    </Section>
  );
}

function UserLevelSheet({ appKey, userId, onClose }: { appKey: string; userId: number | null; onClose: () => void }) {
  const query = useLevelUserQuery(appKey, userId);
  const schemeQuery = useLevelSchemeQuery(appKey);
  const adjust = useAdjustLevelUserMutation(appKey);
  const [confirm, setConfirm] = useState<"sync" | "adjust" | null>(null);
  const [target, setTarget] = useState(1);
  const [topUp, setTopUp] = useState(true);
  const [note, setNote] = useState("");
  const detail = query.data;
  const info = detail?.info;
  const Icon = LEVEL_ICON[info?.icon ?? ""];
  const color = info?.color || "#6366f1";
  const rows = schemeQuery.data?.levels ?? [];
  const max = info?.maxLevel ?? rows.length;

  const submit = (payload: { level: number; topUp?: boolean; note?: string }) =>
    userId &&
    adjust.mutate(
      { userId, payload },
      {
        onSuccess: (result) => {
          setConfirm(null);
          setNote("");
          toast.success(
            result.to !== result.from
              ? `已从 Lv.${result.from} 调到 Lv.${result.to}${result.addExperience ? `，补了 ${formatNumber(result.addExperience)} 经验` : ""}`
              : "等级没有变化"
          );
          void query.refetch();
        },
        onError: (error) => toast.error(error instanceof ApiError ? error.message : "调整失败")
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
            <Button
              size="sm"
              onClick={() => {
                setTarget(Math.min((info?.currentLevel ?? 1) + 1, max || 1));
                setConfirm("adjust");
              }}
              disabled={adjust.isPending || (info?.isMaxLevel ?? false)}
            >
              <ArrowUpCircle /> 调整等级
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
        onConfirm={() => submit({ level: 0 })}
      />
      <ConfirmActionDialog
        open={confirm === "adjust"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="调整等级"
        description="把这个人直接调到目标等级（越过经验门槛与晋级条件），经过的每一级的达成奖励照常发放。等级只能调高。"
        confirmLabel="调整"
        pending={adjust.isPending}
        disabled={!target || target <= (info?.currentLevel ?? 0) || target > max}
        note={{ value: note, onChange: setNote, placeholder: "调整原因（写进升级记录）", max: 200 }}
        onConfirm={() => submit({ level: target, topUp, note: note.trim() })}
      >
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <Label className="w-16 shrink-0 text-xs">目标等级</Label>
            <NumberInput value={target} min={1} max={max} onChange={setTarget} suffix={rows.find((row) => row.level === target)?.name ?? ""} />
          </div>
          <div className="flex items-start gap-2.5">
            <Checkbox id="lv-topup" checked={topUp} onCheckedChange={(value) => setTopUp(Boolean(value))} />
            <Label htmlFor="lv-topup" className="grid gap-0.5 text-xs font-normal">
              <span className="font-medium">同时把经验补到目标等级的门槛</span>
              <span className="text-muted-foreground">
                门槛 {formatNumber(rows.find((row) => row.level === target)?.threshold ?? 0)} 经验；不补时等级会高于经验对应的等级，进度条从 0 开始。
              </span>
            </Label>
          </div>
        </div>
      </ConfirmActionDialog>
    </RecordSheet>
  );
}
