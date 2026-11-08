"use client";

import { useState } from "react";
import { Check, Copy, TriangleAlert } from "lucide-react";
import { toast } from "sonner";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { appConfig } from "@/lib/env";
import type { CreatedMcpKey } from "@/lib/api/mcp-keys";

/** MCP 服务端的安装来源：仓库里的 mcp/ 子目录（Python，uvx 直接拉起） */
const MCP_PACKAGE = "git+https://github.com/MiChongs/aegis#subdirectory=mcp";

/**
 * MCP 服务端要连的地址。控制台把 /api 与 /openapi.json 反代到后端，
 * 所以没配直连地址时用控制台自己的源即可。
 */
function aegisBaseUrl() {
  if (appConfig.apiBaseUrl) return appConfig.apiBaseUrl;
  return typeof window === "undefined" ? "" : window.location.origin;
}

function snippets(secret: string) {
  const baseUrl = aegisBaseUrl();
  const json = JSON.stringify(
    {
      mcpServers: {
        aegis: {
          command: "uvx",
          args: ["--from", MCP_PACKAGE, "aegis-mcp"],
          env: { AEGIS_BASE_URL: baseUrl, AEGIS_MCP_KEY: secret }
        }
      }
    },
    null,
    2
  );
  const claudeCode = [
    "claude mcp add aegis",
    `  -e AEGIS_BASE_URL=${baseUrl}`,
    `  -e AEGIS_MCP_KEY=${secret}`,
    `  -- uvx --from "${MCP_PACKAGE}" aegis-mcp`
  ].join(" \\\n");
  const env = [`AEGIS_BASE_URL=${baseUrl}`, `AEGIS_MCP_KEY=${secret}`].join("\n");
  return { json, claudeCode, env };
}

function CopyBlock({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="relative">
      <pre className="max-h-64 overflow-auto rounded-lg border bg-muted/40 p-3 pr-12 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all">
        {value}
      </pre>
      <Button
        type="button"
        size="icon"
        variant="ghost"
        className="absolute top-1.5 right-1.5 size-7"
        aria-label={`复制${label}`}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
          } catch {
            toast.error("复制失败，请手动选中复制");
          }
        }}
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
      </Button>
    </div>
  );
}

/**
 * 明文只出现这一次：弹窗里给明文本身，以及照着填就能用的客户端配置。
 * 关掉之前不提供「稍后再看」—— 服务端只存摘要，关掉就再也取不回来。
 */
export function McpKeySecretDialog({
  created,
  rotated,
  onClose
}: {
  created: CreatedMcpKey | null;
  rotated?: boolean;
  onClose: () => void;
}) {
  const configs = created ? snippets(created.secret) : null;

  return (
    <Dialog open={Boolean(created)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{rotated ? "密钥已轮换" : "MCP 密钥已签发"}</DialogTitle>
          <DialogDescription>
            {created ? `「${created.name}」` : ""}
            {rotated ? "的旧明文已立即失效。" : ""}明文只显示这一次，请立即保存到 MCP 客户端配置中。
          </DialogDescription>
        </DialogHeader>

        {created && configs ? (
          <div className="space-y-4">
            <CopyBlock value={created.secret} label="密钥" />

            <Tabs defaultValue="json" className="gap-3">
              <TabsList>
                <TabsTrigger value="json" className="text-xs">JSON 配置</TabsTrigger>
                <TabsTrigger value="claude-code" className="text-xs">Claude Code</TabsTrigger>
                <TabsTrigger value="env" className="text-xs">环境变量</TabsTrigger>
              </TabsList>
              <TabsContent value="json" className="space-y-2">
                <p className="text-xs text-muted-foreground">
                  适用于 Claude Desktop、Cursor、Cherry Studio 等读取 mcpServers 配置的客户端。需要本机已安装 uv。
                </p>
                <CopyBlock value={configs.json} label="JSON 配置" />
              </TabsContent>
              <TabsContent value="claude-code" className="space-y-2">
                <p className="text-xs text-muted-foreground">在终端执行，添加到当前项目；加 --scope user 则对所有项目生效。</p>
                <CopyBlock value={configs.claudeCode} label="命令" />
              </TabsContent>
              <TabsContent value="env" className="space-y-2">
                <p className="text-xs text-muted-foreground">
                  自行部署 aegis-mcp（pip install，或 HTTP 模式 aegis-mcp --transport http）时使用。
                </p>
                <CopyBlock value={configs.env} label="环境变量" />
              </TabsContent>
            </Tabs>

            <Alert>
              <TriangleAlert />
              <AlertTitle>妥善保管</AlertTitle>
              <AlertDescription>
                持有明文即可在密钥范围内以你的身份操作，所有调用都会记入审计日志（会话列显示 mcp:{created.keyPrefix}）。
                泄露后请立即轮换或撤销。
              </AlertDescription>
            </Alert>
          </div>
        ) : null}

        <DialogFooter>
          <Button onClick={onClose}>我已保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
