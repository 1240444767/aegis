package lottery

import (
	"fmt"
	"math"
	"slices"

	"gonum.org/v1/gonum/floats"
)

// 行为画像。
//
// 抽奖的「运气」不该只看这一次掷骰子：一个注册两年、天天签到、常在线、开过会员、邀请过朋友的老用户，
// 与一个刚注册、只为刷奖而来的号，常规概率完全一样是不合理的。画像把这个人在应用里的全部行为
// 归成一组信号，每个信号映射到 0–1，再按活动配置的权重加权平均成一个行为分（0–1）：
//
//	信号            来源                                   映射（half 为达到 0.5 的值）
//	注册时长        users.created_at                       x / (x + half)
//	在线时长 / 天数  user_online_daily（实时连接按天累计）     同上
//	登录天数 / 最近活跃 login_audit_logs + user_online_daily   最近活跃取 half / (x + half)，越久越低
//	签到            sign_stats / daily_signins              连续天数、近 30 天天数、累计天数
//	会员            users.vip_expire_at、vip_transactions    当前会员、永久会员、会员率（注册以来处于会员的比例）
//	付费            payment_orders(status = paid)           累计金额、笔数
//	成长            users.experience → user_levels          等级
//	积分            integral_transactions                  近 30 天获得
//	社交            invite_relations                       邀请人数
//	广告            app_rewarded_ad_views(granted)          近 30 天看完次数
//	兑换            points_store_redemptions + card_key_redemptions
//	风险（负向）     risk_assessments                       近 30 天最高风险分
//
// 负向信号不参与平均（否则「没有风险」会白送分数给新号），而是乘在最后：
// 行为分 = 正向信号的加权平均 × Π (1 − s)^权重。风险分 40、权重 2 时行为分只剩四分之一。
//
// 行为分再折成中奖奖品的系数：MinFactor + (MaxBoost − MinFactor) × 分。比如 0.8–1.5：
// 完全没有行为的新号中奖权重打八折，各方面都满的老用户放大到 1.5 倍。
// 每个信号的原始值也都暴露给自定义规则（user.onlineMinutes30d 等），想做「签到满 7 天的人积分奖翻倍」
// 这种单点规则时直接写表达式。

// 信号键。与 BehaviorProfile 的字段、规则环境里 user.* 的变量名一一对应。
const (
	SignalRegisteredDays   = "registeredDays"
	SignalOnlineMinutes7d  = "onlineMinutes7d"
	SignalOnlineMinutes30d = "onlineMinutes30d"
	SignalOnlineDays30d    = "onlineDays30d"
	SignalLoginDays30d     = "loginDays30d"
	SignalRecencyDays      = "recencyDays"
	SignalSignStreak       = "signStreak"
	SignalSignDays30d      = "signDays30d"
	SignalSignTotalDays    = "signTotalDays"
	SignalMember           = "member"
	SignalLifetime         = "lifetime"
	SignalMemberRate       = "memberRate"
	SignalPaidAmount       = "paidAmount"
	SignalPaidOrders       = "paidOrders"
	SignalLevel            = "level"
	SignalPointsEarned30d  = "pointsEarned30d"
	SignalInvites          = "invites"
	SignalAdViews30d       = "adViews30d"
	SignalRedemptions      = "redemptions"
	SignalRiskScore30d     = "riskScore30d"
)

// 映射方式。
const (
	// MapSaturate x / (x + half)：越多越高，边际递减
	MapSaturate = "saturate"
	// MapDecay half / (x + half)：越大越低（最近活跃距今天数）
	MapDecay = "decay"
	// MapBool 0 或 1
	MapBool = "bool"
	// MapRatio 原值本身就是 0–1
	MapRatio = "ratio"
)

// SignalSpec 一个信号的说明与默认参数，控制台据此渲染配置项。
type SignalSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"`
	Unit  string `json:"unit,omitempty"`
	Map   string `json:"map"`
	// Negative 负向信号：映射值越高行为分越低
	Negative bool    `json:"negative,omitempty"`
	Half     float64 `json:"half,omitempty"`
	// Weight 默认权重（「推荐配置」用）
	Weight float64 `json:"weight"`
}

