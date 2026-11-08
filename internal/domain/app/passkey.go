package app

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// 应用级 Passkey 依赖方（Relying Party）配置，存在 apps.settings.passkey。
//
// 平台级配置（SECURITY_PASSKEY_*）只认浏览器：RP ID 跟着地址栏走，来源白名单写的是 https 源。
// 原生 App 走 Credential Manager / ASAuthorization，请求里没有 Origin，clientDataJSON 里的
// origin 是 `android:apk-key-hash:<签名证书 SHA-256 的 base64url>`，RP ID 必须是一个
// 发布了 /.well-known/assetlinks.json 的网站域名。这些都是应用自己的事实，平台配置给不出来。
//
// 配了这一份之后，这个应用的 Passkey 注册与登录一律按它来：RP ID 固定、允许来源只有
// 这里列出的网页源加上由安卓签名证书推出的 apk-key-hash 源。没配的应用维持平台级行为。

// PasskeySettingsKey 应用设置里存放配置的键。
const PasskeySettingsKey = "passkey"

// 上限：防止控制台误粘贴把设置撑爆。
const (
	passkeyMaxOrigins      = 16
	passkeyMaxAndroidApps  = 8
	passkeyMaxFingerprints = 8
)

// AndroidPasskeyApp 一个可以使用本应用 Passkey 的安卓包。
type AndroidPasskeyApp struct {
	PackageName string `json:"packageName"`
	// SHA256CertFingerprints 签名证书 SHA-256，规范成大写冒号分隔（与 keytool、assetlinks 一致）。
	// 证书轮换（APK 签名 v3.1 / v3.2）时把新旧证书都列上。
	SHA256CertFingerprints []string `json:"sha256CertFingerprints"`
}

// PasskeyPolicy 应用的 Passkey 依赖方配置。
type PasskeyPolicy struct {
	// Enabled 是否允许本应用使用 Passkey。显式关闭时注册与登录都拒绝。
	Enabled bool `json:"enabled"`
	// RPID 依赖方标识：一个网站域名（不含协议与端口），凭据绑在它上面。
	RPID string `json:"rpId"`
	// RPName 系统弹窗里展示的名称，留空用应用名。
	RPName string `json:"rpName,omitempty"`
	// Origins 允许的网页来源（https://host[:port]），主机必须是 RPID 或其子域。
	Origins []string `json:"origins"`
	// Android 允许的安卓包与签名证书。
	Android []AndroidPasskeyApp `json:"android"`
	// IsDefault 应用从未配置过，返回的是默认值（此时沿用平台级配置）。
	IsDefault bool `json:"isDefault"`
}

// DefaultPasskeyPolicy 未配置时的取值：沿用平台级 Passkey 配置。
func DefaultPasskeyPolicy() PasskeyPolicy {
	return PasskeyPolicy{Enabled: true, Origins: []string{}, Android: []AndroidPasskeyApp{}, IsDefault: true}
}

// Configured 是否已经配置了独立的依赖方（RP ID 非空）。
func (p PasskeyPolicy) Configured() bool {
	return !p.IsDefault && p.RPID != ""
}

