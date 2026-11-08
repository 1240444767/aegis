package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/invite"
	"aegis/internal/domain/leaderboard"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/timeutil"
)

// 积分排行榜的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「未开启 → 设置（档位与引用核对）→ 周期口径（退款、排行榜奖励、管理员调整不计入；抽奖类内部调整计入）→
// 并列名次与先到先得 → 封禁与移出 → 匿名展示与不允许匿名 → 账号脱敏 → topLimit 与分页、minValue →
// 经验榜与等级榜 → 结算快照（并列落档、匿名冻结）→ 发奖与幂等（同一期跑两次只发一次）→
// 发放时被移出记 skipped、功能停用记 failed → 重试 → 手动结算（未结束报错、已结算原样返回）→
// 往期列表与详情、我的奖励记录、管理端榜单与统计」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLeaderboardIntegration
func TestLeaderboardIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	svc := NewLeaderboardService(zap.NewNop(), pg, nil)
	const appID = int64(32000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'leaderboard-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "online_music", Name: ptrTo("在线音乐")}); err != nil {
		t.Fatal(err)
	}

	// 「现在」固定在本周三中午：奖励流水用的是数据库的 NOW()，与它落在同一周
	loc := timeutil.DefaultLocation()
	week := leaderboard.PeriodAt(leaderboard.PeriodWeek, time.Now(), loc)
	now := week.Start.AddDate(0, 0, 2).Add(12 * time.Hour)
	svc.SetClockForTest(func() time.Time { return now })
	prevWeek := week.Previous()

	user := func(account, nickname string) int64 {
		t.Helper()
		id := insertTestUser(t, ctx, pool, appID, account, "0")
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, nickname) VALUES ($1, NULLIF($2, ''))`, id, nickname); err != nil {
			t.Fatal(err)
		}
		return id
	}
	seq := 0
	// earn 直接落一笔积分流水（并加到余额上）：sourceType / sourceID 用来区分管理员调整与抽奖类内部调整
	earn := func(userID, amount int64, category string, at time.Time, sourceType string, sourceID *int64) {
		t.Helper()
		seq++
		if _, err := pool.Exec(ctx, `INSERT INTO integral_transactions (transaction_no, user_id, appid, type, category, amount,
    balance_before, balance_after, status, title, source_type, source_id, created_at, updated_at)
