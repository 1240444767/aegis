package service

import (
	"aegis/pkg/egress"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	"aegis/internal/service/chinageo"
	geoip2 "github.com/oschwald/geoip2-golang"
	"go.uber.org/zap"
)

type geoDBMeta struct {
	LastUpdatedAt time.Time `json:"lastUpdatedAt"`
}

type geoDatabaseResolver struct {
	log      *zap.Logger
	cfg      config.GeoIPConfig
	http     *http.Client
	baseDir  string
	cityPath string
	asnPath  string
	metaPath string
	// 国内补充层
	qqwryPath   string
	operatorDir string

	mu        sync.RWMutex
	city      *geoip2.Reader
	asn       *geoip2.Reader
	qqwry     *chinageo.QQWry
	operators *chinageo.Operators
	cancel    context.CancelFunc
	done      chan struct{}
}

func newGeoDatabaseResolver(log *zap.Logger, cfg config.GeoIPConfig) (*geoDatabaseResolver, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if log == nil {
		log = zap.NewNop()
	}

	baseDir := strings.TrimSpace(cfg.DatabaseDir)
	if baseDir == "" {
		baseDir = ".runtime/geoip"
	}
	baseDir = filepath.Clean(baseDir)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create geoip database dir: %w", err)
	}

	resolver := &geoDatabaseResolver{
		log: log,
		cfg: cfg,
		// mmdb 从 GitHub 下载，是最典型的出海场景
		http:     egress.NewClient(egress.Profile{Name: "geoip.download", Timeout: cfg.DownloadTimeout}),
		baseDir:  baseDir,
		cityPath: filepath.Join(baseDir, "GeoLite2-City.mmdb"),
		asnPath:  filepath.Join(baseDir, "GeoLite2-ASN.mmdb"),
		metaPath: filepath.Join(baseDir, "meta.json"),
		done:     make(chan struct{}),

		qqwryPath:   filepath.Join(baseDir, "qqwry.dat"),
		operatorDir: filepath.Join(baseDir, "china-operator-ip"),
	}
	if err := resolver.loadReaders(); err != nil {
		resolver.log.Warn("load geoip database readers failed", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	resolver.cancel = cancel
	go resolver.run(ctx)
	return resolver, nil
}

func (r *geoDatabaseResolver) run(ctx context.Context) {
	defer close(r.done)

	if r.needsRefresh() {
		r.refreshDatabases(ctx)
	}

	ticker := time.NewTicker(r.cfg.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshDatabases(ctx)
		}
	}
}

func (r *geoDatabaseResolver) Close() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.done != nil {
		<-r.done
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.city != nil {
		_ = r.city.Close()
		r.city = nil
	}
	if r.asn != nil {
		_ = r.asn.Close()
		r.asn = nil
	}
	r.qqwry = nil
	r.operators = nil
}

func (r *geoDatabaseResolver) Lookup(ip string) (IPLocation, error) {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return IPLocation{}, fmt.Errorf("invalid ip")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var (
		country      string
		countryCode  string
		region       string
		city         string
		district     string
		timezone     string
		isp          string
		organization string
		asn          string
		coordinates  *GeoCoordinate
	)

	if r.city != nil {
		record, err := r.city.City(parsed)
		if err != nil {
			return IPLocation{}, err
		}
		if record != nil {
			country = localizedGeoName(record.Country.Names)
			countryCode = strings.ToUpper(strings.TrimSpace(record.Country.IsoCode))
			if len(record.Subdivisions) > 0 {
				region = localizedGeoName(record.Subdivisions[0].Names)
			}
			if len(record.Subdivisions) > 1 {
				district = localizedGeoName(record.Subdivisions[1].Names)
			}
			city = localizedGeoName(record.City.Names)
			timezone = normalizeTimezone(country, record.Location.TimeZone)
			if record.Location.Latitude != 0 || record.Location.Longitude != 0 {
				lat := record.Location.Latitude
				lng := record.Location.Longitude
				coordinates = &GeoCoordinate{Latitude: &lat, Longitude: &lng}
			}
		}
	}

	if r.asn != nil {
		record, err := r.asn.ASN(parsed)
		if err != nil {
			return IPLocation{}, err
		}
		if record != nil {
			organization = normalizeString(record.AutonomousSystemOrganization)
			isp = normalizeISP(record.AutonomousSystemOrganization)
			if record.AutonomousSystemNumber > 0 {
				asn = fmt.Sprintf("AS%d", record.AutonomousSystemNumber)
			}
		}
	}

	displayCountry, normalizedCountryCode := normalizeCountry(country, countryCode)
	loc := IPLocation{
		IP:          strings.TrimSpace(ip),
		Country:     displayCountry,
		CountryCode: normalizedCountryCode,
		Region:      normalizeString(region),
		City:        normalizeString(city),
		District:    normalizeString(district),
		Location:    composeLocation(displayCountry, region, city, district),
		Timezone:    timezone,
		ISP:         isp,
		Coordinates: coordinates,
		Network: NetworkInfo{
			Type:         detectNetworkType(organization),
			Organization: organization,
			ASN:          normalizeString(asn),
		},
		Source:     "geoip-mmdb",
		ResolvedAt: time.Now().UTC(),
	}
	if addr, err := netip.ParseAddr(strings.TrimSpace(ip)); err == nil {
		r.overlayChina(&loc, addr.Unmap())
	}
	if !loc.isResolved() {
		return IPLocation{}, fmt.Errorf("geoip database returned empty result")
	}
	return loc, nil
}

