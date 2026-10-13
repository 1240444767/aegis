package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	oauthdomain "aegis/internal/domain/oauth"
	apperrors "aegis/pkg/errors"
)

// OAuthProvider 单个第三方登录渠道的协议适配器。
//
// 配置既可能来自平台级 .env，也可能来自应用级 app_oauth_providers；
// 协议差异统一由 cfg.Kind 决定（留空时回落到 cfg.Name，保持旧配置行为）。
type OAuthProvider struct {
	cfg config.OAuthProviderConfig
}

func NewOAuthProvider(cfg config.OAuthProviderConfig) *OAuthProvider {
	return &OAuthProvider{cfg: cfg}
}

// kind 返回协议适配器类型；未显式指定时按渠道名推断，
// 使自定义 slug（如 my-github）也能复用内置适配器。
func (p *OAuthProvider) kind() string {
	kind := strings.ToLower(strings.TrimSpace(p.cfg.Kind))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(p.cfg.Name))
	}
	switch kind {
	case oauthdomain.KindQQ, oauthdomain.KindWechat, oauthdomain.KindWeibo,
		oauthdomain.KindGitHub, oauthdomain.KindMicrosoft:
		return kind
	default:
		return oauthdomain.KindGeneric
	}
}

func (p *OAuthProvider) tokenAuthStyle() string {
	switch strings.ToLower(strings.TrimSpace(p.cfg.TokenAuthStyle)) {
	case oauthdomain.TokenAuthParams:
		return oauthdomain.TokenAuthParams
	case oauthdomain.TokenAuthBasic:
		return oauthdomain.TokenAuthBasic
	default:
		return oauthdomain.TokenAuthAuto
	}
}

func (p *OAuthProvider) userInfoAuthStyle() string {
	if strings.EqualFold(strings.TrimSpace(p.cfg.UserInfoAuthStyle), oauthdomain.UserInfoAuthQuery) {
		return oauthdomain.UserInfoAuthQuery
	}
	return oauthdomain.UserInfoAuthHeader
}

func (p *OAuthProvider) AuthURL(state string) string {
	params := url.Values{}
	switch p.kind() {
	case oauthdomain.KindWechat:
		params.Set("appid", p.cfg.ClientID)
		params.Set("scope", strings.Join(p.cfg.Scopes, ","))
		params.Set("response_type", "code")
		params.Set("redirect_uri", p.cfg.RedirectURL)
		params.Set("state", state)
		p.applyExtraAuthParams(params)
		return p.cfg.AuthURL + "?" + params.Encode() + "#wechat_redirect"
	case oauthdomain.KindQQ:
		params.Set("client_id", p.cfg.ClientID)
		params.Set("redirect_uri", p.cfg.RedirectURL)
		params.Set("response_type", "code")
		params.Set("state", state)
		if len(p.cfg.Scopes) > 0 {
			params.Set("scope", strings.Join(p.cfg.Scopes, ","))
		}
	default:
		params.Set("client_id", p.cfg.ClientID)
		params.Set("redirect_uri", p.cfg.RedirectURL)
		params.Set("response_type", "code")
		params.Set("state", state)
		if len(p.cfg.Scopes) > 0 {
			params.Set("scope", strings.Join(p.cfg.Scopes, " "))
		}
	}
	p.applyExtraAuthParams(params)
	return p.cfg.AuthURL + "?" + params.Encode()
}

// applyExtraAuthParams 追加管理端自定义的授权参数（如 prompt=consent、access_type=offline）。
// 协议必需参数（client_id / redirect_uri / response_type / state / scope）不允许被覆盖。
func (p *OAuthProvider) applyExtraAuthParams(params url.Values) {
	if len(p.cfg.ExtraAuthParams) == 0 {
		return
	}
	reserved := map[string]bool{
		"client_id": true, "appid": true, "redirect_uri": true,
		"response_type": true, "state": true, "scope": true,
	}
	keys := make([]string, 0, len(p.cfg.ExtraAuthParams))
	for key := range p.cfg.ExtraAuthParams {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		normalized := strings.TrimSpace(key)
		if normalized == "" || reserved[strings.ToLower(normalized)] {
			continue
		}
		params.Set(normalized, p.cfg.ExtraAuthParams[key])
	}
}

