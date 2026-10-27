"""Aegis MCP 服务端：工具定义。

工具分两层：
  - 通用层（aegis_list_endpoints / aegis_describe_endpoint / aegis_call_api）覆盖管理端全部接口，
    目录来自后端 /api/admin/mcp/endpoints（已按当前密钥标出能不能调），参数来自 /openapi.json；
  - 常用层把最常见的运营动作包成带类型参数的工具，模型不必先查目录。

一切权限以服务端为准：密钥范围、只读、管理员本人权限都在 Aegis 里判，
这里只把拒绝原因与下一步提示原样交给模型。
"""

from __future__ import annotations

import json
from typing import Annotated, Any, Literal

from mcp.server.mcpserver import Context, MCPServer
from mcp.server.mcpserver.exceptions import ToolError
from mcp.types import ToolAnnotations
from pydantic import Field

from .client import AegisAPIError, AegisClient, build_path, normalize_template
from .config import KEY_PREFIX, Settings

INSTRUCTIONS = """\
你正在通过 MCP 密钥操作 Aegis 管理端（多租户用户系统：应用、用户、会员、积分、工单、内容、审计……）。

工作方式：
1. 先调用 aegis_whoami 看清这把密钥代表谁、能访问哪些应用、权限范围是什么、是不是只读。
2. 常用操作直接用对应工具（aegis_search_users、aegis_get_user、aegis_vip_grant……）。
3. 没有现成工具时：aegis_list_endpoints 按关键词找接口 → aegis_describe_endpoint 看参数 → aegis_call_api 调用。
4. 应用参数 app 可以填 appKey 或数字应用 ID；userId 指管理端用户 ID（数据库主键）。
5. 写操作会真实生效并记入审计日志（会话列显示 mcp:<密钥前缀>）。执行封禁、撤销会员、删除、批量通知等
   不可逆或影响面大的操作前，先向用户复述将要做什么并取得确认。
6. 遇到 40164/40165/40166 等权限错误不要换接口绕过，直接告诉用户需要在控制台调整密钥范围。
"""

READ = ToolAnnotations(read_only_hint=True, open_world_hint=False)
WRITE = ToolAnnotations(read_only_hint=False, destructive_hint=False, open_world_hint=False)
DESTRUCTIVE = ToolAnnotations(read_only_hint=False, destructive_hint=True, open_world_hint=False)

AppRef = Annotated[str, Field(description="应用：appKey 或数字应用 ID（aegis_list_apps 可查）")]
UserID = Annotated[int, Field(description="管理端用户 ID（用户主键，aegis_search_users 结果里的 id）", gt=0)]
Page = Annotated[int, Field(description="页码，从 1 开始", ge=1)]
Limit = Annotated[int, Field(description="每页条数", ge=1, le=200)]


