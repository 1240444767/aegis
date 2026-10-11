package invite

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"aegis/internal/domain/pointstore"
)

func enabledSettings(mutate func(*Config)) Settings {
	cfg := DefaultConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	return Settings{Enabled: true, Config: NormalizeConfig(cfg), Configured: true}
}

func TestDefaultConfigIsValid(t *testing.T) {
	if err := ValidateConfig(NormalizeConfig(DefaultConfig())); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestNormalizeConfig(t *testing.T) {
	cfg := NormalizeConfig(Config{
		CodePrefix:     " vy-1 ",
		InviterTrigger: TriggerSignIn,
		InviterRewards: []Reward{{Type: pointstore.RewardExperience, Amount: 5, Tag: "x"}, {Type: RewardPoints, Amount: 10}},
		Milestones: []Milestone{
			{Count: 10, Rewards: []Reward{{Type: RewardPoints, Amount: 1}}},
			{Count: 3, Title: " 三人 ", Rewards: []Reward{{Type: RewardPoints, Amount: 1}}},
		},
	})
	if cfg.CodePrefix != "VY1" {
		t.Fatalf("prefix = %q", cfg.CodePrefix)
	}
	if cfg.TriggerSignInDays != 1 {
		t.Fatalf("signin trigger should default to 1 day, got %d", cfg.TriggerSignInDays)
	}
	if cfg.InviterRewards[0].Type != RewardPoints || cfg.InviterRewards[1].Tag != "" {
		t.Fatalf("rewards not normalized: %+v", cfg.InviterRewards)
	}
	if cfg.Milestones[0].Count != 3 || cfg.Milestones[0].Title != "三人" {
		t.Fatalf("milestones not sorted/trimmed: %+v", cfg.Milestones)
	}
	if cfg.RegisterField != FieldOptional || cfg.CodeCharset != CharsetAlnum || cfg.CodeLength != DefaultCodeLength {
		t.Fatalf("defaults not filled: %+v", cfg)
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"unknown field", func(c *Config) { c.RegisterField = "x" }, "注册表单"},
		{"digits too short", func(c *Config) { c.CodeCharset = CharsetDigits; c.CodeLength = 5 }, "长度"},
		{"window too long", func(c *Config) { c.BindWindowHours = MaxBindWindowHours + 1 }, "补填时限"},
		{"bad landing", func(c *Config) { c.LandingURL = "ftp://x/{code}" }, "落地页"},
		{"dup milestone", func(c *Config) {
			c.Milestones = []Milestone{
				{Count: 3, Rewards: []Reward{{Type: RewardPoints, Amount: 1}}},
				{Count: 3, Rewards: []Reward{{Type: RewardPoints, Amount: 2}}},
			}
		}, "重复"},
		{"empty milestone", func(c *Config) { c.Milestones = []Milestone{{Count: 3}} }, "没有配置奖励"},
		{"manual not allowed", func(c *Config) { c.InviteeRewards = []Reward{{Type: pointstore.RewardManual, Text: "x"}} }, "未登记"},
		{"points range", func(c *Config) { c.InviteeRewards = []Reward{{Type: RewardPoints}} }, "积分"},
		{"balance decimals", func(c *Config) {
			c.InviterRewards = []Reward{{Type: pointstore.RewardBalance, Money: decimal.RequireFromString("1.234")}}
		}, "两位小数"},
	}
	for _, tc := range cases {
		cfg := DefaultConfig()
		tc.mutate(&cfg)
		// 不经 Normalize 的那几项（余额精度）直接校验
		if tc.name != "balance decimals" {
			cfg = NormalizeConfig(cfg)
		}
		err := ValidateConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want contains %q", tc.name, err, tc.want)
		}
	}
}

func TestCatalogExcludesManualAndHasPoints(t *testing.T) {
	types := RewardTypes()
	if types[0] != RewardPoints {
		t.Fatalf("points should be first: %v", types)
	}
	for _, spec := range RewardCatalog() {
		if spec.Type == pointstore.RewardManual {
			t.Fatal("manual must not be configurable for invites")
		}
	}
	if len(RewardCatalog()) != len(types) {
		t.Fatalf("catalog and types disagree")
	}
}

