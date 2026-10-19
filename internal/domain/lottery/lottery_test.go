package lottery

import (
	"strings"
	"testing"
	"time"
)

func gridInput() ActivityInput {
	prizes := []PrizeInput{
		{Position: 0, Reward: Reward{Type: KindNone}, Weight: 40, Quantity: -1, Fallback: true},
		{Position: 1, Reward: Reward{Type: KindPoints, Amount: 2}, Weight: 30, Quantity: -1},
		{Position: 2, Reward: Reward{Type: KindNone}, Weight: 10, Quantity: -1},
		{Position: 3, Reward: Reward{Type: KindPoints, Amount: 5}, Weight: 10, Quantity: -1},
		{Position: 4, Reward: Reward{Type: KindNone}, Weight: 5, Quantity: -1},
		{Position: 5, Reward: Reward{Type: KindPoints, Amount: 15}, Weight: 3, Quantity: 100},
		{Position: 6, Reward: Reward{Type: KindNone}, Weight: 1, Quantity: -1},
		{Position: 7, Reward: Reward{Type: KindPoints, Amount: 30}, Weight: 1, Quantity: 10},
	}
	return ActivityInput{AppID: 1, Name: "幸运抽奖", UIMode: UIModeGrid, Status: StatusActive, FreeDaily: 1,
		PityThreshold: 6, PityPosition: 1, Prizes: prizes}
}

func TestValidateInput(t *testing.T) {
	if err := ValidateInput(NormalizeInput(gridInput())); err != nil {
		t.Fatalf("合法的九宫格被拒：%v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ActivityInput)
		want   string
	}{
		{"九宫格少一格", func(in *ActivityInput) { in.Prizes = in.Prizes[:7] }, "恰好 8 个"},
		{"位置重复", func(in *ActivityInput) { in.Prizes[1].Position = 0 }, "重复"},
		{"没有不限库存", func(in *ActivityInput) {
			for i := range in.Prizes {
				in.Prizes[i].Quantity = 10
				in.Prizes[i].Fallback = false
			}
		}, "不限库存"},
		{"兜底奖限量", func(in *ActivityInput) { in.Prizes[0].Quantity = 5 }, "兜底奖"},
		{"权重全为 0", func(in *ActivityInput) {
			for i := range in.Prizes {
				in.Prizes[i].Weight = 0
			}
		}, "权重合计"},
		{"保底奖是谢谢参与", func(in *ActivityInput) { in.PityPosition = 0 }, "谢谢参与"},
		{"保底次数为 1", func(in *ActivityInput) { in.PityThreshold = 1 }, "至少为 2"},
		{"余额不能当奖品", func(in *ActivityInput) { in.Prizes[3].Reward = Reward{Type: "balance"} }, "未登记"},
		{"积分为 0", func(in *ActivityInput) { in.Prizes[3].Reward.Amount = 0 }, "积分"},
		{"结束早于开始", func(in *ActivityInput) {
			start := time.Now()
			end := start.Add(-time.Hour)
			in.StartTime, in.EndTime = &start, &end
		}, "结束时间"},
		{"转盘只有 1 个", func(in *ActivityInput) {
			in.UIMode = UIModeWheel
			in.Prizes = in.Prizes[:1]
			in.PityThreshold = 0
		}, "2–12"},
	}
	for _, tc := range cases {
		input := gridInput()
		tc.mutate(&input)
		err := ValidateInput(NormalizeInput(input))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s：期望包含 %q 的错误，得到 %v", tc.name, tc.want, err)
		}
	}
}

func TestNormalizeRewardDropsUnrelatedFields(t *testing.T) {
	got := NormalizeReward(Reward{Type: KindNone, Amount: 5, Text: "x"})
	if got != (Reward{Type: KindNone}) {
		t.Fatalf("谢谢参与只留类型，得到 %+v", got)
	}
	got = NormalizeReward(Reward{Type: KindPoints, Amount: 30, Tag: "x", RefID: 3})
	if got != (Reward{Type: KindPoints, Amount: 30}) {
		t.Fatalf("积分只留数量，得到 %+v", got)
	}
}

func prizes() []Prize {
	return []Prize{
		{ID: 1, Kind: KindNone, Weight: 50, Quantity: -1, Position: 0},
		{ID: 2, Kind: KindPoints, Weight: 30, Quantity: -1, Position: 1, Name: "2 积分"},
		{ID: 3, Kind: KindPoints, Weight: 20, Quantity: 1, Used: 1, Position: 2},
		{ID: 4, Kind: KindPoints, Weight: 0, Quantity: -1, Position: 3},
	}
}

func TestPickAndProbabilities(t *testing.T) {
	candidates := Candidates(prizes())
	if len(candidates) != 2 || TotalWeight(candidates) != 80 {
		t.Fatalf("候选应排除售罄与零权重，得到 %+v", candidates)
	}
	for roll, want := range map[int64]int64{0: 1, 49: 1, 50: 2, 79: 2} {
		got, ok := Pick(candidates, roll)
		if !ok || got.ID != want {
			t.Errorf("roll=%d 应抽中 %d，得到 %d", roll, want, got.ID)
		}
	}
	if _, ok := Pick(candidates, 80); ok {
		t.Fatal("越界的 roll 不该抽中任何奖品")
	}
	probabilities := Probabilities(prizes())
	want := []float64{62.5, 37.5, 0, 0}
	for i := range want {
		if probabilities[i] != want[i] {
			t.Fatalf("公示概率应为 %v，得到 %v", want, probabilities)
		}
	}
}

