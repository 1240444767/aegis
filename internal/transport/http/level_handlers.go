package httptransport

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/levels"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 等级体系（docs/levels.md）：目录、画布（草稿 / 发布 / 版本）、推演、用户等级、升级记录、达成奖励、重算任务；
// 用户端的达成奖励与升级记录。

// LevelDraftRequest 保存草稿。revision 是编辑开始时的草稿修订号（还没有草稿时为 0）。
type LevelDraftRequest struct {
	Graph    levels.Graph `json:"graph"`
	Revision int          `json:"revision"`
}

// LevelGraphRequest 校验画布。
type LevelGraphRequest struct {
	Graph levels.Graph `json:"graph"`
}

// LevelPublishRequest 发布。recalc：发布后让全体用户按新体系判定一次；downgrade：重算时允许降级。
type LevelPublishRequest struct {
	Graph     levels.Graph `json:"graph"`
	Note      string       `json:"note"`
	Recalc    bool         `json:"recalc"`
	Downgrade bool         `json:"downgrade"`
}

// LevelSimulateRequest 推演：graph 为空时用已发布的体系；user（用户 ID 或账号）不为空时按这个人的经验与行为判定。
type LevelSimulateRequest struct {
	Graph      *levels.Graph `json:"graph"`
	Experience int64         `json:"experience"`
	User       string        `json:"user"`
}

// LevelAdjustRequest 管理员调级：level 为 0 时只按当前体系补判；topUp 同时把经验补到目标级门槛。
type LevelAdjustRequest struct {
	Level int    `json:"level"`
	TopUp bool   `json:"topUp"`
	Note  string `json:"note"`
}

// LevelFulfillRequest 人工物品已处理。
type LevelFulfillRequest struct {
	Note string `json:"note"`
}

// LevelBackfillRequest 补发一份达成奖励。
type LevelBackfillRequest struct {
	NodeID string `json:"nodeId" binding:"required"`
}

// LevelRecalcRequest 起一个重算任务。
type LevelRecalcRequest struct {
	Downgrade bool `json:"downgrade"`
}

// AdminLevelCatalog 节点、权益、图标、物品、条件字段、模板。
func (h *Handler) AdminLevelCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.level.Catalog(c.Request.Context(), appID)
	h.levelRespond(c, result, err)
}

// AdminLevelOverview 等级分布、升级趋势、奖励状态。
func (h *Handler) AdminLevelOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.level.Overview(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 14))
	h.levelRespond(c, result, err)
}

// AdminGetLevelScheme 体系：草稿、发布信息、校验结果与逐级展开。
func (h *Handler) AdminGetLevelScheme(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.level.GetScheme(c.Request.Context(), appID)
	h.levelRespond(c, result, err)
}

// AdminSaveLevelScheme 保存草稿。
func (h *Handler) AdminSaveLevelScheme(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LevelDraftRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.level.SaveDraft(c.Request.Context(), appID, req.Graph, req.Revision, operator)
	h.levelRespond(c, result, err)
}

// AdminValidateLevelScheme 校验画布（不保存）。
func (h *Handler) AdminValidateLevelScheme(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LevelGraphRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, http.StatusOK, "ok", h.level.Validate(c.Request.Context(), appID, req.Graph))
}

// AdminPublishLevelScheme 发布。
func (h *Handler) AdminPublishLevelScheme(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LevelPublishRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.level.Publish(c.Request.Context(), service.LevelPublishInput{
		AppID: appID, Graph: req.Graph, Note: req.Note, Operator: operator, Recalc: req.Recalc, Downgrade: req.Downgrade,
	})
	h.levelRespond(c, result, err)
}

// AdminSimulateLevel 推演：按经验或按某个人。
func (h *Handler) AdminSimulateLevel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LevelSimulateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.level.Simulate(c.Request.Context(), service.LevelSimulateInput{
		AppID: appID, Graph: req.Graph, Experience: req.Experience, User: req.User,
	})
	h.levelRespond(c, result, err)
}

// AdminListLevelVersions 发布历史。
func (h *Handler) AdminListLevelVersions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.level.ListVersions(c.Request.Context(), appID)
	h.levelRespond(c, result, err)
}

// AdminGetLevelVersion 一次发布（带画布）。
func (h *Handler) AdminGetLevelVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	version, ok := levelPathInt(c, "version")
	if !ok {
		return
	}
	result, err := h.level.GetVersion(c.Request.Context(), appID, int(version))
	h.levelRespond(c, result, err)
}

// AdminRestoreLevelVersion 把某次发布取回草稿。
func (h *Handler) AdminRestoreLevelVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	version, ok := levelPathInt(c, "version")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	result, err := h.level.RestoreVersion(c.Request.Context(), appID, int(version), operator)
	h.levelRespond(c, result, err)
}

// AdminListLevelUsers 用户等级列表。
func (h *Handler) AdminListLevelUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.level.ListUsers(c.Request.Context(), levels.UserQuery{
		AppID: appID, Level: parsePositiveInt(c.Query("level"), 0), MinLevel: parsePositiveInt(c.Query("minLevel"), 0),
		MaxLevel: parsePositiveInt(c.Query("maxLevel"), 0), Keyword: c.Query("keyword"), Sort: c.Query("sort"),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	h.levelRespond(c, result, err)
}

// AdminGetLevelUser 一个人的等级详情。
func (h *Handler) AdminGetLevelUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	result, err := h.level.GetUser(c.Request.Context(), appID, userID)
	h.levelRespond(c, result, err)
}

