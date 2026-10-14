"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Check, ClipboardPaste, Loader2, Plus, Search, Smartphone, X } from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { CountryFlag } from "@/components/ui/country-flag";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api-client";
import { getAdminAppDevices, getAdminAppUsers, type AdminDeviceItem } from "@/lib/api/apps";
import type { AdminAppUserItem } from "@/lib/api/types";
import { formatRelative } from "@/lib/datetime";
import { useReleasesQuery } from "@/lib/release-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { androidVersionLabel } from "@/lib/package-inspector/inspect";
import { DATA_FONT, parseIdList, parseList } from "./release-shared";

/**
 * 发布受众的可视化选择器：用户、设备、地区、语言、机型、系统版本、来源版本。
 *
 * 名单里存的是 ID，但管理员想的是「张三的手机」「广东的内测群」。选择器把搜索、
 * 已选项与批量粘贴放在一处：搜出来点一下就加入，已选的以头像、设备名、国旗显示，
 * 存着的 ID 打开时按 ID 批量还原；找不到的（用户已删除、设备从未登录）标出来但不丢。
 */

const CHIP = "inline-flex max-w-full items-center gap-1.5 rounded-lg border bg-background py-0.5 pr-1 pl-1 text-xs";

function shortId(id: string) {
  return id.length > 14 ? `${id.slice(0, 6)}…${id.slice(-4)}` : id;
}

function initials(item?: { nickname?: string; account?: string }) {
  return (item?.nickname || item?.account || "?").trim().slice(0, 1).toUpperCase();
}

function PickerFrame({
  icon,
  empty,
  count,
  unit,
  children,
  trigger
}: {
  icon: ReactNode;
  empty: string;
  count: number;
  unit: string;
  children: ReactNode;
  trigger: ReactNode;
}) {
  return (
    <div className="rounded-xl border bg-muted/10">
      <div className="flex min-h-11 flex-wrap items-center gap-1.5 p-2">
        {count === 0 ? (
          <span className="flex items-center gap-2 px-1.5 text-xs text-muted-foreground">
            {icon}
            {empty}
          </span>
        ) : (
          children
        )}
      </div>
      <div className="flex items-center justify-between gap-2 border-t px-2 py-1.5">
        <span className="px-1 text-[11px] text-muted-foreground tabular-nums">{count ? `已选 ${count} ${unit}` : ""}</span>
        {trigger}
      </div>
    </div>
  );
}

function RemoveButton({ onClick, label }: { onClick: () => void; label: string }) {
  return (
    <button type="button" aria-label={label} onClick={onClick} className="grid size-4 shrink-0 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground">
      <X className="size-3" />
    </button>
  );
}

/** 批量粘贴：每行一个或以逗号、空格分隔 */
function PastePanel({ placeholder, parse, onAdd }: { placeholder: string; parse: (text: string) => string[]; onAdd: (items: string[]) => void }) {
  const [text, setText] = useState("");
  const items = parse(text);
  return (
    <div className="space-y-2 p-3">
      <Textarea rows={5} autoFocus className={cn("text-xs", DATA_FONT)} placeholder={placeholder} value={text} onChange={(e) => setText(e.target.value)} />
      <div className="flex items-center justify-between">
        <span className="text-[11px] text-muted-foreground">{items.length ? `识别到 ${items.length} 个` : "每行一个，或以逗号、空格分隔"}</span>
        <Button
          size="sm"
          className="h-7 text-xs"
          disabled={!items.length}
          onClick={() => {
            onAdd(items);
            setText("");
          }}
        >
          添加
        </Button>
      </div>
    </div>
  );
}

