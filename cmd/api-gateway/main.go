package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/internal/health"
	"github.com/rymelabs/rymevisor/internal/logging"
	"github.com/rymelabs/rymevisor/internal/middleware"
	handler "github.com/rymelabs/rymevisor/services/gateway/handler"
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

	serviceCfg := handler.ServiceConfig{
		ControlPlaneURL: envOrDefault("RYMEVISOR_CONTROL_PLANE_URL", "localhost:8081"),
		AuthURL:         envOrDefault("RYMEVISOR_AUTH_URL", "localhost:8082"),
		NetworkURL:      envOrDefault("RYMEVISOR_NETWORK_URL", "localhost:8083"),
		StorageURL:      envOrDefault("RYMEVISOR_STORAGE_URL", "localhost:8084"),
		SchedulerURL:    envOrDefault("RYMEVISOR_SCHEDULER_URL", "localhost:8085"),
	}

	gw := handler.NewGateway(serviceCfg)

	healthHandler := health.NewHandler()

	mux := http.NewServeMux()
	mux.Handle("/", gw.ServeHTTP())
	mux.Handle("/health/live", healthHandler.Liveness())
	mux.Handle("/health/ready", healthHandler.Readiness())

	var httpHandler http.Handler = mux
	httpHandler = middleware.RequestID(httpHandler)
	httpHandler = middleware.RealIP(httpHandler)
	httpHandler = middleware.RequestTracing(httpHandler)
	httpHandler = middleware.CORS()(httpHandler)
	httpHandler = middleware.Logger(httpHandler)
	httpHandler = middleware.Recoverer(httpHandler)
	httpHandler = middleware.RateLimit(100, 200)(httpHandler)

	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      httpHandler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		logger.Info("api-gateway starting", zap.String("addr", cfg.Server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down api-gateway")
	shutdownCtx, cancel := context.WithTimeout(ctx, cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("api-gateway stopped")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
