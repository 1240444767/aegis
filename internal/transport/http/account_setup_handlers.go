package httptransport

import (
	"net/http"

	appdomain "aegis/internal/domain/app"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 账号初始化（docs/account-setup.md）：快速登录建的号补设用户名与密码。

// AccountSetupRequest 提交初始化。两项都可以单独提交，空串表示这次不设。
type AccountSetupRequest struct {
	// Account 新用户名。只能自助设一次；传与当前相同的名字表示沿用（须符合用户名规则）
	Account string `json:"account"`
	// Password 新密码。只在还没有密码时可设，已有密码的走 /auth/password/change
	Password string `json:"password"`
}

// AccountSetupCheckRequest 检查用户名能不能用。
type AccountSetupCheckRequest struct {
	Account string `json:"account" binding:"required"`
}

// AdminAccountSetupPolicyRequest 管理端保存账号初始化策略。
type AdminAccountSetupPolicyRequest struct {
	Policy appdomain.AccountSetupPolicy `json:"policy" binding:"required"`
}

// AdminRenameUserRequest 管理员改用户名。
type AdminRenameUserRequest struct {
	Account string `json:"account" binding:"required"`
}

// AdminResetAccountSetupRequest 要求用户重新初始化。
type AdminResetAccountSetupRequest struct {
	// ResetAccount 用户名也重新算作待设置（用户可以再自助改一次）
	ResetAccount bool `json:"resetAccount"`
}

// AppAccountSetupStatus GET /me/account-setup：还缺什么、现在该弹窗还是出横幅。
func (h *Handler) AppAccountSetupStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	status, err := h.auth.AccountSetupStatus(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", status)
}

// AppAccountSetupPrompted POST /me/account-setup/prompted：首次弹窗已展示，之后只出横幅。
func (h *Handler) AppAccountSetupPrompted(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	status, err := h.auth.MarkAccountSetupPrompted(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已记录", status)
}

// AppAccountSetupDismiss POST /me/account-setup/dismiss：用户关掉横幅，隔 snoozeHours 再出现。
func (h *Handler) AppAccountSetupDismiss(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	status, err := h.auth.DismissAccountSetup(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已关闭", status)
}

// AppAccountSetupCheck POST /me/account-setup/check：用户名能不能用。
func (h *Handler) AppAccountSetupCheck(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req AccountSetupCheckRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.CheckUsername(c.Request.Context(), session, req.Account)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "检查完成", result)
}

// AppCompleteAccountSetup POST /me/account-setup：设用户名与（或）密码。
func (h *Handler) AppCompleteAccountSetup(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req AccountSetupRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.CompleteAccountSetup(c.Request.Context(), session,
		service.AccountSetupInput{Account: req.Account, Password: req.Password})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "账号设置已保存", result)
}

// AdminAppAccountSetup GET /api/admin/apps/:appkey/account-setup：策略与统计。
func (h *Handler) AdminAppAccountSetup(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	view, err := h.auth.AccountSetupPolicyView(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", view)
}

// UpdateAdminAppAccountSetup PUT /api/admin/apps/:appkey/account-setup：保存策略。
func (h *Handler) UpdateAdminAppAccountSetup(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAccountSetupPolicyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	view, err := h.auth.SaveAccountSetupPolicy(c.Request.Context(), appID, req.Policy)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "账号初始化策略已保存", view)
}

// AdminAppUserAccountSetup GET /api/admin/apps/:appkey/users/:userId/account-setup：某个用户的初始化情况。
func (h *Handler) AdminAppUserAccountSetup(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	view, err := h.auth.AdminAccountSetup(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", view)
}

// AdminResetAppUserAccountSetup POST /api/admin/apps/:appkey/users/:userId/account-setup/reset：要求重新初始化。
func (h *Handler) AdminResetAppUserAccountSetup(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminResetAccountSetupRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	view, err := h.auth.AdminResetAccountSetup(c.Request.Context(), appID, userID, req.ResetAccount)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已要求用户重新设置", view)
}

// AdminRenameAppUser PUT /api/admin/apps/:appkey/users/:userId/account：改用户名。
func (h *Handler) AdminRenameAppUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminRenameUserRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	ctx := c.Request.Context()
	user, err := h.auth.AdminRenameUser(ctx, appID, userID, req.Account)
	if err != nil {
		h.writeError(c, err)
		return
	}
	setup, err := h.auth.AdminAccountSetup(ctx, appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户名已修改", gin.H{"account": user.Account, "accountSetup": setup})
}
