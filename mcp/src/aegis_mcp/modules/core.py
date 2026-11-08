"""通用层：凭证自述、接口目录、参数说明、任意管理端接口调用。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import READ, DESTRUCTIVE
from ..core.client import AegisAPIError, build_path, normalize_template
from ..core.runtime import current_key, runtime

NAMESPACE = ""
server = FastMCP("core")

Method = Literal["GET", "POST", "PUT", "PATCH", "DELETE"]


async def _guard(coro):
    try:
        return await coro
    except AegisAPIError as exc:
        raise ToolError(exc.describe()) from exc


@server.tool(**READ("core"))
async def whoami() -> Any:
    """当前凭证代表哪个管理员、密钥的权限范围 / 应用范围 / 是否只读、生效权限点与可访问的应用列表。开始任何操作前先调用一次。"""
    return await _guard(runtime().client.whoami(current_key()))


@server.tool(**READ("core"))
async def list_endpoints(
    query: Annotated[str, Field(description="关键词，空格分隔多个（匹配路径、处理函数名、分组、权限点），如 \"lottery prize\"")] = "",
    group: Annotated[str, Field(description="只看某个分组（中文分组名，见结果里的 groups）")] = "",
    method: Annotated[Literal["", "GET", "POST", "PUT", "PATCH", "DELETE"], Field(description="只看某个 HTTP 方法")] = "",
    allowed_only: Annotated[bool, Field(description="只列出当前密钥可调用的接口")] = True,
    limit: Annotated[int, Field(ge=1, le=300, description="最多返回条数")] = 60,
) -> Any:
    """在 Aegis 管理端全部接口里查找（约 970 条）。每条带方法、路径模板、处理函数名、分组、所需权限点、是否按应用鉴权，以及当前密钥能不能调和原因。没有现成工具时用它，再用 describe_endpoint 看参数。"""
    items = await _guard(runtime().client.endpoints(current_key()))
    words = [word.lower() for word in query.split() if word.strip()]
    groups: dict[str, int] = {}
    matched: list[dict[str, Any]] = []
    for item in items:
        if allowed_only and not item.get("allowed"):
            continue
        if method and item.get("method") != method:
            continue
        if group and item.get("group") != group:
            continue
        haystack = " ".join(str(item.get(k) or "") for k in ("method", "path", "handler", "group", "permission", "permissionName")).lower()
        if words and not all(word in haystack for word in words):
            continue
        groups[item.get("group") or ""] = groups.get(item.get("group") or "", 0) + 1
        matched.append(item)
    rows = []
    for item in matched[:limit]:
        row = {k: item.get(k) for k in ("method", "path", "handler", "group", "appScoped")}
        row["permission"] = item.get("permission") or None
        if not item.get("allowed"):
            row.update(allowed=False, reason=item.get("reason"))
        elif item.get("reason"):
            row["note"] = item.get("reason")
        rows.append(row)
    return {"total": len(matched), "shown": len(rows), "groups": groups, "items": rows}


@server.tool(**READ("core"))
async def describe_endpoint(
    method: Annotated[Method, Field(description="HTTP 方法")],
    path: Annotated[str, Field(description="路径模板，如 /api/admin/apps/:appkey/users/:userId（{x} 写法也可以）")],
) -> Any:
    """一条管理端接口的参数说明：路径参数、查询参数、请求体字段（类型、枚举、必填带 *）、所需权限点，以及当前密钥能否调用。"""
    template = normalize_template(path.strip())
    spec = await _guard(runtime().client.openapi())
    endpoints = await _guard(runtime().client.endpoints(current_key()))
    verdict = next((e for e in endpoints if e.get("method") == method and e.get("path") == template), None)
    operation = None
    for spec_path, item in (spec.get("paths") or {}).items():
        if normalize_template(spec_path) == template:
            operation = item.get(method.lower())
            break
    if operation is None and verdict is None:
        raise ToolError(f"没有 {method} {template} 这条接口，请用 list_endpoints 查找")
    result: dict[str, Any] = {"method": method, "path": template}
    if verdict:
        result.update({k: verdict.get(k) for k in ("handler", "group", "permission", "permissionName", "appScoped", "allowed", "reason") if verdict.get(k) not in (None, "")})
    if operation:
        result["pathParams"] = [p["name"] for p in operation.get("parameters", []) if p.get("in") == "path"]
        result["query"] = {p["name"]: schema_brief(spec, p.get("schema") or {}, depth=1) for p in operation.get("parameters", []) if p.get("in") == "query"}
        request_body = operation.get("requestBody")
        if request_body:
            content = request_body.get("content") or {}
            schema = (content.get("application/json") or next(iter(content.values()), {})).get("schema") or {}
            result["body"] = schema_brief(spec, schema, depth=3)
        if not result["query"] and not request_body and method != "GET":
            result["note"] = "文档没有推导出请求体字段（处理函数可能手写解析或无参数）。可先按相近接口的结构试调，错误信息会指出缺哪个字段。"
    if result.get("appScoped"):
        result["appHint"] = "按应用鉴权：路径里的 :appkey 填 appKey 或应用 ID；没有 :appkey 的接口在 query 或 body 里带 appid。"
    return result


@server.tool(**DESTRUCTIVE("core"))
async def call_api(
    method: Annotated[Method, Field(description="HTTP 方法")],
    path: Annotated[str, Field(description="路径模板或具体路径，必须以 /api/admin/ 或 /api/app/ 开头，如 /api/admin/apps/:appkey/users/:userId")],
    path_params: Annotated[dict[str, Any] | None, Field(description="路径参数，如 {\"appkey\": \"demo\", \"userId\": 12}")] = None,
    query: Annotated[dict[str, Any] | None, Field(description="查询参数")] = None,
    body: Annotated[Any, Field(description="JSON 请求体（GET 忽略）")] = None,
) -> Any:
    """直接调用任意一条 Aegis 管理端接口（通用出口）。先用 list_endpoints / describe_endpoint 确认路径与参数。写操作真实生效并记审计；不可逆操作前先征得用户确认。"""
    raw = path.strip()
    if not raw.startswith(("/api/admin/", "/api/app/")):
        raise ToolError("call_api 只能调用管理端接口（/api/admin/… 或 /api/app/…）")
    if raw.startswith(("/api/admin/profile", "/api/admin/auth/")):
        raise ToolError("个人资料、登录与 MCP 密钥管理接口只接受控制台会话，MCP 不能调用")
    try:
        concrete = build_path(normalize_template(raw), path_params)
    except ValueError as exc:
        raise ToolError(str(exc)) from exc
    client = runtime().client
    result = await _guard(client.request(method, concrete, key=current_key(), query=query, body=body))
    if method != "GET":
        client.invalidate("whoami:")
    return result


def schema_brief(spec: dict[str, Any], schema: dict[str, Any], depth: int) -> Any:
    """把 OpenAPI schema 压成模型好读的简表：字段 → 类型（枚举、必填带 *、嵌套）。"""
    schema = _deref(spec, schema)
    kind = schema.get("type")
    if isinstance(kind, list):
        kind = next((k for k in kind if k != "null"), kind[0] if kind else None)
    if "enum" in schema:
        return {"type": kind or "string", "enum": schema["enum"]}
    if kind == "object" or "properties" in schema:
        props = schema.get("properties") or {}
        if not props:
            return "object"
        if depth <= 0:
            return "object{" + ", ".join(props) + "}"
        required = set(schema.get("required") or [])
        return {(name + ("*" if name in required else "")): schema_brief(spec, sub, depth - 1) for name, sub in props.items()}
    if kind == "array":
        items = schema.get("items") or {}
        return [schema_brief(spec, items, depth - 1)] if depth > 0 else "array"
    fmt = schema.get("format")
    return f"{kind}({fmt})" if fmt else (kind or "any")


def _deref(spec: dict[str, Any], schema: dict[str, Any]) -> dict[str, Any]:
    hops = 0
    while "$ref" in schema and hops < 16:
        name = schema["$ref"].rsplit("/", 1)[-1]
        schema = ((spec.get("components") or {}).get("schemas") or {}).get(name) or {}
        hops += 1
    for combo in ("allOf", "oneOf", "anyOf"):
        if combo in schema and "properties" not in schema:
            parts = [_deref(spec, part) for part in schema[combo]]
            concrete = [p for p in parts if p.get("type") != "null"]
            if combo == "allOf":
                merged: dict[str, Any] = {"type": "object", "properties": {}}
                for part in concrete:
                    merged["properties"].update(part.get("properties") or {})
                return merged
            if concrete:
                return concrete[0]
    return schema
