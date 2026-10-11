package service

import (
	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	userdomain "aegis/internal/domain/user"
	vipdomain "aegis/internal/domain/vip"
	"aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/taskpool"
	"aegis/pkg/timeutil"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// 自动签到。
//
// 两层开关：应用的开放策略（管理员配置，apps.settings.autoSign）决定谁有资格，
// 个人设置（user_settings.autoSign）决定本人开不开、几点签。排班放在 Redis 有序集合里，
// 分值是下一次执行的时刻；API 与 Worker 都跑调度循环，靠 Claim 保证一条排班只执行一次。
//
// 资格在三处判定，用的是同一个纯函数 appdomain.EvaluateAutoSign：
//   - 开启时（PrepareSettings）：没有资格不许打开；
//   - 排班时（RebuildSchedule / SyncUserSchedule）：没有资格不排；
//   - 执行时（processEntry）：到点时已经失去资格（会员到期、管理员收紧范围）就暂停并通知一次。
// 个人设置在暂停时保持开启，资格恢复后由周期性重建重新排上。

type AutoSignService struct {
	mu        sync.RWMutex
	cfg       config.AutoSignConfig
	log       *zap.Logger
	pg        *postgres.Repository
	schedules *redisrepo.AutoSignRepository
	signIn    *SignInService
	location  *time.Location
	notifier  autoSignNotifier
}

// autoSignNotifier 站内信出口（NotificationService.NotifyUser）。
type autoSignNotifier interface {
	NotifyUser(ctx context.Context, appID int64, userID int64,
		notificationType string, title string, content string, level string, metadata map[string]any) error
}

const (
	autoSignDeviceInfo = "Aegis AutoSign HyperNode"
	autoSignIPAddress  = "mesh://autosign.aegis/core"
	autoSignLocation   = "Aegis Chrono Mesh · Autonomous Relay"

	// autoSignSource 自动签到写进签到记录的来源。
	autoSignSource = "auto"

	errCodeAutoSignDisabled    = 40353
	errCodeAutoSignNotEligible = 40354
)

func NewAutoSignService(cfg config.AutoSignConfig, log *zap.Logger, pg *postgres.Repository, schedules *redisrepo.AutoSignRepository, signIn *SignInService) *AutoSignService {
	location := timeutil.MustDefaultRuleLocation(cfg.Timezone)
	return &AutoSignService{
		cfg:       cfg,
		log:       log,
		pg:        pg,
		schedules: schedules,
		signIn:    signIn,
		location:  location,
	}
}

// SetNotifier 接上站内信。未接时签到照常，只是不发通知。
func (s *AutoSignService) SetNotifier(notifier autoSignNotifier) { s.notifier = notifier }

func (s *AutoSignService) Reload(cfg config.AutoSignConfig) {
	location := timeutil.MustDefaultRuleLocation(cfg.Timezone)
	s.mu.Lock()
	s.cfg = cfg
	s.location = location
	s.mu.Unlock()
}

func (s *AutoSignService) CurrentConfig() config.AutoSignConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *AutoSignService) runtime() (config.AutoSignConfig, *time.Location) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg, s.location
}

// ── 资格 ──

// Policy 应用的开放策略。应用不存在时按默认策略（不开放）。
func (s *AutoSignService) Policy(ctx context.Context, appID int64) (appdomain.AutoSignPolicy, error) {
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return appdomain.DefaultAutoSignPolicy(), err
	}
	if app == nil {
		return appdomain.DefaultAutoSignPolicy(), nil
	}
	return appdomain.AutoSignPolicyFromSettings(app.Settings), nil
}

// evaluate 用户在给定策略下的资格；返回空串即可用。全局开关关掉时一律不可用。
func (s *AutoSignService) evaluate(ctx context.Context, policy appdomain.AutoSignPolicy, appID int64, userID int64) (string, error) {
	if cfg, _ := s.runtime(); !cfg.Enabled {
		return appdomain.AutoSignReasonDisabled, nil
	}
	member := appdomain.AutoSignMember{}
	if policy.NeedsMembership() {
		facts, err := s.pg.GetVipEntitlementFacts(ctx, appID, userID)
		if err != nil {
			return "", err
		}
		entitlement := vipdomain.Evaluate(*facts, time.Now())
		member = appdomain.AutoSignMember{
			IsVIP:      entitlement.IsVIP,
			IsLifetime: entitlement.IsLifetime,
			IsTrial:    entitlement.IsTrial,
			Features:   entitlement.Features,
		}
	}
	return appdomain.EvaluateAutoSign(policy, member), nil
}

