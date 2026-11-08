"""会员模块。"""

from __future__ import annotations

BASE = "/api/admin/apps/demo/vip"


async def test_plans_list_filter_and_detail(backend, call, call_error):
    backend.reply("GET", f"{BASE}/plans", [{"id": 1, "name": "月卡", "isActive": True}, {"id": 2, "name": "旧卡", "isActive": False}])
    assert len(await call("vip_plans", {"app": "demo"})) == 2
    assert [p["id"] for p in await call("vip_plans", {"app": "demo", "active_only": True})] == [1]
    assert (await call("vip_plans", {"app": "demo", "plan_id": 2}))["name"] == "旧卡"
    assert "找不到" in await call_error("vip_plans", {"app": "demo", "plan_id": 9})


async def test_save_plan_create_and_update(backend, call, call_error):
    await call("vip_save_plan", {"app": "demo", "name": "月卡", "price": "9.90", "duration_days": 30, "features": ["export"], "bonus_integral": 10})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{BASE}/plans")
    assert req.json == {"id": 0, "name": "月卡", "price": "9.90", "durationDays": 30, "features": ["export"], "bonusIntegral": 10}
    await call("vip_save_plan", {"app": "demo", "name": "永久", "price": "199", "lifetime": True})
    assert backend.last().json == {"id": 0, "name": "永久", "price": "199", "lifetime": True}
    await call("vip_save_plan", {"app": "demo", "plan_id": 3, "description": "", "kind": "trial", "trial_device_limited": True})
    assert backend.last().json == {"id": 3, "description": "", "kind": "trial", "trialDeviceLimited": True}
    assert "name" in await call_error("vip_save_plan", {"app": "demo", "price": "1", "duration_days": 1})
    assert "price" in await call_error("vip_save_plan", {"app": "demo", "name": "x", "duration_days": 1})
    assert "duration_days" in await call_error("vip_save_plan", {"app": "demo", "name": "x", "price": "1"})
    await call_error("vip_save_plan", {"app": "demo", "plan_id": 3, "price": "abc"})


async def test_set_plan_active_and_delete(backend, call):
    await call("vip_set_plan_active", {"app": "demo", "plan_id": 4, "active": False})
    assert (backend.last().path, backend.last().json) == (f"{BASE}/plans", {"id": 4, "isActive": False})
    await call("vip_delete_plan", {"app": "demo", "plan_id": 4})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{BASE}/plans/4")


async def test_features(backend, call, call_error):
    await call("vip_features", {"app": "demo"})
    assert (backend.last().method, backend.last().path) == ("GET", f"{BASE}/features")
    await call("vip_save_feature", {"app": "demo", "tag": "online_music", "name": "在线听歌", "is_active": True})
    assert backend.last().json == {"tag": "online_music", "name": "在线听歌", "isActive": True}
    await call_error("vip_save_feature", {"app": "demo", "tag": "Bad Tag"})
    await call("vip_delete_feature", {"app": "demo", "tag": "online_music"})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{BASE}/features/online_music")


async def test_grant_and_preview(backend, call, call_error):
    await call("vip_grant_preview", {"app": "demo", "user_id": 7, "plan_id": 2, "quantity": 3})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{BASE}/grant/preview", {"userId": 7, "planId": 2, "quantity": 3, "reason": ""})
    await call("vip_grant", {"app": "demo", "user_id": 7, "days": 30, "features": ["export"], "reason": "补偿", "owned_policy": "grant", "upgrade_policy": "keep"})
    req = backend.last()
    assert (req.path, req.json) == (f"{BASE}/grant", {
        "userId": 7, "days": 30, "features": ["export"], "reason": "补偿", "ownedPolicy": "grant", "upgradePolicy": "keep",
    })
    await call("vip_grant", {"app": "demo", "user_id": 7, "lifetime": True})
    assert backend.last().json == {"userId": 7, "lifetime": True, "reason": ""}
    await call_error("vip_grant", {"app": "demo", "user_id": 7})
    await call_error("vip_grant", {"app": "demo", "user_id": 7, "plan_id": 2, "days": 3})
    await call_error("vip_grant", {"app": "demo", "user_id": 7, "plan_id": 2, "quantity": 101})
    await call_error("vip_grant", {"app": "demo", "user_id": 7, "days": 3, "owned_policy": "keep"})


