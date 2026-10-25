"use client";

import { Suspense, useCallback, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { BellRing, Inbox, Plus, Settings2, TrendingUp, UserCheck } from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { SectionHeading } from "@/components/ui/section-heading";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { LoadingState } from "@/components/ui/data-state";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useCreateTicketMutation, useTicketCategoriesQuery, useTicketStatsQuery } from "@/lib/ticket-hooks";
import type { TicketPriority } from "@/lib/api/tickets";
import { TicketSettingsPanel } from "@/components/tickets/ticket-settings-panel";
import { NotifyCenterPanel } from "@/components/tickets/notify-center-panel";
import { PRIORITY_LABEL } from "@/components/tickets/ticket-shared";
import { TicketAnalytics } from "@/components/tickets/workspace/ticket-analytics";
import { TicketWorkspace } from "@/components/tickets/workspace/ticket-workspace";
import { cn } from "@/lib/utils";

// 工单中心。
//
// URL 约定：
//   /tickets?tab=inbox|mine|analytics|settings|notify   —— 面板切换
//   /tickets?id=123                                     —— 直接打开某工单详情
// 第二条是通知里「查看工单」按钮的落点：飞书卡片点进来就是这一屏。
// 打开状态完全由 URL 驱动，页面内点击与外部链接走同一条路径。

type Tab = "inbox" | "mine" | "analytics" | "settings" | "notify";
const WORKSPACE_TABS: Tab[] = ["inbox", "mine"];

export default function TicketsPage() {
  return (
    <Suspense fallback={<LoadingState title="加载工单中心" />}>
      <WidgetBoundary title="工单中心加载失败">
        <TicketsPageInner />
      </WidgetBoundary>
    </Suspense>
  );
}

function TicketsPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = (searchParams.get("tab") as Tab) || "inbox";
  const detailId = searchParams.get("id");
  const selectedId = detailId ? Number(detailId) : null;
  const [createOpen, setCreateOpen] = useState(false);

  const select = useCallback(
    (id: number | null) => router.replace(`/tickets?tab=${tab}${id ? `&id=${id}` : ""}`, { scroll: false }),
    [router, tab]
  );

  // 意见反馈有独立页面，工单中心的计数与列表只统计工单
  const statsQuery = useTicketStatsQuery(undefined, "ticket");
  const stats = statsQuery.data;
  const workspace = WORKSPACE_TABS.includes(tab);

  const newTicketButton = (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon-sm" onClick={() => setCreateOpen(true)} aria-label="新建工单">
          <Plus />
        </Button>
      </TooltipTrigger>
      <TooltipContent>新建工单</TooltipContent>
    </Tooltip>
  );

  return (
    <div
      className={cn(
        "flex flex-col gap-4",
        workspace && "h-[calc(100dvh-5.5rem)] min-h-[560px] lg:h-[calc(100dvh-6rem)]"
      )}
    >
      <SectionHeading
        eyebrow="服务台"
        title="工单中心"
        action={
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus />
            新建工单
          </Button>
        }
      />

      <Tabs
        value={tab}
        onValueChange={(value) => router.replace(`/tickets?tab=${value}`, { scroll: false })}
        className={cn("gap-4", workspace && "min-h-0 flex-1")}
      >
        <TabsList className="flex-wrap">
          <TabsTrigger value="inbox">
            <Inbox className="size-3.5" />
            工单台
            {stats?.open ? (
              <Badge variant="warning" size="sm">
                {stats.open}
              </Badge>
            ) : null}
          </TabsTrigger>
          <TabsTrigger value="mine">
            <UserCheck className="size-3.5" />
            我的待办
            {stats?.mineAssigned ? (
              <Badge variant="info" size="sm">
                {stats.mineAssigned}
              </Badge>
            ) : null}
          </TabsTrigger>
          <TabsTrigger value="analytics">
            <TrendingUp className="size-3.5" />
            统计分析
          </TabsTrigger>
          <TabsTrigger value="settings">
            <Settings2 className="size-3.5" />
            工单配置
          </TabsTrigger>
          <TabsTrigger value="notify">
            <BellRing className="size-3.5" />
            通知出口
          </TabsTrigger>
        </TabsList>

        <TabsContent value="inbox" className="mt-0 flex min-h-0 flex-1 flex-col data-[state=inactive]:hidden">
          <TicketWorkspace
            kind="ticket"
            selectedId={selectedId}
            onSelect={select}
            stats={stats}
            selectable
            actions={newTicketButton}
          />
        </TabsContent>
        <TabsContent value="mine" className="mt-0 flex min-h-0 flex-1 flex-col data-[state=inactive]:hidden">
          <TicketWorkspace kind="ticket" selectedId={selectedId} onSelect={select} mineOnly selectable />
        </TabsContent>
        <TabsContent value="analytics" className="mt-0">
          <TicketAnalytics />
        </TabsContent>
        <TabsContent value="settings" className="mt-0">
          <SettingsTab />
        </TabsContent>
        <TabsContent value="notify" className="mt-0">
          <NotifyCenterPanel />
        </TabsContent>
      </Tabs>

      <CreateTicketDialog open={createOpen} onClose={() => setCreateOpen(false)} onCreated={select} />
    </div>
  );
}

