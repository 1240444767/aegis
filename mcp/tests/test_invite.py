"""邀请模块。"""

from __future__ import annotations

B = "/api/admin/apps/demo/invite"


async def test_catalog_settings_stats(backend, call):
    await call("invite_catalog", {"app": "demo"})
    assert backend.last().path == f"{B}/catalog"
    await call("invite_settings", {"app": "demo"})
    assert backend.last().path == f"{B}/settings"
    await call("invite_stats", {"app": "demo", "days": 30})
    assert (backend.last().path, backend.last().query) == (f"{B}/stats", {"days": "30"})


async def test_update_settings_merges(backend, call, call_error):
    backend.reply("GET", f"{B}/settings", {
        "appid": 42, "enabled": False, "registerField": "optional", "codeLength": 8, "inviteeRewards": [{"type": "points", "amount": 10}],
        "inviterRewards": [], "milestones": [], "inviterTrigger": "bind", "configured": True, "updatedBy": "root",
    })
    await call("invite_update_settings", {
        "app": "demo", "enabled": True, "inviter_rewards": [{"type": "vip_days", "amount": 3}],
        "milestones": [{"count": 5, "title": "五人", "rewards": [{"type": "points", "amount": 100}]}],
    })
    req = backend.find("PUT", f"{B}/settings")
    assert req.json == {
        "enabled": True, "registerField": "optional", "codeLength": 8, "inviteeRewards": [{"type": "points", "amount": 10}],
        "inviterRewards": [{"type": "vip_days", "amount": 3}], "milestones": [{"count": 5, "title": "五人", "rewards": [{"type": "points", "amount": 100}]}],
        "inviterTrigger": "bind",
    }
    await call_error("invite_update_settings", {"app": "demo", "register_field": "maybe"})


async def test_codes(backend, call, call_error):
    await call("invite_codes", {"app": "demo", "kind": "campaign", "owner_id": 7})
    assert (backend.last().path, backend.last().query) == (f"{B}/codes", {"kind": "campaign", "ownerId": "7", "page": "1", "limit": "20"})

    await call("invite_create_codes", {"app": "demo", "title": "直播", "count": 10, "prefix": "LIVE", "rewards": []})
    assert (backend.last().method, backend.last().path, backend.last().json) == (
        "POST", f"{B}/codes", {"count": 10, "prefix": "LIVE", "title": "直播", "rewards": [], "status": "active"})
    await call("invite_create_codes", {"app": "demo", "title": "指定", "code": "SPRING", "count": 3, "max_uses": 100})
    assert backend.last().json == {"code": "SPRING", "title": "指定", "maxUses": 100, "status": "active"}
    await call_error("invite_create_codes", {"app": "demo", "title": "x"})

    await call("invite_set_code_status", {"app": "demo", "code_id": 4, "active": False})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{B}/codes/4/status", {"active": False})
    await call("invite_delete_code", {"app": "demo", "code_id": 4})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/codes/4")


async def test_update_code_merges(backend, call, call_error):
    backend.reply("GET", f"{B}/codes", {"items": [
        {"id": 4, "code": "LIVE01", "kind": "campaign", "title": "直播", "ownerUserId": 7, "rewards": [], "maxUses": 50,
         "usedCount": 3, "status": "active", "endsAt": "2026-11-01T00:00:00Z"},
    ], "total": 1})
    await call("invite_update_code", {"app": "demo", "code_id": 4, "note": "加量", "max_uses": 80, "clear": ["owner_user_id", "rewards"]})
    req = backend.find("PUT", f"{B}/codes/4")
    assert req.json == {"title": "直播", "note": "加量", "maxUses": 80, "endsAt": "2026-11-01T00:00:00Z", "status": "active"}
    assert backend.find("GET", f"{B}/codes").query == {"kind": "campaign", "page": "1", "limit": "100"}
    assert "找不到" in await call_error("invite_update_code", {"app": "demo", "code_id": 99, "note": "x"})


async def test_relations(backend, call):
    await call("invite_relations", {"app": "demo", "inviter_id": 7, "status": "pending", "source": "bind"})
    assert (backend.last().path, backend.last().query) == (f"{B}/relations", {"inviterId": "7", "status": "pending", "source": "bind", "page": "1", "limit": "20"})
    await call("invite_settle", {"app": "demo", "relation_id": 9})
    assert (backend.last().method, backend.last().path) == ("POST", f"{B}/relations/9/settle")
    await call("invite_cancel", {"app": "demo", "relation_id": 9})
    assert (backend.last().method, backend.last().path) == ("POST", f"{B}/relations/9/cancel")
