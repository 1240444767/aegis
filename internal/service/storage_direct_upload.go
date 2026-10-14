package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/minio/minio-go/v7"
	redislib "github.com/redis/go-redis/v9"
	"github.com/tencentyun/cos-go-sdk-v5"

	platformdomain "aegis/internal/domain/platform"
	plugindomain "aegis/internal/domain/plugin"
	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 浏览器直传：文件从浏览器直接 PUT 到对象存储，不经过 Aegis 进程。
//
// 经服务器中转的问题在大文件上最明显：一个几百 MB 的安装包要先穿过 Next.js
// 同源代理（有请求体上限）、再进 Go 进程、再出到存储，带宽与内存都算两遍，
// 慢的那一跳决定整体速度。直传只让服务器做两件轻活：
//
//  1. 准备：做与普通上传相同的治理判定与配置解析，签一个短时有效的 PUT 地址，
//     把「这次上传应该是什么样」（配置、对象键、声明的大小）记进 Redis；
//  2. 完成：按记录去存储 HEAD 一次，确认对象确实存在且大小与声明一致，
//     再写文件索引、触发上传钩子 —— 与 uploadWithConfig 的收尾相同。
//
// 只有 S3 兼容（含 Cloudflare R2）、MinIO、阿里云 OSS、腾讯云 COS 能签预签名 PUT；
// 其余提供商（本地磁盘、WebDAV、网盘类）准备时返回 mode=server，调用方退回经服务器上传。
// 浏览器直传还要求存储桶的 CORS 允许控制台的来源发 PUT，否则浏览器会拦下请求，
// 控制台据此同样退回经服务器上传。

const (
	directUploadTTL    = 30 * time.Minute
	directUploadMaxTTL = 2 * time.Hour
)

// directUploadProvider 能签预签名 PUT 并查询对象元数据的提供商。
type directUploadProvider interface {
	PresignPut(ctx context.Context, cfg *storagedomain.Config, objectKey, contentType string, expires time.Duration) (string, map[string]string, error)
	StatObject(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*directObjectStat, error)
}

type directObjectStat struct {
	Size        int64
	ETag        string
	ContentType string
}

var errDirectUploadUnsupported = stderrors.New("storage provider does not support direct upload")

// asDirectUploader 取出提供商的直传能力；熔断包装要看里面那一层。
func asDirectUploader(provider storageProvider) (directUploadProvider, bool) {
	if wrapped, ok := provider.(*circuitBreakeredStorageProvider); ok {
		provider = wrapped.upstream
	}
	direct, ok := provider.(directUploadProvider)
	return direct, ok
}

// DirectUploadInput 准备直传的参数。ObjectKey 为相对配置根目录的对象键。
type DirectUploadInput struct {
	ConfigName  string
	ObjectKey   string
	FileName    string
	ContentType string
	Size        int64
	UploadedBy  *int64
	Metadata    map[string]string
	Expires     time.Duration
}

// DirectUploadTicket 准备结果。Mode 为 server 时其余字段为空，调用方改走经服务器上传。
type DirectUploadTicket struct {
	Mode      string            `json:"mode"`
	Method    string            `json:"method,omitempty"`
	URL       string            `json:"uploadUrl,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Token     string            `json:"token,omitempty"`
	ExpiresAt time.Time         `json:"expiresAt,omitzero"`
	Provider  string            `json:"provider,omitempty"`
	Reason    string            `json:"reason,omitempty"`
}

// pendingDirectUpload 记在 Redis 里的待完成上传。完成时只信这里，不信客户端再传一遍的参数。
type pendingDirectUpload struct {
	AppID       int64             `json:"appId"`
	ConfigID    int64             `json:"configId"`
	ObjectKey   string            `json:"objectKey"`
	FileName    string            `json:"fileName"`
	ContentType string            `json:"contentType"`
	Size        int64             `json:"size"`
	UploadedBy  *int64            `json:"uploadedBy,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// PrepareDirectUpload 校验并签发直传地址。提供商不支持时返回 Mode=server，不报错。
func (s *StorageService) PrepareDirectUpload(ctx context.Context, appID int64, input DirectUploadInput) (*DirectUploadTicket, error) {
	if s.governance != nil {
		if err := s.governance.EnsureCapability(appID, platformdomain.CapabilityStorage); err != nil {
			return nil, err
		}
	}
	if s.redis == nil {
		return &DirectUploadTicket{Mode: "server", Reason: "直传需要 Redis 记录上传状态"}, nil
	}
	if input.Size <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "文件大小无效")
	}
	cfg, err := s.resolveConfig(ctx, storagedomain.ResolveOptions{AppID: appID, ConfigName: input.ConfigName})
	if err != nil {
		return nil, err
	}
	provider, err := s.buildProvider(cfg)
	if err != nil {
		return nil, err
	}
	direct, ok := asDirectUploader(provider)
	if !ok {
		return &DirectUploadTicket{Mode: "server", Provider: cfg.Provider, Reason: "当前存储不支持浏览器直传"}, nil
	}
	objectKey := strings.TrimSpace(input.ObjectKey)
	if objectKey == "" {
		objectKey = buildUploadedObjectKey(input.FileName)
	}
	objectKey = normalizeObjectKey(cfg.RootPath, objectKey)
	if err := validateStorageObjectKey(objectKey); err != nil {
		return nil, err
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	expires := input.Expires
	if expires <= 0 {
		expires = directUploadTTL
	}
	expires = min(expires, directUploadMaxTTL)

	url, headers, err := direct.PresignPut(ctx, cfg, objectKey, contentType, expires)
	if stderrors.Is(err, errDirectUploadUnsupported) {
		return &DirectUploadTicket{Mode: "server", Provider: cfg.Provider, Reason: "当前存储不支持浏览器直传"}, nil
	}
	if err != nil {
		s.log.Warn("sign direct upload failed", zap.Int64("appid", appID), zap.String("provider", cfg.Provider), zap.Error(err))
		return nil, apperrors.New(50082, http.StatusBadGateway, "生成上传地址失败")
	}
	token, err := randomDirectToken()
	if err != nil {
		return nil, err
	}
	pending, _ := json.Marshal(pendingDirectUpload{
		AppID: appID, ConfigID: cfg.ID, ObjectKey: objectKey, FileName: strings.TrimSpace(input.FileName),
		ContentType: contentType, Size: input.Size, UploadedBy: input.UploadedBy, Metadata: input.Metadata,
	})
	// 记录比签名多留 10 分钟：大文件可能在临到期时才传完
	if err := s.redis.Set(ctx, s.directUploadKey(token), pending, expires+10*time.Minute).Err(); err != nil {
		return nil, err
	}
	return &DirectUploadTicket{
		Mode:      "direct",
		Method:    http.MethodPut,
		URL:       url,
		Headers:   headers,
		Token:     token,
		ExpiresAt: time.Now().Add(expires).UTC(),
		Provider:  cfg.Provider,
	}, nil
}

