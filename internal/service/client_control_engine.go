package service

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
)

// 版本管控的判定引擎：纯函数，不碰数据库与网络，网关闸门、上报接口与控制台模拟共用它。
// 次序见 internal/domain/app/client_control.go 的包注释。

const (
	clientControlDefaultBlockTitle   = "当前版本已停用"
	clientControlDefaultBlockMessage = "当前版本已停止服务，请安装官方最新版本后继续使用。"
	clientControlDefaultUpdateTitle  = "需要更新"
	clientControlDefaultUpdateMsg    = "当前版本已不再支持，请更新到最新版本后继续使用。"
	clientControlPackageTitle        = "非官方版本"
	clientControlPackageMessage      = "当前安装包不是官方发布的版本，已停止服务。请从官方渠道下载安装。"
	clientControlSignatureTitle      = "安装包签名异常"
	clientControlSignatureMessage    = "当前安装包的签名与官方版本不一致，可能已被修改，已停止服务。请从官方渠道下载安装。"
)

// sortClientControlRules priority 高者在前，同优先级按 id 升序（先建的先匹配）。
func sortClientControlRules(rules []appdomain.ClientControlRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].ID < rules[j].ID
	})
}

// clientReportKnown 部分判定时哪些字段是可信已知的。包名总是已知（没有包名就不做部分判定）。
//
// 网关闸门只看得到请求头（包名、版本号、版本名、证书、平台、设备），看不到安装来源、
// 系统 API 级别；版本分布按聚合维度估算时也只有包名、版本与签名。
type clientReportKnown struct {
	VersionCode, VersionName, Cert, Platform, Device, User, Installer, OS bool
}

// clientReportAllKnown 加密上报与控制台模拟：全部字段已知。
var clientReportAllKnown = clientReportKnown{true, true, true, true, true, true, true, true}

// stripUnknownConditions 去掉依赖未知字段的条件，返回剩余条件与是否去掉过。
func stripUnknownConditions(c appdomain.ClientControlConditions, known clientReportKnown) (appdomain.ClientControlConditions, bool) {
	stripped := false
	if !known.VersionCode && (c.VersionCodeMin > 0 || c.VersionCodeMax > 0) {
		c.VersionCodeMin, c.VersionCodeMax, stripped = 0, 0, true
	}
	if !known.VersionName && len(c.VersionNames) > 0 {
		c.VersionNames, stripped = nil, true
	}
	if !known.Cert && len(c.Certs) > 0 {
		c.Certs, c.CertsNegate, stripped = nil, false, true
	}
	if !known.Platform && len(c.Platforms) > 0 {
		c.Platforms, stripped = nil, true
	}
	if !known.Device && len(c.DeviceIDs) > 0 {
		c.DeviceIDs, stripped = nil, true
	}
	if !known.User && len(c.UserIDs) > 0 {
		c.UserIDs, stripped = nil, true
	}
	if !known.Installer && len(c.Installers) > 0 {
		c.Installers, c.InstallersNegate, stripped = nil, false, true
	}
	if !known.OS && (c.OSVersionMin > 0 || c.OSVersionMax > 0) {
		c.OSVersionMin, c.OSVersionMax, stripped = 0, 0, true
	}
	return c, stripped
}

// evaluateClientControl 给出一份判定。trace 为 true 时逐条记录解释（控制台模拟用）。
func evaluateClientControl(settings appdomain.ClientControlSettings, rules []appdomain.ClientControlRule,
	report appdomain.ClientReport, now time.Time, trace bool) appdomain.ClientControlDecision {
	return evaluateClientControlKnown(settings, rules, report, clientReportAllKnown, now, trace)
}

// evaluateClientControlPartial 只凭部分字段判定（网关闸门、版本分布估算）。
//
// 宁可放过不可错杀：某条规则用到了未知字段，而它的已知条件全部满足时，结果取决于看不到的那部分，
// 此时直接返回 undetermined 放行，交给客户端的加密上报给出确定的裁决；已知条件就不满足的规则照常跳过。
// 证书未知时不做官方签名检查。
func evaluateClientControlPartial(settings appdomain.ClientControlSettings, rules []appdomain.ClientControlRule,
	report appdomain.ClientReport, known clientReportKnown, now time.Time) appdomain.ClientControlDecision {
	return evaluateClientControlKnown(settings, rules, report, known, now, false)
}

