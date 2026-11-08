"""钱包模块。"""

from __future__ import annotations

A = "/api/admin/apps/demo"


async def test_get_wallet(backend, call):
    backend.reply("GET", f"{A}/users/7/wallet", {"balance": "12.50"})
    assert await call("wallet_get", {"app": "demo", "user_id": 7}) == {"wallet": {"balance": "12.50"}}
    data = await call("wallet_get", {"app": "demo", "user_id": 7, "include_transactions": True, "type": "admin_adjust", "limit": 5})
    assert set(data) == {"wallet", "transactions"}
    req = backend.find("GET", f"{A}/users/7/wallet/transactions")
    assert req.query == {"type": "admin_adjust", "page": "1", "limit": "5"}


async def test_transactions_and_stats(backend, call, call_error):
    await call("wallet_transactions", {"app": "demo", "user_id": 3, "direction": "in", "keyword": "WAL1", "start": "2026-10-01", "end": "2026-10-28"})
    req = backend.last()
    assert (req.method, req.path) == ("GET", f"{A}/wallet/transactions")
    assert req.query == {"userId": "3", "direction": "in", "keyword": "WAL1", "start": "2026-10-01", "end": "2026-10-28", "page": "1", "limit": "20"}
    await call_error("wallet_transactions", {"app": "demo", "type": "gift"})
    await call("wallet_stats", {"app": "demo"})
    assert (backend.last().path, backend.last().query) == (f"{A}/wallet/stats", {})


async def test_adjust(backend, call, call_error):
    await call("wallet_adjust", {"app": "demo", "user_id": 7, "amount": "-3.50", "reason": "退还差价"})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{A}/wallet/adjust", {"userId": 7, "amount": "-3.50", "reason": "退还差价"})
    assert "不能为 0" in await call_error("wallet_adjust", {"app": "demo", "user_id": 7, "amount": "0.00"})
    assert "两位小数" in await call_error("wallet_adjust", {"app": "demo", "user_id": 7, "amount": "1.005"})
    assert "格式无效" in await call_error("wallet_adjust", {"app": "demo", "user_id": 7, "amount": "abc"})
    await call_error("wallet_adjust", {"app": "demo", "user_id": 7, "amount": "100000000.01"})


async def test_email_receipt(backend, call):
    await call("wallet_email_receipt", {"app": "demo", "transaction_no": "WAL001", "to": "a@b.c", "locale": "zh-Hans"})
    req = backend.last()
    assert (req.path, req.json) == (f"{A}/wallet/receipt/email", {"transactionNo": "WAL001", "to": "a@b.c", "locale": "zh-Hans"})
