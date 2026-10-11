package service

import (
	"errors"
	"reflect"
	"testing"

	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
)

// 找回密码要过哪几项验证，由账号的绑定情况与发起方式决定。这里把每一种组合钉死：
// 规则松一点就是「拿到一个邮箱就能拿走账号」，紧一点就是「本人也找不回来」。
func TestRecoveryRequirementsByBinding(t *testing.T) {
	email := redisrepo.PasswordRecoveryChannel{Type: recoveryFactorEmail}
	phone := redisrepo.PasswordRecoveryChannel{Type: recoveryFactorPhone}
	type reqs = [][]string

	cases := []struct {
		name     string
		mode     string
		channels []redisrepo.PasswordRecoveryChannel
		totp     bool
		want     reqs
		wantCode int
	}{
		{name: "未登录：只有邮箱", mode: passwordRecoveryModeRecover,
			channels: []redisrepo.PasswordRecoveryChannel{email}, want: reqs{{"email"}}},
		{name: "未登录：只有手机", mode: passwordRecoveryModeRecover,
			channels: []redisrepo.PasswordRecoveryChannel{phone}, want: reqs{{"phone"}}},
		// 单一渠道失守不足以拿走账号：两项都绑了就两项都验
		{name: "未登录：邮箱与手机", mode: passwordRecoveryModeRecover,
			channels: []redisrepo.PasswordRecoveryChannel{email, phone}, want: reqs{{"email"}, {"phone"}}},
		{name: "未登录：邮箱 + 两步验证", mode: passwordRecoveryModeRecover,
			channels: []redisrepo.PasswordRecoveryChannel{email}, totp: true, want: reqs{{"email"}, {"totp"}}},
		// 开了两步验证时动态码已经是第二项，联系方式任选其一
		{name: "未登录：邮箱、手机 + 两步验证", mode: passwordRecoveryModeRecover,
			channels: []redisrepo.PasswordRecoveryChannel{email, phone}, totp: true,
			want: reqs{{"email", "phone"}, {"totp"}}},
		// 只有动态码不够：找回之后要有地方寄告知信，也要有一项「收得到」的证明
		{name: "未登录：只有两步验证", mode: passwordRecoveryModeRecover,
			totp: true, wantCode: errCodeRecoveryUnavailable},
		{name: "未登录：什么都没绑", mode: passwordRecoveryModeRecover, wantCode: errCodeRecoveryUnavailable},

		// 已登录：当前会话本身算一项
		{name: "已登录：邮箱与手机", mode: passwordRecoveryModeSession,
			channels: []redisrepo.PasswordRecoveryChannel{email, phone}, want: reqs{{"email", "phone"}}},
		{name: "已登录：手机 + 两步验证", mode: passwordRecoveryModeSession,
			channels: []redisrepo.PasswordRecoveryChannel{phone}, totp: true, want: reqs{{"phone"}, {"totp"}}},
		{name: "已登录：只有两步验证", mode: passwordRecoveryModeSession, totp: true, want: reqs{{"totp"}}},
		{name: "已登录：什么都没绑", mode: passwordRecoveryModeSession, wantCode: errCodeRecoveryUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := recoveryRequirements(tc.mode, tc.channels, tc.totp)
			if tc.wantCode != 0 {
				if errCode(err) != tc.wantCode {
					t.Fatalf("期望错误 %d，得到 %v", tc.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			flat := make(reqs, 0, len(got))
			for _, item := range got {
				flat = append(flat, item.Any)
			}
			if !reflect.DeepEqual(flat, tc.want) {
				t.Fatalf("要求不符：期望 %v，得到 %v", tc.want, flat)
			}
		})
	}
}

// 一项方式只能满足一项要求，而且要记到选项最少的那一项上。
func TestSatisfyRecoveryRequirementPrefersNarrowest(t *testing.T) {
	flow := &redisrepo.PasswordRecoveryFlow{Requirements: []redisrepo.PasswordRecoveryRequirement{
		{Any: []string{"email", "phone"}},
		{Any: []string{"email"}},
	}}
	satisfyRecoveryRequirement(flow, "email", "email")
	if flow.Requirements[1].By != "email" || flow.Requirements[0].By != "" {
		t.Fatalf("邮箱应记到只认邮箱的那一项上：%+v", flow.Requirements)
	}
	if recoveryFactorPending(flow, "email") != true || recoveryFlowSatisfied(flow) {
		t.Fatalf("第一项还能用邮箱满足、流程未完成：%+v", flow.Requirements)
	}
	satisfyRecoveryRequirement(flow, "phone", "phone")
	if !recoveryFlowSatisfied(flow) {
		t.Fatalf("两项都满足后流程应完成：%+v", flow.Requirements)
	}
	if recoveryFactorPending(flow, "email") {
		t.Fatal("满足之后不应再接受任何方式")
	}
}

func TestRecoveryTargetMatches(t *testing.T) {
	cases := []struct {
		channel, bound, target string
		want                   bool
	}{
		{"email", "Alice@Example.com", "alice@example.com", true},
		{"email", "alice@example.com", " alice@example.com ", true},
		{"email", "alice@example.com", "alice@example.co", false},
		{"email", "alice@example.com", "", false},
		{"phone", "13800138000", "138 0013 8000", true},
		{"phone", "13800138000", "+86 138-0013-8000", true},
		{"phone", "+8613800138000", "13800138000", true},
		{"phone", "13800138000", "13800138001", false},
	}
	for _, tc := range cases {
		if got := recoveryTargetMatches(tc.channel, tc.bound, tc.target); got != tc.want {
			t.Errorf("%s %q vs %q：期望 %v，得到 %v", tc.channel, tc.bound, tc.target, tc.want, got)
		}
	}
}

func TestMaskRecoveryAccount(t *testing.T) {
	cases := map[string]string{
		"zhangsan":          "z***n",
		"ab":                "a***",
		"13800138000":       "138****8000",
		"alice@example.com": "a***e@example.com",
		"王小明":               "王***明",
	}
	for in, want := range cases {
		if got := maskRecoveryAccount(in); got != want {
			t.Errorf("%q：期望 %q，得到 %q", in, want, got)
		}
	}
}

// 猜错才记失败；服务不可用之类不能消耗用户的尝试次数。
func TestCountsAsRecoveryFailure(t *testing.T) {
	if !countsAsRecoveryFailure(apperrors.New(40109, 401, "动态码无效")) {
		t.Error("动态码错误应计为失败")
	}
	if !countsAsRecoveryFailure(apperrors.New(errCodeRecoveryCodeInvalid, 400, "验证码错误")) {
		t.Error("验证码错误应计为失败")
	}
	if countsAsRecoveryFailure(apperrors.New(50310, 503, "短信服务暂不可用")) {
		t.Error("服务不可用不应计为失败")
	}
	if countsAsRecoveryFailure(errors.New("io")) {
		t.Error("非业务错误不应计为失败")
	}
}
