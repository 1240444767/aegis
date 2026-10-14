import { ApiError, joinApiUrl } from "@/lib/api/client";
import { completeAdminReleaseAsset, presignAdminReleaseAsset } from "@/lib/api/releases";
import type { ReleaseAssetUploadResult } from "@/lib/api/types";

/**
 * 安装包上传：优先浏览器直传对象存储，不行再经服务器。
 *
 *   1. presign：服务器做治理与配置校验，签一个短时 PUT 地址（不碰文件内容）
 *   2. 浏览器 PUT 到存储，进度来自 XHR（fetch 拿不到上传进度）
 *   3. complete：服务器 HEAD 一次确认大小，写文件索引，返回 storage:// 引用
 *
 * 存储不支持直传（本地磁盘、网盘类）时 presign 返回 mode=server；
 * 存储桶没放行控制台域名的跨域 PUT 时浏览器会拦下请求（状态码 0）。
 * 两种情况都自动改走 multipart 经服务器上传，并把原因告诉调用方。
 */

/** 经服务器上传的上限：与 next.config.ts 的 proxyClientMaxBodySize 一致，超出会被同源代理截断 */
export const SERVER_UPLOAD_MAX_BYTES = 1024 * 1024 * 1024;
/** 直传上限：与后端 releaseAssetMaxSize 一致 */
export const DIRECT_UPLOAD_MAX_BYTES = 2 * 1024 * 1024 * 1024;

export type UploadOutcome = {
  result: ReleaseAssetUploadResult;
  mode: "direct" | "server";
  /** 改走服务器上传的原因（直传时为空） */
  fallbackReason?: string;
};

type Options = {
  token: string;
  appKey: string;
  sha256: string;
  contentType?: string;
  onProgress?: (fraction: number) => void;
  signal?: AbortSignal;
};

class UploadAborted extends Error {
  constructor() {
    super("已取消上传");
    this.name = "AbortError";
  }
}

function xhrSend(
  method: string,
  url: string,
  body: Blob | FormData,
  headers: Record<string, string>,
  onProgress?: (fraction: number) => void,
  signal?: AbortSignal
): Promise<{ status: number; text: string }> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new UploadAborted());
      return;
    }
    const xhr = new XMLHttpRequest();
    xhr.open(method, url);
    for (const [key, value] of Object.entries(headers)) xhr.setRequestHeader(key, value);
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress?.(event.loaded / event.total);
    };
    xhr.onload = () => resolve({ status: xhr.status, text: xhr.responseText });
    xhr.onerror = () => resolve({ status: 0, text: "" });
    xhr.onabort = () => reject(new UploadAborted());
    signal?.addEventListener("abort", () => xhr.abort(), { once: true });
    xhr.send(body);
  });
}

async function uploadViaServer(file: File, options: Options): Promise<ReleaseAssetUploadResult> {
  if (file.size > SERVER_UPLOAD_MAX_BYTES) {
    throw new ApiError("文件超过 1 GB，经服务器上传会被代理截断；请为应用配置支持直传的对象存储");
  }
  const form = new FormData();
  form.append("file", file);
  const url = joinApiUrl(`/api/admin/apps/${encodeURIComponent(options.appKey)}/releases/assets`);
  const { status, text } = await xhrSend(
    "POST",
    url,
    form,
    { Authorization: `Bearer ${options.token}`, "X-Admin-Token": options.token, Accept: "application/json" },
    options.onProgress,
    options.signal
  );
  let payload: { code?: number; message?: string; data?: ReleaseAssetUploadResult } = {};
  try {
    payload = JSON.parse(text);
  } catch {
    // 非 JSON（代理错误页等）
  }
  if (status < 200 || status >= 300 || !payload.data) {
    throw new ApiError(payload.message || (status === 0 ? "网络中断，上传失败" : `上传失败（HTTP ${status}）`), { code: payload.code, status });
  }
  // 服务器边传边算的 SHA-256 与本地算的应当一致；不一致说明传输出了问题
  if (payload.data.sha256 && options.sha256 && payload.data.sha256 !== options.sha256) {
    throw new ApiError("服务器收到的文件与本地文件校验值不一致，请重新上传");
  }
  return payload.data;
}

export async function uploadReleaseAsset(file: File, options: Options): Promise<UploadOutcome> {
  let fallbackReason: string | undefined;
  if (file.size <= DIRECT_UPLOAD_MAX_BYTES) {
    let ticket;
    try {
      ticket = await presignAdminReleaseAsset(options.token, options.appKey, {
        fileName: file.name,
        fileSize: file.size,
        contentType: options.contentType || file.type || undefined
      });
    } catch (error) {
      // 旧版服务端没有直传接口（404）时照常走服务器上传
      if (error instanceof ApiError && error.status === 404) ticket = null;
      else throw error;
    }
    if (ticket?.mode === "direct" && ticket.uploadUrl && ticket.token) {
      if (options.signal?.aborted) throw new UploadAborted();
      const { status } = await xhrSend(ticket.method || "PUT", ticket.uploadUrl, file, ticket.headers ?? {}, options.onProgress, options.signal);
      if (status >= 200 && status < 300) {
        const result = await completeAdminReleaseAsset(options.token, options.appKey, { token: ticket.token, sha256: options.sha256 });
        return { result, mode: "direct" };
      }
      fallbackReason =
        status === 0 ? "存储桶未允许控制台跨域上传，已改为经服务器上传" : `存储拒绝了直传请求（HTTP ${status}），已改为经服务器上传`;
    } else {
      fallbackReason = ticket?.reason || (ticket === null ? "服务端不支持直传" : undefined);
    }
  } else {
    fallbackReason = "文件超过 2 GB";
  }
  options.onProgress?.(0);
  const result = await uploadViaServer(file, options);
  return { result, mode: "server", fallbackReason };
}

export function isUploadAborted(error: unknown) {
  return error instanceof Error && error.name === "AbortError";
}
