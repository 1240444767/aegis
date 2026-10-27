"""风控与防火墙：风控大盘、规则、处置策略、名单、评估记录与复核、设备 / IP / 账号画像、规则模板，以及 IP 封禁、地域封禁、地理围栏与防火墙日志。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, PATCH, POST, PUT, READ, WRITE, IsoTime, Page, app_id_of, body, q

NAMESPACE = "risk"
server = FastMCP("risk")

BASE = "/api/admin/system/risk"
FW = "/api/admin/system/firewall"

# ── 风控中心 ──
METADATA = GET(f"{BASE}/metadata")
EXPR_VALIDATE = POST(f"{BASE}/expression/validate")
DASHBOARD = GET(f"{BASE}/dashboard")
EVALUATE = POST(f"{BASE}/evaluate")
SIMULATE = POST(f"{BASE}/simulate")
RULES = GET(f"{BASE}/rules")
RULE = GET(f"{BASE}/rules/:id")
RULE_CREATE = POST(f"{BASE}/rules")
RULE_UPDATE = PUT(f"{BASE}/rules/:id")
RULE_DELETE = DELETE(f"{BASE}/rules/:id")
RULE_SIMULATE = POST(f"{BASE}/rules/:id/simulate")
ASSESSMENTS = GET(f"{BASE}/assessments")
ASSESSMENT = GET(f"{BASE}/assessments/:id")
ASSESSMENTS_PURGE = DELETE(f"{BASE}/assessments")
ASSESSMENT_REVIEW = POST(f"{BASE}/assessments/:id/review")
ASSESSMENT_REPLAY = POST(f"{BASE}/assessments/:id/replay")
PENDING_REVIEWS = GET(f"{BASE}/reviews/pending")
DEVICES = GET(f"{BASE}/devices")
DEVICES_SUSPICIOUS = GET(f"{BASE}/devices/suspicious")
DEVICE = GET(f"{BASE}/devices/:deviceId")
DEVICE_TAG = PUT(f"{BASE}/devices/:id/tag")
IPS = GET(f"{BASE}/ips")
IP = GET(f"{BASE}/ips/:ip")
IP_REFRESH = POST(f"{BASE}/ips/:ip/refresh")
IP_TAG = PUT(f"{BASE}/ips/:id/tag")
ACTIONS = GET(f"{BASE}/actions")
ACTION_CREATE = POST(f"{BASE}/actions")
ACTION_UPDATE = PUT(f"{BASE}/actions/:id")
ACTION_DELETE = DELETE(f"{BASE}/actions/:id")
LISTS = GET(f"{BASE}/lists")
LISTS_CREATE = POST(f"{BASE}/lists")
LISTS_BATCH_DELETE = POST(f"{BASE}/lists/batch-delete")
LISTS_CHECK = POST(f"{BASE}/lists/check")
LIST_UPDATE = PUT(f"{BASE}/lists/:id")
LIST_DELETE = DELETE(f"{BASE}/lists/:id")
ACCOUNTS = GET(f"{BASE}/accounts")
ACCOUNT = GET(f"{BASE}/accounts/detail")
DISPOSE = POST(f"{BASE}/dispose")
TEMPLATES = GET(f"{BASE}/templates")
TEMPLATE_APPLY = POST(f"{BASE}/templates/apply")

# ── 防火墙 ──
FW_LOGS = GET(f"{FW}/logs")
FW_LOG = GET(f"{FW}/logs/:logId")
FW_LOGS_CLEANUP = DELETE(f"{FW}/logs")
FW_STATS = GET(f"{FW}/stats")
IP_BANS = GET(f"{FW}/bans")
IP_BAN_MODES = GET(f"{FW}/bans/modes")
IP_BAN = POST(f"{FW}/bans")
IP_UNBAN = DELETE(f"{FW}/bans/:banId")
GEO_BANS = GET(f"{FW}/geo-bans")
GEO_BAN_UPSERT = POST(f"{FW}/geo-bans")
GEO_BAN_TOGGLE = PATCH(f"{FW}/geo-bans/:id")
GEO_BAN_DELETE = DELETE(f"{FW}/geo-bans/:id")
GEO_FENCES = GET(f"{FW}/geo-fences")
GEO_FENCE_CREATE = POST(f"{FW}/geo-fences")
GEO_FENCE_PREVIEW = POST(f"{FW}/geo-fences/preview")
GEO_FENCE_UPDATE = PUT(f"{FW}/geo-fences/:id")
GEO_FENCE_TOGGLE = PATCH(f"{FW}/geo-fences/:id")
GEO_FENCE_DELETE = DELETE(f"{FW}/geo-fences/:id")

OptionalApp = Annotated[str, Field(description="限定应用：appKey 或数字应用 ID；留空为全平台 / 不限")]
PageSize = Annotated[int, Field(ge=1, le=200, description="每页条数（风控接口用 pageSize）")]
Scene = Literal[
    "register", "login", "password_reset", "verify_code", "payment", "invite", "lottery",
    "signin", "cardkey", "points_redeem", "ad_reward", "api", "*",
]
SceneArg = Annotated[Scene, Field(description="场景；* 为全部场景")]
SceneFilter = Annotated[Literal["", "register", "login", "password_reset", "verify_code", "payment", "invite", "lottery", "signin", "cardkey", "points_redeem", "ad_reward", "api", "*"], Field(description="按场景过滤")]
Action = Literal["pass", "captcha", "review", "block", "ban"]
ListKind = Literal["block", "allow", "watch"]
ListType = Literal["ip", "cidr", "device", "account", "user", "email_domain", "phone_prefix", "asn", "country", "ua_keyword"]
BanTarget = Literal["account", "device", "ip", "firewall"]
EntityTag = Literal["normal", "suspicious", "blocked", "trusted", "proxy", "vpn", "tor", "datacenter", "bot"]
RecordID = Annotated[int, Field(gt=0, description="记录 ID")]
DurationSeconds = Annotated[int, Field(ge=0, description="时长（秒），0 为永久")]


async def _app(app: str) -> int | None:
    return await app_id_of(app) if app and app.strip() else None


# ═══════════════════════ 大盘、元数据、评估 ═══════════════════════


@server.tool(**READ("risk"))
async def metadata() -> Any:
    """风控元数据：全部场景、条件类型（含参数结构与维度）、表达式变量与函数、处置动作、名单类型、标签等。写规则 / 名单前先看它。"""
    return await METADATA()


@server.tool(**WRITE("risk", idempotent=True))
async def validate_expression(expression: Annotated[str, Field(min_length=1, description="自定义规则表达式（custom_expr 条件用）")]) -> Any:
    """校验一条风控表达式的语法与变量（不保存）。"""
    return await EXPR_VALIDATE(body={"expression": expression})


@server.tool(**READ("risk"))
async def dashboard(
    app: OptionalApp = "",
    start: Annotated[IsoTime, Field(description="起始时间（RFC 3339），默认 7 天前")] = "",
    end: Annotated[IsoTime, Field(description="结束时间（RFC 3339），默认现在")] = "",
) -> Any:
    """风控大盘：评估量、各等级 / 动作 / 场景分布、命中最多的规则、趋势等。"""
    return await DASHBOARD(query=q(appId=await _app(app), start=start, end=end))


def _eval_body(scene: str, app_id: int | None, user_id: int | None, account: str, ip: str, device_id: str, user_agent: str) -> dict[str, Any]:
    return body(
        scene=scene, appId=app_id, userId=user_id, account=account or None, ip=ip or None,
        deviceId=device_id or None, userAgent=user_agent or None,
    )


@server.tool(**WRITE("risk", idempotent=True))
async def simulate(
    scene: SceneArg = "login",
    app: OptionalApp = "",
    rule_id: Annotated[int | None, Field(gt=0, description="只跑这一条已保存的规则")] = None,
    rule_ids: Annotated[list[int] | None, Field(description="只跑这些规则（与 rule_id 二选一）")] = None,
    draft: Annotated[dict[str, Any] | None, Field(description="未保存的规则草稿 {name, scene, conditionType, conditionData, score, mode, decision}，先试再存")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    account: Annotated[str, Field(description="账号")] = "",
    ip: Annotated[str, Field(description="IP")] = "",
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    user_agent: Annotated[str, Field(description="User-Agent")] = "",
    captcha_passed: Annotated[bool, Field(description="假设已通过图形验证码")] = False,
    overrides: Annotated[dict[str, Any] | None, Field(description="覆盖环境变量（构造情报源里没有的组合，变量名见 risk_metadata）")] = None,
) -> Any:
    """模拟一次风控评估：给出得分、命中规则、名单命中与处置，不落库、不执行封禁。改规则前先用它验证。"""
    if rule_id and rule_ids:
        raise ToolError("rule_id 与 rule_ids 只能二选一")
    payload = _eval_body(scene, await _app(app), user_id, account, ip, device_id, user_agent)
    payload["captchaPassed"] = captcha_passed
    payload.update(body(ruleIds=rule_ids or None, draft=draft, overrides=overrides))
    if rule_id:
        return await RULE_SIMULATE(id=rule_id, body=payload)
    return await SIMULATE(body=payload)


@server.tool(**DESTRUCTIVE("risk"))
async def evaluate(
    scene: SceneArg,
    app: OptionalApp = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    account: Annotated[str, Field(description="账号")] = "",
    ip: Annotated[str, Field(description="IP")] = "",
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    user_agent: Annotated[str, Field(description="User-Agent")] = "",
    extra: Annotated[dict[str, Any] | None, Field(description="附加上下文（如 payment 场景的 amount）")] = None,
) -> Any:
    """按线上流程真实评估一次：处置为 ban 时会当场执行封禁（账号 / 设备 / IP / 防火墙）并写评估记录。只想看结果用 risk_simulate。"""
    payload = _eval_body(scene, await _app(app), user_id, account, ip, device_id, user_agent)
    if extra:
        payload["extra"] = extra
    return await EVALUATE(body=payload)


# ═══════════════════════ 规则 ═══════════════════════


@server.tool(**READ("risk"))
async def rules(
    rule_id: Annotated[int | None, Field(gt=0, description="给了就取这一条规则的详情（含区间内命中统计）")] = None,
    scene: SceneFilter = "",
    app: OptionalApp = "",
    platform_only: Annotated[bool, Field(description="只看全平台规则（不含任何应用的）")] = False,
    start: Annotated[IsoTime, Field(description="详情统计起始（RFC 3339），默认 7 天前")] = "",
    end: Annotated[IsoTime, Field(description="详情统计结束（RFC 3339）")] = "",
) -> Any:
    """风控规则列表（给 app 时为该应用规则 + 全平台规则），或给 rule_id 取一条详情与命中统计。"""
    if rule_id:
        return await RULE(id=rule_id, query=q(start=start, end=end))
    app_id = 0 if platform_only else await _app(app)
    return await RULES(query=q(scene=scene, appId=app_id))


@server.tool(**WRITE("risk"))
async def create_rule(
    name: Annotated[str, Field(min_length=1, description="规则名称")],
    scene: SceneArg,
    condition_type: Annotated[str, Field(min_length=1, description="条件类型（如 ip_frequency、device_new、geo_anomaly、custom_expr 等，见 risk_metadata）")],
    condition_data: Annotated[dict[str, Any] | None, Field(description="条件参数（结构随条件类型而定，见 risk_metadata）")] = None,
    score: Annotated[int, Field(ge=0, le=1000, description="命中加分")] = 20,
    priority: Annotated[int, Field(description="优先级（大的先跑）")] = 0,
    mode: Annotated[Literal["enforce", "observe"], Field(description="enforce 计分生效、observe 只留痕不计分（新规则建议先观察）")] = "enforce",
    decision: Annotated[Literal["", "pass", "captcha", "review", "block", "ban"], Field(description="命中即强制的处置；留空只计分")] = "",
    description: Annotated[str, Field(description="说明")] = "",
    is_active: Annotated[bool, Field(description="是否启用")] = True,
    app: OptionalApp = "",
) -> Any:
    """新建风控规则（立即对线上评估生效）。建议先 risk_simulate 用 draft 试跑，或以 observe 模式上线。"""
    return await RULE_CREATE(body=body(
        appId=await _app(app), name=name, description=description, scene=scene, conditionType=condition_type,
        conditionData=condition_data or {}, score=score, priority=priority, isActive=is_active, mode=mode, decision=decision,
    ))


@server.tool(**WRITE("risk", idempotent=True))
async def update_rule(
    rule_id: Annotated[int, Field(gt=0, description="规则 ID")],
    name: Annotated[str | None, Field(description="名称")] = None,
    description: Annotated[str | None, Field(description="说明")] = None,
    scene: Annotated[Scene | None, Field(description="场景")] = None,
    condition_type: Annotated[str | None, Field(description="条件类型")] = None,
    condition_data: Annotated[dict[str, Any] | None, Field(description="条件参数（整体替换）")] = None,
    score: Annotated[int | None, Field(ge=0, le=1000, description="命中加分")] = None,
    priority: Annotated[int | None, Field(description="优先级")] = None,
    mode: Annotated[Literal["enforce", "observe"] | None, Field(description="enforce / observe")] = None,
    decision: Annotated[Literal["", "pass", "captcha", "review", "block", "ban"] | None, Field(description="强制处置；空串为取消")] = None,
    is_active: Annotated[bool | None, Field(description="启用 / 停用")] = None,
    app: Annotated[str | None, Field(description="改作用域：appKey 或数字应用 ID；传 0 改为全平台；不传不改")] = None,
) -> Any:
    """修改风控规则（只传要改的字段），立即对线上评估生效。"""
    app_id: int | None = None
    if app is not None:
        app_id = 0 if app.strip() in ("", "0") else await app_id_of(app)
    payload = body(
        name=name, description=description, scene=scene, conditionType=condition_type, conditionData=condition_data,
        score=score, priority=priority, mode=mode, decision=decision, isActive=is_active, appId=app_id,
    )
    if not payload:
        raise ToolError("没有要修改的字段")
    return await RULE_UPDATE(id=rule_id, body=payload)


@server.tool(**DESTRUCTIVE("risk"))
async def delete_rule(rule_id: Annotated[int, Field(gt=0, description="规则 ID")]) -> Any:
    """删除一条风控规则（不可恢复；只想暂停用 risk_update_rule is_active=false）。"""
    return await RULE_DELETE(id=rule_id)


# ═══════════════════════ 处置策略 ═══════════════════════


@server.tool(**READ("risk"))
async def actions(scene: SceneFilter = "", app: OptionalApp = "") -> Any:
    """分数处置策略（分数区间 → 放行 / 验证码 / 复核 / 拦截 / 封禁）。按「应用+场景 → 应用+全部 → 全平台+场景 → 全平台+全部」取第一层。"""
    return await ACTIONS(query=q(scene=scene, appId=await _app(app)))


@server.tool(**WRITE("risk"))
async def create_action(
    scene: SceneArg,
    action: Annotated[Action, Field(description="处置：pass 放行、captcha 验证码、review 人工复核、block 拦截、ban 封禁")],
    min_score: Annotated[int, Field(ge=0, description="分数下限（含）")] = 0,
    max_score: Annotated[int | None, Field(ge=0, description="分数上限；留空为不封顶")] = None,
    ban_duration: Annotated[int, Field(ge=0, description="ban 时的封禁时长（秒），0 为永久")] = 0,
    ban_targets: Annotated[list[BanTarget] | None, Field(description="ban 时封禁的对象")] = None,
    description: Annotated[str, Field(description="说明")] = "",
    is_active: Annotated[bool, Field(description="是否启用")] = True,
    app: OptionalApp = "",
) -> Any:
    """新建分数处置策略（立即对线上评估生效；区间与同层已有策略冲突会被拒）。"""
    return await ACTION_CREATE(body=body(
        appId=await _app(app), scene=scene, minScore=min_score, maxScore=max_score, action=action,
        banDuration=ban_duration, banTargets=ban_targets, description=description, isActive=is_active,
    ))


@server.tool(**WRITE("risk", idempotent=True))
async def update_action(
    action_id: Annotated[int, Field(gt=0, description="处置策略 ID")],
    action: Annotated[Action | None, Field(description="处置")] = None,
    min_score: Annotated[int | None, Field(ge=0, description="分数下限")] = None,
    max_score: Annotated[int | None, Field(ge=0, description="分数上限")] = None,
    ban_duration: Annotated[int | None, Field(ge=0, description="封禁时长（秒）")] = None,
    ban_targets: Annotated[list[BanTarget] | None, Field(description="封禁对象（整体替换）")] = None,
    description: Annotated[str | None, Field(description="说明")] = None,
    is_active: Annotated[bool | None, Field(description="启用 / 停用")] = None,
) -> Any:
    """修改分数处置策略（只传要改的字段）。"""
    payload = body(
        action=action, minScore=min_score, maxScore=max_score, banDuration=ban_duration,
        banTargets=ban_targets, description=description, isActive=is_active,
    )
    if not payload:
        raise ToolError("没有要修改的字段")
    return await ACTION_UPDATE(id=action_id, body=payload)


@server.tool(**DESTRUCTIVE("risk"))
async def delete_action(action_id: Annotated[int, Field(gt=0, description="处置策略 ID")]) -> Any:
    """删除一条分数处置策略（不可恢复）。"""
    return await ACTION_DELETE(id=action_id)


# ═══════════════════════ 名单 ═══════════════════════


@server.tool(name="lists", **READ("risk"))
async def list_entries(
    list_type: Annotated[Literal["", "ip", "cidr", "device", "account", "user", "email_domain", "phone_prefix", "asn", "country", "ua_keyword"], Field(description="名单类型")] = "",
    kind: Annotated[Literal["", "block", "allow", "watch"], Field(description="block 黑名单、allow 白名单、watch 观察名单")] = "",
    keyword: Annotated[str, Field(description="关键字（对象值、原因）")] = "",
    source: Annotated[str, Field(description="来源（如 manual、risk、review）")] = "",
    active: Annotated[bool | None, Field(description="只看生效中（true）或已过期（false）")] = None,
    app: OptionalApp = "",
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """风控名单（黑 / 白 / 观察 × 十种对象类型）。"""
    return await LISTS(query=q(
        appId=await _app(app), listType=list_type, kind=kind, keyword=keyword, source=source,
        active=active, page=page, pageSize=page_size,
    ))


@server.tool(**DESTRUCTIVE("risk"))
async def add_list(
    list_type: Annotated[ListType, Field(description="名单类型：ip、cidr 网段、device、account、user 用户 ID、email_domain、phone_prefix、asn、country、ua_keyword")],
    values: Annotated[list[str], Field(min_length=1, description="对象值，一项一个；已在名单里的对象会被改写")],
    kind: Annotated[ListKind, Field(description="block 直接拦、allow 直接放、watch 交给观察名单规则加分")],
    scenes: Annotated[list[str] | None, Field(description="只在这些场景生效，留空为全部场景")] = None,
    reason: Annotated[str, Field(description="原因")] = "",
    expires_at: Annotated[IsoTime, Field(description="到期时间（RFC 3339），留空为永久")] = "",
    app: OptionalApp = "",
) -> Any:
    """批量写入风控名单。黑名单立即拦截对应的登录 / 注册 / 支付等请求，执行前先向用户确认对象。"""
    return await LISTS_CREATE(body=body(
        appId=await _app(app), listType=list_type, values=values, kind=kind,
        scenes=scenes, reason=reason, expiresAt=expires_at,
    ))


@server.tool(**WRITE("risk", idempotent=True))
async def update_list(
    entry_id: Annotated[int, Field(gt=0, description="名单条目 ID")],
    kind: Annotated[ListKind | None, Field(description="改成黑 / 白 / 观察")] = None,
    scenes: Annotated[list[str] | None, Field(description="生效场景（整体替换；空列表为全部场景）")] = None,
    reason: Annotated[str | None, Field(description="原因")] = None,
    expires_at: Annotated[str | None, Field(description="到期时间（RFC 3339）；空串改为永久")] = None,
) -> Any:
    """修改一条风控名单（只传要改的字段）。"""
    payload = body(kind=kind, scenes=scenes, reason=reason, expiresAt=expires_at)
    if not payload:
        raise ToolError("没有要修改的字段")
    return await LIST_UPDATE(id=entry_id, body=payload)


@server.tool(**DESTRUCTIVE("risk"))
async def delete_lists(entry_ids: Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, description="名单条目 ID 列表")]) -> Any:
    """删除风控名单条目（一条或多条，不可恢复）。删除黑名单即解除拦截。"""
    if len(entry_ids) == 1:
        return await LIST_DELETE(id=entry_ids[0])
    return await LISTS_BATCH_DELETE(body={"ids": entry_ids})


@server.tool(**WRITE("risk", idempotent=True))
async def check_lists(
    scene: SceneFilter = "",
    app: OptionalApp = "",
    ip: Annotated[str, Field(description="IP")] = "",
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    account: Annotated[str, Field(description="账号")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    user_agent: Annotated[str, Field(description="User-Agent")] = "",
) -> Any:
    """检测一组对象命中了哪些名单（不评估规则）。"""
    if not any((ip, device_id, account, user_id, user_agent)):
        raise ToolError("至少给 ip / device_id / account / user_id / user_agent 之一")
    return await LISTS_CHECK(body=body(
        appId=await _app(app), scene=scene or None, ip=ip or None, deviceId=device_id or None,
        account=account or None, userId=user_id, userAgent=user_agent or None,
    ))


# ═══════════════════════ 评估记录与复核 ═══════════════════════


@server.tool(**READ("risk"))
async def assessments(
    assessment_id: Annotated[int | None, Field(gt=0, description="给了就取这一条评估记录的详情（环境、命中、执行结果）")] = None,
    app: OptionalApp = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    scene: SceneFilter = "",
    risk_level: Annotated[Literal["", "normal", "low", "medium", "high", "critical"], Field(description="风险等级")] = "",
    action: Annotated[Literal["", "pass", "captcha", "review", "block", "ban"], Field(description="处置动作")] = "",
    decision_by: Annotated[Literal["", "allowlist", "blocklist", "rule", "policy", "none"], Field(description="决定来源")] = "",
    dimension: Annotated[str, Field(description="命中维度（network / device / account / behavior / geo / client / list / custom）")] = "",
    ip: Annotated[str, Field(description="IP")] = "",
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    account: Annotated[str, Field(description="账号")] = "",
    keyword: Annotated[str, Field(description="关键字")] = "",
    rule_id: Annotated[int | None, Field(gt=0, description="命中过这条规则")] = None,
    reviewed: Annotated[bool | None, Field(description="已复核（true）或未复核（false）")] = None,
    min_score: Annotated[int | None, Field(ge=0, description="最低分")] = None,
    max_score: Annotated[int | None, Field(ge=0, description="最高分")] = None,
    start: Annotated[IsoTime, Field(description="起始时间（RFC 3339）")] = "",
    end: Annotated[IsoTime, Field(description="结束时间（RFC 3339）")] = "",
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """风控评估记录（排查起点：按 IP / 设备 / 账号 / 规则 / 等级 / 动作 / 时间筛），或给 assessment_id 取详情。"""
    if assessment_id:
        return await ASSESSMENT(id=assessment_id)
    return await ASSESSMENTS(query=q(
        appId=await _app(app), userId=user_id, decisionBy=decision_by, dimension=dimension, scene=scene,
        riskLevel=risk_level, action=action, ip=ip, deviceId=device_id, account=account, keyword=keyword,
        ruleId=rule_id, reviewed=reviewed, minScore=min_score, maxScore=max_score, start=start, end=end,
        page=page, pageSize=page_size,
    ))


@server.tool(**READ("risk"))
async def pending_reviews(app: OptionalApp = "", page: Page = 1, page_size: PageSize = 20) -> Any:
    """等待人工复核的评估记录。"""
    return await PENDING_REVIEWS(query=q(appId=await _app(app), page=page, pageSize=page_size))


@server.tool(**DESTRUCTIVE("risk"))
async def review(
    assessment_id: Annotated[int, Field(gt=0, description="评估记录 ID")],
    result: Annotated[Literal["approved", "rejected"], Field(description="approved 通过（判定为误报）、rejected 驳回（确认有风险并处置）")],
    comment: Annotated[str, Field(description="复核意见")] = "",
    targets: Annotated[list[str] | None, Field(description="通过时可含 allow 加白；驳回时处置对象（account / device / ip / firewall），默认设备 + IP")] = None,
    duration: DurationSeconds = 0,
    revoke_auto: Annotated[bool, Field(description="通过时撤销这条记录触发的自动封禁（名单、账号封禁、防火墙封禁）")] = False,
) -> Any:
    """复核一条评估记录：驳回会按所选对象拉黑 / 封禁，通过可加白并撤回自动封禁。执行前先向用户确认。"""
    return await ASSESSMENT_REVIEW(id=assessment_id, body=body(
        result=result, comment=comment, targets=targets, duration=duration, revokeAuto=revoke_auto,
    ))


@server.tool(**WRITE("risk", idempotent=True))
async def replay(assessment_id: Annotated[int, Field(gt=0, description="评估记录 ID")]) -> Any:
    """用当前规则与策略重放一条历史评估（基于当时记下的环境），对比结论是否变化。不落库、不执行。"""
    return await ASSESSMENT_REPLAY(id=assessment_id)


@server.tool(**DESTRUCTIVE("risk"))
async def purge_assessments(days: Annotated[int, Field(ge=1, description="删除 N 天前的评估记录")]) -> Any:
    """清理 N 天前的风控评估记录（不可恢复）。仅超级管理员，执行前先向用户确认。"""
    return await ASSESSMENTS_PURGE(body={"days": days})


# ═══════════════════════ 设备、IP、账号画像 ═══════════════════════


@server.tool(**READ("risk"))
async def devices(
    device_id: Annotated[str, Field(description="给了就取这台设备的指纹档案详情")] = "",
    suspicious: Annotated[bool, Field(description="只列可疑设备（多账号共用、频繁换 IP 等）")] = False,
    app: OptionalApp = "",
    keyword: Annotated[str, Field(description="关键字")] = "",
    tag: Annotated[Literal["", "normal", "suspicious", "blocked", "trusted", "proxy", "vpn", "tor", "datacenter", "bot"], Field(description="风险标签")] = "",
    only_risk: Annotated[bool, Field(description="只看非正常标签")] = False,
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """设备指纹档案列表 / 可疑设备 / 单台设备详情。列表里的数字 id 用于 risk_tag_device。"""
    if device_id:
        return await DEVICE(deviceId=device_id)
    if suspicious:
        return await DEVICES_SUSPICIOUS(query=q(appId=await _app(app), page=page, pageSize=page_size))
    return await DEVICES(query=q(
        appId=await _app(app), keyword=keyword, tag=tag, onlyRisk=True if only_risk else None, page=page, pageSize=page_size,
    ))


@server.tool(**WRITE("risk", idempotent=True))
async def tag_device(
    record_id: Annotated[int, Field(gt=0, description="设备档案的数字 id（risk_devices 列表里的 id，不是设备 ID 字符串）")],
    tag: Annotated[EntityTag, Field(description="风险标签；blocked / trusted 会同步写入全平台黑 / 白名单")],
    note: Annotated[str, Field(description="备注")] = "",
) -> Any:
    """给设备档案打风险标签（与全平台设备名单双向同步，判定只认名单）。仅超级管理员。"""
    return await DEVICE_TAG(id=record_id, body={"tag": tag, "note": note})


@server.tool(**READ("risk"))
async def ips(
    ip: Annotated[str, Field(description="给了就取这个 IP 的情报详情（信誉、代理 / 机房、归属地、相关评估）")] = "",
    app: OptionalApp = "",
    keyword: Annotated[str, Field(description="关键字")] = "",
    tag: Annotated[Literal["", "normal", "suspicious", "blocked", "trusted", "proxy", "vpn", "tor", "datacenter", "bot"], Field(description="风险标签")] = "",
    only_risk: Annotated[bool, Field(description="只看非正常标签")] = False,
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """IP 风险档案列表，或单个 IP 的情报详情。列表里的数字 id 用于 risk_tag_ip。"""
    if ip:
        return await IP(ip=ip)
    return await IPS(query=q(
        appId=await _app(app), keyword=keyword, tag=tag, onlyRisk=True if only_risk else None, page=page, pageSize=page_size,
    ))


@server.tool(**WRITE("risk", idempotent=True))
async def refresh_ip(ip: Annotated[str, Field(min_length=1, description="IP 地址")]) -> Any:
    """重新拉取一个 IP 的信誉情报（代理 / 机房 / 威胁评分）。仅超级管理员。"""
    return await IP_REFRESH(ip=ip)


@server.tool(**WRITE("risk", idempotent=True))
async def tag_ip(
    record_id: Annotated[int, Field(gt=0, description="IP 档案的数字 id（risk_ips 列表里的 id，不是 IP 字符串）")],
    tag: Annotated[EntityTag, Field(description="风险标签；blocked / trusted 会同步写入全平台黑 / 白名单")],
    note: Annotated[str, Field(description="备注")] = "",
) -> Any:
    """给 IP 档案打风险标签（与全平台 IP 名单双向同步）。仅超级管理员。"""
    return await IP_TAG(id=record_id, body={"tag": tag, "note": note})


@server.tool(**READ("risk"))
async def accounts(
    account: Annotated[str, Field(description="给了 account 或 user_id 就取这个账号的风险画像详情")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID（详情用）")] = None,
    app: OptionalApp = "",
    keyword: Annotated[str, Field(description="列表关键字")] = "",
    only_risk: Annotated[bool, Field(description="只看有风险记录的账号")] = False,
    days: Annotated[int, Field(ge=1, le=365, description="统计最近 N 天")] = 30,
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """账号风险画像：列表（近 N 天评估次数、高危次数、设备 / IP 扩散），或单个账号的详情。"""
    app_id = await _app(app)
    if account or user_id:
        return await ACCOUNT(query=q(appId=app_id, account=account, userId=user_id))
    return await ACCOUNTS(query=q(
        appId=app_id, keyword=keyword, onlyRisk=True if only_risk else None, days=days, page=page, pageSize=page_size,
    ))


@server.tool(**DESTRUCTIVE("risk"))
async def dispose(
    targets: Annotated[list[BanTarget], Field(min_length=1, description="处置对象：account 账号（应用作用域）、device 设备、ip、firewall 防火墙封 IP（全平台）")],
    kind: Annotated[ListKind, Field(description="block 拉黑、allow 加白、watch 观察")],
    assessment_id: Annotated[int | None, Field(gt=0, description="从这条评估记录取对象（账号 / 设备 / IP）")] = None,
    app: OptionalApp = "",
    user_id: Annotated[int | None, Field(gt=0, description="用户 ID")] = None,
    account: Annotated[str, Field(description="账号")] = "",
    ip: Annotated[str, Field(description="IP")] = "",
    device_id: Annotated[str, Field(description="设备 ID")] = "",
    duration: DurationSeconds = 0,
    reason: Annotated[str, Field(description="原因")] = "",
) -> Any:
    """人工处置：把账号 / 设备 / IP 拉黑、加白或观察（firewall 直接在防火墙封 IP）。立即生效，执行前先向用户确认。"""
    if not any((assessment_id, user_id, account, ip, device_id)):
        raise ToolError("需要 assessment_id / user_id / account / ip / device_id 之一来确定对象")
    return await DISPOSE(body=body(
        assessmentId=assessment_id, appId=await _app(app), userId=user_id, account=account or None, ip=ip or None,
        deviceId=device_id or None, targets=targets, kind=kind, duration=duration, reason=reason,
    ))


# ═══════════════════════ 规则模板 ═══════════════════════


@server.tool(**READ("risk"))
async def templates(app: OptionalApp = "") -> Any:
    """规则模板（基础防护 / 账号安全 / 营销反作弊 / 支付风控）及在该作用域的安装情况。"""
    return await TEMPLATES(query=q(appId=await _app(app)))


@server.tool(**WRITE("risk"))
async def apply_template(
    key: Annotated[str, Field(min_length=1, description="模板 key（risk_templates 结果里的 key）")],
    app: OptionalApp = "",
    observe: Annotated[bool, Field(description="以观察模式安装（只留痕不计分），建议先观察")] = True,
    with_actions: Annotated[bool, Field(description="附带安装模板里的处置策略（区间冲突的跳过）")] = False,
) -> Any:
    """一键安装规则模板（装完即普通规则，重复安装会跳过已装的）。"""
    return await TEMPLATE_APPLY(body=body(key=key, appId=await _app(app), observe=observe, withActions=with_actions))


# ═══════════════════════ 防火墙：IP 封禁 ═══════════════════════


@server.tool(**READ("risk"))
async def ip_bans(
    modes: Annotated[bool, Field(description="改为返回可选的封禁响应模式与默认模式")] = False,
    ip: Annotated[str, Field(description="IP")] = "",
    status: Annotated[Literal["", "all", "active", "expired", "revoked"], Field(description="状态")] = "",
    source: Annotated[str, Field(description="来源（all / manual / auto / risk）")] = "",
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """防火墙 IP 封禁记录，或 modes=true 列出可选的封禁响应模式。仅超级管理员。"""
    if modes:
        return await IP_BAN_MODES()
    return await IP_BANS(query=q(ip=ip, status=status, source=source, page=page, pageSize=page_size))


@server.tool(**DESTRUCTIVE("risk"))
async def ban_ip(
    ip: Annotated[str, Field(min_length=1, description="IP 地址")],
    reason: Annotated[str, Field(description="原因")] = "",
    duration: DurationSeconds = 0,
    mode: Annotated[str, Field(description="封禁响应模式（如 forbidden、silent_drop、tarpit、stealth_404，见 risk_ip_bans modes=true）；留空用全局默认")] = "",
    delay_ms: Annotated[int | None, Field(ge=0, description="tarpit 模式的延迟毫秒数")] = None,
) -> Any:
    """在防火墙封禁一个 IP（全平台所有请求都被拦）。执行前先向用户确认，别封到自己的出口 IP。仅超级管理员。"""
    return await IP_BAN(body=body(ip=ip, reason=reason, duration=duration, mode=mode or None, delayMs=delay_ms))


@server.tool(**WRITE("risk"))
async def unban_ip(ban_id: Annotated[int, Field(gt=0, description="封禁记录 ID（risk_ip_bans 结果里的 id）")]) -> Any:
    """解除一条防火墙 IP 封禁。仅超级管理员。"""
    return await IP_UNBAN(banId=ban_id)


# ═══════════════════════ 防火墙：地域封禁与地理围栏 ═══════════════════════


@server.tool(**READ("risk"))
async def geo_bans() -> Any:
    """地域封禁规则（按国家 / 地区 / 城市 / ASN / 运营商）。仅超级管理员。"""
    return await GEO_BANS()


@server.tool(**DESTRUCTIVE("risk"))
async def save_geo_ban(
    scope_type: Annotated[Literal["country", "region", "city", "asn", "isp"], Field(description="作用域类型")],
    scope_value: Annotated[str, Field(min_length=1, description="作用域取值（国家代码、地区名、城市名、ASN 号、运营商名）")],
    mode: Annotated[str, Field(description="封禁响应模式；留空用平台默认")] = "",
    reason: Annotated[str, Field(description="原因")] = "",
    enabled: Annotated[bool, Field(description="是否启用")] = True,
    expires_at: Annotated[IsoTime, Field(description="到期时间（RFC 3339），留空为永久")] = "",
) -> Any:
    """新建或更新一条地域封禁（同一作用域已存在则覆盖）。会拦掉整片地区的全部请求，执行前先向用户确认。仅超级管理员。"""
    return await GEO_BAN_UPSERT(body=body(
        scopeType=scope_type, scopeValue=scope_value, mode=mode, reason=reason, enabled=enabled, expiresAt=expires_at,
    ))


@server.tool(**WRITE("risk", idempotent=True))
async def toggle_geo_ban(
    geo_ban_id: Annotated[int, Field(gt=0, description="地域封禁规则 ID")],
    enabled: Annotated[bool, Field(description="启用（true）或停用（false）")],
) -> Any:
    """启用或停用一条地域封禁。仅超级管理员。"""
    return await GEO_BAN_TOGGLE(id=geo_ban_id, body={"enabled": enabled})


@server.tool(**DESTRUCTIVE("risk"))
async def delete_geo_ban(geo_ban_id: Annotated[int, Field(gt=0, description="地域封禁规则 ID")]) -> Any:
    """删除一条地域封禁（不可恢复；只想暂停用 risk_toggle_geo_ban）。仅超级管理员。"""
    return await GEO_BAN_DELETE(id=geo_ban_id)


@server.tool(**READ("risk"))
async def geo_fences() -> Any:
    """地理围栏（多边形或圆形区域：deny 区内拦截 / allow 区外拦截 / review 只记录）。仅超级管理员。"""
    return await GEO_FENCES()


def _fence_body(
    name: str, mode: str, app_id: int | None, fence: dict[str, Any] | None, center_lat: float | None,
    center_lng: float | None, radius_m: float | None, ban_mode: str, reason: str, enabled: bool, expires_at: str,
) -> dict[str, Any]:
    circle = (center_lat, center_lng, radius_m)
    if fence is None and any(v is None for v in circle):
        raise ToolError("需要 fence（GeoJSON Polygon / MultiPolygon），或 center_lat + center_lng + radius_m 画圆")
    return body(
        appId=app_id, name=name, mode=mode, fence=fence, centerLat=center_lat, centerLng=center_lng, radiusM=radius_m,
        banMode=ban_mode, reason=reason, enabled=enabled, expiresAt=expires_at,
    )


FenceName = Annotated[str, Field(min_length=1, description="围栏名称")]
FenceMode = Annotated[Literal["deny", "allow", "review"], Field(description="deny 区域内拦截、allow 存在 allow 围栏时区域外拦截、review 区域内只记录")]
FenceShape = Annotated[dict[str, Any] | None, Field(description="GeoJSON Polygon / MultiPolygon（与圆形三参数二选一）")]
CenterLat = Annotated[float | None, Field(ge=-90, le=90, description="圆心纬度")]
CenterLng = Annotated[float | None, Field(ge=-180, le=180, description="圆心经度")]
RadiusM = Annotated[float | None, Field(gt=0, description="半径（米）")]


@server.tool(**WRITE("risk", idempotent=True))
async def preview_geo_fence(
    name: FenceName,
    mode: FenceMode,
    fence: FenceShape = None,
    center_lat: CenterLat = None,
    center_lng: CenterLng = None,
    radius_m: RadiusM = None,
    window_days: Annotated[int, Field(ge=1, le=90, description="用最近 N 天的请求估算影响")] = 7,
    app: OptionalApp = "",
) -> Any:
    """预览一个地理围栏草稿：按最近 N 天的流量估算会命中多少请求（不保存）。保存前先预览。"""
    payload = _fence_body(name, mode, await _app(app), fence, center_lat, center_lng, radius_m, "", "", True, "")
    payload["windowDays"] = window_days
    return await GEO_FENCE_PREVIEW(body=payload)


@server.tool(**DESTRUCTIVE("risk"))
async def save_geo_fence(
    name: FenceName,
    mode: FenceMode,
    fence_id: Annotated[int | None, Field(gt=0, description="给了就整体覆盖这条围栏；留空为新建")] = None,
    fence: FenceShape = None,
    center_lat: CenterLat = None,
    center_lng: CenterLng = None,
    radius_m: RadiusM = None,
    ban_mode: Annotated[str, Field(description="拦截时的响应模式；留空用平台默认")] = "",
    reason: Annotated[str, Field(description="原因")] = "",
    enabled: Annotated[bool, Field(description="是否启用")] = True,
    expires_at: Annotated[IsoTime, Field(description="到期时间（RFC 3339），留空为永久")] = "",
    app: OptionalApp = "",
) -> Any:
    """新建或整体覆盖一个地理围栏（立即生效，可能拦掉整片区域）。先用 risk_preview_geo_fence 估算影响并向用户确认。仅超级管理员。"""
    payload = _fence_body(name, mode, await _app(app), fence, center_lat, center_lng, radius_m, ban_mode, reason, enabled, expires_at)
    if fence_id:
        return await GEO_FENCE_UPDATE(id=fence_id, body=payload)
    return await GEO_FENCE_CREATE(body=payload)


@server.tool(**WRITE("risk", idempotent=True))
async def toggle_geo_fence(
    fence_id: Annotated[int, Field(gt=0, description="围栏 ID")],
    enabled: Annotated[bool, Field(description="启用（true）或停用（false）")],
) -> Any:
    """启用或停用一个地理围栏。仅超级管理员。"""
    return await GEO_FENCE_TOGGLE(id=fence_id, body={"enabled": enabled})


@server.tool(**DESTRUCTIVE("risk"))
async def delete_geo_fence(fence_id: Annotated[int, Field(gt=0, description="围栏 ID")]) -> Any:
    """删除一个地理围栏（不可恢复；只想暂停用 risk_toggle_geo_fence）。仅超级管理员。"""
    return await GEO_FENCE_DELETE(id=fence_id)


# ═══════════════════════ 防火墙日志 ═══════════════════════


@server.tool(**READ("risk"))
async def firewall_logs(
    log_id: Annotated[int | None, Field(gt=0, description="给了就取这一条拦截日志的详情")] = None,
    ip: Annotated[str, Field(description="IP")] = "",
    country: Annotated[str, Field(description="国家代码")] = "",
    reason: Annotated[str, Field(description="拦截原因")] = "",
    waf_rule_id: Annotated[int | None, Field(description="WAF 规则 ID")] = None,
    path_pattern: Annotated[str, Field(description="请求路径匹配")] = "",
    severity: Annotated[Literal["", "low", "medium", "high", "critical"], Field(description="严重程度")] = "",
    start_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339）")] = "",
    end_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339）")] = "",
    sort_by: Annotated[Literal["", "blocked_at", "ip"], Field(description="排序字段")] = "",
    sort_order: Annotated[Literal["", "asc", "desc"], Field(description="排序方向")] = "",
    page: Page = 1,
    page_size: PageSize = 20,
) -> Any:
    """防火墙拦截日志列表，或给 log_id 取详情。仅超级管理员。"""
    if log_id:
        return await FW_LOG(logId=log_id)
    return await FW_LOGS(query=q(
        ip=ip, country=country, reason=reason, wafRuleId=waf_rule_id, pathPattern=path_pattern, severity=severity,
        startTime=start_time, endTime=end_time, sortBy=sort_by, sortOrder=sort_order, page=page, pageSize=page_size,
    ))


@server.tool(**READ("risk"))
async def firewall_stats(
    start_time: Annotated[IsoTime, Field(description="起始时间（RFC 3339），默认 24 小时前")] = "",
    end_time: Annotated[IsoTime, Field(description="结束时间（RFC 3339），默认现在")] = "",
    granularity: Annotated[Literal["hour", "day"], Field(description="趋势粒度")] = "hour",
) -> Any:
    """防火墙拦截统计：总量、按原因 / 国家 / IP 排行、趋势。仅超级管理员。"""
    return await FW_STATS(query=q(startTime=start_time, endTime=end_time, granularity=granularity))


@server.tool(**DESTRUCTIVE("risk"))
async def cleanup_firewall_logs(before: Annotated[str, Field(min_length=1, description="删除这个时间（RFC 3339）之前的全部拦截日志")]) -> Any:
    """清理指定时间之前的防火墙拦截日志（不可恢复）。仅超级管理员，执行前先向用户确认。"""
    return await FW_LOGS_CLEANUP(body={"before": before})
