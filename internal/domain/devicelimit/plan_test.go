package devicelimit

import (
	"strings"
	"testing"
	"time"
)

var planBase = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func device(key, platform string, loginAgo, idleAgo time.Duration) Device {
	return Device{
		Key: key, Platform: platform, Class: ClassOf(platform),
		LoginAt: planBase.Add(-loginAgo), LastActiveAt: planBase.Add(-idleAgo),
	}
}

func enabledPolicy() Policy {
	policy := DefaultPolicy()
	policy.Enabled = true
	return policy
}

func keys(devices []Device) []string {
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		out = append(out, d.Key)
	}
	return out
}

func TestPlanDisabledAllowsEverything(t *testing.T) {
	policy := DefaultPolicy()
	decision := Plan(PlanInput{
		Policy:    policy,
		Limits:    Resolve(policy, Member{}, nil, planBase),
		Devices:   []Device{device("a", "android", time.Hour, time.Hour)},
		Candidate: Candidate{Key: "a", Class: ClassMobile},
	})
	if decision.Action != ActionAllow || len(decision.Replaced) != 0 {
		t.Fatalf("未开启时不应处置任何设备，得到 %+v", decision)
	}
}

func TestPlanKickOldestWithinTotal(t *testing.T) {
	policy := enabledPolicy()
	policy.MaxDevices = 2
	devices := []Device{
		device("old", "android", 3*time.Hour, time.Minute),
		device("new", "windows", time.Hour, 2*time.Hour),
	}
	decision := Plan(PlanInput{Policy: policy, Limits: Resolve(policy, Member{}, nil, planBase), Devices: devices,
		Candidate: Candidate{Key: "c", Class: ClassMobile}})
	if decision.Action != ActionKick || len(decision.Victims) != 1 || decision.Victims[0].Key != "old" {
		t.Fatalf("应下线最早登录的 old，得到 %+v", decision)
	}

	policy.Overflow = OverflowKickIdle
	decision = Plan(PlanInput{Policy: policy, Limits: Resolve(policy, Member{}, nil, planBase), Devices: devices,
		Candidate: Candidate{Key: "c", Class: ClassMobile}})
	if len(decision.Victims) != 1 || decision.Victims[0].Key != "new" {
		t.Fatalf("按空闲时长应下线最久没活动的 new，得到 %v", keys(decision.Victims))
	}
}

func TestPlanSameDevice(t *testing.T) {
	policy := enabledPolicy()
	policy.MaxDevices = 1
	devices := []Device{device("a", "android", time.Hour, time.Hour)}

	decision := Plan(PlanInput{Policy: policy, Limits: Resolve(policy, Member{}, nil, planBase), Devices: devices,
		Candidate: Candidate{Key: "a", Class: ClassMobile}})
	if decision.Action != ActionAllow || len(decision.Replaced) != 1 {
		t.Fatalf("同设备再登录应顶掉旧登录且不算超限，得到 %+v", decision)
	}

	policy.SameDevice = SameDeviceCoexist
	decision = Plan(PlanInput{Policy: policy, Limits: Resolve(policy, Member{}, nil, planBase), Devices: devices,
		Candidate: Candidate{Key: "a", Class: ClassMobile}})
	if decision.Action != ActionAllow || len(decision.Replaced) != 0 {
		t.Fatalf("共存模式下同设备再登录什么都不做，得到 %+v", decision)
	}
}

func TestPlanClassLimitPicksSameClassFirst(t *testing.T) {
	policy := enabledPolicy()
	policy.MaxDevices = 3
	policy.ClassLimits = map[string]int{ClassMobile: 1}
	devices := []Device{
		device("pc", "windows", 5*time.Hour, time.Hour),
		device("phone", "ios", time.Hour, time.Hour),
		device("web", "web", 4*time.Hour, time.Hour),
	}
	decision := Plan(PlanInput{Policy: policy, Limits: Resolve(policy, Member{}, nil, planBase), Devices: devices,
		Candidate: Candidate{Key: "phone2", Class: ClassMobile}})
	// 类别超 1、总数超 1：下线一台手机就同时满足两个维度，不该再踢电脑
	if decision.Action != ActionKick || len(decision.Victims) != 1 || decision.Victims[0].Key != "phone" {
		t.Fatalf("应只下线同类别的 phone，得到 %v", keys(decision.Victims))
	}
	if decision.Dimension != DimensionClass {
		t.Fatalf("触发维度应为类别，得到 %s", decision.Dimension)
	}
}

