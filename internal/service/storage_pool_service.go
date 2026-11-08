package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// ════════════════════════════════════════════════════════════
//  写入目标
// ════════════════════════════════════════════════════════════

// writeTarget 一次写入的目标：单个存储配置，或一个存储池。
type writeTarget struct {
	config *storagedomain.Config
	pool   *storagedomain.Pool
}

// resolveWriteTarget 决定写到哪里。
//
// 指定了名称：先按存储配置名找（应用级优先、平台级其次），找不到再按池名找。
// 没指定：应用有自己的存储配置就用应用的（原有行为不变）；否则走默认池；
// 没有默认池再用平台默认配置。
func (s *StorageService) resolveWriteTarget(ctx context.Context, appID int64, name string) (writeTarget, error) {
	name = strings.TrimSpace(name)
	if name != "" {
		cfg, err := s.resolveConfig(ctx, storagedomain.ResolveOptions{AppID: appID, ConfigName: name})
		if err == nil {
			return writeTarget{config: cfg}, nil
		}
		pool, poolErr := s.pg.GetStoragePoolByName(ctx, name)
		if poolErr == nil && pool.Enabled {
			return writeTarget{pool: pool}, nil
		}
		return writeTarget{}, err
	}
	if appID > 0 {
		cfg, err := s.pg.ResolveScopedStorageConfig(ctx, storagedomain.ScopeApp, appID, "")
		if err != nil {
			return writeTarget{}, err
		}
		if cfg != nil {
			return writeTarget{config: cfg}, nil
		}
	}
	pool, err := s.pg.DefaultStoragePool(ctx)
	if err == nil && len(pool.Members) > 0 {
		return writeTarget{pool: pool}, nil
	}
	if err != nil && !errors.Is(err, pgrepo.ErrStoragePoolNotFound) {
		return writeTarget{}, err
	}
	cfg, err := s.resolveConfig(ctx, storagedomain.ResolveOptions{AppID: appID})
	if err != nil {
		return writeTarget{}, err
	}
	return writeTarget{config: cfg}, nil
}

// ════════════════════════════════════════════════════════════
//  池写入：选成员、失败换成员、满了换成员
// ════════════════════════════════════════════════════════════

func (s *StorageService) uploadToPool(ctx context.Context, appID int64, pool *storagedomain.Pool, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	content, err := newRewindableContent(input.Content, input.ContentLength)
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, "读取上传内容失败")
	}
	defer content.cleanup()
	input.ContentLength = content.size

	configs, err := s.poolConfigs(ctx, pool)
	if err != nil {
		return nil, err
	}
	s.ensureUsage(ctx)
	plan := s.pools.Plan(pool, configs, content.size)

	attempts := 0
	maxAttempts := pool.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = storagedomain.PoolDefaultAttempts
	}
	var failed []string
	var lastErr error
	for _, candidate := range plan {
		if !candidate.step.Eligible {
			continue
		}
		if attempts >= maxAttempts {
			break
		}
		attempts++
		if err := content.Rewind(); err != nil {
			return nil, err
		}
		attempt := input
		attempt.Content = content.reader
		item, err := s.uploadWithConfig(ctx, appID, candidate.config, attempt)
		if err != nil {
			if !isStorageBackendError(err) {
				// 规则、参数、治理类错误：换哪个成员都一样。
				return nil, err
			}
			failed = append(failed, candidate.config.ConfigName)
			lastErr = err
			continue
		}
		if len(failed) > 0 {
			s.emitPoolEvent(ctx, pool.ID, candidate.config.ID, storagedomain.EventLevelWarn, storagedomain.EventFailover,
				fmt.Sprintf("写入 %s 失败，已改写到 %s", strings.Join(failed, "、"), candidate.config.ConfigName),
				map[string]any{"failed": failed, "objectKey": item.Key}, "")
		}
		usage := s.pools.AddUsage(candidate.config.ID, content.size)
		s.checkCapacity(ctx, pool, candidate.member, candidate.config, usage)
		return item, nil
	}

	if attempts == 0 {
		full := true
		for _, candidate := range plan {
			if candidate.config != nil && candidate.config.Enabled && candidate.member.Mode != storagedomain.MemberModeReadOnly &&
				candidate.step.Reason != "容量已满" {
				full = false
			}
		}
		if full {
			s.emitPoolEvent(ctx, pool.ID, 0, storagedomain.EventLevelError, storagedomain.EventExhausted,
				fmt.Sprintf("存储池 %s 所有成员都已写满", pool.Name), nil, "full")
			return nil, apperrors.New(50780, http.StatusInsufficientStorage, "存储空间已满，请联系管理员扩容")
		}
		s.emitPoolEvent(ctx, pool.ID, 0, storagedomain.EventLevelError, storagedomain.EventExhausted,
			fmt.Sprintf("存储池 %s 暂无可写成员", pool.Name), nil, "unavailable")
		return nil, apperrors.New(50383, http.StatusServiceUnavailable, "存储服务暂时不可用，请稍后重试")
	}
	s.emitPoolEvent(ctx, pool.ID, 0, storagedomain.EventLevelError, storagedomain.EventExhausted,
		fmt.Sprintf("存储池 %s 尝试了 %d 个成员均写入失败", pool.Name, attempts),
		map[string]any{"failed": failed}, "failed")
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, apperrors.New(50383, http.StatusServiceUnavailable, "存储服务暂时不可用，请稍后重试")
}

