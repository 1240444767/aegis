package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// StorageResourceService 存储资源中心业务逻辑
type StorageResourceService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
	// objects 物理删除对象（StorageService.DeleteObject）。没注入时永久删除只删索引。
	objects storageObjectRemover
}

// storageObjectRemover 从存储里删掉对象并移除索引。
type storageObjectRemover interface {
	DeleteObject(ctx context.Context, configID int64, objectKey string) error
}

// SetObjectRemover 注入物理删除能力（bootstrap 中调用）。
func (s *StorageResourceService) SetObjectRemover(remover storageObjectRemover) { s.objects = remover }

// NewStorageResourceService 创建存储资源服务
func NewStorageResourceService(log *zap.Logger, pg *pgrepo.Repository) *StorageResourceService {
	return &StorageResourceService{log: log, pg: pg}
}

// ════════════════════════════════════════════════════════════
//  文件管理
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) IndexStorageObject(ctx context.Context, obj storagedomain.StorageObject) (*storagedomain.StorageObject, error) {
	return s.pg.IndexStorageObject(ctx, obj)
}

func (s *StorageResourceService) ListStorageObjects(ctx context.Context, query storagedomain.ObjectListQuery) ([]storagedomain.StorageObject, int64, error) {
	return s.pg.ListStorageObjects(ctx, normalizeObjectListQuery(query))
}

// BrowseStorageObjects 目录浏览：一次返回文件、子目录与筛选汇总。
// 三者共用同一组筛选条件，分三个接口取会出现「文件已翻到第 3 页、
// 目录树还停在上一次筛选」这种自相矛盾的画面。
func (s *StorageResourceService) BrowseStorageObjects(ctx context.Context, query storagedomain.ObjectListQuery) (*storagedomain.ObjectListResult, error) {
	query = normalizeObjectListQuery(query)

	items, total, err := s.pg.ListStorageObjects(ctx, query)
	if err != nil {
		return nil, err
	}
	summary, err := s.pg.SummarizeStorageObjects(ctx, query)
	if err != nil {
		return nil, err
	}
	result := &storagedomain.ObjectListResult{
		Items:   items,
		Folders: []storagedomain.ObjectFolder{},
		Total:   total,
		Page:    query.Page,
		Limit:   query.Limit,
		Summary: *summary,
	}
	// 只有目录浏览模式才需要子目录；平铺检索（关键字 / 全局筛选）下
	// 目录聚合既没有意义，还要多扫一次全表。
	if query.FolderView {
		folders, err := s.pg.ListObjectFolders(ctx, query)
		if err != nil {
			return nil, err
		}
		if folders != nil {
			result.Folders = folders
		}
	}
	return result, nil
}

// BatchMutateObjects 批量软删 / 恢复 / 永久删除。
// 逐条调用单条接口是重构前控制台的做法：批量删 20 个文件打 20 个请求，
// 中途失败留下一半状态，且 toast 报的是"已删除 20 个"。
func (s *StorageResourceService) BatchMutateObjects(ctx context.Context, action string, ids []int64) (*storagedomain.BatchObjectResult, error) {
	ids = dedupeInt64(ids)
	result := &storagedomain.BatchObjectResult{Action: action, Requested: len(ids)}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > batchObjectLimit {
		return nil, apperrors.New(40087, http.StatusBadRequest, fmt.Sprintf("单次批量操作最多 %d 个对象", batchObjectLimit))
	}

	var affected int64
	var err error
	switch action {
	case storagedomain.BatchActionDelete:
		affected, err = s.pg.BatchSetObjectStatus(ctx, ids, "deleted")
	case storagedomain.BatchActionRestore:
		affected, err = s.pg.BatchSetObjectStatus(ctx, ids, "active")
	case storagedomain.BatchActionPurge:
		affected, err = s.purgeObjects(ctx, ids)
	default:
		return nil, apperrors.New(40088, http.StatusBadRequest, "不支持的批量操作")
	}
	if err != nil {
		return nil, err
	}
	result.Affected = affected
	result.Skipped = int64(len(ids)) - affected
	if result.Skipped < 0 {
		result.Skipped = 0
	}
	return result, nil
}

