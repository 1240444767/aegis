"""写工具用的公共零件：参数类型、注解、标签、查询参数清洗、应用解析。

约定（tests/test_conventions.py 会逐条检查）：
  - 每个工具恰好带 "read" 或 "write" 标签之一，另带所属模块名标签；不可逆或影响面大的写工具再加 "destructive"。
    用 READ / WRITE / DESTRUCTIVE 三个装饰参数组合即可，不要手写 annotations。
  - 工具文档字符串用中文，第一行说清做什么；写工具说明影响与是否可逆。
  - 调后端一律经 core.route 声明的 Route，不直接用 httpx。
  - 时间参数用 ISO 8601 字符串（IsoTime），分页用 Page / Limit。
"""

from __future__ import annotations

from typing import Annotated, Any

from fastmcp.exceptions import ToolError
from mcp.types import ToolAnnotations
from pydantic import Field

from .client import AegisAPIError
from .runtime import current_key, runtime

AppRef = Annotated[str, Field(description="应用：appKey 或数字应用 ID（apps_list 可查）", min_length=1)]
UserID = Annotated[int, Field(description="管理端用户 ID（用户主键，users_search 结果里的 id）", gt=0)]
Page = Annotated[int, Field(description="页码，从 1 开始", ge=1)]
Limit = Annotated[int, Field(description="每页条数", ge=1, le=200)]
IsoTime = Annotated[str, Field(description="时间，ISO 8601（如 2026-10-28T00:00:00+08:00），留空不限")]
Reason = Annotated[str, Field(description="操作原因（记入流水 / 审计）", min_length=1)]
RequestID = Annotated[str, Field(description="幂等键：同一个值重复提交只生效一次；留空不做幂等")]


def _annotations(read_only: bool, destructive: bool, idempotent: bool | None = None) -> ToolAnnotations:
    return ToolAnnotations(
        read_only_hint=read_only,
        destructive_hint=None if read_only else destructive,
        idempotent_hint=True if read_only else idempotent,
        open_world_hint=False,
    )


def READ(*domain: str) -> dict[str, Any]:
    """只读工具：`@server.tool(**READ("users"))`。"""
    return {"tags": {"read", *domain}, "annotations": _annotations(True, False)}


def WRITE(*domain: str, idempotent: bool | None = None) -> dict[str, Any]:
    """可逆或增量的写工具（新建、修改、启停、发放）。"""
    return {"tags": {"write", *domain}, "annotations": _annotations(False, False, idempotent)}


def DESTRUCTIVE(*domain: str) -> dict[str, Any]:
    """不可逆或影响面大的写工具（删除、封禁、撤销、扣减、批量推送）。"""
    return {"tags": {"write", "destructive", *domain}, "annotations": _annotations(False, True)}


def q(**params: Any) -> dict[str, Any]:
    """组查询参数：去掉 None / 空串 / False 以外的空值；False 保留（显式过滤条件）。"""
    return {key: value for key, value in params.items() if value is not None and value != "" and value != [] and value != {}}


def body(**fields: Any) -> dict[str, Any]:
    """组请求体：只去掉 None，空串与 False 原样保留（可能是有意清空）。"""
    return {key: value for key, value in fields.items() if value is not None}


async def app_id_of(app: str) -> int:
    """appKey → 数字应用 ID（少数旧接口只认 appid）。数字原样返回。"""
    app = str(app).strip()
    if app.isdigit():
        return int(app)
    try:
        who = await runtime().client.whoami(current_key())
    except AegisAPIError as exc:
        raise ToolError(exc.describe()) from exc
    for item in who.get("apps") or []:
        if str(item.get("appKey") or "") == app:
            return int(item["id"])
    raise ToolError(f"找不到 appKey 为 {app} 的应用（或这把密钥无权访问它），请用 apps_list 核对")


def require_one(**options: Any) -> None:
    """几个参数至少给一个。"""
    if not any(value not in (None, "", False, [], {}) for value in options.values()):
        raise ToolError(f"需要 {' / '.join(options)} 之一")
