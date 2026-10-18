package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// fakeCloudflare 只实现快速接入用到的那几个 Cloudflare API。
// 令牌是账户令牌：/user/tokens/verify 回 401，/accounts/{id}/tokens/verify 才认。
type fakeCloudflare struct {
	mu        sync.Mutex
	account   string
	buckets   []string
	cors      []map[string]any
	tokens    []map[string]any
	r2dev     bool
	userCalls int
}

const fakeSetupToken = "setup-token-value"

func (f *fakeCloudflare) handler(t *testing.T) http.Handler {
	ok := func(w http.ResponseWriter, result any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "messages": []any{}, "result": result})
	}
	fail := func(w http.ResponseWriter, status int, message string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []any{map[string]any{"code": 1000, "message": message}}, "messages": []any{}, "result": nil})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+fakeSetupToken {
			fail(w, http.StatusUnauthorized, "Invalid API Token")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/client/v4")
		acct := "/accounts/" + f.account
		switch {
		case r.Method == http.MethodGet && path == "/accounts":
			ok(w, []any{map[string]any{"id": f.account, "name": "Aegis Inc", "type": "standard"}})
		case path == "/user/tokens/verify":
			f.userCalls++
			fail(w, http.StatusUnauthorized, "Invalid API Token")
		case path == acct+"/tokens/verify":
			ok(w, map[string]any{"id": "setup-token-id", "status": "active"})
		case r.Method == http.MethodGet && path == acct+"/r2/buckets":
			if r.Header.Get("cf-r2-jurisdiction") != "" {
				ok(w, map[string]any{"buckets": []any{}})
				return
			}
			list := []any{}
			for _, name := range f.buckets {
				list = append(list, map[string]any{"name": name, "creation_date": "2026-10-18T00:00:00Z", "location": "APAC"})
			}
			ok(w, map[string]any{"buckets": list})
		case r.Method == http.MethodPost && path == acct+"/r2/buckets":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			name, _ := body["name"].(string)
			f.buckets = append(f.buckets, name)
			ok(w, map[string]any{"name": name, "creation_date": "2026-10-18T00:00:00Z", "location": "APAC"})
		case r.Method == http.MethodGet && path == "/zones":
			if r.URL.Query().Get("account.id") != f.account {
				t.Errorf("列域名应按账户过滤：%s", r.URL.RawQuery)
			}
			ok(w, []any{map[string]any{"id": "zone-1", "name": "example.com", "status": "active", "account": map[string]any{"id": f.account}}})
		case r.Method == http.MethodGet && path == acct+"/tokens/permission_groups":
			ok(w, []any{
				map[string]any{"id": "pg-r2-bucket-write", "name": cfPermR2BucketItemWrite},
				map[string]any{"id": "pg-analytics-read", "name": cfPermAccountAnalyticsRead},
			})
		case r.Method == http.MethodPost && path == acct+"/tokens":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.tokens = append(f.tokens, body)
			ok(w, map[string]any{"id": "bucket-token-id", "name": body["name"], "value": "bucket-token-value", "status": "active"})
		case strings.HasSuffix(path, "/domains/managed") && r.Method == http.MethodPut:
			f.r2dev = true
			ok(w, map[string]any{"bucketId": "b1", "domain": "pub-123.r2.dev", "enabled": true})
		case strings.HasSuffix(path, "/domains/managed"):
			ok(w, map[string]any{"bucketId": "b1", "domain": "pub-123.r2.dev", "enabled": f.r2dev})
		case strings.HasSuffix(path, "/domains/custom"):
			ok(w, map[string]any{"domains": []any{}})
		case strings.HasSuffix(path, "/cors") && r.Method == http.MethodGet:
			ok(w, map[string]any{"rules": f.cors})
		case strings.HasSuffix(path, "/cors") && r.Method == http.MethodPut:
			var body struct {
				Rules []map[string]any `json:"rules"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.cors = body.Rules
			ok(w, map[string]any{})
		default:
			t.Errorf("未模拟的 Cloudflare 请求：%s %s", r.Method, r.URL)
			fail(w, http.StatusNotFound, "not found")
		}
	})
}

// fakeR2S3 内存里的 S3 端点，只认用专用令牌派生出的访问密钥。
type fakeR2S3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	keyID   string
}

func (f *fakeR2S3) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	respond := func(status int, body string, header http.Header) (*http.Response, error) {
		if header == nil {
			header = http.Header{}
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
	if !strings.Contains(req.Header.Get("Authorization"), "Credential="+f.keyID+"/") {
		return respond(http.StatusForbidden, `<Error><Code>InvalidAccessKeyId</Code></Error>`, http.Header{"Content-Type": {"application/xml"}})
	}
	key := req.URL.Path
	switch req.Method {
	case http.MethodHead:
		return respond(http.StatusOK, "", nil)
	case http.MethodGet:
		if req.URL.Query().Get("list-type") == "2" {
			return respond(http.StatusOK, `<?xml version="1.0"?><ListBucketResult><KeyCount>0</KeyCount></ListBucketResult>`, http.Header{"Content-Type": {"application/xml"}})
		}
		data, ok := f.objects[key]
		if !ok {
			return respond(http.StatusNotFound, `<Error><Code>NoSuchKey</Code></Error>`, http.Header{"Content-Type": {"application/xml"}})
		}
		return respond(http.StatusOK, string(data), http.Header{"Content-Length": {strconv.Itoa(len(data))}})
	case http.MethodPut:
		data, _ := io.ReadAll(req.Body)
		f.objects[key] = data
		return respond(http.StatusOK, "", http.Header{"ETag": {`"etag"`}})
	case http.MethodDelete:
		delete(f.objects, key)
		return respond(http.StatusNoContent, "", nil)
	}
	return respond(http.StatusMethodNotAllowed, "", nil)
}

// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestCloudflareQuickSetupIntegration
func TestCloudflareQuickSetupIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	account := strings.Repeat("ab", 16)

	cf := &fakeCloudflare{account: account, buckets: []string{"existing"},
		cors: []map[string]any{{"id": "keep-me", "allowed": map[string]any{"methods": []any{"GET"}, "origins": []any{"https://other.test"}}}}}
	server := httptest.NewServer(cf.handler(t))
	defer server.Close()

	sum := sha256.Sum256([]byte("bucket-token-value"))
	s3 := &fakeR2S3{objects: map[string][]byte{}, keyID: "bucket-token-id"}
	storage := NewStorageService(zap.NewNop(), pg, nil, "test")
	storage.cloudflareBaseURL = server.URL + "/client/v4/"
	storage.cloudflareHTTP = server.Client()
	storage.r2.httpClient = &http.Client{Transport: s3}

	// 连接：用户令牌校验失败后按账户令牌校验。
	connection, err := storage.ConnectCloudflare(ctx, fakeSetupToken)
	if err != nil {
		t.Fatal(err)
	}
	if connection.TokenKind != "account" || connection.TokenID != "setup-token-id" || len(connection.Accounts) != 1 || cf.userCalls != 1 {
		t.Fatalf("连接结果不对：%+v", connection)
	}
	if _, err := storage.ConnectCloudflare(ctx, "wrong"); err == nil {
		t.Fatal("错误的令牌应被拒绝")
	}

	// 建桶 + 列资源。
	if _, err := storage.CreateCloudflareBucket(ctx, CloudflareBucketInput{
		CloudflareTokenInput: CloudflareTokenInput{Token: fakeSetupToken, AccountID: account}, Bucket: "Bad_Name",
	}); err == nil {
		t.Fatal("不合法的桶名应被拒绝")
	}
	if _, err := storage.CreateCloudflareBucket(ctx, CloudflareBucketInput{
		CloudflareTokenInput: CloudflareTokenInput{Token: fakeSetupToken, AccountID: account}, Bucket: "aegis-files",
	}); err != nil {
		t.Fatal(err)
	}
	resources, err := storage.CloudflareResources(ctx, CloudflareTokenInput{Token: fakeSetupToken, AccountID: account})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Buckets) != 2 || len(resources.Zones) != 1 {
		t.Fatalf("资源不对：%+v", resources)
	}

	// 一键配置：签发专用令牌、开 r2.dev、合并 CORS、保存、检测、加入存储池。
	if _, err := pool.Exec(ctx, `DELETE FROM storage_pools WHERE name = 'cf-pool'`); err != nil {
		t.Fatal(err)
	}
	poolSaved, err := storage.SavePool(ctx, 0, storagedomain.PoolInput{Name: "cf-pool", Strategy: storagedomain.PoolStrategyPriority, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM storage_pools WHERE id = $1`, poolSaved.ID) })
	result, err := storage.SetupCloudflareR2(ctx, CloudflareSetupInput{
		CloudflareTokenInput: CloudflareTokenInput{Token: fakeSetupToken, AccountID: account},
		Bucket:               "aegis-files", CredentialMode: CloudflareCredentialCreate, PublicAccess: CloudflarePublicR2Dev,
		CORSOrigins: []string{"https://console.example.com/", "not-a-url"}, ConfigName: "cf-r2",
		PoolID: poolSaved.ID, PoolMode: storagedomain.MemberModeActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range result.Steps {
		if step.Status == "failed" {
			t.Fatalf("步骤 %s 失败：%s（全部：%+v）", step.Name, step.Message, result.Steps)
		}
	}
	if result.Config == nil {
		t.Fatal("应保存存储源")
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM storage_configs WHERE id = $1`, result.Config.ID) })
	if result.Probe == nil || !result.Probe.OK {
		t.Fatalf("深度检测应通过：%+v", result.Probe)
	}

	saved, err := pg.GetStorageConfigByID(ctx, result.Config.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ConfigData["access_key_id"] != "bucket-token-id" || saved.ConfigData["secret_access_key"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("S3 凭据应由专用令牌派生：%+v", saved.ConfigData)
	}
	if saved.AccessMode != storagedomain.AccessPublic || saved.BaseURL != "https://pub-123.r2.dev" || saved.ProxyDownload {
		t.Fatalf("r2.dev 公开访问没生效：%+v", saved)
	}
	if _, leaked := result.Config.ConfigData["api_token"]; leaked {
		t.Fatal("返回结果不应带出令牌")
	}

	// 专用令牌：只能读写这个桶 + 账户分析读。
	if len(cf.tokens) != 1 {
		t.Fatalf("应签发一个令牌，实际 %d", len(cf.tokens))
	}
	policies, _ := json.Marshal(cf.tokens[0]["policies"])
	for _, want := range []string{
		"com.cloudflare.edge.r2.bucket." + account + "_default_aegis-files", "pg-r2-bucket-write",
		"com.cloudflare.api.account." + account, "pg-analytics-read",
	} {
		if !strings.Contains(string(policies), want) {
			t.Fatalf("令牌策略缺少 %s：%s", want, policies)
		}
	}

	// CORS：桶上原有规则保留，Aegis 的规则只带合法来源。
	rules, _ := json.Marshal(cf.cors)
	if !strings.Contains(string(rules), "keep-me") || !strings.Contains(string(rules), "aegis-direct-upload") ||
		!strings.Contains(string(rules), "https://console.example.com\"") || strings.Contains(string(rules), "not-a-url") {
		t.Fatalf("CORS 合并不对：%s", rules)
	}

	// 加入了存储池。
	updatedPool, err := storage.GetPool(ctx, poolSaved.ID)
	if err != nil || len(updatedPool.Members) != 1 || updatedPool.Members[0].ConfigID != result.Config.ID {
		t.Fatalf("应加入存储池：%+v %v", updatedPool, err)
	}

	// 资源列表标出已接入的桶。
	resources, _ = storage.CloudflareResources(ctx, CloudflareTokenInput{Token: fakeSetupToken, AccountID: account})
	for _, bucket := range resources.Buckets {
		if bucket.Name == "aegis-files" && bucket.ConfigName != "cf-r2" {
			t.Fatalf("已接入的桶应标出存储源：%+v", bucket)
		}
	}

	// 写入经存储池落到 R2（假 S3）。
	if _, err := pool.Exec(ctx, `UPDATE storage_pools SET is_default = FALSE WHERE id <> $1`, poolSaved.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := storage.UploadForApp(ctx, 0, storagedomain.UploadInput{
		ConfigName: "cf-pool", ObjectKey: "x/hello.txt", FileName: "hello.txt", ContentLength: 5,
		Content: strings.NewReader("hello"), UploaderType: "admin",
	})
	if err != nil || stored.ConfigID != result.Config.ID {
		t.Fatalf("经存储池写入应落到 R2：%+v %v", stored, err)
	}
	if string(s3.objects["/aegis-files/x/hello.txt"]) != "hello" {
		t.Fatalf("假 S3 里应有写入的对象：%v", s3.objects)
	}
}
