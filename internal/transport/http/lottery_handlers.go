package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 抽奖：管理端（目录 / 活动与奖品 / 记录与人工发放 / 统计）与用户端（活动列表 / 活动页 / 抽奖 / 我的记录）。
// 规则见 docs/lottery.md。

// ── DTO ──

// LotteryRewardRequest 奖品发的东西。取值字段由奖品目录的 value 决定用哪个：
// amount（积分、经验、会员天数、抽奖次数；功能解锁时是天数，0 为永久）、refId（会员套餐）、
// tag（功能标识）、text（人工发放的内容）。「谢谢参与」只有 type=none。
type LotteryRewardRequest struct {
	Type   string `json:"type" binding:"required"`
	Amount int64  `json:"amount"`
	RefID  int64  `json:"refId"`
	Tag    string `json:"tag"`
	Text   string `json:"text"`
}

// LotteryPrizeRequest 一个奖品。id 为 0 表示新建；带 id 表示更新（保留已抽中数量）。
type LotteryPrizeRequest struct {
	ID       int64                `json:"id"`
	Name     string               `json:"name"`
	Reward   LotteryRewardRequest `json:"reward"`
	ImageURL string               `json:"imageUrl"`
	// Quantity 总库存，-1 为不限
	Quantity int64 `json:"quantity"`
	Weight   int   `json:"weight"`
	Position int   `json:"position"`
	Fallback bool  `json:"fallback"`
}

// LotteryActivityRequest 新建 / 整体更新一个活动（连同全部奖品）。
type LotteryActivityRequest struct {
	Name        string     `json:"name" binding:"required"`
	Description string     `json:"description"`
	Rules       string     `json:"rules"`
	UIMode      string     `json:"uiMode"`
	Status      string     `json:"status"`
	StartTime   *time.Time `json:"startTime"`
	EndTime     *time.Time `json:"endTime"`
	FreeDaily   int        `json:"freeDaily"`
	PointsCost  int64      `json:"pointsCost"`
	PointsDaily int        `json:"pointsDaily"`
	VideoScene  string     `json:"videoScene"`
	// PityThreshold 连续这么多次未中时必中保底奖，0 为关闭；保底奖按位置指定（PityPosition）
	PityThreshold int                   `json:"pityThreshold"`
	PityPosition  *int                  `json:"pityPosition"`
	SortOrder     int                   `json:"sortOrder"`
	Prizes        []LotteryPrizeRequest `json:"prizes"`
}

// LotteryStatusRequest 改活动状态：draft / active / paused / ended。
type LotteryStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// LotteryFulfillRequest 标记人工发放的奖品已发放；note 会写进给用户的站内信（兑换码、快递单号等）。
type LotteryFulfillRequest struct {
	Note string `json:"note"`
}

// LotteryDrawRequest 用户抽一次。source 为 free / bonus / points，留空按 free → bonus → points 自动选；
// requestId 由客户端生成（建议 UUID），网络重试时带同一个值，不会多抽一次。
type LotteryDrawRequest struct {
	Source    string `json:"source"`
	RequestID string `json:"requestId"`
	DeviceID  string `json:"deviceId"`
}

// LotteryDrawsQuery 我的抽奖记录；activityId 不传为全部活动。
type LotteryDrawsQuery struct {
	ActivityID int64 `form:"activityId"`
	Page       int   `form:"page"`
	Limit      int   `form:"limit"`
}

func (req LotteryActivityRequest) toInput(appID int64, operator string) lotterydomain.ActivityInput {
	input := lotterydomain.ActivityInput{
		AppID:         appID,
		Name:          req.Name,
		Description:   req.Description,
		Rules:         req.Rules,
		UIMode:        req.UIMode,
		Status:        req.Status,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		FreeDaily:     req.FreeDaily,
		PointsCost:    req.PointsCost,
		PointsDaily:   req.PointsDaily,
		VideoScene:    req.VideoScene,
		PityThreshold: req.PityThreshold,
		PityPosition:  -1,
		SortOrder:     req.SortOrder,
		Operator:      operator,
		Prizes:        make([]lotterydomain.PrizeInput, 0, len(req.Prizes)),
	}
	if req.PityPosition != nil {
		input.PityPosition = *req.PityPosition
	}
	for _, prize := range req.Prizes {
		input.Prizes = append(input.Prizes, lotterydomain.PrizeInput{
			ID:   prize.ID,
			Name: prize.Name,
			Reward: lotterydomain.Reward{
				Type: prize.Reward.Type, Amount: prize.Reward.Amount, RefID: prize.Reward.RefID,
				Tag: prize.Reward.Tag, Text: prize.Reward.Text,
			},
			ImageURL: prize.ImageURL,
			Quantity: prize.Quantity,
			Weight:   prize.Weight,
			Position: prize.Position,
			Fallback: prize.Fallback,
		})
	}
	return input
}

// ── 管理端 ──

// AdminLotteryCatalog 奖品目录、界面形态、状态、可选的会员套餐 / 功能 / 激励广告场景。控制台的活动表单由它驱动。
func (h *Handler) AdminLotteryCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	catalog, err := h.lottery.Catalog(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", catalog)
}

