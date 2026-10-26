package httptransport

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authprotocol "aegis/internal/domain/authprotocol"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 版本管控接口。契约见 docs/client-control.md。
//
// 网关：/api/v1/apps/:appkey/client-control/check、/watch，免登录（带令牌时记下用户）。
//       上报与裁决走独立的加密通道，与应用安全等级无关。
// 管理端：/api/admin/apps/:appkey/client-control/*，权限 version:read / version:write。

// ClientControlAPI 版本管控服务对 HTTP 层暴露的能力。
type ClientControlAPI interface {
	Spec(ctx context.Context, app *appdomain.App) (*authprotocol.ClientControlSpec, error)
	Check(ctx context.Context, app *appdomain.App, req service.ClientControlSealedRequest, userID int64, ip string) (*service.ClientControlSealedResponse, error)
	Watch(ctx context.Context, appID int64, since int64, wait time.Duration) (*appdomain.ClientControlWatchResult, error)
	// SignFeatureTicket 用同一套签名密钥签权益凭证（POST /vip/feature-ticket）。
	SignFeatureTicket(ctx context.Context, app *appdomain.App, ticket *service.FeatureTicket) (*service.FeatureTicketEnvelope, error)

	Overview(ctx context.Context, appID int64) (*appdomain.ClientControlOverview, error)
	Settings(ctx context.Context, appID int64) (*appdomain.ClientControlSettings, error)
	UpdateSettings(ctx context.Context, appID int64, patch appdomain.ClientControlSettingsPatch, operator string) (*appdomain.ClientControlSettings, error)
	Keys(ctx context.Context, appID int64) ([]appdomain.ClientControlKeyView, error)
	RotateKey(ctx context.Context, appID int64, kind string) (*appdomain.ClientControlKeyView, error)
	Rules(ctx context.Context, appID int64) ([]appdomain.ClientControlRule, error)
	CreateRule(ctx context.Context, appID int64, input appdomain.ClientControlRuleInput, operator string) (*appdomain.ClientControlRule, error)
	UpdateRule(ctx context.Context, appID int64, ruleID int64, input appdomain.ClientControlRuleInput) (*appdomain.ClientControlRule, error)
	SetRuleEnabled(ctx context.Context, appID int64, ruleID int64, enabled bool) (*appdomain.ClientControlRule, error)
	DeleteRule(ctx context.Context, appID int64, ruleID int64) error
	ReorderRules(ctx context.Context, appID int64, orderedIDs []int64) ([]appdomain.ClientControlRule, error)
	Versions(ctx context.Context, appID int64, days int) ([]appdomain.ClientInstallGroup, error)
	Installs(ctx context.Context, appID int64, query appdomain.ClientInstallQuery) (*appdomain.ClientInstallListResult, error)
	DeleteInstall(ctx context.Context, appID int64, deviceID string, packageName string) error
	Simulate(ctx context.Context, appID int64, report appdomain.ClientReport) (*appdomain.ClientControlDecision, error)
	Refresh(ctx context.Context, appID int64) (int64, error)
}

// clientControlWatchMaxWait 长轮询最长挂起时间，留在常见反代 30 秒空闲超时之内。
const clientControlWatchMaxWait = 25 * time.Second

// AppClientControlWatchQuery 长轮询参数（仅用于接口文档）。
type AppClientControlWatchQuery struct {
	Since int64 `json:"since" form:"since"`
	Wait  int   `json:"wait" form:"wait"`
}

// AdminClientControlSimulateRequest 以某个客户端的身份模拟一次判定。
type AdminClientControlSimulateRequest struct {
	PackageName string `json:"packageName"`
	VersionName string `json:"versionName"`
	VersionCode int64  `json:"versionCode"`
	Platform    string `json:"platform"`
	CertSHA256  string `json:"certSha256"`
	Installer   string `json:"installer"`
	DeviceID    string `json:"deviceId"`
	OSVersion   int    `json:"osVersion"`
	UserID      int64  `json:"userId"`
}

// AdminClientControlReorderRequest ruleIds 按期望的匹配顺序排列，第一个最先匹配。
type AdminClientControlReorderRequest struct {
	RuleIDs []int64 `json:"ruleIds"`
}

