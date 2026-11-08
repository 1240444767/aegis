"use client";

import { useEffect, useId, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { useTheme } from "next-themes";
import type { Monaco } from "@monaco-editor/react";
import type { editor, IDisposable, Position } from "monaco-editor";
import { Loader2 } from "lucide-react";
import type { EmailTemplateProblem, EmailTemplateReference } from "@/lib/api/email-policy";
import { useFunctionWorkbenchStore } from "@/lib/function-workbench-store";
import { AEGIS_THEME, applyAegisTheme } from "@/lib/monaco/aegis-theme";
import { useEditorMotion } from "@/lib/monaco/editor-motion";
import { loadMonaco } from "@/lib/monaco/loader";
import { cn } from "@/lib/utils";

const MonacoEditor = dynamic(() => import("@monaco-editor/react").then((mod) => mod.Editor), {
  ssr: false,
  loading: () => (
    <div className="flex h-full min-h-32 items-center justify-center rounded-lg border bg-muted/20">
      <Loader2 className="size-4 animate-spin text-muted-foreground" />
    </div>
  )
});

/**
 * 邮件模板代码编辑器（MJML / HTML + Liquid）。
 *
 * 在 Monaco 的 HTML 语言上叠一层 Liquid 补全：输入 `{{` 补变量与过滤器，`{%` 补组件与控制标签。
 * 补全条目来自后端下发的模板语言参考（mailtpl.TemplateReference），文档与实现不会漂移。
 * 服务端预览返回的问题按行号画成波浪线；没有行号的问题由调用方在编辑器外列出。
 *
 * 补全提供器是 Monaco 全局注册的（按语言），这里用模块级引用计数只注册一次，
 * 参考目录换成最近一个挂载的编辑器给的那份。
 */
let completionRegistration: IDisposable | null = null;
let completionUsers = 0;
let completionReference: EmailTemplateReference | null = null;

// 已经敲了 `{%` 之后插入的内容（不含开头的 `{%`）
const LIQUID_TAGS = [
  { label: "if", insert: " if ${1:condition} %}\n\t$0\n{% endif %}", doc: "条件" },
  { label: "unless", insert: " unless ${1:condition} %}\n\t$0\n{% endunless %}", doc: "条件取反" },
  { label: "for", insert: " for ${1:item} in ${2:items} %}\n\t$0\n{% endfor %}", doc: "循环" },
  { label: "case", insert: ' case ${1:value} %}\n{% when ${2:"a"} %}\n\t$0\n{% endcase %}', doc: "分支" },
  { label: "assign", insert: " assign ${1:name} = ${2:value} %}", doc: "赋值" },
  { label: "capture", insert: " capture ${1:name} %}$0{% endcapture %}", doc: "捕获一段输出" },
  { label: "comment", insert: " comment %}$0{% endcomment %}", doc: "注释" }
];

/** 组件参考里的完整写法 → 敲了 `{%` 之后要插入的部分 */
function componentInsert(syntax: string) {
  const block = syntax.match(/^\{%\s*(\w+)\s*%\}[\s\S]*\{%\s*end\1\s*%\}$/);
  if (block) return ` ${block[1]} %}\n$0\n{% end${block[1]} %}`;
  return " " + syntax.replace(/^\{%-?\s*/, "").replace(/\s*-?%\}$/, "") + " %}";
}

function registerLiquidCompletion(monaco: Monaco) {
  if (completionRegistration) return;
  completionRegistration = monaco.languages.registerCompletionItemProvider("html", {
    triggerCharacters: ["{", "%", "|", " ", "."],
    provideCompletionItems(model: editor.ITextModel, position: Position) {
      const reference = completionReference;
      if (!reference) return { suggestions: [] };
      const line = model.getLineContent(position.lineNumber).slice(0, position.column - 1);
      const word = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn
      };
      const openObject = line.lastIndexOf("{{");
      const openTag = line.lastIndexOf("{%");
      const inObject = openObject > line.lastIndexOf("}}");
      const inTag = openTag > line.lastIndexOf("%}");
      const kinds = monaco.languages.CompletionItemKind;

      if (inObject && /\|\s*[\w]*$/.test(line)) {
        return {
          suggestions: reference.filters.flatMap((item) =>
            item.name.split(" / ").map((name) => ({
              label: name,
              kind: kinds.Function,
              detail: item.description,
              documentation: item.syntax,
              insertText: name,
              range
            }))
          )
        };
      }
      if (inObject) {
        return {
          suggestions: [...reference.globals, ...reference.theme]
            .filter((item) => !item.syntax.startsWith("{%"))
            .map((item) => ({
              label: item.name,
              kind: kinds.Variable,
              detail: item.description,
              documentation: item.syntax,
              insertText: item.name,
              range: { ...range, startColumn: Math.max(1, position.column - (line.match(/[\w.]*$/)?.[0].length ?? 0)) }
            }))
        };
      }
      if (inTag) {
        // 只在紧跟 `{%` 时给出整段写法，避免在参数中间弹出
        if (!/\{%-?\s*\w*$/.test(line)) return { suggestions: [] };
        const start = line.lastIndexOf("{%") + 3;
        const tagRange = { ...range, startColumn: start };
        const snippet = monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet;
        const components = reference.components.map((item) => ({
          label: item.name,
          kind: kinds.Snippet,
          detail: item.description,
          documentation: item.syntax,
          insertText: componentInsert(item.syntax),
          insertTextRules: snippet,
          range: tagRange
        }));
        const tags = LIQUID_TAGS.map((tag) => ({
          label: tag.label,
          kind: kinds.Keyword,
          detail: tag.doc,
          insertText: tag.insert,
          insertTextRules: snippet,
          range: tagRange
        }));
        return { suggestions: [...components, ...tags] };
      }
      return { suggestions: [] };
    }
  });
}

export function EmailCodeEditor({
  value,
  onChange,
  reference,
  problems,
  height = 360,
  readOnly = false,
  className,
  onReady
}: {
  value: string;
  onChange: (value: string) => void;
  reference?: EmailTemplateReference;
  /** 只取带行号的问题画标记 */
  problems?: EmailTemplateProblem[];
  height?: number | string;
  readOnly?: boolean;
  className?: string;
  /** 挂载后把编辑器交给调用方（插入片段用） */
  onReady?: (instance: editor.IStandaloneCodeEditor) => void;
}) {
  const { resolvedTheme } = useTheme();
  const instanceId = useId().replace(/[^a-zA-Z0-9]/g, "");
  const monacoRef = useRef<Monaco | null>(null);
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const dark = resolvedTheme === "dark";
  const smoothAnimation = useFunctionWorkbenchStore((state) => state.editor.smoothAnimation);
  const motion = useEditorMotion(smoothAnimation);
  const modelPath = `aegis-mail-${instanceId}.html`;

  useEffect(() => {
    let alive = true;
    loadMonaco().then(
      () => alive && setReady(true),
      () => alive && setFailed(true)
    );
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (reference) completionReference = reference;
  }, [reference]);

  useEffect(() => {
    completionUsers += 1;
    return () => {
      completionUsers -= 1;
      if (completionUsers === 0 && completionRegistration) {
        completionRegistration.dispose();
        completionRegistration = null;
      }
    };
  }, []);

  useEffect(() => {
    if (!ready) return;
    const frame = requestAnimationFrame(() => {
      if (monacoRef.current) applyAegisTheme(monacoRef.current, dark);
    });
    return () => cancelAnimationFrame(frame);
  }, [dark, ready]);

  // 服务端问题 → 行内标记
  useEffect(() => {
    const monaco = monacoRef.current;
    const model = editorRef.current?.getModel();
    if (!monaco || !model) return;
    const markers = (problems ?? [])
      .filter((problem) => problem.line && problem.line > 0 && problem.line <= model.getLineCount())
      .map((problem) => ({
        severity: monaco.MarkerSeverity.Error,
        message: problem.message,
        startLineNumber: problem.line as number,
        endLineNumber: problem.line as number,
        startColumn: 1,
        endColumn: model.getLineMaxColumn(problem.line as number)
      }));
    monaco.editor.setModelMarkers(model, "aegis-mail", markers);
  }, [problems, ready]);

  if (failed) {
    return (
      <div className={cn("flex items-center justify-center rounded-lg border bg-muted/20 p-4 text-xs text-muted-foreground", className)} style={{ height }}>
        编辑器加载失败
      </div>
    );
  }
  if (!ready) {
    return (
      <div className={cn("flex items-center justify-center rounded-lg border bg-muted/20", className)} style={{ height }}>
        <Loader2 className="size-4 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className={cn("aegis-code-editor overflow-hidden rounded-lg border", className)}>
      <MonacoEditor
        height={height}
        defaultLanguage="html"
        path={modelPath}
        value={value}
        theme={AEGIS_THEME}
        beforeMount={(monaco) => {
          monacoRef.current = monaco;
          applyAegisTheme(monaco, dark);
          if (reference) completionReference = reference;
          registerLiquidCompletion(monaco);
        }}
        onMount={(instance) => {
          editorRef.current = instance;
          onReady?.(instance);
        }}
        onChange={(next) => onChange(next ?? "")}
        options={{
          ...motion,
          readOnly,
          fontSize: 12,
          fontFamily: "var(--font-mono), Consolas, monospace",
          fontLigatures: true,
          lineHeight: 20,
          minimap: { enabled: false },
          lineNumbers: "on",
          lineNumbersMinChars: 3,
          folding: true,
          wordWrap: "on",
          scrollBeyondLastLine: false,
          automaticLayout: true,
          tabSize: 2,
          padding: { top: 10, bottom: 10 },
          renderLineHighlight: "line",
          overviewRulerLanes: 0,
          overviewRulerBorder: false,
          scrollbar: { alwaysConsumeMouseWheel: false, useShadows: false, verticalScrollbarSize: 8, horizontalScrollbarSize: 8 },
          quickSuggestions: { other: true, strings: true, comments: false },
          suggestOnTriggerCharacters: true
        }}
      />
    </div>
  );
}

/** 在光标处插入一段文本（组件、变量），并把焦点还给编辑器 */
export function insertAtCursor(instance: editor.IStandaloneCodeEditor | null, text: string) {
  if (!instance) return false;
  const selection = instance.getSelection();
  if (!selection) return false;
  instance.executeEdits("aegis-mail-insert", [{ range: selection, text, forceMoveMarkers: true }]);
  instance.focus();
  return true;
}
