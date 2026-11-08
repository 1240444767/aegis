package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	emaildomain "aegis/internal/domain/email"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 邮件策略、场景目录、模板预览、概览、投递详情与抑制名单。
//
// 与通道配置同一条分工：应用级走 `/api/admin/app/email-config/*`（appid 在请求体里），
// 平台级走 `/api/admin/system/email/*`（appid 恒为平台级，不接受请求体里的 appid）。

// ── DTO ──

// AdminEmailScopeRequest 只带应用标识的请求。
type AdminEmailScopeRequest struct {
	AppID int64 `json:"appid" form:"appid" binding:"required"`
}

// AdminEmailPolicySaveRequest 保存应用级邮件策略。
type AdminEmailPolicySaveRequest struct {
	AppID  int64              `json:"appid" binding:"required"`
	Policy emaildomain.Policy `json:"policy"`
}

// EmailPolicySaveRequest 保存平台级邮件策略。
type EmailPolicySaveRequest struct {
	Policy emaildomain.Policy `json:"policy"`
}

// EmailPreviewRequest 模板预览。policy 留空时用已保存的策略。
type EmailPreviewRequest struct {
	Scene  string              `json:"scene" binding:"required"`
	Policy *emaildomain.Policy `json:"policy"`
}

// AdminEmailPreviewRequest 应用级模板预览。
type AdminEmailPreviewRequest struct {
	AppID int64 `json:"appid" binding:"required"`
	EmailPreviewRequest
}

// AdminEmailOverviewRequest 应用级概览。tz 为浏览器时区（趋势按它分日）。
type AdminEmailOverviewRequest struct {
	AppID int64  `json:"appid" binding:"required"`
	Days  int    `json:"days"`
	TZ    string `json:"tz"`
}

// AdminEmailDeliveryDetailRequest 应用级投递详情。
type AdminEmailDeliveryDetailRequest struct {
	AppID      int64 `json:"appid" binding:"required"`
	DeliveryID int64 `json:"delivery_id" binding:"required"`
}

