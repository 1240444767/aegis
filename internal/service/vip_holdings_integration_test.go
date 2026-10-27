package service

import (
	"testing"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 「已包含」的统一判据（vip.Holdings）端到端：真实 Postgres，跑全部迁移。
//
// 复现的是线上的现象：管理员赠送的永久会员，在会员页上权益全亮，底部却还有月卡的购买栏。
// 永久开通带 export，ai 是另外永久解锁的，月卡里还挂着一个已停用的 legacy ——
// 旧判据只拿永久开通的功能去比，月卡于是「还能买」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55441/postgres go test ./internal/service -run TestVipHoldingsIntegration
func TestVipHoldingsIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	pay := &PaymentService{log: zap.NewNop(), pg: pg}
	const appID = int64(20132)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'vip-holdings-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"export", "ai", "legacy", "music"} {
		if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	lifetimePlan := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("永久版"), Lifetime: ptrTo(true),
		Price: ptrTo(decimal.NewFromInt(99)), Features: &[]string{"export"},
	})
	monthly := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("月度会员"), DurationDays: ptrTo(30),
		Price: ptrTo(decimal.NewFromInt(5)), Features: &[]string{"export", "ai", "legacy"},
	})
	premium := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("高级月卡"), DurationDays: ptrTo(30),
		Price: ptrTo(decimal.NewFromInt(20)), Features: &[]string{"export", "music"},
	})
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "legacy", IsActive: ptrTo(false)}); err != nil {
		t.Fatal(err)
	}

	user := insertTestUser(t, ctx, pool, appID, "holdings-user", "100")
	session := &authdomain.Session{AppID: appID, UserID: user}
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: user, AppID: appID, PlanID: lifetimePlan.ID,
		Quantity: 1, Lifetime: true, Reason: "官方赠送", Operator: "admin"}); err != nil {
		t.Fatal(err)
	}

	included := func() map[int64]bool {
		t.Helper()
		plans, err := vip.ListActivePlans(ctx, session)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, plan := range plans {
			out[plan.ID] = plan.Included
		}
		return out
	}
	// 还没有永久解锁 ai：月卡带来启用中的 ai，是实打实的升级
	if got := included(); got[monthly.ID] || !got[lifetimePlan.ID] || got[premium.ID] {
		t.Fatalf("没解锁 ai 时月卡可买、永久版已包含、高级月卡可买，得到 %v", got)
	}

	if _, err := pg.GrantFeatureUnlock(ctx, pgrepo.FeatureUnlockGrant{AppID: appID, UserID: user, Tag: "ai",
		Source: vipdomain.UnlockSourceAdminGrant}); err != nil {
		t.Fatal(err)
	}
	// 永久开通 export + 永久解锁 ai，legacy 已停用：月卡对他是空的
	if got := included(); !got[monthly.ID] || got[premium.ID] {
		t.Fatalf("月卡应已包含、高级月卡仍可买，得到 %v", got)
	}
	if _, err := vip.PurchaseWithWallet(ctx, session, monthly.ID, "holdings-monthly", "127.0.0.1"); errCode(err) != errCodeVipLifetimeIncluded {
		t.Fatalf("余额购买已包含的月卡应报 40378，得到 %v", err)
	}
	if _, err := pay.prepareFulfillmentMetadata(ctx, appID, user, decimal.NewFromInt(5), map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeVipPurchase, paymentdomain.MetaKeyVipPlanID: monthly.ID,
	}); errCode(err) != errCodeVipLifetimeIncluded {
		t.Fatalf("在线下单已包含的月卡应报 40378，得到 %v", err)
	}
	if _, err := vip.PurchaseWithWallet(ctx, session, premium.ID, "holdings-premium", "127.0.0.1"); err != nil {
		t.Fatalf("高级月卡带来 music，应能购买：%v", err)
	}

	// 管理员再发一张月卡：按应用折算策略折成积分，不记会员段
	var before int64
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, user).Scan(&before); err != nil {
		t.Fatal(err)
	}
	txn, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: user, AppID: appID, PlanID: monthly.ID,
		Quantity: 1, Reason: "补偿", Operator: "admin"})
	if err != nil || txn.Conversion == nil || txn.Conversion.Points != 300 {
		t.Fatalf("已包含的月卡发放应折成 30 天 × 10 = 300 积分，得到 %+v %v", txn, err)
	}

	// 停用的功能恢复启用：月卡又带来了 legacy，可以买了
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "legacy", IsActive: ptrTo(true)}); err != nil {
		t.Fatal(err)
	}
	if got := included(); got[monthly.ID] {
		t.Fatalf("legacy 恢复启用后月卡应可买，得到 %v", got)
	}
}
