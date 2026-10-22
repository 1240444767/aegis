package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	securitydomain "aegis/internal/domain/security"
	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// ════════════════════════════════════════════════════════════
//  业务接入与处置执行
//
//  业务服务只调 Guard：它把评估结论翻译成统一的业务错误。
//  处置执行（封账号 / 拉黑设备与 IP / 防火墙封禁）收在 enforce 一处，
//  自动封禁、复核驳回、人工处置三条路径共用，留痕格式因此一致。
// ════════════════════════════════════════════════════════════

// 风控相关错误码
const (
	errCodeRiskBlocked         = 40398
	errCodeRiskCaptchaRequired = 40015
)

// riskAccountBanner 账号封禁执行器（AccountBanService）
type riskAccountBanner interface {
	BanUser(ctx context.Context, appID int64, userID int64, input userdomain.AccountBanCreateInput) (*userdomain.AccountBan, error)
	RevokeBan(ctx context.Context, appID int64, userID int64, banID int64, input userdomain.AccountBanRevokeInput) (*userdomain.AccountBan, error)
	GetActiveBan(ctx context.Context, appID int64, userID int64) (*userdomain.AccountBan, error)
}

// riskFirewall 防火墙封禁执行器（IPBanService）
type riskFirewall interface {
	BanIPWithOptions(ctx context.Context, ip string, reason string, durationSec int64, adminID int64, opts BanIPOptions) (*firewalldomain.IPBan, error)
	UnbanIP(ctx context.Context, banID int64, adminID int64) error
}

// SetAccountBanService 接入账号封禁：ban 动作与复核驳回才能真的封到账号上
func (s *RiskService) SetAccountBanService(bans riskAccountBanner) {
	s.mu.Lock()
	s.accountBans = bans
	s.mu.Unlock()
}

// SetFirewall 接入防火墙：「防火墙」处置对象挡的是这个 IP 的全部请求，而不只是评估点
func (s *RiskService) SetFirewall(firewall riskFirewall) {
	s.mu.Lock()
	s.firewall = firewall
	s.mu.Unlock()
}

// SetCaptchaProbe 接入「应用有没有可用的图形验证码」判定。没有验证码的应用上，
// 验证码处置转人工复核，而不是返回一个客户端拿不到验证码的要求。
func (s *RiskService) SetCaptchaProbe(probe func(ctx context.Context, appID int64) bool) {
	s.mu.Lock()
	s.captchaProbe = probe
	s.mu.Unlock()
}

func (s *RiskService) executors() (riskAccountBanner, riskFirewall, func(context.Context, int64) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accountBans, s.firewall, s.captchaProbe
}

func (s *RiskService) captchaAvailable(ctx context.Context, appID int64) bool {
	_, _, probe := s.executors()
	if probe == nil || appID <= 0 {
		return false
	}
	return probe(ctx, appID)
}

// RiskGuardOption Guard 的业务定制：各业务有自己的拒绝错误码与文案
type RiskGuardOption struct {
	Code    int
	Message string
}

// Guard 评估一次业务请求并翻译成业务错误：拦截 / 封禁 → 403，需要验证码 → 40015，
// 其余放行（复核是事后的）。风控自身出错时放行并记日志 —— 风控抖一下不该让全站登录不了。
func (s *RiskService) Guard(ctx context.Context, req securitydomain.RiskEvalRequest, opts ...RiskGuardOption) (*securitydomain.RiskEvalResult, error) {
	if s == nil {
		return nil, nil
	}
	result, err := s.EvaluateRisk(ctx, req)
	if err != nil {
		s.logWarn("风控评估失败，按放行处理", zap.String("scene", req.Scene), zap.Error(err))
		return nil, nil
	}
	return result, GuardError(result, opts...)
}