func (p *OAuthProvider) ExchangeCode(ctx context.Context, client *http.Client, code string) (authdomain.ProviderProfile, error) {
	if p.cfg.ClientID == "" || p.cfg.ClientSecret == "" {
		return authdomain.ProviderProfile{}, apperrors.New(40010, http.StatusBadRequest, "OAuth2 提供商未配置密钥")
	}
	if strings.TrimSpace(code) == "" {
		return authdomain.ProviderProfile{}, apperrors.New(40011, http.StatusBadRequest, "OAuth2 授权码不能为空")
	}
	if p.cfg.RedirectURL == "" {
		return authdomain.ProviderProfile{}, apperrors.New(40012, http.StatusBadRequest, "OAuth2 回调地址未配置")
	}
	tokenResp, err := p.exchangeToken(ctx, client, code)
	if err != nil {
		return authdomain.ProviderProfile{}, err
	}
	accessToken, _ := tokenResp["access_token"].(string)
	if accessToken == "" {
		return authdomain.ProviderProfile{}, apperrors.New(50201, http.StatusBadGateway, "OAuth2 access_token 获取失败")
	}
	return p.fetchProfile(ctx, client, tokenResp)
}

// exchangeToken 换取 access_token。
//
// tokenAuthStyle=auto 时先按表单参数提交（绝大多数渠道支持），
// 仅在返回 invalid_client / 401 时用 HTTP Basic 重试一次 ——
// 覆盖那些严格要求 Basic 的实现（如 LINUX DO Connect），无需人工切换。
func (p *OAuthProvider) exchangeToken(ctx context.Context, client *http.Client, code string) (map[string]any, error) {
	style := p.tokenAuthStyle()
	kind := p.kind()
	// QQ / 微信走各自的私有参数格式，不适用 Basic
	if kind == oauthdomain.KindQQ || kind == oauthdomain.KindWechat {
		style = oauthdomain.TokenAuthParams
	}
	result, err := p.requestToken(ctx, client, code, style == oauthdomain.TokenAuthBasic)
	if err != nil && style == oauthdomain.TokenAuthAuto && isClientAuthError(err) {
		return p.requestToken(ctx, client, code, true)
	}
	return result, err
}

