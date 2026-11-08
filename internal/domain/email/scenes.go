package email

import "strings"

// 邮件场景目录：平台会发出的每一类信、它的默认文案与可用变量。
//
// 一份目录同时驱动三处：服务层渲染（默认文案 + 策略里的覆盖）、策略校验（变量只能用目录里声明的）、
// 控制台的模板编辑器（场景列表、变量提示、默认文案占位）。新增一类信只需要在这里加一项。
//
// 文案里的变量写成 {{app}} 这种形式，渲染时做纯文本替换，替换完再交给 html/template 转义 ——
// 管理员写的文案不会被当成模板执行，也就没有注入面。

// 场景分组。
const (
	SceneCategoryCode     = "code"
	SceneCategorySecurity = "security"
	SceneCategoryAccount  = "account"
)

var SceneCategoryNames = map[string]string{
	SceneCategoryCode:     "验证码",
	SceneCategorySecurity: "安全通知",
	SceneCategoryAccount:  "账户通知",
}

// SceneCategoryOrder 控制台的分组顺序。
var SceneCategoryOrder = []string{SceneCategoryCode, SceneCategorySecurity, SceneCategoryAccount}

// SceneVariable 文案里能用的一个变量。
type SceneVariable struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Example string `json:"example"`
}

// SceneCopy 一封信可改写的四段文案。
type SceneCopy struct {
	Subject string `json:"subject"`
	Title   string `json:"title"`
	Lead    string `json:"lead"`
	Footer  string `json:"footer"`
}

// Scene 一类信。
type Scene struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	CategoryName string `json:"categoryName"`
	Description  string `json:"description"`
	// Toggle 为 true 的场景可以在策略里关掉；验证码与安全告知不能关
	Toggle    bool            `json:"toggle"`
	DefaultOn bool            `json:"defaultOn"`
	Variables []SceneVariable `json:"variables"`
	Defaults  SceneCopy       `json:"defaults"`
	// Purposes 投递留痕里的 purpose 取值（验证码场景含历史别名）
	Purposes []string `json:"purposes"`
}

// HasVariable 这个场景的文案能不能用这个变量。
func (s Scene) HasVariable(key string) bool {
	for _, variable := range s.Variables {
		if variable.Key == key {
			return true
		}
	}
	return false
}

var (
	varApp     = SceneVariable{Key: "app", Label: "品牌名", Example: "星河"}
	varCode    = SceneVariable{Key: "code", Label: "验证码", Example: "482913"}
	varMinutes = SceneVariable{Key: "minutes", Label: "有效分钟数", Example: "10"}
	varEmail   = SceneVariable{Key: "email", Label: "收件地址", Example: "user@example.com"}
	varName    = SceneVariable{Key: "name", Label: "用户昵称", Example: "小林"}
	varAccount = SceneVariable{Key: "account", Label: "账号", Example: "lin2026"}
	varField   = SceneVariable{Key: "field", Label: "变更项", Example: "邮箱地址"}
)

func codeScene(key, name, description string, purposes []string, noun, title, lead, footer string) Scene {
	return Scene{
		Key:         key,
		Name:        name,
		Category:    SceneCategoryCode,
		Description: description,
		Variables:   []SceneVariable{varApp, varCode, varMinutes, varEmail},
		Defaults: SceneCopy{
			Subject: "{{code}} 是您的" + noun,
			Title:   title,
			Lead:    lead,
			Footer:  footer,
		},
		Purposes: purposes,
	}
}

// 场景键。服务层按键取场景，不要散写字符串。
const (
	SceneCodeRegister       = "code_register"
	SceneCodeLogin          = "code_login"
	SceneCodeAdminLogin     = "code_admin_login"
	SceneCodeBindEmail      = "code_bind_email"
	SceneCodeChangeEmail    = "code_change_email"
	SceneCodeBindPhone      = "code_bind_phone"
	SceneCodeChangePhone    = "code_change_phone"
	SceneCodeResetPassword  = "code_reset_password"
	SceneCodeVerifyIdentity = "code_verify_identity"
	SceneCodeTwoFactor      = "code_two_factor"
	SceneCodeAccountDelete  = "code_account_deletion"
	SceneCodeGeneric        = "code_generic"

	ScenePasswordResetLink   = "password_reset_link"
	ScenePasswordResetNotice = "password_reset_notice"
	SceneProfileChange       = "profile_change"
	SceneWelcome             = "welcome"
)

