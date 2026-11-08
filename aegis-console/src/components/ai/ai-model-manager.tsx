"use client";

import { useMemo, useState } from "react";
import { CloudDownload, Eraser, Loader2, Plus, Search, Sparkles, Star, X } from "lucide-react";
import type { AIModelInfo } from "@/lib/api/ai";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/**
 * 型号管理器：接管通道配置里的 `models`（每行一个）与 `defaultModel` 两个字段。
 *
 * 存储形状不变（仍是换行分隔的字符串 + 单独的默认型号），只是编辑方式从一个大文本框
 * 换成了芯片：星标设默认、× 移除、粘贴一段清单自动拆分；「一键拉取」向供应商要清单，
 * 在下方面板里搜索、勾选后加入或替换。默认型号不在清单里时会被一并加进去 ——
 * 后端「留空取第一行」的约定由排序保证：默认型号总排在第一个。
 */

export function parseModels(raw?: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const piece of (raw ?? "").split(/[\n,\r]/)) {
    const model = piece.trim();
    if (model && !seen.has(model)) {
      seen.add(model);
      out.push(model);
    }
  }
  return out;
}

export function AIModelManager({
  label,
  models,
  defaultModel,
  suggested,
  fetching,
  fetchError,
  fetched,
  onFetch,
  onChange,
  onCloseFetched
}: {
  label: string;
  models: string[];
  defaultModel: string;
  suggested: string[];
  fetching: boolean;
  fetchError?: string | null;
  /** 拉取结果；null 表示面板未打开。 */
  fetched: AIModelInfo[] | null;
  onFetch: () => void;
  onChange: (models: string[], defaultModel: string) => void;
  onCloseFetched: () => void;
}) {
  const [input, setInput] = useState("");
  const effectiveDefault = defaultModel || models[0] || "";
  const missingSuggested = suggested.filter((model) => !models.includes(model));

  const commit = (next: string[], nextDefault = defaultModel) => {
    const unique = parseModels(next.join("\n"));
    // 默认型号被移除时回落到第一个；仍在清单里就排到最前（后端「留空取第一行」与之一致）
    const chosen = nextDefault && unique.includes(nextDefault) ? nextDefault : "";
    const ordered = chosen ? [chosen, ...unique.filter((model) => model !== chosen)] : unique;
    onChange(ordered, chosen);
  };

  const add = (text: string) => {
    const added = parseModels(text);
    if (added.length === 0) return;
    commit([...models, ...added]);
    setInput("");
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">{label}</span>
        <Badge variant="secondary" size="sm">
          {models.length}
        </Badge>
        <div className="ml-auto flex flex-wrap gap-1.5">
          <Button type="button" size="sm" variant="default" onClick={onFetch} disabled={fetching}>
            {fetching ? <Loader2 className="size-3.5 animate-spin" /> : <CloudDownload className="size-3.5" />}
            一键拉取
          </Button>
          {missingSuggested.length > 0 ? (
            <Button type="button" size="sm" variant="outline" onClick={() => commit([...models, ...missingSuggested])}>
              <Sparkles className="size-3.5" />
              建议型号
            </Button>
          ) : null}
          {models.length > 0 ? (
            <Button type="button" size="sm" variant="ghost" onClick={() => onChange([], "")}>
              <Eraser className="size-3.5" />
              清空
            </Button>
          ) : null}
        </div>
      </div>

      {fetchError ? (
        <p className="whitespace-pre-wrap break-all rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2 text-xs text-destructive">
          {fetchError}
        </p>
      ) : null}

      {fetched ? (
        <FetchedModelsPanel
          fetched={fetched}
          current={models}
          onApply={(selected, mode) => {
            if (mode === "replace") {
              commit(selected, selected.includes(effectiveDefault) ? effectiveDefault : "");
            } else {
              commit([...models, ...selected]);
            }
            onCloseFetched();
          }}
          onClose={onCloseFetched}
        />
      ) : null}

      <div className="rounded-xl border bg-muted/20 p-2.5">
        {models.length === 0 ? (
          <p className="px-1 py-3 text-center text-xs text-muted-foreground">暂无型号</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {models.map((model) => {
              const isDefault = model === effectiveDefault;
              return (
                <span
                  key={model}
                  className={cn(
                    "group inline-flex items-center gap-1 rounded-lg border bg-background py-0.5 pr-1 pl-1.5 font-mono text-xs transition-colors",
                    isDefault && "border-amber-400/60 bg-amber-50 dark:border-amber-500/40 dark:bg-amber-500/10"
                  )}
                >
                  <button
                    type="button"
                    aria-label={isDefault ? "默认型号" : "设为默认"}
                    title={isDefault ? "默认型号" : "设为默认"}
                    onClick={() => commit(models, model)}
                    className="text-muted-foreground transition-colors hover:text-amber-500"
                  >
                    <Star className={cn("size-3", isDefault && "fill-amber-400 text-amber-500")} />
                  </button>
                  <span className="max-w-64 truncate">{model}</span>
                  <button
                    type="button"
                    aria-label={`移除 ${model}`}
                    onClick={() => commit(models.filter((item) => item !== model), isDefault ? "" : defaultModel)}
                    className="rounded text-muted-foreground opacity-60 transition hover:text-destructive group-hover:opacity-100"
                  >
                    <X className="size-3" />
                  </button>
                </span>
              );
            })}
          </div>
        )}
        <div className="mt-2 flex gap-1.5">
          <Input
            className="h-8 font-mono text-xs"
            placeholder="输入型号，回车添加；可粘贴多行"
            value={input}
            onChange={(event) => setInput(event.target.value)}
            onPaste={(event) => {
              const text = event.clipboardData.getData("text");
              if (/[\n,]/.test(text)) {
                event.preventDefault();
                add(text);
              }
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                add(input);
              }
            }}
          />
          <Button type="button" size="sm" variant="outline" className="h-8" onClick={() => add(input)} disabled={!input.trim()}>
            <Plus className="size-3.5" />
            添加
          </Button>
        </div>
      </div>
    </div>
  );
}

