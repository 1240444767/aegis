package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	avatardomain "aegis/internal/domain/avatar"
	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 迁移一个根目录整个丢了的本地存储（容器本地盘随部署清空、新挂的卷是空的）：
// 读取要应答「不存在」而不是故障；有数据库副本的（头像、云存储修订）写回目标，
// 没有的标记 missing 照常推进，任务正常完成并设读取转向。「重试」从头重扫且清零失败计数。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestStorageMigrationRecoveryIntegration
func TestStorageMigrationRecoveryIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(31300)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'migration-recovery') ON CONFLICT DO NOTHING`, appID); err != nil {
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
	goneRoot := filepath.Join(t.TempDir(), "storage")
	source := mkConfig("recovery-source", goneRoot)
	targetRoot := t.TempDir()
	target := mkConfig("recovery-target", targetRoot)

	storage := NewStorageService(zap.NewNop(), pg, nil, "test")
	storage.SetLinkSigningKey("master")

	provider, err := storage.buildProvider(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Open(ctx, source, "a/b.txt"); !isStorageObjectNotFound(err) {
		t.Fatalf("根目录不存在时读取应答「不存在」，得到 %v", err)
	}
	if err := deleteStoredObject(ctx, provider, source, "a/b.txt"); err != nil {
		t.Fatalf("根目录不存在时删除应视为已删除：%v", err)
	}

	// 头像：原图与 128 档都有数据库副本。
	userID := insertTestUser(t, ctx, pool, appID, "recovery-a", "0")
	baseKey := fmt.Sprintf("avatars/apps/%d/users/%d/2026/08/01_avatar.jpg", appID, userID)
	variantKey := fmt.Sprintf("avatars/apps/%d/users/%d/2026/08/01_avatar_128.jpg", appID, userID)
	if _, err := pg.ReplaceAvatarAssetWithBlobs(ctx, avatardomain.Asset{
		Owner:    avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: appID, ID: userID},
		ConfigID: source.ID, BaseKey: baseKey, ContentType: "image/jpeg", Source: avatardomain.SourceUpload,
		Variants: []avatardomain.Variant{{Size: 128, Key: variantKey, ContentType: "image/jpeg"}},
	}, []avatardomain.Blob{
		{Size: 0, ContentType: "image/jpeg", Data: []byte("original-bytes")},
		{Size: 128, ContentType: "image/jpeg", Data: []byte("v128")},
	}); err != nil {
		t.Fatal(err)
	}

	// 云存储修订：content 列有副本。
	cloudKey := fmt.Sprintf("cloud/%d/%d/favorites/tools/rev-1", appID, userID)
	var itemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO app_cloud_items (appid, user_id, namespace, item_key, revision)
VALUES ($1, $2, 'favorites', 'tools', 1) RETURNING id`, appID, userID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app_cloud_item_revisions
(item_id, appid, user_id, revision, storage_config_id, object_key, size, content)
VALUES ($1, $2, $3, 1, $4, $5, 11, $6)`, itemID, appID, userID, source.ID, cloudKey, []byte(`{"tools":1}`)); err != nil {
		t.Fatal(err)
	}

	lostKey := "tickets/202610/lost.jpg"
	for _, key := range []string{baseKey, variantKey, cloudKey, lostKey} {
		if _, err := pool.Exec(ctx, `INSERT INTO storage_objects (config_id, app_id, object_key, file_name, content_type, size)
VALUES ($1, $2, $3, $4, '', 10)`, source.ID, appID, key, filepath.Base(key)); err != nil {
			t.Fatal(err)
		}
	}

	migration, err := storage.CreateMigration(ctx, storagedomain.MigrationInput{
		SourceConfigID: source.ID, TargetConfigID: target.ID, SetRedirect: true,
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	waitMigration(t, storage, migration.ID)
	done, _ := storage.GetMigration(ctx, migration.ID)
	if done.Status != storagedomain.MigrationCompleted || done.CopiedObjects != 3 || done.RecoveredObjects != 3 ||
		done.SkippedObjects != 1 || done.FailedObjects != 0 {
		t.Fatalf("迁移结果不对：%+v", done)
	}
	for key, want := range map[string]string{baseKey: "original-bytes", variantKey: "v128", cloudKey: `{"tools":1}`} {
		got, err := os.ReadFile(filepath.Join(targetRoot, key))
		if err != nil || string(got) != want {
			t.Fatalf("目标里 %s 应是副本内容 %q，得到 %q（%v）", key, want, got, err)
		}
	}
	var lostConfig int64
	var lostStatus string
	if err := pool.QueryRow(ctx, `SELECT config_id, status FROM storage_objects WHERE object_key = $1`, lostKey).
		Scan(&lostConfig, &lostStatus); err != nil {
		t.Fatal(err)
	}
	if lostConfig != target.ID || lostStatus != "missing" {
		t.Fatalf("没有副本的对象应改到目标并标记 missing，得到 config=%d status=%s", lostConfig, lostStatus)
	}
	reloaded, err := pg.GetStorageConfigByID(ctx, source.ID)
	if err != nil || reloaded.RedirectConfigID == nil || *reloaded.RedirectConfigID != target.ID {
		t.Fatalf("迁移完成后源配置应转向目标：%+v %v", reloaded, err)
	}

	// 重试：一个失败的任务游标已经越过了留在源里的对象，「重试」要从头扫并清零失败计数。
	if _, err := pool.Exec(ctx, `INSERT INTO storage_objects (config_id, app_id, object_key, file_name, size)
VALUES ($1, $2, 'retry/left.txt', 'left.txt', 4)`, source.ID, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE storage_migrations SET status = 'failed', last_object_id = 1 << 40,
failed_objects = 50, last_error = 'boom' WHERE id = $1`, migration.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ControlMigration(ctx, migration.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	waitMigration(t, storage, migration.ID)
	retried, _ := storage.GetMigration(ctx, migration.ID)
	if retried.Status != storagedomain.MigrationCompleted || retried.FailedObjects != 0 || retried.SkippedObjects != 2 {
		t.Fatalf("重试应从头扫到留下的对象：%+v", retried)
	}
}
