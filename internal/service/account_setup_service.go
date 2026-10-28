package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode"

	"aegis/internal/domain/accountdeletion"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// 账号初始化（docs/account-setup.md）。
//
// 第三方、短信、卡密快速登录首次建号时登记一行（trackAccountSetup），此后：
//   - GET /me/account-setup 按应用策略算出还缺什么、现在该弹窗还是出横幅；
//   - 客户端弹过一次窗就上报 prompted，之后只出横幅；用户关掉横幅上报 dismiss，隔 snoozeHours 再出；
//   - POST /me/account-setup 设用户名（一次）与密码（没有密码时），两项可以分开提交。
//
// 管理员改名不受「一次」限制，改完同样算用户名已设置；也可以要求某个账号重新初始化。

const (
	// errCodeUsernameTaken 用户名已被占用
	errCodeUsernameTaken = 40963
	// errCodeAccountSetupDone 用户名已设置过（自助只能设一次，之后找管理员改）
	errCodeAccountSetupDone = 40964
	// errCodePasswordAlreadySet 已有密码，应走修改密码
	errCodePasswordAlreadySet = 40967
	// errCodeAccountSetupPolicy 账号初始化策略不合法
	errCodeAccountSetupPolicy = 40968

	// accountSetupSuggestionCount 最多给几个可直接选用的用户名
	accountSetupSuggestionCount = 3
)

// AccountSetupInput POST /me/account-setup。两项都可以单独提交，空串表示这次不设。
type AccountSetupInput struct {
	Account  string
	Password string
}

// AccountSetupResult 提交后的结果：新的状态，以及用户名改了之后的账号名。
type AccountSetupResult struct {
	Status         *userdomain.AccountSetupStatus `json:"status"`
	Account        string                         `json:"account"`
	AccountChanged bool                           `json:"accountChanged"`
	PasswordSet    bool                           `json:"passwordSet"`
}

// UsernameCheck 用户名是否可用。
type UsernameCheck struct {
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
}

// AccountSetupPolicyView 管理端策略页。
type AccountSetupPolicyView struct {
	Policy  appdomain.AccountSetupPolicy  `json:"policy"`
	Stats   *userdomain.AccountSetupStats `json:"stats"`
	Sources []string                      `json:"sources"`
	Rules   userdomain.UsernameRules      `json:"rules"`
}

// trackAccountSetup 快速登录建号后登记。失败只记日志：少一行的代价是这个号不被提醒，不该让登录失败。
func (s *AuthService) trackAccountSetup(ctx context.Context, appID, userID int64, source, provider string) {
	if err := s.pg.TrackAccountSetup(ctx, appID, userID, source, provider); err != nil {
		s.log.Warn("登记账号初始化失败", zap.Int64("user_id", userID), zap.String("source", source), zap.Error(err))
	}
}

// accountSetupPolicy 读应用的初始化策略。没有 AppService（单测）时用默认策略。
func (s *AuthService) accountSetupPolicy(ctx context.Context, appID int64) (appdomain.AccountSetupPolicy, error) {
	if s.app == nil {
		return appdomain.DefaultAccountSetupPolicy(), nil
	}
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return appdomain.AccountSetupPolicy{}, err
	}
	if app == nil {
		return appdomain.DefaultAccountSetupPolicy(), nil
	}
	return appdomain.AccountSetupPolicyFromSettings(app.Settings), nil
}

// sessionUser 取会话对应的用户并核对应用。
func (s *AuthService) sessionUser(ctx context.Context, session *authdomain.Session) (*userdomain.User, error) {
	if session == nil || session.UserID <= 0 {
		return nil, apperrors.New(40100, http.StatusUnauthorized, "请先登录")
	}
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != session.AppID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return user, nil
}

