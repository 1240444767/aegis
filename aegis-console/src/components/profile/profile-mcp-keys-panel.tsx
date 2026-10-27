"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Ellipsis, KeyRound, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";

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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Panel } from "@/components/users/detail/user-detail-shared";
import { McpKeyEditorDialog } from "@/components/profile/mcp-key-editor-dialog";
import { McpKeySecretDialog } from "@/components/profile/mcp-key-secret-dialog";
import { useAdminAppsQuery, useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api/client";
import {
  createMcpKey,
  getMcpKeyCatalog,
  listMcpKeys,
  revokeMcpKey,
  rotateMcpKey,
  setMcpKeyEnabled,
  updateMcpKey,
  type CreatedMcpKey,
  type McpKey,
  type McpKeyPayload
} from "@/lib/api/mcp-keys";
import { formatDateTime, formatRelative } from "@/lib/datetime";

function errorMessage(error: unknown) {
  return error instanceof ApiError ? error.message : error instanceof Error ? error.message : "操作失败";
}

const STATUS_BADGE: Record<McpKey["status"], { label: string; variant: "success" | "outline" | "warning" | "danger" }> = {
  active: { label: "可用", variant: "success" },
  disabled: { label: "已停用", variant: "outline" },
  expired: { label: "已过期", variant: "warning" },
  revoked: { label: "已撤销", variant: "danger" }
};

function scopeLabel(key: McpKey) {
  if (key.permissions.includes("*")) return "全部权限";
  const groups = key.permissions.filter((item) => item.endsWith(":*")).length;
  const singles = key.permissions.length - groups;
  return [groups ? `${groups} 个整组` : "", singles ? `${singles} 项` : ""].filter(Boolean).join(" + ");
}

type Confirm = { kind: "revoke" | "rotate"; key: McpKey } | null;

/**
 * 「个人资料 → MCP 密钥」：签发给 AI 助手（MCP 客户端）用的长期凭证。
 *
 * 每个管理员管自己的密钥；超管可以切到「全部管理员」查看与撤销任何人的。
 */
export function ProfileMcpKeysPanel({ isSuperAdmin }: { isSuperAdmin: boolean }) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const appsQuery = useAdminAppsQuery();
  const [showAll, setShowAll] = useState(false);
  const [editor, setEditor] = useState<{ open: boolean; key: McpKey | null; seq: number }>({ open: false, key: null, seq: 0 });
  const [secret, setSecret] = useState<{ created: CreatedMcpKey; rotated: boolean } | null>(null);
  const [confirm, setConfirm] = useState<Confirm>(null);

  const scope = isSuperAdmin && showAll ? "all" : undefined;
  const keysQuery = useQuery({
    queryKey: ["admin-mcp-keys", token, scope],
    queryFn: () => listMcpKeys(token as string, scope),
    enabled: Boolean(token)
  });
  const catalogQuery = useQuery({
    queryKey: ["admin-mcp-key-catalog", token],
    queryFn: () => getMcpKeyCatalog(token as string),
    enabled: Boolean(token),
    staleTime: 5 * 60_000
  });

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["admin-mcp-keys"] });

  const saveMutation = useMutation({
    mutationFn: ({ id, payload }: { id?: number; payload: McpKeyPayload }) =>
      id ? updateMcpKey(token as string, id, payload) : createMcpKey(token as string, payload),
    onSuccess: (result, variables) => {
      setEditor((prev) => ({ ...prev, open: false }));
      void refresh();
      if (!variables.id && "secret" in result) setSecret({ created: result as CreatedMcpKey, rotated: false });
      else toast.success("密钥已更新");
    },
    onError: (error) => toast.error(errorMessage(error))
  });

  const actionMutation = useMutation({
    mutationFn: async ({ kind, key }: { kind: "rotate" | "revoke" | "enable" | "disable"; key: McpKey }) => {
      if (kind === "rotate") return rotateMcpKey(token as string, key.id);
      if (kind === "revoke") return revokeMcpKey(token as string, key.id);
      return setMcpKeyEnabled(token as string, key.id, kind === "enable");
    },
    onSuccess: (result, { kind }) => {
      void refresh();
      if (kind === "rotate") setSecret({ created: result as CreatedMcpKey, rotated: true });
      else toast.success(kind === "revoke" ? "密钥已撤销" : kind === "enable" ? "密钥已启用" : "密钥已停用");
    },
    onError: (error) => toast.error(errorMessage(error))
  });

  const keys = keysQuery.data ?? [];
  const appNames = new Map((appsQuery.data ?? []).map((app) => [app.id, app.name]));
  const usable = keys.filter((key) => key.status !== "revoked").length;
  const limit = catalogQuery.data?.maxPerAdmin ?? 20;

  const openEditor = (key: McpKey | null) => setEditor((prev) => ({ open: true, key, seq: prev.seq + 1 }));

  return (
    <div className="space-y-4">
      <Panel
        title="MCP 密钥"
        icon={<Bot className="size-4" />}
        description="让 Claude、Cursor 等 AI 助手通过 MCP 以你的身份查询与管理 Aegis。密钥能做的不会超过你本人的权限。"
        action={
          <div className="flex items-center gap-3">
            {isSuperAdmin ? (
              <div className="flex items-center gap-2">
                <Switch id="mcp-keys-all" checked={showAll} onCheckedChange={setShowAll} />
                <Label htmlFor="mcp-keys-all" className="text-xs font-normal text-muted-foreground">
                  全部管理员
                </Label>
              </div>
            ) : null}
            <Button size="sm" variant="ghost" aria-label="刷新" onClick={() => void keysQuery.refetch()}>
              <RefreshCw className="size-4" />
            </Button>
            <Button size="sm" disabled={!catalogQuery.data || usable >= limit} onClick={() => openEditor(null)}>
              <Plus className="size-4" />
              签发密钥
            </Button>
          </div>
        }
        bodyClassName="p-0"
      >
        {keysQuery.isLoading ? (
          <div className="space-y-2 p-5">
            {Array.from({ length: 3 }).map((_, index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : keys.length ? (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-5">名称</TableHead>
                  {scope ? <TableHead>管理员</TableHead> : null}
                  <TableHead>范围</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>最近使用</TableHead>
                  <TableHead>过期时间</TableHead>
                  <TableHead className="w-10 pr-5" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((key) => {
                  const status = STATUS_BADGE[key.status] ?? STATUS_BADGE.disabled;
                  const revoked = key.status === "revoked";
                  const apps = key.appIds.length
                    ? key.appIds.map((id) => appNames.get(id) ?? `#${id}`).join("、")
                    : "全部应用";
                  return (
                    <TableRow key={key.id} className={revoked ? "opacity-60" : undefined}>
                      <TableCell className="pl-5">
                        <div className="font-medium">{key.name}</div>
                        <div className="font-mono text-[11px] text-muted-foreground">{key.keyPrefix}…</div>
                      </TableCell>
                      {scope ? <TableCell className="text-sm">{key.adminAccount || `#${key.adminId}`}</TableCell> : null}
                      <TableCell>
                        <div className="flex flex-wrap items-center gap-1">
                          <Badge variant={key.readOnly ? "info" : "warning"} size="sm">
                            {key.readOnly ? "只读" : "可写"}
                          </Badge>
                          <span className="text-xs">{scopeLabel(key)}</span>
                        </div>
                        <div className="max-w-56 truncate text-[11px] text-muted-foreground" title={apps}>
                          {apps}
                          {key.allowIps.length ? `，${key.allowIps.length} 条 IP 限制` : ""}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant={status.variant} size="sm">
                          {status.label}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs">
                        {key.lastUsedAt ? (
                          <>
                            <div title={formatDateTime(key.lastUsedAt)}>{formatRelative(key.lastUsedAt)}</div>
                            <div className="text-[11px] text-muted-foreground">
                              {key.lastUsedIp || "—"}，{key.useCount} 次
                            </div>
                          </>
                        ) : (
                          <span className="text-muted-foreground">从未使用</span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {key.expiresAt ? formatDateTime(key.expiresAt) : "永不过期"}
                      </TableCell>
                      <TableCell className="pr-5 text-right">
                        {!revoked ? (
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button size="icon" variant="ghost" className="size-8" aria-label={`操作 ${key.name}`}>
                                <Ellipsis className="size-4" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem disabled={!catalogQuery.data} onSelect={() => openEditor(key)}>
                                编辑范围
                              </DropdownMenuItem>
                              <DropdownMenuItem onSelect={() => setConfirm({ kind: "rotate", key })}>轮换明文</DropdownMenuItem>
                              <DropdownMenuItem
                                onSelect={() =>
                                  actionMutation.mutate({ kind: key.status === "disabled" ? "enable" : "disable", key })
                                }
                              >
                                {key.status === "disabled" ? "启用" : "停用"}
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem variant="destructive" onSelect={() => setConfirm({ kind: "revoke", key })}>
                                撤销
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        ) : (
          <div className="flex flex-col items-center gap-2 px-5 py-12 text-center">
            <KeyRound className="size-6 text-muted-foreground" />
            <p className="text-sm font-medium">还没有 MCP 密钥</p>
            <p className="max-w-md text-xs text-muted-foreground">
              签发一把密钥，把它填进 MCP 客户端，AI 助手就能在你授予的范围内查询用户、处理工单、发放会员。
            </p>
          </div>
        )}
      </Panel>

      <McpKeyEditorDialog
        key={editor.seq}
        open={editor.open}
        editing={editor.key}
        catalog={catalogQuery.data}
        apps={appsQuery.data ?? []}
        saving={saveMutation.isPending}
        onOpenChange={(open) => setEditor((prev) => ({ ...prev, open }))}
        onSubmit={(payload) => saveMutation.mutate({ id: editor.key?.id, payload })}
      />

      <McpKeySecretDialog created={secret?.created ?? null} rotated={secret?.rotated} onClose={() => setSecret(null)} />

      <AlertDialog open={Boolean(confirm)} onOpenChange={(open) => !open && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirm?.kind === "revoke" ? "撤销密钥" : "轮换明文"}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirm?.kind === "revoke"
                ? `撤销后「${confirm.key.name}」立即失效且无法恢复，正在使用它的 MCP 客户端会全部断开。`
                : `轮换后「${confirm?.key.name ?? ""}」的旧明文立即失效，需要把新明文更新到所有使用它的客户端。范围与使用记录保留。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className={confirm?.kind === "revoke" ? "bg-destructive text-white hover:bg-destructive/90" : undefined}
              onClick={() => {
                if (confirm) actionMutation.mutate(confirm);
                setConfirm(null);
              }}
            >
              {confirm?.kind === "revoke" ? "撤销" : "轮换"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
