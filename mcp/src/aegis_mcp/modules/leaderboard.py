"""排行榜：设置与目录、实时 / 往期榜单、移出名单、周期结算与快照、奖励重试、概览统计。"""

from __future__ import annotations

import re
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import BaseModel, Field

from ..core import DELETE, DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, Page, UserID, body, q
from .invite import Reward, rewards_payload

NAMESPACE = "leaderboard"
server = FastMCP("leaderboard")

B = "/api/admin/apps/:appkey/leaderboard"
SETTINGS = GET(f"{B}/settings")
SETTINGS_SAVE = PUT(f"{B}/settings")
BOARD = GET(f"{B}/boards/:key")
EXCLUSIONS = GET(f"{B}/exclusions")
EXCLUSIONS_ADD = POST(f"{B}/exclusions")
EXCLUSION_REMOVE = DELETE(f"{B}/exclusions/:userId")
SNAPSHOTS = GET(f"{B}/snapshots")
SNAPSHOT_ENTRIES = GET(f"{B}/snapshots/:snapshotId/entries")
SNAPSHOT_RETRY = POST(f"{B}/snapshots/:snapshotId/retry")
SETTLE = POST(f"{B}/settle")
STATS = GET(f"{B}/stats")

BoardKey = Literal["points_total", "points_daily", "points_weekly", "points_monthly", "experience", "level"]
PeriodBoard = Literal["points_daily", "points_weekly", "points_monthly"]
BOARD_DOC = "榜单：points_total 积分总榜 / points_daily 日榜 / points_weekly 周榜 / points_monthly 月榜 / experience 经验榜 / level 等级榜"
PERIOD_DOC = "期号：日 2026-10-19、周 2026-W43（ISO 周）、月 2026-10"
_PERIOD = {
    "points_daily": re.compile(r"\d{4}-\d{2}-\d{2}"),
    "points_weekly": re.compile(r"\d{4}-W\d{2}"),
    "points_monthly": re.compile(r"\d{4}-\d{2}"),
}
SnapshotID = Annotated[int, Field(gt=0, description="快照 ID（leaderboard_snapshots 结果里的 id）")]


class BoardSetting(BaseModel):
    """一个榜单的开关；列表顺序即客户端标签顺序。"""

    key: BoardKey = Field(description=BOARD_DOC)
    enabled: bool = Field(description="是否开启")


class Tier(BaseModel):
    """一档名次奖励。"""

    rank_from: int = Field(ge=1, description="起始名次（含）")
    rank_to: int = Field(ge=1, description="结束名次（含）")
    rewards: list[Reward] = Field(min_length=1, description="这一档每人的奖励")

    def payload(self) -> dict[str, Any]:
        return {"rankFrom": self.rank_from, "rankTo": self.rank_to, "rewards": rewards_payload(self.rewards)}


class BoardRewards(BaseModel):
    """一个周期榜的结算奖励。"""

    enabled: bool = Field(description="是否发放")
    tiers: list[Tier] = Field(default_factory=list, description="名次档位（不能重叠、不能超过 top_limit）")

    def payload(self) -> dict[str, Any]:
        return {"enabled": self.enabled, "tiers": [tier.payload() for tier in self.tiers]}


_SETTINGS_FIELDS = ("enabled", "boards", "topLimit", "minValue", "identity", "allowAnonymous", "countAdminAdjust", "rewards")


def _check_period(board: str, period_key: str) -> str:
    period_key = period_key.strip()
    pattern = _PERIOD.get(board)
    if pattern and not pattern.fullmatch(period_key):
        raise ToolError(f"{board} 的期号格式不对（{PERIOD_DOC}）")
    return period_key


@server.tool(**READ("leaderboard"))
async def settings(app: AppRef) -> Any:
    """排行榜设置与目录：开关、各榜开关与顺序、上榜人数、最低值、展示身份、匿名、是否计入管理员调整、周期榜奖励档位；
    另含榜单目录、可发奖励目录、积分分类计入规则与各项上限。"""
    return await SETTINGS(appkey=app)


@server.tool(**WRITE("leaderboard"))
async def update_settings(
    app: AppRef,
    enabled: Annotated[bool | None, Field(description="开启 / 关闭排行榜")] = None,
    boards: Annotated[list[BoardSetting] | None, Field(description="各榜开关，顺序即标签顺序（缺的榜补在末尾并关闭；整体替换）")] = None,
    top_limit: Annotated[int | None, Field(ge=0, le=500, description="上榜人数（10-500，0 取 100）")] = None,
    min_value: Annotated[int | None, Field(ge=0, le=1_000_000_000, description="上榜最低值")] = None,
    identity: Annotated[Literal["nickname", "masked"] | None, Field(description="展示身份：nickname 昵称 / masked 脱敏账号")] = None,
    allow_anonymous: Annotated[bool | None, Field(description="是否允许用户匿名上榜")] = None,
    count_admin_adjust: Annotated[bool | None, Field(description="管理员调整的积分是否计入周期榜")] = None,
    rewards: Annotated[dict[PeriodBoard, BoardRewards] | None, Field(description="周期榜的结算奖励，键为榜单 key（只有日 / 周 / 月榜能配）；给出的榜整体替换，未给的保持")] = None,
) -> Any:
    """修改排行榜设置，立即生效（缓存键含这些设置）。只传要改的字段，后端整体覆盖，这里先读后合并。
    奖励按结算那一刻的设置快照，已结算的周期不受影响。"""
    data = await SETTINGS(appkey=app) or {}
    current = data.get("settings") if isinstance(data.get("settings"), dict) else data
    merged = {key: current.get(key) for key in _SETTINGS_FIELDS if current.get(key) is not None}
    merged.update(body(
        enabled=enabled, boards=None if boards is None else [item.model_dump() for item in boards], topLimit=top_limit,
        minValue=min_value, identity=identity, allowAnonymous=allow_anonymous, countAdminAdjust=count_admin_adjust,
    ))
    if rewards is not None:
        merged["rewards"] = {**(merged.get("rewards") or {}), **{key: value.payload() for key, value in rewards.items()}}
    merged.setdefault("enabled", False)
    return await SETTINGS_SAVE(appkey=app, body=merged)


