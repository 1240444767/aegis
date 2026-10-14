"use client";

import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { previewAdminReleaseNotes } from "@/lib/api/releases";
import type { ReleaseNotesFormat } from "@/lib/api/types";
import { useAuthStore } from "@/lib/auth-store";
import { markdownFromHtml } from "@/lib/markdown-from-html";
import { MarkdownEditor } from "@/components/ui/markdown-editor";
import { RichContent, RichEditor } from "@/components/ui/rich-editor";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

/**
 * 更新说明编辑：Markdown（默认）或富文本。
 *
 * Markdown 的预览不在浏览器里渲染，而是交给服务端的预览接口：它与保存走同一条渲染与净化，
 * 预览里看到的就是客户端拿到的 HTML —— 哪个标签、哪种颜色写法会被白名单拿掉，作者当场就能看到。
 */

export const RELEASE_NOTES_MAX = 20000;

type Props = {
  appKey: string;
  format: ReleaseNotesFormat;
  markdown: string;
  html: string;
  onFormatChange: (format: ReleaseNotesFormat) => void;
  onMarkdownChange: (markdown: string) => void;
  onHtmlChange: (html: string) => void;
};

export function ReleaseNotesEditor({ appKey, format, markdown, html, onFormatChange, onMarkdownChange, onHtmlChange }: Props) {
  const switchFormat = (next: string) => {
    if (next !== "markdown" && next !== "html") return;
    if (next === format) return;
    // 富文本切到 Markdown 且还没写过原文：从现有正文转一份起稿，不让作者从头再写
    if (next === "markdown" && !markdown.trim() && html.trim()) {
      onMarkdownChange(markdownFromHtml(html));
    }
    onFormatChange(next);
  };

  const formatSwitch = (
    <ToggleGroup type="single" size="sm" variant="outline" value={format} onValueChange={switchFormat}>
      <ToggleGroupItem value="markdown" className="h-7 px-2.5 text-xs">
        Markdown
      </ToggleGroupItem>
      <ToggleGroupItem value="html" className="h-7 px-2.5 text-xs">
        富文本
      </ToggleGroupItem>
    </ToggleGroup>
  );

  if (format === "html") {
    return (
      <div className="space-y-2">
        <div className="flex justify-end">{formatSwitch}</div>
        <RichEditor value={html} onChange={onHtmlChange} placeholder="这次更新了什么？" fullscreenable />
      </div>
    );
  }

  return (
    <MarkdownEditor
      value={markdown}
      onChange={onMarkdownChange}
      placeholder={"## 新增\n- 新增……\n\n## 修复\n- 修复……"}
      maxLength={RELEASE_NOTES_MAX}
      toolbarEnd={formatSwitch}
      preview={<NotesPreview appKey={appKey} markdown={markdown} />}
    />
  );
}

function NotesPreview({ appKey, markdown }: { appKey: string; markdown: string }) {
  const token = useAuthStore((s) => s.accessToken);
  const [state, setState] = useState<{ source: string; html: string; error: string | null } | null>(null);

  useEffect(() => {
    if (!token || !markdown.trim()) return;
    const controller = new AbortController();
    // 停笔 300 ms 再请求，连续输入时只发最后一次
    const timer = window.setTimeout(() => {
      previewAdminReleaseNotes(token, appKey, { format: "markdown", markdown }, controller.signal)
        .then((result) => setState({ source: markdown, html: result.html, error: null }))
        .catch((error: unknown) => {
          if (controller.signal.aborted) return;
          setState((prev) => ({
            source: markdown,
            html: prev?.html ?? "",
            error: error instanceof Error ? error.message : "预览失败"
          }));
        });
    }, 300);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [appKey, markdown, token]);

  if (!markdown.trim()) {
    return <p className="text-sm text-muted-foreground">在左侧输入后，这里显示用户看到的效果</p>;
  }
  const stale = state?.source !== markdown;
  return (
    <div className="relative">
      {stale ? <Loader2 className="absolute right-0 top-0 size-3.5 animate-spin text-muted-foreground" aria-label="正在预览" /> : null}
      {state?.error ? <p className="mb-2 text-xs text-destructive">{state.error}</p> : null}
      {state?.html ? <RichContent html={state.html} /> : stale ? null : <p className="text-sm text-muted-foreground">没有可显示的内容</p>}
    </div>
  );
}
