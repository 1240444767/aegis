package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	storagedomain "aegis/internal/domain/storage"
)

func TestBackoffDurationGrowsAndCaps(t *testing.T) {
	base, max := 10*time.Second, 15*time.Minute
	within := func(d, want time.Duration) bool { return d >= want*8/10 && d <= want*12/10 }
	if d := backoffDuration(base, max, 1); !within(d, base) {
		t.Fatalf("第 1 次失败应冷却约 %s，得到 %s", base, d)
	}
	if d := backoffDuration(base, max, 4); !within(d, 80*time.Second) {
		t.Fatalf("第 4 次失败应冷却约 80s，得到 %s", d)
	}
	if d := backoffDuration(base, max, 40); !within(d, max) {
		t.Fatalf("冷却应封顶在 %s，得到 %s", max, d)
	}
}

func poolFixture(strategy string, members ...storagedomain.PoolMember) (*storagedomain.Pool, map[int64]*storagedomain.Config) {
	pool := &storagedomain.Pool{ID: 1, Name: "p", Strategy: strategy, Enabled: true, MaxAttempts: 3, WarnPercent: 85, Members: members}
	configs := map[int64]*storagedomain.Config{}
	for _, m := range members {
		configs[m.ConfigID] = &storagedomain.Config{ID: m.ConfigID, ConfigName: "c" + string(rune('0'+m.ConfigID)), Enabled: true}
	}
	return pool, configs
}

func planIDs(plan []poolCandidate, eligibleOnly bool) []int64 {
	var ids []int64
	for _, c := range plan {
		if eligibleOnly && !c.step.Eligible {
			continue
		}
		ids = append(ids, c.member.ConfigID)
	}
	return ids
}

func TestPlanPriorityStandbyReadonlyCapacityBackoff(t *testing.T) {
	engine := newStoragePoolEngine()
	pool, configs := poolFixture(storagedomain.PoolStrategyPriority,
		storagedomain.PoolMember{ConfigID: 1, Priority: 20, Weight: 1, Mode: storagedomain.MemberModeActive},
		storagedomain.PoolMember{ConfigID: 2, Priority: 10, Weight: 1, Mode: storagedomain.MemberModeActive, CapacityBytes: 100},
		storagedomain.PoolMember{ConfigID: 3, Priority: 0, Weight: 1, Mode: storagedomain.MemberModeStandby},
		storagedomain.PoolMember{ConfigID: 4, Priority: 0, Weight: 1, Mode: storagedomain.MemberModeReadOnly},
	)
	if got := planIDs(engine.Plan(pool, configs, 10), true); !equalIDs(got, []int64{2, 1, 3}) {
		t.Fatalf("优先级小的先写，备用垫底，只读不写：得到 %v", got)
	}

	// 成员 2 写满：跳过，排到不可写那一组。
	engine.SetUsage(map[int64]storagedomain.MemberUsage{2: {UsedBytes: 95}}, nil)
	plan := engine.Plan(pool, configs, 10)
	if got := planIDs(plan, true); !equalIDs(got, []int64{1, 3}) {
		t.Fatalf("写满的成员应被跳过：得到 %v", got)
	}
	if reason := stepFor(plan, 2).Reason; reason != "容量已满" {
		t.Fatalf("跳过原因应是容量已满，得到 %q", reason)
	}

	// 成员 1 失败进入退避：只剩备用。
	engine.RecordFailure(1, errors.New("boom"), false)
	if got := planIDs(engine.Plan(pool, configs, 10), true); !equalIDs(got, []int64{3}) {
		t.Fatalf("退避中的成员不该被写：得到 %v", got)
	}
	// 冷却期满（半开）后重新可写；成功一次即清零。
	engine.now = func() time.Time { return time.Now().Add(time.Hour) }
	if got := planIDs(engine.Plan(pool, configs, 10), true); !equalIDs(got, []int64{1, 3}) {
		t.Fatalf("冷却期满后应放行：得到 %v", got)
	}
	if !engine.RecordSuccess(1, time.Millisecond, false) {
		t.Fatal("失败后的第一次成功应报告为恢复")
	}
	if h := engine.Health(1); h.ConsecutiveFailures != 0 || h.BackoffUntil != nil {
		t.Fatalf("成功后应清零：%+v", h)
	}

	// 停用的配置不参与。
	configs[3].Enabled = false
	if reason := stepFor(engine.Plan(pool, configs, 10), 3).Reason; reason != "存储配置已停用" {
		t.Fatalf("停用配置的原因不对：%q", reason)
	}
}

