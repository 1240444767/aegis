package service

import (
	"context"
	"net/http"

	appdomain "aegis/internal/domain/app"
	securitydomain "aegis/internal/domain/security"
	apperrors "aegis/pkg/errors"
)

// 应用级 Passkey 依赖方配置的读写，规则见 domain/app/passkey.go。

// AppPasskeyPolicyView 管理端读到的配置。
type AppPasskeyPolicyView struct {
	Policy appdomain.PasskeyPolicy `json:"policy"`
	// AllowedOrigins 校验 clientDataJSON.origin 时实际使用的完整来源（含推出的 apk-key-hash）
	AllowedOrigins []string `json:"allowedOrigins"`
	// AssetLinks RP ID 域名下 /.well-known/assetlinks.json 应发布的内容
	AssetLinks []map[string]any `json:"assetLinks"`
	// AssetLinksURL 应发布到的地址；Aegis 同时在 PublicAssetLinksPath 提供一份可供代理的副本
	AssetLinksURL string `json:"assetLinksUrl,omitempty"`
	// ModuleReady 平台 Passkey 模块是否可用（关着时应用配置不生效）
	ModuleReady bool `json:"moduleReady"`
}

// AppPasskeyPolicy 读取应用配置。
func (s *SecurityService) AppPasskeyPolicy(ctx context.Context, appID int64) (*AppPasskeyPolicyView, error) {
	if s.app == nil {
		return nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.appPasskeyPolicyView(appdomain.PasskeyPolicyFromSettings(app.Settings)), nil
}

// SaveAppPasskeyPolicy 校验并保存应用配置。
func (s *SecurityService) SaveAppPasskeyPolicy(ctx context.Context, appID int64, policy appdomain.PasskeyPolicy) (*AppPasskeyPolicyView, error) {
	if s.app == nil {
		return nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	normalized, err := appdomain.NormalizePasskeyPolicy(policy)
	if err != nil {
		return nil, apperrors.New(errCodePasskeyPolicy, http.StatusBadRequest, err.Error())
	}
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings[appdomain.PasskeySettingsKey] = normalized.ToMap()
	if _, err := s.app.SaveApp(ctx, appdomain.AppMutation{ID: appID, Settings: settings}); err != nil {
		return nil, err
	}
	return s.appPasskeyPolicyView(normalized), nil
}

// AppPasskeyAssetLinks 公开的 assetlinks 内容。应用没有配置安卓包时返回空数组。
func (s *SecurityService) AppPasskeyAssetLinks(app *appdomain.App) []map[string]any {
	if app == nil {
		return []map[string]any{}
	}
	policy := appdomain.PasskeyPolicyFromSettings(app.Settings)
	if !policy.Enabled || !policy.Configured() {
		return []map[string]any{}
	}
	return policy.AssetLinks()
}

func (s *SecurityService) appPasskeyPolicyView(policy appdomain.PasskeyPolicy) *AppPasskeyPolicyView {
	view := &AppPasskeyPolicyView{
		Policy:         policy,
		AllowedOrigins: policy.AllowedOrigins(),
		AssetLinks:     policy.AssetLinks(),
		ModuleReady:    s.ensureModuleReady(securitydomain.ModulePasskey) == nil,
	}
	if policy.RPID != "" {
		view.AssetLinksURL = "https://" + policy.RPID + "/.well-known/assetlinks.json"
	}
	return view
}
