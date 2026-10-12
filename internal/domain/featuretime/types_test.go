package featuretime

import (
	"testing"
	"time"

	vipdomain "aegis/internal/domain/vip"
)

func TestClampLeaseSeconds(t *testing.T) {
	cases := map[int]int{1: 1, 300: 300, MaxLeaseSeconds: MaxLeaseSeconds, MaxLeaseSeconds + 1: MaxLeaseSeconds, 86400: MaxLeaseSeconds}
	for input, want := range cases {
		got, err := ClampLeaseSeconds(input)
		if err != nil || got != want {
			t.Errorf("ClampLeaseSeconds(%d) = %d, %v；期望 %d", input, got, err, want)
		}
	}
	for _, bad := range []int{0, -1} {
		if _, err := ClampLeaseSeconds(bad); err == nil {
			t.Errorf("ClampLeaseSeconds(%d) 应当报参数错误", bad)
		}
	}
}

func TestSettle(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	lease := LeaseRecord{Seconds: 300, ExpiresAt: now.Add(time.Hour)}
	cases := []struct {
		name               string
		used               int
		at                 time.Time
		status             string
		wantUsed, wantBack int
	}{
		{name: "用了一部分", used: 120, at: now, status: LeaseSettled, wantUsed: 120, wantBack: 180},
		{name: "全用完", used: 300, at: now, status: LeaseSettled, wantUsed: 300, wantBack: 0},
		{name: "多报按租约封顶", used: 9999, at: now, status: LeaseSettled, wantUsed: 300, wantBack: 0},
		{name: "负数按 0", used: -5, at: now, status: LeaseSettled, wantUsed: 0, wantBack: 300},
		// 边界：恰好到期算过期，与卡密的口径一致
		{name: "恰好到期", used: 0, at: lease.ExpiresAt, status: LeaseExpired, wantUsed: 0, wantBack: 0},
		{name: "过期不退", used: 10, at: lease.ExpiresAt.Add(time.Second), status: LeaseExpired, wantUsed: 10, wantBack: 0},
	}
	for _, tc := range cases {
		status, used, refund := Settle(lease, tc.used, tc.at)
		if status != tc.status || used != tc.wantUsed || refund != tc.wantBack {
			t.Errorf("%s：得到 %s/%d/%d，期望 %s/%d/%d", tc.name, status, used, refund, tc.status, tc.wantUsed, tc.wantBack)
		}
	}
}

func TestExempt(t *testing.T) {
	if Exempt(vipdomain.Entitlement{}, "online_music") {
		t.Fatal("非会员、没有该功能时不免扣")
	}
	if !Exempt(vipdomain.Entitlement{IsVIP: true}, "online_music") {
		t.Fatal("会员免扣，不论套餐带不带这个功能")
	}
	if !Exempt(vipdomain.Entitlement{Features: []string{"online_music"}}, "online_music") {
		t.Fatal("单独拥有该功能（功能解锁）免扣")
	}
	if Exempt(vipdomain.Entitlement{Features: []string{"export"}}, "online_music") {
		t.Fatal("拥有别的功能不免扣")
	}
}

func TestNormalizeFeature(t *testing.T) {
	if got, err := NormalizeFeature(" Online_Music "); err != nil || got != "online_music" {
		t.Fatalf("应去空白、转小写：%q %v", got, err)
	}
	for _, bad := range []string{"", "a", "1abc", "has space", "中文"} {
		if _, err := NormalizeFeature(bad); err == nil {
			t.Errorf("%q 应当非法", bad)
		}
	}
}

func TestFormatMinutes(t *testing.T) {
	for seconds, want := range map[int64]int64{0: 0, -5: 0, 59: 0, 60: 1, 5699: 94, 5700: 95} {
		if got := FormatMinutes(seconds); got != want {
			t.Errorf("FormatMinutes(%d) = %d，期望 %d", seconds, got, want)
		}
	}
}
