package leaderboard

import (
	"fmt"
	"slices"
	"strings"

	"aegis/internal/domain/invite"
)

// DefaultConfig 没保存过配置时的默认值；控制台第一次打开时展示的也是它。
func DefaultConfig() Config {
	boards := make([]BoardSetting, 0, len(catalog))
	for _, spec := range catalog {
		boards = append(boards, BoardSetting{Key: spec.Key, Enabled: spec.DefaultEnabled})
	}
	return Config{
		Boards:           boards,
		TopLimit:         DefaultTopLimit,
		MinValue:         DefaultMinValue,
		Identity:         IdentityNickname,
		AllowAnonymous:   true,
		CountAdminAdjust: false,
		Rewards:          map[string]BoardRewards{},
	}
}

// Identities 展示身份的枚举，顺序即控制台的展示顺序。
func Identities() []string { return []string{IdentityNickname, IdentityMasked} }

// NormalizeConfig 清洗：榜单去重、补齐目录里没出现的榜（关闭，排在末尾）、档位按名次升序、
// 奖励按目录重排。未登记的榜单 key 原样留着，交给 ValidateConfig 报出来。
func NormalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	seen := map[string]bool{}
	boards := make([]BoardSetting, 0, len(catalog))
	for _, board := range cfg.Boards {
		key := strings.TrimSpace(board.Key)
		if seen[key] {
			continue
		}
		seen[key] = true
		boards = append(boards, BoardSetting{Key: key, Enabled: board.Enabled})
	}
	for _, spec := range catalog {
		if !seen[spec.Key] {
			boards = append(boards, BoardSetting{Key: spec.Key, Enabled: false})
		}
	}
	cfg.Boards = boards
	if cfg.TopLimit == 0 {
		cfg.TopLimit = defaults.TopLimit
	}
	cfg.Identity = strings.TrimSpace(cfg.Identity)
	if cfg.Identity == "" {
		cfg.Identity = defaults.Identity
	}
	rewards := make(map[string]BoardRewards, len(cfg.Rewards))
	for key, board := range cfg.Rewards {
		tiers := make([]Tier, 0, len(board.Tiers))
		for _, tier := range board.Tiers {
			tiers = append(tiers, Tier{RankFrom: tier.RankFrom, RankTo: tier.RankTo, Rewards: invite.NormalizeRewards(tier.Rewards)})
		}
		slices.SortStableFunc(tiers, func(a, b Tier) int { return a.RankFrom - b.RankFrom })
		rewards[strings.TrimSpace(key)] = BoardRewards{Enabled: board.Enabled, Tiers: tiers}
	}
	cfg.Rewards = rewards
	return cfg
}

// ValidateConfig 校验清洗后的配置（引用的套餐与功能由服务层核对）。
func ValidateConfig(cfg Config) error {
	for _, board := range cfg.Boards {
		if _, ok := FindBoard(board.Key); !ok {
			return fmt.Errorf("未知的榜单：%s", board.Key)
		}
	}
	switch {
	case cfg.TopLimit < MinTopLimit || cfg.TopLimit > MaxTopLimit:
		return fmt.Errorf("展示名次需要在 %d–%d 之间", MinTopLimit, MaxTopLimit)
	case cfg.MinValue < 0 || cfg.MinValue > MaxMinValue:
		return fmt.Errorf("上榜门槛需要在 0–%d 之间", MaxMinValue)
	case !slices.Contains(Identities(), cfg.Identity):
		return fmt.Errorf("未知的展示身份：%s", cfg.Identity)
	}
	keys := make([]string, 0, len(cfg.Rewards))
	for key := range cfg.Rewards {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		spec, ok := FindBoard(key)
		if !ok {
			return fmt.Errorf("奖励里有未知的榜单：%s", key)
		}
		if err := ValidateTiers(spec, cfg.Rewards[key].Tiers, cfg.TopLimit); err != nil {
			return err
		}
	}
	return nil
}

// ValidateTiers 校验一个榜的奖励档位：只有周期榜能配；rankFrom ≤ rankTo ≤ topLimit；
// 档位之间不重叠；最多 MaxTiers 档；每档 1–6 项权益（形状同邀请）。
func ValidateTiers(spec BoardSpec, tiers []Tier, topLimit int) error {
	if len(tiers) == 0 {
		return nil
	}
	if !spec.Periodic() {
		return fmt.Errorf("%s不结算，不能配置奖励", spec.Label)
	}
	if len(tiers) > MaxTiers {
		return fmt.Errorf("%s的奖励最多 %d 档", spec.Label, MaxTiers)
	}
	previousTo := 0
	for _, tier := range tiers {
		label := spec.Label + TierLabel(tier)
		switch {
		case tier.RankFrom < 1 || tier.RankTo < tier.RankFrom:
			return fmt.Errorf("%s的名次范围不合法", label)
		case tier.RankTo > topLimit:
			return fmt.Errorf("%s超出了展示名次（前 %d 名）", label, topLimit)
		case tier.RankFrom <= previousTo:
			return fmt.Errorf("%s与上一档的名次重叠", label)
		case len(tier.Rewards) == 0:
			return fmt.Errorf("%s没有配置奖励", label)
		}
		if err := invite.ValidateRewards(label+"的奖励", tier.Rewards); err != nil {
			return err
		}
		previousTo = tier.RankTo
	}
	return nil
}

// TierLabel 档位的说法：「第 1 名」「第 2–3 名」。
func TierLabel(tier Tier) string {
	if tier.RankFrom == tier.RankTo {
		return fmt.Sprintf("第 %d 名", tier.RankFrom)
	}
	return fmt.Sprintf("第 %d–%d 名", tier.RankFrom, tier.RankTo)
}

// EnabledBoards 开启的榜单，按设置里的顺序。
func (cfg Config) EnabledBoards() []BoardSpec {
	out := []BoardSpec{}
	for _, board := range cfg.Boards {
		if !board.Enabled {
			continue
		}
		if spec, ok := FindBoard(board.Key); ok {
			out = append(out, spec)
		}
	}
	return out
}

// BoardEnabled 某个榜是否开启。
func (cfg Config) BoardEnabled(key string) bool {
	for _, board := range cfg.Boards {
		if board.Key == key {
			return board.Enabled
		}
	}
	return false
}

// ActiveTiers 某个榜此刻生效的奖励档位（奖励未开启或不是周期榜时为空）。
func (cfg Config) ActiveTiers(key string) []Tier {
	spec, ok := FindBoard(key)
	if !ok || !spec.Periodic() {
		return []Tier{}
	}
	rewards, ok := cfg.Rewards[key]
	if !ok || !rewards.Enabled {
		return []Tier{}
	}
	return slices.Clone(rewards.Tiers)
}

// TierFor 名次落在哪一档（并列时按名次落档：两人并列第 3，都拿第 3 名所在档）。
func TierFor(tiers []Tier, rank int) (Tier, bool) {
	for _, tier := range tiers {
		if rank >= tier.RankFrom && rank <= tier.RankTo {
			return tier, true
		}
	}
	return Tier{}, false
}

// AllRewards 配置里出现过的全部奖励（核对引用用）。
func (cfg Config) AllRewards() []Reward {
	out := []Reward{}
	for _, board := range cfg.Rewards {
		for _, tier := range board.Tiers {
			out = append(out, tier.Rewards...)
		}
	}
	return out
}