// GuardError 把评估结论翻译成业务错误（nil 为放行）
func GuardError(result *securitydomain.RiskEvalResult, opts ...RiskGuardOption) error {
	if result == nil {
		return nil
	}
	opt := RiskGuardOption{Code: errCodeRiskBlocked, Message: "当前请求存在风险，已被拦截"}
	if len(opts) > 0 {
		if opts[0].Code != 0 {
			opt.Code = opts[0].Code
		}
		if opts[0].Message != "" {
			opt.Message = opts[0].Message
		}
	}
	switch result.Action {
	case securitydomain.ActionBlock, securitydomain.ActionBan:
		return apperrors.New(opt.Code, http.StatusForbidden, opt.Message)
	case securitydomain.ActionCaptcha:
		return apperrors.New(errCodeRiskCaptchaRequired, http.StatusBadRequest, "当前环境存在风险，请先完成图形验证码")
	}
	return nil
}

// IsRiskRejection 错误是否为风控拒绝（调用方据此区分「风控拦了」与「业务失败」）
func IsRiskRejection(err error) bool {
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && (appErr.Code == errCodeRiskBlocked || appErr.Code == errCodeRiskCaptchaRequired)
}

// enforceRequest 一次处置
type enforceRequest struct {
	AssessmentID *int64
	AppID        int64
	UserID       int64
	Account      string
	IP           string
	DeviceID     string
	Targets      []string
	Kind         string
	Duration     int
	Reason       string
	Source       string
	OperatorID   int64
	OperatorName string
}

// enforce 把处置落到每个对象上，逐项留痕。某一项失败不影响其它项。
func (s *RiskService) enforce(ctx context.Context, req enforceRequest) []securitydomain.Enforcement {
	bans, firewall, _ := s.executors()
	out := make([]securitydomain.Enforcement, 0, len(req.Targets))
	var until *time.Time
	if req.Duration > 0 {
		t := timeutil.NowUTC().Add(time.Duration(req.Duration) * time.Second)
		until = &t
	}
	var appID *int64
	if req.AppID > 0 {
		appID = &req.AppID
	}
	addList := func(target, listType, value string) {
		record := securitydomain.Enforcement{Target: target, Subject: value, Until: until}
		if value == "" {
			record.Detail = "缺少对象"
			out = append(out, record)
			return
		}
		var createdBy *int64
		if req.OperatorID > 0 {
			createdBy = &req.OperatorID
		}
		_, _, entries, err := s.pg.UpsertRiskListEntries(ctx, securitydomain.ListEntryInput{
			AppID: listScopeFor(listType, appID), ListType: listType, Values: []string{normalizeListValue(listType, value)},
			Kind: req.Kind, Reason: req.Reason, ExpiresAt: until, Source: req.Source, CreatedBy: createdBy, AssessmentID: req.AssessmentID,
		})
		if err != nil {
			record.Detail = err.Error()
		} else {
			record.OK = true
			record.Detail = "已加入" + catalogLabel(securitydomain.ListKindCatalog(), req.Kind)
			if len(entries) > 0 {
				record.RefID = entries[0].ID
				s.syncEntityTags(ctx, entries, false)
			}
		}
		out = append(out, record)
	}

	for _, target := range req.Targets {
		switch target {
		case securitydomain.BanTargetDevice:
			addList(target, securitydomain.ListTypeDevice, req.DeviceID)
		case securitydomain.BanTargetIP:
			addList(target, securitydomain.ListTypeIP, req.IP)
		case securitydomain.BanTargetAccount:
			if req.Kind != securitydomain.ListKindBlock {
				// 加白 / 观察：落到名单上；能认出用户时按用户 ID，否则按登录名
				if req.UserID > 0 {
					addList(target, securitydomain.ListTypeUser, strconv.FormatInt(req.UserID, 10))
				} else {
					addList(target, securitydomain.ListTypeAccount, req.Account)
				}
				continue
			}
			out = append(out, s.banAccount(ctx, bans, req, until))
		case securitydomain.BanTargetFirewall:
			record := securitydomain.Enforcement{Target: target, Subject: req.IP, Until: until}
			switch {
			case req.Kind != securitydomain.ListKindBlock:
				record.Detail = "防火墙只支持封禁"
			case req.IP == "":
				record.Detail = "缺少 IP"
			case firewall == nil:
				record.Detail = "防火墙未接入"
			default:
				trigger := "risk"
				if req.AssessmentID != nil {
					trigger = fmt.Sprintf("risk:assessment:%d", *req.AssessmentID)
				}
				ban, err := firewall.BanIPWithOptions(ctx, req.IP, req.Reason, int64(req.Duration), req.OperatorID,
					BanIPOptions{Source: "risk", TriggerRule: trigger})
				if err != nil {
					record.Detail = err.Error()
				} else {
					record.OK = true
					record.RefID = ban.ID
					record.Detail = "防火墙已封禁"
				}
			}
			out = append(out, record)
		default:
			out = append(out, securitydomain.Enforcement{Target: target, Detail: "未知处置对象"})
		}
	}
	for _, record := range out {
		if !record.OK {
			s.logWarn("风控处置未生效", zap.String("target", record.Target), zap.String("subject", record.Subject), zap.String("detail", record.Detail))
		}
	}
	s.invalidateSnapshot()
	return out
}