// NormalizePasskeyPolicy 校验并规范化。
func NormalizePasskeyPolicy(policy PasskeyPolicy) (PasskeyPolicy, error) {
	out := PasskeyPolicy{Enabled: policy.Enabled, Origins: []string{}, Android: []AndroidPasskeyApp{}}
	out.RPName = strings.TrimSpace(policy.RPName)
	if len([]rune(out.RPName)) > 64 {
		return out, fmt.Errorf("显示名称不能超过 64 个字符")
	}

	rpID := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(policy.RPID)), ".")
	if rpID != "" {
		if err := validatePasskeyRPID(rpID); err != nil {
			return out, err
		}
	}
	out.RPID = rpID

	for _, raw := range policy.Origins {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		origin, err := normalizePasskeyWebOrigin(raw, rpID)
		if err != nil {
			return out, err
		}
		if !slices.Contains(out.Origins, origin) {
			out.Origins = append(out.Origins, origin)
		}
	}
	if len(out.Origins) > passkeyMaxOrigins {
		return out, fmt.Errorf("网页来源最多 %d 个", passkeyMaxOrigins)
	}

	seenPackages := map[string]bool{}
	for _, item := range policy.Android {
		pkg := strings.TrimSpace(item.PackageName)
		if pkg == "" && len(item.SHA256CertFingerprints) == 0 {
			continue
		}
		if !validAndroidPackageName(pkg) {
			return out, fmt.Errorf("安卓包名 %q 格式不正确", pkg)
		}
		if seenPackages[pkg] {
			return out, fmt.Errorf("安卓包名 %s 重复", pkg)
		}
		seenPackages[pkg] = true
		app := AndroidPasskeyApp{PackageName: pkg, SHA256CertFingerprints: []string{}}
		for _, rawFingerprint := range item.SHA256CertFingerprints {
			if strings.TrimSpace(rawFingerprint) == "" {
				continue
			}
			fingerprint, err := NormalizeCertFingerprint(rawFingerprint)
			if err != nil {
				return out, fmt.Errorf("%s 的证书指纹：%w", pkg, err)
			}
			if !slices.Contains(app.SHA256CertFingerprints, fingerprint) {
				app.SHA256CertFingerprints = append(app.SHA256CertFingerprints, fingerprint)
			}
		}
		if len(app.SHA256CertFingerprints) == 0 {
			return out, fmt.Errorf("%s 至少需要一个签名证书 SHA-256 指纹", pkg)
		}
		if len(app.SHA256CertFingerprints) > passkeyMaxFingerprints {
			return out, fmt.Errorf("%s 的证书指纹最多 %d 个", pkg, passkeyMaxFingerprints)
		}
		out.Android = append(out.Android, app)
	}
	if len(out.Android) > passkeyMaxAndroidApps {
		return out, fmt.Errorf("安卓应用最多 %d 个", passkeyMaxAndroidApps)
	}

	if out.Enabled && out.RPID == "" && (len(out.Origins) > 0 || len(out.Android) > 0) {
		return out, fmt.Errorf("填写网页来源或安卓应用时必须同时填写 RP ID")
	}
	if out.Enabled && out.RPID != "" && len(out.Origins) == 0 && len(out.Android) == 0 {
		return out, fmt.Errorf("RP ID 已填写，但没有任何允许的网页来源或安卓应用")
	}
	return out, nil
}

// AllowedOrigins 校验 clientDataJSON.origin 用的完整来源列表：网页源 + 每个安卓证书的 apk-key-hash 源。
func (p PasskeyPolicy) AllowedOrigins() []string {
	out := make([]string, 0, len(p.Origins)+len(p.Android))
	out = append(out, p.Origins...)
	for _, app := range p.Android {
		for _, fingerprint := range app.SHA256CertFingerprints {
			if origin := AndroidApkKeyHashOrigin(fingerprint); origin != "" && !slices.Contains(out, origin) {
				out = append(out, origin)
			}
		}
	}
	return out
}

// AssetLinks 生成 RP ID 域名下 /.well-known/assetlinks.json 应当发布的内容。
//
// get_login_creds 让 Credential Manager 承认这个包可以替该域名使用凭据（Passkey 与密码），
// handle_all_urls 是 Google 文档里与它成对出现的声明，缺了它部分提供方会拒绝。
func (p PasskeyPolicy) AssetLinks() []map[string]any {
	out := make([]map[string]any, 0, len(p.Android))
	for _, app := range p.Android {
		out = append(out, map[string]any{
			"relation": []string{
				"delegate_permission/common.handle_all_urls",
				"delegate_permission/common.get_login_creds",
			},
			"target": map[string]any{
				"namespace":                "android_app",
				"package_name":             app.PackageName,
				"sha256_cert_fingerprints": slices.Clone(app.SHA256CertFingerprints),
			},
		})
	}
	return out
}

// PasskeyPolicyFromSettings 从应用设置里读出配置；没有或结构不对时返回默认值。
func PasskeyPolicyFromSettings(settings map[string]any) PasskeyPolicy {
	raw, ok := settings[PasskeySettingsKey].(map[string]any)
	if !ok {
		return DefaultPasskeyPolicy()
	}
	policy := PasskeyPolicy{}
	if value, ok := raw["enabled"].(bool); ok {
		policy.Enabled = value
	}
	policy.RPID, _ = raw["rpId"].(string)
	policy.RPName, _ = raw["rpName"].(string)
	policy.Origins = stringsFromAny(raw["origins"])
	if list, ok := raw["android"].([]any); ok {
		for _, item := range list {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			pkg, _ := entry["packageName"].(string)
			policy.Android = append(policy.Android, AndroidPasskeyApp{
				PackageName:            pkg,
				SHA256CertFingerprints: stringsFromAny(entry["sha256CertFingerprints"]),
			})
		}
	}
	normalized, err := NormalizePasskeyPolicy(policy)
	if err != nil {
		// 存进去的都校验过；读出来不对只可能是被手工改坏了。按关闭处理，
		// 不能退回平台级配置 —— 那会让管理员以为收紧过的来源又放开了。
		return PasskeyPolicy{Enabled: false, Origins: []string{}, Android: []AndroidPasskeyApp{}}
	}
	return normalized
}

