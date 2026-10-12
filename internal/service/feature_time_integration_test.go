package service

import (
	"strings"
	"testing"

	"go.uber.org/zap"

	"aegis/internal/config"
	cardkeydomain "aegis/internal/domain/cardkey"
	"aegis/internal/domain/featuretime"
	rewardedad "aegis/internal/domain/rewardedad"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 功能时长的端到端集成测试：真实 Postgres（全部迁移跑两遍）。
// 「场景配功能时长（未登记的功能被拒）→ 余额为 0 租不出 → 看广告领 60 分钟 → 租约扣减（超过上限截断）→
// 结算退回、重复结算幂等 → 别人的 / 不存在的租约 404 → 过期租约不退 → 租到余额为 0 → 账目对平 →
// 会员免扣、功能解锁免扣」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55434/postgres go test ./internal/service -run TestFeatureTimeIntegration
func TestFeatureTimeIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	times := NewFeatureTimeService(zap.NewNop(), pg)

	const appID = int64(32000)
	const music = "online_music"
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'feature-time-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: music, Name: ptrTo("在线听歌")}); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "ft-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "ft-bob", "0")
	carol := insertTestUser(t, ctx, pool, appID, "ft-carol", "0")

	saveScene := func(feature string) error {
		_, err := rewarded.SaveConfig(ctx, rewardedad.SaveConfigInput{
			AppID: appID, Enabled: true, VerifyMode: rewardedad.VerifyClient, Operator: "admin",
			Scenes: []rewardedad.Scene{{
				Key: "music", Name: "看广告听歌", PlacementID: "p1", Enabled: true,
				Rewards: []rewardedad.Reward{{Type: rewardedad.RewardFeatureMinutes, Amount: 60, Feature: feature}},
			}},
		}, "feature-time-test")
		return err
	}
	verified := true
	claim := func(userID int64, transID string) *rewardedad.ClaimResult {
		t.Helper()
		result, err := rewarded.Claim(ctx, RewardedAdClaimInput{AppID: appID, UserID: userID, Scene: "music",
			TransID: transID, Verified: &verified})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	status := func(userID int64) *featuretime.Status {
		t.Helper()
		st, err := times.Status(ctx, appID, userID, music)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	lease := func(userID int64, seconds int) *featuretime.Lease {
		t.Helper()
		result, err := times.Lease(ctx, appID, userID, music, seconds)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	settle := func(userID int64, leaseNo string, used int) *featuretime.Settlement {
		t.Helper()
		result, err := times.Settle(ctx, appID, userID, leaseNo, used)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	// ── 场景配置：功能必须登记在功能目录里 ──
	if err := saveScene("missing_feature"); errCode(err) != 40000 {
		t.Fatalf("未登记的功能应被拒，得到 %v", err)
	}
	if err := saveScene(" Online_Music "); err != nil {
		t.Fatal(err)
	}
	client, err := rewarded.ClientStatus(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.Scenes) != 1 || client.Scenes[0].RewardSummary != "在线听歌 60 分钟" {
		t.Fatalf("场景摘要应说清功能时长：%+v", client.Scenes)
	}
	if reward := client.Scenes[0].Rewards[0]; reward.Type != rewardedad.RewardFeatureMinutes ||
		reward.Feature != music || reward.Amount != 60 {
		t.Fatalf("场景权益应原样带出功能标识：%+v", reward)
	}

	// ── 没领过：余额为 0，租不出来；参数不合法报 40000 ──
	if st := status(alice); st.BalanceSeconds != 0 || st.Exempt || st.Name != "在线听歌" || st.Feature != music {
		t.Fatalf("初始状态不对：%+v", st)
	}
	if _, err := times.Lease(ctx, appID, alice, music, 300); errCode(err) != errCodeFeatureTimeExhausted {
		t.Fatalf("余额为 0 应返回 %d，得到 %v", errCodeFeatureTimeExhausted, err)
	}
	if _, err := times.Lease(ctx, appID, alice, music, 0); errCode(err) != 40000 {
		t.Fatalf("seconds < 1 应返回 40000，得到 %v", err)
	}
	if _, err := times.Status(ctx, appID, alice, "Bad Tag"); errCode(err) != 40000 {
		t.Fatalf("非法功能标识应返回 40000，得到 %v", err)
	}
	if st, err := times.Status(ctx, appID, alice, "unknown_feature"); err != nil || st.Name != "" || st.BalanceSeconds != 0 {
		t.Fatalf("目录里没有的功能 name 为空串：%+v %v", st, err)
	}

	// ── 看广告领 60 分钟 ──
	granted := claim(alice, "ft-trans-0001")
	if granted.Status != rewardedad.StatusGranted || len(granted.Results) != 1 {
		t.Fatalf("应当发放：%+v", granted)
	}
	if result := granted.Results[0]; result.Label != "功能时长" || result.Detail != "在线听歌 60 分钟（余额 60 分钟）" {
		t.Fatalf("发放结果不对：%+v", result)
	}
	if st := status(alice); st.BalanceSeconds != 3600 || st.EarnedSeconds != 3600 || st.UsedSeconds != 0 {
		t.Fatalf("领到后余额应为 3600 秒：%+v", st)
	}

	// ── 租约：超过 600 按 600 截断 ──
	first := lease(alice, 900)
	if first.Exempt || first.Seconds != 600 || first.BalanceSeconds != 3000 ||
		!strings.HasPrefix(first.LeaseNo, featuretime.LeaseNoPrefix) || first.ExpiresAt == "" {
		t.Fatalf("租约不对：%+v", first)
	}

	// ── 结算退回没用完的，重复结算幂等 ──
	done := settle(alice, first.LeaseNo, 120)
	if done.Status != featuretime.LeaseSettled || done.UsedSeconds != 120 || done.RefundedSeconds != 480 ||
		done.BalanceSeconds != 3480 || done.Feature != music || done.Seconds != 600 {
		t.Fatalf("结算不对：%+v", done)
	}
	again := settle(alice, first.LeaseNo, 0)
	if *again != *done {
		t.Fatalf("重复结算应原样返回第一次的结论：%+v vs %+v", again, done)
	}
	if st := status(alice); st.BalanceSeconds != 3480 || st.UsedSeconds != 120 {
		t.Fatalf("结算后余额 3480、用掉 120：%+v", st)
	}

	// ── 别人的租约、不存在的租约：404 ──
	if _, err := times.Settle(ctx, appID, bob, first.LeaseNo, 0); errCode(err) != errCodeFeatureTimeLeaseNotFound {
		t.Fatalf("结算别人的租约应返回 %d，得到 %v", errCodeFeatureTimeLeaseNotFound, err)
	}
	if _, err := times.Settle(ctx, appID, alice, "FTL-not-exists", 0); errCode(err) != errCodeFeatureTimeLeaseNotFound {
		t.Fatalf("不存在的租约应返回 %d，得到 %v", errCodeFeatureTimeLeaseNotFound, err)
	}

	// ── 过期的租约不退款，状态写成 expired ──
	stale := lease(alice, 600)
	if stale.BalanceSeconds != 2880 {
		t.Fatalf("第二次租约后余额应为 2880：%+v", stale)
	}
	if _, err := pool.Exec(ctx, `UPDATE feature_time_leases SET expires_at = NOW() - INTERVAL '1 minute' WHERE lease_no = $1`,
		stale.LeaseNo); err != nil {
		t.Fatal(err)
	}
	expired := settle(alice, stale.LeaseNo, 10)
	if expired.Status != featuretime.LeaseExpired || expired.RefundedSeconds != 0 || expired.BalanceSeconds != 2880 {
		t.Fatalf("过期租约不退款：%+v", expired)
	}
	if again := settle(alice, stale.LeaseNo, 0); again.Status != featuretime.LeaseExpired || again.RefundedSeconds != 0 {
		t.Fatalf("过期租约再结算仍是 expired：%+v", again)
	}

	// ── 租到余额为 0：最后一次按余额给，再租报 40328 ──
	for range 4 {
		lease(alice, 600)
	}
	last := lease(alice, 600)
	if last.Seconds != 480 || last.BalanceSeconds != 0 {
		t.Fatalf("余额不足时按余额给：%+v", last)
	}
	if _, err := times.Lease(ctx, appID, alice, music, 60); errCode(err) != errCodeFeatureTimeExhausted {
		t.Fatalf("余额用完应返回 %d，得到 %v", errCodeFeatureTimeExhausted, err)
	}

	// ── 账目对平：余额等于流水之和，用掉的等于获得减余额 ──
	var ledgerSum, ledgerRows int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(delta_seconds), 0), COUNT(*) FROM feature_time_ledger
WHERE appid = $1 AND user_id = $2 AND feature = $3`, appID, alice, music).Scan(&ledgerSum, &ledgerRows); err != nil {
		t.Fatal(err)
	}
	st := status(alice)
	if ledgerSum != st.BalanceSeconds || st.UsedSeconds != st.EarnedSeconds-st.BalanceSeconds {
		t.Fatalf("账目不平：流水和 %d，%+v", ledgerSum, st)
	}
	// earn 1 + lease 7 + refund 1（过期那次不记）
	if ledgerRows != 9 {
		t.Fatalf("流水应有 9 条，实际 %d", ledgerRows)
	}

	// ── 会员免扣：不扣余额、不建租约 ──
	claim(bob, "ft-trans-0002")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: bob, AppID: appID, Days: 30, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if st := status(bob); !st.Exempt || st.BalanceSeconds != 3600 {
		t.Fatalf("会员应免扣：%+v", st)
	}
	free := lease(bob, 300)
	if !free.Exempt || free.LeaseNo != "" || free.Seconds != 0 || free.BalanceSeconds != 3600 || free.ExpiresAt != "" {
		t.Fatalf("免扣租约不对：%+v", free)
	}
	var bobLeases int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM feature_time_leases WHERE user_id = $1`, bob).Scan(&bobLeases); err != nil {
		t.Fatal(err)
	}
	if bobLeases != 0 {
		t.Fatalf("免扣时不应建租约，实际 %d 条", bobLeases)
	}

	// ── 单独解锁了这个功能（不是会员）也免扣 ──
	if _, err := pool.Exec(ctx, `INSERT INTO user_feature_unlocks (appid, user_id, feature_tag, source, starts_at)
VALUES ($1, $2, $3, 'admin_grant', NOW())`, appID, carol, music); err != nil {
		t.Fatal(err)
	}
	if st := status(carol); !st.Exempt || st.BalanceSeconds != 0 {
		t.Fatalf("解锁了功能应免扣：%+v", st)
	}
	if free := lease(carol, 60); !free.Exempt {
		t.Fatalf("解锁了功能时余额为 0 也不报错：%+v", free)
	}
	// 解锁的是别的功能不算
	if st, err := times.Status(ctx, appID, carol, "other_feature"); err != nil || st.Exempt {
		t.Fatalf("解锁别的功能不免扣：%+v %v", st, err)
	}

	// ── 卡密不接纳功能时长 ──
	if err := cardkeydomain.ValidateRewards([]cardkeydomain.Reward{{Type: rewardedad.RewardFeatureMinutes, Amount: 60, Feature: music}}); err == nil {
		t.Fatal("卡密不应接纳功能时长")
	}
}
