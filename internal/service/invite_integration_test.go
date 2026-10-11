package service

import (
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 邀请的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「未开启 → 配置（引用核对）→ 个人码按格式生成 → 注册前核对（必填 / 无效）→ 填写（双方奖励、老字段写回）→
// 已填写 / 设备限制 / 自己的码 / 下级的码 → 阶梯奖励 → 每日上限 → 补填时限 → 签到满 N 天后发放 →
// 活动码（覆盖奖励、次数用完、批量生成）→ 自定义个人码与占用 → 老系统个人码 → 并发抢次数 →
// 管理员取消与发放 → 预览 → 我邀请的人 → 统计」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55434/postgres go test ./internal/service -run TestInviteIntegration
func TestInviteIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	svc := NewInviteService(zap.NewNop(), pg)
	const appID = int64(31000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'invite-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "online_music", Name: ptrTo("在线音乐")}); err != nil {
		t.Fatal(err)
	}

	user := func(account string) int64 {
		t.Helper()
		id := insertTestUser(t, ctx, pool, appID, account, "0")
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, nickname) VALUES ($1, $2)`, id, account); err != nil {
			t.Fatal(err)
		}
		return id
	}
	integral := func(userID int64) int64 {
		t.Helper()
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	bind := func(userID int64, code, device string) (*invite.BindResult, error) {
		return svc.Bind(ctx, InviteBindRequest{AppID: appID, UserID: userID, Code: code, DeviceID: device, ClientIP: "10.0.0.1"})
	}
	mustBind := func(userID int64, code, device string) *invite.BindResult {
		t.Helper()
		result, err := bind(userID, code, device)
		if err != nil {
			t.Fatalf("填写 %s 失败：%v", code, err)
		}
		return result
	}
	expectCode := func(name string, err error, want int) {
		t.Helper()
		if errCode(err) != want {
			t.Fatalf("%s：期望错误码 %d，得到 %v", name, want, err)
		}
	}
	save := func(enabled bool, mutate func(*invite.Config)) {
		t.Helper()
		settings, err := svc.Settings(ctx, appID)
		if err != nil {
			t.Fatal(err)
		}
		cfg := settings.Config
		mutate(&cfg)
		if _, err := svc.SaveSettings(ctx, appID, enabled, cfg, "admin"); err != nil {
			t.Fatalf("保存配置失败：%v", err)
		}
	}
	relationOf := func(inviteeID int64) *invite.Relation {
		t.Helper()
		relation, err := pg.InviteRelationOfInvitee(ctx, appID, inviteeID)
		if err != nil || relation == nil {
			t.Fatalf("读邀请关系失败：%v", err)
		}
		return relation
	}

	alice, bob, carol, dave := user("alice"), user("bob"), user("carol"), user("dave")

	// ── 未开启 ──
	center, err := svc.Center(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if center.Enabled || center.Code != "" || center.Bind.Reason != invite.ReasonDisabled {
		t.Fatalf("未开启时应 enabled=false，得到 %+v", center)
	}
	_, err = bind(bob, "ANYCODE", "")
	expectCode("未开启时填写", err, errCodeInviteDisabled)
	if err := svc.CheckRegisterCode(ctx, appID, "WHATEVER"); err != nil {
		t.Fatalf("未开启时注册应忽略邀请码，得到 %v", err)
	}
	if capability := svc.Capability(ctx, appID); capability.Field != invite.FieldHidden {
		t.Fatalf("未开启时字段应隐藏，得到 %+v", capability)
	}

	// ── 配置：引用在保存时核对 ──
	bad := invite.DefaultConfig()
	bad.InviteeRewards = []invite.Reward{{Type: pointstore.RewardFeature, Tag: "missing", Amount: 7}}
	_, err = svc.SaveSettings(ctx, appID, true, bad, "admin")
	expectCode("未登记的功能", err, 40000)

	save(true, func(cfg *invite.Config) {
		cfg.CodePrefix = "VY"
		cfg.CodeLength = 6
		cfg.InviteeRewards = []invite.Reward{
			{Type: invite.RewardPoints, Amount: 100},
			{Type: pointstore.RewardFeature, Tag: "online_music", Amount: 7},
		}
		cfg.InviterRewards = []invite.Reward{{Type: invite.RewardPoints, Amount: 50}}
		cfg.Milestones = []invite.Milestone{{Count: 2, Title: "两人", Rewards: []invite.Reward{{Type: pointstore.RewardVipDays, Amount: 3}}}}
		cfg.DeviceLimit = 1
		cfg.LandingURL = "https://example.test/i/{code}"
		cfg.RulesText = "规则"
	})

	// ── 个人码按格式生成，且稳定 ──
	center, err = svc.Center(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	aliceCode := center.Code
	if !center.Enabled || !center.CanInvite || len(aliceCode) != 8 || !strings.HasPrefix(aliceCode, "VY") {
		t.Fatalf("个人码不符合格式：%+v", center)
	}
	if center.ShareLink != "https://example.test/i/"+aliceCode || !strings.Contains(center.ShareText, aliceCode) {
		t.Fatalf("分享文案 / 链接不对：%q %q", center.ShareText, center.ShareLink)
	}
	if len(center.InviteeRewards) != 2 || center.InviteeRewards[1].Detail != "在线音乐 7 天" || len(center.Milestones) != 1 {
		t.Fatalf("奖励描述不对：%+v", center)
	}
	if !center.Bind.Available || center.Bind.Deadline == nil {
		t.Fatalf("新用户应能补填并有截止时间：%+v", center.Bind)
	}
	if again, _ := svc.Center(ctx, appID, alice); again.Code != aliceCode {
		t.Fatalf("个人码应稳定，%s → %s", aliceCode, again.Code)
	}

	// ── 注册前核对 ──
	save(true, func(cfg *invite.Config) { cfg.RegisterField = invite.FieldRequired })
	expectCode("必填却没填", svc.CheckRegisterCode(ctx, appID, " "), errCodeInviteRequired)
	expectCode("无效的码", svc.CheckRegisterCode(ctx, appID, "NOPE1234"), errCodeInviteCodeInvalid)
	if err := svc.CheckRegisterCode(ctx, appID, strings.ToLower(aliceCode)); err != nil {
		t.Fatalf("小写的有效码应通过：%v", err)
	}
	if capability := svc.Capability(ctx, appID); capability.Field != invite.FieldRequired || !capability.Enabled {
		t.Fatalf("能力应为必填：%+v", capability)
	}

	// ── 填写：双方奖励 ──
	result := mustBind(bob, " "+strings.ToLower(aliceCode[:4])+"-"+aliceCode[4:], "dev-bob")
	if len(result.Rewards) != 2 || result.Inviter == nil || result.Inviter.Nickname != "alice" {
		t.Fatalf("填写结果不对：%+v", result)
	}
	if integral(bob) != 100 || integral(alice) != 50 {
		t.Fatalf("积分不对：bob=%d alice=%d", integral(bob), integral(alice))
	}
	if relation := relationOf(bob); relation.InviterStatus != invite.InviterGranted || relation.Source != invite.SourceBind {
		t.Fatalf("关系状态不对：%+v", relation)
	}
	ent := mustEntitlement(t, vip, appID, bob)
	if ent.IsVIP || !ent.HasFeature("online_music") {
		t.Fatalf("被邀请人应只解锁功能、不成为会员：%+v", ent)
	}
	var parent string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(parent_invite_account, '') FROM user_profiles WHERE user_id = $1`, bob).Scan(&parent); err != nil || parent != "alice" {
		t.Fatalf("老字段 parent_invite_account 应写回 alice，得到 %q %v", parent, err)
	}

	_, err = bind(bob, aliceCode, "")
	expectCode("重复填写", err, errCodeInviteAlreadyBound)
	_, err = bind(carol, aliceCode, "dev-bob")
	expectCode("同一设备", err, errCodeInviteLimited)
	_, err = bind(alice, aliceCode, "")
	expectCode("自己的码", err, errCodeInviteSelf)
	bobCenter, err := svc.Center(ctx, appID, bob)
	if err != nil {
		t.Fatal(err)
	}
	if bobCenter.Bind.Available || bobCenter.Bind.Bound == nil || bobCenter.Bind.Bound.Inviter.Nickname != "alice" {
		t.Fatalf("bob 应显示已填写：%+v", bobCenter.Bind)
	}
	_, err = bind(alice, bobCenter.Code, "")
	expectCode("下级的码", err, errCodeInviteSelf)

	// ── 第二个人：阶梯奖励 ──
	mustBind(carol, aliceCode, "dev-carol")
	if integral(alice) != 100 {
		t.Fatalf("alice 应拿到两次奖励，得到 %d", integral(alice))
	}
	if ent := mustEntitlement(t, vip, appID, alice); !ent.IsVIP {
		t.Fatalf("邀请满 2 人应发会员 3 天：%+v", ent)
	}
	center, _ = svc.Center(ctx, appID, alice)
	if center.Stats.Rewarded != 2 || !center.Milestones[0].Reached || !center.Milestones[0].Granted {
		t.Fatalf("统计与阶梯不对：%+v %+v", center.Stats, center.Milestones)
	}

	// ── 每日上限：邀请照样成立，邀请人这次不发 ──
	save(true, func(cfg *invite.Config) { cfg.InviterDailyLimit = 2 })
	mustBind(dave, aliceCode, "dev-dave")
	if relation := relationOf(dave); relation.InviterStatus != invite.InviterLimited {
		t.Fatalf("超过每日上限应记 limited，得到 %s", relation.InviterStatus)
	}
	if integral(alice) != 100 || integral(dave) != 100 {
		t.Fatalf("上限后 alice 不应再加、dave 照拿：alice=%d dave=%d", integral(alice), integral(dave))
	}
	save(true, func(cfg *invite.Config) { cfg.InviterDailyLimit = 0 })

	// ── 补填时限 ──
	old := user("old-user")
	if _, err := pool.Exec(ctx, `UPDATE users SET created_at = NOW() - INTERVAL '73 hours' WHERE id = $1`, old); err != nil {
		t.Fatal(err)
	}
	_, err = bind(old, aliceCode, "")
	expectCode("超过 72 小时", err, errCodeInviteWindowClosed)
	if outcome := svc.BindAfterRegister(ctx, InviteBindRequest{AppID: appID, UserID: old, Code: aliceCode}); outcome == nil || outcome.Bound {
		t.Fatalf("老账号走注册接口不应按注册时填写处理：%+v", outcome)
	}

	// ── 签到满 N 天后发放 ──
	save(true, func(cfg *invite.Config) {
		cfg.InviterTrigger = invite.TriggerSignIn
		cfg.TriggerSignInDays = 2
	})
	frank := user("frank")
	mustBind(frank, bobCenter.Code, "dev-frank")
	if relation := relationOf(frank); relation.InviterStatus != invite.InviterPending {
		t.Fatalf("签到条件未达成应等待，得到 %s", relation.InviterStatus)
	}
	settings, _ := svc.Settings(ctx, appID)
	if _, err := pool.Exec(ctx, `INSERT INTO sign_stats (user_id, appid, total_sign_days, consecutive_days) VALUES ($1, $2, 1, 1)`, frank, appID); err != nil {
		t.Fatal(err)
	}
	if settled, err := pg.SettleInvitePending(ctx, *settings, frank, svc.windows()); err != nil || settled != nil {
		t.Fatalf("签到 1 天不应结算：%+v %v", settled, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sign_stats SET total_sign_days = 2 WHERE user_id = $1`, frank); err != nil {
		t.Fatal(err)
	}
	bobBefore := integral(bob)
	if settled, err := pg.SettleInvitePending(ctx, *settings, frank, svc.windows()); err != nil || settled == nil || settled.Status != invite.InviterGranted {
		t.Fatalf("签到 2 天应结算：%+v %v", settled, err)
	}
	if integral(bob)-bobBefore != 50 {
		t.Fatalf("bob 应拿到邀请奖励 50，得到 %d", integral(bob)-bobBefore)
	}

	// ── 管理员：取消与发放 ──
	gina, hank := user("gina"), user("hank")
	mustBind(gina, bobCenter.Code, "dev-gina")
	mustBind(hank, bobCenter.Code, "dev-hank")
	ginaRelation, hankRelation := relationOf(gina), relationOf(hank)
	if _, err := svc.CancelRelation(ctx, appID, ginaRelation.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SettleRelation(ctx, appID, ginaRelation.ID, "admin")
	expectCode("已取消的不能再发", err, errCodeInviteRelationHandled)
	bobBefore = integral(bob)
	settledRelation, err := svc.SettleRelation(ctx, appID, hankRelation.ID, "admin")
	if err != nil || settledRelation.InviterStatus != invite.InviterGranted || integral(bob)-bobBefore != 50 {
		t.Fatalf("管理员发放失败：%+v %v", settledRelation, err)
	}
	save(true, func(cfg *invite.Config) { cfg.InviterTrigger = invite.TriggerBind })

	// ── 活动码：覆盖奖励、次数用完 ──
	campaignRewards := []invite.Reward{{Type: invite.RewardPoints, Amount: 7}}
	codes, err := svc.CreateCampaignCodes(ctx, invite.CodeInput{AppID: appID, Code: "spring-2026", Title: "春季", Rewards: &campaignRewards, MaxUses: ptrTo(int64(1))})
	if err != nil || len(codes) != 1 || codes[0].Code != "SPRING2026" {
		t.Fatalf("创建活动码失败：%+v %v", codes, err)
	}
	_, err = svc.CreateCampaignCodes(ctx, invite.CodeInput{AppID: appID, Code: "SPRING2026", Title: "重复"})
	expectCode("活动码重复", err, errCodeInviteCodeTaken)
	ivy, jack := user("ivy"), user("jack")
	result = mustBind(ivy, "spring2026", "dev-ivy")
	if result.Inviter != nil || integral(ivy) != 7 || relationOf(ivy).InviterStatus != invite.InviterNone {
		t.Fatalf("纯渠道码：只发被邀请人的覆盖奖励：%+v ivy=%d", result, integral(ivy))
	}
	_, err = bind(jack, "SPRING2026", "dev-jack")
	expectCode("次数用完", err, errCodeInviteCodeInvalid)

	batch, err := svc.CreateCampaignCodes(ctx, invite.CodeInput{AppID: appID, Count: 5, Prefix: "ch", Title: "渠道", OwnerUserID: &dave})
	if err != nil || len(batch) != 5 {
		t.Fatalf("批量生成失败：%v", err)
	}
	for _, code := range batch {
		if !strings.HasPrefix(code.Code, "CH") || len(code.Code) != 8 || code.OwnerUserID == nil || *code.OwnerUserID != dave {
			t.Fatalf("批量码不对：%+v", code)
		}
	}
	daveBefore := integral(dave)
	mustBind(jack, batch[0].Code, "dev-jack")
	if integral(jack) != 100 || integral(dave)-daveBefore != 50 {
		t.Fatalf("推广码：被邀请人拿默认奖励、推广人拿邀请人奖励：jack=%d dave+%d", integral(jack), integral(dave)-daveBefore)
	}

	// ── 自定义个人码 ──
	_, err = svc.UpdateMyCode(ctx, appID, alice, "ALICE01")
	expectCode("未允许自定义", err, errCodeInviteCustomForbidden)
	save(true, func(cfg *invite.Config) { cfg.AllowCustomCode = true })
	if code, err := svc.UpdateMyCode(ctx, appID, alice, "alice01"); err != nil || code.Code != "ALICE01" || !code.Custom {
		t.Fatalf("自定义失败：%+v %v", code, err)
	}
	_, err = svc.UpdateMyCode(ctx, appID, bob, "alice01")
	expectCode("被占用", err, errCodeInviteCodeTaken)
	_, err = svc.UpdateMyCode(ctx, appID, bob, "spring2026")
	expectCode("与活动码冲突", err, errCodeInviteCodeTaken)
	kate := user("kate")
	_, err = bind(kate, aliceCode, "dev-kate")
	expectCode("换码后旧码失效", err, errCodeInviteCodeInvalid)
	mustBind(kate, "alice01", "dev-kate")
	if regenerated, err := svc.UpdateMyCode(ctx, appID, alice, ""); err != nil || regenerated.Custom || !strings.HasPrefix(regenerated.Code, "VY") {
		t.Fatalf("重新生成失败：%+v %v", regenerated, err)
	}

	// ── 老系统的个人码 ──
	legacyOwner, leo := user("legacy-owner"), user("leo")
	const legacy = "ABCDEFGHJKLMNPQRSTUVWX23"
	if _, err := pool.Exec(ctx, `UPDATE user_profiles SET invite_code = $2 WHERE user_id = $1`, legacyOwner, legacy); err != nil {
		t.Fatal(err)
	}
	result = mustBind(leo, strings.ToLower(legacy), "dev-leo")
	if result.Relation.InviterID == nil || *result.Relation.InviterID != legacyOwner || result.Relation.CodeID != nil {
		t.Fatalf("老码应找到原主人：%+v", result.Relation)
	}

	// ── 并发抢次数：3 次的码，8 个人同时填 ──
	limited, err := svc.CreateCampaignCodes(ctx, invite.CodeInput{AppID: appID, Code: "RACE01", Title: "并发", MaxUses: ptrTo(int64(3))})
	if err != nil {
		t.Fatal(err)
	}
	racers := make([]int64, 8)
	for i := range racers {
		racers[i] = user("racer-" + string(rune('a'+i)))
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i, racer := range racers {
		wg.Add(1)
		go func(i int, racer int64) {
			defer wg.Done()
			if _, err := bind(racer, "RACE01", "race-"+string(rune('a'+i))); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			} else if errCode(err) != errCodeInviteCodeInvalid {
				t.Errorf("并发填写的失败应为次数已满，得到 %v", err)
			}
		}(i, racer)
	}
	wg.Wait()
	refreshed, _ := pg.GetInviteCode(ctx, appID, limited[0].ID)
	if succeeded != 3 || refreshed.UsedCount != 3 {
		t.Fatalf("并发下应恰好 3 人成功，得到 %d（usedCount=%d）", succeeded, refreshed.UsedCount)
	}

	// ── 互相填写对方的码：不死锁，后到的一方看到下级 ──
	mike, nina := user("mike"), user("nina")
	mikeCenter, _ := svc.Center(ctx, appID, mike)
	ninaCenter, _ := svc.Center(ctx, appID, nina)
	var crossErrs [2]error
	wg.Add(2)
	go func() { defer wg.Done(); _, crossErrs[0] = bind(mike, ninaCenter.Code, "dev-mike") }()
	go func() { defer wg.Done(); _, crossErrs[1] = bind(nina, mikeCenter.Code, "dev-nina") }()
	wg.Wait()
	if (crossErrs[0] == nil) == (crossErrs[1] == nil) {
		t.Fatalf("互填应恰好一方成功：%v / %v", crossErrs[0], crossErrs[1])
	}
	for _, err := range crossErrs {
		if err != nil && errCode(err) != errCodeInviteSelf {
			t.Fatalf("互填失败的一方应为下级的码，得到 %v", err)
		}
	}

	// ── 预览 ──
	preview, err := svc.Preview(ctx, appID, batch[1].Code)
	if err != nil || !preview.Valid || preview.Inviter == nil || preview.Inviter.Nickname != "dave" || len(preview.Rewards) != 2 {
		t.Fatalf("预览不对：%+v %v", preview, err)
	}
	if preview, _ := svc.Preview(ctx, appID, "SPRING2026"); preview.Valid || preview.Reason != invite.ReasonCodeExhausted {
		t.Fatalf("用完的码预览应不可用：%+v", preview)
	}

	// ── 邀请人资格 ──
	// carol 没邀请过人、不是会员（bob 已经因邀请满 2 人拿到了会员）
	carolCenter, _ := svc.Center(ctx, appID, carol)
	save(true, func(cfg *invite.Config) { cfg.InviterAudience = invite.AudienceVip })
	if blocked, _ := svc.Center(ctx, appID, carol); blocked.CanInvite || blocked.Code != "" || blocked.InviteMessage == "" {
		t.Fatalf("非会员不应有可用的个人码：%+v", blocked)
	}
	if allowed, _ := svc.Center(ctx, appID, bob); !allowed.CanInvite {
		t.Fatalf("会员应能邀请：%+v", allowed)
	}
	oscar := user("oscar")
	_, err = bind(oscar, carolCenter.Code, "dev-oscar")
	expectCode("邀请人不满足条件", err, errCodeInviteCodeInvalid)
	save(true, func(cfg *invite.Config) { cfg.InviterAudience = invite.AudienceAll })

	// ── 我邀请的人与统计 ──
	invitees, err := svc.MyInvitees(ctx, appID, bob, 1, 20)
	if err != nil || invitees.Total != 3 {
		t.Fatalf("bob 邀请了 frank、gina、hank：%+v %v", invitees, err)
	}
	stats, err := svc.Stats(ctx, appID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Summary.Relations < 10 || stats.Summary.Limited != 1 || len(stats.Trend) != 7 || len(stats.TopInviters) == 0 {
		t.Fatalf("统计不对：%+v", stats.Summary)
	}
	if trend := stats.Trend[len(stats.Trend)-1]; trend.Relations != stats.Summary.Today {
		t.Fatalf("今日趋势与概览不一致：%+v vs %d", trend, stats.Summary.Today)
	}
}
