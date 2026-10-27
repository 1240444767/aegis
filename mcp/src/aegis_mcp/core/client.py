"""Aegis 管理端 HTTP 客户端。

只做三件事：带上 MCP 密钥发请求、拆开 `{code, message, data}` 信封、把失败变成带错误码的异常。
权限一律以服务端判定为准，客户端不做任何预判放行。
"""

from __future__ import annotations

import hashlib
import re
import time
from dataclasses import dataclass, field
from typing import Any
from urllib.parse import quote

import httpx

from .. import __version__
from .config import Settings

_PATH_PARAM = re.compile(r"[:{]([A-Za-z_][A-Za-z0-9_]*)}?")


class AegisAPIError(Exception):
    """Aegis 返回的业务错误（或网络错误）。"""

    def __init__(self, status: int, code: int, message: str, request_id: str = "") -> None:
        self.status = status
        self.code = code
        self.message = message
        self.request_id = request_id
        super().__init__(self.describe())

    def describe(self) -> str:
        text = f"Aegis 拒绝了请求（HTTP {self.status}，错误码 {self.code}）：{self.message}"
        hint = ERROR_HINTS.get(self.code)
        if hint:
            text += f"\n提示：{hint}"
        if self.request_id:
            text += f"\n请求编号：{self.request_id}"
        return text


# 与后端 MCP 密钥错误码一一对应（docs/mcp.md），让模型知道下一步该怎么办而不是盲目重试。
ERROR_HINTS: dict[int, str] = {
    40110: "凭证无效。检查 AEGIS_MCP_KEY 是否完整、是否以 aegis_mcp_ 开头。",
    40160: "MCP 密钥无效。可能被轮换过，请到控制台重新复制明文。",
    40161: "MCP 密钥已过期，需要在控制台延长有效期或签发新密钥。",
    40162: "MCP 密钥已停用或撤销，需要在控制台重新启用或签发新密钥。",
    40163: "当前来源 IP 不在密钥的允许列表里。",
    40164: "密钥的权限范围不包含这个操作。不要重试，告诉用户需要在控制台为密钥追加对应权限点。",
    40165: "密钥没有授权这个应用。用 aegis_whoami 查看密钥可访问的应用。",
    40166: "这是只读密钥，不能执行写操作。",
    40167: "这个接口只接受控制台登录会话，MCP 不能调用。",
    40168: "调用过于频繁，稍等一分钟再试。",
    40311: "管理员本人没有这项权限，密钥无法超出签发人的权限。",
    40301: "该接口只允许超级管理员调用，且密钥需授予「全部权限」。",
    40058: "这个接口按应用作用域鉴权，需要在路径或参数里带上应用（appkey 或 appid）。",
    40315: "该接口未在后端登记权限规则，任何凭证都无法调用。",
}


@dataclass
class _Cached:
    value: Any
    expires: float


