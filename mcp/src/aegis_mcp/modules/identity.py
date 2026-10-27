"""用户主档（跨应用统一身份，仅超级管理员）：身份、应用映射与同步、标签、分群、黑白名单、合并、申诉、注销，以及用户设置的初始化与体检。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, Reason, UserID, app_id_of, body, q

NAMESPACE = "identity"
server = FastMCP("identity")

BASE = "/api/admin/system/user-master"
IDENTITIES = GET(f"{BASE}/identities")
IDENTITY = GET(f"{BASE}/identities/:id")
IDENTITY_CREATE = POST(f"{BASE}/identities")
IDENTITY_STATUS = PUT(f"{BASE}/identities/:id/status")
IDENTITY_LIFECYCLE = PUT(f"{BASE}/identities/:id/lifecycle")
IDENTITY_RISK = PUT(f"{BASE}/identities/:id/risk")
IDENTITY_MAPPINGS = GET(f"{BASE}/identities/:id/mappings")
IDENTITY_TAGS = GET(f"{BASE}/identities/:id/tags")
MAPPING_CREATE = POST(f"{BASE}/mappings")
MAPPING_DELETE = DELETE(f"{BASE}/mappings/:id")
SYNC = POST(f"{BASE}/sync")
SYNC_BATCH = POST(f"{BASE}/sync/batch")
TAGS = GET(f"{BASE}/tags")
TAG_CREATE = POST(f"{BASE}/tags")
TAG_DELETE = DELETE(f"{BASE}/tags/:id")
TAG_ASSIGN = POST(f"{BASE}/tags/assign")
TAG_REMOVE = POST(f"{BASE}/tags/remove")
SEGMENTS = GET(f"{BASE}/segments")
SEGMENT_CREATE = POST(f"{BASE}/segments")
SEGMENT_UPDATE = PUT(f"{BASE}/segments/:id")
SEGMENT_DELETE = DELETE(f"{BASE}/segments/:id")
SEGMENT_MEMBERS = GET(f"{BASE}/segments/:id/members")
SEGMENT_MEMBER_ADD = POST(f"{BASE}/segments/:id/members")
SEGMENT_MEMBER_REMOVE = DELETE(f"{BASE}/segments/:id/members/:identityId")
LISTS = GET(f"{BASE}/lists")
LIST_CREATE = POST(f"{BASE}/lists")
LIST_DELETE = DELETE(f"{BASE}/lists/:id")
LIST_CHECK = POST(f"{BASE}/lists/check")
MERGES = GET(f"{BASE}/merges")
MERGE = POST(f"{BASE}/merges")
APPEALS = GET(f"{BASE}/appeals")
APPEAL_CREATE = POST(f"{BASE}/appeals")
APPEAL_REVIEW = PUT(f"{BASE}/appeals/:id")
DEACTIVATIONS = GET(f"{BASE}/deactivations")
DEACTIVATION_CREATE = POST(f"{BASE}/deactivations")
DEACTIVATION_CANCEL = POST(f"{BASE}/deactivations/:id/cancel")

SETTINGS_USER = GET("/api/admin/user-settings/user")
SETTINGS_STATS = GET("/api/admin/user-settings/stats")
SETTINGS_INTEGRITY = GET("/api/admin/user-settings/check-integrity")
SETTINGS_INIT_USER = POST("/api/admin/user-settings/initialize-user")
SETTINGS_BATCH_INIT = POST("/api/admin/user-settings/batch-initialize")
SETTINGS_CLEANUP = DELETE("/api/admin/user-settings/cleanup")

IdentityID = Annotated[int, Field(gt=0, description="全局身份 ID")]


# ── 身份 ──

@server.tool(**READ("identity"))
async def search(
    keyword: Annotated[str, Field(description="姓名、邮箱、手机号")] = "",
    status: Annotated[str, Field(description="active / disabled 等")] = "",
    lifecycle_state: Annotated[str, Field(description="生命周期，如 registered / active / dormant / churned")] = "",
    risk_level: Annotated[str, Field(description="风险等级，如 normal / low / medium / high")] = "",
    tag_id: int | None = None,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """检索全局身份（一个自然人在各应用里的账号归到同一身份下）。"""
    return await IDENTITIES(query=q(keyword=keyword, status=status, lifecycleState=lifecycle_state, riskLevel=risk_level, tagId=tag_id, page=page, limit=limit))


@server.tool(name="get", **READ("identity"))
async def get_identity(identity_id: IdentityID, include: Annotated[list[Literal["mappings", "tags"]], Field(description="附带应用账号映射与标签")] = ["mappings", "tags"]) -> Any:
    """一个全局身份的详情，可附带它在各应用的账号映射与标签。"""
    result: dict[str, Any] = {"identity": await IDENTITY(id=identity_id)}
    if "mappings" in include:
        result["mappings"] = await IDENTITY_MAPPINGS(id=identity_id)
    if "tags" in include:
        result["tags"] = await IDENTITY_TAGS(id=identity_id)
    return result


@server.tool(**WRITE("identity"))
async def create(display_name: str = "", email: str = "", phone: str = "") -> Any:
    """手工建一个全局身份（通常由同步自动生成）。"""
    if not (display_name or email or phone):
        raise ToolError("至少给出名称、邮箱、手机号之一")
    return await IDENTITY_CREATE(body=body(displayName=display_name, email=email, phone=phone))


@server.tool(**WRITE("identity"))
async def update(
    identity_id: IdentityID,
    status: Annotated[str | None, Field(description="身份状态，如 active / disabled")] = None,
    lifecycle_state: Annotated[str | None, Field(description="生命周期状态")] = None,
    risk_level: Annotated[str | None, Field(description="风险等级")] = None,
    risk_score: Annotated[int | None, Field(ge=0, le=100, description="风险分（与 risk_level 一起提交）")] = None,
) -> Any:
    """修改身份的状态、生命周期或风险等级（只传要改的）。"""
    if status is None and lifecycle_state is None and risk_level is None:
        raise ToolError("需要 status、lifecycle_state、risk_level 之一")
    result: dict[str, Any] = {}
    if status is not None:
        result["status"] = await IDENTITY_STATUS(id=identity_id, body={"status": status})
    if lifecycle_state is not None:
        result["lifecycle"] = await IDENTITY_LIFECYCLE(id=identity_id, body={"state": lifecycle_state})
    if risk_level is not None:
        result["risk"] = await IDENTITY_RISK(id=identity_id, body=body(level=risk_level, score=risk_score))
    return result


@server.tool(**WRITE("identity"))
async def link_account(identity_id: IdentityID, app: AppRef, user_id: UserID) -> Any:
    """把某个应用的用户账号挂到全局身份下。"""
    return await MAPPING_CREATE(body={"identityId": identity_id, "appId": await app_id_of(app), "userId": user_id})


@server.tool(**DESTRUCTIVE("identity"))
async def unlink_account(mapping_id: Annotated[int, Field(gt=0, description="映射 ID（identity_get 的 mappings 里）")]) -> Any:
    """解除一条应用账号与全局身份的映射。"""
    return await MAPPING_DELETE(id=mapping_id)


@server.tool(**WRITE("identity", idempotent=True))
async def sync(app: AppRef, user_id: Annotated[int | None, Field(gt=0, description="只同步这个用户；不填则整个应用批量同步")] = None) -> Any:
    """把应用用户同步进用户主档（建身份或更新映射）。整应用同步是后台批量任务。"""
    app_id = await app_id_of(app)
    if user_id:
        return await SYNC(body={"appId": app_id, "userId": user_id})
    return await SYNC_BATCH(body={"appId": app_id})


@server.tool(**DESTRUCTIVE("identity"))
async def merge(primary_id: Annotated[int, Field(gt=0, description="保留的身份")], merged_id: Annotated[int, Field(gt=0, description="被并入后消失的身份")]) -> Any:
    """合并两个全局身份（被并入的身份的映射、标签转到保留身份，不可撤销）。先向用户确认。"""
    if primary_id == merged_id:
        raise ToolError("不能把身份合并到自己")
    return await MERGE(body={"primaryId": primary_id, "mergedId": merged_id})


@server.tool(**READ("identity"))
async def merges() -> Any:
    """身份合并历史。"""
    return await MERGES()


# ── 标签与分群 ──

@server.tool(**READ("identity"))
async def tags() -> Any:
    """全部身份标签。"""
    return await TAGS()


@server.tool(**WRITE("identity"))
async def manage_tag(
    action: Annotated[Literal["create", "delete", "assign", "remove"], Field(description="create 新建 / delete 删除标签 / assign 打标 / remove 摘标")],
    name: Annotated[str, Field(description="新建时的标签名")] = "",
    color: str = "",
    description: str = "",
    tag_id: Annotated[int | None, Field(gt=0)] = None,
    identity_id: Annotated[int | None, Field(gt=0, description="打标 / 摘标的身份")] = None,
) -> Any:
    """管理身份标签：新建、删除，给身份打标或摘标。"""
    if action == "create":
        if not name:
            raise ToolError("新建标签需要 name")
        return await TAG_CREATE(body=body(name=name, color=color, description=description))
    if not tag_id:
        raise ToolError("需要 tag_id")
    if action == "delete":
        return await TAG_DELETE(id=tag_id)
    if not identity_id:
        raise ToolError("打标 / 摘标需要 identity_id")
    route = TAG_ASSIGN if action == "assign" else TAG_REMOVE
    return await route(body={"identityId": identity_id, "tagId": tag_id})


@server.tool(**READ("identity"))
async def segments(segment_id: Annotated[int | None, Field(gt=0, description="给 ID 时列该分群成员")] = None, page: Page = 1, limit: Limit = 20) -> Any:
    """分群列表，或某个分群的成员。"""
    if segment_id:
        return await SEGMENT_MEMBERS(id=segment_id, query=q(page=page, limit=limit))
    return await SEGMENTS()


@server.tool(**WRITE("identity"))
async def save_segment(
    name: Annotated[str, Field(min_length=1)],
    segment_id: Annotated[int | None, Field(gt=0, description="给 ID 即修改，不给即新建")] = None,
    description: str = "",
    segment_type: Annotated[Literal["static", "dynamic"], Field(description="static 手工成员 / dynamic 按规则（仅新建时）")] = "static",
    rules: Annotated[dict[str, Any] | None, Field(description="动态分群规则")] = None,
) -> Any:
    """新建或修改分群。"""
    if segment_id:
        return await SEGMENT_UPDATE(id=segment_id, body=body(name=name, description=description, rules=rules))
    return await SEGMENT_CREATE(body=body(name=name, description=description, segmentType=segment_type, rules=rules))


@server.tool(**WRITE("identity"))
async def segment_member(
    segment_id: Annotated[int, Field(gt=0)],
    identity_id: IdentityID,
    action: Annotated[Literal["add", "remove"], Field(description="加入 / 移出")] = "add",
) -> Any:
    """把身份加入或移出静态分群。"""
    if action == "add":
        return await SEGMENT_MEMBER_ADD(id=segment_id, body={"identityId": identity_id})
    return await SEGMENT_MEMBER_REMOVE(id=segment_id, identityId=identity_id)


@server.tool(**DESTRUCTIVE("identity"))
async def delete_segment(segment_id: Annotated[int, Field(gt=0)]) -> Any:
    """删除分群。"""
    return await SEGMENT_DELETE(id=segment_id)


# ── 黑白名单 ──

@server.tool(name="lists", **READ("identity"))
async def list_entries(list_type: Annotated[Literal["", "blacklist", "whitelist"], Field(description="名单类型")] = "", page: Page = 1, limit: Limit = 20) -> Any:
    """主档黑白名单条目。"""
    return await LISTS(query=q(listType=list_type, page=page, limit=limit))


@server.tool(**WRITE("identity"))
async def add_list_entry(
    list_type: Annotated[Literal["blacklist", "whitelist"], Field(description="名单类型")],
    reason: Reason,
    identity_id: Annotated[int | None, Field(gt=0)] = None,
    email: str = "",
    phone: str = "",
    ip: str = "",
    expires_at: IsoTime = "",
) -> Any:
    """加入黑名单或白名单（按身份、邮箱、手机号或 IP 之一）。"""
    if not (identity_id or email or phone or ip):
        raise ToolError("需要 identity_id、email、phone、ip 之一")
    return await LIST_CREATE(body=body(listType=list_type, reason=reason, identityId=identity_id, email=email or None,
                                       phone=phone or None, ip=ip or None, expiresAt=expires_at or None))


@server.tool(**DESTRUCTIVE("identity"))
async def remove_list_entry(entry_id: Annotated[int, Field(gt=0)]) -> Any:
    """移除一条黑白名单条目。"""
    return await LIST_DELETE(id=entry_id)


@server.tool(**WRITE("identity", idempotent=True))
async def check_list(email: str = "", phone: str = "", ip: str = "") -> Any:
    """检查邮箱、手机号或 IP 是否命中黑名单。"""
    if not (email or phone or ip):
        raise ToolError("需要 email、phone、ip 之一")
    return await LIST_CHECK(body=body(email=email or None, phone=phone or None, ip=ip or None))


# ── 申诉与注销 ──

@server.tool(**READ("identity"))
async def appeals(status: Annotated[Literal["", "pending", "approved", "rejected", "cancelled"], Field(description="申诉状态")] = "", page: Page = 1, limit: Limit = 20) -> Any:
    """身份申诉列表。"""
    return await APPEALS(query=q(status=status, page=page, limit=limit))


@server.tool(**WRITE("identity"))
async def appeal(
    action: Annotated[Literal["create", "approved", "rejected"], Field(description="create 代用户提交 / approved 通过 / rejected 驳回")],
    appeal_id: Annotated[int | None, Field(gt=0, description="审核时的申诉 ID")] = None,
    identity_id: Annotated[int | None, Field(gt=0, description="提交时的身份")] = None,
    appeal_type: str = "",
    reason: str = "",
    evidence: str = "",
    comment: Annotated[str, Field(description="审核意见")] = "",
) -> Any:
    """提交或审核身份申诉。"""
    if action == "create":
        if not (identity_id and appeal_type and reason):
            raise ToolError("提交申诉需要 identity_id、appeal_type、reason")
        return await APPEAL_CREATE(body=body(identityId=identity_id, appealType=appeal_type, reason=reason, evidence=evidence or None))
    if not appeal_id:
        raise ToolError("审核需要 appeal_id")
    return await APPEAL_REVIEW(id=appeal_id, body=body(action=action, comment=comment))


@server.tool(**READ("identity"))
async def deactivations() -> Any:
    """待执行的身份注销请求。"""
    return await DEACTIVATIONS()


@server.tool(**DESTRUCTIVE("identity"))
async def deactivate(
    identity_id: IdentityID,
    reason: Reason,
    cooling_days: Annotated[int, Field(ge=0, le=180, description="冷静期天数，0 为立即")] = 7,
) -> Any:
    """发起身份注销（冷静期满后执行，期间可撤销）。先向用户确认。"""
    return await DEACTIVATION_CREATE(body={"identityId": identity_id, "reason": reason, "coolingDays": cooling_days})


@server.tool(**WRITE("identity"))
async def cancel_deactivation(deactivation_id: Annotated[int, Field(gt=0)]) -> Any:
    """撤销一个尚未执行的身份注销。"""
    return await DEACTIVATION_CANCEL(id=deactivation_id)


# ── 用户设置 ──

@server.tool(**READ("identity"))
async def user_settings(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="给出时看这个用户的全部设置；不给则看应用的设置统计")] = None,
    check_integrity: Annotated[bool, Field(description="改为做一次完整性体检（只检查不修复；修复用 identity_initialize_settings(repair=true)）")] = False,
) -> Any:
    """用户个人设置：查某个用户的设置、应用级统计，或做完整性体检。"""
    app_id = await app_id_of(app)
    if check_integrity:
        return await SETTINGS_INTEGRITY(query={"appid": app_id})
    if user_id:
        return await SETTINGS_USER(query={"appid": app_id, "userId": user_id})
    return await SETTINGS_STATS(query={"appid": app_id})


@server.tool(**WRITE("identity", idempotent=True))
async def initialize_settings(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="只初始化这个用户；不填则整个应用批量初始化")] = None,
    categories: Annotated[list[str] | None, Field(description="只初始化这些分类")] = None,
    batch_size: Annotated[int | None, Field(gt=0, le=5000, description="批量初始化每批人数")] = None,
    repair: Annotated[bool, Field(description="改为执行完整性体检并自动修复缺失的默认设置")] = False,
) -> Any:
    """为用户补齐默认个人设置（已有的不覆盖），或体检并自动修复。"""
    app_id = await app_id_of(app)
    if repair:
        return await SETTINGS_INTEGRITY(query={"appid": app_id, "autoRepair": "true"})
    if user_id:
        return await SETTINGS_INIT_USER(body=body(appid=app_id, userId=user_id, categories=categories))
    return await SETTINGS_BATCH_INIT(body=body(appid=app_id, categories=categories, batchSize=batch_size))


@server.tool(**DESTRUCTIVE("identity"))
async def cleanup_settings(app: AppRef, dry_run: Annotated[bool, Field(description="只统计不删除")] = True) -> Any:
    """清理应用里孤立 / 无效的用户设置。默认只演练，确认后再 dry_run=false。"""
    return await SETTINGS_CLEANUP(body={"AppID": await app_id_of(app), "DryRun": dry_run})
