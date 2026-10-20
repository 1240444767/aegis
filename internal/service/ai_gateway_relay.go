package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 中转调用的编排：暂扣 → 调上游 → 结算或放回。
//
// 计费口径（docs/ai-gateway.md「什么时候扣费」）：
//   - 上游成功：按上游报回的用量结算；上游没报用量时按内容估算，记录上标「用量为估算」。
//   - 上游失败、网关出错、客户端在拿到任何内容之前断开：整笔放回，不扣费。
//   - 流式输出到一半**客户端**断开：已经生成并交付的内容按估算的输出 token 计费（状态 aborted）。
//     上游中途失败则仍按失败处理、不扣费。区分两者是为了不让「快结束时主动断开」变成白用。
//
// 暂扣与结算都用脱离请求上下文的 ctx：客户端断开会取消请求上下文，而放回暂扣恰恰发生在那之后。

// AIGatewayChatInput 一次对话中转。
type AIGatewayChatInput struct {
	Endpoint       string
	Request        aidomain.ChatRequest
	Stream         bool
	IdempotencyKey string
	// OnReserved 暂扣成功、开始调用上游之前回调（处理器在这里写响应头）。
	OnReserved func(requestNo string)
}

// AIGatewayChatResult 对话中转的结论。Response.Model 已换回对外的模型名。
type AIGatewayChatResult struct {
	Response  *aidomain.ChatResponse
	RequestNo string
	Record    *aigateway.Request
}

// AIGatewayPassthroughInput 一次原样转发。
type AIGatewayPassthroughInput struct {
	Endpoint string
	// Path 相对上游端点根地址的路径，如 "images/generations"。
	Path  string
	Model string
	// Build 用上游模型名组装请求体（每个模型的上游名可能与对外名不同）。
	Build    func(upstreamModel string) (body []byte, contentType string, err error)
	Estimate aigateway.Meters
	// Meter 从上游响应里读用量；返回 error 表示响应不可用，按失败处理、不扣费。
	Meter            func(contentType string, body []byte) (aigateway.Meters, bool, error)
	MaxResponseBytes int64
	IdempotencyKey   string
	OnReserved       func(requestNo string)
}

// AIGatewayPassthroughResult 原样转发的结论。
type AIGatewayPassthroughResult struct {
	ContentType string
	Body        []byte
	RequestNo   string
	Record      *aigateway.Request
}

func (s *AIGatewayService) reserve(ctx context.Context, principal *AIGatewayPrincipal, model *aigateway.Model,
	endpoint string, stream bool, estimate aigateway.Meters, clampable bool, idempotencyKey string) (*aigateway.ReserveResult, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) > 128 {
		return nil, aiGatewayError(40223, http.StatusBadRequest, "Idempotency-Key 不能超过 128 个字符")
	}
	result, err := s.pg.ReserveAIGatewayRequest(context.WithoutCancel(ctx), aigateway.ReserveInput{
		AppID: principal.AppID, UserID: principal.UserID, Key: principal.Key, AuthKind: principal.AuthKind,
		Endpoint: endpoint, Stream: stream, Settings: principal.Settings, Model: *model, Estimate: estimate,
		Clampable: clampable, IdempotencyKey: idempotencyKey, ClientIP: principal.ClientIP,
		UserAgent: principal.UserAgent, Now: s.now(),
	})
	if err != nil {
		return nil, mapAIGatewayError(err)
	}
	return result, nil
}

// release 失败放回。放回本身失败只记日志：暂扣会被后台循环按过期放回，结论一样是不扣费。
func (s *AIGatewayService) release(ctx context.Context, requestNo string, cause error, channel *resolvedChannel, started time.Time) {
	input := aigateway.ReleaseInput{
		RequestNo: requestNo, Status: aigateway.StatusFailed, LatencyMs: elapsedMs(started),
		ErrorCode: aiGatewayErrorCode(cause), ErrorMessage: aiGatewayErrorMessage(cause),
	}
	if channel != nil {
		input.ConfigID, input.Provider = channel.Config.ID, channel.Config.Provider
	}
	if _, err := s.pg.ReleaseAIGatewayRequest(context.WithoutCancel(ctx), input); err != nil {
		s.log.Error("AI 网关放回暂扣失败，将由后台按过期放回", zap.String("requestNo", requestNo), zap.Error(err))
	}
}

