package realtime

import (
	"strings"
	"time"
)

type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	AppID     int64          `json:"appid"`
	UserID    int64          `json:"userId"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// PresenceConnection 一条实时连接的在线记录。
//
// 设备与地区字段在建连那一刻从会话和 IP 库取好存进 Redis：在线统计的分布
// （平台、系统、版本、地区）按这些字段聚合，聚合时再回查会话或 IP 库，
// 一次快照就是几千次往返。旧连接没有这些字段，聚合时计入「未知」。
type PresenceConnection struct {
	ConnectionID string    `json:"connectionId"`
	AppID        int64     `json:"appid"`
	UserID       int64     `json:"userId"`
	Account      string    `json:"account,omitempty"`
	TokenID      string    `json:"tokenId,omitempty"`
	DeviceID     string    `json:"deviceId,omitempty"`
	DeviceName   string    `json:"deviceName,omitempty"`
	DeviceModel  string    `json:"deviceModel,omitempty"`
	Platform     string    `json:"platform,omitempty"`
	OS           string    `json:"os,omitempty"`
	OSVersion    string    `json:"osVersion,omitempty"`
	AppVersion   string    `json:"appVersion,omitempty"`
	IP           string    `json:"ip,omitempty"`
	Country      string    `json:"country,omitempty"`
	Region       string    `json:"region,omitempty"`
	City         string    `json:"city,omitempty"`
	ISP          string    `json:"isp,omitempty"`
	UserAgent    string    `json:"userAgent,omitempty"`
	ConnectedAt  time.Time `json:"connectedAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
	ServerID     string    `json:"serverId,omitempty"`
}

type OnlineStats struct {
	OnlineUsers       int64     `json:"onlineUsers"`
	OnlineConnections int64     `json:"onlineConnections"`
	OnlineApps        int64     `json:"onlineApps"`
	RefreshedAt       time.Time `json:"refreshedAt"`
}

type AppOnlineStats struct {
	AppID             int64     `json:"appid"`
	OnlineUsers       int64     `json:"onlineUsers"`
	OnlineConnections int64     `json:"onlineConnections"`
	RefreshedAt       time.Time `json:"refreshedAt"`
}

