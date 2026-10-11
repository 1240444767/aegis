package chinageo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// QQWry 纯真 IP 库（qqwry.dat，仅 IPv4）。整库读入内存，查询只读、可并发。
//
// 格式：头 8 字节为首末索引偏移；索引每条 7 字节（起始 IP + 3 字节记录偏移）；
// 记录为结束 IP 加国家、地区两段 GBK 字符串，两段都可能以重定向模式 1/2 指向别处。
type QQWry struct {
	data    []byte
	first   uint32
	count   uint32
	Version string
}

// OpenQQWry 读取并校验 qqwry.dat。
func OpenQQWry(path string) (*QQWry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseQQWry(data)
}

// ParseQQWry 校验并包装 qqwry.dat 内容。
func ParseQQWry(data []byte) (*QQWry, error) {
	if len(data) < 8 {
		return nil, errors.New("qqwry: file too small")
	}
	first := binary.LittleEndian.Uint32(data[0:4])
	last := binary.LittleEndian.Uint32(data[4:8])
	if last < first || int(last)+7 > len(data) || (last-first)%7 != 0 {
		return nil, errors.New("qqwry: invalid index")
	}
	db := &QQWry{data: data, first: first, count: (last-first)/7 + 1}
	// 最后一条记录（255.255.255.x）写的是数据版本，如「2026年10月07日IP数据」
	if _, area, err := db.record(db.recordOffset(db.count - 1)); err == nil {
		db.Version = area
	}
	return db, nil
}

func (q *QQWry) u24(offset uint32) uint32 {
	return uint32(q.data[offset]) | uint32(q.data[offset+1])<<8 | uint32(q.data[offset+2])<<16
}

func (q *QQWry) recordOffset(i uint32) uint32 {
	return q.u24(q.first + i*7 + 4)
}

func (q *QQWry) cstring(offset uint32) (string, uint32, error) {
	if int(offset) >= len(q.data) {
		return "", 0, errors.New("qqwry: offset out of range")
	}
	end := offset
	for int(end) < len(q.data) && q.data[end] != 0 {
		end++
	}
	if int(end) >= len(q.data) {
		return "", 0, errors.New("qqwry: unterminated string")
	}
	text, err := simplifiedchinese.GBK.NewDecoder().Bytes(q.data[offset:end])
	if err != nil {
		return "", 0, err
	}
	return string(text), end + 1, nil
}

func (q *QQWry) area(offset uint32) (string, error) {
	if int(offset) >= len(q.data) {
		return "", nil
	}
	switch q.data[offset] {
	case 1, 2:
		target := q.u24(offset + 1)
		if target == 0 {
			return "", nil
		}
		text, _, err := q.cstring(target)
		return text, err
	default:
		text, _, err := q.cstring(offset)
		return text, err
	}
}

func (q *QQWry) record(offset uint32) (country, area string, err error) {
	if int(offset)+8 > len(q.data) {
		return "", "", errors.New("qqwry: record out of range")
	}
	offset += 4
	mode := q.data[offset]
	if mode == 1 {
		offset = q.u24(offset + 1)
		mode = q.data[offset]
	}
	if mode == 2 {
		country, _, err = q.cstring(q.u24(offset + 1))
		if err != nil {
			return "", "", err
		}
		area, err = q.area(offset + 4)
		return country, area, err
	}
	country, next, err := q.cstring(offset)
	if err != nil {
		return "", "", err
	}
	area, err = q.area(next)
	return country, area, err
}

// Lookup 查询 IPv4，返回纯真原文的「国家」「地区」两段。IPv6 返回 false。
func (q *QQWry) Lookup(addr netip.Addr) (country, area string, ok bool) {
	if q == nil || !addr.Unmap().Is4() {
		return "", "", false
	}
	ip4 := addr.Unmap().As4()
	value := binary.BigEndian.Uint32(ip4[:])
	lo, hi := uint32(0), q.count-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		start := binary.LittleEndian.Uint32(q.data[q.first+mid*7:])
		if start <= value {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	offset := q.recordOffset(lo)
	if end := binary.LittleEndian.Uint32(q.data[offset:]); value > end {
		return "", "", false
	}
	country, area, err := q.record(offset)
	if err != nil {
		return "", "", false
	}
	return strings.TrimSpace(country), strings.TrimSpace(area), true
}

// QQWryPlace 纯真结果里的大陆地点与运营商。
type QQWryPlace struct {
	Place
	ISP string
}

// LookupPlace 只在纯真判为中国大陆时返回：「中国–广东–深圳–南山区」+「联通/漕河泾数据中心」。
func (q *QQWry) LookupPlace(addr netip.Addr) (QQWryPlace, bool) {
	country, area, ok := q.Lookup(addr)
	if !ok {
		return QQWryPlace{}, false
	}
	parts := strings.Split(country, "–")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) != "中国" {
		return QQWryPlace{}, false
	}
	pick := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	place, ok := Resolve(pick(1), pick(2), pick(3))
	if !ok {
		return QQWryPlace{}, false
	}
	return QQWryPlace{Place: place, ISP: qqwryISP(area)}, true
}

// qqwryISP 取地区段的首项并归一到与全球库一致的简称（电信、联通、移动…）。
func qqwryISP(area string) string {
	head := strings.TrimSpace(strings.SplitN(area, "/", 2)[0])
	if head == "" {
		return ""
	}
	for _, known := range []string{"电信", "联通", "移动", "铁通", "广电", "教育网", "科技网", "鹏博士", "长城宽带"} {
		if strings.Contains(head, known) {
			return known
		}
	}
	// 「阿里云」「腾讯云」这类机房名照原文给出
	if len([]rune(head)) <= 16 && !strings.Contains(head, "CZ88") {
		return head
	}
	return ""
}

func (q *QQWry) String() string {
	if q == nil {
		return "qqwry(nil)"
	}
	return fmt.Sprintf("qqwry(%d records, %s)", q.count, q.Version)
}
