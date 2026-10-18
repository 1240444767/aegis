"use client";

import { useMemo, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CircleCheck,
  CircleMinus,
  CircleX,
  ExternalLink,
  Globe,
  KeyRound,
  Loader2,
  Lock,
  Plus,
  Search,
  ShieldCheck
} from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type {
  CloudflareBucket,
  CloudflareConnection,
  CloudflareSetupResult,
  StorageMemberMode,
  StoragePool,
  StorageSource
} from "@/lib/api/storage-center";
import { formatDate } from "@/lib/datetime";
import {
  useCloudflareBucketDetailQuery,
  useCloudflareResourcesQuery,
  useConnectCloudflareMutation,
  useCreateCloudflareBucketMutation,
  useSetupCloudflareMutation
} from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";
import { MODE_META, StorageBrandBadge } from "./storage-shared";

const CLOUDFLARE_META = { icon: "cloudflare", brandColor: "#F38020", category: "s3", provider: "r2" };

/** Cloudflare 控制台「创建账户令牌」的预填链接：权限组键见 Cloudflare 的 token template 文档。 */
const TOKEN_TEMPLATE_URL = (() => {
  const permissions = [
    { key: "workers_r2", type: "edit" },
    { key: "account_api_tokens", type: "edit" },
    { key: "account_settings", type: "read" },
    { key: "account_analytics", type: "read" },
    { key: "zone", type: "read" }
  ];
  return `https://dash.cloudflare.com/?to=/:account/api-tokens&permissionGroupKeys=${encodeURIComponent(
    JSON.stringify(permissions)
  )}&name=${encodeURIComponent("Aegis R2")}`;
})();

const PERMISSIONS = ["R2 存储：编辑", "API 令牌：编辑", "账户设置：读取", "账户分析：读取", "区域：读取"];

const JURISDICTIONS = [
  { value: "default", label: "默认" },
  { value: "eu", label: "欧盟" },
  { value: "fedramp", label: "FedRAMP" }
];

const LOCATIONS = [
  { value: "auto", label: "自动" },
  { value: "apac", label: "亚太" },
  { value: "weur", label: "西欧" },
  { value: "eeur", label: "东欧" },
  { value: "wnam", label: "北美西部" },
  { value: "enam", label: "北美东部" },
  { value: "oc", label: "大洋洲" }
];

type Step = "connect" | "bucket" | "configure" | "done";
const STEPS: Array<{ key: Step; label: string }> = [
  { key: "connect", label: "连接" },
  { key: "bucket", label: "存储桶" },
  { key: "configure", label: "配置" },
  { key: "done", label: "完成" }
];

type ConfigForm = {
  credentialMode: "create" | "reuse";
  publicAccess: "private" | "r2dev" | "custom";
  zoneId: string;
  subdomain: string;
  cors: string;
  configName: string;
  setDefault: boolean;
  poolId: string;
  poolMode: StorageMemberMode;
};

/**
 * Cloudflare 快速接入。令牌只在这个组件的状态里，关闭即丢弃。
 * 传入 existing（一个已接入的 R2 存储源）时为「更新凭据」：预选它的账户与桶，保存到原存储源。
 */
