package service

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
)

// storagePoolEngine 存储池的运行时状态：各存储配置的健康记录、用量缓存、轮询游标。
//
// 健康记录按存储配置记，不按池记 —— 同一个存储出了问题，在哪个池里都是同一个问题。
// 记录在进程内：Aegis 生产是单实例，多实例时每个实例各自退避，最坏是多试几次，
// 不会写错地方（写入失败本来就会换成员）。
//
// 退避：连续失败第 n 次后冷却 base·2^(n-1)，封顶 max，再加 ±20% 抖动，免得多个成员
// 同时恢复、同一时刻一起被试。冷却期满后先放行一次（半开），成功就清零，失败就翻倍。
// 后台探活会在冷却期满时主动做一次连通检测，不必等真实写入去撞。
type storagePoolEngine struct {
	mu        sync.Mutex
	health    map[int64]*storagedomain.MemberHealth
	usage     map[int64]storagedomain.MemberUsage
	usageAt   time.Time
	cursor    map[int64]uint64
	lastEvent map[string]time.Time

	now         func() time.Time
	backoffBase time.Duration
	backoffMax  time.Duration
}

const (
	storagePoolBackoffBase = 10 * time.Second
	storagePoolBackoffMax  = 15 * time.Minute
	// storageUsageTTL 用量缓存多久重算一次。写入成功后就地累加，所以不必很短。
	storageUsageTTL = time.Minute
	// storageEventDedupe 同一成员同一种事件在这段时间里只记一条。
	storageEventDedupe = 10 * time.Minute
	storageErrorMaxLen = 300
)

func newStoragePoolEngine() *storagePoolEngine {
	return &storagePoolEngine{
		health:      map[int64]*storagedomain.MemberHealth{},
		usage:       map[int64]storagedomain.MemberUsage{},
		cursor:      map[int64]uint64{},
		lastEvent:   map[string]time.Time{},
		now:         time.Now,
		backoffBase: storagePoolBackoffBase,
		backoffMax:  storagePoolBackoffMax,
	}
}

func (e *storagePoolEngine) entry(configID int64) *storagedomain.MemberHealth {
	h, ok := e.health[configID]
	if !ok {
		h = &storagedomain.MemberHealth{}
		e.health[configID] = h
	}
	return h
}

// Health 健康记录的副本。
func (e *storagePoolEngine) Health(configID int64) storagedomain.MemberHealth {
	e.mu.Lock()
	defer e.mu.Unlock()
	if h, ok := e.health[configID]; ok {
		return *h
	}
	return storagedomain.MemberHealth{}
}

// inBackoff 是否在冷却中。冷却期满即视为可试（半开）。
func (e *storagePoolEngine) inBackoff(configID int64, now time.Time) (bool, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	h, ok := e.health[configID]
	if !ok || h.BackoffUntil == nil {
		return false, time.Time{}
	}
	return now.Before(*h.BackoffUntil), *h.BackoffUntil
}

// RecordSuccess 记一次成功；之前在失败状态时返回 true（恢复）。
func (e *storagePoolEngine) RecordSuccess(configID int64, latency time.Duration, probe bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	h := e.entry(configID)
	now := e.now()
	recovered := h.ConsecutiveFailures > 0
	h.ConsecutiveFailures = 0
	h.BackoffUntil = nil
	h.LastLatencyMs = latency.Milliseconds()
	h.LastSuccessAt = &now
	if probe {
		h.LastProbeAt = &now
	} else {
		h.Uploads++
	}
	return recovered
}

// RecordFailure 记一次失败，进入（或延长）退避，返回这次的冷却时长。
func (e *storagePoolEngine) RecordFailure(configID int64, cause error, probe bool) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	h := e.entry(configID)
	now := e.now()
	h.ConsecutiveFailures++
	h.LastFailureAt = &now
	if probe {
		h.LastProbeAt = &now
	} else {
		h.Failures++
	}
	if cause != nil {
		message := cause.Error()
		if len(message) > storageErrorMaxLen {
			message = message[:storageErrorMaxLen]
		}
		h.LastError = message
	}
	cooldown := backoffDuration(e.backoffBase, e.backoffMax, h.ConsecutiveFailures)
	until := now.Add(cooldown)
	h.BackoffUntil = &until
	return cooldown
}

// Reset 手动清除退避。
func (e *storagePoolEngine) Reset(configID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if h, ok := e.health[configID]; ok {
		h.ConsecutiveFailures = 0
		h.BackoffUntil = nil
	}
}

