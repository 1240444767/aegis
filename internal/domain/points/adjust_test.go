package points

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestApplyDeltaBoundaries(t *testing.T) {
	cases := []struct {
		name          string
		before, delta int64
		want          int64
		err           error
	}{
		{"普通增加", 100, 50, 150, nil},
		{"普通扣减", 100, -40, 60, nil},
		{"扣到恰好为零", 100, -100, 0, nil},
		{"扣成负数", 100, -101, 0, ErrBalanceInsufficient},
		{"加到恰好上限", MaxBalance - 10, 10, MaxBalance, nil},
		{"超过上限一", MaxBalance - 10, 11, 0, ErrBalanceOverflow},
		// 旧实现 before+delta 在这里回绕成负数，被误报为余额不足
		{"int64 溢出", math.MaxInt64 - 1, 10, 0, ErrBalanceOverflow},
		{"最大正增量", 0, math.MaxInt64, 0, ErrBalanceOverflow},
		// -MinInt64 会溢出，必须在比较里避开取反
		{"最小负增量", 100, math.MinInt64, 0, ErrBalanceInsufficient},
		{"存量负余额不能再扣", -5, -1, 0, ErrBalanceInsufficient},
		{"存量负余额可以加回", -5, 10, 5, nil},
	}
	for _, c := range cases {
		got, err := ApplyDelta(c.before, c.delta)
		if !errors.Is(err, c.err) || (err == nil && got != c.want) {
			t.Errorf("%s：ApplyDelta(%d, %d) = %d, %v；期望 %d, %v", c.name, c.before, c.delta, got, err, c.want, c.err)
		}
	}
}

func TestValidateAmount(t *testing.T) {
	for _, amount := range []int64{1, -1, MaxAdminAdjustAmount, -MaxAdminAdjustAmount} {
		if err := ValidateAmount(KindIntegral, amount); err != nil {
			t.Errorf("%d 应合法：%v", amount, err)
		}
	}
	for _, amount := range []int64{0, MaxAdminAdjustAmount + 1, -MaxAdminAdjustAmount - 1, math.MinInt64, math.MaxInt64} {
		if err := ValidateAmount(KindIntegral, amount); err == nil {
			t.Errorf("%d 应被拒绝", amount)
		}
	}
	if err := ValidateAmount(KindExperience, 0); err == nil || !strings.Contains(err.Error(), "不能为 0") {
		t.Errorf("零值提示不符：%v", err)
	}
}

func TestAdjustInputValidate(t *testing.T) {
	in := AdjustInput{Kind: KindIntegral, AppID: 1, UserID: 2, Amount: 5, Reason: "  补偿  ", RequestID: " 3f1c0d9e-aaaa-bbbb-cccc-1234567890ab "}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	if in.Reason != "补偿" || strings.HasPrefix(in.RequestID, " ") {
		t.Fatalf("原因与请求编号应去掉首尾空白：%q %q", in.Reason, in.RequestID)
	}

	for name, bad := range map[string]AdjustInput{
		"缺应用":     {Kind: KindIntegral, UserID: 2, Amount: 1},
		"缺用户":     {Kind: KindIntegral, AppID: 1, Amount: 1},
		"原因过长":    {Kind: KindIntegral, AppID: 1, UserID: 2, Amount: 1, Reason: strings.Repeat("长", MaxAdjustReasonRunes+1)},
		"编号含非法字符": {Kind: KindIntegral, AppID: 1, UserID: 2, Amount: 1, RequestID: "abc def ghi"},
		"编号过短":    {Kind: KindIntegral, AppID: 1, UserID: 2, Amount: 1, RequestID: "abc"},
		"类型未知":    {Kind: "coins", AppID: 1, UserID: 2, Amount: 1},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%s 应校验失败", name)
		} else {
			var v *ValidationError
			if !errors.As(err, &v) {
				t.Errorf("%s 的错误应为 ValidationError：%T", name, err)
			}
		}
	}

	exact := AdjustInput{Kind: KindIntegral, AppID: 1, UserID: 2, Amount: 1, Reason: strings.Repeat("长", MaxAdjustReasonRunes)}
	if err := exact.Validate(); err != nil {
		t.Errorf("恰好 %d 字的原因应合法：%v", MaxAdjustReasonRunes, err)
	}
}

func TestGroupDigits(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 100000000: "100,000,000", MaxBalance: "9,007,199,254,740,991"} {
		if got := GroupDigits(in); got != want {
			t.Errorf("GroupDigits(%d) = %q，期望 %q", in, got, want)
		}
	}
}
