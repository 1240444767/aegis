package postgres

import (
	"context"
	"time"

	admindomain "aegis/internal/domain/admin"
)

// MCP 密钥（docs/mcp.md）。明文不进库，按 SHA-256 摘要定位。

const adminMCPKeyColumns = `k.id, k.admin_id, COALESCE(a.account, ''), k.name, k.description, k.key_prefix, k.key_hash,
k.permissions, k.app_ids, k.read_only, k.allow_ips, k.rate_limit_per_minute, k.status, k.expires_at,
k.last_used_at, k.last_used_ip, k.use_count, k.rotated_at, k.revoked_at, k.created_at, k.updated_at`

const adminMCPKeyFrom = ` FROM admin_mcp_keys k LEFT JOIN admin_accounts a ON a.id = k.admin_id`

func (r *Repository) CreateAdminMCPKey(ctx context.Context, adminID int64, input admindomain.MCPKeyInput, prefix string, hash []byte) (*admindomain.MCPKey, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO admin_mcp_keys
(admin_id, name, description, key_prefix, key_hash, permissions, app_ids, read_only, allow_ips, rate_limit_per_minute, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		adminID, input.Name, input.Description, prefix, hash, input.Permissions, input.AppIDs,
		input.ReadOnly, input.AllowIPs, input.RateLimitPerMinute, input.ExpiresAt).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetAdminMCPKey(ctx, id)
}

func (r *Repository) GetAdminMCPKey(ctx context.Context, id int64) (*admindomain.MCPKey, error) {
	return scanAdminMCPKey(r.pool.QueryRow(ctx, `SELECT `+adminMCPKeyColumns+adminMCPKeyFrom+` WHERE k.id=$1`, id))
}

func (r *Repository) GetAdminMCPKeyByHash(ctx context.Context, hash []byte) (*admindomain.MCPKey, error) {
	return scanAdminMCPKey(r.pool.QueryRow(ctx, `SELECT `+adminMCPKeyColumns+adminMCPKeyFrom+` WHERE k.key_hash=$1`, hash))
}

// ListAdminMCPKeys adminID 为 nil 时列全部（超管视图）。
func (r *Repository) ListAdminMCPKeys(ctx context.Context, adminID *int64) ([]admindomain.MCPKey, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+adminMCPKeyColumns+adminMCPKeyFrom+`
WHERE ($1::BIGINT IS NULL OR k.admin_id=$1) ORDER BY k.status='revoked', k.created_at DESC`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]admindomain.MCPKey, 0, 8)
	for rows.Next() {
		item, err := scanAdminMCPKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) CountActiveAdminMCPKeys(ctx context.Context, adminID int64) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_mcp_keys WHERE admin_id=$1 AND status<>'revoked'`, adminID).Scan(&count)
	return count, err
}

func (r *Repository) UpdateAdminMCPKey(ctx context.Context, id int64, input admindomain.MCPKeyInput) error {
	_, err := r.pool.Exec(ctx, `UPDATE admin_mcp_keys SET name=$2, description=$3, permissions=$4, app_ids=$5,
read_only=$6, allow_ips=$7, rate_limit_per_minute=$8, expires_at=$9, updated_at=NOW()
WHERE id=$1 AND status<>'revoked'`,
		id, input.Name, input.Description, input.Permissions, input.AppIDs, input.ReadOnly,
		input.AllowIPs, input.RateLimitPerMinute, input.ExpiresAt)
	return err
}

func (r *Repository) SetAdminMCPKeyStatus(ctx context.Context, id int64, status string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE admin_mcp_keys SET status=$2, updated_at=NOW() WHERE id=$1 AND status<>'revoked'`, id, status)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) RotateAdminMCPKey(ctx context.Context, id int64, prefix string, hash []byte) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE admin_mcp_keys SET key_prefix=$2, key_hash=$3, rotated_at=NOW(), updated_at=NOW()
WHERE id=$1 AND status<>'revoked'`, id, prefix, hash)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) RevokeAdminMCPKey(ctx context.Context, id int64, revokedBy int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE admin_mcp_keys SET status='revoked', revoked_at=NOW(), revoked_by=$2, updated_at=NOW()
WHERE id=$1 AND status<>'revoked'`, id, revokedBy)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// TouchAdminMCPKey 记一次使用。调用方负责节流，这里只管写。
func (r *Repository) TouchAdminMCPKey(ctx context.Context, id int64, ip string, uses int64, at time.Time) {
	_, _ = r.pool.Exec(ctx, `UPDATE admin_mcp_keys SET last_used_at=$2, last_used_ip=$3, use_count=use_count+$4 WHERE id=$1`,
		id, at, ip, uses)
}

func scanAdminMCPKey(row interface{ Scan(...any) error }) (*admindomain.MCPKey, error) {
	var item admindomain.MCPKey
	if err := row.Scan(&item.ID, &item.AdminID, &item.AdminAccount, &item.Name, &item.Description, &item.KeyPrefix,
		&item.KeyHash, &item.Permissions, &item.AppIDs, &item.ReadOnly, &item.AllowIPs, &item.RateLimitPerMinute,
		&item.Status, &item.ExpiresAt, &item.LastUsedAt, &item.LastUsedIP, &item.UseCount, &item.RotatedAt,
		&item.RevokedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	if item.Permissions == nil {
		item.Permissions = []string{}
	}
	if item.AppIDs == nil {
		item.AppIDs = []int64{}
	}
	if item.AllowIPs == nil {
		item.AllowIPs = []string{}
	}
	return &item, nil
}
