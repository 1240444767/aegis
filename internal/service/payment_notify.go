package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	notificationdomain "aegis/internal/domain/notification"
	paymentdomain "aegis/internal/domain/payment"
	"go.uber.org/zap"
)

// SetNotifier 注入站内信投递（bootstrap 中调用）。
//
// 本文件是「支付履约成功 → 用户站内信」的全部实现，刻意独立成一个文件：
// 上游（MiChongs/aegis）没有这个能力，独立文件让以后同步上游时几乎不会撞冲突
// ——payment_service.go 里只留一个 notifier 字段和 ensureOrderFulfilled 里的一行调用。
// 若上游哪天原生支持了同类通知，整体删掉本文件与锚点即可，不会产生重复消息。
func (s *PaymentService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// notifyFulfilled 订单履约成功后给用户投一条站内信。
//
// 覆盖「会员开通」与「余额充值」两种用途 —— 它们同属用户消息中心的「会员与订单」分类，
// 也是用户唯一能确认「钱花了、东西到账了」的地方。失败只记日志：消息发不出去
// 不能影响已经发放的权益（履约事务早已提交，这里只是事后通知）。
func (s *PaymentService) notifyFulfilled(ctx context.Context, order *paymentdomain.Order, instr paymentdomain.FulfillmentInstruction) {
	if s.notifier == nil || order == nil || order.UserID == nil || *order.UserID <= 0 {
		return
	}
	appID, userID := order.AppID, *order.UserID

	meta := map[string]any{
		"scene":      notifySceneKey(instr.Purpose),
		"orderNo":    order.OrderNo,
		"subject":    order.Subject,
		"amount":     order.Amount,
		"payChannel": order.PaymentMethod,
		"purpose":    instr.Purpose,
	}

	// type 用字面量写在各 helper 里：notification_writers_test 会扫描
	// DeliverInput 的 Type 表达式，变量会被当成未登记的写入点拦下。
	switch instr.Purpose {
	case paymentdomain.PurposeVipPurchase:
		planName := strings.TrimSpace(instr.VipPlanName)
		if planName == "" {
			planName = "会员"
		}
		validity := "永久有效"
		if !instr.VipLifetime {
			validity = strconv.Itoa(instr.VipDays) + " 天"
		}
		content := fmt.Sprintf("%s已开通（%s），支付金额 ¥%s。", planName, validity, order.Amount)
		if instr.VipBonus > 0 {
			content += fmt.Sprintf("另赠送 %d 积分。", instr.VipBonus)
		}
		content += "可在会员中心查看有效期与权益。"
		meta["planId"] = instr.VipPlanID
		meta["planName"] = planName
		meta["durationDays"] = instr.VipDays
		meta["lifetime"] = instr.VipLifetime
		s.deliverVipNotice(ctx, appID, userID, planName, content, meta)

	case paymentdomain.PurposeWalletRecharge:
		s.deliverWalletNotice(ctx, appID, userID,
			fmt.Sprintf("充值 ¥%s 已到账，可在钱包查看余额与收支明细。", order.Amount), meta)

	default:
		return
	}
}

// deliverVipNotice 投「会员开通成功」（type=vip → 分类 membership），失败只记日志。
func (s *PaymentService) deliverVipNotice(ctx context.Context, appID, userID int64, planName, content string, meta map[string]any) {
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
		s.log.Warn("payment: notify vip fulfilled failed", zap.Error(err))
	}
}

// deliverWalletNotice 投「余额充值到账」（type=wallet → 分类 membership），失败只记日志。
func (s *PaymentService) deliverWalletNotice(ctx context.Context, appID, userID int64, content string, meta map[string]any) {
	if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
		AppID:    appID,
		UserID:   userID,
		Type:     "wallet",
		Title:    "余额充值到账",
		Content:  content,
		Level:    "success",
		Metadata: meta,
		Action:   notificationdomain.RouteAction(notificationdomain.RouteWallet, "查看钱包"),
	}); err != nil {
		s.log.Warn("payment: notify wallet fulfilled failed", zap.Error(err))
	}
}

// notifySceneKey 给「会员与订单」两类消息一个统一的场景键，客户端可按它做埋点分组。
func notifySceneKey(purpose string) string {
	switch purpose {
	case paymentdomain.PurposeVipPurchase:
		return "vip_purchase"
	case paymentdomain.PurposeWalletRecharge:
		return "wallet_recharge"
	default:
		return purpose
	}
}