// backoffDuration 第 n 次连续失败后的冷却：base·2^(n-1)，封顶 max，±20% 抖动。
func backoffDuration(base, max time.Duration, failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	exp := math.Min(float64(failures-1), 20)
	d := time.Duration(float64(base) * math.Pow(2, exp))
	if d > max || d <= 0 {
		d = max
	}
	jitter := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(d) * jitter)
}

// ── 用量 ──────────────────────────

// Usage 缓存的用量；是否过期由调用方看 UsageStale。
func (e *storagePoolEngine) Usage(configID int64) (storagedomain.MemberUsage, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	usage, ok := e.usage[configID]
	return usage, ok
}

func (e *storagePoolEngine) UsageStale() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now().Sub(e.usageAt) > storageUsageTTL
}

// SetUsage 整体替换用量缓存。reported 是提供商报告的真实用量（如 R2 分析接口），
// 与索引用量取大者：分析数据按小时采样会滞后，索引又看不到不经 Aegis 写入的对象。
func (e *storagePoolEngine) SetUsage(indexed map[int64]storagedomain.MemberUsage, reported map[int64]storagedomain.MemberUsage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	merged := make(map[int64]storagedomain.MemberUsage, len(indexed)+len(reported))
	for id, usage := range indexed {
		merged[id] = usage
	}
	for id, usage := range reported {
		if current, ok := merged[id]; !ok || usage.UsedBytes > current.UsedBytes {
			merged[id] = usage
		}
	}
	e.usage = merged
	e.usageAt = e.now()
}

// InvalidateUsage 让下一次读用量时重算（迁移挪动了大量对象之后）。
func (e *storagePoolEngine) InvalidateUsage() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.usageAt = time.Time{}
}

// AddUsage 写入成功后就地累加，不必等下一次重算。
func (e *storagePoolEngine) AddUsage(configID int64, bytes int64) storagedomain.MemberUsage {
	e.mu.Lock()
	defer e.mu.Unlock()
	usage := e.usage[configID]
	usage.UsedBytes += bytes
	usage.Objects++
	if usage.Source == "" {
		usage.Source = "index"
	}
	usage.UpdatedAt = e.now()
	e.usage[configID] = usage
	return usage
}

// ShouldEmit 事件去重：同一 key 在去重窗口内只放行一次。
func (e *storagePoolEngine) ShouldEmit(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	if last, ok := e.lastEvent[key]; ok && now.Sub(last) < storageEventDedupe {
		return false
	}
	e.lastEvent[key] = now
	return true
}

// ── 选择 ──────────────────────────

// poolCandidate 计划里的一个成员。
type poolCandidate struct {
	member storagedomain.PoolMember
	config *storagedomain.Config
	step   storagedomain.PoolPlanStep
}

// Plan 按策略给出这次写入的成员顺序，并标出哪些会被跳过、为什么。
//
// active 成员按策略排序，standby 成员按优先级接在后面（所有 active 都不可写时才轮到），
// readonly 成员与停用的配置不参与。
func (e *storagePoolEngine) Plan(pool *storagedomain.Pool, configs map[int64]*storagedomain.Config, size int64) []poolCandidate {
	now := e.now()
	var active, standby, rest []poolCandidate
	for _, member := range pool.Members {
		cfg := configs[member.ConfigID]
		candidate := poolCandidate{member: member, config: cfg, step: storagedomain.PoolPlanStep{ConfigID: member.ConfigID}}
		if cfg != nil {
			candidate.step.Name = cfg.ConfigName
			candidate.step.Provider = cfg.Provider
		}
		switch {
		case cfg == nil:
			candidate.step.Reason = "存储配置已删除"
			rest = append(rest, candidate)
			continue
		case !cfg.Enabled:
			candidate.step.Reason = "存储配置已停用"
			rest = append(rest, candidate)
			continue
		case member.Mode == storagedomain.MemberModeReadOnly:
			candidate.step.Reason = "只读成员不接收写入"
			rest = append(rest, candidate)
			continue
		}
		if backing, until := e.inBackoff(member.ConfigID, now); backing {
			candidate.step.Reason = fmt.Sprintf("连续失败，退避至 %s", until.Format("15:04:05"))
		} else if member.CapacityBytes > 0 {
			usage, _ := e.Usage(member.ConfigID)
			if usage.UsedBytes+size > member.CapacityBytes {
				candidate.step.Reason = "容量已满"
			}
		}
		candidate.step.Eligible = candidate.step.Reason == ""
		if member.Mode == storagedomain.MemberModeStandby {
			standby = append(standby, candidate)
		} else {
			active = append(active, candidate)
		}
	}

	byPriority := func(list []poolCandidate) {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].member.Priority != list[j].member.Priority {
				return list[i].member.Priority < list[j].member.Priority
			}
			return list[i].member.ConfigID < list[j].member.ConfigID
		})
	}
	byPriority(active)
	byPriority(standby)

	switch pool.Strategy {
	case storagedomain.PoolStrategyRoundRobin:
		active = e.rotate(pool.ID, active)
	case storagedomain.PoolStrategyWeighted:
		active = weightedOrder(active)
	case storagedomain.PoolStrategyLeastUsed:
		sort.SliceStable(active, func(i, j int) bool {
			return e.usageRatio(active[i].member) < e.usageRatio(active[j].member)
		})
	}
	// 可写的排前面：策略决定的是可写成员之间的先后，不可写的只为在模拟里展示原因。
	ordered := append(append([]poolCandidate{}, active...), standby...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].step.Eligible && !ordered[j].step.Eligible })
	return append(ordered, rest...)
}

