package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aegis/internal/bootstrap"
	"aegis/internal/config"
	"aegis/pkg/crashlog"

	"go.uber.org/zap"
)

func main() {
	started := time.Now()

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		// 配置读不起来时横幅配置也是零值，用默认值兜底，见 config.DefaultBannerConfig
		cfg.Banner = config.DefaultBannerConfig()
	}
	cl := crashlog.New(cfg.CrashLog.Dir, cfg.CrashLog.MaxFiles, cfg.CrashLog.MaxSize)
	defer func() {
		if r := recover(); r != nil {
			cl.Write("main.fatal", r, false)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bootstrap.PrintBootBanner(cfg, bootstrap.RoleWorker)

	system, err := bootstrap.NewWorkerSystem(ctx, cl)
	if err != nil {
		panic(err)
	}

	system.Worker.Logger.Info("aegis worker starting")
	if err := system.StartWithTimeout(ctx); err != nil {
		_ = system.StopWithTimeout()
		panic(err)
	}
	bootstrap.PrintReadyBanner(ctx, system.Worker.BannerRuntimeOf(bootstrap.RoleWorker, time.Since(started)))

	runErr := system.Wait(ctx)
	if err := system.StopWithTimeout(); err != nil {
		system.Worker.Logger.Warn("graceful shutdown incomplete", zap.Error(err))
	}
	if runErr != nil {
		panic(runErr)
	}
}
