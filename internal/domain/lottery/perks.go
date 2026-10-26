package lottery

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// 人群特权：按人群（会员、永久会员、免广告用户……）与当天的行为（签到、在线时长、表达式）
// 给这个人改写活动的参数 —— 多几次免费、视频抽奖按概率免看广告、积分抽奖打折、保底提前、中奖加成。
//
// 特权只改「这个人此刻眼里的活动」（Personalize），不引入新的次数来源，也不改客户端契约：
//
//   - 额外免费次数、免看广告的视频次数 → 都并进 free（chances.free.daily 变大）
//   - 免看广告的视频次数同时从视频次数里扣掉（chances.video.remaining 变小）
//   - 积分折扣与额外次数 → chances.points.cost / daily
//   - 保底提前 → pity.threshold / remaining
//   - 中奖加成 → 中奖奖品的权重（公示概率随之重算）
//
// 所以老版本客户端不升级就按新的数字工作：它本来就照着服务端给的 next 抽、照着 remaining 拦视频。
// 判定在服务端，活动页与抽奖事务调同一个函数，看到的「还剩 N 次」与能抽几次永远一致。

// 特权的人群。
const (
	AudienceAll         = "all"
	AudienceMember      = "member"
	AudienceLifetime    = "lifetime"
	AudienceNonMember   = "non_member"
	AudienceNonLifetime = "non_lifetime"
	AudienceAdFree      = "ad_free"
)

// 上界。
const (
	MaxPerks              = 10
	MaxPerkNameRunes      = 16
	MaxPerkExtraFree      = 20
	MaxPerkExtraPoints    = 100
	MaxPerkPointsDiscount = 90
	MaxPerkAdSkipSlots    = 20
	MaxPerkOnlineMinutes  = 1440
	MaxPerkWinBoost       = 3
	// DefaultAdSkipSlots 视频抽奖不限次时，每天前几次参与「免看广告」的判定
	DefaultAdSkipSlots = 5
)

// Perk 一条特权：谁（人群 + 当天条件）能得到什么（效果）。
type Perk struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled,omitempty"`
	// Audience 人群，见 Audience*；空为全部用户
	Audience string `json:"audience"`
	// SignedToday 今天签到之后才生效；OnlineMinutes 今天在线满这么多分钟之后才生效（0 为不要求）
	SignedToday   bool `json:"signedToday,omitempty"`
	OnlineMinutes int  `json:"onlineMinutes,omitempty"`
	// When 附加条件（expr 表达式，变量见 PerkVariables）；空为不限
	When string `json:"when,omitempty"`

	// ExtraFree 每天多几次免费抽奖
	ExtraFree int `json:"extraFree,omitempty"`
	// AdSkip 视频抽奖免看广告的概率（百分比 0–100）：今天的每一次视频抽奖各自独立判定，
	// 判中的直接变成免费次数。AdSkipSlots 视频不限次时每天前几次参与判定（0 为默认 5）
	AdSkip      float64 `json:"adSkip,omitempty"`
	AdSkipSlots int     `json:"adSkipSlots,omitempty"`
	// PointsDiscount 积分抽奖打折（百分比，30 = 省 30%）；ExtraPoints 积分抽奖每天多几次（活动设了每日上限时）
	PointsDiscount int `json:"pointsDiscount,omitempty"`
	ExtraPoints    int `json:"extraPoints,omitempty"`
	// PityCut 保底提前几次（最少保留 2 次）
	PityCut int `json:"pityCut,omitempty"`
	// WinBoost 中奖奖品的权重系数（1–3；0 或 1 为不加成）
	WinBoost float64 `json:"winBoost,omitempty"`
}

// PerkFacts 判定特权所需的事实。
type PerkFacts struct {
	Member      bool
	Lifetime    bool
	AdFree      bool
	SignedToday bool
	// OnlineMinutesToday 今天的在线分钟数（实时连接心跳累计）
	OnlineMinutesToday float64
	TotalDraws         int
	// Now 应用时区的此刻（表达式里的 draw.hour / weekday）
	Now time.Time
	// Profile 完整画像；只有特权写了表达式时才取
	Profile BehaviorProfile
}