func (p *OAuthProvider) requestToken(ctx context.Context, client *http.Client, code string, basicAuth bool) (map[string]any, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	method := http.MethodPost
	requestURL := p.cfg.TokenURL
	kind := p.kind()
	if kind == oauthdomain.KindWechat || kind == oauthdomain.KindQQ {
		method = http.MethodGet
	}
	if kind == oauthdomain.KindWechat {
		form = url.Values{}
		form.Set("appid", p.cfg.ClientID)
		form.Set("secret", p.cfg.ClientSecret)
		form.Set("code", code)
		form.Set("grant_type", "authorization_code")
	}
	if kind == oauthdomain.KindQQ {
		form = url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("client_id", p.cfg.ClientID)
		form.Set("client_secret", p.cfg.ClientSecret)
		form.Set("code", code)
		form.Set("redirect_uri", p.cfg.RedirectURL)
		form.Set("fmt", "json")
	}
	if basicAuth {
		// Basic 模式下凭据只走 Authorization 头，表单里不再重复携带 secret
		form.Del("client_secret")
		form.Del("client_id")
	}

	var requestBody io.Reader
	if method == http.MethodGet {
		if strings.Contains(requestURL, "?") {
			requestURL += "&" + form.Encode()
		} else {
			requestURL += "?" + form.Encode()
		}
	} else {
		requestBody = strings.NewReader(form.Encode())
	}

	request, err := http.NewRequestWithContext(ctx, method, requestURL, requestBody)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set("Accept", "application/json")
	if basicAuth {
		credential := base64.StdEncoding.EncodeToString([]byte(p.cfg.ClientID + ":" + p.cfg.ClientSecret))
		request.Header.Set("Authorization", "Basic "+credential)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("oauth token exchange http %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	result := map[string]any{}
	if json.Unmarshal(responseBody, &result) == nil {
		if errValue, ok := result["error"]; ok && errValue != nil && fmt.Sprint(errValue) != "" {
			return nil, fmt.Errorf("oauth token exchange failed: %v", errValue)
		}
		return result, nil
	}
	values, err := url.ParseQuery(string(responseBody))
	if err != nil {
		return nil, fmt.Errorf("parse token response failed: %w", err)
	}
	for key, vals := range values {
		if len(vals) > 0 {
			result[key] = vals[0]
		}
	}
	if errValue, ok := result["error"]; ok && errValue != nil && fmt.Sprint(errValue) != "" {
		return nil, fmt.Errorf("oauth token exchange failed: %v", errValue)
	}
	return result, nil
}

// isClientAuthError 判断错误是否为"客户端凭据认证方式不被接受"，用于 auto 模式的 Basic 重试。
func isClientAuthError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid_client") ||
		strings.Contains(message, "unauthorized_client") ||
		strings.Contains(message, "http 401") ||
		strings.Contains(message, "http 403")
}

func (p *OAuthProvider) fetchProfile(ctx context.Context, client *http.Client, tokenResp map[string]any) (authdomain.ProviderProfile, error) {
	accessToken, _ := tokenResp["access_token"].(string)
	refreshToken, _ := tokenResp["refresh_token"].(string)
	result := authdomain.ProviderProfile{
		Provider: p.cfg.Name,
		Tokens: map[string]string{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
		},
	}

	switch p.kind() {
	case oauthdomain.KindQQ:
		return p.fetchQQProfile(ctx, client, result)
	case oauthdomain.KindWechat:
		openid, _ := tokenResp["openid"].(string)
		unionid, _ := tokenResp["unionid"].(string)
		return p.fetchWechatProfile(ctx, client, result, openid, unionid)
	case oauthdomain.KindWeibo:
		uid := firstString(tokenResp, "uid")
		return p.fetchWeiboProfile(ctx, client, result, uid)
	case oauthdomain.KindGitHub:
		return p.fetchGitHubProfile(ctx, client, result)
	default:
		return p.fetchGenericProfile(ctx, client, result)
	}
}

func (p *OAuthProvider) fetchGenericProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile) (authdomain.ProviderProfile, error) {
	if strings.TrimSpace(p.cfg.UserInfoURL) == "" {
		return profile, apperrors.New(40012, http.StatusBadRequest, "OAuth2 用户信息地址未配置")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return profile, err
	}
	if p.userInfoAuthStyle() == oauthdomain.UserInfoAuthQuery {
		query := request.URL.Query()
		query.Set("access_token", profile.Tokens["access_token"])
		request.URL.RawQuery = query.Encode()
	} else {
		request.Header.Set("Authorization", "Bearer "+profile.Tokens["access_token"])
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("oauth profile fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return profile, err
	}
	if errValue, ok := raw["error"]; ok && errValue != nil && fmt.Sprint(errValue) != "" {
		return profile, fmt.Errorf("oauth profile fetch failed: %v", errValue)
	}
	profile.RawProfile = raw
	profile.ProviderUserID = p.mappedString(raw, oauthdomain.MappingID, "sub", "id", "login", "openid", "uid", "user_id")
	profile.Nickname = p.mappedString(raw, oauthdomain.MappingNickname, "name", "login", "preferred_username", "nickname", "screen_name", "username")
	profile.Avatar = p.mappedString(raw, oauthdomain.MappingAvatar, "avatar_url", "picture", "profile_image_url", "avatar", "avatar_template")
	profile.Email = p.mappedString(raw, oauthdomain.MappingEmail, "email")
	profile.UnionID = p.mappedString(raw, oauthdomain.MappingUnionID, "unionid", "union_id")
	if profile.ProviderUserID == "" {
		return profile, apperrors.New(50202, http.StatusBadGateway, "OAuth2 用户信息缺少唯一标识")
	}
	return profile, nil
}

// mappedString 先按管理端配置的字段映射（支持 data.user.id 这类点号路径）取值，
// 未配置映射时回落到内置候选键，兼容既有渠道。
func (p *OAuthProvider) mappedString(raw map[string]any, mappingKey string, fallbackKeys ...string) string {
	if path := strings.TrimSpace(p.cfg.ProfileMapping[mappingKey]); path != "" {
		if value := lookupProfilePath(raw, path); value != "" {
			return value
		}
	}
	return firstString(raw, fallbackKeys...)
}

// lookupProfilePath 按点号路径在嵌套 JSON 中取标量值。
func lookupProfilePath(raw map[string]any, path string) string {
	segments := strings.Split(path, ".")
	var current any = raw
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return ""
		}
		node, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = node[segment]
		if !ok || current == nil {
			return ""
		}
	}
	return scalarToString(current)
}