@dataclass
class AegisClient:
    settings: Settings
    _http: httpx.AsyncClient | None = None
    _cache: dict[str, _Cached] = field(default_factory=dict)

    @property
    def http(self) -> httpx.AsyncClient:
        if self._http is None:
            self._http = httpx.AsyncClient(
                base_url=self.settings.base_url,
                timeout=self.settings.timeout,
                verify=self.settings.verify_tls,
                headers={"User-Agent": f"aegis-mcp/{__version__}", "Accept": "application/json"},
                follow_redirects=False,
            )
        return self._http

    async def aclose(self) -> None:
        if self._http is not None:
            await self._http.aclose()
            self._http = None

    async def request(
        self,
        method: str,
        path: str,
        *,
        key: str,
        query: dict[str, Any] | None = None,
        body: Any = None,
    ) -> Any:
        """发一次管理端请求，返回信封里的 data（无信封时返回原始 JSON / 文本）。"""
        if not key:
            raise AegisAPIError(401, 40110, "未提供 MCP 密钥")
        method = method.upper()
        headers = {"Authorization": f"Bearer {key}"}
        params = _clean_query(query)
        kwargs: dict[str, Any] = {"params": params, "headers": headers}
        if body is not None and method not in {"GET", "HEAD"}:
            kwargs["json"] = body
        try:
            response = await self.http.request(method, path, **kwargs)
        except httpx.TimeoutException as exc:
            raise AegisAPIError(504, 0, f"请求超时（{self.settings.timeout:.0f} 秒）：{exc}") from exc
        except httpx.HTTPError as exc:
            raise AegisAPIError(502, 0, f"无法连接 Aegis：{exc}") from exc
        return _unwrap(response)

    async def get_public(self, path: str) -> Any:
        """免鉴权的公开资源（/openapi.json）。"""
        try:
            response = await self.http.get(path)
        except httpx.HTTPError as exc:
            raise AegisAPIError(502, 0, f"无法连接 Aegis：{exc}") from exc
        return _unwrap(response)

    async def cached(self, cache_key: str, ttl: float, loader) -> Any:
        now = time.monotonic()
        hit = self._cache.get(cache_key)
        if hit and hit.expires > now:
            return hit.value
        value = await loader()
        self._cache[cache_key] = _Cached(value, now + ttl)
        return value

    def invalidate(self, prefix: str) -> None:
        for name in [name for name in self._cache if name.startswith(prefix)]:
            self._cache.pop(name, None)

    # ── 常用读取（带缓存） ──

    async def openapi(self) -> dict[str, Any]:
        return await self.cached("openapi", 600, lambda: self.get_public("/openapi.json"))

    async def whoami(self, key: str) -> dict[str, Any]:
        return await self.cached(f"whoami:{_fingerprint(key)}", 60, lambda: self.request("GET", "/api/admin/mcp/whoami", key=key))

    async def endpoints(self, key: str) -> list[dict[str, Any]]:
        async def load() -> list[dict[str, Any]]:
            data = await self.request("GET", "/api/admin/mcp/endpoints", key=key)
            return list((data or {}).get("items") or [])

        return await self.cached(f"endpoints:{_fingerprint(key)}", 300, load)


def build_path(template: str, params: dict[str, Any] | None) -> str:
    """把 `/api/admin/apps/:appkey/users/{userId}` 这种模板填成具体路径。"""
    params = dict(params or {})
    missing: list[str] = []

    def repl(match: re.Match[str]) -> str:
        name = match.group(1)
        if name not in params or params[name] in (None, ""):
            missing.append(name)
            return match.group(0)
        return quote(str(params.pop(name)), safe="")

    path = _PATH_PARAM.sub(repl, template)
    if missing:
        raise ValueError(f"缺少路径参数：{', '.join(missing)}（模板 {template}）")
    if not path.startswith("/"):
        path = "/" + path
    return path


def normalize_template(path: str) -> str:
    """OpenAPI 的 `{x}` 与 gin 的 `:x` 统一成 gin 写法，便于两份目录对照。"""
    return re.sub(r"\{([A-Za-z_][A-Za-z0-9_]*)\}", r":\1", path)


def _clean_query(query: dict[str, Any] | None) -> dict[str, Any] | None:
    if not query:
        return None
    out: dict[str, Any] = {}
    for name, value in query.items():
        if value is None or value == "":
            continue
        if isinstance(value, bool):
            out[name] = "true" if value else "false"
        elif isinstance(value, (list, tuple)):
            out[name] = ",".join(str(item) for item in value)
        else:
            out[name] = value
    return out or None


def _unwrap(response: httpx.Response) -> Any:
    content_type = response.headers.get("content-type", "")
    payload: Any
    if "json" in content_type:
        try:
            payload = response.json()
        except ValueError:
            payload = response.text
    else:
        payload = response.text

    if isinstance(payload, dict) and "code" in payload and "message" in payload:
        code = int(payload.get("code") or 0)
        request_id = str(payload.get("requestId") or response.headers.get("X-Request-ID") or "")
        if response.is_success and code < 400:
            return payload.get("data")
        raise AegisAPIError(response.status_code, code or response.status_code, str(payload.get("message") or ""), request_id)
    if not response.is_success:
        text = payload if isinstance(payload, str) else str(payload)
        raise AegisAPIError(response.status_code, response.status_code, text[:500] or response.reason_phrase)
    return payload


def _fingerprint(key: str) -> str:
    # 缓存键用摘要：HTTP 模式下多把密钥共用一个进程，前缀相同的两把不能串了缓存
    return hashlib.sha256(key.encode()).hexdigest()[:24]
