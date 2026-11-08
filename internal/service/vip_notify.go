package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	notificationdomain "aegis/internal/domain/notification"
	vipdomain "aegis/internal/domain/vip"
	"go.uber.org/zap"
)

// SetNotifier 注入站内信投递（bootstrap 中调用）。
//
// 与 payment_notify.go 同理：本文件是「余额直购成功 → 用户站内信」的全部实现，
// 独立成文件以避开上游同步冲突；vip_service.go 里只留一个 notifier 字段与一行调用。
func (s *VipService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// notifyVipGranted 余额直购成功后的站内信（type=vip → 分类 membership）。
//
// 失败只记日志：消息发不出去不能影响已经生效的会员。
// 订单支付的开通通知在 payment_notify.go（履约成功处），两条路径各发各的、不会重复。
func (s *VipService) notifyVipGranted(ctx context.Context, appID, userID int64, result *vipdomain.PurchaseResult) {
	if s.notifier == nil || result == nil || userID <= 0 {
		return
	}
	txn := result.Transaction
	planName := strings.TrimSpace(txn.PlanName)
	if planName == "" {
		planName = "会员"
	}
	validity := "永久有效"
	if !txn.Lifetime {
		validity = strconv.Itoa(txn.DurationDays) + " 天"
	}
	content := fmt.Sprintf("%s已开通（%s），余额支付 ¥%s。", planName, validity, txn.PayAmount.StringFixed(2))
	if txn.BonusIntegral > 0 {
		content += fmt.Sprintf("另赠送 %d 积分。", txn.BonusIntegral)
	}
	content += "可在会员中心查看有效期与权益。"
	meta := map[string]any{
		"scene":         "vip_purchase",
		"planId":        txn.PlanID,
		"planName":      planName,
		"durationDays":  txn.DurationDays,
		"lifetime":      txn.Lifetime,
		"payChannel":    txn.PayChannel,
		"amount":        txn.PayAmount.StringFixed(2),
		"transactionNo": txn.TransactionNo,
	}
	if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
		AppID:    appID,
		UserID:   userID,
		Type:     "vip",
		Title:    planName + "开通成功",
		Content:  content,
		Level:    "success",
		Metadata: meta,
		Action:   notificationdomain.RouteAction(notificationdomain.RouteMembership, "查看会员"),
	}); err != nil {
		s.log.Warn("vip: notify purchase failed",
			zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
	}
}
