"use client";

import { useMemo, useState } from "react";
import {
  ArrowDownUp,
  Check,
  CopyPlus,
  Eye,
  EyeOff,
  LayoutGrid,
  List,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  ReceiptText,
  ShoppingBag,
  Trash2,
  X
} from "lucide-react";
import { toast } from "sonner";
import { PointsStoreItemEditor, type EditorTarget } from "@/components/apps/points-store/points-store-item-editor";
import { PointsStoreItemCard, ItemPrice, ItemStock, ShelfBadge } from "@/components/apps/points-store/points-store-item-card";
import { PointsStoreSortList } from "@/components/apps/points-store/points-store-sort-list";
import {
  SHELF_META,
  SHELF_ORDER,
  describeReward,
  formatPoints,
  itemRules,
  rewardVisual,
  shelfKey,
  type ShelfKey
} from "@/components/apps/points-store/points-store-shared";
import { ConfirmActionDialog } from "@/components/users/detail/records/records-shared";
import {
  FilterBar,
  FilterSelect,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  ToneIcon,
  errorMessage,
  type Column
} from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { PointsStoreAdminItem } from "@/lib/api/points-store";
import {
  useDeletePointsStoreItemMutation,
  usePointsStoreCatalogQuery,
  usePointsStoreItemsQuery,
  useSetPointsStoreItemStatusMutation
} from "@/lib/points-store-hooks";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

type SortKey = "" | "redemptions" | "points" | "cost_desc" | "cost_asc" | "newest";

const SORTS: Array<{ value: Exclude<SortKey, "">; label: string }> = [
  { value: "redemptions", label: "兑换最多" },
  { value: "points", label: "消耗积分最多" },
  { value: "cost_desc", label: "价格从高到低" },
  { value: "cost_asc", label: "价格从低到高" },
  { value: "newest", label: "最近创建" }
];

const UNCATEGORIZED = "__none__";

type View = "grid" | "list";

/**
 * 商品管理。
 *
 * 两种视图：卡片按分类分组、接近用户端的样子；列表适合一屏扫完几十件。筛选（关键词、状态、分类）
 * 与排序都在本地做：商品数量有限，接口一次全量下发，兑换汇总随列表带回。
 * 默认顺序即用户端的展示顺序（排序值、再按创建先后），「调整顺序」进入拖拽模式，保存时只写排序值。
 * 勾选后可批量上下架与删除；单件的复制、查看记录、处理待发放在卡片菜单里。
 */
