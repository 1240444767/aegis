package lottery

import (
	"fmt"
	"math"

	"aegis/internal/domain/pointstore"
)

// 无套利：抽奖次数能用积分买到时，一次抽奖的期望积分回报不能超过买一次的价格。
//
// ── 出过的事 ──
//
// 积分商城把「抽奖次数 1 次」卖 10 积分，奖盘里有月度 / 季度会员。永久会员抽中会员是已拥有的，
// 按会员折算单价（每天 10 积分）折成 300 / 1240 积分，一次抽奖的期望回报一百多积分：
// 用积分买次数、抽完再买，58 个永久会员几天里刷走了近 20 万积分。
// 单看哪一边都没错 —— 会员折算按会员的价值算、商城按运营定的价卖 —— 错在两边接起来成了一个环。
//
// ── 怎么判 ──
//
// 这个人眼里的奖盘（剔除、特权加权之后的公示概率）上，一次抽奖的期望积分回报：
//
//	E = (P + O·c) / (1 − Q)
//
//	P  积分奖品的期望积分        Σ p·amount
//	O  已拥有、会被改发成积分或抽奖次数的格子的概率   Σ p
//	c  这些格子每次改发多少（积分计；改发的抽奖次数按价格折成积分）
//	Q  一次抽奖期望再得几次抽奖   Σ p·amount（抽奖次数奖品）
//
// 再得的抽奖接着抽，所以是几何级数。要求 E ≤ 价格，解出 c 的上限：
//
//	c ≤ (价格·(1 − Q) − P) / O
//
// 已拥有格子的改发（按公式折的积分、奖品单独设的改发积分、改发的抽奖次数）超过上限时改为按上限发积分，
// 上限是 0 时什么也不发。
// 价格取所有能用积分换到这个活动次数的路径里最便宜的：积分抽奖价（特权折扣后）、积分商城里抽奖次数的单价。
// 没有任何用积分换次数的路径（只有免费次数、看视频、卡密）时不封顶：积分回不到次数，构不成环。
//
// 经验、会员、功能这些不是积分，不进 E；人工发放的奖品同理。出奖策略（软保底、特权加权、匀速释放）
// 会让某一次的实际概率偏离公示概率，上限按公示概率算，是期望意义上的保证。

// NoCap 不封顶。
const NoCap int64 = -1

// Economy 一次抽奖在积分上的期望（这个人眼里的奖盘）。
type Economy struct {
	// Price 用积分换一次抽奖的最低价；0 表示积分换不到次数
	Price int64 `json:"price"`
	// Points 积分奖品的期望积分（P）
	Points float64 `json:"points"`
	// Owned 会被折成积分的格子的概率（O）
	Owned float64 `json:"owned"`
	// Redraws 一次抽奖期望再得几次抽奖（Q）
	Redraws float64 `json:"redraws"`
	// Cap 已拥有格子折算积分的上限；NoCap 为不封顶
	Cap int64 `json:"cap"`
}

// ExpectedReturn 按 c 折算时一次抽奖的期望积分回报；再得次数的期望不小于 1 时为 +Inf（抽不完）。
func (e Economy) ExpectedReturn(c int64) float64 {
	if e.Redraws >= 1 {
		return math.Inf(1)
	}
	return (e.Points + e.Owned*float64(max(c, 0))) / (1 - e.Redraws)
}

// Arbitrage 不算折算，这个奖盘本身的期望积分回报就超过了价格（运营配置的问题，控制台提示用）。
func (e Economy) Arbitrage() bool {
	return e.Price > 0 && e.ExpectedReturn(0) > float64(e.Price)
}

// EconomyOf 这个人眼里的奖盘在积分上的期望，以及已拥有格子折算积分的上限。
//
// prizes 是这个人的奖盘（PersonalPrizes、BoostPrizes 之后），概率按公示口径（Probabilities）。
func EconomyOf(rule SwapRule, coverage MemberCoverage, prizes []Prize, plans map[int64]pointstore.PlanRef, price int64) Economy {
	economy := Economy{Price: max(price, 0), Cap: NoCap}
	probabilities := Probabilities(prizes)
	uncapped := rule
	uncapped.Capped = false
	for index, prize := range prizes {
		p := probabilities[index] / 100
		if p <= 0 {
			continue
		}
		reward := NormalizeReward(prize.Reward)
		if _, owned := coverage.Owned(reward, plans); owned {
			if swapped, ok := Swap(uncapped, prize, plans); ok {
				reward = swapped
				// 有积分换次数的路径时，改发的抽奖次数与积分一样是钱：一起封顶
				if reward.Type == KindPoints || reward.Type == KindLotteryDraws && economy.Price > 0 {
					economy.Owned += p
					continue
				}
			}
		}
		switch reward.Type {
		case KindPoints:
			economy.Points += p * float64(reward.Amount)
		case KindLotteryDraws:
			economy.Redraws += p * float64(reward.Amount)
		}
	}
	if economy.Price <= 0 || economy.Owned <= 0 {
		return economy
	}
	room := float64(economy.Price)*(1-economy.Redraws) - economy.Points
	economy.Cap = int64(math.Floor(max(room, 0) / economy.Owned))
	return economy
}

