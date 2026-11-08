"""users 模块：每个工具发出的请求（方法、路径、查询串、请求体）与前置校验。"""

from __future__ import annotations

import pytest

A = "/api/admin/apps/demo"
U = f"{A}/users/7"

# (工具, 参数, 方法, 路径, 查询串, 请求体)
CASES = [
    ("users_search", {"app": "demo", "keyword": "bob", "enabled": False, "ids": [1, 2], "sort": "integral", "order": "asc", "register_ip": "1.2.3.4", "markcode": "m1", "created_to": "2026-10-01"},
     "GET", f"{A}/users", {"keyword": "bob", "enabled": "false", "ids": "1,2", "sort": "integral", "order": "asc", "registerIp": "1.2.3.4", "markcode": "m1", "createdTo": "2026-10-01", "page": "1", "limit": "20"}, None),
    ("users_get", {"app": "demo", "user_id": 7}, "GET", U, {}, None),
    ("users_devices", {"app": "demo", "user_id": 7, "ids": ["a", "b"]}, "GET", f"{A}/users/devices", {"userId": "7", "ids": "a,b", "limit": "30"}, None),
    ("users_update_profile", {"app": "demo", "user_id": 7, "nickname": "N", "email": "", "clear_birthday": True},
     "PUT", f"{U}/profile", {}, {"nickname": "N", "email": "", "clearBirthday": True}),
    ("users_set_status", {"app": "demo", "user_id": 7, "enabled": False, "disabled_reason": "spam", "disabled_end_time": "2026-11-01T00:00:00Z"},
     "PUT", f"{U}/status", {}, {"enabled": False, "disabledReason": "spam", "disabledEndTime": "2026-11-01T00:00:00Z"}),
    ("users_set_status_batch", {"app": "demo", "user_ids": [1, 2], "enabled": True, "clear_disabled_end_time": True},
     "PUT", f"{A}/users/status/batch", {}, {"userIds": [1, 2], "enabled": True, "clearDisabledEndTime": True}),
    ("users_delete", {"app": "demo", "user_id": 7}, "DELETE", U, {}, None),
    ("users_reset_password", {"app": "demo", "user_id": 7, "new_password": "Secret#1"}, "POST", f"{U}/reset-password", {}, {"newPassword": "Secret#1"}),
    ("users_remove_avatar", {"app": "demo", "user_id": 7}, "DELETE", f"{U}/avatar", {}, None),
    ("users_sessions", {"app": "demo", "user_id": 7}, "GET", f"{U}/sessions", {}, None),
    ("users_bans", {"app": "demo", "user_id": 7, "status": "revoked"}, "GET", f"{U}/bans", {"status": "revoked", "page": "1", "limit": "20"}, None),
    ("users_bans", {"app": "demo", "user_id": 7, "active_only": True}, "GET", f"{U}/bans/active", {}, None),
    ("users_ban", {"app": "demo", "user_id": 7, "reason": "外挂", "ban_type": "temporary", "ban_scope": "login", "end_at": "2026-12-01T00:00:00Z", "evidence": {"k": 1}},
     "POST", f"{U}/bans", {}, {"reason": "外挂", "banType": "temporary", "banScope": "login", "endAt": "2026-12-01T00:00:00Z", "evidence": {"k": 1}}),
    ("users_ban_batch", {"app": "demo", "user_ids": [3, 4], "reason": "刷单"}, "POST", f"{A}/users/bans/batch", {}, {"userIds": [3, 4], "reason": "刷单"}),
    ("users_unban", {"app": "demo", "user_id": 7, "ban_id": 9, "reason": "误封"}, "POST", f"{U}/bans/9/revoke", {}, {"reason": "误封"}),
    ("users_oauth2_grants", {"app": "demo", "user_id": 7}, "GET", f"{U}/oauth2-grants", {}, None),
    ("users_revoke_oauth2_grants", {"app": "demo", "user_id": 7, "client_id": "c1"}, "DELETE", f"{U}/oauth2-grants", {"clientId": "c1"}, None),
    ("users_login_baseline", {"app": "demo", "user_id": 7}, "GET", f"{A}/login-baseline/7", {}, None),
    ("users_reset_login_baseline", {"app": "demo", "user_id": 7}, "DELETE", f"{A}/login-baseline/7", {}, None),
    ("users_deletion_settings", {"app": "demo"}, "GET", f"{A}/account-deletion/settings", {}, None),
    ("users_deletion_stats", {"app": "demo", "days": 14, "tz": "Asia/Shanghai"}, "GET", f"{A}/account-deletion/stats", {"days": "14", "tz": "Asia/Shanghai"}, None),
    ("users_deletion_requests", {"app": "demo", "status": "scheduled", "source": "admin", "from_time": "2026-10-01T00:00:00Z"},
     "GET", f"{A}/account-deletion/requests", {"status": "scheduled", "source": "admin", "from": "2026-10-01T00:00:00Z", "page": "1", "limit": "20"}, None),
    ("users_deletion_requests", {"app": "demo", "request_id": 5}, "GET", f"{A}/account-deletion/requests/5", {}, None),
    ("users_decide_deletion", {"app": "demo", "request_id": 5, "action": "approve", "note": "ok"}, "POST", f"{A}/account-deletion/requests/5/approve", {}, {"note": "ok"}),
    ("users_decide_deletion", {"app": "demo", "request_id": 5, "action": "reject", "note": "资料不全"}, "POST", f"{A}/account-deletion/requests/5/reject", {}, {"note": "资料不全"}),
    ("users_decide_deletion", {"app": "demo", "request_id": 5, "action": "cancel", "notify": False}, "POST", f"{A}/account-deletion/requests/5/cancel", {}, {"note": "", "notify": False}),
    ("users_execute_deletion", {"app": "demo", "request_id": 5, "confirm": "bob"}, "POST", f"{A}/account-deletion/requests/5/execute", {}, {"confirm": "bob"}),
    ("users_reschedule_deletion", {"app": "demo", "request_id": 5, "scheduled_at": "2026-11-11T00:00:00Z", "notify": False},
     "POST", f"{A}/account-deletion/requests/5/reschedule", {}, {"scheduledAt": "2026-11-11T00:00:00Z", "notify": False}),
    ("users_user_deletion", {"app": "demo", "user_id": 7}, "GET", f"{U}/account-deletion", {}, None),
    ("users_create_deletion", {"app": "demo", "user_id": 7, "mode": "cooling", "reason_code": "privacy", "cooling_days": 7},
     "POST", f"{U}/account-deletion", {}, {"mode": "cooling", "reasonCode": "privacy", "coolingDays": 7}),
    ("users_create_deletion", {"app": "demo", "user_id": 7, "mode": "immediate", "reason": "本人要求", "confirm": "bob", "notify": False},
     "POST", f"{U}/account-deletion", {}, {"mode": "immediate", "reason": "本人要求", "confirm": "bob", "notify": False}),
    ("users_deletion_tombstones", {"app": "demo", "keyword": "b"}, "GET", f"{A}/account-deletion/tombstones", {"keyword": "b", "page": "1", "limit": "20"}, None),
    ("users_release_tombstone", {"app": "demo", "tombstone_id": 3}, "DELETE", f"{A}/account-deletion/tombstones/3", {}, None),
    ("users_device_limit_overview", {"app": "demo", "days": 7, "tz": "UTC"}, "GET", f"{A}/device-limit", {"days": "7", "tz": "UTC"}, None),
    ("users_simulate_device_limit", {"app": "demo", "platform": "android", "user_id": 7, "can_ask": True},
     "POST", f"{A}/device-limit/simulate", {}, {"userId": 7, "platform": "android", "known": False, "newDevicesInWindow": 0, "canAsk": True}),
    ("users_device_limit_events", {"app": "demo", "action": "kicked", "dimension": "class", "to_time": "2026-10-28T00:00:00Z"},
     "GET", f"{A}/device-limit/events", {"action": "kicked", "dimension": "class", "to": "2026-10-28T00:00:00Z", "page": "1", "limit": "20"}, None),
    ("users_device_limit_overrides", {"app": "demo"}, "GET", f"{A}/device-limit/overrides", {"page": "1", "limit": "20"}, None),
    ("users_set_device_limit_override", {"app": "demo", "user_id": 7, "max_devices": 5, "class_limits": {"mobile": 2}, "expires_at": "2027-01-01T00:00:00Z"},
     "PUT", f"{A}/device-limit/overrides/7", {}, {"exempt": False, "maxDevices": 5, "note": "", "classLimits": {"mobile": 2}, "expiresAt": "2027-01-01T00:00:00Z"}),
    ("users_delete_device_limit_override", {"app": "demo", "user_id": 7}, "DELETE", f"{A}/device-limit/overrides/7", {}, None),
    ("users_device_limit_user", {"app": "demo", "user_id": 7}, "GET", f"{A}/device-limit/users/7", {}, None),
    ("users_remove_devices", {"app": "demo", "user_id": 7, "device_keys": ["k1", "k2"]}, "POST", f"{A}/device-limit/users/7/remove", {}, {"deviceKeys": ["k1", "k2"]}),
    ("users_enforce_device_limit", {"app": "demo", "user_id": 7}, "POST", f"{A}/device-limit/users/7/enforce", {}, None),
    ("users_reset_device_ledger", {"app": "demo", "user_id": 7, "key": "ua:abc"}, "DELETE", f"{A}/device-limit/users/7/ledger", {"key": "ua:abc"}, None),
    ("users_cloud_users", {"app": "demo", "sort": "items", "frozen": True}, "GET", f"{A}/cloud-storage/users", {"sort": "items", "frozen": "true", "page": "1", "limit": "20"}, None),
    ("users_cloud_users", {"app": "demo", "sort": "usage", "frozen": False}, "GET", f"{A}/cloud-storage/users", {"frozen": "false", "page": "1", "limit": "20"}, None),
    ("users_cloud_storage", {"app": "demo", "user_id": 7}, "GET", f"{U}/cloud-storage", {}, None),
    ("users_save_cloud_storage", {"app": "demo", "user_id": 7, "frozen": True, "frozen_reason": "滥用"},
     "PUT", f"{U}/cloud-storage", {}, {"quotaBytes": None, "frozen": True, "frozenReason": "滥用", "note": ""}),
    ("users_purge_cloud_storage", {"app": "demo", "user_id": 7}, "DELETE", f"{U}/cloud-storage", {}, None),
    ("users_cloud_items", {"app": "demo", "user_id": 7, "namespace": "notes", "status": "deleted"},
     "GET", f"{U}/cloud-storage/items", {"namespace": "notes", "status": "deleted", "page": "1", "limit": "20"}, None),
    ("users_cloud_items", {"app": "demo", "user_id": 7, "item_id": 11, "revision": 3}, "GET", f"{U}/cloud-storage/items/11", {"revision": "3"}, None),
    ("users_cloud_items", {"app": "demo", "user_id": 7, "item_id": 11, "revisions": True}, "GET", f"{U}/cloud-storage/items/11/revisions", {}, None),
    ("users_cloud_item_link", {"app": "demo", "user_id": 7, "item_id": 11, "download": True}, "POST", f"{U}/cloud-storage/items/11/link", {}, {"download": True}),
    ("users_delete_cloud_item", {"app": "demo", "user_id": 7, "item_id": 11}, "DELETE", f"{U}/cloud-storage/items/11", {}, None),
    ("users_delete_cloud_item", {"app": "demo", "user_id": 7, "item_id": 11, "permanent": True}, "DELETE", f"{U}/cloud-storage/items/11", {"permanent": "true"}, None),
    ("users_restore_cloud_item", {"app": "demo", "user_id": 7, "item_id": 11}, "POST", f"{U}/cloud-storage/items/11/restore", {}, None),
    ("users_rollback_cloud_item", {"app": "demo", "user_id": 7, "item_id": 11, "revision": 2}, "POST", f"{U}/cloud-storage/items/11/rollback", {}, {"revision": 2}),
]