func (s *AIGatewayService) settle(ctx context.Context, requestNo, status string, meters aigateway.Meters, estimated bool,
	channel *resolvedChannel, upstreamModel string, started time.Time) (*aigateway.Request, error) {
	input := aigateway.SettleInput{
		RequestNo: requestNo, Status: status, Meters: meters, UsageEstimated: estimated,
		UpstreamModel: upstreamModel, LatencyMs: elapsedMs(started), Now: s.now(),
	}
	if channel != nil {
		input.ConfigID, input.Provider = channel.Config.ID, channel.Config.Provider
	}
	record, err := s.pg.SettleAIGatewayRequest(context.WithoutCancel(ctx), input)
	if err != nil {
		s.log.Error("AI 网关结算失败，暂扣将由后台按过期放回（不扣费）", zap.String("requestNo", requestNo), zap.Error(err))
		return nil, err
	}
	return record, nil
}

func elapsedMs(started time.Time) int {
	ms := time.Since(started).Milliseconds()
	if ms > 1<<31-1 {
		return 1<<31 - 1
	}
	return int(ms)
}

func aiGatewayErrorCode(err error) string {
	var upstream *aiUpstreamError
	var appErr *apperrors.AppError
	switch {
	case errors.As(err, &upstream):
		if upstream.Status > 0 {
			return "upstream_" + strconv.Itoa(upstream.Status)
		}
		return "upstream_error"
	case errors.As(err, &appErr):
		return strconv.Itoa(appErr.Code)
	case errors.Is(err, context.Canceled):
		return "client_closed"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "error"
}

func aiGatewayErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	if errors.Is(err, context.Canceled) {
		return "客户端在收到结果前断开"
	}
	return err.Error()
}

