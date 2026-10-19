package leaderboard

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 周期种类。
const (
	PeriodTotal = "total"
	PeriodDay   = "day"
	PeriodWeek  = "week" // ISO 周：周一 00:00 起
	PeriodMonth = "month"
)

// TotalPeriodKey 总榜的周期键。
const TotalPeriodKey = "total"

// ErrInvalidPeriodKey 周期键格式不对，或与榜单的周期种类不符。
var ErrInvalidPeriodKey = errors.New("invalid period key")

// Period 一个周期：[Start, End)。总榜没有边界（Start、End 为零值）。
type Period struct {
	Kind  string
	Key   string
	Start time.Time
	End   time.Time
}

// PeriodAt now 所在的周期（按 loc 的自然日 / ISO 周 / 自然月）。
func PeriodAt(kind string, now time.Time, loc *time.Location) Period {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	switch kind {
	case PeriodDay:
		return periodOf(PeriodDay, day)
	case PeriodWeek:
		weekday := int(day.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return periodOf(PeriodWeek, day.AddDate(0, 0, 1-weekday))
	case PeriodMonth:
		return periodOf(PeriodMonth, time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc))
	default:
		return Period{Kind: PeriodTotal, Key: TotalPeriodKey}
	}
}

// periodOf 由起点构造周期（起点必须已对齐到该种类的边界）。
// 终点用 AddDate 推：夏令时那一天不是 24 小时，按日历走才对。
func periodOf(kind string, start time.Time) Period {
	p := Period{Kind: kind, Start: start}
	switch kind {
	case PeriodDay:
		p.End = start.AddDate(0, 0, 1)
		p.Key = start.Format("2006-01-02")
	case PeriodWeek:
		p.End = start.AddDate(0, 0, 7)
		year, week := start.ISOWeek()
		p.Key = fmt.Sprintf("%04d-W%02d", year, week)
	case PeriodMonth:
		p.End = start.AddDate(0, 1, 0)
		p.Key = start.Format("2006-01")
	}
	return p
}

// Previous 紧挨着的上一个周期。总榜没有上一个周期，原样返回。
func (p Period) Previous() Period {
	switch p.Kind {
	case PeriodDay:
		return periodOf(PeriodDay, p.Start.AddDate(0, 0, -1))
	case PeriodWeek:
		return periodOf(PeriodWeek, p.Start.AddDate(0, 0, -7))
	case PeriodMonth:
		return periodOf(PeriodMonth, p.Start.AddDate(0, -1, 0))
	default:
		return p
	}
}

// Ended 周期是否已经结束（总榜永不结束）。
func (p Period) Ended(now time.Time) bool {
	return p.Kind != PeriodTotal && !now.Before(p.End)
}

// ParsePeriodKey 按榜单的周期种类解析周期键：日 2026-10-19，周 2026-W43，月 2026-10，总榜 total。
func ParsePeriodKey(kind, key string, loc *time.Location) (Period, error) {
	if loc == nil {
		loc = time.Local
	}
	key = strings.TrimSpace(key)
	switch kind {
	case PeriodTotal:
		if key != TotalPeriodKey {
			return Period{}, ErrInvalidPeriodKey
		}
		return Period{Kind: PeriodTotal, Key: TotalPeriodKey}, nil
	case PeriodDay:
		start, err := time.ParseInLocation("2006-01-02", key, loc)
		if err != nil {
			return Period{}, ErrInvalidPeriodKey
		}
		return checkKey(periodOf(PeriodDay, start), key)
	case PeriodMonth:
		start, err := time.ParseInLocation("2006-01", key, loc)
		if err != nil {
			return Period{}, ErrInvalidPeriodKey
		}
		return checkKey(periodOf(PeriodMonth, start), key)
	case PeriodWeek:
		yearText, weekText, ok := strings.Cut(key, "-W")
		if !ok || len(yearText) != 4 || len(weekText) != 2 {
			return Period{}, ErrInvalidPeriodKey
		}
		year, err1 := strconv.Atoi(yearText)
		week, err2 := strconv.Atoi(weekText)
		if err1 != nil || err2 != nil || week < 1 || week > 53 {
			return Period{}, ErrInvalidPeriodKey
		}
		// 1 月 4 日一定落在当年的第 1 周
		jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, loc)
		weekday := int(jan4.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := jan4.AddDate(0, 0, 1-weekday+(week-1)*7)
		// 第 53 周只有部分年份有：推出来的周一回算若不是同一个周键就是不存在
		return checkKey(periodOf(PeriodWeek, start), key)
	default:
		return Period{}, ErrInvalidPeriodKey
	}
}

func checkKey(p Period, key string) (Period, error) {
	if p.Key != key {
		return Period{}, ErrInvalidPeriodKey
	}
	return p, nil
}

// Label 周期的说法：当前周期是「今天」「本周」「本月」，其余是绝对说法
// （「2026 年 10 月 19 日」「2026 年第 43 周」「2026 年 10 月」）；总榜是「累计」。
func (p Period) Label(now time.Time, loc *time.Location) string {
	if p.Kind == PeriodTotal {
		return "累计"
	}
	if PeriodAt(p.Kind, now, loc).Key == p.Key {
		switch p.Kind {
		case PeriodDay:
			return "今天"
		case PeriodWeek:
			return "本周"
		case PeriodMonth:
			return "本月"
		}
	}
	return p.FullLabel()
}

// FullLabel 绝对说法（站内信、往期列表、奖励记录用）。
func (p Period) FullLabel() string {
	switch p.Kind {
	case PeriodDay:
		return fmt.Sprintf("%d 年 %d 月 %d 日", p.Start.Year(), int(p.Start.Month()), p.Start.Day())
	case PeriodWeek:
		year, week := p.Start.ISOWeek()
		return fmt.Sprintf("%d 年第 %d 周", year, week)
	case PeriodMonth:
		return fmt.Sprintf("%d 年 %d 月", p.Start.Year(), int(p.Start.Month()))
	default:
		return "累计"
	}
}
