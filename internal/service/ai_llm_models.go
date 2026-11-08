package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
)

// 拉取型号清单：各协议官方的「列出模型」接口。
//
// 端点语义与对话客户端一致（同一个 baseURL 归一规则），所以「能拉到型号」基本等于「这条通道的地址与密钥是对的」——
// 控制台把它当作比连通性测试更轻的第一道自检。

const (
	aiModelListTimeout  = 30 * time.Second
	aiModelListMaxBytes = 8 << 20
	aiModelListMaxPages = 20
)

// ListModels 按通道协议拉型号清单，去重后按 ID 排序。
func (c *aiLLMClient) ListModels(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta) ([]aidomain.ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, aiModelListTimeout)
	defer cancel()
	var models []aidomain.ModelInfo
	var err error
	switch {
	case meta.Protocol == aidomain.ProtocolAnthropic:
		models, err = c.listAnthropicModels(ctx, cfg, meta)
	case meta.Protocol == aidomain.ProtocolGemini:
		models, err = c.listGeminiModels(ctx, cfg, meta)
	case cfg.Provider == aidomain.ProviderAzureOpenAI:
		models, err = c.listAzureDeployments(ctx, cfg, meta)
	default:
		models, err = c.listOpenAIModels(ctx, cfg, meta)
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]aidomain.ModelInfo, 0, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// getJSON 发一次 GET，非 2xx 收敛成 aiUpstreamError（与对话客户端同一套错误摘要）。
func (c *aiLLMClient) getJSON(ctx context.Context, provider, endpoint string, headers http.Header, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &aiUpstreamError{Provider: provider, Message: "端点地址无效：" + err.Error()}
	}
	request.Header = headers
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient(provider).Do(request)
	if err != nil {
		return &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, aiModelListMaxBytes))
	if err != nil {
		return &aiUpstreamError{Provider: provider, Message: "读取响应失败：" + err.Error()}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := parseUpstreamErrorMessage(body)
		if message == "" {
			message = summarizeOpaqueUpstreamBody(body, response.Status)
		}
		if message == "" {
			message = response.Status
		}
		return &aiUpstreamError{Provider: provider, Status: response.StatusCode, Message: message}
	}
	if err := json.Unmarshal(body, target); err != nil {
		if summary := summarizeOpaqueUpstreamBody(body, response.Status); strings.HasPrefix(summary, "上游返回") {
			return &aiUpstreamError{Provider: provider, Status: response.StatusCode, Message: summary}
		}
		return &aiUpstreamError{Provider: provider, Status: response.StatusCode, Message: "型号清单不是合法的 JSON：" + err.Error()}
	}
	return nil
}

func extraHeaders(cfg aidomain.Config) http.Header {
	headers := http.Header{}
	for key, value := range cfg.SettingMap(aidomain.KeyExtraHeaders) {
		if key = strings.TrimSpace(key); key != "" {
			headers.Set(key, value)
		}
	}
	return headers
}

// listOpenAIModels GET {base}/models。兼容站的形状五花八门：标准的 {data:[…]}、
// 裸数组、{models:[…]}（部分国内厂商），都收。
func (c *aiLLMClient) listOpenAIModels(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta) ([]aidomain.ModelInfo, error) {
	headers := http.Header{}
	if key := cfg.Secret(aidomain.KeyAPIKey); key != "" {
		headers.Set("Authorization", "Bearer "+key)
	}
	for key, values := range extraHeaders(cfg) {
		headers[key] = values
	}
	var raw json.RawMessage
	if err := c.getJSON(ctx, meta.Provider, c.baseURL(cfg, meta)+"/models", headers, &raw); err != nil {
		return nil, err
	}
	type entry struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		OwnedBy     string `json:"owned_by"`
		Created     int64  `json:"created"`
		Description string `json:"description"`
		Context     int64  `json:"context_length"`
	}
	var entries []entry
	var envelope struct {
		Data   []entry `json:"data"`
		Models []entry `json:"models"`
	}
	if json.Unmarshal(raw, &envelope) == nil && (len(envelope.Data) > 0 || len(envelope.Models) > 0) {
		entries = append(envelope.Data, envelope.Models...)
	} else {
		_ = json.Unmarshal(raw, &entries)
	}
	out := make([]aidomain.ModelInfo, 0, len(entries))
	for _, item := range entries {
		id := item.ID
		if id == "" {
			id = item.Name
		}
		name := item.DisplayName
		if name == "" && item.Name != id {
			name = item.Name
		}
		out = append(out, aidomain.ModelInfo{ID: id, Name: name, OwnedBy: item.OwnedBy, Created: item.Created,
			Description: item.Description, ContextWindow: item.Context})
	}
	return out, nil
}