func TestPlanRoundRobinRotatesEligibleMembers(t *testing.T) {
	engine := newStoragePoolEngine()
	pool, configs := poolFixture(storagedomain.PoolStrategyRoundRobin,
		storagedomain.PoolMember{ConfigID: 1, Priority: 0, Weight: 1, Mode: storagedomain.MemberModeActive},
		storagedomain.PoolMember{ConfigID: 2, Priority: 1, Weight: 1, Mode: storagedomain.MemberModeActive},
		storagedomain.PoolMember{ConfigID: 3, Priority: 2, Weight: 1, Mode: storagedomain.MemberModeActive},
	)
	var firsts []int64
	for i := 0; i < 6; i++ {
		firsts = append(firsts, engine.Plan(pool, configs, 1)[0].member.ConfigID)
	}
	if !equalIDs(firsts, []int64{1, 2, 3, 1, 2, 3}) {
		t.Fatalf("轮询应依次轮到每个成员：%v", firsts)
	}
}

func TestPlanWeightedFollowsWeights(t *testing.T) {
	engine := newStoragePoolEngine()
	pool, configs := poolFixture(storagedomain.PoolStrategyWeighted,
		storagedomain.PoolMember{ConfigID: 1, Weight: 1, Mode: storagedomain.MemberModeActive},
		storagedomain.PoolMember{ConfigID: 2, Weight: 9, Mode: storagedomain.MemberModeActive},
	)
	counts := map[int64]int{}
	for i := 0; i < 4000; i++ {
		counts[engine.Plan(pool, configs, 1)[0].member.ConfigID]++
	}
	share := float64(counts[2]) / 4000
	if share < 0.85 || share > 0.95 {
		t.Fatalf("权重 9:1 时成员 2 应先被选中约 90%%，实际 %.2f", share)
	}
}

func TestPlanLeastUsedPrefersLowerRatio(t *testing.T) {
	engine := newStoragePoolEngine()
	pool, configs := poolFixture(storagedomain.PoolStrategyLeastUsed,
		storagedomain.PoolMember{ConfigID: 1, Weight: 1, Mode: storagedomain.MemberModeActive, CapacityBytes: 1000},
		storagedomain.PoolMember{ConfigID: 2, Weight: 1, Mode: storagedomain.MemberModeActive, CapacityBytes: 100},
	)
	engine.SetUsage(map[int64]storagedomain.MemberUsage{1: {UsedBytes: 500}, 2: {UsedBytes: 10}}, nil)
	if got := planIDs(engine.Plan(pool, configs, 1), true); !equalIDs(got, []int64{2, 1}) {
		t.Fatalf("占用比例低的先写（10%% < 50%%）：%v", got)
	}
}

func TestSetUsagePrefersLargerReportedValue(t *testing.T) {
	engine := newStoragePoolEngine()
	engine.SetUsage(map[int64]storagedomain.MemberUsage{1: {UsedBytes: 100, Source: "index"}},
		map[int64]storagedomain.MemberUsage{1: {UsedBytes: 300, Source: "cloudflare-analytics"}, 2: {UsedBytes: 7}})
	if u, _ := engine.Usage(1); u.UsedBytes != 300 || u.Source != "cloudflare-analytics" {
		t.Fatalf("应取提供商报告的更大值：%+v", u)
	}
	if u := engine.AddUsage(2, 3); u.UsedBytes != 10 || u.Objects != 1 {
		t.Fatalf("写入后应就地累加：%+v", u)
	}
}