export function PointsStoreItemsPanel({
  appKey,
  onOpenRedemptions
}: {
  appKey: string;
  onOpenRedemptions: (itemId: number, pendingOnly: boolean) => void;
}) {
  const itemsQuery = usePointsStoreItemsQuery(appKey);
  const catalogQuery = usePointsStoreCatalogQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const statusMutation = useSetPointsStoreItemStatusMutation(appKey);
  const deleteMutation = useDeletePointsStoreItemMutation(appKey);

  const [view, setView] = useState<View>("grid");
  const [keyword, setKeyword] = useState("");
  const [shelf, setShelf] = useState("");
  const [category, setCategory] = useState("");
  const [sort, setSort] = useState<SortKey>("");
  const [sorting, setSorting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [editor, setEditor] = useState<EditorTarget | null>(null);
  const [pendingDelete, setPendingDelete] = useState<PointsStoreAdminItem[] | null>(null);
  const [batchBusy, setBatchBusy] = useState(false);

  const all = useMemo(() => itemsQuery.data ?? [], [itemsQuery.data]);
  const reward = useMemo(
    () => ({ catalog: catalogQuery.data?.rewards ?? [], plans: plansQuery.data ?? [], features: featuresQuery.data ?? [] }),
    [catalogQuery.data, plansQuery.data, featuresQuery.data]
  );

  const categories = useMemo(() => {
    const seen: string[] = [];
    for (const item of all) {
      const key = item.category?.trim() || "";
      if (!seen.includes(key)) seen.push(key);
    }
    return seen;
  }, [all]);

  const shelfCounts = useMemo(() => {
    const counts = Object.fromEntries(SHELF_ORDER.map((key) => [key, 0])) as Record<ShelfKey, number>;
    for (const item of all) counts[shelfKey(item)] += 1;
    return counts;
  }, [all]);

  const visible = useMemo(() => {
    const needle = keyword.trim().toLowerCase();
    const list = all.filter((item) => {
      if (shelf && shelfKey(item) !== shelf) return false;
      if (category && (item.category?.trim() || UNCATEGORIZED) !== category) return false;
      if (!needle) return true;
      const haystack = [item.title, item.subtitle, item.category, item.badge, String(item.id)]
        .concat(item.rewards.map((entry) => describeReward(entry, reward.catalog, reward.plans, reward.features)))
        .join(" ")
        .toLowerCase();
      return haystack.includes(needle);
    });
    const by: Record<Exclude<SortKey, "">, (a: PointsStoreAdminItem, b: PointsStoreAdminItem) => number> = {
      redemptions: (a, b) => b.redemptions - a.redemptions,
      points: (a, b) => b.pointsSpent - a.pointsSpent,
      cost_desc: (a, b) => b.cost - a.cost,
      cost_asc: (a, b) => a.cost - b.cost,
      newest: (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
    };
    return sort ? [...list].sort(by[sort]) : list;
  }, [all, shelf, category, keyword, sort, reward]);

  /** 默认顺序时按分类分组（与用户端一致）；换了排序就平铺 */
  const groups = useMemo(() => {
    if (sort) return [["", visible] as const];
    const map = new Map<string, PointsStoreAdminItem[]>();
    for (const item of visible) {
      const key = item.category?.trim() || "";
      map.set(key, [...(map.get(key) ?? []), item]);
    }
    return Array.from(map.entries());
  }, [visible, sort]);

  const filtered = Boolean(keyword || shelf || category || sort);
  const selectedItems = all.filter((item) => selected.has(item.id));
  const allVisibleSelected = visible.length > 0 && visible.every((item) => selected.has(item.id));

  const toggleSelect = (id: number, next: boolean) =>
    setSelected((current) => {
      const copy = new Set(current);
      if (next) copy.add(id);
      else copy.delete(id);
      return copy;
    });

  const toggleStatus = async (item: PointsStoreAdminItem, active: boolean) => {
    try {
      await statusMutation.mutateAsync({ itemId: item.id, active });
      toast.success(active ? `已上架「${item.title}」` : `已下架「${item.title}」`);
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  /** 批量上下架：只提交状态需要变化的那几件 */
  const batchStatus = async (active: boolean) => {
    const targets = selectedItems.filter((item) => (item.status === "active") !== active);
    if (!targets.length) {
      toast.info(active ? "所选商品都已上架" : "所选商品都已下架");
      return;
    }
    setBatchBusy(true);
    let failed = 0;
    for (const item of targets) {
      try {
        await statusMutation.mutateAsync({ itemId: item.id, active });
      } catch {
        failed += 1;
      }
    }
    setBatchBusy(false);
    const done = targets.length - failed;
    if (failed) toast.warning(`${active ? "上架" : "下架"} ${done} 件，${failed} 件失败`);
    else toast.success(`已${active ? "上架" : "下架"} ${done} 件`);
    setSelected(new Set());
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    setBatchBusy(true);
    let failed = 0;
    let lastError: unknown = null;
    for (const item of pendingDelete) {
      try {
        await deleteMutation.mutateAsync(item.id);
      } catch (error) {
        failed += 1;
        lastError = error;
      }
    }
    setBatchBusy(false);
    const done = pendingDelete.length - failed;
    if (!failed) toast.success(pendingDelete.length === 1 ? `已删除「${pendingDelete[0].title}」` : `已删除 ${done} 件`);
    else if (done) toast.warning(`已删除 ${done} 件，${failed} 件失败：${errorMessage(lastError)}`);
    else toast.error(errorMessage(lastError, "删除失败"));
    setSelected((current) => {
      const copy = new Set(current);
      for (const item of pendingDelete) copy.delete(item.id);
      return copy;
    });
    setPendingDelete(null);
  };

  const resetFilters = () => {
    setKeyword("");
    setShelf("");
    setCategory("");
    setSort("");
  };

  const menu = (item: PointsStoreAdminItem) => (
    <ItemMenu
      item={item}
      onEdit={() => setEditor({ mode: "edit", item })}
      onDuplicate={() => setEditor({ mode: "duplicate", item })}
      onToggle={() => toggleStatus(item, item.status !== "active")}
      onRecords={() => onOpenRedemptions(item.id, false)}
      onPending={item.pending > 0 ? () => onOpenRedemptions(item.id, true) : undefined}
      onDelete={() => setPendingDelete([item])}
    />
  );

  if (itemsQuery.isLoading) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {[0, 1, 2, 3, 4, 5].map((index) => (
          <Skeleton key={index} className="h-64 w-full rounded-xl" />
        ))}
      </div>
    );
  }

  if (itemsQuery.error && !all.length) {
    return (
      <Section title="商品" icon={ShoppingBag} tone="primary">
        <StateBlock kind="error" description={errorMessage(itemsQuery.error)} onRetry={() => itemsQuery.refetch()} />
      </Section>
    );
  }

  if (sorting) {
    return (
      <>
        <PointsStoreSortList appKey={appKey} items={all} onDone={() => setSorting(false)} />
        <PointsStoreItemEditor appKey={appKey} target={editor} onClose={() => setEditor(null)} />
      </>
    );
  }

  const columns: Array<Column<PointsStoreAdminItem>> = [
    {
      id: "select",
      header: (
        <Checkbox
          checked={allVisibleSelected ? true : selected.size ? "indeterminate" : false}
          onCheckedChange={(next) => setSelected(next ? new Set(visible.map((item) => item.id)) : new Set())}
          aria-label="全选"
        />
      ),
      width: "36px",
      cell: (row) => (
        <span onClick={(event) => event.stopPropagation()}>
          <Checkbox checked={selected.has(row.id)} onCheckedChange={(next) => toggleSelect(row.id, Boolean(next))} aria-label="选择" />
        </span>
      )
    },
    {
      id: "item",
      header: "商品",
      cell: (row) => {
        const visual = rewardVisual(row.rewards[0]?.type);
        return (
          <div className="flex min-w-0 items-center gap-3">
            <ToneIcon icon={visual.icon} tone={visual.tone} />
            <div className="flex min-w-0 flex-col">
              <span className="truncate font-medium">{row.title}</span>
              <span className="truncate text-xs text-muted-foreground">
                {row.rewards.map((entry) => describeReward(entry, reward.catalog, reward.plans, reward.features)).join("，")}
              </span>
            </div>
          </div>
        );
      }
    },
    { id: "category", header: "分类", hideBelow: "lg", cell: (row) => <span className="text-muted-foreground">{row.category || "未分类"}</span> },
    { id: "cost", header: "价格", align: "right", cell: (row) => <ItemPrice item={row} compact /> },
    { id: "stock", header: "库存", width: "140px", hideBelow: "md", cell: (row) => <ItemStock item={row} compact /> },
    {
      id: "rules",
      header: "限制",
      hideBelow: "xl",
      cell: (row) => <span className="text-xs text-muted-foreground">{itemRules(row).join("，") || "不限"}</span>
    },
    {
      id: "sales",
      header: "兑换",
      align: "right",
      cell: (row) => (
        <div className="flex flex-col items-end">
          <span className="tabular-nums">{formatPoints(row.redemptions)} 笔</span>
          {row.pending > 0 ? (
            <button
              type="button"
              className="text-xs font-medium text-amber-600 tabular-nums hover:underline dark:text-amber-400"
              onClick={(event) => {
                event.stopPropagation();
                onOpenRedemptions(row.id, true);
              }}
            >
              待发放 {row.pending}
            </button>
          ) : (
            <span className="text-xs text-muted-foreground tabular-nums">{formatPoints(row.redeemers)} 人</span>
          )}
        </div>
      )
    },
    { id: "shelf", header: "状态", cell: (row) => <ShelfBadge item={row} /> },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "96px",
      cell: (row) => (
        <div className="flex items-center justify-end gap-1" onClick={(event) => event.stopPropagation()}>
          <Switch checked={row.status === "active"} onCheckedChange={(next) => toggleStatus(row, next)} aria-label="上架" />
          {menu(row)}
        </div>
      )
    }
  ];

  return (
    <div className="flex flex-col gap-4">
      <Section
        flush
        title="商品"
        icon={ShoppingBag}
        tone="primary"
        description={`共 ${all.length} 件，${SHELF_ORDER.filter((key) => shelfCounts[key])
          .map((key) => `${SHELF_META[key].label} ${shelfCounts[key]}`)
          .join("，") || "暂无商品"}`}
        actions={
          <>
            <Button variant="outline" size="sm" disabled={all.length < 2} onClick={() => setSorting(true)}>
              <ArrowDownUp />
              调整顺序
            </Button>
            <Button size="sm" onClick={() => setEditor({ mode: "create" })}>
              <Plus />
              新建商品
            </Button>
          </>
        }
        toolbar={
          selected.size ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm font-medium tabular-nums">已选 {selected.size} 件</span>
              <div className="mx-1 h-4 w-px bg-border" />
              <Button size="sm" variant="outline" disabled={batchBusy} onClick={() => batchStatus(true)}>
                <Eye />
                上架
              </Button>
              <Button size="sm" variant="outline" disabled={batchBusy} onClick={() => batchStatus(false)}>
                <EyeOff />
                下架
              </Button>
              <Button size="sm" variant="outline" className="text-red-600 dark:text-red-400" disabled={batchBusy} onClick={() => setPendingDelete(selectedItems)}>
                <Trash2 />
                删除
              </Button>
              {batchBusy ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : null}
              <Button size="sm" variant="ghost" className="ml-auto text-muted-foreground" onClick={() => setSelected(new Set())}>
                <X />
                取消选择
              </Button>
            </div>
          ) : (
            <FilterBar>
              <SearchField value={keyword} onChange={setKeyword} placeholder="搜索名称、权益、分类、编号" />
              <FilterSelect
                label="状态"
                value={shelf}
                onChange={setShelf}
                options={SHELF_ORDER.map((key) => ({ value: key, label: `${SHELF_META[key].label} ${shelfCounts[key]}` }))}
              />
              {categories.length > 1 || categories[0] ? (
                <FilterSelect
                  label="分类"
                  value={category}
                  onChange={setCategory}
                  options={categories.map((key) => ({ value: key || UNCATEGORIZED, label: key || "未分类" }))}
                />
              ) : null}
              <FilterSelect label="排序" value={sort} onChange={(value) => setSort(value as SortKey)} allLabel="展示顺序" options={SORTS} />
              <ResetButton visible={filtered} onClick={resetFilters} />
              <ToggleGroup
                type="single"
                variant="outline"
                size="sm"
                value={view}
                onValueChange={(value) => value && setView(value as View)}
                className="ml-auto"
              >
                <ToggleGroupItem value="grid" aria-label="卡片视图">
                  <LayoutGrid />
                </ToggleGroupItem>
                <ToggleGroupItem value="list" aria-label="列表视图">
                  <List />
                </ToggleGroupItem>
              </ToggleGroup>
            </FilterBar>
          )
        }
      >
        {!all.length ? (
          <StateBlock
            icon={ShoppingBag}
            title="还没有商品"
            description="商品可以是会员套餐、会员天数、功能解锁、经验、余额、抽奖次数，也可以是需要人工发放的实物或兑换码。"
            action={
              <Button size="sm" onClick={() => setEditor({ mode: "create" })}>
                <Plus />
                新建第一件商品
              </Button>
            }
          />
        ) : !visible.length ? (
          <StateBlock title="没有符合条件的商品" action={<ResetButton visible onClick={resetFilters} />} />
        ) : view === "list" ? (
          <RecordTable
            columns={columns}
            rows={visible}
            rowKey={(row) => row.id}
            onRowClick={(row) => setEditor({ mode: "edit", item: row })}
            rowClassName={(row) => (selected.has(row.id) ? "bg-primary/5" : undefined)}
            minWidth={760}
          />
        ) : (
          <div className="flex flex-col gap-6 p-4 sm:p-5">
            {groups.map(([group, items]) => (
              <div key={group || "_"} className="flex flex-col gap-3">
                {!sort ? (
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-semibold text-muted-foreground">{group || "未分类"}</span>
                    <span className="text-xs tabular-nums text-muted-foreground/70">{items.length}</span>
                    <div className="h-px flex-1 bg-border" />
                  </div>
                ) : null}
                <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                  {items.map((item) => (
                    <PointsStoreItemCard
                      key={item.id}
                      item={item}
                      reward={reward}
                      selected={selected.has(item.id)}
                      selecting={selected.size > 0}
                      onSelect={(next) => toggleSelect(item.id, next)}
                      onToggle={(next) => toggleStatus(item, next)}
                      onOpen={() => setEditor({ mode: "edit", item })}
                      onPending={() => onOpenRedemptions(item.id, true)}
                      menu={menu(item)}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </Section>

      <PointsStoreItemEditor appKey={appKey} target={editor} onClose={() => setEditor(null)} />

      <ConfirmActionDialog
        open={Boolean(pendingDelete)}
        onOpenChange={(open) => (!open && !batchBusy ? setPendingDelete(null) : undefined)}
        title={pendingDelete?.length === 1 ? `删除「${pendingDelete[0].title}」？` : `删除 ${pendingDelete?.length ?? 0} 件商品？`}
        description={deleteDescription(pendingDelete)}
        confirmLabel="确认删除"
        destructive
        pending={batchBusy}
        onConfirm={confirmDelete}
      />
    </div>
  );
}

function deleteDescription(items: PointsStoreAdminItem[] | null) {
  if (!items?.length) return undefined;
  const pending = items.reduce((sum, item) => sum + item.pending, 0);
  const base = "删除后用户端立即不可见，已有兑换记录保留（标题与权益按快照显示）。";
  return pending ? `${base}其中 ${pending} 笔待发放仍需在兑换记录里处理。` : `${base}只想暂时不卖可以改为下架。`;
}

function ItemMenu({
  item,
  onEdit,
  onDuplicate,
  onToggle,
  onRecords,
  onPending,
  onDelete
}: {
  item: PointsStoreAdminItem;
  onEdit: () => void;
  onDuplicate: () => void;
  onToggle: () => void;
  onRecords: () => void;
  onPending?: () => void;
  onDelete: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="更多操作" onClick={(event) => event.stopPropagation()}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-44" onClick={(event) => event.stopPropagation()}>
        <DropdownMenuItem onSelect={onEdit}>
          <Pencil />
          编辑
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onDuplicate}>
          <CopyPlus />
          复制为新商品
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onToggle}>
          {item.status === "active" ? <EyeOff /> : <Eye />}
          {item.status === "active" ? "下架" : "上架"}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={onRecords}>
          <ReceiptText />
          兑换记录
        </DropdownMenuItem>
        {onPending ? (
          <DropdownMenuItem onSelect={onPending}>
            <Check />
            处理待发放（{item.pending}）
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={onDelete}>
          <Trash2 />
          删除
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