func (h *Handler) clientControlReady(c *gin.Context) bool {
	if h.clientControl == nil {
		response.Error(c, http.StatusServiceUnavailable, 50391, "版本管控未启用")
		return false
	}
	return true
}

func clientControlRuleIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("ruleId"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "规则 ID 无效")
		return 0, false
	}
	return id, true
}

/* ───────────────────── 网关 ───────────────────── */

// AppClientControlCheck POST /api/v1/apps/:appkey/client-control/check
// 请求体是加密后的上报（ClientControlSealedRequest），响应 data 是加密后的签名裁决。
func (h *Handler) AppClientControlCheck(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok || !h.clientControlReady(c) {
		return
	}
	var req service.ClientControlSealedRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "管控请求格式无效")
		return
	}
	var userID int64
	if session := gatewayReleaseSession(c); session != nil && session.AppID == app.ID {
		userID = session.UserID
	}
	result, err := h.clientControl.Check(c.Request.Context(), app, req, userID, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "校验完成", result)
}

// AppClientControlWatch GET /api/v1/apps/:appkey/client-control/watch?since=&wait=
// 策略版本与 since 不同时立即返回 changed=true；否则最多挂起 wait 秒（上限 25）。
func (h *Handler) AppClientControlWatch(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok || !h.clientControlReady(c) {
		return
	}
	since, _ := strconv.ParseInt(strings.TrimSpace(c.Query("since")), 10, 64)
	wait := clientControlWatchMaxWait
	if raw := strings.TrimSpace(c.Query("wait")); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
			wait = min(time.Duration(seconds)*time.Second, clientControlWatchMaxWait)
		}
	}
	result, err := h.clientControl.Watch(c.Request.Context(), app.ID, since, wait)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", result)
}

/* ───────────────────── 管理端 ───────────────────── */

