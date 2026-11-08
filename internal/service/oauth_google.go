package service

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	authdomain "aegis/internal/domain/auth"
	apperrors "aegis/pkg/errors"

	"github.com/golang-jwt/jwt/v5"
)

// Google 原生登录的核验。
//
// 安卓 Credential Manager（Sign in with Google）给端上的是 ID Token：一个由 Google 签名的 JWT，
// aud 是控制台里登记的「Web 应用」Client ID（也就是渠道的 ClientID），azp 是安卓客户端 ID。
// 服务端用 Google 公布的公钥验签，再核对 iss、aud、exp；身份只认 sub。
//
// 端上传来的若不是 JWT，就当 access token：先问 tokeninfo 它是签给谁的（aud 必须是 ClientID，
// 否则别的应用拿到的 Google 令牌也能冒充登录），再走 userinfo 取资料。
const (
	googleCertsURL     = "https://www.googleapis.com/oauth2/v3/certs"
	googleTokenInfoURL = "https://oauth2.googleapis.com/tokeninfo"
	googleUserInfoURL  = "https://openidconnect.googleapis.com/v1/userinfo"
	// googleKeysMaxAge 公钥缓存多久；Google 大约每周轮换，响应头给的 max-age 通常是几小时
	googleKeysMaxAge = time.Hour
	// googleKeysMinRefresh 遇到未知 kid 时强制刷新的最小间隔，免得伪造的 kid 把 Google 打满
	googleKeysMinRefresh = time.Minute
	googleRequestTimeout = 5 * time.Second
	// googleClockSkew 验 exp / iat 时容忍的时钟误差
	googleClockSkew = 2 * time.Minute
)

var googleIssuers = []string{"https://accounts.google.com", "accounts.google.com"}

// googleKeyCache Google 签名公钥（kid → 公钥），进程内共享。
type googleKeyCache struct {
	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

var googleKeys = &googleKeyCache{}

// key 取 kid 对应的公钥：缓存过期或不认识这个 kid 时去 Google 重新拉一次。
func (c *googleKeyCache) key(ctx context.Context, client *http.Client, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	age := time.Since(c.fetchedAt)
	if key, ok := c.keys[kid]; ok && age < googleKeysMaxAge {
		return key, nil
	}
	if c.keys == nil || age >= googleKeysMinRefresh {
		keys, err := fetchGoogleKeys(ctx, client)
		if err != nil {
			// 拉不到新的就先用旧的，Google 偶发抖动不该让所有人登录失败
			if key, ok := c.keys[kid]; ok {
				return key, nil
			}
			return nil, err
		}
		c.keys, c.fetchedAt = keys, time.Now()
	}
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	return nil, errors.New("google signing key not found")
}

func fetchGoogleKeys(ctx context.Context, client *http.Client) (map[string]*rsa.PublicKey, error) {
	var body struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := googleGetJSON(ctx, client, googleCertsURL, "", &body); err != nil {
		return nil, err
	}
	keys := make(map[string]*rsa.PublicKey, len(body.Keys))
	for _, item := range body.Keys {
		if item.Kty != "RSA" || item.Kid == "" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(item.N)
		e, errE := base64.RawURLEncoding.DecodeString(item.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		keys[item.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return nil, errors.New("google certs response has no usable keys")
	}
	return keys, nil
}

// googleIDTokenClaims ID Token 里用得到的字段。
type googleIDTokenClaims struct {
	jwt.RegisteredClaims
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// fetchGoogleProfile 按凭据形状选择核验方式：三段式的是 ID Token，其余当 access token。
func (p *OAuthProvider) fetchGoogleProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile, credential string) (authdomain.ProviderProfile, error) {
	if strings.TrimSpace(p.cfg.ClientID) == "" {
		return profile, apperrors.New(40012, http.StatusBadRequest, "Google 渠道未配置 Web 客户端 ID")
	}
	if strings.Count(credential, ".") == 2 {
		return p.verifyGoogleIDToken(ctx, client, profile, credential)
	}
	return p.verifyGoogleAccessToken(ctx, client, profile, credential)
}

func (p *OAuthProvider) verifyGoogleIDToken(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile, idToken string) (authdomain.ProviderProfile, error) {
	claims := &googleIDTokenClaims{}
	var keyErr error
	_, err := jwt.ParseWithClaims(idToken, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		key, err := googleKeys.key(ctx, client, kid)
		keyErr = err
		return key, err
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(p.cfg.ClientID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(googleClockSkew),
	)
	if err != nil {
		if keyErr != nil && !strings.Contains(keyErr.Error(), "not found") {
			// 连不上 Google 取公钥不是令牌的错，别让用户以为是自己授权有问题
			return profile, apperrors.New(50203, http.StatusBadGateway, "Google 身份校验失败，请稍后重试")
		}
		return profile, fmt.Errorf("google id token rejected: %w", err)
	}
	issuerOK := false
	for _, issuer := range googleIssuers {
		if claims.Issuer == issuer {
			issuerOK = true
			break
		}
	}
	if !issuerOK || claims.Subject == "" {
		return profile, errors.New("google id token has unexpected issuer or subject")
	}
	profile.ProviderUserID = claims.Subject
	profile.Nickname = claims.Name
	profile.Avatar = claims.Picture
	if googleTruthy(claims.EmailVerified) {
		profile.Email = claims.Email
	}
	profile.RawProfile = map[string]any{
		"sub": claims.Subject, "name": claims.Name, "picture": claims.Picture,
		"email": claims.Email, "email_verified": googleTruthy(claims.EmailVerified),
	}
	profile.Tokens = map[string]string{"id_token": idToken}
	return profile, nil
}

func (p *OAuthProvider) verifyGoogleAccessToken(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile, accessToken string) (authdomain.ProviderProfile, error) {
	var info struct {
		Aud string `json:"aud"`
		Azp string `json:"azp"`
		Sub string `json:"sub"`
	}
	if err := googleGetJSON(ctx, client, googleTokenInfoURL+"?"+url.Values{"access_token": {accessToken}}.Encode(), "", &info); err != nil {
		return profile, err
	}
	if info.Sub == "" || (info.Aud != p.cfg.ClientID && info.Azp != p.cfg.ClientID) {
		return profile, errors.New("google access token was issued to another client")
	}
	var user struct {
		Sub           string `json:"sub"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
	}
	if err := googleGetJSON(ctx, client, googleUserInfoURL, accessToken, &user); err != nil {
		return profile, err
	}
	if user.Sub != info.Sub {
		return profile, errors.New("google userinfo subject mismatch")
	}
	profile.ProviderUserID = user.Sub
	profile.Nickname = user.Name
	profile.Avatar = user.Picture
	if googleTruthy(user.EmailVerified) {
		profile.Email = user.Email
	}
	profile.RawProfile = map[string]any{
		"sub": user.Sub, "name": user.Name, "picture": user.Picture,
		"email": user.Email, "email_verified": googleTruthy(user.EmailVerified),
	}
	return profile, nil
}

// googleGetJSON GET 一个 Google 接口并解析 JSON；bearer 非空时带上 Authorization。
func googleGetJSON(ctx context.Context, client *http.Client, endpoint, bearer string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, googleRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("google %s http %d: %s", request.URL.Path, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

// googleTruthy email_verified 在 ID Token 里是布尔，在 tokeninfo 里是字符串 "true"。
func googleTruthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	}
	return false
}
