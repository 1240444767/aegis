package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// registerMCPRoutes 注册 MCP 服务端的自述接口（/api/admin/mcp/*），见 docs/mcp.md。
//
// 只挂 AdminAuth：MCP 密钥与控制台会话都能调（控制台用它预览一把密钥能调哪些接口）。
// 密钥的签发与管理在 /api/admin/profile/mcp-keys，只接受会话。
func registerMCPRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService = deps.Admin
		auditService = deps.Audit
	)
	mcp := router.Group("/api/admin/mcp")
	mcp.Use(middleware.AdminAuth(adminService))
	mcp.Use(middleware.AuditMiddleware(auditService))
	{
		mcp.GET("/whoami", h.AdminMCPWhoAmI)
		// 目录要读路由表，路由表在 NewRouter 返回前才完整 —— 闭包在请求时才读，此时早已注册完毕
		mcp.GET("/endpoints", h.mcpEndpointsHandler(router))
	}
}
