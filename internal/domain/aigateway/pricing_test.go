package aigateway

import (
	"math/rand"
	"testing"
	"time"

	aidomain "aegis/internal/domain/ai"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestCostMicroTokens(t *testing.T) {
	price := Price{InputPrice: dec("2.5"), OutputPrice: dec("10")}
	got := CostMicro(price, Meters{InputTokens: 1000, OutputTokens: 500}, decimal.NewFromInt(1))
	// 1000 × 2.5 + 500 × 10 = 7500 微点
	if got != 7500 {
		t.Fatalf("期望 7500 微点，得到 %d", got)
	}
	// 倍率 0.8：6000
	if got := CostMicro(price, Meters{InputTokens: 1000, OutputTokens: 500}, dec("0.8")); got != 6000 {
		t.Fatalf("期望 6000 微点，得到 %d", got)
	}
	// 一个 token、单价 0.1 → 0.1 微点，向上取整成 1：有用量就一定有价格。
	if got := CostMicro(Price{InputPrice: dec("0.1")}, Meters{InputTokens: 1}, decimal.NewFromInt(1)); got != 1 {
		t.Fatalf("不足 1 微点应向上取整成 1，得到 %d", got)
	}
}

func TestCostMicroOtherMeters(t *testing.T) {
	price := Price{
		RequestPrice: dec("0.5"), ImagePrice: dec("20"),
		ImagePrices: map[string]decimal.Decimal{"1024x1792": dec("30"), "hd": dec("40"), "hd:1024x1792": dec("60")},
		CharPrice:   dec("15"), MinutePrice: dec("6"),
	}
	one := decimal.NewFromInt(1)
	cases := []struct {
		name   string
		meters Meters
		want   int64
	}{
		{"固定价", Meters{}, 500_000},
		{"默认尺寸两张", Meters{Images: 2}, 500_000 + 40_000_000},
		{"尺寸档", Meters{Images: 1, ImageSize: "1024x1792"}, 500_000 + 30_000_000},
		{"质量档", Meters{Images: 1, ImageQuality: "HD", ImageSize: "512x512"}, 500_000 + 40_000_000},
		{"质量:尺寸档优先", Meters{Images: 1, ImageQuality: "hd", ImageSize: "1024x1792"}, 500_000 + 60_000_000},
		{"语音合成字符", Meters{Characters: 1000}, 500_000 + 15_000},
		{"转写 90 秒", Meters{AudioMillis: 90_000}, 500_000 + 9_000_000},
	}
	for _, tc := range cases {
		if got := CostMicro(price, tc.meters, one); got != tc.want {
			t.Errorf("%s：期望 %d，得到 %d", tc.name, tc.want, got)
		}
	}
	price.Free = true
	if got := CostMicro(price, Meters{Images: 3}, one); got != 0 {
		t.Fatalf("免费模型应为 0，得到 %d", got)
	}
}

func TestMaxAffordableOutput(t *testing.T) {
	price := Price{InputPrice: dec("1"), OutputPrice: dec("4")}
	// 预算 10000 微点，输入 2000 token 花 2000，剩 8000 / 4 = 2000 个输出 token。
	if got := MaxAffordableOutput(price, Meters{InputTokens: 2000}, decimal.NewFromInt(1), 10_000); got != 2000 {
		t.Fatalf("期望 2000，得到 %d", got)
	}
	// 输入就超了预算
	if got := MaxAffordableOutput(price, Meters{InputTokens: 20000}, decimal.NewFromInt(1), 10_000); got != 0 {
		t.Fatalf("期望 0，得到 %d", got)
	}
	// 输出不计价：不限制
	if got := MaxAffordableOutput(Price{RequestPrice: dec("1")}, Meters{}, decimal.NewFromInt(1), 10); got != -1 {
		t.Fatalf("输出不计价应返回 -1，得到 %d", got)
	}
	// 压到这个上限后，实际价格一定不超过预算
	got := MaxAffordableOutput(price, Meters{InputTokens: 333}, dec("1.37"), 54_321)
	cost := CostMicro(price, Meters{InputTokens: 333, OutputTokens: got}, dec("1.37"))
	if cost > 54_321 {
		t.Fatalf("压低输出后价格 %d 超出预算", cost)
	}
	if over := CostMicro(price, Meters{InputTokens: 333, OutputTokens: got + 1}, dec("1.37")); over <= 54_321 {
		t.Fatalf("多一个 token 仍付得起（%d），说明压得过低", over)
	}
}

// 暂扣向上取整、结算加零头向下取整：任意顺序的调用里，结算要扣的整数单位都不超过暂扣的。
func TestChargeWithCarryNeverExceedsHold(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var carry int64
	var paidUnits, totalMicro int64
	for i := 0; i < 20000; i++ {
		take := rng.Int63n(5 * MicroPerPoint)
		consumed := rng.Int63n(take + 1)
		hold := CeilUnits(take, MicroPerPoint)
		units, next := ChargeWithCarry(carry, consumed, MicroPerPoint)
		if units > hold {
			t.Fatalf("第 %d 次：暂扣 %d 单位，结算要扣 %d", i, hold, units)
		}
		if next < 0 || next >= MicroPerPoint {
			t.Fatalf("零头越界：%d", next)
		}
		carry = next
		paidUnits += units
		totalMicro += consumed
	}
	// 累计扣掉的整数单位与累计价格之差小于一个单位
	if diff := totalMicro - paidUnits*MicroPerPoint; diff < 0 || diff >= MicroPerPoint || diff != carry {
		t.Fatalf("累计误差 %d 应等于零头 %d 且小于一个单位", diff, carry)
	}
}

func TestWalletConversions(t *testing.T) {
	rate := dec("0.01") // 1 点 = 1 分
	if got := WalletAvailableMicro(150, rate); got != 150*MicroPerPoint {
		t.Fatalf("1.50 元应能付 150 点，得到 %d 微点", got)
	}
	if got := WalletHoldCents(1, rate); got != 1 {
		t.Fatalf("1 微点也要暂扣 1 分，得到 %d", got)
	}
	// 暂扣不超过可用：随机比例与余额
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 5000; i++ {
		rate := decimal.NewFromInt(rng.Int63n(5000) + 1).Div(decimal.NewFromInt(10000)) // 0.0001-0.5 元/点
		cents := rng.Int63n(100000)
		available := WalletAvailableMicro(cents, rate)
		if available == 0 {
			continue
		}
		take := rng.Int63n(available) + 1
		hold := WalletHoldCents(take, rate)
		if hold > cents {
			t.Fatalf("费率 %s：可用 %d 微点、取 %d，暂扣 %d 分超过余额 %d 分", rate, available, take, hold, cents)
		}
		consumed := rng.Int63n(take + 1)
		units, _ := ChargeWithCarry(rng.Int63n(MicroPerPoint), WalletMicroCents(consumed, rate), MicroPerPoint)
		if units > hold {
			t.Fatalf("费率 %s：结算 %d 分超过暂扣 %d 分", rate, units, hold)
		}
	}
}

func TestPlanHoldsAndAllocate(t *testing.T) {
	available := []Availability{
		{Source: SourceFreeDaily, Micro: 300},
		{Source: SourceCredit, Micro: 0},
		{Source: SourceIntegral, Micro: 5000},
		{Source: SourceWallet, Micro: 10000},
	}
	holds, total := PlanHolds(available, 2000)
	if total != 2000 || len(holds) != 2 || holds[0].Micro != 300 || holds[1].Micro != 1700 || holds[1].Source != SourceIntegral {
		t.Fatalf("分摊结果不对：%+v（合计 %d）", holds, total)
	}
	// 实际只用了 500：免费额度承担 300，积分承担 200
	consumed, overflow := AllocateCost(holds, 500)
	if overflow != 0 || consumed[0] != 300 || consumed[1] != 200 {
		t.Fatalf("摊费不对：%v 溢出 %d", consumed, overflow)
	}
	// 实际 2600：暂扣只有 2000，溢出 600
	consumed, overflow = AllocateCost(holds, 2600)
	if overflow != 600 || consumed[0] != 300 || consumed[1] != 1700 {
		t.Fatalf("溢出计算不对：%v 溢出 %d", consumed, overflow)
	}
	// 凑不够
	if _, total := PlanHolds(available, 1_000_000); total != 15300 {
		t.Fatalf("可用合计应为 15300，得到 %d", total)
	}
}

func TestSettingsFactor(t *testing.T) {
	settings := DefaultSettings()
	settings.PriceMultiplier = dec("1.5")
	settings.VipMultiplier = dec("0.8")
	settings.LevelMultipliers = []LevelMultiplier{{Level: 3, Multiplier: dec("0.9")}, {Level: 10, Multiplier: dec("0.5")}}
	cases := []struct {
		profile Profile
		want    string
	}{
		{Profile{Level: 1}, "1.5"},
		{Profile{Level: 3}, "1.35"},
		{Profile{Level: 9, IsVip: true}, "1.08"},
		{Profile{Level: 12, IsVip: true}, "0.6"},
	}
	for _, tc := range cases {
		if got := settings.Factor(tc.profile); !got.Equal(dec(tc.want)) {
			t.Errorf("%+v：期望 %s，得到 %s", tc.profile, tc.want, got)
		}
	}
}

func TestNormalizeSettings(t *testing.T) {
	if _, err := NormalizeSettings(DefaultSettings()); err != nil {
		t.Fatalf("默认设置应合法：%v", err)
	}
	broken := map[string]func(*Settings){
		"未知来源":    func(s *Settings) { s.Sources = []string{"gold"} },
		"重复来源":    func(s *Settings) { s.Sources = []string{SourceIntegral, SourceIntegral} },
		"没有来源":    func(s *Settings) { s.Sources = nil },
		"倍率为 0":   func(s *Settings) { s.PriceMultiplier = decimal.Zero },
		"余额无折算":   func(s *Settings) { s.Sources = append(s.Sources, SourceWallet); s.WalletRate = decimal.Zero },
		"等级重复":    func(s *Settings) { s.LevelMultipliers = []LevelMultiplier{{1, dec("1")}, {1, dec("0.5")}} },
		"负额度":     func(s *Settings) { s.FreeDaily = dec("-1") },
		"并发为 0":   func(s *Settings) { s.MaxConcurrent = 0 },
		"时区无效":    func(s *Settings) { s.Timezone = "Mars/Base" },
		"使用对象无效":  func(s *Settings) { s.Audience = "everyone" },
		"默认输出为 0": func(s *Settings) { s.DefaultMaxTokens = 0 },
	}
	for name, mutate := range broken {
		settings := DefaultSettings()
		mutate(&settings)
		if _, err := NormalizeSettings(settings); err == nil {
			t.Errorf("%s：应被拒绝", name)
		}
	}
}

func TestNormalizeModel(t *testing.T) {
	ok, err := NormalizeModel(ModelInput{Model: "gpt-4o", UpstreamModel: "gpt-4o", Kind: KindChat, InputPrice: dec("2.5")})
	if err != nil {
		t.Fatalf("应合法：%v", err)
	}
	if ok.UpstreamModel != "" || !ok.Enabled {
		t.Fatalf("同名上游应清空、默认启用：%+v", ok)
	}
	if _, err := NormalizeModel(ModelInput{Model: "gpt-4o", Kind: KindChat}); err == nil {
		t.Fatal("没有价格又没标免费的模型应被拒绝")
	}
	if _, err := NormalizeModel(ModelInput{Model: "gpt-4o", Kind: KindChat, Free: true}); err != nil {
		t.Fatalf("显式免费应允许：%v", err)
	}
	if _, err := NormalizeModel(ModelInput{Model: "bad name", Kind: KindChat, Free: true}); err == nil {
		t.Fatal("带空格的模型名应被拒绝")
	}
	if _, err := NormalizeModel(ModelInput{Model: "m", Kind: "video", Free: true}); err == nil {
		t.Fatal("未登记的种类应被拒绝")
	}
	if _, err := NormalizeModel(ModelInput{Model: "m", Kind: KindChat, InputPrice: dec("-1")}); err == nil {
		t.Fatal("负价格应被拒绝")
	}
	if _, err := NormalizeModel(ModelInput{Model: "m", Kind: KindImage, ImagePrices: map[string]decimal.Decimal{"超大": dec("1")}}); err == nil {
		t.Fatal("非法的图片分档键应被拒绝")
	}
	img, err := NormalizeModel(ModelInput{Model: "gpt-image-1", Kind: KindImage, ImagePrices: map[string]decimal.Decimal{" HD:1024x1792 ": dec("8")}})
	if err != nil || !img.ImagePrices["hd:1024x1792"].Equal(dec("8")) {
		t.Fatalf("图片分档键应规整为小写：%v %+v", err, img.ImagePrices)
	}
}

func TestKeys(t *testing.T) {
	secret, hash, hint, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !LooksLikeKey(secret) {
		t.Fatalf("生成的密钥应能被识别：%s", secret)
	}
	if HashKey(secret) != hash || len(hash) != 64 {
		t.Fatal("散列不一致")
	}
	if hint == secret || len([]rune(hint)) > 32 {
		t.Fatalf("提示不应暴露全文：%s", hint)
	}
	for _, token := range []string{"eyJhbGciOi.xxx.yyy", "sk-ag-short", secret + "x", "sk-ag-" + string(make([]byte, 43))} {
		if LooksLikeKey(token) {
			t.Errorf("%q 不应被当成密钥", token)
		}
	}
	now := time.Now()
	past := now.Add(-time.Hour)
	if _, err := NormalizeKeyInput(KeyInput{ExpiresAt: &past}, true, now); err == nil {
		t.Fatal("过去的到期时间应被拒绝")
	}
	in, err := NormalizeKeyInput(KeyInput{Status: KeyDisabled, Models: []string{" gpt-4o ", "gpt-4o", ""}}, true, now)
	if err != nil || in.Status != KeyActive || len(in.Models) != 1 {
		t.Fatalf("新建密钥应强制启用并去重白名单：%v %+v", err, in)
	}
}

func TestPeriodKey(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	// UTC 16:30 是上海次日 00:30
	at := time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC)
	if got := PeriodKey(SourceFreeDaily, at, loc); got != "2026-10-20" {
		t.Fatalf("日额度应按上海日历日，得到 %s", got)
	}
	if got := PeriodKey(SourceVipMonthly, time.Date(2026, 10, 31, 16, 30, 0, 0, time.UTC), loc); got != "2026-11" {
		t.Fatalf("月额度应按上海年月，得到 %s", got)
	}
}

func TestEstimateChatInput(t *testing.T) {
	req := aidomain.ChatRequest{
		System: "you are helpful",
		Messages: []aidomain.ChatMessage{
			{Role: aidomain.RoleUser, Content: []aidomain.ContentPart{
				{Type: aidomain.PartText, Text: "你好世界"},
				{Type: aidomain.PartImage, ImageURL: "https://example.com/a.png"},
			}},
		},
	}
	// system 15 字节 → 5；消息开销 4；「你好世界」12 字节 → 4；一张图 1000
	if got := EstimateChatInput(req); got != 5+4+4+ImageTokenEstimate {
		t.Fatalf("估算不对：%d", got)
	}
}
