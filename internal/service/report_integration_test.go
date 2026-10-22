package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	reportdomain "aegis/internal/domain/report"
	pgrepo "aegis/internal/repository/postgres"
)

// 报表分析中心的端到端集成测试：真实 Postgres 跑全部迁移，按已知答案造一小批数据，
// 再把 14 个报表全部跑一遍 —— 每条具名 SQL 都会真正执行一次，口径用手算的数字核对。
//
// AEGIS_TEST_PG_DSN=postgres://aegis:***@127.0.0.1:15432/aegis_report_test go test ./internal/service -run TestReportIntegration
func TestReportIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewReportService(zap.NewNop(), pg)
	sh, _ := time.LoadLocation("Asia/Shanghai")
	at := func(month time.Month, day, hour, minute int) time.Time {
		return time.Date(2026, month, day, hour, minute, 0, 0, sh)
	}
	svc.now = func() time.Time { return at(10, 20, 12, 0) }

	const appID = int64(50000)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO apps (id, name) VALUES ($1, 'report-test'), ($1 + 1, 'other')`, appID)
	user := func(account string, created time.Time, province string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, enabled, created_at) VALUES ($1, $2, TRUE, $3) RETURNING id`,
			appID, account, created).Scan(&id); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO user_profiles (user_id, register_province) VALUES ($1, $2)`, id, province)
		return id
	}
	login := func(userID int64, when time.Time, loginType, provider, status string, metadata map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(metadata)
		exec(`INSERT INTO login_audit_logs (user_id, appid, login_type, provider, status, user_agent, login_ip, metadata, created_at)
VALUES ($1, $2, $3, NULLIF($4, ''), $5, 'okhttp/4.12', '1.2.3.4', $6, $7)`, userID, appID, loginType, provider, status, raw, when)
	}

	// 窗口：2026-10-01 ~ 10-07（上海），上期 09-24 ~ 09-30，「今天」是 10-20
	u1 := user("u1", at(10, 1, 10, 0), "广东")
	u2 := user("u2", at(10, 1, 23, 30), "北京") // UTC 是 15:30，按上海仍是 10-01
	u3 := user("u3", at(10, 3, 9, 0), "")
	u4 := user("u4", at(9, 28, 12, 0), "广东")
	u5 := user("u5", at(9, 1, 12, 0), "")
	// 别的应用的数据不能混进来
	var other int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, created_at) VALUES ($1, 'x', $2) RETURNING id`, appID+1, at(10, 2, 0, 0)).Scan(&other); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO login_audit_logs (user_id, appid, login_type, status, created_at) VALUES ($1, $2, 'password', 'success', $3)`, other, appID+1, at(10, 2, 0, 0))

	device := map[string]any{"device_platform": "android", "device_model": "23127PN0CC",
		"device_extra": map[string]any{"manufacturer": "Xiaomi", "osVersion": "14", "appVersion": "1.2.0"}}
	login(u1, at(10, 1, 10, 0), "register", "password", "success", nil)
	login(u2, at(10, 1, 23, 30), "register", "qq", "success", nil)
	login(u3, at(10, 3, 9, 0), "register", "sms", "success", nil)
	login(u4, at(9, 28, 12, 0), "register", "password", "success", nil)
	login(u1, at(10, 2, 8, 0), "password", "password", "success", device)
	login(u2, at(10, 2, 9, 0), "password", "password", "success", nil)
	login(u1, at(10, 3, 9, 0), "password", "", "failed", map[string]any{"reason": "invalid_password", "account": "u1"})
	login(u4, at(10, 5, 20, 0), "password", "password", "success", nil)
	login(u5, at(9, 30, 20, 0), "password", "password", "success", nil)
	login(u5, at(10, 6, 20, 0), "password", "password", "success", nil)
	login(u1, at(10, 8, 20, 0), "password", "password", "success", nil) // 窗口外，第 7 日留存
	exec(`INSERT INTO session_audit_logs (user_id, appid, token_jti, event_type, created_at) VALUES ($1, $2, 'j', 'user_my_accessed', $3)`, u3, appID, at(10, 4, 9, 0))
	exec(`INSERT INTO daily_signins (user_id, appid, signed_at, sign_date, consecutive_days, sign_in_source, integral_reward) VALUES ($1, $2, $3, '2026-10-02', 1, 'app', 5)`, u1, appID, at(10, 2, 8, 5))
	exec(`INSERT INTO login_geo_events (user_id, app_id, ip, country_code, country, region, city, isp, created_at)