func evaluateClientControlKnown(settings appdomain.ClientControlSettings, rules []appdomain.ClientControlRule,
	report appdomain.ClientReport, known clientReportKnown, now time.Time, trace bool) appdomain.ClientControlDecision {
	decision := appdomain.ClientControlDecision{Action: appdomain.ClientControlActionAllow, Reason: appdomain.ClientControlReasonDefault}
	if !settings.Enabled {
		decision.Reason = appdomain.ClientControlReasonDisabled
		if trace {
			decision.Trace = append(decision.Trace, appdomain.ClientControlTraceStep{Name: "管控开关", Detail: "版本管控未启用，一律放行"})
		}
		return decision
	}
	report = normalizeClientReport(report)
	ordered := slices.Clone(rules)
	sortClientControlRules(ordered)
	for _, rule := range ordered {
		if !rule.ActiveAt(now) {
			if trace {
				detail := "规则已停用"
				if rule.Enabled {
					detail = "不在生效时间内"
				}
				decision.Trace = append(decision.Trace, appdomain.ClientControlTraceStep{RuleID: rule.ID, Name: rule.Name, Detail: detail})
			}
			continue
		}
		conditions, stripped := stripUnknownConditions(rule.Conditions, known)
		ok, detail := matchClientControlConditions(conditions, report)
		if ok && stripped {
			decision.Reason = appdomain.ClientControlReasonUndetermined
			decision.RuleID, decision.RuleName = rule.ID, rule.Name
			return decision
		}
		if trace {
			decision.Trace = append(decision.Trace, appdomain.ClientControlTraceStep{RuleID: rule.ID, Name: rule.Name, Matched: ok, Detail: detail})
		}
		if !ok {
			continue
		}
		decision.Action = rule.Action
		decision.Reason = appdomain.ClientControlReasonRule
		decision.RuleID = rule.ID
		decision.RuleName = rule.Name
		decision.Title, decision.Message = rule.Title, rule.Message
		decision.ActionURL, decision.ActionLabel = rule.ActionURL, rule.ActionLabel
		fillClientControlCopy(&decision, settings)
		return decision
	}

	if len(settings.AllowedPackages) > 0 && !slices.Contains(settings.AllowedPackages, report.PackageName) {
		decision.Action = appdomain.ClientControlActionBlock
		decision.Reason = appdomain.ClientControlReasonPackageNotAllowed
		decision.Title, decision.Message = clientControlPackageTitle, clientControlPackageMessage
		if trace {
			decision.Trace = append(decision.Trace, appdomain.ClientControlTraceStep{Name: "官方包名", Matched: true,
				Detail: fmt.Sprintf("包名 %q 不在官方包名清单内", report.PackageName)})
		}
		return decision
	}
	if known.Cert && len(settings.AllowedCerts) > 0 && !slices.Contains(settings.AllowedCerts, report.CertSHA256) {
		decision.Action = appdomain.ClientControlActionBlock
		decision.Reason = appdomain.ClientControlReasonSignatureMismatch
		decision.Title, decision.Message = clientControlSignatureTitle, clientControlSignatureMessage
		if trace {
			cert := report.CertSHA256
			if cert == "" {
				cert = "（未上报）"
			}
			decision.Trace = append(decision.Trace, appdomain.ClientControlTraceStep{Name: "官方签名", Matched: true,
				Detail: "签名证书 " + cert + " 不在官方签名清单内"})
		}
		return decision
	}
	if trace {
		decision.Trace = append(decision.Trace, appdomain.ClientControlTraceStep{Name: "默认", Matched: true, Detail: "没有规则命中，放行"})
	}
	return decision
}

// fillClientControlCopy 规则没写标题与说明时按结果补上默认文案。
func fillClientControlCopy(decision *appdomain.ClientControlDecision, settings appdomain.ClientControlSettings) {
	switch decision.Action {
	case appdomain.ClientControlActionBlock:
		if decision.Title == "" {
			decision.Title = firstNonEmpty(settings.BlockTitle, clientControlDefaultBlockTitle)
		}
		if decision.Message == "" {
			decision.Message = firstNonEmpty(settings.BlockMessage, clientControlDefaultBlockMessage)
		}
	case appdomain.ClientControlActionForceUpdate:
		if decision.Title == "" {
			decision.Title = clientControlDefaultUpdateTitle
		}
		if decision.Message == "" {
			decision.Message = clientControlDefaultUpdateMsg
		}
	}
}

