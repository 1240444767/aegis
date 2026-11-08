"""审计：应用与用户维度的登录 / 会话审计、平台管理审计日志、应用崩溃报告与服务端崩溃日志。"""

from __future__ import annotations

import json
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, READ, WRITE, AppRef, IsoTime, Limit, Page, q

NAMESPACE = "audit"
server = FastMCP("audit")

# ── 应用 / 用户维度 ──
APP_LOGIN = GET("/api/admin/apps/:appkey/audits/login")
APP_SESSIONS = GET("/api/admin/apps/:appkey/audits/sessions")
USER_LOGIN = GET("/api/admin/apps/:appkey/users/:userId/audits/login")
USER_SESSIONS = GET("/api/admin/apps/:appkey/users/:userId/audits/sessions")

# ── 平台管理审计日志（仅超级管理员） ──
LOGS = GET("/api/admin/system/audit-logs")
LOG = GET("/api/admin/system/audit-logs/:id")
LOG_FACETS = GET("/api/admin/system/audit-logs/facets")
LOG_OVERVIEW = GET("/api/admin/system/audit-logs/overview")
LOG_STATS = GET("/api/admin/system/audit-logs/stats")

# ── 应用崩溃报告 ──
CRASHES = GET("/api/admin/apps/:appkey/crash-reports")
CRASH = GET("/api/admin/apps/:appkey/crash-reports/:crashId")
CRASH_FACETS = GET("/api/admin/apps/:appkey/crash-reports/facets")
CRASH_RESOLVE = POST("/api/admin/apps/:appkey/crash-reports/resolve")
CRASH_DELETE = DELETE("/api/admin/apps/:appkey/crash-reports/:crashId")
CRASH_DELETE_BATCH = POST("/api/admin/apps/:appkey/crash-reports/delete")

# ── 服务端崩溃日志文件（仅超级管理员） ──
CRASHLOGS = GET("/api/admin/system/crashlogs")
CRASHLOG = GET("/api/admin/system/crashlogs/:filename")
CRASHLOG_DELETE = DELETE("/api/admin/system/crashlogs/:filename")

CrashIDs = Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, max_length=200, description="崩溃报告 ID 列表（列表里的 id，一次最多 200 条）")]
Filename = Annotated[str, Field(min_length=1, description="崩溃日志文件名（audit_crashlogs 列表里的文件名）")]


# ═══════════════════════ 登录与会话审计 ═══════════════════════


