// API 进程入口只负责加载配置、连接 MongoDB 并装配 HTTP Server。
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"my-travel/server/internal/app"
	"my-travel/server/internal/httpapi"
	"my-travel/server/internal/repository"
	"my-travel/server/internal/service"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	config, err := app.LoadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo, err := repository.Connect(ctx, config)
	if err != nil {
		slog.Error("mongo connection failed", "error", err)
		os.Exit(1)
	}
	defer repo.Close(context.Background())
	if err := repo.Initialize(ctx); err != nil {
		slog.Error("mongo initialization failed", "error", err)
		os.Exit(1)
	}
	travelService := service.Service{Repo: repo, Config: config}
	reconcile := travelService.ReconcilePendingSnapshots
	reconcilePendingOnce(
		context.Background(),
		config.SnapshotReconcileTimeout,
		config.SnapshotReconcileBatch,
		reconcile,
	)
	reconcileCtx, stopReconciler := context.WithCancel(context.Background())
	defer stopReconciler()
	go runSnapshotReconciler(
		reconcileCtx,
		config.SnapshotReconcileEvery,
		config.SnapshotReconcileTimeout,
		config.SnapshotReconcileBatch,
		reconcile,
	)
	server := &http.Server{Addr: config.APIAddress, Handler: httpapi.New(config, repo), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	slog.Info("travel api listening", "address", config.APIAddress)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}