VALUES ($1, $2, '1.2.3.4', 'CN', '中国', '广东', '深圳', '电信', $3)`, u1, appID, at(10, 2, 8, 0))

	var configID int64
	if err := pool.QueryRow(ctx, `INSERT INTO payment_configs (appid, payment_method, config_name) VALUES ($1, 'epay', 'test') RETURNING id`, appID).Scan(&configID); err != nil {
		t.Fatal(err)
	}
	order := func(userID int64, no string, amount string, paid time.Time, method string) {
		exec(`INSERT INTO payment_orders (appid, user_id, config_id, order_no, subject, amount, payment_method, provider_type, status, paid_at, created_at)
VALUES ($1, $2, $7, $3, '会员', $4::numeric, $5, 'epay', 'paid', $6, $6)`, appID, userID, no, amount, method, paid, configID)
	}
	order(u1, "o1", "12.50", at(10, 2, 8, 30), "alipay")
	order(u2, "o2", "30", at(10, 4, 10, 0), "wxpay")
	order(u4, "o3", "5", at(9, 29, 10, 0), "alipay")
	exec(`INSERT INTO payment_orders (appid, user_id, config_id, order_no, subject, amount, payment_method, provider_type, status, created_at) VALUES ($1, $2, $4, 'o4', '会员', 8, 'alipay', 'epay', 'pending', $3)`, appID, u3, at(10, 3, 9, 30), configID)
	var orderID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM payment_orders WHERE order_no = 'o2'`).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO payment_refunds (appid, order_id, order_no, refund_no, user_id, payment_method, amount, status, refunded_at, created_at)
