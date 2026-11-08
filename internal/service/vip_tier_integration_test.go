package service

import (
	"os"
	"slices"
	"testing"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 会员档位端到端：真实 Postgres，跑全部迁移。
//
// 复现线上的现象：管理员赠送了永久会员，随后删掉永久套餐、重新上架一个功能更全的 ——
// 老会员的权益停在原来那一刻，会员页上新套餐的功能全是「未开通」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55471/postgres go test ./internal/service -run TestVipTierIntegration
func TestVipTierIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	const appID = int64(20138)
	for _, tag := range []string{"old", "splash", "image", "ai", "style"} {
		if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	features := func(user int64) []string {
		t.Helper()
		return mustEntitlement(t, vip, appID, user).Features
	}
	tierOf := func(id int64) vipdomain.Tier {
		t.Helper()
		tiers, err := vip.AdminListTiers(ctx, appID)
		if err != nil {
			t.Fatal(err)
		}
		for _, tier := range tiers {
			if tier.ID == id {
				return tier
			}
		}
		t.Fatalf("档位 %d 不在列表里", id)
		return vipdomain.Tier{}
	}

	oldLifetime := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("永久会员"), Lifetime: ptrTo(true),
		Price: ptrTo(decimal.NewFromInt(49)), Features: &[]string{"old"},
	})
	if oldLifetime.TierID == 0 || !slices.Equal(oldLifetime.Features, []string{"old"}) {
		t.Fatalf("新建套餐应自动建档位，得到 %+v", oldLifetime)
	}
	tierA := oldLifetime.TierID

	// 管理员自定义赠送永久会员（不挂套餐）→ 归入应用的永久档位
	gifted := insertTestUser(t, ctx, pool, appID, "tier-gifted", "0")
	txn, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: gifted, AppID: appID, Lifetime: true, Reason: "官方赠送"})
	if err != nil {
		t.Fatal(err)
	}
	if txn.TierID == nil || *txn.TierID != tierA {
		t.Fatalf("自定义永久会员应归入永久档位 %d，得到 %+v", tierA, txn.TierID)
	}
	bought := insertTestUser(t, ctx, pool, appID, "tier-bought", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: bought, AppID: appID, PlanID: oldLifetime.ID, Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	if got := features(gifted); !slices.Equal(got, []string{"old"}) {
		t.Fatalf("赠送的永久会员应有档位功能 old，得到 %v", got)
	}

	// 删掉套餐：档位还在，权益不动
	if err := vip.AdminDeletePlan(ctx, appID, oldLifetime.ID); err != nil {
		t.Fatal(err)
	}
	if got := tierOf(tierA); got.Members != 2 || got.LifetimeMembers != 2 || len(got.Plans) != 0 || !got.DefaultLifetime {
		t.Fatalf("删套餐后档位应仍有 2 位永久会员、没有套餐、仍是默认永久档位，得到 %+v", got)
	}

	// 重新上架、功能不同、没指定档位 → 新档位；老会员暂时还在原档位
	relisted := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("永久会员"), Lifetime: ptrTo(true),
		Price: ptrTo(decimal.NewFromInt(49)), Features: &[]string{"splash", "image", "ai"},
	})
	tierB := relisted.TierID
	if tierB == tierA {
		t.Fatal("功能不同的新套餐不该并进原档位")
	}
	if !tierOf(tierB).DefaultLifetime || tierOf(tierA).DefaultLifetime {
		t.Fatal("在售永久套餐的档位应成为默认永久档位")
	}

	// 删原档位：有会员时必须指定迁往
	if err := vip.AdminDeleteTier(ctx, appID, tierA, 0); errCode(err) != errCodeVipTierInUse {
		t.Fatalf("档位下还有会员，不指定迁往应报 40996，得到 %v", err)
	}
	if err := vip.AdminDeleteTier(ctx, appID, tierA, tierA); errCode(err) != errCodeVipTierNotFound {
		t.Fatalf("迁往自己应报 40499，得到 %v", err)
	}
	if err := vip.AdminDeleteTier(ctx, appID, tierA, tierB); err != nil {
		t.Fatal(err)
	}
	want := []string{"ai", "image", "splash"}
	if got := features(gifted); !slices.Equal(got, want) {
		t.Fatalf("迁到新档位后赠送的永久会员应有 %v，得到 %v", want, got)
	}
	if got := features(bought); !slices.Equal(got, want) {
		t.Fatalf("迁到新档位后买的永久会员应有 %v，得到 %v", want, got)
	}

	// 改档位功能：所有人即刻跟上，套餐的功能（含镜像列）一起变
	if _, err := vip.AdminSaveTier(ctx, vipdomain.TierMutation{ID: tierB, AppID: appID,
		Features: &[]string{"splash", "image", "ai", "style"}}); err != nil {
		t.Fatal(err)
	}
	want = []string{"ai", "image", "splash", "style"}
	if got := features(gifted); !slices.Equal(got, want) {
		t.Fatalf("改档位后应有 %v，得到 %v", want, got)
	}
	plan, err := pg.GetVipPlan(ctx, appID, relisted.ID)
	if err != nil || !slices.Equal(plan.Features, want) {
		t.Fatalf("套餐功能应跟随档位，得到 %+v %v", plan, err)
	}
	var mirror []string
	if err := pool.QueryRow(ctx, `SELECT features FROM vip_plans WHERE id = $1`, relisted.ID).Scan(&mirror); err != nil || !slices.Equal(mirror, want) {
		t.Fatalf("features 镜像应同步，得到 %v %v", mirror, err)
	}
	// 新会员不在原档位、套餐也不再是已包含之外的东西：永久会员已包含新套餐
	if got := mustEntitlement(t, vip, appID, gifted); !got.IsLifetime {
		t.Fatal("应仍是永久会员")
	}

	// 删掉再上架同样的套餐（不指定档位）：功能相同就回到原档位
	if err := vip.AdminDeletePlan(ctx, appID, relisted.ID); err != nil {
		t.Fatal(err)
	}
	again := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("永久会员 · 新"), Lifetime: ptrTo(true),
		Price: ptrTo(decimal.NewFromInt(59)), Features: &[]string{"style", "ai", "image", "splash"},
	})
	if again.TierID != tierB {
		t.Fatalf("功能相同的新套餐应回到档位 %d，得到 %d", tierB, again.TierID)
	}

	// 指定档位 + 另给功能
	extra := insertTestUser(t, ctx, pool, appID, "tier-extra", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: extra, AppID: appID, Days: 7, TierID: tierB,
		Features: []string{"old"}}); err != nil {
		t.Fatal(err)
	}
	if got := features(extra); !slices.Equal(got, []string{"ai", "image", "old", "splash", "style"}) {
		t.Fatalf("限时档位发放应有档位功能并上另给的 old，得到 %v", got)
	}
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: extra, AppID: appID, Days: 7, TierID: 999999}); errCode(err) != errCodeVipTierNotFound {
		t.Fatalf("不存在的档位应报 40499，得到 %v", err)
	}
	// 不挂档位的会员天数：照旧只有给的功能
	plain := insertTestUser(t, ctx, pool, appID, "tier-plain", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: plain, AppID: appID, Days: 3, Features: []string{"ai"}}); err != nil {
		t.Fatal(err)
	}
	if got := features(plain); !slices.Equal(got, []string{"ai"}) {
		t.Fatalf("不挂档位的会员天数应只有 ai，得到 %v", got)
	}

	// ── 迁移 000138 的存量归档（只在加列那一次做）──
	// 造一份「迁移前」的数据：老系统的自定义永久会员（快照 old）、套餐已删的限时会员（同名套餐还在）
	legacy := insertTestUser(t, ctx, pool, appID, "tier-legacy", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: legacy, AppID: appID, Lifetime: true, Features: []string{"old"}}); err != nil {
		t.Fatal(err)
	}
	monthly := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("月度会员"), DurationDays: ptrTo(30),
		Price: ptrTo(decimal.NewFromInt(9)), Features: &[]string{"ai"},
	})
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: plain, AppID: appID, PlanID: monthly.ID, Quantity: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE vip_transactions SET plan_id = 888888 WHERE user_id = $1 AND plan_id = $2`, plain, monthly.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE vip_transactions SET features = '{old}' WHERE user_id = $1`, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE vip_transactions DROP COLUMN tier_id CASCADE, DROP COLUMN extra_features`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/postgres/000138_vip_tiers.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("round %d: %v", round+1, err)
		}
	}
	want = []string{"ai", "image", "old", "splash", "style"}
	if got := features(legacy); !slices.Equal(got, want) {
		t.Fatalf("迁移后老的自定义永久会员应归入永久档位并保留 old，得到 %v", got)
	}
	var orphanTier *int64
	if err := pool.QueryRow(ctx, `SELECT tier_id FROM vip_transactions WHERE user_id = $1 AND plan_id = 888888`, plain).Scan(&orphanTier); err != nil {
		t.Fatal(err)
	}
	if orphanTier == nil || *orphanTier != monthly.TierID {
		t.Fatalf("套餐已删的月度会员应按同名套餐认回档位 %d，得到 %v", monthly.TierID, orphanTier)
	}
	// 不挂套餐的限时会员天数不归档
	var plainTier *int64
	if err := pool.QueryRow(ctx, `SELECT tier_id FROM vip_transactions WHERE user_id = $1 AND plan_id IS NULL AND duration_days = 3`, plain).Scan(&plainTier); err != nil {
		t.Fatal(err)
	}
	if plainTier != nil {
		t.Fatalf("不挂套餐的会员天数不该被归档，得到 %v", *plainTier)
	}
}
