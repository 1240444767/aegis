package user

import "time"

const (
	AccountBanTypeTemporary = "temporary"
	AccountBanTypePermanent = "permanent"

	// AccountBanScopeLogin 只拦新会话：各类登录都被拒，已登录的设备照常使用（含刷新令牌续期）。
	AccountBanScopeLogin = "login"
	// AccountBanScopeAll 拦全部访问：封禁当场撤销所有会话与第三方授权，已签发的令牌访问业务接口也被拒。
	AccountBanScopeAll = "all"

	AccountBanStatusActive  = "active"
	AccountBanStatusExpired = "expired"
	AccountBanStatusRevoked = "revoked"
)

// AccountBanCheckpoint 是封禁的执行点。两档范围的区别完全落在这里：
// 仅登录的封禁只在 SignIn 生效，全部访问的封禁两处都生效。
type AccountBanCheckpoint uint8

const (
	// AccountBanCheckpointSignIn 换取新会话：密码 / 短信 / 第三方 / 卡密 / 扫码 / 通行密钥登录、
	// 二次验证、找回密码后的登录，以及 OAuth2 授权服务器上的新登录。
	AccountBanCheckpointSignIn AccountBanCheckpoint = iota
	// AccountBanCheckpointSession 已签发的会话继续使用：业务接口、刷新令牌、OAuth2 刷新。
	AccountBanCheckpointSession
)

// Blocks 判定这条封禁在某个执行点是否拦人。nil 表示没有生效中的封禁。
func (b *AccountBan) Blocks(checkpoint AccountBanCheckpoint) bool {
	if b == nil {
		return false
	}
	if checkpoint == AccountBanCheckpointSignIn {
		return true
	}
	return b.BanScope != AccountBanScopeLogin
}

type BanOperator struct {
	AdminID   int64  `json:"adminId"`
	AdminName string `json:"adminName"`
}

type AccountBan struct {
	ID                 int64          `json:"id"`
	AppID              int64          `json:"appid"`
	UserID             int64          `json:"userId"`
	BanType            string         `json:"banType"`
	BanScope           string         `json:"banScope"`
	Status             string         `json:"status"`
	Reason             string         `json:"reason"`
	Evidence           map[string]any `json:"evidence,omitempty"`
	BannedByAdminID    *int64         `json:"bannedByAdminId,omitempty"`
	BannedByAdminName  string         `json:"bannedByAdminName,omitempty"`
	RevokedByAdminID   *int64         `json:"revokedByAdminId,omitempty"`
	RevokedByAdminName string         `json:"revokedByAdminName,omitempty"`
	RevokeReason       string         `json:"revokeReason,omitempty"`
	StartAt            time.Time      `json:"startAt"`
	EndAt              *time.Time     `json:"endAt,omitempty"`
	RevokedAt          *time.Time     `json:"revokedAt,omitempty"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

type AccountBanCreateInput struct {
	BanType  string         `json:"banType"`
	BanScope string         `json:"banScope"`
	Reason   string         `json:"reason"`
	Evidence map[string]any `json:"evidence,omitempty"`
	StartAt  *time.Time     `json:"startAt,omitempty"`
	EndAt    *time.Time     `json:"endAt,omitempty"`
	Operator BanOperator    `json:"operator"`
}

type AccountBanRevokeInput struct {
	Reason   string      `json:"reason"`
	Operator BanOperator `json:"operator"`
}

type AccountBanQuery struct {
	Status string `json:"status"`
	Page   int    `json:"page"`
	Limit  int    `json:"limit"`
}

// AccountBanCounts 是某个用户各状态封禁的条数，不受列表的状态筛选影响。
type AccountBanCounts struct {
	Total   int64 `json:"total"`
	Active  int64 `json:"active"`
	Expired int64 `json:"expired"`
	Revoked int64 `json:"revoked"`
}

type AccountBanListResult struct {
	Items      []AccountBan     `json:"items"`
	Page       int              `json:"page"`
	Limit      int              `json:"limit"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"totalPages"`
	Counts     AccountBanCounts `json:"counts"`
}

type AccountBanBatchCreateInput struct {
	UserIDs []int64 `json:"userIds"`
	AccountBanCreateInput
}

type AccountBanBatchCreateResult struct {
	AppID            int64   `json:"appid"`
	Requested        int     `json:"requested"`
	Created          int64   `json:"created"`
	Failed           int     `json:"failed"`
	ProcessedUserIDs []int64 `json:"processedUserIds"`
	FailedUserIDs    []int64 `json:"failedUserIds"`
}
