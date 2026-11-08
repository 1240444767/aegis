package service

import (
	"context"
	"fmt"
	"strings"

	pointdomain "aegis/internal/domain/points"
	notificationdomain "aegis/internal/domain/notification"
	"go.uber.org/zap"
)

// SetNotifier 注入站内信投递（bootstrap 中调用）。
//
// 本文件是「管理端手动调整积分/经验 → 用户站内信」的全部实现，独立成文件以避开
// 上游同步冲突（上游没有这个能力）；points_service.go 里只留一个 notifier 字段与
// 两个 Adjust 方法里的调用行。类型 points_adjust 落「积分与奖励」分类，
// 已在 internal/domain/notification/category.go 登记。
func (s *PointsService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// notifyAdminIntegralAdjust 管理端调整积分后给用户留一条消息（发放与扣减都发，扣减同样该让用户知道）。
// 幂等重放（Replayed）不重复发；失败只记日志，不影响调整结果。
func (s *PointsService) notifyAdminIntegralAdjust(ctx context.Context, in pointdomain.AdjustInput, result *pointdomain.IntegralAdjustResult) {
	if s.notifier == nil || result == nil || in.UserID <= 0 {
		return
	}
	positive := result.Amount >= 0
	title := "积分发放到账"
	if !positive {
		title = "积分调整"
	}
	content := fmt.Sprintf("系统向您%s %d 积分，当前积分 %d。", mapSign(positive), result.Amount, result.AfterAmount)
	if reason := trimReason(in.Reason); reason != "" {
		content += "备注：" + reason + "。"
	}
	meta := map[string]any{
		"scene":         "points_admin_adjust",
		"kind":          string(pointdomain.KindIntegral),
		"amount":        result.Amount,
		"after":         result.AfterAmount,
		"transactionNo": result.TransactionNo,
		"reason":        in.Reason,
	}
	s.deliverAdjust(ctx, in.AppID, in.UserID, title, content, levelOf(positive), meta)
}

// notifyAdminExperienceAdjust 同上，经验版；等级变化时多说一句。
func (s *PointsService) notifyAdminExperienceAdjust(ctx context.Context, in pointdomain.AdjustInput, result *pointdomain.ExperienceAdjustResult) {
	if s.notifier == nil || result == nil || in.UserID <= 0 {
		return
	}
	positive := result.Amount >= 0
	title := "经验发放到账"
	if !positive {
		title = "经验调整"
	}
	content := fmt.Sprintf("系统向您%s %d 经验，当前经验 %d。", mapSign(positive), result.Amount, result.AfterAmount)
	if result.LevelChanged {
		content += fmt.Sprintf("等级由 Lv.%d 变为 Lv.%d。", result.OldLevel, result.NewLevel)
	}
	if reason := trimReason(in.Reason); reason != "" {
		content += "备注：" + reason + "。"
	}
	meta := map[string]any{
		"scene":         "points_admin_adjust",
		"kind":          string(pointdomain.KindExperience),
		"amount":        result.Amount,
		"after":         result.AfterAmount,
		"transactionNo": result.TransactionNo,
		"reason":        in.Reason,
		"levelChanged":  result.LevelChanged,
	}
	s.deliverAdjust(ctx, in.AppID, in.UserID, title, content, levelOf(positive), meta)
}

// deliverAdjust 统一出口：type=points_adjust（分类 reward），失败只记日志。
func (s *PointsService) deliverAdjust(ctx context.Context, appID, userID int64, title, content, level string, meta map[string]any) {
	if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
		AppID:    appID,
		UserID:   userID,
		// 字面量：notification_writers_test 只认字面量或已登记常量
		Type:     "points_adjust",
		Title:    title,
		Content:  content,
		Level:    level,
		Metadata: meta,
		Action:   notificationdomain.RouteAction(notificationdomain.RoutePoints, "查看积分"),
	}); err != nil {
		s.log.Warn("points: notify admin adjust failed",
			zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
	}
}

func mapSign(positive bool) string {
	if positive {
		return "发放"
	}
	return "调整"
}

func levelOf(positive bool) string {
	if positive {
		return "success"
	}
	return "info"
}

func trimReason(reason string) string {
	return strings.TrimSpace(reason)
}
