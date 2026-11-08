"use client";

import { useRef, useState } from "react";
import dynamic from "next/dynamic";
import { ArrowLeft } from "lucide-react";
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
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useItemGrantCatalogQuery, useItemGrantRuleQuery } from "@/lib/item-grant-hooks";

/**
 * 规则画布编辑器（全屏弹层）：加载目录与规则，画布本身（Flowgram）在浏览器里按需加载，见 item-grant-workspace。
 * 关闭时有未保存的改动先确认。
 */

// Flowgram 依赖 window 与 reflect-metadata，只在客户端加载；首次打开时再拉这块代码
const ItemGrantWorkspace = dynamic(() => import("@/components/apps/item-grant/item-grant-workspace"), {
  ssr: false,
  loading: () => <EditorSkeleton error={null} />
});

export function ItemGrantEditor({
  appKey,
  ruleId,
  open,
  onOpenChange
}: {
  appKey: string;
  ruleId: number | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const catalogQuery = useItemGrantCatalogQuery(open ? appKey : null);
  const ruleQuery = useItemGrantRuleQuery(open ? appKey : null, open ? ruleId : null);
  const [confirmClose, setConfirmClose] = useState(false);
  const dirtyRef = useRef(false);

  const requestClose = () => {
    if (dirtyRef.current) setConfirmClose(true);
    else onOpenChange(false);
  };

  const ready = catalogQuery.data && ruleQuery.data && ruleQuery.data.id === ruleId;

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? onOpenChange(true) : requestClose())}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[100dvh] w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none border-0 p-0 sm:max-w-none"
        onEscapeKeyDown={(event) => event.preventDefault()}
      >
        <DialogTitle className="sr-only">编辑发放规则</DialogTitle>
        <DialogDescription className="sr-only">在画布上编排触发、筛选、分流、限额与发放</DialogDescription>
        <TooltipProvider delayDuration={300}>
          {ready ? (
            <ItemGrantWorkspace
              key={ruleQuery.data.id}
              appKey={appKey}
              rule={ruleQuery.data}
              catalog={catalogQuery.data}
              onClose={requestClose}
              onDirtyChange={(dirty) => {
                dirtyRef.current = dirty;
              }}
            />
          ) : (
            <EditorSkeleton error={catalogQuery.error ?? ruleQuery.error} onClose={() => onOpenChange(false)} />
          )}
        </TooltipProvider>
      </DialogContent>
      <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle>
            <AlertDialogDescription>画布上还有没保存的改动，关闭后会丢失。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                dirtyRef.current = false;
                onOpenChange(false);
              }}
            >
              放弃修改
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  );
}

function EditorSkeleton({ error, onClose }: { error: unknown; onClose?: () => void }) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex h-14 items-center gap-3 border-b px-4">
        <Button size="icon" variant="ghost" onClick={onClose} aria-label="返回">
          <ArrowLeft />
        </Button>
        <Skeleton className="h-5 w-48" />
      </div>
      {error ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {error instanceof Error ? error.message : "加载失败"}
        </div>
      ) : (
        <div className="grid flex-1 grid-cols-1 xl:grid-cols-[minmax(0,1fr)_380px]">
          <div className="relative">
            <Skeleton className="absolute top-3 left-3 h-80 w-60 rounded-2xl" />
            <Skeleton className="absolute bottom-4 left-1/2 h-10 w-96 -translate-x-1/2 rounded-2xl" />
          </div>
          <Skeleton className="m-3 hidden rounded-xl xl:block" />
        </div>
      )}
    </div>
  );
}