function ModeTabs({ mode, onMode }: { mode: "search" | "paste"; onMode: (m: "search" | "paste") => void }) {
  return (
    <div className="flex gap-1 border-b p-1.5">
      {(["search", "paste"] as const).map((m) => (
        <button
          key={m}
          type="button"
          onClick={() => onMode(m)}
          className={cn("flex flex-1 items-center justify-center gap-1.5 rounded-md py-1 text-xs", mode === m ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground")}
        >
          {m === "search" ? <Search className="size-3" /> : <ClipboardPaste className="size-3" />}
          {m === "search" ? "搜索" : "批量粘贴"}
        </button>
      ))}
    </div>
  );
}

function searchError(error: unknown, what: string) {
  if (error instanceof ApiError && error.status === 403) return `当前账号无权查看${what}，可改用批量粘贴`;
  return error ? `${what}加载失败` : null;
}

// ── 用户 ──────────────────────────

export function UserPicker({ appKey, value, onChange, tone }: { appKey: string; value: number[]; onChange: (ids: number[]) => void; tone?: "include" | "exclude" }) {
  const token = useAdminToken();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<"search" | "paste">("search");
  const [keyword, setKeyword] = useState("");
  const query = useDebouncedValue(keyword.trim(), 250);
  const sortedIds = useMemo(() => [...value].sort((a, b) => a - b).join(","), [value]);

  const resolved = useQuery({
    queryKey: ["release-audience-users", token, appKey, sortedIds],
    queryFn: () => getAdminAppUsers(token as string, appKey, { ids: sortedIds, limit: value.length }),
    enabled: Boolean(token && appKey && value.length),
    staleTime: 60_000,
    placeholderData: (prev) => prev
  });
  const search = useQuery({
    queryKey: ["release-audience-user-search", token, appKey, query],
    queryFn: () => getAdminAppUsers(token as string, appKey, query ? { keyword: query, limit: 8 } : { sort: "updatedAt", order: "desc", limit: 8 }),
    enabled: Boolean(token && appKey && open && mode === "search"),
    staleTime: 30_000
  });

  const known = useMemo(() => {
    const map = new Map<number, AdminAppUserItem>();
    for (const item of resolved.data?.items ?? []) map.set(item.id, item);
    for (const item of search.data?.items ?? []) map.set(item.id, item);
    return map;
  }, [resolved.data, search.data]);

  const toggle = (id: number) => onChange(value.includes(id) ? value.filter((v) => v !== id) : [...value, id]);
  const error = searchError(search.error, "用户");

  return (
    <PickerFrame
      icon={<Avatar className="size-5"><AvatarFallback className="text-[10px]">?</AvatarFallback></Avatar>}
      empty="未选择用户"
      count={value.length}
      unit="人"
      trigger={
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button variant="ghost" size="sm" className="h-7 gap-1 text-xs">
              <Plus className="size-3.5" />
              添加用户
            </Button>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-80 p-0">
            <ModeTabs mode={mode} onMode={setMode} />
            {mode === "paste" ? (
              <PastePanel
                placeholder={"10001\n10002, 10003"}
                parse={(text) => parseIdList(text).map(String)}
                onAdd={(ids) => {
                  onChange(Array.from(new Set([...value, ...ids.map(Number)])));
                  setOpen(false);
                }}
              />
            ) : (
              <Command shouldFilter={false}>
                <CommandInput value={keyword} onValueChange={setKeyword} placeholder="账号、昵称、邮箱或 #用户ID" />
                <CommandList className="max-h-72">
                  {search.isFetching && !search.data ? (
                    <div className="flex items-center justify-center gap-2 py-6 text-xs text-muted-foreground">
                      <Loader2 className="size-3.5 animate-spin" />
                      搜索中
                    </div>
                  ) : error ? (
                    <div className="px-3 py-6 text-center text-xs text-muted-foreground">{error}</div>
                  ) : (
                    <>
                      <CommandEmpty>没有找到用户</CommandEmpty>
                      <CommandGroup heading={query ? "搜索结果" : "最近活跃"}>
                        {(search.data?.items ?? []).map((user) => {
                          const selected = value.includes(user.id);
                          return (
                            <CommandItem key={user.id} value={String(user.id)} onSelect={() => toggle(user.id)} className="gap-2.5">
                              <Avatar className="size-7">
                                <AvatarImage src={user.avatar} />
                                <AvatarFallback className="text-[11px]">{initials(user)}</AvatarFallback>
                              </Avatar>
                              <div className="min-w-0 flex-1">
                                <div className="truncate text-sm">{user.nickname || user.account || `用户 ${user.id}`}</div>
                                <div className="truncate text-[11px] text-muted-foreground">
                                  {user.account ? `@${user.account}，` : ""}ID {user.id}
                                </div>
                              </div>
                              {selected ? <Check className="size-4 text-primary" /> : null}
                            </CommandItem>
                          );
                        })}
                      </CommandGroup>
                    </>
                  )}
                </CommandList>
              </Command>
            )}
          </PopoverContent>
        </Popover>
      }
    >
      {value.map((id) => {
        const user = known.get(id);
        const missing = !user && resolved.isFetched && !resolved.isFetching;
        return (
          <Tooltip key={id}>
            <TooltipTrigger asChild>
              <span className={cn(CHIP, tone === "exclude" && "border-red-500/30", missing && "border-dashed border-amber-500/60 text-amber-700 dark:text-amber-300")}>
                {missing ? (
                  <AlertTriangle className="size-3.5 shrink-0" />
                ) : (
                  <Avatar className="size-5">
                    <AvatarImage src={user?.avatar} />
                    <AvatarFallback className="text-[9px]">{initials(user)}</AvatarFallback>
                  </Avatar>
                )}
                <span className="truncate">{user ? user.nickname || user.account || `用户 ${id}` : `#${id}`}</span>
                <RemoveButton label="移除" onClick={() => onChange(value.filter((v) => v !== id))} />
              </span>
            </TooltipTrigger>
            <TooltipContent className="text-xs">{missing ? `用户 ${id} 不存在或已删除` : `${user?.account ? `@${user.account}，` : ""}ID ${id}`}</TooltipContent>
          </Tooltip>
        );
      })}
      {value.length > 1 ? (
        <button type="button" className="ml-1 text-[11px] text-muted-foreground hover:text-foreground" onClick={() => onChange([])}>
          清空
        </button>
      ) : null}
    </PickerFrame>
  );
}

// ── 设备 ──────────────────────────

function DeviceLine({ device, id }: { device?: AdminDeviceItem; id: string }) {
  const info = device?.deviceInfo;
  return (
    <>
      <span className="grid size-5 shrink-0 place-items-center overflow-hidden rounded bg-muted">
        {info?.manufacturerIconUrl ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={info.manufacturerIconUrl} alt="" className="size-3.5 object-contain" />
        ) : (
          <Smartphone className="size-3 text-muted-foreground" />
        )}
      </span>
      <span className="truncate">{info?.name || "未知设备"}</span>
      <span className={cn("shrink-0 text-[10px] text-muted-foreground", DATA_FONT)}>{shortId(id)}</span>
    </>
  );
}

export function DevicePicker({ appKey, value, onChange, tone }: { appKey: string; value: string[]; onChange: (ids: string[]) => void; tone?: "include" | "exclude" }) {
  const token = useAdminToken();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<"search" | "paste">("search");
  const [keyword, setKeyword] = useState("");
  const query = useDebouncedValue(keyword.trim(), 250);
  const sortedIds = useMemo(() => [...value].sort().join(","), [value]);

  const resolved = useQuery({
    queryKey: ["release-audience-devices", token, appKey, sortedIds],
    queryFn: () => getAdminAppDevices(token as string, appKey, { ids: sortedIds }),
    enabled: Boolean(token && appKey && value.length),
    staleTime: 60_000,
    placeholderData: (prev) => prev
  });
  const search = useQuery({
    queryKey: ["release-audience-device-search", token, appKey, query],
    queryFn: () => getAdminAppDevices(token as string, appKey, { keyword: query || undefined, limit: 10 }),
    enabled: Boolean(token && appKey && open && mode === "search"),
    staleTime: 30_000
  });

  const known = useMemo(() => {
    const map = new Map<string, AdminDeviceItem>();
    for (const item of resolved.data?.items ?? []) map.set(item.deviceId, item);
    for (const item of search.data?.items ?? []) map.set(item.deviceId, item);
    return map;
  }, [resolved.data, search.data]);

  const toggle = (id: string) => onChange(value.includes(id) ? value.filter((v) => v !== id) : [...value, id]);
  const error = searchError(search.error, "设备");

  return (
    <PickerFrame
      icon={<Smartphone className="size-4" />}
      empty="未选择设备"
      count={value.length}
      unit="台"
      trigger={
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button variant="ghost" size="sm" className="h-7 gap-1 text-xs">
              <Plus className="size-3.5" />
              添加设备
            </Button>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-96 p-0">
            <ModeTabs mode={mode} onMode={setMode} />
            {mode === "paste" ? (
              <PastePanel
                placeholder="每行一个设备 ID"
                parse={parseList}
                onAdd={(ids) => {
                  onChange(Array.from(new Set([...value, ...ids])));
                  setOpen(false);
                }}
              />
            ) : (
              <Command shouldFilter={false}>
                <CommandInput value={keyword} onValueChange={setKeyword} placeholder="设备 ID、型号，或使用者的账号" />
                <CommandList className="max-h-80">
                  {search.isFetching && !search.data ? (
                    <div className="flex items-center justify-center gap-2 py-6 text-xs text-muted-foreground">
                      <Loader2 className="size-3.5 animate-spin" />
                      搜索中
                    </div>
                  ) : error ? (
                    <div className="px-3 py-6 text-center text-xs text-muted-foreground">{error}</div>
                  ) : (
                    <>
                      <CommandEmpty>没有找到设备（只收录登录过的设备）</CommandEmpty>
                      <CommandGroup heading={query ? "搜索结果" : "最近登录的设备"}>
                        {(search.data?.items ?? []).map((device) => {
                          const selected = value.includes(device.deviceId);
                          const info = device.deviceInfo;
                          return (
                            <CommandItem key={device.deviceId} value={device.deviceId} onSelect={() => toggle(device.deviceId)} className="items-start gap-2.5">
                              <span className="mt-0.5 grid size-8 shrink-0 place-items-center overflow-hidden rounded-lg bg-muted">
                                {info?.deviceImageUrl ? (
                                  // eslint-disable-next-line @next/next/no-img-element
                                  <img src={info.deviceImageUrl} alt="" className="size-7 object-contain" />
                                ) : (
                                  <Smartphone className="size-4 text-muted-foreground" />
                                )}
                              </span>
                              <div className="min-w-0 flex-1">
                                <div className="truncate text-sm">{info?.name || "未知设备"}</div>
                                <div className={cn("truncate text-[11px] text-muted-foreground", DATA_FONT)}>{device.deviceId}</div>
                                <div className="truncate text-[11px] text-muted-foreground">
                                  {device.lastUser ? `${device.lastUser.nickname || device.lastUser.account}${device.users > 1 ? ` 等 ${device.users} 人` : ""}，` : ""}
                                  {formatRelative(device.lastSeenAt)}登录
                                </div>
                              </div>
                              {selected ? <Check className="mt-1 size-4 text-primary" /> : null}
                            </CommandItem>
                          );
                        })}
                      </CommandGroup>
                    </>
                  )}
                </CommandList>
              </Command>
            )}
          </PopoverContent>
        </Popover>
      }
    >
      {value.map((id) => {
        const device = known.get(id);
        const missing = !device && resolved.isFetched && !resolved.isFetching;
        return (
          <Tooltip key={id}>
            <TooltipTrigger asChild>
              <span className={cn(CHIP, tone === "exclude" && "border-red-500/30", missing && "border-dashed border-amber-500/60")}>
                <DeviceLine device={device} id={id} />
                <RemoveButton label="移除" onClick={() => onChange(value.filter((v) => v !== id))} />
              </span>
            </TooltipTrigger>
            <TooltipContent className="max-w-80 text-xs leading-5">
              <div className={cn("break-all", DATA_FONT)}>{id}</div>
              {device?.lastUser ? <div>最近使用者：{device.lastUser.nickname || device.lastUser.account}（ID {device.lastUser.id}）</div> : null}
              {missing ? <div>这台设备没有登录记录，客户端上报相同的设备 ID 时仍会生效</div> : null}
            </TooltipContent>
          </Tooltip>
        );
      })}
      {value.length > 1 ? (
        <button type="button" className="ml-1 text-[11px] text-muted-foreground hover:text-foreground" onClick={() => onChange([])}>
          清空
        </button>
      ) : null}
    </PickerFrame>
  );
}

// ── 地区、语言、机型 ──────────────────────────

const REGION_CODES =
  "CN HK MO TW US JP KR SG MY TH VN ID PH IN GB DE FR IT ES NL BE CH AT SE NO DK FI IE PT PL CZ HU RO GR TR RU UA CA MX BR AR CL CO PE AU NZ AE SA QA KW IL EG ZA NG KE MA BD PK LK NP MM KH LA MN KZ UZ IR IQ JO LB OM BH IS LU MT CY EE LV LT SK SI HR RS BG BY GE AM AZ VE EC UY PY BO CR PA DO PR JM CU GT HN SV NI TT DZ TN LY GH ET TZ UG ZW ZM AO MZ CM CI SN MG MU FJ PG BN MV AF".split(" ");
const PINNED_REGIONS = ["CN", "HK", "MO", "TW", "US", "JP", "KR", "SG"];

function displayName(type: "region" | "language", code: string) {
  try {
    return new Intl.DisplayNames(["zh-CN"], { type }).of(code) ?? code;
  } catch {
    return code;
  }
}

function TokenPicker({
  value,
  onChange,
  placeholder,
  addLabel,
  emptyLabel,
  options,
  renderChip,
  allowCustom,
  normalize
}: {
  value: string[];
  onChange: (v: string[]) => void;
  placeholder: string;
  addLabel: string;
  emptyLabel: string;
  options: Array<{ group: string; items: Array<{ value: string; label: string; icon?: ReactNode; hint?: string }> }>;
  renderChip: (value: string) => ReactNode;
  allowCustom?: boolean;
  /** 把输入整理成可保存的值；不合法时返回空串，不出现「添加」项 */
  normalize?: (v: string) => string;
}) {
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState("");
  const toggle = (v: string) => onChange(value.includes(v) ? value.filter((x) => x !== v) : [...value, v]);
  const custom = (normalize ?? ((v: string) => v.trim()))(keyword);
  const known = options.some((g) => g.items.some((i) => i.value.toLowerCase() === custom.toLowerCase()));

  return (
    <div className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-xl border bg-muted/10 p-1.5">
      {value.map((v) => (
        <span key={v} className={CHIP}>
          {renderChip(v)}
          <RemoveButton label="移除" onClick={() => onChange(value.filter((x) => x !== v))} />
        </span>
      ))}
      {value.length === 0 ? <span className="px-1.5 text-xs text-muted-foreground">{emptyLabel}</span> : null}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button variant="ghost" size="sm" className="ml-auto h-7 gap-1 text-xs">
            <Plus className="size-3.5" />
            {addLabel}
          </Button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-72 p-0">
          <Command>
            <CommandInput value={keyword} onValueChange={setKeyword} placeholder={placeholder} />
            <CommandList className="max-h-72">
              <CommandEmpty>没有匹配项</CommandEmpty>
              {allowCustom && custom && !known ? (
                <CommandGroup heading="自定义">
                  <CommandItem
                    value={`custom-${custom}`}
                    onSelect={() => {
                      if (!value.includes(custom)) onChange([...value, custom]);
                      setKeyword("");
                    }}
                  >
                    <Plus className="size-3.5" />
                    添加「{custom}」
                  </CommandItem>
                </CommandGroup>
              ) : null}
              {options.map((group) => (
                <CommandGroup key={group.group} heading={group.group}>
                  {group.items.map((item) => (
                    <CommandItem key={`${group.group}-${item.value}`} value={`${item.value} ${item.label} ${item.hint ?? ""}`} onSelect={() => toggle(item.value)} className="gap-2">
                      {item.icon}
                      <span className="flex-1 truncate">{item.label}</span>
                      {item.hint ? <span className={cn("text-[11px] text-muted-foreground", DATA_FONT)}>{item.hint}</span> : null}
                      {value.includes(item.value) ? <Check className="size-4 text-primary" /> : null}
                    </CommandItem>
                  ))}
                </CommandGroup>
              ))}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  );
}

export function RegionPicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const options = useMemo(() => {
    const item = (code: string) => ({ value: code, label: displayName("region", code), hint: code, icon: <CountryFlag code={code} size={14} className="rounded-[2px]" /> });
    const rest = REGION_CODES.filter((c) => !PINNED_REGIONS.includes(c))
      .map(item)
      .sort((a, b) => a.label.localeCompare(b.label, "zh-CN"));
    return [
      { group: "常用", items: PINNED_REGIONS.map(item) },
      { group: "全部地区", items: rest }
    ];
  }, []);
  return (
    <TokenPicker
      value={value}
      onChange={onChange}
      placeholder="搜索国家或地区"
      addLabel="添加地区"
      emptyLabel="不限地区"
      options={options}
      allowCustom
      normalize={(v) => (/^[a-z]{2}$/i.test(v.trim()) ? v.trim().toUpperCase() : "")}
      renderChip={(code) => (
        <>
          <CountryFlag code={code} size={14} className="rounded-[2px]" />
          <span className="truncate">{displayName("region", code)}</span>
        </>
      )}
    />
  );
}

