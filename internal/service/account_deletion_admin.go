package service

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"aegis/internal/domain/accountdeletion"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 账号注销的管理端：申请列表与详情、审核、撤销、立即执行、改期、代用户发起、墓碑与统计；
// 以及到点执行与到期提醒的后台循环。

const (
	accountDeletionNoteMaxRunes   = 500
	accountDeletionStatsMaxDays   = 180
	accountDeletionMaxRescheduled = 365 * 24 * time.Hour
)

// ListRequests 申请列表。
func (s *AccountDeletionService) ListRequests(ctx context.Context, query accountdeletion.RequestQuery) (*accountdeletion.RequestPage, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > 100 {
		query.Limit = 20
	}
	query.Status = strings.TrimSpace(query.Status)
	query.Mode = strings.TrimSpace(query.Mode)
	query.Source = strings.TrimSpace(query.Source)
	return s.pg.ListAccountDeletionRequests(ctx, query)
}

// GetRequest 一条申请。
func (s *AccountDeletionService) GetRequest(ctx context.Context, appID, id int64) (*accountdeletion.Request, error) {
	req, err := s.pg.GetAccountDeletionRequest(ctx, appID, id)
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	return req, nil
}

// AccountDeletionUserView 用户详情页上的注销状况：进行中的申请与历史。
type AccountDeletionUserView struct {
	Open    *accountdeletion.Request  `json:"open"`
	History []accountdeletion.Request `json:"history"`
	Enabled bool                      `json:"enabled"`
	Config  accountdeletion.Config    `json:"config"`
}

// UserView 某个用户的注销状况。
func (s *AccountDeletionService) UserView(ctx context.Context, appID, userID int64) (*AccountDeletionUserView, error) {
	settings, err := s.pg.GetAccountDeletionSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	open, err := s.pg.GetOpenAccountDeletionRequest(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	page, err := s.pg.ListAccountDeletionRequests(ctx, accountdeletion.RequestQuery{AppID: appID, UserID: userID, Page: 1, Limit: 20})
	if err != nil {
		return nil, err
	}
	return &AccountDeletionUserView{Open: open, History: page.Items, Enabled: settings.Enabled, Config: settings.Config}, nil
}

func trimNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > accountDeletionNoteMaxRunes {
		return "", apperrors.New(40000, http.StatusBadRequest, "说明不超过 500 字")
	}
	return note, nil
}

// Approve 批准待审核的申请：冷静期从批准时起算；立即注销当场执行。
func (s *AccountDeletionService) Approve(ctx context.Context, appID, id int64, note, operator string) (*accountdeletion.Request, error) {
	note, err := trimNote(note)
	if err != nil {
		return nil, err
	}
	settings, err := s.pg.GetAccountDeletionSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	current, err := s.GetRequest(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if current.Status != accountdeletion.StatusReviewing {
		return nil, apperrors.New(errCodeDeletionStateChanged, http.StatusConflict, "只有待审核的申请可以批准")
	}
	now := s.now()
	set := map[string]any{"reviewed_at": now, "reviewed_by": operator, "review_note": note}
	to := accountdeletion.StatusProcessing
	if current.Mode == accountdeletion.ModeCooling {
		days := current.CoolingDays
		if days < accountdeletion.MinCoolingDays {
			days = settings.Config.CoolingDays
		}
		due, remind := accountdeletion.Schedule(now, days, settings.Config.RemindBeforeHours)
		set["scheduled_at"], set["remind_at"], set["cooling_days"] = due, remind, days
		to = accountdeletion.StatusScheduled
	} else {
		set["attempts"] = 1
	}
	req, err := s.pg.TransitionAccountDeletionRequest(ctx, appID, id, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusReviewing}, To: to, Set: set,
	})
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	s.log.Info("注销申请已批准", zap.Int64("requestId", id), zap.String("operator", operator))
	if to == accountdeletion.StatusProcessing {
		done, execErr := s.execute(ctx, req, settings.Config)
		if execErr != nil {
			return s.GetRequest(ctx, appID, id)
		}
		return done, nil
	}
	s.announce(req, settings.Config, AccountDeletionNoticeScheduled, "", "")
	return req, nil
}

