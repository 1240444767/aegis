package service

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// ════════════════════════════════════════════════════════════
//  名单库
//
//  黑名单直接拦、白名单直接放、观察名单交给「观察名单命中」规则加分。
//  名单在评估前判定，因此「已经确认是坏人」不再依赖某条规则恰好引用了 device_blocked 变量 ——
//  重构前设备与 IP 的「封禁」标签只是一个变量，没有规则读它就什么都不拦。
// ════════════════════════════════════════════════════════════

// riskListIndex 现行名单的内存索引。精确类走 map，模式类（网段 / 后缀 / 前缀 / 关键词）逐条比。
type riskListIndex struct {
	exact    map[string][]securitydomain.RiskListEntry
	prefixes []riskPrefixEntry
	patterns map[string][]securitydomain.RiskListEntry
	total    int
}

type riskPrefixEntry struct {
	prefix netip.Prefix
	entry  securitydomain.RiskListEntry
}

func listIndexKey(listType, value string) string {
	return listType + "\x00" + value
}

func buildRiskListIndex(entries []securitydomain.RiskListEntry) *riskListIndex {
	ix := &riskListIndex{
		exact:    make(map[string][]securitydomain.RiskListEntry, len(entries)),
		patterns: map[string][]securitydomain.RiskListEntry{},
		total:    len(entries),
	}
	for _, entry := range entries {
		switch entry.ListType {
		case securitydomain.ListTypeCIDR:
			if prefix, err := netip.ParsePrefix(entry.Value); err == nil {
				ix.prefixes = append(ix.prefixes, riskPrefixEntry{prefix: prefix.Masked(), entry: entry})
			}
		case securitydomain.ListTypeEmailDomain, securitydomain.ListTypePhonePrefix,
			securitydomain.ListTypeASN, securitydomain.ListTypeCountry, securitydomain.ListTypeUAKeyword:
			ix.patterns[entry.ListType] = append(ix.patterns[entry.ListType], entry)
		default:
			key := listIndexKey(entry.ListType, entry.Value)
			ix.exact[key] = append(ix.exact[key], entry)
		}
	}
	return ix
}

// riskListSubject 拿去和名单比对的一组对象
type riskListSubject struct {
	AppID       int64
	Scene       string
	IP          string
	DeviceID    string
	Account     string
	UserID      int64
	EmailDomain string
	Phone       string
	ASN         string
	ISP         string
	Country     string
	UserAgent   string
}

func listSubjectFromEnv(env *RiskEvalEnv) riskListSubject {
	_, _, phone := accountShape(env.Account)
	return riskListSubject{
		AppID: env.AppID, Scene: env.Scene, IP: env.IP, DeviceID: env.DeviceID, Account: env.Account,
		UserID: env.UserID, EmailDomain: env.EmailDomain, Phone: phone, ASN: env.IPASN, ISP: env.IPISP,
		Country: env.GeoCountry, UserAgent: env.UserAgent,
	}
}

// match 返回命中且对该应用、该场景生效的名单
func (ix *riskListIndex) match(subject riskListSubject, now time.Time) []securitydomain.RiskListEntry {
	if ix == nil || ix.total == 0 {
		return nil
	}
	var hits []securitydomain.RiskListEntry
	keep := func(entry securitydomain.RiskListEntry) {
		if entry.Active(now) && entry.AppliesTo(subject.AppID, subject.Scene) {
			hits = append(hits, entry)
		}
	}
	exact := func(listType, value string) {
		if value == "" {
			return
		}
		for _, entry := range ix.exact[listIndexKey(listType, normalizeListValue(listType, value))] {
			keep(entry)
		}
	}
	exact(securitydomain.ListTypeIP, subject.IP)
	exact(securitydomain.ListTypeDevice, subject.DeviceID)
	exact(securitydomain.ListTypeAccount, subject.Account)
	if subject.UserID > 0 {
		exact(securitydomain.ListTypeUser, strconv.FormatInt(subject.UserID, 10))
	}

	if addr, err := netip.ParseAddr(strings.TrimSpace(subject.IP)); err == nil && len(ix.prefixes) > 0 {
		addr = addr.Unmap()
		for _, item := range ix.prefixes {
			if item.prefix.Contains(addr) {
				keep(item.entry)
			}
		}
	}
	for _, entry := range ix.patterns[securitydomain.ListTypeEmailDomain] {
		if subject.EmailDomain != "" && (subject.EmailDomain == entry.Value || strings.HasSuffix(subject.EmailDomain, "."+entry.Value)) {
			keep(entry)
		}
	}
	for _, entry := range ix.patterns[securitydomain.ListTypePhonePrefix] {
		if subject.Phone != "" && strings.HasPrefix(subject.Phone, entry.Value) {
			keep(entry)
		}
	}
	network := strings.ToLower(subject.ASN + " " + subject.ISP)
	for _, entry := range ix.patterns[securitydomain.ListTypeASN] {
		if strings.TrimSpace(network) != "" && strings.Contains(network, entry.Value) {
			keep(entry)
		}
	}
	for _, entry := range ix.patterns[securitydomain.ListTypeCountry] {
		if subject.Country != "" && strings.EqualFold(subject.Country, entry.Value) {
			keep(entry)
		}
	}
	ua := strings.ToLower(subject.UserAgent)
	for _, entry := range ix.patterns[securitydomain.ListTypeUAKeyword] {
		if ua != "" && strings.Contains(ua, entry.Value) {
			keep(entry)
		}
	}
	return hits
}

