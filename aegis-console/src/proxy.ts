import { NextResponse } from "next/server";

/**
 * 拦下伪造的 Server Action 请求。
 *
 * 控制台没有任何 Server Action，带 `Next-Action` 请求头的 POST 只可能来自扫描器
 * （React2Shell / CVE-2025-55182 的探测脚本固定发 `Next-Action: x`）。放进 Next.js 的话，
 * 它会去解析这个 ID、格式不符时抛错并打一条 ERROR 日志，每次探测刷屏一次。
 * 这里直接回 404，不进入 Server Action 处理流程。
 *
 * matcher 用 `has` 只匹配带这个请求头的请求：正常页面与 /api 反代完全不经过 proxy，
 * 大文件上传的请求体也不会被缓冲。
 */
export function proxy() {
  return new NextResponse(null, { status: 404 });
}

export const config = {
  matcher: [
    {
      source: "/((?!_next/|api/).*)",
      has: [{ type: "header", key: "next-action" }]
    }
  ]
};
