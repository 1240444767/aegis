package httptransport

import (
	"encoding/csv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	authdomain "aegis/internal/domain/auth"
	cardkeydomain "aegis/internal/domain/cardkey"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 卡密：管理端（生成 / 导入 / 补发 / 提卡 / 作废 / 导出 / 核销记录 / 统计）
// 与用户端（预览 / 兑换 / 我的授权 / 兑换记录 / 自助解绑）。

// ── DTO ──

// CardKeyRewardRequest 一项权益。取值字段互斥，由权益目录决定用哪一个。
type CardKeyRewardRequest struct {
	Type    string `json:"type" binding:"required"`
	Amount  int64  `json:"amount"`
	Money   string `json:"money"`
	RefID   int64  `json:"refId"`
	Feature string `json:"feature"`
}

// GenerateCardKeysRequest 生成（或导入）一批卡密。codes 非空时按导入处理，count 与卡面格式不再生效。
type GenerateCardKeysRequest struct {
	Name            string                 `json:"name" binding:"required"`
	Kind            string                 `json:"kind" binding:"required"`
	Remark          string                 `json:"remark"`
	Count           int                    `json:"count"`
	Codes           []string               `json:"codes"`
	CodePrefix      string                 `json:"codePrefix"`
	Segments        int                    `json:"segments"`
	SegmentLength   int                    `json:"segmentLength"`
	Charset         string                 `json:"charset"`
	Rewards         []CardKeyRewardRequest `json:"rewards"`
	MaxDevices      int                    `json:"maxDevices"`
	MaxUses         int                    `json:"maxUses"`
	PerUserLimit    int                    `json:"perUserLimit"`
	Audience        string                 `json:"audience"`
	StartsAt        *time.Time             `json:"startsAt"`
	ValidityMode    string                 `json:"validityMode"`
	ValidityDays    int                    `json:"validityDays"`
	ValidUntil      *time.Time             `json:"validUntil"`
	SelfUnbindLimit int                    `json:"selfUnbindLimit"`
	Channel         string                 `json:"channel"`
	FaceValue       string                 `json:"faceValue"`
}

// UpdateCardKeyBatchRequest 修改批次（卡面格式、形态、有效期模式不能改）。
type UpdateCardKeyBatchRequest struct {
	Name            string                 `json:"name" binding:"required"`
	Remark          string                 `json:"remark"`
	Channel         string                 `json:"channel"`
	FaceValue       string                 `json:"faceValue"`
	Rewards         []CardKeyRewardRequest `json:"rewards"`
	PerUserLimit    int                    `json:"perUserLimit"`
	Audience        string                 `json:"audience"`
	StartsAt        *time.Time             `json:"startsAt"`
	ValidUntil      *time.Time             `json:"validUntil"`
	SelfUnbindLimit int                    `json:"selfUnbindLimit"`
}

// CardKeyCountRequest 补发 / 提卡的数量。
type CardKeyCountRequest struct {
	Count int    `json:"count" binding:"required"`
	Note  string `json:"note"`
}

// CardKeyBatchStatusRequest 批次启停。
type CardKeyBatchStatusRequest struct {
	Enabled bool `json:"enabled"`
}

// CardKeyIDsRequest 批量作废 / 恢复 / 删除。
//
// 按**选中的 id 列表**下发而不是「按当前筛选条件批量」：管理员看到的列表与
// 实际执行之间存在时间差（翻页期间有人兑换），按条件批量会误伤没被看过的卡。
type CardKeyIDsRequest struct {
	IDs    []int64 `json:"ids" binding:"required"`
	Reason string  `json:"reason"`
}

// CardKeyDistributeRequest 批量标记 / 取消「已提取」。
type CardKeyDistributeRequest struct {
	IDs         []int64 `json:"ids" binding:"required"`
	Distributed bool    `json:"distributed"`
	Note        string  `json:"note"`
}

// UpdateCardKeyRequest 调整单卡。字段缺省表示不改。
type UpdateCardKeyRequest struct {
	Remark       *string    `json:"remark"`
	MaxDevices   *int       `json:"maxDevices"`
	ExpiryAction string     `json:"expiryAction"`
	ExtendDays   int        `json:"extendDays"`
	ExpiresAt    *time.Time `json:"expiresAt"`
}

// AdminRedeemCardKeyRequest 管理员代兑。userId 与 account 二选一。
type AdminRedeemCardKeyRequest struct {
	Code    string `json:"code" binding:"required"`
	UserID  int64  `json:"userId"`
	Account string `json:"account"`
}

// CardKeyCodeRequest 用户预览一张卡。
type CardKeyCodeRequest struct {
	Code string `json:"code" binding:"required"`
}

// RedeemCardKeyRequest 用户兑换。
type RedeemCardKeyRequest struct {
	Code     string `json:"code" binding:"required"`
	DeviceID string `json:"deviceId"`
}

// CardKeyLoginResponse 卡密登录的响应：登录结果 + 这张卡的授权快照。
type CardKeyLoginResponse struct {
	*authdomain.LoginResult
	Authorization *cardkeydomain.LoginAuthorization `json:"authorization"`
}

func toCardKeyRewards(items []CardKeyRewardRequest) []cardkeydomain.Reward {
	rewards := make([]cardkeydomain.Reward, 0, len(items))
	for _, item := range items {
		rewards = append(rewards, item.toDomain())
	}
	return rewards
}

// parseCardKeyMoney 金额按字符串传输：JSON number 在前端是 IEEE754 双精度，
// 一张面额 0.1 元的卡走一遍就可能变成 0.09999999999999999。
func parseCardKeyMoney(value string) decimal.Decimal {
	if money := strings.TrimSpace(value); money != "" {
		if parsed, err := decimal.NewFromString(money); err == nil {
			return parsed
		}
	}
	return decimal.Zero
}

func (req GenerateCardKeysRequest) toInput(appID int64, operator string) cardkeydomain.GenerateInput {
	return cardkeydomain.GenerateInput{
		AppID:           appID,
		Name:            req.Name,
		Kind:            req.Kind,
		Remark:          req.Remark,
		Count:           req.Count,
		Codes:           req.Codes,
		CodePrefix:      req.CodePrefix,
		Segments:        req.Segments,
		SegmentLength:   req.SegmentLength,
		Charset:         req.Charset,
		Rewards:         toCardKeyRewards(req.Rewards),
		MaxDevices:      req.MaxDevices,
		MaxUses:         req.MaxUses,
		PerUserLimit:    req.PerUserLimit,
		Audience:        req.Audience,
		StartsAt:        req.StartsAt,
		ValidityMode:    req.ValidityMode,
		ValidityDays:    req.ValidityDays,
		ValidUntil:      req.ValidUntil,
		SelfUnbindLimit: req.SelfUnbindLimit,
		Channel:         req.Channel,
		FaceValue:       parseCardKeyMoney(req.FaceValue),
		Operator:        operator,
	}
}

func (req CardKeyRewardRequest) toDomain() cardkeydomain.Reward {
	return cardkeydomain.Reward{
		Type: req.Type, Amount: req.Amount, RefID: req.RefID, Feature: req.Feature,
		Money: parseCardKeyMoney(req.Money),
	}
}

// cardKeyPathID 取路径上的数字标识，无效时已经写好响应。
func cardKeyPathID(c *gin.Context, name string) (int64, bool) {
	value, err := pathInt64(c, name)
	if err != nil || value <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, name+" 无效")
		return 0, false
	}
	return value, true
}

func cardKeyQueryTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return &parsed
	}
	return nil
}

// ── 管理端：目录与统计 ──

// AdminCardKeyCatalog 权益目录、枚举与可引用的套餐 / 功能。控制台的批次编辑表单由它驱动。
func (h *Handler) AdminCardKeyCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	catalog, err := h.cardKey.Catalog(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", catalog)
}

// AdminCardKeyStats 卡密概览（tz 为分日时区，days 为趋势天数）。
func (h *Handler) AdminCardKeyStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.cardKey.Stats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 30), c.Query("tz"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// ── 管理端：批次 ──

// AdminListCardKeyBatches 批次列表（随行带核销进度）。
func (h *Handler) AdminListCardKeyBatches(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.cardKey.ListBatches(c.Request.Context(), appID,
		strings.TrimSpace(c.Query("keyword")), strings.TrimSpace(c.Query("kind")))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminGetCardKeyBatch 单个批次（带进度）。
func (h *Handler) AdminGetCardKeyBatch(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	batch, err := h.cardKey.GetBatch(c.Request.Context(), appID, batchID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", batch)
}

// AdminGenerateCardKeys 生成（或导入）一批卡密。
func (h *Handler) AdminGenerateCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req GenerateCardKeysRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	batch, err := h.cardKey.GenerateBatch(c.Request.Context(), req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "生成成功"
	if len(req.Codes) > 0 {
		message = "导入成功"
	}
	response.Success(c, http.StatusOK, message, batch)
}

// AdminUpdateCardKeyBatch 修改批次。
func (h *Handler) AdminUpdateCardKeyBatch(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	var req UpdateCardKeyBatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	batch, err := h.cardKey.UpdateBatch(c.Request.Context(), cardkeydomain.BatchUpdateInput{
		AppID:           appID,
		BatchID:         batchID,
		Name:            req.Name,
		Remark:          req.Remark,
		Channel:         req.Channel,
		FaceValue:       parseCardKeyMoney(req.FaceValue),
		Rewards:         toCardKeyRewards(req.Rewards),
		PerUserLimit:    req.PerUserLimit,
		Audience:        req.Audience,
		StartsAt:        req.StartsAt,
		ValidUntil:      req.ValidUntil,
		SelfUnbindLimit: req.SelfUnbindLimit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", batch)
}

// AdminAppendCardKeys 按原格式补发。
func (h *Handler) AdminAppendCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	var req CardKeyCountRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	batch, err := h.cardKey.AppendBatch(c.Request.Context(), cardkeydomain.AppendInput{
		AppID: appID, BatchID: batchID, Count: req.Count, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "补发成功", batch)
}

// AdminExtractCardKeys 提卡：取出还没交给渠道的未使用卡并打上标记，卡面随响应返回。
func (h *Handler) AdminExtractCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	var req CardKeyCountRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	batch, cards, err := h.cardKey.ExtractCards(c.Request.Context(), cardkeydomain.ExtractInput{
		AppID: appID, BatchID: batchID, Count: req.Count, Note: req.Note,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已提取", gin.H{"batch": batch, "items": cards})
}

// AdminSetCardKeyBatchStatus 启停批次。
func (h *Handler) AdminSetCardKeyBatchStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	var req CardKeyBatchStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.cardKey.SetBatchStatus(c.Request.Context(), appID, batchID, req.Enabled); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已更新", gin.H{"enabled": req.Enabled})
}

// AdminDeleteCardKeyBatch 删除批次（只能删从没被用过的批次）。
func (h *Handler) AdminDeleteCardKeyBatch(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	if err := h.cardKey.DeleteBatch(c.Request.Context(), appID, batchID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// AdminExportCardKeyBatch 导出一批卡面。
//
// format=csv（默认，带状态等列）或 txt（一行一张卡面，直接贴给渠道）；
// status=unused 只导出仍可使用的卡；undistributed=1 只导出还没交给渠道的卡。
//
// 按 batchId 导出而不是按当前筛选条件：生成完立刻导出时列表可能还没刷新到那一批，
// 按筛选导出会得到一份不完整的卡 —— 而那份文件是要发给渠道的。
func (h *Handler) AdminExportCardKeyBatch(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	query := cardkeydomain.ExportQuery{
		AppID:             appID,
		BatchID:           batchID,
		OnlyUndistributed: c.Query("undistributed") == "1" || c.Query("undistributed") == "true",
	}
	if c.Query("status") == cardkeydomain.StatusUnused {
		query.Status = cardkeydomain.StatusUnused
	}
	batch, cards, err := h.cardKey.Export(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}

	base := "card_keys_" + strconv.FormatInt(batch.ID, 10)
	if c.Query("format") == "txt" {
		filename := base + ".txt"
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Content-Disposition", "attachment; filename="+filename+"; filename*=UTF-8''"+url.PathEscape(filename))
		var sb strings.Builder
		for _, card := range cards {
			sb.WriteString(card.Code)
			sb.WriteString("\r\n")
		}
		_, _ = c.Writer.WriteString(sb.String())
		return
	}

	filename := base + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	// BOM：Excel 打开中文 CSV 不乱码
	_, _ = c.Writer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"卡密", "批次", "类型", "状态", "已用次数", "可用次数", "绑定账号", "可绑设备数",
		"已绑设备数", "激活时间", "到期时间", "提取时间", "提取备注", "备注"})
	for _, card := range cards {
		_ = writer.Write([]string{
			card.Code,
			batch.Name,
			cardKindLabel(card.Kind),
			cardStatusLabel(&card),
			strconv.Itoa(card.UseCount),
			strconv.Itoa(card.MaxUses),
			card.BoundAccount,
			strconv.Itoa(card.MaxDevices),
			strconv.Itoa(card.DeviceCount),
			formatOptionalTime(card.ActivatedAt),
			formatOptionalTime(card.ExpiresAt),
			formatOptionalTime(card.DistributedAt),
			card.DistributionNote,
			card.Remark,
		})
	}
}

// ── 管理端：单卡 ──

// AdminListCardKeys 单卡列表（服务端筛选 + 分页）。
func (h *Handler) AdminListCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, _ := strconv.ParseInt(c.Query("batchId"), 10, 64)
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, err := h.cardKey.ListCards(c.Request.Context(), cardkeydomain.CardQuery{
		AppID:       appID,
		BatchID:     batchID,
		UserID:      userID,
		Status:      strings.TrimSpace(c.Query("status")),
		Kind:        strings.TrimSpace(c.Query("kind")),
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		Distributed: strings.TrimSpace(c.Query("distributed")),
		Page:        parsePositiveInt(c.Query("page"), 1),
		Limit:       parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminGetCardKey 单卡全貌：卡、批次、设备、核销与解绑流水。
func (h *Handler) AdminGetCardKey(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cardID, ok := cardKeyPathID(c, "cardId")
	if !ok {
		return
	}
	detail, err := h.cardKey.GetCardDetail(c.Request.Context(), appID, cardID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", detail)
}

// AdminUpdateCardKey 调整单卡：备注、可绑设备数、到期时间。
func (h *Handler) AdminUpdateCardKey(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cardID, ok := cardKeyPathID(c, "cardId")
	if !ok {
		return
	}
	var req UpdateCardKeyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	card, err := h.cardKey.UpdateCard(c.Request.Context(), cardkeydomain.CardUpdateInput{
		AppID: appID, CardID: cardID, Remark: req.Remark, MaxDevices: req.MaxDevices,
		ExpiryAction: strings.TrimSpace(req.ExpiryAction), ExtendDays: req.ExtendDays, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", card)
}

// AdminDisableCardKeys 批量作废。
func (h *Handler) AdminDisableCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CardKeyIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	affected, err := h.cardKey.DisableCards(c.Request.Context(), appID, req.IDs, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已作废", gin.H{"affected": affected})
}

// AdminRestoreCardKeys 批量撤销作废。
func (h *Handler) AdminRestoreCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CardKeyIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	affected, err := h.cardKey.RestoreCards(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已恢复", gin.H{"affected": affected})
}

// AdminDeleteCardKeys 批量删除从没被用过的卡。
func (h *Handler) AdminDeleteCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CardKeyIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	affected, err := h.cardKey.DeleteCards(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"affected": affected, "skipped": int64(len(req.IDs)) - affected})
}

// AdminDistributeCardKeys 批量标记 / 取消「已提取」。
func (h *Handler) AdminDistributeCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CardKeyDistributeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	affected, err := h.cardKey.SetDistributed(c.Request.Context(), appID, req.IDs, req.Distributed, req.Note)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已更新", gin.H{"affected": affected})
}

// AdminListCardKeyDevices 一张卡绑定的设备。
func (h *Handler) AdminListCardKeyDevices(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cardID, ok := cardKeyPathID(c, "cardId")
	if !ok {
		return
	}
	items, err := h.cardKey.ListDevices(c.Request.Context(), appID, cardID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminUnbindCardKeyDevice 解绑一台设备，把名额还回去。
func (h *Handler) AdminUnbindCardKeyDevice(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cardID, ok := cardKeyPathID(c, "cardId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	if err := h.cardKey.UnbindDevice(c.Request.Context(), appID, cardID, c.Param("deviceId"), operator); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已解绑", gin.H{"unbound": true})
}

// AdminResetCardKeyDevices 清空一张卡的全部设备。
func (h *Handler) AdminResetCardKeyDevices(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cardID, ok := cardKeyPathID(c, "cardId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	affected, err := h.cardKey.ResetDevices(c.Request.Context(), appID, cardID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已清空", gin.H{"affected": affected})
}

// AdminListCardKeyRedemptions 核销记录。
func (h *Handler) AdminListCardKeyRedemptions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, _ := strconv.ParseInt(c.Query("batchId"), 10, 64)
	cardID, _ := strconv.ParseInt(c.Query("cardId"), 10, 64)
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, err := h.cardKey.ListRedemptions(c.Request.Context(), cardkeydomain.RedemptionQuery{
		AppID:   appID,
		BatchID: batchID,
		CardID:  cardID,
		UserID:  userID,
		Source:  strings.TrimSpace(c.Query("source")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		From:    cardKeyQueryTime(c.Query("from")),
		To:      cardKeyQueryTime(c.Query("to")),
		Page:    parsePositiveInt(c.Query("page"), 1),
		Limit:   parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminRedeemCardKey 管理员代兑：替一个用户核销一张卡。
func (h *Handler) AdminRedeemCardKey(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminRedeemCardKeyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.cardKey.AdminRedeem(c.Request.Context(), appID, req.Code, req.UserID, req.Account, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "兑换成功", result)
}

// ── 用户端 ──

func (h *Handler) cardKeyClient(c *gin.Context, bodyDeviceID string) (service.CardKeyClientContext, bool) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return service.CardKeyClientContext{}, false
	}
	deviceID, _ := h.resolveGatewayDevice(c, bodyDeviceID, "")
	return service.CardKeyClientContext{
		AppID: session.AppID, UserID: session.UserID, DeviceID: deviceID,
		ClientIP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	}, true
}

// AppPreviewCardKey 兑换前预览：这张卡会发什么、现在能不能用。
func (h *Handler) AppPreviewCardKey(c *gin.Context) {
	var req CardKeyCodeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	client, ok := h.cardKeyClient(c, "")
	if !ok {
		return
	}
	preview, err := h.cardKey.Preview(c.Request.Context(), client, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", preview)
}

// AppRedeemCardKey 兑换一张卡密。
func (h *Handler) AppRedeemCardKey(c *gin.Context) {
	var req RedeemCardKeyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	client, ok := h.cardKeyClient(c, req.DeviceID)
	if !ok {
		return
	}
	result, err := h.cardKey.Redeem(c.Request.Context(), client, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "兑换成功", result)
}

// AppMyCardKeys 我名下的授权卡（还剩多久、绑了哪些设备、还能自助解绑几次）。
func (h *Handler) AppMyCardKeys(c *gin.Context) {
	client, ok := h.cardKeyClient(c, c.Query("deviceId"))
	if !ok {
		return
	}
	items, err := h.cardKey.MyLicenses(c.Request.Context(), client.AppID, client.UserID, client.DeviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

// AppMyCardKeyRedemptions 我的兑换记录。
func (h *Handler) AppMyCardKeyRedemptions(c *gin.Context) {
	client, ok := h.cardKeyClient(c, "")
	if !ok {
		return
	}
	page, err := h.cardKey.MyRedemptions(c.Request.Context(), client.AppID, client.UserID,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AppUnbindMyCardKeyDevice 自助解绑自己授权卡上的一台设备。
func (h *Handler) AppUnbindMyCardKeyDevice(c *gin.Context) {
	client, ok := h.cardKeyClient(c, "")
	if !ok {
		return
	}
	cardID, ok := cardKeyPathID(c, "cardId")
	if !ok {
		return
	}
	if err := h.cardKey.SelfUnbind(c.Request.Context(), client.AppID, client.UserID, cardID, c.Param("deviceId")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已解绑", gin.H{"unbound": true})
}

// ── 展示辅助 ──

func cardKindLabel(kind string) string {
	if kind == cardkeydomain.KindLogin {
		return "授权卡"
	}
	return "兑换卡"
}

// cardStatusLabel 「已过期」是算出来的结论，不是一档状态 —— 导出的 CSV 里
// 也要如实说出来，否则运营拿着一张写着「未使用」的过期卡去排查客诉。
func cardStatusLabel(card *cardkeydomain.Card) string {
	if card.Status != cardkeydomain.StatusDisabled && card.Expired(time.Now().UTC()) {
		return "已过期"
	}
	switch card.Status {
	case cardkeydomain.StatusUnused:
		if card.UseCount > 0 {
			return "部分使用"
		}
		return "未使用"
	case cardkeydomain.StatusActive:
		return "使用中"
	case cardkeydomain.StatusUsed:
		return "已核销"
	case cardkeydomain.StatusDisabled:
		return "已作废"
	}
	return card.Status
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04:05")
}
