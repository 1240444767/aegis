package service

import (
	"context"
	"math"
	"sort"
	"strconv"
	"time"

	reportdomain "aegis/internal/domain/report"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service/reportengine"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
	"golang.org/x/sync/errgroup"
)

// 用户侧报表：总览、用户增长、活跃、留存、漏斗。全部建立在同一份活跃事实与注册事实上，口径互相对得上。

// Overview 总览
func (s *ReportService) Overview(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Overview, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		act     *activityFacts
		reg     *registrationFacts
		pay     *paymentFacts
		logins  []pgrepo.ReportDailyRow
		signins []pgrepo.ReportDailyRow
		refunds []pgrepo.ReportDailyRow
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { act, err = s.activity(gctx, appID, w); return })
	g.Go(func() (err error) { reg, err = s.registrations(gctx, appID, w); return })
	g.Go(func() (err error) { pay, err = s.payments(gctx, appID, w); return })
	g.Go(func() (err error) {
		logins, err = s.pg.ReportDaily(gctx, "login.daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		signins, err = s.pg.ReportDaily(gctx, "engagement.sign_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		refunds, err = s.pg.ReportDaily(gctx, "payment.refund_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	now := s.now()
	elapsed := w.ElapsedDays(now)
	newUsers := regDaily(w, reg)
	dau := make([]float64, w.Days)
	prevDau := make([]float64, w.Days)
	for i := 0; i < w.Days; i++ {
		dau[i] = reportengine.Cardinality(act.all.Day(i))
		prevDau[i] = reportengine.Cardinality(act.all.Day(i - w.Days))
	}
	activeCur := act.all.Union(0, w.Days-1)
	activePrev := act.all.Union(-w.Days, -1)
	revenue, payersCur, payersPrev := revenueDaily(w, pay)
	refund, _, _ := mergeDaily(w, refunds)
	loginOK, _, _ := mergeDaily(w, logins)
	signs, _, _ := mergeDaily(w, signins)

	revenueTotal, revenuePrev := revenue.total(), revenue.prevTotal()
	netTotal, netPrev := revenueTotal-refund.total(), revenuePrev-refund.prevTotal()
	payers, prevPayers := float64(payersCur.GetCardinality()), float64(payersPrev.GetCardinality())
	activeN, activePrevN := reportengine.Cardinality(activeCur), reportengine.Cardinality(activePrev)

	d1, d1Prev := dayRetention(w, reg.byDay, act, 0, 1), dayRetention(w, reg.prevByDay, act, -w.Days, 1)

	kpis := map[string]reportdomain.KPI{
		"newUsers":    kpi(newUsers.total(), newUsers.prevTotal(), sumBuckets(w, newUsers.cur)),
		"totalUsers":  kpi(float64(reg.before)+newUsers.total(), float64(reg.before), nil),
		"activeUsers": kpi(activeN, activePrevN, uniqueBuckets(w, act.all)),
		"avgDau":      kpi(reportengine.Sum(dau[:elapsed])/float64(elapsed), reportengine.Sum(prevDau)/float64(w.Days), avgBuckets(w, dau, elapsed)),
		"logins":      kpi(loginOK.total(), loginOK.prevTotal(), sumBuckets(w, loginOK.cur)),
		"revenue":     kpi(revenueTotal, revenuePrev, sumBuckets(w, revenue.cur)),
		"netRevenue":  kpi(netTotal, netPrev, nil),
		"payers":      kpi(payers, prevPayers, nil),
		"arppu":       ratioKPI(revenueTotal, payers, revenuePrev, prevPayers),
		"payRate":     ratioKPI(payers, activeN, prevPayers, activePrevN),
		"signins":     kpi(signs.total(), signs.prevTotal(), sumBuckets(w, signs.cur)),
		"d1Retention": reportengine.NewKPI(d1.rate(), d1.prevPtr(d1Prev), nil),
	}

	report := &reportdomain.Overview{
		Window: w.Public(now),
		KPIs:   kpis,
		Trend: reportdomain.Trend{
			Buckets: w.Buckets(),
			Series: map[string][]float64{
				"newUsers": sumBuckets(w, newUsers.cur),
				"active":   uniqueBuckets(w, act.all),
				"dau":      avgBuckets(w, dau, elapsed),
				"revenue":  sumBuckets(w, revenue.cur),
				"logins":   sumBuckets(w, loginOK.cur),
				"signins":  sumBuckets(w, signs.cur),
			},
		},
		Lifecycle: lifecycle(activeCur, activePrev, reg.current),
	}
	insights := make([]reportdomain.Insight, 0)
	insights = append(insights, dailyInsights(w, "newUsers", newUsers.cur[:elapsed])...)
	insights = append(insights, dailyInsights(w, "dau", dau[:elapsed])...)
	insights = append(insights, dailyInsights(w, "revenue", revenue.cur[:elapsed])...)
	insights = append(insights, dailyInsights(w, "logins", loginOK.cur[:elapsed])...)
	report.Insights = topInsights(insights, 8)
	return report, nil
}

// Users 用户增长
func (s *ReportService) Users(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Users, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		act       *activityFacts
		reg       *registrationFacts
		sources   []pgrepo.ReportGroupRow
		provinces []reportdomain.Slice
	)
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) { act, err = s.activity(gctx, appID, w); return })
	g.Go(func() (err error) { reg, err = s.registrations(gctx, appID, w); return })
	g.Go(func() (err error) {
		sources, err = s.pg.ReportGroups(gctx, "users.sources", appID, w.Start, w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		provinces, err = s.groups(gctx, "users.provinces", appID, w, reportengine.SliceOptions{Top: 34})
		return
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	now := s.now()
	elapsed := w.ElapsedDays(now)
	newUsers := regDaily(w, reg)
	cumulative := make([]float64, w.Days)
	running := float64(reg.before)
	for i, v := range newUsers.cur {
		running += v
		cumulative[i] = running
	}

	// 7 日内回访：注册日之后 7 天里有过活跃。观察期没开始的用户（今天注册的）不进分母
	last := act.lastObserved()
	activated := make([]float64, w.Days)
	var activatedN, eligible, prevActivatedN, prevEligible float64
	for d := 0; d < w.Days; d++ {
		if d+1 > last {
			break
		}
		cohort := reg.byDay[d]
		n := reportengine.Cardinality(reportengine.And(cohort, act.all.Union(d+1, min(d+7, last))))
		activated[d] = n
		activatedN += n
		eligible += reportengine.Cardinality(cohort)
	}
	for d := -w.Days; d < 0; d++ {
		cohort := reg.prevByDay[d+w.Days]
		prevActivatedN += reportengine.Cardinality(reportengine.And(cohort, act.all.Union(d+1, min(d+7, last))))
		prevEligible += reportengine.Cardinality(cohort)
	}

	sourceGroups := toGroups(sources)
	var audited int64
	for _, g := range sourceGroups {
		audited += g.Count
	}
	// 没有注册审计的用户：后台创建、批量导入、早于审计上线的老数据
	if rest := int64(newUsers.total()) - audited; rest > 0 {
		sourceGroups = append(sourceGroups, reportengine.Group{Key: "admin", Count: rest, Users: rest})
	}

	weekdays := make([]reportengine.Group, 7)
	for i, n := range reg.weekdays {
		weekdays[i] = reportengine.Group{Key: strconv.Itoa(i + 1), Count: n}
	}

	return &reportdomain.Users{
		Window: w.Public(now),
		KPIs: map[string]reportdomain.KPI{
			"newUsers":       kpi(newUsers.total(), newUsers.prevTotal(), sumBuckets(w, newUsers.cur)),
			"totalUsers":     kpi(float64(reg.before)+newUsers.total(), float64(reg.before), lastBuckets(w, cumulative, elapsed)),
			"avgDaily":       kpi(newUsers.total()/float64(elapsed), newUsers.prevTotal()/float64(w.Days), avgBuckets(w, newUsers.cur, elapsed)),
			"peakDaily":      reportengine.NewKPI(maxOf(newUsers.cur), reportengine.Ptr(maxOf(newUsers.prev)), nil),
			"activationRate": ratioKPI(activatedN, eligible, prevActivatedN, prevEligible),
		},
		Trend: reportdomain.Trend{
			Buckets: w.Buckets(),
			Series: map[string][]float64{
				"newUsers":   sumBuckets(w, newUsers.cur),
				"cumulative": lastBuckets(w, cumulative, elapsed),
				"activated":  sumBuckets(w, activated),
				"previous":   sumBucketsShifted(w, newUsers.prev),
			},
		},
		Sources:   reportengine.ToSlices(sourceGroups, reportengine.SliceOptions{}),
		Provinces: provinces,
		Weekdays:  reportengine.ToSlices(weekdays, reportengine.SliceOptions{KeepOrder: true}),
		Heatmap:   reg.heatmap,
		Insights:  topInsights(dailyInsights(w, "newUsers", newUsers.cur[:elapsed]), 6),
	}, nil
}

// Active 活跃
func (s *ReportService) Active(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Active, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		act  *activityFacts
		reg  *registrationFacts
		heat reportdomain.Heatmap
	)
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) { act, err = s.activity(gctx, appID, w); return })
	g.Go(func() (err error) { reg, err = s.registrations(gctx, appID, w); return })
	g.Go(func() (err error) { heat, err = s.heatmap(gctx, "active.heatmap", appID, w); return })
	if err := g.Wait(); err != nil {
		return nil, err
	}

	now := s.now()
	elapsed := w.ElapsedDays(now)
	dau := act.all.Rolling(w.Days, 1)
	wau := act.all.Rolling(w.Days, 7)
	mau := act.all.Rolling(w.Days, 30)
	stickiness := make([]float64, w.Days)
	newActive := make([]float64, w.Days)
	returning := make([]float64, w.Days)
	for i := 0; i < w.Days; i++ {
		stickiness[i] = reportengine.Round(reportengine.Ratio(dau[i], mau[i]), 4)
		newActive[i] = float64(act.all.Day(i).AndCardinality(reg.byDay[i]))
		returning[i] = dau[i] - newActive[i]
	}
	prevDau := make([]float64, w.Days)
	for i := 0; i < w.Days; i++ {
		prevDau[i] = reportengine.Cardinality(act.all.Day(i - w.Days))
	}
	activeCur := act.all.Union(0, w.Days-1)
	activePrev := act.all.Union(-w.Days, -1)
	activeN, activePrevN := reportengine.Cardinality(activeCur), reportengine.Cardinality(activePrev)
	sumDau, sumPrevDau := reportengine.Sum(dau[:elapsed]), reportengine.Sum(prevDau)

	signals := make([]reportengine.Group, 0, 3)
	for _, sig := range []struct {
		id  int
		key string
	}{{pgrepo.ReportSignalLogin, "login"}, {pgrepo.ReportSignalSession, "session"}, {pgrepo.ReportSignalSignIn, "signin"}} {
		users := int64(act.signals[sig.id].Union(0, w.Days-1).GetCardinality())
		signals = append(signals, reportengine.Group{Key: sig.key, Count: users, Users: users})
	}
	var stickAvg float64
	for _, v := range stickiness[:elapsed] {
		stickAvg += v
	}
	stickAvg /= float64(elapsed)

	lastIdx := elapsed - 1
	return &reportdomain.Active{
		Window: w.Public(now),
		KPIs: map[string]reportdomain.KPI{
			"activeUsers":   kpi(activeN, activePrevN, uniqueBuckets(w, act.all)),
			"avgDau":        kpi(sumDau/float64(elapsed), sumPrevDau/float64(w.Days), avgBuckets(w, dau, elapsed)),
			"peakDau":       reportengine.NewKPI(maxOf(dau[:elapsed]), reportengine.Ptr(maxOf(prevDau)), nil),
			"wau":           reportengine.NewKPI(wau[lastIdx], nil, lastBuckets(w, wau, elapsed)),
			"mau":           reportengine.NewKPI(mau[lastIdx], nil, lastBuckets(w, mau, elapsed)),
			"stickiness":    reportengine.NewKPI(reportengine.Round(stickAvg, 4), nil, avgBuckets(w, stickiness, elapsed)),
			"avgActiveDays": ratioKPI(sumDau, activeN, sumPrevDau, activePrevN),
		},
		Trend: reportdomain.Trend{
			Buckets: w.Buckets(),
			Series: map[string][]float64{
				"dau":        avgBuckets(w, dau, elapsed),
				"wau":        lastBuckets(w, wau, elapsed),
				"mau":        lastBuckets(w, mau, elapsed),
				"stickiness": avgBuckets(w, stickiness, elapsed),
				"new":        sumBuckets(w, newActive),
				"returning":  avgBuckets(w, returning, elapsed),
				"unique":     uniqueBuckets(w, act.all),
			},
		},
		Lifecycle: lifecycle(activeCur, activePrev, reg.current),
		Frequency: reportengine.Bands(act.all.DayCounts(0, w.Days-1), []int{1, 2, 4, 8, 15}),
		Signals:   reportengine.ToSlices(signals, reportengine.SliceOptions{KeepOrder: true}),
		Heatmap:   heat,
	}, nil
}

