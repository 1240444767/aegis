package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	devicedomain "aegis/internal/domain/device"
	"aegis/internal/domain/devicelimit"
	notificationdomain "aegis/internal/domain/notification"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DeviceLimitService 登录设备数控制。规则见 docs/device-limit.md。
//
// 执行点只有一个：AuthService.issueSession（全部新登录的签发收口）。刷新令牌延续的是已有设备，
// 不经过这里。判定是 devicelimit.Plan 这个纯函数，这里只负责取齐事实与执行结果：
//
//	Admit  加锁 → 取在线设备、会员身份、例外、台账 → Plan → 拒绝 / 请用户选择 / 放行
//	Commit 签发之后：下线被选中的设备、写台账、记处置、发站内信与实时事件 → 解锁
//
// 基础设施出错（Redis、数据库）一律放行并记 warn：设备数控制是治理能力，
// 不能因为它的一次抖动让全体用户登不上。
type DeviceLimitService struct {
	log           *zap.Logger
	pg            *pgrepo.Repository
	sessions      *redisrepo.SessionRepository
	app           *AppService
	refreshTTL    time.Duration
	vip           *VipService
	devices       *DeviceMarketingService
	notifications notificationDeliverer
	realtime      UserEventPublisher
	now           func() time.Time
}

// 错误码，见 auth_protocol_catalog.go。
const (
	errCodeDeviceLimitKicked  = 40130 // 本设备因账号在其他设备登录、超出设备数被下线
	errCodeDeviceRemoved      = 40131 // 本设备被用户自己或管理员在设备管理里下线
	errCodeDeviceLimitReached = 40997 // 设备数已达上限，拒绝这次登录
	errCodeNewDeviceLimited   = 40998 // 窗口内新设备过多，拒绝这次登录
	errCodeDeviceLimitTicket  = 40496 // 选择下线设备的凭证无效或已过期
)

// RealtimeEventSessionRevoked 设备被下线时推给该用户全部在线连接的实时事件。
// 客户端拿 data.families 与自己的刷新令牌族比对，命中即提示「已在其他设备登录」并回到登录页。
const RealtimeEventSessionRevoked = "session.revoked"

const (
	deviceLimitLockTTL     = 10 * time.Second
	deviceLimitLockWait    = 3 * time.Second
	deviceLimitLedgerShown = 50
)

// NewDeviceLimitService 构造服务。refreshTTL 用于撤销令牌族时的标记有效期。
func NewDeviceLimitService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, app *AppService, refreshTTL time.Duration) *DeviceLimitService {
	if log == nil {
		log = zap.NewNop()
	}
	return &DeviceLimitService{log: log, pg: pg, sessions: sessions, app: app, refreshTTL: refreshTTL, now: time.Now}
}

// SetVipService 接上会员判定：有会员档位规则时用它判断用户落在哪一档。
func (s *DeviceLimitService) SetVipService(vip *VipService) { s.vip = vip }

// SetDeviceDirectory 接上设备字典：设备列表里的名称、图标按字典现查。
func (s *DeviceLimitService) SetDeviceDirectory(devices *DeviceMarketingService) { s.devices = devices }

// SetNotificationService 接上站内信：设备被自动下线时提醒用户。
func (s *DeviceLimitService) SetNotificationService(n notificationDeliverer) { s.notifications = n }

// SetRealtime 接上实时推送：设备被下线时通知该用户的在线连接。
func (s *DeviceLimitService) SetRealtime(realtime UserEventPublisher) { s.realtime = realtime }

/* ───────────────────── 策略 ───────────────────── */

// PolicyView 管理端读到的策略。
type PolicyView struct {
	Policy devicelimit.Policy `json:"policy"`
	// Configured false 表示还没用新策略保存过，当前值是按旧字段（multiDeviceLogin）推导的
	Configured bool `json:"configured"`
}

// Policy 读取应用的策略。
func (s *DeviceLimitService) Policy(ctx context.Context, appID int64) (*PolicyView, error) {
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	policy, configured := devicelimit.FromSettings(app.Settings)
	return &PolicyView{Policy: policy, Configured: configured}, nil
}

// SavePolicy 保存策略，并同步写回旧字段。
func (s *DeviceLimitService) SavePolicy(ctx context.Context, appID int64, policy devicelimit.Policy) (*PolicyView, error) {
	normalized, err := policy.Normalize()
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	for _, tier := range normalized.Tiers {
		if tier.Audience != devicelimit.AudienceFeature {
			continue
		}
		feature, err := s.pg.GetVipFeature(ctx, appID, tier.FeatureTag)
		if err != nil {
			return nil, err
		}
		if feature == nil {
			return nil, apperrors.New(errCodeFeatureUnknown, http.StatusNotFound,
				"功能标识 "+tier.FeatureTag+" 未登记，请先在会员功能目录里创建它")
		}
	}
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings[devicelimit.SettingsKey] = normalized.ToSettings()
	multi, num := normalized.LegacySettings()
	settings["multiDeviceLogin"] = multi
	settings["multiDeviceLoginNum"] = num
	if _, err := s.app.SaveApp(ctx, appdomain.AppMutation{ID: appID, Settings: settings}); err != nil {
		return nil, err
	}
	return &PolicyView{Policy: normalized, Configured: true}, nil
}

/* ───────────────────── 在线设备 ───────────────────── */

type familyAggregate struct {
	loginAt    time.Time
	lastActive time.Time
	latest     *refreshSnapshot
}

type refreshSnapshot struct {
	issuedAt  time.Time
	deviceID  string
	device    string
	model     string
	platform  string
	extra     *devicedomain.Extra
	ip        string
	userAgent string
	provider  string
}