func TestCodes(t *testing.T) {
	if got := NormalizeCode(" ab-c d　e "); got != "ABCDE" {
		t.Fatalf("normalize = %q", got)
	}
	for _, charset := range Charsets() {
		cfg := DefaultConfig()
		cfg.CodeCharset = charset
		cfg.CodeLength = 10
		cfg.CodePrefix = "VY"
		code, err := GenerateCode(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 12 || !strings.HasPrefix(code, "VY") || !ValidCodeShape(code) {
			t.Fatalf("%s: bad code %q", charset, code)
		}
		if strings.ContainsAny(code[2:], "IO") || (charset != CharsetDigits && strings.ContainsAny(code[2:], "01")) {
			t.Fatalf("%s: ambiguous characters in %q", charset, code)
		}
	}
	if ValidateCustomCode("ABC") == nil || ValidateCustomCode("AB_CD") == nil || ValidateCustomCode("ALICE2026") != nil {
		t.Fatal("custom code validation")
	}
	if ValidCodeShape(strings.Repeat("A", MaxStoredCodeLength+1)) {
		t.Fatal("too long code accepted")
	}
}

func TestShareTextAndMask(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ShareTemplate = "用 {app} 填 {code} {link}"
	if got := ShareText(cfg, "AB12", "Voyage"); got != "用 Voyage 填 AB12" {
		t.Fatalf("share without link = %q", got)
	}
	cfg.LandingURL = "https://x.test/i?c={code}"
	if got := ShareLink(cfg, "AB12"); got != "https://x.test/i?c=AB12" {
		t.Fatalf("link = %q", got)
	}
	if MaskAccount("13800138000") != "138****00" || MaskAccount("ab") != "a*" || MaskAccount("abcd") != "a**d" {
		t.Fatalf("mask: %q %q %q", MaskAccount("13800138000"), MaskAccount("ab"), MaskAccount("abcd"))
	}
	if DisplayName(" 小明 ", "x") != "小明" {
		t.Fatal("display name should prefer nickname")
	}
}

func TestEvaluateBindOrder(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	owner := int64(7)
	baseCode := &Code{ID: 1, Kind: KindPersonal, Status: CodeActive, OwnerUserID: &owner}
	base := func() BindFacts {
		return BindFacts{
			Now: now, Source: SourceBind, RegisteredAt: now.Add(-time.Hour), InviteeID: 9,
			Code: baseCode, InviterEligible: true, RewardsResolvable: true,
		}
	}
	settings := enabledSettings(func(c *Config) { c.DeviceLimit = 1; c.IPDailyLimit = 2 })

	check := func(name string, s Settings, mutate func(*BindFacts), want string) {
		t.Helper()
		facts := base()
		if mutate != nil {
			mutate(&facts)
		}
		if got := EvaluateBind(s, facts); got != want {
			t.Fatalf("%s: got %s want %s", name, got, want)
		}
	}
	check("ok", settings, nil, ReasonOK)
	check("disabled", Settings{Config: settings.Config}, nil, ReasonDisabled)
	check("bound before window", settings, func(f *BindFacts) {
		f.AlreadyBound = true
		f.RegisteredAt = now.Add(-100 * time.Hour)
	}, ReasonAlreadyBound)
	check("window", settings, func(f *BindFacts) { f.RegisteredAt = now.Add(-72 * time.Hour) }, ReasonWindowClosed)
	check("register ignores window", settings, func(f *BindFacts) {
		f.Source = SourceRegister
		f.RegisteredAt = now.Add(-1000 * time.Hour)
	}, ReasonOK)
	check("register only", enabledSettings(func(c *Config) { c.BindWindowHours = 0 }), nil, ReasonWindowClosed)
	check("not found", settings, func(f *BindFacts) { f.Code = nil }, ReasonCodeNotFound)
	check("self", settings, func(f *BindFacts) { f.InviteeID = owner }, ReasonSelfInvite)
	check("cycle", settings, func(f *BindFacts) { f.InviterParentID = 9 }, ReasonCycle)
	check("ineligible", settings, func(f *BindFacts) { f.InviterEligible = false }, ReasonInviterIneligible)
	check("campaign ignores eligibility", settings, func(f *BindFacts) {
		f.InviterEligible = false
		f.Code = &Code{Kind: KindCampaign, Status: CodeActive}
	}, ReasonOK)
	check("device", settings, func(f *BindFacts) { f.DeviceBindings = 1 }, ReasonDeviceLimited)
	check("ip", settings, func(f *BindFacts) { f.IPBindingsToday = 2 }, ReasonIPLimited)
	check("reward unavailable", settings, func(f *BindFacts) { f.RewardsResolvable = false }, ReasonRewardUnavailable)

	exhausted := int64(3)
	ended := now.Add(-time.Minute)
	later := now.Add(time.Minute)
	if CodeUsable(&Code{Status: CodeActive, MaxUses: &exhausted, UsedCount: 3}, now) != ReasonCodeExhausted ||
		CodeUsable(&Code{Status: CodeActive, EndsAt: &ended}, now) != ReasonCodeExpired ||
		CodeUsable(&Code{Status: CodeActive, StartsAt: &later}, now) != ReasonCodeNotStarted ||
		CodeUsable(&Code{Status: CodeDisabled}, now) != ReasonCodeDisabled {
		t.Fatal("code usability")
	}
	for reason := range reasonMessages {
		if ReasonMessage(reason) == "" {
			t.Fatalf("reason %s has no message", reason)
		}
	}
}

func TestInviterRules(t *testing.T) {
	cfg := NormalizeConfig(Config{InviterAudience: AudienceVip, InviterMinLevel: 3, InviterTrigger: TriggerSignIn, TriggerSignInDays: 3,
		InviterDailyLimit: 2, InviterTotalLimit: 5,
		Milestones: []Milestone{
			{Count: 1, Rewards: []Reward{{Type: RewardPoints, Amount: 1}}},
			{Count: 5, Rewards: []Reward{{Type: RewardPoints, Amount: 1}}},
		}})
	if ok, message := EvaluateInviter(cfg, false, 10); ok || !strings.Contains(message, "会员") {
		t.Fatal("vip audience")
	}
	if ok, message := EvaluateInviter(cfg, true, 2); ok || !strings.Contains(message, "Lv.3") {
		t.Fatal("level")
	}
	if ok, _ := EvaluateInviter(cfg, true, 3); !ok {
		t.Fatal("eligible")
	}
	if InviterDue(cfg, 2) || !InviterDue(cfg, 3) {
		t.Fatal("signin trigger")
	}
	if !InviterDue(NormalizeConfig(Config{}), 0) {
		t.Fatal("bind trigger is always due")
	}
	if InviterWithinLimits(cfg, 2, 2) || InviterWithinLimits(cfg, 0, 5) || !InviterWithinLimits(cfg, 1, 4) {
		t.Fatal("limits")
	}
	if due := DueMilestones(cfg, 5, []int{1}); len(due) != 1 || due[0].Count != 5 {
		t.Fatalf("milestones = %+v", due)
	}
	campaignRewards := []Reward{{Type: RewardPoints, Amount: 9}}
	if got := InviteeRewardsFor(cfg, &Code{Rewards: &campaignRewards}); len(got) != 1 || got[0].Amount != 9 {
		t.Fatal("campaign override")
	}
	if got := InviteeRewardsFor(cfg, &Code{}); len(got) != 0 {
		t.Fatal("default rewards")
	}
}

func TestCapability(t *testing.T) {
	disabled := Settings{Config: NormalizeConfig(Config{RegisterField: FieldRequired})}
	if disabled.Capability().Field != FieldHidden {
		t.Fatal("disabled invite must hide the field")
	}
	enabled := enabledSettings(func(c *Config) { c.RegisterField = FieldRequired })
	if capability := enabled.Capability(); capability.Field != FieldRequired || capability.Label == "" {
		t.Fatalf("capability = %+v", capability)
	}
}

func TestDescribePoints(t *testing.T) {
	described := DescribeReward(Reward{Type: RewardPoints, Amount: 50}, nil, nil)
	if described.Label != "积分" || described.Detail != "积分 50 点" {
		t.Fatalf("described = %+v", described)
	}
}
