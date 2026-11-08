"""测试夹具：用 httpx.MockTransport 模拟 Aegis 后端，用 FastMCP 内存客户端调工具。

写模块测试的方式：

    async def test_xxx(backend, call):
        backend.reply("GET", "/api/admin/apps/demo/users", {"items": [], "total": 0})
        data = await call("users_search", {"app": "demo", "keyword": "a"})
        req = backend.last()
        assert req.query["keyword"] == "a"

- backend.reply(method, path, data, status=200, code=None)：为「方法 + 具体路径」登记返回（data 会包进 {code, message, data} 信封）；
  code 给 4xxxx 时模拟业务错误。未登记的请求默认返回 {"ok": true}。
- backend.requests / backend.last() / backend.find(method, path)：记录下的请求，含 method、path、query（dict）、json（解析后的请求体）、headers。
- call(name, args)：调工具返回结构化结果（工具返回值）；call_error(name, args) 断言工具报错并返回错误文本。
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any

import httpx
import pytest
from fastmcp import Client

from aegis_mcp.core.config import Settings
from aegis_mcp.core.runtime import runtime
from aegis_mcp.server import build_server

KEY = "aegis_mcp_" + "t" * 43


@dataclass
class Recorded:
    method: str
    path: str
    query: dict[str, str]
    json: Any
    headers: dict[str, str]


@dataclass
class MockBackend:
    routes: dict[tuple[str, str], httpx.Response] = field(default_factory=dict)
    requests: list[Recorded] = field(default_factory=list)

    def reply(self, method: str, path: str, data: Any = None, *, status: int = 200, code: int | None = None, message: str = "ok") -> None:
        envelope: dict[str, Any] = {"code": code or status, "message": message, "data": data}
        if code and code >= 400:
            envelope.pop("data")
            status = status if status >= 400 else 400
        self.routes[(method.upper(), path)] = httpx.Response(status, json=envelope)

    def handle(self, request: httpx.Request) -> httpx.Response:
        raw = request.content or b""
        self.requests.append(Recorded(
            method=request.method,
            path=request.url.path,
            query=dict(request.url.params),
            json=json.loads(raw) if raw else None,
            headers=dict(request.headers),
        ))
        hit = self.routes.get((request.method, request.url.path))
        if hit is not None:
            return httpx.Response(hit.status_code, content=hit.content, headers=hit.headers)
        if request.url.path == "/api/admin/mcp/whoami":
            return httpx.Response(200, json={"code": 200, "message": "ok", "data": {"apps": [{"id": 42, "appKey": "demo"}]}})
        return httpx.Response(200, json={"code": 200, "message": "ok", "data": {"ok": True}})

    def last(self) -> Recorded:
        assert self.requests, "没有发出任何请求"
        return self.requests[-1]

    def find(self, method: str, path: str) -> Recorded:
        for item in reversed(self.requests):
            if item.method == method and item.path == path:
                return item
        raise AssertionError(f"没有发出 {method} {path}；实际：{[(r.method, r.path) for r in self.requests]}")


@pytest.fixture
def backend() -> MockBackend:
    return MockBackend()


@pytest.fixture
def server(backend: MockBackend):
    srv = build_server(Settings(base_url="https://aegis.test", api_key=KEY))
    runtime().client._http = httpx.AsyncClient(base_url="https://aegis.test", transport=httpx.MockTransport(backend.handle))
    return srv


@pytest.fixture
async def mcp(server):
    async with Client(server) as client:
        yield client


@pytest.fixture
def call(mcp):
    async def _call(name: str, args: dict[str, Any] | None = None) -> Any:
        result = await mcp.call_tool(name, args or {})
        if result.data is not None:
            return result.data
        text = "".join(getattr(block, "text", "") for block in result.content)
        try:
            return json.loads(text)
        except ValueError:
            return text
    return _call


@pytest.fixture
def call_error(mcp):
    async def _call(name: str, args: dict[str, Any] | None = None) -> str:
        result = await mcp.call_tool(name, args or {}, raise_on_error=False)
        assert result.is_error, f"{name} 应当报错，实际返回：{result.data}"
        return "".join(getattr(block, "text", "") for block in result.content)
    return _call
