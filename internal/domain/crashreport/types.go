// Package crashreport 客户端崩溃报告：用户在崩溃屏上点「发送此报告」后，客户端免登录上报的一份纯文本报告。
//
// 只在用户逐份同意时上报（见 docs/crash-reports.md）：服务端不做任何自动采集，
// 也不把报告挂到「这台设备以后的所有崩溃」上。
package crashreport

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// 报告类型。
const (
	KindJava   = "java"   // JVM 未捕获异常
	KindNative = "native" // 原生层信号（SIGSEGV 等）
	KindANR    = "anr"    // 应用无响应
	KindExit   = "exit"   // 系统记录的异常退出（ApplicationExitInfo）
)

// Kinds 全部类型，顺序即控制台筛选项的顺序。
var Kinds = []string{KindJava, KindNative, KindANR, KindExit}

// 上限。
const (
	// MaxReportBytes 报告正文的上限（UTF-8 字节）。
	MaxReportBytes = 256 << 10
	// MaxRequestBytes 请求体上限：报告正文在 JSON 里转义（换行、引号、制表符）后会变长，
	// 加上其余字段，按正文上限的 4 倍留余量。超过即在读取阶段拒绝，不会把整个请求体读进内存。
	MaxRequestBytes = 4 * MaxReportBytes
	MaxTitleRunes   = 512
	MaxDetailRunes  = 4096
	// HourlyLimit 同一设备、同一 IP 各自每小时最多上报几份。重复上报（同一 reportId）不计入。
	HourlyLimit = 20
	// FutureSkew 发生时间最多允许比服务器时间晚多少（客户端时钟偏快），超出按服务器时间记。
	FutureSkew = 5 * time.Minute
)

var reportIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// ValidReportID 客户端记录编号：8–64 位字母数字 `_` `-`。
func ValidReportID(value string) bool {
	return reportIDPattern.MatchString(value)
}

// ValidKind 是否为已知的报告类型。
func ValidKind(kind string) bool {
	for _, item := range Kinds {
		if item == kind {
			return true
		}
	}
	return false
}

// TruncateRunes 去空白后截断到给定字符数。客户端自报的展示字段一律截断而不是拒绝：
// 崩溃屏上的用户已经点了发送，因为某个字段长了一点就整份丢掉没有意义。
func TruncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

// SanitizeText 去掉 Postgres 文本列存不下的 NUL 与非法 UTF-8 序列（原生崩溃的寄存器转储里两样都可能有）。
func SanitizeText(value string) string {
	if strings.IndexByte(value, 0) >= 0 {
		value = strings.ReplaceAll(value, "\x00", "")
	}
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "�")
	}
	return value
}

// ClampOccurredAt 发生时间晚于服务器时间超过 FutureSkew 的按服务器时间记。
// 早的不动：崩溃报告往往是下次启动、甚至几天后才发出去的。
func ClampOccurredAt(occurred, now time.Time) time.Time {
	if occurred.IsZero() || occurred.After(now.Add(FutureSkew)) {
		return now
	}
	return occurred
}

// DeviceInfo 请求头里的设备补充信息（X-Device-*），客户端自报，只用于展示。
type DeviceInfo struct {
	Platform     string `json:"platform,omitempty"`
	Name         string `json:"name,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Brand        string `json:"brand,omitempty"`
	Codename     string `json:"codename,omitempty"`
	OS           string `json:"os,omitempty"`
	OSVersion    string `json:"osVersion,omitempty"`
	AppVersion   string `json:"appVersion,omitempty"`
}

// SubmitInput 一次上报。UserID 只来自令牌，客户端不能指定。
type SubmitInput struct {
	AppID       int64
	UserID      int64
	ReportID    string
	Kind        string
	Title       string
	Detail      string
	Report      string
	OccurredAt  string
	AppVersion  string
	VersionCode *int64
	OSVersion   string
	Model       string
	ABI         string
	ProcessName string
	Source      string
	DeviceID    string
	Device      DeviceInfo
	UserAgent   string
	ClientIP    string
	Now         time.Time
}

// SubmitResult 上报结果。ID 是对外编号；Duplicate 表示同一 reportId 已经报过，返回的是原来那一条。
type SubmitResult struct {
	ID        string `json:"id"`
	Duplicate bool   `json:"duplicate"`
}

// Location 按客户端 IP 查到的大致位置（本地 GeoIP 库，查不到时为空）。
type Location struct {
	CountryCode string `json:"countryCode,omitempty"`
	Country     string `json:"country,omitempty"`
	Region      string `json:"region,omitempty"`
	City        string `json:"city,omitempty"`
}

// Report 一份已入库的报告。列表接口不带 Report 正文（Summary 视图），详情才带。
type Report struct {
	ID           int64      `json:"id"`
	PublicID     string     `json:"publicId"`
	AppID        int64      `json:"appid"`
	UserID       int64      `json:"userId,omitempty"`
	UserPublicID int64      `json:"userPublicId,omitempty"`
	Account      string     `json:"account,omitempty"`
	ReportID     string     `json:"reportId"`
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	Detail       string     `json:"detail,omitempty"`
	Report       string     `json:"report,omitempty"`
	ReportBytes  int        `json:"reportBytes"`
	AppVersion   string     `json:"appVersion,omitempty"`
	VersionCode  *int64     `json:"versionCode,omitempty"`
	OSVersion    string     `json:"osVersion,omitempty"`
	Model        string     `json:"model,omitempty"`
	ABI          string     `json:"abi,omitempty"`
	ProcessName  string     `json:"processName,omitempty"`
	Source       string     `json:"source,omitempty"`
	DeviceID     string     `json:"deviceId,omitempty"`
	Device       DeviceInfo `json:"device"`
	UserAgent    string     `json:"userAgent,omitempty"`
	ClientIP     string     `json:"clientIp,omitempty"`
	Location     Location   `json:"location"`
	Resolved     bool       `json:"resolved"`
	ResolvedAt   *time.Time `json:"resolvedAt,omitempty"`
	ResolvedBy   string     `json:"resolvedBy,omitempty"`
	OccurredAt   time.Time  `json:"occurredAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// Query 管理端列表筛选。
type Query struct {
	AppID      int64
	Kind       string
	AppVersion string
	Keyword    string
	// Resolved 为空表示不按处理状态筛。
	Resolved *bool
	UserID   int64
	DeviceID string
	Start    *time.Time
	End      *time.Time
	Page     int
	Limit    int
}

// Page 分页结果。
type Page struct {
	Items      []Report `json:"items"`
	Total      int64    `json:"total"`
	Page       int      `json:"page"`
	Limit      int      `json:"limit"`
	TotalPages int      `json:"totalPages"`
}

// VersionCount 某个应用版本的报告数。
type VersionCount struct {
	AppVersion string `json:"appVersion"`
	Count      int64  `json:"count"`
}

// KindCount 某类报告的数量。
type KindCount struct {
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
}

// Facets 筛选项与摘要：控制台筛选下拉里的版本只列真实出现过的。
type Facets struct {
	Total      int64          `json:"total"`
	Unresolved int64          `json:"unresolved"`
	Last24h    int64          `json:"last24h"`
	Kinds      []KindCount    `json:"kinds"`
	Versions   []VersionCount `json:"versions"`
}