// Reject 驳回待审核的申请，理由必填（会展示给用户）。
func (s *AccountDeletionService) Reject(ctx context.Context, appID, id int64, note, operator string) (*accountdeletion.Request, error) {
	note, err := trimNote(note)
	if err != nil {
		return nil, err
	}
	if note == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请填写驳回理由，用户会看到这段说明")
	}
	req, err := s.pg.TransitionAccountDeletionRequest(ctx, appID, id, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusReviewing}, To: accountdeletion.StatusRejected,
		Set: map[string]any{"reviewed_at": s.now(), "reviewed_by": operator, "review_note": note},
	})
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	settings, err := s.pg.GetAccountDeletionSettings(ctx, appID)
	if err == nil {
		s.announce(req, settings.Config, AccountDeletionNoticeRejected, "", note)
	}
	return req, nil
}

// AdminCancel 管理员撤销（待审核、冷静期中、执行失败）。
func (s *AccountDeletionService) AdminCancel(ctx context.Context, appID, id int64, reason, operator string, notify bool) (*accountdeletion.Request, error) {
	reason, err := trimNote(reason)
	if err != nil {
		return nil, err
	}
	req, err := s.pg.TransitionAccountDeletionRequest(ctx, appID, id, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusReviewing, accountdeletion.StatusScheduled, accountdeletion.StatusFailed},
		To:   accountdeletion.StatusCancelled,
		Set: map[string]any{"cancelled_at": s.now(), "cancelled_by": accountdeletion.CancelledByAdminPrefix + operator,
			"cancel_reason": reason},
	})
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	if notify {
		note := "管理员撤销了注销申请"
		if reason != "" {
			note += "：" + reason
		}
		s.afterCancel(ctx, req, note)
	}
	return req, nil
}

// ExecuteNow 立即执行冷静期中或执行失败的申请。confirm 必须与申请的账号名一致（防误点）。
func (s *AccountDeletionService) ExecuteNow(ctx context.Context, appID, id int64, confirm, operator string) (*accountdeletion.Request, error) {
	current, err := s.GetRequest(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(confirm) != current.Account {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请输入账号名确认执行")
	}
	settings, err := s.pg.GetAccountDeletionSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	req, err := s.pg.TransitionAccountDeletionRequest(ctx, appID, id, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusScheduled, accountdeletion.StatusFailed},
		To:   accountdeletion.StatusProcessing,
		Set:  map[string]any{"attempts": 1},
	})
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	s.log.Warn("管理员立即执行注销", zap.Int64("requestId", id), zap.String("operator", operator))
	if done, execErr := s.execute(ctx, req, settings.Config); execErr == nil {
		return done, nil
	}
	return s.GetRequest(ctx, appID, id)
}

// Reschedule 改冷静期的到期时刻（只对冷静期中的申请）；提醒按新的时刻重算。
func (s *AccountDeletionService) Reschedule(ctx context.Context, appID, id int64, scheduledAt time.Time, notify bool) (*accountdeletion.Request, error) {
	now := s.now()
	if !scheduledAt.After(now.Add(time.Minute)) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "注销时间必须晚于现在")
	}
	if scheduledAt.After(now.Add(accountDeletionMaxRescheduled)) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "注销时间不能晚于一年之后")
	}
	settings, err := s.pg.GetAccountDeletionSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	var remind any
	if hours := settings.Config.RemindBeforeHours; hours > 0 {
		if at := scheduledAt.Add(-time.Duration(hours) * time.Hour); at.After(now) {
			remind = at
		}
	}
	req, err := s.pg.TransitionAccountDeletionRequest(ctx, appID, id, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusScheduled}, To: accountdeletion.StatusScheduled,
		Set: map[string]any{"scheduled_at": scheduledAt.UTC(), "remind_at": remind, "reminded_at": nil},
	})
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	if notify {
		s.announce(req, settings.Config, AccountDeletionNoticeScheduled, "", "")
	}
	return req, nil
}

// AccountDeletionAdminCreateInput 管理员代用户发起注销。
type AccountDeletionAdminCreateInput struct {
	AppID       int64
	UserID      int64
	Mode        string
	CoolingDays int
	// ReasonCode 应用配置里的一项原因（选填）；Reason 为补充说明。两者至少给一个
	ReasonCode string
	Reason     string
	Notify     bool
	// Confirm 立即注销时必须与账号名一致（防误点）
	Confirm  string
	Operator string
	IP       string
}

