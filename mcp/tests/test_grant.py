"""物品发放模块。"""

from __future__ import annotations

BASE = "/api/admin/apps/demo/item-grants"

GRAPH = {
    "nodes": [
        {"id": "trigger", "type": "trigger", "position": {"x": 0, "y": 120}, "data": {"kind": "manual"}},
        {"id": "grant", "type": "grant", "position": {"x": 320, "y": 120}, "data": {"items": [{"type": "points", "amount": 10}]}},
    ],
    "edges": [{"id": "e1", "source": "trigger", "sourceHandle": "next", "target": "grant"}],
}

CURRENT = {
    "id": 7, "appid": 42, "name": "补发", "description": "d", "status": "active", "trigger": "manual",
    "startsAt": "2026-10-01T00:00:00+08:00", "graph": GRAPH, "version": 3, "createdAt": "x", "updatedAt": "y",
}


async def test_catalog_overview(backend, call):
    await call("grant_catalog", {"app": "demo"})
    assert backend.last().path == f"{BASE}/catalog"
    await call("grant_overview", {"app": "demo", "days": 30})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/overview", {"days": "30"})


async def test_rules_filter_and_slim(backend, call):
    backend.reply("GET", f"{BASE}/rules", [
        {"id": 1, "status": "active", "trigger": "event", "graph": GRAPH},
        {"id": 2, "status": "draft", "trigger": "manual", "graph": GRAPH},
    ])
    data = await call("grant_rules", {"app": "demo"})
    assert data["total"] == 2 and "graph" not in data["items"][0] and data["items"][0]["nodeCount"] == 2
    data = await call("grant_rules", {"app": "demo", "status": "draft"})
    assert [r["id"] for r in data["items"]] == [2]
    data = await call("grant_rules", {"app": "demo", "trigger": "event", "include_graph": True})
    assert [r["id"] for r in data["items"]] == [1] and data["items"][0]["graph"] == GRAPH
    assert await call("grant_rules", {"app": "demo", "status": "archived"}) == {"items": [], "total": 0}
    await call("grant_get", {"app": "demo", "rule_id": 7})
    assert (backend.last().method, backend.last().path) == ("GET", f"{BASE}/rules/7")


async def test_create(backend, call, call_error):
    await call("grant_create", {"app": "demo", "name": "补发", "graph": GRAPH, "starts_at": "2026-11-01T00:00:00+08:00"})
    req = backend.last()
    assert (req.method, req.path, req.json) == ("POST", f"{BASE}/rules", {
        "name": "补发", "startsAt": "2026-11-01T00:00:00+08:00", "graph": GRAPH,
    })
    await call("grant_create", {"app": "demo", "name": "空白"})
    assert backend.last().json == {"name": "空白"}
    await call_error("grant_create", {"app": "demo", "name": ""})
    await call_error("grant_create", {"app": "demo", "name": "x", "graph": GRAPH, "template": "welcome"})


async def test_create_from_template(backend, call, call_error):
    backend.reply("GET", f"{BASE}/catalog", {"templates": [{"key": "welcome", "graph": GRAPH}]})
    await call("grant_create", {"app": "demo", "name": "新人礼包", "template": "welcome"})
    assert backend.find("POST", f"{BASE}/rules").json == {"name": "新人礼包", "graph": GRAPH}
    assert "welcome" in await call_error("grant_create", {"app": "demo", "name": "x", "template": "nope"})


async def test_create_simple(backend, call, call_error):
    await call("grant_create_simple", {
        "app": "demo", "name": "补发", "user_ids": [5, 6], "items": [{"type": "points", "amount": 100}],
        "per_user": 1, "notify_title": "补发到账", "notify_content": "{items}",
    })
    graph = backend.last().json["graph"]
    assert [n["type"] for n in graph["nodes"]] == ["trigger", "audience", "limit", "grant", "notify"]
    assert graph["nodes"][0]["data"] == {"kind": "manual"}
    assert graph["nodes"][1]["data"] == {"scope": "all", "userIds": [5, 6]}
    assert graph["nodes"][2]["data"] == {"period": "total", "perUser": 1}
    assert [(e["source"], e["sourceHandle"], e["target"]) for e in graph["edges"]] == [
        ("trigger", "next", "audience"), ("audience", "next", "limit"), ("limit", "pass", "grant"), ("grant", "next", "notify"),
    ]
    await call("grant_create_simple", {"app": "demo", "name": "签到", "trigger": "event", "event": "signin",
                                       "items": [{"type": "lottery_draws", "amount": 1}]})
    graph = backend.last().json["graph"]
    assert [n["type"] for n in graph["nodes"]] == ["trigger", "audience", "grant"]
    assert graph["nodes"][0]["data"] == {"kind": "event", "event": "signin"}
    await call_error("grant_create_simple", {"app": "demo", "name": "x", "trigger": "schedule", "items": [{"type": "points", "amount": 1}]})
    await call_error("grant_create_simple", {"app": "demo", "name": "x", "trigger": "event", "items": [{"type": "points", "amount": 1}]})
    await call_error("grant_create_simple", {"app": "demo", "name": "x", "cron": "0 10 * * *", "items": [{"type": "points", "amount": 1}]})
    await call_error("grant_create_simple", {"app": "demo", "name": "x", "items": []})
    await call_error("grant_create_simple", {"app": "demo", "name": "x", "scope": "vip", "items": [{"type": "points", "amount": 1}]})


