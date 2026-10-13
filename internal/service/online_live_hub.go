package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	admindomain "aegis/internal/domain/admin"
	realtimedomain "aegis/internal/domain/realtime"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// OnlineLiveHub 管理端实时在线统计：一条加密 WebSocket 推送快照、曲线与上下线动态。
//
// 数据怎么来：
//
//   - 快照：有订阅者的实例每个节拍抢一把短锁，抢到的那个算一次全量快照写进
//     Redis，其余实例直接读。集群里同一时刻只有一份计算，订阅者再多也不会
//     把 Redis 打成热点 —— 旧实现是每个控制台标签页十秒一轮询，每次全量清理。
//   - 曲线：每分钟由一个实例写一次采样（全站 + 各应用），保留 retention 天。
//     与有没有人在看无关，否则打开页面时看到的永远是一条空曲线。
//   - 动态：连接上线 / 下线由所在实例经 NATS 广播（RealtimeService.OnPresence），
//     各实例攒半秒一批推给本机订阅者。
//
// 谁能看什么：路由本身要求 system:admin:manage。超级管理员看到全部（含控制台
// 在线的管理员与各接入节点）；其余管理员只看到角色分配覆盖的应用。
type OnlineLiveHub struct {
	log      *zap.Logger
	realtime *RealtimeService
	repo     *redisrepo.RealtimeRepository
	admin    *AdminService
	apps     *AppService
	pg       *pgrepo.Repository
	upgrader websocket.Upgrader

	tick          time.Duration
	dimsEvery     time.Duration
	snapshotTTL   time.Duration
	retention     time.Duration
	scanLimit     int
	revalidate    time.Duration
	maxLifetime   time.Duration
	feedFlush     time.Duration
	feedBatchSize int

	mu   sync.RWMutex
	subs map[*liveSubscriber]struct{}

	snapshot    atomic.Pointer[realtimedomain.OnlineSnapshot]
	dimsMu      sync.Mutex
	dimsAt      time.Time
	dimsCache   *liveDims
	appMetaMu   sync.Mutex
	appMetaAt   time.Time
	appMeta     map[int64]liveAppMeta
	lastSampled int64

	events  chan realtimedomain.PresenceEvent
	dropped atomic.Int64
	stopCh  chan struct{}
	stopped sync.Once
}

type liveAppMeta struct {
	name   string
	appKey string
}

type liveDims struct {
	global    realtimedomain.Dimensions
	apps      map[int64]realtimedomain.Dimensions
	nodes     []realtimedomain.NodeLoad
	truncated bool
}

// NewOnlineLiveHub 构造并启动后台节拍。realtime 为 nil 时返回 nil（实时服务不可用）。
func NewOnlineLiveHub(log *zap.Logger, realtime *RealtimeService, admin *AdminService, apps *AppService, pg *pgrepo.Repository, allowedOrigins []string) *OnlineLiveHub {
	hub := newOnlineLiveHub(log, realtime, admin, apps, pg, allowedOrigins)
	hub.start()
	return hub
}

func newOnlineLiveHub(log *zap.Logger, realtime *RealtimeService, admin *AdminService, apps *AppService, pg *pgrepo.Repository, allowedOrigins []string) *OnlineLiveHub {
	if realtime == nil || realtime.Repository() == nil {
		return nil
	}
	if log == nil {
		log = zap.NewNop()
	}
	hub := &OnlineLiveHub{
		log:      log.Named("online-live"),
		realtime: realtime,
		repo:     realtime.Repository(),
		admin:    admin,
		apps:     apps,
		pg:       pg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 8192,
			CheckOrigin:     WebSocketOriginChecker(allowedOrigins),
			// 只回显 "aegis"，令牌子协议绝不回显，理由见 RealtimeSubprotocol
			Subprotocols: []string{RealtimeSubprotocol},
		},
		tick:          2 * time.Second,
		dimsEvery:     10 * time.Second,
		snapshotTTL:   15 * time.Second,
		retention:     7 * 24 * time.Hour,
		scanLimit:     50_000,
		revalidate:    30 * time.Second,
		maxLifetime:   time.Hour,
		feedFlush:     500 * time.Millisecond,
		feedBatchSize: 100,
		subs:          make(map[*liveSubscriber]struct{}),
		events:        make(chan realtimedomain.PresenceEvent, 2048),
		stopCh:        make(chan struct{}),
	}
	return hub
}

