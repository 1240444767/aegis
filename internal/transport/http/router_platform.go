package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 平台治理与平台级配置（恒全局作用域）。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerPlatformGovernanceRoutes 注册平台治理。
func registerPlatformGovernanceRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
	// ─── 平台治理（全站作用域）─────────────────────────────
	//
	// 与 /api/admin/apps/:appkey/* 的分工：那一组是**应用作用域**，
	// 授权按管理员绑定的应用判定；这一组是**全局作用域**，
	// resolveAdminPermission 对 /api/admin/platform/ 前缀恒返回 appScoped=false，
	// 所以应用级管理员即使被授了 platform:app:govern 也进不来 —— 不存在自己解自己封禁。
	adminPlatform := router.Group("/api/admin/platform")
	adminPlatform.Use(middleware.AdminAccess(adminService, appService, governanceService))
	adminPlatform.Use(middleware.AuditMiddleware(auditService))
	{
		adminPlatform.GET("/catalog", h.AdminPlatformGovernanceCatalog)
		adminPlatform.GET("/overview", h.AdminPlatformOverview)
		adminPlatform.GET("/apps", h.AdminPlatformOverview)
		adminPlatform.POST("/apps/batch-governance", h.AdminPlatformBatchGovernance)
		adminPlatform.GET("/apps/:appkey/governance", h.AdminPlatformAppGovernance)
		adminPlatform.POST("/apps/:appkey/governance", h.AdminPlatformApplyGovernance)
		adminPlatform.POST("/apps/:appkey/revoke-sessions", h.AdminPlatformRevokeAppSessions)
		adminPlatform.GET("/governance/actions", h.AdminPlatformGovernanceActions)
		adminPlatform.GET("/governance/appeals", h.AdminPlatformListAppeals)
		adminPlatform.POST("/governance/appeals/:appealId/review", h.AdminPlatformReviewAppeal)
	}
}

// registerPlatformStorageRoutes 注册平台级存储配置。
func registerPlatformStorageRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
	globalStorageAdmin := router.Group("/api/admin/platform/storage-config")
	globalStorageAdmin.Use(middleware.AdminAccess(adminService, appService, governanceService))
	globalStorageAdmin.Use(middleware.AuditMiddleware(auditService))
	{
		globalStorageAdmin.POST("/list", h.AdminGlobalStorageConfigList)
		globalStorageAdmin.POST("/detail", h.AdminGlobalStorageConfigDetail)
		globalStorageAdmin.POST("/create", h.AdminGlobalStorageConfigCreate)
		globalStorageAdmin.POST("/update", h.AdminGlobalStorageConfigUpdate)
		globalStorageAdmin.POST("/delete", h.AdminGlobalStorageConfigDelete)
		globalStorageAdmin.POST("/test", h.AdminGlobalStorageConfigTest)
	}

	// 存储中心：存储池、运行事件、深度检测、CORS、迁移。
	storageCenter := router.Group("/api/admin/platform/storage")
	storageCenter.Use(middleware.AdminAccess(adminService, appService, governanceService))
	storageCenter.Use(middleware.AuditMiddleware(auditService))
	{
		storageCenter.GET("/overview", h.AdminStorageOverview)
		storageCenter.GET("/events", h.AdminStorageEvents)
		storageCenter.POST("/usage/collect", h.AdminCollectStorageUsage)
		storageCenter.GET("/pools", h.AdminStoragePools)
		storageCenter.POST("/pools", h.AdminCreateStoragePool)
		storageCenter.GET("/pools/:poolId", h.AdminStoragePool)
		storageCenter.PUT("/pools/:poolId", h.AdminUpdateStoragePool)
		storageCenter.DELETE("/pools/:poolId", h.AdminDeleteStoragePool)
		storageCenter.POST("/pools/:poolId/simulate", h.AdminSimulateStoragePool)
		storageCenter.POST("/configs/:configId/probe", h.AdminProbeStorageConfig)
		storageCenter.POST("/configs/:configId/reset-health", h.AdminResetStorageHealth)
		storageCenter.GET("/configs/:configId/cors", h.AdminStorageCORS)
		storageCenter.PUT("/configs/:configId/cors", h.AdminSetStorageCORS)
		storageCenter.DELETE("/configs/:configId/redirect", h.AdminClearStorageRedirect)
		storageCenter.GET("/migrations", h.AdminStorageMigrations)
		storageCenter.POST("/migrations", h.AdminCreateStorageMigration)
		storageCenter.GET("/migrations/:migrationId", h.AdminStorageMigration)
		storageCenter.POST("/migrations/:migrationId/pause", h.AdminPauseStorageMigration)
		storageCenter.POST("/migrations/:migrationId/resume", h.AdminResumeStorageMigration)
		storageCenter.POST("/migrations/:migrationId/cancel", h.AdminCancelStorageMigration)
		storageCenter.POST("/cloudflare/connect", h.AdminCloudflareConnect)
		storageCenter.POST("/cloudflare/resources", h.AdminCloudflareResources)
		storageCenter.POST("/cloudflare/buckets", h.AdminCloudflareCreateBucket)
		storageCenter.POST("/cloudflare/bucket", h.AdminCloudflareBucketDetail)
		storageCenter.POST("/cloudflare/setup", h.AdminCloudflareSetup)
	}
}
