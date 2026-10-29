package httptransport

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/response"
)

// LotteryFortuneControlRequest 下一次运气干预（也是预演的请求体）。
//
// target：allUsers 为真时对全站，否则对 userIds（每人一条）。kind：reroll 重抽 / pin 定档 / shift 偏移。
// pin 用 level（burst / lucky / steady / quiet）或 factor（运气系数，按当前差异反算）；shift 用 shift（z 的加减，±4 以内）。
// startsAt / endsAt 不传时为「从现在到明天零点」（只管今天）。
type LotteryFortuneControlRequest struct {
	UserIDs  []int64    `json:"userIds"`
	AllUsers bool       `json:"allUsers"`
	Kind     string     `json:"kind" binding:"required"`
	Level    string     `json:"level"`
	Factor   float64    `json:"factor"`
	Shift    float64    `json:"shift"`
	StartsAt *time.Time `json:"startsAt"`
	EndsAt   *time.Time `json:"endsAt"`
	Note     string     `json:"note"`
}

// LotteryFortuneRevokeRequest 撤销干预：ids 指定哪几条，all 为真时撤销这个活动全部生效中与待生效的干预。
type LotteryFortuneRevokeRequest struct {
	IDs []int64 `json:"ids"`
	All bool    `json:"all"`
}

// LotteryFortuneSettingsRequest 调整运气差异（0 关闭每日运气）。
type LotteryFortuneSettingsRequest struct {
	Spread float64 `json:"spread"`
}

func (r LotteryFortuneControlRequest) input(appID, activityID int64, operator string) lotterydomain.ControlInput {
	input := lotterydomain.ControlInput{
		AppID: appID, ActivityID: activityID, UserIDs: r.UserIDs, AllUsers: r.AllUsers, Kind: r.Kind,
		Level: r.Level, Factor: r.Factor, Shift: r.Shift, Note: r.Note, Operator: operator,
	}
	if r.StartsAt != nil {
		input.StartsAt = *r.StartsAt
	}
	if r.EndsAt != nil {
		input.EndsAt = *r.EndsAt
	}
	return input
}

// fortuneTarget 运气控制接口共用的应用与活动。
func (h *Handler) fortuneTarget(c *gin.Context) (int64, int64, bool) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return 0, 0, false
	}
	activityID, ok := cardKeyPathID(c, "activityId")
	if !ok {
		return 0, 0, false
	}
	return appID, activityID, true
}

// AdminLotteryFortune 运气总览：开没开、差异多大、各档位、今天抽过奖的人的分布（干预前后）、全站干预。
func (h *Handler) AdminLotteryFortune(c *gin.Context) {
	appID, activityID, ok := h.fortuneTarget(c)
	if !ok {
		return
	}
	result, err := h.lottery.FortuneOverview(c.Request.Context(), appID, activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminSetLotteryFortuneSettings 调整运气差异（0 关闭每日运气）。
func (h *Handler) AdminSetLotteryFortuneSettings(c *gin.Context) {
	appID, activityID, ok := h.fortuneTarget(c)
	if !ok {
		return
	}
	var req LotteryFortuneSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.lottery.SetFortuneSpread(c.Request.Context(), appID, activityID, req.Spread, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已更新运气差异", result)
}

// AdminLotteryFortuneUser 一个人此刻的运气：干预前后、生效的与他名下的全部干预。
func (h *Handler) AdminLotteryFortuneUser(c *gin.Context) {
	appID, activityID, ok := h.fortuneTarget(c)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	result, err := h.lottery.FortuneUser(c.Request.Context(), appID, activityID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminListLotteryFortuneControls 干预记录（userId / kind / status 可选），新的在前。
func (h *Handler) AdminListLotteryFortuneControls(c *gin.Context) {
	appID, activityID, ok := h.fortuneTarget(c)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	result, err := h.lottery.ListFortuneControls(c.Request.Context(), pgrepo.FortuneControlQuery{
		AppID: appID, ActivityID: activityID, UserID: userID, Kind: c.Query("kind"), Status: c.Query("status"),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminCreateLotteryFortuneControls 下一次运气干预。立刻生效（开始时间在未来的到时生效），活动页与抽奖同时按它算。
func (h *Handler) AdminCreateLotteryFortuneControls(c *gin.Context) {
	appID, activityID, ok := h.fortuneTarget(c)
	if !ok {
		return
	}
	var req LotteryFortuneControlRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.lottery.CreateFortuneControls(c.Request.Context(), req.input(appID, activityID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已生效", gin.H{"items": result})
}

// AdminPreviewLotteryFortuneControl 预演一次干预：按今天抽过奖的人（或所选用户）算干预之后的档位分布，不落库。
func (h *Handler) AdminPreviewLotteryFortuneControl(c *gin.Context) {
	appID, activityID, ok := h.fortuneTarget(c)
	if !ok {
		return
	}
	var req LotteryFortuneControlRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.lottery.PreviewFortuneControl(c.Request.Context(), req.input(appID, activityID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminRevokeLotteryFortuneControls 撤销干预（或全部恢复自然运气）。
func (h *Handler) AdminRevokeLotteryFortuneControls(c *gin.Context) {
	appID, activityID, ok := h.fortuneTarget(c)
	if !ok {
		return
	}
	var req LotteryFortuneRevokeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	count, err := h.lottery.RevokeFortuneControls(c.Request.Context(), appID, activityID, req.IDs, req.All, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已撤销", gin.H{"revoked": count})
}

// LotterySettingsRequest 应用的抽奖设置：每日运气的至尊加成（活动策略里没写 fortune.supreme 时跟随这里）。
type LotterySettingsRequest struct {
	Supreme lotterydomain.Supreme `json:"supreme"`
}

// AdminLotterySettings 应用的抽奖设置，附可选的会员档位与开了每日运气的活动各自的加成来源。
func (h *Handler) AdminLotterySettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.lottery.Settings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminSaveLotterySettings 保存应用的至尊加成。
func (h *Handler) AdminSaveLotterySettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LotterySettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.lottery.SaveSettings(c.Request.Context(), appID, req.Supreme, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存至尊加成", result)
}
