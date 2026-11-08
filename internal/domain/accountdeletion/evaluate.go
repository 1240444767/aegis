package accountdeletion

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// EvaluateGuards 逐项判定前置条件。档位为 off 的项不出现；warn 的项不挡提交，只提示。
func EvaluateGuards(cfg Config, facts Facts, now time.Time) []Check {
	checks := make([]Check, 0, len(GuardKeys()))
	add := func(key, title string, failed bool, message, okMessage string) {
		level := cfg.Guards.Level(key)
		if level == GuardOff || level == "" {
			return
		}
		check := Check{Key: key, Title: title, Level: level, Passed: !failed, Message: okMessage}
		if failed {
			check.Message = message
		}
		checks = append(checks, check)
	}

	banMessage := "账号处于封禁中，解除封禁后才能注销"
	if facts.BanReason != "" {
		banMessage = fmt.Sprintf("账号处于封禁中（%s），解除封禁后才能注销", facts.BanReason)
	}
	add(GuardActiveBan, "账号状态正常", facts.BanActive, banMessage, "账号未被封禁")
	add(GuardPendingOrders, "没有待支付的订单", facts.PendingOrders > 0,
		fmt.Sprintf("有 %d 笔订单尚未支付完成，请先完成支付或等待订单关闭", facts.PendingOrders), "没有进行中的订单")
	add(GuardPendingRefunds, "没有处理中的退款", facts.PendingRefunds > 0,
		fmt.Sprintf("有 %d 笔退款正在处理，请等待退款完成", facts.PendingRefunds), "没有进行中的退款")
	add(GuardWalletBalance, "钱包余额已清空", facts.WalletPositive,
		fmt.Sprintf("钱包还有 %s 元余额，注销后不予退还", facts.WalletBalance), "钱包没有余额")
	vipActive := facts.VipExpireAt != nil && facts.VipExpireAt.After(now)
	vipMessage := "会员权益尚未到期，注销后剩余时长作废"
	if vipActive {
		vipMessage = fmt.Sprintf("会员权益 %s 到期，注销后剩余时长作废", facts.VipExpireAt.Format("2006-01-02"))
	}
	add(GuardActiveVip, "会员权益已到期", vipActive, vipMessage, "没有生效中的会员")
	add(GuardLifetimeVip, "不是永久会员", facts.Lifetime, "永久会员权益将随账号一并作废", "不是永久会员")
	add(GuardPoints, "积分已用完", facts.Points > 0,
		fmt.Sprintf("还有 %d 积分，注销后清零", facts.Points), "没有剩余积分")
	return checks
}

// FirstBlocking 第一项没通过且档位为 block 的前置条件；都放行时为 nil。
func FirstBlocking(checks []Check) *Check {
	for i := range checks {
		if !checks[i].Passed && checks[i].Level == GuardBlock {
			return &checks[i]
		}
	}
	return nil
}

// PlanVerification 按配置与账号的绑定情况定出要过的验证。
//
// 配置里勾了、账号上也用得了的方式任选其一；一种都用不了时按 RecentLoginMinutes 决定
// 能不能凭最近登录放行。开了两步验证且配置要求时，在此之外再过一次动态码。
func PlanVerification(cfg Config, available Availability) Verification {
	plan := Verification{Methods: []VerifyOption{}}
	for _, method := range cfg.VerifyMethods {
		switch method {
		case VerifyPassword:
			if available.Password {
				plan.Methods = append(plan.Methods, VerifyOption{Type: VerifyPassword})
			}
		case VerifyEmail:
			if available.Email != "" {
				plan.Methods = append(plan.Methods, VerifyOption{Type: VerifyEmail, Masked: available.Email})
			}
		case VerifyPhone:
			if available.Phone != "" {
				plan.Methods = append(plan.Methods, VerifyOption{Type: VerifyPhone, Masked: available.Phone})
			}
		}
	}
	plan.TOTP = cfg.RequireTOTP && available.TOTP
	switch {
	case len(plan.Methods) > 0:
		plan.Available = true
	case cfg.RecentLoginMinutes > 0:
		plan.Session = true
		plan.Available = true
		plan.Message = fmt.Sprintf("账号没有可用的验证方式，需在登录后 %d 分钟内完成注销；超时请重新登录", cfg.RecentLoginMinutes)
	case plan.TOTP:
		// 只开了两步验证：动态码本身就是一项足够强的验证
		plan.Available = true
	default:
		plan.Message = "账号没有可用于注销验证的方式，请先绑定" + verifyNouns(cfg.VerifyMethods) + "后再试"
	}
	return plan
}

func verifyNouns(methods []string) string {
	nouns := make([]string, 0, len(methods))
	for _, method := range methods {
		switch method {
		case VerifyPassword:
			nouns = append(nouns, "登录密码")
		case VerifyEmail:
			nouns = append(nouns, "邮箱")
		case VerifyPhone:
			nouns = append(nouns, "手机号")
		}
	}
	if len(nouns) == 0 {
		return "邮箱或手机号"
	}
	return strings.Join(nouns, "、")
}

