import { apiRequest } from "@/lib/api/client";

/**
 * 账号初始化 API。
 *
 * 第三方、短信、卡密快速登录首次建号时，账号名由系统生成（`qq_<openid>`、手机号、卡面）且没有密码，
 * App 登录后引导用户补设用户名与密码（首次弹窗，之后横幅）。应用策略存在 `apps.settings.accountSetup`，
 * 是否需要提醒、怎么提醒全部由后端判定，控制台只配置与查看。契约见 docs/account-setup.md。
 */

export type AccountSetupSource = "oauth" | "sms" | "cardkey";
export type AccountSetupFirstPrompt = "dialog" | "banner";

export type AccountSetupPolicy = {
  enabled: boolean;
  sources: AccountSetupSource[];
  requireAccount: boolean;
  requirePassword: boolean;
  /** dialog：第一次弹窗，之后横幅；banner：一律横幅 */
  firstPrompt: AccountSetupFirstPrompt;
  /** 用户关掉横幅后多久再出现（小时）；0 表示横幅不能关闭 */
  snoozeHours: number;
  /** 应用从未配置过，返回的是默认策略 */
  isDefault: boolean;
};

export type AccountSetupStats = {
  /** 快速登录建的号（登记过的） */
  tracked: number;
  completed: number;
  pending: number;
  accountPending: number;
  prompted: number;
  bySource: Partial<Record<string, number>> | null;
};

export type UsernameRules = {
  minLength: number;
  maxLength: number;
  hint: string;
};

export type AccountSetupPolicyView = {
  policy: AccountSetupPolicy;
  stats: AccountSetupStats | null;
  sources: AccountSetupSource[];
  rules: UsernameRules;
};

/** 用户详情里的初始化情况。自己注册的账号 `tracked = false`，其余字段只有 passwordPending 有意义。 */
export type AdminAccountSetupView = {
  tracked: boolean;
  source?: AccountSetupSource | string;
  provider?: string;
  accountPending: boolean;
  passwordPending: boolean;
  /** 按应用策略算，还有没完成的项 */
  required: boolean;
  accountSetAt?: string | null;
  promptedAt?: string | null;
  snoozedUntil?: string | null;
  completedAt?: string | null;
  /** 应用策略是否覆盖这个号的建号方式 */
  policyCovers: boolean;
  createdAt?: string | null;
};

export type AdminRenameUserResult = {
  account: string;
  accountSetup: AdminAccountSetupView;
};

const app = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}`;
const user = (appKey: string, userId: number | string) => `${app(appKey)}/users/${userId}`;

export function getAccountSetupPolicy(token: string, appKey: string) {
  return apiRequest<AccountSetupPolicyView>(`${app(appKey)}/account-setup`, { token });
}

/** 不合规时 40968，原因原样返回 */
export function saveAccountSetupPolicy(token: string, appKey: string, policy: Omit<AccountSetupPolicy, "isDefault">) {
  return apiRequest<AccountSetupPolicyView>(`${app(appKey)}/account-setup`, {
    method: "PUT",
    token,
    body: JSON.stringify({ policy })
  });
}

export function getUserAccountSetup(token: string, appKey: string, userId: number | string) {
  return apiRequest<AdminAccountSetupView>(`${user(appKey, userId)}/account-setup`, { token });
}

/** `resetAccount` 时用户名也重新算作待设置；提醒一律从首次弹窗重新开始 */
export function resetUserAccountSetup(token: string, appKey: string, userId: number | string, resetAccount: boolean) {
  return apiRequest<AdminAccountSetupView>(`${user(appKey, userId)}/account-setup/reset`, {
    method: "POST",
    token,
    body: JSON.stringify({ resetAccount })
  });
}

/**
 * 管理员改用户名：沿用注册账号规则（3–64 字节、不含空白），不限次数，改完算用户名已设置。
 * 40963 已被使用，40005 不合规。
 */
export function renameAppUser(token: string, appKey: string, userId: number | string, account: string) {
  return apiRequest<AdminRenameUserResult>(`${user(appKey, userId)}/account`, {
    method: "PUT",
    token,
    body: JSON.stringify({ account })
  });
}
