import { Link2, MessageSquare, Ticket } from "lucide-react";
import type { MethodOption } from "@/components/apps/auth-protocol/auth-protocol-shared";
import type { AccountSetupSource } from "@/lib/api/account-setup";

/** 需要初始化的建号方式，顺序与后端 AccountSetupSources 一致。应用策略页与用户详情共用。 */
export const ACCOUNT_SETUP_SOURCES: Array<MethodOption & { value: AccountSetupSource }> = [
  { value: "oauth", label: "第三方登录", icon: Link2 },
  { value: "sms", label: "短信登录", icon: MessageSquare },
  { value: "cardkey", label: "卡密登录", icon: Ticket }
];

export function accountSetupSourceLabel(source?: string | null) {
  return ACCOUNT_SETUP_SOURCES.find((item) => item.value === source)?.label ?? source ?? "";
}
