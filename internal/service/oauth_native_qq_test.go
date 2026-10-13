package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"aegis/internal/config"
	oauthdomain "aegis/internal/domain/oauth"
)

// fakeQQ 模拟 graph.qq.com 的 /oauth2.0/me 与 /user/get_user_info。
// 真实接口的形状来自线上探测：/me 出错时是 {"error":100013,...}，get_user_info 出错时是 {"ret":-22,...}。
type fakeQQ struct {
	mu sync.Mutex
	// tokens access_token → 签发它的 APP ID 与 openid
	tokens map[string]fakeQQToken
	// unionPermission 应用是否开通了 unionid 接口权限
	unionPermission bool
	// userInfoBroken get_user_info 一律报错
	userInfoBroken bool
	meCalls        int
}

type fakeQQToken struct {
	clientID string
	openID   string
	nickname string
}

func newFakeQQ() *fakeQQ {
	return &fakeQQ{tokens: map[string]fakeQQToken{}, unionPermission: true}
}

func (f *fakeQQ) issue(token, clientID, openID, nickname string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[token] = fakeQQToken{clientID: clientID, openID: openID, nickname: nickname}
}

func (f *fakeQQ) client() *http.Client {
	return &http.Client{Transport: fakeQQTransport{f}}
}

type fakeQQTransport struct{ f *fakeQQ }

func (t fakeQQTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.f.serve(recorder, request)
	return recorder.Result(), nil
}

