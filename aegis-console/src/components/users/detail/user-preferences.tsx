"use client";

import { useState, type ComponentType, type ReactNode } from "react";
import { Bell, Braces, CalendarCheck, ChevronRight, Lock, Palette, Settings2, ShieldCheck, SlidersHorizontal } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { AdminAppUserDetail, AdminUserSettingsRecord } from "@/lib/api/types";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { RecordSheet, Section, ToneIcon, type Tone } from "./kit";
import { ValueTree } from "./user-detail-shared";

/**
 * 用户端偏好设置的可读呈现。
 *
 * 设置在库里是每个分类一份 JSON。直接把 JSON 树摆出来，管理员得自己把
 * `profileVisibility: "public"` 翻译成「主页对所有人可见」。这里按分类写好
 * 说法：开关画成开 / 关两种状态，枚举翻成中文，数值带单位；认不出的键
 * 仍然按通用规则显示（驼峰拆词、布尔转开关），不会因为客户端多存了一个字段就丢掉。
 * 每个分类可点开抽屉看版本、启用状态、时间与原始 JSON。
 *
 * 只读：偏好是用户自己的选择，管理端不代为修改（后端也没有管理端写接口）。
 */

type Row = { label: string; value: ReactNode; muted?: boolean };

type CategorySpec = {
  title: string;
  icon: ComponentType<{ className?: string }>;
  tone: Tone;
  /** 按已知键生成行；used 里的键已被消化，不再进通用渲染 */
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

/** 一组可多选项的开关状态：开的实心、关的描边加删除线。 */
function Pills({ items }: { items: Array<{ label: string; on: boolean }> }) {
  return (
    <span className="flex flex-wrap justify-end gap-1">
      {items.map((item) => (
        <span
          key={item.label}
          className={cn(
            "rounded-full border px-2 py-px text-xs",
            item.on ? "border-transparent bg-foreground/[0.07] text-foreground" : "text-muted-foreground line-through decoration-muted-foreground/40"
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
    tone: "primary",
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
    tone: "success",
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
    tone: "info",
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
    tone: "violet",
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
    tone: "warning",
    rows: (s) => ({
      used: ["theme", "language", "timezone", "dateFormat", "timeFormat"],
      rows: [
        { label: "主题", value: THEME[str(s.theme)] ?? (str(s.theme) || "默认") },
        { label: "语言", value: LANGUAGE[str(s.language)] ?? (str(s.language) || "默认") },
        { label: "时区", value: str(s.timezone) || "默认" },
        {
          label: "日期时间",
          value:
            [str(s.dateFormat), str(s.timeFormat) === "12h" ? "12 小时制" : str(s.timeFormat) === "24h" ? "24 小时制" : str(s.timeFormat)]
              .filter(Boolean)
              .join("，") || "默认"
        }
      ]
    })
  },
  security: {
    title: "安全提醒",
    icon: ShieldCheck,
    tone: "danger",
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

/** 认不出的值：布尔画开关、数字千分位、链接可点、时间本地化，其余收进折叠的树。 */
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
    if (/^\d{4}-\d{2}-\d{2}T/.test(v) && !Number.isNaN(Date.parse(v))) return <span title={v}>{formatDateTime(v)}</span>;
    return <span className="break-all">{v}</span>;
  }
  if (value === null || value === undefined) return <span className="text-muted-foreground">空</span>;
  if (Array.isArray(value) && value.every((item) => ["string", "number"].includes(typeof item))) {
    return value.length ? value.join("、") : <span className="text-muted-foreground">无</span>;
  }
  return (
    <details className="text-left">
      <summary className="cursor-pointer list-none text-xs text-muted-foreground select-none hover:text-foreground">
        {Array.isArray(value) ? `${value.length} 项，展开` : `${Object.keys(value as object).length} 个字段，展开`}
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

/** 「3 天前更新」；超过 30 天 formatRelative 给日期，补个空格。 */
export function updatedText(stamp?: string | null) {
  const rel = formatRelative(stamp, "");
  if (!rel) return "";
  return /[前后刚]$|^刚刚$/.test(rel) ? `${rel}更新` : `${rel} 更新`;
}

function categoryTitle(category: string) {
  return SPECS[category]?.title ?? humanizeKey(category);
}

export function UserPreferences({ user }: { user: AdminAppUserDetail }) {
  const categories = collectCategories(user);
  const records = user.settings?.settings ?? {};
  const configured = categories.filter((c) => records[c]).length;
  const [open, setOpen] = useState<string | null>(null);
  const [rawAll, setRawAll] = useState(false);

  const openRecord = open ? (records[open] as AdminUserSettingsRecord | undefined) : undefined;
  const openInfo = open ? user.settings?.recordInfo?.[open] : undefined;

  return (
    <Section
      title="偏好设置"
      icon={Settings2}
      tone="violet"
      description={
        configured
          ? `用户在客户端里的选择，已保存 ${configured} 类，其余按默认值工作。管理端只读。`
          : "用户尚未保存过任何偏好，客户端全部按默认值工作。管理端只读。"
      }
      actions={
        configured ? (
          <Button size="sm" variant="outline" onClick={() => setRawAll(true)}>
            <Braces />
            原始数据
          </Button>
        ) : null
      }
    >
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {categories.map((category) => (
          <PreferenceCard
            key={category}
            category={category}
            record={records[category] as AdminUserSettingsRecord | undefined}
            updatedAt={user.settings?.recordInfo?.[category]?.updatedAt}
            onOpen={() => setOpen(category)}
          />
        ))}
      </div>

      <RecordSheet
        open={open !== null}
        onOpenChange={(next) => !next && setOpen(null)}
        title={open ? `${categoryTitle(open)}偏好` : ""}
        description={open ? `分类键 ${open}` : undefined}
        fields={
          open
            ? [
                { label: "状态", value: openRecord ? (openRecord.isActive === false ? "已停用" : "生效中") : "未保存，使用默认值" },
                { label: "版本", value: openRecord?.version ?? openInfo?.version, mono: true },
                { label: "首次保存", value: formatDateTime(openRecord?.createdAt ?? openInfo?.createdAt) },
                {
                  label: "最近更新",
                  value: formatDateTime(openRecord?.updatedAt ?? openInfo?.updatedAt),
                  hint: openRecord?.updatedAt ?? openInfo?.updatedAt ? formatRelative(openRecord?.updatedAt ?? openInfo?.updatedAt) : undefined
                },
                { label: "选项数", value: openRecord ? Object.keys(openRecord.settings ?? {}).length : 0 }
              ]
            : undefined
        }
        raw={openRecord?.settings}
      />

      <RecordSheet
        open={rawAll}
        onOpenChange={setRawAll}
        title="全部偏好原始数据"
        description={`共 ${configured} 类已保存的设置`}
        raw={Object.fromEntries(Object.entries(records).map(([key, record]) => [key, (record as AdminUserSettingsRecord)?.settings ?? {}]))}
      />
    </Section>
  );
}

function PreferenceCard({
  category,
  record,
  updatedAt,
  onOpen
}: {
  category: string;
  record?: AdminUserSettingsRecord;
  updatedAt?: string;
  onOpen: () => void;
}) {
  const spec = SPECS[category];
  const settings = record?.settings ?? {};
  const known = spec ? spec.rows(settings) : { rows: [], used: [] };
  const extra = Object.entries(settings).filter(([key]) => !known.used.includes(key));
  const configured = Boolean(record);
  const stamp = record?.updatedAt ?? updatedAt;
  const inactive = configured && record?.isActive === false;

  return (
    <div className="flex min-w-0 flex-col rounded-xl border bg-card">
      <div className="flex items-center justify-between gap-2 px-4 pt-3.5">
        <div className="flex min-w-0 items-center gap-2.5 text-sm font-medium">
          <ToneIcon icon={spec?.icon ?? SlidersHorizontal} tone={configured ? spec?.tone ?? "neutral" : "neutral"} size="sm" />
          <span className="truncate">{categoryTitle(category)}</span>
        </div>
        {inactive ? (
          <Badge variant="warning" size="sm">
            已停用
          </Badge>
        ) : configured ? (
          <Badge variant="success" size="sm">
            已保存
          </Badge>
        ) : (
          <Badge variant="outline" size="sm">
            默认值
          </Badge>
        )}
      </div>
      {configured ? (
        <dl className="flex flex-1 flex-col gap-2 px-4 py-3 text-xs">
          {known.rows.map((row) => (
            <div key={row.label} className="flex items-start justify-between gap-4">
              <dt className="shrink-0 text-muted-foreground">{row.label}</dt>
              <dd className={cn("min-w-0 text-right", row.muted && "text-muted-foreground")}>{row.value}</dd>
            </div>
          ))}
          {extra.map(([key, value]) => (
            <div key={key} className="flex items-start justify-between gap-4">
              <dt className="shrink-0 text-muted-foreground" title={key}>
                {humanizeKey(key)}
              </dt>
              <dd className="min-w-0 text-right">{readableValue(value)}</dd>
            </div>
          ))}
          {known.rows.length === 0 && extra.length === 0 ? <div className="text-muted-foreground">没有保存任何选项</div> : null}
        </dl>
      ) : (
        <p className="flex-1 px-4 py-3 text-xs leading-5 text-muted-foreground">用户还没有改过这一类设置，客户端按默认值工作。</p>
      )}
      <div className="flex items-center justify-between gap-2 border-t px-4 py-1.5 text-xs text-muted-foreground">
        <span className="min-w-0 truncate tabular-nums" title={stamp ? formatDateTime(stamp) : undefined}>
          {configured && stamp ? updatedText(stamp) : "未保存"}
          {configured && record?.version ? `，第 ${record.version} 版` : ""}
        </span>
        <Button size="xs" variant="ghost" onClick={onOpen}>
          详情
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}
