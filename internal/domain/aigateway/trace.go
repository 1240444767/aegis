package aigateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// 调用追踪：一次中转调用在上游走过的每一次尝试，以及请求、响应内容的摘要（docs/ai-gateway.md「调用追踪」）。
//
// 调用记录本身只留计费需要的东西；追踪是给管理员排查用的：链路回退换了几条通道、
// 每条通道回了什么状态、上游的请求 ID、首包与首字耗时、限流余量，以及调用方发了什么、上游回了什么。
// 只给管理端看，用户端的调用记录不带。

// 一次上游尝试的状态。
const (
	AttemptRunning   = "running"
	AttemptSucceeded = "succeeded"
	AttemptFailed    = "failed"
	// AttemptCanceled 客户端断开导致的中止（不是上游的错）。
	AttemptCanceled = "canceled"
)

// Attempt 一次上游尝试：链路里的一条通道被调用了一次。
type Attempt struct {
	Seq        int    `json:"seq"`
	ConfigID   int64  `json:"configId,omitempty"`
	ConfigName string `json:"configName,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	// Inherited 用的是平台共享通道。
	Inherited bool   `json:"inherited,omitempty"`
	Model     string `json:"model,omitempty"`
	// URL 只有 scheme、主机与路径：查询串里可能带密钥（Gemini 的 ?key=）。
	URL        string `json:"url,omitempty"`
	Method     string `json:"method,omitempty"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	// UpstreamRequestID 上游响应头里的请求编号（x-request-id / request-id 等），向供应商报障时用。
	UpstreamRequestID string    `json:"upstreamRequestId,omitempty"`
	Error             string    `json:"error,omitempty"`
	Retryable         bool      `json:"retryable,omitempty"`
	StartedAt         time.Time `json:"startedAt"`
	// HeadersMs 发出请求到收到响应头；FirstTokenMs 到第一个内容增量（流式）；LatencyMs 整次尝试。
	HeadersMs     int   `json:"headersMs,omitempty"`
	FirstTokenMs  int   `json:"firstTokenMs,omitempty"`
	LatencyMs     int   `json:"latencyMs"`
	RequestBytes  int64 `json:"requestBytes,omitempty"`
	ResponseBytes int64 `json:"responseBytes,omitempty"`
	// RateLimit 上游报回的限流余量（x-ratelimit-* / anthropic-ratelimit-* 响应头）。
	RateLimit map[string]string `json:"rateLimit,omitempty"`
}

// Trace 一次调用的追踪详情。
type Trace struct {
	Attempts []Attempt    `json:"attempts"`
	Request  TraceContent `json:"request"`
	Response TraceContent `json:"response"`
}

// TraceContent 请求或响应的摘要：参数原样、长文本截断、base64 换成占位。
type TraceContent map[string]any

// TraceSummary 写进调用记录行的追踪摘要（列表直接显示，不必再取追踪详情）。
type TraceSummary struct {
	Attempts          int    `json:"attempts"`
	FirstTokenMs      int    `json:"firstTokenMs,omitempty"`
	FinishReason      string `json:"finishReason,omitempty"`
	UpstreamRequestID string `json:"upstreamRequestId,omitempty"`
	RequestBytes      int64  `json:"requestBytes,omitempty"`
	ResponseBytes     int64  `json:"responseBytes,omitempty"`
}

// LiveProgress 进行中调用的实时进度（只走实时推送，不入库）。
type LiveProgress struct {
	ElapsedMs    int   `json:"elapsedMs"`
	FirstTokenMs int   `json:"firstTokenMs,omitempty"`
	OutputChars  int64 `json:"outputChars,omitempty"`
	// OutputTokens 按已收到的内容估算的输出 token。
	OutputTokens int64 `json:"outputTokens,omitempty"`
	Attempt      int   `json:"attempt,omitempty"`
}

// ── 内容摘要 ──

