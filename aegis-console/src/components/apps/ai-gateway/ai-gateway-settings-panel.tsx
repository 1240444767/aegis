"use client";

import { useState } from "react";
import { ArrowDown, ArrowUp, Gauge, Layers, Loader2, Percent, Plus, Power, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, NumberField, SectionCard, StatusDot, SwitchRow } from "@/components/apps/app-config-primitives";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useAIGatewayCatalogQuery, useAIGatewaySettingsQuery, useSaveAIGatewaySettingsMutation } from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewaySettings, AIGatewaySourceKey } from "@/lib/api/ai-gateway";
import { formatDateTime } from "@/lib/datetime";

type LevelDraft = { uid: string; level: string; multiplier: string };

/** 数字一律按字符串存（理由同云存储配置）；扣费来源存「全部来源的顺序 + 哪些启用」，取消勾选不丢位置。 */
type Draft = {
  scope: string;
  enabled: boolean;
  allowSessionToken: boolean;
  redactContent: boolean;
  catalogAutoSync: boolean;
  audience: "all" | "vip";
  order: AIGatewaySourceKey[];
  active: AIGatewaySourceKey[];
  priceMultiplier: string;
  vipMultiplier: string;
  levels: LevelDraft[];
  freeDaily: string;
  vipDaily: string;
  vipMonthly: string;
  walletRate: string;
  dailySpendLimit: string;
  rpm: string;
  maxConcurrent: string;
  maxKeysPerUser: string;
  defaultMaxTokens: string;
  timezone: string;
};

let levelSeed = 0;
const DECIMAL_RE = /^\d+(\.\d{1,6})?$/;

function trim(value?: string) {
  if (!value) return "0";
  return value.includes(".") ? value.replace(/0+$/, "").replace(/\.$/, "") : value;
}

function seedDraft(scope: string, settings: AIGatewaySettings | undefined, allSources: AIGatewaySourceKey[]): Draft {
  const active = settings?.sources ?? [];
  return {
    scope,
    enabled: settings?.enabled ?? false,
    allowSessionToken: settings?.allowSessionToken ?? true,
    redactContent: settings?.redactContent ?? false,
    catalogAutoSync: settings?.catalogAutoSync ?? false,
    audience: settings?.audience ?? "all",
    order: [...active, ...allSources.filter((source) => !active.includes(source))],
    active,
    priceMultiplier: trim(settings?.priceMultiplier ?? "1"),
    vipMultiplier: trim(settings?.vipMultiplier ?? "1"),
    levels: (settings?.levelMultipliers ?? []).map((item, index) => ({
      uid: `seed-${index}`,
      level: String(item.level),
      multiplier: trim(item.multiplier)
    })),
    freeDaily: trim(settings?.freeDaily),
    vipDaily: trim(settings?.vipDaily),
    vipMonthly: trim(settings?.vipMonthly),
    walletRate: trim(settings?.walletRate ?? "0.01"),
    dailySpendLimit: trim(settings?.dailySpendLimit),
    rpm: String(settings?.rpm ?? 60),
    maxConcurrent: String(settings?.maxConcurrent ?? 5),
    maxKeysPerUser: String(settings?.maxKeysPerUser ?? 10),
    defaultMaxTokens: String(settings?.defaultMaxTokens ?? 4096),
    timezone: settings?.timezone ?? "Asia/Shanghai"
  };
}

