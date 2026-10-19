package leaderboard

import (
	"strings"
	"testing"
	"time"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/pointstore"
)

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range Catalog() {
		if seen[spec.Key] {
			t.Fatalf("榜单 %s 重复", spec.Key)
		}
		seen[spec.Key] = true
		if spec.Label == "" || spec.ValueUnit == "" || spec.Description == "" {
			t.Errorf("%s 缺少文案", spec.Key)
		}
		switch spec.Period {
		case PeriodTotal, PeriodDay, PeriodWeek, PeriodMonth:
		default:
			t.Errorf("%s 的周期 %s 未知", spec.Key, spec.Period)
		}
	}
	for _, key := range []string{BoardPointsTotal, BoardPointsDaily, BoardPointsWeekly, BoardPointsMonthly, BoardExperience, BoardLevel} {
		if _, ok := FindBoard(key); !ok {
			t.Errorf("目录里缺少 %s", key)
		}
	}
	weekly, _ := FindBoard(BoardPointsWeekly)
	total, _ := FindBoard(BoardPointsTotal)
	if !weekly.Periodic() || total.Periodic() {
		t.Fatal("周期榜判定不对")
	}
	// 默认开启：总榜、周榜、月榜
	enabled := DefaultConfig().EnabledBoards()
	if len(enabled) != 3 || enabled[0].Key != BoardPointsTotal || enabled[1].Key != BoardPointsWeekly || enabled[2].Key != BoardPointsMonthly {
		t.Fatalf("默认开启的榜单不对：%+v", enabled)
	}
}

func TestPeriods(t *testing.T) {
	loc := shanghai(t)
	// 2026-10-19 是周一
	now := time.Date(2026, 10, 19, 0, 30, 0, 0, loc)
	cases := []struct {
		kind, key string
		start     time.Time
		end       time.Time
		label     string
	}{
		{PeriodDay, "2026-10-19", time.Date(2026, 10, 19, 0, 0, 0, 0, loc), time.Date(2026, 10, 20, 0, 0, 0, 0, loc), "今天"},
		{PeriodWeek, "2026-W43", time.Date(2026, 10, 19, 0, 0, 0, 0, loc), time.Date(2026, 10, 26, 0, 0, 0, 0, loc), "本周"},
		{PeriodMonth, "2026-10", time.Date(2026, 10, 1, 0, 0, 0, 0, loc), time.Date(2026, 11, 1, 0, 0, 0, 0, loc), "本月"},
	}
	for _, tc := range cases {
		p := PeriodAt(tc.kind, now, loc)
		if p.Key != tc.key || !p.Start.Equal(tc.start) || !p.End.Equal(tc.end) {
			t.Errorf("%s：得到 %+v", tc.kind, p)
		}
		if label := p.Label(now, loc); label != tc.label {
			t.Errorf("%s 的标签：%s", tc.kind, label)
		}
		parsed, err := ParsePeriodKey(tc.kind, tc.key, loc)
		if err != nil || !parsed.Start.Equal(p.Start) || !parsed.End.Equal(p.End) {
			t.Errorf("%s 解析 %s：%+v %v", tc.kind, tc.key, parsed, err)
		}
	}
	// 周日 23:59 仍属于本周；UTC 时刻按应用时区换算
	sunday := time.Date(2026, 10, 25, 15, 59, 0, 0, time.UTC) // 上海 23:59
	if key := PeriodAt(PeriodWeek, sunday, loc).Key; key != "2026-W43" {
		t.Fatalf("周日晚上应仍是第 43 周，得到 %s", key)
	}
	// 上一个周期
	prev := PeriodAt(PeriodWeek, now, loc).Previous()
	if prev.Key != "2026-W42" || prev.FullLabel() != "2026 年第 42 周" || prev.Label(now, loc) != "2026 年第 42 周" {
		t.Fatalf("上一周：%+v %s", prev, prev.FullLabel())
	}
	if p := PeriodAt(PeriodMonth, time.Date(2026, 1, 5, 0, 0, 0, 0, loc), loc).Previous(); p.Key != "2025-12" || p.FullLabel() != "2025 年 12 月" {
		t.Fatalf("跨年的上个月：%+v", p)
	}
	if p := PeriodAt(PeriodDay, now, loc).Previous(); p.Key != "2026-10-18" || p.FullLabel() != "2026 年 10 月 18 日" {
		t.Fatalf("昨天：%+v", p)
	}
	// ISO 周跨年：2027-01-01（周五）属于 2026 年第 53 周；2026-W53 存在，2027-W53 不存在
	if key := PeriodAt(PeriodWeek, time.Date(2027, 1, 1, 12, 0, 0, 0, loc), loc).Key; key != "2026-W53" {
		t.Fatalf("2027-01-01 应属于 2026-W53，得到 %s", key)
	}
	if p, err := ParsePeriodKey(PeriodWeek, "2026-W53", loc); err != nil || !p.Start.Equal(time.Date(2026, 12, 28, 0, 0, 0, 0, loc)) {
		t.Fatalf("2026-W53：%+v %v", p, err)
	}
	if p, err := ParsePeriodKey(PeriodWeek, "2025-W01", loc); err != nil || !p.Start.Equal(time.Date(2024, 12, 30, 0, 0, 0, 0, loc)) {
		t.Fatalf("2025-W01 从 2024-12-30 开始：%+v %v", p, err)
	}
	for _, bad := range []struct{ kind, key string }{
		{PeriodWeek, "2027-W53"}, {PeriodWeek, "2026-W00"}, {PeriodWeek, "2026-43"}, {PeriodWeek, "2026-W4"},
		{PeriodDay, "2026-10-32"}, {PeriodDay, "2026-1-5"}, {PeriodMonth, "2026-13"}, {PeriodMonth, "2026-10-01"},
		{PeriodTotal, "2026"}, {"unknown", "x"},
	} {
		if _, err := ParsePeriodKey(bad.kind, bad.key, loc); err == nil {
			t.Errorf("%s %s 应当解析失败", bad.kind, bad.key)
		}
	}
	// 总榜
	total := PeriodAt(PeriodTotal, now, loc)
	if total.Key != TotalPeriodKey || total.Label(now, loc) != "累计" || total.Ended(now.AddDate(10, 0, 0)) {
		t.Fatalf("总榜：%+v", total)
	}
	// 结束判定
	week := PeriodAt(PeriodWeek, now, loc)
	if week.Ended(week.End.Add(-time.Second)) || !week.Ended(week.End) {
		t.Fatal("周期结束判定不对")
	}
}

func TestPeriodAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("没有时区数据")
	}
	// 2026-03-08 美东进入夏令时，这一天只有 23 小时
	p := PeriodAt(PeriodDay, time.Date(2026, 3, 8, 12, 0, 0, 0, loc), loc)
	if p.End.Sub(p.Start) != 23*time.Hour || p.End.Hour() != 0 {
		t.Fatalf("夏令时那一天：%v → %v", p.Start, p.End)
	}
}

func TestMaskAccount(t *testing.T) {
	cases := map[string]string{
		"13812345678":        "138****5678",
		"+8613812345678":     "+86****5678",
		"abc@qq.com":         "ab***@q***.com",
		"Alice.Wu@Gmail.COM": "Al***@G***.com",
		"a@x.cn":             "a***@x***.cn",
		"ab@localhost":       "a***@l***",
		"alice":              "a***ce",
		"小风车旅人":              "小***旅人",
		"bob":                "b**",
		"z":                  "z**",
		"":                   "用户",
		"123456":             "1***56", // 太短不算手机号
	}
	for input, want := range cases {
		if got := MaskAccount(input); got != want {
			t.Errorf("MaskAccount(%q) = %q，期望 %q", input, got, want)
		}
	}
	if DisplayName(IdentityNickname, " 小风车 ", "13812345678") != "小风车" {
		t.Error("有昵称应显示昵称")
	}
	if DisplayName(IdentityNickname, "  ", "13812345678") != "138****5678" {
		t.Error("没有昵称应账号脱敏")
	}
	if DisplayName(IdentityMasked, "小风车", "13812345678") != "138****5678" {
		t.Error("masked 一律脱敏")
	}
	if LevelLabel(5, "旅人") != "Lv.5 旅人" || LevelLabel(2, "") != "Lv.2" || LevelLabel(3, "Lv3 学徒") != "Lv3 学徒" {
		t.Error("等级展示名不对")
	}
}

func TestPresentRow(t *testing.T) {
	// 别人匿名：不出 userId、头像、真名
	row := PresentRow(2, 10, "小风车", "storage://a", 100, "", true, 99)
	if row.UserID != nil || row.Name != AnonymousName || row.AvatarOwner != 0 || row.RawAvatar != "" || !row.Anonymous || row.IsMe {
		t.Fatalf("匿名行：%+v", row)
	}
	// 自己匿名：自己看自己显示真名
	row = PresentRow(2, 10, "小风车", "storage://a", 100, "", true, 10)
	if row.UserID == nil || *row.UserID != 10 || row.Name != "小风车" || row.AvatarOwner != 10 || !row.Anonymous || !row.IsMe {
		t.Fatalf("自己的匿名行：%+v", row)
	}
	row = PresentRow(1, 11, "阿明", "", 200, "Lv.3", false, 10)
	if row.UserID == nil || row.Name != "阿明" || row.AvatarOwner != 11 || row.Anonymous || row.LevelName != "Lv.3" {
		t.Fatalf("普通行：%+v", row)
	}
}