func TestFallbackPrize(t *testing.T) {
	list := prizes()
	if got, _ := FallbackPrize(list); got.ID != 1 {
		t.Fatalf("没标兜底时取不限量的谢谢参与，得到 %d", got.ID)
	}
	list[1].Fallback = true
	if got, _ := FallbackPrize(list); got.ID != 2 {
		t.Fatalf("标了兜底就用它，得到 %d", got.ID)
	}
}

func TestPity(t *testing.T) {
	pityID := int64(2)
	activity := Activity{PityThreshold: 5, PityPrizeID: &pityID}
	if PityDue(5, 3) || !PityDue(5, 4) || PityDue(0, 100) {
		t.Fatal("连续 4 次未中时第 5 次必中；阈值为 0 时永不触发")
	}
	progress := PityProgress(activity, prizes(), 0)
	if progress == nil || progress.Remaining != 5 || progress.PrizeName != "2 积分" {
		t.Fatalf("没有未中记录时还需 5 次，得到 %+v", progress)
	}
	if progress := PityProgress(activity, prizes(), 4); progress.Remaining != 1 {
		t.Fatalf("连续 4 次未中时下一次必中，得到 %+v", progress)
	}
	soldOut := int64(3)
	activity.PityPrizeID = &soldOut
	if PityProgress(activity, prizes(), 0) != nil {
		t.Fatal("保底奖抽光后不再承诺必中")
	}
	if NextMissStreak(3, Prize{Kind: KindNone}) != 4 || NextMissStreak(3, Prize{Kind: KindPoints}) != 0 {
		t.Fatal("未中累加、抽中清零")
	}
}

func TestEvaluateChances(t *testing.T) {
	activity := Activity{FreeDaily: 1, PointsCost: 20, PointsDaily: 2}
	chances := EvaluateChances(activity, UserFacts{FreeUsedToday: 0, Bonus: 3, Integral: 100}, VideoChances{})
	if chances.Next != SourceFree || chances.Free.Remaining != 1 {
		t.Fatalf("有免费次数时先用免费，得到 %+v", chances)
	}
	chances = EvaluateChances(activity, UserFacts{FreeUsedToday: 1, Bonus: 3, Integral: 100}, VideoChances{})
	if chances.Next != SourceBonus {
		t.Fatalf("免费用完后用次数余额，得到 %q", chances.Next)
	}
	chances = EvaluateChances(activity, UserFacts{FreeUsedToday: 1, Integral: 100, PointsUsedToday: 1}, VideoChances{})
	if chances.Next != SourcePoints || chances.Points.Remaining != 1 {
		t.Fatalf("最后才用积分，得到 %+v", chances)
	}
	chances = EvaluateChances(activity, UserFacts{FreeUsedToday: 1, Integral: 10}, VideoChances{})
	if chances.Next != "" || chances.Points.Affordable {
		t.Fatalf("积分不够时没有可用次数，得到 %+v", chances)
	}
	if source, reason := ResolveSource(chances, ""); source != "" || reason != ReasonNoChance {
		t.Fatalf("自动选择时报没有次数，得到 %q %q", source, reason)
	}
	if _, reason := ResolveSource(chances, SourcePoints); reason != ReasonPointsShort {
		t.Fatalf("指定积分时报积分不足，得到 %q", reason)
	}
	if _, reason := ResolveSource(chances, SourceFree); reason != ReasonFreeExhausted {
		t.Fatalf("指定免费时报免费用完，得到 %q", reason)
	}
	none := EvaluateChances(Activity{FreeDaily: 1}, UserFacts{Integral: 999}, VideoChances{})
	if _, reason := ResolveSource(none, SourcePoints); reason != ReasonPointsUnavailable {
		t.Fatalf("未开放积分抽奖，得到 %q", reason)
	}
	if _, reason := ResolveSource(none, "video"); reason != ReasonUnknownSource {
		t.Fatalf("video 不是来源（视频抽奖到账的是 bonus），得到 %q", reason)
	}
}

func TestWindow(t *testing.T) {
	now := time.Now()
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	cases := []struct {
		activity Activity
		want     string
	}{
		{Activity{Status: StatusActive}, ReasonOK},
		{Activity{Status: StatusDraft}, ReasonNotFound},
		{Activity{Status: StatusPaused}, ReasonNotFound},
		{Activity{Status: StatusEnded}, ReasonEnded},
		{Activity{Status: StatusActive, StartTime: &future}, ReasonNotStarted},
		{Activity{Status: StatusActive, EndTime: &past}, ReasonEnded},
	}
	for _, tc := range cases {
		if got := Window(tc.activity, now); got != tc.want {
			t.Errorf("%+v：期望 %q，得到 %q", tc.activity, tc.want, got)
		}
	}
}

func TestCatalogDescribesEveryKind(t *testing.T) {
	for _, kind := range Kinds() {
		spec, ok := FindSpec(kind)
		if !ok || spec.Label == "" {
			t.Errorf("奖品类型 %s 没有自述", kind)
		}
	}
	if _, ok := FindSpec("balance"); ok {
		t.Fatal("钱包余额不能作为抽奖奖品")
	}
	if name := DefaultPrizeName(Reward{Type: KindPoints, Amount: 30}, nil, nil); name != "30 积分" {
		t.Fatalf("默认奖品名应为「30 积分」，得到 %q", name)
	}
}
