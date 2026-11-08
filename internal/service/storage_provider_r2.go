package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
)

// Cloudflare R2：S3 兼容 API，经 aws-sdk-go-v2 访问，与 S3 共用 s3StorageProvider 内核。
//
// 和普通 S3 兼容端点的区别：
//   - 端点由账户 ID 与管辖区拼出来（https://<account>.[eu.|fedramp.]r2.cloudflarestorage.com），
//     地区固定为 auto，只需要填账户 ID、桶与一对 R2 API 令牌的访问密钥；
//   - SDK（s3 v1.73 起）默认给每个请求附加 CRC 校验和尾部，R2 对 aws-chunked 尾部的支持
//     不完整，这里改成「仅在操作要求时」计算与校验；
//   - S3 端点不对外公开：公开访问要么绑自定义域名 / r2.dev 子域（填在 base_url），
//     要么由 Aegis 代理下载，要么发有时效的预签名地址；
//   - 可选填一个带 Account Analytics 读权限的 Cloudflare API 令牌，用 GraphQL 分析接口
//     读桶的真实用量，存储池按它判断容量，而不只是看 Aegis 自己索引过的文件。

var r2AccountIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// R2 管辖区。默认区不带前缀；欧盟与 FedRAMP 的桶只能经各自的端点访问。
const (
	r2JurisdictionDefault = "default"
	r2JurisdictionEU      = "eu"
	r2JurisdictionFedRAMP = "fedramp"
)

func decodeR2Config(data map[string]any) (*storagedomain.R2Config, error) {
	var cfg storagedomain.R2Config
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(42280, http.StatusUnprocessableEntity, "R2 配置解析失败")
	}
	cfg.AccountID = strings.ToLower(strings.TrimSpace(cfg.AccountID))
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	cfg.SecretAccessKey = strings.TrimSpace(cfg.SecretAccessKey)
	cfg.APIToken = strings.TrimSpace(cfg.APIToken)
	cfg.Jurisdiction = strings.ToLower(strings.TrimSpace(cfg.Jurisdiction))
	if cfg.AccountID == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, apperrors.New(42281, http.StatusUnprocessableEntity, "R2 配置不完整：账户 ID、存储桶、Access Key ID 与 Secret Access Key 均为必填")
	}
	if !r2AccountIDPattern.MatchString(cfg.AccountID) {
		return nil, apperrors.New(42282, http.StatusUnprocessableEntity, "R2 账户 ID 应为 32 位十六进制字符，可在 Cloudflare 控制台 R2 概览页右侧找到")
	}
	switch cfg.Jurisdiction {
	case "", r2JurisdictionDefault:
		cfg.Jurisdiction = r2JurisdictionDefault
	case r2JurisdictionEU, r2JurisdictionFedRAMP:
	default:
		return nil, apperrors.New(42283, http.StatusUnprocessableEntity, "R2 管辖区只能是 default、eu 或 fedramp")
	}
	return &cfg, nil
}

// r2Endpoint 账户级 S3 端点。
func r2Endpoint(cfg *storagedomain.R2Config) string {
	host := cfg.AccountID + ".r2.cloudflarestorage.com"
	if cfg.Jurisdiction == r2JurisdictionEU || cfg.Jurisdiction == r2JurisdictionFedRAMP {
		host = cfg.AccountID + "." + cfg.Jurisdiction + ".r2.cloudflarestorage.com"
	}
	return "https://" + host
}

// r2AsS3 把 R2 配置换成 S3 内核认识的形态。路径风格访问：桶名含点时虚拟主机风格的证书对不上。
func r2AsS3(data map[string]any) (*storagedomain.S3Config, error) {
	cfg, err := decodeR2Config(data)
	if err != nil {
		return nil, err
	}
	return &storagedomain.S3Config{
		Endpoint:        r2Endpoint(cfg),
		Region:          "auto",
		Bucket:          cfg.Bucket,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		UseSSL:          true,
		ForcePathStyle:  true,
	}, nil
}

func newR2Provider() *s3StorageProvider {
	return &s3StorageProvider{
		name:    storagedomain.ProviderR2,
		resolve: r2AsS3,
		tune: func(o *awss3.Options) {
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		},
		transfer: func(o *transfermanager.Options) {
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		},
		// 走出海网关：R2 在境外，按域名后缀由网关决定直连还是代理。
		httpClient:   &http.Client{Transport: newStorageOutboundTransport()},
		presignReads: true,
	}
}

