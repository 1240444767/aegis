"use client";

import { useCallback, useDeferredValue, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { useHotkeys } from "react-hotkeys-hook";
import {
  AlarmClock,
  ArrowDownUp,
  Check,
  Inbox,
  Keyboard,
  ListFilter,
  MousePointerClick,
  RefreshCw,
  Search,
  Trash2,
  UserCheck,
  X,
  XCircle
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { useAuthStore } from "@/lib/auth-store";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
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
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
  DropdownMenuItem
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ResizableGroup, ResizableHandle, ResizablePanel, usePanelLayout } from "@/components/ui/resizable";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useBulkTicketsMutation,
  useTicketCategoriesQuery,
  useTicketsInfiniteQuery
} from "@/lib/ticket-hooks";
import type { TicketKind, TicketListParams, TicketPriority, TicketStats, TicketStatus } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { Kbd, PRIORITY_LABEL, STATUS_LABEL, StatusDot } from "../ticket-shared";
import { TicketDetailPanel } from "./ticket-detail-panel";
import { TicketList, TicketListEmpty, TicketListSkeleton } from "./ticket-list";

// 服务台工作台：左边列表、右边详情，工单中心与意见反馈共用。
//
// 详情就地打开，不再用抽屉盖住列表：处理完一条按 J 直接看下一条。
// 窄屏（< lg）退回「列表全宽 + 详情抽屉」。打开哪一条由调用方经 URL 管理。

type View = "active" | "open" | "processing" | "pending_user" | "resolved" | "closed" | "all";
type Sort = "updated" | "created" | "priority" | "due";

const SORT_LABEL: Record<Sort, string> = {
  updated: "最近更新",
  created: "最新提交",
  priority: "优先级从高到低",
  due: "即将到期"
};

type Filters = {
  appId: string;
  categoryId: string;
  priority: string;
  unassigned: boolean;
  overdueOnly: boolean;
};

const EMPTY_FILTERS: Filters = { appId: "all", categoryId: "all", priority: "all", unassigned: false, overdueOnly: false };

function subscribeMedia(callback: () => void) {
  const media = window.matchMedia("(min-width: 1024px)");
  media.addEventListener("change", callback);
  return () => media.removeEventListener("change", callback);
}

function useIsDesktop() {
  return useSyncExternalStore(subscribeMedia, () => window.matchMedia("(min-width: 1024px)").matches, () => true);
}

type Props = {
  kind: TicketKind;
  selectedId: number | null;
  onSelect: (id: number | null) => void;
  stats?: TicketStats;
  mineOnly?: boolean;
  /** 开启批量勾选 */
  selectable?: boolean;
  /** 工具栏右侧额外按钮，例如「新建工单」 */
  actions?: ReactNode;
};

