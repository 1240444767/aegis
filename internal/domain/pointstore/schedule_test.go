package pointstore

import (
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestCurrentStockRollsOverOnNewPeriod(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	clock := ClockAt(time.Date(2026, 10, 27, 9, 0, 0, 0, loc), loc)
	item := baseItem()
	item.RestockPeriod, item.RestockQuantity, item.Stock = RestockDay, ptr(int64(50)), ptr(int64(0))

	// 库存属于昨天：今天读到的是补满后的 50
	yesterday := clock.Windows.Day.AddDate(0, 0, -1)
	item.StockWindowStart = &yesterday
	stock, rolled := item.CurrentStock(clock.Windows)
	if !rolled || stock == nil || *stock != 50 {
		t.Fatalf("跨日应补满到 50，得到 %v rolled=%v", stock, rolled)
	}
	// 库存属于今天：照库里的数
	today := clock.Windows.Day
	item.StockWindowStart = &today
	stock, rolled = item.CurrentStock(clock.Windows)
	if rolled || *stock != 0 {
		t.Fatalf("同一天不应补货，得到 %v rolled=%v", *stock, rolled)
	}
	if got := Evaluate(item, baseFacts(), Usage{}, clock); got.Reason != ReasonSoldOut || got.Message != "已兑完，明日补货" {
		t.Fatalf("当天兑完应提示明日补货，得到 %+v", got)
	}
	next := item.NextRestockAt(clock.Windows)
	if next == nil || !next.Equal(time.Date(2026, 10, 28, 0, 0, 0, 0, loc)) {
		t.Fatalf("下次补货应在明天零点，得到 %v", next)
	}
	// 每周补货：周一零点
	item.RestockPeriod = RestockWeek
	if next := item.NextRestockAt(clock.Windows); !next.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, loc)) {
		t.Fatalf("下次周补货应在下周一，得到 %v", next)
	}
	// 没开补货：库存原样、没有补货时刻
	item.RestockPeriod, item.RestockQuantity = RestockNone, nil
	if stock, rolled := item.CurrentStock(clock.Windows); rolled || *stock != 0 || item.NextRestockAt(clock.Windows) != nil {
		t.Fatal("未开补货周期时不应补货")
	}
}

func TestDailyHours(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	at := func(h, m int) Clock { return ClockAt(time.Date(2026, 10, 27, h, m, 0, 0, loc), loc) }
	item := baseItem()
	item.DailyOpenMinute, item.DailyCloseMinute = ptr(10*60), ptr(22*60)

	cases := []struct {
		clock Clock
		open  bool
	}{{at(9, 59), false}, {at(10, 0), true}, {at(21, 59), true}, {at(22, 0), false}}
	for _, tc := range cases {
		if got := item.OpenAt(tc.clock); got != tc.open {
			t.Errorf("%s 应为 open=%v", tc.clock.Now.Format("15:04"), tc.open)
		}
	}
	if got := Evaluate(item, baseFacts(), Usage{}, at(8, 0)); got.Reason != ReasonClosedNow || got.Message != "每日 10:00–22:00 开放兑换" {
		t.Fatalf("时段外应提示开放时段，得到 %+v", got)
	}
	if next := item.NextOpenAt(at(8, 0)); !next.Equal(time.Date(2026, 10, 27, 10, 0, 0, 0, loc)) {
		t.Fatalf("早上 8 点的下次开放应是今天 10 点，得到 %v", next)
	}
	if next := item.NextOpenAt(at(23, 0)); !next.Equal(time.Date(2026, 10, 28, 10, 0, 0, 0, loc)) {
		t.Fatalf("晚上 11 点的下次开放应是明天 10 点，得到 %v", next)
	}
	if item.NextOpenAt(at(12, 0)) != nil {
		t.Fatal("时段内不应有下次开放时刻")
	}

	// 跨零点：22:00–02:00
	item.DailyOpenMinute, item.DailyCloseMinute = ptr(22*60), ptr(2*60)
	for _, tc := range []struct {
		clock Clock
		open  bool
	}{{at(23, 30), true}, {at(1, 0), true}, {at(2, 0), false}, {at(12, 0), false}} {
		if got := item.OpenAt(tc.clock); got != tc.open {
			t.Errorf("跨零点 %s 应为 open=%v", tc.clock.Now.Format("15:04"), tc.open)
		}
	}
	// 关闭写 24:00
	item.DailyOpenMinute, item.DailyCloseMinute = ptr(20*60), ptr(MinutesPerDay)
	if !item.OpenAt(at(23, 59)) || item.OpenHoursLabel() != "20:00–24:00" {
		t.Fatalf("20:00–24:00 应在 23:59 开放，标签 %q", item.OpenHoursLabel())
	}
}