// overlayChina 用国内数据修正大陆 IP：归属以 china-operator-ip 或纯真为准，
// 省市区取纯真并换成官方全称与驻地坐标，运营商优先网段表。调用方持有读锁。
func (r *geoDatabaseResolver) overlayChina(loc *IPLocation, addr netip.Addr) {
	mainland, operator := r.operators.Lookup(addr)
	place, hasPlace := r.qqwry.LookupPlace(addr)
	if !mainland && !hasPlace {
		return
	}

	if hasPlace {
		loc.Region, loc.City, loc.District = place.Province, place.City, place.District
		lat, lng := place.Lat, place.Lng
		loc.Coordinates = &GeoCoordinate{Latitude: &lat, Longitude: &lng}
	} else if loc.CountryCode != "CN" {
		// 全球库把大陆网段判到了别国（常见于大厂 ASN），它给的省市与坐标也不可信
		loc.Region, loc.City, loc.District = "", "", ""
		loc.Coordinates = nil
	}
	loc.Country, loc.CountryCode = "中国", "CN"
	loc.Timezone = "Asia/Shanghai"

	// 纯真对机房、高校等给的是具体名称（「腾讯云」「清华大学」），比网段表的大类更有用；
	// 只是基础运营商时以每日更新的网段表为准
	isp := place.ISP
	if isp == "" || isBasicCarrier(isp) {
		if operator != "" {
			isp = operator
		}
	}
	if isp != "" {
		loc.ISP = isp
	}
	loc.Location = composeLocation(loc.Country, loc.Region, loc.City, loc.District)
	loc.Source = "geoip-mmdb+cn"
}

func isBasicCarrier(isp string) bool {
	switch isp {
	case "电信", "联通", "移动", "铁通", "广电", "教育网", "科技网", "鹏博士":
		return true
	}
	return false
}

func (r *geoDatabaseResolver) needsRefresh() bool {
	if fileMissing(r.cityPath) || fileMissing(r.asnPath) {
		return true
	}
	if r.cfg.ChinaEnabled && (fileMissing(r.qqwryPath) || fileMissing(r.operatorPath("china"))) {
		return true
	}
	meta, err := r.readMeta()
	if err == nil && !meta.LastUpdatedAt.IsZero() {
		return time.Since(meta.LastUpdatedAt) >= r.cfg.UpdateInterval
	}
	info, err := os.Stat(r.cityPath)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) >= r.cfg.UpdateInterval
}

func (r *geoDatabaseResolver) refreshDatabases(ctx context.Context) {
	var (
		cityTemp string
		asnTemp  string
		updated  bool
	)

	if strings.TrimSpace(r.cfg.CityDBURL) != "" {
		if fileMissing(r.cityPath) || r.needsRefresh() {
			path, err := r.downloadDatabase(ctx, r.cfg.CityDBURL, "city")
			if err != nil {
				r.log.Warn("refresh geoip city database failed", zap.Error(err))
			} else {
				cityTemp = path
				updated = true
			}
		}
	}

	if strings.TrimSpace(r.cfg.ASNDBURL) != "" {
		if fileMissing(r.asnPath) || r.needsRefresh() {
			path, err := r.downloadDatabase(ctx, r.cfg.ASNDBURL, "asn")
			if err != nil {
				r.log.Warn("refresh geoip asn database failed", zap.Error(err))
			} else {
				asnTemp = path
				updated = true
			}
		}
	}

	chinaUpdated := r.refreshChina(ctx)

	if updated {
		defer cleanupTempFile(cityTemp)
		defer cleanupTempFile(asnTemp)
		if err := r.promote(cityTemp, asnTemp); err != nil {
			r.log.Warn("promote geoip databases failed", zap.Error(err))
			return
		}
	} else if chinaUpdated {
		if err := r.loadReaders(); err != nil {
			r.log.Warn("reload geoip databases failed", zap.Error(err))
			return
		}
	}
	if !updated && !chinaUpdated {
		return
	}
	if err := r.writeMeta(geoDBMeta{LastUpdatedAt: time.Now().UTC()}); err != nil {
		r.log.Warn("write geoip metadata failed", zap.Error(err))
	}
}