// featureName 功能标识的展示名；查不到退回标识本身。
func (s *AutoSignService) featureName(ctx context.Context, appID int64, policy appdomain.AutoSignPolicy) string {
	if policy.Audience != appdomain.AutoSignAudienceFeature || policy.FeatureTag == "" {
		return ""
	}
	feature, err := s.pg.GetVipFeature(ctx, appID, policy.FeatureTag)
	if err != nil || feature == nil || strings.TrimSpace(feature.Name) == "" {
		return policy.FeatureTag
	}
	return feature.Name
}

// reasonError 把不可用原因翻成接口错误。
func (s *AutoSignService) reasonError(ctx context.Context, appID int64, policy appdomain.AutoSignPolicy, reason string) error {
	message := appdomain.AutoSignReasonMessage(reason, policy, s.featureName(ctx, appID, policy))
	if reason == appdomain.AutoSignReasonDisabled {
		return apperrors.New(errCodeAutoSignDisabled, http.StatusForbidden, message)
	}
	return apperrors.New(errCodeAutoSignNotEligible, http.StatusForbidden, message)
}

// PrepareSettings 写入个人设置前的清洗与资格校验（UserService.UpdateSettings 在 autoSign 分类上调用）。
//
// 只拦「从关到开」：已经开着、因失去资格而暂停的用户仍可以改时刻、改通知，
// 也可以关掉；否则资格一丢，连关掉开关都会被拒绝。
func (s *AutoSignService) PrepareSettings(ctx context.Context, appID int64, userID int64, current map[string]any, patch map[string]any) (map[string]any, error) {
	before := userdomain.AutoSignSettingsFromMap(current)
	merged, err := userdomain.MergeAutoSignSettings(before, patch)
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if merged.Enabled && !before.Enabled {
		policy, err := s.Policy(ctx, appID)
		if err != nil {
			return nil, err
		}
		reason, err := s.evaluate(ctx, policy, appID, userID)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			return nil, s.reasonError(ctx, appID, policy, reason)
		}
	}
	return merged.ToMap(), nil
}

// Status 当前用户的自动签到状态。
func (s *AutoSignService) Status(ctx context.Context, appID int64, userID int64) (*userdomain.AutoSignStatus, error) {
	policy, err := s.Policy(ctx, appID)
	if err != nil {
		return nil, err
	}
	reason, err := s.evaluate(ctx, policy, appID, userID)
	if err != nil {
		return nil, err
	}
	settings := userdomain.DefaultAutoSignSettings()
	stored, err := s.pg.GetUserSettings(ctx, userID, "autoSign")
	if err != nil {
		return nil, err
	}
	if stored != nil {
		settings = userdomain.AutoSignSettingsFromMap(stored.Settings)
	}
	_, location := s.runtime()
	featureName := s.featureName(ctx, appID, policy)
	status := &userdomain.AutoSignStatus{
		Available: reason == "",
		Reason:    reason,
		Message:   appdomain.AutoSignReasonMessage(reason, policy, featureName),
		Active:    reason == "" && settings.Enabled,
		Settings:  settings,
		Access: userdomain.AutoSignAccess{
			Enabled:      policy.Enabled,
			Audience:     policy.Audience,
			IncludeTrial: policy.IncludeTrial,
			FeatureTag:   policy.FeatureTag,
			FeatureName:  featureName,
		},
		Timezone: location.String(),
	}
	if status.Active {
		if next, err := s.schedules.NextRunAt(ctx, appID, userID); err == nil && next != nil {
			at := next.In(location)
			status.NextRunAt = &at
		}
	}
	if last, err := s.schedules.GetLastRun(ctx, appID, userID); err == nil {
		status.LastRun = last
	}
	return status, nil
}

// ── 排班 ──

func (s *AutoSignService) CatchUpOnStartup(ctx context.Context) (int, int, error) {
	cfg, _ := s.runtime()
	if !cfg.Enabled {
		return 0, 0, nil
	}
	scheduled, err := s.RebuildSchedule(ctx)
	if err != nil {
		return scheduled, 0, err
	}
	processed, err := s.RunDue(ctx)
	return scheduled, processed, err
}

