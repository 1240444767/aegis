package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"aegis/internal/domain/modelcatalog"

	"github.com/jackc/pgx/v5"
)

// 公开模型目录的快照（迁移 000128，单行表）。

// AIModelCatalogFetchedAt 快照的拉取时间；从没拉过返回零值。各实例据此判断内存里的索引是否过期，不必读整份数据。
func (r *Repository) AIModelCatalogFetchedAt(ctx context.Context) (time.Time, error) {
	var fetchedAt *time.Time
	err := r.pool.QueryRow(ctx, `SELECT fetched_at FROM ai_model_catalog WHERE id = 1`).Scan(&fetchedAt)
	if errors.Is(err, pgx.ErrNoRows) || fetchedAt == nil {
		return time.Time{}, nil
	}
	return *fetchedAt, err
}

// GetAIModelCatalog 整份快照；从没拉过返回 nil。
func (r *Repository) GetAIModelCatalog(ctx context.Context) (*modelcatalog.Snapshot, error) {
	var entries, sources []byte
	var fetchedAt *time.Time
	err := r.pool.QueryRow(ctx, `SELECT entries, sources, fetched_at FROM ai_model_catalog WHERE id = 1`).
		Scan(&entries, &sources, &fetchedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && fetchedAt == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot := &modelcatalog.Snapshot{FetchedAt: *fetchedAt}
	if err := json.Unmarshal(entries, &snapshot.Entries); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sources, &snapshot.Sources); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// ClaimAIModelCatalogRefresh 抢刷新租约：没有别的实例正在刷新（或它的租约已过）时占下，返回 true。
// staleBefore 非零时只在快照早于它（或从没拉过）时才抢 —— 定时刷新用；手动刷新传零值。
func (r *Repository) ClaimAIModelCatalogRefresh(ctx context.Context, now, staleBefore time.Time, lease time.Duration) (bool, error) {
	query := `UPDATE ai_model_catalog SET refreshing_until = $1
WHERE id = 1 AND (refreshing_until IS NULL OR refreshing_until < $2)`
	args := []any{now.Add(lease), now}
	if !staleBefore.IsZero() {
		query += ` AND (fetched_at IS NULL OR fetched_at < $3)`
		args = append(args, staleBefore)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// SaveAIModelCatalog 写入新快照并释放租约。
func (r *Repository) SaveAIModelCatalog(ctx context.Context, snapshot modelcatalog.Snapshot) error {
	entries, err := json.Marshal(snapshot.Entries)
	if err != nil {
		return err
	}
	sources, err := json.Marshal(snapshot.Sources)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO ai_model_catalog (id, entries, sources, entry_count, fetched_at, refreshing_until)
VALUES (1, $1, $2, $3, $4, NULL)
ON CONFLICT (id) DO UPDATE SET entries = EXCLUDED.entries, sources = EXCLUDED.sources,
  entry_count = EXCLUDED.entry_count, fetched_at = EXCLUDED.fetched_at, refreshing_until = NULL`,
		entries, sources, len(snapshot.Entries), snapshot.FetchedAt)
	return err
}

// ReleaseAIModelCatalogRefresh 刷新失败：释放租约，快照不动。
func (r *Repository) ReleaseAIModelCatalogRefresh(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `UPDATE ai_model_catalog SET refreshing_until = NULL WHERE id = 1`)
	return err
}

// ListAIGatewayCatalogAutoSyncApps 打开了「每天从模型目录自动更新」的应用。
func (r *Repository) ListAIGatewayCatalogAutoSyncApps(ctx context.Context) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT appid FROM ai_gateway_settings
WHERE COALESCE((settings->>'catalogAutoSync')::boolean, FALSE) AND COALESCE((settings->>'enabled')::boolean, FALSE)
ORDER BY appid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var appID int64
		if err := rows.Scan(&appID); err != nil {
			return nil, err
		}
		out = append(out, appID)
	}
	return out, rows.Err()
}
