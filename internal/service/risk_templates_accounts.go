package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/timeutil"
)

// ════════════════════════════════════════════════════════════
//  规则模板
// ════════════════════════════════════════════════════════════

// ListTemplates 模板详情（含按作用域算出的「已安装」）
func (s *RiskService) ListTemplates(ctx context.Context, appID *int64) ([]securitydomain.TemplateDetail, error) {
	installed, err := s.pg.ListRiskRuleTemplateKeys(ctx, appID)
	if err != nil {
		return nil, err
	}
	summaries := securitydomain.TemplateSummaries()
	out := make([]securitydomain.TemplateDetail, 0, len(summaries))
	for i, tpl := range securitydomain.RiskTemplates() {
		detail := securitydomain.TemplateDetail{TemplateSummary: summaries[i], ActionItems: tpl.Actions}
		if detail.ActionItems == nil {
			detail.ActionItems = []securitydomain.CreateRiskActionInput{}
		}
		for _, rule := range tpl.Rules {
			key := securitydomain.TemplateRuleKey(tpl.Key, rule.Key)
			view := securitydomain.TemplateRuleView{
				Key: rule.Key, Name: rule.Name, Scene: rule.Scene, ConditionType: rule.ConditionType,
				ConditionData: rule.ConditionData, Score: rule.Score, Decision: rule.Decision,
				Dimension:   securitydomain.ConditionDimension(rule.ConditionType, rule.ConditionData),
				Explanation: explainRule(securitydomain.RiskRule{ConditionType: rule.ConditionType, ConditionData: rule.ConditionData, Score: rule.Score}),
				Installed:   installed[key],
			}
			if view.Installed {
				detail.Installed++
			}
			detail.Items = append(detail.Items, view)
		}
		out = append(out, detail)
	}
	return out, nil
}

