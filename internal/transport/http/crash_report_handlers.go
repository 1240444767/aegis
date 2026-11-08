package httptransport

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/crashreport"
	"aegis/pkg/response"
)

// 客户端崩溃报告（docs/crash-reports.md）：用户端免登录上报，管理端按应用查看、标记、删除。

// ── DTO ──

// CrashReportRequest 一份崩溃报告。只在用户明确同意发送这一份之后上报（如崩溃屏上的「发送此报告」）。
//
// reportId 是客户端自己的记录编号（8–64 位字母数字 _ -），同一应用内重复上报返回原来那一条；
// kind 取 java / native / anr / exit；report 是完整纯文本报告，上限 256 KB；occurredAt 为 RFC 3339。
// deviceId 缺省取 X-Device-Id 头。
type CrashReportRequest struct {
	ReportID    string `json:"reportId" binding:"required"`
	Kind        string `json:"kind" binding:"required"`
	Title       string `json:"title" binding:"required"`
	Report      string `json:"report" binding:"required"`
	OccurredAt  string `json:"occurredAt" binding:"required"`
	Detail      string `json:"detail"`
	AppVersion  string `json:"appVersion"`
	VersionCode *int64 `json:"versionCode"`
	OSVersion   string `json:"osVersion"`
	Model       string `json:"model"`
	ABI         string `json:"abi"`
	ProcessName string `json:"processName"`
	Source      string `json:"source"`
	DeviceID    string `json:"deviceId"`
}

// CrashReportIDsRequest 管理端批量操作的目标（最多 200 条）。
type CrashReportIDsRequest struct {
	IDs []int64 `json:"ids" binding:"required"`
}

// CrashReportResolveRequest 标记或取消「已处理」。
type CrashReportResolveRequest struct {
	IDs      []int64 `json:"ids" binding:"required"`
	Resolved *bool   `json:"resolved" binding:"required"`
}

// ── 用户端 ──

