"""功能权益：用户的功能解锁（列表、授予、收回）。

功能时长（feature-time）与权益凭证（feature-ticket）目前只有用户端接口，没有管理端接口；
功能时长的发放在激励广告场景（ads_rewarded_save_scene）与抽奖奖品里配置，功能标识目录见 vip_features。
权限：读需要 app:read，授予 / 收回需要 app:write。
"""

from __future__ import annotations

from typing import Annotated, Any

from fastmcp import FastMCP
from pydantic import Field

from ..core import DESTRUCTIVE, GET, POST, READ, WRITE, AppRef, UserID, body

NAMESPACE = "features"
server = FastMCP("features")

UNLOCKS = GET("/api/admin/apps/:appkey/users/:userId/feature-unlocks")
UNLOCK_GRANT = POST("/api/admin/apps/:appkey/users/:userId/feature-unlocks")
UNLOCK_REVOKE = POST("/api/admin/apps/:appkey/users/:userId/feature-unlocks/:unlockId/revoke")


@server.tool(**READ("features"))
async def unlocks(app: AppRef, user_id: UserID) -> Any:
    """一个用户的功能解锁：全部记录（来源、天数、到期、是否已收回）与当前生效的功能。"""
    return await UNLOCKS(appkey=app, userId=user_id)


@server.tool(**WRITE("features"))
async def grant_unlock(
    app: AppRef,
    user_id: UserID,
    tag: Annotated[str, Field(min_length=1, description="功能标识（须已在会员功能目录登记，见 vip_features）")],
    days: Annotated[int, Field(ge=0, le=3650, description="解锁天数，0 为永久")] = 0,
) -> Any:
    """给用户授予一项功能解锁（来源记为管理员授予），立即生效。

    已是永久会员或已永久拥有该功能时可能被视为已拥有。发错了可用 features_revoke_unlock 收回。永久解锁（days=0）执行前先向用户确认。
    """
    return await UNLOCK_GRANT(appkey=app, userId=user_id, body={"tag": tag, "days": days})


@server.tool(**DESTRUCTIVE("features"))
async def revoke_unlock(
    app: AppRef,
    user_id: UserID,
    unlock_id: Annotated[int, Field(gt=0, description="功能解锁记录 ID（features_unlocks 结果里的 id）")],
    reason: Annotated[str, Field(description="收回原因")] = "",
) -> Any:
    """收回用户的一条功能解锁，立即失效（不可撤销，需要的话只能重新授予）。执行前先向用户确认功能与记录 ID。"""
    return await UNLOCK_REVOKE(appkey=app, userId=user_id, unlockId=unlock_id, body=body(reason=reason))