func TestRewindableContentSeeksOrSpools(t *testing.T) {
	seekable, err := newRewindableContent(bytes.NewReader([]byte("hello")), 0)
	if err != nil || seekable.size != 5 {
		t.Fatalf("可 Seek 的内容应量出大小：%v %d", err, seekable.size)
	}
	stream, err := newRewindableContent(io.MultiReader(strings.NewReader("ab"), strings.NewReader("cd")), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.cleanup()
	for i := 0; i < 2; i++ {
		if err := stream.Rewind(); err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(stream.reader)
		if string(data) != "abcd" || stream.size != 4 {
			t.Fatalf("第 %d 次重读应得到全部内容：%q", i+1, data)
		}
	}
}

func TestNormalizeAndEnforceRuleSpecs(t *testing.T) {
	if _, err := NormalizeStorageRule(RuleTypeUploadLimit, map[string]any{"maxBytes": 0}); err == nil {
		t.Fatal("上限为 0 应被拒绝")
	}
	data, err := NormalizeStorageRule(RuleTypeFileType, map[string]any{"mode": "allow", "extensions": []any{"PNG", ".jpg"}, "mimeTypes": []any{"image/*"}})
	if err != nil {
		t.Fatal(err)
	}
	if exts := data["extensions"].([]string); exts[0] != ".png" || exts[1] != ".jpg" {
		t.Fatalf("扩展名应规范为小写带点：%v", exts)
	}
	spec := storageRuleSpec{Mode: "allow", Extensions: []string{".png"}, MimeTypes: []string{"image/*"}}
	if !matchFileType(spec, "a.PNG", "") || !matchFileType(spec, "x.bin", "image/webp") || matchFileType(spec, "a.exe", "application/x-msdownload") {
		t.Fatal("类型匹配不对")
	}
	if _, err := NormalizeStorageRule(RuleTypePathPattern, map[string]any{"mode": "deny", "patterns": []any{}}); err == nil {
		t.Fatal("没有模式的路径规则应被拒绝")
	}
	cases := map[string]map[string]bool{
		"avatars/**":  {"avatars/a.png": true, "avatars/x/y/z.png": true, "other/a.png": false},
		"**/*.exe":    {"a.exe": true, "x/y/a.exe": true, "a.exe.txt": false},
		"uploads/*.?": {"uploads/a.b": true, "uploads/x/a.b": false},
	}
	for pattern, keys := range cases {
		re, err := globToRegexp(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range keys {
			if re.MatchString(key) != want {
				t.Errorf("%s 匹配 %s 应为 %v", pattern, key, want)
			}
		}
	}
}

func TestCDNAccessAndSigning(t *testing.T) {
	cdn := &storagedomain.CDNConfig{
		IPWhitelist:      []string{"10.0.0.0/8", "203.0.113.7"},
		RefererWhitelist: []string{"*.example.com", "https://app.test/"},
		RefererBlacklist: []string{"bad.example.com"},
	}
	cases := []struct {
		ip, referer string
		ok          bool
	}{
		{"10.1.2.3", "", true},
		{"203.0.113.7", "https://www.example.com/page", true},
		{"203.0.113.7", "https://example.com/", true},
		{"203.0.113.7", "https://app.test/x", true},
		{"203.0.113.7", "https://bad.example.com/", false},
		{"203.0.113.7", "https://evil.test/", false},
		{"8.8.8.8", "", false},
	}
	for _, c := range cases {
		err := checkProxyAccess(cdn, ProxyRequest{ClientIP: c.ip, Referer: c.referer})
		if (err == nil) != c.ok {
			t.Errorf("ip=%s referer=%q 期望放行=%v，得到 %v", c.ip, c.referer, c.ok, err)
		}
	}
	link := signCDNTypeA(cdnObjectURL(&storagedomain.CDNConfig{CDNDomain: "cdn.example.com"}, "a b/c.png"), "secret", time.Unix(1700000000, 0))
	// md5("/a%20b/c.png-1700000000-0-0-secret")
	if !strings.HasPrefix(link, "https://cdn.example.com/a%20b/c.png?auth_key=1700000000-0-0-") {
		t.Fatalf("A 型鉴权地址不对：%s", link)
	}
}

func TestDecodeR2ConfigAndEndpoint(t *testing.T) {
	base := map[string]any{"account_id": strings.Repeat("g", 32), "bucket": "b", "access_key_id": "k", "secret_access_key": "s"}
	if _, err := decodeR2Config(base); err == nil {
		t.Fatal("非十六进制的账户 ID 应被拒绝")
	}
	base["account_id"] = strings.Repeat("a", 32)
	cfg, err := decodeR2Config(base)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jurisdiction != "default" || r2Endpoint(cfg) != "https://"+strings.Repeat("a", 32)+".r2.cloudflarestorage.com" {
		t.Fatalf("默认管辖区端点不对：%s", r2Endpoint(cfg))
	}
	base["jurisdiction"] = "EU"
	cfg, _ = decodeR2Config(base)
	if r2Endpoint(cfg) != "https://"+strings.Repeat("a", 32)+".eu.r2.cloudflarestorage.com" || r2AnalyticsBucketName(cfg) != "eu_b" {
		t.Fatalf("欧盟管辖区端点或分析桶名不对：%s %s", r2Endpoint(cfg), r2AnalyticsBucketName(cfg))
	}
	base["jurisdiction"] = "mars"
	if _, err := decodeR2Config(base); err == nil {
		t.Fatal("未知管辖区应被拒绝")
	}
}

// recordingTransport 记下 SDK 发出的请求，回一个最小的 S3 响应。
type recordingTransport struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	if req.Body != nil {
		data, _ := io.ReadAll(req.Body)
		body = string(data)
	}
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.bodies = append(r.bodies, body)
	r.mu.Unlock()
	header := http.Header{}
	header.Set("ETag", `"abc"`)
	header.Set("Content-Type", "text/plain")
	respBody := ""
	if req.Method == http.MethodGet {
		respBody = "hello r2"
		header.Set("Content-Length", "8")
	}
	return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(respBody)), Request: req}, nil
}

