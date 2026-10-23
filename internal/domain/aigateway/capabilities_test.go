package aigateway

import "testing"

func TestNormalizeCapabilities(t *testing.T) {
	got, err := NormalizeCapabilities(&Capabilities{Reasoning: true, ReasoningEfforts: []string{"HIGH", "none", "x-high", "high"}, ContextLength: 128000})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"none", "high", "xhigh"}
	if len(got.ReasoningEfforts) != len(want) {
		t.Fatalf("efforts = %v", got.ReasoningEfforts)
	}
	for index := range want {
		if got.ReasoningEfforts[index] != want[index] {
			t.Fatalf("efforts = %v, want %v", got.ReasoningEfforts, want)
		}
	}
	if _, err := NormalizeCapabilities(&Capabilities{Reasoning: true, ReasoningEfforts: []string{"ultra"}}); err == nil {
		t.Fatal("unknown effort must be rejected")
	}
	off, _ := NormalizeCapabilities(&Capabilities{ReasoningEfforts: []string{"low"}})
	if len(off.ReasoningEfforts) != 0 {
		t.Fatalf("efforts without reasoning should be cleared: %v", off.ReasoningEfforts)
	}
	if def, _ := NormalizeCapabilities(nil); !def.Tools || def.Vision || def.Reasoning {
		t.Fatalf("default = %+v", def)
	}
}
