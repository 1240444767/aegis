"""内核与通用层：信封、错误提示、路径拼接、按请求解析密钥、call_api 的边界。"""

from __future__ import annotations

import httpx
import pytest

from aegis_mcp.core.client import AegisAPIError, _unwrap, build_path, normalize_template
from aegis_mcp.modules.core import schema_brief

from .conftest import KEY


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


async def test_key_is_sent(backend, call):
    backend.reply("GET", "/api/admin/apps", [{"id": 1, "name": "默认应用", "appKey": "demo", "status": True, "settings": {"x": 1}}])
    assert await call("apps_list") == [{"id": 1, "name": "默认应用", "appKey": "demo", "status": True, "registerStatus": None, "loginStatus": None, "createdAt": None}]
    assert backend.last().headers["authorization"] == f"Bearer {KEY}"


async def test_permission_error_becomes_tool_error(backend, call_error):
    backend.reply("DELETE", "/api/admin/apps/demo", code=40166, status=403, message="只读密钥")
    text = await call_error("apps_delete", {"app": "demo"})
    assert "40166" in text and "只读" in text


async def test_call_api_fills_path_params(backend, call):
    await call("call_api", {
        "method": "PUT", "path": "/api/admin/apps/{appkey}/notices/:noticeId",
        "path_params": {"appkey": "demo", "noticeId": 5}, "body": {"title": "t"},
    })
    req = backend.last()
    assert (req.method, req.path, req.json) == ("PUT", "/api/admin/apps/demo/notices/5", {"title": "t"})


async def test_call_api_rejects_session_only_paths(call_error):
    await call_error("call_api", {"method": "POST", "path": "/api/admin/profile/mcp-keys"})
    await call_error("call_api", {"method": "GET", "path": "/api/v1/apps/demo/config"})


async def test_list_endpoints_filters(backend, call):
    backend.reply("GET", "/api/admin/mcp/endpoints", {"items": [
        {"method": "GET", "path": "/api/admin/apps/:appkey/users", "handler": "AdminAppUsers", "group": "应用", "permission": "app:user:read", "allowed": True},
        {"method": "POST", "path": "/api/admin/apps/:appkey/vip/grant", "handler": "AdminGrantAppUserVip", "group": "会员", "permission": "app:write", "allowed": False, "reason": "只读密钥不能调用写接口"},
    ]})
    assert (await call("list_endpoints", {"query": "vip"}))["total"] == 0
    shown = await call("list_endpoints", {"query": "vip", "allowed_only": False})
    assert shown["items"][0]["reason"] == "只读密钥不能调用写接口"


async def test_app_policy_update_merges(backend, call):
    backend.reply("GET", "/api/admin/apps/demo/policy", {"multiDeviceLogin": True, "multiDeviceLimit": 3})
    await call("apps_update_policy", {"app": "demo", "multi_device_limit": 5})
    assert backend.find("PUT", "/api/admin/apps/demo/policy").json == {"multiDeviceLogin": True, "multiDeviceLimit": 5}


def test_schema_brief_resolves_refs():
    spec = {"components": {"schemas": {
        "Req": {"type": "object", "required": ["name"], "properties": {
            "name": {"type": "string"}, "kind": {"type": "string", "enum": ["a", "b"]},
            "tags": {"type": "array", "items": {"type": "string"}}, "child": {"$ref": "#/components/schemas/Child"},
        }},
        "Child": {"type": "object", "properties": {"id": {"type": "integer", "format": "int64"}}},
    }}}
    brief = schema_brief(spec, {"$ref": "#/components/schemas/Req"}, depth=3)
    assert brief == {"name*": "string", "kind": {"type": "string", "enum": ["a", "b"]}, "tags": ["string"], "child": {"id": "integer(int64)"}}
