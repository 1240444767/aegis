package service

import (
	"context"
	"errors"
	"math"
	"net/http"
	"slices"
	"time"

	lotterydomain "aegis/internal/domain/lottery"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 运气控制（管理端）：看今天的运气分布、查某个人的运气、下干预（重抽 / 定档 / 偏移，按人或全站）、
// 预演干预的效果、撤销、看记录、调整运气差异。干预的合成规则见 domain/lottery/fortune_control.go。

const (
	errCodeFortuneControlNotFound = 40404
	// fortuneSampleUsers 今日分布最多按这么多个今天抽过奖的人算
	fortuneSampleUsers = 5000
)

// FortuneOverview 一个活动的运气总览。
type FortuneOverview struct {
	ActivityID int64   `json:"activityId"`
	Enabled    bool    `json:"enabled"`
	Spread     float64 `json:"spread"`
	Low        float64 `json:"low"`
	High       float64 `json:"high"`
	// Levels 各档位：分界、定档时用的 z 与系数、理论占比
	Levels   []FortuneLevelInfo `json:"levels"`
	DayStart time.Time          `json:"dayStart"`
	ResetsAt time.Time          `json:"resetsAt"`
	// Today 今天抽过奖的人（最多 fortuneSampleUsers 个）的运气分布：干预之后与干预之前
	Today FortuneDistribution `json:"today"`
	// Global 生效中与待生效的全站干预
	Global []lotterydomain.FortuneControl `json:"global"`
	Counts pgrepo.FortuneControlCounts    `json:"counts"`
	// Supreme 这个活动实际生效的至尊加成（关闭时为空）与来源（activity / app）；今日分布不含加成
	Supreme       *lotterydomain.Supreme `json:"supreme,omitempty"`
	SupremeSource string                 `json:"supremeSource,omitempty"`
	// SupremeHigh 拿到加成的人最走运时的系数（积分账按它检查）
	SupremeHigh float64 `json:"supremeHigh,omitempty"`
}

// FortuneLevelInfo 一个档位。
type FortuneLevelInfo struct {
	Level  string  `json:"level"`
	Title  string  `json:"title"`
	PinZ   float64 `json:"pinZ"`
	Factor float64 `json:"factor"`
	Share  float64 `json:"share"`
	MinZ   float64 `json:"minZ"`
}

// FortuneDistribution 一批人的运气分布。
type FortuneDistribution struct {
	Users      int            `json:"users"`
	Levels     map[string]int `json:"levels"`
	BaseLevels map[string]int `json:"baseLevels"`
	// Controlled 有干预生效的人数
	Controlled int     `json:"controlled"`
	MeanFactor float64 `json:"meanFactor"`
	BaseMean   float64 `json:"baseMeanFactor"`
}

// FortuneUserReading 一个人此刻的运气：干预前后与生效的干预。
type FortuneUserReading struct {
	UserID   int64                          `json:"userId"`
	Enabled  bool                           `json:"enabled"`
	Base     *lotterydomain.FortuneReading  `json:"base,omitempty"`
	Current  *lotterydomain.FortuneReading  `json:"current,omitempty"`
	BaseZ    float64                        `json:"baseZ"`
	Z        float64                        `json:"z"`
	Applied  []lotterydomain.FortuneControl `json:"applied"`
	Controls []lotterydomain.FortuneControl `json:"controls"`
	// Lifetime / Tiers 他是不是永久会员、名下仍生效的会员档位（至尊加成按这些判定）
	Lifetime bool                 `json:"lifetime"`
	Tiers    []vipdomain.HeldTier `json:"tiers"`
}

func (s *LotteryService) fortuneActivity(ctx context.Context, appID, activityID int64) (*lotterydomain.Activity, error) {
	activity, err := s.pg.GetLotteryActivity(ctx, appID, activityID)
	if err != nil {
		return nil, s.translate(err)
	}
	return activity, nil
}

// FortuneOverview 运气总览：开没开、差异多大、档位、今天的分布与全站干预。
func (s *LotteryService) FortuneOverview(ctx context.Context, appID, activityID int64) (*FortuneOverview, error) {
	activity, err := s.fortuneActivity(ctx, appID, activityID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	day := lotteryDayStart(now)
	out := &FortuneOverview{ActivityID: activity.ID, DayStart: day, ResetsAt: day.AddDate(0, 0, 1),
		Global: []lotterydomain.FortuneControl{}}
	fortune := activity.Strategy.Fortune
	if fortune != nil && fortune.Spread > 0 {
		out.Enabled, out.Spread = true, fortune.Spread
	}
	out.Low, out.High = lotterydomain.FortuneRange(fortune)
	out.Levels = fortuneLevels(fortune)
	if out.Counts, err = s.pg.LotteryFortuneControlCounts(ctx, activity.ID, now); err != nil {
		return nil, err
	}
	global, err := s.pg.LotteryGlobalFortuneControls(ctx, activity.ID, now)
	if err != nil {
		return nil, err
	}
	out.Global = global
	if out.Enabled {
		app, err := s.pg.LotteryAppSupreme(ctx, appID)
		if err != nil {
			return nil, err
		}
		out.Supreme, out.SupremeSource = lotterydomain.EffectiveSupreme(fortune, app)
		if out.Supreme != nil {
			out.SupremeHigh = lotterydomain.SupremeHigh(fortune, out.Supreme)
		}
	}
	if !out.Enabled {
		out.Today = emptyDistribution()
		return out, nil
	}
	users, err := s.pg.LotteryDrawersSince(ctx, activity.ID, day, fortuneSampleUsers)
	if err != nil {
		return nil, err
	}
	personal, err := s.pg.LotteryUserFortuneControls(ctx, activity.ID, users, now)
	if err != nil {
		return nil, err
	}
	out.Today = distributionOf(fortune, activity.ID, users, day, now, func(userID int64) []lotterydomain.FortuneControl {
		return append(slices.Clone(global), personal[userID]...)
	})
	return out, nil
}

// FortuneUser 一个人此刻的运气。
func (s *LotteryService) FortuneUser(ctx context.Context, appID, activityID, userID int64) (*FortuneUserReading, error) {
	activity, err := s.fortuneActivity(ctx, appID, activityID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	day := lotteryDayStart(now)
	controls, err := s.pg.LotteryFortuneControls(ctx, activity.ID, userID, now)
	if err != nil {
		return nil, err
	}
	out := &FortuneUserReading{UserID: userID, Applied: []lotterydomain.FortuneControl{}, Controls: []lotterydomain.FortuneControl{},
		Tiers: []vipdomain.HeldTier{}}
	for _, control := range controls {
		if !control.Global() {
			out.Controls = append(out.Controls, control)
		}
	}
	fortune := activity.Strategy.Fortune
	outcome := lotterydomain.ResolveFortune(fortune, activity.ID, userID, day, now, controls)
	if outcome == nil {
		return out, nil
	}
	out.Enabled = true
	out.BaseZ, out.Z, out.Applied = outcome.BaseZ, outcome.Z, outcome.Applied
	out.Base = lotterydomain.ReadFortune(fortune, activity.ID, userID, day)
	out.Current = lotterydomain.ReadFortuneControlled(fortune, activity.ID, userID, day, now, controls)
	// 至尊加成接在干预之后（Current 计入加成，Base 是什么都不计的自然运气）
	coverage, err := s.pg.LotteryMemberCoverage(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	out.Lifetime, out.Tiers = coverage.Lifetime, coverage.Tiers
	app, err := s.pg.LotteryAppSupreme(ctx, appID)
	if err != nil {
		return nil, err
	}
	config, _ := lotterydomain.EffectiveSupreme(fortune, app)
	if lifted := lotterydomain.ResolveSupreme(config, coverage.Holdings, outcome.Factor); lifted != nil {
		out.Current = lotterydomain.ApplySupremeReading(out.Current, fortune, config, lifted)
	}
	if out.Applied == nil {
		out.Applied = []lotterydomain.FortuneControl{}
	}
	return out, nil
}

// CreateFortuneControls 下干预。全站一条，指定用户每人一条。
func (s *LotteryService) CreateFortuneControls(ctx context.Context, input lotterydomain.ControlInput) (
	[]lotterydomain.FortuneControl, error) {
	activity, err := s.fortuneActivity(ctx, input.AppID, input.ActivityID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	input, z, err := lotterydomain.NormalizeControl(input, fortuneSpread(activity), now, lotteryDayStart(now))
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	controls, err := s.pg.CreateLotteryFortuneControls(ctx, input, z, now)
	if errors.Is(err, pgrepo.ErrFortuneUsersNotFound) {
		return nil, apperrors.New(40401, http.StatusNotFound, "所选用户不存在或不属于这个应用")
	}
	return controls, err
}

// PreviewFortuneControl 预演一次干预：按今天抽过奖的人算，干预之后各档位多少人（不落库）。
// 指定用户时只按这些人算。
func (s *LotteryService) PreviewFortuneControl(ctx context.Context, input lotterydomain.ControlInput) (*FortuneDistribution, error) {
	activity, err := s.fortuneActivity(ctx, input.AppID, input.ActivityID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	day := lotteryDayStart(now)
	input, z, err := lotterydomain.NormalizeControl(input, fortuneSpread(activity), now, day)
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	users := input.UserIDs
	if input.AllUsers {
		if users, err = s.pg.LotteryDrawersSince(ctx, activity.ID, day, fortuneSampleUsers); err != nil {
			return nil, err
		}
	}
	global, err := s.pg.LotteryGlobalFortuneControls(ctx, activity.ID, now)
	if err != nil {
		return nil, err
	}
	personal, err := s.pg.LotteryUserFortuneControls(ctx, activity.ID, users, now)
	if err != nil {
		return nil, err
	}
	// 预演的那条按「现在生效」算（开始时间在未来的也按生效看效果）；盐每次预演随机，结果是一次抽样
	draft := lotterydomain.FortuneControl{ID: math.MaxInt64, Kind: input.Kind, Z: z, Level: input.Level,
		Salt: "preview-" + now.Format(time.RFC3339Nano), StartsAt: now, EndsAt: input.EndsAt}
	if draft.EndsAt.Before(now.Add(time.Second)) {
		draft.EndsAt = now.Add(time.Hour)
	}
	distribution := distributionOf(activity.Strategy.Fortune, activity.ID, users, day, now, func(userID int64) []lotterydomain.FortuneControl {
		controls := append(slices.Clone(global), personal[userID]...)
		item := draft
		if !input.AllUsers {
			id := userID
			item.UserID = &id
		}
		return append(controls, item)
	})
	return &distribution, nil
}

// RevokeFortuneControls 撤销干预；all 为真时撤销这个活动所有生效中与待生效的干预。
func (s *LotteryService) RevokeFortuneControls(ctx context.Context, appID, activityID int64, ids []int64, all bool,
	operator string) (int64, error) {
	if _, err := s.fortuneActivity(ctx, appID, activityID); err != nil {
		return 0, err
	}
	if !all && len(ids) == 0 {
		return 0, apperrors.New(40000, http.StatusBadRequest, "请选择要撤销的干预")
	}
	count, err := s.pg.RevokeLotteryFortuneControls(ctx, appID, activityID, ids, all, operator, time.Now())
	if errors.Is(err, pgrepo.ErrFortuneControlNotFound) {
		return 0, apperrors.New(errCodeFortuneControlNotFound, http.StatusNotFound, "干预不存在或已撤销")
	}
	return count, err
}

// ListFortuneControls 干预记录。
func (s *LotteryService) ListFortuneControls(ctx context.Context, query pgrepo.FortuneControlQuery) (
	*lotterydomain.Page[lotterydomain.FortuneControl], error) {
	if _, err := s.fortuneActivity(ctx, query.AppID, query.ActivityID); err != nil {
		return nil, err
	}
	return s.pg.ListLotteryFortuneControls(ctx, query, time.Now())
}

// SetFortuneSpread 调整运气差异（0 关闭每日运气）。只改活动策略里的这一项，其余原样保存（照常过保存校验）。
func (s *LotteryService) SetFortuneSpread(ctx context.Context, appID, activityID int64, spread float64, operator string) (
	*FortuneOverview, error) {
	if spread < 0 || spread > lotterydomain.MaxFortuneSpread || math.IsNaN(spread) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "运气差异需要在 0–1 之间")
	}
	current, err := s.GetActivity(ctx, appID, activityID)
	if err != nil {
		return nil, err
	}
	input := activityInputOf(current)
	input.Operator = operator
	if spread == 0 {
		input.Strategy.Fortune = nil
	} else {
		fortune := &lotterydomain.Fortune{Spread: math.Round(spread*1000) / 1000}
		// 活动单独设置的至尊加成原样留着
		if previous := current.Strategy.Fortune; previous != nil {
			fortune.Supreme = previous.Supreme
		}
		input.Strategy.Fortune = fortune
	}
	if _, err := s.UpdateActivity(ctx, activityID, input); err != nil {
		return nil, err
	}
	return s.FortuneOverview(ctx, appID, activityID)
}

func fortuneSpread(activity *lotterydomain.Activity) float64 {
	if activity.Strategy.Fortune == nil {
		return 0
	}
	return activity.Strategy.Fortune.Spread
}

func fortuneLevels(fortune *lotterydomain.Fortune) []FortuneLevelInfo {
	shares := lotterydomain.TheoreticalLevelShares()
	spread := 0.0
	if fortune != nil {
		spread = fortune.Spread
	}
	titles := map[string]string{
		lotterydomain.FortuneBurst: "运气大爆发", lotterydomain.FortuneLucky: "鸿运当头",
		lotterydomain.FortuneSteady: "运势平稳", lotterydomain.FortuneQuiet: "手气平淡",
	}
	minZ := map[string]float64{
		lotterydomain.FortuneBurst: 1.5, lotterydomain.FortuneLucky: 0.5,
		lotterydomain.FortuneSteady: -0.5, lotterydomain.FortuneQuiet: -2,
	}
	out := make([]FortuneLevelInfo, 0, 4)
	for _, level := range lotterydomain.FortuneLevels() {
		z := lotterydomain.PinZ(level)
		factor := 1.0
		if spread > 0 {
			factor = math.Round(math.Exp(spread*z-spread*spread/2)*1e4) / 1e4
		}
		out = append(out, FortuneLevelInfo{Level: level, Title: titles[level], PinZ: z, Factor: factor,
			Share: math.Round(shares[level]*1e4) / 1e4, MinZ: minZ[level]})
	}
	return out
}

func emptyDistribution() FortuneDistribution {
	return FortuneDistribution{Levels: lotterydomain.LevelCounts(nil), BaseLevels: lotterydomain.LevelCounts(nil), MeanFactor: 1, BaseMean: 1}
}

// distributionOf 一批人此刻的运气分布；controlsOf 给出每个人相关的干预（全站与本人的）。
func distributionOf(fortune *lotterydomain.Fortune, activityID int64, users []int64, day, now time.Time,
	controlsOf func(userID int64) []lotterydomain.FortuneControl) FortuneDistribution {
	out := emptyDistribution()
	if fortune == nil || fortune.Spread <= 0 || len(users) == 0 {
		return out
	}
	zs := make([]float64, 0, len(users))
	base := make([]float64, 0, len(users))
	var sum, baseSum float64
	for _, userID := range users {
		outcome := lotterydomain.ResolveFortune(fortune, activityID, userID, day, now, controlsOf(userID))
		if outcome == nil {
			continue
		}
		zs = append(zs, outcome.Z)
		base = append(base, outcome.BaseZ)
		sum += outcome.Factor
		baseSum += math.Exp(fortune.Spread*outcome.BaseZ - fortune.Spread*fortune.Spread/2)
		if outcome.Controlled() {
			out.Controlled++
		}
	}
	out.Users = len(zs)
	out.Levels = lotterydomain.LevelCounts(zs)
	out.BaseLevels = lotterydomain.LevelCounts(base)
	if len(zs) > 0 {
		out.MeanFactor = math.Round(sum/float64(len(zs))*1e4) / 1e4
		out.BaseMean = math.Round(baseSum/float64(len(zs))*1e4) / 1e4
	}
	return out
}