export function TicketWorkspace({ kind, selectedId, onSelect, stats, mineOnly, selectable, actions }: Props) {
  const noun = kind === "feedback" ? "反馈" : "工单";
  const isDesktop = useIsDesktop();
  const layout = usePanelLayout({ id: `aegis-${kind}-workspace`, panelIds: ["list", "detail"] });
  const operatorId = useAuthStore((state) => Number(state.operator?.id) || 0);

  const [keyword, setKeyword] = useState("");
  const deferredKeyword = useDeferredValue(keyword.trim());
  const [view, setView] = useState<View>("active");
  const [sort, setSort] = useState<Sort>("updated");
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS);
  const [checked, setChecked] = useState<Set<number>>(() => new Set());
  const [confirmBulk, setConfirmBulk] = useState<"close" | "delete" | null>(null);

  const searchRef = useRef<HTMLInputElement>(null);
  const composerRef = useRef<HTMLTextAreaElement | null>(null);

  const appsQuery = useAdminAppsQuery();
  const numericAppId = filters.appId === "all" ? undefined : Number(filters.appId);
  // 指定应用时后端返回「该应用 + 平台级」分类；未指定时只有平台级
  const categoriesQuery = useTicketCategoriesQuery(numericAppId ?? 0);
  const categories = useMemo(
    () => (categoriesQuery.data ?? []).filter((item) => (item.kind ?? "ticket") === kind),
    [categoriesQuery.data, kind]
  );
  const categoryKeyById = useMemo(() => new Map(categories.map((item) => [item.id, item.key])), [categories]);

  const params = useMemo<Omit<TicketListParams, "page">>(() => {
    const query: Omit<TicketListParams, "page"> = {
      kind,
      keyword: deferredKeyword || undefined,
      appid: numericAppId,
      categoryId: filters.categoryId === "all" ? undefined : Number(filters.categoryId),
      priority: filters.priority === "all" ? undefined : filters.priority,
      unassigned: filters.unassigned || undefined,
      overdueOnly: filters.overdueOnly || undefined,
      mine: mineOnly || undefined,
      sortBy: sort === "updated" ? undefined : sort,
      sortDir: sort === "due" ? "asc" : undefined,
      limit: 30
    };
    // active 是默认视图：后端不传状态时本来就排除终态
    if (view === "all") query.includeClosed = true;
    else if (view !== "active") query.status = view;
    return query;
  }, [kind, deferredKeyword, numericAppId, filters, mineOnly, sort, view]);

  const listQuery = useTicketsInfiniteQuery(params);
  const items = useMemo(() => listQuery.data?.pages.flatMap((page) => page.items) ?? [], [listQuery.data]);
  const total = listQuery.data?.pages[0]?.total ?? 0;
  const scope = listQuery.data?.pages[0]?.scope;

  const activeFilterCount =
    (filters.appId !== "all" ? 1 : 0) +
    (filters.categoryId !== "all" ? 1 : 0) +
    (filters.priority !== "all" ? 1 : 0) +
    (filters.unassigned ? 1 : 0) +
    (filters.overdueOnly ? 1 : 0);
  const filtered = activeFilterCount > 0 || Boolean(deferredKeyword) || view !== "active";

  const selectedIndex = selectedId ? items.findIndex((item) => item.id === selectedId) : -1;
  const step = useCallback(
    (direction: 1 | -1) => {
      if (!items.length) return;
      const next = selectedIndex < 0 ? 0 : selectedIndex + direction;
      if (next >= 0 && next < items.length) onSelect(items[next].id);
    },
    [items, selectedIndex, onSelect]
  );

  const toggleCheck = useCallback((id: number) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  // ── 快捷键：输入框里不触发（库默认行为） ──
  useHotkeys("j", () => step(1), [step]);
  useHotkeys("k", () => step(-1), [step]);
  useHotkeys("slash", () => searchRef.current?.focus(), { preventDefault: true });
  useHotkeys("r", () => composerRef.current?.focus(), { enabled: Boolean(selectedId), preventDefault: true });
  useHotkeys(
    "escape",
    (event) => {
      // 弹层、菜单、确认框开着时 Esc 只关它们，不连带关掉详情
      const overlay = document.querySelector('[data-radix-popper-content-wrapper], [role="alertdialog"], [role="dialog"][data-state="open"]');
      if (!event.defaultPrevented && !overlay) onSelect(null);
    },
    { enabled: Boolean(selectedId) && isDesktop }
  );
  useHotkeys("x", () => selectedId && toggleCheck(selectedId), { enabled: Boolean(selectable && selectedId) }, [selectedId]);

  const bulkMut = useBulkTicketsMutation();
  const runBulk = async (payload: Omit<Parameters<typeof bulkMut.mutateAsync>[0], "ids">, done: string) => {
    const ids = Array.from(checked);
    if (!ids.length) return;
    try {
      const result = await bulkMut.mutateAsync({ ids, ...payload });
      setChecked(new Set());
      if (result.failed?.length) {
        notify.warning(`成功 ${result.succeeded} 条，失败 ${result.failed.length} 条`, { description: result.failed[0]?.reason });
      } else {
        notify.success(`${done} ${result.succeeded} 条${noun}`);
      }
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "批量操作失败");
    }
  };

  const views: { value: View; label: string; count?: number }[] = [
    { value: "active", label: "未结单", count: stats ? stats.open + stats.processing + stats.pendingUser : undefined },
    { value: "open", label: STATUS_LABEL.open, count: stats?.open },
    { value: "processing", label: STATUS_LABEL.processing, count: stats?.processing },
    { value: "pending_user", label: kind === "feedback" ? "待补充" : STATUS_LABEL.pending_user, count: stats?.pendingUser },
    { value: "resolved", label: STATUS_LABEL.resolved, count: stats?.resolved },
    { value: "closed", label: STATUS_LABEL.closed, count: stats?.closed },
    { value: "all", label: "全部", count: stats?.total }
  ];

  const listPane = (
    <div className="flex h-full min-h-0 flex-col">
      {/* 搜索与工具 */}
      <div className="flex items-center gap-1.5 border-b p-2.5">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={searchRef}
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                setKeyword("");
                event.currentTarget.blur();
              }
            }}
            placeholder={kind === "feedback" ? "搜索编号、标题、反馈人或内容" : "搜索工单号、标题、提单人或内容"}
            className="h-8 pl-8 pr-8 text-sm"
          />
          {keyword ? (
            <button
              type="button"
              onClick={() => setKeyword("")}
              aria-label="清空搜索"
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          ) : (
            <Kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2">/</Kbd>
          )}
        </div>

        <FilterPopover
          kind={kind}
          filters={filters}
          count={activeFilterCount}
          onChange={setFilters}
          apps={appsQuery.data ?? []}
          categories={categories}
        />

        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button size="icon-sm" variant="ghost" aria-label="排序">
                  <ArrowDownUp />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>排序：{SORT_LABEL[sort]}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuLabel className="text-xs text-muted-foreground">排序方式</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={sort} onValueChange={(value) => setSort(value as Sort)}>
              {(Object.keys(SORT_LABEL) as Sort[]).map((value) => (
                <DropdownMenuRadioItem key={value} value={value}>
                  {SORT_LABEL[value]}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button size="icon-sm" variant="ghost" onClick={() => listQuery.refetch()} aria-label="刷新">
              <RefreshCw className={cn(listQuery.isRefetching && !listQuery.isFetchingNextPage && "animate-spin")} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>刷新列表</TooltipContent>
        </Tooltip>
        {actions}
      </div>

      {/* 状态视图 */}
      <div className="flex gap-1 overflow-x-auto border-b px-2.5 py-2 [scrollbar-width:none]">
        {views.map((item) => (
          <button
            key={item.value}
            type="button"
            onClick={() => {
              setView(item.value);
              setChecked(new Set());
            }}
            className={cn(
              "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium transition-colors",
              view === item.value ? "bg-foreground text-background" : "text-muted-foreground hover:bg-muted hover:text-foreground"
            )}
          >
            {item.value !== "active" && item.value !== "all" ? <StatusDot status={item.value as TicketStatus} /> : null}
            {item.label}
            {item.count !== undefined ? (
              <span className={cn("tabular-nums", view === item.value ? "text-background/70" : "text-muted-foreground/70")}>
                {item.count}
              </span>
            ) : null}
          </button>
        ))}
      </div>

      {/* 已生效的筛选条件，一眼可见、一键去掉 */}
      {activeFilterCount > 0 ? (
        <ActiveFilters
          filters={filters}
          onChange={setFilters}
          apps={appsQuery.data ?? []}
          categories={categories}
        />
      ) : null}

      {scope && scope.level !== "all" ? (
        <p className="border-b bg-muted/40 px-4 py-1.5 text-xs text-muted-foreground">{scope.label}</p>
      ) : null}

      {/* 批量操作条 */}
      {selectable && checked.size > 0 ? (
        <div className="flex flex-wrap items-center gap-1.5 border-b bg-primary/5 px-3 py-2 text-xs">
          <span className="font-medium text-foreground">已选 {checked.size} 条</span>
          <Button size="xs" variant="ghost" onClick={() => setChecked(new Set(items.map((item) => item.id)))}>
            全选已加载
          </Button>
          <div className="ml-auto flex items-center gap-1">
            {operatorId ? (
              <Button size="xs" variant="outline" onClick={() => void runBulk({ action: "assign", assigneeAdminId: operatorId }, "已指派给你")}>
                <UserCheck />
                指派给我
              </Button>
            ) : null}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="xs" variant="outline">
                  更多
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>设置优先级</DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {(["urgent", "high", "normal", "low"] as TicketPriority[]).map((priority) => (
                      <DropdownMenuItem
                        key={priority}
                        onSelect={() => void runBulk({ action: "priority", priority }, `已改为「${PRIORITY_LABEL[priority]}」`)}
                      >
                        {PRIORITY_LABEL[priority]}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
                <DropdownMenuItem onSelect={() => setConfirmBulk("close")}>
                  <XCircle />
                  批量关闭
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={() => setConfirmBulk("delete")}>
                  <Trash2 />
                  批量删除
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button size="icon-xs" variant="ghost" onClick={() => setChecked(new Set())} aria-label="取消选择">
              <X />
            </Button>
          </div>
        </div>
      ) : null}

      <div className="relative min-h-0 flex-1">
        {listQuery.isLoading ? (
          <TicketListSkeleton />
        ) : listQuery.isError ? (
          <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
            <p className="text-sm text-foreground">
              {listQuery.error instanceof ApiError ? listQuery.error.message : "列表加载失败"}
            </p>
            <Button size="sm" variant="outline" onClick={() => listQuery.refetch()}>
              重试
            </Button>
          </div>
        ) : (
          <TicketList
            items={items}
            kind={kind}
            selectedId={selectedId}
            onOpen={onSelect}
            categoryKeyById={categoryKeyById}
            selectable={selectable}
            checked={checked}
            onToggleCheck={toggleCheck}
            hasMore={Boolean(listQuery.hasNextPage)}
            loadingMore={listQuery.isFetchingNextPage}
            onLoadMore={() => void listQuery.fetchNextPage()}
            empty={<TicketListEmpty filtered={filtered} kind={kind} />}
          />
        )}
        {listQuery.isPlaceholderData ? (
          <div className="pointer-events-none absolute inset-x-0 top-0 h-0.5 animate-pulse bg-primary/60" />
        ) : null}
      </div>

      <div className="flex items-center justify-between border-t px-4 py-1.5 text-xs text-muted-foreground">
        <span>
          {items.length > 0 ? `已加载 ${items.length} / ${total} 条` : listQuery.isLoading ? "加载中" : `共 ${total} 条`}
        </span>
        <ShortcutsPopover selectable={selectable} />
      </div>
    </div>
  );

  const detail = selectedId ? (
    <TicketDetailPanel
      ticketId={selectedId}
      onClose={() => onSelect(null)}
      onStep={step}
      canStep={{ prev: selectedIndex > 0, next: selectedIndex >= 0 && selectedIndex < items.length - 1 }}
      composerRef={composerRef}
    />
  ) : (
    <EmptyDetail kind={kind} stats={stats} />
  );

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden rounded-xl border bg-card">
      {isDesktop ? (
        <ResizableGroup orientation="horizontal" defaultLayout={layout.defaultLayout} onLayoutChanged={layout.onLayoutChanged}>
          <ResizablePanel id="list" defaultSize={420} minSize={320} maxSize="55%">
            {listPane}
          </ResizablePanel>
          <ResizableHandle />
          <ResizablePanel id="detail" minSize="40">
            {detail}
          </ResizablePanel>
        </ResizableGroup>
      ) : (
        <>
          <div className="min-w-0 flex-1">{listPane}</div>
          <Sheet open={Boolean(selectedId)} onOpenChange={(open) => !open && onSelect(null)}>
            <SheetContent side="right" showCloseButton={false} className="w-full gap-0 p-0 sm:max-w-2xl">
              <SheetTitle className="sr-only">{noun}详情</SheetTitle>
              {selectedId ? detail : null}
            </SheetContent>
          </Sheet>
        </>
      )}

      <AlertDialog open={confirmBulk !== null} onOpenChange={(open) => !open && setConfirmBulk(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirmBulk === "delete" ? `删除选中的 ${checked.size} 条${noun}？` : `关闭选中的 ${checked.size} 条${noun}？`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirmBulk === "delete" ? "会话与附件会一并删除，无法恢复。" : "关闭后用户不能再回复，需要时可以逐条重新打开。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className={cn(confirmBulk === "delete" && "bg-destructive text-white hover:bg-destructive/90")}
              onClick={() =>
                void (confirmBulk === "delete"
                  ? runBulk({ action: "delete" }, "已删除")
                  : runBulk({ action: "close" }, "已关闭"))
              }
            >
              {confirmBulk === "delete" ? "删除" : "关闭"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ─────────────── 筛选 ───────────────

type AppOption = { id: number; name: string };
type CategoryOption = { id: number; name: string };

function FilterPopover({
  kind,
  filters,
  count,
  onChange,
  apps,
  categories
}: {
  kind: TicketKind;
  filters: Filters;
  count: number;
  onChange: (filters: Filters) => void;
  apps: AppOption[];
  categories: CategoryOption[];
}) {
  const set = (patch: Partial<Filters>) => onChange({ ...filters, ...patch });
  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <Button size={count ? "sm" : "icon-sm"} variant={count ? "secondary" : "ghost"} aria-label="筛选">
              <ListFilter />
              {count ? count : null}
            </Button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>筛选</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" className="w-72 space-y-3">
        <div className="flex items-center justify-between">
          <p className="text-sm font-medium">筛选</p>
          {count ? (
            <Button size="xs" variant="ghost" onClick={() => onChange(EMPTY_FILTERS)}>
              清除全部
            </Button>
          ) : null}
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">应用</Label>
          <Select
            value={filters.appId}
            // 切换应用后原分类可能不在新列表里，一并复位
            onValueChange={(value) => set({ appId: value, categoryId: "all" })}
          >
            <SelectTrigger size="sm" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部应用</SelectItem>
              {apps.map((app) => (
                <SelectItem key={app.id} value={String(app.id)}>
                  {app.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">分类</Label>
          <Select value={filters.categoryId} onValueChange={(value) => set({ categoryId: value })}>
            <SelectTrigger size="sm" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部分类</SelectItem>
              {categories.map((category) => (
                <SelectItem key={category.id} value={String(category.id)}>
                  {category.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">优先级</Label>
          <Select value={filters.priority} onValueChange={(value) => set({ priority: value })}>
            <SelectTrigger size="sm" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部优先级</SelectItem>
              {(["urgent", "high", "normal", "low"] as TicketPriority[]).map((priority) => (
                <SelectItem key={priority} value={priority}>
                  {PRIORITY_LABEL[priority]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <label className="flex items-center justify-between gap-3 text-sm">
          <span>只看未指派</span>
          <Switch checked={filters.unassigned} onCheckedChange={(value) => set({ unassigned: value })} />
        </label>
        {kind === "ticket" ? (
          <label className="flex items-center justify-between gap-3 text-sm">
            <span className="inline-flex items-center gap-1.5">
              <AlarmClock className="size-3.5 text-muted-foreground" />
              只看已超时
            </span>
            <Switch checked={filters.overdueOnly} onCheckedChange={(value) => set({ overdueOnly: value })} />
          </label>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}

function ActiveFilters({
  filters,
  onChange,
  apps,
  categories
}: {
  filters: Filters;
  onChange: (filters: Filters) => void;
  apps: AppOption[];
  categories: CategoryOption[];
}) {
  const chips: { key: string; label: string; clear: Partial<Filters> }[] = [];
  if (filters.appId !== "all") {
    chips.push({
      key: "app",
      label: apps.find((app) => String(app.id) === filters.appId)?.name ?? `应用 #${filters.appId}`,
      clear: { appId: "all", categoryId: "all" }
    });
  }
  if (filters.categoryId !== "all") {
    chips.push({
      key: "category",
      label: categories.find((item) => String(item.id) === filters.categoryId)?.name ?? "分类",
      clear: { categoryId: "all" }
    });
  }
  if (filters.priority !== "all") {
    chips.push({ key: "priority", label: `优先级：${PRIORITY_LABEL[filters.priority as TicketPriority]}`, clear: { priority: "all" } });
  }
  if (filters.unassigned) chips.push({ key: "unassigned", label: "未指派", clear: { unassigned: false } });
  if (filters.overdueOnly) chips.push({ key: "overdue", label: "已超时", clear: { overdueOnly: false } });

  return (
    <div className="flex flex-wrap items-center gap-1.5 border-b px-3 py-2">
      {chips.map((chip) => (
        <Badge key={chip.key} variant="secondary" className="gap-1 pr-1">
          {chip.label}
          <button
            type="button"
            aria-label={`去掉 ${chip.label}`}
            onClick={() => onChange({ ...filters, ...chip.clear })}
            className="rounded-full p-0.5 transition-colors hover:bg-background"
          >
            <X className="size-3" />
          </button>
        </Badge>
      ))}
      <button
        type="button"
        onClick={() => onChange(EMPTY_FILTERS)}
        className="text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
      >
        清除
      </button>
    </div>
  );
}

// ─────────────── 快捷键与空态 ───────────────

const SHORTCUTS: { keys: string[]; label: string; bulk?: boolean }[] = [
  { keys: ["J"], label: "下一条" },
  { keys: ["K"], label: "上一条" },
  { keys: ["/"], label: "搜索" },
  { keys: ["R"], label: "回复" },
  { keys: ["I"], label: "展开或收起详细信息" },
  { keys: ["Ctrl", "Enter"], label: "发送回复" },
  { keys: ["X"], label: "勾选当前条", bulk: true },
  { keys: ["Esc"], label: "关闭详情" }
];

function ShortcutList({ selectable }: { selectable?: boolean }) {
  return (
    <ul className="space-y-1.5">
      {SHORTCUTS.filter((item) => !item.bulk || selectable).map((item) => (
        <li key={item.label} className="flex items-center justify-between gap-6 text-xs">
          <span className="text-muted-foreground">{item.label}</span>
          <span className="flex gap-1">
            {item.keys.map((key) => (
              <Kbd key={key}>{key}</Kbd>
            ))}
          </span>
        </li>
      ))}
    </ul>
  );
}

function ShortcutsPopover({ selectable }: { selectable?: boolean }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" className="inline-flex items-center gap-1 rounded px-1 py-0.5 transition-colors hover:bg-muted hover:text-foreground">
          <Keyboard className="size-3.5" />
          快捷键
        </button>
      </PopoverTrigger>
      <PopoverContent side="top" align="end" className="w-60">
        <ShortcutList selectable={selectable} />
      </PopoverContent>
    </Popover>
  );
}

function EmptyDetail({ kind, stats }: { kind: TicketKind; stats?: TicketStats }) {
  const noun = kind === "feedback" ? "反馈" : "工单";
  const waiting = stats ? stats.open : undefined;
  return (
    <div className="flex h-full flex-col items-center justify-center gap-6 p-8 text-center">
      <div className="space-y-3">
        <span className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
          {waiting ? <Inbox className="size-6" /> : <MousePointerClick className="size-6" />}
        </span>
        <div className="space-y-1">
          <p className="text-sm font-medium text-foreground">
            {waiting ? `还有 ${waiting} 条${noun}等待受理` : waiting === 0 ? "待受理已清空" : `从左侧选择一条${noun}`}
          </p>
          <p className="text-xs text-muted-foreground">选中后在这里查看会话、回复和处理，按 J 从第一条开始</p>
        </div>
      </div>
      {waiting === 0 ? (
        <Badge variant="success" className="gap-1">
          <Check />
          全部处理完毕
        </Badge>
      ) : null}
      <div className="w-60 rounded-xl border bg-background p-4 text-left">
        <ShortcutList selectable={kind === "ticket"} />
      </div>
    </div>
  );
}
