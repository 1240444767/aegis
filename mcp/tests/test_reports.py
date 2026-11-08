"""报表中心模块。"""

from __future__ import annotations


async def test_single_report(backend, call):
    await call("reports_get", {"app": "demo", "report": "retention", "start": "2026-10-01", "end": "2026-10-07", "unit": "week", "mode": "rolling"})
    req = backend.last()
    assert req.path == "/api/admin/apps/demo/reports/retention"
    assert req.query == {"start": "2026-10-01", "end": "2026-10-07", "tz": "Asia/Shanghai", "unit": "week", "mode": "rolling"}


async def test_dashboard_tolerates_failures(backend, call):
    backend.reply("GET", "/api/admin/apps/demo/reports/payment", code=40164, status=403, message="范围不包含")
    data = await call("reports_dashboard", {"app": "demo", "start": "2026-10-01", "end": "2026-10-07", "reports": ["overview", "payment"]})
    assert data["overview"] == {"ok": True} and "40164" in data["payment"]["error"]