// ApplyTemplate 安装模板。已经装过的规则（同作用域、同模板键）跳过；
// 附带的处置策略与已有区间冲突时跳过并说明，绝不覆盖管理员自己配的策略。
func (s *RiskService) ApplyTemplate(ctx context.Context, input securitydomain.TemplateApplyInput, createdBy int64) (*securitydomain.TemplateApplyResult, error) {
	tpl, ok := securitydomain.FindTemplate(input.Key)
	if !ok {
		return nil, fmt.Errorf("未知规则模板：%s", input.Key)
	}
	installed, err := s.pg.ListRiskRuleTemplateKeys(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	result := &securitydomain.TemplateApplyResult{ActionsSkipped: []string{}}
	mode := securitydomain.RuleModeEnforce
	if input.Observe {
		mode = securitydomain.RuleModeObserve
	}
	for _, rule := range tpl.Rules {
		key := securitydomain.TemplateRuleKey(tpl.Key, rule.Key)
		if installed[key] {
			result.RulesSkipped++
			continue
		}
		data := map[string]any{}
		for k, v := range rule.ConditionData {
			data[k] = v
		}
		if _, err := s.CreateRiskRule(ctx, securitydomain.CreateRiskRuleInput{
			AppID: input.AppID, Name: rule.Name, Description: firstNonEmptyString(rule.Description, tpl.Name+"模板"),
			Scene: rule.Scene, ConditionType: rule.ConditionType, ConditionData: data, Score: rule.Score,
			Priority: rule.Priority, Mode: mode, Decision: rule.Decision, TemplateKey: key,
		}, createdBy); err != nil {
			return result, fmt.Errorf("安装规则「%s」失败：%w", rule.Name, err)
		}
		result.RulesCreated++
	}
	if input.WithActions {
		for _, action := range tpl.Actions {
			action.AppID = input.AppID
			if _, err := s.CreateRiskAction(ctx, action); err != nil {
				result.ActionsSkipped = append(result.ActionsSkipped, fmt.Sprintf("%s %d–%s：%s",
					sceneLabel(action.Scene), action.MinScore, formatMaxScore(action.MaxScore), err.Error()))
				continue
			}
			result.ActionsCreated++
		}
	}
	s.invalidateSnapshot()
	return result, nil
}

// ════════════════════════════════════════════════════════════
//  账号画像
// ════════════════════════════════════════════════════════════

// ListAccountProfiles 账号画像列表
func (s *RiskService) ListAccountProfiles(ctx context.Context, query securitydomain.AccountQuery) ([]securitydomain.AccountRiskProfile, int64, error) {
	items, total, err := s.pg.ListAccountRiskProfiles(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	s.markListed(ctx, items)
	return items, total, nil
}

// markListed 给账号行标上现行名单种类
func (s *RiskService) markListed(ctx context.Context, items []securitydomain.AccountRiskProfile) {
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return
	}
	now := timeutil.Now()
	for i := range items {
		subject := riskListSubject{Account: items[i].Account, Scene: securitydomain.SceneAll}
		if items[i].AppID != nil {
			subject.AppID = *items[i].AppID
		}
		if items[i].UserID != nil {
			subject.UserID = *items[i].UserID
		}
		for _, entry := range snap.lists.match(subject, now) {
			if entry.ListType != securitydomain.ListTypeAccount && entry.ListType != securitydomain.ListTypeUser {
				continue
			}
			if entry.Kind == securitydomain.ListKindBlock || items[i].Listed == "" {
				items[i].Listed = entry.Kind
			}
		}
	}
}

// GetAccountDetail 账号详情：画像、最近评估、用过的 IP 与设备、名单、维度得分、账号封禁状态
func (s *RiskService) GetAccountDetail(ctx context.Context, appID *int64, account string, userID *int64) (*securitydomain.AccountRiskDetail, error) {
	account = strings.TrimSpace(account)
	if account == "" && (userID == nil || *userID <= 0) {
		return nil, fmt.Errorf("请指定账号或用户")
	}
	profile, err := s.pg.GetAccountRiskProfile(ctx, appID, account, userID)
	if err != nil {
		return nil, err
	}
	if profile.UserID == nil && profile.AppID != nil && profile.Account != "" {
		if id, createdAt, err := s.pg.RiskUserFacts(ctx, *profile.AppID, 0, profile.Account); err == nil && createdAt != nil {
			profile.UserID = &id
		}
	}
	scopeApp := appID
	if scopeApp == nil {
		scopeApp = profile.AppID
	}
	detail := &securitydomain.AccountRiskDetail{Profile: *profile}
	if detail.Recent, err = s.pg.ListAccountAssessments(ctx, scopeApp, profile.Account, profile.UserID, 30); err != nil {
		return nil, err
	}
	detail.IPs, detail.Devices, _ = s.pg.ListAccountPeers(ctx, scopeApp, profile.Account, profile.UserID, 20)
	detail.DimensionScores, _ = s.pg.AccountDimensionScores(ctx, scopeApp, profile.Account, profile.UserID)
	pairs := map[string][]string{
		securitydomain.ListTypeAccount: {strings.ToLower(profile.Account)},
		securitydomain.ListTypeDevice:  detail.Devices,
		securitydomain.ListTypeIP:      detail.IPs,
	}
	if profile.UserID != nil {
		pairs[securitydomain.ListTypeUser] = []string{strconv.FormatInt(*profile.UserID, 10)}
	}
	detail.ListEntries, _ = s.pg.FindRiskListEntriesFor(ctx, scopeApp, pairs)
	if bans, _, _ := s.executors(); bans != nil && scopeApp != nil && profile.UserID != nil {
		if ban, err := bans.GetActiveBan(ctx, *scopeApp, *profile.UserID); err == nil && ban != nil {
			detail.ActiveBan = &securitydomain.AccountBanRef{
				ID: ban.ID, BanType: ban.BanType, BanScope: ban.BanScope, Reason: ban.Reason, EndAt: ban.EndAt, StartAt: ban.StartAt,
			}
		}
	}
	if detail.Recent == nil {
		detail.Recent = []securitydomain.RiskAssessment{}
	}
	if detail.IPs == nil {
		detail.IPs = []string{}
	}
	if detail.Devices == nil {
		detail.Devices = []string{}
	}
	if detail.ListEntries == nil {
		detail.ListEntries = []securitydomain.RiskListEntry{}
	}
	if detail.DimensionScores == nil {
		detail.DimensionScores = map[string]int{}
	}
	return detail, nil
}

// PurgeExpiredLists 清掉过期超过 30 天的名单
func (s *RiskService) PurgeExpiredLists(ctx context.Context) (int64, error) {
	return s.pg.PurgeExpiredRiskLists(ctx, timeutil.NowUTC().Add(-30*24*time.Hour))
}