def build_server(settings: Settings) -> MCPServer:
    client = AegisClient(settings)
    server = MCPServer(
        name="aegis",
        title="Aegis 管理端",
        instructions=INSTRUCTIONS,
        version=_version(),
    )

    def key_for(ctx: Context | None) -> str:
        """HTTP 模式下优先用客户端自带的密钥（Authorization: Bearer aegis_mcp_…），否则用环境变量里的。"""
        try:
            headers = ctx.headers if ctx is not None else None
        except ValueError:  # 请求之外（测试、直接调用）没有请求上下文
            headers = None
        if headers:
            for name in ("authorization", "Authorization", "x-aegis-mcp-key", "X-Aegis-MCP-Key"):
                value = (headers.get(name) or "").strip()
                if value.lower().startswith("bearer "):
                    value = value[7:].strip()
                if value.startswith(KEY_PREFIX):
                    return value
        if not settings.api_key:
            raise ToolError("没有可用的 MCP 密钥：设置 AEGIS_MCP_KEY，或在 HTTP 请求头 Authorization 中携带 Bearer aegis_mcp_…")
        return settings.api_key

    async def call(ctx: Context | None, method: str, path: str, *, query: dict[str, Any] | None = None, body: Any = None) -> Any:
        try:
            return await client.request(method, path, key=key_for(ctx), query=query, body=body)
        except AegisAPIError as exc:
            raise ToolError(exc.describe()) from exc

    def out(value: Any) -> str:
        return _render(value, settings.max_result_chars)

    def app_path(app: str, suffix: str = "") -> str:
        app = str(app).strip()
        if not app:
            raise ToolError("缺少应用参数 app")
        return build_path("/api/admin/apps/:appkey" + suffix, {"appkey": app})

    async def app_id_of(ctx: Context | None, app: str) -> int:
        app = str(app).strip()
        if app.isdigit():
            return int(app)
        try:
            who = await client.whoami(key_for(ctx))
        except AegisAPIError as exc:
            raise ToolError(exc.describe()) from exc
        for item in who.get("apps") or []:
            if str(item.get("appKey") or "") == app:
                return int(item["id"])
        raise ToolError(f"找不到 appKey 为 {app} 的应用（或这把密钥无权访问它），请用 aegis_list_apps 核对")

    # ───────────────────────── 自述与通用层 ─────────────────────────

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_whoami(ctx: Context) -> str:
        """当前凭证代表哪个管理员、密钥的权限范围 / 应用范围 / 是否只读、生效权限点与可访问的应用列表。开始任何操作前先调用一次。"""
        try:
            return out(await client.whoami(key_for(ctx)))
        except AegisAPIError as exc:
            raise ToolError(exc.describe()) from exc

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_list_endpoints(
        ctx: Context,
        query: Annotated[str, Field(description="关键词，空格分隔多个（匹配路径、处理函数名、分组、权限点），如 \"vip grant\"、\"users ban\"")] = "",
        group: Annotated[str, Field(description="只看某个分组（中文分组名，见结果里的 groups）")] = "",
        method: Annotated[Literal["", "GET", "POST", "PUT", "PATCH", "DELETE"], Field(description="只看某个 HTTP 方法")] = "",
        allowed_only: Annotated[bool, Field(description="只列出当前密钥可调用的接口")] = True,
        limit: Annotated[int, Field(ge=1, le=300, description="最多返回条数")] = 60,
    ) -> str:
        """在 Aegis 管理端全部接口里查找（约 900 条）。每条带方法、路径模板、处理函数名、分组、所需权限点、是否按应用鉴权，以及当前密钥能不能调和原因。找到后用 aegis_describe_endpoint 看参数。"""
        try:
            items = await client.endpoints(key_for(ctx))
        except AegisAPIError as exc:
            raise ToolError(exc.describe()) from exc
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
        rows = [
            {
                "method": item.get("method"),
                "path": item.get("path"),
                "handler": item.get("handler"),
                "group": item.get("group"),
                "permission": item.get("permission") or None,
                "appScoped": item.get("appScoped"),
                **({"allowed": False, "reason": item.get("reason")} if not item.get("allowed") else {}),
                **({"note": item.get("reason")} if item.get("allowed") and item.get("reason") else {}),
            }
            for item in matched[:limit]
        ]
        return out({"total": len(matched), "shown": len(rows), "groups": groups, "items": rows})

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_describe_endpoint(
        ctx: Context,
        method: Annotated[Literal["GET", "POST", "PUT", "PATCH", "DELETE"], Field(description="HTTP 方法")],
        path: Annotated[str, Field(description="路径模板，如 /api/admin/apps/:appkey/users/:userId（{x} 写法也可以）")],
    ) -> str:
        """一条管理端接口的参数说明：路径参数、查询参数、请求体字段（类型、枚举、是否必填）、所需权限点，以及当前密钥能否调用。"""
        template = normalize_template(path.strip())
        try:
            spec = await client.openapi()
            endpoints = await client.endpoints(key_for(ctx))
        except AegisAPIError as exc:
            raise ToolError(exc.describe()) from exc
        verdict = next((e for e in endpoints if e.get("method") == method and e.get("path") == template), None)
        operation = None
        for spec_path, item in (spec.get("paths") or {}).items():
            if normalize_template(spec_path) == template:
                operation = item.get(method.lower())
                break
        if operation is None and verdict is None:
            raise ToolError(f"没有 {method} {template} 这条接口，请用 aegis_list_endpoints 查找")
        result: dict[str, Any] = {"method": method, "path": template}
        if verdict:
            result.update({k: verdict.get(k) for k in ("handler", "group", "permission", "permissionName", "appScoped", "allowed", "reason") if verdict.get(k) not in (None, "")})
        if operation:
            result["pathParams"] = [p["name"] for p in operation.get("parameters", []) if p.get("in") == "path"]
            result["query"] = {
                p["name"]: _schema_brief(spec, p.get("schema") or {}, depth=1) for p in operation.get("parameters", []) if p.get("in") == "query"
            }
            body = operation.get("requestBody")
            if body:
                content = body.get("content") or {}
                schema = (content.get("application/json") or next(iter(content.values()), {})).get("schema") or {}
                result["body"] = _schema_brief(spec, schema, depth=3)
            if not result["query"] and not body and method != "GET":
                result["note"] = "文档没有推导出请求体字段（处理函数可能手写解析或无参数）。需要时可先用相近接口的结构试调，错误信息会指出缺哪个字段。"
        if result.get("appScoped"):
            result["appHint"] = "按应用鉴权：路径里的 :appkey 填 appKey 或应用 ID；没有 :appkey 的接口在 query 或 body 里带 appid。"
        return out(result)

    @server.tool(annotations=DESTRUCTIVE, structured_output=False)
    async def aegis_call_api(
        ctx: Context,
        method: Annotated[Literal["GET", "POST", "PUT", "PATCH", "DELETE"], Field(description="HTTP 方法")],
        path: Annotated[str, Field(description="路径模板或具体路径，必须以 /api/admin/ 或 /api/app/ 开头，如 /api/admin/apps/:appkey/users/:userId")],
        path_params: Annotated[dict[str, Any] | None, Field(description="路径参数，如 {\"appkey\": \"demo\", \"userId\": 12}")] = None,
        query: Annotated[dict[str, Any] | None, Field(description="查询参数")] = None,
        body: Annotated[Any, Field(description="JSON 请求体（GET 忽略）")] = None,
    ) -> str:
        """直接调用任意一条 Aegis 管理端接口（通用出口）。先用 aegis_list_endpoints / aegis_describe_endpoint 确认路径与参数。写操作会真实生效并记审计；不可逆操作前先征得用户确认。"""
        raw = path.strip()
        if not raw.startswith(("/api/admin/", "/api/app/")):
            raise ToolError("aegis_call_api 只能调用管理端接口（/api/admin/… 或 /api/app/…）")
        if raw.startswith(("/api/admin/profile", "/api/admin/auth/")):
            raise ToolError("个人资料、登录与 MCP 密钥管理接口只接受控制台会话，MCP 不能调用")
        try:
            concrete = build_path(normalize_template(raw), path_params)
        except ValueError as exc:
            raise ToolError(str(exc)) from exc
        result = await call(ctx, method, concrete, query=query, body=body)
        if method != "GET":
            client.invalidate("whoami:")
        return out(result)

    # ───────────────────────── 应用 ─────────────────────────

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_list_apps(ctx: Context) -> str:
        """列出这把密钥可访问的应用（ID、名称、appKey、启用状态、注册/登录开关）。"""
        items = await call(ctx, "GET", "/api/admin/apps")
        rows = [
            {k: item.get(k) for k in ("id", "name", "appKey", "status", "registerStatus", "loginStatus", "createdAt")}
            for item in (items or [])
        ]
        return out(rows)

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_get_app(ctx: Context, app: AppRef) -> str:
        """一个应用的完整信息与设置。"""
        return out(await call(ctx, "GET", app_path(app)))

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_app_stats(
        ctx: Context,
        app: AppRef,
        trend_days: Annotated[int, Field(ge=0, le=365, description="另取最近 N 天的用户增长趋势，0 为不取")] = 0,
    ) -> str:
        """应用的用户统计（总数、今日新增、活跃等）；可附带用户增长趋势。"""
        result: dict[str, Any] = {"stats": await call(ctx, "GET", app_path(app, "/stats"))}
        if trend_days:
            result["userTrend"] = await call(ctx, "GET", app_path(app, "/stats/user-trend"), query={"days": trend_days})
        return out(result)

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_dashboard(ctx: Context) -> str:
        """管理工作台总览（内容按当前管理员身份裁剪）。"""
        return out(await call(ctx, "GET", "/api/admin/dashboard"))

    # ───────────────────────── 用户 ─────────────────────────

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_search_users(
        ctx: Context,
        app: AppRef,
        keyword: Annotated[str, Field(description="模糊搜索（账号、昵称、邮箱、手机号等）")] = "",
        account: str = "",
        email: str = "",
        phone: str = "",
        nickname: str = "",
        user_id: Annotated[int | None, Field(description="按用户 ID 精确查")] = None,
        enabled: Annotated[bool | None, Field(description="true 只看正常账号，false 只看已禁用")] = None,
        created_from: Annotated[str, Field(description="注册时间起，ISO 8601")] = "",
        created_to: Annotated[str, Field(description="注册时间止，ISO 8601")] = "",
        sort: Annotated[str, Field(description="排序字段，如 createdAt / lastLoginAt / integral")] = "",
        order: Annotated[Literal["", "asc", "desc"], Field(description="排序方向")] = "",
        page: Page = 1,
        limit: Limit = 20,
    ) -> str:
        """在一个应用里搜索用户，返回分页结果。"""
        query = {
            "keyword": keyword, "account": account, "email": email, "phone": phone, "nickname": nickname,
            "userId": user_id, "enabled": enabled, "createdFrom": created_from, "createdTo": created_to,
            "sort": sort, "order": order, "page": page, "limit": limit,
        }
        return out(await call(ctx, "GET", app_path(app, "/users"), query=query))

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_get_user(
        ctx: Context,
        app: AppRef,
        user_id: UserID,
        include: Annotated[
            list[Literal["bans", "sessions", "vip", "wallet", "points", "feature_unlocks"]],
            Field(description="同时取回的附加信息"),
        ] = ["bans", "vip"],
    ) -> str:
        """一个用户的详情，可附带封禁记录、在线会话、会员权益、钱包、积分概况、功能解锁。附加项失败不影响主体，错误写在对应字段里。"""
        base = app_path(app, f"/users/{user_id}")
        result: dict[str, Any] = {"user": await call(ctx, "GET", base)}
        extras = {
            "bans": (base + "/bans", None),
            "sessions": (base + "/sessions", None),
            "vip": (app_path(app, "/vip/entitlement"), {"userId": user_id}),
            "wallet": (base + "/wallet", None),
            "points": (base + "/points/integral/summary", None),
            "feature_unlocks": (base + "/feature-unlocks", None),
        }
        for name in include:
            path, query = extras[name]
            try:
                result[name] = await call(ctx, "GET", path, query=query)
            except ToolError as exc:
                result[name] = {"error": str(exc)}
        return out(result)

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_user_login_audits(
        ctx: Context,
        app: AppRef,
        user_id: Annotated[int | None, Field(description="只看某个用户；不填则看整个应用", gt=0)] = None,
        status: Annotated[str, Field(description="success / failed 等")] = "",
        keyword: str = "",
        start: Annotated[str, Field(description="起始时间 ISO 8601（仅按用户查时生效）")] = "",
        end: Annotated[str, Field(description="结束时间 ISO 8601（仅按用户查时生效）")] = "",
        page: Page = 1,
        limit: Limit = 20,
    ) -> str:
        """登录审计：某个用户或整个应用的登录记录（IP、属地、设备、结果）。"""
        if user_id:
            path = app_path(app, f"/users/{user_id}/audits/login")
            query = {"status": status, "keyword": keyword, "from": start, "to": end, "page": page, "limit": limit}
        else:
            path = app_path(app, "/audits/login")
            query = {"status": status, "keyword": keyword, "page": page, "limit": limit}
        return out(await call(ctx, "GET", path, query=query))

    @server.tool(annotations=DESTRUCTIVE, structured_output=False)
    async def aegis_ban_user(
        ctx: Context,
        app: AppRef,
        user_id: UserID,
        reason: Annotated[str, Field(description="封禁原因（用户可见）", min_length=1)],
        ban_type: Annotated[Literal["temporary", "permanent"], Field(description="临时 / 永久")] = "temporary",
        ban_scope: Annotated[Literal["login", "all"], Field(description="login 只拦新登录；all 立即撤销全部会话并拦截一切访问")] = "login",
        end_at: Annotated[str, Field(description="临时封禁的结束时间 ISO 8601，临时封禁必填")] = "",
    ) -> str:
        """封禁一个用户。执行前先向用户确认对象、范围与期限。"""
        if ban_type == "temporary" and not end_at:
            raise ToolError("临时封禁需要 end_at（结束时间）")
        body: dict[str, Any] = {"reason": reason, "banType": ban_type, "banScope": ban_scope}
        if end_at:
            body["endAt"] = end_at
        return out(await call(ctx, "POST", app_path(app, f"/users/{user_id}/bans"), body=body))

    @server.tool(annotations=WRITE, structured_output=False)
    async def aegis_unban_user(
        ctx: Context,
        app: AppRef,
        user_id: UserID,
        ban_id: Annotated[int, Field(gt=0, description="封禁记录 ID（aegis_get_user 的 bans 里可查）")],
        reason: Annotated[str, Field(description="解除原因")] = "",
    ) -> str:
        """解除一条封禁。"""
        return out(await call(ctx, "POST", app_path(app, f"/users/{user_id}/bans/{ban_id}/revoke"), body={"reason": reason}))

    @server.tool(annotations=DESTRUCTIVE, structured_output=False)
    async def aegis_revoke_user_sessions(ctx: Context, app: AppRef, user_id: UserID) -> str:
        """让一个用户在所有设备上下线（撤销全部会话）。"""
        return out(await call(ctx, "POST", app_path(app, f"/users/{user_id}/revoke-sessions"), body={}))

    # ───────────────────────── 积分与会员 ─────────────────────────

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_user_points_ledger(
        ctx: Context,
        app: AppRef,
        user_id: UserID,
        kind: Annotated[Literal["integral", "experience"], Field(description="integral 积分 / experience 经验")] = "integral",
        direction: Annotated[str, Field(description="in 获得 / out 消耗，空为全部")] = "",
        category: str = "",
        keyword: str = "",
        start: Annotated[str, Field(description="起始时间 ISO 8601")] = "",
        end: Annotated[str, Field(description="结束时间 ISO 8601")] = "",
        page: Page = 1,
        limit: Limit = 20,
    ) -> str:
        """一个用户的积分或经验流水。"""
        query = {"direction": direction, "category": category, "keyword": keyword, "from": start, "to": end, "page": page, "limit": limit}
        return out(await call(ctx, "GET", app_path(app, f"/users/{user_id}/points/{kind}/transactions"), query=query))

    @server.tool(annotations=WRITE, structured_output=False)
    async def aegis_adjust_points(
        ctx: Context,
        app: AppRef,
        user_id: UserID,
        amount: Annotated[int, Field(description="调整数量：积分可正可负；经验只能为正")],
        reason: Annotated[str, Field(description="调整原因（记入流水）", min_length=1)],
        kind: Annotated[Literal["integral", "experience"], Field(description="integral 积分 / experience 经验")] = "integral",
        request_id: Annotated[str, Field(description="幂等键：同一个值重复提交只生效一次，建议每次调整生成一个")] = "",
    ) -> str:
        """给用户加减积分或增加经验。"""
        if amount == 0:
            raise ToolError("amount 不能为 0")
        app_id = await app_id_of(ctx, app)
        body: dict[str, Any] = {"appid": app_id, "user_id": user_id, "amount": amount, "reason": reason}
        if request_id:
            body["request_id"] = request_id
        path = "/api/app/points/adjust-integral" if kind == "integral" else "/api/app/points/adjust-experience"
        return out(await call(ctx, "POST", path, body=body))

    @server.tool(annotations=WRITE, structured_output=False)
    async def aegis_vip_grant(
        ctx: Context,
        app: AppRef,
        user_id: UserID,
        reason: Annotated[str, Field(description="发放原因", min_length=1)],
        days: Annotated[int | None, Field(description="会员天数（与 lifetime / plan_id 三选一）", gt=0)] = None,
        lifetime: Annotated[bool, Field(description="发放永久会员")] = False,
        plan_id: Annotated[int | None, Field(description="按套餐发放（套餐 ID，aegis_call_api GET /api/admin/apps/:appkey/vip/plans 可查）", gt=0)] = None,
        quantity: Annotated[int | None, Field(description="按套餐发放时的份数", gt=0)] = None,
        features: Annotated[list[str] | None, Field(description="附带的功能标识")] = None,
        preview: Annotated[bool, Field(description="只预览发放结果（折算、到期时间），不真正发放")] = False,
    ) -> str:
        """给用户发放会员（天数、永久或按套餐）。建议先 preview=true 预览，再确认发放。"""
        if not (days or lifetime or plan_id):
            raise ToolError("需要 days、lifetime、plan_id 三者之一")
        body: dict[str, Any] = {"userId": user_id, "reason": reason, "lifetime": lifetime}
        for name, value in (("days", days), ("planId", plan_id), ("quantity", quantity), ("features", features)):
            if value:
                body[name] = value
        suffix = "/vip/grant/preview" if preview else "/vip/grant"
        return out(await call(ctx, "POST", app_path(app, suffix), body=body))

    @server.tool(annotations=DESTRUCTIVE, structured_output=False)
    async def aegis_vip_revoke(
        ctx: Context,
        app: AppRef,
        user_id: UserID,
        reason: Annotated[str, Field(description="收回原因", min_length=1)],
        days: Annotated[int | None, Field(description="收回的天数", gt=0)] = None,
        lifetime: Annotated[bool, Field(description="取消永久会员")] = False,
    ) -> str:
        """收回用户的会员天数或永久会员。执行前先向用户确认。"""
        if not (days or lifetime):
            raise ToolError("需要 days 或 lifetime")
        body: dict[str, Any] = {"userId": user_id, "reason": reason, "lifetime": lifetime}
        if days:
            body["days"] = days
        return out(await call(ctx, "POST", app_path(app, "/vip/revoke"), body=body))

    # ───────────────────────── 内容与通知 ─────────────────────────

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_list_notices(
        ctx: Context,
        app: AppRef,
        status: Annotated[Literal["", "draft", "published", "archived"], Field(description="生命周期状态")] = "",
        type: Annotated[str, Field(description="notice / activity / maintenance / update / security")] = "",
        keyword: str = "",
        page: Page = 1,
        limit: Limit = 20,
    ) -> str:
        """应用公告列表。"""
        query = {"status": status, "type": type, "keyword": keyword, "page": page, "limit": limit}
        return out(await call(ctx, "GET", app_path(app, "/notices"), query=query))

    @server.tool(annotations=WRITE, structured_output=False)
    async def aegis_create_notice(
        ctx: Context,
        app: AppRef,
        title: Annotated[str, Field(min_length=1)],
        content: Annotated[str, Field(min_length=1, description="正文")],
        type: Annotated[Literal["notice", "activity", "maintenance", "update", "security"], Field(description="公告类型")] = "notice",
        level: Annotated[Literal["normal", "important", "critical"], Field(description="提示级别")] = "normal",
        status: Annotated[Literal["draft", "published"], Field(description="draft 存草稿 / published 立即发布")] = "draft",
        pinned: bool = False,
        start_time: Annotated[str, Field(description="生效时间 ISO 8601")] = "",
        end_time: Annotated[str, Field(description="结束时间 ISO 8601")] = "",
    ) -> str:
        """新建应用公告。默认存为草稿，确认后再发布。"""
        body: dict[str, Any] = {"title": title, "content": content, "type": type, "level": level, "status": status, "pinned": pinned}
        if start_time:
            body["startTime"] = start_time
        if end_time:
            body["endTime"] = end_time
        return out(await call(ctx, "POST", app_path(app, "/notices"), body=body))

    @server.tool(annotations=DESTRUCTIVE, structured_output=False)
    async def aegis_notify_users(
        ctx: Context,
        app: AppRef,
        user_ids: Annotated[list[int], Field(min_length=1, max_length=1000, description="接收人的用户 ID 列表")],
        title: Annotated[str, Field(min_length=1)],
        content: Annotated[str, Field(min_length=1)],
        level: Annotated[str, Field(description="info / warning / error 等")] = "info",
        type: Annotated[str, Field(description="消息类型，决定站内信分类（如 system / activity）")] = "system",
        expires_at: Annotated[str, Field(description="过期时间 ISO 8601")] = "",
    ) -> str:
        """给指定用户发站内信（会实时推送到用户设备）。发送前先向用户确认接收人与内容。"""
        body: dict[str, Any] = {"userIds": user_ids, "title": title, "content": content, "level": level, "type": type}
        if expires_at:
            body["expiresAt"] = expires_at
        return out(await call(ctx, "POST", app_path(app, "/notifications/bulk"), body=body))

    # ───────────────────────── 工单 ─────────────────────────

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_list_tickets(
        ctx: Context,
        app: Annotated[str, Field(description="只看某个应用（appKey 或应用 ID），空为可见的全部")] = "",
        status: Annotated[str, Field(description="open / processing / pending_user / pending_third_party / resolved / closed / cancelled")] = "",
        priority: str = "",
        keyword: str = "",
        mine: Annotated[bool, Field(description="只看指派给我的")] = False,
        unassigned: Annotated[bool, Field(description="只看未指派的")] = False,
        overdue_only: Annotated[bool, Field(description="只看超出 SLA 的")] = False,
        include_closed: bool = False,
        page: Page = 1,
        limit: Limit = 20,
    ) -> str:
        """工单列表（可见范围由服务端按权限裁剪）。"""
        query: dict[str, Any] = {
            "status": status, "priority": priority, "keyword": keyword, "page": page, "limit": limit,
            "mine": mine or None, "unassigned": unassigned or None, "overdueOnly": overdue_only or None,
            "includeClosed": include_closed or None,
        }
        if app:
            query["appid"] = await app_id_of(ctx, app)
        return out(await call(ctx, "GET", "/api/admin/tickets", query=query))

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_get_ticket(ctx: Context, ticket_id: Annotated[int, Field(gt=0)]) -> str:
        """一张工单的详情与往来回复。"""
        return out(await call(ctx, "GET", f"/api/admin/tickets/{ticket_id}"))

    @server.tool(annotations=WRITE, structured_output=False)
    async def aegis_reply_ticket(
        ctx: Context,
        ticket_id: Annotated[int, Field(gt=0)],
        content: Annotated[str, Field(min_length=1, description="回复正文")],
        internal: Annotated[bool, Field(description="true 为内部备注（提单人不可见）")] = False,
        next_status: Annotated[str, Field(description="回复后同时改成的状态，如 pending_user / resolved；空为不变")] = "",
    ) -> str:
        """回复工单或添加内部备注。对提单人可见的回复发出前，先让用户确认内容。"""
        body: dict[str, Any] = {"content": content, "internal": internal}
        if next_status:
            body["nextStatus"] = next_status
        return out(await call(ctx, "POST", f"/api/admin/tickets/{ticket_id}/replies", body=body))

    @server.tool(annotations=WRITE, structured_output=False)
    async def aegis_set_ticket_status(
        ctx: Context,
        ticket_id: Annotated[int, Field(gt=0)],
        status: Annotated[Literal["open", "processing", "pending_user", "pending_third_party", "resolved", "closed"], Field(description="目标状态")],
        reason: str = "",
        solution: Annotated[str, Field(description="解决方案（改为 resolved 时建议填写）")] = "",
    ) -> str:
        """修改工单状态（受理、挂起、解决、关闭、重开）。"""
        return out(await call(ctx, "POST", f"/api/admin/tickets/{ticket_id}/status", body={"status": status, "reason": reason, "solution": solution}))

    # ───────────────────────── 审计与运维 ─────────────────────────

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_audit_logs(
        ctx: Context,
        app_id: Annotated[int | None, Field(description="只看某个应用", gt=0)] = None,
        action: str = "",
        category: str = "",
        severity: Annotated[str, Field(description="info / notice / warning / critical")] = "",
        status: Annotated[str, Field(description="success / failure")] = "",
        admin_id: Annotated[int | None, Field(description="只看某个管理员", gt=0)] = None,
        session_id: Annotated[str, Field(description="会话列，MCP 调用为 mcp:<密钥前缀>")] = "",
        ip: str = "",
        keyword: str = "",
        start_time: Annotated[str, Field(description="起始时间 ISO 8601")] = "",
        end_time: Annotated[str, Field(description="结束时间 ISO 8601")] = "",
        page: Page = 1,
        limit: Limit = 20,
    ) -> str:
        """管理端操作审计日志（谁在什么时候做了什么）。"""
        query = {
            "appId": app_id, "action": action, "category": category, "severity": severity, "status": status,
            "adminId": admin_id, "sessionId": session_id, "ip": ip, "keyword": keyword,
            "startTime": start_time, "endTime": end_time, "page": page, "limit": limit,
        }
        return out(await call(ctx, "GET", "/api/admin/system/audit-logs", query=query))

    @server.tool(annotations=READ, structured_output=False)
    async def aegis_crash_reports(
        ctx: Context,
        app: AppRef,
        kind: str = "",
        app_version: str = "",
        keyword: Annotated[str, Field(description="在报告正文里搜索")] = "",
        user_id: Annotated[int | None, Field(gt=0)] = None,
        device_id: str = "",
        resolved: Annotated[bool | None, Field(description="true 已处理 / false 未处理")] = None,
        start: Annotated[str, Field(description="起始时间 ISO 8601")] = "",
        end: Annotated[str, Field(description="结束时间 ISO 8601")] = "",
        page: Page = 1,
        limit: Limit = 20,
    ) -> str:
        """客户端崩溃报告列表。看全文用 aegis_call_api GET /api/admin/apps/:appkey/crash-reports/:crashId。"""
        query = {
            "kind": kind, "appVersion": app_version, "keyword": keyword, "userId": user_id, "deviceId": device_id,
            "resolved": resolved, "start": start, "end": end, "page": page, "limit": limit,
        }
        return out(await call(ctx, "GET", app_path(app, "/crash-reports"), query=query))

    server._aegis_client = client  # type: ignore[attr-defined]  # 测试与关闭时用
    return server


