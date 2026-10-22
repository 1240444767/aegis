package aigateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeJSONReplacesBase64AndSecrets(t *testing.T) {
	image := strings.Repeat("QUJD", 1000)
	body := `{"created":1,"data":[{"b64_json":"` + image + `","revised_prompt":"a cat"}],"api_key":"sk-live","url":"data:image/png;base64,` + image + `"}`
	out := SanitizeJSON([]byte(body), "application/json", SanitizeOptions{})
	encoded, _ := json.Marshal(out)
	text := string(encoded)
	if strings.Contains(text, image) || !strings.Contains(text, "[base64 · 2.9 KB]") || !strings.Contains(text, "[image/png · 2.9 KB]") {
		t.Fatalf("base64 应换成占位：%s", text)
	}
	if out["api_key"] != "[已移除]" || !strings.Contains(text, `"revised_prompt":"a cat"`) {
		t.Fatalf("凭据应去掉、短文本应原样：%s", text)
	}
}

func TestSanitizeValueTruncatesAndHides(t *testing.T) {
	long := strings.Repeat("字", traceStringLimit+5)
	vector := make([]any, 100)
	for i := range vector {
		vector[i] = float64(i)
	}
	value := map[string]any{"prompt": long, "size": "1024x1024", "embedding": vector}
	out := SanitizeValue(value, SanitizeOptions{}).(map[string]any)
	if got := out["prompt"].(string); !strings.HasSuffix(got, "…（共 1005 字）") || out["embedding"] != "[100 个数]" {
		t.Fatalf("长文本应截断、长向量应只记个数：%v", out)
	}
	hidden := SanitizeValue(map[string]any{"prompt": "hi", "size": "1024x1024",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}}}, SanitizeOptions{HideContent: true}).(map[string]any)
	if hidden["prompt"] != "[已隐藏 2 字]" || hidden["size"] != "1024x1024" {
		t.Fatalf("隐藏内容时提示词不留、参数原样：%v", hidden)
	}
	message := hidden["messages"].([]any)[0].(map[string]any)
	if message["content"] != "[已隐藏 5 字]" || message["role"] != "user" {
		t.Fatalf("消息正文应隐藏：%v", message)
	}
}

func TestSanitizeJSONNonJSONAndNUL(t *testing.T) {
	out := SanitizeJSON([]byte("plain\x00 text"), "text/plain", SanitizeOptions{})
	if out["text"] != "plain text" || out["bytes"] != 11 {
		t.Fatalf("纯文本应记内容（去掉 NUL）与大小：%v", out)
	}
	out = SanitizeJSON([]byte{0xff, 0xd8}, "audio/mpeg", SanitizeOptions{})
	if out["contentType"] != "audio/mpeg" || out["text"] != nil {
		t.Fatalf("二进制只记类型与大小：%v", out)
	}
}
