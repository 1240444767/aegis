package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	admindomain "aegis/internal/domain/admin"
	realtimedomain "aegis/internal/domain/realtime"
	redisrepo "aegis/internal/repository/redis"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	redislib "github.com/redis/go-redis/v9"
)

// liveTestClient 扮演控制台：握手、加解密、按类型等帧。
type liveTestClient struct {
	t       *testing.T
	conn    *websocket.Conn
	channel *liveChannel
}

func (c *liveTestClient) send(v any) {
	c.t.Helper()
	raw, _ := json.Marshal(v)
	if err := c.conn.WriteMessage(websocket.BinaryMessage, c.channel.seal(raw)); err != nil {
		c.t.Fatal(err)
	}
}

// waitFor 读帧直到出现指定类型（中途的快照、动态等跳过）。
func (c *liveTestClient) waitFor(kind string, accept func(map[string]any) bool) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.conn.SetReadDeadline(deadline)
		mt, payload, err := c.conn.ReadMessage()
		if err != nil {
			c.t.Fatalf("等待 %s 帧时连接出错：%v", kind, err)
		}
		if mt != websocket.BinaryMessage {
			c.t.Fatalf("握手后收到了明文帧：%s", payload)
		}
		plain, err := c.channel.open(payload)
		if err != nil {
			c.t.Fatalf("解密失败：%v", err)
		}
		var frame map[string]any
		_ = json.Unmarshal(plain, &frame)
		if frame["t"] == kind && (accept == nil || accept(frame)) {
			return frame
		}
	}
	c.t.Fatalf("5 秒内没有等到 %s 帧", kind)
	return nil
}

func startLiveHub(t *testing.T, access *admindomain.AccessContext) (*OnlineLiveHub, *redisrepo.RealtimeRepository, *liveTestClient) {
	t.Helper()
	mr := miniredis.RunT(t)
	// miniredis 的键过期只随 FastForward 推进，不跟真实时钟走；不推的话快照锁永不释放
	stopClock := make(chan struct{})
	t.Cleanup(func() { close(stopClock) })
	go func() {
		for {
			select {
			case <-stopClock:
				return
			case <-time.After(25 * time.Millisecond):
				mr.FastForward(25 * time.Millisecond)
			}
		}
	}()
	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	repo := redisrepo.NewRealtimeRepository(client, "t")
	realtime, err := NewRealtimeService(nil, nil, repo, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { realtime.Close(context.Background()) })

	hub := newOnlineLiveHub(nil, realtime, nil, nil, nil, nil)
	hub.tick = 150 * time.Millisecond
	hub.feedFlush = 50 * time.Millisecond
	hub.dimsEvery = 0
	hub.start()
	t.Cleanup(hub.Close)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = hub.Serve(w, r, access)
	}))
	t.Cleanup(server.Close)

	header := http.Header{}
	header.Set("Authorization", "Bearer live-token")
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var hello liveHello
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.Type != "hello" || hello.Suite != onlineLiveSuite {
		t.Fatalf("hello 帧不符：%+v", hello)
	}
	channel, clientKey := clientSide(t, hello, "live-token")
	if err := conn.WriteJSON(liveKeyFrame{Type: "key", Key: clientKey}); err != nil {
		t.Fatal(err)
	}
	return hub, repo, &liveTestClient{t: t, conn: conn, channel: channel}
}

