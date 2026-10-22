package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/domain/devicelimit"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 登录设备数控制接口。规则见 docs/device-limit.md。
//
// 网关：/auth/device-limit/resolve（免登录，凭选择页凭证完成登录）、
//       /me/devices（我的登录设备与上限）、/me/devices/remove（下线我的其他设备）。
// 管理端：/api/admin/apps/:appkey/device-limit/*，读 app:user:read；改策略 app:write；
//         例外、下线、整理、清台账 app:user:write。

// AppDeviceLimitResolveRequest 选择页提交：凭证与勾选要下线的设备 Key。
type AppDeviceLimitResolveRequest struct {
	Ticket     string   `json:"ticket"`
	DeviceKeys []string `json:"deviceKeys"`
}

// DeviceKeysRequest 下线若干设备。
type DeviceKeysRequest struct {
	DeviceKeys []string `json:"deviceKeys"`
}

// AdminDeviceLimitOverviewQuery 概览参数（仅用于接口文档）。
type AdminDeviceLimitOverviewQuery struct {
	Days int    `json:"days" form:"days"`
	TZ   string `json:"tz" form:"tz"`
}

func (h *Handler) deviceLimitReady(c *gin.Context) bool {
	if h.deviceLimit == nil {
		response.Error(c, http.StatusServiceUnavailable, 50392, "设备数控制未启用")
		return false
	}
	return true
}

/* ───────────────────── 网关 ───────────────────── */

// AppDeviceLimitResolve POST /api/v1/apps/:appkey/auth/device-limit/resolve
// 登录结果里出现 deviceLimit 时，用户勾好要下线的设备后调这里，成功返回与登录相同的 LoginResult。
func (h *Handler) AppDeviceLimitResolve(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AppDeviceLimitResolveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.ResolveDeviceLimitChallenge(c.Request.Context(), app.ID, req.Ticket, req.DeviceKeys)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, authResultMessage(result, "登录成功"), result)
}

// AppMyDevices GET /api/v1/apps/:appkey/me/devices
func (h *Handler) AppMyDevices(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.deviceLimitReady(c) {
		return
	}
	result, err := h.deviceLimit.Status(c.Request.Context(), session.AppID, session.UserID, session.RefreshFamilyID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AppRemoveMyDevices POST /api/v1/apps/:appkey/me/devices/remove
// 下线自己的其他设备；当前设备不能在这里下线（用退出登录）。返回下线后的状况。
func (h *Handler) AppRemoveMyDevices(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.deviceLimitReady(c) {
		return
	}
	var req DeviceKeysRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	ctx := c.Request.Context()
	if _, err := h.deviceLimit.RemoveDevices(ctx, session.AppID, session.UserID, req.DeviceKeys,
		devicelimit.EventUserRemoved, "", session.RefreshFamilyID); err != nil {
		h.writeError(c, err)
		return
	}
	result, err := h.deviceLimit.Status(ctx, session.AppID, session.UserID, session.RefreshFamilyID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "设备已下线", result)
}

/* ───────────────────── 管理端 ───────────────────── */

// AdminDeviceLimitOverview GET /api/admin/apps/:appkey/device-limit?days=7&tz=Asia/Shanghai
func (h *Handler) AdminDeviceLimitOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	days, _ := strconv.Atoi(c.Query("days"))
	result, err := h.deviceLimit.Overview(c.Request.Context(), appID, days, c.Query("tz"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminDeviceLimitPolicy GET /api/admin/apps/:appkey/device-limit/policy
func (h *Handler) AdminDeviceLimitPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	result, err := h.deviceLimit.Policy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminUpdateDeviceLimitPolicy PUT /api/admin/apps/:appkey/device-limit/policy
func (h *Handler) AdminUpdateDeviceLimitPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	var req devicelimit.Policy
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.deviceLimit.SavePolicy(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", result)
}

// AdminSimulateDeviceLimit POST /api/admin/apps/:appkey/device-limit/simulate
func (h *Handler) AdminSimulateDeviceLimit(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	var req devicelimit.SimulateInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.deviceLimit.Simulate(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "模拟完成", result)
}

// AdminDeviceLimitEvents GET /api/admin/apps/:appkey/device-limit/events
// 参数：page、limit、action、dimension、userId、keyword、from、to（RFC 3339）。
func (h *Handler) AdminDeviceLimitEvents(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	query := devicelimit.EventQuery{
		Action: strings.TrimSpace(c.Query("action")), Dimension: strings.TrimSpace(c.Query("dimension")),
		UserID: userID, Keyword: c.Query("keyword"), Page: page, Limit: limit,
	}
	if from, err := time.Parse(time.RFC3339, c.Query("from")); err == nil {
		query.From = &from
	}
	if to, err := time.Parse(time.RFC3339, c.Query("to")); err == nil {
		query.To = &to
	}
	result, err := h.deviceLimit.Events(c.Request.Context(), appID, query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminDeviceLimitOverrides GET /api/admin/apps/:appkey/device-limit/overrides?page=&limit=&keyword=
func (h *Handler) AdminDeviceLimitOverrides(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.deviceLimit.Overrides(c.Request.Context(), appID, devicelimit.OverrideQuery{
		Keyword: c.Query("keyword"), Page: page, Limit: limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminSaveDeviceLimitOverride PUT /api/admin/apps/:appkey/device-limit/overrides/:userId
func (h *Handler) AdminSaveDeviceLimitOverride(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req devicelimit.OverrideInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.deviceLimit.SaveOverride(c.Request.Context(), appID, userID, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", result)
}

// AdminDeleteDeviceLimitOverride DELETE /api/admin/apps/:appkey/device-limit/overrides/:userId
func (h *Handler) AdminDeleteDeviceLimitOverride(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if err := h.deviceLimit.DeleteOverride(c.Request.Context(), appID, userID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"userId": userID})
}

// AdminDeviceLimitUser GET /api/admin/apps/:appkey/device-limit/users/:userId
func (h *Handler) AdminDeviceLimitUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	result, err := h.deviceLimit.UserView(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminRemoveDeviceLimitDevices POST /api/admin/apps/:appkey/device-limit/users/:userId/remove
func (h *Handler) AdminRemoveDeviceLimitDevices(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req DeviceKeysRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	removed, err := h.deviceLimit.RemoveDevices(c.Request.Context(), appID, userID, req.DeviceKeys,
		devicelimit.EventAdminRemoved, operator, "")
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "设备已下线", gin.H{"removed": removed})
}

// AdminEnforceDeviceLimit POST /api/admin/apps/:appkey/device-limit/users/:userId/enforce
// 按当前策略把该用户已登录的设备收回到上限之内。
func (h *Handler) AdminEnforceDeviceLimit(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	_, operator := adminAccount(c)
	removed, err := h.deviceLimit.Enforce(c.Request.Context(), appID, userID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已按当前策略整理", gin.H{"removed": removed})
}

// AdminResetDeviceLimitLedger DELETE /api/admin/apps/:appkey/device-limit/users/:userId/ledger?key=
// 不带 key 清空整本台账（新设备频率从零算起），带 key 只删那一台。
func (h *Handler) AdminResetDeviceLimitLedger(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deviceLimitReady(c) {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	removed, err := h.deviceLimit.ResetLedger(c.Request.Context(), appID, userID, c.Query("key"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已清除", gin.H{"removed": removed})
}
