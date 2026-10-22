"use client";

import { useState } from "react";
import {
  Ban,
  CheckCircle2,
  FlaskConical,
  ListChecks,
  LogIn,
  LogOut,
  Play,
  Plus,
  Replace,
  Trash2,
  UserRound,
  Wand2
} from "lucide-react";
import { toast } from "sonner";
import {
  CLASS_META,
  ClassGlyph,
  classOf,
  classOfPlatform,
  DeviceSlots,
  DIMENSION_LABEL,
  LimitStepper,
  PLATFORM_OPTIONS,
  SOURCE_META
} from "@/components/apps/device-limit/device-limit-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api-client";
import type {
  DeviceDecision,
  DeviceLimitPolicy,
  DeviceLimitSimulateResult,
  LoginDevice,
  SimulateDevice
} from "@/lib/api/device-limit";
import { useSimulateDeviceLimitMutation } from "@/lib/device-limit-hooks";
import { cn } from "@/lib/utils";

type Member = "none" | "vip" | "trial" | "lifetime";

const MEMBER_OPTIONS: Array<{ value: Member; label: string }> = [
  { value: "none", label: "非会员" },
  { value: "vip", label: "会员" },
  { value: "trial", label: "试用" },
  { value: "lifetime", label: "永久" }
];

const NEW_DEVICE = "__new__";

let deviceSeq = 0;
function nextDevice(platform: string, loginMinutesAgo: number): SimulateDevice {
  deviceSeq += 1;
  const label = PLATFORM_OPTIONS.find((item) => item.value === platform)?.label ?? platform;
  return { key: `sim-${deviceSeq}`, name: `${label} ${deviceSeq}`, platform, loginMinutesAgo, idleMinutesAgo: Math.round(loginMinutesAgo / 3) };
}

const PRESETS: Array<{ label: string; devices: () => SimulateDevice[] }> = [
  { label: "2 台手机", devices: () => [nextDevice("android", 4320), nextDevice("ios", 600)] },
  {
    label: "手机 + 电脑 + 网页",
    devices: () => [nextDevice("android", 2880), nextDevice("windows", 1440), nextDevice("web", 120)]
  },
  {
    label: "5 台混合",
    devices: () => [
      nextDevice("ios", 10080),
      nextDevice("android", 4320),
      nextDevice("macos", 2880),
      nextDevice("web", 720),
      nextDevice("android", 60)
    ]
  }
];

/**
 * 模拟：用草稿或已保存的策略试一次登录。可以拿真实用户（真实的会员身份、例外、在线设备与台账），
 * 也可以虚构会员身份与已登录设备。服务端用与登录完全相同的判定函数，不改动任何数据。
 */
