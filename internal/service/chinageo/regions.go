// Package chinageo 是内置的国内行政区表：把归属地接口给的行政区划代码或省市区简称
// 换成官方全称，并给出政府驻地坐标。
package chinageo

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

// regions.tsv 由 aegis-console/scripts/gen-china-regions.mjs 从控制台地图资源生成。
//
//go:embed regions.tsv
var regionsTSV string

// Region 一条行政区：省级、地级或区县，坐标取政府驻地。
type Region struct {
	Code   int
	Parent int
	Level  string
	Name   string
	Lng    float64
	Lat    float64
}

// Place 解析后的国内地点。City 对直辖市等同 Province；坐标取能解析到的最细一级。
type Place struct {
	Province string
	City     string
	District string
	Lat      float64
	Lng      float64
}

type regionIndex struct {
	byCode   map[int]*Region
	children map[int][]*Region
}

var (
	indexOnce sync.Once
	index     *regionIndex
)

func regions() *regionIndex {
	indexOnce.Do(func() {
		idx := &regionIndex{byCode: map[int]*Region{}, children: map[int][]*Region{}}
		for _, line := range strings.Split(regionsTSV, "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			cols := strings.Split(strings.TrimRight(line, "\r"), "\t")
			if len(cols) != 6 {
				continue
			}
			code, err1 := strconv.Atoi(cols[0])
			parent, err2 := strconv.Atoi(cols[1])
			lng, err3 := strconv.ParseFloat(cols[4], 64)
			lat, err4 := strconv.ParseFloat(cols[5], 64)
			if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
				continue
			}
			region := &Region{Code: code, Parent: parent, Level: cols[2], Name: cols[3], Lng: lng, Lat: lat}
			idx.byCode[code] = region
			idx.children[parent] = append(idx.children[parent], region)
		}
		index = idx
	})
	return index
}

const countryCode = 100000

// 港澳台不在大陆行政区表的范围内，由调用方单独处理。
var nonMainland = map[int]bool{710000: true, 810000: true, 820000: true}

// matchName 把归属地里的简称（「广东」「深圳」「南山区」）对上全称（「广东省」「深圳市」「南山区」）。
func matchName(candidates []*Region, short string, level string) *Region {
	short = strings.TrimSpace(short)
	if short == "" {
		return nil
	}
	var prefixed *Region
	for _, region := range candidates {
		if level != "" && region.Level != level {
			continue
		}
		if region.Name == short {
			return region
		}
		if prefixed == nil && len([]rune(short)) >= 2 && strings.HasPrefix(region.Name, short) {
			prefixed = region
		}
	}
	return prefixed
}

// Resolve 把省、市、区简称解析为全称与坐标。省级对不上或属于港澳台时返回 false。
func Resolve(province, city, district string) (Place, bool) {
	idx := regions()
	prov := matchName(idx.children[countryCode], province, "province")
	if prov == nil || nonMainland[prov.Code] {
		return Place{}, false
	}
	place := Place{Province: prov.Name, Lat: prov.Lat, Lng: prov.Lng}

	cityRegion := matchName(idx.children[prov.Code], city, "city")
	districtParent := prov.Code
	switch {
	case cityRegion != nil:
		place.City = cityRegion.Name
		place.Lat, place.Lng = cityRegion.Lat, cityRegion.Lng
		districtParent = cityRegion.Code
	case isMunicipality(idx, prov):
		// 直辖市下面直接是区，城市一栏写省级名称
		place.City = prov.Name
	}

	if districtRegion := matchName(idx.children[districtParent], district, "district"); districtRegion != nil && place.City != "" {
		place.District = districtRegion.Name
		place.Lat, place.Lng = districtRegion.Lat, districtRegion.Lng
	}
	return place, true
}

func isMunicipality(idx *regionIndex, prov *Region) bool {
	children := idx.children[prov.Code]
	if len(children) == 0 {
		return false
	}
	for _, child := range children {
		if child.Level == "city" {
			return false
		}
	}
	return true
}

// ResolveCode 按行政区划代码（省、地、县任一级）取全称与坐标。港澳台、未知代码返回 false。
func ResolveCode(code string) (Place, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil {
		return Place{}, false
	}
	idx := regions()
	region := idx.byCode[value]
	if region == nil {
		return Place{}, false
	}
	place := Place{Lat: region.Lat, Lng: region.Lng}
	var prov *Region
	for current := region; current != nil && current.Code != countryCode; current = idx.byCode[current.Parent] {
		switch current.Level {
		case "province":
			if nonMainland[current.Code] {
				return Place{}, false
			}
			place.Province, prov = current.Name, current
		case "city":
			place.City = current.Name
		case "district":
			place.District = current.Name
		}
	}
	if prov == nil {
		return Place{}, false
	}
	if place.City == "" && isMunicipality(idx, prov) {
		// 直辖市下面直接是区，城市一栏与 Resolve 一致取省级全称
		place.City = place.Province
	}
	return place, true
}