func (h *OnlineLiveHub) start() {
	if h == nil {
		return
	}
	h.realtime.OnPresence(h.onPresence)
	go h.run()
	go h.runFeed()
}

// Close 停止节拍并断开全部订阅者。
func (h *OnlineLiveHub) Close() {
	if h == nil {
		return
	}
	h.stopped.Do(func() { close(h.stopCh) })
	h.mu.RLock()
	subs := make([]*liveSubscriber, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
	}
	h.mu.RUnlock()
	for _, sub := range subs {
		sub.closeWith(websocket.CloseGoingAway, "server shutdown")
	}
}

// ── 后台节拍 ──

func (h *OnlineLiveHub) run() {
	ticker := time.NewTicker(h.tick)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopCh:
			return
		case now := <-ticker.C:
			h.sample(now)
			if h.subscriberCount() > 0 {
				h.refreshSnapshot(false)
			}
		}
	}
}

// sample 每分钟一个采样点。按分钟抢锁，整个集群只写一次。
func (h *OnlineLiveHub) sample(now time.Time) {
	minute := now.UTC().Truncate(time.Minute).Unix()
	if minute == h.lastSampled {
		return
	}
	h.lastSampled = minute
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok, err := h.repo.TryLock(ctx, "sample:"+strconv.FormatInt(minute, 10), 2*time.Minute)
	if err != nil || !ok {
		return
	}
	users, conns, err := h.repo.GlobalCounts(ctx)
	if err != nil {
		h.log.Debug("sample global counts failed", zap.Error(err))
		return
	}
	admins, _, _ := h.repo.AdminCounts(ctx)
	apps, err := h.repo.ActiveApps(ctx)
	if err != nil {
		h.log.Debug("sample app counts failed", zap.Error(err))
		return
	}
	point := realtimedomain.SeriesPoint{At: minute, Users: users, Connections: conns, Admins: admins}
	if err := h.repo.WriteSample(ctx, point, apps, h.retention); err != nil {
		h.log.Debug("write online sample failed", zap.Error(err))
	}
}

// refreshSnapshot 抢到锁就算一份新快照，否则读别人算好的；有新版本就推给订阅者。
func (h *OnlineLiveHub) refreshSnapshot(force bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var snap *realtimedomain.OnlineSnapshot
	// 锁比节拍略短：持锁的实例下一拍还能续上，挂掉时别人最多晚一拍接手。
	// 必须是正数 —— 非正的过期时间在 go-redis 里等于永不过期，锁会被永久占住。
	ok, err := h.repo.TryLock(ctx, "snapshot", max(h.tick*3/4, 50*time.Millisecond))
	if err == nil && ok {
		snap, err = h.compute(ctx)
		if err != nil {
			h.log.Debug("compute online snapshot failed", zap.Error(err))
			return
		}
		if raw, err := json.Marshal(snap); err == nil {
			_ = h.repo.StoreSnapshot(ctx, raw, h.snapshotTTL)
		}
	} else {
		raw, err := h.repo.LoadSnapshot(ctx)
		if err != nil || raw == nil {
			if force {
				// 刚连上来且还没有任何快照（锁被别人拿着但对方尚未写出）：自己算一份顶上
				snap, err = h.compute(ctx)
				if err != nil {
					return
				}
			} else {
				return
			}
		} else {
			snap = &realtimedomain.OnlineSnapshot{}
			if err := json.Unmarshal(raw, snap); err != nil {
				return
			}
		}
	}
	prev := h.snapshot.Load()
	if !force && prev != nil && prev.Version >= snap.Version {
		return
	}
	h.snapshot.Store(snap)
	for _, sub := range h.subscribers() {
		sub.pushSnapshot(snap)
	}
}

