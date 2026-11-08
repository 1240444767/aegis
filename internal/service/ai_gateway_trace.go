package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aegis/internal/domain/aigateway"
)

// 调用追踪的采集端（docs/ai-gateway.md「调用追踪」）。
//
// 一次中转调用带一个 aiTrace 放进 ctx：
//   - 链路循环（dispatch / Passthrough）每换一条通道调 beginAttempt / endAttempt；
//   - aiTraceTransport 包在上游 HTTP 客户端外面，从同一个 ctx 取到 aiTrace，
//     把 URL、HTTP 状态、响应头里的请求编号与限流余量、首包耗时、字节数记到当前尝试上；
//   - 中转编排在流式增量到达时调 observeOutput，记首字耗时与已生成的内容量。
//
// ctx 里没有 aiTrace 时（应用函数网关、智能体等其它调用方）以上全是空操作。
// 每次变化都回调 onChange，由网关决定要不要推实时事件。

type aiTraceKey struct{}

type aiTrace struct {
	mu       sync.Mutex
	started  time.Time
	attempts []aigateway.Attempt
	// current 进行中的尝试下标，-1 表示没有。
	current     int
	firstToken  time.Duration
	outputChars int64
	onChange    func(kind string)
}

func newAITrace(onChange func(kind string)) *aiTrace {
	return &aiTrace{started: time.Now(), current: -1, onChange: onChange}
}

func withAITrace(ctx context.Context, trace *aiTrace) context.Context {
	if trace == nil {
		return ctx
	}
	return context.WithValue(ctx, aiTraceKey{}, trace)
}

func aiTraceFrom(ctx context.Context) *aiTrace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(aiTraceKey{}).(*aiTrace)
	return trace
}

func (t *aiTrace) notify(kind string) {
	if t != nil && t.onChange != nil {
		t.onChange(kind)
	}
}

// beginAttempt 链路里的一条通道开始被调用。
func (t *aiTrace) beginAttempt(channel *resolvedChannel, model string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	attempt := aigateway.Attempt{Seq: len(t.attempts) + 1, Model: model, Status: aigateway.AttemptRunning, StartedAt: time.Now()}
	if channel != nil {
		attempt.ConfigID, attempt.ConfigName = channel.Config.ID, channel.Config.Name
		attempt.Provider, attempt.Protocol, attempt.Inherited = channel.Config.Provider, channel.Meta.Protocol, channel.Inherited
	}
	t.attempts = append(t.attempts, attempt)
	t.current = len(t.attempts) - 1
	t.mu.Unlock()
	t.notify("attempt")
}

// endAttempt 当前尝试结束。err 为空即成功。
func (t *aiTrace) endAttempt(ctx context.Context, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.current < 0 {
		t.mu.Unlock()
		return
	}
	attempt := &t.attempts[t.current]
	attempt.LatencyMs = elapsedMs(attempt.StartedAt)
	switch {
	case err == nil:
		attempt.Status = aigateway.AttemptSucceeded
	case ctx != nil && errors.Is(ctx.Err(), context.Canceled):
		attempt.Status = aigateway.AttemptCanceled
		attempt.Error = "客户端断开"
	default:
		attempt.Status = aigateway.AttemptFailed
		attempt.Error = aigateway.TruncateText(aiGatewayErrorMessage(err), 500)
		var upstream *aiUpstreamError
		if errors.As(err, &upstream) {
			attempt.Retryable = upstream.Retryable
			if upstream.Status > 0 {
				attempt.HTTPStatus = upstream.Status
			}
		}
	}
	t.current = -1
	t.mu.Unlock()
	t.notify("attempt")
}

// withCurrent 在当前尝试上改字段，返回它的下标；没有进行中的尝试时（调用方没走链路循环）补开一条。
func (t *aiTrace) withCurrent(update func(*aigateway.Attempt)) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current < 0 {
		t.attempts = append(t.attempts, aigateway.Attempt{Seq: len(t.attempts) + 1, Status: aigateway.AttemptRunning, StartedAt: time.Now()})
		t.current = len(t.attempts) - 1
	}
	update(&t.attempts[t.current])
	return t.current
}

// withAttempt 改指定下标的尝试（响应体可能在尝试结束后才关闭）。
func (t *aiTrace) withAttempt(index int, update func(*aigateway.Attempt)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if index >= 0 && index < len(t.attempts) {
		update(&t.attempts[index])
	}
}

