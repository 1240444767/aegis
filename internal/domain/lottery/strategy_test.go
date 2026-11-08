package lottery

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"gonum.org/v1/gonum/stat"
	"gonum.org/v1/gonum/stat/distuv"
)

// strategyPrizes 一张有 ID 的九宫格：4 格谢谢参与（合计 56），4 格中奖（合计 44）。
func strategyPrizes() []Prize {
	return []Prize{
		{ID: 1, Position: 0, Kind: KindNone, Reward: Reward{Type: KindNone}, Weight: 40, Quantity: -1, Fallback: true},
		{ID: 2, Position: 1, Kind: KindPoints, Reward: Reward{Type: KindPoints, Amount: 2}, Weight: 30, Quantity: -1},
		{ID: 3, Position: 2, Kind: KindNone, Reward: Reward{Type: KindNone}, Weight: 10, Quantity: -1},
		{ID: 4, Position: 3, Kind: KindPoints, Reward: Reward{Type: KindPoints, Amount: 5}, Weight: 10, Quantity: -1},
		{ID: 5, Position: 4, Kind: KindNone, Reward: Reward{Type: KindNone}, Weight: 5, Quantity: -1},
		{ID: 6, Position: 5, Kind: KindPoints, Reward: Reward{Type: KindPoints, Amount: 15}, Weight: 3, Quantity: 100},
		{ID: 7, Position: 6, Kind: KindNone, Reward: Reward{Type: KindNone}, Weight: 1, Quantity: -1},
		{ID: 8, Position: 7, Kind: KindVipDays, Reward: Reward{Type: KindVipDays, Amount: 7}, Weight: 1, Quantity: 10},
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-4 }

func TestPlanDrawZeroStrategyMatchesBaseWeights(t *testing.T) {
	prizes := strategyPrizes()
	plan := PlanDraw(Activity{}, Strategy{}, prizes, DrawState{Now: time.Now()})
	for index, odds := range plan.Odds {
		if want := float64(prizes[index].Weight) / 100; !near(odds.Probability, want) {
			t.Fatalf("奖品 %d 概率 %v，应为 %v", odds.PrizeID, odds.Probability, want)
		}
		if len(odds.Factors) != 0 {
			t.Fatalf("零值策略不该有任何系数：%+v", odds.Factors)
		}
	}
	if !near(plan.WinRate, 0.44) {
		t.Fatalf("中奖率 %v，应为 0.44", plan.WinRate)
	}
}

func TestPlanDrawHardLimits(t *testing.T) {
	prizes := strategyPrizes()
	prizes[1].DailyLimit = 5
	prizes[3].UserLimit = 2
	prizes[5].UserDailyLimit = 1
	prizes[7].Used = prizes[7].Quantity
	state := DrawState{
		Now:            time.Now(),
		PrizeDailyUsed: map[int64]int64{2: 5},
		PrizeWins:      map[int64]int{4: 2},
		PrizeWinsToday: map[int64]int{6: 1},
	}
	plan := PlanDraw(Activity{}, Strategy{}, prizes, state)
	want := map[int64]string{2: FactorDailyLimit, 4: FactorUserLimit, 6: FactorUserDailyLimit, 8: FactorStock}
	for _, odds := range plan.Odds {
		if odds.Blocked != want[odds.PrizeID] {
			t.Fatalf("奖品 %d 挡掉原因 %q，应为 %q", odds.PrizeID, odds.Blocked, want[odds.PrizeID])
		}
		if want[odds.PrizeID] != "" && odds.Probability != 0 {
			t.Fatalf("被挡掉的奖品 %d 仍有概率 %v", odds.PrizeID, odds.Probability)
		}
	}
	if plan.WinRate != 0 {
		t.Fatalf("中奖奖品全被挡掉后中奖率应为 0，得到 %v", plan.WinRate)
	}
}

func TestPlanDrawRiskOnlyLosing(t *testing.T) {
	strategy := Strategy{Risk: &RiskGuard{DeviceAccounts: 3}}
	plan := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), DeviceAccounts: 4})
	if !plan.Risk || plan.WinRate != 0 {
		t.Fatalf("同设备 4 个账号超过上限 3，应只出未中奖：risk=%v winRate=%v", plan.Risk, plan.WinRate)
	}
	plan = PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), DeviceAccounts: 3})
	if plan.Risk {
		t.Fatalf("恰好等于上限不该命中风控")
	}
}

