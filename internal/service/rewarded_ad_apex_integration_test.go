package service

import (
	"os"
	"testing"

	"aegis/internal/config"
	cardkeydomain "aegis/internal/domain/cardkey"
	rewardedad "aegis/internal/domain/rewardedad"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 激励广告平台从灰鲸换成 Apex：迁移 000103 改写旧配置，回调按 Apex 文档的参数与签名验收。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestRewardedAdApexIntegration
func TestRewardedAdApexIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	const appID = int64(20100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'apex-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}

	// 迁移前存下的灰鲸配置，跑 000103 后变成 apex
	if _, err := pool.Exec(ctx, `INSERT INTO app_rewarded_ad_configs (appid, provider) VALUES ($1, 'huijing')`, appID); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/postgres/000103_rewarded_ads_apex.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	view, err := rewarded.AdminConfig(ctx, appID, "apex-test")
	if err != nil {
		t.Fatal(err)
	}
	if view.Provider != rewardedad.ProviderApex || view.CallbackURL != "/api/apps/apex-test/ads/apex/callback" {
		t.Fatalf("migrated config should be apex with the apex callback, got %q %q", view.Provider, view.CallbackURL)
	}

	scene := rewardedad.Scene{
		Key: "points", Name: "看广告领积分", PlacementID: "4892299192457961", Enabled: true,
		Rewards: []rewardedad.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 10}},
	}
	save := func(provider string) error {
		_, err := rewarded.SaveConfig(ctx, rewardedad.SaveConfigInput{
			AppID: appID, Enabled: true, Provider: provider, ProviderAppID: "68758", SecurityKey: "apex-security-key",
			VerifyMode: rewardedad.VerifyServer, Scenes: []rewardedad.Scene{scene}, Operator: "admin",
		}, "apex-test")
		return err
	}
	if err := save("huijing"); errCode(err) != 40000 {
		t.Fatalf("huijing is no longer a provider, got %v", err)
	}
	if err := save(""); err != nil {
		t.Fatal(err)
	}

	user := insertTestUser(t, ctx, pool, appID, "apex-a", "0")
	status, err := rewarded.ClientStatus(ctx, appID, user)
	if err != nil {
		t.Fatal(err)
	}
	if status.Provider != rewardedad.ProviderApex || status.ProviderAppID != "68758" || status.UserID == "" {
		t.Fatalf("client status should describe the apex app, got %+v", status)
	}

	callback := func(trans, sign string) (bool, string) {
		return rewarded.AdCallback(ctx, AdCallbackInput{
			AppID: appID, UserID: status.UserID, TransID: trans, Sign: sign,
			PlacementID: "4892299192457961", Extra: `{"aegisScene":"points","bustransid":"x"}`,
		})
	}
	if valid, reason := callback("apex-trans-1", rewardedad.CallbackSign("other-key", "apex-trans-1")); valid || reason != "bad_sign" {
		t.Fatalf("a signature from another key must be refused, got %v %q", valid, reason)
	}
	if valid, reason := callback("apex-trans-1", rewardedad.CallbackSign("apex-security-key", "apex-trans-1")); !valid ||
		reason != rewardedad.StatusGranted {
		t.Fatalf("server mode should grant on a valid apex callback, got %v %q", valid, reason)
	}
	// 平台没收到响应会重试：同一个 transId 落在同一行，结论不变
	if valid, reason := callback("apex-trans-1", rewardedad.CallbackSign("apex-security-key", "apex-trans-1")); !valid ||
		reason != rewardedad.StatusGranted {
		t.Fatalf("a retried callback should keep its verdict, got %v %q", valid, reason)
	}
}