// observeOutput 流式增量到达：记首字耗时（相对调用开始与相对当前尝试）与已生成的内容量。
func (t *aiTrace) observeOutput(chars int) {
	if t == nil {
		return
	}
	first := false
	t.mu.Lock()
	t.outputChars += int64(chars)
	if t.firstToken == 0 {
		t.firstToken = max(time.Since(t.started), time.Millisecond)
		first = true
		if t.current >= 0 {
			attempt := &t.attempts[t.current]
			attempt.FirstTokenMs = max(elapsedMs(attempt.StartedAt), 1)
		}
	}
	t.mu.Unlock()
	if first {
		t.notify("first-token")
	}
}

// snapshot 当前的尝试列表（拷贝）。
func (t *aiTrace) snapshot() []aigateway.Attempt {
	if t == nil {
		return []aigateway.Attempt{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]aigateway.Attempt, len(t.attempts))
	for i, attempt := range t.attempts {
		out[i] = attempt
		if attempt.Status == aigateway.AttemptRunning {
			out[i].LatencyMs = elapsedMs(attempt.StartedAt)
		}
		if attempt.RateLimit != nil {
			out[i].RateLimit = make(map[string]string, len(attempt.RateLimit))
			for key, value := range attempt.RateLimit {
				out[i].RateLimit[key] = value
			}
		}
	}
	return out
}

// progress 进行中的实时进度。
func (t *aiTrace) progress() *aigateway.LiveProgress {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return &aigateway.LiveProgress{
		ElapsedMs:    elapsedMs(t.started),
		FirstTokenMs: int(t.firstToken.Milliseconds()),
		OutputChars:  t.outputChars,
		OutputTokens: (t.outputChars + 2) / 3,
		Attempt:      len(t.attempts),
	}
}

// summary 写进调用记录行的摘要。上游请求编号与响应字节数取最后一次尝试（成功的那次）。
func (t *aiTrace) summary() aigateway.TraceSummary {
	if t == nil {
		return aigateway.TraceSummary{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := aigateway.TraceSummary{Attempts: len(t.attempts), FirstTokenMs: int(t.firstToken.Milliseconds())}
	if n := len(t.attempts); n > 0 {
		last := t.attempts[n-1]
		out.UpstreamRequestID, out.ResponseBytes = last.UpstreamRequestID, last.ResponseBytes
	}
	return out
}

// ── HTTP 层 ──

// aiTraceTransport 包在上游 HTTP 客户端外面，把每一次 HTTP 往返记到 ctx 里的 aiTrace 上。
type aiTraceTransport struct {
	base http.RoundTripper
}

func (t aiTraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	trace := aiTraceFrom(req.Context())
	if trace == nil {
		return t.base.RoundTrip(req)
	}
	sent := time.Now()
	index := trace.withCurrent(func(attempt *aigateway.Attempt) {
		attempt.URL = redactTraceURL(req)
		attempt.Method = req.Method
		if req.ContentLength > 0 {
			attempt.RequestBytes += req.ContentLength
		}
	})
	response, err := t.base.RoundTrip(req)
	if err != nil {
		return response, err
	}
	trace.withAttempt(index, func(attempt *aigateway.Attempt) {
		attempt.HTTPStatus = response.StatusCode
		attempt.HeadersMs = max(int(time.Since(sent).Milliseconds()), 1)
		for _, name := range aigateway.UpstreamRequestIDHeaders {
			if value := strings.TrimSpace(response.Header.Get(name)); value != "" {
				attempt.UpstreamRequestID = aigateway.TruncateText(value, 120)
				break
			}
		}
		for name, values := range response.Header {
			if len(values) > 0 && aigateway.RateLimitHeader(name) {
				if attempt.RateLimit == nil {
					attempt.RateLimit = map[string]string{}
				}
				attempt.RateLimit[strings.ToLower(name)] = values[0]
			}
		}
	})
	trace.notify("headers")
	response.Body = &aiTraceBody{ReadCloser: response.Body, trace: trace, index: index}
	return response, nil
}

// aiTraceBody 数响应体字节数，读完或关闭时记到当前尝试上。
type aiTraceBody struct {
	io.ReadCloser
	trace *aiTrace
	index int
	read  atomic.Int64
	done  atomic.Bool
}

func (b *aiTraceBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read.Add(int64(n))
	if err != nil {
		b.flush()
	}
	return n, err
}

func (b *aiTraceBody) Close() error {
	b.flush()
	return b.ReadCloser.Close()
}

func (b *aiTraceBody) flush() {
	if b.done.Swap(true) {
		return
	}
	read := b.read.Load()
	b.trace.withAttempt(b.index, func(attempt *aigateway.Attempt) { attempt.ResponseBytes += read })
}

// redactTraceURL 只留 scheme、主机与路径。
func redactTraceURL(req *http.Request) string {
	if req.URL == nil {
		return ""
	}
	return req.URL.Scheme + "://" + req.URL.Host + req.URL.EscapedPath()
}
