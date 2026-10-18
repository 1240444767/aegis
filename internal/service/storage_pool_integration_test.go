package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 存储池的端到端集成测试：真实 Postgres + 三个本地磁盘存储。
//
// 走一遍「默认池按优先级写 → 首选成员坏了自动换成员并退避 → 写满自动换成员 → 都写满回 507 →
// 上传规则拦截 → 迁移到另一个存储并设读取转向，源配置停用后照样读得到 → 永久删除真删文件」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestStoragePoolIntegration
func TestStoragePoolIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(31200)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'pool-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	// 测试库可能留着别的测试建的默认配置 / 默认池：清掉，免得干扰解析。
	if _, err := pool.Exec(ctx, `DELETE FROM storage_pools`); err != nil {
		t.Fatal(err)
	}

	mkConfig := func(name, root string) *storagedomain.Config {
		t.Helper()
		cfg, err := pg.UpsertStorageConfig(ctx, storagedomain.Config{
			Scope: storagedomain.ScopeGlobal, Provider: storagedomain.ProviderLocal, ConfigName: name,
			AccessMode: storagedomain.AccessPrivate, Enabled: true, ProxyDownload: true,
			ConfigData: map[string]any{"root_dir": root},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM storage_configs WHERE id = $1`, cfg.ID) })
		return cfg
	}
	// 「坏掉的」成员：根目录指向一个普通文件，建目录必然失败。
	brokenRoot := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(brokenRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := mkConfig("pool-broken", brokenRoot)
	smallRoot, bigRoot := t.TempDir(), t.TempDir()
	small := mkConfig("pool-small", smallRoot)
	big := mkConfig("pool-big", bigRoot)

	storage := NewStorageService(zap.NewNop(), pg, nil, "test")
	storage.SetLinkSigningKey("master")

	saved, err := storage.SavePool(ctx, 0, storagedomain.PoolInput{
		Name: "main", Strategy: storagedomain.PoolStrategyPriority, Enabled: true, IsDefault: true, ReadFallback: true,
		Members: []storagedomain.PoolMemberInput{
			{ConfigID: broken.ID, Priority: 0},
			{ConfigID: small.ID, Priority: 1, CapacityBytes: 10},
			{ConfigID: big.ID, Priority: 2, CapacityBytes: 25},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.IsDefault || len(saved.Members) != 3 || saved.MaxAttempts != storagedomain.PoolDefaultAttempts {
		t.Fatalf("池保存结果不对：%+v", saved)
	}

	upload := func(key, content string) (*storagedomain.StoredObject, error) {
		return storage.UploadForApp(ctx, appID, storagedomain.UploadInput{
			ObjectKey: key, FileName: filepath.Base(key), ContentType: "text/plain",
			ContentLength: int64(len(content)), Content: strings.NewReader(content), UploaderType: "user",
		})
	}

	// 1. 首选成员坏了：换到下一个，坏成员进入退避，记一条切换事件。
	first, err := upload("a/one.txt", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigID != small.ID {
		t.Fatalf("应改写到 pool-small，实际 %d", first.ConfigID)
	}
	if h := storage.pools.Health(broken.ID); h.ConsecutiveFailures != 1 || h.BackoffUntil == nil {
		t.Fatalf("坏成员应进入退避：%+v", h)
	}
	events, err := storage.ListEvents(ctx, storagedomain.EventQuery{PoolID: &saved.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, storagedomain.EventFailover) {
		t.Fatalf("应记一条切换事件：%+v", events)
	}

	// 2. 退避中的成员不再被试；pool-small 再写 8 字节会超 10 字节上限 → 写到 pool-big。
	second, err := upload("a/two.txt", "abcdefgh")
	if err != nil {
		t.Fatal(err)
	}
	if second.ConfigID != big.ID {
		t.Fatalf("pool-small 写满后应转到 pool-big，实际 %d", second.ConfigID)
	}
	if h := storage.pools.Health(broken.ID); h.ConsecutiveFailures != 1 {
		t.Fatalf("退避中的成员不该被再试一次：%+v", h)
	}

	// 3. 模拟：大文件哪个成员都放不下。
	plan, err := storage.SimulatePool(ctx, saved.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target != 0 {
		t.Fatalf("100 字节没有成员放得下，模拟结果却给了目标 %d", plan.Target)
	}

	// 4. 全部写满（坏成员手动恢复后仍然坏，会再次失败）：回 507。
	storage.ResetMemberHealth(ctx, broken.ID)
	_, err = upload("a/three.txt", strings.Repeat("z", 40))
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("应返回 AppError，得到 %v", err)
	}
	if appErr.HTTPStatus != http.StatusServiceUnavailable && appErr.HTTPStatus != http.StatusInsufficientStorage {
		t.Fatalf("坏成员失败、其余已满时应是 503 或 507，得到 %d %s", appErr.HTTPStatus, appErr.Message)
	}
	// 坏成员被移出池后，剩下的都满：明确回 507。
	if _, err := storage.SavePool(ctx, saved.ID, storagedomain.PoolInput{
		Name: "main", Strategy: storagedomain.PoolStrategyPriority, Enabled: true, IsDefault: true, ReadFallback: true,
		Members: []storagedomain.PoolMemberInput{
			{ConfigID: small.ID, Priority: 1, CapacityBytes: 10},
			{ConfigID: big.ID, Priority: 2, CapacityBytes: 25},
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = upload("a/four.txt", strings.Repeat("z", 40))
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInsufficientStorage || appErr.Code != 50780 {
		t.Fatalf("全部写满应回 50780/507，得到 %v", err)
	}

	// 5. 上传规则：只许 .png 的 user 上传被拦，admin 不受影响。
	if _, err := pool.Exec(ctx, `DELETE FROM storage_rules`); err != nil {
		t.Fatal(err)
	}
	resource := NewStorageResourceService(zap.NewNop(), pg)
	resource.SetObjectRemover(storage)
	if _, err := resource.CreateStorageRule(ctx, storagedomain.CreateRuleInput{
		Name: "only png", RuleType: RuleTypeFileType,
		RuleData: map[string]any{"mode": "allow", "extensions": []any{".png"}, "uploaderTypes": []any{"user"}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = storage.UploadForApp(ctx, appID, storagedomain.UploadInput{
		ConfigName: "pool-big", ObjectKey: "r/x.txt", FileName: "x.txt", ContentLength: 1, Content: strings.NewReader("x"), UploaderType: "user",
	})
	if !errors.As(err, &appErr) || appErr.Code != 41500 {
		t.Fatalf("user 上传 .txt 应被类型规则拦下，得到 %v", err)
	}
	if _, err := storage.UploadForApp(ctx, appID, storagedomain.UploadInput{
		ConfigName: "pool-big", ObjectKey: "r/x.txt", FileName: "x.txt", ContentLength: 1, Content: strings.NewReader("x"), UploaderType: "admin",
	}); err != nil {
		t.Fatalf("规则只管 user，admin 上传不该被拦：%v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM storage_rules`); err != nil {
		t.Fatal(err)
	}

	// 6. 迁移：pool-small 的对象搬到新的 pool-archive，设读取转向；源停用后照样读得到。
	archiveRoot := t.TempDir()
	archive := mkConfig("pool-archive", archiveRoot)
	migration, err := storage.CreateMigration(ctx, storagedomain.MigrationInput{
		SourceConfigID: small.ID, TargetConfigID: archive.ID, DeleteSource: true, SetRedirect: true,
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if migration.TotalObjects != 1 {
		t.Fatalf("pool-small 里应有 1 个待迁移对象，得到 %d", migration.TotalObjects)
	}
	waitMigration(t, storage, migration.ID)
	done, _ := storage.GetMigration(ctx, migration.ID)
	if done.Status != storagedomain.MigrationCompleted || done.CopiedObjects != 1 || done.CopiedBytes != 8 {
		t.Fatalf("迁移结果不对：%+v", done)
	}
	if _, err := os.Stat(filepath.Join(archiveRoot, first.Key)); err != nil {
		t.Fatalf("目标存储里应有迁过去的文件：%v", err)
	}
	if _, err := os.Stat(filepath.Join(smallRoot, first.Key)); !os.IsNotExist(err) {
		t.Fatalf("开了删除源对象，源文件应已删除：%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE storage_configs SET enabled = FALSE WHERE id = $1`, small.ID); err != nil {
		t.Fatal(err)
	}
	data, _, err := storage.ReadObjectBytes(ctx, small.ID, first.Key, 1024)
	if err != nil || string(data) != "12345678" {
		t.Fatalf("源配置停用后，按源配置读应转向目标：%q %v", data, err)
	}
	link, err := storage.PermanentObjectLink(ctx, appID, small.ID, first.Key, false, "")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(link, "/api/storage/proxy/")
	_, servedBy, reader, err := storage.OpenProxyObject(ctx, token)
	if err != nil {
		t.Fatalf("迁移后旧的永久地址应仍可读：%v", err)
	}
	body, _ := io.ReadAll(reader.Body)
	reader.Body.Close()
	if servedBy.ID != archive.ID || string(body) != "12345678" {
		t.Fatalf("应由目标存储供应：%d %q", servedBy.ID, body)
	}

	// 7. 永久删除：软删后清除，磁盘上的文件真的没了。
	objects, err := pg.StorageObjectsAfter(ctx, big.ID, 0, 10)
	if err != nil || len(objects) == 0 {
		t.Fatalf("pool-big 里应有对象：%v", err)
	}
	target := objects[0]
	if err := resource.SoftDeleteStorageObject(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	result, err := resource.BatchMutateObjects(ctx, storagedomain.BatchActionPurge, []int64{target.ID})
	if err != nil || result.Affected != 1 {
		t.Fatalf("永久删除应成功：%+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(bigRoot, target.ObjectKey)); !os.IsNotExist(err) {
		t.Fatalf("永久删除后磁盘上的文件应已删除：%v", err)
	}

	// 8. 总览：配置、池、迁移都在，写入目标是默认池。
	overview, err := storage.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.WriteTarget == nil || overview.WriteTarget.Kind != "pool" || overview.WriteTarget.PoolID != saved.ID {
		t.Fatalf("写入目标应是默认池：%+v", overview.WriteTarget)
	}
	if len(overview.Migrations) == 0 || len(overview.Pools) == 0 {
		t.Fatalf("总览缺内容：%+v", overview)
	}
	// 密钥字段出网抹除，只报告「已配置」。
	r2, err := storage.Save(ctx, storagedomain.ConfigMutation{
		Scope: storagedomain.ScopeGlobal, Provider: ptrTo(storagedomain.ProviderR2), ConfigName: ptrTo("pool-r2"),
		AccessMode: ptrTo(storagedomain.AccessPrivate),
		ConfigData: map[string]any{"account_id": strings.Repeat("ab", 16), "bucket": "b", "access_key_id": "AK", "secret_access_key": "SECRET"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM storage_configs WHERE id = $1`, r2.ID) })
	redacted := storagedomain.RedactConfig(*r2)
	if _, leaked := redacted.ConfigData["secret_access_key"]; leaked || len(redacted.SecretsSet) != 1 {
		t.Fatalf("密钥应被抹除并报告已配置：%+v", redacted)
	}
	// 编辑时不带密钥（表单拿不到原值）：保存后原密钥保留，其余字段照改。
	updated, err := storage.Save(ctx, storagedomain.ConfigMutation{
		ID: r2.ID, Scope: storagedomain.ScopeGlobal,
		ConfigData: map[string]any{"account_id": strings.Repeat("ab", 16), "bucket": "renamed", "access_key_id": "AK", "secret_access_key": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ConfigData["secret_access_key"] != "SECRET" || updated.ConfigData["bucket"] != "renamed" {
		t.Fatalf("留空的密钥应保留原值：%+v", updated.ConfigData)
	}
	for _, item := range overview.Configs {
		if _, leaked := item.Config.ConfigData["secret_access_key"]; leaked {
			t.Fatalf("总览不应带出密钥：%s", item.Config.ConfigName)
		}
	}
}

func hasEvent(events []storagedomain.PoolEvent, kind string) bool {
	for _, e := range events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func waitMigration(t *testing.T, storage *StorageService, id int64) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		m, err := storage.GetMigration(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		switch m.Status {
		case storagedomain.MigrationCompleted, storagedomain.MigrationFailed, storagedomain.MigrationCancelled:
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	m, _ := storage.GetMigration(context.Background(), id)
	t.Fatalf("迁移没有在 20 秒内结束：%+v", m)
}

