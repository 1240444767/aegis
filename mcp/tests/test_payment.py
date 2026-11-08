"""支付与交易模块。"""

from __future__ import annotations

P = "/api/admin/app/payment-config"


async def test_methods_and_configs(backend, call):
    await call("payment_methods")
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{P}/methods", {})
    await call("payment_configs", {"app": "demo", "payment_method": "epay", "enabled_only": True})
    assert (backend.last().path, backend.last().json) == (f"{P}/list", {"appid": 42, "payment_method": "epay", "enabled_only": True})
    await call("payment_configs", {"app": "demo"})
    assert backend.last().json == {"appid": 42}
    await call("payment_config", {"app": "demo", "config_id": 3})
    assert (backend.last().path, backend.last().json) == (f"{P}/detail", {"appid": 42, "config_id": 3})


async def test_create_update_delete_config(backend, call):
    await call("payment_create_config", {"app": "demo", "payment_method": "stripe", "config_name": "main", "config_data": {"secretKey": "sk"}})
    assert (backend.last().path, backend.last().json) == (f"{P}/create", {
        "appid": 42, "payment_method": "stripe", "config_name": "main", "config_data": {"secretKey": "sk"}, "enabled": True,
    })

    backend.reply("POST", f"{P}/detail", {"id": 3, "config_data": {"pid": "1", "key": "old"}})
    await call("payment_update_config", {"app": "demo", "config_id": 3, "config_data": {"key": "new"}, "enabled": False})
    assert backend.find("POST", f"{P}/update").json == {"appid": 42, "config_id": 3, "config_data": {"pid": "1", "key": "new"}, "enabled": False}
    await call("payment_update_config", {"app": "demo", "config_id": 3, "config_data": {"key": "only"}, "merge_config_data": False})
    assert backend.last().json == {"appid": 42, "config_id": 3, "config_data": {"key": "only"}}
    await call("payment_update_config", {"app": "demo", "config_id": 3, "description": "备用"})
    assert backend.last().json == {"appid": 42, "config_id": 3, "description": "备用"}

    await call("payment_delete_config", {"app": "demo", "config_id": 3})
    assert (backend.last().path, backend.last().json) == (f"{P}/delete", {"appid": 42, "config_id": 3})
    await call("payment_test_config", {"app": "demo", "config_id": 3})
    assert backend.last().path == f"{P}/test"
    await call("payment_init_epay", {"app": "demo", "epay_config": {"pid": "1001", "key": "k", "apiUrl": "https://pay.test"}})
    assert (backend.last().path, backend.last().json) == (f"{P}/epay/init", {"appid": 42, "epay_config": {"pid": "1001", "key": "k", "apiUrl": "https://pay.test"}})


async def test_orders_and_receipts(backend, call):
    await call("payment_orders", {"app": "demo", "status": "paid", "user_id": 5, "limit": 10})
    assert (backend.last().path, backend.last().json) == (f"{P}/orders/list", {"appid": 42, "status": "paid", "user_id": 5, "page": 1, "limit": 10})
    data = await call("payment_order", {"app": "demo", "order_no": "P001", "include_refunds": True, "include_refundable": True})
    assert set(data) == {"order", "refunds", "refundable"}
    for path in ("orders/detail", "refunds/order", "refunds/refundable"):
        assert backend.find("POST", f"{P}/{path}").json == {"appid": 42, "order_no": "P001"}
    await call("payment_email_receipt", {"app": "demo", "order_no": "P001", "email": "a@b.c", "document_type": "invoice"})
    assert (backend.last().path, backend.last().json) == (f"{P}/orders/receipt/email", {"appid": 42, "order_no": "P001", "email": "a@b.c", "documentType": "invoice"})
    await call("payment_receipt_options", {"app": "demo"})
    assert (backend.last().path, backend.last().json) == (f"{P}/receipt/options", {"appid": 42})


async def test_refunds(backend, call, call_error):
    await call("payment_refunds", {"app": "demo", "status": "processing"})
    assert (backend.last().path, backend.last().json) == (f"{P}/refunds/list", {"appid": 42, "status": "processing", "page": 1, "limit": 20})
    await call("payment_refund", {"app": "demo", "order_no": "P001"})
    assert (backend.last().path, backend.last().json) == (f"{P}/refunds/create", {"appid": 42, "order_no": "P001", "reverse_fulfillment": True})
    await call("payment_refund", {"app": "demo", "order_no": "P001", "amount": "5.00", "reason": "部分退款", "reverse_fulfillment": False})
    assert backend.last().json == {"appid": 42, "order_no": "P001", "amount": "5.00", "reason": "部分退款", "reverse_fulfillment": False}
    assert "大于 0" in await call_error("payment_refund", {"app": "demo", "order_no": "P001", "amount": "-1"})
    await call_error("payment_refund", {"app": "demo", "order_no": "P001", "amount": "abc"})
    await call("payment_sync_refund", {"app": "demo", "refund_no": "R001"})
    assert (backend.last().path, backend.last().json) == (f"{P}/refunds/sync", {"appid": 42, "refund_no": "R001"})


async def test_commerce(backend, call):
    backend.reply("GET", "/api/admin/apps/demo/commerce", {"integralPerCurrency": 100, "receiptEmailOnPaid": False, "walletCurrency": "CNY"})
    data = await call("payment_commerce", {"app": "demo", "include_overview": True, "start": "2026-10-01"})
    assert set(data) == {"settings", "overview"}
    assert backend.find("GET", "/api/admin/apps/demo/commerce/overview").query == {"start": "2026-10-01"}

    await call("payment_update_commerce", {"app": "demo", "receipt_email_on_paid": True, "wallet_currency": "usd"})
    req = backend.last()
    assert (req.method, req.path) == ("PUT", "/api/admin/apps/demo/commerce")
    assert req.json == {"integralPerCurrency": 100, "receiptEmailOnPaid": True, "receiptLocale": "", "walletCurrency": "USD"}
