package service

import (
	"context"
	"errors"
	"testing"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	"aegis/internal/domain/modelcatalog"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

const catalogModelsDevFixture = `{
 "openai": {"id": "openai", "models": {
  "gpt-5": {"id": "gpt-5", "name": "GPT-5", "description": "旗舰推理模型", "reasoning": true, "tool_call": true,
   "reasoning_options": [{"type": "effort", "values": ["minimal", "low", "medium", "high"]}],
   "modalities": {"input": ["text", "image"], "output": ["text"]},
   "limit": {"context": 400000, "output": 128000}, "cost": {"input": 1.25, "output": 10}}
 }},
 "deepseek": {"id": "deepseek", "models": {
  "deepseek-chat": {"id": "deepseek-chat", "name": "DeepSeek Chat", "tool_call": true,
   "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 131072, "output": 8192},
   "cost": {"input": 0.28, "output": 0.42}}
 }}
}`

func TestAIModelCatalogIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	log := zap.NewNop()
	ai := NewAIProviderService(log, pg, "ai-gateway-test-master-key")
	gw := NewAIGatewayService(log, pg, nil, ai)

	calls := 0
	failModelsDev := false
	gw.catalog.fetch = func(_ context.Context, source, _ string) ([]byte, error) {
		calls++
		switch source {
		case modelcatalog.SourceModelsDev:
			if failModelsDev {
				return nil, errors.New("boom")
			}
			return []byte(catalogModelsDevFixture), nil
		case modelcatalog.SourceOpenRouter:
			return nil, errors.New("openrouter down")
		default:
			return []byte(`{"glm-4.6": {"litellm_provider": "zai", "mode": "chat", "supports_function_calling": true}}`), nil
		}
	}

	// 还没拉过：检索报 40229，App 端查询全部未找到但不报错。
	if _, err := gw.SearchModelCatalog(ctx, "gpt", "", "", 10); errCode(err) != 40229 {
		t.Fatalf("empty catalog search should be 40229, got %v", err)
	}
	if items, err := gw.LookupModelCatalog(ctx, []string{"gpt-5"}, nil); err != nil || items[0].Found {
		t.Fatalf("lookup before refresh = %+v %v", items, err)
	}

	status, err := gw.RefreshModelCatalog(ctx, true)
	if err != nil || !status.Refreshed || status.Count != 3 {
		t.Fatalf("refresh = %+v %v", status, err)
	}
	if status.Sources[1].Error == "" || status.Sources[0].Count != 2 {
		t.Fatalf("source statuses = %+v", status.Sources)
	}
	// 冷却内再点：不去拉，返回现有快照。
	before := calls
	if again, err := gw.RefreshModelCatalog(ctx, true); err != nil || again.Refreshed || calls != before {
		t.Fatalf("cooldown refresh = %+v %v (calls %d→%d)", again, err, before, calls)
	}
	// 另一个实例：从库里读到同一份快照。
	other := NewAIGatewayService(log, pg, nil, ai)
	if found, err := other.SearchModelCatalog(ctx, "deepseek", "", "", 10); err != nil || len(found) != 1 {
		t.Fatalf("second instance search = %+v %v", found, err)
	}

	items, err := gw.LookupModelCatalog(ctx, []string{"gpt-5-2025-08-07", "glm-4.6", "nope"}, []string{"openai"})
	if err != nil || !items[0].Found || !items[0].Reasoning || items[0].ContextLength != 400000 || !items[1].Found || items[2].Found {
		t.Fatalf("lookup = %+v %v", items, err)
	}

	const appID = int64(31500)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'catalog-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := ai.Save(ctx, aidomain.ConfigMutation{AppID: appID, Name: ptrTo("openai"),
		Provider: ptrTo(aidomain.ProviderOpenAI), Enabled: ptrTo(true),
		Secrets: map[string]string{aidomain.KeyAPIKey: "k"}}); err != nil {
		t.Fatalf("建通道失败：%v", err)
	}
	chat, err := gw.CreateModel(ctx, appID, aigateway.ModelInput{Model: "gpt-5", Kind: aigateway.KindChat,
		InputPrice: decimal.NewFromInt(1), OutputPrice: decimal.NewFromInt(1)})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := gw.CreateModel(ctx, appID, aigateway.ModelInput{Model: "house-model", Kind: aigateway.KindChat, Free: true})
	if err != nil {
		t.Fatal(err)
	}

	// 预览：列出变化但不写。
	preview, err := gw.SyncModelsFromCatalog(ctx, appID, CatalogSyncInput{
		Fields: []string{CatalogFieldCapabilities, CatalogFieldInfo, CatalogFieldPrices}, PriceRate: decimal.NewFromInt(100), Preview: true})
	if err != nil || len(preview) != 2 {
		t.Fatalf("preview = %+v %v", preview, err)
	}
	if preview[0].Applied || len(preview[0].Changes) == 0 || preview[1].Skipped == "" {
		t.Fatalf("preview results = %+v", preview)
	}
	if current, _ := pg.GetAIGatewayModel(ctx, appID, chat.ID); current.Capabilities.Reasoning {
		t.Fatal("preview must not write")
	}

	// 写入：能力、显示名、说明、价格（1 美元 = 100 点）。
	results, err := gw.SyncModelsFromCatalog(ctx, appID, CatalogSyncInput{ModelIDs: []int64{chat.ID, unknown.ID},
		Fields: []string{CatalogFieldCapabilities, CatalogFieldInfo, CatalogFieldPrices}, PriceRate: decimal.NewFromInt(100)})
	if err != nil || !results[0].Applied {
		t.Fatalf("sync = %+v %v", results, err)
	}
	updated, _ := pg.GetAIGatewayModel(ctx, appID, chat.ID)
	caps := updated.Capabilities
	if !caps.Vision || !caps.Tools || !caps.Reasoning || caps.ContextLength != 400000 || caps.MaxOutput != 128000 ||
		caps.CatalogKey != "openai/gpt-5" || len(caps.ReasoningEfforts) != 4 {
		t.Fatalf("capabilities = %+v", caps)
	}
	if updated.DisplayName != "GPT-5" || updated.Description != "旗舰推理模型" {
		t.Fatalf("info = %q %q", updated.DisplayName, updated.Description)
	}
	if !updated.InputPrice.Equal(decimal.NewFromInt(125)) || !updated.OutputPrice.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("prices = %s %s", updated.InputPrice, updated.OutputPrice)
	}
	if updated.MaxOutputTokens != 0 {
		t.Fatalf("catalog max output must not become the gateway cap: %d", updated.MaxOutputTokens)
	}
	// 再同步一次没有变化。
	if again, _ := gw.SyncModelsFromCatalog(ctx, appID, CatalogSyncInput{ModelIDs: []int64{chat.ID},
		Fields: []string{CatalogFieldCapabilities, CatalogFieldInfo}}); len(again[0].Changes) != 0 {
		t.Fatalf("second sync should be a no-op: %+v", again[0].Changes)
	}
	// 手动指定条目：自家模型对上 deepseek-chat。
	manual, err := gw.SyncModelsFromCatalog(ctx, appID, CatalogSyncInput{ModelIDs: []int64{unknown.ID},
		Keys: map[int64]string{unknown.ID: "deepseek/deepseek-chat"}})
	if err != nil || !manual[0].Applied {
		t.Fatalf("manual key sync = %+v %v", manual, err)
	}
	if got, _ := pg.GetAIGatewayModel(ctx, appID, unknown.ID); got.Capabilities.ContextLength != 131072 || got.Capabilities.Reasoning {
		t.Fatalf("manual key capabilities = %+v", got.Capabilities)
	}
	if _, err := gw.SyncModelsFromCatalog(ctx, appID, CatalogSyncInput{Fields: []string{CatalogFieldPrices}}); errCode(err) != 40219 {
		t.Fatalf("price sync without rate should be 40219, got %v", err)
	}

	// 自动同步：只列出打开了开关且网关开着的应用。
	settings := aigateway.DefaultSettings()
	settings.Enabled, settings.CatalogAutoSync = true, true
	if _, err := gw.SaveSettings(ctx, appID, settings, "test"); err != nil {
		t.Fatal(err)
	}
	apps, err := pg.ListAIGatewayCatalogAutoSyncApps(ctx)
	if err != nil || len(apps) != 1 || apps[0] != appID {
		t.Fatalf("auto sync apps = %v %v", apps, err)
	}

	// models.dev 拉不到：整次失败，旧快照不动，租约放掉。
	failModelsDev = true
	if _, err := pool.Exec(ctx, `UPDATE ai_model_catalog SET fetched_at = fetched_at - interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RefreshModelCatalog(ctx, true); errCode(err) != 40228 {
		t.Fatalf("models.dev failure should be 40228, got %v", err)
	}
	if snapshot, _ := pg.GetAIModelCatalog(ctx); snapshot == nil || len(snapshot.Entries) != 3 {
		t.Fatal("failed refresh must keep the previous snapshot")
	}
	if claimed, _ := pg.ClaimAIModelCatalogRefresh(ctx, gw.now(), gw.now(), aiCatalogLease); !claimed {
		t.Fatal("failed refresh must release the lease")
	}
}
