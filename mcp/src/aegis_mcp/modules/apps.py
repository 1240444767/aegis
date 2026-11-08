"""应用：列表、详情、新建与修改、启停与注册登录开关、登录策略、统计、工作台、治理视图。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, body, q

NAMESPACE = "apps"
server = FastMCP("apps")

APPS = GET("/api/admin/apps")
APP = GET("/api/admin/apps/:appkey")
APP_CREATE = POST("/api/admin/apps")
APP_UPDATE = PUT("/api/admin/apps/:appkey")
APP_DELETE = DELETE("/api/admin/apps/:appkey")
APP_POLICY = GET("/api/admin/apps/:appkey/policy")
APP_POLICY_UPDATE = PUT("/api/admin/apps/:appkey/policy")
APP_STATS = GET("/api/admin/apps/:appkey/stats")
APP_USER_TREND = GET("/api/admin/apps/:appkey/stats/user-trend")
APP_REGIONS = GET("/api/admin/apps/:appkey/stats/regions")
APP_AUTH_SOURCES = GET("/api/admin/apps/:appkey/stats/auth-sources")
APP_GOVERNANCE = GET("/api/admin/apps/:appkey/governance")
APP_GOVERNANCE_HISTORY = GET("/api/admin/apps/:appkey/governance/history")
DASHBOARD = GET("/api/admin/dashboard")


@server.tool(name="list", **READ("apps"))
async def list_apps() -> Any:
    """列出这把密钥可访问的应用（ID、名称、appKey、启用状态、注册 / 登录开关）。"""
    items = await APPS()
    return [
        {k: item.get(k) for k in ("id", "name", "appKey", "status", "registerStatus", "loginStatus", "createdAt")}
        for item in (items if isinstance(items, list) else [])
    ]


@server.tool(name="get", **READ("apps"))
async def get_app(app: AppRef) -> Any:
    """一个应用的完整信息与设置。"""
    return await APP(appkey=app)


@server.tool(**WRITE("apps"))
async def create(
    name: Annotated[str, Field(min_length=1, description="应用名称")],
    status: Annotated[bool, Field(description="是否启用")] = True,
    register_status: Annotated[bool, Field(description="是否开放注册")] = True,
    login_status: Annotated[bool, Field(description="是否开放登录")] = True,
    settings: Annotated[dict[str, Any] | None, Field(description="应用设置（键值对）")] = None,
) -> Any:
    """新建应用。创建者自动成为该应用的管理员。"""
    return await APP_CREATE(body=body(name=name, status=status, registerStatus=register_status, loginStatus=login_status, settings=settings))


@server.tool(**WRITE("apps"))
async def update(
    app: AppRef,
    name: Annotated[str | None, Field(description="新名称，不改留空")] = None,
    status: Annotated[bool | None, Field(description="启用 / 停用整个应用（停用后用户无法使用）")] = None,
    disabled_reason: Annotated[str | None, Field(description="停用原因（用户可见）")] = None,
    register_status: Annotated[bool | None, Field(description="开放 / 关闭注册")] = None,
    disabled_register_reason: str | None = None,
    login_status: Annotated[bool | None, Field(description="开放 / 关闭登录")] = None,
    disabled_login_reason: str | None = None,
    settings: Annotated[dict[str, Any] | None, Field(description="应用设置（键值对，按后端语义合并或覆盖）")] = None,
) -> Any:
    """修改应用基本信息与开关。只传要改的字段。关闭登录或停用应用会立刻影响全部用户，执行前先确认。"""
    return await APP_UPDATE(appkey=app, body=body(
        name=name, status=status, disabledReason=disabled_reason,
        registerStatus=register_status, disabledRegisterReason=disabled_register_reason,
        loginStatus=login_status, disabledLoginReason=disabled_login_reason, settings=settings,
    ))


@server.tool(name="delete", **DESTRUCTIVE("apps"))
async def delete_app(app: AppRef) -> Any:
    """删除应用及其全部数据（不可恢复）。务必先向用户确认应用名称与 ID。"""
    return await APP_DELETE(appkey=app)


@server.tool(**READ("apps"))
async def policy(app: AppRef) -> Any:
    """应用的登录策略：多设备登录、设备 / IP / 账号校验、注册 IP 校验。"""
    return await APP_POLICY(appkey=app)


@server.tool(**WRITE("apps"))
async def update_policy(
    app: AppRef,
    multi_device_login: Annotated[bool | None, Field(description="是否允许多设备同时登录")] = None,
    multi_device_limit: Annotated[int | None, Field(ge=0, description="同时在线设备上限，0 为不限")] = None,
    login_check_device: Annotated[bool | None, Field(description="登录校验设备")] = None,
    login_check_device_timeout: Annotated[int | None, Field(ge=0, description="设备校验超时（秒）")] = None,
    login_check_ip: Annotated[bool | None, Field(description="登录校验 IP")] = None,
    login_check_user: Annotated[bool | None, Field(description="登录校验账号")] = None,
    register_check_ip: Annotated[bool | None, Field(description="注册校验 IP")] = None,
) -> Any:
    """修改应用登录策略。只传要改的字段（未传的保持原值）。"""
    current = await APP_POLICY(appkey=app) or {}
    merged = {**current, **body(
        multiDeviceLogin=multi_device_login, multiDeviceLimit=multi_device_limit,
        loginCheckDevice=login_check_device, loginCheckDeviceTimeOut=login_check_device_timeout,
        loginCheckIp=login_check_ip, loginCheckUser=login_check_user, registerCheckIp=register_check_ip,
    )}
    return await APP_POLICY_UPDATE(appkey=app, body=merged)


@server.tool(**READ("apps"))
async def stats(
    app: AppRef,
    trend_days: Annotated[int, Field(ge=0, le=365, description="另取最近 N 天的用户增长趋势，0 为不取")] = 0,
    regions: Annotated[Literal["", "country", "province", "city"], Field(description="另取用户地域分布的维度，空为不取")] = "",
    auth_sources: Annotated[bool, Field(description="另取注册 / 登录来源分布")] = False,
) -> Any:
    """应用用户统计（总数、今日新增、活跃、登录成功失败），可附带增长趋势、地域分布、来源分布。"""
    result: dict[str, Any] = {"stats": await APP_STATS(appkey=app)}
    if trend_days:
        result["userTrend"] = await APP_USER_TREND(appkey=app, query={"days": trend_days})
    if regions:
        result["regions"] = await APP_REGIONS(appkey=app, query=q(type=regions, limit=50))
    if auth_sources:
        result["authSources"] = await APP_AUTH_SOURCES(appkey=app)
    return result


@server.tool(**READ("apps"))
async def dashboard() -> Any:
    """管理工作台总览（内容按当前管理员身份裁剪）。"""
    return await DASHBOARD()


@server.tool(**READ("apps"))
async def governance(app: AppRef, history: Annotated[bool, Field(description="同时取治理历史")] = False) -> Any:
    """平台对该应用的治理状态（限制 / 冻结 / 停运等）与申诉情况。"""
    result: dict[str, Any] = {"governance": await APP_GOVERNANCE(appkey=app)}
    if history:
        result["history"] = await APP_GOVERNANCE_HISTORY(appkey=app)
    return result
