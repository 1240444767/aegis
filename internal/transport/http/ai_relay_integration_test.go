package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service"
)

// /api/ai/v1 的 HTTP 级集成测试：SDK 实际看到的响应形状、错误信封、SSE、请求编号头，
// 以及 multipart 重组（换模型名、文件原样）与转写 text 格式的转换。计费数值规则由
// service 包的 TestAIGatewayBillingIntegration 覆盖，这里只核对传输层。
//
// AEGIS_TEST_PG_DSN=postgres://…/aegis_aig_test go test ./internal/transport/http -run TestAIRelayHTTPIntegration
func TestAIRelayHTTPIntegration(t *testing.T) {
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetRelayTestDatabase(t, ctx, pool)

	pg := pgrepo.New(pool)
	log := zap.NewNop()
	ai := service.NewAIProviderService(log, pg, "relay-test-master-key")
	gw := service.NewAIGatewayService(log, pg, nil, ai)
	upstream := newRelayFakeUpstream(t)

	const appID = int64(32000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'relay-test')`, appID); err != nil {
		t.Fatal(err)
	}
	name, provider, enabled := "fake", aidomain.ProviderCustomOpenAI, true
	if _, err := ai.Save(ctx, aidomain.ConfigMutation{AppID: appID, Name: &name, Provider: &provider, Enabled: &enabled,
		Settings: map[string]string{aidomain.KeyBaseURL: upstream.server.URL + "/v1"},
		Secrets:  map[string]string{aidomain.KeyAPIKey: "upstream-secret"}}); err != nil {
		t.Fatal(err)
	}
	settings := aigateway.DefaultSettings()
	settings.Enabled = true
	settings.Sources = []string{aigateway.SourceIntegral}
	settings.RPM = 0
	if _, err := gw.SaveSettings(ctx, appID, settings, "test"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []aigateway.ModelInput{
		{Model: "chat", UpstreamModel: "up-chat", Kind: aigateway.KindChat, InputPrice: decimal.NewFromInt(1000), OutputPrice: decimal.NewFromInt(1000)},
		{Model: "edit", UpstreamModel: "up-edit", Kind: aigateway.KindImage, ImagePrice: decimal.NewFromInt(1)},
		{Model: "whisper", UpstreamModel: "up-whisper", Kind: aigateway.KindTranscription, MinutePrice: decimal.NewFromInt(2)},
	} {
		if _, err := gw.CreateModel(ctx, appID, input); err != nil {
			t.Fatal(err)
		}
	}
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, enabled, integral) VALUES ($1, 'relay-user', TRUE, 100) RETURNING id`,
		appID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	created, err := gw.CreateKey(ctx, &authdomain.Session{AppID: appID, UserID: userID}, aigateway.KeyInput{})
	if err != nil {
		t.Fatal(err)
	}
	secret := created.Secret

	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &Handler{aiGateway: gw}
	relay := router.Group("/api/ai/v1")
	relay.GET("/models", h.AIRelayModels)
	relay.POST("/chat/completions", h.AIRelayChatCompletions)
	relay.POST("/messages", h.AIRelayMessages)
	relay.POST("/images/edits", h.AIRelayImageEdits)
	relay.POST("/audio/transcriptions", h.AIRelayTranscriptions)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	do := func(method, path, contentType string, body io.Reader, headers map[string]string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, data
	}
	bearer := map[string]string{"Authorization": "Bearer " + secret}

	// 模型列表
	resp, body := do("GET", "/api/ai/v1/models", "", nil, bearer)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"id":"chat"`) || !strings.Contains(string(body), `"object":"list"`) {
		t.Fatalf("模型列表不对：%d %s", resp.StatusCode, body)
	}

	// 非流式对话：OpenAI 形状、对外模型名、请求编号头
	chatBody := `{"model":"chat","messages":[{"role":"user","content":"hi"}],"max_tokens":20}`
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(chatBody), bearer)
	var completion struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &completion); err != nil || resp.StatusCode != 200 || completion.Object != "chat.completion" ||
		completion.Model != "chat" || len(completion.Choices) != 1 || completion.Usage.PromptTokens != 10 {
		t.Fatalf("对话响应不对：%d %s", resp.StatusCode, body)
	}
	requestNo := resp.Header.Get("X-Aegis-Request-No")
	if !strings.HasPrefix(requestNo, "AIG") {
		t.Fatalf("应带请求编号头，得到 %q", requestNo)
	}

	// 流式：SSE chunk、用量块、[DONE]
	streamBody := `{"model":"chat","stream":true,"messages":[{"role":"user","content":"hi"}],"max_tokens":20}`
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(streamBody), bearer)
	text := string(body)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") ||
		!strings.Contains(text, `"content":"Hel"`) || !strings.Contains(text, `"prompt_tokens":10`) ||
		!strings.HasSuffix(strings.TrimSpace(text), "data: [DONE]") || !strings.Contains(text, `"model":"chat"`) {
		t.Fatalf("流式响应不对：%d %s", resp.StatusCode, text)
	}

	// Anthropic Messages：x-api-key 鉴权、Messages 形状
	messagesBody := `{"model":"chat","max_tokens":20,"messages":[{"role":"user","content":"hi"}]}`
	resp, body = do("POST", "/api/ai/v1/messages", "application/json", strings.NewReader(messagesBody), map[string]string{"x-api-key": secret})
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"type":"message"`) || !strings.Contains(string(body), `"input_tokens":10`) {
		t.Fatalf("Messages 响应不对：%d %s", resp.StatusCode, body)
	}

	// 错误信封：无效密钥 401、未知模型 404、余额不足 402（OpenAI 与 Anthropic 两种形状）
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(chatBody),
		map[string]string{"Authorization": "Bearer " + aigateway.KeyPrefix + strings.Repeat("Z", 43)})
	if resp.StatusCode != 401 || !strings.Contains(string(body), `"code":"invalid_api_key"`) {
		t.Fatalf("无效密钥应 401：%d %s", resp.StatusCode, body)
	}
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"nope","messages":[{"role":"user","content":"hi"}]}`), bearer)
	if resp.StatusCode != 404 || !strings.Contains(string(body), `"code":"model_not_found"`) {
		t.Fatalf("未知模型应 404：%d %s", resp.StatusCode, body)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 0 WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(chatBody), bearer)
	if resp.StatusCode != 402 || !strings.Contains(string(body), `"type":"insufficient_quota"`) || !strings.Contains(string(body), `"aegis_code":40201`) {
		t.Fatalf("余额不足应 402：%d %s", resp.StatusCode, body)
	}
	resp, body = do("POST", "/api/ai/v1/messages", "application/json", strings.NewReader(messagesBody), map[string]string{"x-api-key": secret})
	if resp.StatusCode != 402 || !strings.Contains(string(body), `"type":"billing_error"`) {
		t.Fatalf("Anthropic 形状的余额不足不对：%d %s", resp.StatusCode, body)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 100 WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}

	// 图像编辑（multipart）：上游收到换过的模型名与原样的文件
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	_ = writer.WriteField("model", "edit")
	_ = writer.WriteField("prompt", "make it green")
	part, _ := writer.CreateFormFile("image", "apple.png")
	_, _ = part.Write([]byte("PNGDATA"))
	_ = writer.Close()
	resp, body = do("POST", "/api/ai/v1/images/edits", writer.FormDataContentType(), &form, bearer)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"data"`) {
		t.Fatalf("图像编辑失败：%d %s", resp.StatusCode, body)
	}
	if got := upstream.field("model"); got != "up-edit" {
		t.Fatalf("上游应收到上游模型名，得到 %q", got)
	}
	if got := upstream.field("prompt"); got != "make it green" || upstream.file("image") != "PNGDATA" {
		t.Fatalf("表单字段与文件应原样转发：prompt=%q file=%q", got, upstream.file("image"))
	}

	// 转写：客户端要 text，上游收到 json（带时长），客户端拿到纯文本；按 90 秒计 3 点
	form.Reset()
	writer = multipart.NewWriter(&form)
	_ = writer.WriteField("model", "whisper")
	_ = writer.WriteField("response_format", "text")
	part, _ = writer.CreateFormFile("file", "a.mp3")
	_, _ = part.Write(bytes.Repeat([]byte{1}, 1000))
	_ = writer.Close()
	resp, body = do("POST", "/api/ai/v1/audio/transcriptions", writer.FormDataContentType(), &form, bearer)
	if resp.StatusCode != 200 || string(body) != "hello transcript" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("转写应回纯文本：%d %q %s", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	if got := upstream.field("response_format"); got != "json" {
		t.Fatalf("上游应收到 json 格式，得到 %q", got)
	}
	record, err := gw.GetRequest(ctx, appID, resp.Header.Get("X-Aegis-Request-No"))
	if err != nil || record.AudioMillis != 90_000 || record.CostMicro != 3_000_000 || record.UsageEstimated {
		t.Fatalf("转写应按上游报的 90 秒计 3 点：%+v %v", record, err)
	}
}

func resetRelayTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../../../migrations/postgres/*.up.sql")
	sort.Strings(files)
	for _, file := range files {
		content, _ := os.ReadFile(file)
		if sql := strings.TrimSpace(string(content)); sql != "" {
			if _, err := pool.Exec(ctx, sql); err != nil {
				msg := err.Error()
				if strings.Contains(msg, "vector") || strings.Contains(msg, "postgis") ||
					strings.Contains(msg, "geography") || strings.Contains(msg, "geometry") {
					continue
				}
				t.Fatalf("apply %s: %v", filepath.Base(file), err)
			}
		}
	}
}

type relayFakeUpstream struct {
	server *httptest.Server
	mu     sync.Mutex
	fields map[string]string
	files  map[string]string
}

func (f *relayFakeUpstream) field(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fields[key]
}

func (f *relayFakeUpstream) file(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[key]
}

func newRelayFakeUpstream(t *testing.T) *relayFakeUpstream {
	t.Helper()
	fake := &relayFakeUpstream{fields: map[string]string{}, files: map[string]string{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(8 << 20); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			fake.mu.Lock()
			for key, values := range r.MultipartForm.Value {
				fake.fields[key] = values[0]
			}
			for key, headers := range r.MultipartForm.File {
				file, _ := headers[0].Open()
				data, _ := io.ReadAll(file)
				_ = file.Close()
				fake.files[key] = string(data)
			}
			fake.mu.Unlock()
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/edits"):
			_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"AAAA"}]}`))
		case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
			_, _ = w.Write([]byte(`{"text":"hello transcript","usage":{"type":"duration","seconds":90}}`))
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			var req struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			usage := `"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`
			if !req.Stream {
				_, _ = fmt.Fprintf(w, `{"id":"c","object":"chat.completion","model":%q,"choices":[{"index":0,`+
					`"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],%s}`, req.Model, usage)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":%q,"+
				"\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"},\"finish_reason\":null}]}\n\n", req.Model)
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":%q,"+
				"\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", req.Model)
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[],%s}\n\n", req.Model, usage)
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}
