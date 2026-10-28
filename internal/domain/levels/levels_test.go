package levels

import (
	"encoding/json"
	"strings"
	"testing"

	"aegis/internal/domain/itemgrant"
	lotterydomain "aegis/internal/domain/lottery"
)

func mustCompile(t *testing.T, graph Graph) *Scheme {
	t.Helper()
	scheme, issues := Compile(graph)
	if scheme == nil {
		t.Fatalf("compile failed: %+v", issues)
	}
	return scheme
}

func issueMessages(issues []Issue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Level+":"+issue.Message)
	}
	return strings.Join(parts, " | ")
}

// legacyRows 旧版全站等级表（迁移 000005）的前几级。
var legacyRows = []LegacyLevel{
	{Level: 1, Name: "新手", Threshold: 0, Multiplier: 1, Icon: "seedling", Color: "#94a3b8"},
	{Level: 2, Name: "见习", Threshold: 120, Multiplier: 1, Icon: "spark", Color: "#64748b"},
	{Level: 3, Name: "学徒", Threshold: 300, Multiplier: 1.02, Icon: "feather", Color: "#0f766e"},
	{Level: 4, Name: "进阶", Threshold: 600, Multiplier: 1.04, Icon: "trail", Color: "#0d9488"},
}

func TestDefaultGraphMatchesLegacyTable(t *testing.T) {
	scheme := mustCompile(t, DefaultGraph(legacyRows))
	if scheme.MaxLevel() != 4 {
		t.Fatalf("levels = %d", scheme.MaxLevel())
	}
	cases := []struct {
		exp   int64
		level int
		mult  float64
	}{{0, 1, 1}, {119, 1, 1}, {120, 2, 1}, {299, 2, 1}, {300, 3, 1.02}, {10_000, 4, 1.04}}
	for _, c := range cases {
		level, _, _ := scheme.Resolve(c.exp, 0, nil)
		if level != c.level {
			t.Errorf("exp %d → Lv.%d, want %d", c.exp, level, c.level)
		}
		if got := scheme.Multiplier(level, PrivilegeExpBoost); got != c.mult {
			t.Errorf("exp %d multiplier %v, want %v", c.exp, got, c.mult)
		}
	}
	if icon := scheme.At(1).Icon; icon != "sprout" {
		t.Errorf("legacy icon not mapped: %q", icon)
	}
}

func TestEmptyLegacyGivesStarter(t *testing.T) {
	scheme := mustCompile(t, DefaultGraph(nil))
	if scheme.MaxLevel() != 3 {
		t.Fatalf("starter levels = %d", scheme.MaxLevel())
	}
}

func TestTemplatesCompile(t *testing.T) {
	for _, template := range Templates() {
		scheme, issues := Compile(template.Graph)
		if scheme == nil || HasErrors(issues) {
			t.Fatalf("template %s: %s", template.Key, issueMessages(issues))
		}
	}
}

// chain 搭一条主干：体系 → 各级（gateBefore 指定在哪一级之前插晋级条件）。
func chain(thresholds []int64, gateBefore int, gateExpr string) Graph {
	b := &builder{}
	prev := b.node(NodeScheme, 0, 0, SchemeConfig{NodeMeta: NodeMeta{Label: "测试"}})
	for i, threshold := range thresholds {
		if gateBefore == i+1 {
			gate := b.node(NodeGate, 0, 0, GateConfig{Mode: itemgrant.MatchExpr, Expr: gateExpr, Hint: "需要开通会员"})
			b.link(prev, HandleNext, gate)
			prev = gate
		}
		level := b.node(NodeLevel, 0, 0, LevelConfig{NodeMeta: NodeMeta{Label: "L" + string(rune('A'+i))}, Threshold: threshold})
		b.link(prev, HandleNext, level)
		prev = level
	}
	return b.graph
}