// normalizeListValue 名单值的规范形态。写入与比对共用，否则「AbC@x.com」拉黑后「abc@x.com」照样进来。
func normalizeListValue(listType, value string) string {
	value = strings.TrimSpace(value)
	switch listType {
	case securitydomain.ListTypeIP:
		if addr, err := netip.ParseAddr(value); err == nil {
			return addr.Unmap().String()
		}
		return value
	case securitydomain.ListTypeCIDR:
		if prefix, err := netip.ParsePrefix(value); err == nil {
			return prefix.Masked().String()
		}
		if addr, err := netip.ParseAddr(value); err == nil {
			return netip.PrefixFrom(addr, addr.BitLen()).String()
		}
		return value
	case securitydomain.ListTypeEmailDomain:
		return strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(value), "@"), ".")
	case securitydomain.ListTypeCountry:
		return strings.ToUpper(value)
	case securitydomain.ListTypeAccount, securitydomain.ListTypeASN, securitydomain.ListTypeUAKeyword:
		return strings.ToLower(value)
	case securitydomain.ListTypePhonePrefix:
		return strings.TrimPrefix(value, "+")
	default:
		return value
	}
}

// validateListValue 名单值的格式校验
func validateListValue(listType, value string) error {
	switch listType {
	case securitydomain.ListTypeIP:
		if _, err := netip.ParseAddr(value); err != nil {
			return fmt.Errorf("不是合法 IP")
		}
	case securitydomain.ListTypeCIDR:
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return fmt.Errorf("不是合法网段")
		}
		// /0 ~ /7 的网段一条就能挡掉半个互联网，几乎一定是手误
		if (prefix.Addr().Is4() && prefix.Bits() < 8) || (prefix.Addr().Is6() && prefix.Bits() < 16) {
			return fmt.Errorf("网段过大")
		}
	case securitydomain.ListTypeUser:
		if id, err := strconv.ParseInt(value, 10, 64); err != nil || id <= 0 {
			return fmt.Errorf("不是合法用户 ID")
		}
	case securitydomain.ListTypeEmailDomain:
		if !strings.Contains(value, ".") || strings.ContainsAny(value, " @/") {
			return fmt.Errorf("不是合法域名")
		}
	case securitydomain.ListTypePhonePrefix:
		if len(value) < 3 || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return fmt.Errorf("号段至少 3 位数字")
		}
	case securitydomain.ListTypeCountry:
		if len(value) != 2 {
			return fmt.Errorf("国家代码应为两位字母")
		}
	case securitydomain.ListTypeUAKeyword, securitydomain.ListTypeASN:
		if len([]rune(value)) < 3 {
			return fmt.Errorf("关键词至少 3 个字符")
		}
	}
	if value == "" {
		return fmt.Errorf("值为空")
	}
	return nil
}

func validateListScenes(scenes []string) error {
	for _, scene := range scenes {
		if !securitydomain.ValidRuleScene(scene) {
			return fmt.Errorf("未知场景：%s", scene)
		}
	}
	return nil
}

// SaveListEntries 批量写入名单（逐行校验，非法的列出来不中断其它行）
func (s *RiskService) SaveListEntries(ctx context.Context, input securitydomain.ListEntryInput) (*securitydomain.ListImportResult, error) {
	if !slices.Contains(securitydomain.ListTypeValues(), input.ListType) {
		return nil, fmt.Errorf("未知名单类型：%s", input.ListType)
	}
	if !isListKind(input.Kind) {
		return nil, fmt.Errorf("未知名单种类：%s", input.Kind)
	}
	if err := validateListScenes(input.Scenes); err != nil {
		return nil, err
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(timeutil.Now()) {
		return nil, fmt.Errorf("到期时间已经过去")
	}
	result := &securitydomain.ListImportResult{Invalid: []string{}}
	values := make([]string, 0, len(input.Values))
	seen := map[string]bool{}
	for _, raw := range input.Values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		value := normalizeListValue(input.ListType, raw)
		if err := validateListValue(input.ListType, value); err != nil {
			result.Invalid = append(result.Invalid, fmt.Sprintf("%s：%s", raw, err.Error()))
			continue
		}
		if !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}
	if len(values) > 5000 {
		return nil, fmt.Errorf("单次最多写入 5000 条")
	}
	if len(values) == 0 {
		return result, nil
	}
	input.Values = values
	created, updated, entries, err := s.pg.UpsertRiskListEntries(ctx, input)
	if err != nil {
		return nil, err
	}
	result.Created, result.Updated = created, updated
	s.syncEntityTags(ctx, entries, false)
	s.invalidateSnapshot()
	return result, nil
}

