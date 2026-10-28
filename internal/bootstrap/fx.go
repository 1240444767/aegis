package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"aegis/internal/config"
	"aegis/pkg/crashlog"
	"go.uber.org/fx"
)

type RuntimeSystem struct {
	app     *fx.App
	errors  <-chan error
	API     *APIApp
	Worker  *WorkerApp
	Unified *UnifiedApp

	stopOnce sync.Once
	stopErr  error
}

type runtimeMode string

const (
	runtimeModeAPI     runtimeMode = "api"
	runtimeModeWorker  runtimeMode = "worker"
	runtimeModeUnified runtimeMode = "unified"
)

type runtimeErrorReporter struct {
	ch   chan error
	once sync.Once
}

func newRuntimeErrorReporter() *runtimeErrorReporter {
	return &runtimeErrorReporter{ch: make(chan error, 1)}
}

func (r *runtimeErrorReporter) report(err error) {
	if r == nil || err == nil {
		return
	}
	r.once.Do(func() {
		r.ch <- err
	})
}

func NewAPISystem(ctx context.Context, cl *crashlog.Logger) (*RuntimeSystem, error) {
	return newRuntimeSystem(runtimeModeAPI, ctx, cl)
}

func NewWorkerSystem(ctx context.Context, cl *crashlog.Logger) (*RuntimeSystem, error) {
	return newRuntimeSystem(runtimeModeWorker, ctx, cl)
}

func NewUnifiedSystem(ctx context.Context, cl *crashlog.Logger) (*RuntimeSystem, error) {
	return newRuntimeSystem(runtimeModeUnified, ctx, cl)
}

func (s *RuntimeSystem) Start(ctx context.Context) error {
	if s == nil || s.app == nil {
		return fmt.Errorf("runtime system is not initialized")
	}
	return s.app.Start(ctx)
}

// Stop 只执行一次：入口的 defer 与 Run 的收尾都可能调用它。
func (s *RuntimeSystem) Stop(ctx context.Context) error {
	if s == nil || s.app == nil {
		return nil
	}
	s.stopOnce.Do(func() {
		s.stopErr = s.app.Stop(ctx)
	})
	return s.stopErr
}

// StartWithTimeout 按 ShutdownTimeout 为启动设上限：依赖迟迟连不上时让进程失败退出，
// 交给编排系统重启，而不是一直卡在半启动状态。
func (s *RuntimeSystem) StartWithTimeout(ctx context.Context) error {
	startCtx, cancel := context.WithTimeout(ctx, s.shutdownTimeout())
	defer cancel()
	return s.Start(startCtx)
}

// Wait 阻塞到收到停止信号（ctx 取消）或任一组件上报致命错误。
func (s *RuntimeSystem) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-s.errors:
		return err
	}
}

// StopWithTimeout 在 ShutdownTimeout 内完成优雅关停。
func (s *RuntimeSystem) StopWithTimeout() error {
	stopCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout())
	defer cancel()
	return s.Stop(stopCtx)
}

// Run = 启动 → 等待 → 关停。需要在启动完成后做事（打印就绪横幅等）时，
// 分开调用 StartWithTimeout / Wait / StopWithTimeout。
func (s *RuntimeSystem) Run(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("runtime system is not initialized")
	}
	if err := s.StartWithTimeout(ctx); err != nil {
		_ = s.StopWithTimeout()
		return err
	}
	defer func() { _ = s.StopWithTimeout() }()
	return s.Wait(ctx)
}

func (s *RuntimeSystem) shutdownTimeout() time.Duration {
	switch {
	case s.API != nil && s.API.Config.ShutdownTimeout > 0:
		return s.API.Config.ShutdownTimeout
	case s.Worker != nil && s.Worker.Config.ShutdownTimeout > 0:
		return s.Worker.Config.ShutdownTimeout
	default:
		return 30 * time.Second
	}
}

func newRuntimeSystem(mode runtimeMode, constructCtx context.Context, cl *crashlog.Logger) (*RuntimeSystem, error) {
	if constructCtx == nil {
		constructCtx = context.Background()
	}

	system := &RuntimeSystem{}
	reporter := newRuntimeErrorReporter()

	options := []fx.Option{
		fx.Supply(cl),
		fx.Supply(reporter),
		fx.Provide(func() (*config.Manager, error) {
			return config.NewManager()
		}),
	}

	switch mode {
	case runtimeModeAPI:
		options = append(options,
			fx.Provide(func(manager *config.Manager, crash *crashlog.Logger) (*APIApp, error) {
				return NewAPIAppWithConfigManager(constructCtx, crash, manager)
			}),
			fx.Invoke(registerAPIRuntimeLifecycle),
			fx.Populate(&system.API),
		)
	case runtimeModeWorker:
		options = append(options,
			fx.Provide(func(manager *config.Manager, crash *crashlog.Logger) (*WorkerApp, error) {
				return NewWorkerAppWithConfigManager(constructCtx, crash, manager)
			}),
			fx.Invoke(registerWorkerRuntimeLifecycle),
			fx.Populate(&system.Worker),
		)
	case runtimeModeUnified:
		options = append(options,
			fx.Provide(
				func(manager *config.Manager, crash *crashlog.Logger) (*APIApp, error) {
					return NewAPIAppWithConfigManager(constructCtx, crash, manager)
				},
				func(manager *config.Manager, crash *crashlog.Logger) (*WorkerApp, error) {
					return NewWorkerAppWithConfigManager(constructCtx, crash, manager)
				},
				func(api *APIApp, worker *WorkerApp) *UnifiedApp {
					return &UnifiedApp{
						API:    api,
						Worker: worker,
					}
				},
			),
			// fx 按注册顺序启动、逆序停止：Worker 先起、API 最后开始接流量；
			// 关停时 API 先停止接新请求，Worker 再把在途任务收尾。
			fx.Invoke(registerWorkerRuntimeLifecycle, registerAPIRuntimeLifecycle),
			fx.Populate(&system.API, &system.Worker, &system.Unified),
		)
	default:
		return nil, fmt.Errorf("unsupported runtime mode %q", mode)
	}

	app := fx.New(options...)
	if err := app.Err(); err != nil {
		return nil, err
	}

	system.app = app
	system.errors = reporter.ch
	return system, nil
}

func registerAPIRuntimeLifecycle(lc fx.Lifecycle, shutdowner fx.Shutdowner, reporter *runtimeErrorReporter, app *APIApp) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			runCtx, runCancel := context.WithCancel(context.Background())
			cancel = runCancel

			if err := app.Start(runCtx); err != nil {
				cancel()
				cancel = nil
				return err
			}

			if err := app.Serve(func(err error) {
				reporter.report(err)
				_ = shutdowner.Shutdown()
			}); err != nil {
				cancel()
				cancel = nil
				return err
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			app.Close(ctx)
			return nil
		},
	})
}

func registerWorkerRuntimeLifecycle(lc fx.Lifecycle, shutdowner fx.Shutdowner, reporter *runtimeErrorReporter, worker *WorkerApp) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			runCtx, runCancel := context.WithCancel(context.Background())
			cancel = runCancel

			go func() {
				if err := worker.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
					reporter.report(fmt.Errorf("worker run failed: %w", err))
					_ = shutdowner.Shutdown()
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			worker.Close(ctx)
			return nil
		},
	})
}
