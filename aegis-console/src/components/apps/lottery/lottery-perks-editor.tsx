"use client";

import {
  BadgePercent,
  CalendarCheck,
  Clapperboard,
  Clock,
  Crown,
  Gem,
  Plus,
  ShieldCheck,
  ShieldOff,
  Sparkles,
  Ticket,
  Trash2,
  TrendingUp,
  UserMinus,
  Users,
  type LucideIcon
} from "lucide-react";
import { Field, Stepper, ToggleTile } from "@/components/apps/lottery/editor/editor-fields";
import {
  PERK_AUDIENCES,
  PERK_LIMITS,
  PERK_TEMPLATES,
  blankPerk,
  perkConditionLine,
  perkEffectLines,
  type PerkDraft
} from "@/components/apps/lottery/lottery-perks-form";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { LotteryCatalog, LotteryPerkAudience } from "@/lib/api/lottery";
import { cn } from "@/lib/utils";

/**
 * 人群特权编辑器：一条特权一张卡 —— 人群、今天的条件、效果。
 *
 * 特权只改「这个人眼里的活动」（免费次数、积分价格、保底、中奖权重），客户端照着服务端给的次数抽，
 * 所以老版本 App 不升级就生效。「免看广告」按顺序逐次解锁：轮到的那一次视频抽奖判中时变成 1 次免费，
 * 用掉之后才判下一次，视频次数相应减少。
 */

const AUDIENCE_ICON: Record<LotteryPerkAudience, LucideIcon> = {
  all: Users,
  member: Crown,
  lifetime: Gem,
  non_member: UserMinus,
  non_lifetime: UserMinus,
  ad_free: ShieldOff
};