// poolConfigs 池成员对应的存储配置。
func (s *StorageService) poolConfigs(ctx context.Context, pool *storagedomain.Pool) (map[int64]*storagedomain.Config, error) {
	configs := make(map[int64]*storagedomain.Config, len(pool.Members))
	for _, member := range pool.Members {
		cfg, err := s.pg.GetStorageConfigByID(ctx, member.ConfigID)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			configs[member.ConfigID] = cfg
		}
	}
	return configs, nil
}

// checkCapacity 写入后看是否越过预警线或写满，越过时记事件（去重）。
func (s *StorageService) checkCapacity(ctx context.Context, pool *storagedomain.Pool, member storagedomain.PoolMember, cfg *storagedomain.Config, usage storagedomain.MemberUsage) {
	if member.CapacityBytes <= 0 {
		return
	}
	detail := map[string]any{"usedBytes": usage.UsedBytes, "capacityBytes": member.CapacityBytes}
	switch {
	case usage.UsedBytes >= member.CapacityBytes:
		s.emitPoolEvent(ctx, pool.ID, cfg.ID, storagedomain.EventLevelWarn, storagedomain.EventFull,
			fmt.Sprintf("%s 已写满（%s / %s），之后的写入转到其他成员", cfg.ConfigName,
				formatStorageBytes(usage.UsedBytes), formatStorageBytes(member.CapacityBytes)), detail, "")
	case pool.WarnPercent > 0 && usage.UsedBytes*100 >= member.CapacityBytes*int64(pool.WarnPercent):
		s.emitPoolEvent(ctx, pool.ID, cfg.ID, storagedomain.EventLevelWarn, storagedomain.EventNearFull,
			fmt.Sprintf("%s 已用 %d%%（%s / %s）", cfg.ConfigName, usage.UsedBytes*100/member.CapacityBytes,
				formatStorageBytes(usage.UsedBytes), formatStorageBytes(member.CapacityBytes)), detail, "")
	}
}

// recordBackendSuccess / recordBackendFailure 每次真实写入都记健康：不在池里的配置
// 也有状态可看，加进池时不必从零开始。
func (s *StorageService) recordBackendSuccess(ctx context.Context, cfg *storagedomain.Config, latency time.Duration) {
	if s.pools.RecordSuccess(cfg.ID, latency, false) {
		s.emitPoolEvent(ctx, 0, cfg.ID, storagedomain.EventLevelInfo, storagedomain.EventRecovered,
			fmt.Sprintf("%s 已恢复写入", cfg.ConfigName), nil, "")
	}
}

func (s *StorageService) recordBackendFailure(ctx context.Context, cfg *storagedomain.Config, cause error) {
	cooldown := s.pools.RecordFailure(cfg.ID, cause, false)
	health := s.pools.Health(cfg.ID)
	s.emitPoolEvent(ctx, 0, cfg.ID, storagedomain.EventLevelWarn, storagedomain.EventBackoff,
		fmt.Sprintf("%s 连续失败 %d 次，暂停写入 %s", cfg.ConfigName, health.ConsecutiveFailures, formatStorageDuration(cooldown)),
		map[string]any{"error": health.LastError, "cooldownSeconds": int64(cooldown.Seconds())},
		fmt.Sprintf("%d", health.ConsecutiveFailures))
}

