package bootstrap

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
)

// drainNATS 让连接进入 Drain 并等它自行关闭。
//
// nats.go 的 Drain 是异步的：它先退订、等在途 handler 处理完并把 Ack 刷出去，
// 然后才自己关闭连接。紧跟着调用 Close 会把这个过程拦腰截断，
// 在途消息既没处理完也没 Ack，重启后被 JetStream 重投一遍。
// ctx 到期仍未排空时才强制 Close，避免关停被卡住超过 ShutdownTimeout。
func drainNATS(ctx context.Context, nc *nats.Conn) {
	if nc == nil || nc.IsClosed() {
		return
	}
	if err := nc.Drain(); err != nil {
		nc.Close()
		return
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for !nc.IsClosed() {
		select {
		case <-ctx.Done():
			nc.Close()
			return
		case <-tick.C:
		}
	}
}
