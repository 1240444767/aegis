package user

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// 个人设置里的自动签到（user_settings.autoSign）。
//
// 这一类设置会被调度器读去决定几点签、失败重试几次，所以写入前必须清洗成固定形状：
// 以前原样存客户端的 map，"time": "8:5"、"maxRetries": "3" 之类的值进了库，
// 调度器只能按 00:00 和默认值兜底，用户看到的设置与实际行为对不上。

// 自动签到设置的取值范围。
const (
	AutoSignMaxRetriesLimit   = 10
	AutoSignDefaultMaxRetries = 3
	AutoSignDefaultTime       = "08:00"
)

// AutoSignSettings 清洗后的自动签到设置。
type AutoSignSettings struct {
	Enabled                 bool   `json:"enabled"`
	Time                    string `json:"time"`
	RetryOnFail             bool   `json:"retryOnFail"`
	MaxRetries              int    `json:"maxRetries"`
	NotifyOnSuccess         bool   `json:"notifyOnSuccess"`
	NotifyOnFail            bool   `json:"notifyOnFail"`
	DisableLocationTracking bool   `json:"disableLocationTracking"`
}

// DefaultAutoSignSettings 首次初始化时写入的默认值。
func DefaultAutoSignSettings() AutoSignSettings {
	return AutoSignSettings{
		Enabled:                 false,
		Time:                    AutoSignDefaultTime,
		RetryOnFail:             true,
		MaxRetries:              AutoSignDefaultMaxRetries,
		NotifyOnSuccess:         true,
		NotifyOnFail:            true,
		DisableLocationTracking: true,
	}
}

// ToMap 存库的形状。
func (s AutoSignSettings) ToMap() map[string]any {
	return map[string]any{
		"enabled":                 s.Enabled,
		"time":                    s.Time,
		"retryOnFail":             s.RetryOnFail,
		"maxRetries":              s.MaxRetries,
		"notifyOnSuccess":         s.NotifyOnSuccess,
		"notifyOnFail":            s.NotifyOnFail,
		"disableLocationTracking": s.DisableLocationTracking,
	}
}

// Clock 解析出的时、分。Time 已经过清洗，解析不会失败。
func (s AutoSignSettings) Clock() (int, int) {
	parsed, err := time.Parse("15:04", s.Time)
	if err != nil {
		return 0, 0
	}
	return parsed.Hour(), parsed.Minute()
}

// AutoSignSettingsFromMap 从库里的 map 读出设置；缺的、坏的字段取默认值，不报错。
func AutoSignSettingsFromMap(raw map[string]any) AutoSignSettings {
	settings := DefaultAutoSignSettings()
	if raw == nil {
		return settings
	}
	if value, ok := raw["enabled"].(bool); ok {
		settings.Enabled = value
	}
	if value, ok := raw["time"].(string); ok {
		if normalized, err := NormalizeAutoSignTime(value); err == nil {
			settings.Time = normalized
		}
	}
	if value, ok := raw["retryOnFail"].(bool); ok {
		settings.RetryOnFail = value
	}
	if value, ok := intFromAny(raw["maxRetries"]); ok && value >= 0 && value <= AutoSignMaxRetriesLimit {
		settings.MaxRetries = value
	}
	if value, ok := raw["notifyOnSuccess"].(bool); ok {
		settings.NotifyOnSuccess = value
	}
	if value, ok := raw["notifyOnFail"].(bool); ok {
		settings.NotifyOnFail = value
	}
	if value, ok := raw["disableLocationTracking"].(bool); ok {
		settings.DisableLocationTracking = value
	}
	return settings
}