func TestSoftPityRaisesWinRateByStep(t *testing.T) {
	strategy := Strategy{SoftPity: &SoftPity{Start: 3, Step: 10}}
	for _, tc := range []struct {
		miss int
		want float64
	}{{2, 0.44}, {3, 0.54}, {5, 0.74}, {20, 1}} {
		plan := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), MissStreak: tc.miss})
		if !near(plan.WinRate, tc.want) {
			t.Fatalf("连续未中 %d 次时中奖率 %v，应为 %v", tc.miss, plan.WinRate, tc.want)
		}
	}
	// 中奖奖品之间的比例不变
	plan := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), MissStreak: 4})
	if ratio := plan.Odds[1].Probability / plan.Odds[3].Probability; !near(ratio, 3) {
		t.Fatalf("软保底改变了中奖奖品之间的比例：%v", ratio)
	}
}

func TestPacingHoldsAndBoosts(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(10 * 24 * time.Hour)
	activity := Activity{StartTime: &start, EndTime: &end}
	strategy := NormalizeStrategy(Strategy{Pacing: &Pacing{Slack: 0.05, MaxBoost: 3}})
	now := start.Add(5 * 24 * time.Hour) // 进度 50%
	prizes := strategyPrizes()
	limited := 5 // 库存 100

	prizes[limited].Used = 60 // 超前：已放量 55
	plan := PlanDraw(activity, strategy, prizes, DrawState{Now: now})
	if plan.Odds[limited].Weight != 0 {
		t.Fatalf("超前于进度时应暂停放量，权重 %v", plan.Odds[limited].Weight)
	}
	prizes[limited].Used = 52 // 超前 2，提前量 5：系数 0.6
	plan = PlanDraw(activity, strategy, prizes, DrawState{Now: now})
	if !near(plan.Odds[limited].Weight, 3*0.6) {
		t.Fatalf("提前量内应线性收紧，权重 %v", plan.Odds[limited].Weight)
	}
	prizes[limited].Used = 10 // 落后很多：放大到 3 倍
	plan = PlanDraw(activity, strategy, prizes, DrawState{Now: now})
	if !near(plan.Odds[limited].Weight, 9) {
		t.Fatalf("落后于进度时应加速，权重 %v", plan.Odds[limited].Weight)
	}
	// 不限库存的奖品不受影响
	if plan.Odds[0].Weight != 40 {
		t.Fatalf("不限库存的奖品被匀速释放影响了：%v", plan.Odds[0].Weight)
	}
}

func TestBudgetTapersPointsPrizes(t *testing.T) {
	strategy := Strategy{Budget: &Budget{DailyPoints: 100}}
	cases := []struct {
		issued int64
		want   [3]float64 // 2 / 5 / 15 积分奖的权重
	}{
		{40, [3]float64{30, 10, 3}},
		{75, [3]float64{15, 5, 1.5}},
		{90, [3]float64{6, 2, 0}}, // 剩 10，15 积分的不出
		{100, [3]float64{0, 0, 0}},
	}
	for _, tc := range cases {
		plan := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), PointsIssuedToday: tc.issued})
		got := [3]float64{plan.Odds[1].Weight, plan.Odds[3].Weight, plan.Odds[5].Weight}
		for i := range got {
			if !near(got[i], tc.want[i]) {
				t.Fatalf("已发 %d 积分时权重 %v，应为 %v", tc.issued, got, tc.want)
			}
		}
	}
}

func TestCooldownDampsRecentWins(t *testing.T) {
	strategy := Strategy{Cooldown: &Cooldown{Draws: 2, Factor: 0.25}}
	plan := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), Recent: []int64{0, 4, 2}})
	if plan.Odds[3].Weight != 2.5 {
		t.Fatalf("最近 2 次里中过的奖品应打折，权重 %v", plan.Odds[3].Weight)
	}
	if plan.Odds[1].Weight != 30 {
		t.Fatalf("第 3 次之前中过的不在冷却窗口里，权重 %v", plan.Odds[1].Weight)
	}
}

