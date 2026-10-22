package httptransport

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/crashreport"
)

// 请求体在解析之前就被限住：超过上限回 413，不把整个请求体读进内存；上限以内的原样放回给 bind。
func TestLimitCrashReportBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	oversized := strings.Repeat("x", crashreport.MaxRequestBytes+1)
	c.Request = httptest.NewRequest(http.MethodPost, "/crash-reports", strings.NewReader(oversized))
	if limitCrashReportBody(c) {
		t.Fatal("超过上限的请求体应被拒绝")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge || !strings.Contains(recorder.Body.String(), "40000") {
		t.Fatalf("应回 413 + 40000，得到 %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	payload := []byte(`{"reportId":"rpt-00000001"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/crash-reports", bytes.NewReader(payload))
	if !limitCrashReportBody(c) {
		t.Fatalf("上限以内应放行，得到 %d %s", recorder.Code, recorder.Body.String())
	}
	restored, _ := io.ReadAll(c.Request.Body)
	if !bytes.Equal(restored, payload) {
		t.Fatalf("读出的请求体应原样放回，得到 %q", restored)
	}
}