func TestR2ProviderSpeaksS3ToAccountEndpoint(t *testing.T) {
	transport := &recordingTransport{}
	provider := newR2Provider()
	provider.httpClient = &http.Client{Transport: transport}
	account := strings.Repeat("0f", 16)
	cfg := &storagedomain.Config{ID: 9, Provider: storagedomain.ProviderR2, ConfigData: map[string]any{
		"account_id": account, "bucket": "aegis", "access_key_id": "AK", "secret_access_key": "SK",
	}}
	ctx := context.Background()
	if _, err := provider.Upload(ctx, cfg, storagedomain.UploadInput{
		ObjectKey: "cloud/1/a.json", ContentType: "application/json", ContentLength: 5, Content: strings.NewReader("hello"),
	}); err != nil {
		t.Fatal(err)
	}
	reader, err := provider.Open(ctx, cfg, "cloud/1/a.json")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(reader.Body)
	if string(data) != "hello r2" {
		t.Fatalf("读回内容不对：%q", data)
	}
	put := transport.requests[0]
	if put.Method != http.MethodPut || put.URL.Host != account+".r2.cloudflarestorage.com" || put.URL.Path != "/aegis/cloud/1/a.json" {
		t.Fatalf("应以路径风格写到账户端点：%s %s", put.Method, put.URL)
	}
	auth := put.Header.Get("Authorization")
	if !strings.Contains(auth, "/auto/s3/aws4_request") || !strings.Contains(auth, "Credential=AK/") {
		t.Fatalf("签名应使用 auto 地区与访问密钥：%s", auth)
	}
	if put.Header.Get("X-Amz-Sdk-Checksum-Algorithm") != "" || put.Header.Get("X-Amz-Trailer") != "" {
		t.Fatalf("R2 不应附加校验和尾部：%v", put.Header)
	}
	if transport.bodies[0] != "hello" {
		t.Fatalf("上传的内容不对：%q", transport.bodies[0])
	}
	if _, err := provider.PublicURL(ctx, cfg, "a.json", 0); err == nil {
		t.Fatal("没有公开域名时不能给永久直链")
	}
	signed, err := provider.PublicURL(ctx, cfg, "a.json", time.Hour)
	if err != nil || !strings.Contains(signed, "X-Amz-Signature=") {
		t.Fatalf("没有公开域名时应给预签名地址：%s %v", signed, err)
	}
	cfg.BaseURL = "https://files.example.com"
	if link, _ := provider.PublicURL(ctx, cfg, "a b.json", 0); link != "https://files.example.com/a%20b.json" {
		t.Fatalf("有公开域名时应拼域名：%s", link)
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stepFor(plan []poolCandidate, id int64) storagedomain.PoolPlanStep {
	for _, c := range plan {
		if c.member.ConfigID == id {
			return c.step
		}
	}
	return storagedomain.PoolPlanStep{}
}

// R2 / S3 读不存在的键：回 404 NoSuchKey，要认成「对象不存在」，读取回退与云存储 41060 都靠它。
func TestS3NoSuchKeyIsRecognizedAsMissing(t *testing.T) {
	provider := newR2Provider()
	provider.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`
		return &http.Response{StatusCode: 404, Header: http.Header{"Content-Type": []string{"application/xml"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	cfg := &storagedomain.Config{ID: 1, Provider: storagedomain.ProviderR2, ConfigData: map[string]any{
		"account_id": strings.Repeat("ab", 16), "bucket": "b", "access_key_id": "k", "secret_access_key": "s",
	}}
	_, err := provider.Open(context.Background(), cfg, "missing.json")
	if err == nil || !isStorageObjectNotFound(err) {
		t.Fatalf("NoSuchKey 应认成对象不存在：%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// ctxBody 读的时候检查请求上下文：上下文被取消就读不出来（真实的 net/http 响应体就是这样）。
type ctxBody struct {
	ctx  context.Context
	data *strings.Reader
}

func (b *ctxBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.data.Read(p)
}

func (b *ctxBody) Close() error { return nil }

func TestCircuitBreakerOpenKeepsBodyAliveAndIgnoresMissing(t *testing.T) {
	provider := newR2Provider()
	var misses int
	provider.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/missing") {
			misses++
			return &http.Response{StatusCode: 404, Header: http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<Error><Code>NoSuchKey</Code></Error>`)), Request: req}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"5"}},
			Body: &ctxBody{ctx: req.Context(), data: strings.NewReader("hello")}, Request: req}, nil
	})}
	cfg := &storagedomain.Config{ID: 77, ConfigName: "r2-breaker", Provider: storagedomain.ProviderR2, ConfigData: map[string]any{
		"account_id": strings.Repeat("cd", 16), "bucket": "b", "access_key_id": "k", "secret_access_key": "s",
	}}
	wrapped := wrapStorageProvider(cfg, provider)
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		if _, err := wrapped.Open(ctx, cfg, "missing"); !isStorageObjectNotFound(err) {
			t.Fatalf("第 %d 次读不存在的对象应报不存在，得到 %v", i+1, err)
		}
	}
	if misses != 30 {
		t.Fatalf("对象不存在不该重试：请求了 %d 次", misses)
	}
	reader, err := wrapped.Open(ctx, cfg, "present")
	if err != nil {
		t.Fatalf("连续 404 之后熔断器不该打开：%v", err)
	}
	data, err := io.ReadAll(reader.Body)
	reader.Body.Close()
	if err != nil || string(data) != "hello" {
		t.Fatalf("Open 返回后响应体应仍可读：%q %v", data, err)
	}
}
