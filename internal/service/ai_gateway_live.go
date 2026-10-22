package service

import (
	"encoding/json"
	"sync"
	"time"

	"aegis/internal/domain/aigateway"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// 调用记录的实时推送（docs/ai-gateway.md「实时」）。
//
// 每次调用在暂扣、换通道、收到首字、流式进行中（至多每秒一次）、结束时各发一个事件。
// 有 NATS 时发到 aiGatewayLiveSubject，每个实例订阅同一个主题再分给本机的订阅者 ——
// 调用落在哪个实例、控制台连在哪个实例都看得到；没有 NATS（单机、测试）时直接本机分发。
//
// 推送尽力而为：订阅者的缓冲满了就丢事件，并给它补一个 resync，让控制台重新拉一次列表。

const (
	aiGatewayLiveSubject = "aegis.aigateway.live"
	aiGatewayLiveBuffer  = 256
	// aiGatewayProgressInterval 流式进度至多每秒推一次。
	aiGatewayProgressInterval = time.Second
)

// 实时事件类型。
const (
	AIGatewayLiveCreated  = "created"
	AIGatewayLiveAttempt  = "attempt"
	AIGatewayLiveProgress = "progress"
	AIGatewayLiveFinished = "finished"
	AIGatewayLiveResync   = "resync"
)

// AIGatewayLiveEvent 一条实时事件。Request 是列表形状的调用记录（不含追踪详情）。
type AIGatewayLiveEvent struct {
	Type      string                  `json:"type"`
	AppID     int64                   `json:"appid"`
	RequestNo string                  `json:"requestNo,omitempty"`
	Request   *aigateway.Request      `json:"request,omitempty"`
	Attempts  []aigateway.Attempt     `json:"attempts,omitempty"`
	Progress  *aigateway.LiveProgress `json:"progress,omitempty"`
	At        time.Time               `json:"at"`
}

type aiGatewayLiveSubscriber struct {
	appID  int64
	events chan AIGatewayLiveEvent
	// lagging 丢过事件、还没补 resync。
	lagging bool
}

type aiGatewayLiveHub struct {
	log  *zap.Logger
	mu   sync.RWMutex
	subs map[*aiGatewayLiveSubscriber]struct{}
	conn *nats.Conn
	sub  *nats.Subscription
}

func newAIGatewayLiveHub(log *zap.Logger) *aiGatewayLiveHub {
	return &aiGatewayLiveHub{log: log, subs: map[*aiGatewayLiveSubscriber]struct{}{}}
}

// attach 接上 NATS：之后发布走 NATS，本机订阅者从 NATS 收（含本机发出的）。
func (h *aiGatewayLiveHub) attach(conn *nats.Conn) {
	if conn == nil {
		return
	}
	sub, err := conn.Subscribe(aiGatewayLiveSubject, func(msg *nats.Msg) {
		var event AIGatewayLiveEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			return
		}
		h.deliver(event)
	})
	if err != nil {
		h.log.Warn("AI 网关实时推送订阅 NATS 失败，退回本机推送", zap.Error(err))
		return
	}
	h.mu.Lock()
	h.conn, h.sub = conn, sub
	h.mu.Unlock()
}

func (h *aiGatewayLiveHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sub != nil {
		_ = h.sub.Unsubscribe()
		h.sub = nil
	}
	h.conn = nil
}

// subscribe 订阅一个应用的事件。返回的 cancel 必须调用。
func (h *aiGatewayLiveHub) subscribe(appID int64) (<-chan AIGatewayLiveEvent, func()) {
	sub := &aiGatewayLiveSubscriber{appID: appID, events: make(chan AIGatewayLiveEvent, aiGatewayLiveBuffer)}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return sub.events, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, sub)
			h.mu.Unlock()
		})
	}
}

// publish 发一条事件。
func (h *aiGatewayLiveHub) publish(event AIGatewayLiveEvent) {
	if h == nil {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	h.mu.RLock()
	conn := h.conn
	h.mu.RUnlock()
	if conn != nil {
		data, err := json.Marshal(event)
		if err == nil {
			if err = conn.Publish(aiGatewayLiveSubject, data); err == nil {
				return
			}
		}
		h.log.Debug("AI 网关实时事件经 NATS 发布失败，改为本机分发", zap.Error(err))
	}
	h.deliver(event)
}

func (h *aiGatewayLiveHub) deliver(event AIGatewayLiveEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if sub.appID != event.AppID {
			continue
		}
		if sub.lagging {
			select {
			case sub.events <- AIGatewayLiveEvent{Type: AIGatewayLiveResync, AppID: event.AppID, At: time.Now()}:
				sub.lagging = false
			default:
				continue
			}
		}
		select {
		case sub.events <- event:
		default:
			sub.lagging = true
		}
	}
}
