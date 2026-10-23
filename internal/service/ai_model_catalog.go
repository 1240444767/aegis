package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	"aegis/internal/domain/modelcatalog"
	"aegis/pkg/egress"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// ═══════════════════════════ 公开模型目录 ═══════════════════════════
//
// models.dev 为主、OpenRouter 与 LiteLLM 补缺的模型元数据（docs/ai-gateway.md「模型目录」）。
// 全平台一份快照存在 ai_model_catalog；各实例读它建内存索引，快照变了（fetched_at 前进）才重建。
// 刷新：管理员手动，或后台每天一次（抢到租约的实例做）。拿到新快照后，打开了自动更新的应用
// 把对话模型的能力声明同步一遍（不动价格）。

const (
	aiCatalogRefreshEvery = 24 * time.Hour
	aiCatalogCheckEvery   = time.Hour
	aiCatalogReloadEvery  = 5 * time.Minute
	aiCatalogLease        = 10 * time.Minute
	aiCatalogFetchTimeout = 90 * time.Second
	aiCatalogMaxBytes     = 64 << 20
	// aiCatalogManualCooldown 手动刷新的最短间隔：三个来源合计近 10 MB，不让人连点。
	aiCatalogManualCooldown = 2 * time.Minute
)

// 同步字段。
const (
	CatalogFieldCapabilities = "capabilities"
	CatalogFieldInfo         = "info"
	CatalogFieldPrices       = "prices"
)

type aiModelCatalog struct {
	mu        sync.RWMutex
	index     *modelcatalog.Index
	snapshot  *modelcatalog.Snapshot
	checkedAt time.Time
	// fetch 拉一个来源的原文；测试替换它。
	fetch func(ctx context.Context, source, url string) ([]byte, error)
}

func newAIModelCatalog() *aiModelCatalog {
	return &aiModelCatalog{fetch: fetchCatalogSource}
}

// fetchCatalogSource 经 egress 网关拉一个来源（境外地址按出网配置走线路）。
func fetchCatalogSource(ctx context.Context, source, url string) ([]byte, error) {
	client := egress.NewClient(egress.Profile{Name: "ai.catalog." + source, Timeout: aiCatalogFetchTimeout,
		DialTimeout: 15 * time.Second, ResponseHeaderTimeout: time.Minute})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "aegis-model-catalog/1")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, aiCatalogMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > aiCatalogMaxBytes {
		return nil, fmt.Errorf("响应超过 %d MB", aiCatalogMaxBytes>>20)
	}
	return body, nil
}

// CatalogStatus 目录概况（不含条目）。
type CatalogStatus struct {
	FetchedAt *time.Time                   `json:"fetchedAt"`
	Count     int                          `json:"count"`
	Sources   []modelcatalog.SourceStatus  `json:"sources"`
	Providers []modelcatalog.ProviderCount `json:"providers"`
	// Refreshing 这一次调用是否真的去拉了（手动刷新在冷却中、或别的实例正在拉时为 false）。
	Refreshed bool `json:"refreshed"`
}

// catalogIndex 当前的内存索引；快照在别的实例上更新过就重读。没有快照时返回 nil。
func (s *AIGatewayService) catalogIndex(ctx context.Context) (*modelcatalog.Index, *modelcatalog.Snapshot, error) {
	c := s.catalog
	c.mu.RLock()
	index, snapshot, checked := c.index, c.snapshot, c.checkedAt
	c.mu.RUnlock()
	if index != nil && s.now().Sub(checked) < aiCatalogReloadEvery {
		return index, snapshot, nil
	}
	fetchedAt, err := s.pg.AIModelCatalogFetchedAt(ctx)
	if err != nil {
		return index, snapshot, err
	}
	if snapshot != nil && !fetchedAt.After(snapshot.FetchedAt) {
		c.mu.Lock()
		c.checkedAt = s.now()
		c.mu.Unlock()
		return index, snapshot, nil
	}
	if fetchedAt.IsZero() {
		return nil, nil, nil
	}
	loaded, err := s.pg.GetAIModelCatalog(ctx)
	if err != nil || loaded == nil {
		return index, snapshot, err
	}
	built := modelcatalog.NewIndex(loaded.Entries)
	c.mu.Lock()
	c.index, c.snapshot, c.checkedAt = built, loaded, s.now()
	c.mu.Unlock()
	return built, loaded, nil
}