// buildAccountSetupStatus 按策略算出状态。纯计算，不写库。
func buildAccountSetupStatus(policy appdomain.AccountSetupPolicy, user *userdomain.User,
	record *userdomain.AccountSetupRecord, now time.Time) *userdomain.AccountSetupStatus {
	status := &userdomain.AccountSetupStatus{
		Prompt:      userdomain.AccountSetupPromptNone,
		Account:     user.Account,
		Dismissible: policy.SnoozeHours > 0,
		Rules:       userdomain.DefaultUsernameRules(),
	}
	if record == nil {
		return status
	}
	status.Source = record.Source
	status.Provider = record.Provider
	status.CompletedAt = record.CompletedAt
	if !policy.Covers(record.Source) {
		return status
	}
	status.AccountRequired = policy.RequireAccount && record.AccountPending
	status.PasswordRequired = policy.RequirePassword && user.PasswordHash == ""
	status.Required = status.AccountRequired || status.PasswordRequired
	if !status.Required {
		return status
	}
	switch {
	case record.PromptedAt == nil && policy.FirstPrompt == appdomain.AccountSetupFirstPromptDialog:
		status.Prompt = userdomain.AccountSetupPromptDialog
	case record.SnoozedUntil != nil && record.SnoozedUntil.After(now) && policy.SnoozeHours > 0:
		status.SnoozedUntil = record.SnoozedUntil
	default:
		status.Prompt = userdomain.AccountSetupPromptBanner
	}
	return status
}

// AccountSetupStatus GET /me/account-setup。
func (s *AuthService) AccountSetupStatus(ctx context.Context, session *authdomain.Session) (*userdomain.AccountSetupStatus, error) {
	user, err := s.sessionUser(ctx, session)
	if err != nil {
		return nil, err
	}
	return s.accountSetupStatusOf(ctx, user)
}

