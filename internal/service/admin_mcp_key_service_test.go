package service

import (
	"testing"

	admindomain "aegis/internal/domain/admin"
)

func TestMCPPermissionMatches(t *testing.T) {
	cases := []struct {
		pattern, permission string
		want                bool
	}{
		{"*", "app:user:write", true},
		{"ticket:*", "ticket:read", true},
		{"ticket:*", "tickets:read", false},
		{"app:*", "app:user:write", true},
		{"app:user:read", "app:user:read", true},
		{"app:user:read", "app:user:write", false},
		{"", "app:read", false},
	}
	for _, tc := range cases {
		if got := admindomain.MCPPermissionMatches(tc.pattern, tc.permission); got != tc.want {
			t.Errorf("%q ~ %q：期望 %v，得到 %v", tc.pattern, tc.permission, tc.want, got)
		}
	}
}

func TestNormalizeMCPPermissions(t *testing.T) {
	got, err := normalizeMCPPermissions([]string{" ticket:* ", "app:user:read", "ticket:*"})
	if err != nil || len(got) != 2 || got[0] != "app:user:read" || got[1] != "ticket:*" {
		t.Fatalf("去重排序失败：%v %v", got, err)
	}
	if got, _ := normalizeMCPPermissions([]string{"app:read", "*"}); len(got) != 1 || got[0] != "*" {
		t.Fatalf("含 * 时应只留 *：%v", got)
	}
	for _, bad := range [][]string{{}, {"app:nope"}, {"nope:*"}} {
		if _, err := normalizeMCPPermissions(bad); err == nil {
			t.Errorf("%v 应被拒绝", bad)
		}
	}
}

func TestMCPAllowIPs(t *testing.T) {
	ips, err := normalizeMCPAllowIPs([]string{"10.0.0.0/8", "192.168.1.5", "::ffff:192.168.1.5", "10.1.2.3/8"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 2 {
		t.Fatalf("应规范化去重成两条：%v", ips)
	}
	if !mcpIPAllowed(ips, "10.20.30.40") || !mcpIPAllowed(ips, "192.168.1.5") || mcpIPAllowed(ips, "192.168.1.6") {
		t.Fatal("允许列表判定错误")
	}
	if !mcpIPAllowed(nil, "") {
		t.Fatal("空列表应不限来源")
	}
	if _, err := normalizeMCPAllowIPs([]string{"300.1.1.1"}); err == nil {
		t.Fatal("非法 IP 应被拒绝")
	}
}

func TestAuthorizeMCPScope(t *testing.T) {
	access := &admindomain.AccessContext{
		Session: admindomain.Session{AdminID: 1, IsSuperAdmin: true},
		MCPKey:  &admindomain.MCPKeyScope{Name: "k", Permissions: []string{"app:user:read"}, AppIDs: []int64{7}},
	}
	app7, app8 := int64(7), int64(8)
	if err := authorizeMCPScope(access, "app:user:read", &app7); err != nil {
		t.Fatalf("范围内应放行：%v", err)
	}
	if err := authorizeMCPScope(access, "app:user:write", &app7); err == nil {
		t.Fatal("超管的密钥也必须受权限范围约束")
	}
	if err := authorizeMCPScope(access, "app:user:read", &app8); err == nil {
		t.Fatal("范围外的应用应拒绝")
	}
	svc := &AdminService{}
	if err := svc.Authorize(t.Context(), access, "app:user:write", &app7); err == nil {
		t.Fatal("Authorize 的超管短路不能绕过密钥范围")
	}
}

func TestRestrictSnapshotToMCPKey(t *testing.T) {
	snapshot := admindomain.PermissionSnapshot{
		Global: []string{"app:read", "ticket:read"},
		Apps:   map[string][]string{"5": {"app:read", "app:user:read", "ticket:read"}, "6": {"app:read"}},
	}
	key := &admindomain.MCPKeyScope{Permissions: []string{"app:*"}, AppIDs: []int64{5, 9}}
	got := restrictSnapshotToMCPKey(snapshot, key)
	if len(got.Global) != 1 || got.Global[0] != "app:read" {
		t.Fatalf("全局集收窄错误：%v", got.Global)
	}
	if len(got.Apps["5"]) != 2 || len(got.Apps["9"]) != 1 || got.Apps["6"] != nil {
		t.Fatalf("应用集收窄错误：%v", got.Apps)
	}
}
