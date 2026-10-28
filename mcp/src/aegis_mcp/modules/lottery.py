"""抽奖：表单目录、活动与奖品（新建、修改、启停、删除）、策略模拟、统计与出奖审计、抽奖记录与人工奖品发放。

权限：读需要 app:read，其余（含模拟）需要 app:write。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, body, q

NAMESPACE = "lottery"
server = FastMCP("lottery")

CATALOG = GET("/api/admin/apps/:appkey/lottery/catalog")
ACTIVITIES = GET("/api/admin/apps/:appkey/lottery/activities")
ACTIVITY = GET("/api/admin/apps/:appkey/lottery/activities/:activityId")
ACTIVITY_CREATE = POST("/api/admin/apps/:appkey/lottery/activities")
ACTIVITY_UPDATE = PUT("/api/admin/apps/:appkey/lottery/activities/:activityId")
ACTIVITY_STATUS = PUT("/api/admin/apps/:appkey/lottery/activities/:activityId/status")
ACTIVITY_DELETE = DELETE("/api/admin/apps/:appkey/lottery/activities/:activityId")
ACTIVITY_STATS = GET("/api/admin/apps/:appkey/lottery/activities/:activityId/stats")
ACTIVITY_AUDIT = GET("/api/admin/apps/:appkey/lottery/activities/:activityId/audit")
SIMULATE = POST("/api/admin/apps/:appkey/lottery/simulate")
DRAWS = GET("/api/admin/apps/:appkey/lottery/draws")
DRAW_FULFILL = POST("/api/admin/apps/:appkey/lottery/draws/:drawId/fulfill")

ActivityID = Annotated[int, Field(gt=0, description="抽奖活动 ID（lottery_activities 结果里的 id）")]
Days = Annotated[int, Field(ge=1, le=90, description="统计最近 N 天")]
Status = Literal["draft", "active", "paused", "ended"]

PRIZES_HELP = (
    "奖品列表（整体替换，九宫格恰好 8 个，转盘 2–12 个）。每项 {id?（带 id 为更新已有奖品、保留已抽中数量，不带为新建）, name, "
    "reward: {type, amount?, refId?, tag?, text?}, imageUrl?, quantity（总库存，-1 不限）, weight（权重，0 不参与常规抽取）, "
    "position（奖盘位置，九宫格外圈顺时针 0–7）, fallback?（库存抢光时改发它，须不限库存）, dailyLimit?, userLimit?, userDailyLimit?, "
    "value?（积分当量）, lifetimeReward?（已拥有时改发，积分 / 经验 / 抽奖次数）}。reward.type：none 谢谢参与 / points 积分 / "
    "vip_plan（refId=套餐 ID）/ vip_days（amount=天）/ vip_lifetime / feature（tag=功能，amount=天，0 永久）/ "
    "feature_time（tag=功能，amount=分钟）/ ad_free（amount=天）/ experience / lottery_draws / manual（text=人工发放内容）。"
    "以 lottery_catalog 为准"
)
STRATEGY_HELP = (
    "多维度出奖策略（整体替换，可选键）：softPity {start, step} / pacing / cooldown / budget / risk / behavior / rules / "
    "luck / variance（independent | prd）/ winRate / guard / fortune {spread}（每日运气）。不确定时先用 lottery_simulate 验证"
)
PERKS_HELP = (
    "人群特权（整体替换）。每项 {name, disabled?, audience: all | member | lifetime | non_member | non_lifetime | ad_free, "
    "signedToday?, onlineMinutes?, when?（表达式）, extraFree?, adSkip?（0–100）, adSkipSlots?, pointsDiscount?（百分比）, "
    "extraPoints?, pityCut?, winBoost?（1–3）}"
)

REQUEST_KEYS = (
    "name", "description", "rules", "uiMode", "status", "startTime", "endTime", "freeDaily", "pointsCost", "pointsDaily",
    "videoScene", "pityThreshold", "lifetimePolicy", "lifetimePointsPerDay", "lifetimePointsLifetime", "lifetimeDraws",
    "strategy", "perks", "sortOrder",
)
PRIZE_KEYS = (
    "id", "name", "reward", "imageUrl", "quantity", "weight", "position", "fallback",
    "dailyLimit", "userLimit", "userDailyLimit", "value", "lifetimeReward",
)


def _time(value: str | None) -> Any:
    """None 不改；空串表示清空（发 null）。"""
    if value is None:
        return None
    return value or _CLEAR


_CLEAR = object()


def _activity_fields(
    *, name: str | None, description: str | None, rules: str | None, ui_mode: str | None, status: str | None,
    start_time: str | None, end_time: str | None, free_daily: int | None, points_cost: int | None, points_daily: int | None,
    video_scene: str | None, pity_threshold: int | None, pity_position: int | None, lifetime_policy: str | None,
    lifetime_points_per_day: int | None, lifetime_points_lifetime: int | None, lifetime_draws: int | None,
    strategy: dict[str, Any] | None, perks: list[dict[str, Any]] | None, sort_order: int | None,
    prizes: list[dict[str, Any]] | None,
) -> dict[str, Any]:
    fields = body(
        name=name, description=description, rules=rules, uiMode=ui_mode, status=status,
        startTime=_time(start_time), endTime=_time(end_time), freeDaily=free_daily, pointsCost=points_cost,
        pointsDaily=points_daily, videoScene=video_scene, pityThreshold=pity_threshold, pityPosition=pity_position,
        lifetimePolicy=lifetime_policy, lifetimePointsPerDay=lifetime_points_per_day,
        lifetimePointsLifetime=lifetime_points_lifetime, lifetimeDraws=lifetime_draws,
        strategy=strategy, perks=perks, sortOrder=sort_order, prizes=prizes,
    )
    return {key: (None if value is _CLEAR else value) for key, value in fields.items()}


def _request_of(current: dict[str, Any]) -> dict[str, Any]:
    """已保存的活动 → 整体更新的请求体（保底奖从 pityPrizeId 换回位置）。"""
    request = {key: current[key] for key in REQUEST_KEYS if key in current}
    request["prizes"] = []
    pity_prize = current.get("pityPrizeId")
    for prize in current.get("prizes") or []:
        request["prizes"].append({key: prize[key] for key in PRIZE_KEYS if key in prize})
        if pity_prize is not None and prize.get("id") == pity_prize:
            request["pityPosition"] = prize.get("position")
    return request


@server.tool(**READ("lottery"))
async def catalog(app: AppRef) -> Any:
    """抽奖活动表单的目录：奖品类型与取值形态、界面形态、活动状态、已拥有处理方式、可选的会员套餐 / 功能标识 / 激励广告场景、策略与特权的可选项。新建或修改活动前先看它。"""
    return await CATALOG(appkey=app)


@server.tool(**READ("lottery"))
async def activities(app: AppRef) -> Any:
    """抽奖活动列表（含奖品、总抽奖次数、人数、今日次数、待人工发放数与积分套利检查）。"""
    return await ACTIVITIES(appkey=app)


@server.tool(name="get", **READ("lottery"))
async def get_activity(app: AppRef, activity_id: ActivityID) -> Any:
    """一个抽奖活动的完整配置（奖品、策略、人群特权、已拥有处理方式）与汇总。"""
    return await ACTIVITY(appkey=app, activityId=activity_id)


Name = Annotated[str | None, Field(max_length=32, description="活动名称（≤32 字）")]
Description = Annotated[str | None, Field(max_length=200, description="活动简介（≤200 字）")]
Rules = Annotated[str | None, Field(description="活动规则说明（用户可见）")]
UIMode = Annotated[Literal["grid", "wheel"] | None, Field(description="界面形态：grid 九宫格（8 个奖品）/ wheel 转盘（2–12 个）")]
StartTime = Annotated[str | None, Field(description="开始时间 ISO 8601（RFC 3339）；空串为不限")]
EndTime = Annotated[str | None, Field(description="结束时间 ISO 8601（RFC 3339）；空串为不限")]
FreeDaily = Annotated[int | None, Field(ge=0, description="每人每天免费抽几次，0 为不送")]
PointsCost = Annotated[int | None, Field(ge=0, description="积分抽奖每次消耗多少积分，0 为不开放积分抽奖")]
PointsDaily = Annotated[int | None, Field(ge=0, description="积分抽奖每人每天上限，0 为不限")]
VideoScene = Annotated[str | None, Field(description="激励广告场景标识（看完发抽奖次数），空串为不开放视频抽奖")]
PityThreshold = Annotated[int | None, Field(ge=0, description="连续这么多次未中时必中保底奖，0 为关闭")]
PityPosition = Annotated[int | None, Field(ge=0, description="保底奖在奖盘上的位置（position）")]
LifetimePolicy = Annotated[
    Literal["points", "draws", "exclude", "grant"] | None,
    Field(description="抽中已拥有的奖品（永久会员抽中会员等）：points 折算积分（默认）/ draws 改发抽奖次数 / exclude 不参与抽取 / grant 照常发放"),
]
LifetimePerDay = Annotated[int | None, Field(ge=0, description="折算积分时每天会员 / 功能折多少积分")]
LifetimePerLifetime = Annotated[int | None, Field(ge=0, description="折算积分时永久套餐 / 永久功能折多少积分")]
LifetimeDraws = Annotated[int | None, Field(ge=0, description="改发抽奖次数时发几次")]
Strategy = Annotated[dict[str, Any] | None, Field(description=STRATEGY_HELP)]
Perks = Annotated[list[dict[str, Any]] | None, Field(description=PERKS_HELP)]
SortOrder = Annotated[int | None, Field(description="排序，小的在前")]


@server.tool(**WRITE("lottery"))
async def create(
    app: AppRef,
    name: Annotated[str, Field(min_length=1, max_length=32, description="活动名称（≤32 字）")],
    prizes: Annotated[list[dict[str, Any]], Field(min_length=1, description=PRIZES_HELP)],
    ui_mode: UIMode = None,
    status: Annotated[Status | None, Field(description="初始状态，默认 draft（草稿，用户不可见）")] = None,
    description: Description = None,
    rules: Rules = None,
    start_time: StartTime = None,
    end_time: EndTime = None,
    free_daily: FreeDaily = None,
    points_cost: PointsCost = None,
    points_daily: PointsDaily = None,
    video_scene: VideoScene = None,
    pity_threshold: PityThreshold = None,
    pity_position: PityPosition = None,
    lifetime_policy: LifetimePolicy = None,
    lifetime_points_per_day: LifetimePerDay = None,
    lifetime_points_lifetime: LifetimePerLifetime = None,
    lifetime_draws: LifetimeDraws = None,
    strategy: Strategy = None,
    perks: Perks = None,
    sort_order: SortOrder = None,
) -> Any:
    """新建抽奖活动（连同全部奖品）。建议先以 draft 创建、用 lottery_simulate 验证中奖率，再用 lottery_set_status 上线。直接以 active 创建会立即对用户开放。"""
    return await ACTIVITY_CREATE(appkey=app, body=_activity_fields(
        name=name, description=description, rules=rules, ui_mode=ui_mode, status=status,
        start_time=start_time, end_time=end_time, free_daily=free_daily, points_cost=points_cost, points_daily=points_daily,
        video_scene=video_scene, pity_threshold=pity_threshold, pity_position=pity_position, lifetime_policy=lifetime_policy,
        lifetime_points_per_day=lifetime_points_per_day, lifetime_points_lifetime=lifetime_points_lifetime,
        lifetime_draws=lifetime_draws, strategy=strategy, perks=perks, sort_order=sort_order, prizes=prizes,
    ))


@server.tool(**WRITE("lottery"))
async def update(
    app: AppRef,
    activity_id: ActivityID,
    name: Name = None,
    description: Description = None,
    rules: Rules = None,
    ui_mode: UIMode = None,
    start_time: StartTime = None,
    end_time: EndTime = None,
    free_daily: FreeDaily = None,
    points_cost: PointsCost = None,
    points_daily: PointsDaily = None,
    video_scene: VideoScene = None,
    pity_threshold: PityThreshold = None,
    pity_position: PityPosition = None,
    lifetime_policy: LifetimePolicy = None,
    lifetime_points_per_day: LifetimePerDay = None,
    lifetime_points_lifetime: LifetimePerLifetime = None,
    lifetime_draws: LifetimeDraws = None,
    strategy: Strategy = None,
    perks: Perks = None,
    sort_order: SortOrder = None,
    prizes: Annotated[list[dict[str, Any]] | None, Field(description=PRIZES_HELP + "。不传则保留现有奖品")] = None,
) -> Any:
    """修改抽奖活动。只传要改的字段，其余沿用当前配置（后端整体覆盖，这里先读后合并；状态请用 lottery_set_status）。

    传 prizes 即整体替换奖品：不带 id 的是新奖品，现有奖品没出现在列表里的会被移除。已有抽奖记录是快照，不受影响。
    进行中的活动改概率、库存会立即影响用户，执行前向用户复述改动，必要时先 lottery_simulate。
    """
    current = await ACTIVITY(appkey=app, activityId=activity_id)
    if not isinstance(current, dict):
        raise ToolError(f"读取活动 {activity_id} 失败")
    request = _request_of(current)
    request.update(_activity_fields(
        name=name, description=description, rules=rules, ui_mode=ui_mode, status=None,
        start_time=start_time, end_time=end_time, free_daily=free_daily, points_cost=points_cost, points_daily=points_daily,
        video_scene=video_scene, pity_threshold=pity_threshold, pity_position=pity_position, lifetime_policy=lifetime_policy,
        lifetime_points_per_day=lifetime_points_per_day, lifetime_points_lifetime=lifetime_points_lifetime,
        lifetime_draws=lifetime_draws, strategy=strategy, perks=perks, sort_order=sort_order, prizes=prizes,
    ))
    return await ACTIVITY_UPDATE(appkey=app, activityId=activity_id, body=request)


@server.tool(**WRITE("lottery", idempotent=True))
async def set_status(
    app: AppRef,
    activity_id: ActivityID,
    status: Annotated[Status, Field(description="draft 草稿 / active 上线 / paused 暂停 / ended 结束")],
) -> Any:
    """上线、暂停或结束抽奖活动。上线前会按完整规则复核一遍（奖品、引用的套餐 / 功能 / 场景）；上线与结束立即对用户生效，执行前先确认。"""
    return await ACTIVITY_STATUS(appkey=app, activityId=activity_id, body={"status": status})


@server.tool(name="delete", **DESTRUCTIVE("lottery"))
async def delete_activity(app: AppRef, activity_id: ActivityID) -> Any:
    """删除抽奖活动（仅限没有抽奖记录的活动，不可恢复）。有记录的活动请用 lottery_set_status 结束。执行前先向用户确认活动名称与 ID。"""
    return await ACTIVITY_DELETE(appkey=app, activityId=activity_id)


@server.tool(**WRITE("lottery", idempotent=True))
async def simulate(
    app: AppRef,
    activity_id: Annotated[int | None, Field(gt=0, description="模拟已保存的这个活动")] = None,
    activity: Annotated[dict[str, Any] | None, Field(description="模拟一份还没保存的活动配置（字段同 lottery_create 的请求体，camelCase）；给了就不用 activity_id 的已存配置")] = None,
    users: Annotated[int, Field(ge=1, le=5000, description="虚拟用户数")] = 100,
    draws_per_user: Annotated[int, Field(ge=1, le=1000, description="每人连续抽几次（用户数 × 次数 ≤ 200000）")] = 20,
    source: Annotated[Literal["", "free", "bonus", "points"], Field(description="按哪种次数来源抽，留空自动")] = "",
    isolated_stock: Annotated[bool, Field(description="每个虚拟用户独立库存（不互相抢库存）")] = False,
    seed: Annotated[int | None, Field(ge=0, description="随机种子，固定后结果可复现")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="用这个真实用户的画像（并附带他下一次抽奖的出奖方案）")] = None,
    profile: Annotated[dict[str, Any] | None, Field(description="不指定用户时的虚拟画像（行为信号，如 registeredDays、signStreak、member、paidAmount…）")] = None,
    perk: Annotated[dict[str, Any] | None, Field(description="不指定用户时虚拟用户的特权身份：{member, lifetime, adFree, signedToday, onlineMinutesToday}")] = None,
) -> Any:
    """按出奖策略模拟抽奖（不写库、不发奖、不扣次数）：实际中奖率、各奖品分布、首中次数与连续未中分布。需要 app:write 权限。"""
    if activity_id is None and activity is None:
        raise ToolError("需要 activity_id（已保存的活动）或 activity（未保存的配置）之一")
    if users * draws_per_user > 200_000:
        raise ToolError("虚拟用户数 × 每人次数不能超过 200000")
    return await SIMULATE(appkey=app, body=body(
        activityId=activity_id, activity=activity, users=users, drawsPerUser=draws_per_user, source=source or None,
        isolatedStock=isolated_stock, seed=seed, userId=user_id, profile=profile, perk=perk,
    ))


@server.tool(**READ("lottery"))
async def stats(app: AppRef, activity_id: ActivityID, days: Days = 14) -> Any:
    """一个活动的概览、按天趋势、次数来源与奖品分布。"""
    return await ACTIVITY_STATS(appkey=app, activityId=activity_id, query={"days": days})


@server.tool(**READ("lottery"))
async def audit(app: AppRef, activity_id: ActivityID, days: Days = 14) -> Any:
    """出奖审计：实际出奖分布与配置概率的整体检验（Mahalanobis χ²）和各奖品的边际检验，判断是否偏离配置。"""
    return await ACTIVITY_AUDIT(appkey=app, activityId=activity_id, query={"days": days})


@server.tool(**READ("lottery"))
async def draws(
    app: AppRef,
    activity_id: Annotated[int | None, Field(gt=0, description="只看这个活动，留空为全部")] = None,
    user_id: Annotated[int | None, Field(gt=0, description="只看这个用户（管理端用户 ID）")] = None,
    status: Annotated[Literal["", "granted", "pending", "fulfilled"], Field(description="granted 已到账 / pending 人工奖品待发放 / fulfilled 人工奖品已发放，留空为全部")] = "",
    source: Annotated[Literal["", "free", "bonus", "points"], Field(description="次数来源：free 免费 / bonus 抽奖次数余额 / points 积分")] = "",
    winning: Annotated[bool, Field(description="只看抽中的（排除谢谢参与）")] = False,
    keyword: Annotated[str, Field(description="关键词（账号、奖品名等）")] = "",
    start: IsoTime = "",
    end: IsoTime = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """抽奖记录 / 中奖记录（奖品快照、来源、发放结果与折算、状态）。查待人工发放的奖品用 status=pending。"""
    return await DRAWS(appkey=app, query=q(
        activityId=activity_id, userId=user_id, status=status, source=source, winning=winning or None,
        keyword=keyword, start=start, end=end, page=page, limit=limit,
    ))


@server.tool(**WRITE("lottery"))
async def fulfill(
    app: AppRef,
    draw_id: Annotated[int, Field(gt=0, description="抽奖记录 ID（lottery_draws 结果里的 id，状态须为 pending）")],
    note: Annotated[str, Field(description="发放说明，会写进给用户的站内信（兑换码、快递单号等）")] = "",
) -> Any:
    """把一条人工发放的奖品标记为已发放，并给用户发站内信（含 note）。消息发出后无法撤回，执行前先向用户确认 note 内容。"""
    return await DRAW_FULFILL(appkey=app, drawId=draw_id, body=body(note=note))