func TestPlanRejectAndAsk(t *testing.T) {
	policy := enabledPolicy()
	policy.MaxDevices = 1
	policy.Overflow = OverflowReject
	devices := []Device{device("a", "android", time.Hour, time.Hour)}
	limits := Resolve(policy, Member{}, nil, planBase)

	decision := Plan(PlanInput{Policy: policy, Limits: limits, Devices: devices, Candidate: Candidate{Key: "b", Class: ClassMobile}})
	if decision.Action != ActionReject || decision.Message == "" {
		t.Fatalf("应拒绝，得到 %+v", decision)
	}

	policy.Overflow = OverflowAsk
	policy.AskFallback = OverflowKickOldest
	decision = Plan(PlanInput{Policy: policy, Limits: limits, Devices: devices, Candidate: Candidate{Key: "b", Class: ClassMobile}, CanAsk: true})
	if decision.Action != ActionAsk || MustRemoveTotal(decision.Requirements) != 1 {
		t.Fatalf("客户端支持时应请用户选择，得到 %+v", decision)
	}
	decision = Plan(PlanInput{Policy: policy, Limits: limits, Devices: devices, Candidate: Candidate{Key: "b", Class: ClassMobile}})
	if decision.Action != ActionKick {
		t.Fatalf("客户端不支持选择时按兜底方式自动下线，得到 %+v", decision)
	}

	// 选好了：被选中的设备腾出来后放行
	decision = Plan(PlanInput{Policy: policy, Limits: limits, Devices: devices, Candidate: Candidate{Key: "b", Class: ClassMobile},
		CanAsk: true, Chosen: []string{"a"}})
	if decision.Action != ActionKick || len(decision.Victims) != 1 || decision.Victims[0].Key != "a" {
		t.Fatalf("勾选的设备应被下线并放行，得到 %+v", decision)
	}
	// 选得不够：不再追问，直接拒绝
	two := append(devices, device("c", "ios", time.Hour, time.Hour))
	decision = Plan(PlanInput{Policy: policy, Limits: limits, Devices: two, Candidate: Candidate{Key: "b", Class: ClassMobile},
		CanAsk: true, Chosen: []string{"a"}})
	if decision.Action != ActionReject {
		t.Fatalf("勾选不够时应拒绝，得到 %+v", decision)
	}
}

func TestPlanNewDeviceRate(t *testing.T) {
	policy := enabledPolicy()
	policy.MaxDevices = 0
	policy.NewDevice = NewDeviceRule{Enabled: true, WindowDays: 7, MaxNew: 2}
	limits := Resolve(policy, Member{}, nil, planBase)

	decision := Plan(PlanInput{Policy: policy, Limits: limits, Candidate: Candidate{Key: "x", Class: ClassMobile}, NewDevicesInWindow: 2})
	if decision.Action != ActionReject || decision.Dimension != DimensionNewDevice {
		t.Fatalf("窗口内新设备已满应拒绝，得到 %+v", decision)
	}
	decision = Plan(PlanInput{Policy: policy, Limits: limits, Candidate: Candidate{Key: "x", Class: ClassMobile, Known: true}, NewDevicesInWindow: 5})
	if decision.Action != ActionAllow {
		t.Fatalf("登录过的设备不受新设备限制，得到 %+v", decision)
	}
	decision = Plan(PlanInput{Policy: policy, Limits: limits, Candidate: Candidate{Key: "s:family", Class: ClassWeb}, NewDevicesInWindow: 5})
	if decision.Action != ActionAllow {
		t.Fatalf("单次登录 Key 不进台账，也不受新设备限制，得到 %+v", decision)
	}
}

func TestResolvePrecedence(t *testing.T) {
	policy := enabledPolicy()
	policy.MaxDevices = 2
	policy.ClassLimits = map[string]int{ClassWeb: 1}
	policy.Tiers = []Tier{
		{ID: "life", Audience: AudienceLifetime, MaxDevices: 10},
		{ID: "vip", Audience: AudienceVIP, MaxDevices: 5, ClassLimits: map[string]int{ClassMobile: 3}},
	}
	normalized, err := policy.Normalize()
	if err != nil {
		t.Fatal(err)
	}

	if got := Resolve(normalized, Member{}, nil, planBase); got.MaxDevices != 2 || got.Source != SourceDefault {
		t.Fatalf("非会员走默认，得到 %+v", got)
	}
	got := Resolve(normalized, Member{IsVIP: true}, nil, planBase)
	if got.MaxDevices != 5 || got.TierID != "vip" || got.ClassLimits[ClassMobile] != 3 || got.ClassLimits[ClassWeb] != 0 {
		t.Fatalf("会员档位自带类别上限时不沿用默认，得到 %+v", got)
	}
	got = Resolve(normalized, Member{IsVIP: true, IsLifetime: true}, nil, planBase)
	if got.TierID != "life" || got.ClassLimits[ClassWeb] != 1 {
		t.Fatalf("第一条命中的档位生效，且没配类别上限时沿用默认，得到 %+v", got)
	}

	expired := planBase.Add(-time.Minute)
	override := &Override{MaxDevices: 1, ExpiresAt: &expired}
	if got := Resolve(normalized, Member{IsVIP: true}, override, planBase); got.Source != SourceTier {
		t.Fatalf("过期的例外不生效，得到 %+v", got)
	}
	override.ExpiresAt = nil
	if got := Resolve(normalized, Member{IsVIP: true}, override, planBase); got.Source != SourceOverride || got.MaxDevices != 1 {
		t.Fatalf("例外优先于档位，得到 %+v", got)
	}
	override.Exempt = true
	if got := Resolve(normalized, Member{}, override, planBase); !got.Unlimited {
		t.Fatalf("豁免不受限制，得到 %+v", got)
	}
}

