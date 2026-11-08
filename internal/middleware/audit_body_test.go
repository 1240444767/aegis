package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 审计只截取前 8 KB 做摘要，处理函数必须仍能读到完整的请求体。
func TestCaptureAuditRequestBodyKeepsFullBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := []byte(`{"text":"` + string(bytes.Repeat([]byte("a"), 3*maxAuditBodyBytes)) + `"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/admin/x", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")

	summary, ok := captureAuditRequestBody(c).(map[string]any)
	if !ok || summary["truncated"] != true {
		t.Fatalf("summary = %#v, want truncated", summary)
	}
	got, err := io.ReadAll(c.Request.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("handler read %d bytes, want %d", len(got), len(payload))
	}
}