// PerkEffects 这个人此刻得到的全部特权（多条叠加之后）。
type PerkEffects struct {
	// Names 生效的特权名
	Names     []string `json:"names"`
	ExtraFree int      `json:"extraFree,omitempty"`
	// AdSkip 合并后的免看广告概率（0–1，各条独立：1 − Π(1 − p)）
	AdSkip      float64 `json:"adSkip,omitempty"`
	AdSkipSlots int     `json:"adSkipSlots,omitempty"`
	// Waived 已解锁的免看广告次数（已并进免费次数）= WaivedUsed + 当前这一次（NextWaived）
	Waived int `json:"waived,omitempty"`
	// WaivedUsed 今天已用掉的免看广告次数；NextWaived 下一次视频抽奖免看广告（已作为 1 次免费给出）
	WaivedUsed     int     `json:"waivedUsed,omitempty"`
	NextWaived     bool    `json:"nextWaived,omitempty"`
	PointsDiscount int     `json:"pointsDiscount,omitempty"`
	ExtraPoints    int     `json:"extraPoints,omitempty"`
	PityCut        int     `json:"pityCut,omitempty"`
	WinBoost       float64 `json:"winBoost,omitempty"`
}

// Empty 没有任何生效的特权。
func (e PerkEffects) Empty() bool { return len(e.Names) == 0 }

// PerkAudienceSpec 控制台上的人群自述。
type PerkAudienceSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// PerkAudiences 人群目录，顺序即控制台的展示顺序。
func PerkAudiences() []PerkAudienceSpec {
	return []PerkAudienceSpec{
		{AudienceAll, "全部用户", "每个登录用户"},
		{AudienceMember, "会员", "当前有效的会员（含永久会员）"},
		{AudienceLifetime, "永久会员", "已开通永久会员"},
		{AudienceNonMember, "非会员", "当前不是会员"},
		{AudienceNonLifetime, "非永久会员", "不是永久会员（含限时会员）"},
		{AudienceAdFree, "免广告用户", "免广告时长有效期内的用户"},
	}
}

func audienceKnown(audience string) bool {
	return slices.ContainsFunc(PerkAudiences(), func(spec PerkAudienceSpec) bool { return spec.Key == audience })
}

// NormalizePerks 清洗：去空白、空人群按全部用户、不加成的系数归零。
func NormalizePerks(perks []Perk) []Perk {
	if len(perks) == 0 {
		return []Perk{}
	}
	out := make([]Perk, 0, len(perks))
	for _, perk := range perks {
		perk.Name = strings.TrimSpace(perk.Name)
		perk.When = strings.TrimSpace(perk.When)
		perk.Audience = strings.TrimSpace(perk.Audience)
		if perk.Audience == "" {
			perk.Audience = AudienceAll
		}
		if perk.WinBoost == 1 {
			perk.WinBoost = 0
		}
		if perk.AdSkip == 0 {
			perk.AdSkipSlots = 0
		}
		out = append(out, perk)
	}
	return out
}

// ValidatePerks 校验特权配置（表达式在这里编译，写错变量名保存时就报错）。
func ValidatePerks(perks []Perk) error {
	if len(perks) > MaxPerks {
		return fmt.Errorf("人群特权最多 %d 条", MaxPerks)
	}
	for index, perk := range perks {
		label := fmt.Sprintf("第 %d 条特权", index+1)
		if perk.Name != "" {
			label = "特权「" + perk.Name + "」"
		}
		switch {
		case utf8.RuneCountInString(perk.Name) > MaxPerkNameRunes:
			return fmt.Errorf("%s的名称不能超过 %d 字", label, MaxPerkNameRunes)
		case !audienceKnown(perk.Audience):
			return fmt.Errorf("%s：未知的人群 %s", label, perk.Audience)
		case perk.OnlineMinutes < 0 || perk.OnlineMinutes > MaxPerkOnlineMinutes:
			return fmt.Errorf("%s的在线时长条件需要在 0–%d 分钟之间", label, MaxPerkOnlineMinutes)
		case perk.ExtraFree < 0 || perk.ExtraFree > MaxPerkExtraFree:
			return fmt.Errorf("%s的额外免费次数需要在 0–%d 之间", label, MaxPerkExtraFree)
		case perk.AdSkip < 0 || perk.AdSkip > 100 || math.IsNaN(perk.AdSkip):
			return fmt.Errorf("%s的免看广告概率需要在 0–100%% 之间", label)
		case perk.AdSkipSlots < 0 || perk.AdSkipSlots > MaxPerkAdSkipSlots:
			return fmt.Errorf("%s的免看广告判定次数需要在 0–%d 之间", label, MaxPerkAdSkipSlots)
		case perk.PointsDiscount < 0 || perk.PointsDiscount > MaxPerkPointsDiscount:
			return fmt.Errorf("%s的积分折扣需要在 0–%d%% 之间", label, MaxPerkPointsDiscount)
		case perk.ExtraPoints < 0 || perk.ExtraPoints > MaxPerkExtraPoints:
			return fmt.Errorf("%s的积分抽奖额外次数需要在 0–%d 之间", label, MaxPerkExtraPoints)
		case perk.PityCut < 0 || perk.PityCut > MaxPityThreshold:
			return fmt.Errorf("%s的保底提前次数需要在 0–%d 之间", label, MaxPityThreshold)
		case perk.WinBoost != 0 && (perk.WinBoost < 1 || perk.WinBoost > MaxPerkWinBoost || math.IsNaN(perk.WinBoost)):
			return fmt.Errorf("%s的中奖加成需要在 1–%d 倍之间", label, MaxPerkWinBoost)
		case !perk.hasEffect():
			return fmt.Errorf("%s没有设置任何效果", label)
		}
		if _, err := compilePerk(perk.When); err != nil {
			return fmt.Errorf("%s的条件表达式有误：%v", label, err)
		}
	}
	return nil
}

