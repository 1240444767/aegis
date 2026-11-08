package httptransport

import (
	"net/http"

	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 应用级 Passkey 依赖方：管理端读写，以及给 RP ID 域名代理用的 assetlinks 副本。

func (h *Handler) passkeyPolicyReady(c *gin.Context) bool {
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50322, "当前安全模块暂不可用")
		return false
	}
	return true
}

// AdminAppPasskeyPolicy GET /api/admin/apps/:appkey/passkey
func (h *Handler) AdminAppPasskeyPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.passkeyPolicyReady(c) {
		return
	}
	result, err := h.security.AppPasskeyPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// UpdateAdminAppPasskeyPolicy PUT /api/admin/apps/:appkey/passkey
func (h *Handler) UpdateAdminAppPasskeyPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.passkeyPolicyReady(c) {
		return
	}
	var req appdomain.PasskeyPolicy
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.security.SaveAppPasskeyPolicy(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Passkey 配置已保存", result)
}

// PublicAppPasskeyAssetLinks GET /api/public/apps/:appkey/passkey/assetlinks.json
//
// 原样输出 Digital Asset Links 数组（不套统一响应壳）：RP ID 域名的网站把
// /.well-known/assetlinks.json 反代或定时同步到这里，指纹只在控制台维护一处。
func (h *Handler) PublicAppPasskeyAssetLinks(c *gin.Context) {
	if h.security == nil || h.app == nil {
		c.JSON(http.StatusServiceUnavailable, []any{})
		return
	}
	app, err := h.app.GetAppByKey(c.Request.Context(), c.Param("appkey"))
	if err != nil || app == nil || !app.Status {
		c.JSON(http.StatusNotFound, []any{})
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.security.AppPasskeyAssetLinks(app))
}