const batchObjectLimit = 500

// normalizeObjectListQuery 归一化查询参数。
// 分页与排序的兜底放在服务层而不是 handler：管理端、后台任务、
// 未来的批量导出都会调到这里，兜底放在入口处才只写一遍。
func normalizeObjectListQuery(query storagedomain.ObjectListQuery) storagedomain.ObjectListQuery {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > 200 {
		query.Limit = 20
	}
	switch query.Sort {
	case storagedomain.ObjectSortSize, storagedomain.ObjectSortFileName,
		storagedomain.ObjectSortObjectKey, storagedomain.ObjectSortDeletedAt:
		// 白名单内，原样保留
	default:
		query.Sort = storagedomain.ObjectSortCreatedAt
	}
	if !strings.EqualFold(query.Order, "asc") {
		query.Order = "desc"
	} else {
		query.Order = "asc"
	}
	query.Folder = strings.Trim(strings.TrimSpace(strings.ReplaceAll(query.Folder, "\\", "/")), "/")
	query.Keyword = strings.TrimSpace(query.Keyword)
	query.Prefix = strings.TrimSpace(query.Prefix)
	query.ContentType = strings.TrimSpace(query.ContentType)
	return query
}

func (s *StorageResourceService) GetStorageObject(ctx context.Context, id int64) (*storagedomain.StorageObject, error) {
	return s.pg.GetStorageObject(ctx, id)
}

func (s *StorageResourceService) SoftDeleteStorageObject(ctx context.Context, id int64) error {
	return s.pg.SoftDeleteStorageObject(ctx, id)
}

func (s *StorageResourceService) RestoreStorageObject(ctx context.Context, id int64) error {
	return s.pg.RestoreStorageObject(ctx, id)
}

// PermanentDeleteStorageObject 永久删除：对象从存储里删掉，索引一并移除。
func (s *StorageResourceService) PermanentDeleteStorageObject(ctx context.Context, id int64) error {
	affected, err := s.purgeObjects(ctx, []int64{id})
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.New(40482, http.StatusNotFound, "存储对象不存在")
	}
	return nil
}

// purgeObjects 逐个物理删除。删除失败的对象索引保留（还能在回收站里看到、再试一次），
// 而不是只删索引让对象在桶里变成没人知道的孤儿。
func (s *StorageResourceService) purgeObjects(ctx context.Context, ids []int64) (int64, error) {
	if s.objects == nil {
		return s.pg.BatchPermanentDeleteObjects(ctx, ids)
	}
	objects, err := s.pg.ListStorageObjectsByIDs(ctx, ids)
	if err != nil {
		return 0, err
	}
	var affected int64
	var firstErr error
	for _, obj := range objects {
		if err := s.objects.DeleteObject(ctx, obj.ConfigID, obj.ObjectKey); err != nil {
			s.log.Warn("永久删除对象失败", zap.Int64("object_id", obj.ID), zap.String("key", obj.ObjectKey), zap.Error(err))
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// DeleteObject 按配置 + 键清索引；同键的重复行由这里按 id 收尾。
		_, _ = s.pg.BatchPermanentDeleteObjects(ctx, []int64{obj.ID})
		affected++
	}
	if affected == 0 && firstErr != nil {
		return 0, apperrors.New(50386, http.StatusServiceUnavailable, "删除存储中的对象失败，请稍后重试")
	}
	return affected, nil
}

func (s *StorageResourceService) ListDeletedObjects(ctx context.Context, configID *int64, page, limit int) ([]storagedomain.StorageObject, int64, error) {
	return s.pg.ListDeletedObjects(ctx, configID, page, limit)
}

// CleanupDeletedObjects 清空回收站里放了超过 olderThan 的对象（物理删除）。
func (s *StorageResourceService) CleanupDeletedObjects(ctx context.Context, olderThan time.Duration) (int64, error) {
	if s.objects == nil {
		return s.pg.CleanupDeletedObjects(ctx, olderThan)
	}
	cutoff := time.Now().Add(-olderThan)
	var total int64
	for {
		batch, err := s.pg.DeletedStorageObjectsBefore(ctx, cutoff, 200)
		if err != nil {
			return total, err
		}
		if len(batch) == 0 {
			return total, nil
		}
		ids := make([]int64, len(batch))
		for i, obj := range batch {
			ids[i] = obj.ID
		}
		affected, err := s.purgeObjects(ctx, ids)
		total += affected
		if err != nil || affected == 0 {
			// 一整批都删不掉（存储不可用）：停下，免得同一批反复重试成死循环。
			return total, err
		}
	}
}

// ════════════════════════════════════════════════════════════
//  规则管理
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) CreateStorageRule(ctx context.Context, input storagedomain.CreateRuleInput) (*storagedomain.StorageRule, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 128 {
		return nil, apperrors.New(42277, http.StatusUnprocessableEntity, "规则名称不能为空，且不超过 128 字")
	}
	data, err := NormalizeStorageRule(input.RuleType, input.RuleData)
	if err != nil {
		return nil, err
	}
	input.RuleData = data
	return s.pg.CreateStorageRule(ctx, input)
}