// AppSubmitCrashReport POST /api/v1/apps/:appkey/crash-reports
//
// 免登录可用：带令牌时报告挂到账号上，令牌无效按未登录处理（OptionalAuth）。
func (h *Handler) AppSubmitCrashReport(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if h.crashReport == nil {
		response.Error(c, http.StatusServiceUnavailable, 50300, "崩溃报告服务未启用")
		return
	}
	if !limitCrashReportBody(c) {
		return
	}
	var req CrashReportRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var userID int64
	if session := gatewayReleaseSession(c); session != nil && session.AppID == app.ID {
		userID = session.UserID
	}
	deviceID, deviceName := h.resolveGatewayDevice(c, req.DeviceID, req.Model)
	extra := deviceExtraFromHeaders(c)
	result, err := h.crashReport.Submit(c.Request.Context(), crashreport.SubmitInput{
		AppID:       app.ID,
		UserID:      userID,
		ReportID:    req.ReportID,
		Kind:        req.Kind,
		Title:       req.Title,
		Detail:      req.Detail,
		Report:      req.Report,
		OccurredAt:  req.OccurredAt,
		AppVersion:  req.AppVersion,
		VersionCode: req.VersionCode,
		OSVersion:   req.OSVersion,
		Model:       req.Model,
		ABI:         req.ABI,
		ProcessName: req.ProcessName,
		Source:      req.Source,
		DeviceID:    deviceID,
		Device: crashreport.DeviceInfo{
			Platform:     resolveDevicePlatform(c),
			Name:         deviceName,
			Manufacturer: extra.Manufacturer,
			Brand:        extra.Brand,
			Codename:     extra.Codename,
			OS:           extra.OS,
			OSVersion:    extra.OSVersion,
			AppVersion:   extra.AppVersion,
		},
		UserAgent: c.Request.UserAgent(),
		// RemoteAddr 已由 middleware.ClientIP（pkg/clientip）改写为真实客户端地址
		ClientIP: c.ClientIP(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已收到崩溃报告"
	if result.Duplicate {
		message = "该崩溃报告已收到"
	}
	response.Success(c, http.StatusOK, message, result)
}

// limitCrashReportBody 在解析之前限住请求体：超过上限的请求读到上限就停，不把整个请求体读进内存。
// 读出来的字节放回请求，后面照常走 bind。
func limitCrashReportBody(c *gin.Context) bool {
	if c.Request.Body == nil || c.Request.Body == http.NoBody {
		return true
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, crashreport.MaxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Error(c, http.StatusRequestEntityTooLarge, 40000,
				"请求体超过 "+strconv.Itoa(crashreport.MaxRequestBytes>>20)+" MB 上限（报告正文上限 256 KB）")
			return false
		}
		response.Error(c, http.StatusBadRequest, 40000, "请求体读取失败")
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	return true
}

// ── 管理端 ──

// AdminCrashReports GET /api/admin/apps/:appkey/crash-reports
// 筛选：kind / appVersion / keyword（标题、详情、编号）/ resolved / userId / deviceId / start / end。不带报告正文。
func (h *Handler) AdminCrashReports(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	query := crashreport.Query{
		AppID:      appID,
		Kind:       strings.TrimSpace(c.Query("kind")),
		AppVersion: strings.TrimSpace(c.Query("appVersion")),
		Keyword:    strings.TrimSpace(c.Query("keyword")),
		UserID:     userID,
		DeviceID:   strings.TrimSpace(c.Query("deviceId")),
		Page:       parsePositiveInt(c.Query("page"), 1),
		Limit:      parsePositiveInt(c.Query("limit"), 20),
	}
	if resolved, set := parseOptionalBool(c.Query("resolved")); set {
		query.Resolved = &resolved
	}
	query.Start, query.End = parseRewardedAdRange(c.Query("start"), c.Query("end"))
	page, err := h.crashReport.List(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminCrashReportFacets GET /api/admin/apps/:appkey/crash-reports/facets
// 摘要（总数 / 未处理 / 近 24 小时）与筛选项（各类型数量、出现过的版本）。
func (h *Handler) AdminCrashReportFacets(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	facets, err := h.crashReport.Facets(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", facets)
}

// AdminCrashReport GET /api/admin/apps/:appkey/crash-reports/:crashId —— 详情，含报告正文。
func (h *Handler) AdminCrashReport(c *gin.Context) {
	appID, id, ok := h.crashReportParams(c)
	if !ok {
		return
	}
	item, err := h.crashReport.Get(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", item)
}

// AdminResolveCrashReports POST /api/admin/apps/:appkey/crash-reports/resolve —— 标记或取消「已处理」。
func (h *Handler) AdminResolveCrashReports(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CrashReportResolveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	updated, err := h.crashReport.SetResolved(c.Request.Context(), appID, req.IDs, *req.Resolved, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已标记为已处理"
	if !*req.Resolved {
		message = "已标记为未处理"
	}
	response.Success(c, http.StatusOK, message, gin.H{"updated": updated})
}

// AdminDeleteCrashReport DELETE /api/admin/apps/:appkey/crash-reports/:crashId
func (h *Handler) AdminDeleteCrashReport(c *gin.Context) {
	appID, id, ok := h.crashReportParams(c)
	if !ok {
		return
	}
	deleted, err := h.crashReport.Delete(c.Request.Context(), appID, []int64{id})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": deleted})
}

// AdminDeleteCrashReports POST /api/admin/apps/:appkey/crash-reports/delete —— 批量删除。
func (h *Handler) AdminDeleteCrashReports(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CrashReportIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deleted, err := h.crashReport.Delete(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除 "+strconv.FormatInt(deleted, 10)+" 条", gin.H{"deleted": deleted})
}

func (h *Handler) crashReportParams(c *gin.Context) (int64, int64, bool) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return 0, 0, false
	}
	id, err := strconv.ParseInt(c.Param("crashId"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "崩溃报告编号无效")
		return 0, 0, false
	}
	return appID, id, true
}