// AdminAdjustLevelUser 管理员调级（或只按当前体系补判）。
func (h *Handler) AdminAdjustLevelUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req LevelAdjustRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, operator := adminAccount(c)
	result, err := h.level.AdjustUser(c.Request.Context(), service.LevelAdjustInput{
		AppID: appID, UserID: userID, Level: req.Level, TopUp: req.TopUp, Note: req.Note, Operator: operator, AdminID: adminID,
	})
	h.levelRespond(c, result, err)
}

// AdminListLevelEvents 升级记录。
func (h *Handler) AdminListLevelEvents(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	result, err := h.level.ListEvents(c.Request.Context(), levels.EventQuery{
		AppID: appID, UserID: userID, Reason: c.Query("reason"), Level: parsePositiveInt(c.Query("level"), 0),
		From: cardKeyQueryTime(c.Query("start")), To: cardKeyQueryTime(c.Query("end")),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	h.levelRespond(c, result, err)
}

// AdminListLevelRewards 达成奖励记录。
func (h *Handler) AdminListLevelRewards(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	result, err := h.level.ListRewards(c.Request.Context(), levels.RewardQuery{
		AppID: appID, UserID: userID, Status: c.Query("status"), Level: parsePositiveInt(c.Query("level"), 0),
		NodeID: c.Query("nodeId"), Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	h.levelRespond(c, result, err)
}

// AdminGetLevelReward 一条达成奖励记录。
func (h *Handler) AdminGetLevelReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	rewardID, ok := cardKeyPathID(c, "rewardId")
	if !ok {
		return
	}
	result, err := h.level.GetReward(c.Request.Context(), appID, rewardID)
	h.levelRespond(c, result, err)
}

// AdminRetryLevelReward 重试失败的奖励。
func (h *Handler) AdminRetryLevelReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	rewardID, ok := cardKeyPathID(c, "rewardId")
	if !ok {
		return
	}
	result, err := h.level.RetryReward(c.Request.Context(), appID, rewardID)
	h.levelRespond(c, result, err)
}

// AdminFulfillLevelReward 人工物品已处理。
func (h *Handler) AdminFulfillLevelReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	rewardID, ok := cardKeyPathID(c, "rewardId")
	if !ok {
		return
	}
	var req LevelFulfillRequest
	_ = bind(c, &req)
	_, operator := adminAccount(c)
	result, err := h.level.FulfillReward(c.Request.Context(), appID, rewardID, operator, req.Note)
	h.levelRespond(c, result, err)
}

// AdminBackfillLevelReward 补发：给已达到该等级、还没有这份奖励的人各写一条。
func (h *Handler) AdminBackfillLevelReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LevelBackfillRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	count, err := h.level.Backfill(c.Request.Context(), appID, req.NodeID)
	h.levelRespond(c, gin.H{"created": count}, err)
}

// AdminListLevelJobs 最近的重算任务。
func (h *Handler) AdminListLevelJobs(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.level.ListJobs(c.Request.Context(), appID)
	h.levelRespond(c, result, err)
}

// AdminStartLevelJob 起一个重算任务（202，后台执行）。
func (h *Handler) AdminStartLevelJob(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LevelRecalcRequest
	_ = bind(c, &req)
	_, operator := adminAccount(c)
	result, err := h.level.StartRecalc(c.Request.Context(), appID, req.Downgrade, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusAccepted, "ok", result)
}

// AdminGetLevelJob 一个任务的进度。
func (h *Handler) AdminGetLevelJob(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	jobID, ok := cardKeyPathID(c, "jobId")
	if !ok {
		return
	}
	result, err := h.level.GetJob(c.Request.Context(), appID, jobID)
	h.levelRespond(c, result, err)
}

// AdminCancelLevelJob 取消任务。
func (h *Handler) AdminCancelLevelJob(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	jobID, ok := cardKeyPathID(c, "jobId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	result, err := h.level.CancelJob(c.Request.Context(), appID, jobID, operator)
	h.levelRespond(c, result, err)
}

// ── 用户端 ──

// MyLevelRewards 我的达成奖励；status 可选 claimable / granted / queued / pending / failed。
func (h *Handler) MyLevelRewards(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	result, err := h.level.MyRewards(c.Request.Context(), session, c.Query("status"))
	h.levelRespond(c, result, err)
}

// ClaimLevelReward 领取一份待领取的达成奖励。
func (h *Handler) ClaimLevelReward(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	rewardID, ok := cardKeyPathID(c, "rewardId")
	if !ok {
		return
	}
	result, err := h.level.ClaimReward(c.Request.Context(), session, rewardID)
	h.levelRespond(c, result, err)
}

// MyLevelHistory 我的升级记录。
func (h *Handler) MyLevelHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	result, err := h.level.MyHistory(c.Request.Context(), session, parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	h.levelRespond(c, result, err)
}

func (h *Handler) levelRespond(c *gin.Context, data any, err error) {
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", data)
}

func levelPathInt(c *gin.Context, name string) (int64, bool) {
	return cardKeyPathID(c, name)
}
