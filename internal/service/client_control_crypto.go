package service

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"

	appdomain "aegis/internal/domain/app"
	apperrors "aegis/pkg/errors"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// 版本管控通道的密码学构造。与应用安全等级无关：standard 档、未登录的客户端同样加密上报，
// 同样拿到签名裁决。规格逐字节写在 docs/client-control.md，Kotlin SDK 的 AegisClientControl 与这里一一对应。
//
//	请求：X25519（客户端临时密钥 × 服务端 box 公钥）→ HKDF-SHA256 → XChaCha20-Poly1305
//	  AAD  = "aegis-client-control-v1\n{appKey}\n{keyId}\nrequest\n{timestamp}\n{nonce}"
//	  salt = SHA-256("aegis-client-control-v1:{appKey}:{keyId}")，info = AAD，L = 32
//	响应：responseKey = SHA-256(requestKey ‖ "aegis-client-control-response-v1")
//	  AAD  = "aegis-client-control-v1\n{appKey}\n{keyId}\nresponse\n{requestNonce}\n{responseNonce}"
//	裁决：Ed25519(signKey, "aegis-client-control-verdict-v1\n" ‖ verdictJSON)
//	box 公钥：Ed25519(signKey, "aegis-client-control-box-v1\n{appKey}\n{keyId}\n{publicKeyB64}")
//	密钥背书：Ed25519(上一把 signKey, "aegis-client-control-endorse-v1\n{appKey}\n{keyId}\n{publicKeyB64}")
//
// 加密保证上报内容与裁决不被旁路读取；签名保证裁决不可伪造；nonce 同时是裁决里的 challenge，
// 客户端只接受 challenge 等于本次请求 nonce 的裁决，旧的放行裁决因此无法被重放。

const (
	clientControlResponseInfo = "aegis-client-control-response-v1"
	clientControlVerdictLabel = "aegis-client-control-verdict-v1\n"
	clientControlBoxLabel     = "aegis-client-control-box-v1"
	clientControlEndorseLabel = "aegis-client-control-endorse-v1"
)

// ClientControlSealedRequest POST /client-control/check 的请求体。
type ClientControlSealedRequest struct {
	KeyID     string `json:"keyId"`
	ClientKey string `json:"clientKey"`
	Nonce     string `json:"nonce"`
	Timestamp int64  `json:"timestamp"`
	Payload   string `json:"payload"`
}

// ClientControlSealedResponse /client-control/check 的响应 data。
type ClientControlSealedResponse struct {
	KeyID   string `json:"keyId"`
	Nonce   string `json:"nonce"`
	Payload string `json:"payload"`
}

// ClientControlSignedVerdict 响应密文解开后的明文：裁决原始字节与签名。
type ClientControlSignedVerdict struct {
	Verdict      string `json:"verdict"`
	Signature    string `json:"signature"`
	SigningKeyID string `json:"signingKeyId"`
}

func clientControlRequestAAD(appKey, keyID string, timestamp int64, nonceB64 string) []byte {
	return []byte(strings.Join([]string{
		appdomain.ClientControlProtocol, appKey, keyID, "request", strconv.FormatInt(timestamp, 10), nonceB64,
	}, "\n"))
}

func clientControlResponseAAD(appKey, keyID, requestNonceB64, responseNonceB64 string) []byte {
	return []byte(strings.Join([]string{
		appdomain.ClientControlProtocol, appKey, keyID, "response", requestNonceB64, responseNonceB64,
	}, "\n"))
}

func deriveClientControlKey(shared []byte, appKey, keyID string, aad []byte) ([]byte, error) {
	salt := sha256.Sum256([]byte(appdomain.ClientControlProtocol + ":" + appKey + ":" + keyID))
	reader := hkdf.New(sha256.New, shared, salt[:], aad)
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := io.ReadFull(reader, key)
	return key, err
}

func clientControlPayloadError(message string) error {
	return apperrors.New(40000, http.StatusBadRequest, message)
}

// openClientControlRequest 用 box 私钥解开上报。返回明文与请求密钥（响应加密要用）。
func openClientControlRequest(boxPrivate []byte, appKey string, req ClientControlSealedRequest) ([]byte, []byte, error) {
	nonce, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(req.Nonce))
	if err != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, nil, clientControlPayloadError("管控请求 nonce 无效")
	}
	clientPublic, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(req.ClientKey))
	if err != nil || len(clientPublic) != 32 {
		return nil, nil, clientControlPayloadError("管控请求的客户端临时公钥无效")
	}
	curve := ecdh.X25519()
	private, err := curve.NewPrivateKey(boxPrivate)
	if err != nil {
		return nil, nil, err
	}
	peer, err := curve.NewPublicKey(clientPublic)
	if err != nil {
		return nil, nil, clientControlPayloadError("管控请求的客户端临时公钥无效")
	}
	shared, err := private.ECDH(peer)
	if err != nil {
		return nil, nil, clientControlPayloadError("管控请求密钥协商失败")
	}
	aad := clientControlRequestAAD(appKey, req.KeyID, req.Timestamp, strings.TrimSpace(req.Nonce))
	key, err := deriveClientControlKey(shared, appKey, req.KeyID, aad)
	if err != nil {
		return nil, nil, err
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(req.Payload))
	if err != nil {
		return nil, nil, clientControlPayloadError("管控载荷格式无效")
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, nil, clientControlPayloadError("管控载荷认证失败")
	}
	return plaintext, key, nil
}

// sealClientControlResponse 加密响应明文，返回响应 nonce 与密文（均为 base64url）。
func sealClientControlResponse(requestKey []byte, appKey, keyID, requestNonceB64 string, plaintext []byte) (string, string, error) {
	responseNonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, responseNonce); err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(append(append([]byte(nil), requestKey...), []byte(clientControlResponseInfo)...))
	aead, err := chacha20poly1305.NewX(digest[:])
	if err != nil {
		return "", "", err
	}
	nonceB64 := base64.RawURLEncoding.EncodeToString(responseNonce)
	sealed := aead.Seal(nil, responseNonce, plaintext, clientControlResponseAAD(appKey, keyID, requestNonceB64, nonceB64))
	return nonceB64, base64.RawURLEncoding.EncodeToString(sealed), nil
}

func signClientVerdict(private ed25519.PrivateKey, verdict []byte) []byte {
	return ed25519.Sign(private, append([]byte(clientControlVerdictLabel), verdict...))
}

// VerifyClientVerdict 供测试与服务端自检使用；客户端用同一条消息验签。
func VerifyClientVerdict(public ed25519.PublicKey, verdict, signature []byte) bool {
	return ed25519.Verify(public, append([]byte(clientControlVerdictLabel), verdict...), signature)
}

func clientControlBoxMessage(appKey, keyID, publicKeyB64 string) []byte {
	return []byte(strings.Join([]string{clientControlBoxLabel, appKey, keyID, publicKeyB64}, "\n"))
}

func clientControlEndorseMessage(appKey, keyID, publicKeyB64 string) []byte {
	return []byte(strings.Join([]string{clientControlEndorseLabel, appKey, keyID, publicKeyB64}, "\n"))
}
