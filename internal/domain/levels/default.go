package levels

import (
	"encoding/json"
	"fmt"
	"strconv"

	"aegis/internal/domain/itemgrant"
)

// 没有配置过等级体系的应用使用默认体系：由旧版全站等级表（user_levels）原样转成画布，
// 所以重构前后同一个经验值对应的等级、名称与签到经验倍率完全一致。旧表里的「权益文字」与「奖励」
// 从来没有生效过（只是展示用的 JSON），不搬过来。

// LegacyLevel 旧版全站等级表的一行。
type LegacyLevel struct {
	Level       int
	Name        string
	Threshold   int64
	Multiplier  float64
	Icon        string
	Color       string
	Description string
}

// 画布布局：主干竖排，挂件横排在所属等级右侧。坐标是卡片左上角（与控制台的换算口径一致）。
const (
	layoutSpineX    = 0
	layoutRowGap    = 220
	layoutAttachX   = 380
	layoutAttachGap = 320
)

type builder struct {
	graph Graph
	seq   int
}

func (b *builder) node(nodeType string, x, y float64, data any) string {
	b.seq++
	id := nodeType + "-" + strconv.Itoa(b.seq)
	raw, _ := json.Marshal(data)
	b.graph.Nodes = append(b.graph.Nodes, Node{ID: id, Type: nodeType, Data: raw})
	b.graph.Nodes[len(b.graph.Nodes)-1].Position.X = x
	b.graph.Nodes[len(b.graph.Nodes)-1].Position.Y = y
	return id
}

func (b *builder) link(source, handle, target string) {
	b.graph.Edges = append(b.graph.Edges, Edge{
		ID: source + ":" + handle + "->" + target, Source: source, SourceHandle: handle, Target: target,
	})
}

// DefaultGraph 默认体系的画布。legacy 为空时给一套三级的起步体系。
func DefaultGraph(legacy []LegacyLevel) Graph {
	if len(legacy) == 0 {
		legacy = []LegacyLevel{
			{Level: 1, Name: "新手", Threshold: 0, Multiplier: 1, Icon: "sprout", Color: "#94a3b8", Description: "默认初始等级。"},
			{Level: 2, Name: "进阶", Threshold: 300, Multiplier: 1, Icon: "footprints", Color: "#0d9488"},
			{Level: 3, Name: "精英", Threshold: 1000, Multiplier: 1.1, Icon: "shield", Color: "#2563eb"},
		}
	}
	b := &builder{}
	scheme := b.node(NodeScheme, layoutSpineX, 0, SchemeConfig{
		NodeMeta: NodeMeta{Label: "成长等级"},
		Notify:   true, NotifyTitle: "恭喜升到 Lv.{level} {name}", NotifyContent: "继续保持活跃，解锁更多等级权益。{rewards}",
	})
	previous, handle := scheme, HandleNext
	lastMultiplier := 1.0
	for i, row := range legacy {
		y := float64(i+1) * layoutRowGap
		threshold := row.Threshold
		if i == 0 {
			threshold = 0
		}
		level := b.node(NodeLevel, layoutSpineX, y, LevelConfig{
			NodeMeta: NodeMeta{Label: row.Name}, Threshold: threshold, Icon: legacyIcon(row.Icon),
			Color: normalizeColor(row.Color), Description: row.Description,
		})
		b.link(previous, handle, level)
		previous, handle = level, HandleNext
		if row.Multiplier > 0 && row.Multiplier != lastMultiplier {
			privilege := b.node(NodePrivilege, layoutAttachX, y, PrivilegeConfig{
				NodeMeta: NodeMeta{Label: fmt.Sprintf("签到经验 %g 倍", row.Multiplier)},
				Kind:     PrivilegeExpBoost, Value: row.Multiplier, Icon: "zap", Scope: ScopeInherit,
			})
			b.link(level, HandleAttach, privilege)
			lastMultiplier = row.Multiplier
		}
	}
	return b.graph
}

func normalizeColor(color string) string {
	if colorPattern.MatchString(color) {
		return color
	}
	return ""
}

// TemplateGraph 控制台「从模板开始」的几套体系。
type Template struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Graph       Graph  `json:"graph"`
}

// Templates 内置模板。
func Templates() []Template {
	return []Template{templateClassic(), templateMember()}
}

