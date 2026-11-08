package modelcatalog

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Inputs 一次合并的原料：各来源解析好的条目。Fill 只给已有条目补缺，不新建。
type Inputs struct {
	ModelsDev  []Entry
	OpenRouter []Entry
	// OpenRouterFill OpenRouter 按厂商前缀拆出来的副本，补官方条目的缺。
	OpenRouterFill []Entry
	LiteLLM        []Entry
}

// Merge 合并成一份目录：models.dev 为主，OpenRouter 与 LiteLLM 只补它没有的字段；
// models.dev 没有的条目由 OpenRouter（自己的供应商名下）与 LiteLLM（主流供应商）补建。
func Merge(in Inputs) []Entry {
	index := map[string]int{}
	out := make([]Entry, 0, len(in.ModelsDev)+len(in.OpenRouter)+len(in.LiteLLM)/2)
	add := func(entries []Entry, create bool) {
		for _, entry := range entries {
			key := strings.ToLower(entry.Key)
			if at, ok := index[key]; ok {
				fillEntry(&out[at], entry)
				continue
			}
			if !create {
				continue
			}
			entry.Cost = roundCost(entry.Cost)
			index[key] = len(out)
			out = append(out, entry)
		}
	}
	add(in.ModelsDev, true)
	add(in.OpenRouter, true)
	add(in.OpenRouterFill, false)
	add(in.LiteLLM, true)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// fillEntry 用 src 补 dst 的空字段。能力布尔值以先到的来源为准，只有先到的来源没说「有」、
// 而它又不是 models.dev（能力表最全）时才取并集。
func fillEntry(dst *Entry, src Entry) {
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
	if dst.Family == "" {
		dst.Family = src.Family
	}
	if len(dst.Sources) == 0 || dst.Sources[0] != SourceModelsDev {
		dst.Vision = dst.Vision || src.Vision
		dst.Tools = dst.Tools || src.Tools
		dst.Reasoning = dst.Reasoning || src.Reasoning
		dst.StructuredOutput = dst.StructuredOutput || src.StructuredOutput
	}
	if dst.Reasoning && len(dst.ReasoningEfforts) == 0 {
		dst.ReasoningEfforts = src.ReasoningEfforts
	}
	if dst.ReasoningBudget == nil {
		dst.ReasoningBudget = src.ReasoningBudget
	}
	if len(dst.InputModalities) == 0 {
		dst.InputModalities = src.InputModalities
	}
	if len(dst.OutputModalities) == 0 {
		dst.OutputModalities = src.OutputModalities
	}
	if dst.ContextLength == 0 {
		dst.ContextLength = src.ContextLength
	}
	if dst.InputLimit == 0 {
		dst.InputLimit = src.InputLimit
	}
	if dst.MaxOutput == 0 {
		dst.MaxOutput = src.MaxOutput
	}
	if dst.Cost.Empty() && !src.Cost.Empty() {
		dst.Cost = roundCost(src.Cost)
	}
	for _, source := range src.Sources {
		if !slices.Contains(dst.Sources, source) {
			dst.Sources = append(dst.Sources, source)
		}
	}
}

// roundCost 价格保留 6 位小数：每 token 价 × 10⁶ 会带出 1.2499999… 这样的尾巴。
func roundCost(cost *Cost) *Cost {
	if cost.Empty() {
		return nil
	}
	round := func(value float64) float64 { return math.Round(value*1e6) / 1e6 }
	return &Cost{Input: round(cost.Input), Output: round(cost.Output), CacheRead: round(cost.CacheRead),
		CacheWrite: round(cost.CacheWrite), Image: round(cost.Image), Request: round(cost.Request)}
}

// ── 名字规整与匹配 ──

var (
	dateSuffix    = regexp.MustCompile(`[-_@](20\d{2}[-_]?\d{2}[-_]?\d{2}|2[3-9][01]\d[0-3]\d|\d{4})$`)
	versionSuffix = regexp.MustCompile(`[-_](latest|v\d+(\.\d+)?)$`)
)

// Normalize 规整模型名，用于「去掉日期与版本后缀后相同」的匹配：
// 小写；去掉 models/ 前缀与「厂商/」前缀；去掉 :free 一类的变体后缀、@ 之后的版本；
// 去掉末尾的日期（-20250929、-2025-08-07、豆包的 -250615、-0528）与 -latest、-v1；「.」「_」统一成「-」。
func Normalize(model string) string {
	value := strings.ToLower(strings.TrimSpace(model))
	value = strings.TrimPrefix(value, "models/")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	if index := strings.Index(value, ":"); index > 0 {
		value = value[:index]
	}
	if index := strings.Index(value, "@"); index > 0 {
		value = value[:index]
	}
	for range 2 {
		value = dateSuffix.ReplaceAllString(value, "")
		value = versionSuffix.ReplaceAllString(value, "")
	}
	value = strings.NewReplacer(".", "-", "_", "-", " ", "-").Replace(value)
	return strings.Trim(value, "-")
}

// officialRank 同名模型出现在多家时优先取谁：模型的原厂排前，聚合与转售排后。
var officialRank = map[string]int{
	"openai": 1, "anthropic": 1, "google": 1, "deepseek": 1, "xai": 1, "mistral": 1, "alibaba": 2,
	"alibaba-cn": 2, "moonshotai": 2, "moonshotai-cn": 2, "zhipuai": 2, "volcengine": 2, "minimax": 2,
	"cohere": 2, "llama": 2, "groq": 4, "siliconflow": 4, "siliconflow-cn": 4, "openrouter": 6,
}

// originVendors 按模型名前缀认出原厂：同名模型在几十家转售商那里都有，原厂的那一条才是准的。
var originVendors = []struct {
	prefix    string
	providers []string
}{
	{"gpt-", []string{"openai"}}, {"o1", []string{"openai"}}, {"o3", []string{"openai"}}, {"o4", []string{"openai"}},
	{"chatgpt", []string{"openai"}}, {"claude", []string{"anthropic"}}, {"gemini", []string{"google"}},
	{"gemma", []string{"google"}}, {"deepseek", []string{"deepseek"}}, {"grok", []string{"xai"}},
	{"qwen", []string{"alibaba-cn", "alibaba"}}, {"qwq", []string{"alibaba-cn", "alibaba"}},
	{"kimi", []string{"moonshotai-cn", "moonshotai"}}, {"moonshot", []string{"moonshotai-cn", "moonshotai"}},
	{"glm", []string{"zhipuai"}}, {"doubao", []string{"volcengine"}}, {"mistral", []string{"mistral"}},
	{"magistral", []string{"mistral"}}, {"codestral", []string{"mistral"}}, {"devstral", []string{"mistral"}},
	{"llama", []string{"llama"}}, {"minimax", []string{"minimax"}}, {"command", []string{"cohere"}},
}

func originOf(model string) []string {
	model = strings.ToLower(model)
	for _, item := range originVendors {
		if strings.HasPrefix(model, item.prefix) {
			return item.providers
		}
	}
	return nil
}

func providerRank(provider string) int {
	if rank, ok := officialRank[provider]; ok {
		return rank
	}
	return 5
}

// Index 目录的检索索引。
type Index struct {
	entries    []Entry
	byModel    map[string][]int
	normalized map[string][]int
}

// NewIndex 建索引。entries 之后不能再改。
func NewIndex(entries []Entry) *Index {
	index := &Index{entries: entries, byModel: map[string][]int{}, normalized: map[string][]int{}}
	for position, entry := range entries {
		model := strings.ToLower(entry.Model)
		index.byModel[model] = append(index.byModel[model], position)
		norm := Normalize(entry.Model)
		index.normalized[norm] = append(index.normalized[norm], position)
	}
	return index
}

// Len 条目数。
func (x *Index) Len() int { return len(x.entries) }

// Get 按 Key 取。
func (x *Index) Get(key string) (Entry, bool) {
	key = strings.ToLower(key)
	for _, position := range x.byModel[strings.ToLower(key[strings.Index(key, "/")+1:])] {
		if strings.ToLower(x.entries[position].Key) == key {
			return x.entries[position], true
		}
	}
	return Entry{}, false
}

// Match 按模型名找最像的几条，最好的在前。providers 是供应商提示（通道的供应商对应的目录供应商名），
// 命中提示的优先。query 可以带「厂商/」前缀（OpenRouter 风格），前缀认得出就当作提示。
func (x *Index) Match(query string, providers []string, limit int) []Candidate {
	query = strings.TrimSpace(query)
	if query == "" || x == nil {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}
	hints := append([]string(nil), providers...)
	lower := strings.ToLower(strings.TrimPrefix(query, "models/"))
	bare := lower
	if vendor, rest, ok := strings.Cut(lower, "/"); ok {
		if provider, known := openRouterVendors[vendor]; known {
			hints = append(hints, provider)
		} else {
			hints = append(hints, vendor)
		}
		bare = rest
	}
	// 调用方的提示（通道是哪一家）压过一切；其次是按模型名认出的原厂 —— 原厂的条目哪怕要去掉日期后缀
	// 才对得上（claude-opus-4-1 ↔ claude-opus-4-1-20250805），也胜过转售商那里逐字相同的一条。
	origins := originOf(bare)
	score := func(entry Entry, base int) int {
		value := base * 1000
		if at := slices.Index(hints, entry.Provider); at >= 0 {
			value -= 5000 - at*10
		} else if at := slices.Index(origins, entry.Provider); at >= 0 {
			value -= 2000 - at*10
		}
		value += providerRank(entry.Provider) * 20
		value -= len(entry.Sources) * 2
		if entry.Status == "deprecated" {
			value += 50
		}
		return value
	}
	seen := map[int]bool{}
	var out []Candidate
	collect := func(positions []int, match string, base int) {
		for _, position := range positions {
			if seen[position] {
				continue
			}
			seen[position] = true
			out = append(out, Candidate{Entry: x.entries[position], Match: match, Score: score(x.entries[position], base)})
		}
	}
	// 带前缀的整串（openrouter/openai/gpt-5 这种）先按 OpenRouter 自己的条目对一次。
	if bare != lower {
		collect(x.byModel[lower], MatchExact, 0)
	}
	collect(x.byModel[bare], MatchExact, 0)
	norm := Normalize(query)
	collect(x.normalized[norm], MatchNormalized, 1)
	if len(out) == 0 && len(norm) >= 4 {
		for position, entry := range x.entries {
			candidate := Normalize(entry.Model)
			if len(candidate) < 4 {
				continue
			}
			if strings.HasPrefix(candidate, norm) || strings.HasPrefix(norm, candidate) {
				diff := len(candidate) - len(norm)
				if diff < 0 {
					diff = -diff
				}
				if !seen[position] {
					seen[position] = true
					out = append(out, Candidate{Entry: entry, Match: MatchFuzzy, Score: score(entry, 2) + diff*5})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score < out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Search 关键词检索（空格分开的每个词都要出现在供应商、模型名或显示名里）。kind、provider 为空不限。
func (x *Index) Search(query, provider, kind string, limit int) []Entry {
	if x == nil {
		return nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	words := strings.Fields(strings.ToLower(query))
	type scored struct {
		entry Entry
		rank  int
	}
	var hits []scored
	for _, entry := range x.entries {
		if provider != "" && entry.Provider != provider {
			continue
		}
		if kind != "" && entry.Kind != kind {
			continue
		}
		haystack := strings.ToLower(entry.Key + " " + entry.Name)
		matched := true
		for _, word := range words {
			if !strings.Contains(haystack, word) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		rank := providerRank(entry.Provider)*100 + len(entry.Model)
		if len(words) > 0 && strings.EqualFold(entry.Model, strings.Join(words, " ")) {
			rank -= 1000
		}
		hits = append(hits, scored{entry, rank})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Entry, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.entry)
	}
	return out
}

// Providers 目录里出现过的供应商与各自的条目数，按条目数从多到少。
func (x *Index) Providers() []ProviderCount {
	if x == nil {
		return nil
	}
	counts := map[string]int{}
	for _, entry := range x.entries {
		counts[entry.Provider]++
	}
	out := make([]ProviderCount, 0, len(counts))
	for provider, count := range counts {
		out = append(out, ProviderCount{Provider: provider, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

// ProviderCount 一个供应商的条目数。
type ProviderCount struct {
	Provider string `json:"provider"`
	Count    int    `json:"count"`
}
