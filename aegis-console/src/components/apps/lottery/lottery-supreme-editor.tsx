"use client";

import { Crown, Layers, Plus, Trash2 } from "lucide-react";
import { NumberField, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  SUPREME_LIMITS,
  supremeRangeHint,
  type SupremeDraft,
  type SupremeMode,
  type SupremeTierDraft
} from "@/components/apps/lottery/lottery-supreme-form";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

export type SupremeTierOption = { id: number; name: string; members?: number; lifetimeMembers?: number };

const MODE_LABEL: Record<SupremeMode, string> = {
  inherit: "跟随应用设置",
  off: "本活动不加成",
  custom: "本活动单独设置"
};

/** 活动策略里的至尊加成来源选择 */
export function SupremeModePicker({ value, onChange }: { value: SupremeMode; onChange: (next: SupremeMode) => void }) {
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size="sm"
      value={value}
      onValueChange={(next) => next && onChange(next as SupremeMode)}
      className="flex-wrap justify-start"
    >
      {(Object.keys(MODE_LABEL) as SupremeMode[]).map((mode) => (
        <ToggleGroupItem key={mode} value={mode} className="h-7 px-2.5 text-[11px]">
          {MODE_LABEL[mode]}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

/**
 * 至尊加成的参数：范围（永久会员 / 按会员档位分级）、幅度、保底、上限、各档位与名称。
 * 只改草稿；载荷与校验在 lottery-supreme-form.ts。spread 用来给出加成之后的系数范围。
 */
export function SupremeFields({
  value,
  onChange,
  tiers,
  spread
}: {
  value: SupremeDraft;
  onChange: (next: SupremeDraft) => void;
  tiers: SupremeTierOption[];
  spread?: number;
}) {
  const patch = (next: Partial<SupremeDraft>) => onChange({ ...value, ...next });
  const patchTier = (key: number, next: Partial<SupremeTierDraft>) =>
    patch({ tiers: value.tiers.map((tier) => (tier.key === key ? { ...tier, ...next } : tier)) });
  const unused = tiers.filter((option) => !value.tiers.some((tier) => tier.tierId === option.id));
  const addTier = () => {
    const key = Math.max(0, ...value.tiers.map((tier) => tier.key)) + 1;
    patch({
      tiers: [...value.tiers, { key, tierId: unused[0]?.id ?? 0, lifetimeOnly: false, boost: "1.5", floor: "", cap: "" }]
    });
  };

  return (
    <div className="space-y-3">
      <div className="space-y-1.5">
        <Label className="text-[11px] font-medium">生效范围</Label>
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          value={value.scope}
          onValueChange={(next) => next && patch({ scope: next as SupremeDraft["scope"] })}
          className="justify-start"
        >
          <ToggleGroupItem value="lifetime" className="h-7 gap-1 px-2.5 text-[11px]">
            <Crown className="size-3" />
            只对永久会员
          </ToggleGroupItem>
          <ToggleGroupItem value="tiers" className="h-7 gap-1 px-2.5 text-[11px]">
            <Layers className="size-3" />
            按会员档位分级
          </ToggleGroupItem>
        </ToggleGroup>
      </div>

      <div className="space-y-3 rounded-xl border border-border p-3">
        <span className="text-[11px] font-medium text-muted-foreground">
          {value.scope === "lifetime" ? "永久会员" : "其余永久会员（不在下列档位时，可不设）"}
        </span>
        <GradeFields
          boost={value.boost}
          floor={value.floor}
          cap={value.cap}
          onChange={(next) => patch(next)}
        />
        <p className="text-[11px] text-muted-foreground">{supremeRangeHint(spread, value.boost, value.floor, value.cap)}</p>
      </div>

      {value.scope === "tiers" ? (
        <div className="space-y-2">
          {value.tiers.map((tier, index) => (
            <div key={tier.key} className="space-y-3 rounded-xl border border-border p-3">
              <div className="flex items-center gap-2">
                <span className="text-[11px] font-medium text-muted-foreground">档位 {index + 1}</span>
                <Select value={tier.tierId ? String(tier.tierId) : ""} onValueChange={(next) => patchTier(tier.key, { tierId: Number(next) })}>
                  <SelectTrigger className="h-7 w-48 text-[12px]">
                    <SelectValue placeholder="选择会员档位" />
                  </SelectTrigger>
                  <SelectContent>
                    {tiers.map((option) => (
                      <SelectItem
                        key={option.id}
                        value={String(option.id)}
                        disabled={option.id !== tier.tierId && value.tiers.some((t) => t.tierId === option.id)}
                      >
                        {option.name}
                        {option.members !== undefined ? `（${option.members} 人）` : ""}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  variant="ghost"
                  size="icon"
                  className="ml-auto size-7 text-muted-foreground"
                  aria-label="删除档位"
                  onClick={() => patch({ tiers: value.tiers.filter((t) => t.key !== tier.key) })}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
              <GradeFields
                boost={tier.boost}
                floor={tier.floor}
                cap={tier.cap}
                onChange={(next) => patchTier(tier.key, next)}
              />
              <SwitchRow
                label="只认永久开通"
                hint="这个档位里的限时会员不加成"
                checked={tier.lifetimeOnly}
                onChange={(checked) => patchTier(tier.key, { lifetimeOnly: checked })}
              />
              <p className="text-[11px] text-muted-foreground">{supremeRangeHint(spread, tier.boost, tier.floor, tier.cap)}</p>
            </div>
          ))}
          <Button
            variant="outline"
            size="sm"
            onClick={addTier}
            disabled={value.tiers.length >= SUPREME_LIMITS.tiers || (tiers.length > 0 && unused.length === 0)}
          >
            <Plus className="size-3.5" />
            添加档位
          </Button>
          {tiers.length === 0 ? <p className="text-[11px] text-muted-foreground">应用还没有会员档位，请先在会员区块里建立</p> : null}
          <p className="text-[11px] text-muted-foreground">同时落在几个档位时，取对他当天最好的那一档</p>
        </div>
      ) : null}

      <div className="space-y-1.5">
        <div className="flex items-baseline justify-between gap-2">
          <Label className="text-[11px] font-medium">展示名称</Label>
          <span className="text-[10px] text-muted-foreground">
            {value.title.trim().length}/{SUPREME_LIMITS.title}
          </span>
        </div>
        <Input
          value={value.title}
          maxLength={SUPREME_LIMITS.title}
          placeholder="至尊加成"
          onChange={(event) => patch({ title: event.target.value })}
          className="h-8 text-[12px]"
        />
      </div>
    </div>
  );
}

function GradeFields({
  boost,
  floor,
  cap,
  onChange
}: {
  boost: string;
  floor: string;
  cap: string;
  onChange: (next: { boost?: string; floor?: string; cap?: string }) => void;
}) {
  return (
    <div className="grid gap-3 sm:grid-cols-3">
      <NumberField label="加成幅度" value={boost} min={1} max={SUPREME_LIMITS.boost} unit="倍" placeholder="不乘" onChange={(next) => onChange({ boost: next })} />
      <NumberField label="保底运气" value={floor} min={0} max={SUPREME_LIMITS.factor} unit="系数，1 为平均" placeholder="不设" onChange={(next) => onChange({ floor: next })} />
      <NumberField label="上限" value={cap} min={1} max={SUPREME_LIMITS.factor} unit="系数" placeholder={String(SUPREME_LIMITS.factor)} onChange={(next) => onChange({ cap: next })} />
    </div>
  );
}