func (r *geoDatabaseResolver) operatorPath(name string) string {
	return filepath.Join(r.operatorDir, name+"46.txt")
}

// refreshChina 下载纯真库与 china-operator-ip 网段表，校验通过后替换到位。
// 只替换文件，读取器由调用方统一重载。
func (r *geoDatabaseResolver) refreshChina(ctx context.Context) bool {
	if !r.cfg.ChinaEnabled {
		return false
	}
	updated := false

	if raw := strings.TrimSpace(r.cfg.QQWryURL); raw != "" {
		path, err := r.downloadFile(ctx, raw, "qqwry", ".dat", func(path string) error {
			db, err := chinageo.OpenQQWry(path)
			if err != nil {
				return err
			}
			if _, _, ok := db.Lookup(netip.MustParseAddr("114.114.114.114")); !ok {
				return fmt.Errorf("qqwry lookup self-check failed")
			}
			return nil
		})
		if err != nil {
			r.log.Warn("refresh qqwry database failed", zap.Error(err))
		} else if err := replaceFile(path, r.qqwryPath); err != nil {
			cleanupTempFile(path)
			r.log.Warn("replace qqwry database failed", zap.Error(err))
		} else {
			updated = true
		}
	}

	if base := strings.TrimSpace(r.cfg.ChinaOperatorBaseURL); base != "" {
		if err := os.MkdirAll(r.operatorDir, 0o755); err != nil {
			r.log.Warn("create china operator dir failed", zap.Error(err))
			return updated
		}
		// 先全部下载校验，再统一替换，避免新旧表混用
		temps := make(map[string]string, len(chinageo.OperatorLists))
		ok := true
		for _, list := range chinageo.OperatorLists {
			path, err := r.downloadFile(ctx, strings.TrimRight(base, "/")+"/"+list.Name+"46.txt", "operator-"+list.Name, ".txt", nil)
			if err != nil {
				r.log.Warn("refresh china operator list failed", zap.String("list", list.Name), zap.Error(err))
				ok = false
				break
			}
			temps[list.Name] = path
		}
		if ok {
			if info, err := os.Stat(temps["china"]); err != nil || info.Size() < 1024 {
				r.log.Warn("china operator list looks empty, skipped")
				ok = false
			}
		}
		for name, path := range temps {
			if !ok {
				cleanupTempFile(path)
				continue
			}
			if err := replaceFile(path, r.operatorPath(name)); err != nil {
				cleanupTempFile(path)
				r.log.Warn("replace china operator list failed", zap.String("list", name), zap.Error(err))
				continue
			}
			updated = true
		}
	}
	return updated
}

func (r *geoDatabaseResolver) loadChina() {
	if !r.cfg.ChinaEnabled {
		r.qqwry, r.operators = nil, nil
		return
	}
	r.qqwry = nil
	if fileExists(r.qqwryPath) {
		if db, err := chinageo.OpenQQWry(r.qqwryPath); err != nil {
			r.log.Warn("load qqwry database failed", zap.Error(err))
		} else {
			r.qqwry = db
			r.log.Info("qqwry database loaded", zap.String("version", db.Version))
		}
	}
	ops, err := chinageo.LoadOperators(func(name string) (io.ReadCloser, error) {
		file, err := os.Open(r.operatorPath(name))
		if os.IsNotExist(err) {
			return nil, nil
		}
		return file, err
	})
	if err != nil {
		r.log.Warn("load china operator lists failed", zap.Error(err))
	}
	r.operators = ops
}

func (r *geoDatabaseResolver) downloadDatabase(ctx context.Context, rawURL, prefix string) (string, error) {
	return r.downloadFile(ctx, rawURL, prefix, ".mmdb", func(path string) error {
		reader, err := geoip2.Open(path)
		if err != nil {
			return err
		}
		return reader.Close()
	})
}

