package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	apperrors "aegis/pkg/errors"

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

// passthrough 向一条通道发一次请求。非 2xx 收敛成 aiUpstreamError（与 SDK 中间件同一套规则）。
func (c *aiLLMClient) passthrough(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta,
	req aiPassthroughRequest) (int, string, []byte, error) {
	base := c.baseURL(cfg, meta)
	key := cfg.Secret(aidomain.KeyAPIKey)
	endpoint := base + "/" + strings.TrimLeft(req.Path, "/")
	headers := http.Header{}
	if cfg.Provider == aidomain.ProviderAzureOpenAI {
		version := cfg.Setting(aidomain.KeyAPIVersion)
		if version == "" {
			version = "2024-10-21"
		}
		endpoint = base + "/openai/deployments/" + url.PathEscape(req.Model) + "/" + strings.TrimLeft(req.Path, "/") +
			"?api-version=" + url.QueryEscape(version)
		if key != "" {
			headers.Set("api-key", key)
		}
	} else if key != "" {
		headers.Set("Authorization", "Bearer "+key)
	}
	for headerKey, headerValue := range cfg.SettingMap(aidomain.KeyExtraHeaders) {
		if headerKey = strings.TrimSpace(headerKey); headerKey != "" {
			headers.Set(headerKey, headerValue)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, aiPassthroughTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(req.Body))
	if err != nil {
		return 0, "", nil, &aiUpstreamError{Provider: meta.Provider, Message: err.Error()}
	}
	httpReq.Header = headers
	httpReq.Header.Set("Content-Type", req.ContentType)
	response, err := c.httpClient(meta.Provider).Do(httpReq)
	if err != nil {
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, "", nil, err
		}
		return 0, "", nil, &aiUpstreamError{Provider: meta.Provider, Message: err.Error(), Retryable: true}
	}
	defer response.Body.Close()
	limit := req.MaxResponseBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		message := parseUpstreamErrorMessage(body)
		if message == "" {
			message = summarizeOpaqueUpstreamBody(body, response.Status)
		}
		if message == "" {
			message = response.Status
		}
		return 0, "", nil, &aiUpstreamError{
			Provider: meta.Provider, Status: response.StatusCode, Message: message,
			Retryable: response.StatusCode != http.StatusBadRequest,
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
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
		status, contentType, body, err := s.client.passthrough(ctx, channel.Config, channel.Meta, req)
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
