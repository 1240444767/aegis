"use client";

import { useState } from "react";
import { Link2, Loader2, Unlink } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
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
import { BrandIcon } from "@/components/ui/brand-icon";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { OAuthBinding } from "@/lib/api/app-oauth";
import { useAppOAuthBindingsQuery, useDeleteAppOAuthBindingMutation } from "@/lib/admin-hooks";
import { Panel, formatTime } from "./user-detail-shared";

/** 后端「解绑后该账号将无法登录」的保护码。 */
const LAST_LOGIN_METHOD = 40094;

type PendingUnbind = { binding: OAuthBinding; force: boolean };

/**
 * 用户详情 › 安全 › 第三方账号（QQ、微信等登录渠道的绑定）。
 *
 * 解绑先按普通方式提交；账号没有密码、这是最后一个登录方式时后端拒绝（40094），
 * 弹窗改成强制解绑的确认，管理员确认后才带 force 再提交一次。
 */
export function UserOAuthBindingsPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const bindingsQuery = useAppOAuthBindingsQuery(appKey, { userId, page: 1, pageSize: 50 });
  const unbind = useDeleteAppOAuthBindingMutation(appKey);
  const [pending, setPending] = useState<PendingUnbind | null>(null);
  const items = bindingsQuery.data?.items ?? [];

  async function handleUnbind() {
    if (!pending) return;
    const { binding, force } = pending;
    const name = binding.displayName || binding.provider;
    try {
      await unbind.mutateAsync({ userId, provider: binding.provider, force });
      toast.success(`已解绑${name}`);
      setPending(null);
    } catch (err) {
      if (!force && err instanceof ApiError && err.code === LAST_LOGIN_METHOD) {
        setPending({ binding, force: true });
        return;
      }
      toast.error(err instanceof ApiError ? err.message : "解绑失败");
    }
  }

  const pendingName = pending ? pending.binding.displayName || pending.binding.provider : "";

  return (
    <>
      <Panel title="第三方账号" icon={<Link2 className="size-4" />}>
        {bindingsQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-14 w-full rounded-xl" />
          </div>
        ) : items.length === 0 ? (
          <div className="py-6 text-center text-sm text-muted-foreground">暂无绑定</div>
        ) : (
          <div className="space-y-2">
            {items.map((binding) => (
              <div
                key={binding.id}
                className="flex flex-col gap-3 rounded-xl border px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between"
              >
                <div className="flex min-w-0 items-center gap-3">
                  {binding.avatar ? (
                    // eslint-disable-next-line @next/next/no-img-element -- 第三方头像是任意外链，不走 next/image 的域名白名单
                    <img
                      src={binding.avatar}
                      alt=""
                      referrerPolicy="no-referrer"
                      className="size-9 shrink-0 rounded-full object-cover ring-1 ring-border"
                    />
                  ) : (
                    <div className="grid size-9 shrink-0 place-items-center rounded-full bg-muted">
                      <BrandIcon slug={binding.icon} className="size-4" title={binding.displayName || binding.provider} />
                    </div>
                  )}
                  <div className="min-w-0 space-y-1">
                    <div className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
                      <BrandIcon slug={binding.icon} className="size-3.5 shrink-0" title={binding.displayName || binding.provider} />
                      <span className="shrink-0">{binding.displayName || binding.provider}</span>
                      {binding.nickname ? <span className="truncate text-muted-foreground">{binding.nickname}</span> : null}
                    </div>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
                      <span className="max-w-[260px] truncate font-mono">{binding.providerUserId}</span>
                      {binding.unionId ? (
                        <span className="max-w-[260px] truncate font-mono">UnionID {binding.unionId}</span>
                      ) : null}
                      <span>绑定于 {formatTime(binding.createdAt)}</span>
                    </div>
                  </div>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-8 shrink-0 gap-1 self-end text-xs text-destructive hover:text-destructive sm:self-auto"
                  onClick={() => setPending({ binding, force: false })}
                >
                  <Unlink className="size-3" />
                  解绑
                </Button>
              </div>
            ))}
          </div>
        )}
      </Panel>

      <AlertDialog open={Boolean(pending)} onOpenChange={(open) => !open && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{pending?.force ? `强制解绑${pendingName}？` : `解绑${pendingName}？`}</AlertDialogTitle>
            <AlertDialogDescription>
              {pending?.force
                ? `该账号没有密码，解绑后将无法登录。`
                : `该用户将不能再用这个${pendingName}账号登录。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                // 保持弹窗：40094 时要原地换成强制确认
                event.preventDefault();
                void handleUnbind();
              }}
              disabled={unbind.isPending}
            >
              {unbind.isPending ? <Loader2 className="size-3 animate-spin" /> : null}
              {pending?.force ? "强制解绑" : "确认解绑"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
