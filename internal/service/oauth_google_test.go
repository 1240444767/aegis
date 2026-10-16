package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aegis/internal/config"
	oauthdomain "aegis/internal/domain/oauth"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testGoogleWebClient     = "1234-web.apps.googleusercontent.com"
	testGoogleAndroidClient = "1234-android.apps.googleusercontent.com"
)

// fakeGoogle 模拟 www.googleapis.com/oauth2/v3/certs、tokeninfo 与 userinfo。
type fakeGoogle struct {
	mu         sync.Mutex
	key        *rsa.PrivateKey
	kid        string
	certsDown  bool
	certsCalls int
	// accessTokens access_token → tokeninfo 里的 aud 与 sub
	accessTokens map[string][2]string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeGoogle{key: key, kid: "kid-1", accessTokens: map[string][2]string{}}
}

func (f *fakeGoogle) client() *http.Client { return &http.Client{Transport: fakeGoogleTransport{f}} }

type fakeGoogleTransport struct{ f *fakeGoogle }

func (t fakeGoogleTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f := t.f
	f.mu.Lock()
	defer f.mu.Unlock()
	recorder := httptest.NewRecorder()
	writeJSON := func(value any) { _ = json.NewEncoder(recorder).Encode(value) }
	switch request.URL.Host + request.URL.Path {
	case "www.googleapis.com/oauth2/v3/certs":
		f.certsCalls++
		if f.certsDown {
			return nil, errors.New("dial tcp: connection refused")
		}
		writeJSON(map[string]any{"keys": []map[string]string{{
			"kid": f.kid, "kty": "RSA", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes()),
		}}})
	case "oauth2.googleapis.com/tokeninfo":
		info, ok := f.accessTokens[request.URL.Query().Get("access_token")]
		if !ok {
			recorder.WriteHeader(http.StatusBadRequest)
			writeJSON(map[string]any{"error": "invalid_token"})
			break
		}
		writeJSON(map[string]any{"aud": info[0], "azp": info[0], "sub": info[1], "expires_in": "3599"})
	case "openidconnect.googleapis.com/v1/userinfo":
		token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		info, ok := f.accessTokens[token]
		if !ok {
			recorder.WriteHeader(http.StatusUnauthorized)
			break
		}
		writeJSON(map[string]any{"sub": info[1], "name": "Ada", "picture": "https://lh3.googleusercontent.com/a/x", "email": "ada@example.com", "email_verified": true})
	default:
		recorder.WriteHeader(http.StatusNotFound)
	}
	return recorder.Result(), nil
}

// sign 按 Credential Manager 签出的形状造一张 ID Token；edit 用来改出各种坏令牌。
func (f *fakeGoogle) sign(t *testing.T, edit func(claims jwt.MapClaims, header map[string]any)) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": testGoogleWebClient, "azp": testGoogleAndroidClient,
		"sub": "110169484474386276334", "email": "ada@example.com", "email_verified": true,
		"name": "Ada Lovelace", "picture": "https://lh3.googleusercontent.com/a/ada",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = f.kid
	if edit != nil {
		edit(claims, token.Header)
	}
	signed, err := token.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func newTestGoogleProvider() *OAuthProvider {
	return NewOAuthProvider(config.OAuthProviderConfig{
		Name: "google", Kind: oauthdomain.KindGoogle, ClientID: testGoogleWebClient, ClientSecret: "secret",
		UserInfoURL: googleUserInfoURL,
	})
}

func resetGoogleKeys(t *testing.T) {
	t.Helper()
	googleKeys = &googleKeyCache{}
	t.Cleanup(func() { googleKeys = &googleKeyCache{} })
}

