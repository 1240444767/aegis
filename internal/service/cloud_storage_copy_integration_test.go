package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	cloudstorage "aegis/internal/domain/cloudstorage"
	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 云存储内容数据库副本的端到端集成测试：真实 Postgres + 本地存储。
//
// 走一遍「写入落副本 → 存储目录整体清空（容器重建）仍读得回来 → 回滚照常 →
// 迁移前的修订（没有副本）对象还在时读一次补写副本 → 对象没了也没副本时回 41060 而不是 502 →
// 带当前修订覆盖写即可恢复」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestCloudContentCopyIntegration
func TestCloudContentCopyIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(31100)

	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'cloud-copy-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := pg.UpsertStorageConfig(ctx, storagedomain.Config{
		Scope:      storagedomain.ScopeGlobal,
		Provider:   storagedomain.ProviderLocal,
		ConfigName: "local-cloud-copy",
		AccessMode: storagedomain.AccessPrivate,
		Enabled:    true,
		IsDefault:  true,
		ConfigData: map[string]any{"root_dir": root},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := cloudstorage.DefaultConfig(appID)
	cfg.Enabled = true
	cfg.StorageConfigName = "local-cloud-copy"
	if _, err := pg.SaveCloudStorageConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	userID := insertTestUser(t, ctx, pool, appID, "cloud-copy-a", "0")

	storage := NewStorageService(zap.NewNop(), pg, nil, "test")
	storage.SetLinkSigningKey("master")
	svc := NewCloudStorageService(zap.NewNop(), pg, storage)

	put := func(content string, ifRevision *int64) *cloudstorage.Item {
		t.Helper()
		item, err := svc.PutItem(ctx, CloudWriteInput{
			AppID: appID, UserID: userID, Namespace: "favorites", Key: "tools",
			Data: []byte(content), Encoding: cloudstorage.EncodingJSON,
			IfRevision: ifRevision,
		})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	read := func(revision int64) (string, error) {
		t.Helper()
		got, err := svc.GetItem(ctx, appID, userID, "favorites", "tools", revision)
		if err != nil {
			return "", err
		}
		return string(got.Content), nil
	}
	wipe := func() {
		t.Helper()
		if err := os.RemoveAll(filepath.Join(root, "cloud")); err != nil {
			t.Fatal(err)
		}
	}
	codeOf := func(err error) (int, int) {
		appErr, ok := errors.AsType[*apperrors.AppError](err)
		if !ok {
			t.Fatalf("应是 AppError，得到 %v", err)
		}
		return appErr.Code, appErr.HTTPStatus
	}

	put(`{"items":[1]}`, nil)
	second := put(`{"items":[1,2]}`, nil)

	// 存储目录被清空：当前与历史修订都从副本读回。
	wipe()
	if got, err := read(0); err != nil || got != `{"items":[1,2]}` {
		t.Fatalf("清空存储后应从副本读到当前修订，得到 %q %v", got, err)
	}
	if got, err := read(1); err != nil || got != `{"items":[1]}` {
		t.Fatalf("清空存储后应从副本读到历史修订，得到 %q %v", got, err)
	}
	rolled, err := svc.Rollback(ctx, appID, userID, "favorites", "tools", 1, &second.Revision, "")
	if err != nil {
		t.Fatalf("清空存储后回滚应照常，得到 %v", err)
	}
	if got, err := read(0); err != nil || got != `{"items":[1]}` || rolled.Revision != 3 {
		t.Fatalf("回滚后应读到第 1 版内容的第 3 版，得到 %q r%d %v", got, rolled.Revision, err)
	}

	// 迁移之前写入的修订没有副本：对象还在时读一次补写。
	legacy := put(`{"items":["legacy"]}`, nil)
	if _, err := pool.Exec(ctx, `UPDATE app_cloud_item_revisions SET content = NULL
WHERE user_id = $1 AND revision = $2`, userID, legacy.Revision); err != nil {
		t.Fatal(err)
	}
	if got, err := read(0); err != nil || got != `{"items":["legacy"]}` {
		t.Fatalf("没有副本时应从存储读到，得到 %q %v", got, err)
	}
	var backfilled bool
	if err := pool.QueryRow(ctx, `SELECT content IS NOT NULL FROM app_cloud_item_revisions
WHERE user_id = $1 AND revision = $2`, userID, legacy.Revision).Scan(&backfilled); err != nil {
		t.Fatal(err)
	}
	if !backfilled {
		t.Fatal("从存储读到的旧修订应补写副本")
	}

	// 对象没了、也没有副本：41060 / 410，而不是会被 Cloudflare 换成 HTML 的 502。
	lost := put(`{"items":["lost"]}`, nil)
	if _, err := pool.Exec(ctx, `UPDATE app_cloud_item_revisions SET content = NULL
WHERE user_id = $1 AND revision = $2`, userID, lost.Revision); err != nil {
		t.Fatal(err)
	}
	wipe()
	_, err = read(0)
	if code, status := codeOf(err); code != errCodeCloudContentLost || status != http.StatusGone {
		t.Fatalf("内容丢失应回 41060/410，得到 %d/%d", code, status)
	}
	_, err = svc.Rollback(ctx, appID, userID, "favorites", "tools", lost.Revision, nil, "")
	if code, _ := codeOf(err); code != errCodeCloudContentLost {
		t.Fatalf("回滚到丢失的修订应回 41060，得到 %d", code)
	}

	// 客户端带当前修订覆盖写即可恢复。
	healed := put(`{"items":["local"]}`, &lost.Revision)
	if got, err := read(0); err != nil || got != `{"items":["local"]}` || healed.Revision != lost.Revision+1 {
		t.Fatalf("覆盖写后应恢复可读，得到 %q %v", got, err)
	}

	// 超过副本上限的内容只放存储桶。
	big := make([]byte, 0, cloudstorage.DatabaseCopyLimit+16)
	big = append(big, '"')
	for int64(len(big)) < cloudstorage.DatabaseCopyLimit+8 {
		big = append(big, 'x')
	}
	big = append(big, '"')
	if !json.Valid(big) {
		t.Fatal("测试数据应是合法 JSON")
	}
	if databaseCopy(big) != nil {
		t.Fatal("超过副本上限的内容不应进数据库")
	}
}

func TestStorageObjectMissingIsRecognized(t *testing.T) {
	missing := storageObjectMissingError{apperrors.New(40481, http.StatusNotFound, "资源不可用")}
	if !errors.Is(missing, ErrStorageObjectMissing) {
		t.Fatal("应能认出对象不存在")
	}
	if appErr, ok := errors.AsType[*apperrors.AppError](missing); !ok || appErr.Code != 40481 {
		t.Fatal("对外仍应是 40481 AppError")
	}
	if !isStorageObjectNotFound(apperrors.New(40401, http.StatusNotFound, "文件不存在")) {
		t.Fatal("本地存储的 404 应算对象不存在")
	}
	if !isStorageObjectNotFound(os.ErrNotExist) {
		t.Fatal("os.ErrNotExist 应算对象不存在")
	}
	if isStorageObjectNotFound(errors.New("dial tcp: i/o timeout")) {
		t.Fatal("网络故障不应算对象不存在")
	}
	if isStorageObjectNotFound(apperrors.New(50313, http.StatusServiceUnavailable, "存储通道暂不可用")) {
		t.Fatal("熔断不应算对象不存在")
	}
}