func (s *AuthService) accountSetupStatusOf(ctx context.Context, user *userdomain.User) (*userdomain.AccountSetupStatus, error) {
	record, err := s.pg.GetAccountSetup(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	policy, err := s.accountSetupPolicy(ctx, user.AppID)
	if err != nil {
		return nil, err
	}
	record = s.syncAccountSetupCompletion(ctx, user, record)
	status := buildAccountSetupStatus(policy, user, record, timeutil.NowUTC())
	if status.AccountRequired {
		status.Suggestions = s.usernameSuggestions(ctx, user)
	}
	return status, nil
}

// syncAccountSetupCompletion 用户名已设且有密码就记为完成，反之清掉完成时刻。
//
// 密码可能经别的路径设上（修改密码、找回密码、管理员重置），这里在读状态时顺手对齐，
// 免得每条改密路径都要记得来同步。
func (s *AuthService) syncAccountSetupCompletion(ctx context.Context, user *userdomain.User,
	record *userdomain.AccountSetupRecord) *userdomain.AccountSetupRecord {
	if record == nil {
		return nil
	}
	done := !record.AccountPending && user.PasswordHash != ""
	if done == (record.CompletedAt != nil) {
		return record
	}
	var completedAt *time.Time
	if done {
		now := timeutil.NowUTC()
		completedAt = &now
	}
	if err := s.pg.SetAccountSetupCompleted(ctx, user.ID, completedAt); err != nil {
		s.log.Warn("更新账号初始化完成状态失败", zap.Int64("user_id", user.ID), zap.Error(err))
		return record
	}
	updated := *record
	updated.CompletedAt = completedAt
	return &updated
}

// MarkAccountSetupPrompted 客户端展示过首次弹窗。
func (s *AuthService) MarkAccountSetupPrompted(ctx context.Context, session *authdomain.Session) (*userdomain.AccountSetupStatus, error) {
	user, err := s.sessionUser(ctx, session)
	if err != nil {
		return nil, err
	}
	if err := s.pg.MarkAccountSetupPrompted(ctx, user.ID, timeutil.NowUTC()); err != nil {
		return nil, err
	}
	return s.accountSetupStatusOf(ctx, user)
}

// DismissAccountSetup 用户关掉横幅。策略不许关（snoozeHours = 0）时原样返回状态。
func (s *AuthService) DismissAccountSetup(ctx context.Context, session *authdomain.Session) (*userdomain.AccountSetupStatus, error) {
	user, err := s.sessionUser(ctx, session)
	if err != nil {
		return nil, err
	}
	policy, err := s.accountSetupPolicy(ctx, user.AppID)
	if err != nil {
		return nil, err
	}
	if policy.SnoozeHours > 0 {
		now := timeutil.NowUTC()
		until := now.Add(time.Duration(policy.SnoozeHours) * time.Hour)
		if err := s.pg.SnoozeAccountSetup(ctx, user.ID, now, until); err != nil {
			return nil, err
		}
	}
	return s.accountSetupStatusOf(ctx, user)
}

// CheckUsername 输入框里实时检查用户名能不能用。
func (s *AuthService) CheckUsername(ctx context.Context, session *authdomain.Session, username string) (*UsernameCheck, error) {
	user, err := s.sessionUser(ctx, session)
	if err != nil {
		return nil, err
	}
	username = strings.TrimSpace(username)
	if err := userdomain.ValidateUsername(username); err != nil {
		return &UsernameCheck{Message: err.Error()}, nil
	}
	if strings.EqualFold(username, user.Account) {
		return &UsernameCheck{Available: true}, nil
	}
	taken, err := s.pg.UsernameTaken(ctx, user.AppID, username, user.ID)
	if err != nil {
		return nil, err
	}
	if taken {
		return &UsernameCheck{Message: "该用户名已被使用"}, nil
	}
	if err := s.checkReregister(ctx, user.AppID,
		RegisterIdentifier{Kind: accountdeletion.TombstoneAccount, Value: username}); err != nil {
		return &UsernameCheck{Message: "该用户名暂不可用"}, nil
	}
	return &UsernameCheck{Available: true}, nil
}

// CompleteAccountSetup POST /me/account-setup。
//
// 用户名只能自助设一次（record.AccountPending 为真时）；密码只在还没有密码时能设，
// 已有密码的走修改密码。两项分开提交时，先成功的那项不会因为后一项失败而回滚。
func (s *AuthService) CompleteAccountSetup(ctx context.Context, session *authdomain.Session, input AccountSetupInput) (*AccountSetupResult, error) {
	user, err := s.sessionUser(ctx, session)
	if err != nil {
		return nil, err
	}
	account := strings.TrimSpace(input.Account)
	password := input.Password
	if account == "" && password == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请填写用户名或密码")
	}
	record, err := s.pg.GetAccountSetup(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	result := &AccountSetupResult{Account: user.Account}

	accountChanging := account != "" && account != user.Account
	if account != "" {
		if record == nil || !record.AccountPending {
			if accountChanging {
				return nil, apperrors.New(errCodeAccountSetupDone, http.StatusConflict, "用户名已设置，如需修改请联系客服")
			}
		}
	}
	if password != "" && user.PasswordHash != "" {
		return nil, apperrors.New(errCodePasswordAlreadySet, http.StatusConflict, "已设置过密码，请在账号安全中修改")
	}
	// 先把两项都校验完再落库：用户在一个弹窗里同时填了两项，不该出现「用户名改了、密码报错」的半截状态
	if account != "" {
		if err := userdomain.ValidateUsername(account); err != nil {
			return nil, apperrors.New(40005, http.StatusBadRequest, err.Error())
		}
		if accountChanging {
			if err := s.ensureUsernameAvailable(ctx, user, account); err != nil {
				return nil, err
			}
		}
	}
	if password != "" {
		pctx := s.userPasswordContext(ctx, user)
		if account != "" {
			pctx.Account = account
		}
		if err := s.validatePasswordPolicyWithContext(ctx, user.AppID, password, pctx); err != nil {
			return nil, err
		}
	}

	if account != "" && record != nil && record.AccountPending {
		if accountChanging {
			if err := s.renameUserAccount(ctx, user, account); err != nil {
				return nil, err
			}
			user.Account = account
			result.AccountChanged = true
		} else if err := s.pg.RenameUserAccount(ctx, user.AppID, user.ID, account); err != nil {
			// 用户确认沿用现在的名字：名字不变，只记为已设置
			return nil, err
		}
		result.Account = account
	}
	if password != "" {
		if err := s.ChangePassword(ctx, session, "", password); err != nil {
			return nil, err
		}
		result.PasswordSet = true
		refreshed, err := s.pg.GetUserByID(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		if refreshed != nil {
			user = refreshed
		}
	}
	status, err := s.accountSetupStatusOf(ctx, user)
	if err != nil {
		return nil, err
	}
	result.Status = status
	userID, appID := user.ID, user.AppID
	s.runDetached("account_setup.audit", 3*time.Second, func(actx context.Context) {
		_ = s.pg.InsertLoginAudit(actx, appID, userID, "account_setup", "", "", "", session.DeviceID, "", "success",
			map[string]any{"account": result.Account, "accountChanged": result.AccountChanged, "passwordSet": result.PasswordSet})
	})
	return result, nil
}

// ensureUsernameAvailable 改名前核对占用与注销后的保留期。
func (s *AuthService) ensureUsernameAvailable(ctx context.Context, user *userdomain.User, account string) error {
	taken, err := s.pg.UsernameTaken(ctx, user.AppID, account, user.ID)
	if err != nil {
		return err
	}
	if taken {
		return apperrors.New(errCodeUsernameTaken, http.StatusConflict, "该用户名已被使用")
	}
	if err := s.checkReregister(ctx, user.AppID,
		RegisterIdentifier{Kind: accountdeletion.TombstoneAccount, Value: account}); err != nil {
		return apperrors.New(errCodeUsernameTaken, http.StatusConflict, "该用户名暂不可用")
	}
	return nil
}

// renameUserAccount 改名落库并刷新各处缓存：已签发会话里的账号名、资料缓存、管理端搜索索引。
func (s *AuthService) renameUserAccount(ctx context.Context, user *userdomain.User, account string) error {
	if err := s.pg.RenameUserAccount(ctx, user.AppID, user.ID, account); err != nil {
		if err == pgrepo.ErrAccountAlreadyExists {
			return apperrors.New(errCodeUsernameTaken, http.StatusConflict, "该用户名已被使用")
		}
		if err == pgrepo.ErrUserNotFound {
			return apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		return err
	}
	if s.sessions != nil {
		if err := s.sessions.RenameSessionAccount(ctx, user.AppID, user.ID, account); err != nil {
			s.log.Warn("同步会话账号名失败", zap.Int64("user_id", user.ID), zap.Error(err))
		}
		_ = s.sessions.DeleteMyView(ctx, user.AppID, user.ID)
		_ = s.sessions.DeleteUserProfile(ctx, user.AppID, user.ID)
		_ = s.sessions.DeleteSecurityStatus(ctx, user.AppID, user.ID)
	}
	s.syncAdminUserSearch(user.AppID, user.ID)
	return nil
}

// usernameSuggestions 由昵称派生几个可用的用户名。查不到就少给，不报错。
func (s *AuthService) usernameSuggestions(ctx context.Context, user *userdomain.User) []string {
	base := ""
	if profile, err := s.pg.GetUserProfileByUserID(ctx, user.ID); err == nil && profile != nil {
		base = usernameBase(profile.Nickname)
	}
	fromNickname := base != ""
	if !fromNickname {
		base = "user"
	}
	out := make([]string, 0, accountSetupSuggestionCount)
	for attempt := 0; attempt < accountSetupSuggestionCount*3 && len(out) < accountSetupSuggestionCount; attempt++ {
		candidate := base
		// 昵称本身够长又合规时先试原样，之后（以及没有昵称时）都加数字后缀
		if !fromNickname || attempt > 0 || userdomain.ValidateUsername(candidate) != nil {
			candidate = fmt.Sprintf("%s_%d", base, randomSuffix(1000, 9999))
		}
		if userdomain.ValidateUsername(candidate) != nil || containsFold(out, candidate) {
			continue
		}
		taken, err := s.pg.UsernameTaken(ctx, user.AppID, candidate, user.ID)
		if err != nil || taken {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// usernameBase 昵称里取能当用户名的部分：字母、汉字、数字、下划线，以字母或汉字开头，最长 16 个字符。
func usernameBase(nickname string) string {
	var builder strings.Builder
	count := 0
	for _, r := range strings.TrimSpace(nickname) {
		isLetter := (r < unicode.MaxASCII && unicode.IsLetter(r)) || unicode.Is(unicode.Han, r)
		isDigit := r >= '0' && r <= '9'
		if builder.Len() == 0 && !isLetter {
			continue
		}
		if !isLetter && !isDigit && r != '_' {
			continue
		}
		builder.WriteRune(unicode.ToLower(r))
		count++
		if count >= 16 {
			break
		}
	}
	return builder.String()
}

func randomSuffix(min, max int64) int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(max-min+1))
	if err != nil {
		return min
	}
	return min + n.Int64()
}

// ── 管理端 ──

// AdminAccountSetup 用户详情里的初始化情况。
func (s *AuthService) AdminAccountSetup(ctx context.Context, appID, userID int64) (*userdomain.AdminAccountSetupView, error) {
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	record, err := s.pg.GetAccountSetup(ctx, userID)
	if err != nil {
		return nil, err
	}
	view := &userdomain.AdminAccountSetupView{PasswordPending: user.PasswordHash == ""}
	if record == nil {
		return view, nil
	}
	policy, err := s.accountSetupPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	record = s.syncAccountSetupCompletion(ctx, user, record)
	status := buildAccountSetupStatus(policy, user, record, timeutil.NowUTC())
	created := record.CreatedAt
	view.Tracked = true
	view.Source = record.Source
	view.Provider = record.Provider
	view.AccountPending = record.AccountPending
	view.Required = status.Required
	view.AccountSetAt = record.AccountSetAt
	view.PromptedAt = record.PromptedAt
	view.SnoozedUntil = record.SnoozedUntil
	view.CompletedAt = record.CompletedAt
	view.PolicyCovers = policy.Covers(record.Source)
	view.CreatedAt = &created
	return view, nil
}

// AdminRenameUser 管理员改用户名。规则比自助宽（沿用注册时的账号规则），可以改任意次。
func (s *AuthService) AdminRenameUser(ctx context.Context, appID, userID int64, account string) (*userdomain.User, error) {
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	account = normalizeAccount(account)
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	if strings.ContainsFunc(account, unicode.IsSpace) {
		return nil, apperrors.New(40005, http.StatusBadRequest, "用户名不能包含空白字符")
	}
	if account == user.Account {
		// 名字不变也记为已设置：管理员核对过，这个号的用户名就是它
		if err := s.pg.RenameUserAccount(ctx, appID, userID, account); err != nil {
			return nil, err
		}
		return user, nil
	}
	if err := s.ensureUsernameAvailable(ctx, user, account); err != nil {
		return nil, err
	}
	if err := s.renameUserAccount(ctx, user, account); err != nil {
		return nil, err
	}
	user.Account = account
	return user, nil
}

// AdminResetAccountSetup 要求用户重新初始化：用户名重新算作待设置（resetAccount），提醒从首次弹窗重新开始。
func (s *AuthService) AdminResetAccountSetup(ctx context.Context, appID, userID int64, resetAccount bool) (*userdomain.AdminAccountSetupView, error) {
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	record, err := s.pg.GetAccountSetup(ctx, userID)
	if err != nil {
		return nil, err
	}
	source := appdomain.AccountSetupSourceOAuth
	accountPending := resetAccount
	if record != nil {
		source = record.Source
		accountPending = resetAccount || record.AccountPending
	}
	if err := s.pg.ResetAccountSetup(ctx, appID, userID, source, accountPending); err != nil {
		return nil, err
	}
	return s.AdminAccountSetup(ctx, appID, userID)
}

// AccountSetupPolicyView 管理端读策略与统计。
func (s *AuthService) AccountSetupPolicyView(ctx context.Context, appID int64) (*AccountSetupPolicyView, error) {
	policy, err := s.accountSetupPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	stats, err := s.pg.AccountSetupStats(ctx, appID)
	if err != nil {
		return nil, err
	}
	return &AccountSetupPolicyView{Policy: policy, Stats: stats,
		Sources: appdomain.AccountSetupSources(), Rules: userdomain.DefaultUsernameRules()}, nil
}

// SaveAccountSetupPolicy 管理端保存策略。
func (s *AuthService) SaveAccountSetupPolicy(ctx context.Context, appID int64, policy appdomain.AccountSetupPolicy) (*AccountSetupPolicyView, error) {
	if s.app == nil {
		return nil, apperrors.New(50300, http.StatusServiceUnavailable, "应用服务不可用")
	}
	normalized, err := appdomain.NormalizeAccountSetupPolicy(policy)
	if err != nil {
		return nil, apperrors.New(errCodeAccountSetupPolicy, http.StatusBadRequest, err.Error())
	}
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "应用不存在")
	}
	settings := cloneSettingsMap(app.Settings)
	settings[appdomain.AccountSetupSettingsKey] = normalized.ToMap()
	if _, err := s.app.SaveApp(ctx, appdomain.AppMutation{ID: appID, Settings: settings}); err != nil {
		return nil, err
	}
	return s.AccountSetupPolicyView(ctx, appID)
}
