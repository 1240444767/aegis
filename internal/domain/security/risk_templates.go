package security

import "slices"

// 规则模板：一键装上一组「开箱即用」的规则与处置策略。
//
// 没有模板时风控中心的初始状态是「零规则」——每一次评估都是 0 分放行，
// 大盘上一片正常，管理员会把「根本没有防线」误读成「没有风险」。
// 模板里的每一条都是可编辑的普通规则，装完即与模板脱钩；TemplateKey
// 只用来在重复安装时跳过已经装过的那条。

// RiskTemplate 一组规则与策略
type RiskTemplate struct {
	Key         string
	Name        string
	Description string
	Rules       []RiskTemplateRule
	Actions     []CreateRiskActionInput
}

// RiskTemplateRule 模板里的一条规则。Key 在模板内唯一，与模板 Key 拼成 TemplateKey。
type RiskTemplateRule struct {
	Key           string
	Name          string
	Description   string
	Scene         string
	ConditionType string
	ConditionData map[string]any
	Score         int
	Priority      int
	Decision      string
}

// TemplateRuleKey 规则的模板来源键
func TemplateRuleKey(templateKey, ruleKey string) string {
	return templateKey + ":" + ruleKey
}

// RiskTemplates 内置模板
func RiskTemplates() []RiskTemplate {
	return []RiskTemplate{
		{
			Key:         "baseline",
			Name:        "基础防护",
			Description: "全场景通用：机器人、缺失 UA、代理与 Tor、低信誉 IP、高频访问、观察名单与历史高危账号，附三档分数策略",
			Rules: []RiskTemplateRule{
				{Key: "ua_bot", Name: "机器人客户端", Scene: SceneAll, ConditionType: CondUABot, ConditionData: map[string]any{}, Score: 40, Priority: 10},
				{Key: "ua_missing", Name: "缺失或异常 UA", Scene: SceneAll, ConditionType: CondUAMissing, ConditionData: map[string]any{"min_length": 16}, Score: 20, Priority: 20},
				{Key: "ip_proxy", Name: "代理 / VPN / Tor 出口", Scene: SceneAll, ConditionType: CondIPProxy, ConditionData: map[string]any{"include_tor": true, "include_datacenter": false}, Score: 25, Priority: 30},
				{Key: "ip_reputation", Name: "低信誉 IP", Scene: SceneAll, ConditionType: CondIPReputation, ConditionData: map[string]any{"min_score": 75}, Score: 30, Priority: 40},
				{Key: "ip_frequency", Name: "IP 高频访问", Scene: SceneAll, ConditionType: CondIPFrequency, ConditionData: map[string]any{"threshold": 60, "dimension": "ip"}, Score: 25, Priority: 50},
				{Key: "list_watch", Name: "命中观察名单", Scene: SceneAll, ConditionType: CondListWatch, ConditionData: map[string]any{}, Score: 30, Priority: 60},
				{Key: "risk_trail", Name: "账号近期多次高危", Scene: SceneAll, ConditionType: CondAccountRiskTrail, ConditionData: map[string]any{"threshold": 3}, Score: 20, Priority: 70},
			},
			Actions: []CreateRiskActionInput{
				{Scene: SceneAll, MinScore: 41, MaxScore: new(60), Action: ActionCaptcha, Description: "中危：人机验证"},
				{Scene: SceneAll, MinScore: 61, MaxScore: new(80), Action: ActionReview, Description: "高危：放行并进入人工复核"},
				{Scene: SceneAll, MinScore: 81, Action: ActionBlock, Description: "严重：拦截"},
			},
		},
		{
			Key:         "account",
			Name:        "账号安全",
			Description: "登录、注册、找回密码与短信验证码：撞库、批量注册、临时邮箱、异地登录、短信轰炸",
			Rules: []RiskTemplateRule{
				{Key: "credential_stuffing", Name: "撞库：同 IP 多账号登录", Scene: SceneLogin, ConditionType: CondAccountVelocity, ConditionData: map[string]any{"dimension": "ip", "threshold": 8}, Score: 40, Priority: 10},
				{Key: "login_rate", Name: "单账号登录过频", Scene: SceneLogin, ConditionType: CondRateLimited, ConditionData: map[string]any{"dimension": "account"}, Score: 30, Priority: 20},
				{Key: "login_ip_spread", Name: "账号多 IP 漂移", Scene: SceneLogin, ConditionType: CondAccountIPSpread, ConditionData: map[string]any{"threshold": 6}, Score: 20, Priority: 30},
				{Key: "login_geo_jump", Name: "登录归属地跳变", Scene: SceneLogin, ConditionType: CondGeoJump, ConditionData: map[string]any{}, Score: 20, Priority: 40},
				{Key: "register_shared_device", Name: "同设备批量注册", Scene: SceneRegister, ConditionType: CondDeviceShared, ConditionData: map[string]any{"threshold": 4}, Score: 40, Priority: 10},
				{Key: "register_ip_burst", Name: "同 IP 批量注册", Scene: SceneRegister, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "ip", "threshold": 10}, Score: 40, Priority: 20},
				{Key: "register_datacenter", Name: "机房 IP 注册", Scene: SceneRegister, ConditionType: CondCustomExpr, ConditionData: map[string]any{"expression": "ip_is_datacenter and not ip_trusted", "dimension": DimNetwork}, Score: 30, Priority: 30},
				{Key: "register_temp_mail", Name: "临时邮箱注册", Scene: SceneRegister, ConditionType: CondCustomExpr, ConditionData: map[string]any{
					"expression": `contains_any(email_domain, ["mailinator", "tempmail", "temp-mail", "10minutemail", "guerrillamail", "yopmail", "sharklasers", "trashmail"])`,
					"dimension":  DimAccount,
				}, Score: 45, Priority: 40},
				{Key: "reset_burst", Name: "找回密码过频", Scene: ScenePasswordReset, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "subject", "threshold": 5}, Score: 45, Priority: 10},
				{Key: "sms_ip_bomb", Name: "短信轰炸：同 IP", Scene: SceneVerifyCode, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "ip", "threshold": 20}, Score: 60, Priority: 10, Decision: ActionBlock},
				{Key: "sms_target_bomb", Name: "短信轰炸：同号码", Scene: SceneVerifyCode, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "subject", "threshold": 8}, Score: 50, Priority: 20},
			},
		},
		{
			Key:         "marketing",
			Name:        "营销反作弊",
			Description: "邀请、抽奖、签到、广告奖励、积分与卡密：新号薅羊毛、设备多开、高频刷取、卡密试探",
			Rules: []RiskTemplateRule{
				{Key: "fresh_account", Name: "新号参与营销活动", Scene: SceneAll, ConditionType: CondCustomExpr, ConditionData: map[string]any{
					"expression": `user_known and user_age_hours < 24 and scene in ["invite", "lottery", "ad_reward", "points_redeem", "signin"]`,
					"dimension":  DimAccount,
				}, Score: 20, Priority: 10},
				{Key: "invite_shared_device", Name: "邀请：设备多账号", Scene: SceneInvite, ConditionType: CondDeviceShared, ConditionData: map[string]any{"threshold": 3}, Score: 45, Priority: 20},
				{Key: "invite_ip_farm", Name: "邀请：同 IP 多账号", Scene: SceneInvite, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "ip", "threshold": 6}, Score: 35, Priority: 30},
				{Key: "lottery_burst", Name: "抽奖过频", Scene: SceneLottery, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "subject", "threshold": 120}, Score: 40, Priority: 20},
				{Key: "ad_reward_burst", Name: "广告奖励过频", Scene: SceneAdReward, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "device", "threshold": 40}, Score: 40, Priority: 20},
				{Key: "signin_shared_device", Name: "签到：设备多账号", Scene: SceneSignIn, ConditionType: CondDeviceShared, ConditionData: map[string]any{"threshold": 5}, Score: 35, Priority: 20},
				{Key: "cardkey_probe", Name: "卡密试探", Scene: SceneCardKey, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "ip", "threshold": 30}, Score: 50, Priority: 20},
				{Key: "points_burst", Name: "积分兑换过频", Scene: ScenePointsRedeem, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "subject", "threshold": 30}, Score: 35, Priority: 20},
				{Key: "device_proxy_pool", Name: "设备轮换代理池", Scene: SceneAll, ConditionType: CondDeviceIPSpread, ConditionData: map[string]any{"threshold": 8}, Score: 30, Priority: 30},
			},
		},
		{
			Key:         "payment",
			Name:        "支付风控",
			Description: "下单过频、新号大额、归属地跳变",
			Rules: []RiskTemplateRule{
				{Key: "order_burst", Name: "下单过频", Scene: ScenePayment, ConditionType: CondSceneVelocity, ConditionData: map[string]any{"dimension": "subject", "threshold": 20}, Score: 40, Priority: 10},
				{Key: "fresh_large", Name: "新号大额下单", Scene: ScenePayment, ConditionType: CondCustomExpr, ConditionData: map[string]any{
					"expression": `user_known and user_age_hours < 1 and extra["amount"] != nil and extra["amount"] >= 500`,
					"dimension":  DimAccount,
				}, Score: 40, Priority: 20},
				{Key: "pay_geo_jump", Name: "支付归属地跳变", Scene: ScenePayment, ConditionType: CondGeoJump, ConditionData: map[string]any{}, Score: 20, Priority: 30},
			},
		},
	}
}

