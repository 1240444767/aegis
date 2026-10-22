package lottery

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"gonum.org/v1/gonum/stat"
)

// 策略模拟。
//
// 调策略最怕「改了一个系数，不知道对谁、在第几次抽奖上产生了什么影响」。模拟器让一批虚拟用户
// （同一画像）各自连续抽若干次，每一次都走与线上同一个 PlanDraw / PityChoice / Sample，
// 软保底、冷却、个人上限随每个人的抽奖进度演变；库存、全站每日限量与积分预算按所有虚拟用户共享。
// 结果给出实际中奖率与公示概率的对照、首次中奖要抽几次、连续未中的分位数、保底触发次数。
// 不写数据库、不发奖。

// 模拟的规模上界：用户数 × 每人次数不超过 MaxSimulationDraws。
const (
	MaxSimulationUsers = 5000
	MaxSimulationEach  = 1000
	MaxSimulationDraws = 200_000
)

// SimulationInput 模拟参数。
type SimulationInput struct {
	Users        int
	DrawsPerUser int
	Source       string
	Profile      BehaviorProfile
	Now          time.Time
	// IsolatedStock 为真时每个虚拟用户各自用一份完整库存（看单人体验）；默认所有人共享
	IsolatedStock bool
	// Seed 非 0 时结果可复现
	Seed uint64
}

// ValidateSimulation 校验模拟规模。
func ValidateSimulation(input SimulationInput) error {
	switch {
	case input.Users < 1 || input.Users > MaxSimulationUsers:
		return fmt.Errorf("虚拟用户数需要在 1–%d 之间", MaxSimulationUsers)
	case input.DrawsPerUser < 1 || input.DrawsPerUser > MaxSimulationEach:
		return fmt.Errorf("每人抽奖次数需要在 1–%d 之间", MaxSimulationEach)
	case input.Users*input.DrawsPerUser > MaxSimulationDraws:
		return fmt.Errorf("总抽奖次数不能超过 %d", MaxSimulationDraws)
	case input.Source != "" && !slices.Contains(Sources(), input.Source):
		return fmt.Errorf("未知的次数来源：%s", input.Source)
	}
	return nil
}

