package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
)

// CDN 与防盗链：存储中心「CDN 与安全」里配的东西在这里生效。
//
//   - CDN 域名：公开直链改用 CDN 域名拼（协议 + 域名 + 对象键）；
//   - 签名 URL：直链附 A 型鉴权参数 auth_key（阿里云、腾讯云、百度云等 CDN 通用的格式），
//     有效期由 CDN 侧按签名时间判断；永久地址不签（签了就不永久了），改发代理地址；
//   - Referer 白名单 / 黑名单、IP 白名单：在 Aegis 代理下载时执行（直链由 CDN 自己执行）。
//     Referer 为空的请求放行 —— App、播放器与直接打开的链接都不带 Referer；
//   - 缓存时长：代理下载响应的 Cache-Control max-age。

const storageCDNCacheTTL = 30 * time.Second

type cdnCacheEntry struct {
	config  *storagedomain.CDNConfig
	expires time.Time
}

var storageCDNCache = struct {
	sync.Mutex
	entries map[int64]cdnCacheEntry
}{entries: map[int64]cdnCacheEntry{}}

// cdnConfig 一个存储配置的 CDN 设置（30 秒缓存；没配时为 nil）。
func (s *StorageService) cdnConfig(ctx context.Context, configID int64) *storagedomain.CDNConfig {
	now := time.Now()
	storageCDNCache.Lock()
	if entry, ok := storageCDNCache.entries[configID]; ok && now.Before(entry.expires) {
		storageCDNCache.Unlock()
		return entry.config
	}
	storageCDNCache.Unlock()
	cfg, err := s.pg.GetCDNConfig(ctx, configID)
	if err != nil {
		return nil
	}
	storageCDNCache.Lock()
	storageCDNCache.entries[configID] = cdnCacheEntry{config: cfg, expires: now.Add(storageCDNCacheTTL)}
	storageCDNCache.Unlock()
	return cfg
}

// InvalidateCDNConfig CDN 设置改动后立即生效。
func InvalidateCDNConfig(configID int64) {
	storageCDNCache.Lock()
	delete(storageCDNCache.entries, configID)
	storageCDNCache.Unlock()
}

// publicObjectURL 公开直链：配了 CDN 域名走 CDN，否则交给提供商。permanent 时不签名。
// 返回空串表示这个地址不能以直链形式永久给出（开了签名），调用方改发代理地址。
func (s *StorageService) publicObjectURL(ctx context.Context, cfg *storagedomain.Config, provider storageProvider, objectKey string, expiresIn time.Duration, permanent bool) (string, error) {
	if cdn := s.cdnConfig(ctx, cfg.ID); cdn != nil && strings.TrimSpace(cdn.CDNDomain) != "" {
		if cdn.SignURLEnabled && permanent {
			return "", nil
		}
		link := cdnObjectURL(cdn, objectKey)
		if cdn.SignURLEnabled && strings.TrimSpace(cdn.SignURLSecret) != "" {
			link = signCDNTypeA(link, cdn.SignURLSecret, time.Now())
		}
		return link, nil
	}
	return provider.PublicURL(ctx, cfg, objectKey, expiresIn)
}

func cdnObjectURL(cdn *storagedomain.CDNConfig, objectKey string) string {
	domain := strings.TrimRight(strings.TrimSpace(cdn.CDNDomain), "/")
	if !strings.HasPrefix(domain, "http://") && !strings.HasPrefix(domain, "https://") {
		protocol := strings.ToLower(strings.TrimSpace(cdn.CDNProtocol))
		if protocol != "http" {
			protocol = "https"
		}
		domain = protocol + "://" + domain
	}
	return domain + "/" + encodeObjectPath(objectKey)
}

// signCDNTypeA A 型鉴权：auth_key = timestamp-rand-uid-md5(uri-timestamp-rand-uid-key)。
func signCDNTypeA(link, secret string, now time.Time) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return link
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	const random, uid = "0", "0"
	sum := md5.Sum([]byte(parsed.EscapedPath() + "-" + timestamp + "-" + random + "-" + uid + "-" + secret))
	query := parsed.Query()
	query.Set("auth_key", timestamp+"-"+random+"-"+uid+"-"+hex.EncodeToString(sum[:]))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// ProxyRequest 代理下载请求的来源信息（防盗链用）。
type ProxyRequest struct {
	ClientIP string
	Referer  string
}

// checkProxyAccess 按 CDN 设置里的 IP 白名单与 Referer 名单判定能不能下载。
func checkProxyAccess(cdn *storagedomain.CDNConfig, request ProxyRequest) error {
	if cdn == nil {
		return nil
	}
	denied := apperrors.New(40389, http.StatusForbidden, "当前来源不允许访问该资源")
	if len(cdn.IPWhitelist) > 0 && !ipAllowed(cdn.IPWhitelist, request.ClientIP) {
		return denied
	}
	host := refererHost(request.Referer)
	if host == "" {
		return nil
	}
	for _, pattern := range cdn.RefererBlacklist {
		if hostMatches(pattern, host) {
			return denied
		}
	}
	if len(cdn.RefererWhitelist) > 0 {
		for _, pattern := range cdn.RefererWhitelist {
			if hostMatches(pattern, host) {
				return nil
			}
		}
		return denied
	}
	return nil
}

func ipAllowed(list []string, raw string) bool {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return false
	}
	for _, entry := range list {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
				return true
			}
			continue
		}
		if allowed := net.ParseIP(entry); allowed != nil && allowed.Equal(ip) {
			return true
		}
	}
	return false
}

func refererHost(referer string) string {
	referer = strings.TrimSpace(referer)
	if referer == "" {
		return ""
	}
	parsed, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// hostMatches 名单条目可以写域名、*.域名，或带协议的完整地址。
func hostMatches(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	if strings.Contains(pattern, "://") {
		if parsed, err := url.Parse(pattern); err == nil {
			pattern = parsed.Hostname()
		}
	}
	pattern = strings.TrimSuffix(pattern, "/")
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:]
		return strings.HasSuffix(host, suffix) || host == pattern[2:]
	}
	return host == pattern
}

// ProxyCacheControl CDN 设置里的缓存时长换成 Cache-Control；没配时返回空串。
func (s *StorageService) ProxyCacheControl(ctx context.Context, configID int64) string {
	cdn := s.cdnConfig(ctx, configID)
	if cdn == nil || cdn.CacheMaxAge <= 0 {
		return ""
	}
	return fmt.Sprintf("public, max-age=%d", cdn.CacheMaxAge)
}
