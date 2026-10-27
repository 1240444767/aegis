"""接口声明：每个工具调用的后端接口都先声明成一个 Route。

好处有两个：
  - 路径模板与后端 gin 路由同一种写法（`/api/admin/apps/:appkey/users/:userId`），
    tests/test_routes.py 拿全部声明去对后端的 routes.golden，写错一个字母就过不了测试；
  - 调用处只写参数，拼路径、带密钥、拆信封、把错误变成带提示的 ToolError 都在这里。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from fastmcp.exceptions import ToolError

from .client import AegisAPIError, build_path
from .runtime import current_key, runtime

REGISTRY: dict[tuple[str, str], "Route"] = {}


@dataclass(frozen=True)
class Route:
    method: str
    template: str

    async def __call__(self, *, query: dict[str, Any] | None = None, body: Any = None, **path_params: Any) -> Any:
        try:
            path = build_path(self.template, path_params)
        except ValueError as exc:
            raise ToolError(str(exc)) from exc
        try:
            return await runtime().client.request(self.method, path, key=current_key(), query=query, body=body)
        except AegisAPIError as exc:
            raise ToolError(exc.describe()) from exc

    @property
    def path_params(self) -> list[str]:
        return [seg[1:] for seg in self.template.split("/") if seg.startswith(":")]


def route(method: str, template: str) -> Route:
    item = Route(method.upper(), template)
    REGISTRY[(item.method, item.template)] = item
    return item


def GET(template: str) -> Route:
    return route("GET", template)


def POST(template: str) -> Route:
    return route("POST", template)


def PUT(template: str) -> Route:
    return route("PUT", template)


def PATCH(template: str) -> Route:
    return route("PATCH", template)


def DELETE(template: str) -> Route:
    return route("DELETE", template)
