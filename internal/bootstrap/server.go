package bootstrap

// UnifiedApp 是单进程同时承载 API 与 Worker 时两者的句柄。
// 启停由 RuntimeSystem（fx 生命周期）统一编排，见 NewUnifiedSystem：
// Worker 先启动、API 最后开始接流量；关停逆序，且每个组件的 Close 只执行一次。
type UnifiedApp struct {
	API    *APIApp
	Worker *WorkerApp
}
