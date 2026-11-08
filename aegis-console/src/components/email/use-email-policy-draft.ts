"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import type { EmailPolicy, EmailPolicyView } from "@/lib/api/email-policy";
import { useEmailPolicyQuery, useSaveEmailPolicyMutation, type EmailScope } from "@/lib/email-hooks";

/** 目录还没到时也能渲染出一份完整的表单（与后端 DefaultPolicy 一致） */
export const FALLBACK_POLICY: EmailPolicy = {
  brand: {},
  theme: {},
  layout: { enabled: false },
  code: { length: 6, ttlMinutes: 10, resendSeconds: 60, maxAttempts: 5, addressHourly: 10, addressDaily: 30, ipHourly: 60 },
  scenes: {},
  suppression: { hardBounce: true, complaint: true },
  resetLinks: []
};

function scopeId(scope: EmailScope) {
  return scope.kind === "platform" ? "platform" : `app-${scope.appId}`;
}

/** 后端回的策略里 null 的集合字段补成空，比较草稿时才不会把 null 与 [] 当成两样 */
function normalize(policy?: EmailPolicy | null): EmailPolicy {
  const base = policy ?? FALLBACK_POLICY;
  return {
    brand: { ...base.brand },
    theme: { ...(base.theme ?? {}) },
    layout: { enabled: Boolean(base.layout?.enabled), source: base.layout?.source ?? "" },
    code: { ...FALLBACK_POLICY.code, ...base.code },
    scenes: { ...(base.scenes ?? {}) },
    suppression: { ...FALLBACK_POLICY.suppression, ...base.suppression },
    resetLinks: [...(base.resetLinks ?? [])]
  };
}

/** 比较用的规整形态：丢掉空文案与空场景，与后端 NormalizePolicy 的取舍一致 */
function canonical(policy: EmailPolicy) {
  const scenes: Record<string, unknown> = {};
  for (const [key, setting] of Object.entries(policy.scenes)) {
    const template =
      setting.template && (setting.template.body?.trim() || setting.template.text?.trim())
        ? {
            enabled: Boolean(setting.template.enabled),
            format: setting.template.format === "html" ? "html" : "mjml",
            body: setting.template.body ?? "",
            text: setting.template.text ?? ""
          }
        : undefined;
    const trimmed = {
      enabled: setting.enabled,
      subject: setting.subject?.trim() || undefined,
      title: setting.title?.trim() || undefined,
      lead: setting.lead?.trim() || undefined,
      footer: setting.footer?.trim() || undefined,
      template
    };
    if (Object.values(trimmed).some((value) => value !== undefined)) scenes[key] = trimmed;
  }
  const brand = Object.fromEntries(
    Object.entries(policy.brand).map(([key, value]) => [key, (value ?? "").trim()]).filter(([, value]) => value)
  );
  const theme = Object.fromEntries(
    Object.entries(policy.theme ?? {}).filter(
      ([key, value]) => value !== undefined && value !== null && value !== "" && !(key === "logoHeight" && value === 0)
    )
  );
  const layout = policy.layout?.source?.trim()
    ? { enabled: Boolean(policy.layout.enabled), source: policy.layout.source }
    : null;
  return JSON.stringify({
    brand,
    theme: Object.fromEntries(Object.entries(theme).sort(([a], [b]) => a.localeCompare(b))),
    layout,
    code: policy.code,
    scenes: Object.fromEntries(Object.entries(scenes).sort(([a], [b]) => a.localeCompare(b))),
    suppression: policy.suppression,
    resetLinks: policy.resetLinks.map((link) => link.trim()).filter(Boolean)
  });
}

export type EmailPolicyDraft = {
  loading: boolean;
  view?: EmailPolicyView;
  policy: EmailPolicy;
  dirty: boolean;
  saving: boolean;
  update: (next: EmailPolicy | ((current: EmailPolicy) => EmailPolicy)) => void;
  reset: () => void;
  save: () => Promise<void>;
};

/**
 * 模板与发信策略两个视图编辑的是同一份策略，草稿提到区块这一层：
 * 在模板里改了品牌名、切到策略页再保存，改动不会丢。草稿按作用域绑定，切换应用即作废。
 */
export function useEmailPolicyDraft(scope: EmailScope): EmailPolicyDraft {
  const query = useEmailPolicyQuery(scope);
  const mutation = useSaveEmailPolicyMutation(scope);
  const [draft, setDraft] = useState<{ scope: string; value: EmailPolicy } | null>(null);
  const id = scopeId(scope);
  // 记忆化：草稿为空时 policy 就是它，每次渲染都新建对象会让下游的防抖预览不停重置
  const saved = useMemo(() => normalize(query.data?.policy), [query.data]);
  const policy = draft?.scope === id ? draft.value : saved;
  const dirty = !query.isLoading && canonical(policy) !== canonical(saved);

  return {
    loading: query.isLoading,
    view: query.data,
    policy,
    dirty,
    saving: mutation.isPending,
    update: (next) =>
      setDraft({ scope: id, value: typeof next === "function" ? next(policy) : next }),
    reset: () => setDraft(null),
    save: async () => {
      try {
        await mutation.mutateAsync({
          ...policy,
          resetLinks: policy.resetLinks.map((link) => link.trim()).filter(Boolean)
        });
        setDraft(null);
        toast.success("邮件策略已保存");
      } catch (error) {
        toast.error(error instanceof ApiError ? error.message : "保存失败");
      }
    }
  };
}
