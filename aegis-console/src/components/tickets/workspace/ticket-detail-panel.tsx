"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useHotkeys } from "react-hotkeys-hook";
import {
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Copy,
  Link2,
  Lock,
  LockOpen,
  MoreHorizontal,
  PanelRightClose,
  PanelRightOpen,
  Pencil,
  RotateCcw,
  Trash2,
  X,
  XCircle
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
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
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useChangeTicketStatusMutation,
  useDeleteTicketMutation,
  useTicketCategoriesQuery,
  useTicketDetailQuery,
  useUpdateTicketMutation
} from "@/lib/ticket-hooks";
import type { TicketItem, TicketStatus } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import {
  FeedbackCategoryBadge,
  Kbd,
  PriorityBadge,
  SLABadge,
  STATUS_LABEL,
  StatusDot,
  formatClientShort,
  formatDateTime,
  formatRelativeTime,
  getTicketClient,
  isTerminalStatus
} from "../ticket-shared";
import { TicketComposer } from "./ticket-composer";
import { TicketConversation } from "./ticket-conversation";
import { TicketInspector, copyText } from "./ticket-inspector";

// 详情面板：工作台右栏，也被窄屏抽屉复用。
//
// 所有按钮的显隐都读后端返回的 `permissions`（ActionSet），不在前端按角色猜，
// 「组成员能回复但不能删除」这类规则只在服务端实现一次。

const INSPECTOR_KEY = "aegis.tickets.inspector";
// 详情面板宽于这个值时属性栏并排显示，否则改成覆盖在会话上的浮层
const WIDE_PANEL = 880;

type Props = {
  ticketId: number;
  onClose: () => void;
  onStep?: (direction: 1 | -1) => void;
  canStep?: { prev: boolean; next: boolean };
  composerRef?: React.RefObject<HTMLTextAreaElement | null>;
};

function subscribeStorage(callback: () => void) {
  window.addEventListener("storage", callback);
  return () => window.removeEventListener("storage", callback);
}

function useElementWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return [ref, width] as const;
}

export function TicketDetailPanel({ ticketId, onClose, onStep, canStep, composerRef }: Props) {
  const detailQuery = useTicketDetailQuery(ticketId);
  const ticket = detailQuery.data;

  const [bodyRef, bodyWidth] = useElementWidth<HTMLDivElement>();
  const wide = bodyWidth >= WIDE_PANEL;
  // 宽屏时是否并排显示属性栏：记在本机，下次打开保持
  const inspectorPinned = useSyncExternalStore(
    subscribeStorage,
    () => window.localStorage.getItem(INSPECTOR_KEY) !== "0",
    () => true
  );
  const [overlayOpen, setOverlayOpen] = useState(false);
  const inspectorVisible = wide ? inspectorPinned : overlayOpen;

  const toggleInspector = () => {
    if (wide) {
      window.localStorage.setItem(INSPECTOR_KEY, inspectorPinned ? "0" : "1");
      window.dispatchEvent(new StorageEvent("storage", { key: INSPECTOR_KEY }));
    } else {
      setOverlayOpen((open) => !open);
    }
  };

  useHotkeys("i", toggleInspector, [wide, inspectorPinned]);

  if (detailQuery.isError) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
        <XCircle className="size-8 text-muted-foreground" />
        <p className="text-sm text-foreground">
          {detailQuery.error instanceof ApiError ? detailQuery.error.message : "详情加载失败"}
        </p>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={() => detailQuery.refetch()}>
            重试
          </Button>
          <Button size="sm" variant="ghost" onClick={onClose}>
            关闭
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      {ticket ? (
        <DetailHeader
          key={ticket.id}
          ticket={ticket}
          onClose={onClose}
          onStep={onStep}
          canStep={canStep}
          inspectorVisible={inspectorVisible}
          onToggleInspector={toggleInspector}
        />
      ) : (
        <HeaderSkeleton />
      )}

      <div ref={bodyRef} className="relative flex min-h-0 flex-1">
        {ticket ? (
          <>
            <div className="flex min-w-0 flex-1 flex-col">
              <TicketConversation key={`c-${ticket.id}`} ticket={ticket} />
              <TicketComposer key={`r-${ticket.id}`} ticket={ticket} textareaRef={composerRef} />
            </div>
            {inspectorVisible ? (
              <aside
                className={cn(
                  "flex min-h-0 flex-col border-l bg-background",
                  wide ? "w-80 shrink-0" : "absolute inset-y-0 right-0 z-30 w-[min(20rem,100%)] shadow-xl"
                )}
              >
                {!wide ? (
                  <div className="flex items-center justify-between border-b px-4 py-2">
                    <span className="text-sm font-medium">详细信息</span>
                    <Button size="icon-sm" variant="ghost" onClick={() => setOverlayOpen(false)} aria-label="收起详细信息">
                      <X />
                    </Button>
                  </div>
                ) : null}
                <TicketInspector ticket={ticket} />
              </aside>
            ) : null}
          </>
        ) : (
          <BodySkeleton />
        )}
      </div>
    </div>
  );
}