// RebuildSchedule 全量重建排班：按资格补排或撤下。周期执行，兜住认领后中断、资格恢复这类情况。
func (s *AutoSignService) RebuildSchedule(ctx context.Context) (int, error) {
	return s.rebuild(ctx, 0)
}

// RebuildApp 只重建一个应用的排班。管理员改了开放策略后调用，不必等下一次周期重建。
func (s *AutoSignService) RebuildApp(ctx context.Context, appID int64) (int, error) {
	if appID <= 0 {
		return 0, nil
	}
	return s.rebuild(ctx, appID)
}

func (s *AutoSignService) rebuild(ctx context.Context, appID int64) (int, error) {
	cfg, location := s.runtime()
	if !cfg.Enabled {
		return 0, nil
	}
	lastUserID := int64(0)
	scheduled := 0
	// 一轮重建里同一个应用的策略只读一次
	policies := map[int64]appdomain.AutoSignPolicy{}
	for {
		items, err := s.pg.ListAutoSignCandidatesAfterUserID(ctx, appID, lastUserID, cfg.BatchSize)
		if err != nil {
			return scheduled, err
		}
		if len(items) == 0 {
			return scheduled, nil
		}
		lastUserID = items[len(items)-1].UserID
		for _, item := range items {
			if _, ok := policies[item.AppID]; ok {
				continue
			}
			policy, err := s.Policy(ctx, item.AppID)
			if err != nil {
				return scheduled, err
			}
			policies[item.AppID] = policy
		}
		reference := timeutil.Now().In(location)
		var batchScheduled atomic.Int64
		if err := taskpool.DispatchWithError(ctx, cfg.Concurrency, items, func(taskCtx context.Context, item userdomain.AutoSignCandidate) error {
			eligible, err := s.isEligible(taskCtx, policies[item.AppID], item)
			if err != nil {
				return err
			}
			if !eligible {
				return s.schedules.Remove(taskCtx, item.AppID, item.UserID)
			}
			// 已经在排班上的不动：重建不该把一条正在等重试的排班提前或推迟
			if next, err := s.schedules.NextRunAt(taskCtx, item.AppID, item.UserID); err == nil && next != nil {
				batchScheduled.Add(1)
				return nil
			}
			if err := s.schedules.Schedule(taskCtx, item.AppID, item.UserID, s.initialDue(reference, item.Settings.Time, location)); err != nil {
				return err
			}
			batchScheduled.Add(1)
			return nil
		}); err != nil {
			return scheduled, err
		}
		scheduled += int(batchScheduled.Load())
	}
}

// SyncUserSchedule 个人设置变更后按新设置重排。时刻改了要立刻按新时刻排，所以总是覆盖。
func (s *AutoSignService) SyncUserSchedule(ctx context.Context, userID int64, appID int64) error {
	cfg, location := s.runtime()
	if !cfg.Enabled {
		return nil
	}
	item, err := s.pg.GetAutoSignCandidate(ctx, userID, appID)
	if err != nil {
		return err
	}
	if item == nil {
		return s.schedules.Remove(ctx, appID, userID)
	}
	policy, err := s.Policy(ctx, appID)
	if err != nil {
		return err
	}
	eligible, err := s.isEligible(ctx, policy, *item)
	if err != nil {
		return err
	}
	if !eligible {
		return s.schedules.Remove(ctx, appID, userID)
	}
	return s.schedules.Schedule(ctx, appID, userID, s.initialDue(timeutil.Now().In(location), item.Settings.Time, location))
}

func (s *AutoSignService) RunDue(ctx context.Context) (int, error) {
	cfg, location := s.runtime()
	if !cfg.Enabled {
		return 0, nil
	}
	entries, err := s.schedules.GetDue(ctx, timeutil.Now().In(location), int64(cfg.BatchSize))
	if err != nil {
		return 0, err
	}
	var processed atomic.Int64
	if err := taskpool.Dispatch(ctx, cfg.Concurrency, entries, func(taskCtx context.Context, entry redisrepo.AutoSignEntry) {
		claimed, err := s.processEntry(taskCtx, entry)
		if err != nil {
			s.log.Warn("auto sign process failed", zap.Int64("user_id", entry.UserID), zap.Int64("appid", entry.AppID), zap.Error(err))
			return
		}
		if claimed {
			processed.Add(1)
		}
	}); err != nil {
		return int(processed.Load()), err
	}
	return int(processed.Load()), nil
}