// ActiveDevices 用户此刻仍在登录的设备：按刷新令牌族聚合（访问令牌寿命短，过期了设备也仍在登录），
// 再按设备 Key 合并。最近活动的在前。
func (s *DeviceLimitService) ActiveDevices(ctx context.Context, appID, userID int64) ([]devicelimit.Device, error) {
	items, err := s.sessions.ListIndexedRefreshSessions(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	families := map[string]*familyAggregate{}
	for _, item := range items {
		session := item.Session
		if session.FamilyID == "" || !session.ExpiresAt.After(now) {
			continue
		}
		agg := families[session.FamilyID]
		if agg == nil {
			agg = &familyAggregate{loginAt: session.IssuedAt, lastActive: session.IssuedAt}
			families[session.FamilyID] = agg
		}
		if session.IssuedAt.Before(agg.loginAt) {
			agg.loginAt = session.IssuedAt
		}
		if session.IssuedAt.After(agg.lastActive) {
			agg.lastActive = session.IssuedAt
		}
		// 已轮换过的刷新令牌只用来推算最早登录时间；仍可用的那一份才代表这次登录还活着
		if session.UsedAt != nil || strings.TrimSpace(session.ReplacedByToken) != "" {
			continue
		}
		if agg.latest == nil || session.IssuedAt.After(agg.latest.issuedAt) {
			agg.latest = &refreshSnapshot{
				issuedAt: session.IssuedAt, deviceID: session.DeviceID, device: session.Device, model: session.DeviceModel,
				platform: session.DevicePlatform, extra: session.DeviceExtra, ip: session.IP, userAgent: session.UserAgent,
				provider: session.Provider,
			}
		}
	}
	byKey := map[string]*devicelimit.Device{}
	latestAt := map[string]time.Time{}
	for familyID, agg := range families {
		if agg.latest == nil {
			continue
		}
		snap := agg.latest
		key := devicelimit.DeviceKey(snap.deviceID, snap.userAgent, familyID)
		device := byKey[key]
		if device == nil {
			device = &devicelimit.Device{Key: key, LoginAt: agg.loginAt, LastActiveAt: agg.lastActive}
			byKey[key] = device
		}
		device.Families = append(device.Families, familyID)
		device.Sessions++
		if agg.loginAt.Before(device.LoginAt) {
			device.LoginAt = agg.loginAt
		}
		if agg.lastActive.After(device.LastActiveAt) {
			device.LastActiveAt = agg.lastActive
		}
		// 设备的展示信息取最近一次活动的那份登录
		if snap.issuedAt.After(latestAt[key]) || latestAt[key].IsZero() {
			latestAt[key] = snap.issuedAt
			platform := devicedomain.NormalizePlatform(snap.platform)
			if platform == "" {
				platform = devicedomain.PlatformFromUserAgent(snap.userAgent)
			}
			device.DeviceID = snap.deviceID
			device.Platform = platform
			device.Class = devicelimit.ClassOf(platform)
			device.Name = snap.device
			device.Model = snap.model
			device.Extra = snap.extra
			device.IP = snap.ip
			device.UserAgent = snap.userAgent
			device.Provider = snap.provider
		}
	}
	out := make([]devicelimit.Device, 0, len(byKey))
	for _, device := range byKey {
		sort.Strings(device.Families)
		out = append(out, *device)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActiveAt.After(out[j].LastActiveAt) })
	return out, nil
}

// describe 按设备字典补齐展示信息。
func (s *DeviceLimitService) describe(ctx context.Context, devices []devicelimit.Device) []devicelimit.Device {
	for i := range devices {
		devices[i].Info = s.describeOne(ctx, devices[i].Model, devices[i].Platform, devices[i].DeviceID, devices[i].Name,
			devices[i].UserAgent, devices[i].Extra)
	}
	return devices
}

func (s *DeviceLimitService) describeOne(ctx context.Context, model, platform, deviceID, name, userAgent string, extra *devicedomain.Extra) *devicedomain.Info {
	input := devicedomain.DescribeInput{
		Model: model, Platform: platform, DeviceID: deviceID, Name: name, UserAgent: userAgent, Extra: extra.Value(),
	}
	if s.devices != nil {
		return s.devices.DescribeRef(ctx, input)
	}
	display := strings.TrimSpace(name)
	if display == "" {
		display = devicedomain.GuessFromUserAgent(userAgent)
	}
	return &devicedomain.Info{Name: display, Identifier: model, Platform: platform, Source: devicedomain.InfoSourceClient}
}

func displayName(device devicelimit.Device) string {
	if device.Info != nil && device.Info.Name != "" {
		return device.Info.Name
	}
	if device.Name != "" {
		return device.Name
	}
	if device.UserAgent != "" {
		return devicedomain.GuessFromUserAgent(device.UserAgent)
	}
	return "未知设备"
}

/* ───────────────────── 生效上限 ───────────────────── */

