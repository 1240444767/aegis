package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	storagedomain "aegis/internal/domain/storage"
	"aegis/pkg/response"
)

// 存储中心（平台级）：总览、存储池、运行事件、深度检测、CORS、迁移。

// AdminStorageOverview 总览：全部平台级存储的用量与健康、全部池、最近的迁移。
func (h *Handler) AdminStorageOverview(c *gin.Context) {
	overview, err := h.storage.Overview(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", overview)
}

// AdminStoragePools 全部存储池。
func (h *Handler) AdminStoragePools(c *gin.Context) {
	pools, err := h.storage.ListPools(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", pools)
}

// AdminStoragePool 一个存储池。
func (h *Handler) AdminStoragePool(c *gin.Context) {
	id, ok := storagePathID(c, "poolId")
	if !ok {
		return
	}
	pool, err := h.storage.GetPool(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", pool)
}

// AdminCreateStoragePool 新建存储池。
func (h *Handler) AdminCreateStoragePool(c *gin.Context) {
	var req storagedomain.PoolInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	pool, err := h.storage.SavePool(c.Request.Context(), 0, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "存储池已创建", pool)
}

// AdminUpdateStoragePool 修改存储池（成员整体替换）。
func (h *Handler) AdminUpdateStoragePool(c *gin.Context) {
	id, ok := storagePathID(c, "poolId")
	if !ok {
		return
	}
	var req storagedomain.PoolInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	pool, err := h.storage.SavePool(c.Request.Context(), id, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "存储池已保存", pool)
}

// AdminDeleteStoragePool 删除存储池。
func (h *Handler) AdminDeleteStoragePool(c *gin.Context) {
	id, ok := storagePathID(c, "poolId")
	if !ok {
		return
	}
	if err := h.storage.DeletePool(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "存储池已删除", nil)
}

// StoragePoolSimulateRequest 模拟写入的文件大小。
type StoragePoolSimulateRequest struct {
	Size int64 `json:"size"`
}

// AdminSimulateStoragePool 模拟一次写入的成员顺序。
func (h *Handler) AdminSimulateStoragePool(c *gin.Context) {
	id, ok := storagePathID(c, "poolId")
	if !ok {
		return
	}
	var req StoragePoolSimulateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	plan, err := h.storage.SimulatePool(c.Request.Context(), id, req.Size)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "模拟完成", plan)
}

// AdminStorageEvents 运行事件。?poolId= &configId= &level= &limit=
func (h *Handler) AdminStorageEvents(c *gin.Context) {
	query := storagedomain.EventQuery{Level: strings.TrimSpace(c.Query("level"))}
	if v, err := strconv.ParseInt(c.Query("poolId"), 10, 64); err == nil && v > 0 {
		query.PoolID = &v
	}
	if v, err := strconv.ParseInt(c.Query("configId"), 10, 64); err == nil && v > 0 {
		query.ConfigID = &v
	}
	if v, err := strconv.Atoi(c.Query("limit")); err == nil {
		query.Limit = v
	}
	events, err := h.storage.ListEvents(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", events)
}

// AdminProbeStorageConfig 深度检测：连通、写入、读回校验、删除。
func (h *Handler) AdminProbeStorageConfig(c *gin.Context) {
	id, ok := storagePathID(c, "configId")
	if !ok {
		return
	}
	result, err := h.storage.ProbeConfig(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "检测完成", result)
}

// AdminResetStorageHealth 手动清除退避。
func (h *Handler) AdminResetStorageHealth(c *gin.Context) {
	id, ok := storagePathID(c, "configId")
	if !ok {
		return
	}
	if err := h.storage.ResetMemberHealth(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已恢复写入", nil)
}

// AdminStorageCORS 读存储桶的 CORS 规则。
func (h *Handler) AdminStorageCORS(c *gin.Context) {
	id, ok := storagePathID(c, "configId")
	if !ok {
		return
	}
	rules, err := h.storage.GetBucketCORS(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", rules)
}

// StorageCORSRequest 允许直传的来源。
type StorageCORSRequest struct {
	Origins []string `json:"origins"`
}

// AdminSetStorageCORS 写入允许浏览器直传的 CORS 规则。
func (h *Handler) AdminSetStorageCORS(c *gin.Context) {
	id, ok := storagePathID(c, "configId")
	if !ok {
		return
	}
	var req StorageCORSRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rules, err := h.storage.SetBucketCORS(c.Request.Context(), id, req.Origins)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "CORS 规则已写入", rules)
}

// AdminClearStorageRedirect 撤销读取转向。
func (h *Handler) AdminClearStorageRedirect(c *gin.Context) {
	id, ok := storagePathID(c, "configId")
	if !ok {
		return
	}
	if err := h.storage.ClearRedirect(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "读取转向已撤销", nil)
}

// AdminStorageMigrations 最近的迁移任务。
func (h *Handler) AdminStorageMigrations(c *gin.Context) {
	items, err := h.storage.ListMigrations(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminStorageMigration 一个迁移任务。
func (h *Handler) AdminStorageMigration(c *gin.Context) {
	id, ok := storagePathID(c, "migrationId")
	if !ok {
		return
	}
	item, err := h.storage.GetMigration(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", item)
}

// AdminCreateStorageMigration 新建并启动迁移。
func (h *Handler) AdminCreateStorageMigration(c *gin.Context) {
	var req storagedomain.MigrationInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.storage.CreateMigration(c.Request.Context(), req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "迁移已开始", item)
}

// AdminPauseStorageMigration / AdminResumeStorageMigration / AdminCancelStorageMigration 控制迁移。
func (h *Handler) AdminPauseStorageMigration(c *gin.Context) {
	h.controlStorageMigration(c, "pause", "已暂停")
}

func (h *Handler) AdminResumeStorageMigration(c *gin.Context) {
	h.controlStorageMigration(c, "resume", "已继续")
}

func (h *Handler) AdminCancelStorageMigration(c *gin.Context) {
	h.controlStorageMigration(c, "cancel", "已取消")
}

func (h *Handler) controlStorageMigration(c *gin.Context, action, message string) {
	id, ok := storagePathID(c, "migrationId")
	if !ok {
		return
	}
	item, err := h.storage.ControlMigration(c.Request.Context(), id, action)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, message, item)
}

// AdminCollectStorageUsage 立即采集一次用量快照。
func (h *Handler) AdminCollectStorageUsage(c *gin.Context) {
	if err := h.storage.CollectUsageSnapshots(c.Request.Context()); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已采集用量快照", nil)
}

func storagePathID(c *gin.Context, name string) (int64, bool) {
	id, err := pathInt64(c, name)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "参数 "+name+" 无效")
		return 0, false
	}
	return id, true
}