func (s *AutoSignService) ScheduledCount(ctx context.Context) int64 {
	count, err := s.schedules.Count(ctx)
	if err != nil {
		return 0
	}
	return count
}

// processEntry 执行一条到期排班。返回是否由本实例认领到。
func (s *AutoSignService) processEntry(ctx context.Context, entry redisrepo.AutoSignEntry) (bool, error) {
	claimed, err := s.schedules.Claim(ctx, entry.AppID, entry.UserID)
	if err != nil || !claimed {
		return false, err
	}
	cfg, location := s.runtime()
	now := timeutil.Now().In(location)
	retryLater := func() error {
		return s.schedules.Schedule(ctx, entry.AppID, entry.UserID, now.Add(cfg.RetryDelay))
	}

	item, err := s.pg.GetAutoSignCandidate(ctx, entry.UserID, entry.AppID)
	if err != nil {
		return true, errors.Join(err, retryLater())
	}
	if item == nil || !item.Enabled || !item.Settings.Enabled {
		return true, nil
	}
	policy, err := s.Policy(ctx, entry.AppID)
	if err != nil {
		return true, errors.Join(err, retryLater())
	}
	reason, err := s.evaluate(ctx, policy, entry.AppID, entry.UserID)
	if err != nil {
		return true, errors.Join(err, retryLater())
	}
	if reason != "" {
		// 到点时已经没有资格：不再排班，告诉用户一次。资格恢复后由重建排回来。
		message := appdomain.AutoSignReasonMessage(reason, policy, s.featureName(ctx, entry.AppID, policy))
		s.recordRun(ctx, entry, userdomain.AutoSignRun{At: now, Status: userdomain.AutoSignRunSuspended, Message: message})
		if item.Settings.NotifyOnFail && reason != appdomain.AutoSignReasonDisabled {
			s.notify(ctx, entry, "自动签到已暂停", message+"，自动签到已暂停。恢复资格后将自动继续。", "warning", userdomain.AutoSignRunSuspended)
		}
		return true, nil
	}

	signDate := now.Format("2006-01-02")
	nextDue := s.nextDayDue(now.Add(time.Second), item.Settings.Time, location)
	result, err := s.signIn.SignInForUser(ctx, entry.UserID, entry.AppID, autoSignSource, autoSignDeviceInfo, autoSignIPAddress, s.autoSignLocation(item))
	if err != nil {
		if appErr, ok := errors.AsType[*apperrors.AppError](err); ok && appErr.Code == 40902 {
			_ = s.schedules.ClearAttempts(ctx, entry.AppID, entry.UserID, signDate)
			s.recordRun(ctx, entry, userdomain.AutoSignRun{At: now, Status: userdomain.AutoSignRunAlreadySigned, SignDate: signDate})
			return true, s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
		}
		attempt, countErr := s.schedules.IncrAttempt(ctx, entry.AppID, entry.UserID, signDate)
		if countErr != nil {
			attempt = 1
		}
		message := autoSignFailureMessage(err)
		if item.Settings.RetryOnFail && attempt <= item.Settings.MaxRetries {
			s.recordRun(ctx, entry, userdomain.AutoSignRun{At: now, Status: userdomain.AutoSignRunRetrying, Message: message, SignDate: signDate, Attempt: attempt})
			s.log.Warn("auto sign failed, will retry", zap.Int64("user_id", entry.UserID), zap.Int64("appid", entry.AppID), zap.Int("attempt", attempt), zap.Error(err))
			return true, retryLater()
		}
		s.recordRun(ctx, entry, userdomain.AutoSignRun{At: now, Status: userdomain.AutoSignRunFailed, Message: message, SignDate: signDate, Attempt: attempt})
		if item.Settings.NotifyOnFail {
			s.notify(ctx, entry, "自动签到未成功", "今日自动签到未成功："+message+"。请稍后手动签到。", "warning", userdomain.AutoSignRunFailed)
		}
		s.log.Warn("auto sign failed", zap.Int64("user_id", entry.UserID), zap.Int64("appid", entry.AppID), zap.Int("attempt", attempt), zap.Error(err))
		return true, s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
	}

	_ = s.schedules.ClearAttempts(ctx, entry.AppID, entry.UserID, signDate)
	if result != nil {
		run := userdomain.AutoSignRun{
			At:               now,
			Status:           userdomain.AutoSignRunSuccess,
			SignDate:         result.Record.SignDate,
			IntegralReward:   result.Record.IntegralReward,
			ExperienceReward: result.Record.ExperienceReward,
			ConsecutiveDays:  result.Record.ConsecutiveDays,
		}
		if result.AlreadySigned {
			run = userdomain.AutoSignRun{At: now, Status: userdomain.AutoSignRunAlreadySigned, SignDate: result.Record.SignDate}
		}
		s.recordRun(ctx, entry, run)
		if !result.AlreadySigned && item.Settings.NotifyOnSuccess {
			s.notify(ctx, entry, "自动签到成功", autoSignSuccessMessage(result), "info", userdomain.AutoSignRunSuccess)
		}
		s.log.Info("auto sign completed",
			zap.Int64("user_id", entry.UserID),
			zap.Int64("appid", entry.AppID),
			zap.String("sign_date", result.Record.SignDate),
			zap.Bool("already_signed", result.AlreadySigned),
			zap.Int64("integral_reward", result.Record.IntegralReward),
			zap.Int64("experience_reward", result.Record.ExperienceReward),
		)
	}
	return true, s.schedules.Schedule(ctx, entry.AppID, entry.UserID, nextDue)
}

