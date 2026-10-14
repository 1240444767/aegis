"use client";

import type { ComponentType, ReactNode } from "react";
import { Bell, CalendarCheck, Lock, Palette, Settings2, ShieldCheck, SlidersHorizontal } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { AdminAppUserDetail, AdminUserSettingsRecord } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { ValueTree, formatTime, relativeTime } from "./user-detail-shared";

/**
 * 用户端偏好设置的可读呈现。
 *
 * 设置在库里是每个分类一份 JSON。直接把 JSON 树摆出来，管理员得自己把
 * `profileVisibility: "public"` 翻译成「主页对所有人可见」。这里按分类写好
 * 说法：开关画成开 / 关两种状态，枚举翻成中文，数值带单位；认不出的键
 * 仍然按通用规则显示（驼峰拆词、布尔转开关），不会因为客户端多存了一个字段就丢掉。
 *
 * 只读：偏好是用户自己的选择，管理端不代为修改。
 */

type Row = { label: string; value: ReactNode; muted?: boolean };

type CategorySpec = {
  title: string;
  icon: ComponentType<{ className?: string }>;
  /** 按已知键生成行；返回 null 表示该键已被消化，不再进通用渲染 */
  rows: (s: Record<string, unknown>) => { rows: Row[]; used: string[] };
};

const LANGUAGE: Record<string, string> = {
  "zh-CN": "简体中文",
  "zh-TW": "繁体中文",
  "zh-HK": "繁体中文（香港）",
  "en-US": "English",
  en: "English",
  "ja-JP": "日本語",
  "ko-KR": "한국어"
};

const VISIBILITY: Record<string, string> = {
  public: "所有人可见",
  friends: "仅好友可见",
  private: "仅自己可见"
};

const THEME: Record<string, string> = { auto: "跟随系统", light: "浅色", dark: "深色", system: "跟随系统" };

function bool(value: unknown) {
  return value === true || value === "true";
}

