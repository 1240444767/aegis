"use client";

import { Suspense, useCallback } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Settings2 } from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/ui/data-state";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { useTicketStatsQuery } from "@/lib/ticket-hooks";
import { TicketKpiStrip } from "@/components/tickets/workspace/ticket-analytics";
import { TicketWorkspace } from "@/components/tickets/workspace/ticket-workspace";

// 意见反馈。
//
// 反馈就是 kind=feedback 的工单：回复、改状态、关闭全部复用工单接口，
// 界面与工单中心共用同一套工作台，只是列表按反馈的关注点排（分类、提交端、图片数）。
//
// URL 约定：/feedback?id=123 直接打开某条反馈详情（站内信与通知的落点）。

export default function FeedbackPage() {
  return (
    <Suspense fallback={<LoadingState title="加载意见反馈" />}>
      <WidgetBoundary title="意见反馈加载失败">
        <FeedbackPageInner />
      </WidgetBoundary>
    </Suspense>
  );
}

function FeedbackPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const detailId = searchParams.get("id");
  const selectedId = detailId ? Number(detailId) : null;

  const select = useCallback(
    (id: number | null) => router.replace(id ? `/feedback?id=${id}` : "/feedback", { scroll: false }),
    [router]
  );

  const statsQuery = useTicketStatsQuery(undefined, "feedback");

  return (
    <div className="flex h-[calc(100dvh-5.5rem)] min-h-[560px] flex-col gap-4 lg:h-[calc(100dvh-6rem)]">
      <SectionHeading
        eyebrow="服务台"
        title="意见反馈"
        action={
          <Button size="sm" variant="ghost" asChild>
            <Link href="/tickets?tab=settings">
              <Settings2 />
              分类与快捷回复
            </Link>
          </Button>
        }
      />
      <TicketKpiStrip stats={statsQuery.data} loading={statsQuery.isLoading} />
      <TicketWorkspace kind="feedback" selectedId={selectedId} onSelect={select} stats={statsQuery.data} />
    </div>
  );
}
