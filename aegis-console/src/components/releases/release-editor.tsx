"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import {
  Check,
  ChevronDown,
  Crosshair,
  FileText,
  Layers,
  Link2,
  Loader2,
  Megaphone,
  Package,
  Rocket,
  Sparkles,
  Tag,
  Users
} from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { Release, ReleaseNotesFormat, ReleaseSavePayload, ReleaseTargeting, ReleaseUpdateType, ReleaseVisibility, VersionChannel } from "@/lib/api/types";
import { useAuthStore } from "@/lib/auth-store";
import { inspectPackage, releasePackageInfo, type PackageInfo } from "@/lib/package-inspector/inspect";
import { hashBlob } from "@/lib/package-inspector/sha256";
import { usePublishReleaseMutation, useSaveReleaseMutation } from "@/lib/release-hooks";
import { DIRECT_UPLOAD_MAX_BYTES, isUploadAborted, uploadReleaseAsset } from "@/lib/release-upload";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Slider } from "@/components/ui/slider";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { RELEASE_NOTES_MAX, ReleaseNotesEditor } from "./release-notes-editor";
import { AppIcon, AssetCard, PackageDropzone, computeAssetIssues, type AssetItem } from "./release-assets";
import { ApiLevelRange, DevicePicker, LocalePicker, ModelPicker, RegionPicker, SourceVersionRange, UserPicker } from "./release-audience";
import {
  COMMON_ABIS,
  ChannelDot,
  DATA_FONT,
  RELEASE_PLATFORMS,
  ROLLOUT_PRESETS,
  ReleaseStatusBadge,
  UPDATE_TYPES,
  VISIBILITIES
} from "./release-shared";

/**
 * 版本编辑抽屉。
 *
 * 发版最常见的路径是「拖入安装包 → 写两句更新说明 → 发布」，所以安装包放在最上面：
 * 文件在浏览器里解析，版本名、版本码、平台、最低系统要求随之填好；SHA-256 本地算，
 * 文件直接 PUT 到对象存储（存储不支持时自动经服务器上传）。自动填写的字段带标记，
 * 手动改过之后不再被后续安装包覆盖。
 *
 * 新建一律落在草稿；底部「保存并发布」一步到位。安装包整组提交，带 id 的保留下载计数。
 */

type Form = {
  version: string;
  versionCode: string;
  title: string;
  notesFormat: ReleaseNotesFormat;
  notes: string;
  notesMarkdown: string;
  platform: string;
  minOsVersion: string;
  updateType: ReleaseUpdateType;
  minSupportedCode: string;
  channelId: string;
  visibility: ReleaseVisibility;
  rolloutPct: number;
  testerUserIds: number[];
  testerDeviceIds: string[];
  excludeUserIds: number[];
  excludeDeviceIds: string[];
  regions: string[];
  locales: string[];
  deviceModels: string[];
  abis: string[];
  minOsApi: string;
  maxOsApi: string;
  minSourceCode: string;
  maxSourceCode: string;
};

/** 自动填写过的字段 → 来源文件名；用户手动改过的字段会从这里移除 */
type AutoMap = Partial<Record<keyof Form, string>>;

type Errors = Partial<Record<keyof Form | "assets", string>>;

let rowSeq = 0;
const nextKey = () => `asset-${++rowSeq}`;

function toForm(item?: Release | null): Form {
  const t = item?.targeting ?? {};
  return {
    version: item?.version ?? "",
    versionCode: item?.versionCode ? String(item.versionCode) : "",
    title: item?.title ?? "",
    // 新版本默认用 Markdown 写；已有版本沿用它保存时的格式
    notesFormat: item ? (item.notesFormat ?? "html") : "markdown",
    notes: item?.notes ?? "",
    notesMarkdown: item?.notesMarkdown ?? "",
    platform: item?.platform ?? "android",
    minOsVersion: item?.minOsVersion ?? "",
    updateType: item?.updateType ?? "optional",
    minSupportedCode: item?.minSupportedCode ? String(item.minSupportedCode) : "",
    channelId: item?.channel?.id ? String(item.channel.id) : "none",
    visibility: item?.visibility ?? "public",
    rolloutPct: item?.rolloutPct ?? 100,
    testerUserIds: t.testerUserIds ?? [],
    testerDeviceIds: t.testerDeviceIds ?? [],
    excludeUserIds: t.excludeUserIds ?? [],
    excludeDeviceIds: t.excludeDeviceIds ?? [],
    regions: t.regions ?? [],
    locales: t.locales ?? [],
    deviceModels: t.deviceModels ?? [],
    abis: t.abis ?? [],
    minOsApi: t.minOsVersion ? String(t.minOsVersion) : "",
    maxOsApi: t.maxOsVersion ? String(t.maxOsVersion) : "",
    minSourceCode: t.minSourceVersionCode ? String(t.minSourceVersionCode) : "",
    maxSourceCode: t.maxSourceVersionCode ? String(t.maxSourceVersionCode) : ""
  };
}

