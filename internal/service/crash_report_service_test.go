package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/crashreport"
	apperrors "aegis/pkg/errors"
)

// fakeCrashReportStore 内存版仓储：按 (appid, reportId) 去重，按设备 / IP 与创建时间计数，行为与 SQL 一致。
type fakeCrashReportStore struct {
	items    []crashreport.Report
	countErr error
	inserts  int
	// raceOnce 模拟并发：第一次 Insert 时假装另一份同号报告刚好先落库
	raceOnce bool
}

func (f *fakeCrashReportStore) FindCrashReportByReportID(_ context.Context, appID int64, reportID string) (*crashreport.Report, error) {
	for index := range f.items {
		if f.items[index].AppID == appID && f.items[index].ReportID == reportID {
			item := f.items[index]
			return &item, nil
		}
	}
	return nil, nil
}

func (f *fakeCrashReportStore) InsertCrashReport(_ context.Context, item crashreport.Report) (*crashreport.Report, bool, error) {
	f.inserts++
	if f.raceOnce {
		f.raceOnce = false
		winner := item
		winner.PublicID = "CR-WINNER"
		winner.ID = int64(len(f.items) + 1)
		f.items = append(f.items, winner)
		return nil, false, nil
	}
	for _, existing := range f.items {
		if existing.AppID == item.AppID && existing.ReportID == item.ReportID {
			return nil, false, nil
		}
	}
	item.ID = int64(len(f.items) + 1)
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	f.items = append(f.items, item)
	return &item, true, nil
}

func (f *fakeCrashReportStore) CountRecentCrashReports(_ context.Context, appID int64, deviceID, clientIP string, since time.Time) (int64, int64, error) {
	if f.countErr != nil {
		return 0, 0, f.countErr
	}
	var byDevice, byIP int64
	for _, item := range f.items {
		if item.AppID != appID || item.CreatedAt.Before(since) {
			continue
		}
		if deviceID != "" && item.DeviceID == deviceID {
			byDevice++
		}
		if clientIP != "" && item.ClientIP == clientIP {
			byIP++
		}
	}
	return byDevice, byIP, nil
}

func (f *fakeCrashReportStore) GetCrashReport(context.Context, int64, int64) (*crashreport.Report, error) {
	return nil, nil
}

func (f *fakeCrashReportStore) ListCrashReports(context.Context, crashreport.Query) (*crashreport.Page, error) {
	return &crashreport.Page{}, nil
}

func (f *fakeCrashReportStore) CrashReportFacets(context.Context, int64, time.Time) (*crashreport.Facets, error) {
	return &crashreport.Facets{}, nil
}

func (f *fakeCrashReportStore) SetCrashReportsResolved(_ context.Context, _ int64, ids []int64, _ bool, _ string) (int64, error) {
	return int64(len(ids)), nil
}

func (f *fakeCrashReportStore) DeleteCrashReports(_ context.Context, _ int64, ids []int64) (int64, error) {
	if len(ids) == 1 && ids[0] == 404 {
		return 0, nil
	}
	return int64(len(ids)), nil
}

type fakeCrashLocator struct{ loc IPLocation }

func (f fakeCrashLocator) GetCached(context.Context, string) (IPLocation, bool) { return f.loc, true }

func newTestCrashReportService(store *fakeCrashReportStore) *CrashReportService {
	return &CrashReportService{log: zap.NewNop(), store: store, now: time.Now}
}

func validCrashInput() crashreport.SubmitInput {
	return crashreport.SubmitInput{
		AppID:      7,
		ReportID:   "rpt-00000001",
		Kind:       "java",
		Title:      "java.lang.NullPointerException",
		Report:     "java.lang.NullPointerException\n\tat a.b.C.d(C.kt:10)",
		OccurredAt: "2026-10-22T08:00:00+08:00",
		DeviceID:   "device-a",
		ClientIP:   "203.0.113.9",
	}
}

func crashErrCode(err error) int {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return 0
}

