"use client";

import { OverviewHero } from "@/components/overview/overview-hero";
import { AttentionPanel, RecentActivity } from "@/components/overview/overview-workspace";
import { PlatformBannerCarousel } from "@/components/overview/platform-banner-carousel";
import { QuickActions } from "@/components/overview/quick-actions";
import { RuntimeSection } from "@/components/overview/runtime-section";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { useActivePlatformBannersQuery } from "@/lib/admin-hooks";
import { useIsSuperAdmin } from "@/lib/permissions";

export default function OverviewPage() {
  const bannersQuery = useActivePlatformBannersQuery();
  const isSuperAdmin = useIsSuperAdmin();
  const hasBanners = (bannersQuery.data?.length ?? 0) > 0;

  return (
    <div className="space-y-6">
      <OverviewHero />

      {/* 平台横幅独立成行，无数据时整行不渲染 */}
      {hasBanners ? (
        <WidgetBoundary title="平台横幅加载失败">
          <div className="lg:h-[240px]">
            <PlatformBannerCarousel />
          </div>
        </WidgetBoundary>
      ) : null}

      <QuickActions />

      <div className="grid gap-4 lg:grid-cols-3">
        <AttentionPanel />
        <RecentActivity className="lg:col-span-2" />
      </div>

      {/* 运行信息接口要求超管，非超管挂载只会拿到 401 */}
      {isSuperAdmin ? <RuntimeSection /> : null}
    </div>
  );
}
