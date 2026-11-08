package device

import (
	"context"
	"strings"
)

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

// HeaderPreviousID 客户端换了设备码生成方式之后，用这个请求头带上本机以前的设备码。
//
// 服务端据此把以前按旧码记下的东西迁到新码上：刷新令牌的设备绑定、登录设备检查的基线、
// 设备登录数里的旧会话、卡密的设备绑定。迁移只在当前用户自己的数据里做，
// 声称一个别人的旧码不会碰到别人的数据。
const HeaderPreviousID = "X-Device-Id-Previous"

type previousIDKey struct{}

// WithPreviousID 把本机以前的设备码挂到请求上下文上；占位值、空值或与当前设备码相同时不挂。
func WithPreviousID(ctx context.Context, previous, current string) context.Context {
	previous = NormalizeID(previous)
	if previous == "" || previous == NormalizeID(current) {
		return ctx
	}
	return context.WithValue(ctx, previousIDKey{}, previous)
}

// PreviousIDFrom 请求上下文上本机以前的设备码，没有时为空。
func PreviousIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	previous, _ := ctx.Value(previousIDKey{}).(string)
	return previous
}

// Continues 以前按 stored 记下的设备，是不是现在用 current 上报的这台：
// 两者相同；或者 stored 本来就不是有效设备码（空、占位值）；或者 stored 正是客户端声明的旧设备码。
func Continues(ctx context.Context, stored, current string) bool {
	stored = NormalizeID(stored)
	current = NormalizeID(current)
	return stored == "" || current == "" || stored == current || stored == PreviousIDFrom(ctx)
}