func (s *StorageResourceService) ListStorageRules(ctx context.Context, configID *int64, appID *int64) ([]storagedomain.StorageRule, error) {
	return s.pg.ListStorageRules(ctx, configID, appID)
}

func (s *StorageResourceService) UpdateStorageRule(ctx context.Context, id int64, name string, ruleData map[string]any, isActive bool) error {
	current, err := s.pg.ListStorageRules(ctx, nil, nil)
	if err != nil {
		return err
	}
	ruleType := ""
	for _, rule := range current {
		if rule.ID == id {
			ruleType = rule.RuleType
		}
	}
	if ruleType == "" {
		return apperrors.New(40489, http.StatusNotFound, "规则不存在")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 128 {
		return apperrors.New(42277, http.StatusUnprocessableEntity, "规则名称不能为空，且不超过 128 字")
	}
	data, err := NormalizeStorageRule(ruleType, ruleData)
	if err != nil {
		return err
	}
	return s.pg.UpdateStorageRule(ctx, id, name, data, isActive)
}

func (s *StorageResourceService) DeleteStorageRule(ctx context.Context, id int64) error {
	return s.pg.DeleteStorageRule(ctx, id)
}

func (s *StorageResourceService) GetActiveUploadRules(ctx context.Context, configID int64, appID *int64) ([]storagedomain.StorageRule, error) {
	return s.pg.GetActiveUploadRules(ctx, configID, appID)
}

// ════════════════════════════════════════════════════════════
//  CDN 配置
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) UpsertCDNConfig(ctx context.Context, configID int64, input storagedomain.UpsertCDNConfigInput) (*storagedomain.CDNConfig, error) {
	// 签名密钥留空表示不修改：编辑表单拿不到原值。
	if strings.TrimSpace(input.SignURLSecret) == "" {
		if existing, err := s.pg.GetCDNConfig(ctx, configID); err == nil && existing != nil {
			input.SignURLSecret = existing.SignURLSecret
		}
	}
	if err := normalizeCDNInput(&input); err != nil {
		return nil, err
	}
	saved, err := s.pg.UpsertCDNConfig(ctx, configID, input)
	if err == nil {
		InvalidateCDNConfig(configID)
	}
	return saved, err
}