// matchClientControlConditions 条件之间为「且」。返回是否命中与一句解释（第一个不满足的条件）。
func matchClientControlConditions(c appdomain.ClientControlConditions, r appdomain.ClientReport) (bool, string) {
	if c.IsEmpty() {
		return true, "无条件，命中所有客户端"
	}
	if len(c.Packages) > 0 {
		in := slices.Contains(c.Packages, r.PackageName)
		if in == c.PackagesNegate {
			return false, "包名 " + quoteOrEmpty(r.PackageName) + negateText(c.PackagesNegate, "在排除清单内", "不在清单内")
		}
	}
	if c.VersionCodeMin > 0 && r.VersionCode < c.VersionCodeMin {
		return false, fmt.Sprintf("版本号 %d 低于 %d", r.VersionCode, c.VersionCodeMin)
	}
	if c.VersionCodeMax > 0 && r.VersionCode > c.VersionCodeMax {
		return false, fmt.Sprintf("版本号 %d 高于 %d", r.VersionCode, c.VersionCodeMax)
	}
	if len(c.VersionNames) > 0 && !slices.Contains(c.VersionNames, r.VersionName) {
		return false, "版本名 " + quoteOrEmpty(r.VersionName) + " 不在清单内"
	}
	if len(c.Certs) > 0 {
		in := slices.Contains(c.Certs, r.CertSHA256)
		if in == c.CertsNegate {
			return false, "签名证书" + negateText(c.CertsNegate, "在排除清单内", "不在清单内")
		}
	}
	if len(c.Platforms) > 0 && !slices.Contains(c.Platforms, r.Platform) {
		return false, "平台 " + quoteOrEmpty(r.Platform) + " 不在清单内"
	}
	if len(c.DeviceIDs) > 0 && !slices.Contains(c.DeviceIDs, r.DeviceID) {
		return false, "设备不在清单内"
	}
	if len(c.UserIDs) > 0 && (r.UserID <= 0 || !slices.Contains(c.UserIDs, r.UserID)) {
		return false, "用户不在清单内（未登录不命中用户条件）"
	}
	if len(c.Installers) > 0 {
		in := slices.Contains(c.Installers, r.Installer)
		if in == c.InstallersNegate {
			return false, "安装来源 " + quoteOrEmpty(r.Installer) + negateText(c.InstallersNegate, "在排除清单内", "不在清单内")
		}
	}
	if c.OSVersionMin > 0 && r.OSVersion < c.OSVersionMin {
		return false, fmt.Sprintf("系统版本 %d 低于 %d", r.OSVersion, c.OSVersionMin)
	}
	if c.OSVersionMax > 0 && r.OSVersion > c.OSVersionMax {
		return false, fmt.Sprintf("系统版本 %d 高于 %d", r.OSVersion, c.OSVersionMax)
	}
	return true, "全部条件满足"
}

func negateText(negate bool, whenNegate, otherwise string) string {
	if negate {
		return " " + whenNegate
	}
	return " " + otherwise
}

func quoteOrEmpty(value string) string {
	if value == "" {
		return "（空）"
	}
	return fmt.Sprintf("%q", value)
}

// normalizeClientReport 与规则落库时同一套归一化，避免大小写、冒号写法不同造成漏判。
func normalizeClientReport(r appdomain.ClientReport) appdomain.ClientReport {
	r.PackageName = strings.TrimSpace(r.PackageName)
	r.VersionName = strings.TrimSpace(r.VersionName)
	r.Platform = strings.ToLower(strings.TrimSpace(r.Platform))
	r.CertSHA256 = normalizeCertDigest(r.CertSHA256)
	r.Installer = strings.TrimSpace(r.Installer)
	r.DeviceID = strings.TrimSpace(r.DeviceID)
	return r
}

// normalizeCertDigest 接受 `AB:CD:...`、大写、带空格的写法，统一成 64 位小写十六进制。
func normalizeCertDigest(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(":", "", " ", "", "-", "").Replace(value)
	return value
}