// listScopeFor 处置写名单时的作用域：账号与用户 ID 只在本应用内有意义，设备与 IP 是全平台的物理实体
func listScopeFor(listType string, appID *int64) *int64 {
	switch listType {
	case securitydomain.ListTypeAccount, securitydomain.ListTypeUser:
		return appID
	}
	return nil
}

func (s *RiskService) banAccount(ctx context.Context, bans riskAccountBanner, req enforceRequest, until *time.Time) securitydomain.Enforcement {
	record := securitydomain.Enforcement{Target: securitydomain.BanTargetAccount, Subject: req.Account, Until: until}
	userID := req.UserID
	if userID <= 0 && req.AppID > 0 && req.Account != "" {
		if id, createdAt, err := s.pg.RiskUserFacts(ctx, req.AppID, 0, req.Account); err == nil && createdAt != nil {
			userID = id
		}
	}
	if userID > 0 {
		record.Subject = fmt.Sprintf("%s #%d", req.Account, userID)
	}
	switch {
	case bans == nil:
		record.Detail = "账号封禁服务未接入"
		return record
	case req.AppID <= 0 || userID <= 0:
		record.Detail = "认不出用户，未封禁账号"
		return record
	}
	banType := userdomain.AccountBanTypeTemporary
	if until == nil {
		banType = userdomain.AccountBanTypePermanent
	}
	evidence := map[string]any{"source": "risk"}
	if req.AssessmentID != nil {
		evidence["assessmentId"] = *req.AssessmentID
	}
	operatorName := req.OperatorName
	if operatorName == "" {
		operatorName = "风控中心"
	}
	ban, err := bans.BanUser(ctx, req.AppID, userID, userdomain.AccountBanCreateInput{
		BanType: banType, BanScope: userdomain.AccountBanScopeAll, Reason: req.Reason, Evidence: evidence,
		EndAt: until, Operator: userdomain.BanOperator{AdminID: req.OperatorID, AdminName: operatorName},
	})
	if err != nil {
		record.Detail = err.Error()
		return record
	}
	record.OK = true
	record.RefID = ban.ID
	record.Detail = "账号已封禁"
	return record
}

