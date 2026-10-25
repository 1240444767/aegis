package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	devicedomain "aegis/internal/domain/device"
	"aegis/internal/domain/devicelimit"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	miniredis "github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// 客户端换设备码（占位值修复、换生成方式）时的端到端行为：真实 Postgres + miniredis。
//
// AEGIS_TEST_PG_DSN=postgres://… go test ./internal/service -run TestDeviceIDMigrationIntegration
func TestDeviceIDMigrationIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	const appID = int64(34100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'device-migration-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{}
	cfg.JWT.Secret = "device-migration-test-secret-device-migration-test"
	cfg.JWT.Issuer = "aegis-test"
	cfg.JWT.TTL = time.Hour
	cfg.JWT.RefreshTTL = 24 * time.Hour

	log := zap.NewNop()
	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	apps := NewAppService(log, pg, sessions)
	auth := NewAuthService(cfg, log, pg, sessions, event.NewPublisher(&captureJetStream{published: map[string][][]byte{}}), apps, nil)
	limits := NewDeviceLimitService(log, pg, sessions, apps, cfg.JWT.RefreshTTL)
	auth.SetDeviceLimitService(limits)
	auth.SetCardKeyService(NewCardKeyService(log, pg))

	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!"), bcrypt.MinCost)
	user, err := pg.CreateUser(ctx, appID, "bob", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	signIn := func(lctx context.Context, deviceID string) (access, refresh string, err error) {
		lctx = devicedomain.WithClient(lctx, devicedomain.Client{Model: "M", Platform: "android"})
		result, err := auth.PasswordLogin(lctx, appID, "bob", "Passw0rd!", deviceID, "手机", "10.0.0.1", "ua-phone")
		if err != nil {
			return "", "", err
		}
		return result.AccessToken, result.RefreshToken, nil
	}

	// ── 修复前的会话存的是全零：带真实设备码刷新应放行 ──
	_, zeroRefresh, err := signIn(ctx, strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Refresh(ctx, zeroRefresh, "real-new-id", "10.0.0.1", "ua-phone"); err != nil {
		t.Fatalf("全零会话换真实设备码刷新应放行：%v", err)
	}

	// ── 换了设备码却不声明旧码：仍按设备绑定校验失败处理 ──
	_, strayRefresh, err := signIn(ctx, "old-id")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Refresh(ctx, strayRefresh, "new-id", "10.0.0.1", "ua-phone"); errCode(err) != 40104 {
		t.Fatalf("未声明旧码换设备码刷新应返回 40104，实际 %v", err)
	}

	// ── 声明了旧码：刷新放行，卡密绑定迁到新码 ──
	var batchID, cardID int64
	if err := pool.QueryRow(ctx, `INSERT INTO card_key_batches (appid, name) VALUES ($1, 'b') RETURNING id`, appID).Scan(&batchID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO card_keys (appid, batch_id, code, kind, status, bound_user_id, max_devices)
VALUES ($1, $2, 'CARD-1', 'login', 'active', $3, 1) RETURNING id`, appID, batchID, user.ID).Scan(&cardID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO card_key_devices (card_key_id, appid, device_id, device_name) VALUES ($1, $2, 'old-id', '手机')`,
		cardID, appID); err != nil {
		t.Fatal(err)
	}
	_, oldRefresh, err := signIn(ctx, "old-id")
	if err != nil {
		t.Fatal(err)
	}
	migrating := devicedomain.WithPreviousID(ctx, "old-id", "new-id")
	if _, err := auth.Refresh(migrating, oldRefresh, "new-id", "10.0.0.1", "ua-phone"); err != nil {
		t.Fatalf("声明旧码后刷新应放行：%v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var ids []string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(array_agg(device_id ORDER BY device_id), '{}') FROM card_key_devices WHERE card_key_id = $1`,
			cardID).Scan(&ids); err != nil {
			t.Fatal(err)
		}
		if len(ids) == 1 && ids[0] == "new-id" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("卡密设备绑定没有迁到新码：%v", ids)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 已有新码时合并：旧码那条删掉，不重复占位
	if _, err := pool.Exec(ctx, `INSERT INTO card_key_devices (card_key_id, appid, device_id) VALUES ($1, $2, 'older-id')`, cardID, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.MigrateCardKeyDevices(ctx, appID, user.ID, "older-id", "new-id"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM card_key_devices WHERE card_key_id = $1`, cardID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("合并后应只剩 1 条绑定：%d %v", count, err)
	}
	// 别人的卡不受影响
	other, err := pg.CreateUser(ctx, appID, "carol", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	if moved, err := pg.MigrateCardKeyDevices(ctx, appID, other.ID, "new-id", "evil-id"); err != nil || moved != 0 {
		t.Fatalf("迁移不应碰到别人的卡：%d %v", moved, err)
	}

	// ── 设备登录数 1 台、超限拒绝：旧码下的会话与声明了旧码的新登录算同一台 ──
	policy := devicelimit.DefaultPolicy()
	policy.Enabled = true
	policy.MaxDevices = 1
	policy.Overflow = devicelimit.OverflowReject
	if _, err := limits.SavePolicy(ctx, appID, policy); err != nil {
		t.Fatal(err)
	}
	active, err := limits.ActiveDevices(ctx, appID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(active))
	for _, device := range active {
		keys = append(keys, device.Key)
	}
	if _, err := limits.RemoveDevices(ctx, appID, user.ID, keys, devicelimit.EventAdminRemoved, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := signIn(ctx, "legacy-id"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := signIn(ctx, "fresh-id"); errCode(err) != errCodeDeviceLimitReached {
		t.Fatalf("不声明旧码时应算第二台被拒绝，实际 %v", err)
	}
	if _, _, err := signIn(devicedomain.WithPreviousID(ctx, "legacy-id", "fresh-id"), "fresh-id"); err != nil {
		t.Fatalf("声明旧码后应算同一台设备：%v", err)
	}
}
