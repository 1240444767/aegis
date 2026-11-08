package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 公开模型目录（docs/ai-gateway.md「模型目录」）：管理端检索、匹配、刷新与同步到网关模型；
// App 端按模型名批量查能力（用户自己接的供应商拉完模型列表后用它补全能力）。

// AdminAIModelCatalog GET /api/admin/apps/:appkey/ai-gateway/model-catalog
func (h *Handler) AdminAIModelCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok || !h.aiGatewayReady(c) {
		return
	}
	status, err := h.aiGateway.ModelCatalogStatus(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", status)
}

// AdminRefreshAIModelCatalog POST /api/admin/apps/:appkey/ai-gateway/model-catalog/refresh
func (h *Handler) AdminRefreshAIModelCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok || !h.aiGatewayReady(c) {
		return
	}
	status, err := h.aiGateway.RefreshModelCatalog(c.Request.Context(), true)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", status)
}

// AdminSearchAIModelCatalog GET /api/admin/apps/:appkey/ai-gateway/model-catalog/search?q=&provider=&kind=&limit=
func (h *Handler) AdminSearchAIModelCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok || !h.aiGatewayReady(c) {
		return
	}
	entries, err := h.aiGateway.SearchModelCatalog(c.Request.Context(), c.Query("q"), c.Query("provider"),
		strings.TrimSpace(c.Query("kind")), parsePositiveInt(c.Query("limit"), 50))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": entries})
}

// AdminMatchAIModelCatalog GET /api/admin/apps/:appkey/ai-gateway/model-catalog/match?model=&configId=
func (h *Handler) AdminMatchAIModelCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	model := strings.TrimSpace(c.Query("model"))
	configID, _ := strconv.ParseInt(c.Query("configId"), 10, 64)
	provider := strings.TrimSpace(c.Query("provider"))
	if provider == "" {
		provider = h.aiGateway.ChannelProviderFor(c.Request.Context(), appID, configID, model)
	}
	candidates, err := h.aiGateway.MatchModelCatalog(c.Request.Context(), model, provider, parsePositiveInt(c.Query("limit"), 5))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": candidates})
}

// AdminSyncAIGatewayModels POST /api/admin/apps/:appkey/ai-gateway/models/sync
func (h *Handler) AdminSyncAIGatewayModels(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	var req service.CatalogSyncInput
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	results, err := h.aiGateway.SyncModelsFromCatalog(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": results})
}

// appCatalogLookupLimit 一次最多查多少个模型。
const appCatalogLookupLimit = 200

// AppAICatalogLookup GET /api/v1/apps/:appkey/ai/catalog/lookup?models=a,b&providers=openai
//
// 免登录：用户在 App 里接自己的供应商时还不一定登录了。只回能力与上限，不回价格。
// providers 是目录里的供应商名（openai、anthropic、google、deepseek …），作为匹配提示。
func (h *Handler) AppAICatalogLookup(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok || !h.aiGatewayReady(c) {
		return
	}
	var models []string
	for _, item := range strings.Split(c.Query("models"), ",") {
		if item = strings.TrimSpace(item); item != "" {
			models = append(models, item)
		}
	}
	if len(models) > appCatalogLookupLimit {
		models = models[:appCatalogLookupLimit]
	}
	var providers []string
	for _, item := range strings.Split(c.Query("providers"), ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			providers = append(providers, item)
		}
	}
	items, err := h.aiGateway.LookupModelCatalog(c.Request.Context(), models, providers)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}
