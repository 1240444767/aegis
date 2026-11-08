"""签到模块。"""

from __future__ import annotations

A = "/api/admin/apps/demo"


async def test_stats_and_records(backend, call, call_error):
    await call("signin_stats", {"app": "demo"})
    assert (backend.last().path, backend.last().query) == (f"{A}/signin/stats", {"days": "14"})
    await call("signin_records", {"app": "demo", "user_id": 7, "bonus_type": "weekend", "date_from": "2026-10-01", "date_to": "2026-10-28"})
    assert (backend.last().path, backend.last().query) == (f"{A}/signin/records", {
        "userId": "7", "bonusType": "weekend", "dateFrom": "2026-10-01", "dateTo": "2026-10-28", "page": "1", "limit": "20"})
    assert "晚于" in await call_error("signin_records", {"app": "demo", "date_from": "2026-10-28", "date_to": "2026-10-01"})
    await call_error("signin_records", {"app": "demo", "date_from": "2026/10/01"})


async def test_reward_policy_read_and_templates(backend, call):
    await call("signin_reward_policy", {"app": "demo"})
    assert backend.last().path == f"{A}/signin-reward"
    await call("signin_reward_templates")
    assert (backend.last().method, backend.last().path) == ("GET", "/api/admin/apps/signin-reward/templates")


async def test_reward_test(backend, call):
    await call("signin_test_reward", {"app": "demo", "consecutive_days": 7, "simulate_days": 30, "policy": {"baseIntegral": 5}})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{A}/signin-reward/test")
    assert req.json == {"policy": {"baseIntegral": 5}, "consecutiveDays": 7, "totalSignIns": 0, "userExperience": 0, "simulateDays": 30}
    await call("signin_test_reward", {"app": "demo"})
    assert backend.last().json == {"consecutiveDays": 1, "totalSignIns": 0, "userExperience": 0}


async def test_update_and_reset_reward_policy(backend, call, call_error):
    backend.reply("GET", f"{A}/signin-reward", {"appid": 42, "appName": "演示", "policy": {
        "name": "默认", "enabled": True, "baseIntegral": 1, "rules": [{"key": "r1"}], "isDefault": True}})
    await call("signin_update_reward_policy", {"app": "demo", "policy": {"baseIntegral": 3}})
    req = backend.find("PUT", f"{A}/signin-reward")
    assert req.json == {"policy": {"name": "默认", "enabled": True, "baseIntegral": 3, "rules": [{"key": "r1"}]}}
    await call("signin_update_reward_policy", {"app": "demo", "policy": {"name": "全新", "rules": []}, "merge": False})
    assert backend.last().json == {"policy": {"name": "全新", "rules": []}}
    await call_error("signin_update_reward_policy", {"app": "demo", "policy": {}})

    await call("signin_reset_reward_policy", {"app": "demo"})
    assert (backend.last().method, backend.last().path) == ("POST", f"{A}/signin-reward/reset")


async def test_auto_sign(backend, call, call_error):
    backend.reply("GET", f"{A}/auto-sign", {"policy": {"enabled": False, "audience": "all", "includeTrial": True, "isDefault": True}, "enabledUsers": 3})
    await call("signin_auto_sign", {"app": "demo"})
    assert backend.last().path == f"{A}/auto-sign"
    await call("signin_update_auto_sign", {"app": "demo", "enabled": True, "audience": "vip", "include_trial": False})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("PUT", f"{A}/auto-sign", {"policy": {"enabled": True, "audience": "vip", "includeTrial": False}})
    assert "feature_tag" in await call_error("signin_update_auto_sign", {"app": "demo", "audience": "feature"})
    await call("signin_update_auto_sign", {"app": "demo", "audience": "feature", "feature_tag": "music"})
    assert backend.last().json["policy"]["featureTag"] == "music"
