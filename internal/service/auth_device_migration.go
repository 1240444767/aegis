package service

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// migrateDeviceID 客户端换了设备码生成方式（devicedomain.HeaderPreviousID）：把这个用户在旧码下的绑定迁到新码。
//
// 刷新令牌的设备绑定由 Refresh 当场放行并以新码重新签发，设备登录数的旧会话在 Admit 时按新码合并，
// 这里补上另外两处会一直记着旧码的地方：登录设备检查的基线、卡密的设备绑定。
// 只动当前用户自己的数据，旁路执行，失败只记日志 —— 迁不过去的后果是多一次换绑，而不是登录失败。
func (s *AuthService) migrateDeviceID(appID, userID int64, from, to string) {
	if appID <= 0 || userID <= 0 || from == "" || to == "" || from == to {
		return
	}
	s.runDetached("device.migrate", 5*time.Second, func(ctx context.Context) {
		if s.consistency != nil {
			s.consistency.MigrateDevice(ctx, appID, userID, from, to)
		}
		if s.cardKey != nil {
			if moved, err := s.cardKey.MigrateUserDevice(ctx, appID, userID, from, to); err != nil {
				s.log.Warn("迁移卡密设备绑定失败", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
			} else if moved > 0 {
				s.log.Info("卡密设备绑定已迁到新设备码", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Int64("bindings", moved))
			}
		}
	})
}
