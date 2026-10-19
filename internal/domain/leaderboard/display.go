package leaderboard

import (
	"strconv"
	"strings"
)

// DisplayName 对外展示的名字（不含匿名）。identity = nickname 且有昵称时用昵称，否则账号脱敏。
// 接口永远不返回原始账号：账号可能就是手机号、邮箱。
func DisplayName(identity, nickname, account string) string {
	if identity != IdentityMasked {
		if name := strings.TrimSpace(nickname); name != "" {
			return name
		}
	}
	return MaskAccount(account)
}

// MaskAccount 账号脱敏：
//
//   - 手机号（可带 +，7–15 位数字）：前 3 位 + **** + 后 4 位，如 138****5678
//   - 邮箱：本地部分前 2 个字符 + ***，域名首字符 + *** + 顶级域，如 ab***@q***.com
//   - 其它：首字符 + *** + 末两位；长度 ≤ 3 时首字符 + **
func MaskAccount(account string) string {
	account = strings.TrimSpace(account)
	runes := []rune(account)
	if len(runes) == 0 {
		return "用户"
	}
	if isPhoneLike(account) {
		return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
	}
	if at := strings.LastIndex(account, "@"); at > 0 && at < len(account)-1 {
		return maskEmailLocal(account[:at]) + "@" + maskEmailDomain(account[at+1:])
	}
	if len(runes) <= 3 {
		return string(runes[:1]) + "**"
	}
	return string(runes[:1]) + "***" + string(runes[len(runes)-2:])
}

func isPhoneLike(account string) bool {
	digits := strings.TrimPrefix(account, "+")
	if len(digits) < 7 || len(digits) > 15 {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func maskEmailLocal(local string) string {
	runes := []rune(local)
	keep := 2
	if len(runes) <= 2 {
		keep = 1
	}
	return string(runes[:keep]) + "***"
}

func maskEmailDomain(domain string) string {
	dot := strings.LastIndex(domain, ".")
	if dot <= 0 || dot == len(domain)-1 {
		runes := []rune(domain)
		return string(runes[:1]) + "***"
	}
	name := []rune(domain[:dot])
	return string(name[:1]) + "***" + strings.ToLower(domain[dot:])
}

// LevelLabel 等级的展示名：「Lv.5 旅人」；等级名为空时只有「Lv.5」，等级名自己已经带 Lv 时原样。
func LevelLabel(level int, name string) string {
	name = strings.TrimSpace(name)
	prefix := "Lv." + strconv.Itoa(level)
	switch {
	case name == "":
		return prefix
	case strings.HasPrefix(strings.ToLower(name), "lv"):
		return name
	default:
		return prefix + " " + name
	}
}