func TestCrashReportValidation(t *testing.T) {
	t.Parallel()
	svc := newTestCrashReportService(&fakeCrashReportStore{})
	cases := map[string]func(*crashreport.SubmitInput){
		"reportId 过短":    func(in *crashreport.SubmitInput) { in.ReportID = "short" },
		"reportId 非法字符":  func(in *crashreport.SubmitInput) { in.ReportID = "rpt 0000/0001" },
		"reportId 过长":    func(in *crashreport.SubmitInput) { in.ReportID = strings.Repeat("a", 65) },
		"未知 kind":        func(in *crashreport.SubmitInput) { in.Kind = "oom" },
		"空标题":            func(in *crashreport.SubmitInput) { in.Title = "   " },
		"空报告":            func(in *crashreport.SubmitInput) { in.Report = "\n" },
		"报告超过 256 KB":    func(in *crashreport.SubmitInput) { in.Report = strings.Repeat("x", crashreport.MaxReportBytes+1) },
		"缺发生时间":          func(in *crashreport.SubmitInput) { in.OccurredAt = "" },
		"发生时间不是 RFC3339": func(in *crashreport.SubmitInput) { in.OccurredAt = "2026-10-22 08:00:00" },
		"负的 versionCode": func(in *crashreport.SubmitInput) { v := int64(-1); in.VersionCode = &v },
	}
	for name, mutate := range cases {
		input := validCrashInput()
		mutate(&input)
		if _, err := svc.Submit(context.Background(), input); crashErrCode(err) != 40000 {
			t.Errorf("%s：期望 40000，得到 %v", name, err)
		}
	}

	// 恰好 256 KB 收下；大写 kind 归一
	input := validCrashInput()
	input.Report = strings.Repeat("x", crashreport.MaxReportBytes)
	input.Kind = "NATIVE"
	if _, err := svc.Submit(context.Background(), input); err != nil {
		t.Fatalf("恰好 256 KB 应当收下：%v", err)
	}
}

func TestCrashReportNormalizesFields(t *testing.T) {
	t.Parallel()
	store := &fakeCrashReportStore{}
	svc := newTestCrashReportService(store)
	svc.location = fakeCrashLocator{loc: IPLocation{CountryCode: "JP", Country: "日本", Region: "東京都", City: "東京"}}
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	code := int64(120)
	input := validCrashInput()
	input.Now = now
	input.Title = strings.Repeat("标", crashreport.MaxTitleRunes+50)
	input.Detail = strings.Repeat("d", crashreport.MaxDetailRunes+1)
	input.Report = "signal 11\x00 (SIGSEGV)\xff"
	input.OccurredAt = now.Add(time.Hour).Format(time.RFC3339) // 客户端时钟快了一小时
	input.VersionCode = &code
	result, err := svc.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Duplicate || !strings.HasPrefix(result.ID, "CR20261022") || len(result.ID) != 18 {
		t.Fatalf("对外编号应为 CR + 日期 + 8 位，得到 %+v", result)
	}
	saved := store.items[0]
	if got := len([]rune(saved.Title)); got != crashreport.MaxTitleRunes {
		t.Errorf("标题应截断到 %d 字，得到 %d", crashreport.MaxTitleRunes, got)
	}
	if len(saved.Detail) != crashreport.MaxDetailRunes {
		t.Errorf("详情应截断到 %d 字，得到 %d", crashreport.MaxDetailRunes, len(saved.Detail))
	}
	if strings.ContainsRune(saved.Report, 0) || !strings.Contains(saved.Report, "�") || saved.ReportBytes != len(saved.Report) {
		t.Errorf("正文应去掉 NUL、替换非法 UTF-8，并记下字节数，得到 %q / %d", saved.Report, saved.ReportBytes)
	}
	if !saved.OccurredAt.Equal(now) {
		t.Errorf("晚于服务器时间太多的发生时间应按服务器时间记，得到 %v", saved.OccurredAt)
	}
	if saved.Kind != "java" || saved.VersionCode == nil || *saved.VersionCode != 120 || saved.Location.CountryCode != "JP" {
		t.Errorf("字段落库不对：%+v", saved)
	}
}

func TestCrashReportDeduplicates(t *testing.T) {
	t.Parallel()
	store := &fakeCrashReportStore{}
	svc := newTestCrashReportService(store)
	first, err := svc.Submit(context.Background(), validCrashInput())
	if err != nil || first.Duplicate {
		t.Fatalf("第一次上报：%+v %v", first, err)
	}
	again, err := svc.Submit(context.Background(), validCrashInput())
	if err != nil || !again.Duplicate || again.ID != first.ID {
		t.Fatalf("同一 reportId 应返回原来那一条并标 duplicate，得到 %+v %v", again, err)
	}
	// 别的应用里同号不算重复
	other := validCrashInput()
	other.AppID = 8
	if result, err := svc.Submit(context.Background(), other); err != nil || result.Duplicate {
		t.Fatalf("不同应用的同号报告应各自入库，得到 %+v %v", result, err)
	}
	if len(store.items) != 2 {
		t.Fatalf("应只有两条记录，得到 %d", len(store.items))
	}

	// 并发重传：查重时还没有，插入时被另一次抢先 —— 按重复返回赢家的编号
	race := &fakeCrashReportStore{raceOnce: true}
	result, err := newTestCrashReportService(race).Submit(context.Background(), validCrashInput())
	if err != nil || !result.Duplicate || result.ID != "CR-WINNER" {
		t.Fatalf("并发重传应按重复返回先落库那一条，得到 %+v %v", result, err)
	}
}