// AdminClientControlOverview GET /api/admin/apps/:appkey/client-control
func (h *Handler) AdminClientControlOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	result, err := h.clientControl.Overview(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminClientControlSettings GET /api/admin/apps/:appkey/client-control/settings
func (h *Handler) AdminClientControlSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	result, err := h.clientControl.Settings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminUpdateClientControlSettings PUT /api/admin/apps/:appkey/client-control/settings
func (h *Handler) AdminUpdateClientControlSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	var req appdomain.ClientControlSettingsPatch
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.clientControl.UpdateSettings(c.Request.Context(), appID, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", result)
}

// AdminClientControlKeys GET /api/admin/apps/:appkey/client-control/keys
func (h *Handler) AdminClientControlKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	result, err := h.clientControl.Keys(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminRotateClientControlKey POST /api/admin/apps/:appkey/client-control/keys/:kind/rotate
// kind：sign（裁决签名密钥，新密钥由旧密钥背书）或 box（上报加密密钥）。
func (h *Handler) AdminRotateClientControlKey(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	kind := strings.TrimSpace(c.Param("kind"))
	if kind != appdomain.ClientControlKeySign && kind != appdomain.ClientControlKeyBox {
		response.Error(c, http.StatusBadRequest, 40000, "密钥类型只能是 sign 或 box")
		return
	}
	result, err := h.clientControl.RotateKey(c.Request.Context(), appID, kind)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已轮换", result)
}

// AdminClientControlRules GET /api/admin/apps/:appkey/client-control/rules
func (h *Handler) AdminClientControlRules(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	result, err := h.clientControl.Rules(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminCreateClientControlRule POST /api/admin/apps/:appkey/client-control/rules
func (h *Handler) AdminCreateClientControlRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	var req appdomain.ClientControlRuleInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.clientControl.CreateRule(c.Request.Context(), appID, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已创建", result)
}

// AdminUpdateClientControlRule PUT /api/admin/apps/:appkey/client-control/rules/:ruleId
func (h *Handler) AdminUpdateClientControlRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	ruleID, ok := clientControlRuleIDParam(c)
	if !ok {
		return
	}
	var req appdomain.ClientControlRuleInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.clientControl.UpdateRule(c.Request.Context(), appID, ruleID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", result)
}

// AdminEnableClientControlRule POST /api/admin/apps/:appkey/client-control/rules/:ruleId/enable
func (h *Handler) AdminEnableClientControlRule(c *gin.Context) {
	h.setClientControlRuleEnabled(c, true)
}

// AdminDisableClientControlRule POST /api/admin/apps/:appkey/client-control/rules/:ruleId/disable
func (h *Handler) AdminDisableClientControlRule(c *gin.Context) {
	h.setClientControlRuleEnabled(c, false)
}

func (h *Handler) setClientControlRuleEnabled(c *gin.Context, enabled bool) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	ruleID, ok := clientControlRuleIDParam(c)
	if !ok {
		return
	}
	result, err := h.clientControl.SetRuleEnabled(c.Request.Context(), appID, ruleID, enabled)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已停用"
	if enabled {
		message = "已启用"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AdminDeleteClientControlRule DELETE /api/admin/apps/:appkey/client-control/rules/:ruleId
func (h *Handler) AdminDeleteClientControlRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	ruleID, ok := clientControlRuleIDParam(c)
	if !ok {
		return
	}
	if err := h.clientControl.DeleteRule(c.Request.Context(), appID, ruleID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"id": ruleID})
}

// AdminReorderClientControlRules POST /api/admin/apps/:appkey/client-control/rules/reorder
func (h *Handler) AdminReorderClientControlRules(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	var req AdminClientControlReorderRequest
	if err := bind(c, &req); err != nil || len(req.RuleIDs) == 0 {
		response.Error(c, http.StatusBadRequest, 40000, "缺少 ruleIds")
		return
	}
	result, err := h.clientControl.ReorderRules(c.Request.Context(), appID, req.RuleIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已调整顺序", result)
}

// AdminClientControlVersions GET /api/admin/apps/:appkey/client-control/versions?days=30
func (h *Handler) AdminClientControlVersions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	result, err := h.clientControl.Versions(c.Request.Context(), appID, queryInt(c, "days", 30))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminClientControlDevices GET /api/admin/apps/:appkey/client-control/devices
func (h *Handler) AdminClientControlDevices(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	versionCode, _ := strconv.ParseInt(strings.TrimSpace(c.Query("versionCode")), 10, 64)
	result, err := h.clientControl.Installs(c.Request.Context(), appID, appdomain.ClientInstallQuery{
		Keyword:     c.Query("keyword"),
		PackageName: strings.TrimSpace(c.Query("packageName")),
		VersionCode: versionCode,
		CertSHA256:  strings.TrimSpace(c.Query("certSha256")),
		Action:      strings.TrimSpace(c.Query("action")),
		Days:        queryInt(c, "days", 0),
		Page:        queryInt(c, "page", 1),
		Limit:       queryInt(c, "limit", 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminDeleteClientControlDevice DELETE /api/admin/apps/:appkey/client-control/devices/:deviceId?packageName=
// 只删除设备清单里的这条记录，不影响规则；设备下次上报会重新出现。
func (h *Handler) AdminDeleteClientControlDevice(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	deviceID := strings.TrimSpace(c.Param("deviceId"))
	packageName := strings.TrimSpace(c.Query("packageName"))
	if deviceID == "" || packageName == "" {
		response.Error(c, http.StatusBadRequest, 40000, "缺少设备或包名")
		return
	}
	if err := h.clientControl.DeleteInstall(c.Request.Context(), appID, deviceID, packageName); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deviceId": deviceID, "packageName": packageName})
}

// AdminSimulateClientControl POST /api/admin/apps/:appkey/client-control/simulate
func (h *Handler) AdminSimulateClientControl(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	var req AdminClientControlSimulateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.clientControl.Simulate(c.Request.Context(), appID, appdomain.ClientReport{
		PackageName: req.PackageName, VersionName: req.VersionName, VersionCode: req.VersionCode,
		Platform: req.Platform, CertSHA256: req.CertSHA256, Installer: req.Installer,
		DeviceID: req.DeviceID, OSVersion: req.OSVersion, UserID: req.UserID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "模拟完成", result)
}

// AdminRefreshClientControl POST /api/admin/apps/:appkey/client-control/refresh
// 策略版本加一，正在长轮询的客户端立即重新校验。
func (h *Handler) AdminRefreshClientControl(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.clientControlReady(c) {
		return
	}
	version, err := h.clientControl.Refresh(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已通知客户端重新校验", gin.H{"policyVersion": version})
}
