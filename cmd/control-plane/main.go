package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/internal/database"
	"github.com/rymelabs/rymevisor/internal/health"
	"github.com/rymelabs/rymevisor/internal/logging"
	"github.com/rymelabs/rymevisor/internal/middleware"
	natspkg "github.com/rymelabs/rymevisor/internal/nats"
	cpHandler "github.com/rymelabs/rymevisor/services/controlplane/handler"
	cpNats "github.com/rymelabs/rymevisor/services/controlplane/nats"
	"github.com/rymelabs/rymevisor/services/controlplane/postgres"
	"github.com/rymelabs/rymevisor/services/controlplane"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := logging.New(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	ctx := context.Background()

	pool, err := database.Connect(ctx, cfg.Database)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer pool.Close()

	js, err := natspkg.Connect(ctx, cfg.NATS)
	if err != nil {
		logger.Warn("failed to connect to NATS, running without event publishing", zap.Error(err))
	}

	vmRepo := postgres.NewVMRepository(pool)
	nodeRepo := postgres.NewNodeRepository(pool)
	imageRepo := postgres.NewImageRepository(pool)
	backupRepo := postgres.NewBackupRepository(pool)
	snapRepo := postgres.NewSnapshotRepository(pool)
	flavorRepo := postgres.NewFlavorRepository(pool)
	keypairRepo := postgres.NewKeypairRepository(pool)

	var publisher controlplane.EventPublisher
	if js != nil {
		publisher = cpNats.NewEventPublisher(js)
	}

	svc := controlplane.NewService(vmRepo, nodeRepo, imageRepo, backupRepo, snapRepo, publisher)
	svc.SetLogger(logger)
	svc.InitPuller(cfg.Storage.ImagesPath, logger)
	svc.SetFlavorRepository(flavorRepo)
	svc.SetKeypairRepository(keypairRepo)

	// Recovery: resume downloads and reconcile VMs after restart/crash
	recovery := controlplane.NewRecovery(svc, logger)
	if err := recovery.Recover(ctx); err != nil {
		logger.Warn("initial recovery failed", zap.Error(err))
	}

	handler := cpHandler.NewHandler(svc)

	healthHandler := health.NewHandler()
	healthHandler.Register("database", func(ctx context.Context) error {
		return pool.Ping(ctx)
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Mount("/", handler.Routes())
	})
	r.Handle("/health/live", healthHandler.Liveness())
	r.Handle("/health/ready", healthHandler.Readiness())

	var httpHandler http.Handler = r
	httpHandler = middleware.RequestTracing(httpHandler)
	httpHandler = middleware.CORS()(httpHandler)
	httpHandler = middleware.Logger(httpHandler)
	httpHandler = middleware.Recoverer(httpHandler)

	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      httpHandler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		logger.Info("control-plane starting", zap.String("addr", cfg.Server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down control-plane")
	shutdownCtx, cancel := context.WithTimeout(ctx, cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("control-plane stopped")
}