func TestCrashReportRateLimit(t *testing.T) {
	t.Parallel()
	store := &fakeCrashReportStore{}
	svc := newTestCrashReportService(store)
	submit := func(reportID, deviceID, ip string) error {
		input := validCrashInput()
		input.ReportID = reportID
		input.DeviceID = deviceID
		input.ClientIP = ip
		_, err := svc.Submit(context.Background(), input)
		return err
	}
	for index := 0; index < crashreport.HourlyLimit; index++ {
		if err := submit("dev-a-"+padCrashIndex(index), "device-a", "198.51.100."+padCrashIndex(index)); err != nil {
			t.Fatalf("第 %d 份不应被限：%v", index+1, err)
		}
	}
	// 同一设备第 21 份：换 IP 也拦
	err := submit("dev-a-over", "device-a", "198.51.100.250")
	if crashErrCode(err) != 42900 {
		t.Fatalf("同一设备超过每小时 %d 份应报 42900，得到 %v", crashreport.HourlyLimit, err)
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.HTTPStatus != 429 {
		t.Fatalf("限频应为 HTTP 429，得到 %d", appErr.HTTPStatus)
	}
	// 已经报过的那一份重传不吃额度，仍按重复返回
	if err := submit("dev-a-"+padCrashIndex(0), "device-a", "198.51.100.1"); err != nil {
		t.Fatalf("重复上报不应被限频：%v", err)
	}

	// 同一 IP 第 21 份：换设备也拦
	for index := 0; index < crashreport.HourlyLimit; index++ {
		if err := submit("ip-b-"+padCrashIndex(index), "device-b-"+padCrashIndex(index), "192.0.2.1"); err != nil {
			t.Fatalf("IP 维度第 %d 份不应被限：%v", index+1, err)
		}
	}
	if err := submit("ip-b-over", "device-c", "192.0.2.1"); crashErrCode(err) != 42900 {
		t.Fatalf("同一 IP 超过上限应报 42900，得到 %v", err)
	}

	// 一小时前的不算
	for index := range store.items {
		store.items[index].CreatedAt = time.Now().Add(-2 * time.Hour)
	}
	if err := submit("dev-a-later", "device-a", "192.0.2.1"); err != nil {
		t.Fatalf("窗口过去后应能再报：%v", err)
	}

	// 计数失败放行
	broken := &fakeCrashReportStore{countErr: errors.New("db down")}
	if _, err := newTestCrashReportService(broken).Submit(context.Background(), validCrashInput()); err != nil {
		t.Fatalf("限频计数失败时应放行：%v", err)
	}
}

func TestCrashReportAdminIDs(t *testing.T) {
	t.Parallel()
	svc := newTestCrashReportService(&fakeCrashReportStore{})
	if _, err := svc.Delete(context.Background(), 7, nil); crashErrCode(err) != 40000 {
		t.Fatalf("空列表应报 40000，得到 %v", err)
	}
	tooMany := make([]int64, crashReportBatchLimit+1)
	for index := range tooMany {
		tooMany[index] = int64(index + 1)
	}
	if _, err := svc.SetResolved(context.Background(), 7, tooMany, true, "admin"); crashErrCode(err) != 40000 {
		t.Fatalf("超过批量上限应报 40000，得到 %v", err)
	}
	if _, err := svc.Delete(context.Background(), 7, []int64{404}); crashErrCode(err) != errCodeCrashReportNotFound {
		t.Fatalf("单条删除找不到应报 404 码，得到 %v", err)
	}
	if deleted, err := svc.Delete(context.Background(), 7, []int64{1, 1, 2, -3}); err != nil || deleted != 2 {
		t.Fatalf("应去重并忽略非法编号，得到 %d %v", deleted, err)
	}
	if _, err := svc.List(context.Background(), crashreport.Query{AppID: 7, Kind: "oom"}); crashErrCode(err) != 40000 {
		t.Fatalf("未知类型筛选应报 40000，得到 %v", err)
	}
}

func padCrashIndex(index int) string {
	return fmt.Sprintf("%04d", index)
}
