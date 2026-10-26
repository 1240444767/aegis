package service

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	vipdomain "aegis/internal/domain/vip"
	apperrors "aegis/pkg/errors"
)

// 权益凭证（POST /vip/feature-ticket）：服务端签名的「此人此设备此刻拥有某项权益」。
//
// ── 为什么要有它 ──
//
// `/vip/status` 的答案客户端只能信一次：落盘以后，改一个布尔值就成了会员。
// 有的功能必须离线也能判 —— 例如冷启动第一帧就要决定放不放自定义启动动画，那时还没联网。
// 凭证把判定连同签名一起交给客户端：客户端只认验得过签的凭证，验签用的公钥内置在客户端里。
//
// ── 构造（逐字节对应 Kotlin SDK 的 AegisFeatureTicket）──
//
//	签名：Ed25519(版本管控签名密钥, "aegis-feature-ticket-v1\n" ‖ ticketJSON)
//	信封：{ ticket: base64url(ticketJSON), signature, signingKeyId, keys: 签名密钥背书链 }
//
// 复用版本管控那套签名密钥（docs/client-control.md）：内置一把根公钥的客户端
// 沿 keys 里的背书链验到签名密钥，不依赖本地存储里记下的任何东西 —— 本地存储改得动，内置公钥改不动。
//
// 凭证绑定 appKey、对外用户 ID、设备、功能标识与客户端给的 digest（例如配置的摘要）：
// 换一台设备、换一个账号、改一份配置，都要回来重新签。challenge 是客户端这次请求的随机数，
// 客户端只接受 challenge 对得上的凭证作为「刚拿到的」。
//
// 只给拥有该权益的人签：没有就返回 40230，客户端据此删掉手里的旧凭证。

const (
	featureTicketLabel = "aegis-feature-ticket-v1\n"

	// 永久会员的凭证可离线用 30 天，限时会员最多 7 天且不超过到期时间。客户端每次联网都会换新的。
	featureTicketLifetimeTTL = 30 * 24 * time.Hour
	featureTicketTimedTTL    = 7 * 24 * time.Hour

	errCodeFeatureNotGranted = 40230
)

var featureTicketTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{0,128}$`)

// FeatureTicketRequest POST /vip/feature-ticket 的请求体。
type FeatureTicketRequest struct {
	Feature string `json:"feature"`
	// Digest 客户端要绑进凭证的摘要（base64url，最多 128 字符），可为空。
	Digest string `json:"digest"`
	// Nonce 客户端随机数（base64url，16–128 字符），原样写进凭证的 challenge。
	Nonce string `json:"nonce"`
}

// FeatureTicket 被签名的凭证明文。字段只增不改：老客户端忽略不认识的字段。
type FeatureTicket struct {
	V         int    `json:"v"`
	AppKey    string `json:"appKey"`
	Feature   string `json:"feature"`
	UserID    int64  `json:"userId"`
	DeviceID  string `json:"deviceId"`
	Lifetime  bool   `json:"lifetime"`
	Digest    string `json:"digest"`
	Challenge string `json:"challenge"`
	IssuedAt  int64  `json:"issuedAt"`
	ExpiresAt int64  `json:"expiresAt"`
}

// FeatureTicketEnvelope 响应 data：客户端原样保存整个信封，用时重新验签。
type FeatureTicketEnvelope struct {
	Ticket       string                    `json:"ticket"`
	Signature    string                    `json:"signature"`
	SigningKeyID string                    `json:"signingKeyId"`
	Keys         []FeatureTicketSigningKey `json:"keys"`
}

// FeatureTicketSigningKey 背书链上的一把签名公钥（与 /config 的 clientControl.signingKeys 同构）。
type FeatureTicketSigningKey struct {
	KeyID       string `json:"keyId"`
	PublicKey   string `json:"publicKey"`
	EndorsedBy  string `json:"endorsedBy,omitempty"`
	Endorsement string `json:"endorsement,omitempty"`
}

// NormalizeFeatureTicketRequest 校验请求体，返回规整后的副本。
func NormalizeFeatureTicketRequest(req FeatureTicketRequest) (FeatureTicketRequest, error) {
	req.Feature = strings.ToLower(strings.TrimSpace(req.Feature))
	req.Digest = strings.TrimSpace(req.Digest)
	req.Nonce = strings.TrimSpace(req.Nonce)
	if !vipdomain.FeatureTagPattern.MatchString(req.Feature) {
		return req, apperrors.New(40000, http.StatusBadRequest, "功能标识无效")
	}
	if !featureTicketTokenPattern.MatchString(req.Digest) {
		return req, apperrors.New(40000, http.StatusBadRequest, "digest 须为不超过 128 字符的 base64url")
	}
	if len(req.Nonce) < 16 || !featureTicketTokenPattern.MatchString(req.Nonce) {
		return req, apperrors.New(40000, http.StatusBadRequest, "nonce 须为 16–128 字符的 base64url")
	}
	return req, nil
}

// BuildFeatureTicket 由权益校验结论组装凭证明文；没有该权益时返回 40230。
func BuildFeatureTicket(app *appdomain.App, req FeatureTicketRequest, verification *vipdomain.Verification, deviceID string, now time.Time) (*FeatureTicket, error) {
	if verification == nil || !verification.Granted {
		return nil, apperrors.New(errCodeFeatureNotGranted, http.StatusPaymentRequired, "当前账号未拥有该会员权益")
	}
	membership := verification.Membership
	expires := now.Add(featureTicketTimedTTL)
	if membership.IsLifetime {
		expires = now.Add(featureTicketLifetimeTTL)
	} else if membership.ExpireAt != nil && membership.ExpireAt.Before(expires) {
		expires = *membership.ExpireAt
	}
	// 单独解锁的功能（不靠会员身份）也可能有到期时间
	for _, unlock := range membership.Unlocks {
		if strings.EqualFold(unlock.Tag, req.Feature) && unlock.ExpireAt != nil && !membership.IsVIP && unlock.ExpireAt.Before(expires) {
			expires = *unlock.ExpireAt
		}
	}
	if !expires.After(now) {
		return nil, apperrors.New(errCodeFeatureNotGranted, http.StatusPaymentRequired, "当前账号未拥有该会员权益")
	}
	return &FeatureTicket{
		V:         1,
		AppKey:    app.AppKey,
		Feature:   req.Feature,
		UserID:    verification.UserID,
		DeviceID:  strings.TrimSpace(deviceID),
		Lifetime:  membership.IsLifetime,
		Digest:    req.Digest,
		Challenge: req.Nonce,
		IssuedAt:  now.Unix(),
		ExpiresAt: expires.Unix(),
	}, nil
}

// SignFeatureTicket 用应用当前的版本管控签名密钥签凭证，并附上全部签名公钥的背书链。
func (s *ClientControlService) SignFeatureTicket(ctx context.Context, app *appdomain.App, ticket *FeatureTicket) (*FeatureTicketEnvelope, error) {
	if ticket == nil {
		return nil, errors.New("feature ticket is nil")
	}
	raw, err := json.Marshal(ticket)
	if err != nil {
		return nil, err
	}
	ring, err := s.keyring(ctx, app)
	if err != nil {
		return nil, err
	}
	sign := ring.active(appdomain.ClientControlKeySign)
	if sign == nil {
		return nil, errors.New("client control signing key unavailable")
	}
	s.keyMu.Lock()
	signer, err := s.signingKey(ring, sign)
	s.keyMu.Unlock()
	if err != nil {
		return nil, err
	}
	envelope := &FeatureTicketEnvelope{
		Ticket:       base64.RawURLEncoding.EncodeToString(raw),
		Signature:    base64.RawURLEncoding.EncodeToString(signFeatureTicket(signer, raw)),
		SigningKeyID: sign.KeyID,
	}
	for _, key := range ring.keys {
		if key.Kind != appdomain.ClientControlKeySign {
			continue
		}
		view := clientControlKeyView(key)
		envelope.Keys = append(envelope.Keys, FeatureTicketSigningKey{
			KeyID: view.KeyID, PublicKey: view.PublicKey, EndorsedBy: view.EndorsedBy, Endorsement: view.Endorsement,
		})
	}
	return envelope, nil
}

func signFeatureTicket(private ed25519.PrivateKey, ticket []byte) []byte {
	return ed25519.Sign(private, append([]byte(featureTicketLabel), ticket...))
}

// VerifyFeatureTicket 供测试与服务端自检使用；客户端用同一条消息验签。
func VerifyFeatureTicket(public ed25519.PublicKey, ticket, signature []byte) bool {
	return ed25519.Verify(public, append([]byte(featureTicketLabel), ticket...), signature)
}
