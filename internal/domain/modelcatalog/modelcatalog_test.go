package modelcatalog

import (
	"slices"
	"testing"
)

const modelsDevFixture = `{
 "openai": {"id": "openai", "models": {
  "gpt-5": {"id": "gpt-5", "name": "GPT-5", "reasoning": true, "tool_call": true, "temperature": false,
   "reasoning_options": [{"type": "effort", "values": ["high", "minimal", "low", "medium"]}],
   "modalities": {"input": ["text", "image"], "output": ["text"]},
   "limit": {"context": 400000, "output": 128000}, "cost": {"input": 1.25, "output": 10, "cache_read": 0.125}},
  "gpt-image-1": {"id": "gpt-image-1", "type": "image", "modalities": {"input": ["text"], "output": ["image"]}, "limit": {}}
 }},
 "anthropic": {"id": "anthropic", "models": {
  "claude-sonnet-4-5-20250929": {"id": "claude-sonnet-4-5-20250929", "name": "Claude Sonnet 4.5", "reasoning": true, "tool_call": true,
   "reasoning_options": [{"type": "budget_tokens", "min": 1024}],
   "modalities": {"input": ["text", "image", "pdf"], "output": ["text"]}, "limit": {"context": 200000, "output": 64000}}
 }},
 "zhipuai": {"id": "zhipuai", "models": {
  "glm-4.6": {"id": "glm-4.6", "reasoning": true, "tool_call": true, "reasoning_options": [{"type": "toggle"}],
   "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 204800}}
 }},
 "helicone": {"id": "helicone", "models": {
  "gpt-5": {"id": "gpt-5", "tool_call": true, "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 1}},
  "claude-sonnet-4-5": {"id": "claude-sonnet-4-5", "tool_call": true, "modalities": {"input": ["text"], "output": ["text"]}, "limit": {}}
 }}
}`

const openRouterFixture = `{"data": [
 {"id": "anthropic/claude-sonnet-4.5", "name": "Anthropic: Claude Sonnet 4.5", "context_length": 1000000,
  "architecture": {"input_modalities": ["text", "image"], "output_modalities": ["text"]},
  "pricing": {"prompt": "0.000003", "completion": "0.000015"}, "top_provider": {"max_completion_tokens": 64000},
  "supported_parameters": ["tools", "reasoning", "temperature"]},
 {"id": "zhipuai-free/whatever", "context_length": 8000, "architecture": {"output_modalities": ["text"]},
  "pricing": {"prompt": "0", "completion": "0"}, "supported_parameters": []}
]}`

const liteLLMFixture = `{
 "sample_spec": {"litellm_provider": "openai"},
 "glm-4.6": {"litellm_provider": "zai", "mode": "chat", "max_output_tokens": 128000,
  "input_cost_per_token": 6e-07, "output_cost_per_token": 2.2e-06, "supports_function_calling": true},
 "bedrock/anthropic.claude": {"litellm_provider": "bedrock", "mode": "chat"},
 "deepseek/deepseek-chat": {"litellm_provider": "deepseek", "mode": "chat", "max_input_tokens": 131072,
  "input_cost_per_token": 2.8e-07, "output_cost_per_token": 4.2e-07, "supports_function_calling": true}
}`

func buildFixtureIndex(t *testing.T) *Index {
	t.Helper()
	modelsDev, err := ParseModelsDev([]byte(modelsDevFixture))
	if err != nil {
		t.Fatal(err)
	}
	own, fill, err := ParseOpenRouter([]byte(openRouterFixture))
	if err != nil {
		t.Fatal(err)
	}
	lite, err := ParseLiteLLM([]byte(liteLLMFixture))
	if err != nil {
		t.Fatal(err)
	}
	return NewIndex(Merge(Inputs{ModelsDev: modelsDev, OpenRouter: own, OpenRouterFill: fill, LiteLLM: lite}))
}

