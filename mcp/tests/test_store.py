"""积分商城模块。"""

from __future__ import annotations

B = "/api/admin/apps/demo/points-store"

ITEM = {
    "id": 5, "appid": 42, "title": "会员 7 天", "cost": 700, "rewards": [{"type": "vip_days", "amount": 7}],
    "stock": 10, "currentStock": 8, "restockPeriod": "day", "restockQuantity": 10, "globalLimitPeriod": "day",
    "globalLimitCount": 0, "limitPeriod": "day", "limitCount": 1, "userTotalLimit": 0, "audience": "all",
    "minLevel": 0, "status": "active", "sortOrder": 10, "soldCount": 3, "redemptions": 3, "createdAt": "2026-10-01T00:00:00Z",
    "endsAt": "2026-12-31T00:00:00Z",
}


async def test_catalog_settings(backend, call):
    await call("store_catalog", {"app": "demo"})
    assert backend.last().path == f"{B}/catalog"
    await call("store_settings", {"app": "demo"})
    assert backend.last().path == f"{B}/settings"
    await call("store_set_enabled", {"app": "demo", "enabled": True})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{B}/settings", {"enabled": True})
    await call("store_stats", {"app": "demo", "tz": "Asia/Shanghai"})
    assert (backend.last().path, backend.last().query) == (f"{B}/stats", {"days": "14", "tz": "Asia/Shanghai"})


async def test_create_item(backend, call, call_error):
    await call("store_create_item", {
        "app": "demo", "title": "余额 5 元", "cost": 500,
        "rewards": [{"type": "balance", "money": "5.00"}, {"type": "feature", "tag": "music", "amount": 0}],
        "stock": 100, "limit_period": "week", "limit_count": 1, "daily_open_minute": 600, "daily_close_minute": 1320, "status": "inactive",
    })
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{B}/items")
    assert req.json == {
        "title": "余额 5 元", "cost": 500, "stock": 100, "limitPeriod": "week", "limitCount": 1,
        "dailyOpenMinute": 600, "dailyCloseMinute": 1320, "status": "inactive",
        "rewards": [{"type": "balance", "money": "5.00"}, {"type": "feature", "amount": 0, "tag": "music"}],
    }
    await call_error("store_create_item", {"app": "demo", "title": "x", "cost": 1, "rewards": []})
    await call_error("store_create_item", {"app": "demo", "title": "x", "cost": 1, "rewards": [{"type": "integral", "amount": 1}]})


async def test_update_item_merges(backend, call, call_error):
    backend.reply("GET", f"{B}/items", [ITEM])
    await call("store_update_item", {"app": "demo", "item_id": 5, "cost": 600, "clear": ["ends_at"]})
    req = backend.find("PUT", f"{B}/items/5")
    assert req.json == {
        "title": "会员 7 天", "cost": 600, "stock": 8, "restockPeriod": "day", "restockQuantity": 10, "globalLimitPeriod": "day",
        "globalLimitCount": 0, "limitPeriod": "day", "limitCount": 1, "userTotalLimit": 0, "audience": "all", "minLevel": 0,
        "status": "active", "sortOrder": 10, "rewards": [{"type": "vip_days", "amount": 7}],
    }
    await call("store_update_item", {"app": "demo", "item_id": 5, "rewards": [{"type": "vip_plan", "ref_id": 2}], "clear": ["stock"]})
    req = backend.last()
    assert req.json["rewards"] == [{"type": "vip_plan", "refId": 2}] and "stock" not in req.json and req.json["endsAt"] == "2026-12-31T00:00:00Z"
    assert "找不到" in await call_error("store_update_item", {"app": "demo", "item_id": 99, "cost": 1})


async def test_status_order_delete(backend, call):
    await call("store_set_item_status", {"app": "demo", "item_id": 5, "active": False})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{B}/items/5/status", {"active": False})
    await call("store_reorder_items", {"app": "demo", "item_ids": [3, 1, 2]})
    assert (backend.last().path, backend.last().json) == (f"{B}/item-order", {"itemIds": [3, 1, 2]})
    await call("store_delete_item", {"app": "demo", "item_id": 5})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/items/5")
    await call("store_items", {"app": "demo"})
    assert (backend.last().method, backend.last().path) == ("GET", f"{B}/items")


async def test_redemptions(backend, call, call_error):
    await call("store_redemptions", {"app": "demo", "user_id": 7, "status": "pending", "from_time": "2026-10-01T00:00:00Z"})
    assert (backend.last().path, backend.last().query) == (f"{B}/redemptions", {"userId": "7", "status": "pending", "from": "2026-10-01T00:00:00Z", "page": "1", "limit": "20"})
    await call_error("store_redemptions", {"app": "demo", "status": "done"})
    await call("store_fulfill", {"app": "demo", "redemption_id": 11, "note": "兑换码 ABC"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{B}/redemptions/11/fulfill", {"note": "兑换码 ABC"})
    await call("store_cancel", {"app": "demo", "redemption_id": 11})
    assert (backend.last().path, backend.last().json) == (f"{B}/redemptions/11/cancel", {"note": ""})
