package httptransport

import (
	"net/http"

	captchadomain "aegis/internal/domain/captcha"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 找回密码：/auth/password/recovery/* 与已登录入口 /me/password/recovery。
//
// 流程与安全规则全在 service/auth_password_recovery.go；这里只做三件事：
// 发起前过图形验证码（未登录入口）、把设备与 IP 交给服务层、把位置交给告知信。
// 设备取请求体 deviceId，缺省取 X-Device-Id 头 —— 流程绑定在发起它的设备上。

// AppPasswordRecoveryStart 未登录发起找回。
func (h *Handler) AppPasswordRecoveryStart(c *gin.Context) {
	app, policy, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AppPasswordRecoveryStartRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if !h.verifyGatewayCaptcha(c, policy, app.ID, req.CaptchaID, req.CaptchaAnswer, captchadomain.PurposeResetPassword) {
		return
	}
	deviceID, _ := resolveClientDevice(c, req.DeviceID, "", "")
	result, err := h.auth.StartPasswordRecovery(c.Request.Context(), service.PasswordRecoveryStartInput{
		AppID: app.ID, Identifier: req.Account,
		DeviceID: deviceID, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "请完成身份验证", result)
}

// AppMyPasswordRecoveryStart 已登录发起找回（忘了原密码，不退出登录）。
func (h *Handler) AppMyPasswordRecoveryStart(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req AppPasswordRecoveryDeviceRequest
	_ = bind(c, &req)
	deviceID, _ := resolveClientDevice(c, req.DeviceID, "", "")
	result, err := h.auth.StartSessionPasswordRecovery(c.Request.Context(), session, deviceID, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "请完成身份验证", result)
}

// AppPasswordRecoveryStatus 读流程当前状态。
func (h *Handler) AppPasswordRecoveryStatus(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AppPasswordRecoveryFlowRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.PasswordRecoveryStatus(c.Request.Context(), recoveryStep(c, app.ID, req.FlowToken, req.DeviceID))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AppPasswordRecoverySend 向一项联系方式发验证码。
func (h *Handler) AppPasswordRecoverySend(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AppPasswordRecoverySendRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.SendPasswordRecoveryCode(c.Request.Context(),
		recoveryStep(c, app.ID, req.FlowToken, req.DeviceID), req.Channel, req.Target)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "验证码已发送", result)
}

// AppPasswordRecoveryVerify 核验一项。
func (h *Handler) AppPasswordRecoveryVerify(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AppPasswordRecoveryVerifyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.VerifyPasswordRecoveryFactor(c.Request.Context(),
		recoveryStep(c, app.ID, req.FlowToken, req.DeviceID), req.Factor, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "验证通过"
	if result.Ready {
		message = "验证完成，请设置新密码"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AppPasswordRecoveryComplete 设新密码。
func (h *Handler) AppPasswordRecoveryComplete(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AppPasswordRecoveryCompleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, device := h.resolveGatewayDevice(c, req.DeviceID, req.Device)
	ip := c.ClientIP()
	result, err := h.auth.CompletePasswordRecovery(c.Request.Context(), service.PasswordRecoveryCompleteInput{
		PasswordRecoveryStepInput: recoveryStep(c, app.ID, req.FlowToken, req.DeviceID),
		NewPassword:               req.NewPassword,
		Device:                    device,
		UserAgent:                 c.Request.UserAgent(),
		Location:                  h.qrLoginLocation(c, ip),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "密码已重置", result)
}

// recoveryStep 拼出流程定位：设备取请求体，缺省取 X-Device-Id 头。
func recoveryStep(c *gin.Context, appID int64, flowToken, bodyDeviceID string) service.PasswordRecoveryStepInput {
	deviceID, _ := resolveClientDevice(c, bodyDeviceID, "", "")
	return service.PasswordRecoveryStepInput{
		AppID: appID, FlowToken: flowToken, DeviceID: deviceID, IP: c.ClientIP(),
	}
}