// Retention 留存
func (s *ReportService) Retention(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Retention, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		act *activityFacts
		reg *registrationFacts
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { act, err = s.activity(gctx, appID, w); return })
	g.Go(func() (err error) { reg, err = s.registrations(gctx, appID, w); return })
	if err := g.Wait(); err != nil {
		return nil, err
	}
	unit := "day"
	if p.Unit == "week" {
		unit = "week"
	}
	mode := "exact"
	if p.Mode == "rolling" {
		mode = "rolling"
	}
	last := act.lastObserved()

	type cohort struct {
		label string
		start int // 首日（窗口坐标，周队列可能早于窗口）
		users *roaring64.Bitmap
	}
	cohorts := make([]cohort, 0, w.Days)
	var offsets []int
	length := 1
	if unit == "week" {
		offsets = []int{1, 2, 3, 4, 5, 6, 7, 8}
		length = 7
		for d := 0; d < w.Days; d++ {
			weekStart := d - weekdayOffset(w, d)
			if len(cohorts) == 0 || cohorts[len(cohorts)-1].start != weekStart {
				cohorts = append(cohorts, cohort{label: w.DayLabel(weekStart), start: weekStart, users: roaring64.New()})
			}
			cohorts[len(cohorts)-1].users.Or(reg.byDay[d])
		}
	} else {
		offsets = []int{1, 2, 3, 4, 5, 6, 7, 14, 30}
		for d := 0; d < w.Days; d++ {
			cohorts = append(cohorts, cohort{label: w.DayLabel(d), start: d, users: reg.byDay[d]})
		}
	}

	retainedSum := make([]float64, len(offsets))
	sizeSum := make([]float64, len(offsets))
	rows := make([]reportdomain.Cohort, 0, len(cohorts))
	for _, c := range cohorts {
		size := int64(c.users.GetCardinality())
		row := reportdomain.Cohort{Cohort: c.label, Size: size, Retained: make([]*int64, len(offsets)), Rates: make([]*float64, len(offsets))}
		for i, off := range offsets {
			from := c.start + off*length
			if from > last || size == 0 {
				continue
			}
			to := from + length - 1
			if mode == "rolling" {
				to = last
			}
			if to > last {
				to = last
			}
			n := int64(c.users.AndCardinality(act.all.Union(from, to)))
			rate := reportengine.Round(float64(n)/float64(size), 4)
			row.Retained[i] = &n
			row.Rates[i] = &rate
			retainedSum[i] += float64(n)
			sizeSum[i] += float64(size)
		}
		rows = append(rows, row)
	}
	average := make([]*float64, len(offsets))
	for i := range offsets {
		if sizeSum[i] > 0 {
			average[i] = reportengine.Ptr(reportengine.Round(retainedSum[i]/sizeSum[i], 4))
		}
	}

	kpis := map[string]reportdomain.KPI{}
	for _, n := range []int{1, 7, 30} {
		cur := dayRetention(w, reg.byDay, act, 0, n)
		prev := dayRetention(w, reg.prevByDay, act, -w.Days, n)
		kpis["d"+strconv.Itoa(n)] = reportengine.NewKPI(cur.rate(), cur.prevPtr(prev), nil)
	}
	kpis["cohortUsers"] = kpi(reportengine.Cardinality(reg.current), reportengine.Cardinality(reg.previous), nil)

	return &reportdomain.Retention{
		Window:  w.Public(s.now()),
		Unit:    unit,
		Mode:    mode,
		Offsets: offsets,
		Rows:    rows,
		Average: average,
		KPIs:    kpis,
	}, nil
}