func (h *OnlineLiveHub) compute(ctx context.Context) (*realtimedomain.OnlineSnapshot, error) {
	users, conns, err := h.repo.GlobalCounts(ctx)
	if err != nil {
		return nil, err
	}
	active, err := h.repo.ActiveApps(ctx)
	if err != nil {
		return nil, err
	}
	admins, adminConns, err := h.repo.AdminCounts(ctx)
	if err != nil {
		return nil, err
	}
	version, err := h.repo.NextSnapshotVersion(ctx)
	if err != nil {
		return nil, err
	}
	meta := h.appMetadata(ctx)
	snap := &realtimedomain.OnlineSnapshot{
		Version: version,
		At:      time.Now().UTC(),
		Totals:  realtimedomain.OnlineTotals{Users: users, Connections: conns, Apps: int64(len(active))},
		Apps:    make([]realtimedomain.AppOnlineSummary, 0, len(active)),
		Admins:  &realtimedomain.AdminOnlineTotals{Admins: admins, Connections: adminConns},
	}
	for _, app := range active {
		row := realtimedomain.AppOnlineSummary{AppID: app.AppID, Users: app.Users, Connections: app.Connections}
		if m, ok := meta[app.AppID]; ok {
			row.Name, row.AppKey = m.name, m.appKey
		}
		snap.Apps = append(snap.Apps, row)
	}
	dims := h.dimensions(ctx)
	if dims != nil {
		snap.Dims = dims.global
		snap.AppDims = dims.apps
		snap.Nodes = dims.nodes
		snap.Truncated = dims.truncated
	}
	return snap, nil
}

// dimensions 分布要扫描每条连接的元数据，比计数贵得多，单独按更慢的节拍重算。
func (h *OnlineLiveHub) dimensions(ctx context.Context) *liveDims {
	h.dimsMu.Lock()
	defer h.dimsMu.Unlock()
	if h.dimsCache != nil && time.Since(h.dimsAt) < h.dimsEvery {
		return h.dimsCache
	}
	global := newDimCounter()
	perApp := make(map[int64]*dimCounter)
	nodes := make(map[string]*realtimedomain.NodeLoad)
	node := func(id string) *realtimedomain.NodeLoad {
		if id == "" {
			id = "unknown"
		}
		n := nodes[id]
		if n == nil {
			n = &realtimedomain.NodeLoad{ServerID: id}
			nodes[id] = n
		}
		return n
	}
	scanned := 0
	total, err := h.repo.ScanConnections(ctx, h.scanLimit, func(conn realtimedomain.PresenceConnection) {
		scanned++
		global.add(conn)
		counter := perApp[conn.AppID]
		if counter == nil {
			counter = newDimCounter()
			perApp[conn.AppID] = counter
		}
		counter.add(conn)
		node(conn.ServerID).Connections++
	})
	if err != nil {
		h.log.Debug("scan online connections failed", zap.Error(err))
		return h.dimsCache
	}
	_, _ = h.repo.ScanAdminConnections(ctx, 5000, func(conn realtimedomain.PresenceConnection) {
		node(conn.ServerID).Admins++
	})
	out := &liveDims{
		global:    global.result(12),
		apps:      make(map[int64]realtimedomain.Dimensions, len(perApp)),
		truncated: total > int64(scanned),
	}
	for appID, counter := range perApp {
		out.apps[appID] = counter.result(8)
	}
	for _, n := range nodes {
		out.nodes = append(out.nodes, *n)
	}
	sort.Slice(out.nodes, func(i, j int) bool { return out.nodes[i].Connections > out.nodes[j].Connections })
	h.dimsCache = out
	h.dimsAt = time.Now()
	return out
}

// appMetadata 应用名称缓存一分钟：快照每两秒一份，没必要每份都查库。
func (h *OnlineLiveHub) appMetadata(ctx context.Context) map[int64]liveAppMeta {
	h.appMetaMu.Lock()
	defer h.appMetaMu.Unlock()
	if h.appMeta != nil && time.Since(h.appMetaAt) < time.Minute {
		return h.appMeta
	}
	if h.apps == nil {
		return h.appMeta
	}
	items, err := h.apps.ListApps(ctx)
	if err != nil {
		return h.appMeta
	}
	meta := make(map[int64]liveAppMeta, len(items))
	for _, app := range items {
		meta[app.ID] = liveAppMeta{name: app.Name, appKey: app.AppKey}
	}
	h.appMeta = meta
	h.appMetaAt = time.Now()
	return meta
}

