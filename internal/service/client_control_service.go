package service

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authprotocol "aegis/internal/domain/authprotocol"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ClientControlService 版本管控：设置与规则、管控密钥、加密上报与签名裁决、长轮询、网关闸门与统计。
// 判定在 client_control_engine.go（纯函数），密码学构造在 client_control_crypto.go，契约见 docs/client-control.md。
//
// 策略版本（policy_version）是客户端得知「该重新校验了」的唯一信号：设置或规则的任何变更都让它加一，
// 同时写进 Redis 并经 Redis 发布订阅通知所有实例上挂着的长轮询，再发一条 client_control.changed 实时事件。
type ClientControlService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	redis     *redis.Client
	events    AppEventPublisher
	masterKey []byte
	location  *time.Location

	cacheMu sync.Mutex
	cache   map[int64]clientControlSnapshot

	keyMu    sync.Mutex
	keyrings map[int64]*clientControlKeyring

	nonceMu sync.Mutex
	nonces  map[string]time.Time

	hub *clientControlWatchHub
}

type clientControlSnapshot struct {
	settings  appdomain.ClientControlSettings
	rules     []appdomain.ClientControlRule
	fetchedAt time.Time
}

// clientControlKeyring 一个应用的全部管控密钥，私钥按需解密后缓存。
type clientControlKeyring struct {
	keys      []appdomain.ClientControlKey
	private   map[string][]byte
	fetchedAt time.Time
}

const (
	clientControlCacheTTL     = 10 * time.Second
	clientControlKeyCacheTTL  = 5 * time.Minute
	clientControlClockSkew    = 10 * time.Minute
	clientControlNonceTTL     = 2*clientControlClockSkew + time.Minute
	clientControlBoxKeyGrace  = 7 * 24 * time.Hour
	clientControlWatchRecheck = 5 * time.Second
	clientControlRecordTTL    = 5 * time.Second
	clientControlDailyDays    = 14
	clientControlPayloadLimit = 16 << 10

	clientControlChannel = "aegis:client-control:changed"
)

