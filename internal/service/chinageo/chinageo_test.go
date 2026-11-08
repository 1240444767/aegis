package chinageo

import "testing"

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

func TestResolveCode(t *testing.T) {
	cases := []struct {
		code string
		want Place
		ok   bool
	}{
		{"420984", Place{Province: "湖北省", City: "孝感市", District: "汉川市"}, true},
		{"440100", Place{Province: "广东省", City: "广州市"}, true},
		{"310104", Place{Province: "上海市", City: "上海市", District: "徐汇区"}, true},
		{"110000", Place{Province: "北京市", City: "北京市"}, true},
		{"420000", Place{Province: "湖北省"}, true},
		{"810000", Place{}, false},
		{"", Place{}, false},
		{"999999", Place{}, false},
	}
	for _, tc := range cases {
		got, ok := ResolveCode(tc.code)
		if ok != tc.ok {
			t.Fatalf("ResolveCode(%q) ok=%v", tc.code, ok)
		}
		if ok && (got.Province != tc.want.Province || got.City != tc.want.City || got.District != tc.want.District) {
			t.Fatalf("ResolveCode(%q) = %+v", tc.code, got)
		}
	}
}
