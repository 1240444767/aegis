package accountdeletion

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := NormalizeConfig(DefaultConfig())
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("默认配置应当合法：%v", err)
	}
	if cfg.DefaultMode != ModeCooling || !cfg.AllowCooling || cfg.AllowImmediate {
		t.Fatalf("默认只开放冷静期注销：%+v", cfg)
	}
}

func TestNormalizeConfigFollowsSingleMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowCooling, cfg.AllowImmediate, cfg.DefaultMode = false, true, ModeCooling
	cfg = NormalizeConfig(cfg)
	if cfg.DefaultMode != ModeImmediate {
		t.Fatalf("只开放立即注销时默认方式应当跟着变：%s", cfg.DefaultMode)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestValidateConfigRejects(t *testing.T) {
	cases := map[string]func(*Config){
		"两种方式都关":   func(c *Config) { c.AllowCooling, c.AllowImmediate = false, false },
		"冷静期过长":    func(c *Config) { c.CoolingDays = 181 },
		"未知验证方式":   func(c *Config) { c.VerifyMethods = []string{"face"} },
		"无验证也不放行":  func(c *Config) { c.VerifyMethods, c.RecentLoginMinutes = nil, 0 },
		"原因标识非法":   func(c *Config) { c.Reasons = []Reason{{Code: "No Space", Label: "x"}} },
		"必填原因却无选项": func(c *Config) { c.ReasonRequired, c.Reasons, c.AllowCustomReason = true, nil, false },
		"前置条件档位非法": func(c *Config) { c.Guards.WalletBalance = "deny" },
		"登录处理非法":   func(c *Config) { c.LoginDuringCooling = "deny" },
		"重新注册限制过长": func(c *Config) { c.ReregisterDays = MaxReregisterDays + 1 },
		"默认方式未开放":  func(c *Config) { c.AllowImmediate, c.DefaultMode = true, ModeImmediate; c.AllowImmediate = false },
	}
	for name, mutate := range cases {
		cfg := DefaultConfig()
		mutate(&cfg)
		// 不走 Normalize 的场景要原样校验（Normalize 会替「默认方式未开放」兜底）
		if name != "默认方式未开放" {
			cfg = NormalizeConfig(cfg)
		}
		if err := ValidateConfig(cfg); err == nil {
			t.Errorf("%s：应当被拒绝", name)
		}
	}
}

func TestEvaluateGuards(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	future := now.Add(48 * time.Hour)
	cfg := DefaultConfig()
	facts := Facts{PendingOrders: 2, WalletBalance: "12.50", WalletPositive: true, VipExpireAt: &future, Points: 30}
	checks := EvaluateGuards(cfg, facts, now)
	keys := make([]string, 0, len(checks))
	for _, check := range checks {
		keys = append(keys, check.Key)
	}
	// points 默认 off，不出现
	if strings.Join(keys, ",") != "activeBan,pendingOrders,pendingRefunds,walletBalance,activeVip,lifetimeVip" {
		t.Fatalf("前置条件顺序或范围不对：%v", keys)
	}
	blocking := FirstBlocking(checks)
	if blocking == nil || blocking.Key != GuardPendingOrders || !strings.Contains(blocking.Message, "2 笔") {
		t.Fatalf("待支付订单应当挡住：%+v", blocking)
	}
	for _, check := range checks {
		if check.Key == GuardWalletBalance && (check.Passed || check.Level != GuardWarn) {
			t.Fatalf("余额只提示不阻止：%+v", check)
		}
	}
	facts.PendingOrders = 0
	if blocking := FirstBlocking(EvaluateGuards(cfg, facts, now)); blocking != nil {
		t.Fatalf("没有 block 项未通过时应当放行：%+v", blocking)
	}
}

func TestPlanVerification(t *testing.T) {
	cfg := DefaultConfig()
	plan := PlanVerification(cfg, Availability{Password: true, Email: "a***@x.com", TOTP: true})
	if !plan.Available || len(plan.Methods) != 2 || plan.Methods[0].Type != VerifyPassword || plan.Methods[1].Masked != "a***@x.com" || !plan.TOTP {
		t.Fatalf("密码与邮箱任选其一并要求两步验证：%+v", plan)
	}

	plan = PlanVerification(cfg, Availability{})
	if !plan.Available || !plan.Session || len(plan.Methods) != 0 {
		t.Fatalf("一种方式都没有时按最近登录放行：%+v", plan)
	}

	cfg.RecentLoginMinutes = 0
	plan = PlanVerification(cfg, Availability{})
	if plan.Available || plan.Message == "" {
		t.Fatalf("不允许回落时应当不可用并说明原因：%+v", plan)
	}

	plan = PlanVerification(cfg, Availability{TOTP: true})
	if !plan.Available || !plan.TOTP {
		t.Fatalf("只开了两步验证时动态码本身足够：%+v", plan)
	}

	cfg.VerifyMethods = []string{VerifyPhone}
	plan = PlanVerification(cfg, Availability{Password: true, Phone: "138****0000"})
	if len(plan.Methods) != 1 || plan.Methods[0].Type != VerifyPhone {
		t.Fatalf("只列出配置里勾选的方式：%+v", plan)
	}
}

func TestResolveReason(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ReasonRequired = true
	if _, _, err := ResolveReason(cfg, "", ""); err == nil {
		t.Fatal("必填时不选不填应当被拒")
	}
	if _, _, err := ResolveReason(cfg, "unknown", ""); err == nil {
		t.Fatal("不在列表里的原因应当被拒")
	}
	code, text, err := ResolveReason(cfg, " PRIVACY ", "  说明  ")
	if err != nil || code != "privacy" || text != "说明" {
		t.Fatalf("原因应当被整理：%q %q %v", code, text, err)
	}
	cfg.AllowCustomReason = false
	if _, text, _ := ResolveReason(cfg, "privacy", "多余"); text != "" {
		t.Fatal("不允许自填时丢掉文字")
	}
}

func TestStatusFlow(t *testing.T) {
	if InitialStatus(ModeCooling, true) != StatusReviewing || InitialStatus(ModeImmediate, false) != StatusProcessing ||
		InitialStatus(ModeCooling, false) != StatusScheduled {
		t.Fatal("初始状态不对")
	}
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	due, remind := Schedule(now, 15, 24)
	if !due.Equal(now.AddDate(0, 0, 15)) || remind == nil || !remind.Equal(due.Add(-24*time.Hour)) {
		t.Fatalf("到期与提醒时刻不对：%v %v", due, remind)
	}
	if _, remind := Schedule(now, 1, 48); remind != nil {
		t.Fatal("提醒时刻早于现在时不提醒")
	}
	if !UserCanCancel(StatusFailed) || UserCanCancel(StatusProcessing) || UserCanCancel(StatusCompleted) {
		t.Fatal("本人撤销的状态范围不对")
	}
	if AdminCanExecute(StatusReviewing) || !AdminCanExecute(StatusFailed) {
		t.Fatal("管理员立即执行的状态范围不对")
	}
}

func TestModeOptionsOrder(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowImmediate, cfg.DefaultMode = true, ModeImmediate
	options := ModeOptions(cfg)
	if len(options) != 2 || options[0].Mode != ModeImmediate {
		t.Fatalf("默认方式排第一：%+v", options)
	}
}

func TestNormalizeIdentifier(t *testing.T) {
	if NormalizeIdentifier(TombstoneEmail, " A@B.Com ") != "a@b.com" {
		t.Fatal("邮箱转小写")
	}
	if NormalizeIdentifier(TombstonePhone, "138-0000 (1111)") != "13800001111" {
		t.Fatal("手机号去分隔符")
	}
}
