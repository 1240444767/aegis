"""用户主档模块。"""

from __future__ import annotations

B = "/api/admin/system/user-master"


async def test_search_and_get(backend, call):
    await call("identity_search", {"keyword": "a", "risk_level": "high"})
    assert (backend.last().path, backend.last().query) == (f"{B}/identities", {"keyword": "a", "riskLevel": "high", "page": "1", "limit": "20"})
    data = await call("identity_get", {"identity_id": 3})
    assert set(data) == {"identity", "mappings", "tags"}
    assert backend.find("GET", f"{B}/identities/3/tags")


async def test_create_and_update(backend, call, call_error):
    await call_error("identity_create", {})
    await call("identity_create", {"email": "a@b.c"})
    assert backend.last().json == {"displayName": "", "email": "a@b.c", "phone": ""}
    await call("identity_update", {"identity_id": 3, "status": "disabled", "risk_level": "high", "risk_score": 80})
    assert backend.find("PUT", f"{B}/identities/3/status").json == {"status": "disabled"}
    assert backend.find("PUT", f"{B}/identities/3/risk").json == {"level": "high", "score": 80}
    await call_error("identity_update", {"identity_id": 3})


async def test_mapping_sync_merge(backend, call, call_error):
    await call("identity_link_account", {"identity_id": 3, "app": "demo", "user_id": 9})
    assert backend.find("POST", f"{B}/mappings").json == {"identityId": 3, "appId": 42, "userId": 9}
    await call("identity_unlink_account", {"mapping_id": 5})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/mappings/5")
    await call("identity_sync", {"app": "7"})
    assert (backend.last().path, backend.last().json) == (f"{B}/sync/batch", {"appId": 7})
    await call("identity_sync", {"app": "7", "user_id": 2})
    assert (backend.last().path, backend.last().json) == (f"{B}/sync", {"appId": 7, "userId": 2})
    await call_error("identity_merge", {"primary_id": 1, "merged_id": 1})
    await call("identity_merge", {"primary_id": 1, "merged_id": 2})
    assert backend.last().json == {"primaryId": 1, "mergedId": 2}
    await call("identity_merges")
    assert backend.last().path == f"{B}/merges"


async def test_tags_and_segments(backend, call, call_error):
    await call("identity_tags")
    await call("identity_manage_tag", {"action": "create", "name": "高价值"})
    assert backend.last().json == {"name": "高价值", "color": "", "description": ""}
    await call("identity_manage_tag", {"action": "assign", "tag_id": 2, "identity_id": 3})
    assert (backend.last().path, backend.last().json) == (f"{B}/tags/assign", {"identityId": 3, "tagId": 2})
    await call("identity_manage_tag", {"action": "delete", "tag_id": 2})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/tags/2")
    await call_error("identity_manage_tag", {"action": "remove", "tag_id": 2})
    await call("identity_segments", {"segment_id": 4})
    assert backend.last().path == f"{B}/segments/4/members"
    await call("identity_save_segment", {"name": "s", "segment_type": "dynamic", "rules": {"a": 1}})
    assert backend.last().json == {"name": "s", "description": "", "segmentType": "dynamic", "rules": {"a": 1}}
    await call("identity_save_segment", {"name": "s", "segment_id": 4})
    assert (backend.last().method, backend.last().path) == ("PUT", f"{B}/segments/4")
    await call("identity_segment_member", {"segment_id": 4, "identity_id": 3, "action": "remove"})
    assert (backend.last().method, backend.last().path) == ("DELETE", f"{B}/segments/4/members/3")
    await call("identity_delete_segment", {"segment_id": 4})
    assert backend.last().method == "DELETE"


async def test_lists_appeals_deactivation(backend, call, call_error):
    await call("identity_lists", {"list_type": "blacklist"})
    assert backend.last().query == {"listType": "blacklist", "page": "1", "limit": "20"}
    await call_error("identity_add_list_entry", {"list_type": "blacklist", "reason": "r"})
    await call("identity_add_list_entry", {"list_type": "blacklist", "reason": "r", "ip": "1.2.3.4"})
    assert backend.last().json == {"listType": "blacklist", "reason": "r", "ip": "1.2.3.4"}
    await call("identity_remove_list_entry", {"entry_id": 8})
    await call("identity_check_list", {"phone": "138"})
    assert (backend.last().path, backend.last().json) == (f"{B}/lists/check", {"phone": "138"})
    await call("identity_appeals", {"status": "pending"})
    await call("identity_appeal", {"action": "approved", "appeal_id": 6, "comment": "ok"})
    assert (backend.last().method, backend.last().path, backend.last().json) == ("PUT", f"{B}/appeals/6", {"action": "approved", "comment": "ok"})
    await call_error("identity_appeal", {"action": "create"})
    await call("identity_deactivations")
    await call("identity_deactivate", {"identity_id": 3, "reason": "用户申请"})
    assert backend.last().json == {"identityId": 3, "reason": "用户申请", "coolingDays": 7}
    await call("identity_cancel_deactivation", {"deactivation_id": 2})
    assert backend.last().path == f"{B}/deactivations/2/cancel"


async def test_user_settings(backend, call):
    await call("identity_user_settings", {"app": "demo", "user_id": 5})
    assert (backend.last().path, backend.last().query) == ("/api/admin/user-settings/user", {"appid": "42", "userId": "5"})
    await call("identity_user_settings", {"app": "demo", "check_integrity": True})
    assert backend.last().query == {"appid": "42"}
    await call("identity_initialize_settings", {"app": "demo", "repair": True})
    assert (backend.last().path, backend.last().query) == ("/api/admin/user-settings/check-integrity", {"appid": "42", "autoRepair": "true"})
    await call("identity_user_settings", {"app": "demo"})
    assert backend.last().path == "/api/admin/user-settings/stats"
    await call("identity_initialize_settings", {"app": "demo", "batch_size": 100})
    assert (backend.last().path, backend.last().json) == ("/api/admin/user-settings/batch-initialize", {"appid": 42, "batchSize": 100})
    await call("identity_cleanup_settings", {"app": "demo"})
    assert (backend.last().method, backend.last().json) == ("DELETE", {"AppID": 42, "DryRun": True})
