"use client";

import { useState } from "react";
import { FileType2, FolderTree, Gauge, Loader2, Pencil, Plus, Ruler, Trash2 } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import { updateStorageRule } from "@/lib/api/storage-resource";
import type { StorageRule } from "@/lib/api/types";
import type { StorageSource } from "@/lib/api/storage-center";
import {
  useAdminAppsQuery,
  useAdminToken,
  useCreateStorageRuleMutation,
  useDeleteStorageRuleMutation,
  useStorageRulesQuery
} from "@/lib/admin-hooks";
import { useQueryClient } from "@tanstack/react-query";
import { cn } from "@/lib/utils";
import { SIZE_UNITS, formatSize, joinSize, splitSize, type SizeUnit } from "./storage-shared";

type RuleType = "upload_limit" | "file_type" | "path_pattern" | "quota";

const TYPE_META: Record<RuleType, { label: string; icon: LucideIcon }> = {
  upload_limit: { label: "单文件上限", icon: Ruler },
  file_type: { label: "文件类型", icon: FileType2 },
  path_pattern: { label: "路径", icon: FolderTree },
  quota: { label: "配额", icon: Gauge }
};

type RuleForm = {
  name: string;
  ruleType: RuleType;
  configId: string;
  appId: string;
  uploader: "all" | "user" | "admin";
  maxSize: string;
  maxUnit: SizeUnit;
  maxFiles: string;
  mode: "allow" | "deny";
  extensions: string;
  mimeTypes: string;
  patterns: string;
};

const EMPTY: RuleForm = {
  name: "",
  ruleType: "upload_limit",
  configId: "all",
  appId: "all",
  uploader: "all",
  maxSize: "",
  maxUnit: "MB",
  maxFiles: "",
  mode: "allow",
  extensions: "",
  mimeTypes: "",
  patterns: ""
};

function list(value: unknown): string[] {
  return Array.isArray(value) ? value.map(String) : [];
}

function seed(rule: StorageRule | null): RuleForm {
  if (!rule) return EMPTY;
  const data = rule.ruleData ?? {};
  const size = splitSize(Number(data.maxBytes ?? 0));
  const uploaders = list(data.uploaderTypes);
  return {
    name: rule.name,
    ruleType: rule.ruleType as RuleType,
    configId: rule.configId ? String(rule.configId) : "all",
    appId: rule.appId ? String(rule.appId) : "all",
    uploader: uploaders.length === 1 ? (uploaders[0] as "user" | "admin") : "all",
    maxSize: size.value,
    maxUnit: size.unit,
    maxFiles: data.maxFiles ? String(data.maxFiles) : "",
    mode: data.mode === "deny" ? "deny" : "allow",
    extensions: list(data.extensions).join(" "),
    mimeTypes: list(data.mimeTypes).join(" "),
    patterns: list(data.patterns).join("\n")
  };
}

function toRuleData(form: RuleForm): Record<string, unknown> {
  const tokens = (value: string) => value.split(/[\s,，]+/).map((item) => item.trim()).filter(Boolean);
  const data: Record<string, unknown> = {};
  if (form.uploader !== "all") data.uploaderTypes = [form.uploader];
  switch (form.ruleType) {
    case "upload_limit":
      data.maxBytes = joinSize(form.maxSize, form.maxUnit);
      break;
    case "file_type":
      data.mode = form.mode;
      data.extensions = tokens(form.extensions);
      data.mimeTypes = tokens(form.mimeTypes);
      break;
    case "path_pattern":
      data.mode = form.mode;
      data.patterns = form.patterns.split("\n").map((item) => item.trim()).filter(Boolean);
      break;
    case "quota":
      data.maxBytes = joinSize(form.maxSize, form.maxUnit);
      data.maxFiles = Number.parseInt(form.maxFiles, 10) || 0;
      break;
  }
  return data;
}

function summary(rule: StorageRule): string {
  const data = rule.ruleData ?? {};
  const mode = data.mode === "deny" ? "禁止" : "只允许";
  switch (rule.ruleType) {
    case "upload_limit":
      return `≤ ${formatSize(Number(data.maxBytes ?? 0))}`;
    case "file_type":
      return `${mode} ${[...list(data.extensions), ...list(data.mimeTypes)].join(" ")}`;
    case "path_pattern":
      return `${mode} ${list(data.patterns).join("  ")}`;
    case "quota": {
      const parts = [];
      if (Number(data.maxBytes)) parts.push(formatSize(Number(data.maxBytes)));
      if (Number(data.maxFiles)) parts.push(`${data.maxFiles} 个文件`);
      return `≤ ${parts.join(" · ")}`;
    }
    default:
      return "";
  }
}

