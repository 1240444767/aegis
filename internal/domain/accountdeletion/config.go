package accountdeletion

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// DefaultNotice 默认的注销须知。管理员可以整段改写。
const DefaultNotice = `注销是不可恢复的操作，请在提交前确认以下事项：

1. 账号注销后，账号名、昵称、头像、绑定的邮箱与手机号、第三方登录关联将全部解除并删除。
2. 会员权益、积分、等级、签到记录、已解锁的功能与云端数据将一并清除，且无法找回。
3. 钱包余额与未完成的订单请在注销前处理完毕，注销后不予退还。
4. 依照法律法规需要留存的交易记录会脱离账号后按规定期限保存。
5. 选择冷静期注销时，冷静期内可以随时撤销；期满后账号将被自动注销。`

// DefaultReasons 默认的注销原因。
func DefaultReasons() []Reason {
	return []Reason{
		{Code: "no_longer_used", Label: "不再使用"},
		{Code: "duplicate_account", Label: "已有其他账号"},
		{Code: "privacy", Label: "担心隐私安全"},
		{Code: "bad_experience", Label: "体验不佳"},
		{Code: "too_many_notifications", Label: "消息打扰太多"},
		{Code: "other", Label: "其他原因"},
	}
}

// DefaultConfig 没保存过配置时的默认值；控制台第一次打开时展示的也是它。
func DefaultConfig() Config {
	return Config{
		AllowImmediate:     false,
		AllowCooling:       true,
		DefaultMode:        ModeCooling,
		CoolingDays:        DefaultCoolingDays,
		VerifyMethods:      []string{VerifyPassword, VerifyEmail, VerifyPhone},
		RequireTOTP:        true,
		RecentLoginMinutes: 10,
		Reasons:            DefaultReasons(),
		AllowCustomReason:  true,
		NoticeTitle:        "注销须知",
		NoticeContent:      DefaultNotice,
		AgreementLabel:     "我已阅读并同意注销须知，知晓注销后数据无法恢复",
		Guards: Guards{
			ActiveBan:      GuardBlock,
			PendingOrders:  GuardBlock,
			PendingRefunds: GuardBlock,
			WalletBalance:  GuardWarn,
			ActiveVip:      GuardWarn,
			LifetimeVip:    GuardWarn,
			Points:         GuardOff,
		},
		LoginDuringCooling: LoginKeep,
		SignOutOthers:      true,
		RemindBeforeHours:  24,
		ReregisterDays:     0,
		NotifyEmail:        true,
	}
}

// Modes / VerifyMethodList / GuardLevels / LoginBehaviors 枚举，顺序即控制台的展示顺序。
func Modes() []string            { return []string{ModeCooling, ModeImmediate} }
func VerifyMethodList() []string { return []string{VerifyPassword, VerifyEmail, VerifyPhone} }
func GuardLevels() []string      { return []string{GuardOff, GuardWarn, GuardBlock} }
func LoginBehaviors() []string   { return []string{LoginKeep, LoginCancel} }
func Statuses() []string {
	return []string{StatusReviewing, StatusScheduled, StatusProcessing, StatusCompleted, StatusCancelled, StatusRejected, StatusFailed}
}

// GuardKeys 前置条件的键，顺序即展示顺序。
func GuardKeys() []string {
	return []string{GuardActiveBan, GuardPendingOrders, GuardPendingRefunds, GuardWalletBalance, GuardActiveVip, GuardLifetimeVip, GuardPoints}
}

// Level 按键取一项前置条件的档位。
func (g Guards) Level(key string) string {
	switch key {
	case GuardActiveBan:
		return g.ActiveBan
	case GuardPendingOrders:
		return g.PendingOrders
	case GuardPendingRefunds:
		return g.PendingRefunds
	case GuardWalletBalance:
		return g.WalletBalance
	case GuardActiveVip:
		return g.ActiveVip
	case GuardLifetimeVip:
		return g.LifetimeVip
	case GuardPoints:
		return g.Points
	}
	return GuardOff
}

