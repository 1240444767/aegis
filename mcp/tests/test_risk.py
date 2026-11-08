"""risk 模块：风控中心与防火墙工具的请求形状与前置校验。"""

from __future__ import annotations

import pytest

R = "/api/admin/system/risk"
F = "/api/admin/system/firewall"
PAGE = {"page": "1", "pageSize": "20"}

CASES = [
    ("risk_metadata", {}, "GET", f"{R}/metadata", {}, None),
    ("risk_validate_expression", {"expression": "ip_score > 80"}, "POST", f"{R}/expression/validate", {}, {"expression": "ip_score > 80"}),
    ("risk_dashboard", {"app": "demo", "start": "2026-10-01T00:00:00Z"}, "GET", f"{R}/dashboard", {"appId": "42", "start": "2026-10-01T00:00:00Z"}, None),
    ("risk_simulate", {"scene": "register", "app": "42", "ip": "1.1.1.1", "draft": {"conditionType": "ua_bot"}, "captcha_passed": True},
     "POST", f"{R}/simulate", {}, {"scene": "register", "appId": 42, "ip": "1.1.1.1", "captchaPassed": True, "draft": {"conditionType": "ua_bot"}}),
    ("risk_simulate", {"rule_id": 8, "account": "bob"}, "POST", f"{R}/rules/8/simulate", {}, {"scene": "login", "account": "bob", "captchaPassed": False}),
    ("risk_evaluate", {"scene": "payment", "user_id": 7, "extra": {"amount": 100}}, "POST", f"{R}/evaluate", {}, {"scene": "payment", "userId": 7, "extra": {"amount": 100}}),
    ("risk_rules", {"scene": "login", "app": "demo"}, "GET", f"{R}/rules", {"scene": "login", "appId": "42"}, None),
    ("risk_rules", {"platform_only": True}, "GET", f"{R}/rules", {"appId": "0"}, None),
    ("risk_rules", {"rule_id": 8, "start": "2026-10-01T00:00:00Z"}, "GET", f"{R}/rules/8", {"start": "2026-10-01T00:00:00Z"}, None),
    ("risk_create_rule", {"name": "机房 IP", "scene": "*", "condition_type": "ip_proxy", "mode": "observe"},
     "POST", f"{R}/rules", {}, {"name": "机房 IP", "description": "", "scene": "*", "conditionType": "ip_proxy", "conditionData": {}, "score": 20, "priority": 0, "isActive": True, "mode": "observe", "decision": ""}),
    ("risk_update_rule", {"rule_id": 8, "score": 40, "is_active": False, "app": "0"}, "PUT", f"{R}/rules/8", {}, {"score": 40, "isActive": False, "appId": 0}),
    ("risk_update_rule", {"rule_id": 8, "app": "demo"}, "PUT", f"{R}/rules/8", {}, {"appId": 42}),
    ("risk_delete_rule", {"rule_id": 8}, "DELETE", f"{R}/rules/8", {}, None),
    ("risk_actions", {"scene": "login"}, "GET", f"{R}/actions", {"scene": "login"}, None),
    ("risk_create_action", {"scene": "login", "action": "ban", "min_score": 80, "ban_targets": ["device", "ip"], "ban_duration": 3600},
     "POST", f"{R}/actions", {}, {"scene": "login", "minScore": 80, "action": "ban", "banDuration": 3600, "banTargets": ["device", "ip"], "description": "", "isActive": True}),
    ("risk_update_action", {"action_id": 3, "max_score": 99}, "PUT", f"{R}/actions/3", {}, {"maxScore": 99}),
    ("risk_delete_action", {"action_id": 3}, "DELETE", f"{R}/actions/3", {}, None),
    ("risk_lists", {"list_type": "ip", "kind": "block", "active": True}, "GET", f"{R}/lists", {"listType": "ip", "kind": "block", "active": "true", **PAGE}, None),
    ("risk_add_list", {"list_type": "cidr", "values": ["10.0.0.0/8"], "kind": "block", "reason": "内网", "expires_at": "2027-01-01T00:00:00Z"},
     "POST", f"{R}/lists", {}, {"listType": "cidr", "values": ["10.0.0.0/8"], "kind": "block", "reason": "内网", "expiresAt": "2027-01-01T00:00:00Z"}),
    ("risk_update_list", {"entry_id": 4, "expires_at": ""}, "PUT", f"{R}/lists/4", {}, {"expiresAt": ""}),
    ("risk_delete_lists", {"entry_ids": [4]}, "DELETE", f"{R}/lists/4", {}, None),
    ("risk_delete_lists", {"entry_ids": [4, 5]}, "POST", f"{R}/lists/batch-delete", {}, {"ids": [4, 5]}),
    ("risk_check_lists", {"ip": "1.1.1.1", "user_id": 7}, "POST", f"{R}/lists/check", {}, {"ip": "1.1.1.1", "userId": 7}),
    ("risk_assessments", {"ip": "1.1.1.1", "reviewed": False, "min_score": 50, "risk_level": "high"},
     "GET", f"{R}/assessments", {"ip": "1.1.1.1", "reviewed": "false", "minScore": "50", "riskLevel": "high", **PAGE}, None),
    ("risk_assessments", {"assessment_id": 12}, "GET", f"{R}/assessments/12", {}, None),
    ("risk_pending_reviews", {"page_size": 50}, "GET", f"{R}/reviews/pending", {"page": "1", "pageSize": "50"}, None),
    ("risk_review", {"assessment_id": 12, "result": "approved", "targets": ["allow"], "revoke_auto": True},
     "POST", f"{R}/assessments/12/review", {}, {"result": "approved", "comment": "", "targets": ["allow"], "duration": 0, "revokeAuto": True}),
    ("risk_replay", {"assessment_id": 12}, "POST", f"{R}/assessments/12/replay", {}, None),
    ("risk_purge_assessments", {"days": 90}, "DELETE", f"{R}/assessments", {}, {"days": 90}),
    ("risk_devices", {"tag": "bot", "only_risk": True}, "GET", f"{R}/devices", {"tag": "bot", "onlyRisk": "true", **PAGE}, None),
    ("risk_devices", {"suspicious": True}, "GET", f"{R}/devices/suspicious", PAGE, None),
    ("risk_devices", {"device_id": "dev-1"}, "GET", f"{R}/devices/dev-1", {}, None),
    ("risk_tag_device", {"record_id": 3, "tag": "trusted"}, "PUT", f"{R}/devices/3/tag", {}, {"tag": "trusted", "note": ""}),
    ("risk_ips", {"keyword": "10."}, "GET", f"{R}/ips", {"keyword": "10.", **PAGE}, None),
    ("risk_ips", {"ip": "1.1.1.1"}, "GET", f"{R}/ips/1.1.1.1", {}, None),
    ("risk_refresh_ip", {"ip": "1.1.1.1"}, "POST", f"{R}/ips/1.1.1.1/refresh", {}, None),
    ("risk_tag_ip", {"record_id": 5, "tag": "proxy", "note": "代理"}, "PUT", f"{R}/ips/5/tag", {}, {"tag": "proxy", "note": "代理"}),
    ("risk_accounts", {"only_risk": True, "days": 7}, "GET", f"{R}/accounts", {"onlyRisk": "true", "days": "7", **PAGE}, None),
    ("risk_accounts", {"account": "bob", "app": "demo"}, "GET", f"{R}/accounts/detail", {"appId": "42", "account": "bob"}, None),
    ("risk_dispose", {"targets": ["account", "firewall"], "kind": "block", "assessment_id": 12, "duration": 600},
     "POST", f"{R}/dispose", {}, {"assessmentId": 12, "targets": ["account", "firewall"], "kind": "block", "duration": 600, "reason": ""}),
    ("risk_templates", {"app": "demo"}, "GET", f"{R}/templates", {"appId": "42"}, None),
    ("risk_apply_template", {"key": "basic"}, "POST", f"{R}/templates/apply", {}, {"key": "basic", "observe": True, "withActions": False}),
    ("risk_ip_bans", {"status": "active", "source": "risk"}, "GET", f"{F}/bans", {"status": "active", "source": "risk", **PAGE}, None),
    ("risk_ip_bans", {"modes": True}, "GET", f"{F}/bans/modes", {}, None),
    ("risk_ban_ip", {"ip": "6.6.6.6", "duration": 3600, "mode": "tarpit", "delay_ms": 5000},
     "POST", f"{F}/bans", {}, {"ip": "6.6.6.6", "reason": "", "duration": 3600, "mode": "tarpit", "delayMs": 5000}),
    ("risk_unban_ip", {"ban_id": 9}, "DELETE", f"{F}/bans/9", {}, None),
    ("risk_geo_bans", {}, "GET", f"{F}/geo-bans", {}, None),
    ("risk_save_geo_ban", {"scope_type": "country", "scope_value": "KP"},
     "POST", f"{F}/geo-bans", {}, {"scopeType": "country", "scopeValue": "KP", "mode": "", "reason": "", "enabled": True, "expiresAt": ""}),
    ("risk_toggle_geo_ban", {"geo_ban_id": 2, "enabled": False}, "PATCH", f"{F}/geo-bans/2", {}, {"enabled": False}),
    ("risk_delete_geo_ban", {"geo_ban_id": 2}, "DELETE", f"{F}/geo-bans/2", {}, None),
    ("risk_geo_fences", {}, "GET", f"{F}/geo-fences", {}, None),
    ("risk_preview_geo_fence", {"name": "园区", "mode": "deny", "center_lat": 31.2, "center_lng": 121.5, "radius_m": 500, "window_days": 3},
     "POST", f"{F}/geo-fences/preview", {}, {"name": "园区", "mode": "deny", "centerLat": 31.2, "centerLng": 121.5, "radiusM": 500, "banMode": "", "reason": "", "enabled": True, "expiresAt": "", "windowDays": 3}),
    ("risk_save_geo_fence", {"name": "区", "mode": "allow", "fence": {"type": "Polygon", "coordinates": []}, "app": "demo"},
     "POST", f"{F}/geo-fences", {}, {"appId": 42, "name": "区", "mode": "allow", "fence": {"type": "Polygon", "coordinates": []}, "banMode": "", "reason": "", "enabled": True, "expiresAt": ""}),
    ("risk_save_geo_fence", {"fence_id": 6, "name": "区", "mode": "review", "fence": {"type": "Polygon", "coordinates": []}, "enabled": False},
     "PUT", f"{F}/geo-fences/6", {}, {"name": "区", "mode": "review", "fence": {"type": "Polygon", "coordinates": []}, "banMode": "", "reason": "", "enabled": False, "expiresAt": ""}),
    ("risk_toggle_geo_fence", {"fence_id": 6, "enabled": True}, "PATCH", f"{F}/geo-fences/6", {}, {"enabled": True}),
    ("risk_delete_geo_fence", {"fence_id": 6}, "DELETE", f"{F}/geo-fences/6", {}, None),
    ("risk_firewall_logs", {"ip": "6.6.6.6", "severity": "high", "sort_by": "ip", "waf_rule_id": 3},
     "GET", f"{F}/logs", {"ip": "6.6.6.6", "severity": "high", "sortBy": "ip", "wafRuleId": "3", **PAGE}, None),
    ("risk_firewall_logs", {"log_id": 77}, "GET", f"{F}/logs/77", {}, None),
    ("risk_firewall_stats", {"granularity": "day"}, "GET", f"{F}/stats", {"granularity": "day"}, None),
    ("risk_cleanup_firewall_logs", {"before": "2026-01-01T00:00:00Z"}, "DELETE", f"{F}/logs", {}, {"before": "2026-01-01T00:00:00Z"}),
]