func points(amount int64) Reward { return Reward{Type: invite.RewardPoints, Amount: amount} }

func TestSettingsValidation(t *testing.T) {
	base := NormalizeConfig(DefaultConfig())
	if err := ValidateConfig(base); err != nil {
		t.Fatalf("默认配置应当合法：%v", err)
	}
	if len(base.Boards) != len(Catalog()) {
		t.Fatalf("默认配置应包含全部榜单：%+v", base.Boards)
	}

	// 清洗：去重、补齐、档位排序
	cfg := DefaultConfig()
	cfg.Boards = []BoardSetting{{Key: BoardPointsWeekly, Enabled: true}, {Key: BoardPointsWeekly, Enabled: false}}
	cfg.TopLimit = 0
	cfg.Identity = ""
	cfg.Rewards = map[string]BoardRewards{BoardPointsWeekly: {Enabled: true, Tiers: []Tier{
		{RankFrom: 4, RankTo: 10, Rewards: []Reward{points(100)}},
		{RankFrom: 1, RankTo: 1, Rewards: []Reward{points(500)}},
		{RankFrom: 2, RankTo: 3, Rewards: []Reward{{Type: pointstore.RewardVipDays, Amount: 3}}},
	}}}
	cfg = NormalizeConfig(cfg)
	if cfg.Boards[0].Key != BoardPointsWeekly || !cfg.Boards[0].Enabled || len(cfg.Boards) != len(Catalog()) {
		t.Fatalf("榜单清洗不对：%+v", cfg.Boards)
	}
	if cfg.TopLimit != DefaultTopLimit || cfg.Identity != IdentityNickname {
		t.Fatalf("默认值没补上：%+v", cfg)
	}
	tiers := cfg.Rewards[BoardPointsWeekly].Tiers
	if tiers[0].RankFrom != 1 || tiers[1].RankFrom != 2 || tiers[2].RankFrom != 4 {
		t.Fatalf("档位应按名次升序：%+v", tiers)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("合法配置被拒：%v", err)
	}
	if got := cfg.ActiveTiers(BoardPointsWeekly); len(got) != 3 {
		t.Fatalf("生效档位：%+v", got)
	}
	if tier, ok := TierFor(tiers, 3); !ok || tier.RankFrom != 2 {
		t.Fatalf("第 3 名应落在第 2–3 名档：%+v", tier)
	}
	if _, ok := TierFor(tiers, 11); ok {
		t.Fatal("第 11 名不该有奖励")
	}
	if TierLabel(tiers[0]) != "第 1 名" || TierLabel(tiers[1]) != "第 2–3 名" {
		t.Fatal("档位说法不对")
	}
	if len(cfg.AllRewards()) != 3 {
		t.Fatal("AllRewards 漏了")
	}

	bad := func(name string, mutate func(*Config), contains string) {
		t.Helper()
		c := NormalizeConfig(cfg)
		c.Rewards = map[string]BoardRewards{}
		for key, value := range cfg.Rewards {
			c.Rewards[key] = BoardRewards{Enabled: value.Enabled, Tiers: append([]Tier{}, value.Tiers...)}
		}
		mutate(&c)
		c = NormalizeConfig(c)
		err := ValidateConfig(c)
		if err == nil || !strings.Contains(err.Error(), contains) {
			t.Errorf("%s：期望含「%s」的错误，得到 %v", name, contains, err)
		}
	}
	bad("未知榜单", func(c *Config) { c.Boards = append(c.Boards, BoardSetting{Key: "nope"}) }, "未知的榜单")
	bad("展示名次太小", func(c *Config) { c.TopLimit = 9 }, "展示名次")
	bad("展示名次太大", func(c *Config) { c.TopLimit = 501 }, "展示名次")
	bad("门槛为负", func(c *Config) { c.MinValue = -1 }, "上榜门槛")
	bad("未知身份", func(c *Config) { c.Identity = "real" }, "展示身份")
	bad("总榜配奖励", func(c *Config) {
		c.Rewards[BoardPointsTotal] = BoardRewards{Enabled: true, Tiers: []Tier{{RankFrom: 1, RankTo: 1, Rewards: []Reward{points(1)}}}}
	}, "不能配置奖励")
	bad("未知榜单的奖励", func(c *Config) { c.Rewards["nope"] = BoardRewards{} }, "未知的榜单")
	bad("档位重叠", func(c *Config) {
		c.Rewards[BoardPointsWeekly] = BoardRewards{Enabled: true, Tiers: []Tier{
			{RankFrom: 1, RankTo: 3, Rewards: []Reward{points(1)}}, {RankFrom: 3, RankTo: 5, Rewards: []Reward{points(1)}}}}
	}, "重叠")
	bad("档位倒置", func(c *Config) {
		c.Rewards[BoardPointsWeekly] = BoardRewards{Enabled: true, Tiers: []Tier{{RankFrom: 3, RankTo: 2, Rewards: []Reward{points(1)}}}}
	}, "名次范围")
	bad("超出展示名次", func(c *Config) {
		c.TopLimit = 10
		c.Rewards[BoardPointsWeekly] = BoardRewards{Enabled: true, Tiers: []Tier{{RankFrom: 1, RankTo: 11, Rewards: []Reward{points(1)}}}}
	}, "超出了展示名次")
	bad("空奖励", func(c *Config) {
		c.Rewards[BoardPointsWeekly] = BoardRewards{Enabled: true, Tiers: []Tier{{RankFrom: 1, RankTo: 1}}}
	}, "没有配置奖励")
	bad("太多档", func(c *Config) {
		tiers := []Tier{}
		for i := 1; i <= MaxTiers+1; i++ {
			tiers = append(tiers, Tier{RankFrom: i, RankTo: i, Rewards: []Reward{points(1)}})
		}
		c.Rewards[BoardPointsWeekly] = BoardRewards{Enabled: true, Tiers: tiers}
	}, "最多")
	bad("权益不合法", func(c *Config) {
		c.Rewards[BoardPointsWeekly] = BoardRewards{Enabled: true, Tiers: []Tier{{RankFrom: 1, RankTo: 1, Rewards: []Reward{points(0)}}}}
	}, "积分")
	bad("人工发放不可配", func(c *Config) {
		c.Rewards[BoardPointsWeekly] = BoardRewards{Enabled: true, Tiers: []Tier{{RankFrom: 1, RankTo: 1,
			Rewards: []Reward{{Type: pointstore.RewardManual, Text: "周边"}}}}}
	}, "未登记的权益类型")

	// 奖励未开启时不生效
	off := NormalizeConfig(cfg)
	off.Rewards[BoardPointsWeekly] = BoardRewards{Enabled: false, Tiers: off.Rewards[BoardPointsWeekly].Tiers}
	if len(off.ActiveTiers(BoardPointsWeekly)) != 0 || len(off.ActiveTiers(BoardPointsTotal)) != 0 {
		t.Fatal("奖励关闭或总榜时不应有生效档位")
	}
}

