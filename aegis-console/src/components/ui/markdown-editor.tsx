"use client";

import { useCallback, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import {
  Bold,
  Code,
  Columns2,
  Eye,
  Heading2,
  Heading3,
  Italic,
  Link as LinkIcon,
  List,
  ListChecks,
  ListOrdered,
  Maximize2,
  Minimize2,
  Minus,
  Palette,
  PencilLine,
  Quote,
  Strikethrough
} from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

/**
 * Markdown 编辑器：原生 textarea 加一条工具栏，预览由调用方给。
 *
 * 不用所见即所得：Markdown 的价值就在于原文即所存，所见即所得编辑器会把作者的写法改写成它自己的。
 * 工具栏只做文本替换，走 execCommand("insertText") 以保留浏览器原生的撤销栈 ——
 * 直接改 value 会让 Ctrl+Z 一次撤掉整段输入。
 *
 * 文字颜色写成 <span style="color:#…">…</span>：服务端白名单只放行这一种写法（与 <font color>）。
 */

export type MarkdownViewMode = "edit" | "split" | "preview";

export const MARKDOWN_TEXT_COLORS: Array<{ value: string; label: string }> = [
  { value: "#D32F2F", label: "红" },
  { value: "#EF6C00", label: "橙" },
  { value: "#F9A825", label: "黄" },
  { value: "#2E7D32", label: "绿" },
  { value: "#00838F", label: "青" },
  { value: "#1565C0", label: "蓝" },
  { value: "#6A1B9A", label: "紫" },
  { value: "#616161", label: "灰" }
];

type MarkdownEditorProps = {
  value: string;
  onChange: (value: string) => void;
  /** 预览区内容；不给则不显示预览切换 */
  preview?: ReactNode;
  placeholder?: string;
  maxLength?: number;
  className?: string;
  fullscreenable?: boolean;
  defaultMode?: MarkdownViewMode;
  /** 工具栏右侧的附加控件，如格式切换 */
  toolbarEnd?: ReactNode;
};

const COLOR_SPAN = /^<span style="color:\s*[^"]*">([\s\S]*)<\/span>$/i;

function ToolButton({ title, onClick, children }: { title: string; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      onMouseDown={(e) => {
        // 不让按钮抢走焦点：抢走了 textarea 的选区就丢了
        e.preventDefault();
        onClick();
      }}
      className="flex size-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
    >
      {children}
    </button>
  );
}

function Sep() {
  return <span className="mx-0.5 h-4 w-px shrink-0 bg-border" />;
}