async def test_grant_backend_error(backend, call_error):
    backend.reply("POST", f"{BASE}/grant", code=40480, status=404, message="套餐不存在")
    assert "40480" in await call_error("vip_grant", {"app": "demo", "user_id": 7, "plan_id": 99})


async def test_revoke(backend, call, call_error):
    await call("vip_revoke", {"app": "demo", "user_id": 7, "days": 5, "reason": "误发"})
    assert (backend.last().path, backend.last().json) == (f"{BASE}/revoke", {"userId": 7, "lifetime": False, "days": 5, "reason": "误发"})
    await call("vip_revoke", {"app": "demo", "user_id": 7, "lifetime": True, "days": 5})
    assert backend.last().json == {"userId": 7, "lifetime": True, "reason": ""}
    await call_error("vip_revoke", {"app": "demo", "user_id": 7})


async def test_entitlement_transactions_conversions(backend, call):
    await call("vip_entitlement", {"app": "demo", "user_id": 7})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/entitlement", {"userId": "7"})
    await call("vip_transactions", {"app": "demo"})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/transactions", {"page": "1", "limit": "20"})
    await call("vip_conversions", {"app": "demo", "user_id": 7, "page": 2, "limit": 50})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/conversions", {"userId": "7", "page": "2", "limit": "50"})


async def test_trial(backend, call):
    await call("vip_trial_claims", {"app": "demo", "page": 3})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/trial/claims", {"page": "3", "limit": "20"})
    await call("vip_claim_trial", {"app": "demo", "user_id": 7})
    assert (backend.last().method, backend.last().json) == ("POST", {"userId": 7})
    await call("vip_reset_trial", {"app": "demo", "user_id": 7})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{BASE}/trial/claims/7")


async def test_conversion_policy_merges(backend, call):
    backend.reply("GET", f"{BASE}/conversion-policy", {
        "policy": {"appid": 42, "owned": "convert", "upgrade": "convert", "perVipDay": 10, "perFeatureDay": 5,
                   "perFeatureHour": 10, "perLifetime": 3650, "restoreOnRevoke": True, "configured": True},
        "ownedModes": [], "upgradeModes": [], "defaults": {},
    })
    assert (await call("vip_conversion_policy", {"app": "demo"}))["policy"]["perVipDay"] == 10
    await call("vip_save_conversion_policy", {"app": "demo", "upgrade": "keep", "per_vip_day": 20, "restore_on_revoke": False})
    req = backend.find("PUT", f"{BASE}/conversion-policy")
    assert req.json == {"owned": "convert", "upgrade": "keep", "perVipDay": 20, "perFeatureDay": 5,
                        "perFeatureHour": 10, "perLifetime": 3650, "restoreOnRevoke": False}


async def test_tiers(backend, call, call_error):
    await call("vip_tiers", {"app": "demo"})
    assert (backend.last().method, backend.last().path) == ("GET", f"{BASE}/tiers")
    await call("vip_save_tier", {"app": "demo", "name": "永久会员", "features": ["export"]})
    assert (backend.last().path, backend.last().json) == (f"{BASE}/tiers", {"id": 0, "name": "永久会员", "features": ["export"]})
    assert "name" in await call_error("vip_save_tier", {"app": "demo"})
    await call("vip_delete_tier", {"app": "demo", "tier_id": 3, "move_to": 5})
    req = backend.last()
    assert (req.method, req.path, req.query.get("moveTo")) == ("DELETE", f"{BASE}/tiers/3", "5")
    await call("vip_save_plan", {"app": "demo", "plan_id": 2, "tier_id": 5})
    assert backend.last().json == {"id": 2, "tierId": 5}
    await call("vip_grant", {"app": "demo", "user_id": 7, "lifetime": True, "tier_id": 5})
    assert backend.last().json == {"userId": 7, "lifetime": True, "tierId": 5, "reason": ""}
    assert "tier_id" in await call_error("vip_grant", {"app": "demo", "user_id": 7, "plan_id": 1, "tier_id": 5})
