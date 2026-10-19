package lottery

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/pointstore"
)

// 奖品类型。除「谢谢参与」外与积分商城、邀请同名同义（发放走同一套账本）。
const (
	KindNone         = "none"
	KindPoints       = invite.RewardPoints
	KindVipPlan      = pointstore.RewardVipPlan
	KindVipDays      = pointstore.RewardVipDays
	KindFeature      = pointstore.RewardFeature
	KindExperience   = pointstore.RewardExperience
	KindLotteryDraws = pointstore.RewardLotteryDraws
	KindManual       = pointstore.RewardManual
)

// IntegralCategory / SourceType 积分与经验流水上的分类和来源：抽奖。
const (
	IntegralCategory = "lottery"
	SourceType       = "lottery"
)

// RewardSpec 一档奖品的自述，形状与积分商城相同；控制台的奖品表单由它驱动。
type RewardSpec = pointstore.RewardSpec

// kinds 抽奖能发的奖品，顺序即控制台的展示顺序。
//
// 不含钱包余额：余额是真钱，放进一个按概率出奖的盘子里，等于运营在做一件需要牌照的事。
var kinds = []string{
	KindNone,
	KindPoints,
	KindExperience,
	KindVipDays,
	KindVipPlan,
	KindFeature,
	KindLotteryDraws,
	KindManual,
}

var noneSpec = RewardSpec{
	Type:  KindNone,
	Label: "谢谢参与",
	Hint:  "不发放任何东西；连续抽到它会累计保底进度",
	Value: "",
}

// Catalog 全部奖品档位。
func Catalog() []RewardSpec {
	out := make([]RewardSpec, 0, len(kinds))
	for _, kind := range kinds {
		if spec, ok := FindSpec(kind); ok {
			out = append(out, spec)
		}
	}
	return out
}

// FindSpec 按类型取奖品自述。
func FindSpec(kind string) (RewardSpec, bool) {
	switch kind {
	case KindNone:
		return noneSpec, true
	case KindPoints:
		spec, ok := invite.FindRewardSpec(KindPoints)
		if ok {
			spec.Hint = "计入积分余额，流水分类为抽奖"
		}
		return spec, ok
	}
	if !slices.Contains(kinds, kind) {
		return RewardSpec{}, false
	}
	spec, ok := pointstore.FindRewardSpec(kind)
	if !ok {
		return RewardSpec{}, false
	}
	spec.Repeatable, spec.Exclusive = false, false
	switch kind {
	case KindVipPlan:
		spec.Hint = "按套餐配置发放时长与功能；不能选择试用套餐"
	case KindFeature:
		spec.Hint = "单独解锁一个功能，不成为会员；天数为 0 即永久，同一功能多次抽中时顺延"
	case KindLotteryDraws:
		spec.Hint = "抽中后立即计入抽奖次数，任意活动可用"
	case KindManual:
		spec.Hint = "抽中后进入待发放，由管理员在抽奖记录里标记已发放"
	}
	return spec, true
}

// Kinds 全部奖品类型。
func Kinds() []string { return slices.Clone(kinds) }

// UIModes 界面形态。
func UIModes() []string { return []string{UIModeGrid, UIModeWheel} }

// Statuses 活动状态，顺序即控制台的展示顺序。
func Statuses() []string { return []string{StatusDraft, StatusActive, StatusPaused, StatusEnded} }

// Sources 次数来源。
func Sources() []string { return []string{SourceFree, SourceBonus, SourcePoints} }

// NormalizeReward 只留与类型取值形态相关的字段。
func NormalizeReward(reward Reward) Reward {
	reward.Type = strings.TrimSpace(reward.Type)
	if reward.Type == KindNone {
		return Reward{Type: KindNone}
	}
	if reward.Type == KindPoints {
		return Reward{Type: KindPoints, Amount: reward.Amount}
	}
	normalized := pointstore.NormalizeRewards([]Reward{reward})
	if len(normalized) == 0 {
		// 未登记的类型原样留着，交给 ValidateReward 报出来
		return reward
	}
	return normalized[0]
}