func (p Perk) hasEffect() bool {
	return p.ExtraFree > 0 || p.AdSkip > 0 || p.PointsDiscount > 0 || p.ExtraPoints > 0 || p.PityCut > 0 || p.WinBoost > 1
}

// PerksNeedProfile 有没有特权写了表达式（要取完整画像）。
func PerksNeedProfile(perks []Perk) bool {
	return slices.ContainsFunc(perks, func(perk Perk) bool { return !perk.Disabled && perk.When != "" })
}

// PerksActive 有没有启用的特权（没有时连事实都不必取）。
func PerksActive(perks []Perk) bool {
	return slices.ContainsFunc(perks, func(perk Perk) bool { return !perk.Disabled })
}

// PerksAdSkip 有没有启用的特权带免看广告（要先知道视频抽奖今天有几次）。
func PerksAdSkip(perks []Perk) bool {
	return slices.ContainsFunc(perks, func(perk Perk) bool { return !perk.Disabled && perk.AdSkip > 0 })
}

// ResolvePerks 这个人此刻满足哪些特权，叠加成一份效果（不含免看广告的进度，见 WithAdSkip）。
//
// 叠加口径：次数相加（免费次数合计不超过单日上限）；免看广告的概率按独立事件合并；
// 折扣取最大；保底提前相加；中奖加成相乘（封顶 3 倍）。
func ResolvePerks(perks []Perk, facts PerkFacts) PerkEffects {
	effects := PerkEffects{Names: []string{}}
	keep := 1.0
	boost := 1.0
	for index, perk := range perks {
		if perk.Disabled || !perkApplies(perk, facts) {
			continue
		}
		name := perk.Name
		if name == "" {
			name = "特权 " + strconv.Itoa(index+1)
		}
		effects.Names = append(effects.Names, name)
		effects.ExtraFree += perk.ExtraFree
		if perk.AdSkip > 0 {
			keep *= 1 - perk.AdSkip/100
			slots := perk.AdSkipSlots
			if slots == 0 {
				slots = DefaultAdSkipSlots
			}
			effects.AdSkipSlots = max(effects.AdSkipSlots, slots)
		}
		effects.PointsDiscount = max(effects.PointsDiscount, perk.PointsDiscount)
		effects.ExtraPoints += perk.ExtraPoints
		effects.PityCut += perk.PityCut
		if perk.WinBoost > 1 {
			boost *= perk.WinBoost
		}
	}
	effects.ExtraFree = min(effects.ExtraFree, MaxFreeDaily)
	if keep < 1 {
		effects.AdSkip = math.Round((1-keep)*1e6) / 1e6
	}
	if boost > 1 {
		effects.WinBoost = math.Min(boost, MaxPerkWinBoost)
	}
	return effects
}

