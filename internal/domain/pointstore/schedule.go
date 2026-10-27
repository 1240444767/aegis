package pointstore

import (
	"fmt"
	"slices"
	"time"
)

// 时间相关的规则：补货周期、全站名额的周期、每日开放时段。全是纯函数，
// 判定（Evaluate）、列表展示与兑换事务用的是同一组。

// Clock 判定所用的「此刻」：时间点、应用默认时区，以及由二者算出的各周期起点。
type Clock struct {
	Now      time.Time
	Location *time.Location
	Windows  Windows
}

// ClockAt 按 loc 构造判定时钟；loc 为空用 time.Local。
func ClockAt(now time.Time, loc *time.Location) Clock {
	if loc == nil {
		loc = time.Local
	}
	return Clock{Now: now, Location: loc, Windows: WindowsAt(now, loc)}
}

// RestockPeriods 补货周期，顺序即控制台的展示顺序。
func RestockPeriods() []string { return []string{RestockNone, RestockDay, RestockWeek, RestockMonth} }

// ValidRestockPeriod 是否已登记的补货周期。
func ValidRestockPeriod(period string) bool { return slices.Contains(RestockPeriods(), period) }

// Restocks 是否开了补货周期。
func (item Item) Restocks() bool {
	return item.RestockPeriod != "" && item.RestockPeriod != RestockNone && item.RestockQuantity != nil
}

// CurrentStock 此刻的剩余库存，以及库里的值是否已经过期（跨进了新的补货周期）。
//
// 补货是惰性的：不跑定时任务，跨周期后第一次读到时按「补满」看待，第一笔兑换在锁住的商品行上
// 把 stock 与 stock_window_start 一起回写。这样补货与兑换落在同一把锁里，不会出现
// 「定时任务补货的同时有人在扣库存」。
func (item Item) CurrentStock(windows Windows) (stock *int64, rolled bool) {
	if !item.Restocks() {
		return item.Stock, false
	}
	start := windows.Start(item.RestockPeriod)
	if item.StockWindowStart == nil || item.StockWindowStart.Before(start) {
		quantity := *item.RestockQuantity
		return &quantity, true
	}
	return item.Stock, false
}

// NextRestockAt 下一次补货的时刻；没开补货周期为空。
func (item Item) NextRestockAt(windows Windows) *time.Time {
	if !item.Restocks() {
		return nil
	}
	return windows.Next(item.RestockPeriod)
}

// HasDailyHours 是否限定了每日开放时段。
func (item Item) HasDailyHours() bool {
	return item.DailyOpenMinute != nil && item.DailyCloseMinute != nil
}

// OpenAt 此刻是否在每日开放时段内；没限定时段时总是开放。
func (item Item) OpenAt(clock Clock) bool {
	if !item.HasDailyHours() {
		return true
	}
	local := clock.Now.In(clock.Location)
	minute := local.Hour()*60 + local.Minute()
	open, close := *item.DailyOpenMinute, *item.DailyCloseMinute
	if open < close {
		return minute >= open && minute < close
	}
	// 跨零点：22:00–02:00 意为 22 点之后或 2 点之前
	return minute >= open || minute < close
}

// NextOpenAt 下一次开放的时刻：今天的开放时刻还没到就是今天，否则是明天。在时段内或没限定时段为空。
func (item Item) NextOpenAt(clock Clock) *time.Time {
	if !item.HasDailyHours() || item.OpenAt(clock) {
		return nil
	}
	day := clock.Windows.Day
	next := time.Date(day.Year(), day.Month(), day.Day(), 0, *item.DailyOpenMinute, 0, 0, clock.Location)
	if !next.After(clock.Now) {
		next = next.AddDate(0, 0, 1)
	}
	return &next
}

// OpenHoursLabel 「10:00–22:00」；没限定时段为空串。
func (item Item) OpenHoursLabel() string {
	if !item.HasDailyHours() {
		return ""
	}
	return minuteLabel(*item.DailyOpenMinute) + "–" + minuteLabel(*item.DailyCloseMinute)
}

func minuteLabel(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

// GlobalLimited 是否设了全站名额。
func (item Item) GlobalLimited() bool { return item.GlobalLimitCount > 0 }
