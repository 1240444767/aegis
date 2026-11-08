"use client";

import { useRef, useState } from "react";
import type { editor } from "monaco-editor";
import {
  AlertTriangle,
  Braces,
  ChevronDown,
  Code2,
  Download,
  FileText,
  LayoutPanelTop,
  Loader2,
  Monitor,
  RotateCcw,
  Smartphone,
  Sparkles,
  Type
} from "lucide-react";
import { FieldGroup, SectionCard } from "@/components/apps/app-config-primitives";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type {
  EmailPolicy,
  EmailScene,
  EmailSceneCatalog,
  EmailSceneSetting,
  EmailSceneTemplate,
  EmailTemplateProblem,
  EmailTemplateReference
} from "@/lib/api/email-policy";
import { useEmailPreviewQuery, type EmailScope } from "@/lib/email-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { EmailCodeEditor, insertAtCursor } from "./email-code-editor";
import type { EmailPolicyDraft } from "./use-email-policy-draft";

type CopyField = "subject" | "title" | "lead" | "footer";

const COPY_FIELDS: Array<{ key: CopyField; label: string; max: number; multiline?: boolean }> = [
  { key: "subject", label: "主题", max: 120 },
  { key: "title", label: "标题", max: 60 },
  { key: "lead", label: "引导语", max: 300, multiline: true },
  { key: "footer", label: "页脚提示", max: 300, multiline: true }
];

const PART_LABELS: Record<string, string> = { layout: "版式", body: "正文", text: "纯文本", copy: "文案", subject: "主题" };

/**
 * 一个场景的编辑器：「文案」只改四段文字（Liquid，可用变量），「模板代码」直接写这个场景的正文。
 * 两者可以同时生效：自定义正文里的 {{ mail.title }} 读到的就是文案里改写后的标题。
 */
export function SceneEditor({
  scope,
  draft,
  scene,
  catalog,
  disabled
}: {
  scope: EmailScope;
  draft: EmailPolicyDraft;
  scene: EmailScene;
  catalog: EmailSceneCatalog;
  disabled: boolean;
}) {
  const setting = draft.policy.scenes[scene.key] ?? {};
  const template: EmailSceneTemplate = setting.template ?? { enabled: false, format: "mjml", body: "", text: "" };
  const [tab, setTab] = useState<"copy" | "code">(template.enabled ? "code" : "copy");
  const update = (next: EmailSceneSetting) =>
    draft.update((prev) => ({ ...prev, scenes: { ...prev.scenes, [scene.key]: next } }));
  const updateTemplate = (patch: Partial<EmailSceneTemplate>) => update({ ...setting, template: { ...template, ...patch } });
  const preview = usePreview(scope, scene.key, draft.policy);
  const enabled = scene.toggle ? setting.enabled ?? scene.defaultOn : true;
  const copyEdited = Boolean(setting.subject || setting.title || setting.lead || setting.footer);

  return (
    <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <SectionCard
        icon={<FileText className="size-4" />}
        title={scene.name}
        description={scene.description}
        aside={
          scene.toggle ? (
            <label className="flex items-center gap-2 text-xs">
              <span className="text-muted-foreground">{enabled ? "发送" : "不发送"}</span>
              <Switch checked={enabled} disabled={disabled} onCheckedChange={(value) => update({ ...setting, enabled: value })} />
            </label>
          ) : null
        }
      >
        <Tabs value={tab} onValueChange={(value) => setTab(value as "copy" | "code")}>
          <TabsList className="mb-4">
            <TabsTrigger value="copy" className="gap-1.5 text-xs">
              <Type className="size-3.5" /> 文案
              {copyEdited ? <span className="size-1.5 rounded-full bg-sky-500" /> : null}
            </TabsTrigger>
            <TabsTrigger value="code" className="gap-1.5 text-xs">
              <Code2 className="size-3.5" /> 模板代码
              {template.enabled ? <span className="size-1.5 rounded-full bg-sky-500" /> : null}
            </TabsTrigger>
          </TabsList>

          <TabsContent value="copy" className="mt-0">
            <CopyEditor scene={scene} setting={setting} disabled={disabled} onChange={update} />
          </TabsContent>

          <TabsContent value="code" className="mt-0">
            <TemplateCodeEditor
              template={template}
              builtinBody={catalog.templates.bodies[scene.key] ?? catalog.templates.bodies.blocks ?? ""}
              reference={catalog.reference}
              sceneVariables={scene.variables.map((variable) => ({ name: variable.key, syntax: `{{ ${variable.key} }}`, description: variable.label }))}
              problems={template.enabled ? preview.problems : []}
              disabled={disabled}
              onChange={updateTemplate}
            />
          </TabsContent>
        </Tabs>
      </SectionCard>

      <PreviewPane preview={preview} />
    </div>
  );
}

