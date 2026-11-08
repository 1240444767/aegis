"use client";

import { useState } from "react";
import { Crown, Loader2, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
import { SupremeFields } from "@/components/apps/lottery/lottery-supreme-editor";
import {
  seedSupreme,
  supremePayload,
  validateSupreme,
  type SupremeDraft
} from "@/components/apps/lottery/lottery-supreme-form";
import { formatStoreDate } from "@/components/apps/points-store/points-store-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import type { LotteryFortuneOverview, LotterySettings, LotterySupremeReading } from "@/lib/api/lottery";
import { useLotterySettingsQuery, useSaveLotterySettingsMutation } from "@/lib/lottery-hooks";

/**
 * 至尊加成（应用设置）：永久会员或指定档位的每日运气在自然运气之上再抬一截。
 * 活动策略里单独设置过的活动不跟随这里（列表里标出来源）。规则见 docs/lottery.md「至尊加成」。
 */
export function SupremeCard({ appKey, overview }: { appKey: string; overview: LotteryFortuneOverview }) {
  const query = useLotterySettingsQuery(appKey);
  if (query.isLoading || !query.data) return <Skeleton className="h-48 w-full rounded-2xl" />;
  // 保存后服务端回读的设置作为新的起点（key 变化时重新播种草稿）
  return <SupremeEditorCard key={query.data.updatedAt ?? "new"} appKey={appKey} settings={query.data} overview={overview} />;
}

function SupremeEditorCard({
  appKey,
  settings,
  overview
}: {
  appKey: string;
  settings: LotterySettings;
  overview: LotteryFortuneOverview;
}) {
  const [draft, setDraft] = useState<SupremeDraft>(() => seedSupreme(settings.supreme));
  const [dirty, setDirty] = useState(false);
  const mutation = useSaveLotterySettingsMutation(appKey);
  const issues = validateSupreme(draft);
  const change = (next: SupremeDraft) => {
    setDraft(next);
    setDirty(true);
  };
  const reset = () => {
    setDraft(seedSupreme(settings.supreme));
    setDirty(false);
  };
  const save = async () => {
    try {
      await mutation.mutateAsync(supremePayload(draft));
      setDirty(false);
      toast.success(draft.enabled ? "已保存至尊加成" : "已关闭至尊加成");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const current = settings.activities.find((item) => item.id === overview.activityId);
  const ownSetting = current?.source === "activity";

  return (
    <SectionCard
      icon={<Crown className="size-4" />}
      title="至尊加成"
      description="应用设置：永久会员或指定会员档位的每日运气在自然运气之上再提升，作用在管理员干预之后"
      aside={
        <Badge variant={settings.supreme.enabled ? "success" : "outline"} size="sm">
          {settings.supreme.enabled ? "已开启" : "未开启"}
        </Badge>
      }
    >
      <div className="space-y-4 px-5 py-4">
        {overview.enabled ? (
          <p className="rounded-xl bg-muted px-3 py-2.5 text-[11px] text-muted-foreground">
            当前活动{ownSetting ? "在活动策略里单独设置了至尊加成，不跟随这里" : "跟随应用设置"}；
            {overview.supreme
              ? `此刻生效，拿到加成的人最走运时系数 ×${(overview.supremeHigh ?? overview.high).toFixed(2)}`
              : "此刻没有生效的至尊加成"}
          </p>
        ) : (
          <p className="rounded-xl bg-muted px-3 py-2.5 text-[11px] text-muted-foreground">当前活动没有开启每日运气，至尊加成不生效</p>
        )}

        <SwitchRow
          label="开启至尊加成"
          hint="只作用于开了每日运气、且没有单独设置的活动"
          icon={<Crown className="size-3.5" />}
          checked={draft.enabled}
          onChange={(enabled) => change({ ...draft, enabled })}
        />
        <SupremeFields
          value={draft}
          onChange={change}
          tiers={settings.tiers}
          spread={overview.enabled ? overview.spread : undefined}
        />

        {issues.length ? (
          <ul className="space-y-1 rounded-xl border border-destructive/30 bg-destructive/5 px-3 py-2.5 text-[11px] text-destructive">
            {issues.map((issue) => (
              <li key={issue}>{issue}</li>
            ))}
          </ul>
        ) : null}

        {settings.activities.length ? (
          <div className="space-y-1.5">
            <p className="text-[11px] text-muted-foreground">开了每日运气的活动</p>
            <div className="flex flex-wrap gap-1.5">
              {settings.activities.map((item) => (
                <Badge key={item.id} variant={item.enabled ? "success" : "outline"} size="sm">
                  {item.name} {item.source === "activity" ? "单独设置" : "跟随应用"} {item.enabled ? "加成中" : "不加成"}
                </Badge>
              ))}
            </div>
          </div>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
          <span className="text-[11px] text-muted-foreground">
            {settings.updatedBy ? `${settings.updatedBy} 于 ${formatStoreDate(settings.updatedAt)} 修改` : "尚未保存过"}
          </span>
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={reset} disabled={!dirty || mutation.isPending}>
              <Undo2 className="size-3.5" />
              还原
            </Button>
            <Button size="sm" onClick={save} disabled={!dirty || issues.length > 0 || mutation.isPending}>
              {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              保存
            </Button>
          </div>
        </div>
      </div>
    </SectionCard>
  );
}

/** 单人读数里的至尊加成说明 */
export function SupremeReadingNote({ supreme }: { supreme?: LotterySupremeReading }) {
  if (!supreme) return null;
  return (
    <div className="flex items-start gap-2 rounded-xl border border-amber-500/30 bg-amber-500/5 px-3 py-2.5 text-[11px]">
      <Crown className="mt-0.5 size-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
      <div className="space-y-0.5">
        <p className="font-medium">
          {supreme.title}
          {supreme.tier ? `（${supreme.tier}）` : ""}
        </p>
        <p className="text-muted-foreground">
          {supreme.baseTitle} ×{supreme.baseFactor.toFixed(2)} 提升至 ×{(supreme.baseFactor * supreme.lift).toFixed(2)}，
          幅度 ×{supreme.boost}
          {supreme.floor ? `，保底 ${supreme.floor}` : ""}
          {supreme.cap ? `，上限 ${supreme.cap}` : ""}
        </p>
      </div>
    </div>
  );
}
