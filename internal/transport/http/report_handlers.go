package httptransport

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// parseReportParams 解析报表的公共参数
func parseReportParams(c *gin.Context, q ReportQueryParams) (service.ReportParams, bool) {
	loc := time.UTC
	if tz := strings.TrimSpace(q.TZ); tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "无效的时区: "+tz)
			return service.ReportParams{}, false
		}
		loc = l
	}
	parse := func(raw string) (time.Time, error) {
		raw = strings.TrimSpace(raw)
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t, nil
		}
		return time.ParseInLocation("2006-01-02", raw, loc)
	}
	start, err := parse(q.Start)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "start 格式错误，需要 RFC3339 或 YYYY-MM-DD")
		return service.ReportParams{}, false
	}
	end, err := parse(q.End)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "end 格式错误，需要 RFC3339 或 YYYY-MM-DD")
		return service.ReportParams{}, false
	}
	return service.ReportParams{From: start, To: end, TimeZone: q.TZ, Granularity: q.Granularity, Unit: q.Unit, Mode: q.Mode}, true
}

// serveReport 报表接口的公共外壳：解析应用与参数，调用对应的报表
func serveReport[T any](h *Handler, c *gin.Context, run func(ctx context.Context, appID int64, p service.ReportParams) (T, error)) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var q ReportQueryParams
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少必要的时间参数 start/end")
		return
	}
	p, ok := parseReportParams(c, q)
	if !ok {
		return
	}
	result, err := run(c.Request.Context(), appID, p)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// ReportOverview 总览
func (h *Handler) ReportOverview(c *gin.Context) { serveReport(h, c, h.report.Overview) }

// ReportRegistration 用户增长（注册）
func (h *Handler) ReportRegistration(c *gin.Context) { serveReport(h, c, h.report.Users) }

// ReportLogin 登录
func (h *Handler) ReportLogin(c *gin.Context) { serveReport(h, c, h.report.Login) }

// ReportRetention 留存
func (h *Handler) ReportRetention(c *gin.Context) { serveReport(h, c, h.report.Retention) }

// ReportActive 活跃
func (h *Handler) ReportActive(c *gin.Context) { serveReport(h, c, h.report.Active) }

// ReportDevice 设备
func (h *Handler) ReportDevice(c *gin.Context) { serveReport(h, c, h.report.Device) }

// ReportRegion 地区
func (h *Handler) ReportRegion(c *gin.Context) { serveReport(h, c, h.report.Region) }

// ReportChannel 渠道
func (h *Handler) ReportChannel(c *gin.Context) { serveReport(h, c, h.report.Channel) }

// ReportPayment 支付
func (h *Handler) ReportPayment(c *gin.Context) { serveReport(h, c, h.report.Payment) }

// ReportNotification 通知
func (h *Handler) ReportNotification(c *gin.Context) { serveReport(h, c, h.report.Notification) }

// ReportRisk 风控（防火墙部分为全局数据）
func (h *Handler) ReportRisk(c *gin.Context) { serveReport(h, c, h.report.Risk) }

// ReportActivity 抽奖活动
func (h *Handler) ReportActivity(c *gin.Context) { serveReport(h, c, h.report.Activity) }

// ReportEngagement 互动
func (h *Handler) ReportEngagement(c *gin.Context) { serveReport(h, c, h.report.Engagement) }

// ReportFunnel 转化漏斗
func (h *Handler) ReportFunnel(c *gin.Context) { serveReport(h, c, h.report.Funnel) }

// ReportExport 报表导出（CSV，UTF-8 带 BOM，Excel 直接打开不乱码）。多张表之间空一行，每张表前一行是表名
func (h *Handler) ReportExport(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var q ExportQueryParams
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少必要参数 type/start/end")
		return
	}
	p, ok := parseReportParams(c, q.ReportQueryParams)
	if !ok {
		return
	}
	tables, err := h.report.ExportTables(c.Request.Context(), appID, q.Type, p)
	if err != nil {
		h.writeError(c, err)
		return
	}
	filename := fmt.Sprintf("report_%s_%s_%s.csv", q.Type, p.From.Format("20060102"), p.To.Format("20060102"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(c.Writer)
	for i, t := range tables {
		if i > 0 {
			_ = writer.Write([]string{})
		}
		_ = writer.Write([]string{"# " + t.Name})
		_ = writer.Write(t.Columns)
		for _, row := range t.Rows {
			_ = writer.Write(row)
		}
	}
	writer.Flush()
}