func TestGoogleIDTokenProfile(t *testing.T) {
	resetGoogleKeys(t)
	ctx := context.Background()
	google := newFakeGoogle(t)
	provider := newTestGoogleProvider()

	idToken := google.sign(t, nil)
	profile, err := provider.ProfileFromAccessToken(ctx, google.client(), idToken, "")
	if err != nil {
		t.Fatalf("合法 ID Token 应当换出身份：%v", err)
	}
	if profile.ProviderUserID != "110169484474386276334" || profile.Nickname != "Ada Lovelace" ||
		profile.Email != "ada@example.com" || profile.Avatar != "https://lh3.googleusercontent.com/a/ada" {
		t.Fatalf("身份不对：%+v", profile)
	}
	// 客户端声称的 openid 与 sub 不一致时拒绝
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), idToken, "someone-else"); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("openid 对不上应报 %d，得到 %v", errCodeOAuthTokenInvalid, err)
	}
	// 公钥缓存住了，第二次不再拉
	if google.certsCalls != 1 {
		t.Fatalf("公钥应只拉一次，实际 %d 次", google.certsCalls)
	}

	// 没验证过的邮箱不收
	unverified := google.sign(t, func(claims jwt.MapClaims, _ map[string]any) { claims["email_verified"] = false })
	if profile, err := provider.ProfileFromAccessToken(ctx, google.client(), unverified, ""); err != nil || profile.Email != "" {
		t.Fatalf("未验证邮箱应留空：%+v / %v", profile, err)
	}
	// 不带 scheme 的签发者也是 Google 的合法写法
	bareIssuer := google.sign(t, func(claims jwt.MapClaims, _ map[string]any) { claims["iss"] = "accounts.google.com" })
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), bareIssuer, ""); err != nil {
		t.Fatalf("accounts.google.com 签发者应通过：%v", err)
	}

	bad := map[string]func(jwt.MapClaims, map[string]any){
		"别的应用的受众":  func(claims jwt.MapClaims, _ map[string]any) { claims["aud"] = "other.apps.googleusercontent.com" },
		"受众是安卓客户端": func(claims jwt.MapClaims, _ map[string]any) { claims["aud"] = testGoogleAndroidClient },
		"已过期":      func(claims jwt.MapClaims, _ map[string]any) { claims["exp"] = time.Now().Add(-time.Hour).Unix() },
		"没有过期时间":   func(claims jwt.MapClaims, _ map[string]any) { delete(claims, "exp") },
		"别的签发者":    func(claims jwt.MapClaims, _ map[string]any) { claims["iss"] = "https://evil.example.com" },
		"没有 sub":   func(claims jwt.MapClaims, _ map[string]any) { delete(claims, "sub") },
		"未知 kid":   func(_ jwt.MapClaims, header map[string]any) { header["kid"] = "kid-unknown" },
	}
	for name, edit := range bad {
		token := google.sign(t, edit)
		if _, err := provider.ProfileFromAccessToken(ctx, google.client(), token, ""); errCode(err) != errCodeOAuthTokenInvalid {
			t.Fatalf("%s：应报 %d，得到 %v", name, errCodeOAuthTokenInvalid, err)
		}
	}

	// 别的密钥签的：kid 对得上但签名不对
	forger := newFakeGoogle(t)
	forged := forger.sign(t, nil)
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), forged, ""); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("伪造签名应报 %d，得到 %v", errCodeOAuthTokenInvalid, err)
	}
	// alg=none 之类的算法替换
	none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": testGoogleWebClient, "sub": "x", "exp": time.Now().Add(time.Hour).Unix(),
	})
	unsigned, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), unsigned, ""); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("alg=none 应报 %d，得到 %v", errCodeOAuthTokenInvalid, err)
	}
}

func TestGoogleIDTokenCertsUnreachable(t *testing.T) {
	resetGoogleKeys(t)
	ctx := context.Background()
	google := newFakeGoogle(t)
	provider := newTestGoogleProvider()
	google.certsDown = true
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), google.sign(t, nil), ""); errCode(err) != 50203 {
		t.Fatalf("拉不到公钥应报 50203（不是令牌无效），得到 %v", err)
	}

	// 拉到过一次之后 Google 抖动：缓存过期也继续用旧公钥
	google.certsDown = false
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), google.sign(t, nil), ""); err != nil {
		t.Fatal(err)
	}
	google.certsDown = true
	googleKeys.fetchedAt = time.Now().Add(-2 * googleKeysMaxAge)
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), google.sign(t, nil), ""); err != nil {
		t.Fatalf("Google 暂时不可达时应沿用旧公钥：%v", err)
	}
}

func TestGoogleAccessTokenProfile(t *testing.T) {
	resetGoogleKeys(t)
	ctx := context.Background()
	google := newFakeGoogle(t)
	google.accessTokens["ya29.ours"] = [2]string{testGoogleWebClient, "sub-ours"}
	google.accessTokens["ya29.theirs"] = [2]string{"other.apps.googleusercontent.com", "sub-theirs"}
	provider := newTestGoogleProvider()

	profile, err := provider.ProfileFromAccessToken(ctx, google.client(), "ya29.ours", "")
	if err != nil || profile.ProviderUserID != "sub-ours" || profile.Email != "ada@example.com" || profile.Nickname != "Ada" {
		t.Fatalf("本应用的 access token 应当换出身份：%+v / %v", profile, err)
	}
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), "ya29.theirs", ""); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("别的应用签出的 access token 应报 %d，得到 %v", errCodeOAuthTokenInvalid, err)
	}
	if _, err := provider.ProfileFromAccessToken(ctx, google.client(), "ya29.forged", ""); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("无效 access token 应报 %d，得到 %v", errCodeOAuthTokenInvalid, err)
	}
}

func TestGooglePublicViewCarriesClientID(t *testing.T) {
	google := oauthdomain.Provider{Provider: "google", Kind: oauthdomain.KindGoogle, ClientID: testGoogleWebClient, ClientSecret: "s"}
	if view := publicView(&google); view.ClientID != testGoogleWebClient {
		t.Fatalf("Google 渠道应下发 Web 客户端 ID：%+v", view)
	}
	// 平台级渠道没写 kind，按渠道名推断
	platform := oauthdomain.Provider{Provider: "google", ClientID: testGoogleWebClient}
	if view := publicView(&platform); view.ClientID != testGoogleWebClient {
		t.Fatalf("未写 kind 的 google 渠道也应下发：%+v", view)
	}
	qq := oauthdomain.Provider{Provider: "qq", Kind: oauthdomain.KindQQ, ClientID: testQQAppID}
	if view := publicView(&qq); view.ClientID != "" {
		t.Fatalf("其它渠道不下发 ClientID：%+v", view)
	}
	raw, _ := json.Marshal(publicView(&qq))
	if strings.Contains(string(raw), "clientId") {
		t.Fatalf("其它渠道的 JSON 里不应出现 clientId：%s", raw)
	}
}