var (
	clientControlPackagePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,254}$`)
	clientControlCertPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func NewClientControlService(log *zap.Logger, pg *pgrepo.Repository, redisClient *redis.Client, cfg config.Config) *ClientControlService {
	if log == nil {
		log = zap.NewNop()
	}
	// 主密钥按用途派生，不与其他模块复用。
	master := sha256.Sum256([]byte("aegis.client-control.master\x00" + cfg.Security.MasterKey))
	s := &ClientControlService{
		log:       log,
		pg:        pg,
		redis:     redisClient,
		masterKey: master[:],
		location:  timeutil.DefaultLocation(),
		cache:     map[int64]clientControlSnapshot{},
		keyrings:  map[int64]*clientControlKeyring{},
		nonces:    map[string]time.Time{},
	}
	s.hub = newClientControlWatchHub(log, redisClient, s.invalidate)
	return s
}

func (s *ClientControlService) SetAppEventPublisher(p AppEventPublisher) { s.events = p }

func clientControlRuleNotFound() error {
	return apperrors.New(40456, http.StatusNotFound, "管控规则不存在")
}

func clientControlInvalid(message string) error {
	return apperrors.New(40000, http.StatusBadRequest, message)
}

/* ───────────────────── 设置 ───────────────────── */

func defaultClientControlSettings(appID int64) appdomain.ClientControlSettings {
	return appdomain.ClientControlSettings{
		AppID:             appID,
		PolicyVersion:     1,
		AllowedPackages:   []string{},
		AllowedCerts:      []string{},
		VerdictTTLSeconds: 86400,
		RecheckSeconds:    600,
		OfflinePolicy:     appdomain.ClientControlOfflineAllow,
		WatchEnabled:      true,
		ServerGate:        true,
	}
}

// Settings 没有设置行时返回默认值（未启用）。
func (s *ClientControlService) Settings(ctx context.Context, appID int64) (*appdomain.ClientControlSettings, error) {
	item, err := s.pg.GetClientControlSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		defaults := defaultClientControlSettings(appID)
		return &defaults, nil
	}
	return item, nil
}

func (s *ClientControlService) UpdateSettings(ctx context.Context, appID int64, patch appdomain.ClientControlSettingsPatch, operator string) (*appdomain.ClientControlSettings, error) {
	current, err := s.Settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	next := *current
	if patch.Enabled != nil {
		next.Enabled = *patch.Enabled
	}
	if patch.AllowedPackages != nil {
		packages, err := normalizeClientControlPackages(*patch.AllowedPackages, 50, "官方包名")
		if err != nil {
			return nil, err
		}
		next.AllowedPackages = packages
	}
	if patch.AllowedCerts != nil {
		certs, err := normalizeClientControlCerts(*patch.AllowedCerts, 20, "官方签名")
		if err != nil {
			return nil, err
		}
		next.AllowedCerts = certs
	}
	if patch.VerdictTTLSeconds != nil {
		next.VerdictTTLSeconds = *patch.VerdictTTLSeconds
	}
	if next.VerdictTTLSeconds < 300 || next.VerdictTTLSeconds > 30*86400 {
		return nil, clientControlInvalid("裁决有效期应在 5 分钟到 30 天之间")
	}
	if patch.RecheckSeconds != nil {
		next.RecheckSeconds = *patch.RecheckSeconds
	}
	if next.RecheckSeconds < 60 || next.RecheckSeconds > 86400 {
		return nil, clientControlInvalid("复查间隔应在 1 分钟到 1 天之间")
	}
	if patch.OfflinePolicy != nil {
		next.OfflinePolicy = strings.TrimSpace(*patch.OfflinePolicy)
	}
	if next.OfflinePolicy != appdomain.ClientControlOfflineAllow && next.OfflinePolicy != appdomain.ClientControlOfflineBlock {
		return nil, clientControlInvalid("离线策略只能是 allow 或 block")
	}
	if patch.WatchEnabled != nil {
		next.WatchEnabled = *patch.WatchEnabled
	}
	if patch.ServerGate != nil {
		next.ServerGate = *patch.ServerGate
	}
	if patch.BlockTitle != nil {
		next.BlockTitle = strings.TrimSpace(*patch.BlockTitle)
	}
	if utf8.RuneCountInString(next.BlockTitle) > 80 {
		return nil, clientControlInvalid("停用标题不超过 80 个字符")
	}
	if patch.BlockMessage != nil {
		next.BlockMessage = strings.TrimSpace(*patch.BlockMessage)
	}
	if utf8.RuneCountInString(next.BlockMessage) > 1000 {
		return nil, clientControlInvalid("停用说明不超过 1000 个字符")
	}
	next.AppID = appID
	next.UpdatedBy = operator
	saved, err := s.pg.SaveClientControlSettings(ctx, next)
	if err != nil {
		return nil, err
	}
	s.changed(ctx, appID, saved.PolicyVersion, "settings")
	return saved, nil
}

// Refresh 策略版本加一，让所有客户端立即重新校验。
func (s *ClientControlService) Refresh(ctx context.Context, appID int64) (int64, error) {
	version, err := s.pg.BumpClientControlPolicyVersion(ctx, appID)
	if err != nil {
		return 0, err
	}
	s.changed(ctx, appID, version, "refresh")
	return version, nil
}

/* ───────────────────── 规则 ───────────────────── */

func (s *ClientControlService) Rules(ctx context.Context, appID int64) ([]appdomain.ClientControlRule, error) {
	return s.pg.ListClientControlRules(ctx, appID)
}

func (s *ClientControlService) CreateRule(ctx context.Context, appID int64, input appdomain.ClientControlRuleInput, operator string) (*appdomain.ClientControlRule, error) {
	rule, err := buildClientControlRule(input)
	if err != nil {
		return nil, err
	}
	rule.AppID = appID
	rule.CreatedBy = operator
	if input.Enabled == nil {
		rule.Enabled = true
	}
	saved, err := s.pg.InsertClientControlRule(ctx, rule)
	if err != nil {
		return nil, err
	}
	s.bump(ctx, appID, "rule.create")
	return saved, nil
}

func (s *ClientControlService) UpdateRule(ctx context.Context, appID int64, ruleID int64, input appdomain.ClientControlRuleInput) (*appdomain.ClientControlRule, error) {
	current, err := s.pg.GetClientControlRule(ctx, appID, ruleID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, clientControlRuleNotFound()
	}
	rule, err := buildClientControlRule(input)
	if err != nil {
		return nil, err
	}
	rule.AppID, rule.ID = appID, ruleID
	if input.Enabled == nil {
		rule.Enabled = current.Enabled
	}
	saved, err := s.pg.UpdateClientControlRule(ctx, rule)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, clientControlRuleNotFound()
	}
	s.bump(ctx, appID, "rule.update")
	return saved, nil
}

func (s *ClientControlService) SetRuleEnabled(ctx context.Context, appID int64, ruleID int64, enabled bool) (*appdomain.ClientControlRule, error) {
	saved, err := s.pg.SetClientControlRuleEnabled(ctx, appID, ruleID, enabled)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, clientControlRuleNotFound()
	}
	s.bump(ctx, appID, "rule.toggle")
	return saved, nil
}

func (s *ClientControlService) DeleteRule(ctx context.Context, appID int64, ruleID int64) error {
	deleted, err := s.pg.DeleteClientControlRule(ctx, appID, ruleID)
	if err != nil {
		return err
	}
	if !deleted {
		return clientControlRuleNotFound()
	}
	s.bump(ctx, appID, "rule.delete")
	return nil
}

// ReorderRules orderedIDs 必须恰好是该应用的全部规则，第一个最先匹配。
func (s *ClientControlService) ReorderRules(ctx context.Context, appID int64, orderedIDs []int64) ([]appdomain.ClientControlRule, error) {
	rules, err := s.pg.ListClientControlRules(ctx, appID)
	if err != nil {
		return nil, err
	}
	known := make(map[int64]bool, len(rules))
	for _, rule := range rules {
		known[rule.ID] = true
	}
	seen := make(map[int64]bool, len(orderedIDs))
	for _, id := range orderedIDs {
		if !known[id] || seen[id] {
			return nil, clientControlInvalid("排序里的规则 ID 无效或重复")
		}
		seen[id] = true
	}
	if len(seen) != len(known) {
		return nil, clientControlInvalid("排序需要包含全部规则")
	}
	if err := s.pg.SetClientControlRulePriorities(ctx, appID, orderedIDs); err != nil {
		return nil, err
	}
	s.bump(ctx, appID, "rule.reorder")
	return s.pg.ListClientControlRules(ctx, appID)
}

// buildClientControlRule 校验并归一化规则输入（条件的写法与引擎的判定口径一致）。
func buildClientControlRule(input appdomain.ClientControlRuleInput) (appdomain.ClientControlRule, error) {
	rule := appdomain.ClientControlRule{
		Name:        strings.TrimSpace(input.Name),
		Priority:    input.Priority,
		Action:      strings.TrimSpace(input.Action),
		Title:       strings.TrimSpace(input.Title),
		Message:     strings.TrimSpace(input.Message),
		ActionURL:   strings.TrimSpace(input.ActionURL),
		ActionLabel: strings.TrimSpace(input.ActionLabel),
		StartsAt:    input.StartsAt,
		EndsAt:      input.EndsAt,
		Note:        strings.TrimSpace(input.Note),
	}
	if input.Enabled != nil {
		rule.Enabled = *input.Enabled
	}
	if rule.Name == "" || utf8.RuneCountInString(rule.Name) > 80 {
		return rule, clientControlInvalid("规则名称不能为空且不超过 80 个字符")
	}
	if _, ok := appdomain.ValidClientControlActions[rule.Action]; !ok {
		return rule, clientControlInvalid("结果只能是 allow、warn、force_update 或 block")
	}
	if rule.Priority < -100000 || rule.Priority > 100000 {
		return rule, clientControlInvalid("优先级超出范围")
	}
	if utf8.RuneCountInString(rule.Title) > 80 {
		return rule, clientControlInvalid("标题不超过 80 个字符")
	}
	if utf8.RuneCountInString(rule.Message) > 2000 {
		return rule, clientControlInvalid("说明不超过 2000 个字符")
	}
	if rule.ActionURL != "" {
		parsed, err := url.Parse(rule.ActionURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || len(rule.ActionURL) > 1000 {
			return rule, clientControlInvalid("跳转链接必须是 http 或 https 地址")
		}
	}
	if utf8.RuneCountInString(rule.ActionLabel) > 32 {
		return rule, clientControlInvalid("按钮文字不超过 32 个字符")
	}
	if utf8.RuneCountInString(rule.Note) > 500 {
		return rule, clientControlInvalid("备注不超过 500 个字符")
	}
	if rule.StartsAt != nil && rule.EndsAt != nil && !rule.StartsAt.Before(*rule.EndsAt) {
		return rule, clientControlInvalid("生效开始时间必须早于结束时间")
	}
	conditions, err := normalizeClientControlConditions(input.Conditions)
	if err != nil {
		return rule, err
	}
	rule.Conditions = conditions
	return rule, nil
}

func normalizeClientControlConditions(c appdomain.ClientControlConditions) (appdomain.ClientControlConditions, error) {
	var err error
	if c.Packages, err = normalizeClientControlPackages(c.Packages, 200, "包名"); err != nil {
		return c, err
	}
	if c.Certs, err = normalizeClientControlCerts(c.Certs, 50, "签名证书"); err != nil {
		return c, err
	}
	c.VersionNames = normalizeClientControlStrings(c.VersionNames, false)
	c.Platforms = normalizeClientControlStrings(c.Platforms, true)
	c.DeviceIDs = normalizeClientControlStrings(c.DeviceIDs, false)
	c.Installers = normalizeClientControlStrings(c.Installers, false)
	for _, list := range [][]string{c.VersionNames, c.Platforms, c.DeviceIDs, c.Installers} {
		if len(list) > 1000 {
			return c, clientControlInvalid("单个条件最多 1000 个取值")
		}
		for _, value := range list {
			if len(value) > 255 {
				return c, clientControlInvalid("条件取值过长")
			}
		}
	}
	userIDs := make([]int64, 0, len(c.UserIDs))
	for _, id := range c.UserIDs {
		if id > 0 && !slices.Contains(userIDs, id) {
			userIDs = append(userIDs, id)
		}
	}
	if len(userIDs) > 1000 {
		return c, clientControlInvalid("单个条件最多 1000 个取值")
	}
	c.UserIDs = userIDs
	if c.VersionCodeMin < 0 || c.VersionCodeMax < 0 || c.OSVersionMin < 0 || c.OSVersionMax < 0 {
		return c, clientControlInvalid("版本号与系统版本不能为负数")
	}
	if c.VersionCodeMin > 0 && c.VersionCodeMax > 0 && c.VersionCodeMin > c.VersionCodeMax {
		return c, clientControlInvalid("版本号下限不能高于上限")
	}
	if c.OSVersionMin > 0 && c.OSVersionMax > 0 && c.OSVersionMin > c.OSVersionMax {
		return c, clientControlInvalid("系统版本下限不能高于上限")
	}
	if len(c.Packages) == 0 {
		c.PackagesNegate = false
	}
	if len(c.Certs) == 0 {
		c.CertsNegate = false
	}
	if len(c.Installers) == 0 {
		c.InstallersNegate = false
	}
	return c, nil
}

func normalizeClientControlStrings(values []string, lower bool) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if lower {
			value = strings.ToLower(value)
		}
		if value != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func normalizeClientControlPackages(values []string, limit int, label string) ([]string, error) {
	result := normalizeClientControlStrings(values, false)
	if len(result) > limit {
		return nil, clientControlInvalid(fmt.Sprintf("%s最多 %d 个", label, limit))
	}
	for _, value := range result {
		if !clientControlPackagePattern.MatchString(value) {
			return nil, clientControlInvalid(fmt.Sprintf("%s %q 格式无效", label, value))
		}
	}
	return result, nil
}

func normalizeClientControlCerts(values []string, limit int, label string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeCertDigest(value)
		if value == "" || slices.Contains(result, value) {
			continue
		}
		if !clientControlCertPattern.MatchString(value) {
			return nil, clientControlInvalid(label + "必须是 SHA-256 摘要（64 位十六进制，可带冒号）")
		}
		result = append(result, value)
	}
	if len(result) > limit {
		return nil, clientControlInvalid(fmt.Sprintf("%s最多 %d 个", label, limit))
	}
	return result, nil
}

/* ───────────────────── 密钥 ───────────────────── */

func (s *ClientControlService) Keys(ctx context.Context, appID int64) ([]appdomain.ClientControlKeyView, error) {
	app, err := s.app(ctx, appID)
	if err != nil {
		return nil, err
	}
	ring, err := s.keyring(ctx, app)
	if err != nil {
		return nil, err
	}
	views := make([]appdomain.ClientControlKeyView, 0, len(ring.keys))
	for _, key := range ring.keys {
		views = append(views, clientControlKeyView(key))
	}
	return views, nil
}

// RotateKey 生成新的 sign 或 box 密钥并退役旧的。
//
// 新签名密钥由当前签名密钥背书，客户端顺着背书链信任它；已退役的 box 密钥在
// clientControlBoxKeyGrace 内仍可解密上报，缓存了旧 /config 的客户端不会立刻失败。
func (s *ClientControlService) RotateKey(ctx context.Context, appID int64, kind string) (*appdomain.ClientControlKeyView, error) {
	if kind != appdomain.ClientControlKeySign && kind != appdomain.ClientControlKeyBox {
		return nil, clientControlInvalid("密钥类型只能是 sign 或 box")
	}
	app, err := s.app(ctx, appID)
	if err != nil {
		return nil, err
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	ring, err := s.loadKeyringLocked(ctx, app)
	if err != nil {
		return nil, err
	}
	key, err := s.generateKey(app, kind, ring)
	if err != nil {
		return nil, err
	}
	if err := s.pg.InsertClientControlKey(ctx, key); err != nil {
		return nil, err
	}
	delete(s.keyrings, appID)
	view := clientControlKeyView(key)
	view.Status = appdomain.ClientControlKeyActive
	view.CreatedAt = time.Now()
	return &view, nil
}

func clientControlKeyView(key appdomain.ClientControlKey) appdomain.ClientControlKeyView {
	view := appdomain.ClientControlKeyView{
		KeyID:      key.KeyID,
		Kind:       key.Kind,
		PublicKey:  base64.RawURLEncoding.EncodeToString(key.PublicKey),
		Status:     key.Status,
		EndorsedBy: key.EndorsedBy,
		CreatedAt:  key.CreatedAt,
		RetiredAt:  key.RetiredAt,
	}
	if len(key.Endorsement) > 0 {
		view.Endorsement = base64.RawURLEncoding.EncodeToString(key.Endorsement)
	}
	return view
}

// keyring 取应用的密钥（缓存 5 分钟），缺哪类就懒生成哪类。
func (s *ClientControlService) keyring(ctx context.Context, app *appdomain.App) (*clientControlKeyring, error) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if ring, ok := s.keyrings[app.ID]; ok && time.Since(ring.fetchedAt) < clientControlKeyCacheTTL {
		return ring, nil
	}
	return s.loadKeyringLocked(ctx, app)
}

func (s *ClientControlService) loadKeyringLocked(ctx context.Context, app *appdomain.App) (*clientControlKeyring, error) {
	keys, err := s.pg.ListClientControlKeys(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	ring := &clientControlKeyring{keys: keys, private: map[string][]byte{}, fetchedAt: time.Now()}
	generated := false
	for _, kind := range []string{appdomain.ClientControlKeySign, appdomain.ClientControlKeyBox} {
		if ring.active(kind) != nil {
			continue
		}
		key, err := s.generateKey(app, kind, ring)
		if err != nil {
			return nil, err
		}
		if _, err := s.pg.InsertClientControlKeyIfAbsent(ctx, key); err != nil {
			return nil, err
		}
		generated = true
	}
	if generated {
		if ring.keys, err = s.pg.ListClientControlKeys(ctx, app.ID); err != nil {
			return nil, err
		}
	}
	s.keyrings[app.ID] = ring
	return ring, nil
}

// generateKey 生成一把新密钥；sign 类且已有签名密钥时由它背书。
func (s *ClientControlService) generateKey(app *appdomain.App, kind string, ring *clientControlKeyring) (appdomain.ClientControlKey, error) {
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return appdomain.ClientControlKey{}, err
	}
	prefix := "ccb_"
	if kind == appdomain.ClientControlKeySign {
		prefix = "ccs_"
	}
	key := appdomain.ClientControlKey{AppID: app.ID, KeyID: prefix + hex.EncodeToString(idBytes), Kind: kind, Status: appdomain.ClientControlKeyActive}
	var private []byte
	switch kind {
	case appdomain.ClientControlKeySign:
		public, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return key, err
		}
		key.PublicKey, private = public, priv.Seed()
		if current := ring.active(appdomain.ClientControlKeySign); current != nil {
			signer, err := s.signingKey(ring, current)
			if err != nil {
				return key, err
			}
			key.EndorsedBy = current.KeyID
			key.Endorsement = ed25519.Sign(signer, clientControlEndorseMessage(app.AppKey, key.KeyID,
				base64.RawURLEncoding.EncodeToString(key.PublicKey)))
		}
	default:
		priv, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return key, err
		}
		key.PublicKey, private = priv.PublicKey().Bytes(), priv.Bytes()
	}
	cipher, err := encryptSecret(s.masterKey, base64.RawURLEncoding.EncodeToString(private))
	if err != nil {
		return key, err
	}
	key.PrivateKeyCipher = cipher
	return key, nil
}

func (r *clientControlKeyring) active(kind string) *appdomain.ClientControlKey {
	for i := range r.keys {
		if r.keys[i].Kind == kind && r.keys[i].Status == appdomain.ClientControlKeyActive {
			return &r.keys[i]
		}
	}
	return nil
}

func (r *clientControlKeyring) find(kind, keyID string) *appdomain.ClientControlKey {
	for i := range r.keys {
		if r.keys[i].Kind == kind && r.keys[i].KeyID == keyID {
			return &r.keys[i]
		}
	}
	return nil
}

// privateKey 解密私钥（调用方持有 keyMu 或接受并发解密同一把密钥的重复开销）。
func (s *ClientControlService) privateKey(ring *clientControlKeyring, key *appdomain.ClientControlKey) ([]byte, error) {
	if cached, ok := ring.private[key.KeyID]; ok {
		return cached, nil
	}
	plain, err := decryptSecret(s.masterKey, key.PrivateKeyCipher)
	if err != nil {
		return nil, fmt.Errorf("decrypt client control key %s: %w", key.KeyID, err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(plain)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("client control key %s is malformed", key.KeyID)
	}
	ring.private[key.KeyID] = raw
	return raw, nil
}

func (s *ClientControlService) signingKey(ring *clientControlKeyring, key *appdomain.ClientControlKey) (ed25519.PrivateKey, error) {
	seed, err := s.privateKey(ring, key)
	if err != nil {
		return nil, err
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

/* ───────────────────── 网关：/config、上报、长轮询 ───────────────────── */

// Spec /config 下发的管控规格：全部签名公钥（背书链）与由当前签名密钥签过的 box 公钥。
func (s *ClientControlService) Spec(ctx context.Context, app *appdomain.App) (*authprotocol.ClientControlSpec, error) {
	snapshot, err := s.snapshot(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	ring, err := s.keyring(ctx, app)
	if err != nil {
		return nil, err
	}
	sign := ring.active(appdomain.ClientControlKeySign)
	box := ring.active(appdomain.ClientControlKeyBox)
	if sign == nil || box == nil {
		return nil, errors.New("client control keys unavailable")
	}
	s.keyMu.Lock()
	signer, err := s.signingKey(ring, sign)
	s.keyMu.Unlock()
	if err != nil {
		// 私钥解不开几乎总是主密钥换了：/config 会因此不再下发 clientControl，必须让运维看见。
		s.log.Warn("client control signing key unusable", zap.Int64("appid", app.ID), zap.String("key_id", sign.KeyID), zap.Error(err))
		return nil, err
	}
	boxPublic := base64.RawURLEncoding.EncodeToString(box.PublicKey)
	spec := &authprotocol.ClientControlSpec{
		Protocol:       appdomain.ClientControlProtocol,
		Enabled:        snapshot.settings.Enabled,
		PolicyVersion:  snapshot.settings.PolicyVersion,
		RecheckSeconds: snapshot.settings.RecheckSeconds,
		WatchEnabled:   snapshot.settings.WatchEnabled,
		SigningKeyID:   sign.KeyID,
		BoxKey: authprotocol.ClientControlBoxKey{
			KeyID:        box.KeyID,
			PublicKey:    boxPublic,
			SigningKeyID: sign.KeyID,
			Signature:    base64.RawURLEncoding.EncodeToString(ed25519.Sign(signer, clientControlBoxMessage(app.AppKey, box.KeyID, boxPublic))),
		},
		CheckPath: "/client-control/check",
		WatchPath: "/client-control/watch",
	}
	for _, key := range ring.keys {
		if key.Kind != appdomain.ClientControlKeySign {
			continue
		}
		view := clientControlKeyView(key)
		spec.SigningKeys = append(spec.SigningKeys, authprotocol.ClientControlSigningKey{
			KeyID: view.KeyID, PublicKey: view.PublicKey, Status: view.Status,
			EndorsedBy: view.EndorsedBy, Endorsement: view.Endorsement,
		})
	}
	return spec, nil
}

// Check 处理一次加密上报：解密、防重放、判定、签名、加密返回；安装与统计异步记录。
func (s *ClientControlService) Check(ctx context.Context, app *appdomain.App, req ClientControlSealedRequest, userID int64, ip string) (*ClientControlSealedResponse, error) {
	if len(req.Payload) > clientControlPayloadLimit {
		return nil, clientControlPayloadError("管控载荷过大")
	}
	now := time.Now()
	skew := now.Sub(time.Unix(req.Timestamp, 0))
	if skew > clientControlClockSkew || skew < -clientControlClockSkew {
		return nil, apperrors.New(40071, http.StatusBadRequest, "请求时间戳无效或已过期")
	}
	snapshot, err := s.snapshot(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	ring, err := s.keyring(ctx, app)
	if err != nil {
		return nil, err
	}
	boxKey := ring.find(appdomain.ClientControlKeyBox, strings.TrimSpace(req.KeyID))
	if boxKey == nil || (boxKey.RetiredAt != nil && now.Sub(*boxKey.RetiredAt) > clientControlBoxKeyGrace) {
		return nil, apperrors.New(40457, http.StatusNotFound, "管控密钥不存在或已过期，请重新获取配置")
	}
	sign := ring.active(appdomain.ClientControlKeySign)
	if sign == nil {
		return nil, errors.New("client control signing key unavailable")
	}
	s.keyMu.Lock()
	boxPrivate, err := s.privateKey(ring, boxKey)
	var signer ed25519.PrivateKey
	if err == nil {
		signer, err = s.signingKey(ring, sign)
	}
	s.keyMu.Unlock()
	if err != nil {
		return nil, err
	}

	plaintext, requestKey, err := openClientControlRequest(boxPrivate, app.AppKey, req)
	if err != nil {
		return nil, err
	}
	// 解密成功之后才占用 nonce：伪造的请求过不了 AEAD，占不了 Redis。
	nonce := strings.TrimSpace(req.Nonce)
	if fresh, err := s.claimNonce(ctx, app.ID, nonce); err != nil {
		return nil, err
	} else if !fresh {
		return nil, apperrors.New(40970, http.StatusConflict, "请求已被使用")
	}

	var report appdomain.ClientReport
	if err := json.Unmarshal(plaintext, &report); err != nil {
		return nil, clientControlPayloadError("管控上报内容无效")
	}
	report = sanitizeClientReport(normalizeClientReport(report))
	if report.PackageName == "" {
		return nil, clientControlPayloadError("管控上报缺少包名")
	}
	report.UserID, report.IP = userID, truncateRunes(strings.TrimSpace(ip), 64)

	settings := snapshot.settings
	decision := evaluateClientControl(settings, snapshot.rules, report, now, false)
	verdict := appdomain.ClientVerdict{
		V:             1,
		AppKey:        app.AppKey,
		PolicyVersion: settings.PolicyVersion,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(time.Duration(settings.VerdictTTLSeconds) * time.Second).Unix(),
		Challenge:     nonce,
		Subject: appdomain.ClientVerdictSubject{
			PackageName: report.PackageName, VersionCode: report.VersionCode,
			CertSHA256: report.CertSHA256, DeviceID: report.DeviceID,
		},
		Enabled:       settings.Enabled,
		Action:        decision.Action,
		Reason:        decision.Reason,
		RuleID:        decision.RuleID,
		Title:         decision.Title,
		Message:       decision.Message,
		ActionURL:     decision.ActionURL,
		ActionLabel:   decision.ActionLabel,
		RecheckAfter:  settings.RecheckSeconds,
		WatchEnabled:  settings.WatchEnabled,
		OfflinePolicy: settings.OfflinePolicy,
	}
	raw, err := json.Marshal(verdict)
	if err != nil {
		return nil, err
	}
	signed, err := json.Marshal(ClientControlSignedVerdict{
		Verdict:      base64.RawURLEncoding.EncodeToString(raw),
		Signature:    base64.RawURLEncoding.EncodeToString(signClientVerdict(signer, raw)),
		SigningKeyID: sign.KeyID,
	})
	if err != nil {
		return nil, err
	}
	responseNonce, sealed, err := sealClientControlResponse(requestKey, app.AppKey, boxKey.KeyID, nonce, signed)
	if err != nil {
		return nil, err
	}
	s.record(app.ID, report, decision)
	return &ClientControlSealedResponse{KeyID: boxKey.KeyID, Nonce: responseNonce, Payload: sealed}, nil
}

// sanitizeClientReport 截断超长字段，与表结构的列宽一致。
func sanitizeClientReport(r appdomain.ClientReport) appdomain.ClientReport {
	r.PackageName = truncateRunes(r.PackageName, 255)
	r.VersionName = truncateRunes(r.VersionName, 64)
	r.Platform = truncateRunes(r.Platform, 16)
	r.CertSHA256 = truncateRunes(r.CertSHA256, 64)
	r.Installer = truncateRunes(r.Installer, 255)
	r.DeviceID = truncateRunes(r.DeviceID, 128)
	r.Model = truncateRunes(strings.TrimSpace(r.Model), 128)
	r.Brand = truncateRunes(strings.TrimSpace(r.Brand), 64)
	r.ABI = truncateRunes(strings.TrimSpace(r.ABI), 32)
	if r.VersionCode < 0 {
		r.VersionCode = 0
	}
	if r.OSVersion < 0 {
		r.OSVersion = 0
	}
	return r
}

// record 异步记下安装、每日统计与规则命中，不拖慢上报响应。
func (s *ClientControlService) record(appID int64, report appdomain.ClientReport, decision appdomain.ClientControlDecision) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), clientControlRecordTTL)
		defer cancel()
		install := appdomain.ClientInstall{
			DeviceID: report.DeviceID, PackageName: report.PackageName, VersionName: report.VersionName,
			VersionCode: report.VersionCode, Platform: report.Platform, CertSHA256: report.CertSHA256,
			Installer: report.Installer, OSVersion: report.OSVersion, Model: report.Model, Brand: report.Brand,
			ABI: report.ABI, IP: report.IP, LastAction: decision.Action, LastReason: decision.Reason,
		}
		if install.DeviceID == "" {
			// 没有设备 ID 时按来源推一个匿名标识，只用于设备清单；裁决的 subject 仍是空串，与客户端自报一致。
			sum := sha256.Sum256([]byte(report.IP + "\x00" + report.Model + "\x00" + report.Brand + "\x00" + report.PackageName))
			install.DeviceID = "anon-" + hex.EncodeToString(sum[:8])
		}
		if report.UserID > 0 {
			userID := report.UserID
			install.UserID = &userID
		}
		if decision.RuleID > 0 {
			ruleID := decision.RuleID
			install.LastRuleID = &ruleID
		}
		newToday, err := s.pg.UpsertClientInstall(ctx, appID, install)
		if err != nil {
			s.log.Warn("record client install failed", zap.Int64("appid", appID), zap.Error(err))
		}
		blocked := appdomain.ClientControlActionBlocks(decision.Action)
		if err := s.pg.RecordClientControlDaily(ctx, appID, time.Now().In(s.location), newToday, blocked); err != nil {
			s.log.Warn("record client control daily failed", zap.Int64("appid", appID), zap.Error(err))
		}
		if decision.RuleID > 0 {
			if err := s.pg.RecordClientControlRuleHit(ctx, decision.RuleID); err != nil {
				s.log.Warn("record client control rule hit failed", zap.Int64("rule_id", decision.RuleID), zap.Error(err))
			}
		}
	}()
}

// claimNonce 占用一个 nonce，返回是否是第一次出现。没有 Redis 时退回进程内记录。
func (s *ClientControlService) claimNonce(ctx context.Context, appID int64, nonce string) (bool, error) {
	key := fmt.Sprintf("aegis:client-control:nonce:%d:%s", appID, nonce)
	if s.redis != nil {
		fresh, err := s.redis.SetNX(ctx, key, 1, clientControlNonceTTL).Result()
		if err == nil {
			return fresh, nil
		}
		s.log.Warn("client control nonce check fell back to memory", zap.Error(err))
	}
	now := time.Now()
	s.nonceMu.Lock()
	defer s.nonceMu.Unlock()
	if len(s.nonces) > 10000 {
		for k, expires := range s.nonces {
			if now.After(expires) {
				delete(s.nonces, k)
			}
		}
	}
	if expires, ok := s.nonces[key]; ok && now.Before(expires) {
		return false, nil
	}
	s.nonces[key] = now.Add(clientControlNonceTTL)
	return true, nil
}

// Watch 长轮询：策略版本与 since 不同时立即返回；否则挂起到变更或 wait 到期。
func (s *ClientControlService) Watch(ctx context.Context, appID int64, since int64, wait time.Duration) (*appdomain.ClientControlWatchResult, error) {
	snapshot, err := s.snapshot(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := snapshot.settings
	current, err := s.policyVersion(ctx, appID)
	if err != nil {
		return nil, err
	}
	result := &appdomain.ClientControlWatchResult{PolicyVersion: current, WatchEnabled: settings.WatchEnabled}
	if current != since {
		result.Changed = true
		return result, nil
	}
	if !settings.WatchEnabled || wait <= 0 {
		if !settings.WatchEnabled {
			result.RetryAfter = settings.RecheckSeconds
		}
		return result, nil
	}

	notify, cancel := s.hub.subscribe(appID)
	defer cancel()
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(clientControlWatchRecheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return result, nil
		case <-deadline.C:
			return result, nil
		case <-notify:
		case <-ticker.C:
		}
		// 发布订阅丢了消息也不怕：定期回读 Redis 里的版本。
		latest, err := s.policyVersion(ctx, appID)
		if err != nil {
			continue
		}
		if latest != since {
			result.PolicyVersion, result.Changed = latest, true
			return result, nil
		}
	}
}

// GateDecision 网关闸门：只凭请求头判定，宁可放过不可错杀（见 evaluateClientControlPartial）。
func (s *ClientControlService) GateDecision(ctx context.Context, appID int64, report appdomain.ClientReport) (*appdomain.ClientControlDecision, int64, bool) {
	snapshot, err := s.snapshot(ctx, appID)
	if err != nil {
		s.log.Warn("client control gate skipped", zap.Int64("appid", appID), zap.Error(err))
		return nil, 0, false
	}
	settings := snapshot.settings
	if !settings.Enabled || !settings.ServerGate {
		return nil, settings.PolicyVersion, false
	}
	report = normalizeClientReport(report)
	known := clientReportKnown{
		VersionCode: report.VersionCode > 0,
		VersionName: report.VersionName != "",
		Cert:        report.CertSHA256 != "",
		Platform:    report.Platform != "",
		Device:      report.DeviceID != "",
		// 请求头里的系统版本是版本名（如 14），规则里的是 API 级别，口径不同，闸门不用它。
	}
	decision := evaluateClientControlPartial(settings, snapshot.rules, report, known, time.Now())
	return &decision, settings.PolicyVersion, true
}

/* ───────────────────── 管理端：统计与模拟 ───────────────────── */

func (s *ClientControlService) Overview(ctx context.Context, appID int64) (*appdomain.ClientControlOverview, error) {
	settings, err := s.Settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	counts, err := s.pg.ClientControlCounts(ctx, appID)
	if err != nil {
		return nil, err
	}
	rules, err := s.pg.ListClientControlRules(ctx, appID)
	if err != nil {
		return nil, err
	}
	keys, err := s.pg.ListClientControlKeys(ctx, appID)
	if err != nil {
		return nil, err
	}
	today := time.Now().In(s.location)
	start := today.AddDate(0, 0, -(clientControlDailyDays - 1))
	daily, err := s.pg.ListClientControlDaily(ctx, appID, start)
	if err != nil {
		return nil, err
	}
	overview := &appdomain.ClientControlOverview{
		Settings: *settings, Devices24h: counts.Devices24h, Devices7d: counts.Devices7d, Devices30d: counts.Devices30d,
		Blocked7d: counts.Blocked7d, Packages: counts.Packages, Versions: counts.Versions,
		Rules: int64(len(rules)), ActionBreakdown: counts.Actions,
	}
	now := time.Now()
	for _, rule := range rules {
		if rule.ActiveAt(now) {
			overview.ActiveRules++
		}
	}
	for _, key := range keys {
		if key.Status != appdomain.ClientControlKeyActive {
			continue
		}
		view := clientControlKeyView(key)
		if key.Kind == appdomain.ClientControlKeySign {
			overview.SigningKey = &view
		} else {
			overview.BoxKey = &view
		}
	}
	// 补齐没有上报的日期，图表按天连续。
	byDay := make(map[string]appdomain.ClientControlDayStat, len(daily))
	for _, item := range daily {
		byDay[item.Day] = item
	}
	overview.Daily = make([]appdomain.ClientControlDayStat, 0, clientControlDailyDays)
	for i := 0; i < clientControlDailyDays; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		item, ok := byDay[day]
		if !ok {
			item = appdomain.ClientControlDayStat{Day: day}
		}
		overview.Daily = append(overview.Daily, item)
	}
	return overview, nil
}

// Versions 版本分布；decision 按当前规则只凭包名、版本、签名、平台估算。
func (s *ClientControlService) Versions(ctx context.Context, appID int64, days int) ([]appdomain.ClientInstallGroup, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	groups, err := s.pg.ListClientInstallGroups(ctx, appID, time.Now().Add(-time.Duration(days)*24*time.Hour), 200)
	if err != nil {
		return nil, err
	}
	settings, err := s.Settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	rules, err := s.pg.ListClientControlRules(ctx, appID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	known := clientReportKnown{VersionCode: true, VersionName: true, Cert: true, Platform: true}
	for i := range groups {
		group := groups[i]
		decision := evaluateClientControlPartial(*settings, rules, appdomain.ClientReport{
			PackageName: group.PackageName, VersionName: group.VersionName, VersionCode: group.VersionCode,
			CertSHA256: group.CertSHA256, Platform: group.Platform,
		}, known, now)
		groups[i].Decision = &decision
	}
	return groups, nil
}

func (s *ClientControlService) Installs(ctx context.Context, appID int64, query appdomain.ClientInstallQuery) (*appdomain.ClientInstallListResult, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.Limit <= 0 || query.Limit > 100 {
		query.Limit = 20
	}
	if query.Days < 0 || query.Days > 365 {
		query.Days = 0
	}
	query.CertSHA256 = normalizeCertDigest(query.CertSHA256)
	if query.Action != "" {
		if _, ok := appdomain.ValidClientControlActions[query.Action]; !ok {
			return nil, clientControlInvalid("结果筛选无效")
		}
	}
	items, total, err := s.pg.ListClientInstalls(ctx, appID, query)
	if err != nil {
		return nil, err
	}
	pages := int((total + int64(query.Limit) - 1) / int64(query.Limit))
	return &appdomain.ClientInstallListResult{Items: items, Page: query.Page, Limit: query.Limit, Total: total, TotalPages: pages}, nil
}

func (s *ClientControlService) DeleteInstall(ctx context.Context, appID int64, deviceID string, packageName string) error {
	deleted, err := s.pg.DeleteClientInstall(ctx, appID, deviceID, packageName)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40459, http.StatusNotFound, "设备记录不存在")
	}
	return nil
}

// Simulate 按当前规则模拟一次判定（读库，不走缓存）。管控未启用时按启用后的效果判定并在解释里注明。
func (s *ClientControlService) Simulate(ctx context.Context, appID int64, report appdomain.ClientReport) (*appdomain.ClientControlDecision, error) {
	settings, err := s.Settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	rules, err := s.pg.ListClientControlRules(ctx, appID)
	if err != nil {
		return nil, err
	}
	effective := *settings
	var preface []appdomain.ClientControlTraceStep
	if !effective.Enabled {
		effective.Enabled = true
		preface = append(preface, appdomain.ClientControlTraceStep{Name: "管控开关", Detail: "版本管控尚未启用，以下是启用后的判定"})
	}
	decision := evaluateClientControl(effective, rules, report, time.Now(), true)
	decision.Trace = append(preface, decision.Trace...)
	return &decision, nil
}

/* ───────────────────── 内部 ───────────────────── */

func (s *ClientControlService) app(ctx context.Context, appID int64) (*appdomain.App, error) {
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40404, http.StatusNotFound, "应用不存在")
	}
	return app, nil
}

// snapshot 设置与规则（缓存 10 秒，变更时本实例立即失效，其他实例经发布订阅失效）。
func (s *ClientControlService) snapshot(ctx context.Context, appID int64) (clientControlSnapshot, error) {
	s.cacheMu.Lock()
	entry, ok := s.cache[appID]
	s.cacheMu.Unlock()
	if ok && time.Since(entry.fetchedAt) < clientControlCacheTTL {
		return entry, nil
	}
	settings, err := s.Settings(ctx, appID)
	if err != nil {
		return clientControlSnapshot{}, err
	}
	rules, err := s.pg.ListClientControlRules(ctx, appID)
	if err != nil {
		return clientControlSnapshot{}, err
	}
	entry = clientControlSnapshot{settings: *settings, rules: rules, fetchedAt: time.Now()}
	s.cacheMu.Lock()
	s.cache[appID] = entry
	s.cacheMu.Unlock()
	return entry, nil
}

func (s *ClientControlService) invalidate(appID int64) {
	s.cacheMu.Lock()
	delete(s.cache, appID)
	s.cacheMu.Unlock()
}

func clientControlVersionKey(appID int64) string {
	return fmt.Sprintf("aegis:client-control:policy:%d", appID)
}

// policyVersion 先读 Redis，没有再读库并回填。
func (s *ClientControlService) policyVersion(ctx context.Context, appID int64) (int64, error) {
	if s.redis != nil {
		if value, err := s.redis.Get(ctx, clientControlVersionKey(appID)).Int64(); err == nil {
			return value, nil
		}
	}
	settings, err := s.Settings(ctx, appID)
	if err != nil {
		return 0, err
	}
	if s.redis != nil {
		_ = s.redis.Set(ctx, clientControlVersionKey(appID), settings.PolicyVersion, 24*time.Hour).Err()
	}
	return settings.PolicyVersion, nil
}

func (s *ClientControlService) bump(ctx context.Context, appID int64, action string) {
	version, err := s.pg.BumpClientControlPolicyVersion(ctx, appID)
	if err != nil {
		s.log.Warn("bump client control policy version failed", zap.Int64("appid", appID), zap.Error(err))
		s.invalidate(appID)
		return
	}
	s.changed(ctx, appID, version, action)
}

// changed 策略版本变了：清缓存、写 Redis、通知各实例的长轮询、发实时事件。
func (s *ClientControlService) changed(ctx context.Context, appID int64, version int64, action string) {
	s.invalidate(appID)
	if s.redis != nil {
		if err := s.redis.Set(ctx, clientControlVersionKey(appID), version, 24*time.Hour).Err(); err != nil {
			s.log.Warn("store client control policy version failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}
	s.hub.publish(ctx, appID)
	if s.events != nil {
		if err := s.events.PublishAppEvent(ctx, appID, "client_control.changed", map[string]any{
			"action": action, "policyVersion": version,
		}); err != nil {
			s.log.Warn("publish client control event failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}
}

/* ───────────────────── 长轮询通知 ───────────────────── */

// clientControlWatchHub 把「某应用的策略变了」通知给本实例上挂着的长轮询。
// 跨实例经 Redis 发布订阅（每个进程一个订阅，第一次长轮询时启动），没有 Redis 时只在进程内通知。
type clientControlWatchHub struct {
	log        *zap.Logger
	redis      *redis.Client
	onChange   func(appID int64)
	startOnce  sync.Once
	mu         sync.Mutex
	subscribed map[int64]map[chan struct{}]struct{}
}

func newClientControlWatchHub(log *zap.Logger, redisClient *redis.Client, onChange func(int64)) *clientControlWatchHub {
	return &clientControlWatchHub{log: log, redis: redisClient, onChange: onChange, subscribed: map[int64]map[chan struct{}]struct{}{}}
}

func (h *clientControlWatchHub) subscribe(appID int64) (<-chan struct{}, func()) {
	h.startOnce.Do(h.start)
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subscribed[appID] == nil {
		h.subscribed[appID] = map[chan struct{}]struct{}{}
	}
	h.subscribed[appID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subscribed[appID], ch)
		if len(h.subscribed[appID]) == 0 {
			delete(h.subscribed, appID)
		}
		h.mu.Unlock()
	}
}

func (h *clientControlWatchHub) publish(ctx context.Context, appID int64) {
	h.notify(appID)
	if h.redis == nil {
		return
	}
	if err := h.redis.Publish(ctx, clientControlChannel, strconv.FormatInt(appID, 10)).Err(); err != nil {
		h.log.Warn("publish client control change failed", zap.Int64("appid", appID), zap.Error(err))
	}
}

func (h *clientControlWatchHub) notify(appID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribed[appID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// start 订阅 Redis 频道。go-redis 的 PubSub 断线自动重连，这个协程随进程常驻。
func (h *clientControlWatchHub) start() {
	if h.redis == nil {
		return
	}
	pubsub := h.redis.Subscribe(context.Background(), clientControlChannel)
	go func() {
		for message := range pubsub.Channel() {
			appID, err := strconv.ParseInt(message.Payload, 10, 64)
			if err != nil || appID <= 0 {
				continue
			}
			if h.onChange != nil {
				h.onChange(appID)
			}
			h.notify(appID)
		}
	}()
}
