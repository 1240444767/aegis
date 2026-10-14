package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func appIDContext(target, body, contentType string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	return c
}

// 鉴权按 extractAdminAppID 的结果判定，处理函数从请求体绑定：两处必须是同一个应用。
// 旧实现取到查询串里的 appid 就返回，「?appid=有权限的应用」+「请求体写别的应用」可以越权。
func TestExtractAdminAppIDRejectsConflictingSources(t *testing.T) {
	cases := []struct {
		name, target, body, contentType string
		want                            int64
		err                             error
	}{
		{"只有请求体", "/x", `{"appid": 7}`, "application/json", 7, nil},
		{"查询串与请求体一致", "/x?appid=7", `{"appid": 7}`, "application/json", 7, nil},
		{"字符串形式", "/x", `{"appId": "7"}`, "application/json", 7, nil},
		{"查询串与请求体不同", "/x?appid=7", `{"appid": 8}`, "application/json", 0, errAppIDConflict},
		{"appid 与 appId 不同", "/x", `{"appid": 7, "appId": 8}`, "application/json", 0, errAppIDConflict},
		{"表单与查询串不同", "/x?appid=7", `appid=8`, "application/x-www-form-urlencoded", 0, errAppIDConflict},
		{"大数精度", "/x", `{"appid": 9007199254740993, "appId": 9007199254740992}`, "application/json", 0, errAppIDConflict},
		{"非正整数", "/x", `{"appid": -1}`, "application/json", 0, errAppIDInvalid},
		{"缺失", "/x", `{"userId": 1}`, "application/json", 0, io.EOF},
	}
	for _, tc := range cases {
		c := appIDContext(tc.target, tc.body, tc.contentType)
		got, err := extractAdminAppID(c, nil)
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Errorf("%s：期望 %v，实际 %v %v", tc.name, tc.err, got, err)
			}
			continue
		}
		if err != nil || got == nil || *got != tc.want {
			t.Errorf("%s：期望 %d，实际 %v %v", tc.name, tc.want, got, err)
		}
		// 请求体必须还能被处理函数读到
		if rest, _ := io.ReadAll(c.Request.Body); tc.contentType == "application/json" && len(rest) == 0 {
			t.Errorf("%s：请求体被消耗", tc.name)
		}
	}
}
