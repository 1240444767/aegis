package rewardedad

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	cardkeydomain "aegis/internal/domain/cardkey"
	vipdomain "aegis/internal/domain/vip"
)

var (
	sceneKeyPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	placementIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	providerAppPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{0,64}$`)
	transIDPattern     = regexp.MustCompile(`^[A-Za-z0-9_.:-]{6,128}$`)
)

// featureMinutesSpec 「功能时长」的自述。取值形态是功能标识 + 分钟数（Reward.Feature + Reward.Amount）。
var featureMinutesSpec = cardkeydomain.RewardSpec{
	Type:  RewardFeatureMinutes,
	Label: "功能时长",
	Hint:  "给一个功能加使用时长，按实际使用扣；会员或已拥有该功能的用户使用时不扣，余额保留",
	Value: cardkeydomain.ValueFeature,
	Unit:  "分钟",
	Min:   1,
	Max:   MaxFeatureMinutes,
}

// aiCreditSpec 「AI 额度」的自述。取值形态是点数（Reward.Amount）。
var aiCreditSpec = cardkeydomain.RewardSpec{
	Type:  RewardAICredit,
	Label: "AI 额度",
	Hint:  "给 AI 中转加赠送额度，1 点按 1 积分定价、不过期；调用时按应用设置的扣费顺序使用",
	Value: cardkeydomain.ValueAmount,
	Unit:  "点",
	Min:   1,
	Max:   MaxAICreditPoints,
}

// RewardCatalog 激励广告可配的权益档位：卡密目录的子集（顺序沿用卡密目录），末尾加上广告独有的「功能时长」。
//
// 「会员天数」的说明与卡密不同：卡密赠送的天数不带功能，激励广告的按场景的会员权益带。
func RewardCatalog() []cardkeydomain.RewardSpec {
	out := make([]cardkeydomain.RewardSpec, 0, len(allowedRewardTypes))
	for _, kind := range allowedRewardTypes {
		if spec, ok := FindRewardSpec(kind); ok {
			out = append(out, spec)
		}
	}
	return out
}

// FindRewardSpec 按类型取激励广告开放的权益自述；不开放给广告的档位返回 false。
func FindRewardSpec(rewardType string) (cardkeydomain.RewardSpec, bool) {
	if rewardType == RewardFeatureMinutes {
		return featureMinutesSpec, true
	}
	if rewardType == RewardAICredit {
		return aiCreditSpec, true
	}
	if !slices.Contains(allowedRewardTypes, rewardType) {
		return cardkeydomain.RewardSpec{}, false
	}
	spec, ok := cardkeydomain.FindRewardSpec(rewardType)
	if ok && spec.Type == cardkeydomain.RewardVipDays {
		spec.Hint = "不挂套餐，直接把会员到期时间往后顺延；功能标识按场景的会员权益配置，改配置对已领到的会员同样生效"
	}
	return spec, ok
}

// RewardTypes 激励广告可发的权益类型。
func RewardTypes() []string {
	out := make([]string, len(allowedRewardTypes))
	copy(out, allowedRewardTypes)
	return out
}

// VerifyModes 全部校验模式，顺序即控制台的展示顺序。
func VerifyModes() []string {
	return []string{VerifyDual, VerifyServer, VerifyClient}
}

// Providers 已支持的广告平台。
func Providers() []string {
	return []string{ProviderApex}
}

// ValidVerifyMode 校验模式是否合法。
func ValidVerifyMode(mode string) bool {
	return slices.Contains(VerifyModes(), mode)
}

// ValidTransID 平台 trans_id 的形态校验。只挡明显的垃圾输入，不对平台的格式做更多假设。
func ValidTransID(transID string) bool {
	return transIDPattern.MatchString(transID)
}

// ValidateRewards 校验一个场景的权益：卡密目录的规则之上，再挡掉不开放给广告的档位，
// 并校验广告独有的「功能时长」（功能标识的形状与分钟数；是否在本应用的功能目录里由服务层核对）。
func ValidateRewards(rewards []Reward) error {
	if len(rewards) == 0 {
		return fmt.Errorf("至少要配置一项权益")
	}
	shared := make([]Reward, 0, len(rewards))
	featureMinutes := 0
	aiCredits := 0
	for _, reward := range rewards {
		if !slices.Contains(allowedRewardTypes, reward.Type) {
			if spec, ok := cardkeydomain.FindRewardSpec(reward.Type); ok {
				return fmt.Errorf("激励广告不能发放「%s」", spec.Label)
			}
			return fmt.Errorf("未登记的权益类型：%s", reward.Type)
		}
		if reward.Type == RewardAICredit {
			aiCredits++
			if aiCredits > 1 {
				return fmt.Errorf("「%s」重复配置了两次", aiCreditSpec.Label)
			}
			if reward.Amount < aiCreditSpec.Min || reward.Amount > aiCreditSpec.Max {
				return fmt.Errorf("「%s」需要在 %d–%d %s之间", aiCreditSpec.Label, aiCreditSpec.Min, aiCreditSpec.Max, aiCreditSpec.Unit)
			}
			continue
		}
		if reward.Type != RewardFeatureMinutes {
			shared = append(shared, reward)
			continue
		}
		featureMinutes++
		if featureMinutes > 1 {
			return fmt.Errorf("「%s」重复配置了两次", featureMinutesSpec.Label)
		}
		if strings.TrimSpace(reward.Feature) == "" {
			return fmt.Errorf("「%s」没有选择功能", featureMinutesSpec.Label)
		}
		if !vipdomain.FeatureTagPattern.MatchString(reward.Feature) {
			return fmt.Errorf("「%s」的功能标识「%s」无效", featureMinutesSpec.Label, reward.Feature)
		}
		if reward.Amount < featureMinutesSpec.Min || reward.Amount > featureMinutesSpec.Max {
			return fmt.Errorf("「%s」需要在 %d–%d %s之间", featureMinutesSpec.Label,
				featureMinutesSpec.Min, featureMinutesSpec.Max, featureMinutesSpec.Unit)
		}
	}
	if len(rewards) > cardkeydomain.MaxRewardsPerCard {
		return fmt.Errorf("一个场景最多配置 %d 项权益", cardkeydomain.MaxRewardsPerCard)
	}
	if len(shared) == 0 {
		return nil
	}
	return cardkeydomain.ValidateRewards(shared)
}

// NormalizeRewards 按目录顺序重排权益，并抹掉与取值形态无关的字段（卡密的规则），
// 「功能时长」排在最后、只留功能标识（去空白、转小写）与分钟数。未开放的类型被丢掉。
func NormalizeRewards(rewards []Reward) []Reward {
	out := cardkeydomain.NormalizeRewards(rewards)
	if out == nil {
		out = []Reward{}
	}
	for _, reward := range rewards {
		if reward.Type != RewardFeatureMinutes {
			continue
		}
		out = append(out, Reward{
			Type:    RewardFeatureMinutes,
			Amount:  reward.Amount,
			Feature: strings.ToLower(strings.TrimSpace(reward.Feature)),
		})
		break
	}
	for _, reward := range rewards {
		if reward.Type == RewardAICredit {
			out = append(out, Reward{Type: RewardAICredit, Amount: reward.Amount})
			break
		}
	}
	return out
}

// FeatureMinutesTags 场景权益里「功能时长」用到的功能标识。
func (s Scene) FeatureMinutesTags() []string {
	tags := make([]string, 0, 1)
	for _, reward := range s.Rewards {
		if reward.Type == RewardFeatureMinutes && reward.Feature != "" {
			tags = append(tags, reward.Feature)
		}
	}
	return tags
}

// NormalizeScene 清洗一个场景（去空白、权益按目录排序），不做合法性判断。
func NormalizeScene(scene Scene) Scene {
	scene.Key = strings.ToLower(strings.TrimSpace(scene.Key))
	scene.Name = strings.TrimSpace(scene.Name)
	scene.PlacementID = strings.TrimSpace(scene.PlacementID)
	scene.Rewards = NormalizeRewards(scene.Rewards)
	scene.Membership.Features = vipdomain.NormalizeFeatureTags(scene.Membership.Features)
	scene.Lifetime = normalizeLifetime(scene.Lifetime)
	return scene
}

// GrantsMembership 这个场景是否送会员（会员套餐或会员天数）。
func (s Scene) GrantsMembership() bool {
	for _, reward := range s.Rewards {
		if reward.Type == cardkeydomain.RewardVipPlan || reward.Type == cardkeydomain.RewardVipDays {
			return true
		}
	}
	return false
}

// GrantsVipDays 这个场景是否送「会员天数」（会员权益里的功能标识只对这一档生效）。
func (s Scene) GrantsVipDays() bool {
	for _, reward := range s.Rewards {
		if reward.Type == cardkeydomain.RewardVipDays {
			return true
		}
	}
	return false
}

// ValidateScenes 校验全部场景。每一类问题都给出能直接照着改的文案。
func ValidateScenes(scenes []Scene) error {
	if len(scenes) > MaxScenes {
		return fmt.Errorf("最多配置 %d 个场景", MaxScenes)
	}
	seen := make(map[string]bool, len(scenes))
	for index, scene := range scenes {
		label := fmt.Sprintf("第 %d 个场景", index+1)
		if !sceneKeyPattern.MatchString(scene.Key) {
			return fmt.Errorf("%s的标识需为 1–32 位小写字母、数字、下划线或短横线", label)
		}
		if seen[scene.Key] {
			return fmt.Errorf("场景标识「%s」重复", scene.Key)
		}
		seen[scene.Key] = true
		label = "场景「" + scene.Key + "」"
		if scene.Name == "" || utf8.RuneCountInString(scene.Name) > 32 {
			return fmt.Errorf("%s的名称需为 1–32 个字", label)
		}
		if !placementIDPattern.MatchString(scene.PlacementID) {
			return fmt.Errorf("%s的广告位 ID 需为 1–64 位字母或数字", label)
		}
		if scene.DailyLimit < 0 || scene.DailyLimit > MaxDailyLimit {
			return fmt.Errorf("%s的每日次数需在 0–%d 之间", label, MaxDailyLimit)
		}
		if scene.CooldownSeconds < 0 || scene.CooldownSeconds > MaxCooldownSeconds {
			return fmt.Errorf("%s的冷却时间需在 0–%d 秒之间", label, MaxCooldownSeconds)
		}
		if err := ValidateRewards(scene.Rewards); err != nil {
			return fmt.Errorf("%s：%w", label, err)
		}
		if err := validateMembership(scene); err != nil {
			return fmt.Errorf("%s：%w", label, err)
		}
		if err := validateLifetime(scene.Lifetime); err != nil {
			return fmt.Errorf("%s：%w", label, err)
		}
	}
	return nil
}

// validateMembership 会员权益的形态校验。功能标识是否在本应用的功能目录里由服务层核对。
//
// 不送「会员天数」却配了功能标识的场景直接挡下：那些标识不会落到任何人头上，
// 留着只会让运营以为「看广告送的会员能用 X」。
func validateMembership(scene Scene) error {
	features := scene.Membership.Features
	if len(features) > MaxSceneFeatures {
		return fmt.Errorf("会员权益最多 %d 个功能", MaxSceneFeatures)
	}
	for _, tag := range features {
		if !vipdomain.FeatureTagPattern.MatchString(tag) {
			return fmt.Errorf("功能标识「%s」无效", tag)
		}
	}
	if len(features) > 0 && !scene.GrantsVipDays() {
		return fmt.Errorf("会员权益里的功能只对「会员天数」生效，请先添加会员天数或清空功能")
	}
	return nil
}

// ValidProviderAppID 平台应用 ID 的形态校验（允许留空）。
func ValidProviderAppID(value string) bool {
	return providerAppPattern.MatchString(value)
}

// DescribeScene 一句话说清一个场景看完能拿到什么：权益，再加上送出的会员带什么。
//
// [featureNames] 是功能标识到展示名的映射，认不出的标识原样展示（停用的功能不会出现在
// 会员的权益里，这里也不列）。
func DescribeScene(scene Scene, planNames map[int64]string, featureNames map[string]string) string {
	summary := DescribeRewards(scene.Rewards, planNames, featureNames)
	if !scene.GrantsMembership() {
		return summary
	}
	extras := make([]string, 0, 2)
	if scene.GrantsVipDays() && len(scene.Membership.Features) > 0 {
		names := make([]string, 0, len(scene.Membership.Features))
		for _, tag := range scene.Membership.Features {
			name, ok := featureNames[tag]
			if featureNames != nil && !ok {
				continue
			}
			if name == "" {
				name = tag
			}
			names = append(names, name)
		}
		if len(names) > 0 {
			extras = append(extras, "会员可用"+strings.Join(names, "、"))
		}
	}
	if scene.Membership.AdFreeOrDefault() {
		extras = append(extras, "会员期间免广告")
	}
	if len(extras) == 0 {
		return summary
	}
	return summary + "（" + strings.Join(extras, "，") + "）"
}

// DescribeRewards 一句话说清一组权益，客户端与核销结果直接展示。
//
// 「功能时长」说成「在线听歌 60 分钟」：功能名取 [featureNames]，认不出时用标识本身。
func DescribeRewards(rewards []Reward, planNames map[int64]string, featureNames map[string]string) string {
	parts := make([]string, 0, len(rewards))
	for _, reward := range rewards {
		switch reward.Type {
		case cardkeydomain.RewardVipPlan:
			if name := planNames[reward.RefID]; name != "" {
				parts = append(parts, "会员套餐「"+name+"」")
				continue
			}
		case RewardFeatureMinutes:
			parts = append(parts, DescribeFeatureMinutes(reward, featureNames[reward.Feature]))
			continue
		case RewardAICredit:
			parts = append(parts, DescribeAICredit(reward))
			continue
		}
		parts = append(parts, cardkeydomain.DescribeReward(reward))
	}
	return strings.Join(parts, "、")
}

// DescribeFeatureMinutes 「功能时长」一项的说法：「在线听歌 60 分钟」。name 为空时用功能标识。
func DescribeFeatureMinutes(reward Reward, name string) string {
	if name == "" {
		name = reward.Feature
	}
	return fmt.Sprintf("%s %d %s", name, reward.Amount, featureMinutesSpec.Unit)
}

// DescribeAICredit 「AI 额度」一项的说法：「AI 额度 50 点」。
func DescribeAICredit(reward Reward) string {
	return fmt.Sprintf("%s %d %s", aiCreditSpec.Label, reward.Amount, aiCreditSpec.Unit)
}

// ReasonMessage 拒发原因的用户可读文案。
func ReasonMessage(status, reason string) string {
	switch status {
	case StatusGranted:
		return "奖励已发放"
	case StatusPending:
		return "正在确认观看结果，奖励稍后到账"
	}
	switch reason {
	case ReasonDisabled:
		return "激励广告暂未开放"
	case ReasonSceneUnavailable:
		return "该奖励活动已下线"
	case ReasonDailyLimit:
		return "今天的观看奖励次数已用完，明天再来"
	case ReasonSceneDailyLimit:
		return "今天这个奖励已经领满了，明天再来"
	case ReasonCooldown:
		return "领取太频繁了，请稍后再看"
	case ReasonUserMismatch:
		return "这次观看不属于当前账号"
	case ReasonClientUnverified:
		return "广告未完整播放，未获得奖励"
	case ReasonUserNotFound:
		return "账号不存在"
	}
	return "未获得奖励"
}

// CallbackSign Apex（ToBid）服务端回调的签名：sha256(securityKey + ":" + transId) 的十六进制小写。
func CallbackSign(securityKey, transID string) string {
	sum := sha256.Sum256([]byte(securityKey + ":" + transID))
	return hex.EncodeToString(sum[:])
}

// VerifyCallbackSign 常数时间比较签名，大小写不敏感。
func VerifyCallbackSign(securityKey, transID, sign string) bool {
	if securityKey == "" || transID == "" || sign == "" {
		return false
	}
	expected := CallbackSign(securityKey, transID)
	got := strings.ToLower(strings.TrimSpace(sign))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

// sceneExtraKeys 客户端放进 SDK options 里表示场景的键。
//
// Apex 把 ApexAdxRewardRequest（展示时为 show 的参数）的 options 原样作为 EXTRAINFO 回传，但文档没有约定序列化形态，
// 实测既见过 JSON 对象，也可能是 a=b&c=d。两种都认；认不出来就当没有，交给客户端上报。
var sceneExtraKeys = []string{"aegisScene", "aegis_scene", "scene"}

// SceneFromExtra 从回调的 EXTRAINFO 里取出客户端声明的场景。
func SceneFromExtra(extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return ""
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(extra), &object); err == nil {
		for _, key := range sceneExtraKeys {
			if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.ToLower(strings.TrimSpace(value))
			}
		}
		return ""
	}
	if values, err := url.ParseQuery(extra); err == nil {
		for _, key := range sceneExtraKeys {
			if value := strings.TrimSpace(values.Get(key)); value != "" {
				return strings.ToLower(value)
			}
		}
	}
	return ""
}
