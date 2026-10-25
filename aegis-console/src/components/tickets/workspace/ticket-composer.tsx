"use client";

import { useRef, useState } from "react";
import TextareaAutosize from "react-textarea-autosize";
import { useDropzone } from "react-dropzone";
import { ChevronDown, FileText, Loader2, Lock, MessageSquareReply, Paperclip, Send, Upload, X, Zap } from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList
} from "@/components/ui/command";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useReplyTicketMutation,
  useTicketQuickRepliesQuery,
  useUploadTicketAttachmentMutation
} from "@/lib/ticket-hooks";
import type { TicketAttachment, TicketItem, TicketStatus } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { Kbd, STATUS_LABEL, StatusDot, attachmentUrl, formatBytes, isImageAttachment } from "../ticket-shared";

// 回复框。
//
// - 「回复用户 / 内部备注」两种模式，备注模式整框变琥珀色，避免把内部讨论发给用户；
// - 拖文件进来、直接粘贴截图都能上传；
// - 空框里敲 / 弹出快捷回复，⌘/Ctrl + Enter 发送；
// - 草稿按工单记在内存里，来回切换工单不会丢字。

type Mode = "reply" | "internal";
type Draft = { content: string; mode: Mode; files: TicketAttachment[]; nextStatus: string };

const drafts = new Map<number, Draft>();

const NEXT_STATUS: { value: string; label: string; status?: TicketStatus }[] = [
  { value: "", label: "保持当前状态" },
  { value: "processing", label: "处理中", status: "processing" },
  { value: "pending_user", label: "待用户补充", status: "pending_user" },
  { value: "pending_third_party", label: "等待第三方", status: "pending_third_party" },
  { value: "resolved", label: "已解决", status: "resolved" }
];

