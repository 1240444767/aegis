package mailtpl

import (
	"fmt"
	"strings"
	"time"

	"github.com/inbucket/html2text"
	"github.com/osteele/liquid"
)

// 在 Liquid 标准过滤器（upcase、default、date、truncate、replace……）之外补的几个，
// 都是写邮件时常用、自己拼又容易写错的。
func registerFilters(engine *liquid.Engine) {
	// mask_email: a***e@example.com
	engine.RegisterFilter("mask_email", func(value any) string { return MaskEmail(fmt.Sprint(valueOr(value, ""))) })
	// mask_phone: 138****8000
	engine.RegisterFilter("mask_phone", func(value any) string { return MaskPhone(fmt.Sprint(valueOr(value, ""))) })
	// datetime: 按时区格式化时间，{{ now | datetime: "Asia/Shanghai" }}，第二个参数是 Go 布局（可省略）
	engine.RegisterFilter("datetime", func(value any, zone func(string) string, layout func(string) string) string {
		at, ok := toTime(value)
		if !ok {
			return fmt.Sprint(valueOr(value, ""))
		}
		loc := time.Local
		if name := strings.TrimSpace(zone("")); name != "" {
			if parsed, err := time.LoadLocation(name); err == nil {
				loc = parsed
			}
		}
		return at.In(loc).Format(firstNonEmpty(layout(""), "2006-01-02 15:04"))
	})
	// minutes_text: 90 → 1 小时 30 分钟
	engine.RegisterFilter("minutes_text", func(value any) string {
		minutes := 0
		fmt.Sscan(fmt.Sprint(valueOr(value, "0")), &minutes)
		if minutes < 60 {
			return fmt.Sprintf("%d 分钟", minutes)
		}
		if minutes%60 == 0 {
			return fmt.Sprintf("%d 小时", minutes/60)
		}
		return fmt.Sprintf("%d 小时 %d 分钟", minutes/60, minutes%60)
	})
}

func toTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case *time.Time:
		if typed != nil {
			return *typed, true
		}
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, strings.TrimSpace(typed)); err == nil {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

// MaskEmail 邮箱打码：保留首尾各一个字符。
func MaskEmail(value string) string {
	value = strings.TrimSpace(value)
	at := strings.LastIndex(value, "@")
	if at <= 0 {
		return value
	}
	local, domain := []rune(value[:at]), value[at:]
	switch len(local) {
	case 1:
		return "*" + domain
	case 2:
		return string(local[:1]) + "*" + domain
	default:
		return string(local[:1]) + strings.Repeat("*", len(local)-2) + string(local[len(local)-1:]) + domain
	}
}

// MaskPhone 手机号打码：保留前三后四。
func MaskPhone(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= 7 {
		return "***"
	}
	return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
}

// HTMLToText 从 HTML 抓纯文本。只用于「正文是一段自定义 HTML / MJML、又没写纯文本模板」的信。
func HTMLToText(source string) string {
	text, err := html2text.FromString(source, html2text.Options{OmitLinks: false, TextOnly: false})
	if err != nil {
		return strings.TrimSpace(source)
	}
	return collapseBlankLines(text)
}
