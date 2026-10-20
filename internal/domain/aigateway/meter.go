package aigateway

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// 从上游响应里读用量。
//
// 每个函数返回 (计量, 是否为估算)。上游报了用量就以上游为准；没报时按请求估算并标记，
// 调用记录上会显示「用量为估算」。返回 error 的表示响应不可用（例如图像接口一张图都没给），
// 网关按失败处理、整笔放回。

// ErrEmptyResult 上游返回了 2xx 但没有任何可用结果。
var ErrEmptyResult = errors.New("上游没有返回任何结果")

type openAIUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	// 转写接口：{"type": "duration", "seconds": 12.3} 或 {"type": "tokens", ...}
	Type    string  `json:"type"`
	Seconds float64 `json:"seconds"`
}

func (u *openAIUsage) input() int64 {
	if u == nil {
		return 0
	}
	if u.InputTokens > 0 {
		return u.InputTokens
	}
	if u.PromptTokens > 0 {
		return u.PromptTokens
	}
	if u.OutputTokens == 0 && u.CompletionTokens == 0 {
		return u.TotalTokens
	}
	return 0
}

func (u *openAIUsage) output() int64 {
	if u == nil {
		return 0
	}
	if u.OutputTokens > 0 {
		return u.OutputTokens
	}
	return u.CompletionTokens
}

// MeterEmbeddings 嵌入：usage.prompt_tokens。
func MeterEmbeddings(body []byte, estimate Meters) (Meters, bool, error) {
	var payload struct {
		Data  []json.RawMessage `json:"data"`
		Usage *openAIUsage      `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Meters{}, false, ErrEmptyResult
	}
	if len(payload.Data) == 0 {
		return Meters{}, false, ErrEmptyResult
	}
	if tokens := payload.Usage.input(); tokens > 0 {
		return Meters{InputTokens: tokens}, false, nil
	}
	return Meters{InputTokens: estimate.InputTokens}, true, nil
}

// MeterImages 图像：按实际返回的张数计；gpt-image 系列另报 token 用量，有就一并计入。
func MeterImages(body []byte, size, quality string) (Meters, bool, error) {
	var payload struct {
		Data  []json.RawMessage `json:"data"`
		Usage *openAIUsage      `json:"usage"`
		// 有的兼容站把尺寸写回顶层（请求的方形尺寸会被改成别的值），以它为准。
		Size    string `json:"size"`
		Quality string `json:"quality"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Data) == 0 {
		return Meters{}, false, ErrEmptyResult
	}
	meters := Meters{Images: int64(len(payload.Data)), ImageSize: size, ImageQuality: quality}
	if payload.Size != "" && size == "" {
		meters.ImageSize = payload.Size
	}
	if payload.Quality != "" && quality == "" {
		meters.ImageQuality = payload.Quality
	}
	if payload.Usage != nil {
		meters.InputTokens = payload.Usage.input()
		meters.OutputTokens = payload.Usage.output()
	}
	return meters, false, nil
}

var subtitleTimestamp = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})`)

// MeterTranscription 转写：依次看 usage（token 或时长）、verbose_json 的 duration、字幕最后一个时间戳；
// 都没有时按上传文件估算的时长计，并标记为估算。
func MeterTranscription(contentType string, body []byte, estimate Meters) (Meters, bool, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return Meters{}, false, ErrEmptyResult
	}
	if strings.HasPrefix(trimmed, "{") {
		var payload struct {
			Text     string       `json:"text"`
			Duration float64      `json:"duration"`
			Usage    *openAIUsage `json:"usage"`
		}
		if err := json.Unmarshal(body, &payload); err == nil {
			if payload.Usage != nil {
				if payload.Usage.Type == "duration" && payload.Usage.Seconds > 0 {
					return Meters{AudioMillis: int64(payload.Usage.Seconds * 1000)}, false, nil
				}
				if in, out := payload.Usage.input(), payload.Usage.output(); in > 0 || out > 0 {
					return Meters{InputTokens: in, OutputTokens: out}, false, nil
				}
			}
			if payload.Duration > 0 {
				return Meters{AudioMillis: int64(payload.Duration * 1000)}, false, nil
			}
		}
	}
	matches := subtitleTimestamp.FindAllStringSubmatch(trimmed, -1)
	if len(matches) > 0 {
		last := matches[len(matches)-1]
		hours, _ := strconv.ParseInt(last[1], 10, 64)
		minutes, _ := strconv.ParseInt(last[2], 10, 64)
		seconds, _ := strconv.ParseInt(last[3], 10, 64)
		fraction := last[4]
		for len(fraction) < 3 {
			fraction += "0"
		}
		millis, _ := strconv.ParseInt(fraction, 10, 64)
		total := ((hours*60+minutes)*60+seconds)*1000 + millis
		if total > 0 {
			return Meters{AudioMillis: total}, false, nil
		}
	}
	return Meters{AudioMillis: estimate.AudioMillis}, true, nil
}

// TranscriptionText 从 JSON 转写结果里取正文（客户端要 text 格式而上游给了 json 时用）。
func TranscriptionText(body []byte) (string, bool) {
	var payload struct {
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Text == nil {
		return "", false
	}
	return *payload.Text, true
}

// MeterRerank 重排：Jina / SiliconFlow 的 usage.total_tokens、Cohere 的 meta.billed_units / meta.tokens。
func MeterRerank(body []byte, estimate Meters) (Meters, bool, error) {
	var payload struct {
		Results []json.RawMessage `json:"results"`
		Data    []json.RawMessage `json:"data"`
		Usage   *openAIUsage      `json:"usage"`
		Meta    *struct {
			Tokens *struct {
				InputTokens int64 `json:"input_tokens"`
			} `json:"tokens"`
			BilledUnits *struct {
				InputTokens int64 `json:"input_tokens"`
			} `json:"billed_units"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || (len(payload.Results) == 0 && len(payload.Data) == 0) {
		return Meters{}, false, ErrEmptyResult
	}
	if tokens := payload.Usage.input(); tokens > 0 {
		return Meters{InputTokens: tokens}, false, nil
	}
	if payload.Meta != nil {
		if payload.Meta.Tokens != nil && payload.Meta.Tokens.InputTokens > 0 {
			return Meters{InputTokens: payload.Meta.Tokens.InputTokens}, false, nil
		}
		if payload.Meta.BilledUnits != nil && payload.Meta.BilledUnits.InputTokens > 0 {
			return Meters{InputTokens: payload.Meta.BilledUnits.InputTokens}, false, nil
		}
	}
	return Meters{InputTokens: estimate.InputTokens}, true, nil
}

// EstimateAudioMillis 按上传文件大小估算音频时长：按 32 kbps 算（偏低的码率 → 偏长的时长，宁高勿低），
// 至少 1 分钟。只用于暂扣与上游完全不报时长时的兜底。
func EstimateAudioMillis(bytes int64) int64 {
	return max(bytes/4, 60_000)
}