def _render(value: Any, limit: int) -> str:
    text = json.dumps(value, ensure_ascii=False, indent=1, default=str) if not isinstance(value, str) else value
    if len(text) <= limit:
        return text
    return text[:limit] + f"\n…（结果过长，已截断，共 {len(text)} 个字符。请缩小 limit、加过滤条件或分页查看）"


def _schema_brief(spec: dict[str, Any], schema: dict[str, Any], depth: int) -> Any:
    """把 OpenAPI schema 压成模型好读的简表：字段 → 类型（枚举、必填、嵌套）。"""
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
        return {
            (name + ("*" if name in required else "")): _schema_brief(spec, sub, depth - 1)
            for name, sub in props.items()
        }
    if kind == "array":
        items = schema.get("items") or {}
        return [_schema_brief(spec, items, depth - 1)] if depth > 0 else "array"
    fmt = schema.get("format")
    return f"{kind}({fmt})" if fmt else (kind or "any")


def _deref(spec: dict[str, Any], schema: dict[str, Any]) -> dict[str, Any]:
    seen = 0
    while "$ref" in schema and seen < 16:
        name = schema["$ref"].rsplit("/", 1)[-1]
        schema = ((spec.get("components") or {}).get("schemas") or {}).get(name) or {}
        seen += 1
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


def _version() -> str:
    from . import __version__

    return __version__
