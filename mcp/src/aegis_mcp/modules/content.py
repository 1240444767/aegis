"""内容与通知：应用公告、Banner、用户站内信（发送与清理）、平台公告、内容总览。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, body, q

NAMESPACE = "content"
server = FastMCP("content")

OVERVIEW = GET("/api/admin/apps/:appkey/content/overview")

NOTICES = GET("/api/admin/apps/:appkey/notices")
NOTICE_CREATE = POST("/api/admin/apps/:appkey/notices")
NOTICE_UPDATE = PUT("/api/admin/apps/:appkey/notices/:noticeId")
NOTICE_DELETE = DELETE("/api/admin/apps/:appkey/notices/:noticeId")
NOTICES_DELETE = DELETE("/api/admin/apps/:appkey/notices")

BANNERS = GET("/api/admin/apps/:appkey/banners")
BANNER_CREATE = POST("/api/admin/apps/:appkey/banners")
BANNER_UPDATE = PUT("/api/admin/apps/:appkey/banners/:bannerId")
BANNER_DELETE = DELETE("/api/admin/apps/:appkey/banners/:bannerId")
BANNERS_DELETE = DELETE("/api/admin/apps/:appkey/banners")
BANNERS_ORDER = PUT("/api/admin/apps/:appkey/banners/order")

NOTIFICATIONS = GET("/api/admin/apps/:appkey/notifications")
NOTIFY_BULK = POST("/api/admin/apps/:appkey/notifications/bulk")
NOTIFICATIONS_DELETE = DELETE("/api/admin/apps/:appkey/notifications")
NOTIFICATIONS_DELETE_BY_FILTER = POST("/api/admin/apps/:appkey/notifications/delete-by-filter")

ANNOUNCEMENTS = GET("/api/admin/system/announcements")
ANNOUNCEMENT = GET("/api/admin/system/announcements/:id")
ANNOUNCEMENT_CREATE = POST("/api/admin/system/announcements")
ANNOUNCEMENT_UPDATE = PUT("/api/admin/system/announcements/:id")
ANNOUNCEMENT_DELETE = DELETE("/api/admin/system/announcements/:id")
ANNOUNCEMENT_PUBLISH = POST("/api/admin/system/announcements/:id/publish")
ANNOUNCEMENT_ARCHIVE = POST("/api/admin/system/announcements/:id/archive")

NoticeType = Literal["notice", "activity", "maintenance", "update", "security"]
NoticeLevel = Literal["normal", "important", "critical"]
NoticeStatus = Literal["draft", "published", "archived"]
Ids = Annotated[list[int], Field(min_length=1, max_length=500, description="ID 列表")]


@server.tool(**READ("content"))
async def overview(app: AppRef) -> Any:
    """应用内容总览：Banner、公告的数量与状态分布。"""
    return await OVERVIEW(appkey=app)


# ── 公告 ──

@server.tool(**READ("content"))
async def notices(
    app: AppRef,
    status: Annotated[Literal["", "draft", "published", "archived"], Field(description="生命周期状态")] = "",
    type: Annotated[Literal["", "notice", "activity", "maintenance", "update", "security"], Field(description="公告类型")] = "",
    level: Annotated[Literal["", "normal", "important", "critical"], Field(description="提示级别")] = "",
    keyword: str = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """应用公告列表（分页）。"""
    return await NOTICES(appkey=app, query=q(status=status, type=type, level=level, keyword=keyword, page=page, limit=limit))


@server.tool(**WRITE("content"))
async def create_notice(
    app: AppRef,
    title: Annotated[str, Field(min_length=1)],
    content: Annotated[str, Field(min_length=1, description="正文（Markdown）")],
    type: Annotated[NoticeType, Field(description="公告类型")] = "notice",
    level: Annotated[NoticeLevel, Field(description="提示级别")] = "normal",
    status: Annotated[Literal["draft", "published"], Field(description="draft 存草稿 / published 立即对用户可见")] = "draft",
    pinned: bool = False,
    start_time: IsoTime = "",
    end_time: IsoTime = "",
) -> Any:
    """新建应用公告。默认存为草稿，确认后再用 content_update_notice 发布。"""
    return await NOTICE_CREATE(appkey=app, body=body(
        title=title, content=content, type=type, level=level, status=status, pinned=pinned,
        startTime=start_time or None, endTime=end_time or None,
    ))


@server.tool(**WRITE("content"))
async def update_notice(
    app: AppRef,
    notice_id: Annotated[int, Field(gt=0)],
    title: str | None = None,
    content: str | None = None,
    type: NoticeType | None = None,
    level: NoticeLevel | None = None,
    status: Annotated[NoticeStatus | None, Field(description="改为 published 即发布，archived 即下线归档")] = None,
    pinned: bool | None = None,
    start_time: Annotated[str | None, Field(description="生效时间 ISO 8601")] = None,
    end_time: Annotated[str | None, Field(description="结束时间 ISO 8601")] = None,
) -> Any:
    """修改公告（只传要改的字段），也用于发布与归档。"""
    return await NOTICE_UPDATE(appkey=app, noticeId=notice_id, body=body(
        title=title, content=content, type=type, level=level, status=status, pinned=pinned,
        startTime=start_time, endTime=end_time,
    ))


@server.tool(**DESTRUCTIVE("content"))
async def delete_notices(app: AppRef, notice_ids: Ids) -> Any:
    """删除公告（一条或多条，不可恢复）。"""
    if len(notice_ids) == 1:
        return await NOTICE_DELETE(appkey=app, noticeId=notice_ids[0])
    return await NOTICES_DELETE(appkey=app, body={"ids": notice_ids})


# ── Banner ──

@server.tool(**READ("content"))
async def banners(
    app: AppRef,
    status: Annotated[Literal["", "enabled", "disabled"], Field(description="启用状态")] = "",
    type: str = "",
    keyword: str = "",
) -> Any:
    """应用 Banner 列表（按展示顺序，不分页）。"""
    return await BANNERS(appkey=app, query=q(status=status, type=type, keyword=keyword))


def _banner_body(header, title, content, url, type, position, status, start_time, end_time) -> dict[str, Any]:
    return body(header=header, title=title, content=content, url=url, type=type, position=position,
                status=status, startTime=start_time or None, endTime=end_time or None)


@server.tool(**WRITE("content"))
async def create_banner(
    app: AppRef,
    title: Annotated[str, Field(min_length=1)],
    header: Annotated[str, Field(description="图片地址（先在控制台上传图片，或填外链）")],
    url: Annotated[str, Field(description="点击跳转地址")] = "",
    content: str = "",
    type: Annotated[str, Field(description="Banner 类型（应用自定义）")] = "",
    position: Annotated[int | None, Field(description="位置序号")] = None,
    status: Annotated[bool, Field(description="是否启用")] = True,
    start_time: IsoTime = "",
    end_time: IsoTime = "",
) -> Any:
    """新建 Banner。图片上传是二进制接口，请提供已有图片地址。"""
    return await BANNER_CREATE(appkey=app, body=_banner_body(header, title, content, url, type or None, position, status, start_time, end_time))


@server.tool(**WRITE("content"))
async def update_banner(
    app: AppRef,
    banner_id: Annotated[int, Field(gt=0)],
    title: str | None = None,
    header: Annotated[str | None, Field(description="图片地址")] = None,
    url: str | None = None,
    content: str | None = None,
    type: str | None = None,
    position: int | None = None,
    status: Annotated[bool | None, Field(description="启用 / 停用")] = None,
    start_time: str | None = None,
    end_time: str | None = None,
) -> Any:
    """修改 Banner（只传要改的字段），也用于启停。"""
    return await BANNER_UPDATE(appkey=app, bannerId=banner_id, body=body(
        header=header, title=title, content=content, url=url, type=type, position=position,
        status=status, startTime=start_time, endTime=end_time,
    ))


@server.tool(**WRITE("content", idempotent=True))
async def reorder_banners(app: AppRef, banner_ids: Annotated[list[int], Field(min_length=1, description="完整的展示顺序（全部 Banner ID）")]) -> Any:
    """调整 Banner 展示顺序：提交完整顺序。"""
    return await BANNERS_ORDER(appkey=app, body={"ids": banner_ids})


@server.tool(**DESTRUCTIVE("content"))
async def delete_banners(app: AppRef, banner_ids: Ids) -> Any:
    """删除 Banner（一条或多条，不可恢复）。"""
    if len(banner_ids) == 1:
        return await BANNER_DELETE(appkey=app, bannerId=banner_ids[0])
    return await BANNERS_DELETE(appkey=app, body={"ids": banner_ids})


# ── 站内信 ──

@server.tool(**READ("content"))
async def notifications(
    app: AppRef,
    keyword: str = "",
    category: Annotated[str, Field(description="站内信分类")] = "",
    type: str = "",
    level: str = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """应用里已发出的用户站内信（管理视图，分页）。"""
    return await NOTIFICATIONS(appkey=app, query=q(keyword=keyword, category=category, type=type, level=level, page=page, limit=limit))


@server.tool(**DESTRUCTIVE("content"))
async def notify_users(
    app: AppRef,
    title: Annotated[str, Field(min_length=1)],
    content: Annotated[str, Field(min_length=1)],
    user_ids: Annotated[list[int] | None, Field(max_length=5000, description="接收人用户 ID；不填则按 keyword / enabled 筛选用户群发")] = None,
    keyword: Annotated[str, Field(description="不指定 user_ids 时按关键词筛选接收人")] = "",
    enabled_only: Annotated[bool | None, Field(description="群发时只发给正常账号")] = None,
    max_recipients: Annotated[int | None, Field(gt=0, description="群发时最多发给多少人")] = None,
    type: Annotated[str, Field(description="消息类型，决定站内信分类（如 system / activity）")] = "system",
    category: str = "",
    level: Annotated[str, Field(description="info / warning / error 等")] = "info",
    image: Annotated[str, Field(description="配图地址")] = "",
    action_type: Annotated[Literal["", "route", "url"], Field(description="点击动作：route 应用内路由键 / url https 链接")] = "",
    action_target: str = "",
    action_label: str = "",
    expires_at: IsoTime = "",
) -> Any:
    """给用户发站内信，实时推送到用户全部设备。可指定用户，也可按筛选条件群发。发送前务必向用户确认接收范围与内容。"""
    if not user_ids and not keyword and not max_recipients:
        raise ToolError("未指定 user_ids 时，必须给 keyword 或 max_recipients 限定群发范围，避免误发全体用户")
    action = body(type=action_type or None, target=action_target or None, label=action_label or None) or None
    return await NOTIFY_BULK(appkey=app, body=body(
        userIds=user_ids, keyword=keyword or None, enabled=enabled_only, limit=max_recipients,
        type=type, category=category or None, title=title, content=content, level=level,
        image=image or None, action=action, expiresAt=expires_at or None,
    ))


@server.tool(**DESTRUCTIVE("content"))
async def delete_notifications(
    app: AppRef,
    notification_ids: Annotated[list[int] | None, Field(description="按 ID 删除")] = None,
    keyword: Annotated[str, Field(description="按筛选条件删除：关键词")] = "",
    category: str = "",
    type: str = "",
    level: str = "",
    max_count: Annotated[int | None, Field(gt=0, description="按筛选删除时最多删除条数")] = None,
) -> Any:
    """删除已发出的站内信（用户端随之消失，不可恢复）。给 ID 就按 ID 删，否则按筛选条件删。"""
    if notification_ids:
        return await NOTIFICATIONS_DELETE(appkey=app, body={"ids": notification_ids})
    if not (keyword or category or type or level):
        raise ToolError("按筛选删除时至少给一个条件（keyword / category / type / level）")
    return await NOTIFICATIONS_DELETE_BY_FILTER(appkey=app, body=body(
        keyword=keyword or None, category=category or None, type=type or None, level=level or None, limit=max_count,
    ))


# ── 平台公告 ──

@server.tool(**READ("content"))
async def announcements(announcement_id: Annotated[int | None, Field(gt=0, description="给 ID 时取详情")] = None) -> Any:
    """平台公告（面向全部管理员的控制台公告）列表或详情。"""
    if announcement_id:
        return await ANNOUNCEMENT(id=announcement_id)
    return await ANNOUNCEMENTS()


@server.tool(**WRITE("content"))
async def save_announcement(
    title: Annotated[str, Field(min_length=1)],
    content: Annotated[str, Field(min_length=1)],
    announcement_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    type: str = "",
    level: str = "",
    pinned: bool = False,
    expires_at: IsoTime = "",
    metadata: dict[str, Any] | None = None,
) -> Any:
    """新建或修改平台公告（新建为草稿，用 content_announcement_status 发布）。需要超级管理员。"""
    payload = body(title=title, content=content, type=type or None, level=level or None, pinned=pinned,
                   expiresAt=expires_at or None, metadata=metadata)
    if announcement_id:
        return await ANNOUNCEMENT_UPDATE(id=announcement_id, body=payload)
    return await ANNOUNCEMENT_CREATE(body=payload)


@server.tool(**DESTRUCTIVE("content"))
async def announcement_status(
    announcement_id: Annotated[int, Field(gt=0)],
    action: Annotated[Literal["publish", "archive", "delete"], Field(description="publish 发布 / archive 归档 / delete 删除")],
) -> Any:
    """发布、归档或删除平台公告。"""
    if action == "publish":
        return await ANNOUNCEMENT_PUBLISH(id=announcement_id)
    if action == "archive":
        return await ANNOUNCEMENT_ARCHIVE(id=announcement_id)
    return await ANNOUNCEMENT_DELETE(id=announcement_id)