func TestLegacySettingsMapping(t *testing.T) {
	policy, configured := FromSettings(map[string]any{"multiDeviceLogin": false})
	if configured || !policy.Enabled || policy.MaxDevices != 1 {
		t.Fatalf("关闭多设备应推导为单设备，得到 %+v", policy)
	}
	policy, _ = FromSettings(map[string]any{"multiDeviceLogin": true, "multiDeviceLoginNum": float64(4)})
	if !policy.Enabled || policy.MaxDevices != 4 {
		t.Fatalf("旧上限应沿用，得到 %+v", policy)
	}
	policy, _ = FromSettings(map[string]any{})
	if policy.Enabled {
		t.Fatalf("没配置时不限制，得到 %+v", policy)
	}

	saved := enabledPolicy()
	saved.MaxDevices = 6
	saved.Tiers = []Tier{{Audience: AudienceVIP, MaxDevices: 8}}
	normalized, err := saved.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, configured := FromSettings(map[string]any{SettingsKey: normalized.ToSettings()})
	if !configured || roundTrip.MaxDevices != 6 || len(roundTrip.Tiers) != 1 || roundTrip.Tiers[0].Name != "会员" {
		t.Fatalf("写回再读出应一致，得到 %+v", roundTrip)
	}
}

func TestDeviceKey(t *testing.T) {
	if DeviceKey(" abc ", "ua", "f") != "abc" {
		t.Fatal("有设备标识时用设备标识")
	}
	if key := DeviceKey("", "Mozilla/5.0", "f"); KindOf(key) != KeyKindAgent || key != DeviceKey("", "Mozilla/5.0", "g") {
		t.Fatalf("同一 UA 应得到同一个 Key，得到 %s", key)
	}
	if key := DeviceKey("", "", "f"); KindOf(key) != KeyKindSession || Ledgerable(key) {
		t.Fatalf("单次登录 Key 不进台账，得到 %s", key)
	}
}

func TestTrimBringsDevicesWithinLimits(t *testing.T) {
	policy := enabledPolicy()
	policy.MaxDevices = 2
	policy.ClassLimits = map[string]int{ClassDesktop: 1}
	policy.Overflow = OverflowReject
	devices := []Device{
		device("pc1", "windows", 5*time.Hour, time.Hour),
		device("pc2", "macos", 4*time.Hour, time.Hour),
		device("phone", "android", 3*time.Hour, time.Hour),
		device("web", "web", 2*time.Hour, time.Hour),
	}
	victims := Trim(policy, Resolve(policy, Member{}, nil, planBase), devices)
	got := keys(victims)
	if len(got) != 2 || got[0] != "pc1" || got[1] != "pc2" {
		t.Fatalf("先按类别收回 pc1，再按总数收回最早的 pc2，得到 %v", got)
	}
}

func TestDeviceKeyIgnoresPlaceholderIDs(t *testing.T) {
	ua := "Mozilla/5.0 (Linux; Android 16) Voyage/1.0"
	zero := strings.Repeat("0", 64)
	if got := DeviceKey(zero, ua, "f1"); KindOf(got) != KeyKindAgent {
		t.Fatalf("all-zero id should fall back to the UA key, got %q", got)
	}
	if got := DeviceKey("9774d56d682e549c", "", "f1"); got != "s:f1" {
		t.Fatalf("placeholder without UA should be a session key, got %q", got)
	}
	if got := DeviceKey(" real-id ", ua, "f1"); got != "real-id" {
		t.Fatalf("real id should be kept, got %q", got)
	}
}
