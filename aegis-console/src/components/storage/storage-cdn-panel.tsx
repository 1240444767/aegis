"use client";

import { useState } from "react";
import { Globe, Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { StorageCDNConfig } from "@/lib/api/types";
import type { StorageSource } from "@/lib/api/storage-center";
import { useCDNConfigQuery, useDeleteCDNConfigMutation, useUpsertCDNConfigMutation } from "@/lib/admin-hooks";
import { useStorageProvidersQuery } from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";
import { StorageBrandBadge, providerMeta } from "./storage-shared";

type CDNForm = {
  domain: string;
  protocol: "https" | "http";
  cacheMaxAge: string;
  signEnabled: boolean;
  signSecret: string;
  signTtl: string;
  refererWhitelist: string;
  refererBlacklist: string;
  ipWhitelist: string;
};

const CACHE_PRESETS = [
  { label: "不缓存", value: "0" },
  { label: "5 分钟", value: "300" },
  { label: "1 天", value: "86400" },
  { label: "30 天", value: "2592000" }
];

function seed(cdn?: StorageCDNConfig | null): CDNForm {
  return {
    domain: cdn?.cdnDomain ?? "",
    protocol: cdn?.cdnProtocol === "http" ? "http" : "https",
    cacheMaxAge: String(cdn?.cacheMaxAge ?? 0),
    signEnabled: Boolean(cdn?.signUrlEnabled),
    signSecret: "",
    signTtl: String(cdn?.signUrlTtl ?? 3600),
    refererWhitelist: (cdn?.refererWhitelist ?? []).join("\n"),
    refererBlacklist: (cdn?.refererBlacklist ?? []).join("\n"),
    ipWhitelist: (cdn?.ipWhitelist ?? []).join("\n")
  };
}

const lines = (value: string) => value.split("\n").map((item) => item.trim()).filter(Boolean);

export function StorageCDNPanel({ sources }: { sources: StorageSource[] }) {
  const providers = useStorageProvidersQuery().data;
  const [selected, setSelected] = useState<number | null>(null);
  const configId = selected ?? sources[0]?.id ?? null;
  const query = useCDNConfigQuery(configId);
  const upsert = useUpsertCDNConfigMutation();
  const remove = useDeleteCDNConfigMutation();
  const [draft, setDraft] = useState<{ key: number | null; value: CDNForm } | null>(null);
  const cdn = query.data as (StorageCDNConfig & { signUrlSecretSet?: boolean }) | null | undefined;
  const form = draft?.key === configId ? draft.value : seed(cdn);
  const patch = (next: Partial<CDNForm>) => setDraft({ key: configId, value: { ...form, ...next } });

  async function save() {
    if (!configId) return;
    try {
      await upsert.mutateAsync({
        configId,
        data: {
          cdnDomain: form.domain.trim(),
          cdnProtocol: form.protocol,
          cacheMaxAge: Number.parseInt(form.cacheMaxAge, 10) || 0,
          signUrlEnabled: form.signEnabled,
          // 留空即不修改：表单拿不到已配置的密钥。
          signUrlSecret: form.signSecret,
          signUrlTtl: Number.parseInt(form.signTtl, 10) || 3600,
          refererWhitelist: lines(form.refererWhitelist),
          refererBlacklist: lines(form.refererBlacklist),
          ipWhitelist: lines(form.ipWhitelist)
        }
      });
      toast.success("已保存");
      setDraft(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function clear() {
    if (!configId) return;
    try {
      await remove.mutateAsync(configId);
      toast.success("已清除");
      setDraft(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "清除失败");
    }
  }

  if (sources.length === 0) {
    return <div className="rounded-2xl border border-dashed py-12 text-center text-xs text-muted-foreground">暂无存储源</div>;
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[240px_minmax(0,1fr)]">
      <div className="space-y-1">
        {sources.map((source) => (
          <button
            key={source.id}
            type="button"
            onClick={() => {
              setSelected(source.id);
              setDraft(null);
            }}
            className={cn(
              "flex w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-left text-sm transition-colors hover:bg-muted",
              configId === source.id && "bg-muted font-medium"
            )}
          >
            <StorageBrandBadge meta={providerMeta(providers, source.provider)} provider={source.provider} size="sm" />
            <span className="truncate">{source.config_name}</span>
          </button>
        ))}
      </div>

      <div className="space-y-5 rounded-2xl border bg-card p-5">
        {query.isLoading ? (
          <Skeleton className="h-80 rounded-xl" />
        ) : (
          <>
            <div className="flex items-center gap-2">
              <Globe className="size-4 text-muted-foreground" />
              <span className="text-sm font-semibold">CDN</span>
              {cdn ? (
                <Badge variant="success" size="sm">
                  已配置
                </Badge>
              ) : null}
            </div>
            <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
              <div className="space-y-1.5">
                <Label className="text-xs">CDN 域名</Label>
                <Input value={form.domain} placeholder="cdn.example.com" onChange={(e) => patch({ domain: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">协议</Label>
                <ToggleGroup
                  type="single"
                  variant="outline"
                  value={form.protocol}
                  onValueChange={(protocol) => protocol && patch({ protocol: protocol as CDNForm["protocol"] })}
                >
                  <ToggleGroupItem value="https" className="px-3 text-xs">
                    HTTPS
                  </ToggleGroupItem>
                  <ToggleGroupItem value="http" className="px-3 text-xs">
                    HTTP
                  </ToggleGroupItem>
                </ToggleGroup>
              </div>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">代理下载缓存</Label>
              <div className="flex flex-wrap items-center gap-2">
                <ToggleGroup
                  type="single"
                  variant="outline"
                  value={CACHE_PRESETS.some((item) => item.value === form.cacheMaxAge) ? form.cacheMaxAge : ""}
                  onValueChange={(value) => value && patch({ cacheMaxAge: value })}
                >
                  {CACHE_PRESETS.map((item) => (
                    <ToggleGroupItem key={item.value} value={item.value} className="px-3 text-xs">
                      {item.label}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
                <div className="flex items-center gap-1.5">
                  <Input
                    value={form.cacheMaxAge}
                    inputMode="numeric"
                    className="h-9 w-28 font-mono text-xs"
                    onChange={(e) => patch({ cacheMaxAge: e.target.value.replace(/[^\d]/g, "") })}
                  />
                  <span className="text-xs text-muted-foreground">秒</span>
                </div>
              </div>
            </div>

            <div className="space-y-3">
              <SwitchRow label="签名 URL" checked={form.signEnabled} onChange={(signEnabled) => patch({ signEnabled })} />
              {form.signEnabled ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label className="text-xs">签名密钥</Label>
                    <Input
                      type="password"
                      autoComplete="new-password"
                      value={form.signSecret}
                      placeholder={cdn?.signUrlSecretSet ? "已配置，留空不修改" : ""}
                      className="font-mono"
                      onChange={(e) => patch({ signSecret: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-xs">有效期（秒）</Label>
                    <Input
                      value={form.signTtl}
                      inputMode="numeric"
                      className="font-mono"
                      onChange={(e) => patch({ signTtl: e.target.value.replace(/[^\d]/g, "") })}
                    />
                  </div>
                </div>
              ) : null}
            </div>

            <div className="grid gap-3 md:grid-cols-3">
              <ListField
                label="Referer 白名单"
                placeholder={"example.com\n*.example.com"}
                value={form.refererWhitelist}
                onChange={(refererWhitelist) => patch({ refererWhitelist })}
              />
              <ListField
                label="Referer 黑名单"
                placeholder="bad.example.com"
                value={form.refererBlacklist}
                onChange={(refererBlacklist) => patch({ refererBlacklist })}
              />
              <ListField
                label="IP 白名单"
                placeholder={"10.0.0.0/8\n203.0.113.7"}
                value={form.ipWhitelist}
                onChange={(ipWhitelist) => patch({ ipWhitelist })}
              />
            </div>

            <div className="flex items-center gap-2 border-t pt-4">
              {cdn ? (
                <Button variant="ghost" className="text-destructive hover:text-destructive" onClick={clear} disabled={remove.isPending}>
                  <Trash2 className="size-4" />
                  清除
                </Button>
              ) : null}
              <Button className="ml-auto" onClick={save} disabled={upsert.isPending}>
                {upsert.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                保存
              </Button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function ListField({
  label,
  placeholder,
  value,
  onChange
}: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs">{label}</Label>
      <Textarea rows={4} value={value} placeholder={placeholder} className="font-mono text-xs" onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}
