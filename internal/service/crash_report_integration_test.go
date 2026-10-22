package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/crashreport"
	pgrepo "aegis/internal/repository/postgres"
)

// 崩溃报告的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「登录与未登录上报 → 去重 → 按设备 / IP 限频（数库）→ 列表筛选（含下划线关键字）→ 摘要 →
// 详情带正文与对外用户 ID → 标记已处理 → 批量删除 → 注销账号后报告保留」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestCrashReportIntegration
func TestCrashReportIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewCrashReportService(zap.NewNop(), pg, nil)
	const appID = int64(21000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'crash-test'), ($1 + 1, 'crash-other') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	userID := insertTestUser(t, ctx, pool, appID, "crash-a", "0")
	code := int64(321)

	base := crashreport.SubmitInput{
		AppID: appID, UserID: userID, ReportID: "rpt-java-0001", Kind: "java",
		Title:      "java.lang.IllegalStateException: view_not_attached",
		Detail:     "at com.example.player_ui.PlayerFragment.onResume",
		Report:     "FATAL EXCEPTION: main\njava.lang.IllegalStateException: view_not_attached\n\tat com.example.Foo.bar(Foo.kt:42)",
		OccurredAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		AppVersion: "1.2.0", VersionCode: &code, OSVersion: "15", Model: "23127PN0CC", ABI: "arm64-v8a",
		ProcessName: "com.example", Source: "crash_screen", DeviceID: "device-a", ClientIP: "203.0.113.9",
		Device:    crashreport.DeviceInfo{Platform: "android", Name: "Xiaomi 14", Manufacturer: "Xiaomi"},
		UserAgent: "okhttp/5",
	}
	first, err := svc.Submit(ctx, base)
	if err != nil || first.Duplicate || !strings.HasPrefix(first.ID, "CR") {
		t.Fatalf("第一次上报：%+v %v", first, err)
	}
	again, err := svc.Submit(ctx, base)
	if err != nil || !again.Duplicate || again.ID != first.ID {
		t.Fatalf("重复上报应返回原来那一条：%+v %v", again, err)
	}
	// 另一个应用里同号不冲突
	other := base
	other.AppID, other.UserID = appID+1, 0
	if result, err := svc.Submit(ctx, other); err != nil || result.Duplicate {
		t.Fatalf("另一个应用的同号报告应各自入库：%+v %v", result, err)
	}

	guest := base
	guest.UserID, guest.ReportID, guest.Kind, guest.AppVersion = 0, "rpt-native-01", "native", "1.3.0"
	guest.Title, guest.Detail, guest.DeviceID, guest.ClientIP = "signal 11 (SIGSEGV)", "", "device-b", "198.51.100.7"
	guest.VersionCode = nil
	if _, err := svc.Submit(ctx, guest); err != nil {
		t.Fatal(err)
	}

	// ── 限频：device-c 在一小时内报满 20 份，第 21 份被拒；重复上报不受影响 ──
	for index := 0; index < crashreport.HourlyLimit; index++ {
		item := guest
		item.ReportID = fmt.Sprintf("rpt-flood-%04d", index)
		item.DeviceID = "device-c"
		item.ClientIP = fmt.Sprintf("192.0.2.%d", index+1)
		if _, err := svc.Submit(ctx, item); err != nil {
			t.Fatalf("第 %d 份不应被限：%v", index+1, err)
		}
	}
	flood := guest
	flood.ReportID, flood.DeviceID, flood.ClientIP = "rpt-flood-over", "device-c", "192.0.2.200"
	if _, err := svc.Submit(ctx, flood); errCode(err) != 42900 {
		t.Fatalf("同一设备第 21 份应报 42900，得到 %v", err)
	}
	flood.ReportID = "rpt-flood-0000"
	if result, err := svc.Submit(ctx, flood); err != nil || !result.Duplicate {
		t.Fatalf("已收到的报告重传应按重复返回，不被限频：%+v %v", result, err)
	}

	// ── 列表与筛选 ──
	page, err := svc.List(ctx, crashreport.Query{AppID: appID, Kind: "java", Page: 1, Limit: 20})
	if err != nil || page.Total != 1 || page.Items[0].Report != "" || page.Items[0].ReportBytes == 0 {
		t.Fatalf("按类型筛选应只有一条且不带正文：%+v %v", page, err)
	}
	listed := page.Items[0]
	if listed.UserID != userID || listed.Account != "crash-a" || listed.UserPublicID < 1_000_000_000_000_000 {
		t.Fatalf("列表应带账号与对外用户 ID：%+v", listed)
	}
	// 关键字里的下划线按字面匹配
	if page, _ := svc.List(ctx, crashreport.Query{AppID: appID, Keyword: "player_ui", Page: 1, Limit: 20}); page.Total != 1 {
		t.Fatalf("按详情关键字应命中一条，得到 %d", page.Total)
	}
	if page, _ := svc.List(ctx, crashreport.Query{AppID: appID, Keyword: "playerXui", Page: 1, Limit: 20}); page.Total != 0 {
		t.Fatalf("下划线不应当成通配符，得到 %d", page.Total)
	}
	if page, _ := svc.List(ctx, crashreport.Query{AppID: appID, Keyword: strings.ToLower(first.ID), Page: 1, Limit: 20}); page.Total != 1 {
		t.Fatalf("按对外编号应能搜到，得到 %d", page.Total)
	}
	if page, _ := svc.List(ctx, crashreport.Query{AppID: appID, AppVersion: "1.3.0", Page: 1, Limit: 5}); page.Total != 21 || len(page.Items) != 5 || page.TotalPages != 5 {
		t.Fatalf("按版本筛选并分页：%+v", page)
	}
	future := time.Now().Add(time.Hour)
	if page, _ := svc.List(ctx, crashreport.Query{AppID: appID, Start: &future, Page: 1, Limit: 20}); page.Total != 0 {
		t.Fatalf("时间范围筛选不对，得到 %d", page.Total)
	}

	facets, err := svc.Facets(ctx, appID)
	if err != nil || facets.Total != 22 || facets.Unresolved != 22 || facets.Last24h != 22 ||
		len(facets.Versions) != 2 || facets.Versions[0].AppVersion != "1.3.0" || len(facets.Kinds) != 2 {
		t.Fatalf("摘要不对：%+v %v", facets, err)
	}

	// ── 详情、标记已处理、删除 ──
	detail, err := svc.Get(ctx, appID, listed.ID)
	if err != nil || detail.Report != base.Report || detail.Device.Name != "Xiaomi 14" || detail.VersionCode == nil ||
		*detail.VersionCode != 321 || detail.PublicID != first.ID || detail.Source != "crash_screen" {
		t.Fatalf("详情不对：%+v %v", detail, err)
	}
	if _, err := svc.Get(ctx, appID+1, listed.ID); errCode(err) != errCodeCrashReportNotFound {
		t.Fatalf("别的应用不能看到这条报告，得到 %v", err)
	}
	if updated, err := svc.SetResolved(ctx, appID, []int64{listed.ID}, true, "admin"); err != nil || updated != 1 {
		t.Fatalf("标记已处理：%d %v", updated, err)
	}
	if updated, _ := svc.SetResolved(ctx, appID, []int64{listed.ID}, true, "admin"); updated != 0 {
		t.Fatalf("已处理的不应重复改写，得到 %d", updated)
	}
	resolved := true
	if page, _ := svc.List(ctx, crashreport.Query{AppID: appID, Resolved: &resolved, Page: 1, Limit: 20}); page.Total != 1 ||
		!page.Items[0].Resolved || page.Items[0].ResolvedBy != "admin" {
		t.Fatalf("按已处理筛选：%+v", page)
	}

	flooded, _ := svc.List(ctx, crashreport.Query{AppID: appID, DeviceID: "device-c", Page: 1, Limit: 100})
	ids := make([]int64, 0, len(flooded.Items))
	for _, item := range flooded.Items {
		ids = append(ids, item.ID)
	}
	if deleted, err := svc.Delete(ctx, appID+1, ids); err != nil || deleted != 0 {
		t.Fatalf("别的应用不能删这些报告：%d %v", deleted, err)
	}
	if deleted, err := svc.Delete(ctx, appID, ids); err != nil || deleted != int64(len(ids)) {
		t.Fatalf("批量删除：%d %v", deleted, err)
	}
	// 删掉之后该设备的限频额度随之释放
	flood.ReportID = "rpt-flood-after"
	if _, err := svc.Submit(ctx, flood); err != nil {
		t.Fatalf("删除后应能再报：%v", err)
	}

	// 账号删除后报告保留，只解除归属
	if _, err := pool.Exec(ctx, `DELETE FROM user_wallets WHERE user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	orphan, err := svc.Get(ctx, appID, listed.ID)
	if err != nil || orphan.UserID != 0 || orphan.UserPublicID != 0 {
		t.Fatalf("账号删除后报告应保留且不再挂账号：%+v %v", orphan, err)
	}
}