async def test_update_merges_current(backend, call):
    backend.reply("GET", f"{BASE}/rules/7", CURRENT)
    await call("grant_update", {"app": "demo", "rule_id": 7, "name": "补发二期", "starts_at": "", "ends_at": "2026-12-01T00:00:00+08:00"})
    req = backend.find("PUT", f"{BASE}/rules/7")
    assert req.json == {"name": "补发二期", "description": "d", "startsAt": None, "endsAt": "2026-12-01T00:00:00+08:00", "graph": GRAPH}
    new_graph = {**GRAPH, "edges": []}
    await call("grant_update", {"app": "demo", "rule_id": 7, "graph": new_graph})
    req = backend.last()
    assert req.json["graph"] == new_graph and req.json["name"] == "补发" and req.json["startsAt"] == CURRENT["startsAt"]
    assert "status" not in req.json and "version" not in req.json


async def test_status_delete_run(backend, call, call_error):
    await call("grant_set_status", {"app": "demo", "rule_id": 7, "status": "active"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{BASE}/rules/7/status", {"status": "active"})
    await call_error("grant_set_status", {"app": "demo", "rule_id": 7, "status": "ended"})
    await call("grant_delete", {"app": "demo", "rule_id": 7})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{BASE}/rules/7")
    backend.reply("DELETE", f"{BASE}/rules/8", code=40900, status=409, message="规则已有发放记录")
    assert "40900" in await call_error("grant_delete", {"app": "demo", "rule_id": 8})
    await call("grant_run", {"app": "demo", "rule_id": 7})
    assert (backend.last().method, backend.last().path) == ("POST", f"{BASE}/rules/7/run")


async def test_validate_and_preview(backend, call, call_error):
    await call("grant_validate", {"app": "demo", "graph": GRAPH})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{BASE}/validate", {"graph": GRAPH})
    backend.reply("GET", f"{BASE}/rules/7", CURRENT)
    await call("grant_validate", {"app": "demo", "rule_id": 7})
    assert backend.find("POST", f"{BASE}/validate").json == {"graph": GRAPH}
    await call_error("grant_validate", {"app": "demo"})
    await call_error("grant_validate", {"app": "demo", "graph": GRAPH, "rule_id": 7})

    await call("grant_preview", {"app": "demo", "rule_id": 7, "samples": 50})
    assert backend.find("POST", f"{BASE}/preview").json == {"ruleId": 7, "graph": GRAPH, "samples": 50}
    await call("grant_preview", {"app": "demo", "graph": GRAPH, "user": " alice ", "event_data": {"consecutiveDays": 7}})
    assert backend.last().json == {"graph": GRAPH, "user": "alice", "eventData": {"consecutiveDays": 7}}
    await call_error("grant_preview", {"app": "demo"})
    await call_error("grant_preview", {"app": "demo", "graph": GRAPH, "samples": 5000})


async def test_runs_records(backend, call):
    await call("grant_runs", {"app": "demo", "rule_id": 7, "page": 2})
    assert (backend.last().path, backend.last().query) == (f"{BASE}/runs", {"ruleId": "7", "page": "2", "limit": "20"})
    await call("grant_get_run", {"app": "demo", "run_id": 3})
    assert backend.last().path == f"{BASE}/runs/3"
    await call("grant_cancel_run", {"app": "demo", "run_id": 3})
    assert (backend.last().method, backend.last().path) == ("POST", f"{BASE}/runs/3/cancel")
    await call("grant_records", {"app": "demo", "status": "pending", "user_id": 5, "start": "2026-10-01T00:00:00+08:00"})
    assert backend.last().query == {"userId": "5", "status": "pending", "start": "2026-10-01T00:00:00+08:00", "page": "1", "limit": "20"}
    await call("grant_get_record", {"app": "demo", "record_id": 9})
    assert backend.last().path == f"{BASE}/records/9"
    await call("grant_fulfill", {"app": "demo", "record_id": 9, "note": "单号 SF123"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("POST", f"{BASE}/records/9/fulfill", {"note": "单号 SF123"})
    await call("grant_retry", {"app": "demo", "record_id": 9})
    assert (backend.last().method, backend.last().path) == ("POST", f"{BASE}/records/9/retry")
