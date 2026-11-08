package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/itemgrant"
	"aegis/internal/domain/levels"
	notificationdomain "aegis/internal/domain/notification"
	pointdomain "aegis/internal/domain/points"
	pgrepo "aegis/internal/repository/postgres"
)

type captureNotifier struct {
	mu    sync.Mutex
	items []notificationdomain.DeliverInput
}

func (c *captureNotifier) Deliver(_ context.Context, in notificationdomain.DeliverInput) (*notificationdomain.Item, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append(c.items, in)
	return &notificationdomain.Item{}, nil
}

// 等级体系的端到端集成测试（真实 Postgres）：
// 默认体系与旧表一致 → 画布发布 → 加经验升级（自动奖励后台到账、手动奖励待领取、升级通知）→ 领取与重复领取 →
// 晋级条件挡住、满足后补判升级 → 等级权益倍率 → 管理员调级 → 新体系发布并重算（允许降级）→ 补发 → 版本与恢复 →
// 草稿并发保护 → 总览 → 等级排名用新体系的名称。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55438/postgres go test ./internal/service -run TestLevelIntegration
func TestLevelIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	notifier := &captureNotifier{}
	svc := NewLevelService(zap.NewNop(), pg)
	svc.SetNotifier(notifier)
	const appID = int64(40700)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'levels') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "level-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "level-bob", "0")
	carol := insertTestUser(t, ctx, pool, appID, "level-carol", "0")
	addExp := func(userID, amount int64) {
		t.Helper()
		if _, err := pg.AdminAdjustExperience(ctx, pointdomain.AdjustInput{Kind: pointdomain.KindExperience, AppID: appID, UserID: userID, Amount: amount},
			pointdomain.AdminAdjustOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	profile := func(userID int64) pointdomain.LevelInfo {
		t.Helper()
		p, err := pg.GetUserLevelProfile(ctx, userID, appID)
		if err != nil || p == nil {
			t.Fatalf("profile: %v", err)
		}
		return p.LevelInfo
	}

	// ── 默认体系：旧版全站等级表 ──
	scheme, err := svc.GetScheme(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	if !scheme.IsDefault || scheme.Summary.Levels != 18 || len(scheme.Issues) != 0 {
		t.Fatalf("默认体系应由旧表 18 级转换而来：%+v issues=%v", scheme.Summary, scheme.Issues)
	}
	addExp(carol, 300)
	if info := profile(carol); info.CurrentLevel != 3 || info.CurrentLevelName != "学徒" || info.ExpMultiplier != 1.02 {
		t.Fatalf("默认体系下 300 经验应是 Lv.3 学徒 ×1.02，得到 %+v", info)
	}

	// ── 画布：L1(0) 自动奖励 → L2(100) 待领取奖励 + 经验加成 → 晋级条件（会员）→ L3(500) 自定义权益 ──
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	n := func(id, typ string, data any) levels.Node { return levels.Node{ID: id, Type: typ, Data: raw(data)} }
	e := func(src, handle, dst string) levels.Edge {
		return levels.Edge{ID: src + ":" + handle + "->" + dst, Source: src, SourceHandle: handle, Target: dst}
	}
	graph := levels.Graph{
		Nodes: []levels.Node{
			n("s", levels.NodeScheme, levels.SchemeConfig{NodeMeta: levels.NodeMeta{Label: "测试等级"}, Notify: true,
				NotifyTitle: "升到 Lv.{level} {name}", NotifyContent: "{rewards}"}),
			n("l1", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "青铜"}, Threshold: 0, Icon: "medal"}),
			n("r1", levels.NodeReward, levels.RewardConfig{NodeMeta: levels.NodeMeta{Label: "见面礼"}, Items: []itemgrant.Item{{Type: itemgrant.ItemPoints, Amount: 10}}}),
			n("l2", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "白银"}, Threshold: 100}),
			n("r2", levels.NodeReward, levels.RewardConfig{NodeMeta: levels.NodeMeta{Label: "白银礼包"}, Claim: levels.ClaimManual,
				Items: []itemgrant.Item{{Type: itemgrant.ItemLotteryDraws, Amount: 2}}}),
			n("p2", levels.NodePrivilege, levels.PrivilegeConfig{Kind: levels.PrivilegeExpBoost, Value: 1.5}),
			n("g3", levels.NodeGate, levels.GateConfig{Mode: itemgrant.MatchAll, Hint: "需要开通会员",
				Rules: []itemgrant.ConditionRule{{Field: "user.member", Op: itemgrant.OpTrue}}}),
			n("l3", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "黄金"}, Threshold: 500}),
			n("p3", levels.NodePrivilege, levels.PrivilegeConfig{NodeMeta: levels.NodeMeta{Label: "金色徽章"}, Kind: levels.PrivilegeCustom, Key: "gold_badge", Value: 1}),
		},
		Edges: []levels.Edge{
			e("s", levels.HandleNext, "l1"), e("l1", levels.HandleAttach, "r1"), e("l1", levels.HandleNext, "l2"),
			e("l2", levels.HandleAttach, "r2"), e("l2", levels.HandleAttach, "p2"), e("l2", levels.HandleNext, "g3"),
			e("g3", levels.HandleNext, "l3"), e("l3", levels.HandleAttach, "p3"),
		},
	}
	saved, err := svc.SaveDraft(ctx, appID, graph, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if saved.IsDefault || !saved.Dirty || saved.Summary.Levels != 3 || saved.Summary.Gates != 1 {
		t.Fatalf("草稿：%+v", saved.Summary)
	}
	if _, err := svc.SaveDraft(ctx, appID, graph, saved.DraftRevision-1, "other"); errCode(err) != 40900 {
		t.Fatalf("过期的修订号应被拒绝（40900），得到 %v", err)
	}
	published, err := svc.Publish(ctx, LevelPublishInput{AppID: appID, Graph: graph, Note: "首版", Operator: "admin"})
	if err != nil || published.Version != 1 {
		t.Fatalf("发布：%+v %v", published, err)
	}

	// ── 升级：自动奖励后台到账、手动奖励待领取、升级通知 ──
	addExp(alice, 150)
	info := profile(alice)
	if info.CurrentLevel != 2 || info.CurrentLevelName != "白银" || info.ClaimableRewards != 1 || info.ExpMultiplier != 1.5 {
		t.Fatalf("150 经验应是 Lv.2 白银、1 份待领取、经验 ×1.5，得到 %+v", info)
	}
	svc.tick(ctx)
	var integral int64
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, alice).Scan(&integral); err != nil || integral != 10 {
		t.Fatalf("Lv.1 自动奖励应到账 10 积分，得到 %d %v", integral, err)
	}
	var aliceNotice *notificationdomain.DeliverInput
	for i := range notifier.items {
		if notifier.items[i].UserID == alice {
			aliceNotice = &notifier.items[i]
		}
	}
	if aliceNotice == nil || !strings.Contains(aliceNotice.Title, "Lv.2 白银") ||
		!strings.Contains(aliceNotice.Content, "白银礼包") || !strings.Contains(aliceNotice.Content, "10 积分") {
		t.Fatalf("升级通知：%+v", notifier.items)
	}
	session := &authdomain.Session{AppID: appID, UserID: alice}
	mine, err := svc.MyRewards(ctx, session, levels.RewardClaimable)
	if err != nil || len(mine) != 1 || mine[0].Label != "白银礼包" {
		t.Fatalf("待领取：%+v %v", mine, err)
	}
	claimed, err := svc.ClaimReward(ctx, session, mine[0].ID)
	if err != nil || claimed.Status != levels.RewardGranted {
		t.Fatalf("领取：%+v %v", claimed, err)
	}
	if _, err := svc.ClaimReward(ctx, session, mine[0].ID); errCode(err) != 40900 {
		t.Fatalf("重复领取应是 40900，得到 %v", err)
	}
	if _, err := svc.ClaimReward(ctx, &authdomain.Session{AppID: appID, UserID: bob}, mine[0].ID); errCode(err) != 40400 {
		t.Fatalf("别人的奖励应是 40400，得到 %v", err)
	}

	// ── 晋级条件：经验够了但不是会员 → 停在 Lv.2；开通后补判升到 Lv.3 ──
	addExp(alice, 1000)
	info = profile(alice)
	if info.CurrentLevel != 2 || info.BlockedBy == nil || info.BlockedBy.Hint != "需要开通会员" {
		t.Fatalf("非会员应卡在 Lv.2 的晋级条件上，得到 %+v", info)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET vip_lifetime_at = NOW() WHERE id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	info = profile(alice)
	if info.CurrentLevel != 3 || info.BlockedBy != nil || !info.IsMaxLevel {
		t.Fatalf("开通会员后应补判到 Lv.3，得到 %+v", info)
	}
	hasBadge := false
	for _, p := range info.Privileges {
		if p.Kind == levels.PrivilegeCustom && p.Key == "gold_badge" {
			hasBadge = true
		}
	}
	if !hasBadge {
		t.Fatalf("Lv.3 应享有 gold_badge：%+v", info.Privileges)
	}
	if exp, integralMul, err := pg.LevelMultipliers(ctx, appID, alice, 0); err != nil || exp != 1.5 || integralMul != 1 {
		t.Fatalf("倍率：%v %v %v", exp, integralMul, err)
	}

	// ── 管理员调级：bob 越过门槛与条件到 Lv.3 ──
	adjusted, err := svc.AdjustUser(ctx, LevelAdjustInput{AppID: appID, UserID: bob, Level: 3, Note: "活动补偿", Operator: "admin"})
	if err != nil || adjusted.To != 3 || adjusted.User.Level != 3 {
		t.Fatalf("调级：%+v %v", adjusted, err)
	}
	events, err := svc.ListEvents(ctx, levels.EventQuery{AppID: appID, UserID: bob, Reason: levels.ReasonAdmin})
	if err != nil || len(events.Items) != 1 || events.Items[0].Note != "活动补偿" || events.Items[0].ToName != "黄金" {
		t.Fatalf("调级记录：%+v %v", events, err)
	}

	// ── 新体系：门槛整体调高，重算并允许降级 ──
	for i, node := range graph.Nodes {
		if node.ID == "l2" {
			graph.Nodes[i].Data = raw(levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "白银"}, Threshold: 5000})
		}
		if node.ID == "l3" {
			graph.Nodes[i].Data = raw(levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "黄金"}, Threshold: 9000})
		}
	}
	second, err := svc.Publish(ctx, LevelPublishInput{AppID: appID, Graph: graph, Operator: "admin", Recalc: true, Downgrade: true})
	if err != nil || second.Version != 2 || second.Job == nil {
		t.Fatalf("第二版发布与重算：%+v %v", second, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	var job *levels.Job
	for time.Now().Before(deadline) {
		job, err = svc.GetJob(ctx, appID, second.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != levels.JobRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.Status != levels.JobSucceeded || job.Processed != 3 || job.Changed != 3 {
		t.Fatalf("重算任务：%+v", job)
	}
	if info := profile(alice); info.CurrentLevel != 1 {
		t.Fatalf("允许降级的重算后 alice（1150 经验）应回到 Lv.1，得到 %d", info.CurrentLevel)
	}

	// ── 补发：新挂在 Lv.1 上的奖励补给已有等级记录的三个人 ──
	graph.Nodes = append(graph.Nodes, n("r1b", levels.NodeReward, levels.RewardConfig{NodeMeta: levels.NodeMeta{Label: "回馈礼"},
		Items: []itemgrant.Item{{Type: itemgrant.ItemPoints, Amount: 5}}}))
	graph.Edges = append(graph.Edges, e("l1", levels.HandleAttach, "r1b"))
	if _, err := svc.Publish(ctx, LevelPublishInput{AppID: appID, Graph: graph, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.Backfill(ctx, appID, "r1b")
	if err != nil || created != 3 {
		t.Fatalf("补发应给 3 人各写一条，得到 %d %v", created, err)
	}
	if again, _ := svc.Backfill(ctx, appID, "r1b"); again != 0 {
		t.Fatalf("重复补发不应再写，得到 %d", again)
	}
	svc.tick(ctx)
	rewards, err := svc.ListRewards(ctx, levels.RewardQuery{AppID: appID, NodeID: "r1b"})
	if err != nil || len(rewards.Items) != 3 || rewards.Items[0].Status != levels.RewardGranted || rewards.Items[0].Origin != levels.OriginBackfill {
		t.Fatalf("补发记录：%+v %v", rewards, err)
	}

	// ── 版本与恢复 ──
	versions, err := svc.ListVersions(ctx, appID)
	if err != nil || len(versions) != 3 || versions[2].Note != "首版" || versions[2].Summary.Levels != 3 {
		t.Fatalf("版本：%+v %v", versions, err)
	}
	restored, err := svc.RestoreVersion(ctx, appID, 1, "admin")
	if err != nil || !restored.Dirty || restored.PublishedVersion != 3 {
		t.Fatalf("恢复：%+v %v", restored, err)
	}

	// ── 推演、总览与排名 ──
	sim, err := svc.Simulate(ctx, LevelSimulateInput{AppID: appID, User: "level-alice"})
	if err != nil || sim.Level != 1 || sim.Experience != 1150 {
		t.Fatalf("单人推演：%+v %v", sim, err)
	}
	overview, err := svc.Overview(ctx, appID, 7)
	if err != nil || overview.TotalUsers != 3 || overview.Version != 3 || len(overview.Distribution) != 3 || overview.Distribution[0].Users != 3 {
		t.Fatalf("总览：%+v %v", overview, err)
	}
	ranking, err := pg.GetLevelRankings(ctx, appID, 1, 10, alice)
	if err != nil || len(ranking.Items) != 3 || ranking.Items[0].LevelName == "" || ranking.MyRank == nil {
		t.Fatalf("等级排名：%+v %v", ranking, err)
	}
	history, err := svc.MyHistory(ctx, session, 1, 20)
	if err != nil || history.Total < 3 {
		t.Fatalf("我的升级记录：%+v %v", history, err)
	}
}
