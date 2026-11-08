package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aidomain "aegis/internal/domain/ai"

	"go.uber.org/zap"
)

// 三种协议的「列出模型」各走各的官方形状：路径、鉴权头、翻页、过滤与排序去重。
// 平台作用域的草稿不碰数据库，所以这里不需要 Postgres。
func TestFetchModelsByProtocol(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery+" auth="+r.Header.Get("Authorization")+
			" xkey="+r.Header.Get("x-api-key")+" goog="+r.Header.Get("x-goog-api-key")+" x-route="+r.Header.Get("X-Route"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/models" && r.Header.Get("anthropic-version") != "":
			if r.URL.Query().Get("after_id") == "" {
				_, _ = w.Write([]byte(`{"data":[{"id":"claude-b","display_name":"Claude B"}],"has_more":true,"last_id":"claude-b"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-a","display_name":"Claude A"}],"has_more":false}`))
		case r.URL.Path == "/v1/models":
			// 重复项与空 ID 要被去掉
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-b","owned_by":"x"},{"id":"gpt-a"},{"id":"gpt-a"},{"id":""}]}`))
		case r.URL.Path == "/compat/models":
			_, _ = w.Write([]byte(`[{"id":"bare-1"},{"name":"bare-2"}]`))
		case r.URL.Path == "/v1beta/models":
			if r.URL.Query().Get("pageToken") == "" {
				_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-pro","displayName":"Gemini Pro","supportedGenerationMethods":["generateContent"]},` +
					`{"name":"models/text-embedding","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"p2"}`))
				return
			}
			_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-flash","supportedGenerationMethods":["generateContent","countTokens"]}]}`))
		case r.URL.Path == "/bad/models":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := NewAIProviderService(zap.NewNop(), nil, "test-master")
	fetch := func(draft aidomain.ConfigDraft) map[string]any {
		t.Helper()
		result, err := svc.FetchModels(context.Background(), aidomain.PlatformAppID, draft)
		if err != nil {
			t.Fatalf("FetchModels(%s) 返回错误：%v", draft.Provider, err)
		}
		return result
	}
	ids := func(result map[string]any) string {
		t.Helper()
		if result["ok"] != true {
			t.Fatalf("应拉取成功：%v", result)
		}
		models := result["models"].([]aidomain.ModelInfo)
		out := make([]string, 0, len(models))
		for _, model := range models {
			out = append(out, model.ID)
		}
		return strings.Join(out, ",")
	}

	// OpenAI 兼容：站点根地址自动补 /v1、Bearer 鉴权、附加请求头带上、去重排序
	got := ids(fetch(aidomain.ConfigDraft{Provider: aidomain.ProviderNewAPI,
		Settings: map[string]string{aidomain.KeyBaseURL: server.URL, aidomain.KeyExtraHeaders: `{"X-Route":"cn"}`},
		Secrets:  map[string]string{aidomain.KeyAPIKey: "sk-test"}}))
	if got != "gpt-a,gpt-b" {
		t.Fatalf("OpenAI 型号不对：%s", got)
	}
	if last := seen[len(seen)-1]; !strings.Contains(last, "auth=Bearer sk-test") || !strings.Contains(last, "x-route=cn") {
		t.Fatalf("OpenAI 请求头不对：%s", last)
	}

	// 自定义兼容端点按原样拼接、裸数组形状
	if got := ids(fetch(aidomain.ConfigDraft{Provider: aidomain.ProviderCustomOpenAI,
		Settings: map[string]string{aidomain.KeyBaseURL: server.URL + "/compat"}})); got != "bare-1,bare-2" {
		t.Fatalf("裸数组形状不对：%s", got)
	}

	// Anthropic：x-api-key、按 last_id 翻页
	if got := ids(fetch(aidomain.ConfigDraft{Provider: aidomain.ProviderCustomAnthropic,
		Settings: map[string]string{aidomain.KeyBaseURL: server.URL + "/v1"},
		Secrets:  map[string]string{aidomain.KeyAPIKey: "ak"}})); got != "claude-a,claude-b" {
		t.Fatalf("Anthropic 型号不对：%s", got)
	}
	if !strings.Contains(strings.Join(seen, "\n"), "after_id=claude-b") || !strings.Contains(strings.Join(seen, "\n"), "xkey=ak") {
		t.Fatalf("Anthropic 应带 x-api-key 并翻页：%v", seen)
	}

	// Gemini：x-goog-api-key、翻页、只留 generateContent、去掉 models/ 前缀
	if got := ids(fetch(aidomain.ConfigDraft{Provider: aidomain.ProviderGemini,
		Settings: map[string]string{aidomain.KeyBaseURL: server.URL + "/v1beta"},
		Secrets:  map[string]string{aidomain.KeyAPIKey: "gk"}})); got != "gemini-flash,gemini-pro" {
		t.Fatalf("Gemini 型号不对：%s", got)
	}

	// 上游失败：ok=false 带上游原话，不当作接口错误
	failed := fetch(aidomain.ConfigDraft{Provider: aidomain.ProviderCustomOpenAI,
		Settings: map[string]string{aidomain.KeyBaseURL: server.URL + "/bad"}})
	if failed["ok"] != false || !strings.Contains(failed["error"].(string), "invalid api key") || failed["status"] != http.StatusUnauthorized {
		t.Fatalf("上游 401 应以 ok=false 返回原话：%v", failed)
	}

	// 未知供应商是请求错误
	if _, err := svc.FetchModels(context.Background(), aidomain.PlatformAppID, aidomain.ConfigDraft{Provider: "nope"}); errCode(err) != 40511 {
		t.Fatalf("未知供应商应返回 40511，得到 %v", err)
	}
}
