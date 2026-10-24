package service

import (
	"strings"
	"testing"

	"aegis/internal/config"
	cardkeydomain "aegis/internal/domain/cardkey"
	rewardedad "aegis/internal/domain/rewardedad"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 永久会员看激励广告的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「永久会员看『领会员』广告 → 按价值折成积分、不再记会员段 → 客户端列表与上报结果的说明 →
// 带永久会员没有的功能时照发 → 隐藏场景 → 改发固定权益 → 管理端补发同样折算 → 统计」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55433/postgres go test ./internal/service -run TestRewardedAdLifetimeIntegration
func TestRewardedAdLifetimeIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	const appID = int64(20100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ad-lifetime-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"export", "pro", "music"} {
		if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	member := insertTestUser(t, ctx, pool, appID, "ad-lifetime-member", "0")
	plain := insertTestUser(t, ctx, pool, appID, "ad-lifetime-plain", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: member, AppID: appID, Lifetime: true,
		Features: []string{"export"}, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}

	integral := func(userID int64) int64 {
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	adSegments := func(userID int64) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM vip_transactions WHERE user_id = $1 AND pay_channel = $2`,
			userID, vipdomain.ChannelAdReward).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	save := func(scenes ...rewardedad.Scene) {
		t.Helper()
		if _, err := rewarded.SaveConfig(ctx, rewardedad.SaveConfigInput{
			AppID: appID, Enabled: true, VerifyMode: rewardedad.VerifyClient, Scenes: scenes, Operator: "admin",
		}, "ad-lifetime-test"); err != nil {
			t.Fatal(err)
		}
	}
	verified := true
	claim := func(userID int64, scene, trans string) *rewardedad.ClaimResult {
		t.Helper()
		result, err := rewarded.Claim(ctx, RewardedAdClaimInput{AppID: appID, UserID: userID, Scene: scene,
			TransID: trans, Verified: &verified})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != rewardedad.StatusGranted {
			t.Fatalf("claim should be granted, got %+v", result)
		}
		return result
	}
	findScene := func(status *rewardedad.ClientStatus, key string) *rewardedad.ClientScene {
		for index := range status.Scenes {
			if status.Scenes[index].Key == key {
				return &status.Scenes[index]
			}
		}
		return nil
	}

	vipScene := rewardedad.Scene{
		Key: "vip", Name: "看广告领会员", PlacementID: "p1", Enabled: true,
		Rewards: []rewardedad.Reward{
			{Type: cardkeydomain.RewardVipDays, Amount: 2},
			{Type: cardkeydomain.RewardExperience, Amount: 5},
		},
		Membership: rewardedad.SceneMembership{Features: []string{"export"}},
	}
	musicScene := rewardedad.Scene{
		Key: "music", Name: "看广告听歌", PlacementID: "p1", Enabled: true,
		Rewards: []rewardedad.Reward{{Type: rewardedad.RewardFeatureMinutes, Feature: "music", Amount: 30}},
		Lifetime: rewardedad.SceneLifetime{Policy: rewardedad.LifetimeHide},
	}
	save(vipScene, musicScene)

	// ── 客户端列表：永久会员看到的是折算后的；隐藏场景不出现 ──
	status, err := rewarded.ClientStatus(ctx, appID, member)
	if err != nil {
		t.Fatal(err)
	}
	item := findScene(status, "vip")
	if item == nil || item.Lifetime == nil || !strings.Contains(item.RewardSummary, "积分 20") ||
		strings.Contains(item.RewardSummary, "会员天数") || !strings.Contains(item.Lifetime.Message, "将折算为") {
		t.Fatalf("lifetime member should see the converted scene, got %+v", item)
	}
	// 不升级客户端：rewards 仍是场景原样（老 App 拿它判断发不发会员 / 听歌时长）
	if len(item.Rewards) != 2 || item.Rewards[0].Type != cardkeydomain.RewardVipDays {
		t.Fatalf("client scene rewards must stay as configured, got %+v", item.Rewards)
	}
	if findScene(status, "music") != nil {
		t.Fatal("hidden scene should not be listed for lifetime members")
	}
	plainStatus, err := rewarded.ClientStatus(ctx, appID, plain)
	if err != nil {
		t.Fatal(err)
	}
	if item := findScene(plainStatus, "vip"); item == nil || item.Lifetime != nil || !strings.Contains(item.RewardSummary, "会员天数 2 天") {
		t.Fatalf("non-members see the scene as configured, got %+v", item)
	}
	if findScene(plainStatus, "music") == nil {
		t.Fatal("non-members still see the hidden scene")
	}

	// ── 结算：会员天数折成 2 × 10 积分，经验照发，不再记会员段 ──
	before := integral(member)
	result := claim(member, "vip", "life-trans-0001")
	if result.Conversion == nil || result.Conversion.Reason != rewardedad.OwnedLifetime ||
		!strings.HasPrefix(result.Message, "你已是永久会员") {
		t.Fatalf("claim should carry the conversion, got %+v", result)
	}
	if got := integral(member) - before; got != 20 {
		t.Fatalf("converted integral = %d, want 20", got)
	}
	if adSegments(member) != 0 {
		t.Fatal("converted membership must not be recorded as a segment")
	}
	// 不升级客户端：老 App 只逐行展示 results，第一行就是折算说明
	if len(result.Results) == 0 || result.Results[0].Type != rewardedad.ResultConversion ||
		result.Results[0].Detail != result.Conversion.Message {
		t.Fatalf("first result should explain the conversion, got %+v", result.Results)
	}
	var converted, experience bool
	for _, r := range result.Results {
		converted = converted || (r.Type == cardkeydomain.RewardIntegral && strings.Contains(r.Label, "折算"))
		experience = experience || r.Type == cardkeydomain.RewardExperience
	}
	if !converted || !experience {
		t.Fatalf("results should list the converted integral and the experience, got %+v", result.Results)
	}

	// 隐藏场景：列表是旧的、观看仍然到达时按价值折算（30 分钟 × 10 / 小时 = 5）
	before = integral(member)
	claim(member, "music", "life-trans-0002")
	if got := integral(member) - before; got != 5 {
		t.Fatalf("hidden scene should still convert on arrival, got %d", got)
	}

	// 非会员照常领会员
	claim(plain, "vip", "life-trans-0003")
	if adSegments(plain) != 1 {
		t.Fatal("non-members get the membership as before")
	}

	// ── 带永久会员没有的功能：那是升级，照发 ──
	upgrade := vipScene
	upgrade.Membership.Features = []string{"export", "pro"}
	save(upgrade, musicScene)
	if result := claim(member, "vip", "life-trans-0004"); result.Conversion != nil {
		t.Fatalf("an upgrade should be granted as is, got %+v", result.Conversion)
	}
	if adSegments(member) != 1 {
		t.Fatal("upgrade should record the ad membership segment")
	}

	// ── 改发固定权益 / 照常发放 ──
	replace := vipScene
	replace.Lifetime = rewardedad.SceneLifetime{Policy: rewardedad.LifetimeReplace,
		Rewards: []rewardedad.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 66}}}
	save(replace, musicScene)
	before = integral(member)
	claim(member, "vip", "life-trans-0005")
	if got := integral(member) - before; got != 66 {
		t.Fatalf("replace should grant the fixed rewards, got %d", got)
	}
	grant := vipScene
	grant.Lifetime = rewardedad.SceneLifetime{Policy: rewardedad.LifetimeGrant}
	save(grant, musicScene)
	if result := claim(member, "vip", "life-trans-0006"); result.Conversion != nil {
		t.Fatal("grant policy keeps the old behaviour")
	}
	if adSegments(member) != 2 {
		t.Fatal("grant policy records the segment")
	}

	// ── 管理端补发同样折算 ──
	save(vipScene, musicScene)
	if _, err := pool.Exec(ctx, `INSERT INTO app_rewarded_ad_views (appid, user_id, trans_id, scene, status, reason)
VALUES ($1, $2, 'life-trans-0007', 'vip', 'rejected', 'daily_limit')`, appID, member); err != nil {
		t.Fatal(err)
	}
	var viewID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM app_rewarded_ad_views WHERE trans_id = 'life-trans-0007'`).Scan(&viewID); err != nil {
		t.Fatal(err)
	}
	before = integral(member)
	view, err := rewarded.GrantView(ctx, appID, viewID, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if view.Conversion == nil || integral(member)-before != 20 {
		t.Fatalf("admin grant should convert as well, got %+v", view.Conversion)
	}
	if stored, err := pg.GetRewardedAdView(ctx, appID, viewID); err != nil || stored.Conversion == nil ||
		stored.Conversion.Policy != rewardedad.LifetimeConvert {
		t.Fatalf("conversion should be persisted, got %+v / %v", stored, err)
	}

	// ── 统计 ──
	stats, err := rewarded.Stats(ctx, appID, 7)
	if err != nil {
		t.Fatal(err)
	}
	// 折算过的：0001、0002、0005、0007；积分合计 20 + 5 + 66 + 20
	if stats.Summary.TotalConverted != 4 || stats.Summary.ConvertedIntegral != 111 {
		t.Fatalf("stats should count conversions, got %+v", stats.Summary)
	}
	if last := stats.Trend[len(stats.Trend)-1]; last.Converted != 4 {
		t.Fatalf("today's trend should count conversions, got %+v", last)
	}
}