function formatContext(value?: number) {
  if (!value) return "";
  if (value >= 1_000_000) return `${Math.round(value / 100_000) / 10}M`;
  if (value >= 1000) return `${Math.round(value / 1000)}K`;
  return String(value);
}

/** 拉取结果面板：搜索、勾选、加入或替换。已在清单里的默认勾上并标出。 */
function FetchedModelsPanel({
  fetched,
  current,
  onApply,
  onClose
}: {
  fetched: AIModelInfo[];
  current: string[];
  onApply: (selected: string[], mode: "append" | "replace") => void;
  onClose: () => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [selected, setSelected] = useState<Set<string>>(
    () => new Set(fetched.filter((model) => current.includes(model.id)).map((model) => model.id))
  );
  const visible = useMemo(() => {
    const needle = keyword.trim().toLowerCase();
    if (!needle) return fetched;
    return fetched.filter(
      (model) => model.id.toLowerCase().includes(needle) || (model.name ?? "").toLowerCase().includes(needle)
    );
  }, [fetched, keyword]);
  const allVisibleSelected = visible.length > 0 && visible.every((model) => selected.has(model.id));
  const newlySelected = [...selected].filter((id) => !current.includes(id));

  const toggle = (id: string, on: boolean) => {
    const next = new Set(selected);
    if (on) next.add(id);
    else next.delete(id);
    setSelected(next);
  };

  return (
    <div className="overflow-hidden rounded-xl border border-primary/30 bg-card shadow-sm">
      <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
        <CloudDownload className="size-4 text-primary" />
        <span className="text-sm font-medium">拉取到 {fetched.length} 个型号</span>
        <span className="text-xs text-muted-foreground">已选 {selected.size}</span>
        <button type="button" onClick={onClose} aria-label="关闭" className="ml-auto text-muted-foreground hover:text-foreground">
          <X className="size-4" />
        </button>
      </div>
      {fetched.length === 0 ? (
        <p className="px-3 py-6 text-center text-xs text-muted-foreground">供应商没有返回任何型号</p>
      ) : (
        <>
          <div className="flex items-center gap-2 px-3 py-2">
            <div className="relative flex-1">
              <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="h-8 pl-8 text-xs"
                placeholder="搜索型号"
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
              />
            </div>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-8"
              onClick={() => {
                const next = new Set(selected);
                for (const model of visible) {
                  if (allVisibleSelected) next.delete(model.id);
                  else next.add(model.id);
                }
                setSelected(next);
              }}
            >
              {allVisibleSelected ? "取消全选" : keyword ? `全选结果（${visible.length}）` : "全选"}
            </Button>
          </div>
          <div className="max-h-72 overflow-y-auto border-y">
            {visible.length === 0 ? (
              <p className="px-3 py-6 text-center text-xs text-muted-foreground">没有匹配的型号</p>
            ) : (
              visible.map((model) => {
                const checked = selected.has(model.id);
                const added = current.includes(model.id);
                return (
                  <label
                    key={model.id}
                    className={cn(
                      "flex cursor-pointer items-center gap-2.5 px-3 py-1.5 transition-colors hover:bg-muted/50",
                      checked && "bg-primary/5"
                    )}
                  >
                    <Checkbox checked={checked} onCheckedChange={(value) => toggle(model.id, value === true)} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-mono text-xs">{model.id}</span>
                      {model.name && model.name !== model.id ? (
                        <span className="block truncate text-[11px] text-muted-foreground">{model.name}</span>
                      ) : null}
                    </span>
                    {model.contextWindow ? (
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{formatContext(model.contextWindow)}</span>
                    ) : null}
                    {model.ownedBy ? (
                      <span className="hidden shrink-0 text-[10px] text-muted-foreground sm:inline">{model.ownedBy}</span>
                    ) : null}
                    {added ? (
                      <Badge variant="outline" size="sm">
                        已添加
                      </Badge>
                    ) : null}
                  </label>
                );
              })
            )}
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2 px-3 py-2">
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={selected.size === 0}
              onClick={() => onApply(fetched.filter((model) => selected.has(model.id)).map((model) => model.id), "replace")}
            >
              只保留选中（{selected.size}）
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={newlySelected.length === 0}
              onClick={() => onApply(newlySelected, "append")}
            >
              <Plus className="size-3.5" />
              加入清单（{newlySelected.length}）
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
