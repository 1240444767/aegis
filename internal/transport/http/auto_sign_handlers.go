package httptransport

import (
	"net/http"

	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// AutoSignUpdateRequest 修改自动签到设置。只改传了的字段。
type AutoSignUpdateRequest struct {
	// Enabled 开关。从关到开时校验应用的开放策略，不满足返回 40353 / 40354
	Enabled *bool `json:"enabled"`
	// Time 每天的签到时刻，HH:MM，按状态里的 timezone 解释
	Time            *string `json:"time"`
	RetryOnFail     *bool   `json:"retryOnFail"`
	MaxRetries      *int    `json:"maxRetries"`
	NotifyOnSuccess *bool   `json:"notifyOnSuccess"`
	NotifyOnFail    *bool   `json:"notifyOnFail"`
}

func (r AutoSignUpdateRequest) patch() map[string]any {
	patch := map[string]any{}
	if r.Enabled != nil {
		patch["enabled"] = *r.Enabled
	}
	if r.Time != nil {
		patch["time"] = *r.Time
	}
	if r.RetryOnFail != nil {
		patch["retryOnFail"] = *r.RetryOnFail
	}
	if r.MaxRetries != nil {
		patch["maxRetries"] = *r.MaxRetries
	}
	if r.NotifyOnSuccess != nil {
		patch["notifyOnSuccess"] = *r.NotifyOnSuccess
	}
	if r.NotifyOnFail != nil {
		patch["notifyOnFail"] = *r.NotifyOnFail
	}
	return patch
}

// AdminAutoSignPolicyRequest 管理端保存开放策略。
type AdminAutoSignPolicyRequest struct {
	Policy appdomain.AutoSignPolicy `json:"policy" binding:"required"`
}

// AppAutoSignStatus 当前用户的自动签到状态：开放策略、资格、个人设置、下一次执行与最近一次结果。
func (h *Handler) AppAutoSignStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.autoSign == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "自动签到服务不可用")
		return
	}
	status, err := h.autoSign.Status(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", status)
}

// AppUpdateAutoSign 修改自动签到设置，立即按新设置重排，返回修改后的状态。
func (h *Handler) AppUpdateAutoSign(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.autoSign == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "自动签到服务不可用")
		return
	}
	var req AutoSignUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	ctx := c.Request.Context()
	if patch := req.patch(); len(patch) > 0 {
		if _, err := h.user.UpdateSettings(ctx, session, "autoSign", patch); err != nil {
			h.writeError(c, err)
			return
		}
		// 设置变更事件也会触发重排（失败时由它兜底），这里同步再排一次，返回的下一次执行时刻才是新的
		_ = h.autoSign.SyncUserSchedule(ctx, session.UserID, session.AppID)
	}
	status, err := h.autoSign.Status(ctx, session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", status)
}

// AdminAppAutoSign 管理端：自动签到开放策略与使用情况。
func (h *Handler) AdminAppAutoSign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if h.autoSign == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "自动签到服务不可用")
		return
	}
	overview, err := h.autoSign.AdminOverview(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", overview)
}

// UpdateAdminAppAutoSign 管理端：保存自动签到开放策略。
func (h *Handler) UpdateAdminAppAutoSign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if h.autoSign == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "自动签到服务不可用")
		return
	}
	var req AdminAutoSignPolicyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	overview, err := h.autoSign.SavePolicy(c.Request.Context(), h.app, appID, req.Policy)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "自动签到策略已保存", overview)
}