export function StorageRulesPanel({ sources }: { sources: StorageSource[] }) {
  const rulesQuery = useStorageRulesQuery();
  const apps = useAdminAppsQuery().data ?? [];
  const create = useCreateStorageRuleMutation();
  const remove = useDeleteStorageRuleMutation();
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<{ rule: StorageRule | null } | null>(null);
  const [draft, setDraft] = useState<{ key: string; value: RuleForm } | null>(null);
  const [saving, setSaving] = useState(false);
  const rules = rulesQuery.data ?? [];

  const key = editing ? String(editing.rule?.id ?? "new") : "";
  const form = draft?.key === key ? draft.value : seed(editing?.rule ?? null);
  const patch = (next: Partial<RuleForm>) => setDraft({ key, value: { ...form, ...next } });
  const sourceName = (id?: number | null) => sources.find((item) => item.id === id)?.config_name ?? `#${id}`;
  const appName = (id?: number | null) => apps.find((item) => item.id === id)?.name ?? `#${id}`;

  async function updateRule(rule: StorageRule, data: { name?: string; ruleData?: Record<string, unknown>; isActive?: boolean }) {
    await updateStorageRule(token as string, rule.id, {
      name: data.name ?? rule.name,
      ruleData: data.ruleData ?? rule.ruleData,
      isActive: data.isActive ?? rule.isActive
    });
    await queryClient.invalidateQueries({ queryKey: ["storage-rules"] });
  }

  async function submit() {
    if (!form.name.trim()) {
      toast.error("请填写规则名称");
      return;
    }
    setSaving(true);
    try {
      if (editing?.rule) {
        await updateRule(editing.rule, { name: form.name.trim(), ruleData: toRuleData(form) });
      } else {
        await create.mutateAsync({
          name: form.name.trim(),
          ruleType: form.ruleType,
          ruleData: toRuleData(form),
          configId: form.configId === "all" ? undefined : Number(form.configId),
          appId: form.appId === "all" ? undefined : Number(form.appId)
        });
      }
      toast.success("已保存");
      setEditing(null);
      setDraft(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs tabular-nums text-muted-foreground">{rules.length} 条规则</span>
        <Button size="sm" onClick={() => setEditing({ rule: null })}>
          <Plus className="size-3.5" />
          新建规则
        </Button>
      </div>

      {rulesQuery.isLoading ? (
        <Skeleton className="h-40 rounded-2xl" />
      ) : rules.length === 0 ? (
        <div className="rounded-2xl border border-dashed py-12 text-center text-xs text-muted-foreground">暂无规则</div>
      ) : (
        <div className="divide-y rounded-2xl border bg-card">
          {rules.map((rule) => {
            const meta = TYPE_META[rule.ruleType as RuleType] ?? { label: rule.ruleType, icon: Ruler };
            const Icon = meta.icon;
            const uploaders = list(rule.ruleData?.uploaderTypes);
            return (
              <div key={rule.id} className={cn("flex items-center gap-3 px-4 py-3", !rule.isActive && "opacity-60")}>
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                  <Icon className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-sm font-medium">{rule.name}</span>
                    <Badge variant="outline" size="sm">
                      {meta.label}
                    </Badge>
                    <Badge variant="secondary" size="sm">
                      {rule.configId ? sourceName(rule.configId) : "全部存储"}
                    </Badge>
                    <Badge variant="secondary" size="sm">
                      {rule.appId ? appName(rule.appId) : "全部应用"}
                    </Badge>
                    {uploaders.length === 1 ? (
                      <Badge variant="secondary" size="sm">
                        {uploaders[0] === "admin" ? "管理员上传" : "用户上传"}
                      </Badge>
                    ) : null}
                  </div>
                  <div className="mt-0.5 truncate font-mono text-[11px] text-muted-foreground">{summary(rule)}</div>
                </div>
                <Switch
                  checked={rule.isActive}
                  onCheckedChange={async (isActive) => {
                    try {
                      await updateRule(rule, { isActive });
                    } catch (error) {
                      toast.error(error instanceof ApiError ? error.message : "更新失败");
                    }
                  }}
                />
                <Button size="icon-sm" variant="ghost" onClick={() => setEditing({ rule })} aria-label="编辑">
                  <Pencil className="size-3.5" />
                </Button>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  className="text-destructive"
                  aria-label="删除"
                  onClick={async () => {
                    try {
                      await remove.mutateAsync(rule.id);
                      toast.success("已删除");
                    } catch (error) {
                      toast.error(error instanceof ApiError ? error.message : "删除失败");
                    }
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
            );
          })}
        </div>
      )}

      <Dialog
        open={Boolean(editing)}
        onOpenChange={(open) => {
          if (!open) {
            setEditing(null);
            setDraft(null);
          }
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle className="text-base">{editing?.rule ? "编辑规则" : "新建规则"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            {!editing?.rule ? (
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                {(Object.keys(TYPE_META) as RuleType[]).map((type) => {
                  const Icon = TYPE_META[type].icon;
                  const active = form.ruleType === type;
                  return (
                    <button
                      key={type}
                      type="button"
                      onClick={() => patch({ ruleType: type })}
                      className={cn(
                        "flex flex-col items-center gap-1.5 rounded-xl bg-muted px-2 py-3 text-xs transition-colors hover:bg-muted-foreground/10",
                        active && "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30"
                      )}
                    >
                      <Icon className={cn("size-4 text-muted-foreground", active && "text-emerald-600 dark:text-emerald-400")} />
                      {TYPE_META[type].label}
                    </button>
                  );
                })}
              </div>
            ) : null}

            <Field label="名称">
              <Input value={form.name} onChange={(e) => patch({ name: e.target.value })} />
            </Field>

            {form.ruleType === "upload_limit" || form.ruleType === "quota" ? (
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label={form.ruleType === "quota" ? "总大小" : "单文件上限"}>
                  <SizeInput value={form.maxSize} unit={form.maxUnit} onChange={(maxSize, maxUnit) => patch({ maxSize, maxUnit })} />
                </Field>
                {form.ruleType === "quota" ? (
                  <Field label="文件数">
                    <Input
                      value={form.maxFiles}
                      inputMode="numeric"
                      placeholder="不限"
                      className="font-mono"
                      onChange={(e) => patch({ maxFiles: e.target.value.replace(/[^\d]/g, "") })}
                    />
                  </Field>
                ) : null}
              </div>
            ) : null}

            {form.ruleType === "file_type" || form.ruleType === "path_pattern" ? (
              <ToggleGroup
                type="single"
                variant="outline"
                value={form.mode}
                onValueChange={(mode) => mode && patch({ mode: mode as RuleForm["mode"] })}
                className="w-full"
              >
                <ToggleGroupItem value="allow" className="flex-1 text-xs">
                  只允许
                </ToggleGroupItem>
                <ToggleGroupItem value="deny" className="flex-1 text-xs">
                  禁止
                </ToggleGroupItem>
              </ToggleGroup>
            ) : null}

            {form.ruleType === "file_type" ? (
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="扩展名">
                  <Input
                    value={form.extensions}
                    placeholder=".png .jpg"
                    className="font-mono"
                    onChange={(e) => patch({ extensions: e.target.value })}
                  />
                </Field>
                <Field label="MIME 类型">
                  <Input
                    value={form.mimeTypes}
                    placeholder="image/*"
                    className="font-mono"
                    onChange={(e) => patch({ mimeTypes: e.target.value })}
                  />
                </Field>
              </div>
            ) : null}

            {form.ruleType === "path_pattern" ? (
              <Field label="路径模式">
                <Textarea
                  rows={3}
                  value={form.patterns}
                  placeholder={"avatars/**\n**/*.exe"}
                  className="font-mono text-xs"
                  onChange={(e) => patch({ patterns: e.target.value })}
                />
              </Field>
            ) : null}

            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="存储">
                <Select value={form.configId} onValueChange={(configId) => patch({ configId })} disabled={Boolean(editing?.rule)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部存储</SelectItem>
                    {sources.map((source) => (
                      <SelectItem key={source.id} value={String(source.id)}>
                        {source.config_name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="应用">
                <Select value={form.appId} onValueChange={(appId) => patch({ appId })} disabled={Boolean(editing?.rule)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部应用</SelectItem>
                    {apps.map((app) => (
                      <SelectItem key={app.id} value={String(app.id)}>
                        {app.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="上传者">
                <Select value={form.uploader} onValueChange={(uploader) => patch({ uploader: uploader as RuleForm["uploader"] })}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部</SelectItem>
                    <SelectItem value="user">用户</SelectItem>
                    <SelectItem value="admin">管理员</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setEditing(null)}>
              取消
            </Button>
            <Button onClick={submit} disabled={saving}>
              {saving ? <Loader2 className="size-4 animate-spin" /> : null}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}

function SizeInput({ value, unit, onChange }: { value: string; unit: SizeUnit; onChange: (value: string, unit: SizeUnit) => void }) {
  return (
    <div className="flex items-center">
      <Input
        value={value}
        inputMode="decimal"
        placeholder="不限"
        className="rounded-r-none font-mono"
        onChange={(e) => onChange(e.target.value.replace(/[^\d.]/g, ""), unit)}
      />
      <Select value={unit} onValueChange={(next) => onChange(value, next as SizeUnit)}>
        <SelectTrigger className="w-[72px] rounded-l-none border-l-0">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {SIZE_UNITS.map((item) => (
            <SelectItem key={item} value={item}>
              {item}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
