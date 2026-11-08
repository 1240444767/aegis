package service

import (
	"testing"

	"go.uber.org/zap"
)

func TestAIGatewayLiveHubFiltersByAppAndResyncsLaggers(t *testing.T) {
	hub := newAIGatewayLiveHub(zap.NewNop())
	events, cancel := hub.subscribe(1)
	defer cancel()
	other, cancelOther := hub.subscribe(2)
	defer cancelOther()

	hub.publish(AIGatewayLiveEvent{Type: AIGatewayLiveCreated, AppID: 1, RequestNo: "A"})
	if event := <-events; event.RequestNo != "A" || event.At.IsZero() {
		t.Fatalf("应收到本应用的事件：%+v", event)
	}
	if len(other) != 0 {
		t.Fatal("别的应用不应收到")
	}

	// 塞满缓冲：多出来的丢掉，腾出位置后先补一个 resync。
	for i := 0; i < aiGatewayLiveBuffer+5; i++ {
		hub.publish(AIGatewayLiveEvent{Type: AIGatewayLiveProgress, AppID: 1})
	}
	for i := 0; i < aiGatewayLiveBuffer; i++ {
		<-events
	}
	hub.publish(AIGatewayLiveEvent{Type: AIGatewayLiveFinished, AppID: 1, RequestNo: "B"})
	if event := <-events; event.Type != AIGatewayLiveResync {
		t.Fatalf("丢过事件后应先补 resync，得到 %s", event.Type)
	}
	if event := <-events; event.RequestNo != "B" {
		t.Fatalf("resync 之后应是新事件：%+v", event)
	}

	cancel()
	hub.publish(AIGatewayLiveEvent{Type: AIGatewayLiveCreated, AppID: 1})
	if len(events) != 0 {
		t.Fatal("取消订阅后不应再收到")
	}
}

func TestAITraceRecordsAttemptsAndFirstToken(t *testing.T) {
	var kinds []string
	trace := newAITrace(func(kind string) { kinds = append(kinds, kind) })
	trace.beginAttempt(nil, "m1")
	trace.endAttempt(nil, &aiUpstreamError{Provider: "x", Status: 503, Message: "busy", Retryable: true})
	trace.beginAttempt(nil, "m1")
	trace.observeOutput(3)
	trace.observeOutput(3)
	trace.endAttempt(nil, nil)

	attempts := trace.snapshot()
	if len(attempts) != 2 || attempts[0].Status != "failed" || attempts[0].HTTPStatus != 503 || !attempts[0].Retryable ||
		attempts[1].Status != "succeeded" || attempts[1].FirstTokenMs <= 0 || attempts[1].Seq != 2 {
		t.Fatalf("尝试记录不对：%+v", attempts)
	}
	summary := trace.summary()
	progress := trace.progress()
	if summary.Attempts != 2 || summary.FirstTokenMs <= 0 || progress.OutputChars != 6 || progress.OutputTokens != 2 {
		t.Fatalf("摘要或进度不对：%+v %+v", summary, progress)
	}
	if len(kinds) != 5 || kinds[3] != "first-token" {
		t.Fatalf("变化回调不对：%v", kinds)
	}
	var none *aiTrace
	none.beginAttempt(nil, "")
	none.observeOutput(1)
	none.endAttempt(nil, nil)
}