// QQ 互联用 access_token 反查身份与拉取资料的接口。
//
// 两个域名提供同样的接口、同样的返回格式：openmobile.qq.com 是 QQ 移动 SDK 自己用的，
// graph.qq.com 是网站接入文档里的。生产服务器在美国，实测连 graph.qq.com 能建 TCP 却卡在
// TLS / 响应上，一直等到超时；openmobile.qq.com 可达。所以先走 openmobile，不通再退到 graph。
var qqAPIBases = []string{"https://openmobile.qq.com", "https://graph.qq.com"}

const (
	qqOpenIDPath   = "/oauth2.0/me"
	qqUserInfoPath = "/user/get_user_info"
	// qqUserInfoURL 渠道模板里登记的资料地址（graph 域名），管理员没改过时就是它
	qqUserInfoURL = "https://graph.qq.com" + qqUserInfoPath
	// qqErrNoUnionIDPermission 应用没有 unionid 接口权限（要向 QQ 互联单独申请）
	qqErrNoUnionIDPermission = "100048"
)

// 单个 QQ 地址的等待上限。出站客户端整体是 8 秒，一个地址卡住时要留出时间换下一个；
// 资料只用来填昵称头像，等得更短。
var (
	qqIdentityAttemptTimeout = 4 * time.Second
	qqUserInfoAttemptTimeout = 3 * time.Second
)

// qqIdentity /oauth2.0/me 的结果。
type qqIdentity struct {
	ClientID string
	OpenID   string
	UnionID  string
	// Base 答复了这次核验的域名。资料接口先走它：另一个刚被证明不通，再去试只是白等
	Base string
}

// errQQUnreachable 这一个 QQ 地址没有给出可用的答复（连不上、超时、5xx、不是 JSON），换下一个地址。
var errQQUnreachable = errors.New("qq endpoint unreachable")

func (p *OAuthProvider) fetchQQProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile) (authdomain.ProviderProfile, error) {
	accessToken := profile.Tokens["access_token"]
	identity, err := p.fetchQQIdentity(ctx, client, accessToken, true)
	if err != nil {
		return profile, err
	}
	profile.ProviderUserID = identity.OpenID
	profile.UnionID = identity.UnionID

	// 身份到这里已经确定。资料只用来填昵称头像，拉不到（权限未开通、接口抖动）不该挡住登录
	query := url.Values{}
	query.Set("access_token", accessToken)
	query.Set("oauth_consumer_key", identity.ClientID)
	query.Set("openid", identity.OpenID)
	for _, endpoint := range p.qqUserInfoURLs(identity.Base) {
		raw, err := qqGetJSON(ctx, client, endpoint+"?"+query.Encode(), qqUserInfoAttemptTimeout)
		if err != nil {
			continue
		}
		if ret := firstString(raw, "ret"); ret != "" && ret != "0" {
			return profile, nil
		}
		profile.Nickname = firstString(raw, "nickname")
		profile.Avatar = secureQQAvatar(firstString(raw, "figureurl_qq_2", "figureurl_qq_1", "figureurl_2", "figureurl_1"))
		profile.RawProfile = raw
		return profile, nil
	}
	return profile, nil
}