// ValidateReward 校验一个奖品发的东西（只校验形状，引用的套餐与功能是否存在由服务层查）。
func ValidateReward(reward Reward) error {
	if reward.Type == KindNone {
		return nil
	}
	if !slices.Contains(kinds, reward.Type) {
		return fmt.Errorf("未登记的奖品类型：%s", reward.Type)
	}
	if reward.Type == KindPoints {
		return invite.ValidateRewards("奖品", []Reward{reward})
	}
	return pointstore.ValidateRewards([]Reward{reward})
}

// DescribeReward 一句话说清这个奖品发什么（「积分 30 点」「会员 7 天」）。
func DescribeReward(reward Reward, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef) pointstore.ClientReward {
	if reward.Type == KindNone {
		return pointstore.ClientReward{Type: KindNone, Label: noneSpec.Label, Detail: noneSpec.Label}
	}
	return invite.DescribeReward(reward, plans, features)
}

// DefaultPrizeName 奖品没起名字时用的名字：「30 积分」「会员 7 天」「谢谢参与」。
func DefaultPrizeName(reward Reward, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef) string {
	switch reward.Type {
	case KindNone:
		return noneSpec.Label
	case KindPoints:
		return fmt.Sprintf("%d 积分", reward.Amount)
	case KindExperience:
		return fmt.Sprintf("%d 经验", reward.Amount)
	case KindLotteryDraws:
		return fmt.Sprintf("抽奖 %d 次", reward.Amount)
	}
	name := DescribeReward(reward, plans, features).Detail
	if utf8.RuneCountInString(name) > MaxPrizeNameRunes {
		name = string([]rune(name)[:MaxPrizeNameRunes])
	}
	return name
}

// NormalizeInput 清洗活动输入：去空白、补默认值、奖品按位置排序。
func NormalizeInput(input ActivityInput) ActivityInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Rules = strings.TrimSpace(input.Rules)
	input.VideoScene = strings.ToLower(strings.TrimSpace(input.VideoScene))
	if input.UIMode == "" {
		input.UIMode = UIModeGrid
	}
	if input.Status == "" {
		input.Status = StatusDraft
	}
	if input.PityThreshold <= 0 {
		input.PityThreshold = 0
		input.PityPosition = -1
	}
	prizes := make([]PrizeInput, 0, len(input.Prizes))
	for _, prize := range input.Prizes {
		prize.Name = strings.TrimSpace(prize.Name)
		prize.ImageURL = strings.TrimSpace(prize.ImageURL)
		prize.Reward = NormalizeReward(prize.Reward)
		if prize.Quantity < 0 {
			prize.Quantity = -1
		}
		prizes = append(prizes, prize)
	}
	slices.SortStableFunc(prizes, func(a, b PrizeInput) int { return a.Position - b.Position })
	input.Prizes = prizes
	return input
}

// ValidateInput 校验清洗后的活动输入。每一类问题都给出能直接照着改的文案。
func ValidateInput(input ActivityInput) error {
	switch {
	case input.Name == "":
		return fmt.Errorf("活动名称不能为空")
	case utf8.RuneCountInString(input.Name) > MaxNameRunes:
		return fmt.Errorf("活动名称不能超过 %d 字", MaxNameRunes)
	case utf8.RuneCountInString(input.Description) > MaxDescriptionRunes:
		return fmt.Errorf("活动简介不能超过 %d 字", MaxDescriptionRunes)
	case utf8.RuneCountInString(input.Rules) > MaxRulesRunes:
		return fmt.Errorf("活动规则不能超过 %d 字", MaxRulesRunes)
	case !slices.Contains(UIModes(), input.UIMode):
		return fmt.Errorf("未知的界面形态：%s", input.UIMode)
	case !slices.Contains(Statuses(), input.Status):
		return fmt.Errorf("未知的活动状态：%s", input.Status)
	case input.StartTime != nil && input.EndTime != nil && !input.EndTime.After(*input.StartTime):
		return fmt.Errorf("结束时间需要晚于开始时间")
	case input.FreeDaily < 0 || input.FreeDaily > MaxFreeDaily:
		return fmt.Errorf("每日免费次数需要在 0–%d 之间", MaxFreeDaily)
	case input.PointsCost < 0 || input.PointsCost > MaxPointsCost:
		return fmt.Errorf("每次消耗积分需要在 0–%d 之间", MaxPointsCost)
	case input.PointsDaily < 0 || input.PointsDaily > MaxPointsDaily:
		return fmt.Errorf("积分抽奖每日上限需要在 0–%d 之间", MaxPointsDaily)
	case input.PityThreshold < 0 || input.PityThreshold > MaxPityThreshold:
		return fmt.Errorf("保底次数需要在 0–%d 之间", MaxPityThreshold)
	case input.PityThreshold == 1:
		return fmt.Errorf("保底次数至少为 2（为 1 等于每次必中保底奖）")
	}
	return validatePrizes(input)
}