VALUES ($1, $2, $3, 'earn', $4, $5, 0, 0, 'completed', 'test', NULLIF($6, ''), $7, $8, $8)`,
			"LBT"+time.Now().Format("150405")+"-"+strconv.Itoa(seq), userID, appID, category, amount, sourceType, sourceID, at); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET integral = integral + $2 WHERE id = $1`, userID, amount); err != nil {
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
	expectCode := func(name string, err error, want int) {
		t.Helper()
		if errCode(err) != want {
			t.Fatalf("%s：期望错误码 %d，得到 %v", name, want, err)
		}
	}
	save := func(enabled bool, mutate func(*leaderboard.Config)) {
		t.Helper()
		settings, err := pg.GetLeaderboardSettings(ctx, appID)
		if err != nil {
			t.Fatal(err)
		}
		cfg := settings.Config
		mutate(&cfg)
		if _, err := svc.SaveSettings(ctx, appID, enabled, cfg, "admin"); err != nil {
			t.Fatalf("保存设置失败：%v", err)
		}
	}
	board := func(viewer int64, key string, page, limit int) *leaderboard.ClientBoardPage {
		t.Helper()
		result, err := svc.Board(ctx, appID, viewer, key, page, limit)
		if err != nil {
			t.Fatalf("读榜 %s 失败：%v", key, err)
		}
		return result
	}
	names := func(rows []leaderboard.ClientRow) string {
		parts := []string{}
		for _, row := range rows {
			parts = append(parts, row.Name+"#"+strconv.Itoa(row.Rank)+"="+strconv.Itoa(int(row.Value)))
		}
		return strings.Join(parts, ",")
	}

	alice, bob := user("alice@example.com", "小A"), user("bob", "小B")
	carol := user("13812345678", "")
	dave, erin, frank := user("dave", "阿D"), user("erin", "阿E"), user("frank", "阿F")
	adminID := int64(1)
	day := func(offsetDays int, hour int) time.Time {
		return week.Start.AddDate(0, 0, offsetDays).Add(time.Duration(hour) * time.Hour)
	}

	// ── 未开启 ──
	home, err := svc.Boards(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if home.Enabled || len(home.Boards) != 0 || !home.Me.AllowAnonymous {
		t.Fatalf("未开启时应 enabled=false、boards=[]：%+v", home)
	}
	_, err = svc.Board(ctx, appID, alice, leaderboard.BoardPointsWeekly, 1, 20)
	expectCode("未开启时读榜", err, errCodeLeaderboardDisabled)

	// ── 设置：档位与引用在保存时核对 ──
	bad := leaderboard.DefaultConfig()
	bad.Rewards = map[string]leaderboard.BoardRewards{leaderboard.BoardPointsWeekly: {Enabled: true, Tiers: []leaderboard.Tier{
		{RankFrom: 1, RankTo: 1, Rewards: []leaderboard.Reward{{Type: pointstore.RewardFeature, Tag: "missing", Amount: 7}}}}}}
	_, err = svc.SaveSettings(ctx, appID, true, bad, "admin")
	expectCode("未登记的功能", err, 40000)
	bad.Rewards = map[string]leaderboard.BoardRewards{leaderboard.BoardPointsTotal: {Enabled: true, Tiers: []leaderboard.Tier{
		{RankFrom: 1, RankTo: 1, Rewards: []leaderboard.Reward{{Type: invite.RewardPoints, Amount: 1}}}}}}
	_, err = svc.SaveSettings(ctx, appID, true, bad, "admin")
	expectCode("总榜配奖励", err, 40000)

	save(true, func(cfg *leaderboard.Config) {
		cfg.Rewards = map[string]leaderboard.BoardRewards{leaderboard.BoardPointsWeekly: {Enabled: true, Tiers: []leaderboard.Tier{
			{RankFrom: 1, RankTo: 1, Rewards: []leaderboard.Reward{{Type: invite.RewardPoints, Amount: 500}}},
			{RankFrom: 2, RankTo: 4, Rewards: []leaderboard.Reward{{Type: pointstore.RewardFeature, Tag: "online_music", Amount: 7}}},
		}}}
	})
	home, err = svc.Boards(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if !home.Enabled || len(home.Boards) != 3 || home.Boards[1].Key != leaderboard.BoardPointsWeekly ||
		home.Boards[1].PeriodKey != week.Key || home.Boards[1].PeriodLabel != "本周" || len(home.Boards[1].Rewards) != 2 ||
		home.Boards[1].Rewards[0].Label != "第 1 名" || home.Boards[0].PeriodStart != nil {
		t.Fatalf("首页不对：%+v", home)
	}

	// ── 本周的流水 ──
	earn(bob, 100, "rewarded_ad", day(0, 9), "rewarded_ad", nil)
	earn(alice, 100, "daily_signin", day(0, 10), "daily_signin", nil)
	earn(alice, 500, pointstore.IntegralCategoryRefund, day(0, 11), "points_store", nil) // 兑换取消退回：不是「获得」
	earn(alice, 1000, "admin_adjust", day(1, 9), "admin_manual", &adminID)               // 管理员调整：默认不计入
	earn(carol, 50, "invite", day(1, 10), "invite", nil)
	earn(carol, 999, leaderboard.IntegralCategory, day(1, 11), "", nil) // 排行榜奖励：不滚雪球
	earn(dave, 30, "admin_adjust", day(1, 12), "admin_manual", nil)     // 抽奖奖励（没有操作管理员）：计入
	earn(erin, 80, "card_key", day(1, 13), "card_key", nil)
	earn(frank, 70, "purchase", day(1, 14), "payment_order", nil)
	earn(alice, 40, "daily_signin", prevWeek.Start.AddDate(0, 0, 1), "daily_signin", nil) // 上周：不进本周
	// erin 被封禁（仅限制登录一档也不上榜），frank 被移出
	if _, err := pool.Exec(ctx, `INSERT INTO user_account_bans (appid, user_id, ban_type, ban_scope, reason)
VALUES ($1, $2, 'permanent', 'login', 'test')`, appID, erin); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddExclusions(ctx, appID, []int64{frank}, "刷分", "admin"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddExclusions(ctx, appID, []int64{999999}, "", "admin")
	expectCode("移出不存在的用户", err, 40401)

	// ── 并列名次、先到先得、口径 ──
	weekly := board(alice, leaderboard.BoardPointsWeekly, 1, 20)
	if got := names(weekly.Items); got != "小B#1=100,小A#1=100,138****5678#3=50,阿D#4=30" {
		t.Fatalf("周榜不对：%s", got)
	}
	if weekly.Total != 4 || weekly.TotalPages != 1 || weekly.TopLimit != leaderboard.DefaultTopLimit {
		t.Fatalf("周榜分页不对：%+v", weekly)
	}
	if !weekly.Items[1].IsMe || weekly.Items[1].UserID == nil || weekly.Me.Rank == nil || *weekly.Me.Rank != 1 || weekly.Me.GapToPrev != 0 ||
		!weekly.Me.InTop || weekly.Me.Name != "小A" || weekly.Me.Value != 100 {
		t.Fatalf("我的名次不对：%+v", weekly.Me)
	}
	if me := board(carol, leaderboard.BoardPointsWeekly, 1, 20).Me; me.Rank == nil || *me.Rank != 3 || me.GapToPrev != 50 || me.Value != 50 {
		t.Fatalf("carol 的名次与差距不对：%+v", me)
	}
	if me := board(dave, leaderboard.BoardPointsWeekly, 1, 20).Me; me.Rank == nil || *me.Rank != 4 || me.GapToPrev != 20 {
		t.Fatalf("dave 的名次不对：%+v", me)
	}
	if me := board(frank, leaderboard.BoardPointsWeekly, 1, 20).Me; !me.Excluded || me.Rank != nil || me.Value != 70 {
		t.Fatalf("被移出的人应 excluded=true、rank=null：%+v", me)
	}
	if me := board(erin, leaderboard.BoardPointsWeekly, 1, 20).Me; me.Excluded || me.Rank != nil {
		t.Fatalf("被封禁的人不上榜：%+v", me)
	}
	if home, _ := svc.Boards(ctx, appID, frank); !home.Me.Excluded {
		t.Fatal("首页应标出被移出")
	}
	// 管理员调整计入
	save(true, func(cfg *leaderboard.Config) { cfg.CountAdminAdjust = true })
	if got := names(board(alice, leaderboard.BoardPointsWeekly, 1, 20).Items); !strings.HasPrefix(got, "小A#1=1100,小B#2=100") {
		t.Fatalf("计入管理员调整后：%s", got)
	}
	save(true, func(cfg *leaderboard.Config) { cfg.CountAdminAdjust = false })

	// ── 匿名 ──
	if _, err := svc.UpdatePreference(ctx, appID, bob, true); err != nil {
		t.Fatal(err)
	}
	weekly = board(alice, leaderboard.BoardPointsWeekly, 1, 20)
	if row := weekly.Items[0]; row.Name != leaderboard.AnonymousName || row.UserID != nil || row.AvatarOwner != 0 || !row.Anonymous {
		t.Fatalf("别人看匿名的人：%+v", row)
	}
	if row := board(bob, leaderboard.BoardPointsWeekly, 1, 20).Items[0]; row.Name != "小B" || !row.IsMe || !row.Anonymous || row.UserID == nil {
		t.Fatalf("自己看自己应显示真名：%+v", row)
	}
	save(true, func(cfg *leaderboard.Config) { cfg.AllowAnonymous = false })
	if row := board(alice, leaderboard.BoardPointsWeekly, 1, 20).Items[0]; row.Name != "小B" || row.Anonymous {
		t.Fatalf("不允许匿名时偏好不生效：%+v", row)
	}
	_, err = svc.UpdatePreference(ctx, appID, alice, true)
	expectCode("不允许匿名时开启", err, errCodeLeaderboardAnonymousForbidden)
	if _, err := svc.UpdatePreference(ctx, appID, alice, false); err != nil {
		t.Fatalf("不允许匿名时仍可以关：%v", err)
	}
	save(true, func(cfg *leaderboard.Config) { cfg.AllowAnonymous = true })

	// ── 账号永不出网 ──
	raw, _ := json.Marshal(board(dave, leaderboard.BoardPointsWeekly, 1, 20))
	if strings.Contains(string(raw), "13812345678") || strings.Contains(string(raw), "alice@example.com") {
		t.Fatalf("用户端返回了原始账号：%s", raw)
	}
	save(true, func(cfg *leaderboard.Config) { cfg.Identity = leaderboard.IdentityMasked })
	if got := names(board(dave, leaderboard.BoardPointsWeekly, 1, 20).Items); !strings.Contains(got, "al***@e***.com#1") {
		t.Fatalf("masked 应一律脱敏：%s", got)
	}
	save(true, func(cfg *leaderboard.Config) { cfg.Identity = leaderboard.IdentityNickname })

	// ── 总榜：topLimit 与分页、minValue ──
	small := []int64{}
	for i := 1; i <= 8; i++ {
		id := user("p0"+strconv.Itoa(i), "")
		if _, err := pool.Exec(ctx, `UPDATE users SET integral = $2 WHERE id = $1`, id, i); err != nil {
			t.Fatal(err)
		}
		small = append(small, id)
	}
	save(true, func(cfg *leaderboard.Config) { cfg.TopLimit = 10 })
	total := board(small[0], leaderboard.BoardPointsTotal, 3, 4)
	// 参与者：alice 1640、carol 1049、bob 100、dave 30 + p01..p08（erin 封禁、frank 移出）
	if total.Total != 12 || total.TotalPages != 3 || len(total.Items) != 2 || total.Items[0].Rank != 9 || total.Items[1].Rank != 10 {
		t.Fatalf("总榜分页不对：total=%d pages=%d items=%s", total.Total, total.TotalPages, names(total.Items))
	}
	if total.Me.Rank == nil || *total.Me.Rank != 12 || total.Me.InTop || total.Me.GapToPrev != 1 {
		t.Fatalf("榜外的我：%+v", total.Me)
	}
	if empty := board(small[0], leaderboard.BoardPointsTotal, 4, 4); len(empty.Items) != 0 || empty.Total != 12 {
		t.Fatalf("超出 topLimit 的页应为空：%+v", empty)
	}
	if first := board(alice, leaderboard.BoardPointsTotal, 1, 2); names(first.Items) != "小A#1=1640,138****5678#2=1049" {
		t.Fatalf("总榜前两名：%s", names(first.Items))
	}
	save(true, func(cfg *leaderboard.Config) { cfg.MinValue = 10 })
	if res := board(small[0], leaderboard.BoardPointsTotal, 1, 20); res.Total != 4 || res.Me.Rank != nil || res.Me.Value != 1 {
		t.Fatalf("minValue 之下不上榜：total=%d me=%+v", res.Total, res.Me)
	}
	save(true, func(cfg *leaderboard.Config) { cfg.MinValue = 1; cfg.TopLimit = leaderboard.DefaultTopLimit })

	// ── 经验榜与等级榜 ──
	if _, err := pool.Exec(ctx, `UPDATE users SET experience = 500 WHERE id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	save(true, func(cfg *leaderboard.Config) {
		for i := range cfg.Boards {
			if cfg.Boards[i].Key == leaderboard.BoardExperience || cfg.Boards[i].Key == leaderboard.BoardLevel {
				cfg.Boards[i].Enabled = true
			}
		}
	})
	if exp := board(alice, leaderboard.BoardExperience, 1, 5); len(exp.Items) == 0 || exp.Items[0].Value != 500 ||
		!strings.HasPrefix(exp.Items[0].LevelName, "Lv.") || exp.Me.LevelName == "" {
		t.Fatalf("经验榜：%+v", exp)
	}
	// 榜上给的是对外 ID，不是主键（docs/user-public-id.md）
	alicePublic := svc.publicIDs.Of(ctx, alice)
	if lvl := board(alice, leaderboard.BoardLevel, 1, 5); len(lvl.Items) == 0 || lvl.Items[0].UserID == nil || alicePublic < userPublicIDMin ||
		*lvl.Items[0].UserID != alicePublic ||
		lvl.Items[0].LevelName == "" {
		t.Fatalf("等级榜：%+v", lvl)
	}
	_, err = svc.Board(ctx, appID, alice, leaderboard.BoardPointsDaily, 1, 20)
	expectCode("未开启的榜", err, errCodeLeaderboardNotFound)
	_, err = svc.Board(ctx, appID, alice, "nope", 1, 20)
	expectCode("不存在的榜", err, errCodeLeaderboardNotFound)

	// ── 管理端看榜：真实账号，标出被移出 ──
	adminBoard, err := svc.AdminBoard(ctx, appID, leaderboard.BoardPointsWeekly, "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	var frankRow *leaderboard.AdminEntry
	for i := range adminBoard.Items {
		if adminBoard.Items[i].UserID == frank {
			frankRow = &adminBoard.Items[i]
		}
		if adminBoard.Items[i].UserID == erin {
			t.Fatal("管理端也不该列出封禁的人")
		}
	}
	if adminBoard.Source != "live" || adminBoard.Total != 4 || frankRow == nil || !frankRow.Excluded || frankRow.Rank != nil ||
		frankRow.Account != "frank" || adminBoard.Items[0].Account != "bob" || !adminBoard.Items[0].Anonymous {
		t.Fatalf("管理端榜单不对：%+v", adminBoard)
	}

	// ── 上周的流水，然后结算 ──
	earn(alice, 40, "daily_signin", prevWeek.Start.AddDate(0, 0, 2), "daily_signin", nil) // alice 上周共 80
	earn(bob, 80, "rewarded_ad", prevWeek.Start.AddDate(0, 0, 3), "rewarded_ad", nil)
	earn(carol, 10, "invite", prevWeek.Start.AddDate(0, 0, 3), "invite", nil)
	earn(dave, 5, "rewarded_ad", prevWeek.Start.AddDate(0, 0, 4), "rewarded_ad", nil)
	earn(frank, 999, "rewarded_ad", prevWeek.Start.AddDate(0, 0, 4), "rewarded_ad", nil) // 被移出：不进快照

	_, _, err = svc.Settle(ctx, appID, leaderboard.BoardPointsWeekly, week.Key, "admin")
	expectCode("本周未结束", err, errCodeLeaderboardPeriodNotEnded)
	_, _, err = svc.Settle(ctx, appID, leaderboard.BoardPointsWeekly, "2026-43", "admin")
	expectCode("期号格式不对", err, 40000)
	_, _, err = svc.Settle(ctx, appID, leaderboard.BoardPointsTotal, "total", "admin")
	expectCode("总榜不结算", err, 40000)

	settled, err := svc.SettleDue(ctx)
	if err != nil || settled < 1 {
		t.Fatalf("应结算上一周：settled=%d err=%v", settled, err)
	}
	snapshot, err := pg.LeaderboardSnapshotByPeriod(ctx, appID, leaderboard.BoardPointsWeekly, prevWeek.Key)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != leaderboard.SnapshotSettled || snapshot.Participants != 4 || len(snapshot.Tiers) != 2 || snapshot.SettledBy != "system" {
		t.Fatalf("快照不对：%+v", snapshot)
	}
	entries, err := pg.ListLeaderboardSnapshotEntries(ctx, snapshot.ID, "", 1, 20, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	// bob 80（周三）与 alice 80（周二）并列第 1：alice 先到在前；两人都拿第 1 名档
	if len(entries.Items) != 4 || entries.Items[0].UserID != alice || entries.Items[1].UserID != bob ||
		entries.Items[0].Rank != 1 || entries.Items[1].Rank != 1 || entries.Items[2].Rank != 3 || entries.Items[3].Rank != 4 {
		t.Fatalf("快照条目不对：%+v", entries.Items)
	}
	for _, entry := range entries.Items {
		if entry.RewardStatus != leaderboard.RewardPending || len(entry.RewardResults) != 1 {
			t.Fatalf("应发奖励没写入：%+v", entry)
		}
	}
	if !entries.Items[1].Anonymous || entries.Items[1].RawAvatar != "" || entries.Items[1].DisplayName != "小B" {
		t.Fatalf("匿名应在结算时冻结：%+v", entries.Items[1])
	}
	again, err := svc.SettleDue(ctx)
	if err != nil || again != 0 {
		t.Fatalf("同一期不该再结算：%d %v", again, err)
	}

	// 结算后、发放前：dave 被移出、功能被停用
	if _, err := svc.AddExclusions(ctx, appID, []int64{dave}, "测试号", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "online_music", IsActive: ptrTo(false)}); err != nil {
		t.Fatal(err)
	}
	aliceBefore, bobBefore := integral(alice), integral(bob)
	processed, err := svc.GrantPending(ctx, 50)
	if err != nil || processed != 4 {
		t.Fatalf("发放：processed=%d err=%v", processed, err)
	}
	if integral(alice) != aliceBefore+500 || integral(bob) != bobBefore+500 {
		t.Fatalf("第 1 名（并列）都应拿 500 积分：alice %d→%d bob %d→%d", aliceBefore, integral(alice), bobBefore, integral(bob))
	}
	statusOf := func(userID int64) *leaderboard.SnapshotEntry {
		t.Helper()
		entry, err := pg.LeaderboardEntryOf(ctx, snapshot.ID, userID)
		if err != nil || entry == nil {
			t.Fatalf("读条目失败：%v", err)
		}
		return entry
	}
	if entry := statusOf(alice); entry.RewardStatus != leaderboard.RewardGranted || entry.RewardResults[0].TransactionNo == "" || entry.GrantedAt == nil {
		t.Fatalf("alice 应已发放：%+v", entry)
	}
	if entry := statusOf(carol); entry.RewardStatus != leaderboard.RewardFailed || entry.RewardError == "" {
		t.Fatalf("功能停用时应记失败：%+v", entry)
	}
	if entry := statusOf(dave); entry.RewardStatus != leaderboard.RewardSkipped || !strings.Contains(entry.RewardError, "移出") {
		t.Fatalf("发放时已被移出应记 skipped：%+v", entry)
	}
	var category string
	if err := pool.QueryRow(ctx, `SELECT category FROM integral_transactions WHERE user_id = $1 AND amount = 500 AND source_type = 'leaderboard'`,
		alice).Scan(&category); err != nil || category != leaderboard.IntegralCategory {
		t.Fatalf("奖励积分的流水分类应为 leaderboard：%q %v", category, err)
	}
	// 幂等：再跑一轮，什么都不发
	if _, _, err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var grants int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM integral_transactions WHERE appid = $1 AND category = 'leaderboard' AND source_type = 'leaderboard'`,
		appID).Scan(&grants); err != nil || grants != 2 || integral(alice) != aliceBefore+500 {
		t.Fatalf("同一期只发一次：grants=%d alice=%d", grants, integral(alice))
	}
	// 奖励积分不滚进本周的周榜
	if got := names(board(alice, leaderboard.BoardPointsWeekly, 1, 20).Items); !strings.Contains(got, "小A#1=100") {
		t.Fatalf("奖励积分不该计入周榜：%s", got)
	}
	// 手动结算已结算的一期：原样返回
	existing, created, err := svc.Settle(ctx, appID, leaderboard.BoardPointsWeekly, prevWeek.Key, "admin")
	if err != nil || created || existing.ID != snapshot.ID {
		t.Fatalf("已结算的一期应原样返回：%+v %v %v", existing, created, err)
	}

	// ── 重试失败的条目 ──
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "online_music", IsActive: ptrTo(true)}); err != nil {
		t.Fatal(err)
	}
	retried, err := svc.RetrySnapshot(ctx, appID, snapshot.ID)
	if err != nil || retried != 1 {
		t.Fatalf("重试：%d %v", retried, err)
	}
	if processed, err := svc.GrantPending(ctx, 50); err != nil || processed != 1 {
		t.Fatalf("重试后发放：%d %v", processed, err)
	}
	if entry := statusOf(carol); entry.RewardStatus != leaderboard.RewardGranted || entry.RewardError != "" {
		t.Fatalf("重试后应已发放：%+v", entry)
	}
	var unlocks int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_feature_unlocks WHERE user_id = $1 AND source = 'leaderboard'`, carol).Scan(&unlocks); err != nil || unlocks != 1 {
		t.Fatalf("功能解锁应只发一次：%d %v", unlocks, err)
	}
	_, err = svc.RetrySnapshot(ctx, appID, 999999)
	expectCode("重试不存在的快照", err, errCodeLeaderboardSnapshotNotFound)

	// ── 往期与奖励记录（用户端）──
	periods, err := svc.Periods(ctx, appID, alice, leaderboard.BoardPointsWeekly, 12)
	if err != nil || len(periods.Items) != 1 || periods.Items[0].PeriodKey != prevWeek.Key || periods.Items[0].My == nil ||
		periods.Items[0].My.Rank != 1 || periods.Items[0].My.RewardStatus != leaderboard.RewardGranted ||
		!strings.Contains(periods.Items[0].PeriodLabel, "周") {
		t.Fatalf("往期列表：%+v %v", periods, err)
	}
	if other, _ := svc.Periods(ctx, appID, small[0], leaderboard.BoardPointsWeekly, 12); other.Items[0].My != nil {
		t.Fatal("没上过这一期的人不该有 my")
	}
	detail, err := svc.Period(ctx, appID, alice, leaderboard.BoardPointsWeekly, prevWeek.Key, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Items) != 4 || detail.Items[1].Name != leaderboard.AnonymousName || detail.Items[1].UserID != nil ||
		detail.Me == nil || detail.Me.Rank != 1 || len(detail.Rewards) != 2 || detail.Period.Participants != 4 {
		t.Fatalf("往期详情：%+v", detail)
	}
	_, err = svc.Period(ctx, appID, alice, leaderboard.BoardPointsWeekly, week.Key, 1, 20)
	expectCode("未结算的一期", err, errCodeLeaderboardSnapshotNotFound)
	rewards, err := svc.MyRewards(ctx, appID, alice, 1, 20)
	if err != nil || rewards.Total != 1 || rewards.Items[0].Status != leaderboard.RewardGranted || rewards.Items[0].BoardLabel != "积分周榜" ||
		len(rewards.Items[0].Rewards) != 1 || rewards.Items[0].Rewards[0].TransactionNo == "" {
		t.Fatalf("我的奖励记录：%+v %v", rewards, err)
	}

	// ── 管理端：结算记录、明细、往期榜、概览、移回 ──
	snapshots, err := svc.ListSnapshots(ctx, appID, leaderboard.BoardPointsWeekly, 1, 20)
	if err != nil || snapshots.Total != 1 || snapshots.Items[0].Stats == nil || snapshots.Items[0].Stats.Granted != 3 ||
		snapshots.Items[0].Stats.Skipped != 1 || snapshots.Items[0].Stats.Rewarded != 4 {
		t.Fatalf("结算记录：%+v %v", snapshots, err)
	}
	if detail, err := svc.SnapshotEntries(ctx, appID, snapshot.ID, leaderboard.RewardSkipped, 1, 20); err != nil ||
		detail["entries"].(*leaderboard.Page[leaderboard.SnapshotEntry]).Total != 1 ||
		detail["entries"].(*leaderboard.Page[leaderboard.SnapshotEntry]).Items[0].Account != "dave" {
		t.Fatalf("结算明细：%+v %v", detail, err)
	}
	if past, err := svc.AdminBoard(ctx, appID, leaderboard.BoardPointsWeekly, prevWeek.Key, 1, 20); err != nil || past.Source != "snapshot" ||
		len(past.Items) != 4 || past.Items[0].Account != "alice@example.com" {
		t.Fatalf("管理端往期榜：%+v %v", past, err)
	}
	stats, err := svc.Stats(ctx, appID)
	if err != nil || stats["exclusions"].(int64) != 2 {
		t.Fatalf("概览：%+v %v", stats, err)
	}
	// ── 旧接口：账号脱敏，临时冻结的人不上榜 ──
	if _, err := pool.Exec(ctx, `UPDATE users SET disabled_end_time = NOW() + INTERVAL '1 day' WHERE id = $1`, bob); err != nil {
		t.Fatal(err)
	}
	points := NewPointsService(zap.NewNop(), pg, nil)
	for _, kind := range []string{"integral", "experience", "level", "sign_today", "sign_consecutive", "sign_monthly"} {
		legacy, err := points.GetRankings(ctx, &authdomain.Session{AppID: appID, UserID: carol}, kind, 1, 50)
		if err != nil {
			t.Fatalf("旧榜 %s：%v", kind, err)
		}
		for _, item := range legacy.Items {
			if item.Account == "13812345678" || item.Account == "alice@example.com" || item.UserID == bob {
				t.Fatalf("旧榜 %s 返回了原始账号或冻结的人：%+v", kind, item)
			}
		}
		if kind == "integral" && (legacy.MyRank == nil || legacy.MyRank.Account != "138****5678") {
			t.Fatalf("旧榜的 myRank 也要脱敏：%+v", legacy.MyRank)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET disabled_end_time = NULL WHERE id = $1`, bob); err != nil {
		t.Fatal(err)
	}

	if err := svc.RemoveExclusion(ctx, appID, frank); err != nil {
		t.Fatal(err)
	}
	expectCode("移回不在名单里的人", svc.RemoveExclusion(ctx, appID, frank), errCodeLeaderboardExclusionNotFound)
	if me := board(frank, leaderboard.BoardPointsWeekly, 1, 20).Me; me.Excluded || me.Rank == nil || *me.Rank != 3 {
		t.Fatalf("移回后应重新上榜：%+v", me)
	}
}