// AdminCreate 管理员代用户发起：不过验证与前置条件（前置条件的结论记进快照供查看）。
// 立即注销当场执行；冷静期按给定天数（不给时用应用配置）。
func (s *AccountDeletionService) AdminCreate(ctx context.Context, input AccountDeletionAdminCreateInput) (*accountdeletion.Request, error) {
	reason, err := trimNote(input.Reason)
	if err != nil {
		return nil, err
	}
	mode := strings.TrimSpace(input.Mode)
	if mode != accountdeletion.ModeImmediate && mode != accountdeletion.ModeCooling {
		return nil, apperrors.New(40000, http.StatusBadRequest, "mode 只能是 immediate 或 cooling")
	}
	settings, err := s.pg.GetAccountDeletionSettings(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	cfg := settings.Config
	reasonCode := strings.ToLower(strings.TrimSpace(input.ReasonCode))
	if reasonCode != "" && !slices.ContainsFunc(cfg.Reasons, func(r accountdeletion.Reason) bool { return r.Code == reasonCode }) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择有效的注销原因")
	}
	if reasonCode == "" && reason == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择或填写注销原因")
	}
	subject, err := s.loadSubject(ctx, input.AppID, input.UserID)
	if err != nil {
		return nil, err
	}
	facts, err := s.pg.AccountDeletionFacts(ctx, input.AppID, input.UserID)
	if err != nil {
		return nil, err
	}
	if mode == accountdeletion.ModeImmediate && strings.TrimSpace(input.Confirm) != subject.user.Account {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请输入账号名确认立即注销")
	}
	checks := accountdeletion.EvaluateGuards(cfg, *facts, s.now())
	record := accountdeletion.RequestInput{
		AppID: input.AppID, UserID: input.UserID, Account: subject.user.Account, Nickname: subjectNickname(subject),
		Mode: mode, Source: accountdeletion.SourceAdmin, ReasonCode: reasonCode, ReasonText: reason, Operator: input.Operator, IP: input.IP,
		VerifiedBy: []string{}, Snapshot: s.snapshot(subject, facts, checks),
	}
	now := s.now()
	if mode == accountdeletion.ModeCooling {
		days := input.CoolingDays
		if days == 0 {
			days = cfg.CoolingDays
		}
		if days < accountdeletion.MinCoolingDays || days > accountdeletion.MaxCoolingDays {
			return nil, apperrors.New(40000, http.StatusBadRequest, "冷静期为 1–180 天")
		}
		due, remind := accountdeletion.Schedule(now, days, cfg.RemindBeforeHours)
		record.Status, record.CoolingDays, record.ScheduledAt, record.RemindAt = accountdeletion.StatusScheduled, days, &due, remind
	} else {
		record.Status = accountdeletion.StatusProcessing
	}
	req, err := s.pg.CreateAccountDeletionRequest(ctx, record)
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	s.log.Warn("管理员发起账号注销", zap.Int64("appid", input.AppID), zap.Int64("userId", input.UserID),
		zap.String("mode", mode), zap.String("operator", input.Operator))
	if record.Status == accountdeletion.StatusProcessing {
		notifyCfg := cfg
		notifyCfg.NotifyEmail = cfg.NotifyEmail && input.Notify
		if done, execErr := s.execute(ctx, req, notifyCfg); execErr == nil {
			return done, nil
		}
		return s.GetRequest(ctx, input.AppID, req.ID)
	}
	if input.Notify {
		s.announce(req, cfg, AccountDeletionNoticeScheduled, subject.email, "")
	}
	return req, nil
}

// ── 墓碑 ──

// ListTombstones 未到期的重新注册限制。
func (s *AccountDeletionService) ListTombstones(ctx context.Context, appID int64, keyword string, page, limit int) (*accountdeletion.TombstonePage, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.pg.ListAccountDeletionTombstones(ctx, appID, keyword, page, limit)
}

// ReleaseTombstone 解除一条重新注册限制。
func (s *AccountDeletionService) ReleaseTombstone(ctx context.Context, appID, id int64) error {
	ok, err := s.pg.DeleteAccountDeletionTombstone(ctx, appID, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40449, http.StatusNotFound, "限制记录不存在或已到期")
	}
	return nil
}

