"""钱包：用户余额与流水、应用级资金流水与统计、管理员调整余额、把流水凭证寄到邮箱。"""

from __future__ import annotations

from decimal import Decimal, InvalidOperation
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DESTRUCTIVE, GET, POST, READ, WRITE, AppRef, IsoTime, Limit, Page, UserID, body, q

NAMESPACE = "wallet"
server = FastMCP("wallet")

USER_WALLET = GET("/api/admin/apps/:appkey/users/:userId/wallet")
USER_TRANSACTIONS = GET("/api/admin/apps/:appkey/users/:userId/wallet/transactions")
APP_TRANSACTIONS = GET("/api/admin/apps/:appkey/wallet/transactions")
APP_STATS = GET("/api/admin/apps/:appkey/wallet/stats")
ADJUST = POST("/api/admin/apps/:appkey/wallet/adjust")
RECEIPT_EMAIL = POST("/api/admin/apps/:appkey/wallet/receipt/email")

MAX_ADJUST = Decimal("100000000")

TxnType = Annotated[
    Literal["", "recharge", "consume", "refund", "admin_adjust", "vip_purchase", "order_pay", "card_key", "points_exchange", "invite", "leaderboard", "ai_refund"],
    Field(description="流水类型：recharge 充值 / consume 业务消费 / refund 退款入账 / admin_adjust 管理员调整 / vip_purchase 余额购会员 / "
                      "order_pay 余额付订单 / card_key 卡密 / points_exchange 积分兑换 / invite 邀请奖励 / leaderboard 排行榜奖励 / ai_refund AI 退款；空为不限"),
]
DocumentType = Annotated[Literal["", "receipt", "credit_note"], Field(description="凭证类型：receipt 收据 / credit_note 退款凭证；留空按流水类型推导")]


@server.tool(name="get", **READ("wallet"))
async def get_wallet(
    app: AppRef,
    user_id: UserID,
    include_transactions: Annotated[bool, Field(description="同时取该用户的钱包流水")] = False,
    type: TxnType = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """某用户的钱包：余额、冻结金额、累计充值与累计消费，可附带该用户的流水分页。需要 app:user:read。"""
    result: dict[str, Any] = {"wallet": await USER_WALLET(appkey=app, userId=user_id)}
    if include_transactions:
        result["transactions"] = await USER_TRANSACTIONS(appkey=app, userId=user_id, query=q(type=type, page=page, limit=limit))
    return result


@server.tool(**READ("wallet"))
async def transactions(
    app: AppRef,
    user_id: Annotated[int | None, Field(gt=0, description="只看某个用户（管理端用户 ID），留空看全应用")] = None,
    type: TxnType = "",
    direction: Annotated[Literal["", "in", "out"], Field(description="in 入账 / out 出账，空为不限")] = "",
    keyword: Annotated[str, Field(description="按流水号 / 标题 / 备注 / 关联订单号搜索")] = "",
    start: IsoTime = "",
    end: IsoTime = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """按应用查钱包流水（全部用户的资金往来），可按用户、类型、方向、关键词、时间窗筛选。时间也接受 YYYY-MM-DD 或 Unix 秒。"""
    return await APP_TRANSACTIONS(appkey=app, query=q(
        userId=user_id, type=type, direction=direction, keyword=keyword, start=start, end=end, page=page, limit=limit,
    ))


@server.tool(**READ("wallet"))
async def stats(app: AppRef, start: IsoTime = "", end: IsoTime = "") -> Any:
    """应用资金面板：时间窗内的入账 / 出账汇总、按类型分布、余额总量。"""
    return await APP_STATS(appkey=app, query=q(start=start, end=end))


@server.tool(**DESTRUCTIVE("wallet"))
async def adjust(
    app: AppRef,
    user_id: UserID,
    amount: Annotated[str, Field(min_length=1, description="调整金额（十进制字符串，最多两位小数）：正数加钱、负数扣钱，如 \"10.50\" / \"-3\"")],
    reason: Annotated[str, Field(description="调整原因（写入流水备注，用户可见），留空为「管理员余额调整」")] = "",
) -> Any:
    """管理员调整用户钱包余额（类型 admin_adjust，立即生效，余额不足时扣减被拒绝）。

    动的是真金白银：执行前先向用户确认用户、金额与正负方向。后端没有幂等键，同一调用重复执行会重复记账，
    失败重试前先用 wallet_transactions 核对是否已经入账。反向操作需要再调一次相反金额。需要 app:write。
    """
    text = amount.strip()
    try:
        value = Decimal(text)
    except InvalidOperation as exc:
        raise ToolError("金额格式无效，请用十进制数字字符串，如 10.50 或 -3") from exc
    if not value.is_finite() or value == 0:
        raise ToolError("调整金额不能为 0")
    if value != value.quantize(Decimal("0.01")):
        raise ToolError("调整金额最多保留两位小数")
    if abs(value) > MAX_ADJUST:
        raise ToolError("单次调整金额不能超过 100,000,000")
    return await ADJUST(appkey=app, body=body(userId=user_id, amount=text, reason=reason))


@server.tool(**WRITE("wallet"))
async def email_receipt(
    app: AppRef,
    transaction_no: Annotated[str, Field(min_length=1, description="钱包流水号（WAL 开头）")],
    to: Annotated[str, Field(min_length=3, description="收件邮箱（可以是任意地址，操作记入审计）")],
    locale: Annotated[str, Field(description="凭证语言（BCP 47，如 zh-Hans / en / ja），留空按用户设置")] = "",
    document_type: DocumentType = "",
    timezone: Annotated[str, Field(description="凭证上时间的时区（IANA，如 Asia/Shanghai），留空按用户设置再空为 UTC")] = "",
) -> Any:
    """把一笔钱包流水的 PDF 凭证寄到指定邮箱（客服代开、对账留档）。邮件发出后无法撤回，先确认收件地址。"""
    return await RECEIPT_EMAIL(appkey=app, body=body(
        transactionNo=transaction_no.strip(), to=to.strip(), locale=locale or None,
        documentType=document_type or None, timezone=timezone or None,
    ))
