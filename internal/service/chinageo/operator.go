package chinageo

import (
	"bufio"
	"io"
	"net/netip"
	"sort"
	"strings"
)

// OperatorList 一份 china-operator-ip 网段表（`<name>46.txt`，IPv4 与 IPv6 混排的 CIDR）。
type OperatorList struct {
	Name  string // 文件名前缀，如 chinanet
	Label string // 归一后的运营商简称，如「电信」；china 为空，只表示大陆归属
}

// OperatorLists china-operator-ip 的全部表。china 必须在首位：先判大陆归属，再判运营商。
var OperatorLists = []OperatorList{
	{Name: "china"},
	{Name: "chinanet", Label: "电信"},
	{Name: "unicom", Label: "联通"},
	{Name: "cmcc", Label: "移动"},
	{Name: "cernet", Label: "教育网"},
	{Name: "cstnet", Label: "科技网"},
	{Name: "drpeng", Label: "鹏博士"},
	{Name: "googlecn", Label: "谷歌"},
}

type addrRange struct {
	start netip.Addr
	end   netip.Addr
}

// rangeSet 按起点排序的区间表，二分查找。
type rangeSet []addrRange

func (s rangeSet) contains(addr netip.Addr) bool {
	i := sort.Search(len(s), func(i int) bool { return s[i].start.Compare(addr) > 0 })
	if i == 0 {
		return false
	}
	r := s[i-1]
	return addr.Compare(r.start) >= 0 && addr.Compare(r.end) <= 0
}

// ParseRanges 解析一份 CIDR 列表，跳过空行、注释与无法解析的行。
func parseRanges(r io.Reader) (rangeSet, error) {
	var set rangeSet
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		set = append(set, addrRange{start: prefix.Addr(), end: lastAddr(prefix)})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(set, func(i, j int) bool { return set[i].start.Less(set[j].start) })
	return set, nil
}

func lastAddr(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Addr().AsSlice()
	bits := prefix.Bits()
	for i := range bytes {
		start := i * 8
		switch {
		case start >= bits:
			bytes[i] = 0xff
		case start+8 > bits:
			bytes[i] |= byte(0xff >> (bits - start))
		}
	}
	addr, _ := netip.AddrFromSlice(bytes)
	return addr
}

// Operators 已加载的全部网段表。
type Operators struct {
	mainland rangeSet
	labeled  []labeledSet
	Ranges   int
}

type labeledSet struct {
	label string
	set   rangeSet
}

// LoadOperators 按 OperatorLists 的顺序读取各表。open 返回 nil 读取器表示该表缺失，跳过。
// 大陆总表缺失时返回 nil：没有它就无法判定归属，运营商结论也不该单独使用。
func LoadOperators(open func(name string) (io.ReadCloser, error)) (*Operators, error) {
	ops := &Operators{}
	for _, list := range OperatorLists {
		reader, err := open(list.Name)
		if err != nil {
			return nil, err
		}
		if reader == nil {
			continue
		}
		set, err := parseRanges(reader)
		_ = reader.Close()
		if err != nil {
			return nil, err
		}
		ops.Ranges += len(set)
		if list.Label == "" {
			ops.mainland = set
		} else {
			ops.labeled = append(ops.labeled, labeledSet{label: list.Label, set: set})
		}
	}
	if len(ops.mainland) == 0 {
		return nil, nil
	}
	return ops, nil
}

// Lookup 返回是否属于中国大陆网段，以及命中的运营商简称（可能为空）。
func (o *Operators) Lookup(addr netip.Addr) (mainland bool, operator string) {
	if o == nil {
		return false, ""
	}
	addr = addr.Unmap()
	if !o.mainland.contains(addr) {
		return false, ""
	}
	for _, item := range o.labeled {
		if item.set.contains(addr) {
			return true, item.label
		}
	}
	return true, ""
}
