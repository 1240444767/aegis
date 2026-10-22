package lottery

import (
	"fmt"
	"slices"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// 自定义规则的表达式环境与编译缓存。
//
// 表达式用 expr-lang/expr，环境是强类型的 RuleEnv：写错变量名、类型对不上在保存活动时就报错，
// 不会拖到抽奖时才失败。编译结果按表达式原文缓存，抽奖热路径上只执行不编译。
//
// 变量（全部只读）：
//
//	user.*      行为画像的全部字段（registeredDays、onlineMinutes30d、signStreak、memberRate、paidAmount、
//	            level、invites、adViews30d、riskScore30d……），以及本活动里的 totalDraws、todayDraws、
//	            missStreak、winsTotal、winsToday、prizeWins、prizeWinsToday，行为分 score（0–1）
//	draw.*      source（free / bonus / points）、hour（0–23，应用时区）、weekday（0 = 周日）、
//	            deviceAccounts、ipAccounts
//	prize.*     id、kind、position、amount、weight、probability（公示概率，百分比）、limited、remaining
//	activity.*  progress（0–1，无结束时间为 -1）、daysLeft、pointsIssuedToday
//
// 例：
//
//	user.totalDraws == 0                                新人首抽
//	user.signStreak >= 7 && prize.kind == "points"     连签一周积分奖加成
//	user.memberRate < 0.1 && user.registeredDays > 90  老用户但很少开会员
//	draw.hour >= 20 && draw.hour < 23                  晚间时段
//	activity.progress > 0.9                            活动收尾放量

// RuleUser 规则里的 user。
type RuleUser struct {
	BehaviorProfile
	TotalDraws     int     `expr:"totalDraws"`
	TodayDraws     int     `expr:"todayDraws"`
	MissStreak     int     `expr:"missStreak"`
	WinsTotal      int     `expr:"winsTotal"`
	WinsToday      int     `expr:"winsToday"`
	PrizeWins      int     `expr:"prizeWins"`
	PrizeWinsToday int     `expr:"prizeWinsToday"`
	Score          float64 `expr:"score"`
}

// RuleDraw 规则里的 draw。
type RuleDraw struct {
	Source         string `expr:"source"`
	Hour           int    `expr:"hour"`
	Weekday        int    `expr:"weekday"`
	DeviceAccounts int    `expr:"deviceAccounts"`
	IPAccounts     int    `expr:"ipAccounts"`
}

// RulePrize 规则里的 prize。
type RulePrize struct {
	ID          int64   `expr:"id"`
	Kind        string  `expr:"kind"`
	Position    int     `expr:"position"`
	Amount      int64   `expr:"amount"`
	Weight      int     `expr:"weight"`
	Probability float64 `expr:"probability"`
	Limited     bool    `expr:"limited"`
	Remaining   int64   `expr:"remaining"`
}

// RuleActivity 规则里的 activity。
type RuleActivity struct {
	Progress          float64 `expr:"progress"`
	DaysLeft          float64 `expr:"daysLeft"`
	PointsIssuedToday int64   `expr:"pointsIssuedToday"`
}

// RuleEnv 一条规则执行时看到的全部变量。
type RuleEnv struct {
	User     RuleUser     `expr:"user"`
	Draw     RuleDraw     `expr:"draw"`
	Prize    RulePrize    `expr:"prize"`
	Activity RuleActivity `expr:"activity"`
}

var ruleCache sync.Map // 表达式原文 → *vm.Program

func ruleOptions() []expr.Option {
	return []expr.Option{
		expr.Env(RuleEnv{}),
		expr.AsBool(),
		expr.DisableBuiltin("fetch"),
	}
}

// compileRule 编译（带缓存）。空表达式表示总是生效，返回 nil。
func compileRule(source string) (*vm.Program, error) {
	if source == "" {
		return nil, nil
	}
	if cached, ok := ruleCache.Load(source); ok {
		return cached.(*vm.Program), nil
	}
	program, err := expr.Compile(source, ruleOptions()...)
	if err != nil {
		return nil, err
	}
	ruleCache.Store(source, program)
	return program, nil
}

type compiledRule struct {
	StrategyRule
	index   int
	program *vm.Program
}

// compiledRules 取出启用的规则并编译；编译失败的规则跳过（保存时已校验，这里只防历史数据）。
func compiledRules(rules []StrategyRule) []compiledRule {
	out := make([]compiledRule, 0, len(rules))
	for index, rule := range rules {
		if rule.Disabled {
			continue
		}
		program, err := compileRule(rule.When)
		if err != nil {
			continue
		}
		out = append(out, compiledRule{StrategyRule: rule, index: index, program: program})
	}
	return out
}

func (r compiledRule) applies(prize Prize) bool {
	if len(r.Kinds) > 0 && !slices.Contains(r.Kinds, prize.Kind) {
		return false
	}
	if len(r.Positions) > 0 && !slices.Contains(r.Positions, prize.Position) {
		return false
	}
	return true
}

func (r compiledRule) eval(env RuleEnv) (bool, error) {
	if r.program == nil {
		return true, nil
	}
	out, err := expr.Run(r.program, env)
	if err != nil {
		return false, err
	}
	hit, _ := out.(bool)
	return hit, nil
}

func (r compiledRule) label() string {
	if r.Name != "" {
		return r.Name
	}
	return fmt.Sprintf("规则 %d", r.index+1)
}

func ruleEnv(state DrawState, prize Prize, probability, progress, daysLeft, score float64) RuleEnv {
	remaining := int64(-1)
	if prize.Quantity >= 0 {
		remaining = max(prize.Quantity-prize.Used, 0)
	}
	return RuleEnv{
		User: RuleUser{
			BehaviorProfile: state.Profile,
			TotalDraws:      state.TotalDraws,
			TodayDraws:      state.TodayDraws,
			MissStreak:      state.MissStreak,
			WinsTotal:       state.WinsTotal,
			WinsToday:       state.WinsToday,
			PrizeWins:       state.PrizeWins[prize.ID],
			PrizeWinsToday:  state.PrizeWinsToday[prize.ID],
			Score:           score,
		},
		Draw: RuleDraw{
			Source:         state.Source,
			Hour:           state.Now.Hour(),
			Weekday:        int(state.Now.Weekday()),
			DeviceAccounts: state.DeviceAccounts,
			IPAccounts:     state.IPAccounts,
		},
		Prize: RulePrize{
			ID: prize.ID, Kind: prize.Kind, Position: prize.Position, Amount: prize.Reward.Amount,
			Weight: prize.Weight, Probability: probability, Limited: prize.Quantity >= 0, Remaining: remaining,
		},
		Activity: RuleActivity{Progress: progress, DaysLeft: daysLeft, PointsIssuedToday: state.PointsIssuedToday},
	}
}

// RuleVariable 控制台的变量提示。
type RuleVariable struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

// RuleVariables 规则里能用的变量（控制台编辑器的提示与补全）。
func RuleVariables() []RuleVariable {
	vars := make([]RuleVariable, 0, len(signalCatalog)+32)
	for _, spec := range signalCatalog {
		typ := "number"
		if spec.Map == MapBool {
			typ = "bool"
		}
		vars = append(vars, RuleVariable{Name: "user." + spec.Key, Type: typ, Label: spec.Label})
	}
	vars = append(vars,
		RuleVariable{"user.signedToday", "bool", "今天已签到"},
		RuleVariable{"user.memberDays", "number", "累计会员天数"},
		RuleVariable{"user.experience", "number", "经验"},
		RuleVariable{"user.integral", "number", "积分余额"},
		RuleVariable{"user.devices", "number", "登录过的设备数"},
		RuleVariable{"user.totalDraws", "number", "本活动累计抽奖次数"},
		RuleVariable{"user.todayDraws", "number", "本活动今天抽奖次数"},
		RuleVariable{"user.missStreak", "number", "连续未中次数"},
		RuleVariable{"user.winsTotal", "number", "本活动累计中奖次数"},
		RuleVariable{"user.winsToday", "number", "本活动今天中奖次数"},
		RuleVariable{"user.prizeWins", "number", "抽中过这个奖品几次"},
		RuleVariable{"user.prizeWinsToday", "number", "今天抽中过这个奖品几次"},
		RuleVariable{"user.score", "number", "行为分（0–1）"},
		RuleVariable{"draw.source", "string", "次数来源：free / bonus / points"},
		RuleVariable{"draw.hour", "number", "小时（0–23）"},
		RuleVariable{"draw.weekday", "number", "星期（0 为周日）"},
		RuleVariable{"draw.deviceAccounts", "number", "今天同设备抽奖账号数"},
		RuleVariable{"draw.ipAccounts", "number", "今天同网络抽奖账号数"},
		RuleVariable{"prize.kind", "string", "奖品类型"},
		RuleVariable{"prize.position", "number", "奖品位置"},
		RuleVariable{"prize.amount", "number", "奖品数量（积分数、天数等）"},
		RuleVariable{"prize.weight", "number", "基础权重"},
		RuleVariable{"prize.probability", "number", "公示概率（%）"},
		RuleVariable{"prize.limited", "bool", "是否限量"},
		RuleVariable{"prize.remaining", "number", "剩余库存（不限为 -1）"},
		RuleVariable{"activity.progress", "number", "活动进度（0–1）"},
		RuleVariable{"activity.daysLeft", "number", "活动剩余天数"},
		RuleVariable{"activity.pointsIssuedToday", "number", "今天已发积分"},
	)
	return vars
}