// Funnel 本期注册用户的转化漏斗（截至今天的行为）
func (s *ReportService) Funnel(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Funnel, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		act     *activityFacts
		reg     *registrationFacts
		paid    = roaring64.New()
		engaged = roaring64.New()
		hours   []float64
	)
	now := s.now()
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) { act, err = s.activity(gctx, appID, w); return })
	g.Go(func() (err error) { reg, err = s.registrations(gctx, appID, w); return })
	g.Go(func() error {
		return s.pg.ReportCohortPayments(gctx, appID, w.Start, w.End(), func(userID int64, createdAt, paidAt time.Time) error {
			paid.Add(uint64(userID))
			hours = append(hours, math.Max(paidAt.Sub(createdAt).Hours(), 0))
			return nil
		})
	})
	g.Go(func() error {
		return s.pg.ReportEngagedUsers(gctx, appID, w.Start, now, func(userID int64) error {
			engaged.Add(uint64(userID))
			return nil
		})
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	cohort := reg.current
	last := act.lastObserved()
	// 回访：注册日之后任一天活跃。按天推进：registeredBefore 是「注册日早于今天」的累计位图
	registeredBefore := roaring64.New()
	returned := roaring64.New()
	var daysToReturn []float64
	for d := 0; d <= last; d++ {
		if d > 0 && d-1 < w.Days {
			registeredBefore.Or(reg.byDay[d-1])
		}
		fresh := roaring64.AndNot(roaring64.And(act.all.Day(d), registeredBefore), returned)
		if fresh.IsEmpty() {
			continue
		}
		it := fresh.Iterator()
		for it.HasNext() {
			if regDay, ok := reg.dayOf[it.Next()]; ok {
				daysToReturn = append(daysToReturn, float64(d-regDay))
			}
		}
		returned.Or(fresh)
	}
	counts := act.all.DayCounts(0, last)
	active3 := roaring64.New()
	it := returned.Iterator()
	for it.HasNext() {
		uid := it.Next()
		if counts[uid] >= 3 {
			active3.Add(uid)
		}
	}
	steps := []struct {
		key   string
		users *roaring64.Bitmap
	}{
		{"registered", cohort},
		{"returned", returned},
		{"active3", active3},
		{"paid", roaring64.And(active3, paid)},
	}
	out := make([]reportdomain.FunnelStep, len(steps))
	first := reportengine.Cardinality(cohort)
	for i, st := range steps {
		n := reportengine.Cardinality(st.users)
		prev := first
		if i > 0 {
			prev = reportengine.Cardinality(steps[i-1].users)
		}
		out[i] = reportdomain.FunnelStep{
			Key:         st.key,
			Users:       int64(n),
			StepRate:    reportengine.Round(reportengine.Ratio(n, prev), 4),
			OverallRate: reportengine.Round(reportengine.Ratio(n, first), 4),
		}
	}
	if len(out) > 0 && first > 0 {
		out[0].StepRate, out[0].OverallRate = 1, 1
	}

	// 每个注册桶的回访率与付费率
	returnRate, payRate := w.Zeros(), w.Zeros()
	sizes := w.Zeros()
	backs, pays := w.Zeros(), w.Zeros()
	for d := 0; d < w.Days; d++ {
		b := w.Bucket(d)
		sizes[b] += reportengine.Cardinality(reg.byDay[d])
		backs[b] += float64(reg.byDay[d].AndCardinality(returned))
		pays[b] += float64(reg.byDay[d].AndCardinality(paid))
	}
	for i := range sizes {
		returnRate[i] = reportengine.Round(reportengine.Ratio(backs[i], sizes[i]), 4)
		payRate[i] = reportengine.Round(reportengine.Ratio(pays[i], sizes[i]), 4)
	}

	cohortPaid := float64(cohort.AndCardinality(paid))
	return &reportdomain.Funnel{
		Window: w.Public(now),
		KPIs: map[string]reportdomain.KPI{
			"registered":   reportengine.NewKPI(first, nil, nil),
			"returnRate":   reportengine.NewKPI(reportengine.Ratio(reportengine.Cardinality(returned), first), nil, returnRate),
			"payRate":      reportengine.NewKPI(reportengine.Ratio(cohortPaid, first), nil, payRate),
			"interactRate": reportengine.NewKPI(reportengine.Ratio(float64(cohort.AndCardinality(engaged)), first), nil, nil),
		},
		Steps:        out,
		Trend:        reportdomain.Trend{Buckets: w.Buckets(), Series: map[string][]float64{"returnRate": returnRate, "payRate": payRate, "registered": sizes}},
		HoursToPay:   reportengine.Summarize(hours),
		DaysToReturn: reportengine.Summarize(daysToReturn),
	}, nil
}

// ── 公用计算 ──

func regDaily(w *reportengine.Window, reg *registrationFacts) *dailySeries {
	series := newDailySeries(w)
	for i := 0; i < w.Days; i++ {
		series.cur[i] = reportengine.Cardinality(reg.byDay[i])
		series.prev[i] = reportengine.Cardinality(reg.prevByDay[i])
	}
	return series
}

// revenueDaily 实收（元）按日，及本期 / 上期的付费用户
func revenueDaily(w *reportengine.Window, pay *paymentFacts) (*dailySeries, *roaring64.Bitmap, *roaring64.Bitmap) {
	series := newDailySeries(w)
	cur, prev := roaring64.New(), roaring64.New()
	for _, o := range pay.current {
		series.add(w, w.DayIndexOf(o.PaidAt), cents(o.Cents))
		if o.UserID > 0 {
			cur.Add(uint64(o.UserID))
		}
	}
	for _, o := range pay.previous {
		series.add(w, w.DayIndexOf(o.PaidAt), cents(o.Cents))
		if o.UserID > 0 {
			prev.Add(uint64(o.UserID))
		}
	}
	return series, cur, prev
}

func lifecycle(activeCur, activePrev, regCur *roaring64.Bitmap) reportdomain.Lifecycle {
	return reportdomain.Lifecycle{
		New:         int64(activeCur.AndCardinality(regCur)),
		Retained:    int64(activeCur.AndCardinality(activePrev)),
		Resurrected: int64(roaring64.AndNot(roaring64.AndNot(activeCur, activePrev), regCur).GetCardinality()),
		Churned:     int64(roaring64.AndNot(activePrev, activeCur).GetCardinality()),
	}
}

type retentionTally struct{ retained, size float64 }

func (r retentionTally) rate() float64 {
	return reportengine.Round(reportengine.Ratio(r.retained, r.size), 4)
}

// prevPtr 上期没有可观察的队列时不给对照
func (r retentionTally) prevPtr(prev retentionTally) *float64 {
	if prev.size == 0 {
		return nil
	}
	return reportengine.Ptr(prev.rate())
}

// dayRetention 第 n 日留存（恰好第 n 天回访），按人数加权。byDay[i] 是第 base+i 天注册的用户
func dayRetention(w *reportengine.Window, byDay []*roaring64.Bitmap, act *activityFacts, base, n int) retentionTally {
	var t retentionTally
	last := act.lastObserved()
	for i, cohort := range byDay {
		day := base + i
		if day+n > last {
			break
		}
		t.retained += float64(cohort.AndCardinality(act.all.Day(day + n)))
		t.size += reportengine.Cardinality(cohort)
	}
	return t
}

func dailyInsights(w *reportengine.Window, metric string, daily []float64) []reportdomain.Insight {
	if len(daily) < 7 {
		return nil
	}
	labels := make([]string, len(daily))
	for i := range labels {
		labels[i] = w.DayLabel(i)
	}
	return reportengine.Anomalies(metric, labels, daily, 14, 3, 5)
}

func topInsights(items []reportdomain.Insight, limit int) []reportdomain.Insight {
	sort.SliceStable(items, func(i, j int) bool { return math.Abs(items[i].Score) > math.Abs(items[j].Score) })
	if len(items) > limit {
		items = items[:limit]
	}
	if items == nil {
		items = []reportdomain.Insight{}
	}
	return items
}

func maxOf(values []float64) float64 {
	var m float64
	for _, v := range values {
		if v > m {
			m = v
		}
	}
	return m
}

// sumBucketsShifted 上期按日序列套到本期的桶上（第 i 天对第 i 天），用于同比叠线
func sumBucketsShifted(w *reportengine.Window, prev []float64) []float64 { return sumBuckets(w, prev) }

// weekdayOffset 第 d 天距所在周周一的天数
func weekdayOffset(w *reportengine.Window, d int) int {
	wd, _ := w.Weekday(w.DayStart(d))
	return wd
}
