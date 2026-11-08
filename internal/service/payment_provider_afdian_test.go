package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// 官方文档给出的签名示例：token=123、user_id=abc、params={"a":333}、ts=1624339905。
// 这条断言站在爱发电那一侧 —— 签名规则写错时本地怎么算都自洽，只有对照官方样例才看得出来。
func TestAfdianSignMatchesOfficialExample(t *testing.T) {
	if got := afdianSign("123", `{"a":333}`, 1624339905, "abc"); got != "a4acc28b81598b7e5d84ebdc3e91710c" {
		t.Fatalf("签名 = %s，与文档示例不一致", got)
	}
}

// fakeAfdian 模拟爱发电开放 API：按文档规则验签，query-order 支持按页与按单号查。
type fakeAfdian struct {
	t      *testing.T
	token  string
	userID string
	orders []map[string]any // 新单在前
	calls  atomic.Int32
	pages  []int
}

func (f *fakeAfdian) handler(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	body, _ := io.ReadAll(r.Body)
	var req struct {
		UserID string `json:"user_id"`
		Params string `json:"params"`
		TS     int64  `json:"ts"`
		Sign   string `json:"sign"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		f.t.Errorf("请求不是 JSON：%s", body)
		return
	}
	sum := md5.Sum([]byte(f.token + "params" + req.Params + "ts" + strconv.FormatInt(req.TS, 10) + "user_id" + req.UserID))
	if req.UserID != f.userID || req.Sign != hex.EncodeToString(sum[:]) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ec": 400005, "em": "sign validation failed"})
		return
	}
	var params map[string]any
	// 真实接口把空对象也判为非法 params
	if err := json.Unmarshal([]byte(req.Params), &params); err != nil || len(params) == 0 {
		_ = json.NewEncoder(w).Encode(map[string]any{"ec": 400003, "em": "params was not valid json string"})
		return
	}
	switch r.URL.Path {
	case "/api/open/ping":
		_ = json.NewEncoder(w).Encode(map[string]any{"ec": 200, "em": "pong", "data": map[string]any{"uid": f.userID}})
	case "/api/open/query-sponsor":
		_ = json.NewEncoder(w).Encode(map[string]any{"ec": 200, "data": map[string]any{"total_count": 7, "total_page": 1, "list": []any{}}})
	case "/api/open/query-order":
		var list []map[string]any
		totalPage := (len(f.orders) + afdianOrdersPage - 1) / afdianOrdersPage
		if no, ok := params["out_trade_no"].(string); ok {
			for _, o := range f.orders {
				for _, want := range strings.Split(no, ",") {
					if o["out_trade_no"] == want {
						list = append(list, o)
					}
				}
			}
		} else {
			page := int(params["page"].(float64))
			f.pages = append(f.pages, page)
			start := (page - 1) * afdianOrdersPage
			for i := start; i < len(f.orders) && i < start+afdianOrdersPage; i++ {
				list = append(list, f.orders[i])
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ec": 200, "data": map[string]any{
			"list": list, "total_count": len(f.orders), "total_page": totalPage,
		}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func afdianOrderFixture(outTradeNo, customOrderID, amount string) map[string]any {
	return map[string]any{
		"out_trade_no":    outTradeNo,
		"custom_order_id": customOrderID,
		"user_id":         "buyer01",
		"plan_id":         "",
		"month":           1,
		"total_amount":    amount,
		"show_amount":     amount,
		"status":          2,
		"remark":          "",
		"redeem_id":       "",
		"product_type":    0,
		"discount":        "0.00",
		"sku_detail":      []any{},
	}
}

func newFakeAfdian(t *testing.T, orders ...map[string]any) (*fakeAfdian, map[string]any) {
	fake := &fakeAfdian{t: t, token: "tok_secret", userID: "creator01", orders: orders}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)
	cfg := map[string]any{"userId": fake.userID, "token": fake.token, "siteUrl": server.URL}
	return fake, cfg
}

func afdianWebhook(order map[string]any) map[string]string {
	raw, _ := json.Marshal(map[string]any{"ec": 200, "em": "ok", "data": map[string]any{"type": "order", "order": order}})
	return map[string]string{CallbackRawBodyKey: string(raw)}
}

func TestAfdianCreateOrderCustomPrice(t *testing.T) {
	provider := newAfdianProvider(resty.New())
	payload, err := provider.CreateOrder(context.Background(),
		map[string]any{"userId": "creator01", "token": "t"},
		PaymentOrderRequest{OrderNo: "P1202610130000001", Subject: "会员", Amount: decimal.RequireFromString("29.9")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(payload.PaymentURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme+"://"+u.Host+u.Path != "https://afdian.com/order/create" {
		t.Errorf("下单地址 = %s", payload.PaymentURL)
	}
	q := u.Query()
	want := map[string]string{"user_id": "creator01", "custom_price": "29.90", "custom_order_id": "P1202610130000001", "month": "1"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q，期望 %q", k, q.Get(k), v)
		}
	}
	if q.Has("plan_id") {
		t.Error("自选金额模式不应带 plan_id")
	}
	if payload.RedirectURL != payload.PaymentURL {
		t.Error("跳转地址与支付地址应一致")
	}
}

func TestAfdianCreateOrderPlanMode(t *testing.T) {
	provider := newAfdianProvider(resty.New())
	cfg := map[string]any{
		"userId": "creator01", "token": "t", "orderMode": "plan", "siteUrl": "https://ifdian.net",
		"planMap": "# 月卡\n9.9=plan_month\n29.90 ＝ plan_year\n",
	}
	if err := provider.ValidateConfig(cfg); err != nil {
		t.Fatalf("合法的方案映射被拒：%v", err)
	}
	payload, err := provider.CreateOrder(context.Background(), cfg,
		PaymentOrderRequest{OrderNo: "P1", Amount: decimal.RequireFromString("29.9")})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(payload.PaymentURL)
	if u.Host != "ifdian.net" || u.Query().Get("plan_id") != "plan_year" || u.Query().Get("product_type") != "0" {
		t.Errorf("方案模式下单地址不对：%s", payload.PaymentURL)
	}
	if u.Query().Has("custom_price") {
		t.Error("方案模式不应带 custom_price")
	}

	_, err = provider.CreateOrder(context.Background(), cfg, PaymentOrderRequest{OrderNo: "P2", Amount: decimal.RequireFromString("1")})
	if appErr, ok := errors.AsType[*apperrors.AppError](err); !ok || appErr.Code != 40117 {
		t.Fatalf("没有映射的金额应返回 40117，实得 %v", err)
	}
}

func TestAfdianValidateConfig(t *testing.T) {
	provider := newAfdianProvider(resty.New())
	bad := []map[string]any{
		{"userId": "", "token": "t"},
		{"userId": "u", "token": "t", "orderMode": "plan"},
		{"userId": "u", "token": "t", "orderMode": "plan", "planMap": "abc=plan"},
		{"userId": "u", "token": "t", "orderMode": "plan", "planMap": "9.9"},
		{"userId": "u", "token": "t", "orderMode": "weird"},
		{"userId": "u", "token": "t", "siteUrl": "http://afdian.com"},
	}
	for i, cfg := range bad {
		if err := provider.ValidateConfig(cfg); err == nil {
			t.Errorf("第 %d 份非法配置被放行：%v", i, cfg)
		}
	}
	if err := provider.ValidateConfig(map[string]any{"userId": "u", "token": "t"}); err != nil {
		t.Errorf("最小配置被拒：%v", err)
	}
}

func TestAfdianWebhookIsVerifiedAgainstAPI(t *testing.T) {
	const orderNo = "P1202610130000001"
	real := afdianOrderFixture("202610131200001", orderNo, "29.90")
	_, cfg := newFakeAfdian(t, real)
	provider := newAfdianProvider(resty.New())
	ctx := context.Background()

	t.Run("真实推送通过并以接口数据为准", func(t *testing.T) {
		pushed := afdianOrderFixture("202610131200001", orderNo, "999.00") // 推送里的金额被改过
		data := afdianWebhook(pushed)
		if got := provider.ExtractOrderNo(data); got != orderNo {
			t.Fatalf("ExtractOrderNo = %q", got)
		}
		result, err := provider.HandleCallback(ctx, cfg, data, "")
		if err != nil {
			t.Fatal(err)
		}
		if !result.Paid || result.OrderNo != orderNo || result.ProviderOrderNo != "202610131200001" {
			t.Fatalf("结果不对：%+v", result)
		}
		if !result.Amount.Equal(decimal.RequireFromString("29.90")) {
			t.Fatalf("金额应取接口返回的 29.90，实得 %s", result.Amount)
		}
	})

	t.Run("伪造的爱发电单号查无此单", func(t *testing.T) {
		_, err := provider.HandleCallback(ctx, cfg, afdianWebhook(afdianOrderFixture("FORGED", orderNo, "29.90")), "")
		if err == nil {
			t.Fatal("伪造推送被放行")
		}
	})

	t.Run("custom_order_id 被改成别的订单", func(t *testing.T) {
		_, err := provider.HandleCallback(ctx, cfg, afdianWebhook(afdianOrderFixture("202610131200001", "P_OTHER", "29.90")), "")
		if err == nil {
			t.Fatal("篡改订单号的推送被放行")
		}
	})

	t.Run("Token 错误时回查失败", func(t *testing.T) {
		wrong := map[string]any{"userId": cfg["userId"], "token": "wrong", "siteUrl": cfg["siteUrl"]}
		_, err := provider.HandleCallback(ctx, wrong, afdianWebhook(real), "")
		appErr, ok := errors.AsType[*apperrors.AppError](err)
		if !ok || appErr.HTTPStatus < 500 {
			t.Fatalf("回查失败应是可重试的 5xx，实得 %v", err)
		}
	})
}

// 兑换码订单实付 0.00：带着 Paid=true 过网关，金额校验（只查正数）拦不住，就成了零元购。
func TestAfdianRedeemOrderIsNotPaid(t *testing.T) {
	order := afdianOrderFixture("R1", "P1", "0.00")
	order["redeem_id"] = "code1"
	_, cfg := newFakeAfdian(t, order)
	result, err := newAfdianProvider(resty.New()).HandleCallback(context.Background(), cfg, afdianWebhook(order), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Paid || result.TradeStatus != "REDEEMED" {
		t.Fatalf("兑换码订单不应视为已支付：%+v", result)
	}
}

// 后台「发送测试」与普通赞助都没有 custom_order_id，不能让网关把它当成本平台订单。
func TestAfdianTestPushHasNoOrderNo(t *testing.T) {
	sample := afdianOrderFixture("202106232138371083454010626", "", "5.00")
	delete(sample, "custom_order_id")
	if got := newAfdianProvider(resty.New()).ExtractOrderNo(afdianWebhook(sample)); got != "" {
		t.Fatalf("测试推送提取出了订单号 %q", got)
	}
}

func TestAfdianAckCallback(t *testing.T) {
	provider := newAfdianProvider(resty.New())
	cases := []struct {
		err    error
		wantEC int
	}{
		{nil, 200},
		{apperrors.New(40074, http.StatusBadRequest, "缺少订单号"), 200},
		{apperrors.New(40096, http.StatusBadRequest, "金额不一致"), 200},
		{apperrors.New(50084, http.StatusBadGateway, "回查失败"), 500},
		{errors.New("db down"), 500},
	}
	for _, c := range cases {
		var got struct {
			EC int    `json:"ec"`
			EM string `json:"em"`
		}
		if err := json.Unmarshal(provider.AckCallback(nil, c.err), &got); err != nil {
			t.Fatal(err)
		}
		if got.EC != c.wantEC {
			t.Errorf("err=%v：ec = %d，期望 %d", c.err, got.EC, c.wantEC)
		}
	}
}

func TestAfdianSettleRemoteOrdersPages(t *testing.T) {
	orders := make([]map[string]any, 0, 120)
	for i := range 120 {
		orders = append(orders, afdianOrderFixture("A"+strconv.Itoa(i), "X"+strconv.Itoa(i), "1.00"))
	}
	// 第 2 页上的目标订单，以及同一订单号的两笔：新的那笔是兑换码，旧的那笔真付了钱
	orders[60] = afdianOrderFixture("A60", "P_TARGET", "9.90")
	orders[3] = afdianOrderFixture("A3", "P_TWICE", "0.00")
	orders[70] = afdianOrderFixture("A70", "P_TWICE", "5.00")
	fake, cfg := newFakeAfdian(t, orders...)
	provider := newAfdianProvider(resty.New())

	results, err := provider.SettleRemoteOrders(context.Background(), cfg, []string{"P_TARGET", "P_TWICE", "P_MISSING"})
	if err != nil {
		t.Fatal(err)
	}
	if r := results["P_TARGET"]; r == nil || !r.Paid || r.ProviderOrderNo != "A60" {
		t.Fatalf("第 2 页的订单没找到：%+v", r)
	}
	if r := results["P_TWICE"]; r == nil || !r.Paid || r.ProviderOrderNo != "A70" {
		t.Fatalf("重复付款应取成交的那笔：%+v", r)
	}
	if _, ok := results["P_MISSING"]; ok {
		t.Fatal("不存在的订单出现在结果里")
	}
	if len(fake.pages) != 3 {
		t.Fatalf("找不全时应翻完全部 3 页，实际翻了 %v", fake.pages)
	}

	// 页数上限生效
	fake.pages = nil
	cfg["scanPages"] = 1
	results, _ = provider.SettleRemoteOrders(context.Background(), cfg, []string{"P_TARGET"})
	if _, ok := results["P_TARGET"]; ok || len(fake.pages) != 1 {
		t.Fatalf("scanPages=1 时只应翻第 1 页，实际 %v", fake.pages)
	}

	// QueryRemoteOrder 给出的结论与补单一致
	cfg["scanPages"] = 3
	remote, err := provider.QueryRemoteOrder(context.Background(), cfg, "P_TARGET")
	if err != nil {
		t.Fatal(err)
	}
	if remote["paid"] != true || remote["provider_order_no"] != "A60" {
		t.Fatalf("QueryRemoteOrder = %v", remote)
	}
}

func TestAfdianTestConnection(t *testing.T) {
	_, cfg := newFakeAfdian(t, afdianOrderFixture("A1", "P1", "1.00"))
	provider := newAfdianProvider(resty.New())
	result, err := provider.TestConnection(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result["api_accessible"] != true || result["order_count"] != 1 || result["sponsor_count"] != 7 {
		t.Fatalf("连通性结果不对：%v", result)
	}
	cfg["token"] = "wrong"
	result, err = provider.TestConnection(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result["api_accessible"] != false {
		t.Fatalf("错误 Token 被报告为可用：%v", result)
	}
}
