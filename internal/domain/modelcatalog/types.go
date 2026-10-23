// Package modelcatalog 公开模型目录：从 models.dev、OpenRouter、LiteLLM 拉来的模型元数据
// （能力、上下文与输出上限、思考选项、美元单价），归一成一份可检索、可按名字匹配的目录。
//
// 这里全是纯函数：解析三种来源的 JSON、合并、规整模型名、按名字找最像的那一条。
// 拉取、落库、定时刷新与同步到网关模型在 service/ai_model_catalog.go。
package modelcatalog

import "time"

// 来源。合并时按这个顺序取值：前面的来源有值就不再看后面的。
const (
	SourceModelsDev  = "models.dev"
	SourceOpenRouter = "openrouter"
	SourceLiteLLM    = "litellm"
)

// Sources 全部来源，按优先级。
var Sources = []string{SourceModelsDev, SourceOpenRouter, SourceLiteLLM}

// SourceURLs 各来源的地址。
var SourceURLs = map[string]string{
	SourceModelsDev:  "https://models.dev/api.json",
	SourceOpenRouter: "https://openrouter.ai/api/v1/models",
	SourceLiteLLM:    "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json",
}

// 模型种类，与 aigateway 的种类同名。
const (
	KindChat          = "chat"
	KindEmbedding     = "embedding"
	KindImage         = "image"
	KindSpeech        = "speech"
	KindTranscription = "transcription"
	KindRerank        = "rerank"
	KindOther         = "other"
)

// Cost 美元单价：每百万 token（或每百万字符 / 每张图 / 每分钟，按 Kind 理解）。
type Cost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
	// Image 每张图（生图模型）。
	Image float64 `json:"image,omitempty"`
	// Request 每次调用。
	Request float64 `json:"request,omitempty"`
}

// Empty 一个价格都没有。
func (c *Cost) Empty() bool {
	return c == nil || (c.Input == 0 && c.Output == 0 && c.CacheRead == 0 && c.CacheWrite == 0 && c.Image == 0 && c.Request == 0)
}

// Budget 思考预算的上下限（token），0 表示没说。
type Budget struct {
	Min int `json:"min,omitempty"`
	Max int `json:"max,omitempty"`
}

// Entry 目录里的一个模型。
type Entry struct {
	// Key 「供应商/模型」，目录内唯一。
	Key      string `json:"key"`
	Provider string `json:"provider"`
	Model    string `json:"model"`

	Name        string `json:"name,omitempty"`
	Family      string `json:"family,omitempty"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`

	Vision           bool `json:"vision"`
	Tools            bool `json:"tools"`
	Reasoning        bool `json:"reasoning"`
	StructuredOutput bool `json:"structuredOutput,omitempty"`
	// NoTemperature 模型不收 temperature（OpenAI 推理模型）。
	NoTemperature bool `json:"noTemperature,omitempty"`
	OpenWeights   bool `json:"openWeights,omitempty"`

	// ReasoningEfforts 可选的思考强度（reasoning_effort 取值，由低到高）；Reasoning 为 false 时为空。
	ReasoningEfforts []string `json:"reasoningEfforts,omitempty"`
	// ReasoningBudget 思考按预算控制的模型（Anthropic、Gemini 2.x）的预算范围。
	ReasoningBudget *Budget `json:"reasoningBudget,omitempty"`

	InputModalities  []string `json:"inputModalities,omitempty"`
	OutputModalities []string `json:"outputModalities,omitempty"`

	ContextLength int `json:"contextLength,omitempty"`
	InputLimit    int `json:"inputLimit,omitempty"`
	MaxOutput     int `json:"maxOutput,omitempty"`

	Cost *Cost `json:"cost,omitempty"`

	Knowledge   string `json:"knowledge,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	// Status deprecated / beta 等，空表示正常。
	Status string `json:"status,omitempty"`

	// Sources 哪些来源提供了这一条（按优先级）。
	Sources []string `json:"sources"`
}

// SourceStatus 一个来源最近一次拉取的结果。
type SourceStatus struct {
	Source    string    `json:"source"`
	URL       string    `json:"url"`
	Count     int       `json:"count"`
	Error     string    `json:"error,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// Snapshot 合并好的整份目录。
type Snapshot struct {
	Entries   []Entry        `json:"entries"`
	Sources   []SourceStatus `json:"sources"`
	FetchedAt time.Time      `json:"fetchedAt"`
}

// 匹配的把握。
const (
	// MatchExact 模型名逐字相同（含供应商提示命中）。
	MatchExact = "exact"
	// MatchNormalized 去掉日期、版本后缀、供应商前缀后相同。
	MatchNormalized = "normalized"
	// MatchFuzzy 只是包含关系，要人看一眼。
	MatchFuzzy = "fuzzy"
)

// Candidate 一个匹配结果。
type Candidate struct {
	Entry Entry  `json:"entry"`
	Match string `json:"match"`
	Score int    `json:"score"`
}