// SimulationPrize 一个奖品的模拟结果。
type SimulationPrize struct {
	PrizeID  int64  `json:"prizeId"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Wins     int    `json:"wins"`
	// Rate 实际出现比例（0–1）；BaseRate 公示概率（0–1）
	Rate     float64 `json:"rate"`
	BaseRate float64 `json:"baseRate"`
	// Blocked 有多少次被硬性资格挡掉（库存、限量、上限、风控）
	Blocked int `json:"blocked"`
}

// Distribution 一组次数的分布摘要。
type Distribution struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

// SimulationResult 模拟结果。
type SimulationResult struct {
	Users       int     `json:"users"`
	Draws       int     `json:"draws"`
	WinRate     float64 `json:"winRate"`
	BaseWinRate float64 `json:"baseWinRate"`
	PityHits    int     `json:"pityHits"`
	// SoftPityDraws 软保底生效的抽奖次数
	SoftPityDraws int `json:"softPityDraws"`
	// FirstWin 首次中奖是第几次（只统计中过的人）；NeverWon 一次都没中的人数
	FirstWin     Distribution      `json:"firstWin"`
	NeverWon     int               `json:"neverWon"`
	MissStreak   Distribution      `json:"missStreak"`
	PointsIssued int64             `json:"pointsIssued"`
	Prizes       []SimulationPrize `json:"prizes"`
	Behavior     *BehaviorScore    `json:"behavior,omitempty"`
}

type simUser struct {
	missStreak, totalDraws, wins int
	longestMiss, firstWin        int
	prizeWins                    map[int64]int
	recent                       []int64
}

// Simulate 按策略模拟。prizes 是奖盘（或某个人的个人奖盘）。
func Simulate(activity Activity, strategy Strategy, prizes []Prize, input SimulationInput) SimulationResult {
	seed := input.Seed
	if seed == 0 {
		seed = rand.Uint64()
	}
	src := rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	source := input.Source
	if source == "" {
		source = SourceFree
	}
	cooldownDraws := 0
	if strategy.Cooldown != nil {
		cooldownDraws = strategy.Cooldown.Draws
	}

	shared := clonePrizes(prizes)
	dailyUsed := map[int64]int64{}
	var pointsIssued int64
	result := SimulationResult{Users: input.Users, Prizes: make([]SimulationPrize, len(prizes))}
	base := Probabilities(prizes)
	for index, prize := range prizes {
		result.Prizes[index] = SimulationPrize{
			PrizeID: prize.ID, Position: prize.Position, Name: prize.Name, Kind: prize.Kind, BaseRate: base[index] / 100,
		}
		if prize.Winning() {
			result.BaseWinRate += base[index] / 100
		}
	}
	firstWins := make([]float64, 0, input.Users)
	longest := make([]float64, 0, input.Users)
	wins := 0

	for range input.Users {
		pool := shared
		if input.IsolatedStock {
			pool = clonePrizes(prizes)
			dailyUsed = map[int64]int64{}
			pointsIssued = 0
		}
		user := simUser{prizeWins: map[int64]int{}}
		for range input.DrawsPerUser {
			state := DrawState{
				Now: now, Source: source, MissStreak: user.missStreak, TotalDraws: user.totalDraws,
				TodayDraws: user.totalDraws, WinsTotal: user.wins, WinsToday: user.wins,
				PrizeWins: user.prizeWins, PrizeWinsToday: user.prizeWins, Recent: user.recent,
				PrizeDailyUsed: dailyUsed, PointsIssuedToday: pointsIssued, DeviceAccounts: 1, IPAccounts: 1,
				Profile: input.Profile,
			}
			plan := PlanDraw(activity, strategy, pool, state)
			if result.Behavior == nil {
				result.Behavior = plan.Behavior
			}
			if plan.SoftPity > 0 {
				result.SoftPityDraws++
			}
			for index, odds := range plan.Odds {
				if odds.Blocked != "" && pool[index].Weight > 0 {
					result.Prizes[index].Blocked++
				}
			}
			index, pity := PityChoice(activity, plan, pool, user.missStreak)
			if pity {
				result.PityHits++
			} else {
				var ok bool
				if index, ok = Sample(plan, src); !ok {
					fallback, found := FallbackPrize(pool)
					if !found {
						continue
					}
					index = slices.IndexFunc(pool, func(p Prize) bool { return p.ID == fallback.ID })
				}
			}
			prize := &pool[index]
			if prize.Quantity >= 0 {
				prize.Used++
			}
			if prize.DailyLimit > 0 {
				dailyUsed[prize.ID]++
			}
			if prize.Kind == KindPoints {
				pointsIssued += prize.Reward.Amount
				result.PointsIssued += prize.Reward.Amount
			}
			result.Prizes[index].Wins++
			result.Draws++
			user.totalDraws++
			if prize.Winning() {
				wins++
				user.wins++
				user.prizeWins[prize.ID]++
				if user.firstWin == 0 {
					user.firstWin = user.totalDraws
				}
			}
			user.missStreak = NextMissStreak(user.missStreak, *prize)
			user.longestMiss = max(user.longestMiss, user.missStreak)
			if cooldownDraws > 0 {
				id := int64(0)
				if prize.Winning() {
					id = prize.ID
				}
				user.recent = append([]int64{id}, user.recent...)
				if len(user.recent) > cooldownDraws {
					user.recent = user.recent[:cooldownDraws]
				}
			}
		}
		if user.firstWin > 0 {
			firstWins = append(firstWins, float64(user.firstWin))
		} else {
			result.NeverWon++
		}
		longest = append(longest, float64(user.longestMiss))
	}

	if result.Draws > 0 {
		result.WinRate = round6(float64(wins) / float64(result.Draws))
		for index := range result.Prizes {
			result.Prizes[index].Rate = round6(float64(result.Prizes[index].Wins) / float64(result.Draws))
		}
	}
	result.BaseWinRate = round6(result.BaseWinRate)
	result.FirstWin = describe(firstWins)
	result.MissStreak = describe(longest)
	return result
}

func clonePrizes(prizes []Prize) []Prize {
	return slices.Clone(prizes)
}

// describe 用 gonum/stat 算均值与经验分位数。
func describe(values []float64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return Distribution{
		Mean: round6(stat.Mean(sorted, nil)),
		P50:  stat.Quantile(0.5, stat.Empirical, sorted, nil),
		P90:  stat.Quantile(0.9, stat.Empirical, sorted, nil),
		P99:  stat.Quantile(0.99, stat.Empirical, sorted, nil),
		Max:  sorted[len(sorted)-1],
	}
}