// rotate 轮询：只在可写成员之间轮，游标每次写入前进一步。
func (e *storagePoolEngine) rotate(poolID int64, list []poolCandidate) []poolCandidate {
	eligible := make([]poolCandidate, 0, len(list))
	blocked := make([]poolCandidate, 0)
	for _, c := range list {
		if c.step.Eligible {
			eligible = append(eligible, c)
		} else {
			blocked = append(blocked, c)
		}
	}
	if len(eligible) > 1 {
		e.mu.Lock()
		start := int(e.cursor[poolID] % uint64(len(eligible)))
		e.cursor[poolID]++
		e.mu.Unlock()
		eligible = append(eligible[start:], eligible[:start]...)
	}
	return append(eligible, blocked...)
}

// weightedOrder 按权重的无放回随机排序（Efraimidis–Spirakis：key = u^(1/w)，大的在前）。
func weightedOrder(list []poolCandidate) []poolCandidate {
	type keyed struct {
		c   poolCandidate
		key float64
	}
	items := make([]keyed, len(list))
	for i, c := range list {
		w := float64(c.member.Weight)
		if w <= 0 {
			w = 1
		}
		u := rand.Float64()
		if u == 0 {
			u = math.SmallestNonzeroFloat64
		}
		items[i] = keyed{c, math.Pow(u, 1/w)}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].key > items[j].key })
	out := make([]poolCandidate, len(items))
	for i, item := range items {
		out[i] = item.c
	}
	return out
}

// usageRatio 占用比例；不限容量的成员按 1 PiB 折算，排在有余量的限额成员之后。
func (e *storagePoolEngine) usageRatio(member storagedomain.PoolMember) float64 {
	usage, _ := e.Usage(member.ConfigID)
	capacity := member.CapacityBytes
	if capacity <= 0 {
		capacity = 1 << 50
	}
	return float64(usage.UsedBytes) / float64(capacity)
}

// MemberState 成员当前状态（给控制台看）。
func (e *storagePoolEngine) MemberState(member storagedomain.PoolMember, cfg *storagedomain.Config, warnPercent int) string {
	if cfg == nil || !cfg.Enabled {
		return storagedomain.MemberStateDisabled
	}
	if member.Mode == storagedomain.MemberModeReadOnly {
		return storagedomain.MemberStateReadOnly
	}
	return e.configState(cfg.ID, member.CapacityBytes, warnPercent)
}

// configState 单看一个存储配置的状态（不在池里时容量不限）。
func (e *storagePoolEngine) configState(configID int64, capacity int64, warnPercent int) string {
	now := e.now()
	if backing, _ := e.inBackoff(configID, now); backing {
		return storagedomain.MemberStateBackoff
	}
	if capacity > 0 {
		usage, _ := e.Usage(configID)
		if usage.UsedBytes >= capacity {
			return storagedomain.MemberStateFull
		}
		if warnPercent > 0 && usage.UsedBytes*100 >= capacity*int64(warnPercent) {
			return storagedomain.MemberStateNearFull
		}
	}
	if h := e.Health(configID); h.ConsecutiveFailures > 0 {
		return storagedomain.MemberStateDegraded
	}
	return storagedomain.MemberStateHealthy
}

// ── 错误 ──────────────────────────

// storageBackendError 存储提供商本身出错（网络、鉴权、对方 5xx…）。
//
// 对外仍是原来的 AppError；存储池据此判断该换成员重试，而参数、规则、治理类的错误
// 换哪个成员都一样，直接返回。
type storageBackendError struct {
	*apperrors.AppError
	cause error
}

func (e *storageBackendError) Unwrap() []error { return []error{e.AppError, e.cause} }

func isStorageBackendError(err error) bool {
	var backend *storageBackendError
	return errors.As(err, &backend)
}