func (g *Guards) set(key, level string) {
	switch key {
	case GuardActiveBan:
		g.ActiveBan = level
	case GuardPendingOrders:
		g.PendingOrders = level
	case GuardPendingRefunds:
		g.PendingRefunds = level
	case GuardWalletBalance:
		g.WalletBalance = level
	case GuardActiveVip:
		g.ActiveVip = level
	case GuardLifetimeVip:
		g.LifetimeVip = level
	case GuardPoints:
		g.Points = level
	}
}

var reasonCodePattern = regexp.MustCompile(`^[a-z0-9_\-]{1,64}$`)

// NormalizeConfig 清洗：去空白、补默认值、去重。不做范围校验（交给 ValidateConfig 报错）。
func NormalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	cfg.DefaultMode = strings.TrimSpace(cfg.DefaultMode)
	if cfg.CoolingDays == 0 {
		cfg.CoolingDays = defaults.CoolingDays
	}
	// 只剩一种方式时默认方式就是它
	switch {
	case cfg.AllowCooling && !cfg.AllowImmediate:
		cfg.DefaultMode = ModeCooling
	case cfg.AllowImmediate && !cfg.AllowCooling:
		cfg.DefaultMode = ModeImmediate
	case cfg.DefaultMode == "":
		cfg.DefaultMode = ModeCooling
	}

	methods := make([]string, 0, len(cfg.VerifyMethods))
	for _, method := range cfg.VerifyMethods {
		method = strings.ToLower(strings.TrimSpace(method))
		if method != "" && !slices.Contains(methods, method) {
			methods = append(methods, method)
		}
	}
	// 按目录顺序排，App 上的选项顺序稳定
	slices.SortStableFunc(methods, func(a, b string) int {
		return slices.Index(VerifyMethodList(), a) - slices.Index(VerifyMethodList(), b)
	})
	cfg.VerifyMethods = methods

	reasons := make([]Reason, 0, len(cfg.Reasons))
	for _, reason := range cfg.Reasons {
		reason.Code = strings.ToLower(strings.TrimSpace(reason.Code))
		reason.Label = strings.TrimSpace(reason.Label)
		if reason.Code == "" && reason.Label == "" {
			continue
		}
		if slices.ContainsFunc(reasons, func(r Reason) bool { return r.Code == reason.Code }) {
			continue
		}
		reasons = append(reasons, reason)
	}
	cfg.Reasons = reasons

	cfg.NoticeTitle = strings.TrimSpace(cfg.NoticeTitle)
	if cfg.NoticeTitle == "" {
		cfg.NoticeTitle = defaults.NoticeTitle
	}
	cfg.NoticeContent = strings.TrimSpace(strings.ReplaceAll(cfg.NoticeContent, "\r\n", "\n"))
	if cfg.NoticeContent == "" {
		cfg.NoticeContent = defaults.NoticeContent
	}
	cfg.AgreementLabel = strings.TrimSpace(cfg.AgreementLabel)
	if cfg.AgreementLabel == "" {
		cfg.AgreementLabel = defaults.AgreementLabel
	}
	cfg.SupportContact = strings.TrimSpace(cfg.SupportContact)

	for _, key := range GuardKeys() {
		level := strings.ToLower(strings.TrimSpace(cfg.Guards.Level(key)))
		if level == "" {
			level = defaults.Guards.Level(key)
		}
		cfg.Guards.set(key, level)
	}
	cfg.LoginDuringCooling = strings.TrimSpace(cfg.LoginDuringCooling)
	if cfg.LoginDuringCooling == "" {
		cfg.LoginDuringCooling = defaults.LoginDuringCooling
	}
	return cfg
}

