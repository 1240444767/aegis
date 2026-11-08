package service

import (
	"context"
	"net/http"
	"os"
	"testing"

	"aegis/internal/config"
)

func TestBtCNLocation(t *testing.T) {
	cases := []struct {
		name string
		info btcnIPInfo
		want IPLocation
	}{
		{
			name: "mainland district",
			info: btcnIPInfo{Country: "中国", Province: "湖北", City: "孝感", Region: "汉川", Carrier: "移动", Division: "420984", EnShortCode: "CN", Longitude: "113.83898", Latitude: "30.66117"},
			want: IPLocation{Country: "中国", CountryCode: "CN", Region: "湖北省", City: "孝感市", District: "汉川市", ISP: "移动", Location: "中国 湖北省 孝感市 汉川市", Timezone: "Asia/Shanghai"},
		},
		{
			name: "mainland without division",
			info: btcnIPInfo{Country: "中国", Province: "广东", City: "深圳", Carrier: "电信", EnShortCode: "CN"},
			want: IPLocation{Country: "中国", CountryCode: "CN", Region: "广东省", City: "深圳市", ISP: "电信", Location: "中国 广东省 深圳市", Timezone: "Asia/Shanghai"},
		},
		{
			name: "hong kong",
			info: btcnIPInfo{Country: "中国", Province: "香港", Carrier: "腾讯云", Division: "810000", EnShortCode: "HK"},
			want: IPLocation{Country: "中国香港", CountryCode: "HK", ISP: "腾讯云", Location: "中国香港", Timezone: "Asia/Hong_Kong"},
		},
		{
			name: "abroad",
			info: btcnIPInfo{Country: "美国", City: "雷德蒙德", Carrier: "Microsoft/Azure", EnShortCode: "US"},
			want: IPLocation{Country: "美国", CountryCode: "US", City: "雷德蒙德", ISP: "Microsoft/Azure", Location: "美国 雷德蒙德"},
		},
		{
			name: "named service",
			info: btcnIPInfo{Country: "GoogleDNS", Carrier: "GoogleDNS"},
			want: IPLocation{ISP: "GoogleDNS", Location: "GoogleDNS"},
		},
		{
			name: "reserved",
			info: btcnIPInfo{Country: "保留"},
			want: IPLocation{Location: "保留地址", IsPrivate: true},
		},
		{
			name: "ipv6 unsupported",
			info: btcnIPInfo{},
			want: IPLocation{},
		},
	}
	for _, tc := range cases {
		got := btcnLocation("1.2.3.4", tc.info)
		if got.Country != tc.want.Country || got.CountryCode != tc.want.CountryCode || got.Region != tc.want.Region ||
			got.City != tc.want.City || got.District != tc.want.District || got.ISP != tc.want.ISP ||
			got.Location != tc.want.Location || got.Timezone != tc.want.Timezone || got.IsPrivate != tc.want.IsPrivate {
			t.Fatalf("%s: got %+v", tc.name, got)
		}
	}
	if loc := btcnLocation("1.2.3.4", btcnIPInfo{}); loc.isResolved() {
		t.Fatalf("empty answer must stay unresolved: %+v", loc)
	}
	if loc := btcnLocation("1.2.3.4", cases[3].info); detectNetworkType(loc.Network.Organization) != "IDC" || loc.Network.Type != "IDC" {
		t.Fatalf("azure should be IDC: %+v", loc.Network)
	}
}

// 真实接口：BTCN_LIVE=1 时运行。
func TestBtCNLive(t *testing.T) {
	if os.Getenv("BTCN_LIVE") == "" {
		t.Skip("BTCN_LIVE not set")
	}
	s := NewLocationService(nil, nil, "", configGeoEnabled())
	s.http = http.DefaultClient
	got := s.ResolveMany(context.Background(), []string{"117.152.187.30", "223.5.5.5:443", "8.8.8.8", "192.168.1.1", "240e:1::1", "13.107.42.14"})
	for ip, loc := range got {
		t.Logf("%s => %s | %s | %s | %v", ip, loc.Location, loc.ISP, loc.Source, loc.Coordinates != nil)
	}
	if got["117.152.187.30"].Region != "湖北省" || got["223.5.5.5:443"].City != "杭州市" {
		t.Fatalf("unexpected: %+v", got)
	}
	if !got["192.168.1.1"].IsPrivate {
		t.Fatal("private ip must not be sent")
	}
}

func configGeoEnabled() config.GeoIPConfig { return config.GeoIPConfig{Enabled: true} }