var sceneCatalog = func() []Scene {
	scenes := []Scene{
		codeScene(SceneCodeRegister, "注册验证码", "用户用邮箱注册时收到", []string{"register", "signup", "sign-up"},
			"注册验证码", "注册验证码", "输入验证码完成注册，之后就用这个邮箱登录。",
			"不是您本人在注册的话，忽略这封信即可，账号不会被创建。"),
		codeScene(SceneCodeLogin, "登录验证码", "邮箱验证码登录", []string{"login", "signin", "sign-in"},
			"登录验证码", "登录验证码", "输入验证码即可登录。",
			"如果不是您本人在登录，建议立刻改密码，并看一下最近的登录记录。"),
		codeScene(SceneCodeAdminLogin, "管理员登录验证码", "管理后台登录的二次验证", []string{"admin_login"},
			"管理员登录验证码", "管理后台登录验证码", "输入验证码登录管理后台。",
			"后台账号权限高。不是您本人在登录的话，请立刻联系平台管理员。"),
		codeScene(SceneCodeBindEmail, "绑定邮箱", "给账号绑定邮箱时验证新地址", []string{"bind_email", "bind-email"},
			"邮箱绑定验证码", "绑定这个邮箱", "验证通过后，这个邮箱会绑定到您的账号。",
			"不是您本人在操作的话，忽略即可，绑定不会生效。"),
		codeScene(SceneCodeChangeEmail, "更换邮箱", "更换账号邮箱时验证新地址", []string{"change_email", "change-email", "profile_email_change"},
			"邮箱变更验证码", "换成这个邮箱", "验证通过后，账号邮箱会改成您当前正在查看的这个地址。",
			"变更完成后原邮箱会收到通知。不是您本人在操作的话，请立刻改密码。"),
		codeScene(SceneCodeBindPhone, "绑定手机号", "绑定手机号时向账号邮箱确认", []string{"bind_phone", "bind-phone"},
			"手机绑定验证码", "绑定手机号", "验证通过后，这个手机号会绑定到您的账号。",
			"不是您本人在操作的话，忽略即可，绑定不会生效。"),
		codeScene(SceneCodeChangePhone, "更换手机号", "更换手机号时向账号邮箱确认", []string{"change_phone", "change-phone", "profile_phone_change"},
			"手机变更验证码", "更换手机号", "验证通过后，账号手机号会换成新的那个。",
			"不是您本人在操作的话，请立刻改密码，并检查账号的绑定信息。"),
		codeScene(SceneCodeResetPassword, "找回密码", "找回密码流程里发往绑定邮箱", []string{"reset_password", "password_reset", "reset-password"},
			"密码重置验证码", "重置密码", "输入验证码后就可以设置新密码。",
			"新密码设置好之前，原密码一直有效。不是您本人申请的话，忽略即可。"),
		codeScene(SceneCodeVerifyIdentity, "身份验证", "敏感操作前的身份核验", []string{"verify_identity", "identity_verify", "identity-verification"},
			"身份验证码", "身份验证", "输入验证码完成这次身份核验。",
			"任何人向您要这串数字都是诈骗，包括自称客服的。"),
		codeScene(SceneCodeTwoFactor, "两步验证", "密码通过后的第二步", []string{"two_factor", "two-factor", "2fa", "mfa"},
			"两步验证码", "两步验证", "密码已经通过，再输入验证码就能完成这次登录。",
			"如果您并没有在登录，说明密码可能已经泄露，请尽快更换。"),
		codeScene(SceneCodeAccountDelete, "注销账号", "提交账号注销时确认本人", []string{"account_deletion", "account-deletion", "delete_account"},
			"账号注销验证码", "确认注销账号", "您正在申请注销账号，输入验证码确认是本人操作。",
			"不是您本人在操作的话，请立即修改密码；不输入验证码，注销不会生效。"),
		codeScene(SceneCodeGeneric, "其他验证码", "接入方自定义用途的验证码", nil,
			"验证码", "验证码", "输入验证码完成这次操作。",
			"任何人向您要这串数字都是诈骗，包括自称客服的。"),
		{
			Key:         ScenePasswordResetLink,
			Name:        "重置密码链接",
			Category:    SceneCategorySecurity,
			Description: "旧版找回接口发出的重置链接",
			Variables:   []SceneVariable{varApp, varMinutes, varEmail},
			Defaults: SceneCopy{
				Subject: "重置 {{app}} 的登录密码",
				Title:   "重置密码",
				Lead:    "我们收到了一次重置这个账号密码的请求。",
				Footer:  "不是您本人申请的话，忽略这封信即可，密码不会有任何变化。",
			},
			Purposes: []string{"password_reset_link"},
		},
		{
			Key:         ScenePasswordResetNotice,
			Name:        "密码已重置",
			Category:    SceneCategorySecurity,
			Description: "找回密码完成后告知绑定邮箱",
			Variables:   []SceneVariable{varApp, varAccount},
			Defaults: SceneCopy{
				Subject: "{{app}} 的登录密码已重置",
				Title:   "密码已重置",
				Lead:    "账号的登录密码已通过找回流程重新设置。",
				Footer:  "不是您本人操作的话，请立即再次找回密码，并检查绑定的邮箱、手机号与两步验证。",
			},
			Purposes: []string{"password_reset_notice"},
		},
		{
			Key:         SceneProfileChange,
			Name:        "资料变更",
			Category:    SceneCategorySecurity,
			Description: "邮箱或手机号变更后告知",
			Toggle:      true,
			DefaultOn:   true,
			Variables:   []SceneVariable{varApp, varField},
			Defaults: SceneCopy{
				Subject: "{{app}} 的{{field}}已变更",
				Title:   "{{field}}已变更",
				Lead:    "变更已经生效，下面是这次的改动。",
				Footer:  "不是您本人改的话，请立刻修改密码，并检查账号的绑定信息和最近登录记录。",
			},
			Purposes: []string{"profile_change"},
		},
		{
			Key:         SceneWelcome,
			Name:        "欢迎信",
			Category:    SceneCategoryAccount,
			Description: "注册成功且填了邮箱时发送",
			Toggle:      true,
			DefaultOn:   false,
			Variables:   []SceneVariable{varApp, varName, varEmail},
			Defaults: SceneCopy{
				Subject: "{{app}} 账号已开通",
				Title:   "账号已开通",
				Lead:    "您在 {{app}} 的账号已经可以用了。",
				Footer:  "如果您没有注册过 {{app}}，忽略这封信即可。",
			},
			Purposes: []string{"welcome"},
		},
	}
	for i := range scenes {
		scenes[i].CategoryName = SceneCategoryNames[scenes[i].Category]
	}
	return scenes
}()