// downloadFile 下载到数据目录下的临时文件；validate 不通过时删除并报错。
func (r *geoDatabaseResolver) downloadFile(ctx context.Context, rawURL, prefix, ext string, validate func(path string) error) (string, error) {
	requestURL := r.optimizedURL(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "aegis-geoip-updater/1.0")

	resp, err := r.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("download %s failed with status %d", prefix, resp.StatusCode)
	}

	tempFile, err := os.CreateTemp(r.baseDir, prefix+"-*"+ext)
	if err != nil {
		return "", err
	}
	tempPath := tempFile.Name()
	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		_ = tempFile.Close()
		cleanupTempFile(tempPath)
		return "", err
	}
	if err := tempFile.Close(); err != nil {
		cleanupTempFile(tempPath)
		return "", err
	}
	if validate != nil {
		if err := validate(tempPath); err != nil {
			cleanupTempFile(tempPath)
			return "", fmt.Errorf("validate %s database: %w", prefix, err)
		}
	}
	return tempPath, nil
}

func (r *geoDatabaseResolver) promote(cityTemp, asnTemp string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 国内补充层与 mmdb 互不依赖，mmdb 打不开也照常加载
	r.loadChina()

	if r.city != nil {
		_ = r.city.Close()
		r.city = nil
	}
	if r.asn != nil {
		_ = r.asn.Close()
		r.asn = nil
	}

	if cityTemp != "" {
		if err := replaceFile(cityTemp, r.cityPath); err != nil {
			return err
		}
	}
	if asnTemp != "" {
		if err := replaceFile(asnTemp, r.asnPath); err != nil {
			return err
		}
	}

	if fileExists(r.cityPath) {
		reader, err := geoip2.Open(r.cityPath)
		if err != nil {
			return err
		}
		r.city = reader
	}
	if fileExists(r.asnPath) {
		reader, err := geoip2.Open(r.asnPath)
		if err != nil {
			if r.city != nil {
				_ = r.city.Close()
				r.city = nil
			}
			return err
		}
		r.asn = reader
	}
	return nil
}

func (r *geoDatabaseResolver) loadReaders() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 国内补充层与 mmdb 互不依赖，mmdb 打不开也照常加载
	r.loadChina()

	if r.city != nil {
		_ = r.city.Close()
		r.city = nil
	}
	if r.asn != nil {
		_ = r.asn.Close()
		r.asn = nil
	}

	if fileExists(r.cityPath) {
		reader, err := geoip2.Open(r.cityPath)
		if err != nil {
			return err
		}
		r.city = reader
	}
	if fileExists(r.asnPath) {
		reader, err := geoip2.Open(r.asnPath)
		if err != nil {
			if r.city != nil {
				_ = r.city.Close()
				r.city = nil
			}
			return err
		}
		r.asn = reader
	}
	return nil
}

func (r *geoDatabaseResolver) optimizedURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if !r.cfg.ChinaOptimized {
		return raw
	}
	mirror := strings.TrimSpace(r.cfg.GitHubMirror)
	if mirror == "" {
		return raw
	}
	if strings.HasPrefix(raw, mirror) {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	if !strings.Contains(host, "github.com") && !strings.Contains(host, "githubusercontent.com") {
		return raw
	}
	return strings.TrimRight(mirror, "/") + "/" + strings.TrimLeft(raw, "/")
}

func (r *geoDatabaseResolver) readMeta() (geoDBMeta, error) {
	data, err := os.ReadFile(r.metaPath)
	if err != nil {
		return geoDBMeta{}, err
	}
	var meta geoDBMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return geoDBMeta{}, err
	}
	return meta, nil
}

func (r *geoDatabaseResolver) writeMeta(meta geoDBMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.metaPath, data, 0o644)
}

func localizedGeoName(names map[string]string) string {
	for _, key := range []string{"zh-CN", "zh", "en"} {
		if value := normalizeChineseGeoName(names[key]); strings.TrimSpace(value) != "" {
			return value
		}
	}
	for _, value := range names {
		if value = normalizeChineseGeoName(value); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeChineseGeoName(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "china":
		return "中国"
	case "hong kong", "hong kong sar":
		return "中国香港"
	case "macau", "macao", "macao sar":
		return "中国澳门"
	case "taiwan":
		return "中国台湾"
	default:
		return value
	}
}

func replaceFile(src, dst string) error {
	if src == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, input, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

func cleanupTempFile(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.Remove(path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return err != nil
}