// Dispose 人工处置
func (s *RiskService) Dispose(ctx context.Context, input securitydomain.DisposeInput) ([]securitydomain.Enforcement, error) {
	if !isListKind(input.Kind) {
		return nil, fmt.Errorf("未知处置种类：%s", input.Kind)
	}
	if len(input.Targets) == 0 {
		return nil, fmt.Errorf("请选择处置对象")
	}
	for _, target := range input.Targets {
		if !slices.ContainsFunc(securitydomain.BanTargetCatalog(), func(e securitydomain.CatalogEntry) bool { return e.Value == target }) {
			return nil, fmt.Errorf("未知处置对象：%s", target)
		}
	}
	if input.Duration < 0 {
		return nil, fmt.Errorf("时长不能为负")
	}
	req := enforceRequest{
		AssessmentID: input.AssessmentID, Account: strings.TrimSpace(input.Account), IP: strings.TrimSpace(input.IP),
		DeviceID: strings.TrimSpace(input.DeviceID), Targets: input.Targets, Kind: input.Kind, Duration: input.Duration,
		Reason: strings.TrimSpace(input.Reason), Source: firstNonEmptyString(input.Source, "dispose"),
		OperatorID: input.OperatorID, OperatorName: input.OperatorName,
	}
	if input.AppID != nil {
		req.AppID = *input.AppID
	}
	if input.UserID != nil {
		req.UserID = *input.UserID
	}
	// 从评估记录发起时，缺的对象从记录里补
	if input.AssessmentID != nil {
		if a, err := s.pg.GetRiskAssessment(ctx, *input.AssessmentID); err == nil && a != nil {
			fillFromAssessment(&req, a)
		}
	}
	if req.Reason == "" {
		req.Reason = "风控中心人工处置"
	}
	records := s.enforce(ctx, req)
	if input.AssessmentID != nil {
		if err := s.pg.AppendAssessmentEnforcement(ctx, *input.AssessmentID, records); err != nil {
			s.logWarn("写入处置留痕失败", zap.Error(err))
		}
	}
	return records, nil
}

func fillFromAssessment(req *enforceRequest, a *securitydomain.RiskAssessment) {
	if req.AppID == 0 && a.AppID != nil {
		req.AppID = *a.AppID
	}
	if req.UserID == 0 && a.UserID != nil {
		req.UserID = *a.UserID
	}
	if req.Account == "" {
		req.Account = a.Account
	}
	if req.IP == "" {
		req.IP = a.IP
	}
	if req.DeviceID == "" {
		req.DeviceID = a.DeviceID
	}
}

// ReviewRiskAssessment 复核。驳回 = 按所选对象处置；通过 = 可选加白、撤销这条记录触发的自动封禁。
// 复核结论不落到对象上，同一个人下一次请求还会被同样地拦下或放过。
func (s *RiskService) ReviewRiskAssessment(ctx context.Context, id, reviewerID int64, reviewerName string, input securitydomain.ReviewInput) ([]securitydomain.Enforcement, error) {
	result := strings.TrimSpace(input.Result)
	if result != "approved" && result != "rejected" {
		return nil, fmt.Errorf("复核结果只能是 approved 或 rejected")
	}
	assessment, err := s.pg.GetRiskAssessment(ctx, id)
	if err != nil {
		return nil, err
	}
	if assessment == nil {
		return nil, fmt.Errorf("评估记录不存在")
	}
	if err := s.pg.ReviewRiskAssessment(ctx, id, reviewerID, result, input.Comment); err != nil {
		return nil, err
	}

	req := enforceRequest{AssessmentID: &id, Duration: input.Duration, Source: "review", OperatorID: reviewerID, OperatorName: reviewerName}
	fillFromAssessment(&req, assessment)
	records := []securitydomain.Enforcement{}

	if result == "rejected" {
		targets := input.Targets
		if len(targets) == 0 {
			targets = []string{securitydomain.BanTargetDevice, securitydomain.BanTargetIP}
		}
		req.Targets = targets
		req.Kind = securitydomain.ListKindBlock
		req.Reason = fmt.Sprintf("复核驳回 #%d：%s", id, firstNonEmptyString(input.Comment, sceneLabel(assessment.Scene)))
		records = append(records, s.enforce(ctx, req)...)
	} else {
		if input.RevokeAuto {
			records = append(records, s.revokeAutoEnforcement(ctx, assessment, reviewerID)...)
		}
		if len(input.Targets) > 0 {
			req.Targets = input.Targets
			req.Kind = securitydomain.ListKindAllow
			req.Reason = fmt.Sprintf("复核通过 #%d：%s", id, firstNonEmptyString(input.Comment, sceneLabel(assessment.Scene)))
			records = append(records, s.enforce(ctx, req)...)
		}
	}
	if err := s.pg.AppendAssessmentEnforcement(ctx, id, records); err != nil {
		s.logWarn("写入复核处置留痕失败", zap.Error(err))
	}
	return records, nil
}