// qqUserInfoURLs 资料接口的候选地址：答复了身份核验的域名最先，其余内置域名其次，
// 管理员自定义的地址（若不同）最后。
func (p *OAuthProvider) qqUserInfoURLs(preferred string) []string {
	urls := make([]string, 0, len(qqAPIBases)+1)
	if preferred != "" {
		urls = append(urls, preferred+qqUserInfoPath)
	}
	for _, base := range qqAPIBases {
		if !slices.Contains(urls, base+qqUserInfoPath) {
			urls = append(urls, base+qqUserInfoPath)
		}
	}
	if custom := strings.TrimSpace(p.cfg.UserInfoURL); custom != "" && !slices.Contains(urls, custom) {
		urls = append(urls, custom)
	}
	return urls
}

// fetchQQIdentity 用 access_token 反查 openid / unionid，并核对令牌是签给本应用的。
//
// openid 按应用隔离，别的应用签出的令牌查出来的是那个应用下的 openid；
// 不核对 client_id，就等于接受任何 QQ 应用的令牌，校验形同虚设。
// 应用没开通 unionid 权限时 QQ 报 100048，这时退回只取 openid。
// 依次尝试 [qqAPIBases]：一个地址不通就换下一个；拿到 QQ 的明确答复（包括令牌无效）就不再换。
func (p *OAuthProvider) fetchQQIdentity(ctx context.Context, client *http.Client, accessToken string, withUnionID bool) (qqIdentity, error) {
	query := url.Values{}
	query.Set("access_token", accessToken)
	query.Set("fmt", "json")
	if withUnionID {
		query.Set("unionid", "1")
	}
	var (
		raw      map[string]any
		answered string
	)
	for _, base := range qqAPIBases {
		answer, err := qqGetJSON(ctx, client, base+qqOpenIDPath+"?"+query.Encode(), qqIdentityAttemptTimeout)
		if err == nil {
			raw, answered = answer, base
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	if raw == nil {
		return qqIdentity{}, apperrors.New(50203, http.StatusBadGateway, "QQ 身份校验失败，请稍后重试")
	}
	if code := firstString(raw, "error"); code != "" && code != "0" {
		if withUnionID && code == qqErrNoUnionIDPermission {
			return p.fetchQQIdentity(ctx, client, accessToken, false)
		}
		return qqIdentity{}, apperrors.New(errCodeOAuthTokenInvalid, http.StatusForbidden, "QQ 授权已失效，请重新授权")
	}
	identity := qqIdentity{
		ClientID: firstString(raw, "client_id"),
		OpenID:   firstString(raw, "openid"),
		UnionID:  firstString(raw, "unionid"),
		Base:     answered,
	}
	if identity.OpenID == "" {
		return qqIdentity{}, apperrors.New(50203, http.StatusBadGateway, "QQ 身份校验失败，请稍后重试")
	}
	if expected := strings.TrimSpace(p.cfg.ClientID); expected != "" && identity.ClientID != expected {
		return qqIdentity{}, apperrors.New(errCodeOAuthTokenInvalid, http.StatusForbidden, "QQ 授权不属于本应用，请重新授权")
	}
	if identity.ClientID == "" {
		identity.ClientID = p.cfg.ClientID
	}
	return identity, nil
}

// qqGetJSON 对一个 QQ 地址发 GET，单次最多等 timeout。
// 不带 fmt=json 时 QQ 回的是 callback( {...} ); 这种 JSONP，两种都兼容。
// 没拿到可解析的答复一律返回 errQQUnreachable，由调用方换下一个地址。
func qqGetJSON(ctx context.Context, client *http.Client, target string, timeout time.Duration) (map[string]any, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errQQUnreachable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode >= http.StatusInternalServerError {
		return nil, errQQUnreachable
	}
	text := string(body)
	begin := strings.Index(text, "{")
	finish := strings.LastIndex(text, "}")
	if begin == -1 || finish <= begin {
		return nil, errQQUnreachable
	}
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(text[begin:finish+1]), &raw); err != nil {
		return nil, errQQUnreachable
	}
	return raw, nil
}

// secureQQAvatar QQ 资料里的头像是 http 地址，HTTPS 页面上会被浏览器当混合内容拦掉；
// qlogo.cn 同样支持 https，换过去即可。别的域名原样返回。
func secureQQAvatar(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" {
		return raw
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "qlogo.cn" && !strings.HasSuffix(host, ".qlogo.cn") {
		return raw
	}
	parsed.Scheme = "https"
	return parsed.String()
}

// ProfileFromAccessToken 用原生 SDK 在端上拿到的 access_token，由服务端向服务商换回第三方身份。
//
// 客户端自报的 openid、昵称一概不作数，身份只认服务端拿令牌查回来的结果。
// openID 只有微信、微博需要：它们的资料接口按 openid / uid 查，令牌本身反查不出是谁。
// 客户端传了 openID 而与查回来的不一致时拒绝，免得拿 A 的令牌冒充 B。
func (p *OAuthProvider) ProfileFromAccessToken(ctx context.Context, client *http.Client, accessToken, openID string) (authdomain.ProviderProfile, error) {
	accessToken = strings.TrimSpace(accessToken)
	openID = strings.TrimSpace(openID)
	if accessToken == "" {
		return authdomain.ProviderProfile{}, apperrors.New(40004, http.StatusBadRequest, "缺少第三方授权凭据 accessToken")
	}
	base := authdomain.ProviderProfile{
		Provider: p.cfg.Name,
		Tokens:   map[string]string{"access_token": accessToken},
	}
	var (
		profile authdomain.ProviderProfile
		err     error
	)
	switch p.kind() {
	case oauthdomain.KindQQ:
		profile, err = p.fetchQQProfile(ctx, client, base)
	case oauthdomain.KindWechat:
		if openID == "" {
			return base, apperrors.New(40004, http.StatusBadRequest, "微信登录需要同时提供 openid")
		}
		profile, err = p.fetchWechatProfile(ctx, client, base, openID, "")
	case oauthdomain.KindWeibo:
		var uid string
		uid, err = p.fetchWeiboTokenUID(ctx, client, accessToken)
		if err == nil {
			profile, err = p.fetchWeiboProfile(ctx, client, base, uid)
		}
	case oauthdomain.KindGitHub:
		profile, err = p.fetchGitHubProfile(ctx, client, base)
	default:
		profile, err = p.fetchGenericProfile(ctx, client, base)
	}
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return profile, err
		}
		return profile, apperrors.New(errCodeOAuthTokenInvalid, http.StatusForbidden, "第三方授权校验失败，请重新授权")
	}
	if profile.ProviderUserID == "" {
		return profile, apperrors.New(50202, http.StatusBadGateway, "第三方用户信息缺少唯一标识")
	}
	if openID != "" && openID != profile.ProviderUserID {
		return profile, apperrors.New(errCodeOAuthTokenInvalid, http.StatusForbidden, "第三方授权与账号不一致，请重新授权")
	}
	return profile, nil
}

