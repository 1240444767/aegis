package notification

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCategoryCatalog(t *testing.T) {
	want := []string{CategorySystem, CategoryAnnouncement, CategorySecurity, CategoryMembership, CategoryReward, CategoryTicket}
	got := Categories()
	if len(got) != len(want) {
		t.Fatalf("分类数量 %d，期望 %d", len(got), len(want))
	}
	for i, spec := range got {
		if spec.Key != want[i] {
			t.Errorf("第 %d 个分类是 %s，期望 %s（顺序即展示顺序）", i, spec.Key, want[i])
		}
		if spec.Label == "" {
			t.Errorf("分类 %s 没有中文名", spec.Key)
		}
	}
	if !ValidCategory(CategoryReward) || ValidCategory("nope") || ValidCategory("") {
		t.Fatal("ValidCategory 判定不对")
	}
	if CategoryLabel(CategoryReward) != "积分与奖励" || CategoryLabel("nope") != "系统通知" {
		t.Fatal("CategoryLabel 不对")
	}
}

func TestCategoryOf(t *testing.T) {
	cases := map[string]string{
		"system":           CategorySystem,
		"":                 CategorySystem,
		"whatever":         CategorySystem,
		"announcement":     CategoryAnnouncement,
		"campaign":         CategoryAnnouncement,
		"marketing":        CategoryAnnouncement,
		"security":         CategorySecurity,
		" Security ":       CategorySecurity,
		"vip":              CategoryMembership,
		"payment":          CategoryMembership,
		"order":            CategoryMembership,
		"wallet":           CategoryMembership,
		"refund":           CategoryMembership,
		"account_deletion": CategoryMembership,
		"signin":           CategoryReward,
		"auto_sign":        CategoryReward,
		"points_store":     CategoryReward,
		"invite":           CategoryReward,
		"leaderboard":      CategoryReward,
		"lottery":          CategoryReward,
		"rewarded_ad":      CategoryReward,
		"cardkey":          CategoryReward,
		"ticket":           CategoryTicket,
		"feedback":         CategoryTicket,
	}
	for typ, want := range cases {
		if got := CategoryOf(typ); got != want {
			t.Errorf("CategoryOf(%q) = %s，期望 %s", typ, got, want)
		}
	}
	for typ, category := range TypeCategories() {
		if !ValidCategory(category) {
			t.Errorf("type %s 映射到了不存在的分类 %s", typ, category)
		}
	}
	// 写入点常量都必须显式登记
	for _, typ := range []string{TypeSystem, TypeAnnouncement, TypeSecurity, TypeAccountDeletion, TypeSignIn,
		TypeAutoSign, TypePointsStore, TypeInvite, TypeLeaderboard, TypeTicket} {
		if !TypeMapped(typ) {
			t.Errorf("type 常量 %s 没在映射表里登记", typ)
		}
	}
}

func TestPushPreference(t *testing.T) {
	if PushMutable(CategorySecurity) {
		t.Fatal("账号安全类不能关闭提醒")
	}
	for _, spec := range Categories() {
		if spec.Key != CategorySecurity && !spec.Mutable {
			t.Errorf("分类 %s 应当可关", spec.Key)
		}
	}
	prefs := map[string]bool{CategoryReward: false, CategorySecurity: false, CategoryAnnouncement: true}
	if PushEnabled(CategoryReward, prefs) {
		t.Error("关闭的分类不应提醒")
	}
	if !PushEnabled(CategorySecurity, prefs) {
		t.Error("账号安全类即使偏好里写了 false 也要提醒")
	}
	if !PushEnabled(CategorySystem, prefs) {
		t.Error("没有偏好行即开启")
	}
	if !PushEnabled(CategoryAnnouncement, prefs) {
		t.Error("显式开启应提醒")
	}
}

func TestNormalizeLevel(t *testing.T) {
	cases := map[string]string{"": LevelInfo, "INFO": LevelInfo, "success": LevelSuccess, "warn": LevelWarning,
		"warning": LevelWarning, "error": LevelError, "critical": LevelError, "danger": LevelError}
	for in, want := range cases {
		got, ok := NormalizeLevel(in)
		if !ok || got != want {
			t.Errorf("NormalizeLevel(%q) = %s,%v，期望 %s", in, got, ok, want)
		}
	}
	if _, ok := NormalizeLevel("fatal"); ok {
		t.Error("未知级别应当报错")
	}
}

func TestNormalizeAction(t *testing.T) {
	if got, msg := NormalizeAction(nil); got != nil || msg != "" {
		t.Fatal("nil 动作应视为没有动作")
	}
	if got, msg := NormalizeAction(&Action{}); got != nil || msg != "" {
		t.Fatal("空动作应视为没有动作")
	}
	got, msg := NormalizeAction(&Action{Type: " Route ", Target: " leaderboard ", Label: " 查看排行榜 "})
	if msg != "" || got == nil || got.Type != ActionRoute || got.Target != "leaderboard" || got.Label != "查看排行榜" {
		t.Fatalf("合法路由动作被拒或没整理：%+v %s", got, msg)
	}
	for _, route := range Routes() {
		if _, msg := NormalizeAction(&Action{Type: ActionRoute, Target: route.Key}); msg != "" {
			t.Errorf("白名单路由 %s 被拒：%s", route.Key, msg)
		}
		if route.Label == "" {
			t.Errorf("路由 %s 没有中文名", route.Key)
		}
	}
	if _, msg := NormalizeAction(&Action{Type: ActionURL, Target: "https://example.com/a?b=1"}); msg != "" {
		t.Fatalf("https 链接被拒：%s", msg)
	}
	bad := []Action{
		{Type: ActionRoute, Target: "settings"},
		{Type: ActionRoute, Target: ""},
		{Type: ActionURL, Target: "http://example.com"},
		{Type: ActionURL, Target: "javascript:alert(1)"},
		{Type: ActionURL, Target: "https://"},
		{Type: ActionURL, Target: "https://user:pass@example.com"},
		{Type: ActionURL, Target: "/relative"},
		{Type: ActionURL, Target: "https://example.com/" + strings.Repeat("a", ActionURLMaxLength)},
		{Type: "deeplink", Target: "voyage://x"},
		{Type: ActionRoute, Target: "points", Label: strings.Repeat("长", ActionLabelMaxRunes+1)},
	}
	for _, action := range bad {
		if _, msg := NormalizeAction(&action); msg == "" {
			t.Errorf("非法动作被放行：%+v", action)
		}
	}
}

// 可空字段为空时输出 null，客户端按「字段恒在」声明即可。
func TestItemNullableFieldsSerializeAsNull(t *testing.T) {
	raw, err := json.Marshal(Item{ID: 1, Category: CategorySystem, Status: "unread"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"readAt", "action", "image", "expiresAt"} {
		value, ok := decoded[field]
		if !ok || value != nil {
			t.Errorf("字段 %s 应当输出 null，得到 %v（存在=%v）", field, value, ok)
		}
	}
	for _, field := range []string{"category", "categoryLabel", "read", "status"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("缺少字段 %s", field)
		}
	}
	// 写操作的计数：categoryUnread 恒为对象
	raw, _ = json.Marshal(ReadAllResult{ReadAll: true, Counts: Counts{CategoryUnread: map[string]int64{}}})
	if !strings.Contains(string(raw), `"categoryUnread":{}`) || !strings.Contains(string(raw), `"unread":0`) {
		t.Fatalf("计数没有内联输出：%s", raw)
	}
}