// MergeAutoSignSettings 把客户端提交的字段合并到当前设置上，并逐项校验。
//
// 只改提交了的字段：客户端只想关掉开关时不必把时刻、重试次数原样回传一遍。
// 未知字段忽略；已知字段类型或取值不对时报错，而不是悄悄换成默认值。
func MergeAutoSignSettings(current AutoSignSettings, patch map[string]any) (AutoSignSettings, error) {
	out := current
	for key, value := range patch {
		switch key {
		case "enabled", "retryOnFail", "notifyOnSuccess", "notifyOnFail", "disableLocationTracking":
			flag, ok := value.(bool)
			if !ok {
				return current, fmt.Errorf("%s 需要是布尔值", key)
			}
			switch key {
			case "enabled":
				out.Enabled = flag
			case "retryOnFail":
				out.RetryOnFail = flag
			case "notifyOnSuccess":
				out.NotifyOnSuccess = flag
			case "notifyOnFail":
				out.NotifyOnFail = flag
			case "disableLocationTracking":
				out.DisableLocationTracking = flag
			}
		case "time":
			text, ok := value.(string)
			if !ok {
				return current, fmt.Errorf("time 需要是 HH:MM 格式的字符串")
			}
			normalized, err := NormalizeAutoSignTime(text)
			if err != nil {
				return current, err
			}
			out.Time = normalized
		case "maxRetries":
			count, ok := intFromAny(value)
			if !ok || count < 0 || count > AutoSignMaxRetriesLimit {
				return current, fmt.Errorf("maxRetries 需要是 0–%d 之间的整数", AutoSignMaxRetriesLimit)
			}
			out.MaxRetries = count
		}
	}
	return out, nil
}

// NormalizeAutoSignTime 接受 H:M、HH:MM，统一成 HH:MM。
func NormalizeAutoSignTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	var hour, minute int
	var rest string
	if n, _ := fmt.Sscanf(value, "%d:%d%s", &hour, &minute, &rest); n < 2 || rest != "" {
		return "", fmt.Errorf("签到时刻格式不正确，应为 HH:MM")
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return "", fmt.Errorf("签到时刻超出范围，应在 00:00–23:59 之间")
	}
	return fmt.Sprintf("%02d:%02d", hour, minute), nil
}

// intFromAny JSON 解出来的数字是 float64；只接受整数值。
func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v != math.Trunc(v) || math.IsInf(v, 0) || math.IsNaN(v) {
			return 0, false
		}
		return int(v), true
	default:
		return 0, false
	}
}

// 最近一次自动签到的结果。
const (
	AutoSignRunSuccess       = "success"        // 签到成功
	AutoSignRunAlreadySigned = "already_signed" // 到点时今天已经签过
	AutoSignRunRetrying      = "retrying"       // 失败，稍后重试
	AutoSignRunFailed        = "failed"         // 失败且不再重试，次日再签
	AutoSignRunSuspended     = "suspended"      // 失去使用资格，已暂停
)

// AutoSignRun 最近一次自动签到的结果，给用户看「上次签到成功了没有」。
type AutoSignRun struct {
	At               time.Time `json:"at"`
	Status           string    `json:"status"`
	Message          string    `json:"message,omitempty"`
	SignDate         string    `json:"signDate,omitempty"`
	IntegralReward   int64     `json:"integralReward,omitempty"`
	ExperienceReward int64     `json:"experienceReward,omitempty"`
	ConsecutiveDays  int       `json:"consecutiveDays,omitempty"`
	Attempt          int       `json:"attempt,omitempty"`
}

// AutoSignAccess 应用开放策略对当前用户的结论。
type AutoSignAccess struct {
	// Enabled 应用是否开放了自动签到
	Enabled bool `json:"enabled"`
	// Audience 开放对象：all / vip / lifetime / feature
	Audience     string `json:"audience"`
	IncludeTrial bool   `json:"includeTrial"`
	FeatureTag   string `json:"featureTag,omitempty"`
	FeatureName  string `json:"featureName,omitempty"`
}

// AutoSignStatus 自动签到的完整状态：开放策略、资格、个人设置、排班与最近一次结果。
type AutoSignStatus struct {
	// Available 当前用户有资格使用自动签到
	Available bool `json:"available"`
	// Reason 没有资格的原因：disabled / vip_required / trial_excluded / lifetime_required / feature_required
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
	// Active 已开启且有资格，到点会自动签到
	Active   bool             `json:"active"`
	Settings AutoSignSettings `json:"settings"`
	Access   AutoSignAccess   `json:"access"`
	// Timezone 签到时刻按这个时区解释
	Timezone  string       `json:"timezone"`
	NextRunAt *time.Time   `json:"nextRunAt,omitempty"`
	LastRun   *AutoSignRun `json:"lastRun,omitempty"`
}