func (s *AutoSignService) recordRun(ctx context.Context, entry redisrepo.AutoSignEntry, run userdomain.AutoSignRun) {
	if err := s.schedules.SetLastRun(ctx, entry.AppID, entry.UserID, run); err != nil {
		s.log.Debug("record auto sign run failed", zap.Int64("user_id", entry.UserID), zap.Error(err))
	}
}

func (s *AutoSignService) notify(ctx context.Context, entry redisrepo.AutoSignEntry, title string, content string, level string, status string) {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.NotifyUser(ctx, entry.AppID, entry.UserID, "system", title, content, level, map[string]any{
		"scene":  "auto_sign",
		"status": status,
	}); err != nil {
		s.log.Debug("auto sign notification failed", zap.Int64("user_id", entry.UserID), zap.Error(err))
	}
}

func autoSignSuccessMessage(result *userdomain.SignInResult) string {
	parts := make([]string, 0, 2)
	if result.Record.IntegralReward > 0 {
		parts = append(parts, fmt.Sprintf("%d 积分", result.Record.IntegralReward))
	}
	if result.Record.ExperienceReward > 0 {
		parts = append(parts, fmt.Sprintf("%d 经验", result.Record.ExperienceReward))
	}
	message := "今日已自动签到"
	if len(parts) > 0 {
		message += "，获得 " + strings.Join(parts, "、")
	}
	if result.Record.ConsecutiveDays > 1 {
		message += fmt.Sprintf("，已连续签到 %d 天", result.Record.ConsecutiveDays)
	}
	return message + "。"
}

// autoSignFailureMessage 业务错误给出原话，其它错误不把内部细节透给用户。
func autoSignFailureMessage(err error) string {
	if appErr, ok := errors.AsType[*apperrors.AppError](err); ok && strings.TrimSpace(appErr.Message) != "" {
		return strings.TrimSuffix(strings.TrimSpace(appErr.Message), "。")
	}
	return "服务暂时不可用"
}

func (s *AutoSignService) isEligible(ctx context.Context, policy appdomain.AutoSignPolicy, item userdomain.AutoSignCandidate) (bool, error) {
	if !item.Enabled || !item.Settings.Enabled {
		return false, nil
	}
	reason, err := s.evaluate(ctx, policy, item.AppID, item.UserID)
	if err != nil {
		return false, err
	}
	return reason == "", nil
}

func (s *AutoSignService) initialDue(reference time.Time, timeValue string, location *time.Location) time.Time {
	hour, minute := s.parseScheduleTime(timeValue)
	due := time.Date(reference.Year(), reference.Month(), reference.Day(), hour, minute, 0, 0, location)
	if due.After(reference) {
		return due
	}
	return reference
}