const (
	// TraceTextLimit 单段文本最多留多少字。
	TraceTextLimit = 2000
	// traceStringLimit JSON 里单个字符串值最多留多少字（参数里的长文本、文档）。
	traceStringLimit = 1000
	// traceArrayLimit 数组最多留多少项（嵌入向量、重排文档）。
	traceArrayLimit = 20
	// traceDepthLimit 嵌套深度上限。
	traceDepthLimit = 8
)

// traceSecretKeys 摘要里一律不留值的键（调用方偶尔把凭据放进请求体）。
var traceSecretKeys = map[string]bool{
	"api_key": true, "apikey": true, "authorization": true, "password": true, "secret": true, "token": true,
	"access_token": true, "x-api-key": true,
}

// traceContentKeys 隐藏内容时不留值的键：提示词与正文。
var traceContentKeys = map[string]bool{
	"prompt": true, "input": true, "messages": true, "content": true, "text": true, "query": true,
	"documents": true, "system": true, "instructions": true, "negative_prompt": true, "image": true,
	"mask": true, "images": true, "data": true, "choices": true, "output": true, "results": true,
}

// SanitizeOptions 摘要选项。
type SanitizeOptions struct {
	// HideContent 不留提示词与正文，只留参数与长度（设置 redactContent）。
	HideContent bool
}

// TruncateText 截断到 limit 个字，超出时注明原长。
func TruncateText(text string, limit int) string {
	// jsonb 不收 \u0000。
	text = strings.ReplaceAll(text, "\x00", "")
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + fmt.Sprintf("…（共 %d 字）", len(runes))
}

// HiddenText 隐藏内容时的占位。
func HiddenText(text string) string {
	return fmt.Sprintf("[已隐藏 %d 字]", utf8.RuneCountInString(text))
}

// looksLikeBase64 长串且只含 base64 字符：生成的图片、上传的音频。
func looksLikeBase64(text string) bool {
	if len(text) < 256 {
		return false
	}
	if strings.HasPrefix(text, "data:") {
		return true
	}
	sample := text
	if len(sample) > 512 {
		sample = sample[:512]
	}
	for i := 0; i < len(sample); i++ {
		ch := sample[i]
		if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '+' || ch == '/' || ch == '=' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func base64Placeholder(text string) string {
	size := len(text)
	if index := strings.Index(text, ","); strings.HasPrefix(text, "data:") && index > 0 {
		return fmt.Sprintf("[%s · %s]", text[5:strings.IndexAny(text, ";,")], formatTraceBytes(int64(size-index-1)*3/4))
	}
	return "[base64 · " + formatTraceBytes(int64(size)*3/4) + "]"
}

func formatTraceBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// SanitizeValue 把一个已解码的 JSON 值做成摘要：长文本截断、base64 换占位、长数组只留前几项、凭据去掉。
func SanitizeValue(value any, opts SanitizeOptions) any {
	return sanitizeValue(value, "", opts, 0)
}

func sanitizeValue(value any, key string, opts SanitizeOptions, depth int) any {
	lower := strings.ToLower(key)
	if traceSecretKeys[lower] {
		return "[已移除]"
	}
	if depth > traceDepthLimit {
		return "[…]"
	}
	switch typed := value.(type) {
	case string:
		if looksLikeBase64(typed) {
			return base64Placeholder(typed)
		}
		if opts.HideContent && (traceContentKeys[lower] || utf8.RuneCountInString(typed) > 64) {
			return HiddenText(typed)
		}
		return TruncateText(typed, traceStringLimit)
	case []any:
		if len(typed) > 0 && isNumberArray(typed) && len(typed) > traceArrayLimit {
			return fmt.Sprintf("[%d 个数]", len(typed))
		}
		limit := min(len(typed), traceArrayLimit)
		out := make([]any, 0, limit+1)
		for _, item := range typed[:limit] {
			out = append(out, sanitizeValue(item, key, opts, depth+1))
		}
		if len(typed) > limit {
			out = append(out, fmt.Sprintf("…（共 %d 项）", len(typed)))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, child := range typed {
			out[childKey] = sanitizeValue(child, childKey, opts, depth+1)
		}
		return out
	}
	return value
}

func isNumberArray(values []any) bool {
	for _, value := range values[:min(len(values), 4)] {
		if _, ok := value.(float64); !ok {
			if _, ok := value.(json.Number); !ok {
				return false
			}
		}
	}
	return true
}

// SanitizeJSON 解码一段 JSON 再做摘要；不是 JSON 时只记类型与大小。
func SanitizeJSON(raw []byte, contentType string, opts SanitizeOptions) TraceContent {
	trimmed := strings.TrimSpace(string(raw[:min(len(raw), 1)]))
	if len(raw) > 0 && (trimmed == "{" || trimmed == "[") {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			sanitized := SanitizeValue(decoded, opts)
			if object, ok := sanitized.(map[string]any); ok {
				return object
			}
			return TraceContent{"body": sanitized}
		}
	}
	out := TraceContent{"bytes": len(raw)}
	if contentType != "" {
		out["contentType"] = contentType
	}
	if strings.HasPrefix(contentType, "text/") && len(raw) > 0 {
		text := string(raw)
		if opts.HideContent {
			out["text"] = HiddenText(text)
		} else {
			out["text"] = TruncateText(text, TraceTextLimit)
		}
	}
	return out
}

// SanitizeMap 对一个对象（解码后的请求体）做摘要。
func SanitizeMap(payload map[string]json.RawMessage, opts SanitizeOptions) TraceContent {
	out := TraceContent{}
	for key, raw := range payload {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			continue
		}
		out[key] = sanitizeValue(decoded, key, opts, 1)
	}
	return out
}

// RateLimitHeader 一个响应头是否是限流余量。
func RateLimitHeader(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "x-ratelimit-") || strings.HasPrefix(lower, "anthropic-ratelimit-") ||
		lower == "retry-after"
}