// fetchWeiboTokenUID 微博的令牌自省：查出令牌属于哪个 uid，并核对是签给本应用的。
func (p *OAuthProvider) fetchWeiboTokenUID(ctx context.Context, client *http.Client, accessToken string) (string, error) {
	form := url.Values{}
	form.Set("access_token", accessToken)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.weibo.com/oauth2/get_token_info", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return "", apperrors.New(50205, http.StatusBadGateway, "微博身份校验失败，请稍后重试")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", apperrors.New(50205, http.StatusBadGateway, "微博身份校验失败，请稍后重试")
	}
	raw := map[string]any{}
	if json.Unmarshal(body, &raw) != nil || response.StatusCode >= http.StatusBadRequest || firstString(raw, "error") != "" {
		return "", apperrors.New(errCodeOAuthTokenInvalid, http.StatusForbidden, "微博授权已失效，请重新授权")
	}
	if expected := strings.TrimSpace(p.cfg.ClientID); expected != "" && firstString(raw, "appkey") != expected {
		return "", apperrors.New(errCodeOAuthTokenInvalid, http.StatusForbidden, "微博授权不属于本应用，请重新授权")
	}
	uid := firstString(raw, "uid")
	if uid == "" {
		return "", apperrors.New(50205, http.StatusBadGateway, "微博 uid 获取失败")
	}
	return uid, nil
}

