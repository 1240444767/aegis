package user

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 账号初始化（docs/account-setup.md）：快速登录建的号补设用户名与密码。

// 客户端该怎么提醒。
const (
	AccountSetupPromptNone   = "none"   // 不提醒（已完成、未开启，或横幅关掉后还没到再次出现的时刻）
	AccountSetupPromptDialog = "dialog" // 弹窗。只出现一次，展示后客户端上报 prompted
	AccountSetupPromptBanner = "banner" // 横幅
)

// 用户自己设的用户名的长度（按字符计）。
const (
	UsernameMinLength = 4
	UsernameMaxLength = 24
)

// UsernameRuleHint 规则的用户可读说法，客户端输入框下方直接显示。
const UsernameRuleHint = "4–24 个字符，以字母或汉字开头，可包含数字、下划线、点和短横线"

// AccountSetupRecord user_account_setup 的一行。
type AccountSetupRecord struct {
	UserID         int64      `json:"userId"`
	AppID          int64      `json:"appid"`
	Source         string     `json:"source"`
	Provider       string     `json:"provider,omitempty"`
	AccountPending bool       `json:"accountPending"`
	AccountSetAt   *time.Time `json:"accountSetAt,omitempty"`
	PromptedAt     *time.Time `json:"promptedAt,omitempty"`
	SnoozedUntil   *time.Time `json:"snoozedUntil,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// UsernameRules 下发给客户端的用户名规则，前端校验与服务端用同一份数字。
type UsernameRules struct {
	MinLength int    `json:"minLength"`
	MaxLength int    `json:"maxLength"`
	Hint      string `json:"hint"`
}

// AccountSetupStatus GET /me/account-setup 的结果。
type AccountSetupStatus struct {
	// Required 还有没完成的项（按应用策略要求的算）
	Required bool `json:"required"`
	// AccountRequired 用户名还是系统生成的，需要设置
	AccountRequired bool `json:"accountRequired"`
	// PasswordRequired 还没有密码，需要设置
	PasswordRequired bool `json:"passwordRequired"`
	// Prompt 现在该怎么提醒：none / dialog / banner
	Prompt string `json:"prompt"`
	// Dismissible 横幅能不能关闭
	Dismissible bool `json:"dismissible"`
	// Account 当前账号名
	Account string `json:"account"`
	// Source 建号方式：oauth / sms / cardkey；自己注册的账号为空
	Source string `json:"source,omitempty"`
	// Provider 第三方渠道（source = oauth 时）
	Provider     string     `json:"provider,omitempty"`
	SnoozedUntil *time.Time `json:"snoozedUntil,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	// Suggestions 可直接选用的用户名（已确认未被占用）
	Suggestions []string      `json:"suggestions,omitempty"`
	Rules       UsernameRules `json:"rules"`
}

// AdminAccountSetupView 管理端用户详情里的初始化情况。
type AdminAccountSetupView struct {
	Tracked         bool       `json:"tracked"`
	Source          string     `json:"source,omitempty"`
	Provider        string     `json:"provider,omitempty"`
	AccountPending  bool       `json:"accountPending"`
	PasswordPending bool       `json:"passwordPending"`
	Required        bool       `json:"required"`
	AccountSetAt    *time.Time `json:"accountSetAt,omitempty"`
	PromptedAt      *time.Time `json:"promptedAt,omitempty"`
	SnoozedUntil    *time.Time `json:"snoozedUntil,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	PolicyCovers    bool       `json:"policyCovers"`
	CreatedAt       *time.Time `json:"createdAt,omitempty"`
}

// AccountSetupStats 管理端策略页的统计。
type AccountSetupStats struct {
	Tracked        int64            `json:"tracked"`
	Completed      int64            `json:"completed"`
	Pending        int64            `json:"pending"`
	AccountPending int64            `json:"accountPending"`
	Prompted       int64            `json:"prompted"`
	BySource       map[string]int64 `json:"bySource"`
}

// DefaultUsernameRules 规则的下发形状。
func DefaultUsernameRules() UsernameRules {
	return UsernameRules{MinLength: UsernameMinLength, MaxLength: UsernameMaxLength, Hint: UsernameRuleHint}
}

// reservedUsernames 不许用户自己取的名字（比较时忽略大小写）。
var reservedUsernames = map[string]struct{}{
	"admin": {}, "administrator": {}, "root": {}, "system": {}, "aegis": {}, "official": {},
	"support": {}, "service": {}, "null": {}, "undefined": {}, "guest": {},
	"管理员": {}, "官方": {}, "客服": {}, "系统": {},
}

// ValidateUsername 校验用户自己设的用户名。返回的错误文案直接给用户看。
//
// 以字母或汉字开头：保证用户名永远不会是纯数字（与用户 ID、手机号登录区分），
// 不含 @（与邮箱登录区分）。这样登录框里输什么都只会落到一种标识上。
func ValidateUsername(username string) error {
	if username == "" {
		return fmt.Errorf("请输入用户名")
	}
	length := utf8.RuneCountInString(username)
	if length < UsernameMinLength || length > UsernameMaxLength {
		return fmt.Errorf("用户名长度应为 %d–%d 个字符", UsernameMinLength, UsernameMaxLength)
	}
	for index, r := range username {
		isLetter := unicode.IsLetter(r) && (r < unicode.MaxASCII || unicode.Is(unicode.Han, r))
		if index == 0 && !isLetter {
			return fmt.Errorf("用户名应以字母或汉字开头")
		}
		switch {
		case isLetter, r >= '0' && r <= '9', r == '_', r == '.', r == '-':
		default:
			return fmt.Errorf("用户名只能包含字母、汉字、数字、下划线、点和短横线")
		}
	}
	if strings.Contains(username, "..") || strings.HasSuffix(username, ".") {
		return fmt.Errorf("用户名不能以点结尾，也不能包含连续的点")
	}
	if _, reserved := reservedUsernames[strings.ToLower(username)]; reserved {
		return fmt.Errorf("该用户名为保留名称，请换一个")
	}
	return nil
}
