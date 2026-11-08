package vip

import (
	"slices"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
)

// 「这份权益对他是不是空的」的唯一判据。
//
// ── 为什么不能只拿永久开通的功能去比 ──
//
// 早先的判据（LifetimeIncludes）只问「套餐的每个功能都在永久开通里吗」，漏了三件事：
//
//	永久解锁   功能解锁那条线上永久拥有的功能，与永久会员一样永远不会过期；
//	          永久会员加上永久解锁的功能已经覆盖月卡，月卡却仍显示「可购买」。
//	停用功能   限时套餐里停用的功能此刻开了也拿不到（权益只在启用目录里取，见 Evaluate）；
//	          一个停用标识就让永久会员看到购买栏、抽中月卡也照发一段空会员。
//	免广告     看广告领到的永久会员可以不免广告；这时再拿一段付费会员，多出的免广告是实打实的，
//	          不该被当成空的折掉。
//
// 现在一律问 Holdings：它由会员判定的同一份事实（EvalInput）推出，与 /vip/status 不会各说各话。
// 购买、下单、在售列表、积分商城、抽奖、激励广告、卡密、邀请、排行榜、管理员发放、开通永久时折存量，
// 全部走这里。

// Holdings 一个人已经永久拥有什么。
type Holdings struct {
	Lifetime bool `json:"lifetime"`
	// LifetimeAdFree 永久那条线免广告（看广告领到的永久段可以不免）
	LifetimeAdFree bool `json:"lifetimeAdFree"`
	// LifetimeFeatures 仍生效的永久开通的功能并集（不与功能目录取交集：停用的功能他有就是有）
	LifetimeFeatures []string `json:"lifetimeFeatures"`
	// PermanentUnlocks 已永久解锁的功能（功能解锁那条线）
	PermanentUnlocks []string `json:"permanentUnlocks"`
	// Catalog 启用中的功能；CatalogKnown 为假时不按目录过滤（手工拼出来的 Holdings）
	Catalog      []string `json:"-"`
	CatalogKnown bool     `json:"-"`
	// Tiers 此刻仍生效的会员期所属的档位（同一档位既有永久又有限时的，记为永久）。按档位分级的权益
	// （如抽奖的至尊加成）只问这里，与会员判定同一份段
	Tiers []HeldTier `json:"tiers,omitempty"`
}

// HeldTier 他名下仍生效的一个档位。
type HeldTier struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Lifetime 这个档位里有永久开通
	Lifetime bool `json:"lifetime"`
}

// HoldsTier 他有没有落在这个档位的会员期；lifetimeOnly 时只认永久开通。
func (h Holdings) HoldsTier(tierID int64, lifetimeOnly bool) (HeldTier, bool) {
	for _, tier := range h.Tiers {
		if tier.ID == tierID && (tier.Lifetime || !lifetimeOnly) {
			return tier, true
		}
	}
	return HeldTier{}, false
}

// HoldingsOf 从会员判定的事实推出持有（与 Evaluate 同一份输入、同一套段规则）。
func HoldingsOf(in EvalInput, now time.Time) Holdings {
	holdings := Holdings{
		Lifetime:         in.LifetimeSince != nil,
		LifetimeFeatures: []string{},
		PermanentUnlocks: PermanentUnlockTags(in.Unlocks, now),
		Catalog:          NormalizeFeatureTags(in.FeatureCatalog),
		CatalogKnown:     true,
	}
	live := liveSegments(in.Segments, now)
	holdings.Tiers = heldTiers(live)
	if !holdings.Lifetime {
		return holdings
	}
	features := []string{}
	segments := 0
	for _, segment := range live {
		if !segment.Lifetime {
			continue
		}
		segments++
		features = append(features, segment.EffectiveFeatures()...)
		if segment.GrantsAdFree() {
			holdings.LifetimeAdFree = true
		}
	}
	// 是永久会员却找不到永久开通（老系统写进 users 的）：与 resolveAdFree 同口径按免广告
	if segments == 0 {
		holdings.LifetimeAdFree = true
	}
	holdings.LifetimeFeatures = NormalizeFeatureTags(features)
	return holdings
}

// heldTiers 仍生效的段落在哪些档位（按档位 ID 排列）。
func heldTiers(live []Segment) []HeldTier {
	out := []HeldTier{}
	for _, segment := range live {
		if segment.TierID == nil {
			continue
		}
		name := ""
		if segment.Tier != nil {
			name = segment.Tier.Name
		}
		index := slices.IndexFunc(out, func(tier HeldTier) bool { return tier.ID == *segment.TierID })
		if index < 0 {
			out = append(out, HeldTier{ID: *segment.TierID, Name: name, Lifetime: segment.Lifetime})
			continue
		}
		out[index].Lifetime = out[index].Lifetime || segment.Lifetime
		if out[index].Name == "" {
			out[index].Name = name
		}
	}
	slices.SortFunc(out, func(a, b HeldTier) int { return int(a.ID - b.ID) })
	return out
}

// Benefit 一份会员类权益：一段会员期、一个套餐、一次永久开通。
type Benefit struct {
	Features []string
	// Lifetime 永久的：没有到期，停用的功能以后恢复了也还是他的
	Lifetime bool
	// AdFree 这份会员免广告（只有看广告领的可能不免）
	AdFree bool
	// Trial 试用：永久会员一律视为已包含
	Trial bool
}

