package levels

import (
	"math"
	"sort"
)

// 等级判定。纯函数：输入体系、累计经验、已达到的等级（保底）与晋级条件的判定器，输出等级与进度。
//
// 等级只升不降：已经达到的等级是保底，体系改了门槛也不会掉级（管理员的「按新体系重算并允许降级」除外）。
// 体系的级数变少时，保底截到新的最高级。

// GateCheck 判定某一级的晋级条件。只在经验够了、要跨过这道门时才会被调用（行为画像按需取）。
type GateCheck func(level Level) (bool, error)

// Resolve 判定等级：从保底的下一级开始，经验够且（有条件时）条件满足就升，遇到第一个不满足的条件停下。
// floor 为 0 表示还没有等级记录（从第一级开始）。返回的等级至少为 1。
func (s *Scheme) Resolve(experience int64, floor int, check GateCheck) (int, *Level, error) {
	if s == nil || len(s.Levels) == 0 {
		return 1, nil, nil
	}
	level := floor
	if level < 1 {
		level = 1
	}
	if level > len(s.Levels) {
		level = len(s.Levels)
	}
	for level < len(s.Levels) {
		next := s.Levels[level]
		if experience < next.Threshold {
			return level, nil, nil
		}
		if next.Gate != nil && next.Gate.program != nil {
			if check == nil {
				return level, &next, nil
			}
			ok, err := check(next)
			if err != nil {
				return level, &next, err
			}
			if !ok {
				return level, &next, nil
			}
		}
		level++
	}
	return level, nil, nil
}

// At 取第 n 级（越界时截到两端）。
func (s *Scheme) At(level int) Level {
	if s == nil || len(s.Levels) == 0 {
		return Level{Level: 1, Name: "新手"}
	}
	if level < 1 {
		level = 1
	}
	if level > len(s.Levels) {
		level = len(s.Levels)
	}
	return s.Levels[level-1]
}

// MaxLevel 最高等级。
func (s *Scheme) MaxLevel() int {
	if s == nil || len(s.Levels) == 0 {
		return 1
	}
	return len(s.Levels)
}

// State 一个人在某一级时的进度。
type State struct {
	Level             int
	Config            Level
	Next              *Level
	ExpInCurrentLevel int64
	ExpRangeForLevel  int64
	ExpToNextLevel    int64
	// Progress 0–100
	Progress   float64
	IsMaxLevel bool
}

// StateOf 进度：本级门槛到下一级门槛之间走了多少。保底等级高于经验对应的等级时（体系调高了门槛），
// 本级内的经验按 0 计，不出现负数。
func (s *Scheme) StateOf(level int, experience int64) State {
	if experience < 0 {
		experience = 0
	}
	current := s.At(level)
	state := State{Level: current.Level, Config: current, IsMaxLevel: current.Level >= s.MaxLevel()}
	state.ExpInCurrentLevel = max(experience-current.Threshold, 0)
	if state.IsMaxLevel {
		state.Progress = 100
		return state
	}
	next := s.Levels[current.Level]
	state.Next = &next
	state.ExpRangeForLevel = next.Threshold - current.Threshold
	state.ExpToNextLevel = max(next.Threshold-experience, 0)
	if state.ExpRangeForLevel > 0 {
		state.Progress = math.Round(float64(state.ExpInCurrentLevel)/float64(state.ExpRangeForLevel)*10000) / 100
	}
	state.Progress = math.Min(math.Max(state.Progress, 0), 100)
	return state
}

// Effective 某一级实际享有的权益：本级及以下「本级及以上」的权益，加上本级「仅本级」的权益。
// 倍率类与同一个键的自定义权益只取最高等级的那一条；展示权益按节点逐条保留。
func (s *Scheme) Effective(level int) []Privilege {
	if s == nil {
		return []Privilege{}
	}
	if level > len(s.Levels) {
		level = len(s.Levels)
	}
	winners := map[string]Privilege{}
	order := []string{}
	for i := 0; i < level; i++ {
		for _, privilege := range s.Levels[i].Privileges {
			if privilege.Scope == ScopeOnly && privilege.Level != level {
				continue
			}
			slot := privilege.Kind
			switch privilege.Kind {
			case PrivilegeCustom:
				slot = "custom:" + privilege.Key
			case PrivilegeDisplay:
				slot = "display:" + privilege.NodeID
			}
			if _, seen := winners[slot]; !seen {
				order = append(order, slot)
			}
			winners[slot] = privilege
		}
	}
	out := make([]Privilege, 0, len(order))
	for _, slot := range order {
		out = append(out, winners[slot])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level < out[j].Level })
	return out
}

// Multiplier 某一级某种倍率权益的取值（没有配置时为 1）。
func (s *Scheme) Multiplier(level int, kind string) float64 {
	for _, privilege := range s.Effective(level) {
		if privilege.Kind == kind && privilege.Value > 0 {
			return privilege.Value
		}
	}
	return 1
}

// RewardsBetween 从 from（不含）升到 to（含）一路经过的达成奖励。
func (s *Scheme) RewardsBetween(from, to int) []LevelReward {
	if s == nil {
		return nil
	}
	if from < 0 {
		from = 0
	}
	if to > len(s.Levels) {
		to = len(s.Levels)
	}
	var out []LevelReward
	for level := from + 1; level <= to; level++ {
		for _, reward := range s.Levels[level-1].Rewards {
			out = append(out, LevelReward{Level: level, Reward: reward})
		}
	}
	return out
}

// LevelReward 某一级上的一份达成奖励。
type LevelReward struct {
	Level int
	Reward
}

// FindReward 按节点找达成奖励（补发用）。
func (s *Scheme) FindReward(nodeID string) (LevelReward, bool) {
	if s == nil {
		return LevelReward{}, false
	}
	for _, level := range s.Levels {
		for _, reward := range level.Rewards {
			if reward.NodeID == nodeID {
				return LevelReward{Level: level.Level, Reward: reward}, true
			}
		}
	}
	return LevelReward{}, false
}

// HasGates 体系里有没有晋级条件（没有就永远不需要取行为画像）。
func (s *Scheme) HasGates() bool {
	if s == nil {
		return false
	}
	for _, level := range s.Levels {
		if level.Gate != nil && level.Gate.program != nil {
			return true
		}
	}
	return false
}
