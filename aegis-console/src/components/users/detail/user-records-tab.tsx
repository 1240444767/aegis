"use client";

/**
 * 互动记录：这个用户在各业务模块里留下的全部记录，按模块分子页。
 *
 * 子页同步到 `?sub=`（保留其他参数，如 `tab`、`from`），深链可直接落到「抽奖」等子页。
 * 只挂载当前子页，切走的子页不再发请求；各子页的查询有缓存，切回来立即显示。
 *
 * 站内通知不在这里：后端 `/notifications` 管理接口不支持按用户筛选。
 */

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { CalendarCheck2, Dices, Gift, MonitorPlay, ShoppingBag, Ticket } from "lucide-react";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { AdminAppUserDetail } from "@/lib/api/types";
import { CardKeyRecords } from "./records/card-key-records";
import { InviteRecords } from "./records/invite-records";
import { LotteryRecords } from "./records/lottery-records";
import { PointsStoreRecords } from "./records/points-store-records";
import { RewardedAdRecords } from "./records/rewarded-ad-records";
import { SignInRecords } from "./records/signin-records";

const SUB_TABS = [
  { value: "signin", label: "签到", icon: CalendarCheck2 },
  { value: "invite", label: "邀请", icon: Gift },
  { value: "lottery", label: "抽奖", icon: Dices },
  { value: "card-key", label: "卡密兑换", icon: Ticket },
  { value: "points-store", label: "积分商城", icon: ShoppingBag },
  { value: "rewarded-ad", label: "激励广告", icon: MonitorPlay }
] as const;

type SubTab = (typeof SUB_TABS)[number]["value"];

const SUB_VALUES = new Set<string>(SUB_TABS.map((item) => item.value));

export function UserRecordsTab({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const raw = searchParams.get("sub");
  const sub: SubTab = raw && SUB_VALUES.has(raw) ? (raw as SubTab) : "signin";

  const go = (next: string) => {
    const params = new URLSearchParams(searchParams.toString());
    params.set("sub", next);
    router.replace(`?${params.toString()}`, { scroll: false });
  };

  return (
    <div className="flex flex-col gap-5">
      <Tabs value={sub} onValueChange={go} className="gap-0">
        <div className="-mx-1 overflow-x-auto px-1 pb-1">
          <TabsList variant="line" className="w-max justify-start gap-1 border-b-0">
            {SUB_TABS.map((item) => {
              const Icon = item.icon;
              return (
                <TabsTrigger key={item.value} value={item.value} className="flex-none px-2.5">
                  <Icon />
                  {item.label}
                </TabsTrigger>
              );
            })}
          </TabsList>
        </div>
      </Tabs>

      {sub === "signin" ? <SignInRecords key={`${appKey}-${userId}`} appKey={appKey} userId={userId} /> : null}
      {sub === "invite" ? <InviteRecords key={`${appKey}-${userId}`} appKey={appKey} userId={userId} /> : null}
      {sub === "lottery" ? <LotteryRecords key={`${appKey}-${userId}`} appKey={appKey} userId={userId} /> : null}
      {sub === "card-key" ? (
        <CardKeyRecords key={`${appKey}-${userId}`} appKey={appKey} userId={userId} account={user.account || user.nickname} />
      ) : null}
      {sub === "points-store" ? <PointsStoreRecords key={`${appKey}-${userId}`} appKey={appKey} userId={userId} /> : null}
      {sub === "rewarded-ad" ? <RewardedAdRecords key={`${appKey}-${userId}`} appKey={appKey} userId={userId} /> : null}
    </div>
  );
}
