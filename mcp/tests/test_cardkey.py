"""卡密模块。"""

from __future__ import annotations

B = "/api/admin/apps/demo/card-keys"


async def test_catalog_stats_batches(backend, call):
    await call("cardkey_catalog", {"app": "demo"})
    assert backend.last().path == f"{B}/catalog"
    await call("cardkey_stats", {"app": "demo", "days": 7})
    assert (backend.last().path, backend.last().query) == (f"{B}/stats", {"days": "7"})
    await call("cardkey_batches", {"app": "demo", "kind": "login"})
    assert (backend.last().path, backend.last().query) == (f"{B}/batches", {"kind": "login"})
    await call("cardkey_batch", {"app": "demo", "batch_id": 3})
    assert backend.last().path == f"{B}/batches/3"


async def test_generate(backend, call, call_error):
    await call("cardkey_generate", {
        "app": "demo", "name": "双十一", "kind": "redeem", "count": 100, "code_prefix": "GIFT", "max_uses": 1,
        "rewards": [{"type": "vip_days", "amount": 30}, {"type": "balance", "money": "10.00"}], "validity_mode": "days_from_first_use", "validity_days": 90,
    })
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{B}/batches")
    assert req.json == {
        "name": "双十一", "kind": "redeem", "count": 100, "codePrefix": "GIFT", "maxUses": 1, "validityMode": "days_from_first_use",
        "validityDays": 90, "rewards": [{"type": "vip_days", "amount": 30}, {"type": "balance", "money": "10.00"}],
    }
    await call("cardkey_generate", {"app": "demo", "name": "导入", "kind": "login", "count": 5, "codes": ["AAAA-BBBB"]})
    assert backend.last().json == {"name": "导入", "kind": "login", "codes": ["AAAA-BBBB"]}
    assert "count" in await call_error("cardkey_generate", {"app": "demo", "name": "x", "kind": "redeem"})
    await call_error("cardkey_generate", {"app": "demo", "name": "x", "kind": "redeem", "count": 10001})


async def test_update_batch_merges(backend, call):
    backend.reply("GET", f"{B}/batches/3", {
        "id": 3, "name": "旧名", "kind": "redeem", "remark": "r", "faceValue": "9.9", "rewards": [{"type": "integral", "amount": 100}],
        "perUserLimit": 1, "audience": "all", "startsAt": "2026-10-01T00:00:00Z", "selfUnbindLimit": 0, "segments": 4, "total": 10,
    })
    await call("cardkey_update_batch", {"app": "demo", "batch_id": 3, "name": "新名", "clear": ["starts_at"]})
    req = backend.find("PUT", f"{B}/batches/3")
    assert req.json == {"name": "新名", "remark": "r", "faceValue": "9.9", "rewards": [{"type": "integral", "amount": 100}],
                        "perUserLimit": 1, "audience": "all", "selfUnbindLimit": 0}


async def test_batch_operations(backend, call):
    await call("cardkey_append", {"app": "demo", "batch_id": 3, "count": 20})
    assert (backend.last().path, backend.last().json) == (f"{B}/batches/3/append", {"count": 20})
    await call("cardkey_extract", {"app": "demo", "batch_id": 3, "count": 5, "note": "代理 A"})
    assert (backend.last().path, backend.last().json) == (f"{B}/batches/3/extract", {"count": 5, "note": "代理 A"})
    await call("cardkey_set_batch_status", {"app": "demo", "batch_id": 3, "enabled": False})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{B}/batches/3/status", {"enabled": False})
    await call("cardkey_delete_batch", {"app": "demo", "batch_id": 3})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/batches/3")


async def test_cards(backend, call, call_error):
    await call("cardkey_list", {"app": "demo", "batch_id": 3, "status": "expired", "distributed": "no"})
    assert (backend.last().path, backend.last().query) == (f"{B}/codes", {"batchId": "3", "status": "expired", "distributed": "no", "page": "1", "limit": "20"})
    data = await call("cardkey_get", {"app": "demo", "card_id": 8, "include_devices": True})
    assert set(data) == {"card", "devices"}
    assert backend.find("GET", f"{B}/codes/8/devices")

    await call("cardkey_update", {"app": "demo", "card_id": 8, "expiry_action": "extend", "extend_days": 30})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{B}/codes/8", {"expiryAction": "extend", "extendDays": 30})
    await call("cardkey_update", {"app": "demo", "card_id": 8, "remark": ""})
    assert backend.last().json == {"remark": ""}
    await call_error("cardkey_update", {"app": "demo", "card_id": 8})
    await call_error("cardkey_update", {"app": "demo", "card_id": 8, "expiry_action": "set"})

    for tool, path in (("cardkey_disable", "disable"), ("cardkey_restore", "restore"), ("cardkey_delete", "delete")):
        await call(tool, {"app": "demo", "ids": [1, 2], "reason": "测试"})
        assert (backend.last().path, backend.last().json) == (f"{B}/codes/{path}", {"ids": [1, 2], "reason": "测试"})
    await call_error("cardkey_disable", {"app": "demo", "ids": []})
    await call("cardkey_distribute", {"app": "demo", "ids": [1], "distributed": False})
    assert (backend.last().path, backend.last().json) == (f"{B}/codes/distribute", {"ids": [1], "distributed": False, "note": ""})


async def test_devices_redemptions_redeem(backend, call, call_error):
    await call("cardkey_unbind_devices", {"app": "demo", "card_id": 8, "device_id": " dev-1 "})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/codes/8/devices/dev-1")
    await call("cardkey_unbind_devices", {"app": "demo", "card_id": 8})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/codes/8/devices")

    await call("cardkey_redemptions", {"app": "demo", "source": "admin", "to_time": "2026-10-28T00:00:00Z", "limit": 50})
    assert (backend.last().path, backend.last().query) == (f"{B}/redemptions", {"source": "admin", "to": "2026-10-28T00:00:00Z", "page": "1", "limit": "50"})

    await call("cardkey_redeem", {"app": "demo", "code": " VIP-AAAA ", "account": "alice"})
    assert (backend.last().path, backend.last().json) == (f"{B}/redeem", {"code": "VIP-AAAA", "account": "alice"})
    await call("cardkey_redeem", {"app": "demo", "code": "VIP-AAAA", "user_id": 7})
    assert backend.last().json == {"code": "VIP-AAAA", "userId": 7}
    await call_error("cardkey_redeem", {"app": "demo", "code": "VIP-AAAA"})
