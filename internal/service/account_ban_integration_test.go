package service

import (
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// 账号封禁的端到端集成测试：真实 Postgres（全部迁移）+ miniredis。
//
// 走一遍「仅登录封禁：新登录被拒、已登录设备照常用且能刷新 → 全部访问封禁顶替它：会话被撤、业务接口被拒 →
// 列表计数 → 解除 → 重复解除被拒 → 临时封禁到期后列表显示已到期 → 空原因被拒」。
// 也覆盖了用户行带永久会员列之后封禁写库的扫描列数（控制台上「执行封禁」报 11 vs 12 的那个错）。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55471/postgres go test ./internal/service -run TestAccountBanIntegration
func TestAccountBanIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	const appID = int64(33000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ban-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{}
	cfg.JWT.Secret = "account-ban-test-secret-account-ban-test-secret"
	cfg.JWT.Issuer = "aegis-test"
	cfg.JWT.TTL = time.Hour
	cfg.JWT.RefreshTTL = 24 * time.Hour

	log := zap.NewNop()
	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	app := NewAppService(log, pg, sessions)
	auth := NewAuthService(cfg, log, pg, sessions, event.NewPublisher(&captureJetStream{published: map[string][][]byte{}}), app, nil)
	bans, err := NewAccountBanService(config.AccountBanConfig{}, log, pg, sessions)
	if err != nil {
		t.Fatal(err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!"), bcrypt.MinCost)
	user, err := pg.CreateUser(ctx, appID, "alice", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	login := func() error {
		_, err := auth.PasswordLogin(ctx, appID, "alice", "Passw0rd!", "dev-1", "Pixel", "10.0.0.1", "ua")
		return err
	}
	reload := func() *userdomain.User {
		t.Helper()
		item, err := pg.GetUserByID(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	sessionCount := func() int {
		t.Helper()
		items, err := sessions.ListUserSessions(ctx, appID, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(items)
	}
	expectCode := func(name string, err error, want int) {
		t.Helper()
		if errCode(err) != want {
			t.Fatalf("%s：期望错误码 %d，实际 %v", name, want, err)
		}
	}
	operator := userdomain.BanOperator{AdminID: 1, AdminName: "root"}

	first, err := auth.PasswordLogin(ctx, appID, "alice", "Passw0rd!", "dev-1", "Pixel", "10.0.0.1", "ua")
	if err != nil {
		t.Fatalf("封禁前登录失败：%v", err)
	}
	if sessionCount() == 0 {
		t.Fatal("登录后应有会话")
	}

	// ── 仅登录 ──
	endAt := time.Now().Add(time.Hour)
	loginBan, err := bans.BanUser(ctx, appID, user.ID, userdomain.AccountBanCreateInput{
		BanType: userdomain.AccountBanTypeTemporary, BanScope: userdomain.AccountBanScopeLogin,
		Reason: "批量注册小号", EndAt: &endAt, Operator: operator,
	})
	if err != nil {
		t.Fatalf("仅登录封禁失败：%v", err)
	}
	if loginBan.Status != userdomain.AccountBanStatusActive || loginBan.BannedByAdminName != "root" {
		t.Fatalf("封禁记录不对：%+v", loginBan)
	}
	if sessionCount() == 0 {
		t.Fatal("仅登录的封禁不应撤销已有会话")
	}
	err = login()
	expectCode("仅登录封禁下的新登录", err, 40302)
	if !strings.Contains(err.Error(), "批量注册小号") || !strings.Contains(err.Error(), "解除时间") {
		t.Fatalf("提示里应带原因与解除时间：%v", err)
	}
	if err := enforceAccountBan(ctx, pg, log, reload(), userdomain.AccountBanCheckpointSession); err != nil {
		t.Fatalf("仅登录封禁不应拦已登录的会话：%v", err)
	}
	if _, err := auth.Refresh(ctx, first.RefreshToken, "dev-1", "10.0.0.1", "ua"); err != nil {
		t.Fatalf("仅登录封禁不应拦刷新令牌：%v", err)
	}
	if projected := reload(); projected.DisabledEndTime == nil {
		t.Fatal("仅登录封禁也应投影到 users.disabled_end_time，列表才能筛出受限用户")
	}

	// ── 全部访问，顶替上一条 ──
	allBan, err := bans.BanUser(ctx, appID, user.ID, userdomain.AccountBanCreateInput{
		Reason: "盗刷支付", Operator: operator,
	})
	if err != nil {
		t.Fatalf("全部访问封禁失败：%v", err)
	}
	if allBan.BanType != userdomain.AccountBanTypePermanent || allBan.BanScope != userdomain.AccountBanScopeAll {
		t.Fatalf("类型 / 范围留空应得到永久 + 全部访问：%+v", allBan)
	}
	if n := sessionCount(); n != 0 {
		t.Fatalf("全部访问的封禁应撤销全部会话，还剩 %d 个", n)
	}
	expectCode("全部访问封禁下的新登录", login(), 40301)
	expectCode("全部访问封禁下的业务接口", enforceAccountBan(ctx, pg, log, reload(), userdomain.AccountBanCheckpointSession), 40301)
	if reload().Enabled {
		t.Fatal("永久封禁应投影为 enabled=false")
	}

	list, err := bans.ListBans(ctx, appID, user.ID, userdomain.AccountBanQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Counts.Total != 2 || list.Counts.Active != 1 || list.Counts.Revoked != 1 {
		t.Fatalf("计数不对：%+v", list.Counts)
	}
	superseded := list.Items[1]
	if superseded.ID != loginBan.ID || superseded.RevokeReason != pgrepo.AccountBanSupersededReason {
		t.Fatalf("被顶替的封禁应标为已撤销并写明原因：%+v", superseded)
	}
	if filtered, err := bans.ListBans(ctx, appID, user.ID, userdomain.AccountBanQuery{Status: "revoked"}); err != nil || filtered.Total != 1 || filtered.Counts.Total != 2 {
		t.Fatalf("按状态筛选：%+v %v", filtered, err)
	}
	_, err = bans.ListBans(ctx, appID, user.ID, userdomain.AccountBanQuery{Status: "bogus"})
	expectCode("未知状态筛选", err, 40000)

	// ── 解除 ──
	revoked, err := bans.RevokeBan(ctx, appID, user.ID, allBan.ID, userdomain.AccountBanRevokeInput{Reason: "申诉通过", Operator: operator})
	if err != nil {
		t.Fatalf("解除失败：%v", err)
	}
	if revoked.Status != userdomain.AccountBanStatusRevoked || revoked.RevokeReason != "申诉通过" || revoked.RevokedByAdminName != "root" {
		t.Fatalf("解除后的记录不对：%+v", revoked)
	}
	_, err = bans.RevokeBan(ctx, appID, user.ID, allBan.ID, userdomain.AccountBanRevokeInput{Operator: operator})
	expectCode("重复解除", err, 40934)
	if err := login(); err != nil {
		t.Fatalf("解除后应能登录：%v", err)
	}
	if after := reload(); !after.Enabled || after.DisabledEndTime != nil {
		t.Fatalf("解除后投影应复原：%+v", after)
	}

	// ── 临时封禁到期 ──
	soon := time.Now().Add(time.Hour)
	tempBan, err := bans.BanUser(ctx, appID, user.ID, userdomain.AccountBanCreateInput{
		BanType: userdomain.AccountBanTypeTemporary, Reason: "刷屏", EndAt: &soon, Operator: operator,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_account_bans SET start_at = NOW() - INTERVAL '2 hours', end_at = NOW() - INTERVAL '1 second' WHERE id = $1`, tempBan.ID); err != nil {
		t.Fatal(err)
	}
	_, err = bans.RevokeBan(ctx, appID, user.ID, tempBan.ID, userdomain.AccountBanRevokeInput{Operator: operator})
	expectCode("解除已到期的封禁", err, 40934)
	list, err = bans.ListBans(ctx, appID, user.ID, userdomain.AccountBanQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Items[0].ID != tempBan.ID || list.Items[0].Status != userdomain.AccountBanStatusExpired || list.Counts.Active != 0 {
		t.Fatalf("到期的临时封禁应在列表里显示为已到期：%+v %+v", list.Items[0], list.Counts)
	}
	if err := login(); err != nil {
		t.Fatalf("到期后应能登录：%v", err)
	}

	// ── 校验 ──
	_, err = bans.BanUser(ctx, appID, user.ID, userdomain.AccountBanCreateInput{Reason: "  ", Operator: operator})
	expectCode("空原因", err, 40000)
	_, err = bans.BanUser(ctx, appID, 999999, userdomain.AccountBanCreateInput{Reason: "x", Operator: operator})
	expectCode("不存在的用户", err, 40401)

	// ── 改状态的接口同样落成封禁记录 ──
	users := NewUserService(log, pg, sessions, nil, nil)
	users.SetAccountBanService(bans)
	disabled, enabled := false, true
	if _, err := users.UpdateAdminUserStatus(ctx, appID, user.ID, userdomain.AdminUserStatusMutation{Enabled: &disabled}, operator); err != nil {
		t.Fatalf("限制账号失败：%v", err)
	}
	active, err := bans.GetActiveBan(ctx, appID, user.ID)
	if err != nil || active == nil || active.BanScope != userdomain.AccountBanScopeAll || active.Reason == "" {
		t.Fatalf("限制账号应落成带原因的全部访问封禁：%+v %v", active, err)
	}
	legacy, err := pg.CreateUser(ctx, appID, "bob", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET enabled = FALSE WHERE id = $1`, legacy.ID); err != nil {
		t.Fatal(err)
	}
	expectCode("老数据受限且无封禁记录", enforceAccountBan(ctx, pg, log, loadUser(t, pg, legacy.ID), userdomain.AccountBanCheckpointSession), 40301)
	result, err := users.BatchUpdateAdminUserStatus(ctx, appID, userdomain.AdminUserBatchStatusMutation{
		UserIDs:                 []int64{user.ID, legacy.ID},
		AdminUserStatusMutation: userdomain.AdminUserStatusMutation{Enabled: &enabled, ClearDisabledEndTime: true},
	}, operator)
	if err != nil {
		t.Fatalf("批量解除失败：%v", err)
	}
	if result.Updated != 2 {
		t.Fatalf("批量解除应同时处理有封禁记录的与老数据，实际 %d", result.Updated)
	}
	if !reload().Enabled || !loadUser(t, pg, legacy.ID).Enabled {
		t.Fatal("批量解除后两个账号都应恢复正常")
	}
}

func loadUser(t *testing.T, pg *pgrepo.Repository, userID int64) *userdomain.User {
	t.Helper()
	item, err := pg.GetUserByID(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestDeriveBanActionFromStatusMutation(t *testing.T) {
	disabled := false
	action, input, _, _ := deriveBanActionFromStatusMutation(userdomain.AdminUserStatusMutation{Enabled: &disabled}, userdomain.BanOperator{})
	if action != "create" || input.BanScope != userdomain.AccountBanScopeAll || input.BanType != userdomain.AccountBanTypePermanent || input.Reason == "" {
		t.Fatalf("限制账号应映射为带原因的永久全部访问封禁：%s %+v", action, input)
	}
}
