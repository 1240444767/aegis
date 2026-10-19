package service

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/chacha20poly1305"
)

const testOfficialCert = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"

func TestClientControlEngineOrderAndIdentity(t *testing.T) {
	now := time.Now()
	settings := defaultClientControlSettings(1)
	settings.Enabled = true
	settings.AllowedPackages = []string{"com.example.app"}
	settings.AllowedCerts = []string{testOfficialCert}
	rules := []appdomain.ClientControlRule{
		{ID: 1, Name: "停用旧版", Enabled: true, Priority: 10, Action: appdomain.ClientControlActionForceUpdate,
			Conditions: appdomain.ClientControlConditions{VersionCodeMax: 99}},
		{ID: 2, Name: "测试机放行", Enabled: true, Priority: 20, Action: appdomain.ClientControlActionAllow,
			Conditions: appdomain.ClientControlConditions{DeviceIDs: []string{"tester"}}},
	}
	base := appdomain.ClientReport{PackageName: "com.example.app", VersionCode: 50, CertSHA256: "AA:BB:" + testOfficialCert[4:], DeviceID: "d1"}

	if got := evaluateClientControl(settings, rules, base, now, false); got.Action != appdomain.ClientControlActionForceUpdate || got.RuleID != 1 {
		t.Fatalf("old version should be force_update by rule 1, got %+v", got)
	}
	tester := base
	tester.DeviceID = "tester"
	if got := evaluateClientControl(settings, rules, tester, now, false); got.Action != appdomain.ClientControlActionAllow || got.RuleID != 2 {
		t.Fatalf("higher priority allow rule should win, got %+v", got)
	}
	fresh := base
	fresh.VersionCode = 120
	if got := evaluateClientControl(settings, rules, fresh, now, false); got.Action != appdomain.ClientControlActionAllow || got.Reason != appdomain.ClientControlReasonDefault {
		t.Fatalf("official new version should pass, got %+v", got)
	}
	repacked := fresh
	repacked.CertSHA256 = strings.Repeat("0", 64)
	if got := evaluateClientControl(settings, rules, repacked, now, false); got.Action != appdomain.ClientControlActionBlock || got.Reason != appdomain.ClientControlReasonSignatureMismatch {
		t.Fatalf("foreign signature should be blocked, got %+v", got)
	}
	clone := fresh
	clone.PackageName = "com.example.clone"
	if got := evaluateClientControl(settings, rules, clone, now, false); got.Reason != appdomain.ClientControlReasonPackageNotAllowed {
		t.Fatalf("foreign package should be blocked, got %+v", got)
	}
	settings.Enabled = false
	if got := evaluateClientControl(settings, rules, repacked, now, false); got.Action != appdomain.ClientControlActionAllow || got.Reason != appdomain.ClientControlReasonDisabled {
		t.Fatalf("disabled control must allow, got %+v", got)
	}
}

func TestClientControlPartialNeverGuessesBlock(t *testing.T) {
	now := time.Now()
	settings := defaultClientControlSettings(1)
	settings.Enabled = true
	settings.AllowedCerts = []string{testOfficialCert}
	rules := []appdomain.ClientControlRule{
		// 从应用商店之外安装的旧版本停用：闸门看不到安装来源
		{ID: 1, Name: "侧载旧版", Enabled: true, Priority: 10, Action: appdomain.ClientControlActionBlock,
			Conditions: appdomain.ClientControlConditions{VersionCodeMax: 99, Installers: []string{"com.android.vending"}, InstallersNegate: true}},
		{ID: 2, Name: "停用 1.0", Enabled: true, Priority: 5, Action: appdomain.ClientControlActionBlock,
			Conditions: appdomain.ClientControlConditions{VersionNames: []string{"1.0"}}},
	}
	known := clientReportKnown{VersionCode: true, VersionName: true, Platform: true, Device: true}

	got := evaluateClientControlPartial(settings, rules, appdomain.ClientReport{PackageName: "p", VersionCode: 50, VersionName: "1.0"}, known, now)
	if got.Action != appdomain.ClientControlActionAllow || got.Reason != appdomain.ClientControlReasonUndetermined || got.RuleID != 1 {
		t.Fatalf("rule depending on unknown installer must yield undetermined, got %+v", got)
	}
	got = evaluateClientControlPartial(settings, rules, appdomain.ClientReport{PackageName: "p", VersionCode: 120, VersionName: "1.0"}, known, now)
	if got.Action != appdomain.ClientControlActionBlock || got.RuleID != 2 {
		t.Fatalf("rule 1 cannot match on known fields, rule 2 should block, got %+v", got)
	}
	got = evaluateClientControlPartial(settings, rules, appdomain.ClientReport{PackageName: "p", VersionCode: 120, VersionName: "2.0"}, known, now)
	if got.Action != appdomain.ClientControlActionAllow {
		t.Fatalf("unknown cert must skip the official signature check, got %+v", got)
	}
}

