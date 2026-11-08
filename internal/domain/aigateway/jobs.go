package aigateway

import (
	"encoding/json"
	"time"
)

// 异步任务（docs/ai-gateway.md「异步任务」）：长调用提交后立刻返回任务，客户端轮询取结果。
// 任务编号就是调用编号，计费仍按调用记录走。

// 任务状态（与 OpenAI 的 batch / response 状态措辞一致）。
const (
	JobInProgress = "in_progress"
	JobCompleted  = "completed"
	JobFailed     = "failed"
	JobCanceled   = "canceled"
)

const (
	// JobObject 任务对象的 object 字段。
	JobObject = "aegis.job"
	// JobRunningTTL 进行中的任务超过多久没有结论就视为中断（实例被杀）；比上游调用上限略长。
	JobRunningTTL = 15 * time.Minute
	// JobResultTTL 结束后的结果保留多久。
	JobResultTTL = time.Hour
)

// JobError 失败任务的错误，形状同 OpenAI 错误信封里的 error。
type JobError struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Code      string `json:"code"`
	AegisCode int    `json:"aegis_code,omitempty"`
	// Status 同步调用时会回的 HTTP 状态。
	Status int `json:"status"`
}

// Job 一个异步任务（存储形状）。
type Job struct {
	RequestNo     string
	AppID         int64
	UserID        int64
	Endpoint      string
	Status        string
	HTTPStatus    int
	ContentType   string
	Result        []byte
	Error         *JobError
	CreatedAt     time.Time
	FinishedAt    *time.Time
	ExpiresAt     time.Time
	RequestStatus string
}

// JobView 任务的对外形状。JSON 结果直接内嵌在 result；二进制结果（语音合成）给 result_url。
type JobView struct {
	ID          string          `json:"id"`
	Object      string          `json:"object"`
	Endpoint    string          `json:"endpoint"`
	Status      string          `json:"status"`
	CreatedAt   int64           `json:"created_at"`
	CompletedAt *int64          `json:"completed_at,omitempty"`
	ElapsedMs   int64           `json:"elapsed_ms"`
	Result      json.RawMessage `json:"result,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	ResultURL   string          `json:"result_url,omitempty"`
	Error       *JobError       `json:"error,omitempty"`
	// PollAfterMs 建议的下一次轮询间隔。
	PollAfterMs int `json:"poll_after_ms,omitempty"`
}
