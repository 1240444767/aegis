"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, KeyRound, Loader2, RotateCw, ShieldCheck, Trash2, Waypoints } from "lucide-react";
import { toast } from "sonner";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  listTransportKeys,
  revokeTransportKey,
  rotateTransportKey,
  type AuthProtocolPolicy,
  type SecurityLevel
} from "@/lib/api/app-auth-protocol";
import { AppTransportEncryptionPanel } from "@/components/apps/app-transport-encryption-panel";
import { ModeCard, SectionCard, StatusDot } from "@/components/apps/app-config-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RadioGroup } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { CopyButton, InlineNotice, LEVELS, errorMessage, formatTime, type PolicyDraft } from "./auth-protocol-shared";

/**
 * 安全与密钥：三档安全等级、签名用的应用密钥、加密档的传输公钥。
 *
 * 等级跟着草稿走（保存后才生效）；密钥轮换是即时动作，不进草稿。
 * 保留了旧接口时，旧命名空间自己的传输加密也在这里，它与上面的等级无关。
 */
export function AuthProtocolSecurity({
  appKey,
  saved,
  draft,
  update,
  rotating,
  revealedSecret,
  onRotateSecret,
  onDismissSecret
}: {
  appKey: string;
  saved: AuthProtocolPolicy;
  draft: PolicyDraft;
  update: (patch: Partial<PolicyDraft>) => void;
  rotating: boolean;
  revealedSecret: string | null;
  onRotateSecret: () => void;
  onDismissSecret: () => void;
}) {
  const needsSecret = draft.securityLevel !== "standard";
  const willIssue = needsSecret && !saved.signingSecretSet;

  return (
    <div className="space-y-4">
      <SectionCard icon={<ShieldCheck className="size-4" />} title="安全等级">
        <RadioGroup
          value={draft.securityLevel}
          onValueChange={(value) => update({ securityLevel: value as SecurityLevel })}
          className="grid gap-2 sm:grid-cols-3"
        >
          {LEVELS.map((item) => (
            <ModeCard
              key={item.level}
              value={item.level}
              active={draft.securityLevel === item.level}
              title={item.title}
              description={item.requirement}
              icon={<item.icon className="size-4" />}
            />
          ))}
        </RadioGroup>
        {willIssue ? (
          <div className="mt-3">
            <InlineNotice icon={KeyRound}>保存后自动签发应用密钥</InlineNotice>
          </div>
        ) : null}
      </SectionCard>

      {needsSecret ? (
        <SectionCard
          icon={<KeyRound className="size-4" />}
          title="应用密钥"
          aside={
            <StatusDot active={saved.signingSecretSet} labelActive="已签发" labelInactive="未签发" />
          }
          footer={
            saved.signingSecretSet ? (
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">
                  上次轮换 {formatTime(saved.signingSecretRotatedAt)}
                </span>
                <Button size="sm" variant="outline" className="h-8 gap-1.5" disabled={rotating} onClick={onRotateSecret}>
                  {rotating ? <Loader2 className="size-3.5 animate-spin" /> : <RotateCw className="size-3.5" />}
                  轮换密钥
                </Button>
              </div>
            ) : null
          }
        >
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
              <span className="text-xs text-muted-foreground">App Secret</span>
              <span className="truncate font-mono text-sm">
                {saved.signingSecretSet ? saved.signingSecretHint : "保存后签发"}
              </span>
            </div>
            {revealedSecret ? (
              <div className="space-y-2 rounded-xl border border-amber-300/60 bg-amber-50/60 p-3 dark:border-amber-900/60 dark:bg-amber-950/30">
                <div className="flex items-center gap-1.5 text-xs font-medium text-amber-800 dark:text-amber-200">
                  <AlertTriangle className="size-3.5" />
                  明文仅显示一次
                  <button
                    type="button"
                    className="ml-auto text-[11px] font-normal text-amber-700/80 underline-offset-2 hover:underline dark:text-amber-300/80"
                    onClick={onDismissSecret}
                  >
                    已保存，隐藏
                  </button>
                </div>
                <div className="flex items-center gap-2">
                  <code className="min-w-0 flex-1 truncate rounded-lg bg-background px-2.5 py-2 font-mono text-xs">
                    {revealedSecret}
                  </code>
                  <CopyButton value={revealedSecret} label="复制" className="h-8 border bg-background" />
                </div>
              </div>
            ) : null}
          </div>
        </SectionCard>
      ) : null}

      {draft.securityLevel === "sealed" ? <TransportKeys appKey={appKey} /> : null}

      {/*
        旧命名空间的传输加密。只在保留了 /api/auth/* 时才有意义：关掉 allowLegacy 后
        这段配置不作用于任何可达路径，摆在那里只会让人误以为它管的是上面的安全等级。
      */}
      {draft.allowLegacy ? <AppTransportEncryptionPanel appKey={appKey} /> : null}
    </div>
  );
}

/** 加密档的传输公钥：当前有效的、轮换下来仍在宽限期的、已撤销的。 */
function TransportKeys({ appKey }: { appKey: string }) {
  const token = useAdminToken();
  const keysQuery = useQuery({
    queryKey: ["app-transport-keys", token, appKey],
    queryFn: () => listTransportKeys(token as string, appKey),
    enabled: Boolean(token)
  });
  // 兜底成数组：列表渲染不该因为接口形状变动就把整个区块打崩
  const keys = Array.isArray(keysQuery.data) ? keysQuery.data : [];

  async function rotate() {
    if (!token) return;
    try {
      await rotateTransportKey(token, appKey);
      await keysQuery.refetch();
      toast.success("传输密钥已轮换");
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  async function revoke(keyId: string) {
    if (!token) return;
    try {
      await revokeTransportKey(token, appKey, keyId);
      await keysQuery.refetch();
      toast.success("密钥已撤销");
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  return (
    <SectionCard
      icon={<Waypoints className="size-4" />}
      title="传输密钥"
      aside={
        <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={() => void rotate()}>
          <RotateCw className="size-3.5" />
          轮换
        </Button>
      }
    >
      {keysQuery.isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </div>
      ) : keys.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">暂无传输密钥</p>
      ) : (
        <div className="overflow-x-auto rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Key ID</TableHead>
                <TableHead>算法</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>有效期至</TableHead>
                <TableHead className="w-12" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {keys.map((key) => (
                <TableRow key={key.keyId}>
                  <TableCell className="max-w-48 truncate font-mono text-xs">{key.keyId}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{key.algorithm}</TableCell>
                  <TableCell>
                    <Badge
                      size="sm"
                      variant={key.status === "active" ? "success" : key.status === "revoked" ? "outline" : "warning"}
                    >
                      {key.status === "active" ? "生效中" : key.status === "revoked" ? "已撤销" : "宽限期"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatTime(key.notAfter)}</TableCell>
                  <TableCell className="text-right">
                    {key.status !== "revoked" ? (
                      <Button
                        size="icon"
                        variant="ghost"
                        className="size-8"
                        aria-label={`撤销密钥 ${key.keyId}`}
                        onClick={() => void revoke(key.keyId)}
                      >
                        <Trash2 className="size-3.5 text-destructive" />
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </SectionCard>
  );
}
