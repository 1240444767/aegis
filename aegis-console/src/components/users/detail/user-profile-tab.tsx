"use client";

import { useMemo, useRef, useState, type ComponentType, type ReactNode } from "react";
import {
  AtSign,
  Cake,
  Camera,
  Check,
  ImageUp,
  Link2,
  Loader2,
  Mail,
  MapPin,
  Phone,
  Quote,
  Shapes,
  Trash2,
  Undo2,
  UserPlus,
  UserRound,
  X
} from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { BrandIcon, getBrandIcon } from "@/components/ui/brand-icon";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useNow } from "@/components/users/ban-form";
import { ApiError } from "@/lib/api-client";
import {
  useRemoveAdminAppUserAvatarMutation,
  useUpdateAdminAppUserProfileMutation,
  useUploadAdminAppUserAvatarMutation
} from "@/lib/admin-hooks";
import type { AdminAppUserDetail } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { formatDate, inputValue, relativeTime } from "./user-detail-shared";
import { UserPreferences, humanizeKey, readableValue } from "./user-preferences";

/**
 * 资料：这个人是谁、怎么联系、喜欢怎么用。
 *
 * 账号层面的事实（自定义 ID、邀请码、标识码、注册 IP）在「概览」里已经列过，
 * 这里不再摆一张「字段名：值」的明细表，而是：
 *
 *   资料卡     头像、称呼、简介、联系方式、生日与年龄，以及一句话的来历
 *              （何时从哪注册、由谁邀请）
 *   编辑资料   分成「称呼与生日」「联系方式」「简介」三组，改过的字段带标记，
 *              邮箱与手机号当场校验，底部固定显示未保存的改动
 *   自定义资料 应用写进 extra 的字段，按值的类型显示
 *   偏好设置   用户端设置翻译成人话（见 user-preferences）
 *
 * 保存走按字段 PATCH：只发**改过的**字段；改过且为空 = 清空（生日走 clearBirthday）。
 */

/** 头像上传的入口限制：超过 8MB 的原图先在这里拦下，别等后端报错。 */
const AVATAR_MAX_BYTES = 8 * 1024 * 1024;
/** 与后端 user_service.go 的 profile*MaxLen 一致 */
const NICKNAME_MAX = 128;
const EMAIL_MAX = 255;
const PHONE_MAX = 32;
const BIO_MAX = 2000;
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

const CONTACT_META: Record<string, { label: string; icon?: ComponentType<{ className?: string }> }> = {
  wechat: { label: "微信" },
  qq: { label: "QQ" },
  telegram: { label: "Telegram" },
  discord: { label: "Discord" },
  twitter: { label: "X" },
  github: { label: "GitHub" },
  gitee: { label: "Gitee" },
  weibo: { label: "微博" },
  phone: { label: "电话", icon: Phone },
  email: { label: "邮箱", icon: Mail },
  other: { label: "其他", icon: AtSign }
};