// ModelCatalogStatus 目录概况。
func (s *AIGatewayService) ModelCatalogStatus(ctx context.Context) (*CatalogStatus, error) {
	index, snapshot, err := s.catalogIndex(ctx)
	if err != nil {
		return nil, err
	}
	return catalogStatusOf(index, snapshot, false), nil
}

func catalogStatusOf(index *modelcatalog.Index, snapshot *modelcatalog.Snapshot, refreshed bool) *CatalogStatus {
	status := &CatalogStatus{Sources: []modelcatalog.SourceStatus{}, Providers: []modelcatalog.ProviderCount{}, Refreshed: refreshed}
	if snapshot != nil {
		fetchedAt := snapshot.FetchedAt
		status.FetchedAt = &fetchedAt
		status.Sources = snapshot.Sources
	}
	if index != nil {
		status.Count = index.Len()
		status.Providers = index.Providers()
	}
	return status
}

// RefreshModelCatalog 立即从各来源拉一次。manual 为 true 时只受冷却与租约限制；后台调用只在快照过期时拉。
func (s *AIGatewayService) RefreshModelCatalog(ctx context.Context, manual bool) (*CatalogStatus, error) {
	now := s.now()
	staleBefore := now.Add(-aiCatalogRefreshEvery)
	if manual {
		staleBefore = now.Add(-aiCatalogManualCooldown)
	}
	claimed, err := s.pg.ClaimAIModelCatalogRefresh(ctx, now, staleBefore, aiCatalogLease)
	if err != nil {
		return nil, err
	}
	if !claimed {
		index, snapshot, err := s.catalogIndex(ctx)
		if err != nil {
			return nil, err
		}
		return catalogStatusOf(index, snapshot, false), nil
	}
	snapshot, err := s.buildCatalogSnapshot(ctx)
	if err != nil {
		_ = s.pg.ReleaseAIModelCatalogRefresh(context.WithoutCancel(ctx))
		return nil, err
	}
	if err := s.pg.SaveAIModelCatalog(ctx, *snapshot); err != nil {
		_ = s.pg.ReleaseAIModelCatalogRefresh(context.WithoutCancel(ctx))
		return nil, err
	}
	index := modelcatalog.NewIndex(snapshot.Entries)
	s.catalog.mu.Lock()
	s.catalog.index, s.catalog.snapshot, s.catalog.checkedAt = index, snapshot, s.now()
	s.catalog.mu.Unlock()
	s.log.Info("模型目录已刷新", zap.Int("entries", len(snapshot.Entries)))
	return catalogStatusOf(index, snapshot, true), nil
}

// buildCatalogSnapshot 并发拉三个来源并合并。models.dev 拉不到时整次失败（它是能力表的主来源，
// 只靠补缺的来源建出来的目录会把能力判错）；其余来源失败只记在来源状态里。
func (s *AIGatewayService) buildCatalogSnapshot(ctx context.Context) (*modelcatalog.Snapshot, error) {
	type result struct {
		source string
		body   []byte
		err    error
		at     time.Time
	}
	results := make([]result, len(modelcatalog.Sources))
	var wait sync.WaitGroup
	for position, source := range modelcatalog.Sources {
		wait.Add(1)
		go func() {
			defer wait.Done()
			body, err := s.catalog.fetch(ctx, source, modelcatalog.SourceURLs[source])
			results[position] = result{source: source, body: body, err: err, at: s.now()}
		}()
	}
	wait.Wait()

	var inputs modelcatalog.Inputs
	statuses := make([]modelcatalog.SourceStatus, 0, len(results))
	for _, item := range results {
		status := modelcatalog.SourceStatus{Source: item.source, URL: modelcatalog.SourceURLs[item.source], FetchedAt: item.at}
		err := item.err
		if err == nil {
			switch item.source {
			case modelcatalog.SourceModelsDev:
				inputs.ModelsDev, err = modelcatalog.ParseModelsDev(item.body)
				status.Count = len(inputs.ModelsDev)
			case modelcatalog.SourceOpenRouter:
				inputs.OpenRouter, inputs.OpenRouterFill, err = modelcatalog.ParseOpenRouter(item.body)
				status.Count = len(inputs.OpenRouter)
			case modelcatalog.SourceLiteLLM:
				inputs.LiteLLM, err = modelcatalog.ParseLiteLLM(item.body)
				status.Count = len(inputs.LiteLLM)
			}
		}
		if err != nil {
			status.Error = err.Error()
			s.log.Warn("模型目录来源拉取失败", zap.String("source", item.source), zap.Error(err))
		}
		statuses = append(statuses, status)
	}
	if len(inputs.ModelsDev) == 0 {
		return nil, aiGatewayError(40228, http.StatusBadGateway, "拉取 models.dev 失败："+statuses[0].Error)
	}
	return &modelcatalog.Snapshot{Entries: modelcatalog.Merge(inputs), Sources: statuses, FetchedAt: s.now()}, nil
}