func TestBehaviorScore(t *testing.T) {
	model := normalizeBehavior(&BehaviorModel{MinFactor: 0.8, MaxBoost: 1.5})
	empty := ScoreBehavior(model, BehaviorProfile{RecencyDays: 1000})
	if empty.Score > 0.05 || empty.Factor > 0.84 {
		t.Fatalf("没有任何行为的新号行为分应接近 0：%+v", empty)
	}
	loyal := ScoreBehavior(model, BehaviorProfile{
		RegisteredDays: 3000, OnlineMinutes7d: 50_000, OnlineMinutes30d: 200_000, OnlineDays30d: 30,
		LoginDays30d: 30, RecencyDays: 0, SignStreak: 365, SignDays30d: 30, SignTotalDays: 2000,
		Member: true, Lifetime: true, MemberRate: 1, PaidAmount: 10_000, PaidOrders: 100, Level: 100,
		PointsEarned30d: 100_000, Invites: 300, AdViews30d: 1000, Redemptions: 300,
	})
	if loyal.Score < 0.95 || loyal.Factor < 1.46 {
		t.Fatalf("各方面都满的老用户行为分应接近 1：%+v", loyal)
	}
	risky := ScoreBehavior(model, BehaviorProfile{
		RegisteredDays: 3000, OnlineMinutes30d: 200_000, SignStreak: 365, Member: true, MemberRate: 1, RiskScore30d: 5000,
	})
	clean := ScoreBehavior(model, BehaviorProfile{
		RegisteredDays: 3000, OnlineMinutes30d: 200_000, SignStreak: 365, Member: true, MemberRate: 1,
	})
	if risky.Score >= clean.Score {
		t.Fatalf("风险分是负向信号：risky=%v clean=%v", risky.Score, clean.Score)
	}

	// 只作用于中奖奖品
	strategy := Strategy{Behavior: model}
	plan := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), Profile: BehaviorProfile{RecencyDays: 1000}})
	if plan.Odds[0].Weight != 40 || plan.Odds[1].Weight >= 30 {
		t.Fatalf("行为系数应只作用于中奖奖品：%v / %v", plan.Odds[0].Weight, plan.Odds[1].Weight)
	}
	if plan.Behavior == nil {
		t.Fatalf("方案里应带行为分明细")
	}
}

func TestRulesSeeWholeBehavior(t *testing.T) {
	strategy := NormalizeStrategy(Strategy{Rules: []StrategyRule{
		{Name: "连签一周", When: `user.signStreak >= 7 && user.onlineMinutes30d > 60`, Kinds: []string{KindPoints}, Factor: 2},
		{Name: "新人首抽", When: `user.totalDraws == 0 && draw.source == "free"`, Positions: []int{7}, Factor: 10},
		{Name: "低会员率老用户", When: `user.memberRate < 0.1 && user.registeredDays > 90`, Factor: 0},
	}})
	if err := ValidateStrategy(strategy, 8); err != nil {
		t.Fatalf("合法规则被拒：%v", err)
	}
	state := DrawState{Now: time.Now(), Source: SourceFree, Profile: BehaviorProfile{
		SignStreak: 8, OnlineMinutes30d: 120, MemberRate: 0.5, RegisteredDays: 200,
	}}
	plan := PlanDraw(Activity{}, strategy, strategyPrizes(), state)
	if plan.Odds[1].Weight != 60 || plan.Odds[7].Weight != 10 || plan.Odds[0].Weight != 40 {
		t.Fatalf("规则系数不对：%v %v %v", plan.Odds[1].Weight, plan.Odds[7].Weight, plan.Odds[0].Weight)
	}
	state.Profile.MemberRate = 0.05
	plan = PlanDraw(Activity{}, strategy, strategyPrizes(), state)
	if plan.WinRate != 0 && plan.Odds[0].Weight != 0 {
		t.Fatalf("系数 0 的规则应剔除全部奖品")
	}

	bad := []struct{ rule StrategyRule }{
		{StrategyRule{When: `user.notExists > 1`, Factor: 1}},
		{StrategyRule{When: `user.signStreak`, Factor: 1}},
		{StrategyRule{When: `true`, Factor: -1}},
		{StrategyRule{When: `true`, Positions: []int{9}, Factor: 1}},
	}
	for _, tc := range bad {
		if err := ValidateStrategy(NormalizeStrategy(Strategy{Rules: []StrategyRule{tc.rule}}), 8); err == nil {
			t.Fatalf("非法规则没有被拒：%+v", tc.rule)
		}
	}
}