func TestIntegralCategories(t *testing.T) {
	seen := map[string]bool{}
	for _, rule := range IntegralCategories() {
		if seen[rule.Category] {
			t.Fatalf("分类 %s 重复登记", rule.Category)
		}
		seen[rule.Category] = true
		if rule.Label == "" {
			t.Errorf("%s 缺少说明", rule.Category)
		}
	}
	excluded := ExcludedCategories()
	for _, want := range []string{"points_store_refund", "refund", IntegralCategory} {
		found := false
		for _, category := range excluded {
			found = found || category == want
		}
		if !found {
			t.Errorf("%s 必须排除", want)
		}
	}
	type tc struct {
		category      string
		amount        int64
		completed     bool
		adminOperated bool
		countAdmin    bool
		want          bool
	}
	for _, c := range []tc{
		{"daily_signin", 10, true, false, false, true},
		{"daily_signin", 10, false, false, false, false}, // 未完成
		{"daily_signin", 0, true, false, false, false},
		{"rewarded_ad", 5, true, false, false, true},
		{"invite", 5, true, false, false, true},
		{"purchase", 5, true, false, false, true},
		{"points_store", -100, true, false, false, false},
		{"points_store_refund", 100, true, false, false, false}, // 兑换取消退回不是「获得」
		{"refund", -100, true, false, false, false},
		{IntegralCategory, 500, true, false, true, false}, // 排行榜奖励不滚雪球
		{CategoryAdminAdjust, 100, true, true, false, false},
		{CategoryAdminAdjust, 100, true, true, true, true},
		{CategoryAdminAdjust, 100, true, false, false, true}, // 抽奖奖励、远程函数：没有操作管理员
		{CategoryAdminAdjust, -100, true, true, true, false},
		{"brand_new_category", 1, true, false, false, true}, // 未登记的分类按计入
	} {
		if got := Counts(c.category, c.amount, c.completed, c.adminOperated, c.countAdmin); got != c.want {
			t.Errorf("Counts(%+v) = %v", c, got)
		}
	}
}

func TestPageHelpers(t *testing.T) {
	if TotalPages(0, 20) != 0 || TotalPages(1, 20) != 1 || TotalPages(40, 20) != 2 || TotalPages(41, 20) != 3 {
		t.Fatal("TotalPages 不对")
	}
	if page, limit := NormalizePage(0, 0, 20, 50); page != 1 || limit != 20 {
		t.Fatal("NormalizePage 默认值不对")
	}
	if _, limit := NormalizePage(2, 500, 20, 50); limit != 50 {
		t.Fatal("NormalizePage 上限不对")
	}
}