// SearchModelCatalog 关键词检索。
func (s *AIGatewayService) SearchModelCatalog(ctx context.Context, query, provider, kind string, limit int) ([]modelcatalog.Entry, error) {
	index, _, err := s.catalogIndex(ctx)
	if err != nil {
		return nil, err
	}
	if index == nil {
		return nil, aiGatewayError(40229, http.StatusConflict, "模型目录还没有拉取过，请先刷新")
	}
	return index.Search(query, strings.ToLower(strings.TrimSpace(provider)), kind, limit), nil
}

// MatchModelCatalog 按模型名找目录条目。channelProvider 是通道的供应商键（aidomain.Provider*），用作提示。
func (s *AIGatewayService) MatchModelCatalog(ctx context.Context, model, channelProvider string, limit int) ([]modelcatalog.Candidate, error) {
	index, _, err := s.catalogIndex(ctx)
	if err != nil {
		return nil, err
	}
	if index == nil {
		return nil, aiGatewayError(40229, http.StatusConflict, "模型目录还没有拉取过，请先刷新")
	}
	return index.Match(model, CatalogProvidersFor(channelProvider), limit), nil
}

// ChannelProviderFor 模型会走的通道是哪家供应商（匹配提示用）。
func (s *AIGatewayService) ChannelProviderFor(ctx context.Context, appID, configID int64, model string) string {
	return s.ai.ChannelProvider(ctx, appID, configID, model)
}

// CatalogLookupItem App 端批量查询的一项：只有能力与上限，不含价格。
type CatalogLookupItem struct {
	Model string `json:"model"`
	// Found 目录里有没有；Match 把握（exact / normalized / fuzzy）。
	Found            bool     `json:"found"`
	Match            string   `json:"match,omitempty"`
	Key              string   `json:"key,omitempty"`
	Name             string   `json:"name,omitempty"`
	Kind             string   `json:"kind,omitempty"`
	Vision           bool     `json:"vision"`
	Tools            bool     `json:"tools"`
	Reasoning        bool     `json:"reasoning"`
	ReasoningEfforts []string `json:"reasoningEfforts"`
	Documents        bool     `json:"documents"`
	Audio            bool     `json:"audio"`
	ContextLength    int      `json:"contextLength,omitempty"`
	MaxOutput        int      `json:"maxOutput,omitempty"`
	NoTemperature    bool     `json:"noTemperature,omitempty"`
}

