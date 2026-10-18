package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go/v7"
	cfaccounts "github.com/cloudflare/cloudflare-go/v7/accounts"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/cloudflare/cloudflare-go/v7/r2"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	cfuser "github.com/cloudflare/cloudflare-go/v7/user"
	"github.com/cloudflare/cloudflare-go/v7/zones"
	"go.uber.org/zap"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
)

// Cloudflare 快速接入：管理员贴一个 Cloudflare API 令牌，Aegis 经 cloudflare-go 代办余下的事 ——
// 列出账户与桶、建桶、开公开访问（r2.dev 或自定义域名）、配 CORS、签发只能访问这一个桶的专用凭据、
// 保存存储源、深度检测、加入存储池。
//
// 令牌只在这几次请求里用，不落库；落库的是为这个桶专门签发的令牌（权限只到这个桶的对象读写，
// 外加账户分析读，用来读桶用量）。R2 的 S3 凭据由 API 令牌派生：Access Key ID 是令牌 ID，
// Secret Access Key 是令牌值的 SHA-256。
//
// 令牌分两种：用户令牌（/user/tokens）与账户令牌（/accounts/{id}/tokens），两套校验与签发接口。
// 连接时先按用户令牌校验，不行再按账户令牌校验；签发时按同一种走，失败再试另一种。

const (
	cloudflareRequestTimeout = 30 * time.Second
	// R2 存储桶对象读写，可限定到单个桶。
	cfPermR2BucketItemWrite = "Workers R2 Storage Bucket Item Write"
	// 账户分析读：读桶用量（GraphQL r2StorageAdaptiveGroups）。
	cfPermAccountAnalyticsRead = "Account Analytics Read"
	// 新签发的令牌要过一会儿才在 R2 的 S3 端点生效，检测按这个间隔重试。
	cloudflareCredentialPropagation = 3 * time.Second
)

var r2BucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// CloudflareAccount 令牌能访问的账户。
type CloudflareAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CloudflareConnection 令牌校验结果。
type CloudflareConnection struct {
	TokenID       string              `json:"tokenId"`
	TokenKind     string              `json:"tokenKind"` // user / account
	Status        string              `json:"status"`
	ExpiresOn     *time.Time          `json:"expiresOn,omitempty"`
	Accounts      []CloudflareAccount `json:"accounts"`
	AccountsError string              `json:"accountsError,omitempty"`
}

