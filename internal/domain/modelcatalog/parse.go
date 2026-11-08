package modelcatalog

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// 三种来源的解析。只认需要的字段，其余忽略：来源随时会加字段，解析不能因此失败。

// ── models.dev ──

type modelsDevProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Family           string `json:"family"`
	Type             string `json:"type"`
	Attachment       bool   `json:"attachment"`
	Reasoning        bool   `json:"reasoning"`
	ReasoningOptions []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
		Min    *int     `json:"min"`
		Max    *int     `json:"max"`
	} `json:"reasoning_options"`
	ToolCall         bool  `json:"tool_call"`
	StructuredOutput bool  `json:"structured_output"`
	Temperature      *bool `json:"temperature"`
	OpenWeights      bool  `json:"open_weights"`
	Modalities       struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context"`
		Input   int `json:"input"`
		Output  int `json:"output"`
	} `json:"limit"`
	Cost *struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"cost"`
	Knowledge   string `json:"knowledge"`
	ReleaseDate string `json:"release_date"`
	Status      string `json:"status"`
}

// ParseModelsDev 解析 https://models.dev/api.json：{供应商: {models: {模型: …}}}。
func ParseModelsDev(raw []byte) ([]Entry, error) {
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, fmt.Errorf("models.dev: %w", err)
	}
	out := make([]Entry, 0, 9000)
	for providerKey, provider := range providers {
		providerID := strings.ToLower(strings.TrimSpace(firstNonEmpty(provider.ID, providerKey)))
		for modelKey, model := range provider.Models {
			id := strings.TrimSpace(firstNonEmpty(model.ID, modelKey))
			if providerID == "" || id == "" {
				continue
			}
			entry := Entry{
				Provider: providerID, Model: id,
				Name: model.Name, Family: model.Family, Description: model.Description,
				Kind:             modelsDevKind(model.Type, model.Modalities.Output),
				Vision:           slices.Contains(model.Modalities.Input, "image"),
				Tools:            model.ToolCall,
				Reasoning:        model.Reasoning,
				StructuredOutput: model.StructuredOutput,
				NoTemperature:    model.Temperature != nil && !*model.Temperature,
				OpenWeights:      model.OpenWeights,
				InputModalities:  model.Modalities.Input,
				OutputModalities: model.Modalities.Output,
				ContextLength:    model.Limit.Context,
				InputLimit:       model.Limit.Input,
				MaxOutput:        model.Limit.Output,
				Knowledge:        model.Knowledge,
				ReleaseDate:      model.ReleaseDate,
				Status:           model.Status,
				Sources:          []string{SourceModelsDev},
			}
			if model.Cost != nil {
				entry.Cost = &Cost{Input: model.Cost.Input, Output: model.Cost.Output,
					CacheRead: model.Cost.CacheRead, CacheWrite: model.Cost.CacheWrite}
			}
			if entry.Reasoning {
				for _, option := range model.ReasoningOptions {
					switch option.Type {
					case "effort":
						entry.ReasoningEfforts = NormalizeEfforts(option.Values)
					case "toggle":
						// 只能开关：关是 none，开取 high（网关把任何「开」翻成各家的开关）。
						entry.ReasoningEfforts = []string{"none", "high"}
					case "budget_tokens":
						budget := &Budget{}
						if option.Min != nil {
							budget.Min = *option.Min
						}
						if option.Max != nil {
							budget.Max = *option.Max
						}
						entry.ReasoningBudget = budget
						entry.ReasoningEfforts = budgetEfforts(providerID, budget, entry.MaxOutput)
					}
				}
			}
			entry.Key = entry.Provider + "/" + entry.Model
			out = append(out, entry)
		}
	}
	return out, nil
}

func modelsDevKind(kind string, output []string) string {
	switch strings.ToLower(kind) {
	case "image", "video":
		return KindImage
	case "embedding":
		return KindEmbedding
	case "speech":
		return KindSpeech
	case "transcription":
		return KindTranscription
	case "reranking", "rerank":
		return KindRerank
	case "realtime":
		return KindOther
	}
	if len(output) > 0 && !slices.Contains(output, "text") {
		if slices.Contains(output, "image") || slices.Contains(output, "video") {
			return KindImage
		}
		if slices.Contains(output, "audio") {
			return KindSpeech
		}
		return KindOther
	}
	return KindChat
}

// budgetEfforts 按预算控制思考的模型折算成强度档：low / medium / high 总有；
// 预算下限为 0（或 Anthropic 一系，思考本来就能关）时可以关；预算够大时给 xhigh。
func budgetEfforts(provider string, budget *Budget, maxOutput int) []string {
	out := make([]string, 0, 5)
	if budget.Min == 0 || strings.Contains(provider, "anthropic") {
		out = append(out, "none")
	}
	out = append(out, "low", "medium", "high")
	if budget.Max >= 32768 || (budget.Max == 0 && maxOutput >= 40000) {
		out = append(out, "xhigh")
	}
	return out
}

// effortOrder 强度由低到高，与 aidomain.ReasoningEfforts 一致（这里不引用它，保持本包无依赖）。
var effortOrder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// NormalizeEfforts 去重、去掉不认识的、按由低到高排好。
func NormalizeEfforts(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "x-high" || value == "extra_high" {
			value = "xhigh"
		}
		seen[value] = true
	}
	out := make([]string, 0, len(seen))
	for _, effort := range effortOrder {
		if seen[effort] {
			out = append(out, effort)
		}
	}
	return out
}

// ── OpenRouter ──

type openRouterModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
		Request    string `json:"request"`
		Image      string `json:"image"`
		CacheRead  string `json:"input_cache_read"`
		CacheWrite string `json:"input_cache_write"`
	} `json:"pricing"`
	TopProvider struct {
		ContextLength       int `json:"context_length"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
	SupportedParameters []string `json:"supported_parameters"`
}