/** RFC3339 / 零值时间 → date input 的 YYYY-MM-DD；没有就空串。生日是日历日，按 UTC 取年月日。 */
function birthdayInputValue(raw?: string | null) {
  if (!raw) return "";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}`;
}

/** 本地日期 YYYY-MM-DD，用作生日输入的上限 */
function todayKey(now: number) {
  const d = new Date(now);
  const pad = (v: number) => String(v).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** 「3 月 14 日，28 岁，还有 12 天过生日」 */
function describeBirthday(value: string, now: number) {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  if (!match) return null;
  const [year, month, day] = [Number(match[1]), Number(match[2]), Number(match[3])];
  const today = new Date(now);
  let age = today.getFullYear() - year;
  const thisYear = new Date(today.getFullYear(), month - 1, day);
  const todayStart = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  if (todayStart < thisYear) age -= 1;
  let next = thisYear;
  if (next < todayStart) next = new Date(today.getFullYear() + 1, month - 1, day);
  const days = Math.round((next.getTime() - todayStart.getTime()) / 86_400_000);
  const countdown = days === 0 ? "今天生日" : days <= 30 ? `还有 ${days} 天过生日` : "";
  return {
    date: `${month} 月 ${day} 日`,
    age: age >= 0 && age < 150 ? `${age} 岁` : "",
    countdown
  };
}

type ProfileForm = {
  nickname: string;
  email: string;
  phone: string;
  birthday: string;
  bio: string;
};

const FIELD_LABEL: Record<keyof ProfileForm, string> = {
  nickname: "昵称",
  email: "邮箱",
  phone: "手机号",
  birthday: "生日",
  bio: "简介"
};

export function UserProfileTab({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  const profile = user.profile;
  const mutation = useUpdateAdminAppUserProfileMutation(appKey, userId);
  const scope = `${appKey}:${userId}`;
  const now = useNow(60 * 60_000);

  const original: ProfileForm = useMemo(
    () => ({
      nickname: inputValue(user.nickname ?? profile?.nickname),
      email: inputValue(user.email ?? profile?.email),
      phone: inputValue(user.phone ?? profile?.phone),
      birthday: birthdayInputValue(profile?.birthday),
      bio: inputValue(profile?.bio)
    }),
    [profile, user]
  );

  // 草稿按 scope 绑定：换个用户时上一个人的未保存改动不会串过来，
  // 也不需要一个 useEffect 去同步（与配置面板同一约束）。
  const [draft, setDraft] = useState<{ scope: string; value: ProfileForm } | null>(null);
  const form = draft?.scope === scope ? draft.value : original;

  const changedKeys = (Object.keys(original) as Array<keyof ProfileForm>).filter((key) => form[key] !== original[key]);
  const dirty = changedKeys.length > 0;

  // 规则与后端 validateAdminUserProfilePatch 相同，当场提示，省一次往返
  const errors: Partial<Record<keyof ProfileForm, string>> = {};
  const length = (value: string) => Array.from(value.trim()).length;
  if (length(form.nickname) > NICKNAME_MAX) errors.nickname = `昵称最多 ${NICKNAME_MAX} 个字符`;
  if (form.email.trim() && !EMAIL_RE.test(form.email.trim())) errors.email = "邮箱格式不正确";
  else if (length(form.email) > EMAIL_MAX) errors.email = `邮箱最多 ${EMAIL_MAX} 个字符`;
  if (length(form.phone) > PHONE_MAX) errors.phone = `手机号最多 ${PHONE_MAX} 个字符`;
  if (length(form.bio) > BIO_MAX) errors.bio = `简介最多 ${BIO_MAX} 个字符`;
  if (form.birthday && form.birthday > todayKey(now)) errors.birthday = "生日不能是未来的日期";
  const invalid = Object.keys(errors).length > 0;

  const patch = (key: keyof ProfileForm, value: string) => setDraft({ scope, value: { ...form, [key]: value } });
  const revert = (key: keyof ProfileForm) => patch(key, original[key]);

  async function handleSave() {
    if (invalid) return;
    // 只发改过的字段：没动过的字段连键都不出现，后端一概不碰。
    const payload: Record<string, unknown> = {};
    for (const key of changedKeys) {
      if (key === "birthday") {
        const value = form.birthday.trim();
        if (value) payload.birthday = value;
        else payload.clearBirthday = true;
        continue;
      }
      payload[key] = form[key].trim();
    }
    try {
      await mutation.mutateAsync(payload);
      setDraft(null);
      toast.success("资料已更新");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "更新失败");
    }
  }

  const extra = profile?.extra ?? {};

  return (
    <div className="space-y-6">
      <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
        <ProfileCard appKey={appKey} userId={userId} user={user} />

        <Card className="gap-0 overflow-hidden py-0">
          <div className="flex items-center justify-between gap-3 border-b px-4 py-3.5 sm:px-5">
            <div className="flex items-center gap-2 text-sm font-semibold">
              <UserRound className="size-4 text-muted-foreground" />
              编辑资料
            </div>
            <span className="text-[11px] text-muted-foreground">只提交改过的字段，留空即清除</span>
          </div>

          <div className="space-y-5 px-4 py-5 sm:px-5">
            <FieldGroup title="称呼与生日">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="昵称" changed={form.nickname !== original.nickname} error={errors.nickname} onRevert={() => revert("nickname")}>
                  <Input value={form.nickname} placeholder="未填写" onChange={(e) => patch("nickname", e.target.value)} />
                </Field>
                <Field label="生日" changed={form.birthday !== original.birthday} error={errors.birthday} onRevert={() => revert("birthday")}>
                  <Input type="date" value={form.birthday} max={todayKey(now)} onChange={(e) => patch("birthday", e.target.value)} />
                </Field>
              </div>
            </FieldGroup>

            <FieldGroup title="联系方式">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="邮箱" changed={form.email !== original.email} error={errors.email} onRevert={() => revert("email")}>
                  <Input type="email" value={form.email} placeholder="未填写" onChange={(e) => patch("email", e.target.value)} />
                </Field>
                <Field label="手机号" changed={form.phone !== original.phone} error={errors.phone} onRevert={() => revert("phone")}>
                  <Input inputMode="tel" value={form.phone} placeholder="未填写" onChange={(e) => patch("phone", e.target.value)} />
                </Field>
              </div>
            </FieldGroup>

            <FieldGroup title="简介">
              <Field
                label="个人简介"
                changed={form.bio !== original.bio}
                error={errors.bio}
                onRevert={() => revert("bio")}
                hint={<span className={cn("tabular-nums", errors.bio && "text-destructive")}>{Array.from(form.bio.trim()).length} / {BIO_MAX}</span>}
              >
                <Textarea value={form.bio} rows={4} placeholder="未填写" className="resize-none" onChange={(e) => patch("bio", e.target.value)} />
              </Field>
            </FieldGroup>
          </div>

          <div
            className={cn(
              "flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3 transition-colors sm:px-5",
              dirty ? "bg-amber-500/[0.06]" : "bg-muted/20"
            )}
          >
            <span className="min-w-0 truncate text-xs text-muted-foreground">
              {dirty ? `未保存：${changedKeys.map((k) => FIELD_LABEL[k]).join("、")}` : "资料与线上一致"}
            </span>
            <div className="flex shrink-0 items-center gap-2">
              <Button size="sm" variant="ghost" className="h-8" disabled={!dirty || mutation.isPending} onClick={() => setDraft(null)}>
                放弃改动
              </Button>
              <Button size="sm" className="h-8" disabled={!dirty || invalid || mutation.isPending} onClick={handleSave}>
                {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Check className="size-3.5" />}
                保存
              </Button>
            </div>
          </div>
        </Card>
      </div>

      {Object.keys(extra).length ? <CustomFields extra={extra} /> : null}

      <UserPreferences user={user} />
    </div>
  );
}

// ── 资料卡 ──────────────────────────

function ProfileCard({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const profile = user.profile;
  const now = useNow(60 * 60_000);
  const name = user.nickname || profile?.nickname || user.account || `用户 ${userId}`;
  const bio = (profile?.bio ?? "").trim();
  const email = user.email || profile?.email;
  const phone = user.phone || profile?.phone;
  const birthday = describeBirthday(birthdayInputValue(profile?.birthday), now);
  const contacts = (profile?.contacts ?? []).filter((c) => c.value?.trim());

  const place = [user.registerProvince || profile?.registerProvince, user.registerCity || profile?.registerCity]
    .filter((v, i, arr) => v && arr.indexOf(v) === i)
    .join(" ");
  const registeredAt = user.registerTime || profile?.registerTime || user.createdAt;
  const inviter = profile?.parentInviteAccount;

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="flex gap-4 px-4 pt-5 pb-4 sm:px-5">
        <AvatarEditor appKey={appKey} userId={userId} user={user} />
        <div className="min-w-0 flex-1 pt-1">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <h3 className="truncate text-lg font-semibold tracking-tight">{name}</h3>
            {profile?.role ? (
              <Badge variant="secondary" size="sm">
                {profile.role}
              </Badge>
            ) : null}
          </div>
          <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
            {user.account ? <span className="truncate font-mono">@{user.account}</span> : null}
            {profile?.customId ? (
              <span className="truncate" title={typeof profile.customIdCount === "number" ? `已修改 ${profile.customIdCount} 次` : undefined}>
                ID <span className="font-mono text-foreground/80">{profile.customId}</span>
              </span>
            ) : null}
          </div>
        </div>
      </div>

      <div className="px-4 pb-4 sm:px-5">
        {bio ? (
          <blockquote className="relative rounded-xl bg-muted/40 px-4 py-3 text-sm leading-6 text-foreground/90">
            <Quote className="absolute top-2.5 left-2.5 size-3 text-muted-foreground/50" />
            <p className="pl-3 whitespace-pre-wrap break-words">{bio}</p>
          </blockquote>
        ) : (
          <p className="rounded-xl border border-dashed px-4 py-3 text-sm text-muted-foreground">还没有写个人简介</p>
        )}
      </div>

      <ul className="space-y-1 border-t px-2 py-2 sm:px-3">
        <ContactLine icon={Mail} label="邮箱" value={email} href={email ? `mailto:${email}` : undefined} />
        <ContactLine icon={Phone} label="手机" value={phone} href={phone ? `tel:${phone.replace(/\s/g, "")}` : undefined} mono />
        <ContactLine
          icon={Cake}
          label="生日"
          value={birthday ? [birthday.date, birthday.age].filter(Boolean).join("，") : undefined}
          trailing={birthday?.countdown ? <Badge variant="info" size="sm">{birthday.countdown}</Badge> : null}
        />
        {contacts.map((contact, i) => {
          const key = contact.platform?.toLowerCase() ?? "other";
          const meta = CONTACT_META[key] ?? { label: contact.label || humanizeKey(key) };
          const brand = getBrandIcon(key);
          return (
            <ContactLine
              key={`${key}-${i}`}
              icon={meta.icon ?? AtSign}
              brand={brand ? key : undefined}
              label={contact.label || meta.label}
              value={contact.value}
              copyable
            />
          );
        })}
      </ul>

      <div className="space-y-1.5 border-t bg-muted/20 px-4 py-3 text-xs text-muted-foreground sm:px-5">
        <div className="flex items-center gap-2">
          <MapPin className="size-3.5 shrink-0" />
          <span className="min-w-0 truncate">
            {registeredAt ? `${formatDate(registeredAt)} ` : ""}
            {place ? `在 ${place} 注册` : "注册"}
            {registeredAt ? `，距今 ${relativeTime(registeredAt).replace(/前$/, "")}` : ""}
          </span>
        </div>
        {inviter ? (
          <div className="flex items-center gap-2">
            <UserPlus className="size-3.5 shrink-0" />
            <span className="min-w-0 truncate">
              由 <span className="font-medium text-foreground">{inviter}</span> 邀请加入
            </span>
          </div>
        ) : null}
        {profile?.updatedAt ? (
          <div className="flex items-center gap-2">
            <UserRound className="size-3.5 shrink-0" />
            资料{relativeTime(profile.updatedAt)}更新
          </div>
        ) : null}
      </div>
    </Card>
  );
}

function ContactLine({
  icon: Icon,
  brand,
  label,
  value,
  href,
  mono,
  copyable,
  trailing
}: {
  icon: ComponentType<{ className?: string }>;
  brand?: string;
  label: string;
  value?: string | null;
  href?: string;
  mono?: boolean;
  copyable?: boolean;
  trailing?: ReactNode;
}) {
  const text = (value ?? "").trim();
  const content = (
    <span className={cn("min-w-0 truncate text-sm", mono && "font-mono text-[13px]", !text && "text-muted-foreground")}>{text || "未填写"}</span>
  );
  return (
    <li className="group flex items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-muted/40">
      <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
        {brand ? <BrandIcon slug={brand} className="size-3.5" /> : <Icon className="size-3.5" />}
      </span>
      <span className="w-10 shrink-0 text-xs text-muted-foreground">{label}</span>
      {text && href ? (
        <a href={href} className="min-w-0 truncate underline-offset-2 hover:underline">
          {content}
        </a>
      ) : (
        content
      )}
      <span className="ml-auto flex shrink-0 items-center gap-1">
        {trailing}
        {text && (copyable || href) ? (
          <button
            type="button"
            className="rounded px-1.5 py-0.5 text-[11px] text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 hover:text-foreground focus-visible:opacity-100"
            onClick={() => {
              void navigator.clipboard?.writeText(text).then(() => toast.success(`已复制${label}`));
            }}
          >
            复制
          </button>
        ) : null}
      </span>
    </li>
  );
}

// ── 编辑表单 ──────────────────────────

function FieldGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="space-y-3">
      <legend className="mb-3 text-[11px] font-medium tracking-wide text-muted-foreground">{title}</legend>
      {children}
    </fieldset>
  );
}

function Field({
  label,
  changed,
  error,
  hint,
  onRevert,
  children
}: {
  label: string;
  changed: boolean;
  error?: string;
  hint?: ReactNode;
  onRevert: () => void;
  children: ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex h-5 items-center justify-between gap-2">
        <span className="flex items-center gap-1.5 text-xs font-medium">
          {label}
          {changed ? <span className="size-1.5 rounded-full bg-amber-500" aria-label="已修改" /> : null}
        </span>
        <span className="flex items-center gap-2 text-[11px] text-muted-foreground">
          {hint}
          {changed ? (
            <button type="button" className="inline-flex items-center gap-0.5 hover:text-foreground" onClick={onRevert}>
              <Undo2 className="size-3" />
              还原
            </button>
          ) : null}
        </span>
      </div>
      <div className={cn("[&_input]:h-9", error && "[&_input]:border-destructive [&_textarea]:border-destructive")}>{children}</div>
      {error ? <p className="text-[11px] text-destructive">{error}</p> : null}
    </div>
  );
}

// ── 自定义资料 ──────────────────────────

function CustomFields({ extra }: { extra: Record<string, unknown> }) {
  const entries = Object.entries(extra);
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2 px-1">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <Shapes className="size-4 text-muted-foreground" />
          自定义资料
        </h3>
        <span className="text-xs text-muted-foreground">应用自行写入的 {entries.length} 个字段</span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {entries.map(([key, value]) => (
          <div key={key} className="min-w-0 rounded-2xl border bg-card px-4 py-3">
            <div className="truncate text-[11px] text-muted-foreground" title={key}>
              {humanizeKey(key)}
            </div>
            <div className="mt-1 min-w-0 text-sm">{readableValue(value)}</div>
          </div>
        ))}
      </div>
    </section>
  );
}

// ── 头像 ──────────────────────────

/**
 * 头像编辑：上传文件 / 填外链 / 移除，从头像角上的相机按钮进入。
 *
 * 三个动作即时生效，不进「保存」的草稿 —— 上传本身就是一次落库，
 * 让它再等一次保存按钮只会造出「预览换了、刷新又回去了」的错觉。
 * 外链走资料 PATCH 的 avatar 字段（后端只收 http(s)，storage:// 会被拒绝）。
 */
function AvatarEditor({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const fileRef = useRef<HTMLInputElement>(null);
  const uploadMutation = useUploadAdminAppUserAvatarMutation(appKey, userId);
  const removeMutation = useRemoveAdminAppUserAvatarMutation(appKey, userId);
  const profileMutation = useUpdateAdminAppUserProfileMutation(appKey, userId);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [linkEditing, setLinkEditing] = useState(false);
  const [link, setLink] = useState("");

  const avatar = user.avatar || user.profile?.avatar || "";
  const fallback = String(user.nickname || user.account || "U").trim().slice(0, 2).toUpperCase();
  const busy = uploadMutation.isPending || removeMutation.isPending || profileMutation.isPending;

  async function handleFile(file: File | undefined) {
    if (!file) return;
    if (!file.type.startsWith("image/")) {
      toast.error("只支持图片文件");
      return;
    }
    if (file.size > AVATAR_MAX_BYTES) {
      toast.error("图片不能超过 8MB");
      return;
    }
    try {
      await uploadMutation.mutateAsync(file);
      toast.success("头像已更新");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "上传失败");
    } finally {
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  async function handleLinkSave() {
    const value = link.trim();
    if (!/^https?:\/\//i.test(value)) {
      toast.error("请填写以 http:// 或 https:// 开头的图片地址");
      return;
    }
    try {
      await profileMutation.mutateAsync({ avatar: value });
      toast.success("头像已更新");
      setLinkEditing(false);
      setLink("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "设置失败");
    }
  }

  return (
    <div className="shrink-0">
      <div className="relative">
        <Avatar className="size-20 rounded-2xl border shadow-sm">
          <AvatarImage src={avatar} className="object-cover" />
          <AvatarFallback className="rounded-2xl text-lg font-medium">{fallback}</AvatarFallback>
        </Avatar>
        {busy ? (
          <span className="absolute inset-0 grid place-items-center rounded-2xl bg-background/60">
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          </span>
        ) : null}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="icon"
              variant="outline"
              className="absolute -right-1.5 -bottom-1.5 size-7 rounded-full bg-background shadow-sm"
              aria-label="更换头像"
              disabled={busy}
            >
              <Camera className="size-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-40">
            <DropdownMenuItem onClick={() => fileRef.current?.click()}>
              <ImageUp />
              上传图片
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setLinkEditing(true)}>
              <Link2 />
              使用图片地址
            </DropdownMenuItem>
            {avatar ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onClick={() => setConfirmRemove(true)}>
                  <Trash2 />
                  移除头像
                </DropdownMenuItem>
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <input
        ref={fileRef}
        type="file"
        accept="image/*"
        className="hidden"
        aria-label="选择头像图片"
        onChange={(event) => handleFile(event.target.files?.[0])}
      />

      {linkEditing ? (
        <div className="mt-3 flex w-64 max-w-[calc(100vw-4rem)] items-center gap-1.5">
          <Input
            autoFocus
            value={link}
            placeholder="https://"
            className="h-8 flex-1 text-xs"
            onChange={(event) => setLink(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") void handleLinkSave();
              if (event.key === "Escape") setLinkEditing(false);
            }}
          />
          <Button size="icon" className="size-8" aria-label="确定" disabled={!link.trim() || profileMutation.isPending} onClick={handleLinkSave}>
            <Check className="size-3.5" />
          </Button>
          <Button size="icon" variant="ghost" className="size-8" aria-label="取消" onClick={() => setLinkEditing(false)}>
            <X className="size-3.5" />
          </Button>
        </div>
      ) : null}

      <AlertDialog open={confirmRemove} onOpenChange={setConfirmRemove}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除头像</AlertDialogTitle>
            <AlertDialogDescription>移除后恢复为默认头像。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                try {
                  await removeMutation.mutateAsync();
                  toast.success("已移除头像");
                } catch (error) {
                  toast.error(error instanceof ApiError ? error.message : "移除失败");
                }
              }}
            >
              移除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
