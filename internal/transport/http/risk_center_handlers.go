package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/response"
	"aegis/pkg/timeutil"

	"github.com/gin-gonic/gin"
)

// ════════════════════════════════════════════════════════════
//  风控中心：名单库 / 账号画像 / 人工处置 / 规则模板
// ════════════════════════════════════════════════════════════

// AdminListRiskLists 名单列表
// GET /api/admin/system/risk/lists
func (h *Handler) AdminListRiskLists(c *gin.Context) {
	var req RiskListQueryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	query := securitydomain.ListQuery{
		AppID: req.AppID, ListType: strings.TrimSpace(req.ListType), Kind: strings.TrimSpace(req.Kind),
		Keyword: strings.TrimSpace(req.Keyword), Source: strings.TrimSpace(req.Source), Page: req.Page, PageSize: req.PageSize,
	}
	if active, ok := parseOptionalBool(req.Active); ok {
		query.Active = &active
	}
	items, total, err := h.risk.ListListEntries(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminCreateRiskLists 批量写入名单
// POST /api/admin/system/risk/lists
func (h *Handler) AdminCreateRiskLists(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, riskForbiddenMsg)
		return
	}
	var req RiskListCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	expiresAt, err := parseOptionalRFC3339(req.ExpiresAt)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "到期时间格式错误")
		return
	}
	adminID := session.AdminID
	result, err := h.risk.SaveListEntries(c.Request.Context(), securitydomain.ListEntryInput{
		AppID: positiveID(req.AppID), ListType: req.ListType, Values: req.Values, Kind: req.Kind, Scenes: req.Scenes,
		Reason: strings.TrimSpace(req.Reason), ExpiresAt: expiresAt, Source: "manual", CreatedBy: &adminID,
	})
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "create", "risk_list", req.ListType,
		fmt.Sprintf("名单写入 %s/%s：新增 %d，改写 %d", req.ListType, req.Kind, result.Created, result.Updated))
	response.Success(c, 200, "保存成功", result)
}

// AdminUpdateRiskList 修改名单
// PUT /api/admin/system/risk/lists/:id
func (h *Handler) AdminUpdateRiskList(c *gin.Context) {
	if !requireRiskAdmin(c) {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的名单 ID")
		return
	}
	var req RiskListUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := securitydomain.ListEntryUpdate{Kind: req.Kind, Scenes: req.Scenes, Reason: req.Reason}
	if req.ExpiresAt != nil {
		expiresAt, err := parseOptionalRFC3339(*req.ExpiresAt)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "到期时间格式错误")
			return
		}
		input.ExpiresAt = &expiresAt
	}
	entry, err := h.risk.UpdateListEntry(c.Request.Context(), id, input)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "update", "risk_list", strconv.FormatInt(id, 10), fmt.Sprintf("修改名单 %s %s", entry.ListType, entry.Value))
	response.Success(c, 200, "更新成功", entry)
}

// AdminDeleteRiskList 删除名单
// DELETE /api/admin/system/risk/lists/:id
func (h *Handler) AdminDeleteRiskList(c *gin.Context) {
	if !requireRiskAdmin(c) {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的名单 ID")
		return
	}
	removed, err := h.risk.DeleteListEntries(c.Request.Context(), []int64{id})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "risk_list", strconv.FormatInt(id, 10), "删除名单")
	response.Success(c, 200, "删除成功", gin.H{"removed": removed})
}

// AdminBatchDeleteRiskLists 批量删除名单
// POST /api/admin/system/risk/lists/batch-delete
func (h *Handler) AdminBatchDeleteRiskLists(c *gin.Context) {
	if !requireRiskAdmin(c) {
		return
	}
	var req RiskListBatchDeleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if len(req.IDs) > 1000 {
		response.Error(c, http.StatusBadRequest, 40000, "单次最多删除 1000 条")
		return
	}
	removed, err := h.risk.DeleteListEntries(c.Request.Context(), req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "risk_list", "", fmt.Sprintf("批量删除名单 %d 条", removed))
	response.Success(c, 200, "删除成功", gin.H{"removed": removed})
}

