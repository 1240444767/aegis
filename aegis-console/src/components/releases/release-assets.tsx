"use client";

import { useRef, useState, type DragEvent, type ReactNode } from "react";
import {
  AlertTriangle,
  Check,
  ChevronDown,
  CloudUpload,
  Copy,
  Cpu,
  ExternalLink,
  FileArchive,
  Fingerprint,
  KeyRound,
  Link2,
  Loader2,
  Package,
  RotateCw,
  Server,
  ShieldCheck,
  Trash2,
  X,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { PackageIcon, PackageInfo } from "@/lib/package-inspector/inspect";
import { cn } from "@/lib/utils";
import { COMMON_ABIS, DATA_FONT, formatBytes } from "./release-shared";

/**
 * 发布编辑器的安装包区：拖放区与安装包卡片。
 *
 * 一张卡片走完一个文件的一生：本地解析 → 计算 SHA-256 → 上传（直传或经服务器）→ 就绪。
 * 解析出的应用信息（图标、名称、包名、版本、SDK、架构、签名）直接画在卡片上，
 * 多个安装包之间版本、包名、签名不一致时就地标出 —— 签名不同的包互相无法覆盖安装。
 */

export const PACKAGE_ACCEPT = ".apk,.aab,.ipa,.hap,.exe,.msi,.msix,.dmg,.pkg,.appimage,.AppImage,.deb,.rpm,.zip";

export type AssetPhase = "idle" | "parsing" | "hashing" | "uploading" | "done" | "error";

export type AssetItem = {
  key: string;
  id?: number;
  abi: string;
  label: string;
  url: string;
  fileSize: number;
  sha256: string;
  downloadUrl?: string;
  fileName?: string;
  /** 本地文件（刚拖进来的） */
  file?: File;
  info?: PackageInfo;
  phase: AssetPhase;
  progress: number;
  mode?: "direct" | "server";
  note?: string;
  error?: string;
  /** 手填外链 */
  external?: boolean;
  abort?: AbortController;
};

/** 跨安装包的一致性问题，按卡片 key 索引 */
export type AssetIssues = Record<string, string[]>;

export function computeAssetIssues(items: AssetItem[], versionCode: number): AssetIssues {
  const issues: AssetIssues = {};
  const parsed = items.filter((item) => item.info);
  const first = parsed[0]?.info;
  const abis = new Map<string, number>();
  for (const item of items) abis.set(item.abi, (abis.get(item.abi) ?? 0) + 1);
  for (const item of items) {
    const list: string[] = [];
    const info = item.info;
    if (info && first && info !== first) {
      if (info.packageName && first.packageName && info.packageName !== first.packageName) list.push(`包名与其他安装包不同（${first.packageName}）`);
      if (info.signer?.sha256 && first.signer?.sha256 && info.signer.sha256 !== first.signer.sha256) list.push("签名证书与其他安装包不同，用户之间无法互相覆盖安装");
    }
    if (info?.versionCode && versionCode && info.versionCode !== versionCode) list.push(`版本码 ${info.versionCode} 与本次发布的 ${versionCode} 不一致`);
    if ((abis.get(item.abi) ?? 0) > 1) list.push("与其他安装包的类型重复");
    if (list.length) issues[item.key] = list;
  }
  return issues;
}

// ── 拖放区 ──────────────────────────

export function PackageDropzone({ onFiles, compact, disabled }: { onFiles: (files: File[]) => void; compact?: boolean; disabled?: boolean }) {
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);

  const handleDrop = (event: DragEvent) => {
    event.preventDefault();
    setOver(false);
    const files = Array.from(event.dataTransfer.files ?? []);
    if (files.length) onFiles(files);
  };

  return (
    <div
      role="button"
      tabIndex={0}
      aria-disabled={disabled}
      onClick={() => !disabled && input.current?.click()}
      onKeyDown={(event) => {
        if ((event.key === "Enter" || event.key === " ") && !disabled) {
          event.preventDefault();
          input.current?.click();
        }
      }}
      onDragOver={(event) => {
        event.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={handleDrop}
      className={cn(
        "group relative flex cursor-pointer items-center gap-4 rounded-2xl border-2 border-dashed transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring",
        compact ? "px-4 py-3" : "flex-col justify-center px-6 py-10 text-center",
        over ? "border-primary bg-primary/[0.04]" : "border-border hover:border-foreground/25 hover:bg-muted/30"
      )}
    >
      <input
        ref={input}
        type="file"
        multiple
        className="hidden"
        accept={PACKAGE_ACCEPT}
        onChange={(event) => {
          const files = Array.from(event.target.files ?? []);
          if (files.length) onFiles(files);
          event.target.value = "";
        }}
      />
      <span
        className={cn(
          "grid shrink-0 place-items-center rounded-xl bg-muted text-muted-foreground transition-colors group-hover:text-foreground",
          compact ? "size-9" : "size-12",
          over && "bg-primary/10 text-primary"
        )}
      >
        <CloudUpload className={compact ? "size-4" : "size-5"} />
      </span>
      <div className={cn("min-w-0", compact ? "text-left" : "")}>
        <div className="text-sm font-medium">{over ? "松开即可添加" : compact ? "继续添加安装包" : "拖入安装包，或点击选择"}</div>
        <div className="mt-0.5 text-xs text-muted-foreground">
          {compact
            ? "可一次拖入多个分架构安装包"
            : "APK、AAB、IPA、HAP 与桌面安装包；在本地读取版本、包名、图标与签名并自动填写，文件直接上传到存储"}
        </div>
      </div>
    </div>
  );
}

// ── 安装包卡片 ──────────────────────────

export function AppIcon({ icon, size = 48, className }: { icon?: PackageIcon; size?: number; className?: string }) {
  const radius = Math.round(size * 0.24);
  if (!icon?.src && !icon?.background) {
    return (
      <span className={cn("grid shrink-0 place-items-center bg-muted text-muted-foreground", className)} style={{ width: size, height: size, borderRadius: radius }}>
        <Package style={{ width: size * 0.45, height: size * 0.45 }} />
      </span>
    );
  }
  return (
    <span
      className={cn("relative shrink-0 overflow-hidden shadow-sm ring-1 ring-black/5", className)}
      style={{ width: size, height: size, borderRadius: radius, background: icon.background ?? (icon.adaptive ? "#fff" : undefined) }}
    >
      {icon.src ? (
        // 自适应图标的前景画布是 108dp，系统只显示中间 72dp：放大 1.5 倍后居中裁切
        // eslint-disable-next-line @next/next/no-img-element
        <img src={icon.src} alt="" className="absolute inset-0 size-full object-contain" style={icon.adaptive ? { transform: "scale(1.5)" } : undefined} />
      ) : null}
    </span>
  );
}

const PHASE_TEXT: Record<AssetPhase, string> = {
  idle: "",
  parsing: "正在读取安装包",
  hashing: "正在计算校验值",
  uploading: "正在上传",
  done: "",
  error: ""
};

export function AssetCard({
  item,
  issues,
  onChange,
  onRemove,
  onRetry,
  onCancel
}: {
  item: AssetItem;
  issues?: string[];
  onChange: (changes: Partial<AssetItem>) => void;
  onRemove: () => void;
  onRetry: () => void;
  onCancel: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const [editingLink, setEditingLink] = useState(Boolean(item.external && !item.url));
  const info = item.info;
  const busy = item.phase === "parsing" || item.phase === "hashing" || item.phase === "uploading";
  const title = info?.appName || item.label || item.fileName || (item.external ? "外部下载地址" : "安装包");
  const abiOptions = Array.from(new Set([...COMMON_ABIS, "x64", "arm64", item.abi].filter(Boolean)));

  return (
    <div className={cn("overflow-hidden rounded-2xl border bg-card transition-colors", item.phase === "error" && "border-destructive/40", issues?.length && item.phase !== "error" && "border-amber-500/40")}>
      <div className="flex items-start gap-3.5 p-4">
        {info ? <AppIcon icon={info.icon} /> : <span className="grid size-12 shrink-0 place-items-center rounded-xl bg-muted text-muted-foreground">{item.external ? <Link2 className="size-5" /> : <FileArchive className="size-5" />}</span>}

        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="truncate text-sm font-semibold">{title}</span>
            {info?.versionName ? <Badge variant="secondary" size="sm" className={DATA_FONT}>{info.versionName}{info.versionCode ? ` (${info.versionCode})` : ""}</Badge> : null}
          </div>
          <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
            {info?.packageName ? <span className={cn("truncate", DATA_FONT)}>{info.packageName}</span> : null}
            {item.fileName && info?.appName ? <span className="truncate">{item.fileName}</span> : null}
            {item.fileSize ? <span className="tabular-nums">{formatBytes(item.fileSize)}</span> : null}
          </div>

          {info ? (
            <div className="mt-2 flex flex-wrap gap-1.5">
              {info.minOsLabel ? <Chip icon={<ShieldCheck className="size-3" />}>最低 {info.minOsLabel}</Chip> : null}
              {info.targetSdk && info.kind !== "ipa" ? <Chip>目标 API {info.targetSdk}</Chip> : null}
              <Chip icon={<Cpu className="size-3" />}>{info.nativeAbis.length ? info.nativeAbis.join(" / ") : info.kind === "apk" || info.kind === "aab" ? "无原生库" : "通用"}</Chip>
              {info.signer ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span>
                      <Chip icon={<KeyRound className="size-3" />}>签名 {info.signer.scheme}</Chip>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent className="max-w-80 text-xs leading-5">
                    <div>{info.signer.subject || "未知主体"}</div>
                    <div className={cn("break-all", DATA_FONT)}>SHA-256 {info.signer.sha256}</div>
                    {info.signer.notAfter ? <div>有效期至 {info.signer.notAfter}</div> : null}
                  </TooltipContent>
                </Tooltip>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {busy && item.phase === "uploading" ? (
            <Button variant="ghost" size="icon" className="size-8 text-muted-foreground" aria-label="取消上传" onClick={onCancel}>
              <X className="size-4" />
            </Button>
          ) : null}
          {item.phase === "error" && item.file ? (
            <Button variant="ghost" size="icon" className="size-8" aria-label="重试" onClick={onRetry}>
              <RotateCw className="size-4" />
            </Button>
          ) : null}
          <Button variant="ghost" size="icon" className="size-8 text-muted-foreground hover:text-destructive" aria-label="移除安装包" disabled={busy} onClick={onRemove}>
            <Trash2 className="size-4" />
          </Button>
        </div>
      </div>

      {busy ? (
        <div className="px-4 pb-3.5">
          <div className="mb-1.5 flex items-center justify-between text-[11px] text-muted-foreground">
            <span className="flex items-center gap-1.5">
              <Loader2 className="size-3 animate-spin" />
              {PHASE_TEXT[item.phase]}
              {item.phase === "uploading" && item.mode === "direct" ? "（直传存储）" : null}
            </span>
            {item.phase !== "parsing" ? <span className="tabular-nums">{Math.round(item.progress * 100)}%</span> : null}
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-muted">
            <div
              className={cn("h-full rounded-full bg-primary transition-[width] duration-300", item.phase === "parsing" && "w-1/3 animate-pulse")}
              style={item.phase !== "parsing" ? { width: `${Math.max(2, item.progress * 100)}%` } : undefined}
            />
          </div>
        </div>
      ) : null}

      {item.phase === "error" ? (
        <div className="mx-4 mb-3.5 flex items-start gap-2 rounded-lg bg-destructive/10 px-3 py-2 text-xs text-destructive">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span className="min-w-0 break-words">{item.error}</span>
        </div>
      ) : null}

      {issues?.length || info?.warnings.length ? (
        <ul className="mx-4 mb-3.5 space-y-1 rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">
          {[...(issues ?? []), ...(info?.warnings ?? [])].map((text) => (
            <li key={text} className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
              {text}
            </li>
          ))}
        </ul>
      ) : null}

      <div className="grid gap-2 border-t bg-muted/20 px-4 py-3 sm:grid-cols-[11rem_minmax(0,1fr)]">
        <Select value={item.abi} onValueChange={(abi) => onChange({ abi })}>
          <SelectTrigger className="h-8 bg-background text-xs" aria-label="安装包类型">
            <SelectValue placeholder="类型" />
          </SelectTrigger>
          <SelectContent>
            {abiOptions.map((abi) => (
              <SelectItem key={abi} value={abi} className={cn("text-xs", DATA_FONT)}>
                {abi === "universal" ? "universal（通用）" : abi}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input value={item.label} placeholder="下载按钮上显示的名称，可留空" className="h-8 bg-background text-xs" onChange={(e) => onChange({ label: e.target.value })} />
      </div>

      {item.external && editingLink ? (
        <div className="space-y-2 border-t px-4 py-3">
          <Input autoFocus className={cn("h-8 text-xs", DATA_FONT)} value={item.url} placeholder="https://example.com/app-release.apk" onChange={(e) => onChange({ url: e.target.value, downloadUrl: undefined })} />
          <div className="grid gap-2 sm:grid-cols-[11rem_minmax(0,1fr)]">
            <Input
              className={cn("h-8 text-xs", DATA_FONT)}
              inputMode="numeric"
              value={item.fileSize ? String(item.fileSize) : ""}
              placeholder="大小（字节）"
              onChange={(e) => onChange({ fileSize: Number(e.target.value.replace(/\D/g, "")) || 0 })}
            />
            <Input className={cn("h-8 text-xs", DATA_FONT)} value={item.sha256} placeholder="SHA-256，可留空" onChange={(e) => onChange({ sha256: e.target.value })} />
          </div>
          <div className="flex justify-end">
            <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setEditingLink(false)}>
              完成
            </Button>
          </div>
        </div>
      ) : null}

      <button
        type="button"
        className="flex w-full items-center gap-3 border-t px-4 py-2 text-left text-[11px] text-muted-foreground transition-colors hover:bg-muted/30"
        onClick={() => (item.external ? setEditingLink((v) => !v) : setExpanded((v) => !v))}
      >
        <StatusLine item={item} />
        <span className="ml-auto flex items-center gap-1">
          {item.external ? (editingLink ? "收起" : "编辑地址") : expanded ? "收起" : "详细信息"}
          {item.external ? null : <ChevronDown className={cn("size-3.5 transition-transform", expanded && "rotate-180")} />}
        </span>
      </button>

      {expanded && !item.external ? <AssetDetails item={item} /> : null}
    </div>
  );
}

function StatusLine({ item }: { item: AssetItem }) {
  if (item.phase === "done") {
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        <Check className="size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />
        <span className="truncate">
          {item.file ? (item.mode === "direct" ? "已直传到存储" : "已经服务器上传") : item.id ? "已保存" : "已就绪"}
          {item.note ? `，${item.note}` : ""}
        </span>
        {item.mode === "direct" ? <Zap className="size-3 shrink-0" /> : item.mode === "server" ? <Server className="size-3 shrink-0" /> : null}
      </span>
    );
  }
  if (item.external) {
    return <span className={cn("min-w-0 truncate", DATA_FONT)}>{item.url || "尚未填写下载地址"}</span>;
  }
  if (item.phase === "error") return <span>上传未完成</span>;
  return <span>处理中</span>;
}

function AssetDetails({ item }: { item: AssetItem }) {
  const info = item.info;
  return (
    <div className="space-y-3 border-t px-4 py-3 text-xs">
      <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2">
        <Detail label="SHA-256" value={item.sha256} copy />
        {info?.signer ? <Detail label="签名证书 SHA-256" value={info.signer.sha256} copy icon={<Fingerprint className="size-3" />} /> : null}
        {info?.signer?.subject ? <Detail label="证书主体" value={info.signer.subject} /> : null}
        {info?.signer?.notAfter ? <Detail label="证书有效期至" value={info.signer.notAfter} /> : null}
        {info?.compileSdk ? <Detail label="编译 SDK" value={`API ${info.compileSdk}`} /> : null}
        {item.downloadUrl ? (
          <Detail
            label="下载地址"
            value={
              <a href={item.downloadUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 break-all underline-offset-2 hover:underline">
                打开
                <ExternalLink className="size-3" />
              </a>
            }
          />
        ) : null}
      </dl>
      {info?.permissions.length ? (
        <div>
          <div className="mb-1.5 text-muted-foreground">权限（{info.permissions.length}）</div>
          <div className="flex max-h-36 flex-wrap gap-1 overflow-y-auto">
            {info.permissions.map((p) => (
              <span key={p} className={cn("rounded-md bg-muted px-1.5 py-0.5 text-[11px]", DATA_FONT)} title={p}>
                {p.replace(/^android\.permission\./, "")}
              </span>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function Detail({ label, value, copy, icon }: { label: string; value?: ReactNode; copy?: boolean; icon?: ReactNode }) {
  const empty = value === undefined || value === "";
  return (
    <div className="min-w-0">
      <dt className="flex items-center gap-1 text-muted-foreground">
        {icon}
        {label}
      </dt>
      <dd className={cn("mt-0.5 flex min-w-0 items-start gap-1.5 break-all", typeof value === "string" && DATA_FONT, empty && "text-muted-foreground")}>
        <span className="min-w-0">{empty ? "无" : value}</span>
        {copy && typeof value === "string" && value ? (
          <button
            type="button"
            className="shrink-0 text-muted-foreground hover:text-foreground"
            aria-label={`复制${label}`}
            onClick={() => void navigator.clipboard?.writeText(value).then(() => toast.success(`已复制${label}`))}
          >
            <Copy className="size-3" />
          </button>
        ) : null}
      </dd>
    </div>
  );
}

function Chip({ icon, children }: { icon?: ReactNode; children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border bg-background px-1.5 py-0.5 text-[11px] text-muted-foreground">
      {icon}
      {children}
    </span>
  );
}