// ModeOptions App 上展示的方式，默认方式排在第一位。
func ModeOptions(cfg Config) []ModeOption {
	options := make([]ModeOption, 0, 2)
	cooling := ModeOption{Mode: ModeCooling, Title: "冷静期注销",
		Description: fmt.Sprintf("提交后进入 %d 天冷静期，期间可随时撤销，期满自动注销", cfg.CoolingDays)}
	immediate := ModeOption{Mode: ModeImmediate, Title: "立即注销",
		Description: "验证通过后立即注销，账号与数据当场删除，无法撤销"}
	if cfg.RequireReview {
		cooling.Description = fmt.Sprintf("审核通过后进入 %d 天冷静期，期间可随时撤销，期满自动注销", cfg.CoolingDays)
		immediate.Description = "审核通过后立即注销，账号与数据随即删除"
	}
	if cfg.DefaultMode == ModeImmediate {
		if cfg.AllowImmediate {
			options = append(options, immediate)
		}
		if cfg.AllowCooling {
			options = append(options, cooling)
		}
		return options
	}
	if cfg.AllowCooling {
		options = append(options, cooling)
	}
	if cfg.AllowImmediate {
		options = append(options, immediate)
	}
	return options
}

// ModeAllowed 这种方式是否开放给用户。
func ModeAllowed(cfg Config, mode string) bool {
	switch mode {
	case ModeImmediate:
		return cfg.AllowImmediate
	case ModeCooling:
		return cfg.AllowCooling
	}
	return false
}

// ResolveReason 校验并整理用户提交的原因：code 必须在列表里（选「其他」时可以只填文字），
// 不允许自填时丢掉文字。返回的错误原话可以直接展示。
func ResolveReason(cfg Config, code, text string) (string, string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	text = strings.TrimSpace(text)
	if !cfg.AllowCustomReason {
		text = ""
	}
	if utf8.RuneCountInString(text) > MaxReasonTextRunes {
		return "", "", fmt.Errorf("补充说明不超过 %d 字", MaxReasonTextRunes)
	}
	if code != "" && !slices.ContainsFunc(cfg.Reasons, func(r Reason) bool { return r.Code == code }) {
		return "", "", fmt.Errorf("请选择有效的注销原因")
	}
	if cfg.ReasonRequired && code == "" && text == "" {
		return "", "", fmt.Errorf("请选择或填写注销原因")
	}
	return code, text, nil
}

// ReasonLabel 按 code 取原因名称；找不到时原样返回 code。
func ReasonLabel(cfg Config, code string) string {
	for _, reason := range cfg.Reasons {
		if reason.Code == code {
			return reason.Label
		}
	}
	return code
}

// InitialStatus 新申请的初始状态：需要审核时先待审核，否则冷静期进 scheduled、立即注销进 processing。
func InitialStatus(mode string, review bool) string {
	switch {
	case review:
		return StatusReviewing
	case mode == ModeImmediate:
		return StatusProcessing
	default:
		return StatusScheduled
	}
}

// Schedule 冷静期的到期时刻与提醒时刻（提醒时刻不晚于现在时为空）。
func Schedule(now time.Time, coolingDays, remindBeforeHours int) (time.Time, *time.Time) {
	due := now.Add(time.Duration(coolingDays) * 24 * time.Hour)
	if remindBeforeHours <= 0 {
		return due, nil
	}
	remind := due.Add(-time.Duration(remindBeforeHours) * time.Hour)
	if !remind.After(now) {
		return due, nil
	}
	return due, &remind
}

// UserCanCancel 本人能不能撤销：待审核、冷静期中与执行失败（账号还在，本人可以改主意）时可以。
func UserCanCancel(status string) bool {
	return status == StatusReviewing || status == StatusScheduled || status == StatusFailed
}

// AdminCanCancel 管理员能撤销的状态：失败的也可以撤销（不再重试，放开账号）。
func AdminCanCancel(status string) bool {
	return status == StatusReviewing || status == StatusScheduled || status == StatusFailed
}

// AdminCanExecute 管理员能立即执行的状态。
func AdminCanExecute(status string) bool {
	return status == StatusScheduled || status == StatusFailed
}

// ToClient 把申请翻成 App 看到的样子。
func ToClient(req *Request) *ClientRequest {
	if req == nil {
		return nil
	}
	return &ClientRequest{
		ID: req.ID, Mode: req.Mode, Status: req.Status, Source: req.Source,
		ReasonCode: req.ReasonCode, ReasonText: req.ReasonText, CoolingDays: req.CoolingDays,
		RequestedAt: req.CreatedAt, ScheduledAt: req.ScheduledAt, ReviewNote: req.ReviewNote,
		CancelledAt: req.CancelledAt, CanCancel: UserCanCancel(req.Status),
	}
}

// NormalizeIdentifier 墓碑比对前的规范化：账号名去空白、邮箱转小写、手机号去分隔符。
func NormalizeIdentifier(kind, value string) string {
	value = strings.TrimSpace(value)
	switch kind {
	case TombstoneEmail:
		return strings.ToLower(value)
	case TombstonePhone:
		return strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(value)
	}
	return value
}