func (f *fakeQQ) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	query := r.URL.Query()
	writeJSON := func(value any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}
	if r.URL.Host != "graph.qq.com" {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	switch r.URL.Path {
	case "/oauth2.0/me":
		f.meCalls++
		token, ok := f.tokens[query.Get("access_token")]
		if !ok {
			writeJSON(map[string]any{"error": 100013, "error_description": "access token is illegal"})
			return
		}
		if query.Get("unionid") == "1" && !f.unionPermission {
			writeJSON(map[string]any{"error": 100048, "error_description": "companyid not set"})
			return
		}
		body := map[string]any{"client_id": token.clientID, "openid": token.openID}
		if query.Get("unionid") == "1" {
			body["unionid"] = "UID_" + token.openID
		}
		if query.Get("fmt") != "json" {
			raw, _ := json.Marshal(body)
			_, _ = io.WriteString(w, "callback( "+string(raw)+" );\n")
			return
		}
		writeJSON(body)
	case "/user/get_user_info":
		token, ok := f.tokens[query.Get("access_token")]
		if f.userInfoBroken || !ok || token.openID != query.Get("openid") || token.clientID != query.Get("oauth_consumer_key") {
			writeJSON(map[string]any{"ret": -22, "msg": "openid is invalid"})
			return
		}
		writeJSON(map[string]any{
			"ret": 0, "msg": "", "nickname": token.nickname,
			"figureurl_qq_1": "http://thirdqq.qlogo.cn/g?b=oidb&k=" + token.openID + "&s=40",
			"figureurl_qq_2": "http://thirdqq.qlogo.cn/g?b=oidb&k=" + token.openID + "&s=100",
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const testQQAppID = "102104884"

func newTestQQProvider() *OAuthProvider {
	return NewOAuthProvider(config.OAuthProviderConfig{
		Name: "qq", Kind: oauthdomain.KindQQ, ClientID: testQQAppID, ClientSecret: "secret",
		UserInfoURL: qqUserInfoURL,
	})
}

func TestQQProfileFromAccessToken(t *testing.T) {
	ctx := context.Background()
	qq := newFakeQQ()
	qq.issue("tok-ok", testQQAppID, "OPENID_A", "小明")
	qq.issue("tok-other-app", "100000001", "OPENID_X", "别人")
	provider := newTestQQProvider()

	profile, err := provider.ProfileFromAccessToken(ctx, qq.client(), "tok-ok", "")
	if err != nil {
		t.Fatalf("合法令牌应当换出身份：%v", err)
	}
	if profile.ProviderUserID != "OPENID_A" || profile.UnionID != "UID_OPENID_A" || profile.Nickname != "小明" ||
		!strings.HasPrefix(profile.Avatar, "https://thirdqq.qlogo.cn/") || !strings.Contains(profile.Avatar, "s=100") ||
		profile.Tokens["access_token"] != "tok-ok" {
		t.Fatalf("身份不对：%+v", profile)
	}

	// 客户端声称的 openid 与令牌一致可以，不一致拒绝
	if _, err := provider.ProfileFromAccessToken(ctx, qq.client(), "tok-ok", "OPENID_A"); err != nil {
		t.Fatalf("openid 一致时应通过：%v", err)
	}
	if _, err := provider.ProfileFromAccessToken(ctx, qq.client(), "tok-ok", "OPENID_B"); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("openid 对不上应报 %d，得到 %v", errCodeOAuthTokenInvalid, err)
	}
	// 无效令牌、别的应用签出的令牌都拒绝
	if _, err := provider.ProfileFromAccessToken(ctx, qq.client(), "tok-forged", ""); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("无效令牌应报 %d，得到 %v", errCodeOAuthTokenInvalid, err)
	}
	if _, err := provider.ProfileFromAccessToken(ctx, qq.client(), "tok-other-app", ""); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("别的应用的令牌应报 %d，得到 %v", errCodeOAuthTokenInvalid, err)
	}
	if _, err := provider.ProfileFromAccessToken(ctx, qq.client(), "  ", ""); errCode(err) != 40004 {
		t.Fatalf("缺令牌应报 40004，得到 %v", err)
	}

	// 没开通 unionid 权限：退回只取 openid
	qq.unionPermission = false
	profile, err = provider.ProfileFromAccessToken(ctx, qq.client(), "tok-ok", "")
	if err != nil || profile.ProviderUserID != "OPENID_A" || profile.UnionID != "" {
		t.Fatalf("无 unionid 权限时应退回 openid：%+v / %v", profile, err)
	}
	qq.unionPermission = true

	// 资料接口失败不挡登录，只是没有昵称头像
	qq.userInfoBroken = true
	profile, err = provider.ProfileFromAccessToken(ctx, qq.client(), "tok-ok", "")
	if err != nil || profile.ProviderUserID != "OPENID_A" || profile.Nickname != "" {
		t.Fatalf("资料接口失败时应只缺昵称：%+v / %v", profile, err)
	}
	qq.userInfoBroken = false

	// 网页授权码链路走同一个反查，也要核对 client_id
	web := map[string]any{"access_token": "tok-other-app"}
	if _, err := provider.fetchProfile(ctx, qq.client(), web); errCode(err) != errCodeOAuthTokenInvalid {
		t.Fatalf("网页链路也应拒绝别的应用的令牌，得到 %v", err)
	}
}

func TestQQIdentityAcceptsJSONP(t *testing.T) {
	// 不带 fmt=json 时 QQ 回 JSONP；fetchQQIdentity 总带 fmt=json，这里直接喂一段 JSONP 验证解析兜底
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `callback( {"client_id":"`+testQQAppID+`","openid":"OPENID_J"} );`)
	}))
	defer server.Close()
	client := &http.Client{Transport: rewriteHostTransport{target: server.URL}}
	identity, err := newTestQQProvider().fetchQQIdentity(context.Background(), client, "tok", false)
	if err != nil || identity.OpenID != "OPENID_J" || identity.ClientID != testQQAppID {
		t.Fatalf("JSONP 应能解析：%+v / %v", identity, err)
	}
}

// rewriteHostTransport 把请求原样转到测试服务器。
type rewriteHostTransport struct{ target string }

func (t rewriteHostTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	parsed, _ := http.NewRequest(request.Method, t.target+request.URL.Path+"?"+request.URL.RawQuery, nil)
	clone.URL = parsed.URL
	clone.Host = parsed.URL.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func TestSecureQQAvatar(t *testing.T) {
	cases := map[string]string{
		"http://thirdqq.qlogo.cn/g?b=oidb&k=x&s=100": "https://thirdqq.qlogo.cn/g?b=oidb&k=x&s=100",
		"http://qlogo.cn/a.png":                      "https://qlogo.cn/a.png",
		"https://thirdqq.qlogo.cn/a":                 "https://thirdqq.qlogo.cn/a",
		"http://evilqlogo.cn/a":                      "http://evilqlogo.cn/a",
		"http://example.com/a.png":                   "http://example.com/a.png",
		"":                                           "",
	}
	for input, want := range cases {
		if got := secureQQAvatar(input); got != want {
			t.Errorf("secureQQAvatar(%q) = %q，期望 %q", input, got, want)
		}
	}
}