// openRouterVendors OpenRouter 的厂商前缀 → models.dev 的供应商名：用来给官方条目补缺。
var openRouterVendors = map[string]string{
	"openai": "openai", "anthropic": "anthropic", "google": "google", "deepseek": "deepseek",
	"x-ai": "xai", "mistralai": "mistral", "moonshotai": "moonshotai", "qwen": "alibaba",
	"z-ai": "zhipuai", "meta-llama": "llama", "cohere": "cohere", "minimax": "minimax",
}

// ParseOpenRouter 解析 https://openrouter.ai/api/v1/models。
// 返回 OpenRouter 自己的条目，以及按厂商前缀给官方条目补缺用的副本（fill 只补不建）。
func ParseOpenRouter(raw []byte) (own []Entry, fill []Entry, err error) {
	var body struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, nil, fmt.Errorf("openrouter: %w", err)
	}
	for _, model := range body.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		params := model.SupportedParameters
		entry := Entry{
			Provider: "openrouter", Model: id, Name: model.Name, Description: model.Description,
			Kind:             openRouterKind(model.Architecture.OutputModalities),
			Vision:           slices.Contains(model.Architecture.InputModalities, "image"),
			Tools:            slices.Contains(params, "tools"),
			Reasoning:        slices.Contains(params, "reasoning") || slices.Contains(params, "include_reasoning"),
			StructuredOutput: slices.Contains(params, "structured_outputs"),
			NoTemperature:    len(params) > 0 && !slices.Contains(params, "temperature"),
			InputModalities:  model.Architecture.InputModalities,
			OutputModalities: model.Architecture.OutputModalities,
			ContextLength:    firstPositive(model.ContextLength, model.TopProvider.ContextLength),
			MaxOutput:        model.TopProvider.MaxCompletionTokens,
			Sources:          []string{SourceOpenRouter},
		}
		cost := &Cost{
			Input: perMillion(model.Pricing.Prompt), Output: perMillion(model.Pricing.Completion),
			CacheRead: perMillion(model.Pricing.CacheRead), CacheWrite: perMillion(model.Pricing.CacheWrite),
			Request: parseFloat(model.Pricing.Request), Image: parseFloat(model.Pricing.Image),
		}
		if !cost.Empty() {
			entry.Cost = cost
		}
		entry.Key = entry.Provider + "/" + entry.Model
		own = append(own, entry)

		if vendor, name, ok := strings.Cut(id, "/"); ok {
			if provider, known := openRouterVendors[vendor]; known {
				copied := entry
				copied.Provider, copied.Model = provider, strings.TrimSuffix(name, ":free")
				copied.Key = copied.Provider + "/" + copied.Model
				fill = append(fill, copied)
			}
		}
	}
	return own, fill, nil
}

func openRouterKind(output []string) string {
	if len(output) > 0 && !slices.Contains(output, "text") && slices.Contains(output, "image") {
		return KindImage
	}
	return KindChat
}

