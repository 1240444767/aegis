package httptransport

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/pkg/response"
)

// 抽奖次数：用户端的次数明细，管理端的总览、账户、流水与增减（docs/lottery.md「抽奖次数」）。

// AppLotteryTickets 我的抽奖次数：余额、赠送 / 兑换两档、还有剩余的批次（按使用顺序）与流水一页。
func (h *Handler) AppLotteryTickets(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	result, err := h.lottery.MyTickets(c.Request.Context(), session.AppID, session.UserID,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminLotteryTicketOverview 次数总览：持有人数、未用次数（两档）、今天发了与用了几次、各来源累计与剩余。
func (h *Handler) AdminLotteryTicketOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.lottery.TicketOverview(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminListLotteryTicketAccounts 有次数账户的人（keyword 搜账号、昵称或用户 ID），余额多的在前。
func (h *Handler) AdminListLotteryTicketAccounts(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.lottery.TicketAccounts(c.Request.Context(), lotterydomain.TicketQuery{
		AppID: appID, Keyword: c.Query("keyword"),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminLotteryTicketAccount 一个人的次数账户与还有剩余的批次。
func (h *Handler) AdminLotteryTicketAccount(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	result, err := h.lottery.TicketAccount(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminListLotteryTicketLedger 次数流水（userId / kind / origin / keyword 可选），新的在前。
func (h *Handler) AdminListLotteryTicketLedger(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	result, err := h.lottery.TicketLedger(c.Request.Context(), lotterydomain.TicketQuery{
		AppID: appID, UserID: userID, Kind: c.Query("kind"), Origin: c.Query("origin"), Keyword: c.Query("keyword"),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// LotteryTicketAdjustRequest 管理员增减抽奖次数：amount 为正是发放、为负是扣减（按扣除顺序从赠送的开始扣）。
type LotteryTicketAdjustRequest struct {
	Amount int64 `json:"amount"`
	// Paid 发放的次数按兑换所得记（排在赠送的之后用），用于补发用户花钱买的次数
	Paid   bool   `json:"paid"`
	Reason string `json:"reason"`
}

// AdminAdjustLotteryTickets 给一个人发放或扣减抽奖次数，返回调整后的账户。
func (h *Handler) AdminAdjustLotteryTickets(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req LotteryTicketAdjustRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.lottery.AdjustTickets(c.Request.Context(), lotterydomain.TicketAdjust{
		AppID: appID, UserID: userID, Amount: req.Amount, Paid: req.Paid, Reason: req.Reason, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已发放"
	if req.Amount < 0 {
		message = "已扣减"
	}
	response.Success(c, http.StatusOK, message, result)
}