func (p *OAuthProvider) fetchGitHubProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile) (authdomain.ProviderProfile, error) {
	profile, err := p.fetchGenericProfile(ctx, client, profile)
	if err != nil {
		return profile, err
	}
	if profile.Email != "" {
		return profile, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return profile, err
	}
	request.Header.Set("Authorization", "Bearer "+profile.Tokens["access_token"])
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("github email fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var emails []map[string]any
	if err := json.Unmarshal(body, &emails); err != nil {
		return profile, nil
	}
	for _, item := range emails {
		email := firstString(item, "email")
		if email == "" {
			continue
		}
		primary, _ := item["primary"].(bool)
		verified, _ := item["verified"].(bool)
		if primary || verified {
			profile.Email = email
			return profile, nil
		}
	}
	if len(emails) > 0 {
		profile.Email = firstString(emails[0], "email")
	}
	return profile, nil
}

func (p *OAuthProvider) fetchWechatProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile, openid, unionid string) (authdomain.ProviderProfile, error) {
	if openid == "" {
		return profile, apperrors.New(50204, http.StatusBadGateway, "微信 openid 获取失败")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return profile, err
	}
	query := request.URL.Query()
	query.Set("access_token", profile.Tokens["access_token"])
	query.Set("openid", openid)
	query.Set("lang", "zh_CN")
	request.URL.RawQuery = query.Encode()
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("wechat profile fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return profile, err
	}
	if errCode := firstString(raw, "errcode"); errCode != "" && errCode != "0" {
		return profile, apperrors.New(50204, http.StatusBadGateway, "微信用户信息获取失败")
	}
	profile.ProviderUserID = openid
	profile.UnionID = unionid
	if profile.UnionID == "" {
		profile.UnionID = firstString(raw, "unionid")
	}
	profile.Nickname = firstString(raw, "nickname")
	profile.Avatar = firstString(raw, "headimgurl")
	profile.RawProfile = raw
	return profile, nil
}

func (p *OAuthProvider) fetchWeiboProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile, uid string) (authdomain.ProviderProfile, error) {
	if uid == "" {
		return profile, apperrors.New(50205, http.StatusBadGateway, "微博 uid 获取失败")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return profile, err
	}
	query := request.URL.Query()
	query.Set("access_token", profile.Tokens["access_token"])
	query.Set("uid", uid)
	request.URL.RawQuery = query.Encode()
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("weibo profile fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return profile, err
	}
	profile.ProviderUserID = uid
	profile.Nickname = firstString(raw, "screen_name", "name")
	profile.Avatar = firstString(raw, "avatar_large", "profile_image_url")
	profile.RawProfile = raw
	return profile, nil
}

func firstString(source map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := source[key]
		if !ok || value == nil {
			continue
		}
		if text := scalarToString(value); text != "" {
			return text
		}
	}
	return ""
}

func scalarToString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case json.Number:
		return typed.String()
	}
	return ""
}
