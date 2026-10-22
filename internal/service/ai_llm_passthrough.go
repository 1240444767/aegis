package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	apperrors "aegis/pkg/errors"

	openaioption "github.com/openai/openai-go/v3/option"

	"go.uber.org/zap"
)

// 原样转发：对话以外的 OpenAI 兼容接口（图像、嵌入、语音、重排）。
//
// 这些接口的请求与响应形状在各家兼容站之间高度一致，网关不需要像对话那样做协议互转，
// 只替换模型名、换上通道的鉴权就转发；响应体原样交回，由调用方从里面读用量计费。
// 只有 OpenAI 协议的通道能接（含 Azure，它的路径形状单独拼）；Anthropic / Gemini 原生协议没有这些接口。

// aiPassthroughTimeout 单次转发的上限。图像生成与长音频转写可能要一两分钟。
const aiPassthroughTimeout = 10 * time.Minute

// aiPassthroughRequest 一次转发。Body 已经换好了上游模型名。
type aiPassthroughRequest struct {
	// Path 相对端点根地址的路径，如 "images/generations"。
	Path        string
	Model       string
	ContentType string
	Body        []byte
	// MaxResponseBytes 响应体上限；超出视为失败（不计费）。
	MaxResponseBytes int64
}

// aiPassthroughResponse 上游 2xx 的响应。
type aiPassthroughResponse struct {
	Status      int
	ContentType string
	Body        []byte
	Channel     *resolvedChannel
}

// passthrough 向一条通道发一次请求，走 openai-go 的请求管线：通道的地址、鉴权（含 Azure 的部署路径、
// api-version 与 api-key 头）、附加请求头与对话完全同一套（openAIClient），非 2xx 由
// upstreamErrorMiddleware 收敛成 aiUpstreamError。SDK 只负责发请求，请求体与响应体都是原样字节：
// 网关不解析这些接口的结构，只从响应里读用量。
func (c *aiLLMClient) passthrough(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta,
	req aiPassthroughRequest) (int, string, []byte, error) {
	client := c.openAIClient(cfg, meta, req.Model)
	ctx, cancel := context.WithTimeout(ctx, aiPassthroughTimeout)
	defer cancel()

	var response *http.Response
	err := client.Execute(ctx, http.MethodPost, strings.TrimLeft(req.Path, "/"), nil, nil,
		openaioption.WithRequestBody(req.ContentType, req.Body),
		openaioption.WithResponseBodyInto(&response))
	if err != nil {
		var upstream *aiUpstreamError
		if errors.As(err, &upstream) {
			return 0, "", nil, upstream
		}
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, "", nil, err
		}
		return 0, "", nil, &aiUpstreamError{Provider: meta.Provider, Message: err.Error(), Retryable: true}
	}
	if response == nil || response.Body == nil {
		return 0, "", nil, &aiUpstreamError{Provider: meta.Provider, Message: "上游没有返回响应", Retryable: true}
	}
	defer response.Body.Close()
	limit := req.MaxResponseBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, "", nil, err
		}
		return 0, "", nil, &aiUpstreamError{Provider: meta.Provider, Message: "读取响应失败：" + err.Error(), Retryable: true}
	}
	if int64(len(body)) > limit {
		return 0, "", nil, &aiUpstreamError{Provider: meta.Provider,
			Message: fmt.Sprintf("上游响应超过 %d MB 上限", limit>>20)}
	}
	return response.StatusCode, response.Header.Get("Content-Type"), body, nil
}

// pinnedChannel 钉死一条通道：应用作用域找不到时再看平台共享（与 dispatch 同一口径）。
func (s *AIProviderService) pinnedChannel(ctx context.Context, appID, configID int64) (*resolvedChannel, error) {
	config, err := s.loadConfig(ctx, appID, configID)
	if err != nil {
		if appID == aidomain.PlatformAppID {
			return nil, err
		}
		shared, sharedErr := s.loadSharedConfig(ctx, configID)
		if sharedErr != nil {
			return nil, err
		}
		config = shared
	}
	if !config.Enabled {
		return nil, apperrors.New(40513, http.StatusFailedDependency, "指定的 AI 通道已停用")
	}
	meta, ok := aidomain.ProviderByKey(config.Provider)
	if !ok {
		return nil, apperrors.New(40511, http.StatusBadRequest, "不支持的 AI 供应商："+config.Provider)
	}
	return &resolvedChannel{Config: *config, Meta: meta, Inherited: config.IsPlatform() && appID != aidomain.PlatformAppID}, nil
}

// ChannelExists 某条通道对这个应用是否可用（应用自有或平台共享）。模型钉通道前校验用。
func (s *AIProviderService) ChannelExists(ctx context.Context, appID, configID int64) error {
	_, err := s.pinnedChannel(ctx, appID, configID)
	return err
}

// Passthrough 带链路回退的原样转发：只走 OpenAI 协议的通道，可重试的失败换下一条。
func (s *AIProviderService) Passthrough(ctx context.Context, appID, configID int64, req aiPassthroughRequest) (*aiPassthroughResponse, error) {
	if err := s.ensureUsable(ctx, appID); err != nil {
		return nil, err
	}
	var chain []resolvedChannel
	if configID > 0 {
		channel, err := s.pinnedChannel(ctx, appID, configID)
		if err != nil {
			return nil, err
		}
		chain = []resolvedChannel{*channel}
	} else {
		resolved, err := s.ResolveChain(ctx, appID, req.Model)
		if err != nil {
			return nil, err
		}
		chain = resolved
	}
	usable := chain[:0:0]
	for _, channel := range chain {
		if channel.Meta.Protocol == aidomain.ProtocolOpenAI {
			usable = append(usable, channel)
		}
	}
	if len(usable) == 0 {
		return nil, apperrors.New(40222, http.StatusFailedDependency,
			"没有支持该接口的 AI 通道：图像、嵌入、语音与重排只能走 OpenAI 兼容协议的通道")
	}

	var lastErr error
	for i := range usable {
		channel := &usable[i]
		trace := aiTraceFrom(ctx)
		trace.beginAttempt(channel, req.Model)
		status, contentType, body, err := s.client.passthrough(ctx, channel.Config, channel.Meta, req)
		trace.endAttempt(ctx, err)
		if err == nil {
			return &aiPassthroughResponse{Status: status, ContentType: contentType, Body: body, Channel: channel}, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, err
		}
		var upstream *aiUpstreamError
		if !errors.As(err, &upstream) || !upstream.Retryable {
			return &aiPassthroughResponse{Channel: channel}, err
		}
		s.log.Warn("AI 通道转发失败，尝试链路里的下一条",
			zap.Int64("appid", appID), zap.String("config", channel.Config.Name),
			zap.String("provider", channel.Config.Provider), zap.String("path", req.Path), zap.Error(err))
	}
	return nil, lastErr
}

// GatewayDispatch 网关的对话调用面：可钉通道、返回实际使用的通道（计费记录要它）。
func (s *AIProviderService) GatewayDispatch(ctx context.Context, appID, configID int64, request aidomain.ChatRequest,
	onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, *resolvedChannel, error) {
	return s.dispatch(ctx, aiChatArgs{AppID: appID, ConfigID: configID, Request: request}, onEvent)
}
