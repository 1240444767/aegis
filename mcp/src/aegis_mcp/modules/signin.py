"""签到：签到统计与明细、签到奖励策略（查看 / 修改 / 试算 / 重置 / 模板）、自动签到开放策略。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, Page, body, q

NAMESPACE = "signin"
server = FastMCP("signin")

STATS = GET("/api/admin/apps/:appkey/signin/stats")
RECORDS = GET("/api/admin/apps/:appkey/signin/records")
REWARD = GET("/api/admin/apps/:appkey/signin-reward")
REWARD_UPDATE = PUT("/api/admin/apps/:appkey/signin-reward")
REWARD_TEST = POST("/api/admin/apps/:appkey/signin-reward/test")
REWARD_RESET = POST("/api/admin/apps/:appkey/signin-reward/reset")
REWARD_TEMPLATES = GET("/api/admin/apps/signin-reward/templates")
AUTO_SIGN = GET("/api/admin/apps/:appkey/auto-sign")
AUTO_SIGN_UPDATE = PUT("/api/admin/apps/:appkey/auto-sign")

Date = Annotated[str, Field(pattern=r"^(\d{4}-\d{2}-\d{2})?$", description="日期 YYYY-MM-DD（服务器时区），留空不限")]
Policy = Annotated[dict[str, Any], Field(description=(
    "签到奖励策略（与 signin_reward_policy 返回的 policy 同形）：name、description、enabled、timezone、baseIntegral、baseExperience、"
    "firstSignInExperienceBonus、consecutiveExperienceStep、consecutiveExperienceStepCap、applyLevelExperienceMultiplier、"
    "maxIntegralReward、maxExperienceReward、rules[{key,name,enabled,priority,group,expression,bonusType,integralMultiplierDelta,"
    "integralBonus,experienceMultiplierDelta,…}]、milestones[…]"
))]


def _policy_of(view: Any) -> dict[str, Any]:
    if isinstance(view, dict) and isinstance(view.get("policy"), dict):
        return dict(view["policy"])
    return dict(view or {})


@server.tool(**READ("signin"))
async def stats(app: AppRef, days: Annotated[int, Field(ge=1, le=90, description="统计最近 N 天")] = 14) -> Any:
    """应用签到统计：今日 / 区间签到人数与次数、按天趋势、连签分布、来源分布、发放的积分与经验。"""
    return await STATS(appkey=app, query={"days": days})


@server.tool(**READ("signin"))
async def records(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="只看某个用户（管理端用户 ID）")] = None,
    keyword: Annotated[str, Field(description="按账号 / 昵称搜索")] = "",
    source: Annotated[str, Field(description="签到来源（取值见 signin_stats 的 sources），留空不限")] = "",
    bonus_type: Annotated[str, Field(description="命中的奖励规则类型（策略规则的 bonusType），留空不限")] = "",
    date_from: Date = "",
    date_to: Date = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """签到明细：每次签到的用户、时间、连签天数、来源、获得的积分与经验、命中的规则。"""
    if date_from and date_to and date_from > date_to:
        raise ToolError("date_from 不能晚于 date_to")
    return await RECORDS(appkey=app, query=q(
        userId=user_id, keyword=keyword, source=source, bonusType=bonus_type, dateFrom=date_from, dateTo=date_to, page=page, limit=limit,
    ))


@server.tool(**READ("signin"))
async def reward_policy(app: AppRef) -> Any:
    """应用当前的签到奖励策略（基础积分 / 经验、连签递增、上限、规则表达式与里程碑；isDefault 表示未自定义）。"""
    return await REWARD(appkey=app)


@server.tool(**READ("signin"))
async def reward_templates() -> Any:
    """平台内置的签到奖励策略模板（balanced 默认基线 / growth 拉新 / retention 长周期留存），可作为 signin_update_reward_policy 的起点。"""
    return await REWARD_TEMPLATES()


@server.tool(**WRITE("signin", idempotent=True))
async def test_reward(
    app: AppRef,
    policy: Annotated[dict[str, Any] | None, Field(description="草稿策略（与 policy 同形）；不传按应用已保存的策略试算")] = None,
    consecutive_days: Annotated[int, Field(ge=1, description="假设的连续签到天数（含这一次）")] = 1,
    total_sign_ins: Annotated[int, Field(ge=0, description="假设的累计签到次数")] = 0,
    user_experience: Annotated[int, Field(ge=0, description="假设的用户当前经验（影响等级倍率）")] = 0,
    occurred_at: Annotated[str, Field(description="假设的签到时刻（ISO 8601），留空为现在（影响按星期 / 日期的规则）")] = "",
    simulate_days: Annotated[int, Field(ge=0, le=120, description="从这一天起逐日推演连续签到 N 天的奖励，0 不推演")] = 0,
) -> Any:
    """试算签到奖励：给定连签天数等条件，算出能拿多少积分 / 经验、命中哪些规则；可逐日推演。不写入任何数据。

    修改策略前先用草稿 policy 试算确认效果。后端按写权限（app:write）鉴权，只读密钥不可用。
    """
    return await REWARD_TEST(appkey=app, body=body(
        policy=policy, consecutiveDays=consecutive_days, totalSignIns=total_sign_ins, userExperience=user_experience,
        occurredAt=occurred_at or None, simulateDays=simulate_days or None,
    ))


@server.tool(**WRITE("signin"))
async def update_reward_policy(
    app: AppRef,
    policy: Policy,
    merge: Annotated[bool, Field(description="true：policy 只写要改的顶层字段，与现有策略浅合并（rules / milestones 给了就整体替换）；false：policy 是完整策略，整体覆盖")] = True,
) -> Any:
    """修改签到奖励策略，之后的签到立即按新策略发奖。执行前先用 signin_test_reward 试算，并向用户确认改动。

    校验失败（表达式错误、数值越界）返回 40067 与逐项原因。想回到平台默认用 signin_reset_reward_policy。
    """
    if not policy:
        raise ToolError("policy 不能为空")
    data = {**_policy_of(await REWARD(appkey=app)), **policy} if merge else dict(policy)
    data.pop("isDefault", None)
    return await REWARD_UPDATE(appkey=app, body={"policy": data})


@server.tool(**DESTRUCTIVE("signin"))
async def reset_reward_policy(app: AppRef) -> Any:
    """把签到奖励策略重置为平台默认（自定义的规则与里程碑全部丢弃，不可恢复）。执行前先用 signin_reward_policy 备份并向用户确认。"""
    return await REWARD_RESET(appkey=app)


@server.tool(**READ("signin"))
async def auto_sign(app: AppRef) -> Any:
    """自动签到的开放策略与使用情况：开放对象、开启了自动签到的用户数、今日自动签到次数、近期趋势。"""
    return await AUTO_SIGN(appkey=app)


@server.tool(**WRITE("signin"))
async def update_auto_sign(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="是否开放自动签到（关闭时所有人暂停，个人设置保留）")] = None,
    audience: Annotated[Literal["all", "vip", "lifetime", "feature"] | None, Field(description="开放对象：all 所有用户 / vip 有效会员 / lifetime 仅永久会员 / feature 持有指定功能")] = None,
    include_trial: Annotated[bool | None, Field(description="开放对象为 vip 时，试用会员是否算在内")] = None,
    feature_tag: Annotated[str | None, Field(description="开放对象为 feature 时要求持有的功能标识")] = None,
) -> Any:
    """修改自动签到开放策略。只传要改的字段，其余保持原值。收紧范围不会改动任何人的个人设置，资格恢复后照常生效。"""
    overview = await AUTO_SIGN(appkey=app) or {}
    current = dict(overview.get("policy") or {})
    current.pop("isDefault", None)
    merged = {**current, **body(enabled=enabled, audience=audience, includeTrial=include_trial, featureTag=feature_tag)}
    if merged.get("audience") == "feature" and not merged.get("featureTag"):
        raise ToolError("开放对象为 feature 时需要 feature_tag")
    return await AUTO_SIGN_UPDATE(appkey=app, body={"policy": merged})