/**
 * 全局版式：作用域内每一封信的外框（页眉、卡片、页脚），用 {% content %} 标出正文位置。
 * 预览可以换成任意场景，看同一份版式套在不同正文上的样子。
 */
export function LayoutEditor({
  scope,
  draft,
  catalog,
  disabled
}: {
  scope: EmailScope;
  draft: EmailPolicyDraft;
  catalog: EmailSceneCatalog;
  disabled: boolean;
}) {
  const layout = draft.policy.layout ?? { enabled: false, source: "" };
  const [previewScene, setPreviewScene] = useState(catalog.scenes[0]?.key ?? "code_login");
  const preview = usePreview(scope, previewScene, draft.policy);
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  const [confirmLoad, setConfirmLoad] = useState(false);
  const setLayout = (patch: Partial<typeof layout>) => draft.update((prev) => ({ ...prev, layout: { ...layout, ...patch } }));
  const source = layout.source ?? "";
  const missingContent = layout.enabled && source.trim() !== "" && !/\{%-?\s*content\s*-?%\}|\{\{-?\s*content\s*-?\}\}/.test(source);
  const layoutProblems = layout.enabled ? preview.problems.filter((problem) => problem.part === "layout") : [];

  const loadBuiltin = () => {
    setLayout({ source: catalog.templates.layout });
    setConfirmLoad(false);
  };

  return (
    <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <SectionCard
        icon={<LayoutPanelTop className="size-4" />}
        title="全局版式"
        description="所有邮件共用的外框：页眉、信体卡片、页脚。正文的位置用 {% content %} 标出"
        aside={
          <label className="flex items-center gap-2 text-xs">
            <span className="text-muted-foreground">{layout.enabled ? "使用自定义版式" : "使用内置版式"}</span>
            <Switch checked={layout.enabled} disabled={disabled || !source.trim()} onCheckedChange={(value) => setLayout({ enabled: value })} />
          </label>
        }
      >
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              className="h-8"
              disabled={disabled}
              onClick={() => (source.trim() ? setConfirmLoad(true) : loadBuiltin())}
            >
              <Download className="size-3.5" /> {source.trim() ? "重新载入内置版式" : "从内置版式开始"}
            </Button>
            {!source.trim() ? <span className="text-[11px] text-muted-foreground">载入内置版式的源码后在此基础上修改</span> : null}
          </div>
          {source.trim() || layout.enabled ? (
            <EmailCodeEditor
              value={source}
              onChange={(value) => setLayout({ source: value })}
              reference={catalog.reference}
              problems={layoutProblems}
              readOnly={disabled}
              height={520}
              onReady={(instance) => (editorRef.current = instance)}
            />
          ) : (
            <EmptyCode label="当前使用内置版式" />
          )}
          {missingContent ? <ProblemList problems={[{ part: "layout", message: "版式里没有 {% content %}，正文不会出现" }]} /> : null}
          <ProblemList problems={layoutProblems} />
          <ReferencePalette reference={catalog.reference} onInsert={(text) => insertAtCursor(editorRef.current, text)} disabled={disabled} />
        </div>
      </SectionCard>

      <div className="space-y-3">
        <div className="flex items-center justify-end gap-2">
          <span className="text-xs text-muted-foreground">用这个场景预览</span>
          <Select value={previewScene} onValueChange={setPreviewScene}>
            <SelectTrigger className="h-8 w-44 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {catalog.scenes.map((scene) => (
                <SelectItem key={scene.key} value={scene.key}>
                  {scene.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <PreviewPane preview={preview} />
      </div>

      <AlertDialog open={confirmLoad} onOpenChange={setConfirmLoad}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重新载入内置版式</AlertDialogTitle>
            <AlertDialogDescription>编辑器里现有的版式源码会被内置版式替换。保存之前都可以放弃修改。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={loadBuiltin}>替换</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function CopyEditor({
  scene,
  setting,
  disabled,
  onChange
}: {
  scene: EmailScene;
  setting: EmailSceneSetting;
  disabled: boolean;
  onChange: (next: EmailSceneSetting) => void;
}) {
  const focused = useRef<{ field: CopyField; element: HTMLInputElement | HTMLTextAreaElement } | null>(null);
  const edited = Boolean(setting.subject || setting.title || setting.lead || setting.footer);

  const insertVariable = (key: string) => {
    const token = `{{ ${key} }}`;
    const target = focused.current;
    const field = target?.field ?? "subject";
    const value = setting[field] ?? "";
    let next = value + token;
    if (target) {
      const start = target.element.selectionStart ?? value.length;
      const end = target.element.selectionEnd ?? value.length;
      next = value.slice(0, start) + token + value.slice(end);
      requestAnimationFrame(() => {
        target.element.focus();
        target.element.setSelectionRange(start + token.length, start + token.length);
      });
    }
    onChange({ ...setting, [field]: next });
  };

  return (
    <div className="space-y-4">
      {COPY_FIELDS.map((field) => {
        const value = setting[field.key] ?? "";
        const common = {
          value,
          disabled,
          maxLength: field.max,
          placeholder: scene.defaults[field.key],
          onFocus: (event: React.FocusEvent<HTMLInputElement | HTMLTextAreaElement>) => {
            focused.current = { field: field.key, element: event.currentTarget };
          },
          onChange: (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange({ ...setting, [field.key]: event.target.value })
        };
        return (
          <div key={field.key} className="space-y-1.5">
            <div className="flex items-baseline justify-between gap-2">
              <Label className="text-[11px] font-medium">{field.label}</Label>
              <span className="text-[10px] tabular-nums text-muted-foreground">{value ? `${value.length}/${field.max}` : "默认"}</span>
            </div>
            {field.multiline ? <Textarea {...common} rows={2} className="resize-none text-sm" /> : <Input {...common} className="text-sm" />}
          </div>
        );
      })}

      <FieldGroup label="可用变量" hint="点击插入到光标处，也可以用 Liquid 过滤器">
        <div className="flex flex-wrap gap-1.5">
          {scene.variables.map((variable) => (
            <button
              key={variable.key}
              type="button"
              disabled={disabled}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => insertVariable(variable.key)}
              title={`示例：${variable.example}`}
              className="inline-flex items-center gap-1 rounded-lg border bg-muted/40 px-2 py-1 text-[11px] transition-colors hover:border-primary/40 hover:bg-muted disabled:opacity-50"
            >
              <Braces className="size-3 text-muted-foreground" />
              <span className="font-mono">{`{{ ${variable.key} }}`}</span>
              <span className="text-muted-foreground">{variable.label}</span>
            </button>
          ))}
        </div>
      </FieldGroup>

      {edited && !disabled ? (
        <Button
          size="sm"
          variant="ghost"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => onChange({ enabled: setting.enabled, template: setting.template })}
        >
          <RotateCcw className="size-3.5" /> 恢复默认文案
        </Button>
      ) : null}
    </div>
  );
}

function TemplateCodeEditor({
  template,
  builtinBody,
  reference,
  sceneVariables,
  problems,
  disabled,
  onChange
}: {
  template: EmailSceneTemplate;
  builtinBody: string;
  reference: EmailTemplateReference;
  sceneVariables: Array<{ name: string; syntax: string; description: string }>;
  problems: EmailTemplateProblem[];
  disabled: boolean;
  onChange: (patch: Partial<EmailSceneTemplate>) => void;
}) {
  const bodyEditor = useRef<editor.IStandaloneCodeEditor | null>(null);
  const textEditor = useRef<editor.IStandaloneCodeEditor | null>(null);
  const activeEditor = useRef<"body" | "text">("body");
  const [confirmLoad, setConfirmLoad] = useState(false);
  const [textOpen, setTextOpen] = useState(Boolean(template.text?.trim()));
  const body = template.body ?? "";
  const format = template.format === "html" ? "html" : "mjml";
  const bodyProblems = problems.filter((problem) => problem.part === "body");
  const textProblems = problems.filter((problem) => problem.part === "text");
  const otherProblems = problems.filter((problem) => problem.part !== "body" && problem.part !== "text");
  const referenceWithScene: EmailTemplateReference = { ...reference, globals: [...sceneVariables, ...reference.globals] };

  const loadBuiltin = () => {
    onChange({ body: builtinBody, format: "mjml" });
    setConfirmLoad(false);
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-muted px-3 py-2.5">
        <label className="flex items-center gap-2 text-xs font-medium">
          <Switch checked={template.enabled} disabled={disabled || !body.trim()} onCheckedChange={(value) => onChange({ enabled: value })} />
          {template.enabled ? "使用自定义模板" : "使用内置正文"}
        </label>
        <div className="flex items-center gap-2">
          <ToggleGroup type="single" size="sm" value={format} disabled={disabled} onValueChange={(next) => next && onChange({ format: next })}>
            <ToggleGroupItem value="mjml" className="h-7 px-2.5 text-xs">MJML</ToggleGroupItem>
            <ToggleGroupItem value="html" className="h-7 px-2.5 text-xs">HTML</ToggleGroupItem>
          </ToggleGroup>
          <Button size="sm" variant="outline" className="h-7 text-xs" disabled={disabled} onClick={() => (body.trim() ? setConfirmLoad(true) : loadBuiltin())}>
            <Download className="size-3.5" /> {body.trim() ? "重新载入内置正文" : "从内置正文开始"}
          </Button>
        </div>
      </div>

      <p className="text-[11px] leading-relaxed text-muted-foreground">
        {format === "mjml"
          ? "写在一栏里的 MJML 组件（mj-text、mj-button、mj-image……），会套进全局版式；以 <mjml> 开头的完整文档不套版式。"
          : "完整的 HTML 文档，不套版式，<style> 里的样式会自动内联。HTML 格式里不能用内置组件。"}
      </p>

      {body.trim() || template.enabled ? (
        <div onFocusCapture={() => (activeEditor.current = "body")}>
          <EmailCodeEditor
            value={body}
            onChange={(value) => onChange({ body: value })}
            reference={referenceWithScene}
            problems={bodyProblems}
            readOnly={disabled}
            height={380}
            onReady={(instance) => (bodyEditor.current = instance)}
          />
        </div>
      ) : (
        <EmptyCode label="当前使用内置正文" />
      )}
      <ProblemList problems={[...bodyProblems, ...otherProblems]} />

      <Collapsible open={textOpen} onOpenChange={setTextOpen}>
        <CollapsibleTrigger className="flex w-full items-center justify-between rounded-lg px-1 py-1 text-xs font-medium text-muted-foreground hover:text-foreground">
          <span>纯文本模板{template.text?.trim() ? "（已填写）" : "（可选，留空从网页版抓取）"}</span>
          <ChevronDown className={cn("size-3.5 transition-transform", textOpen && "rotate-180")} />
        </CollapsibleTrigger>
        <CollapsibleContent className="space-y-2 pt-2">
          <div onFocusCapture={() => (activeEditor.current = "text")}>
            <EmailCodeEditor
              value={template.text ?? ""}
              onChange={(value) => onChange({ text: value })}
              reference={referenceWithScene}
              problems={textProblems}
              readOnly={disabled}
              height={160}
              onReady={(instance) => (textEditor.current = instance)}
            />
          </div>
          <ProblemList problems={textProblems} />
        </CollapsibleContent>
      </Collapsible>

      <ReferencePalette
        reference={referenceWithScene}
        components={format === "mjml"}
        disabled={disabled}
        onInsert={(text) => insertAtCursor(activeEditor.current === "text" ? textEditor.current : bodyEditor.current, text)}
      />

      <AlertDialog open={confirmLoad} onOpenChange={setConfirmLoad}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重新载入内置正文</AlertDialogTitle>
            <AlertDialogDescription>编辑器里现有的模板会被内置正文替换。保存之前都可以放弃修改。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={loadBuiltin}>替换</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** 变量、组件、过滤器的速查与插入 */
function ReferencePalette({
  reference,
  onInsert,
  components = true,
  disabled
}: {
  reference: EmailTemplateReference;
  onInsert: (text: string) => void;
  components?: boolean;
  disabled?: boolean;
}) {
  const groups = [
    ...(components ? [{ key: "components", label: "组件", items: reference.components }] : []),
    { key: "globals", label: "变量", items: reference.globals },
    { key: "theme", label: "主题", items: reference.theme },
    { key: "filters", label: "过滤器", items: reference.filters }
  ];
  const [active, setActive] = useState(groups[0].key);
  const current = groups.find((group) => group.key === active) ?? groups[0];
  return (
    <div className="rounded-xl border">
      <div className="flex flex-wrap items-center gap-1 border-b px-2 py-1.5">
        <Sparkles className="ml-1 mr-1 size-3.5 text-muted-foreground" />
        {groups.map((group) => (
          <button
            key={group.key}
            type="button"
            onClick={() => setActive(group.key)}
            className={cn(
              "rounded-md px-2 py-1 text-[11px] font-medium transition-colors",
              active === group.key ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"
            )}
          >
            {group.label}
          </button>
        ))}
        <span className="ml-auto pr-1 text-[10px] text-muted-foreground">点击插入到光标处</span>
      </div>
      <div className="grid max-h-56 gap-1 overflow-y-auto p-2 sm:grid-cols-2">
        {current.items.map((item) => (
          <button
            key={item.name + item.syntax}
            type="button"
            disabled={disabled}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => onInsert(item.syntax)}
            className="rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-muted disabled:opacity-50"
          >
            <div className="truncate font-mono text-[11px]">{item.syntax}</div>
            <div className="truncate text-[10px] text-muted-foreground">{item.description}</div>
          </button>
        ))}
      </div>
    </div>
  );
}

function ProblemList({ problems }: { problems: EmailTemplateProblem[] }) {
  if (!problems.length) return null;
  return (
    <div className="space-y-1 rounded-xl border border-red-200 bg-red-50/60 px-3 py-2.5 dark:border-red-900/60 dark:bg-red-950/30">
      {problems.slice(0, 6).map((problem, index) => (
        <div key={index} className="flex items-start gap-2 text-xs text-red-700 dark:text-red-300">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span className="break-all">
            {PART_LABELS[problem.part] ?? problem.part}
            {problem.line ? `第 ${problem.line} 行` : ""}：{problem.message}
          </span>
        </div>
      ))}
    </div>
  );
}

function EmptyCode({ label }: { label: string }) {
  return (
    <div className="flex h-40 flex-col items-center justify-center gap-1.5 rounded-lg border border-dashed text-center">
      <Code2 className="size-5 text-muted-foreground/60" />
      <p className="text-xs text-muted-foreground">{label}</p>
    </div>
  );
}

// ── 预览 ──

type PreviewState = ReturnType<typeof usePreview>;

function usePreview(scope: EmailScope, scene: string, policy: EmailPolicy) {
  // 跟着草稿走，500ms 防抖：模板代码每敲一个字都请求一次服务端渲染没有必要
  const debounced = useDebouncedValue(policy, 500);
  const query = useEmailPreviewQuery(scope, scene, debounced);
  return {
    data: query.data,
    problems: query.data?.problems ?? [],
    fetching: query.isFetching,
    error: query.isError ? (query.error instanceof Error ? query.error.message : "预览失败") : null
  };
}

function PreviewPane({ preview }: { preview: PreviewState }) {
  const [device, setDevice] = useState<"desktop" | "mobile">("desktop");
  const [format, setFormat] = useState<"html" | "text">("html");
  const data = preview.data;
  return (
    <section className="flex min-h-[640px] flex-col overflow-hidden rounded-2xl border bg-card 2xl:sticky 2xl:top-4 2xl:self-start">
      <header className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
        <div className="flex items-center gap-2 text-sm font-semibold">
          <Sparkles className="size-4 text-muted-foreground" /> 预览
          {preview.fetching ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
          {preview.problems.length ? (
            <Badge variant="danger" size="sm" className="font-normal">
              {preview.problems.length} 个问题
            </Badge>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <ToggleGroup type="single" size="sm" value={format} onValueChange={(next) => next && setFormat(next as "html" | "text")}>
            <ToggleGroupItem value="html" className="h-7 px-2.5 text-xs">网页</ToggleGroupItem>
            <ToggleGroupItem value="text" className="h-7 px-2.5 text-xs">纯文本</ToggleGroupItem>
          </ToggleGroup>
          <ToggleGroup type="single" size="sm" value={device} onValueChange={(next) => next && setDevice(next as "desktop" | "mobile")}>
            <ToggleGroupItem value="desktop" className="h-7 px-2" aria-label="桌面">
              <Monitor className="size-3.5" />
            </ToggleGroupItem>
            <ToggleGroupItem value="mobile" className="h-7 px-2" aria-label="手机">
              <Smartphone className="size-3.5" />
            </ToggleGroupItem>
          </ToggleGroup>
        </div>
      </header>

      <div className="border-b bg-muted/30 px-4 py-2.5">
        <div className="text-[10px] text-muted-foreground">主题</div>
        {data ? (
          <div className="truncate text-sm font-medium" title={data.subject}>
            {data.subject}
          </div>
        ) : (
          <Skeleton className="mt-1 h-4 w-2/3" />
        )}
      </div>

      <div className="flex flex-1 justify-center bg-muted/40 p-4">
        {preview.error ? (
          <div className="self-center text-xs text-destructive">{preview.error}</div>
        ) : !data ? (
          <Skeleton className="h-full min-h-[520px] w-full max-w-[640px] rounded-xl" />
        ) : !data.html ? (
          <div className="flex flex-col items-center justify-center gap-2 self-center text-center">
            <AlertTriangle className="size-6 text-red-500" />
            <p className="text-sm font-medium">模板有问题，无法渲染</p>
            <p className="max-w-sm text-xs text-muted-foreground">{data.problems[0]?.message}</p>
          </div>
        ) : format === "html" ? (
          <iframe
            title="邮件预览"
            sandbox=""
            srcDoc={data.html}
            className={cn(
              "h-[600px] rounded-xl border bg-white shadow-sm transition-[width] duration-200 ease-[cubic-bezier(0.22,1,0.36,1)]",
              device === "mobile" ? "w-[375px]" : "w-full max-w-[680px]"
            )}
          />
        ) : (
          <pre
            className={cn(
              "h-[600px] overflow-auto whitespace-pre-wrap rounded-xl border bg-background p-4 font-mono text-xs leading-relaxed",
              device === "mobile" ? "w-[375px]" : "w-full max-w-[680px]"
            )}
          >
            {data.text}
          </pre>
        )}
      </div>
    </section>
  );
}