var signalCatalog = []SignalSpec{
	{Key: SignalRegisteredDays, Label: "注册时长", Group: "账号", Unit: "天", Map: MapSaturate, Half: 30, Weight: 1},
	{Key: SignalOnlineMinutes7d, Label: "近 7 天在线时长", Group: "活跃", Unit: "分钟", Map: MapSaturate, Half: 180, Weight: 1},
	{Key: SignalOnlineMinutes30d, Label: "近 30 天在线时长", Group: "活跃", Unit: "分钟", Map: MapSaturate, Half: 600, Weight: 1},
	{Key: SignalOnlineDays30d, Label: "近 30 天在线天数", Group: "活跃", Unit: "天", Map: MapSaturate, Half: 10, Weight: 1},
	{Key: SignalLoginDays30d, Label: "近 30 天登录天数", Group: "活跃", Unit: "天", Map: MapSaturate, Half: 8, Weight: 0.5},
	{Key: SignalRecencyDays, Label: "距上次活跃", Group: "活跃", Unit: "天", Map: MapDecay, Half: 7, Weight: 0.5},
	{Key: SignalSignStreak, Label: "连续签到", Group: "签到", Unit: "天", Map: MapSaturate, Half: 7, Weight: 1},
	{Key: SignalSignDays30d, Label: "近 30 天签到天数", Group: "签到", Unit: "天", Map: MapSaturate, Half: 10, Weight: 1},
	{Key: SignalSignTotalDays, Label: "累计签到", Group: "签到", Unit: "天", Map: MapSaturate, Half: 60, Weight: 0.5},
	{Key: SignalMember, Label: "当前会员", Group: "会员", Map: MapBool, Weight: 1},
	{Key: SignalLifetime, Label: "永久会员", Group: "会员", Map: MapBool, Weight: 0.5},
	{Key: SignalMemberRate, Label: "会员率", Group: "会员", Unit: "%", Map: MapRatio, Weight: 1},
	{Key: SignalPaidAmount, Label: "累计付费", Group: "付费", Unit: "元", Map: MapSaturate, Half: 30, Weight: 1},
	{Key: SignalPaidOrders, Label: "付费笔数", Group: "付费", Unit: "笔", Map: MapSaturate, Half: 2, Weight: 0.5},
	{Key: SignalLevel, Label: "等级", Group: "成长", Unit: "级", Map: MapSaturate, Half: 5, Weight: 0.5},
	{Key: SignalPointsEarned30d, Label: "近 30 天获得积分", Group: "成长", Unit: "积分", Map: MapSaturate, Half: 300, Weight: 0.5},
	{Key: SignalInvites, Label: "邀请人数", Group: "社交", Unit: "人", Map: MapSaturate, Half: 3, Weight: 1},
	{Key: SignalAdViews30d, Label: "近 30 天看完激励广告", Group: "广告", Unit: "次", Map: MapSaturate, Half: 10, Weight: 0.5},
	{Key: SignalRedemptions, Label: "兑换次数", Group: "兑换", Unit: "次", Map: MapSaturate, Half: 3, Weight: 0.5},
	{Key: SignalRiskScore30d, Label: "近 30 天风险分", Group: "风险", Unit: "分", Map: MapSaturate, Negative: true, Half: 40, Weight: 2},
}

// SignalCatalog 全部信号（控制台的配置项与说明）。
func SignalCatalog() []SignalSpec { return slices.Clone(signalCatalog) }

// FindSignal 按键找信号。
func FindSignal(key string) (SignalSpec, bool) {
	for _, spec := range signalCatalog {
		if spec.Key == key {
			return spec, true
		}
	}
	return SignalSpec{}, false
}