@pytest.mark.parametrize(("tool", "args", "method", "path", "query", "payload"), CASES, ids=[f"{c[0]}-{i}" for i, c in enumerate(CASES)])
async def test_request_shape(backend, call, tool, args, method, path, query, payload):
    await call(tool, args)
    req = backend.last()
    assert (req.method, req.path) == (method, path)
    assert req.query == query
    assert req.json == payload


async def test_app_key_resolves_through_whoami(backend, call, call_error):
    await call("risk_templates", {"app": "demo"})
    assert backend.requests[0].path == "/api/admin/mcp/whoami"
    text = await call_error("risk_templates", {"app": "nope"})
    assert "nope" in text


async def test_precondition_errors_send_nothing(backend, call_error):
    assert "二选一" in await call_error("risk_simulate", {"rule_id": 1, "rule_ids": [2]})
    assert "没有要修改" in await call_error("risk_update_rule", {"rule_id": 1})
    assert "没有要修改" in await call_error("risk_update_action", {"action_id": 1})
    assert "没有要修改" in await call_error("risk_update_list", {"entry_id": 1})
    assert "至少" in await call_error("risk_check_lists", {})
    assert "assessment_id" in await call_error("risk_dispose", {"targets": ["ip"], "kind": "block"})
    assert "fence" in await call_error("risk_save_geo_fence", {"name": "x", "mode": "deny", "center_lat": 1.0})
    assert "fence" in await call_error("risk_preview_geo_fence", {"name": "x", "mode": "deny"})
    await call_error("risk_add_list", {"list_type": "ip", "values": [], "kind": "block"})
    await call_error("risk_add_list", {"list_type": "nope", "values": ["1"], "kind": "block"})
    assert not backend.requests


async def test_backend_error_becomes_tool_error(backend, call_error):
    backend.reply("POST", f"{F}/bans", None, code=40313, message="仅超级管理员可管理 IP 封禁")
    text = await call_error("risk_ban_ip", {"ip": "6.6.6.6"})
    assert "40313" in text and "超级管理员" in text
