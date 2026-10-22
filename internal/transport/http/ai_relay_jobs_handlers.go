package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	aidomain "aegis/internal/domain/ai"

	"github.com/gin-gonic/gin"
)

// 长调用的两种出路（docs/ai-gateway.md「长调用」）：
//
//   - 异步任务：`Prefer: respond-async` 提交，GET /jobs/{id} 轮询，DELETE 取消。推荐 —— 每个请求都很短，
//     不受任何一层代理的读超时影响，客户端断网重连也不会丢结果。
//   - 同步保活：不改调用方式的 OpenAI SDK 也要能用。调用超过 aiRelayKeepaliveAfter 还没结果时，
//     先回 200 与响应头，之后每 15 秒写一个空格（JSON 允许前导空白），代理看到持续有字节就不会超时断开。
//     代价是此后出错也只能是 200 + 错误信封；请求头 `X-Aegis-Keepalive: off` 可以关掉。

// 变量而不是常量：测试把它们调短。
var (
	aiRelayKeepaliveAfter = 20 * time.Second
	aiRelayKeepaliveEvery = 15 * time.Second
)

// aiRelayKeepaliveEnabled 调用方没有关掉保活。
func aiRelayKeepaliveEnabled(c *gin.Context) bool {
	return !strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Aegis-Keepalive")), "off")
}

// runAIRelayWithKeepalive 在后台跑 run，超过 aiRelayKeepaliveAfter 还没结果就先回头、写空格保活。
//
// 处理器必须等 run 结束才返回：run 里会读 multipart 的临时文件，处理器一返回它们就被删掉。
// 响应头只在这个 goroutine 里写，run 通过 onReserved 交回调用编号，避免两边同时改响应头。
func runAIRelayWithKeepalive(c *gin.Context, protocol string, run func(onReserved func(string)) (string, []byte, error)) {
	type outcome struct {
		contentType string
		body        []byte
		err         error
	}
	done := make(chan outcome, 1)
	var requestNo atomic.Value
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: fmt.Errorf("调用异常：%v", r)}
			}
		}()
		contentType, body, err := run(func(no string) { requestNo.Store(no) })
		done <- outcome{contentType, body, err}
	}()
	setRequestNo := func() {
		if no, ok := requestNo.Load().(string); ok && no != "" {
			c.Header("X-Aegis-Request-No", no)
		}
	}

	timer := time.NewTimer(aiRelayKeepaliveAfter)
	defer timer.Stop()
	select {
	case out := <-done:
		setRequestNo()
		if out.err != nil {
			writeAIRelayError(c, protocol, out.err)
			return
		}
		c.Data(http.StatusOK, out.contentType, out.body)
		return
	case <-timer.C:
	}

	setRequestNo()
	header := c.Writer.Header()
	header.Set("Content-Type", "application/json; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	header.Set("X-Aegis-Keepalive", "whitespace")
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	c.Status(http.StatusOK)
	c.Writer.WriteHeaderNow()
	c.Writer.Flush()

	ticker := time.NewTicker(aiRelayKeepaliveEvery)
	defer ticker.Stop()
	gone := c.Request.Context().Done()
	for {
		select {
		case out := <-done:
			body := out.body
			if out.err != nil {
				body, _ = json.Marshal(aiRelayErrorBody(protocol, aiRelayErrorOf(out.err)))
			}
			_, _ = c.Writer.Write(body)
			c.Writer.Flush()
			return
		case <-ticker.C:
			if gone == nil {
				continue
			}
			if _, err := c.Writer.WriteString(" "); err != nil {
				gone = nil
				continue
			}
			c.Writer.Flush()
		case <-gone:
			// 客户端走了：不再写，但要等 run 收尾（它会因请求上下文取消而放回暂扣）。
			gone = nil
		}
	}
}

// ── 异步任务 ──

// AIRelayJob GET /api/ai/v1/jobs/:jobId?wait=20 —— 取任务；wait（秒，≤25）长轮询。
func (h *Handler) AIRelayJob(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	wait, _ := strconv.Atoi(c.Query("wait"))
	job, err := h.aiGateway.GetJob(c.Request.Context(), principal, c.Param("jobId"), time.Duration(wait)*time.Second)
	if err != nil {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, err)
		return
	}
	c.Header("X-Aegis-Request-No", job.ID)
	c.JSON(http.StatusOK, job)
}

// AIRelayJobContent GET /api/ai/v1/jobs/:jobId/content —— 已完成任务的结果原样（语音合成的音频等）。
func (h *Handler) AIRelayJobContent(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	job, err := h.aiGateway.JobContent(c.Request.Context(), principal, c.Param("jobId"))
	if err != nil {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, err)
		return
	}
	contentType := job.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("X-Aegis-Request-No", job.RequestNo)
	c.Data(http.StatusOK, contentType, job.Result)
}

// AIRelayCancelJob DELETE /api/ai/v1/jobs/:jobId —— 取消进行中的任务（不扣费）；已结束的原样返回。
func (h *Handler) AIRelayCancelJob(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	job, err := h.aiGateway.CancelJob(c.Request.Context(), principal, c.Param("jobId"))
	if err != nil {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, err)
		return
	}
	c.Header("X-Aegis-Request-No", job.ID)
	c.JSON(http.StatusOK, job)
}