// emitPoolEvent 记一条事件。dedupeSuffix 参与去重键：同一成员同一种事件在去重窗口内只记一条，
// 后缀不同（例如失败次数变了）则照记。事件写失败只打日志，绝不影响写入本身。
func (s *StorageService) emitPoolEvent(ctx context.Context, poolID, configID int64, level, kind, message string, detail map[string]any, dedupeSuffix string) {
	key := fmt.Sprintf("%d:%d:%s:%s", poolID, configID, kind, dedupeSuffix)
	if !s.pools.ShouldEmit(key) {
		return
	}
	event := storagedomain.PoolEvent{Level: level, Kind: kind, Message: message, Detail: detail}
	if poolID > 0 {
		event.PoolID = &poolID
	}
	if configID > 0 {
		event.ConfigID = &configID
	}
	// 事件不该被请求取消带走：写入失败的那一刻恰恰是最需要留下记录的时候。
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.pg.InsertStoragePoolEvent(writeCtx, event); err != nil {
		s.log.Warn("存储池事件写入失败", zap.String("kind", kind), zap.Error(err))
	}
	switch level {
	case storagedomain.EventLevelError:
		s.log.Error("存储池事件", zap.String("kind", kind), zap.String("message", message))
	case storagedomain.EventLevelWarn:
		s.log.Warn("存储池事件", zap.String("kind", kind), zap.String("message", message))
	default:
		s.log.Info("存储池事件", zap.String("kind", kind), zap.String("message", message))
	}
}

// ensureUsage 用量缓存过期时重算（索引汇总，一条 GROUP BY）。提供商报告的真实用量
// 只在后台循环里取 —— 那是一次外部 API 调用，不能挂在写入热路径上。
func (s *StorageService) ensureUsage(ctx context.Context) {
	if !s.pools.UsageStale() {
		return
	}
	indexed, err := s.pg.StorageConfigUsage(ctx)
	if err != nil {
		s.log.Warn("存储用量统计失败", zap.Error(err))
		return
	}
	s.pools.SetUsage(indexed, s.reportedUsageSnapshot())
}

// ════════════════════════════════════════════════════════════
//  读取：转向与池内回退
// ════════════════════════════════════════════════════════════

// openStoredObject 打开对象，必要时去别处找：
//
//  1. 配置启用时先在原处读；
//  2. 原处找不到（或配置已停用）且设了读取转向，去转向的配置读 —— 迁移就靠这一步，
//     引用里记的仍是源配置；
//  3. 还找不到，且原配置在开启了读取回退的池里，去池里其他成员读同一个键。
//
// 只有「对象不存在」才往下找；网络、鉴权这类故障原样返回，免得把一次抖动放大成一串外部请求。
func (s *StorageService) openStoredObject(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, *storagedomain.Config, error) {
	var firstErr error
	visited := map[int64]bool{}
	try := func(candidate *storagedomain.Config) (*storagedomain.ObjectReader, bool, error) {
		if candidate == nil || visited[candidate.ID] || !candidate.Enabled {
			return nil, false, nil
		}
		visited[candidate.ID] = true
		provider, err := s.buildProvider(candidate)
		if err != nil {
			return nil, false, err
		}
		reader, err := provider.Open(ctx, candidate, objectKey)
		if err == nil {
			return reader, true, nil
		}
		return nil, false, err
	}

	if cfg.Enabled {
		reader, ok, err := try(cfg)
		if ok {
			return reader, cfg, nil
		}
		if err != nil && !isStorageObjectNotFound(err) {
			return nil, nil, err
		}
		firstErr = err
	}
	visited[cfg.ID] = true

	// 转向链：A → B → C，至多走几步，防止配置成环。
	current := cfg
	for hops := 0; hops < 4 && current.RedirectConfigID != nil; hops++ {
		next, err := s.pg.GetStorageConfigByID(ctx, *current.RedirectConfigID)
		if err != nil || next == nil {
			break
		}
		reader, ok, err := try(next)
		if ok {
			return reader, next, nil
		}
		if err != nil && !isStorageObjectNotFound(err) && firstErr == nil {
			firstErr = err
		}
		current = next
	}

	if siblings, err := s.fallbackSiblings(ctx, cfg.ID); err == nil {
		for _, sibling := range siblings {
			reader, ok, _ := try(sibling)
			if ok {
				return reader, sibling, nil
			}
		}
	}
	if firstErr == nil {
		firstErr = storageObjectMissingError{apperrors.New(40481, http.StatusNotFound, "资源不可用")}
	}
	return nil, nil, firstErr
}

