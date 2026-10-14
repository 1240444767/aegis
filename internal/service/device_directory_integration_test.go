package service

import (
	"testing"

	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 设备目录与按 ID 批量取用户：发布中心的内测、排除名单靠它们把 ID 还原成人与设备。
//
// AEGIS_TEST_PG_DSN=... go test ./internal/service -run TestDeviceDirectoryIntegration
func TestDeviceDirectoryIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	users := NewUserService(zap.NewNop(), pg, nil, nil, nil)

	const appID, otherApp = int64(42000), int64(42001)
	for _, id := range []int64{appID, otherApp} {
		if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'device-dir') ON CONFLICT DO NOTHING`, id); err != nil {
			t.Fatal(err)
		}
	}
	alice := insertTestUser(t, ctx, pool, appID, "dd-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "dd-bob", "0")
	outsider := insertTestUser(t, ctx, pool, otherApp, "dd-outsider", "0")
	login := func(user int64, app int64, device, model, ago string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO login_audit_logs (user_id, appid, login_type, device_id, user_agent, status, metadata, created_at, login_ip)
VALUES ($1, $2, 'password', $3, 'okhttp/4.12', 'success', jsonb_build_object('device_model', $4::text, 'device_platform', 'android'), NOW() - $5::interval, '203.0.113.7')`,
			user, app, device, model, ago); err != nil {
			t.Fatal(err)
		}
	}
	login(alice, appID, "dev-pixel", "Pixel 8", "3 days")
	login(alice, appID, "dev-pixel", "Pixel 8", "1 hour")
	login(bob, appID, "dev-pixel", "Pixel 8", "2 hours") // 同一台设备两个人用
	login(bob, appID, "dev-mi", "23127PN0CC", "1 day")
	login(outsider, otherApp, "dev-other", "iPhone", "1 hour")
	login(alice, appID, "dev-ancient", "Nexus 5", "500 days")

	list, err := users.ListAdminDevices(ctx, appID, userdomain.AdminDeviceQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].DeviceID != "dev-pixel" {
		t.Fatalf("应只列出本应用近一年的两台设备，最近活跃的在前：%+v", list)
	}
	pixel := list[0]
	if pixel.Logins != 3 || pixel.Users != 2 || pixel.LastUser == nil || pixel.LastUser.ID != alice || pixel.LastIP != "203.0.113.7" {
		t.Fatalf("聚合结果不符：%+v %+v", pixel, pixel.LastUser)
	}
	if pixel.DeviceInfo == nil || pixel.DeviceInfo.Name == "" {
		t.Fatalf("应解析出设备名：%+v", pixel.DeviceInfo)
	}

	for kw, want := range map[string]string{"pixel": "dev-pixel", "dd-bob": "dev-pixel", "23127": "dev-mi", "dev-m": "dev-mi"} {
		got, err := users.ListAdminDevices(ctx, appID, userdomain.AdminDeviceQuery{Keyword: kw})
		if err != nil || len(got) == 0 || got[0].DeviceID != want {
			t.Errorf("关键字 %q 应先命中 %s：%+v %v", kw, want, got, err)
		}
	}
	if got, _ := users.ListAdminDevices(ctx, appID, userdomain.AdminDeviceQuery{Keyword: "50%_x"}); len(got) != 0 {
		t.Errorf("LIKE 通配符应被转义：%+v", got)
	}
	byUser, _ := users.ListAdminDevices(ctx, appID, userdomain.AdminDeviceQuery{UserID: bob})
	if len(byUser) != 2 {
		t.Errorf("bob 用过两台设备：%+v", byUser)
	}
	// 按 ID 取不受一年窗口限制，跨应用的设备取不到
	byIDs, _ := users.ListAdminDevices(ctx, appID, userdomain.AdminDeviceQuery{IDs: []string{"dev-ancient", "dev-other", "missing"}})
	if len(byIDs) != 1 || byIDs[0].DeviceID != "dev-ancient" {
		t.Errorf("按 ID 批量取不符：%+v", byIDs)
	}

	// 按 ID 批量取用户：只返回本应用的人
	found, err := users.ListAdminUsers(ctx, appID, userdomain.AdminUserQuery{IDs: []int64{bob, alice, outsider}})
	if err != nil {
		t.Fatal(err)
	}
	if found.Total != 2 {
		t.Fatalf("应取回本应用的两个用户：%+v", found.Items)
	}
}