// ValidateConfig 范围与枚举校验，返回的错误原话可以直接展示给管理员。
func ValidateConfig(cfg Config) error {
	if !cfg.AllowImmediate && !cfg.AllowCooling {
		return fmt.Errorf("立即注销与冷静期注销至少开放一种")
	}
	if !slices.Contains(Modes(), cfg.DefaultMode) {
		return fmt.Errorf("默认注销方式只能是 %s", strings.Join(Modes(), " / "))
	}
	if (cfg.DefaultMode == ModeCooling && !cfg.AllowCooling) || (cfg.DefaultMode == ModeImmediate && !cfg.AllowImmediate) {
		return fmt.Errorf("默认注销方式必须是已开放的方式")
	}
	if cfg.CoolingDays < MinCoolingDays || cfg.CoolingDays > MaxCoolingDays {
		return fmt.Errorf("冷静期为 %d–%d 天", MinCoolingDays, MaxCoolingDays)
	}
	for _, method := range cfg.VerifyMethods {
		if !slices.Contains(VerifyMethodList(), method) {
			return fmt.Errorf("不支持的验证方式 %q", method)
		}
	}
	if cfg.RecentLoginMinutes < 0 || cfg.RecentLoginMinutes > MaxRecentLoginMinutes {
		return fmt.Errorf("最近登录放行时限为 0–%d 分钟", MaxRecentLoginMinutes)
	}
	if len(cfg.VerifyMethods) == 0 && cfg.RecentLoginMinutes == 0 {
		return fmt.Errorf("至少选择一种验证方式，或允许凭最近登录放行")
	}
	if len(cfg.Reasons) > MaxReasons {
		return fmt.Errorf("注销原因最多 %d 项", MaxReasons)
	}
	for _, reason := range cfg.Reasons {
		if !reasonCodePattern.MatchString(reason.Code) {
			return fmt.Errorf("原因标识 %q 只能由小写字母、数字、下划线与连字符组成（1–64 位）", reason.Code)
		}
		if reason.Label == "" || utf8.RuneCountInString(reason.Label) > MaxReasonLabelRunes {
			return fmt.Errorf("原因「%s」的名称不能为空且不超过 %d 字", reason.Code, MaxReasonLabelRunes)
		}
	}
	if cfg.ReasonRequired && len(cfg.Reasons) == 0 && !cfg.AllowCustomReason {
		return fmt.Errorf("要求填写原因时，至少提供一个可选原因或允许自填")
	}
	if utf8.RuneCountInString(cfg.NoticeTitle) > MaxNoticeTitleRunes {
		return fmt.Errorf("须知标题不超过 %d 字", MaxNoticeTitleRunes)
	}
	if utf8.RuneCountInString(cfg.NoticeContent) > MaxNoticeRunes {
		return fmt.Errorf("须知正文不超过 %d 字", MaxNoticeRunes)
	}
	if utf8.RuneCountInString(cfg.AgreementLabel) > MaxAgreementRunes {
		return fmt.Errorf("同意勾选文案不超过 %d 字", MaxAgreementRunes)
	}
	if utf8.RuneCountInString(cfg.SupportContact) > MaxSupportRunes {
		return fmt.Errorf("客服联系方式不超过 %d 字", MaxSupportRunes)
	}
	for _, key := range GuardKeys() {
		if !slices.Contains(GuardLevels(), cfg.Guards.Level(key)) {
			return fmt.Errorf("前置条件 %s 的处置只能是 off / warn / block", key)
		}
	}
	if !slices.Contains(LoginBehaviors(), cfg.LoginDuringCooling) {
		return fmt.Errorf("冷静期内登录的处理只能是 keep / cancel")
	}
	if cfg.RemindBeforeHours < 0 || cfg.RemindBeforeHours > MaxRemindBeforeHours {
		return fmt.Errorf("到期提醒为 0–%d 小时", MaxRemindBeforeHours)
	}
	if cfg.ReregisterDays < 0 || cfg.ReregisterDays > MaxReregisterDays {
		return fmt.Errorf("重新注册限制为 0–%d 天", MaxReregisterDays)
	}
	return nil
}