func TestValidateStrategyViaInput(t *testing.T) {
	in := gridInput()
	in.Prizes[5].UserDailyLimit = 3
	in.Prizes[5].UserLimit = 2
	if err := ValidateInput(NormalizeInput(in)); err == nil || !strings.Contains(err.Error(), "每人每日") {
		t.Fatalf("每人每日上限大于累计上限应被拒：%v", err)
	}
	in = gridInput()
	in.Strategy = Strategy{Behavior: &BehaviorModel{MaxBoost: 2, Signals: []BehaviorSignal{{Key: "nope", Weight: 1}}}}
	if err := ValidateInput(NormalizeInput(in)); err == nil || !strings.Contains(err.Error(), "行为信号") {
		t.Fatalf("未知信号应被拒：%v", err)
	}
}

// TestSampleFollowsPlan 用卡方检验确认 gonum 抽样确实服从方案里的概率。
func TestSampleFollowsPlan(t *testing.T) {
	plan := PlanDraw(Activity{}, Strategy{}, strategyPrizes(), DrawState{Now: time.Now()})
	src := rand.NewPCG(1, 2)
	const n = 200_000
	observed := make([]float64, len(plan.Odds))
	for range n {
		index, ok := Sample(plan, src)
		if !ok {
			t.Fatal("有正权重时抽样不该失败")
		}
		observed[index]++
	}
	expected := make([]float64, len(plan.Odds))
	for index, odds := range plan.Odds {
		expected[index] = odds.Probability * n
	}
	chi := stat.ChiSquare(observed, expected)
	p := 1 - distuv.ChiSquared{K: float64(len(expected) - 1)}.CDF(chi)
	if p < 0.001 {
		t.Fatalf("抽样分布与方案不符：χ²=%.2f p=%.5f", chi, p)
	}
	if _, ok := Sample(DrawPlan{Odds: []PrizeOdds{{Weight: 0}}}, src); ok {
		t.Fatal("权重全为 0 时应返回 false")
	}
}

func TestNewDrawSourceIsUnique(t *testing.T) {
	a, err := NewDrawSource()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewDrawSource()
	if a.Uint64() == b.Uint64() {
		t.Fatal("两个随机源给出了同一个数")
	}
}

func TestSimulateRespectsPityAndStock(t *testing.T) {
	pityID := int64(2)
	activity := Activity{PityThreshold: 5, PityPrizeID: &pityID}
	prizes := strategyPrizes()
	result := Simulate(activity, Strategy{SoftPity: &SoftPity{Start: 2, Step: 5}}, prizes, SimulationInput{
		Users: 500, DrawsPerUser: 40, Seed: 7,
	})
	if result.Draws != 20_000 {
		t.Fatalf("应模拟 20000 次，得到 %d", result.Draws)
	}
	if result.MissStreak.Max >= 5 {
		t.Fatalf("保底 5 次，连续未中不应达到 5：%+v", result.MissStreak)
	}
	if result.WinRate <= result.BaseWinRate {
		t.Fatalf("软保底应抬高实际中奖率：%v ≤ %v", result.WinRate, result.BaseWinRate)
	}
	if result.Prizes[7].Wins > 10 || result.Prizes[5].Wins > 100 {
		t.Fatalf("共享库存被超发：%d / %d", result.Prizes[7].Wins, result.Prizes[5].Wins)
	}
	if result.FirstWin.P50 < 1 || result.NeverWon != 0 {
		t.Fatalf("首中分布不对：%+v never=%d", result.FirstWin, result.NeverWon)
	}
	if err := ValidateSimulation(SimulationInput{Users: 5000, DrawsPerUser: 1000}); err == nil {
		t.Fatal("超过总次数上限的模拟应被拒")
	}
}