func (s *AutoSignService) nextDayDue(reference time.Time, timeValue string, location *time.Location) time.Time {
	hour, minute := s.parseScheduleTime(timeValue)
	next := time.Date(reference.Year(), reference.Month(), reference.Day(), hour, minute, 0, 0, location)
	if !next.After(reference) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (s *AutoSignService) parseScheduleTime(timeValue string) (int, int) {
	hour, minute := 0, 0
	if _, err := fmt.Sscanf(timeValue, "%d:%d", &hour, &minute); err != nil {
		hour, minute = 0, 0
	}
	return hour, minute
}

func (s *AutoSignService) autoSignLocation(item *userdomain.AutoSignCandidate) string {
	if item != nil && item.Settings.DisableLocationTracking {
		return ""
	}
	return autoSignLocation
}

// ── 管理端 ──

// AdminOverview 管理端：开放策略与使用情况。
func (s *AutoSignService) AdminOverview(ctx context.Context, appID int64) (*AutoSignAdminOverview, error) {
	policy, err := s.Policy(ctx, appID)
	if err != nil {
		return nil, err
	}
	enabledUsers, err := s.pg.CountAutoSignEnabledUsers(ctx, appID)
	if err != nil {
		return nil, err
	}
	_, location := s.runtime()
	today := timeutil.Now().In(location)
	start := today.AddDate(0, 0, -(autoSignTrendDays - 1))
	counts, err := s.pg.CountSignInsBySourceSince(ctx, appID, autoSignSource, start.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	trend := make([]AutoSignDailyCount, 0, autoSignTrendDays)
	for i := 0; i < autoSignTrendDays; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		trend = append(trend, AutoSignDailyCount{Date: date, Count: counts[date]})
	}
	return &AutoSignAdminOverview{
		AppID:        appID,
		Policy:       policy,
		FeatureName:  s.featureName(ctx, appID, policy),
		Timezone:     location.String(),
		EnabledUsers: enabledUsers,
		TodayCount:   counts[today.Format("2006-01-02")],
		Trend:        trend,
	}, nil
}

const autoSignTrendDays = 14

// AutoSignAdminOverview 管理端概览。
type AutoSignAdminOverview struct {
	AppID       int64                    `json:"appid"`
	Policy      appdomain.AutoSignPolicy `json:"policy"`
	FeatureName string                   `json:"featureName,omitempty"`
	Timezone    string                   `json:"timezone"`
	// EnabledUsers 开启了自动签到的用户数（不论当前有无资格）
	EnabledUsers int64 `json:"enabledUsers"`
	// TodayCount 今天自动签到成功的次数
	TodayCount int64                `json:"todayCount"`
	Trend      []AutoSignDailyCount `json:"trend"`
}

// AutoSignDailyCount 某天自动签到成功的次数。
type AutoSignDailyCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// SavePolicy 管理端：保存开放策略，并按新策略重排本应用的排班。
func (s *AutoSignService) SavePolicy(ctx context.Context, apps *AppService, appID int64, policy appdomain.AutoSignPolicy) (*AutoSignAdminOverview, error) {
	normalized, err := appdomain.NormalizeAutoSignPolicy(policy)
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if normalized.Audience == appdomain.AutoSignAudienceFeature {
		feature, err := s.pg.GetVipFeature(ctx, appID, normalized.FeatureTag)
		if err != nil {
			return nil, err
		}
		if feature == nil {
			return nil, apperrors.New(errCodeFeatureUnknown, http.StatusNotFound,
				"功能标识 "+normalized.FeatureTag+" 未登记，请先在会员功能目录里创建它")
		}
	}
	app, err := apps.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings[appdomain.AutoSignSettingsKey] = normalized.ToMap()
	if _, err := apps.SaveApp(ctx, appdomain.AppMutation{ID: appID, Settings: settings}); err != nil {
		return nil, err
	}
	// 收紧范围时要把失去资格的人撤下，放宽时要把新获得资格的人排上；不阻塞保存
	go func() {
		rebuildCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := s.RebuildApp(rebuildCtx, appID); err != nil {
			s.log.Warn("rebuild auto sign schedule after policy change failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}()
	return s.AdminOverview(ctx, appID)
}