// RelayChat 对话中转（OpenAI Chat Completions 与 Anthropic Messages 共用）。
func (s *AIGatewayService) RelayChat(ctx context.Context, principal *AIGatewayPrincipal, in AIGatewayChatInput,
	onEvent func(aidomain.StreamEvent) error) (*AIGatewayChatResult, error) {
	model, err := s.resolveModel(ctx, principal, in.Request.Model, in.Endpoint)
	if err != nil {
		return nil, err
	}
	request := in.Request
	request.Model = model.Upstream()
	if model.MaxOutputTokens > 0 && (request.MaxTokens <= 0 || request.MaxTokens > model.MaxOutputTokens) {
		request.MaxTokens = model.MaxOutputTokens
	}
	outputEstimate := int64(request.MaxTokens)
	if outputEstimate <= 0 {
		outputEstimate = int64(principal.Settings.DefaultMaxTokens)
	}
	estimate := aigateway.Meters{InputTokens: aigateway.EstimateChatInput(request), OutputTokens: outputEstimate}
	reserved, err := s.reserve(ctx, principal, model, in.Endpoint, in.Stream, estimate, true, in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if reserved.ClampMaxTokens > 0 {
		request.MaxTokens = reserved.ClampMaxTokens
	}
	if in.OnReserved != nil {
		in.OnReserved(reserved.RequestNo)
	}

	started := time.Now()
	// 流式：累计已经交付给客户端的内容，客户端中途断开时按它计费。
	var delivered aidomain.ChatResponse
	toolArgs := map[string]int{}
	clientGone := false
	var wrapped func(aidomain.StreamEvent) error
	if in.Stream {
		wrapped = func(event aidomain.StreamEvent) error {
			if err := onEvent(event); err != nil {
				clientGone = true
				return err
			}
			switch event.Type {
			case aidomain.StreamText:
				delivered.Text += event.Delta
			case aidomain.StreamReasoning:
				delivered.Reasoning += event.Delta
			case aidomain.StreamToolStart:
				toolArgs[event.ToolID] = len(delivered.ToolCalls)
				delivered.ToolCalls = append(delivered.ToolCalls, aidomain.ToolCall{ID: event.ToolID, Name: event.ToolName})
			case aidomain.StreamToolDelta:
				if index, ok := toolArgs[event.ToolID]; ok {
					delivered.ToolCalls[index].Arguments = append(delivered.ToolCalls[index].Arguments, event.Delta...)
				}
			}
			return nil
		}
	}

	response, channel, err := s.ai.GatewayDispatch(ctx, principal.AppID, model.ConfigID, request, wrapped)
	if err != nil {
		aborted := in.Stream && (clientGone || ctx.Err() != nil)
		if output := aigateway.EstimateChatOutput(&delivered); aborted && output > 0 {
			meters := aigateway.Meters{InputTokens: estimate.InputTokens, OutputTokens: output}
			_, _ = s.settle(ctx, reserved.RequestNo, aigateway.StatusAborted, meters, true, channel, request.Model, started)
			return nil, err
		}
		s.release(ctx, reserved.RequestNo, err, channel, started)
		return nil, err
	}

	meters := aigateway.Meters{InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens}
	estimated := false
	if meters.InputTokens <= 0 && meters.OutputTokens <= 0 {
		meters = aigateway.Meters{InputTokens: estimate.InputTokens, OutputTokens: aigateway.EstimateChatOutput(response)}
		estimated = true
	}
	upstreamModel := response.Model
	if upstreamModel == "" {
		upstreamModel = request.Model
	}
	record, err := s.settle(ctx, reserved.RequestNo, aigateway.StatusSucceeded, meters, estimated, channel, upstreamModel, started)
	if err != nil && !in.Stream {
		// 非流式还没交付：记不上账就不交付，暂扣由后台放回（不扣费）。
		return nil, apperrors.New(50000, http.StatusInternalServerError, "计费失败，本次调用不扣费，请重试")
	}
	response.Model = model.Model
	return &AIGatewayChatResult{Response: response, RequestNo: reserved.RequestNo, Record: record}, nil
}

// RelayPassthrough 原样转发（图像、嵌入、语音、重排）。
func (s *AIGatewayService) RelayPassthrough(ctx context.Context, principal *AIGatewayPrincipal, in AIGatewayPassthroughInput) (*AIGatewayPassthroughResult, error) {
	model, err := s.resolveModel(ctx, principal, in.Model, in.Endpoint)
	if err != nil {
		return nil, err
	}
	body, contentType, err := in.Build(model.Upstream())
	if err != nil {
		return nil, aiGatewayError(40223, http.StatusBadRequest, "请求体不合法："+err.Error())
	}
	reserved, err := s.reserve(ctx, principal, model, in.Endpoint, false, in.Estimate, false, in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if in.OnReserved != nil {
		in.OnReserved(reserved.RequestNo)
	}

	started := time.Now()
	response, err := s.ai.Passthrough(ctx, principal.AppID, model.ConfigID, aiPassthroughRequest{
		Path: in.Path, Model: model.Upstream(), ContentType: contentType, Body: body, MaxResponseBytes: in.MaxResponseBytes,
	})
	var channel *resolvedChannel
	if response != nil {
		channel = response.Channel
	}
	if err != nil {
		s.release(ctx, reserved.RequestNo, err, channel, started)
		return nil, err
	}
	meters, estimated, err := in.Meter(response.ContentType, response.Body)
	if err != nil {
		cause := apperrors.New(50201, http.StatusBadGateway, "上游返回了无法使用的结果："+err.Error())
		s.release(ctx, reserved.RequestNo, cause, channel, started)
		return nil, cause
	}
	record, err := s.settle(ctx, reserved.RequestNo, aigateway.StatusSucceeded, meters, estimated, channel, model.Upstream(), started)
	if err != nil {
		return nil, apperrors.New(50000, http.StatusInternalServerError, "计费失败，本次调用不扣费，请重试")
	}
	return &AIGatewayPassthroughResult{ContentType: response.ContentType, Body: response.Body,
		RequestNo: reserved.RequestNo, Record: record}, nil
}
