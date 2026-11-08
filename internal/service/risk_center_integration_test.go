package service

import (
	"testing"
	"time"

	"aegis/internal/config"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 风控中心端到端：真实 Postgres（全部迁移跑两遍）+ miniredis。
//
// 模板安装 → 线上评估（维度得分、观察名单、自动封禁落到账号与名单）→ 名单增删改查与检测 →
// 评估记录的各种筛选 → 复核通过撤销自动封禁 → 人工处置 → 账号画像 → 大盘全部聚合。
//
// AEGIS_TEST_PG_DSN=postgres://aegis:***@127.0.0.1:15432/aegis_risk_test?sslmode=disable go test ./internal/service -run TestRiskCenterIntegration
func TestRiskCenterIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	const appID = int64(34000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'risk-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	log := zap.NewNop()
	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	bans, err := NewAccountBanService(config.AccountBanConfig{}, log, pg, sessions)
	if err != nil {
		t.Fatal(err)
	}
	risk := NewRiskService(config.RiskConfig{}, log, pg, rdb, "t")
	risk.SetAccountBanService(bans)

	user, err := pg.CreateUser(ctx, appID, "mallory", "x")
	if err != nil {
		t.Fatal(err)
	}
	app := appID

	// ── 模板 ──
	result, err := risk.ApplyTemplate(ctx, securitydomain.TemplateApplyInput{Key: "baseline", WithActions: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.RulesCreated == 0 || result.ActionsCreated != 3 {
		t.Fatalf("模板安装结果不对：%+v", result)
	}
	again, err := risk.ApplyTemplate(ctx, securitydomain.TemplateApplyInput{Key: "baseline", WithActions: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if again.RulesCreated != 0 || again.RulesSkipped != result.RulesCreated || len(again.ActionsSkipped) != 3 {
		t.Fatalf("重复安装应全部跳过：%+v", again)
	}
	templates, err := risk.ListTemplates(ctx, nil)
	if err != nil || templates[0].Installed != result.RulesCreated {
		t.Fatalf("模板已安装计数：%v %+v", err, templates)
	}
	// 应用级 ban 策略：机器人 + 观察名单 = 70 分以上直接封禁
	if _, err := risk.CreateRiskAction(ctx, securitydomain.CreateRiskActionInput{
		AppID: &app, Scene: securitydomain.SceneLottery, MinScore: 61, Action: securitydomain.ActionBan,
		BanDuration: 3600, BanTargets: []string{securitydomain.BanTargetAccount, securitydomain.BanTargetDevice},
	}); err != nil {
		t.Fatal(err)
	}

	// ── 观察名单 ──
	saved, err := risk.SaveListEntries(ctx, securitydomain.ListEntryInput{
		ListType: securitydomain.ListTypeIP, Values: []string{"198.51.100.7", "bad-ip"}, Kind: securitydomain.ListKindWatch, Reason: "test",
	})
	if err != nil || saved.Created != 1 || len(saved.Invalid) != 1 {
		t.Fatalf("名单写入：%v %+v", err, saved)
	}

	// ── 线上评估：机器人 UA + 观察名单 IP 在抽奖场景 → 封禁 ──
	uid := user.ID
	res, err := risk.EvaluateRisk(ctx, securitydomain.RiskEvalRequest{
		Scene: securitydomain.SceneLottery, AppID: &app, UserID: &uid, IP: "198.51.100.7", DeviceID: "dev-bot",
		UserAgent: "Googlebot/2.1 (+http://www.google.com/bot.html)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != securitydomain.ActionBan || res.DimensionScores[securitydomain.DimList] == 0 || res.AssessmentID == 0 {
		t.Fatalf("应判封禁并含名单维度：%+v", res)
	}
	okTargets := map[string]bool{}
	for _, e := range res.Enforcement {
		okTargets[e.Target] = e.OK
	}
	if !okTargets[securitydomain.BanTargetAccount] || !okTargets[securitydomain.BanTargetDevice] {
		t.Fatalf("账号与设备都应封禁成功：%+v", res.Enforcement)
	}
	if ban, _ := bans.GetActiveBan(ctx, appID, user.ID); ban == nil {
		t.Fatal("账号封禁没有落到账号封禁服务")
	}
	// 设备已在黑名单：下一次请求即使 UA 正常也被拦
	next, err := risk.EvaluateRisk(ctx, securitydomain.RiskEvalRequest{
		Scene: securitydomain.SceneSignIn, AppID: &app, UserID: &uid, IP: "192.0.2.1", DeviceID: "dev-bot",
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36",
	})
	if err != nil || next.Action != securitydomain.ActionBlock || next.DecisionBy != securitydomain.DecisionByBlocklist {
		t.Fatalf("设备黑名单应拦截：%v %+v", err, next)
	}
	if GuardError(next) == nil {
		t.Fatal("拦截应翻译成业务错误")
	}

	// 等异步留痕落库
	waitFor(t, func() bool {
		items, total, _ := risk.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{AppID: &app})
		return total >= 2 && len(items) >= 2
	})

	// ── 评估记录筛选 ──
	for name, q := range map[string]securitydomain.AssessmentQuery{
		"decisionBy": {DecisionBy: securitydomain.DecisionByBlocklist},
		"dimension":  {Dimension: securitydomain.DimClient},
		"user":       {UserID: &uid},
		"keyword":    {Keyword: "dev-bot"},
	} {
		if _, total, err := risk.ListRiskAssessments(ctx, q); err != nil || total == 0 {
			t.Fatalf("筛选 %s 无结果：%v", name, err)
		}
	}
	detail, err := risk.GetRiskAssessmentDetail(ctx, res.AssessmentID)
	if err != nil || detail == nil || len(detail.Assessment.Enforcement) == 0 || len(detail.ListEntries) == 0 {
		t.Fatalf("详情应带处置留痕与现行名单：%v %+v", err, detail)
	}

	// ── 复核通过：撤销自动封禁 ──
	if _, err := pool.Exec(ctx, `UPDATE risk_assessments SET action = 'review' WHERE id = $1`, res.AssessmentID); err != nil {
		t.Fatal(err)
	}
	records, err := risk.ReviewRiskAssessment(ctx, res.AssessmentID, 1, "tester", securitydomain.ReviewInput{
		Result: "approved", RevokeAuto: true, Targets: []string{securitydomain.BanTargetIP},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ban, _ := bans.GetActiveBan(ctx, appID, user.ID); ban != nil {
		t.Fatalf("复核通过应解除账号封禁：%+v", records)
	}
	check, err := risk.CheckLists(ctx, &app, "", "198.51.100.7", "dev-bot", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, hit := range check.Hits {
		kinds[hit.ListType] = hit.Kind
	}
	if _, stillBlocked := kinds[securitydomain.ListTypeDevice]; stillBlocked || kinds[securitydomain.ListTypeIP] != securitydomain.ListKindAllow {
		t.Fatalf("设备黑名单应撤销、IP 应改为白名单：%+v", check.Hits)
	}

	// ── 人工处置 + 名单增删改 ──
	disposed, err := risk.Dispose(ctx, securitydomain.DisposeInput{
		AppID: &app, Account: "someone", DeviceID: "dev-2", Targets: []string{securitydomain.BanTargetDevice, securitydomain.BanTargetAccount},
		Kind: securitydomain.ListKindWatch, Duration: 600, Reason: "观察",
	})
	if err != nil || len(disposed) != 2 || !disposed[0].OK || !disposed[1].OK {
		t.Fatalf("人工处置：%v %+v", err, disposed)
	}
	entries, total, err := risk.ListListEntries(ctx, securitydomain.ListQuery{Kind: securitydomain.ListKindWatch})
	if err != nil || total < 2 {
		t.Fatalf("名单列表：%v %d", err, total)
	}
	block := securitydomain.ListKindBlock
	if _, err := risk.UpdateListEntry(ctx, entries[0].ID, securitydomain.ListEntryUpdate{Kind: &block}); err != nil {
		t.Fatal(err)
	}
	if n, err := risk.DeleteListEntries(ctx, []int64{entries[0].ID}); err != nil || n != 1 {
		t.Fatalf("删除名单：%v %d", err, n)
	}

	// ── 账号画像 ──
	profiles, total, err := risk.ListAccountProfiles(ctx, securitydomain.AccountQuery{AppID: &app})
	if err != nil || total == 0 || profiles[0].UserID == nil {
		t.Fatalf("账号画像：%v %+v", err, profiles)
	}
	account, err := risk.GetAccountDetail(ctx, &app, "", &uid)
	if err != nil || len(account.Recent) == 0 || len(account.Devices) == 0 {
		t.Fatalf("账号详情：%v %+v", err, account)
	}

	// ── 大盘与引擎状态（每一段聚合 SQL 都跑一遍）──
	now := time.Now()
	for _, scope := range []*int64{nil, &app} {
		dash, err := risk.GetRiskDashboard(ctx, now.Add(-time.Hour), now.Add(time.Minute), scope)
		if err != nil {
			t.Fatal(err)
		}
		if dash.Summary.TotalAssessments < 2 || len(dash.Dimensions) == 0 || len(dash.DecisionSources) == 0 || len(dash.Lists) != 3 {
			t.Fatalf("大盘数据不完整：%+v", dash.Summary)
		}
		if dash.Engine.ActiveRules == 0 || !dash.Engine.AccountBanReady || len(dash.Engine.ScenesCovered) == 0 {
			t.Fatalf("引擎状态不对：%+v", dash.Engine)
		}
	}

	// ── 模拟不改计数、不落库 ──
	_, before, _ := risk.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{})
	sim, err := risk.SimulateRisk(ctx, securitydomain.SimulateInput{Scene: securitydomain.SceneLogin, AppID: &app, IP: "203.0.113.1", UserAgent: "curl/8"})
	if err != nil || len(sim.EvaluatedRules) == 0 {
		t.Fatalf("模拟：%v %+v", err, sim)
	}
	time.Sleep(200 * time.Millisecond)
	if _, after, _ := risk.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{}); after != before {
		t.Fatalf("模拟不应落库：%d → %d", before, after)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("等待超时")
}