export function AIGatewaySettingsPanel({ appKey }: { appKey: string }) {
  const catalogQuery = useAIGatewayCatalogQuery(appKey);
  const settingsQuery = useAIGatewaySettingsQuery(appKey);
  const saveMutation = useSaveAIGatewaySettingsMutation(appKey);
  const [draft, setDraft] = useState<Draft | null>(null);

  const sources = catalogQuery.data?.sources ?? [];
  const settings = settingsQuery.data;
  const current = draft?.scope === appKey ? draft : seedDraft(appKey, settings, sources.map((item) => item.key));
  const patch = (changes: Partial<Draft>) => setDraft({ ...current, ...changes, scope: appKey });
  const sourceMeta = (key: AIGatewaySourceKey) => sources.find((item) => item.key === key);

  const move = (index: number, delta: number) => {
    const order = [...current.order];
    const target = index + delta;
    if (target < 0 || target >= order.length) return;
    [order[index], order[target]] = [order[target], order[index]];
    patch({ order });
  };
  const toggleSource = (key: AIGatewaySourceKey, on: boolean) =>
    patch({ active: on ? [...current.active, key] : current.active.filter((item) => item !== key) });

  const save = async () => {
    const enabledSources = current.order.filter((key) => current.active.includes(key));
    if (enabledSources.length === 0) {
      toast.error("至少启用一个扣费来源");
      return;
    }
    const decimals: Array<[string, string]> = [
      ["全局倍率", current.priceMultiplier], ["会员倍率", current.vipMultiplier],
      ["每日免费额度", current.freeDaily], ["会员每日额度", current.vipDaily], ["会员每月额度", current.vipMonthly],
      ["余额折算", current.walletRate], ["每日消费上限", current.dailySpendLimit]
    ];
    for (const [label, value] of decimals) {
      if (!DECIMAL_RE.test(value.trim() || "0")) {
        toast.error(`${label}需为非负数，最多 6 位小数`);
        return;
      }
    }
    const levels = new Set<number>();
    for (const item of current.levels) {
      const level = Number(item.level);
      if (!Number.isInteger(level) || level < 1 || levels.has(level) || !DECIMAL_RE.test(item.multiplier.trim())) {
        toast.error(`等级倍率「Lv.${item.level || "?"}」无效或重复`);
        return;
      }
      levels.add(level);
    }
    try {
      await saveMutation.mutateAsync({
        enabled: current.enabled,
        allowSessionToken: current.allowSessionToken,
        redactContent: current.redactContent,
        catalogAutoSync: current.catalogAutoSync,
        audience: current.audience,
        sources: enabledSources,
        priceMultiplier: current.priceMultiplier.trim(),
        vipMultiplier: current.vipMultiplier.trim(),
        levelMultipliers: current.levels.map((item) => ({ level: Number(item.level), multiplier: item.multiplier.trim() })),
        freeDaily: current.freeDaily.trim() || "0",
        vipDaily: current.vipDaily.trim() || "0",
        vipMonthly: current.vipMonthly.trim() || "0",
        walletRate: current.walletRate.trim() || "0",
        dailySpendLimit: current.dailySpendLimit.trim() || "0",
        rpm: Number(current.rpm) || 0,
        maxConcurrent: Number(current.maxConcurrent) || 0,
        maxKeysPerUser: Number(current.maxKeysPerUser) || 0,
        defaultMaxTokens: Number(current.defaultMaxTokens) || 0,
        timezone: current.timezone.trim()
      });
      toast.success("设置已保存");
      setDraft(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  if (settingsQuery.isLoading || catalogQuery.isLoading) {
    return (
      <div className="space-y-4">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-40 w-full rounded-2xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Power className="size-4" />}
        title="开关"
        aside={<StatusDot active={Boolean(settings?.enabled)} labelActive="已开启" labelInactive="未开启" />}
      >
        <div className="space-y-4">
          <SwitchRow label="开启 AI 中转" checked={current.enabled} onChange={(value) => patch({ enabled: value })} />
          <SwitchRow label="允许用登录令牌调用" hint="关闭后只接受用户的 API 密钥"
            checked={current.allowSessionToken} onChange={(value) => patch({ allowSessionToken: value })} />
          <SwitchRow label="调用记录不留内容" hint="开启后调用详情只记参数与长度，不保存提示词与回复"
            checked={current.redactContent} onChange={(value) => patch({ redactContent: value })} />
          <SwitchRow label="每天从模型目录更新" hint="按 models.dev 等公开目录更新对话模型的能力、上下文与名称说明，不改价格"
            checked={current.catalogAutoSync} onChange={(value) => patch({ catalogAutoSync: value })} />
          <FieldGroup label="使用对象">
            <Select value={current.audience} onValueChange={(value) => patch({ audience: value as "all" | "vip" })}>
              <SelectTrigger className="h-8 w-40 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">所有用户</SelectItem>
                <SelectItem value="vip">仅会员</SelectItem>
              </SelectContent>
            </Select>
          </FieldGroup>
        </div>
      </SectionCard>

      <SectionCard icon={<Layers className="size-4" />} title="扣费来源与顺序">
        <div className="space-y-1.5">
          {current.order.map((key, index) => {
            const meta = sourceMeta(key);
            const on = current.active.includes(key);
            return (
              <div key={key} className="flex items-center gap-3 rounded-xl border border-border px-3 py-2">
                <Checkbox checked={on} onCheckedChange={(checked) => toggleSource(key, checked === true)} />
                <div className="min-w-0 flex-1">
                  <div className={on ? "text-xs font-medium" : "text-xs text-muted-foreground"}>{meta?.label ?? key}</div>
                  {meta?.hint ? <div className="text-[11px] text-muted-foreground">{meta.hint}</div> : null}
                </div>
                <Button size="icon" variant="ghost" className="size-7" aria-label="上移" disabled={index === 0}
                  onClick={() => move(index, -1)}>
                  <ArrowUp className="size-3.5" />
                </Button>
                <Button size="icon" variant="ghost" className="size-7" aria-label="下移"
                  disabled={index === current.order.length - 1} onClick={() => move(index, 1)}>
                  <ArrowDown className="size-3.5" />
                </Button>
              </div>
            );
          })}
        </div>
        <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <NumberField label="每日免费额度" unit="点" min={0} value={current.freeDaily}
            onChange={(value) => patch({ freeDaily: value })} />
          <NumberField label="会员每日额度" unit="点" min={0} value={current.vipDaily}
            onChange={(value) => patch({ vipDaily: value })} />
          <NumberField label="会员每月额度" unit="点" min={0} value={current.vipMonthly}
            onChange={(value) => patch({ vipMonthly: value })} />
          <NumberField label="余额折算" unit="元 / 点" min={0} value={current.walletRate}
            disabled={!current.active.includes("wallet")} onChange={(value) => patch({ walletRate: value })} />
        </div>
      </SectionCard>

      <SectionCard
        icon={<Percent className="size-4" />}
        title="价格倍率"
        aside={
          <Button size="sm" variant="outline"
            onClick={() => patch({ levels: [...current.levels, { uid: `level-${++levelSeed}`, level: "", multiplier: "1" }] })}>
            <Plus className="size-3.5" />
            等级倍率
          </Button>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <NumberField label="全局倍率" min={0} value={current.priceMultiplier}
              onChange={(value) => patch({ priceMultiplier: value })} />
            <NumberField label="会员倍率" min={0} value={current.vipMultiplier}
              onChange={(value) => patch({ vipMultiplier: value })} />
          </div>
          {current.levels.map((item) => (
            <div key={item.uid} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] items-center gap-2">
              <Input className="h-8 text-xs" inputMode="numeric" placeholder="等级 ≥" value={item.level}
                onChange={(event) => patch({ levels: current.levels.map((entry) => entry.uid === item.uid ? { ...entry, level: event.target.value } : entry) })} />
              <Input className="h-8 text-xs" inputMode="decimal" placeholder="倍率" value={item.multiplier}
                onChange={(event) => patch({ levels: current.levels.map((entry) => entry.uid === item.uid ? { ...entry, multiplier: event.target.value } : entry) })} />
              <Button size="icon" variant="ghost" className="size-8 text-muted-foreground hover:text-destructive" aria-label="删除"
                onClick={() => patch({ levels: current.levels.filter((entry) => entry.uid !== item.uid) })}>
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          ))}
        </div>
      </SectionCard>

      <SectionCard
        icon={<Gauge className="size-4" />}
        title="限制"
        footer={
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-[11px] text-muted-foreground">
              {settings?.updatedAt ? `上次保存 ${formatDateTime(settings.updatedAt)}` : "尚未保存"}
              {settings?.updatedBy ? ` · ${settings.updatedBy}` : ""}
            </span>
            <Button size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
              保存
            </Button>
          </div>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <NumberField label="每日消费上限" unit="点" min={0} hint="0 不限" value={current.dailySpendLimit}
            onChange={(value) => patch({ dailySpendLimit: value })} />
          <NumberField label="每分钟请求" unit="次" min={0} hint="0 不限" value={current.rpm}
            onChange={(value) => patch({ rpm: value })} />
          <NumberField label="同时进行" unit="个" min={1} value={current.maxConcurrent}
            onChange={(value) => patch({ maxConcurrent: value })} />
          <NumberField label="每人密钥" unit="把" min={0} hint="0 不允许创建" value={current.maxKeysPerUser}
            onChange={(value) => patch({ maxKeysPerUser: value })} />
          <NumberField label="默认输出估算" unit="token" min={1} value={current.defaultMaxTokens}
            onChange={(value) => patch({ defaultMaxTokens: value })} />
          <FieldGroup label="时区">
            <Input className="h-8 font-mono text-xs" value={current.timezone}
              onChange={(event) => patch({ timezone: event.target.value })} />
          </FieldGroup>
        </div>
      </SectionCard>
    </div>
  );
}
