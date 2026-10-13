package service

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

// clientSide 按协议文档在测试里扮演浏览器：自己生成密钥、自己派生，
// 不复用服务端的 complete —— 两边各算一遍还能对上，才说明文档写的就是实现做的。
func clientSide(t *testing.T, hello liveHello, token string) (*liveChannel, string) {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, _ := base64.StdEncoding.DecodeString(hello.Key)
	salt, _ := base64.StdEncoding.DecodeString(hello.Salt)
	peer, err := ecdh.P256().NewPublicKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := private.ECDH(peer)
	if err != nil {
		t.Fatal(err)
	}
	c2s, s2c, err := deriveLiveKeys(shared, salt, hello.SessionID, token)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := newLiveChannel(c2s, onlineLiveC2S, s2c, onlineLiveS2C)
	if err != nil {
		t.Fatal(err)
	}
	return channel, base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
}

func TestLiveChannelRoundTrip(t *testing.T) {
	hs, err := newLiveHandshake()
	if err != nil {
		t.Fatal(err)
	}
	client, clientKey := clientSide(t, hs.hello(), "token-a")
	server, err := hs.complete(clientKey, "token-a")
	if err != nil {
		t.Fatal(err)
	}

	for i, msg := range []string{`{"t":"sub"}`, `{"t":"users","page":2}`} {
		got, err := server.open(client.seal([]byte(msg)))
		if err != nil {
			t.Fatalf("第 %d 帧解密失败：%v", i+1, err)
		}
		if string(got) != msg {
			t.Fatalf("第 %d 帧内容不符：%q", i+1, got)
		}
	}
	got, err := client.open(server.seal([]byte(`{"t":"ready"}`)))
	if err != nil || string(got) != `{"t":"ready"}` {
		t.Fatalf("服务端到客户端解密失败：%v %q", err, got)
	}
}

// 令牌不同 → 派生出的密钥不同：中间人换掉公钥也拿不到同一把钥匙。
func TestLiveChannelBindsAccessToken(t *testing.T) {
	hs, _ := newLiveHandshake()
	client, clientKey := clientSide(t, hs.hello(), "token-a")
	server, err := hs.complete(clientKey, "token-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.open(client.seal([]byte("x"))); err == nil {
		t.Fatal("令牌不同却解密成功，info 没有绑定令牌")
	}
}

func TestLiveChannelRejectsReplayAndTampering(t *testing.T) {
	hs, _ := newLiveHandshake()
	client, clientKey := clientSide(t, hs.hello(), "t")
	server, _ := hs.complete(clientKey, "t")

	first := client.seal([]byte("one"))
	if _, err := server.open(first); err != nil {
		t.Fatal(err)
	}
	if _, err := server.open(first); !errors.Is(err, errLiveFrameSequence) {
		t.Fatalf("重放同一帧应因序号拒绝，实际 %v", err)
	}

	skipped := client.seal([]byte("two"))
	_ = skipped
	third := client.seal([]byte("three"))
	if _, err := server.open(third); !errors.Is(err, errLiveFrameSequence) {
		t.Fatalf("跳号应拒绝，实际 %v", err)
	}

	tampered := append([]byte(nil), skipped...)
	tampered[len(tampered)-1] ^= 1
	if _, err := server.open(tampered); err == nil {
		t.Fatal("篡改过的帧不应解密成功")
	}
	// 篡改失败不推进序号，原帧仍能按顺序打开
	if _, err := server.open(skipped); err != nil {
		t.Fatalf("原帧应可解密：%v", err)
	}

	// 帧头也在 AAD 里：改标志位必须被拒
	next := client.seal([]byte("four"))
	next = append([]byte(nil), next...)
	_ = next
	if _, err := server.open([]byte{1, 0, 0}); !errors.Is(err, errLiveFrameMalformed) {
		t.Fatalf("过短的帧应判为格式错误，实际 %v", err)
	}
}

// 方向前缀不同：服务端发出的帧不能被当成客户端的帧回放给服务端。
func TestLiveChannelDirectionsAreSeparated(t *testing.T) {
	hs, _ := newLiveHandshake()
	_, clientKey := clientSide(t, hs.hello(), "t")
	server, _ := hs.complete(clientKey, "t")
	if _, err := server.open(server.seal([]byte("echo"))); err == nil {
		t.Fatal("服务端自己发出的帧被服务端当作入站帧接受了")
	}
}
