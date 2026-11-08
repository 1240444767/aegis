package bootstrap

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	temporalworker "go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

// fakeTemporalWorker 复刻 SDK 的行为：Stop 关闭内部 channel，第二次 Stop 会 panic。
type fakeTemporalWorker struct {
	temporalworker.Worker
	stopCh chan struct{}
	stops  atomic.Int32
}

func (f *fakeTemporalWorker) Stop() {
	f.stops.Add(1)
	close(f.stopCh)
}

func TestWorkerAppCloseIsIdempotent(t *testing.T) {
	tw := &fakeTemporalWorker{stopCh: make(chan struct{})}
	w := &WorkerApp{TemporalWorker: tw}

	// 信号路径与 fx OnStop 各调用一次，且可能并发
	done := make(chan struct{})
	go func() {
		w.Close(context.Background())
		close(done)
	}()
	w.Close(context.Background())
	<-done

	if got := tw.stops.Load(); got != 1 {
		t.Fatalf("temporal worker stopped %d times, want 1", got)
	}
}

func TestAPIAppCloseIsIdempotent(t *testing.T) {
	a := &APIApp{}
	a.Close(context.Background())
	a.Close(context.Background())
}

func TestSafeGoDoesNotRestartAfterNormalReturn(t *testing.T) {
	var calls atomic.Int32
	finished := make(chan struct{})
	SafeGo(zap.NewNop(), nil, "test.normal", true, func() {
		if calls.Add(1) == 1 {
			close(finished)
		}
	})
	<-finished
	time.Sleep(1500 * time.Millisecond) // 超过 1s 退避，若会重启此时已经第二次调用
	if got := calls.Load(); got != 1 {
		t.Fatalf("fn called %d times after normal return, want 1", got)
	}
}

func TestSafeGoRestartsAfterPanic(t *testing.T) {
	var calls atomic.Int32
	second := make(chan struct{})
	SafeGo(zap.NewNop(), nil, "test.panic", true, func() {
		switch calls.Add(1) {
		case 1:
			panic("boom")
		case 2:
			close(second)
		}
	})
	select {
	case <-second:
	case <-time.After(3 * time.Second):
		t.Fatal("fn was not restarted after panic")
	}
}

func TestDrainNATSNilIsNoop(t *testing.T) {
	drainNATS(context.Background(), nil)
}