const LOCALES = ["zh", "zh-CN", "zh-TW", "zh-HK", "en", "en-US", "en-GB", "ja", "ko", "fr", "de", "es", "pt", "pt-BR", "ru", "ar", "th", "vi", "id", "ms", "it", "tr"];

export function LocalePicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const options = useMemo(
    () => [{ group: "常用语言", items: LOCALES.map((code) => ({ value: code, label: displayName("language", code), hint: code })) }],
    []
  );
  return (
    <TokenPicker
      value={value}
      onChange={onChange}
      placeholder="搜索语言，或输入如 en-AU"
      addLabel="添加语言"
      emptyLabel="不限语言"
      options={options}
      allowCustom
      normalize={(v) => (/^[a-z]{2,3}(-[a-z0-9]{2,8})*$/i.test(v.trim()) ? v.trim() : "")}
      renderChip={(code) => (
        <>
          <span className="truncate">{displayName("language", code)}</span>
          <span className={cn("text-[10px] text-muted-foreground", DATA_FONT)}>{code}</span>
        </>
      )}
    />
  );
}

const MODEL_BRANDS = ["Xiaomi", "Redmi", "HUAWEI", "HONOR", "OPPO", "vivo", "iQOO", "OnePlus", "realme", "Meizu", "Samsung", "Pixel", "Motorola", "Sony", "nubia", "ROG"];

