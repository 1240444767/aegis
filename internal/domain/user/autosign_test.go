package user

import "testing"

func TestNormalizeAutoSignTime(t *testing.T) {
	for input, want := range map[string]string{"8:5": "08:05", "08:00": "08:00", " 23:59 ": "23:59", "0:0": "00:00"} {
		if got, err := NormalizeAutoSignTime(input); err != nil || got != want {
			t.Errorf("%q：得到 %q %v，期望 %q", input, got, err, want)
		}
	}
	for _, input := range []string{"24:00", "12:60", "-1:00", "8", "08:00x", "", "ab:cd"} {
		if _, err := NormalizeAutoSignTime(input); err == nil {
			t.Errorf("%q 应当报错", input)
		}
	}
}

func TestMergeAutoSignSettings(t *testing.T) {
	current := DefaultAutoSignSettings()
	merged, err := MergeAutoSignSettings(current, map[string]any{"enabled": true, "time": "7:30", "maxRetries": float64(5), "unknown": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Enabled || merged.Time != "07:30" || merged.MaxRetries != 5 || !merged.NotifyOnSuccess {
		t.Fatalf("合并结果不对：%+v", merged)
	}
	bad := []map[string]any{
		{"enabled": "true"},
		{"time": 730},
		{"time": "25:00"},
		{"maxRetries": float64(1.5)},
		{"maxRetries": float64(AutoSignMaxRetriesLimit + 1)},
		{"maxRetries": -1},
	}
	for _, patch := range bad {
		if _, err := MergeAutoSignSettings(current, patch); err == nil {
			t.Errorf("%v 应当报错", patch)
		}
	}
}

func TestAutoSignSettingsFromMap(t *testing.T) {
	// 老数据里的坏值不报错，取默认
	got := AutoSignSettingsFromMap(map[string]any{"enabled": true, "time": "99:99", "maxRetries": "3"})
	want := DefaultAutoSignSettings()
	want.Enabled = true
	if got != want {
		t.Fatalf("得到 %+v，期望 %+v", got, want)
	}
	if round := AutoSignSettingsFromMap(want.ToMap()); round != want {
		t.Fatalf("存取不一致：%+v", round)
	}
}
