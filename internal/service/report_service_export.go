package service

import (
	"context"
	"sort"
	"strconv"

	reportdomain "aegis/internal/domain/report"
	apperrors "aegis/pkg/errors"
)

// ReportKinds 可导出的报表，顺序即「全部导出」时各段的顺序
var ReportKinds = []string{
	"overview", "registration", "active", "retention", "login", "device", "region", "channel",
	"payment", "notification", "risk", "activity", "engagement", "funnel",
}

// ExportTables 把一个报表（kind = all 时为全部）展开成二维表，供 CSV 导出
func (s *ReportService) ExportTables(ctx context.Context, appID int64, kind string, p ReportParams) ([]reportdomain.Table, error) {
	kinds := []string{kind}
	if kind == "all" {
		kinds = ReportKinds
	}
	out := make([]reportdomain.Table, 0, 8)
	for _, k := range kinds {
		tables, err := s.exportOne(ctx, appID, k, p)
		if err != nil {
			return nil, err
		}
		out = append(out, tables...)
	}
	return out, nil
}

func (s *ReportService) exportOne(ctx context.Context, appID int64, kind string, p ReportParams) ([]reportdomain.Table, error) {
	switch kind {
	case "overview":
		r, err := s.Overview(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{kpiTable("总览指标", r.KPIs), trendTable("总览趋势", r.Trend)}, nil
	case "registration":
		r, err := s.Users(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			kpiTable("注册指标", r.KPIs), trendTable("注册趋势", r.Trend),
			sliceTable("注册来源", r.Sources), sliceTable("注册省份", r.Provinces),
		}, nil
	case "active":
		r, err := s.Active(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			kpiTable("活跃指标", r.KPIs), trendTable("活跃趋势", r.Trend),
			sliceTable("活跃天数分布", r.Frequency),
			{Name: "用户分层", Columns: []string{"分层", "人数"}, Rows: [][]string{
				{"新增", itoa64(r.Lifecycle.New)}, {"留存", itoa64(r.Lifecycle.Retained)},
				{"回流", itoa64(r.Lifecycle.Resurrected)}, {"流失", itoa64(r.Lifecycle.Churned)},
			}},
		}, nil
	case "retention":
		r, err := s.Retention(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		cols := []string{"队列", "人数"}
		for _, off := range r.Offsets {
			cols = append(cols, "第"+strconv.Itoa(off)+map[string]string{"day": "天", "week": "周"}[r.Unit]+"(%)")
		}
		rows := make([][]string, 0, len(r.Rows))
		for _, row := range r.Rows {
			line := []string{row.Cohort, itoa64(row.Size)}
			for _, rate := range row.Rates {
				line = append(line, pct(rate))
			}
			rows = append(rows, line)
		}
		avg := []string{"加权平均", ""}
		for _, rate := range r.Average {
			avg = append(avg, pct(rate))
		}
		rows = append(rows, avg)
		return []reportdomain.Table{{Name: "留存矩阵", Columns: cols, Rows: rows}}, nil
	case "login":
		r, err := s.Login(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			kpiTable("登录指标", r.KPIs), trendTable("登录趋势", r.Trend),
			sliceTable("登录方式", r.Types), sliceTable("登录渠道", r.Providers),
			sliceTable("失败原因", r.Failures), sliceTable("失败来源 IP", r.FailureIPs),
		}, nil
	case "device":
		r, err := s.Device(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			kpiTable("设备指标", r.KPIs), sliceTable("平台", r.Platforms), sliceTable("厂商", r.Manufacturers),
			sliceTable("机型", r.Models), sliceTable("系统版本", r.OSVersions), sliceTable("应用版本", r.AppVersions),
			sliceTable("浏览器", r.Browsers),
		}, nil
	case "region":
		r, err := s.Region(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			sliceTable("国家和地区", r.Countries), sliceTable("省份", r.Provinces), sliceTable("城市", r.Cities),
			sliceTable("运营商", r.ISPs), sliceTable("注册省份", r.RegisterProvinces),
		}, nil
	case "channel":
		r, err := s.Channel(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			kpiTable("渠道指标", r.KPIs), trendTable("注册来源趋势", r.Trend), sliceTable("注册来源", r.RegisterSource),
			sliceTable("登录渠道", r.LoginProviders), sliceTable("邀请来源", r.InviteSources), sliceTable("卡密来源", r.CardKeySources),
		}, nil
	case "payment":
		r, err := s.Payment(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		hist := reportdomain.Table{Name: "订单金额分布", Columns: []string{"区间(元)", "订单数"}}
		for _, b := range r.Histogram {
			hist.Rows = append(hist.Rows, []string{b.Label, itoa64(b.Count)})
		}
		return []reportdomain.Table{
			kpiTable("支付指标", r.KPIs), trendTable("支付趋势", r.Trend), sliceTable("支付方式", r.Methods),
			sliceTable("支付通道", r.Providers), sliceTable("商品", r.Products), sliceTable("订单状态", r.Statuses),
			sliceTable("会员套餐", r.VipPlans), summaryTable("订单金额统计(元)", r.Amounts), hist,
		}, nil
	case "notification":
		r, err := s.Notification(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			kpiTable("通知指标", r.KPIs), trendTable("通知趋势", r.Trend), sliceTable("通知类型", r.Types),
			sliceTable("通知级别", r.Levels), summaryTable("阅读耗时(分钟)", r.ReadLatency),
		}, nil
	case "risk":
		r, err := s.Risk(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			kpiTable("风控指标", r.KPIs), trendTable("风险等级趋势", r.Trend), sliceTable("风险等级", r.Levels),
			sliceTable("处置动作", r.Actions), sliceTable("场景", r.Scenes), sliceTable("命中规则", r.Rules),
			sliceTable("高风险 IP", r.IPs), trendTable("防火墙拦截趋势（全局）", r.Firewall.Trend),
			sliceTable("防火墙规则（全局）", r.Firewall.Rules), sliceTable("防火墙来源（全局）", r.Firewall.Countries),
		}, nil
	case "activity":
		r, err := s.Activity(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		acts := reportdomain.Table{Name: "活动明细", Columns: []string{"ID", "活动", "状态", "抽奖次数", "参与人数", "中奖次数", "中奖率(%)", "消耗积分", "保底次数"}}
		for _, a := range r.Activities {
			acts.Rows = append(acts.Rows, []string{
				itoa64(a.ID), a.Name, a.Status, itoa64(a.Draws), itoa64(a.Participants), itoa64(a.Wins),
				pct(&a.WinRate), itoa64(a.PointsCost), itoa64(a.Pity),
			})
		}
		return []reportdomain.Table{
			kpiTable("活动指标", r.KPIs), trendTable("抽奖趋势", r.Trend), acts,
			sliceTable("奖品", r.Prizes), sliceTable("奖品类型", r.Kinds), sliceTable("次数来源", r.Sources),
		}, nil
	case "engagement":
		r, err := s.Engagement(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		return []reportdomain.Table{
			kpiTable("互动指标", r.KPIs), trendTable("互动趋势", r.Trend), sliceTable("签到来源", r.SignSources),
			sliceTable("连签分布", r.Streaks), sliceTable("积分获取", r.PointsEarned), sliceTable("积分消耗", r.PointsSpent),
			sliceTable("激励广告", r.RewardedAds), sliceTable("激励广告场景", r.RewardedScenes), sliceTable("开屏广告", r.SplashAds),
		}, nil
	case "funnel":
		r, err := s.Funnel(ctx, appID, p)
		if err != nil {
			return nil, err
		}
		steps := reportdomain.Table{Name: "转化漏斗", Columns: []string{"步骤", "人数", "步骤转化(%)", "整体转化(%)"}}
		for _, st := range r.Steps {
			steps.Rows = append(steps.Rows, []string{st.Key, itoa64(st.Users), pct(&st.StepRate), pct(&st.OverallRate)})
		}
		return []reportdomain.Table{
			kpiTable("漏斗指标", r.KPIs), steps, trendTable("分批转化", r.Trend),
			summaryTable("注册到首次付费(小时)", r.HoursToPay), summaryTable("注册到首次回访(天)", r.DaysToReturn),
		}, nil
	}
	return nil, apperrors.New(40000, 400, "不支持的报表类型: "+kind)
}

func kpiTable(name string, kpis map[string]reportdomain.KPI) reportdomain.Table {
	keys := make([]string, 0, len(kpis))
	for k := range kpis {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t := reportdomain.Table{Name: name, Columns: []string{"指标", "本期", "上期", "变化(%)"}}
	for _, k := range keys {
		v := kpis[k]
		t.Rows = append(t.Rows, []string{k, num(v.Value), optNum(v.Previous), pct(v.Change)})
	}
	return t
}

func trendTable(name string, trend reportdomain.Trend) reportdomain.Table {
	keys := make([]string, 0, len(trend.Series))
	for k := range trend.Series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t := reportdomain.Table{Name: name, Columns: append([]string{"时间"}, keys...)}
	for i, b := range trend.Buckets {
		row := []string{b}
		for _, k := range keys {
			v := 0.0
			if i < len(trend.Series[k]) {
				v = trend.Series[k][i]
			}
			row = append(row, num(v))
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

func sliceTable(name string, items []reportdomain.Slice) reportdomain.Table {
	t := reportdomain.Table{Name: name, Columns: []string{"键", "名称", "次数", "人数", "金额(元)", "附加值", "占比(%)"}}
	for _, it := range items {
		share := it.Share
		t.Rows = append(t.Rows, []string{it.Key, it.Label, itoa64(it.Count), itoa64(it.Users), num(it.Amount), num(it.Value), pct(&share)})
	}
	return t
}

func summaryTable(name string, s reportdomain.Summary) reportdomain.Table {
	return reportdomain.Table{Name: name, Columns: []string{"样本数", "合计", "均值", "标准差", "最小", "P25", "中位数", "P75", "P90", "P99", "最大"}, Rows: [][]string{{
		itoa64(s.Count), num(s.Sum), num(s.Mean), num(s.Std), num(s.Min), num(s.P25), num(s.P50), num(s.P75), num(s.P90), num(s.P99), num(s.Max),
	}}}
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func optNum(v *float64) string {
	if v == nil {
		return ""
	}
	return num(*v)
}

func pct(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v*100, 'f', 2, 64)
}
