package middleware

import (
	"net/http"
	"strings"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	"aegis/internal/service"
	apperrors "aegis/pkg/errors"
	"github.com/gin-gonic/gin"
)

// MCP 密钥在管理端中间件里的落点，见 docs/mcp.md。
//
// 权限点与应用范围在 AdminService.Authorize 里判（service 层的细粒度判定也经过它），
// 这里只管 Authorize 看不到的三件事：请求方法（只读）、没有权限点的路由、
// 以及只挂 AdminAuth 的「管理员自己」那组路由。

// isMCPKeyToken 令牌是不是 MCP 密钥（固定前缀，与会话 JWT 不可能相撞）。
func isMCPKeyToken(token string) bool {
	return strings.HasPrefix(token, admindomain.MCPKeyPrefix)
}

// authenticateAdmin 按令牌形态分流：MCP 密钥走密钥校验，其余按会话 JWT。
func authenticateAdmin(c *gin.Context, adminService *service.AdminService, token string) (*admindomain.AccessContext, error) {
	if isMCPKeyToken(token) {
		return adminService.ValidateMCPKey(c.Request.Context(), token, RequestClientIP(c))
	}
	return adminService.ValidateAccessToken(c.Request.Context(), token)
}

// mcpKeySessionRoutes 只挂 AdminAuth 的路由里，接受 MCP 密钥的那几条（只读）。
var mcpKeySessionRoutes = map[string]bool{
	"/api/admin/auth/me":                    true,
	"/api/admin/apps":                       true,
	"/api/admin/system/banners/active":      true,
	"/api/admin/notifications":              true,
	"/api/admin/notifications/unread-count": true,
}

func mcpKeySessionRouteAllowed(c *gin.Context) bool {
	path := routePath(c)
	if strings.HasPrefix(path, "/api/admin/mcp/") {
		return true
	}
	if !isReadMethod(c.Request.Method) {
		return false
	}
	return mcpKeySessionRoutes[path]
}

// mcpImplicitPermissions 路由规则表里「不要权限点、细粒度下沉到 service 层」的模块。
//
// 这些模块的 service 层对超管直接短路（工单可见范围、组织架构），密钥范围在那里没有落点。
// 因此在入口处要求密钥至少覆盖该模块的读权限，否则一把只给了「用户查看」的超管密钥
// 就能读全站工单。
//
// 写操作对非超管照常交给 service 层逐项判（经过 Authorize，密钥范围在那里生效）；
// 超管在 service 层被短路，因此超管的密钥写这些模块要求覆盖该模块的全部权限点。
var mcpImplicitPermissions = []struct {
	prefix     string
	permission string
	group      string
}{
	{prefix: "/api/admin/tickets", permission: "ticket:read", group: "ticket:"},
	{prefix: "/api/admin/system/organizations", permission: "org:dept:read", group: "org:"},
}

// enforceMCPKeyRoute AdminAccess 在 Authorize 之前对 MCP 密钥的额外约束。
func enforceMCPKeyRoute(c *gin.Context, access *admindomain.AccessContext, permission string, appScoped bool) error {
	if access == nil || access.MCPKey == nil {
		return nil
	}
	key := access.MCPKey
	write := !isReadMethod(c.Request.Method)
	if write && key.ReadOnly && !IsReadPermission(permission) {
		return apperrors.New(40166, http.StatusForbidden,
			"MCP 密钥「"+key.Name+"」是只读密钥，只能调用查询接口")
	}
	if permission == "" {
		path := routePath(c)
		for _, item := range mcpImplicitPermissions {
			if strings.HasPrefix(path, item.prefix) {
				if !key.AllowsPermission(item.permission) {
					return apperrors.New(40164, http.StatusForbidden,
						"MCP 密钥「"+key.Name+"」的权限范围不包含「"+item.permission+"」，不能进入该模块")
				}
				if write && access.IsSuperAdmin && !mcpKeyCoversGroup(key, item.group) {
					return apperrors.New(40164, http.StatusForbidden,
						"超级管理员的 MCP 密钥写该模块需覆盖「"+item.group+"*」的全部权限点")
				}
				return nil
			}
		}
		if write && !key.AllowsAll() {
			return apperrors.New(40164, http.StatusForbidden,
				"该接口没有登记具体权限点，MCP 密钥需授予「全部权限」才能调用其写操作")
		}
		return nil
	}
	// 限定了应用的密钥不能改平台级（跨应用）的东西：应用范围在平台作用域下没有意义，
	// 放行等于让「只管应用 A」的密钥改到所有应用。
	if write && !appScoped && key.RestrictsApps() {
		return apperrors.New(40165, http.StatusForbidden,
			"MCP 密钥「"+key.Name+"」限定了应用，不能调用平台级写接口")
	}
	return nil
}

// mcpKeyCoversGroup 密钥是否覆盖权限目录里该前缀下的每一个权限点。
func mcpKeyCoversGroup(key *admindomain.MCPKeyScope, prefix string) bool {
	for _, code := range authz.AllPermissionCodes() {
		if strings.HasPrefix(code, prefix) && !key.AllowsPermission(code) {
			return false
		}
	}
	return true
}

// IsReadPermission 规则表把这条路由判成读权限点（`*:read`）。
//
// 一批旧接口与查询接口用 POST 承载（支付配置列表、订单详情、积分统计、发布模拟、邮件预览……），
// 规则表已经按读判定它们；只读密钥据此放行，而不是一律按方法拒绝。
func IsReadPermission(permission string) bool {
	return strings.HasSuffix(permission, ":read")
}

func isReadMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