// CloudflareBucket 一个 R2 桶。
type CloudflareBucket struct {
	Name         string `json:"name"`
	Jurisdiction string `json:"jurisdiction"`
	Location     string `json:"location,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	// ConfigID / ConfigName 已经接入为存储源时给出（同账户、同桶、同管辖区）。
	ConfigID   int64  `json:"configId,omitempty"`
	ConfigName string `json:"configName,omitempty"`
}

// CloudflareZone 账户下的域名（绑自定义域名用）。
type CloudflareZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// CloudflareResources 一个账户下的桶与域名。
type CloudflareResources struct {
	Buckets    []CloudflareBucket `json:"buckets"`
	Zones      []CloudflareZone   `json:"zones"`
	ZonesError string             `json:"zonesError,omitempty"`
}

// CloudflareCustomDomain 桶上绑的自定义域名。
type CloudflareCustomDomain struct {
	Domain    string `json:"domain"`
	Enabled   bool   `json:"enabled"`
	ZoneName  string `json:"zoneName,omitempty"`
	Ownership string `json:"ownership,omitempty"`
	SSL       string `json:"ssl,omitempty"`
}

// CloudflareBucketDetail 桶的公开访问与 CORS 现状。
type CloudflareBucketDetail struct {
	ManagedDomain        string                   `json:"managedDomain,omitempty"`
	ManagedDomainEnabled bool                     `json:"managedDomainEnabled"`
	CustomDomains        []CloudflareCustomDomain `json:"customDomains"`
	CORSOrigins          []string                 `json:"corsOrigins"`
}

// CloudflareTokenInput 每个请求都带令牌（不落库）。
type CloudflareTokenInput struct {
	Token     string `json:"token"`
	AccountID string `json:"accountId"`
}

// CloudflareBucketInput 建桶 / 查桶。
type CloudflareBucketInput struct {
	CloudflareTokenInput
	Bucket       string `json:"bucket"`
	Jurisdiction string `json:"jurisdiction"`
	LocationHint string `json:"locationHint"`
	StorageClass string `json:"storageClass"`
}

// 公开访问方式。
const (
	CloudflarePublicPrivate = "private" // 私有，经 Aegis 代理下载
	CloudflarePublicR2Dev   = "r2dev"   // r2.dev 子域（限速，适合测试）
	CloudflarePublicCustom  = "custom"  // 自定义域名
)

// 凭据方式。
const (
	CloudflareCredentialCreate = "create" // 签发只能访问这个桶的专用令牌
	CloudflareCredentialReuse  = "reuse"  // 直接用当前令牌派生
)

// CloudflareSetupInput 一键配置。
type CloudflareSetupInput struct {
	CloudflareTokenInput
	Bucket         string   `json:"bucket"`
	Jurisdiction   string   `json:"jurisdiction"`
	CredentialMode string   `json:"credentialMode"`
	PublicAccess   string   `json:"publicAccess"`
	CustomDomain   string   `json:"customDomain"`
	ZoneID         string   `json:"zoneId"`
	CORSOrigins    []string `json:"corsOrigins"`
	// ConfigID 非 0 时更新这个已有的存储源（换凭据、改公开访问），否则新建。
	ConfigID    int64  `json:"configId"`
	ConfigName  string `json:"configName"`
	Description string `json:"description"`
	SetDefault  bool   `json:"setDefault"`
	PoolID      int64  `json:"poolId"`
	PoolMode    string `json:"poolMode"`
}

// CloudflareSetupStep 一键配置的一步。
type CloudflareSetupStep struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok / failed / skipped
	Message string `json:"message,omitempty"`
}

// CloudflareSetupResult 一键配置结果。
type CloudflareSetupResult struct {
	Steps  []CloudflareSetupStep      `json:"steps"`
	Config *storagedomain.Config      `json:"config,omitempty"`
	Probe  *storagedomain.ProbeResult `json:"probe,omitempty"`
}

// ── 客户端 ──────────────────────────

func (s *StorageService) cloudflareClient(token string) (*cloudflare.Client, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, apperrors.New(42210, http.StatusUnprocessableEntity, "请填写 Cloudflare API 令牌")
	}
	base := s.cloudflareBaseURL
	if base == "" {
		base = "https://api.cloudflare.com/client/v4/"
	}
	httpClient := s.cloudflareHTTP
	if httpClient == nil {
		httpClient = &http.Client{Transport: newStorageOutboundTransport(), Timeout: cloudflareRequestTimeout}
	}
	return cloudflare.NewClient(
		option.WithBaseURL(base),
		option.WithHTTPClient(httpClient),
		option.WithAPIToken(token),
		// SDK 会读 CLOUDFLARE_API_KEY 等环境变量；这里只认管理员贴进来的令牌。
		option.WithHeaderDel("X-Auth-Key"),
		option.WithHeaderDel("X-Auth-Email"),
		option.WithHeaderDel("X-Auth-User-Service-Key"),
		option.WithMaxRetries(1),
	), nil
}

// cloudflareError 把 SDK 的错误换成管理员看得懂的一句话。
func cloudflareError(action string, err error) error {
	var apiErr *cloudflare.Error
	if errors.As(err, &apiErr) {
		message := ""
		if len(apiErr.Errors) > 0 {
			message = apiErr.Errors[0].Message
		}
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized:
			return apperrors.New(42210, http.StatusUnprocessableEntity, action+"失败：令牌无效或已过期")
		case apiErr.StatusCode == http.StatusForbidden:
			return apperrors.New(42211, http.StatusUnprocessableEntity, action+"失败：令牌缺少所需权限"+suffix(message))
		case apiErr.StatusCode >= 500:
			return apperrors.New(50390, http.StatusServiceUnavailable, action+"失败：Cloudflare 暂时不可用")
		default:
			return apperrors.New(42211, http.StatusUnprocessableEntity, action+"失败"+suffix(message))
		}
	}
	return apperrors.New(50390, http.StatusServiceUnavailable, action+"失败："+truncateStorageError(err))
}

func suffix(message string) string {
	if message == "" {
		return ""
	}
	return "（" + message + "）"
}

func normalizeJurisdiction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case r2JurisdictionEU:
		return r2JurisdictionEU
	case r2JurisdictionFedRAMP:
		return r2JurisdictionFedRAMP
	default:
		return r2JurisdictionDefault
	}
}

// ── 连接 ──────────────────────────

// ConnectCloudflare 校验令牌并列出能访问的账户。
func (s *StorageService) ConnectCloudflare(ctx context.Context, token string) (*CloudflareConnection, error) {
	client, err := s.cloudflareClient(token)
	if err != nil {
		return nil, err
	}
	result := &CloudflareConnection{Accounts: []CloudflareAccount{}}
	page, accountsErr := client.Accounts.List(ctx, cfaccounts.AccountListParams{PerPage: cloudflare.F(50.0)})
	if accountsErr == nil {
		for _, account := range page.Result {
			result.Accounts = append(result.Accounts, CloudflareAccount{ID: account.ID, Name: account.Name})
		}
	} else {
		result.AccountsError = cloudflareError("列出账户", accountsErr).Error()
	}

	if verified, err := client.User.Tokens.Verify(ctx); err == nil {
		result.TokenKind = "user"
		result.TokenID = verified.ID
		result.Status = string(verified.Status)
		if !verified.ExpiresOn.IsZero() {
			expires := verified.ExpiresOn
			result.ExpiresOn = &expires
		}
	} else {
		var verifyErr error = err
		for _, account := range result.Accounts {
			verified, err := client.Accounts.Tokens.Verify(ctx, cfaccounts.TokenVerifyParams{AccountID: cloudflare.F(account.ID)})
			if err != nil {
				verifyErr = err
				continue
			}
			result.TokenKind = "account"
			result.TokenID = verified.ID
			result.Status = string(verified.Status)
			if !verified.ExpiresOn.IsZero() {
				expires := verified.ExpiresOn
				result.ExpiresOn = &expires
			}
			break
		}
		if result.TokenKind == "" {
			return nil, cloudflareError("校验令牌", verifyErr)
		}
	}
	if result.Status != "" && result.Status != "active" {
		return nil, apperrors.New(42210, http.StatusUnprocessableEntity, "令牌状态为 "+result.Status+"，不可使用")
	}
	return result, nil
}

// ── 资源 ──────────────────────────

// CloudflareResources 列出账户下的桶（三个管辖区）与域名。
func (s *StorageService) CloudflareResources(ctx context.Context, input CloudflareTokenInput) (*CloudflareResources, error) {
	client, err := s.cloudflareClient(input.Token)
	if err != nil {
		return nil, err
	}
	accountID := strings.TrimSpace(input.AccountID)
	if accountID == "" {
		return nil, apperrors.New(42213, http.StatusUnprocessableEntity, "请选择账户")
	}
	existing := s.existingR2Configs(ctx, accountID)
	result := &CloudflareResources{Buckets: []CloudflareBucket{}, Zones: []CloudflareZone{}}
	for _, jurisdiction := range []string{r2JurisdictionDefault, r2JurisdictionEU, r2JurisdictionFedRAMP} {
		params := r2.BucketListParams{AccountID: cloudflare.F(accountID), PerPage: cloudflare.F(1000.0)}
		if jurisdiction != r2JurisdictionDefault {
			params.CfR2Jurisdiction = cloudflare.F(r2.BucketListParamsCfR2Jurisdiction(jurisdiction))
		}
		listed, err := client.R2.Buckets.List(ctx, params)
		if err != nil {
			// 默认管辖区都列不出来，就是令牌没有 R2 权限；欧盟 / FedRAMP 列不出来多半是没开通，忽略。
			if jurisdiction == r2JurisdictionDefault {
				return nil, cloudflareError("列出存储桶", err)
			}
			continue
		}
		for _, bucket := range listed.Buckets {
			item := CloudflareBucket{
				Name: bucket.Name, Jurisdiction: jurisdiction, Location: string(bucket.Location),
				StorageClass: string(bucket.StorageClass), CreatedAt: bucket.CreationDate,
			}
			if cfg, ok := existing[jurisdiction+"/"+bucket.Name]; ok {
				item.ConfigID, item.ConfigName = cfg.ID, cfg.ConfigName
			}
			result.Buckets = append(result.Buckets, item)
		}
	}
	zonePage, err := client.Zones.List(ctx, zones.ZoneListParams{
		Account: cloudflare.F(zones.ZoneListParamsAccount{ID: cloudflare.F(accountID)}),
		PerPage: cloudflare.F(50.0),
	})
	if err != nil {
		result.ZonesError = cloudflareError("列出域名", err).Error()
	} else {
		for _, zone := range zonePage.Result {
			result.Zones = append(result.Zones, CloudflareZone{ID: zone.ID, Name: zone.Name, Status: string(zone.Status)})
		}
	}
	return result, nil
}

// existingR2Configs 已接入的 R2 存储源，键为「管辖区/桶名」。
func (s *StorageService) existingR2Configs(ctx context.Context, accountID string) map[string]storagedomain.Config {
	out := map[string]storagedomain.Config{}
	configs, err := s.pg.ListStorageConfigs(ctx, storagedomain.ListQuery{Scope: storagedomain.ScopeGlobal, Provider: storagedomain.ProviderR2})
	if err != nil {
		return out
	}
	for _, cfg := range configs {
		raw, err := decodeR2Config(cfg.ConfigData)
		if err != nil || raw.AccountID != strings.ToLower(accountID) {
			continue
		}
		out[raw.Jurisdiction+"/"+raw.Bucket] = cfg
	}
	return out
}

// CreateCloudflareBucket 建桶。
func (s *StorageService) CreateCloudflareBucket(ctx context.Context, input CloudflareBucketInput) (*CloudflareBucket, error) {
	client, err := s.cloudflareClient(input.Token)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Bucket)
	if !r2BucketNamePattern.MatchString(name) {
		return nil, apperrors.New(42212, http.StatusUnprocessableEntity, "桶名需为 3–63 位小写字母、数字或连字符，且以字母或数字开头结尾")
	}
	jurisdiction := normalizeJurisdiction(input.Jurisdiction)
	params := r2.BucketNewParams{AccountID: cloudflare.F(strings.TrimSpace(input.AccountID)), Name: cloudflare.F(name)}
	if jurisdiction != r2JurisdictionDefault {
		params.CfR2Jurisdiction = cloudflare.F(r2.BucketNewParamsCfR2Jurisdiction(jurisdiction))
	} else if hint := strings.TrimSpace(input.LocationHint); hint != "" {
		params.LocationHint = cloudflare.F(r2.BucketNewParamsLocationHint(hint))
	}
	if class := strings.TrimSpace(input.StorageClass); class != "" {
		params.StorageClass = cloudflare.F(r2.BucketNewParamsStorageClass(class))
	}
	bucket, err := client.R2.Buckets.New(ctx, params)
	if err != nil {
		return nil, cloudflareError("创建存储桶", err)
	}
	return &CloudflareBucket{
		Name: bucket.Name, Jurisdiction: jurisdiction, Location: string(bucket.Location),
		StorageClass: string(bucket.StorageClass), CreatedAt: bucket.CreationDate,
	}, nil
}

// CloudflareBucketDetail 桶的 r2.dev、自定义域名与 CORS。
func (s *StorageService) CloudflareBucketDetail(ctx context.Context, input CloudflareBucketInput) (*CloudflareBucketDetail, error) {
	client, err := s.cloudflareClient(input.Token)
	if err != nil {
		return nil, err
	}
	accountID, bucket, jurisdiction := strings.TrimSpace(input.AccountID), strings.TrimSpace(input.Bucket), normalizeJurisdiction(input.Jurisdiction)
	detail := &CloudflareBucketDetail{CustomDomains: []CloudflareCustomDomain{}, CORSOrigins: []string{}}

	managedParams := r2.BucketDomainManagedListParams{AccountID: cloudflare.F(accountID)}
	if jurisdiction != r2JurisdictionDefault {
		managedParams.CfR2Jurisdiction = cloudflare.F(r2.BucketDomainManagedListParamsCfR2Jurisdiction(jurisdiction))
	}
	managed, err := client.R2.Buckets.Domains.Managed.List(ctx, bucket, managedParams)
	if err != nil {
		return nil, cloudflareError("读取 r2.dev 域名", err)
	}
	detail.ManagedDomain, detail.ManagedDomainEnabled = managed.Domain, managed.Enabled

	customParams := r2.BucketDomainCustomListParams{AccountID: cloudflare.F(accountID)}
	if jurisdiction != r2JurisdictionDefault {
		customParams.CfR2Jurisdiction = cloudflare.F(r2.BucketDomainCustomListParamsCfR2Jurisdiction(jurisdiction))
	}
	if custom, err := client.R2.Buckets.Domains.Custom.List(ctx, bucket, customParams); err == nil {
		for _, domain := range custom.Domains {
			detail.CustomDomains = append(detail.CustomDomains, CloudflareCustomDomain{
				Domain: domain.Domain, Enabled: domain.Enabled, ZoneName: domain.ZoneName,
				Ownership: string(domain.Status.Ownership), SSL: string(domain.Status.SSL),
			})
		}
	}

	corsParams := r2.BucketCORSGetParams{AccountID: cloudflare.F(accountID)}
	if jurisdiction != r2JurisdictionDefault {
		corsParams.CfR2Jurisdiction = cloudflare.F(r2.BucketCORSGetParamsCfR2Jurisdiction(jurisdiction))
	}
	if cors, err := client.R2.Buckets.CORS.Get(ctx, bucket, corsParams); err == nil {
		seen := map[string]bool{}
		for _, rule := range cors.Rules {
			for _, origin := range rule.Allowed.Origins {
				if !seen[origin] {
					seen[origin] = true
					detail.CORSOrigins = append(detail.CORSOrigins, origin)
				}
			}
		}
	}
	return detail, nil
}

// ── 一键配置 ──────────────────────────

// SetupCloudflareR2 凭据 → 公开访问 → CORS → 保存存储源 → 深度检测 → 加入存储池。
// 凭据或保存失败就停；公开访问、CORS、加入存储池失败只记在步骤里，存储源照样可用。
func (s *StorageService) SetupCloudflareR2(ctx context.Context, input CloudflareSetupInput) (*CloudflareSetupResult, error) {
	client, err := s.cloudflareClient(input.Token)
	if err != nil {
		return nil, err
	}
	accountID := strings.ToLower(strings.TrimSpace(input.AccountID))
	bucket := strings.TrimSpace(input.Bucket)
	jurisdiction := normalizeJurisdiction(input.Jurisdiction)
	if accountID == "" || bucket == "" {
		return nil, apperrors.New(42213, http.StatusUnprocessableEntity, "请选择账户与存储桶")
	}
	if !r2AccountIDPattern.MatchString(accountID) {
		return nil, apperrors.New(42282, http.StatusUnprocessableEntity, "R2 账户 ID 应为 32 位十六进制字符")
	}
	publicAccess := input.PublicAccess
	switch publicAccess {
	case "", CloudflarePublicPrivate:
		publicAccess = CloudflarePublicPrivate
	case CloudflarePublicR2Dev:
	case CloudflarePublicCustom:
		input.CustomDomain = strings.ToLower(strings.TrimSpace(input.CustomDomain))
		if input.CustomDomain == "" || strings.TrimSpace(input.ZoneID) == "" {
			return nil, apperrors.New(42213, http.StatusUnprocessableEntity, "请选择域名并填写自定义域名")
		}
	default:
		return nil, apperrors.New(42213, http.StatusUnprocessableEntity, "不支持的公开访问方式")
	}
	var existing *storagedomain.Config
	if input.ConfigID > 0 {
		existing, err = s.pg.GetStorageConfigByID(ctx, input.ConfigID)
		if err != nil {
			return nil, err
		}
		if existing == nil || existing.Provider != storagedomain.ProviderR2 || existing.Scope != storagedomain.ScopeGlobal {
			return nil, apperrors.New(42213, http.StatusUnprocessableEntity, "要更新的存储源不存在或不是平台级 R2")
		}
	}
	configName := strings.TrimSpace(input.ConfigName)
	if configName == "" {
		configName = "r2-" + bucket
	}

	result := &CloudflareSetupResult{Steps: []CloudflareSetupStep{}}
	record := func(name, status, message string) {
		result.Steps = append(result.Steps, CloudflareSetupStep{Name: name, Status: status, Message: message})
	}

	// 1. 凭据
	connection, err := s.ConnectCloudflare(ctx, input.Token)
	if err != nil {
		return nil, err
	}
	var accessKeyID, secretKey, apiToken, tokenID, tokenKind string
	if input.CredentialMode == CloudflareCredentialReuse {
		tokenID, tokenKind, apiToken = connection.TokenID, connection.TokenKind, strings.TrimSpace(input.Token)
		record("凭据", "ok", "使用当前令牌")
	} else {
		created, kind, err := s.createR2BucketToken(ctx, client, connection.TokenKind, accountID, bucket, jurisdiction, configName)
		if err != nil {
			record("凭据", "failed", err.Error())
			return result, nil
		}
		tokenID, tokenKind, apiToken = created.id, kind, created.value
		record("凭据", "ok", "已签发只能访问 "+bucket+" 的专用令牌")
	}
	sum := sha256.Sum256([]byte(apiToken))
	accessKeyID, secretKey = tokenID, hex.EncodeToString(sum[:])

	// 2. 公开访问
	accessMode, proxy, baseURL := storagedomain.AccessPrivate, true, ""
	switch publicAccess {
	case CloudflarePublicR2Dev:
		params := r2.BucketDomainManagedUpdateParams{AccountID: cloudflare.F(accountID), Enabled: cloudflare.F(true)}
		if jurisdiction != r2JurisdictionDefault {
			params.CfR2Jurisdiction = cloudflare.F(r2.BucketDomainManagedUpdateParamsCfR2Jurisdiction(jurisdiction))
		}
		managed, err := client.R2.Buckets.Domains.Managed.Update(ctx, bucket, params)
		if err != nil {
			record("公开访问", "failed", cloudflareError("开启 r2.dev", err).Error())
		} else {
			accessMode, proxy, baseURL = storagedomain.AccessPublic, false, "https://"+managed.Domain
			record("公开访问", "ok", managed.Domain)
		}
	case CloudflarePublicCustom:
		if err := s.attachR2CustomDomain(ctx, client, accountID, bucket, jurisdiction, input.CustomDomain, input.ZoneID); err != nil {
			record("公开访问", "failed", err.Error())
		} else {
			accessMode, proxy, baseURL = storagedomain.AccessPublic, false, "https://"+input.CustomDomain
			record("公开访问", "ok", input.CustomDomain+"（证书签发可能需要几分钟）")
		}
	default:
		record("公开访问", "skipped", "私有，经 Aegis 代理下载")
	}

	// 3. CORS（浏览器直传）
	origins := cleanOrigins(input.CORSOrigins)
	if len(origins) == 0 {
		record("CORS", "skipped", "未填写来源")
	} else if err := s.mergeR2CORS(ctx, client, accountID, bucket, jurisdiction, origins); err != nil {
		record("CORS", "failed", err.Error())
	} else {
		record("CORS", "ok", strings.Join(origins, "、"))
	}

	// 4. 保存存储源
	data := map[string]any{
		"account_id": accountID, "bucket": bucket, "jurisdiction": jurisdiction,
		"access_key_id": accessKeyID, "secret_access_key": secretKey, "api_token": apiToken,
		"token_id": tokenID, "token_kind": tokenKind, "managed_by": "cloudflare",
	}
	mutation := storagedomain.ConfigMutation{
		Scope: storagedomain.ScopeGlobal, Provider: ptr(storagedomain.ProviderR2), ConfigName: ptr(configName),
		AccessMode: ptr(accessMode), Enabled: ptr(true), ProxyDownload: ptr(proxy), BaseURL: ptr(baseURL),
		Description: ptr(strings.TrimSpace(input.Description)), ConfigData: data,
	}
	if input.SetDefault {
		mutation.IsDefault = ptr(true)
	}
	if existing != nil {
		mutation.ID = existing.ID
		mutation.ConfigName = ptr(existing.ConfigName)
	}
	saved, err := s.Save(ctx, mutation)
	if err != nil {
		record("保存存储源", "failed", storageErrorMessage(err))
		return result, nil
	}
	redacted := storagedomain.RedactConfig(*saved)
	result.Config = &redacted
	record("保存存储源", "ok", saved.ConfigName)

	// 5. 深度检测（新令牌要几秒才在 S3 端点生效）
	var probe *storagedomain.ProbeResult
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return result, nil
			case <-time.After(cloudflareCredentialPropagation):
			}
		}
		probe, err = s.ProbeConfig(ctx, saved.ID)
		if err != nil || probe.OK {
			break
		}
	}
	if err != nil {
		record("深度检测", "failed", storageErrorMessage(err))
	} else {
		result.Probe = probe
		if probe.OK {
			s.pools.Reset(saved.ID)
			record("深度检测", "ok", "连通、写入、读回、删除均通过")
		} else {
			record("深度检测", "failed", firstFailedProbeStep(probe))
		}
	}

	// 6. 加入存储池
	if input.PoolID > 0 {
		if err := s.addToPool(ctx, input.PoolID, saved.ID, input.PoolMode); err != nil {
			record("加入存储池", "failed", storageErrorMessage(err))
		} else {
			record("加入存储池", "ok", "")
		}
	} else {
		record("加入存储池", "skipped", "")
	}
	s.emitPoolEvent(ctx, 0, saved.ID, storagedomain.EventLevelInfo, storagedomain.EventConfig,
		fmt.Sprintf("经 Cloudflare 快速接入配置了 %s（桶 %s）", saved.ConfigName, bucket), nil, fmt.Sprintf("%d", time.Now().UnixNano()))
	return result, nil
}

type createdToken struct {
	id    string
	value string
}

// createR2BucketToken 签发只能读写这一个桶、外加账户分析读的令牌。按令牌种类先试对应的签发接口，
// 不行再试另一种（账户管理员的用户令牌也可能只有账户级的令牌管理权限）。
func (s *StorageService) createR2BucketToken(ctx context.Context, client *cloudflare.Client, preferredKind, accountID, bucket, jurisdiction, label string) (*createdToken, string, error) {
	bucketResource := fmt.Sprintf("com.cloudflare.edge.r2.bucket.%s_%s_%s", accountID, jurisdiction, bucket)
	accountResource := "com.cloudflare.api.account." + accountID
	name := "Aegis · " + label

	buildPolicies := func(groups map[string]string) ([]shared.TokenPolicyParam, error) {
		writeID := groups[cfPermR2BucketItemWrite]
		if writeID == "" {
			return nil, apperrors.New(42211, http.StatusUnprocessableEntity, "找不到权限组「"+cfPermR2BucketItemWrite+"」")
		}
		policies := []shared.TokenPolicyParam{{
			Effect:           cloudflare.F(shared.TokenPolicyEffectAllow),
			PermissionGroups: cloudflare.F([]shared.TokenPolicyPermissionGroupParam{{ID: cloudflare.F(writeID)}}),
			Resources:        cloudflare.F[shared.TokenPolicyResourcesUnionParam](shared.TokenPolicyResourcesIAMResourcesTypeObjectStringParam{bucketResource: "*"}),
		}}
		if analyticsID := groups[cfPermAccountAnalyticsRead]; analyticsID != "" {
			policies = append(policies, shared.TokenPolicyParam{
				Effect:           cloudflare.F(shared.TokenPolicyEffectAllow),
				PermissionGroups: cloudflare.F([]shared.TokenPolicyPermissionGroupParam{{ID: cloudflare.F(analyticsID)}}),
				Resources:        cloudflare.F[shared.TokenPolicyResourcesUnionParam](shared.TokenPolicyResourcesIAMResourcesTypeObjectStringParam{accountResource: "*"}),
			})
		}
		return policies, nil
	}

	tryAccount := func() (*createdToken, error) {
		page, err := client.Accounts.Tokens.PermissionGroups.List(ctx, cfaccounts.TokenPermissionGroupListParams{AccountID: cloudflare.F(accountID)})
		if err != nil {
			return nil, cloudflareError("读取权限组", err)
		}
		groups := map[string]string{}
		for _, group := range page.Result {
			groups[group.Name] = group.ID
		}
		policies, err := buildPolicies(groups)
		if err != nil {
			return nil, err
		}
		created, err := client.Accounts.Tokens.New(ctx, cfaccounts.TokenNewParams{
			AccountID: cloudflare.F(accountID), Name: cloudflare.F(name), Policies: cloudflare.F(policies),
		})
		if err != nil {
			return nil, cloudflareError("签发专用令牌", err)
		}
		return &createdToken{id: created.ID, value: created.Value}, nil
	}
	tryUser := func() (*createdToken, error) {
		page, err := client.User.Tokens.PermissionGroups.List(ctx, cfuser.TokenPermissionGroupListParams{})
		if err != nil {
			return nil, cloudflareError("读取权限组", err)
		}
		groups := map[string]string{}
		for _, group := range page.Result {
			groups[group.Name] = group.ID
		}
		policies, err := buildPolicies(groups)
		if err != nil {
			return nil, err
		}
		created, err := client.User.Tokens.New(ctx, cfuser.TokenNewParams{Name: cloudflare.F(name), Policies: cloudflare.F(policies)})
		if err != nil {
			return nil, cloudflareError("签发专用令牌", err)
		}
		return &createdToken{id: created.ID, value: created.Value}, nil
	}

	order := []struct {
		kind string
		fn   func() (*createdToken, error)
	}{{"account", tryAccount}, {"user", tryUser}}
	if preferredKind == "user" {
		order[0], order[1] = order[1], order[0]
	}
	var firstErr error
	for _, attempt := range order {
		created, err := attempt.fn()
		if err == nil && created.id != "" && created.value != "" {
			return created, attempt.kind, nil
		}
		if err == nil {
			err = apperrors.New(42211, http.StatusUnprocessableEntity, "签发专用令牌失败：Cloudflare 没有返回令牌值")
		}
		if firstErr == nil {
			firstErr = err
		}
		s.log.Info("签发 R2 专用令牌失败，换另一种令牌接口", zap.String("kind", attempt.kind), zap.Error(err))
	}
	return nil, "", apperrors.New(42211, http.StatusUnprocessableEntity,
		storageErrorMessage(firstErr)+"。令牌需要「API 令牌：编辑」权限才能签发专用凭据，也可以改用当前令牌")
}

// attachR2CustomDomain 把自定义域名绑到桶上；已经绑过就跳过。
func (s *StorageService) attachR2CustomDomain(ctx context.Context, client *cloudflare.Client, accountID, bucket, jurisdiction, domain, zoneID string) error {
	listParams := r2.BucketDomainCustomListParams{AccountID: cloudflare.F(accountID)}
	if jurisdiction != r2JurisdictionDefault {
		listParams.CfR2Jurisdiction = cloudflare.F(r2.BucketDomainCustomListParamsCfR2Jurisdiction(jurisdiction))
	}
	if current, err := client.R2.Buckets.Domains.Custom.List(ctx, bucket, listParams); err == nil {
		for _, item := range current.Domains {
			if strings.EqualFold(item.Domain, domain) {
				return nil
			}
		}
	}
	params := r2.BucketDomainCustomNewParams{
		AccountID: cloudflare.F(accountID), Domain: cloudflare.F(domain), Enabled: cloudflare.F(true),
		ZoneID: cloudflare.F(strings.TrimSpace(zoneID)), MinTLS: cloudflare.F(r2.BucketDomainCustomNewParamsMinTLS1_2),
	}
	if jurisdiction != r2JurisdictionDefault {
		params.CfR2Jurisdiction = cloudflare.F(r2.BucketDomainCustomNewParamsCfR2Jurisdiction(jurisdiction))
	}
	if _, err := client.R2.Buckets.Domains.Custom.New(ctx, bucket, params); err != nil {
		return cloudflareError("绑定自定义域名", err)
	}
	return nil
}

// mergeR2CORS 写入允许直传的 CORS 规则：保留桶上其他规则，只替换 Aegis 自己那一条。
func (s *StorageService) mergeR2CORS(ctx context.Context, client *cloudflare.Client, accountID, bucket, jurisdiction string, origins []string) error {
	const ruleID = "aegis-direct-upload"
	getParams := r2.BucketCORSGetParams{AccountID: cloudflare.F(accountID)}
	updateParams := r2.BucketCORSUpdateParams{AccountID: cloudflare.F(accountID)}
	if jurisdiction != r2JurisdictionDefault {
		getParams.CfR2Jurisdiction = cloudflare.F(r2.BucketCORSGetParamsCfR2Jurisdiction(jurisdiction))
		updateParams.CfR2Jurisdiction = cloudflare.F(r2.BucketCORSUpdateParamsCfR2Jurisdiction(jurisdiction))
	}
	rules := []r2.BucketCORSUpdateParamsRule{}
	if current, err := client.R2.Buckets.CORS.Get(ctx, bucket, getParams); err == nil {
		for _, rule := range current.Rules {
			if rule.ID == ruleID {
				continue
			}
			methods := make([]r2.BucketCORSUpdateParamsRulesAllowedMethod, 0, len(rule.Allowed.Methods))
			for _, method := range rule.Allowed.Methods {
				methods = append(methods, r2.BucketCORSUpdateParamsRulesAllowedMethod(method))
			}
			kept := r2.BucketCORSUpdateParamsRule{
				Allowed: cloudflare.F(r2.BucketCORSUpdateParamsRulesAllowed{
					Methods: cloudflare.F(methods), Origins: cloudflare.F(rule.Allowed.Origins), Headers: cloudflare.F(rule.Allowed.Headers),
				}),
				ExposeHeaders: cloudflare.F(rule.ExposeHeaders),
				MaxAgeSeconds: cloudflare.F(rule.MaxAgeSeconds),
			}
			if rule.ID != "" {
				kept.ID = cloudflare.F(rule.ID)
			}
			rules = append(rules, kept)
		}
	}
	rules = append(rules, r2.BucketCORSUpdateParamsRule{
		ID: cloudflare.F(ruleID),
		Allowed: cloudflare.F(r2.BucketCORSUpdateParamsRulesAllowed{
			Methods: cloudflare.F([]r2.BucketCORSUpdateParamsRulesAllowedMethod{
				r2.BucketCORSUpdateParamsRulesAllowedMethodGet, r2.BucketCORSUpdateParamsRulesAllowedMethodHead,
				r2.BucketCORSUpdateParamsRulesAllowedMethodPut,
			}),
			Origins: cloudflare.F(origins),
			Headers: cloudflare.F([]string{"*"}),
		}),
		ExposeHeaders: cloudflare.F([]string{"ETag"}),
		MaxAgeSeconds: cloudflare.F(3600.0),
	})
	updateParams.Rules = cloudflare.F(rules)
	if _, err := client.R2.Buckets.CORS.Update(ctx, bucket, updateParams); err != nil {
		return cloudflareError("写入 CORS", err)
	}
	return nil
}

// addToPool 把存储源追加为池的最后一个成员（已在池里则只改角色）。
func (s *StorageService) addToPool(ctx context.Context, poolID, configID int64, mode string) error {
	pool, err := s.pg.GetStoragePool(ctx, poolID)
	if err != nil {
		return mapStoragePoolError(err)
	}
	if mode == "" {
		mode = storagedomain.MemberModeActive
	}
	input := storagedomain.PoolInput{
		Name: pool.Name, Description: pool.Description, Strategy: pool.Strategy, Enabled: pool.Enabled,
		IsDefault: pool.IsDefault, ReadFallback: pool.ReadFallback, MaxAttempts: pool.MaxAttempts, WarnPercent: pool.WarnPercent,
	}
	found, last := false, 0
	for _, member := range pool.Members {
		item := storagedomain.PoolMemberInput{ConfigID: member.ConfigID, Priority: member.Priority, Weight: member.Weight,
			CapacityBytes: member.CapacityBytes, Mode: member.Mode}
		if member.ConfigID == configID {
			item.Mode, found = mode, true
		}
		if member.Priority > last {
			last = member.Priority
		}
		input.Members = append(input.Members, item)
	}
	if !found {
		input.Members = append(input.Members, storagedomain.PoolMemberInput{ConfigID: configID, Priority: last + 10, Weight: 1, Mode: mode})
	}
	_, err = s.SavePool(ctx, pool.ID, input)
	return err
}

func cleanOrigins(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" || seen[value] {
			continue
		}
		if value != "*" && !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstFailedProbeStep(probe *storagedomain.ProbeResult) string {
	for _, step := range probe.Steps {
		if !step.OK && !step.Skipped {
			return step.Name + "：" + step.Message
		}
	}
	return "未通过"
}

func storageErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return truncateStorageError(err)
}

func ptr[T any](value T) *T { return &value }