// ── 统计 ──

// Stats days 为趋势天数（7–180），timezone 为浏览器时区名（非法时回落到服务器默认时区）。
func (s *AccountDeletionService) Stats(ctx context.Context, appID int64, days int, timezone string) (*accountdeletion.Stats, error) {
	if days < 7 {
		days = 30
	}
	if days > accountDeletionStatsMaxDays {
		days = accountDeletionStatsMaxDays
	}
	loc := timeutil.DefaultLocation()
	if tz := strings.TrimSpace(timezone); tz != "" {
		if parsed, err := time.LoadLocation(tz); err == nil {
			loc = parsed
		}
	}
	now := s.now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	stats, err := s.pg.AccountDeletionStats(ctx, appID, start, loc.String())
	if err != nil {
		return nil, err
	}
	stats.Days = days
	// 补齐没有数据的日子，前端直接画
	byDay := make(map[string]accountdeletion.TrendPoint, len(stats.Trend))
	for _, point := range stats.Trend {
		byDay[point.Date] = point
	}
	trend := make([]accountdeletion.TrendPoint, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		point, ok := byDay[day]
		if !ok {
			point = accountdeletion.TrendPoint{Date: day}
		}
		trend = append(trend, point)
	}
	stats.Trend = trend
	if settings, err := s.pg.GetAccountDeletionSettings(ctx, appID); err == nil {
		for i := range stats.Reasons {
			switch stats.Reasons[i].Key {
			case "":
				stats.Reasons[i].Label = "未选择"
				continue
			case accountdeletion.AdminReasonKey:
				stats.Reasons[i].Label = "管理员发起"
				continue
			}
			stats.Reasons[i].Label = accountdeletion.ReasonLabel(settings.Config, stats.Reasons[i].Key)
		}
	}
	return stats, nil
}

// ════════════════════════════════════════════════════════════
//  后台循环
// ════════════════════════════════════════════════════════════

// Start 起后台循环：每分钟执行到期的冷静期申请、发到期提醒。多实例同时跑靠 SKIP LOCKED 认领，不会重复执行。
func (s *AccountDeletionService) Start(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("account deletion loop panic", zap.Any("panic", r))
			}
		}()
		if n, err := s.pg.RecoverStaleAccountDeletions(ctx, accountDeletionStaleAfter); err != nil {
			s.log.Warn("回收中断的注销失败", zap.Error(err))
		} else if n > 0 {
			s.log.Warn("中断的注销已标记为失败", zap.Int64("count", n))
		}
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-timer.C:
				if processed, err := s.RunDue(ctx); err != nil {
					s.log.Warn("执行到期注销失败", zap.Error(err))
				} else if processed > 0 {
					s.log.Info("到期注销已执行", zap.Int("processed", processed))
				}
				timer.Reset(accountDeletionTickInterval)
			}
		}
	}()
}

// Stop 停止后台循环。
func (s *AccountDeletionService) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// RunDue 发到期提醒、执行到期的冷静期申请，返回执行的条数。
func (s *AccountDeletionService) RunDue(ctx context.Context) (int, error) {
	now := s.now()
	reminders, err := s.pg.ClaimDeletionReminders(ctx, now, accountDeletionBatch)
	if err != nil {
		return 0, err
	}
	settingsCache := map[int64]accountdeletion.Config{}
	configOf := func(appID int64) accountdeletion.Config {
		if cfg, ok := settingsCache[appID]; ok {
			return cfg
		}
		cfg := accountdeletion.DefaultConfig()
		if settings, err := s.pg.GetAccountDeletionSettings(ctx, appID); err == nil {
			cfg = settings.Config
		}
		settingsCache[appID] = cfg
		return cfg
	}
	for i := range reminders {
		s.announce(&reminders[i], configOf(reminders[i].AppID), AccountDeletionNoticeReminder, "", "")
	}
	processed := 0
	for {
		due, err := s.pg.ClaimDueAccountDeletions(ctx, now, accountDeletionBatch)
		if err != nil {
			return processed, err
		}
		for i := range due {
			if _, err := s.execute(ctx, &due[i], configOf(due[i].AppID)); err == nil {
				processed++
			}
		}
		if len(due) < accountDeletionBatch {
			return processed, nil
		}
	}
}