function str(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

export function OnOff({ on, onText = "开启", offText = "关闭" }: { on: boolean; onText?: string; offText?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs", on ? "text-foreground" : "text-muted-foreground")}>
      <span className={cn("size-1.5 rounded-full", on ? "bg-emerald-500" : "bg-muted-foreground/40")} />
      {on ? onText : offText}
    </span>
  );
}

/** 一组可多选项的开关状态：开的实心、关的描边。 */
function Pills({ items }: { items: Array<{ label: string; on: boolean }> }) {
  return (
    <span className="flex flex-wrap justify-end gap-1">
      {items.map((item) => (
        <span
          key={item.label}
          className={cn(
            "rounded-full px-2 py-0.5 text-[11px]",
            item.on ? "bg-foreground/[0.07] text-foreground" : "text-muted-foreground line-through decoration-muted-foreground/40"
          )}
        >
          {item.label}
        </span>
      ))}
    </span>
  );
}

const NOTIFY_TYPES: Record<string, string> = { payment: "支付", system: "系统", security: "安全", promotion: "推广" };
const NOTIFY_METHODS: Record<string, string> = { websocket: "站内实时", email: "邮件", sms: "短信", push: "推送" };

function mapPills(value: unknown, labels: Record<string, string>) {
  if (!value || typeof value !== "object") return null;
  const entries = Object.entries(value as Record<string, unknown>);
  if (!entries.length) return null;
  return <Pills items={entries.map(([key, on]) => ({ label: labels[key] ?? humanizeKey(key), on: bool(on) }))} />;
}

const SPECS: Record<string, CategorySpec> = {
  general: {
    title: "通用",
    icon: SlidersHorizontal,
    rows: (s) => ({
      used: ["language", "displayName", "bio", "profileVisibility"],
      rows: [
        { label: "语言", value: LANGUAGE[str(s.language)] ?? (str(s.language) || "默认") },
        { label: "个人主页", value: VISIBILITY[str(s.profileVisibility)] ?? (str(s.profileVisibility) || "默认") },
        ...(str(s.displayName) ? [{ label: "展示名", value: str(s.displayName) }] : []),
        ...(str(s.bio) ? [{ label: "展示简介", value: <span className="line-clamp-2">{str(s.bio)}</span> }] : [])
      ]
    })
  },
  autoSign: {
    title: "自动签到",
    icon: CalendarCheck,
    rows: (s) => {
      const enabled = bool(s.enabled);
      const time = str(s.time) || "08:00";
      const retries = typeof s.maxRetries === "number" ? s.maxRetries : 0;
      return {
        used: ["enabled", "time", "retryOnFail", "maxRetries", "notifyOnSuccess", "notifyOnFail", "disableLocationTracking"],
        rows: [
          { label: "状态", value: enabled ? `每天 ${time} 自动签到` : "未开启", muted: !enabled },
          { label: "失败重试", value: bool(s.retryOnFail) ? `最多 ${retries} 次` : "不重试", muted: !bool(s.retryOnFail) },
          {
            label: "结果通知",
            value: <Pills items={[{ label: "成功", on: bool(s.notifyOnSuccess) }, { label: "失败", on: bool(s.notifyOnFail) }]} />
          },
          { label: "签到定位", value: <OnOff on={!bool(s.disableLocationTracking)} onText="记录" offText="不记录" /> }
        ]
      };
    }
  },
  notifications: {
    title: "通知",
    icon: Bell,
    rows: (s) => {
      const enabled = bool(s.enabled);
      return {
        used: ["enabled", "types", "methods"],
        rows: [
          { label: "接收通知", value: <OnOff on={enabled} /> },
          ...(enabled
            ? [
                { label: "通知类型", value: mapPills(s.types, NOTIFY_TYPES) ?? "未设置", muted: !s.types },
                { label: "送达渠道", value: mapPills(s.methods, NOTIFY_METHODS) ?? "未设置", muted: !s.methods }
              ]
            : [])
        ]
      };
    }
  },
  privacy: {
    title: "隐私",
    icon: Lock,
    rows: (s) => ({
      used: ["showOnlineStatus", "allowDirectMessage", "shareActivityStatus"],
      rows: [
        { label: "显示在线状态", value: <OnOff on={bool(s.showOnlineStatus)} onText="显示" offText="隐藏" /> },
        { label: "接收私信", value: <OnOff on={bool(s.allowDirectMessage)} onText="允许" offText="不允许" /> },
        { label: "分享动态", value: <OnOff on={bool(s.shareActivityStatus)} onText="分享" offText="不分享" /> }
      ]
    })
  },
  ui: {
    title: "界面",
    icon: Palette,
    rows: (s) => ({
      used: ["theme", "language", "timezone", "dateFormat", "timeFormat"],
      rows: [
        { label: "主题", value: THEME[str(s.theme)] ?? (str(s.theme) || "默认") },
        { label: "语言", value: LANGUAGE[str(s.language)] ?? (str(s.language) || "默认") },
        { label: "时区", value: str(s.timezone) || "默认" },
        {
          label: "日期时间",
          value: [str(s.dateFormat), str(s.timeFormat) === "12h" ? "12 小时制" : str(s.timeFormat) === "24h" ? "24 小时制" : str(s.timeFormat)]
            .filter(Boolean)
            .join("，") || "默认"
        }
      ]
    })
  },
  security: {
    title: "安全提醒",
    icon: ShieldCheck,
    rows: (s) => ({
      used: ["twoFactorEnabled", "loginNotification", "suspiciousActivityAlert", "sessionTimeout"],
      rows: [
        { label: "两步验证偏好", value: <OnOff on={bool(s.twoFactorEnabled)} /> },
        { label: "新登录提醒", value: <OnOff on={bool(s.loginNotification)} /> },
        { label: "异常活动提醒", value: <OnOff on={bool(s.suspiciousActivityAlert)} /> },
        {
          label: "闲置自动退出",
          value: typeof s.sessionTimeout === "number" && s.sessionTimeout > 0 ? `${s.sessionTimeout} 分钟` : "不退出"
        }
      ]
    })
  }
};

/** camelCase / snake_case → 「Camel case」，给认不出的键一个能读的名字。 */
export function humanizeKey(key: string) {
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
    .toLowerCase();
  return words ? words[0].toUpperCase() + words.slice(1) : key;
}

/** 认不出的值：布尔画开关、数字千分位、短字符串原样，其余收进折叠的树。 */
export function readableValue(value: unknown): ReactNode {
  if (typeof value === "boolean") return <OnOff on={value} onText="是" offText="否" />;
  if (typeof value === "number") return <span className="tabular-nums">{value.toLocaleString("zh-CN")}</span>;
  if (typeof value === "string") {
    const v = value.trim();
    if (!v) return <span className="text-muted-foreground">空</span>;
    if (/^https?:\/\//i.test(v)) {
      return (
        <a href={v} target="_blank" rel="noreferrer" className="break-all text-primary underline-offset-2 hover:underline">
          {v}
        </a>
      );
    }
    if (/^\d{4}-\d{2}-\d{2}T/.test(v) && !Number.isNaN(Date.parse(v))) return <span title={v}>{formatTime(v)}</span>;
    return <span className="break-all">{v}</span>;
  }
  if (value === null || value === undefined) return <span className="text-muted-foreground">空</span>;
  if (Array.isArray(value) && value.every((item) => ["string", "number"].includes(typeof item))) {
    return value.length ? value.join("、") : <span className="text-muted-foreground">无</span>;
  }
  return (
    <details className="text-left">
      <summary className="cursor-pointer list-none text-xs text-muted-foreground select-none hover:text-foreground">
        {Array.isArray(value) ? `${value.length} 项` : `${Object.keys(value as object).length} 个字段`}
      </summary>
      <div className="mt-1 rounded-lg bg-muted/40 px-2 py-1.5">
        <ValueTree value={value} />
      </div>
    </details>
  );
}

function collectCategories(user: AdminAppUserDetail) {
  const order = Object.keys(SPECS);
  const set = new Set<string>(order);
  for (const item of user.settings?.categories ?? []) set.add(item);
  for (const item of Object.keys(user.settings?.settings ?? {})) set.add(item);
  return Array.from(set).filter((c) => SPECS[c] || user.settings?.settings?.[c]);
}

export function UserPreferences({ user }: { user: AdminAppUserDetail }) {
  const categories = collectCategories(user);
  const configured = user.settings?.configuredCategories?.length ?? 0;

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2 px-1">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <Settings2 className="size-4 text-muted-foreground" />
          偏好设置
        </h3>
        <span className="text-xs text-muted-foreground">
          用户在客户端里的选择，{configured ? `已保存 ${configured} 类` : "尚未保存过"}
        </span>
      </div>
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {categories.map((category) => (
          <PreferenceCard
            key={category}
            category={category}
            record={user.settings?.settings?.[category] as AdminUserSettingsRecord | undefined}
            updatedAt={user.settings?.recordInfo?.[category]?.updatedAt}
          />
        ))}
      </div>
    </section>
  );
}