// ── 上下线动态 ──

func (h *OnlineLiveHub) onPresence(evt realtimedomain.PresenceEvent) {
	select {
	case h.events <- evt:
	default:
		h.dropped.Add(1)
	}
}

func (h *OnlineLiveHub) runFeed() {
	ticker := time.NewTicker(h.feedFlush)
	defer ticker.Stop()
	var batch []realtimedomain.PresenceEvent
	for {
		select {
		case <-h.stopCh:
			return
		case evt := <-h.events:
			// 窗口内超出上限的事件只计数不下发：瞬时上万人重连时，
			// 控制台要的是「发生了大量上下线」，不是一万行滚动。
			if len(batch) < h.feedBatchSize {
				batch = append(batch, evt)
			} else {
				h.dropped.Add(1)
			}
		case <-ticker.C:
			dropped := h.dropped.Swap(0)
			if len(batch) == 0 && dropped == 0 {
				continue
			}
			for _, sub := range h.subscribers() {
				sub.pushFeed(batch, dropped)
			}
			batch = nil
		}
	}
}

// ── 订阅者登记 ──

func (h *OnlineLiveHub) register(sub *liveSubscriber) {
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
}

func (h *OnlineLiveHub) unregister(sub *liveSubscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

func (h *OnlineLiveHub) subscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

func (h *OnlineLiveHub) subscribers() []*liveSubscriber {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*liveSubscriber, 0, len(h.subs))
	for sub := range h.subs {
		out = append(out, sub)
	}
	return out
}

// ── 连接入口 ──

// Serve 升级连接、完成密钥交换，然后进入收发循环。access 由 AdminAccess 中间件校验过。
func (h *OnlineLiveHub) Serve(w http.ResponseWriter, req *http.Request, access *admindomain.AccessContext) error {
	if h == nil {
		return stderrors.New("实时在线统计不可用")
	}
	token := ExtractRealtimeToken(req)
	if token == "" {
		token = strings.TrimSpace(req.Header.Get("X-Admin-Token"))
	}
	conn, err := h.upgrader.Upgrade(w, req, nil)
	if err != nil {
		return err
	}
	conn.SetReadLimit(64 << 10)
	sub := &liveSubscriber{
		hub:     h,
		conn:    conn,
		token:   token,
		send:    make(chan []byte, 64),
		done:    make(chan struct{}),
		started: time.Now(),
	}
	sub.applyAccess(access)
	channel, err := sub.handshake()
	if err != nil {
		h.log.Debug("online live handshake failed", zap.Error(err))
		sub.closeWith(liveCloseHandshake, "handshake failed")
		return nil
	}
	sub.channel = channel
	h.register(sub)
	go sub.writePump()
	sub.sendJSON(sub.readyFrame())
	if snap := h.snapshot.Load(); snap != nil && time.Since(snap.At) < h.snapshotTTL {
		sub.pushSnapshot(snap)
	} else {
		go h.refreshSnapshot(true)
	}
	go sub.watch()
	sub.readPump()
	return nil
}

// ── 维度计数 ──

type dimCounter struct {
	platform, os, appVersion, country, region, device map[string]int64
}

func newDimCounter() *dimCounter {
	return &dimCounter{
		platform:   map[string]int64{},
		os:         map[string]int64{},
		appVersion: map[string]int64{},
		country:    map[string]int64{},
		region:     map[string]int64{},
		device:     map[string]int64{},
	}
}

func (d *dimCounter) add(conn realtimedomain.PresenceConnection) {
	d.platform[normalizePlatform(conn.Platform)]++
	osKey := normalizePlatform(conn.OS)
	if osKey != "" && conn.OSVersion != "" {
		osKey += " " + majorVersion(conn.OSVersion)
	}
	d.os[osKey]++
	d.appVersion[strings.TrimSpace(conn.AppVersion)]++
	d.country[strings.TrimSpace(conn.Country)]++
	region := strings.TrimSpace(conn.Region)
	if region == "" {
		region = strings.TrimSpace(conn.Country)
	}
	d.region[region]++
	device := strings.TrimSpace(conn.DeviceName)
	if device == "" {
		device = strings.TrimSpace(conn.DeviceModel)
	}
	d.device[device]++
}