export function LotteryPerksEditor({
  value,
  onChange,
  catalog,
  context
}: {
  value: PerkDraft[];
  onChange: (perks: PerkDraft[]) => void;
  catalog?: LotteryCatalog | null;
  /** 活动当前的配置：决定哪些效果有意义（没开视频抽奖时免看广告不起作用等） */
  context: { video: boolean; points: boolean; pointsDaily: boolean; pity: boolean };
}) {
  const limits = catalog?.perkLimits ?? PERK_LIMITS;
  const audiences = catalog?.perkAudiences?.length ? catalog.perkAudiences : PERK_AUDIENCES;
  const nextKey = value.reduce((max, perk) => Math.max(max, perk.key), 0) + 1;
  const add = (template?: (typeof PERK_TEMPLATES)[number]) => onChange([...value, blankPerk(nextKey, template)]);
  const update = (key: number, next: Partial<PerkDraft>) =>
    onChange(value.map((perk) => (perk.key === key ? { ...perk, ...next } : perk)));

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <p className="text-[11px] leading-snug text-muted-foreground">
          多条同时满足时叠加：次数相加，免看广告的概率按独立事件合并，折扣取最大，中奖加成相乘（封顶 {limits.winBoost} 倍）
        </p>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" variant="outline" className="h-7 shrink-0" disabled={value.length >= limits.perks}>
              <Plus className="size-3.5" /> 添加特权
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-60">
            <DropdownMenuItem onSelect={() => add()}>空白特权</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuLabel className="text-[10px] text-muted-foreground">常用</DropdownMenuLabel>
            {PERK_TEMPLATES.map((template) => (
              <DropdownMenuItem key={template.name} onSelect={() => add(template)}>
                <span className="truncate">{template.name}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {value.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-3 py-5 text-center text-[11px] leading-relaxed text-muted-foreground">
          按人群给额外免费次数、视频抽奖按概率免看广告、积分折扣、保底提前或中奖加成
          <br />
          例：永久会员看视频抽奖免看广告，今天签到后多 1 次免费
        </p>
      ) : (
        value.map((perk, index) => (
          <PerkCard
            key={perk.key}
            index={index}
            perk={perk}
            audiences={audiences}
            catalog={catalog}
            context={context}
            onChange={(next) => update(perk.key, next)}
            onRemove={() => onChange(value.filter((item) => item.key !== perk.key))}
          />
        ))
      )}
    </div>
  );
}

function PerkCard({
  index,
  perk,
  audiences,
  catalog,
  context,
  onChange,
  onRemove
}: {
  index: number;
  perk: PerkDraft;
  audiences: Array<{ key: LotteryPerkAudience; label: string; hint: string }>;
  catalog?: LotteryCatalog | null;
  context: { video: boolean; points: boolean; pointsDaily: boolean; pity: boolean };
  onChange: (next: Partial<PerkDraft>) => void;
  onRemove: () => void;
}) {
  const limits = catalog?.perkLimits ?? PERK_LIMITS;
  const variables = catalog?.perkVariables ?? [];
  const audience = audiences.find((item) => item.key === perk.audience);
  const effects = perkEffectLines(perk, limits);
  const adSkip = Number(perk.adSkip) || 0;
  const insert = (name: string) => onChange({ when: perk.when ? `${perk.when.trimEnd()} ${name}` : name });

  return (
    <div className={cn("overflow-hidden rounded-2xl border border-border", perk.disabled && "opacity-60")}>
      <div className="flex items-center gap-2 border-b border-border bg-muted/30 px-3 py-2">
        <span className="w-5 shrink-0 text-center font-data text-[11px] text-muted-foreground">{index + 1}</span>
        <Input
          value={perk.name}
          maxLength={limits.name}
          placeholder="特权名称（App 上显示）"
          onChange={(event) => onChange({ name: event.target.value })}
          className="h-8 text-xs"
        />
        <Switch checked={!perk.disabled} onCheckedChange={(checked) => onChange({ disabled: !checked })} aria-label="启用" />
        <Button size="icon" variant="ghost" className="size-8 shrink-0" onClick={onRemove} aria-label="删除特权">
          <Trash2 className="size-3.5" />
        </Button>
      </div>

      <div className="space-y-4 p-3">
        <Field label="人群" aside={audience?.hint}>
          <div role="radiogroup" className="flex flex-wrap gap-1.5">
            {audiences.map((item) => {
              const Icon = AUDIENCE_ICON[item.key] ?? Users;
              const active = item.key === perk.audience;
              return (
                <button
                  key={item.key}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => onChange({ audience: item.key })}
                  className={cn(
                    "flex h-7 items-center gap-1.5 rounded-lg border px-2.5 text-[11px] font-medium transition-colors",
                    active
                      ? "border-foreground/80 bg-foreground text-background"
                      : "border-border bg-card text-muted-foreground hover:text-foreground"
                  )}
                >
                  <Icon className="size-3" />
                  {item.label}
                </button>
              );
            })}
          </div>
        </Field>

        <div className="grid gap-2 sm:grid-cols-2">
          <ToggleTile
            checked={perk.signedToday}
            onChange={(signedToday) => onChange({ signedToday })}
            icon={<CalendarCheck />}
            label="今天签到之后"
            hint="签到当天生效"
          />
          <Field label="今天在线满" aside="0 为不要求">
            <Stepper
              value={perk.onlineMinutes}
              min={0}
              max={limits.onlineMinutes}
              step={10}
              suffix="分钟"
              onChange={(onlineMinutes) => onChange({ onlineMinutes })}
            />
          </Field>
        </div>

        <div className="space-y-1">
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-medium">附加条件</span>
            <Popover>
              <PopoverTrigger asChild>
                <Button size="sm" variant="ghost" className="h-6 px-2 text-[11px]">
                  <Sparkles className="size-3" /> 变量
                </Button>
              </PopoverTrigger>
              <PopoverContent align="end" className="w-80 p-0">
                <ScrollArea className="h-72">
                  <div className="p-1">
                    {variables.map((variable) => (
                      <button
                        key={variable.name}
                        type="button"
                        onClick={() => insert(variable.name)}
                        className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left hover:bg-muted"
                      >
                        <span className="font-mono text-[11px]">{variable.name}</span>
                        <span className="truncate text-[10px] text-muted-foreground">{variable.label}</span>
                      </button>
                    ))}
                  </div>
                </ScrollArea>
              </PopoverContent>
            </Popover>
          </div>
          <Textarea
            value={perk.when}
            rows={1}
            spellCheck={false}
            placeholder="留空为不限，例：user.signStreak >= 7 || draw.hour >= 20"
            onChange={(event) => onChange({ when: event.target.value })}
            className="min-h-0 font-mono text-xs"
          />
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <EffectField icon={Ticket} label="额外免费次数" hint="每天">
            <Stepper value={perk.extraFree} min={0} max={limits.extraFree} suffix="次" onChange={(extraFree) => onChange({ extraFree })} />
          </EffectField>
          <EffectField
            icon={Clapperboard}
            label="视频抽奖免看广告"
            hint={context.video ? "逐次判定，轮到时才变成 1 次免费" : "活动未开视频抽奖"}
            muted={!context.video}
          >
            <Stepper value={perk.adSkip} min={0} max={100} step={5} suffix="%" onChange={(next) => onChange({ adSkip: next })} />
          </EffectField>
          {adSkip > 0 ? (
            <EffectField icon={Clock} label="判定次数" hint={`视频不限次时每天前几次参与判定，0 为 ${limits.defaultAdSkipSlots} 次`}>
              <Stepper
                value={perk.adSkipSlots}
                min={0}
                max={limits.adSkipSlots}
                suffix="次"
                onChange={(adSkipSlots) => onChange({ adSkipSlots })}
              />
            </EffectField>
          ) : null}
          <EffectField icon={BadgePercent} label="积分抽奖优惠" hint={context.points ? "省下的百分比" : "活动未开积分抽奖"} muted={!context.points}>
            <Stepper
              value={perk.pointsDiscount}
              min={0}
              max={limits.pointsDiscount}
              step={5}
              suffix="%"
              onChange={(pointsDiscount) => onChange({ pointsDiscount })}
            />
          </EffectField>
          <EffectField
            icon={Plus}
            label="积分抽奖额外次数"
            hint={context.points && context.pointsDaily ? "每天" : "积分抽奖不限次时不起作用"}
            muted={!context.points || !context.pointsDaily}
          >
            <Stepper value={perk.extraPoints} min={0} max={limits.extraPoints} suffix="次" onChange={(extraPoints) => onChange({ extraPoints })} />
          </EffectField>
          <EffectField icon={ShieldCheck} label="保底提前" hint={context.pity ? "最少保留 2 次" : "活动未开保底"} muted={!context.pity}>
            <Stepper value={perk.pityCut} min={0} max={limits.pityCut} suffix="次" onChange={(pityCut) => onChange({ pityCut })} />
          </EffectField>
          <EffectField icon={TrendingUp} label="中奖加成" hint="中奖奖品权重的倍数">
            <Stepper value={perk.winBoost} min={1} max={limits.winBoost} step={0.1} suffix="倍" onChange={(winBoost) => onChange({ winBoost })} />
          </EffectField>
        </div>

        <p
          className={cn(
            "rounded-xl px-3 py-2 text-[11px] leading-relaxed",
            effects.length ? "bg-emerald-500/[0.07] text-emerald-800 dark:text-emerald-300" : "bg-amber-500/[0.08] text-amber-800 dark:text-amber-300"
          )}
        >
          {effects.length
            ? `${perkConditionLine(perk, audience?.label ?? perk.audience)}：${effects.join("，")}`
            : "还没有设置任何效果"}
        </p>
      </div>
    </div>
  );
}

function EffectField({
  icon: Icon,
  label,
  hint,
  muted,
  children
}: {
  icon: LucideIcon;
  label: string;
  hint?: string;
  muted?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Field
      label={
        <span className={cn("flex items-center gap-1.5", muted && "text-muted-foreground")}>
          <Icon className="size-3 text-muted-foreground" />
          {label}
        </span>
      }
      aside={hint}
    >
      {children}
    </Field>
  );
}