// UpstreamRequestIDHeaders 各家放请求编号的响应头，按优先级。
var UpstreamRequestIDHeaders = []string{"x-request-id", "request-id", "x-amzn-requestid", "apim-request-id",
	"x-goog-request-id", "cf-ray", "x-trace-id", "trace-id"}

// ── 实时概况 ──

// LiveSummary 最近一段时间的调用概况（调用记录页顶部，随实时事件刷新）。
type LiveSummary struct {
	Minutes  int   `json:"minutes"`
	InFlight int64 `json:"inFlight"`
	Total    int64 `json:"total"`
	// Succeeded 含中途断开（aborted）与之后退款的；Failed 含超时放回。
	Succeeded       int64           `json:"succeeded"`
	Failed          int64           `json:"failed"`
	Retried         int64           `json:"retried"`
	Users           int64           `json:"users"`
	InputTokens     int64           `json:"inputTokens"`
	OutputTokens    int64           `json:"outputTokens"`
	Charged         decimal.Decimal `json:"charged"`
	AvgLatencyMs    int             `json:"avgLatencyMs"`
	P95LatencyMs    int             `json:"p95LatencyMs"`
	AvgFirstTokenMs int             `json:"avgFirstTokenMs"`
	// Upstreams 按通道汇总每一次上游尝试（含被回退掉的）。
	Upstreams []UpstreamSummary `json:"upstreams"`
}

// UpstreamSummary 一条通道在这段时间里的上游尝试。
type UpstreamSummary struct {
	ConfigID     int64  `json:"configId"`
	ConfigName   string `json:"configName"`
	Provider     string `json:"provider"`
	Attempts     int64  `json:"attempts"`
	Failed       int64  `json:"failed"`
	AvgLatencyMs int    `json:"avgLatencyMs"`
	LastError    string `json:"lastError,omitempty"`
}