// ToMap 写回应用设置的形状。
func (p PasskeyPolicy) ToMap() map[string]any {
	android := make([]any, 0, len(p.Android))
	for _, app := range p.Android {
		fingerprints := make([]any, 0, len(app.SHA256CertFingerprints))
		for _, item := range app.SHA256CertFingerprints {
			fingerprints = append(fingerprints, item)
		}
		android = append(android, map[string]any{"packageName": app.PackageName, "sha256CertFingerprints": fingerprints})
	}
	origins := make([]any, 0, len(p.Origins))
	for _, item := range p.Origins {
		origins = append(origins, item)
	}
	out := map[string]any{"enabled": p.Enabled, "rpId": p.RPID, "origins": origins, "android": android}
	if p.RPName != "" {
		out["rpName"] = p.RPName
	}
	return out
}

// NormalizeCertFingerprint 接受 keytool 的冒号格式、无分隔的十六进制，规范成大写冒号分隔。
func NormalizeCertFingerprint(raw string) (string, error) {
	cleaned := strings.NewReplacer(":", "", " ", "", "-", "").Replace(strings.TrimSpace(raw))
	decoded, err := hex.DecodeString(cleaned)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("%q 不是 SHA-256 指纹（应为 32 字节十六进制）", strings.TrimSpace(raw))
	}
	parts := make([]string, len(decoded))
	for i, b := range decoded {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":"), nil
}

// AndroidApkKeyHashOrigin 由证书 SHA-256 推出安卓原生 WebAuthn 的来源：
// android:apk-key-hash:<base64url 无填充>。
func AndroidApkKeyHashOrigin(fingerprint string) string {
	normalized, err := NormalizeCertFingerprint(fingerprint)
	if err != nil {
		return ""
	}
	decoded, _ := hex.DecodeString(strings.ReplaceAll(normalized, ":", ""))
	return "android:apk-key-hash:" + base64.RawURLEncoding.EncodeToString(decoded)
}

func validatePasskeyRPID(rpID string) error {
	if strings.ContainsAny(rpID, "/:@?# ") {
		return fmt.Errorf("RP ID 只能是域名，不能带协议、端口或路径")
	}
	if rpID == "localhost" {
		return nil
	}
	labels := strings.Split(rpID, ".")
	if len(labels) < 2 {
		return fmt.Errorf("RP ID %q 不是有效域名", rpID)
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("RP ID %q 不是有效域名", rpID)
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return fmt.Errorf("RP ID %q 不是有效域名", rpID)
			}
		}
	}
	return nil
}

func normalizePasskeyWebOrigin(raw, rpID string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("网页来源 %q 格式不正确，应为 https://域名", raw)
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if scheme != "https" && !(scheme == "http" && (host == "localhost" || host == "127.0.0.1")) {
		return "", fmt.Errorf("网页来源 %q 必须使用 https", raw)
	}
	if rpID != "" && host != rpID && !strings.HasSuffix(host, "."+rpID) {
		return "", fmt.Errorf("网页来源 %s 的域名不是 RP ID %s 或它的子域", raw, rpID)
	}
	return scheme + "://" + strings.ToLower(parsed.Host), nil
}

// validAndroidPackageName 按 AAPT 规则：至少两段，每段字母开头，只含字母数字下划线。
func validAndroidPackageName(pkg string) bool {
	segments := strings.Split(pkg, ".")
	if len(segments) < 2 || len(pkg) > 255 {
		return false
	}
	for _, segment := range segments {
		if segment == "" {
			return false
		}
		for i, r := range segment {
			letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
			if i == 0 && !letter {
				return false
			}
			if !letter && !(r >= '0' && r <= '9') && r != '_' {
				return false
			}
		}
	}
	return true
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return slices.Clone(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}
