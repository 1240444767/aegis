package levels

import (
	"slices"
	"strings"

	"aegis/internal/domain/itemgrant"
	notificationdomain "aegis/internal/domain/notification"
)

// 等级体系的画布节点、权益种类与图标目录。控制台的节点面板、检查器与 App 的等级页都读它。

// 节点类型。
const (
	NodeScheme    = "scheme"
	NodeLevel     = "level"
	NodeGate      = "gate"
	NodeReward    = "reward"
	NodePrivilege = "privilege"
)

// 输出端口。
const (
	HandleNext   = "next"
	HandleAttach = "attach"
)

// 节点分类（节点面板的分组）。
const (
	CategorySpine  = "spine"
	CategoryAttach = "attach"
)

// HandleSpec 一个输出端口。
type HandleSpec struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Tone  string `json:"tone"`
}

// NodeSpec 一种节点的自述。
type NodeSpec struct {
	Type     string       `json:"type"`
	Label    string       `json:"label"`
	Hint     string       `json:"hint"`
	Category string       `json:"category"`
	Outputs  []HandleSpec `json:"outputs"`
	// Inputs 是否有输入端口
	Inputs bool `json:"inputs"`
	// Single 画布上只能有一个
	Single bool `json:"single,omitempty"`
}

var nodeSpecs = []NodeSpec{
	{Type: NodeScheme, Label: "体系设置", Category: CategorySpine, Single: true,
		Hint:    "整套等级的入口：名称、升级通知、App 是否展示未达到的等级。从这里连出第一个等级",
		Outputs: []HandleSpec{{ID: HandleNext, Label: "第一级", Tone: "neutral"}}},
	{Type: NodeLevel, Label: "等级", Category: CategorySpine, Inputs: true,
		Hint: "一个等级：累计经验门槛、名称、图标与颜色。等级号按主干上的先后顺序自动编排",
		Outputs: []HandleSpec{
			{ID: HandleNext, Label: "下一级", Tone: "neutral"},
			{ID: HandleAttach, Label: "奖励与权益", Tone: "positive"},
		}},
	{Type: NodeGate, Label: "晋级条件", Category: CategorySpine, Inputs: true,
		Hint:    "放在两个等级之间：经验够了还要满足这些条件才能升到下一级（如开通会员、连续签到）",
		Outputs: []HandleSpec{{ID: HandleNext, Label: "满足后", Tone: "positive"}}},
	{Type: NodeReward, Label: "达成奖励", Category: CategoryAttach, Inputs: true,
		Hint:    "首次达到所挂等级时发放一次：可自动到账，也可以由用户在 App 里领取。物品与物品发放同一套",
		Outputs: []HandleSpec{}},
	{Type: NodePrivilege, Label: "等级权益", Category: CategoryAttach, Inputs: true,
		Hint:    "达到所挂等级后持续享有：签到加成、展示型权益或供 App 判断的自定义权益",
		Outputs: []HandleSpec{}},
}

// CategorySpec 节点面板的一组。
type CategorySpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var categorySpecs = []CategorySpec{
	{Key: CategorySpine, Label: "等级主干"},
	{Key: CategoryAttach, Label: "奖励与权益"},
}

// NodeCatalog 全部节点类型。
func NodeCatalog() []NodeSpec { return slices.Clone(nodeSpecs) }

// NodeCategories 节点分组。
func NodeCategories() []CategorySpec { return slices.Clone(categorySpecs) }

// FindNodeSpec 按类型取节点自述。
func FindNodeSpec(nodeType string) (NodeSpec, bool) {
	for _, spec := range nodeSpecs {
		if spec.Type == nodeType {
			return spec, true
		}
	}
	return NodeSpec{}, false
}

// ── 权益 ──

// 权益种类。
const (
	// PrivilegeExpBoost 签到经验倍率（签到奖励策略开启「按等级加成经验」时生效）
	PrivilegeExpBoost = "exp_boost"
	// PrivilegeIntegralBoost 签到积分倍率
	PrivilegeIntegralBoost = "integral_boost"
	// PrivilegeDisplay 展示型权益：只给用户看（专属头像框、身份标识……）
	PrivilegeDisplay = "display"
	// PrivilegeCustom 自定义权益：带一个键，App 按键判断是否享有（可附数值）
	PrivilegeCustom = "custom"
)

// 权益作用范围。
const (
	// ScopeInherit 本级及以上都享有（默认）
	ScopeInherit = "inherit"
	// ScopeOnly 只在本级享有，升到下一级就换成下一级的
	ScopeOnly = "only"
)

// PrivilegeSpec 一种权益的自述。
type PrivilegeSpec struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	// Numeric 需要一个数值（倍率或自定义数值）
	Numeric bool    `json:"numeric"`
	Min     float64 `json:"min,omitempty"`
	Max     float64 `json:"max,omitempty"`
	Unit    string  `json:"unit,omitempty"`
	// NeedsKey 需要一个键
	NeedsKey bool `json:"needsKey,omitempty"`
}

var privilegeSpecs = []PrivilegeSpec{
	{Kind: PrivilegeExpBoost, Label: "签到经验加成", Numeric: true, Min: 1, Max: 10, Unit: "倍",
		Hint: "签到所得经验乘以这个倍率；签到奖励策略需开启「按等级加成经验」。多个等级都配置时取最高等级的"},
	{Kind: PrivilegeIntegralBoost, Label: "签到积分加成", Numeric: true, Min: 1, Max: 10, Unit: "倍",
		Hint: "签到所得积分乘以这个倍率（在策略的封顶之前计算）。多个等级都配置时取最高等级的"},
	{Kind: PrivilegeDisplay, Label: "展示权益",
		Hint: "只在 App 的等级页展示，例如「专属头像框」「评论高亮」"},
	{Kind: PrivilegeCustom, Label: "自定义权益", NeedsKey: true, Min: -1_000_000, Max: 1_000_000,
		Hint: "App 通过 /points/level 返回的 privileges 按键判断，可附一个数值（如每日下载次数）。同一个键取最高等级的"},
}

