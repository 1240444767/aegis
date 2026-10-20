"use client";

import { useState } from "react";
import { Copy, Download, Loader2, PackageOpen, PlusCircle } from "lucide-react";
import { toast } from "sonner";
import { NumberField } from "@/components/apps/app-config-primitives";
import { capacityEnough, seedForm } from "@/components/apps/card-key/card-key-batch-form";
import { copyText, formatCount } from "@/components/apps/card-key/card-key-shared";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ApiError } from "@/lib/api/client";
import { saveTextFile, type CardKey, type CardKeyBatch } from "@/lib/api/card-key";
import { useAppendCardKeysMutation, useExtractCardKeysMutation } from "@/lib/card-key-hooks";

const MAX_COUNT = 10000;

/** 补发：按原批次的前缀、段数与字符集再生成一些。导入的批次没有格式，不能补发。 */
export function CardKeyAppendDialog({
  appKey,
  batch,
  onClose
}: {
  appKey: string;
  batch: CardKeyBatch | null;
  onClose: () => void;
}) {
  const [count, setCount] = useState("100");
  const mutation = useAppendCardKeysMutation(appKey);

  const close = () => {
    setCount("100");
    onClose();
  };

  const submit = async () => {
    if (!batch) return;
    const value = Number(count);
    if (!Number.isInteger(value) || value < 1 || value > MAX_COUNT) {
      toast.error("单次补发数量需要在 1–10000 之间");
      return;
    }
    const enough = capacityEnough(
      {
        ...seedForm(),
        charset: batch.charset,
        segments: String(batch.segments),
        segmentLength: String(batch.segmentLength),
        count: String(batch.total)
      },
      value
    );
    if (!enough) {
      toast.error("当前卡面格式能表达的组合太少，装不下这个数量");
      return;
    }
    try {
      const next = await mutation.mutateAsync({ batchId: batch.id, count: value });
      toast.success(`已补发 ${value} 张，共 ${next.total} 张`);
      close();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "补发失败");
    }
  };

  return (
    <Dialog open={Boolean(batch)} onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>补发卡密</DialogTitle>
          <DialogDescription>
            {batch ? `「${batch.name}」按原格式再生成，现有 ${formatCount(batch.total)} 张` : null}
          </DialogDescription>
        </DialogHeader>
        <NumberField label="补发数量" value={count} min={1} max={MAX_COUNT} unit="张" onChange={setCount} />
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={close}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <PlusCircle className="size-3.5" />}
            补发
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 提卡：取出一些还没交给渠道的未使用卡并打上标记，结果当场给出（复制或下载 TXT）。
 *
 * 取与标记在后端是同一条语句，两个运营同时提卡各拿各的；关掉对话框之后，
 * 这些卡在列表里按「已提取」筛得出来，也可以在卡密列表里取消提取。
 */
export function CardKeyExtractDialog({
  appKey,
  batch,
  onClose
}: {
  appKey: string;
  batch: CardKeyBatch | null;
  onClose: () => void;
}) {
  const [count, setCount] = useState("10");
  const [note, setNote] = useState("");
  const [extracted, setExtracted] = useState<CardKey[] | null>(null);
  const mutation = useExtractCardKeysMutation(appKey);
  const available = batch?.stats?.available ?? 0;

  const close = () => {
    setCount("10");
    setNote("");
    setExtracted(null);
    onClose();
  };

  const submit = async () => {
    if (!batch) return;
    const value = Number(count);
    if (!Number.isInteger(value) || value < 1 || value > MAX_COUNT) {
      toast.error("单次提取数量需要在 1–10000 之间");
      return;
    }
    if (value > available) {
      toast.error(`这一批只剩 ${available} 张可提取的卡，请先补发`);
      return;
    }
    try {
      const result = await mutation.mutateAsync({ batchId: batch.id, count: value, note: note.trim() });
      setExtracted(result.items);
      toast.success(`已提取 ${result.items.length} 张`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "提取失败");
    }
  };

  const text = (extracted ?? []).map((card) => card.code).join("\n");
  const filename = () => {
    const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, "");
    return `${batch?.name ?? "card_keys"}_${stamp}.txt`;
  };

  return (
    <Dialog open={Boolean(batch)} onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{extracted ? "提取完成" : "提取卡密"}</DialogTitle>
          <DialogDescription>
            {extracted
              ? `${extracted.length} 张卡已标记为已提取，请复制或下载后交给渠道`
              : batch
                ? `「${batch.name}」还有 ${formatCount(available)} 张未使用且未提取的卡`
                : null}
          </DialogDescription>
        </DialogHeader>

        {extracted ? (
          <ScrollArea className="h-64 rounded-xl border border-border bg-muted/30">
            <pre className="px-4 py-3 font-mono text-xs leading-6">{text}</pre>
          </ScrollArea>
        ) : (
          <div className="space-y-4">
            <NumberField
              label="提取数量"
              value={count}
              min={1}
              max={Math.min(available, MAX_COUNT)}
              unit="张"
              onChange={setCount}
            />
            <div className="space-y-1.5">
              <span className="text-[11px] font-medium">提取备注</span>
              <Input
                value={note}
                maxLength={128}
                onChange={(event) => setNote(event.target.value)}
                placeholder="交给谁，例如：代理 A 十月批"
              />
            </div>
          </div>
        )}

        <DialogFooter>
          {extracted ? (
            <>
              <Button variant="outline" size="sm" onClick={() => void copyText(text, `已复制 ${extracted.length} 张卡密`)}>
                <Copy className="size-3.5" /> 复制全部
              </Button>
              <Button variant="outline" size="sm" onClick={() => saveTextFile(text + "\n", filename())}>
                <Download className="size-3.5" /> 下载 TXT
              </Button>
              <Button size="sm" onClick={close}>
                完成
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" size="sm" onClick={close}>
                取消
              </Button>
              <Button size="sm" onClick={submit} disabled={mutation.isPending || available === 0}>
                {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <PackageOpen className="size-3.5" />}
                提取
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
