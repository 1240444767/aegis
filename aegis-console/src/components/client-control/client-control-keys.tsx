"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Copy, KeyRound, Lock, RotateCw } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { ClientControlKeyView } from "@/lib/api/client-control";
import { useClientControlKeysQuery, useRotateClientControlKeyMutation } from "@/lib/client-control-hooks";
import { formatDateTime } from "@/lib/datetime";
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
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { DATA_FONT } from "./client-control-shared";

const ROTATE_COPY = {
  sign: {
    title: "轮换签名密钥",
    description:
      "新签名密钥由当前密钥背书，内置了旧公钥的客户端会沿背书链自动信任它，已发布的版本不需要更新。此后的裁决都由新密钥签名。"
  },
  box: {
    title: "轮换上报加密密钥",
    description: "新的加密密钥立即用于上报。旧密钥在 7 天内仍可解密，缓存了旧配置的客户端不会因此失败。"
  }
} as const;

async function copy(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(`已复制${label}`);
  } catch {
    toast.error("复制失败");
  }
}

/**
 * 管控密钥。签名密钥决定裁决可信与否：建议把当前签名公钥内置进客户端，
 * 不内置时客户端首次使用即信任服务端的根密钥。私钥只在服务端，以主密钥加密保存。
 */
export function ClientControlKeys({ appKey }: { appKey: string }) {
  const keysQuery = useClientControlKeysQuery(appKey);
  const rotate = useRotateClientControlKeyMutation(appKey);
  const [rotating, setRotating] = useState<"sign" | "box" | null>(null);
  const keys = keysQuery.data ?? [];
  const signKeys = keys.filter((key) => key.kind === "sign");
  const boxKeys = keys.filter((key) => key.kind === "box");
  const activeSign = signKeys.find((key) => key.status === "active");
  const snippet = activeSign
    ? `AegisControlTrustStore.inMemory(mapOf("${activeSign.keyId}" to "${activeSign.publicKey}"))`
    : "";

  if (keysQuery.isLoading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-36 rounded-xl" />
        <Skeleton className="h-48 rounded-xl" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {activeSign ? (
        <div className="space-y-3 rounded-xl border bg-card px-4 py-3">
          <div className="flex items-center gap-2">
            <KeyRound className="size-4 text-muted-foreground" />
            <span className="font-medium">内置到客户端的签名公钥</span>
          </div>
          <p className="text-sm text-muted-foreground">
            把下面这把公钥写进客户端，作为验签的信任起点。之后轮换出的新密钥由它逐把背书，客户端不需要随之更新。
          </p>
          <div className="flex items-start gap-2 rounded-lg bg-muted/50 px-3 py-2">
            <code className={cn("min-w-0 flex-1 break-all text-xs", DATA_FONT)}>{snippet}</code>
            <Button variant="ghost" size="icon" className="size-7 shrink-0" aria-label="复制代码" onClick={() => copy(snippet, "代码")}>
              <Copy className="size-3.5" />
            </Button>
          </div>
        </div>
      ) : null}

      <KeyList
        title="签名密钥"
        hint="Ed25519，用于签发裁决"
        keys={signKeys}
        onRotate={() => setRotating("sign")}
      />
      <KeyList
        title="上报加密密钥"
        hint="X25519，客户端用它加密上报"
        keys={boxKeys}
        onRotate={() => setRotating("box")}
      />

      <AlertDialog open={rotating !== null} onOpenChange={(open) => !open && setRotating(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{rotating ? ROTATE_COPY[rotating].title : ""}</AlertDialogTitle>
            <AlertDialogDescription>{rotating ? ROTATE_COPY[rotating].description : ""}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                if (!rotating) return;
                try {
                  await rotate.mutateAsync(rotating);
                  toast.success("已轮换");
                } catch (error) {
                  toast.error(error instanceof ApiError ? error.message : "轮换失败");
                } finally {
                  setRotating(null);
                }
              }}
            >
              轮换
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function KeyList({
  title,
  hint,
  keys,
  onRotate
}: {
  title: string;
  hint: string;
  keys: ClientControlKeyView[];
  onRotate: () => void;
}) {
  const ordered = [...keys].reverse();
  return (
    <div className="rounded-xl border bg-card">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2">
        <div>
          <span className="text-sm font-medium">{title}</span>
          <span className="ml-2 text-xs text-muted-foreground">{hint}</span>
        </div>
        <Button variant="outline" size="sm" className="h-7 text-xs" onClick={onRotate}>
          <RotateCw className="size-3.5" />
          轮换
        </Button>
      </div>
      {ordered.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-muted-foreground">暂无密钥</p>
      ) : (
        <div className="divide-y">
          {ordered.map((key) => (
            <div key={key.keyId} className={cn("flex items-start gap-3 px-4 py-2.5", key.status !== "active" && "opacity-60")}>
              <Lock className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1 space-y-0.5">
                <div className="flex flex-wrap items-center gap-2">
                  <span className={cn("text-sm", DATA_FONT)}>{key.keyId}</span>
                  {key.status === "active" ? (
                    <Badge variant="success" size="sm">
                      当前
                    </Badge>
                  ) : (
                    <Badge variant="secondary" size="sm">
                      已退役
                    </Badge>
                  )}
                  {key.endorsedBy ? (
                    <span className="text-xs text-muted-foreground">
                      由 <span className={DATA_FONT}>{key.endorsedBy}</span> 背书
                    </span>
                  ) : key.kind === "sign" ? (
                    <span className="text-xs text-muted-foreground">根密钥</span>
                  ) : null}
                </div>
                <div className={cn("break-all text-xs text-muted-foreground", DATA_FONT)}>{key.publicKey}</div>
                <div className="text-[11px] text-muted-foreground">
                  创建于 {formatDateTime(key.createdAt)}
                  {key.retiredAt ? `，退役于 ${formatDateTime(key.retiredAt)}` : ""}
                </div>
              </div>
              <Button variant="ghost" size="icon" className="size-7 shrink-0" aria-label="复制公钥" onClick={() => copy(key.publicKey, "公钥")}>
                <Copy className="size-3.5" />
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
