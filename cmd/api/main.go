package main

import (
	"aegis/internal/bootstrap"
	"aegis/internal/config"
	"aegis/pkg/crashlog"
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			if err := bootstrap.RunMigrations(ctx); err != nil {
				panic(err)
			}
			return
		case "sync-legacy-user":
			if err := bootstrap.RunSyncLegacyUser(ctx, os.Args[2:]); err != nil {
				panic(err)
			}
			return
		case "sync-legacy-batch":
			if err := bootstrap.RunSyncLegacyBatch(ctx, os.Args[2:]); err != nil {
				panic(err)
			}
			return
		}
	}

	bootstrap.PrintBootBanner(cfg, bootstrap.RoleAPI)

	system, err := bootstrap.NewAPISystem(ctx, cl)
	if err != nil {
		panic(err)
	}

	system.API.Logger.Info("aegis api starting", zap.Int("port", system.API.Config.HTTPPort))
	if err := system.StartWithTimeout(ctx); err != nil {
		_ = system.StopWithTimeout()
		panic(err)
	}
	bootstrap.PrintReadyBanner(ctx, system.API.BannerRuntimeOf(bootstrap.RoleAPI, time.Since(started)))

	runErr := system.Wait(ctx)
	if err := system.StopWithTimeout(); err != nil {
		system.API.Logger.Warn("graceful shutdown incomplete", zap.Error(err))
	}
	if runErr != nil {
		panic(runErr)
	}
}