func seedPresence(t *testing.T, repo *redisrepo.RealtimeRepository) {
	t.Helper()
	now := time.Now().UTC()
	for _, c := range []realtimedomain.PresenceConnection{
		{ConnectionID: "c1", AppID: 7, UserID: 1, Platform: "android", OS: "Android", OSVersion: "15.0", Country: "中国", Region: "广东", IP: "203.0.113.9", ServerID: "n1", LastSeenAt: now},
		{ConnectionID: "c2", AppID: 7, UserID: 2, Platform: "iOS", ServerID: "n1", LastSeenAt: now},
		{ConnectionID: "c3", AppID: 9, UserID: 3, Platform: "Android", ServerID: "n2", LastSeenAt: now},
		{ConnectionID: "adm", AppID: 0, UserID: 99, ServerID: "n1", LastSeenAt: now},
	} {
		if err := repo.UpsertConnection(context.Background(), c, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOnlineLiveSuperAdminSeesEverything(t *testing.T) {
	access := &admindomain.AccessContext{Session: admindomain.Session{AdminID: 1, Account: "root", IsSuperAdmin: true}}
	hub, repo, client := startLiveHub(t, access)
	seedPresence(t, repo)

	ready := client.waitFor("ready", nil)
	if ready["admin"].(map[string]any)["isSuperAdmin"] != true || ready["scope"].(map[string]any)["all"] != true {
		t.Fatalf("ready 帧不符：%v", ready)
	}
	snap := client.waitFor("snapshot", func(f map[string]any) bool {
		return f["totals"].(map[string]any)["users"].(float64) == 3
	})
	if len(snap["apps"].([]any)) != 2 || snap["admins"] == nil || snap["nodes"] == nil {
		t.Fatalf("超管快照应含全部应用、管理员与节点：%v", snap)
	}
	platforms := snap["dims"].(map[string]any)["platform"].([]any)
	if first := platforms[0].(map[string]any); first["key"] != "Android" || first["count"].(float64) != 2 {
		t.Fatalf("平台分布未归一大小写：%v", platforms)
	}

	client.send(map[string]any{"t": "sub", "id": "s1", "app": 7, "feed": true})
	client.waitFor("sub", nil)
	focused := client.waitFor("snapshot", func(f map[string]any) bool { return f["focus"] != nil })
	if focus := focused["focus"].(map[string]any); focus["users"].(float64) != 2 {
		t.Fatalf("聚焦应用的统计不符：%v", focus)
	}

	client.send(map[string]any{"t": "users", "id": "u1", "app": 7, "page": 1, "limit": 20})
	users := client.waitFor("users", nil)
	list := users["list"].(map[string]any)
	if list["total"].(float64) != 2 || len(list["items"].([]any)) != 2 {
		t.Fatalf("在线用户列表不符：%v", list)
	}

	client.send(map[string]any{"t": "users", "id": "u2", "app": 7, "q": "203.0.113"})
	found := client.waitFor("users", func(f map[string]any) bool { return f["re"] == "u2" })
	if items := found["list"].(map[string]any)["items"].([]any); len(items) != 1 {
		t.Fatalf("按 IP 前缀搜索应命中 1 人：%v", found)
	}

	client.send(map[string]any{"t": "admins", "id": "a1"})
	admins := client.waitFor("admins", nil)
	if items := admins["items"].([]any); len(items) != 1 {
		t.Fatalf("在线管理员不符：%v", admins)
	}

	// 上下线动态：只推聚焦应用的事件
	hub.realtime.publishPresence(realtimedomain.PresenceEvent{Kind: "online", AppID: 9, UserID: 5, At: time.Now()})
	hub.realtime.publishPresence(realtimedomain.PresenceEvent{Kind: "online", AppID: 7, UserID: 6, Account: "bob", At: time.Now()})
	feed := client.waitFor("feed", nil)
	events := feed["events"].([]any)
	if len(events) != 1 || events[0].(map[string]any)["account"] != "bob" {
		t.Fatalf("动态应只含聚焦应用的事件：%v", events)
	}

	client.send(map[string]any{"t": "ping", "id": "p", "ts": 123})
	if pong := client.waitFor("pong", nil); pong["ts"].(float64) != 123 {
		t.Fatalf("pong 未回填 ts：%v", pong)
	}
}

func TestOnlineLiveScopedAdminSeesOnlyAssignedApps(t *testing.T) {
	appID := int64(7)
	access := &admindomain.AccessContext{
		Session:     admindomain.Session{AdminID: 2, Account: "ops"},
		Assignments: []admindomain.Assignment{{RoleKey: "app_admin", AppID: &appID}},
	}
	_, repo, client := startLiveHub(t, access)
	seedPresence(t, repo)

	client.waitFor("ready", nil)
	snap := client.waitFor("snapshot", func(f map[string]any) bool {
		return f["totals"].(map[string]any)["connections"].(float64) == 2
	})
	totals := snap["totals"].(map[string]any)
	if totals["users"].(float64) != 2 || totals["apps"].(float64) != 1 {
		t.Fatalf("应用管理员只应看到应用 7：%v", totals)
	}
	if snap["admins"] != nil || snap["nodes"] != nil {
		t.Fatalf("非超管不应看到管理员与节点：%v", snap)
	}

	client.send(map[string]any{"t": "users", "id": "x", "app": 9})
	errFrame := client.waitFor("error", nil)
	if errFrame["code"].(float64) != 40311 {
		t.Fatalf("越权查看应用 9 应被拒：%v", errFrame)
	}
	client.send(map[string]any{"t": "admins", "id": "y"})
	if f := client.waitFor("error", func(f map[string]any) bool { return f["re"] == "y" }); f["code"].(float64) != 40311 {
		t.Fatalf("非超管查看在线管理员应被拒：%v", f)
	}
}

// 握手后发明文帧：服务端必须断开，不能当作请求处理。
func TestOnlineLiveRejectsPlaintextAfterHandshake(t *testing.T) {
	access := &admindomain.AccessContext{Session: admindomain.Session{AdminID: 1, IsSuperAdmin: true}}
	_, _, client := startLiveHub(t, access)
	client.waitFor("ready", nil)
	_ = client.conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"admins"}`))
	_ = client.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := client.conn.ReadMessage()
		if err == nil {
			continue
		}
		var closeErr *websocket.CloseError
		if ok := asCloseError(err, &closeErr); !ok || closeErr.Code != liveCloseHandshake {
			t.Fatalf("应以 %d 关闭，实际 %v", liveCloseHandshake, err)
		}
		return
	}
}

func asCloseError(err error, target **websocket.CloseError) bool {
	ce, ok := err.(*websocket.CloseError)
	if ok {
		*target = ce
	}
	return ok
}