// BehaviorProfile 一个人在应用里的全部行为（抽奖前取一次，不在抽奖事务的锁里）。
type BehaviorProfile struct {
	RegisteredDays   float64 `json:"registeredDays" expr:"registeredDays"`
	OnlineMinutes7d  float64 `json:"onlineMinutes7d" expr:"onlineMinutes7d"`
	OnlineMinutes30d float64 `json:"onlineMinutes30d" expr:"onlineMinutes30d"`
	OnlineDays30d    float64 `json:"onlineDays30d" expr:"onlineDays30d"`
	LoginDays30d     float64 `json:"loginDays30d" expr:"loginDays30d"`
	// RecencyDays 距上次登录或在线多少天；从没有记录时按注册天数算
	RecencyDays     float64 `json:"recencyDays" expr:"recencyDays"`
	SignStreak      float64 `json:"signStreak" expr:"signStreak"`
	SignDays30d     float64 `json:"signDays30d" expr:"signDays30d"`
	SignTotalDays   float64 `json:"signTotalDays" expr:"signTotalDays"`
	SignedToday     bool    `json:"signedToday" expr:"signedToday"`
	Member          bool    `json:"member" expr:"member"`
	Lifetime        bool    `json:"lifetime" expr:"lifetime"`
	MemberDays      float64 `json:"memberDays" expr:"memberDays"`
	MemberRate      float64 `json:"memberRate" expr:"memberRate"`
	PaidAmount      float64 `json:"paidAmount" expr:"paidAmount"`
	PaidOrders      float64 `json:"paidOrders" expr:"paidOrders"`
	Experience      float64 `json:"experience" expr:"experience"`
	Level           float64 `json:"level" expr:"level"`
	Integral        float64 `json:"integral" expr:"integral"`
	PointsEarned30d float64 `json:"pointsEarned30d" expr:"pointsEarned30d"`
	Invites         float64 `json:"invites" expr:"invites"`
	AdViews30d      float64 `json:"adViews30d" expr:"adViews30d"`
	Redemptions     float64 `json:"redemptions" expr:"redemptions"`
	Devices         float64 `json:"devices" expr:"devices"`
	RiskScore30d    float64 `json:"riskScore30d" expr:"riskScore30d"`
}

