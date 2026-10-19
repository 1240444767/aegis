package service

import (
	"context"
	"fmt"

	notificationdomain "aegis/internal/domain/notification"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"go.uber.org/zap"
)

type WorkerEventService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	// notifications 站内信出口（签到成功提醒），见 SetNotificationService
	notifications notificationDeliverer
}

// SetNotificationService 接上站内信：签到提醒经 NotificationService.Deliver 投递（缓存失效与实时推送都在那里）。
func (s *WorkerEventService) SetNotificationService(notifications notificationDeliverer) {
	s.notifications = notifications
}

func NewWorkerEventService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) *WorkerEventService {
	return &WorkerEventService{log: log, pg: pg, sessions: sessions}
}

func (s *WorkerEventService) HandleAuthLoginAudit(ctx context.Context, payload map[string]any) error {
	return s.pg.InsertLoginAudit(
		ctx,
		int64FromAny(payload["appid"]),
		int64FromAny(payload["user_id"]),
		stringFromAny(payload["login_type"]),
		stringFromAny(payload["provider"]),
		stringFromAny(payload["token_jti"]),
		stringFromAny(payload["ip"]),
		stringFromAny(payload["device_id"]),
		stringFromAny(payload["user_agent"]),
		"success",
		payload,
	)
}

func (s *WorkerEventService) HandleSessionAudit(ctx context.Context, payload map[string]any) error {
	return s.pg.InsertSessionAudit(
		ctx,
		int64FromAny(payload["appid"]),
		int64FromAny(payload["user_id"]),
		stringFromAny(payload["token_jti"]),
		stringFromAny(payload["event_type"]),
		payload,
	)
}

func (s *WorkerEventService) HandleUserMyAccessed(ctx context.Context, payload map[string]any) error {
	return s.pg.InsertSessionAudit(
		ctx,
		int64FromAny(payload["appid"]),
		int64FromAny(payload["user_id"]),
		stringFromAny(payload["token_jti"]),
		"user_my_accessed",
		payload,
	)
}

func (s *WorkerEventService) HandleUserSignedIn(ctx context.Context, payload map[string]any) error {
	metadata := map[string]any{
		"sign_date":         stringFromAny(payload["sign_date"]),
		"source":            stringFromAny(payload["source"]),
		"consecutive_days":  intFromAny(payload["consecutive_days"]),
		"integral_reward":   int64FromAny(payload["integral_reward"]),
		"experience_reward": int64FromAny(payload["experience_reward"]),
	}
	if err := s.pg.InsertSessionAudit(
		ctx,
		int64FromAny(payload["appid"]),
		int64FromAny(payload["user_id"]),
		stringFromAny(payload["token_jti"]),
		"signin_completed",
		metadata,
	); err != nil {
		return err
	}

	userID := int64FromAny(payload["user_id"])
	appID := int64FromAny(payload["appid"])
	if userID == 0 || appID == 0 || s.notifications == nil {
		return nil
	}

	enabled, err := s.notificationEnabled(ctx, userID)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	source := stringFromAny(payload["source"])
	title := "签到成功"
	content := fmt.Sprintf("您已完成 %s 签到，连续签到 %d 天，获得 %d 积分和 %d 经验。", sourceLabel(source), intFromAny(payload["consecutive_days"]), int64FromAny(payload["integral_reward"]), int64FromAny(payload["experience_reward"]))
	if source == "auto" {
		title = "自动签到成功"
	}
	// 站内信失败只记日志：签到审计已经落库，返回错误会让消息重投、审计重复
	if _, err := s.notifications.Deliver(ctx, notificationdomain.DeliverInput{
		AppID: appID, UserID: userID, Type: "signin", Title: title, Content: content, Level: "info", Metadata: metadata,
		Action: notificationdomain.RouteAction(notificationdomain.RouteSignIn, "查看签到"),
	}); err != nil {
		s.log.Warn("signin notification failed", zap.Int64("appid", appID), zap.Int64("user_id", userID), zap.Error(err))
	}
	return nil
}

func (s *WorkerEventService) notificationEnabled(ctx context.Context, userID int64) (bool, error) {
	setting, err := s.pg.GetUserSettings(ctx, userID, "notifications")
	if err != nil {
		return false, err
	}
	if setting == nil || setting.Settings == nil {
		return true, nil
	}
	if enabled, ok := setting.Settings["enabled"]; ok && !boolFromAny(enabled, true) {
		return false, nil
	}
	if signEnabled, ok := setting.Settings["signIn"]; ok && !boolFromAny(signEnabled, true) {
		return false, nil
	}
	return true, nil
}

func sourceLabel(source string) string {
	switch source {
	case "auto":
		return "自动"
	default:
		return "手动"
	}
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case jsonNumber:
		v, _ := typed.Int64()
		return v
	default:
		return 0
	}
}

func intFromAny(value any) int {
	return int(int64FromAny(value))
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func boolFromAny(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch typed {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}

type jsonNumber interface {
	Int64() (int64, error)
	String() string
}
