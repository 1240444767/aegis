package service

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	storagedomain "aegis/internal/domain/storage"
)

// 熔断包装不能把直传能力藏起来；本地存储与网盘类没有直传能力，调用方要退回经服务器上传。
func TestAsDirectUploaderSeesThroughCircuitBreaker(t *testing.T) {
	cfg := &storagedomain.Config{ID: 1, Provider: storagedomain.ProviderS3}
	wrapped := wrapStorageProvider(cfg, newS3StorageProvider(nil))
	if _, ok := wrapped.(*circuitBreakeredStorageProvider); !ok {
		t.Fatal("S3 提供商应被熔断包装")
	}
	if _, ok := asDirectUploader(wrapped); !ok {
		t.Fatal("包装后的 S3 提供商应仍可直传")
	}
	for _, provider := range []storageProvider{newLocalStorageProvider(), newWebDAVProvider(), newAzureBlobProvider()} {
		if _, ok := asDirectUploader(provider); ok {
			t.Errorf("%s 不应声称支持直传", provider.Name())
		}
	}
}

// 预签名地址不发网络请求就能生成：检查它指向正确的桶与对象，并且签了 PUT。
func TestS3PresignPutURL(t *testing.T) {
	cfg := &storagedomain.Config{Provider: storagedomain.ProviderS3, ConfigData: map[string]any{
		"endpoint": "https://s3.example.com", "region": "us-east-1", "bucket": "releases",
		"access_key_id": "AK", "secret_access_key": "SK", "force_path_style": true,
	}}
	signed, headers, err := (&s3StorageProvider{}).PresignPut(context.Background(), cfg, "apps/1/a b.apk", "application/vnd.android.package-archive", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "s3.example.com" || !strings.HasPrefix(parsed.Path, "/releases/apps/1/a") {
		t.Fatalf("地址不符：%s", signed)
	}
	q := parsed.Query()
	if q.Get("X-Amz-Signature") == "" || q.Get("X-Amz-Expires") != "1800" {
		t.Fatalf("缺少签名或有效期不对：%s", signed)
	}
	// aws-sdk 的预签名只签 host，不签 Content-Type：浏览器带什么类型存储都收，
	// 完成时以存储 HEAD 回来的类型为准
	if !strings.Contains(q.Get("X-Amz-SignedHeaders"), "host") {
		t.Fatalf("签名头不符：%s", q.Get("X-Amz-SignedHeaders"))
	}
	if headers["Content-Type"] != "application/vnd.android.package-archive" {
		t.Fatalf("返回给浏览器的请求头不符：%v", headers)
	}
}