@pytest.mark.parametrize(("tool", "args", "method", "path", "query", "payload"), CASES, ids=[f"{c[0]}-{i}" for i, c in enumerate(CASES)])
async def test_request_shape(backend, call, tool, args, method, path, query, payload):
    await call(tool, args)
    req = backend.last()
    assert (req.method, req.path) == (method, path)
    assert req.query == query
    assert req.json == payload


async def test_get_with_includes_collects_extras_and_errors(backend, call):
    backend.reply("GET", U, {"id": 7, "account": "bob"})
    backend.reply("GET", f"{U}/bans/active", {"id": 1})
    backend.reply("GET", f"{U}/wallet", {"balance": 10})
    backend.reply("GET", f"{A}/login-baseline/7", {"bound": True})
    backend.reply("GET", f"{U}/cloud-storage", None, code=50380, message="云存储未启用")
    data = await call("users_get", {"app": "demo", "user_id": 7, "include": ["active_ban", "wallet", "login_baseline", "cloud_storage"]})
    assert data["user"]["account"] == "bob"
    assert data["active_ban"] == {"id": 1}
    assert data["wallet"] == {"balance": 10}
    assert data["login_baseline"] == {"bound": True}
    assert "云存储未启用" in data["errors"]["cloud_storage"]
    paths = [r.path for r in backend.requests]
    assert paths == [U, f"{U}/bans/active", f"{U}/wallet", f"{A}/login-baseline/7", f"{U}/cloud-storage"]