VALUES ($1, $2, 'o2', 'r1', $3, 'wxpay', 10, 'success', $4, $4)`, appID, orderID, u2, at(10, 5, 10, 0))

	var noticeID int64
	if err := pool.QueryRow(ctx, `INSERT INTO notifications (appid, user_id, type, title, content, level, created_at) VALUES ($1, $2, 'system', 't', 'c', 'info', $3) RETURNING id`,
		appID, u1, at(10, 2, 10, 0)).Scan(&noticeID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO notification_receipts (notification_id, user_id, status, read_at) VALUES ($1, $2, 'read', $3)`, noticeID, u1, at(10, 2, 10, 30))
	exec(`INSERT INTO risk_assessments (scene, app_id, user_id, ip, total_score, risk_level, matched_rules, action, created_at, latency_ms)
VALUES ('login', $1, $2, '1.2.3.4', 60, 'high', '[{"ruleId":1,"ruleName":"异地登录","conditionType":"geo","score":60}]', 'block', $3, 12)`, appID, u1, at(10, 3, 9, 0))

	params := ReportParams{From: at(10, 1, 0, 0), To: at(10, 7, 0, 0), TimeZone: "Asia/Shanghai", Granularity: "day"}

	ov, err := svc.Overview(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	expectKPI(t, ov.KPIs, "newUsers", 3, 1)
	expectKPI(t, ov.KPIs, "totalUsers", 5, 2)
	expectKPI(t, ov.KPIs, "activeUsers", 5, 2)
	expectKPI(t, ov.KPIs, "revenue", 42.5, 5)
	expectKPI(t, ov.KPIs, "netRevenue", 32.5, 5)
	expectKPI(t, ov.KPIs, "payers", 2, 1)
	expectKPI(t, ov.KPIs, "d1Retention", 1, math.NaN()) // 上期 u4（09-28）次日没回来
	if got := ov.Trend.Series["dau"]; !floatsEqual(got, []float64{2, 2, 1, 1, 1, 1, 0}) {
		t.Fatalf("DAU = %v", got)
	}
	if ov.Lifecycle != (reportdomain.Lifecycle{New: 3, Retained: 2}) {
		t.Fatalf("生命周期 = %+v", ov.Lifecycle)
	}

	users, err := svc.Users(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	if got := users.Trend.Series["newUsers"]; !floatsEqual(got, []float64{2, 0, 1, 0, 0, 0, 0}) {
		t.Fatalf("日注册 = %v", got)
	}
	if got := users.Trend.Series["cumulative"]; got[6] != 5 || got[0] != 4 {
		t.Fatalf("累计用户 = %v", got)
	}
	expectKPI(t, users.KPIs, "activationRate", 1, 1) // 三人 7 天内都回来了；上期 u4 在第 7 天（10-05）回来
	if len(users.Sources) != 3 || users.Provinces[0].Key != "unknown" && users.Provinces[0].Key != "广东" {
		t.Fatalf("来源 %+v 省份 %+v", users.Sources, users.Provinces)
	}

	active, err := svc.Active(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	if got := active.Trend.Series["new"]; !floatsEqual(got, []float64{2, 0, 1, 0, 0, 0, 0}) {
		t.Fatalf("新增活跃 = %v", got)
	}
	if active.Signals[0].Count != 5 || active.Signals[1].Count != 1 || active.Signals[2].Count != 1 {
		t.Fatalf("信号 = %+v", active.Signals)
	}

	ret, err := svc.Retention(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	expectKPI(t, ret.KPIs, "d1", 1, 0)
	expectKPI(t, ret.KPIs, "d7", 1.0/3, 1)
	if r := ret.Rows[0]; r.Size != 2 || *r.Rates[0] != 1 || *r.Rates[6] != 0.5 {
		t.Fatalf("首个队列 = %+v", r)
	}
	if r := ret.Rows[6]; r.Size != 0 || r.Rates[0] != nil {
		t.Fatalf("空队列应保留且不给比率：%+v", r)
	}
	if ret.Average[8] != nil || ret.Rows[0].Rates[8] != nil {
		t.Fatalf("第 30 天要到 10-31 才可观察，今天是 10-20，应为空：%v", ret.Average[8])
	}
	weekly, err := svc.Retention(ctx, appID, ReportParams{From: params.From, To: params.To, TimeZone: "Asia/Shanghai", Unit: "week", Mode: "rolling"})
	if err != nil {
		t.Fatal(err)
	}
	if len(weekly.Rows) != 2 || weekly.Rows[0].Cohort != "2026-09-28" || weekly.Rows[0].Size != 3 {
		t.Fatalf("周队列 = %+v", weekly.Rows)
	}

	funnel, err := svc.Funnel(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	if got := []int64{funnel.Steps[0].Users, funnel.Steps[1].Users, funnel.Steps[2].Users, funnel.Steps[3].Users}; got[0] != 3 || got[1] != 3 || got[2] != 1 || got[3] != 1 {
		t.Fatalf("漏斗 = %+v", funnel.Steps)
	}
	expectKPI(t, funnel.KPIs, "payRate", 2.0/3, math.NaN())

	lg, err := svc.Login(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	expectKPI(t, lg.KPIs, "success", 7, 2)
	expectKPI(t, lg.KPIs, "failure", 1, 0)
	if len(lg.Failures) != 1 || lg.Failures[0].Key != "invalid_password" || lg.FailureIPs[0].Key != "1.2.3.4" {
		t.Fatalf("失败原因 %+v IP %+v", lg.Failures, lg.FailureIPs)
	}

	dev, err := svc.Device(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	if dev.Platforms[0].Key == "" || len(dev.AppVersions) == 0 {
		t.Fatalf("设备 = %+v", dev)
	}

	pay, err := svc.Payment(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	expectKPI(t, pay.KPIs, "net", 32.5, 5)
	expectKPI(t, pay.KPIs, "newPayers", 2, 1)
	expectKPI(t, pay.KPIs, "successRate", 2.0/3, 1)
	if pay.Amounts.P50 != 12.5 || pay.Methods[0].Key != "wxpay" || pay.Methods[0].Amount != 30 {
		t.Fatalf("金额 %+v 方式 %+v", pay.Amounts, pay.Methods)
	}

	region, err := svc.Region(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	if len(region.Countries) != 1 || region.Countries[0].Key != "CN" || region.Cities[0].Key != "深圳" {
		t.Fatalf("地区 = %+v", region)
	}

	note, err := svc.Notification(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	expectKPI(t, note.KPIs, "readRate", 1, math.NaN())
	if note.ReadLatency.P50 != 30 {
		t.Fatalf("阅读耗时 = %+v", note.ReadLatency)
	}

	risk, err := svc.Risk(ctx, appID, params)
	if err != nil {
		t.Fatal(err)
	}
	if risk.Rules[0].Key != "异地登录" || risk.Rules[0].Value != 60 || risk.Trend.Series["high"][2] != 1 {
		t.Fatalf("风控 = %+v", risk)
	}

	for name, run := range map[string]func() error{
		"channel":    func() error { _, err := svc.Channel(ctx, appID, params); return err },
		"activity":   func() error { _, err := svc.Activity(ctx, appID, params); return err },
		"engagement": func() error { _, err := svc.Engagement(ctx, appID, params); return err },
		"export":     func() error { _, err := svc.ExportTables(ctx, appID, "all", params); return err },
		"week":       func() error { _, err := svc.Overview(ctx, appID, ReportParams{From: params.From, To: params.To, Granularity: "week"}); return err },
		"month":      func() error { _, err := svc.Payment(ctx, appID, ReportParams{From: params.From, To: params.To, Granularity: "month"}); return err },
	} {
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	checkReportSQL(t, ctx, pool)
}

// checkReportSQL 每条具名查询在空窗口上也要能执行
func checkReportSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	pg := pgrepo.New(pool)
	from, to := time.Now().Add(-time.Hour), time.Now()
	for _, name := range []string{"users.sources", "users.provinces", "login.types", "login.providers", "login.failures", "login.failure_ips",
		"login.per_user", "region.countries", "region.provinces", "region.cities", "region.isps", "channel.invites", "channel.cardkeys",
		"payment.statuses", "payment.vip", "notification.types", "notification.levels", "risk.levels", "risk.actions", "risk.scenes",
		"risk.rules", "risk.ips", "firewall.severity", "firewall.rules", "firewall.countries", "firewall.paths", "activity.prizes",
		"activity.kinds", "activity.sources", "engagement.sign_sources", "engagement.streaks", "engagement.points_earned",
		"engagement.points_spent", "engagement.rewarded", "engagement.rewarded_scenes", "engagement.splash"} {
		if _, err := pg.ReportGroups(ctx, name, 1, from, to, "UTC"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func expectKPI(t *testing.T, kpis map[string]reportdomain.KPI, key string, value, previous float64) {
	t.Helper()
	k, ok := kpis[key]
	if !ok {
		t.Fatalf("缺少指标 %s", key)
	}
	if math.Abs(k.Value-value) > 1e-3 {
		t.Fatalf("%s = %v，期望 %v", key, k.Value, value)
	}
	if math.IsNaN(previous) {
		return
	}
	if k.Previous == nil || math.Abs(*k.Previous-previous) > 1e-3 {
		t.Fatalf("%s 上期 = %v，期望 %v", key, k.Previous, previous)
	}
}

func floatsEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-9 {
			return false
		}
	}
	return true
}