func perkApplies(perk Perk, facts PerkFacts) bool {
	switch perk.Audience {
	case AudienceMember:
		if !facts.Member && !facts.Lifetime {
			return false
		}
	case AudienceLifetime:
		if !facts.Lifetime {
			return false
		}
	case AudienceNonMember:
		if facts.Member || facts.Lifetime {
			return false
		}
	case AudienceNonLifetime:
		if facts.Lifetime {
			return false
		}
	case AudienceAdFree:
		if !facts.AdFree {
			return false
		}
	}
	if perk.SignedToday && !facts.SignedToday {
		return false
	}
	if perk.OnlineMinutes > 0 && facts.OnlineMinutesToday < float64(perk.OnlineMinutes) {
		return false
	}
	if perk.When == "" {
		return true
	}
	program, err := compilePerk(perk.When)
	if err != nil || program == nil {
		// 保存时已校验；历史坏数据按不满足处理，宁可少给
		return false
	}
	out, err := expr.Run(program, perkEnv(facts))
	if err != nil {
		return false
	}
	hit, _ := out.(bool)
	return hit
}

// VideoSlots 今天有几次视频抽奖参与「免看广告」的判定：视频抽奖没开为 0；场景限了每日次数就是那么多次，
// 不限次时取特权里的判定次数。
func VideoSlots(video VideoChances, effects PerkEffects) int {
	if !video.Enabled || effects.AdSkip <= 0 {
		return 0
	}
	if video.DailyLimit > 0 {
		return video.DailyLimit
	}
	return max(effects.AdSkipSlots, 1)
}

// AdSkipSlot 今天第 k 次视频抽奖（从 1 起）是否免看广告。
//
// 判定是确定的：同一个人、同一个活动、同一天、同一个 k 永远得到同一个结论（SHA-256 散列成 [0, 1) 的数），
// 活动页刷新多少次、抽奖事务里再算一次都是同一个数。用户改不了散列的输入，提前知道结论也换不来更多次数。
func AdSkipSlot(activityID, userID int64, dayStart time.Time, k int, p float64) bool {
	if p <= 0 {
		return false
	}
	if p >= 1 {
		return true
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("lottery-ad-skip|%d|%d|%d|%d", activityID, userID, dayStart.Unix(), k)))
	return float64(binary.BigEndian.Uint64(sum[:8])>>11)/float64(uint64(1)<<53) < p
}

// AdSkipProgress 今天走到第几次视频抽奖、免看广告解锁了几次。
type AdSkipProgress struct {
	// Used 已经用掉的免看广告次数；Next 当前这一次（下一次视频抽奖）是否免看
	Used int
	Next bool
}

// Unlocked 已解锁（并进免费次数）的免看广告次数：用掉的加上当前这一次。
func (p AdSkipProgress) Unlocked() int {
	if p.Next {
		return p.Used + 1
	}
	return p.Used
}

// WalkAdSkip 按顺序走今天的视频抽奖：第 k 次轮到时才看它免不免广告 —— 免看的那一次并进免费次数，
// 用掉之后才轮到第 k+1 次；不免的那一次要看完广告才往下走。所以任何时刻最多只有「当前这一次」是解锁的，
// 不会出现一早就把全天判中的次数一次性发出来。
//
// watched 今天已看完并发放的视频次数；waivedUsed 已用掉的免看广告次数（免费次数里超出基础免费的部分）。
// 看广告可以消耗任何一次（哪怕那一次本来免看：用户主动看了也照样给次数），免看次数只能消耗免看的那一次。
func WalkAdSkip(activityID, userID int64, dayStart time.Time, slots int, p float64, watched, waivedUsed int) AdSkipProgress {
	var progress AdSkipProgress
	if slots <= 0 || p <= 0 {
		return progress
	}
	for k := 1; k <= slots; k++ {
		skip := AdSkipSlot(activityID, userID, dayStart, k, p)
		switch {
		case skip && waivedUsed > 0:
			waivedUsed--
			progress.Used++
		case watched > 0:
			watched--
		case skip:
			progress.Next = true
			return progress
		default:
			return progress
		}
	}
	return progress
}