function DetailHeader({
  ticket,
  onClose,
  onStep,
  canStep,
  inspectorVisible,
  onToggleInspector
}: {
  ticket: TicketItem;
  onClose: () => void;
  onStep?: (direction: 1 | -1) => void;
  canStep?: { prev: boolean; next: boolean };
  inspectorVisible: boolean;
  onToggleInspector: () => void;
}) {
  const perms = ticket.permissions;
  const isFeedback = ticket.kind === "feedback";
  const noun = isFeedback ? "反馈" : "工单";
  const client = formatClientShort(getTicketClient(ticket.metadata));
  const categoriesQuery = useTicketCategoriesQuery(ticket.appid);
  const categoryKey = (categoriesQuery.data ?? []).find((item) => item.id === ticket.categoryId)?.key;

  const statusMut = useChangeTicketStatusMutation();
  const updateMut = useUpdateTicketMutation();
  const deleteMut = useDeleteTicketMutation();

  const [editingTitle, setEditingTitle] = useState(false);
  const [titleDraft, setTitleDraft] = useState(ticket.title);
  const [solution, setSolution] = useState("");
  const [resolveOpen, setResolveOpen] = useState(false);
  const [confirm, setConfirm] = useState<"delete" | "close" | null>(null);

  const terminal = isTerminalStatus(ticket.status);

  const changeStatus = async (status: TicketStatus, extra?: { solution?: string }) => {
    try {
      await statusMut.mutateAsync({ id: ticket.id, status, solution: extra?.solution });
      notify.success(`已改为「${STATUS_LABEL[status]}」`);
      return true;
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "状态更新失败");
      return false;
    }
  };

  const saveTitle = async () => {
    const next = titleDraft.trim();
    setEditingTitle(false);
    if (!next || next === ticket.title) return;
    try {
      await updateMut.mutateAsync({ id: ticket.id, payload: { title: next } });
      notify.success("标题已更新");
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "标题保存失败");
    }
  };

  const toggleLock = async () => {
    try {
      await updateMut.mutateAsync({ id: ticket.id, payload: { locked: !ticket.locked } });
      notify.success(ticket.locked ? "已解除锁定" : "已锁定，用户不能再回复");
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const remove = async () => {
    try {
      await deleteMut.mutateAsync(ticket.id);
      notify.success(`${noun}已删除`);
      onClose();
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  // 可直接切换到的状态：结束类状态需要对应权限
  const statusOptions = (
    ["open", "processing", "pending_user", "pending_third_party", "resolved", "closed"] as TicketStatus[]
  ).filter((status) => status !== "closed" || perms?.close || ticket.status === "closed");

  return (
    <header className="border-b px-5 pb-3 pt-3">
      <div className="flex items-center gap-2">
        {perms?.changeStatus ? (
          <Select
            value={ticket.status}
            onValueChange={(value) => {
              if (value === "closed") setConfirm("close");
              else void changeStatus(value as TicketStatus);
            }}
          >
            <SelectTrigger size="sm" className="h-7 w-auto gap-1.5 rounded-full px-2.5 text-xs font-medium">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {statusOptions.map((status) => (
                <SelectItem key={status} value={status}>
                  <StatusDot status={status} />
                  {STATUS_LABEL[status]}
                </SelectItem>
              ))}
              {ticket.status === "cancelled" ? (
                <SelectItem value="cancelled" disabled>
                  <StatusDot status="cancelled" />
                  {STATUS_LABEL.cancelled}
                </SelectItem>
              ) : null}
            </SelectContent>
          </Select>
        ) : (
          <Badge variant="outline" className="gap-1.5">
            <StatusDot status={ticket.status} />
            {STATUS_LABEL[ticket.status]}
          </Badge>
        )}
        {isFeedback ? <FeedbackCategoryBadge name={ticket.categoryName} categoryKey={categoryKey} /> : null}
        {ticket.priority !== "normal" ? <PriorityBadge priority={ticket.priority} /> : null}
        <SLABadge state={ticket.slaState} />
        {ticket.locked ? (
          <Badge variant="secondary" size="sm">
            <Lock />
            已锁定
          </Badge>
        ) : null}

        <div className="ml-auto flex items-center gap-0.5">
          {onStep ? (
            <>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button size="icon-sm" variant="ghost" disabled={!canStep?.prev} onClick={() => onStep(-1)} aria-label="上一条">
                    <ChevronUp />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  上一条 <Kbd className="ml-1">K</Kbd>
                </TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button size="icon-sm" variant="ghost" disabled={!canStep?.next} onClick={() => onStep(1)} aria-label="下一条">
                    <ChevronDown />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  下一条 <Kbd className="ml-1">J</Kbd>
                </TooltipContent>
              </Tooltip>
              <Separator orientation="vertical" className="mx-1 h-4" />
            </>
          ) : null}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button size="icon-sm" variant="ghost" onClick={onToggleInspector} aria-label="详细信息">
                {inspectorVisible ? <PanelRightClose /> : <PanelRightOpen />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              {inspectorVisible ? "收起详细信息" : "展开详细信息"} <Kbd className="ml-1">I</Kbd>
            </TooltipContent>
          </Tooltip>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon-sm" variant="ghost" aria-label="更多操作">
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem onSelect={() => copyText(ticket.ticketNo, "编号")}>
                <Copy />
                复制编号
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={() => copyText(`${window.location.origin}${window.location.pathname}?id=${ticket.id}`, "链接")}
              >
                <Link2 />
                复制链接
              </DropdownMenuItem>
              {perms?.edit ? (
                <DropdownMenuItem onSelect={() => void toggleLock()}>
                  {ticket.locked ? <LockOpen /> : <Lock />}
                  {ticket.locked ? "解除锁定" : "锁定"}
                </DropdownMenuItem>
              ) : null}
              {perms?.close && ticket.status !== "closed" ? (
                <DropdownMenuItem onSelect={() => setConfirm("close")}>
                  <XCircle />
                  关闭{noun}
                </DropdownMenuItem>
              ) : null}
              {perms?.delete ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onSelect={() => setConfirm("delete")}>
                    <Trash2 />
                    删除{noun}
                  </DropdownMenuItem>
                </>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button size="icon-sm" variant="ghost" onClick={onClose} aria-label="关闭">
                <X />
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              关闭 <Kbd className="ml-1">Esc</Kbd>
            </TooltipContent>
          </Tooltip>
        </div>
      </div>

      <div className="mt-2 flex items-start gap-3">
        <div className="min-w-0 flex-1">
          {editingTitle ? (
            <Input
              autoFocus
              value={titleDraft}
              onChange={(event) => setTitleDraft(event.target.value)}
              onBlur={() => void saveTitle()}
              onKeyDown={(event) => {
                if (event.key === "Enter") void saveTitle();
                if (event.key === "Escape") {
                  setTitleDraft(ticket.title);
                  setEditingTitle(false);
                }
              }}
              className="h-8 text-base font-semibold"
            />
          ) : (
            <h2 className="group/title flex items-center gap-1.5 text-base font-semibold leading-8 text-foreground">
              <span className="truncate">{ticket.title}</span>
              {perms?.edit ? (
                <button
                  type="button"
                  onClick={() => {
                    setTitleDraft(ticket.title);
                    setEditingTitle(true);
                  }}
                  aria-label="修改标题"
                  className="rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-muted group-hover/title:opacity-100"
                >
                  <Pencil className="size-3.5" />
                </button>
              ) : null}
            </h2>
          )}
          <p className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
            <span className="font-medium text-foreground/80">{ticket.requesterName || "匿名用户"}</span>
            {ticket.appName ? <span>{ticket.appName}</span> : null}
            {client ? <span>{client}</span> : null}
            <span title={formatDateTime(ticket.createdAt)}>{formatRelativeTime(ticket.createdAt)}提交</span>
            <span className="font-mono">{ticket.ticketNo}</span>
          </p>
        </div>

        {perms?.changeStatus && !terminal ? (
          <Popover open={resolveOpen} onOpenChange={setResolveOpen}>
            <PopoverTrigger asChild>
              <Button size="sm" className="shrink-0">
                <CheckCircle2 />
                解决
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-80 space-y-3">
              <div className="space-y-1">
                <p className="text-sm font-medium">标记为已解决</p>
                <p className="text-xs text-muted-foreground">解决说明会展示给{isFeedback ? "反馈人" : "提单人"}，可以不填。</p>
              </div>
              <Textarea
                autoFocus
                rows={3}
                value={solution}
                onChange={(event) => setSolution(event.target.value)}
                onKeyDown={async (event) => {
                  if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                    event.preventDefault();
                    if (await changeStatus("resolved", { solution: solution.trim() || undefined })) {
                      setSolution("");
                      setResolveOpen(false);
                    }
                  }
                }}
                placeholder="例如：已在 1.0.21 修复，更新后即可正常使用"
              />
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="ghost" onClick={() => setResolveOpen(false)}>
                  取消
                </Button>
                <Button
                  size="sm"
                  disabled={statusMut.isPending}
                  onClick={async () => {
                    if (await changeStatus("resolved", { solution: solution.trim() || undefined })) {
                      setSolution("");
                      setResolveOpen(false);
                    }
                  }}
                >
                  确认解决
                </Button>
              </div>
            </PopoverContent>
          </Popover>
        ) : perms?.reopen ? (
          <Button size="sm" variant="outline" className="shrink-0" onClick={() => void changeStatus("processing")}>
            <RotateCcw />
            重新打开
          </Button>
        ) : null}
      </div>

      <AlertDialog open={confirm !== null} onOpenChange={(open) => !open && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirm === "delete" ? `删除这条${noun}？` : `关闭这条${noun}？`}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirm === "delete"
                ? `「${ticket.title}」及其全部会话、附件会被永久删除，无法恢复。`
                : `关闭后${isFeedback ? "反馈人" : "提单人"}不能再回复，需要时可以重新打开。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className={cn(confirm === "delete" && "bg-destructive text-white hover:bg-destructive/90")}
              onClick={() => {
                if (confirm === "delete") void remove();
                else void changeStatus("closed");
              }}
            >
              {confirm === "delete" ? "删除" : `关闭${noun}`}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </header>
  );
}

function HeaderSkeleton() {
  return (
    <div className="space-y-3 border-b px-5 py-3">
      <div className="flex gap-2">
        <Skeleton className="h-7 w-20 rounded-full" />
        <Skeleton className="h-7 w-16 rounded-full" />
      </div>
      <Skeleton className="h-6 w-2/3" />
      <Skeleton className="h-3 w-1/2" />
    </div>
  );
}

function BodySkeleton() {
  return (
    <div className="flex-1 space-y-5 p-6">
      {[0, 1, 2].map((index) => (
        <div key={index} className={cn("flex gap-3", index === 1 && "flex-row-reverse")}>
          <Skeleton className="size-8 shrink-0 rounded-full" />
          <Skeleton className={cn("h-16 rounded-2xl", index === 1 ? "w-1/2" : "w-2/3")} />
        </div>
      ))}
    </div>
  );
}