// AdminEmailSuppressionListRequest 应用级抑制名单。
type AdminEmailSuppressionListRequest struct {
	AppID    int64  `json:"appid" binding:"required"`
	Reason   string `json:"reason"`
	Keyword  string `json:"keyword"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

// EmailSuppressionCreateRequest 手动加入抑制名单。expires_at 为空即长期有效。
type EmailSuppressionCreateRequest struct {
	Address   string     `json:"address" binding:"required"`
	Reason    string     `json:"reason"`
	Note      string     `json:"note"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// AdminEmailSuppressionCreateRequest 应用级手动加入抑制名单。
type AdminEmailSuppressionCreateRequest struct {
	AppID int64 `json:"appid" binding:"required"`
	EmailSuppressionCreateRequest
}

// AdminEmailSuppressionDeleteRequest 应用级移出抑制名单。
type AdminEmailSuppressionDeleteRequest struct {
	AppID int64 `json:"appid" binding:"required"`
	ID    int64 `json:"id" binding:"required"`
}

// ── 目录 ──

// EmailSceneCatalog 邮件场景目录：静态自述，两个作用域共用。
func (h *Handler) EmailSceneCatalog(c *gin.Context) {
	response.Success(c, http.StatusOK, "获取成功", h.email.SceneCatalog())
}

// ── 应用级 ──

func (h *Handler) AdminEmailPolicy(c *gin.Context) {
	var req AdminEmailScopeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.respondEmail(c, "获取成功")(h.email.GetPolicy(c.Request.Context(), req.AppID))
}

func (h *Handler) AdminEmailPolicySave(c *gin.Context) {
	var req AdminEmailPolicySaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	h.respondEmail(c, "保存成功")(h.email.SavePolicy(c.Request.Context(), req.AppID, req.Policy, operator))
}

func (h *Handler) AdminEmailPreview(c *gin.Context) {
	var req AdminEmailPreviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.respondEmail(c, "获取成功")(h.email.Preview(c.Request.Context(), req.AppID,
		service.EmailPreviewRequest{Scene: req.Scene, Policy: req.Policy}))
}

func (h *Handler) AdminEmailOverview(c *gin.Context) {
	var req AdminEmailOverviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.respondEmail(c, "获取成功")(h.email.DeliveryOverview(c.Request.Context(), req.AppID, req.Days, req.TZ))
}

func (h *Handler) AdminEmailDeliveryDetail(c *gin.Context) {
	var req AdminEmailDeliveryDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.respondEmail(c, "获取成功")(h.email.DeliveryDetail(c.Request.Context(), req.AppID, req.DeliveryID))
}

func (h *Handler) AdminEmailSuppressionList(c *gin.Context) {
	var req AdminEmailSuppressionListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.respondEmail(c, "获取成功")(h.email.ListSuppressions(c.Request.Context(), emaildomain.SuppressionQuery{
		AppID: req.AppID, Reason: strings.TrimSpace(req.Reason), Keyword: strings.TrimSpace(req.Keyword),
		Page: req.Page, PageSize: req.PageSize,
	}))
}

func (h *Handler) AdminEmailSuppressionCreate(c *gin.Context) {
	var req AdminEmailSuppressionCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.createEmailSuppression(c, req.AppID, req.EmailSuppressionCreateRequest)
}

func (h *Handler) AdminEmailSuppressionDelete(c *gin.Context) {
	var req AdminEmailSuppressionDeleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.email.RemoveSuppression(c.Request.Context(), req.AppID, req.ID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已移出抑制名单", nil)
}

// ── 平台级 ──

func (h *Handler) AdminPlatformEmailPolicy(c *gin.Context) {
	h.respondEmail(c, "获取成功")(h.email.GetPolicy(c.Request.Context(), emaildomain.PlatformAppID))
}

func (h *Handler) AdminPlatformEmailPolicySave(c *gin.Context) {
	var req EmailPolicySaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	h.respondEmail(c, "保存成功")(h.email.SavePolicy(c.Request.Context(), emaildomain.PlatformAppID, req.Policy, operator))
}

func (h *Handler) AdminPlatformEmailPreview(c *gin.Context) {
	var req EmailPreviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.respondEmail(c, "获取成功")(h.email.Preview(c.Request.Context(), emaildomain.PlatformAppID,
		service.EmailPreviewRequest{Scene: req.Scene, Policy: req.Policy}))
}

func (h *Handler) AdminPlatformEmailOverview(c *gin.Context) {
	h.respondEmail(c, "获取成功")(h.email.DeliveryOverview(c.Request.Context(), emaildomain.PlatformAppID,
		int(parseInt64Query(c, "days")), strings.TrimSpace(c.Query("tz"))))
}

func (h *Handler) AdminPlatformEmailDeliveryDetail(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("deliveryId")), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "投递记录标识非法")
		return
	}
	h.respondEmail(c, "获取成功")(h.email.DeliveryDetail(c.Request.Context(), emaildomain.PlatformAppID, id))
}

func (h *Handler) AdminPlatformEmailSuppressionList(c *gin.Context) {
	h.respondEmail(c, "获取成功")(h.email.ListSuppressions(c.Request.Context(), emaildomain.SuppressionQuery{
		AppID:    emaildomain.PlatformAppID,
		Reason:   strings.TrimSpace(c.Query("reason")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Page:     int(parseInt64Query(c, "page")),
		PageSize: int(parseInt64Query(c, "pageSize")),
	}))
}

func (h *Handler) AdminPlatformEmailSuppressionCreate(c *gin.Context) {
	var req EmailSuppressionCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.createEmailSuppression(c, emaildomain.PlatformAppID, req)
}

func (h *Handler) AdminPlatformEmailSuppressionDelete(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("suppressionId")), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "抑制记录标识非法")
		return
	}
	if err := h.email.RemoveSuppression(c.Request.Context(), emaildomain.PlatformAppID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已移出抑制名单", nil)
}

// ── 共用 ──

func (h *Handler) createEmailSuppression(c *gin.Context, appID int64, req EmailSuppressionCreateRequest) {
	_, operator := adminAccount(c)
	item, err := h.email.AddSuppression(c.Request.Context(), emaildomain.SuppressionInput{
		AppID:     appID,
		Address:   req.Address,
		Reason:    strings.TrimSpace(req.Reason),
		Note:      req.Note,
		ExpiresAt: req.ExpiresAt,
		Operator:  operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已加入抑制名单", item)
}

// respondEmail 把「结果 + 错误」两段返回值收成一次响应。
func (h *Handler) respondEmail(c *gin.Context, message string) func(any, error) {
	return func(data any, err error) {
		if err != nil {
			h.writeError(c, err)
			return
		}
		response.Success(c, http.StatusOK, message, data)
	}
}
