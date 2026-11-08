package service

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	lotterydomain "aegis/internal/domain/lottery"
	vipdomain "aegis/internal/domain/vip"
	apperrors "aegis/pkg/errors"
)

// 抽奖的应用级设置：每日运气的至尊加成（活动策略里没写 fortune.supreme 时跟随这里）。规则见 domain/lottery/supreme.go。

// LotterySettingsView 管理端的应用级设置，附上可选的会员档位与哪些活动在跟随它。
type LotterySettingsView struct {
	lotterydomain.AppSettings
	// Tiers 应用的会员档位（按档位分级时选）
	Tiers []LotterySupremeTierOption `json:"tiers"`
	// Activities 开了每日运气的活动各自的至尊加成来源：activity 活动单独设置 / app 跟随这里
	Activities []LotterySupremeActivity `json:"activities"`
}

// LotterySupremeTierOption 一个可选的会员档位。
type LotterySupremeTierOption struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Members         int64  `json:"members"`
	LifetimeMembers int64  `json:"lifetimeMembers"`
}

// LotterySupremeActivity 一个开了每日运气的活动用的是哪份至尊加成。
type LotterySupremeActivity struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Source string `json:"source"`
	// Enabled 这个活动此刻有没有生效的至尊加成
	Enabled bool `json:"enabled"`
}

// Settings 应用的抽奖设置。
func (s *LotteryService) Settings(ctx context.Context, appID int64) (*LotterySettingsView, error) {
	settings, err := s.pg.GetLotteryAppSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := &LotterySettingsView{AppSettings: settings, Tiers: []LotterySupremeTierOption{}, Activities: []LotterySupremeActivity{}}
	tiers, err := s.pg.ListVipTiers(ctx, appID)
	if err != nil {
		return nil, err
	}
	for _, tier := range tiers {
		out.Tiers = append(out.Tiers, LotterySupremeTierOption{ID: tier.ID, Name: tier.Name, Members: tier.Members,
			LifetimeMembers: tier.LifetimeMembers})
	}
	activities, err := s.pg.ListLotteryActivities(ctx, appID, false)
	if err != nil {
		return nil, err
	}
	var app *lotterydomain.Supreme
	if settings.Supreme.Enabled {
		app = &settings.Supreme
	}
	for _, activity := range activities {
		fortune := activity.Strategy.Fortune
		if fortune == nil || fortune.Spread <= 0 {
			continue
		}
		config, source := lotterydomain.EffectiveSupreme(fortune, app)
		if source == "" {
			source = lotterydomain.SupremeSourceApp
		}
		out.Activities = append(out.Activities, LotterySupremeActivity{ID: activity.ID, Name: activity.Name,
			Status: activity.Status, Source: source, Enabled: config != nil})
	}
	return out, nil
}

// SaveSettings 保存应用的至尊加成。
func (s *LotteryService) SaveSettings(ctx context.Context, appID int64, supreme lotterydomain.Supreme, operator string) (
	*LotterySettingsView, error) {
	normalized := lotterydomain.NormalizeSupreme(&supreme)
	if err := lotterydomain.ValidateSupreme(normalized); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if err := s.checkSupremeTiers(ctx, appID, normalized); err != nil {
		return nil, err
	}
	if _, err := s.pg.SaveLotteryAppSettings(ctx, appID, *normalized, operator, time.Now()); err != nil {
		return nil, err
	}
	return s.Settings(ctx, appID)
}

// checkSupremeTiers 至尊加成引用的会员档位必须属于这个应用。
func (s *LotteryService) checkSupremeTiers(ctx context.Context, appID int64, supreme *lotterydomain.Supreme) error {
	ids := supreme.TierIDs()
	if len(ids) == 0 {
		return nil
	}
	tiers, err := s.pg.ListVipTiers(ctx, appID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !slices.ContainsFunc(tiers, func(tier vipdomain.Tier) bool { return tier.ID == id }) {
			return apperrors.New(40000, http.StatusBadRequest, "至尊加成选择的会员档位 "+strconv.FormatInt(id, 10)+" 不存在")
		}
	}
	return nil
}
