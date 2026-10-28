package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/itemgrant"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 物品发放（docs/item-grant.md）：目录、规则（画布）、校验与预演、执行、发放记录。

// ItemGrantRuleRequest 新建 / 保存规则。graph 是控制台画布原样（nodes / edges / viewport）。
type ItemGrantRuleRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	StartsAt    *time.Time      `json:"startsAt"`
	EndsAt      *time.Time      `json:"endsAt"`
	Graph       itemgrant.Graph `json:"graph"`
}

// ItemGrantStatusRequest 改规则状态：draft / active / paused / archived。
type ItemGrantStatusRequest struct {
	Status string `json:"status"`
}

// ItemGrantGraphRequest 校验画布。
type ItemGrantGraphRequest struct {
	Graph itemgrant.Graph `json:"graph"`
}

// ItemGrantPreviewRequest 预演：user（用户 ID 或账号）不为空时追踪这一个人，否则抽样 samples 人估算。
type ItemGrantPreviewRequest struct {
	RuleID    int64           `json:"ruleId"`
	Graph     itemgrant.Graph `json:"graph"`
	User      string          `json:"user"`
	EventData map[string]any  `json:"eventData"`
	Samples   int             `json:"samples"`
}

// ItemGrantFulfillRequest 标记人工发放的物品已处理。
type ItemGrantFulfillRequest struct {
	Note string `json:"note"`
}

// AdminItemGrantCatalog 物品、节点、触发方式、事件、条件字段、模板、套餐与功能目录。
func (h *Handler) AdminItemGrantCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.itemGrant.Catalog(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminItemGrantOverview 发放总览：规则数、发放量、近 days 天趋势、近 30 天按物品汇总。
func (h *Handler) AdminItemGrantOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.itemGrant.Overview(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 14))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminListItemGrantRules 全部规则（附发放统计与正在进行的执行）。
func (h *Handler) AdminListItemGrantRules(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.itemGrant.ListRules(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminGetItemGrantRule 一条规则（含画布）。
func (h *Handler) AdminGetItemGrantRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	result, err := h.itemGrant.GetRule(c.Request.Context(), appID, ruleID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminCreateItemGrantRule 新建规则（草稿；画布为空时给一张初始画布）。
func (h *Handler) AdminCreateItemGrantRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req ItemGrantRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.itemGrant.CreateRule(c.Request.Context(), itemgrant.RuleInput{
		AppID: appID, Name: req.Name, Description: req.Description, StartsAt: req.StartsAt, EndsAt: req.EndsAt,
		Graph: req.Graph, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "已创建", result)
}

// AdminUpdateItemGrantRule 保存规则（生效中的规则画布不能有错误）。
func (h *Handler) AdminUpdateItemGrantRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	var req ItemGrantRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.itemGrant.UpdateRule(c.Request.Context(), itemgrant.RuleInput{
		AppID: appID, RuleID: ruleID, Name: req.Name, Description: req.Description, StartsAt: req.StartsAt,
		EndsAt: req.EndsAt, Graph: req.Graph, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", result)
}

// AdminDeleteItemGrantRule 删除规则（有发放记录的只能归档）。
func (h *Handler) AdminDeleteItemGrantRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	if err := h.itemGrant.DeleteRule(c.Request.Context(), appID, ruleID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", nil)
}

// AdminSetItemGrantRuleStatus 上线 / 暂停 / 归档 / 回到草稿。
func (h *Handler) AdminSetItemGrantRuleStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	var req ItemGrantStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.itemGrant.SetStatus(c.Request.Context(), appID, ruleID, strings.TrimSpace(req.Status), operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已更新", result)
}

// AdminRunItemGrantRule 立即执行一次（后台执行，返回这次执行；用执行详情接口看进度）。
func (h *Handler) AdminRunItemGrantRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	if operator == "" {
		operator = "admin"
	}
	result, err := h.itemGrant.RunRule(c.Request.Context(), appID, ruleID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusAccepted, "已开始执行", result)
}

// AdminValidateItemGrant 校验画布：结构、配置、表达式、引用的套餐与功能（不保存）。
func (h *Handler) AdminValidateItemGrant(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req ItemGrantGraphRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	issues := h.itemGrant.Validate(c.Request.Context(), appID, req.Graph)
	response.Success(c, http.StatusOK, "ok", gin.H{"issues": issues, "valid": !itemgrant.HasErrors(issues)})
}

// AdminPreviewItemGrant 预演（只判定、不发放）：单人追踪或抽样估算。
func (h *Handler) AdminPreviewItemGrant(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req ItemGrantPreviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.itemGrant.Preview(c.Request.Context(), service.ItemGrantPreviewInput{
		AppID: appID, RuleID: req.RuleID, Graph: req.Graph, User: req.User, EventData: req.EventData, Samples: req.Samples,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminListItemGrantRuns 执行记录（ruleId 可选），新的在前。
func (h *Handler) AdminListItemGrantRuns(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, _ := strconv.ParseInt(c.Query("ruleId"), 10, 64)
	result, err := h.itemGrant.ListRuns(c.Request.Context(), itemgrant.RunQuery{
		AppID: appID, RuleID: ruleID,
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminGetItemGrantRun 一次执行（含进度与执行时的画布快照）。
func (h *Handler) AdminGetItemGrantRun(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	runID, ok := cardKeyPathID(c, "runId")
	if !ok {
		return
	}
	result, err := h.itemGrant.GetRun(c.Request.Context(), appID, runID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminCancelItemGrantRun 取消执行（当前这批处理完就停，已发的不收回）。
func (h *Handler) AdminCancelItemGrantRun(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	runID, ok := cardKeyPathID(c, "runId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	result, err := h.itemGrant.CancelRun(c.Request.Context(), appID, runID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已取消", result)
}

// AdminListItemGrantRecords 发放记录（ruleId / runId / userId / status / keyword / start / end 可选）。
func (h *Handler) AdminListItemGrantRecords(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, _ := strconv.ParseInt(c.Query("ruleId"), 10, 64)
	runID, _ := strconv.ParseInt(c.Query("runId"), 10, 64)
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	result, err := h.itemGrant.ListRecords(c.Request.Context(), itemgrant.RecordQuery{
		AppID: appID, RuleID: ruleID, RunID: runID, UserID: userID, Status: c.Query("status"), Keyword: c.Query("keyword"),
		Start: cardKeyQueryTime(c.Query("start")), End: cardKeyQueryTime(c.Query("end")),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminGetItemGrantRecord 一条发放记录（判定路径、计划发放与实际结果）。
func (h *Handler) AdminGetItemGrantRecord(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	recordID, ok := cardKeyPathID(c, "recordId")
	if !ok {
		return
	}
	result, err := h.itemGrant.GetRecord(c.Request.Context(), appID, recordID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminFulfillItemGrantRecord 人工发放的物品已处理：标记已发放并通知用户。
func (h *Handler) AdminFulfillItemGrantRecord(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	recordID, ok := cardKeyPathID(c, "recordId")
	if !ok {
		return
	}
	var req ItemGrantFulfillRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.itemGrant.FulfillRecord(c.Request.Context(), itemgrant.HandleInput{
		AppID: appID, RecordID: recordID, Note: req.Note, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已标记发放", result)
}

// AdminRetryItemGrantRecord 重试一条失败的记录（按规则当前的画布重新判定、发放）。
func (h *Handler) AdminRetryItemGrantRecord(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	recordID, ok := cardKeyPathID(c, "recordId")
	if !ok {
		return
	}
	result, err := h.itemGrant.RetryRecord(c.Request.Context(), appID, recordID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已重试", result)
}