// WithAdSkip 补上免看广告的进度。freeUsed 今天已用的免费次数（基础免费 + 额外免费先用，超出的才算免看广告）。
func (e PerkEffects) WithAdSkip(activity Activity, userID int64, dayStart time.Time, video VideoChances, freeUsed int) PerkEffects {
	slots := VideoSlots(video, e)
	if slots <= 0 {
		return e
	}
	base := activity.FreeDaily + e.ExtraFree
	progress := WalkAdSkip(activity.ID, userID, dayStart, slots, e.AdSkip, video.Watched, max(freeUsed-base, 0))
	e.Waived, e.WaivedUsed, e.NextWaived = progress.Unlocked(), progress.Used, progress.Next
	return e
}

// Personalize 这个人眼里的活动：免费次数、积分价格与上限、保底次数按特权改写。
func (e PerkEffects) Personalize(activity Activity) Activity {
	if e.Empty() {
		return activity
	}
	activity.FreeDaily += e.ExtraFree + e.Waived
	if activity.PointsCost > 0 {
		if e.PointsDiscount > 0 {
			discounted := int64(math.Ceil(float64(activity.PointsCost) * float64(100-e.PointsDiscount) / 100))
			activity.PointsCost = max(discounted, 1)
		}
		if activity.PointsDaily > 0 {
			activity.PointsDaily += e.ExtraPoints
		}
	}
	if activity.PityThreshold > 0 && e.PityCut > 0 {
		activity.PityThreshold = max(activity.PityThreshold-e.PityCut, 2)
	}
	return activity
}

// winBoostScale 加成时先把所有权重放大这么多倍，整数权重才乘得出小数系数
const winBoostScale = 100

// BoostPrizes 中奖加成：中奖奖品的权重 × 系数（谢谢参与不变），公示概率随之重算。
func (e PerkEffects) BoostPrizes(prizes []Prize) []Prize {
	if e.WinBoost <= 1 {
		return prizes
	}
	out := slices.Clone(prizes)
	for index := range out {
		weight := out[index].Weight * winBoostScale
		if out[index].Winning() {
			weight = int(math.Round(float64(weight) * e.WinBoost))
		}
		out[index].Weight = weight
	}
	return out
}

// AdjustVideo 视频次数里扣掉已解锁的免看广告次数（它们已经并进免费次数）：用掉的免看次数与看完的广告
// 一样占今天的视频次数，当前这一次免看也不必再看广告。
func (e PerkEffects) AdjustVideo(video VideoChances) VideoChances {
	if e.Waived <= 0 || !video.Enabled || video.Remaining < 0 {
		return video
	}
	video.Remaining = max(video.Remaining-e.Waived, 0)
	if video.Remaining == 0 {
		video.Available = false
	}
	return video
}

// PerkSummary 用户端活动页上的特权说明。
type PerkSummary struct {
	Names []string `json:"names"`
	// Lines 一条效果一句话（「每天多 2 次免费抽奖」「视频抽奖 30% 概率免看广告」）
	Lines []string `json:"lines"`
	// Waived 今天已免看广告的次数（含已解锁未用的那一次）；NextWaived 下一次视频抽奖免看广告
	Waived     int    `json:"waived"`
	NextWaived bool   `json:"nextWaived,omitempty"`
	Message    string `json:"message"`
}

// SummaryOf 用户端的特权说明；没有生效的特权时为 nil。
func SummaryOf(effects PerkEffects, activity Activity, video VideoChances) *PerkSummary {
	if effects.Empty() {
		return nil
	}
	lines := []string{}
	if effects.ExtraFree > 0 {
		lines = append(lines, fmt.Sprintf("每天多 %d 次免费抽奖", effects.ExtraFree))
	}
	if effects.AdSkip > 0 && video.Enabled {
		line := "视频抽奖免看广告"
		if effects.AdSkip < 1 {
			line = fmt.Sprintf("视频抽奖 %s%% 概率免看广告", trimFloat(effects.AdSkip*100))
		}
		switch {
		case effects.NextWaived:
			line += "（本次免看，已计入免费次数）"
		case effects.WaivedUsed > 0:
			line += fmt.Sprintf("（今天已免 %d 次）", effects.WaivedUsed)
		}
		lines = append(lines, line)
	}
	if effects.PointsDiscount > 0 && activity.PointsCost > 0 {
		lines = append(lines, fmt.Sprintf("积分抽奖%s", discountLabel(effects.PointsDiscount)))
	}
	if effects.ExtraPoints > 0 && activity.PointsCost > 0 && activity.PointsDaily > 0 {
		lines = append(lines, fmt.Sprintf("积分抽奖每天多 %d 次", effects.ExtraPoints))
	}
	if effects.PityCut > 0 && activity.PityThreshold > 0 {
		lines = append(lines, fmt.Sprintf("保底提前 %d 次", effects.PityCut))
	}
	if effects.WinBoost > 1 {
		lines = append(lines, fmt.Sprintf("中奖概率提升至 %s 倍", trimFloat(effects.WinBoost)))
	}
	if len(lines) == 0 {
		return nil
	}
	return &PerkSummary{
		Names:      effects.Names,
		Lines:      lines,
		Waived:     effects.Waived,
		NextWaived: effects.NextWaived,
		Message:    strings.Join(effects.Names, "、") + "：" + strings.Join(lines, "，"),
	}
}

