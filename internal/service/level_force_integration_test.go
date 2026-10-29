package service

import (
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/itemgrant"
	"aegis/internal/domain/levels"
	pgrepo "aegis/internal/repository/postgres"
)

// 强制升级（docs/levels.md「强制升级」）：越过经验门槛与晋级条件、锁定保底扛住「重算并允许降级」、
// 不发奖励与不发通知、批量逐人执行、解除锁定。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55438/postgres go test ./internal/service -run TestLevelForceUpgradeIntegration
func TestLevelForceUpgradeIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	notifier := &captureNotifier{}
	svc := NewLevelService(zap.NewNop(), pg)
	svc.SetNotifier(notifier)
	const appID = int64(40701)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'levels-force') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	dora := insertTestUser(t, ctx, pool, appID, "force-dora", "0")
	evan := insertTestUser(t, ctx, pool, appID, "force-evan", "0")
	finn := insertTestUser(t, ctx, pool, appID, "force-finn", "0")

	// L1(0) 自动奖励 → L2(100) 待领取奖励 → 晋级条件（会员）→ L3(500) 自动奖励
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	n := func(id, typ string, data any) levels.Node { return levels.Node{ID: id, Type: typ, Data: raw(data)} }
	e := func(src, handle, dst string) levels.Edge {
		return levels.Edge{ID: src + ":" + handle + "->" + dst, Source: src, SourceHandle: handle, Target: dst}
	}
	points := []itemgrant.Item{{Type: itemgrant.ItemPoints, Amount: 10}}
	graph := levels.Graph{
		Nodes: []levels.Node{
			n("s", levels.NodeScheme, levels.SchemeConfig{NodeMeta: levels.NodeMeta{Label: "强制升级"}, Notify: true,
				NotifyTitle: "升到 Lv.{level} {name}", NotifyContent: "{rewards}"}),
			n("l1", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "青铜"}, Threshold: 0}),
			n("r1", levels.NodeReward, levels.RewardConfig{NodeMeta: levels.NodeMeta{Label: "见面礼"}, Items: points}),
			n("l2", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "白银"}, Threshold: 100}),
			n("r2", levels.NodeReward, levels.RewardConfig{NodeMeta: levels.NodeMeta{Label: "白银礼包"}, Claim: levels.ClaimManual, Items: points}),
			n("g3", levels.NodeGate, levels.GateConfig{Mode: itemgrant.MatchAll, Hint: "需要开通会员",
				Rules: []itemgrant.ConditionRule{{Field: "user.member", Op: itemgrant.OpTrue}}}),
			n("l3", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "黄金"}, Threshold: 500}),
			n("r3", levels.NodeReward, levels.RewardConfig{NodeMeta: levels.NodeMeta{Label: "黄金礼"}, Items: points}),
		},
		Edges: []levels.Edge{
			e("s", levels.HandleNext, "l1"), e("l1", levels.HandleAttach, "r1"), e("l1", levels.HandleNext, "l2"),
			e("l2", levels.HandleAttach, "r2"), e("l2", levels.HandleNext, "g3"), e("g3", levels.HandleNext, "l3"),
			e("l3", levels.HandleAttach, "r3"),
		},
	}
	if _, err := svc.Publish(ctx, LevelPublishInput{AppID: appID, Graph: graph, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	rewardsOf := func(userID int64) int {
		t.Helper()
		page, err := svc.ListRewards(ctx, levels.RewardQuery{AppID: appID, UserID: userID})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Items)
	}
	noticesOf := func(userID int64) int {
		notifier.mu.Lock()
		defer notifier.mu.Unlock()
		count := 0
		for _, item := range notifier.items {
			if item.UserID == userID {
				count++
			}
		}
		return count
	}

	// ── 参数校验 ──
	if _, err := svc.AdjustUser(ctx, LevelAdjustInput{AppID: appID, UserID: dora, Level: 4}); errCode(err) != 40000 {
		t.Fatalf("超过最高级应是 40000，得到 %v", err)
	}
	if _, err := svc.AdjustUser(ctx, LevelAdjustInput{AppID: appID, UserID: dora, Level: 2, Lock: true, Unlock: true}); errCode(err) != 40000 {
		t.Fatalf("同时锁定与解锁应是 40000，得到 %v", err)
	}
	if _, err := svc.AdjustUsers(ctx, LevelBatchAdjustInput{LevelAdjustInput: LevelAdjustInput{AppID: appID, Level: 2}}); errCode(err) != 40000 {
		t.Fatalf("批量没有用户应是 40000，得到 %v", err)
	}

	// ── 单人：0 经验、不是会员，强制到 Lv.3 并锁定；经过的三级奖励照发、升级通知照发 ──
	forced, err := svc.AdjustUser(ctx, LevelAdjustInput{AppID: appID, UserID: dora, Level: 3, Lock: true, Note: "内测用户", Operator: "admin"})
	if err != nil || forced.From != 1 || forced.To != 3 || forced.User.Level != 3 || forced.User.LockedLevel != 3 || forced.AddExperience != 0 {
		t.Fatalf("强制升级：%+v %v", forced, err)
	}
	if got := rewardsOf(dora); got != 3 {
		t.Fatalf("强制升级经过三级，应写 3 条达成奖励，得到 %d", got)
	}
	svc.tick(ctx)
	if noticesOf(dora) == 0 {
		t.Fatal("强制升级应发升级通知")
	}

	// ── 批量：不发奖励、不发通知；不存在的人单独失败 ──
	batch, err := svc.AdjustUsers(ctx, LevelBatchAdjustInput{
		LevelAdjustInput: LevelAdjustInput{AppID: appID, Level: 3, SkipRewards: true, Silent: true, Operator: "admin"},
		UserIDs:          []int64{evan, finn, evan, 987654321},
	})
	if err != nil || batch.Total != 3 || batch.Succeeded != 2 || batch.Changed != 2 || batch.Failed != 1 || batch.Items[2].Error != "用户不存在" {
		t.Fatalf("批量强制升级：%+v %v", batch, err)
	}
	svc.tick(ctx)
	if got := rewardsOf(evan); got != 0 {
		t.Fatalf("不发奖励时不应写达成奖励，得到 %d", got)
	}
	if noticesOf(evan) != 0 {
		t.Fatal("不发通知时不应有升级通知")
	}
	events, err := svc.ListEvents(ctx, levels.EventQuery{AppID: appID, UserID: evan, Reason: levels.ReasonAdmin})
	if err != nil || len(events.Items) != 1 || events.Items[0].ToLevel != 3 {
		t.Fatalf("不发通知也要写升级记录：%+v %v", events, err)
	}

	// ── 重算并允许降级：锁定的 dora 留在 Lv.3，没锁定的 evan、finn 回到 Lv.1 ──
	republished, err := svc.Publish(ctx, LevelPublishInput{AppID: appID, Graph: graph, Operator: "admin", Recalc: true, Downgrade: true})
	if err != nil || republished.Job == nil {
		t.Fatalf("重算：%+v %v", republished, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		job, err := svc.GetJob(ctx, appID, republished.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != levels.JobRunning {
			if job.Status != levels.JobSucceeded || job.Changed != 2 {
				t.Fatalf("重算任务：%+v", job)
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	levelOf := func(userID int64) levels.UserLevel {
		t.Helper()
		detail, err := svc.GetUser(ctx, appID, userID)
		if err != nil {
			t.Fatal(err)
		}
		return detail.User
	}
	if user := levelOf(dora); user.Level != 3 || user.LockedLevel != 3 {
		t.Fatalf("锁定的等级不应被重算降掉：%+v", user)
	}
	if user := levelOf(evan); user.Level != 1 {
		t.Fatalf("没锁定的应按经验降回 Lv.1：%+v", user)
	}

	// ── 解除锁定：等级不变，之后的降级重算才会生效 ──
	unlocked, err := svc.AdjustUser(ctx, LevelAdjustInput{AppID: appID, UserID: dora, Unlock: true, Operator: "admin"})
	if err != nil || unlocked.To != 3 || unlocked.User.LockedLevel != 0 {
		t.Fatalf("解除锁定：%+v %v", unlocked, err)
	}
}
