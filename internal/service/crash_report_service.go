package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/crashreport"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 崩溃报告的错误码。参数错误与限频沿用全站通用码（40000 / 42900），客户端不必为这一条接口另记一套。
const (
	errCodeCrashReportInvalid     = 40000
	errCodeCrashReportRateLimited = 42900
	errCodeCrashReportNotFound    = 40497
)

// crashReportBatchLimit 管理端批量操作一次最多多少条。
const crashReportBatchLimit = 200

// crashReportStore 服务层用到的仓储方法。单独成接口是为了让校验、去重、限频的单元测试不依赖数据库。
type crashReportStore interface {
	FindCrashReportByReportID(ctx context.Context, appID int64, reportID string) (*crashreport.Report, error)
	InsertCrashReport(ctx context.Context, item crashreport.Report) (*crashreport.Report, bool, error)
	CountRecentCrashReports(ctx context.Context, appID int64, deviceID, clientIP string, since time.Time) (int64, int64, error)
	GetCrashReport(ctx context.Context, appID, id int64) (*crashreport.Report, error)
	ListCrashReports(ctx context.Context, query crashreport.Query) (*crashreport.Page, error)
	CrashReportFacets(ctx context.Context, appID int64, now time.Time) (*crashreport.Facets, error)
	SetCrashReportsResolved(ctx context.Context, appID int64, ids []int64, resolved bool, operator string) (int64, error)
	DeleteCrashReports(ctx context.Context, appID int64, ids []int64) (int64, error)
}

// crashReportLocator 按 IP 查位置。只用本地缓存与 GeoIP 库（LocationService.GetCached），
// 不触发远程查询：上报是免登录接口，不能让它变成替任何人打外部 IP 接口的入口。
type crashReportLocator interface {
	GetCached(ctx context.Context, ip string) (IPLocation, bool)
}

// CrashReportService 客户端崩溃报告：免登录上报（去重、限频）与管理端查看、标记、删除。
type CrashReportService struct {
	log      *zap.Logger
	store    crashReportStore
	location crashReportLocator
	now      func() time.Time
}

func NewCrashReportService(log *zap.Logger, pg *pgrepo.Repository, location *LocationService) *CrashReportService {
	if log == nil {
		log = zap.NewNop()
	}
	svc := &CrashReportService{log: log, now: time.Now}
	if pg != nil {
		svc.store = pg
	}
	if location != nil {
		svc.location = location
	}
	return svc
}

// Submit 收一份报告。顺序是「校验 → 去重 → 限频 → 落库」：
// 同一 reportId 的重传先按重复返回，不吃限频额度 —— 客户端没收到响应时会重发，
// 那不该把它推到 429 上去。
func (s *CrashReportService) Submit(ctx context.Context, input crashreport.SubmitInput) (*crashreport.SubmitResult, error) {
	now := input.Now
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()
	item, err := normalizeCrashReport(input, now)
	if err != nil {
		return nil, err
	}

	existing, err := s.store.FindCrashReportByReportID(ctx, item.AppID, item.ReportID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &crashreport.SubmitResult{ID: existing.PublicID, Duplicate: true}, nil
	}

	if err := s.checkRate(ctx, item.AppID, item.DeviceID, item.ClientIP, now); err != nil {
		return nil, err
	}

	if s.location != nil && item.ClientIP != "" {
		if loc, ok := s.location.GetCached(ctx, item.ClientIP); ok && !loc.IsPrivate {
			item.Location = crashreport.Location{
				CountryCode: crashreport.TruncateRunes(loc.CountryCode, 8),
				Country:     crashreport.TruncateRunes(loc.Country, 64),
				Region:      crashreport.TruncateRunes(loc.Region, 64),
				City:        crashreport.TruncateRunes(loc.City, 64),
			}
		}
	}

	// 对外编号撞号（概率可以忽略）时换一个再试，不让它变成一次 500。
	for attempt := 0; ; attempt++ {
		item.PublicID = newCrashReportPublicID(now)
		saved, inserted, err := s.store.InsertCrashReport(ctx, item)
		if err != nil {
			if attempt < 2 && pgrepo.IsUniqueViolation(err) {
				continue
			}
			return nil, err
		}
		if inserted {
			return &crashreport.SubmitResult{ID: saved.PublicID, Duplicate: false}, nil
		}
		// 并发的另一次重传刚好先落了库
		existing, err := s.store.FindCrashReportByReportID(ctx, item.AppID, item.ReportID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, apperrors.New(50000, http.StatusInternalServerError, "崩溃报告保存失败")
		}
		return &crashreport.SubmitResult{ID: existing.PublicID, Duplicate: true}, nil
	}
}

