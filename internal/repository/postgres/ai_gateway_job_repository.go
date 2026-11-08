package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"aegis/internal/domain/aigateway"

	"github.com/jackc/pgx/v5"
)

// 异步任务（docs/ai-gateway.md「异步任务」）。任务行与调用记录一一对应，计费不经过这里。

// ErrAIGatewayJobNotFound 任务不存在（或不属于这个用户、或结果已过期删除）。
var ErrAIGatewayJobNotFound = errors.New("ai gateway job not found")

// CreateAIGatewayJob 暂扣成功后登记一个进行中的任务。
func (r *Repository) CreateAIGatewayJob(ctx context.Context, requestNo, endpoint string, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `INSERT INTO ai_gateway_jobs (request_id, request_no, appid, user_id, endpoint, status, created_at, expires_at)
SELECT id, request_no, appid, user_id, $2, 'in_progress', $3, $4 FROM ai_gateway_requests WHERE request_no = $1`,
		requestNo, endpoint, now, now.Add(aigateway.JobRunningTTL))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAIGatewayRequestNotFound
	}
	return nil
}

// FinishAIGatewayJob 写任务结论。只改进行中的任务（取消与完成竞争时先到者为准），返回是否写入。
func (r *Repository) FinishAIGatewayJob(ctx context.Context, requestNo, status string, httpStatus int, contentType string,
	result []byte, jobErr *aigateway.JobError, now time.Time) (bool, error) {
	var errJSON []byte
	if jobErr != nil {
		encoded, err := json.Marshal(jobErr)
		if err != nil {
			return false, err
		}
		errJSON = encoded
	}
	tag, err := r.pool.Exec(ctx, `UPDATE ai_gateway_jobs SET status = $2, http_status = $3, content_type = $4, result = $5,
error = $6, finished_at = $7, expires_at = $8 WHERE request_no = $1 AND status = 'in_progress'`,
		requestNo, status, httpStatus, truncateColumn(contentType, 128), result, errJSON, now, now.Add(aigateway.JobResultTTL))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// GetAIGatewayJob 取一个任务（含调用记录的当前状态）。userID 非 0 时校验归属。
func (r *Repository) GetAIGatewayJob(ctx context.Context, appID, userID int64, requestNo string) (*aigateway.Job, error) {
	var job aigateway.Job
	var errJSON []byte
	err := r.pool.QueryRow(ctx, `SELECT j.request_no, j.appid, j.user_id, j.endpoint, j.status, j.http_status, j.content_type,
j.result, j.error, j.created_at, j.finished_at, j.expires_at, r.status
FROM ai_gateway_jobs j JOIN ai_gateway_requests r ON r.id = j.request_id
WHERE j.request_no = $1 AND j.appid = $2`, requestNo, appID).Scan(
		&job.RequestNo, &job.AppID, &job.UserID, &job.Endpoint, &job.Status, &job.HTTPStatus, &job.ContentType,
		&job.Result, &errJSON, &job.CreatedAt, &job.FinishedAt, &job.ExpiresAt, &job.RequestStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAIGatewayJobNotFound
	}
	if err != nil {
		return nil, err
	}
	if userID > 0 && job.UserID != userID {
		return nil, ErrAIGatewayJobNotFound
	}
	if len(errJSON) > 0 {
		job.Error = &aigateway.JobError{}
		if err := json.Unmarshal(errJSON, job.Error); err != nil {
			return nil, err
		}
	}
	return &job, nil
}

// SweepAIGatewayJobs 清理：删掉结果已过期的任务；把超时仍在进行中、或调用记录已经没有在暂扣的任务判为中断。
// 后者是实例在任务进行中被杀：调用记录会被后台按过期放回（不扣费），任务这边也要给出结论。
func (r *Repository) SweepAIGatewayJobs(ctx context.Context, now time.Time) (deleted, interrupted int64, err error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ai_gateway_jobs WHERE status <> 'in_progress' AND expires_at < $1`, now)
	if err != nil {
		return 0, 0, err
	}
	deleted = tag.RowsAffected()
	interruptedErr, _ := json.Marshal(aigateway.JobError{Message: "任务被中断，本次未扣费，请重新提交", Type: "api_error",
		Code: "job_interrupted", Status: 502})
	tag, err = r.pool.Exec(ctx, `UPDATE ai_gateway_jobs j SET status = 'failed', http_status = 502, error = $2,
finished_at = $1, expires_at = $1 + make_interval(secs => $3)
FROM ai_gateway_requests r
WHERE r.id = j.request_id AND j.status = 'in_progress'
  AND (j.expires_at < $1 OR (r.status IN ('failed', 'expired') AND r.settled_at < $1 - interval '1 minute'))`,
		now, interruptedErr, aigateway.JobResultTTL.Seconds())
	if err != nil {
		return deleted, 0, err
	}
	return deleted, tag.RowsAffected(), nil
}
