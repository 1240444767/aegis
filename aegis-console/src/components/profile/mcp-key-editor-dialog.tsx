"use client";

import { useMemo, useState } from "react";
import { ChevronDown, ChevronRight, LoaderCircle } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { formatDateTime } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import type { McpKey, McpKeyCatalog, McpKeyPayload } from "@/lib/api/mcp-keys";
import type { AppSummary, PermissionGroup } from "@/lib/api/types";

type ScopeMode = "all" | "read" | "custom";

type Draft = {
  name: string;
  description: string;
  mode: ScopeMode;
  /** 自定义模式下勾选的权限点（展开后的具体代码） */
  codes: string[];
  limitApps: boolean;
  appIds: number[];
  readOnly: boolean;
  allowIps: string;
  rate: string;
  /** keep：编辑时保持原过期时间；never：永不过期；数字：从现在起的天数 */
  expiry: string;
};

const EXPIRY_OPTIONS = [
  { value: "30", label: "30 天" },
  { value: "90", label: "90 天" },
  { value: "180", label: "180 天" },
  { value: "365", label: "1 年" },
  { value: "never", label: "永不过期" }
];

function allCodes(groups: PermissionGroup[]) {
  return groups.flatMap((group) => group.permissions.map((item) => item.code));
}

function readCodes(groups: PermissionGroup[]) {
  return allCodes(groups).filter((code) => code.endsWith(":read"));
}

/** 整组勾满时折成 `组:*`，让以后新增到这一组的权限点也跟着生效；只在组内代码确实共享前缀时折 */
function compressCodes(groups: PermissionGroup[], selected: string[]) {
  const set = new Set(selected);
  const out: string[] = [];
  for (const group of groups) {
    const codes = group.permissions.map((item) => item.code);
    const prefix = `${group.key}:`;
    const full = codes.length > 0 && codes.every((code) => set.has(code));
    if (full && codes.every((code) => code.startsWith(prefix))) {
      out.push(`${group.key}:*`);
    } else {
      out.push(...codes.filter((code) => set.has(code)));
    }
  }
  return out;
}

/** 已保存的模式（含 `组:*`）展开回具体代码，供勾选框回显 */
function expandPatterns(groups: PermissionGroup[], patterns: string[]) {
  const codes = allCodes(groups);
  return codes.filter((code) =>
    patterns.some((pattern) => pattern === "*" || pattern === code || (pattern.endsWith(":*") && code.startsWith(pattern.slice(0, -1))))
  );
}

function seedDraft(groups: PermissionGroup[], key?: McpKey | null): Draft {
  if (!key) {
    return {
      name: "",
      description: "",
      mode: "read",
      codes: [],
      limitApps: false,
      appIds: [],
      readOnly: true,
      allowIps: "",
      rate: "120",
      expiry: "90"
    };
  }
  const reads = readCodes(groups);
  const expanded = expandPatterns(groups, key.permissions);
  const isAll = key.permissions.includes("*");
  const isRead = !isAll && expanded.length === reads.length && expanded.every((code) => code.endsWith(":read"));
  return {
    name: key.name,
    description: key.description,
    mode: isAll ? "all" : isRead ? "read" : "custom",
    codes: expanded,
    limitApps: key.appIds.length > 0,
    appIds: key.appIds,
    readOnly: key.readOnly,
    allowIps: key.allowIps.join("\n"),
    rate: String(key.rateLimitPerMinute),
    expiry: "keep"
  };
}

function toPayload(groups: PermissionGroup[], draft: Draft, editing: boolean): McpKeyPayload {
  const permissions =
    draft.mode === "all" ? ["*"] : draft.mode === "read" ? compressCodes(groups, readCodes(groups)) : compressCodes(groups, draft.codes);
  const payload: McpKeyPayload = {
    name: draft.name.trim(),
    description: draft.description.trim(),
    permissions,
    appIds: draft.limitApps ? draft.appIds : [],
    readOnly: draft.readOnly,
    allowIps: draft.allowIps
      .split(/[\s,]+/)
      .map((item) => item.trim())
      .filter(Boolean),
    rateLimitPerMinute: Math.max(0, Math.trunc(Number(draft.rate) || 0))
  };
  if (draft.expiry === "never") payload.neverExpires = true;
  else if (draft.expiry !== "keep") payload.expiresInDays = Number(draft.expiry);
  else if (!editing) payload.expiresInDays = 90;
  return payload;
}