export function ModelPicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const options = useMemo(() => [{ group: "常见品牌与系列", items: MODEL_BRANDS.map((b) => ({ value: b, label: b })) }], []);
  return (
    <TokenPicker
      value={value}
      onChange={onChange}
      placeholder="输入型号关键字，如 Pixel 8"
      addLabel="添加机型"
      emptyLabel="不限机型"
      options={options}
      allowCustom
      renderChip={(v) => <span className="truncate px-0.5">{v}</span>}
    />
  );
}

// ── 系统版本与来源版本 ──────────────────────────

const API_LEVELS = Array.from({ length: 17 }, (_, i) => 21 + i);

function RangeSelect({
  min,
  max,
  onMin,
  onMax,
  options
}: {
  min: string;
  max: string;
  onMin: (v: string) => void;
  onMax: (v: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
  const withCustom = (current: string) =>
    current && !options.some((o) => o.value === current) ? [{ value: current, label: current }, ...options] : options;
  const select = (current: string, onValue: (v: string) => void, placeholder: string) => (
    <Select value={current || "none"} onValueChange={(v) => onValue(v === "none" ? "" : v)}>
      <SelectTrigger className="h-9 w-full min-w-0 text-xs">
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="none" className="text-xs">
          {placeholder}
        </SelectItem>
        {withCustom(current).map((o) => (
          <SelectItem key={o.value} value={o.value} className="text-xs">
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
  return (
    <div className="flex items-center gap-2">
      {select(min, onMin, "最低不限")}
      <span className="shrink-0 text-xs text-muted-foreground">至</span>
      {select(max, onMax, "最高不限")}
    </div>
  );
}

export function ApiLevelRange({ min, max, onMin, onMax }: { min: string; max: string; onMin: (v: string) => void; onMax: (v: string) => void }) {
  const options = API_LEVELS.map((api) => ({ value: String(api), label: androidVersionLabel(api) ?? `API ${api}` })).reverse();
  return <RangeSelect min={min} max={max} onMin={onMin} onMax={onMax} options={options} />;
}

export function SourceVersionRange({
  appKey,
  min,
  max,
  onMin,
  onMax
}: {
  appKey: string;
  min: string;
  max: string;
  onMin: (v: string) => void;
  onMax: (v: string) => void;
}) {
  const releases = useReleasesQuery(appKey, { limit: 100 });
  const options = useMemo(() => {
    const seen = new Set<number>();
    const out: Array<{ value: string; label: string }> = [];
    for (const r of releases.data?.items ?? []) {
      if (!r.versionCode || seen.has(r.versionCode)) continue;
      seen.add(r.versionCode);
      out.push({ value: String(r.versionCode), label: `${r.version}（${r.versionCode}）` });
    }
    return out.sort((a, b) => Number(b.value) - Number(a.value));
  }, [releases.data]);
  return <RangeSelect min={min} max={max} onMin={onMin} onMax={onMax} options={options} />;
}