// CompleteDirectUpload 确认直传已落到存储并写入文件索引。令牌只能用一次。
func (s *StorageService) CompleteDirectUpload(ctx context.Context, appID int64, token string) (*storagedomain.StoredObject, error) {
	if s.redis == nil {
		return nil, apperrors.New(50380, http.StatusServiceUnavailable, "存储服务未启用直传")
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 128 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "上传令牌无效")
	}
	raw, err := s.redis.Get(ctx, s.directUploadKey(token)).Bytes()
	if stderrors.Is(err, redislib.Nil) {
		return nil, apperrors.New(40483, http.StatusNotFound, "上传令牌不存在或已过期，请重新上传")
	}
	if err != nil {
		return nil, err
	}
	var pending pendingDirectUpload
	if err := json.Unmarshal(raw, &pending); err != nil || pending.AppID != appID {
		return nil, apperrors.New(40000, http.StatusBadRequest, "上传令牌无效")
	}
	cfg, err := s.pg.GetStorageConfigByID(ctx, pending.ConfigID)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, apperrors.New(40482, http.StatusNotFound, "未配置可用存储服务")
	}
	provider, err := s.buildProvider(cfg)
	if err != nil {
		return nil, err
	}
	direct, ok := asDirectUploader(provider)
	if !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "当前存储不支持浏览器直传")
	}
	stat, err := direct.StatObject(ctx, cfg, pending.ObjectKey)
	if err != nil || stat == nil {
		return nil, apperrors.New(40484, http.StatusBadRequest, "存储中没有找到上传的文件，请确认上传已完成")
	}
	if stat.Size != pending.Size {
		return nil, apperrors.New(40485, http.StatusBadRequest,
			fmt.Sprintf("上传的文件大小（%d 字节）与声明（%d 字节）不一致，请重新上传", stat.Size, pending.Size))
	}
	// 先删令牌再写索引：同一令牌并发完成两次时只有一次能删成功
	if deleted, err := s.redis.Del(ctx, s.directUploadKey(token)).Result(); err != nil {
		return nil, err
	} else if deleted == 0 {
		return nil, apperrors.New(40483, http.StatusNotFound, "上传令牌已被使用")
	}

	contentType := pending.ContentType
	if stat.ContentType != "" {
		contentType = stat.ContentType
	}
	item := &storagedomain.StoredObject{
		Key:         pending.ObjectKey,
		FileName:    pending.FileName,
		Size:        stat.Size,
		ContentType: contentType,
		ETag:        strings.Trim(stat.ETag, "\""),
		ConfigID:    cfg.ID,
		Provider:    cfg.Provider,
		AccessMode:  cfg.AccessMode,
	}
	item.ProxyRequired = cfg.AccessMode == storagedomain.AccessPrivate || cfg.ProxyDownload
	if item.ProxyRequired {
		linkToken, err := s.links.Encode(storageLinkClaims{ConfigID: cfg.ID, ObjectKey: item.Key, AppID: appID})
		if err != nil {
			return nil, err
		}
		item.URL = StorageProxyPath(linkToken)
	} else if url, err := provider.PublicURL(ctx, cfg, item.Key, 0); err == nil {
		item.URL = url
	}

	meta := make(map[string]any, len(pending.Metadata)+1)
	for k, v := range pending.Metadata {
		meta[k] = v
	}
	meta["aegis:uploadMode"] = "direct"
	_, _ = s.pg.IndexStorageObject(ctx, storagedomain.StorageObject{
		ConfigID:     cfg.ID,
		AppID:        &appID,
		ObjectKey:    item.Key,
		FileName:     item.FileName,
		ContentType:  item.ContentType,
		Size:         item.Size,
		ETag:         item.ETag,
		UploadedBy:   pending.UploadedBy,
		UploaderType: "admin",
		Status:       "active",
		Metadata:     meta,
	})
	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookFileUploaded, map[string]any{
			"appId": appID, "objectKey": item.Key, "configId": cfg.ID, "provider": cfg.Provider,
		}, plugindomain.HookMetadata{AppID: &appID})
	}
	return item, nil
}

