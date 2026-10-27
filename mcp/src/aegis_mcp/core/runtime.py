"""进程级运行时：配置、共享的 HTTP 客户端，以及「这次调用用哪把密钥」。

stdio 模式只有一把密钥（环境变量）；HTTP 模式下每个 MCP 客户端可以在请求头里带自己的密钥，
同一进程服务多个管理员，因此密钥按请求解析，绝不缓存到模块级变量里。
"""

from __future__ import annotations

from dataclasses import dataclass

from fastmcp.exceptions import ToolError
from fastmcp.server.dependencies import get_http_headers

from .client import AegisClient
from .config import KEY_PREFIX, Settings


@dataclass
class Runtime:
    settings: Settings
    client: AegisClient


_runtime: Runtime | None = None


def init_runtime(settings: Settings) -> Runtime:
    global _runtime
    _runtime = Runtime(settings=settings, client=AegisClient(settings))
    return _runtime


def runtime() -> Runtime:
    if _runtime is None:
        raise RuntimeError("aegis-mcp 运行时未初始化：先调用 build_server(settings)")
    return _runtime


def current_key() -> str:
    """本次工具调用使用的 MCP 密钥：请求头优先，其次环境变量。"""
    headers = get_http_headers(include={"authorization", "x-aegis-mcp-key"})
    for name in ("authorization", "x-aegis-mcp-key"):
        value = (headers.get(name) or "").strip()
        if value.lower().startswith("bearer "):
            value = value[7:].strip()
        if value.startswith(KEY_PREFIX):
            return value
    key = runtime().settings.api_key
    if not key:
        raise ToolError("没有可用的 MCP 密钥：设置 AEGIS_MCP_KEY，或在 HTTP 请求头 Authorization 中携带 Bearer aegis_mcp_…")
    return key
