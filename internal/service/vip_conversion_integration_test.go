package service

import (
	"strings"
	"testing"
	"time"

	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 会员权益统一折算的端到端集成测试：真实 Postgres，跑全部迁移。
//
//	开通永久 → 限时会员、限时功能解锁、功能时长、免广告时长按剩余价值折成积分并就此结束（预览与实际一致）
//	永久会员再收到会员天数 → 折成积分、不记会员段；管理员选「照常发放」时照记
//	取消永久会员 → 折掉的存量还回、积分收回
//	基础版被覆盖、后面排着的高级版前移；策略为保留时什么也不动
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55441/postgres go test ./internal/service -run TestVipConversionIntegration
func TestVipConversionIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	const appID = int64(20131)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'vip-conversion-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"export", "ai", "music"} {
		if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	integral := func(userID int64) int64 {
		t.Helper()
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	expireAt := func(userID int64) *time.Time {
		t.Helper()
		var value *time.Time
		if err := pool.QueryRow(ctx, `SELECT vip_expire_at FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	near := func(got *time.Time, want time.Time) bool {
		return got != nil && got.Sub(want).Abs() < time.Minute
	}
	balance := func(userID int64) int64 {
		t.Helper()
		var value int64
		_ = pool.QueryRow(ctx, `SELECT balance_seconds FROM feature_time_accounts WHERE appid = $1 AND user_id = $2 AND feature = 'music'`,
			appID, userID).Scan(&value)
		return value
	}

	// ── 开通永久：存量折算 ──
	alice := insertTestUser(t, ctx, pool, appID, "conv-alice", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: alice, AppID: appID, Days: 30,
		Features: []string{"export"}, Reason: "月卡", Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.GrantFeatureUnlock(ctx, pgrepo.FeatureUnlockGrant{AppID: appID, UserID: alice, Tag: "export", Days: 5,
		Source: vipdomain.UnlockSourceAdminGrant}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO feature_time_accounts (appid, user_id, feature, balance_seconds, earned_seconds)
VALUES ($1, $2, 'music', 3600, 3600)`, appID, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_ad_free_passes (appid, user_id, expires_at, granted_days)
VALUES ($1, $2, NOW() + INTERVAL '2 days', 2)`, appID, alice); err != nil {
		t.Fatal(err)
	}
	// 30 天 × 10 + 解锁 5 天 × 5 + 1 小时 × 10 + 免广告 2 天 × 5（按秒向上取整，允许 ±1）
	const wantPoints = 300 + 25 + 10 + 10
	grantLifetime := AdminVipGrantInput{UserID: alice, AppID: appID, Lifetime: true, Features: []string{"export"}, Operator: "admin"}
	preview, err := vip.AdminPreviewGrant(ctx, grantLifetime)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Upgrade == nil || preview.Upgrade.Mode != vipdomain.UpgradeConvert || len(preview.Upgrade.Items) != 4 ||
		preview.Upgrade.Points < wantPoints || preview.Upgrade.Points > wantPoints+4 {
		t.Fatalf("开通永久的预览不对：%+v", preview.Upgrade)
	}
	before := integral(alice)
	txn, err := vip.AdminGrantVip(ctx, grantLifetime)
	if err != nil {
		t.Fatal(err)
	}
	if txn.Conversion == nil || txn.Conversion.Kind != vipdomain.ConversionUpgrade {
		t.Fatalf("永久开通应带回折算摘要：%+v", txn.Conversion)
	}
	gained := integral(alice) - before
	if gained != txn.Conversion.Points || gained < wantPoints || gained > wantPoints+4 {
		t.Fatalf("折算积分应约为 %d，得到 %d（摘要 %d）", wantPoints, gained, txn.Conversion.Points)
	}
	if !near(expireAt(alice), time.Now()) {
		t.Fatalf("限时会员应结束于此刻，得到 %v", expireAt(alice))
	}
	if balance(alice) != 0 {
		t.Fatalf("功能时长余额应折掉，剩 %d", balance(alice))
	}
	var liveUnlocks, adFreeLive int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_feature_unlocks WHERE user_id = $1 AND revoked_at IS NULL AND ends_at > NOW()`, alice).Scan(&liveUnlocks)
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_ad_free_passes WHERE user_id = $1 AND expires_at > NOW()`, alice).Scan(&adFreeLive)
	if liveUnlocks != 0 || adFreeLive != 0 {
		t.Fatalf("功能解锁与免广告应结束：%d / %d", liveUnlocks, adFreeLive)
	}
	ent, err := vip.AdminEntitlement(ctx, appID, alice)
	if err != nil || !ent.IsLifetime || len(ent.Features) != 1 || ent.Features[0] != "export" {
		t.Fatalf("应为永久会员且功能仍在：%+v %v", ent, err)
	}
	records, _, err := vip.AdminListTransactions(ctx, appID, alice, 1, 10)
	if err != nil || records[0].PayChannel != vipdomain.ChannelLifetimeConvert || records[0].DurationDays != -30 ||
		records[0].Conversion == nil {
		t.Fatalf("账本最新一行应为折算说明：%+v %v", records[0], err)
	}

	// ── 永久会员再收到会员天数：折成积分 ──
	expireBefore := expireAt(alice)
	before = integral(alice)
	owned, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: alice, AppID: appID, Days: 7, Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if owned.Conversion == nil || owned.Conversion.Kind != vipdomain.ConversionOwned || integral(alice)-before != 70 ||
		owned.DurationDays != 0 || !strings.Contains(owned.Conversion.Message, "已折算为 70 积分") {
		t.Fatalf("已拥有的会员天数应折成 70 积分：%+v %+v", owned, owned.Conversion)
	}
	if got := expireAt(alice); (got == nil) != (expireBefore == nil) || (got != nil && !got.Equal(*expireBefore)) {
		t.Fatalf("折算不应动到期时间：%v → %v", expireBefore, got)
	}
	// 带永久会员没有的功能：照常发放
	upgrade, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: alice, AppID: appID, Days: 3, Features: []string{"ai"}, Operator: "admin"})
	if err != nil || upgrade.Conversion != nil || upgrade.DurationDays != 3 {
		t.Fatalf("多出功能的限时会员应照常发放：%+v %v", upgrade, err)
	}
	// 管理员这一次选照常发放
	forced, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: alice, AppID: appID, Days: 2, OwnedPolicy: vipdomain.OwnedGrant, Operator: "admin"})
	if err != nil || forced.Conversion != nil || forced.DurationDays != 2 {
		t.Fatalf("照常发放应记一段：%+v %v", forced, err)
	}

	// ── 取消永久会员：还回 ──
	// 先把上面两段照常发放的收掉，免得混进到期时间的断言
	if _, err := vip.AdminRevokeVip(ctx, AdminVipRevokeInput{UserID: alice, AppID: appID, Days: 10, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	before = integral(alice)
	if _, err := vip.AdminRevokeVipLifetime(ctx, AdminVipRevokeLifetimeInput{UserID: alice, AppID: appID, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if !near(expireAt(alice), time.Now().Add(30*24*time.Hour)) {
		t.Fatalf("取消永久后限时会员应还回 30 天，得到 %v", expireAt(alice))
	}
	if before-integral(alice) != txn.Conversion.Points {
		t.Fatalf("应收回 %d 积分，实际 %d", txn.Conversion.Points, before-integral(alice))
	}
	if balance(alice) != 3600 {
		t.Fatalf("功能时长应还回，得到 %d", balance(alice))
	}
	conversions, total, err := vip.AdminListConversions(ctx, appID, alice, 1, 10)
	if err != nil || total != 2 {
		t.Fatalf("应有两条折算记录：%d %v", total, err)
	}
	for _, item := range conversions {
		if item.Kind == vipdomain.ConversionUpgrade && item.Status != vipdomain.ConversionRestored {
			t.Fatalf("开通折算应标为已还回：%+v", item)
		}
	}
	ent, err = vip.AdminEntitlement(ctx, appID, alice)
	if err != nil || ent.IsLifetime || !ent.IsVIP {
		t.Fatalf("取消永久后应是限时会员：%+v %v", ent, err)
	}

	// ── 基础版被覆盖，后面的高级版前移 ──
	bob := insertTestUser(t, ctx, pool, appID, "conv-bob", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: bob, AppID: appID, Days: 10, Features: []string{"export"}, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: bob, AppID: appID, Days: 20, Features: []string{"export", "ai"}, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: bob, AppID: appID, Lifetime: true, Features: []string{"export"}, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if !near(expireAt(bob), time.Now().Add(20*24*time.Hour)) {
		t.Fatalf("高级版应前移到此刻开始、还剩 20 天，得到 %v", expireAt(bob))
	}
	ent, err = vip.AdminEntitlement(ctx, appID, bob)
	if err != nil || len(ent.Features) != 2 {
		t.Fatalf("高级版的功能应仍在：%+v %v", ent, err)
	}

	// ── 策略为保留：什么也不动 ──
	keep := vipdomain.DefaultConversionPolicy(appID)
	keep.Upgrade = vipdomain.UpgradeKeep
	if _, err := vip.AdminSaveConversionPolicy(ctx, keep); err != nil {
		t.Fatal(err)
	}
	carol := insertTestUser(t, ctx, pool, appID, "conv-carol", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: carol, AppID: appID, Days: 15, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	kept, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: carol, AppID: appID, Lifetime: true, Operator: "admin"})
	if err != nil || kept.Conversion != nil || !near(expireAt(carol), time.Now().Add(15*24*time.Hour)) {
		t.Fatalf("保留策略下限时会员应原样保留：%+v %v %v", kept, err, expireAt(carol))
	}
	// 管理员这一次单独指定折算
	dave := insertTestUser(t, ctx, pool, appID, "conv-dave", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: dave, AppID: appID, Days: 15, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	forcedUpgrade, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: dave, AppID: appID, Lifetime: true,
		UpgradePolicy: vipdomain.UpgradeConvert, Operator: "admin"})
	if err != nil || forcedUpgrade.Conversion == nil || forcedUpgrade.Conversion.Points != 150 {
		t.Fatalf("单独指定折算时应折 150 积分：%+v %v", forcedUpgrade, err)
	}
	policy, err := vip.AdminConversionPolicy(ctx, appID)
	if err != nil || !policy.Configured || policy.Upgrade != vipdomain.UpgradeKeep {
		t.Fatalf("策略应已保存：%+v %v", policy, err)
	}
}