// PlanBenefit 一个套餐带来什么。套餐来自购买、兑换、发放，一律免广告。
func PlanBenefit(plan Plan) Benefit {
	return Benefit{Features: plan.Features, Lifetime: plan.Lifetime, AdFree: true, Trial: plan.IsTrial()}
}

// MembershipBenefit 一段不挂套餐的会员（会员天数、自定义发放）。
func MembershipBenefit(features []string, lifetime, adFree bool) Benefit {
	return Benefit{Features: features, Lifetime: lifetime, AdFree: adFree}
}

// GrantBenefit 一笔发放带来什么：看广告领的按 metadata 里记下的 adFree，其余一律免广告。
func GrantBenefit(grant Grant) Benefit {
	adFree := true
	if grant.PayChannel == ChannelAdReward {
		if value, ok := grant.Metadata["adFree"].(bool); ok {
			adFree = value
		}
	}
	return MembershipBenefit(grant.Features, grant.Lifetime, adFree)
}

// Gain 一份权益对他新增了什么。
type Gain struct {
	// Membership 会员身份本身：他还不是永久会员，任何会员期都有用
	Membership bool `json:"membership"`
	// Features 他还没有永久拥有的功能
	Features []string `json:"features"`
	// AdFree 永久那条线不免广告，这份会员免
	AdFree bool `json:"adFree"`
}

// Empty 什么新东西都拿不到。
func (g Gain) Empty() bool { return !g.Membership && len(g.Features) == 0 && !g.AdFree }

// owned 永久拥有的功能：永久开通里的与永久解锁的。
func (h Holdings) owned() mapset.Set[string] {
	set := mapset.NewThreadUnsafeSet(NormalizeFeatureTags(h.PermanentUnlocks)...)
	if h.Lifetime {
		set.Append(NormalizeFeatureTags(h.LifetimeFeatures)...)
	}
	return set
}

// GainOf 这份权益对他新增了什么。
//
// 限时的只算启用中的功能：停用的此刻开了也用不上，到期之前恢复的机会不值得让人再付一次钱。
// 永久的全部算：停用的功能以后恢复了也是他的。
func (h Holdings) GainOf(benefit Benefit) Gain {
	want := mapset.NewThreadUnsafeSet(NormalizeFeatureTags(benefit.Features)...)
	if !h.Lifetime {
		return Gain{Membership: true, Features: sorted(want), AdFree: benefit.AdFree}
	}
	if benefit.Trial {
		return Gain{Features: []string{}}
	}
	if !benefit.Lifetime && h.CatalogKnown {
		want = want.Intersect(mapset.NewThreadUnsafeSet(h.Catalog...))
	}
	return Gain{
		Features: sorted(want.Difference(h.owned())),
		AdFree:   benefit.AdFree && !h.LifetimeAdFree,
	}
}

// Covers 这份权益对他是空的。
func (h Holdings) Covers(benefit Benefit) bool { return h.GainOf(benefit).Empty() }

// CoversMembership 一段带这些功能、免广告的限时会员对他是不是空的（会员天数不带功能）。
func (h Holdings) CoversMembership(features []string) bool {
	return h.Covers(MembershipBenefit(features, false, true))
}

// CoversPlan 开通这个套餐对他是不是空的（购买 40378 与所有发放入口同一判据）。
func (h Holdings) CoversPlan(plan Plan) bool { return h.Covers(PlanBenefit(plan)) }

// CoversFeature 这个功能的解锁对他是不是空的：已永久解锁，或永久会员已包含。
func (h Holdings) CoversFeature(tag string) (string, bool) {
	tags := NormalizeFeatureTags([]string{tag})
	if len(tags) == 0 {
		return "", false
	}
	if slices.Contains(NormalizeFeatureTags(h.PermanentUnlocks), tags[0]) {
		return OwnedFeature, true
	}
	if h.Lifetime && slices.Contains(NormalizeFeatureTags(h.LifetimeFeatures), tags[0]) {
		return OwnedLifetime, true
	}
	return "", false
}

// CoversFeatureTime 这个功能的使用时长对他是不是空的：会员一律免扣（永久会员的余额永远用不上，
// 见 featuretime.Exempt），已永久解锁这个功能的人同理。
func (h Holdings) CoversFeatureTime(tag string) (string, bool) {
	if h.Lifetime {
		return OwnedLifetime, true
	}
	tags := NormalizeFeatureTags([]string{tag})
	if len(tags) > 0 && slices.Contains(NormalizeFeatureTags(h.PermanentUnlocks), tags[0]) {
		return OwnedFeature, true
	}
	return "", false
}

// CoversAdFree 免广告时长对他是不是空的：永久会员且永久那条线免广告。
func (h Holdings) CoversAdFree() bool { return h.Lifetime && h.LifetimeAdFree }

// Assume 假如此刻再开通一笔永久（预览：那一笔还没落账）。
func (h Holdings) Assume(benefit Benefit) Holdings {
	out := h
	out.Lifetime = true
	out.LifetimeFeatures = NormalizeFeatureTags(append(slices.Clone(h.LifetimeFeatures), benefit.Features...))
	out.LifetimeAdFree = h.LifetimeAdFree || benefit.AdFree
	return out
}

func sorted(set mapset.Set[string]) []string {
	out := set.ToSlice()
	slices.Sort(out)
	return out
}