// AppOnlineUser 一个在线用户在某应用下的连接概况。
//
// Account / Nickname / IP / ConnectedAt 是给管理端表格直接用的：
// presence 存在 Redis 里，那里只有 userId，账号名要回 Postgres 查；
// IP 与建立时间躺在 SampleConnection 里，嵌一层就得让每个调用方自己去挖。
// 之前这四项都没有，管理端的在线用户表于是只有时间列有值，
// 用户与 IP 两列一直是空的。
type AppOnlineUser struct {
	AppID       int64  `json:"appid"`
	UserID      int64  `json:"userId"`
	Account     string `json:"account,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	IP          string `json:"ip,omitempty"`
	Location    string `json:"location,omitempty"`
	Connections int64  `json:"connections"`
	// ConnectedAt 取最早的一条连接：用户关心的是"从什么时候开始在线"，
	// 而不是最近一次重连的时刻。
	ConnectedAt       time.Time            `json:"connectedAt,omitzero"`
	LastSeenAt        time.Time            `json:"lastSeenAt"`
	SampleConnection  *PresenceConnection  `json:"sampleConnection,omitempty"`
	ConnectionSamples []PresenceConnection `json:"connectionSamples,omitempty"`
}

type AppOnlineUserList struct {
	AppID       int64           `json:"appid"`
	Page        int             `json:"page"`
	Limit       int             `json:"limit"`
	Total       int64           `json:"total"`
	TotalPages  int             `json:"totalPages"`
	Items       []AppOnlineUser `json:"items"`
	RefreshedAt time.Time       `json:"refreshedAt"`
}

// ── 实时在线统计（管理端加密长连接 /api/admin/system/online/live） ──

// Bucket 分布里的一项：键与连接数。键为空串表示客户端没有上报。
type Bucket struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// Dimensions 在线连接按维度的分布，每个维度按连接数降序、截取前若干项。
type Dimensions struct {
	Platform   []Bucket `json:"platform"`
	OS         []Bucket `json:"os"`
	AppVersion []Bucket `json:"appVersion"`
	Country    []Bucket `json:"country"`
	Region     []Bucket `json:"region"`
	Device     []Bucket `json:"device"`
}

// OnlineTotals 全站或单个应用的在线总量。
type OnlineTotals struct {
	Users       int64 `json:"users"`
	Connections int64 `json:"connections"`
	Apps        int64 `json:"apps"`
}

// AppOnlineSummary 快照里每个应用的一行。
type AppOnlineSummary struct {
	AppID       int64  `json:"appid"`
	Users       int64  `json:"users"`
	Connections int64  `json:"connections"`
	Name        string `json:"name,omitempty"`
	AppKey      string `json:"appKey,omitempty"`
}

// NodeLoad 接入节点（API 实例）上承载的连接数。
type NodeLoad struct {
	ServerID    string `json:"serverId"`
	Connections int64  `json:"connections"`
	Admins      int64  `json:"admins"`
}

// AdminOnlineTotals 控制台在线的管理员（开着控制台、实时连接未断）。
type AdminOnlineTotals struct {
	Admins      int64 `json:"admins"`
	Connections int64 `json:"connections"`
}

// OnlineSnapshot 一次全量在线快照。
//
// 由集群里抢到锁的那个实例按固定节拍计算并写入 Redis，其余实例直接读，
// 一次计算供全部订阅者使用。按订阅者权限裁剪（Scoped）之后才下发。
type OnlineSnapshot struct {
	Version   int64                `json:"version"`
	At        time.Time            `json:"at"`
	Totals    OnlineTotals         `json:"totals"`
	Apps      []AppOnlineSummary   `json:"apps"`
	Dims      Dimensions           `json:"dims"`
	AppDims   map[int64]Dimensions `json:"appDims,omitempty"`
	Nodes     []NodeLoad           `json:"nodes,omitempty"`
	Admins    *AdminOnlineTotals   `json:"admins,omitempty"`
	Truncated bool                 `json:"truncated,omitempty"`
}

// SeriesPoint 在线曲线上的一个点：时间（Unix 秒）、在线用户、连接数、在线管理员。
type SeriesPoint struct {
	At          int64 `json:"t"`
	Users       int64 `json:"u"`
	Connections int64 `json:"c"`
	Admins      int64 `json:"a,omitempty"`
}

// PresenceEvent 上线或下线事件，经 NATS 在实例之间广播，再按权限推给订阅者。
type PresenceEvent struct {
	Kind         string    `json:"kind"` // online / offline
	AppID        int64     `json:"appid"`
	UserID       int64     `json:"userId"`
	Account      string    `json:"account,omitempty"`
	ConnectionID string    `json:"connectionId"`
	IP           string    `json:"ip,omitempty"`
	Location     string    `json:"location,omitempty"`
	Platform     string    `json:"platform,omitempty"`
	DeviceName   string    `json:"deviceName,omitempty"`
	ServerID     string    `json:"serverId,omitempty"`
	At           time.Time `json:"at"`
	// Duration 下线事件携带的本次连接时长（秒）
	Duration int64 `json:"duration,omitempty"`
	// Admin 为真表示这是控制台管理员的连接，只推给超级管理员
	Admin bool `json:"admin,omitempty"`
}

// AdminPresence 一位在控制台在线的管理员及其连接。
type AdminPresence struct {
	AdminID     int64                `json:"adminId"`
	Account     string               `json:"account,omitempty"`
	DisplayName string               `json:"displayName,omitempty"`
	Avatar      string               `json:"avatar,omitempty"`
	SuperAdmin  bool                 `json:"isSuperAdmin,omitempty"`
	Connections []PresenceConnection `json:"connections"`
	ConnectedAt time.Time            `json:"connectedAt,omitzero"`
	LastSeenAt  time.Time            `json:"lastSeenAt"`
}

// Location 拼出「国家 省份 城市」的可读地区，重复的层级只留一次。
func (c PresenceConnection) Location() string {
	parts := make([]string, 0, 3)
	for _, part := range []string{c.Country, c.Region, c.City} {
		if part == "" {
			continue
		}
		if len(parts) > 0 && parts[len(parts)-1] == part {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}