func discountLabel(percent int) string {
	rest := 100 - percent
	if rest%10 == 0 {
		return fmt.Sprintf(" %d 折", rest/10)
	}
	return fmt.Sprintf(" %s 折", trimFloat(float64(rest)/10))
}

func trimFloat(value float64) string {
	return strconv.FormatFloat(math.Round(value*100)/100, 'f', -1, 64)
}

// ── 表达式 ──

// PerkUser 特权条件里的 user：画像全部字段，加上今天的在线分钟、免广告、本活动累计抽奖次数。
type PerkUser struct {
	BehaviorProfile
	AdFree             bool    `expr:"adFree"`
	OnlineMinutesToday float64 `expr:"onlineMinutesToday"`
	TotalDraws         int     `expr:"totalDraws"`
}

// PerkDraw 特权条件里的 draw：此刻的小时与星期（应用时区）。
type PerkDraw struct {
	Hour    int `expr:"hour"`
	Weekday int `expr:"weekday"`
}

// PerkEnv 特权条件执行时看到的变量。
type PerkEnv struct {
	User PerkUser `expr:"user"`
	Draw PerkDraw `expr:"draw"`
}

var perkCache sync.Map // 表达式原文 → *vm.Program

func compilePerk(source string) (*vm.Program, error) {
	if source == "" {
		return nil, nil
	}
	if cached, ok := perkCache.Load(source); ok {
		return cached.(*vm.Program), nil
	}
	program, err := expr.Compile(source, expr.Env(PerkEnv{}), expr.AsBool(), expr.DisableBuiltin("fetch"))
	if err != nil {
		return nil, err
	}
	perkCache.Store(source, program)
	return program, nil
}

func perkEnv(facts PerkFacts) PerkEnv {
	profile := facts.Profile
	// 轻量事实比画像新（画像可能没取）：以它为准
	profile.Member = facts.Member || facts.Lifetime
	profile.Lifetime = facts.Lifetime
	profile.SignedToday = facts.SignedToday
	return PerkEnv{
		User: PerkUser{BehaviorProfile: profile, AdFree: facts.AdFree,
			OnlineMinutesToday: facts.OnlineMinutesToday, TotalDraws: facts.TotalDraws},
		Draw: PerkDraw{Hour: facts.Now.Hour(), Weekday: int(facts.Now.Weekday())},
	}
}

// PerkVariables 特权条件里能用的变量（控制台提示）。
func PerkVariables() []RuleVariable {
	vars := []RuleVariable{}
	for _, variable := range RuleVariables() {
		if strings.HasPrefix(variable.Name, "user.") && !slices.Contains(ruleOnlyUserVars, variable.Name) {
			vars = append(vars, variable)
		}
	}
	return append(vars,
		RuleVariable{"user.adFree", "bool", "免广告时长有效期内"},
		RuleVariable{"user.onlineMinutesToday", "number", "今天在线分钟数"},
		RuleVariable{"user.totalDraws", "number", "本活动累计抽奖次数"},
		RuleVariable{"draw.hour", "number", "小时（0–23）"},
		RuleVariable{"draw.weekday", "number", "星期（0 为周日）"},
	)
}

// ruleOnlyUserVars 只在出奖规则里有的 user 变量（依赖这一次抽奖的状态，特权判定时还没有）。
var ruleOnlyUserVars = []string{
	"user.totalDraws", "user.todayDraws", "user.missStreak", "user.winsTotal", "user.winsToday",
	"user.prizeWins", "user.prizeWinsToday", "user.score", "user.luck",
}
