package device

import "strings"

// NormalizeID 客户端上报的设备标识：去掉首尾空白；占位值一律视为没有上报，返回空串。
//
// 占位值是系统或厂商在拿不到真实标识时给的固定值，所有这类设备都一样：
//   - 全零：用户关闭广告追踪后的 OAID（各家长度不同，64 位、32 位、UUID 形式都有）、
//     部分 ROM 的 ANDROID_ID、IDFA 被限制时的 00000000-0000-0000-0000-000000000000
//   - 9774d56d682e549c：早期 Android 的 ANDROID_ID 缺陷值，一代设备都报这一个
//   - unknown / null 等：客户端把空值序列化成了字符串
//
// 原样采信的话，这些设备会在设备登录数、登录设备检查、卡密绑定、设备台账里被当成同一台：
// 互相挤下线、共用一张卡密、换绑冷却互相影响。按没上报处理时，设备 Key 退回 UA 摘要。
func NormalizeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if _, ok := placeholderIDs[strings.ToLower(id)]; ok {
		return ""
	}
	for _, r := range id {
		switch r {
		case '0', '-', ':', '_', ' ', '.':
		default:
			return id
		}
	}
	// 只剩分隔符与 0
	return ""
}

// IsPlaceholderID 上报了值但只是占位值（不含空串）。
func IsPlaceholderID(id string) bool {
	id = strings.TrimSpace(id)
	return id != "" && NormalizeID(id) == ""
}

var placeholderIDs = map[string]struct{}{
	"9774d56d682e549c": {},
	"unknown":          {},
	"null":             {},
	"nil":              {},
	"none":             {},
	"undefined":        {},
}