function PreferenceCard({ category, record, updatedAt }: { category: string; record?: AdminUserSettingsRecord; updatedAt?: string }) {
  const spec = SPECS[category];
  const Icon = spec?.icon ?? SlidersHorizontal;
  const settings = record?.settings ?? {};
  const known = spec ? spec.rows(settings) : { rows: [], used: [] };
  const extra = Object.entries(settings).filter(([key]) => !known.used.includes(key));
  const configured = Boolean(record);
  const stamp = record?.updatedAt ?? updatedAt;

  return (
    <div className="flex flex-col rounded-2xl border bg-card">
      <div className="flex items-center justify-between gap-2 px-4 pt-3.5">
        <div className="flex items-center gap-2 text-sm font-medium">
          <span className="grid size-7 place-items-center rounded-lg bg-muted text-muted-foreground">
            <Icon className="size-3.5" />
          </span>
          {spec?.title ?? humanizeKey(category)}
        </div>
        {configured ? null : (
          <Badge variant="outline" size="sm">
            默认值
          </Badge>
        )}
      </div>
      {configured ? (
        <dl className="flex-1 space-y-2 px-4 py-3 text-xs">
          {known.rows.map((row) => (
            <div key={row.label} className="flex items-start justify-between gap-4">
              <dt className="shrink-0 text-muted-foreground">{row.label}</dt>
              <dd className={cn("min-w-0 text-right", row.muted && "text-muted-foreground")}>{row.value}</dd>
            </div>
          ))}
          {extra.map(([key, value]) => (
            <div key={key} className="flex items-start justify-between gap-4">
              <dt className="shrink-0 text-muted-foreground">{humanizeKey(key)}</dt>
              <dd className="min-w-0 text-right">{readableValue(value)}</dd>
            </div>
          ))}
          {known.rows.length === 0 && extra.length === 0 ? <div className="text-muted-foreground">没有保存任何选项</div> : null}
        </dl>
      ) : (
        <p className="flex-1 px-4 py-3 text-xs leading-5 text-muted-foreground">
          用户还没有改过这一类设置，客户端按默认值工作。
        </p>
      )}
      <div className="border-t px-4 py-2 text-[11px] text-muted-foreground" title={stamp ? formatTime(stamp) : undefined}>
        {configured && stamp ? `${relativeTime(stamp)}更新` : "未保存"}
      </div>
    </div>
  );
}
