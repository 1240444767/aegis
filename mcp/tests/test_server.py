"""用 httpx.MockTransport 模拟 Aegis 后端，验证工具拼出的请求与错误处理。"""

from __future__ import annotations

import json

import httpx
import pytest
from mcp.server.mcpserver.exceptions import ToolError

from aegis_mcp.client import AegisAPIError, _unwrap, build_path, normalize_template
from aegis_mcp.config import Settings
from aegis_mcp.server import _schema_brief, build_server

KEY = "aegis_mcp_" + "x" * 43


def make_server(handler):
    server = build_server(Settings(base_url="https://aegis.test", api_key=KEY))
    client = server._aegis_client
    client._http = httpx.AsyncClient(base_url="https://aegis.test", transport=httpx.MockTransport(handler))
    return server


def ok(data, status=200):
    return httpx.Response(status, json={"code": status, "message": "ok", "data": data})


def text_of(result) -> str:
    return "".join(getattr(block, "text", "") for block in result.content)


@pytest.fixture
def anyio_backend():
    return "asyncio"


def test_build_path():
    assert build_path("/api/admin/apps/:appkey/users/{userId}", {"appkey": "demo app", "userId": 7}) == "/api/admin/apps/demo%20app/users/7"
    with pytest.raises(ValueError):
        build_path("/api/admin/apps/:appkey", {})
    assert normalize_template("/api/admin/apps/{appkey}/users/{userId}") == "/api/admin/apps/:appkey/users/:userId"


def test_unwrap_errors_carry_hint():
    response = httpx.Response(403, json={"code": 40164, "message": "范围不包含", "requestId": "r1"})
    with pytest.raises(AegisAPIError) as info:
        _unwrap(response)
    assert info.value.code == 40164
    assert "控制台" in info.value.describe() and "r1" in info.value.describe()


@pytest.mark.anyio
async def test_search_users_builds_query_and_sends_key():
    seen = {}

    def handler(request: httpx.Request):
        seen["auth"] = request.headers["Authorization"]
        seen["url"] = str(request.url)
        return ok({"list": [{"id": 1}], "total": 1})

    server = make_server(handler)
    result = await server.call_tool("aegis_search_users", {"app": "demo", "keyword": "alice", "enabled": True})
    assert seen["auth"] == f"Bearer {KEY}"
    assert seen["url"].startswith("https://aegis.test/api/admin/apps/demo/users?")
    assert "keyword=alice" in seen["url"] and "enabled=true" in seen["url"] and "email=" not in seen["url"]
    assert json.loads(text_of(result))["total"] == 1


@pytest.mark.anyio
async def test_permission_error_becomes_tool_error():
    server = make_server(lambda request: httpx.Response(403, json={"code": 40166, "message": "只读密钥"}))
    with pytest.raises(ToolError) as info:
        await server.call_tool("aegis_revoke_user_sessions", {"app": "1", "user_id": 3})
    assert "40166" in str(info.value) and "只读" in str(info.value)


@pytest.mark.anyio
async def test_adjust_points_resolves_app_key_to_id():
    calls = []

    def handler(request: httpx.Request):
        calls.append((request.method, request.url.path, request.content))
        if request.url.path == "/api/admin/mcp/whoami":
            return ok({"apps": [{"id": 42, "appKey": "demo"}]})
        return ok({"balance": 10})

    server = make_server(handler)
    await server.call_tool("aegis_adjust_points", {"app": "demo", "user_id": 9, "amount": -5, "reason": "测试"})
    method, path, body = calls[-1]
    assert (method, path) == ("POST", "/api/app/points/adjust-integral")
    assert json.loads(body) == {"appid": 42, "user_id": 9, "amount": -5, "reason": "测试"}


@pytest.mark.anyio
async def test_list_endpoints_filters_by_keyword_and_permission():
    items = [
        {"method": "GET", "path": "/api/admin/apps/:appkey/users", "handler": "AdminAppUsers", "group": "应用", "permission": "app:user:read", "allowed": True},
        {"method": "POST", "path": "/api/admin/apps/:appkey/vip/grant", "handler": "AdminGrantAppUserVip", "group": "会员", "permission": "payment:write", "allowed": False, "reason": "只读密钥不能调用写接口"},
    ]
    server = make_server(lambda request: ok({"items": items, "total": 2}))
    shown = json.loads(text_of(await server.call_tool("aegis_list_endpoints", {"query": "vip"})))
    assert shown["total"] == 0
    shown = json.loads(text_of(await server.call_tool("aegis_list_endpoints", {"query": "vip", "allowed_only": False})))
    assert shown["items"][0]["reason"] == "只读密钥不能调用写接口"


@pytest.mark.anyio
async def test_call_api_rejects_session_only_paths():
    server = make_server(lambda request: ok({}))
    with pytest.raises(ToolError):
        await server.call_tool("aegis_call_api", {"method": "POST", "path": "/api/admin/profile/mcp-keys"})
    with pytest.raises(ToolError):
        await server.call_tool("aegis_call_api", {"method": "GET", "path": "/api/v1/apps/demo/config"})


@pytest.mark.anyio
async def test_call_api_fills_path_params():
    seen = {}

    def handler(request: httpx.Request):
        seen["path"] = request.url.path
        seen["body"] = json.loads(request.content or b"null")
        return ok({"done": True})

    server = make_server(handler)
    await server.call_tool("aegis_call_api", {
        "method": "PUT", "path": "/api/admin/apps/{appkey}/notices/:noticeId",
        "path_params": {"appkey": "demo", "noticeId": 5}, "body": {"title": "t"},
    })
    assert seen == {"path": "/api/admin/apps/demo/notices/5", "body": {"title": "t"}}


def test_schema_brief_resolves_refs():
    spec = {"components": {"schemas": {
        "Req": {"type": "object", "required": ["name"], "properties": {
            "name": {"type": "string"}, "kind": {"type": "string", "enum": ["a", "b"]},
            "tags": {"type": "array", "items": {"type": "string"}}, "child": {"$ref": "#/components/schemas/Child"},
        }},
        "Child": {"type": "object", "properties": {"id": {"type": "integer", "format": "int64"}}},
    }}}
    brief = _schema_brief(spec, {"$ref": "#/components/schemas/Req"}, depth=3)
    assert brief == {"name*": "string", "kind": {"type": "string", "enum": ["a", "b"]}, "tags": ["string"], "child": {"id": "integer(int64)"}}
