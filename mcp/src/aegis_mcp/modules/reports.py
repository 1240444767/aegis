"""报表中心：总览、注册、活跃、留存、登录、渠道、设备、地域、付费、通知触达、参与度、漏斗、风险等运营报表。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import GET, READ, AppRef, q

NAMESPACE = "reports"
server = FastMCP("reports")

REPORTS = {
    name: GET(f"/api/admin/apps/:appkey/reports/{name}")
    for name in (
        "overview", "registration", "active", "retention", "login", "channel", "device", "region",
        "payment", "notification", "engagement", "activity", "funnel", "risk",
    )
}

ReportName = Literal[
    "overview", "registration", "active", "retention", "login", "channel", "device", "region",
    "payment", "notification", "engagement", "activity", "funnel", "risk",
]
DateArg = Annotated[str, Field(min_length=8, description="日期 YYYY-MM-DD 或 RFC3339 时刻（按 tz 取日历日，含首尾两天）")]


@server.tool(name="get", **READ("reports"))
async def get_report(
    app: AppRef,
    report: Annotated[ReportName, Field(description="报表：overview 总览 / registration 注册 / active 活跃 / retention 留存 / login 登录 / channel 渠道 / device 设备 / region 地域 / payment 付费 / notification 通知触达 / engagement 参与度 / activity 活跃时段 / funnel 漏斗 / risk 风险")],
    start: DateArg,
    end: DateArg,
    tz: Annotated[str, Field(description="时区，如 Asia/Shanghai；留空用服务器默认")] = "Asia/Shanghai",
    granularity: Annotated[Literal["", "day", "week", "month"], Field(description="时间粒度")] = "",
    unit: Annotated[Literal["", "day", "week"], Field(description="留存报表的周期单位")] = "",
    mode: Annotated[Literal["", "exact", "rolling"], Field(description="留存报表口径：exact 第 N 天当天 / rolling 第 N 天及以后")] = "",
) -> Any:
    """取一张运营报表（按日期区间）。导出 CSV 请在控制台操作。"""
    return await REPORTS[report](appkey=app, query=q(start=start, end=end, tz=tz, granularity=granularity, unit=unit, mode=mode))


@server.tool(**READ("reports"))
async def dashboard(
    app: AppRef,
    start: DateArg,
    end: DateArg,
    tz: str = "Asia/Shanghai",
    reports: Annotated[list[ReportName], Field(description="一次取多张报表")] = ["overview", "registration", "active", "payment"],
) -> Any:
    """一次取多张报表拼成运营看板，单张失败不影响其它（错误写在对应项里）。"""
    result: dict[str, Any] = {}
    for name in dict.fromkeys(reports):
        try:
            result[name] = await REPORTS[name](appkey=app, query=q(start=start, end=end, tz=tz))
        except ToolError as exc:
            result[name] = {"error": str(exc)}
    return result
