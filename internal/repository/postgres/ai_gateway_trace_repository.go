package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"aegis/internal/domain/aigateway"

	"github.com/jackc/pgx/v5"
)

// 调用追踪（docs/ai-gateway.md「调用追踪」）：摘要写在调用记录行上，详情另放 ai_gateway_request_traces。
// 追踪不参与计费，写失败只影响排查，所以不进结算事务。

// SaveAIGatewayTrace 写一次调用的追踪：更新记录行上的摘要列，详情按记录 id 覆盖写入。
func (r *Repository) SaveAIGatewayTrace(ctx context.Context, requestNo string, summary aigateway.TraceSummary, trace aigateway.Trace) error {
	if trace.Attempts == nil {
		trace.Attempts = []aigateway.Attempt{}
	}
	attempts, err := json.Marshal(trace.Attempts)
	if err != nil {
		return err
	}
	request, err := json.Marshal(trace.Request)
	if err != nil {
		return err
	}
	response, err := json.Marshal(trace.Response)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `WITH updated AS (
  UPDATE ai_gateway_requests SET attempts = $2, first_token_ms = $3, finish_reason = $4, upstream_request_id = $5,
    request_bytes = $6, response_bytes = $7
  WHERE request_no = $1 RETURNING id)
INSERT INTO ai_gateway_request_traces (request_id, attempts, request, response, updated_at)
SELECT id, $8, $9, $10, NOW() FROM updated
ON CONFLICT (request_id) DO UPDATE SET attempts = EXCLUDED.attempts, request = EXCLUDED.request,
  response = EXCLUDED.response, updated_at = NOW()`,
		requestNo, min(summary.Attempts, 32767), summary.FirstTokenMs, truncateColumn(summary.FinishReason, 32),
		truncateColumn(summary.UpstreamRequestID, 128), summary.RequestBytes, summary.ResponseBytes,
		attempts, request, response)
	return err
}

// GetAIGatewayTrace 一次调用的追踪详情；没有（追踪上线之前的调用、进行中的调用）时返回 nil。
func (r *Repository) GetAIGatewayTrace(ctx context.Context, requestID int64) (*aigateway.Trace, error) {
	var attempts, request, response []byte
	err := r.pool.QueryRow(ctx, `SELECT attempts, request, response FROM ai_gateway_request_traces WHERE request_id = $1`,
		requestID).Scan(&attempts, &request, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	trace := &aigateway.Trace{}
	if err := json.Unmarshal(attempts, &trace.Attempts); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(request, &trace.Request); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &trace.Response); err != nil {
		return nil, err
	}
	if trace.Attempts == nil {
		trace.Attempts = []aigateway.Attempt{}
	}
	return trace, nil
}

// AIGatewayLiveSummary 最近 minutes 分钟的调用概况，以及按通道汇总的上游尝试。
func (r *Repository) AIGatewayLiveSummary(ctx context.Context, appID int64, minutes int, now time.Time) (*aigateway.LiveSummary, error) {
	since := now.Add(-time.Duration(minutes) * time.Minute)
	out := &aigateway.LiveSummary{Minutes: minutes, Upstreams: []aigateway.UpstreamSummary{}}
	var charged int64
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE status <> 'reserved'),
  COUNT(*) FILTER (WHERE status IN ('succeeded', 'aborted', 'refunded')),
  COUNT(*) FILTER (WHERE status IN ('failed', 'expired')),
  COUNT(*) FILTER (WHERE attempts > 1),
  COUNT(DISTINCT user_id),
  COALESCE(SUM(input_tokens), 0)::bigint,
  COALESCE(SUM(output_tokens), 0)::bigint,
  COALESCE(SUM(charged) FILTER (WHERE status <> 'refunded'), 0)::bigint,
  COALESCE(AVG(latency_ms) FILTER (WHERE status <> 'reserved'), 0)::int,
  COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) FILTER (WHERE status <> 'reserved'), 0)::int,
  COALESCE(AVG(first_token_ms) FILTER (WHERE first_token_ms > 0), 0)::int
FROM ai_gateway_requests WHERE appid = $1 AND created_at >= $2`, appID, since).Scan(
		&out.Total, &out.Succeeded, &out.Failed, &out.Retried, &out.Users, &out.InputTokens, &out.OutputTokens,
		&charged, &out.AvgLatencyMs, &out.P95LatencyMs, &out.AvgFirstTokenMs); err != nil {
		return nil, err
	}
	out.Charged = aigateway.MicroToPoints(charged)
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ai_gateway_requests
WHERE appid = $1 AND status = 'reserved' AND expires_at > $2`, appID, now).Scan(&out.InFlight); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT COALESCE((a->>'configId')::bigint, 0), COALESCE(a->>'configName', ''),
  COALESCE(a->>'provider', ''), COUNT(*), COUNT(*) FILTER (WHERE a->>'status' = 'failed'),
  COALESCE(AVG((a->>'latencyMs')::int), 0)::int,
  COALESCE((array_agg(a->>'error' ORDER BY a->>'startedAt' DESC) FILTER (WHERE a->>'status' = 'failed'))[1], '')
FROM ai_gateway_requests r
JOIN ai_gateway_request_traces t ON t.request_id = r.id
CROSS JOIN LATERAL jsonb_array_elements(t.attempts) a
WHERE r.appid = $1 AND r.created_at >= $2
GROUP BY 1, 2, 3 ORDER BY 4 DESC LIMIT 20`, appID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item aigateway.UpstreamSummary
		if err := rows.Scan(&item.ConfigID, &item.ConfigName, &item.Provider, &item.Attempts, &item.Failed,
			&item.AvgLatencyMs, &item.LastError); err != nil {
			return nil, err
		}
		out.Upstreams = append(out.Upstreams, item)
	}
	return out, rows.Err()
}
