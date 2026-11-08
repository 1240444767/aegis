package service

import "testing"

func TestSameLoginRegion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"cn/广东", "cn/广东省", true},
		{"cn/广西", "cn/广西壮族自治区", true},
		{"cn/上海市", "cn/上海", true},
		{"cn/", "cn/广东省", true},
		{"cn/广东省", "cn/浙江省", false},
		{"cn/广东省", "us/california", false},
		{"us/california", "us/california", true},
	}
	for _, tc := range cases {
		if got := sameLoginRegion(tc.a, tc.b); got != tc.want {
			t.Fatalf("sameLoginRegion(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}