// AdminListLotteryActivities 活动列表（含奖品与汇总）。
func (h *Handler) AdminListLotteryActivities(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.lottery.ListActivities(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminGetLotteryActivity 单个活动（含奖品与汇总）。
func (h *Handler) AdminGetLotteryActivity(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	activityID, ok := cardKeyPathID(c, "activityId")
	if !ok {
		return
	}
	item, err := h.lottery.GetActivity(c.Request.Context(), appID, activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", item)
}

// AdminCreateLotteryActivity 新建活动（连同奖品）。
func (h *Handler) AdminCreateLotteryActivity(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LotteryActivityRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.lottery.CreateActivity(c.Request.Context(), req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动已创建", item)
}

// AdminUpdateLotteryActivity 整体更新活动与奖品。已有的抽奖记录是快照，不受影响。
func (h *Handler) AdminUpdateLotteryActivity(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	activityID, ok := cardKeyPathID(c, "activityId")
	if !ok {
		return
	}
	var req LotteryActivityRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.lottery.UpdateActivity(c.Request.Context(), activityID, req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动已保存", item)
}

// AdminSetLotteryActivityStatus 上线 / 暂停 / 结束。
func (h *Handler) AdminSetLotteryActivityStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	activityID, ok := cardKeyPathID(c, "activityId")
	if !ok {
		return
	}
	var req LotteryStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.lottery.SetStatus(c.Request.Context(), appID, activityID, strings.TrimSpace(req.Status))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "状态已更新", item)
}

// AdminDeleteLotteryActivity 删除没有抽奖记录的活动。
func (h *Handler) AdminDeleteLotteryActivity(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	activityID, ok := cardKeyPathID(c, "activityId")
	if !ok {
		return
	}
	if err := h.lottery.DeleteActivity(c.Request.Context(), appID, activityID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// AdminLotteryStats 一个活动的概览、按天趋势、来源与奖品分布（days，默认 14，最多 90）。
func (h *Handler) AdminLotteryStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	activityID, ok := cardKeyPathID(c, "activityId")
	if !ok {
		return
	}
	stats, err := h.lottery.Stats(c.Request.Context(), appID, activityID, parsePositiveInt(c.Query("days"), 14))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// AdminListLotteryDraws 抽奖记录（activityId / userId / status / source / winning / keyword / start / end）。
func (h *Handler) AdminListLotteryDraws(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	activityID, _ := strconv.ParseInt(c.Query("activityId"), 10, 64)
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	query := lotterydomain.DrawQuery{
		AppID:      appID,
		ActivityID: activityID,
		UserID:     userID,
		Status:     strings.TrimSpace(c.Query("status")),
		Source:     strings.TrimSpace(c.Query("source")),
		Winning:    c.Query("winning") == "true" || c.Query("winning") == "1",
		Keyword:    strings.TrimSpace(c.Query("keyword")),
		Page:       parsePositiveInt(c.Query("page"), 1),
		Limit:      parsePositiveInt(c.Query("limit"), 20),
	}
	if start, err := time.Parse(time.RFC3339, c.Query("start")); err == nil {
		query.Start = &start
	}
	if end, err := time.Parse(time.RFC3339, c.Query("end")); err == nil {
		query.End = &end
	}
	page, err := h.lottery.ListDraws(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminFulfillLotteryDraw 标记人工发放的奖品已发放（站内信通知用户）。
func (h *Handler) AdminFulfillLotteryDraw(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	drawID, ok := cardKeyPathID(c, "drawId")
	if !ok {
		return
	}
	var req LotteryFulfillRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	draw, err := h.lottery.Fulfill(c.Request.Context(), lotterydomain.HandleInput{
		AppID: appID, DrawID: drawID, Note: req.Note, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已标记为已发放", draw)
}

// ── 用户端（应用接入网关）──

// AppLotteryActivities 进行中的活动。没有活动时 activities 为空（不报错），客户端据此隐藏入口。
func (h *Handler) AppLotteryActivities(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	list, err := h.lottery.List(c.Request.Context(), session.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", list)
}

// AppLotteryState 活动页：奖品（带公示概率）、我的各种次数（含视频抽奖的广告位与签名用户标识）、保底进度。
func (h *Handler) AppLotteryState(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	activityID, ok := cardKeyPathID(c, "activityId")
	if !ok {
		return
	}
	state, err := h.lottery.State(c.Request.Context(), session.AppID, session.UserID, activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", state)
}

// AppLotteryDraw 抽一次。抽中什么由服务端决定并在同一个事务里到账，返回的是结果与抽完后的次数。
func (h *Handler) AppLotteryDraw(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	activityID, ok := cardKeyPathID(c, "activityId")
	if !ok {
		return
	}
	var req LotteryDrawRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	result, err := h.lottery.Draw(c.Request.Context(), service.LotteryDrawRequest{
		AppID:      session.AppID,
		UserID:     session.UserID,
		ActivityID: activityID,
		Source:     req.Source,
		RequestID:  req.RequestID,
		DeviceID:   deviceID,
		ClientIP:   c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "谢谢参与"
	switch {
	case result.Draw.Status == lotterydomain.DrawPending:
		message = "恭喜中奖，奖品待发放"
	case result.Draw.PrizeKind != lotterydomain.KindNone:
		message = "恭喜中奖"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AppLotteryDraws 我的抽奖记录（activityId 可选）。
func (h *Handler) AppLotteryDraws(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	activityID, _ := strconv.ParseInt(c.Query("activityId"), 10, 64)
	page, err := h.lottery.MyDraws(c.Request.Context(), session.AppID, session.UserID, activityID,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}