// AdminCheckRiskLists 名单检测
// POST /api/admin/system/risk/lists/check
func (h *Handler) AdminCheckRiskLists(c *gin.Context) {
	var req RiskListCheckRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.risk.CheckLists(c.Request.Context(), positiveID(req.AppID), strings.TrimSpace(req.Scene),
		req.IP, req.DeviceID, req.Account, positiveID(req.UserID), req.UserAgent)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "检测完成", result)
}

// AdminListRiskAccounts 账号画像列表
// GET /api/admin/system/risk/accounts
func (h *Handler) AdminListRiskAccounts(c *gin.Context) {
	var req RiskAccountListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	query := securitydomain.AccountQuery{
		AppID: positiveID(req.AppID), Keyword: strings.TrimSpace(req.Keyword), Page: req.Page, PageSize: req.PageSize,
	}
	if onlyRisk, ok := parseOptionalBool(req.OnlyRisk); ok {
		query.OnlyRisk = onlyRisk
	}
	days := req.Days
	if days <= 0 {
		days = 30
	}
	start := timeutil.NowUTC().Add(-time.Duration(min(days, 365)) * 24 * time.Hour)
	query.Start = &start
	items, total, err := h.risk.ListAccountProfiles(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminGetRiskAccount 账号详情
// GET /api/admin/system/risk/accounts/detail
func (h *Handler) AdminGetRiskAccount(c *gin.Context) {
	var req RiskAccountDetailRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	detail, err := h.risk.GetAccountDetail(c.Request.Context(), positiveID(req.AppID), req.Account, positiveID(req.UserID))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, 200, "获取成功", detail)
}

// AdminDisposeRisk 人工处置
// POST /api/admin/system/risk/dispose
func (h *Handler) AdminDisposeRisk(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, riskForbiddenMsg)
		return
	}
	var req RiskDisposeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	records, err := h.risk.Dispose(c.Request.Context(), securitydomain.DisposeInput{
		AssessmentID: positiveID(req.AssessmentID), AppID: positiveID(req.AppID), UserID: positiveID(req.UserID),
		Account: req.Account, IP: req.IP, DeviceID: req.DeviceID, Targets: req.Targets, Kind: req.Kind,
		Duration: req.Duration, Reason: req.Reason, OperatorID: session.AdminID, OperatorName: adminDisplayName(session),
	})
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	ok2 := 0
	for _, record := range records {
		if record.OK {
			ok2++
		}
	}
	h.recordAudit(c, "dispose", "risk", strings.Join(req.Targets, ","),
		fmt.Sprintf("风控处置（%s）：成功 %d / %d", req.Kind, ok2, len(records)))
	response.Success(c, 200, "处置完成", gin.H{"enforcement": records})
}

// AdminListRiskTemplates 规则模板
// GET /api/admin/system/risk/templates
func (h *Handler) AdminListRiskTemplates(c *gin.Context) {
	var appID *int64
	if raw := strings.TrimSpace(c.Query("appId")); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			appID = &id
		}
	}
	items, err := h.risk.ListTemplates(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminApplyRiskTemplate 安装规则模板
// POST /api/admin/system/risk/templates/apply
func (h *Handler) AdminApplyRiskTemplate(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, riskForbiddenMsg)
		return
	}
	var req RiskTemplateApplyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.risk.ApplyTemplate(c.Request.Context(), securitydomain.TemplateApplyInput{
		Key: req.Key, AppID: positiveID(req.AppID), Observe: req.Observe, WithActions: req.WithActions,
	}, session.AdminID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "create", "risk_template", req.Key,
		fmt.Sprintf("安装规则模板 %s：规则 %d 条，策略 %d 条", req.Key, result.RulesCreated, result.ActionsCreated))
	response.Success(c, 200, "安装完成", result)
}

// parseOptionalRFC3339 空串为 nil（永久）
func parseOptionalRFC3339(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &at, nil
}