// checkRate 同一设备、同一 IP 各自每小时最多 HourlyLimit 份。
//
// 直接数库：量级很小，走两条部分索引，不必为此引入 Redis 计数器，也没有计数与真实数据对不上的问题
// （与意见反馈的限频同一取向）。计数失败时放行 —— 限频是保护措施，不该让它的故障丢掉用户同意发送的报告。
func (s *CrashReportService) checkRate(ctx context.Context, appID int64, deviceID, clientIP string, now time.Time) error {
	byDevice, byIP, err := s.store.CountRecentCrashReports(ctx, appID, deviceID, clientIP, now.Add(-time.Hour))
	if err != nil {
		s.log.Warn("崩溃报告限频计数失败，本次放行", zap.Int64("appid", appID), zap.Error(err))
		return nil
	}
	if byDevice >= crashreport.HourlyLimit || byIP >= crashreport.HourlyLimit {
		return apperrors.New(errCodeCrashReportRateLimited, http.StatusTooManyRequests,
			"崩溃报告提交过于频繁，请稍后再试")
	}
	return nil
}

// normalizeCrashReport 校验必填项与上限，截断展示字段。
func normalizeCrashReport(input crashreport.SubmitInput, now time.Time) (crashreport.Report, error) {
	invalid := func(message string) (crashreport.Report, error) {
		return crashreport.Report{}, apperrors.New(errCodeCrashReportInvalid, http.StatusBadRequest, message)
	}
	reportID := strings.TrimSpace(input.ReportID)
	if !crashreport.ValidReportID(reportID) {
		return invalid("reportId 须为 8–64 位字母、数字、下划线或连字符")
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if !crashreport.ValidKind(kind) {
		return invalid("kind 须为 " + strings.Join(crashreport.Kinds, " / "))
	}
	title := crashreport.TruncateRunes(crashreport.SanitizeText(input.Title), crashreport.MaxTitleRunes)
	if title == "" {
		return invalid("title 不能为空")
	}
	report := crashreport.SanitizeText(input.Report)
	if strings.TrimSpace(report) == "" {
		return invalid("report 不能为空")
	}
	if len(report) > crashreport.MaxReportBytes {
		return invalid("report 超过 " + strconv.Itoa(crashreport.MaxReportBytes>>10) + " KB 上限")
	}
	rawOccurred := strings.TrimSpace(input.OccurredAt)
	if rawOccurred == "" {
		return invalid("occurredAt 不能为空")
	}
	occurred, err := time.Parse(time.RFC3339Nano, rawOccurred)
	if err != nil {
		return invalid("occurredAt 须为 RFC 3339 时间")
	}
	if input.VersionCode != nil && *input.VersionCode < 0 {
		return invalid("versionCode 不能为负数")
	}

	device := input.Device
	device = crashreport.DeviceInfo{
		Platform:     crashreport.TruncateRunes(device.Platform, 16),
		Name:         crashreport.TruncateRunes(device.Name, 128),
		Manufacturer: crashreport.TruncateRunes(device.Manufacturer, 64),
		Brand:        crashreport.TruncateRunes(device.Brand, 64),
		Codename:     crashreport.TruncateRunes(device.Codename, 64),
		OS:           crashreport.TruncateRunes(device.OS, 64),
		OSVersion:    crashreport.TruncateRunes(device.OSVersion, 64),
		AppVersion:   crashreport.TruncateRunes(device.AppVersion, 64),
	}
	return crashreport.Report{
		AppID:       input.AppID,
		UserID:      input.UserID,
		ReportID:    reportID,
		Kind:        kind,
		Title:       title,
		Detail:      crashreport.TruncateRunes(crashreport.SanitizeText(input.Detail), crashreport.MaxDetailRunes),
		Report:      report,
		ReportBytes: len(report),
		AppVersion:  crashreport.TruncateRunes(input.AppVersion, 64),
		VersionCode: input.VersionCode,
		OSVersion:   crashreport.TruncateRunes(input.OSVersion, 64),
		Model:       crashreport.TruncateRunes(input.Model, 128),
		ABI:         crashreport.TruncateRunes(input.ABI, 64),
		ProcessName: crashreport.TruncateRunes(input.ProcessName, 256),
		Source:      crashreport.TruncateRunes(input.Source, 32),
		DeviceID:    crashreport.TruncateRunes(input.DeviceID, 128),
		Device:      device,
		UserAgent:   crashreport.TruncateRunes(input.UserAgent, 512),
		ClientIP:    crashreport.TruncateRunes(input.ClientIP, 64),
		OccurredAt:  crashreport.ClampOccurredAt(occurred, now).UTC(),
	}, nil
}

var crashReportIDEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newCrashReportPublicID 对外编号：CR + 上报日期 + 8 位随机串（大写字母与数字），用户口头报号也不容易念错。
func newCrashReportPublicID(now time.Time) string {
	buf := make([]byte, 5)
	_, _ = rand.Read(buf)
	return "CR" + now.UTC().Format("20060102") + crashReportIDEncoding.EncodeToString(buf)
}

// ── 管理端 ──

// List 列表，不带正文。
func (s *CrashReportService) List(ctx context.Context, query crashreport.Query) (*crashreport.Page, error) {
	query.Kind = strings.ToLower(strings.TrimSpace(query.Kind))
	if query.Kind != "" && !crashreport.ValidKind(query.Kind) {
		return nil, apperrors.New(errCodeCrashReportInvalid, http.StatusBadRequest, "未知的报告类型")
	}
	return s.store.ListCrashReports(ctx, query)
}

// Facets 摘要与筛选项。
func (s *CrashReportService) Facets(ctx context.Context, appID int64) (*crashreport.Facets, error) {
	return s.store.CrashReportFacets(ctx, appID, s.now().UTC())
}

// Get 详情（含正文）。
func (s *CrashReportService) Get(ctx context.Context, appID, id int64) (*crashreport.Report, error) {
	item, err := s.store.GetCrashReport(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(errCodeCrashReportNotFound, http.StatusNotFound, "崩溃报告不存在")
	}
	return item, nil
}

// SetResolved 标记或取消「已处理」，返回实际改动了几条。
func (s *CrashReportService) SetResolved(ctx context.Context, appID int64, ids []int64, resolved bool, operator string) (int64, error) {
	ids, err := normalizeCrashReportIDs(ids)
	if err != nil {
		return 0, err
	}
	return s.store.SetCrashReportsResolved(ctx, appID, ids, resolved, crashreport.TruncateRunes(operator, 128))
}

// Delete 删除，返回删掉了几条。单条删除找不到时报 404，批量删除按实际删掉的数量回答。
func (s *CrashReportService) Delete(ctx context.Context, appID int64, ids []int64) (int64, error) {
	ids, err := normalizeCrashReportIDs(ids)
	if err != nil {
		return 0, err
	}
	deleted, err := s.store.DeleteCrashReports(ctx, appID, ids)
	if err != nil {
		return 0, err
	}
	if len(ids) == 1 && deleted == 0 {
		return 0, apperrors.New(errCodeCrashReportNotFound, http.StatusNotFound, "崩溃报告不存在")
	}
	return deleted, nil
}

func normalizeCrashReportIDs(ids []int64) ([]int64, error) {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, apperrors.New(errCodeCrashReportInvalid, http.StatusBadRequest, "请选择崩溃报告")
	}
	if len(out) > crashReportBatchLimit {
		return nil, apperrors.New(errCodeCrashReportInvalid, http.StatusBadRequest,
			"一次最多处理 "+strconv.Itoa(crashReportBatchLimit)+" 条")
	}
	return out, nil
}