function PermissionPicker({
  groups,
  selected,
  held,
  onChange
}: {
  groups: PermissionGroup[];
  selected: string[];
  /** 签发人当前拥有的权限点；超管为 null（全部都有） */
  held: Set<string> | null;
  onChange: (codes: string[]) => void;
}) {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const set = new Set(selected);

  const toggle = (codes: string[], on: boolean) => {
    const next = new Set(set);
    codes.forEach((code) => (on ? next.add(code) : next.delete(code)));
    onChange([...next]);
  };

  return (
    <ScrollArea className="h-72 rounded-lg border">
      <div className="divide-y">
        {groups.map((group) => {
          const codes = group.permissions.map((item) => item.code);
          const count = codes.filter((code) => set.has(code)).length;
          const expanded = open[group.key] ?? false;
          return (
            <div key={group.key}>
              <div className="flex items-center gap-2 px-3 py-2">
                <Checkbox
                  checked={count === codes.length ? true : count > 0 ? "indeterminate" : false}
                  onCheckedChange={(value) => toggle(codes, value === true)}
                  aria-label={`整组选择 ${group.name}`}
                />
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-1.5 text-left text-sm"
                  onClick={() => setOpen((prev) => ({ ...prev, [group.key]: !expanded }))}
                >
                  {expanded ? <ChevronDown className="size-3.5 text-muted-foreground" /> : <ChevronRight className="size-3.5 text-muted-foreground" />}
                  <span className="font-medium">{group.name}</span>
                  <span className="ml-auto text-xs tabular-nums text-muted-foreground">
                    {count}/{codes.length}
                  </span>
                </button>
              </div>
              {expanded ? (
                <div className="space-y-1 pb-2 pl-9 pr-3">
                  {group.permissions.map((item) => {
                    const missing = held !== null && !held.has(item.code);
                    return (
                      <label key={item.code} className="flex cursor-pointer items-start gap-2 rounded-md px-1 py-1 hover:bg-muted/50">
                        <Checkbox
                          className="mt-0.5"
                          checked={set.has(item.code)}
                          onCheckedChange={(value) => toggle([item.code], value === true)}
                        />
                        <span className="min-w-0 flex-1">
                          <span className={cn("text-sm", missing && "text-muted-foreground")}>{item.name}</span>
                          <span className="ml-2 font-mono text-[11px] text-muted-foreground">{item.code}</span>
                          {missing ? <span className="ml-2 text-[11px] text-amber-600 dark:text-amber-400">你当前没有此权限</span> : null}
                        </span>
                      </label>
                    );
                  })}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </ScrollArea>
  );
}

/**
 * 签发 / 编辑一把 MCP 密钥。
 *
 * 范围默认收得最紧（只读、全部读权限、90 天）：助手接入的第一步通常只是「看看数据」，
 * 需要写的时候再到这里放开，比一上来给全权更不容易出事。
 */
export function McpKeyEditorDialog({
  open,
  editing,
  catalog,
  apps,
  saving,
  onOpenChange,
  onSubmit
}: {
  open: boolean;
  editing: McpKey | null;
  catalog?: McpKeyCatalog;
  apps: AppSummary[];
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (payload: McpKeyPayload) => void;
}) {
  const groups = useMemo(() => catalog?.groups ?? [], [catalog]);
  const [draft, setDraft] = useState<Draft>(() => seedDraft(groups, editing));
  const held = useMemo(() => {
    if (!catalog || catalog.effective.all) return null;
    return new Set([...catalog.effective.global, ...Object.values(catalog.effective.apps).flat()]);
  }, [catalog]);

  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((prev) => ({ ...prev, [key]: value }));

  const nameError = !draft.name.trim() ? "请填写名称" : draft.name.trim().length > 96 ? "名称最多 96 个字符" : null;
  const scopeError = draft.mode === "custom" && draft.codes.length === 0 ? "至少勾选一个权限点" : null;
  const appsError = draft.limitApps && draft.appIds.length === 0 ? "至少选择一个应用，或改为不限应用" : null;
  const rateValue = Number(draft.rate);
  const rateError = !Number.isFinite(rateValue) || rateValue < 0 || rateValue > 6000 ? "每分钟上限需在 0 到 6000 之间" : null;
  const blocked = Boolean(nameError || scopeError || appsError || rateError);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{editing ? "编辑 MCP 密钥" : "签发 MCP 密钥"}</DialogTitle>
          <DialogDescription>
            密钥代表你本人，实际能做的是「你此刻的权限」与「密钥范围」的交集；你的权限被收回，密钥随之收窄。
          </DialogDescription>
        </DialogHeader>

        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault();
            if (!blocked) onSubmit(toPayload(groups, draft, Boolean(editing)));
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="mcp-key-name">名称</Label>
              <Input
                id="mcp-key-name"
                value={draft.name}
                maxLength={96}
                placeholder="如 Claude Desktop 运营助手"
                onChange={(event) => patch("name", event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label>有效期</Label>
              <Select value={draft.expiry} onValueChange={(value) => patch("expiry", value)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {editing ? (
                    <SelectItem value="keep">
                      保持不变（{editing.expiresAt ? formatDateTime(editing.expiresAt) : "永不过期"}）
                    </SelectItem>
                  ) : null}
                  {EXPIRY_OPTIONS.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {editing && item.value !== "never" ? `从现在起 ${item.label}` : item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="mcp-key-description">说明</Label>
            <Textarea
              id="mcp-key-description"
              value={draft.description}
              maxLength={500}
              rows={2}
              placeholder="用在哪个客户端、谁在用"
              onChange={(event) => patch("description", event.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label>权限范围</Label>
            <RadioGroup value={draft.mode} onValueChange={(value) => patch("mode", value as ScopeMode)} className="gap-2">
              {[
                { value: "read", title: "全部查看权限", hint: "所有以 :read 结尾的权限点，适合查询与分析" },
                { value: "custom", title: "自定义", hint: "逐项勾选；整组勾满时自动包含该组以后新增的权限点" },
                { value: "all", title: "全部权限", hint: "包含无权限点的写接口；超级管理员签发时还包含超管专属接口" }
              ].map((item) => (
                <label
                  key={item.value}
                  className={cn(
                    "flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 transition-colors",
                    draft.mode === item.value ? "border-primary/60 bg-primary/5" : "hover:bg-muted/40"
                  )}
                >
                  <RadioGroupItem value={item.value} className="mt-0.5" />
                  <span>
                    <span className="block text-sm font-medium">{item.title}</span>
                    <span className="block text-xs text-muted-foreground">{item.hint}</span>
                  </span>
                </label>
              ))}
            </RadioGroup>
            {draft.mode === "custom" ? (
              <PermissionPicker groups={groups} selected={draft.codes} held={held} onChange={(codes) => patch("codes", codes)} />
            ) : null}
            {scopeError ? <p className="text-xs text-destructive">{scopeError}</p> : null}
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <div>
                <Label>限定应用</Label>
                <p className="text-xs text-muted-foreground">限定后只能访问所选应用，且不能调用平台级写接口</p>
              </div>
              <Switch checked={draft.limitApps} onCheckedChange={(value) => patch("limitApps", value)} />
            </div>
            {draft.limitApps ? (
              <ScrollArea className="h-40 rounded-lg border">
                <div className="space-y-0.5 p-2">
                  {apps.map((app) => (
                    <label key={app.id} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted/50">
                      <Checkbox
                        checked={draft.appIds.includes(app.id)}
                        onCheckedChange={(value) =>
                          patch(
                            "appIds",
                            value === true ? [...draft.appIds, app.id] : draft.appIds.filter((id) => id !== app.id)
                          )
                        }
                      />
                      <span className="text-sm">{app.name}</span>
                      <span className="font-mono text-[11px] text-muted-foreground">#{app.id}</span>
                    </label>
                  ))}
                  {!apps.length ? <p className="px-2 py-4 text-center text-xs text-muted-foreground">暂无可选应用</p> : null}
                </div>
              </ScrollArea>
            ) : null}
            {appsError ? <p className="text-xs text-destructive">{appsError}</p> : null}
          </div>

          <div className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5">
            <div>
              <Label>只读</Label>
              <p className="text-xs text-muted-foreground">只允许 GET 请求；关闭后可执行写操作（发会员、封禁、回复工单等）</p>
            </div>
            <Switch checked={draft.readOnly} onCheckedChange={(value) => patch("readOnly", value)} />
          </div>

          <div className="grid gap-4 sm:grid-cols-[1fr_10rem]">
            <div className="space-y-1.5">
              <Label htmlFor="mcp-key-ips">来源 IP 限制</Label>
              <Textarea
                id="mcp-key-ips"
                value={draft.allowIps}
                rows={2}
                placeholder="每行一个 IP 或 CIDR，留空不限"
                className="font-mono text-xs"
                onChange={(event) => patch("allowIps", event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="mcp-key-rate">每分钟调用上限</Label>
              <Input
                id="mcp-key-rate"
                inputMode="numeric"
                value={draft.rate}
                onChange={(event) => patch("rate", event.target.value.replace(/[^\d]/g, ""))}
              />
              <p className="text-[11px] text-muted-foreground">0 为不限</p>
              {rateError ? <p className="text-xs text-destructive">{rateError}</p> : null}
            </div>
          </div>

          {nameError && draft.name ? <p className="text-xs text-destructive">{nameError}</p> : null}

          <DialogFooter className="items-center gap-2 sm:justify-between">
            <div className="flex flex-wrap gap-1.5">
              {draft.readOnly ? <Badge variant="info" size="sm">只读</Badge> : <Badge variant="warning" size="sm">可写</Badge>}
              {draft.mode === "all" ? <Badge variant="danger" size="sm">全部权限</Badge> : null}
            </div>
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button type="submit" disabled={blocked || saving}>
                {saving ? <LoaderCircle className="size-4 animate-spin" /> : null}
                {editing ? "保存" : "签发"}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
