package aigateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// KeyPrefix 用户 API 密钥的前缀。网关凭前缀区分「密钥」与「登录会话令牌」。
const KeyPrefix = "sk-ag-"

// keySecretLength 前缀之后的随机部分长度（base62）：43 位约 256 bit。
const keySecretLength = 43

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// GenerateKey 生成一把新密钥，返回明文、散列与展示用的提示（前 10 位 + 后 4 位）。
func GenerateKey() (secret string, hash string, hint string, err error) {
	alphabet := big.NewInt(int64(len(base62Alphabet)))
	var builder strings.Builder
	builder.Grow(len(KeyPrefix) + keySecretLength)
	builder.WriteString(KeyPrefix)
	for i := 0; i < keySecretLength; i++ {
		n, err := rand.Int(rand.Reader, alphabet)
		if err != nil {
			return "", "", "", err
		}
		builder.WriteByte(base62Alphabet[n.Int64()])
	}
	secret = builder.String()
	return secret, HashKey(secret), KeyHint(secret), nil
}

// HashKey 密钥的散列。密钥本身是 256 bit 随机数，不需要加盐或慢散列。
func HashKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// KeyHint 列表里展示的提示：sk-ag-AbCd…wxyz。
func KeyHint(secret string) string {
	if len(secret) <= len(KeyPrefix)+8 {
		return secret
	}
	return secret[:len(KeyPrefix)+4] + "…" + secret[len(secret)-4:]
}

// LooksLikeKey 令牌是不是网关密钥的形状（否则按登录会话令牌处理）。
func LooksLikeKey(token string) bool {
	if !strings.HasPrefix(token, KeyPrefix) || len(token) != len(KeyPrefix)+keySecretLength {
		return false
	}
	for i := len(KeyPrefix); i < len(token); i++ {
		if !strings.ContainsRune(base62Alphabet, rune(token[i])) {
			return false
		}
	}
	return true
}

// MaxSpendLimit 单把密钥的消费上限（点）。
var MaxSpendLimit = decimal.NewFromInt(1_000_000_000)

// NormalizeKeyInput 校验密钥入参。creating 为真时状态只能是 active。
func NormalizeKeyInput(in KeyInput, creating bool, now time.Time) (KeyInput, error) {
	out := in
	out.Name = strings.TrimSpace(out.Name)
	if !utf8.ValidString(out.Name) || utf8.RuneCountInString(out.Name) > 64 {
		return out, invalid("密钥名称不能超过 64 个字")
	}
	out.Status = strings.TrimSpace(out.Status)
	if creating {
		out.Status = KeyActive
	} else if out.Status != "" && out.Status != KeyActive && out.Status != KeyDisabled {
		return out, invalid("密钥状态只能是 active 或 disabled")
	}
	if len(out.Models) > 100 {
		return out, invalid("可用模型最多 100 个")
	}
	seen := map[string]bool{}
	models := make([]string, 0, len(out.Models))
	for _, raw := range out.Models {
		model := strings.TrimSpace(raw)
		if model == "" || seen[model] {
			continue
		}
		if !ValidModelName(model) {
			return out, invalid("模型名「%s」不合法", raw)
		}
		seen[model] = true
		models = append(models, model)
	}
	out.Models = models
	if out.SpendLimit.Sign() < 0 || out.SpendLimit.GreaterThan(MaxSpendLimit) {
		return out, invalid("消费上限应在 0-%s 点之间（0 不限）", MaxSpendLimit.String())
	}
	if out.ExpiresAt != nil && !out.ExpiresAt.After(now) {
		return out, invalid("到期时间必须晚于现在")
	}
	return out, nil
}

func loadLocation(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}
