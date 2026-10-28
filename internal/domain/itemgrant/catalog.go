package itemgrant

import (
	"slices"

	lotterydomain "aegis/internal/domain/lottery"
	notificationdomain "aegis/internal/domain/notification"
)

// 规则画布的节点、触发方式、事件与条件字段目录。控制台的节点面板、配置表单、条件构建器全部读它。

// 节点类型。
const (
	NodeTrigger   = "trigger"
	NodeAudience  = "audience"
	NodeCondition = "condition"
	NodeWindow    = "window"
	NodeSplit     = "split"
	NodeLimit     = "limit"
	NodeGrant     = "grant"
	NodeRandom    = "random"
	NodeNotify    = "notify"
	NodeEnd       = "end"
)

// 节点分类（节点面板的分组）。
const (
	CategoryEntry  = "entry"
	CategoryFilter = "filter"
	CategoryFlow   = "flow"
	CategoryReward = "reward"
	CategoryAction = "action"
)

// 固定的输出端口。
const (
	HandleNext = "next"
	HandleYes  = "yes"
	HandleNo   = "no"
	HandleIn   = "in"
	HandleOut  = "out"
	HandlePass = "pass"
	HandleOver = "over"
)

// HandleSpec 一个输出端口。
type HandleSpec struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Tone 端口的语义色：positive / negative / neutral
	Tone string `json:"tone"`
}

// NodeSpec 一种节点的自述。
type NodeSpec struct {
	Type     string       `json:"type"`
	Label    string       `json:"label"`
	Hint     string       `json:"hint"`
	Category string       `json:"category"`
	Outputs  []HandleSpec `json:"outputs"`
	// DynamicOutputs 输出端口由配置决定（随机分流的每个分支一个端口）
	DynamicOutputs bool `json:"dynamicOutputs,omitempty"`
	// Singleton 一条规则只能有一个（触发器）
	Singleton bool `json:"singleton,omitempty"`
	// NoInput 没有输入端口（触发器）
	NoInput bool `json:"noInput,omitempty"`
}

var nodeSpecs = []NodeSpec{
	{Type: NodeTrigger, Label: "触发器", Category: CategoryEntry, Singleton: true, NoInput: true,
		Hint:    "规则从这里开始：手动执行、按计划定时执行，或在用户注册 / 登录 / 签到 / 支付时实时触发",
		Outputs: []HandleSpec{{ID: HandleNext, Label: "开始", Tone: "neutral"}}},
	{Type: NodeAudience, Label: "目标人群", Category: CategoryFilter,
		Hint:    "按会员身份、注册时长、活跃、等级与用户名单筛选；接在触发器后面时决定手动与定时执行的发放范围",
		Outputs: []HandleSpec{{ID: HandleNext, Label: "命中", Tone: "positive"}}},
	{Type: NodeCondition, Label: "条件判断", Category: CategoryFilter,
		Hint: "用可视化条件或表达式判断这个人（行为画像、时间、事件内容），分成满足与不满足两路",
		Outputs: []HandleSpec{
			{ID: HandleYes, Label: "满足", Tone: "positive"},
			{ID: HandleNo, Label: "不满足", Tone: "negative"},
		}},
	{Type: NodeWindow, Label: "时间窗口", Category: CategoryFilter,
		Hint: "按星期、每日时段与日期范围（应用时区）分成窗口内与窗口外两路",
		Outputs: []HandleSpec{
			{ID: HandleIn, Label: "窗口内", Tone: "positive"},
			{ID: HandleOut, Label: "窗口外", Tone: "negative"},
		}},
	{Type: NodeSplit, Label: "随机分流", Category: CategoryFlow, DynamicOutputs: true,
		Hint: "按权重把人分到不同分支（A/B 测试）；固定分配时同一个人每次都进同一个分支"},
	{Type: NodeLimit, Label: "频次限额", Category: CategoryFlow,
		Hint: "每人在周期内最多经过几次、全站在周期内最多几人次；超出的走「超出」分支",
		Outputs: []HandleSpec{
			{ID: HandlePass, Label: "未超出", Tone: "positive"},
			{ID: HandleOver, Label: "超出", Tone: "negative"},
		}},
	{Type: NodeGrant, Label: "发放物品", Category: CategoryReward,
		Hint:    "发放一组物品；数量可写成按人计算的表达式。可以继续连到下一步",
		Outputs: []HandleSpec{{ID: HandleNext, Label: "然后", Tone: "neutral"}}},
	{Type: NodeRandom, Label: "随机礼包", Category: CategoryReward,
		Hint:    "从若干个选项里按权重抽出一个或几个（不放回），每个选项是一组物品",
		Outputs: []HandleSpec{{ID: HandleNext, Label: "然后", Tone: "neutral"}}},
	{Type: NodeNotify, Label: "站内信", Category: CategoryAction,
		Hint:    "发放成功后给用户发一条站内信；{items} 换成本次发放的物品，{nickname} 换成昵称",
		Outputs: []HandleSpec{{ID: HandleNext, Label: "然后", Tone: "neutral"}}},
	{Type: NodeEnd, Label: "结束", Category: CategoryAction, Outputs: []HandleSpec{},
		Hint: "显式结束这一路；没有连出的节点也会自然结束"},
}

