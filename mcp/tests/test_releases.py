"""发布中心模块。"""

from __future__ import annotations

A = "/api/admin/apps/demo"


async def test_list_and_get(backend, call):
    data = await call("releases_list", {"app": "demo", "status": "published", "overview": True})
    assert set(data) == {"overview", "releases"}
    assert backend.find("GET", f"{A}/releases").query == {"status": "published", "page": "1", "limit": "20"}
    await call("releases_get", {"app": "demo", "release_id": 3})
    assert backend.find("GET", f"{A}/releases/3/stats")


async def test_save_release(backend, call):
    await call("releases_save", {"app": "demo", "version": "2.0.0", "version_code": 200, "platform": "android", "notes_markdown": "# 新功能"})
    req = backend.last()
    assert (req.method, req.path) == ("POST", f"{A}/releases")
    assert req.json == {"version": "2.0.0", "versionCode": 200, "platform": "android", "notesMarkdown": "# 新功能", "notesFormat": "markdown",
                        "updateType": "optional", "visibility": "public", "rolloutPct": 100}
    await call("releases_save", {"app": "demo", "version": "2.0.0", "version_code": 200, "platform": "android", "release_id": 3, "rollout_pct": 10})
    assert (backend.last().method, backend.last().path, backend.last().json["rolloutPct"]) == ("PUT", f"{A}/releases/3", 10)


async def test_control(backend, call, call_error):
    await call("releases_control", {"app": "demo", "release_id": 3, "action": "publish", "publish_at": "2026-11-01T00:00:00Z"})
    assert (backend.last().path, backend.last().json) == (f"{A}/releases/3/publish", {"publishAt": "2026-11-01T00:00:00Z"})
    await call("releases_control", {"app": "demo", "release_id": 3, "action": "rollout", "rollout_pct": 50})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{A}/releases/3/rollout", {"rolloutPct": 50})
    await call_error("releases_control", {"app": "demo", "release_id": 3, "action": "rollout"})
    for action in ("pause", "resume", "revoke"):
        await call("releases_control", {"app": "demo", "release_id": 3, "action": action})
        assert backend.last().path == f"{A}/releases/3/{action}"
    await call("releases_control", {"app": "demo", "release_id": 3, "action": "delete"})
    assert backend.last().method == "DELETE"


async def test_simulate_and_preview(backend, call):
    await call("releases_simulate", {"app": "demo", "platform": "android", "version_code": 100, "user_id": 5})
    assert backend.last().json == {"platform": "android", "versionCode": 100, "userId": 5}
    await call("releases_preview_notes", {"app": "demo", "markdown": "**x**"})
    assert backend.last().json == {"format": "markdown", "markdown": "**x**"}


async def test_channels(backend, call):
    await call("releases_channels", {"app": "demo"})
    assert backend.last().path == f"{A}/channels"
    await call("releases_channels", {"app": "demo", "channel_id": 2, "members": True})
    assert backend.last().path == f"{A}/channels/2/users"
    await call("releases_save_channel", {"app": "demo", "name": "内测", "code": "beta", "self_join": True})
    assert backend.last().json == {"name": "内测", "code": "beta", "description": "", "self_join": True, "status": True}
    await call("releases_channel_members", {"app": "demo", "channel_id": 2, "user_ids": [1], "action": "remove"})
    assert (backend.last().method, backend.last().json) == ("DELETE", {"user_ids": [1]})
    await call("releases_delete_channel", {"app": "demo", "channel_id": 2})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{A}/channels/2")


async def test_versions(backend, call):
    await call("releases_versions", {"app": "demo", "stats": True})
    assert backend.last().path == f"{A}/versions/stats"
    await call("releases_versions", {"app": "demo", "channel_id": 2})
    assert backend.last().query == {"channel_id": "2", "page": "1", "limit": "20"}
    await call("releases_save_version", {"app": "demo", "version": "1.0", "version_code": 1, "platform": "android", "force_update": True})
    assert backend.last().json == {"version": "1.0", "version_code": 1, "platform": "android", "force_update": True}
    await call("releases_version_control", {"app": "demo", "version_id": 4, "action": "revoke"})
    assert backend.last().path == f"{A}/versions/4/revoke"