function toAssetRows(item?: Release | null): AssetItem[] {
  return (item?.assets ?? []).map((asset) => ({
    key: nextKey(),
    id: asset.id || undefined,
    abi: asset.abi,
    label: asset.label ?? "",
    url: asset.url ?? asset.downloadUrl,
    fileSize: asset.fileSize,
    sha256: asset.sha256 ?? "",
    downloadUrl: asset.downloadUrl,
    phase: "done",
    progress: 1,
    external: !(asset.url ?? "").startsWith("storage://")
  }));
}

const toInt = (value: string) => {
  const n = Number(value.trim());
  return Number.isFinite(n) && n > 0 ? Math.trunc(n) : 0;
};

/** 与保存相关的部分，用于判断有没有未保存的改动 */
function snapshot(form: Form, assets: AssetItem[]) {
  return JSON.stringify({ form, assets: assets.map((a) => [a.id, a.abi, a.label, a.url, a.fileSize, a.sha256]) });
}

const SECTIONS = [
  { id: "assets", label: "安装包", icon: Package },
  { id: "basics", label: "版本信息", icon: Tag },
  { id: "notes", label: "更新说明", icon: FileText },
  { id: "policy", label: "更新策略", icon: Megaphone },
  { id: "audience", label: "受众与灰度", icon: Users },
  { id: "targeting", label: "定向与名单", icon: Crosshair }
] as const;

type SectionId = (typeof SECTIONS)[number]["id"];

