"""audit 模块：登录 / 会话审计、平台审计日志、崩溃报告与崩溃日志的请求形状与前置校验。"""

from __future__ import annotations

import httpx
import pytest

A = "/api/admin/apps/demo"
S = "/api/admin/system"

CASES = [
    ("audit_login", {"app": "demo", "status": "failed", "keyword": "1.2.3.4"}, "GET", f"{A}/audits/login", {"status": "failed", "keyword": "1.2.3.4", "page": "1", "limit": "20"}, None),
    ("audit_login", {"app": "demo", "user_id": 7, "from_time": "2026-10-01T00:00:00Z", "to_time": "2026-10-02T00:00:00Z", "limit": 50},
     "GET", f"{A}/users/7/audits/login", {"from": "2026-10-01T00:00:00Z", "to": "2026-10-02T00:00:00Z", "page": "1", "limit": "50"}, None),
    ("audit_sessions", {"app": "demo", "event_type": "revoked"}, "GET", f"{A}/audits/sessions", {"eventType": "revoked", "page": "1", "limit": "20"}, None),
    ("audit_sessions", {"app": "demo", "user_id": 7, "event_type": "issued", "page": 2},
     "GET", f"{A}/users/7/audits/sessions", {"eventType": "issued", "page": "2", "limit": "20"}, None),
    ("audit_logs", {"kind": "write", "app_id": 42, "severity": "high", "status_code": 403, "admin_id": 3, "session_id": "s1", "start_time": "2026-10-01T00:00:00Z"},
     "GET", f"{S}/audit-logs", {"kind": "write", "appId": "42", "severity": "high", "statusCode": "403", "adminId": "3", "sessionId": "s1", "startTime": "2026-10-01T00:00:00Z", "page": "1", "limit": "20"}, None),
    ("audit_logs", {"log_id": 99}, "GET", f"{S}/audit-logs/99", {}, None),
    ("audit_facets", {}, "GET", f"{S}/audit-logs/facets", {}, None),
    ("audit_overview", {}, "GET", f"{S}/audit-logs/overview", {}, None),
    ("audit_crash_reports", {"app": "demo", "kind": "anr", "resolved": False, "app_version": "1.2.0", "user_id": 7, "start": "2026-10-01"},
     "GET", f"{A}/crash-reports", {"kind": "anr", "resolved": "false", "appVersion": "1.2.0", "userId": "7", "start": "2026-10-01", "page": "1", "limit": "20"}, None),
    ("audit_crash_reports", {"app": "demo", "crash_id": 5}, "GET", f"{A}/crash-reports/5", {}, None),
    ("audit_crash_facets", {"app": "demo"}, "GET", f"{A}/crash-reports/facets", {}, None),
    ("audit_resolve_crashes", {"app": "demo", "crash_ids": [1, 2]}, "POST", f"{A}/crash-reports/resolve", {}, {"ids": [1, 2], "resolved": True}),
    ("audit_resolve_crashes", {"app": "demo", "crash_ids": [1], "resolved": False}, "POST", f"{A}/crash-reports/resolve", {}, {"ids": [1], "resolved": False}),
    ("audit_delete_crashes", {"app": "demo", "crash_ids": [5]}, "DELETE", f"{A}/crash-reports/5", {}, None),
    ("audit_delete_crashes", {"app": "demo", "crash_ids": [5, 6]}, "POST", f"{A}/crash-reports/delete", {}, {"ids": [5, 6]}),
    ("audit_crashlogs", {}, "GET", f"{S}/crashlogs", {}, None),
    ("audit_delete_crashlog", {"filename": "crash-2026-10-28.jsonl"}, "DELETE", f"{S}/crashlogs/crash-2026-10-28.jsonl", {}, None),
]


@pytest.mark.parametrize(("tool", "args", "method", "path", "query", "payload"), CASES, ids=[f"{c[0]}-{i}" for i, c in enumerate(CASES)])
async def test_request_shape(backend, call, tool, args, method, path, query, payload):
    await call(tool, args)
    req = backend.last()
    assert (req.method, req.path) == (method, path)
    assert req.query == query
    assert req.json == payload


async def test_overview_with_stats(backend, call):
    backend.reply("GET", f"{S}/audit-logs/overview", {"today": 3})
    backend.reply("GET", f"{S}/audit-logs/stats", {"total": 9})
    assert await call("audit_overview", {"stats": True}) == {"overview": {"today": 3}, "stats": {"total": 9}}


async def test_app_level_audit_rejects_time_range(backend, call_error):
    assert "user_id" in await call_error("audit_login", {"app": "demo", "from_time": "2026-10-01T00:00:00Z"})
    assert "user_id" in await call_error("audit_sessions", {"app": "demo", "to_time": "2026-10-01T00:00:00Z"})
    assert not backend.requests


async def test_crashlog_reads_ndjson_tail(backend, call):
    lines = "\n".join(['{"n":1}', '{"n":2}', "not json", '{"n":4}']) + "\n"
    backend.routes[("GET", f"{S}/crashlogs/a.jsonl")] = httpx.Response(200, text=lines, headers={"content-type": "application/x-ndjson; charset=utf-8"})
    data = await call("audit_crashlogs", {"filename": "a.jsonl", "tail": 3})
    assert data["totalLines"] == 4
    assert data["entries"] == [{"n": 2}, "not json", {"n": 4}]


async def test_crash_ids_limit(backend, call_error):
    await call_error("audit_delete_crashes", {"app": "demo", "crash_ids": []})
    await call_error("audit_resolve_crashes", {"app": "demo", "crash_ids": list(range(1, 202))})
    assert not backend.requests


async def test_super_admin_error_surfaces(backend, call_error):
    backend.reply("GET", f"{S}/audit-logs", None, code=40301, message="仅超级管理员可查看全局审计日志")
    text = await call_error("audit_logs", {})
    assert "40301" in text and "超级管理员" in text
