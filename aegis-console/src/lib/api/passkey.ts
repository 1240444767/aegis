import { apiRequest } from "@/lib/api/client";

/**
 * 应用级通行密钥（Passkey）依赖方。
 *
 * 平台级 Passkey 配置只认浏览器：RP ID 跟着地址栏走。原生 App 的请求里没有 Origin，
 * clientDataJSON 的来源是 `android:apk-key-hash:<证书 SHA-256>`，RP ID 必须是一个发布了
 * `/.well-known/assetlinks.json` 的网站域名。配了这一份，这个应用的通行密钥注册与登录一律按它校验。
 * 规则见 internal/domain/app/passkey.go。
 */

export type AndroidPasskeyApp = {
  packageName: string;
  /** 签名证书 SHA-256，大写冒号分隔；证书轮换时新旧都列上 */
  sha256CertFingerprints: string[];
};

export type AppPasskeyPolicy = {
  enabled: boolean;
  rpId: string;
  rpName?: string;
  origins: string[];
  android: AndroidPasskeyApp[];
  /** 从未配置过：当前沿用平台级配置 */
  isDefault?: boolean;
};

export type AppPasskeyPolicyView = {
  policy: AppPasskeyPolicy;
  /** 校验 clientDataJSON.origin 时实际使用的完整来源（含由证书推出的 apk-key-hash） */
  allowedOrigins: string[];
  /** RP ID 域名下 /.well-known/assetlinks.json 应发布的内容 */
  assetLinks: unknown[];
  assetLinksUrl?: string;
  /** 平台 Passkey 模块是否可用 */
  moduleReady: boolean;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/passkey`;

export function getAppPasskeyPolicy(token: string, appKey: string) {
  return apiRequest<AppPasskeyPolicyView>(base(appKey), { token });
}

export function saveAppPasskeyPolicy(token: string, appKey: string, policy: AppPasskeyPolicy) {
  return apiRequest<AppPasskeyPolicyView>(base(appKey), {
    method: "PUT",
    token,
    body: JSON.stringify(policy)
  });
}

/** Aegis 提供的 assetlinks 副本地址，RP ID 域名的网站把 /.well-known/assetlinks.json 反代到这里 */
export function publicAssetLinksPath(appKey: string) {
  return `/api/public/apps/${encodeURIComponent(appKey)}/passkey/assetlinks.json`;
}