func (s *StorageService) directUploadKey(token string) string {
	return fmt.Sprintf("%s:storage:direct-upload:%s", s.keyPrefix, token)
}

func randomDirectToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ── 各提供商的直传实现 ──────────────────────────

func (p *s3StorageProvider) PresignPut(ctx context.Context, cfg *storagedomain.Config, objectKey, contentType string, expires time.Duration) (string, map[string]string, error) {
	client, raw, err := p.client(ctx, cfg)
	if err != nil {
		return "", nil, err
	}
	signed, err := awss3.NewPresignClient(client).PresignPutObject(ctx, &awss3.PutObjectInput{
		Bucket:      &raw.Bucket,
		Key:         &objectKey,
		ContentType: &contentType,
	}, awss3.WithPresignExpires(expires))
	if err != nil {
		return "", nil, err
	}
	return signed.URL, map[string]string{"Content-Type": contentType}, nil
}

func (p *s3StorageProvider) StatObject(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*directObjectStat, error) {
	client, raw, err := p.client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	head, err := client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: &raw.Bucket, Key: &objectKey})
	if err != nil {
		return nil, err
	}
	return &directObjectStat{Size: int64Value(head.ContentLength), ETag: stringValue(head.ETag), ContentType: stringValue(head.ContentType)}, nil
}

func (p *minioProvider) PresignPut(ctx context.Context, cfg *storagedomain.Config, objectKey, contentType string, expires time.Duration) (string, map[string]string, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return "", nil, err
	}
	if _, err := p.ensureBucket(ctx, client, raw); err != nil {
		return "", nil, err
	}
	// minio-go 的 PresignedPutObject 不签 Content-Type，浏览器带什么都能传上去；
	// 文件类型在完成时以存储记录为准
	signed, err := client.PresignedPutObject(ctx, raw.Bucket, objectKey, expires)
	if err != nil {
		return "", nil, err
	}
	return signed.String(), map[string]string{"Content-Type": contentType}, nil
}

func (p *minioProvider) StatObject(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*directObjectStat, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	info, err := client.StatObject(ctx, raw.Bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	return &directObjectStat{Size: info.Size, ETag: info.ETag, ContentType: info.ContentType}, nil
}

func (p *aliyunOSSProvider) PresignPut(_ context.Context, cfg *storagedomain.Config, objectKey, contentType string, expires time.Duration) (string, map[string]string, error) {
	bucket, _, err := p.bucket(cfg)
	if err != nil {
		return "", nil, err
	}
	signed, err := bucket.SignURL(objectKey, oss.HTTPPut, int64(expires.Seconds()), oss.ContentType(contentType))
	if err != nil {
		return "", nil, err
	}
	return signed, map[string]string{"Content-Type": contentType}, nil
}

func (p *aliyunOSSProvider) StatObject(_ context.Context, cfg *storagedomain.Config, objectKey string) (*directObjectStat, error) {
	bucket, _, err := p.bucket(cfg)
	if err != nil {
		return nil, err
	}
	header, err := bucket.GetObjectDetailedMeta(objectKey)
	if err != nil {
		return nil, err
	}
	var size int64
	_, _ = fmt.Sscan(header.Get("Content-Length"), &size)
	return &directObjectStat{Size: size, ETag: header.Get("ETag"), ContentType: header.Get("Content-Type")}, nil
}

func (p *tencentCOSProvider) PresignPut(ctx context.Context, cfg *storagedomain.Config, objectKey, contentType string, expires time.Duration) (string, map[string]string, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return "", nil, err
	}
	signed, err := client.Object.GetPresignedURL(ctx, http.MethodPut, objectKey, raw.SecretID, raw.SecretKey, expires, &cos.PresignedURLOptions{
		Header: &http.Header{"Content-Type": []string{contentType}},
	})
	if err != nil {
		return "", nil, err
	}
	return signed.String(), map[string]string{"Content-Type": contentType}, nil
}

func (p *tencentCOSProvider) StatObject(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*directObjectStat, error) {
	client, _, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.Object.Head(ctx, objectKey, nil)
	if err != nil {
		return nil, err
	}
	return &directObjectStat{Size: resp.ContentLength, ETag: resp.Header.Get("ETag"), ContentType: resp.Header.Get("Content-Type")}, nil
}
