package service

import (
	"fmt"
	"strings"

	securitydomain "aegis/internal/domain/security"
)

// ════════════════════════════════════════════════════════════
//  条件判定
//
//  每一种条件类型在这里都必须有分支，且返回**人类可读的判据**。
//  只回 true/false 的引擎在事后复盘时提供不了任何信息 ——
//  「命中了 IP 高频」和「命中了 IP 高频：当前 312 次 > 阈值 100」
//  是两种完全不同的可运维性。
// ════════════════════════════════════════════════════════════

func (s *RiskService) evaluateCondition(rule securitydomain.RiskRule, env *RiskEvalEnv) (bool, string, error) {
	data := rule.ConditionData
	switch rule.ConditionType {

	case securitydomain.CondIPFrequency:
		threshold := int64(numberOf(data["threshold"], 100))
		count := requestCountFor(env, stringOf(data["dimension"], "ip"))
		if count > threshold {
			return true, fmt.Sprintf("窗口内请求 %d 次 > 阈值 %d", count, threshold), nil
		}
		return false, fmt.Sprintf("窗口内请求 %d 次 ≤ 阈值 %d", count, threshold), nil

	case securitydomain.CondRateLimited:
		dimension := stringOf(data["dimension"], "ip")
		if rateLimitedFor(env, dimension) {
			return true, fmt.Sprintf("%s 维度已触发平台限流阈值", dimension), nil
		}
		return false, fmt.Sprintf("%s 维度未触发限流", dimension), nil

	case securitydomain.CondAccountVelocity:
		threshold := int64(numberOf(data["threshold"], 5))
		dimension := stringOf(data["dimension"], "ip")
		seen := env.IPAccountsSeen
		if dimension == "device" {
			seen = env.DeviceAccountsSeen
		}
		if seen >= threshold {
			return true, fmt.Sprintf("%s 维度窗口内触达 %d 个不同账号 ≥ 阈值 %d", dimension, seen, threshold), nil
		}
		return false, fmt.Sprintf("%s 维度窗口内触达 %d 个不同账号 < 阈值 %d", dimension, seen, threshold), nil

	case securitydomain.CondDeviceNew:
		maxHours := numberOf(data["max_hours"], 1)
		if !env.DeviceKnown {
			// 设备完全没在档：这是最"新"的情况，但只有真的带了设备标识才算数。
			// 客户端没上报 device_id 时不该被判成新设备 —— 那是缺数据，不是风险信号。
			if env.DeviceID == "" {
				return false, "请求未携带设备标识，不参与设备判定", nil
			}
			return true, "设备首次出现", nil
		}
		if env.DeviceAgeHours < maxHours {
			return true, fmt.Sprintf("设备存续 %.1f 小时 < 阈值 %.0f 小时", env.DeviceAgeHours, maxHours), nil
		}
		return false, fmt.Sprintf("设备存续 %.1f 小时 ≥ 阈值 %.0f 小时", env.DeviceAgeHours, maxHours), nil

	case securitydomain.CondDeviceShared:
		threshold := int64(numberOf(data["threshold"], 3))
		if env.DeviceID == "" {
			return false, "请求未携带设备标识，不参与设备判定", nil
		}
		if env.DeviceAccountsSeen >= threshold {
			return true, fmt.Sprintf("该设备窗口内出现 %d 个不同账号 ≥ 阈值 %d", env.DeviceAccountsSeen, threshold), nil
		}
		return false, fmt.Sprintf("该设备窗口内出现 %d 个不同账号 < 阈值 %d", env.DeviceAccountsSeen, threshold), nil

	case securitydomain.CondUABot:
		if env.UAIsBot {
			return true, fmt.Sprintf("User-Agent 被识别为 %s", firstNonEmptyString(env.UABrowser, "机器人")), nil
		}
		return false, "User-Agent 未被识别为机器人", nil

	case securitydomain.CondUAMissing:
		minLength := int(numberOf(data["min_length"], 16))
		if env.UALength == 0 {
			return true, "请求未携带 User-Agent", nil
		}
		if env.UALength < minLength {
			return true, fmt.Sprintf("User-Agent 长度 %d < 阈值 %d", env.UALength, minLength), nil
		}
		return false, fmt.Sprintf("User-Agent 长度 %d ≥ 阈值 %d", env.UALength, minLength), nil

	case securitydomain.CondUADeviceClass:
		classes := toStringSlice(data["classes"])
		if len(classes) == 0 {
			return false, "", fmt.Errorf("未配置客户端类型")
		}
		for _, class := range classes {
			if strings.EqualFold(strings.TrimSpace(class), env.UADeviceClass) {
				return true, fmt.Sprintf("客户端类型为 %s，在名单内", env.UADeviceClass), nil
			}
		}
		return false, fmt.Sprintf("客户端类型为 %s，不在名单 [%s] 内", env.UADeviceClass, strings.Join(classes, ", ")), nil

	case securitydomain.CondIPProxy:
		includeTor := boolOf(data["include_tor"], true)
		includeDatacenter := boolOf(data["include_datacenter"], false)
		// 人工加白的 IP 直接放过：管理员的结论优先于情报源的判断。
		if env.IPTrusted {
			return false, "该 IP 已人工加白", nil
		}
		hits := make([]string, 0, 4)
		if env.IPIsProxy {
			hits = append(hits, "代理")
		}
		if env.IPIsVPN {
			hits = append(hits, "VPN")
		}
		if includeTor && env.IPIsTor {
			hits = append(hits, "Tor")
		}
		if includeDatacenter && env.IPIsDatacenter {
			hits = append(hits, "机房")
		}
		if len(hits) > 0 {
			return true, "IP 判定为 " + strings.Join(hits, " / "), nil
		}
		if !env.IPKnown {
			return false, "IP 情报缺失，未做判定", nil
		}
		return false, "IP 非代理出口", nil

	case securitydomain.CondIPReputation:
		minScore := int(numberOf(data["min_score"], 75))
		if env.IPTrusted {
			return false, "该 IP 已人工加白", nil
		}
		if !env.IPKnown {
			return false, "IP 情报缺失，未做判定", nil
		}
		if env.IPRiskScore >= minScore {
			return true, fmt.Sprintf("IP 信誉分 %d ≥ 阈值 %d", env.IPRiskScore, minScore), nil
		}
		return false, fmt.Sprintf("IP 信誉分 %d < 阈值 %d", env.IPRiskScore, minScore), nil

	case securitydomain.CondIPCIDR:
		cidrs := toStringSlice(data["cidrs"])
		if len(cidrs) == 0 {
			return false, "", fmt.Errorf("未配置网段列表")
		}
		inside := ipInAnyCIDR(env.IP, cidrs)
		if boolOf(data["negate"], false) {
			if !inside {
				return true, fmt.Sprintf("IP %s 不在任何配置网段内", env.IP), nil
			}
			return false, fmt.Sprintf("IP %s 在配置网段内", env.IP), nil
		}
		if inside {
			return true, fmt.Sprintf("IP %s 落在配置网段内", env.IP), nil
		}
		return false, fmt.Sprintf("IP %s 不在配置网段内", env.IP), nil

	case securitydomain.CondGeoAnomaly:
		expected := strings.ToUpper(strings.TrimSpace(stringOf(data["expected_country"], "")))
		if expected == "" {
			return false, "", fmt.Errorf("未配置预期国家代码")
		}
		if !env.GeoKnown || env.GeoCountry == "" {
			// 情报缺失时判「不命中」。反过来会把每一个查不到归属地的请求
			// 都算成异常 —— 那不是风控，那是拒绝服务。
			return false, "归属地未知，未做判定", nil
		}
		if !strings.EqualFold(env.GeoCountry, expected) {
			return true, fmt.Sprintf("归属地 %s ≠ 预期 %s", env.GeoCountry, expected), nil
		}
		return false, fmt.Sprintf("归属地 %s 与预期一致", env.GeoCountry), nil

	case securitydomain.CondGeoCountryIn, securitydomain.CondGeoCountryNotIn:
		countries := toStringSlice(data["countries"])
		if len(countries) == 0 {
			return false, "", fmt.Errorf("未配置国家代码列表")
		}
		if !env.GeoKnown || env.GeoCountry == "" {
			return false, "归属地未知，未做判定", nil
		}
		inList := false
		for _, country := range countries {
			if strings.EqualFold(strings.TrimSpace(country), env.GeoCountry) {
				inList = true
				break
			}
		}
		if rule.ConditionType == securitydomain.CondGeoCountryIn {
			if inList {
				return true, fmt.Sprintf("归属地 %s 在名单内", env.GeoCountry), nil
			}
			return false, fmt.Sprintf("归属地 %s 不在名单内", env.GeoCountry), nil
		}
		if !inList {
			return true, fmt.Sprintf("归属地 %s 不在允许名单内", env.GeoCountry), nil
		}
		return false, fmt.Sprintf("归属地 %s 在允许名单内", env.GeoCountry), nil

	case securitydomain.CondASNIn:
		keywords := toStringSlice(data["keywords"])
		if len(keywords) == 0 {
			return false, "", fmt.Errorf("未配置 ASN / ISP 关键词")
		}
		target := strings.TrimSpace(env.IPASN + " " + env.IPISP)
		if target == "" {
			return false, "ASN / ISP 情报缺失，未做判定", nil
		}
		if containsAnyFold(target, keywords) {
			return true, fmt.Sprintf("归属 %s 命中关键词", target), nil
		}
		return false, fmt.Sprintf("归属 %s 未命中关键词", target), nil

	case securitydomain.CondTimeWindow:
		start := stringOf(data["start"], "")
		end := stringOf(data["end"], "")
		hit, err := inTimeWindow(env.evaluatedAt, start, end)
		if err != nil {
			return false, "", err
		}
		if hit {
			return true, fmt.Sprintf("请求发生在 %s–%s 时段内", start, end), nil
		}
		return false, fmt.Sprintf("请求不在 %s–%s 时段内", start, end), nil

	case securitydomain.CondCustomExpr:
		expression := strings.TrimSpace(stringOf(data["expression"], ""))
		if expression == "" {
			return false, "", fmt.Errorf("未配置表达式")
		}
		hit, err := runRiskExpr(expression, env)
		if err != nil {
			return false, "", err
		}
		if hit {
			return true, "表达式判定为真：" + truncateText(expression, 120), nil
		}
		return false, "表达式判定为假", nil

	case securitydomain.CondAccountNew:
		maxHours := numberOf(data["max_hours"], 24)
		if !env.UserKnown {
			return false, "未认出用户，未做判定", nil
		}
		if env.UserAgeHours < maxHours {
			return true, fmt.Sprintf("账号注册 %.1f 小时 < %g 小时", env.UserAgeHours, maxHours), nil
		}
		return false, fmt.Sprintf("账号注册 %.1f 小时", env.UserAgeHours), nil

	case securitydomain.CondAccountIPSpread:
		threshold := int64(numberOf(data["threshold"], 5))
		if env.AccountIPsSeen >= threshold {
			return true, fmt.Sprintf("账号窗口内用过 %d 个 IP ≥ 阈值 %d", env.AccountIPsSeen, threshold), nil
		}
		return false, fmt.Sprintf("账号窗口内用过 %d 个 IP", env.AccountIPsSeen), nil

	case securitydomain.CondAccountRiskTrail:
		threshold := int64(numberOf(data["threshold"], 3))
		if env.AccountRiskTrail >= threshold {
			return true, fmt.Sprintf("近 7 天高危 %d 次 ≥ 阈值 %d", env.AccountRiskTrail, threshold), nil
		}
		return false, fmt.Sprintf("近 7 天高危 %d 次", env.AccountRiskTrail), nil

	case securitydomain.CondGeoJump:
		if env.AccountLastCountry == "" || !env.GeoKnown {
			return false, "缺少上一次或本次归属地，未做判定", nil
		}
		if env.GeoChanged {
			return true, fmt.Sprintf("归属地 %s → %s", env.AccountLastCountry, env.GeoCountry), nil
		}
		return false, fmt.Sprintf("归属地仍为 %s", env.GeoCountry), nil

	case securitydomain.CondDeviceIPSpread:
		threshold := int64(numberOf(data["threshold"], 8))
		if env.DeviceID == "" {
			return false, "请求未携带设备标识", nil
		}
		if env.DeviceIPsSeen >= threshold {
			return true, fmt.Sprintf("设备窗口内出现 %d 个 IP ≥ 阈值 %d", env.DeviceIPsSeen, threshold), nil
		}
		return false, fmt.Sprintf("设备窗口内出现 %d 个 IP", env.DeviceIPsSeen), nil

	case securitydomain.CondSceneVelocity:
		threshold := int64(numberOf(data["threshold"], 60))
		dimension := stringOf(data["dimension"], "subject")
		count, label := sceneHourlyFor(env, dimension)
		if count > threshold {
			return true, fmt.Sprintf("%s一小时内 %d 次 > 阈值 %d", label, count, threshold), nil
		}
		return false, fmt.Sprintf("%s一小时内 %d 次", label, count), nil

	case securitydomain.CondListWatch:
		types := toStringSlice(data["list_types"])
		if !env.ListWatch {
			return false, "未命中观察名单", nil
		}
		if len(types) == 0 {
			return true, "命中观察名单：" + strings.Join(env.ListWatchTypes, "、"), nil
		}
		for _, t := range env.ListWatchTypes {
			for _, want := range types {
				if strings.EqualFold(strings.TrimSpace(want), t) {
					return true, "命中观察名单：" + t, nil
				}
			}
		}
		return false, "命中的观察名单类型不在限定范围内", nil

	default:
		return false, "", fmt.Errorf("未知规则条件类型：%s", rule.ConditionType)
	}
}