// FindTemplate 按键找模板
func FindTemplate(key string) (RiskTemplate, bool) {
	for _, tpl := range RiskTemplates() {
		if tpl.Key == key {
			return tpl, true
		}
	}
	return RiskTemplate{}, false
}

// TemplateSummaries 模板摘要（下发控制台）
func TemplateSummaries() []TemplateSummary {
	templates := RiskTemplates()
	out := make([]TemplateSummary, 0, len(templates))
	for _, tpl := range templates {
		summary := TemplateSummary{
			Key: tpl.Key, Name: tpl.Name, Description: tpl.Description,
			Rules: len(tpl.Rules), Actions: len(tpl.Actions),
		}
		for _, rule := range tpl.Rules {
			if !slices.Contains(summary.Scenes, rule.Scene) {
				summary.Scenes = append(summary.Scenes, rule.Scene)
			}
			dim := ConditionDimension(rule.ConditionType, rule.ConditionData)
			if !slices.Contains(summary.Dimensions, dim) {
				summary.Dimensions = append(summary.Dimensions, dim)
			}
		}
		out = append(out, summary)
	}
	return out
}

// TemplateDetail 模板详情：逐条列出会装上什么、哪些已经装过
type TemplateDetail struct {
	TemplateSummary
	Items       []TemplateRuleView      `json:"items"`
	ActionItems []CreateRiskActionInput `json:"actionItems"`
	Installed   int                     `json:"installed"`
}

// TemplateRuleView 模板里一条规则的展示形态
type TemplateRuleView struct {
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	Scene         string         `json:"scene"`
	ConditionType string         `json:"conditionType"`
	ConditionData map[string]any `json:"conditionData"`
	Dimension     string         `json:"dimension"`
	Score         int            `json:"score"`
	Decision      string         `json:"decision,omitempty"`
	Explanation   string         `json:"explanation"`
	Installed     bool           `json:"installed"`
}