export function MarkdownEditor({
  value,
  onChange,
  preview,
  placeholder,
  maxLength,
  className,
  fullscreenable = true,
  defaultMode = "split",
  toolbarEnd
}: MarkdownEditorProps) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [mode, setMode] = useState<MarkdownViewMode>(preview ? defaultMode : "edit");
  const [fullscreen, setFullscreen] = useState(false);
  const [colorOpen, setColorOpen] = useState(false);

  /** 用 insertText 替换 [start, end)，保留原生撤销；不支持时退回 setRangeText。 */
  const replaceRange = useCallback(
    (start: number, end: number, text: string, selectStart?: number, selectEnd?: number) => {
      const el = ref.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(start, end);
      const ok = typeof document.execCommand === "function" && document.execCommand("insertText", false, text);
      if (!ok) {
        el.setRangeText(text, start, end, "end");
        onChange(el.value);
      }
      const from = selectStart ?? start + text.length;
      el.setSelectionRange(from, selectEnd ?? from);
    },
    [onChange]
  );

  /** 行内包裹：有选区就包住选区，再按一次去掉；没有选区就插入占位文字并选中它。 */
  const wrap = useCallback(
    (before: string, after: string, placeholderText: string) => {
      const el = ref.current;
      if (!el) return;
      const { selectionStart: s, selectionEnd: e, value: v } = el;
      const selected = v.slice(s, e);
      if (selected && s >= before.length && v.slice(s - before.length, s) === before && v.slice(e, e + after.length) === after) {
        replaceRange(s - before.length, e + after.length, selected, s - before.length, e - before.length);
        return;
      }
      const body = selected || placeholderText;
      replaceRange(s, e, before + body + after, s + before.length, s + before.length + body.length);
    },
    [replaceRange]
  );

  /** 行首前缀：作用于选区覆盖的每一行；全部已有该前缀时去掉。 */
  const prefixLines = useCallback(
    (prefix: string | ((index: number) => string), pattern: RegExp) => {
      const el = ref.current;
      if (!el) return;
      const { selectionStart: s, selectionEnd: e, value: v } = el;
      const lineStart = v.lastIndexOf("\n", s - 1) + 1;
      const nextBreak = v.indexOf("\n", e);
      const lineEnd = nextBreak === -1 ? v.length : nextBreak;
      const lines = v.slice(lineStart, lineEnd).split("\n");
      const allPrefixed = lines.every((line) => pattern.test(line));
      const next = lines
        .map((line, i) => {
          const bare = line.replace(pattern, "");
          if (allPrefixed) return bare;
          return (typeof prefix === "function" ? prefix(i) : prefix) + bare;
        })
        .join("\n");
      replaceRange(lineStart, lineEnd, next, lineStart, lineStart + next.length);
    },
    [replaceRange]
  );

  const heading = (level: 2 | 3) => prefixLines("#".repeat(level) + " ", /^#{1,6}\s+/);
  const bullet = () => prefixLines("- ", /^\s*[-*+]\s+(?!\[[ xX]\])/);
  const ordered = () => prefixLines((i) => `${i + 1}. `, /^\s*\d+[.)]\s+/);
  const task = () => prefixLines("- [ ] ", /^\s*[-*+]\s+\[[ xX]\]\s+/);
  const quote = () => prefixLines("> ", /^>\s?/);

  const link = () => {
    const el = ref.current;
    if (!el) return;
    const { selectionStart: s, selectionEnd: e, value: v } = el;
    const selected = v.slice(s, e);
    const isUrl = /^https?:\/\/\S+$/i.test(selected);
    const text = isUrl ? "链接文字" : selected || "链接文字";
    const url = isUrl ? selected : "https://";
    const inserted = `[${text}](${url})`;
    // 选中最需要改的那一段：选的是网址就选文字，否则选网址
    const focusStart = isUrl || !selected ? s + 1 : s + text.length + 3;
    const focusEnd = isUrl || !selected ? s + 1 + text.length : focusStart + url.length;
    replaceRange(s, e, inserted, focusStart, focusEnd);
  };

  const rule = () => {
    const el = ref.current;
    if (!el) return;
    const { selectionStart: s, selectionEnd: e, value: v } = el;
    const lead = s === 0 || v.slice(0, s).endsWith("\n\n") ? "" : v.slice(0, s).endsWith("\n") ? "\n" : "\n\n";
    replaceRange(s, e, `${lead}---\n\n`);
  };

  const color = (hex: string | null) => {
    setColorOpen(false);
    const el = ref.current;
    if (!el) return;
    const { selectionStart: s, selectionEnd: e, value: v } = el;
    const selected = v.slice(s, e);
    const existing = COLOR_SPAN.exec(selected);
    const body = existing ? existing[1] : selected;
    if (!hex) {
      if (existing) replaceRange(s, e, body, s, s + body.length);
      return;
    }
    const text = body || "文字";
    const open = `<span style="color:${hex}">`;
    replaceRange(s, e, `${open}${text}</span>`, s + open.length, s + open.length + text.length);
  };

  /** 列表里回车自动续上标记；空项回车结束列表。 */
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    const mod = event.metaKey || event.ctrlKey;
    if (mod && !event.shiftKey && !event.altKey) {
      const key = event.key.toLowerCase();
      if (key === "b") {
        event.preventDefault();
        wrap("**", "**", "加粗文字");
        return;
      }
      if (key === "i") {
        event.preventDefault();
        wrap("*", "*", "斜体文字");
        return;
      }
      if (key === "k") {
        event.preventDefault();
        link();
        return;
      }
    }
    if (event.key !== "Enter" || event.shiftKey || mod || event.nativeEvent.isComposing) return;
    const el = event.currentTarget;
    const { selectionStart: s, selectionEnd: e, value: v } = el;
    if (s !== e) return;
    const lineStart = v.lastIndexOf("\n", s - 1) + 1;
    const line = v.slice(lineStart, s);
    const match = /^(\s*)([-*+] \[[ xX]\] |[-*+] |(\d+)([.)]) |> )/.exec(line);
    if (!match) return;
    event.preventDefault();
    if (line.trim() === match[2].trim()) {
      // 空项：去掉标记，结束列表
      replaceRange(lineStart, s, "");
      return;
    }
    let marker = match[2];
    if (match[3]) marker = `${Number(match[3]) + 1}${match[4]} `;
    else if (/\[[xX]\]/.test(marker)) marker = marker.replace(/\[[xX]\]/, "[ ]");
    replaceRange(s, e, `\n${match[1]}${marker}`);
  };

  const count = [...value].length;
  const over = maxLength !== undefined && count > maxLength;

  const toolbar = (
    <div className="sticky top-0 z-10 flex shrink-0 flex-wrap items-center gap-0.5 border-b bg-background px-2 py-1">
      <ToolButton title="二级标题" onClick={() => heading(2)}>
        <Heading2 className="size-3.5" />
      </ToolButton>
      <ToolButton title="三级标题" onClick={() => heading(3)}>
        <Heading3 className="size-3.5" />
      </ToolButton>
      <Sep />
      <ToolButton title="粗体（Ctrl+B）" onClick={() => wrap("**", "**", "加粗文字")}>
        <Bold className="size-3.5" />
      </ToolButton>
      <ToolButton title="斜体（Ctrl+I）" onClick={() => wrap("*", "*", "斜体文字")}>
        <Italic className="size-3.5" />
      </ToolButton>
      <ToolButton title="删除线" onClick={() => wrap("~~", "~~", "删除的文字")}>
        <Strikethrough className="size-3.5" />
      </ToolButton>
      <ToolButton title="行内代码" onClick={() => wrap("`", "`", "代码")}>
        <Code className="size-3.5" />
      </ToolButton>
      <Popover open={colorOpen} onOpenChange={setColorOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            title="文字颜色"
            aria-label="文字颜色"
            onMouseDown={(e) => e.preventDefault()}
            className="flex size-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          >
            <Palette className="size-3.5" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-auto p-2" onOpenAutoFocus={(e) => e.preventDefault()} onCloseAutoFocus={(e) => e.preventDefault()}>
          <div className="grid grid-cols-4 gap-1.5">
            {MARKDOWN_TEXT_COLORS.map((c) => (
              <button
                key={c.value}
                type="button"
                title={`${c.label} ${c.value}`}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => color(c.value)}
                className="flex h-8 w-12 items-center justify-center rounded-md border text-xs font-medium transition-colors hover:bg-accent"
                style={{ color: c.value }}
              >
                {c.label}
              </button>
            ))}
          </div>
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => color(null)}
            className="mt-1.5 h-7 w-full rounded-md text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          >
            清除颜色
          </button>
        </PopoverContent>
      </Popover>
      <Sep />
      <ToolButton title="无序列表" onClick={bullet}>
        <List className="size-3.5" />
      </ToolButton>
      <ToolButton title="有序列表" onClick={ordered}>
        <ListOrdered className="size-3.5" />
      </ToolButton>
      <ToolButton title="任务列表" onClick={task}>
        <ListChecks className="size-3.5" />
      </ToolButton>
      <ToolButton title="引用" onClick={quote}>
        <Quote className="size-3.5" />
      </ToolButton>
      <ToolButton title="链接（Ctrl+K）" onClick={link}>
        <LinkIcon className="size-3.5" />
      </ToolButton>
      <ToolButton title="分割线" onClick={rule}>
        <Minus className="size-3.5" />
      </ToolButton>
      <div className="ml-auto flex items-center gap-1">
        {toolbarEnd}
        {preview ? (
          <ToggleGroup type="single" size="sm" value={mode} onValueChange={(v) => v && setMode(v as MarkdownViewMode)}>
            <ToggleGroupItem value="edit" className="h-7 px-2" title="仅编辑" aria-label="仅编辑">
              <PencilLine className="size-3.5" />
            </ToggleGroupItem>
            <ToggleGroupItem value="split" className="h-7 px-2" title="编辑与预览" aria-label="编辑与预览">
              <Columns2 className="size-3.5" />
            </ToggleGroupItem>
            <ToggleGroupItem value="preview" className="h-7 px-2" title="仅预览" aria-label="仅预览">
              <Eye className="size-3.5" />
            </ToggleGroupItem>
          </ToggleGroup>
        ) : null}
        {fullscreenable ? (
          <ToolButton title={fullscreen ? "退出全屏" : "全屏编辑"} onClick={() => setFullscreen((v) => !v)}>
            {fullscreen ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
          </ToolButton>
        ) : null}
      </div>
    </div>
  );

  const showEditor = mode !== "preview";
  const showPreview = Boolean(preview) && mode !== "edit";

  const body = (
    <div className={cn("grid min-h-0 flex-1", showEditor && showPreview && "md:grid-cols-2")}>
      {showEditor ? (
        <textarea
          ref={ref}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder={placeholder}
          spellCheck={false}
          aria-invalid={over || undefined}
          className={cn(
            "w-full resize-none bg-transparent px-4 py-3 font-mono text-[13px] leading-6 outline-none placeholder:text-muted-foreground",
            fullscreen ? "h-full min-h-0" : "field-sizing-content min-h-56 max-h-[60vh]"
          )}
        />
      ) : null}
      {showPreview ? (
        <div
          className={cn(
            "min-w-0 overflow-y-auto px-4 py-3",
            showEditor && "border-t md:border-t-0 md:border-l",
            fullscreen ? "h-full" : "min-h-56 max-h-[60vh]"
          )}
        >
          {preview}
        </div>
      ) : null}
    </div>
  );

  const footer = (
    <div className="flex shrink-0 items-center justify-between gap-3 border-t px-3 py-1.5 text-[11px] text-muted-foreground">
      <span>支持 Markdown：标题、列表、引用、表格、链接与文字颜色；段落内换行即换行</span>
      {maxLength !== undefined ? (
        <span className={cn("tabular-nums", over && "font-medium text-destructive")}>
          {count} / {maxLength}
        </span>
      ) : null}
    </div>
  );

  if (fullscreen) {
    return (
      <div className="fixed inset-0 z-50 flex flex-col bg-background">
        {toolbar}
        {body}
        {footer}
      </div>
    );
  }

  return (
    <div className={cn("flex flex-col overflow-hidden rounded-lg border bg-background", className)}>
      {toolbar}
      {body}
      {footer}
    </div>
  );
}
