package httptransport

import "strings"

// ── 应用级第三方登录（OAuth2）渠道配置 DTO ──

// AppOAuthProviderRequest 管理端新建 / 更新渠道配置。
//
// clientSecret 留空表示保持原密钥不变（编辑表单不回填密钥）；
// 需要清空时传 clearClientSecret=true。
type AppOAuthProviderRequest struct {
	Provider          string            `json:"provider"`
	Kind              string            `json:"kind"`
	DisplayName       string            `json:"displayName"`
	Icon              string            `json:"icon"`
	Color             string            `json:"color"`
	Enabled           bool              `json:"enabled"`
	ClientID          string            `json:"clientId"`
	ClientSecret      string            `json:"clientSecret"`
	ClearClientSecret bool              `json:"clearClientSecret"`
	RedirectURL       string            `json:"redirectUrl"`
	AuthURL           string            `json:"authUrl"`
	TokenURL          string            `json:"tokenUrl"`
	UserInfoURL       string            `json:"userInfoUrl"`
	Scopes            []string          `json:"scopes"`
	TokenAuthStyle    string            `json:"tokenAuthStyle"`
	UserInfoAuthStyle string            `json:"userInfoAuthStyle"`
	ProfileMapping    map[string]string `json:"profileMapping"`
	ExtraAuthParams   map[string]string `json:"extraAuthParams"`
	AllowLogin        *bool             `json:"allowLogin"`
	AllowRegister     *bool             `json:"allowRegister"`
	AllowBind         *bool             `json:"allowBind"`
	SortOrder         *int              `json:"sortOrder"`
	Remark            string            `json:"remark"`
}

// AppOAuthProviderEnabledRequest 列表页一键启停。
type AppOAuthProviderEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// AppOAuthProviderReorderRequest 按数组顺序重排登录页展示次序。
type AppOAuthProviderReorderRequest struct {
	Providers []string `json:"providers" binding:"required"`
}

// AppOAuthBindingQuery 管理端绑定记录查询。
type AppOAuthBindingQuery struct {
	Provider string `form:"provider"`
	UserID   int64  `form:"userId"`
	Keyword  string `form:"keyword"`
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
}

// AdminUnbindOAuthRequest 管理端强制解绑。
type AdminUnbindOAuthRequest struct {
	UserID   int64  `json:"userId" binding:"required"`
	Provider string `json:"provider" binding:"required"`
	// Force 跳过"解绑后无法登录"的保护性校验
	Force bool `json:"force"`
}

// OAuthProvidersQuery 登录页拉取可用渠道。
type OAuthProvidersQuery struct {
	AppID int64 `form:"appid" binding:"required"`
}

// OAuthBindURLRequest 已登录用户发起第三方账号绑定。
type OAuthBindURLRequest struct {
	Provider string `json:"provider" binding:"required"`
	DeviceID string `json:"deviceId"`
	Device   string `json:"device"`
	MarkCode string `json:"markCode"`
}

// OAuthLinkRegisterRequest 凭关联凭证快速注册。
type OAuthLinkRegisterRequest struct {
	Ticket   string `json:"ticket" binding:"required"`
	DeviceID string `json:"deviceId"`
	Device   string `json:"device"`
	MarkCode string `json:"markCode"`
}

// OAuthLinkBindRequest 已登录后凭关联凭证绑定。
type OAuthLinkBindRequest struct {
	Ticket string `json:"ticket" binding:"required"`
}

// OAuthBindTokenRequest 已登录用户用原生 SDK 拿到的第三方令牌绑定。
// openId 只有微信、微博需要；QQ 由服务端反查。
type OAuthBindTokenRequest struct {
	Provider    string `json:"provider" binding:"required"`
	AccessToken string `json:"accessToken" binding:"required"`
	OpenID      string `json:"openId"`
}

// AppOAuthExchangeRequest v1 网关的原生 SDK 换会话。
//
// 应用由路径上的 appKey 决定，请求体里没有 appid —— 复用旧命名空间的 OAuthMobileLoginRequest
// 会因为它的 appid 必填而一律 400「缺少必要的请求参数」。身份只认服务端拿 accessToken 核实的结果，
// openId（或老字段 providerUserId）只用来核对。
type AppOAuthExchangeRequest struct {
	Provider       string `json:"provider" binding:"required"`
	AccessToken    string `json:"accessToken" binding:"required"`
	OpenID         string `json:"openId"`
	ProviderUserID string `json:"providerUserId"`
	// DeferRegistration 未绑定时返回关联凭证（oauthLink），由用户选择快速注册或绑定已有账号
	DeferRegistration bool   `json:"deferRegistration"`
	DeviceID          string `json:"deviceId"`
	Device            string `json:"device"`
	MarkCode          string `json:"markCode"`
}

// claimedOpenID 客户端声称的第三方 ID：新字段 openId 优先，兼容旧字段 providerUserId。
func (r AppOAuthExchangeRequest) claimedOpenID() string {
	if openID := strings.TrimSpace(r.OpenID); openID != "" {
		return openID
	}
	return strings.TrimSpace(r.ProviderUserID)
}