// ── LiteLLM ──

type liteLLMModel struct {
	Provider            string   `json:"litellm_provider"`
	Mode                string   `json:"mode"`
	MaxInputTokens      int      `json:"max_input_tokens"`
	MaxOutputTokens     int      `json:"max_output_tokens"`
	MaxTokens           int      `json:"max_tokens"`
	InputCost           float64  `json:"input_cost_per_token"`
	OutputCost          float64  `json:"output_cost_per_token"`
	CacheReadCost       float64  `json:"cache_read_input_token_cost"`
	CacheWriteCost      float64  `json:"cache_creation_input_token_cost"`
	ImageCost           float64  `json:"output_cost_per_image"`
	SupportsVision      bool     `json:"supports_vision"`
	SupportsTools       bool     `json:"supports_function_calling"`
	SupportsReasoning   bool     `json:"supports_reasoning"`
	SupportsSchema      bool     `json:"supports_response_schema"`
	SupportedModalities []string `json:"supported_modalities"`
	OutputModalities    []string `json:"supported_output_modalities"`
}

// liteLLMProviders LiteLLM 的供应商名 → models.dev 的供应商名。只收这些：LiteLLM 里同一个模型在
// Bedrock、Azure、Vertex 各有一份，全收只会把检索结果搅乱，官方条目又已经由 models.dev 给了。
var liteLLMProviders = map[string]string{
	"openai": "openai", "anthropic": "anthropic", "gemini": "google", "deepseek": "deepseek",
	"mistral": "mistral", "xai": "xai", "groq": "groq", "dashscope": "alibaba", "moonshot": "moonshotai",
	"zai": "zhipuai", "openrouter": "openrouter", "cohere": "cohere", "volcengine": "volcengine",
}

// ParseLiteLLM 解析 LiteLLM 的 model_prices_and_context_window.json：{模型: {…}}。
func ParseLiteLLM(raw []byte) ([]Entry, error) {
	var models map[string]json.RawMessage
	if err := json.Unmarshal(raw, &models); err != nil {
		return nil, fmt.Errorf("litellm: %w", err)
	}
	out := make([]Entry, 0, 1500)
	for key, value := range models {
		if key == "sample_spec" {
			continue
		}
		var model liteLLMModel
		if err := json.Unmarshal(value, &model); err != nil {
			continue
		}
		provider, ok := liteLLMProviders[model.Provider]
		if !ok {
			continue
		}
		name := key
		if prefix, rest, found := strings.Cut(key, "/"); found && (prefix == model.Provider || liteLLMProviders[prefix] != "") {
			name = rest
		}
		entry := Entry{
			Provider: provider, Model: name,
			Kind:             liteLLMKind(model.Mode),
			Vision:           model.SupportsVision,
			Tools:            model.SupportsTools,
			Reasoning:        model.SupportsReasoning,
			StructuredOutput: model.SupportsSchema,
			InputModalities:  model.SupportedModalities,
			OutputModalities: model.OutputModalities,
			ContextLength:    model.MaxInputTokens,
			MaxOutput:        firstPositive(model.MaxOutputTokens, model.MaxTokens),
			Sources:          []string{SourceLiteLLM},
		}
		cost := &Cost{Input: model.InputCost * 1e6, Output: model.OutputCost * 1e6,
			CacheRead: model.CacheReadCost * 1e6, CacheWrite: model.CacheWriteCost * 1e6, Image: model.ImageCost}
		entry.Cost = roundCost(cost)
		entry.Key = entry.Provider + "/" + entry.Model
		out = append(out, entry)
	}
	return out, nil
}

func liteLLMKind(mode string) string {
	switch mode {
	case "chat", "completion", "responses":
		return KindChat
	case "embedding":
		return KindEmbedding
	case "image_generation", "image_edit":
		return KindImage
	case "audio_speech":
		return KindSpeech
	case "audio_transcription":
		return KindTranscription
	case "rerank":
		return KindRerank
	case "":
		return KindChat
	}
	return KindOther
}

// ── 小工具 ──

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func parseFloat(value string) float64 {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || number < 0 {
		return 0
	}
	return number
}

// perMillion 每 token 的美元价 → 每百万 token，消掉浮点尾巴（0.00000125 × 1e6 = 1.2499999…）。
func perMillion(value string) float64 {
	number := parseFloat(value) * 1e6
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(number, 'f', 6, 64), 64)
	return rounded
}
