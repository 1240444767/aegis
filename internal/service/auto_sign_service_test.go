package service

import (
	"testing"
	"time"

	appdomain "aegis/internal/domain/app"
	userdomain "aegis/internal/domain/user"
)

func TestAutoSignServiceIsEligibleWithoutPolicy(t *testing.T) {
	service := &AutoSignService{}

	// 账号停用或个人开关关着时不必查策略，直接不排
	for name, item := range map[string]userdomain.AutoSignCandidate{
		"账号停用": {Enabled: false, Settings: userdomain.AutoSignSettings{Enabled: true}},
		"开关关闭": {Enabled: true, Settings: userdomain.AutoSignSettings{Enabled: false}},
	} {
		eligible, err := service.isEligible(t.Context(), appdomain.DefaultAutoSignPolicy(), item)
		if err != nil || eligible {
			t.Fatalf("%s：应当不排班，得到 eligible=%v err=%v", name, eligible, err)
		}
	}
}

func TestAutoSignServiceInitialDue(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	service := &AutoSignService{location: location}
	reference := time.Date(2026, 3, 29, 9, 30, 0, 0, location)

	due := service.initialDue(reference, "10:15", location)
	expected := time.Date(2026, 3, 29, 10, 15, 0, 0, location)
	if !due.Equal(expected) {
		t.Fatalf("expected due %v, got %v", expected, due)
	}

	pastDue := service.initialDue(reference, "08:15", location)
	if !pastDue.Equal(reference) {
		t.Fatalf("expected past due to fall back to reference %v, got %v", reference, pastDue)
	}
}

func TestAutoSignServiceNextDayDue(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	service := &AutoSignService{location: location}

	// 签完当天的时刻之后，下一次是明天同一时刻
	after := time.Date(2026, 3, 29, 8, 0, 1, 0, location)
	if got, want := service.nextDayDue(after, "08:00", location), time.Date(2026, 3, 30, 8, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	// 重试拖到了时刻之前（比如凌晨补签）：下一次就是今天的时刻
	before := time.Date(2026, 3, 29, 6, 0, 0, 0, location)
	if got, want := service.nextDayDue(before, "08:00", location), time.Date(2026, 3, 29, 8, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestAutoSignSuccessMessage(t *testing.T) {
	result := &userdomain.SignInResult{Record: userdomain.DailySignIn{IntegralReward: 10, ExperienceReward: 5, ConsecutiveDays: 3}}
	if got, want := autoSignSuccessMessage(result), "今日已自动签到，获得 10 积分、5 经验，已连续签到 3 天。"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := autoSignSuccessMessage(&userdomain.SignInResult{Record: userdomain.DailySignIn{ConsecutiveDays: 1}}), "今日已自动签到。"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