// UpdateListEntry 修改名单
func (s *RiskService) UpdateListEntry(ctx context.Context, id int64, input securitydomain.ListEntryUpdate) (*securitydomain.RiskListEntry, error) {
	if input.Kind != nil && !isListKind(*input.Kind) {
		return nil, fmt.Errorf("未知名单种类：%s", *input.Kind)
	}
	if input.Scenes != nil {
		if err := validateListScenes(*input.Scenes); err != nil {
			return nil, err
		}
	}
	entry, err := s.pg.UpdateRiskListEntry(ctx, id, input)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, fmt.Errorf("名单不存在")
	}
	s.syncEntityTags(ctx, []securitydomain.RiskListEntry{*entry}, false)
	s.invalidateSnapshot()
	return entry, nil
}

// DeleteListEntries 删除名单
func (s *RiskService) DeleteListEntries(ctx context.Context, ids []int64) (int, error) {
	removed, err := s.pg.DeleteRiskListEntries(ctx, ids)
	if err != nil {
		return 0, err
	}
	s.syncEntityTags(ctx, removed, true)
	s.invalidateSnapshot()
	return len(removed), nil
}

// ListListEntries 分页查询名单
func (s *RiskService) ListListEntries(ctx context.Context, query securitydomain.ListQuery) ([]securitydomain.RiskListEntry, int64, error) {
	return s.pg.ListRiskListEntries(ctx, query)
}

// CheckLists 「这些对象在不在名单里」：直接读库，不受快照刷新延迟影响
func (s *RiskService) CheckLists(ctx context.Context, appID *int64, scene string, ip, deviceID, account string, userID *int64, userAgent string) (*securitydomain.ListCheckResult, error) {
	entries, err := s.pg.ListActiveRiskListEntries(ctx)
	if err != nil {
		return nil, err
	}
	env := newRiskEvalEnv(timeutil.Now())
	env.Scene = scene
	env.IP = strings.TrimSpace(ip)
	env.DeviceID = strings.TrimSpace(deviceID)
	env.Account = strings.TrimSpace(account)
	env.AccountKind, env.EmailDomain, _ = accountShape(env.Account)
	env.UserAgent = userAgent
	if appID != nil {
		env.AppID = *appID
	}
	if userID != nil {
		env.UserID = *userID
	}
	if env.IP != "" {
		s.populateNetworkEnv(ctx, env)
	}
	subject := listSubjectFromEnv(env)
	if scene == "" {
		// 不指定场景时任何场景限定的名单都算
		subject.Scene = securitydomain.SceneAll
		for i := range entries {
			entries[i].Scenes = nil
		}
	}
	hits := buildRiskListIndex(entries).match(subject, timeutil.Now())
	if hits == nil {
		hits = []securitydomain.RiskListEntry{}
	}
	return &securitydomain.ListCheckResult{Hits: hits}, nil
}

// syncEntityTags 名单与设备 / IP 档案上的标签保持一致：标签是档案页上给人看的，判定只认名单。
// removed 为真表示这些条目被删掉了，把仍停留在对应标签上的档案改回正常。
func (s *RiskService) syncEntityTags(ctx context.Context, entries []securitydomain.RiskListEntry, removed bool) {
	for _, entry := range entries {
		if entry.AppID != nil {
			// 应用级名单不改全局档案标签：一个应用的黑名单不代表这台设备在全平台都是坏的
			continue
		}
		tag := securitydomain.TagNormal
		if !removed {
			switch entry.Kind {
			case securitydomain.ListKindBlock:
				tag = securitydomain.TagBlocked
			case securitydomain.ListKindAllow:
				tag = securitydomain.TagTrusted
			case securitydomain.ListKindWatch:
				tag = securitydomain.TagSuspicious
			}
		}
		var err error
		switch entry.ListType {
		case securitydomain.ListTypeIP:
			if removed {
				err = s.pg.ResetIPRiskTagIf(ctx, entry.Value, []string{securitydomain.TagBlocked, securitydomain.TagTrusted, securitydomain.TagSuspicious})
			} else {
				err = s.pg.SetIPRiskTag(ctx, entry.Value, tag, riskSourceManual, entry.Reason)
			}
			s.invalidateIPRiskCache(ctx, entry.Value)
		case securitydomain.ListTypeDevice:
			if removed {
				err = s.pg.ResetDeviceRiskTagIf(ctx, entry.Value, []string{securitydomain.TagBlocked, securitydomain.TagTrusted, securitydomain.TagSuspicious})
			} else {
				err = s.pg.SetDeviceRiskTagByDeviceID(ctx, entry.Value, tag)
			}
		}
		if err != nil {
			s.logWarn("同步档案标签失败", zap.String("type", entry.ListType), zap.String("value", entry.Value), zap.Error(err))
		}
	}
}

func isListKind(kind string) bool {
	return kind == securitydomain.ListKindBlock || kind == securitydomain.ListKindAllow || kind == securitydomain.ListKindWatch
}