var (
	sceneByKey     = map[string]Scene{}
	sceneByPurpose = map[string]string{}
)

func init() {
	for _, scene := range sceneCatalog {
		sceneByKey[scene.Key] = scene
		for _, purpose := range scene.Purposes {
			sceneByPurpose[purpose] = scene.Key
		}
	}
}

// Scenes 全部场景，顺序即控制台的展示顺序。返回副本。
func Scenes() []Scene {
	out := make([]Scene, len(sceneCatalog))
	copy(out, sceneCatalog)
	return out
}

// FindScene 按键取场景。
func FindScene(key string) (Scene, bool) {
	scene, ok := sceneByKey[strings.TrimSpace(key)]
	return scene, ok
}

// CodeSceneForPurpose 验证码用途对应的场景；认不出的用途落到「其他验证码」。
func CodeSceneForPurpose(purpose string) Scene {
	key := sceneByPurpose[strings.ToLower(strings.TrimSpace(purpose))]
	if scene, ok := sceneByKey[key]; ok && scene.Category == SceneCategoryCode {
		return scene
	}
	return sceneByKey[SceneCodeGeneric]
}

// ResolveCopy 默认文案叠上策略里的覆盖，再替换变量。
func (p Policy) ResolveCopy(scene Scene, vars map[string]string) SceneCopy {
	result := scene.Defaults
	if setting, ok := p.Scenes[scene.Key]; ok {
		if setting.Subject != "" {
			result.Subject = setting.Subject
		}
		if setting.Title != "" {
			result.Title = setting.Title
		}
		if setting.Lead != "" {
			result.Lead = setting.Lead
		}
		if setting.Footer != "" {
			result.Footer = setting.Footer
		}
	}
	replace := func(text string) string {
		return placeholderPattern.ReplaceAllStringFunc(text, func(token string) string {
			match := placeholderPattern.FindStringSubmatch(token)
			if value, ok := vars[match[1]]; ok {
				return value
			}
			return token
		})
	}
	result.Subject = strings.Join(strings.Fields(replace(result.Subject)), " ")
	result.Title = replace(result.Title)
	result.Lead = replace(result.Lead)
	result.Footer = replace(result.Footer)
	return result
}

// SampleVars 场景变量的示例值，预览用。
func (s Scene) SampleVars(appName string) map[string]string {
	vars := make(map[string]string, len(s.Variables))
	for _, variable := range s.Variables {
		vars[variable.Key] = variable.Example
	}
	if strings.TrimSpace(appName) != "" {
		vars["app"] = appName
	}
	return vars
}