// WithCap 带上折算上限的处理方式（Economy.Cap 为 NoCap 时不封顶）。
func (rule SwapRule) WithCap(economy Economy) SwapRule {
	rule.Capped, rule.Cap, rule.Price = economy.Cap >= 0, max(economy.Cap, 0), economy.Price
	return rule
}

// DrawPrice 用积分换一次这个活动的抽奖的最低价：积分抽奖价（这个人眼里的，特权折扣后），
// 与积分商城里抽奖次数的单价（storePrice，0 为商城不卖）。两者都没有时为 0。
func DrawPrice(personal Activity, storePrice int64) int64 {
	price := int64(0)
	if personal.PointsCost > 0 {
		price = personal.PointsCost
	}
	if storePrice > 0 && (price == 0 || storePrice < price) {
		price = storePrice
	}
	return price
}

// ActivityEconomy 控制台上一个活动的积分账：普通用户与永久会员各抽一次期望拿回多少积分，与能否用积分稳赚。
type ActivityEconomy struct {
	// Price 用积分换一次抽奖的最低价（积分抽奖价、积分商城单价）；0 表示积分换不到次数，不存在套利
	Price int64 `json:"price"`
	// MemberReturn 不拥有任何奖品的用户抽一次的期望积分回报；-1 表示抽一次期望再得一次以上、抽不完
	MemberReturn float64 `json:"memberReturn"`
	// LifetimeReturn 已拥有奖盘里全部会员与功能奖品的永久会员（封顶之后）抽一次的期望积分回报
	LifetimeReturn float64 `json:"lifetimeReturn"`
	// LifetimeCap 永久会员抽中已拥有的奖品时最多折多少积分；-1 为不封顶
	LifetimeCap int64 `json:"lifetimeCap"`
	// Warnings 需要运营处理的问题（奖盘本身就能用积分稳赚）
	Warnings []string `json:"warnings"`
}

// AuditEconomy 按活动的公示奖盘算积分账（不含人群特权：特权只会让价格更低、权重更偏向中奖，另行留意）。
func AuditEconomy(activity Activity, prizes []Prize, plans map[int64]pointstore.PlanRef, coverage MemberCoverage,
	storePrice int64) ActivityEconomy {
	price := DrawPrice(activity, storePrice)
	rule := RuleOf(activity, coverage.Policy)
	out := ActivityEconomy{Price: price, LifetimeCap: NoCap, Warnings: []string{}}

	member := EconomyOf(rule, MemberCoverage{Policy: coverage.Policy}, prizes, plans, price)
	out.MemberReturn = round2(member.ExpectedReturn(0))

	lifetimeBoard := PersonalPrizes(rule, coverage, prizes, plans)
	lifetime := EconomyOf(rule, coverage, lifetimeBoard, plans, price)
	out.LifetimeCap = lifetime.Cap
	cap := lifetime.Cap
	if cap < 0 {
		cap = 0
		for index := range lifetimeBoard {
			if _, owned := coverage.Owned(lifetimeBoard[index].Reward, plans); owned {
				if alt, ok := Swap(rule, lifetimeBoard[index], plans); ok && alt.Type == KindPoints {
					cap = max(cap, alt.Amount)
				}
			}
		}
	}
	out.LifetimeReturn = round2(lifetime.ExpectedReturn(cap))

	if price <= 0 {
		return out
	}
	if member.Redraws >= 1 || lifetime.Redraws >= 1 {
		out.Warnings = append(out.Warnings, "奖盘里抽奖次数奖品的期望不小于 1 次：抽一次期望再得一次以上，能一直抽下去")
		return out
	}
	if member.Arbitrage() {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"积分能换到抽奖次数（每次 %d 积分），而抽一次期望拿回 %.2f 积分：用积分买次数能稳赚。请调低积分奖品、抽奖次数奖品的概率，或提高价格",
			price, out.MemberReturn))
	} else if lifetime.Arbitrage() {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"对永久会员剔除已拥有的奖品之后，其余积分奖品的概率变大，抽一次期望拿回 %.2f 积分，超过每次 %d 积分的价格。请改用「折算为积分」，或调低积分奖品的概率",
			out.LifetimeReturn, price))
	}
	return out
}

// round2 保留两位；抽不完（+Inf，JSON 写不出来）记 -1，原因在 Warnings 里。
func round2(value float64) float64 {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return -1
	}
	return math.Round(value*100) / 100
}