async def test_get_includes_cover_every_route(backend, call):
    names = ["active_ban", "bans", "sessions", "wallet", "oauth2_grants", "login_baseline", "account_deletion", "device_limit", "cloud_storage", "ad_policy"]
    data = await call("users_get", {"app": "demo", "user_id": 7, "include": names})
    assert set(names) <= set(data)
    assert {r.path for r in backend.requests} >= {f"{U}/ad-policy", f"{U}/account-deletion", f"{A}/device-limit/users/7", f"{U}/oauth2-grants"}


async def test_revoke_sessions_routes(backend, call, call_error):
    await call("users_revoke_sessions", {"app": "demo", "user_id": 7, "token_hashes": ["h1"]})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{U}/sessions/h1")
    await call("users_revoke_sessions", {"app": "demo", "user_id": 7, "token_hashes": ["h1", "h2"]})
    assert (backend.last().path, backend.last().json) == (f"{U}/sessions/revoke-batch", {"tokenHashes": ["h1", "h2"]})
    await call("users_revoke_sessions", {"app": "demo", "user_id": 7, "all_sessions": True})
    assert (backend.last().method, backend.last().path) == ("POST", f"{U}/revoke-sessions")
    count = len(backend.requests)
    assert "二选一" in await call_error("users_revoke_sessions", {"app": "demo", "user_id": 7, "token_hashes": ["h"], "all_sessions": True})
    assert "token_hashes" in await call_error("users_revoke_sessions", {"app": "demo", "user_id": 7})
    assert len(backend.requests) == count


