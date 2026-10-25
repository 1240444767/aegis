package httptransport

import (
	"context"

	"aegis/internal/service"
)

// 列表接口的 IP 位置回填。
//
// 位置不落库：审计行里只有 IP，位置是**查询时**得出的结论，
// 把当时的结论写进历史行反而会让同一个 IP 在不同记录里说法不一。
//
// 缓存未命中的 IP 由 ResolveMany 合并成一次远程请求，整页只多一次往返。
func (h *Handler) resolveIPLocations(ctx context.Context, ips []string) map[string]service.IPLocation {
	if h.location == nil || len(ips) == 0 {
		return nil
	}
	return h.location.ResolveMany(ctx, ips)
}

// geoCoords 拆出经纬度指针。GeoIP 对内网地址没有结论，此时返回 (nil, nil) ——
// 补一个 (0,0) 会让所有内网来源在地图上堆到几内亚湾。
func geoCoords(loc service.IPLocation) (*float64, *float64) {
	if loc.Coordinates == nil {
		return nil, nil
	}
	return loc.Coordinates.Latitude, loc.Coordinates.Longitude
}
