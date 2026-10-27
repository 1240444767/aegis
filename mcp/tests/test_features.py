"""功能权益模块。"""

from __future__ import annotations

BASE = "/api/admin/apps/demo/users/7/feature-unlocks"


async def test_unlocks(backend, call):
    backend.reply("GET", BASE, {"items": [], "active": []})
    assert await call("features_unlocks", {"app": "demo", "user_id": 7}) == {"items": [], "active": []}
    assert backend.last().method == "GET"


async def test_grant_unlock(backend, call, call_error):
    await call("features_grant_unlock", {"app": "demo", "user_id": 7, "tag": "export", "days": 30})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", BASE, {"tag": "export", "days": 30})
    await call("features_grant_unlock", {"app": "demo", "user_id": 7, "tag": "export"})
    assert backend.last().json == {"tag": "export", "days": 0}
    await call_error("features_grant_unlock", {"app": "demo", "user_id": 7, "tag": "export", "days": 4000})
    await call_error("features_grant_unlock", {"app": "demo", "user_id": 0, "tag": "export"})


async def test_grant_unknown_feature(backend, call_error):
    backend.reply("POST", BASE, code=40486, status=404, message="功能标识未登记")
    assert "40486" in await call_error("features_grant_unlock", {"app": "demo", "user_id": 7, "tag": "nope"})


async def test_revoke_unlock(backend, call):
    await call("features_revoke_unlock", {"app": "demo", "user_id": 7, "unlock_id": 3, "reason": "误发"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{BASE}/3/revoke", {"reason": "误发"})