// PrivilegeCatalog 全部权益种类。
func PrivilegeCatalog() []PrivilegeSpec { return slices.Clone(privilegeSpecs) }

// FindPrivilegeSpec 按种类取权益自述。
func FindPrivilegeSpec(kind string) (PrivilegeSpec, bool) {
	for _, spec := range privilegeSpecs {
		if spec.Kind == kind {
			return spec, true
		}
	}
	return PrivilegeSpec{}, false
}

// ── 奖励领取方式 ──

const (
	// ClaimAuto 升级时自动到账
	ClaimAuto = "auto"
	// ClaimManual 进入「待领取」，用户在 App 里点领取
	ClaimManual = "manual"
)

// ── 图标 ──

// IconSpec 等级与权益可选的图标。Key 是 lucide 图标名（控制台直接渲染），App 自行映射到本地图标。
type IconSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var iconSpecs = []IconSpec{
	{Key: "sprout", Label: "萌芽"}, {Key: "leaf", Label: "叶片"}, {Key: "feather", Label: "羽毛"},
	{Key: "footprints", Label: "足迹"}, {Key: "sparkles", Label: "星火"}, {Key: "shield", Label: "盾牌"},
	{Key: "award", Label: "徽章"}, {Key: "medal", Label: "奖牌"}, {Key: "anchor", Label: "船锚"},
	{Key: "compass", Label: "罗盘"}, {Key: "mountain", Label: "山峰"}, {Key: "orbit", Label: "轨道"},
	{Key: "rocket", Label: "火箭"}, {Key: "zap", Label: "闪电"}, {Key: "flame", Label: "火焰"},
	{Key: "sun", Label: "烈日"}, {Key: "moon", Label: "明月"}, {Key: "star", Label: "星辰"},
	{Key: "gem", Label: "宝石"}, {Key: "diamond", Label: "钻石"}, {Key: "crown", Label: "王冠"},
	{Key: "trophy", Label: "奖杯"}, {Key: "heart", Label: "心"}, {Key: "gift", Label: "礼物"},
	{Key: "badge-check", Label: "认证"}, {Key: "palette", Label: "调色板"}, {Key: "download", Label: "下载"},
	{Key: "message-circle", Label: "消息"},
}

// IconCatalog 全部图标。
func IconCatalog() []IconSpec { return slices.Clone(iconSpecs) }

// ValidIcon 图标是否在目录里（空也算合法：App 用等级号代替）。
func ValidIcon(key string) bool {
	if key == "" {
		return true
	}
	for _, spec := range iconSpecs {
		if spec.Key == key {
			return true
		}
	}
	return false
}

// legacyIcons 旧版全站等级表里的图标名 → 目录里的图标。
var legacyIcons = map[string]string{
	"seedling": "sprout", "spark": "sparkles", "feather": "feather", "trail": "footprints", "shield": "shield",
	"crest": "award", "anchor": "anchor", "compass": "compass", "summit": "mountain", "orbit": "orbit",
	"crown": "crown", "aurora": "moon", "phoenix": "flame", "sunfire": "sun", "skyforge": "gem",
	"starlight": "star", "everglow": "diamond", "apex": "trophy",
}

func legacyIcon(name string) string {
	name = strings.TrimSpace(name)
	if mapped, ok := legacyIcons[name]; ok {
		return mapped
	}
	if ValidIcon(name) {
		return name
	}
	return ""
}

// ── 晋级条件可用的字段 ──

// gateExcludedFields 晋级条件里不能用的字段：等级与经验本身（循环），以及时间与事件（晋级条件是持久的事实）。
var gateExcludedFields = map[string]bool{"user.level": true, "user.experience": true}

// GateFields 晋级条件可引用的字段：行为画像（与抽奖、物品发放同一份），去掉等级与经验本身。
func GateFields() []itemgrant.FieldSpec {
	out := make([]itemgrant.FieldSpec, 0, 32)
	for _, field := range itemgrant.FieldCatalog() {
		if !strings.HasPrefix(field.Key, "user.") || gateExcludedFields[field.Key] {
			continue
		}
		out = append(out, field)
	}
	return out
}

func validGateField(key string) bool {
	for _, field := range GateFields() {
		if field.Key == key {
			return true
		}
	}
	return false
}

// NotifyRoutes 升级通知可跳转的 App 页面（与消息中心同一份路由表）。
func NotifyRoutes() []notificationdomain.RouteSpec { return notificationdomain.Routes() }

// 上界（防手滑）。
const (
	MaxLevels             = 100
	MaxNodes              = 400
	MaxEdges              = 600
	MaxAttachPerLevel     = 12
	MaxThreshold          = 1_000_000_000_000
	MaxLabelRunes         = 24
	MaxDescriptionRunes   = 200
	MaxHintRunes          = 60
	MaxPrivilegeTextRunes = 80
	MaxKeyLength          = 48
	MaxNotifyTitleRunes   = 60
	MaxNotifyContentRunes = 500
	MaxConditionRules     = 20
	MaxExprLength         = 2000
)