// fallbackSiblings 与 configID 同在开启了读取回退的池里的其他成员。
func (s *StorageService) fallbackSiblings(ctx context.Context, configID int64) ([]*storagedomain.Config, error) {
	pools, err := s.pg.ListStoragePools(ctx)
	if err != nil {
		return nil, err
	}
	var result []*storagedomain.Config
	seen := map[int64]bool{configID: true}
	for _, pool := range pools {
		if !pool.Enabled || !pool.ReadFallback || !poolHasMember(&pool, configID) {
			continue
		}
		for _, member := range pool.Members {
			if seen[member.ConfigID] {
				continue
			}
			seen[member.ConfigID] = true
			cfg, err := s.pg.GetStorageConfigByID(ctx, member.ConfigID)
			if err == nil && cfg != nil {
				result = append(result, cfg)
			}
		}
	}
	return result, nil
}

func poolHasMember(pool *storagedomain.Pool, configID int64) bool {
	for _, member := range pool.Members {
		if member.ConfigID == configID {
			return true
		}
	}
	return false
}

// ════════════════════════════════════════════════════════════
//  池管理
// ════════════════════════════════════════════════════════════

var storagePoolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ListPools 全部池，带成员的运行状态。
func (s *StorageService) ListPools(ctx context.Context) ([]storagedomain.Pool, error) {
	pools, err := s.pg.ListStoragePools(ctx)
	if err != nil {
		return nil, err
	}
	s.ensureUsage(ctx)
	for i := range pools {
		if err := s.decoratePool(ctx, &pools[i]); err != nil {
			return nil, err
		}
	}
	return pools, nil
}