// ─────────────── 新建工单 ───────────────

const EMPTY_FORM = {
  title: "",
  content: "",
  categoryId: "none",
  priority: "normal" as TicketPriority,
  requesterName: "",
  requesterContact: ""
};

function CreateTicketDialog({
  open,
  onClose,
  onCreated
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (id: number) => void;
}) {
  const appsQuery = useAdminAppsQuery();
  const createMut = useCreateTicketMutation();
  const [appId, setAppId] = useState("0");
  const categoriesQuery = useTicketCategoriesQuery(Number(appId) || 0);
  const [form, setForm] = useState(EMPTY_FORM);

  const submit = async () => {
    if (!form.title.trim() || !form.content.trim()) {
      notify.warning("请填写标题与描述");
      return;
    }
    try {
      const ticket = await createMut.mutateAsync({
        appid: Number(appId) || 0,
        title: form.title.trim(),
        content: form.content.trim(),
        categoryId: form.categoryId === "none" ? undefined : Number(form.categoryId),
        priority: form.priority,
        requesterName: form.requesterName.trim() || undefined,
        requesterContact: form.requesterContact.trim() || undefined,
        source: "console"
      });
      notify.success(`工单 ${ticket.ticketNo} 已创建`);
      setForm(EMPTY_FORM);
      onClose();
      onCreated(ticket.id);
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "创建失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>新建工单</DialogTitle>
          <DialogDescription>代用户登记问题，或建一张内部跟进单。</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label>所属应用</Label>
              <Select value={appId} onValueChange={setAppId}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">平台内部工单</SelectItem>
                  {(appsQuery.data ?? []).map((app) => (
                    <SelectItem key={app.id} value={String(app.id)}>
                      {app.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>分类</Label>
              <Select value={form.categoryId} onValueChange={(value) => setForm((prev) => ({ ...prev, categoryId: value }))}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">不指定</SelectItem>
                  {(categoriesQuery.data ?? [])
                    .filter((category) => (category.kind ?? "ticket") === "ticket")
                    .map((category) => (
                      <SelectItem key={category.id} value={String(category.id)}>
                        {category.name}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="ticket-title">标题</Label>
            <Input
              id="ticket-title"
              value={form.title}
              onChange={(event) => setForm((prev) => ({ ...prev, title: event.target.value }))}
              placeholder="一句话说明问题"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="ticket-content">问题描述</Label>
            <Textarea
              id="ticket-content"
              rows={5}
              value={form.content}
              onChange={(event) => setForm((prev) => ({ ...prev, content: event.target.value }))}
              placeholder="现象、复现步骤、期望结果"
            />
          </div>
          <div className="space-y-1.5">
            <Label>优先级</Label>
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              value={form.priority}
              onValueChange={(value) => value && setForm((prev) => ({ ...prev, priority: value as TicketPriority }))}
              className="w-full"
            >
              {(["low", "normal", "high", "urgent"] as TicketPriority[]).map((priority) => (
                <ToggleGroupItem key={priority} value={priority} className="flex-1">
                  {PRIORITY_LABEL[priority]}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="ticket-requester">提单人</Label>
              <Input
                id="ticket-requester"
                value={form.requesterName}
                onChange={(event) => setForm((prev) => ({ ...prev, requesterName: event.target.value }))}
                placeholder="留空取当前管理员"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ticket-contact">联系方式</Label>
              <Input
                id="ticket-contact"
                value={form.requesterContact}
                onChange={(event) => setForm((prev) => ({ ...prev, requesterContact: event.target.value }))}
                placeholder="邮箱或手机"
              />
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button disabled={createMut.isPending} onClick={submit}>
            创建工单
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─────────────── 配置 ───────────────

function SettingsTab() {
  const appsQuery = useAdminAppsQuery();
  const [appId, setAppId] = useState("0");

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Label className="text-xs text-muted-foreground">配置范围</Label>
        <Select value={appId} onValueChange={setAppId}>
          <SelectTrigger className="h-9 w-52">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="0">平台级</SelectItem>
            {(appsQuery.data ?? []).map((app) => (
              <SelectItem key={app.id} value={String(app.id)}>
                {app.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {appId === "0" ? (
          <Badge variant="secondary" size="sm">
            仅超级管理员可修改
          </Badge>
        ) : null}
      </div>
      <TicketSettingsPanel appId={Number(appId) || 0} />
    </div>
  );
}
