package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/accountdeletion"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 账号注销：管理端（配置 / 申请 / 审核 / 撤销 / 立即执行 / 改期 / 代发起 / 重新注册限制 / 统计）
// 与用户端（注销页 / 发验证码 / 提交 / 撤销）。规则见 docs/account-deletion.md。

// ── DTO ──

// AccountDeletionSettingsRequest 注销的开关与配置。字段含义见 docs/account-deletion.md。
type AccountDeletionSettingsRequest struct {
	Enabled bool                   `json:"enabled"`
	Config  accountdeletion.Config `json:"config"`
}

// AccountDeletionNoteRequest 审核与撤销时的说明。
type AccountDeletionNoteRequest struct {
	Note   string `json:"note"`
	Notify *bool  `json:"notify"`
}

// AccountDeletionExecuteRequest 立即执行：confirm 填申请里的账号名。
type AccountDeletionExecuteRequest struct {
	Confirm string `json:"confirm" binding:"required"`
}

// AccountDeletionRescheduleRequest 改冷静期的到期时刻。
type AccountDeletionRescheduleRequest struct {
	ScheduledAt time.Time `json:"scheduledAt" binding:"required"`
	Notify      *bool     `json:"notify"`
}

// AccountDeletionAdminCreateRequest 管理员代用户发起注销。
// mode 为 immediate 时 confirm 必须填账号名；coolingDays 不传按应用配置。
type AccountDeletionAdminCreateRequest struct {
	Mode        string `json:"mode" binding:"required"`
	CoolingDays int    `json:"coolingDays"`
	Reason      string `json:"reason" binding:"required"`
	Notify      *bool  `json:"notify"`
	Confirm     string `json:"confirm"`
}

