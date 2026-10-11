package app

import "testing"

func TestEvaluateAutoSign(t *testing.T) {
	open := func(audience string) AutoSignPolicy {
		return AutoSignPolicy{Enabled: true, Audience: audience, IncludeTrial: true}
	}
	vip := AutoSignMember{IsVIP: true}
	trial := AutoSignMember{IsVIP: true, IsTrial: true}
	lifetime := AutoSignMember{IsVIP: true, IsLifetime: true}
	withFeature := AutoSignMember{Features: []string{"auto_sign"}}

	noTrial := open(AutoSignAudienceVip)
	noTrial.IncludeTrial = false
	feature := open(AutoSignAudienceFeature)
	feature.FeatureTag = "auto_sign"

	cases := []struct {
		name   string
		policy AutoSignPolicy
		member AutoSignMember
		want   string
	}{
		{"未开放", DefaultAutoSignPolicy(), lifetime, AutoSignReasonDisabled},
		{"所有用户", open(AutoSignAudienceAll), AutoSignMember{}, ""},
		{"会员：非会员", open(AutoSignAudienceVip), AutoSignMember{}, AutoSignReasonVipRequired},
		{"会员：会员", open(AutoSignAudienceVip), vip, ""},
		{"会员：永久会员", open(AutoSignAudienceVip), lifetime, ""},
		{"会员：试用算在内", open(AutoSignAudienceVip), trial, ""},
		{"会员：试用不算", noTrial, trial, AutoSignReasonTrialExcluded},
		{"永久：限时会员", open(AutoSignAudienceLifetime), vip, AutoSignReasonLifetimeRequired},
		{"永久：永久会员", open(AutoSignAudienceLifetime), lifetime, ""},
		{"功能：没有", feature, vip, AutoSignReasonFeatureRequired},
		{"功能：有（非会员也行）", feature, withFeature, ""},
	}
	for _, tc := range cases {
		if got := EvaluateAutoSign(tc.policy, tc.member); got != tc.want {
			t.Errorf("%s：得到 %q，期望 %q", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeAutoSignPolicy(t *testing.T) {
	if _, err := NormalizeAutoSignPolicy(AutoSignPolicy{Audience: "staff"}); err == nil {
		t.Fatal("未知开放对象应当报错")
	}
	if _, err := NormalizeAutoSignPolicy(AutoSignPolicy{Audience: AutoSignAudienceFeature}); err == nil {
		t.Fatal("指定功能却没选功能应当报错")
	}
	got, err := NormalizeAutoSignPolicy(AutoSignPolicy{Enabled: true, Audience: AutoSignAudienceVip, FeatureTag: "x", IsDefault: true})
	if err != nil || got.FeatureTag != "" || got.IsDefault {
		t.Fatalf("非功能对象应当清掉功能标识与默认标记：%+v %v", got, err)
	}
	got, err = NormalizeAutoSignPolicy(AutoSignPolicy{Audience: AutoSignAudienceFeature, FeatureTag: " Auto_Sign "})
	if err != nil || got.FeatureTag != "auto_sign" {
		t.Fatalf("功能标识应当去空白转小写：%+v %v", got, err)
	}
}

func TestAutoSignPolicyRoundTrip(t *testing.T) {
	if policy := AutoSignPolicyFromSettings(nil); !policy.IsDefault || policy.Enabled {
		t.Fatalf("未配置时应当是不开放的默认策略：%+v", policy)
	}
	want := AutoSignPolicy{Enabled: true, Audience: AutoSignAudienceFeature, FeatureTag: "auto_sign"}
	got := AutoSignPolicyFromSettings(map[string]any{AutoSignSettingsKey: want.ToMap()})
	if got != want {
		t.Fatalf("存取不一致：得到 %+v，期望 %+v", got, want)
	}
	broken := AutoSignPolicyFromSettings(map[string]any{AutoSignSettingsKey: map[string]any{"enabled": true, "audience": "nobody"}})
	if broken.Enabled {
		t.Fatalf("被改坏的策略应当按不开放处理：%+v", broken)
	}
}