async def test_deletion_settings_with_catalog(backend, call):
    backend.reply("GET", f"{A}/account-deletion/settings", {"enabled": True})
    backend.reply("GET", f"{A}/account-deletion/catalog", {"limits": {}})
    data = await call("users_deletion_settings", {"app": "demo", "catalog": True})
    assert data == {"settings": {"enabled": True}, "catalog": {"limits": {}}}


async def test_save_deletion_settings_merges_config(backend, call, call_error):
    backend.reply("GET", f"{A}/account-deletion/settings", {"enabled": False, "config": {"coolingDays": 15, "requireReview": False}})
    await call("users_save_deletion_settings", {"app": "demo", "config": {"requireReview": True}})
    req = backend.find("PUT", f"{A}/account-deletion/settings")
    assert req.json == {"enabled": False, "config": {"coolingDays": 15, "requireReview": True}}
    assert "没有要修改" in await call_error("users_save_deletion_settings", {"app": "demo"})


async def test_device_limit_overview_with_policy(backend, call):
    backend.reply("GET", f"{A}/device-limit/policy", {"policy": {"enabled": True}})
    data = await call("users_device_limit_overview", {"app": "demo", "policy": True})
    assert data["policy"] == {"policy": {"enabled": True}}
    assert backend.find("GET", f"{A}/device-limit").query == {}


async def test_update_device_limit_policy_merges(backend, call, call_error):
    backend.reply("GET", f"{A}/device-limit/policy", {"policy": {
        "enabled": True, "maxDevices": 3, "overflow": "kick_oldest", "newDevice": {"enabled": False, "windowDays": 7, "maxNew": 3},
    }, "configured": True})
    await call("users_update_device_limit_policy", {"app": "demo", "max_devices": 2, "overflow": "ask", "new_device": {"enabled": True}})
    req = backend.find("PUT", f"{A}/device-limit/policy")
    assert req.json == {"enabled": True, "maxDevices": 2, "overflow": "ask", "newDevice": {"enabled": True, "windowDays": 7, "maxNew": 3}}
    assert "没有要修改" in await call_error("users_update_device_limit_policy", {"app": "demo"})


async def test_precondition_errors_send_nothing(backend, call_error):
    assert "end_at" in await call_error("users_ban", {"app": "demo", "user_id": 7, "reason": "x", "ban_type": "temporary"})
    assert "end_at" in await call_error("users_ban_batch", {"app": "demo", "user_ids": [1], "reason": "x", "ban_type": "temporary"})
    assert "note" in await call_error("users_decide_deletion", {"app": "demo", "request_id": 1, "action": "reject"})
    assert "reason_code" in await call_error("users_create_deletion", {"app": "demo", "user_id": 7, "mode": "cooling"})
    assert "confirm" in await call_error("users_create_deletion", {"app": "demo", "user_id": 7, "mode": "immediate", "reason": "x"})
    assert "没有要修改" in await call_error("users_update_profile", {"app": "demo", "user_id": 7})
    assert "enabled" in await call_error("users_set_status", {"app": "demo", "user_id": 7})
    assert "item_id" in await call_error("users_cloud_items", {"app": "demo", "user_id": 7, "revisions": True})
    assert not backend.requests


async def test_backend_error_becomes_tool_error(backend, call_error):
    backend.reply("DELETE", U, None, code=40404, message="用户不存在")
    text = await call_error("users_delete", {"app": "demo", "user_id": 7})
    assert "40404" in text and "用户不存在" in text