func validatePrizes(input ActivityInput) error {
	count := len(input.Prizes)
	switch input.UIMode {
	case UIModeGrid:
		if count != GridPrizeCount {
			return fmt.Errorf("九宫格需要恰好 %d 个奖品，当前 %d 个", GridPrizeCount, count)
		}
	case UIModeWheel:
		if count < WheelMinPrizes || count > WheelMaxPrizes {
			return fmt.Errorf("转盘需要 %d–%d 个奖品，当前 %d 个", WheelMinPrizes, WheelMaxPrizes, count)
		}
	}
	positions := make(map[int]bool, count)
	ids := make(map[int64]bool, count)
	totalWeight := 0
	unlimited := false
	fallbacks := 0
	for index, prize := range input.Prizes {
		label := fmt.Sprintf("第 %d 个奖品", index+1)
		if prize.Position < 0 || prize.Position >= count {
			return fmt.Errorf("%s的位置需要在 0–%d 之间", label, count-1)
		}
		if positions[prize.Position] {
			return fmt.Errorf("奖品位置 %d 重复", prize.Position)
		}
		positions[prize.Position] = true
		if prize.ID > 0 {
			if ids[prize.ID] {
				return fmt.Errorf("奖品 #%d 重复出现", prize.ID)
			}
			ids[prize.ID] = true
		}
		if utf8.RuneCountInString(prize.Name) > MaxPrizeNameRunes {
			return fmt.Errorf("%s的名称不能超过 %d 字", label, MaxPrizeNameRunes)
		}
		if len(prize.ImageURL) > 512 {
			return fmt.Errorf("%s的图片地址过长", label)
		}
		if prize.ImageURL != "" && !strings.HasPrefix(prize.ImageURL, "https://") && !strings.HasPrefix(prize.ImageURL, "http://") {
			return fmt.Errorf("%s的图片地址需以 http(s):// 开头", label)
		}
		if err := ValidateReward(prize.Reward); err != nil {
			return fmt.Errorf("%s：%w", label, err)
		}
		if prize.Weight < 0 || prize.Weight > MaxWeight {
			return fmt.Errorf("%s的权重需要在 0–%d 之间", label, MaxWeight)
		}
		if prize.Quantity > MaxQuantity {
			return fmt.Errorf("%s的库存不能超过 %d", label, MaxQuantity)
		}
		totalWeight += prize.Weight
		if prize.Quantity < 0 {
			unlimited = true
		}
		if prize.Fallback {
			fallbacks++
			if prize.Quantity >= 0 {
				return fmt.Errorf("%s是兜底奖，库存必须不限", label)
			}
		}
	}
	if fallbacks > 1 {
		return fmt.Errorf("最多设置一个兜底奖")
	}
	if totalWeight <= 0 {
		return fmt.Errorf("奖品的权重合计必须大于 0")
	}
	// 至少一个不限库存的奖品：否则奖池总有被抽空的一天，那之后每一次抽奖都只能报错，
	// 而用户的免费次数、积分与看过的广告已经花出去了。
	if !unlimited {
		return fmt.Errorf("至少需要一个不限库存的奖品（通常是「谢谢参与」）")
	}
	if input.PityThreshold > 0 {
		if input.PityPosition < 0 {
			return fmt.Errorf("开启保底时需要选择保底奖品")
		}
		found := false
		for _, prize := range input.Prizes {
			if prize.Position != input.PityPosition {
				continue
			}
			found = true
			if prize.Reward.Type == KindNone {
				return fmt.Errorf("保底奖品不能是「谢谢参与」")
			}
		}
		if !found {
			return fmt.Errorf("保底奖品不在奖品列表里")
		}
	}
	return nil
}