func TestParseAndMerge(t *testing.T) {
	index := buildFixtureIndex(t)
	gpt, ok := index.Get("openai/gpt-5")
	if !ok {
		t.Fatal("openai/gpt-5 missing")
	}
	if !gpt.Vision || !gpt.Tools || !gpt.Reasoning || !gpt.NoTemperature || gpt.ContextLength != 400000 || gpt.MaxOutput != 128000 {
		t.Fatalf("gpt-5 = %+v", gpt)
	}
	if !slices.Equal(gpt.ReasoningEfforts, []string{"minimal", "low", "medium", "high"}) {
		t.Fatalf("efforts not normalized: %v", gpt.ReasoningEfforts)
	}
	if gpt.Cost == nil || gpt.Cost.Input != 1.25 || gpt.Cost.Output != 10 {
		t.Fatalf("cost = %+v", gpt.Cost)
	}
	if image, _ := index.Get("openai/gpt-image-1"); image.Kind != KindImage {
		t.Fatalf("gpt-image-1 kind = %s", image.Kind)
	}

	claude, _ := index.Get("anthropic/claude-sonnet-4-5-20250929")
	if !slices.Equal(claude.ReasoningEfforts, []string{"none", "low", "medium", "high", "xhigh"}) {
		t.Fatalf("budget efforts = %v", claude.ReasoningEfforts)
	}
	if claude.ReasoningBudget == nil || claude.ReasoningBudget.Min != 1024 {
		t.Fatalf("budget = %+v", claude.ReasoningBudget)
	}

	// 智谱只有开关；LiteLLM 补上 models.dev 没给的价格与输出上限，能力仍以 models.dev 为准。
	glm, _ := index.Get("zhipuai/glm-4.6")
	if !slices.Equal(glm.ReasoningEfforts, []string{"none", "high"}) {
		t.Fatalf("toggle efforts = %v", glm.ReasoningEfforts)
	}
	if glm.Cost == nil || glm.Cost.Input != 0.6 || glm.Cost.Output != 2.2 || glm.MaxOutput != 128000 {
		t.Fatalf("litellm fill = %+v %+v", glm.Cost, glm.MaxOutput)
	}
	if !slices.Equal(glm.Sources, []string{SourceModelsDev, SourceLiteLLM}) {
		t.Fatalf("sources = %v", glm.Sources)
	}

	// LiteLLM 只收主流供应商：Bedrock 不进目录；DeepSeek 由它新建，前缀去掉。
	if _, ok := index.Get("deepseek/deepseek-chat"); !ok {
		t.Fatal("litellm should create deepseek/deepseek-chat")
	}
	for _, entry := range index.Search("claude", "", "", 50) {
		if entry.Provider == "bedrock" {
			t.Fatal("bedrock entries must be skipped")
		}
	}
	// OpenRouter 自己的条目留在 openrouter 名下，价格换算成每百万 token。
	router, ok := index.Get("openrouter/anthropic/claude-sonnet-4.5")
	if !ok || router.Cost == nil || router.Cost.Input != 3 || router.Cost.Output != 15 {
		t.Fatalf("openrouter entry = %+v", router)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-4-5-20250929":  "claude-sonnet-4-5",
		"anthropic/claude-sonnet-4.5": "claude-sonnet-4-5",
		"gpt-4o-2024-08-06":           "gpt-4o",
		"models/gemini-2.5-pro":       "gemini-2-5-pro",
		"deepseek-r1-0528":            "deepseek-r1",
		"doubao-seed-1-6-250615":      "doubao-seed-1-6",
		"openai/gpt-oss-120b:free":    "gpt-oss-120b",
		"claude-3-5-sonnet@20240620":  "claude-3-5-sonnet",
		"qwen3-235b-a22b-instruct":    "qwen3-235b-a22b-instruct",
		"mistral-large-latest":        "mistral-large",
		"text-embedding-3-small":      "text-embedding-3-small",
		"Meta-Llama-3.1-8B-Instruct":  "meta-llama-3-1-8b-instruct",
	}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMatchPrefersOriginAndHints(t *testing.T) {
	index := buildFixtureIndex(t)
	if got := index.Match("gpt-5", nil, 3); len(got) == 0 || got[0].Entry.Key != "openai/gpt-5" || got[0].Match != MatchExact {
		t.Fatalf("gpt-5 should pick the origin vendor: %+v", got)
	}
	if got := index.Match("gpt-5", []string{"helicone"}, 3); got[0].Entry.Key != "helicone/gpt-5" {
		t.Fatalf("explicit hint should win: %+v", got[0].Entry.Key)
	}
	// 转售商那里逐字相同，原厂要去掉日期才对得上：取原厂。
	if got := index.Match("claude-sonnet-4-5", nil, 3); got[0].Entry.Key != "anthropic/claude-sonnet-4-5-20250929" {
		t.Fatalf("origin vendor should beat a reseller's exact id: %+v", got[0].Entry.Key)
	}
	if got := index.Match("claude-sonnet-4.5", nil, 3); len(got) == 0 || got[0].Entry.Provider != "anthropic" || got[0].Match != MatchNormalized {
		t.Fatalf("normalized match = %+v", got)
	}
	// 「厂商/模型」写法：没有提示时归到原厂；通道是 OpenRouter 时取 OpenRouter 自己的条目（价格是它的）。
	if got := index.Match("anthropic/claude-sonnet-4.5", nil, 3); got[0].Entry.Key != "anthropic/claude-sonnet-4-5-20250929" {
		t.Fatalf("vendor-prefixed id should resolve to the origin entry: %+v", got[0].Entry.Key)
	}
	if got := index.Match("anthropic/claude-sonnet-4.5", []string{"openrouter"}, 3); got[0].Entry.Key != "openrouter/anthropic/claude-sonnet-4.5" {
		t.Fatalf("openrouter hint should pick the openrouter entry: %+v", got[0].Entry.Key)
	}
	if got := index.Match("gpt-5-mini-preview", nil, 3); len(got) == 0 || got[0].Match != MatchFuzzy {
		t.Fatalf("prefix should be a fuzzy match: %+v", got)
	}
	if got := index.Match("nothing-like-this", nil, 3); len(got) != 0 {
		t.Fatalf("unexpected match: %+v", got)
	}
}