// Value 信号的原始值。
func (p BehaviorProfile) Value(key string) float64 {
	switch key {
	case SignalRegisteredDays:
		return p.RegisteredDays
	case SignalOnlineMinutes7d:
		return p.OnlineMinutes7d
	case SignalOnlineMinutes30d:
		return p.OnlineMinutes30d
	case SignalOnlineDays30d:
		return p.OnlineDays30d
	case SignalLoginDays30d:
		return p.LoginDays30d
	case SignalRecencyDays:
		return p.RecencyDays
	case SignalSignStreak:
		return p.SignStreak
	case SignalSignDays30d:
		return p.SignDays30d
	case SignalSignTotalDays:
		return p.SignTotalDays
	case SignalMember:
		return boolValue(p.Member)
	case SignalLifetime:
		return boolValue(p.Lifetime)
	case SignalMemberRate:
		return p.MemberRate
	case SignalPaidAmount:
		return p.PaidAmount
	case SignalPaidOrders:
		return p.PaidOrders
	case SignalLevel:
		return p.Level
	case SignalPointsEarned30d:
		return p.PointsEarned30d
	case SignalInvites:
		return p.Invites
	case SignalAdViews30d:
		return p.AdViews30d
	case SignalRedemptions:
		return p.Redemptions
	case SignalRiskScore30d:
		return p.RiskScore30d
	}
	return 0
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// BehaviorModel 行为分怎么算、怎么用（Strategy.Behavior）。
type BehaviorModel struct {
	// Signals 参与的信号与权重；空表示用全部信号的推荐权重
	Signals []BehaviorSignal `json:"signals,omitempty"`
	// MinFactor 行为分为 0 时中奖奖品的系数（0–1）；MaxBoost 行为分为 1 时的系数（≥1）
	MinFactor float64 `json:"minFactor"`
	MaxBoost  float64 `json:"maxBoost"`
	// Kinds 只作用于这些奖品类型；空为全部中奖奖品
	Kinds []string `json:"kinds,omitempty"`
}

// BehaviorSignal 一个信号的权重；Half 不填时用目录里的默认值。
type BehaviorSignal struct {
	Key    string  `json:"key"`
	Weight float64 `json:"weight"`
	Half   float64 `json:"half,omitempty"`
}

// 行为模型的取值上界。
const (
	MaxBehaviorBoost  = 10
	MaxSignalWeight   = 100
	MaxSignalHalf     = 1_000_000
	DefaultMinFactor  = 1
	DefaultBehaviorUp = 1.5
)

// SignalScore 一个信号的映射值与贡献，trace 与画像预览里展示。
type SignalScore struct {
	Key    string  `json:"key"`
	Value  float64 `json:"value"`
	Score  float64 `json:"score"`
	Weight float64 `json:"weight"`
}

// BehaviorScore 行为分与各信号明细。
type BehaviorScore struct {
	Score   float64       `json:"score"`
	Factor  float64       `json:"factor"`
	Signals []SignalScore `json:"signals"`
}

// normalizeBehavior 补默认值；没有可用信号时整个模型不生效。
func normalizeBehavior(model *BehaviorModel) *BehaviorModel {
	if model == nil {
		return nil
	}
	out := *model
	if out.MinFactor <= 0 && out.MaxBoost <= 0 {
		out.MinFactor, out.MaxBoost = DefaultMinFactor, DefaultBehaviorUp
	}
	if out.MinFactor < 0 {
		out.MinFactor = 0
	}
	if out.MaxBoost < out.MinFactor {
		out.MaxBoost = out.MinFactor
	}
	signals := make([]BehaviorSignal, 0, len(out.Signals))
	seen := map[string]bool{}
	for _, signal := range out.Signals {
		if signal.Weight == 0 || seen[signal.Key] {
			continue
		}
		seen[signal.Key] = true
		signals = append(signals, signal)
	}
	out.Signals = signals
	if out.MinFactor == 1 && out.MaxBoost == 1 {
		return nil
	}
	return &out
}

func validateBehavior(model *BehaviorModel) error {
	if model == nil {
		return nil
	}
	if model.MinFactor > 1 {
		return fmt.Errorf("行为分为 0 时的系数需要在 0–1 之间")
	}
	if model.MaxBoost > MaxBehaviorBoost {
		return fmt.Errorf("行为分为满分时的系数需要在 1–%d 之间", MaxBehaviorBoost)
	}
	for _, signal := range model.Signals {
		spec, ok := FindSignal(signal.Key)
		if !ok {
			return fmt.Errorf("未知的行为信号：%s", signal.Key)
		}
		if signal.Weight < 0 || signal.Weight > MaxSignalWeight {
			return fmt.Errorf("「%s」的权重需要在 0–%d 之间", spec.Label, MaxSignalWeight)
		}
		if signal.Half < 0 || signal.Half > MaxSignalHalf {
			return fmt.Errorf("「%s」的半饱和值需要在 0–%d 之间", spec.Label, MaxSignalHalf)
		}
	}
	for _, kind := range model.Kinds {
		if _, ok := FindSpec(kind); !ok {
			return fmt.Errorf("行为加成里的奖品类型 %s 不存在", kind)
		}
	}
	return nil
}

// ScoreBehavior 按模型给画像打分。模型为空时返回 nil。
func ScoreBehavior(model *BehaviorModel, profile BehaviorProfile) *BehaviorScore {
	if model == nil {
		return nil
	}
	signals := model.Signals
	if len(signals) == 0 {
		signals = make([]BehaviorSignal, 0, len(signalCatalog))
		for _, spec := range signalCatalog {
			signals = append(signals, BehaviorSignal{Key: spec.Key, Weight: spec.Weight})
		}
	}
	result := &BehaviorScore{Signals: make([]SignalScore, 0, len(signals))}
	scores := make([]float64, 0, len(signals))
	weights := make([]float64, 0, len(signals))
	penalty := 1.0
	for _, signal := range signals {
		spec, ok := FindSignal(signal.Key)
		if !ok || signal.Weight <= 0 {
			continue
		}
		half := signal.Half
		if half <= 0 {
			half = spec.Half
		}
		value := profile.Value(spec.Key)
		score := mapSignal(spec.Map, value, half)
		if spec.Negative {
			// 负向信号的明细记它保留下来的比例
			score = math.Pow(1-score, signal.Weight)
			penalty *= score
		} else {
			scores = append(scores, score)
			weights = append(weights, signal.Weight)
		}
		result.Signals = append(result.Signals, SignalScore{
			Key: spec.Key, Value: round6(value), Score: round6(score), Weight: signal.Weight,
		})
	}
	if total := floats.Sum(weights); total > 0 {
		result.Score = round6(floats.Dot(scores, weights) / total * penalty)
	}
	result.Factor = round6(model.MinFactor + (model.MaxBoost-model.MinFactor)*result.Score)
	return result
}

func mapSignal(mapping string, value, half float64) float64 {
	value = math.Max(0, value)
	switch mapping {
	case MapBool:
		if value > 0 {
			return 1
		}
		return 0
	case MapRatio:
		return math.Min(1, value)
	case MapDecay:
		if half <= 0 {
			return 0
		}
		return half / (value + half)
	default:
		if half <= 0 {
			return 0
		}
		return value / (value + half)
	}
}

// behaviorApplies 行为系数是否作用于这个奖品。
func behaviorApplies(model *BehaviorModel, prize Prize) bool {
	if !prize.Winning() {
		return false
	}
	return len(model.Kinds) == 0 || slices.Contains(model.Kinds, prize.Kind)
}