// Probabilities 常规抽取下每个奖品的公示概率（百分比，保留两位小数），按 prizes 的顺序。
// 售罄的奖品不参与抽取，概率为 0。
func Probabilities(prizes []Prize) []float64 {
	total := 0
	for _, prize := range prizes {
		if prize.InStock() {
			total += prize.Weight
		}
	}
	out := make([]float64, len(prizes))
	if total == 0 {
		return out
	}
	for index, prize := range prizes {
		if !prize.InStock() || prize.Weight == 0 {
			continue
		}
		percent := float64(prize.Weight) * 100 / float64(total)
		out[index] = float64(int64(percent*100+0.5)) / 100
	}
	return out
}

// Candidates 常规抽取的候选：有库存、权重大于 0。
func Candidates(prizes []Prize) []Prize {
	out := make([]Prize, 0, len(prizes))
	for _, prize := range prizes {
		if prize.Weight > 0 && prize.InStock() {
			out = append(out, prize)
		}
	}
	return out
}

// Pick 在候选里按权重取一个。roll 是 [0, 权重合计) 里的均匀随机数，由调用方用 crypto/rand 产生。
func Pick(candidates []Prize, roll int64) (Prize, bool) {
	var cumulative int64
	for _, prize := range candidates {
		cumulative += int64(prize.Weight)
		if roll < cumulative {
			return prize, true
		}
	}
	return Prize{}, false
}

// TotalWeight 候选的权重合计。
func TotalWeight(candidates []Prize) int64 {
	var total int64
	for _, prize := range candidates {
		total += int64(prize.Weight)
	}
	return total
}

// FallbackPrize 抽中的奖品被抢光时改发的奖品：标了兜底的那个；没有标的话，
// 依次取不限库存的「谢谢参与」、任意不限库存的奖品。
func FallbackPrize(prizes []Prize) (Prize, bool) {
	for _, prize := range prizes {
		if prize.Fallback && prize.Quantity < 0 {
			return prize, true
		}
	}
	for _, prize := range prizes {
		if prize.Kind == KindNone && prize.Quantity < 0 {
			return prize, true
		}
	}
	for _, prize := range prizes {
		if prize.Quantity < 0 {
			return prize, true
		}
	}
	return Prize{}, false
}

// PityDue 这一次是否触发保底：连续 missStreak 次没抽中，这一次就是第 threshold 次。
func PityDue(threshold, missStreak int) bool {
	return threshold > 0 && missStreak+1 >= threshold
}

// PityProgress 保底进度：最多再抽几次必中。
func PityProgress(activity Activity, prizes []Prize, missStreak int) *Pity {
	if activity.PityThreshold <= 0 || activity.PityPrizeID == nil {
		return nil
	}
	for _, prize := range prizes {
		if prize.ID != *activity.PityPrizeID {
			continue
		}
		// 保底奖抽光了就不再承诺「必中」
		if !prize.InStock() {
			return nil
		}
		remaining := activity.PityThreshold - missStreak
		if remaining < 1 {
			remaining = 1
		}
		return &Pity{Threshold: activity.PityThreshold, PrizeID: prize.ID, PrizeName: prize.Name, Remaining: remaining}
	}
	return nil
}

// NextMissStreak 抽完之后的连续未中次数。
func NextMissStreak(missStreak int, prize Prize) int {
	if prize.Winning() {
		return 0
	}
	return missStreak + 1
}