// GetPool 一个池。
func (s *StorageService) GetPool(ctx context.Context, id int64) (*storagedomain.Pool, error) {
	pool, err := s.pg.GetStoragePool(ctx, id)
	if err != nil {
		return nil, mapStoragePoolError(err)
	}
	s.ensureUsage(ctx)
	if err := s.decoratePool(ctx, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

func (s *StorageService) decoratePool(ctx context.Context, pool *storagedomain.Pool) error {
	configs, err := s.poolConfigs(ctx, pool)
	if err != nil {
		return err
	}
	for i := range pool.Members {
		member := &pool.Members[i]
		cfg := configs[member.ConfigID]
		if cfg != nil {
			member.Config = &storagedomain.PoolMemberConfig{Name: cfg.ConfigName, Provider: cfg.Provider, Enabled: cfg.Enabled, AccessMode: cfg.AccessMode}
		}
		usage, _ := s.pools.Usage(member.ConfigID)
		member.Usage = &usage
		health := s.pools.Health(member.ConfigID)
		member.Health = &health
		member.State = s.pools.MemberState(*member, cfg, pool.WarnPercent)
	}
	return nil
}

// SavePool 新建（id = 0）或修改池。
func (s *StorageService) SavePool(ctx context.Context, id int64, input storagedomain.PoolInput) (*storagedomain.Pool, error) {
	if err := s.normalizePoolInput(ctx, &input); err != nil {
		return nil, err
	}
	saved, err := s.pg.SaveStoragePool(ctx, id, input)
	if err != nil {
		return nil, mapStoragePoolError(err)
	}
	action := "已创建"
	if id > 0 {
		action = "已更新"
	}
	s.emitPoolEvent(ctx, saved.ID, 0, storagedomain.EventLevelInfo, storagedomain.EventConfig,
		fmt.Sprintf("存储池 %s %s（%d 个成员，%s）", saved.Name, action, len(saved.Members), poolStrategyLabel(saved.Strategy)),
		nil, fmt.Sprintf("%d", time.Now().UnixNano()))
	return s.GetPool(ctx, saved.ID)
}

func (s *StorageService) normalizePoolInput(ctx context.Context, input *storagedomain.PoolInput) error {
	input.Name = strings.ToLower(strings.TrimSpace(input.Name))
	input.Description = strings.TrimSpace(input.Description)
	if !storagePoolNamePattern.MatchString(input.Name) {
		return apperrors.New(42285, http.StatusUnprocessableEntity, "池名称只能包含小写字母、数字、点、下划线与连字符，且以字母或数字开头，至多 64 位")
	}
	if len(input.Description) > 500 {
		return apperrors.New(42286, http.StatusUnprocessableEntity, "描述不能超过 500 字")
	}
	switch input.Strategy {
	case "":
		input.Strategy = storagedomain.PoolStrategyPriority
	case storagedomain.PoolStrategyPriority, storagedomain.PoolStrategyRoundRobin,
		storagedomain.PoolStrategyWeighted, storagedomain.PoolStrategyLeastUsed:
	default:
		return apperrors.New(42287, http.StatusUnprocessableEntity, "不支持的选择策略")
	}
	if input.MaxAttempts == 0 {
		input.MaxAttempts = storagedomain.PoolDefaultAttempts
	}
	if input.MaxAttempts < 1 || input.MaxAttempts > storagedomain.PoolMaxAttempts {
		return apperrors.New(42288, http.StatusUnprocessableEntity, fmt.Sprintf("单次写入的最多尝试次数需在 1 到 %d 之间", storagedomain.PoolMaxAttempts))
	}
	if input.WarnPercent == 0 {
		input.WarnPercent = storagedomain.PoolDefaultWarnPct
	}
	if input.WarnPercent < 1 || input.WarnPercent > 100 {
		return apperrors.New(42289, http.StatusUnprocessableEntity, "容量预警比例需在 1 到 100 之间")
	}
	if len(input.Members) > storagedomain.PoolMaxMembers {
		return apperrors.New(42293, http.StatusUnprocessableEntity, fmt.Sprintf("一个池至多 %d 个成员", storagedomain.PoolMaxMembers))
	}
	seen := map[int64]bool{}
	writable := 0
	for i := range input.Members {
		member := &input.Members[i]
		if seen[member.ConfigID] {
			return apperrors.New(42294, http.StatusUnprocessableEntity, "同一个存储配置不能重复加入一个池")
		}
		seen[member.ConfigID] = true
		cfg, err := s.pg.GetStorageConfigByID(ctx, member.ConfigID)
		if err != nil {
			return err
		}
		if cfg == nil {
			return apperrors.New(42295, http.StatusUnprocessableEntity, fmt.Sprintf("存储配置 #%d 不存在", member.ConfigID))
		}
		if cfg.Scope != storagedomain.ScopeGlobal {
			return apperrors.New(42296, http.StatusUnprocessableEntity, fmt.Sprintf("%s 是应用级存储，只有平台级存储能加入存储池", cfg.ConfigName))
		}
		switch member.Mode {
		case "":
			member.Mode = storagedomain.MemberModeActive
		case storagedomain.MemberModeActive, storagedomain.MemberModeStandby, storagedomain.MemberModeReadOnly:
		default:
			return apperrors.New(42297, http.StatusUnprocessableEntity, "成员角色只能是 active、standby 或 readonly")
		}
		if member.Weight == 0 {
			member.Weight = 1
		}
		if member.Weight < 1 || member.Weight > storagedomain.PoolMemberMaxWeight {
			return apperrors.New(42298, http.StatusUnprocessableEntity, fmt.Sprintf("权重需在 1 到 %d 之间", storagedomain.PoolMemberMaxWeight))
		}
		if member.Priority < 0 || member.Priority > storagedomain.PoolMemberMaxPriority {
			return apperrors.New(42299, http.StatusUnprocessableEntity, "优先级需在 0 到 10000 之间")
		}
		if member.CapacityBytes < 0 {
			return apperrors.New(42273, http.StatusUnprocessableEntity, "容量上限不能为负数")
		}
		if member.Mode != storagedomain.MemberModeReadOnly {
			writable++
		}
	}
	if input.IsDefault && input.Enabled && writable == 0 {
		return apperrors.New(42274, http.StatusUnprocessableEntity, "默认池至少需要一个可写成员，否则平台的写入将无处可去")
	}
	return nil
}

// DeletePool 删除池。默认池被删后写入回到平台默认存储配置。
func (s *StorageService) DeletePool(ctx context.Context, id int64) error {
	pool, err := s.pg.GetStoragePool(ctx, id)
	if err != nil {
		return mapStoragePoolError(err)
	}
	if err := s.pg.DeleteStoragePool(ctx, id); err != nil {
		return mapStoragePoolError(err)
	}
	s.emitPoolEvent(ctx, 0, 0, storagedomain.EventLevelInfo, storagedomain.EventConfig,
		fmt.Sprintf("存储池 %s 已删除", pool.Name), nil, fmt.Sprintf("%d", time.Now().UnixNano()))
	return nil
}

// SimulatePool 模拟一次写入：给出成员顺序与每个成员会不会被尝试。
func (s *StorageService) SimulatePool(ctx context.Context, id int64, size int64) (*storagedomain.PoolPlan, error) {
	pool, err := s.pg.GetStoragePool(ctx, id)
	if err != nil {
		return nil, mapStoragePoolError(err)
	}
	if size < 0 {
		size = 0
	}
	configs, err := s.poolConfigs(ctx, pool)
	if err != nil {
		return nil, err
	}
	s.ensureUsage(ctx)
	plan := &storagedomain.PoolPlan{PoolID: pool.ID, Size: size, Strategy: pool.Strategy, Steps: []storagedomain.PoolPlanStep{}}
	attempts := 0
	for _, candidate := range s.pools.Plan(pool, configs, size) {
		step := candidate.step
		if step.Eligible {
			if attempts >= pool.MaxAttempts {
				step.Eligible = false
				step.Reason = fmt.Sprintf("超出单次写入的最多尝试次数（%d）", pool.MaxAttempts)
			} else {
				if attempts == 0 {
					plan.Target = step.ConfigID
				}
				attempts++
			}
		}
		plan.Steps = append(plan.Steps, step)
	}
	return plan, nil
}

// ResetMemberHealth 手动清除一个存储配置的退避状态。
func (s *StorageService) ResetMemberHealth(ctx context.Context, configID int64) error {
	cfg, err := s.pg.GetStorageConfigByID(ctx, configID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return apperrors.New(40480, http.StatusNotFound, "存储配置不存在")
	}
	s.pools.Reset(configID)
	s.emitPoolEvent(ctx, 0, configID, storagedomain.EventLevelInfo, storagedomain.EventRecovered,
		fmt.Sprintf("%s 的退避状态已手动清除", cfg.ConfigName), nil, fmt.Sprintf("%d", time.Now().UnixNano()))
	return nil
}

// ListEvents 运行事件。
func (s *StorageService) ListEvents(ctx context.Context, query storagedomain.EventQuery) ([]storagedomain.PoolEvent, error) {
	return s.pg.ListStoragePoolEvents(ctx, query)
}

func mapStoragePoolError(err error) error {
	switch {
	case errors.Is(err, pgrepo.ErrStoragePoolNotFound):
		return apperrors.New(40487, http.StatusNotFound, "存储池不存在")
	case errors.Is(err, pgrepo.ErrStoragePoolNameTaken):
		return apperrors.New(40987, http.StatusConflict, "已有同名的存储池")
	}
	return err
}

func poolStrategyLabel(strategy string) string {
	switch strategy {
	case storagedomain.PoolStrategyRoundRobin:
		return "轮询"
	case storagedomain.PoolStrategyWeighted:
		return "按权重"
	case storagedomain.PoolStrategyLeastUsed:
		return "最少占用"
	default:
		return "按优先级"
	}
}

// formatStorageBytes 人读的大小（事件文案用）。
func formatStorageBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	i := -1
	for value >= unit && i < len(units)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func formatStorageDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%d 小时 %d 分", int(d.Hours()), int(d.Minutes())%60)
	}
}