// LookupModelCatalog 按名字批量查能力。目录还没拉过时返回全部未找到，不报错：App 照常按名字推断。
func (s *AIGatewayService) LookupModelCatalog(ctx context.Context, models, providers []string) ([]CatalogLookupItem, error) {
	index, _, err := s.catalogIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CatalogLookupItem, 0, len(models))
	for _, model := range models {
		item := CatalogLookupItem{Model: model, ReasoningEfforts: []string{}}
		if index != nil {
			if candidates := index.Match(model, providers, 1); len(candidates) > 0 && candidates[0].Match != modelcatalog.MatchFuzzy {
				entry := candidates[0].Entry
				item.Found, item.Match, item.Key, item.Name, item.Kind = true, candidates[0].Match, entry.Key, entry.Name, entry.Kind
				item.Vision, item.Tools, item.Reasoning = entry.Vision, entry.Tools, entry.Reasoning
				if entry.ReasoningEfforts != nil {
					item.ReasoningEfforts = entry.ReasoningEfforts
				}
				item.Documents = slices.Contains(entry.InputModalities, "pdf")
				item.Audio = slices.Contains(entry.InputModalities, "audio")
				item.ContextLength, item.MaxOutput, item.NoTemperature = entry.ContextLength, entry.MaxOutput, entry.NoTemperature
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// CatalogProvidersFor 通道的供应商 → 目录里对应的供应商名（按优先级）。
func CatalogProvidersFor(provider string) []string {
	switch provider {
	case aidomain.ProviderOpenAI:
		return []string{"openai"}
	case aidomain.ProviderAzureOpenAI:
		return []string{"azure", "openai"}
	case aidomain.ProviderAnthropic:
		return []string{"anthropic"}
	case aidomain.ProviderGemini:
		return []string{"google"}
	case aidomain.ProviderDeepSeek:
		return []string{"deepseek"}
	case aidomain.ProviderMoonshot:
		return []string{"moonshotai-cn", "moonshotai"}
	case aidomain.ProviderZhipu:
		return []string{"zhipuai"}
	case aidomain.ProviderQwen:
		return []string{"alibaba-cn", "alibaba"}
	case aidomain.ProviderDoubao:
		return []string{"volcengine"}
	case aidomain.ProviderOpenRouter:
		return []string{"openrouter"}
	case aidomain.ProviderGroq:
		return []string{"groq"}
	case aidomain.ProviderXAI:
		return []string{"xai"}
	case aidomain.ProviderSiliconFlow:
		return []string{"siliconflow-cn", "siliconflow"}
	case aidomain.ProviderOllama:
		return []string{"ollama-cloud"}
	}
	return nil
}

// ── 同步到网关模型 ──

// CatalogSyncInput 一次同步。
type CatalogSyncInput struct {
	// ModelIDs 要同步的模型，空表示全部对话模型。
	ModelIDs []int64 `json:"modelIds"`
	// Fields capabilities / info / prices，空表示 capabilities 与 info。
	Fields []string `json:"fields"`
	// PriceRate 1 美元折合多少点；同步价格时必填。
	PriceRate decimal.Decimal `json:"priceRate"`
	// Overwrite info 字段（显示名、说明）覆盖已有内容；否则只填空的。
	Overwrite bool `json:"overwrite"`
	// Preview 只算出变化，不写入。
	Preview bool `json:"preview"`
	// Keys 管理员手动指定的目录条目（模型 ID → 供应商/模型），覆盖自动匹配。
	Keys map[int64]string `json:"keys"`
	// AllowFuzzy 自动匹配只有「包含」级别的把握时也同步；默认跳过，留给管理员确认。
	AllowFuzzy bool `json:"allowFuzzy"`
}

// CatalogFieldChange 一个字段的变化。
type CatalogFieldChange struct {
	Field string `json:"field"`
	From  any    `json:"from"`
	To    any    `json:"to"`
}

// CatalogSyncResult 一个模型的同步结果。
type CatalogSyncResult struct {
	ModelID  int64                    `json:"modelId"`
	Model    string                   `json:"model"`
	Upstream string                   `json:"upstream"`
	Match    *modelcatalog.Candidate  `json:"match,omitempty"`
	Others   []modelcatalog.Candidate `json:"others,omitempty"`
	Changes  []CatalogFieldChange     `json:"changes"`
	Applied  bool                     `json:"applied"`
	Skipped  string                   `json:"skipped,omitempty"`
	Updated  *aigateway.Model         `json:"updated,omitempty"`
}

// SyncModelsFromCatalog 把目录数据同步到应用的网关模型。
func (s *AIGatewayService) SyncModelsFromCatalog(ctx context.Context, appID int64, in CatalogSyncInput) ([]CatalogSyncResult, error) {
	index, _, err := s.catalogIndex(ctx)
	if err != nil {
		return nil, err
	}
	if index == nil {
		return nil, aiGatewayError(40229, http.StatusConflict, "模型目录还没有拉取过，请先刷新")
	}
	fields := in.Fields
	if len(fields) == 0 {
		fields = []string{CatalogFieldCapabilities, CatalogFieldInfo}
	}
	for _, field := range fields {
		if field != CatalogFieldCapabilities && field != CatalogFieldInfo && field != CatalogFieldPrices {
			return nil, aiGatewayError(40219, http.StatusBadRequest, "不支持的同步字段："+field)
		}
	}
	syncPrices := slices.Contains(fields, CatalogFieldPrices)
	if syncPrices && in.PriceRate.Sign() <= 0 {
		return nil, aiGatewayError(40219, http.StatusBadRequest, "同步价格需要填写汇率（1 美元折合多少点）")
	}
	models, err := s.pg.ListAIGatewayModels(ctx, appID, false)
	if err != nil {
		return nil, err
	}
	results := make([]CatalogSyncResult, 0, len(models))
	for _, model := range models {
		if len(in.ModelIDs) > 0 && !slices.Contains(in.ModelIDs, model.ID) {
			continue
		}
		if len(in.ModelIDs) == 0 && model.Kind != aigateway.KindChat {
			continue
		}
		result := CatalogSyncResult{ModelID: model.ID, Model: model.Model, Upstream: model.Upstream(), Changes: []CatalogFieldChange{}}
		var picked *modelcatalog.Candidate
		if key := strings.TrimSpace(in.Keys[model.ID]); key != "" {
			entry, ok := index.Get(key)
			if !ok {
				result.Skipped = "指定的目录条目不存在：" + key
				results = append(results, result)
				continue
			}
			picked = &modelcatalog.Candidate{Entry: entry, Match: modelcatalog.MatchExact}
		} else {
			candidates := index.Match(model.Upstream(), CatalogProvidersFor(s.ai.ChannelProvider(ctx, appID, model.ConfigID, model.Upstream())), 4)
			if len(candidates) == 0 {
				result.Skipped = "目录里找不到这个模型"
				results = append(results, result)
				continue
			}
			picked = &candidates[0]
			result.Others = candidates[1:]
			if picked.Match == modelcatalog.MatchFuzzy && !in.AllowFuzzy {
				result.Match = picked
				result.Skipped = "只找到相近的条目，请确认后手动指定"
				results = append(results, result)
				continue
			}
		}
		result.Match = picked
		next, changes := applyCatalogEntry(model, picked.Entry, fields, in.PriceRate, in.Overwrite)
		result.Changes = changes
		if len(changes) == 0 || in.Preview {
			results = append(results, result)
			continue
		}
		updated, err := s.pg.UpdateAIGatewayModel(ctx, appID, model.ID, next)
		if err != nil {
			result.Skipped = "写入失败：" + mapAIGatewayError(err).Error()
		} else {
			result.Applied, result.Updated = true, updated
		}
		results = append(results, result)
	}
	return results, nil
}

// applyCatalogEntry 按目录条目改模型，返回改后的模型与变化清单。
func applyCatalogEntry(model aigateway.Model, entry modelcatalog.Entry, fields []string, rate decimal.Decimal, overwrite bool) (aigateway.Model, []CatalogFieldChange) {
	next := model
	var changes []CatalogFieldChange
	change := func(field string, from, to any) {
		changes = append(changes, CatalogFieldChange{Field: field, From: from, To: to})
	}
	if slices.Contains(fields, CatalogFieldCapabilities) && (model.Kind == aigateway.KindChat || model.Kind == "") {
		caps := model.Capabilities
		target := aigateway.Capabilities{
			Vision: entry.Vision, Tools: entry.Tools, Reasoning: entry.Reasoning,
			ReasoningEfforts: entry.ReasoningEfforts, ContextLength: entry.ContextLength,
			MaxOutput: entry.MaxOutput, CatalogKey: entry.Key,
		}
		if normalized, err := aigateway.NormalizeCapabilities(&target); err == nil {
			target = normalized
		}
		if caps.Vision != target.Vision {
			change("capabilities.vision", caps.Vision, target.Vision)
		}
		if caps.Tools != target.Tools {
			change("capabilities.tools", caps.Tools, target.Tools)
		}
		if caps.Reasoning != target.Reasoning {
			change("capabilities.reasoning", caps.Reasoning, target.Reasoning)
		}
		if !slices.Equal(caps.ReasoningEfforts, target.ReasoningEfforts) {
			change("capabilities.reasoningEfforts", caps.ReasoningEfforts, target.ReasoningEfforts)
		}
		if target.ContextLength > 0 && caps.ContextLength != target.ContextLength {
			change("capabilities.contextLength", caps.ContextLength, target.ContextLength)
		} else if target.ContextLength == 0 {
			target.ContextLength = caps.ContextLength
		}
		if target.MaxOutput > 0 && caps.MaxOutput != target.MaxOutput {
			change("capabilities.maxOutput", caps.MaxOutput, target.MaxOutput)
		} else if target.MaxOutput == 0 {
			target.MaxOutput = caps.MaxOutput
		}
		if caps.CatalogKey != target.CatalogKey {
			change("capabilities.catalogKey", caps.CatalogKey, target.CatalogKey)
		}
		next.Capabilities = target
	}
	if slices.Contains(fields, CatalogFieldInfo) {
		name := strings.TrimSpace(entry.Name)
		if name != "" && len([]rune(name)) <= 128 && (overwrite || model.DisplayName == "") && model.DisplayName != name {
			change("displayName", model.DisplayName, name)
			next.DisplayName = name
		}
		description := strings.TrimSpace(entry.Description)
		if runes := []rune(description); len(runes) > 500 {
			description = string(runes[:500])
		}
		if description != "" && (overwrite || model.Description == "") && model.Description != description {
			change("description", model.Description, description)
			next.Description = description
		}
	}
	if slices.Contains(fields, CatalogFieldPrices) && entry.Cost != nil && rate.Sign() > 0 {
		convert := func(usd float64) decimal.Decimal {
			return decimal.NewFromFloat(usd).Mul(rate).Round(6)
		}
		price := func(field string, current *decimal.Decimal, usd float64) {
			target := convert(usd)
			if !current.Equal(target) {
				change(field, current.String(), target.String())
				*current = target
			}
		}
		switch model.Kind {
		case aigateway.KindChat:
			price("inputPrice", &next.InputPrice, entry.Cost.Input)
			price("outputPrice", &next.OutputPrice, entry.Cost.Output)
			if entry.Cost.Request > 0 {
				price("requestPrice", &next.RequestPrice, entry.Cost.Request)
			}
		case aigateway.KindEmbedding:
			price("inputPrice", &next.InputPrice, entry.Cost.Input)
		case aigateway.KindImage:
			if entry.Cost.Image > 0 {
				price("imagePrice", &next.ImagePrice, entry.Cost.Image)
			}
			if entry.Cost.Input > 0 || entry.Cost.Output > 0 {
				price("inputPrice", &next.InputPrice, entry.Cost.Input)
				price("outputPrice", &next.OutputPrice, entry.Cost.Output)
			}
		}
		// 目录说它免费：价格全为 0 时必须显式标记免费，否则保存会被拒。
		allZero := next.InputPrice.IsZero() && next.OutputPrice.IsZero() && next.RequestPrice.IsZero() &&
			next.ImagePrice.IsZero() && next.CharPrice.IsZero() && next.MinutePrice.IsZero() && len(next.ImagePrices) == 0
		if allZero && !next.Free {
			change("free", false, true)
			next.Free = true
		} else if !allZero && next.Free && entry.Cost != nil && !entry.Cost.Empty() {
			change("free", true, false)
			next.Free = false
		}
	}
	return next, changes
}

// ── 后台：每天刷新、自动同步 ──

// startCatalogLoop 每小时看一次：快照超过一天就刷新（多实例抢租约，只有一台去拉），
// 刷新成功后给打开了自动更新的应用同步对话模型的能力声明与说明。
func (s *AIGatewayService) startCatalogLoop(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("model catalog loop panic", zap.Any("panic", r))
			}
		}()
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-timer.C:
				s.catalogTick(ctx)
				timer.Reset(aiCatalogCheckEvery)
			}
		}
	}()
}

func (s *AIGatewayService) catalogTick(ctx context.Context) {
	status, err := s.RefreshModelCatalog(ctx, false)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			s.log.Warn("定时刷新模型目录失败", zap.Error(err))
		}
		return
	}
	if !status.Refreshed {
		return
	}
	apps, err := s.pg.ListAIGatewayCatalogAutoSyncApps(ctx)
	if err != nil {
		s.log.Warn("列出自动同步模型目录的应用失败", zap.Error(err))
		return
	}
	for _, appID := range apps {
		results, err := s.SyncModelsFromCatalog(ctx, appID, CatalogSyncInput{Fields: []string{CatalogFieldCapabilities, CatalogFieldInfo}})
		if err != nil {
			s.log.Warn("自动同步模型目录失败", zap.Int64("appid", appID), zap.Error(err))
			continue
		}
		applied := 0
		for _, result := range results {
			if result.Applied {
				applied++
			}
		}
		if applied > 0 {
			s.log.Info("模型目录已自动同步", zap.Int64("appid", appID), zap.Int("models", applied))
		}
	}
}