func TestGateStopsPromotion(t *testing.T) {
	scheme := mustCompile(t, chain([]int64{0, 100, 200, 300}, 3, "user.member"))
	member := func(ok bool) GateCheck {
		return func(level Level) (bool, error) { return level.Gate.Pass(lotterydomain.BehaviorProfile{Member: ok}) }
	}
	level, blocked, _ := scheme.Resolve(1000, 0, member(false))
	if level != 2 || blocked == nil || blocked.Level != 3 {
		t.Fatalf("non-member: Lv.%d blocked=%v", level, blocked)
	}
	level, blocked, _ = scheme.Resolve(1000, 0, member(true))
	if level != 4 || blocked != nil {
		t.Fatalf("member: Lv.%d blocked=%v", level, blocked)
	}
	// 已经过了门（保底在门之后）就不再判定
	level, _, _ = scheme.Resolve(1000, 3, member(false))
	if level != 4 {
		t.Fatalf("floor past gate: Lv.%d", level)
	}
	// 没有判定器时停在门前
	level, _, _ = scheme.Resolve(1000, 0, nil)
	if level != 2 {
		t.Fatalf("no checker: Lv.%d", level)
	}
}

func TestFloorNeverDropsButClampsToMax(t *testing.T) {
	scheme := mustCompile(t, chain([]int64{0, 100, 200}, 0, ""))
	if level, _, _ := scheme.Resolve(0, 3, nil); level != 3 {
		t.Fatalf("floor kept: Lv.%d", level)
	}
	if level, _, _ := scheme.Resolve(0, 9, nil); level != 3 {
		t.Fatalf("floor clamped: Lv.%d", level)
	}
	state := scheme.StateOf(2, 50)
	if state.ExpInCurrentLevel != 0 || state.Progress != 0 || state.ExpToNextLevel != 150 {
		t.Fatalf("state below threshold: %+v", state)
	}
	if state := scheme.StateOf(3, 999); !state.IsMaxLevel || state.Progress != 100 {
		t.Fatalf("max state: %+v", state)
	}
}

func TestCompileErrors(t *testing.T) {
	cases := map[string]Graph{
		"first threshold": chain([]int64{10, 100}, 0, ""),
		"not increasing":  chain([]int64{0, 100, 100}, 0, ""),
		"gate first":      chain([]int64{0, 100}, 1, "user.member"),
		"bad expr":        chain([]int64{0, 100}, 2, "user.nope +"),
		"no scheme":       {Nodes: []Node{{ID: "l", Type: NodeLevel, Data: json.RawMessage(`{"label":"a","threshold":0}`)}}},
	}
	for name, graph := range cases {
		if scheme, issues := Compile(graph); scheme != nil || !HasErrors(issues) {
			t.Errorf("%s: expected errors, got %s", name, issueMessages(issues))
		}
	}
}

func TestBranchingSpineRejected(t *testing.T) {
	graph := chain([]int64{0, 100}, 0, "")
	raw, _ := json.Marshal(LevelConfig{NodeMeta: NodeMeta{Label: "分叉"}, Threshold: 500})
	graph.Nodes = append(graph.Nodes, Node{ID: "fork", Type: NodeLevel, Data: raw})
	graph.Edges = append(graph.Edges, Edge{ID: "e-fork", Source: graph.Nodes[1].ID, SourceHandle: HandleNext, Target: "fork"})
	if scheme, issues := Compile(graph); scheme != nil {
		t.Fatalf("fork should fail: %s", issueMessages(issues))
	}
}

func attach(graph *Graph, levelIndex int, nodeType string, data any) string {
	raw, _ := json.Marshal(data)
	id := nodeType + "-x" + string(rune('a'+len(graph.Nodes)))
	graph.Nodes = append(graph.Nodes, Node{ID: id, Type: nodeType, Data: raw})
	// 主干上第 levelIndex 个等级节点
	count := 0
	for _, node := range graph.Nodes {
		if node.Type == NodeLevel {
			count++
			if count == levelIndex {
				graph.Edges = append(graph.Edges, Edge{ID: node.ID + "->" + id, Source: node.ID, SourceHandle: HandleAttach, Target: id})
			}
		}
	}
	return id
}