export function StorageCloudflareWizard({
  open,
  onOpenChange,
  pools,
  existing
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pools: StoragePool[];
  existing?: StorageSource | null;
}) {
  const [step, setStep] = useState<Step>("connect");
  const [token, setToken] = useState("");
  const [connection, setConnection] = useState<CloudflareConnection | null>(null);
  const [accountId, setAccountId] = useState<string | null>(null);
  const [bucket, setBucket] = useState<CloudflareBucket | null>(null);
  const [form, setForm] = useState<ConfigForm | null>(null);
  const [result, setResult] = useState<CloudflareSetupResult | null>(null);

  const connect = useConnectCloudflareMutation();
  const setup = useSetupCloudflareMutation();

  const reset = () => {
    setStep("connect");
    setToken("");
    setConnection(null);
    setAccountId(null);
    setBucket(null);
    setForm(null);
    setResult(null);
  };
  const close = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  async function doConnect() {
    try {
      const connected = await connect.mutateAsync(token.trim());
      setConnection(connected);
      const preferred = String(existing?.config_data?.account_id ?? "");
      const pick = connected.accounts.find((item) => item.id === preferred) ?? connected.accounts[0];
      setAccountId(pick?.id ?? (preferred || null));
      setStep("bucket");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "连接失败");
    }
  }

  const stepIndex = STEPS.findIndex((item) => item.key === step);

  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent side="right" className="w-[min(760px,100vw)] gap-0 overflow-y-auto p-0">
        <SheetHeader className="space-y-4 border-b px-5 py-4">
          <SheetTitle className="flex items-center gap-3 text-base">
            <StorageBrandBadge meta={CLOUDFLARE_META} size="sm" />
            {existing ? `更新 ${existing.config_name}` : "Cloudflare R2 快速接入"}
          </SheetTitle>
          <ol className="flex items-center gap-2">
            {STEPS.map((item, index) => (
              <li key={item.key} className="flex flex-1 items-center gap-2">
                <span
                  className={cn(
                    "grid size-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold",
                    index < stepIndex && "bg-emerald-500 text-white",
                    index === stepIndex && "bg-foreground text-background",
                    index > stepIndex && "bg-muted text-muted-foreground"
                  )}
                >
                  {index < stepIndex ? <Check className="size-3.5" /> : index + 1}
                </span>
                <span className={cn("text-xs", index === stepIndex ? "font-medium" : "text-muted-foreground")}>{item.label}</span>
                {index < STEPS.length - 1 ? <span className="h-px flex-1 bg-border" /> : null}
              </li>
            ))}
          </ol>
        </SheetHeader>

        {step === "connect" ? (
          <div className="space-y-5 px-5 py-5">
            <div className="space-y-1.5">
              <Label className="text-xs">API 令牌</Label>
              <div className="flex gap-2">
                <Input
                  type="password"
                  autoComplete="off"
                  value={token}
                  className="font-mono"
                  onChange={(e) => setToken(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && token.trim() && doConnect()}
                />
                <Button onClick={doConnect} disabled={!token.trim() || connect.isPending}>
                  {connect.isPending ? <Loader2 className="size-4 animate-spin" /> : <KeyRound className="size-4" />}
                  连接
                </Button>
              </div>
            </div>
            <div className="space-y-2 rounded-xl border bg-muted/40 p-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">所需权限</span>
                <a
                  href={TOKEN_TEMPLATE_URL}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 text-xs text-orange-600 hover:underline dark:text-orange-400"
                >
                  在 Cloudflare 创建令牌
                  <ExternalLink className="size-3" />
                </a>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {PERMISSIONS.map((item) => (
                  <Badge key={item} variant="outline" size="sm">
                    {item}
                  </Badge>
                ))}
              </div>
            </div>
            <div className="flex items-start gap-2 text-[11px] text-muted-foreground">
              <ShieldCheck className="mt-0.5 size-3.5 shrink-0" />
              令牌只用于本次配置，不会保存
            </div>
          </div>
        ) : null}

        {step === "bucket" && connection ? (
          <BucketStep
            token={token}
            connection={connection}
            accountId={accountId}
            onAccount={setAccountId}
            existing={existing}
            selected={bucket}
            onBack={() => setStep("connect")}
            onPick={(picked) => {
              setBucket(picked);
              setForm(null);
              setStep("configure");
            }}
          />
        ) : null}

        {step === "configure" && connection && accountId && bucket ? (
          <ConfigureStep
            token={token}
            accountId={accountId}
            bucket={bucket}
            pools={pools}
            existing={existing}
            form={form}
            onForm={setForm}
            pending={setup.isPending}
            onBack={() => setStep("bucket")}
            onSubmit={async (values, zoneName) => {
              try {
                const domain = values.subdomain.trim() ? `${values.subdomain.trim()}.${zoneName}` : zoneName;
                const response = await setup.mutateAsync({
                  token: token.trim(),
                  accountId,
                  bucket: bucket.name,
                  jurisdiction: bucket.jurisdiction,
                  credentialMode: values.credentialMode,
                  publicAccess: values.publicAccess,
                  customDomain: values.publicAccess === "custom" ? domain : undefined,
                  zoneId: values.publicAccess === "custom" ? values.zoneId : undefined,
                  corsOrigins: values.cors.split(/[\n,]/).map((item) => item.trim()).filter(Boolean),
                  configId: existing?.id,
                  configName: values.configName.trim(),
                  setDefault: values.setDefault,
                  poolId: values.poolId === "none" ? undefined : Number(values.poolId),
                  poolMode: values.poolMode
                });
                setResult(response);
                setStep("done");
              } catch (error) {
                toast.error(error instanceof ApiError ? error.message : "配置失败");
              }
            }}
          />
        ) : null}

        {step === "done" && result ? (
          <div className="space-y-4 px-5 py-5">
            <div className="space-y-2">
              {result.steps.map((item) => {
                const Icon = item.status === "ok" ? CircleCheck : item.status === "failed" ? CircleX : CircleMinus;
                return (
                  <div
                    key={item.name}
                    className={cn(
                      "flex items-start gap-3 rounded-xl px-3 py-2.5",
                      item.status === "failed" ? "bg-destructive/10 ring-1 ring-inset ring-destructive/20" : "bg-muted/60"
                    )}
                  >
                    <Icon
                      className={cn(
                        "mt-0.5 size-4 shrink-0",
                        item.status === "ok" && "text-emerald-500",
                        item.status === "failed" && "text-destructive",
                        item.status === "skipped" && "text-muted-foreground"
                      )}
                    />
                    <div className="min-w-0">
                      <div className="text-sm font-medium">{item.name}</div>
                      {item.message ? <div className="break-all text-[11px] text-muted-foreground">{item.message}</div> : null}
                    </div>
                  </div>
                );
              })}
            </div>
            <div className="flex justify-end gap-2 border-t pt-4">
              {!result.config ? (
                <Button variant="ghost" onClick={() => setStep("configure")}>
                  <ArrowLeft className="size-4" />
                  返回修改
                </Button>
              ) : null}
              <Button onClick={() => close(false)}>完成</Button>
            </div>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function BucketStep({
  token,
  connection,
  accountId,
  onAccount,
  existing,
  selected,
  onBack,
  onPick
}: {
  token: string;
  connection: CloudflareConnection;
  accountId: string | null;
  onAccount: (id: string) => void;
  existing?: StorageSource | null;
  selected: CloudflareBucket | null;
  onBack: () => void;
  onPick: (bucket: CloudflareBucket) => void;
}) {
  const resources = useCloudflareResourcesQuery(token.trim(), accountId);
  const createBucket = useCreateCloudflareBucketMutation();
  const [keyword, setKeyword] = useState("");
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [newJurisdiction, setNewJurisdiction] = useState("default");
  const [newLocation, setNewLocation] = useState("auto");
  const [manualAccount, setManualAccount] = useState("");

  const preferredBucket = String(existing?.config_data?.bucket ?? "");
  const buckets = useMemo(
    () =>
      (resources.data?.buckets ?? []).filter((item) => item.name.toLowerCase().includes(keyword.trim().toLowerCase())),
    [resources.data, keyword]
  );

  async function submitBucket() {
    if (!accountId) return;
    try {
      const created = await createBucket.mutateAsync({
        token: token.trim(),
        accountId,
        bucket: newName.trim(),
        jurisdiction: newJurisdiction,
        locationHint: newLocation === "auto" ? undefined : newLocation
      });
      toast.success("存储桶已创建");
      setCreating(false);
      setNewName("");
      onPick(created);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "创建失败");
    }
  }

  return (
    <div className="space-y-4 px-5 py-5">
      <div className="flex flex-wrap items-center gap-2 rounded-xl border px-3 py-2 text-xs">
        <Badge variant="success" size="sm">
          已连接
        </Badge>
        <span className="text-muted-foreground">{connection.tokenKind === "account" ? "账户令牌" : "用户令牌"}</span>
        {connection.expiresOn ? <span className="text-muted-foreground">· {formatDate(connection.expiresOn)} 到期</span> : null}
        <div className="ml-auto">
          {connection.accounts.length > 0 ? (
            <Select value={accountId ?? ""} onValueChange={onAccount}>
              <SelectTrigger size="sm" className="w-56 text-xs">
                <SelectValue placeholder="选择账户" />
              </SelectTrigger>
              <SelectContent>
                {connection.accounts.map((account) => (
                  <SelectItem key={account.id} value={account.id}>
                    {account.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <div className="flex gap-1.5">
              <Input
                value={manualAccount}
                placeholder="账户 ID"
                className="h-8 w-64 font-mono text-xs"
                onChange={(e) => setManualAccount(e.target.value.trim())}
              />
              <Button size="sm" variant="outline" disabled={manualAccount.length !== 32} onClick={() => onAccount(manualAccount)}>
                确定
              </Button>
            </div>
          )}
        </div>
      </div>

      {accountId ? (
        <>
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input value={keyword} placeholder="搜索存储桶" className="h-8 pl-8 text-xs" onChange={(e) => setKeyword(e.target.value)} />
            </div>
            <Button size="sm" variant="outline" onClick={() => setCreating((value) => !value)}>
              <Plus className="size-3.5" />
              新建存储桶
            </Button>
          </div>

          {creating ? (
            <div className="grid gap-3 rounded-xl border bg-muted/30 p-3 sm:grid-cols-[minmax(0,1fr)_120px_130px_auto] sm:items-end">
              <div className="space-y-1.5">
                <Label className="text-xs">桶名</Label>
                <Input
                  value={newName}
                  placeholder="aegis-files"
                  className="h-9 font-mono"
                  onChange={(e) => setNewName(e.target.value.toLowerCase())}
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">管辖区</Label>
                <Select value={newJurisdiction} onValueChange={setNewJurisdiction}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {JURISDICTIONS.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">位置</Label>
                <Select value={newLocation} onValueChange={setNewLocation} disabled={newJurisdiction !== "default"}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {LOCATIONS.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <Button onClick={submitBucket} disabled={!newName.trim() || createBucket.isPending}>
                {createBucket.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                创建
              </Button>
            </div>
          ) : null}

          {resources.isLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-14 rounded-xl" />
              ))}
            </div>
          ) : resources.isError ? (
            <div className="rounded-xl bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {resources.error instanceof ApiError ? resources.error.message : "读取失败"}
            </div>
          ) : buckets.length === 0 ? (
            <div className="rounded-xl border border-dashed py-10 text-center text-xs text-muted-foreground">暂无存储桶</div>
          ) : (
            <div className="max-h-[52vh] space-y-1.5 overflow-y-auto">
              {buckets.map((item) => {
                const active =
                  (selected?.name === item.name && selected.jurisdiction === item.jurisdiction) ||
                  (!selected && item.name === preferredBucket);
                return (
                  <button
                    key={`${item.jurisdiction}/${item.name}`}
                    type="button"
                    onClick={() => onPick(item)}
                    className={cn(
                      "flex w-full items-center gap-3 rounded-xl border bg-card px-3 py-2.5 text-left transition-colors hover:border-foreground/20",
                      active && "border-orange-500/50 bg-orange-500/5"
                    )}
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate font-mono text-sm font-medium">{item.name}</span>
                        {item.jurisdiction !== "default" ? (
                          <Badge variant="info" size="sm">
                            {JURISDICTIONS.find((j) => j.value === item.jurisdiction)?.label}
                          </Badge>
                        ) : null}
                        {item.configName ? (
                          <Badge variant="outline" size="sm">
                            已接入 {item.configName}
                          </Badge>
                        ) : null}
                      </div>
                      <div className="text-[11px] text-muted-foreground">
                        {[item.location, item.createdAt ? formatDate(item.createdAt) : ""].filter(Boolean).join(" · ")}
                      </div>
                    </div>
                    <ArrowRight className="size-4 text-muted-foreground" />
                  </button>
                );
              })}
            </div>
          )}
        </>
      ) : connection.accountsError ? (
        <div className="rounded-xl bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">{connection.accountsError}</div>
      ) : null}

      <div className="flex border-t pt-4">
        <Button variant="ghost" onClick={onBack}>
          <ArrowLeft className="size-4" />
          换令牌
        </Button>
      </div>
    </div>
  );
}

function ConfigureStep({
  token,
  accountId,
  bucket,
  pools,
  existing,
  form,
  onForm,
  pending,
  onBack,
  onSubmit
}: {
  token: string;
  accountId: string;
  bucket: CloudflareBucket;
  pools: StoragePool[];
  existing?: StorageSource | null;
  form: ConfigForm | null;
  onForm: (form: ConfigForm) => void;
  pending: boolean;
  onBack: () => void;
  onSubmit: (form: ConfigForm, zoneName: string) => void;
}) {
  const detail = useCloudflareBucketDetailQuery({ token: token.trim(), accountId, bucket: bucket.name, jurisdiction: bucket.jurisdiction });
  const resources = useCloudflareResourcesQuery(token.trim(), accountId);
  const zones = resources.data?.zones ?? [];
  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const seeded: ConfigForm = {
    credentialMode: "create",
    publicAccess: "private",
    zoneId: zones[0]?.id ?? "",
    subdomain: "files",
    cors: [...new Set([origin, ...(detail.data?.corsOrigins ?? [])].filter(Boolean))].join("\n"),
    configName: existing?.config_name ?? `r2-${bucket.name}`,
    setDefault: false,
    poolId: "none",
    poolMode: "active"
  };
  const values = form ?? seeded;
  const patch = (next: Partial<ConfigForm>) => onForm({ ...values, ...next });
  const zone = zones.find((item) => item.id === values.zoneId);

  return (
    <div className="space-y-6 px-5 py-5">
      <div className="flex flex-wrap items-center gap-2 rounded-xl border px-3 py-2 text-xs">
        <span className="font-mono font-medium">{bucket.name}</span>
        {detail.isLoading ? (
          <Skeleton className="h-4 w-40" />
        ) : detail.data ? (
          <>
            <Badge variant={detail.data.managedDomainEnabled ? "success" : "outline"} size="sm">
              r2.dev {detail.data.managedDomainEnabled ? "已开启" : "未开启"}
            </Badge>
            {detail.data.customDomains.map((item) => (
              <Badge key={item.domain} variant="info" size="sm">
                {item.domain}
              </Badge>
            ))}
          </>
        ) : null}
      </div>

      <section className="space-y-2">
        <SectionLabel>凭据</SectionLabel>
        <div className="grid gap-2 sm:grid-cols-2">
          <OptionCard
            active={values.credentialMode === "create"}
            icon={<ShieldCheck className="size-4" />}
            title="签发专用令牌"
            badge="推荐"
            onClick={() => patch({ credentialMode: "create" })}
          />
          <OptionCard
            active={values.credentialMode === "reuse"}
            icon={<KeyRound className="size-4" />}
            title="使用当前令牌"
            onClick={() => patch({ credentialMode: "reuse" })}
          />
        </div>
      </section>

      <section className="space-y-2">
        <SectionLabel>访问</SectionLabel>
        <div className="grid gap-2 sm:grid-cols-3">
          <OptionCard
            active={values.publicAccess === "private"}
            icon={<Lock className="size-4" />}
            title="私有"
            onClick={() => patch({ publicAccess: "private" })}
          />
          <OptionCard
            active={values.publicAccess === "custom"}
            icon={<Globe className="size-4" />}
            title="自定义域名"
            disabled={zones.length === 0}
            onClick={() => patch({ publicAccess: "custom" })}
          />
          <OptionCard
            active={values.publicAccess === "r2dev"}
            icon={<ExternalLink className="size-4" />}
            title="r2.dev"
            badge="限速"
            onClick={() => patch({ publicAccess: "r2dev" })}
          />
        </div>
        {values.publicAccess === "custom" ? (
          <div className="flex items-center gap-1.5 pt-1">
            <Input
              value={values.subdomain}
              placeholder="files"
              className="h-9 w-40 font-mono text-xs"
              onChange={(e) => patch({ subdomain: e.target.value.toLowerCase().replace(/[^a-z0-9.-]/g, "") })}
            />
            <span className="text-muted-foreground">.</span>
            <Select value={values.zoneId} onValueChange={(zoneId) => patch({ zoneId })}>
              <SelectTrigger className="w-56">
                <SelectValue placeholder="选择域名" />
              </SelectTrigger>
              <SelectContent>
                {zones.map((item) => (
                  <SelectItem key={item.id} value={item.id}>
                    {item.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : null}
      </section>

      <section className="space-y-1.5">
        <SectionLabel>直传来源（CORS）</SectionLabel>
        <Textarea rows={2} value={values.cors} className="font-mono text-xs" onChange={(e) => patch({ cors: e.target.value })} />
      </section>

      <section className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label className="text-xs">存储源名称</Label>
          <Input
            value={values.configName}
            disabled={Boolean(existing)}
            onChange={(e) => patch({ configName: e.target.value })}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs">加入存储池</Label>
          <div className="flex gap-1.5">
            <Select value={values.poolId} onValueChange={(poolId) => patch({ poolId })}>
              <SelectTrigger className="flex-1">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">不加入</SelectItem>
                {pools.map((pool) => (
                  <SelectItem key={pool.id} value={String(pool.id)}>
                    {pool.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {values.poolId !== "none" ? (
              <Select value={values.poolMode} onValueChange={(poolMode) => patch({ poolMode: poolMode as StorageMemberMode })}>
                <SelectTrigger className="w-24">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(MODE_META) as StorageMemberMode[]).map((mode) => (
                    <SelectItem key={mode} value={mode}>
                      {MODE_META[mode].label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}
          </div>
        </div>
      </section>

      <SwitchRow label="设为平台默认存储" checked={values.setDefault} onChange={(setDefault) => patch({ setDefault })} />

      <div className="flex items-center gap-2 border-t pt-4">
        <Button variant="ghost" onClick={onBack}>
          <ArrowLeft className="size-4" />
          换存储桶
        </Button>
        <Button
          className="ml-auto"
          disabled={pending || !values.configName.trim() || (values.publicAccess === "custom" && !zone)}
          onClick={() => onSubmit(values, zone?.name ?? "")}
        >
          {pending ? <Loader2 className="size-4 animate-spin" /> : null}
          {pending ? "配置中" : "开始配置"}
        </Button>
      </div>
    </div>
  );
}

function OptionCard({
  active,
  icon,
  title,
  badge,
  disabled,
  onClick
}: {
  active: boolean;
  icon: React.ReactNode;
  title: string;
  badge?: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "flex items-center gap-2 rounded-xl bg-muted px-3 py-2.5 text-left text-sm transition-colors hover:bg-muted-foreground/10 disabled:pointer-events-none disabled:opacity-50",
        active && "bg-orange-500/10 ring-1 ring-inset ring-orange-500/40 hover:bg-orange-500/15"
      )}
    >
      <span className={cn("text-muted-foreground", active && "text-orange-600 dark:text-orange-400")}>{icon}</span>
      <span className="font-medium">{title}</span>
      {badge ? (
        <Badge variant="outline" size="sm" className="ml-auto">
          {badge}
        </Badge>
      ) : null}
    </button>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{children}</div>;
}
