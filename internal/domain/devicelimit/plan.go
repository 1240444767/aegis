package devicelimit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	devicedomain "aegis/internal/domain/device"
)

// Device 一台已登录的设备：同一个 Key 下的全部登录（刷新令牌族）合成一台。
type Device struct {
	Key      string `json:"key"`
	DeviceID string `json:"deviceId,omitempty"`
	Platform string `json:"platform,omitempty"`
	Class    string `json:"class"`
	// Name 登录时保存的设备名；展示用 Info
	Name      string              `json:"name,omitempty"`
	Model     string              `json:"-"`
	Extra     *devicedomain.Extra `json:"-"`
	Info      *devicedomain.Info  `json:"deviceInfo,omitempty"`
	IP        string              `json:"ip,omitempty"`
	UserAgent string              `json:"userAgent,omitempty"`
	Provider  string              `json:"provider,omitempty"`
	// Families 这台设备上的登录（刷新令牌族），下线时逐个撤销
	Families []string `json:"-"`
	Sessions int      `json:"sessions"`
	// LoginAt 这台设备上最早一次仍有效的登录；LastActiveAt 最近一次签发或刷新
	LoginAt      time.Time `json:"loginAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
	// Current 是不是发起这次请求的设备（用户查看自己的设备时标出）
	Current bool `json:"current"`
}

// KeyKind 设备 Key 的来源。
const (
	KeyKindDevice  = "device"  // 客户端上报的设备标识
	KeyKindAgent   = "agent"   // UA 摘要
	KeyKindSession = "session" // 单次登录
)

const (
	agentKeyPrefix   = "ua:"
	sessionKeyPrefix = "s:"
)

// DeviceKey 设备 Key：有设备标识用设备标识；没有时用 UA 摘要；都没有就按这一次登录算一台。
// 全零等占位标识不算设备标识（devicedomain.NormalizeID），否则这些设备会合成一台互相挤下线；
// 修复前已存下占位值的会话也在这里一并退回 UA 摘要。
func DeviceKey(deviceID, userAgent, familyID string) string {
	if id := devicedomain.NormalizeID(deviceID); id != "" {
		return id
	}
	if ua := strings.TrimSpace(userAgent); ua != "" {
		sum := sha256.Sum256([]byte(ua))
		return agentKeyPrefix + hex.EncodeToString(sum[:8])
	}
	return sessionKeyPrefix + familyID
}

// KindOf 设备 Key 的来源。
func KindOf(key string) string {
	switch {
	case strings.HasPrefix(key, agentKeyPrefix):
		return KeyKindAgent
	case strings.HasPrefix(key, sessionKeyPrefix):
		return KeyKindSession
	}
	return KeyKindDevice
}

// Ledgerable 这个 Key 能不能进设备台账：单次登录的 Key 每次都不一样，记下来没有意义，
// 还会让「新设备频率」把每次网页登录都当成新设备。
func Ledgerable(key string) bool {
	return key != "" && KindOf(key) != KeyKindSession
}

// Candidate 正在登录的这台设备。
type Candidate struct {
	Key      string
	Class    string
	Platform string
	// Known 设备台账里有它（以前登录过），新设备频率限制不算它
	Known bool
}

// 超限的维度。
const (
	DimensionTotal     = "total"
	DimensionClass     = "class"
	DimensionNewDevice = "new_device"
)

// Requirement 某个维度上超了多少。
type Requirement struct {
	Dimension string `json:"dimension"`
	Class     string `json:"class,omitempty"`
	Limit     int    `json:"limit"`
	// Current 算上这次登录后的设备数
	Current    int `json:"current"`
	MustRemove int `json:"mustRemove"`
}

// 判定结果。
const (
	ActionAllow  = "allow"
	ActionKick   = "kick"
	ActionReject = "reject"
	ActionAsk    = "ask"
)

// Decision 一次登录的处置方案。
type Decision struct {
	Action string `json:"action"`
	// Dimension 触发的维度（放行时为空）；多个维度同时超限时取类别优先
	Dimension    string        `json:"dimension,omitempty"`
	Requirements []Requirement `json:"requirements"`
	// Replaced 同一台设备上被新登录顶掉的旧登录（sameDevice=replace）
	Replaced []Device `json:"replaced"`
	// Victims 为腾位置要下线的设备：自动选出的，或用户在选择页勾的
	Victims []Device `json:"victims"`
	// Message 拒绝或请用户选择时的提示
	Message string `json:"message,omitempty"`
}

// PlanInput 判定一次登录所需的全部事实。
type PlanInput struct {
	Policy    Policy
	Limits    Limits
	Devices   []Device
	Candidate Candidate
	// NewDevicesInWindow 窗口内首次出现的设备数（不含这一台）
	NewDevicesInWindow int
	// CanAsk 客户端声明了能让用户选择下线哪台
	CanAsk bool
	// Chosen 用户在选择页勾选要下线的设备 Key（只在完成选择时有）
	Chosen []string
}

// Plan 判定一次新登录怎么处置。纯函数：读写都在调用方。
//
// 顺序：同设备去重 → 用户勾选的先腾出来 → 新设备频率 → 类别上限 → 总上限。
// 自动下线时先满足类别（在同类别里挑），再满足总数（在剩下的全部设备里挑），
// 这样一次下线同时满足两个维度，不会为了凑总数把别的类别的设备白白踢掉。
func Plan(in PlanInput) Decision {
	decision := Decision{Action: ActionAllow, Requirements: []Requirement{}, Replaced: []Device{}, Victims: []Device{}}
	if !in.Policy.Enabled {
		return decision
	}
	cand := in.Candidate
	existing := make([]Device, 0, len(in.Devices))
	for _, device := range in.Devices {
		if device.Key == cand.Key {
			if in.Policy.SameDevice == SameDeviceCoexist {
				// 这台设备已经在线：这次登录不增加设备数，什么都不用做
				return decision
			}
			decision.Replaced = append(decision.Replaced, device)
			continue
		}
		existing = append(existing, device)
	}
	if len(in.Chosen) > 0 {
		kept := existing[:0:0]
		for _, device := range existing {
			if slices.Contains(in.Chosen, device.Key) {
				decision.Victims = append(decision.Victims, device)
				continue
			}
			kept = append(kept, device)
		}
		existing = kept
	}
	if in.Limits.Unlimited {
		return finishAllow(decision)
	}
	if rule := in.Limits.NewDevice; rule.Enabled && rule.MaxNew > 0 && !cand.Known && Ledgerable(cand.Key) &&
		in.NewDevicesInWindow >= rule.MaxNew {
		decision.Action = ActionReject
		decision.Dimension = DimensionNewDevice
		decision.Requirements = append(decision.Requirements, Requirement{
			Dimension: DimensionNewDevice, Limit: rule.MaxNew, Current: in.NewDevicesInWindow + 1,
		})
		decision.Message = fmt.Sprintf("近 %d 天在新设备上登录的次数已达上限（%d 台），请在常用设备上登录或稍后再试",
			rule.WindowDays, rule.MaxNew)
		decision.Victims = []Device{}
		return decision
	}

	requirements := requirementsFor(in.Limits, existing, cand.Class)
	if len(requirements) == 0 {
		return finishAllow(decision)
	}
	decision.Requirements = requirements
	decision.Dimension = requirements[0].Dimension
	mode := in.Policy.Overflow
	if mode == OverflowAsk && (!in.CanAsk || len(in.Chosen) > 0) {
		// 已经在选择页选过一轮还不够：不再无限地问下去，按兜底方式处理
		if len(in.Chosen) > 0 {
			mode = OverflowReject
		} else {
			mode = in.Policy.AskFallback
		}
	}
	switch mode {
	case OverflowReject:
		decision.Action = ActionReject
		decision.Message = rejectMessage(in.Policy, requirements)
		decision.Victims = []Device{}
		return decision
	case OverflowAsk:
		decision.Action = ActionAsk
		decision.Message = askMessage(requirements)
		decision.Victims = []Device{}
		return decision
	}

	ordered := slices.Clone(existing)
	sortForEviction(ordered, mode)
	picked := map[string]bool{}
	for _, req := range requirements {
		if req.Dimension != DimensionClass {
			continue
		}
		need := req.MustRemove
		for _, device := range ordered {
			if need == 0 {
				break
			}
			if device.Class == req.Class && !picked[device.Key] {
				picked[device.Key] = true
				decision.Victims = append(decision.Victims, device)
				need--
			}
		}
	}
	for _, req := range requirements {
		if req.Dimension != DimensionTotal {
			continue
		}
		need := req.MustRemove - len(picked)
		for _, device := range ordered {
			if need <= 0 {
				break
			}
			if !picked[device.Key] {
				picked[device.Key] = true
				decision.Victims = append(decision.Victims, device)
				need--
			}
		}
	}
	decision.Action = ActionKick
	return decision
}

func finishAllow(decision Decision) Decision {
	if len(decision.Victims) > 0 {
		decision.Action = ActionKick
	}
	return decision
}

// requirementsFor 算上这次登录后各维度超了多少。类别在前：自动下线先满足类别。
func requirementsFor(limits Limits, existing []Device, class string) []Requirement {
	out := []Requirement{}
	if limit := limits.ClassLimits[class]; limit > 0 {
		count := 0
		for _, device := range existing {
			if device.Class == class {
				count++
			}
		}
		if count+1 > limit {
			out = append(out, Requirement{Dimension: DimensionClass, Class: class, Limit: limit, Current: count + 1, MustRemove: count + 1 - limit})
		}
	}
	if limits.MaxDevices > 0 && len(existing)+1 > limits.MaxDevices {
		out = append(out, Requirement{
			Dimension: DimensionTotal, Limit: limits.MaxDevices, Current: len(existing) + 1,
			MustRemove: len(existing) + 1 - limits.MaxDevices,
		})
	}
	return out
}

// MustRemoveTotal 至少要下线几台才能同时满足全部维度。
func MustRemoveTotal(requirements []Requirement) int {
	most := 0
	for _, req := range requirements {
		most = max(most, req.MustRemove)
	}
	return most
}

func sortForEviction(devices []Device, mode string) {
	sort.SliceStable(devices, func(i, j int) bool {
		if mode == OverflowKickIdle {
			return devices[i].LastActiveAt.Before(devices[j].LastActiveAt)
		}
		return devices[i].LoginAt.Before(devices[j].LoginAt)
	})
}

func limitPhrase(req Requirement) string {
	if req.Dimension == DimensionClass {
		return fmt.Sprintf("同时登录的%s已达上限（%d 台）", ClassLabel(req.Class), req.Limit)
	}
	return fmt.Sprintf("同时登录的设备已达上限（%d 台）", req.Limit)
}

func rejectMessage(policy Policy, requirements []Requirement) string {
	if policy.RejectMessage != "" {
		return strings.ReplaceAll(policy.RejectMessage, "{limit}", fmt.Sprint(requirements[0].Limit))
	}
	return limitPhrase(requirements[0]) + "，请先在已登录的设备上退出"
}

func askMessage(requirements []Requirement) string {
	return fmt.Sprintf("%s，请选择至少 %d 台设备下线", limitPhrase(requirements[0]), MustRemoveTotal(requirements))
}

// Usage 当前已登录设备在各维度上的占用。
type Usage struct {
	Total   int            `json:"total"`
	ByClass map[string]int `json:"byClass"`
}

// UsageOf 统计占用。
func UsageOf(devices []Device) Usage {
	usage := Usage{Total: len(devices), ByClass: map[string]int{}}
	for _, class := range Classes {
		usage.ByClass[class] = 0
	}
	for _, device := range devices {
		usage.ByClass[device.Class]++
	}
	return usage
}

// Trim 不登录新设备，只把已登录的设备收回到上限之内：管理员「按当前策略整理」、调低上限后立即生效时用。
// 下线顺序与自动下线相同；策略本身是拒绝或让用户选择时，按最早登录的先下线。
func Trim(policy Policy, limits Limits, devices []Device) []Device {
	if !policy.Enabled || limits.Unlimited {
		return []Device{}
	}
	mode := policy.Overflow
	if mode == OverflowReject || mode == OverflowAsk {
		mode = policy.AskFallback
	}
	if mode != OverflowKickIdle {
		mode = OverflowKickOldest
	}
	ordered := slices.Clone(devices)
	sortForEviction(ordered, mode)
	picked := map[string]bool{}
	victims := []Device{}
	for _, class := range Classes {
		limit := limits.ClassLimits[class]
		if limit <= 0 {
			continue
		}
		count := 0
		for _, device := range ordered {
			if device.Class == class {
				count++
			}
		}
		need := count - limit
		for _, device := range ordered {
			if need <= 0 {
				break
			}
			if device.Class == class && !picked[device.Key] {
				picked[device.Key] = true
				victims = append(victims, device)
				need--
			}
		}
	}
	if limits.MaxDevices > 0 {
		need := len(ordered) - len(picked) - limits.MaxDevices
		for _, device := range ordered {
			if need <= 0 {
				break
			}
			if !picked[device.Key] {
				picked[device.Key] = true
				victims = append(victims, device)
				need--
			}
		}
	}
	return victims
}
