package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/zap"

	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
)

// 对外用户 ID 的端到端集成测试：真实 Postgres，跑全部迁移（两遍，回填与约束可重复执行），再验证
// 「建号即发号 → 号段与唯一 → 不可改 → 删号退役不复用 → 主键 ↔ 对外 ID 换算（限定应用）→
// 管理端列表 / 详情 / 按 ID 直达 → /me 与登录结果给对外 ID → OAuth2 subject 两种形态都认」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55497/postgres go test ./internal/service -run TestUserPublicIDIntegration
func TestUserPublicIDIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID, otherAppID = int64(32000), int64(32001)
	for _, id := range []int64{appID, otherAppID} {
		if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name, status) VALUES ($1, 'public-id-test', TRUE) ON CONFLICT DO NOTHING`, id); err != nil {
			t.Fatal(err)
		}
	}

	// 建号路径不传 public_id，由列默认值发号
	created, err := pg.CreateUser(ctx, appID, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if created.PublicID < userPublicIDMin || created.PublicID > 9007199254740991 {
		t.Fatalf("对外 ID 不在号段内：%d", created.PublicID)
	}
	if len(strconv.FormatInt(created.PublicID, 10)) != 16 {
		t.Fatalf("对外 ID 应为 16 位：%d", created.PublicID)
	}
	reloaded, err := pg.GetUserByID(ctx, created.ID)
	if err != nil || reloaded == nil || reloaded.PublicID != created.PublicID {
		t.Fatalf("读回的对外 ID 不一致：%v %v", reloaded, err)
	}

	// 批量建号：各不相同
	seen := map[int64]bool{created.PublicID: true}
	for i := 0; i < 300; i++ {
		u, err := pg.CreateUser(ctx, appID, "bulk"+strconv.Itoa(i), "")
		if err != nil {
			t.Fatal(err)
		}
		if seen[u.PublicID] {
			t.Fatalf("对外 ID 重复：%d", u.PublicID)
		}
		seen[u.PublicID] = true
	}

	// 不可改
	if _, err := pool.Exec(ctx, `UPDATE users SET public_id = public_id + 1 WHERE id = $1`, created.ID); err == nil ||
		!strings.Contains(err.Error(), "immutable") {
		t.Fatalf("改号应被触发器拒绝，得到 %v", err)
	}
	// 其它列照常可改
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 5 WHERE id = $1`, created.ID); err != nil {
		t.Fatalf("改其它列不应受影响：%v", err)
	}
	// 号段之外写不进去
	if _, err := pool.Exec(ctx, `INSERT INTO users (appid, account, public_id) VALUES ($1, 'tiny', 42)`, appID); err == nil {
		t.Fatal("号段之外的对外 ID 应被 CHECK 拒绝")
	}

	// 换算：限定应用
	ids, err := pg.GetUserPublicIDs(ctx, []int64{created.ID, 999999999})
	if err != nil || ids[created.ID] != created.PublicID || len(ids) != 1 {
		t.Fatalf("批量换算不对：%v %v", ids, err)
	}
	if id, err := pg.FindUserIDByPublicID(ctx, appID, created.PublicID); err != nil || id != created.ID {
		t.Fatalf("按对外 ID 找主键：%d %v", id, err)
	}
	if id, _ := pg.FindUserIDByPublicID(ctx, otherAppID, created.PublicID); id != 0 {
		t.Fatalf("别的应用不应摸到这个人：%d", id)
	}

	// 删号：号退役，生成函数不会再发它
	victim, err := pg.CreateUser(ctx, appID, "victim", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, victim.ID); err != nil {
		t.Fatal(err)
	}
	var retired bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_public_id_retired WHERE public_id = $1)`, victim.PublicID).Scan(&retired); err != nil || !retired {
		t.Fatalf("删号后对外 ID 应进退役表：%v %v", retired, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (appid, account, public_id) VALUES ($1, 'reuse', $2)`, appID, victim.PublicID); err != nil {
		// 显式写入不经过生成函数；这里只确认生成函数会跳过它
		t.Logf("显式复用退役号：%v", err)
	} else {
		var next int64
		if err := pool.QueryRow(ctx, `SELECT aegis_new_user_public_id()`).Scan(&next); err != nil || next == victim.PublicID {
			t.Fatalf("生成函数不应发出退役号：%d %v", next, err)
		}
	}

	// 管理端：列表（快速路径与条件路径）、详情、按 ID 直达都带对外 ID
	list, _, err := pg.ListAdminUsersByAppQuery(ctx, appID, userdomain.AdminUserQuery{Page: 1, Limit: 5}, 1, 5)
	if err != nil || len(list) == 0 || list[0].PublicID < userPublicIDMin {
		t.Fatalf("管理端快速列表：%v %v", list, err)
	}
	enabled := true
	list, _, err = pg.ListAdminUsersByAppQuery(ctx, appID, userdomain.AdminUserQuery{Keyword: "alice", Enabled: &enabled, Page: 1, Limit: 5}, 1, 5)
	if err != nil || len(list) != 1 || list[0].PublicID != created.PublicID {
		t.Fatalf("管理端条件列表：%v %v", list, err)
	}
	for _, keyword := range []string{strconv.FormatInt(created.PublicID, 10), "#" + strconv.FormatInt(created.PublicID, 10), "#" + strconv.FormatInt(created.ID, 10)} {
		list, _, err = pg.ListAdminUsersByAppQuery(ctx, appID, userdomain.AdminUserQuery{Keyword: keyword, Page: 1, Limit: 5}, 1, 5)
		if err != nil || len(list) != 1 || list[0].ID != created.ID {
			t.Fatalf("按 %q 直达：%v %v", keyword, list, err)
		}
	}
	detail, err := pg.GetAdminUserByApp(ctx, appID, created.ID)
	if err != nil || detail == nil || detail.PublicID != created.PublicID {
		t.Fatalf("管理端详情：%v %v", detail, err)
	}
	byIDs, err := pg.ListAdminUsersByIDs(ctx, appID, []int64{created.ID})
	if err != nil || len(byIDs) != 1 || byIDs[0].PublicID != created.PublicID {
		t.Fatalf("管理端按主键批量：%v %v", byIDs, err)
	}

	// 等级资料（用户端「我的等级」）给对外 ID
	level, err := pg.GetUserLevelProfile(ctx, created.ID, appID)
	if err != nil || level == nil || level.UserInfo.ID != created.PublicID {
		t.Fatalf("等级资料应给对外 ID：%v %v", level, err)
	}

	// OAuth2 subject：对外 ID 与切换前的主键两种都认，别的应用都不认
	auth := &AuthService{pg: pg, log: zap.NewNop()}
	if got, err := auth.ResolveOAuth2Subject(ctx, appID, OAuth2Subject(created), true); err != nil || got.ID != created.ID {
		t.Fatalf("对外 ID subject：%v %v", got, err)
	}
	if got, err := auth.ResolveOAuth2Subject(ctx, appID, LegacyOAuth2Subject(created.ID), true); err != nil || got.ID != created.ID {
		t.Fatalf("旧 subject：%v %v", got, err)
	}
	if _, err := auth.ResolveOAuth2Subject(ctx, otherAppID, OAuth2Subject(created), true); err == nil {
		t.Fatal("别的应用不应认出这个 subject")
	}

	// 登录结果的 userId 是对外 ID，主键不出现在 JSON 里
	login := &authdomain.LoginResult{UserID: created.ID, PublicUserID: created.PublicID, Account: created.Account}
	if raw := mustJSON(t, login); !strings.Contains(raw, `"userId":`+strconv.FormatInt(created.PublicID, 10)) ||
		strings.Contains(raw, `:`+strconv.FormatInt(created.ID, 10)+`,`) {
		t.Fatalf("登录结果 JSON：%s", raw)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
