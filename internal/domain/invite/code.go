package invite

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"unicode/utf8"
)

// charsets 个人码的字符集。去掉易混字符：口头念、手抄、截图里看错一位就是「邀请码无效」。
var charsets = map[string]string{
	CharsetAlnum:   "ABCDEFGHJKLMNPQRSTUVWXYZ23456789",
	CharsetDigits:  "0123456789",
	CharsetLetters: "ABCDEFGHJKLMNPQRSTUVWXYZ",
}

// NormalizeCode 用户输入的邀请码 → 比对用的形态：去空白与连字符、转大写。
// 用户从分享文案里复制时常带空格，手抄时常写成小写，这些都不该算错。
func NormalizeCode(raw string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		switch r {
		case ' ', '-', '\t', '\n', '\r', '　':
			continue
		}
		builder.WriteRune(r)
	}
	return strings.ToUpper(builder.String())
}

// ValidCodeShape 只含大写字母与数字、长度在库能存的范围内。
// 查库前先挡掉明显不是邀请码的输入，省一次查询，也不让奇怪的字符进 SQL 参数。
func ValidCodeShape(code string) bool {
	if code == "" || len(code) > MaxStoredCodeLength {
		return false
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// GenerateCode 按应用的格式生成一个个人码（前缀 + 随机主体）。撞码由调用方换码重试。
func GenerateCode(cfg Config) (string, error) {
	return GenerateCodeWithPrefix(cfg, cfg.CodePrefix)
}

// GenerateCodeWithPrefix 按应用的字符集与长度、指定前缀生成（活动码批量生成用）。
func GenerateCodeWithPrefix(cfg Config, prefix string) (string, error) {
	alphabet, ok := charsets[cfg.CodeCharset]
	if !ok {
		alphabet = charsets[CharsetAlnum]
	}
	length := cfg.CodeLength
	if length < MinCodeLength || length > MaxCodeLength {
		length = DefaultCodeLength
	}
	limit := big.NewInt(int64(len(alphabet)))
	var builder strings.Builder
	builder.WriteString(prefix)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		builder.WriteByte(alphabet[n.Int64()])
	}
	return builder.String(), nil
}

// ValidateCustomCode 用户自定义的个人码 / 运营指定的活动码。
func ValidateCustomCode(code string) error {
	length := utf8.RuneCountInString(code)
	if length < MinCustomCodeLength || length > MaxCustomCodeLength {
		return fmt.Errorf("邀请码需要 %d–%d 位", MinCustomCodeLength, MaxCustomCodeLength)
	}
	if !ValidCodeShape(code) {
		return fmt.Errorf("邀请码只能使用字母与数字")
	}
	return nil
}

// ShareLink 落地页地址，{code} 换成邀请码。没配置落地页时为空。
func ShareLink(cfg Config, code string) string {
	if cfg.LandingURL == "" || code == "" {
		return ""
	}
	return strings.ReplaceAll(cfg.LandingURL, "{code}", url.QueryEscape(code))
}

// ShareText 分享文案：{code} {link} {app} 换成实际内容；没有落地页时 {link} 换成空串并收掉多余空白。
func ShareText(cfg Config, code, appName string) string {
	if cfg.ShareTemplate == "" || code == "" {
		return ""
	}
	text := strings.NewReplacer(
		"{code}", code,
		"{link}", ShareLink(cfg, code),
		"{app}", appName,
	).Replace(cfg.ShareTemplate)
	return strings.TrimSpace(text)
}

// DisplayName 用户端看到的另一个人：有昵称用昵称，没有就用打码的账号。
func DisplayName(nickname, account string) string {
	if name := strings.TrimSpace(nickname); name != "" {
		return name
	}
	return MaskAccount(account)
}

// MaskAccount 账号打码：保留首尾，中间换成星号。手机号、邮箱这类账号直接露出来就是泄露。
func MaskAccount(account string) string {
	runes := []rune(strings.TrimSpace(account))
	switch {
	case len(runes) == 0:
		return "用户"
	case len(runes) <= 2:
		return string(runes[:1]) + "*"
	case len(runes) <= 6:
		return string(runes[:1]) + strings.Repeat("*", len(runes)-2) + string(runes[len(runes)-1:])
	default:
		return string(runes[:3]) + "****" + string(runes[len(runes)-2:])
	}
}