@server.tool(**READ("leaderboard"))
async def board(
    app: AppRef,
    key: Annotated[BoardKey, Field(description=BOARD_DOC)],
    period_key: Annotated[str, Field(description=f"看某一期（{PERIOD_DOC}）：已结算取快照，未结算按该周期实时算；留空为当前期实时榜")] = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """管理端榜单：真实账号、取值、名次（被移出的人也列出、名次为空）、匿名偏好、最后获得积分时刻。"""
    return await BOARD(appkey=app, key=key, query=q(
        periodKey=_check_period(key, period_key) if period_key else "", page=page, limit=limit,
    ))


@server.tool(**READ("leaderboard"))
async def exclusions(
    app: AppRef,
    keyword: Annotated[str, Field(description="按账号 / 昵称 / 原因搜索")] = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """移出名单（被管理员移出所有榜单的用户、原因、操作人）。"""
    return await EXCLUSIONS(appkey=app, query=q(keyword=keyword, page=page, limit=limit))


@server.tool(**WRITE("leaderboard"))
async def exclude(
    app: AppRef,
    user_ids: Annotated[list[Annotated[int, Field(gt=0)]], Field(min_length=1, max_length=100, description="要移出的用户 ID（管理端主键），1-100 个")],
    reason: Annotated[str, Field(max_length=200, description="原因（如 刷分、测试号）")] = "",
) -> Any:
    """把用户移出所有排行榜（含未结算的周期，已结算的快照不改写）。可用 leaderboard_include 放回。返回 added 与不存在的 missing。"""
    return await EXCLUSIONS_ADD(appkey=app, body={"userIds": user_ids, "reason": reason})


@server.tool(**WRITE("leaderboard"))
async def include(app: AppRef, user_id: UserID) -> Any:
    """把一个用户从移出名单里放回，重新参与排名。不在名单里时返回 40454。"""
    return await EXCLUSION_REMOVE(appkey=app, userId=user_id)


@server.tool(**READ("leaderboard"))
async def snapshots(
    app: AppRef,
    board: Annotated[Literal["", "points_daily", "points_weekly", "points_monthly"], Field(description="只看某个周期榜，空为全部")] = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """往期结算快照列表（期号、参与人数、结算人与时间、奖励发放统计）。"""
    return await SNAPSHOTS(appkey=app, query=q(board=board, page=page, limit=limit))


@server.tool(**READ("leaderboard"))
async def snapshot_entries(
    app: AppRef,
    snapshot_id: SnapshotID,
    status: Annotated[Literal["", "none", "pending", "granted", "failed", "skipped"], Field(description="奖励状态：none 无奖励 / pending 待发 / granted 已发 / failed 失败 / skipped 跳过（被移出或已注销）；空为不限")] = "",
    page: Page = 1,
    limit: Annotated[int, Field(ge=1, le=100, description="每页条数")] = 20,
) -> Any:
    """一期快照的名次条目（结算时冻结的名次、取值、展示名、应发奖励与发放结果 / 失败原因）。"""
    return await SNAPSHOT_ENTRIES(appkey=app, snapshotId=snapshot_id, query=q(status=status, page=page, limit=limit))


@server.tool(**WRITE("leaderboard"))
async def retry_snapshot(app: AppRef, snapshot_id: SnapshotID) -> Any:
    """把一期快照里发放失败（failed）的奖励全部置回待发放，由结算循环重新发放（幂等键保证不重复发）。返回重新排队的条数。"""
    return await SNAPSHOT_RETRY(appkey=app, snapshotId=snapshot_id)


@server.tool(**DESTRUCTIVE("leaderboard"))
async def settle(
    app: AppRef,
    board: Annotated[PeriodBoard, Field(description="要结算的周期榜")],
    period_key: Annotated[str, Field(min_length=1, description=PERIOD_DOC)],
) -> Any:
    """手动结算一个已结束的周期：生成快照并按档位发奖励（幂等：已结算过直接返回原快照，created=false）。
    周期未结束返回 40922。结算会真实发奖且不可撤销，执行前先用 leaderboard_board 带 period_key 预览名次并向用户确认。"""
    return await SETTLE(appkey=app, body={"board": board, "periodKey": _check_period(board, period_key)})


@server.tool(**READ("leaderboard"))
async def stats(app: AppRef) -> Any:
    """排行榜概览：各榜参与人数、本期前 3、最近一次结算时间、移出人数、待发与失败的奖励数。"""
    return await STATS(appkey=app)
