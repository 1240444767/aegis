package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// aiGatewayCall 一次中转调用的追踪与实时推送：暂扣之后建立，结算或放回之后收尾。
type aiGatewayCall struct {
	s         *AIGatewayService
	appID     int64
	requestNo string
	account   string
	hide      bool
	trace     *aiTrace
	request   aigateway.TraceContent
	// requestBytes 调用方请求体的大小。
	requestBytes int64
	lastProgress atomic.Int64
}

// AttachLiveBus 接上 NATS，让调用记录的实时事件跨实例可见。
func (s *AIGatewayService) AttachLiveBus(conn *nats.Conn) {
	s.live.attach(conn)
}

// SubscribeLive 订阅一个应用的调用记录实时事件（管理端 SSE）。返回的 cancel 必须调用。
func (s *AIGatewayService) SubscribeLive(appID int64) (<-chan AIGatewayLiveEvent, func()) {
	return s.live.subscribe(appID)
}

// beginCall 暂扣成功后建立追踪，推 created 事件。返回带追踪的 ctx，上游调用都要用它。
func (s *AIGatewayService) beginCall(ctx context.Context, principal *AIGatewayPrincipal, requestNo string,
	request aigateway.TraceContent, requestBytes int64) (context.Context, *aiGatewayCall) {
	call := &aiGatewayCall{s: s, appID: principal.AppID, requestNo: requestNo, hide: principal.Settings.RedactContent,
		request: request, requestBytes: requestBytes}
	call.trace = newAITrace(call.onTrace)
	if record, err := s.pg.GetAIGatewayRequest(context.WithoutCancel(ctx), principal.AppID, 0, requestNo); err == nil {
		call.account = record.Account
		record.Live = call.trace.progress()
		s.live.publish(AIGatewayLiveEvent{Type: AIGatewayLiveCreated, AppID: call.appID, RequestNo: requestNo, Request: record})
	}
	return withAITrace(ctx, call.trace), call
}

func (c *aiGatewayCall) onTrace(kind string) {
	if kind == "first-token" {
		c.publishProgress()
		return
	}
	c.s.live.publish(AIGatewayLiveEvent{Type: AIGatewayLiveAttempt, AppID: c.appID, RequestNo: c.requestNo,
		Attempts: c.trace.snapshot(), Progress: c.trace.progress()})
}

func (c *aiGatewayCall) publishProgress() {
	c.lastProgress.Store(time.Now().UnixNano())
	c.s.live.publish(AIGatewayLiveEvent{Type: AIGatewayLiveProgress, AppID: c.appID, RequestNo: c.requestNo,
		Progress: c.trace.progress()})
}

// observeStream 流式增量到达：记首字与内容量，至多每秒推一次进度。
func (c *aiGatewayCall) observeStream(event aidomain.StreamEvent) {
	chars := len([]rune(event.Delta))
	if event.Type == aidomain.StreamToolStart {
		chars = len(event.ToolName)
	}
	c.trace.observeOutput(chars)
	if time.Duration(time.Now().UnixNano()-c.lastProgress.Load()) >= aiGatewayProgressInterval {
		c.publishProgress()
	}
}

// finish 收尾：追踪写库，推 finished 事件。record 为空时（结算或放回本身失败）从库里重取。
func (c *aiGatewayCall) finish(ctx context.Context, record *aigateway.Request, response aigateway.TraceContent, finishReason string) {
	ctx = context.WithoutCancel(ctx)
	summary := c.trace.summary()
	summary.FinishReason = finishReason
	summary.RequestBytes = c.requestBytes
	if response == nil {
		response = aigateway.TraceContent{}
	}
	request := c.request
	if request == nil {
		request = aigateway.TraceContent{}
	}
	trace := aigateway.Trace{Attempts: c.trace.snapshot(), Request: request, Response: response}
	if err := c.s.pg.SaveAIGatewayTrace(ctx, c.requestNo, summary, trace); err != nil {
		c.s.log.Warn("AI 网关调用追踪写入失败", zap.String("requestNo", c.requestNo), zap.Error(err))
	}
	if record == nil {
		fetched, err := c.s.pg.GetAIGatewayRequest(ctx, c.appID, 0, c.requestNo)
		if err != nil {
			return
		}
		record = fetched
	}
	copied := *record
	copied.TraceSummary = summary
	if copied.Account == "" {
		copied.Account = c.account
	}
	c.s.live.publish(AIGatewayLiveEvent{Type: AIGatewayLiveFinished, AppID: c.appID, RequestNo: c.requestNo,
		Request: &copied, Attempts: trace.Attempts})
}

// publishFinished 调用记录在中转之外改变了状态（超时放回、退款）。
func (s *AIGatewayService) publishFinished(record *aigateway.Request) {
	if record == nil {
		return
	}
	s.live.publish(AIGatewayLiveEvent{Type: AIGatewayLiveFinished, AppID: record.AppID, RequestNo: record.RequestNo, Request: record})
}

// ── 内容摘要 ──

