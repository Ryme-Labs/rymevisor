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
	"github.com/rymelabs/rymevisor/services/network"
	networkhandler "github.com/rymelabs/rymevisor/services/network/handler"
	"github.com/rymelabs/rymevisor/services/network/postgres"
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
	defer logger.Sync()

	ctx := context.Background()

	pool, err := database.Connect(ctx, cfg.Database)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer pool.Close()

	networkRepo := postgres.NewNetworkRepository(pool)
	subnetRepo := postgres.NewSubnetRepository(pool)
	firewallRepo := postgres.NewFirewallRepository(pool)
	floatingIPRepo := postgres.NewFloatingIPRepository(pool)
	svc := network.NewService(networkRepo, subnetRepo, firewallRepo, floatingIPRepo)

	h := networkhandler.NewHandler(svc)

	healthHandler := health.NewHandler()
	healthHandler.Register("database", func(ctx context.Context) error {
		return pool.Ping(ctx)
	})

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CORS())

	h.RegisterRoutes(r)

	r.Handle("/health/live", healthHandler.Liveness())
	r.Handle("/health/ready", healthHandler.Readiness())

	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		logger.Info("networking-engine starting", zap.String("addr", cfg.Server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down networking-engine")
	shutdownCtx, cancel := context.WithTimeout(ctx, cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("networking-engine stopped")
}
