package service

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aegis/internal/config"
)

// 真实数据端到端：GEOIP_TEST_DIR 指向含 GeoLite2-City/ASN.mmdb、qqwry.dat
// 与 china-operator-ip/*46.txt 的目录时运行，对比叠加国内数据层前后的结果。
func TestGeoResolverChinaOverlayRealData(t *testing.T) {
	src := os.Getenv("GEOIP_TEST_DIR")
	if src == "" {
		t.Skip("GEOIP_TEST_DIR not set")
	}
	dir := t.TempDir()
	copyTree(t, src, dir)

	open := func(china bool) *geoDatabaseResolver {
		resolver, err := newGeoDatabaseResolver(nil, config.GeoIPConfig{
			Enabled:        true,
			DatabaseDir:    dir,
			UpdateInterval: 10000 * time.Hour,
			ChinaEnabled:   china,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(resolver.Close)
		return resolver
	}
	plain, overlay := open(false), open(true)
	if overlay.qqwry == nil || overlay.operators == nil {
		t.Fatalf("china data not loaded: qqwry=%v operators=%v", overlay.qqwry, overlay.operators)
	}

	for _, ip := range []string{
		"120.229.0.1", "112.65.0.1", "36.110.0.1", "183.192.0.1", "101.6.0.1",
		"119.29.29.29", "223.5.5.5", "114.114.114.114", "117.136.0.1", "58.250.0.1",
		"240e:3b0::1", "2408:8000::1", "8.8.8.8", "1.1.1.1", "103.21.244.1",
	} {
		before, _ := plain.Lookup(ip)
		after, err := overlay.Lookup(ip)
		if err != nil {
			t.Fatalf("%s: %v", ip, err)
		}
		t.Logf("%-16s 旧：%s｜%s｜%s", ip, before.Location, before.ISP, before.CountryCode)
		t.Logf("%-16s 新：%s｜%s｜%s｜%s", "", after.Location, after.ISP, after.CountryCode, after.Source)
	}

	if loc, _ := overlay.Lookup("120.229.0.1"); loc.Region != "广东省" || loc.City != "深圳市" || loc.ISP != "移动" || loc.Coordinates == nil {
		t.Fatalf("120.229.0.1 = %+v", loc)
	}
	if loc, _ := overlay.Lookup("8.8.8.8"); loc.CountryCode != "US" || loc.Source != "geoip-mmdb" {
		t.Fatalf("8.8.8.8 = %+v", loc)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

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
