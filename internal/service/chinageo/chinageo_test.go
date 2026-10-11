package chinageo

import (
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		province, city, district string
		want                     Place
		ok                       bool
	}{
		{"广东", "深圳", "南山区", Place{Province: "广东省", City: "深圳市", District: "南山区"}, true},
		{"上海", "上海", "徐汇区", Place{Province: "上海市", City: "上海市", District: "徐汇区"}, true},
		{"北京", "北京", "", Place{Province: "北京市", City: "北京市"}, true},
		{"内蒙古", "呼和浩特", "", Place{Province: "内蒙古自治区", City: "呼和浩特市"}, true},
		{"吉林", "吉林", "", Place{Province: "吉林省", City: "吉林市"}, true},
		{"湖北", "", "", Place{Province: "湖北省"}, true},
		{"香港", "", "", Place{}, false},
		{"火星", "", "", Place{}, false},
	}
	for _, tc := range cases {
		got, ok := Resolve(tc.province, tc.city, tc.district)
		if ok != tc.ok {
			t.Fatalf("Resolve(%q,%q,%q) ok=%v", tc.province, tc.city, tc.district, ok)
		}
		if !ok {
			continue
		}
		if got.Province != tc.want.Province || got.City != tc.want.City || got.District != tc.want.District {
			t.Fatalf("Resolve(%q,%q,%q) = %+v", tc.province, tc.city, tc.district, got)
		}
		if got.Lat < 18 || got.Lat > 54 || got.Lng < 73 || got.Lng > 136 {
			t.Fatalf("coordinates out of China: %+v", got)
		}
	}
}

func TestOperators(t *testing.T) {
	lists := map[string]string{
		"china":    "1.0.1.0/24\n36.96.0.0/11\n240e::/20\n",
		"chinanet": "36.96.0.0/11\n240e::/20\n",
	}
	ops, err := LoadOperators(func(name string) (io.ReadCloser, error) {
		body, ok := lists[name]
		if !ok {
			return nil, nil
		}
		return io.NopCloser(strings.NewReader(body)), nil
	})
	if err != nil || ops == nil {
		t.Fatalf("load: %v", err)
	}
	cases := map[string]struct {
		mainland bool
		operator string
	}{
		"36.110.0.1":     {true, "电信"},
		"36.127.255.255": {true, "电信"},
		"36.128.0.0":     {false, ""},
		"1.0.1.9":        {true, ""},
		"240e:3b0::1":    {true, "电信"},
		"2001:db8::1":    {false, ""},
		"8.8.8.8":        {false, ""},
	}
	for ip, want := range cases {
		mainland, operator := ops.Lookup(netip.MustParseAddr(ip))
		if mainland != want.mainland || operator != want.operator {
			t.Fatalf("%s = %v %q, want %v %q", ip, mainland, operator, want.mainland, want.operator)
		}
	}
}

// 真实数据校验：GEOIP_TEST_DIR 指向含 qqwry.dat 的目录时运行。
func TestQQWryRealData(t *testing.T) {
	dir := os.Getenv("GEOIP_TEST_DIR")
	if dir == "" {
		t.Skip("GEOIP_TEST_DIR not set")
	}
	db, err := OpenQQWry(filepath.Join(dir, "qqwry.dat"))
	if err != nil {
		t.Fatal(err)
	}
	t.Log(db)
	place, ok := db.LookupPlace(netip.MustParseAddr("120.229.0.1"))
	if !ok || place.Province != "广东省" || place.City != "深圳市" || place.ISP != "移动" {
		t.Fatalf("120.229.0.1 = %+v %v", place, ok)
	}
	if _, ok := db.LookupPlace(netip.MustParseAddr("8.8.8.8")); ok {
		t.Fatal("8.8.8.8 should not be mainland")
	}
}