func sceneHourlyFor(env *RiskEvalEnv, dimension string) (int64, string) {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case "device":
		return env.SceneDeviceHourly, "同设备"
	case "ip":
		return env.SceneIPHourly, "同 IP "
	default:
		return env.SceneSubjectHourly, "同账号"
	}
}

func requestCountFor(env *RiskEvalEnv, dimension string) int64 {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case "account":
		return env.AccountRequestCount
	case "device":
		return env.DeviceRequestCount
	case "account_device":
		return env.AccountDeviceRequestCount
	default:
		return env.IPRequestCount
	}
}

func rateLimitedFor(env *RiskEvalEnv, dimension string) bool {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case "account":
		return env.AccountRateLimited
	case "device":
		return env.DeviceRateLimited
	case "account_device":
		return env.AccountDeviceRateLimited
	default:
		return env.IPRateLimited
	}
}

// explainRule 把一条规则翻译成一句中文。
// 控制台上并排列着十几条规则时，`conditionData` 的 JSON 是看不动的。
func explainRule(rule securitydomain.RiskRule) string {
	data := rule.ConditionData
	switch rule.ConditionType {
	case securitydomain.CondIPFrequency:
		return fmt.Sprintf("当 %s 维度窗口内请求数超过 %g 次时 +%d 分", stringOf(data["dimension"], "ip"), numberOf(data["threshold"], 100), rule.Score)
	case securitydomain.CondRateLimited:
		return fmt.Sprintf("当 %s 维度触发平台限流时 +%d 分", stringOf(data["dimension"], "ip"), rule.Score)
	case securitydomain.CondAccountVelocity:
		return fmt.Sprintf("当同一 %s 触达的不同账号数达到 %g 个时 +%d 分", stringOf(data["dimension"], "ip"), numberOf(data["threshold"], 5), rule.Score)
	case securitydomain.CondDeviceNew:
		return fmt.Sprintf("当设备首次出现不足 %g 小时时 +%d 分", numberOf(data["max_hours"], 1), rule.Score)
	case securitydomain.CondDeviceShared:
		return fmt.Sprintf("当同一设备出现 %g 个以上不同账号时 +%d 分", numberOf(data["threshold"], 3), rule.Score)
	case securitydomain.CondUABot:
		return fmt.Sprintf("当客户端被识别为机器人时 +%d 分", rule.Score)
	case securitydomain.CondUAMissing:
		return fmt.Sprintf("当 User-Agent 缺失或短于 %g 字符时 +%d 分", numberOf(data["min_length"], 16), rule.Score)
	case securitydomain.CondUADeviceClass:
		return fmt.Sprintf("当客户端类型属于 [%s] 时 +%d 分", strings.Join(toStringSlice(data["classes"]), ", "), rule.Score)
	case securitydomain.CondIPProxy:
		return fmt.Sprintf("当 IP 为代理 / VPN 出口时 +%d 分", rule.Score)
	case securitydomain.CondIPReputation:
		return fmt.Sprintf("当 IP 信誉分达到 %g 时 +%d 分", numberOf(data["min_score"], 75), rule.Score)
	case securitydomain.CondIPCIDR:
		verb := "落在"
		if boolOf(data["negate"], false) {
			verb = "不落在"
		}
		return fmt.Sprintf("当 IP %s [%s] 时 +%d 分", verb, strings.Join(toStringSlice(data["cidrs"]), ", "), rule.Score)
	case securitydomain.CondGeoAnomaly:
		return fmt.Sprintf("当归属地不是 %s 时 +%d 分", stringOf(data["expected_country"], "?"), rule.Score)
	case securitydomain.CondGeoCountryIn:
		return fmt.Sprintf("当归属地属于 [%s] 时 +%d 分", strings.Join(toStringSlice(data["countries"]), ", "), rule.Score)
	case securitydomain.CondGeoCountryNotIn:
		return fmt.Sprintf("当归属地不属于 [%s] 时 +%d 分", strings.Join(toStringSlice(data["countries"]), ", "), rule.Score)
	case securitydomain.CondASNIn:
		return fmt.Sprintf("当 ASN / ISP 命中 [%s] 时 +%d 分", strings.Join(toStringSlice(data["keywords"]), ", "), rule.Score)
	case securitydomain.CondTimeWindow:
		return fmt.Sprintf("当请求发生在 %s–%s 时段内时 +%d 分", stringOf(data["start"], "?"), stringOf(data["end"], "?"), rule.Score)
	case securitydomain.CondCustomExpr:
		return fmt.Sprintf("当表达式 `%s` 为真时 +%d 分", truncateText(stringOf(data["expression"], ""), 80), rule.Score)
	case securitydomain.CondAccountNew:
		return fmt.Sprintf("当账号注册不足 %g 小时时 +%d 分", numberOf(data["max_hours"], 24), rule.Score)
	case securitydomain.CondAccountIPSpread:
		return fmt.Sprintf("当同一账号窗口内用过 %g 个以上 IP 时 +%d 分", numberOf(data["threshold"], 5), rule.Score)
	case securitydomain.CondAccountRiskTrail:
		return fmt.Sprintf("当账号近 7 天高危达到 %g 次时 +%d 分", numberOf(data["threshold"], 3), rule.Score)
	case securitydomain.CondGeoJump:
		return fmt.Sprintf("当账号归属地与上一次不同时 +%d 分", rule.Score)
	case securitydomain.CondDeviceIPSpread:
		return fmt.Sprintf("当同一设备窗口内出现 %g 个以上 IP 时 +%d 分", numberOf(data["threshold"], 8), rule.Score)
	case securitydomain.CondSceneVelocity:
		_, label := sceneHourlyFor(&RiskEvalEnv{}, stringOf(data["dimension"], "subject"))
		return fmt.Sprintf("当%s一小时内本场景超过 %g 次时 +%d 分", label, numberOf(data["threshold"], 60), rule.Score)
	case securitydomain.CondListWatch:
		types := toStringSlice(data["list_types"])
		if len(types) == 0 {
			return fmt.Sprintf("当命中任一观察名单时 +%d 分", rule.Score)
		}
		return fmt.Sprintf("当命中 [%s] 观察名单时 +%d 分", strings.Join(types, ", "), rule.Score)
	default:
		return ""
	}
}