export function DeviceLimitSimulatorPanel({
  appKey,
  draft,
  dirty
}: {
  appKey: string;
  draft: DeviceLimitPolicy;
  dirty: boolean;
}) {
  const mutation = useSimulateDeviceLimitMutation(appKey);
  const [mode, setMode] = useState<"virtual" | "real">("virtual");
  const [useDraft, setUseDraft] = useState(true);
  const [user, setUser] = useState<PickedUser | null>(null);
  const [member, setMember] = useState<Member>("none");
  const [devices, setDevices] = useState<SimulateDevice[]>(() => PRESETS[1].devices());
  const [platform, setPlatform] = useState("android");
  const [sameAs, setSameAs] = useState<string>(NEW_DEVICE);
  const [known, setKnown] = useState(false);
  const [newInWindow, setNewInWindow] = useState(0);
  const [canAsk, setCanAsk] = useState(true);
  const result = mutation.data;

  const patchDevice = (index: number, next: Partial<SimulateDevice>) =>
    setDevices((current) => current.map((item, i) => (i === index ? { ...item, ...next } : item)));

  async function run() {
    if (mode === "real" && !user) {
      toast.error("请选择用户");
      return;
    }
    const sameDevice = mode === "virtual" ? devices.find((item) => item.key === sameAs) : undefined;
    try {
      await mutation.mutateAsync({
        policy: dirty && useDraft ? draft : null,
        userId: mode === "real" ? user?.id : undefined,
        member:
          mode === "virtual"
            ? {
                isVip: member !== "none",
                isTrial: member === "trial",
                isLifetime: member === "lifetime",
                features: []
              }
            : undefined,
        devices: mode === "virtual" ? devices : undefined,
        platform: sameDevice?.platform ?? platform,
        deviceKey: sameDevice?.key,
        known,
        newDevicesInWindow: newInWindow,
        canAsk
      });
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "模拟失败");
    }
  }

  return (
    <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,5fr)_minmax(0,6fr)]">
      <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
        <header className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
          <h3 className="flex items-center gap-2 text-sm font-semibold tracking-tight">
            <FlaskConical className="size-4 text-muted-foreground" /> 场景
          </h3>
          <ToggleGroup type="single" size="sm" variant="outline" value={mode} onValueChange={(value) => value && setMode(value as typeof mode)}>
            <ToggleGroupItem value="virtual" className="h-7 px-2.5 text-xs">
              虚构
            </ToggleGroupItem>
            <ToggleGroupItem value="real" className="h-7 px-2.5 text-xs">
              真实用户
            </ToggleGroupItem>
          </ToggleGroup>
        </header>
        <div className="space-y-5 p-4">
          {dirty ? (
            <div className="flex items-center justify-between gap-2 rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-700 ring-1 ring-inset ring-amber-500/20 dark:text-amber-300">
              策略有未保存的改动
              <ToggleGroup type="single" size="sm" value={useDraft ? "draft" : "saved"} onValueChange={(value) => value && setUseDraft(value === "draft")}>
                <ToggleGroupItem value="draft" className="h-6 px-2 text-[11px]">
                  用草稿
                </ToggleGroupItem>
                <ToggleGroupItem value="saved" className="h-6 px-2 text-[11px]">
                  用已保存
                </ToggleGroupItem>
              </ToggleGroup>
            </div>
          ) : null}

          {mode === "real" ? (
            <div className="space-y-1.5">
              <Label className="text-xs">用户</Label>
              <UserPicker appKey={appKey} value={user} onChange={setUser} />
            </div>
          ) : (
            <>
              <div className="space-y-1.5">
                <Label className="text-xs">会员身份</Label>
                <ToggleGroup type="single" size="sm" variant="outline" value={member} onValueChange={(value) => value && setMember(value as Member)}>
                  {MEMBER_OPTIONS.map((item) => (
                    <ToggleGroupItem key={item.value} value={item.value} className="h-8 px-3 text-xs">
                      {item.label}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </div>

              <div className="space-y-2">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Label className="text-xs">已登录设备</Label>
                  <div className="flex flex-wrap gap-1">
                    {PRESETS.map((preset) => (
                      <Button
                        key={preset.label}
                        size="xs"
                        variant="ghost"
                        onClick={() => {
                          setDevices(preset.devices());
                          setSameAs(NEW_DEVICE);
                        }}
                      >
                        {preset.label}
                      </Button>
                    ))}
                  </div>
                </div>
                {devices.length === 0 ? (
                  <p className="rounded-lg border border-dashed border-border py-4 text-center text-xs text-muted-foreground">无</p>
                ) : (
                  <ul className="space-y-1.5">
                    {devices.map((device, index) => (
                      <li key={device.key} className="flex flex-wrap items-center gap-2 rounded-lg bg-muted px-2 py-1.5">
                        <ClassGlyph cls={classOfPlatform(device.platform)} className="size-7" />
                        <Select value={device.platform} onValueChange={(value) => patchDevice(index, { platform: value })}>
                          <SelectTrigger size="sm" className="h-7 w-28 bg-background text-xs">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {PLATFORM_OPTIONS.map((item) => (
                              <SelectItem key={item.value} value={item.value} className="text-xs">
                                {item.label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <Input
                          value={device.name}
                          onChange={(event) => patchDevice(index, { name: event.target.value })}
                          className="h-7 w-24 min-w-0 flex-1 bg-background text-xs"
                        />
                        <AgoInput label="登录" minutes={device.loginMinutesAgo} onChange={(value) => patchDevice(index, { loginMinutesAgo: value })} />
                        <AgoInput label="活动" minutes={device.idleMinutesAgo} onChange={(value) => patchDevice(index, { idleMinutesAgo: value })} />
                        <Button
                          size="icon-xs"
                          variant="ghost"
                          aria-label="移除"
                          onClick={() => {
                            setDevices((current) => current.filter((_, i) => i !== index));
                            if (sameAs === device.key) setSameAs(NEW_DEVICE);
                          }}
                        >
                          <Trash2 className="size-3" />
                        </Button>
                      </li>
                    ))}
                  </ul>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  className="h-7 w-full border-dashed"
                  disabled={devices.length >= 12}
                  onClick={() => setDevices((current) => [...current, nextDevice("android", 30)])}
                >
                  <Plus className="size-3.5" /> 添加设备
                </Button>
              </div>
            </>
          )}

          <div className="space-y-2 rounded-xl border border-border p-3">
            <div className="flex items-center gap-1.5 text-xs font-medium">
              <LogIn className="size-3.5 text-emerald-600 dark:text-emerald-400" /> 正在登录
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {mode === "virtual" ? (
                <Select value={sameAs} onValueChange={setSameAs}>
                  <SelectTrigger size="sm" className="w-40 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NEW_DEVICE} className="text-xs">
                      另一台设备
                    </SelectItem>
                    {devices.map((device) => (
                      <SelectItem key={device.key} value={device.key} className="text-xs">
                        与「{device.name}」相同
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}
              {sameAs === NEW_DEVICE || mode === "real" ? (
                <Select value={platform} onValueChange={setPlatform}>
                  <SelectTrigger size="sm" className="w-32 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PLATFORM_OPTIONS.map((item) => (
                      <SelectItem key={item.value} value={item.value} className="text-xs">
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              <label className="flex items-center justify-between gap-2 rounded-lg bg-muted px-2.5 py-1.5 text-xs">
                以前登录过
                <Switch checked={known} onCheckedChange={setKnown} />
              </label>
              <label className="flex items-center justify-between gap-2 rounded-lg bg-muted px-2.5 py-1.5 text-xs">
                客户端支持选择
                <Switch checked={canAsk} onCheckedChange={setCanAsk} />
              </label>
            </div>
            {mode === "virtual" && draft.newDevice.enabled ? (
              <div className="flex items-center justify-between gap-2 rounded-lg bg-muted px-2.5 py-1.5 text-xs">
                窗口内已有新设备
                <LimitStepper value={newInWindow} onChange={setNewInWindow} zeroLabel="" />
              </div>
            ) : null}
          </div>

          <Button className="w-full" onClick={run} disabled={mutation.isPending}>
            <Play className="size-3.5" />
            {mutation.isPending ? "模拟中" : "运行模拟"}
          </Button>
        </div>
      </section>

      <SimulationResult result={result} />
    </div>
  );
}

function AgoInput({ label, minutes, onChange }: { label: string; minutes: number; onChange: (minutes: number) => void }) {
  const hours = Math.round((minutes / 60) * 10) / 10;
  return (
    <label className="flex items-center gap-1 text-[11px] text-muted-foreground">
      {label}
      <input
        type="number"
        min={0}
        step={0.5}
        value={hours}
        onChange={(event) => onChange(Math.max(0, Math.round(Number(event.target.value || 0) * 60)))}
        className="h-7 w-14 rounded-md border border-input bg-background px-1.5 text-right text-xs tabular-nums text-foreground"
      />
      小时前
    </label>
  );
}

const DECISION_META: Record<
  DeviceDecision["action"],
  { title: string; icon: typeof CheckCircle2; className: string }
> = {
  allow: {
    title: "放行",
    icon: CheckCircle2,
    className: "bg-emerald-500/10 text-emerald-700 ring-emerald-500/25 dark:text-emerald-300"
  },
  kick: {
    title: "放行并下线设备",
    icon: Wand2,
    className: "bg-amber-500/10 text-amber-700 ring-amber-500/25 dark:text-amber-300"
  },
  reject: { title: "拒绝登录", icon: Ban, className: "bg-rose-500/10 text-rose-700 ring-rose-500/25 dark:text-rose-300" },
  ask: { title: "请用户选择", icon: ListChecks, className: "bg-sky-500/10 text-sky-700 ring-sky-500/25 dark:text-sky-300" }
};

function SimulationResult({ result }: { result?: DeviceLimitSimulateResult }) {
  if (!result) {
    return (
      <div className="flex min-h-80 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-border text-center">
        <FlaskConical className="size-6 text-muted-foreground/50" />
        <span className="text-sm text-muted-foreground">暂无结果</span>
      </div>
    );
  }
  const { decision, limits } = result;
  const meta = DECISION_META[decision.action];
  const Icon = meta.icon;
  const victimKeys = new Set(decision.victims.map((item) => item.key));
  const replacedKeys = new Set(decision.replaced.map((item) => item.key));
  const source = SOURCE_META[limits.source] ?? SOURCE_META.default;
  const ordered = [...result.devices].sort((a, b) => Date.parse(a.loginAt) - Date.parse(b.loginAt));
  const after = ordered.length - victimKeys.size - replacedKeys.size + (decision.action === "allow" || decision.action === "kick" ? 1 : 0);

  return (
    <section className="space-y-4">
      <div className={cn("flex items-start gap-3 rounded-2xl px-4 py-3.5 ring-1 ring-inset", meta.className)}>
        <Icon className="mt-0.5 size-5 shrink-0" />
        <div className="min-w-0 space-y-0.5">
          <div className="text-sm font-semibold">
            {meta.title}
            {decision.action === "kick" ? ` ${decision.victims.length} 台` : ""}
          </div>
          {decision.message ? <div className="text-xs opacity-90">{decision.message}</div> : null}
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="rounded-xl border border-border bg-card px-4 py-3">
          <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">生效上限</div>
          <div className="mt-1.5 flex flex-wrap items-center gap-2">
            <Badge variant={source.variant} size="sm">{source.label}</Badge>
            <span className="text-xs text-muted-foreground">{limits.sourceLabel}</span>
          </div>
          <div className="mt-2 flex items-center gap-2">
            <span className="text-lg font-semibold tabular-nums">{limits.unlimited ? "不限" : limits.maxDevices || "不限"}</span>
            {!limits.unlimited && limits.maxDevices > 0 ? <DeviceSlots limit={limits.maxDevices} used={after} size="sm" /> : null}
          </div>
        </div>
        <div className="rounded-xl border border-border bg-card px-4 py-3">
          <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">超出</div>
          {decision.requirements.length === 0 ? (
            <div className="mt-2 text-sm text-muted-foreground">未超出</div>
          ) : (
            <ul className="mt-1.5 space-y-1">
              {decision.requirements.map((req, index) => (
                <li key={index} className="flex items-center justify-between gap-2 text-xs">
                  <span>
                    {req.dimension === "class" && req.class ? CLASS_META[classOf(req.class)].label : DIMENSION_LABEL[req.dimension]}
                    <span className="text-muted-foreground"> 上限 {req.limit}</span>
                  </span>
                  <span className="tabular-nums">
                    {req.current} 台{req.mustRemove ? <span className="text-rose-600 dark:text-rose-400">，需下线 {req.mustRemove}</span> : null}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      <div className="overflow-hidden rounded-2xl border border-border bg-card">
        <div className="border-b border-border px-4 py-2.5 text-sm font-medium">设备</div>
        <ol className="divide-y divide-border">
          {ordered.map((device) => (
            <ResultRow
              key={device.key}
              device={device}
              state={victimKeys.has(device.key) ? "kicked" : replacedKeys.has(device.key) ? "replaced" : "kept"}
            />
          ))}
          <li className="flex items-center gap-3 bg-muted/30 px-4 py-2.5">
            <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-emerald-500/10 text-emerald-600 ring-1 ring-inset ring-emerald-500/20 dark:text-emerald-400">
              <UserRound className="size-4" />
            </span>
            <span className="flex-1 text-xs font-medium">正在登录的设备</span>
            {decision.action === "reject" ? (
              <Badge variant="danger" size="sm" className="gap-1">
                <Ban className="size-3" /> 被拒绝
              </Badge>
            ) : decision.action === "ask" ? (
              <Badge variant="info" size="sm" className="gap-1">
                <ListChecks className="size-3" /> 等待选择
              </Badge>
            ) : (
              <Badge variant="success" size="sm" className="gap-1">
                <LogIn className="size-3" /> 登录
              </Badge>
            )}
          </li>
        </ol>
      </div>
    </section>
  );
}

function ResultRow({ device, state }: { device: LoginDevice; state: "kept" | "kicked" | "replaced" }) {
  const cls = classOf(device.class);
  const name = device.deviceInfo?.name || device.name || device.key;
  return (
    <li className={cn("flex items-center gap-3 px-4 py-2.5", state !== "kept" && "bg-muted/40")}>
      <ClassGlyph cls={cls} className={cn(state !== "kept" && "opacity-50")} />
      <div className="min-w-0 flex-1">
        <div className={cn("truncate text-xs font-medium", state !== "kept" && "text-muted-foreground line-through")}>{name}</div>
        <div className="text-[10px] text-muted-foreground">
          {ago(device.loginAt)}登录，{ago(device.lastActiveAt)}活动
        </div>
      </div>
      {state === "kicked" ? (
        <Badge variant="warning" size="sm" className="gap-1">
          <LogOut className="size-3" /> 下线
        </Badge>
      ) : state === "replaced" ? (
        <Badge variant="secondary" size="sm" className="gap-1">
          <Replace className="size-3" /> 被顶替
        </Badge>
      ) : (
        <span className="text-[11px] text-muted-foreground">保留</span>
      )}
    </li>
  );
}

function ago(iso: string) {
  const minutes = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 60000));
  if (minutes < 60) return `${minutes} 分钟前`;
  if (minutes < 1440) return `${Math.round(minutes / 60)} 小时前`;
  return `${Math.round(minutes / 1440)} 天前`;
}