func TestPrivilegesInheritAndOverride(t *testing.T) {
	graph := chain([]int64{0, 100, 200, 300}, 0, "")
	attach(&graph, 2, NodePrivilege, PrivilegeConfig{Kind: PrivilegeExpBoost, Value: 1.2})
	attach(&graph, 3, NodePrivilege, PrivilegeConfig{Kind: PrivilegeExpBoost, Value: 1.5})
	attach(&graph, 2, NodePrivilege, PrivilegeConfig{Kind: PrivilegeCustom, Key: "downloads", Value: 3})
	attach(&graph, 4, NodePrivilege, PrivilegeConfig{Kind: PrivilegeCustom, Key: "downloads", Value: 10})
	attach(&graph, 3, NodePrivilege, PrivilegeConfig{NodeMeta: NodeMeta{Label: "银色边框"}, Kind: PrivilegeDisplay, Scope: ScopeOnly})
	scheme := mustCompile(t, graph)

	if got := scheme.Multiplier(1, PrivilegeExpBoost); got != 1 {
		t.Errorf("Lv1 boost %v", got)
	}
	if got := scheme.Multiplier(2, PrivilegeExpBoost); got != 1.2 {
		t.Errorf("Lv2 boost %v", got)
	}
	if got := scheme.Multiplier(4, PrivilegeExpBoost); got != 1.5 {
		t.Errorf("Lv4 boost %v", got)
	}
	find := func(level int, kind, key string) *Privilege {
		for _, p := range scheme.Effective(level) {
			if p.Kind == kind && p.Key == key {
				return &p
			}
		}
		return nil
	}
	if p := find(3, PrivilegeCustom, "downloads"); p == nil || p.Value != 3 {
		t.Errorf("Lv3 downloads = %+v", p)
	}
	if p := find(4, PrivilegeCustom, "downloads"); p == nil || p.Value != 10 {
		t.Errorf("Lv4 downloads = %+v", p)
	}
	if find(3, PrivilegeDisplay, "") == nil || find(4, PrivilegeDisplay, "") != nil {
		t.Errorf("scope only: Lv3 %v Lv4 %v", scheme.Effective(3), scheme.Effective(4))
	}
}

func TestRewardsBetweenAndValidation(t *testing.T) {
	graph := chain([]int64{0, 100, 200}, 0, "")
	first := attach(&graph, 1, NodeReward, RewardConfig{Items: []itemgrant.Item{{Type: itemgrant.ItemPoints, Amount: 10}}})
	attach(&graph, 3, NodeReward, RewardConfig{Claim: ClaimManual, Items: []itemgrant.Item{{Type: itemgrant.ItemLotteryDraws, Amount: 2, AmountExpr: "user.level"}}})
	scheme := mustCompile(t, graph)
	if rewards := scheme.RewardsBetween(0, 3); len(rewards) != 2 || rewards[0].Reward.NodeID != first || rewards[0].Level != 1 {
		t.Fatalf("rewards 0→3: %+v", rewards)
	}
	if rewards := scheme.RewardsBetween(1, 3); len(rewards) != 1 || rewards[0].Claim != ClaimManual || rewards[0].Items[0].AmountExpr != "" {
		t.Fatalf("rewards 1→3: %+v", rewards)
	}
	if key := scheme.RewardsBetween(0, 1)[0].Key(1); key != "1:"+first {
		t.Fatalf("reward key %s", key)
	}

	bad := chain([]int64{0, 100}, 0, "")
	attach(&bad, 1, NodeReward, RewardConfig{Items: nil})
	if scheme, _ := Compile(bad); scheme != nil {
		t.Fatalf("empty reward should fail")
	}
	shared := chain([]int64{0, 100}, 0, "")
	id := attach(&shared, 1, NodePrivilege, PrivilegeConfig{Kind: PrivilegeExpBoost, Value: 2})
	shared.Edges = append(shared.Edges, Edge{ID: "dup", Source: shared.Nodes[2].ID, SourceHandle: HandleAttach, Target: id})
	if scheme, _ := Compile(shared); scheme != nil {
		t.Fatalf("attachment on two levels should fail")
	}
}

func TestGateFieldsExcludeLevelAndExperience(t *testing.T) {
	for _, field := range GateFields() {
		if field.Key == "user.level" || field.Key == "user.experience" || !strings.HasPrefix(field.Key, "user.") {
			t.Fatalf("gate field %s should not be offered", field.Key)
		}
	}
	graph := chain([]int64{0, 100}, 2, "")
	for i, node := range graph.Nodes {
		if node.Type == NodeGate {
			raw, _ := json.Marshal(GateConfig{Mode: itemgrant.MatchAll, Rules: []itemgrant.ConditionRule{{Field: "user.level", Op: itemgrant.OpGte, Value: []float64{3}}}})
			graph.Nodes[i].Data = raw
		}
	}
	if scheme, _ := Compile(graph); scheme != nil {
		t.Fatalf("gate on user.level should fail")
	}
}