func TestQuotaAndTotalLimit(t *testing.T) {
	clock := ClockAt(time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), time.UTC)
	item := baseItem()
	item.GlobalLimitPeriod, item.GlobalLimitCount = PeriodDay, 100

	if got := Evaluate(item, baseFacts(), Usage{Global: Counts{Total: 500, Day: 99}}, clock); !got.Redeemable {
		t.Fatalf("今日 99/100 应可兑，得到 %+v", got)
	}
	if got := Evaluate(item, baseFacts(), Usage{Global: Counts{Total: 500, Day: 100}}, clock); got.Reason != ReasonQuotaReached || got.Message != "今日名额已满" {
		t.Fatalf("今日 100/100 应名额已满，得到 %+v", got)
	}
	// 名额排在每人限兑之前：「大家都抢完了」比「你兑过了」更说明情况
	item.LimitPeriod, item.LimitCount = PeriodDay, 1
	if got := Evaluate(item, baseFacts(), Usage{Day: 1, Total: 1, Global: Counts{Day: 100}}, clock); got.Reason != ReasonQuotaReached {
		t.Fatalf("名额与个人次数都满时应先说名额，得到 %+v", got)
	}

	item.GlobalLimitCount = 0
	item.UserTotalLimit = 3
	if got := Evaluate(item, baseFacts(), Usage{Day: 0, Total: 3}, clock); got.Reason != ReasonLimitReached || got.Message != "已达到累计兑换次数上限" {
		t.Fatalf("累计 3/3 应提示累计上限，得到 %+v", got)
	}
	if got := Evaluate(item, baseFacts(), Usage{Day: 1, Total: 1}, clock); got.Message != "今日兑换次数已用完" {
		t.Fatalf("今日次数用完应提示今日，得到 %+v", got)
	}
}

func TestValidateScheduleInput(t *testing.T) {
	base := func() ItemInput {
		return ItemInput{Title: "测试", Cost: 10, Rewards: []Reward{{Type: RewardVipDays, Amount: 7}}}
	}
	cases := []struct {
		name    string
		mutate  func(*ItemInput)
		wantErr string
	}{
		{"补货没填数量", func(i *ItemInput) { i.RestockPeriod = RestockDay }, "每次补到的数量"},
		{"剩余多于补货量", func(i *ItemInput) { i.RestockPeriod, i.RestockQuantity, i.Stock = RestockDay, ptr(int64(5)), ptr(int64(9)) }, "不能多于"},
		{"未知补货周期", func(i *ItemInput) { i.RestockPeriod = "hour" }, "未知的补货周期"},
		{"名额为负", func(i *ItemInput) { i.GlobalLimitCount = -1 }, "全站名额"},
		{"累计少于周期次数", func(i *ItemInput) { i.LimitPeriod, i.LimitCount, i.UserTotalLimit = PeriodDay, 5, 3 }, "不能少于"},
		{"时段相同", func(i *ItemInput) { i.DailyOpenMinute, i.DailyCloseMinute = ptr(600), ptr(600) }, "不能相同"},
		{"全天写成 0–24", func(i *ItemInput) { i.DailyOpenMinute, i.DailyCloseMinute = ptr(0), ptr(MinutesPerDay) }, "不能相同"},
		{"开放越界", func(i *ItemInput) { i.DailyOpenMinute, i.DailyCloseMinute = ptr(MinutesPerDay), ptr(60) }, "开放时刻"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := base()
			tc.mutate(&input)
			err := ValidateItemInput(NormalizeItemInput(input))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("期望报「%s」，实际 %v", tc.wantErr, err)
			}
		})
	}

	// 开了补货却没填剩余：从满货开始；累计周期下的累计上限被抹掉；只填一端的时段被清空
	input := base()
	input.RestockPeriod, input.RestockQuantity = RestockWeek, ptr(int64(20))
	input.UserTotalLimit = 9
	input.DailyOpenMinute = ptr(600)
	input = NormalizeItemInput(input)
	if input.Stock == nil || *input.Stock != 20 || input.UserTotalLimit != 0 || input.DailyOpenMinute != nil {
		t.Fatalf("归一化结果不对：%+v", input)
	}
	if err := ValidateItemInput(input); err != nil {
		t.Fatalf("合法输入被拒：%v", err)
	}
}