// templateClassic 十级成长：倍率逐级提高，五级、十级送礼包。
func templateClassic() Template {
	names := []string{"新手", "见习", "学徒", "进阶", "精锐", "骨干", "资深", "先锋", "卓越", "大师"}
	icons := []string{"sprout", "leaf", "feather", "footprints", "shield", "award", "anchor", "compass", "mountain", "crown"}
	colors := []string{"#94a3b8", "#64748b", "#0f766e", "#0d9488", "#0891b2", "#2563eb", "#4f46e5", "#7c3aed", "#9333ea", "#db2777"}
	b := &builder{}
	scheme := b.node(NodeScheme, layoutSpineX, 0, SchemeConfig{
		NodeMeta: NodeMeta{Label: "十级成长"}, Notify: true,
		NotifyTitle: "恭喜升到 Lv.{level} {name}", NotifyContent: "{rewards}",
	})
	previous := scheme
	threshold := int64(0)
	step := int64(100)
	for i, name := range names {
		y := float64(i+1) * layoutRowGap
		level := b.node(NodeLevel, layoutSpineX, y, LevelConfig{
			NodeMeta: NodeMeta{Label: name}, Threshold: threshold, Icon: icons[i], Color: colors[i],
		})
		b.link(previous, HandleNext, level)
		previous = level
		x := float64(layoutAttachX)
		if i >= 2 {
			privilege := b.node(NodePrivilege, x, y, PrivilegeConfig{
				NodeMeta: NodeMeta{Label: fmt.Sprintf("签到经验 %g 倍", 1+float64(i-1)*0.05)},
				Kind:     PrivilegeExpBoost, Value: 1 + float64(i-1)*0.05, Icon: "zap", Scope: ScopeInherit,
			})
			b.link(level, HandleAttach, privilege)
			x += layoutAttachGap
		}
		if i == 4 || i == 9 {
			reward := b.node(NodeReward, x, y, RewardConfig{
				NodeMeta: NodeMeta{Label: name + "礼包"}, Claim: ClaimManual,
				Items: []itemgrant.Item{{Type: "points", Amount: int64(200 * (i + 1))}, {Type: "lottery_draws", Amount: int64(i/4 + 1)}},
			})
			b.link(level, HandleAttach, reward)
		}
		threshold += step
		step = step * 3 / 2
	}
	return Template{Key: "classic", Name: "十级成长", Description: "门槛逐级放大，三级起签到经验加成，五级与十级可领取礼包", Graph: b.graph}
}

// templateMember 会员晋级：高等级要求开通会员。
func templateMember() Template {
	b := &builder{}
	scheme := b.node(NodeScheme, layoutSpineX, 0, SchemeConfig{
		NodeMeta: NodeMeta{Label: "会员晋级"}, Notify: true,
		NotifyTitle: "恭喜升到 Lv.{level} {name}", NotifyContent: "{rewards}",
	})
	rows := []struct {
		name      string
		threshold int64
		icon      string
		color     string
	}{
		{"青铜", 0, "medal", "#b45309"}, {"白银", 500, "shield", "#64748b"}, {"黄金", 2000, "star", "#ca8a04"},
		{"铂金", 6000, "gem", "#0891b2"}, {"钻石", 15000, "diamond", "#7c3aed"},
	}
	previous := scheme
	y := 0.0
	for i, row := range rows {
		if i == 3 {
			y += layoutRowGap
			gate := b.node(NodeGate, layoutSpineX, y, GateConfig{
				NodeMeta: NodeMeta{Label: "开通会员"}, Mode: "all", Hint: "需要开通会员",
				Rules: []itemgrant.ConditionRule{{Field: "user.member", Op: "true"}},
			})
			b.link(previous, HandleNext, gate)
			previous = gate
		}
		y += layoutRowGap
		level := b.node(NodeLevel, layoutSpineX, y, LevelConfig{
			NodeMeta: NodeMeta{Label: row.name}, Threshold: row.threshold, Icon: row.icon, Color: row.color,
		})
		b.link(previous, HandleNext, level)
		previous = level
		if i >= 3 {
			privilege := b.node(NodePrivilege, layoutAttachX, y, PrivilegeConfig{
				NodeMeta: NodeMeta{Label: row.name + "身份标识"}, Kind: PrivilegeDisplay, Icon: "badge-check",
				Text: "昵称旁展示专属标识", Scope: ScopeOnly,
			})
			b.link(level, HandleAttach, privilege)
		}
	}
	return Template{Key: "member", Name: "会员晋级", Description: "五个段位，铂金起需要开通会员才能晋级，高段位有专属标识", Graph: b.graph}
}