// normalizeCDNInput 校验 CDN 设置：名单里的 IP / CIDR 必须能解析，免得一条笔误把所有人挡在外面。
func normalizeCDNInput(input *storagedomain.UpsertCDNConfigInput) error {
	input.CDNDomain = strings.TrimRight(strings.TrimSpace(input.CDNDomain), "/")
	input.CDNProtocol = strings.ToLower(strings.TrimSpace(input.CDNProtocol))
	if input.CDNProtocol != "http" {
		input.CDNProtocol = "https"
	}
	if input.CacheMaxAge < 0 {
		input.CacheMaxAge = 0
	}
	if input.SignURLEnabled && strings.TrimSpace(input.SignURLSecret) == "" {
		return apperrors.New(42277, http.StatusUnprocessableEntity, "开启签名 URL 时必须填写签名密钥")
	}
	for _, entry := range input.IPWhitelist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				return apperrors.New(42277, http.StatusUnprocessableEntity, "IP 白名单里的网段无效："+entry)
			}
		} else if net.ParseIP(entry) == nil {
			return apperrors.New(42277, http.StatusUnprocessableEntity, "IP 白名单里的地址无效："+entry)
		}
	}
	return nil
}

func (s *StorageResourceService) GetCDNConfig(ctx context.Context, configID int64) (*storagedomain.CDNConfig, error) {
	return s.pg.GetCDNConfig(ctx, configID)
}

func (s *StorageResourceService) DeleteCDNConfig(ctx context.Context, configID int64) error {
	err := s.pg.DeleteCDNConfig(ctx, configID)
	if err == nil {
		InvalidateCDNConfig(configID)
	}
	return err
}

// ════════════════════════════════════════════════════════════
//  图片规则
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) CreateImageRule(ctx context.Context, input storagedomain.CreateImageRuleInput) (*storagedomain.ImageRule, error) {
	return s.pg.CreateImageRule(ctx, input)
}

func (s *StorageResourceService) ListImageRules(ctx context.Context, configID *int64) ([]storagedomain.ImageRule, error) {
	return s.pg.ListImageRules(ctx, configID)
}

func (s *StorageResourceService) DeleteImageRule(ctx context.Context, id int64) error {
	return s.pg.DeleteImageRule(ctx, id)
}

// ════════════════════════════════════════════════════════════
//  用量统计
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) CreateUsageSnapshot(ctx context.Context, snapshot storagedomain.UsageSnapshot) error {
	return s.pg.CreateUsageSnapshot(ctx, snapshot)
}

func (s *StorageResourceService) GetLatestUsageSnapshot(ctx context.Context, configID int64) (*storagedomain.UsageSnapshot, error) {
	return s.pg.GetLatestUsageSnapshot(ctx, configID)
}

func (s *StorageResourceService) GetUsageHistory(ctx context.Context, configID int64, days int) ([]storagedomain.UsageSnapshot, error) {
	return s.pg.GetUsageHistory(ctx, configID, days)
}

func (s *StorageResourceService) GetObjectTypeStats(ctx context.Context, configID *int64) ([]storagedomain.TypeStat, error) {
	return s.pg.GetObjectTypeStats(ctx, configID)
}

// GetUsageStats 获取存储用量统计（组合最新快照 + 类型统计）
// GetUsageStats 获取用量统计（configID=0 时返回全局汇总，实时从 storage_objects 聚合）
func (s *StorageResourceService) GetUsageStats(ctx context.Context, configID int64) (*storagedomain.UsageStats, error) {
	stats := &storagedomain.UsageStats{ConfigID: configID}

	// 实时聚合（不依赖快照，始终准确）
	realtime, err := s.pg.GetRealtimeUsageStats(ctx, configID)
	if err != nil {
		return nil, err
	}
	if realtime != nil {
		stats.TotalFiles = realtime.TotalFiles
		stats.TotalSize = realtime.TotalSize
		stats.ActiveFiles = realtime.ActiveFiles
		stats.DeletedFiles = realtime.DeletedFiles
	}

	// 获取类型统计
	var cfgPtr *int64
	if configID > 0 {
		cfgPtr = &configID
	}
	typeStats, err := s.pg.GetObjectTypeStats(ctx, cfgPtr)
	if err != nil {
		return nil, err
	}
	stats.TopTypes = typeStats
	if stats.TopTypes == nil {
		stats.TopTypes = []storagedomain.TypeStat{}
	}
	return stats, nil
}
