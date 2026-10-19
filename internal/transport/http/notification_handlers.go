package httptransport

// 站内信（用户端消息中心）。规则见 docs/notification-center.md。
//
// 网关（/api/v1/apps/:appkey/notifications*）与 /api/notifications* 两处挂的是同一组 handler。
// 写操作统一带回 unread 与 categoryUnread；不存在、不属于本人或已过期的消息一律 40455。

import (
	"net/http"

	notificationdomain "aegis/internal/domain/notification"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Notifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query NotificationQuery
	_ = bind(c, &query)
	items, err := h.notifications.List(c.Request.Context(), session, notificationdomain.UserListQuery{
		Status:   query.Status,
		Category: query.Category,
		Type:     query.Type,
		Level:    query.Level,
		Page:     normalizePage(query.Page),
		Limit:    normalizeLimit(query.Limit),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// NotificationSummary 消息中心首页：总未读与每个分类的未读、总数、最新一条。
func (h *Handler) NotificationSummary(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	result, err := h.notifications.Summary(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// NotificationDetail 单条消息。
func (h *Handler) NotificationDetail(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	notificationID, err := pathInt64(c, "notificationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的通知标识")
		return
	}
	item, err := h.notifications.Get(c.Request.Context(), session, notificationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) NotificationUnreadCount(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	result, err := h.notifications.UnreadCount(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ReadNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationReadRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.MarkRead(c.Request.Context(), session, req.NotificationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已标记已读", result)
}

// UnreadNotification 标为未读。
func (h *Handler) UnreadNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationReadRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.MarkUnread(c.Request.Context(), session, req.NotificationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已标记未读", result)
}

func (h *Handler) ReadNotificationsBatch(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationReadBatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.MarkReadBatch(c.Request.Context(), session, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已批量标记已读", result)
}

func (h *Handler) ReadAllNotifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	// 请求体可空（旧客户端不带）
	var req NotificationReadAllRequest
	_ = bind(c, &req)
	result, err := h.notifications.MarkAllRead(c.Request.Context(), session, req.Category)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已全部标记已读", result)
}

func (h *Handler) DeleteNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	notificationID, err := pathInt64(c, "notificationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的通知标识")
		return
	}
	result, err := h.notifications.Delete(c.Request.Context(), session, notificationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", result)
}

// DeleteNotificationsBatch 批量删除。
func (h *Handler) DeleteNotificationsBatch(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationDeleteBatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.DeleteBatch(c.Request.Context(), session, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", result)
}

func (h *Handler) ClearNotifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationClearRequest
	_ = bind(c, &req)
	result, err := h.notifications.ClearFiltered(c.Request.Context(), session, req.Status, req.Category, req.Type, req.Level)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "清空成功", result)
}

// NotificationPreferences 各分类的提醒开关。
func (h *Handler) NotificationPreferences(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	result, err := h.notifications.Preferences(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// UpdateNotificationPreferences 改提醒开关；账号安全类不能关闭（40347）。
func (h *Handler) UpdateNotificationPreferences(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationPreferencesRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.UpdatePreferences(c.Request.Context(), session, req.Categories)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", result)
}
