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
import type { ItemGrantGraph } from "@/lib/api/item-grant";
import { useLevelCatalogQuery, useLevelSchemeQuery } from "@/lib/level-hooks";

/**
 * 等级体系画布编辑器（全屏弹层）：加载目录与体系，画布（Flowgram）在浏览器里按需加载，见 level-workspace。
 * 默认载入草稿；从版本记录打开某一版、在顶栏换模板时换一张画布重新挂载编辑器。关闭时有未保存的改动先确认。
 */

const LevelWorkspace = dynamic(() => import("@/components/apps/levels/level-workspace"), {
  ssr: false,
  loading: () => <EditorSkeleton error={null} />
});

export type LevelEditorSeed = { graph: ItemGrantGraph; from: string };

export function LevelEditor({
  appKey,
  open,
  seed,
  onOpenChange
}: {
  appKey: string;
  open: boolean;
  /** 不载入草稿、改载入这张画布（某次发布） */
  seed?: LevelEditorSeed | null;
  onOpenChange: (open: boolean) => void;
}) {
  const catalogQuery = useLevelCatalogQuery(open ? appKey : null);
  const schemeQuery = useLevelSchemeQuery(open ? appKey : null);
  const [replacement, setReplacement] = useState<(LevelEditorSeed & { key: number }) | null>(null);
  const [confirmClose, setConfirmClose] = useState(false);
  const dirtyRef = useRef(false);

  const close = () => {
    dirtyRef.current = false;
    setReplacement(null);
    onOpenChange(false);
  };
  const requestClose = () => {
    if (dirtyRef.current) setConfirmClose(true);
    else close();
  };

  const scheme = schemeQuery.data;
  const catalog = catalogQuery.data;
  const current = replacement ?? (seed ? { ...seed, key: -1 } : null);

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? onOpenChange(true) : requestClose())}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[100dvh] w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none border-0 p-0 sm:max-w-none"
        onEscapeKeyDown={(event) => event.preventDefault()}
      >
        <DialogTitle className="sr-only">编辑等级体系</DialogTitle>
        <DialogDescription className="sr-only">在画布上编排等级、晋级条件、达成奖励与等级权益</DialogDescription>
        <TooltipProvider delayDuration={300}>
          {scheme && catalog ? (
            <LevelWorkspace
              key={current ? `seed-${current.key}` : "draft"}
              appKey={appKey}
              scheme={scheme}
              catalog={catalog}
              graph={current?.graph ?? scheme.draft}
              seededFrom={current?.from}
              onClose={requestClose}
              onDirtyChange={(dirty) => {
                dirtyRef.current = dirty;
              }}
              onReplace={(graph, from) => setReplacement((previous) => ({ graph, from, key: (previous?.key ?? 0) + 1 }))}
            />
          ) : (
            <EditorSkeleton error={catalogQuery.error ?? schemeQuery.error} onClose={close} />
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
            <AlertDialogAction className="bg-destructive text-white hover:bg-destructive/90" onClick={close}>
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
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">{error instanceof Error ? error.message : "加载失败"}</div>
      ) : (
        <div className="grid flex-1 grid-cols-1 xl:grid-cols-[minmax(0,1fr)_400px]">
          <div className="relative">
            <Skeleton className="absolute top-3 left-3 h-80 w-60 rounded-2xl" />
            <div className="absolute top-16 left-1/2 flex -translate-x-1/2 flex-col items-center gap-10">
              {[0, 1, 2, 3].map((index) => (
                <Skeleton key={index} className="h-28 w-[280px] rounded-2xl" />
              ))}
            </div>
            <Skeleton className="absolute bottom-4 left-1/2 h-10 w-96 -translate-x-1/2 rounded-2xl" />
          </div>
          <Skeleton className="m-3 hidden rounded-xl xl:block" />
        </div>
      )}
    </div>
  );
}