// revokeAutoEnforcement 撤销一条评估记录当时自动执行的封禁：名单、账号封禁、防火墙封禁
func (s *RiskService) revokeAutoEnforcement(ctx context.Context, a *securitydomain.RiskAssessment, operatorID int64) []securitydomain.Enforcement {
	bans, firewall, _ := s.executors()
	out := []securitydomain.Enforcement{}
	removed, err := s.pg.DeleteRiskListEntriesByAssessment(ctx, a.ID, securitydomain.ListKindBlock)
	if err != nil {
		out = append(out, securitydomain.Enforcement{Target: "list", Detail: "撤销名单失败：" + err.Error()})
	} else {
		s.syncEntityTags(ctx, removed, true)
		for _, entry := range removed {
			out = append(out, securitydomain.Enforcement{Target: entry.ListType, Subject: entry.Value, OK: true, RefID: entry.ID, Detail: "已撤销黑名单"})
		}
	}
	for _, record := range a.Enforcement {
		if !record.OK || record.RefID == 0 {
			continue
		}
		switch record.Target {
		case securitydomain.BanTargetAccount:
			if bans == nil || a.AppID == nil || a.UserID == nil {
				continue
			}
			item := securitydomain.Enforcement{Target: record.Target, Subject: record.Subject, RefID: record.RefID}
			if _, err := bans.RevokeBan(ctx, *a.AppID, *a.UserID, record.RefID, userdomain.AccountBanRevokeInput{
				Reason: fmt.Sprintf("风控复核通过 #%d", a.ID), Operator: userdomain.BanOperator{AdminID: operatorID, AdminName: "风控中心"},
			}); err != nil {
				item.Detail = "解除账号封禁失败：" + err.Error()
			} else {
				item.OK = true
				item.Detail = "已解除账号封禁"
			}
			out = append(out, item)
		case securitydomain.BanTargetFirewall:
			if firewall == nil {
				continue
			}
			item := securitydomain.Enforcement{Target: record.Target, Subject: record.Subject, RefID: record.RefID}
			if err := firewall.UnbanIP(ctx, record.RefID, operatorID); err != nil {
				item.Detail = "解除防火墙封禁失败：" + err.Error()
			} else {
				item.OK = true
				item.Detail = "已解除防火墙封禁"
			}
			out = append(out, item)
		}
	}
	s.invalidateSnapshot()
	return out
}

type riskCaptchaKey struct{}

// WithRiskCaptchaPassed 标记这次请求已经通过了图形验证码。由接入层在验证码校验成功后写入上下文，
// 认证链路无须逐层加参数，风控评估自己从上下文里读。
func WithRiskCaptchaPassed(ctx context.Context) context.Context {
	return context.WithValue(ctx, riskCaptchaKey{}, true)
}

func riskCaptchaPassed(ctx context.Context) bool {
	passed, _ := ctx.Value(riskCaptchaKey{}).(bool)
	return passed
}

// riskHook 业务服务接入风控的统一挂点：嵌进服务结构体，装配时 SetRiskService，
// 没接（测试、离线工具）时 guard 直接放行。
type riskHook struct {
	risk *RiskService
}

// SetRiskService 接入风控中心
func (h *riskHook) SetRiskService(risk *RiskService) { h.risk = risk }

func (h *riskHook) riskGuard(ctx context.Context, req securitydomain.RiskEvalRequest, opts ...RiskGuardOption) error {
	if h == nil || h.risk == nil {
		return nil
	}
	_, err := h.risk.Guard(ctx, req, opts...)
	return err
}
