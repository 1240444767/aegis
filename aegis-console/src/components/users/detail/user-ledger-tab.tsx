"use client";

/**
 * 流水：积分、经验、余额、会员四本账。
 *
 * 子页同步到 `?sub=`，资产页、概览里的「查看流水」能直接落到对应的那本账。
 * 调整入口都回到资产页（那里有方向、预览与防重复提交的完整表单），这里只看与冲正。
 */

import { Coins, Crown, Sparkles, Wallet } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { AdminAppUserDetail } from "@/lib/api/types";
import { PointLedgerView } from "./ledger/ledger-points";
import { VipLedgerView } from "./ledger/ledger-vip";
import { WalletLedgerView } from "./ledger/ledger-wallet";

const SUBS = [
  { value: "integral", label: "积分", icon: Coins },
  { value: "experience", label: "经验", icon: Sparkles },
  { value: "wallet", label: "余额", icon: Wallet },
  { value: "vip", label: "会员", icon: Crown }
] as const;

type Sub = (typeof SUBS)[number]["value"];

export function UserLedgerTab({
  appKey,
  userId,
  user,
  onNavigate
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  onNavigate?: (tab: string, sub?: string) => void;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const raw = searchParams.get("sub");
  const sub: Sub = SUBS.some((item) => item.value === raw) ? (raw as Sub) : "integral";

  function select(next: string) {
    const params = new URLSearchParams(searchParams.toString());
    params.set("sub", next);
    router.replace(`?${params.toString()}`, { scroll: false });
  }

  const toAssets = onNavigate ? () => onNavigate("assets") : undefined;

  return (
    <div className="flex flex-col gap-5">
      <Tabs value={sub} onValueChange={select}>
        <TabsList className="w-full sm:w-auto">
          {SUBS.map(({ value, label, icon: Icon }) => (
            <TabsTrigger key={value} value={value} className="flex-1 sm:flex-none sm:px-4">
              <Icon />
              {label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      {sub === "integral" || sub === "experience" ? (
        <PointLedgerView key={sub} appKey={appKey} userId={userId} kind={sub} onAdjust={toAssets} />
      ) : sub === "wallet" ? (
        <WalletLedgerView appKey={appKey} userId={userId} onAdjust={toAssets} />
      ) : (
        <VipLedgerView appKey={appKey} userId={userId} user={user} onManage={toAssets} />
      )}
    </div>
  );
}