func (d *dimCounter) result(top int) realtimedomain.Dimensions {
	return realtimedomain.Dimensions{
		Platform:   topBuckets(d.platform, top),
		OS:         topBuckets(d.os, top),
		AppVersion: topBuckets(d.appVersion, top),
		Country:    topBuckets(d.country, top),
		Region:     topBuckets(d.region, top),
		Device:     topBuckets(d.device, top),
	}
}

// liveOtherBucket 截断后剩余项合并成的那一桶；空串键表示客户端未上报。
const liveOtherBucket = "*"

func topBuckets(counts map[string]int64, top int) []realtimedomain.Bucket {
	out := make([]realtimedomain.Bucket, 0, len(counts))
	for key, count := range counts {
		out = append(out, realtimedomain.Bucket{Key: key, Count: count})
	}
	sortBuckets(out)
	if len(out) <= top {
		return out
	}
	var rest int64
	for _, b := range out[top:] {
		rest += b.Count
	}
	out = append(out[:top:top], realtimedomain.Bucket{Key: liveOtherBucket, Count: rest})
	return out
}

func sortBuckets(out []realtimedomain.Bucket) {
	sort.Slice(out, func(i, j int) bool {
		// 「未上报」与「其他」排在末尾，其余按数量降序
		ki, kj := out[i].Key == "" || out[i].Key == liveOtherBucket, out[j].Key == "" || out[j].Key == liveOtherBucket
		if ki != kj {
			return !ki
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
}

// mergeDimensions 非全局管理员看到的分布是其可见应用的分布之和。
// 每个应用只保留了前若干项，合并结果是近似值（「其他」桶会偏大），对概览足够。
func mergeDimensions(parts []realtimedomain.Dimensions, top int) realtimedomain.Dimensions {
	merge := func(pick func(realtimedomain.Dimensions) []realtimedomain.Bucket) []realtimedomain.Bucket {
		counts := map[string]int64{}
		for _, p := range parts {
			for _, b := range pick(p) {
				counts[b.Key] += b.Count
			}
		}
		other := counts[liveOtherBucket]
		delete(counts, liveOtherBucket)
		out := topBuckets(counts, top)
		if other > 0 {
			if n := len(out); n > 0 && out[n-1].Key == liveOtherBucket {
				out[n-1].Count += other
			} else {
				out = append(out, realtimedomain.Bucket{Key: liveOtherBucket, Count: other})
			}
		}
		return out
	}
	return realtimedomain.Dimensions{
		Platform:   merge(func(d realtimedomain.Dimensions) []realtimedomain.Bucket { return d.Platform }),
		OS:         merge(func(d realtimedomain.Dimensions) []realtimedomain.Bucket { return d.OS }),
		AppVersion: merge(func(d realtimedomain.Dimensions) []realtimedomain.Bucket { return d.AppVersion }),
		Country:    merge(func(d realtimedomain.Dimensions) []realtimedomain.Bucket { return d.Country }),
		Region:     merge(func(d realtimedomain.Dimensions) []realtimedomain.Bucket { return d.Region }),
		Device:     merge(func(d realtimedomain.Dimensions) []realtimedomain.Bucket { return d.Device }),
	}
}

var platformNames = map[string]string{
	"android":   "Android",
	"ios":       "iOS",
	"ipados":    "iPadOS",
	"harmonyos": "HarmonyOS",
	"harmony":   "HarmonyOS",
	"ohos":      "HarmonyOS",
	"windows":   "Windows",
	"macos":     "macOS",
	"mac":       "macOS",
	"darwin":    "macOS",
	"linux":     "Linux",
	"web":       "Web",
	"browser":   "Web",
	"flutter":   "Flutter",
	"other":     "Other",
}

// normalizePlatform 客户端自报的平台大小写五花八门（android / Android / ANDROID），统一成一种写法。
func normalizePlatform(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if name, ok := platformNames[strings.ToLower(value)]; ok {
		return name
	}
	return value
}

func majorVersion(version string) string {
	version = strings.TrimSpace(version)
	if i := strings.IndexAny(version, "._ "); i > 0 {
		return version[:i]
	}
	return version
}
