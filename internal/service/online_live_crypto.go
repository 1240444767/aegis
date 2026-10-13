package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// 实时在线统计长连接的应用层加密。
//
// 传输层已经是 WSS，再加一层的理由：在线统计里有用户 IP、设备与地区，
// 而 TLS 往往在反向代理、CDN 或 Next.js 同源代理那一跳就被终结，之后
// 是明文。应用层加密让这些数据只在浏览器与 Aegis 进程里出现明文。
//
// 握手（明文 JSON 文本帧，各一次）：
//
//	服务端 → {"t":"hello","v":1,"suite":"ECDH-P256/HKDF-SHA256/AES-256-GCM",
//	          "sid":"<uuid>","key":"<base64 服务端临时公钥，65 字节未压缩点>",
//	          "salt":"<base64 32 字节>"}
//	客户端 → {"t":"key","key":"<base64 客户端临时公钥>"}
//
// 派生：shared = ECDH(P-256)；
// okm = HKDF-SHA256(ikm=shared, salt, info="aegis/online-live/v1|<sid>|<hex(SHA256(访问令牌))>", 64)；
// 前 32 字节是客户端→服务端密钥，后 32 字节是服务端→客户端密钥。
// info 绑定令牌摘要：拿不到这名管理员令牌的中间人即使替换了双方公钥，
// 也派生不出同一把密钥，第一帧就会解密失败。
//
// 之后全部是二进制帧：
//
//	[0] 版本 = 1   [1] 标志 = 0（保留）   [2..9] 序号，大端 uint64，从 1 开始逐帧加一
//	[10..] AES-256-GCM 密文 + 16 字节标签
//
// nonce = 4 字节方向前缀（服务端发 "S2C\0"，客户端发 "C2S\0"）+ 8 字节序号；
// AAD = 帧头 10 字节。接收方要求序号恰好是上一帧加一，重放、乱序、丢帧一律断开。
// 临时密钥只存在于这条连接的内存里，连接一断即丢弃（前向保密）。
const (
	onlineLiveVersion   = 1
	onlineLiveSuite     = "ECDH-P256/HKDF-SHA256/AES-256-GCM"
	onlineLiveInfo      = "aegis/online-live/v1"
	onlineLiveHeaderLen = 10
	onlineLiveSaltLen   = 32
)

var (
	onlineLiveS2C = [4]byte{'S', '2', 'C', 0}
	onlineLiveC2S = [4]byte{'C', '2', 'S', 0}

	errLiveFrameMalformed = errors.New("加密帧格式错误")
	errLiveFrameSequence  = errors.New("加密帧序号不连续")
)

// liveHandshake 服务端一侧尚未完成的握手。
type liveHandshake struct {
	sessionID string
	private   *ecdh.PrivateKey
	salt      []byte
}

type liveHello struct {
	Type      string `json:"t"`
	Version   int    `json:"v"`
	Suite     string `json:"suite"`
	SessionID string `json:"sid"`
	Key       string `json:"key"`
	Salt      string `json:"salt"`
}

func newLiveHandshake() (*liveHandshake, error) {
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, onlineLiveSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return &liveHandshake{sessionID: uuid.NewString(), private: private, salt: salt}, nil
}

func (h *liveHandshake) hello() liveHello {
	return liveHello{
		Type:      "hello",
		Version:   onlineLiveVersion,
		Suite:     onlineLiveSuite,
		SessionID: h.sessionID,
		Key:       base64.StdEncoding.EncodeToString(h.private.PublicKey().Bytes()),
		Salt:      base64.StdEncoding.EncodeToString(h.salt),
	}
}

// complete 用客户端公钥完成握手，得到服务端一侧的加密通道。
func (h *liveHandshake) complete(clientKey string, accessToken string) (*liveChannel, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(clientKey))
	if err != nil {
		return nil, fmt.Errorf("客户端公钥不是合法的 base64：%w", err)
	}
	peer, err := ecdh.P256().NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("客户端公钥无效：%w", err)
	}
	shared, err := h.private.ECDH(peer)
	if err != nil {
		return nil, err
	}
	c2s, s2c, err := deriveLiveKeys(shared, h.salt, h.sessionID, accessToken)
	if err != nil {
		return nil, err
	}
	return newLiveChannel(s2c, onlineLiveS2C, c2s, onlineLiveC2S)
}

// deriveLiveKeys 返回（客户端→服务端，服务端→客户端）两把 32 字节密钥。
func deriveLiveKeys(shared, salt []byte, sessionID, accessToken string) ([]byte, []byte, error) {
	digest := sha256.Sum256([]byte(accessToken))
	info := onlineLiveInfo + "|" + sessionID + "|" + hex.EncodeToString(digest[:])
	okm, err := hkdf.Key(sha256.New, shared, salt, info, 64)
	if err != nil {
		return nil, nil, err
	}
	return okm[:32], okm[32:], nil
}

// liveChannel 一条连接上的双向加密状态。发送侧加锁：写泵与握手收尾可能并发调用。
type liveChannel struct {
	sendMu     sync.Mutex
	send       cipher.AEAD
	sendPrefix [4]byte
	sendSeq    uint64

	recv       cipher.AEAD
	recvPrefix [4]byte
	recvSeq    uint64
}

func newLiveChannel(sendKey []byte, sendPrefix [4]byte, recvKey []byte, recvPrefix [4]byte) (*liveChannel, error) {
	send, err := newLiveAEAD(sendKey)
	if err != nil {
		return nil, err
	}
	recv, err := newLiveAEAD(recvKey)
	if err != nil {
		return nil, err
	}
	return &liveChannel{send: send, sendPrefix: sendPrefix, recv: recv, recvPrefix: recvPrefix}, nil
}

func newLiveAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal 加密一帧明文，返回完整的二进制帧。
func (c *liveChannel) seal(plaintext []byte) []byte {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.sendSeq++
	frame := make([]byte, onlineLiveHeaderLen, onlineLiveHeaderLen+len(plaintext)+c.send.Overhead())
	frame[0] = onlineLiveVersion
	frame[1] = 0
	binary.BigEndian.PutUint64(frame[2:onlineLiveHeaderLen], c.sendSeq)
	nonce := liveNonce(c.sendPrefix, c.sendSeq)
	return c.send.Seal(frame, nonce[:], plaintext, frame[:onlineLiveHeaderLen])
}

// open 校验并解密一帧。只在读泵里调用，接收侧不加锁。
func (c *liveChannel) open(frame []byte) ([]byte, error) {
	if len(frame) < onlineLiveHeaderLen+c.recv.Overhead() || frame[0] != onlineLiveVersion || frame[1] != 0 {
		return nil, errLiveFrameMalformed
	}
	seq := binary.BigEndian.Uint64(frame[2:onlineLiveHeaderLen])
	if seq != c.recvSeq+1 {
		return nil, errLiveFrameSequence
	}
	nonce := liveNonce(c.recvPrefix, seq)
	plaintext, err := c.recv.Open(nil, nonce[:], frame[onlineLiveHeaderLen:], frame[:onlineLiveHeaderLen])
	if err != nil {
		return nil, err
	}
	c.recvSeq = seq
	return plaintext, nil
}

func liveNonce(prefix [4]byte, seq uint64) [12]byte {
	var nonce [12]byte
	copy(nonce[:4], prefix[:])
	binary.BigEndian.PutUint64(nonce[4:], seq)
	return nonce
}