// ── 桶用量（可选：需要 Cloudflare API 令牌） ──────────────────────────

// storageUsageReporter 能报告存储桶真实用量的提供商（可选能力）。
type storageUsageReporter interface {
	BucketUsage(ctx context.Context, cfg *storagedomain.Config) (*storagedomain.BucketUsage, error)
}

var errBucketUsageUnavailable = fmt.Errorf("bucket usage is not available for this provider")

const cloudflareGraphQLEndpoint = "https://api.cloudflare.com/client/v4/graphql"

// BucketUsage 经 Cloudflare GraphQL 分析接口读桶的最新存储量。没填 API 令牌时报不可用，
// 调用方退回 Aegis 自己索引的用量。
func (p *s3StorageProvider) BucketUsage(ctx context.Context, cfg *storagedomain.Config) (*storagedomain.BucketUsage, error) {
	if p.name != storagedomain.ProviderR2 {
		return nil, errBucketUsageUnavailable
	}
	raw, err := decodeR2Config(cfg.ConfigData)
	if err != nil {
		return nil, err
	}
	if raw.APIToken == "" {
		return nil, errBucketUsageUnavailable
	}
	// 存储量是每小时采样一次的序列，取最近一天里最新的那个点。
	now := time.Now().UTC()
	query := `query($account: String!, $bucket: String!, $start: Time!, $end: Time!) {
  viewer { accounts(filter: {accountTag: $account}) {
    r2StorageAdaptiveGroups(limit: 1, filter: {bucketName: $bucket, datetime_geq: $start, datetime_leq: $end}, orderBy: [datetime_DESC]) {
      max { objectCount payloadSize metadataSize }
      dimensions { datetime }
    }
  } }
}`
	body, _ := json.Marshal(map[string]any{
		"query": query,
		"variables": map[string]any{
			"account": raw.AccountID, "bucket": r2AnalyticsBucketName(raw),
			"start": now.Add(-24 * time.Hour).Format(time.RFC3339), "end": now.Format(time.RFC3339),
		},
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudflareGraphQLEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+raw.APIToken)
	request.Header.Set("Content-Type", "application/json")
	client := p.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer closeSilently(response.Body)
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Data struct {
			Viewer struct {
				Accounts []struct {
					Groups []struct {
						Max struct {
							ObjectCount  int64 `json:"objectCount"`
							PayloadSize  int64 `json:"payloadSize"`
							MetadataSize int64 `json:"metadataSize"`
						} `json:"max"`
						Dimensions struct {
							Datetime time.Time `json:"datetime"`
						} `json:"dimensions"`
					} `json:"r2StorageAdaptiveGroups"`
				} `json:"accounts"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("cloudflare analytics: status %d", response.StatusCode)
	}
	if len(decoded.Errors) > 0 {
		return nil, fmt.Errorf("cloudflare analytics: %s", decoded.Errors[0].Message)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cloudflare analytics: status %d", response.StatusCode)
	}
	usage := &storagedomain.BucketUsage{Source: "cloudflare-analytics"}
	if accounts := decoded.Data.Viewer.Accounts; len(accounts) > 0 && len(accounts[0].Groups) > 0 {
		group := accounts[0].Groups[0]
		usage.Bytes = group.Max.PayloadSize + group.Max.MetadataSize
		usage.Objects = group.Max.ObjectCount
		at := group.Dimensions.Datetime
		usage.SampledAt = &at
	}
	return usage, nil
}

// r2AnalyticsBucketName 分析数据集里受管辖区限制的桶要带「管辖区_」前缀，如 eu_my-bucket。
func r2AnalyticsBucketName(cfg *storagedomain.R2Config) string {
	if cfg.Jurisdiction == r2JurisdictionEU || cfg.Jurisdiction == r2JurisdictionFedRAMP {
		return cfg.Jurisdiction + "_" + cfg.Bucket
	}
	return cfg.Bucket
}

func (p *circuitBreakeredStorageProvider) BucketUsage(ctx context.Context, cfg *storagedomain.Config) (*storagedomain.BucketUsage, error) {
	reporter, ok := p.upstream.(storageUsageReporter)
	if !ok {
		return nil, errBucketUsageUnavailable
	}
	return reporter.BucketUsage(ctx, cfg)
}
