"use client";

import { useState } from "react";
import { Search } from "lucide-react";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { UserVipPanel } from "@/components/users/detail/user-vip-panel";

/**
 * 会员查询与发放。
 *
 * 选中用户之后就是用户详情里的那块会员面板 —— 现状、发放（含折算预览）、收回、试用、
 * 会员记录与折算记录都只有那一份实现。这里曾经另有一套简版的授予与收回，
 * 两边的口径（永久会员已包含时怎么办、开通永久时存量怎么办）迟早会说得不一样。
 */
export function VipMemberPanel({ appKey }: { appKey: string }) {
  const [user, setUser] = useState<PickedUser | null>(null);

  return (
    <div className="space-y-5">
      <SectionCard
        icon={<Search className="size-4" />}
        title="会员查询"
        // UserPicker 的触发器是 w-full，必须给它一个定宽容器才不会撑破卡头
        aside={
          <div className="w-64 min-w-0">
            <UserPicker appKey={appKey} value={user} onChange={setUser} />
          </div>
        }
      >
        {!user ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            请选择用户
          </p>
        ) : (
          <p className="text-xs text-muted-foreground">
            {user.account ?? `用户 ${user.id}`} 的会员现状、发放与收回、会员记录与折算记录
          </p>
        )}
      </SectionCard>

      {user ? <UserVipPanel key={user.id} appKey={appKey} userId={user.id} /> : null}
    </div>
  );
}