// chatTraceRequest 对话请求的摘要：参数、系统提示、每条消息（长文截断、图片换占位）、工具名。
func chatTraceRequest(request aidomain.ChatRequest, endpoint string, hide bool) aigateway.TraceContent {
	text := func(value string) string {
		if hide {
			return aigateway.HiddenText(value)
		}
		return aigateway.TruncateText(value, aigateway.TraceTextLimit)
	}
	out := aigateway.TraceContent{"model": request.Model, "endpoint": endpoint, "messageCount": len(request.Messages)}
	if request.MaxTokens > 0 {
		out["maxTokens"] = request.MaxTokens
	}
	if request.Temperature != nil {
		out["temperature"] = *request.Temperature
	}
	if request.TopP != nil {
		out["topP"] = *request.TopP
	}
	if len(request.Stop) > 0 {
		out["stop"] = request.Stop
	}
	if request.JSONMode {
		out["jsonMode"] = true
	}
	if request.ToolChoice != "" {
		out["toolChoice"] = request.ToolChoice
	}
	if request.Reasoning != nil {
		reasoning := map[string]any{}
		if request.Reasoning.Effort != "" {
			reasoning["effort"] = request.Reasoning.Effort
		}
		if request.Reasoning.BudgetTokens > 0 {
			reasoning["budgetTokens"] = request.Reasoning.BudgetTokens
		}
		out["reasoning"] = reasoning
	}
	if request.System != "" {
		out["system"] = text(request.System)
	}
	if len(request.Tools) > 0 {
		names := make([]string, 0, len(request.Tools))
		for _, tool := range request.Tools {
			names = append(names, tool.Name)
		}
		out["tools"] = names
	}
	// 只留最后 20 条：长对话的历史在排查时用处不大，留着只会撑大追踪。
	messages := request.Messages
	if len(messages) > 20 {
		messages = messages[len(messages)-20:]
		out["messagesOmitted"] = len(request.Messages) - 20
	}
	items := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{"role": message.Role}
		parts := make([]any, 0, len(message.Content))
		for _, part := range message.Content {
			switch part.Type {
			case aidomain.PartImage:
				image := part.ImageURL
				if s, ok := aigateway.SanitizeValue(image, aigateway.SanitizeOptions{}).(string); ok {
					image = s
				}
				parts = append(parts, map[string]any{"type": "image", "url": aigateway.TruncateText(image, 200)})
			default:
				parts = append(parts, map[string]any{"type": "text", "text": text(part.Text)})
			}
		}
		if len(parts) == 1 {
			if part, ok := parts[0].(map[string]any); ok && part["type"] == "text" {
				item["content"] = part["text"]
			} else {
				item["content"] = parts
			}
		} else if len(parts) > 0 {
			item["content"] = parts
		}
		if len(message.ToolCalls) > 0 {
			item["toolCalls"] = traceToolCalls(message.ToolCalls, hide)
		}
		if message.ToolCallID != "" {
			item["toolCallId"] = message.ToolCallID
		}
		items = append(items, item)
	}
	out["messages"] = items
	return out
}

// chatTraceResponse 对话结果的摘要。partial 为真表示客户端中途断开、只有已生成的部分。
func chatTraceResponse(response *aidomain.ChatResponse, hide, partial bool) aigateway.TraceContent {
	if response == nil {
		return aigateway.TraceContent{}
	}
	text := func(value string) string {
		if hide {
			return aigateway.HiddenText(value)
		}
		return aigateway.TruncateText(value, aigateway.TraceTextLimit)
	}
	out := aigateway.TraceContent{}
	if response.ID != "" {
		out["id"] = response.ID
	}
	if response.Model != "" {
		out["model"] = response.Model
	}
	if response.Text != "" {
		out["text"] = text(response.Text)
	}
	if response.Reasoning != "" {
		out["reasoning"] = text(response.Reasoning)
	}
	if len(response.ToolCalls) > 0 {
		out["toolCalls"] = traceToolCalls(response.ToolCalls, hide)
	}
	if response.FinishReason != "" {
		out["finishReason"] = response.FinishReason
	}
	if response.Usage.InputTokens > 0 || response.Usage.OutputTokens > 0 {
		out["usage"] = response.Usage
	}
	if partial {
		out["partial"] = true
	}
	return out
}

func traceToolCalls(calls []aidomain.ToolCall, hide bool) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		item := map[string]any{"id": call.ID, "name": call.Name}
		if len(call.Arguments) > 0 {
			var decoded any
			if json.Unmarshal(call.Arguments, &decoded) == nil {
				item["arguments"] = aigateway.SanitizeValue(decoded, aigateway.SanitizeOptions{HideContent: hide})
			} else if hide {
				item["arguments"] = aigateway.HiddenText(string(call.Arguments))
			} else {
				item["arguments"] = aigateway.TruncateText(string(call.Arguments), aigateway.TraceTextLimit)
			}
		}
		out = append(out, item)
	}
	return out
}