// CategorySpec 节点分类的自述。
type CategorySpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var categorySpecs = []CategorySpec{
	{Key: CategoryEntry, Label: "入口"},
	{Key: CategoryFilter, Label: "筛选"},
	{Key: CategoryFlow, Label: "分流与限额"},
	{Key: CategoryReward, Label: "发放"},
	{Key: CategoryAction, Label: "动作"},
}

// NodeCatalog 全部节点（副本）。
func NodeCatalog() []NodeSpec { return slices.Clone(nodeSpecs) }

// NodeCategories 节点分类（副本）。
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

// ── 触发方式 ──

const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
	TriggerEvent    = "event"
)

// TriggerSpec 触发方式的自述。
type TriggerSpec struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

var triggerSpecs = []TriggerSpec{
	{Kind: TriggerManual, Label: "手动执行", Hint: "在控制台点「执行」时，对目标人群逐人判定并发放"},
	{Kind: TriggerSchedule, Label: "定时执行", Hint: "按 Cron 表达式（应用时区）定时对目标人群执行，例如每天 10:00"},
	{Kind: TriggerEvent, Label: "事件触发", Hint: "用户发生指定行为时实时为这个人判定并发放"},
}

// TriggerCatalog 触发方式（副本）。
func TriggerCatalog() []TriggerSpec { return slices.Clone(triggerSpecs) }

// ValidTrigger 触发方式是否登记过。
func ValidTrigger(kind string) bool {
	return slices.ContainsFunc(triggerSpecs, func(spec TriggerSpec) bool { return spec.Kind == kind })
}

// ── 事件 ──

const (
	EventRegister = "register"
	EventLogin    = "login"
	EventSignIn   = "signin"
	EventPayment  = "payment"
)

// EventSpec 一种事件的自述。Fields 是事件自带的数据，条件里以 event.<key> 引用。
type EventSpec struct {
	Key    string      `json:"key"`
	Label  string      `json:"label"`
	Hint   string      `json:"hint"`
	Fields []FieldSpec `json:"fields"`
}

var eventSpecs = []EventSpec{
	{Key: EventRegister, Label: "注册成功", Hint: "新用户完成注册（账号密码、短信、第三方、卡密注册都算）；同一个人只触发一次",
		Fields: []FieldSpec{}},
	{Key: EventLogin, Label: "登录成功", Hint: "每次签发登录会话；常与「频次限额」搭配做每日登录礼",
		Fields: []FieldSpec{}},
	{Key: EventSignIn, Label: "每日签到", Hint: "用户完成当天签到（含自动签到）",
		Fields: []FieldSpec{
			{Key: "event.consecutiveDays", Label: "本次连续签到天数", Group: "事件", Kind: FieldNumber, Unit: "天"},
		}},
	{Key: EventPayment, Label: "支付成功", Hint: "一笔在线支付订单确认到账",
		Fields: []FieldSpec{
			{Key: "event.amount", Label: "支付金额", Group: "事件", Kind: FieldNumber, Unit: "元"},
		}},
}

// EventCatalog 全部事件（副本）。
func EventCatalog() []EventSpec { return slices.Clone(eventSpecs) }

// FindEventSpec 按键取事件。
func FindEventSpec(key string) (EventSpec, bool) {
	for _, spec := range eventSpecs {
		if spec.Key == key {
			return spec, true
		}
	}
	return EventSpec{}, false
}

// ── 条件字段 ──

const (
	FieldNumber = "number"
	FieldBool   = "bool"
)

// FieldSpec 条件里可引用的一个字段。Key 就是表达式里的写法（user.level、now.hour、event.amount）。
type FieldSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"`
	Kind  string `json:"kind"`
	Unit  string `json:"unit,omitempty"`
}

// extraUserFields 行为画像里有、但不是抽奖信号的字段。
var extraUserFields = []FieldSpec{
	{Key: "user.signedToday", Label: "今天已签到", Group: "签到", Kind: FieldBool},
	{Key: "user.memberDays", Label: "累计会员天数", Group: "会员", Kind: FieldNumber, Unit: "天"},
	{Key: "user.experience", Label: "经验值", Group: "成长", Kind: FieldNumber, Unit: "点"},
	{Key: "user.integral", Label: "当前积分", Group: "成长", Kind: FieldNumber, Unit: "积分"},
	{Key: "user.devices", Label: "登录过的设备数", Group: "账号", Kind: FieldNumber, Unit: "台"},
}

var timeFields = []FieldSpec{
	{Key: "now.hour", Label: "当前小时（0–23）", Group: "时间", Kind: FieldNumber, Unit: "时"},
	{Key: "now.weekday", Label: "星期（1 为周一）", Group: "时间", Kind: FieldNumber},
	{Key: "now.day", Label: "日期（几号）", Group: "时间", Kind: FieldNumber, Unit: "号"},
	{Key: "now.month", Label: "月份", Group: "时间", Kind: FieldNumber, Unit: "月"},
}

