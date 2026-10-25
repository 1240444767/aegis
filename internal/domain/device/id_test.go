package device

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeID(t *testing.T) {
	cases := map[string]string{
		"":                                     "",
		"   ":                                  "",
		strings.Repeat("0", 64):                "",
		strings.Repeat("0", 32):                "",
		"0000000000000000":                     "",
		"00000000-0000-0000-0000-000000000000": "",
		"02:00:00:00:00:00":                    "02:00:00:00:00:00",
		"9774D56D682E549C":                     "",
		"unknown":                              "",
		"NULL":                                 "",
		" a1b2c3d4e5f60708 ":                   "a1b2c3d4e5f60708",
		"00000000000000000000000000000001":     "00000000000000000000000000000001",
		"c3f1e2d0-1a2b-4c5d-8e9f-0123456789ab": "c3f1e2d0-1a2b-4c5d-8e9f-0123456789ab",
	}
	for in, want := range cases {
		if got := NormalizeID(in); got != want {
			t.Errorf("NormalizeID(%q) = %q, want %q", in, got, want)
		}
	}
	if !IsPlaceholderID(strings.Repeat("0", 64)) || IsPlaceholderID("") || IsPlaceholderID("abc") {
		t.Fatal("IsPlaceholderID")
	}
}

func TestPreviousIDAndContinues(t *testing.T) {
	ctx := WithPreviousID(context.Background(), " old-id ", "new-id")
	if PreviousIDFrom(ctx) != "old-id" {
		t.Fatalf("previous = %q", PreviousIDFrom(ctx))
	}
	if PreviousIDFrom(WithPreviousID(context.Background(), strings.Repeat("0", 64), "new-id")) != "" {
		t.Fatal("placeholder previous id should be ignored")
	}
	if PreviousIDFrom(WithPreviousID(context.Background(), "same", "same")) != "" {
		t.Fatal("previous equal to current should be ignored")
	}
	if !Continues(ctx, "old-id", "new-id") || !Continues(ctx, "new-id", "new-id") ||
		!Continues(ctx, strings.Repeat("0", 64), "new-id") || Continues(ctx, "other", "new-id") {
		t.Fatal("Continues")
	}
	if Continues(context.Background(), "old-id", "new-id") {
		t.Fatal("without a previous id a changed device must not continue")
	}
}