@server.tool(**READ("audit"))
async def login(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="只看这个用户（走用户维度接口，支持时间范围并回填归属地坐标）")] = None,
    status: Annotated[str, Field(description="登录结果过滤（如 success / failed，all 或留空为全部）")] = "",
    keyword: Annotated[str, Field(description="关键字：账号、昵称、IP、设备号、UA、登录方式")] = "",
    from_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339），仅 user_id 时生效")] = "",
    to_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339），仅 user_id 时生效")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """登录审计：应用全部用户或单个用户的登录记录（方式、结果、IP、设备、UA）。需要 audit:login:read。"""
    if user_id:
        return await USER_LOGIN(appkey=app, userId=user_id, query=q(
            status=status, keyword=keyword, **{"from": from_time, "to": to_time}, page=page, limit=limit,
        ))
    if from_time or to_time:
        raise ToolError("应用维度的登录审计不支持时间范围；需要按时间筛选请指定 user_id")
    return await APP_LOGIN(appkey=app, query=q(status=status, keyword=keyword, page=page, limit=limit))


@server.tool(**READ("audit"))
async def sessions(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="只看这个用户（走用户维度接口，支持时间范围）")] = None,
    event_type: Annotated[str, Field(description="事件类型（如 issued、refreshed、revoked、password_changed、password_reset；all 或留空为全部）")] = "",
    keyword: Annotated[str, Field(description="关键字：账号、昵称、令牌 JTI、事件类型、IP")] = "",
    from_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339），仅 user_id 时生效")] = "",
    to_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339），仅 user_id 时生效")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """会话审计：令牌签发、刷新、撤销、改密等会话事件（应用全部或单个用户）。需要 audit:session:read。"""
    if user_id:
        return await USER_SESSIONS(appkey=app, userId=user_id, query=q(
            eventType=event_type, keyword=keyword, **{"from": from_time, "to": to_time}, page=page, limit=limit,
        ))
    if from_time or to_time:
        raise ToolError("应用维度的会话审计不支持时间范围；需要按时间筛选请指定 user_id")
    return await APP_SESSIONS(appkey=app, query=q(eventType=event_type, keyword=keyword, page=page, limit=limit))


# ═══════════════════════ 平台管理审计日志 ═══════════════════════


@server.tool(**READ("audit"))
async def logs(
    log_id: Annotated[int | None, Field(gt=0, description="给了就只取这一条审计日志的详情")] = None,
    kind: Annotated[Literal["", "operation", "read", "write", "export", "auth"], Field(description="视图：operation 操作（默认视图，不含查看）、read 查看、write 写入、export 导出、auth 鉴权；留空为全部")] = "",
    app_id: Annotated[int | None, Field(gt=0, description="只看某个应用（数字应用 ID）")] = None,
    action: Annotated[str, Field(description="动作标识（如 user.ban.create；可选值见 audit_facets）")] = "",
    resource: Annotated[str, Field(description="资源类型（如 user_ban）")] = "",
    category: Annotated[str, Field(description="分类（可选值见 audit_facets）")] = "",
    severity: Annotated[Literal["", "info", "low", "medium", "high", "critical"], Field(description="风险等级")] = "",
    status: Annotated[str, Field(description="结果（如 success / failed）")] = "",
    status_code: Annotated[int | None, Field(ge=100, le=599, description="HTTP 状态码")] = None,
    admin_id: Annotated[int | None, Field(gt=0, description="操作的管理员 ID")] = None,
    ip: Annotated[str, Field(description="来源 IP")] = "",
    country: Annotated[str, Field(description="来源国家")] = "",
    request_id: Annotated[str, Field(description="请求编号")] = "",
    trace_id: Annotated[str, Field(description="链路编号")] = "",
    session_id: Annotated[str, Field(description="管理员会话 ID")] = "",
    keyword: Annotated[str, Field(description="关键字（摘要、路径等）")] = "",
    start_time: Annotated[IsoTime, Field(description="起始时间")] = "",
    end_time: Annotated[IsoTime, Field(description="结束时间")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """平台管理审计日志：管理员在控制台 / API 上的操作记录，或给 log_id 取一条详情。仅超级管理员可查。"""
    if log_id:
        return await LOG(id=log_id)
    return await LOGS(query=q(
        kind=kind, appId=app_id, action=action, resource=resource, category=category, severity=severity,
        status=status, statusCode=status_code, adminId=admin_id, ip=ip, country=country,
        requestId=request_id, traceId=trace_id, sessionId=session_id, keyword=keyword,
        startTime=start_time, endTime=end_time, page=page, limit=limit,
    ))


@server.tool(**READ("audit"))
async def facets() -> Any:
    """审计日志的筛选项取值（动作、资源、分类、管理员等出现过的值）。仅超级管理员可查。"""
    return await LOG_FACETS()


@server.tool(**READ("audit"))
async def overview(stats: Annotated[bool, Field(description="同时取统计数据（总量、按等级 / 分类分布等）")] = False) -> Any:
    """审计日志总览（近期操作量、高风险操作、活跃管理员等），可附带统计。仅超级管理员可查。"""
    data = await LOG_OVERVIEW()
    if not stats:
        return data
    return {"overview": data, "stats": await LOG_STATS()}


# ═══════════════════════ 应用崩溃报告 ═══════════════════════


@server.tool(**READ("audit"))
async def crash_reports(
    app: AppRef,
    crash_id: Annotated[int | None, Field(gt=0, description="给了就取这一条的详情（含报告正文、上报账号与对外用户 ID）")] = None,
    kind: Annotated[Literal["", "java", "native", "anr", "exit"], Field(description="类型：java 未捕获异常、native 原生信号、anr 无响应、exit 系统记录的异常退出")] = "",
    app_version: Annotated[str, Field(description="App 版本")] = "",
    keyword: Annotated[str, Field(description="关键字：标题、详情、两种编号")] = "",
    resolved: Annotated[bool | None, Field(description="只看已处理（true）或未处理（false）")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="上报用户 ID")] = None,
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    start: Annotated[IsoTime, Field(description="起始时间（RFC 3339 或 YYYY-MM-DD）")] = "",
    end: Annotated[IsoTime, Field(description="结束时间（RFC 3339 或 YYYY-MM-DD）")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """应用崩溃报告列表（不带正文），或给 crash_id 取一条详情。需要 app:read。"""
    if crash_id:
        return await CRASH(appkey=app, crashId=crash_id)
    return await CRASHES(appkey=app, query=q(
        kind=kind, appVersion=app_version, keyword=keyword, resolved=resolved, userId=user_id,
        deviceId=device_id, start=start, end=end, page=page, limit=limit,
    ))


@server.tool(**READ("audit"))
async def crash_facets(app: AppRef) -> Any:
    """崩溃报告概况：总数、未处理、近 24 小时，各类型数量与出现过的版本（供筛选）。"""
    return await CRASH_FACETS(appkey=app)


@server.tool(**WRITE("audit", idempotent=True))
async def resolve_crashes(
    app: AppRef,
    crash_ids: CrashIDs,
    resolved: Annotated[bool, Field(description="true 标记为已处理，false 取消标记")] = True,
) -> Any:
    """把一批崩溃报告标记为已处理（或取消标记），可随时改回。返回 {updated}。需要 app:write。"""
    return await CRASH_RESOLVE(appkey=app, body={"ids": crash_ids, "resolved": resolved})


@server.tool(**DESTRUCTIVE("audit"))
async def delete_crashes(app: AppRef, crash_ids: CrashIDs) -> Any:
    """删除崩溃报告（真删，不进回收站，不可恢复）。执行前先向用户确认。需要 app:write。"""
    if len(crash_ids) == 1:
        return await CRASH_DELETE(appkey=app, crashId=crash_ids[0])
    return await CRASH_DELETE_BATCH(appkey=app, body={"ids": crash_ids})


# ═══════════════════════ 服务端崩溃日志 ═══════════════════════


@server.tool(**READ("audit"))
async def crashlogs(
    filename: Annotated[str, Field(description="给了就读取这个日志文件的内容（JSON Lines）")] = "",
    tail: Annotated[int, Field(ge=1, le=2000, description="读取文件时只返回最后 N 行")] = 200,
) -> Any:
    """服务端（Aegis 后端进程）的崩溃日志：列出文件，或读取一个文件的最后 N 行。仅超级管理员可查。"""
    if not filename:
        return await CRASHLOGS()
    raw = await CRASHLOG(filename=filename)
    if not isinstance(raw, str):
        raw = json.dumps(raw, ensure_ascii=False)
    lines = [line for line in raw.splitlines() if line.strip()]
    entries: list[Any] = []
    for line in lines[-tail:]:
        try:
            entries.append(json.loads(line))
        except ValueError:
            entries.append(line)
    return {"filename": filename, "totalLines": len(lines), "returned": len(entries), "entries": entries}


@server.tool(**DESTRUCTIVE("audit"))
async def delete_crashlog(filename: Filename) -> Any:
    """删除一个服务端崩溃日志文件（不可恢复）。仅超级管理员可操作，执行前先确认。"""
    return await CRASHLOG_DELETE(filename=filename)