// FieldCatalog 全部条件字段：行为画像（与抽奖的行为信号同一份）、时间、各事件自带的数据。
func FieldCatalog() []FieldSpec {
	out := make([]FieldSpec, 0, 40)
	for _, signal := range lotterydomain.SignalCatalog() {
		kind := FieldNumber
		if signal.Map == lotterydomain.MapBool {
			kind = FieldBool
		}
		unit := signal.Unit
		if signal.Key == lotterydomain.SignalMemberRate {
			// 规则里写的是 0–1 的原值，不是百分数
			unit = ""
		}
		out = append(out, FieldSpec{Key: "user." + signal.Key, Label: signal.Label, Group: signal.Group, Kind: kind, Unit: unit})
	}
	out = append(out, extraUserFields...)
	out = append(out, timeFields...)
	for _, event := range eventSpecs {
		out = append(out, event.Fields...)
	}
	return out
}

// FindField 按键取条件字段。
func FindField(key string) (FieldSpec, bool) {
	for _, field := range FieldCatalog() {
		if field.Key == key {
			return field, true
		}
	}
	return FieldSpec{}, false
}

// ── 条件运算符 ──

const (
	OpEq      = "eq"
	OpNe      = "ne"
	OpGt      = "gt"
	OpGte     = "gte"
	OpLt      = "lt"
	OpLte     = "lte"
	OpBetween = "between"
	OpIn      = "in"
	OpTrue    = "true"
	OpFalse   = "false"
)

// OperatorSpec 运算符的自述；Kinds 是它适用的字段类型。
type OperatorSpec struct {
	Op    string   `json:"op"`
	Label string   `json:"label"`
	Kinds []string `json:"kinds"`
	// Arity 取值个数：0 不需要、1 一个、2 两个（区间）、-1 任意多个（列表）
	Arity int `json:"arity"`
}

var operatorSpecs = []OperatorSpec{
	{Op: OpEq, Label: "等于", Kinds: []string{FieldNumber}, Arity: 1},
	{Op: OpNe, Label: "不等于", Kinds: []string{FieldNumber}, Arity: 1},
	{Op: OpGt, Label: "大于", Kinds: []string{FieldNumber}, Arity: 1},
	{Op: OpGte, Label: "大于等于", Kinds: []string{FieldNumber}, Arity: 1},
	{Op: OpLt, Label: "小于", Kinds: []string{FieldNumber}, Arity: 1},
	{Op: OpLte, Label: "小于等于", Kinds: []string{FieldNumber}, Arity: 1},
	{Op: OpBetween, Label: "介于", Kinds: []string{FieldNumber}, Arity: 2},
	{Op: OpIn, Label: "属于", Kinds: []string{FieldNumber}, Arity: -1},
	{Op: OpTrue, Label: "是", Kinds: []string{FieldBool}, Arity: 0},
	{Op: OpFalse, Label: "否", Kinds: []string{FieldBool}, Arity: 0},
}

// OperatorCatalog 全部运算符（副本）。
func OperatorCatalog() []OperatorSpec { return slices.Clone(operatorSpecs) }

// FindOperator 按键取运算符。
func FindOperator(op string) (OperatorSpec, bool) {
	for _, spec := range operatorSpecs {
		if spec.Op == op {
			return spec, true
		}
	}
	return OperatorSpec{}, false
}

// ── 人群范围、限额周期 ──

const (
	ScopeAll         = "all"
	ScopeMember      = "member"
	ScopeNonMember   = "non_member"
	ScopeLifetime    = "lifetime"
	ScopeNonLifetime = "non_lifetime"
)

// OptionSpec 一个枚举取值。
type OptionSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var scopeSpecs = []OptionSpec{
	{Key: ScopeAll, Label: "全部用户"},
	{Key: ScopeMember, Label: "会员（含永久）"},
	{Key: ScopeNonMember, Label: "非会员"},
	{Key: ScopeLifetime, Label: "永久会员"},
	{Key: ScopeNonLifetime, Label: "非永久会员"},
}

const (
	PeriodTotal = "total"
	PeriodDay   = "day"
	PeriodWeek  = "week"
	PeriodMonth = "month"
)

var periodSpecs = []OptionSpec{
	{Key: PeriodTotal, Label: "累计"},
	{Key: PeriodDay, Label: "每天"},
	{Key: PeriodWeek, Label: "每周"},
	{Key: PeriodMonth, Label: "每月"},
}

// Scopes 人群范围（副本）。
func Scopes() []OptionSpec { return slices.Clone(scopeSpecs) }

// Periods 限额周期（副本）。
func Periods() []OptionSpec { return slices.Clone(periodSpecs) }

func validOption(specs []OptionSpec, key string) bool {
	return slices.ContainsFunc(specs, func(spec OptionSpec) bool { return spec.Key == key })
}

// NotifyRoutes 站内信可跳转的 App 页面（与消息中心同一份路由键）。
func NotifyRoutes() []notificationdomain.RouteSpec { return notificationdomain.Routes() }
