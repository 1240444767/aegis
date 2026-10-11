/**
 * 控制台统一的时间处理：一律按浏览器时区显示与录入。
 *
 * 后端返回的都是带偏移的绝对时刻（RFC3339），`new Date()` 解析后交给
 * 不带 `timeZone` 的 `Intl.DateTimeFormat` 即落到浏览器时区。容易出错的是另外两类：
 *
 *  - 截取字符串。`iso.slice(0, 16)`、`toISOString().slice(0, 10)` 拿到的是
 *    服务器偏移或 UTC 的墙上时间，东八区凌晨会差出一天。
 *  - `<input type="datetime-local">` / `type="date"`。它们的值没有时区，
 *    必须经本文件转换成本地墙上时间再回填，提交时再转回绝对时刻。
 */

const LOCALE = "zh-CN";

/** 浏览器时区，如 `Asia/Shanghai`。取不到时返回 `UTC`。 */
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/** 当前浏览器时区相对 UTC 的偏移，如 `UTC+08:00`。 */
export function browserUtcOffset(at: Date = new Date()): string {
  const minutes = -at.getTimezoneOffset();
  const sign = minutes >= 0 ? "+" : "-";
  const abs = Math.abs(minutes);
  return `UTC${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
}

/**
 * 解析后端时间。空值、非法值与 Go 零值（`0001-01-01T00:00:00Z`）都返回 null，
 * 调用方据此显示占位符，而不是一个公元一年的日期。
 */
export function parseInstant(value?: string | number | Date | null): Date | null {
  if (value === null || value === undefined || value === "") return null;
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) return null;
  return date;
}

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(options: Intl.DateTimeFormatOptions) {
  const key = JSON.stringify(options);
  let cached = formatters.get(key);
  if (!cached) {
    cached = new Intl.DateTimeFormat(LOCALE, { hourCycle: "h23", ...options });
    formatters.set(key, cached);
  }
  return cached;
}

const DATE_TIME: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit"
};
const DATE_TIME_MINUTE: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit"
};
const SHORT_DATE_TIME: Intl.DateTimeFormatOptions = { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" };
const DATE_ONLY: Intl.DateTimeFormatOptions = { year: "numeric", month: "2-digit", day: "2-digit" };
const TIME_MINUTE: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit" };
const TIME_SECOND: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", second: "2-digit" };

type Input = string | number | Date | null | undefined;

function format(value: Input, options: Intl.DateTimeFormatOptions, fallback: string) {
  const date = parseInstant(value);
  return date ? formatter(options).format(date) : fallback;
}

/** 2026/10/12 15:04:05 */
export function formatDateTime(value: Input, fallback = "—") {
  return format(value, DATE_TIME, fallback);
}

/** 2026/10/12 15:04 */
export function formatDateTimeMinute(value: Input, fallback = "—") {
  return format(value, DATE_TIME_MINUTE, fallback);
}

/** 10/12 15:04 */
export function formatShortDateTime(value: Input, fallback = "—") {
  return format(value, SHORT_DATE_TIME, fallback);
}

/** 2026/10/12 */
export function formatDate(value: Input, fallback = "—") {
  return format(value, DATE_ONLY, fallback);
}

/** 15:04 */
export function formatClock(value: Input, fallback = "—") {
  return format(value, TIME_MINUTE, fallback);
}

/** 15:04:05 */
export function formatClockSeconds(value: Input, fallback = "—") {
  return format(value, TIME_SECOND, fallback);
}

/** 任意 Intl 选项，按浏览器时区格式化。 */
export function formatWith(value: Input, options: Intl.DateTimeFormatOptions, fallback = "—") {
  return format(value, options, fallback);
}

/** 同一天只给时刻，否则给月日时分。用于流水、动态列表。 */
export function formatCompact(value: Input, fallback = "—", now: Date = new Date()) {
  const date = parseInstant(value);
  if (!date) return fallback;
  return localDateKey(date) === localDateKey(now) ? formatter(TIME_MINUTE).format(date) : formatter(SHORT_DATE_TIME).format(date);
}

const RELATIVE_UNITS: Array<[number, string]> = [
  [60_000, "分钟"],
  [3_600_000, "小时"],
  [86_400_000, "天"]
];

/** 「刚刚」「5 分钟前」「3 天后」；超过 30 天退回日期。 */
export function formatRelative(value: Input, fallback = "—", now: number = Date.now()) {
  const date = parseInstant(value);
  if (!date) return fallback;
  const diff = date.getTime() - now;
  const abs = Math.abs(diff);
  if (abs < 60_000) return diff <= 0 ? "刚刚" : "即将";
  if (abs > 30 * 86_400_000) return formatDate(date, fallback);
  let unit = RELATIVE_UNITS[0]!;
  for (const candidate of RELATIVE_UNITS) {
    if (abs >= candidate[0]) unit = candidate;
  }
  const count = Math.floor(abs / unit[0]);
  return diff < 0 ? `${count} ${unit[1]}前` : `${count} ${unit[1]}后`;
}

function pad(value: number) {
  return String(value).padStart(2, "0");
}

/** 浏览器时区下的日期键 `YYYY-MM-DD`。不要用 `toISOString().slice(0, 10)`，那是 UTC 的日期。 */
export function localDateKey(value: Input = new Date()) {
  const date = parseInstant(value);
  if (!date) return "";
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** 绝对时刻 → `<input type="datetime-local">` 的值（浏览器时区墙上时间）。 */
export function toLocalInputValue(value: Input) {
  const date = parseInstant(value);
  if (!date) return "";
  return `${localDateKey(date)}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** 绝对时刻 → `<input type="date">` 的值（浏览器时区日期）。 */
export function toLocalDateInputValue(value: Input) {
  return localDateKey(value);
}

/**
 * `datetime-local` 的值 → RFC3339 绝对时刻。浏览器按本地时区解释无偏移的
 * `YYYY-MM-DDTHH:mm`，所以这里得到的就是用户眼中的那个时刻。空值返回 undefined。
 */
export function fromLocalInputValue(value?: string | null) {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

/**
 * `<input type="date">` 的值 → 该日在浏览器时区的起点（`edge = "end"` 时为终点）。
 * `new Date("2026-10-12")` 会被当成 UTC 零点，这里手动按本地拼。
 */
export function fromLocalDateValue(value?: string | null, edge: "start" | "end" = "start") {
  if (!value) return undefined;
  const matched = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!matched) return undefined;
  const [, y, m, d] = matched;
  const date =
    edge === "start"
      ? new Date(Number(y), Number(m) - 1, Number(d), 0, 0, 0, 0)
      : new Date(Number(y), Number(m) - 1, Number(d), 23, 59, 59, 999);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

/** 浏览器时区下今天零点的绝对时刻。 */
export function startOfLocalDay(at: Date = new Date()) {
  return new Date(at.getFullYear(), at.getMonth(), at.getDate());
}
