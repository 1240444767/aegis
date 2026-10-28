package service

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"aegis/internal/domain/itemgrant"
	plugindomain "aegis/internal/domain/plugin"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 物品发放的端到端集成测试（真实 Postgres）：
// 画布规则上线 → 手动执行（后台逐批）→ 各账本到账 → 账号不可用的人跳过 → 每人一次的限额挡住第二次执行 →
// 事件触发与幂等 → 发放失败整笔回滚并留失败记录 → 人工发放待处理 → 预演（抽样与单人）→ 总览 → 有记录的规则拒删。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestItemGrantIntegration
func TestItemGrantIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	grants := NewItemGrantService(zap.NewNop(), pg)
	const appID = int64(40500)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'item-grant') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "online_music", Name: ptrTo("在线音乐")}); err != nil {
		t.Fatal(err)
	}
	monthly := mustSavePlan(t, vip, vipdomain.PlanMutation{AppID: appID, Name: ptrTo("月度会员"), DurationDays: ptrTo(30),
		Price: ptrTo(decimal.NewFromInt(5)), Features: &[]string{"online_music"}})
	alice := insertTestUser(t, ctx, pool, appID, "grant-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "grant-bob", "0")
	carol := insertTestUser(t, ctx, pool, appID, "grant-carol", "0")
	if _, err := pool.Exec(ctx, `UPDATE users SET enabled = FALSE WHERE id = $1`, carol); err != nil {
		t.Fatal(err)
	}

	node := func(id, nodeType string, x float64, data any) itemgrant.Node {
		raw, _ := json.Marshal(data)
		return itemgrant.Node{ID: id, Type: nodeType, Position: itemgrant.Position{X: x}, Data: raw}
	}
	link := func(source, handle, target string) itemgrant.Edge {
		return itemgrant.Edge{ID: source + ">" + target, Source: source, SourceHandle: handle, Target: target}
	}
	waitRun := func(run *itemgrant.Run) *itemgrant.Run {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			current, err := grants.GetRun(ctx, appID, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != itemgrant.RunRunning {
				return current
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("执行 %d 没有在 20 秒内结束", run.ID)
		return nil
	}
	createActive := func(name string, graph itemgrant.Graph) *itemgrant.Rule {
		t.Helper()
		rule, err := grants.CreateRule(ctx, itemgrant.RuleInput{AppID: appID, Name: name, Graph: graph, Operator: "admin"})
		if err != nil {
			t.Fatalf("新建规则失败：%v", err)
		}
		if rule.Status != itemgrant.StatusDraft {
			t.Fatalf("新规则应是草稿，得到 %s", rule.Status)
		}
		active, err := grants.SetStatus(ctx, appID, rule.ID, itemgrant.StatusActive, "admin")
		if err != nil {
			t.Fatalf("上线失败：%v", err)
		}
		return active
	}

	// ── 手动执行：全部可用账号，每人一次，发一整组物品 ──
	welcome := itemgrant.Graph{
		Nodes: []itemgrant.Node{
			node("t", itemgrant.NodeTrigger, 0, itemgrant.TriggerConfig{Kind: itemgrant.TriggerManual}),
			node("aud", itemgrant.NodeAudience, 300, itemgrant.AudienceConfig{Scope: itemgrant.ScopeAll}),
			node("lim", itemgrant.NodeLimit, 600, itemgrant.LimitConfig{Period: itemgrant.PeriodTotal, PerUser: 1}),
			node("g", itemgrant.NodeGrant, 900, itemgrant.GrantConfig{Items: []itemgrant.Item{
				{Type: itemgrant.ItemPoints, Amount: 100},
				{Type: itemgrant.ItemExperience, Amount: 50},
				{Type: itemgrant.ItemVipPlan, RefID: monthly.ID},
				{Type: itemgrant.ItemFeatureTime, Tag: "online_music", Amount: 30},
				{Type: itemgrant.ItemAdFree, Amount: 2},
				{Type: itemgrant.ItemLotteryDraws, Amount: 3},
				{Type: itemgrant.ItemAICredit, Amount: 5},
				{Type: itemgrant.ItemBalance, Money: decimal.RequireFromString("1.50")},
			}}),
			node("n", itemgrant.NodeNotify, 1200, itemgrant.NotifyConfig{Title: "到账", Content: "{items}"}),
		},
		Edges: []itemgrant.Edge{link("t", "next", "aud"), link("aud", "next", "lim"), link("lim", "pass", "g"), link("g", "next", "n")},
	}
	rule := createActive("新人礼包", welcome)
	run, err := grants.RunRule(ctx, appID, rule.ID, "admin")
	if err != nil {
		t.Fatalf("执行失败：%v", err)
	}
	if run.Total != 2 {
		t.Fatalf("目标人群只算可用账号（2 人），得到 %d", run.Total)
	}
	if _, err := grants.RunRule(ctx, appID, rule.ID, "admin"); errCode(err) != 40900 {
		// 第一次可能已经跑完；只有仍在执行时才会撞上
		if err != nil {
			t.Logf("第二次执行：%v", err)
		}
	}
	done := waitRun(run)
	if done.Status != itemgrant.RunSucceeded || done.Granted != 2 || done.Failed != 0 {
		t.Fatalf("执行结果应是 2 人到账，得到 %+v", done)
	}
	var integral, experience int64
	var vipExpire *time.Time
	if err := pool.QueryRow(ctx, `SELECT integral, experience, vip_expire_at FROM users WHERE id = $1`, alice).
		Scan(&integral, &experience, &vipExpire); err != nil {
		t.Fatal(err)
	}
	if integral != 100 || experience != 50 || vipExpire == nil || vipExpire.Before(time.Now().Add(29*24*time.Hour)) {
		t.Fatalf("积分 / 经验 / 会员应到账，得到 %d %d %v", integral, experience, vipExpire)
	}
	var category string
	if err := pool.QueryRow(ctx, `SELECT category FROM integral_transactions WHERE user_id = $1 AND amount = 100`, alice).
		Scan(&category); err != nil || category != itemgrant.IntegralCategory {
		t.Fatalf("积分流水分类应为 item_grant，得到 %q %v", category, err)
	}
	var lots int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draw_lots WHERE user_id = $1 AND origin = 'item_grant' AND NOT paid`, bob).
		Scan(&lots); err != nil || lots != 1 {
		t.Fatalf("抽奖次数应记一批赠送的 item_grant，得到 %d %v", lots, err)
	}
	var balance decimal.Decimal
	if err := pool.QueryRow(ctx, `SELECT balance FROM user_wallets WHERE user_id = $1`, bob).Scan(&balance); err != nil ||
		!balance.Equal(decimal.RequireFromString("1.50")) {
		t.Fatalf("钱包余额应为 1.50，得到 %s %v", balance, err)
	}
	var adFree, featureSeconds, aiCredit int64
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_ad_free_passes WHERE user_id = $1`, alice).Scan(&adFree)
	_ = pool.QueryRow(ctx, `SELECT balance_seconds FROM feature_time_accounts WHERE user_id = $1`, alice).Scan(&featureSeconds)
	_ = pool.QueryRow(ctx, `SELECT credit FROM ai_gateway_accounts WHERE user_id = $1`, alice).Scan(&aiCredit)
	if adFree != 1 || featureSeconds != 1800 || aiCredit != 5_000_000 {
		t.Fatalf("免广告 / 功能时长 / AI 额度应到账，得到 %d %d %d", adFree, featureSeconds, aiCredit)
	}
	page, err := grants.ListRecords(ctx, itemgrant.RecordQuery{AppID: appID, RuleID: rule.ID})
	if err != nil || page.Total != 2 {
		t.Fatalf("应有 2 条发放记录，得到 %+v %v", page, err)
	}
	if record := page.Items[0]; record.Status != itemgrant.RecordGranted || len(record.Results) != 8 || len(record.Steps) != 5 {
		t.Fatalf("记录应带 8 项结果与 5 步路径，得到 %+v", record)
	}

	// 每人一次：第二次执行什么都不发
	again := waitRun(mustRun(t, grants, appID, rule.ID))
	if again.Granted != 0 || again.Skipped != 2 {
		t.Fatalf("限额应挡住第二次执行，得到 %+v", again)
	}

	// ── 事件触发：注册时发，幂等键是用户 ──
	onRegister := itemgrant.Graph{
		Nodes: []itemgrant.Node{
			node("t", itemgrant.NodeTrigger, 0, itemgrant.TriggerConfig{Kind: itemgrant.TriggerEvent, Event: itemgrant.EventRegister}),
			node("g", itemgrant.NodeGrant, 300, itemgrant.GrantConfig{Items: []itemgrant.Item{{Type: itemgrant.ItemPoints, Amount: 7}}}),
		},
		Edges: []itemgrant.Edge{link("t", "next", "g")},
	}
	eventRule := createActive("注册礼", onRegister)
	dave := insertTestUser(t, ctx, pool, appID, "grant-dave", "0")
	app := appID
	for range 2 {
		grants.ObserveHook(HookUserRegistered, map[string]any{}, plugindomain.HookMetadata{AppID: &app, UserID: &dave})
	}
	deadline := time.Now().Add(10 * time.Second)
	var eventRecords int64
	for time.Now().Before(deadline) {
		page, _ := grants.ListRecords(ctx, itemgrant.RecordQuery{AppID: appID, RuleID: eventRule.ID})
		if eventRecords = page.Total; eventRecords > 0 {
			time.Sleep(300 * time.Millisecond)
			page, _ = grants.ListRecords(ctx, itemgrant.RecordQuery{AppID: appID, RuleID: eventRule.ID})
			eventRecords = page.Total
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if eventRecords != 1 {
		t.Fatalf("注册事件两次只应发一次，得到 %d 条", eventRecords)
	}
	if _, err := grants.RunRule(ctx, appID, eventRule.ID, "admin"); errCode(err) != 40000 {
		t.Fatalf("事件规则不能批量执行，得到 %v", err)
	}

	// ── 登录事件：每次登录一个幂等键，频次交给限额节点（每天一次）──
	onLogin := itemgrant.Graph{
		Nodes: []itemgrant.Node{
			node("t", itemgrant.NodeTrigger, 0, itemgrant.TriggerConfig{Kind: itemgrant.TriggerEvent, Event: itemgrant.EventLogin}),
			node("lim", itemgrant.NodeLimit, 300, itemgrant.LimitConfig{Period: itemgrant.PeriodDay, PerUser: 1}),
			node("g", itemgrant.NodeGrant, 600, itemgrant.GrantConfig{Items: []itemgrant.Item{{Type: itemgrant.ItemExperience, Amount: 5}}}),
		},
		Edges: []itemgrant.Edge{link("t", "next", "lim"), link("lim", "pass", "g")},
	}
	loginRule := createActive("每日登录", onLogin)
	grants.OnLogin(appID, dave, "password")
	time.Sleep(400 * time.Millisecond)
	grants.OnLogin(appID, dave, "sms")
	time.Sleep(600 * time.Millisecond)
	loginPage, _ := grants.ListRecords(ctx, itemgrant.RecordQuery{AppID: appID, RuleID: loginRule.ID})
	if loginPage.Total != 1 {
		t.Fatalf("一天两次登录在每天一次的限额下只应发一次，得到 %d 条", loginPage.Total)
	}
	if _, err := grants.SetStatus(ctx, appID, loginRule.ID, itemgrant.StatusArchived, "admin"); err != nil {
		t.Fatal(err)
	}

	// ── 发放失败：名下没有授权卡，整笔回滚、留一条失败记录；重试仍失败 ──
	broken := itemgrant.Graph{
		Nodes: []itemgrant.Node{
			node("t", itemgrant.NodeTrigger, 0, itemgrant.TriggerConfig{Kind: itemgrant.TriggerManual}),
			node("aud", itemgrant.NodeAudience, 300, itemgrant.AudienceConfig{Scope: itemgrant.ScopeAll, UserIDs: []int64{alice}}),
			node("g", itemgrant.NodeGrant, 600, itemgrant.GrantConfig{Items: []itemgrant.Item{
				{Type: itemgrant.ItemPoints, Amount: 1}, {Type: itemgrant.ItemLicenseDays, Amount: 30},
			}}),
		},
		Edges: []itemgrant.Edge{link("t", "next", "aud"), link("aud", "next", "g")},
	}
	brokenRule := createActive("续费卡", broken)
	failed := waitRun(mustRun(t, grants, appID, brokenRule.ID))
	if failed.Failed != 1 || failed.Granted != 0 {
		t.Fatalf("没有授权卡应发放失败，得到 %+v", failed)
	}
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, alice).Scan(&integral); err != nil || integral != 100 {
		t.Fatalf("失败时同一笔里的积分也应回滚，得到 %d", integral)
	}
	failedPage, _ := grants.ListRecords(ctx, itemgrant.RecordQuery{AppID: appID, RuleID: brokenRule.ID, Status: itemgrant.RecordFailed})
	if failedPage.Total != 1 || failedPage.Items[0].Error == "" {
		t.Fatalf("应留一条带原因的失败记录，得到 %+v", failedPage)
	}
	retried, err := grants.RetryRecord(ctx, appID, failedPage.Items[0].ID)
	if err != nil || retried.Status != itemgrant.RecordFailed {
		t.Fatalf("重试仍应失败，得到 %+v %v", retried, err)
	}

	// ── 人工发放：记录待处理，标记后为已发放 ──
	manual := itemgrant.Graph{
		Nodes: []itemgrant.Node{
			node("t", itemgrant.NodeTrigger, 0, itemgrant.TriggerConfig{Kind: itemgrant.TriggerManual}),
			node("aud", itemgrant.NodeAudience, 300, itemgrant.AudienceConfig{Scope: itemgrant.ScopeAll, UserIDs: []int64{bob}}),
			node("g", itemgrant.NodeGrant, 600, itemgrant.GrantConfig{Items: []itemgrant.Item{{Type: itemgrant.ItemManual, Text: "周边贴纸"}}}),
		},
		Edges: []itemgrant.Edge{link("t", "next", "aud"), link("aud", "next", "g")},
	}
	manualRule := createActive("周边", manual)
	waitRun(mustRun(t, grants, appID, manualRule.ID))
	pending, _ := grants.ListRecords(ctx, itemgrant.RecordQuery{AppID: appID, Status: itemgrant.RecordPending})
	if pending.Total != 1 {
		t.Fatalf("人工发放应待处理，得到 %+v", pending)
	}
	fulfilled, err := grants.FulfillRecord(ctx, itemgrant.HandleInput{AppID: appID, RecordID: pending.Items[0].ID, Note: "已寄出", Operator: "admin"})
	if err != nil || fulfilled.Status != itemgrant.RecordFulfilled {
		t.Fatalf("标记发放失败：%+v %v", fulfilled, err)
	}

	// ── 预演：抽样与单人追踪，不发放 ──
	preview, err := grants.Preview(ctx, ItemGrantPreviewInput{AppID: appID, RuleID: rule.ID, Graph: welcome, Samples: 50})
	if err != nil || preview.Estimate == nil {
		t.Fatalf("抽样预演失败：%+v %v", preview, err)
	}
	if preview.Estimate.Candidates != 3 || preview.Estimate.Granting != 1 {
		t.Fatalf("3 个可用账号里只有 dave 还没领过，得到 %+v", preview.Estimate)
	}
	trace, err := grants.Preview(ctx, ItemGrantPreviewInput{AppID: appID, RuleID: rule.ID, Graph: welcome, User: "grant-alice"})
	if err != nil || trace.Trace == nil || trace.Trace.Outcome.Granting() {
		t.Fatalf("alice 已领过，单人预演应走到「超出」，得到 %+v %v", trace, err)
	}
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, dave).Scan(&integral); err != nil || integral != 7 {
		t.Fatalf("预演不应发放，dave 只有注册礼的 7 积分，得到 %d", integral)
	}

	// ── 定时规则：上线即排好下一次；Cron 太密拒绝 ──
	scheduled := itemgrant.Graph{
		Nodes: []itemgrant.Node{
			node("t", itemgrant.NodeTrigger, 0, itemgrant.TriggerConfig{Kind: itemgrant.TriggerSchedule, Cron: "0 10 * * *"}),
			node("g", itemgrant.NodeGrant, 300, itemgrant.GrantConfig{Items: []itemgrant.Item{{Type: itemgrant.ItemPoints, Amount: 1}}}),
		},
		Edges: []itemgrant.Edge{link("t", "next", "g")},
	}
	daily := createActive("每日 10 点", scheduled)
	if daily.NextRunAt == nil || daily.Trigger != itemgrant.TriggerSchedule {
		t.Fatalf("定时规则上线后应排好下一次，得到 %+v", daily)
	}
	issues := grants.Validate(ctx, appID, itemgrant.Graph{
		Nodes: []itemgrant.Node{
			node("t", itemgrant.NodeTrigger, 0, itemgrant.TriggerConfig{Kind: itemgrant.TriggerSchedule, Cron: "* * * * *"}),
			node("g", itemgrant.NodeGrant, 300, itemgrant.GrantConfig{Items: []itemgrant.Item{{Type: itemgrant.ItemVipPlan, RefID: 99999}}}),
		},
		Edges: []itemgrant.Edge{link("t", "next", "g")},
	})
	if len(issues) < 2 || !itemgrant.HasErrors(issues) {
		t.Fatalf("每分钟执行与不存在的套餐都应报错，得到 %+v", issues)
	}

	// ── 总览与删除 ──
	overview, err := grants.Overview(ctx, appID, 7)
	if err != nil || overview.Records != 5 || overview.Failed != 1 || overview.ActiveRules != 5 || len(overview.Trend) != 7 {
		t.Fatalf("总览不对：%+v %v", overview, err)
	}
	if err := grants.DeleteRule(ctx, appID, rule.ID); errCode(err) != 40900 {
		t.Fatalf("有记录的规则应拒删，得到 %v", err)
	}
	if err := grants.DeleteRule(ctx, appID, daily.ID); err != nil {
		t.Fatalf("没有记录的规则可以删除：%v", err)
	}
	rules, err := grants.ListRules(ctx, appID)
	if err != nil || len(rules) != 5 || rules[0].Stats == nil {
		t.Fatalf("规则列表应带统计，得到 %d 条 %v", len(rules), err)
	}
	t.Logf("records=%d users=%d byItem=%s", overview.Records, overview.Users, strconv.Itoa(len(overview.ByItem)))
}

func mustRun(t *testing.T, grants *ItemGrantService, appID, ruleID int64) *itemgrant.Run {
	t.Helper()
	run, err := grants.RunRule(t.Context(), appID, ruleID, "admin")
	if err != nil {
		t.Fatalf("执行规则 %d 失败：%v", ruleID, err)
	}
	return run
}