// listAnthropicModels GET {base}/models（base 自带 /v1），按 has_more / last_id 翻页。
func (c *aiLLMClient) listAnthropicModels(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta) ([]aidomain.ModelInfo, error) {
	headers := http.Header{}
	headers.Set("anthropic-version", "2023-06-01")
	if key := cfg.Secret(aidomain.KeyAPIKey); key != "" {
		headers.Set("x-api-key", key)
	}
	for key, values := range extraHeaders(cfg) {
		headers[key] = values
	}
	out := []aidomain.ModelInfo{}
	after := ""
	for page := 0; page < aiModelListMaxPages; page++ {
		query := url.Values{"limit": {"1000"}}
		if after != "" {
			query.Set("after_id", after)
		}
		var payload struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				CreatedAt   string `json:"created_at"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := c.getJSON(ctx, meta.Provider, c.baseURL(cfg, meta)+"/models?"+query.Encode(), headers.Clone(), &payload); err != nil {
			return nil, err
		}
		for _, item := range payload.Data {
			var created int64
			if parsed, err := time.Parse(time.RFC3339, item.CreatedAt); err == nil {
				created = parsed.Unix()
			}
			out = append(out, aidomain.ModelInfo{ID: item.ID, Name: item.DisplayName, OwnedBy: "anthropic", Created: created})
		}
		if !payload.HasMore || payload.LastID == "" || payload.LastID == after {
			break
		}
		after = payload.LastID
	}
	return out, nil
}

// listGeminiModels GET {base}/v1beta/models，只留支持 generateContent 的（嵌入、向量模型对对话通道没用），
// 去掉 "models/" 前缀 —— 对话请求里填的就是去掉前缀的名字。
func (c *aiLLMClient) listGeminiModels(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta) ([]aidomain.ModelInfo, error) {
	base := geminiBaseURL(cfg)
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	headers := http.Header{}
	if key := cfg.Secret(aidomain.KeyAPIKey); key != "" {
		headers.Set("x-goog-api-key", key)
	}
	for key, values := range extraHeaders(cfg) {
		headers[key] = values
	}
	out := []aidomain.ModelInfo{}
	token := ""
	for page := 0; page < aiModelListMaxPages; page++ {
		query := url.Values{"pageSize": {"1000"}}
		if token != "" {
			query.Set("pageToken", token)
		}
		var payload struct {
			Models []struct {
				Name                       string   `json:"name"`
				DisplayName                string   `json:"displayName"`
				Description                string   `json:"description"`
				InputTokenLimit            int64    `json:"inputTokenLimit"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.getJSON(ctx, meta.Provider, base+"/v1beta/models?"+query.Encode(), headers.Clone(), &payload); err != nil {
			return nil, err
		}
		for _, item := range payload.Models {
			if len(item.SupportedGenerationMethods) > 0 && !slices.Contains(item.SupportedGenerationMethods, "generateContent") {
				continue
			}
			out = append(out, aidomain.ModelInfo{ID: strings.TrimPrefix(item.Name, "models/"), Name: item.DisplayName,
				OwnedBy: "google", Description: item.Description, ContextWindow: item.InputTokenLimit})
		}
		if payload.NextPageToken == "" || payload.NextPageToken == token {
			break
		}
		token = payload.NextPageToken
	}
	return out, nil
}

// listAzureDeployments Azure 的「型号」是部署名：GET {endpoint}/openai/deployments。
// 这个数据面接口在较新的 api-version 里已移除，固定用最后一个还提供它的版本。
func (c *aiLLMClient) listAzureDeployments(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta) ([]aidomain.ModelInfo, error) {
	headers := http.Header{}
	if key := cfg.Secret(aidomain.KeyAPIKey); key != "" {
		headers.Set("api-key", key)
	}
	for key, values := range extraHeaders(cfg) {
		headers[key] = values
	}
	var payload struct {
		Data []struct {
			ID     string `json:"id"`
			Model  string `json:"model"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, meta.Provider, c.baseURL(cfg, meta)+"/openai/deployments?api-version=2022-12-01", headers, &payload); err != nil {
		return nil, err
	}
	out := make([]aidomain.ModelInfo, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.Status != "" && item.Status != "succeeded" {
			continue
		}
		out = append(out, aidomain.ModelInfo{ID: item.ID, Name: item.Model, OwnedBy: "azure"})
	}
	return out, nil
}