export function TicketComposer({
  ticket,
  textareaRef
}: {
  ticket: TicketItem;
  /** 交给工作台，用于 R 键聚焦 */
  textareaRef?: React.RefObject<HTMLTextAreaElement | null>;
}) {
  const perms = ticket.permissions;
  const canReply = Boolean(perms?.reply);
  const canNote = Boolean(perms?.internalNote);

  const initial = drafts.get(ticket.id);
  const [content, setContent] = useState(initial?.content ?? "");
  const [mode, setMode] = useState<Mode>(initial?.mode ?? (canReply ? "reply" : "internal"));
  const [files, setFiles] = useState<TicketAttachment[]>(initial?.files ?? []);
  const [nextStatus, setNextStatus] = useState(initial?.nextStatus ?? "");
  const [uploading, setUploading] = useState(0);
  const [quickOpen, setQuickOpen] = useState(false);
  const localRef = useRef<HTMLTextAreaElement | null>(null);

  const replyMut = useReplyTicketMutation();
  const uploadMut = useUploadTicketAttachmentMutation();
  const quickRepliesQuery = useTicketQuickRepliesQuery(ticket.appid);
  const quickReplies = (quickRepliesQuery.data ?? []).filter((item) => item.enabled);

  const saveDraft = (patch: Partial<Draft>) => {
    const next = { content, mode, files, nextStatus, ...patch };
    if (!next.content && !next.files.length && !next.nextStatus) drafts.delete(ticket.id);
    else drafts.set(ticket.id, next);
  };

  const updateContent = (value: string) => {
    setContent(value);
    saveDraft({ content: value });
  };

  const internal = mode === "internal";

  const upload = async (list: File[]) => {
    if (!list.length || !perms?.uploadAttachment) return;
    setUploading((count) => count + list.length);
    const done: TicketAttachment[] = [];
    for (const file of list) {
      try {
        done.push(await uploadMut.mutateAsync({ file, appid: ticket.appid, ticketId: ticket.id }));
      } catch (error) {
        notify.error(error instanceof ApiError ? error.message : `${file.name} 上传失败`);
      } finally {
        setUploading((count) => count - 1);
      }
    }
    if (done.length) {
      setFiles((prev) => {
        const next = [...prev, ...done];
        saveDraft({ files: next });
        return next;
      });
    }
  };

  const { getRootProps, getInputProps, isDragActive, open: openFilePicker } = useDropzone({
    onDrop: (accepted) => void upload(accepted),
    noClick: true,
    noKeyboard: true,
    disabled: !perms?.uploadAttachment
  });

  const removeFile = (id: number) => {
    setFiles((prev) => {
      const next = prev.filter((file) => file.id !== id);
      saveDraft({ files: next });
      return next;
    });
  };

  const insertQuickReply = (text: string) => {
    const value = content ? `${content.trimEnd()}\n${text}` : text;
    updateContent(value);
    setQuickOpen(false);
    requestAnimationFrame(() => {
      const el = localRef.current;
      if (el) {
        el.focus();
        el.setSelectionRange(value.length, value.length);
      }
    });
  };

  const send = async (statusOverride?: string) => {
    const text = content.trim();
    if (!text) {
      notify.warning(internal ? "请填写备注内容" : "请填写回复内容");
      return;
    }
    if (uploading > 0) {
      notify.warning("附件还在上传，请稍候");
      return;
    }
    const status = statusOverride ?? nextStatus;
    try {
      await replyMut.mutateAsync({
        id: ticket.id,
        payload: {
          content: text,
          internal,
          nextStatus: internal ? "" : ((status as TicketStatus) || ""),
          attachmentIds: files.map((file) => file.id)
        }
      });
      setContent("");
      setFiles([]);
      setNextStatus("");
      drafts.delete(ticket.id);
      notify.success(internal ? "内部备注已添加" : status ? `已回复，状态改为「${STATUS_LABEL[status as TicketStatus]}」` : "回复已发送");
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "发送失败");
    }
  };

  if (!canReply && !canNote) {
    return (
      <div className="border-t bg-muted/30 px-5 py-4 text-center text-xs text-muted-foreground">
        {ticket.locked ? "已锁定，不能继续回复" : ticket.status === "closed" || ticket.status === "cancelled" ? "已结束，重新打开后才能回复" : "你没有回复权限"}
      </div>
    );
  }

  const nextLabel = NEXT_STATUS.find((item) => item.value === nextStatus);

  return (
    <div className="border-t bg-background px-4 pb-4 pt-3">
      <div
        {...getRootProps()}
        className={cn(
          "relative mx-auto max-w-3xl rounded-xl border bg-card shadow-xs transition-colors focus-within:border-ring/60 focus-within:ring-[3px] focus-within:ring-ring/20",
          internal && "border-amber-300 bg-amber-50/50 focus-within:border-amber-400 focus-within:ring-amber-400/20 dark:border-amber-900 dark:bg-amber-950/20"
        )}
      >
        <input {...getInputProps()} />

        {isDragActive ? (
          <div className="absolute inset-0 z-20 flex flex-col items-center justify-center gap-1 rounded-xl border-2 border-dashed border-primary bg-background/90 text-sm text-primary">
            <Upload className="size-5" />
            松开即可上传
          </div>
        ) : null}

        <div className="flex items-center justify-between gap-2 px-2 pt-2">
          <Tabs value={mode} onValueChange={(value) => {
            setMode(value as Mode);
            saveDraft({ mode: value as Mode });
          }}>
            <TabsList className="h-8">
              {canReply ? (
                <TabsTrigger value="reply" className="text-xs">
                  <MessageSquareReply className="size-3.5" />
                  回复用户
                </TabsTrigger>
              ) : null}
              {canNote ? (
                <TabsTrigger value="internal" className="text-xs">
                  <Lock className="size-3.5" />
                  内部备注
                </TabsTrigger>
              ) : null}
            </TabsList>
          </Tabs>
          <span className="hidden items-center gap-1 pr-1 text-xs text-muted-foreground sm:flex">
            <Kbd>/</Kbd> 快捷回复
            <Kbd className="ml-2">Ctrl</Kbd>
            <Kbd>Enter</Kbd> 发送
          </span>
        </div>

        <TextareaAutosize
          ref={(el) => {
            localRef.current = el;
            if (textareaRef) textareaRef.current = el;
          }}
          value={content}
          minRows={2}
          maxRows={12}
          onChange={(event) => updateContent(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
              event.preventDefault();
              void send();
            } else if (event.key === "/" && !content && quickReplies.length > 0) {
              event.preventDefault();
              setQuickOpen(true);
            } else if (event.key === "Escape") {
              event.currentTarget.blur();
            }
          }}
          onPaste={(event) => {
            const pasted = Array.from(event.clipboardData.files);
            if (pasted.length) {
              event.preventDefault();
              void upload(pasted);
            }
          }}
          placeholder={internal ? "写给同事看的备注，用户看不到" : `回复 ${ticket.requesterName || "用户"}，支持 Markdown`}
          className="block w-full resize-none bg-transparent px-3.5 py-2.5 text-sm leading-6 outline-none placeholder:text-muted-foreground"
        />

        {files.length > 0 || uploading > 0 ? (
          <div className="flex flex-wrap gap-2 px-3 pb-2">
            {files.map((file) => (
              <PendingFile key={file.id} file={file} onRemove={() => removeFile(file.id)} />
            ))}
            {uploading > 0 ? (
              <span className="inline-flex h-12 items-center gap-1.5 rounded-lg border border-dashed px-3 text-xs text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" />
                正在上传 {uploading} 个
              </span>
            ) : null}
          </div>
        ) : null}

        <div className="flex items-center gap-1 border-t px-2 py-1.5">
          {perms?.uploadAttachment ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="icon-sm" variant="ghost" onClick={openFilePicker} aria-label="添加附件">
                  <Paperclip />
                </Button>
              </TooltipTrigger>
              <TooltipContent>添加附件，也可以拖进来或直接粘贴截图</TooltipContent>
            </Tooltip>
          ) : null}

          <Popover open={quickOpen} onOpenChange={setQuickOpen}>
            <PopoverTrigger asChild>
              <Button size="sm" variant="ghost" className="text-muted-foreground" disabled={quickReplies.length === 0}>
                <Zap />
                快捷回复
              </Button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-80 p-0">
              <Command>
                <CommandInput placeholder="搜索快捷回复" />
                <CommandList className="max-h-72">
                  <CommandEmpty>没有匹配的快捷回复</CommandEmpty>
                  <CommandGroup>
                    {quickReplies.map((item) => (
                      <CommandItem
                        key={item.id}
                        value={`${item.title} ${item.content}`}
                        onSelect={() => insertQuickReply(item.content)}
                        className="flex-col items-start gap-0.5"
                      >
                        <span className="font-medium">{item.title}</span>
                        <span className="line-clamp-2 text-xs text-muted-foreground">{item.content}</span>
                      </CommandItem>
                    ))}
                  </CommandGroup>
                </CommandList>
              </Command>
            </PopoverContent>
          </Popover>

          <div className="ml-auto flex items-center gap-2">
            {!internal && nextStatus ? (
              <span className="hidden items-center gap-1.5 text-xs text-muted-foreground md:inline-flex">
                发送后转为
                <StatusDot status={nextStatus as TicketStatus} />
                {nextLabel?.label}
              </span>
            ) : null}
            <ButtonGroup>
              <Button
                size="sm"
                className={cn(internal && "bg-amber-500 text-white hover:bg-amber-500/90")}
                disabled={replyMut.isPending || !content.trim()}
                onClick={() => void send()}
              >
                {replyMut.isPending ? <Loader2 className="animate-spin" /> : <Send />}
                {internal ? "添加备注" : "发送"}
              </Button>
              {!internal && perms?.changeStatus ? (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button size="icon-sm" className="border-l border-primary-foreground/20" aria-label="发送选项">
                      <ChevronDown />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-52">
                    <DropdownMenuLabel className="text-xs text-muted-foreground">发送后状态</DropdownMenuLabel>
                    <DropdownMenuRadioGroup
                      value={nextStatus}
                      onValueChange={(value) => {
                        setNextStatus(value);
                        saveDraft({ nextStatus: value });
                      }}
                    >
                      {NEXT_STATUS.map((item) => (
                        <DropdownMenuRadioItem key={item.value || "keep"} value={item.value}>
                          {item.status ? <StatusDot status={item.status} /> : null}
                          {item.label}
                        </DropdownMenuRadioItem>
                      ))}
                    </DropdownMenuRadioGroup>
                    <DropdownMenuSeparator />
                    <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
                      选中后这一次发送生效，之后恢复为保持状态
                    </DropdownMenuLabel>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : null}
            </ButtonGroup>
          </div>
        </div>
      </div>
    </div>
  );
}

function PendingFile({ file, onRemove }: { file: TicketAttachment; onRemove: () => void }) {
  const image = isImageAttachment(file) && file.downloadUrl;
  return (
    <div className="group/file relative flex h-12 items-center gap-2 overflow-hidden rounded-lg border bg-background pr-7 text-xs">
      {image ? (
        // eslint-disable-next-line @next/next/no-img-element -- 同源存储代理地址，见 ticket-attachments 文件头注释
        <img src={attachmentUrl(file)} alt={file.fileName} className="size-12 object-cover" />
      ) : (
        <span className="flex size-12 items-center justify-center bg-muted text-muted-foreground">
          <FileText className="size-4" />
        </span>
      )}
      <span className="max-w-32 space-y-0.5">
        <span className="block truncate text-foreground">{file.fileName}</span>
        <span className="block text-muted-foreground">{formatBytes(file.sizeBytes)}</span>
      </span>
      <button
        type="button"
        onClick={onRemove}
        aria-label={`移除 ${file.fileName}`}
        className="absolute right-1 top-1 rounded-full p-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      >
        <X className="size-3.5" />
      </button>
    </div>
  );
}
