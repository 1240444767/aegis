"""排行榜模块。"""

from __future__ import annotations

B = "/api/admin/apps/demo/leaderboard"


async def test_settings_merge(backend, call, call_error):
    backend.reply("GET", f"{B}/settings", {"settings": {
        "appid": 42, "enabled": True, "boards": [{"key": "points_total", "enabled": True}], "topLimit": 100, "minValue": 1,
        "identity": "nickname", "allowAnonymous": True, "countAdminAdjust": False,
        "rewards": {"points_daily": {"enabled": True, "tiers": []}}, "configured": True,
    }, "catalog": []})
    await call("leaderboard_settings", {"app": "demo"})
    assert backend.last().path == f"{B}/settings"
    await call("leaderboard_update_settings", {
        "app": "demo", "top_limit": 50,
        "rewards": {"points_weekly": {"enabled": True, "tiers": [{"rank_from": 1, "rank_to": 3, "rewards": [{"type": "points", "amount": 100}]}]}},
    })
    req = backend.find("PUT", f"{B}/settings")
    assert req.json == {
        "enabled": True, "boards": [{"key": "points_total", "enabled": True}], "topLimit": 50, "minValue": 1, "identity": "nickname",
        "allowAnonymous": True, "countAdminAdjust": False, "rewards": {
            "points_daily": {"enabled": True, "tiers": []},
            "points_weekly": {"enabled": True, "tiers": [{"rankFrom": 1, "rankTo": 3, "rewards": [{"type": "points", "amount": 100}]}]},
        },
    }
    await call_error("leaderboard_update_settings", {"app": "demo", "rewards": {"points_total": {"enabled": True}}})


async def test_board(backend, call, call_error):
    await call("leaderboard_board", {"app": "demo", "key": "points_weekly", "period_key": "2026-W43"})
    assert (backend.last().path, backend.last().query) == (f"{B}/boards/points_weekly", {"periodKey": "2026-W43", "page": "1", "limit": "20"})
    await call("leaderboard_board", {"app": "demo", "key": "level"})
    assert backend.last().query == {"page": "1", "limit": "20"}
    assert "期号" in await call_error("leaderboard_board", {"app": "demo", "key": "points_daily", "period_key": "2026-10"})
    await call_error("leaderboard_board", {"app": "demo", "key": "nope"})


async def test_exclusions(backend, call, call_error):
    await call("leaderboard_exclusions", {"app": "demo", "keyword": "bot"})
    assert (backend.last().path, backend.last().query) == (f"{B}/exclusions", {"keyword": "bot", "page": "1", "limit": "20"})
    await call("leaderboard_exclude", {"app": "demo", "user_ids": [1, 2], "reason": "刷分"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{B}/exclusions", {"userIds": [1, 2], "reason": "刷分"})
    await call_error("leaderboard_exclude", {"app": "demo", "user_ids": list(range(1, 102))})
    await call("leaderboard_include", {"app": "demo", "user_id": 2})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/exclusions/2")


async def test_snapshots_settle_stats(backend, call, call_error):
    await call("leaderboard_snapshots", {"app": "demo", "board": "points_monthly"})
    assert (backend.last().path, backend.last().query) == (f"{B}/snapshots", {"board": "points_monthly", "page": "1", "limit": "20"})
    await call("leaderboard_snapshot_entries", {"app": "demo", "snapshot_id": 5, "status": "failed"})
    assert (backend.last().path, backend.last().query) == (f"{B}/snapshots/5/entries", {"status": "failed", "page": "1", "limit": "20"})
    await call("leaderboard_retry_snapshot", {"app": "demo", "snapshot_id": 5})
    assert (backend.last().method, backend.last().path) == ("POST", f"{B}/snapshots/5/retry")
    await call("leaderboard_settle", {"app": "demo", "board": "points_monthly", "period_key": "2026-09"})
    assert (backend.last().path, backend.last().json) == (f"{B}/settle", {"board": "points_monthly", "periodKey": "2026-09"})
    await call_error("leaderboard_settle", {"app": "demo", "board": "points_total", "period_key": "total"})
    await call_error("leaderboard_settle", {"app": "demo", "board": "points_weekly", "period_key": "2026-43"})
    backend.reply("POST", f"{B}/settle", code=40922, status=409, message="周期未结束")
    assert "40922" in await call_error("leaderboard_settle", {"app": "demo", "board": "points_weekly", "period_key": "2026-W44"})
    await call("leaderboard_stats", {"app": "demo"})
    assert backend.last().path == f"{B}/stats"
