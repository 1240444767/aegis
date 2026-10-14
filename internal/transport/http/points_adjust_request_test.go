package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

func adjustContext(body string, contentType string, scopedApp int64, header map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/api/app/points/adjust-integral", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	c.Request = req
	if scopedApp > 0 {
		c.Set(middleware.AdminScopeAppIDKey, scopedApp)
	}
	return c
}

// 这条就是线上那次故障：控制台发 userId，旧 DTO 只认 user_id，于是每次都「缺少必要的请求参数」。
func TestDecodePointsAdjustAcceptsCamelCase(t *testing.T) {
	c := adjustContext(`{"userId": 42, "appid": 7, "amount": -30, "reason": "补偿"}`, "application/json", 7, nil)
	req, err := decodePointsAdjust(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if req.UserID != 42 || req.AppID != 7 || req.Amount != -30 || req.Reason != "补偿" {
		t.Fatalf("解析结果不符：%+v", req)
	}
	// 请求体可以再次读取（审计中间件要用）
	if c.Request.Body == nil {
		t.Fatal("请求体被消耗了")
	}
}

func TestDecodePointsAdjustSnakeCaseAndForm(t *testing.T) {
	c := adjustContext(`{"user_id": "42", "appid": "7", "amount": "15", "request_id": "req-0001-abcd"}`, "application/json", 7, nil)
	req, err := decodePointsAdjust(c, false)
	if err != nil || req.UserID != 42 || req.Amount != 15 || req.RequestID != "req-0001-abcd" {
		t.Fatalf("字符串形式的整数应可解析：%+v %v", req, err)
	}

	form := adjustContext("user_id=42&appid=7&amount=-5&reason=x", "application/x-www-form-urlencoded", 7, nil)
	req, err = decodePointsAdjust(form, false)
	if err != nil || req.UserID != 42 || req.Amount != -5 {
		t.Fatalf("表单应可解析：%+v %v", req, err)
	}
}

func TestDecodePointsAdjustRejectsUnsafeNumbers(t *testing.T) {
	cases := map[string]string{
		`{"userId": 42, "appid": 7, "amount": 1.5}`:                  "必须是整数",
		`{"userId": 42, "appid": 7, "amount": 1e3}`:                  "必须是整数",
		`{"userId": 42, "appid": 7, "amount": "abc"}`:                "必须是整数",
		`{"userId": 42, "appid": 7, "amount": 99999999999999999999}`: "超出允许范围",
		`{"userId": 42, "appid": 7, "amount": true}`:                 "必须是整数",
		`{"userId": 42, "appid": 7}`:                                 "缺少调整数量",
		`{"appid": 7, "amount": 1}`:                                  "缺少用户 ID",
		`{"userId": 42, "user_id": 43, "appid": 7, "amount": 1}`:     "取值不一致",
		`[1, 2]`: "JSON 对象",
		``:       "不能为空",
	}
	for body, want := range cases {
		_, err := decodePointsAdjust(adjustContext(body, "application/json", 7, nil), false)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s：期望错误含 %q，实际 %v", body, want, err)
		}
	}
}

// 处理函数以鉴权通过的应用为准：请求体里的 appid 与之不同直接拒绝。
func TestDecodePointsAdjustUsesScopedApp(t *testing.T) {
	_, err := decodePointsAdjust(adjustContext(`{"userId": 1, "appid": 8, "amount": 1}`, "application/json", 7, nil), false)
	if err == nil || !strings.Contains(err.Error(), "appid 不一致") {
		t.Fatalf("应拒绝与鉴权应用不一致的 appid：%v", err)
	}
	req, err := decodePointsAdjust(adjustContext(`{"userId": 1, "amount": 1}`, "application/json", 7, nil), false)
	if err != nil || req.AppID != 7 {
		t.Fatalf("请求体不带 appid 时取鉴权应用：%+v %v", req, err)
	}
}

func TestDecodePointsAdjustIdempotencyHeader(t *testing.T) {
	req, err := decodePointsAdjust(adjustContext(`{"userId": 1, "appid": 7, "amount": 1}`, "application/json", 7,
		map[string]string{"Idempotency-Key": "key-12345678"}), false)
	if err != nil || req.RequestID != "key-12345678" {
		t.Fatalf("应从 Idempotency-Key 取请求编号：%+v %v", req, err)
	}
	_, err = decodePointsAdjust(adjustContext(`{"userId": 1, "appid": 7, "amount": 1, "requestId": "body-12345678"}`, "application/json", 7,
		map[string]string{"Idempotency-Key": "key-12345678"}), false)
	if err == nil {
		t.Fatal("请求体与请求头的编号不一致应拒绝")
	}
}

func TestDecodePointsBatch(t *testing.T) {
	req, err := decodePointsAdjust(adjustContext(`{"userIds": [1, "2", 3], "appid": 7, "amount": 10, "operationType": "consume"}`, "application/json", 7, nil), true)
	if err != nil || len(req.UserIDs) != 3 || req.UserIDs[1] != 2 || req.OperationType != "consume" {
		t.Fatalf("批量请求解析不符：%+v %v", req, err)
	}
	if _, err := decodePointsAdjust(adjustContext(`{"user_ids": [1, 2.5], "appid": 7, "amount": 10}`, "application/json", 7, nil), true); err == nil {
		t.Fatal("用户 ID 列表里的小数应拒绝")
	}
	if _, err := decodePointsAdjust(adjustContext(`{"user_ids": [], "appid": 7, "amount": 10}`, "application/json", 7, nil), true); err == nil {
		t.Fatal("空列表应拒绝")
	}
}