export function ReleaseEditor({
  appKey,
  item,
  channels,
  open,
  onOpenChange,
  onSaved
}: {
  appKey: string;
  item?: Release | null;
  channels: VersionChannel[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved?: (release: Release) => void;
}) {
  const token = useAuthStore((s) => s.accessToken);
  const [state, setState] = useState<{ form: Form; auto: AutoMap }>(() => ({ form: toForm(item), auto: {} }));
  const { form, auto } = state;
  const [assets, setAssets] = useState<AssetItem[]>(() => toAssetRows(item));
  const [errors, setErrors] = useState<Errors>({});
  const [confirmClose, setConfirmClose] = useState(false);
  const [active, setActive] = useState<SectionId>("assets");
  const [dragging, setDragging] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const assetsRef = useRef(assets);
  const [initial] = useState(() => snapshot(toForm(item), toAssetRows(item)));

  useEffect(() => {
    assetsRef.current = assets;
  }, [assets]);

  // 卸载时中止所有上传、释放图标占用的 object URL
  useEffect(
    () => () => {
      for (const row of assetsRef.current) {
        row.abort?.abort();
        releasePackageInfo(row.info);
      }
    },
    []
  );

  const save = useSaveReleaseMutation(appKey);
  const publish = usePublishReleaseMutation(appKey);
  const busyRows = assets.filter((a) => a.phase === "parsing" || a.phase === "hashing" || a.phase === "uploading");
  const failedRows = assets.filter((a) => a.phase === "error");
  const saving = save.isPending || publish.isPending;
  const live = item?.effectiveStatus === "published";
  const canPublish = !item || item.status === "draft";
  const dirty = snapshot(form, assets) !== initial;
  const versionCode = toInt(form.versionCode);
  const issues = useMemo(() => computeAssetIssues(assets, versionCode), [assets, versionCode]);
  const primary = assets.find((a) => a.info)?.info;

  const patch = <K extends keyof Form>(key: K, value: Form[K]) => {
    setState((s) => {
      const nextAuto = { ...s.auto };
      delete nextAuto[key];
      return { form: { ...s.form, [key]: value }, auto: nextAuto };
    });
    setErrors((e) => ({ ...e, [key]: undefined }));
  };

  const updateAsset = useCallback((key: string, changes: Partial<AssetItem>) => {
    setAssets((rows) => rows.map((row) => (row.key === key ? { ...row, ...changes } : row)));
  }, []);

  /** 用安装包信息填写空着的、或此前同样是自动填写的字段 */
  const autofill = useCallback((info: PackageInfo) => {
    setState((s) => {
      const form = { ...s.form };
      const auto = { ...s.auto };
      const fill = (key: keyof Form, value?: string) => {
        if (!value) return;
        const current = String(form[key] ?? "");
        if (current && !auto[key]) return;
        (form as Record<string, unknown>)[key] = value;
        auto[key] = info.fileName;
      };
      fill("version", info.versionName);
      fill("versionCode", info.versionCode ? String(info.versionCode) : undefined);
      if (info.kind !== "unknown") fill("platform", info.platform);
      fill("minOsVersion", info.minOsLabel);
      return { form, auto };
    });
    setErrors((e) => ({ ...e, version: undefined, versionCode: undefined, assets: undefined }));
  }, []);

  const processFile = useCallback(
    async (key: string, file: File) => {
      const abort = new AbortController();
      updateAsset(key, { phase: "parsing", progress: 0, error: undefined, note: undefined, abort });
      let info: PackageInfo | undefined;
      try {
        info = await inspectPackage(file);
      } catch (error) {
        updateAsset(key, { note: error instanceof Error ? error.message : "无法解析" });
      }
      if (info) {
        const parsed = info;
        updateAsset(key, { info: parsed, abi: parsed.abi });
        autofill(parsed);
        // 同类型的旧包（已上传完成、不在处理中）由新文件替换
        const replaced = assetsRef.current.find((row) => row.key !== key && row.abi === parsed.abi && row.phase === "done");
        if (replaced) {
          setAssets((rows) => rows.filter((row) => row.key !== replaced.key));
          releasePackageInfo(replaced.info);
          toast.info(`已用 ${file.name} 替换原有的 ${parsed.abi} 安装包`);
        }
      }
      try {
        updateAsset(key, { phase: "hashing", progress: 0 });
        const sha256 = await hashBlob(file, (done, total) => updateAsset(key, { progress: done / total }), abort.signal);
        updateAsset(key, { sha256, phase: "uploading", progress: 0 });
        if (!token) throw new Error("登录状态已失效，请重新登录");
        const outcome = await uploadReleaseAsset(file, {
          token,
          appKey,
          sha256,
          contentType: file.name.toLowerCase().endsWith(".apk") ? "application/vnd.android.package-archive" : file.type,
          onProgress: (fraction) => updateAsset(key, { progress: fraction }),
          signal: abort.signal
        });
        updateAsset(key, {
          phase: "done",
          progress: 1,
          url: outcome.result.reference,
          downloadUrl: outcome.result.downloadUrl,
          fileSize: outcome.result.fileSize,
          mode: outcome.mode,
          note: outcome.fallbackReason,
          abort: undefined
        });
      } catch (error) {
        updateAsset(key, {
          phase: "error",
          abort: undefined,
          error: isUploadAborted(error) ? "已取消上传" : error instanceof ApiError || error instanceof Error ? error.message : "上传失败"
        });
      }
    },
    [appKey, autofill, token, updateAsset]
  );

  const addFiles = useCallback(
    (files: File[]) => {
      const accepted: Array<{ key: string; file: File }> = [];
      for (const file of files) {
        if (file.size > DIRECT_UPLOAD_MAX_BYTES) {
          toast.error(`${file.name} 超过 2 GB，无法上传`);
          continue;
        }
        if (assetsRef.current.some((row) => row.file && row.file.name === file.name && row.file.size === file.size)) {
          toast.info(`${file.name} 已经在列表里`);
          continue;
        }
        accepted.push({ key: nextKey(), file });
      }
      if (!accepted.length) return;
      setAssets((rows) => [
        ...rows,
        ...accepted.map(({ key, file }) => ({
          key,
          abi: "universal",
          label: "",
          url: "",
          fileSize: file.size,
          sha256: "",
          fileName: file.name,
          file,
          phase: "parsing" as const,
          progress: 0
        }))
      ]);
      setErrors((e) => ({ ...e, assets: undefined }));
      for (const { key, file } of accepted) void processFile(key, file);
    },
    [processFile]
  );

  function removeAsset(row: AssetItem) {
    row.abort?.abort();
    releasePackageInfo(row.info);
    setAssets((rows) => rows.filter((r) => r.key !== row.key));
  }

  function addExternal() {
    setAssets((rows) => [
      ...rows,
      {
        key: nextKey(),
        abi: COMMON_ABIS.find((abi) => !rows.some((r) => r.abi === abi)) ?? "universal",
        label: "",
        url: "",
        fileSize: 0,
        sha256: "",
        phase: "done",
        progress: 1,
        external: true
      }
    ]);
  }

  function validate(): ReleaseSavePayload | null {
    const next: Errors = {};
    if (!form.version.trim()) next.version = "填写版本名";
    if (!versionCode) next.versionCode = "填写正整数版本码";
    const minSupported = toInt(form.minSupportedCode);
    if (minSupported && versionCode && minSupported > versionCode) next.minSupportedCode = "不能高于本版本码";
    const abis = assets.map((row) => row.abi.trim().toLowerCase());
    if (busyRows.length) next.assets = "还有安装包在处理，请等上传完成";
    else if (failedRows.length) next.assets = "有安装包上传失败，请重试或移除";
    else if (assets.some((row) => !row.abi.trim() || !row.url.trim())) next.assets = "每个安装包都需要类型与下载地址";
    else if (new Set(abis).size !== abis.length) next.assets = "安装包类型不能重复";
    const minOs = toInt(form.minOsApi);
    const maxOs = toInt(form.maxOsApi);
    if (minOs && maxOs && minOs > maxOs) next.minOsApi = "下限不能高于上限";
    const minSrc = toInt(form.minSourceCode);
    const maxSrc = toInt(form.maxSourceCode);
    if (minSrc && maxSrc && minSrc > maxSrc) next.minSourceCode = "下限不能高于上限";
    if (form.notesFormat === "markdown" && [...form.notesMarkdown].length > RELEASE_NOTES_MAX) {
      next.notes = `更新说明不超过 ${RELEASE_NOTES_MAX} 个字符`;
    }
    setErrors(next);
    const firstError = Object.keys(next)[0] as keyof Errors | undefined;
    if (firstError) {
      const sectionOf: Partial<Record<keyof Errors, SectionId>> = {
        assets: "assets",
        notes: "notes",
        minSupportedCode: "policy",
        minOsApi: "targeting",
        minSourceCode: "targeting"
      };
      const section: SectionId = sectionOf[firstError] ?? "basics";
      scrollTo(section);
      return null;
    }

    const targeting: ReleaseTargeting = {
      testerUserIds: form.testerUserIds,
      testerDeviceIds: form.testerDeviceIds,
      excludeUserIds: form.excludeUserIds,
      excludeDeviceIds: form.excludeDeviceIds,
      regions: form.regions.map((v) => v.toUpperCase()),
      locales: form.locales,
      deviceModels: form.deviceModels,
      abis: form.abis,
      minOsVersion: minOs,
      maxOsVersion: maxOs,
      minSourceVersionCode: minSrc,
      maxSourceVersionCode: maxSrc
    };
    return {
      version: form.version.trim(),
      versionCode,
      title: form.title.trim(),
      notesFormat: form.notesFormat,
      ...(form.notesFormat === "markdown" ? { notesMarkdown: form.notesMarkdown } : { notes: form.notes }),
      platform: form.platform,
      minOsVersion: form.minOsVersion.trim(),
      updateType: form.updateType,
      minSupportedCode: minSupported,
      channelId: form.channelId === "none" ? null : Number(form.channelId),
      visibility: form.visibility,
      rolloutPct: form.rolloutPct,
      targeting,
      assets: assets.map((row) => ({
        id: row.id,
        abi: row.abi.trim().toLowerCase(),
        label: row.label.trim(),
        url: row.url.trim(),
        fileSize: row.fileSize || 0,
        sha256: row.sha256.trim().toLowerCase()
      }))
    };
  }

  async function submit(andPublish: boolean) {
    if (saving) return;
    const payload = validate();
    if (!payload) return;
    try {
      let saved = await save.mutateAsync({ releaseId: item?.id, payload });
      if (andPublish) {
        saved = await publish.mutateAsync({ releaseId: saved.id });
        toast.success(`${saved.version} 已发布`);
      } else {
        toast.success("已保存");
      }
      onSaved?.(saved);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  const submitRef = useRef(submit);
  useEffect(() => {
    submitRef.current = submit;
  });
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        void submitRef.current(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  function requestClose(next: boolean) {
    if (next) return onOpenChange(true);
    if (busyRows.length || dirty) {
      setConfirmClose(true);
      return;
    }
    onOpenChange(false);
  }

  function scrollTo(id: SectionId) {
    setActive(id);
    document.getElementById(`release-section-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  // 滚动时高亮当前所在的分区
  useEffect(() => {
    const root = scrollRef.current;
    if (!root) return;
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        if (visible) setActive(visible.target.id.replace("release-section-", "") as SectionId);
      },
      { root, rootMargin: "0px 0px -65% 0px", threshold: 0 }
    );
    for (const s of SECTIONS) {
      const el = document.getElementById(`release-section-${s.id}`);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, []);

  const conditionCount =
    [form.regions, form.locales, form.deviceModels, form.abis].filter((v) => v.length).length +
    [form.minOsApi || form.maxOsApi, form.minSourceCode || form.maxSourceCode].filter(Boolean).length;
  const listCount = [form.testerUserIds, form.testerDeviceIds, form.excludeUserIds, form.excludeDeviceIds].filter((v) => v.length).length;
  const targetingCount = conditionCount + listCount;
  // 同时出现在内测与排除名单里的人或设备：排除优先，提醒一下免得以为内测生效了
  const overlap =
    form.testerUserIds.filter((id) => form.excludeUserIds.includes(id)).length +
    form.testerDeviceIds.filter((id) => form.excludeDeviceIds.includes(id)).length;

  const sectionDone: Record<SectionId, boolean> = {
    assets: assets.length > 0 && !busyRows.length && !failedRows.length && assets.every((a) => a.url),
    basics: Boolean(form.version.trim() && versionCode),
    notes:
      form.notesFormat === "markdown"
        ? form.notesMarkdown.trim().length > 0 && [...form.notesMarkdown].length <= RELEASE_NOTES_MAX
        : form.notes.replace(/<[^>]*>/g, "").trim().length > 0,
    policy: true,
    audience: true,
    targeting: targetingCount > 0
  };

  const uploadProgress = busyRows.length ? busyRows.reduce((sum, r) => sum + r.progress, 0) / busyRows.length : 0;

  return (
    <TooltipProvider delayDuration={200}>
    <Sheet open={open} onOpenChange={requestClose}>
      <SheetContent
        side="right"
        className="flex w-[96vw] flex-col gap-0 p-0 sm:max-w-5xl"
        // 不让 Radix 把焦点放到第一个导航按钮上（会画出焦点框）；键盘用户按 Tab 照常进入
        onOpenAutoFocus={(e) => e.preventDefault()}
        onDragEnter={(e) => {
          if (Array.from(e.dataTransfer.types).includes("Files")) setDragging(true);
        }}
      >
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <div className="flex items-start gap-3">
            {primary ? <AppIcon icon={primary.icon} size={40} /> : null}
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <SheetTitle className="truncate">
                  {item ? `编辑 ${item.version}` : primary?.appName ? `发布 ${primary.appName}` : "新建版本"}
                </SheetTitle>
                {item ? <ReleaseStatusBadge release={item} /> : null}
              </div>
              <SheetDescription className="mt-0.5">
                {live ? "该版本正在下发，保存后立即生效" : item ? "保存后不改变发布状态" : "拖入安装包即可自动填写，新建的版本先保存为草稿"}
              </SheetDescription>
            </div>
          </div>
        </SheetHeader>

        <div className="flex min-h-0 flex-1">
          <nav className="hidden w-48 shrink-0 border-r bg-muted/20 p-3 md:block" aria-label="编辑分区">
            <ul className="space-y-0.5">
              {SECTIONS.map((s) => {
                const Icon = s.icon;
                const isActive = active === s.id;
                return (
                  <li key={s.id}>
                    <button
                      type="button"
                      onClick={() => scrollTo(s.id)}
                      className={cn(
                        "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-sm transition-colors",
                        isActive ? "bg-background font-medium text-foreground shadow-sm" : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                      )}
                    >
                      <Icon className="size-4 shrink-0" />
                      <span className="flex-1 truncate">{s.label}</span>
                      {s.id === "assets" && assets.length ? (
                        <span className="text-[11px] tabular-nums text-muted-foreground">{assets.length}</span>
                      ) : s.id === "targeting" && targetingCount ? (
                        <span className="text-[11px] tabular-nums text-muted-foreground">{targetingCount}</span>
                      ) : sectionDone[s.id] && s.id !== "policy" && s.id !== "audience" ? (
                        <Check className="size-3.5 text-emerald-600 dark:text-emerald-400" />
                      ) : null}
                    </button>
                  </li>
                );
              })}
            </ul>
          </nav>

          <div ref={scrollRef} className="relative min-w-0 flex-1 overflow-y-auto">
            <div className="mx-auto max-w-3xl space-y-10 px-6 py-6">
              <Section id="assets" title="安装包" description="安装包在本地解析与校验，文件直接上传到对象存储">
                {assets.length === 0 ? (
                  <PackageDropzone onFiles={addFiles} />
                ) : (
                  <div className="space-y-3">
                    {assets.map((row) => (
                      <AssetCard
                        key={row.key}
                        item={row}
                        issues={issues[row.key]}
                        onChange={(changes) => {
                          updateAsset(row.key, changes);
                          setErrors((e) => ({ ...e, assets: undefined }));
                        }}
                        onRemove={() => removeAsset(row)}
                        onRetry={() => row.file && void processFile(row.key, row.file)}
                        onCancel={() => row.abort?.abort()}
                      />
                    ))}
                    <PackageDropzone onFiles={addFiles} compact />
                  </div>
                )}
                <div className="flex items-center justify-between gap-3">
                  {errors.assets ? <p className="text-xs text-destructive">{errors.assets}</p> : <span />}
                  <Button variant="ghost" size="sm" className="h-8 gap-1.5 text-xs text-muted-foreground" onClick={addExternal}>
                    <Link2 className="size-3.5" />
                    改用外部下载地址
                  </Button>
                </div>
              </Section>

              <Section id="basics" title="版本信息">
                <div className="grid gap-4 sm:grid-cols-3">
                  <Field label="版本名" error={errors.version} auto={auto.version}>
                    <Input value={form.version} placeholder="1.2.0" onChange={(e) => patch("version", e.target.value)} />
                  </Field>
                  <Field label="版本码" error={errors.versionCode} auto={auto.versionCode}>
                    <Input
                      className={DATA_FONT}
                      inputMode="numeric"
                      value={form.versionCode}
                      placeholder="1020099"
                      onChange={(e) => patch("versionCode", e.target.value.replace(/\D/g, ""))}
                    />
                  </Field>
                  <Field label="平台" auto={auto.platform}>
                    <Select value={form.platform} onValueChange={(value) => patch("platform", value)}>
                      <SelectTrigger className="h-9 w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {RELEASE_PLATFORMS.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                </div>
                <div className="grid gap-4 sm:grid-cols-[2fr_1fr]">
                  <Field label="标题" hint="版本列表与更新弹窗上的标题，可留空">
                    <Input value={form.title} placeholder={primary?.appName && form.version ? `${primary.appName} ${form.version}` : "例如：春季大更新"} onChange={(e) => patch("title", e.target.value)} />
                  </Field>
                  <Field label="最低系统要求" auto={auto.minOsVersion} hint="展示给用户">
                    <Input value={form.minOsVersion} placeholder="如 Android 7.0" onChange={(e) => patch("minOsVersion", e.target.value)} />
                  </Field>
                </div>
              </Section>

              <Section id="notes" title="更新说明" description="会显示在客户端的更新弹窗与官网下载页">
                <ReleaseNotesEditor
                  appKey={appKey}
                  format={form.notesFormat}
                  markdown={form.notesMarkdown}
                  html={form.notes}
                  onFormatChange={(value) => patch("notesFormat", value)}
                  onMarkdownChange={(value) => patch("notesMarkdown", value)}
                  onHtmlChange={(value) => patch("notes", value)}
                />
                {errors.notes ? <p className="text-xs text-destructive">{errors.notes}</p> : null}
              </Section>

              <Section id="policy" title="更新策略">
                <ChoiceCards
                  value={form.updateType}
                  onChange={(value) => patch("updateType", value)}
                  options={UPDATE_TYPES.map((o) => ({ value: o.value, label: o.label, hint: o.hint, tone: o.value === "force" ? "danger" : o.value === "recommended" ? "warning" : "default" }))}
                />
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="最低支持版本码" error={errors.minSupportedCode} hint="低于此版本码的客户端必须更新，留空不限">
                    <Input
                      className={DATA_FONT}
                      inputMode="numeric"
                      value={form.minSupportedCode}
                      placeholder="不限"
                      onChange={(e) => patch("minSupportedCode", e.target.value.replace(/\D/g, ""))}
                    />
                  </Field>
                </div>
              </Section>

              <Section id="audience" title="受众与灰度">
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="渠道">
                    <Select value={form.channelId} onValueChange={(value) => patch("channelId", value)}>
                      <SelectTrigger className="h-9 w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="none">不限渠道</SelectItem>
                        {channels.map((channel) => (
                          <SelectItem key={channel.id} value={String(channel.id)}>
                            <span className="flex items-center gap-2">
                              <ChannelDot color={channel.color} />
                              {channel.name}
                              {channel.is_default ? <span className="text-muted-foreground">（默认）</span> : null}
                            </span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                </div>
                <Field label="可见范围">
                  <ChoiceCards value={form.visibility} onChange={(value) => patch("visibility", value)} options={VISIBILITIES.map((o) => ({ value: o.value, label: o.label, hint: o.hint }))} />
                </Field>
                <div className="space-y-3 rounded-2xl border p-4">
                  <div className="flex items-baseline justify-between">
                    <div>
                      <div className="text-sm font-medium">灰度比例</div>
                      <div className="mt-0.5 text-xs text-muted-foreground">
                        {form.rolloutPct === 100 ? "全部符合条件的用户都会收到" : form.rolloutPct === 0 ? "暂不推送给任何人（内测名单除外）" : `约 ${form.rolloutPct}% 符合条件的用户会收到`}
                      </div>
                    </div>
                    <span className={cn("text-2xl font-semibold", DATA_FONT)}>{form.rolloutPct}%</span>
                  </div>
                  <Slider value={[form.rolloutPct]} min={0} max={100} step={1} onValueChange={(v) => patch("rolloutPct", v[0] ?? 0)} />
                  <div className="flex flex-wrap gap-1.5">
                    {ROLLOUT_PRESETS.map((pct) => (
                      <Button key={pct} variant={form.rolloutPct === pct ? "default" : "outline"} size="sm" className="h-7 px-2.5 text-xs" onClick={() => patch("rolloutPct", pct)}>
                        {pct}%
                      </Button>
                    ))}
                  </div>
                  <p className="text-[11px] leading-5 text-muted-foreground">按用户（未登录时按设备）稳定分桶，扩大比例时已覆盖的用户保持不变。内测名单不受灰度限制。</p>
                </div>
              </Section>

              <Section id="targeting" title="定向与名单" description="均为可选；同时设置时需全部满足。内测名单跳过定向与灰度，排除名单优先于一切">
                <Collapsible title="定向条件" count={conditionCount} defaultOpen={Boolean(errors.minOsApi || errors.minSourceCode)}>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="地区" hint="按客户端上报或 IP 判定的国家与地区">
                      <RegionPicker value={form.regions} onChange={(v) => patch("regions", v)} />
                    </Field>
                    <Field label="语言" hint="按客户端系统语言匹配">
                      <LocalePicker value={form.locales} onChange={(v) => patch("locales", v)} />
                    </Field>
                    <Field label="机型" hint="型号包含任一关键字即匹配">
                      <ModelPicker value={form.deviceModels} onChange={(v) => patch("deviceModels", v)} />
                    </Field>
                    <Field label="设备架构">
                      <div className="flex min-h-9 flex-wrap items-center gap-1.5">
                        {COMMON_ABIS.filter((abi) => abi !== "universal").map((abi) => {
                          const on = form.abis.includes(abi);
                          return (
                            <Button key={abi} variant={on ? "default" : "outline"} size="sm" className={cn("h-7 px-2 text-xs", DATA_FONT)} onClick={() => patch("abis", on ? form.abis.filter((x) => x !== abi) : [...form.abis, abi])}>
                              {abi}
                            </Button>
                          );
                        })}
                      </div>
                    </Field>
                    <Field label="系统版本" error={errors.minOsApi}>
                      <ApiLevelRange min={form.minOsApi} max={form.maxOsApi} onMin={(v) => patch("minOsApi", v)} onMax={(v) => patch("maxOsApi", v)} />
                    </Field>
                    <Field label="来源版本" error={errors.minSourceCode} hint="只推给从这些版本升级的用户">
                      <SourceVersionRange appKey={appKey} min={form.minSourceCode} max={form.maxSourceCode} onMin={(v) => patch("minSourceCode", v)} onMax={(v) => patch("maxSourceCode", v)} />
                    </Field>
                  </div>
                </Collapsible>
                <Collapsible title="内测与排除名单" count={listCount}>
                  <div className="space-y-5">
                    <AudienceGroup tone="include" title="内测名单" hint="始终能收到这个版本，不受定向条件、灰度比例与可见范围限制">
                      <Field label="用户">
                        <UserPicker appKey={appKey} value={form.testerUserIds} onChange={(v) => patch("testerUserIds", v)} tone="include" />
                      </Field>
                      <Field label="设备">
                        <DevicePicker appKey={appKey} value={form.testerDeviceIds} onChange={(v) => patch("testerDeviceIds", v)} tone="include" />
                      </Field>
                    </AudienceGroup>
                    <AudienceGroup tone="exclude" title="排除名单" hint="永远收不到这个版本，优先于一切规则">
                      <Field label="用户">
                        <UserPicker appKey={appKey} value={form.excludeUserIds} onChange={(v) => patch("excludeUserIds", v)} tone="exclude" />
                      </Field>
                      <Field label="设备">
                        <DevicePicker appKey={appKey} value={form.excludeDeviceIds} onChange={(v) => patch("excludeDeviceIds", v)} tone="exclude" />
                      </Field>
                    </AudienceGroup>
                    {overlap ? (
                      <p className="rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">
                        有 {overlap} 项同时在内测与排除名单中，按排除处理
                      </p>
                    ) : null}
                  </div>
                </Collapsible>
              </Section>
            </div>

            {dragging ? (
              <div
                className="absolute inset-0 z-10 m-3 grid place-items-center rounded-2xl border-2 border-dashed border-primary bg-background/85 backdrop-blur-sm"
                onDragOver={(e) => e.preventDefault()}
                onDragLeave={(e) => {
                  if (e.currentTarget === e.target) setDragging(false);
                }}
                onDrop={(e) => {
                  e.preventDefault();
                  setDragging(false);
                  const files = Array.from(e.dataTransfer.files ?? []);
                  if (files.length) {
                    addFiles(files);
                    scrollTo("assets");
                  }
                }}
              >
                <div className="pointer-events-none flex flex-col items-center gap-2 text-center">
                  <span className="grid size-14 place-items-center rounded-2xl bg-primary/10 text-primary">
                    <Layers className="size-6" />
                  </span>
                  <div className="text-sm font-medium">松开以添加安装包</div>
                  <div className="text-xs text-muted-foreground">将在本地读取版本信息并自动填写</div>
                </div>
              </div>
            ) : null}
          </div>
        </div>

        <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t bg-background px-6 py-3">
          <div className="min-w-0 text-xs text-muted-foreground">
            {busyRows.length ? (
              <span className="flex items-center gap-2">
                <Loader2 className="size-3.5 animate-spin" />
                正在处理 {busyRows.length} 个安装包，{Math.round(uploadProgress * 100)}%
              </span>
            ) : failedRows.length ? (
              <span className="text-destructive">{failedRows.length} 个安装包上传失败</span>
            ) : assets.length ? (
              <span className="flex items-center gap-1.5">
                <Check className="size-3.5 text-emerald-600 dark:text-emerald-400" />
                {assets.length} 个安装包已就绪{dirty ? "，有未保存的修改" : ""}
              </span>
            ) : (
              <span>还没有安装包</span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => requestClose(false)}>
              取消
            </Button>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant={canPublish ? "outline" : "default"} size="sm" disabled={saving} onClick={() => void submit(false)}>
                  {save.isPending && !publish.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
                  {canPublish ? "保存草稿" : "保存"}
                </Button>
              </TooltipTrigger>
              <TooltipContent className="text-xs">Ctrl / ⌘ + S</TooltipContent>
            </Tooltip>
            {canPublish ? (
              <Button size="sm" disabled={saving || assets.length === 0} onClick={() => void submit(true)}>
                {publish.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Rocket className="size-3.5" />}
                保存并发布
              </Button>
            ) : null}
          </div>
        </div>

        <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{busyRows.length ? "安装包还在上传" : "放弃未保存的修改？"}</AlertDialogTitle>
              <AlertDialogDescription>
                {busyRows.length ? "关闭后上传会中止，已填写的内容也不会保存。" : "关闭后本次填写的内容不会保存。"}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>继续编辑</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={() => onOpenChange(false)}>
                放弃并关闭
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SheetContent>
    </Sheet>
    </TooltipProvider>
  );
}

function Section({ id, title, description, children }: { id: SectionId; title: string; description?: string; children: ReactNode }) {
  return (
    <section id={`release-section-${id}`} className="scroll-mt-6 space-y-4">
      <div>
        <h3 className="text-base font-semibold">{title}</h3>
        {description ? <p className="mt-0.5 text-xs text-muted-foreground">{description}</p> : null}
      </div>
      {children}
    </section>
  );
}

function Field({ label, error, hint, auto, children }: { label: string; error?: string; hint?: string; auto?: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="flex h-5 items-center gap-1.5">
        <span className="text-xs font-medium">{label}</span>
        {auto ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="inline-flex items-center gap-0.5 rounded-full bg-violet-500/10 px-1.5 text-[10px] leading-4 text-violet-700 dark:text-violet-300">
                <Sparkles className="size-2.5" />
                自动
              </span>
            </TooltipTrigger>
            <TooltipContent className="text-xs">读取自 {auto}，手动修改后不再自动更新</TooltipContent>
          </Tooltip>
        ) : null}
      </div>
      {children}
      {error ? <p className="text-xs text-destructive">{error}</p> : hint ? <p className="text-[11px] text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function ChoiceCards<T extends string>({
  value,
  onChange,
  options
}: {
  value: T;
  onChange: (value: T) => void;
  options: Array<{ value: T; label: string; hint: string; tone?: "default" | "warning" | "danger" }>;
}) {
  return (
    <div role="radiogroup" className="grid gap-2 sm:grid-cols-3">
      {options.map((option) => {
        const selected = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={selected}
            onClick={() => onChange(option.value)}
            className={cn(
              "relative rounded-xl border px-3.5 py-3 text-left transition-colors",
              selected
                ? option.tone === "danger"
                  ? "border-red-500/60 bg-red-500/[0.05]"
                  : option.tone === "warning"
                    ? "border-amber-500/60 bg-amber-500/[0.05]"
                    : "border-primary/60 bg-primary/[0.04]"
                : "hover:bg-muted/40"
            )}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm font-medium">{option.label}</span>
              <span className={cn("grid size-4 place-items-center rounded-full border", selected && "border-transparent bg-primary text-primary-foreground")}>
                {selected ? <Check className="size-3" /> : null}
              </span>
            </div>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">{option.hint}</p>
          </button>
        );
      })}
    </div>
  );
}

function Collapsible({ title, count, defaultOpen, children }: { title: string; count: number; defaultOpen?: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(Boolean(defaultOpen || count > 0));
  return (
    <div className="rounded-2xl border">
      <button type="button" aria-expanded={open} onClick={() => setOpen((v) => !v)} className="flex w-full items-center gap-2 px-4 py-3 text-left">
        <span className="text-sm font-medium">{title}</span>
        {count ? (
          <Badge variant="secondary" size="sm">
            已设置 {count} 项
          </Badge>
        ) : (
          <span className="text-xs text-muted-foreground">未设置</span>
        )}
        <ChevronDown className={cn("ml-auto size-4 text-muted-foreground transition-transform", open && "rotate-180")} />
      </button>
      {open ? <div className="border-t px-4 py-4">{children}</div> : null}
    </div>
  );
}

function AudienceGroup({ tone, title, hint, children }: { tone: "include" | "exclude"; title: string; hint: string; children: ReactNode }) {
  return (
    <div className="space-y-3">
      <div className="flex items-start gap-2">
        <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", tone === "include" ? "bg-emerald-500" : "bg-red-500")} />
        <div>
          <div className="text-sm font-medium">{title}</div>
          <p className="text-[11px] text-muted-foreground">{hint}</p>
        </div>
      </div>
      <div className="grid gap-4 pl-4 lg:grid-cols-2">{children}</div>
    </div>
  );
}