func (s *DeviceLimitService) member(ctx context.Context, appID, userID int64) devicelimit.Member {
	if s.vip == nil {
		return devicelimit.Member{}
	}
	entitlement, err := s.vip.ResolveEntitlement(ctx, appID, userID, "")
	if err != nil || entitlement == nil {
		if err != nil {
			s.log.Warn("device limit: resolve membership failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
		return devicelimit.Member{}
	}
	return devicelimit.Member{
		IsVIP: entitlement.IsVIP, IsLifetime: entitlement.IsLifetime, IsTrial: entitlement.IsTrial, Features: entitlement.Features,
	}
}

func (s *DeviceLimitService) override(ctx context.Context, appID, userID int64) *devicelimit.Override {
	item, err := s.pg.GetDeviceLimitOverride(ctx, appID, userID)
	if err != nil {
		s.log.Warn("device limit: load override failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		return nil
	}
	return item
}

func (s *DeviceLimitService) limitsFor(ctx context.Context, policy devicelimit.Policy, appID, userID int64) (devicelimit.Limits, devicelimit.Member, *devicelimit.Override) {
	if !policy.Enabled {
		return devicelimit.Resolve(policy, devicelimit.Member{}, nil, s.now()), devicelimit.Member{}, nil
	}
	var member devicelimit.Member
	if policy.NeedsMembership() {
		member = s.member(ctx, appID, userID)
	}
	override := s.override(ctx, appID, userID)
	return devicelimit.Resolve(policy, member, override, s.now()), member, override
}

func (s *DeviceLimitService) newDevicesInWindow(ctx context.Context, appID, userID int64, rule devicelimit.NewDeviceRule) int {
	if !rule.Enabled {
		return 0
	}
	since := s.now().Add(-time.Duration(rule.WindowDays) * 24 * time.Hour)
	count, err := s.pg.CountNewLoginDevices(ctx, appID, userID, since)
	if err != nil {
		s.log.Warn("device limit: count new devices failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		return 0
	}
	return count
}

/* ───────────────────── 登录关卡 ───────────────────── */

// DeviceLoginInput 正在登录的设备与登录现场。
type DeviceLoginInput struct {
	Provider  string
	LoginType string
	DeviceID  string
	Device    string
	IP        string
	UserAgent string
	Client    devicedomain.Client
	// Chosen 用户在选择页勾选要下线的设备（完成选择时）
	Chosen []string
	// CanAsk 客户端声明了能让用户选择
	CanAsk bool
}

// DeviceAdmission 一次放行的登录：签发之后交给 Commit 执行下线、记账，最后解锁。
type DeviceAdmission struct {
	svc       *DeviceLimitService
	appID     int64
	userID    int64
	policy    devicelimit.Policy
	limits    devicelimit.Limits
	decision  devicelimit.Decision
	key       string
	platform  string
	input     DeviceLoginInput
	lockToken string
	released  bool
	// resolved 这次是在选择页选好之后完成的登录
	resolved bool
}

// Release 解锁；可重复调用。签发失败时调用方也必须调它。
func (a *DeviceAdmission) Release() {
	if a == nil || a.released {
		return
	}
	a.released = true
	if a.lockToken == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.svc.sessions.ReleaseDeviceLimitLock(ctx, a.appID, a.userID, a.lockToken); err != nil {
		a.svc.log.Warn("device limit: release lock failed", zap.Int64("appid", a.appID), zap.Int64("userId", a.userID), zap.Error(err))
	}
}

// Admit 新登录的设备数关卡。
//
// 返回三种结果之一：放行（admission，签发后调 Commit）、请用户选择（challenge，此时不签发）、
// 拒绝（error）。没有开启时也返回 admission，Commit 只记台账。
func (s *DeviceLimitService) Admit(ctx context.Context, app *appdomain.App, user *userdomain.User, in DeviceLoginInput) (*DeviceAdmission, *devicelimit.Challenge, error) {
	if s == nil || app == nil || user == nil {
		return nil, nil, nil
	}
	policy, _ := devicelimit.FromSettings(app.Settings)
	platform := in.Client.Platform
	if platform == "" {
		platform = devicedomain.PlatformFromUserAgent(in.UserAgent)
	}
	admission := &DeviceAdmission{
		svc: s, appID: user.AppID, userID: user.ID, policy: policy, input: in, platform: platform,
		key:      devicelimit.DeviceKey(in.DeviceID, in.UserAgent, "pending-"+uuid.NewString()),
		decision: devicelimit.Decision{Action: devicelimit.ActionAllow},
		resolved: len(in.Chosen) > 0,
	}
	if !policy.Enabled {
		return admission, nil, nil
	}

	token := uuid.NewString()
	locked, err := s.sessions.AcquireDeviceLimitLock(ctx, user.AppID, user.ID, token, deviceLimitLockTTL, deviceLimitLockWait)
	if err != nil {
		s.log.Warn("device limit: acquire lock failed", zap.Int64("appid", user.AppID), zap.Int64("userId", user.ID), zap.Error(err))
	}
	if locked {
		admission.lockToken = token
	}

	devices, err := s.ActiveDevices(ctx, user.AppID, user.ID)
	if err != nil {
		s.log.Warn("device limit: list devices failed, allowing login", zap.Int64("appid", user.AppID), zap.Int64("userId", user.ID), zap.Error(err))
		return admission, nil, nil
	}
	// 客户端换了设备码生成方式：旧码下还活着的会话就是这台设备，不能让它挤掉自己
	previous := devicedomain.PreviousIDFrom(ctx)
	if previous != "" {
		devices = devicelimit.RenameKey(devices, previous, admission.key)
	}
	limits, _, _ := s.limitsFor(ctx, policy, user.AppID, user.ID)
	admission.limits = limits
	candidate := devicelimit.Candidate{Key: admission.key, Class: devicelimit.ClassOf(platform), Platform: platform}
	newCount := 0
	if limits.NewDevice.Enabled && !limits.Unlimited && devicelimit.Ledgerable(candidate.Key) {
		known, err := s.pg.LoginDeviceKnown(ctx, user.AppID, user.ID, candidate.Key)
		if err == nil && !known && previous != "" {
			// 台账里只有旧码：同一台设备，不算新设备
			known, err = s.pg.LoginDeviceKnown(ctx, user.AppID, user.ID, previous)
		}
		if err != nil {
			s.log.Warn("device limit: ledger lookup failed", zap.Int64("appid", user.AppID), zap.Int64("userId", user.ID), zap.Error(err))
			known = true
		}
		candidate.Known = known
		if !known {
			newCount = s.newDevicesInWindow(ctx, user.AppID, user.ID, limits.NewDevice)
		}
	}
	decision := devicelimit.Plan(devicelimit.PlanInput{
		Policy: policy, Limits: limits, Devices: devices, Candidate: candidate,
		NewDevicesInWindow: newCount, CanAsk: in.CanAsk, Chosen: in.Chosen,
	})
	admission.decision = decision
	triggerName := s.describeOne(ctx, in.Client.Model, platform, in.DeviceID, in.Device, in.UserAgent, in.Client.Extra.Ref()).Name

	switch decision.Action {
	case devicelimit.ActionReject:
		admission.Release()
		action := devicelimit.EventRejected
		code := errCodeDeviceLimitReached
		if decision.Dimension == devicelimit.DimensionNewDevice {
			action = devicelimit.EventNewDevice
			code = errCodeNewDeviceLimited
		}
		s.recordEvents(user.AppID, []devicelimit.Event{{
			AppID: user.AppID, UserID: user.ID, Action: action, Dimension: decision.Dimension,
			DeviceKey: candidate.Key, DeviceName: triggerName, Platform: platform, Class: candidate.Class,
			IP: in.IP, Limit: firstLimit(decision), Detail: decisionDetail(decision, limits),
		}})
		return nil, nil, apperrors.New(code, http.StatusConflict, decision.Message)
	case devicelimit.ActionAsk:
		admission.Release()
		challenge, err := s.createChallenge(ctx, user, in, devices, decision)
		if err != nil {
			return nil, nil, err
		}
		s.recordEvents(user.AppID, []devicelimit.Event{{
			AppID: user.AppID, UserID: user.ID, Action: devicelimit.EventChallenged, Dimension: decision.Dimension,
			DeviceKey: candidate.Key, DeviceName: triggerName, Platform: platform, Class: candidate.Class,
			IP: in.IP, Limit: firstLimit(decision), Detail: decisionDetail(decision, limits),
		}})
		return nil, challenge, nil
	}
	return admission, nil, nil
}

// IssuedLogin 签发结果里 Commit 需要的部分。
type IssuedLogin struct {
	FamilyID string
}

// Commit 签发之后执行处置：下线被顶掉与被选中的设备、写台账、记处置、提醒，最后解锁。
// 会话已经签发，这里的任何失败都只记日志。
func (s *DeviceLimitService) Commit(ctx context.Context, admission *DeviceAdmission, issued IssuedLogin) {
	if s == nil || admission == nil {
		return
	}
	defer admission.Release()
	appID, userID := admission.appID, admission.userID
	decision := admission.decision
	in := admission.input
	triggerName := s.describeOne(ctx, in.Client.Model, admission.platform, in.DeviceID, in.Device, in.UserAgent, in.Client.Extra.Ref()).Name

	if len(decision.Replaced) > 0 {
		families := collectFamilies(decision.Replaced, issued.FamilyID)
		if err := s.revokeFamilies(ctx, appID, userID, families, redisrepo.RevokeReasonGeneric); err != nil {
			s.log.Warn("device limit: revoke replaced login failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}
	if len(decision.Victims) > 0 {
		victims := s.describe(ctx, decision.Victims)
		families := collectFamilies(victims, issued.FamilyID)
		if err := s.revokeFamilies(ctx, appID, userID, families, redisrepo.RevokeReasonDeviceLimit); err != nil {
			s.log.Warn("device limit: revoke victims failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
		action := devicelimit.EventKicked
		if admission.resolved {
			action = devicelimit.EventResolved
		}
		events := make([]devicelimit.Event, 0, len(victims))
		for _, victim := range victims {
			events = append(events, devicelimit.Event{
				AppID: appID, UserID: userID, Action: action, Dimension: decision.Dimension,
				DeviceKey: victim.Key, DeviceName: displayName(victim), Platform: victim.Platform, Class: victim.Class,
				TriggerName: triggerName, IP: in.IP, Limit: firstLimit(decision), Detail: decisionDetail(decision, admission.limits),
			})
		}
		s.recordEvents(appID, events)
		s.announceRevoked(appID, userID, victims, families, redisrepo.RevokeReasonDeviceLimit)
		if admission.policy.NotifyKicked && !admission.resolved {
			s.notifyKicked(appID, userID, triggerName, victims)
		}
	}

	key := admission.key
	if devicelimit.KindOf(key) == devicelimit.KeyKindSession {
		key = devicelimit.DeviceKey("", "", issued.FamilyID)
	}
	if devicelimit.Ledgerable(key) {
		touch := devicelimit.LedgerTouch{
			AppID: appID, UserID: userID, DeviceKey: key, Platform: admission.platform, Model: in.Client.Model,
			Name: triggerName, Extra: in.Client.Extra.Ref(), IP: in.IP, Login: true, At: s.now(),
		}
		if err := s.pg.TouchLoginDevice(ctx, touch); err != nil {
			s.log.Warn("device limit: touch ledger failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}
}

// TouchRefresh 刷新令牌时更新台账的最后出现时间（不计登录次数）。
func (s *DeviceLimitService) TouchRefresh(ctx context.Context, appID, userID int64, deviceID, userAgent, familyID, ip string, client devicedomain.Client) {
	if s == nil {
		return
	}
	key := devicelimit.DeviceKey(deviceID, userAgent, familyID)
	if !devicelimit.Ledgerable(key) {
		return
	}
	platform := client.Platform
	if platform == "" {
		platform = devicedomain.PlatformFromUserAgent(userAgent)
	}
	if err := s.pg.TouchLoginDevice(ctx, devicelimit.LedgerTouch{
		AppID: appID, UserID: userID, DeviceKey: key, Platform: platform, Model: client.Model,
		Extra: client.Extra.Ref(), IP: ip, At: s.now(),
	}); err != nil {
		s.log.Debug("device limit: touch ledger on refresh failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
	}
}

func collectFamilies(devices []devicelimit.Device, keep string) []string {
	out := []string{}
	for _, device := range devices {
		for _, family := range device.Families {
			if family != "" && family != keep {
				out = append(out, family)
			}
		}
	}
	return out
}

func firstLimit(decision devicelimit.Decision) int {
	if len(decision.Requirements) == 0 {
		return 0
	}
	return decision.Requirements[0].Limit
}

func decisionDetail(decision devicelimit.Decision, limits devicelimit.Limits) map[string]any {
	detail := map[string]any{
		"requirements": decision.Requirements,
		"source":       limits.Source,
		"sourceLabel":  limits.SourceLabel,
	}
	if limits.TierID != "" {
		detail["tierId"] = limits.TierID
	}
	return detail
}

/* ───────────────────── 下线 ───────────────────── */

// revokeFamilies 撤销若干刷新令牌族：访问令牌与刷新令牌都拉黑并记下原因，令牌族打上撤销标记。
func (s *DeviceLimitService) revokeFamilies(ctx context.Context, appID, userID int64, families []string, reason string) error {
	if len(families) == 0 {
		return nil
	}
	set := make(map[string]bool, len(families))
	for _, family := range families {
		set[family] = true
	}
	now := s.now()
	access, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		return err
	}
	for _, item := range access {
		if !set[item.Session.RefreshFamilyID] {
			continue
		}
		if err := s.sessions.DeleteSessionByHash(ctx, appID, userID, item.TokenHash); err != nil {
			return err
		}
		if ttl := item.Session.ExpiresAt.Sub(now); ttl > 0 {
			if err := s.sessions.BlacklistTokenWithReason(ctx, item.Session.TokenID, reason, ttl); err != nil {
				return err
			}
		}
	}
	refresh, err := s.sessions.ListIndexedRefreshSessions(ctx, appID, userID)
	if err != nil {
		return err
	}
	for _, item := range refresh {
		if !set[item.Session.FamilyID] {
			continue
		}
		if err := s.sessions.DeleteRefreshSessionByHash(ctx, appID, userID, item.TokenHash); err != nil {
			return err
		}
		if ttl := item.Session.ExpiresAt.Sub(now); ttl > 0 {
			if err := s.sessions.BlacklistTokenWithReason(ctx, item.Session.TokenID, reason, ttl); err != nil {
				return err
			}
		}
	}
	for family := range set {
		if err := s.sessions.RevokeRefreshFamilyWithReason(ctx, appID, userID, family, reason, s.refreshTTL); err != nil {
			return err
		}
	}
	return nil
}

func (s *DeviceLimitService) announceRevoked(appID, userID int64, devices []devicelimit.Device, families []string, reason string) {
	if s.realtime == nil || len(families) == 0 {
		return
	}
	keys := make([]string, 0, len(devices))
	for _, device := range devices {
		keys = append(keys, device.Key)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.realtime.PublishUserEvent(ctx, appID, userID, RealtimeEventSessionRevoked, map[string]any{
			"reason": reason, "families": families, "deviceKeys": keys,
		}); err != nil {
			s.log.Debug("device limit: publish revoke event failed", zap.Error(err))
		}
	}()
}

func (s *DeviceLimitService) notifyKicked(appID, userID int64, triggerName string, victims []devicelimit.Device) {
	if s.notifications == nil || len(victims) == 0 {
		return
	}
	names := make([]string, 0, len(victims))
	for _, victim := range victims {
		names = append(names, "「"+displayName(victim)+"」")
	}
	if triggerName == "" {
		triggerName = "新设备"
	}
	content := fmt.Sprintf("你的账号在「%s」上登录，登录设备数超出上限，%s已自动退出登录。如果不是你本人操作，请尽快修改密码。",
		triggerName, strings.Join(names, "、"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notifications.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: appID, UserID: userID, Type: notificationdomain.TypeSecurity, Title: "账号已在新设备登录",
			Content: content, Level: "warning",
			Metadata: map[string]any{"kind": "device_limit", "trigger": triggerName},
		}); err != nil {
			s.log.Debug("device limit: deliver notification failed", zap.Error(err))
		}
	}()
}

func (s *DeviceLimitService) recordEvents(appID int64, events []devicelimit.Event) {
	if len(events) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.pg.InsertDeviceLimitEvents(ctx, events); err != nil {
			s.log.Warn("device limit: record events failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}()
}

// RemoveDevices 下线指定设备。actor 为 EventUserRemoved（用户自己）或 EventAdminRemoved；
// keepFamily 是发起请求的那次登录，用户不能用这个接口把自己当前的设备下线（那是退出登录）。
func (s *DeviceLimitService) RemoveDevices(ctx context.Context, appID, userID int64, keys []string, actor, operator, keepFamily string) (int, error) {
	keys = uniqueStrings(keys)
	if len(keys) == 0 {
		return 0, apperrors.New(40000, http.StatusBadRequest, "请选择要下线的设备")
	}
	devices, err := s.ActiveDevices(ctx, appID, userID)
	if err != nil {
		return 0, err
	}
	targets := []devicelimit.Device{}
	for _, device := range devices {
		if !containsString(keys, device.Key) {
			continue
		}
		if keepFamily != "" && containsString(device.Families, keepFamily) {
			return 0, apperrors.New(40000, http.StatusBadRequest, "不能在这里下线当前设备，请使用退出登录")
		}
		targets = append(targets, device)
	}
	if len(targets) == 0 {
		return 0, apperrors.New(40400, http.StatusNotFound, "设备已不在登录状态")
	}
	targets = s.describe(ctx, targets)
	families := collectFamilies(targets, "")
	if err := s.revokeFamilies(ctx, appID, userID, families, redisrepo.RevokeReasonRemoved); err != nil {
		return 0, err
	}
	events := make([]devicelimit.Event, 0, len(targets))
	for _, device := range targets {
		events = append(events, devicelimit.Event{
			AppID: appID, UserID: userID, Action: actor, DeviceKey: device.Key, DeviceName: displayName(device),
			Platform: device.Platform, Class: device.Class, IP: device.IP, Operator: operator,
		})
	}
	s.recordEvents(appID, events)
	s.announceRevoked(appID, userID, targets, families, redisrepo.RevokeReasonRemoved)
	return len(targets), nil
}

// Enforce 按当前策略把一个用户已登录的设备收回到上限之内。
func (s *DeviceLimitService) Enforce(ctx context.Context, appID, userID int64, operator string) (int, error) {
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return 0, err
	}
	policy, _ := devicelimit.FromSettings(app.Settings)
	if !policy.Enabled {
		return 0, apperrors.New(40000, http.StatusBadRequest, "设备数控制未开启")
	}
	devices, err := s.ActiveDevices(ctx, appID, userID)
	if err != nil {
		return 0, err
	}
	limits, _, _ := s.limitsFor(ctx, policy, appID, userID)
	victims := devicelimit.Trim(policy, limits, devices)
	if len(victims) == 0 {
		return 0, nil
	}
	victims = s.describe(ctx, victims)
	families := collectFamilies(victims, "")
	if err := s.revokeFamilies(ctx, appID, userID, families, redisrepo.RevokeReasonDeviceLimit); err != nil {
		return 0, err
	}
	events := make([]devicelimit.Event, 0, len(victims))
	for _, device := range victims {
		events = append(events, devicelimit.Event{
			AppID: appID, UserID: userID, Action: devicelimit.EventAdminEnforced, DeviceKey: device.Key,
			DeviceName: displayName(device), Platform: device.Platform, Class: device.Class, IP: device.IP,
			Limit: limits.MaxDevices, Operator: operator, Detail: map[string]any{"sourceLabel": limits.SourceLabel},
		})
	}
	s.recordEvents(appID, events)
	s.announceRevoked(appID, userID, victims, families, redisrepo.RevokeReasonDeviceLimit)
	return len(victims), nil
}

/* ───────────────────── 选择下线哪台 ───────────────────── */

func (s *DeviceLimitService) createChallenge(ctx context.Context, user *userdomain.User, in DeviceLoginInput, devices []devicelimit.Device, decision devicelimit.Decision) (*devicelimit.Challenge, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	pending := devicelimit.PendingLogin{
		AppID: user.AppID, UserID: user.ID, Provider: in.Provider, LoginType: in.LoginType, DeviceID: in.DeviceID,
		Device: in.Device, IP: in.IP, UserAgent: in.UserAgent, Model: in.Client.Model, Platform: in.Client.Platform,
		Extra: in.Client.Extra.Ref(), CreatedAt: now,
	}
	if err := s.sessions.SetDeviceLimitChallenge(ctx, ticket, pending, devicelimit.ChallengeTTL); err != nil {
		return nil, err
	}
	anyTotal := false
	classes := map[string]bool{}
	for _, req := range decision.Requirements {
		if req.Dimension == devicelimit.DimensionTotal {
			anyTotal = true
		}
		if req.Dimension == devicelimit.DimensionClass {
			classes[req.Class] = true
		}
	}
	described := s.describe(ctx, devices)
	items := make([]devicelimit.ChallengeDevice, 0, len(described))
	for _, device := range described {
		if containsString(collectKeys(decision.Replaced), device.Key) {
			continue
		}
		items = append(items, devicelimit.ChallengeDevice{Device: device, Eligible: anyTotal || classes[device.Class]})
	}
	return &devicelimit.Challenge{
		Ticket: ticket, ExpiresAt: now.Add(devicelimit.ChallengeTTL), Dimension: decision.Dimension,
		Requirements: decision.Requirements, MustRemove: devicelimit.MustRemoveTotal(decision.Requirements),
		Message: decision.Message, Devices: items,
	}, nil
}

// LoadChallenge 取回选择页暂存的登录现场。
func (s *DeviceLimitService) LoadChallenge(ctx context.Context, appID int64, ticket string) (*devicelimit.PendingLogin, error) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return nil, apperrors.New(errCodeDeviceLimitTicket, http.StatusNotFound, "设备选择已失效，请重新登录")
	}
	pending, err := s.sessions.GetDeviceLimitChallenge(ctx, ticket)
	if err != nil {
		return nil, err
	}
	if pending == nil || pending.AppID != appID {
		return nil, apperrors.New(errCodeDeviceLimitTicket, http.StatusNotFound, "设备选择已失效，请重新登录")
	}
	return pending, nil
}

// FinishChallenge 登录完成后作废凭证。
func (s *DeviceLimitService) FinishChallenge(ctx context.Context, ticket string) {
	if err := s.sessions.DeleteDeviceLimitChallenge(ctx, strings.TrimSpace(ticket)); err != nil {
		s.log.Debug("device limit: delete challenge failed", zap.Error(err))
	}
}

func collectKeys(devices []devicelimit.Device) []string {
	out := make([]string, 0, len(devices))
	for _, device := range devices {
		out = append(out, device.Key)
	}
	return out
}

/* ───────────────────── 状况 ───────────────────── */

// Status 一个用户的设备数状况。currentFamily 是发起请求的那次登录，用于标出「本机」。
func (s *DeviceLimitService) Status(ctx context.Context, appID, userID int64, currentFamily string) (*devicelimit.Status, error) {
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	policy, _ := devicelimit.FromSettings(app.Settings)
	devices, err := s.ActiveDevices(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	devices = s.describe(ctx, devices)
	for i := range devices {
		devices[i].Current = currentFamily != "" && containsString(devices[i].Families, currentFamily)
	}
	sort.SliceStable(devices, func(i, j int) bool { return devices[i].Current && !devices[j].Current })
	limits, _, _ := s.limitsFor(ctx, policy, appID, userID)
	status := &devicelimit.Status{
		Enabled: policy.Enabled, Limits: limits, Usage: devicelimit.UsageOf(devices), Devices: devices,
		SameDevice: policy.SameDevice, Overflow: policy.Overflow,
	}
	if limits.NewDevice.Enabled && !limits.Unlimited {
		status.NewDevicesInWindow = s.newDevicesInWindow(ctx, appID, userID, limits.NewDevice)
	}
	return status, nil
}

/* ───────────────────── 管理端 ───────────────────── */

// Overview 概览：days 天内的处置统计与设备分布，tz 是浏览器时区（按它划分自然日）。
func (s *DeviceLimitService) Overview(ctx context.Context, appID int64, days int, tz string) (*devicelimit.Overview, error) {
	if days <= 0 {
		days = 7
	}
	days = min(days, 90)
	location := time.Local
	if tz = strings.TrimSpace(tz); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			location = loaded
		} else {
			tz = ""
		}
	}
	if tz == "" {
		tz = location.String()
		if tz == "Local" {
			tz = "UTC"
		}
	}
	view, err := s.Policy(ctx, appID)
	if err != nil {
		return nil, err
	}
	now := s.now().In(location)
	startDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -(days - 1))
	atLimit := 0
	if view.Policy.Enabled {
		atLimit = view.Policy.MaxDevices
	}
	stats, err := s.pg.DeviceLimitOverviewStats(ctx, appID, startDay, tz, atLimit)
	if err != nil {
		return nil, err
	}
	overview := &devicelimit.Overview{
		Policy: view.Policy, Configured: view.Configured, Days: days, Totals: map[string]int64{},
		Dimensions: stats.Dimensions, ActiveDevices: stats.ActiveDevices, ActiveUsers: stats.ActiveUsers,
		ByClass: map[string]int64{}, Distribution: map[string]int64{}, AtLimitUsers: stats.AtLimitUsers,
		Overrides: stats.Overrides, TopUsers: stats.TopUsers,
	}
	for _, action := range devicelimit.EventActions {
		overview.Totals[action] = stats.Totals[action]
	}
	for _, class := range devicelimit.Classes {
		overview.ByClass[class] = 0
	}
	for platform, count := range stats.ByPlatform {
		overview.ByClass[devicelimit.ClassOf(platform)] += count
	}
	for bucket := 1; bucket <= 10; bucket++ {
		overview.Distribution[fmt.Sprint(bucket)] = stats.Distribution[bucket]
	}
	for i := 0; i < days; i++ {
		day := startDay.AddDate(0, 0, i).Format("2006-01-02")
		counts := map[string]int64{}
		for _, action := range devicelimit.EventActions {
			counts[action] = stats.Trend[day][action]
		}
		overview.Trend = append(overview.Trend, devicelimit.TrendPoint{Date: day, Counts: counts})
	}
	return overview, nil
}

// Events 处置记录。
func (s *DeviceLimitService) Events(ctx context.Context, appID int64, q devicelimit.EventQuery) (*devicelimit.EventList, error) {
	q.Page = max(1, q.Page)
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 20
	}
	items, total, err := s.pg.ListDeviceLimitEvents(ctx, appID, q)
	if err != nil {
		return nil, err
	}
	return &devicelimit.EventList{Items: items, Total: total, Page: q.Page, Limit: q.Limit}, nil
}

// Overrides 例外名单。
func (s *DeviceLimitService) Overrides(ctx context.Context, appID int64, q devicelimit.OverrideQuery) (*devicelimit.OverrideList, error) {
	q.Page = max(1, q.Page)
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 20
	}
	items, total, err := s.pg.ListDeviceLimitOverrides(ctx, appID, q)
	if err != nil {
		return nil, err
	}
	return &devicelimit.OverrideList{Items: items, Total: total, Page: q.Page, Limit: q.Limit}, nil
}

func (s *DeviceLimitService) requireAppUser(ctx context.Context, appID, userID int64) (*userdomain.User, error) {
	if userID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "用户 ID 无效")
	}
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40400, http.StatusNotFound, "用户不存在")
	}
	return user, nil
}

// SaveOverride 写入一个用户的例外。
func (s *DeviceLimitService) SaveOverride(ctx context.Context, appID, userID int64, in devicelimit.OverrideInput, operator string) (*devicelimit.Override, error) {
	if _, err := s.requireAppUser(ctx, appID, userID); err != nil {
		return nil, err
	}
	normalized, err := in.Normalize(s.now())
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return s.pg.UpsertDeviceLimitOverride(ctx, appID, userID, normalized, operator)
}

// DeleteOverride 删除一个用户的例外。
func (s *DeviceLimitService) DeleteOverride(ctx context.Context, appID, userID int64) error {
	deleted, err := s.pg.DeleteDeviceLimitOverride(ctx, appID, userID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40400, http.StatusNotFound, "该用户没有例外设置")
	}
	return nil
}

// UserView 管理端查看一个用户：生效上限、在线设备、台账、例外与最近的处置记录。
func (s *DeviceLimitService) UserView(ctx context.Context, appID, userID int64) (*devicelimit.AdminUserView, error) {
	user, err := s.requireAppUser(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	status, err := s.Status(ctx, appID, userID, "")
	if err != nil {
		return nil, err
	}
	view := &devicelimit.AdminUserView{
		UserID: user.ID, Account: user.Account, Member: s.member(ctx, appID, userID), Status: *status,
		Override: s.override(ctx, appID, userID), Ledger: []devicelimit.LedgerEntry{}, Events: []devicelimit.Event{},
	}
	if profile, err := s.pg.GetUserProfileByUserID(ctx, user.ID); err == nil && profile != nil {
		view.Nickname = profile.Nickname
		view.Avatar = profile.Avatar
	}
	ledger, err := s.pg.ListLoginDevices(ctx, appID, userID, deviceLimitLedgerShown)
	if err != nil {
		return nil, err
	}
	online := map[string]bool{}
	for _, device := range status.Devices {
		online[device.Key] = true
	}
	for i := range ledger {
		ledger[i].Info = s.describeOne(ctx, ledger[i].Model, ledger[i].Platform, "", ledger[i].Name, "", ledger[i].Extra)
		ledger[i].Online = online[ledger[i].DeviceKey]
	}
	view.Ledger = ledger
	events, _, err := s.pg.ListDeviceLimitEvents(ctx, appID, devicelimit.EventQuery{UserID: userID, Page: 1, Limit: 20})
	if err != nil {
		return nil, err
	}
	view.Events = events
	return view, nil
}

// ResetLedger 清空用户的设备台账（key 为空）或删除其中一台：新设备频率从零算起。
func (s *DeviceLimitService) ResetLedger(ctx context.Context, appID, userID int64, key string) (int64, error) {
	if _, err := s.requireAppUser(ctx, appID, userID); err != nil {
		return 0, err
	}
	return s.pg.DeleteLoginDevices(ctx, appID, userID, strings.TrimSpace(key))
}

// Simulate 模拟一次登录：用草稿或已保存的策略、真实或虚构的用户与设备，看会怎么处置。不改动任何数据。
func (s *DeviceLimitService) Simulate(ctx context.Context, appID int64, in devicelimit.SimulateInput) (*devicelimit.SimulateResult, error) {
	var policy devicelimit.Policy
	if in.Policy != nil {
		normalized, err := in.Policy.Normalize()
		if err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		policy = normalized
	} else {
		view, err := s.Policy(ctx, appID)
		if err != nil {
			return nil, err
		}
		policy = view.Policy
	}
	now := s.now()
	platform := devicedomain.NormalizePlatform(in.Platform)
	candidate := devicelimit.Candidate{Key: strings.TrimSpace(in.DeviceKey), Class: devicelimit.ClassOf(platform), Platform: platform, Known: in.Known}
	if candidate.Key == "" {
		candidate.Key = "simulated-new-device"
	}
	var devices []devicelimit.Device
	var limits devicelimit.Limits
	newCount := in.NewDevicesInWindow
	if in.UserID > 0 {
		if _, err := s.requireAppUser(ctx, appID, in.UserID); err != nil {
			return nil, err
		}
		active, err := s.ActiveDevices(ctx, appID, in.UserID)
		if err != nil {
			return nil, err
		}
		devices = s.describe(ctx, active)
		member := s.member(ctx, appID, in.UserID)
		limits = devicelimit.Resolve(policy, member, s.override(ctx, appID, in.UserID), now)
		if devicelimit.Ledgerable(candidate.Key) && !in.Known {
			known, err := s.pg.LoginDeviceKnown(ctx, appID, in.UserID, candidate.Key)
			if err == nil {
				candidate.Known = known
			}
		}
		newCount = s.newDevicesInWindow(ctx, appID, in.UserID, policy.NewDevice)
	} else {
		var override *devicelimit.Override
		if in.Override != nil {
			normalized, err := in.Override.Normalize(now)
			if err != nil {
				return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
			}
			override = &devicelimit.Override{Exempt: normalized.Exempt, MaxDevices: normalized.MaxDevices,
				ClassLimits: normalized.ClassLimits, ExpiresAt: normalized.ExpiresAt}
		}
		limits = devicelimit.Resolve(policy, in.Member, override, now)
		for i, item := range in.Devices {
			key := strings.TrimSpace(item.Key)
			if key == "" {
				key = fmt.Sprintf("sim-%d", i+1)
			}
			p := devicedomain.NormalizePlatform(item.Platform)
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = fmt.Sprintf("设备 %d", i+1)
			}
			devices = append(devices, devicelimit.Device{
				Key: key, Platform: p, Class: devicelimit.ClassOf(p), Name: name, Families: []string{key},
				Sessions: 1, LoginAt: now.Add(-time.Duration(max(0, item.LoginMinutes)) * time.Minute),
				LastActiveAt: now.Add(-time.Duration(max(0, item.IdleMinutes)) * time.Minute),
				Info:         &devicedomain.Info{Name: name, Platform: p, Source: devicedomain.InfoSourceClient},
			})
		}
	}
	if devices == nil {
		devices = []devicelimit.Device{}
	}
	decision := devicelimit.Plan(devicelimit.PlanInput{
		Policy: policy, Limits: limits, Devices: devices, Candidate: candidate,
		NewDevicesInWindow: newCount, CanAsk: in.CanAsk,
	})
	return &devicelimit.SimulateResult{Limits: limits, Devices: devices, Decision: decision}, nil
}