// AccountDeletionRequestQuery 管理端申请列表的查询参数。From / To 为 RFC 3339。
type AccountDeletionRequestQuery struct {
	Status  string `form:"status"`
	Mode    string `form:"mode"`
	Source  string `form:"source"`
	UserID  int64  `form:"userId"`
	Keyword string `form:"keyword"`
	From    string `form:"from"`
	To      string `form:"to"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

// AccountDeletionCodeRequest 发注销验证码。
type AccountDeletionCodeRequest struct {
	Channel string `json:"channel" binding:"required"`
}

// AccountDeletionSubmitRequest 提交注销。身份验证按 /me/deletion 的 verification 填：
// password，或 channel + code（先调 /me/deletion/code）；verification.totp 为真时再带 totpCode 或 recoveryCode。
type AccountDeletionSubmitRequest struct {
	Mode         string `json:"mode"`
	ReasonCode   string `json:"reasonCode"`
	ReasonText   string `json:"reasonText"`
	Agree        bool   `json:"agree"`
	Password     string `json:"password"`
	Channel      string `json:"channel"`
	Code         string `json:"code"`
	TOTPCode     string `json:"totpCode"`
	RecoveryCode string `json:"recoveryCode"`
	DeviceID     string `json:"deviceId"`
	Device       string `json:"device"`
}

func boolOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func (h *Handler) deletionReady(c *gin.Context) bool {
	if h.accountDeletion == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "账号注销服务不可用")
		return false
	}
	return true
}

// ── 管理端：配置 ──

// AdminAccountDeletionCatalog 配置表单的枚举、默认值与上限。
func (h *Handler) AdminAccountDeletionCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok || !h.deletionReady(c) {
		return
	}
	response.Success(c, http.StatusOK, "ok", h.accountDeletion.Catalog())
}

// AdminAccountDeletionSettings 注销的开关与配置。
func (h *Handler) AdminAccountDeletionSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	settings, err := h.accountDeletion.Settings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", settings)
}

// AdminSaveAccountDeletionSettings 保存注销的开关与配置。
func (h *Handler) AdminSaveAccountDeletionSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	var req AccountDeletionSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	settings, err := h.accountDeletion.SaveSettings(c.Request.Context(), appID, req.Enabled, req.Config, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "注销配置已保存", settings)
}

// AdminAccountDeletionStats 统计（days 7–180，tz 为浏览器时区）。
func (h *Handler) AdminAccountDeletionStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	stats, err := h.accountDeletion.Stats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 30), c.Query("tz"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// ── 管理端：申请 ──

// AdminListAccountDeletions 申请列表。
func (h *Handler) AdminListAccountDeletions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	var query AccountDeletionRequestQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	filter := accountdeletion.RequestQuery{
		AppID: appID, Status: query.Status, Mode: query.Mode, Source: query.Source, UserID: query.UserID,
		Keyword: query.Keyword, Page: query.Page, Limit: query.Limit,
	}
	for _, item := range []struct {
		raw    string
		target **time.Time
	}{{query.From, &filter.From}, {query.To, &filter.To}} {
		if strings.TrimSpace(item.raw) == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(item.raw))
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "时间格式应为 RFC 3339")
			return
		}
		*item.target = &parsed
	}
	page, err := h.accountDeletion.ListRequests(c.Request.Context(), filter)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminGetAccountDeletion 一条申请。
func (h *Handler) AdminGetAccountDeletion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "requestId")
	if !ok {
		return
	}
	req, err := h.accountDeletion.GetRequest(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", req)
}

// AdminApproveAccountDeletion 批准待审核的申请。
func (h *Handler) AdminApproveAccountDeletion(c *gin.Context) {
	h.deletionNoteAction(c, "已批准", func(c *gin.Context, appID, id int64, req AccountDeletionNoteRequest, operator string) (*accountdeletion.Request, error) {
		return h.accountDeletion.Approve(c.Request.Context(), appID, id, req.Note, operator)
	})
}

// AdminRejectAccountDeletion 驳回待审核的申请（理由必填）。
func (h *Handler) AdminRejectAccountDeletion(c *gin.Context) {
	h.deletionNoteAction(c, "已驳回", func(c *gin.Context, appID, id int64, req AccountDeletionNoteRequest, operator string) (*accountdeletion.Request, error) {
		return h.accountDeletion.Reject(c.Request.Context(), appID, id, req.Note, operator)
	})
}

// AdminCancelAccountDeletion 撤销申请（待审核、冷静期中、执行失败）。
func (h *Handler) AdminCancelAccountDeletion(c *gin.Context) {
	h.deletionNoteAction(c, "已撤销", func(c *gin.Context, appID, id int64, req AccountDeletionNoteRequest, operator string) (*accountdeletion.Request, error) {
		return h.accountDeletion.AdminCancel(c.Request.Context(), appID, id, req.Note, operator, boolOr(req.Notify, true))
	})
}

func (h *Handler) deletionNoteAction(c *gin.Context, message string,
	run func(c *gin.Context, appID, id int64, req AccountDeletionNoteRequest, operator string) (*accountdeletion.Request, error)) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "requestId")
	if !ok {
		return
	}
	var req AccountDeletionNoteRequest
	if c.Request.ContentLength != 0 {
		if err := bind(c, &req); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, err.Error())
			return
		}
	}
	_, operator := adminAccount(c)
	result, err := run(c, appID, id, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, message, result)
}

// AdminExecuteAccountDeletion 立即执行（冷静期中或执行失败的申请），confirm 填账号名。
func (h *Handler) AdminExecuteAccountDeletion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "requestId")
	if !ok {
		return
	}
	var req AccountDeletionExecuteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.accountDeletion.ExecuteNow(c.Request.Context(), appID, id, req.Confirm, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "账号已注销"
	if result.Status != accountdeletion.StatusCompleted {
		message = "执行失败：" + result.Failure
	}
	response.Success(c, http.StatusOK, message, result)
}

// AdminRescheduleAccountDeletion 改冷静期的到期时刻。
func (h *Handler) AdminRescheduleAccountDeletion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "requestId")
	if !ok {
		return
	}
	var req AccountDeletionRescheduleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.accountDeletion.Reschedule(c.Request.Context(), appID, id, req.ScheduledAt, boolOr(req.Notify, true))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "注销时间已调整", result)
}

// ── 管理端：用户 ──

// AdminUserAccountDeletion 某个用户的注销状况（进行中的申请与历史）。
func (h *Handler) AdminUserAccountDeletion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	view, err := h.accountDeletion.UserView(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", view)
}

// AdminCreateUserAccountDeletion 管理员代用户发起注销。
func (h *Handler) AdminCreateUserAccountDeletion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req AccountDeletionAdminCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.accountDeletion.AdminCreate(c.Request.Context(), service.AccountDeletionAdminCreateInput{
		AppID: appID, UserID: userID, Mode: req.Mode, CoolingDays: req.CoolingDays, Reason: req.Reason,
		Notify: boolOr(req.Notify, true), Confirm: req.Confirm, Operator: operator, IP: c.ClientIP(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已进入注销冷静期"
	switch result.Status {
	case accountdeletion.StatusCompleted:
		message = "账号已注销"
	case accountdeletion.StatusFailed:
		message = "执行失败：" + result.Failure
	}
	response.Success(c, http.StatusOK, message, result)
}

// ── 管理端：重新注册限制 ──

// AdminListAccountDeletionTombstones 未到期的重新注册限制。
func (h *Handler) AdminListAccountDeletionTombstones(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	page, err := h.accountDeletion.ListTombstones(c.Request.Context(), appID, c.Query("keyword"),
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminReleaseAccountDeletionTombstone 解除一条重新注册限制。
func (h *Handler) AdminReleaseAccountDeletionTombstone(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.deletionReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "tombstoneId")
	if !ok {
		return
	}
	if err := h.accountDeletion.ReleaseTombstone(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已解除限制", gin.H{"id": strconv.FormatInt(id, 10)})
}

// ── 用户端 ──

// AppAccountDeletion 注销页：方式、须知、原因、要过的验证、前置条件与进行中的申请。
func (h *Handler) AppAccountDeletion(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.deletionReady(c) {
		return
	}
	view, err := h.accountDeletion.ClientView(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", view)
}

// AppAccountDeletionCode 向绑定的邮箱或手机号发注销验证码。
func (h *Handler) AppAccountDeletionCode(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.deletionReady(c) {
		return
	}
	var req AccountDeletionCodeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.accountDeletion.SendCode(c.Request.Context(), session, req.Channel, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "验证码已发送", result)
}

// AppSubmitAccountDeletion 提交注销。立即注销且无需审核时，返回里 deleted 为真，账号已经没了。
func (h *Handler) AppSubmitAccountDeletion(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.deletionReady(c) {
		return
	}
	var req AccountDeletionSubmitRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, device := h.resolveGatewayDevice(c, req.DeviceID, req.Device)
	result, err := h.accountDeletion.Submit(c.Request.Context(), service.AccountDeletionSubmitInput{
		Session: session, Mode: req.Mode, ReasonCode: req.ReasonCode, ReasonText: req.ReasonText, Agree: req.Agree,
		Password: req.Password, Channel: req.Channel, Code: req.Code, TOTPCode: req.TOTPCode, RecoveryCode: req.RecoveryCode,
		IP: c.ClientIP(), DeviceID: deviceID, Device: device, UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "注销申请已提交"
	switch {
	case result.Deleted:
		message = "账号已注销"
	case result.Request != nil && result.Request.Status == accountdeletion.StatusScheduled:
		message = "已进入注销冷静期"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AppCancelAccountDeletion 撤销进行中的注销申请。
func (h *Handler) AppCancelAccountDeletion(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.deletionReady(c) {
		return
	}
	result, err := h.accountDeletion.CancelByUser(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "注销申请已撤销", result)
}
