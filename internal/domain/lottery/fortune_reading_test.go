package lottery

import (
	"math"
	"testing"
	"time"
)

// 今日运气的解读与出奖用的系数是同一个；档位按 z 分，各档人数占比接近正态分布的理论值。
func TestReadFortune(t *testing.T) {
	if ReadFortune(nil, 1, 1, time.Now()) != nil || ReadFortune(&Fortune{}, 1, 1, time.Now()) != nil {
		t.Fatal("没开每日运气时不应有解读")
	}
	fortune := &Fortune{Spread: 0.3}
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	counts := map[string]int{}
	const users = 20000
	for user := int64(1); user <= users; user++ {
		reading := ReadFortune(fortune, 7, user, day)
		if reading.Factor != FortuneOf(fortune, 7, user, day) {
			t.Fatalf("用户 %d 的解读系数 %v 与出奖系数 %v 不同", user, reading.Factor, FortuneOf(fortune, 7, user, day))
		}
		if reading.Percentile < 1 || reading.Percentile > 99 || reading.Title == "" || reading.Message == "" {
			t.Fatalf("解读不完整：%+v", reading)
		}
		if !reading.ResetsAt.Equal(day.AddDate(0, 0, 1)) {
			t.Fatalf("应在次日零点换运气，得到 %v", reading.ResetsAt)
		}
		counts[reading.Level]++
	}
	// Φ(-0.5)=30.85%，Φ(0.5)-Φ(-0.5)=38.29%，Φ(1.5)-Φ(0.5)=24.17%，1-Φ(1.5)=6.68%
	want := map[string]float64{FortuneQuiet: 0.3085, FortuneSteady: 0.3829, FortuneLucky: 0.2417, FortuneBurst: 0.0668}
	for level, share := range want {
		if got := float64(counts[level]) / users; math.Abs(got-share) > 0.015 {
			t.Fatalf("档位 %s 应约占 %.2f%%，得到 %.2f%%", level, share*100, got*100)
		}
	}
}

func TestRealPrizeShare(t *testing.T) {
	prizes := []Prize{
		{Weight: 50, Quantity: -1, Kind: KindExperience, Fallback: true},
		{Weight: 30, Quantity: -1, Kind: KindPoints},
		{Weight: 20, Quantity: -1, Kind: KindNone},
	}
	if share := RealPrizeShare(prizes); share != 30 {
		t.Fatalf("兜底奖与谢谢参与以外的概率应为 30%%，得到 %v", share)
	}
	if share := RealPrizeShare(FortunePrizes(prizes, 2)); share <= 30 {
		t.Fatalf("运气系数大于 1 时真实奖品的概率应升高，得到 %v", share)
	}
}
