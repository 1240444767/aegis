package httptransport

import (
	"net/http"

	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 功能时长：余额、租约与结算（见 docs/feature-time.md）。应用由 appkey 决定，用户由访问令牌决定。

// FeatureTimeLeaseRequest 租一段时长。
type FeatureTimeLeaseRequest struct {
	// Seconds 要租多少秒，1–600，超过 600 按 600 截断；余额不足时按余额给
	Seconds int `json:"seconds"`
}

// FeatureTimeSettleRequest 结算一个租约。
type FeatureTimeSettleRequest struct {
	// UsedSeconds 这段租约实际用了多少秒，夹到 [0, 租约秒数]，没用完的退回余额
	UsedSeconds int `json:"usedSeconds"`
}

// AppFeatureTimeStatus 当前用户在一个功能上的时长：余额、累计获得与用掉的秒数、是否免扣。
func (h *Handler) AppFeatureTimeStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.featureTime == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "功能时长服务不可用")
		return
	}
	status, err := h.featureTime.Status(c.Request.Context(), session.AppID, session.UserID, c.Param("feature"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", status)
}

// AppLeaseFeatureTime 开始使用前租一段时长。免扣时不扣余额（exempt=true），余额为 0 返回 40328。
func (h *Handler) AppLeaseFeatureTime(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.featureTime == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "功能时长服务不可用")
		return
	}
	var req FeatureTimeLeaseRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	lease, err := h.featureTime.Lease(c.Request.Context(), session.AppID, session.UserID, c.Param("feature"), req.Seconds)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "租用成功", lease)
}

// AppSettleFeatureTimeLease 停止使用时结算租约，没用完的退回余额。重复结算返回第一次的结论。
func (h *Handler) AppSettleFeatureTimeLease(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.featureTime == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "功能时长服务不可用")
		return
	}
	var req FeatureTimeSettleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	settlement, err := h.featureTime.Settle(c.Request.Context(), session.AppID, session.UserID, c.Param("leaseNo"), req.UsedSeconds)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "结算成功", settlement)
}
