package middleware

import (
	"net/http"
	"testing"

	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
)

func mcpAccess(super bool, key admindomain.MCPKeyScope) *admindomain.AccessContext {
	return &admindomain.AccessContext{
		Session: admindomain.Session{AdminID: 1, IsSuperAdmin: super},
		MCPKey:  &key,
	}
}

func mcpErrorCode(err error) int {
	if appErr, ok := err.(*apperrors.AppError); ok {
		return appErr.Code
	}
	return 0
}

// 密钥在中间件这一层要挡住 Authorize 看不到的东西：方法、无权限点模块、平台级写。
func TestEnforceMCPKeyRoute(t *testing.T) {
	cases := []struct {
		name       string
		super      bool
		key        admindomain.MCPKeyScope
		method     string
		path       string
		permission string
		appScoped  bool
		want       int
	}{
		{"只读密钥读", false, admindomain.MCPKeyScope{Permissions: []string{"app:user:read"}, ReadOnly: true}, http.MethodGet, "/api/admin/apps/:appkey/users", "app:user:read", true, 0},
		{"只读密钥写", false, admindomain.MCPKeyScope{Permissions: []string{"*"}, ReadOnly: true}, http.MethodPost, "/api/admin/apps/:appkey/users", "app:user:write", true, 40166},
		{"工单需覆盖读", true, admindomain.MCPKeyScope{Permissions: []string{"app:user:read"}}, http.MethodGet, "/api/admin/tickets", "", false, 40164},
		{"工单读", true, admindomain.MCPKeyScope{Permissions: []string{"ticket:read"}}, http.MethodGet, "/api/admin/tickets", "", false, 0},
		{"超管写工单需全组", true, admindomain.MCPKeyScope{Permissions: []string{"ticket:read", "ticket:reply"}}, http.MethodPost, "/api/admin/tickets/:ticketId/replies", "", false, 40164},
		{"超管写工单全组", true, admindomain.MCPKeyScope{Permissions: []string{"ticket:*"}}, http.MethodPost, "/api/admin/tickets/:ticketId/replies", "", false, 0},
		{"非超管写工单交给 service", false, admindomain.MCPKeyScope{Permissions: []string{"ticket:read", "ticket:reply"}}, http.MethodPost, "/api/admin/tickets/:ticketId/replies", "", false, 0},
		{"无权限点写需全部", false, admindomain.MCPKeyScope{Permissions: []string{"app:*"}}, http.MethodPost, "/api/admin/apps", "", false, 40164},
		{"无权限点写全部", false, admindomain.MCPKeyScope{Permissions: []string{"*"}}, http.MethodPost, "/api/admin/apps", "", false, 0},
		{"限定应用不能平台写", false, admindomain.MCPKeyScope{Permissions: []string{"*"}, AppIDs: []int64{3}}, http.MethodPut, "/api/admin/system/settings", "system:settings:write", false, 40165},
		{"限定应用可应用写", false, admindomain.MCPKeyScope{Permissions: []string{"*"}, AppIDs: []int64{3}}, http.MethodPut, "/api/admin/apps/:appkey/users/:id", "app:user:write", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPermissionTestContext(tc.method, tc.path)
			err := enforceMCPKeyRoute(c, mcpAccess(tc.super, tc.key), tc.permission, tc.appScoped)
			if got := mcpErrorCode(err); got != tc.want {
				t.Fatalf("期望错误码 %d，得到 %d（%v）", tc.want, got, err)
			}
		})
	}
}

func TestMCPKeySessionRouteAllowed(t *testing.T) {
	allowed := []struct{ method, path string }{
		{http.MethodGet, "/api/admin/auth/me"},
		{http.MethodGet, "/api/admin/apps"},
		{http.MethodGet, "/api/admin/mcp/whoami"},
	}
	for _, item := range allowed {
		if !mcpKeySessionRouteAllowed(newPermissionTestContext(item.method, item.path)) {
			t.Errorf("%s %s 应接受 MCP 密钥", item.method, item.path)
		}
	}
	denied := []struct{ method, path string }{
		{http.MethodPost, "/api/admin/auth/logout"},
		{http.MethodGet, "/api/admin/profile/mcp-keys"},
		{http.MethodPost, "/api/admin/profile/mcp-keys"},
		{http.MethodPost, "/api/admin/profile/password"},
		{http.MethodGet, "/api/admin/profile"},
		{http.MethodPost, "/api/admin/notifications/read"},
	}
	for _, item := range denied {
		if mcpKeySessionRouteAllowed(newPermissionTestContext(item.method, item.path)) {
			t.Errorf("%s %s 不应接受 MCP 密钥", item.method, item.path)
		}
	}
}