func TestClientControlRuleValidation(t *testing.T) {
	enabled := true
	rule, err := buildClientControlRule(appdomain.ClientControlRuleInput{
		Name: " 停用非官方签名 ", Enabled: &enabled, Action: "block",
		Conditions: appdomain.ClientControlConditions{
			Certs: []string{"AA:BB:" + strings.ToUpper(testOfficialCert[4:]), testOfficialCert}, CertsNegate: true,
			Platforms: []string{"Android", "android"}, InstallersNegate: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rule.Name != "停用非官方签名" || len(rule.Conditions.Certs) != 1 || rule.Conditions.Certs[0] != testOfficialCert {
		t.Fatalf("certs should be normalized and deduplicated: %+v", rule.Conditions)
	}
	if len(rule.Conditions.Platforms) != 1 || rule.Conditions.InstallersNegate {
		t.Fatalf("platforms lowercased, empty negation cleared: %+v", rule.Conditions)
	}
	bad := []appdomain.ClientControlRuleInput{
		{Name: "", Action: "block"},
		{Name: "x", Action: "delete"},
		{Name: "x", Action: "block", Conditions: appdomain.ClientControlConditions{Certs: []string{"abc"}}},
		{Name: "x", Action: "block", Conditions: appdomain.ClientControlConditions{VersionCodeMin: 10, VersionCodeMax: 5}},
		{Name: "x", Action: "warn", ActionURL: "javascript:alert(1)"},
	}
	for i, input := range bad {
		if _, err := buildClientControlRule(input); err == nil {
			t.Fatalf("case %d should be rejected", i)
		}
	}
}

// testSealClientControl 按文档从客户端一侧加密一次上报，返回请求体与请求密钥。
func testSealClientControl(t *testing.T, boxPublic []byte, appKey, keyID string, timestamp int64, report appdomain.ClientReport) (ClientControlSealedRequest, []byte) {
	t.Helper()
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := ecdh.X25519().NewPublicKey(boxPublic)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := ephemeral.ECDH(peer)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	_, _ = rand.Read(nonce)
	nonceB64 := base64.RawURLEncoding.EncodeToString(nonce)
	aad := clientControlRequestAAD(appKey, keyID, timestamp, nonceB64)
	key, err := deriveClientControlKey(shared, appKey, keyID, aad)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, _ := json.Marshal(report)
	aead, _ := chacha20poly1305.NewX(key)
	return ClientControlSealedRequest{
		KeyID: keyID, ClientKey: base64.RawURLEncoding.EncodeToString(ephemeral.PublicKey().Bytes()),
		Nonce: nonceB64, Timestamp: timestamp,
		Payload: base64.RawURLEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, aad)),
	}, key
}

// testOpenClientControl 客户端一侧解开响应并验签，返回裁决。
func testOpenClientControl(t *testing.T, signer ed25519.PublicKey, requestKey []byte, appKey string, req ClientControlSealedRequest, resp *ClientControlSealedResponse) appdomain.ClientVerdict {
	t.Helper()
	digest := sha256.Sum256(append(append([]byte(nil), requestKey...), []byte(clientControlResponseInfo)...))
	aead, _ := chacha20poly1305.NewX(digest[:])
	nonce, _ := base64.RawURLEncoding.DecodeString(resp.Nonce)
	sealed, _ := base64.RawURLEncoding.DecodeString(resp.Payload)
	plaintext, err := aead.Open(nil, nonce, sealed, clientControlResponseAAD(appKey, req.KeyID, req.Nonce, resp.Nonce))
	if err != nil {
		t.Fatalf("open response: %v", err)
	}
	var envelope ClientControlSignedVerdict
	if err := json.Unmarshal(plaintext, &envelope); err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(envelope.Verdict)
	signature, _ := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if !VerifyClientVerdict(signer, raw, signature) {
		t.Fatal("verdict signature does not verify")
	}
	var verdict appdomain.ClientVerdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Challenge != req.Nonce || verdict.AppKey != appKey {
		t.Fatalf("verdict not bound to request: %+v", verdict)
	}
	return verdict
}

func testAppErrorCode(err error) int {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return 0
}

// 版本管控端到端：真实 Postgres，从 /config 规格验签开始，走加密上报、规则判定、
// 防重放、密钥轮换（背书链与旧 box 密钥宽限）、长轮询与网关闸门。
//
//	AEGIS_TEST_PG_DSN=postgres://…/空库 go test ./internal/service -run TestClientControlIntegration
//
// 另设 AEGIS_TEST_REDIS_ADDR 时走 Redis（nonce、策略版本与跨实例的发布订阅），用第 15 号库。
func TestClientControlIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	var redisClient *redis.Client
	if addr := os.Getenv("AEGIS_TEST_REDIS_ADDR"); addr != "" {
		redisClient = redis.NewClient(&redis.Options{Addr: addr, DB: 15})
		t.Cleanup(func() { _ = redisClient.Close() })
		if err := redisClient.FlushDB(ctx).Err(); err != nil {
			t.Fatal(err)
		}
	}
	const appID = int64(20109)
	const appKey = "cc_test_app"
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name, app_key) VALUES ($1, 'client-control-test', $2) ON CONFLICT DO NOTHING`, appID, appKey); err != nil {
		t.Fatal(err)
	}
	pg := pgrepo.New(pool)
	svc := NewClientControlService(nil, pg, redisClient, config.Config{Security: config.SecurityConfig{MasterKey: "test-master"}})
	app, err := pg.GetAppByID(ctx, appID)
	if err != nil || app == nil || app.AppKey != appKey {
		t.Fatalf("load app: %v %+v", err, app)
	}

	// 规格：box 公钥由根签名密钥签过
	spec, err := svc.Spec(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.SigningKeys) != 1 || spec.SigningKeys[0].EndorsedBy != "" || spec.Enabled {
		t.Fatalf("unexpected spec %+v", spec)
	}
	rootPublic, _ := base64.RawURLEncoding.DecodeString(spec.SigningKeys[0].PublicKey)
	boxSig, _ := base64.RawURLEncoding.DecodeString(spec.BoxKey.Signature)
	if !ed25519.Verify(rootPublic, clientControlBoxMessage(appKey, spec.BoxKey.KeyID, spec.BoxKey.PublicKey), boxSig) {
		t.Fatal("box key signature invalid")
	}
	boxPublic, _ := base64.RawURLEncoding.DecodeString(spec.BoxKey.PublicKey)
	report := appdomain.ClientReport{PackageName: "com.example.app", VersionName: "1.0", VersionCode: 50,
		Platform: "android", CertSHA256: testOfficialCert, DeviceID: "device-1", OSVersion: 34}
	check := func(r appdomain.ClientReport, keyID string, box []byte, signer ed25519.PublicKey) appdomain.ClientVerdict {
		t.Helper()
		req, key := testSealClientControl(t, box, appKey, keyID, time.Now().Unix(), r)
		resp, err := svc.Check(ctx, app, req, 0, "203.0.113.9")
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		return testOpenClientControl(t, signer, key, appKey, req, resp)
	}

	// 未启用：仍然签名放行
	if v := check(report, spec.BoxKey.KeyID, boxPublic, rootPublic); v.Action != "allow" || v.Reason != "disabled" || v.Subject.DeviceID != "device-1" {
		t.Fatalf("disabled verdict %+v", v)
	}

	// 启用 + 官方签名 + 停用旧版本
	enabled := true
	certs := []string{strings.ToUpper(testOfficialCert)}
	settings, err := svc.UpdateSettings(ctx, appID, appdomain.ClientControlSettingsPatch{Enabled: &enabled, AllowedCerts: &certs}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if settings.PolicyVersion < 2 || settings.AllowedCerts[0] != testOfficialCert {
		t.Fatalf("settings not saved: %+v", settings)
	}
	rule, err := svc.CreateRule(ctx, appID, appdomain.ClientControlRuleInput{Name: "停用 1.x", Action: "block",
		Conditions: appdomain.ClientControlConditions{VersionCodeMax: 99}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if v := check(report, spec.BoxKey.KeyID, boxPublic, rootPublic); v.Action != "block" || v.RuleID != rule.ID || v.Title == "" {
		t.Fatalf("old version should be blocked with default copy: %+v", v)
	}
	if v := check(appdomain.ClientReport{PackageName: "com.example.app", VersionCode: 200, CertSHA256: strings.Repeat("1", 64)},
		spec.BoxKey.KeyID, boxPublic, rootPublic); v.Reason != "signature_mismatch" {
		t.Fatalf("foreign signature should be blocked: %+v", v)
	}
	if v := check(appdomain.ClientReport{PackageName: "com.example.app", VersionCode: 200, CertSHA256: testOfficialCert},
		spec.BoxKey.KeyID, boxPublic, rootPublic); v.Action != "allow" || v.Subject.DeviceID != "" {
		t.Fatalf("new official version should pass, subject keeps empty device: %+v", v)
	}

	// 防重放与时间窗
	req, _ := testSealClientControl(t, boxPublic, appKey, spec.BoxKey.KeyID, time.Now().Unix(), report)
	if _, err := svc.Check(ctx, app, req, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Check(ctx, app, req, 0, ""); testAppErrorCode(err) != 40970 {
		t.Fatalf("replay should be 40970, got %v", err)
	}
	stale, _ := testSealClientControl(t, boxPublic, appKey, spec.BoxKey.KeyID, time.Now().Add(-11*time.Minute).Unix(), report)
	if _, err := svc.Check(ctx, app, stale, 0, ""); testAppErrorCode(err) != 40071 {
		t.Fatalf("stale timestamp should be 40071, got %v", err)
	}
	tampered, _ := testSealClientControl(t, boxPublic, appKey, spec.BoxKey.KeyID, time.Now().Unix(), report)
	tampered.Payload = tampered.Payload[:len(tampered.Payload)-2] + "AA"
	if _, err := svc.Check(ctx, app, tampered, 0, ""); testAppErrorCode(err) != 40000 {
		t.Fatalf("tampered payload should be 40000, got %v", err)
	}

	// 网关闸门：头里没有安装来源，依赖它的规则不拦
	decision, version, applies := svc.GateDecision(ctx, appID, appdomain.ClientReport{PackageName: "com.example.app", VersionCode: 50})
	if !applies || decision.Action != "block" || version != settings.PolicyVersion+1 {
		t.Fatalf("gate should block old version at policy %d: %+v %d", settings.PolicyVersion+1, decision, version)
	}

	// 轮换：新签名密钥由根密钥背书；旧 box 密钥在宽限期内仍可用
	newSign, err := svc.RotateKey(ctx, appID, "sign")
	if err != nil {
		t.Fatal(err)
	}
	if newSign.EndorsedBy != spec.SigningKeys[0].KeyID {
		t.Fatalf("new signing key must be endorsed by root: %+v", newSign)
	}
	newPublic, _ := base64.RawURLEncoding.DecodeString(newSign.PublicKey)
	endorsement, _ := base64.RawURLEncoding.DecodeString(newSign.Endorsement)
	if !ed25519.Verify(rootPublic, clientControlEndorseMessage(appKey, newSign.KeyID, newSign.PublicKey), endorsement) {
		t.Fatal("endorsement invalid")
	}
	if _, err := svc.RotateKey(ctx, appID, "box"); err != nil {
		t.Fatal(err)
	}
	if v := check(report, spec.BoxKey.KeyID, boxPublic, newPublic); v.Action != "block" {
		t.Fatalf("retired box key inside grace should still work, signed by new key: %+v", v)
	}
	if _, err := pool.Exec(ctx, `UPDATE app_client_control_keys SET retired_at = NOW() - INTERVAL '8 days' WHERE key_id = $1`, spec.BoxKey.KeyID); err != nil {
		t.Fatal(err)
	}
	svc.keyrings = map[int64]*clientControlKeyring{}
	expired, _ := testSealClientControl(t, boxPublic, appKey, spec.BoxKey.KeyID, time.Now().Unix(), report)
	if _, err := svc.Check(ctx, app, expired, 0, ""); testAppErrorCode(err) != 40457 {
		t.Fatalf("expired box key should be 40457, got %v", err)
	}
	spec2, err := svc.Spec(ctx, app)
	if err != nil || len(spec2.SigningKeys) != 2 || spec2.SigningKeyID != newSign.KeyID || spec2.BoxKey.KeyID == spec.BoxKey.KeyID {
		t.Fatalf("spec after rotation: %v %+v", err, spec2)
	}

	// 长轮询：版本相同挂起，规则变更立即唤醒
	current, _ := svc.policyVersion(ctx, appID)
	done := make(chan *appdomain.ClientControlWatchResult, 1)
	go func() {
		result, _ := svc.Watch(context.Background(), appID, current, 10*time.Second)
		done <- result
	}()
	time.Sleep(200 * time.Millisecond)
	if _, err := svc.SetRuleEnabled(ctx, appID, rule.ID, false); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result == nil || !result.Changed || result.PolicyVersion != current+1 {
			t.Fatalf("watch should report change: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch was not woken up by the change")
	}
	if result, _ := svc.Watch(ctx, appID, current+1, 0); result.Changed {
		t.Fatalf("watch with current version must not report change: %+v", result)
	}

	// 统计（异步写入，稍等）
	time.Sleep(500 * time.Millisecond)
	devices, err := svc.Installs(ctx, appID, appdomain.ClientInstallQuery{})
	if err != nil {
		t.Fatal(err)
	}
	foundAnon := false
	for _, item := range devices.Items {
		if strings.HasPrefix(item.DeviceID, "anon-") {
			foundAnon = true
		}
	}
	if devices.Total < 2 || !foundAnon {
		t.Fatalf("installs not recorded (anonymous id expected): %+v", devices)
	}
	groups, err := svc.Versions(ctx, appID, 30)
	if err != nil || len(groups) == 0 || groups[0].Decision == nil {
		t.Fatalf("versions: %v %+v", err, groups)
	}
	overview, err := svc.Overview(ctx, appID)
	if err != nil || overview.Devices24h < 2 || len(overview.Daily) != clientControlDailyDays || overview.SigningKey == nil {
		t.Fatalf("overview: %v %+v", err, overview)
	}
	rules, _ := svc.Rules(ctx, appID)
	if rules[0].HitCount == 0 {
		t.Fatalf("rule hits not recorded: %+v", rules[0])
	}
	sim, err := svc.Simulate(ctx, appID, report)
	if err != nil || len(sim.Trace) == 0 {
		t.Fatalf("simulate: %v %+v", err, sim)
	}
	if err := svc.DeleteRule(ctx, appID, rule.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteRule(ctx, appID, rule.ID); testAppErrorCode(err) != 40456 {
		t.Fatalf("deleting twice should be 40456, got %v", err)
	}
}
